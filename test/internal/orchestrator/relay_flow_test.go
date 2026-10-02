// 人間のコメントを最初のメッセージに付けて渡す機能（relay）と、Claude Code を閉じた記録の、
// 流れの検査である（設計 3-85。issue #246）。
//
// **外部へ1回も接続しない。**偽の tracker と偽の herdr だけを使う。
package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// relaySectionHeading は、最初のメッセージに付ける節の見出しである。
const relaySectionHeading = "# 権限確認済みの人間からのメッセージ"

// relayHumanGrant は、検査で人間が書く許可の本文である。
const relayHumanGrant = "issue を1件作ってよい"

// seedRelayComments は、閉じた記録と、そのあとに OWNER が書いた許可を issue に置く。
//
// fx: fixture。
// nodeID: issue のノード ID。
func seedRelayComments(fx *fixture, nodeID string) {
	now := time.Now()
	fx.Tracker.AddCommentAs(nodeID, "<!-- continuo:closed -->\nClaude Code を閉じました。", "OWNER",
		"https://github.com/octocat/hello-world/issues/1#issuecomment-101", now.Add(-2*time.Hour))
	fx.Tracker.AddCommentAs(nodeID, relayHumanGrant, "OWNER",
		"https://github.com/octocat/hello-world/issues/1#issuecomment-102", now.Add(-1*time.Hour))
}

// 目的: 最初のメッセージの末尾に、閉じた記録より後に人間が書いたコメントを付けることを固定する。
//
// **付けないと、人間が issue のコメントで出した許可が、auto の判定役に届かない**（判定役は `gh` で読んだ
// コメントを道具の結果として取り除く）。
//
// 与える情報: 閉じた記録と、そのあとに OWNER が書いた許可を持つ `Ready` の issue。既定の設定（relay は有効）。
// 成功条件: 最初に送った本文に節の見出しと許可の本文が入り、組み込みの本文（1回目の本文）も残っていること。
func TestRelay_最初のメッセージに境目より後の人間のコメントを付ける(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prompts := recordPrompts(fx)
	fx.Tracker.AddIssue(sampleIssue(301, "Ready"))
	seedRelayComments(fx, "I_node301")

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "最初のメッセージが送られる", func() bool { return len(prompts()) > 0 })

	got := prompts()[0]
	for _, want := range []string{firstPromptMarker, relaySectionHeading, relayHumanGrant, "#issuecomment-102"} {
		if !strings.Contains(got, want) {
			t.Errorf("最初のメッセージに %q が無い:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Claude Code を閉じました") {
		t.Errorf("閉じた記録そのものを渡している:\n%s", got)
	}
}

// 目的: relay が無効なら、コメントを読まず、節も付けないことを固定する。
//
// 与える情報: 上と同じ issue。`agent.relay_trusted_comments: false` と、`permission_mode: dontAsk` の2通り。
// 成功条件: どちらも最初のメッセージに節が無く、relay 専用の読み取りが1回も呼ばれないこと。
func TestRelay_relayが無効なら読まずに付けない(t *testing.T) {
	for name, mutate := range map[string]func(cfg *config.Config){
		"設定が false": func(cfg *config.Config) { cfg.Agent.RelayTrustedComments = false },
		"dontAsk":   func(cfg *config.Config) { cfg.Claude.PermissionMode = config.ClaudePermissionModeDontAsk },
	} {
		t.Run(name, func(t *testing.T) {
			fx := newFixture(t, fixtureOptions{Mutate: mutate})
			prompts := recordPrompts(fx)
			fx.Tracker.AddIssue(sampleIssue(302, "Ready"))
			seedRelayComments(fx, "I_node302")

			fx.Orc.Tick(context.Background())
			waitFor(t, 10*time.Second, "最初のメッセージが送られる", func() bool { return len(prompts()) > 0 })

			if strings.Contains(prompts()[0], relaySectionHeading) {
				t.Errorf("relay が無効なのに節を付けている:\n%s", prompts()[0])
			}
			if n := fx.Tracker.RelayCalls(); n != 0 {
				t.Errorf("relay が無効なのにコメントを読んでいる: %d 回", n)
			}
		})
	}
}

// 目的: コメントを読めないときと、期限を過ぎたときは、節を付けずに最初のメッセージだけを送ることを固定する。
//
// **読めないことで着手を止めない。**エラーで返すと、テンプレートの誤りと同じく `failRun` へ落ち、
// WORKFLOW.md を直すよう人間へ知らせてしまう。
//
// 与える情報: 上と同じ issue。relay 専用の読み取りが失敗する / 期限まで返らない（期限は 200 ミリ秒に縮める）。
// 成功条件: どちらも最初のメッセージが送られ、節が無く、1回目の本文は残っていること。
func TestRelay_読めないときと期限切れのときは付けずに送る(t *testing.T) {
	for _, name := range []string{"読めない", "期限切れ"} {
		t.Run(name, func(t *testing.T) {
			fx := newFixture(t, fixtureOptions{})
			fx.AllowLog("人間のコメントを付けずに最初のメッセージを送ります")
			prompts := recordPrompts(fx)
			fx.Tracker.AddIssue(sampleIssue(303, "Ready"))
			seedRelayComments(fx, "I_node303")
			if name == "読めない" {
				fx.Tracker.SetRelayError(errors.New("GraphQL が落ちた"))
			} else {
				release := fx.Tracker.HoldRelay()
				t.Cleanup(release)
				fx.Orc.SetRelayTimeoutForTest(200 * time.Millisecond)
			}

			fx.Orc.Tick(context.Background())
			waitFor(t, 10*time.Second, "最初のメッセージが送られる", func() bool { return len(prompts()) > 0 })

			got := prompts()[0]
			if strings.Contains(got, relaySectionHeading) {
				t.Errorf("読めなかったのに節を付けている:\n%s", got)
			}
			if !strings.Contains(got, firstPromptMarker) {
				t.Errorf("1回目の本文が送られていない:\n%s", got)
			}
		})
	}
}

// 目的: コメントを読んでいる間に人間が direct chat へ引き取ったら、最初のメッセージを送らないことを固定する
// （送る前の確認のやり直し）。
//
// **読み取りは最大 60 秒かかりうる。**その間に人間がカードを動かすと、確かめ直さなければ
// 人間が話そうとしている pane へ1回目の本文が届く。
//
// 与える情報: relay 専用の読み取りを止めた状態で着手し、読み取りの最中にカードを direct chat へ動かす。
// 成功条件: 読み取りを放したあとも、`agent.prompt` が1回も呼ばれないこと。
func TestRelay_読んでいる間に人間が引き取ったら送らない(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	prompts := recordPrompts(fx)
	issue := sampleIssue(304, "Ready")
	fx.Tracker.AddIssue(issue)
	seedRelayComments(fx, "I_node304")
	release := fx.Tracker.HoldRelay()
	t.Cleanup(release)

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "relay の読み取りに入る", func() bool { return fx.Tracker.RelayCalls() > 0 })

	fx.Tracker.SetState(issue.ID, humanState)
	fx.Orc.Tick(context.Background())
	release()
	time.Sleep(500 * time.Millisecond)

	if n := len(prompts()); n != 0 {
		t.Fatalf("人間が引き取ったのに最初のメッセージを送った（%d 件）:\n%s", n, prompts()[0])
	}
}

