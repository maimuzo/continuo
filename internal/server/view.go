package server

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/orchestrator"
)

// Snapshot はダッシュボードが1回の要求で返す写しである。
//
// **`orchestrator.RunView` をそのまま出さない。**表示と JSON の両方に使う形へ
// 組み替え、時刻の書き方と合計をここで確定させる。
type Snapshot struct {
	// GeneratedAt はこの写しを作った時刻である。
	GeneratedAt time.Time `json:"generated_at"`
	// Counts は run の件数の内訳である（`SPEC.md` 13.7.2 の `counts`）。
	Counts Counts `json:"counts"`
	// Runs は実行中の run である。**identifier で昇順に並べてある**
	// （`RunViews` の順序は不定なので、そのまま出すと表示が毎回入れ替わる）。
	//
	// **仕様の例が `running` と `retrying` の2本の配列に分けているのに対し、
	// continuo は1本で出す。**印の集合が1本しか無く（設計 3-25）、バックオフ中の run も
	// 同じ集合に残るためである。どちらであるかは `backoff_until` で分かる。
	Runs []Run `json:"runs"`
	// Gated は着手の関門で止めた issue である（issue #134）。
	// **Since の古い順、同じなら Identifier の昇順に並べてある。**
	// `GateViews` の順序は不定で、`sort.Slice` は安定ではない。
	Gated []Gated `json:"gated"`
	// Totals は Runs のトークンの総和である。
	//
	// **いま走っている run の分しか入っていない。**終わった run は印から外れて消えるので、
	// **run をまたぐ合計が要るなら CumulativeTotals を見ること**（issue #238）。
	Totals Tokens `json:"totals"`
	// CumulativeTotals は run をまたぐトークンの累計である（issue #238）。
	//
	// **この continuo が起動してから、turn の終わりに読み取った transcript の合計である。**
	// **引き継いだ run では、起動より前に書かれた分も含む**（continuo が再起動しても
	// pane の Claude Code は生きたままなので、その transcript には前の分が残っている）。
	// **走行中の turn の分はまだ入っていない**（集計は turn の終わりにしか走らない）。
	// **メモリだけに持つので、continuo を再起動すると0へ戻る。**
	//
	// **「continuo を起動してから」でも「continuo の一生ぶん」でもない、中間の値である。**
	// **鍵の名前を仕様（`SPEC.md` 13.3）の `codex_totals` にしない。**continuo は codex を
	// 使わない。応答の形が仕様の例と違うことは docs/plans/impl/09_dashboard.md が記録している。
	//
	// **Totals より小さくなることは無い。**保証しているのは2つである。
	// `addTokenUsage` が累計と run ごとの値を1つの錠の区間で書くことと、
	// `Server.snapshot` が run の写しを先に、累計を後に読むことである。
	CumulativeTotals Tokens `json:"cumulative_totals"`
}

// Gated は着手の関門で止めた issue 1件の表示用の写しである（issue #134）。
type Gated struct {
	// Identifier は issue の識別子である。
	Identifier string `json:"identifier"`
	// Title は issue の題名である。**HTML へ出すときは必ずエスケープすること。**
	Title string `json:"title"`
	// URL は issue の URL である。空なら画面はリンクにしない。
	URL string `json:"url"`
	// Reason は止めた理由の種類である（機械が読むための値。`"human_assigned"` など）。
	Reason string `json:"reason"`
	// ReasonText は理由の1行である（資源から引いた文言）。
	ReasonText string `json:"reason_text"`
	// Remedy は直し方の1行である（資源から引いた文言）。
	Remedy string `json:"remedy"`
	// Assignees は担当者のログイン名を `, ` で繋いだものである。
	Assignees string `json:"assignees"`
	// Since は最初にこの理由で止めた時刻である。
	Since time.Time `json:"since"`
	// SinceAgo は Since からの経過を人間向けに書いたものである。
	SinceAgo string `json:"since_ago"`
	// Noticed は issue へ案内を書き終えているかである。
	Noticed bool `json:"noticed"`
	// NoticeSkip は案内を書かないと決めた理由である（機械が読むための値。`"off_by_config"` など）。
	NoticeSkip string `json:"notice_skip"`
	// NoticeFailed は案内の投稿に失敗したことを表す。**Noticed も同時に真になる。**
	NoticeFailed bool `json:"notice_failed"`
	// NoticeBadge は行に添える印の文言である（資源から引いた文言）。**空なら印を出さない。**
	// **テンプレートで分岐させない。**どの印を出すかはこのファイルが1箇所で決める。
	NoticeBadge string `json:"notice_badge"`
}

