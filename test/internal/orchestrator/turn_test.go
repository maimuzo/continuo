// {"RUCM-CFG-SHA256": "4a61db11c52f5ba42b23b7180d4dfe2d79b39f257e065f54fe735fd3e48d11e6", "SOURCE": "docs/spec/usecases/particular_case/issue を1件処理する.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**
package orchestrator_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/hookserver"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/orchestrator"
)

// {"RUCM-PATH": "P014"}
//
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
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
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
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 10*time.Second, "stall としてリトライが積まれる", func() bool {
		for _, v := range fx.Orc.RunViews() {
			if v.Identifier == "octocat/hello-world#188" && v.RetryCount == 1 {
				return true
			}
		}
		return false
	})
	if len(fx.Orc.RunningIdentifiers()) != 1 {
		t.Fatalf("バックオフ中の run を印から外している（30秒後の巡回で即座に拾い直される）")
	}
}

// TestTurn_まだ動いていると名乗ったStopを捨てない は、issue #77 の欠陥を塞ぐ。
//
// 目的: 設計 3-2 の「`background_tasks` が空でない → **まだ動いている。turn の終わりとしては
// 扱わない**」と、設計 1-7 の「待っていれば `background_tasks` が空の `Stop` が来る」を
// 守っていることを示す。
//
// **欠陥はこうだった。**空でない `Stop` は「空の `Stop`」ではないので読み捨てられ、
// `settle_ms`（既定2000ミリ秒）が過ぎたところで turn の終わりを検知できなかったとして
// pane を閉じていた。**「まだ動いています」と名乗ってきた2秒後に殺していた。**
//
// 与える情報: 1回目の `agent.prompt` のあとに、バックグラウンドの shell を1件載せた
// `Stop` が届く。**しばらく空の `Stop` は来ない。**
// 成功条件: run が生き続け、リトライも積まれないこと。そのあと空の `Stop` を受けて初めて
// turn が終わること。
func TestTurn_まだ動いていると名乗ったStopを捨てない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// **Claude Code 自身の「まだ動いています」という申告。**
		fx.Orc.OnHook(runningShellStopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// settle_ms（50ms）と poll_wait_ms（200ms）の何倍も待っても、まだ生きていること。
	time.Sleep(1 * time.Second)
	if len(fx.Orc.RunningIdentifiers()) == 0 {
		t.Fatalf("まだ動いていると名乗った Stop を受けたのに run を終わらせた:\n%s", fx.Logs.String())
	}
	if strings.Contains(fx.Logs.String(), "run を諦めてリトライを積みました") {
		t.Fatalf("まだ動いていると名乗った Stop を捨てて run を諦めた:\n%s", fx.Logs.String())
	}

	// バックグラウンド処理が終わり、空の `Stop` が来る。
	fx.Tracker.SetState("PVTI_item188", "Done")
	fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
	fx.Orc.OnHook(stopEvent("session-1", path, "p1"))

	waitFor(t, 10*time.Second, "空の Stop で turn が終わる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
}

// TestTurn_Stopが読めなかったときは届かなかったと書かない は、issue #77 の後半を塞ぐ。
//
// 目的: `background_tasks` の項目が欠けた `Stop`（判定不能）で打ち切るときに、
// **「Stop hook から continuo へ通知が届きませんでした」と書かない**ことを示す。
// **届いている。読めなかっただけである。**逆に書くと、読んだ人は正常に配線されている
// hook の設定ファイルを確かめに行かされる。
//
// 与える情報: `background_tasks` の項目そのものが無い `Stop`。
// 成功条件: 打ち切りの理由が「Stop hook は continuo へ届きましたが」で、
// 「届きませんでした」を含まないこと。
func TestTurn_Stopが読めなかったときは届かなかったと書かない(t *testing.T) {
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
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "判定不能として打ち切られる", func() bool {
		return strings.Contains(fx.Logs.String(), "run を諦めてリトライを積みました")
	})

	logs := fx.Logs.String()
	if !strings.Contains(logs, "Stop hook は continuo へ届きましたが") {
		t.Fatalf("届いていたことを理由に書いていない:\n%s", logs)
	}
	if strings.Contains(logs, "Stop hook から continuo へ通知が届きませんでした") {
		t.Fatalf("届いていたのに「届かなかった」と書いている:\n%s", logs)
	}
}

// TestTurn_Stopが読めなかったときの文面は持っていないものを案内しない は、
// issue #77 で書き直した文面が、また読む人を存在しない場所へ行かせていないかを見る。
//
// 目的: 設計 3-34b の「**持っていないものは案内しない**」を、判定不能で打ち切るときの
// 文面が守っていることを示す。
//
// **continuo は hook の中身をどこにも残していない。**ログにも出さず、逃がし先（設計 3-19）は
// socket へ繋がらなかった hook だけを置く場所なので、配送できた hook は残らない。
// **だから「continuo のログで `Stop` の中身を見てください」と書いてはならない。**
//
// **「JSON が途中で切れた」も原因に挙げてはならない。**切れた JSON は受け口が弾くので、
// この文面が出る経路までは届かない。
//
// **ログではなくコメント本文を見る。**打ち切りの理由がログへ出るのは最初の1行だけで
// （`summaryLine`）、【確かめ方】はログには流れない。ログを見ても検査にならない。
//
// 与える情報: `background_tasks` の項目が欠けた `Stop`。リトライの回数は 0 にして、
// 1回目の打ち切りで引き渡しの通知が issue に付くようにする。
// 成功条件: 通知の本文が【確かめ方】【よくある原因】【対処】を持ち、
// **continuo のログも、切れた JSON も案内していない**こと。
func TestTurn_Stopが読めなかったときの文面は持っていないものを案内しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Agent.MaxRetries = 0
	}})
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
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	var body string
	waitFor(t, 20*time.Second, "引き渡しの通知が issue に付く", func() bool {
		for _, c := range fx.Tracker.CommentsOf("I_node188") {
			if strings.Contains(c.Body, "Stop hook は continuo へ届きましたが") {
				body = c.Body
				return true
			}
		}
		return false
	})

	for _, want := range []string{"【確かめ方】", "【よくある原因】", "【対処】"} {
		if !strings.Contains(body, want) {
			t.Errorf("打ち切りの文面に %q が無い:\n%s", want, body)
		}
	}
	for _, ng := range []string{"continuo のログ", "JSON が途中で切れた"} {
		if strings.Contains(body, ng) {
			t.Errorf("持っていないものを案内している（%q）:\n%s", ng, body)
		}
	}
}

