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
// **値の出どころはステータスラインである。**continuo が起動する Claude Code（issue の pane と
// statusline取得の pane）のステータスラインが運ぶ `rate_limits` を、`continuo statusline` が
// `sl.sock` へ送り、OnStatusline が受ける。期間（5時間と7日）ごとに値を1つだけ保管する。
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
	// handoff.LimitKindWeeklyAll（seven_day）。quota.json にも置く。
	windows map[string]quotaWindow
	// freshAt は新しさの時刻（rate_limits を持つ新しい応答の行を最後に受けた時刻）である。
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
// **新しさの時刻があり、そこから refresh_interval_ms を過ぎておらず、保管値のどの期間も
// resets_at を過ぎていないこと。**
func (o *Orchestrator) quotaFreshLocked(now time.Time) bool {
	qs := &o.quota
	if qs.freshAt.IsZero() || len(qs.windows) == 0 {
		return false
	}
	if now.Sub(qs.freshAt) >= o.quotaRefreshInterval() {
		return false
	}
	for _, w := range qs.windows {
		if !w.ResetsAt.After(now) {
			return false
		}
	}
	return true
}

// quotaRefreshInterval は rate_limit.refresh_interval_ms を返す。
func (o *Orchestrator) quotaRefreshInterval() time.Duration {
	d := time.Duration(o.cfg.RateLimit.RefreshIntervalMs) * time.Millisecond
	if d <= 0 {
		d = 5 * time.Minute
	}
	return d
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
		resetsAt := w.ResetsAt
		snap.Limits = append(snap.Limits, ratelimit.Limit{Kind: kind, Percent: w.Percent, ResetsAt: &resetsAt})
	}
	return snap
}

// quotaAtFullNow は、期限内の保管値に 100 の期間があるかを返す（statusline取得を開かない条件）。
//
// **上限の最中は断られるだけで値は変わらない**ので、statusline取得を開かない。その期間の
// resets_at を過ぎると保管値から消え、開く。
func (o *Orchestrator) quotaAtFullNow() bool {
	return o.quotaSnapshot().AtFullPercent()
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
// statusline取得で取り直す）。
func (o *Orchestrator) persistQuota() {
	if o.quotaPath == "" {
		return
	}
	o.quotaWriteMu.Lock()
	defer o.quotaWriteMu.Unlock()
	o.quotaMu.Lock()
	out := make(map[string]quotaFileWindow, len(o.quota.windows))
	for kind, w := range o.quota.windows {
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
	for kind, w := range in {
		if kind != handoff.LimitKindSession && kind != handoff.LimitKindWeeklyAll {
			continue
		}
		if qw, ok := shapeWindow(float64(w.Percent), w.ResetsAt, now); ok {
			o.quota.windows[kind] = qw
		}
	}
	o.quotaMu.Unlock()
}
