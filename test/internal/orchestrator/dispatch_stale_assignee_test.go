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
)

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
