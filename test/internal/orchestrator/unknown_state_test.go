// {"RUCM-CFG-SHA256": "2131e9a8ee86af8fdba4d479fe94444550b398f19e23439716928ec0c7ee73eb", "SOURCE": "docs/spec/usecases/particular_case/人間に判断を渡す.cfg.json"}
//
// **設定に名前の無い Status へ動かされたときの検査である**（設計 3-50 / 3-51）。
//
// **黙って止まらないこと**と、**turn の終わりの表明を待つこと**、そして
// **continuo 自身が止めたせいの失敗を外の障害として印字しないこと**を見る。
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

// promptUntilPaneClosed は **pane が閉じられるまで返らない `agent.prompt`** を仕込む。
//
// **実運用で起きている順番をそのまま作る。**continuo が `pane.close` を呼ぶと、
// 待ち受けの中にいた `agent.prompt` が `agent is no longer running` で落ちる。
// **その失敗は外の障害ではなく、continuo が1秒前に自分で起こしたものである。**
//
// t: 呼び出し元のテスト。後始末で必ず解放する。
// fx: 対象の fixture。
func promptUntilPaneClosed(t *testing.T, fx *fixture) {
	t.Helper()
	closed := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(closed) }) }
	t.Cleanup(release)

	paneClose := fx.Herdr.HandlerOf(herdr.MethodPaneClose)
	fx.Herdr.Handle(herdr.MethodPaneClose, func(params map[string]any) (any, *rpcErr) {
		release()
		return paneClose(params)
	})

	var mu sync.Mutex
	count := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		count++
		first := count == 1
		mu.Unlock()
		if first {
			<-closed
			return nil, &rpcErr{Code: "agent_not_found", Message: "agent is no longer running"}
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})
}

// selfCommentBody は issue に付いた continuo 自身のコメントを1本の文字列にまとめて返す。
//
// fx: 対象の fixture。
// nodeID: 下敷きの GitHub issue のノード ID。
// 戻り値: continuo 自身が書いたコメントの本文を連結したもの。
// **Status を動かした記録は数えない。**着手のたびに1件付くので、混ぜると
// 「止めた理由を書いたか」を判定できなくなる（設計 3-29）。
func selfCommentBody(fx *fixture, nodeID string) string {
	var b strings.Builder
	for _, c := range fx.Tracker.CommentsOf(nodeID) {
		if c.IsSelf && !isStatusMoveComment(c) {
			b.WriteString(c.Body)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// {"RUCM-PATH": "P013"}
//
// TestRUCMHandoff_P013_知らないStatusで止めるときはissueに理由を書く は、設計 3-50 を確かめる。
//
// 目的: 設定に名前が出てこない Status へ動かされた issue を、continuo は黙って止めていた。
// **その issue は `active_states` に入らないので二度と拾われず、ボードにも issue にも
// 何も残らない。**人間には「なぜか止まった issue」だけが残る。
//
// 与える情報: 着手済みの issue を、設定のどこにも名前が出てこない `Icebox` へ動かす。
// 猶予は 0（turn の終わりを待たない）。
// 成功条件: issue に continuo のコメントが1件付き、そこに
// 「どの Status になったか（元の値も）」「なぜ止めたか」「続けるにはどうするか」が書かれていること。
// **Status は continuo が書き換えないこと**（人間の操作を巻き戻さない）。
func TestRUCMHandoff_P013_知らないStatusで止めるときはissueに理由を書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.VerifyStatesEvery = 0
			// **猶予を置かない。**待つ側の挙動は別のテストで見る。
			cfg.Tracker.UnknownStateGraceMs = 0
		},
	})
	blockFirstPrompt(t, fx)
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// 人間（またはボードの自動化）が、continuo の知らない Status へ動かした。
	fx.Tracker.SetState(issue.ID, "Icebox")
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)

	body := selfCommentBody(fx, "I_node188")
	if body == "" {
		t.Fatalf("知らない Status で止めたのに issue に1文字も残っていない")
	}
	for _, want := range []string{
		// どの Status になったか。
		"Icebox",
		// 元は何だったか（continuo が最後に書いた値）。
		"In Progress",
		// なぜ止めたか。
		"知らない Status",
		// 続けるにはどうするか。
		"active_states",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("止めた理由のコメントに %q が無い:\n%s", want, body)
		}
	}
	if got := fx.Tracker.StateOf(issue.ID); got != "Icebox" {
		t.Errorf("人間が動かした Status を continuo が書き換えている: got %q, want %q", got, "Icebox")
	}
}