// TestTurn_空のStopのあとに来た走行中のStopも捨てない は、issue #77 の欠陥が
// もう1つの経路に残っていたのを塞ぐ。
//
// 目的: 設計 3-2 の「空でない `Stop` を捨てない」は、**待ち受けの窓がどれであっても
// 成り立たなければならない**ことを示す。
//
// **欠陥はこうだった。**空の `Stop` を1件受けたあとの `settle_ms` の窓は
// `<task-notification>` だけを待っており、その窓に届いた「`background_tasks` が
// 空でない `Stop`」は条件に合わないものとして読み捨てられていた。
// **窓が閉じた時点で turn の終わりとして扱われ、pane が閉じられる。**
// 「まだ動いています」と名乗った相手を殺す点で、issue #77 とまったく同じ形である。
//
// 与える情報: 1回目の `agent.prompt` のあとに、空の `Stop` → バックグラウンドの shell を
// 1件載せた `Stop` の順で届く。**しばらく次の空の `Stop` は来ない。**
// 成功条件: settle_ms が過ぎても run が生き続け、待ち直したことがログに残ること。
// そのあと空の `Stop` を受けて初めて turn が終わること。
func TestTurn_空のStopのあとに来た走行中のStopも捨てない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		// **settle_ms を広げるのは、テストを通すためではない。**既定の 50ms では
		// 「窓が開いている間に届いた」のか「窓が閉じたあとに届いた」のかを
		// 区別できず、狙った経路を通ったことにならない。
		cfg.Claude.SettleMs = 300
	}})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// 空の `Stop` の直後に、「まだ動いています」という申告が届く。
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		fx.Orc.OnHook(runningShellStopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "settle の窓で走行中の Stop を拾って待ち直す", func() bool {
		return strings.Contains(fx.Logs.String(),
			"空の Stop のあとにバックグラウンド処理が残ると名乗る Stop を受けたので、turn の終わりとせずに待ち直します")
	})

	// settle_ms（300ms）と poll_wait_ms（200ms）の何倍も待っても、まだ生きていること。
	time.Sleep(1 * time.Second)
	if len(fx.Orc.RunningIdentifiers()) == 0 {
		t.Fatalf("settle の窓に届いた走行中の Stop を捨てて run を終わらせた:\n%s", fx.Logs.String())
	}

	// バックグラウンド処理が終わり、空の `Stop` が来る。
	fx.Tracker.SetState("PVTI_item188", "Done")
	fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
	fx.Orc.OnHook(stopEvent("session-1", path, "p1"))

	waitFor(t, 10*time.Second, "空の Stop で turn が終わる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
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
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
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

// {"RUCM-PATH": "P023"}
//
// TestTurn_max_dispatch_turnsに達したらfailure_stateへ落とす は、打ち切りを確かめる。
//
// 目的: 設計 3-8 の「打ち切り: max_dispatch_turns に達したら failure_state へ落とす」と、
// 設計 3-14 の「turn は continuo が送った回数だけで数える」を守っていることを示す。
// 与える情報: `max_dispatch_turns` が1。1回目の turn で表明を書かず、Status は `In Progress` のまま。
// 成功条件: 2回目の turn を送らずに Status が `Blocked` へ落ちる。
func TestTurn_max_dispatch_turnsに達したらfailure_stateへ落とす(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Agent.MaxDispatchTurns = 1
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
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "Status が failure_state へ落ちる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})
	// **後始末まで待つ。**Status は worker を止める前に書かれる（helpers_test.go の WaitRunsDrained）。
	fx.WaitRunsDrained(t, 10*time.Second)
	// **pane.close は Status の書き込みと同時ではない。**finishRunClaimed は
	// Status を書いたあと、引き渡しのコメント・エージェントのコメントの確認・after_run を
	// 通してから stopWorker を呼ぶ（設計 3-25 の「worker を止める前にコメントを確かめる」）。
	// Status だけを待って直後に検査すると、負荷が高いときに間に合わずに落ちる。
	waitFor(t, 20*time.Second, "worker が止まる（pane.close）", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodPaneClose) > 0
	})
	if got := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); got > 2 {
		// 段7（コメントを書かせ直す）で1回だけ余分に送ることがあるが、
		// **turn として2回目を送ってはならない。**
		t.Fatalf("max_dispatch_turns を超えて turn を送っている: agent.prompt が %d 回", got)
	}
}

// {"RUCM-PATH": "P019"}
//
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
	// **後始末まで待つ。**Status は worker を止める前に書かれる（helpers_test.go の WaitRunsDrained）。
	fx.WaitRunsDrained(t, 10*time.Second)

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