// 目的: コメントを読んでいる間に、run を終わらせる処理が始まったら、最初のメッセージを送らないことを固定する
// （送る前の確認のやり直しのうち、`isTerminating`）。
//
// **終わらせる処理は、pane を閉じる前に `after_run` を走らせる。**その間は待ちのコンテキストが切れていないので、
// 確かめ直さないと、閉じようとしている pane へ1回目の本文が届き、`after_run` と並んで turn が走る。
// **上の direct chat の検査は、待ちのコンテキストが切れるので確かめ直しが無くても通る。**こちらはそれでは通らない。
//
// 与える情報: relay 専用の読み取りを止めた状態で着手し、読み取りの最中にカードを `Blocked` へ動かす
// （`workspace_hooks.after_run` は1秒かかる）。
// 成功条件: 読み取りを放したあとも、`agent.prompt` が1回も呼ばれないこと。
func TestRelay_読んでいる間にrunを終わらせ始めたら送らない(t *testing.T) {
	slow := "sleep 1"
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) { cfg.WorkspaceHooks.AfterRun = &slow }})
	prompts := recordPrompts(fx)
	issue := sampleIssue(312, "Ready")
	fx.Tracker.AddIssue(issue)
	seedRelayComments(fx, "I_node312")
	release := fx.Tracker.HoldRelay()
	t.Cleanup(release)

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "relay の読み取りに入る", func() bool { return fx.Tracker.RelayCalls() > 0 })

	fx.Tracker.SetState(issue.ID, "Blocked")
	fx.Orc.Tick(context.Background())
	release()
	waitFor(t, 15*time.Second, "run を終えて印を外す", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})

	if n := len(prompts()); n != 0 {
		t.Fatalf("終わらせている run へ最初のメッセージを送った（%d 件）:\n%s", n, prompts()[0])
	}
}

