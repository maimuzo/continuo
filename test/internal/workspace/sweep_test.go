package workspace_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
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

// TestSweepOrphanBranches_worktreeのgitが書き換えられていたら別のリポジトリを掃除しない は、
// 掃除の対象リポジトリを git の答えだけで決めないことを確かめる。
//
// 目的: worktree の `.git` は `gitdir: …` と書かれただけのファイルであり、
// **その worktree で動くエージェントが書き換えられる。**書き換えを信じると、
// 無関係のリポジトリの `continuo/` で始まる branch を `git branch -D`（強制削除）で消す。
//
// 与える情報: `.git` を別のリポジトリへ向けた worktree と、その別のリポジトリにある
// `continuo/victim-branch` の branch。
//
// 成功条件: 1本も消さず、別のリポジトリの branch が残っていること。
func TestSweepOrphanBranches_worktreeのgitが書き換えられていたら別のリポジトリを掃除しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	ctx := context.Background()
	prepared, err := fx.Manager.Prepare(ctx, sampleIssue(188))
	if err != nil {
		t.Fatalf("worktree を用意できません: %v", err)
	}

	victim := newTestRepo(t)
	runGit(t, victim.Dir, "branch", "continuo/victim-branch")
	tamperGitFile(t, prepared.Path, victim)

	deleted, err := fx.Manager.SweepOrphanBranches(ctx, workspace.OrphanBranchSweepRequest{
		Worktrees: []string{prepared.Path},
	})
	if err != nil {
		t.Fatalf("SweepOrphanBranches がエラーを返した: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("書き換えられた .git を信じて branch を消した: %v", deleted)
	}
	branches := runGit(t, victim.Dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if !strings.Contains(branches, "continuo/victim-branch") {
		t.Fatalf("無関係のリポジトリの branch を消した: %s", branches)
	}
}

// TestSweepOrphanBranches_消す前にSHAをログへ残す は、強制削除から戻せる手掛かりを
// 残すことを確かめる。
//
// 目的: 設計 3-9 の手順6b の削除は `git branch -D`（マージ状態を見ない強制削除）である。
// **未 push の commit が載ったままの branch も消えるので、**戻すための SHA を
// 消す前にログへ残す。残さないと復旧手段は reflog を掘ることしか無い。
//
// 与える情報: worktree を持たない `continuo/orphan` の branch と、出力を受け取るロガー。
//
// 成功条件: ログにその branch の SHA と、そのまま実行すれば戻せるコマンドが出ること。
func TestSweepOrphanBranches_消す前にSHAをログへ残す(t *testing.T) {
	var logged bytes.Buffer
	fx := newFixture(t, fixtureOptions{
		Logger: slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelInfo})),
	})
	ctx := context.Background()
	prepared, err := fx.Manager.Prepare(ctx, sampleIssue(188))
	if err != nil {
		t.Fatalf("worktree を用意できません: %v", err)
	}
	runGit(t, fx.Repo.Dir, "branch", "continuo/orphan")
	sha := runGit(t, fx.Repo.Dir, "rev-parse", "continuo/orphan")

	deleted, err := fx.Manager.SweepOrphanBranches(ctx, workspace.OrphanBranchSweepRequest{
		Worktrees: []string{prepared.Path},
	})
	if err != nil {
		t.Fatalf("SweepOrphanBranches に失敗した: %v", err)
	}
	if len(deleted) != 1 {
		t.Fatalf("孤児 branch を消していない: %v", deleted)
	}

	out := logged.String()
	if !strings.Contains(out, sha) {
		t.Fatalf("消した branch の SHA がログに残っていない（reflog を掘るしかなくなる）:\n%s", out)
	}
	if !strings.Contains(out, "branch continuo/orphan "+sha) {
		t.Fatalf("戻すためのコマンドがログに残っていない:\n%s", out)
	}
}

// TestSweepOrphanBranches_deleteBranchが偽なら1本も消さない は、起動時の掃除が
// `cleanup.delete_branch` に従うことを確かめる。
//
// 目的: **片付けは設定を見て branch を残し、「branch は残しました」と人間へ言う。**
// その branch は「接頭辞に一致し・worktree も無く・実行中の run も無い」の3条件を
// 全部満たすので、設定を見ない掃除は**次に continuo を起動しただけで
// `git branch -D`（強制削除）で消す。****`continuo abandon --force` で片付けた
// worktree の branch には未 push の commit が載っていることがあり、消えれば
// reflog を掘る以外に戻す手立てが無い。**
//
// 与える情報: `cleanup.delete_branch` を偽にした設定、worktree を1つ持つリポジトリ、
// worktree を持たない `continuo/orphan` の branch、**壊れた ref にした
// `continuo/broken` の branch**（壊れた ref だけを例外にしていないかも見る）。
//
// 成功条件: 1本も消さず、通常の branch も壊れた ref のファイルも残っていること。
func TestSweepOrphanBranches_deleteBranchが偽なら1本も消さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Cleanup.DeleteBranch = false
	}})
	ctx := context.Background()

	prepared, err := fx.Manager.Prepare(ctx, sampleIssue(188))
	if err != nil {
		t.Fatalf("worktree を用意できません: %v", err)
	}
	runGit(t, fx.Repo.Dir, "branch", "continuo/orphan")
	runGit(t, fx.Repo.Dir, "branch", "continuo/broken")
	brokenRef := breakBranchRef(t, fx.Repo.Dir, "continuo/broken")

	deleted, err := fx.Manager.SweepOrphanBranches(ctx, workspace.OrphanBranchSweepRequest{
		Worktrees: []string{prepared.Path},
	})
	if err != nil {
		t.Fatalf("SweepOrphanBranches がエラーを返した: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("cleanup.delete_branch が偽なのに branch を消した: %v", deleted)
	}

	branches := runGit(t, fx.Repo.Dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if !strings.Contains(branches, "continuo/orphan") {
		t.Fatalf("cleanup.delete_branch が偽なのに孤児 branch を消してしまった: %s", branches)
	}
	if _, err := os.Stat(brokenRef); err != nil {
		t.Fatalf("cleanup.delete_branch が偽なのに壊れた ref のファイルを消してしまった（%s）: %v",
			brokenRef, err)
	}
}
