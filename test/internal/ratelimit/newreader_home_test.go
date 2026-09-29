package ratelimit_test

import (
	"testing"

	"github.com/maimuzo/continuo/internal/ratelimit"
)

// 目的: usage API を読まない設定（`rate_limit.source` が statusline か none）では、NewReader が
// ホームディレクトリを引かないことを確かめる（issue #284。PR #294 の実装レビュー1周目）。
// statusline と none はトークンを1回も読まないので、HOME を引けない環境（HOME を渡さない
// サービスの管理から起動した場合など）でも起動を止めてはならない。
// 与える情報: HOME を空にした環境と、token_source が claude_credentials の設定。
// 成功条件: statusline と none では NewReader が成功し、oauth_usage_api では失敗すること
// （oauth_usage_api が失敗することで、この環境で本当にホームディレクトリを引けないことも確かめる）。
func TestNewReader_usage_APIを読まない設定ならHOMEを引けなくても組み立てられる(t *testing.T) {
	t.Setenv("HOME", "")
	for _, tc := range []struct {
		source  string
		wantErr bool
	}{
		{source: ratelimit.SourceStatusline, wantErr: false},
		{source: ratelimit.SourceNone, wantErr: false},
		{source: ratelimit.SourceOAuthUsageAPI, wantErr: true},
	} {
		t.Run(tc.source, func(t *testing.T) {
			cfg := usageConfig()
			cfg.Source = tc.source
			_, err := ratelimit.NewReader(ratelimit.Options{Config: cfg})
			if tc.wantErr && err == nil {
				t.Fatalf("source: %s で HOME を引けないのに NewReader が成功した（前提が崩れている）", tc.source)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("source: %s は usage API を読まないのに NewReader が失敗した: %v", tc.source, err)
			}
		})
	}
}
