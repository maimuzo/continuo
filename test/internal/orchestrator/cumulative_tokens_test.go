package orchestrator_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/orchestrator"
)

// oneResponse は assistantLine 1件ぶんのトークンである。
//
// **`assistantLine` が入れる `usage` と揃えてある**（input 10 / cache_creation 20 /
// cache_read 30 / output 40）。ずれたらこの定数を直すこと。
var oneResponse = orchestrator.TokenUsage{
	APICalls: 1, Input: 10, CacheCreation: 20, CacheRead: 30, Output: 40,
}

// timesResponse は assistantLine n 件ぶんのトークンを返す。
//
// n: 件数。
// 戻り値: n 件ぶんの合計。
func timesResponse(n int) orchestrator.TokenUsage {
	var out orchestrator.TokenUsage
	for i := 0; i < n; i++ {
		out = out.Add(oneResponse)
	}
	return out
}

// TestTokenTotals_turnを重ねても同じtranscriptを二重に数えない は、
// run をまたぐ累計が差分で足されていることを確かめる（issue #238）。
//
// 目的: **`ReadTranscript` が返すのは「その transcript 1ファイルの絶対値」であり、
// turn を重ねるたびに単調に増える。**そのまま毎回足すと、10 turn 回った run は
// 10回ぶん足される。`SPEC.md` 13.5 が「絶対値の合計を扱うときは、二重計上を避けるため、
// 最後に報告した合計との差分を追うこと」と求めている。
//
// 与える情報: 1回目の turn は API 応答1件の transcript で終わり、2回目の turn は
// **同じファイルが2件目まで伸びた状態**で終わる。3回目以降は `Stop` を返さない。
//
// 成功条件: 累計が API 応答2件ぶんであること。**1件目を二重に数えて3件ぶんに
// なっていないこと。**
func TestTokenTotals_turnを重ねても同じtranscriptを二重に数えない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
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
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		case 2:
			// **同じファイルが伸びる。**1件目も入ったままである。
			writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
				typedUserLine("p1", "実装してください"),
				assistantLine("req1", "作業を続けています。\nCONTINUO-STATUS: working", false),
				typedUserLine("p2", "続けてください"),
				assistantLine("req2", "まだ作業しています。\nCONTINUO-STATUS: working", false),
			})
			fx.Orc.OnHook(stopEvent("session-1", path, "p2"))
		default:
			// **`Stop` を返さない。**turn を延々と回さないようにする。
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 30*time.Second, "2回目の turn の集計が載る", func() bool {
		v, ok := viewOfFixture(fx, "octocat/hello-world#188")
		return ok && v.Tokens.APICalls >= 2
	})

	if got, want := fx.Orc.TokenTotals(), timesResponse(2); got != want {
		t.Fatalf("同じ transcript を二重に数えている: got %+v, want %+v", got, want)
	}
}

// TestTokenTotals_runが終わっても累計に残る は、この issue の症状そのものを確かめる
// （issue #238）。
//
// 目的: **run が終わると印の集合から消え、そのトークンも一緒に消えていた。**
// 画面の合計は「いま走っている run」だけを足すので、**長い turn が並んでいる間、
// 合計はほぼ常に0だった。**run をまたぐ累計は、run が消えても残らなければ意味が無い。
//
// 与える情報: 1回の turn で `review` を表明して終わる run を1件。
//
// 成功条件: 走行中の run が0件になったあとも、累計に API 応答1件ぶんが残っていること。
func TestTokenTotals_runが終わっても累計に残る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
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
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})
	fx.AllowLog("コメントを書かせる", "セッションを復元できません", "Status を落とせません")

	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 30*time.Second)

	if n := len(fx.Orc.RunViews()); n != 0 {
		t.Fatalf("run が印から外れていない: %d 件", n)
	}
	if got, want := fx.Orc.TokenTotals(), oneResponse; got != want {
		t.Fatalf("run が終わったら累計まで消えた: got %+v, want %+v", got, want)
	}
}

