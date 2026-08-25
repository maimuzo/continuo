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

// 目的: **正規化で名前が変わるリポジトリでも、ghq には実名をそのまま渡す**ことを
// 確認する（設計 3-7 の正規化は置き場所のディレクトリ名を作るためのものであり、
// ghq が要るのは GitHub に実在する名前そのものである）。
//
// **`<owner>/.github` は組織の community health 用リポジトリとして実在する。**
// これを `_github` に書き換えて問い合わせると、手元に clone があっても
// 永久に「ありません」になる。しかも人間へ出す案内は生の名前を埋めるので、
// **案内どおりに `ghq list -p -e <owner>/.github` を叩くと1行返り、
// continuo だけが「無い」と言い続ける。**
//
// 与える情報: 引数をファイルへ書き出すテスト用ghq mock と、リポジトリ名 `.github`。
// 成功条件: `list -p -e octocat/.github` の形で呼ばれていること。
func TestRunGhqList_正規化で変わる名前もそのまま渡す(t *testing.T) {
	out := filepath.Join(t.TempDir(), "args.txt")
	fakeGhq(t, "echo \"$@\" > "+out+"\necho /tmp/ghq/github.com/octocat/.github")

	path, err := workspace.RunGhqList(context.Background(), "octocat", ".github")
	if err != nil {
		t.Fatalf("RunGhqList に失敗した: %v", err)
	}
	if path != "/tmp/ghq/github.com/octocat/.github" {
		t.Fatalf("clone のパスが返っていない: %q", path)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("テスト用ghq mock が引数を書き出していない: %v", err)
	}
	if got := strings.TrimSpace(string(raw)); got != "list -p -e octocat/.github" {
		t.Fatalf("ghq へ渡した引数が違う: got %q, want %q", got, "list -p -e octocat/.github")
	}
}

// 目的: `ghq get` も同じく実名をそのまま渡すことを確認する。
//
// **`continuo trust` の案内が `ghq get` を叩く。**ここで書き換えると、
// 案内された対処そのものが「存在しないリポジトリ」を取りに行って失敗する。
//
// 与える情報: 引数をファイルへ書き出すテスト用ghq mock と、リポジトリ名 `.github`。
// 成功条件: `get octocat/.github` の形で呼ばれていること。
func TestRunGhqGet_正規化で変わる名前もそのまま渡す(t *testing.T) {
	out := filepath.Join(t.TempDir(), "args.txt")
	fakeGhq(t, "echo \"$@\" > "+out+"\nexit 0")

	if err := workspace.RunGhqGet(context.Background(), "octocat", ".github"); err != nil {
		t.Fatalf("取得に失敗した: %v", err)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("テスト用ghq mock が引数を書き出していない: %v", err)
	}
	if got := strings.TrimSpace(string(raw)); got != "get octocat/.github" {
		t.Fatalf("ghq へ渡した引数が違う: got %q, want %q", got, "get octocat/.github")
	}
}

// 目的: ghq へ渡せない形の名前は、**別名に直さずに断る**ことを確認する。
//
// **正規化をやめた代わりの守りである。**先頭が `-` の値をそのまま渡すと
// ghq のオプションとして解釈され、スラッシュや空白が入った値は
// 別のリポジトリを指す。**黙って直すと、また「無い」と言い続ける側へ戻る。**
//
// 与える情報: 引数をファイルへ書き出すテスト用ghq mock と、通してはならない名前。
// 成功条件: エラーになり、**ghq を1度も起動していない**こと。
func TestRunGhqList_通せない名前は起動せずに断る(t *testing.T) {
	for _, tc := range []struct {
		name  string
		owner string
		repo  string
	}{
		{name: "先頭がハイフン", owner: "octocat", repo: "-rf"},
		{name: "スラッシュを含む", owner: "octocat", repo: "hello/world"},
		{name: "空白を含む", owner: "octo cat", repo: "hello-world"},
		{name: "空文字", owner: "octocat", repo: ""},
		{name: "上の階層", owner: "octocat", repo: ".."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "args.txt")
			fakeGhq(t, "echo \"$@\" > "+out+"\nexit 0")

			if _, err := workspace.RunGhqList(context.Background(), tc.owner, tc.repo); err == nil {
				t.Fatalf("通してはならない名前を通した: %q/%q", tc.owner, tc.repo)
			}
			if _, err := os.Stat(out); err == nil {
				raw, _ := os.ReadFile(out)
				t.Fatalf("断ったはずなのに ghq を起動している: %q", strings.TrimSpace(string(raw)))
			}
		})
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
