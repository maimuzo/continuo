package orchestrator

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/maimuzo/continuo/internal/atomicfile"
	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/internal/statuslineserver"
)

// 使用率の保管値（issue #284。設計 3-27）。
//
// **値の出どころは2つある。**
//
//   - usage API（`rate_limit.source: oauth_usage_api` のとき。既定）… 巡回の先頭で
//     `poll_interval_ms` ごとに読み、OnAPISnapshot が入れる（orchestrator.go の pollAPI）
//   - ステータスライン … continuo が起動する Claude Code（issue の pane と statusline取得の pane）の
//     ステータスラインが運ぶ `rate_limits` を、`continuo statusline` が `sl.sock` へ送り、
//     OnStatusline が受ける（`source` が `none` でなく、statusline を使えるとき）
//
// 期間（5時間・7日・モデル別の7日）ごとに値を1つだけ保管する。**モデル別の7日（weekly_scoped）は
// usage API しか運ばない。**usage API の値も、ステータスラインの新しい応答の行と同じ規則で入れる。
//
// **落とし穴は4つある**（計画の「何が問題なのか」）。
//
//   - 止まっているセッションは古い値を送る。受け取った時刻を新しさとして使うと、古い値で入札する
//   - 期間は独立に欠けうる。欠けた期間を持つ行を「読めない」とすると入札が永久に止まる
//   - 上限で断られた呼び出しで `api_ms` が増えるかは分からない。増えた行の値だけを受けると、
//     上限の 100 を捨てうる
//   - アカウントを替えると使用率は下がり `resets_at` も変わる
//
// そこで、**新しい応答の行**（そのセッションで `api_ms` が基準より増えた行）と、
// **それ以外の行**を分けて扱う。

// quotaFileName は保管値を置くファイルの名前である（実行時ディレクトリの下）。
const quotaFileName = "quota.json"

// quotaFilePerm は quota.json の権限である。
const quotaFilePerm os.FileMode = 0o600

// quotaSessionTTL は、行が届かなくなったセッションの記録を消すまでの時間である。
const quotaSessionTTL = 24 * time.Hour

// quotaWindow は1つの期間の値である。
type quotaWindow struct {
	// Percent は used_percentage を切り捨てた値である。100 以上は 100。
	Percent int
	// ResetsAt は resets_at である。
	ResetsAt time.Time
	// Standin は、ResetsAt が usage API の返した値ではなく continuo が付けた仮の期限であるかである
	// （usage API が resets_at: null を返した期間。issue #284）。**仮の期限の切れは、新しさの判定で
	// 数えない**（ステータスラインは weekly_scoped を運ばないので、usage API から statusline取得へ
	// 切り替えると置き換わらず、新しい値が届いていても入札を見送ってしまう）。quota.json にも書かない。
	Standin bool
}

// sessionMark はセッションごとに覚えるものである。
type sessionMark struct {
	// baseAPIMs は基準の api_ms である。最初の行の値で始め、そのあとは rate_limits を持つ
	// 行を受けたらその行の値にする。基準より減った行を受けたら、その値に下げる。
	baseAPIMs int64
	// seenAt は最後に行を受けた時刻である（quotaSessionTTL で消す）。
	seenAt time.Time
}

// fetchWatch は、いま走っている statusline取得を見張るものである。やり直すたびに入れ替える。
type fetchWatch struct {
	// sessionID は statusline取得用のセッションの ID（--session-id に渡した UUID）である。
	sessionID string
	// sawResponse は、このセッションの行で api_ms が1度でも増えたかである。
	sawResponse bool
	// done は、rate_limits を持つ新しい応答の行を初めて受けたときに1度だけ閉じる。
	done chan struct{}
	// once は done を1度だけ閉じるために使う。**2本目以降の行で閉じ直すと panic して本体が落ちる。**
	once sync.Once
}

// newFetchWatch は見張りを作る。
func newFetchWatch(sessionID string) *fetchWatch {
	return &fetchWatch{sessionID: sessionID, done: make(chan struct{})}
}

