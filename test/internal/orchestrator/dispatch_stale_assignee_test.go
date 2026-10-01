// **候補の写しの担当者が古いときに、着手しないことを確かめる**（設計 3-27。issue #197）。
//
// **候補の写しは巡回の最初に1回だけ取る。**そのあとで手放しが担当者を外し、run の登録まで外すと、
// 同じ巡回の着手の判定は「担当は自分」という古い写しのまま進む。
// **着手の段2 が取り直した issue の担当者に自分がいなければ、着手してはならない。**
package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/tracker"
)

// TestDispatch_候補の写しでは自分が担当でも取り直して担当者にいなければ着手しない は、設計 3-27 を確かめる。
//
// 目的: 担当を手放した直後の issue に、同じ機械がもう1度着手しないこと。
// **着手すると、GitHub の上では担当者がいないので別の機械が入札して拾い、同じ branch で2台が動く。**
//
// 与える情報: カンバンの実体は担当者が0人なのに、候補の一覧には「担当はこの機械」という
// 古い写しが載っている issue（手放しが候補の取得のあとで担当者を外した状況）。
// 成功条件: 着手の直前の取り直しが走り、Status を書かず、run の登録が残らないこと。
func TestDispatch_候補の写しでは自分が担当でも取り直して担当者にいなければ着手しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	// **実体は `In Progress` で担当者0人。**手放しは Status を動かさない。
	fx.Tracker.AddIssue(sampleIssue(4201, "In Progress"))
	stale := sampleIssue(4201, "In Progress")
	stale.Assignees = []tracker.Assignee{{ID: "U_" + fakeViewerLogin, Login: fakeViewerLogin}}
	stale.AssigneeCount = 1
	fx.Tracker.SetExtraCandidates(stale)
	fx.AllowLog("この機械が担当者にいないので着手しません", "着手を取りやめました")

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "着手の直前の取り直しが走る", func() bool {
		return fx.Tracker.CountIDRefreshes() > 0
	})
	fx.WaitRunsDrained(t, 10*time.Second)

	if got := fx.Tracker.CountCall("UpdateStatus"); got != 0 {
		t.Fatalf("担当者にいない issue の Status を書いた: UpdateStatus = %d 回, want 0", got)
	}
	if got := fx.Tracker.CountCall("AddAssignees"); got != 0 {
		t.Fatalf("この巡回で担当者を書いた: AddAssignees = %d 回, want 0（次の巡回で、いまの担当者で判定し直す）", got)
	}
}

// TestDispatch_取り直しても自分が担当なら着手する は、上の検査の対である。
//
// 目的: 上の門が、担当が自分のままの issue まで止めていないこと。
//
// 与える情報: 候補の写しでも、カンバンの実体でも、担当者がこの機械の issue。
// 成功条件: Status を `In Progress` へ書くこと（着手が段2 を越える）。
func TestDispatch_取り直しても自分が担当なら着手する(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	issue := sampleIssue(4202, "In Progress")
	fx.Tracker.AddIssue(issue)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "担当が自分の issue は着手の段2 を越える", func() bool {
		return fx.Tracker.CountCall("UpdateStatus") > 0
	})
}
