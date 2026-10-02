// **同じカンバンを複数の機械で見張るときの、担当の決め方の検査である**（設計 3-77 / 3-77b / 3-77c）。
//
// **見張っているのは1点である。**「2台が同じ issue を掴まない」こと。
// そのために、担当者（assignee）と hold のコメントだけで判定が閉じているかを確かめる。
package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/tracker"
)

// rivalLogin は別の機械が使っている gh の持ち主である（架空の名前）。
const rivalLogin = "octocat-bot-b"

// testBidWindow は、別の機械の入札を組み立てるときに渡す締め切りまでの長さである。
//
// **判定には効かない。**この値は入札のコメントに書く「担当は約何分後に決まるか」の
// 1行にしか使われない（設計 3-77a）。締め切りそのものは、コメントに付いた作成時刻と
// 設定の `bid_window_ms` から continuo が数える。
const testBidWindow = 3 * time.Minute

// issueNode は sampleIssue が使う issue のノード ID を返す。
//
// number: issue 番号。
// 戻り値: 下敷きの GitHub issue のノード ID。
func issueNode(number int) string {
	return "I_node" + itoa(number)
}

// itoa は整数を10進の文字列にする（fmt を持ち込まずに済ませるため）。
//
// n: 変換する整数。
// 戻り値: 10進の文字列。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// assignedIssue は担当者の付いた issue を作る。
//
// number: issue 番号。
// state: Status の値。
// assignees: 担当者のログイン名。
// 戻り値: 担当者を持つ issue。
func assignedIssue(number int, state string, assignees ...string) tracker.Issue {
	issue := sampleIssue(number, state)
	for _, login := range assignees {
		issue.Assignees = append(issue.Assignees, tracker.Assignee{ID: "U_" + login, Login: login})
	}
	issue.AssigneeCount = len(issue.Assignees)
	return issue
}

// 目的: 人間が付けた担当で飛ばすとき、**WARN の水準で、直し方を添えて**知らせることを確かめる
// （issue #131）。
//
// **これがこの変更の成果物そのものである。**expectedWarnings への登録は「出てよい」を許すだけで
// 「出ること」を求めないので、Warn を Info へ戻しても、案内の1文を消しても、それだけでは
// どのテストも落ちない。**水準と文面の両方を、ここで固定する。**
//
// **文面を固定する理由。**[docs/FAQ.md](../../../docs/FAQ.md) が
// `grep '担当者が付いているので着手しません' <ログの出力先>` を唯一の手がかりとして公開している。
// 文面が変わると、その案内が空振りする。
//
// 与える情報: ほかの人が担当していて、hold のコメントが1件も無い issue。
// 成功条件: level=WARN の行に、飛ばした理由と直し方と担当者が載っていること。
func TestHandoff_人間が付けた担当はWARNで直し方つきで知らせる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", rivalLogin))
	fx.AllowLog("担当者が付いているので着手しません")

	fx.Orc.Tick(context.Background())

	var line string
	for _, l := range strings.Split(fx.Logs.String(), "\n") {
		if strings.Contains(l, "担当者が付いているので着手しません") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatal("人間が付けた担当で飛ばしたのに、その旨のログが1行も出ていない")
	}
	if !strings.Contains(line, "level=WARN") {
		t.Errorf("WARN で出ていない（INFO だと、ログを見ていても異常だと気づけない）: %s", line)
	}
	// **直し方が同じ行にあること。**別の行にあると、grep で拾った人に届かない。
	if !strings.Contains(line, "その担当者を外してください") {
		t.Errorf("直し方が同じ行に無い: %s", line)
	}
	// **誰が担当者かが分かること。**
	if !strings.Contains(line, rivalLogin) {
		t.Errorf("担当者が載っていない: %s", line)
	}
}

// ownBidsOf は、この機械が書いた入札のコメントだけを数える。
//
// **ほかのアカウントの入札と混ぜて数えない。**巡回のたびに増えるのはこの continuo の入札であり、
// **そこが増えないことが「次の回が始まった」の証拠である。**
//
// **投稿者で絞る**（設計 3-77-0）。入札の JSON には、自分で名乗る欄が無い。
//
// fx: 検査対象。
// node: 下敷きの GitHub issue のノード ID。
// 戻り値: この continuo が書いた入札のコメント。
func ownBidsOf(fx *fixture, node string) []tracker.Comment {
	out := make([]tracker.Comment, 0, 4)
	for _, c := range fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffBidMarker) {
		if strings.EqualFold(c.Author, testGHLogin) {
			out = append(out, c)
		}
	}
	return out
}
