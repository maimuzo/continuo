// Package ratelimit_test のうち、このファイルは `token_source: keychain`
// （macOS の Keychain から資格情報を読む経路）を検証する。
//
// **本物の `security` は1回も起動しない。**PATH の先頭にテスト用security mock を置き、
// 終了コードと出力を決め打ちにする（test/internal/workspace/ghq_test.go の fakeGhq と同じ手口）。
// **本物の Keychain も読まない。**読んでしまうと、テストの実行で確認のダイアログが出る。
package ratelimit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// keychainTestToken はテスト用security mock が返すトークンである。
//
// **エラー文やログにこの文字列が現れないことを検査する**ので、ほかの文字列と紛れない形にしてある。
// keychainTestTimeout は偽の `security` を待つ上限である。
//
// **固定 1 秒にしてはならない。**`go test -coverpkg=./...` は全パッケージを instrument
// するので実行が遅くなり、1 秒では偽の `security` の起動が間に合わずに落ちる
// （2026-08-21 に実際に起きた）。**ここで検査しているのは「値を返さないこと」であって
// 速さではない。**
const keychainTestTimeout = 10 * time.Second

const keychainTestToken = "sk-ant-oat01-テスト用のキーチェーンのトークン"

// fakeSecurity は PATH の先頭に置くテスト用security mock を作る。
//
// **本物の `security` を実行しない。**本物を叩くと、テストの実行中に Keychain の確認の
// ダイアログが出て、答える人がいないまま止まる。
//
// t: 呼び出し元のテスト。PATH の差し替えを t.Setenv で行う（後始末は testing が行う）。
// script: `security` として実行させるシェルスクリプトの中身（`#!/bin/sh` の次の行から）。
func fakeSecurity(t *testing.T, script string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "security")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatalf("テスト用security mock を書けない: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// emptyPATH は PATH を空にして、`security` がどこにも無い環境を作る。
//
// **macOS 以外の環境（`security` が存在しない）を再現する**ために使う。
//
// t: 呼び出し元のテスト。
func emptyPATH(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", "")
}

// keychainConfig は Keychain から資格情報を読む設定を返す。
//
// 戻り値: `source: oauth_usage_api` / `token_source: keychain` の設定。
func keychainConfig() config.RateLimitConfig {
	cfg := usageConfig()
	cfg.TokenSource = ratelimit.TokenSourceKeychain
	return cfg
}

// 目的: `token_source: keychain` のとき、`security` が返した JSON から accessToken を読み、
// それを usage API の Authorization ヘッダへ載せることを確認する。
//
// **macOS では `~/.claude/.credentials.json` が無いのが普通で、資格情報は Keychain にある。**
// この経路が動かないと、macOS では枠の判定が丸ごと効かない。
//
// 与える情報: 資格情報の JSON を返すテスト用security mock と、偽の usage API。
// 成功条件: Fetch が枠を読み取れること。`security` へ渡した引数が
// `find-generic-password -s Claude Code-credentials -w` であること。
func TestFetch_keychainからトークンを読んで枠を取得する(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"limits":[{"kind":"session","percent":42,"resets_at":null,"severity":"normal"}]}`))
	}))
	defer srv.Close()

	argsFile := filepath.Join(t.TempDir(), "args.txt")
	fakeSecurity(t, "echo \"$@\" > "+argsFile+"\n"+
		`printf '%s' '{"claudeAiOauth":{"accessToken":"`+keychainTestToken+`"}}'`)

	reader, err := ratelimit.NewReader(ratelimit.Options{
		Config:   keychainConfig(),
		Endpoint: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewReader が失敗した: %v", err)
	}

	snap, err := reader.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch が失敗した: %v", err)
	}
	if snap == nil {
		t.Fatalf("枠を読めていない（snapshot が nil）")
	}
	if snap.MaxPercent() != 42 {
		t.Errorf("読み取った使用率が違う: got %d, want %d", snap.MaxPercent(), 42)
	}
	if want := "Bearer " + keychainTestToken; gotAuth != want {
		t.Errorf("Keychain から読んだトークンが Authorization ヘッダに載っていない: got %q, want %q", gotAuth, want)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("テスト用security mock が引数を書き出していない: %v", err)
	}
	want := "find-generic-password -s " + ratelimit.KeychainService + " -w"
	if got := strings.TrimSpace(string(raw)); got != want {
		t.Errorf("security へ渡した引数が違う: got %q, want %q", got, want)
	}
}

// 目的: `token_source: keychain` のとき、ホームディレクトリを引かずに Reader を組み立てられる
// ことを確認する。
//
// **Keychain を読む設定では `~/.claude/.credentials.json` を1回も探さない。**
// ここでホームディレクトリを要求すると、引けない環境で起動そのものが止まる。
//
// 与える情報: HomeDir を渡さない Options。
// 成功条件: NewReader がエラーを返さないこと。
func TestNewReader_keychainならホームディレクトリを要求しない(t *testing.T) {
	if _, err := ratelimit.NewReader(ratelimit.Options{Config: keychainConfig()}); err != nil {
		t.Fatalf("keychain なのにホームディレクトリを要求した: %v", err)
	}
}

// 目的: `security` が返した中身が JSON として壊れているとき、枠の判定を捨てて
// **起動を止めない**ことを確認する。
//
// 与える情報: JSON になっていない文字列を返すテスト用security mock。
// 成功条件: Fetch が (nil, nil) を返し（**エラーを上へ投げない**）、以後 Enabled が偽になること。
func TestFetch_keychainの中身が壊れていたら枠の判定を捨てる(t *testing.T) {
	fakeSecurity(t, `printf '%s' 'これは JSON ではありません'`)

	reader, err := ratelimit.NewReader(ratelimit.Options{Config: keychainConfig()})
	if err != nil {
		t.Fatalf("NewReader が失敗した: %v", err)
	}

	snap, err := reader.Fetch(context.Background())
	if err != nil {
		t.Fatalf("壊れた中身をエラーとして上へ投げた（起動が止まる）: %v", err)
	}
	if snap != nil {
		t.Fatalf("壊れた中身なのに枠を読めたことになっている: %+v", snap)
	}
	if reader.Enabled() {
		t.Fatal("諦めたのに Enabled が真のまま（次の巡回でも読みに行ってしまう）")
	}
}

// 目的: `security` が期限内に返らないとき、枠の判定を捨てて**起動を止めない**ことを確認する。
//
// **確認のダイアログが出たまま誰も答えないと `security` は返らない。**
// 上限が無いと、無人の常駐プロセスが巡回のループごとそこで止まる。
//
// 与える情報: 返ってこないテスト用security mock（長く眠る）と、短い KeychainTimeout。
// 成功条件: Fetch が上限の付近で戻り、(nil, nil) を返し、以後 Enabled が偽になること。
func TestFetch_keychainが返ってこなければ枠の判定を捨てる(t *testing.T) {
	// **`exec` で置き換える。**シェルを残すと、殺したあともシェルが標準出力の書き手として
	// 残り、後始末を待つぶんテストが遅くなる。
	fakeSecurity(t, "exec sleep 30")

	const timeout = 300 * time.Millisecond
	reader, err := ratelimit.NewReader(ratelimit.Options{
		Config:          keychainConfig(),
		KeychainTimeout: timeout,
	})
	if err != nil {
		t.Fatalf("NewReader が失敗した: %v", err)
	}

	start := time.Now()
	snap, err := reader.Fetch(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("返ってこないことをエラーとして上へ投げた（起動が止まる）: %v", err)
	}
	if snap != nil {
		t.Fatalf("返ってこないのに枠を読めたことになっている: %+v", snap)
	}
	if reader.Enabled() {
		t.Fatal("諦めたのに Enabled が真のまま")
	}
	if elapsed > 10*time.Second {
		t.Fatalf("上限を掛けずに待ち続けた: %v", elapsed)
	}
}

// 目的: Keychain から読んだトークンの値が、エラー文にもログにも1回も現れないことを確認する。
//
// **無人の常駐プロセスのログは長く残る。**そこへ accessToken が落ちると、ログを読める人が
// そのまま Claude の API を叩けてしまう。
//
// 与える情報: **トークンそのものを** JSON ではない形で返すテスト用security mock。
// 成功条件: 諦めたときのログにトークンの値が含まれないこと。
func TestFetch_keychainのトークンはログにもエラー文にも出ない(t *testing.T) {
	// **`security` の標準出力に生のトークンが出る状況を作る。**JSON として壊れているので
	// 解析に失敗し、その失敗の文言がログへ流れる経路になる。
	fakeSecurity(t, `printf '%s' '`+keychainTestToken+`'`)

	buf, logger := newTestLogger()
	reader, err := ratelimit.NewReader(ratelimit.Options{Config: keychainConfig(), Logger: logger})
	if err != nil {
		t.Fatalf("NewReader が失敗した: %v", err)
	}

	if _, err := reader.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch がエラーを上へ投げた: %v", err)
	}
	if got := buf.String(); strings.Contains(got, keychainTestToken) {
		t.Fatalf("ログにトークンの値が出ている:\n%s", got)
	}
	if buf.Len() == 0 {
		t.Fatal("諦めたのに警告が1行も出ていない（気づけない）")
	}
}

// 目的: `security` が異常終了したとき、その理由（標準エラー出力）をエラー文に残しつつ、
// 枠の判定を捨てて起動を止めないことを確認する。
//
// 与える情報: 標準エラーへ理由を書いて終了コード 44 で終わるテスト用security mock。
// 成功条件: Fetch が (nil, nil) を返し、ログに標準エラーの内容が残ること。
func TestFetch_keychainに項目が無ければ理由を残して捨てる(t *testing.T) {
	fakeSecurity(t, "echo 'security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain.' >&2\nexit 44")

	buf, logger := newTestLogger()
	reader, err := ratelimit.NewReader(ratelimit.Options{Config: keychainConfig(), Logger: logger})
	if err != nil {
		t.Fatalf("NewReader が失敗した: %v", err)
	}

	snap, err := reader.Fetch(context.Background())
	if err != nil {
		t.Fatalf("異常終了をエラーとして上へ投げた（起動が止まる）: %v", err)
	}
	if snap != nil {
		t.Fatalf("異常終了なのに枠を読めたことになっている: %+v", snap)
	}
	if got := buf.String(); !strings.Contains(got, "could not be found in the keychain") {
		t.Fatalf("ログに security の標準エラー出力が残っていない（原因が分からない）:\n%s", got)
	}
}

// 目的: `security` が PATH に無い環境で、コマンドを起動しに行かずに枠の判定を捨てることを
// 確認する。
//
// **macOS 以外にはこのコマンドが無い。**設定の検査（internal/config）が
// `keychain` を macOS 以外で弾くので本番では到達しないが、**無い実行ファイルを
// 起動しに行かない**ことをここで固定する。
//
// 与える情報: PATH を空にした環境。
// 成功条件: Fetch が (nil, nil) を返し、ログに `security` が無いことが残ること。
func TestFetch_securityが無ければ起動せずに捨てる(t *testing.T) {
	emptyPATH(t)

	buf, logger := newTestLogger()
	reader, err := ratelimit.NewReader(ratelimit.Options{Config: keychainConfig(), Logger: logger})
	if err != nil {
		t.Fatalf("NewReader が失敗した: %v", err)
	}

	snap, err := reader.Fetch(context.Background())
	if err != nil {
		t.Fatalf("security が無いことをエラーとして上へ投げた（起動が止まる）: %v", err)
	}
	if snap != nil {
		t.Fatalf("security が無いのに枠を読めたことになっている: %+v", snap)
	}
	if got := buf.String(); !strings.Contains(got, "security") {
		t.Fatalf("ログに security が見つからないことが残っていない:\n%s", got)
	}
}

// 目的: ProbeKeychain が**項目の名前だけ**を返し、値を1つも返さないことを確認する。
//
// **`continuo allow-keychain-access` が画面へ出すのはこの戻り値である。**
// ここに値が混ざると、そのまま端末とスクロールバッファへトークンが残る。
//
// 与える情報: 実測（2026-08-21、macOS）と同じ7項目を持つ JSON を返すテスト用security mock。
// 成功条件: 項目の名前が昇順で返り、HasAccessToken が真で、
// 返った名前のどれにもトークンの値が含まれないこと。
func TestProbeKeychain_項目の名前だけを返す(t *testing.T) {
	fakeSecurity(t, `printf '%s' '{"claudeAiOauth":{`+
		`"accessToken":"`+keychainTestToken+`",`+
		`"refreshToken":"refresh-の値",`+
		`"expiresAt":1,"refreshTokenExpiresAt":2,`+
		`"scopes":["a"],"subscriptionType":"max","rateLimitTier":"tier"}}'`)

	probe, err := ratelimit.ProbeKeychain(context.Background(), keychainTestTimeout)
	if err != nil {
		t.Fatalf("ProbeKeychain が失敗した: %v", err)
	}
	if !probe.HasAccessToken {
		t.Error("accessToken があるのに HasAccessToken が偽になっている")
	}
	want := []string{
		"accessToken", "expiresAt", "rateLimitTier", "refreshToken",
		"refreshTokenExpiresAt", "scopes", "subscriptionType",
	}
	if strings.Join(probe.Fields, ",") != strings.Join(want, ",") {
		t.Fatalf("読めた項目の名前が違う: got %v, want %v", probe.Fields, want)
	}
	for _, f := range probe.Fields {
		if strings.Contains(f, keychainTestToken) || strings.Contains(f, "refresh-の値") {
			t.Fatalf("項目の名前に値が混ざっている: %q", f)
		}
	}
}

// 目的: ProbeKeychain が期限内に返らなかったことを、ほかの失敗と言い分けられることを確認する。
//
// **「読めなかった」と「返ってこなかった」で人間に見せる案内が変わる**（設計 3-34b）。
// 返ってこなかった場合は、確認のダイアログが出たままである可能性が高い。
//
// 与える情報: 返ってこないテスト用security mock と、短い上限。
// 成功条件: errors.Is で ratelimit.ErrKeychainTimeout に辿れること。
func TestProbeKeychain_期限切れは他の失敗と言い分けられる(t *testing.T) {
	fakeSecurity(t, "exec sleep 30")

	_, err := ratelimit.ProbeKeychain(context.Background(), 300*time.Millisecond)
	if err == nil {
		t.Fatal("返ってこないのにエラーにならなかった")
	}
	if !isKeychainTimeout(err) {
		t.Fatalf("期限切れを言い分けられない（ErrKeychainTimeout へ辿れない）: %v", err)
	}
	if !isNoCredentials(err) {
		t.Fatalf("期限切れでも枠の判定は諦めるので ErrNoCredentials を包むべき: %v", err)
	}
}
