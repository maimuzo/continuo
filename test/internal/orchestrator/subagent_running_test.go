package orchestrator_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/hookserver"
)

// 走行中の subagent の印を**下ろす**経路を確かめるテストを集めたファイルである（設計 3-11）。
//
// **立てる経路は turn_test.go が見ている。**こちらは下ろす側だけを見る。
// **下ろし損なうと、終わった subagent を「走行中」と数える。**そうなると
// 猶予いっぱい待たされたうえ、引き渡しの通知に
// 「走行中のサブエージェントを止めました」「書きかけの変更が残っている可能性があります」と
// **事実でないことを書く。**issue #65 の「案内が当てはまらない」と同じ形である。

// subagentStopWithTasksEvent は「`background_tasks` を持ってくる `SubagentStop`」を作る。
//
// **実測記録の `SubagentStop` はこの形だった**（docs/evidence/hooks_probe_20260817.jsonl）。
// **いま終わったその subagent が、`status` が `running` のまま載っていた。**
// だから `SubagentStop` を受けた側は、`background_tasks` 側の印も明示的に外さなければ
// ならない。
//
// sessionID: セッション UUID。
// agentID: `agent_id`。
// agentType: `agent_type`。
// tasks: `background_tasks` に載せる `agent_id` の並び（`agent_type` は agentType を使う）。
// 戻り値: hook のイベント。
func subagentStopWithTasksEvent(sessionID, agentID, agentType string, tasks []string) hookserver.HookEvent {
	bt := make([]hookserver.BackgroundTask, 0, len(tasks))
	for _, id := range tasks {
		bt = append(bt, hookserver.BackgroundTask{
			ID:          id,
			Type:        "subagent",
			Status:      "running",
			Description: "実装",
			AgentType:   agentType,
		})
	}
	ev := subagentStopEvent(sessionID, agentID, agentType)
	ev.BackgroundTasks = &bt
	return ev
}

// assertNoRunningSubagentNotice は、引き渡しの通知が「走行中のサブエージェントを止めました」を
// **書いていない**ことを確かめる。
//
// **`blocked` の引き渡し自体は起きていなければならない。**通知そのものが無いのを
// 「書いていない」と読み違えないよう、`blocked` の理由が載っていることも見る。
//
// t: 呼び出し元のテスト。
// body: 引き渡しの通知の本文。
func assertNoRunningSubagentNotice(t *testing.T, body string) {
	t.Helper()
	if !strings.Contains(body, "確認の画面に止まりました") {
		t.Fatalf("`blocked` の引き渡しになっていない:\n%s", body)
	}
	if strings.Contains(body, "【走行中のサブエージェントを止めました】") {
		t.Errorf("終わった subagent を走行中と数えている:\n%s", body)
	}
	// **「止めた時点で走っていた」の印も付いてはならない。**印が付いた記録は、
	// 人間が「これが原因だ」と当たりを付けて読みにいく先である。
	if strings.Contains(body, "**止めた時点で走っていた**") {
		t.Errorf("終わった subagent の記録に「走っていた」印を付けている:\n%s", body)
	}
}

// TestSubagent_background_tasksが空のStopで走行中の印が下りる は、
// Claude Code 自身の申告で数え直すことを確かめる。
//
// 目的: 設計 3-11 の「空に戻す契機は turn を送るときと**`background_tasks` が空の `Stop`**
// を受けたとき」を守っていることを示す。**`background_tasks` が空なのは「1つも走っていない」
// という Claude Code 自身の申告であり、こちらの数え方より確かである。**
// `SubagentStop` を1件取りこぼしても、ここで印が残り続けてはならない。
// 与える情報: 2本の申告の両方に印を立ててから、`background_tasks` が空の `Stop` を
// 届けて `blocked` になる台本。**`SubagentStop` は1件も送らない**（取りこぼした場合である）。
// 成功条件: 通知に「走行中のサブエージェントを止めました」が出ないこと。
func TestSubagent_background_tasksが空のStopで走行中の印が下りる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(193, "Ready"))

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// 1本目の申告（`SubagentStart` 〜 `SubagentStop`）に印を立てる。
		fx.Orc.OnHook(subagentStartEvent("session-1", "", "a1f9f743842d397e1", "Explore"))
		// 2本目の申告（直近の `Stop` の `background_tasks`）にも印を立てる。
		fx.Orc.OnHook(runningStopEvent("session-1", "", "p1", "b2c0e5551ff248a2", "impl"))
		// **Claude Code 自身が「1つも走っていない」と言った。**両方下りなければならない。
		fx.Orc.OnHook(stopEvent("session-1", "", "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "引き渡しの通知が投稿される", func() bool {
		return handoffCommentBody(fx, "I_node193") != ""
	})
	fx.WaitRunsDrained(t, 15*time.Second)

	assertNoRunningSubagentNotice(t, handoffCommentBody(fx, "I_node193"))
}

