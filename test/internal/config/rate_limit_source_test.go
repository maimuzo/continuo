// Package config_test のうち、このファイルは rate_limit の節（使用率をどこから受けるか）の
// 検査と既定値を扱う（issue #284）。
//
// **`rate_limit.source` は `oauth_usage_api`（既定）・`statusline`・`none` のどれかである。**
// `oauth_usage_api` は usage API を主に読み、エラーのときは statusline取得へ切り替える。
// **v0.1.15 までの WORKFLOW.md（token_source / token_env / poll_interval_ms を書いたもの）は
// そのまま通す。**
package config_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// rateLimitFrontMatter は、polling.interval_ms と rate_limit の節を足した front matter を組み立てる。
//
// intervalMs: polling.interval_ms に書く値。
// rateLimit: rate_limit の直下に足す行（インデント2文字から始め、末尾は "\n" で終えること）。
// 戻り値: front matter の YAML 本体。
func rateLimitFrontMatter(intervalMs int, rateLimit string) string {
	return validFrontMatter +
		fmt.Sprintf("polling:\n  interval_ms: %d\n", intervalMs) +
		"rate_limit:\n" + rateLimit
}

// v0115RateLimit は v0.1.15 の雛形（`continuo init`）が書いた rate_limit の節である（値とキーをそのまま写した）。
//
// **`pause_above_percent` を含む。**このキーは v0.2.0 で消えたので、
// **この節をそのまま渡すと起動が止まる**（issue #173 の破壊的変更）。
const v0115RateLimit = "rate_limit:\n" +
	"  source: oauth_usage_api\n" +
	"  token_source: claude_credentials\n" +
	"  token_env: CLAUDE_CODE_OAUTH_TOKEN\n" +
	"  pause_above_percent: 95\n" +
	"  poll_interval_ms: 300000\n"

// 目的: rate_limit の既定値が、usage API を主に読む形であることを確認する。
//
// **既定は `oauth_usage_api` で、usage API を読む間隔と値の古さの上限（statusline取得の間隔）は
// どちらも5分である。**値の古さの上限は既定の巡回の間隔（30秒）より長いので、既定のままで
// `polling.interval_ms` の2倍として扱われることはない。
//
// 与える情報: config.DefaultConfig() の値。
// 成功条件: Source が "oauth_usage_api"、PollIntervalMs と RefreshIntervalMs が 300000、
// TokenEnv が "CLAUDE_CODE_OAUTH_TOKEN" で、RefreshIntervalMs が
// 既定の polling.interval_ms より長いこと。
//
// **`pause_above_percent` は見ない。**キーごと消えた（人間の決定。2026-09-06。issue #173）。
func TestDefaultConfig_rate_limitの既定はoauth_usage_apiで5分ごと(t *testing.T) {
	def := config.DefaultConfig()
	if got := def.RateLimit.Source; got != config.RateLimitSourceOAuthUsageAPI {
		t.Errorf("rate_limit.source の既定が違う: got %q, want %q", got, config.RateLimitSourceOAuthUsageAPI)
	}
	if got := def.RateLimit.PollIntervalMs; got != 300000 {
		t.Errorf("rate_limit.poll_interval_ms の既定が違う: got %d, want 300000", got)
	}
	if got := def.RateLimit.RefreshIntervalMs; got != 300000 {
		t.Errorf("rate_limit.refresh_interval_ms の既定が違う: got %d, want 300000", got)
	}
	if got := def.RateLimit.TokenEnv; got != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Errorf("rate_limit.token_env の既定が違う: got %q", got)
	}
	if def.RateLimit.RefreshIntervalMs <= def.Polling.IntervalMs {
		t.Errorf("既定の refresh_interval_ms（%d）が既定の polling.interval_ms（%d）以下である",
			def.RateLimit.RefreshIntervalMs, def.Polling.IntervalMs)
	}
}

// 目的: internal/config が持つ rate_limit.source の値と、internal/ratelimit が持つ値が
// 同じ文字列であることを確認する。
//
// **internal/ratelimit は internal/config を読むので、逆向きに参照すると循環する。**
// そのため同じ文字列を2か所に書いており、片方だけ直すと `source: none` が効かなくなる。
//
// 与える情報: 両 package の定数。
// 成功条件: oauth_usage_api・statusline・none のどれも、両方の値が一致すること。
func TestSource_configとratelimitの値がずれていない(t *testing.T) {
	for _, c := range []struct {
		name      string
		inConfig  string
		inRatelim string
	}{
		{"oauth_usage_api", config.RateLimitSourceOAuthUsageAPI, ratelimit.SourceOAuthUsageAPI},
		{"statusline", config.RateLimitSourceStatusline, ratelimit.SourceStatusline},
		{"none", config.RateLimitSourceNone, ratelimit.SourceNone},
	} {
		if c.inConfig != c.inRatelim || c.inConfig != c.name {
			t.Errorf("%s の値がずれている: internal/config=%q, internal/ratelimit=%q",
				c.name, c.inConfig, c.inRatelim)
		}
	}
}

