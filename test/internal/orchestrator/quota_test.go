// {"RUCM-CFG-SHA256": "b93e1494e785db49e6829871584330f7cffbb630f0c44be8f295124fbc2e6319", "SOURCE": "docs/spec/usecases/particular_case/レートリミットで待って再開する.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**「レートリミットで待って再開する」の
// 21本のパスは、9通りの結末の組み合わせである。**終端フローごとに代表を1本ずつ**対応づける。
package orchestrator_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/internal/tracker"
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
			Source:         ratelimit.SourceOAuthUsageAPI,
			TokenSource:    ratelimit.TokenSourceEnv,
			TokenEnv:       tokenEnv,
			PollIntervalMs: 1,
		},
		Endpoint: endpoint,
	})
	if err != nil {
		t.Fatalf("ratelimit.NewReader に失敗した: %v", err)
	}
	return reader
}

// {"RUCM-PATH": "P001"}
//
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

	v188, ok := viewOf(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatalf("枠待ちにすべき run が印から外れている")
	}
	if !v188.WaitingQuota {
		t.Fatalf("枠が100%%で hook も来ていないのに枠待ちにしていない: %+v", v188)
	}
	if v188.RetryCount != 0 {
		t.Fatalf("枠待ちの run を stall として殺している: retry_count = %d", v188.RetryCount)
	}

	v189, ok := viewOf(fx, "octocat/hello-world#189")
	if !ok {
		t.Fatalf("hook を受けている run が印から外れている")
	}
	if v189.WaitingQuota {
		t.Fatalf("hook が来ている run まで枠待ちにしている（固まった run を見逃す）: %+v", v189)
	}
}

