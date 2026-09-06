// **終端と引き渡しの Status をカンバンの自動化が書いたときの検査である**（設計 3-74。issue #79）。
//
// **エージェントが turn の途中で自分の PR をマージすると、カンバンの組み込みの自動化が
// `Done` を書く。**それを「人間が終わったと言っている」と読んで、continuo は走っている
// Claude Code を殺し、worktree を消しにいっていた。**自分の足元が消える。**
//
// **知らない Status と同じ猶予を掛けること**と、**人間が動かしたときは
// いままでどおり即座に止めること**を見る。
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

// automatedMoveGraceConfig は「猶予を1分だけ置く」設定を作る。
//
// **`unknown_state_grace_ms` は知らない Status と共用である**（設計 3-74）。
// **待つ長さの設定は共用だが、猶予の起点は種類が変わったら切り直す**（`externalMoveKind`）。
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

// TestReconcile_自動化がDoneを書いてもturnの終わりまでは止めない は、設計 3-74 を確かめる。
//
// 目的: 「PR がマージされたら Done」の自動化は、エージェントが turn の途中で自分の PR を
// マージした瞬間に走る。**次の巡回がそれを終端と読むと、走っている Claude Code を
// continuo 自身が殺す。**書いたのが人間でないと分かっているのだから、待つ。
//
// 与える情報: 1回目の turn が `agent.prompt` の待ち受けに入ったままの run。
// その間にカンバンの自動化が Status を `Done` へ動かす。猶予は1分。
// 成功条件:
//   - 猶予の内側では印を外さない（pane も閉じない）
//   - 待っていることをログに出す
//   - 猶予を過ぎたら、いままでどおり終える
func TestReconcile_自動化がDoneを書いてもturnの終わりまでは止めない(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now, Mutate: automatedMoveGraceConfig})
	itemID := startRunAndBlockTurn(t, fx)
	closesBefore := fx.Herdr.CountMethod(herdr.MethodPaneClose)

	// エージェントが自分の PR をマージし、カンバンの組み込みの自動化が `Done` を書いた。
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

// TestReconcile_人間がDoneを書いたらturnの途中でも止める は、設計 3-74 を確かめる。
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

// TestReconcile_自動化がInReviewを書いてもturnの終わりまでは止めない は、設計 3-74 を確かめる。
//
// 目的: 引き渡しの Status も自動化が書く（PR を issue に紐づけたとき）。
// **終端だけ塞いでも、同じ形でエージェントが殺される。**
//
// 与える情報: 1回目の turn が `agent.prompt` の待ち受けに入ったままの run。
// その間にカンバンの自動化が Status を `In Review` へ動かす。猶予は1分。
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

// TestReconcile_猶予が0なら自動化が書いた終端でもその場で止める は、設計 3-74 を確かめる。
//
// 目的: `tracker.unknown_state_grace_ms` を 0 にした利用者は「その場で止める」と決めている。
// **知らない Status でそう決めた設定が、終端と引き渡しでだけ効かない、を作らない。**
//
// 与える情報: 猶予 0 の設定で、turn の待ち受けに入ったままの run。
// その間にカンバンの自動化が Status を `Done` へ動かす。
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

// TestReconcile_作業中のままdispatchできなくなったら待たずに止める は、設計 3-74 を確かめる。
//
// 目的: `active_states` に入ったままでも `Dispatchable` が偽になると止める（設計 3-13。
// リポジトリの Claude Code への信頼登録が外れた等）。**これは Status の引き渡しではない。**
// **書いたのが自動化かどうかで待つと、Status と無関係な理由で止めるはずの run が
// 猶予ぶん止まらなくなる。**
//
// 与える情報: turn の待ち受けに入ったままの run。Status は `In Progress` のままで、
// **書いたのはカンバンの自動化**、`Dispatchable` だけが偽になる。猶予は1分。
// 成功条件: 時計を進めずに、その巡回で run が終わること。
func TestReconcile_作業中のままdispatchできなくなったら待たずに止める(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now, Mutate: automatedMoveGraceConfig})
	itemID := startRunAndBlockTurn(t, fx)

	// Status は作業中のまま。**書いたのは自動化**だが、止める理由はそこではない。
	fx.Tracker.SetStateByAutomation(itemID, "In Progress")
	fx.Tracker.SetDispatchable(itemID, false)
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)
}

