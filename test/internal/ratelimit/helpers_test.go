// Package ratelimit_test は internal/ratelimit（Claude の OAuth usage API の読み取り）を
// 検証する。
//
// **本番の usage API へは接続しない。**httptest.Server で偽の usage API を立て、
// Options.Endpoint にその URL を渡す（docs/plans/continuo_design.md 3-15）。
// 資格情報も本物（~/.claude/.credentials.json）は読まない。t.TempDir() に作った
// テスト用ホームディレクトリを Options.HomeDir に渡す。
package ratelimit_test

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// newTestLogger はテスト用の logger と、その出力を溜めるバッファを返す。
// 警告が1回だけ出ることの検査に使う。
//
// 戻り値の1つ目: 出力先のバッファ。
// 戻り値の2つ目: バッファへ書く logger。
func newTestLogger() (*bytes.Buffer, *slog.Logger) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return buf, logger
}

// writeCredentials はテスト用ホームディレクトリに `.claude/.credentials.json` を書く。
//
// t: 呼び出し元のテスト。
// home: テスト用ホームディレクトリ（t.TempDir() の値）。
// body: 書き込む JSON の中身（壊れた JSON も渡せるように文字列で受ける）。
// 戻り値: 書いたファイルの絶対パス。
func writeCredentials(t *testing.T, home, body string) string {
	t.Helper()
	path := filepath.Join(home, ratelimit.CredentialsRelPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("偽の資格情報ディレクトリを作れません: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("偽の資格情報ファイルを書けません: %v", err)
	}
	return path
}

// usageConfig は usage API を読む設定（資格情報はファイルから）を返す。
func usageConfig() config.RateLimitConfig {
	return config.RateLimitConfig{
		Source:            ratelimit.SourceOAuthUsageAPI,
		TokenSource:       ratelimit.TokenSourceClaudeCredentials,
		PauseAbovePercent: 90,
		PollIntervalMs:    300000,
	}
}

// isKeychainTimeout は、エラーが「`security` が期限内に返らなかった」に辿れるかを返す。
//
// err: 検査するエラー。
// 戻り値: ratelimit.ErrKeychainTimeout へ辿れれば true。
func isKeychainTimeout(err error) bool {
	return errors.Is(err, ratelimit.ErrKeychainTimeout)
}

// isNoCredentials は、エラーが「資格情報を取得できない」に辿れるかを返す。
//
// err: 検査するエラー。
// 戻り値: ratelimit.ErrNoCredentials へ辿れれば true。
func isNoCredentials(err error) bool {
	return errors.Is(err, ratelimit.ErrNoCredentials)
}
