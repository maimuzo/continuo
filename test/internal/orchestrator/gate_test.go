package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/orchestrator"
)

// humanLogin は、continuo が使うアカウントではない人間の担当者である。
const humanLogin = "octocat-human"

// anotherHumanLogin は、2人目の人間の担当者である。
const anotherHumanLogin = "octocat-human-2"

// gateViewOf は、着手の関門で止めた記録から identifier で1件を引く。
//
// fx: 対象の fixture。
// identifier: `<owner>/<repo>#<番号>` の識別子。
// 戻り値の1つ目: 見つかった写し。
// 戻り値の2つ目: 見つかったかどうか。
func gateViewOf(fx *fixture, identifier string) (orchestrator.GateView, bool) {
	for _, v := range fx.Orc.GateViews() {
		if v.Identifier == identifier {
			return v, true
		}
	}
	return orchestrator.GateView{}, false
}

// tickN は巡回を n 回まわし、そのあいだ時計を1回ずつ進める。
//
// fx: 対象の fixture。
// clock: 進める時計。
// n: 巡回する回数。
// step: 1回の巡回のあいだに進める長さ。
func tickN(fx *fixture, clock *testClock, n int, step time.Duration) {
	for i := 0; i < n; i++ {
		fx.Orc.Tick(context.Background())
		clock.Advance(step)
	}
}

// gatedComments は、issue に付いた「着手できずに止まっている」案内のコメントを返す。
//
// fx: 対象の fixture。
// node: 下敷きの GitHub issue のノード ID。
// 戻り値: 案内の本文。
func gatedComments(fx *fixture, node string) []string {
	var out []string
	for _, c := range fx.Tracker.CommentsOf(node) {
		if strings.Contains(c.Body, "<!-- continuo:gated:") {
			out = append(out, c.Body)
		}
	}
	return out
}

// 目的: 人間が付けた担当で止めたことが、ダッシュボードの写しに出ることを確かめる
// （#134（ダッシュボードに「着手できずに止まっているもの」を出す））。
//
// **これがこの変更の成果物そのものである。**いままで手がかりはログの1行だけで、
// continuo はログをファイルに書かないので、pane を見ていない限り誰にも届かなかった。
//
// 与える情報: 人間が担当者になっている issue 1件（hold のコメントは無い）。
// 成功条件: `GateViews` が1件返し、理由が `human_assigned`、担当者と識別子が入っていること。
func TestGate_人間が付けた担当はダッシュボードの写しに出る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", humanLogin))
	fx.AllowLog("担当者が付いているので着手しません")

	fx.Orc.Tick(context.Background())

	v, ok := gateViewOf(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatalf("着手できずに止まっているものに出ていない: %+v", fx.Orc.GateViews())
	}
	if v.Reason != orchestrator.GateReasonHumanAssigned {
		t.Errorf("理由が違う: got %q, want %q", v.Reason, orchestrator.GateReasonHumanAssigned)
	}
	if len(v.Assignees) != 1 || v.Assignees[0] != humanLogin {
		t.Errorf("担当者が入っていない: %+v", v.Assignees)
	}
	if v.URL == "" {
		t.Error("URL が入っていない（画面がリンクにできない）")
	}
	if v.Since.IsZero() {
		t.Error("いつから止まっているかが入っていない")
	}
	if v.Noticed {
		t.Error("1巡回目で「issue へ書き終えた」ことになっている")
	}
}