// TestReconcile_知らないStatusで待った時間は自動化の猶予へ持ち越さない は、設計 3-74 を確かめる。
//
// 目的: 猶予の起点は知らない Status と同じ場所に持つが、**種類が変わったら切り直す。**
// 2つは同時には起きないが、**順には起きる。**知らない Status で待っていた run が続けて
// 自動化に `Done` へ動かされたとき、起点を繰り越すと猶予が前回ぶんだけ短くなる。
//
// 与える情報: 猶予1分。turn の待ち受けに入ったままの run へ、
// まず人間が知らない Status（`Icebox`）を書き、50秒後にカンバンの自動化が `Done` を書く。
// 成功条件: `Done` から50秒（最初の動きからは100秒）経っても、まだ止まっていないこと。
// **起点を繰り越していれば、この時点で猶予（1分）を過ぎて止まっている。**
func TestReconcile_知らないStatusで待った時間は自動化の猶予へ持ち越さない(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now, Mutate: automatedMoveGraceConfig})
	itemID := startRunAndBlockTurn(t, fx)
	closesBefore := fx.Herdr.CountMethod(herdr.MethodPaneClose)

	// 人間が知らない Status へ動かした。turn が動いているので猶予の内側で待つ。
	fx.Tracker.SetState(itemID, "Icebox")
	fx.Orc.Tick(context.Background())
	time.Sleep(300 * time.Millisecond)
	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Fatalf("知らない Status の猶予の内側なのに印を外している: 印は %d 件", got)
	}

	// 50秒後、カンバンの自動化が `Done` を書いた。**ここで猶予を数え直す。**
	clock.Advance(50 * time.Second)
	fx.Tracker.SetStateByAutomation(itemID, "Done")
	fx.Orc.Tick(context.Background())
	time.Sleep(300 * time.Millisecond)

	// さらに50秒。`Done` からは50秒しか経っていないので、まだ待つ。
	clock.Advance(50 * time.Second)
	fx.Orc.Tick(context.Background())
	time.Sleep(500 * time.Millisecond)

	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Fatalf("起点を繰り越して猶予を短くしている: 印は %d 件", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodPaneClose); got != closesBefore {
		t.Fatalf("起点を繰り越して pane を閉じている: pane.close が %d 回", got-closesBefore)
	}

	// `Done` から猶予を過ぎれば、いままでどおり終える。
	clock.Advance(2 * time.Minute)
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)
}

// TestReconcile_人間がInReviewを書いたらturnの途中でも止める は、設計 3-74c を確かめる。
//
// **なぜこの検査が要るか。**設計 3-74c で「**continuo 自身が書いた Status には巡回が反応しない**」
// という門を1つ足した（`holdForOwnMove`）。**その門が広すぎると、人間が動かしたときも待ってしまう。**
// 人間が `In Review` にしたのなら、その人は自分の操作の結果を分かっている。
// **待たされると、止めたい人が turn の終わりまで足止めされる。**
//
// **門が狭いことの根拠。**`rs.lastWrittenState()` は、continuo がカンバンへ**書き込みに成功したとき
// だけ**更新される（[internal/orchestrator/lifecycle.go:365-368]）。
// この検査の時点で控えてあるのは、着手のときに書いた `In Progress` である。
// **人間が書いた `In Review` とは一致しないので、門は開かない。**
//
// **「continuo 自身が書いたとき」の側は、この検査では作れない。**
// その状態を作るには turn の終わりの経路が Status を書き終えている必要があり、
// 書き終えた直後にその経路は自分で run を畳むので、**巡回と競う窓を狙って止められない。**
// **そちらは `test/e2e` の `TestE2E_手順書の段1から段9までをmockだけで通す` が押さえている**
// （設計 3-74c を入れる前は、CI の `-race` を付けたステップで
// 「段8: run の終わりまで進む」が60秒の上限に届かず落ちていた）。
//
// 与える情報: 1回目の turn が `agent.prompt` の待ち受けに入ったままの run。
// その間に**人間が** Status を `In Review` へ動かす。猶予は1分（掛かれば止まらないはずの長さ）。
// 成功条件: 時計を進めずに、その巡回で run が終わること。
func TestReconcile_人間がInReviewを書いたらturnの途中でも止める(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now, Mutate: automatedMoveGraceConfig})
	itemID := startRunAndBlockTurn(t, fx)

	// **SetState は人間が動かした扱いである**（SetStateByAutomation と対になる）。
	fx.Tracker.SetState(itemID, "In Review")
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)
}

