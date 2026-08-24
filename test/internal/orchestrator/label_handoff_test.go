package orchestrator_test

import (
	"testing"

	"github.com/maimuzo/continuo/internal/herdr"
)

// TestRestore_旧い形のlabelが付いたpaneでも引き継げる は、label の形を変えても
// 再起動後の引き継ぎが壊れないことを固定する（issue #12 の受け入れ条件）。
//
// 目的: **label は人間が herdr の画面で pane を見分けるための表示名であり、
// continuo は読み戻さない**（設計 3-3）ことを、テストで動かせない形にする。
// 引き継ぎの照合は pane の cwd と worktree のパスだけで行う。
//
// 与える情報:
//   - `In Progress` の issue が1件。その worktree と身元ファイルがディスクにある
//   - その worktree を cwd に持つ pane が生きていて、**label には issue の URL
//     （label の形を変える前の、continuo が以前書いていた文字列）が入っている**
//
// 成功条件: label の形が新しいものと違っていても、印（実行中の一覧）に入ること。
func TestRestore_旧い形のlabelが付いたpaneでも引き継げる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{SessionUUID: "sess-188"})

	// **いま continuo が書く label とは違う形である。**
	oldLabel := *issue.URL
	if oldLabel == herdr.IssueLabel(issue.Owner, issue.Repo, issue.Number) {
		t.Fatalf("旧い形と新しい形が同じでは、この検査が何も確かめていない: %q", oldLabel)
	}
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188", Label: oldLabel,
	})

	result, _ := restore(t, fx)

	if got := fx.Orc.RunningIdentifiers(); len(got) != 1 || got[0] != issue.Identifier {
		t.Fatalf("旧い形の label が付いた pane を引き継げていない: got %v", got)
	}
	if len(result.Adopted) != 1 || result.Adopted[0] != issue.Identifier {
		t.Fatalf("復元の記録に引き継ぎが残っていない: got %v", result.Adopted)
	}
	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("引き継げるはずの pane を閉じてしまった: %v", ids)
	}
}
