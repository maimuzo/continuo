// Package ratelimit_test のうち、このファイルは `continuo allow-keychain-access` を
// 実際に起動して、端から端まで通ることを確かめるための補助関数を置く。
// テストは、ユースケース記述の名前のファイル（`Keychainの読み取りを許可する_test.go`）に在る。
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
	"testing"

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