// 目的: `GateViews` が返したスライスへ書いても、記録そのものが変わらないことを確かめる
// （設計 3-25。ダッシュボードの都合で内部状態を壊さない）。
//
// **`Assignees` はスライスである。**構造体の代入ではヘッダしか写らないので、
// 写し直していないと呼び出し側の書き込みが記録へ届く。
//
// 与える情報: 人間が担当者になっている issue 1件。
// 成功条件: 返ったスライスの中身を書き換えても、次に取り直した写しが元のままであること。
func TestGate_写しへ書いても記録は変わらない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", humanLogin))
	fx.AllowLog("担当者が付いているので着手しません")

	fx.Orc.Tick(context.Background())

	first, ok := gateViewOf(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatal("着手できずに止まっているものに出ていない")
	}
	if len(first.Assignees) == 0 {
		t.Fatal("担当者が入っていない")
	}
	first.Assignees[0] = "書き換えた"

	second, ok := gateViewOf(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatal("2回目で消えている")
	}
	if second.Assignees[0] != humanLogin {
		t.Errorf("写しへの書き込みが記録へ届いている: got %q, want %q", second.Assignees[0], humanLogin)
	}
}

// 目的: 案内を issue へ書くのは、3巡回目かつ最初に止めてから60秒たったあとの1回だけであることを
// 確かめる（#140（人間が担当者で着手できないことを、issue のコメントとして1回だけ書く））。
//
// **1回目では書かない。**人間が担当者を付け替えている最中の1巡回で書くと、
// 数秒で解消する状態に永久に残るコメントを1件足すことになる。
//
// 与える情報: 人間が担当者になっている issue 1件と、1巡回ごとに30秒進む時計。
// 成功条件: 1巡回目と2巡回目では0件、3巡回目で1件、そのあと何度まわしても1件のままであること。
func TestGate_案内は3巡回目に1回だけ書く(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", humanLogin))
	fx.AllowLog("担当者が付いているので着手しません")
	node := issueNode(188)

	tickN(fx, clock, 2, 30*time.Second)
	if got := len(gatedComments(fx, node)); got != 0 {
		t.Fatalf("2巡回目までに書いている: %d 件", got)
	}

	tickN(fx, clock, 1, 30*time.Second)
	bodies := gatedComments(fx, node)
	if len(bodies) != 1 {
		t.Fatalf("3巡回目で1件書いていない: %d 件", len(bodies))
	}
	if !strings.Contains(bodies[0], "<!-- continuo:gated:human_assigned -->") {
		t.Errorf("理由の印が入っていない: %q", bodies[0])
	}
	if strings.Contains(bodies[0], humanLogin) {
		t.Errorf("担当者の名前を書いている（設計 8-1 が禁じている）: %q", bodies[0])
	}

	tickN(fx, clock, 5, 30*time.Second)
	if got := len(gatedComments(fx, node)); got != 1 {
		t.Errorf("巡回のたびに積んでいる: %d 件", got)
	}

	v, ok := gateViewOf(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatal("記録が消えている")
	}
	if !v.Noticed {
		t.Error("書いたのに「まだ書いていない」ことになっている")
	}
}

// 目的: 3巡回目に60秒へ届かなくても、条件が揃った巡回で書けることを確かめる
// （#140（人間が担当者で着手できないことを、issue のコメントとして1回だけ書く））。
//
// **`Count == 3` と書くと、この場合に永久に書かれない。**
// `polling.interval_ms` の既定は30000ミリ秒なので、3巡回目はちょうど60秒であり、
// 揺らぎで59.9秒になった瞬間に条件が二度と揃わなくなる。
//
// 与える情報: 1巡回ごとに20秒しか進まない時計。
// 成功条件: 3巡回目（40秒）では書かず、4巡回目（60秒）で1件書くこと。
func TestGate_3巡回目が60秒に届かなくても次の巡回で書く(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", humanLogin))
	fx.AllowLog("担当者が付いているので着手しません")
	node := issueNode(188)

	tickN(fx, clock, 3, 20*time.Second)
	if got := len(gatedComments(fx, node)); got != 0 {
		t.Fatalf("60秒たっていないのに書いている: %d 件", got)
	}

	tickN(fx, clock, 1, 20*time.Second)
	if got := len(gatedComments(fx, node)); got != 1 {
		t.Errorf("条件が揃った巡回で書いていない: %d 件", got)
	}
}

