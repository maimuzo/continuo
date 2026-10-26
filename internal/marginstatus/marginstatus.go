// Package marginstatus は、continuo 本体が書く `margin-status`（レートリミットのマージンを切ったかを
// 1行で書いたファイル）の置き場所と中身の形を決める（issue #262。設計 3-27a）。
//
// **置き場所を決める関数を1つだけ持つ。**書く側（`internal/orchestrator` の巡回）と、システムプロンプトへ
// パスを入れる側（`internal/prompt` の `RenderData`）が、どちらもこれを呼ぶ。**2か所で別々に組み立てると、
// 指示書が指すパスと、本体が書くパスがずれる。**`internal/prompt` は `internal/orchestrator` を
// import できないので、下の package に置く。
//
// **判定はしない。**どの期間がマージンを切ったかは、`internal/handoff` の `MarginCut` が決める。
// ここは、決まった結果を1行の文字列にするだけである。
package marginstatus

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// FileName は、実行時ディレクトリの下に置くファイルの名前である。
const FileName = "margin-status"

// Path は、`margin-status` を書くかどうかと、そのパスを返す。
//
// **書くのは、`quota.json` と同じく、`rate_limit.source` が `none` 以外で、hook を受ける socket の
// パスが空でないときだけである**（`internal/orchestrator` の `quotaPath` と同じ条件）。
// **パスは、その socket と同じディレクトリ（実行時ディレクトリ）の `margin-status` である。**
// 同じ機械で continuo を2つ動かすときは、socket と同じ規則で実行時ディレクトリが分かれるので、
// 互いのファイルを上書きしない。
//
// rateLimit: 設定の `rate_limit`。
// hookSocketPath: hook を受ける socket の絶対パス。空なら書かない。
// 戻り値: 書くならそのパス。書かないなら空文字。
func Path(rateLimit config.RateLimitConfig, hookSocketPath string) string {
	if rateLimit.Source == ratelimit.SourceNone || hookSocketPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(hookSocketPath), FileName)
}

// ShellQuote は、パスを単引用符で囲んだシェルの1語にするときの中身を返す（囲みの引用符は含めない）。
//
// **スキル「reporting-progress」の「レートリミットのマージンを切ったら、切り上げる」は `cat '<パス>'` の形で叩かせる。**パスに単引用符が入っていると、
// そこで引用が切れて `cat` が壊れる。**中の単引用符を `'\”`（引用を閉じ、引用符を1つ置き、開き直す）
// に置き換える。**
//
// path: パス。
// 戻り値: 単引用符の中へそのまま置ける文字列。
func ShellQuote(path string) string {
	return strings.ReplaceAll(path, "'", `'\''`)
}

// Period は、`margin-status` の1行に載せる、マージンを切った期間の1つである。
type Period struct {
	// Kind は期間の種別である（`session` / `weekly_all` / `weekly_scoped`）。
	Kind string
	// Used は使用率（%）である。
	Used int
	// Margin はその期間のマージン（%）である。
	Margin int
	// ResetsAt はその期間がリセットされる時刻である。nil なら読めない。
	ResetsAt *time.Time
}

// State は、`margin-status` の1行の状態である。
type State int

const (
	// StateUnknown は、判定に使える値が1つも無いことを表す（`MARGIN: unknown`）。
	StateUnknown State = iota
	// StateOK は、マージンを切った期間が無いことを表す（`MARGIN: ok`）。
	StateOK
	// StateCut は、マージンを切った期間が1つ以上あることを表す（`MARGIN: cut`）。
	StateCut
)

// Format は、`margin-status` の1行を組み立てる（改行を含む）。
//
// **形の例（切っているとき）。**
//
//	MARGIN: cut kind=session used=93 margin=10 resets_at=2026-10-07T14:00:00+09:00 expires_at=1791336000
//
// **2つ以上の期間が切っているときは、期間ごとに `kind=…` から `resets_at=…` までを並べる。**
// **`expires_at` は最後に1つだけ置き、UNIX 時刻の秒で書く。**Claude Code は `expires_at=` を `date +%s` の出力と
// 数として比べ、過ぎていれば信じない（continuo が止まって書き直されなくなった古い1行で、切り上げ続けないため）。
// **`date -u` と比べさせてはならない**（issue #258）。システムプロンプトと手順のスキルは `date -u` を使わせない決まりで、
// UTC の時刻を見たエージェントは、それを issue のコメントへ写す。
// **`resets_at` は機械のタイムゾーンで書く**（continuo 自身のコメントと同じ。issue #258）。
//
// state: 状態。
// periods: マージンを切った期間（state が StateCut のときだけ使う）。
// expiresAt: この1行を信じてよい期限。
// 戻り値: ファイルへ書く1行。
func Format(state State, periods []Period, expiresAt time.Time) string {
	var b strings.Builder
	switch state {
	case StateCut:
		b.WriteString("MARGIN: cut")
		for _, p := range periods {
			resets := "unknown"
			if p.ResetsAt != nil {
				resets = p.ResetsAt.Local().Format(time.RFC3339)
			}
			fmt.Fprintf(&b, " kind=%s used=%d margin=%d resets_at=%s", p.Kind, p.Used, p.Margin, resets)
		}
	case StateOK:
		b.WriteString("MARGIN: ok")
	default:
		b.WriteString("MARGIN: unknown")
	}
	fmt.Fprintf(&b, " expires_at=%d\n", expiresAt.Unix())
	return b.String()
}

// ExpiresAt は、`margin-status` の1行を信じてよい期限を決める。
//
// **書いた時刻に「巡回の間隔の3倍」と「10分」の長いほうを足す。**巡回のたびに書き直すので、
// 数回書き直しが飛んでも古くならない長さにする。**ただし、切っている期間のリセットの時刻のうち、
// いちばん早いものより後にはしない**（リセットの時刻を読めない期間は数えない）。リセットのあとまで
// 「切った」を信じさせないためである。
//
// writtenAt: この1行を書く時刻。
// pollInterval: 巡回の間隔。
// periods: マージンを切った期間。
// 戻り値: 期限。
func ExpiresAt(writtenAt time.Time, pollInterval time.Duration, periods []Period) time.Time {
	width := 3 * pollInterval
	if width < 10*time.Minute {
		width = 10 * time.Minute
	}
	expires := writtenAt.Add(width)
	for _, p := range periods {
		if p.ResetsAt != nil && p.ResetsAt.Before(expires) {
			expires = *p.ResetsAt
		}
	}
	return expires
}