// quotaStore は orchestrator が専用の錠（o.quotaMu）の下で持つ。o.mu とは別にする。
type quotaStore struct {
	// windows は期間ごとの保管値である。鍵は handoff.LimitKindSession（five_hour）と
	// handoff.LimitKindWeeklyAll（seven_day）と handoff.LimitKindWeeklyScoped（usage API だけが運ぶ）。
	// quota.json にも置く。
	windows map[string]quotaWindow
	// freshAt は新しさの時刻（rate_limits を持つ新しい応答の行か、usage API の値を最後に受けた時刻）である。
	freshAt time.Time
	// sessions はセッションごとの記録である。
	sessions map[string]sessionMark
	// fetch は、走っている statusline取得の見張りである。走っていなければ nil。
	// **閉じる仕事の Do が返り、goroutine が終わるときに nil に戻す。**
	fetch *fetchWatch
	// fetchRunning は、statusline取得の goroutine が走っているかである（fetch が nil の
	// 作り直しの合間も真のまま）。
	fetchRunning bool
	// lastAttemptAt は、前回の statusline取得の試行を始めた時刻である（開く条件に使う）。
	lastAttemptAt time.Time

	// 以下は `source: oauth_usage_api` のときだけ使う（issue #284。orchestrator.go の pollAPI）。
	//
	// **持つのは状態だけで、誤りの種類は持たない。**誤りの種類で分けるたびに、当たらない道が
	// 見つかったためである（API キーの機械と、トークンが読めない・失効したサブスクリプションの
	// 機械は、usage API の誤りの種類では区別できない）。

	// apiNextAt は usage API を次に試してよい時刻である。ゼロなら今すぐ試してよい。
	apiNextAt time.Time
	// apiSwitched は、usage API の直前の試しが誤りで、statusline取得へ切り替えているかである。
	apiSwitched bool
	// apiLastOK は、usage API の直前の試しが成功したかである（新しさの幅を決める）。
	apiLastOK bool
	// apiGaveUp は、トークンの読み取りの恒久的な失敗で、立て直すまで usage API を試さないかである
	// （切り替えたままにする）。
	apiGaveUp bool
	// everRead は、この起動のあいだに使用率を1度でも読めたかである。usage API が成功したか、
	// 使用率を持つ行（windowsOfLine が1つ以上を返す行。statusline取得でも issue の run の pane でも）を
	// 受けたら立てる。**quota.json から読み戻した値は数えない。**
	everRead bool
	// fetchStopped は取得止め（statusline取得を開かない）の印である。切り替えているあいだに、
	// haiku に話しかけたのに値が届かず、その時点で1度も読めていなければ立てる。
	// **everRead を立てるときに解く**（立てたあとに読めた場合も）。立て直すと解ける（メモリだけに持つ）。
	fetchStopped bool
}

// newQuotaStore は空の保管値を作る。
func newQuotaStore() quotaStore {
	return quotaStore{windows: map[string]quotaWindow{}, sessions: map[string]sessionMark{}}
}

// windowsOfLine は、行の期間を保管値の形にする（「値の形」の規則）。
//
// **used_percentage は切り捨てて int にし、100 以上は 100 にする。**上限の判定を
// 小数の切り上げで早めない。**今より前の resets_at の期間は取り込まない**（期間が消えたあとに、
// 過ぎた期間の行が遅れて届いても戻さない）。
func windowsOfLine(line statuslineserver.Line, now time.Time) map[string]quotaWindow {
	out := map[string]quotaWindow{}
	add := func(kind string, w *statuslineserver.Window) {
		if w == nil {
			return
		}
		qw, ok := shapeWindow(w.UsedPercentage, time.Unix(w.ResetsAt, 0), now)
		if ok {
			out[kind] = qw
		}
	}
	add(handoff.LimitKindSession, line.FiveHour)
	add(handoff.LimitKindWeeklyAll, line.SevenDay)
	return out
}

// shapeWindow は1つの期間に「値の形」の規則を当てる。
//
// 戻り値の2つ目: 取り込んでよければ true（resets_at が今より後）。
func shapeWindow(percent float64, resetsAt, now time.Time) (quotaWindow, bool) {
	if !resetsAt.After(now) {
		return quotaWindow{}, false
	}
	p := int(math.Floor(percent))
	if p > 100 {
		p = 100
	}
	return quotaWindow{Percent: p, ResetsAt: resetsAt}, true
}

