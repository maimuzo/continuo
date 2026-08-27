// **continuo が Status を動かしたら、何から何へ動かしたかを issue に残す**（設計 3-29）。
//
// **Status を書くのは continuo であって、エージェントではない。**エージェントは最終応答に
// `CONTINUO-STATUS: review` の1行を書くだけであり、それを受けてボードへ書き込むのは
// continuo の Go のコードである（設計 3-25）。ここで確かめるのは、その書き込みの記録である。
//
// **「何から」は `UpdateStatus` が書く直前に取り直した値でなければならない。**巡回で読んだ
// 時点の値を書くと、この機能そのものが嘘をつく。
package orchestrator_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// TestStatusMove_着手と表明のそれぞれで何から何へを残す は、設計 3-29 を確かめる。
//
// 目的: 1つの run で Status が動くのは「着手したとき」と「終わるとき」の2回である。
// **その2回とも、何から何へ動かしたのかが issue に残る**ことを示す。
//
// 与える情報: `Ready` の issue と、1回目の turn で `CONTINUO-STATUS: review` を書く transcript。
// 成功条件: Status を動かした記録が2件出る。1件目は `Ready → In Progress` で理由が着手、
// 2件目は `In Progress → In Review` で理由が表明である。どちらにも「いつ」と
// 「書いたのは continuo です」が入る。
func TestStatusMove_着手と表明のそれぞれで何から何へを残す(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now})
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, clock.Now())
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 30*time.Second)

	moves := fx.Tracker.StatusMoveCommentsOf("I_node188")
	if len(moves) != 2 {
		t.Fatalf("Status を動かした記録が2件ではない: %d 件（%+v）",
			len(moves), fx.Tracker.CommentsOf("I_node188"))
	}

	start := moves[0].Body
	if !strings.Contains(start, "Status を **Ready → In Progress** へ動かしました。") {
		t.Errorf("着手の記録に「何から」「何へ」の両方が入っていない:\n%s", start)
	}
	if !strings.Contains(start, "着手し") {
		t.Errorf("着手の記録の「なぜ」が着手になっていない:\n%s", start)
	}

	end := moves[1].Body
	if !strings.Contains(end, "Status を **In Progress → In Review** へ動かしました。") {
		t.Errorf("表明の記録に「何から」「何へ」の両方が入っていない:\n%s", end)
	}
	if !strings.Contains(end, "`CONTINUO-STATUS: review` と表明した") {
		t.Errorf("表明の記録の「なぜ」に、エージェントが書いた値が入っていない:\n%s", end)
	}

	// **「いつ」は o.now() で書く。**time.Now を直に呼んでいると、差し替えた時計と揃わない。
	wantAt := "- いつ: " + clock.Now().Format("2006-01-02 15:04 (MST)")
	for _, c := range moves {
		if !strings.Contains(c.Body, wantAt) {
			t.Errorf("「いつ」が差し替えた時計になっていない（want %q）:\n%s", wantAt, c.Body)
		}
		if !strings.Contains(c.Body, "書いたのは continuo です（人間の操作ではありません）") {
			t.Errorf("誰が書いたのかが入っていない:\n%s", c.Body)
		}
	}
}

// TestStatusMove_何からは書く直前に取り直した値である は、設計 3-29 の要を確かめる。
//
// 目的: 呼び出し側が持っている issue の Status は**巡回で読んだ時点の値**であり、
// `UpdateStatus` が書く直前に取り直した値とは限らない。**古い値を「何から」として書くと、
// この機能そのものが嘘をつく。**
//
// 与える情報: 着手で `In Progress` を書いたあと、表明を反映する直前にボードだけを
// `Ready` へ戻す（人間が画面から動かした状況の再現）。**continuo が持っている写しは
// `In Progress` のままである。**
// 成功条件: 表明の記録が `Ready → In Review` になっている（`In Progress → In Review` ではない）。
func TestStatusMove_何からは書く直前に取り直した値である(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
		// **ボードだけを動かす。**continuo が持っている写しは In Progress のままである。
		fx.Tracker.SetState(issue.ID, "Ready")
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 30*time.Second)

	moves := fx.Tracker.StatusMoveCommentsOf("I_node188")
	if len(moves) != 2 {
		t.Fatalf("Status を動かした記録が2件ではない: %d 件（%+v）",
			len(moves), fx.Tracker.CommentsOf("I_node188"))
	}
	end := moves[1].Body
	if !strings.Contains(end, "Status を **Ready → In Review** へ動かしました。") {
		t.Errorf("「何から」が書く直前に取り直した値になっていない（巡回で読んだ古い値を書いている）:\n%s", end)
	}
}

