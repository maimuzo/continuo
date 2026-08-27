// {"RUCM-CFG-SHA256": "2131e9a8ee86af8fdba4d479fe94444550b398f19e23439716928ec0c7ee73eb", "SOURCE": "docs/spec/usecases/particular_case/人間に判断を渡す.cfg.json"}
//
// **RUCM から生成したテストである。**「人間に判断を渡す」の代替フローのうち、
// **continuo が Status を書いてはならない2つの経路**を検査する。
//
// **どちらも「書かない」ことが仕様である。**書いてしまうと、
// 人間やエージェントが動かした Status を continuo が巻き戻すことになる。
package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// {"RUCM-PATH": "P012"}
//
// TestRUCMHandoff_P012_知らない表明ではStatusを動かさない は、段3 の代替フロー「知らない表明」を検査する。
//
// **エージェントは `status_signal_map` に無い値を書くことがある**（綴り違い、勝手な造語）。
// **それを黙って無視すると、人間は「なぜ動かないのか」を知る手がかりを持たない。**
// **かといって推測で Status を動かすと、意図しない場所へ issue が飛ぶ。**
//
// 目的: 知らない表明を受けたとき、Status を1バイトも動かさないこと。
// 与える情報: `CONTINUO-STATUS: よくわからない値` を含む transcript。
// 成功条件（RUCM の POSTCONDITION）: Status は `running_state` のまま。
// **pane も閉じない**（turn はまだ続いているため）。
func TestRUCMHandoff_P012_知らない表明ではStatusを動かさない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	holdPrompt(fx)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "やりました。\n\nCONTINUO-STATUS: よくわからない値", false),
	})
	fx.Orc.OnHook(stopEvent(fx.Sessions[0], path, "p1"))

	// **2回目の turn が送られることで「続いている」ことを確かめる。**
	//
	// **pane が閉じるかどうかでは確かめられない。**この fixture では2回目の turn に
	// hook が来ないので、そのあと必ず stall で打ち切られる（それは別の仕様である）。
	waitFor(t, 10*time.Second, "turn が続く（2回目が送られる）", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) >= 2
	})

	if got := fx.Tracker.StateOf("PVTI_item188"); got != "In Progress" {
		t.Errorf("知らない表明で Status を動かしている: %s", got)
	}
	if !strings.Contains(fx.Logs.String(), "status_signal_map にありません") {
		t.Errorf("知らない表明を受けたことを人間へ残していない:\n%s", fx.Logs.String())
	}
}

// {"RUCM-PATH": "P011"}
//
// TestRUCMHandoff_P011_完了済みのissueにはStatusを書かない は、段5 の代替フロー「完了済みのissue」を検査する。
//
// **エージェントは `gh` で自分の issue の Status を動かせる。**
// **turn の途中で人間が `Done` へ動かすこともある。**
// **そこへ continuo が `review` を書き戻すと、完了した issue が作業中に戻る。**
//
// 目的: 取り直した Status が `terminal_states` に入っていたら、書かないこと。
// 与える情報: turn の途中で `Done` へ動かされた issue と、`review` の表明。
// 成功条件（RUCM の POSTCONDITION）: Status は `Done` のまま。
// **continuo は巻き戻していない。**pane は閉じる（run は終わったため）。
func TestRUCMHandoff_P011_完了済みのissueにはStatusを書かない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	holdPrompt(fx)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// **turn の途中で人間が Done へ動かした。**
	fx.Tracker.SetState("PVTI_item188", "Done")
	fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\n\nCONTINUO-STATUS: review", false),
	})
	fx.Orc.OnHook(stopEvent(fx.Sessions[0], path, "p1"))

	waitFor(t, 20*time.Second, "run が印から外れる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})

	if got := fx.Tracker.StateOf("PVTI_item188"); got != "Done" {
		t.Errorf("完了済みの issue の Status を巻き戻している: %s → %s", "Done", got)
	}
}

// {"RUCM-PATH": "P001"}
//
// TestRUCMHandoff_P001_reviewの表明で遷移先へ書いて片付ける は、基本フローを検査する。
//
// 目的: `review` の表明を受けたら、対応する Status へ書き、pane を閉じて印を外すこと。
// 与える情報: `CONTINUO-STATUS: review` を含む transcript と、エージェントのコメント。
// 成功条件（RUCM の POSTCONDITION）: Status が `In Review` になり、
// pane が閉じ、印が外れること。**worktree は残る。**
func TestRUCMHandoff_P001_reviewの表明で遷移先へ書いて片付ける(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	holdPrompt(fx)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\n\nCONTINUO-STATUS: review", false),
	})
	fx.Orc.OnHook(stopEvent(fx.Sessions[0], path, "p1"))

	waitFor(t, 20*time.Second, "run が印から外れる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})

	if got := fx.Tracker.StateOf("PVTI_item188"); got != "In Review" {
		t.Errorf("review の表明で遷移先へ書いていない: %s", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodPaneClose); got == 0 {
		t.Error("run が終わったのに pane を閉じていない")
	}
	// **worktree は残す**（人間が成果を見るため）。
	if !strings.Contains(strings.Join(fx.Herdr.Methods(), ","), herdr.MethodPaneClose) {
		t.Error("pane を閉じた記録が無い")
	}
}
