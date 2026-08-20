package orchestrator_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// newUsageServer は Claude の OAuth usage API の代わりに使う偽のサーバを立てる。
//
// **本番の API へは接続しない。**
//
// t: 呼び出し元のテスト。後始末を t.Cleanup に登録する。
// limits: 返す枠の一覧（JSON にそのまま載る形）。
// 戻り値の1つ目: 偽サーバの URL。
// 戻り値の2つ目: 受け取ったリクエストの回数を数えるカウンタ。
func newUsageServer(t *testing.T, limits []map[string]any) (string, *atomic.Int32) {
	t.Helper()
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"limits": limits}); err != nil {
			t.Errorf("偽の usage API が応答を書けません: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &count
}

// newUsageReader は偽の usage API を向いた枠の読み取りを作る。
//
// t: 呼び出し元のテスト。
// endpoint: 偽サーバの URL。
// tokenEnv: トークンを入れた環境変数の名前。
// 戻り値: 組み立てた Reader。
func newUsageReader(t *testing.T, endpoint, tokenEnv string) *ratelimit.Reader {
	t.Helper()
	t.Setenv(tokenEnv, "test-token")
	reader, err := ratelimit.NewReader(ratelimit.Options{
		Config: config.RateLimitConfig{
			Source:            ratelimit.SourceOAuthUsageAPI,
			TokenSource:       ratelimit.TokenSourceEnv,
			TokenEnv:          tokenEnv,
			PauseAbovePercent: 95,
			PollIntervalMs:    1,
		},
		Endpoint: endpoint,
	})
	if err != nil {
		t.Fatalf("ratelimit.NewReader に失敗した: %v", err)
	}
	return reader
}

// TestQuota_100パーセントかつhookが来ていない run だけを枠待ちにする は、
// 枠待ちの判定が2条件の連言であることを確かめる。
//
// 目的: 設計 3-27 の「**この run は枠待ちである**は次の2つが同時に成り立つとき。
// 条件その1: `percent` が 100 に達している。条件その2: その run から `claude.turn_timeout_ms` の
// あいだ hook が1件も来ていない」と、「**`severity` は見ない**」を守っていることを示す。
//
// **条件その2 を入れる理由。**枠を使い切っていても、別の run は動いていることがある。
// 枠の状態だけで全部の run の時計を止めると、固まった run を見逃す。
//
// 与える情報: 枠が100%。hook が来ていない run と、閾値の手前で hook を受けた run。
// 成功条件: 前者だけが枠待ちになり、時計が止まる。後者は枠待ちにならない。
func TestQuota_100パーセントかつhookが来ていないrunだけを枠待ちにする(t *testing.T) {
	resetsAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	endpoint, _ := newUsageServer(t, []map[string]any{
		{"kind": "session", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	})
	reader := newUsageReader(t, endpoint, "CONTINUO_TEST_OAUTH_TOKEN_A")

	fx := newStubFixture(t, stubFixtureOptions{
		AgentStatus: herdr.AgentStatusUnknown,
		RateLimit:   reader,
		Mutate: func(cfg *config.Config) {
			cfg.Claude.TurnTimeoutMs = 50
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
		},
	})
	adoptRun(fx, 188)
	adoptRun(fx, 189)

	// 閾値を跨がせる。**189 だけは直前に hook を受けているので時計が新しい。**
	time.Sleep(120 * time.Millisecond)
	fx.Orc.OnHook(toolHook("session-189", "PostToolUse"))

	fx.Orc.Tick(context.Background())

	v188, ok := viewOf(fx, "maimuzo/koetsumugi#188")
	if !ok {
		t.Fatalf("枠待ちにすべき run が印から外れている")
	}
	if !v188.WaitingQuota {
		t.Fatalf("枠が100%%で hook も来ていないのに枠待ちにしていない: %+v", v188)
	}
	if v188.RetryCount != 0 {
		t.Fatalf("枠待ちの run を stall として殺している: retry_count = %d", v188.RetryCount)
	}

	v189, ok := viewOf(fx, "maimuzo/koetsumugi#189")
	if !ok {
		t.Fatalf("hook を受けている run が印から外れている")
	}
	if v189.WaitingQuota {
		t.Fatalf("hook が来ている run まで枠待ちにしている（固まった run を見逃す）: %+v", v189)
	}
}

// TestQuota_pause_above_percentを超えたら新規のdispatchだけを止める は、
// 「新規を止める閾値」と「この run は枠待ちである」を分けていることを確かめる。
//
// 目的: 設計 3-27 の「`pause_above_percent`（既定95%）を超えただけでは、枠待ちとみなさない。
// **走行中の turn は止めないし、時計も止めない**」を守っていることを示す。
//
// 与える情報: 枠が 96%（100 には達していない）。`Ready` の issue が1件。
// 成功条件: 新規の dispatch が起きず、既にある run は枠待ちにならない。
func TestQuota_pause_above_percentを超えたら新規のdispatchだけを止める(t *testing.T) {
	endpoint, _ := newUsageServer(t, []map[string]any{
		{"kind": "session", "percent": 96, "resets_at": nil, "severity": "normal"},
	})
	reader := newUsageReader(t, endpoint, "CONTINUO_TEST_OAUTH_TOKEN_B")

	fx := newStubFixture(t, stubFixtureOptions{
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.RateLimit.PauseAbovePercent = 95
			cfg.Trust.RequireRepoTrusted = false
		},
	})
	running := adoptRun(fx, 188)
	fx.Tracker.AddIssue(sampleIssue(190, "Ready"))

	fx.Orc.Tick(context.Background())

	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == "maimuzo/koetsumugi#190" {
			t.Fatalf("閾値を超えているのに新規を dispatch している: %+v", v)
		}
		if v.Identifier == running.Identifier && v.WaitingQuota {
			t.Fatalf("95%%を超えただけで走行中の run の時計を止めている: %+v", v)
		}
	}
}