// 目的: `on_assignee_gate: warn_only` にすると issue へは書かず、
// **記録とダッシュボードは残る**ことを確かめる（設計 8-2）。
//
// **この設定は記録そのものには一切効かせない。**効かせると、切った運用者が
// 「なぜ着手されないのか」を知る手立てを1つも持たなくなる。
//
// 与える情報: `on_assignee_gate: warn_only` の設定と、人間が担当者になっている issue 1件。
// 成功条件: 案内が0件で、写しは残り、`NoticeSkip` が `off_by_config` になっていること。
func TestGate_warn_onlyではissueへ書かず記録は残る(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{
		Now: clock.Now,
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.Provider.Handoff.OnAssigneeGate = config.OnAssigneeGateWarnOnly
		},
	})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", humanLogin))
	fx.AllowLog("担当者が付いているので着手しません")

	tickN(fx, clock, 5, 30*time.Second)

	if got := len(gatedComments(fx, issueNode(188))); got != 0 {
		t.Errorf("warn_only なのに issue へ書いている: %d 件", got)
	}
	v, ok := gateViewOf(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatal("warn_only で記録まで消えている（ダッシュボードから読めなくなる）")
	}
	if v.Noticed {
		t.Error("書いていないのに「書いた」ことになっている")
	}
	if v.NoticeSkip != orchestrator.GateNoticeOffByConfig {
		t.Errorf("書かない理由が違う: got %q, want %q", v.NoticeSkip, orchestrator.GateNoticeOffByConfig)
	}
}

// 目的: 担当者が2人以上で、そこに gh の持ち主が混じっていないときは、
// 案内も記録も作ることを確かめる（#136（担当者が2人以上いる issue も、着手できないことを知らせる））。
//
// 与える情報: 人間2人が担当者になっている issue 1件。
// 成功条件: 理由が `many_assignees` で、3巡回目に案内が1件書かれること。
func TestGate_担当者が2人以上なら人間だけのときに案内する(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", humanLogin, anotherHumanLogin))
	fx.AllowLog("担当者が2人以上いるので触りません")

	tickN(fx, clock, 3, 30*time.Second)

	v, ok := gateViewOf(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatalf("着手できずに止まっているものに出ていない: %+v", fx.Orc.GateViews())
	}
	if v.Reason != orchestrator.GateReasonManyAssignees {
		t.Errorf("理由が違う: got %q, want %q", v.Reason, orchestrator.GateReasonManyAssignees)
	}
	bodies := gatedComments(fx, issueNode(188))
	if len(bodies) != 1 {
		t.Fatalf("案内を1件書いていない: %d 件", len(bodies))
	}
	if !strings.Contains(bodies[0], "<!-- continuo:gated:many_assignees -->") {
		t.Errorf("理由の印が入っていない: %q", bodies[0])
	}
}

// 目的: 担当者が2人以上で、そこに gh の持ち主が混じっているときは、
// **issue へは書かず、記録は作る**ことを確かめる（設計 8-3）。
//
// **この状態がいちばん切り分けが難しい。**この分岐は hold のコメントを1行も読まないので、
// 「人間が2人」と「人間1人＋別の機械が hold を持っている」を区別できない。
// **後者で「担当者をすべて外してください」と案内すると、走っている別の機械の担当が外れ、
// 次の巡回で同じ issue に2台が乗る。**
// **だからといってダッシュボードからも消すと、人間の手がかりが WARN の1行だけになる。**
//
// 与える情報: 人間1人と gh の持ち主が担当者になっている issue 1件。
// 成功条件: 案内が0件で、写しの理由が `many_assignees_with_self`、
// `NoticeSkip` が `unclear_owner` になっていること。
func TestGate_担当者にghの持ち主が混じっていたら書かずに記録だけ残す(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", humanLogin, testGHLogin))
	fx.AllowLog("担当者が2人以上いるので触りません")

	tickN(fx, clock, 5, 30*time.Second)

	if got := len(gatedComments(fx, issueNode(188))); got != 0 {
		t.Errorf("切り分けられないのに issue へ書いている: %d 件", got)
	}
	v, ok := gateViewOf(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatalf("ダッシュボードから消えている（いちばん切り分けの難しい状態が読めなくなる）: %+v",
			fx.Orc.GateViews())
	}
	if v.Reason != orchestrator.GateReasonManyAssigneesWithSelf {
		t.Errorf("理由が違う: got %q, want %q", v.Reason, orchestrator.GateReasonManyAssigneesWithSelf)
	}
	if v.Noticed {
		t.Error("書いていないのに「書いた」ことになっている")
	}
	if v.NoticeSkip != orchestrator.GateNoticeUnclearOwner {
		t.Errorf("書かない理由が違う: got %q, want %q", v.NoticeSkip, orchestrator.GateNoticeUnclearOwner)
	}
}

