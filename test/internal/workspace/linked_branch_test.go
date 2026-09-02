package workspace_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/workspace"
)

// pushLinkedBranch は origin に branch を1本作り、clone 側からはその痕跡を消す。
//
// **「人間が GitHub の画面で branch を作り、issue の Development からリンクした直後」を
// 再現する。**手元の clone はまだその branch を1バイトも知らない状態になる。
//
// t: 呼び出し元のテスト。
// repo: 対象のリポジトリ。
// branch: 作る branch の名前。
// 戻り値: その branch の先頭の commit の SHA。
func pushLinkedBranch(t *testing.T, repo *testRepo, branch string) string {
	t.Helper()
	runGit(t, repo.Dir, "checkout", "--quiet", "-b", branch)
	if err := os.WriteFile(repo.Dir+"/LINKED.md", []byte("リンクされた branch の中身\n"), 0o644); err != nil {
		t.Fatalf("リンクされた branch のファイルを書けません: %v", err)
	}
	runGit(t, repo.Dir, "add", ".")
	runGit(t, repo.Dir, "commit", "--quiet", "-m", "リンクされた branch の commit")
	runGit(t, repo.Dir, "push", "--quiet", "origin", branch)
	sha := runGit(t, repo.Dir, "rev-parse", "HEAD")

	// 手元から消す。**ローカル branch とリモート追跡 ref の両方を落とす。**
	// どちらかが残っていると「取ってこないと base に据えられない」状態にならない。
	runGit(t, repo.Dir, "checkout", "--quiet", repo.Base)
	runGit(t, repo.Dir, "branch", "--quiet", "-D", branch)
	runGit(t, repo.Dir, "update-ref", "-d", "refs/remotes/origin/"+branch)
	return sha
}

// linkedIssue は sampleIssue に、リンクされた branch を1本足した issue を返す。
//
// number: issue 番号。
// branch: リンクされた branch の名前（`origin/` は付けない）。
// 戻り値: LinkedBranch を持つ IssueRef。
func linkedIssue(number int, branch string) workspace.IssueRef {
	issue := sampleIssue(number)
	issue.LinkedBranch = branch
	return issue
}

// 目的: issue にリンクされた branch を worktree の起点にすることを確認する
// （設計 3-22d）。**手元に無ければ、その1本だけを fetch してから使う。**
// 与える情報: base を null にした設定と、origin にだけ在る `work/issue-42` を
// リンクした issue。
// 成功条件: PrepareResult.Base が `origin/work/issue-42` であり、作られた worktree の
// HEAD がその branch の commit と一致すること。
func TestPrepare_リンクされたbranchを起点にする(t *testing.T) {
	repo := newTestRepo(t)
	linkedSHA := pushLinkedBranch(t, repo, "work/issue-42")
	fx := newFixture(t, fixtureOptions{
		Repo:   repo,
		Mutate: func(cfg *config.Config) { cfg.Herdr.Worktree.Base = nil },
	})

	result, err := fx.Manager.Prepare(context.Background(), linkedIssue(42, "work/issue-42"))
	if err != nil {
		t.Fatalf("Prepare に失敗した: %v", err)
	}
	if result.Base.String() != "origin/work/issue-42" {
		t.Fatalf("base がリンクされた branch になっていない: got %q, want %q",
			result.Base.String(), "origin/work/issue-42")
	}
	head := runGit(t, result.Path, "rev-parse", "HEAD")
	if head != linkedSHA {
		t.Fatalf("worktree の起点がリンクされた branch でない: got %q, want %q", head, linkedSHA)
	}
}

