// {"RUCM-CFG-SHA256": "5a056949d9f623cf99d633f9af6071abceba14e2cc94d9d9fb5b3d503a4a221a", "SOURCE": "docs/spec/usecases/particular_case/レートリミットで待って再開する.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**「レートリミットで待って再開する」の
// 15本のパスは、6通りの結末の組み合わせである。**終端フローごとに代表を1本ずつ**対応づける。
package orchestrator_test

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

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
// 与える情報: ステータスラインから届いた5時間の期間の使用率が100%（issue #284）。
// hook が来ていない run と、閾値の手前で hook を受けた run。
// 成功条件: 前者だけが枠待ちになり、時計が止まる。後者は枠待ちにならない。
func TestQuota_100パーセントかつhookが来ていないrunだけを枠待ちにする(t *testing.T) {
	fx := newStubFixture(t, stubFixtureOptions{
		AgentStatus: herdr.AgentStatusUnknown,
		Mutate: func(cfg *config.Config) {
			cfg.Claude.TurnTimeoutMs = 50
			cfg.RateLimit.Source = ratelimit.SourceStatusline
		},
	})
	t.Cleanup(fx.Close)
	// **回復待ちと閾値は新しさを問わない**ので、初めて見るセッションの1行で足りる。
	fx.Orc.OnStatusline(slLine("pane-a", 100, slWin(100, time.Now().Add(2*time.Hour)), nil))
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
// TestQuota_pause_above_percentを超えたら新規のdispatchだけを止める は、
// 「新規を止める閾値」と「この run は枠待ちである」を分けていることを確かめる。
//
// 目的: 設計 3-27 の「`pause_above_percent`（既定95%）を超えただけでは、枠待ちとみなさない。
// **走行中の turn は止めないし、時計も止めない**」を守っていることを示す。
//
// 与える情報: ステータスラインから届いた使用率が 96%（100 には達していない。issue #284）。
// `Ready` の issue が1件。
// 成功条件: 新規の dispatch が起きず、既にある run は枠待ちにならない。
func TestQuota_pause_above_percentを超えたら新規のdispatchだけを止める(t *testing.T) {
	fx := newStubFixture(t, stubFixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceStatusline
			cfg.RateLimit.PauseAbovePercent = 95
			cfg.Trust.RequireRepoTrusted = false
		},
	})
	t.Cleanup(fx.Close)
	feedFreshQuota(fx.Orc, "pane-a", time.Now(), 96, 10)
	running := adoptRun(fx, 188)
	fx.Tracker.AddIssue(sampleIssue(190, "Ready"))

	fx.Orc.Tick(context.Background())

	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == "octocat/hello-world#190" {
			t.Fatalf("閾値を超えているのに新規を dispatch している: %+v", v)
		}
		if v.Identifier == running.Identifier && v.WaitingQuota {
			t.Fatalf("95%%を超えただけで走行中の run の時計を止めている: %+v", v)
		}
	}
}

