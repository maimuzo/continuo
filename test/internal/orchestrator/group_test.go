package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// TestSignal_グループの別issueもIceBoxのまま動かせる は、グループの表明を確かめる。
//
// 目的: 設計 3-26 の「代表の issue を1件 dispatch し、**表明を受けた時点で初めて、
// 指された issue を1件ずつ照合して Status を動かす**」と、設計 3-25 の
// 「**`active_states` で絞らない。**グループの issue は `Ice Box` にあるので、絞ると1件も
// 反映されない」を守っていることを示す。
//
// 与える情報: 代表の issue（`Ready`）とグループの issue（`Ice Box`）。エージェントは
// `CONTINUO-STATUS: review` と `CONTINUO-STATUS: #45 review` の2行を書く。
// 成功条件: どちらの issue も `In Review` へ動き、`Ice Box` の issue は
// `FetchIssueByIdentifier` で引かれている（巡回の候補には入っていないため）。
func TestSignal_グループの別issueもIceBoxのまま動かせる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.AddIssue(sampleIssue(45, "Ice Box"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1",
			"2件ともまとめて直しました。\n\nCONTINUO-STATUS: review\nCONTINUO-STATUS: #45 review", false),
	})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n2件とも直しました", true, time.Now())
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

	if got := fx.Tracker.StateOf("PVTI_item188"); got != "In Review" {
		t.Fatalf("代表の issue が動いていない: got %q, want %q", got, "In Review")
	}
	if got := fx.Tracker.StateOf("PVTI_item45"); got != "In Review" {
		t.Fatalf("グループの issue（Ice Box）が動いていない: got %q, want %q", got, "In Review")
	}
	if fx.Tracker.CountCall("FetchIssueByIdentifier") == 0 {
		t.Fatalf("識別子で item を引いていない（Ice Box の issue は巡回の候補に入っていない）: %v",
			fx.Tracker.Calls())
	}
}

// TestSignal_ボードに載っていない対象はコメントに残して捨てる は、
// 表明の安全のための制約を確かめる。
//
// 目的: 設計 3-26 の「表明で指せるのは、ボードに載っている issue だけ。**指定されたら
// ログに残して無視する**」と、設計 3-25 の「ボードに載っていなかったら、その行を捨て、
// issue のコメントに『ボードに無いので動かせなかった』と書く」を守っていることを示す。
//
// 与える情報: ボードに無い `#999` を指す表明。
// 成功条件: continuo 自身のコメントが issue に付き、run は普通に終わる。
func TestSignal_ボードに載っていない対象はコメントに残して捨てる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "CONTINUO-STATUS: review\nCONTINUO-STATUS: #999 review", false),
	})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n直しました", true, time.Now())
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

	found := false
	for _, c := range fx.Tracker.CommentsOf("I_node188") {
		if c.IsSelf && strings.Contains(c.Body, "#999") {
			found = true
		}
	}
	if !found {
		t.Fatalf("ボードに無い対象を人間に知らせるコメントが無い: %+v", fx.Tracker.CommentsOf("I_node188"))
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "In Review" {
		t.Fatalf("代表の issue の表明まで捨てている: got %q", got)
	}
}

// TestComment_runが終わるときにコメントが無ければセッションを復元して書かせる は、
// 設計 3-25 の9段を確かめる。
//
// 目的: 「run が終わるときにコメントを確かめ、無ければ 3-25 の9段で書かせる
// （**毎 turn ではない**）」「continuo は代筆しない」を守っていることを示す。
//
// 与える情報: エージェントが `review` を表明するが、issue にコメントを1件も残さない。
// 成功条件:
//   - 先に `pane.close` で worker を止めてから（同じセッション UUID が2つ生きるのを防ぐ）
//   - `worktree.open` → `pane.list` → `agent.start`（`--resume <UUID>`）→ `agent.prompt` の順で復元する
//   - それでも書かれないので Status が `failure_state` へ落ちる
func TestComment_runが終わるときにコメントが無ければセッションを復元して書かせる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "CONTINUO-STATUS: review", false),
	})
	prompts := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		prompts++
		if prompts == 1 {
			// **コメントは書かない。**
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 30*time.Second, "Status が failure_state へ落ちる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})

	methods := fx.Herdr.Methods()
	closeIdx := indexOf(methods, herdr.MethodPaneClose)
	if closeIdx < 0 {
		t.Fatalf("復元の前に worker を止めていない: %v", methods)
	}
	// 復元の agent.start は pane.close より後に来る。
	resumeStart := -1
	for i := closeIdx; i < len(methods); i++ {
		if methods[i] == herdr.MethodAgentStart {
			resumeStart = i
			break
		}
	}
	if resumeStart < 0 {
		t.Fatalf("セッションを復元していない（agent.start が pane.close の後に無い）: %v", methods)
	}

	// **`--resume <UUID>` と `--settings` を毎回渡し直す**（復元されないため。設計 3-25）。
	var resumeArgs string
	for _, r := range fx.Herdr.Requests() {
		if r.Method != herdr.MethodAgentStart {
			continue
		}
		args, _ := r.Params["args"].([]any)
		joined := joinAny(args)
		if strings.Contains(joined, "--resume") {
			resumeArgs = joined
		}
	}
	if resumeArgs == "" {
		t.Fatalf("--resume を付けて起動していない: %v", fx.Herdr.Requests())
	}
	for _, want := range []string{"--resume session-1", "--settings", "--permission-mode dontAsk"} {
		if !strings.Contains(resumeArgs, want) {
			t.Fatalf("復元の起動フラグに %q が無い: %q", want, resumeArgs)
		}
	}
	if strings.Contains(resumeArgs, "--session-id") {
		t.Fatalf("復元なのに --session-id を渡している（既に使った UUID は再利用できない）: %q", resumeArgs)
	}
}

// TestComment_この run が書いたコメントだけを数える は、コメントの数え方を確かめる。
//
// 目的: 設計 3-25 の「**『この run が書いたもの』だけを数える**（marker があり、
// `runState.StartedAt` より新しいもの）。**worktree を再利用すると前の run のコメントが
// 残っている**」を守っていることを示す。
//
// 与える情報: run が始まる**前**に付いたエージェントのコメントだけがある issue。
// 成功条件: 「コメントが無い」と判定され、セッションの復元（`--resume`）が走る。
func TestComment_このrunが書いたコメントだけを数える(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	// 前の run が残したコメント（run の開始よりずっと前）。
	fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n前の run の記録", true,
		time.Now().Add(-24*time.Hour))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "CONTINUO-STATUS: review", false),
	})
	prompts := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		prompts++
		if prompts == 1 {
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 30*time.Second, "セッションの復元が走る", func() bool {
		for _, r := range fx.Herdr.Requests() {
			if r.Method != herdr.MethodAgentStart {
				continue
			}
			args, _ := r.Params["args"].([]any)
			if strings.Contains(joinAny(args), "--resume") {
				return true
			}
		}
		return false
	})
}
