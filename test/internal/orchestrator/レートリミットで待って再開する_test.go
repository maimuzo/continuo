// {"RUCM-CFG-SHA256": "b15e418544ffa731b6667f97b7d3b677a02302b5e6586fbca90ff227331093ed", "SOURCE": "docs/spec/usecases/particular_case/レートリミットで待って再開する.cfg.json"}
//
// **ユースケース記述「レートリミットで待って再開する」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package orchestrator_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// {"RUCM-PATH": "P003"}
//
// Test_レートリミットで待って再開する_P003_枠明けにClaudeCodeが自分で継続していたら継続の指示を送らない は、
// 二重投入の防止を確かめる。
//
// 目的: 設計 3-27 の「**Claude Code 2.1.234 は『枠のリセット時にセッションを自動継続する』
// 機能を既定で持つ。**continuo がそこへ継続の指示を送ると二重投入になる。送る前に
// `agent_status` を見る（`working` なら送らない。hook を待つ）」を守っていることを示す。
//
// 与える情報: 1回目の巡回では使用率に余裕があり dispatch される（ステータスラインから 50% が
// 届いている。issue #284）。turn を投げたあとに、数秒後に明ける5時間の期間が 100% で届き、
// `agent.prompt` が `timeout` で返る。`agent.get` は起動の確認のあと `working` を返す。
// 成功条件: 期間が明けたあとも `agent.prompt` が1回だけで、枠明けの継続の指示が送られない。
// そのあと `Stop` を流せば turn が終わる。
func Test_レートリミットで待って再開する_P003_枠明けにClaudeCodeが自分で継続していたら継続の指示を送らない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceStatusline
			cfg.Claude.PollWaitMs = 100
		},
	})
	// **1回目の巡回では使用率に余裕がある**（値が新しいので statusline取得も開かない）。
	feedFreshQuota(fx.Orc, "pane-a", time.Now(), 50, 10)
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

	// **turn の途中で使用率が 100% になる。**その期間は数秒後に明ける。
	// そのあと agent.prompt を timeout で返す。
	resetsAt := time.Now().Add(3 * time.Second)
	fx.Orc.OnStatusline(slLine("pane-a", 300, slWin(100, resetsAt), nil))
	close(released)

	waitFor(t, 10*time.Second, "枠待ちの待ち直しへ入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentWait) > 0
	})
	// **期間が明けるまで待つ。**明けたあとに継続の指示を送るかどうかを見る。
	time.Sleep(time.Until(resetsAt) + time.Second)

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
// Test_レートリミットで待って再開する_P008_枠待ちの待ち直しがherdrへ届かなくてもrunを捨てない は、
// 一時的な失敗の判定が**枠待ちの待ち直しの経路でも**使われていることを確かめる。
//
// 目的: 枠を使い切ると、continuo は `agent.prompt` を再送せずに `agent.wait` で待ち直す
// （設計 3-27）。**その待ち直しの最中に herdr が再起動すると、run を捨ててはならない。**
// 捨てると、枠が明けるのを待っていただけの issue が failure_state へ落ちる。
//
// 与える情報: 着手のときは使用率が空いていて（入札の余裕値が残っている）、
// turn を送った瞬間にステータスラインから 100% の行が届く（issue #284）。
// `agent.prompt` は herdr の `timeout` を返し、`agent.wait` は応答を書かずに接続を切る。
// リトライは 0 回。
// 成功条件: Status が `In Progress` のままで、issue にコメントが1件も残らず、
// **枠待ちの印も残ったままであること**（外すと stall の時計が動き出し、枠が明けるより
// 先に stall として諦めることになる）。
func Test_レートリミットで待って再開する_P008_枠待ちの待ち直しがherdrへ届かなくてもrunを捨てない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Agent.MaxRetries = 0
			cfg.Tracker.VerifyStatesEvery = 0
			cfg.RateLimit.Source = ratelimit.SourceStatusline
		},
	})
	// **着手が済むまでは使用率を空けておく。**100% のままだと入札の余裕値で
	// dispatch が止まり、turn の経路に1度も入れない。値は新しいので statusline取得も開かない。
	feedFreshQuota(fx.Orc, "pane-a", time.Now(), 0, 0)
	// **turn を送った瞬間に使い切る**（ステータスラインから 100% の新しい応答の行が届く）。
	// herdr の待ち受けは期限までに落ち着かなかった（＝枠待ちの入口。設計 3-27）。
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(map[string]any) (any, *rpcErr) {
		fx.Orc.OnStatusline(slLine("pane-a", 300, slWin(100, time.Now().Add(2*time.Hour)), nil))
		return nil, &rpcErr{Code: herdr.ErrCodeTimeout, Message: "待ち受けが期限までに落ち着きませんでした"}
	})
	// **待ち直しの最中に herdr が再起動した。**
	fx.Herdr.DropConnection(herdr.MethodAgentWait)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.AllowLog("枠待ちの待ち直しが herdr へ届かないので", "herdr との通信が一時的に失敗した")

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "turn の送信が herdr へ届く", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// **使用率は読みに行かない**（issue #284）。届いた 100% の保管値で枠待ちに入る。
	waitFor(t, 20*time.Second, "枠待ちの待ち直しが herdr へ届く", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentWait) > 0
	})
	// 捨てる実装なら、ここで打ち切りまで走り切る。走り切らせてから見る。
	time.Sleep(2 * time.Second)

	if got := fx.Tracker.StateOf("PVTI_item188"); got != "In Progress" {
		t.Errorf("待ち直しが届かなかっただけで Status を落とした: got %q, want In Progress", got)
	}
	if got := fx.Tracker.HandoffCommentsOf("I_node188"); len(got) != 0 {
		t.Errorf("run を捨てて issue へ引き渡しを書いた: %d 件\n%s", len(got), got[0].Body)
	}
	v, ok := viewOfFixture(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatalf("走行中の run を手放した")
	}
	if !v.WaitingQuota {
		t.Errorf("枠待ちの印を外した（stall の時計が動き出し、枠が明ける前に諦めることになる）: %+v", v)
	}
}
