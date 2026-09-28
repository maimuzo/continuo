// Package config_test のうち、このファイルは rate_limit の節（使用率をどこから受けるか）の
// 検査と既定値を扱う（issue #284）。
//
// **使用率はステータスラインから受ける。**`rate_limit.source` は `statusline`（既定）か `none` の
// どちらかで、usage API を読む `oauth_usage_api` と、そのための `token_source` / `token_env` /
// `poll_interval_ms` は消した。**消した値やキーを書いたままの WORKFLOW.md は、起動時の検査で止める。**
// 黙って通すと、利用者は「usage API を読んでいる」つもりのまま、別の経路で動くことになる。
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

// 目的: rate_limit の既定値が、ステータスラインから受ける形であることを確認する。
//
// **既定は `statusline` で、値の古さの上限（statusline取得の間隔）は5分である。**
// 5分は人間が決めた値で、何もしていない機械で1日最大288回の haiku の起動に当たる（issue #284）。
// 既定の巡回の間隔（30秒）より長いので、既定のままで起動時の検査を通る。
//
// 与える情報: config.DefaultConfig() の値。
// 成功条件: Source が "statusline"、RefreshIntervalMs が 300000、PauseAbovePercent が 95 で、
// RefreshIntervalMs が既定の polling.interval_ms より長いこと。
func TestDefaultConfig_rate_limitの既定はstatuslineで5分ごと(t *testing.T) {
	def := config.DefaultConfig()
	if got := def.RateLimit.Source; got != config.RateLimitSourceStatusline {
		t.Errorf("rate_limit.source の既定が違う: got %q, want %q", got, config.RateLimitSourceStatusline)
	}
	if got := def.RateLimit.RefreshIntervalMs; got != 300000 {
		t.Errorf("rate_limit.refresh_interval_ms の既定が違う: got %d, want 300000", got)
	}
	if got := def.RateLimit.PauseAbovePercent; got != 95 {
		t.Errorf("rate_limit.pause_above_percent の既定が違う: got %d, want 95", got)
	}
	if def.RateLimit.RefreshIntervalMs <= def.Polling.IntervalMs {
		t.Errorf("既定の refresh_interval_ms（%d）が既定の polling.interval_ms（%d）以下で、既定のままでは起動できない",
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
// 成功条件: statusline と none のどちらも、両方の値が一致すること。
func TestSource_configとratelimitの値がずれていない(t *testing.T) {
	for _, c := range []struct {
		name      string
		inConfig  string
		inRatelim string
	}{
		{"statusline", config.RateLimitSourceStatusline, ratelimit.SourceStatusline},
		{"none", config.RateLimitSourceNone, ratelimit.SourceNone},
	} {
		if c.inConfig != c.inRatelim || c.inConfig != c.name {
			t.Errorf("%s の値がずれている: internal/config=%q, internal/ratelimit=%q",
				c.name, c.inConfig, c.inRatelim)
		}
	}
}

// 目的: 消した値 `oauth_usage_api` を rate_limit.source に書くと、起動が止まることを確認する。
//
// **usage API は読まなくなった。**書いたまま通すと、利用者は usage API を読んでいるつもりのまま、
// 別の経路で動く。選べる値を案内に並べないと、何に書き換えればよいか分からない。
//
// 与える情報: `rate_limit.source: oauth_usage_api` を書いた front matter。
// 成功条件: config.Load が落ち、エラー文に "rate_limit.source" と、選べる値の
// "statusline" と "none" が入っていること。
func TestLoad_rate_limitのsourceにoauth_usage_apiを書くと起動が止まる(t *testing.T) {
	front := validFrontMatter + "rate_limit:\n  source: oauth_usage_api\n"
	path := writeWorkflow(t, front, "")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("消した oauth_usage_api なのに起動が通ってしまった")
	}
	for _, want := range []string{"rate_limit.source", "statusline", "none"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("エラー文に %q が入っていない: %v", want, err)
		}
	}
}

// 目的: rate_limit から消したキー（token_source / token_env / poll_interval_ms）を書くと、
// 起動が止まることを確認する。
//
// **消したキーは未知のキーとして扱われる**（設計 8-1。front matter の未知のキーで起動を止める）。
// 黙って捨てると、`poll_interval_ms` を縮めたつもりの利用者は、効いていないことに気づけない。
//
// 与える情報: 消したキーを1つずつ、rate_limit の下に書いた front matter。
// 成功条件: どのキーでも config.Load が落ち、エラー文にそのキーの名前が入っていること。
func TestLoad_rate_limitから消したキーを書くと起動が止まる(t *testing.T) {
	for _, c := range []struct {
		key  string
		line string
	}{
		{"token_source", "  token_source: claude_credentials\n"},
		{"token_env", "  token_env: CLAUDE_CODE_OAUTH_TOKEN\n"},
		{"poll_interval_ms", "  poll_interval_ms: 300000\n"},
	} {
		t.Run(c.key, func(t *testing.T) {
			front := validFrontMatter + "rate_limit:\n" + c.line
			path := writeWorkflow(t, front, "")

			_, err := config.Load(path)
			if err == nil {
				t.Fatalf("消したキー rate_limit.%s を書いたのに起動が通ってしまった", c.key)
			}
			if !strings.Contains(err.Error(), c.key) {
				t.Errorf("エラー文がどのキーかを名指ししていない（期待したキー: %s）: %v", c.key, err)
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