// TestQuota_source_noneならusageAPIを1回も叩かない は、設定の意味を確かめる。
//
// 目的: 設計 3-27 の「`rate_limit.source` に `none` を指定すれば、この API を1回も
// 叩かずに運用できる」を守っていることを示す。
//
// 与える情報: `rate_limit.source: none` の Reader。巡回を3回。
// 成功条件: 偽の usage API へのリクエストが0件になる。
func TestQuota_source_noneならusageAPIを1回も叩かない(t *testing.T) {
	endpoint, count := newUsageServer(t, []map[string]any{
		{"kind": "session", "percent": 100, "resets_at": nil, "severity": "normal"},
	})
	t.Setenv("CONTINUO_TEST_OAUTH_TOKEN_C", "test-token")
	reader, err := ratelimit.NewReader(ratelimit.Options{
		Config: config.RateLimitConfig{
			Source:      ratelimit.SourceNone,
			TokenSource: ratelimit.TokenSourceEnv,
			TokenEnv:    "CONTINUO_TEST_OAUTH_TOKEN_C",
		},
		Endpoint: endpoint,
	})
	if err != nil {
		t.Fatalf("ratelimit.NewReader に失敗した: %v", err)
	}

	fx := newStubFixture(t, stubFixtureOptions{RateLimit: reader})
	adoptRun(fx, 188)
	for i := 0; i < 3; i++ {
		fx.Orc.Tick(context.Background())
	}

	if got := count.Load(); got != 0 {
		t.Fatalf("rate_limit.source が none なのに usage API を %d 回叩いた", got)
	}
	if reader.Enabled() {
		t.Fatalf("rate_limit.source が none なのに Enabled が真である")
	}
}