// OnStatusline は statusline の受け口から1行を受ける（issue #284）。
//
// **受け口は run の状態（hook の時刻・stall の時計）へ何も書かない。**別の socket にした理由
// そのものである。テストもこの関数で行を入れる。
//
// line: `continuo statusline` が送った1行。
func (o *Orchestrator) OnStatusline(line statuslineserver.Line) {
	now := o.now()
	windows := windowsOfLine(line, now)
	hasRL := line.HasRateLimits()

	o.quotaMu.Lock()
	qs := &o.quota
	mark, known := qs.sessions[line.SessionID]
	newResponse := known && line.APIMs > mark.baseAPIMs
	changed := false
	switch {
	case !known:
		// **初めて見るセッションの最初の行。**基準をその行の api_ms にし、値は「それ以外の行」の
		// 規則で保管値に当てる。新しさの時刻は進めない（立て直した直後に上限に当たっている
		// run の 100 を捨てないため。止まっているセッションの古い値で新しさを作らないため）。
		mark.baseAPIMs = line.APIMs
		changed = applyOtherLine(qs.windows, windows)
	case newResponse && hasRL:
		// **新しい応答は今の値である。**まず resets_at の過ぎた保管値を全部消し（取り直した印を
		// 兼ねる）、期間ごとに resets_at が違えば置き換え、同じなら大きいほうを残す。
		// 行に無い期間で期限内の保管値は触らない。**期間は独立に欠けうる**ので、片方だけの
		// 行でも新しさの時刻を進める。
		changed = dropExpiredWindows(qs.windows, now)
		if applyNewResponse(qs.windows, windows) {
			changed = true
		}
		qs.freshAt = now
		mark.baseAPIMs = line.APIMs
	case newResponse:
		// **rate_limits が null の新しい応答の行。**保管値にも新しさの時刻にも触らない
		// （statusline取得の「応答はあったが値が無い」の判定にだけ使う）。基準も上げない
		// （api_ms だけが先に増えた null の行が、増えた分を使い切らないようにする）。
	default:
		if line.APIMs < mark.baseAPIMs {
			mark.baseAPIMs = line.APIMs
		}
		changed = applyOtherLine(qs.windows, windows)
	}
	mark.seenAt = now
	qs.sessions[line.SessionID] = mark
	// **使用率を持つ行なら、1度でも読めた印を立てる**（source: oauth_usage_api の取得止めを解く）。
	// 保管値への入れ方とは切り離す（同じ値の行で changed が偽でも立てる）。行の読み方は変えない。
	if len(windows) > 0 {
		qs.everRead = true
		qs.fetchStopped = false
	}

	if w := qs.fetch; w != nil && w.sessionID == line.SessionID && newResponse {
		w.sawResponse = true
		if hasRL {
			w.once.Do(func() { close(w.done) })
		}
	}
	for id, m := range qs.sessions {
		if now.Sub(m.seenAt) > quotaSessionTTL {
			delete(qs.sessions, id)
		}
	}
	o.quotaMu.Unlock()

	if changed {
		o.persistQuota()
	}
}

// applyNewResponse は、新しい応答の行の期間を保管値に当てる。
//
// 戻り値: 保管値が変わったら true。
func applyNewResponse(stored, incoming map[string]quotaWindow) bool {
	changed := false
	for kind, w := range incoming {
		cur, ok := stored[kind]
		switch {
		case !ok || !cur.ResetsAt.Equal(w.ResetsAt):
			stored[kind] = w
			changed = true
		case w.Percent > cur.Percent:
			stored[kind] = w
			changed = true
		}
	}
	return changed
}

// applyOtherLine は、新しい応答でない行の期間を保管値に当てる。
//
// **期間ごとに、保管値が無いか、resets_at が保管値より遅ければ置き換える。同じなら大きいほうを
// 残す。早ければ捨てる。**上限で断られた呼び出しで api_ms が増えなくても 100 を受け、
// 止まっているセッションの古い値では下がらない。
//
// 戻り値: 保管値が変わったら true。
func applyOtherLine(stored, incoming map[string]quotaWindow) bool {
	changed := false
	for kind, w := range incoming {
		cur, ok := stored[kind]
		switch {
		case !ok || w.ResetsAt.After(cur.ResetsAt):
			stored[kind] = w
			changed = true
		case w.ResetsAt.Equal(cur.ResetsAt) && w.Percent > cur.Percent:
			stored[kind] = w
			changed = true
		}
	}
	return changed
}

