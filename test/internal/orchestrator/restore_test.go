package orchestrator_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// writePendingStop は逃がし先へ `Stop` の JSON を1件置く（設計 3-19 の形）。
//
// t: 呼び出し元のテスト。
// dir: 逃がし先のディレクトリ。
// receivedAt: ファイル名に使う受信時刻（マイクロ秒）。
// sessionID: hook の session_id。
func writePendingStop(t *testing.T, dir, receivedAt, sessionID string) {
	t.Helper()
	body := `{"session_id":"` + sessionID + `","hook_event_name":"Stop","background_tasks":[]}`
	path := filepath.Join(dir, receivedAt+"-Stop.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("逃がし先のファイルを書けません: %v", err)
	}
}

// TestReconcile_active_statesに戻ったらdispatchの前にpaneを閉じる は、
// 設計 3-9 の手順7b の本来の目的を確かめる。
//
// 目的: 復元が引き渡し状態で残した pane が、人間の操作で候補に戻ったとき、
// **dispatch する前に閉じないと同じ worktree に2つ目が立つ。**
//
// 与える情報: `In Review` で復元して pane を残したあと、Status を `Ready` へ戻し、
// 巡回を1回回す。
//
// 成功条件: 残っていた pane が閉じられ、その issue が dispatch されて印に入る。
func TestReconcile_active_statesに戻ったらdispatchの前にpaneを閉じる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	issue := sampleIssue(188, "In Review")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	paneID := wt.WorkspaceID + ":p1"
	installPanes(fx, livePane{
		PaneID: paneID, Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})

	restore(t, fx)
	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("復元の時点で pane を閉じてしまった: %v", ids)
	}

	// 人間が回答して候補に戻した。
	fx.Tracker.SetState(issue.ID, "Ready")
	fx.Orc.Tick(context.Background())

	if ids := closedPaneIDs(fx); indexOf(ids, paneID) < 0 {
		t.Fatalf("候補に戻ったのに残っていた pane を閉じていない: %v", ids)
	}
	waitFor(t, 5*time.Second, "候補に戻った issue が dispatch される", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 1
	})
}