// TestTurn_blockedで引き渡すときサブエージェントの記録も案内する は、
// 「案内された記録の末尾に何も無い」で行き止まりにしないことを確かめる。
//
// 目的: 設計 3-11 の「引き渡しの通知には、親のセッションの記録だけでなく
// subagent の記録も載せる」を守っていることを示す。**親の記録の末尾には何も
// 残っていないことがあり、そこだけを案内すると人間が原因に辿り着けない。**
// 与える情報: `blocked` を返す台本と、hook が渡す親の記録、その隣の
// `<セッション UUID>/subagents/agent-*.jsonl`。
// 成功条件: 引き渡しの通知の【調べるところ】に subagent の記録のパスと置き場所が並び、
// 【確かめ方】が【調べるところ】を指していること。**原因を断定していないこと。**
func TestTurn_blockedで引き渡すときサブエージェントの記録も案内する(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(189, "Ready"))

	transcriptDir := t.TempDir()
	parent := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "調べます", false),
	})
	// **置き場所の規則は実測で確かめられている**（docs/evidence/hooks_probe_20260817.jsonl）。
	subagentDir := filepath.Join(transcriptDir, "session-1", "subagents")
	if err := os.MkdirAll(subagentDir, 0o700); err != nil {
		t.Fatalf("subagents ディレクトリを作れません: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subagentDir, "agent-a1f9f743.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("subagent の記録を書けません: %v", err)
	}

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// **記録のパスは hook から届く。**引き渡しの通知はこの値から subagents を辿る。
		fx.Orc.OnHook(stopEvent("session-1", parent, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "引き渡しの通知が投稿される", func() bool {
		for _, c := range fx.Tracker.CommentsOf("I_node189") {
			if strings.Contains(c.Body, "人間へ引き渡しました") {
				return true
			}
		}
		return false
	})
	fx.WaitRunsDrained(t, 15*time.Second)

	var body string
	for _, c := range fx.Tracker.CommentsOf("I_node189") {
		if strings.Contains(c.Body, "人間へ引き渡しました") {
			body = c.Body
		}
	}
	for _, want := range []string{
		"【調べるところ】",
		"サブエージェントの記録（新しい順）",
		"agent-a1f9f743.jsonl",
		"サブエージェントの記録の置き場所",
		"下記の【調べるところ】に挙げた記録",
		"dontAsk",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("引き渡しの通知に %q が無い:\n%s", want, body)
		}
	}
	// **原因を断定してはならない。**何が確認の画面を出したかは continuo の側に残らない。
	if strings.Contains(body, "許可されていないコマンドを実行しようとした") {
		t.Errorf("確かめていない原因を断定している:\n%s", body)
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

// {"RUCM-PATH": "P021"}
//
// TestTurn_一時的な送信の失敗ではpaneを閉じない は、
// **一時的な失敗と、送信そのものを断られたときとで、後始末が正反対である**ことを確かめる。
//
// 目的: 設計 3-48。`送信の失敗` は pane を閉じてリトライを積むが、`一時的な送信の失敗` は
// **何も閉じず、何も積まない**（RUCM「issue を1件処理する」の事後条件
// 「印は残っている。リトライの回数は増えていない。herdr の pane は閉じていない」）。
// **pane を閉じると、その中で動いている Claude Code が turn の途中で消える。**
// herdr が一瞬落ちただけなのに、エージェントの作業がそこで失われる。
//
// 与える情報: `agent.prompt` を受けたところで応答を書かずに接続を切るテスト用herdr mock
// （herdr の再起動そのものである）。リトライは 0 回。
// 成功条件: `pane.close` が1度も呼ばれず、run が印に残り、Status が `In Progress` のまま
// であること。
func TestTurn_一時的な送信の失敗ではpaneを閉じない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Agent.MaxRetries = 0
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	fx.Herdr.DropConnection(herdr.MethodAgentPrompt)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.AllowLog("herdr へ届かなかったので", "herdr との通信が一時的に失敗した")

	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "turn の送信が herdr へ届く", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	// **run を捨てる実装なら、ここで pane を閉じるところまで走り切る。**走り切らせてから見る。
	time.Sleep(2 * time.Second)

	if got := fx.Herdr.CountMethod(herdr.MethodPaneClose); got != 0 {
		t.Errorf("herdr が一瞬落ちただけで pane を閉じた: pane.close が %d 回", got)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Errorf("印から run が外れた: %d 件（1 件のはず）", got)
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "In Progress" {
		t.Errorf("Status を動かした: got %q, want In Progress", got)
	}
}

// {"RUCM-PATH": "P017"}
//
// TestTurn_待ち受けが返ってもStopHookが来なければ打ち切る は、
// 代替フロー「turnの終わりの取りこぼし」を検査する。
//
// 目的: 設計 3-2 / 3-40 の「**待ち受けが返ったあとに Stop hook が来なかったこと**だけが
// 『Stop hook が届かなかった』と言ってよい場所である」を示す。
// **巡回の停滞の検知（`claude.turn_timeout_ms` の沈黙）とは別の経路である。**
// あちらは画面の版で測るが、こちらは待ち受けが返った直後の `settle_ms` だけを見る。
//
// 与える情報: `agent.prompt` は `idle` で返るのに、Stop hook が1件も届かない。
// 成功条件: 「turn が終わったことを検知できませんでした」を理由にリトライを1つ積み、
// **Status は running_state のままで、印にも残る**（`failure_state` へは落とさない）。
func TestTurn_待ち受けが返ってもStopHookが来なければ打ち切る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "Stop hook が来ないまま打ち切られる", func() bool {
		return strings.Contains(fx.Logs.String(), "run を諦めてリトライを積みました")
	})

	if !strings.Contains(fx.Logs.String(), "turn が終わったことを検知できませんでした") {
		t.Fatalf("Stop hook が届かなかったことを理由にしていない:\n%s", fx.Logs.String())
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "In Progress" {
		t.Errorf("リトライが残っているのに Status を動かした: got %q, want In Progress", got)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Errorf("バックオフ中も印には残すはずが外れている: %d 件（1 件のはず）", got)
	}
}

// {"RUCM-PATH": "P005"}
//
// TestComment_身元ファイルを読めなければ復元をあきらめて片付けへ進む は、
// 代替フロー「復元の断念」を検査する。
//
// 目的: 設計 3-25 の「コメントを書かせるための復元は、材料が足りなければ**そこでやめる**」
// を示す。**run を `failure_state` へ落とし直さない。**落とすのは
// `agent.start --resume` まで進んで書かせられなかったとき（`コメントの取り戻しの失敗`）だけである。
//
// 与える情報: 身元ファイル（`.continuo.json`）の無い worktree を持つ run。
// リトライは 0 なので、1回目の打ち切りでそのまま引き渡しへ進む。
// 成功条件: 「身元ファイルを読めないので復元できません」を記録に残し、
// **そのまま片付けを続けて印から外す**こと。
func TestComment_身元ファイルを読めなければ復元をあきらめて片付けへ進む(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Agent.MaxRetries = 0
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	// **身元ファイルを1バイトも置いていない worktree を持たせる。**
	fx.AllowLog("身元ファイルを読めないので復元できません")
	if !fx.Orc.Adopt(issue, orchestrator.AdoptedRun{
		AgentName:    normalize.SafeName("continuo-hello-world-188"),
		PaneID:       "p-188",
		SessionUUID:  "session-1",
		WorktreePath: t.TempDir(),
	}, true) {
		t.Fatalf("検査用の run を印の集合へ入れられません")
	}

	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "身元ファイルを読めないことが記録に残る", func() bool {
		return strings.Contains(fx.Logs.String(), "身元ファイルを読めないので復元できません")
	})
	waitFor(t, 20*time.Second, "片付けが続いて印から外れる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
}

// subagentStartEvent は `SubagentStart` の hook を作る（設計 3-11）。
//
// **`transcript_path` は親のものである。**実測記録でもそうなっている
// （docs/evidence/hooks_probe_20260817.jsonl）。
//
// sessionID: セッション UUID。
// transcriptPath: 親のセッションの記録のパス。
// agentID: `agent_id`。
// agentType: `agent_type`。
// 戻り値: hook のイベント。
func subagentStartEvent(sessionID, transcriptPath, agentID, agentType string) hookserver.HookEvent {
	return hookserver.HookEvent{
		HookEventName:  "SubagentStart",
		SessionID:      sessionID,
		TranscriptPath: transcriptPath,
		AgentID:        agentID,
		AgentType:      agentType,
	}
}

// subagentStopEvent は `SubagentStop` の hook を作る（設計 3-11）。
//
// **`agent_type` を空にしてはならない。**空文字のものは orchestrator が捨てる（設計 1-3）。
//
// sessionID: セッション UUID。
// agentID: `agent_id`。
// agentType: `agent_type`。
// 戻り値: hook のイベント。
func subagentStopEvent(sessionID, agentID, agentType string) hookserver.HookEvent {
	return hookserver.HookEvent{
		HookEventName: "SubagentStop",
		SessionID:     sessionID,
		AgentID:       agentID,
		AgentType:     agentType,
	}
}

// handoffCommentBody は引き渡しの通知の本文を返す（無ければ空文字）。
//
// fx: 対象の fixture。
// nodeID: issue の node ID。
// 戻り値: 引き渡しの通知の本文。
func handoffCommentBody(fx *fixture, nodeID string) string {
	body := ""
	for _, c := range fx.Tracker.CommentsOf(nodeID) {
		if strings.Contains(c.Body, "人間へ引き渡しました") {
			body = c.Body
		}
	}
	return body
}

// TestTurn_走行中のサブエージェントを止めたら通知にそう書く は、
// 「4件中3件まで終わっていて4件目で止まった」が黙って起きないことを確かめる。
//
// 目的: 設計 3-11 の「走行中の subagent を止めたなら、引き渡しの通知にそう書き、
// **その `agent_id` から記録のパスを組み立てて載せる**」を守っていることを示す。
// **引き渡しは直後に pane を閉じるので、走っていたものは途中で終わる。**
// **書かないと、人間は worktree に書きかけの変更が残っていることに気づけない。**
// 与える情報: `SubagentStart` だけが届き `SubagentStop` が来ないまま `blocked` になる台本と、
// その `agent_id` で組み立てた記録のファイル。
// **`agent_type` には backtick と制御文字を混ぜる**（hook は外部入力である）。
// 成功条件: 通知に件数と名前が載り、backtick と制御文字が落ち、
// **glob ではなく `agent_id` から組み立てたパスが「走っていた」印つきで載ること。**
func TestTurn_走行中のサブエージェントを止めたら通知にそう書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	// **これは想定して起こしている失敗である。**猶予のあいだ待っても終わらない
	// subagent を、走行中のまま止める場面をわざと作っている。
	fx.AllowLog("猶予のあいだにサブエージェントが終わらなかったので")
	fx.Tracker.AddIssue(sampleIssue(190, "Ready"))

	transcriptDir := t.TempDir()
	parent := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
	})
	// **ファイル名の規則は実測記録1件から言えることである**
	// （`agent_id` = a1f9f743842d397e1 に対して `agent-a1f9f743842d397e1.jsonl`）。
	subagentDir := filepath.Join(transcriptDir, "session-1", "subagents")
	if err := os.MkdirAll(subagentDir, 0o700); err != nil {
		t.Fatalf("subagents ディレクトリを作れません: %v", err)
	}
	running := filepath.Join(subagentDir, "agent-a1f9f743842d397e1.jsonl")
	if err := os.WriteFile(running, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("走行中の subagent の記録を書けません: %v", err)
	}
	// **glob なら、こちらのほうが新しいので先に並ぶ。**走行中のものを出すなら選ばれない。
	stale := filepath.Join(subagentDir, "agent-old0000000000000.jsonl")
	if err := os.WriteFile(stale, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("前の turn の subagent の記録を書けません: %v", err)
	}

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// **backtick と制御文字は落とさなければならない。**落とさないと通知の
		// code span を抜け出して、issue へ好きな Markdown を書き込める。
		fx.Orc.OnHook(subagentStartEvent("session-1", parent, "a1f9f743842d397e1", "impl-`x`\x07"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "引き渡しの通知が投稿される", func() bool {
		return handoffCommentBody(fx, "I_node190") != ""
	})
	fx.WaitRunsDrained(t, 15*time.Second)

	body := handoffCommentBody(fx, "I_node190")
	for _, want := range []string{
		"【走行中のサブエージェントを止めました】",
		"1 件が動いていました",
		"impl-x(a1f9f743842d397e1)",
		"worktree には書きかけの変更が残っている可能性があります",
		"**止めた時点で走っていた**サブエージェントの記録",
		running,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("引き渡しの通知に %q が無い:\n%s", want, body)
		}
	}
	// **推測で選んではならない。**走行中のものが分かっているのに glob の結果を並べると、
	// 止まった直前に動いていたものと関係の無い記録を「これを見ろ」と案内することになる。
	if strings.Contains(body, stale) {
		t.Errorf("走行中のものが分かっているのに glob の結果を載せている:\n%s", body)
	}
	// **外部入力をそのまま載せてはならない。**
	if strings.Contains(body, "impl-`x`") {
		t.Errorf("hook が渡した backtick をそのまま載せている:\n%s", body)
	}
	if strings.ContainsRune(body, '\x07') {
		t.Errorf("hook が渡した制御文字をそのまま載せている:\n%q", body)
	}
}

