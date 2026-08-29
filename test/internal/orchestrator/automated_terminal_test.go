// **終端と引き渡しの Status をボードの自動化が書いたときの検査である**（設計 3-63。issue #79）。
//
// **エージェントが turn の途中で自分の PR をマージすると、ボードの組み込みの自動化が
// `Done` を書く。**それを「人間が終わったと言っている」と読んで、continuo は走っている
// Claude Code を殺し、worktree を消しにいっていた。**自分の足元が消える。**
//
// **知らない Status と同じ猶予を掛けること**と、**人間が動かしたときは
// いままでどおり即座に止めること**を見る。
package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// automatedMoveGraceConfig は「猶予を1分だけ置く」設定を作る。
//
// **`unknown_state_grace_ms` は知らない Status と共用である**（設計 3-63）。
// 1つの issue の Status は1つなので、猶予の起点も設定キーも1つで足りる。
func automatedMoveGraceConfig(cfg *config.Config) {
	cfg.Tracker.VerifyStatesEvery = 0
	cfg.Tracker.UnknownStateGraceMs = 60000
}

// startRunAndBlockTurn は issue を1件着手させ、1回目の turn を待ち受けに入れたままにする。
//
// **turn が動いている状態を作るためのものである。**`agent.prompt` が返らない限り、
// turn ループの goroutine は生きている（`runState.turnLoopActive` が真になる）。
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
// 戻り値: 着手した issue の project item ID。
func startRunAndBlockTurn(t *testing.T, fx *fixture) string {
	t.Helper()
	blockFirstPrompt(t, fx)
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	return issue.ID
}

// TestReconcile_自動化がDoneを書いてもturnの終わりまでは止めない は、設計 3-63 を確かめる。
//
// 目的: 「PR がマージされたら Done」の自動化は、エージェントが turn の途中で自分の PR を
// マージした瞬間に走る。**次の巡回がそれを終端と読むと、走っている Claude Code を
// continuo 自身が殺す。**書いたのが人間でないと分かっているのだから、待つ。
//
// 与える情報: 1回目の turn が `agent.prompt` の待ち受けに入ったままの run。
// その間にボードの自動化が Status を `Done` へ動かす。猶予は1分。
// 成功条件:
//   - 猶予の内側では印を外さない（pane も閉じない）
//   - 待っていることをログに出す
//   - 猶予を過ぎたら、いままでどおり終える
func TestReconcile_自動化がDoneを書いてもturnの終わりまでは止めない(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now, Mutate: automatedMoveGraceConfig})
	itemID := startRunAndBlockTurn(t, fx)
	closesBefore := fx.Herdr.CountMethod(herdr.MethodPaneClose)

	// エージェントが自分の PR をマージし、ボードの組み込みの自動化が `Done` を書いた。
	fx.Tracker.SetStateByAutomation(itemID, "Done")
	fx.Orc.Tick(context.Background())
	// **終わらせる処理は別の goroutine で走る。**走らないことを見たいので、少し待つ。
	time.Sleep(500 * time.Millisecond)

	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Fatalf("turn の途中なのに印を外している: 印は %d 件", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodPaneClose); got != closesBefore {
		t.Fatalf("turn の途中なのに pane を閉じている: pane.close が %d 回", got-closesBefore)
	}
	if logs := fx.Logs.String(); !strings.Contains(logs, "turn の終わりを待っています") {
		t.Fatalf("待っていることをログに出していない（黙って遅らせている）")
	}

	// 猶予を過ぎた。**ここからは、いままでどおり終える。**
	clock.Advance(2 * time.Minute)
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)
}

// TestReconcile_人間がDoneを書いたらturnの途中でも止める は、設計 3-63 を確かめる。
//
// 目的: **待つのは「書いたのが自動化だったとき」だけである。**人間が `Done` にしたのなら、
// その人は自分の操作の結果を分かっている。**猶予を掛けると、止めたい人が既定10分待たされる。**
//
// 与える情報: 1回目の turn が `agent.prompt` の待ち受けに入ったままの run。
// その間に**人間が** Status を `Done` へ動かす。猶予は1分（掛かれば止まらないはずの長さ）。
// 成功条件: 時計を進めずに、その巡回で run が終わること。
func TestReconcile_人間がDoneを書いたらturnの途中でも止める(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now, Mutate: automatedMoveGraceConfig})
	itemID := startRunAndBlockTurn(t, fx)

	// **SetState は人間が動かした扱いである**（SetStateByAutomation と対になる）。
	fx.Tracker.SetState(itemID, "Done")
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)
}

// TestReconcile_自動化がInReviewを書いてもturnの終わりまでは止めない は、設計 3-63 を確かめる。
//
// 目的: 引き渡しの Status も自動化が書く（PR を issue に紐づけたとき）。
// **終端だけ塞いでも、同じ形でエージェントが殺される。**
//
// 与える情報: 1回目の turn が `agent.prompt` の待ち受けに入ったままの run。
// その間にボードの自動化が Status を `In Review` へ動かす。猶予は1分。
// 成功条件:
//   - 猶予の内側では印を外さない（pane も閉じない）
//   - 猶予を過ぎたら worker を止める
func TestReconcile_自動化がInReviewを書いてもturnの終わりまでは止めない(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now, Mutate: automatedMoveGraceConfig})
	itemID := startRunAndBlockTurn(t, fx)
	closesBefore := fx.Herdr.CountMethod(herdr.MethodPaneClose)

	fx.Tracker.SetStateByAutomation(itemID, "In Review")
	fx.Orc.Tick(context.Background())
	time.Sleep(500 * time.Millisecond)

	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Fatalf("turn の途中なのに印を外している: 印は %d 件", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodPaneClose); got != closesBefore {
		t.Fatalf("turn の途中なのに pane を閉じている: pane.close が %d 回", got-closesBefore)
	}

	clock.Advance(2 * time.Minute)
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)
}

// TestReconcile_猶予が0なら自動化が書いた終端でもその場で止める は、設計 3-63 を確かめる。
//
// 目的: `tracker.unknown_state_grace_ms` を 0 にした利用者は「その場で止める」と決めている。
// **知らない Status でそう決めた設定が、終端と引き渡しでだけ効かない、を作らない。**
//
// 与える情報: 猶予 0 の設定で、turn の待ち受けに入ったままの run。
// その間にボードの自動化が Status を `Done` へ動かす。
// 成功条件: その巡回で run が終わること。
func TestReconcile_猶予が0なら自動化が書いた終端でもその場で止める(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.VerifyStatesEvery = 0
			cfg.Tracker.UnknownStateGraceMs = 0
		},
	})
	itemID := startRunAndBlockTurn(t, fx)

	fx.Tracker.SetStateByAutomation(itemID, "Done")
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)
}
