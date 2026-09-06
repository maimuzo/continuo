// Package orchestrator_test のうち、この1本は「生きているのに殺す」を止める2つの変更を検査する。
//
//	設計 3-80  起動の確認を `agent.get` 1本に頼らない。hook の着信も証拠にする
//	設計 3-81  run を終える前に、バックグラウンド処理の申告を1度見る
//
// **どちらも実運用で起きた**（2026-09-05）。復帰した Claude Code が起動直後から作業を始め、
// herdr が入力待ちの画面を一度も見ずに agent を登録しなかったため、continuo が
// 「起動していない」と判断して pane を閉じ、`git merge` の解消の最中だった本人を殺した。
package orchestrator_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/hookserver"
)

// agentNotFoundErr は herdr の `agent_not_found` を返す台本の応答である。
//
// **`agent.get` が答えられるのは「herdr がその agent を登録しているか」だけである。**
// 登録するのは入力待ちの画面を見分けたときなので、作業中の Claude Code は登録されない。
func agentNotFoundErr() *rpcErr {
	return &rpcErr{Code: "agent_not_found", Message: "agent not found: continuo-hello-world-1"}
}

// TestStartup_hookが届いていれば起動していると扱う は、設計 3-80 の中心を検査する。
//
// 目的: `agent.get` が `agent_not_found` を返しても、**その run のセッションから
// 作業中の hook が届いていれば、continuo が pane を閉じず、
// 走っている turn へ1回目の指示も投げないこと。**
// 与える情報: `agent_not_found` を返しながら、この run の `PreToolUse` を1件流す
// `agent.get` の台本。**流すのは `agent.start` より後である**（そこが証拠になる線である）。
// 成功条件（3つ）: issue が `failure_state`（`Blocked`）へ落ちず、`pane.close` が
// 1回も呼ばれず、**その時点で `agent.prompt` が1回も呼ばれていないこと。**
// **そのうえで、走っていた turn が終わったら1回目の指示が送られること。**
func TestStartup_hookが届いていれば起動していると扱う(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	// **これは想定して起こしている失敗である。**1回目の指示のあと `blocked` にして、
	// run を確実に終わらせている。
	fx.AllowLog("権限の確認で止まりました", "run を終えます")
	// **herdr がまだ登録していないので、turn の終わりの裏取り（設計 3-79）は答えを得られない。**
	// **それでよい。**あの裏取りは「読めなかったら従来どおり進む」と決めてあり、
	// **turn の終わりの判定そのものは hook（`Stop`）だけで足りている。**
	fx.AllowLog("turn の終わりの裏取りができませんでした")
	// **CI でだけ出ることがある。**herdr の mock が応答を書く前にテストが次へ進むと、
	// 復元の経路が「agent は登録されていないが Claude Code は生きている」を検知して WARN を出す。
	// **これは設計どおりの動きで、このテストが確かめたいこと（hook が届いていれば起動と扱う）とは別である。**
	fx.AllowLog("復元した Claude Code が走っているので、コメントを書かせる指示は送れません")
	fx.Tracker.AddIssue(sampleIssue(235, "Ready"))

	transcriptDir := t.TempDir()
	parent := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
	})

	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		// **作業中の Claude Code が hook を送ってくる場面である。**
		// **`agent.start` より後に届いたものだけが証拠になる。**
		fx.Orc.OnHook(toolHook("session-1", "PreToolUse"))
		return nil, agentNotFoundErr()
	})
	// **1回目の指示が送られたことを確かめたら、そこで run を終わらせる。**
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "起動の確認が「Claude Code は走っている」で終わる", func() bool {
		return strings.Contains(fx.Logs.String(), "1回目の指示を送らずに turn の終わりを待ちます")
	})

	// **ここまでで、走っている turn へ何もしていないことを確かめる。**
	if n := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); n != 0 {
		t.Errorf("走っている turn へ1回目の指示を投げた: agent.prompt の回数 %d", n)
	}
	if n := fx.Herdr.CountMethod(herdr.MethodPaneClose); n != 0 {
		t.Errorf("生きている Claude Code の pane を閉じた: pane.close の回数 %d", n)
	}
	if n := fx.Herdr.CountMethod(herdr.MethodAgentStart); n != 1 {
		t.Errorf("走っている Claude Code へ agent.start をやり直した: agent.start の回数 %d", n)
	}
	if got := fx.Tracker.StateOf("PVTI_item235"); got == "Blocked" {
		t.Errorf("hook が届いているのに人間へ渡している: state=%s", got)
	}

	// **走っていた turn が終わる。**turn ループは hook だけでこれを見分ける。
	fx.Orc.Tick(context.Background())
	fx.Orc.OnHook(stopEvent("session-1", parent, "p1"))
	waitFor(t, 20*time.Second, "turn の終わりのあとに1回目の指示が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
}