// dropExpiredWindows は resets_at の過ぎた保管値を消す。
//
// 戻り値: 消したものがあれば true。
func dropExpiredWindows(stored map[string]quotaWindow, now time.Time) bool {
	changed := false
	for kind, w := range stored {
		if !w.ResetsAt.After(now) {
			delete(stored, kind)
			changed = true
		}
	}
	return changed
}

// quotaFreshLocked は、入札に使ってよいほど値が新しいかを返す。o.quotaMu を持って呼ぶ。
//
// **新しさの時刻があり、そこから新しさの幅（quotaRefreshInterval）を過ぎておらず、保管値のどの
// 期間も resets_at を過ぎていないこと。**
func (o *Orchestrator) quotaFreshLocked(now time.Time) bool {
	qs := &o.quota
	if qs.freshAt.IsZero() || len(qs.windows) == 0 {
		return false
	}
	if now.Sub(qs.freshAt) >= o.quotaRefreshInterval() {
		return false
	}
	for _, w := range qs.windows {
		// **仮の期限の切れは数えない**（Standin のコメント）。切れた期間は snapshotOf が除くので、
		// 入札はその期間を使用率0と読む（resets_at が null の期間はまだ使っていない）。
		if !w.Standin && !w.ResetsAt.After(now) {
			return false
		}
	}
	return true
}

// apiSwitched は、usage API から statusline取得へ切り替えているかを返す（issue #284）。
// **o.quotaMu を持たずに呼ぶ**（中で取る）。
func (o *Orchestrator) apiSwitched() bool {
	o.quotaMu.Lock()
	defer o.quotaMu.Unlock()
	return o.quota.apiSwitched
}

// quotaRefreshInterval は新しさの幅（入札に使ってよい値の古さの上限）を返す。statusline取得の
// 間隔もこれを読む。**o.quotaMu を持って呼ぶ**（中で錠を取らない。apiLastOK を読むため）。
//
//   - 基本は `rate_limit.refresh_interval_ms`
//   - `source: oauth_usage_api` で `refresh_interval_ms` ≤ `polling.interval_ms` なら、
//     `polling.interval_ms` の2倍として扱う（起動は止めない。起動時に WARN を1回出す）
//   - `source: oauth_usage_api` で usage API の直前の試しが成功なら、
//     `max(上の値, poll_interval_ms + polling.interval_ms)`。usage API の次の読み取りが巡回1回ぶん
//     遅れても古い扱いにしない（statusline取得を開かない）ためである。**誤りに変わると上の値へ縮む**
func (o *Orchestrator) quotaRefreshInterval() time.Duration {
	d := time.Duration(o.cfg.RateLimit.RefreshIntervalMs) * time.Millisecond
	if d <= 0 {
		d = 5 * time.Minute
	}
	if o.cfg.RateLimit.Source != ratelimit.SourceOAuthUsageAPI {
		return d
	}
	polling := time.Duration(o.cfg.Polling.IntervalMs) * time.Millisecond
	if d <= polling {
		d = 2 * polling
	}
	if o.quota.apiLastOK {
		if w := o.apiPollInterval() + polling; w > d {
			d = w
		}
	}
	return d
}

// apiPollInterval は rate_limit.poll_interval_ms を返す（usage API を読む間隔）。
func (o *Orchestrator) apiPollInterval() time.Duration {
	d := time.Duration(o.cfg.RateLimit.PollIntervalMs) * time.Millisecond
	if d <= 0 {
		d = 5 * time.Minute
	}
	return d
}

// apiResetsAtSnap は、usage API の resets_at を保管値の期間と同じものとして扱う幅である。
//
// **usage API の resets_at は区切りの前後1秒以内で揺れる**（2026-09-28 の実測。同じ期間が
// `18:59:59.662Z` と `19:00:00.362Z` で返り、ステータスラインは `19:00:00Z` だった）。
// 揺れたまま入れると「resets_at が違えば置き換え」に当たり、同じ期間の値が下がる。
const apiResetsAtSnap = time.Minute

