package orchestrator_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/hookserver"
)

// TestTurn_background_tasksが空のStopだけでturnの終わりと判定しない は、
// turn の終わりの判定の要を確かめる。
//
// 目的: 設計 1-3 の実測「空の `Stop` 20件のうち4件が turn の途中だった」を踏まえ、
// 設計 3-2 の「`background_tasks` が空配列 → `settle_ms` のあいだ待ち、
// `<task-notification>` が来なければ turn の終わりとする。**来たら turn は続いている**」を
// 守っていることを示す。
//
// 与える情報: 1回目の `agent.prompt` のあとに、空の `Stop` → `<task-notification>` の順で
// hook が届く。**しばらく2つ目の `Stop` は来ない。**
// 成功条件: 1つ目の空の `Stop` では run が終わらず、2つ目の空の `Stop`（`<task-notification>`
// が続かない）を受けて初めて終わる。
func TestTurn_background_tasksが空のStopだけでturnの終わりと判定しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// 途中の `Stop`（空配列）と、その直後の `<task-notification>`（実測で 0.033〜0.037 秒後）。
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		fx.Orc.OnHook(taskNotificationEvent("session-1", "t1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// settle_ms（50ms）の何倍も待っても、まだ終わっていないこと。
	time.Sleep(500 * time.Millisecond)
	if len(fx.Orc.RunningIdentifiers()) == 0 {
		t.Fatalf("空の Stop だけで turn の終わりと判定してしまった（設計 1-3 の実測 4/20 に反する）")
	}

	// 2つ目の `Stop`（今度は `<task-notification>` が続かない）。
	fx.Tracker.SetState("PVTI_item188", "Done")
	fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
	fx.Orc.OnHook(stopEvent("session-1", path, "p1"))

	waitFor(t, 10*time.Second, "2つ目の Stop で turn が終わる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
}

// TestTurn_background_tasksの項目が欠けていたらturnの終わりとみなさない は、
// 「判定不能」の扱いを確かめる。
//
// 目的: 設計 3-2 の「`background_tasks` の項目が欠けている → 判定不能。turn の終わりと
// みなさない（連続したら stall 検知へ）」を守っていることを示す。
// 与える情報: `background_tasks` の項目そのものが無い `Stop`。
// 成功条件: turn の終わりにならず、stall として扱われてリトライが積まれる
// （**印には残る。**バックオフ中に外すと30秒後の巡回で即座に拾い直される）。
func TestTurn_background_tasksの項目が欠けていたらturnの終わりとみなさない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		fx.Orc.OnHook(hookserver.HookEvent{
			HookEventName: "Stop",
			SessionID:     "session-1",
			PromptID:      "p1",
			// BackgroundTasks は nil のまま（項目が欠けている状態）。
		})
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle"},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 10*time.Second, "stall としてリトライが積まれる", func() bool {
		for _, v := range fx.Orc.RunViews() {
			if v.Identifier == "maimuzo/koetsumugi#188" && v.RetryCount == 1 {
				return true
			}
		}
		return false
	})
	if len(fx.Orc.RunningIdentifiers()) != 1 {
		t.Fatalf("バックオフ中の run を印から外している（30秒後の巡回で即座に拾い直される）")
	}
}

// TestTurn_表明が無かった次のturnで促す は、設計 3-25 の第3層を確かめる。
//
// 目的: 「表明せずに終わったら、次の turn の継続の指示で促す（hook から差し戻す仕組みは
// 採らない）」を守っていることを示す。
// 与える情報: 1回目の turn では表明を書かず、2回目で `review` を書く transcript。
// 成功条件: 2回目のプロンプトに促しの1文が入り、1回目の本文（テンプレート）は送り直さない。
func TestTurn_表明が無かった次のturnで促す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	noSignal := writeTranscript(t, transcriptDir, "no-signal.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "作業を進めています。", false),
	})
	withSignal := writeTranscript(t, transcriptDir, "with-signal.jsonl", []any{
		typedUserLine("p2", "続けてください"),
		assistantLine("req2", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	var mu sync.Mutex
	var texts []string
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		text, _ := params["text"].(string)
		texts = append(texts, text)
		n := len(texts)
		mu.Unlock()

		path := noSignal
		if n >= 2 {
			path = withSignal
			fx.Tracker.SetState("PVTI_item188", "Done")
			fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
		}
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "run が終わる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})

	mu.Lock()
	defer mu.Unlock()
	if len(texts) < 2 {
		t.Fatalf("2回目の turn が送られていない: %d 回", len(texts))
	}
	if !strings.Contains(texts[1], "のままです") {
		t.Fatalf("表明を促す1文が2回目のプロンプトに入っていない: %q", texts[1])
	}
	if strings.Contains(texts[1], "gh issue view") {
		t.Fatalf("2回目に1回目の本文を送り直している（設計 5-4 / SPEC.md 7.1 に反する）: %q", texts[1])
	}
}

// TestTurn_max_turnsに達したらfailure_stateへ落とす は、打ち切りを確かめる。
//
// 目的: 設計 3-8 の「打ち切り: max_turns に達したら failure_state へ落とす」と、
// 設計 3-14 の「turn は continuo が送った回数だけで数える」を守っていることを示す。
// 与える情報: `max_turns` が1。1回目の turn で表明を書かず、Status は `In Progress` のまま。
// 成功条件: 2回目の turn を送らずに Status が `Blocked` へ落ちる。
func TestTurn_max_turnsに達したらfailure_stateへ落とす(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Agent.MaxTurns = 1
	}})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "作業を進めています。", false),
	})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle"},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "Status が failure_state へ落ちる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})
	if got := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); got > 2 {
		// 段7（コメントを書かせ直す）で1回だけ余分に送ることがあるが、
		// **turn として2回目を送ってはならない。**
		t.Fatalf("max_turns を超えて turn を送っている: agent.prompt が %d 回", got)
	}
	if fx.Herdr.CountMethod(herdr.MethodPaneClose) == 0 {
		t.Fatalf("打ち切ったのに worker を止めていない（pane.close が唯一の手段である）")
	}
}

