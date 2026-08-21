// Package doctor_test のうち、このファイルは見出し語 `資格情報` の
// `token_source: keychain` の経路を確かめる。
//
// **本物の `security` は1回も起動しない。**PATH の先頭にテスト用security mock を置く。
// **本物の Keychain も読まない。**読んでしまうと、テストの実行で確認のダイアログが出る。
package doctor_test

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/doctor"
)

// keychainWorkflow は `token_source: keychain` を書いた rate_limit の節である。
const keychainWorkflow = "rate_limit:\n  source: oauth_usage_api\n  token_source: keychain\n"

// requireDarwin は macOS 以外でテストを飛ばす。
//
// **`keychain` は macOS でだけ選べる**（internal/config の検査が弾く）ので、
// macOS 以外ではこの設定が config.Load を通らず、資格情報の検査まで到達しない。
//
// t: 呼び出し元のテスト。
func requireDarwin(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skipf("token_source: keychain は macOS でだけ選べる（いまの OS: %s）", runtime.GOOS)
	}
}

// 目的: `token_source: keychain` で Keychain から読めたら `✓` にすることを確認する。
//
// **以前は macOS で必ず `!` になっていた**（Keychain を読まなかったため）。
// それでは macOS の利用者はこの検査から何も得られない。
//
// 与える情報: 資格情報の JSON を返すテスト用security mock。
// 成功条件: 資格情報が `✓` になり、説明に Keychain の項目名が入り、終了コードが 0 であること。
func TestDoctor_資格情報_keychainから読めたら通る(t *testing.T) {
	requireDarwin(t)

	fx := newFixture(t)
	fx.WriteWorkflow(t, keychainWorkflow)
	writeFakeSecurity(t, fx.BinDir, `printf '%s' '{"claudeAiOauth":{"accessToken":"テスト用のトークン"}}'`)

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelCredentials, doctor.SymbolOK)
	if !strings.Contains(res.Detail, "Claude Code-credentials") {
		t.Fatalf("説明に Keychain の項目名が入っていない: %q", res.Detail)
	}
	if report.ExitCode() != 0 {
		t.Fatalf("すべて通ったのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
}

// 目的: Keychain に項目が無いとき `✗` にし、直し方に `continuo allow-keychain-access` を
// 出すことを確認する。
//
// **`token_source: keychain` は利用者が明示して選んだ値である。**取れていないのに
// 「確かめられなかった」で流すと、枠の判定が効いていないことに誰も気づけない。
//
// 与える情報: 標準エラーへ理由を書いて異常終了するテスト用security mock。
// 成功条件: 資格情報が `✗` になり、直し方に allow-keychain-access が入り、終了コードが 1 であること。
func TestDoctor_資格情報_keychainを読めなければ足りないと出す(t *testing.T) {
	requireDarwin(t)

	fx := newFixture(t)
	fx.WriteWorkflow(t, keychainWorkflow)
	writeFakeSecurity(t, fx.BinDir,
		"echo 'security: The specified item could not be found in the keychain.' >&2\nexit 44")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelCredentials, doctor.SymbolMissing)
	if !strings.Contains(res.Detail, "could not be found in the keychain") {
		t.Fatalf("説明に security の標準エラー出力が入っていない（原因が分からない）: %q", res.Detail)
	}
	if !strings.Contains(strings.Join(res.Remedies, "\n"), "allow-keychain-access") {
		t.Fatalf("直し方に continuo allow-keychain-access が入っていない: %v", res.Remedies)
	}
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
}

// 目的: `security` が期限内に返らないとき、doctor が固まらずに `!` を出すことを確認する。
//
// **doctor が Keychain を読むと決めた前提が「固まらないこと」である。**
// 確認のダイアログが出たまま誰も答えなくても、1項目あたりの期限で打ち切られる。
//
// 与える情報: 返ってこないテスト用security mock と、短い1項目あたりの上限。
// 成功条件: 資格情報が `!` になり、案内がダイアログを疑わせ、終了コードが 0 のままであること。
// 検査全体が上限の付近で戻ること。
func TestDoctor_資格情報_keychainが返ってこなければ確かめられなかったと出す(t *testing.T) {
	requireDarwin(t)

	fx := newFixture(t)
	fx.WriteWorkflow(t, keychainWorkflow)
	// **`exec` で置き換える。**シェルを残すと、殺したあとの後始末を待つぶん遅くなる。
	writeFakeSecurity(t, fx.BinDir, "exec sleep 30")
	fx.CheckTimeout = 500 * time.Millisecond

	start := time.Now()
	report := fx.Run(t)
	elapsed := time.Since(start)

	res := assertSymbol(t, report, doctor.LabelCredentials, doctor.SymbolUnknown)
	if !strings.Contains(strings.Join(res.Remedies, "\n"), "ダイアログ") {
		t.Fatalf("案内がダイアログを疑わせていない: %v", res.Remedies)
	}
	if report.ExitCode() != 0 {
		t.Fatalf("! だけなのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
	if elapsed > 25*time.Second {
		t.Fatalf("期限を掛けずに待ち続けた: %v", elapsed)
	}
}

// 目的: `token_source: claude_credentials` でファイルが無い macOS で、
// Keychain へ移る道を案内することを確認する。
//
// **macOS ではこのファイルが無いのが普通である。**「飛ばしました」だけで終わると、
// 利用者は枠の判定を諦めるしかないと読んでしまう。
//
// 与える情報: `token_source: claude_credentials` と、資格情報のファイルが無いホームディレクトリ。
// 成功条件: 資格情報が `!` のまま、直し方に keychain の案内が入ること。
func TestDoctor_資格情報_macOSでファイルが無ければkeychainを案内する(t *testing.T) {
	requireDarwin(t)

	fx := newFixture(t)
	fx.WriteWorkflow(t, "rate_limit:\n  source: oauth_usage_api\n  token_source: claude_credentials\n")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelCredentials, doctor.SymbolUnknown)
	joined := strings.Join(res.Remedies, "\n")
	if !strings.Contains(joined, "keychain") {
		t.Fatalf("直し方に keychain への案内が入っていない: %v", res.Remedies)
	}
	if !strings.Contains(joined, "allow-keychain-access") {
		t.Fatalf("直し方に continuo allow-keychain-access が入っていない: %v", res.Remedies)
	}
}
