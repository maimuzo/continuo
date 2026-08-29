// {"RUCM-CFG-SHA256": "2131e9a8ee86af8fdba4d479fe94444550b398f19e23439716928ec0c7ee73eb", "SOURCE": "docs/spec/usecases/particular_case/人間に判断を渡す.cfg.json"}
//
// **設定に名前の無い Status へ動かされたときの検査である**（設計 3-50 / 3-51）。
//
// **黙って止まらないこと**と、**turn の終わりの表明を待つこと**、そして
// **continuo 自身が止めたせいの失敗を外の障害として印字しないこと**を見る。
package orchestrator_test

import (
	"context"
	"os"
	"path/filepath"
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
// 設計 3-57b を確かめる（issue #76）。
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

// TestUnknownState_対応表と片付けの両方に名前があっても貼ると起動しない案内を出さない は、
// 設計 3-57b を確かめる（issue #76）。
//
// 目的: **同じ名前が `tracker.automated_state_rewrite` のキーと `cleanup.on_states` の
// 両方にある設定は、そのまま起動できる**（`config.Validate` はどちらの検査にも当たらない）。
// **その設定で止まると、対応表側の案内だけが出る。**そこには「対応表のその行を消してから
// `tracker.active_states` へ書き足せ」と書いてあるが、**そのとおりに直すと
// `cleanup.on_states` に名前が残るので、`config.Validate` が弾いて起動しなくなる。**
// issue #76 と同じ症状が、この組み合わせでだけ残っていた。
//
// 与える情報: `Archived` を `tracker.automated_state_rewrite` のキー（`Archived` → `Ready`）
// と `cleanup.on_states` の両方に書いた設定と、**人間が**着手済みの issue を `Archived` へ
// 動かす操作（人間が動かしたので書き戻しは起きず、止まる道へ入る）。猶予は 0。
//
// 成功条件: 止めた理由のコメントが、**名前が2箇所にあることと、両方から消す順番**を書いて
// いること。**対応表の行を消すだけで済むかのように書いていないこと**
// （`cleanup.on_states` からも消す、という文が要る）。
func TestUnknownState_対応表と片付けの両方に名前があっても貼ると起動しない案内を出さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.VerifyStatesEvery = 0
			cfg.Tracker.UnknownStateGraceMs = 0
			// **同じ名前を2箇所に置く。**`config.Validate` はこの組み合わせを弾かない。
			cfg.Tracker.AutomatedStateRewrite = map[string]string{"Archived": "Ready"}
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

	// ★ **人間が**動かした（`SetState` は人間の操作である）。書き戻しは起きない。
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
		t.Errorf("2箇所に名前がある Status なのに既定の案内を出している"+
			"（言われたとおりに書くと continuo が起動しない）:\n%s", body)
	}
	for _, want := range []string{
		// 名前がどこにあるか（2箇所とも）。
		"`tracker.automated_state_rewrite` のキー（`Archived` → `Ready`）",
		"`cleanup.on_states`",
		// 消す順番。
		"まず `tracker.automated_state_rewrite` からその行を消してください",
		// **`cleanup.on_states` からも消す**、が抜けていたのが issue #76 の残りである。
		"`cleanup.on_states` からもその行を消してから",
		// 終わったとみなす側の直し方。
		"`tracker.terminal_states` に書き足してください",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("止めた理由のコメントに %q が無い:\n%s", want, body)
		}
	}
}

// TestUnknownState_cleanup_on_statesで止めたらworktreeを片付けると書く は、
// 設計 3-57b を確かめる（issue #76）。
//
// 目的: **止めた理由のコメントは「worktree は残してあります（下記）」と書き、パスまで
// 載せていた。**だが `cleanup.on_states` の Status では、continuo はその worktree を
// 片付ける（`finishRunClaimed` の `ShouldCleanup` と、次の巡回の `reconcileWorktrees`）。
// **案内どおりにパスを開こうとすると、もう無い。**
//
// 与える情報: `cleanup.on_states` が `Archived` だけの設定と、着手済みの issue を
// `Archived` へ動かす操作。猶予は 0。**そのあと巡回をもう1回回す。**
//
// 成功条件: コメントに「残してあります」が無く、片付けることが書いてあること。
// そして**実際に worktree が消えていること**（文言と実装が食い違っていないこと）。
func TestUnknownState_cleanup_on_statesで止めたらworktreeを片付けると書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.VerifyStatesEvery = 0
			cfg.Tracker.UnknownStateGraceMs = 0
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
	wtPath := filepath.Join(
		fx.WorktreeRoot, "github.com", "octocat", "hello-world", "continuo-octocat-hello-world-188")
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("着手したのに worktree が無い: %v", err)
	}

	fx.Tracker.SetState(issue.ID, "Archived")
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)

	body := selfCommentBody(fx, "I_node188")
	if body == "" {
		t.Fatalf("知らない Status で止めたのに issue に1文字も残っていない")
	}
	if strings.Contains(body, "worktree は残してあります") {
		t.Errorf("片付ける Status なのに「worktree は残してあります」と書いている:\n%s", body)
	}
	for _, want := range []string{
		"worktree は残りません",
		"この worktree と branch を片付けます",
		"コミットしていない変更か、push していない commit が残っている worktree は片付けません",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("止めた理由のコメントに %q が無い:\n%s", want, body)
		}
	}

	// **文言だけでなく、実際に消えることを見る。**次の巡回の `reconcileWorktrees` が片付ける。
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("片付けると書いたのに worktree が残っている: %s (err=%v)", wtPath, err)
	}
}