// TestTurn_escを送る前に走行中のサブエージェントが終わるのを待つ は、
// 「走っているなら待つ」を確かめる。
//
// 目的: 設計 3-11 の「esc を送る前に、走っている subagent が終わるのを
// `claude.poll_wait_ms` のあいだ待つ」を守っていることを示す。**待つのは
// 「別の subagent が書き終えるのを待つ」ためであって、`blocked` が解けるからではない。**
// 与える情報: `blocked` を返したあとに `SubagentStop` が遅れて届く台本と、
// それより十分に長い猶予（`poll_wait_ms` = 5秒）。
// 成功条件: 通知に「走行中のサブエージェントを止めました」が**出ない**こと。
// **待たずに esc を送っていれば、この行は必ず出る。**
func TestTurn_escを送る前に走行中のサブエージェントが終わるのを待つ(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		// **猶予を、遅れて届く `SubagentStop` より十分に長くする。**
		cfg.Claude.PollWaitMs = 5000
	}})
	fx.Tracker.AddIssue(sampleIssue(191, "Ready"))

	var timerMu sync.Mutex
	var timer *time.Timer
	t.Cleanup(func() {
		timerMu.Lock()
		defer timerMu.Unlock()
		if timer != nil {
			timer.Stop()
		}
	})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		fx.Orc.OnHook(subagentStartEvent("session-1", "", "a1f9f743842d397e1", "Explore"))
		timerMu.Lock()
		timer = time.AfterFunc(200*time.Millisecond, func() {
			fx.Orc.OnHook(subagentStopEvent("session-1", "a1f9f743842d397e1", "Explore"))
		})
		timerMu.Unlock()
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "引き渡しの通知が投稿される", func() bool {
		return handoffCommentBody(fx, "I_node191") != ""
	})
	fx.WaitRunsDrained(t, 20*time.Second)

	body := handoffCommentBody(fx, "I_node191")
	if strings.Contains(body, "【走行中のサブエージェントを止めました】") {
		t.Errorf("サブエージェントが終わるのを待たずに esc を送っている:\n%s", body)
	}
	// **待ったからといって、引き渡しをやめるわけではない。**確認の画面は自分では消えない。
	if !strings.Contains(body, "確認の画面に止まりました") {
		t.Errorf("引き渡しの理由が `blocked` のものになっていない:\n%s", body)
	}
}