// 目的: リモート追跡 ref が既に手元にあるときは、1バイトも通信しないことを確認する
// （設計 3-22d）。**着手のたびに通信すると、遅い回線で巡回のループごと詰まる。**
// 与える情報: `origin/work/issue-42` を手元に持たせたうえで、**origin の実体そのものを
// 消した** clone。fetch を1本でも叩けば必ず失敗する状態である。
// 成功条件: Prepare が成功し、base がリンクされた branch になっていること。
func TestPrepare_リンクされたbranchが手元にあるなら通信しない(t *testing.T) {
	repo := newTestRepo(t)
	runGit(t, repo.Dir, "checkout", "--quiet", "-b", "work/issue-42")
	runGit(t, repo.Dir, "push", "--quiet", "origin", "work/issue-42")
	linkedSHA := runGit(t, repo.Dir, "rev-parse", "HEAD")
	runGit(t, repo.Dir, "checkout", "--quiet", repo.Base)
	runGit(t, repo.Dir, "branch", "--quiet", "-D", "work/issue-42")
	// **リモート追跡 ref は残す。**push が作ったものをそのまま使う。
	if err := os.RemoveAll(repo.Origin); err != nil {
		t.Fatalf("origin を消せません: %v", err)
	}

	fx := newFixture(t, fixtureOptions{
		Repo:   repo,
		Mutate: func(cfg *config.Config) { cfg.Herdr.Worktree.Base = nil },
	})

	result, err := fx.Manager.Prepare(context.Background(), linkedIssue(42, "work/issue-42"))
	if err != nil {
		t.Fatalf("手元にリモート追跡 ref があるのに Prepare が失敗した（fetch を叩いている）: %v", err)
	}
	if result.Base.String() != "origin/work/issue-42" {
		t.Fatalf("base がリンクされた branch になっていない: got %q", result.Base.String())
	}
	head := runGit(t, result.Path, "rev-parse", "HEAD")
	if head != linkedSHA {
		t.Fatalf("worktree の起点がリンクされた branch でない: got %q, want %q", head, linkedSHA)
	}
}

// 目的: リンクされた branch を取ってこられないときに、黙って既定 branch へ倒さないことを
// 確認する（設計 3-22d）。**倒すと、人間がリンクした branch とは別の起点でエージェントが
// 作業を始め、食い違いに気づくのは PR を出したあとになる。**
// 与える情報: origin に存在しない branch をリンクした issue。
// 成功条件: Prepare が ErrBaseUnknown を返し、worktree の branch が作られないこと。
func TestPrepare_リンクされたbranchを取ってこられなければ失敗させる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Herdr.Worktree.Base = nil },
	})

	_, err := fx.Manager.Prepare(context.Background(), linkedIssue(42, "work/does-not-exist"))
	if !errors.Is(err, workspace.ErrBaseUnknown) {
		t.Fatalf("取ってこられないのに ErrBaseUnknown にならない: %v", err)
	}
	if branches := runGit(t, fx.Repo.Dir, "branch", "--list", "continuo/*"); strings.TrimSpace(branches) != "" {
		t.Fatalf("base が決まらないのに branch が作られている: %q", branches)
	}
}

// 目的: 設定の `herdr.worktree.base` が、リンクされた branch より優先されることを
// 確認する（設計 3-22d の base を決める順番）。**明示があれば、いつでもそれが勝つ。**
// 与える情報: base を `main` にした設定と、origin にだけ在る `work/issue-42` を
// リンクした issue。
// 成功条件: PrepareResult.Base が `main` であり、worktree の起点が main の commit で
// あること。
func TestPrepare_設定のbaseがリンクされたbranchより優先される(t *testing.T) {
	repo := newTestRepo(t)
	pushLinkedBranch(t, repo, "work/issue-42")
	configured := "main"
	fx := newFixture(t, fixtureOptions{
		Repo:   repo,
		Mutate: func(cfg *config.Config) { cfg.Herdr.Worktree.Base = &configured },
	})

	result, err := fx.Manager.Prepare(context.Background(), linkedIssue(42, "work/issue-42"))
	if err != nil {
		t.Fatalf("Prepare に失敗した: %v", err)
	}
	if result.Base.String() != "main" {
		t.Fatalf("設定の base が使われていない: got %q, want %q", result.Base.String(), "main")
	}
	head := runGit(t, result.Path, "rev-parse", "HEAD")
	mainHead := runGit(t, fx.Repo.Dir, "rev-parse", "main")
	if head != mainHead {
		t.Fatalf("worktree の起点が main でない: got %q, want %q", head, mainHead)
	}
}

// 目的: リンクが無い issue は今までどおり既定 branch を起点にすることを確認する
// （設計 3-22d。**リンクを足したことで、リンクの無い issue の挙動を変えていない**）。
// 与える情報: base を null にした設定と、LinkedBranch が空の issue。
// 成功条件: PrepareResult.Base が `main` であること。
func TestPrepare_リンクが無ければ既定branchを起点にする(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Herdr.Worktree.Base = nil },
	})

	result, err := fx.Manager.Prepare(context.Background(), sampleIssue(42))
	if err != nil {
		t.Fatalf("Prepare に失敗した: %v", err)
	}
	if result.Base.String() != "main" {
		t.Fatalf("base が既定 branch になっていない: got %q, want %q", result.Base.String(), "main")
	}
}
