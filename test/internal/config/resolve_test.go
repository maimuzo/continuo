// Package config_test のうち、このファイルは「どのパスを基準に読むか」と
// 「front matter をどこで切るか」を検証する。
//
// どちらも、間違っていても起動は通ってしまう種類の挙動である。worktree が思わぬ場所に
// できる・書いたつもりの設定が本文へ落ちる、という形で後から効いてくるので、
// 期待する挙動をここで固定する。
package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// 目的: workspace.root に相対パスを書いたとき、WORKFLOW.md が置かれているディレクトリを
// 基準に絶対パスへ解決されることを確認する（設計 5-1。SPEC.md 5.3.3 / 5.4）。
// 解決しないと workspace.EnsureRoot が「絶対パスではありません」で起動を止めてしまい、
// 仕様どおりに書いた WORKFLOW.md が動かない。
// 与える情報: workspace.root に "worktrees" と書いた front matter。
// 成功条件: 読み込んだ workspace.root が <WORKFLOW.md の置き場所>/worktrees になっていること。
func TestLoad_相対パスのworkspace_rootはWORKFLOW_mdの置き場所を基準に解決される(t *testing.T) {
	front := validFrontMatter + "workspace:\n  root: worktrees\n"
	path := writeWorkflow(t, front, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}

	want := filepath.Join(filepath.Dir(path), "worktrees")
	if loaded.Config.Workspace.Root != want {
		t.Fatalf("相対パスが WORKFLOW.md の置き場所を基準に解決されていない: got %q, want %q",
			loaded.Config.Workspace.Root, want)
	}
}

// 目的: 絶対パスで書いた workspace.root は、解決の対象にならずそのまま残ることを確認する。
// 与える情報: workspace.root に絶対パスを書いた front matter。
// 成功条件: 読み込んだ workspace.root が書いたとおりの絶対パスであること。
func TestLoad_絶対パスのworkspace_rootはそのまま残る(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "somewhere", "worktrees")
	front := validFrontMatter + "workspace:\n  root: " + abs + "\n"
	path := writeWorkflow(t, front, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}
	if loaded.Config.Workspace.Root != abs {
		t.Fatalf("絶対パスが書き換わっている: got %q, want %q", loaded.Config.Workspace.Root, abs)
	}
}

// 目的: WORKFLOW.md のパスを相対パスで渡すと Load が起動を止めることを確認する。
// 相対パスだと「設定に書いた相対パスを何を基準に解決するか」が起動ディレクトリ依存になり、
// worktree の置き場所が実行のたびに変わってしまう（5-1）。
// 与える情報: 相対パスの "WORKFLOW.md"。
// 成功条件: config.Load がエラーを返し、その文に渡したパスが含まれること。
func TestLoad_WORKFLOW_mdのパスが相対だとエラーになる(t *testing.T) {
	_, err := config.Load("WORKFLOW.md")
	if err == nil {
		t.Fatal("相対パスを渡したのにエラーが返らなかった")
	}
	if !strings.Contains(err.Error(), "WORKFLOW.md") {
		t.Fatalf("エラー文に渡したパスが含まれていない: %v", err)
	}
}

// 目的: front matter の中でブロックスカラー（|）を使い、その中に "---" を書いても
// front matter がそこで切れないことを確認する。
// 終端と判定するのは「行頭から '---' だけの行」である。ブロックスカラーの中身は必ず
// インデントされるので、インデントされた "---" は終端にならない。
// 与える情報: workspace_hooks.after_create にブロックスカラーで3行を書き、
// 真ん中にインデントした "---" を置いた front matter。
// 成功条件: config.Load が成功し、after_create に "---" を含む3行が丸ごと残っていること。
// 本文にはブロックスカラーの後半が漏れていないこと。
func TestLoad_ブロックスカラーの中のインデントされた区切り行では切れない(t *testing.T) {
	front := validFrontMatter +
		"workspace_hooks:\n" +
		"  after_create: |\n" +
		"    echo one\n" +
		"    ---\n" +
		"    echo two\n"
	path := writeWorkflow(t, front, "本文\n")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}
	if loaded.Config.WorkspaceHooks.AfterCreate == nil {
		t.Fatal("workspace_hooks.after_create が読めていない")
	}
	got := *loaded.Config.WorkspaceHooks.AfterCreate
	// splitFrontMatter は行で切って join するため、front matter の最後の行の改行は残らない。
	want := "echo one\n---\necho two"
	if got != want {
		t.Fatalf("ブロックスカラーの中身が切れている: got %q, want %q", got, want)
	}
	if strings.Contains(loaded.PromptTemplate, "echo two") {
		t.Fatalf("ブロックスカラーの後半が本文へ漏れている: %q", loaded.PromptTemplate)
	}
}

// 目的: 行頭の "---" は、front matter の中に書かれていてもそこで終端になることを確認する。
// splitFrontMatter の制約（GoDoc に明記した）をテストで固定し、あとから読む人が
// 「どこで切れるのか」を実物で確かめられるようにする。
// 与える情報: front matter の途中に行頭から "---" を書き、その後ろに設定を続けたファイル。
// 成功条件: 前半だけが front matter として読まれ、後半（polling.interval_ms）は本文に落ちる。
// つまり書いたつもりの polling.interval_ms は効かず、既定値のままになる。
func TestLoad_行頭の区切り行はfront_matterの中でも終端になる(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	content := "---\n" + validFrontMatter + "---\npolling:\n  interval_ms: 12345\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("テスト用の WORKFLOW.md を書き込めません: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}
	if loaded.Config.Polling.IntervalMs == 12345 {
		t.Fatal("行頭の \"---\" より後ろが front matter として読まれてしまった")
	}
	if !strings.Contains(loaded.PromptTemplate, "interval_ms: 12345") {
		t.Fatalf("行頭の \"---\" より後ろが本文になっていない: %q", loaded.PromptTemplate)
	}
}