// 目的: Claude Code を起動した pane を閉じたら、閉じた記録を1件書くことを固定する。relay が無効なら書かない。
//
// **記録が境目になる。**書かないと、次の起動で渡すコメントの境目が前の run に残り、
// その run の途中で Claude Code が書いたものまで人間のコメントとして渡りうる。
// **self_marker は付けない。**1行目が印そのものでないと、別の機械が境目として読めない。
//
// 与える情報: turn が終わらずに打ち切られる run（リトライが残るので、pane を閉じてバックオフへ入る）。
// 既定の設定と、`agent.relay_trusted_comments: false`。
// 成功条件: 既定では閉じた記録がちょうど1件あり、1行目が `<!-- continuo:closed -->` で、self_marker で始まらないこと。
// 無効では1件も無いこと。
func TestClosedRecord_Claude_Codeを閉じたら記録を1件書く(t *testing.T) {
	for _, relay := range []bool{true, false} {
		name := "relay有効"
		if !relay {
			name = "relay無効"
		}
		t.Run(name, func(t *testing.T) {
			fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
				cfg.Agent.RelayTrustedComments = relay
			}})
			fx.AllowLog("turn が終わったことを検知できません")
			recordPrompts(fx)
			fx.Tracker.AddIssue(sampleIssue(305, "Ready"))

			fx.Orc.Tick(context.Background())
			waitFor(t, 15*time.Second, "pane が閉じられる", func() bool {
				return fx.Herdr.CountMethod(herdr.MethodPaneClose) > 0
			})
			time.Sleep(300 * time.Millisecond)

			records := fx.Tracker.ClosedRecordsOf("I_node305")
			if !relay {
				if len(records) != 0 {
					t.Fatalf("relay が無効なのに閉じた記録を書いた: %d 件", len(records))
				}
				return
			}
			if len(records) != 1 {
				t.Fatalf("閉じた記録が1件ではない: %d 件", len(records))
			}
			body := strings.TrimLeft(records[0].Body, "\n")
			if !strings.HasPrefix(body, config.ClosedMarker+"\n") {
				t.Errorf("1行目が閉じた記録の印ではない: %q", records[0].Body)
			}
			if !strings.Contains(body, "Claude Code を閉じました") {
				t.Errorf("2行目の文が無い: %q", records[0].Body)
			}
		})
	}
}

// 目的: `agent.start` に失敗した pane（Claude Code が起動していない）を閉じたときは、記録を書かないことを固定する。
//
// **書くと、人間が前の記録のあとに書いた許可が、まだ一度も渡らないまま境目より前へ押し出される。**
//
// 与える情報: `agent.start` が必ず失敗する着手。
// 成功条件: 着手が失敗して pane を閉じたあとも、閉じた記録が1件も無いこと。
func TestClosedRecord_agent_startに失敗したpaneでは書かない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.AllowLog("着手に失敗しました", "agent を起動できません", "agent_start_failed")
	fx.Herdr.Handle(herdr.MethodAgentStart, func(map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "agent_start_failed", Message: "起動できませんでした"}
	})
	fx.Tracker.AddIssue(sampleIssue(306, "Ready"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "pane が閉じられる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodPaneClose) > 0
	})
	fx.WaitRunsDrained(t, 15*time.Second)

	if n := len(fx.Tracker.ClosedRecordsOf("I_node306")); n != 0 {
		t.Fatalf("Claude Code が起動していない pane を閉じたのに記録を書いた: %d 件", n)
	}
}

// 目的: pane を閉じ損ねたら、閉じた記録を書かないことを固定する。
//
// **Claude Code が生きたまま記録を付けると、その Claude Code があとで書いたものが人間のコメントとして渡りうる。**
//
// 与える情報: `pane.close` が必ず失敗する herdr と、turn が終わらずに打ち切られる run。
// 成功条件: `pane.close` を試したあとも、閉じた記録が1件も無いこと。
func TestClosedRecord_paneを閉じ損ねたら書かない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.AllowLog("turn が終わったことを検知できません", "pane を閉じられませんでした")
	recordPrompts(fx)
	fx.Herdr.Handle(herdr.MethodPaneClose, func(map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "internal", Message: "閉じられませんでした"}
	})
	fx.Tracker.AddIssue(sampleIssue(307, "Ready"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "pane を閉じようとする", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodPaneClose) > 0
	})
	time.Sleep(300 * time.Millisecond)

	if n := len(fx.Tracker.ClosedRecordsOf("I_node307")); n != 0 {
		t.Fatalf("pane を閉じ損ねたのに記録を書いた: %d 件", n)
	}
}

