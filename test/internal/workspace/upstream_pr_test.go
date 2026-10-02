// **「本家のリポジトリへ PR を出す」のうち、continuo 側の振る舞いだけを固定する。**
// このユースケースは issue が非公開のリポジトリにあり、コードは public の fork にある。
// **成果は worktree の中に1バイトも残らない**（エージェントが worktree の外の clone で直す）。
//
// **このファイルに置くのは、このユースケースにしか無い観点だけである。**
// base の決め方（経路 P013）は test/internal/workspace/prepare_test.go の
// `Test_本家のリポジトリへPRを出す_P019_baseもdefault_branchも無ければ失敗させる` が、判定の hook を足すかどうかは
// test/internal/orchestrator/tool_gate_test.go が押さえているので、**同じ検査をここへ写さない。**
//
// **エージェントの判断に属する段はテストにできない。**理由は
// docs/spec/usecases/particular_case/本家のリポジトリへ PR を出す.judge_log.md に書いてある。
package workspace_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dropIdentityExclude は `info/exclude` を消し、身元ファイルが未追跡として
// `git status --porcelain` に現れる状態にする。
//
// **これをしないと身元ファイルは除外に載ったままで、`git status --porcelain` に出てこない。**
// 出てこないものを「数から外せているか」で確かめることはできない。
//
// t: 呼び出し元のテスト。
// cf: 片付けの検査に使う状態。
func dropIdentityExclude(t *testing.T, cf *cleanupFixture) {
	t.Helper()
	excludePath := filepath.Join(cf.Repo.Dir, ".git", "info", "exclude")
	if err := os.Remove(excludePath); err != nil {
		t.Fatalf("info/exclude を消せない: %v", err)
	}
	status := runGit(t, cf.Prepared.Path, "status", "--porcelain")
	if !strings.Contains(status, cf.Config.Workspace.IdentityFile) {
		t.Fatalf("前提が崩れている（身元ファイルが未追跡として出ていない）: %q", status)
	}
}
