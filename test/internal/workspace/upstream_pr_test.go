// **「本家のリポジトリへPRを出す」のうち、continuo 側の振る舞いだけを固定する。**
// このユースケースは issue が非公開のリポジトリにあり、コードは public の fork にある。
// **成果は worktree の中に1バイトも残らない**（エージェントが worktree の外の clone で直す）。
//
// **このファイルに置くのは、このユースケースにしか無い観点だけである。**
// base の決め方（経路 P013）は test/internal/workspace/prepare_test.go の
// `Test_本家のリポジトリへPRを出す_P013_baseもdefault_branchも無ければ失敗させる` が、判定の hook を足すかどうかは
// test/internal/orchestrator/tool_gate_test.go が押さえているので、**同じ検査をここへ写さない。**
//
// **エージェントの判断に属する段はテストにできない。**理由は
// docs/spec/usecases/particular_case/本家のリポジトリへPRを出す.judge_log.md に書いてある。
package workspace_test

import (
	"context"
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

// 目的: **remote を1つも持たない clone でも、base と差分が無ければ片付くこと**を確かめる
// （設計 3-9 の手順2b の段3）。段1（HEAD がリモート追跡 ref に載っているか）は
// `refs/remotes/` が空だと必ず偽になるので、この経路は base との差分だけが手掛かりになる。
//
// 与える情報: origin を外した clone と、コードを1行も足していない worktree。
// 成功条件: Removed が真になり、worktree の実体が消えること。
func TestUpstreamPR_リモート追跡refが無くてもbaseと差分が無ければ片付く(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	// `refs/remotes/` を空にする。**worktree は clone を共有する**ので、
	// clone 側で外せば worktree からも見えなくなる。
	runGit(t, cf.Repo.Dir, "remote", "remove", "origin")
	if refs := runGit(t, cf.Prepared.Path, "for-each-ref", "--format=%(refname)", "refs/remotes/"); refs != "" {
		t.Fatalf("リモート追跡 ref が残っている: %q", refs)
	}

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed || result.Deferred {
		t.Fatalf("base と差分が無いのに片付けていない: %+v", *result)
	}
	if _, statErr := os.Stat(cf.Prepared.Path); statErr == nil {
		t.Fatal("worktree の実体が消えていない")
	}
}

// 目的: **worktree の中に push していない commit があれば片付けないこと**を確かめる。
// このユースケースの成果は worktree の外にあるが、**エージェントが worktree の中でも直した場合、
// そちらは fork へ push されていない。**消すと失われる（設計 3-9 の手順2b）。
//
// 与える情報: origin を外した clone と、worktree の中で積んだ commit 1件。
// 成功条件: Deferred が真、Removed が偽で、worktree が残ること。
func TestUpstreamPR_worktreeの中にpushしていないcommitがあれば片付けない(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	runGit(t, cf.Repo.Dir, "remote", "remove", "origin")

	if err := os.WriteFile(filepath.Join(cf.Prepared.Path, "worktreeの中の成果.md"), []byte("できた\n"), 0o600); err != nil {
		t.Fatalf("成果のファイルを書けない: %v", err)
	}
	runGit(t, cf.Prepared.Path, "add", ".")
	runGit(t, cf.Prepared.Path, "commit", "--quiet", "-m", "worktree の中の成果")

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if result.Removed || !result.Deferred {
		t.Fatalf("base と差分があるのに片付けている: %+v", *result)
	}
	if _, statErr := os.Stat(cf.Prepared.Path); statErr != nil {
		t.Fatalf("見送ったのに worktree が消えている: %v", statErr)
	}
}

// 目的: **身元ファイルだけの worktree を「変更が残っている」と数えないこと**を確かめる
// （設計 3-9 の手順2 と 3-18）。continuo 自身が置いた身元ファイルを数に入れると、
// **このユースケースの worktree は1つも片付かない。**中身がそれしか無いためである。
//
// **`info/exclude` を先に消す。**Prepare は身元ファイルの名前をそこへ書くので、
// **消さないと `git status --porcelain` に身元ファイルが1度も現れず、この検査は空振りする**
// （`identityStatusExcludes` が空を返すようになっても緑のままになる）。
//
// **数えるべきものは数えることも、同じテストで見る。**エージェントが worktree の中に
// 置いたままにしたファイルは見送りの理由になる。
//
// 与える情報: `info/exclude` を消した worktree と、そこへ足した未追跡のファイル1件。
// 成功条件: 足す前は Removed が真、足したあとは Deferred が真になること。
func TestUpstreamPR_身元ファイルだけなら変更として数えない(t *testing.T) {
	clean := newCleanupFixture(t, nil)
	dropIdentityExclude(t, clean)

	identityPath := filepath.Join(clean.Prepared.Path, clean.Config.Workspace.IdentityFile)
	if _, err := os.Stat(identityPath); err != nil {
		t.Fatalf("身元ファイルが worktree の中に無い: %v", err)
	}

	cleanResult, err := clean.Manager.Cleanup(context.Background(), cleanupRequest(clean))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !cleanResult.Removed || cleanResult.Deferred {
		t.Fatalf("身元ファイルを変更として数えている: %+v", *cleanResult)
	}

	dirty := newCleanupFixture(t, nil)
	dropIdentityExclude(t, dirty)
	if err := os.WriteFile(filepath.Join(dirty.Prepared.Path, "書きかけ.md"), []byte("途中\n"), 0o600); err != nil {
		t.Fatalf("未追跡のファイルを書けない: %v", err)
	}

	dirtyResult, err := dirty.Manager.Cleanup(context.Background(), cleanupRequest(dirty))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if dirtyResult.Removed || !dirtyResult.Deferred {
		t.Fatalf("未追跡のファイルが残っているのに片付けている: %+v", *dirtyResult)
	}
}