// TestStartup_hookが1件も来なければこれまでどおり諦める は、設計 3-80 が
// 諦める道を壊していないことを検査する。
//
// 目的: hook が1件も届かない run では、**これまでどおり `herdr.startup_timeout_ms` で
// 見切って人間へ渡すこと。**
// **hook を証拠にする変更で、起動できていない run を永久に待つようになってはならない。**
// 与える情報: `agent.get` が常に `agent_not_found` を返す台本と、リトライを1回で
// 使い切らせる設定。
// 成功条件: issue が `failure_state`（`Blocked`）へ落ちること。
func TestStartup_hookが1件も来なければこれまでどおり諦める(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		// **リトライを1回で使い切らせる。**既定（3回）のままだと、バックオフの合計が
		// テストの待ち時間を超える。
		Mutate: func(cfg *config.Config) { cfg.Agent.MaxRetries = 0 },
	})
	// **これは想定して起こしている失敗である。**
	fx.AllowLog("agent.get", "起動していない", "run を諦めて")
	fx.Tracker.AddIssue(sampleIssue(235, "Ready"))

	fx.Herdr.Handle(herdr.MethodAgentGet, func(map[string]any) (any, *rpcErr) {
		return nil, agentNotFoundErr()
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 40*time.Second, "hook が来ないので人間へ渡す", func() bool {
		return fx.Tracker.StateOf("PVTI_item235") == "Blocked"
	})
	fx.WaitRunsDrained(t, 20*time.Second)
}

// TestStartup_SessionStartだけでは走っているとみなさない は、設計 3-80 の
// 「`SessionStart` は数えない」を検査する。
//
// 目的: `SessionStart` は**起動しただけで出る。**これを「走っている証拠」に数えると、
// **起動して入力待ちのまま止まった Claude Code まで「走っている」と読み、
// `agent.start` のやり直しが1回も起きず、来ない `Stop` を待ち続けることになる。**
// 与える情報: `SessionStart` だけを流し、`agent_not_found` を返し続ける `agent.get` の台本と、
// リトライを1回で使い切らせる設定。
// 成功条件: **`agent.start` がやり直されること**と、issue が `failure_state`（`Blocked`）へ
// 落ちること。**「turn の終わりを待ちます」の行が出ないこと。**
func TestStartup_SessionStartだけでは走っているとみなさない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Agent.MaxRetries = 0 },
	})
	// **これは想定して起こしている失敗である。**
	fx.AllowLog("agent.get", "起動していない", "run を諦めて")
	fx.Tracker.AddIssue(sampleIssue(235, "Ready"))

	fx.Herdr.Handle(herdr.MethodAgentGet, func(map[string]any) (any, *rpcErr) {
		// **起動はしたが、入力待ちのまま止まっている。**`SessionStart` しか出ない。
		fx.Orc.OnHook(toolHook("session-1", "SessionStart"))
		return nil, agentNotFoundErr()
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 40*time.Second, "SessionStart だけでは走っているとみなさず、人間へ渡す", func() bool {
		return fx.Tracker.StateOf("PVTI_item235") == "Blocked"
	})
	fx.WaitRunsDrained(t, 20*time.Second)

	if n := fx.Herdr.CountMethod(herdr.MethodAgentStart); n < 2 {
		t.Errorf("SessionStart を「走っている」と読んで agent.start をやり直していない: agent.start の回数 %d", n)
	}
	if strings.Contains(fx.Logs.String(), "1回目の指示を送らずに turn の終わりを待ちます") {
		t.Errorf("SessionStart だけで「走っている」と判断した")
	}
}