// 目的: 手元のコメントが取得の上限で切れていたら、案内を書かずに印だけ立てることを確かめる
// （設計 7-1）。
//
// **書けないことより、同じ案内を2件書くことのほうが困る。**コメントを消す手段が無いためである。
// 上限に達すると落ちるのは古い側で、**前の起動で書いた案内はそこにある。**
//
// 与える情報: 「古い側を読み切れなかった」と名乗るトラッカーと、人間が担当者の issue 1件。
// 成功条件: 案内が0件で、`NoticeSkip` が `too_many_comments` になっていること。
func TestGate_コメントが上限で切れていたら書かない(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now})
	holdPrompt(fx)
	fx.Tracker.SetCommentsTruncated(true)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", humanLogin))
	fx.AllowLog("担当者が付いているので着手しません")
	fx.AllowLog("コメントが多すぎて、前に案内を書いたかどうかを確かめられないので書きません")

	tickN(fx, clock, 5, 30*time.Second)

	if got := len(gatedComments(fx, issueNode(188))); got != 0 {
		t.Errorf("切れているのに書いている（同じ案内が2件になりうる）: %d 件", got)
	}
	v, ok := gateViewOf(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatal("記録が消えている")
	}
	if v.NoticeSkip != orchestrator.GateNoticeTooManyComments {
		t.Errorf("書かない理由が違う: got %q, want %q", v.NoticeSkip, orchestrator.GateNoticeTooManyComments)
	}
}

// 目的: 前の起動で書いた案内が手元のコメントに見つかったら、二度と書かないことを確かめる
// （設計 7）。**記録はメモリだけに持つので、再起動すると印は消える。**
//
// **`found` を `truncated` より先に見る。**新しい側に案内が残っているなら、
// 古い側が切れていても答えは出ている。
//
// 与える情報: gh の持ち主が既に案内を書いてある issue と、切れていると名乗るトラッカー。
// 成功条件: 案内が1件のまま増えず、写しが「書き終えている」を名乗ること。
func TestGate_前の起動で書いた案内があれば二度と書かない(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now})
	holdPrompt(fx)
	fx.Tracker.SetCommentsTruncated(true)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", humanLogin))
	fx.Tracker.AddCommentBy(issueNode(188), testGHLogin,
		"<!-- continuo:self -->\n<!-- continuo:gated:human_assigned -->\n前の起動で書いた案内",
		clock.Now().Add(-24*time.Hour))
	fx.AllowLog("担当者が付いているので着手しません")

	tickN(fx, clock, 5, 30*time.Second)

	if got := len(gatedComments(fx, issueNode(188))); got != 1 {
		t.Errorf("前の案内があるのに書き足している: %d 件", got)
	}
	v, ok := gateViewOf(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatal("記録が消えている")
	}
	if !v.Noticed {
		t.Error("前の案内を見つけたのに「まだ書いていない」ことになっている")
	}
	if v.NoticeSkip != "" {
		t.Errorf("見つかったのに書かない理由が立っている: %q", v.NoticeSkip)
	}
}