// TestUnknownState_cleanup_enabledがfalseならworktreeは残ると書く は、設計 3-57b を確かめる
// （issue #76）。
//
// 目的: **「片付ける Status か」だけを見て「片付けます」と書いてはならない。**
// `cleanup.enabled` が false のとき `workspace.Manager.Cleanup` は `Deferred` を返して
// 1バイトも消さない。それでも `cleanup.on_states` に名前があるだけで「worktree は残りません」
// と書くと、**残っている worktree を捨てたと読ませることになる**（前の直しが作った逆向きの嘘）。
//
// 与える情報: `cleanup.on_states` が `Archived` だけで、**`cleanup.enabled` が false** の設定と、
// 着手済みの issue を `Archived` へ動かす操作。猶予は 0。**そのあと巡回をもう1回回す。**
//
// 成功条件: コメントが「worktree は残してあります」と書き、`cleanup.enabled` が false である
// ことに触れていること。**「残りません」と書いていないこと。**そして**実際に worktree が
// 残っていること**（文言と実装が食い違っていないこと）。
func TestUnknownState_cleanup_enabledがfalseならworktreeは残ると書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.VerifyStatesEvery = 0
			cfg.Tracker.UnknownStateGraceMs = 0
			cfg.Cleanup.OnStates = []string{"Archived"}
			// ★ 片付けそのものを切ってある。名前が `cleanup.on_states` にあっても消えない。
			cfg.Cleanup.Enabled = false
		},
	})
	blockFirstPrompt(t, fx)
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	wtPath := filepath.Join(
		fx.WorktreeRoot, "github.com", "octocat", "hello-world", "continuo-octocat-hello-world-188")
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("着手したのに worktree が無い: %v", err)
	}

	fx.Tracker.SetState(issue.ID, "Archived")
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)

	body := selfCommentBody(fx, "I_node188")
	if body == "" {
		t.Fatalf("知らない Status で止めたのに issue に1文字も残っていない")
	}
	if strings.Contains(body, "worktree は残りません") {
		t.Errorf("`cleanup.enabled` が false なのに「worktree は残りません」と書いている:\n%s", body)
	}
	for _, want := range []string{
		"worktree は残してあります（下記）。",
		"`cleanup.enabled` が false なので continuo は片付けを行いません",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("止めた理由のコメントに %q が無い:\n%s", want, body)
		}
	}
	// **ログの1行も同じ判定でなければならない。**
	if strings.Contains(fx.Logs.String(), "cleanup.on_states の Status なので worktree は片付けます") {
		t.Errorf("`cleanup.enabled` が false なのに WARN が「worktree は片付けます」と言っている:\n%s",
			fx.Logs.String())
	}

	// **文言だけでなく、実際に残ることを見る。**巡回をもう1回回しても消えない。
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("残してあると書いたのに worktree が無い: %s (err=%v)", wtPath, err)
	}
}

// TestUnknownState_見送る条件を切ってあれば残っていれば片付けないと書かない は、
// 設計 3-57b を確かめる（issue #76）。
//
// 目的: 「コミットしていない変更か、push していない commit が残っていれば片付けません」は
// **`cleanup.require_clean_worktree` / `cleanup.require_pushed` が真のときだけの話である**
// （`internal/workspace/cleanup.go` の `leftoverReasons` が、それぞれのフラグで囲っている）。
// **両方を false にした設定へその一文を出すと、消えないと読める worktree が消える。**
//
// 与える情報: `cleanup.on_states` が `Archived` だけで、**見送る条件を2つとも false** にした
// 設定と、着手済みの issue を `Archived` へ動かす操作。猶予は 0。
//
// 成功条件: コメントが「worktree は残りません」と書きつつ、**見送りの一文を出していないこと。**
func TestUnknownState_見送る条件を切ってあれば残っていれば片付けないと書かない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.VerifyStatesEvery = 0
			cfg.Tracker.UnknownStateGraceMs = 0
			cfg.Cleanup.OnStates = []string{"Archived"}
			// ★ 残りものを見ない設定。見ないので、残っていても片付ける。
			cfg.Cleanup.RequireCleanWorktree = false
			cfg.Cleanup.RequirePushed = false
		},
	})
	blockFirstPrompt(t, fx)
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	fx.Tracker.SetState(issue.ID, "Archived")
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)

	body := selfCommentBody(fx, "I_node188")
	if body == "" {
		t.Fatalf("知らない Status で止めたのに issue に1文字も残っていない")
	}
	if !strings.Contains(body, "worktree は残りません") {
		t.Errorf("片付ける設定なのに「worktree は残りません」が無い:\n%s", body)
	}
	for _, unwanted := range []string{
		"コミットしていない変更",
		"push していない commit",
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("見送る条件を切ってあるのに %q が残っていれば片付けない、と書いている:\n%s",
				unwanted, body)
		}
	}
}