// blockSecondPrompt は2回目の `agent.prompt` を返させず、turn ループを動かしたままにする。
//
// **1回目ではなく2回目を止める。**1回目の終わりに表明を読ませたいので、
// **1回目は返さないと `Stop` の判定が始まらない。**
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
// 戻り値: 待たせている2回目を返させる関数。
func blockSecondPrompt(t *testing.T, fx *fixture) func() {
	t.Helper()
	release := make(chan struct{})
	var once sync.Once
	releaseOnce := func() { once.Do(func() { close(release) }) }
	t.Cleanup(releaseOnce)

	var mu sync.Mutex
	count := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		count++
		second := count == 2
		mu.Unlock()
		if second {
			<-release
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})
	return releaseOnce
}

// TestReconcile_continuoが書いたInReviewには巡回が反応しない は、設計 3-74c を確かめる。
//
// **これが、この変更が直した側の検査である。**
//
// **なぜ e2e では足りないか。**巡回と turn の終わりの経路のどちらが勝つかは、
// 実行の順番で決まる。**門を消しても e2e は緑のまま通る**
// （レビュワーが `-race` を付けて20回走らせ、20回とも通ることを実測した）。
// **偶然に頼らずに窓を開く必要がある。**
//
// **どうやって窓を開くか。**窓は「Status を書いてから権利を取るまで」だけでなく、
// **「Status を書いたのに turn ループが run を畳まなかったとき」にも開く。**
// `handleTurnEnd` の取り直しが失敗すると、古い写し（`Ready`）が返るので、
// **turn ループは畳まずに次の turn へ進む。**その隙に巡回を1回だけ回す。
//
// 与える情報: 1回目の turn でエージェントが `CONTINUO-STATUS: review` を表明し、
// その直後の取り直しだけを失敗させた run。2回目の `agent.prompt` は返さない。
// 成功条件:
//   - 印から外れないこと（巡回が run を畳んでいない）
//   - pane を閉じていないこと
//   - 任せたことをログに出していること
func TestReconcile_continuoが書いたInReviewには巡回が反応しない(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now, Mutate: automatedMoveGraceConfig})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	releaseSecond := blockSecondPrompt(t, fx)
	closesBefore := fx.Herdr.CountMethod(herdr.MethodPaneClose)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "1回目の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// **取り直しだけを失敗させる。**turn の終わりの経路は Status を書いたあと、
	// 古い写し（`Ready`）を見て「まだ作業中」と読み、run を畳まずに次の turn へ進む。
	//
	// **この WARN は、この検査がわざと起こしているものである。**
	fx.AllowLog("issue を取り直せません")
	fx.Tracker.SetIDsError(errors.New("取り直せません"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\n\nCONTINUO-STATUS: review", false),
	})
	fx.Orc.OnHook(stopEvent(fx.Sessions[0], path, "p1"))

	waitFor(t, 20*time.Second, "continuo が In Review を書く", func() bool {
		return strings.Contains(fx.Logs.String(), "表明どおりに Status を動かしました")
	})
	waitFor(t, 20*time.Second, "2回目の turn が送られる（turn ループが生きている）", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) >= 2
	})

	// **ここから巡回を1回回す。**取り直しは元に戻す（戻さないと巡回もカンバンを読めない）。
	fx.Tracker.SetIDsError(nil)
	fx.Orc.Tick(context.Background())
	time.Sleep(500 * time.Millisecond)

	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Errorf("continuo 自身が書いた In Review で印を外しています: 印は %d 件。"+
			"**巡回が turn の終わりの経路を追い越して run を畳むと、"+
			"エージェントのコメントの確認も worktree の片付けも飛びます**（issue #175）", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodPaneClose); got != closesBefore {
		t.Errorf("continuo 自身が書いた In Review で pane を閉じています: pane.close が %d 回",
			got-closesBefore)
	}
	if !strings.Contains(fx.Logs.String(), "turn の終わりの経路に任せます") {
		t.Errorf("任せたことをログに出していません。全文:\n%s", fx.Logs.String())
	}

	releaseSecond()
}