// runningStopEvent は「background_tasks に走行中の subagent が載っている Stop」を作る
// （設計 1-7 / 3-2）。
//
// **これは turn の終わりではない。**`background_tasks` が空でないので、
// 設計 3-2 の判定では「まだ動いている」である。
//
// sessionID: セッション UUID。
// transcriptPath: 親のセッションの記録のパス。
// promptID: prompt_id。
// agentID: `background_tasks[].id`（`agent_id` と同じ文字列である。設計 1-3）。
// agentType: `background_tasks[].agent_type`。
// 戻り値: hook のイベント。
func runningStopEvent(sessionID, transcriptPath, promptID, agentID, agentType string) hookserver.HookEvent {
	tasks := []hookserver.BackgroundTask{{
		ID:          agentID,
		Type:        "subagent",
		Status:      "running",
		Description: "実装",
		AgentType:   agentType,
	}}
	return hookserver.HookEvent{
		HookEventName:   "Stop",
		SessionID:       sessionID,
		TranscriptPath:  transcriptPath,
		PromptID:        promptID,
		BackgroundTasks: &tasks,
	}
}

// TestTurn_SubagentStartを取りこぼしてもbackground_tasksが走行中を拾う は、
// 走行中の判定の2本目の足を確かめる。
//
// 目的: 設計 3-11 の「`SubagentStart` から `SubagentStop` までの `agent_id` の集合と、
// 直近の `Stop` の `background_tasks` を足し合わせる。**どちらかが「動いている」と
// 言っているなら、動いていると扱う**」を守っていることを示す。
// **片方だけでは足りない。**`SubagentStart` を取りこぼすと1本目は空のままになる。
// 与える情報: `SubagentStart` を**送らず**、`background_tasks` に subagent が1件載った
// `Stop` だけを届けてから `blocked` になる台本。
// 成功条件: 走行中として扱われ、その `id` から組み立てた記録が通知に載ること。
func TestTurn_SubagentStartを取りこぼしてもbackground_tasksが走行中を拾う(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	// **これは想定して起こしている失敗である。**猶予のあいだ待っても終わらない
	// subagent を、走行中のまま止める場面をわざと作っている。
	fx.AllowLog("猶予のあいだにサブエージェントが終わらなかったので")
	fx.Tracker.AddIssue(sampleIssue(192, "Ready"))

	transcriptDir := t.TempDir()
	parent := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
	})
	subagentDir := filepath.Join(transcriptDir, "session-1", "subagents")
	if err := os.MkdirAll(subagentDir, 0o700); err != nil {
		t.Fatalf("subagents ディレクトリを作れません: %v", err)
	}
	running := filepath.Join(subagentDir, "agent-b2c0e5551ff248a2.jsonl")
	if err := os.WriteFile(running, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("走行中の subagent の記録を書けません: %v", err)
	}

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// **`SubagentStart` は送らない。**取りこぼした場合を作っている。
		fx.Orc.OnHook(runningStopEvent("session-1", parent, "p1", "b2c0e5551ff248a2", "impl"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "引き渡しの通知が投稿される", func() bool {
		return handoffCommentBody(fx, "I_node192") != ""
	})
	fx.WaitRunsDrained(t, 15*time.Second)

	body := handoffCommentBody(fx, "I_node192")
	for _, want := range []string{
		"【走行中のサブエージェントを止めました】",
		"1 件が動いていました",
		"impl(b2c0e5551ff248a2)",
		"**止めた時点で走っていた**サブエージェントの記録",
		running,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("引き渡しの通知に %q が無い:\n%s", want, body)
		}
	}
}

