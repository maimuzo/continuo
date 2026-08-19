package ratelimit_test

import (
	"context"
	"errors"
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

// 目的: 本番の既定である token_source: claude_credentials の**成功**経路を確認する
// （レビュー指摘「成功経路が1件もテストされていない」の回帰テスト）。
// 与える情報: 偽のホームディレクトリに正しい `.claude/.credentials.json` を書き、
// 偽の usage API を立てる。
// 成功条件: Fetch が枠を読み取れること。ファイルから読んだ accessToken が
// Authorization: Bearer ヘッダに載り、anthropic-beta と User-Agent も一緒に送られること
// （**どれか1つでも落とすと 401 になる**。設計 3-15）。
func TestFetch_資格情報ファイルからトークンを読んで枠を取得する(t *testing.T) {
	const wantToken = "sk-ant-oat-テスト用のトークン"

	var gotAuth, gotBeta, gotUA, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		gotUA = r.Header.Get("User-Agent")
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"limits":[
		  {"kind":"session","percent":4,"resets_at":"2026-08-18T14:09:59Z","severity":"normal"},
		  {"kind":"weekly_all","percent":7,"resets_at":"2026-08-24T18:59:59Z","severity":"normal"}
		]}`))
	}))
	defer srv.Close()

	home := t.TempDir()
	writeCredentials(t, home, `{"claudeAiOauth":{"accessToken":"`+wantToken+`"}}`)

	reader, err := ratelimit.NewReader(ratelimit.Options{
		Config:   usageConfig(),
		Endpoint: srv.URL,
		HomeDir:  home,
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
	if len(snap.Limits) != 2 {
		t.Fatalf("枠の件数が想定と違う: got %d, want 2", len(snap.Limits))
	}
	if snap.MaxPercent() != 7 {
		t.Fatalf("MaxPercent が想定と違う: got %d, want 7", snap.MaxPercent())
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("GET で送っていない: got %q", gotMethod)
	}
	if gotAuth != "Bearer "+wantToken {
		t.Fatalf("Authorization ヘッダが想定と違う: got %q", gotAuth)
	}
	if gotBeta != "oauth-2025-04-20" {
		t.Fatalf("anthropic-beta ヘッダが想定と違う（落とすと 401 になる）: got %q", gotBeta)
	}
	if !strings.HasPrefix(gotUA, "claude-code/") {
		t.Fatalf("User-Agent が想定と違う: got %q", gotUA)
	}
}

// 目的: 資格情報ファイルが壊れている場合・accessToken が空の場合に、枠の判定を諦めて
// 起動を止めないことを確認する（設計 3-27）。
// 与える情報: 壊れた JSON、accessToken が空の JSON、ファイルが無い状態の3通り。
// 成功条件: いずれも Fetch が (nil, nil) を返し、以後 Enabled が偽になること。
// HTTP リクエストが1本も出ないこと。
func TestFetch_資格情報が読めなければ諦めて起動を止めない(t *testing.T) {
	cases := []struct {
		name string
		// body は資格情報ファイルの中身。空文字ならファイルを作らない。
		body string
	}{
		{name: "JSONが壊れている", body: `{"claudeAiOauth":{`},
		{name: "accessTokenが空", body: `{"claudeAiOauth":{"accessToken":""}}`},
		{name: "ファイルが無い", body: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = w.Write([]byte(`{"limits":[]}`))
			}))
			defer srv.Close()

			home := t.TempDir()
			if tc.body != "" {
				writeCredentials(t, home, tc.body)
			}

			buf, logger := newTestLogger()
			reader, err := ratelimit.NewReader(ratelimit.Options{
				Config:   usageConfig(),
				Endpoint: srv.URL,
				HomeDir:  home,
				Logger:   logger,
			})
			if err != nil {
				t.Fatalf("NewReader が失敗した: %v", err)
			}

			snap, err := reader.Fetch(context.Background())
			if err != nil {
				t.Fatalf("資格情報が取れないのにエラーを返した（起動を止めてはならない）: %v", err)
			}
			if snap != nil {
				t.Fatalf("資格情報が取れないのに snapshot を返した")
			}
			if reader.Enabled() {
				t.Fatalf("諦めたあとも Enabled が真のままである（毎回読みに行ってしまう）")
			}
			if calls != 0 {
				t.Fatalf("資格情報が無いのに usage API を叩いた: %d 回", calls)
			}
			if !strings.Contains(buf.String(), "枠の判定を諦めます") {
				t.Fatalf("諦めた警告がログに出ていない: %s", buf.String())
			}

			// 2回目も叩かない。警告も増えない（警告は1回だけ。設計 3-15）。
			before := strings.Count(buf.String(), "枠の判定を諦めます")
			if _, err := reader.Fetch(context.Background()); err != nil {
				t.Fatalf("2回目の Fetch がエラーを返した: %v", err)
			}
			if calls != 0 {
				t.Fatalf("諦めたあとに usage API を叩いた: %d 回", calls)
			}
			if after := strings.Count(buf.String(), "枠の判定を諦めます"); after != before {
				t.Fatalf("警告が2回以上出ている: got %d, want %d", after, before)
			}
		})
	}
}

// 目的: usage API が 401 / 403 を返したら枠の判定を諦めることを確認する
// （レビュー指摘「401 を返しても諦めず、30秒おきに永久に叩き直す」の回帰テスト）。
//
// **失効した accessToken を抱えたまま放置すると、無人のプロセスが 401 を巡回のたびに
// 取りに行き、ログも同じ頻度で汚れる。**
//
// 与える情報: 常に 401（または 403）を返す偽の usage API。
// 成功条件: 1回目の Fetch が (nil, nil) を返して Enabled が偽になり、2回目以降は
// HTTP リクエストを1本も出さないこと。
func TestFetch_401と403は諦めて叩き直さない(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"認証に失敗しました"}`))
			}))
			defer srv.Close()

			home := t.TempDir()
			writeCredentials(t, home, `{"claudeAiOauth":{"accessToken":"expired"}}`)

			buf, logger := newTestLogger()
			reader, err := ratelimit.NewReader(ratelimit.Options{
				Config:   usageConfig(),
				Endpoint: srv.URL,
				HomeDir:  home,
				Logger:   logger,
			})
			if err != nil {
				t.Fatalf("NewReader が失敗した: %v", err)
			}

			snap, err := reader.Fetch(context.Background())
			if err != nil {
				t.Fatalf("%d は諦める扱いなのにエラーを返した: %v", status, err)
			}
			if snap != nil {
				t.Fatalf("%d なのに snapshot を返した", status)
			}
			if reader.Enabled() {
				t.Fatalf("%d を受けても Enabled が真のままである（30秒おきに叩き続ける）", status)
			}
			if !strings.Contains(buf.String(), "枠の判定を諦めます") {
				t.Fatalf("諦めた警告がログに出ていない: %s", buf.String())
			}

			if _, err := reader.Fetch(context.Background()); err != nil {
				t.Fatalf("2回目の Fetch がエラーを返した: %v", err)
			}
			if calls != 1 {
				t.Fatalf("%d を受けたあとも叩き直している: %d 回", status, calls)
			}
		})
	}
}

