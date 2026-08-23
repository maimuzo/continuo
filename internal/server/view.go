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
	// Totals は Runs のトークンの総和である。
	Totals Tokens `json:"totals"`
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
// now: いまの時刻（経過の計算に使う）。
// 戻り値: 表示用のスナップショット。
func NewSnapshot(views []orchestrator.RunView, now time.Time) Snapshot {
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
	return Snapshot{GeneratedAt: now, Counts: counts, Runs: runs, Totals: totals}
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
