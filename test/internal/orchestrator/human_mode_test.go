// 人間が pane で直接エージェントと話しているあいだの振る舞いの検査である（設計 3-82）。
//
// **守るのは1つだけである。「人間モードの run に対して `pane.close` を呼ばない」。**
// 呼ばれた瞬間に、人間が話していた画面が消える。
package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// humanState は、この検査で「人間が引き取っている」を表す Status である。
//
// **`tracker.active_states` にも `terminal_states` にも入っていない名前にする。**
const humanState = "Human"

// withHumanState は `tracker.human_state` を設定した検査対象を作る。
//
// t: 呼び出し元のテスト。
// 戻り値: 組み立てた stubFixture。
func withHumanState(t *testing.T) *stubFixture {
	t.Helper()
	return newStubFixture(t, stubFixtureOptions{
		AgentStatus: herdr.AgentStatusWorking,
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.HumanState = humanState
			cfg.Claude.TurnTimeoutMs = int(stallTimeout / time.Millisecond)
		},
	})
}

// TestHumanMode_人間モードのあいだは画面が止まっていても打ち切らない は、
// この機能がいちばん守りたいものを確かめる。
//
// 目的: **人間は画面の前で考えるので、画面の版は何時間も変わらない。**
// stall 検知（設計 3-21）はそれを「止まった」と読んで pane を閉じ、
// **人間が話していた画面ごと消す。**人間モードではその判定を飛ばす。
//
// 与える情報: `tracker.human_state` を設定し、Status をその値にした run。
// **画面の版は一度も増やさない。**閾値を50回またぐ（50分ぶん）。
// 成功条件: pane が1つも閉じられず、印にも残り、turn も1つも送られていないこと。
//
// **実時間はゼロである。**`testing/synctest` の bubble の中で時計を進める。
func TestHumanMode_人間モードのあいだは画面が止まっていても打ち切らない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withHumanState(t)
		issue := adoptRun(fx, 188)
		fx.Tracker.SetState(issue.ID, humanState)

		const rounds = 50
		for i := range rounds {
			time.Sleep(stallTimeout + time.Second)
			fx.Orc.Tick(context.Background())
			synctest.Wait()

			v, ok := viewOf(fx, issue.Identifier)
			if !ok {
				t.Fatalf("人間が引き取っている run を印から外した（%d 周目）", i+1)
			}
			if v.RetryCount != 0 {
				t.Fatalf("人間が引き取っている run を打ち切った（%d 周目）: retry_count = %d", i+1, v.RetryCount)
			}
		}
		if ids := fx.Herdr.ClosedPanes(); len(ids) != 0 {
			t.Fatalf("人間が話している pane を閉じた: %v", ids)
		}
		if got := fx.Herdr.Prompts(); len(got) != 0 {
			t.Fatalf("人間が話している pane へ continuo が指示を送った: %v", got)
		}
	})
}