// 目的: 5xx は「一時的な失敗」としてエラーで返し、枠の判定は諦めないことを確認する。
//
// **401 / 403 と同じ扱いにしてはならない。**provider 側の一時的な不調で、
// 以後ずっと枠の判定を切ってしまうことになる。
//
// 与える情報: 500 を返す偽の usage API。
// 成功条件: Fetch がエラーを返し、Enabled は真のままであること。次の Fetch も叩くこと。
func TestFetch_5xxは諦めずエラーとして返す(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"内部エラー"}`))
	}))
	defer srv.Close()

	home := t.TempDir()
	writeCredentials(t, home, `{"claudeAiOauth":{"accessToken":"ok"}}`)

	reader, err := ratelimit.NewReader(ratelimit.Options{
		Config:   usageConfig(),
		Endpoint: srv.URL,
		HomeDir:  home,
	})
	if err != nil {
		t.Fatalf("NewReader が失敗した: %v", err)
	}

	if _, err := reader.Fetch(context.Background()); err == nil {
		t.Fatalf("500 なのにエラーを返さなかった")
	}
	if !reader.Enabled() {
		t.Fatalf("500 で枠の判定を諦めている（一時的な失敗である）")
	}
	if _, err := reader.Fetch(context.Background()); err == nil {
		t.Fatalf("2回目の 500 でもエラーを返さなかった")
	}
	if calls != 2 {
		t.Fatalf("2回叩いていない: %d 回", calls)
	}
}

// 目的: `rate_limit.source: none` のときに usage API を1回も叩かないことを確認する
// （設計 3-15 の絶対条件）。
// 与える情報: source を "none" にした設定と、叩かれたら数える偽の usage API。
// 成功条件: Enabled が偽で、Fetch が (nil, nil) を返し、リクエストが0回であること。
func TestFetch_sourceがnoneならAPIを1回も叩かない(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
	}))
	defer srv.Close()

	reader, err := ratelimit.NewReader(ratelimit.Options{
		Config:   config.RateLimitConfig{Source: ratelimit.SourceNone},
		Endpoint: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewReader が失敗した: %v", err)
	}
	if reader.Enabled() {
		t.Fatalf("source: none なのに Enabled が真である")
	}
	snap, err := reader.Fetch(context.Background())
	if err != nil || snap != nil {
		t.Fatalf("source: none なのに読みに行った: snap=%v, err=%v", snap, err)
	}
	if calls != 0 {
		t.Fatalf("source: none なのに usage API を叩いた: %d 回", calls)
	}
}

// 目的: 資格情報ファイルが symlink のときは読まないことを確認する
// （レビュー指摘「ファイルの種別も権限も確かめずに読む」の回帰テスト）。
//
// **中身は Claude の OAuth アクセストークンである。**symlink を辿ると、
// 別の場所に置き換えられたファイルを黙って読んで HTTP ヘッダに載せることになる。
//
// 与える情報: 実体は別の場所に置き、`.claude/.credentials.json` をそこへの symlink にする。
// 成功条件: Fetch が (nil, nil) を返し、Enabled が偽になること。
// usage API を1回も叩かないこと。
func TestFetch_資格情報がsymlinkなら読まない(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
	}))
	defer srv.Close()

	home := t.TempDir()
	real := filepath.Join(t.TempDir(), "real-credentials.json")
	if err := os.WriteFile(real, []byte(`{"claudeAiOauth":{"accessToken":"leaked"}}`), 0o600); err != nil {
		t.Fatalf("実体のファイルを書けません: %v", err)
	}
	linkPath := filepath.Join(home, ratelimit.CredentialsRelPath)
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o700); err != nil {
		t.Fatalf("偽の資格情報ディレクトリを作れません: %v", err)
	}
	if err := os.Symlink(real, linkPath); err != nil {
		t.Fatalf("symlink を作れません: %v", err)
	}

	reader, err := ratelimit.NewReader(ratelimit.Options{
		Config:   usageConfig(),
		Endpoint: srv.URL,
		HomeDir:  home,
	})
	if err != nil {
		t.Fatalf("NewReader が失敗した: %v", err)
	}

	snap, err := reader.Fetch(context.Background())
	if err != nil {
		t.Fatalf("symlink は諦める扱いなのにエラーを返した: %v", err)
	}
	if snap != nil {
		t.Fatalf("symlink の資格情報を読んでしまった")
	}
	if reader.Enabled() {
		t.Fatalf("symlink を読まなかったのに Enabled が真のままである")
	}
	if calls != 0 {
		t.Fatalf("symlink の資格情報でトークンを送った: %d 回", calls)
	}
}

// 目的: 資格情報ファイルの権限が自分以外にも開いている場合に警告を出すことを確認する
// （レビュー指摘「権限が緩んでいても静かに使い続ける」の回帰テスト）。
// 与える情報: 0o644 の `.claude/.credentials.json`。
// 成功条件: 枠は読めること（読むこと自体は止めない）。ログに chmod 600 を促す警告が
// 出ていること。
func TestFetch_資格情報の権限が緩いと警告を出す(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"limits":[]}`))
	}))
	defer srv.Close()

	home := t.TempDir()
	path := writeCredentials(t, home, `{"claudeAiOauth":{"accessToken":"ok"}}`)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("権限を変更できません: %v", err)
	}

	buf, logger := newTestLogger()
	reader, err := ratelimit.NewReader(ratelimit.Options{
		Config:   usageConfig(),
		Endpoint: srv.URL,
		HomeDir:  home,
		Logger:   logger,
	})
	if err != nil {
		t.Fatalf("NewReader が失敗した: %v", err)
	}

	if _, err := reader.Fetch(context.Background()); err != nil {
		t.Fatalf("権限が緩くても読むはずが失敗した: %v", err)
	}
	if !strings.Contains(buf.String(), "chmod 600") {
		t.Fatalf("権限が緩いことの警告が出ていない: %s", buf.String())
	}
}