// TestStartup_idle_promptだけでは走っているとみなさない は、設計 3-80b の
// 「入力待ちでも出る hook を数えない」の2つ目を検査する。
//
// 目的: `Notification` の `idle_prompt` は、**turn が終わったあとの無音を
// 60.040〜60.058 秒で破る**（12/12 の実測。設計 1-2）。
// **`herdr.startup_timeout_ms` の既定（60000ミリ秒）とほぼ同時に飛ぶ**ので、
// **起動の確認のいちばん危ないところで当たる。**数えると、入力待ちで固まった Claude Code を
// 「走っている」と読み、`agent.start` のやり直しが1回も起きなくなる。
// 与える情報: `idle_prompt` の `Notification` だけを流し、`agent_not_found` を返し続ける台本。
// 成功条件: **`agent.start` がやり直されること**と、issue が `failure_state`（`Blocked`）へ
// 落ちること。
func TestStartup_idle_promptだけでは走っているとみなさない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Agent.MaxRetries = 0 },
	})
	fx.AllowLog("agent.get", "起動していない", "run を諦めて")
	fx.Tracker.AddIssue(sampleIssue(235, "Ready"))

	fx.Herdr.Handle(herdr.MethodAgentGet, func(map[string]any) (any, *rpcErr) {
		// **起動して入力待ちのまま座っているだけの Claude Code である。**
		fx.Orc.OnHook(hookserver.HookEvent{
			HookEventName:    "Notification",
			SessionID:        "session-1",
			NotificationType: "idle_prompt",
		})
		return nil, agentNotFoundErr()
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 40*time.Second, "idle_prompt だけでは走っているとみなさず、人間へ渡す", func() bool {
		return fx.Tracker.StateOf("PVTI_item235") == "Blocked"
	})
	fx.WaitRunsDrained(t, 20*time.Second)

	if n := fx.Herdr.CountMethod(herdr.MethodAgentStart); n < 2 {
		t.Errorf("idle_prompt を「走っている」と読んで agent.start をやり直していない: agent.start の回数 %d", n)
	}
	if strings.Contains(fx.Logs.String(), "1回目の指示を送らずに turn の終わりを待ちます") {
		t.Errorf("idle_prompt だけで「走っている」と判断した")
	}
}

// TestStartup_走っていると判断したrunにも働き始めた時刻を入れる は、設計 3-80c を検査する。
//
// 目的: `ErrStartupBusy` の道は `beginTurn` を通らないので、**働き始めた時刻を入れないと
// `ensureAgentComment` が「1回も turn を送っていないので書かせる材料が無い」として抜け、
// 成果のコメントを確かめる網が黙って外れる。**
// 与える情報: 起動の確認で `ErrStartupBusy` へ倒れ、そのあと走っていた turn が
// `In Review` の表明つきで終わる台本。**エージェントは成果のコメントを書かない。**
// 成功条件: **「セッションを復元して書かせます」の行が出ること。**
// **時刻を入れない実装では「turn を1回も送っていないので、コメントの確認は行いません」が出る。**
func TestStartup_走っていると判断したrunにも働き始めた時刻を入れる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.AllowLog("turn の終わりの裏取りができませんでした", "run を終えます",
		"セッションを復元できません", "Status を落とせません", "コメントを書かせる",
		"復帰する先のセッションに会話の記録が無い", "何をしたのかを issue に書き残しませんでした")
	fx.Tracker.AddIssue(sampleIssue(235, "Ready"))

	transcriptDir := t.TempDir()
	parent := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	fx.Herdr.Handle(herdr.MethodAgentGet, func(map[string]any) (any, *rpcErr) {
		fx.Orc.OnHook(toolHook("session-1", "PreToolUse"))
		return nil, agentNotFoundErr()
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "起動の確認が「Claude Code は走っている」で終わる", func() bool {
		return strings.Contains(fx.Logs.String(), "1回目の指示を送らずに turn の終わりを待ちます")
	})

	// **走っていた turn が、成果のコメントを書かないまま終わる。**
	fx.Orc.Tick(context.Background())
	fx.Orc.OnHook(stopEvent("session-1", parent, "p1"))
	fx.WaitRunsDrained(t, 40*time.Second)

	logs := fx.Logs.String()
	if strings.Contains(logs, "turn を1回も送っていないので、コメントの確認は行いません") {
		t.Errorf("走っていると判断した run で、成果のコメントを確かめる網が外れた:\n%s", logs)
	}
	if !strings.Contains(logs, "この run のコメントが無いので、セッションを復元して書かせます") {
		t.Errorf("成果のコメントを確かめていない:\n%s", logs)
	}
}

