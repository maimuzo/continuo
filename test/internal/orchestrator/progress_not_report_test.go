// 途中経過の報告を「この run の成果の報告」として数えないことの検査である
// （issue #178（途中経過を1回書いたエージェントが最後の報告を忘れても、continuo が書き直させない））。
//
// **外部へ1回も接続しない。**偽の tracker と偽の herdr だけを使う。
package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// 目的: 途中経過の報告しか書かずに終えた run へ、セッションを復元して書き直させることを固定する
// （issue #178。設計 3-25 の段1）。
//
// **なぜ要るか。**進捗報告のコメントも `<!-- continuo:agent -->` の印を持つ
// （組み込みの 5-3 が両方の印を書かせている）。**数えてしまうと、continuo は
// 「コメントは書かれている」と判定して書き直させない。**
// **issue には「まだ作業中です」だけが残り、何をしたのかが誰にも分からないまま `In Review` に立つ。**
//
// **書き直しの経路は、エージェントの報告が失われるのを防ぐために置かれている。**
// **その防波堤が、途中経過1件で開いていた。**
//
// 与える情報: run が始まった**あと**に付いた、進捗報告の印つきのエージェントのコメント。
// 成功条件: 「コメントが無い」と判定され、セッションの復元（`--resume`）が走ること。
func TestComment_進捗報告は成果の報告として数えない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	// **この run が始まったあとのコメントである。**時刻では落ちない。
	// **落ちる理由は、進捗報告の印が入っていることだけである。**
	fx.Tracker.AddComment("I_node188",
		"<!-- continuo:agent -->\n<!-- continuo:progress -->\nまだ作業中です。\n\n- いま実装しています",
		true, time.Now().Add(1*time.Hour))

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
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
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

// 目的: 進捗報告の印が付いていない成果の報告は、いままでどおり数えることを固定する
// （issue #178）。
//
// **これが無いと、上の検査は「常に復元する」でも通ってしまう。**
//
// 与える情報: run が始まったあとに付いた、進捗報告の印を持たないエージェントのコメント。
// 成功条件: セッションの復元（`--resume`）が走らないこと。
func TestComment_印の無い成果の報告はいままでどおり数える(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.AddComment("I_node188",
		"<!-- continuo:agent -->\nこの run でやったことを書きました",
		true, time.Now().Add(1*time.Hour))

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
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	// **コメントの確認より「あと」に起きることを待つ。**
	//
	// **Status を待ってはならない。**Status を書くのは `applySignals` で、
	// **コメントの確認（`finishRunClaimed` → `ensureAgentComment`）より前である。**
	// Status で待つと、確認が走る前にこの検査が終わりうる。
	// **そうなると「復元が走っていない」は「まだ走っていないだけ」と区別が付かず、
	// この検査は何も守らなくなる。**
	//
	// **`pane.close` でも足りない。**コメントが無いと判定した経路も、復元へ入る前に
	// `stopWorker` を呼ぶ（同じセッション UUID が2つ生きるのを防ぐため）。
	// **数えると、復元が始まる前に条件が満たされる。**
	//
	// **`finishRunClaimed` の最後の `release` まで待つ。**そこまで来ていれば、
	// コメントの確認も、復元の経路も、全部終わっている。
	waitFor(t, 30*time.Second, "run が実行中の一覧から外れる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})

	for _, r := range fx.Herdr.Requests() {
		if r.Method != herdr.MethodAgentStart {
			continue
		}
		args, _ := r.Params["args"].([]any)
		if strings.Contains(joinAny(args), "--resume") {
			t.Fatalf("印の無い成果の報告があるのにセッションを復元しました（issue #178）: %q", joinAny(args))
		}
	}
}

// 目的: 復元のときに送る文面が、禁じる印を「囲み付きのほう」と名指しすることを固定する
// （issue #178）。
//
// **なぜ要るか。**成果の報告を捨てるのは、囲み付きの印（`<!` から始まる形）である。
// **囲みを外した形は捨てない。**それなのに「囲みを外した形を先頭に置かないでください」と書くと、
// **捨てられるほうが1度も禁じられないまま残る。**しかも次の行が
// 「本文の中では囲みを外した形で書いてください」と言うので、**外した形だけが禁止だと読める。**
// **読んだエージェントが囲み付きを先頭に置くと、その報告が飛ばされ、
// 書いたのに `failure_state` へ落ちる。**この経路が防ごうとした結末そのものである。
//
// 与える情報: 進捗報告しか書かずに終えた run（上と同じ）。
// 成功条件: 送った文面に「囲み付きの … を置かないでください」があり、
// 印そのもの（囲み付きの文字列）は1度も埋まっていないこと。
func TestComment_書き直しの文面は囲み付きの印を名指しで禁じる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.AddComment("I_node188",
		"<!-- continuo:agent -->\n<!-- continuo:progress -->\nまだ作業中です。",
		true, time.Now().Add(1*time.Hour))

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
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	// **段7 の送信そのものを待つ。**復元が始まっただけでは、まだ文面は送られていない。
	sent := ""
	waitFor(t, 30*time.Second, "コメントを書かせる文面が送られる", func() bool {
		for _, r := range fx.Herdr.Requests() {
			if r.Method != herdr.MethodAgentPrompt {
				continue
			}
			text, _ := r.Params["text"].(string)
			if strings.Contains(text, "issue のコメントに書いてください") {
				sent = text
				return true
			}
		}
		return false
	})

	if !strings.Contains(sent, "囲み付きの") {
		t.Errorf("禁じる印を「囲み付き」と名指ししていません:\n%s", sent)
	}
	if !strings.Contains(sent, "先頭に、囲み付きの") {
		t.Errorf("先頭に置くなと言っている相手が、囲み付きのほうになっていません:\n%s", sent)
	}
	// **印そのものを埋めない。**埋めると、写しただけでその報告が途中経過として捨てられる。
	bare := "continuo:progress"
	if strings.Contains(sent, "<!-- "+bare+" -->") {
		t.Errorf("進捗報告の印そのものが送る文面に埋まっています:\n%s", sent)
	}
}
