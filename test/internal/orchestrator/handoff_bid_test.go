// {"RUCM-CFG-SHA256": "70c0cff8cba4eaa3f5fac65c98b23adc44a4a52a4332c5e24fae829f94c60af5", "SOURCE": "docs/spec/usecases/particular_case/issue の担当を入札で決める.cfg.json"}
//
// **同じボードを複数の機械で見張るときの、担当の決め方の検査である**（設計 3-77 / 3-77b / 3-77c）。
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
	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/tracker"
)

// rivalHost は入札を競う別の機械の名前である（架空の名前）。
const rivalHost = "thinkpad"

// rivalLogin は別の機械が使っている gh の持ち主である（架空の名前）。
const rivalLogin = "octocat-bot-b"

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

// {"RUCM-PATH": "P001"}
//
// TestHandoff_勝ったら担当者になり入札とholdを1件ずつ書く は、基本フローを確かめる。
//
// 目的: 設計 3-77 の「担当者がいなければ入札し、勝ったら自分を担当者に加えて hold を書く」。
// 与える情報: 担当者のいない `Ready` の issue 1件と、ほかの機械の入札は無い状態。
// 成功条件: 着手されること。入札のコメントと hold のコメントが1件ずつ増え、
// 担当者が gh の持ち主1人になっていること。
func TestHandoff_勝ったら担当者になり入札とholdを1件ずつ書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())

	waitFor(t, 5*time.Second, "dispatch される", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 1
	})

	node := issueNode(188)
	bids := fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffBidMarker)
	if len(bids) != 1 {
		t.Fatalf("入札のコメントが1件ではない: %d 件", len(bids))
	}
	if !strings.Contains(bids[0].Body, `"host":"`+testHostName+`"`) {
		t.Errorf("入札に機械の名前が入っていない:\n%s", bids[0].Body)
	}

	holds := fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffHoldMarker)
	if len(holds) != 1 {
		t.Fatalf("hold のコメントが1件ではない: %d 件", len(holds))
	}
	if !strings.Contains(holds[0].Body, `"branch":"continuo/octocat/hello-world/188"`) {
		t.Errorf("hold に branch の名前が入っていない:\n%s", holds[0].Body)
	}

	issue, ok := fx.Tracker.IssueByID("PVTI_item188")
	if !ok {
		t.Fatal("issue がボードから消えた")
	}
	if len(issue.Assignees) != 1 || issue.Assignees[0].Login != testGHLogin {
		t.Errorf("担当者が gh の持ち主1人になっていない: %+v", issue.Assignees)
	}
}

// TestHandoff_締め切りの前は担当者にならない は、締め切りの待ちを確かめる。
//
// 目的: 設計 3-77 の「締め切りは、入札が1件も無い issue への最初の投稿から bid_window_ms」。
// 与える情報: 締め切りを3分にした設定と、担当者のいない issue 1件。
// 成功条件: 入札のコメントは1件書かれるが、担当者は付かず、着手もしないこと。
func TestHandoff_締め切りの前は担当者にならない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.Provider.Handoff.BidWindowMs = 180000
		},
	})
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())

	node := issueNode(188)
	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffBidMarker)); got != 1 {
		t.Fatalf("入札のコメントが1件ではない: %d 件", got)
	}
	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffHoldMarker)); got != 0 {
		t.Errorf("締め切り前なのに hold を書いている: %d 件", got)
	}
	issue, _ := fx.Tracker.IssueByID("PVTI_item188")
	if len(issue.Assignees) != 0 {
		t.Errorf("締め切り前なのに担当者になっている: %+v", issue.Assignees)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Errorf("締め切り前なのに着手している: %d 件", got)
	}
}