// TestSubagent_turnを送るときに走行中の印が下りる は、前の turn の subagent を
// 持ち越さないことを確かめる。
//
// 目的: 設計 3-11 の「空に戻す契機は**turn を送るとき**と `background_tasks` が空の `Stop`」を
// 守っていることを示す。**持ち越すと、次の turn で `blocked` になったときに
// 「走っている」と誤って判定し、猶予いっぱい待たされる。**
// 与える情報: 1回目の turn が終わった**あと**に遅れて届く `SubagentStart` と
// `SubagentStop`（`background_tasks` に別の subagent を載せたもの）。
// **turn の終わりの `Stop` より後に届くので、空の `Stop` では下ろせない。**
// 2回目の turn で `blocked` になる。
// 成功条件: 通知に「走行中のサブエージェントを止めました」が出ないこと。
func TestSubagent_turnを送るときに走行中の印が下りる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(194, "Ready"))

	// **1回目の turn を正常に終わらせるので、記録が要る**（表明を読みにいくため）。
	// **表明は書かない。**書くと1回目で run が終わり、2回目の turn が送られない。
	parent := writeTranscript(t, t.TempDir(), "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "まだ途中です", false),
	})

	var turns int
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		turns++
		if turns == 1 {
			// **先に turn を終わらせる。**この `Stop` が印を下ろす。
			fx.Orc.OnHook(stopEvent("session-1", parent, "p1"))
			// **そのあとに届く。**turn の終わりの判定には使われないので、
			// ここで立った印を下ろせるのは次の `beginTurn` だけである。
			fx.Orc.OnHook(subagentStartEvent("session-1", "", "a1f9f743842d397e1", "Explore"))
			fx.Orc.OnHook(subagentStopWithTasksEvent(
				"session-1", "c3d1a7b20e94f615", "impl", []string{"b2c0e5551ff248a2"}))
			return map[string]any{
				"type":  "agent_prompted",
				"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
			}, nil
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "引き渡しの通知が投稿される", func() bool {
		return handoffCommentBody(fx, "I_node194") != ""
	})
	fx.WaitRunsDrained(t, 20*time.Second)

	if turns < 2 {
		t.Fatalf("2回目の turn が送られていない（turn の回数: %d）", turns)
	}
	assertNoRunningSubagentNotice(t, handoffCommentBody(fx, "I_node194"))
}

// TestSubagent_SubagentStopでbackground_tasks側の印も下りる は、
// **印を立てる側が2本あるのに下ろす側が1本しか無い**という食い違いを見張る。
//
// 目的: 設計 3-11 の「`SubagentStop` を受けたら、2つの申告の**両方**からその
// `agent_id` を外す」を守っていることを示す。
// **`SubagentStop` 自身が `background_tasks` を持ってくる。**実測記録の1件は、
// いま終わったその subagent を `status` が `running` のまま載せていた
// （docs/evidence/hooks_probe_20260817.jsonl）。**`runningSubagents` からしか外さないと、
// `background_tasks` 側に残り続ける。**そちらが空へ戻るのは
// 「turn を送るとき」と「`background_tasks` が空の `Stop`」だけであり、
// **`blocked` で終わる turn にはどちらも来ない。**
// 与える情報: `background_tasks` に自分を載せた `SubagentStop` だけが届いて
// `blocked` になる台本。**空の `Stop` も次の turn も来ない。**
// 成功条件: 通知に「走行中のサブエージェントを止めました」が出ないこと。
func TestSubagent_SubagentStopでbackground_tasks側の印も下りる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(195, "Ready"))

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		fx.Orc.OnHook(subagentStartEvent("session-1", "", "a1f9f743842d397e1", "Explore"))
		// **これで終わっている。**`background_tasks` に自分が `running` のまま載っていても、
		// 走行中と数えてはならない。
		fx.Orc.OnHook(subagentStopWithTasksEvent(
			"session-1", "a1f9f743842d397e1", "Explore", []string{"a1f9f743842d397e1"}))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "引き渡しの通知が投稿される", func() bool {
		return handoffCommentBody(fx, "I_node195") != ""
	})
	fx.WaitRunsDrained(t, 15*time.Second)

	assertNoRunningSubagentNotice(t, handoffCommentBody(fx, "I_node195"))
}

