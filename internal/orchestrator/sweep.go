package orchestrator

import (
	"context"

	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// SweepOnStartup は起動時の掃除を行う（設計 3-9 の手順6 と 6b）。
//
// **必ず復元（Restore）が終わったあとに呼ぶこと。**先に走らせると、これから引き継ぐ run の
// branch を孤児と判定して消す。
//
// やることは2つである。
//
//	手順6  … トラッカーから cleanup.on_states の issue を取り、対応する worktree を片付ける
//	手順6b … 孤児 branch を消す（実体は internal/workspace にある）
//
// **取得に失敗したら警告を出して起動を続ける**（`SPEC.md` 8.6）。
// **`cleanup.enabled` か `cleanup.sweep_on_startup` が偽なら何もしない。**
//
// ctx: 呼び出しに適用するコンテキスト。
// restored: 復元の記録。**残った worktree と、引き継いだ run の branch をここから読む。**
func (o *Orchestrator) SweepOnStartup(ctx context.Context, restored *RestoreResult) {
	if restored == nil {
		return
	}
	if !o.cfg.Cleanup.Enabled || !o.cfg.Cleanup.SweepOnStartup {
		o.logger.Info("起動時の掃除は設定で無効になっています",
			"cleanup.enabled", o.cfg.Cleanup.Enabled,
			"cleanup.sweep_on_startup", o.cfg.Cleanup.SweepOnStartup)
		return
	}

	remaining := o.sweepFinishedWorktrees(ctx, restored.Worktrees)

	// 手順6b: 孤児 branch を消す。**対象は走査で見つかった worktree が属するリポジトリだけ。**
	// **「実行中の issue も無い」の判定には復元後の印の集合を使う。**
	keep := append([]string{}, restored.AdoptedBranches...)
	deleted, err := o.ws.SweepOrphanBranches(ctx, workspace.OrphanBranchSweepRequest{
		Worktrees:    remaining,
		KeepBranches: keep,
	})
	if err != nil {
		o.logger.Warn("孤児 branch の掃除に失敗しました（起動は続けます）", "error", err)
		return
	}
	if len(deleted) > 0 {
		o.logger.Info("孤児 branch を掃除しました", "count", len(deleted), "branches", deleted)
	}
}

// sweepFinishedWorktrees は cleanup.on_states の issue に対応する worktree を片付ける
// （設計 3-9 の手順6）。
//
// **カンバンから取るのは `cleanup.on_states` である**（`terminal_states` ではない。
// 既定値はどちらも `["Done"]` だが別のキーである）。
//
// ctx: 呼び出しに適用するコンテキスト。
// worktrees: 復元のあとに残っている worktree の絶対パス。
// 戻り値: 片付けずに残った worktree の絶対パス（孤児 branch の掃除の対象になる）。
func (o *Orchestrator) sweepFinishedWorktrees(ctx context.Context, worktrees []string) []string {
	if len(worktrees) == 0 {
		return nil
	}
	issues, err := o.tracker.FetchIssuesByStates(ctx, o.cfg.Cleanup.OnStates)
	if err != nil {
		o.logger.Warn("起動時の掃除で issue を取れません（掃除を飛ばして起動を続けます）", "error", err)
		return worktrees
	}
	finished := make(map[string]tracker.Issue, len(issues))
	for _, issue := range issues {
		finished[issue.ID] = issue
	}

	var remaining []string
	for _, path := range worktrees {
		identity, err := o.ws.ReadIdentity(path)
		if err != nil {
			remaining = append(remaining, path)
			continue
		}
		// **印に入っている worktree は触らない。**実行中の照合が見る。
		if _, claimed := o.lookupRunByID(identity.ProjectItemID); claimed {
			remaining = append(remaining, path)
			continue
		}
		issue, done := finished[identity.ProjectItemID]
		if !done {
			remaining = append(remaining, path)
			continue
		}
		o.logger.Info("起動時の掃除で終わっている worktree を片付けます",
			"identifier", issue.Identifier, "path", path, "状態", issue.State)
		if !o.cleanupPath(ctx, issue.Identifier, path, "", issueNodeID(issue)) {
			remaining = append(remaining, path)
		}
	}
	return remaining
}
