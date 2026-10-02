// {"RUCM-CFG-SHA256": "5e6a13717ce537aa2e5c4e87dfa29888982c80b2be1a921910c5b6a5f770b150", "SOURCE": "docs/spec/usecases/particular_case/本家のリポジトリへ PR を出す.cfg.json"}
//
// **ユースケース記述「本家のリポジトリへ PR を出す」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
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
// **worktree のすぐ隣**に別のディレクトリを1つ置き、コードのリポジトリの clone に見立てる。
// **隣に置くのは、片付けが親ごと消す形や兄弟を巻き込む形へ壊れたときに、この検査が落ちるようにするためである。**
// 無関係な一時ディレクトリへ置くと、片付けをどう壊しても残ってしまい、検査が空振りする。
// 成功条件: base が main になり、worktree と branch が消え、**隣のディレクトリが残ること。**
func Test_本家のリポジトリへPRを出す_P001_成果がworktreeの外にあっても片付けが通る(t *testing.T) {
	cf := newCleanupFixtureWith(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Herdr.Worktree.Base = nil },
	})

	if cf.Prepared.Base.String() != "main" {
		t.Fatalf("base が issue のリポジトリの既定 branch になっていない: got %q, want %q",
			cf.Prepared.Base.String(), "main")
	}

	// worktree のすぐ隣に置いた「コードのリポジトリの clone」に見立てたディレクトリ。
	// **片付けはここに触ってはならない。**
	// `filepath.Dir(cf.Prepared.Path)` の下へ置くので、片付けが親ごと消す形や
	// 兄弟を巻き込む形へ壊れると、この検査が落ちる。
	forkClone := filepath.Join(filepath.Dir(cf.Prepared.Path), "fork-clone")
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
		t.Fatalf("worktree の隣の clone まで片付けている: %v", statErr)
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
func Test_本家のリポジトリへPRを出す_P002_リモート追跡refが無くてもbaseと差分が無ければ片付く(t *testing.T) {
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
func Test_本家のリポジトリへPRを出す_P003_worktreeの中にpushしていないcommitがあれば片付けない(t *testing.T) {
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

// {"RUCM-PATH": "P006"}
//
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
func Test_本家のリポジトリへPRを出す_P006_身元ファイルだけなら変更として数えない(t *testing.T) {
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

// {"RUCM-PATH": "P019"}
//
// 目的: base を決められない issue を失敗として扱う（base を推測しない）ことを確認する
// （設計 3-22 の段4）。
//
// **「本家のリポジトリへ PR を出す」もここに載る。**あちらの issue は非公開のリポジトリにあり、
// **コードのリポジトリの名前は issue の本文にしか無い。**base を推測されると、continuo は
// 知りもしないリポジトリの branch を起点にしてしまう。
//
// 与える情報: base が null の設定と、NativeRef に default_branch を持たない issue。
// 成功条件: Prepare が ErrBaseUnknown を返し、worktree も branch も作られないこと。
func Test_本家のリポジトリへPRを出す_P019_baseもdefault_branchも無ければ失敗させる(t *testing.T) {
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
