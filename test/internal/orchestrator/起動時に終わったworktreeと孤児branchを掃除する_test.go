// {"RUCM-CFG-SHA256": "6c290fa8c9e070110efe1c69e9be80ccf98831a3038afeb7ee9b6ff7fae60fd4", "SOURCE": "docs/spec/usecases/particular_case/起動時に終わったworktreeと孤児branchを掃除する.cfg.json"}
//
// **ユースケース記述「起動時に終わったworktreeと孤児branchを掃除する」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package orchestrator_test

import (
	"context"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// {"RUCM-PATH": "P001"}
//
// Test_起動時に終わったworktreeと孤児branchを掃除する_P001_復元のあとに走り引き継いだbranchを消さない は、
// 起動時の掃除の順番と対象を確かめる。
//
// 目的: 設計 3-9 の手順6b。**先に走らせると、これから引き継ぐ run の branch を
// 孤児と判定して消す。**「実行中の issue も無い」の判定には復元後の印の集合を使う。
//
// 与える情報:
//   - 引き継げる run が1件（その branch は worktree がある）
//   - worktree を持たない孤児 branch `continuo/orphan/1` が1本
//   - 接頭辞に一致しない branch `feature/keep` が1本
//
// 成功条件: 孤児 branch だけが消え、引き継いだ run の branch と接頭辞に一致しない
// branch は残る。
func Test_起動時に終わったworktreeと孤児branchを掃除する_P001_復元のあとに走り引き継いだbranchを消さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})
	runGit(t, fx.Repo.Dir, "branch", "continuo/orphan/1")
	runGit(t, fx.Repo.Dir, "branch", "feature/keep")

	result, _ := restore(t, fx)
	fx.Orc.SweepOnStartup(context.Background(), result)

	branches := runGit(t, fx.Repo.Dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if strings.Contains(branches, "continuo/orphan/1") {
		t.Fatalf("孤児 branch を消していない: %s", branches)
	}
	if !strings.Contains(branches, wt.Branch) {
		t.Fatalf("引き継いだ run の branch を消してしまった: %s", branches)
	}
	if !strings.Contains(branches, "feature/keep") {
		t.Fatalf("接頭辞に一致しない branch を消してしまった: %s", branches)
	}
}

// {"RUCM-PATH": "P003"}
//
// Test_起動時に終わったworktreeと孤児branchを掃除する_P003_deleteBranchが偽なら孤児branchを1本も消さない は、
// **起動時の掃除が `cleanup.delete_branch` を見る**ことを確かめる。
//
// 目的: 片付け（設計 3-9 の段4）はこの設定を見て branch を残し、「branch は残しました」と
// 人間へ言う。**その branch は掃除の3条件を全部満たす**（接頭辞に一致し、どの worktree も
// チェックアウトしておらず、実行中の run も無い）。**設定を見ない掃除は、次に continuo を
// 起動しただけでその branch を強制削除で消す。**`continuo abandon --force` で片付けた
// worktree の branch には未 push の commit が載っていることがあり、消えれば reflog を
// 掘る以外に戻す手立ては無い。
//
// 与える情報: `cleanup.delete_branch` を偽にした設定と、worktree を持たない孤児 branch
// `continuo/orphan/1`。**掃除そのものは有効のまま**（`cleanup.enabled` と
// `cleanup.sweep_on_startup` は真）なので、掃除の入口までは同じように入る。
// 成功条件: 孤児 branch が残っていること。
func Test_起動時に終わったworktreeと孤児branchを掃除する_P003_deleteBranchが偽なら孤児branchを1本も消さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Cleanup.DeleteBranch = false
	}})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})
	runGit(t, fx.Repo.Dir, "branch", "continuo/orphan/1")

	result, _ := restore(t, fx)
	fx.Orc.SweepOnStartup(context.Background(), result)

	branches := runGit(t, fx.Repo.Dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if !strings.Contains(branches, "continuo/orphan/1") {
		t.Fatalf("cleanup.delete_branch が false なのに孤児 branch を消した: %s", branches)
	}
	if !strings.Contains(branches, wt.Branch) {
		t.Fatalf("引き継いだ run の branch を消してしまった: %s", branches)
	}
}