// 目的: rate_limit.source に知らない値を書くと起動が止まり、選べる値が案内に並ぶことを確認する。
//
// 与える情報: `rate_limit.source: usage_api`（知らない値）を書いた front matter。
// 成功条件: config.Load が落ち、エラー文に "rate_limit.source" と、選べる値の
// "oauth_usage_api" と "statusline" と "none" が入っていること。
func TestLoad_rate_limitのsourceに知らない値を書くと起動が止まる(t *testing.T) {
	front := validFrontMatter + "rate_limit:\n  source: usage_api\n"
	path := writeWorkflow(t, front, "")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("知らない値なのに起動が通ってしまった")
	}
	for _, want := range []string{"rate_limit.source", "oauth_usage_api", "statusline", "none"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("エラー文に %q が入っていない: %v", want, err)
		}
	}
}

// 目的: v0.1.15 の雛形が書いた `pause_above_percent` の行は、消すまで起動しないことを確認する
// （issue #173。破壊的変更）。
//
// **front matter は知らないキーで起動を止める**（`yaml.Strict()`）。
// **このキーは `continuo init` の雛形がずっと書いてきたので、手を入れていない `WORKFLOW.md` にも入っている。**
// **移行の手順は [docs/upgrading.md](../../../docs/upgrading.md) にある。**
//
// **止める側に倒す理由。**黙って捨てると、**利用者は「95%で止まる」と読んだままマージンを触らない。**
// **実際に止まるのは 90%（マージン既定10）なので、設定と挙動が食い違ったまま動き続ける。**
//
// 与える情報: v0.1.15 の雛形の rate_limit の節（`pause_above_percent` を含む）を書いた front matter と、
// その1行だけを消した front matter。
// 成功条件: 前者は起動が止まり、誤りの文面にキーの名前が出ること。後者は通り、書いた値がそのまま読めること。
func TestLoad_v0_1_15の雛形のpause_above_percentは消すまで起動しない(t *testing.T) {
	t.Run("消していないと止まる", func(t *testing.T) {
		path := writeWorkflow(t, validFrontMatter+v0115RateLimit, "")

		if _, err := config.Load(path); err == nil {
			t.Fatal("知らないキーがあるのに起動が止まらなかった")
		} else if !strings.Contains(err.Error(), "pause_above_percent") {
			t.Errorf("誤りの文面にキーの名前が出ていない: %v", err)
		}
	})

	t.Run("消すと通る", func(t *testing.T) {
		front := strings.ReplaceAll(v0115RateLimit, "  pause_above_percent: 95\n", "")
		path := writeWorkflow(t, validFrontMatter+front, "")

		loaded, err := config.Load(path)
		if err != nil {
			t.Fatalf("1行消したのに起動が止まった: %v", err)
		}
		rl := loaded.Config.RateLimit
		if rl.Source != config.RateLimitSourceOAuthUsageAPI {
			t.Errorf("rate_limit.source が読めていない: got %q", rl.Source)
		}
		if rl.TokenSource != config.RateLimitTokenSourceClaudeCredentials {
			t.Errorf("rate_limit.token_source が読めていない: got %q", rl.TokenSource)
		}
		if rl.TokenEnv != "CLAUDE_CODE_OAUTH_TOKEN" {
			t.Errorf("rate_limit.token_env が読めていない: got %q", rl.TokenEnv)
		}
		if rl.PollIntervalMs != 300000 {
			t.Errorf("rate_limit.poll_interval_ms が読めていない: got %d", rl.PollIntervalMs)
		}
		if rl.RefreshIntervalMs != 300000 {
			t.Errorf("rate_limit.refresh_interval_ms が既定になっていない: got %d", rl.RefreshIntervalMs)
		}
	})
}

