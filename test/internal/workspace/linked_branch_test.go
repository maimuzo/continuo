// {"RUCM-CFG-SHA256": "5ba01b4a174c146c45e05e581754e126e476934e66380f1e243886859c4b3419", "SOURCE": "docs/spec/usecases/particular_case/issue にリンクされた branch を起点にして着手する.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**「issue にリンクされた branch を
// 起点にして着手する」のうち、base の決まり方を分ける3本に印を付けてある。
package workspace_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/workspace"
)

// pushBranchAndForget は、origin に branch を1本作り、**手元からその痕跡を全部消す。**
//
// **こうしないと fetch の要不要を確かめられない。**`git push` は
// `refs/remotes/origin/<名前>` を作るので、消さないと「手元にあるので通信しない」の枝を通る。
//
// t: 呼び出し元のテスト。
// repo: 出来合いの clone。
// branch: 作る branch の名前。
// 戻り値: その branch の先端の commit。
func pushBranchAndForget(t *testing.T, repo *testRepo, branch string) string {
	t.Helper()
	runGit(t, repo.Dir, "checkout", "--quiet", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo.Dir, "linked.txt"), []byte("リンク先の中身\n"), 0o644); err != nil {
		t.Fatalf("ファイルを書けません: %v", err)
	}
	runGit(t, repo.Dir, "add", ".")
	runGit(t, repo.Dir, "commit", "--quiet", "-m", "リンクされた branch の commit")
	tip := runGit(t, repo.Dir, "rev-parse", "HEAD")
	runGit(t, repo.Dir, "push", "--quiet", "origin", branch)

	// **手元の痕跡を消す。**local の branch と、リモート追跡 ref の両方である。
	runGit(t, repo.Dir, "checkout", "--quiet", "main")
	runGit(t, repo.Dir, "branch", "-D", branch)
	runGit(t, repo.Dir, "update-ref", "-d", "refs/remotes/origin/"+branch)
	return tip
}

// 目的: リンクされた branch が手元に無くても、1本だけ取ってきて base にすることを確認する
// （#144（worktree の branch は変えず push 先だけ分ける）の設計 10 / 11a）。
//
// **refspec を明示しないと、`--single-branch` の clone ではリモート追跡 ref ができず、
// `git worktree add … origin/<名前>` が `fatal: invalid reference` で落ちる。**
//
// 与える情報: origin にだけある branch と、それをリンクとして持つ issue。
// 成功条件: base が `origin/<名前>` になり、**worktree の HEAD がその branch の先端**であること。
// {"RUCM-PATH": "P001"}
func TestPrepare_リンクされたbranchを取ってきてbaseにする(t *testing.T) {
	repo := newTestRepo(t)
	tip := pushBranchAndForget(t, repo, "work/issue-42")
	fx := newFixture(t, fixtureOptions{Repo: repo})

	issue := sampleIssue(42)
	issue.LinkedBranch = "work/issue-42"

	prepared, err := fx.Manager.Prepare(context.Background(), issue)
	if err != nil {
		t.Fatalf("Prepare に失敗した: %v", err)
	}
	if prepared.Base.String() != "origin/work/issue-42" {
		t.Fatalf("base がリンクされた branch になっていない: got %q", prepared.Base.String())
	}
	if got := runGit(t, prepared.Path, "rev-parse", "HEAD"); got != tip {
		t.Fatalf("worktree がリンクされた branch の先端から切られていない: got %q, want %q", got, tip)
	}
	// **worktree の branch は continuo のものである。**リンク先を出してはならない
	// （出すと、次の巡回で「別の branch を出している」と判定されて着手できなくなる）。
	if prepared.Branch.String() != "continuo/octocat/hello-world/42" {
		t.Fatalf("worktree の branch がリンク先に置き換わっている: got %q", prepared.Branch.String())
	}
}

// 目的: 設定の `herdr.worktree.base` が書いてあれば、リンクより先に効くことを確認する
// （#144（worktree の branch は変えず push 先だけ分ける）の設計 11a の段1）。
//
// **この順番は利用者へ知らせる必要がある。**fork を使うカンバンで `main` と書いてあると、
// **fork にしか無い branch から始められない。**
//
// 与える情報: `herdr.worktree.base` が `main`、リンクは `work/issue-42`。
// 成功条件: base が `main` になること。
// {"RUCM-PATH": "P006"}
func TestPrepare_設定のbaseはリンクより先に効く(t *testing.T) {
	repo := newTestRepo(t)
	pushBranchAndForget(t, repo, "work/issue-42")
	base := "main"
	fx := newFixture(t, fixtureOptions{
		Repo:   repo,
		Mutate: func(c *config.Config) { c.Herdr.Worktree.Base = &base },
	})

	issue := sampleIssue(42)
	issue.LinkedBranch = "work/issue-42"

	prepared, err := fx.Manager.Prepare(context.Background(), issue)
	if err != nil {
		t.Fatalf("Prepare に失敗した: %v", err)
	}
	if prepared.Base.String() != "main" {
		t.Fatalf("設定の base が効いていない: got %q", prepared.Base.String())
	}
}

// 目的: リンクされた branch を取ってこられなければ、専用の番兵で断ることを確認する
// （#144（worktree の branch は変えず push 先だけ分ける）の設計 10b）。
//
// **黙ってログだけにしない。**呼び出し側（着手の段3）がこの番兵を見て、
// その issue を失敗として扱い、理由を issue へ書く。
//
// 与える情報: origin に存在しない branch をリンクとして持つ issue。
// 成功条件: `ErrLinkedBranchFetch` が返り、**worktree が1つも作られていない**こと。
// {"RUCM-PATH": "P003"}
func TestPrepare_リンクされたbranchを取ってこられなければ番兵で断る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})

	issue := sampleIssue(42)
	issue.LinkedBranch = "work/does-not-exist"

	prepared, err := fx.Manager.Prepare(context.Background(), issue)
	if err == nil {
		t.Fatalf("取ってこられないのに Prepare が成功した: %+v", prepared)
	}
	if !errors.Is(err, workspace.ErrLinkedBranchFetch) {
		t.Fatalf("番兵が ErrLinkedBranchFetch でない: %v", err)
	}
}