// TestStatusMove_20turn回しても記録は着手と終わりの2件だけ は、記録が増えすぎないことを確かめる。
//
// 目的: **turn ごとに Status が動くわけではない。**作業中の turn でエージェントが出す
// `working` は `status_signal_map` で null に対応づいており、書き込みが1回も起きない。
// **書き込みが起きなければ記録も出ない。**
//
// 与える情報: 19回 `working` を書き、20回目に `review` を書く transcript
// （`max_dispatch_turns` は既定の 20）。
// 成功条件: Status を動かした記録が2件だけである（着手の1件と終わりの1件）。
func TestStatusMove_20turn回しても記録は着手と終わりの2件だけ(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	working := writeTranscript(t, transcriptDir, "working.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "まだ続きがあります。\nCONTINUO-STATUS: working", false),
	})
	done := writeTranscript(t, transcriptDir, "done.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	const lastTurn = 20
	var mu sync.Mutex
	prompts := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		prompts++
		n := prompts
		mu.Unlock()
		path := working
		if n >= lastTurn {
			fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
			path = done
		}
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 60*time.Second)

	if got := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); got < lastTurn {
		t.Fatalf("turn が %d 回まで回っていない: %d 回", lastTurn, got)
	}
	moves := fx.Tracker.StatusMoveCommentsOf("I_node188")
	if len(moves) != 2 {
		t.Fatalf("Status を動かした記録が2件ではない: %d 件（%+v）",
			len(moves), fx.Tracker.CommentsOf("I_node188"))
	}
}

// TestStatusMove_既に同じ値なら記録を残さない は、書き込みが起きない経路を確かめる。
//
// 目的: 取り直した値が既に遷移先と同じなら、`UpdateStatus` は書き込みの mutation を
// 送らない。**ボードは何も動いていないので、記録を書いてはならない。**
//
// 与える情報: 表明を反映する直前に、ボードを遷移先と同じ `In Review` にしておく。
// 成功条件: Status を動かした記録が1件（着手のぶん）だけである。
func TestStatusMove_既に同じ値なら記録を残さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
		// エージェントが自分で gh を叩いて先に In Review へ動かしていた状況。
		fx.Tracker.SetState(issue.ID, "In Review")
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 30*time.Second)

	moves := fx.Tracker.StatusMoveCommentsOf("I_node188")
	if len(moves) != 1 {
		t.Fatalf("書き込みを省いたのに記録を書いている: %d 件（着手の1件だけのはず）\n%+v",
			len(moves), fx.Tracker.CommentsOf("I_node188"))
	}
	if !strings.Contains(moves[0].Body, "Ready → In Progress") {
		t.Errorf("残っているのが着手の記録ではない:\n%s", moves[0].Body)
	}
}

// TestStatusMove_書いてはいけない状態なら記録を残さない は、書き込みを断る経路を確かめる。
//
// 目的: 取り直した結果が `terminal_states` に入っていたら、`UpdateStatus` は書かない。
// **人間やエージェントが先に `Done` へ動かした結果を巻き戻さない**ためである（設計 3-4）。
// 書いていない以上、記録も出してはならない。
//
// 与える情報: 表明を反映する直前に、ボードを `Done` にしておく。
// 成功条件: Status を動かした記録が1件（着手のぶん）だけで、ボードは `Done` のままである。
func TestStatusMove_書いてはいけない状態なら記録を残さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
		fx.Tracker.SetState(issue.ID, "Done")
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 30*time.Second)

	if got := fx.Tracker.StateOf(issue.ID); got != "Done" {
		t.Fatalf("Done を巻き戻してしまった: got %q", got)
	}
	moves := fx.Tracker.StatusMoveCommentsOf("I_node188")
	if len(moves) != 1 {
		t.Fatalf("書かなかったのに記録を書いている: %d 件（着手の1件だけのはず）\n%+v",
			len(moves), fx.Tracker.CommentsOf("I_node188"))
	}
}

