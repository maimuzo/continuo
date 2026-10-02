// {"RUCM-CFG-SHA256": "19b0a93c99a5252131ded67068886d67eb0f111f61d2ad527f7508dc249d3ee7", "SOURCE": "docs/spec/usecases/particular_case/表明を読んでStatusを動かす.cfg.json"}
//
// **ユースケース記述「表明を読んでStatusを動かす」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package orchestrator_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// {"RUCM-PATH": "P006"}
//
// Test_表明を読んでStatusを動かす_P006_知らない表明ではStatusを動かさない は、代替フロー「知らない表明」を検査する。
//
// **エージェントは `status_signal_map` に無い値を書くことがある**（綴り違い、勝手な造語）。
// **それを黙って無視すると、人間は「なぜ動かないのか」を知る手がかりを持たない。**
// **かといって推測で Status を動かすと、意図しない場所へ issue が飛ぶ。**
//
// 目的: 知らない表明を受けたとき、Status を1バイトも動かさないこと。
// 与える情報: `CONTINUO-STATUS: よくわからない値` を含む transcript。
// 成功条件（RUCM の POSTCONDITION）: Status は `running_state` のまま。
// **pane も閉じない**（turn はまだ続いているため）。
func Test_表明を読んでStatusを動かす_P006_知らない表明ではStatusを動かさない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	// **1回目の `agent.prompt` は、`Stop` を流すまで返させない**（`blockFirstPrompt`）。
	// 返った瞬間から `claude.settle_ms`（この fixture では 50ms）の時計が走り出し、
	// **遅い機械では準備が終わる前に run を諦めてしまう。**
	releasePrompt := blockFirstPrompt(t, fx)

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
	// **`Stop` を積んでから返す。**ここから turn の終わりの判定が始まる。
	releasePrompt()

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

// {"RUCM-PATH": "P004"}
//
// Test_表明を読んでStatusを動かす_P004_完了済みのissueにはStatusを書かない は、代替フロー「書いてはいけないStatus」を検査する。
//
// **エージェントは `gh` で自分の issue の Status を動かせる。**
// **turn の途中で人間が `Done` へ動かすこともある。**
// **そこへ continuo が `review` を書き戻すと、完了した issue が作業中に戻る。**
//
// 目的: 取り直した Status が `terminal_states` に入っていたら、書かないこと。
// 与える情報: turn の途中で `Done` へ動かされた issue と、`review` の表明。
// 成功条件（RUCM の POSTCONDITION）: Status は `Done` のまま。
// **continuo は巻き戻していない。**pane は閉じる（run は終わったため）。
func Test_表明を読んでStatusを動かす_P004_完了済みのissueにはStatusを書かない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	// **1回目の `agent.prompt` は、`Stop` を流すまで返させない**（`blockFirstPrompt`）。
	// 返った瞬間から `claude.settle_ms`（この fixture では 50ms）の時計が走り出し、
	// **遅い機械では準備が終わる前に run を諦めてしまう。**
	releasePrompt := blockFirstPrompt(t, fx)

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
	// **`Stop` を積んでから返す。**ここから turn の終わりの判定が始まる。
	releasePrompt()

	waitFor(t, 20*time.Second, "run が印から外れる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})

	if got := fx.Tracker.StateOf("PVTI_item188"); got != "Done" {
		t.Errorf("完了済みの issue の Status を巻き戻している: %s → %s", "Done", got)
	}
}

// {"RUCM-PATH": "P007"}
//
// Test_表明を読んでStatusを動かす_P007_表明が無かった次のturnで促す は、設計 3-25 の第3層を確かめる。
//
// 目的: 「表明せずに終わったら、次の turn の継続の指示で促す（hook から差し戻す仕組みは
// 採らない）」を守っていることを示す。
// 与える情報: 1回目の turn では表明を書かず、2回目で `review` を書く transcript。
// 成功条件: 2回目のプロンプトに促しの1文が入り、1回目の本文（テンプレート）は送り直さない。
func Test_表明を読んでStatusを動かす_P007_表明が無かった次のturnで促す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	noSignal := writeTranscript(t, transcriptDir, "no-signal.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "作業を進めています。", false),
	})
	withSignal := writeTranscript(t, transcriptDir, "with-signal.jsonl", []any{
		typedUserLine("p2", "続けてください"),
		assistantLine("req2", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	var mu sync.Mutex
	var texts []string
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		text, _ := params["text"].(string)
		texts = append(texts, text)
		n := len(texts)
		mu.Unlock()

		path := noSignal
		if n >= 2 {
			path = withSignal
			fx.Tracker.SetState("PVTI_item188", "Done")
			fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
		}
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "run が終わる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})

	mu.Lock()
	defer mu.Unlock()
	if len(texts) < 2 {
		t.Fatalf("2回目の turn が送られていない: %d 回", len(texts))
	}
	if !strings.Contains(texts[1], "のままです") {
		t.Fatalf("表明を促す1文が2回目のプロンプトに入っていない: %q", texts[1])
	}
	if strings.Contains(texts[1], "gh issue view") {
		t.Fatalf("2回目に1回目の本文を送り直している（設計 5-4 / SPEC.md 7.1 に反する）: %q", texts[1])
	}
}
