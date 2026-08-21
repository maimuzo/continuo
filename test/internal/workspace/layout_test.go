package workspace_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/workspace"
)

// 目的: 置き場所が `<root>/<host>/<owner>/<repo>/<スラグ>` になり、スラグが branch 名の
// スラッシュをハイフンに置き換えたものになることを確認する（設計 3-22）。
// 与える情報: 既定の branch_template（continuo/{{.issue.owner}}/{{.issue.repo}}/{{.issue.number}}）と、
// github.com の issue の URL。
// 成功条件: Path が `<root>/github.com/octocat/hello-world/continuo-octocat-hello-world-188` になること。
func TestLocate_置き場所がgwqの規則になる(t *testing.T) {
	root := t.TempDir()
	loc, warnings, err := workspace.Locate(
		root,
		"continuo/{{.issue.owner}}/{{.issue.repo}}/{{.issue.number}}",
		sampleIssue(188),
	)
	if err != nil {
		t.Fatalf("Locate に失敗した: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("正規化の警告が出るはずがないのに出た: %v", warnings)
	}

	want := filepath.Join(root, "github.com", "octocat", "hello-world", "continuo-octocat-hello-world-188")
	if loc.Path != want {
		t.Fatalf("置き場所が一致しない: got %q, want %q", loc.Path, want)
	}
	if loc.Branch.String() != "continuo/octocat/hello-world/188" {
		t.Fatalf("branch 名が一致しない: got %q", loc.Branch.String())
	}
}

// 目的: 置き場所の <host> が issue の URL のホスト部から取られることを確認する
// （設計 3-22。GitHub Enterprise で別のホストになる）。
// 与える情報: ホストが github.example.com の issue の URL。
// 成功条件: 置き場所の2階層目が github.example.com になること。
func TestLocate_hostはissueのURLのホスト部から取る(t *testing.T) {
	root := t.TempDir()
	issue := sampleIssue(7)
	issue.URL = "https://github.example.com/octocat/hello-world/issues/7"

	loc, _, err := workspace.Locate(root, "continuo/{{.issue.number}}", issue)
	if err != nil {
		t.Fatalf("Locate に失敗した: %v", err)
	}
	if loc.Host != "github.example.com" {
		t.Fatalf("host が issue の URL から取られていない: got %q", loc.Host)
	}
	if !strings.HasPrefix(loc.Path, filepath.Join(root, "github.example.com")+string(os.PathSeparator)) {
		t.Fatalf("置き場所に host が入っていない: %q", loc.Path)
	}
}

// 目的: issue の URL が空のときに <host> が github.com になることを確認する（設計 3-22）。
// 与える情報: URL が空文字の issue。
// 成功条件: Host が github.com になること。
func TestLocate_URLが空ならhostはgithub_comになる(t *testing.T) {
	root := t.TempDir()
	issue := sampleIssue(1)
	issue.URL = ""

	loc, _, err := workspace.Locate(root, "continuo/{{.issue.number}}", issue)
	if err != nil {
		t.Fatalf("Locate に失敗した: %v", err)
	}
	if loc.Host != workspace.DefaultHost {
		t.Fatalf("URL が空のときの host が既定値になっていない: got %q, want %q", loc.Host, workspace.DefaultHost)
	}
}

// 目的: branch_template に未知の変数があれば描画を失敗させることを確認する（設計 3-22）。
// 与える情報: `.issue.unknown` を参照するテンプレート。
// 成功条件: Locate がエラーを返すこと（その issue は失敗として扱う）。
func TestLocate_未知の変数は描画を失敗させる(t *testing.T) {
	_, _, err := workspace.Locate(t.TempDir(), "continuo/{{.issue.unknown}}", sampleIssue(1))
	if err == nil {
		t.Fatal("未知の変数を含むテンプレートなのにエラーにならなかった")
	}
}

// 目的: workspace.root を 0700 で作り、シンボリックリンクを解決したパスを返すことを
// 確認する（設計 3-20 の段1・段2）。
// 与える情報: シンボリックリンク経由の、まだ存在しない置き場所のパス。
// 成功条件: ディレクトリが 0700 で作られ、返る値がリンク先の実体のパスであること。
func TestEnsureRoot_0700で作りシンボリックリンクを解決する(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatalf("実体のディレクトリを作れない: %v", err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("シンボリックリンクを作れない: %v", err)
	}

	root := filepath.Join(link, "worktrees")
	resolved, err := workspace.EnsureRoot(root)
	if err != nil {
		t.Fatalf("EnsureRoot に失敗した: %v", err)
	}

	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("作られた置き場所を stat できない: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("置き場所のパーミッションが 0700 でない: got %o", info.Mode().Perm())
	}

	realResolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatalf("実体を解決できない: %v", err)
	}
	want := filepath.Join(realResolved, "worktrees")
	if resolved != want {
		t.Fatalf("シンボリックリンクが解決されていない: got %q, want %q", resolved, want)
	}
}