// TestSubagent_覚える件数の上限を超えたら走っていない側へ倒れる は、
// 倒す向きを確かめる。
//
// 目的: 設計 3-11 の「覚える件数に上限を置く。**上限に達したら、そこから先は覚えない。**
// 覚えなかったぶんは『走っていない』側に倒れる」を守っていることを示す。
// **倒す向きはこちらでなければならない。**逆にすると、作り話の `SubagentStart` を
// 送るだけで引き渡しを永久に足止めできる。
// 与える情報: 200件の `SubagentStart` と、**先頭128件ぶんだけ**の `SubagentStop`。
// **末尾の72件には `SubagentStop` を送らない。**上限（64件）が効いていれば、
// 末尾はそもそも覚えられていないので、走行中には1件も残らない。
// 成功条件: 通知に「走行中のサブエージェントを止めました」が出ないこと。
// **上限が無ければ、送らなかった72件が走行中として残り、猶予いっぱい待たされる。**
func TestSubagent_覚える件数の上限を超えたら走っていない側へ倒れる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(196, "Ready"))

	// **上限そのものは非公開の定数である**（`maxTrackedSubagents` = 64）。
	// テストから値は読めないので、**上限より確実に多い件数を送り、
	// 上限より確実に多い件数を下ろす。**こうすれば、定数を後から少し変えても
	// このテストは意味を保つ（上限が stops 以下であるかぎり成り立つ）。
	const starts = 200
	const stops = 128

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		for i := 0; i < starts; i++ {
			fx.Orc.OnHook(subagentStartEvent("session-1", "", fmt.Sprintf("agent%03d", i), "Explore"))
		}
		// **覚えたものは必ずこの中に含まれる**（先に来たものから覚えるため）。
		// **上限を超えて捨てられたぶんには、対応する `SubagentStop` が無い。**
		// 捨てられていれば、走行中には数えられない。
		for i := 0; i < stops; i++ {
			fx.Orc.OnHook(subagentStopEvent("session-1", fmt.Sprintf("agent%03d", i), "Explore"))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "引き渡しの通知が投稿される", func() bool {
		return handoffCommentBody(fx, "I_node196") != ""
	})
	fx.WaitRunsDrained(t, 15*time.Second)

	assertNoRunningSubagentNotice(t, handoffCommentBody(fx, "I_node196"))
}

// TestSubagent_通知に並べる名前は記録のパスと同じ件数で切る は、
// 通知が名前で埋まらないことを確かめる。
//
// 目的: 設計 3-11 の「名前を並べるのは `handoffSubagentLimit` 件までとし、
// **残りは件数だけ書く**」を守っていることを示す。
// **走行中として数えうる件数は、2つの申告を足し合わせるぶん上限の2倍まで増える。**
// 記録のパスは3件までに切っているのに名前だけ全部並べると、
// **引き渡しの通知が名前の羅列になり、【対処】まで読まれない。**
// 与える情報: `SubagentStop` が1件も来ないまま `blocked` になる、10件の `SubagentStart`。
// 成功条件: 「10 件が動いていました」と正しい件数が出たうえで、
// 名前は3件までしか並ばず、残りが「ほか 7 件」とまとめられていること。
func TestSubagent_通知に並べる名前は記録のパスと同じ件数で切る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	// **これは想定して起こしている失敗である。**猶予のあいだ待っても終わらない
	// subagent を、走行中のまま止める場面をわざと作っている。
	fx.AllowLog("猶予のあいだにサブエージェントが終わらなかったので")
	fx.Tracker.AddIssue(sampleIssue(197, "Ready"))

	const running = 10
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		for i := 0; i < running; i++ {
			// **名前順に並ぶ。**`agent0` 〜 `agent9` なので、載るのは先頭3件である。
			fx.Orc.OnHook(subagentStartEvent("session-1", "", fmt.Sprintf("id%d", i), fmt.Sprintf("agent%d", i)))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "引き渡しの通知が投稿される", func() bool {
		return handoffCommentBody(fx, "I_node197") != ""
	})
	fx.WaitRunsDrained(t, 15*time.Second)

	body := handoffCommentBody(fx, "I_node197")
	// **件数そのものは切らない。**人間が worktree を確かめるかどうかを決める材料である。
	for _, want := range []string{"10 件が動いていました", "agent0(id0)", "agent1(id1)", "agent2(id2)", "ほか 7 件"} {
		if !strings.Contains(body, want) {
			t.Errorf("引き渡しの通知に %q が無い:\n%s", want, body)
		}
	}
	// **4件目から先は名前を出さない。**
	for _, unwanted := range []string{"agent3(id3)", "agent9(id9)"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("上限を超えた名前を並べている（%q）:\n%s", unwanted, body)
		}
	}
}