// TestStartup_permission_promptだけでは走っているとみなさない は、設計 3-80b の
// 「`Notification` は全部外す」を検査する。
//
// 目的: `permission_prompt` のとき turn は走っているが、**この判定へ来るのは herdr が
// agent を登録していないときだけである。**登録していないので `agent.get` は `blocked` を
// 返せず、esc を送る道（設計 3-11）へ入れない。
// **数えると、人間が確認の画面に答えるまで来ない `Stop` を待ち続けることになる。**
// 与える情報: `permission_prompt` の `Notification` だけを流し、`agent_not_found` を
// 返し続ける台本。
// 成功条件: issue が `failure_state`（`Blocked`）へ落ちること。
func TestStartup_permission_promptだけでは走っているとみなさない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Agent.MaxRetries = 0 },
	})
	fx.AllowLog("agent.get", "起動していない", "run を諦めて", "権限の確認で止まりました")
	fx.Tracker.AddIssue(sampleIssue(235, "Ready"))

	fx.Herdr.Handle(herdr.MethodAgentGet, func(map[string]any) (any, *rpcErr) {
		fx.Orc.OnHook(hookserver.HookEvent{
			HookEventName:    "Notification",
			SessionID:        "session-1",
			NotificationType: "permission_prompt",
		})
		return nil, agentNotFoundErr()
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 40*time.Second, "permission_prompt だけでは走っているとみなさず、人間へ渡す", func() bool {
		return fx.Tracker.StateOf("PVTI_item235") == "Blocked"
	})
	fx.WaitRunsDrained(t, 20*time.Second)

	if strings.Contains(fx.Logs.String(), "1回目の指示を送らずに turn の終わりを待ちます") {
		t.Errorf("permission_prompt だけで「走っている」と判断した（esc を送れないので永久に待つ）")
	}
}

// TestFinish_道連れにしたバックグラウンド処理を通知へ書く は、設計 3-81 の記録を検査する。
//
// 目的: `background_tasks` に処理が載ったまま pane を閉じたなら、**何を道連れにしたかを
// 引き渡しの通知に書くこと。**書かないと、次に同じセッションへ復帰したときに発火する
// 「前のバックグラウンドコマンドに完了の記録が無い」という通知の出どころを、人間が辿れない。
// 与える情報: `type` が `shell` のバックグラウンド処理を1件載せた `Stop` を流してから
// `blocked` になる台本（`blocked` の道には「申告を空へ戻す `Stop`」が来ない）。
// 成功条件: 通知に `shell` の1件が載ること。**`subagent` に限らないことがここで決まる。**
func TestFinish_道連れにしたバックグラウンド処理を通知へ書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	// **これは想定して起こしている失敗である。**申告が残ったまま pane を閉じる場面を
	// わざと作っている。
	fx.AllowLog("バックグラウンド処理が残ったまま pane を閉じます")
	fx.Tracker.AddIssue(sampleIssue(236, "Ready"))

	transcriptDir := t.TempDir()
	parent := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
	})

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		fx.Orc.OnHook(runningShellStopEvent("session-1", parent, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "引き渡しの通知が投稿される", func() bool {
		return handoffCommentBody(fx, "I_node236") != ""
	})
	fx.WaitRunsDrained(t, 20*time.Second)

	body := handoffCommentBody(fx, "I_node236")
	for _, want := range []string{
		"止めた時点で走っていたバックグラウンド処理",
		"shell(bmr1ksf9i) sleep 45",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("引き渡しの通知に %q が無い:\n%s", want, body)
		}
	}
}

