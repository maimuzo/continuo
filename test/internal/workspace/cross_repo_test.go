package workspace_test

import (
	"path/filepath"
	"testing"

	"github.com/maimuzo/continuo/internal/workspace"
)

// {"RUCM-CFG-SHA256": "66cd40967b07d450648d4cf101addcb45c3483604dd6c390c52366005e83524c", "SOURCE": "docs/spec/usecases/particular_case/issue とコードが別のリポジトリにある issue に着手する.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**「issue とコードが別のリポジトリに
// ある issue に着手する」のうち、置き場所の組み立てに印を付けてある。
// 復元の経路（pane の label から引き直す）は internal/orchestrator の側で確かめる。

// {"RUCM-PATH": "P001"}
// 目的: 置き場所の `<host>/<owner>/<repo>` が**コードのリポジトリ**で切られ、
// **スラグは issue から作られる**ことを確認する
// （#144（worktree の branch は変えず push 先だけ分ける）の設計 8）。
//
// **1本のパスに「どのコードの、どの issue か」が両方出ていなければならない。**
// 出ていないと、片付けも復元も、どちらか一方を見失う。
//
// 与える情報: issue は `myorg/internal-tasks#42`、コードは fork の `myorg/project`。
// 成功条件: 置き場所が `<root>/github.com/myorg/project/continuo-myorg-internal-tasks-42` になり、
// branch 名が issue から作られていること。
func TestLocate_置き場所はコードのリポジトリで切りスラグはissueから作る(t *testing.T) {
	root := t.TempDir()
	issue := workspace.IssueRef{
		URL:        "https://github.com/myorg/internal-tasks/issues/42",
		Identifier: "myorg/internal-tasks#42",
		Owner:      "myorg",
		Repo:       "internal-tasks",
		Number:     42,
		CodeOwner:  "myorg",
		CodeRepo:   "project",
		CodeHost:   "github.com",
	}

	loc, _, err := workspace.Locate(
		root, "continuo/{{.issue.owner}}/{{.issue.repo}}/{{.issue.number}}", issue)
	if err != nil {
		t.Fatalf("Locate に失敗した: %v", err)
	}

	want := filepath.Join(root, "github.com", "myorg", "project", "continuo-myorg-internal-tasks-42")
	if loc.Path != want {
		t.Fatalf("置き場所が一致しない: got %q, want %q", loc.Path, want)
	}
	if loc.Branch.String() != "continuo/myorg/internal-tasks/42" {
		t.Fatalf("branch 名が issue から作られていない: got %q", loc.Branch.String())
	}
}

// 目的: コードのリポジトリを渡さなければ、置き場所が今までと1文字も変わらないことを確認する
// （#144（worktree の branch は変えず push 先だけ分ける）の設計 8）。
//
// **リンクを張っていない利用者の worktree は1つも動いてはならない。**
//
// 与える情報: `CodeOwner` / `CodeRepo` / `CodeHost` が空の issue。
// 成功条件: 置き場所が issue の owner / repo / ホストで切られること。
func TestLocate_コードのリポジトリが空なら置き場所は今までどおり(t *testing.T) {
	root := t.TempDir()

	loc, _, err := workspace.Locate(
		root, "continuo/{{.issue.owner}}/{{.issue.repo}}/{{.issue.number}}", sampleIssue(188))
	if err != nil {
		t.Fatalf("Locate に失敗した: %v", err)
	}

	want := filepath.Join(root, "github.com", "octocat", "hello-world", "continuo-octocat-hello-world-188")
	if loc.Path != want {
		t.Fatalf("置き場所が一致しない: got %q, want %q", loc.Path, want)
	}
}

// 目的: 置き場所の owner/repo が issue のものと違っても、pane の label から
// 番号を切り出せることを確認する
// （#144（worktree の branch は変えず push 先だけ分ける）の設計 8c）。
//
// **置き場所の2・3階層目はコードのリポジトリである。**そこからスラグを組み立てても
// 突き合わせは外れるので、**issue の owner/repo は外から渡してもらうしかない。**
//
// 与える情報: `<root>/github.com/myorg/project/continuo-myorg-internal-tasks-42` と、
// pane の label から取った issue の owner/repo。
// 成功条件: 番号が 42 になり、`Identifier()` が **issue の側**の名前で組み立つこと。
// **`IssueURL()` は空文字であること**（ホストがコードのリポジトリのものなので、捏造しない）。
func TestPathClueOf_コードのリポジトリが違ってもpaneのlabelから番号を切り出す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	dir := filepath.Join(fx.Manager.ResolvedRoot(), "github.com", "myorg", "project",
		"continuo-myorg-internal-tasks-42")

	clue, err := fx.Manager.PathClueOf(dir, "myorg", "internal-tasks")
	if err != nil {
		t.Fatalf("PathClueOf に失敗した: %v", err)
	}
	if clue.Number != 42 {
		t.Fatalf("issue の番号を切り出せていない: got %d, want 42", clue.Number)
	}
	if got, want := clue.Identifier(), "myorg/internal-tasks#42"; got != want {
		t.Fatalf("identifier が issue の側で組み立てられていない: got %q, want %q", got, want)
	}
	if got := clue.IssueURL(); got != "" {
		t.Fatalf("実在しない URL を組み立てている: got %q", got)
	}
}

// 目的: 手掛かりを渡さなくても、コードのリポジトリと issue のリポジトリが同じなら
// 番号を切り出せることを確認する
// （#144（worktree の branch は変えず push 先だけ分ける）の設計 8c）。
//
// **渡さない形にすると、いままで復元できていた worktree が pane を失った瞬間に
// 復元できなくなる。**
//
// 与える情報: いままでどおりの置き場所と、空の手掛かり。
// 成功条件: 番号が切り出せ、`IssueURL()` が組み立つこと。
func TestPathClueOf_手掛かりが無ければ置き場所のownerとrepoで試す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	dir := filepath.Join(fx.Manager.ResolvedRoot(), "github.com", "octocat", "hello-world",
		"continuo-octocat-hello-world-188")

	clue, err := fx.Manager.PathClueOf(dir, "", "")
	if err != nil {
		t.Fatalf("PathClueOf に失敗した: %v", err)
	}
	if clue.Number != 188 {
		t.Fatalf("issue の番号を切り出せていない: got %d", clue.Number)
	}
	if got, want := clue.IssueURL(), "https://github.com/octocat/hello-world/issues/188"; got != want {
		t.Fatalf("issue の URL が違う: got %q, want %q", got, want)
	}
}
