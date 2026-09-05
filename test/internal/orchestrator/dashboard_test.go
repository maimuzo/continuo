package orchestrator_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/orchestrator"
)

// TestRunViews_turnの終わりの集計がダッシュボードへ届く は、
// ダッシュボード（第9段階）が読む写しが、本物の供給経路を通って埋まることを確かめる。
//
// 目的: 「transcript を読む → `requestId` で重複排除して集計する → `runState` に控える →
// `RunViews` の写しに載る」の配線が全部つながっていることを示す（設計 3-15 / 5-2）。
// **偽の供給元に値を直接入れるテストでは、この経路が1本でも切れても気づけない。**
//
// 与える情報: 同じ `requestId` を2回持ち、別の `requestId` を1回持つ transcript を、
// turn の終わり（`background_tasks` が空の `Stop`）とともに渡す。表明は `working` なので
// Status は動かず、run は印に残ったままになる。
//
// 成功条件: `RunViews` の写しに、重複排除後のトークン（API 応答2件）と、
// issue のタイトル・URL・最初の turn を送った時刻・最後に hook を受けた時刻・
// 集計した時刻が入っていること。
func TestRunViews_turnの終わりの集計がダッシュボードへ届く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "作業を続けています。\nCONTINUO-STATUS: working", false),
		// **同じ `requestId` の行。**重複排除が効いていれば数に入らない。
		assistantLine("req1", "同じ API 応答が2行に分かれて残っている場合", false),
		assistantLine("req2", "まだ続けます。", false),
	})

	var mu sync.Mutex
	calls := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		}
		// **2回目以降は `Stop` を返さない。**表明が `working` なので run は印に残るが、
		// turn ループは stall で止まる（テストが turn を延々と回さないようにする）。
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 30*time.Second, "turn の終わりの集計が写しに載る", func() bool {
		v, ok := viewOfFixture(fx, "octocat/hello-world#188")
		return ok && !v.TokensAt.IsZero()
	})

	v, ok := viewOfFixture(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatalf("run が印から外れている")
	}

	// **`requestId` で重複排除した値である**（assistant の行は3つだが API 応答は2件）。
	want := orchestrator.TokenUsage{APICalls: 2, Input: 20, CacheCreation: 40, CacheRead: 60, Output: 80}
	if v.Tokens != want {
		t.Fatalf("トークンの集計が違う: got %+v, want %+v", v.Tokens, want)
	}
	if v.Title != "テスト用の issue 188" {
		t.Errorf("issue のタイトルが写しに入っていない: got %q", v.Title)
	}
	if v.URL != "https://github.com/octocat/hello-world/issues/188" {
		t.Errorf("issue の URL が写しに入っていない: got %q", v.URL)
	}
	if v.StartedAt.IsZero() {
		t.Error("最初の turn を送った時刻が写しに入っていない")
	}
	if v.LastHookAt.IsZero() {
		t.Error("最後に hook を受けた時刻が写しに入っていない")
	}
	if v.TokensAt.Before(v.StartedAt) {
		t.Errorf("集計した時刻が turn を送るより前になっている: started=%v tokens_at=%v",
			v.StartedAt, v.TokensAt)
	}
}

