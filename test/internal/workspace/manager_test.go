package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/workspace"
)

// 目的: workspace.root を起動時に 0700 で作ることを確認する（設計 3-20 の段1。
// **封じ込め検査は解決済みの root と比較するので、検査より前に作らなければならない**）。
// 与える情報: まだ存在しない置き場所を指した設定。
// 成功条件: workspace.New だけでディレクトリができ、パーミッションが 0700 で、
// ResolvedRoot がその実体を指していること。
func TestNew_起動時に置き場所を0700で作る(t *testing.T) {
	root := filepath.Join(t.TempDir(), "まだ無い", "worktrees")
	cfg := *config.DefaultConfig()
	cfg.Workspace.Root = root

	mgr, err := workspace.New(workspace.Options{Config: cfg, HomeDir: t.TempDir()})
	if err != nil {
		t.Fatalf("workspace.New に失敗した: %v", err)
	}

	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("起動時に置き場所が作られていない: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("置き場所がディレクトリでない")
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("置き場所のパーミッションが 0700 でない: got %o", info.Mode().Perm())
	}

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("置き場所を解決できない: %v", err)
	}
	if mgr.ResolvedRoot() != resolvedRoot {
		t.Fatalf("ResolvedRoot が実体を指していない: got %q, want %q", mgr.ResolvedRoot(), resolvedRoot)
	}
}

// 目的: workspace.root を決められない設定では Manager を作れないことを確認する
// （置き場所が決まらないまま worktree を作ると、封じ込め検査そのものが成立しない）。
// 与える情報: workspace.root が空文字の設定。
// 成功条件: workspace.New がエラーを返すこと。
func TestNew_置き場所が空ならエラーになる(t *testing.T) {
	cfg := *config.DefaultConfig()
	cfg.Workspace.Root = ""

	if _, err := workspace.New(workspace.Options{Config: cfg, HomeDir: t.TempDir()}); err == nil {
		t.Fatal("workspace.root が空なのにエラーにならなかった")
	}
}

// 目的: workspace.identity_file がファイルの名前でなければ Manager を作れないことを確認する
// （設計 3-18 はこの値を「worktree の直下に置くファイルの名前」と定めている。
// パスの区切りを含む値だと身元ファイルが worktree の外へ書かれ、
// `info/exclude` へ書く行も `/../secret.json` になる）。
// 与える情報: `..` やパスの区切りを含む identity_file の値。
// 成功条件: workspace.New がすべてエラーを返すこと。
func TestNew_identity_fileがファイル名でなければエラーになる(t *testing.T) {
	for _, name := range []string{"../secret.json", "a/b.json", "..", ".", " .continuo.json", ""} {
		cfg := *config.DefaultConfig()
		cfg.Workspace.Root = filepath.Join(t.TempDir(), "worktrees")
		cfg.Workspace.IdentityFile = name

		if _, err := workspace.New(workspace.Options{Config: cfg, HomeDir: t.TempDir()}); err == nil {
			t.Fatalf("identity_file が %q なのにエラーにならなかった（worktree の外へ書ける）", name)
		}
	}
}

// 目的: issue ごとの設定ファイルの置き場所が相対パスなら Manager を作れないことを確認する
// （相対パスだと、片付けで settings_path がその内側かを判定できない）。
// 与える情報: 相対パスの SettingsRoot。
// 成功条件: workspace.New がエラーを返すこと。
func TestNew_設定ファイルの置き場所が相対パスならエラーになる(t *testing.T) {
	cfg := *config.DefaultConfig()
	cfg.Workspace.Root = filepath.Join(t.TempDir(), "worktrees")

	_, err := workspace.New(workspace.Options{
		Config:       cfg,
		HomeDir:      t.TempDir(),
		SettingsRoot: "issues",
	})
	if err == nil {
		t.Fatal("SettingsRoot が相対パスなのにエラーにならなかった")
	}
}