// 目的: `source: oauth_usage_api` なら、refresh_interval_ms が polling.interval_ms 以下でも
// 起動が止まらないことを確認する（issue #284）。
//
// **止めると、v0.1.15 までの設定で巡回の間隔を長くした人が起動できなくなる。**orchestrator が
// polling.interval_ms の2倍として扱い、起動時に WARN を1回出す（test/internal/orchestrator が見る）。
//
// 与える情報: source を oauth_usage_api にし、polling.interval_ms を 30000、refresh_interval_ms を
// 30000（同じ）と 10000（短い）にした front matter。
// 成功条件: どちらも config.Load が成功し、書いた値のまま読めること。
func TestLoad_oauth_usage_apiならrefresh_interval_msが巡回の間隔以下でも起動する(t *testing.T) {
	for _, refreshMs := range []int{30000, 10000} {
		t.Run(fmt.Sprint(refreshMs), func(t *testing.T) {
			front := rateLimitFrontMatter(30000,
				fmt.Sprintf("  source: oauth_usage_api\n  refresh_interval_ms: %d\n", refreshMs))
			path := writeWorkflow(t, front, "")

			loaded, err := config.Load(path)
			if err != nil {
				t.Fatalf("oauth_usage_api なのに refresh_interval_ms を理由に起動が止まった: %v", err)
			}
			if got := loaded.Config.RateLimit.RefreshIntervalMs; got != refreshMs {
				t.Errorf("rate_limit.refresh_interval_ms が読めていない: got %d, want %d", got, refreshMs)
			}
		})
	}
}

// 目的: `source: statusline` のとき、refresh_interval_ms が polling.interval_ms 以下なら
// 起動が止まることを確認する。
//
// **短いと、巡回のたびに statusline取得が走る。**1日に何百回も haiku を起動し、枠を削る。
// 同じ長さも「以下」に入る（同じだと、巡回のたびに値が古くなりうる）。
//
// 与える情報: polling.interval_ms を 30000 にし、refresh_interval_ms を
// 30000（同じ）と 10000（短い）にした front matter。
// 成功条件: どちらも config.Load が落ち、エラー文に "rate_limit.refresh_interval_ms" と
// "polling.interval_ms" が入っていること。
func TestLoad_statuslineでrefresh_interval_msが巡回の間隔以下なら起動が止まる(t *testing.T) {
	for _, c := range []struct {
		name      string
		refreshMs int
	}{
		{"巡回の間隔と同じ", 30000},
		{"巡回の間隔より短い", 10000},
	} {
		t.Run(c.name, func(t *testing.T) {
			front := rateLimitFrontMatter(30000,
				fmt.Sprintf("  source: statusline\n  refresh_interval_ms: %d\n", c.refreshMs))
			path := writeWorkflow(t, front, "")

			_, err := config.Load(path)
			if err == nil {
				t.Fatalf("refresh_interval_ms（%d）が polling.interval_ms（30000）以下なのに起動が通ってしまった", c.refreshMs)
			}
			for _, want := range []string{"rate_limit.refresh_interval_ms", "polling.interval_ms"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("エラー文に %q が入っていない: %v", want, err)
				}
			}
		})
	}
}

// 目的: `source: statusline` のとき、refresh_interval_ms が polling.interval_ms より
// 1ミリ秒でも長ければ起動が通ることを確認する（上の検査が効きすぎていないこと）。
//
// 与える情報: polling.interval_ms を 30000、refresh_interval_ms を 30001 にした front matter。
// 成功条件: config.Load が成功し、source と refresh_interval_ms が書いたとおりに読めること。
func TestLoad_statuslineでrefresh_interval_msが巡回の間隔より長ければ起動する(t *testing.T) {
	front := rateLimitFrontMatter(30000, "  source: statusline\n  refresh_interval_ms: 30001\n")
	path := writeWorkflow(t, front, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("refresh_interval_ms が polling.interval_ms より長いのに起動が止まった: %v", err)
	}
	if got := loaded.Config.RateLimit.Source; got != config.RateLimitSourceStatusline {
		t.Errorf("rate_limit.source が読めていない: got %q", got)
	}
	if got := loaded.Config.RateLimit.RefreshIntervalMs; got != 30001 {
		t.Errorf("rate_limit.refresh_interval_ms が読めていない: got %d, want 30001", got)
	}
}

// 目的: `source: none` なら、refresh_interval_ms が polling.interval_ms 以下でも起動が
// 止まらないことを確認する。
//
// **none は statusline取得をしないので、間隔の決まりは効かない。**
// Pro / Max 以外の契約と API キーの人は none にするしかなく、そこで関係の無い値を理由に
// 止めてはならない。
//
// 与える情報: source を none にし、polling.interval_ms を 30000、refresh_interval_ms を
// 10000 にした front matter。
// 成功条件: config.Load が成功し、source が "none" のまま読めること。
func TestLoad_noneならrefresh_interval_msが巡回の間隔以下でも起動する(t *testing.T) {
	front := rateLimitFrontMatter(30000, "  source: none\n  refresh_interval_ms: 10000\n")
	path := writeWorkflow(t, front, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("source: none なのに refresh_interval_ms を理由に起動が止まった: %v", err)
	}
	if got := loaded.Config.RateLimit.Source; got != config.RateLimitSourceNone {
		t.Errorf("rate_limit.source が読めていない: got %q", got)
	}
}
