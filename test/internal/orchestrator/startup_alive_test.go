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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
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
// 目的: `agent.get` が `agent_not_found` を返し続けても、**その run のセッションから
// hook が届いていれば、continuo が run を諦めないこと。**
// 与える情報: **`herdr.startup_timeout_ms`（この fixture では2000ミリ秒）より長いあいだ**
// `agent_not_found` を返し、そのあいだ毎回この run の hook を1件流す `agent.get` の台本。
// **hook を流すのは `agent.start` より後である**（そこが証拠になる線である）。
// 成功条件: issue が `failure_state`（`Blocked`）へ落ちず、`pane.close` が1回も呼ばれず、
// **`agent.start` を1度もやり直さずに**、herdr が登録し直したあとに turn が送られること。
func TestStartup_hookが届いていれば起動していると扱う(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(235, "Ready"))

	// **登録されない時間を、起動の確認の期限より長く取る。**
	// **短いと、直していない実装でも「やり直しているうちに間に合った」で通ってしまう。**
	notFoundUntil := time.Now().Add(3 * time.Second)
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		if time.Now().Before(notFoundUntil) {
			// **作業中の Claude Code が hook を送ってくる場面である。**
			// **`agent.start` より後に届いたものだけが証拠になる。**
			fx.Orc.OnHook(toolHook("session-1", "PreToolUse"))
			return nil, agentNotFoundErr()
		}
		// 走っていた turn が終わり、入力待ちの画面が出た。herdr が登録し直す。
		return map[string]any{
			"type": "agent_info",
			"agent": map[string]any{
				"name": params["target"], "agent_status": "idle", "interactive_ready": true,
			},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "herdr が登録し直したあとに turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	if got := fx.Tracker.StateOf("PVTI_item235"); got == "Blocked" {
		t.Errorf("hook が届いているのに人間へ渡している: state=%s", got)
	}
	if n := fx.Herdr.CountMethod(herdr.MethodPaneClose); n != 0 {
		t.Errorf("生きている Claude Code の pane を閉じた: pane.close の回数 %d", n)
	}
	if n := fx.Herdr.CountMethod(herdr.MethodAgentStart); n != 1 {
		t.Errorf("hook が届いているのに agent.start をやり直した: agent.start の回数 %d", n)
	}
}

// TestStartup_待った先でworkingを見ても殺さない は、設計 3-80 の
// 「諦める時計は枝ごとに持たない」を検査する。
//
// 目的: hook を待っているうちに入口からの期限は過ぎる。**そのあと herdr がやっと登録して
// `working` を返した瞬間に run を捨ててはならない。**
// **待った先で殺すのでは、殺す時点が後ろへずれただけである。**
// 与える情報: `agent_not_found` → `working` → `idle` と変わる `agent.get` の台本。
// **`agent_not_found` の期間だけで `herdr.startup_timeout_ms`（2000ミリ秒）を超えさせ、
// そのあとも hook を流し続ける。**
// 成功条件: issue が `failure_state`（`Blocked`）へ落ちず、turn が送られること。
func TestStartup_待った先でworkingを見ても殺さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(235, "Ready"))

	notFoundUntil := time.Now().Add(3 * time.Second)
	workingUntil := notFoundUntil.Add(1 * time.Second)
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		// **どの段でも hook は届き続ける。**Claude Code は走り続けている。
		fx.Orc.OnHook(toolHook("session-1", "PreToolUse"))
		now := time.Now()
		if now.Before(notFoundUntil) {
			return nil, agentNotFoundErr()
		}
		status := "idle"
		if now.Before(workingUntil) {
			// herdr がやっと登録した。**まだ作業中なので `working` である。**
			status = "working"
		}
		return map[string]any{
			"type": "agent_info",
			"agent": map[string]any{
				"name": params["target"], "agent_status": status, "interactive_ready": true,
			},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 30*time.Second, "working を見送ったあとに turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	if got := fx.Tracker.StateOf("PVTI_item235"); got == "Blocked" {
		t.Errorf("hook が届いているのに working の期限切れで人間へ渡している: state=%s", got)
	}
	if n := fx.Herdr.CountMethod(herdr.MethodPaneClose); n != 0 {
		t.Errorf("生きている Claude Code の pane を閉じた: pane.close の回数 %d", n)
	}
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

// TestStartup_hookが止まったら諦める は、設計 3-80 の時計が止まらないことを検査する。
//
// 目的: hook が届いたあとに止まったら、**その最後の hook から
// `herdr.startup_timeout_ms` ぶん待って諦めること。**
// **「1件でも届いたら永久に待つ」になってはならない。**
// 与える情報: 1回目だけ hook を流し、あとは `agent_not_found` を返し続ける
// `agent.get` の台本。
// 成功条件: issue が `failure_state`（`Blocked`）へ落ちること。
func TestStartup_hookが止まったら諦める(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Agent.MaxRetries = 0 },
	})
	// **これは想定して起こしている失敗である。**
	fx.AllowLog("agent.get", "起動していない", "run を諦めて")
	fx.Tracker.AddIssue(sampleIssue(235, "Ready"))

	var gets atomic.Int32
	fx.Herdr.Handle(herdr.MethodAgentGet, func(map[string]any) (any, *rpcErr) {
		if gets.Add(1) == 1 {
			fx.Orc.OnHook(toolHook("session-1", "PreToolUse"))
		}
		return nil, agentNotFoundErr()
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 40*time.Second, "hook が止まったので人間へ渡す", func() bool {
		return fx.Tracker.StateOf("PVTI_item235") == "Blocked"
	})
	fx.WaitRunsDrained(t, 20*time.Second)
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
			// **hook の新しさの線。**広げないと「hook が来ていない」と見なされて待たない。
			cfg.Claude.SettleMs = 5000
			// **待ちの上限。**下の goroutine が動くだけの余裕を取る。
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
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			// **hook の新しさでは弾けないことを確かめる。**権限の確認のあと、
			// `LastHookAt` は真新しいままである。
			cfg.Claude.SettleMs = 5000
		},
	})
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