// gateReasonKeys は、止めた理由をダッシュボードの文言のキーへ写す表である（issue #134）。
//
// **キーを文字列から組み立てない。**組み立てると internal/i18n の `allKeys` に載らない
// キーが生まれ、日英の突き合わせの検査を素通りする。**表なら、キーは全部ここに書いてある。**
var gateReasonKeys = map[orchestrator.GateReason]struct{ Reason, Remedy i18n.Key }{
	orchestrator.GateReasonHumanAssigned: {
		Reason: i18n.KeyDashboardGateReasonHumanAssigned,
		Remedy: i18n.KeyDashboardGateRemedyHumanAssigned,
	},
	orchestrator.GateReasonManyAssignees: {
		Reason: i18n.KeyDashboardGateReasonManyAssignees,
		Remedy: i18n.KeyDashboardGateRemedyManyAssignees,
	},
	orchestrator.GateReasonManyAssigneesWithSelf: {
		Reason: i18n.KeyDashboardGateReasonManyAssigneesWithSelf,
		Remedy: i18n.KeyDashboardGateRemedyManyAssigneesWithSelf,
	},
}

// Counts は run の件数の内訳である。
type Counts struct {
	// Running は次の turn を送れる状態の run の件数である。
	Running int `json:"running"`
	// Retrying はバックオフ中（再 dispatch を待っている）run の件数である。
	Retrying int `json:"retrying"`
}

// Run は実行中の run 1件の表示用の写しである。
type Run struct {
	// Identifier は issue の識別子（`<owner>/<repo>#<番号>`）である。
	Identifier string `json:"identifier"`
	// Title は issue のタイトルである。
	// **外部から来る文字列である。**HTML へ出すときは必ずエスケープすること。
	Title string `json:"title"`
	// URL は issue の URL である。draft issue は持たないので空文字になる。
	URL string `json:"url"`
	// State はボード上の Status である。
	State string `json:"state"`
	// TurnCount は continuo が送った turn の回数である（設計 3-14）。
	TurnCount int `json:"turn_count"`
	// RetryCount は stall や起動失敗で積んだリトライの回数である。
	RetryCount int `json:"retry_count"`
	// WaitingQuota は枠待ちで時計を止めていることを表す（設計 3-27）。
	WaitingQuota bool `json:"waiting_quota"`
	// LastHookAt は最後に hook を実際に受けた時刻である。まだ1件も受けていなければ nil。
	// **stall の時計（StallClockAt）とは別である。**
	LastHookAt *time.Time `json:"last_hook_at"`
	// LastHookAgo は LastHookAt からの経過を人間向けに書いたものである。
	LastHookAgo string `json:"last_hook_ago"`
	// StallClockAt は stall の判定に使っている時計である（設計 3-21）。
	// **hook 以外でも進む**（turn を送った・枠待ちを外した・猶予を与えた）ので、
	// **「エージェントが生きているか」の判断には使えない。**運用の調査のために出す。
	StallClockAt *time.Time `json:"stall_clock_at"`
	// StartedAt はこの run が最初の turn を送った時刻である。まだなら nil。
	StartedAt *time.Time `json:"started_at"`
	// BackoffUntil はこの時刻まで再 dispatch しない、という時刻である。待っていなければ nil。
	BackoffUntil *time.Time `json:"backoff_until"`
	// Tokens はこの run のトークンの累計である（設計 3-15）。
	Tokens Tokens `json:"tokens"`
	// TokensAt は Tokens を集計した時刻である。一度も集計していなければ nil。
	// **turn の終わりにしか更新されない**ので、いま走っている turn の分は入っていない。
	TokensAt *time.Time `json:"tokens_at"`
}