// TestFinish_走っているあいだは待ってから閉じる は、設計 3-81 の待ちそのものを検査する。
//
// 目的: 申告が残っていて、**そのうえで hook が来ていて、確認の画面でも止まっていないなら**、
// pane を閉じる前に終わるのを待つこと。**待たずに閉じると、走っていたものが道連れになる。**
// 与える情報: 指示の回数の上限を1回にして打ち切らせ、**turn が終わったあと
// （`stillWorkingAfterStop` の `agent.get` の中）で `shell` を1件載せた `Stop` を流す**台本。
// **待ちが始まったのをログで見てから空の `Stop` を流す goroutine** を添える。
// 成功条件: 「バックグラウンド処理が終わったので pane を閉じます」が出ること。
// **待たない実装では「待ちます」の行が出ないので goroutine が動かず、この行も出ない。**
func TestFinish_走っているあいだは待ってから閉じる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			// **指示の回数の上限を1回にする。**1回目の turn の終わりで打ち切りへ進む。
			cfg.Agent.MaxDispatchTurns = 1
			// **待ちの上限。**下の goroutine が動くだけの余裕を取る。
			// **`claude.settle_ms` は既定のままにする。**広げないと通らない形にしていたが、
			// **それは「既定では1回も待たない」ことの証拠だった**（設計 3-81）。
			cfg.Claude.PollWaitMs = 5000
		},
	})
	// **これは想定して起こしている失敗である。**指示の回数の上限に達したので人間へ渡す。
	fx.AllowLog("指示の回数が、上限の", "run を終えます")
	fx.Tracker.AddIssue(sampleIssue(236, "Ready"))

	transcriptDir := t.TempDir()
	parent := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
	})

	var prompted atomic.Bool
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// **turn は普通に終わる**（`background_tasks` が空の `Stop`）。
		prompted.Store(true)
		fx.Orc.OnHook(stopEvent("session-1", parent, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})
	// **turn が終わったあとにバックグラウンド処理が申告される場面を作る。**
	// `confirmTurnEnd` は settle の窓が閉じるとき `agent.get` で裏を取る（設計 3-79）。
	// **そこから先に走る `finishRunClaimed` が、この申告を見ることになる。**
	//
	// **turn を送る前の `agent.get`（起動の確認）では流さない。**そこで流すと、
	// turn を送るときの `beginTurn` が控えを空へ戻してしまう。
	var injected atomic.Bool
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		if prompted.Load() && injected.CompareAndSwap(false, true) {
			fx.Orc.OnHook(runningShellStopEvent("session-1", parent, "p2"))
			// **待ちが始まったのを見てから終わらせる。**時間で当てると、
			// 待ちに入る前に届いて「待たずに閉じた」と見分けが付かなくなる。
			go func() {
				deadline := time.Now().Add(20 * time.Second)
				for time.Now().Before(deadline) {
					if strings.Contains(fx.Logs.String(), "pane を閉じる前に待ちます") {
						fx.Orc.OnHook(stopEvent("session-1", parent, "p2"))
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
			}()
		}
		return map[string]any{
			"type":  "agent_info",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 40*time.Second)

	if !strings.Contains(fx.Logs.String(), "バックグラウンド処理が終わったので pane を閉じます") {
		t.Errorf("走っているあいだ待っていない（待ちが終わった記録が無い）:\n%s", fx.Logs.String())
	}
}

// TestFinish_確認の画面で止まっているなら待たない は、設計 3-81 の
// 「待たない条件」を検査する。
//
// 目的: `blocked` で止まった Claude Code は新しい `Stop` を二度と出さないので、
// **待っても申告は1件も減らない。**そこで待つと、直前の `waitForRunningSubagents` と
// 合わせて `claude.poll_wait_ms` を2回ぶん、人間への引き渡しが遅れるだけになる。
// 与える情報: `shell` を1件載せた `Stop` を流してから `blocked` になる台本。
// 成功条件: 「確認の画面で止まっているので、バックグラウンド処理を待ちません」が出て、
// **「pane を閉じる前に待ちます」が出ないこと。**
func TestFinish_確認の画面で止まっているなら待たない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.AllowLog("バックグラウンド処理が残ったまま pane を閉じます")
	fx.Tracker.AddIssue(sampleIssue(236, "Ready"))

	transcriptDir := t.TempDir()
	parent := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
	})

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		fx.Orc.OnHook(runningShellStopEvent("session-1", parent, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 30*time.Second, "引き渡しの通知が投稿される", func() bool {
		return handoffCommentBody(fx, "I_node236") != ""
	})
	fx.WaitRunsDrained(t, 30*time.Second)

	logs := fx.Logs.String()
	if !strings.Contains(logs, "確認の画面で止まっているので、バックグラウンド処理を待ちません") {
		t.Errorf("確認の画面で止まっているのに待とうとしている:\n%s", logs)
	}
	if strings.Contains(logs, "pane を閉じる前に待ちます") {
		t.Errorf("確認の画面で止まっているのに待った（引き渡しがそのぶん遅れる）")
	}
}

