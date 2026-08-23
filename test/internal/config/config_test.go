// Package config_test は internal/config の振る舞いを、公開 API（config.Load）を通して検証する。
// front matter の切り出し・展開規則の実装は internal/config パッケージ内部に閉じているため、
// ここでは実際に WORKFLOW.md 相当のファイルを一時ディレクトリへ書き、Load の結果を確認する
// ブラックボックステストとして書く（テストファイルは test/ 配下に internal/ と同じ構造で置く）。
package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// validFrontMatter は、検証を通すのに最低限必要な front matter である。
// tracker.provider.owner / project_number / status_field はデフォルト値を持たない
// 必須項目なので、テストのたびにここへ書き足す形にする。
//
// ここに herdr: ブロックを書かないのは、herdr.worktree.branch_template を上書きしたい
// テストが "herdr:" という同じ YAML トップレベルキーを2重に書くことになり
// （front matter の YAML としてキー重複エラーになる）、テストが書きにくくなるためである。
// herdr.socket は既定値（~/.config/herdr/herdr.sock）がそのまま使われる。
const validFrontMatter = "tracker:\n" +
	"  provider:\n" +
	"    owner: acme\n" +
	"    project_number: 1\n" +
	"    status_field: Status\n"

// writeWorkflow は front matter と本文を結合して WORKFLOW.md 相当のファイルを
// 一時ディレクトリに書き、そのパスを返す。
//
// t: テストコンテキスト。
// frontMatterYAML: front matter の YAML 本体。末尾は "\n" で終えること。
// body: 本文（プロンプトのテンプレート文字列）。空文字も許容する。
// 戻り値: 書き込んだファイルの絶対パス。
func writeWorkflow(t *testing.T, frontMatterYAML, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	content := "---\n" + frontMatterYAML + "---\n" + body
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("テスト用の WORKFLOW.md を書き込めません: %v", err)
	}
	return path
}

// 目的: front matter に未知のキーがあると起動を止める（設計 8-1 が仕様と意図的に逆にしている点）ことを確認する。
// 与える情報: 検証を通る front matter に、どのキーにも存在しない "foo_unknown_key" を混ぜたファイル。
// 成功条件: config.Load がエラーを返すこと。
func TestLoad_未知のキーがあるとエラーになる(t *testing.T) {
	front := validFrontMatter + "foo_unknown_key: 1\n"
	path := writeWorkflow(t, front, "本文\n")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("未知のキーがあるのにエラーが返らなかった")
	}
}

// 目的: front matter のネストしたキーに未知のものがあっても起動を止めることを確認する。
// 与える情報: tracker.provider の中に存在しないキー "unexpected_nested_key" を混ぜたファイル。
// 成功条件: config.Load がエラーを返すこと。
func TestLoad_ネストした未知のキーもエラーになる(t *testing.T) {
	front := "tracker:\n" +
		"  provider:\n" +
		"    owner: acme\n" +
		"    project_number: 1\n" +
		"    status_field: Status\n" +
		"    unexpected_nested_key: 1\n"
	path := writeWorkflow(t, front, "")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("ネストした未知のキーがあるのにエラーが返らなかった")
	}
}

// 目的: front matter と本文が正しく分かれることを確認する。
// 与える情報: front matter に有効な設定、本文に複数行・空行を含むテンプレート文字列を持つファイル。
// 成功条件: Loaded.PromptTemplate が本文と完全一致し、Loaded.Config に front matter の値が反映されていること。
func TestLoad_front_matterと本文が正しく分かれる(t *testing.T) {
	body := "Hello {{.issue.title}}\n\nSome content\n"
	path := writeWorkflow(t, validFrontMatter, body)

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}
	if loaded.PromptTemplate != body {
		t.Fatalf("本文が一致しない: got %q, want %q", loaded.PromptTemplate, body)
	}
	if loaded.Config.Tracker.Provider.Owner != "acme" {
		t.Fatalf("front matter の値が反映されていない: got %q, want %q", loaded.Config.Tracker.Provider.Owner, "acme")
	}
}

// 目的: 本文が空でも読めることを確認する（終端の "---" の直後でファイルが終わるケース）。
// 与える情報: front matter だけで本文が無い WORKFLOW.md 相当のファイル。
// 成功条件: config.Load がエラーにならず、Loaded.PromptTemplate が空文字であること。
func TestLoad_本文が空でも読める(t *testing.T) {
	path := writeWorkflow(t, validFrontMatter, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}
	if loaded.PromptTemplate != "" {
		t.Fatalf("本文が空であるべきなのに %q が返った", loaded.PromptTemplate)
	}
}