// agentStatusScript は、テスト用herdr mock の `agent.get` が返す状態を後から差し替えられる
// ようにする小さな台本である（#166 の検査で使う）。
//
// **既定の台本を写し取らずに書き直している。**`agent.get` の応答は
// `name` / `agent_status` / `interactive_ready` の3つで足りており、
// ここで確かめたいのは `agent_status` の値ひとつだからである。
type agentStatusScript struct {
	mu     sync.Mutex
	status string
	err    *rpcErr
}

// Set は次に返す状態を差し替える。
//
// status: 返す `agent_status`。
func (s *agentStatusScript) Set(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
	s.err = nil
}

// SetError は `agent.get` をエラーで返させる。
//
// err: 返すエラー。
func (s *agentStatusScript) SetError(err *rpcErr) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// Install はテスト用herdr mock へこの台本を差し込む。
//
// fx: 対象の fixture。
func (s *agentStatusScript) Install(fx *fixture) {
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		s.mu.Lock()
		status, err := s.status, s.err
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"type": "agent_info",
			"agent": map[string]any{
				"name": params["target"], "agent_status": status, "interactive_ready": true,
			},
		}, nil
	})
}

// TestTurn_差し戻して書き直している間はturnの終わりとしない は、#166 の欠陥そのものを押さえる。
//
// 目的: 設計 3-79 の「空の `Stop` は『turn が終わった』ではなく『止まってよいか hook に
// 尋ねた』である」を守っていることを示す。**`Stop` hook が `{"decision":"block"}` を返すと、
// Claude Code は turn を終わらせずに応答を書き直すが、その差し戻しは continuo に届かない。**
//
// **1つ上の TestTurn_空のStopのあとに来た走行中のStopも捨てない と同じ形の欠陥である。**
// あちらは Claude Code 自身が「まだ動いています」と申告してくる場合で、こちらは
// **誰も申告してこない**場合である。だから並べて置いてある。
//
// 与える情報: 空の `Stop` が1件届く。**`settle_ms` のあいだ、それ以上は何も届かない。**
// そのとき herdr は `working` を返す（差し戻された応答を書き直している最中である）。
// transcript には差し戻された側の応答A（`CONTINUO-STATUS: review`）だけが在る。
// 成功条件: `settle_ms` を何倍も過ぎても run が生きており、Status が `In Review` へ
// 動いておらず、次の指示も送られていないこと。書き直しが終わったあとに応答Bの表明
// （`CONTINUO-STATUS: working` ＝ Status を動かさない）が採られること。
func TestTurn_差し戻して書き直している間はturnの終わりとしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		// **settle_ms を広げるのは、テストを通すためではない。**既定の 50ms では
		// 「窓が閉じる前に裏取りした」のか「たまたま間に合った」のかを区別できない。
		cfg.Claude.SettleMs = 300
		// **poll_wait_ms も一緒に広げる。**fixture の既定は 200ms で、settle_ms のほうが
		// 長くなる。**その大小関係は internal/config/validate.go の
		// 「claude.settle_ms は claude.poll_wait_ms 以下にすること」が起動時に弾くので、
		// 利用者の手元では絶対に起きない。**弾かれる設定でテストを走らせると、
		// **待ち直しを settle_ms で刻んでいるのか poll_wait_ms で刻んでいるのかも測れない。**
		cfg.Claude.PollWaitMs = 5000
	}})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	// 応答A。**差し戻される側である。**ここで Status を動かすと pane を閉じてしまう。
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	// **着手の段では `idle` を返させる。**起動の落ち着きを待つところ（`herdr.startup_timeout_ms`）
	// も同じ `agent.get` を読むので、最初から `working` にすると着手そのものが失敗する。
	script := &agentStatusScript{status: "idle"}
	script.Install(fx)

	var mu sync.Mutex
	var prompts int
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		prompts++
		first := prompts == 1
		mu.Unlock()
		if first {
			// 1回目の応答が差し戻され、書き直している最中である。
			script.Set("working")
		}
		// **hook から見えるのは「空の Stop」だけである。**差し戻しは hook の戻り値なので
		// continuo には飛んでこない。
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "空の Stop のあとに herdr へ裏取りして待ち直す", func() bool {
		return strings.Contains(fx.Logs.String(),
			"空の Stop のあともエージェントが動いているので、turn の終わりとせずに待ち直します")
	})

	// settle_ms（300ms）と poll_wait_ms（200ms）の何倍も待っても、まだ生きていること。
	time.Sleep(1 * time.Second)
	if len(fx.Orc.RunningIdentifiers()) == 0 {
		t.Fatalf("書き直している最中に turn を終わらせて pane を閉じた:\n%s", fx.Logs.String())
	}
	if state := fx.Tracker.StateOf("PVTI_item188"); state == "In Review" {
		t.Fatalf("差し戻された側の応答Aで Status を動かした: %q", state)
	}
	mu.Lock()
	sent := prompts
	mu.Unlock()
	if sent != 1 {
		t.Fatalf("書き直している最中に次の指示を送った: %d 回", sent)
	}

	// 書き直しが終わった。応答Bが transcript へ足され、herdr は idle に戻り、
	// 2本目の空の `Stop` が届く。
	writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
		assistantLine("req2", "書き直しました。まだ続けます。\nCONTINUO-STATUS: working", false),
	})
	script.Set("idle")
	fx.Orc.OnHook(stopEvent("session-1", path, "p1"))

	waitFor(t, 10*time.Second, "書き直しが終わったので次の指示が送られる", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return prompts >= 2
	})
	if state := fx.Tracker.StateOf("PVTI_item188"); state == "In Review" {
		t.Fatalf("応答Bの表明ではなく応答Aの表明が採られた: %q", state)
	}
}

