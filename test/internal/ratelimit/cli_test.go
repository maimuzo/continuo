// Package ratelimit_test のうち、このファイルは `continuo allow-keychain-access` を
// 実際に起動して、端から端まで通ることを確かめる。
//
// **本物の `security` は1回も起動しない。**PATH の先頭にテスト用security mock を置く。
// **本物のホームディレクトリも渡さない。**環境変数は明示的に組み立てる。
package ratelimit_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/test/testlang"
)

// buildContinuo は `continuo` をビルドする。
//
// **リポジトリの中には出力しない**（生成物を残さないため、テストの一時ディレクトリへ出す）。
//
// t: 呼び出し元のテスト。
// outDir: 出力先のディレクトリ。
// 戻り値: ビルドしたバイナリの絶対パス。
func buildContinuo(t *testing.T, outDir string) string {
	t.Helper()

	goBin, err := exec.LookPath("go")
	if err != nil {
		goBin = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("リポジトリの場所を決められません: %v", err)
	}

	bin := filepath.Join(outDir, "continuo")
	cmd := exec.Command(goBin, "build", "-o", bin, "./cmd/continuo")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("continuo をビルドできません: %v\n%s", err, out)
	}
	return bin
}

// writeSecurityMock はテスト用security mock を1つ作り、その置き場所を返す。
//
// t: 呼び出し元のテスト。
// script: `security` として実行させるシェルスクリプトの中身（`#!/bin/sh` の次の行から）。
// 戻り値: 実行ファイルを置いたディレクトリの絶対パス。
func writeSecurityMock(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "security"), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("テスト用security mock を書けません: %v", err)
	}
	return dir
}

// runAllowKeychainAccess は `continuo allow-keychain-access` を起動する。
//
// t: 呼び出し元のテスト。
// bin: ビルドしたバイナリの絶対パス。
// pathDir: PATH の先頭へ置くディレクトリ（テスト用security mock の置き場所）。
// args: サブコマンド名のあとに渡す引数。
// 戻り値の1つ目: 標準出力と標準エラーを連結した出力。
// 戻り値の2つ目: 終了コード。
func runAllowKeychainAccess(t *testing.T, bin, pathDir string, args ...string) (string, int) {
	t.Helper()

	cmd := exec.Command(bin, append([]string{"allow-keychain-access"}, args...)...)
	cmd.Dir = t.TempDir()
	cmd.Env = []string{
		"PATH=" + pathDir + string(os.PathListSeparator) + "/usr/bin:/bin",
		"HOME=" + t.TempDir(),
		testlang.EnvEntry(),
	}
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("continuo を起動できません: %v\n%s", err, out)
		}
		code = exitErr.ExitCode()
	}
	return string(out), code
}

// 目的: 位置引数を受け付けないことを確認する（引数の指定の誤りは終了コード 2）。
// 与える情報: 位置引数を1つ付けた実行。
// 成功条件: 終了コードが 2 で、受け付けないことが出力に出ること。
func TestCLI_allow_keychain_accessは位置引数を受け付けない(t *testing.T) {
	bin := buildContinuo(t, t.TempDir())
	dir := writeSecurityMock(t, "exit 0")

	out, code := runAllowKeychainAccess(t, bin, dir, "余計な引数")

	if code != 2 {
		t.Fatalf("引数の指定が誤っているのに終了コードが %d だった:\n%s", code, out)
	}
	if !strings.Contains(out, "位置引数") {
		t.Fatalf("何が誤っているかが出力に出ていない:\n%s", out)
	}
}

// 目的: macOS 以外では「意味がありません」と出して終了コード 0 で終わることを確認する。
//
// **失敗として扱わない。**前提が違うだけであり、CI を落とす理由が無い。
//
// 与える情報: macOS 以外での実行。
// 成功条件: 終了コードが 0 で、macOS でだけ意味があることが出力に出ること。
func TestCLI_allow_keychain_accessはmacOS以外では何もしない(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS 以外の振る舞いを見るテストである（いまの OS: darwin）")
	}
	bin := buildContinuo(t, t.TempDir())
	dir := writeSecurityMock(t, "exit 0")

	out, code := runAllowKeychainAccess(t, bin, dir)

	if code != 0 {
		t.Fatalf("macOS 以外なのに終了コードが %d だった:\n%s", code, out)
	}
	if !strings.Contains(out, "macOS") {
		t.Fatalf("macOS でだけ意味があることが出力に出ていない:\n%s", out)
	}
}

// 目的: 読めたときに、先に案内を出し、読めた項目の**名前だけ**を出すことを確認する。
//
// **値（トークン）を出してはならない。**端末とスクロールバッファに残る。
//
// 与える情報: 資格情報の JSON を返すテスト用security mock。
// 成功条件: 終了コードが 0。実行前の案内（「常に許可」）と、読めた項目の名前が出ること。
// **トークンの値が1回も出ないこと。**
func TestCLI_allow_keychain_accessは読めたら項目の名前だけを出す(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skipf("Keychain を読む経路は macOS でだけ意味がある（いまの OS: %s）", runtime.GOOS)
	}
	bin := buildContinuo(t, t.TempDir())
	dir := writeSecurityMock(t, `printf '%s' '{"claudeAiOauth":{"accessToken":"`+keychainTestToken+`","scopes":["a"]}}'`)

	out, code := runAllowKeychainAccess(t, bin, dir)

	if code != 0 {
		t.Fatalf("読めたのに終了コードが %d だった:\n%s", code, out)
	}
	if !strings.Contains(out, "常に許可") {
		t.Fatalf("実行前の案内（ダイアログで何を選ぶか）が出ていない:\n%s", out)
	}
	for _, want := range []string{"accessToken", "scopes", ratelimit.KeychainService} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q が無い:\n%s", want, out)
		}
	}
	if strings.Contains(out, keychainTestToken) {
		t.Fatalf("トークンの値が出力に出ている:\n%s", out)
	}
}

// 目的: 読めなかったときに、原因と対処を書いて終了コード 1 で終わることを確認する
// （設計 3-34b の形）。
//
// 与える情報: 標準エラーへ理由を書いて異常終了するテスト用security mock。
// 成功条件: 終了コードが 1。【確かめ方】【よくある原因】【対処】がすべて出ること。
func TestCLI_allow_keychain_accessは読めなければ原因と対処を出す(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skipf("Keychain を読む経路は macOS でだけ意味がある（いまの OS: %s）", runtime.GOOS)
	}
	bin := buildContinuo(t, t.TempDir())
	dir := writeSecurityMock(t,
		"echo 'security: The specified item could not be found in the keychain.' >&2\nexit 44")

	out, code := runAllowKeychainAccess(t, bin, dir)

	if code != 1 {
		t.Fatalf("読めなかったのに終了コードが %d だった:\n%s", code, out)
	}
	for _, want := range []string{"【確かめ方】", "【よくある原因】", "【対処】", "could not be found in the keychain"} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q が無い（次に何をすればよいか分からない）:\n%s", want, out)
		}
	}
}