// TestFinish_復元した先が走っていたらコメントの依頼で殺さない は、設計 3-80 の
// `ErrStartupBusy` を、もう1つの呼び出し側でも守っていることを検査する。
//
// 目的: コメントを書かせる復元（設計 3-25 の9段）の段6 も `confirmStartup` を通る。
// **そこで `ErrStartupBusy` を「落ち着かなかった」として扱うと、
// 走っている本人の pane を閉じたうえで、「作業を終えたと表明したのに何も書き残さなかった」
// という事実と違う理由を issue へ書く。**
// 与える情報: コメントを書かないまま指示の回数の上限に達する run と、
// **復元の `agent.start` のあとだけ `agent_not_found` を返しながら作業中の hook を流す**
// `agent.get` の台本。
// 成功条件: 「復元した Claude Code が走っているので」の警告が出て、
// **`failCommentRecovery` の文面（「何をしたのかを issue に書き残しませんでした」）が
// 1件も投稿されないこと。**
func TestFinish_復元した先が走っていたらコメントの依頼で殺さない(t *testing.T) {
	transcriptDir := t.TempDir()
	parent := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
	})
	fx := newFixture(t, fixtureOptions{
		// **復帰できる会話の記録がある状態にする。**無いと段3b で先に戻ってしまい、
		// 段6 まで届かない。
		TranscriptRoot: transcriptDir,
		// **1回目の turn の終わりで打ち切らせる。**そこからコメントの確認へ進む。
		Mutate: func(cfg *config.Config) { cfg.Agent.MaxDispatchTurns = 1 },
	})
	// **これは想定して起こしている失敗である。**
	fx.AllowLog("指示の回数が、上限の", "run を終えます",
		"復元した Claude Code が走っているので")
	fx.Tracker.AddIssue(sampleIssue(236, "Ready"))

	var starts atomic.Int32
	fx.Herdr.Handle(herdr.MethodAgentStart, func(params map[string]any) (any, *rpcErr) {
		starts.Add(1)
		return map[string]any{
			"type": "agent_started",
			"agent": map[string]any{
				"name": params["name"], "agent_status": "idle",
				"interactive_ready": true, "pane_id": params["pane_id"],
			},
		}, nil
	})
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		if starts.Load() >= 2 {
			// **復元した Claude Code が、前の会話の続きを走らせている場面である。**
			fx.Orc.OnHook(toolHook("session-1", "PreToolUse"))
			return nil, agentNotFoundErr()
		}
		return map[string]any{
			"type":  "agent_info",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// **エージェントはコメントを書かない。**確認の経路を必ず通す。
		fx.Orc.OnHook(stopEvent("session-1", parent, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 40*time.Second)

	if !strings.Contains(fx.Logs.String(), "復元した Claude Code が走っているので") {
		t.Errorf("走っている Claude Code を「落ち着かない」として扱った:\n%s", fx.Logs.String())
	}
	for _, c := range fx.Tracker.CommentsOf("I_node236") {
		if strings.Contains(c.Body, "何をしたのかを issue に書き残しませんでした") {
			t.Errorf("走っている最中なのに「書き残さなかった」と issue へ書いた:\n%s", c.Body)
		}
	}
	// **黙って戻ってはならない。**pane はどのみち閉じるので、
	// **閉じたことと成果が残っていないことを人間へ渡す道を通る。**
	// **この台本では引き渡しの通知の枠を打ち切りの理由が先に取っているので**
	// （1つの run につき1件。`takeHandoffPost`）、**ここで確かめるのはログである。**
	if !strings.Contains(fx.Logs.String(), "復元した Claude Code が走っているので、コメントを書かせる指示は送れません") {
		t.Errorf("走っていたことを記録していない:\n%s", fx.Logs.String())
	}
}

// TestFinish_切り捨てたバックグラウンド処理の件数を書く は、設計 3-81b の
// 「黙って切り捨てない」を検査する。
//
// 目的: 通知へ載せるのは `handoffBackgroundTaskLimit`（5件）までである。
// **黙って上から5件だけ出すと、読んだ人は「道連れになったのはこれで全部だ」と読む。**
// 与える情報: `shell` を6件載せた `Stop` を流してから `blocked` になる台本。
// 成功条件: 通知に「ほかに 1 件」が載ること。
func TestFinish_切り捨てたバックグラウンド処理の件数を書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.AllowLog("バックグラウンド処理が残ったまま pane を閉じます")
	fx.Tracker.AddIssue(sampleIssue(236, "Ready"))

	transcriptDir := t.TempDir()
	parent := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
	})

	var first atomic.Bool
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		if first.CompareAndSwap(false, true) {
			fx.Orc.OnHook(manyShellStopEvent("session-1", parent, "p1", 6))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "引き渡しの通知が投稿される", func() bool {
		return handoffCommentBody(fx, "I_node236") != ""
	})
	fx.WaitRunsDrained(t, 20*time.Second)

	body := handoffCommentBody(fx, "I_node236")
	if !strings.Contains(body, "ほかに 1 件") {
		t.Errorf("切り捨てた件数が通知に載っていない:\n%s", body)
	}
}

