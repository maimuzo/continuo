// Package cli_test のうち、このファイルは `continuo github-app token` を確かめる
// （docs/plans/impl/issue245_github_app_attribution.md の 3-82d「`continuo github-app token` の輪郭」）。
//
// **本物の GitHub は1回も叩かない。**トークンを取る処理は `cli.Deps.GitHubAppToken` から偽物へ向ける。
// **本物のホームも見ない。**`cli.Deps.UserHomeDir` を一時ディレクトリへ向ける。
package cli_test

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/cli"
	"github.com/maimuzo/continuo/internal/daemon"
	"github.com/maimuzo/continuo/internal/githubapp"
)

// fakeGitHubAppToken は `continuo github-app token` の差し替え口を、固定の応答で埋める。
//
// t: 呼び出し元のテスト。
// token: 返すトークン。
// err: 返すエラー（nil なら token を返す）。
// 戻り値の1つ目: 差し替え済みの Deps（ホームも一時ディレクトリへ向けてある）。
// 戻り値の2つ目: 向けたホーム。
// 戻り値の3つ目: 呼ばれた回数と、渡された Store のパスを受ける入れ物。
func fakeGitHubAppToken(t *testing.T, token string, err error) (cli.Deps, string, *struct {
	Calls     int
	StorePath string
}) {
	t.Helper()
	deps, home := fakeHome(t)
	got := &struct {
		Calls     int
		StorePath string
	}{}
	deps.GitHubAppToken = func(_ context.Context, store githubapp.Store, _ *slog.Logger) (string, error) {
		got.Calls++
		got.StorePath = store.Path()
		if err != nil {
			return "", err
		}
		return token, nil
	}
	return deps, home, got
}

// 目的: `github-app token` が、取れたトークンを標準出力へ1行だけ出して 0 で終わることを確認する。
//
// **標準出力はトークン1行だけである。**エージェントは `TOKEN=$(continuo github-app token)` で受けるので、
// 1文字でも余計に出ると `GH_TOKEN` が壊れる。**標準エラーへはトークンを1文字も出さない。**
//
// 与える情報: `ghu_test_token` を返す差し替え。
// 成功条件: 終了コードが 0、stdout が `ghu_test_token\n` そのもの、stderr にトークンが無く、
// 渡った Store が `<ホーム>/.continuo/github-app-credentials.json` を指すこと。
func TestRunGitHubApp_トークンを標準出力へ1行だけ出す(t *testing.T) {
	deps, home, got := fakeGitHubAppToken(t, "ghu_test_token", nil)

	code, stdout, stderr := runCLIWith(deps, []string{"github-app", "token"}, "")
	if code != 0 {
		t.Fatalf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	if stdout != "ghu_test_token\n" {
		t.Fatalf("標準出力がトークン1行だけでない: %q", stdout)
	}
	if strings.Contains(stderr, "ghu_test_token") {
		t.Fatalf("標準エラーへトークンを出している: %q", stderr)
	}
	if got.Calls != 1 {
		t.Fatalf("トークンを取る処理を %d 回呼んだ（1回のはず）", got.Calls)
	}
	want := filepath.Join(home, ".continuo", "github-app-credentials.json")
	if got.StorePath != want {
		t.Fatalf("資格情報の置き場所が UserHomeDir から引かれていない: got %q, want %q", got.StorePath, want)
	}
}

// 目的: トークンを取れなければ、理由を標準エラーへ出し、標準出力には何も出さず、1 で終わることを確認する。
//
// **`gh auth token` へは落ちない。**落ちると attribution の無い投稿が黙って通る。
// **終了コードは 1 の1種類だけである**（3-82d の「終了コードは2通りだけにする」）。
//
// 与える情報: `ErrNotFound` を包んだエラーを返す差し替え。
// 成功条件: 終了コードが 1、stdout が空、stderr に「GitHub App のトークンを取れません」と元の理由が入ること。
func TestRunGitHubApp_取れなければ理由を標準エラーへ出して1で終わる(t *testing.T) {
	deps, _, _ := fakeGitHubAppToken(t, "", errors.New("GitHub App の資格情報がありません（テストの理由）"))

	code, stdout, stderr := runCLIWith(deps, []string{"github-app", "token"}, "")
	if code != 1 {
		t.Fatalf("終了コードが 1 でない: %d", code)
	}
	if stdout != "" {
		t.Fatalf("失敗したのに標準出力へ何か出している: %q", stdout)
	}
	for _, want := range []string{"GitHub App のトークンを取れません", "テストの理由"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("標準エラーに %q が入っていない: %q", want, stderr)
		}
	}
}