// {"RUCM-PATH": "P002"}
//
// TestHandoff_入札に負けたら担当者にならない は、勝者の決め方を確かめる。
//
// 目的: 設計 3-77 の「判定スコアがいちばん大きい機械が勝つ」。
// 与える情報: 判定スコアがこの機械より大きい、ほかの機械の入札が既に1件ある issue。
// 成功条件: 担当者にならず、着手もしないこと。hold のコメントも増えないこと。
func TestHandoff_入札に負けたら担当者にならない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	node := issueNode(188)
	// **この機械の判定スコアは 270 である**（枠を読まない設定なので余裕値は 100 − マージン 10）。
	// それより大きい入札を、ほかの機械が先に書いてある状態にする。
	fx.Tracker.AddCommentBy(node, rivalLogin, handoff.FormatBid(handoff.Bid{
		Host: rivalHost, FiveHour: 100, Weekly: 100, Score: 300, At: time.Now(),
	}), time.Now().Add(-time.Minute))

	fx.Orc.Tick(context.Background())

	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffHoldMarker)); got != 0 {
		t.Errorf("負けたのに hold を書いている: %d 件", got)
	}
	issue, _ := fx.Tracker.IssueByID("PVTI_item188")
	if len(issue.Assignees) != 0 {
		t.Errorf("負けたのに担当者になっている: %+v", issue.Assignees)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Errorf("負けたのに着手している: %d 件", got)
	}
}

// {"RUCM-PATH": "P015"}
//
// TestHandoff_期限内の他人の担当には入札もしない は、設計 3-77b の表の1行を確かめる。
//
// 目的: 「他人1人 ＋ hold あり ＋ 期限内」は触らない（入札もしない）。
// 与える情報: ほかの機械が担当していて、その機械の hold と進捗のコメントが1時間前にある issue。
// 成功条件: 入札も hold も1件も増えず、担当者が変わらないこと。
func TestHandoff_期限内の他人の担当には入札もしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", rivalLogin))

	node := issueNode(188)
	fx.Tracker.AddCommentBy(node, rivalLogin, handoff.FormatHold(handoff.Hold{
		Host: rivalHost, Assignee: rivalLogin, Branch: "continuo/octocat/hello-world/188", At: time.Now(),
	}), time.Now().Add(-time.Hour))

	fx.Orc.Tick(context.Background())

	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffBidMarker)); got != 0 {
		t.Errorf("期限内の他人の担当に入札している: %d 件", got)
	}
	issue, _ := fx.Tracker.IssueByID("PVTI_item188")
	if len(issue.Assignees) != 1 || issue.Assignees[0].Login != rivalLogin {
		t.Errorf("担当者が変わっている: %+v", issue.Assignees)
	}
}

// {"RUCM-PATH": "P016"}
//
// TestHandoff_holdの無い担当は奪わない は、設計 3-77b の「人間が付けた担当」を確かめる。
//
// 目的: **hold のコメントがあることが「その担当者は機械である」の唯一の証拠である。**
// 無ければ人間が付けた担当なので、continuo は取り上げない。
// 与える情報: ほかの人が担当していて、hold のコメントが1件も無い issue
// （**進捗のコメントは1年前**。期限だけで判定していれば奪ってしまう）。
// 成功条件: 入札も hold も増えず、担当者が変わらないこと。
func TestHandoff_holdの無い担当は奪わない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", rivalLogin))

	node := issueNode(188)
	fx.Tracker.AddCommentBy(node, rivalLogin, "去年書いた進捗", time.Now().Add(-365*24*time.Hour))

	fx.Orc.Tick(context.Background())

	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, "")); got != 0 {
		t.Errorf("人間が付けた担当の issue へ持ち回りのコメントを書いている: %d 件", got)
	}
	issue, _ := fx.Tracker.IssueByID("PVTI_item188")
	if len(issue.Assignees) != 1 || issue.Assignees[0].Login != rivalLogin {
		t.Errorf("人間が付けた担当を取り上げている: %+v", issue.Assignees)
	}
}