// OnAPISnapshot は usage API の読めた値を保管値へ入れる（issue #284）。
//
// **ステータスラインの新しい応答の行とまったく同じ規則で入れる**（applyNewResponse）。
// 期限の過ぎた期間を消してから、応答に在る期間ごとに、resets_at が同じなら大きいほう、違えば
// 置き換える。応答に無い期間は触らない。新しさの時刻を進め、1度でも読めた印を立てる。
//
//   - resets_at は最も近い分へ丸め、保管値の同じ期間との差が1分以内なら保管値の resets_at を採る
//   - resets_at が null の期間は、使用率が 100 なら入れない（枠待ちの明ける時刻にされないため）。
//     100 未満なら、期限を「次に試してよい時刻 + polling.interval_ms」として入れる（次の読み取りで
//     置き換わり、読めなければ期限が過ぎて見えなくなる）
//   - 同じ種別が複数あれば、使用率が最大のもの（同じなら resets_at の遅いもの）を採る
//   - **weekly_scoped は usage API しか運ばないので、応答に無ければ保管値から消す**（アカウントを
//     替えたときなどに古い値が居座らないため）。session と weekly_all は応答に無くても触らない
//
// 錠の取り方は OnStatusline と同じである（quotaMu の下で入れ、放してから persistQuota）。
//
// snap: usage API が返した枠の一覧。nil なら何もしない。
func (o *Orchestrator) OnAPISnapshot(snap *ratelimit.Snapshot) {
	if snap == nil {
		return
	}
	now := o.now()
	polling := time.Duration(o.cfg.Polling.IntervalMs) * time.Millisecond

	o.quotaMu.Lock()
	qs := &o.quota
	nullExpiry := now.Add(o.apiPollInterval() + polling)
	incoming, scopedSeen := windowsOfAPI(snap.Limits, qs.windows, now, nullExpiry)
	changed := dropExpiredWindows(qs.windows, now)
	if applyNewResponse(qs.windows, incoming) {
		changed = true
	}
	if !scopedSeen {
		if _, ok := qs.windows[handoff.LimitKindWeeklyScoped]; ok {
			delete(qs.windows, handoff.LimitKindWeeklyScoped)
			changed = true
		}
	}
	qs.freshAt = now
	qs.everRead = true
	qs.fetchStopped = false
	o.quotaMu.Unlock()

	if changed {
		o.persistQuota()
	}
}

// windowsOfAPI は usage API の枠の一覧を保管値の形にする（OnAPISnapshot の規則）。
//
// limits: usage API が返した枠の一覧。
// stored: いまの保管値（resets_at を寄せる相手。書き換えない）。
// now: 今の時刻。
// nullExpiry: resets_at が null の期間に付ける期限。
// 戻り値の1つ目: 期間ごとの値。
// 戻り値の2つ目: 応答に weekly_scoped が1件でもあったか。
func windowsOfAPI(
	limits []ratelimit.Limit, stored map[string]quotaWindow, now, nullExpiry time.Time,
) (map[string]quotaWindow, bool) {
	best := map[string]ratelimit.Limit{}
	scopedSeen := false
	for _, l := range limits {
		switch l.Kind {
		case handoff.LimitKindSession, handoff.LimitKindWeeklyAll:
		case handoff.LimitKindWeeklyScoped:
			scopedSeen = true
		default:
			continue
		}
		cur, ok := best[l.Kind]
		if !ok || l.Percent > cur.Percent || (l.Percent == cur.Percent && resetsLater(l.ResetsAt, cur.ResetsAt)) {
			best[l.Kind] = l
		}
	}
	out := map[string]quotaWindow{}
	for kind, l := range best {
		var resetsAt time.Time
		if l.ResetsAt == nil {
			if l.Percent >= 100 {
				continue
			}
			resetsAt = nullExpiry
		} else {
			resetsAt = l.ResetsAt.Round(time.Minute)
			if cur, ok := stored[kind]; ok {
				diff := cur.ResetsAt.Sub(resetsAt)
				if diff < 0 {
					diff = -diff
				}
				if diff <= apiResetsAtSnap {
					resetsAt = cur.ResetsAt
				}
			}
		}
		if qw, ok := shapeWindow(float64(l.Percent), resetsAt, now); ok {
			qw.Standin = l.ResetsAt == nil
			out[kind] = qw
		}
	}
	return out, scopedSeen
}

// resetsLater は、a が b より遅い resets_at かを返す（null は最も早いものとして扱う）。
func resetsLater(a, b *time.Time) bool {
	switch {
	case a == nil:
		return false
	case b == nil:
		return true
	default:
		return a.After(*b)
	}
}