// TestTurn_blockedが返ったらescを送ってから人間へ渡す は、安全に関わる分岐を確かめる。
//
// 目的: 設計 3-11 の「`blocked` が返ったとき、そのまま次のプロンプトを投げると、保留中の
// 権限要求が承認されて実行される（3/3 で再現）」を防ぐことを示す。
// 与える情報: 1回目の `agent.prompt` が `blocked` を返す台本。
// 成功条件: **次のプロンプトを投げる前に `agent.send_keys` で `["esc"]` が送られ、
// さらに `pane.close` で worker が止まっている**（そのあとコメントを書かせ直すために
// セッションを復元して送るのは、保留中の要求が消えたあとなので安全である。設計 3-25 の9段）。
// Status は `failure_state` へ落ちる。
func TestTurn_blockedが返ったらescを送ってから人間へ渡す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "Status が failure_state へ落ちる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})

	methods := fx.Herdr.Methods()
	escIdx, closeIdx, firstPrompt, secondPrompt := -1, -1, -1, -1
	for i, m := range methods {
		switch m {
		case herdr.MethodAgentSendKeys:
			if escIdx < 0 {
				escIdx = i
			}
		case herdr.MethodPaneClose:
			if closeIdx < 0 {
				closeIdx = i
			}
		case herdr.MethodAgentPrompt:
			if firstPrompt < 0 {
				firstPrompt = i
			} else if secondPrompt < 0 {
				secondPrompt = i
			}
		}
	}
	if escIdx < 0 {
		t.Fatalf("blocked なのに esc を送っていない: %v", methods)
	}
	if closeIdx < 0 {
		t.Fatalf("人間へ渡すときに worker を止めていない: %v", methods)
	}
	if escIdx < firstPrompt {
		t.Fatalf("esc を送った順番が想定と違う: %v", methods)
	}
	if secondPrompt >= 0 && !(escIdx < secondPrompt && closeIdx < secondPrompt) {
		t.Fatalf("esc と pane.close より前に次のプロンプトを投げている"+
			"（保留中の権限要求が承認されて実行される）: %v", methods)
	}
	keysParams := fx.Herdr.ParamsOf(t, herdr.MethodAgentSendKeys)
	keys, _ := keysParams["keys"].([]any)
	if len(keys) != 1 || keys[0] != "esc" {
		t.Fatalf("送ったキーが想定と違う: got %v, want [esc]", keys)
	}
}

// TestTurn_waitはuntilにblockedを含めてagent_promptに載せる は、待ち受けの掛け方を確かめる。
//
// 目的: 設計 3-2 の「`agent.prompt` を wait つきで送る（`until = [idle, done, blocked]` /
// `timeout_ms = turn_timeout_ms`）。**`agent.wait` を単独で使わない**」を守っていることを示す。
// **`blocked` を外すと、権限の確認で止まった turn を拾えず時間切れまで待つ**（3/3 で再現）。
// 与える情報: `Ready` の issue が1件。
// 成功条件: `agent.prompt` の params の `wait.until` に3つの状態が載り、
// `timeout_ms` が `turn_timeout_ms` と一致する。
func TestTurn_waitはuntilにblockedを含めてagent_promptに載せる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	params := fx.Herdr.ParamsOf(t, herdr.MethodAgentPrompt)
	wait, ok := params["wait"].(map[string]any)
	if !ok {
		t.Fatalf("agent.prompt に wait を載せていない: %v", params)
	}
	until, _ := wait["until"].([]any)
	if joinAny(until) != "idle done blocked" {
		t.Fatalf("wait.until が想定と違う: got %v, want [idle done blocked]", until)
	}
	if got, want := wait["timeout_ms"], float64(fx.Config.Claude.TurnTimeoutMs); got != want {
		t.Fatalf("wait.timeout_ms が turn_timeout_ms と違う: got %v, want %v", got, want)
	}
	if fx.Herdr.CountMethod(herdr.MethodAgentWait) != 0 {
		t.Fatalf("agent.wait を単独で使っている（投入直後の idle を turn の終わりと取り違える）")
	}
}
