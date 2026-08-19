package workspace_test

import (
	"context"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/workspace"
)

// TestSweepOrphanBranches_worktreeも実行中のissueも無いbranchだけを消す は、
// 孤児 branch の掃除の条件を確かめる（設計 3-9 の手順6b）。
//
// 目的: 消してよいのは「接頭辞に一致し、worktree も無く、実行中の issue も無い」branch
// だけである。1つでも条件を落とすと、作業中の成果が入った branch を消す。
//
// 与える情報:
//   - worktree を持つ branch（`Prepare` が作ったもの）
//   - worktree を持たない `continuo/orphan` の branch
//   - 実行中の issue の branch として渡す `continuo/keep` の branch
//   - 接頭辞に一致しない `feature/other` の branch
//
// 成功条件: `continuo/orphan` だけが消え、残りの3本は残る。
func TestSweepOrphanBranches_worktreeも実行中のissueも無いbranchだけを消す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	ctx := context.Background()

	prepared, err := fx.Manager.Prepare(ctx, sampleIssue(188))
	if err != nil {
		t.Fatalf("worktree を用意できません: %v", err)
	}
	runGit(t, fx.Repo.Dir, "branch", "continuo/orphan")
	runGit(t, fx.Repo.Dir, "branch", "continuo/keep")
	runGit(t, fx.Repo.Dir, "branch", "feature/other")

	deleted, err := fx.Manager.SweepOrphanBranches(ctx, workspace.OrphanBranchSweepRequest{
		Worktrees:    []string{prepared.Path},
		KeepBranches: []string{"continuo/keep"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanBranches に失敗した: %v", err)
	}
	if len(deleted) != 1 || !strings.HasSuffix(deleted[0], "continuo/orphan") {
		t.Fatalf("消した branch が想定と違う: got %v", deleted)
	}

	branches := runGit(t, fx.Repo.Dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	for _, want := range []string{prepared.Branch.String(), "continuo/keep", "feature/other"} {
		if !strings.Contains(branches, want) {
			t.Fatalf("消してはならない branch %q を消した: %s", want, branches)
		}
	}
	if strings.Contains(branches, "continuo/orphan") {
		t.Fatalf("孤児 branch を消していない: %s", branches)
	}
}

// TestSweepOrphanBranches_branch_templateに変数が無ければ1本も消さない は、
// 接頭辞を決められないときに掃除しないことを確かめる。
//
// 目的: 設計 3-9 の手順6b。**接頭辞が空だと全部の branch が対象になってしまう。**
//
// 与える情報: `herdr.worktree.branch_template` が変数を1つも含まない設定。
//
// 成功条件: 1本も消さず、掃除を行わなかったことをエラーにもしない。
func TestSweepOrphanBranches_branch_templateに変数が無ければ1本も消さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Herdr.Worktree.BranchTemplate = "continuo-fixed"
	}})
	ctx := context.Background()
	runGit(t, fx.Repo.Dir, "branch", "continuo-fixed")

	deleted, err := fx.Manager.SweepOrphanBranches(ctx, workspace.OrphanBranchSweepRequest{
		Worktrees: []string{fx.Repo.Dir},
	})
	if err != nil {
		t.Fatalf("SweepOrphanBranches がエラーを返した: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("接頭辞を決められないのに branch を消した: %v", deleted)
	}
	branches := runGit(t, fx.Repo.Dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if !strings.Contains(branches, "continuo-fixed") {
		t.Fatalf("branch を消してしまった: %s", branches)
	}
}

// TestSweepOrphanBranches_走査で見つからなかったリポジトリには触らない は、
// 掃除の対象がボードではなく走査の結果で決まることを確かめる。
//
// 目的: 設計 3-9 の手順6b。**対象は走査で見つかった worktree が属するリポジトリだけである。**
//
// 与える情報: worktree を1つも渡さない要求と、孤児 branch を持つリポジトリ。
//
// 成功条件: 1本も消さない。
func TestSweepOrphanBranches_走査で見つからなかったリポジトリには触らない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	runGit(t, fx.Repo.Dir, "branch", "continuo/orphan")

	deleted, err := fx.Manager.SweepOrphanBranches(context.Background(), workspace.OrphanBranchSweepRequest{})
	if err != nil {
		t.Fatalf("SweepOrphanBranches に失敗した: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("対象でないリポジトリの branch を消した: %v", deleted)
	}
	branches := runGit(t, fx.Repo.Dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if !strings.Contains(branches, "continuo/orphan") {
		t.Fatalf("対象でないリポジトリの branch を消してしまった: %s", branches)
	}
}
