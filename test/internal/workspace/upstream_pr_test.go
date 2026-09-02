// {"RUCM-CFG-SHA256": "b5cdee62809a11dd51093149b06eba6a835ce3d6326900510463169ac3d95fc5", "SOURCE": "docs/spec/usecases/particular_case/本家のリポジトリへ PR を出す.cfg.json"}
//
// **「本家のリポジトリへ PR を出す」のうち、continuo 側の振る舞いだけを固定する。**
// このユースケースは issue が非公開のリポジトリにあり、コードは public の fork にある。
// **成果は worktree の中に1バイトも残らない**（エージェントが worktree の外の clone で直す）。
//
// **エージェントの判断に属する段はテストにできない。**理由は
// docs/spec/usecases/particular_case/本家のリポジトリへ PR を出す.judge_log.md に書いてある。
package workspace_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/workspace"
)

// {"RUCM-PATH": "P001"}
//
// 目的: **成果が worktree の外にあっても片付けが通ること**を確かめる。
// このユースケースでは、エージェントは worktree の外の clone でコードを直して fork へ push する。
// **worktree には身元ファイルしか無い。**それでも continuo は「失うものがある」と判定してはならない
// （設計 3-9 の手順2。判定してしまうと worktree が永久に残る）。
//
// **base が issue のリポジトリの既定 branch になることも、同じテストで見る。**
// コードのリポジトリの名前は issue の本文にしか無く、continuo はそれを知らないまま worktree を作る
// （設計 3-22 の段4）。
//
// 与える情報: `herdr.worktree.base` を null にした設定と、`default_branch` が main の issue。
// worktree の外に別のディレクトリを1つ置き、コードのリポジトリの clone に見立てる。
// 成功条件: base が main になり、worktree と branch が消え、**worktree の外のディレクトリが残ること。**
func TestUpstreamPR_成果がworktreeの外にあっても片付けが通る(t *testing.T) {
	cf := newCleanupFixtureWith(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Herdr.Worktree.Base = nil },
	})

	if cf.Prepared.Base.String() != "main" {
		t.Fatalf("base が issue のリポジトリの既定 branch になっていない: got %q, want %q",
			cf.Prepared.Base.String(), "main")
	}

	// worktree の外に置いた「コードのリポジトリの clone」に見立てたディレクトリ。
	// **片付けはここに触ってはならない。**
	forkClone := filepath.Join(t.TempDir(), "fork-clone")
	if err := os.MkdirAll(forkClone, 0o755); err != nil {
		t.Fatalf("コードのリポジトリの clone に見立てたディレクトリを作れない: %v", err)
	}
	forkFile := filepath.Join(forkClone, "直したコード.md")
	if err := os.WriteFile(forkFile, []byte("本家へ出した内容\n"), 0o600); err != nil {
		t.Fatalf("コードのリポジトリの clone にファイルを置けない: %v", err)
	}

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed || result.Deferred {
		t.Fatalf("worktree に成果が無いのに片付けていない: %+v", *result)
	}
	if _, statErr := os.Stat(cf.Prepared.Path); statErr == nil {
		t.Fatal("worktree の実体が消えていない")
	}
	if _, statErr := os.Stat(forkFile); statErr != nil {
		t.Fatalf("worktree の外の clone まで片付けている: %v", statErr)
	}
}

// {"RUCM-PATH": "P002"}
//
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

// {"RUCM-PATH": "P003"}
//
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

// {"RUCM-PATH": "P004"}
//
// 目的: **身元ファイルだけの worktree を「変更が残っている」と数えないこと**を確かめる
// （設計 3-9 の手順2 と 3-18）。continuo 自身が置いた身元ファイルを数に入れると、
// **このユースケースの worktree は1つも片付かない。**中身がそれしか無いためである。
//
// **数えるべきものは数えることも、同じテストで見る。**エージェントが worktree の中に
// 置いたままにしたファイルは見送りの理由になる。
//
// 与える情報: 身元ファイルだけの worktree と、そこへ足した未追跡のファイル1件。
// 成功条件: 足す前は Removed が真、足したあとは Deferred が真になること。
func TestUpstreamPR_身元ファイルだけなら変更として数えない(t *testing.T) {
	clean := newCleanupFixture(t, nil)

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

// {"RUCM-PATH": "P013"}
//
// 目的: **issue のリポジトリの既定 branch が分からなければ着手しないこと**を確かめる
// （設計 3-22 の段4）。このユースケースの issue は非公開のリポジトリにあり、
// **コードのリポジトリの名前は issue の本文にしか無い。**continuo は base を推測してはならない。
//
// 与える情報: `herdr.worktree.base` を null にした設定と、`default_branch` を持たない issue。
// 成功条件: Prepare が ErrBaseUnknown を返し、worktree も branch も作られないこと。
func TestUpstreamPR_既定branchが分からなければ着手しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Herdr.Worktree.Base = nil },
	})
	issue := sampleIssue(188)
	issue.NativeRef = map[string]any{}

	_, err := fx.Manager.Prepare(context.Background(), issue)
	if !errors.Is(err, workspace.ErrBaseUnknown) {
		t.Fatalf("base を決められないのに ErrBaseUnknown にならない: %v", err)
	}
	if branches := runGit(t, fx.Repo.Dir, "branch", "--list", "continuo/*"); strings.TrimSpace(branches) != "" {
		t.Fatalf("base が決まらないのに branch が作られている: %q", branches)
	}
}