// TestTurn_裏取りが読めなければ従来どおりturnを終わらせる は、裏取りの失敗で待ちに倒れないことを
// 確かめる（設計 3-79）。
//
// 目的: **herdr が答えないときに待ちへ倒すと、turn が永久に終わらなくなる。**
// 読めなかったら偽を返して従来どおり進む、という決めを守っていることを示す。
// 与える情報: 空の `Stop` が1件届き、`agent.get` はエラーを返す。
// 成功条件: turn が終わり、run が畳まれること。
func TestTurn_裏取りが読めなければ従来どおりturnを終わらせる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Claude.SettleMs = 300
		// **poll_wait_ms を settle_ms 以上に置く。**下回ると
		// internal/config/validate.go が起動時に弾く設定になり、
		// **利用者の手元では起きない大小関係でテストが走ることになる。**
		cfg.Claude.PollWaitMs = 5000
	}})
	// **わざと herdr を答えなくしているので、その失敗から出る WARN は想定内である。**
	fx.AllowLog("herdr が答えません")
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	// **着手の段では読める状態にしておく。**起動の落ち着きを待つところも同じ
	// `agent.get` を読むので、最初からエラーにすると着手そのものが失敗する。
	script := &agentStatusScript{status: "idle"}
	script.Install(fx)

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// turn を送ったあとで herdr が答えなくなる。
		fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
		script.SetError(&rpcErr{Code: "internal_error", Message: "herdr が答えません"})
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "裏取りが読めなくても turn が終わる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
	// **run が畳まれただけでは足りない。**待ちに倒しても、出口の枝が `settle_ms` の
	// 後に拾い直すので、run はいずれ畳まれる。**「すぐ終わった」と「待たされてから
	// 終わった」を区別できるのは、この1行が出ていないことだけである。**
	if strings.Contains(fx.Logs.String(), "turn の終わりとせずに待ち直します") {
		t.Fatalf("裏取りが読めなかったのに待ち直している:\n%s", fx.Logs.String())
	}
}

// TestTurn_裏取りがidleなら空のStopでそのままturnが終わる は、既存の挙動を変えていないことを
// 確かめる（設計 3-79）。
//
// 目的: **裏取りを足したせいで、差し戻しの無い普通の turn が遅くなったり終わらなくなったり
// していないこと**を示す。
// 与える情報: 空の `Stop` が1件届き、`agent.get` は `idle` を返す。
// 成功条件: `settle_ms` の経過で turn が終わり、応答Aの表明が採られて run が畳まれること。
func TestTurn_裏取りがidleなら空のStopでそのままturnが終わる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Claude.SettleMs = 300
		// **poll_wait_ms を settle_ms 以上に置く。**下回ると
		// internal/config/validate.go が起動時に弾く設定になり、
		// **利用者の手元では起きない大小関係でテストが走ることになる。**
		cfg.Claude.PollWaitMs = 5000
	}})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	script := &agentStatusScript{status: "idle"}
	script.Install(fx)

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// **引き渡しのコメントは run が始まったあとに付いていなければならない。**
		// 先に付けておくと「この run のコメントが無い」と判定され、セッションの復元へ入る。
		fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "空の Stop で turn が終わり run が畳まれる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
	if state := fx.Tracker.StateOf("PVTI_item188"); state != "In Review" {
		t.Fatalf("応答Aの表明が採られていない: %q\n%s", state, fx.Logs.String())
	}
	if strings.Contains(fx.Logs.String(), "turn の終わりとせずに待ち直します") {
		t.Fatalf("差し戻しが無いのに待ち直している:\n%s", fx.Logs.String())
	}
}

