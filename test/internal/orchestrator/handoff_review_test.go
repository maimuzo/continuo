// **PR #108 のレビューで見つかった穴を塞いだことの検査である**（設計 3-77b / 3-77f 〜 3-77i）。
//
// **見張っているのは3点である。**
//
//	着手をやめたときの後始末  … 書いた担当者を消し戻す。**残すと18時間塞がる**
//	使用率の値の寿命         … 古くなったら入札しない。**古い値で入札し続けない**
//	巡回を塞がないこと        … コメントを読む issue の数に上限を置く
package orchestrator_test

import (
	"context"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// 目的: 着手の直前の検査に落ちる issue には、担当者を1バイトも書かないことを確認する
// （設計 3-77g）。
//
// **担当者と hold を書いてから落とすと、ほかの機械はそれを「期限内の担当」と読み、
// `idle_timeout_ms`（既定18時間）触らない。**
// **この機械では信頼していないが、別の機械では信頼しているリポジトリが、そのあいだ塞がる。**
//
// 与える情報: リポジトリを信頼登録していない状態（着手の直前の検査で必ず落ちる）と、
// 担当者のいない issue 1件。
// 成功条件: 入札も hold も1件も書かれず、担当者も付かないこと。
func TestHandoff_信頼していないリポジトリには担当者を書かない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Untrusted: true})
	holdPrompt(fx)
	fx.AllowLog("信頼登録されていません")
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())

	node := issueNode(188)
	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffBidMarker)); got != 0 {
		t.Errorf("着手できない issue に入札している: %d 件", got)
	}
	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffHoldMarker)); got != 0 {
		t.Errorf("着手できない issue の担当者になっている: hold が %d 件", got)
	}
	issue, ok := fx.Tracker.IssueByID("PVTI_item188")
	if !ok {
		t.Fatal("issue がカンバンから消えた")
	}
	if len(issue.Assignees) != 0 {
		t.Errorf("着手できない issue に担当者を書いた（18時間塞がる）: %+v", issue.Assignees)
	}
}
