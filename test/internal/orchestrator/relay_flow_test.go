// 人間のコメントを最初のメッセージに付けて渡す機能（relay）と、Claude Code を閉じた記録の、
// 流れの検査である（設計 3-85。issue #246）。
//
// **外部へ1回も接続しない。**偽の tracker と偽の herdr だけを使う。
package orchestrator_test

import (
	"context"
	"strings"
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