// {"RUCM-PATH": "P004"}
//
// TestQuota_余裕値が0以下でも走行中のrunの時計は止めない は、
// 「新規の着手を止めること」と「この run は枠待ちである」を分けていることを確かめる。
//
// 目的: 設計 3-27 の「**走行中の turn は止めないし、時計も止めない**」を守っていることを示す。
// **枠待ちの条件は2つの連言である**ので、余裕値が0以下でも、
// **hook が来ている run の時計は止まらない。**
//
// 与える情報: 5時間の枠が 96%（マージン10なので余裕値は −6）。`Ready` の issue が1件。
// 成功条件: 新規の dispatch が起きず、既にある run は枠待ちにならない。
func TestQuota_余裕値が0以下でも走行中のrunの時計は止めない(t *testing.T) {
	endpoint, _ := newUsageServer(t, []map[string]any{
		{"kind": "session", "percent": 96, "resets_at": nil, "severity": "normal"},
	})
	reader := newUsageReader(t, endpoint, "CONTINUO_TEST_OAUTH_TOKEN_B")

	fx := newStubFixture(t, stubFixtureOptions{
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.Trust.RequireRepoTrusted = false
		},
	})
	running := adoptRun(fx, 188)
	fx.Tracker.AddIssue(sampleIssue(190, "Ready"))

	fx.Orc.Tick(context.Background())

	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == "octocat/hello-world#190" {
			t.Fatalf("余裕値が0以下なのに新規を dispatch している: %+v", v)
		}
		if v.Identifier == running.Identifier && v.WaitingQuota {
			t.Fatalf("hook が来ている走行中の run の時計を止めている: %+v", v)
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

// {"RUCM-PATH": "P002"}
//
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
	//
	// **空の `Stop` を流したあとは idle へ戻す。**本物の herdr は、書き終えて
	// `Stop` hook が通ったエージェントを `working` のままにしない
	// （[docs/evidence/stop_hook_block_20260902.md](../../../docs/evidence/stop_hook_block_20260902.md)
	// の実測では、最後の `Stop` の 0.09 秒後に `idle` へ落ちた）。
	// **`working` のままにすると、turn の終わりの裏取り（3-79）が
	// 「まだ書き直している」と読んで待ち続ける。**それは偽物だけで起きる状態である。
	var agentGets atomic.Int32
	var stopped atomic.Bool
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		status := "working"
		if agentGets.Add(1) == 1 || stopped.Load() {
			status = "idle"
		}
		return map[string]any{
			"type": "agent_info",
			"agent": map[string]any{
				"name": params["target"], "agent_status": status,
				"interactive_ready": status == "idle" || status == "done",
			},
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
	stopped.Store(true)
	fx.Orc.OnHook(stopEvent("session-1", path, "p1"))

	waitFor(t, 20*time.Second, "hook を受けて turn が終わる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
}

// {"RUCM-PATH": "P008"}
//
// TestQuota_枠を使い切っていなければ待ち直さない は、枠待ちの条件その1 を確かめる。
//
// **枠待ちの判定は2つの条件をどちらも満たすときだけ立つ**（設計 3-27）。
// **1つ目は「使い切っている枠が1つでもあること」。**
// 使い切っていないのに待ち直すと、動いていない run を永久に抱える。
//
// 目的: 枠に余裕があるとき、枠待ちにしないこと。
// 与える情報: 使用率 50% を返す usage API と、hook を1件も受けていない run。
// 成功条件: 枠待ちの印が立たないこと。
func TestQuota_枠を使い切っていなければ待ち直さない(t *testing.T) {
	resetsAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	endpoint, _ := newUsageServer(t, []map[string]any{
		{"kind": "session", "percent": 50, "resets_at": resetsAt, "severity": "normal"},
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

	time.Sleep(120 * time.Millisecond)
	fx.Orc.Tick(context.Background())

	v, ok := viewOf(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatal("run が印から外れている")
	}
	if v.WaitingQuota {
		t.Errorf("枠に余裕があるのに枠待ちにしている: %+v", v)
	}
}

// TestQuota_resets_atがnullの枠は待ち時間を決められない は、リセット時刻の扱いを確かめる。
//
// **`resets_at` が null の枠がある**（設計 3-27）。
// **いつ明けるか分からないものを「待つ」と決めると、永久に待つ run ができる。**
//
// 目的: 使い切っている枠の `resets_at` が null のとき、その時刻を待ち時間にしないこと。
// 与える情報: `percent: 100` かつ `resets_at: null` の枠。
// 成功条件: 落ちずに巡回が回りきること（**時刻を決められないまま先へ進まない**）。
func TestQuota_resets_atがnullの枠は待ち時間を決められない(t *testing.T) {
	endpoint, _ := newUsageServer(t, []map[string]any{
		{"kind": "session", "percent": 100, "resets_at": nil, "severity": "normal"},
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

	time.Sleep(120 * time.Millisecond)
	// **落ちないことを確かめる。**時刻を決められないまま進むと、ここで panic するか固まる。
	fx.Orc.Tick(context.Background())
}

// **CFG のパスに対応づけない。**このユースケース記述は「走っている run が枠明けを待って
// 再開するまで」を書いたもので、**新しい issue を取るかどうかの門は1段も持っていない。**
// 対応づけると、無関係なパスに代表を立てたことになる。
//
// TestQuota_枠を読めなければ入札の要るissueには着手しない は、2つの門を1つに揃えたことを
// 確かめる（設計 3-77j。issue #173）。
//
// 目的: **枠を読めないとき、入札は「黙る」、新規 dispatch は「止めない」で逆を向いていた。**
// 入札が先に効くので後ろは一度も効かず、**ボードが1件も進まないのに出るのは `Debug` の1行だけ**
// だった。**判定を1つに揃え、既定の水準で理由を出すことを示す。**
//
// 与える情報: usage API が 500 を返す（枠を読めない）。担当者のいない `Ready` の issue が1件。
// 成功条件: その issue が dispatch されず、`Info` で理由が出ること。
func TestQuota_枠を読めなければ入札の要るissueには着手しない(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	reader := newUsageReader(t, srv.URL, "CONTINUO_TEST_OAUTH_TOKEN_UNREADABLE")

	fx := newStubFixture(t, stubFixtureOptions{
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.Trust.RequireRepoTrusted = false
		},
	})
	fx.Tracker.AddIssue(sampleIssue(190, "Ready"))

	fx.Orc.Tick(context.Background())

	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == "octocat/hello-world#190" {
			t.Fatalf("枠を読めないのに入札の要る issue へ着手している: %+v", v)
		}
	}
	// **止まったことが人間に見えなければ、直したことにならない。**
	got := fx.Logs.String()
	if !strings.Contains(got, "level=INFO") || !strings.Contains(got, "枠を読めないので") {
		t.Fatalf("止めたことを INFO で出していない:\n%s", got)
	}
	if !strings.Contains(got, "枠を読めない") {
		t.Fatalf("止めた理由を出していない:\n%s", got)
	}
	// **直し方を取り違えさせない。**枠を読めないのは資格情報の話であって、
	// **マージンをいくら下げても動き出さない。**
	if !strings.Contains(got, "マージンを下げても動き出しません") {
		t.Fatalf("枠を読めないときに、マージンでは直らないと書いていない:\n%s", got)
	}
}

// TestQuota_枠を読めなくても自分が担当のissueには着手する は、巡回を打ち切っていないことを
// 確かめる（設計 3-77j。issue #173）。
//
// 目的: **枠を読めないだけで巡回を打ち切ってはならない。**打ち切ると、
// **この機械が既に担当者になっている issue まで着手されなくなる**（印が無いのでこの経路からしか
// 拾えない）。**期限切れの担当を外す経路も通らない。**
//
// 与える情報: usage API が 500 を返す。**この機械（gh の持ち主）が担当者の `Ready` の issue が1件。**
// 成功条件: その issue が dispatch されること。
func TestQuota_枠を読めなくても自分が担当のissueには着手する(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	reader := newUsageReader(t, srv.URL, "CONTINUO_TEST_OAUTH_TOKEN_MINE")

	fx := newStubFixture(t, stubFixtureOptions{
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.Trust.RequireRepoTrusted = false
		},
	})
	fx.Tracker.AddIssue(assignedIssue(191, "Ready", testGHLogin))

	fx.Orc.Tick(context.Background())

	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == "octocat/hello-world#191" {
			return
		}
	}
	t.Fatalf("既に自分が担当の issue にまで着手していない（枠を読めないだけで巡回を打ち切っている）:\n%s",
		fx.Logs.String())
}

// TestQuota_枠が逼迫していても担当が自分のissueには着手する は、
// 止める範囲が入札の要る issue だけであることを確かめる（設計 3-27。issue #173）。
//
// 目的: **巡回を丸ごと打ち切ってはならない。**
// **以前は `rate_limit.pause_above_percent` を超えると `dispatchCandidates` が即 `return` していた。**
// **その設定は消えた**（人間の決定。2026-09-06）。**打ち切ると、この機械が既に担当者に
// なっている issue まで着手されなくなる**（印が無いのでこの経路からしか拾えない）。
// **再起動で復元した run も拾えない**（`restart.orphan_running_action` の既定 `redispatch` は
// 復元では何もせず、次の巡回に委ねる）。**`handoffGate` の中にある「期限切れの担当を外す」
// 経路も通らなくなる**ので、詰まったカンバンを誰も解けない。
//
// 与える情報: 1回目は 99% を返し、2回目以降は 500 を返す usage API。
// **この機械が担当者の `Ready` の issue が1件**（入札を要さない経路）。
// 成功条件: その issue に着手すること。
func TestQuota_枠が逼迫していても担当が自分のissueには着手する(t *testing.T) {
	var reads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if reads.Add(1) > 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{"limits": []map[string]any{
			{"kind": "session", "percent": 99, "resets_at": nil, "severity": "normal"},
		}}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Errorf("偽の usage API が応答を書けません: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	reader := newUsageReader(t, srv.URL, "CONTINUO_TEST_OAUTH_TOKEN_STALE")

	fx := newStubFixture(t, stubFixtureOptions{
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.Trust.RequireRepoTrusted = false
		},
	})
	fx.Tracker.AddIssue(assignedIssue(192, "Ready", testGHLogin))

	// 1回目で 99% を読み、2回目からは読めなくなる。
	fx.Orc.Tick(context.Background())
	fx.Orc.Tick(context.Background())

	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == "octocat/hello-world#192" {
			return
		}
	}
	t.Fatalf("担当が自分の issue にまで着手していない（巡回を丸ごと打ち切っている）:\n%s",
		fx.Logs.String())
}

// TestQuota_マージンが先に効いて止まり使用率と閾値が出る は、出す1行の中身を確かめる
// （設計 3-77j。issue #173）。
//
// 目的: **新規着手が止まる使用率は `100 − マージン` である。**
// マージン10なら **90% から**である（`rate_limit.pause_above_percent` は消えた。issue #173）。
// **観測した使用率と、枠ごとの閾値の両方を出さないと、どちらの枠が原因かを読めない。**
//
// 与える情報: 1週間の枠が 92%。担当者のいない `Ready` の issue が1件。
// 成功条件: dispatch されず、使用率と閾値が1行に出ること。
func TestQuota_マージンが先に効いて止まり使用率と閾値が出る(t *testing.T) {
	endpoint, _ := newUsageServer(t, []map[string]any{
		{"kind": "session", "percent": 30, "resets_at": nil, "severity": "normal"},
		{"kind": "weekly_all", "percent": 92, "resets_at": nil, "severity": "normal"},
	})
	reader := newUsageReader(t, endpoint, "CONTINUO_TEST_OAUTH_TOKEN_MARGIN")

	fx := newStubFixture(t, stubFixtureOptions{
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.Tracker.Provider.Handoff.FiveHourMarginPercent = 10
			cfg.Tracker.Provider.Handoff.WeeklyMarginPercent = 10
			cfg.Trust.RequireRepoTrusted = false
		},
	})
	fx.Tracker.AddIssue(sampleIssue(193, "Ready"))

	fx.Orc.Tick(context.Background())

	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == "octocat/hello-world#193" {
			t.Fatalf("余裕値がマイナスなのに着手している: %+v", v)
		}
	}
	got := fx.Logs.String()
	for _, want := range []string{
		"余裕値が0以下",
		"1週間の枠の使用率=92",
		"5時間の枠の使用率=30",
		`1週間の枠の閾値="90% に達したら止まります"`,
		`5時間の枠の閾値="90% に達したら止まります"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("1行に %q が入っていない:\n%s", want, got)
		}
	}
}

// weeklyWaitFixture は「1週間の枠を待つ上限」の検査で使う一式を組み立てる（issue #197）。
//
// **担当者はこの機械（gh の持ち主）である。**手放す相手が自分でないと、外す対象が見つからない。
// **枠待ちの条件その2（turn_timeout_ms のあいだ hook が来ていない）は、時計を進めて作る。**
//
// t: 呼び出し元のテスト。
// limits: usage API が返す枠の一覧。
// limitMinutes: `rate_limit.weekly_wait_limit_minutes` に入れる値。
// tokenEnv: トークンを入れる環境変数の名前（テストごとに変える）。
// 戻り値: 組み立てた一式・印へ入れた issue・進められる時計。
func weeklyWaitFixture(
	t *testing.T, limits []map[string]any, limitMinutes int, tokenEnv string,
) (*stubFixture, tracker.Issue, *testClock) {
	t.Helper()
	endpoint, _ := newUsageServer(t, limits)
	reader := newUsageReader(t, endpoint, tokenEnv)
	clock := newTestClock()

	fx := newStubFixture(t, stubFixtureOptions{
		// **「止まっている」を表す状態にする**（issue #197）。
		// **`unknown` では手放さない。**herdr が状態を判定できないという意味であり、
		// **確かめられていないのに pane を閉じて担当を外すことになる。**
		AgentStatus: herdr.AgentStatusIdle,
		RateLimit:   reader,
		Now:         clock.Now,
		Mutate: func(cfg *config.Config) {
			cfg.Claude.TurnTimeoutMs = 60000
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.RateLimit.WeeklyWaitLimitMinutes = limitMinutes
		},
	})

	// **担当者をこの機械にした issue を、印へ入れる。**
	issue := assignedIssue(188, "In Progress", testGHLogin)
	fx.Tracker.AddIssue(issue)
	fx.Orc.Adopt(issue, orchestrator.AdoptedRun{
		AgentName:        normalize.SafeName("continuo-hello-world-188"),
		PaneID:           "w1:p1",
		SessionUUID:      "session-188",
		HerdrWorkspaceID: "w1",
	}, false)

	// **枠待ちの条件その2 を満たす**（turn_timeout_ms のあいだ hook が来ていない）。
	clock.Advance(2 * time.Minute)
	return fx, issue, clock
}

// tickOnce は巡回を1回だけ回す（issue #197）。
//
// **1回では手放さない。**画面の版を初めて見た巡回では「そこからどれだけ止まっていたか」が
// 分からないので、**次の巡回まで待つ**（設計 3-27 の段0b）。
// **窓を満たすまで回すのは `waitForRelease` である。**
//
// fx: 対象の一式。
func tickOnce(fx *stubFixture) {
	fx.Orc.Tick(context.Background())
}

// waitForRelease は、担当を手放して印から外れるまで巡回を回す（issue #197）。
//
// **1回の巡回では手放さない。**画面の版を初めて見た巡回では
// 「そこからどれだけ止まっていたか」が分からないので、**次の巡回まで待つ**
// （設計 3-27 の段0b）。**手放しは別の goroutine で走る**ので、
// **巡回を止めて待つのではなく、時計を進めながら巡回を回し続ける。**
//
// t: 呼び出し元のテスト。
// fx: 対象の一式。
// clock: 進められる時計。
// identifier: 対象の issue の識別子。
func waitForRelease(t *testing.T, fx *stubFixture, clock *testClock, identifier string) {
	t.Helper()
	for i := 0; i < 60; i++ {
		if _, ok := viewOf(fx, identifier); !ok {
			return
		}
		clock.Advance(2 * time.Minute)
		fx.Orc.Tick(context.Background())
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("担当を手放して印から外れませんでした:\n%s", fx.Logs.String())
}

// assigneeLoginsOf は、いまボードに載っている担当者のログイン名を返す。
//
// fx: 対象の一式。
// id: issue の ID。
// 戻り値: 担当者のログイン名。
func assigneeLoginsOf(fx *stubFixture, id string) []string {
	issue, ok := fx.Tracker.IssueByID(id)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(issue.Assignees))
	for _, a := range issue.Assignees {
		out = append(out, a.Login)
	}
	return out
}

// TestQuota_画面が動いていれば枠待ちと判定しない は、stall の評価順を確かめる
// （設計 3-27。issue #197）。
//
// 目的: **枠待ちの条件は「使用率が100」と「hook が来ていない」の2つで、
// 「枠を待っている」と「長い1つの仕事をしている」を区別できない。**
// hook はツールが終わってから飛ぶので、**1時間を超える1回のツール呼び出しの最中は1件も来ない。**
// **そこへ1週間のモデル別の枠が100%だと条件が両方そろい、正常に走っている run が枠待ちと名乗る。**
// **stall の時計が止まったまま戻らないので、そのあと本当に固まっても誰も止められない。**
//
// **専用の仕組みは持たない。**`checkStalls` の評価順で、画面の版を枠待ちの判定より前に置く。
//
// 与える情報: 1週間のモデル別の枠が 100% で、リセットは48時間後。上限は300分。**画面の版が増えている。**
// 成功条件: 枠待ちと判定しないこと。印から外れないこと。担当者が残っていること。
//
// **CFG のパスに対応づけない。**この判定は基本フローの stall の評価順であり、
// 代替フローではない。
func TestQuota_画面が動いていれば枠待ちと判定しない(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, _ := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_scoped", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 300, "CONTINUO_TEST_OAUTH_TOKEN_W6")

	// **画面が動いている**（エージェントは長い1つのツール呼び出しの最中である）。
	// **枠待ちと判定される前に動かす。**判定してからでは、標識が立った run は
	// 次の巡回で画面を見に行かない（枠が明けたときに標識が外れる）。
	fx.Herdr.BumpRevision()

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "枠待ちと判定しない理由が出る", func() bool {
		return strings.Contains(fx.Logs.String(), "画面が変わっているので待ち続けます")
	})

	if _, ok := viewOf(fx, issue.Identifier); !ok {
		t.Fatalf("画面が動いているのに印から外している")
	}
	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 1 || got[0] != testGHLogin {
		t.Fatalf("担当者が変わっている: %v", got)
	}
	if got := fx.Logs.String(); strings.Contains(got, "枠待ちと判定したので") {
		t.Fatalf("画面が動いているのに枠待ちと判定している:\n%s", got)
	}
}

// {"RUCM-PATH": "P006"}
//
// TestQuota_担当が移っていたらafter_runを走らせずに止める は、代替フロー「待つ上限を超えた」の
// 担当の確かめで引き返す枝を検査する（設計 3-27 / 3-77c。issue #197）。
//
// 目的: **枠待ちのあいだ、担当は自分の意思と無関係に外れる。**
// `idle_timeout_ms` は「担当者の最後の進捗報告から」で数え、**枠待ち中は hook が来ないので
// 進捗のコメントも増えない。**
// **3-77c は「担当を外された機械は、その branch へ push してはならない」と決めている。**
// **確かめずに `after_run` を走らせると、利用者が書いた `git push` が別の機械の branch へ飛ぶ。**
//
// 与える情報: 1週間の枠が 100% で、リセットは48時間後。上限は300分。**担当者は別の人である。**
// 成功条件: 印から外れること。**別の人の担当者が残っていること**（こちらは触らない）。
func TestQuota_担当が移っていたらafter_runを走らせずに止める(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 300, "CONTINUO_TEST_OAUTH_TOKEN_W7")

	// **待っているあいだに、別の機械が担当を取っていった。**
	// **`testGHLogin` とは違うアカウントにする。**同じにすると「担当は自分のまま」になる。
	fx.Tracker.SetAssignees(issue.ID, "another-machine")

	waitForRelease(t, fx, clock, issue.Identifier)

	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 1 || got[0] != "another-machine" {
		t.Fatalf("担当が移っているのに担当者へ触っている: %v", got)
	}
	if got := fx.Logs.String(); !strings.Contains(got, "担当が移ったので") {
		t.Fatalf("担当が移ったことを出していない:\n%s", got)
	}
	if got := fx.Logs.String(); strings.Contains(got, "担当を手放しました") {
		t.Fatalf("担当が移っているのに手放しの経路を通っている:\n%s", got)
	}
}

// {"RUCM-PATH": "P007"}
//
// TestQuota_1週間の枠のリセットが上限より先なら担当を手放す は、#197 の本体を確かめる
// （時刻で測る側）。
//
// 目的: **1週間の枠は最長で7日先までリセットされない。**待つ上限を設けないと、
// その issue を抱えたまま何日も止まる。
//
// 与える情報: 1週間の枠が 100% で、リセットは48時間後。上限は300分（5時間）。
// 成功条件: 印から外れ（スロットが空き）、担当者が空になること。
func TestQuota_1週間の枠のリセットが上限より先なら担当を手放す(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 300, "CONTINUO_TEST_OAUTH_TOKEN_W1")

	tickOnce(fx)
	waitForRelease(t, fx, clock, issue.Identifier)

	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 0 {
		t.Fatalf("担当者が残っている: %v", got)
	}
	// **結果の1行が既定の水準で出ること。**
	// 「上限を超えたので手放します」は `Debug` である（手放せずに戻る経路が毎巡回で通るため）。
	if got := fx.Logs.String(); !strings.Contains(got, "担当を手放しました") {
		t.Fatalf("手放したことを出していない:\n%s", got)
	}
}

// TestQuota_リセット時刻が読めなくても経過が上限を超えたら手放す は、#197 の本体を確かめる
// （経過で測る側）。
//
// 目的: **`resets_at` を持たない1週間の枠がある。**時刻で測れない以上、
// **上限を掛けないと、印を外す条件も無いまま待ち続ける。**
//
// 与える情報: 1週間の枠が 100% で `resets_at` が null。上限は10分。
// 成功条件: 満杯を見た直後（経過0）では手放さず、10分を過ぎたら手放すこと。
func TestQuota_リセット時刻が読めなくても経過が上限を超えたら手放す(t *testing.T) {
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_scoped", "percent": 100, "resets_at": nil, "severity": "normal"},
	}, 10, "CONTINUO_TEST_OAUTH_TOKEN_W2")

	// 1回目の巡回で「満杯を見た時刻」が入る。**まだ経過は0なので手放さない。**
	fx.Orc.Tick(context.Background())
	if _, ok := viewOf(fx, issue.Identifier); !ok {
		t.Fatalf("満杯を見た直後（経過0）で手放している:\n%s", fx.Logs.String())
	}

	// **20分進める。**上限（10分）を超える。
	clock.Advance(20 * time.Minute)
	fx.Orc.Tick(context.Background())
	// **この巡回で画面の版を初めて見る。**そこから窓（60秒）ぶん止まっていることを、
	// **次の巡回で確かめてから手放す**（設計 3-27 の段0b）。
	waitForRelease(t, fx, clock, issue.Identifier)
}

// TestQuota_5時間の枠だけなら上限を超えても待ち続ける は、人間が決めた表の1行目を確かめる。
//
// 目的: **2026-08-26 の決定「5時間枠 → 待つ。担当は変えない」。**
// 5時間の枠は待てば必ず明けるので、担当を動かす必要が無い。
//
// 与える情報: 5時間の枠だけが 100% で、リセットは48時間後（上限をはるかに超える）。
// 成功条件: 印から外れないこと。
func TestQuota_5時間の枠だけなら上限を超えても待ち続ける(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "session", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 300, "CONTINUO_TEST_OAUTH_TOKEN_W3")

	fx.Orc.Tick(context.Background())
	clock.Advance(10 * time.Hour)
	fx.Orc.Tick(context.Background())

	if _, ok := viewOf(fx, issue.Identifier); !ok {
		t.Fatalf("5時間の枠だけなのに担当を手放している:\n%s", fx.Logs.String())
	}
}

// TestQuota_画面の状態を判定できないうちは手放さない は、確かめられないときの倒し方を確かめる
// （設計 3-27。issue #197）。
//
// 目的: **herdr の `unknown` は「agent は居るが状態を判定できない」である。**
// **確かめられていないのに手放してはならない。**この判定の先には GitHub への2回の書き込みと
// pane を閉じる操作があり、**書きかけの編集を持ったまま閉じると、その編集は戻らない。**
//
// 与える情報: 1週間の枠が 100% でリセットは48時間後。上限は10分。
// **`agent_status` は `unknown`。**
// 成功条件: 上限を超えていても手放さないこと。
func TestQuota_画面の状態を判定できないうちは手放さない(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 10, "CONTINUO_TEST_OAUTH_TOKEN_W_UNKNOWN")
	fx.Herdr.SetStatus(herdr.AgentStatusUnknown)

	for i := 0; i < 5; i++ {
		clock.Advance(2 * time.Minute)
		fx.Orc.Tick(context.Background())
	}

	if _, ok := viewOf(fx, issue.Identifier); !ok {
		t.Fatalf("画面の状態を判定できないのに担当を手放した:\n%s", fx.Logs.String())
	}
}

// TestQuota_サブエージェントが走っているうちは手放さない は、
// 「完全停止するまで待つ」を確かめる（設計 3-27。issue #197）。
//
// 目的: **人間の指示は「そのセッションのサブエージェントを含め完全停止するまで待って」である**
// （2026-09-06）。**herdr の `agent_status` はサブエージェントを知らない。**
// continuo は `SubagentStart` / `SubagentStop` を自分で数えている。
// **待たずに閉じると、そのとき書きかけだった編集がまるごと消える。**
//
// 与える情報: 1週間の枠が 100% でリセットは48時間後。上限は10分。
// **`agent_status` は `idle` だが、サブエージェントが1つ走っている。**
// 成功条件: 上限を超えていても手放さないこと。
func TestQuota_サブエージェントが走っているうちは手放さない(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 10, "CONTINUO_TEST_OAUTH_TOKEN_W_SUBAGENT")
	fx.Orc.OnHook(subagentStartEvent("session-188", "", "a1f9f743842d397e1", "Explore"))

	for i := 0; i < 5; i++ {
		clock.Advance(2 * time.Minute)
		fx.Orc.Tick(context.Background())
	}

	if _, ok := viewOf(fx, issue.Identifier); !ok {
		t.Fatalf("サブエージェントが走っているのに担当を手放した:\n%s", fx.Logs.String())
	}
}

// TestQuota_5時間の枠の時刻で1週間の枠を判定しない は、待つ先の取り方を確かめる。
//
// 目的: **`LatestResetForClearing` は種別を選ばない。**1週間の枠が `resets_at` を持たず、
// 5時間の枠が2時間後に明けるとき、**あれを使うと「2時間後」で判定してしまい、
// 上限（10分）を超えないので手放さない。**
//
// 与える情報: 1週間の枠が 100% で `resets_at` が null。5時間の枠も 100% で2時間後。上限は10分。
// 成功条件: 経過で測って手放すこと（5時間の枠の時刻に引きずられない）。
func TestQuota_5時間の枠の時刻で1週間の枠を判定しない(t *testing.T) {
	soon := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "session", "percent": 100, "resets_at": soon, "severity": "normal"},
		{"kind": "weekly_scoped", "percent": 100, "resets_at": nil, "severity": "normal"},
	}, 10, "CONTINUO_TEST_OAUTH_TOKEN_W4")

	fx.Orc.Tick(context.Background())
	clock.Advance(20 * time.Minute)
	fx.Orc.Tick(context.Background())
	// **画面の版を初めて見た巡回では手放さない**（設計 3-27 の段0b）。
	waitForRelease(t, fx, clock, issue.Identifier)
}

// TestQuota_上限が0なら1週間の枠でも待ち続ける は、逃げ道を確かめる。
//
// 目的: **`weekly_wait_limit_minutes: 0` は「上限を設けない」である。**
// `claude.turn_timeout_ms` と `tracker.provider.handoff.recheck_interval_ms` と同じ向きである。
//
// 与える情報: 1週間の枠が 100% で、リセットは48時間後。**上限は0。**
// 成功条件: 印から外れないこと。
func TestQuota_上限が0なら1週間の枠でも待ち続ける(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 0, "CONTINUO_TEST_OAUTH_TOKEN_W5")

	fx.Orc.Tick(context.Background())
	clock.Advance(10 * time.Hour)
	fx.Orc.Tick(context.Background())

	if _, ok := viewOf(fx, issue.Identifier); !ok {
		t.Fatalf("上限が0なのに担当を手放している:\n%s", fx.Logs.String())
	}
}

// TestQuota_打ち切りを切っていても巡回は落ちない は、判定の置き場所を確かめる
// （設計 3-27。issue #197）。
//
// 目的: **`claude.turn_timeout_ms` が0以下だと、巡回の打ち切りの判定は行わない**
// （`SPEC.md` 8.4 が「0 以下なら stall 検知を行わない」と決めている）。
// **それでも枠待ちの印は立つ**（hook を1件も受けていない run は、無音の長さを見ずに
// 枠待ちと判定される）。**だから上限の判定を打ち切りの門より前へ出した。**
//
// 与える情報: `claude.turn_timeout_ms: 0`。1週間の枠が 100% で、リセットは48時間後。上限は300分。
// 成功条件: 巡回が落ちず、**枠待ちでない run を誤って手放さない**こと。
func TestQuota_打ち切りを切っていても巡回は落ちない(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	endpoint, _ := newUsageServer(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	})
	reader := newUsageReader(t, endpoint, "CONTINUO_TEST_OAUTH_TOKEN_W6")
	clock := newTestClock()

	fx := newStubFixture(t, stubFixtureOptions{
		AgentStatus: herdr.AgentStatusUnknown,
		RateLimit:   reader,
		Now:         clock.Now,
		Mutate: func(cfg *config.Config) {
			// **打ち切りの判定を切る。**この設定は validate が明示的に許している。
			cfg.Claude.TurnTimeoutMs = 0
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.RateLimit.WeeklyWaitLimitMinutes = 300
		},
	})

	issue := assignedIssue(188, "In Progress", testGHLogin)
	fx.Tracker.AddIssue(issue)
	fx.Orc.Adopt(issue, orchestrator.AdoptedRun{
		AgentName:        normalize.SafeName("continuo-hello-world-188"),
		PaneID:           "w1:p1",
		SessionUUID:      "session-188",
		HerdrWorkspaceID: "w1",
	}, false)

	// **この検査は「落ちないこと」までしか確かめられない。**
	// **枠待ちの印を、巡回に入る前に立てる手立てが無いためである**
	// （`orchestrator.AdoptedRun` に枠待ちの欄が無く、その型は
	// このリポジトリの決まりで触れないファイルにある）。
	// **印を立てられるのは turn の待ちループだけで、そこを通すには turn を1回走らせる必要がある。**
	//
	// **確かめられているのは、`claude.turn_timeout_ms` が0以下でも巡回が落ちないことと、
	// 枠待ちでない run を誤って手放さないことの2つである。**
	// **上限そのものは、上の5本が確かめている。**
	fx.Orc.Tick(context.Background())

	if _, ok := viewOf(fx, issue.Identifier); !ok {
		t.Fatalf("枠待ちでない run を手放している:\n%s", fx.Logs.String())
	}
}