// manyShellStopEvent は `type` が `shell` のバックグラウンド処理を n 件載せた `Stop` を作る。
//
// sessionID: セッション UUID。
// transcriptPath: transcript のパス。
// promptID: prompt_id。
// n: 載せる件数。
// 戻り値: hook のイベント。
func manyShellStopEvent(sessionID, transcriptPath, promptID string, n int) hookserver.HookEvent {
	running := make([]hookserver.BackgroundTask, 0, n)
	for i := range n {
		running = append(running, hookserver.BackgroundTask{
			ID:      fmt.Sprintf("bg%02d", i),
			Type:    "shell",
			Status:  "running",
			Command: fmt.Sprintf("sleep %d", 10+i),
		})
	}
	ev := stopEvent(sessionID, transcriptPath, promptID)
	ev.BackgroundTasks = &running
	return ev
}

// TestFinish_申告が空なら待たずに閉じる は、設計 3-81 が正常な run を遅らせないことを検査する。
//
// 目的: `background_tasks` が空なら、**1ミリ秒も待たずにこれまでどおり閉じること。**
// **待ちを足したせいで、走っていない run まで `claude.poll_wait_ms` ぶん遅れてはならない。**
// 与える情報: `shell` を1件載せた `Stop` を流したあと、esc を送る段で
// 「`background_tasks` が空の `Stop`」を流す台本。
// 成功条件: 「残ったまま閉じます」の警告が1行も出ないこと。
// **`AllowLog` を宣言していないので、出れば fixture の検査がこのテストを落とす。**
func TestFinish_申告が空なら待たずに閉じる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(236, "Ready"))

	transcriptDir := t.TempDir()
	parent := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
	})

	// **1回目の turn でだけ流す。**コメントを書かせる復元（設計 3-25 の9段）も
	// `agent.prompt` を通るので、毎回流すと**pane を閉じたあとにまた申告が立つ。**
	var first atomic.Bool
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		if first.CompareAndSwap(false, true) {
			fx.Orc.OnHook(runningShellStopEvent("session-1", parent, "p1"))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})
	// **esc は `finishRun` の直前に送られる**（設計 3-11）。そこで空の `Stop` を流すと、
	// run を終える前の判定は「1件も走っていない」を見る。
	fx.Herdr.Handle(herdr.MethodAgentSendKeys, func(map[string]any) (any, *rpcErr) {
		fx.Orc.OnHook(stopEvent("session-1", parent, "p1"))
		return map[string]any{"type": "keys_sent"}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "引き渡しの通知が投稿される", func() bool {
		return handoffCommentBody(fx, "I_node236") != ""
	})
	fx.WaitRunsDrained(t, 20*time.Second)

	if body := handoffCommentBody(fx, "I_node236"); strings.Contains(body, "止めた時点で走っていたバックグラウンド処理") {
		t.Errorf("申告が空なのに道連れにしたと書いている:\n%s", body)
	}
}