// 目的: `token` 以外の引数では、使い方を標準エラーへ出して 1 で終わり、トークンを取りに行かないことを確認する。
//
// **2 を返さない。**他のサブコマンドの引数の誤りは 2 だが、このサブコマンドは
// エージェントが終了コードを見分けないように 1 の1種類にする（3-82d）。`--help` も同じである。
//
// 与える情報: 語が無い・知らない語・語が多い・`--help`・`-h`。
// 成功条件: すべて終了コードが 1 で、stderr に `continuo github-app token` が入り、stdout が空で、
// トークンを取る処理が1回も呼ばれないこと。
func TestRunGitHubApp_token以外は使い方を出して1で終わる(t *testing.T) {
	for _, args := range [][]string{
		{"github-app"},
		{"github-app", "foo"},
		{"github-app", "token", "extra"},
		{"github-app", "--help"},
		{"github-app", "-h"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			deps, _, got := fakeGitHubAppToken(t, "ghu_never", nil)

			code, stdout, stderr := runCLIWith(deps, args, "")
			if code != 1 {
				t.Errorf("終了コードが 1 でない: %d", code)
			}
			if !strings.Contains(stderr, "continuo github-app token") {
				t.Errorf("使い方が標準エラーに出ていない: %q", stderr)
			}
			if stdout != "" {
				t.Errorf("標準出力へ何か出している: %q", stdout)
			}
			if got.Calls != 0 {
				t.Errorf("引数が誤っているのにトークンを取りに行った（%d 回）", got.Calls)
			}
		})
	}
}

// 目的: ホームディレクトリを引けなければ、トークンを取りに行かずに 1 で終わることを確認する。
//
// 与える情報: エラーを返す `UserHomeDir`。
// 成功条件: 終了コードが 1、stderr に「ホームディレクトリ」が入り、トークンを取る処理が呼ばれないこと。
func TestRunGitHubApp_ホームを引けなければ1で終わる(t *testing.T) {
	calls := 0
	deps := cli.Deps{
		UserHomeDir: func() (string, error) { return "", errors.New("HOME が空です") },
		GitHubAppToken: func(_ context.Context, _ githubapp.Store, _ *slog.Logger) (string, error) {
			calls++
			return "ghu_never", nil
		},
	}

	code, stdout, stderr := runCLIWith(deps, []string{"github-app", "token"}, "")
	if code != 1 {
		t.Fatalf("終了コードが 1 でない: %d", code)
	}
	if stdout != "" {
		t.Fatalf("標準出力へ何か出している: %q", stdout)
	}
	if !strings.Contains(stderr, "ホームディレクトリ") {
		t.Fatalf("理由が標準エラーに出ていない: %q", stderr)
	}
	if calls != 0 {
		t.Fatalf("ホームが無いのにトークンを取りに行った")
	}
}

// 目的: 常駐の起動が、`UserHomeDir` で引いたホームを `daemon.Options.HomeDir` へ渡すことを確認する。
//
// **渡さないと、daemon が `os.UserHomeDir()` へ落ち、`internal/cli` の差し替え口が
// GitHub App の資格情報には効かなくなる**（3-82b「ホームディレクトリは、差し替えられる関数から引く」）。
//
// 与える情報: 一時ディレクトリを返す `UserHomeDir` と、受け取った Options を記録する `DaemonRun`。
// 成功条件: `HomeDir` がその一時ディレクトリで、`GHLogin` は nil（既定の `gh api user` に任せる）であること。
func TestRunMain_ホームをdaemonのHomeDirへ渡す(t *testing.T) {
	tempCLIHome(t)
	deps, home := fakeHome(t)

	var gotHome string
	var gotGHLoginSet bool
	deps.DaemonRun = func(_ context.Context, opts daemon.Options) error {
		gotHome = opts.HomeDir
		gotGHLoginSet = opts.GHLogin != nil
		return nil
	}

	code, _, stderr := runCLIWith(deps, []string{writeWorkflowFor(t)}, "")
	if code != 0 {
		t.Fatalf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	if gotHome != home {
		t.Fatalf("HomeDir が UserHomeDir の結果でない: got %q, want %q", gotHome, home)
	}
	if gotGHLoginSet {
		t.Fatalf("CLI が GHLogin を渡している（nil で daemon の既定に任せるはず）")
	}
}