// TestHumanMode_作業中のStatusへ戻すと同じpaneへ続きの指示を送る は、戻し方を確かめる。
//
// 目的: 設計 3-82 の「`active_states` へ戻したら、**同じ pane・同じセッションのまま**
// 続きの指示を1回送る」を示す。**pane を閉じて作り直さない。**
//
// 与える情報: 人間モードに入れたあと、Status を `In Progress` へ戻した run。
// 成功条件: `agent.prompt` が飛び、その本文が継続の指示（1回目の本文ではない）であり、
// pane が1つも閉じられていないこと。
func TestHumanMode_作業中のStatusへ戻すと同じpaneへ続きの指示を送る(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withHumanState(t)
		// **turn ループを止めてから抜ける。**この検査は turn を1つ送らせるので、
		// 送ったあとの turn ループは `Stop` hook を待ち続ける（stub は hook を出さない）。
		// **止めないと bubble から抜けられない。**
		defer fx.Orc.Close()
		issue := adoptRun(fx, 188)

		// 人間が引き取る。
		fx.Tracker.SetState(issue.ID, humanState)
		fx.Orc.Tick(context.Background())
		synctest.Wait()
		if got := fx.Herdr.Prompts(); len(got) != 0 {
			t.Fatalf("人間モードへ入れた巡回で指示を送った: %v", got)
		}

		// 人間が continuo へ返す。
		fx.Tracker.SetState(issue.ID, fx.Config.Tracker.RunningState)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		got := fx.Herdr.Prompts()
		if len(got) != 1 {
			t.Fatalf("戻したあとに送られた指示が1回ではない: %d 回 %v", len(got), got)
		}
		// **1回目の本文ではなく継続の指示であること。**人間と長く話したあとに
		// タスクの説明をもう一度送ると、そこまでの会話と噛み合わない。
		// **1回目の本文にしか出ない文字列で見分ける**（samplePromptTemplate の書き出し）。
		if strings.Contains(got[0], "を実装してください") {
			t.Errorf("戻したあとに1回目の本文を送った（継続の指示であるべき）: %q", got[0])
		}
		if ids := fx.Herdr.ClosedPanes(); len(ids) != 0 {
			t.Fatalf("戻すときに pane を閉じた（同じ pane に送るはずである）: %v", ids)
		}
	})
}

// TestHumanMode_完了のStatusへ動かすと人間モードを抜けて片付ける は、抜け方を確かめる。
//
// 目的: **抜ける条件を「`active_states` へ戻ったとき」に絞ってはならない**（設計 3-82）。
// 絞ると `Done` へ動かしたときに印が立ったままになり、`stopWorker` の門が pane を守り続けて
// **worktree も片付かない。**
//
// 与える情報: 人間モードに入れたあと、Status を `Done`（`terminal_states`）へ動かした run。
// 成功条件: pane が閉じられ、印からも外れること。
func TestHumanMode_完了のStatusへ動かすと人間モードを抜けて片付ける(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withHumanState(t)
		issue := adoptRun(fx, 188)

		fx.Tracker.SetState(issue.ID, humanState)
		fx.Orc.Tick(context.Background())
		synctest.Wait()
		if ids := fx.Herdr.ClosedPanes(); len(ids) != 0 {
			t.Fatalf("人間モードで pane を閉じた: %v", ids)
		}

		fx.Tracker.SetState(issue.ID, fx.Config.Tracker.TerminalStates[0])
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		if ids := fx.Herdr.ClosedPanes(); len(ids) == 0 {
			t.Fatal("完了へ動かしたのに pane を閉じていない（人間モードの門が開いていない）")
		}
		if _, ok := viewOf(fx, issue.Identifier); ok {
			t.Fatal("完了へ動かしたのに印から外れていない")
		}
	})
}

// TestHumanMode_設定していなければいままでどおり止める は、既定の振る舞いを守る。
//
// 目的: **`tracker.human_state` を書かなければ1つも挙動が変わらないこと。**
// 空のときに `Human` という Status へ動かされたら、それは「知らない Status」であり、
// いままでどおり猶予のあとで worker を止める（設計 3-50）。
//
// 与える情報: `human_state` を空のままにし、Status を `Human` にした run。
// **猶予は0にする**（turn ループが走っていないので、そもそも猶予は使われない）。
// 成功条件: pane が閉じられること。
func TestHumanMode_設定していなければいままでどおり止める(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newStubFixture(t, stubFixtureOptions{
			AgentStatus: herdr.AgentStatusWorking,
			Mutate: func(cfg *config.Config) {
				cfg.Tracker.UnknownStateGraceMs = 0
			},
		})
		issue := adoptRun(fx, 188)
		fx.Tracker.SetState(issue.ID, humanState)

		fx.Orc.Tick(context.Background())
		synctest.Wait()

		if ids := fx.Herdr.ClosedPanes(); len(ids) == 0 {
			t.Fatal("human_state を設定していないのに pane を閉じていない（既定の振る舞いが変わっている）")
		}
	})
}
