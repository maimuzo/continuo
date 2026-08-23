package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/workspace"
)

// fakeGhq は PATH の先頭に置く偽の `ghq` を作る。
//
// **本物の ghq を実行しない。**終了コードと出力を決め打ちにしないと、
// 「該当が無い」と「ghq 自身が壊れている」を区別できることを確かめられない。
//
// t: 呼び出し元のテスト。PATH の差し替えを t.Setenv で行う（後始末は testing が行う）。
// script: `ghq` として実行させるシェルスクリプトの中身（`#!/bin/sh` の次の行から）。
func fakeGhq(t *testing.T, script string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "ghq")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatalf("テスト用ghq mock を書けない: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// 目的: 「clone が無い」ときだけ空文字を返すことを確認する（設計 3-22 / 3-6）。
// 与える情報: 何も出力せず終了コード 1 で終わる ghq（該当が無いときの振る舞い）。
// 成功条件: 空文字が返り、エラーにならないこと。
func TestRunGhqList_該当が無ければ空文字を返す(t *testing.T) {
	fakeGhq(t, "exit 1")

	path, err := workspace.RunGhqList(context.Background(), "octocat", "hello-world")
	if err != nil {
		t.Fatalf("該当が無いだけでエラーになった: %v", err)
	}
	if path != "" {
		t.Fatalf("該当が無いのにパスが返った: %q", path)
	}
}

// 目的: 該当が無いこと**以外**の失敗を「clone が無い」に丸めないことを確認する。
//
// **丸めると、設定の誤りや ghq 自身の異常が「clone を作りに行け」という誤った案内になり、
// 原因も残らない**（標準エラー出力は捨てられる）。
//
// 与える情報: 標準エラー出力に理由を書いて終了コード 2 で終わる ghq。
// 成功条件: エラーになり、そのエラー文に終了コードと標準エラー出力が入っていること。
func TestRunGhqList_該当なし以外の失敗はエラーにする(t *testing.T) {
	fakeGhq(t, "echo '設定ファイルが壊れています' >&2; exit 2")

	path, err := workspace.RunGhqList(context.Background(), "octocat", "hello-world")
	if err == nil {
		t.Fatalf("ghq が異常終了したのにエラーにならなかった（clone が無いことに丸めている）: %q", path)
	}
	if !strings.Contains(err.Error(), "設定ファイルが壊れています") {
		t.Fatalf("エラー文に標準エラー出力が入っていない（原因が残らない）: %v", err)
	}
	if !strings.Contains(err.Error(), "2") {
		t.Fatalf("エラー文に終了コードが入っていない: %v", err)
	}
}

// 目的: clone が見つかったときは、その絶対パスを返すことを確認する。
// 与える情報: パスを1行出力して 0 で終わる ghq。
// 成功条件: そのパスが返ること。
func TestRunGhqList_cloneのパスを返す(t *testing.T) {
	fakeGhq(t, "echo /tmp/ghq/github.com/octocat/hello-world")

	path, err := workspace.RunGhqList(context.Background(), "octocat", "hello-world")
	if err != nil {
		t.Fatalf("RunGhqList に失敗した: %v", err)
	}
	if path != "/tmp/ghq/github.com/octocat/hello-world" {
		t.Fatalf("clone のパスが返っていない: %q", path)
	}
}

// 目的: `ghq get` を実際に起動し、引数がそのまま渡ることを確認する（設計 3-22）。
// 与える情報: 引数をファイルへ書き出すテスト用ghq mock。
// 成功条件: エラーにならず、`get <owner>/<repo>` の形で呼ばれていること。
func TestRunGhqGet_引数をそのまま渡して起動する(t *testing.T) {
	out := filepath.Join(t.TempDir(), "args.txt")
	fakeGhq(t, "echo \"$@\" > "+out+"\nexit 0")

	if err := workspace.RunGhqGet(context.Background(), "octocat", "hello-world"); err != nil {
		t.Fatalf("取得に失敗した: %v", err)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("テスト用ghq mock が引数を書き出していない: %v", err)
	}
	if got := strings.TrimSpace(string(raw)); got != "get octocat/hello-world" {
		t.Fatalf("ghq へ渡した引数が違う: got %q, want %q", got, "get octocat/hello-world")
	}
}

// 目的: `ghq get` が失敗したとき、標準エラー出力をエラー文に含めることを確認する。
// **失敗の理由が消えると、人間は「取れませんでした」だけを見て原因を探せない。**
// 与える情報: 標準エラーへ理由を出して終了コード 1 で終わるテスト用ghq mock。
// 成功条件: エラーが返り、その文面に終了コードと標準エラーの内容が入っていること。
func TestRunGhqGet_失敗したら理由をエラー文に含める(t *testing.T) {
	fakeGhq(t, "echo 'repository not found' >&2\nexit 1")

	err := workspace.RunGhqGet(context.Background(), "octocat", "no-such-repo")
	if err == nil {
		t.Fatal("失敗したのにエラーが返っていない")
	}
	if !strings.Contains(err.Error(), "repository not found") {
		t.Errorf("標準エラーの内容がエラー文に入っていない: %v", err)
	}
	if !strings.Contains(err.Error(), "終了コード 1") {
		t.Errorf("終了コードがエラー文に入っていない: %v", err)
	}
}

// 目的: `ghq` が PATH に無いとき、起動できなかったと分かる文面になることを確認する。
// 与える情報: PATH を空にした状態。
// 成功条件: エラーが返り、「起動できません」を含むこと。
func TestRunGhqGet_ghqが無ければ起動できないと言う(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	err := workspace.RunGhqGet(context.Background(), "octocat", "hello-world")
	if err == nil {
		t.Fatal("ghq が無いのにエラーが返っていない")
	}
	if !strings.Contains(err.Error(), "起動できません") {
		t.Errorf("起動できなかったことが分かる文面になっていない: %v", err)
	}
}