// Tokens は transcript から集計したトークンである（設計 3-15）。
//
// **`requestId` で重複排除したあとの値である。**重複排除は
// `orchestrator.ReadTranscript` が transcript を読む時点で済ませている
// （assistant の行が API 呼び出しと1対1である保証が無いため）。
type Tokens struct {
	// APICalls は数えた API 応答の件数である。
	APICalls int `json:"api_calls"`
	// Input は入力のトークンの合計である。
	Input int `json:"input"`
	// CacheCreation はキャッシュ作成のトークンの合計である。
	CacheCreation int `json:"cache_creation"`
	// CacheRead はキャッシュ読み出しのトークンの合計である。
	CacheRead int `json:"cache_read"`
	// Output は出力のトークンの合計である。
	Output int `json:"output"`
	// Total は上の4つの総和である（表示の都合でここで足しておく）。
	Total int `json:"total"`
}

// NewSnapshot は run の写しを、表示用のスナップショットへ組み替える。
//
// **`RunViews` の順序は不定なので identifier で昇順に並べ替える。**並べ替えないと、
// 再読み込みのたびに行が入れ替わって読めない。
//
// views: `orchestrator.RunViews` が返した写し。
// gates: `orchestrator.GateViews` が返した写し（issue #134）。
// cumulative: `orchestrator.TokenTotals` が返した run をまたぐ累計（issue #238）。
// **`views` より後に取ったものを渡すこと**（`Server.snapshot` の理由を見よ）。
// now: いまの時刻（経過の計算に使う）。
// 戻り値: 表示用のスナップショット。
func NewSnapshot(
	views []orchestrator.RunView,
	gates []orchestrator.GateView,
	cumulative orchestrator.TokenUsage,
	now time.Time,
) Snapshot {
	runs := make([]Run, 0, len(views))
	var totals Tokens
	var counts Counts
	for _, v := range views {
		tokens := newTokens(v.Tokens)
		runs = append(runs, Run{
			Identifier:   v.Identifier,
			Title:        v.Title,
			URL:          v.URL,
			State:        v.State,
			TurnCount:    v.TurnCount,
			RetryCount:   v.RetryCount,
			WaitingQuota: v.WaitingQuota,
			LastHookAt:   optionalTime(v.LastHookAt),
			LastHookAgo:  humanizeSince(v.LastHookAt, now),
			StallClockAt: optionalTime(v.StallClockAt),
			StartedAt:    optionalTime(v.StartedAt),
			BackoffUntil: optionalTime(v.BackoffUntil),
			Tokens:       tokens,
			TokensAt:     optionalTime(v.TokensAt),
		})
		if v.BackoffUntil.After(now) {
			counts.Retrying++
		} else {
			counts.Running++
		}
		totals.APICalls += tokens.APICalls
		totals.Input += tokens.Input
		totals.CacheCreation += tokens.CacheCreation
		totals.CacheRead += tokens.CacheRead
		totals.Output += tokens.Output
		totals.Total += tokens.Total
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].Identifier < runs[j].Identifier })

	gated := make([]Gated, 0, len(gates))
	for _, g := range gates {
		gated = append(gated, newGated(g, now))
	}
	// **鍵を Since 1本にしない。**同じ巡回で2件以上が同時に止まると値が同じになりうる。
	// `sort.Slice` は安定ではないので、同値のままだと10秒ごとの再読み込みで行が入れ替わる。
	// **Identifier は `<owner>/<repo>#<番号>` で、記録の鍵1件につき1つなので重複しない。**
	sort.Slice(gated, func(i, j int) bool {
		if !gated[i].Since.Equal(gated[j].Since) {
			return gated[i].Since.Before(gated[j].Since)
		}
		return gated[i].Identifier < gated[j].Identifier
	})

	return Snapshot{
		GeneratedAt:      now,
		Counts:           counts,
		Runs:             runs,
		Gated:            gated,
		Totals:           totals,
		CumulativeTotals: newTokens(cumulative),
	}
}