// TestStatusMove_failure_stateへ落とす経路では独立した記録を出さない は、
// 引き渡しの通知との重なりを確かめる（設計 3-29）。
//
// 目的: 打ち切りの経路では「`Blocked` へ落とした」ことが引き渡しの通知にも書かれる。
// **独立した記録を別に出すと、同じことを言うコメントが2件並ぶ。**通知の中に1行入れる。
//
// 与える情報: `max_dispatch_turns` が1で、表明を1度も書かない run。
// 成功条件: 引き渡しの通知が1件だけ出て、その本文に `In Progress → Blocked` の1行が入る。
// Status を動かした記録の独立したコメントは、着手のぶん1件だけである。
func TestStatusMove_failure_stateへ落とす経路では独立した記録を出さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Agent.MaxDispatchTurns = 1
	}})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "作業を進めています。", false),
	})
	var mu sync.Mutex
	prompts := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		prompts++
		n := prompts
		mu.Unlock()
		if n == 1 {
			// **表明もコメントも書かない。**
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 30*time.Second)

	handoffs := fx.Tracker.HandoffCommentsOf("I_node188")
	if len(handoffs) != 1 {
		t.Fatalf("引き渡しの通知が1件ではない: %d 件（%+v）",
			len(handoffs), fx.Tracker.CommentsOf("I_node188"))
	}
	if !strings.Contains(handoffs[0].Body, "Status を **In Progress → Blocked** へ動かしました。") {
		t.Errorf("引き渡しの通知に Status の遷移が入っていない:\n%s", handoffs[0].Body)
	}

	moves := fx.Tracker.StatusMoveCommentsOf("I_node188")
	if len(moves) != 1 {
		t.Fatalf("引き渡しの通知と重ねて独立した記録を出している: %d 件（着手の1件だけのはず）\n%+v",
			len(moves), fx.Tracker.CommentsOf("I_node188"))
	}
	if !strings.Contains(moves[0].Body, "Ready → In Progress") {
		t.Errorf("残っているのが着手の記録ではない:\n%s", moves[0].Body)
	}
}

// TestStatusMove_自分の記録はエージェントが書いたコメントとして数えない は、
// 設計 3-29 の「self_marker を付ける」を確かめる。
//
// 目的: continuo が自分で書いた記録が「エージェントが何をしたか書いた」の判定
// （`hasRunComment`）をすり抜けて満たしてしまうと、**成果が何も残っていない run が
// 完了として扱われる。**
//
// 与える情報: 着手の記録は投稿されるが、エージェントは issue に1件もコメントを書かない run。
// 成功条件: continuo が「何をしたのかを書き残しませんでした」として人間へ引き渡す
// （＝自分の記録をエージェントのコメントとして数えていない）。
func TestStatusMove_自分の記録はエージェントが書いたコメントとして数えない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})
	var mu sync.Mutex
	prompts := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		prompts++
		n := prompts
		mu.Unlock()
		if n == 1 {
			// **エージェントはコメントを書かない。**continuo の記録だけが issue にある。
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})
	fx.AllowLog("コメントを書かせる", "セッションを復元できません", "Status を落とせません")

	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 30*time.Second)

	// 着手の記録は投稿されている（＝すり抜ける材料は issue にある）。
	if len(fx.Tracker.StatusMoveCommentsOf("I_node188")) == 0 {
		t.Fatalf("着手の記録が投稿されていない（この検査の前提が崩れている）:\n%+v",
			fx.Tracker.CommentsOf("I_node188"))
	}
	handoffs := fx.Tracker.HandoffCommentsOf("I_node188")
	if len(handoffs) != 1 {
		t.Fatalf("引き渡しの通知が1件ではない: %d 件（%+v）",
			len(handoffs), fx.Tracker.CommentsOf("I_node188"))
	}
	if !strings.Contains(handoffs[0].Body, "何をしたのかを issue に書き残しませんでした") {
		t.Errorf("continuo が自分の記録をエージェントのコメントとして数えている:\n%s", handoffs[0].Body)
	}
}
