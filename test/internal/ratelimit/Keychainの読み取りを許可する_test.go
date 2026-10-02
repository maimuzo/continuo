// {"RUCM-CFG-SHA256": "f15436ef1f4b32a8a33d8ea1b790521660aeec3b550b492f47f24dce283f64ba", "SOURCE": "docs/spec/usecases/particular_case/Keychainの読み取りを許可する.cfg.json"}
//
// **ユースケース記述「Keychainの読み取りを許可する」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package ratelimit_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/ratelimit"
)

// {"RUCM-PATH": "P006"}
//
// 目的: 位置引数を受け付けないことを確認する（引数の指定の誤りは終了コード 2）。
// 与える情報: 位置引数を1つ付けた実行。
// 成功条件: 終了コードが 2 で、受け付けないことが出力に出ること。
func Test_Keychainの読み取りを許可する_P006_位置引数を受け付けない(t *testing.T) {
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

// {"RUCM-PATH": "P005"}
//
// 目的: macOS 以外では「意味がありません」と出して終了コード 0 で終わることを確認する。
//
// **失敗として扱わない。**前提が違うだけであり、CI を落とす理由が無い。
//
// 与える情報: macOS 以外での実行。
// 成功条件: 終了コードが 0 で、macOS でだけ意味があることが出力に出ること。
func Test_Keychainの読み取りを許可する_P005_実行ファイルはmacOS以外では何もしない(t *testing.T) {
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

// {"RUCM-PATH": "P001"}
//
// 目的: 読めたときに、先に案内を出し、読めた項目の**名前だけ**を出すことを確認する。
//
// **値（トークン）を出してはならない。**端末とスクロールバッファに残る。
//
// 与える情報: 資格情報の JSON を返すテスト用security mock。
// 成功条件: 終了コードが 0。実行前の案内（「常に許可」）と、読めた項目の名前が出ること。
// **トークンの値が1回も出ないこと。**
func Test_Keychainの読み取りを許可する_P001_実行ファイルは読めたら項目の名前だけを出す(t *testing.T) {
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

// {"RUCM-PATH": "P003"}
//
// 目的: 読めなかったときに、原因と対処を書いて終了コード 1 で終わることを確認する
// （設計 3-34b の形）。
//
// 与える情報: 標準エラーへ理由を書いて異常終了するテスト用security mock。
// 成功条件: 終了コードが 1。【確かめ方】【よくある原因】【対処】がすべて出ること。
func Test_Keychainの読み取りを許可する_P003_実行ファイルは読めなければ原因と対処を出す(t *testing.T) {
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