// newGated は、着手の関門で止めた issue の写しを表示用の形へ組み替える（issue #134）。
//
// **表に無い理由が来たら、i18n を1度も引かない。**`Reason` の文字列をそのまま
// `ReasonText` へ入れ、`Remedy` は空にする。**`dashboard.none` にも落とさない。**
// 落とすと `—` になり、**知らない理由が来たことが画面から読めなくなる。**
//
// v: `orchestrator.GateViews` が返した写しの1件。
// now: いまの時刻（経過の計算に使う）。
// 戻り値: 表示用の1行。
func newGated(v orchestrator.GateView, now time.Time) Gated {
	assignees := strings.Join(v.Assignees, ", ")
	reasonText := string(v.Reason)
	remedy := ""
	if keys, ok := gateReasonKeys[v.Reason]; ok {
		reasonText = i18n.T(keys.Reason, assignees)
		remedy = i18n.T(keys.Remedy)
	}
	return Gated{
		Identifier:   v.Identifier,
		Title:        v.Title,
		URL:          v.URL,
		Reason:       string(v.Reason),
		ReasonText:   reasonText,
		Remedy:       remedy,
		Assignees:    assignees,
		Since:        v.Since,
		SinceAgo:     humanizeSince(v.Since, now),
		Noticed:      v.Noticed,
		NoticeSkip:   string(v.NoticeSkip),
		NoticeFailed: v.NoticeFailed,
		NoticeBadge:  gateNoticeBadge(v),
	}
}

// gateNoticeBadge は、行に添える印の文言を決める（issue #134）。
//
// **書き終えている行には印を出さない。**それが正常な状態だからである。
//
// v: `orchestrator.GateViews` が返した写しの1件。
// 戻り値: 印の文言。空なら印を出さない。
func gateNoticeBadge(v orchestrator.GateView) string {
	if v.NoticeFailed {
		// **`Noticed` より先に見る。**印は残したままなので（設計 8-2）、
		// 後に置くと「書き終えた」の側へ落ちて、issue に1件も無いことが画面から読めなくなる。
		return i18n.T(i18n.KeyDashboardBadgeNoticeFailed)
	}
	if v.Noticed {
		return ""
	}
	switch v.NoticeSkip {
	case "":
		return i18n.T(i18n.KeyDashboardBadgeNotNoticed)
	case orchestrator.GateNoticeOffByConfig:
		return i18n.T(i18n.KeyDashboardBadgeNoticeOff)
	case orchestrator.GateNoticeTooManyComments:
		return i18n.T(i18n.KeyDashboardBadgeNoticeCapped)
	case orchestrator.GateNoticeUnclearOwner:
		return i18n.T(i18n.KeyDashboardBadgeNoticeUnclearOwner)
	case orchestrator.GateNoticeNoBody:
		return i18n.T(i18n.KeyDashboardBadgeNoticeNoBody)
	default:
		// **知らない値を黙って落とさない。**機械が読む値をそのまま出して、
		// 印を足し忘れたことが画面から分かるようにする。
		return string(v.NoticeSkip)
	}
}