// 目的: 印が付いていても、投稿者が gh の持ち主でなければ「前に書いた案内」と読まないことを確かめる
// （設計 3-65）。
//
// **第三者が同じ印を書いただけで案内を止められてはならない。**
//
// 与える情報: 人間が同じ印のコメントを書いてある issue 1件。
// 成功条件: continuo が自分の案内を1件書くこと（人間のものと合わせて2件になる）。
func TestGate_他人が書いた印は前の案内と読まない(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", humanLogin))
	fx.Tracker.AddCommentBy(issueNode(188), humanLogin,
		"<!-- continuo:gated:human_assigned -->\n人間が印を騙って書いた",
		clock.Now().Add(-24*time.Hour))
	fx.AllowLog("担当者が付いているので着手しません")

	tickN(fx, clock, 3, 30*time.Second)

	if got := len(gatedComments(fx, issueNode(188))); got != 2 {
		t.Errorf("他人の印を自分の案内と読んでいる: %d 件（人間の1件 + continuo の1件で2件であるべき）", got)
	}
}

// 目的: 理由が変わったら回数を数え直し、**理由ごとの案内の状態は持ち越す**ことを確かめる
// （設計 6-5）。
//
// **どちらに倒しても壊れる。**全部数え直すと、担当者を2人と1人で往復させるたびに
// 同じ案内が積まれる。何も数え直さないと、先に書いたほうの理由が、もう一方の案内を永久に塞ぐ。
//
// 与える情報: 人間1人で案内まで進めたあと、担当者を2人に増やし、また1人に戻す。
// 成功条件: 理由ごとに1件ずつ（合わせて2件）書かれ、往復しても3件目が増えないこと。
func TestGate_理由が変わっても同じ理由の案内は1回だけ(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", humanLogin))
	fx.AllowLog("担当者が付いているので着手しません")
	fx.AllowLog("担当者が2人以上いるので触りません")
	node := issueNode(188)

	// 人間1人で案内まで進める。
	tickN(fx, clock, 3, 30*time.Second)
	if got := len(gatedComments(fx, node)); got != 1 {
		t.Fatalf("人間1人の案内が書かれていない: %d 件", got)
	}

	// 担当者を2人に増やす。**別の理由なので、この理由でも3巡回と60秒が要る。**
	fx.Tracker.SetAssignees("PVTI_item188", humanLogin, anotherHumanLogin)
	tickN(fx, clock, 1, 30*time.Second)
	if got := len(gatedComments(fx, node)); got != 1 {
		t.Errorf("理由が変わった1巡回目で書いている（数え直していない）: %d 件", got)
	}
	tickN(fx, clock, 2, 30*time.Second)
	if got := len(gatedComments(fx, node)); got != 2 {
		t.Fatalf("担当者が2人以上の案内が書かれていない: %d 件", got)
	}

	// 1人へ戻し、また2人へ増やす。**どちらの理由も、もう書いてある。**
	fx.Tracker.SetAssignees("PVTI_item188", humanLogin)
	tickN(fx, clock, 4, 30*time.Second)
	fx.Tracker.SetAssignees("PVTI_item188", humanLogin, anotherHumanLogin)
	tickN(fx, clock, 4, 30*time.Second)

	if got := len(gatedComments(fx, node)); got != 2 {
		t.Errorf("往復のたびに案内が積まれている: %d 件（理由ごとに1件ずつで2件であるべき）", got)
	}
}