// {"RUCM-PATH": "P014"}
//
// TestRUCMHandoff_P014_turnが動いている間は知らないStatusでもすぐには止めない は、設計 3-50 を確かめる。
//
// 目的: エージェントが turn の最後に `CONTINUO-STATUS:` を書けば、continuo が正しい Status へ
// 戻す（3-25）。**turn が終わる前に殺すと、その表明が読まれずに捨てられる。**
//
// 与える情報: 1回目の turn が `agent.prompt` の待ち受けに入ったままの run。
// その間にボードの Status が `Icebox`（設定のどこにも名前が出てこない）へ動く。猶予は1分。
// 成功条件:
//   - 猶予の内側では worker を止めない（pane も閉じない。コメントも書かない）
//   - 待っていることをログに出す（人間が止めたいときに遅れることを黙って隠さない）
//   - 猶予を過ぎたら止めて、理由を issue へ書く
func TestRUCMHandoff_P014_turnが動いている間は知らないStatusでもすぐには止めない(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{
		Now: clock.Now,
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.VerifyStatesEvery = 0
			cfg.Tracker.UnknownStateGraceMs = 60000
		},
	})
	blockFirstPrompt(t, fx)
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	closesBefore := fx.Herdr.CountMethod(herdr.MethodPaneClose)

	fx.Tracker.SetState(issue.ID, "Icebox")
	fx.Orc.Tick(context.Background())
	// **止める処理は別の goroutine で走る。**走らないことを見たいので、少し待つ。
	time.Sleep(500 * time.Millisecond)

	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Fatalf("turn の途中なのに印を外している: 印は %d 件", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodPaneClose); got != closesBefore {
		t.Fatalf("turn の途中なのに pane を閉じている: pane.close が %d 回", got-closesBefore)
	}
	if body := selfCommentBody(fx, "I_node188"); body != "" {
		t.Fatalf("turn の途中なのに止めた理由を書いている:\n%s", body)
	}
	if logs := fx.Logs.String(); !strings.Contains(logs, "turn の終わりを待っています") {
		t.Fatalf("待っていることをログに出していない（人間が止めたいときに遅れることを隠している）")
	}

	// 猶予を過ぎた。**ここからは止める。**
	clock.Advance(2 * time.Minute)
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)

	if body := selfCommentBody(fx, "I_node188"); !strings.Contains(body, "Icebox") {
		t.Fatalf("猶予を過ぎても止めた理由を issue へ書いていない:\n%s", body)
	}
}

// TestStopWorker_自分で止めた直後のturnの失敗をWARNにしない は、設計 3-51 を確かめる。
//
// 目的: `turn を送れませんでした（agent is no longer running）` は外の障害ではない。
// **continuo が1秒前に自分で `pane.close` を呼んだために起きている。**
// WARN で出すと、読んだ人は herdr か Claude Code を疑って原因を探しにいく。
//
// 与える情報: 1回目の `agent.prompt` は pane が閉じられるまで返らず、閉じられたら
// `agent_not_found` を返す。その間にボードの Status が `In Review` へ動き、
// continuo が worker を止める。
// 成功条件:
//   - `turn を送れませんでした` が1行も出ないこと
//   - 代わりに「continuo が止めたから終わりにした」と分かる1行が出ること
func TestStopWorker_自分で止めた直後のturnの失敗をWARNにしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	promptUntilPaneClosed(t, fx)
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// 人間がレビューへ引き取った。continuo は worker を止める（worktree は残す）。
	fx.Tracker.SetState(issue.ID, "In Review")
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)

	waitFor(t, 5*time.Second, "止めたことと結び付けた1行が出る", func() bool {
		return strings.Contains(fx.Logs.String(), "continuo がこの worker を止めたので")
	})
	if logs := fx.Logs.String(); strings.Contains(logs, "turn を送れませんでした") {
		t.Fatalf("continuo 自身が止めたせいの失敗を、外の障害として印字している:\n%s", logs)
	}
}

