// Package config_test のうち、このファイルは rate_limit.token_source の検査と既定値を扱う。
//
// **`keychain` は macOS でだけ選べる。**`security` は macOS の標準コマンドであり、
// ほかの OS には無い。ここで弾かないと、Linux の運用者は起動時ではなく5分ごとの取得で
// 毎回失敗し、枠の判定が黙って無効化される。
package config_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// 目的: rate_limit.token_source の既定値が OS で分かれることを確認する。
//
// **macOS の Claude Code は資格情報を Keychain に置き、`~/.claude/.credentials.json` は
// 無いのが普通である**（2026-08-21 に実測）。既定が `claude_credentials` のままだと、
// macOS では枠の判定が黙って効かない。
//
// 与える情報: config.DefaultConfig() の値と、走っている OS。
// 成功条件: darwin なら "keychain"、それ以外なら "claude_credentials" であること。
func TestDefaultConfig_rate_limitのtoken_sourceの既定がOSで分かれる(t *testing.T) {
	got := config.DefaultConfig().RateLimit.TokenSource

	want := config.RateLimitTokenSourceClaudeCredentials
	if runtime.GOOS == "darwin" {
		want = config.RateLimitTokenSourceKeychain
	}
	if got != want {
		t.Fatalf("rate_limit.token_source の既定が違う（OS: %s）: got %q, want %q", runtime.GOOS, got, want)
	}
}

// 目的: internal/config が持つ token_source の値と、internal/ratelimit が持つ値が
// 同じ文字列であることを確認する。
//
// **internal/ratelimit は internal/config を読むので、逆向きに参照すると循環する。**
// そのため同じ文字列を2箇所に持たざるを得ない。**ずれたらここで落とす。**
//
// 与える情報: 両方のパッケージの定数。
// 成功条件: 3つとも一致すること。
func TestTokenSource_configとratelimitの値がずれていない(t *testing.T) {
	pairs := []struct {
		name       string
		fromConfig string
		fromReader string
	}{
		{"claude_credentials", config.RateLimitTokenSourceClaudeCredentials, ratelimit.TokenSourceClaudeCredentials},
		{"keychain", config.RateLimitTokenSourceKeychain, ratelimit.TokenSourceKeychain},
		{"env", config.RateLimitTokenSourceEnv, ratelimit.TokenSourceEnv},
	}
	for _, p := range pairs {
		if p.fromConfig != p.fromReader {
			t.Errorf("%s の値がずれている: internal/config=%q, internal/ratelimit=%q",
				p.name, p.fromConfig, p.fromReader)
		}
	}
}

// 目的: `rate_limit.token_source: keychain` が macOS でだけ通り、
// ほかの OS では**起動が止まる**ことを確認する。
//
// **止めないと、Linux の運用者は5分ごとの取得で毎回失敗し、枠の判定が黙って無効化される。**
//
// 与える情報: `token_source: keychain` を書いた front matter。
// 成功条件: darwin なら config.Load が通り、値が読めること。
// darwin 以外なら config.Load が落ち、そのエラー文が設定キーと "macOS" を名指しすること。
func TestLoad_token_sourceのkeychainはmacOSでだけ通る(t *testing.T) {
	front := validFrontMatter + "rate_limit:\n  token_source: keychain\n"

	if runtime.GOOS != "darwin" {
		assertLoadFailsWith(t, front, "rate_limit.token_source")
		path := writeWorkflow(t, front, "")
		_, err := config.Load(path)
		if err == nil {
			t.Fatal("macOS 以外なのに keychain で起動が通ってしまった")
		}
		if !strings.Contains(err.Error(), "macOS") {
			t.Fatalf("エラー文が「macOS でだけ使える」ことを伝えていない: %v", err)
		}
		if !strings.Contains(err.Error(), runtime.GOOS) {
			t.Fatalf("エラー文にいまの OS が入っていない（何が問題か分からない）: %v", err)
		}
		return
	}

	path := writeWorkflow(t, front, "")
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("macOS なのに keychain で起動が止まった: %v", err)
	}
	if got := loaded.Config.RateLimit.TokenSource; got != config.RateLimitTokenSourceKeychain {
		t.Fatalf("rate_limit.token_source が反映されていない: got %q, want %q",
			got, config.RateLimitTokenSourceKeychain)
	}
}

// 目的: 知らない token_source を書いたとき、選べる値を全部名指しして落ちることを確認する。
//
// **`keychain` が案内に出ていないと、macOS の利用者はその選択肢に辿り着けない。**
//
// 与える情報: `token_source: oauth` を書いた front matter。
// 成功条件: config.Load が落ち、そのエラー文に3つの値が全部入っていること。
func TestLoad_未知のtoken_sourceは選べる値を全部並べて落ちる(t *testing.T) {
	front := validFrontMatter + "rate_limit:\n  token_source: oauth\n"
	path := writeWorkflow(t, front, "")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("知らない token_source なのに起動が通ってしまった")
	}
	for _, want := range []string{
		config.RateLimitTokenSourceClaudeCredentials,
		config.RateLimitTokenSourceKeychain,
		config.RateLimitTokenSourceEnv,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("エラー文に選べる値 %q が並んでいない: %v", want, err)
		}
	}
}