// TestTokenTotals_引き継いだrunはtranscript全体を累計へ入れる は、
// 画面に出す文言が約束していることを確かめる（issue #238）。
//
// 目的: continuo が再起動しても pane の Claude Code は生きたままなので、
// **引き継いだ run の transcript には、起動より前に書かれた分が残っている。**
// 台帳は空なので、最初の turn の終わりに**そのファイルの全部**が累計へ入る。
// **画面の注記（`dashboard.note_cumulative`）が「引き継いだ run では、起動より前に
// 書かれた分も含む」と人間へ約束しているので、そのとおりになることを確かめる。**
//
// 与える情報: API 応答を2件持つ transcript を先に置き、その run を `Adopt` で引き継ぐ。
// 引き継いだあとの turn が、その transcript で終わる。
//
// 成功条件: 累計が API 応答2件ぶんであること（**引き継ぐ前に書かれた分も入っている**）。
func TestTokenTotals_引き継いだrunはtranscript全体を累計へ入れる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)

	// **引き継ぐ前に、既に2件の API 応答が書かれている。**
	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-188.jsonl", []any{
		typedUserLine("p0", "実装してください"),
		assistantLine("req1", "起動より前に書かれた分です。", false),
		assistantLine("req2", "これも起動より前です。", false),
	})

	var mu sync.Mutex
	calls := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			fx.Orc.OnHook(stopEvent("session-188", path, "p0"))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	if !fx.Orc.Adopt(issue, orchestrator.AdoptedRun{
		AgentName:        normalize.SafeName("continuo-hello-world-" + strconv.Itoa(188)),
		PaneID:           "w1:p1",
		SessionUUID:      "session-188",
		HerdrWorkspaceID: "w1",
	}, true) {
		t.Fatal("引き継げなかった")
	}

	fx.Orc.Tick(context.Background())
	waitFor(t, 30*time.Second, "引き継いだ run の集計が載る", func() bool {
		v, ok := viewOfFixture(fx, "octocat/hello-world#188")
		return ok && !v.TokensAt.IsZero()
	})

	if got, want := fx.Orc.TokenTotals(), timesResponse(2); got != want {
		t.Fatalf("引き継ぐ前に書かれた分が累計に入っていない: got %+v, want %+v", got, want)
	}
}

// TestTokenUsage_Sub は差分の取り方を確かめる（issue #238）。
//
// 目的: **同じ transcript から読んだ絶対値どうしを引く限り、負にはならないはずである。**
// だが transcript が書き換えられないことは測っていないので、**負になったら0へ丸め、
// 丸めたことを呼び出し元へ知らせる。**知らせないと、累計が実際より小さくなったことを
// あとから確かめる手立てが無い。
//
// 与える情報: 増えている場合と、1項目だけ減っている場合。
//
// 成功条件: 増えている場合は差がそのまま出て丸めが起きないこと。減っている場合は
// その項目が0になり、丸めたことが知らされること。
func TestTokenUsage_Sub(t *testing.T) {
	grown := orchestrator.TokenUsage{APICalls: 3, Input: 30, CacheCreation: 60, CacheRead: 90, Output: 120}
	prev := orchestrator.TokenUsage{APICalls: 1, Input: 10, CacheCreation: 20, CacheRead: 30, Output: 40}

	got, clamped := grown.Sub(prev)
	if want := timesResponse(2); got != want {
		t.Errorf("増えている場合の差分が違う: got %+v, want %+v", got, want)
	}
	if clamped {
		t.Error("増えているのに丸めたと知らせた")
	}

	// **1項目だけ減っている。**その項目だけ0になり、丸めたことが知らされる。
	shrunk := orchestrator.TokenUsage{APICalls: 3, Input: 30, CacheCreation: 60, CacheRead: 20, Output: 120}
	got, clamped = shrunk.Sub(prev)
	want := orchestrator.TokenUsage{APICalls: 2, Input: 20, CacheCreation: 40, CacheRead: 0, Output: 80}
	if got != want {
		t.Errorf("減っている項目を0へ丸めていない: got %+v, want %+v", got, want)
	}
	if !clamped {
		t.Error("丸めたのに知らせていない")
	}
}
