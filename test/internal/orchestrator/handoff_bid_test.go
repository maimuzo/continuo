// {"RUCM-CFG-SHA256": "fe753abb326479a54b57fdfb144ce72406238b073244ff713b29db67ad24490d", "SOURCE": "docs/spec/usecases/particular_case/issue の担当を入札で決める.cfg.json"}
//
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
	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/tracker"
)

// rivalHost は入札を競う別の機械の名前である（架空の名前）。
const rivalHost = "thinkpad"

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
		t.Fatal("issue がカンバンから消えた")
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
	}, testBidWindow), time.Now().Add(-time.Minute))

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

// ownBidsOf は、この機械が書いた入札のコメントだけを数える。
//
// **ほかの機械の入札と混ぜて数えない。**巡回のたびに増えるのはこの機械の入札であり、
// **そこが増えないことが「次の回が始まった」の証拠である。**
//
// fx: 検査対象。
// node: 下敷きの GitHub issue のノード ID。
// 戻り値: この機械の名前が入った入札のコメント。
func ownBidsOf(fx *fixture, node string) []tracker.Comment {
	out := make([]tracker.Comment, 0, 4)
	for _, c := range fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffBidMarker) {
		if strings.Contains(c.Body, `"host":"`+testHostName+`"`) {
			out = append(out, c)
		}
	}
	return out
}

// TestHandoff_古い入札が残っていても締め切りをまたいで担当者が決まる は、設計 3-77e を確かめる。
//
// 目的: **前の回の入札は issue に残り続ける**（入札は1回ごとに新しいコメントを書く）。
// それを数え続けると締め切りが常にその古い時刻から数えられ、**次の回が1度も始まらない。**
// **巡回のたびに入札のコメントだけが増え、担当者は永久に決まらない。**
//
// 与える情報: 締め切りを3分にした設定と、30分前に書かれたほかの機械の入札1件
// （判定スコアはこの機械より大きい **300**）。時計は手で進める。
// 成功条件: 巡回を3回行っても、**この機械の入札のコメントは1件だけ**であること。
// 締め切りを過ぎた巡回でこの機械が担当者になり、hold が1件書かれ、着手されること。
func TestHandoff_古い入札が残っていても締め切りをまたいで担当者が決まる(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{
		Now: clock.Now,
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.Provider.Handoff.BidWindowMs = 180000
		},
	})
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	node := issueNode(188)
	// **終わった回の入札である。**締め切り（3分）にも決着の猶予（さらに3分）にも入らない。
	fx.Tracker.AddCommentBy(node, rivalLogin, handoff.FormatBid(handoff.Bid{
		Host: rivalHost, FiveHour: 100, Weekly: 100, Score: 300, At: clock.Now().Add(-30 * time.Minute),
	}, testBidWindow), clock.Now().Add(-30*time.Minute))

	// 1回目。**次の回を始める入札を1件書き、締め切りを待つ。**
	fx.Orc.Tick(context.Background())
	if got := len(ownBidsOf(fx, node)); got != 1 {
		t.Fatalf("1回目の巡回でこの機械の入札が1件ではない: %d 件", got)
	}

	// 2回目（30秒後）。**締め切りの中なので、入札は増えない。**
	clock.Advance(30 * time.Second)
	fx.Orc.Tick(context.Background())
	if got := len(ownBidsOf(fx, node)); got != 1 {
		t.Fatalf("締め切りを待つあいだに入札が増えた: %d 件", got)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Fatalf("締め切り前なのに着手している: %d 件", got)
	}

	// 3回目（締め切りの後）。**勝って担当者になる。**
	clock.Advance(4 * time.Minute)
	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "dispatch される", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 1
	})

	if got := len(ownBidsOf(fx, node)); got != 1 {
		t.Errorf("巡回のたびに入札が増えている: %d 件", got)
	}
	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffHoldMarker)); got != 1 {
		t.Errorf("hold のコメントが1件ではない: %d 件", got)
	}
	issue, _ := fx.Tracker.IssueByID("PVTI_item188")
	if len(issue.Assignees) != 1 || issue.Assignees[0].Login != testGHLogin {
		t.Errorf("担当者が gh の持ち主1人になっていない: %+v", issue.Assignees)
	}
}

// TestHandoff_期限切れの担当を外したあと前の回の入札に負けない は、設計 3-77e を確かめる。
//
// 目的: **担当を外した直後は、前の回に勝った機械の入札が必ず issue に残っている。**
// それを数えると、担当を外した機械は毎回その入札に負ける。**担当者は誰にも書かれず、
// 巡回のたびに入札のコメントだけが増える。**この機能の主目的である「期限で担当を入れ替える」
// 経路が、そのままこの状態に入る。
//
// 与える情報: ほかの機械が担当していて、その機械の hold が19時間前、
// **その機械が前の回に勝ったときの入札（判定スコア 300）がその1分前**にある issue。
// 成功条件: 担当が外れ、この機械が担当者になって着手すること。
func TestHandoff_期限切れの担当を外したあと前の回の入札に負けない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", rivalLogin))

	node := issueNode(188)
	old := time.Now().Add(-19 * time.Hour)
	// **前の回の入札。**hold より前にあり、判定スコアはこの機械（270）より大きい。
	fx.Tracker.AddCommentBy(node, rivalLogin, handoff.FormatBid(handoff.Bid{
		Host: rivalHost, FiveHour: 100, Weekly: 100, Score: 300, At: old.Add(-time.Minute),
	}, testBidWindow), old.Add(-time.Minute))
	fx.Tracker.AddCommentBy(node, rivalLogin, handoff.FormatHold(handoff.Hold{
		Host: rivalHost, Assignee: rivalLogin, Branch: "continuo/octocat/hello-world/188", At: old,
	}), old)

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "dispatch される", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 1
	})

	if got := len(ownBidsOf(fx, node)); got != 1 {
		t.Errorf("この機械の入札が1件ではない: %d 件", got)
	}
	issue, _ := fx.Tracker.IssueByID("PVTI_item188")
	if len(issue.Assignees) != 1 || issue.Assignees[0].Login != testGHLogin {
		t.Errorf("前の回の入札に負けて担当者になれていない: %+v", issue.Assignees)
	}
	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffHoldMarker)); got != 2 {
		t.Errorf("hold のコメントが2件（前の回とこの回）ではない: %d 件", got)
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
