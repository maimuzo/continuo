// {"RUCM-CFG-SHA256": "f15436ef1f4b32a8a33d8ea1b790521660aeec3b550b492f47f24dce283f64ba", "SOURCE": "docs/spec/usecases/particular_case/Keychainの読み取りを許可する.cfg.json"}
//
// **ユースケース記述「Keychainの読み取りを許可する」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package cli_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/cli"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// {"RUCM-PATH": "P005"}
//
// Test_Keychainの読み取りを許可する_P005_macOS以外では何もしない は、OS の判定を確かめる。
//
// **`security` は macOS の標準コマンドであり、ほかの OS には無い。**
// **黙って失敗させると、Linux の利用者は「なぜ動かないのか」を知る手がかりを持たない。**
//
// 目的: macOS 以外では Keychain を叩かず、その旨を出して終わること。
// 与える情報: `goos` を linux にした状態。
// 成功条件: Keychain を1回も叩かず、OS 名を含む案内が出ること。
func Test_Keychainの読み取りを許可する_P005_macOS以外では何もしない(t *testing.T) {
	deps := cli.Deps{GOOS: "linux"}
	var probed bool
	deps.ProbeKeychain = func(_ context.Context, _ time.Duration) (ratelimit.KeychainProbe, error) {
		probed = true
		return ratelimit.KeychainProbe{}, nil
	}

	_, stdout, _ := runCLIWith(deps, []string{"allow-keychain-access"}, "")
	if probed {
		t.Error("macOS 以外なのに Keychain を叩いている")
	}
	if !strings.Contains(stdout, "linux") {
		t.Errorf("どの OS で動いているかを示していない: %s", stdout)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_Keychainの読み取りを許可する_P001_読めたら項目の名前だけを出す は、値を漏らさないことを確かめる。
//
// **この出力は端末とスクロールバッファに残る。**
// **トークンの値が1文字でも混ざってはならない。**
//
// 目的: 読めたとき、項目の名前だけを出すこと。
// 与える情報: 項目の名前を返す probeKeychain。
// 成功条件: 終了コードが 0 で、項目の名前が出ること。
func Test_Keychainの読み取りを許可する_P001_読めたら項目の名前だけを出す(t *testing.T) {
	deps := cli.Deps{GOOS: "darwin"}
	deps.ProbeKeychain = func(_ context.Context, _ time.Duration) (ratelimit.KeychainProbe, error) {
		return ratelimit.KeychainProbe{
			Fields:         []string{"accessToken", "expiresAt", "refreshToken"},
			HasAccessToken: true,
		}, nil
	}

	code, stdout, stderr := runCLIWith(deps, []string{"allow-keychain-access"}, "")
	if code != 0 {
		t.Errorf("読めたのに終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	if !strings.Contains(stdout, "accessToken") {
		t.Errorf("項目の名前を出していない: %s", stdout)
	}
}

// {"RUCM-PATH": "P004"}
//
// Test_Keychainの読み取りを許可する_P004_期限内に返らなければ直し方を出す は、ダイアログで止まった場合を確かめる。
//
// **確認のダイアログが出たまま誰も答えないと、`security` は返らない。**
// **黙って待ち続けると、人間は何が起きているか分からない。**
//
// 目的: 期限切れのとき、何が起きているかと直し方を出すこと。
// 与える情報: `ErrKeychainTimeout` を返す probeKeychain。
// 成功条件: 終了コードが 0 でなく、案内が出ること。
func Test_Keychainの読み取りを許可する_P004_期限内に返らなければ直し方を出す(t *testing.T) {
	deps := cli.Deps{GOOS: "darwin"}
	deps.ProbeKeychain = func(_ context.Context, _ time.Duration) (ratelimit.KeychainProbe, error) {
		return ratelimit.KeychainProbe{}, ratelimit.ErrKeychainTimeout
	}

	code, stdout, _ := runCLIWith(deps, []string{"allow-keychain-access"}, "")
	if code == 0 {
		t.Error("期限切れなのに成功として終わっている")
	}
	if stdout == "" {
		t.Error("何が起きたかを出していない")
	}
}

// {"RUCM-PATH": "P003"}
//
// Test_Keychainの読み取りを許可する_P003_読めなければ直し方を出す は、Keychain の失敗の案内を確かめる。
//
// **トークンの値をエラー文へ混ぜてはならない。**この出力は端末とスクロールバッファに残る。
//
// 目的: Keychain を読めないとき、何が起きたかと直し方を出すこと。
// 与える情報: エラーを返す probeKeychain。
// 成功条件: 終了コードが 0 でなく、案内が出ること。
func Test_Keychainの読み取りを許可する_P003_読めなければ直し方を出す(t *testing.T) {
	deps := cli.Deps{
		GOOS: "darwin",
		ProbeKeychain: func(_ context.Context, _ time.Duration) (ratelimit.KeychainProbe, error) {
			return ratelimit.KeychainProbe{}, errors.New("security コマンドが失敗しました")
		},
	}

	code, stdout, _ := runCLIWith(deps, []string{"allow-keychain-access"}, "")
	if code == 0 {
		t.Error("読めないのに成功として終わっている")
	}
	if !strings.Contains(stdout, "security コマンドが失敗しました") {
		t.Errorf("何が起きたかを出していない:\n%s", stdout)
	}
}

// {"RUCM-PATH": "P002"}
//
// Test_Keychainの読み取りを許可する_P002_accessTokenが無ければ落とす は、中身の検査を確かめる。
//
// **Keychain の項目は読めても、`accessToken` が空のことがある。**
// **そのまま「読めました」と出すと、人間は枠を読めると思い込む。**
//
// 目的: `accessToken` が無いとき、成功として終わらないこと。
// 与える情報: 項目はあるが `HasAccessToken` が偽の probe。
// 成功条件: 終了コードが 0 でないこと。
func Test_Keychainの読み取りを許可する_P002_accessTokenが無ければ落とす(t *testing.T) {
	deps := cli.Deps{
		GOOS: "darwin",
		ProbeKeychain: func(_ context.Context, _ time.Duration) (ratelimit.KeychainProbe, error) {
			return ratelimit.KeychainProbe{Fields: []string{"expiresAt"}, HasAccessToken: false}, nil
		},
	}

	code, _, _ := runCLIWith(deps, []string{"allow-keychain-access"}, "")
	if code == 0 {
		t.Error("accessToken が無いのに成功として終わっている")
	}
}

// {"RUCM-PATH": "P007"}
//
// Test_Keychainの読み取りを許可する_P007_使い方の表示ではKeychainを読まない は、代替フロー「使い方の表示」を確かめる。
//
// 目的: `--help` を付けたとき、Keychain を読みに行かず、使い方を出して終了コード 0 で終わること。
// 与える情報: macOS として動かし、`--help` を付けた実行。Keychain を読む関数は、呼ばれたことを記録する。
// 成功条件: 終了コードが 0 で、Keychain を1回も読まず、標準エラーにコマンド名を含む使い方が出ること。
func Test_Keychainの読み取りを許可する_P007_使い方の表示ではKeychainを読まない(t *testing.T) {
	deps := cli.Deps{GOOS: "darwin"}
	var probed bool
	deps.ProbeKeychain = func(_ context.Context, _ time.Duration) (ratelimit.KeychainProbe, error) {
		probed = true
		return ratelimit.KeychainProbe{}, nil
	}

	code, stdout, stderr := runCLIWith(deps, []string{"allow-keychain-access", "--help"}, "")
	if code != 0 {
		t.Errorf("使い方の表示なのに終了コードが 0 でない: %d", code)
	}
	if probed {
		t.Error("使い方の表示なのに Keychain を読んでいる")
	}
	if !strings.Contains(stderr, "allow-keychain-access") {
		t.Errorf("標準エラーに使い方が出ていない: stdout=%q stderr=%q", stdout, stderr)
	}
}