// TestStopWorker_止めたらherdrの待ち受けの中のturnループも解ける は、設計 3-51 を確かめる。
//
// 目的: **止める側は turn ループへ「待つのをやめろ」と伝える手段を持っていなかった。**
// herdr が答えを返さないまま pane を失うと、turn ループは待ち受けの中に残り続ける。
// `claude.turn_timeout_ms` は turn の総実行時間の上限ではない（3-21）ので、
// **その待ちには期限が無い。**
//
// 与える情報: 1回目の `agent.prompt` が**何があっても返らない**（pane を閉じても返らない）
// テスト用herdr mock。その間にボードの Status が `In Review` へ動き、continuo が worker を止める。
// 成功条件: 待ち受けが解け、止めたことと結び付けた1行が出ること。
func TestStopWorker_止めたらherdrの待ち受けの中のturnループも解ける(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	// **pane を閉じても返らない。**herdr が黙り込んだ状態である。
	blockFirstPrompt(t, fx)
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	fx.Tracker.SetState(issue.ID, "In Review")
	fx.Orc.Tick(context.Background())

	waitFor(t, 5*time.Second, "待ち受けが解けて、止めたことと結び付けた1行が出る", func() bool {
		return strings.Contains(fx.Logs.String(), "continuo がこの worker を止めたので")
	})
}

// TestUnknownState_cleanup_on_statesで止めても貼ると起動しない案内を出さない は、
// 設計 3-57 を確かめる（issue #76）。
//
// 目的: **止めたときの案内は `tracker.active_states` へ足せと言っていた。**
// その名前が `cleanup.on_states` にあると、**言われたとおりに足した設定は起動しない**
// （`config.Validate` が「走っている worktree を片付けてしまう」として弾く。設計 3-9）。
// **人間は案内どおりに直したのに、continuo を起動できなくなる。**
//
// 与える情報: `cleanup.on_states` が `Archived` だけの設定（`tracker.terminal_states` は
// 既定の `Done` のままなので、`Archived` は continuo の知らない Status である）と、
// 着手済みの issue を `Archived` へ動かす操作。猶予は 0（turn の終わりを待たない）。
//
// 成功条件: 止めた理由のコメントに `active_states` へ書き足せという案内が無く、
// 代わりに「この名前は `cleanup.on_states` にある」ことと、
// **そのまま書いても起動する直し方**（`tracker.terminal_states` へ足す）が書かれていること。
func TestUnknownState_cleanup_on_statesで止めても貼ると起動しない案内を出さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.VerifyStatesEvery = 0
			// **猶予を置かない。**待つ側の挙動は別のテストで見る。
			cfg.Tracker.UnknownStateGraceMs = 0
			// **`tracker.terminal_states` には足さない。**足すと知っている Status になり、
			// 知らない Status で止まる道を1度も通らない。
			cfg.Cleanup.OnStates = []string{"Archived"}
		},
	})
	blockFirstPrompt(t, fx)
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// ★ 人間が、片付けを始める Status へ動かした。
	fx.Tracker.SetState(issue.ID, "Archived")
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)

	body := selfCommentBody(fx, "I_node188")
	if body == "" {
		t.Fatalf("知らない Status で止めたのに issue に1文字も残っていない")
	}
	// **既定の案内をそのまま出してはならない。**書くと continuo が起動しなくなる。
	if strings.Contains(body,
		"WORKFLOW.md の `tracker.active_states` か `tracker.status_signal_map` にその名前を書き足して") {
		t.Errorf("`cleanup.on_states` にある Status なのに `active_states` へ足す案内を出している"+
			"（言われたとおりに書くと continuo が起動しない）:\n%s", body)
	}
	for _, want := range []string{
		// どこに書いてある名前なのか。
		"`cleanup.on_states`",
		// そのまま書いても起動する直し方。
		"`tracker.terminal_states` に書き足してください",
		// 起動しなくなる直し方は、そうと分かるように書いてある。
		"continuo は起動しません",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("止めた理由のコメントに %q が無い:\n%s", want, body)
		}
	}
}
