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

		// ★ エージェントは応答を書き終えている（人間が切りのいいところで戻す、が前提）。
		fx.Herdr.SetStatus(herdr.AgentStatusIdle)

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

// TestHumanMode_エージェントが動いている最中に戻したら指示を送らない は、turn の混ざりを防ぐ。
//
// 目的: **人間が話しかけた直後（エージェントが応答を書いている最中）にカードを戻すのは
// 自然な操作である。**そこへ `agent.prompt` を投げると turn が混ざる。
// 復元の段5a2（設計 3-4）が同じ場面で「送らずに turn の終わりを待つ」と決めているので、
// **人間モードから戻すときも同じ判断にする。**
//
// 与える情報: 人間モードに入れたあと、`agent_status` が `working` のまま
// Status を `In Progress` へ戻した run。
// 成功条件: 指示が1つも飛ばないこと（turn の終わりを待つ側へ倒れていること）。
func TestHumanMode_エージェントが動いている最中に戻したら指示を送らない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withHumanState(t) // AgentStatus は working のまま
		defer fx.Orc.Close()
		issue := adoptRun(fx, 188)

		fx.Tracker.SetState(issue.ID, humanState)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		fx.Tracker.SetState(issue.ID, fx.Config.Tracker.RunningState)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		if got := fx.Herdr.Prompts(); len(got) != 0 {
			t.Fatalf("エージェントが動いている最中に指示を送った（turn が混ざる）: %v", got)
		}
		if _, ok := viewOf(fx, issue.Identifier); !ok {
			t.Fatal("戻したのに印から外れている")
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

// TestHumanMode_話している最中の表明でカードを奪われない は、issue #263 が名指しした症状を塞ぐ。
//
// 目的: **人間が pane で話しかけた返事に `CONTINUO-STATUS: blocked` の1行が入っていても、
// 人間モードのカードを `Blocked` へ書き換えないこと。**
// 書き換えると Status が人間モードから外れ、そのまま引き渡しとして pane が閉じる。
// **これが「AIの判断でblockedなどに遷移し、チャットが強制切断される」の中身である。**
//
// **巡回が人間モードを立てるより先に turn の終わりが来る筋も、同時に確かめている。**
// カードを動かしてから巡回を1回も回さずに `Stop` を流すので、continuo が人間モードを
// 知るのは `decideAfterTurn` が Status を取り直した時点である。
//
// 与える情報: `human_state` を設定した fixture。着手して1回目の turn が待ち受けに入った run。
// その最中に人間がカードを `Human` へ動かし、エージェントの応答には `blocked` の表明がある。
// 成功条件: Status が `Human` のまま、pane が1つも閉じず、2回目の指示も飛ばないこと。
func TestHumanMode_話している最中の表明でカードを奪われない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Tracker.HumanState = humanState },
	})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	releasePrompt := blockFirstPrompt(t, fx)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "1回目の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// ★ 人間が pane に入る前に、カードを人間モードへ動かした（FAQ の手順1）。
	fx.Tracker.SetState("PVTI_item188", humanState)
	// **エージェントのコメントを置いておく。**無いと run を終えるときに
	// 「コメントの取り戻し」（設計 3-25 の9段）へ入り、そこでも `agent.prompt` を呼ぶ。
	fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())

	// ★ 人間に返事をしたエージェントが、その応答に `blocked` の表明を書いた。
	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "ここはどうしますか"),
		assistantLine("req1", "判断を仰ぎたいです。\n\nCONTINUO-STATUS: blocked", false),
	})
	fx.Orc.OnHook(stopEvent(fx.Sessions[0], path, "p1"))
	releasePrompt()

	// turn の終わりの処理が Status を書きに行くところまで進むのを待つ。
	waitFor(t, 20*time.Second, "表明の適用が Status を書きに行く", func() bool {
		return fx.Tracker.CountCall("UpdateStatus") > 0
	})
	// **書きに行っても、人間モードのカードは動かない**（`protectedStates`）。
	waitFor(t, 20*time.Second, "turn ループが畳まれる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) == 1
	})

	if got := fx.Tracker.StateOf("PVTI_item188"); got != humanState {
		t.Fatalf("人間が引き取ったカードを continuo が動かした: got %q, want %q", got, humanState)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodPaneClose); got != 0 {
		t.Fatalf("人間が話している pane を閉じた: pane.close が %d 回", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); got != 1 {
		t.Fatalf("人間が話している pane へ continuo が指示を送った: agent.prompt が %d 回（1回のはず）", got)
	}
	if len(fx.Orc.RunningIdentifiers()) != 1 {
		t.Fatalf("人間が引き取っている run を印から外した: %v", fx.Orc.RunningIdentifiers())
	}
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