// quotaForBid は、入札の判定に使ってよい枠の写しを返す（設計 3-77i）。
//
// **値が新しければ保管値の全部の写しを返す。それ以外は nil**（`handoff.Evaluate` は nil を
// 「読めない」と読み、入札そのものを取りやめる。古い値で入札させない）。
// **最後の応答の行に無かった期間も、期限内の保管値なら渡す**（渡さないと入札は0と読む）。
// **古さは期間ごとではなく新しさの時刻で判定する**（期間ごとにすると、その期間を返さない
// アカウントで入札が最長7日止まる）。**期限は読む時点の時計で見る**（行が1つも届かない暇な
// 機械でも、期限が過ぎた時点で入札を止めるため）。
//
// 戻り値: 枠の状態。新しくなければ nil。
func (o *Orchestrator) quotaForBid() *ratelimit.Snapshot {
	now := o.now()
	o.quotaMu.Lock()
	defer o.quotaMu.Unlock()
	if !o.quotaFreshLocked(now) {
		return nil
	}
	return snapshotOf(o.quota.windows, now, now)
}

// quotaSnapshot は、回復待ちと閾値が読む枠の状態を返す（設計 3-77i の「止めるのは入札だけ」）。
//
// **読む時点の時計で、保管値から resets_at を過ぎた期間を除いて返す。新しさは問わない。**
// 同じ期間の中で値は下がらないので、古くても上限と閾値の判定に使える。
//
// 戻り値: 枠の状態。値が無ければ nil。
func (o *Orchestrator) quotaSnapshot() *ratelimit.Snapshot {
	now := o.now()
	o.quotaMu.Lock()
	defer o.quotaMu.Unlock()
	return snapshotOf(o.quota.windows, now, o.quota.freshAt)
}

// quotaForPoll は、巡回が使う2つの写しを1回のロックで返す（設計 3-27。issue #173。
// 実装レビュー6周目の MEDIUM）。
//
// **2回に分けて読んではならない。**`OnStatusline` は statusline の受け口の goroutine から
// 同じ mutex を取って保管値を差し替えるので、**2回のあいだに差し替わると、
// 片方が「余裕が無い」と控えた時刻を、もう片方が「余裕がある」で消しうる。**
// **消えると、リセット時刻を読めない枠で上限を測る唯一の道（経過時間）が閉じる。**
//
// **`checkStalls` はこれを1回だけ呼ぶ。**`quotaSnapshot` と `quotaForBid` を続けて
// 呼ぶ形へ戻してはならない。
//
// 戻り値の1つ目: **新しさを問わない写し。**回復待ちと閾値の判定が読む（設計 3-77i）。
// 戻り値の2つ目: **新しさを問う写し。**手放しと、その起点の記録が読む（設計 3-27）。
// **新しくなければ nil。**
func (o *Orchestrator) quotaForPoll() (*ratelimit.Snapshot, *ratelimit.Snapshot) {
	now := o.now()
	o.quotaMu.Lock()
	defer o.quotaMu.Unlock()
	stale := snapshotOf(o.quota.windows, now, o.quota.freshAt)
	if !o.quotaFreshLocked(now) {
		return stale, nil
	}
	return stale, snapshotOf(o.quota.windows, now, now)
}

// snapshotOf は保管値から、期限内の期間だけの写しを作る。
//
// 戻り値: 写し。期限内の期間が無ければ nil。
func snapshotOf(windows map[string]quotaWindow, now, fetchedAt time.Time) *ratelimit.Snapshot {
	kinds := make([]string, 0, len(windows))
	for kind, w := range windows {
		if w.ResetsAt.After(now) {
			kinds = append(kinds, kind)
		}
	}
	if len(kinds) == 0 {
		return nil
	}
	sort.Strings(kinds)
	snap := &ratelimit.Snapshot{FetchedAt: fetchedAt}
	for _, kind := range kinds {
		w := windows[kind]
		lim := ratelimit.Limit{Kind: kind, Percent: w.Percent}
		// **仮の期限は写しへ出さない**（issue #197）。
		//
		// **usage API が `resets_at: null` を返した期間には、continuo が仮の期限を付ける**
		// （`OnAPISnapshot` の `nullExpiry`）。**あれは「次の読み取りまで見える」ことを表すための
		// 内側の値であって、「いつ明けるか」ではない。**
		//
		// **写しへ本物の期限として出すと、1週間の枠を待つ上限が1度も効かなくなる。**
		// `weeklyWaitExceededWith` は「リセット時刻 − 現在時刻 > 上限」で手放すかを決めるので、
		// **30秒ほど先の仮の期限を渡されると「待てばすぐ明ける」と読み、永久に手放さない。**
		// **`resets_at` を読めない機械で上限を測る唯一の道は経過であり、その道が閉じる。**
		//
		// **nil にすれば、期限を見る判定は揃って「この枠は判定から外す」へ倒れる**
		// （設計 3-27 の「`resets_at` が null の枠は判定から外す」）。
		// **写しに載せること自体は続ける。**使用率は本物なので、閾値と回復待ちの判定には要る。
		if !w.Standin {
			resetsAt := w.ResetsAt
			lim.ResetsAt = &resetsAt
		}
		snap.Limits = append(snap.Limits, lim)
	}
	return snap
}