// 目的: 担当者を外して着手できるようになったら、記録が消えることを確かめる（設計 6-2）。
//
// **直ったのに残ると、直しても消えない行がダッシュボードに残る。**
//
// 与える情報: 人間が担当者の issue 1件と、そのあと担当者を外した状態。
// 成功条件: 担当者を外した巡回で記録が0件になること。
func TestGate_担当者を外したら記録が消える(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", humanLogin))
	fx.AllowLog("担当者が付いているので着手しません")

	tickN(fx, clock, 1, 30*time.Second)
	if _, ok := gateViewOf(fx, "octocat/hello-world#188"); !ok {
		t.Fatal("止めたのに記録が無い")
	}

	fx.Tracker.SetAssignees("PVTI_item188")
	tickN(fx, clock, 1, 30*time.Second)

	if v, ok := gateViewOf(fx, "octocat/hello-world#188"); ok {
		t.Errorf("直ったのに記録が残っている: %+v", v)
	}
}

// 目的: ボードの候補から消えたら記録も消えることを確かめる（設計 6-3）。
//
// 与える情報: 人間が担当者の issue 1件と、そのあとボードから外した状態。
// 成功条件: 外した巡回で記録が0件になること。
func TestGate_ボードから外れたら記録が消える(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", humanLogin))
	fx.AllowLog("担当者が付いているので着手しません")

	tickN(fx, clock, 1, 30*time.Second)
	if _, ok := gateViewOf(fx, "octocat/hello-world#188"); !ok {
		t.Fatal("止めたのに記録が無い")
	}

	fx.Tracker.RemoveIssue("PVTI_item188")
	tickN(fx, clock, 1, 30*time.Second)

	if v, ok := gateViewOf(fx, "octocat/hello-world#188"); ok {
		t.Errorf("ボードから消えたのに記録が残っている: %+v", v)
	}
}

// 目的: 候補の取得に失敗した巡回では、記録が1件も減らないことを確かめる（設計 6-3）。
//
// **失敗した巡回で掃除すると、全件が消える。**v1 の設計はここで落ちた。
//
// 与える情報: 人間が担当者の issue 1件と、そのあと候補の取得が失敗する状態。
// 成功条件: 失敗した巡回のあとも記録が残っていること。
func TestGate_候補を取れなかった巡回では記録が減らない(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", humanLogin))
	fx.AllowLog("担当者が付いているので着手しません")
	fx.AllowLog("候補の取得に失敗しました")

	tickN(fx, clock, 1, 30*time.Second)
	if _, ok := gateViewOf(fx, "octocat/hello-world#188"); !ok {
		t.Fatal("止めたのに記録が無い")
	}

	fx.Tracker.SetStatesError(errors.New("GitHub へ繋がりません"))
	tickN(fx, clock, 1, 30*time.Second)

	if _, ok := gateViewOf(fx, "octocat/hello-world#188"); !ok {
		t.Error("候補を取れなかっただけで記録が消えている（v1 の穴）")
	}
}

// 目的: 関門より前で飛ばした issue の記録が、消えることを確かめる（設計 6-1）。
//
// **ここを塞がないと、関門へ到達しなくなった issue の行が、
// 古い理由と誤った直し方を付けて永久に残る。**
//
// **`Dispatchable` を偽にして再現する。**`handoffGate` より前にある5つの `continue` の1つで、
// アダプタが「このリポジトリは信頼されていない」と判定した状態にあたる。
//
// 与える情報: 人間が担当者の issue 1件と、そのあと `Dispatchable` を偽にした状態。
// 成功条件: 関門より前で飛ばした巡回で記録が消えること。
func TestGate_関門より前で飛ばしたら記録が消える(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", humanLogin))
	fx.AllowLog("担当者が付いているので着手しません")

	tickN(fx, clock, 1, 30*time.Second)
	if _, ok := gateViewOf(fx, "octocat/hello-world#188"); !ok {
		t.Fatal("止めたのに記録が無い")
	}

	fx.Tracker.SetDispatchable("PVTI_item188", false)
	tickN(fx, clock, 1, 30*time.Second)

	if v, ok := gateViewOf(fx, "octocat/hello-world#188"); ok {
		t.Errorf("関門より前で飛ばしたのに、担当者の理由が残っている: %+v", v)
	}
}