// newTokens は orchestrator の集計を表示用の形へ写す。
//
// usage: `orchestrator` が transcript から集計したトークン。
// 戻り値: 総和を足した表示用のトークン。
func newTokens(usage orchestrator.TokenUsage) Tokens {
	return Tokens{
		APICalls:      usage.APICalls,
		Input:         usage.Input,
		CacheCreation: usage.CacheCreation,
		CacheRead:     usage.CacheRead,
		Output:        usage.Output,
		Total:         usage.Input + usage.CacheCreation + usage.CacheRead + usage.Output,
	}
}

// optionalTime はゼロ値の時刻を nil にする。
//
// **JSON で `"0001-01-01T00:00:00Z"` を出さないためである。**「まだ無い」ことを
// 読む側が判別できるようにする。
//
// t: 対象の時刻。
// 戻り値: ゼロ値なら nil、そうでなければ t への参照。
func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// humanizeSince は時刻からの経過を人間向けに書く。
//
// **「最後に hook を受けてから何秒経ったか」を、人間が一目で判断できる形にする。**
// 絶対時刻だけだと、止まっているのかどうかを頭の中で引き算しないと分からない。
//
// t: 起点の時刻。ゼロ値なら「まだ受けていない」とみなす。
// now: いまの時刻。
// 戻り値: 「12秒前」「3分12秒前」「1時間5分前」など（文言は internal/i18n から引く。設計 3-35）。
// ゼロ値なら「まだ無い」ことを表す印。
func humanizeSince(t, now time.Time) string {
	if t.IsZero() {
		return i18n.T(i18n.KeyDashboardNone)
	}
	d := now.Sub(t)
	if d < 0 {
		// 時計が巻き戻った場合。負の秒数を見せない。
		d = 0
	}
	switch {
	case d < time.Minute:
		return i18n.T(i18n.KeyDashboardAgoSeconds, int(d.Seconds()))
	case d < time.Hour:
		return i18n.T(i18n.KeyDashboardAgoMinutes, int(d.Minutes()), int(d.Seconds())%60)
	default:
		return i18n.T(i18n.KeyDashboardAgoHours, int(d.Hours()), int(d.Minutes())%60)
	}
}

// formatTime は時刻を表示用の文字列にする。
//
// t: 対象の時刻。nil なら「まだ無い」とみなす。
// 戻り値: RFC3339 の文字列。nil なら「まだ無い」ことを表す印（設計 3-35）。
func formatTime(t *time.Time) string {
	if t == nil {
		return i18n.T(i18n.KeyDashboardNone)
	}
	return t.Format(time.RFC3339)
}

// formatSinceTime は「いつから止まっているか」を表示用の文字列にする（issue #134）。
//
// **`formatTime` と分けてある。**あちらは「まだ無い」を nil で表す run の時刻のためのもので、
// 着手の関門の記録の `Since` は必ず値を持つ（記録を作った巡回の時刻である）。
//
// t: 対象の時刻。ゼロ値なら「まだ無い」とみなす。
// 戻り値: RFC3339 の文字列。ゼロ値なら「まだ無い」ことを表す印（設計 3-35）。
func formatSinceTime(t time.Time) string {
	if t.IsZero() {
		return i18n.T(i18n.KeyDashboardNone)
	}
	return t.Format(time.RFC3339)
}

// formatInt は整数に3桁区切りを入れる。
//
// **トークン数は7桁になることがあり、区切りが無いと読めない。**
//
// n: 対象の整数。
// 戻り値: 「701,185」のような文字列。
func formatInt(n int) string {
	s := fmt.Sprintf("%d", n)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return sign + b.String()
}

// writeJSON はスナップショットを JSON で書き出す。
//
// w: 書き出し先。
// snap: 書き出すスナップショット。
// 戻り値: 書き出しに失敗した場合のエラー。
func writeJSON(w io.Writer, snap Snapshot) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// **`SetEscapeHTML` は既定の true のままにする。**JSON を HTML に貼られても
	// script が閉じないようにするためである。
	if err := enc.Encode(snap); err != nil {
		return i18n.Errorf(i18n.KeyServerWriteJSONEncodeFailed, err)
	}
	return nil
}