// {"RUCM-PATH": "P017"}
//
// TestHandoff_担当者が2人以上なら触らない は、設計 3-77b の表の1行を確かめる。
//
// 目的: 担当者が2人以上いるのは人間が触っている合図なので、continuo は触らず WARN を出す。
// 与える情報: 担当者が2人いる issue。
// 成功条件: 持ち回りのコメントが1件も増えず、担当者が2人のままであること。
func TestHandoff_担当者が2人以上なら触らない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.AllowLog("担当者が2人以上いるので触りません")
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", testGHLogin, rivalLogin))

	fx.Orc.Tick(context.Background())

	if got := len(fx.Tracker.MarkedHandoffCommentsOf(issueNode(188), "")); got != 0 {
		t.Errorf("担当者が2人いる issue へ書き込んでいる: %d 件", got)
	}
	issue, _ := fx.Tracker.IssueByID("PVTI_item188")
	if len(issue.Assignees) != 2 {
		t.Errorf("担当者の数が変わっている: %+v", issue.Assignees)
	}
}

// {"RUCM-PATH": "P008"}
//
// TestHandoff_期限切れの担当を外してreleasedを書く は、設計 3-77c を確かめる。
//
// 目的: 「担当者の最後のコメントから idle_timeout_ms を過ぎたら、担当を外して入札をやり直す」。
// 与える情報: ほかの機械が担当していて、その機械の最後のコメントが19時間前にある issue。
// 成功条件: 担当者からその機械が外れ、released のコメントが1件増え、
// **その本文に外した機械の名前が入っていて、引き継ぐ機械の名前は入っていない**こと。
func TestHandoff_期限切れの担当を外してreleasedを書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", rivalLogin))

	node := issueNode(188)
	old := time.Now().Add(-19 * time.Hour)
	fx.Tracker.AddCommentBy(node, rivalLogin, handoff.FormatHold(handoff.Hold{
		Host: rivalHost, Assignee: rivalLogin, Branch: "continuo/octocat/hello-world/188", At: old,
	}), old)

	fx.Orc.Tick(context.Background())

	released := fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffReleasedMarker)
	if len(released) != 1 {
		t.Fatalf("released のコメントが1件ではない: %d 件", len(released))
	}
	if !strings.Contains(released[0].Body, `"from":"`+rivalHost+`"`) {
		t.Errorf("released に外した機械の名前が入っていない:\n%s", released[0].Body)
	}
	if strings.Contains(released[0].Body, `"to":`) {
		t.Errorf("released に引き継ぐ機械の名前を書いている（この段では決まっていない）:\n%s", released[0].Body)
	}

	issue, _ := fx.Tracker.IssueByID("PVTI_item188")
	for _, a := range issue.Assignees {
		if a.Login == rivalLogin {
			t.Errorf("期限切れの担当が外れていない: %+v", issue.Assignees)
		}
	}
}

// {"RUCM-PATH": "P006"}
//
// TestHandoff_枠を読めない機械は入札しない は、設計 3-77 の「投稿しない条件」を確かめる。
//
// 目的: **読めないと使用率0（＝いちばん暇）に見え、必ず勝ってしまう。**だから黙る。
// 与える情報: 枠を読む設定（`oauth_usage_api`）だが、枠を1度も読めていない状態。
// 成功条件: 入札のコメントが1件も増えず、着手もしないこと。
func TestHandoff_枠を読めない機械は入札しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			// **枠を読む設定にする。**読み取り（RateLimit）は渡していないので、
			// 枠の写しは永久に nil のままになる（＝読めなかった状態）。
			cfg.RateLimit.Source = "oauth_usage_api"
		},
	})
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())

	if got := len(fx.Tracker.MarkedHandoffCommentsOf(issueNode(188), config.HandoffBidMarker)); got != 0 {
		t.Errorf("枠を読めないのに入札している: %d 件", got)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Errorf("枠を読めないのに着手している: %d 件", got)
	}
}