// TestRunViews_セッションに復帰した再着手でトークンを二重に数えない は、
// 復帰したときの累計を確かめる。
//
// 目的: 設計 3-3b の「**復帰した場合、transcript のファイルは同じである。**
// したがって `tokensBase` を作り直してはならない」を示す。作り直すと、同じファイルの
// 中身をもう一度足すことになり、**使ったトークンを実際の2倍に見せる。**
//
// 与える情報: 1回目の turn は `session-1` の transcript（API 応答1件）で終わり、
// 2回目の turn は `Stop` が来ずに stall で打ち切られる。バックオフが明けたあとの再着手は
// **同じ `session-1` へ復帰し、同じファイルが2件目まで伸びた状態**で終わる。
// **時計は手で進める**（実時間では待たない）。
//
// 成功条件: 再着手のあとの累計が、そのファイルの中身そのもの（API 応答2件）になること。
// **1件目を二重に数えて3件ぶんになっていないこと。**
func TestRunViews_セッションに復帰した再着手でトークンを二重に数えない(t *testing.T) {
	clock := newTestClock()
	// **記録の根は、このテスト専用にする**（`sessionTranscriptDir` の説明）。
	fx := newFixture(t, fixtureOptions{
		Now:            clock.Now,
		TranscriptRoot: t.TempDir(),
		Mutate: func(cfg *config.Config) {
			cfg.Agent.MaxRetryBackoffMs = 10000
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	// **記録は記録の根の直下1階層へ置く**（設計 3-3c）。着手の段5b がここを探す。
	//
	// **fixture が採番のときに置く記録とは、別のディレクトリになる。**
	// **同じ名前の記録が2つできるが、この検査が見るのは「在るか」だけなので結果は変わらない。**
	// ここで置くのは、**トークンの集計が読む中身が要るから**である（fixture が置くのは空に近い）。
	transcriptDir := sessionTranscriptDir(t, fx)
	transcript := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "作業を続けています。\nCONTINUO-STATUS: working", false),
	})

	var mu sync.Mutex
	calls := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		switch n {
		case 1:
			fx.Orc.OnHook(stopEvent("session-1", transcript, "p1"))
		case 3:
			// 再着手のあとの turn。**復帰したので transcript は同じファイルであり、
			// その中には1件目の応答も入ったままである。**
			writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
				typedUserLine("p1", "実装してください"),
				assistantLine("req1", "作業を続けています。\nCONTINUO-STATUS: working", false),
				typedUserLine("p2", "実装してください"),
				assistantLine("req2", "まだ作業しています。\nCONTINUO-STATUS: working", false),
			})
			fx.Orc.OnHook(stopEvent("session-1", transcript, "p2"))
		default:
			// **`Stop` を返さない。**stall として打ち切られ、リトライが1つ積まれる。
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 30*time.Second, "1件目の応答の集計が載る", func() bool {
		v, ok := viewOfFixture(fx, "octocat/hello-world#188")
		return ok && v.Tokens.APICalls == 1
	})
	waitFor(t, 30*time.Second, "2回目の turn が stall で打ち切られる", func() bool {
		v, ok := viewOfFixture(fx, "octocat/hello-world#188")
		return ok && v.RetryCount == 1
	})

	// バックオフが明けるまで時計を進めて再着手させる（**セッションへ復帰する**）。
	clock.Advance(30 * time.Second)
	fx.Orc.Tick(context.Background())

	waitFor(t, 30*time.Second, "再着手のあとの集計が2件になる", func() bool {
		v, ok := viewOfFixture(fx, "octocat/hello-world#188")
		return ok && v.Tokens.APICalls >= 2
	})

	v, ok := viewOfFixture(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatalf("run が印から外れている")
	}
	want := orchestrator.TokenUsage{APICalls: 2, Input: 20, CacheCreation: 40, CacheRead: 60, Output: 80}
	if v.Tokens != want {
		t.Fatalf("復帰した先の transcript を二重に数えている: got %+v, want %+v", v.Tokens, want)
	}
}

// TestRunViews_最後にhookを受けた時刻はhookでしか進まない は、
// ダッシュボードが「エージェントが生きているか」を判断できる値を出していることを確かめる。
//
// 目的: **stall の時計（`LastSeenAt`）は hook を1件も受けていなくても進む**
// （turn を送った・枠待ちを外した・猶予を与えた）。それを「最後に hook を受けた時刻」
// として見せると、固まったエージェントでも「0秒前」と表示され、生死を判断できない。
//
// 与える情報: 引き継いだ直後（hook をまだ1件も受けていない）run と、
// そこへ届く `PreToolUse` の hook。
//
// 成功条件: hook を受けるまで `LastHookAt` がゼロ値のままで、stall の時計だけが
// 入っていること。hook を受けたら `LastHookAt` が入ること。
func TestRunViews_最後にhookを受けた時刻はhookでしか進まない(t *testing.T) {
	fx := newStubFixture(t, stubFixtureOptions{})
	adoptRun(fx, 188)

	v, ok := viewOf(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatalf("引き継いだ run が印に入っていない")
	}
	if !v.LastHookAt.IsZero() {
		t.Fatalf("hook を1件も受けていないのに時刻が入っている: %v", v.LastHookAt)
	}
	if v.StallClockAt.IsZero() {
		t.Fatal("stall の時計が入っていない（引き継いだ時点で動き出す）")
	}

	if !fx.Orc.OnHook(toolHook("session-188", "PreToolUse")) {
		t.Fatalf("hook を知らない run のものとして捨てた")
	}

	v, ok = viewOf(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatalf("run が印から外れている")
	}
	if v.LastHookAt.IsZero() {
		t.Fatal("hook を受けたのに、最後に hook を受けた時刻が入っていない")
	}
}