// TestQuota_資格情報が取れなければ枠の判定を諦めて起動は続ける は、macOS での既定の動きを確かめる。
//
// 目的: 設計 3-15 / 3-27 の「資格情報が取れなかったら、枠の判定を諦めて `none` と同じ動きに
// する。**起動は止めない**」「**macOS では `~/.claude/.credentials.json` が無いのが普通である**
// （Keychain にある）」を守っていることを示す。
//
// 与える情報: `~/.claude/.credentials.json` が無いホームディレクトリ。
// 成功条件: `Fetch` がエラーを返さず nil を返し、以後 `Enabled` が偽になる。
func TestQuota_資格情報が取れなければ枠の判定を諦めて起動は続ける(t *testing.T) {
	endpoint, count := newUsageServer(t, nil)
	reader, err := ratelimit.NewReader(ratelimit.Options{
		Config: config.RateLimitConfig{
			Source:      ratelimit.SourceOAuthUsageAPI,
			TokenSource: ratelimit.TokenSourceClaudeCredentials,
		},
		Endpoint: endpoint,
		// **Keychain は読まない。**このディレクトリには .claude/.credentials.json が無い。
		HomeDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("ratelimit.NewReader に失敗した: %v", err)
	}

	snap, err := reader.Fetch(context.Background())
	if err != nil {
		t.Fatalf("資格情報が無いだけでエラーを返した（起動を止めてはならない）: %v", err)
	}
	if snap != nil {
		t.Fatalf("資格情報が無いのに枠を返した: %+v", snap)
	}
	if reader.Enabled() {
		t.Fatalf("諦めたあとも Enabled が真である")
	}
	if got := count.Load(); got != 0 {
		t.Fatalf("資格情報が無いのに usage API を %d 回叩いた", got)
	}
}

// TestQuota_枠明けにClaudeCodeが自分で継続していたら継続の指示を送らない は、
// 二重投入の防止を確かめる。
//
// 目的: 設計 3-27 の「**Claude Code 2.1.234 は『枠のリセット時にセッションを自動継続する』
// 機能を既定で持つ。**continuo がそこへ継続の指示を送ると二重投入になる。送る前に
// `agent_status` を見る（`working` なら送らない。hook を待つ）」を守っていることを示す。
//
// 与える情報: 1回目の巡回では枠に余裕があり dispatch される。turn を投げたあとに枠が
// 100%（`resets_at` は既に過去）になり、`agent.prompt` が `timeout` で返る。
// `agent.get` は起動の確認のあと `working` を返す。
// 成功条件: `agent.prompt` が1回だけで、枠明けの継続の指示が送られない。
// そのあと `Stop` を流せば turn が終わる。
func TestQuota_枠明けにClaudeCodeが自分で継続していたら継続の指示を送らない(t *testing.T) {
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	var usageReads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		percent := 100
		if usageReads.Add(1) == 1 {
			// 1回目の巡回では枠に余裕がある（dispatch させるため）。
			percent = 50
		}
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{"limits": []map[string]any{
			{"kind": "session", "percent": percent, "resets_at": past, "severity": "normal"},
		}}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Errorf("偽の usage API が応答を書けません: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	reader := newUsageReader(t, srv.URL, "CONTINUO_TEST_OAUTH_TOKEN_D")

	fx := newFixture(t, fixtureOptions{
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.Claude.PollWaitMs = 100
		},
	})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\n\nCONTINUO-STATUS: review", false),
	})

	// agent.prompt は、枠が100%になるまで待ってから timeout のエラー応答で返る
	// （turn_timeout_ms の待ちが枠で切れた状況）。
	released := make(chan struct{})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(map[string]any) (any, *rpcErr) {
		<-released
		return nil, &rpcErr{Code: herdr.ErrCodeTimeout, Message: "timed out waiting for agent"}
	})
	// 段10（起動の確認）では idle、そのあと（**枠が明けたあと**）は
	// **Claude Code が自分で継続している** working を返す。
	var agentGets atomic.Int32
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		status := "working"
		if agentGets.Add(1) == 1 {
			status = "idle"
		}
		return map[string]any{
			"type":  "agent_info",
			"agent": map[string]any{"name": params["target"], "agent_status": status},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "1回目の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// 2回目の巡回で枠が100%になる。そのあと agent.prompt を timeout で返す。
	fx.Orc.Tick(context.Background())
	close(released)

	waitFor(t, 10*time.Second, "枠待ちの待ち直しへ入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentWait) > 0
	})
	time.Sleep(500 * time.Millisecond)

	if got := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); got != 1 {
		t.Fatalf("Claude Code が自分で継続しているのに継続の指示を送った（二重投入になる）: agent.prompt が %d 回", got)
	}

	// hook を待っていること。**Stop を流せば turn が終わる。**
	fx.Tracker.SetState("PVTI_item188", "Done")
	fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
	fx.Orc.OnHook(stopEvent("session-1", path, "p1"))

	waitFor(t, 20*time.Second, "hook を受けて turn が終わる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
}