// 目的: 未定義の環境変数を参照すると設定エラーになり、空文字に落ちないことを確認する（設計 5-5）。
// 与える情報: workspace.root に、定義していない環境変数名 $CONTINUO_TEST_UNDEFINED_XYZ を書いた front matter。
// 成功条件: config.Load がエラーを返し、エラーメッセージに設定キー名（workspace.root）と
// 環境変数名（CONTINUO_TEST_UNDEFINED_XYZ）の両方が含まれること。
func TestLoad_未定義の環境変数はエラーになる(t *testing.T) {
	front := validFrontMatter + "workspace:\n  root: $CONTINUO_TEST_UNDEFINED_XYZ\n"
	path := writeWorkflow(t, front, "")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("未定義の環境変数を参照しているのにエラーが返らなかった")
	}
	msg := err.Error()
	if !strings.Contains(msg, "workspace.root") {
		t.Errorf("エラーメッセージに設定キー名が含まれていない: %q", msg)
	}
	if !strings.Contains(msg, "CONTINUO_TEST_UNDEFINED_XYZ") {
		t.Errorf("エラーメッセージに環境変数名が含まれていない: %q", msg)
	}
}

// 目的: 環境変数は定義されていても値が空文字ならエラーになることを確認する（5-5 の
// 「設定されているが空」もエラーにする、という規則）。
// 与える情報: workspace.root に、値を空文字にした環境変数 $CONTINUO_TEST_EMPTY を書いた front matter。
// 成功条件: config.Load がエラーを返すこと。
func TestLoad_定義されているが空の環境変数もエラーになる(t *testing.T) {
	t.Setenv("CONTINUO_TEST_EMPTY", "")
	front := validFrontMatter + "workspace:\n  root: $CONTINUO_TEST_EMPTY\n"
	path := writeWorkflow(t, front, "")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("値が空文字の環境変数を参照しているのにエラーが返らなかった")
	}
}

// 目的: $NAME / ${NAME} / $$ 以外の "$" の使い方は設定エラーになることを確認する。
// os.Expand であれば "price is $100" は "price is 00" に化けてしまうため、
// それを許さないことを検証する（5-5 が明示的に禁止している例そのもの）。
// 与える情報: workspace.root に "price is $100" を書いた front matter。
// 成功条件: config.Load がエラーを返すこと。
func TestLoad_ドル記号のあとが変数名として不正だとエラーになる(t *testing.T) {
	front := validFrontMatter + "workspace:\n  root: \"price is $100\"\n"
	path := writeWorkflow(t, front, "")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("\"price is $100\" はエラーになるべきなのにエラーが返らなかった")
	}
}

// 目的: "$$" がリテラルの "$" 1文字に展開されることを確認する。
// 与える情報: workspace.root に "literal $$ dollar" を書いた front matter。
// 成功条件: config.Load が成功し、展開後の値が "literal $ dollar" になっていること。
func TestLoad_ドル記号が2つ続くとリテラルのドル記号になる(t *testing.T) {
	front := validFrontMatter + "workspace:\n  root: \"literal $$ dollar\"\n"
	path := writeWorkflow(t, front, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}
	// workspace.root は相対パスなので、展開のあとに WORKFLOW.md の置き場所を基準に
	// 絶対パスへ解決される（設計 5-1）。ここで見たいのは "$$" が "$" になることである。
	want := filepath.Join(filepath.Dir(path), "literal $ dollar")
	if loaded.Config.Workspace.Root != want {
		t.Fatalf("展開結果が一致しない: got %q, want %q", loaded.Config.Workspace.Root, want)
	}
}

// 目的: "${" が "}" で閉じられていない場合にエラーになることを確認する。
// 与える情報: workspace.root に "${UNCLOSED" を書いた front matter。
// 成功条件: config.Load がエラーを返すこと。
func TestLoad_閉じられていない波括弧はエラーになる(t *testing.T) {
	front := validFrontMatter + "workspace:\n  root: \"${UNCLOSED\"\n"
	path := writeWorkflow(t, front, "")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("\"${UNCLOSED\" はエラーになるべきなのにエラーが返らなかった")
	}
}

// 目的: チルダの展開が先頭の "~" または "~/" だけに限られ、ホームディレクトリへ正しく
// 展開されることを確認する。
// 与える情報: workspace.root に "~/sub/dir" を書いた front matter。
// 成功条件: config.Load が成功し、展開後の値がホームディレクトリ配下の "sub/dir" になっていること。
func TestLoad_チルダは先頭だけ展開される(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("ホームディレクトリを取得できないためスキップする: %v", err)
	}

	front := validFrontMatter + "workspace:\n  root: \"~/sub/dir\"\n"
	path := writeWorkflow(t, front, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}
	want := home + "/sub/dir"
	if loaded.Config.Workspace.Root != want {
		t.Fatalf("チルダの展開結果が一致しない: got %q, want %q", loaded.Config.Workspace.Root, want)
	}
}