// 目的: workspace.root が相対パスならエラーになることを確認する
// （起動したディレクトリで置き場所が変わってしまうため）。
// 与える情報: 相対パスの root。
// 成功条件: EnsureRoot がエラーを返すこと。
func TestEnsureRoot_相対パスはエラーになる(t *testing.T) {
	if _, err := workspace.EnsureRoot("worktrees"); err == nil {
		t.Fatal("相対パスなのにエラーにならなかった")
	}
}

// 目的: 封じ込め検査が、置き場所の内側のパスを通し、外側のパスを弾くことを確認する
// （設計 3-20。SPEC.md 9.5 Invariant 2）。
// 与える情報: 解決済みの root と、その内側・外側・root 自身・`..` を含むパス。
// 成功条件: 内側だけがエラーにならず、他はすべてエラーになること。
func TestCheckContainment_置き場所の外側を弾く(t *testing.T) {
	root := t.TempDir()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("root を解決できない: %v", err)
	}

	inside := filepath.Join(resolved, "github.com", "octocat", "hello-world", "continuo-1")
	if err := workspace.CheckContainment(resolved, inside); err != nil {
		t.Fatalf("内側のパスが弾かれた: %v", err)
	}

	for name, path := range map[string]string{
		"外側":      filepath.Join(filepath.Dir(resolved), "elsewhere"),
		"root自身":  resolved,
		"上へ抜けるパス": filepath.Join(resolved, "..", "escaped"),
	} {
		if err := workspace.CheckContainment(resolved, path); err == nil {
			t.Fatalf("%s（%q）が弾かれなかった", name, path)
		}
	}
}

// 目的: 封じ込め検査が root だけシンボリックリンクを解決して比較することを確認する
// （設計 3-20。この機械の ~/ghq の下は全部シンボリックリンクで、素朴な文字列比較では必ず落ちる）。
// 与える情報: シンボリックリンク経由の root と、リンク先の実体の下にある worktree のパス。
// 成功条件: EnsureRoot が返した解決済みの root と実体のパスを比べて、内側と判定されること。
func TestCheckContainment_rootのシンボリックリンクを解決して比較する(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatalf("実体のディレクトリを作れない: %v", err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("シンボリックリンクを作れない: %v", err)
	}

	resolved, err := workspace.EnsureRoot(filepath.Join(link, "worktrees"))
	if err != nil {
		t.Fatalf("EnsureRoot に失敗した: %v", err)
	}

	realResolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatalf("実体を解決できない: %v", err)
	}
	actual := filepath.Join(realResolved, "worktrees", "github.com", "octocat", "hello-world", "continuo-1")
	if err := workspace.CheckContainment(resolved, actual); err != nil {
		t.Fatalf("実体のパスが内側と判定されなかった: %v", err)
	}
}

// 目的: 作ったあとの封じ込め検査が、実体が置き場所の外側にあるときに失敗することを確認する
// （設計 3-20 の段4。食い違ったら worktree を消さずに残して失敗として扱う）。
// 与える情報: 置き場所の中に置いた、外側の実体を指すシンボリックリンク。
// 成功条件: CheckContainmentResolved がエラーを返し、リンクそのものは消えていないこと。
func TestCheckContainmentResolved_実体が外側ならエラーになる(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "worktrees")
	resolved, err := workspace.EnsureRoot(root)
	if err != nil {
		t.Fatalf("EnsureRoot に失敗した: %v", err)
	}
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatalf("外側のディレクトリを作れない: %v", err)
	}
	link := filepath.Join(resolved, "sneaky")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("シンボリックリンクを作れない: %v", err)
	}

	if _, err := workspace.CheckContainmentResolved(resolved, link); err == nil {
		t.Fatal("実体が外側なのにエラーにならなかった")
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("検査に落ちた worktree が消されている: %v", err)
	}
}