// TestQuota_source_noneなら使用率を読まずに0として入札に参加する は、`rate_limit.source: none` の意味を
// 確かめる（issue #284。計画の「source: none のとき」）。
//
// 目的: `none` は「読めなかった」ではなく、運用者が使用率で判定しないと決めた状態である
// （設計 3-27 の逃げ道）。**statusline取得を開かず、quota.json を読まず、届いた行も保管しない。**
//
// 与える情報: `rate_limit.source: none`。実行時ディレクトリに 100% を書いた quota.json。
// 起動時の準備（PrepareStatusline）と巡回3回。ステータスラインの行を1行。
// 成功条件: 保管値が空のまま（quota.json を読まず、枠待ちの判定に使う写しが nil）で、
// statusline取得を1度も開かないこと。
func TestQuota_source_noneなら使用率を読まずに0として入札に参加する(t *testing.T) {
	root := t.TempDir()
	quota := `{"session":{"percent":100,"resets_at":"` + time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339) + `"}}`
	if err := os.WriteFile(filepath.Join(root, "quota.json"), []byte(quota), 0o600); err != nil {
		t.Fatalf("quota.json を書けません: %v", err)
	}
	fx := newStubFixture(t, stubFixtureOptions{Root: root, Logs: true})
	t.Cleanup(fx.Close)

	fx.Orc.PrepareStatusline(context.Background())
	adoptRun(fx, 188)
	for i := 0; i < 3; i++ {
		fx.Orc.Tick(context.Background())
	}

	if snap := fx.Orc.QuotaSnapshotForTest(); snap != nil {
		t.Errorf("source が none なのに quota.json を読んだ: %+v", snap)
	}
	if fx.Orc.StatuslineFetchRunningForTest() || fx.countLog("statusline取得") != 0 {
		t.Errorf("source が none なのに statusline取得を開いた:\n%s", fx.Logs.String())
	}
	if got := len(fx.Herdr.SLStarts()); got != 0 {
		t.Errorf("source が none なのに statusline取得の Claude Code を %d 回起動した", got)
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
// 与える情報: 1回目の巡回では使用率に余裕があり dispatch される（ステータスラインから 50% が
// 届いている。issue #284）。turn を投げたあとに、数秒後に明ける5時間の期間が 100% で届き、
// `agent.prompt` が `timeout` で返る。`agent.get` は起動の確認のあと `working` を返す。
// 成功条件: 期間が明けたあとも `agent.prompt` が1回だけで、枠明けの継続の指示が送られない。
// そのあと `Stop` を流せば turn が終わる。
func TestQuota_枠明けにClaudeCodeが自分で継続していたら継続の指示を送らない(t *testing.T) {
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

// {"RUCM-PATH": "P005"}
//
// TestQuota_枠を使い切っていなければ待ち直さない は、枠待ちの条件その1 を確かめる。
//
// **枠待ちの判定は2つの条件をどちらも満たすときだけ立つ**（設計 3-27）。
// **1つ目は「使い切っている枠が1つでもあること」。**
// 使い切っていないのに待ち直すと、動いていない run を永久に抱える。
//
// 目的: 枠に余裕があるとき、枠待ちにしないこと。
// 与える情報: ステータスラインから届いた使用率 50%（issue #284）と、hook を1件も受けていない run。
// 成功条件: 枠待ちの印が立たないこと。
func TestQuota_枠を使い切っていなければ待ち直さない(t *testing.T) {
	fx := newStubFixture(t, stubFixtureOptions{
		AgentStatus: herdr.AgentStatusUnknown,
		Mutate: func(cfg *config.Config) {
			cfg.Claude.TurnTimeoutMs = 50
			cfg.RateLimit.Source = ratelimit.SourceStatusline
		},
	})
	t.Cleanup(fx.Close)
	feedFreshQuota(fx.Orc, "pane-a", time.Now(), 50, 10)
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

// TestQuota_resets_atの無い期間は保管しない は、リセット時刻の扱いを確かめる（issue #284）。
//
// **いつ明けるか分からないものを「待つ」と決めると、永久に待つ run ができる**（設計 3-27）。
// ステータスラインの `resets_at` が欠けた期間は、`resets_at` が 0（1970年）として届く。
// **今より前の `resets_at` の期間は取り込まない**（計画の「値の形」）ので、保管値にも入らない。
//
// 目的: `resets_at` の無い 100% の期間で、枠待ちにしないこと。
// 与える情報: `used_percentage: 100` かつ `resets_at` の無い5時間の期間と、hook を送らない run。
// 成功条件: 落ちずに巡回が回りきり、保管値が空で、run が枠待ちにならないこと。
func TestQuota_resets_atの無い期間は保管しない(t *testing.T) {
	fx := newStubFixture(t, stubFixtureOptions{
		AgentStatus: herdr.AgentStatusUnknown,
		Mutate: func(cfg *config.Config) {
			cfg.Claude.TurnTimeoutMs = 50
			cfg.RateLimit.Source = ratelimit.SourceStatusline
		},
	})
	t.Cleanup(fx.Close)
	fx.Orc.OnStatusline(slLine("pane-a", 100, slWin(100, time.Unix(0, 0)), nil))
	fx.Orc.OnStatusline(slLine("pane-a", 200, slWin(100, time.Unix(0, 0)), nil))
	adoptRun(fx, 188)

	time.Sleep(120 * time.Millisecond)
	// **落ちないことを確かめる。**時刻を決められないまま進むと、ここで panic するか固まる。
	fx.Orc.Tick(context.Background())

	if snap := fx.Orc.QuotaSnapshotForTest(); snap != nil {
		t.Errorf("resets_at の無い期間を保管した: %+v", snap)
	}
	if v, ok := viewOf(fx, "octocat/hello-world#188"); !ok || v.WaitingQuota {
		t.Errorf("resets_at の無い期間で枠待ちにした: ok=%v %+v", ok, v)
	}
}
