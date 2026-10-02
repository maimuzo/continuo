// {"RUCM-CFG-SHA256": "c2483109f99df4d7427c9e07a6f32e8934546b6b061fd37636c67bc715479c63", "SOURCE": "docs/spec/usecases/particular_case/本家のリポジトリへ PR を出す.cfg.json"}
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

// {"RUCM-PATH": "P013"}
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
func Test_本家のリポジトリへPRを出す_P013_baseもdefault_branchも無ければ失敗させる(t *testing.T) {
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