// 目的: 報告の書かせ直しを通っても、閉じた記録は1件になることを固定する（段2 は書かずに保留する）。
//
// **段2 で書くと、人間がその記録を見て書いた許可が、段8 のあとの2件目の記録より前になり黙って落ちる。**
//
// 与える情報: 1回目の turn で成果のコメントを書かずに review を表明する run。書かせ直しの指示を受けたら
// エージェントのコメントを書く台本。
// 成功条件: 書かせ直し（`--resume`）が走り、run が終わったあとの閉じた記録がちょうど1件で、
// エージェントのコメントより後に書かれていること。
func TestClosedRecord_書かせ直しでも記録は1件(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(308, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "CONTINUO-STATUS: review", false),
	})
	var mu sync.Mutex
	prompts := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		prompts++
		n := prompts
		mu.Unlock()
		if n == 1 {
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		} else {
			// **書かせ直しの指示を受けて、エージェントが成果を書く。**
			fx.Tracker.AddComment("I_node308", "<!-- continuo:agent -->\nこの run でやったこと", true, time.Now().Add(time.Hour))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 30*time.Second, "書かせ直しの指示が送られる", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return prompts >= 2
	})
	fx.WaitRunsDrained(t, 30*time.Second)

	records := fx.Tracker.ClosedRecordsOf("I_node308")
	if len(records) != 1 {
		t.Fatalf("閉じた記録が1件ではない: %d 件", len(records))
	}
	all := fx.Tracker.CommentsOf("I_node308")
	agentAt, recordAt := -1, -1
	for i, c := range all {
		if strings.Contains(c.Body, "この run でやったこと") {
			agentAt = i
		}
		if isClosedRecord(c) {
			recordAt = i
		}
	}
	if agentAt < 0 || recordAt < agentAt {
		t.Errorf("閉じた記録がエージェントのコメントより後にない（エージェント %d / 記録 %d）", agentAt, recordAt)
	}
}

// 目的: 書かせ直しの段2 で閉じたあと、セッションを復元できずに戻っても、閉じた記録を1件書くことを固定する。
//
// **段2 で閉じた Claude Code はもう動いていない。**保留を誰も書かないと、境目が前の run に残る。
//
// 与える情報: リトライを使い切って人間へ渡す run（stall で打ち切る）。書かせ直しの `agent.start --resume` が断られる。
// 成功条件: 引き渡しの通知が残ったあと、閉じた記録がちょうど1件あること。
func TestClosedRecord_段2のあと失敗して戻っても記録を書く(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{
		Now: clock.Now,
		Mutate: func(cfg *config.Config) {
			cfg.Claude.TurnTimeoutMs = 1000
			cfg.Agent.MaxRetries = 0
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	blockFirstPrompt(t, fx)
	fx.Herdr.Handle(herdr.MethodAgentStart, func(params map[string]any) (any, *rpcErr) {
		args, _ := params["args"].([]any)
		if strings.Contains(joinAny(args), "--resume") {
			return nil, &rpcErr{Code: "agent_start_failed", Message: "No conversation found"}
		}
		return map[string]any{
			"type":  "agent_started",
			"agent": map[string]any{"name": params["name"], "agent_status": "idle", "interactive_ready": true, "pane_id": params["pane_id"]},
		}, nil
	})
	fx.Tracker.AddIssue(sampleIssue(309, "Ready"))
	fx.AllowLog("リトライの回数を使い切りました", "セッションを復元できません",
		"turn が終わったことを検知できません", "画面が変わらないまま", "stall")

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	clock.Advance(5 * time.Second)
	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "引き渡しの通知が issue に残る", func() bool {
		return len(fx.Tracker.HandoffCommentsOf("I_node309")) > 0
	})
	waitFor(t, 20*time.Second, "閉じた記録が書かれる", func() bool {
		return len(fx.Tracker.ClosedRecordsOf("I_node309")) > 0
	})
	time.Sleep(500 * time.Millisecond)

	if n := len(fx.Tracker.ClosedRecordsOf("I_node309")); n != 1 {
		t.Fatalf("閉じた記録が1件ではない: %d 件", n)
	}
}

// 目的: self_marker が空なら、閉じた記録を書かず、起動時に WARN を1行出すことを固定する。
//
// **self_marker が空だと、continuo 自身の「Status を動かしました」に目印が付かず、人間のコメントとして渡る。**
//
// 与える情報: `tracker.comments.self_marker` が空の設定と、turn が終わらずに打ち切られる run。
// 成功条件: 起動時の WARN が出て、pane を閉じたあとも閉じた記録が1件も無いこと。
func TestClosedRecord_self_markerが空なら書かず起動時に知らせる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Tracker.Comments.SelfMarker = ""
	}})
	fx.AllowLog("tracker.comments.self_marker が空なので", "turn が終わったことを検知できません")
	recordPrompts(fx)
	fx.Tracker.AddIssue(sampleIssue(311, "Ready"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "pane が閉じられる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodPaneClose) > 0
	})
	time.Sleep(300 * time.Millisecond)

	if !strings.Contains(fx.Logs.String(), "tracker.comments.self_marker が空なので") {
		t.Errorf("起動時の WARN が出ていない")
	}
	if n := len(fx.Tracker.ClosedRecordsOf("I_node311")); n != 0 {
		t.Fatalf("self_marker が空なのに閉じた記録を書いた: %d 件", n)
	}
}