// 目的: "~user" 形式（他ユーザーのホームディレクトリを指す形式）はサポートせず、エラーに
// なることを確認する。
// 与える情報: workspace.root に "~someuser/dir" を書いた front matter。
// 成功条件: config.Load がエラーを返すこと。
func TestLoad_チルダユーザー形式はエラーになる(t *testing.T) {
	front := validFrontMatter + "workspace:\n  root: \"~someuser/dir\"\n"
	path := writeWorkflow(t, front, "")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("\"~someuser/dir\" はエラーになるべきなのにエラーが返らなかった")
	}
}

// 目的: 展開を適用しないキー（herdr.worktree.branch_template）には $ や ~ があっても
// 一切手を加えないことを確認する（5-5 の「適用しないキー」の一覧）。
// 与える情報: herdr.worktree.branch_template にテンプレート変数 "{{.issue.owner}}" と
// 展開対象になりうる文字列を含む front matter。
// 成功条件: config.Load が成功し、branch_template の値がまったく変化していないこと
// （$ を含んでいてもエラーにならないことも含めて確認する）。
func TestLoad_branch_templateには展開を適用しない(t *testing.T) {
	front := validFrontMatter + "herdr:\n  worktree:\n    branch_template: \"continuo/{{.issue.owner}}/$literal/not-expanded\"\n"
	path := writeWorkflow(t, front, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("展開対象外のキーなのにエラーになった: %v", err)
	}
	want := "continuo/{{.issue.owner}}/$literal/not-expanded"
	if loaded.Config.Herdr.Worktree.BranchTemplate != want {
		t.Fatalf("branch_template が変化してしまった: got %q, want %q", loaded.Config.Herdr.Worktree.BranchTemplate, want)
	}
}

// 目的: WORKFLOW.md が読めない場合（存在しないパス）にエラーになることを確認する。
// 与える情報: 存在しないファイルパス。
// 成功条件: config.Load がエラーを返すこと。
func TestLoad_ファイルが存在しないとエラーになる(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "not-exist.md"))
	if err == nil {
		t.Fatal("存在しないファイルなのにエラーが返らなかった")
	}
}

// 目的: front matter の開始行が "---" でない場合にエラーになることを確認する。
// 与える情報: 1行目が "---" ではないファイル。
// 成功条件: config.Load がエラーを返すこと。
func TestLoad_開始の区切り行が無いとエラーになる(t *testing.T) {
	path := filepath.Join(t.TempDir(), "WORKFLOW.md")
	if err := os.WriteFile(path, []byte("tracker:\n  kind: github_projects_v2\n"), 0o600); err != nil {
		t.Fatalf("テスト用ファイルを書き込めません: %v", err)
	}

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("開始の区切り行が無いのにエラーが返らなかった")
	}
}

// 目的: front matter の終端の区切り行が無い場合にエラーになることを確認する。
// 与える情報: 開始の "---" はあるが、終端の "---" が無いファイル。
// 成功条件: config.Load がエラーを返すこと。
func TestLoad_終端の区切り行が無いとエラーになる(t *testing.T) {
	path := filepath.Join(t.TempDir(), "WORKFLOW.md")
	if err := os.WriteFile(path, []byte("---\ntracker:\n  kind: github_projects_v2\n"), 0o600); err != nil {
		t.Fatalf("テスト用ファイルを書き込めません: %v", err)
	}

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("終端の区切り行が無いのにエラーが返らなかった")
	}
}

// 目的: ResolvePath が CLI の位置引数を最優先し、無指定なら作業ディレクトリの
// WORKFLOW.md を使うことを確認する（設計 5-1 の探索順）。
// 与える情報: 位置引数ありの場合・無しの場合それぞれの workDir。
// 成功条件: 位置引数ありなら workDir 基準の絶対パスに解決され、無指定なら
// "<workDir>/WORKFLOW.md" になること。
func TestResolvePath_位置引数を優先し無指定ならWORKFLOW_mdを探す(t *testing.T) {
	workDir := "/home/example/project"

	gotWithArg, err := config.ResolvePath("custom.md", workDir)
	if err != nil {
		t.Fatalf("位置引数ありの解決に失敗した: %v", err)
	}
	wantWithArg := filepath.Join(workDir, "custom.md")
	if gotWithArg != wantWithArg {
		t.Errorf("位置引数ありの解決結果が一致しない: got %q, want %q", gotWithArg, wantWithArg)
	}

	gotDefault, err := config.ResolvePath("", workDir)
	if err != nil {
		t.Fatalf("無指定の解決に失敗した: %v", err)
	}
	wantDefault := filepath.Join(workDir, config.DefaultFileName)
	if gotDefault != wantDefault {
		t.Errorf("無指定の解決結果が一致しない: got %q, want %q", gotDefault, wantDefault)
	}
}
