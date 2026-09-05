// **走っている最中に担当が移ったときの検査である**（設計 3-77c）。
//
// **担当を外された機械が push すると、新しい担当の機械が書いた続きと衝突する。**
// だから turn の終わりで止める。
package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/tracker"
)

// TestHandoffRecheck_担当が移っていたらturnの終わりで止める は、設計 3-77c を確かめる。
//
// 目的: 「1時間ごとに、走っている最中も issue の担当者を読み直す。担当が移っていれば、
// その turn の終わりで止まる」。
// 与える情報: 着手して走っている run と、その最中に担当者を別の機械へ書き換えたボード。
// 確かめ直す間隔は 1ms にしてある。
// 成功条件: turn の終わりで run が印から外れること。**Status は動かさないこと**
// （動かすと、新しい担当の機械が着手しようとしているボードを外された機械が書き換える）。
func TestHandoffRecheck_担当が移っていたらturnの終わりで止める(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			// **1回の turn のあいだに確かめ直す時刻が来るようにする。**
			cfg.Tracker.Provider.Handoff.RecheckIntervalMs = 1
		},
	})
	fx.AllowLog("担当が移ったので")
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "続けます。\nCONTINUO-STATUS: working", false),
	})

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// **turn が終わる前に、別の機械が担当を取り上げた状態にする。**
		fx.Tracker.SetAssignees("PVTI_item188", rivalLogin)
		fx.Tracker.AddCommentBy(issueNode(188), rivalLogin, handoff.FormatHold(handoff.Hold{
			Assignee: rivalLogin,
			Branch:   "continuo/octocat/hello-world/188", At: time.Now(),
		}), time.Now())
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 10*time.Second, "担当が移った run が止まる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})

	// **Status は動かさない。**着手のときに書いた `In Progress` のままであるべきである。
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "In Progress" {
		t.Errorf("担当を外された機械がボードを書き換えている: Status が %q（In Progress のままであるべき）", got)
	}
}

// TestHandoffRecheck_担当者が1人もいないだけでは止めない は、止めすぎを防ぐ検査である。
//
// 目的: **「まだ誰も担当していない」と「担当を外された」は見分けられない。**
// 復元した run・この機能より前に着手した run・hold を書けなかった run は、
// どれも担当者が付いていない。**そこで止めると、走っている run が片端から捨てられる。**
// 与える情報: 着手して走っている run と、その最中に担当者を全部消したボード。
// 成功条件: turn の終わりで止まらず、次の turn へ進むこと。
func TestHandoffRecheck_担当者が1人もいないだけでは止めない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.Provider.Handoff.RecheckIntervalMs = 1
		},
	})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "続けます。\nCONTINUO-STATUS: working", false),
	})

	sent := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		sent++
		if sent == 1 {
			// **担当者を全部消す。**入札で外されたのか、そもそも付いていなかったのかは
			// ここからは見分けられない。
			fx.Tracker.SetAssignees("PVTI_item188")
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 10*time.Second, "次の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) >= 2
	})
	if len(fx.Orc.RunningIdentifiers()) != 1 {
		t.Errorf("担当者が消えただけで run を捨てている")
	}
}

// TestHandoffRecheck_復元した_run_は_turn_を送る前に担当を確かめる は、設計 3-77h を確かめる。
//
// 目的: 「作業を再開するとき、issue の担当者を読み直す。担当が自分でなくなっていれば、
// push せずに止まる」。
//
// **turn の終わりで確かめるだけでは遅い。**朝、PC を起動したときに担当が既に移っていると、
// **run を組み立てて turn を1回丸ごと送ってしまう**（`workspace_hooks.after_run` も走る）。
//
// 与える情報: 引き継いだ（＝1度も担当を確かめていない）run と、
// **別の機械が担当者になっている**ボード。確かめ直す間隔は 0（走行中は確かめない設定）。
// 成功条件: turn が1回も送られず、run が印から外れること。
// **`recheck_interval_ms` が 0 でも、再開の確かめは行われること。**
func TestHandoffRecheck_復元したrunはturnを送る前に担当を確かめる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			// **走っている最中は確かめない設定にする。**再開の確かめはそれとは別の場面である。
			cfg.Tracker.Provider.Handoff.RecheckIntervalMs = 0
		},
	})
	fx.AllowLog("担当が移ったので")

	issue := sampleIssue(188, "In Progress")
	issue.Assignees = []tracker.Assignee{{ID: "U_" + rivalLogin, Login: rivalLogin}}
	issue.AssigneeCount = 1
	fx.Tracker.AddIssue(issue)

	node := issueNode(188)
	now := time.Now()
	fx.Tracker.AddCommentBy(node, rivalLogin, handoff.FormatHold(handoff.Hold{
		Assignee: rivalLogin,
		Branch:   "continuo/octocat/hello-world/188", At: now,
	}), now)

	// **引き継いだ run として印を付ける。**`NeedsPrompt` を立てるので、
	// 担当を確かめなければ次の巡回で turn が1回送られる。
	if !fx.Orc.Adopt(issue, orchestrator.AdoptedRun{
		AgentName:        normalize.SafeName("continuo-hello-world-188"),
		PaneID:           "w1:p1",
		SessionUUID:      "session-188",
		HerdrWorkspaceID: "w1",
	}, true) {
		t.Fatal("run を引き継げなかった")
	}

	fx.Orc.Tick(context.Background())

	waitFor(t, 10*time.Second, "担当が移った run が止まる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
	if got := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); got != 0 {
		t.Errorf("担当が移っているのに turn を送った: %d 回", got)
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "In Progress" {
		t.Errorf("担当を外された機械がボードを書き換えている: Status が %q", got)
	}
}

// TestHandoffRecheck_名前を読めない担当者がいるだけでは止めない は、止めすぎを防ぐ検査である
// （設計 3-77c）。
//
// 目的: **担当者の名前は `assignees(first: 10)` で取るので、11人以上が付いている issue では
// 名前が10件までしか返らない。**残りは埋め草（`(取得できなかった担当者)`）で埋まる。
// **自分がその窓の外にいると、名前の突き合わせが1度も当たらず、
// まだ自分が担当している run を「担当が移った」と読んで捨ててしまう。**
// **捨てると、push していない変更が失われる。**
//
// 与える情報: 走っている run と、**担当者が自分1人のまま人数だけ11人になったボード。**
// 成功条件: turn の終わりで止まらず、次の turn へ進むこと。
func TestHandoffRecheck_名前を読めない担当者がいるだけでは止めない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.Provider.Handoff.RecheckIntervalMs = 1
		},
	})
	fx.AllowLog("担当者の名前を全部は読めない")
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "続けます。\nCONTINUO-STATUS: working", false),
	})

	sent := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		sent++
		if sent == 1 {
			// **名前は他人1人ぶんだけ返り、人数は11人。**
			// 自分は窓の外にいるかもしれず、ここからは見分けられない。
			fx.Tracker.SetAssignees("PVTI_item188", rivalLogin)
			fx.Tracker.SetAssigneeCount("PVTI_item188", 11)
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 10*time.Second, "次の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) >= 2
	})
	if len(fx.Orc.RunningIdentifiers()) != 1 {
		t.Errorf("名前を読めない担当者がいるだけで run を捨てている")
	}
}