// quotaFileWindow は quota.json の1つの期間である。
type quotaFileWindow struct {
	// Percent は使用率である。
	Percent int `json:"percent"`
	// ResetsAt は期間が切り替わる時刻である。
	ResetsAt time.Time `json:"resets_at"`
}

// persistQuota は保管値を quota.json へ書く（一時ファイルへ書いてから差し替える）。
//
// **書き込み用の錠を取ってから、o.quotaMu の中で写しを取り、外で書く。**2つの行がほぼ同時に
// 届いても、最後に書かれるのが最新の写しになる。**書けないときは WARN を出して動き続ける。**
// **新しさの時刻とセッションごとの記録は置かない**（立て直したあとは値が古いものとして扱い、
// usage API か statusline取得で取り直す）。
func (o *Orchestrator) persistQuota() {
	if o.quotaPath == "" {
		return
	}
	o.quotaWriteMu.Lock()
	defer o.quotaWriteMu.Unlock()
	o.quotaMu.Lock()
	out := make(map[string]quotaFileWindow, len(o.quota.windows))
	for kind, w := range o.quota.windows {
		// **仮の期限の期間は書かない。**読み戻すと本物の期限として扱われ、切れた時点で新しさの判定を
		// 止めてしまう。usage API の次の読み取りでまた入る。
		if w.Standin {
			continue
		}
		out[kind] = quotaFileWindow{Percent: w.Percent, ResetsAt: w.ResetsAt}
	}
	o.quotaMu.Unlock()
	data, err := json.Marshal(out)
	if err != nil {
		o.logger.Warn("使用率の保管値を JSON にできませんでした", "error", err)
		return
	}
	if err := atomicfile.Write(o.quotaPath, append(data, '\n'), quotaFilePerm); err != nil {
		o.logger.Warn("使用率の保管値を quota.json へ書けませんでした（動き続けます）",
			"path", o.quotaPath, "error", err)
	}
}

// loadQuota は quota.json を読んで保管値に戻す（起動時。sl.sock の受け付けより前に呼ぶ）。
//
// **「値の形」の規則を当て直す**（resets_at の過ぎた期間は捨て、100 を超えた値は 100 にする）。
// **読めない・形が違うときは WARN を出して捨てる。**新しさの時刻は戻さない。
//
// 上限の最中に立て直しても、回復待ちの判定が効くようにするためである（issue の「決めること」7）。
func (o *Orchestrator) loadQuota() {
	if o.quotaPath == "" {
		return
	}
	data, err := os.ReadFile(o.quotaPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			o.logger.Warn("quota.json を読めないので捨てます", "path", o.quotaPath, "error", err)
		}
		return
	}
	var in map[string]quotaFileWindow
	if err := json.Unmarshal(data, &in); err != nil {
		o.logger.Warn("quota.json の形が違うので捨てます", "path", o.quotaPath, "error", err)
		return
	}
	now := o.now()
	o.quotaMu.Lock()
	// **weekly_scoped は source: oauth_usage_api のときだけ戻す**（usage API しか運ばないので、
	// statusline では取り直す手段が無く、古い値が期限まで居座る）。
	scoped := o.cfg.RateLimit.Source == ratelimit.SourceOAuthUsageAPI
	for kind, w := range in {
		switch {
		case kind == handoff.LimitKindSession, kind == handoff.LimitKindWeeklyAll:
		case kind == handoff.LimitKindWeeklyScoped && scoped:
		default:
			continue
		}
		if qw, ok := shapeWindow(float64(w.Percent), w.ResetsAt, now); ok {
			o.quota.windows[kind] = qw
		}
	}
	o.quotaMu.Unlock()
}