// TestTurn_書き直しが来ないまま止まっていれば待ち続けない は、裏取りが外れたときの代償を
// 抑えていることを確かめる（設計 3-79）。
//
// 目的: **`working` は推測である。**遅い `Stop` hook が走っているだけでも `working` に
// 見える。**そのとき新しい `Stop` は二度と来ないので、待ち続けると巡回の stall 検知が
// `turn_timeout_ms`（既定1時間）で拾うまで run が空転する。**
// 待ち直しの1回分（`settle_ms`）が過ぎてもエージェントが動いていなければ、
// turn の終わりとして進むことを示す。
//
// 与える情報: 空の `Stop` が1件届き、そのとき `agent.get` は `working` を返す。
// **そのあと `Stop` は二度と来ず、`agent.get` は `idle` に変わる。**
// 成功条件: 待ち直しの1回分（`settle_ms`）を過ぎたところで turn が終わり、run が畳まれること。
func TestTurn_書き直しが来ないまま止まっていれば待ち続けない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Claude.SettleMs = 300
		// **poll_wait_ms を settle_ms 以上に置く。**下回ると
		// internal/config/validate.go が起動時に弾く設定になり、
		// **利用者の手元では起きない大小関係でテストが走ることになる。**
		//
		// **刻みの長さそのものは、このテストでは測っていない**（下の
		// TestTurn_遅いStophookでもpoll_wait_msを待たない が測る）。
		cfg.Claude.PollWaitMs = 5000
	}})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	script := &agentStatusScript{status: "idle"}
	script.Install(fx)

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// **遅い `Stop` hook がまだ走っているだけである。**差し戻してはいない。
		script.Set("working")
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "裏取りで待ち直す", func() bool {
		return strings.Contains(fx.Logs.String(),
			"空の Stop のあともエージェントが動いているので、turn の終わりとせずに待ち直します")
	})
	// **書き直しは始まっていなかった。**遅い hook が終わり、エージェントは止まっている。
	script.Set("idle")

	waitFor(t, 15*time.Second, "待ち続けずに turn の終わりとして進む", func() bool {
		return strings.Contains(fx.Logs.String(),
			"書き直しを待ちましたが Stop も来ずエージェントも動いていないので、turn の終わりとします")
	})
	waitFor(t, 15*time.Second, "run が畳まれる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
}

// TestTurn_遅いStophookでもpoll_wait_msを待たない は、待ち直しが `settle_ms` で刻まれて
// いることを確かめる（設計 3-79）。
//
// 目的: **`settle_ms`（既定2秒）より遅い `Stop` hook を1本でも持つ利用者は、差し戻して
// いなくてもこの待ち直しへ毎 turn 入る。**hook が走っている間、herdr から見たエージェントは
// `working` である。**だが差し戻していないので、新しい `Stop` は二度と来ない。**
// **刻みが `poll_wait_ms`（既定30秒）だと、その利用者は毎 turn ちょうど30秒を捨てる。**
// `max_dispatch_turns`（既定20）を掛けると1 run あたり10分である。
//
// **1つ上の TestTurn_書き直しが来ないまま止まっていれば待ち続けない とは見ているものが違う。**
// あちらは「いつかは終わる」ことを見ており、こちらは「**どれだけ待って終わるか**」を見る。
// あちらは `poll_wait_ms` で刻んでいても通る。
//
// 与える情報: `settle_ms` を 100ms、`poll_wait_ms` をその50倍の 5000ms に置く。
// 空の `Stop` が1件届き、そのとき `agent.get` は `working`（遅い hook がまだ走っている）。
// **そのあと `Stop` は二度と来ず、`agent.get` は `idle` に変わる。**
// 成功条件: `idle` に変えてから **2秒以内**に「turn の終わりとします」の1行が出ること。
// **`poll_wait_ms` で刻んでいれば 5 秒待つので、必ず間に合わない。**
func TestTurn_遅いStophookでもpoll_wait_msを待たない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		// **この2つの差が、このテストの物差しである。**同じ値にすると、
		// どちらで刻んでも同じ時間で終わってしまい、何も測れない。
		cfg.Claude.SettleMs = 100
		cfg.Claude.PollWaitMs = 5000
	}})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	script := &agentStatusScript{status: "idle"}
	script.Install(fx)

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// **遅い `Stop` hook が走っているだけである。**差し戻してはいない。
		script.Set("working")
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "裏取りで待ち直す", func() bool {
		return strings.Contains(fx.Logs.String(),
			"空の Stop のあともエージェントが動いているので、turn の終わりとせずに待ち直します")
	})

	// 遅い hook が終わった。エージェントは止まっている。
	script.Set("idle")
	waitFor(t, 2*time.Second, "poll_wait_ms を待たずに turn の終わりとして進む", func() bool {
		return strings.Contains(fx.Logs.String(),
			"書き直しを待ちましたが Stop も来ずエージェントも動いていないので、turn の終わりとします")
	})
	waitFor(t, 15*time.Second, "run が畳まれる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
}