// 目的: token_source: env のときに環境変数からトークンを読むこと、
// 環境変数が空なら諦めることを確認する。
// 与える情報: token_env に指定した環境変数を設定した場合と、設定しない場合。
// 成功条件: 設定した場合はその値が Authorization に載ること。設定しない場合は
// ErrNoCredentials として諦め、Enabled が偽になること。
func TestFetch_token_sourceがenvなら環境変数から読む(t *testing.T) {
	const envName = "CONTINUO_TEST_RATE_LIMIT_TOKEN"

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"limits":[]}`))
	}))
	defer srv.Close()

	cfg := usageConfig()
	cfg.TokenSource = ratelimit.TokenSourceEnv
	cfg.TokenEnv = envName

	t.Run("環境変数がある", func(t *testing.T) {
		t.Setenv(envName, "env-token")
		reader, err := ratelimit.NewReader(ratelimit.Options{Config: cfg, Endpoint: srv.URL})
		if err != nil {
			t.Fatalf("NewReader が失敗した: %v", err)
		}
		if _, err := reader.Fetch(context.Background()); err != nil {
			t.Fatalf("Fetch が失敗した: %v", err)
		}
		if gotAuth != "Bearer env-token" {
			t.Fatalf("環境変数のトークンが送られていない: got %q", gotAuth)
		}
	})

	t.Run("環境変数が空", func(t *testing.T) {
		t.Setenv(envName, "")
		reader, err := ratelimit.NewReader(ratelimit.Options{Config: cfg, Endpoint: srv.URL})
		if err != nil {
			t.Fatalf("NewReader が失敗した: %v", err)
		}
		snap, err := reader.Fetch(context.Background())
		if err != nil || snap != nil {
			t.Fatalf("環境変数が空なのに読みに行った: snap=%v, err=%v", snap, err)
		}
		if reader.Enabled() {
			t.Fatalf("環境変数が空でも Enabled が真のままである")
		}
	})
}

// 目的: ErrNoCredentials が errors.Is で判定できることを確認する
// （呼び出し側が「資格情報が無い」と「通信に失敗した」を区別するために使う）。
// 与える情報: 資格情報ファイルが無い状態の Reader。
// 成功条件: ErrNoCredentials が nil でない番兵として公開されていること。
func TestErrNoCredentials_番兵として判定できる(t *testing.T) {
	if ratelimit.ErrNoCredentials == nil {
		t.Fatalf("ErrNoCredentials が nil である")
	}
	wrapped := errors.Join(ratelimit.ErrNoCredentials, errors.New("包んだ側"))
	if !errors.Is(wrapped, ratelimit.ErrNoCredentials) {
		t.Fatalf("errors.Is で ErrNoCredentials を辿れない")
	}
}

// 目的: 呼び出し側の ctx に期限を付けなくても、応答が返らない相手で無期限にブロックしない
// ことを確認する（レビュー指摘の「http.DefaultClient は Timeout を持たない」と同じ問題を
// ratelimit 側でも固定する）。
// 与える情報: HTTPClient に 100 ミリ秒の Timeout を持つクライアントを渡し、
// 応答を1秒遅らせる偽の usage API。
// 成功条件: 期限の無い ctx でも 1 秒未満でエラーが返ること。
func TestFetch_期限のないctxでもクライアント側の上限で打ち切られる(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Second)
		_, _ = w.Write([]byte(`{"limits":[]}`))
	}))
	defer srv.Close()

	home := t.TempDir()
	writeCredentials(t, home, `{"claudeAiOauth":{"accessToken":"ok"}}`)

	reader, err := ratelimit.NewReader(ratelimit.Options{
		Config:     usageConfig(),
		Endpoint:   srv.URL,
		HomeDir:    home,
		HTTPClient: &http.Client{Timeout: 100 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("NewReader が失敗した: %v", err)
	}

	start := time.Now()
	if _, err := reader.Fetch(context.Background()); err == nil {
		t.Fatalf("応答が遅いのにエラーが返らなかった")
	}
	if elapsed := time.Since(start); elapsed > 900*time.Millisecond {
		t.Fatalf("クライアント側の上限が効いていない（%s かかった）", elapsed)
	}
}
