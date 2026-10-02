// {"RUCM-CFG-SHA256": "fbf5e2c834729398c81d2877ce33335c3d7671131c465b93cbc62707be8f5a5a", "SOURCE": "docs/spec/usecases/particular_case/人間がpaneに入って直接続ける.cfg.json"}
//
// **ユースケース記述「人間がpaneに入って直接続ける」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイル（`direct_chat_test.go` と `direct_chat_setup_test.go`）に在る。
package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// {"RUCM-PATH": "P001"}
//
// Test_人間がpaneに入って直接続ける_P001_着手待ちへ戻したら作業中のStatusを書く は、設計 3-83 の書き込みを確かめる。
//
// 目的: **direct chat の run は、着手の段2 を1度も通っていない。**
// `dispatch_state`（既定 `Ready`）へ戻されたまま放っておくと、
// **エージェントが走っているのにカードは着手待ちに見え、
// `agent.max_concurrent_agents_by_state` の勘定からも外れる。**
// 同じカンバンを見張る別の機械からは、担当者の付いていない着手待ちの issue に見える。
//
// 与える情報: direct chat に入れたあと、Status を `Ready` へ戻した run。
// 成功条件: カンバンの Status が `In Progress` になり、指示も1回送られること。
func Test_人間がpaneに入って直接続ける_P001_着手待ちへ戻したら作業中のStatusを書く(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
		defer fx.Orc.Close()
		issue := adoptOwnRun(fx, 191)

		fx.Tracker.SetState(issue.ID, humanState)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		fx.Herdr.SetStatus(herdr.AgentStatusIdle)
		fx.Tracker.SetState(issue.ID, fx.Config.Tracker.DispatchState)
		// **戻すときの後始末は巡回のループの外で走る**（設計 3-83）。
		fx.Orc.Tick(context.Background())
		synctest.Wait()
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		if got, want := fx.Tracker.StateOf(issue.ID), fx.Config.Tracker.RunningState; !strings.EqualFold(got, want) {
			t.Errorf("着手待ちのままになっている: %q（期待 %q）", got, want)
		}
		if got := fx.Herdr.Prompts(); len(got) != 1 {
			t.Errorf("戻したあとに送られた指示が1回ではない: %d 回 %v", len(got), got)
		}
		if ids := fx.Herdr.ClosedPanes(); len(ids) != 0 {
			t.Errorf("戻すときに pane を閉じた: %v", ids)
		}
	})
}

// {"RUCM-PATH": "P003"}
//
// Test_人間がpaneに入って直接続ける_P003_作業中のStatusへ戻すと同じpaneへ続きの指示を送る は、戻し方を確かめる。
//
// 目的: 設計 3-83 の「`active_states` へ戻したら、**同じ pane・同じセッションのまま**
// 続きの指示を1回送る」を示す。**pane を閉じて作り直さない。**
//
// 与える情報: direct chat に入れたあと、Status を `In Progress` へ戻した run。
// 成功条件: `agent.prompt` が飛び、その本文が継続の指示（1回目の本文ではない）であり、
// pane が1つも閉じられていないこと。
func Test_人間がpaneに入って直接続ける_P003_作業中のStatusへ戻すと同じpaneへ続きの指示を送る(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
		// **turn ループを止めてから抜ける。**この検査は turn を1つ送らせるので、
		// 送ったあとの turn ループは `Stop` hook を待ち続ける（stub は hook を出さない）。
		// **止めないと bubble から抜けられない。**
		defer fx.Orc.Close()
		issue := adoptOwnRun(fx, 188)

		// 人間が引き取る。
		fx.Tracker.SetState(issue.ID, humanState)
		fx.Orc.Tick(context.Background())
		synctest.Wait()
		if got := fx.Herdr.Prompts(); len(got) != 0 {
			t.Fatalf("direct chat へ入れた巡回で指示を送った: %v", got)
		}

		// ★ エージェントは応答を書き終えている（人間が切りのいいところで戻す、が前提）。
		fx.Herdr.SetStatus(herdr.AgentStatusIdle)

		// 人間が continuo へ返す。
		fx.Tracker.SetState(issue.ID, fx.Config.Tracker.RunningState)
		// **戻すときの後始末は巡回のループの外で走る**（設計 3-83）。
		// **1回目の巡回で印が立ち、2回目の `wakeRuns` が指示を送る。**
		fx.Orc.Tick(context.Background())
		synctest.Wait()
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

// {"RUCM-PATH": "P004"}
//
// Test_人間がpaneに入って直接続ける_P004_エージェントが動いている最中に戻したら指示を送らない は、turn の混ざりを防ぐ。
//
// 目的: **人間が話しかけた直後（エージェントが応答を書いている最中）にカードを戻すのは
// 自然な操作である。**そこへ `agent.prompt` を投げると turn が混ざる。
// 復元の段5a2（設計 3-4）が同じ場面で「送らずに turn の終わりを待つ」と決めているので、
// **direct chat から戻すときも同じ判断にする。**
//
// 与える情報: direct chat に入れたあと、`agent_status` が `working` のまま
// Status を `In Progress` へ戻した run。
// 成功条件: turn ループが送る直前の検査（応答を書いている最中か）まで進み、そこで待つ側へ倒れたことがログに出て、
// 指示が1つも飛ばないこと。
//
// **戻したあとの巡回は2回回す。**送る合図は、巡回のループの外の書き込み（hold のコメント）が終わってから立つ。
// 1回目の巡回の `wakeRuns` はその合図をまだ読めないので、1回だけだと turn ループが立たず、
// 検査を通らないまま「指示が0回」で通ってしまう。**だから、検査へ届いたことをログの文面でも確かめる。**
func Test_人間がpaneに入って直接続ける_P004_エージェントが動いている最中に戻したら指示を送らない(t *testing.T) {
	// turn ループが送る直前の検査で待つ側へ倒れたときにだけ出るログ（`turnLoop`）。
	const waitedLog = "direct chat から戻りましたが、エージェントが動いているので turn の終わりを待ちます"
	synctest.Test(t, func(t *testing.T) {
		// **ログを溜める。**`withDirectChatState` はログを捨てるので、同じ設定をここで組む。
		fx := newStubFixture(t, stubFixtureOptions{
			AgentStatus: herdr.AgentStatusWorking, // 戻したあとも working のまま
			Logs:        true,
			Mutate: func(cfg *config.Config) {
				cfg.Tracker.DirectChatState = humanState
				cfg.Claude.TurnTimeoutMs = int(stallTimeout / time.Millisecond)
			},
		})
		fx.Tracker.SetStatusOptions(directChatBoardOptions...)
		defer fx.Orc.Close()
		issue := adoptOwnRun(fx, 188)

		fx.Tracker.SetState(issue.ID, humanState)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		fx.Tracker.SetState(issue.ID, fx.Config.Tracker.RunningState)
		// 1回目の巡回が direct chat の印を下ろし、巡回のループの外で hold を書いてから送る合図を立てる。
		fx.Orc.Tick(context.Background())
		synctest.Wait()
		if strings.Contains(fx.Logs.String(), waitedLog) {
			t.Fatal("前提が崩れている（1回目の巡回で、もう turn ループが検査へ届いている。2回目の巡回が要る理由が無くなった）")
		}
		// 2回目の巡回の `wakeRuns` が合図を読み、turn ループを起こす。
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		if !strings.Contains(fx.Logs.String(), waitedLog) {
			t.Fatalf("turn ループが送る直前の検査へ届いていない（指示が0回でも、検査を通ったことにならない）:\n%s", fx.Logs.String())
		}
		if got := fx.Herdr.Prompts(); len(got) != 0 {
			t.Fatalf("エージェントが動いている最中に指示を送った（turn が混ざる）: %v", got)
		}
		if _, ok := viewOf(fx, issue.Identifier); !ok {
			t.Fatal("戻したのに印から外れている")
		}
	})
}

// {"RUCM-PATH": "P006"}
//
// TestDirectChatMode_完了のStatusへ動かすとdirect chatを抜けて片付ける は、抜け方を確かめる。
//
// 目的: **抜ける条件を「`active_states` へ戻ったとき」に絞ってはならない**（設計 3-83）。
// 絞ると `Done` へ動かしたときに印が立ったままになり、`stopWorker` の門が pane を守り続けて
// **worktree も片付かない。**
//
// 与える情報: direct chat に入れたあと、Status を `Done`（`terminal_states`）へ動かした run。
// 成功条件: pane が閉じられ、印からも外れること。
func Test_人間がpaneに入って直接続ける_P006_完了のStatusへ動かすとdirectChatを抜けて片付ける(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
		issue := adoptOwnRun(fx, 188)

		fx.Tracker.SetState(issue.ID, humanState)
		fx.Orc.Tick(context.Background())
		synctest.Wait()
		if ids := fx.Herdr.ClosedPanes(); len(ids) != 0 {
			t.Fatalf("direct chat で pane を閉じた: %v", ids)
		}

		fx.Tracker.SetState(issue.ID, fx.Config.Tracker.TerminalStates[0])
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		if ids := fx.Herdr.ClosedPanes(); len(ids) == 0 {
			t.Fatal("完了へ動かしたのに pane を閉じていない（direct chat の門が開いていない）")
		}
		if _, ok := viewOf(fx, issue.Identifier); ok {
			t.Fatal("完了へ動かしたのに印から外れていない")
		}
	})
}

// {"RUCM-PATH": "P007"}
//
// Test_人間がpaneに入って直接続ける_P007_印を持つrunの担当者が他人に替わったら手を離す は、3-83h の「手を離す経路」を確かめる。
//
// 目的: 担当者が別の1人に替わったら、印を持つ機械は pane を閉じ、印を外す。
// **Status を書かず、コメントも書かない。**新しい担当者の機械が pane を用意する。
// 与える情報: direct chat に入った run。担当者を他人1人に替える。
// 成功条件: pane が閉じ、印から外れ、`UpdateStatus` を1回も呼ばず、コメントも無いこと。
func Test_人間がpaneに入って直接続ける_P007_印を持つrunの担当者が他人に替わったら手を離す(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
		issue := adoptOwnRun(fx, 306)
		fx.Tracker.SetState(issue.ID, humanState)
		fx.Orc.Tick(context.Background())
		synctest.Wait()
		if ids := fx.Herdr.ClosedPanes(); len(ids) != 0 {
			t.Fatalf("自分が担当の direct chat で pane を閉じた: %v", ids)
		}

		fx.Tracker.ResetCalls()
		fx.Tracker.SetAssignees(issue.ID, "someone-else")
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		if ids := fx.Herdr.ClosedPanes(); len(ids) != 1 {
			t.Fatalf("担当者が替わったのに pane を閉じていない（または2枚以上閉じた）: %v", ids)
		}
		if _, ok := viewOf(fx, issue.Identifier); ok {
			t.Fatal("担当者が替わったのに印を外していない")
		}
		if n := fx.Tracker.CountCall("UpdateStatus"); n != 0 {
			t.Fatalf("手を離すときに Status を書いた: %d 回", n)
		}
		if n := len(fx.Tracker.CommentsOf("I_node306")); n != 0 {
			t.Fatalf("手を離すときにコメントを書いた: %d 件", n)
		}
		if got := fx.Tracker.StateOf(issue.ID); got != humanState {
			t.Fatalf("手を離すときにカードを動かした: %q", got)
		}
	})
}

// {"RUCM-PATH": "P008"}
//
// Test_人間がpaneに入って直接続ける_P008_印を持つrunの担当者が0人になったらfailure_stateへ動かし指示を送らない は、
// 3-83h の判定の表の順1（印を持っている機械）を確かめる。
//
// 目的: **入ったあとも毎巡回同じ判定を当てる**（人間の決定）。direct chat の最中に担当者を外すと
// `failure_state` へ落ちてチャットが切れる。**書けるまでの巡回で turn を送らないよう、印も立てる。**
// 与える情報: 印を持つ run。カードを direct chat へ動かし、担当者を0人にする。
// 成功条件: Status が `failure_state` になり、コメントが1件あり、指示を1つも送っていないこと。
// そのあとの巡回で、`failure_state` の既存の出口が pane を閉じること。
func Test_人間がpaneに入って直接続ける_P008_印を持つrunの担当者が0人になったらfailure_stateへ動かし指示を送らない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
		issue := adoptRun(fx, 305) // 担当者は付けない
		fx.Tracker.SetState(issue.ID, humanState)

		fx.Orc.Tick(context.Background())
		synctest.Wait()

		if got, want := fx.Tracker.StateOf(issue.ID), fx.Config.Tracker.FailureState; got != want {
			t.Fatalf("担当者が0人になったのに failure_state へ動かしていない: %q（期待 %q）", got, want)
		}
		if n := commentsContaining(fx.Tracker, "I_node305", assigneesInvalidMarker); n != 1 {
			t.Fatalf("「担当者を1人に」のコメントが1件ではない: %d 件", n)
		}
		if got := fx.Herdr.Prompts(); len(got) != 0 {
			t.Fatalf("担当者が1人でない direct chat の run へ指示を送った: %v", got)
		}

		// **次の巡回で、`failure_state` の既存の出口（3-83g）が pane を閉じる。**
		fx.Orc.Tick(context.Background())
		synctest.Wait()
		if ids := fx.Herdr.ClosedPanes(); len(ids) == 0 {
			t.Fatal("failure_state へ動いたのに pane を閉じていない")
		}
	})
}

// {"RUCM-PATH": "P008"}
//
// Test_人間がpaneに入って直接続ける_P008_選択肢の写しが空なら書く経路は書かない は、3-83e の書く経路の拒否リストを確かめる。
//
// 目的: 拒否リストは「カンバンの選択肢のうち direct chat 以外の全部」で作る。
// **写しが空なら、書かずに WARN を1行出す**（空の拒否リストで書くと、人間が戻した直後のカードを上書きする）。
// 与える情報: 選択肢を1つも読めていない状態で、担当者0人の direct chat の run。
// 成功条件: `UpdateStatus` を1回も呼ばず、Status が direct chat のままであること。
func Test_人間がpaneに入って直接続ける_P008_選択肢の写しが空なら書く経路は書かない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
		fx.Tracker.SetStatusOptions()
		issue := adoptRun(fx, 308) // 担当者0人
		fx.Tracker.SetState(issue.ID, humanState)

		fx.Orc.Tick(context.Background())
		synctest.Wait()

		if n := fx.Tracker.CountCall("UpdateStatus"); n != 0 {
			t.Fatalf("選択肢の写しが空なのに書きに行った: %d 回", n)
		}
		if got := fx.Tracker.StateOf(issue.ID); got != humanState {
			t.Fatalf("選択肢の写しが空なのにカードを動かした: %q", got)
		}
	})
}

// {"RUCM-PATH": "P003"}
//
// Test_人間がpaneに入って直接続ける_P003_コメントを書かせる途中で引き取って戻すと同じpaneで続く は、
// 設計 3-83f の打ち切りを `ensureAgentComment` の段5〜段7 から通して確かめる。
//
// 目的: エージェントが表明を出して終わり、成果のコメントが無いので continuo が段2 で pane を閉じ（止めた印が立つ）、
// 段5 で新しい pane に `--resume` で立て直している最中に、人間がカードを direct chat へ動かす。
// **これがこの機能のいちばん普通の使い方である。**
// 段7 の直前で打ち切り、**人間の pane へ「コメントに書いてください」を送らない。**
// そのあと作業中の Status へ戻したら、**同じ pane で続きの指示が届く。**
// 止めた印が残っていると、戻したときの turn ループが即座に抜けて、指示が1つも届かない。
// 与える情報: `review` を表明するがコメントを書かない run。復元の `agent.start` の最中にカードを direct chat へ動かす。
// 成功条件: 打ち切りのログが出て、指示は1回目の1つだけのまま。作業中へ戻すと、1回目の本文ではない指示が1つ届き、
// pane は段2 の1枚しか閉じていないこと。
func Test_人間がpaneに入って直接続ける_P003_コメントを書かせる途中で引き取って戻すと同じpaneで続く(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	issue := sampleIssue(341, "Ready")
	fx.Tracker.AddIssue(issue)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "CONTINUO-STATUS: review", false),
	})
	var mu sync.Mutex
	var texts []string
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		text, _ := params["text"].(string)
		mu.Lock()
		texts = append(texts, text)
		first := len(texts) == 1
		mu.Unlock()
		if first {
			// **コメントは書かない。**run の終わりで成果のコメントを書かせに行く（設計 3-25）。
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})
	prompts := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), texts...)
	}
	// **2回目の `agent.start`（段5 の立て直し）だけを待たせる。**
	gate := make(chan struct{})
	entered := make(chan struct{})
	var once, enterOnce sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	starts := 0
	fx.Herdr.Handle(herdr.MethodAgentStart, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		starts++
		second := starts == 2
		mu.Unlock()
		if second {
			enterOnce.Do(func() { close(entered) })
			<-gate
		}
		return map[string]any{
			"type":  "agent_started",
			"agent": map[string]any{"name": params["name"], "agent_status": "idle", "interactive_ready": true, "pane_id": params["pane_id"]},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("成果のコメントを書かせるための立て直し（段5 の agent.start）が来ない")
	}
	// ★ 人間が引き取る。巡回が direct chat の印を立てる。
	fx.Tracker.SetState(issue.ID, humanState)
	fx.Orc.Tick(context.Background())
	release()
	waitFor(t, 20*time.Second, "終わらせる処理を打ち切る", func() bool {
		return strings.Contains(fx.Logs.String(), abortedLog)
	})
	if got := prompts(); len(got) != 1 {
		t.Fatalf("人間が引き取った pane へ指示を送った（1回目の指示だけのはず）: %v", got)
	}

	// 人間が continuo へ返す。
	fx.Tracker.SetState(issue.ID, fx.Config.Tracker.RunningState)
	waitFor(t, 20*time.Second, "戻した run へ続きの指示を送る", func() bool {
		fx.Orc.Tick(context.Background())
		return len(prompts()) >= 2
	})
	if got := prompts()[1]; strings.Contains(got, firstPromptMarker) {
		t.Errorf("戻したのに1回目の本文を送った（継続の指示であるべき）: %q", got)
	}
	if n := fx.Herdr.CountMethod(herdr.MethodPaneClose); n != 1 {
		t.Errorf("閉じた pane が段2 の1枚ではない: %d 回", n)
	}
}

// {"RUCM-PATH": "P003"}
//
// Test_人間がpaneに入って直接続ける_P003_turnの終わりに引き取りを見たあと取り直しに失敗した巡回では指示を送らない は、
// 設計 3-83f の `wakeRuns` と turn ループの先頭の行を確かめる。
//
// 目的: turn の終わりがカードを direct chat と読んで送る印を立てたあと、**巡回の取り直しが失敗すると
// direct chat の印は立たない。**送る側が印しか見ないと、カンバンでは Direct Chat のまま
// 人間の pane へ続きの指示が届く。**控えの Status でも見るので送らない。**
// 作業中へ戻したあとは送る（送る印を下ろしていない）。
// 与える情報: 1回目の turn の終わりにカードが direct chat だった run。次の巡回では ID 指定の取り直しが失敗する。
// 成功条件: 取り直しに失敗した巡回で `agent.prompt` が増えないこと。取り直せる巡回でも増えないこと。
// 作業中へ戻した巡回で2回目の指示が届くこと。pane を閉じないこと。
func Test_人間がpaneに入って直接続ける_P003_turnの終わりに引き取りを見たあと取り直しに失敗した巡回では指示を送らない(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	fx.AllowLog("実行中の issue を取り直せません", "worktree の照合で issue を取り直せません")
	issue := turnEndSeesDirectChat(t, fx, 352)
	sent := fx.Herdr.CountMethod(herdr.MethodAgentPrompt)

	fx.Tracker.SetIDsError(errors.New("GraphQL が一時的に失敗しました"))
	fx.Orc.Tick(context.Background())
	assertNoPromptFor(t, fx, sent, "取り直しに失敗した巡回で人間の pane へ指示を送った")

	fx.Tracker.SetIDsError(nil)
	fx.Orc.Tick(context.Background())
	assertNoPromptFor(t, fx, sent, "direct chat へ入れた巡回で指示を送った")

	fx.Tracker.SetState(issue.ID, fx.Config.Tracker.RunningState)
	waitFor(t, 20*time.Second, "戻した run へ2回目の指示を送る", func() bool {
		fx.Orc.Tick(context.Background())
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > sent
	})
	if n := fx.Herdr.CountMethod(herdr.MethodPaneClose); n != 0 {
		t.Errorf("pane を閉じた: %d 回", n)
	}
}

// {"RUCM-PATH": "P003"}
//
// Test_人間がpaneに入って直接続ける_P003_turnの終わりを待つ印はdirectChatを抜けるときに下ろす は、設計 3-83i を確かめる。
//
// 目的: direct chat へ入る前の turn ループが一時的な失敗で立てた「turn の終わりを待つ印」が残ると、
// 戻したあと `wakeRuns` が送る印より先にそれを取り、**指示を送らずに待つだけの turn ループを起こす。**
// 戻しても指示が届かず、約1時間後に stall で打ち切られる。**抜けるときに下ろせば、送る印で続きの指示が届く。**
// 与える情報: 1回目の `agent.prompt` が herdr へ届かずに切れた run（待つ印が立つ）。そのあと direct chat へ動かし、
// 巡回で入れてから作業中へ戻す。
// 成功条件: 戻したあと、1回目の本文ではない続きの指示が届くこと。
func Test_人間がpaneに入って直接続ける_P003_turnの終わりを待つ印はdirectChatを抜けるときに下ろす(t *testing.T) {
	fx := newDirectChatFixture(t, func(cfg *config.Config) { cfg.Tracker.VerifyStatesEvery = 0 })
	fx.AllowLog("herdr へ届かなかったので", "herdr との通信が一時的に失敗した")
	issue := sampleIssue(356, "Ready")
	fx.Tracker.AddIssue(issue)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)
	fx.Herdr.DropConnection(herdr.MethodAgentPrompt)

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "1回目の turn が herdr へ届かず、待つ印が立つ", func() bool {
		return strings.Contains(fx.Logs.String(), "herdr との通信が一時的に失敗した")
	})
	fx.Herdr.StopDropping(herdr.MethodAgentPrompt)
	prompts := recordPrompts(fx)

	fx.Tracker.SetState(issue.ID, humanState)
	fx.Orc.Tick(context.Background())
	if got := prompts(); len(got) != 0 {
		t.Fatalf("direct chat へ入れた巡回で指示を送った: %v", got)
	}

	fx.Tracker.SetState(issue.ID, fx.Config.Tracker.RunningState)
	waitFor(t, 20*time.Second, "戻した run へ続きの指示を送る", func() bool {
		fx.Orc.Tick(context.Background())
		return len(prompts()) >= 1
	})
	if got := prompts()[0]; strings.Contains(got, firstPromptMarker) {
		t.Errorf("戻した run へ1回目の本文を送った（継続の指示であるべき）: %q", got)
	}
}

// {"RUCM-PATH": "P006"}
//
// Test_人間がpaneに入って直接続ける_P006_Doneへ直接抜けたら成果のコメントを書かせに行かない は、設計 3-83g の `Done` の行を確かめる。
//
// 目的: 人間が Claude Code を終了させてから `Done` へ動かすのは、人間が名指しした出口である。
// **書かせに行くと、終了させたものを `--resume` で立て直すことになる。**
// 与える情報: 1回目の turn を送ったあと direct chat へ入れ、コメントを1件も書かずに `Done` へ動かした run。
// 成功条件: 立て直しの `agent.start` も指示も無く、pane を閉じ、印から外すこと。
func Test_人間がpaneに入って直接続ける_P006_Doneへ直接抜けたら成果のコメントを書かせに行かない(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	holdPrompt(fx)
	issue := sampleIssue(345, "Ready")
	fx.Tracker.AddIssue(issue)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)
	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "1回目の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	fx.Tracker.SetState(issue.ID, humanState)
	fx.Orc.Tick(context.Background())
	fx.Tracker.SetState(issue.ID, fx.Config.Tracker.TerminalStates[0])
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 20*time.Second)

	if n := fx.Herdr.CountMethod(herdr.MethodAgentStart); n != 1 {
		t.Fatalf("direct chat から Done へ抜けたのに、成果のコメントを書かせるために立て直した: agent.start %d 回", n)
	}
	if n := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); n != 1 {
		t.Fatalf("direct chat から Done へ抜けたのに指示を送った: agent.prompt %d 回", n)
	}
	if n := fx.Herdr.CountMethod(herdr.MethodPaneClose); n == 0 {
		t.Fatal("Done へ動かしたのに pane を閉じていない")
	}
}

// {"RUCM-PATH": "P007"}
//
// Test_人間がpaneに入って直接続ける_P007_turnの終わりに引き取りを見たあと手を離す巡回では指示を送らない は、
// 設計 3-83h の「手を離す経路」と 3-83f の `wakeRuns` の行を確かめる。
//
// 目的: turn の終わりが送る印を立てたあと、次の巡回で担当者が別の1人に替わっていたら手を離す。
// **手を離す途中（印を外すまで）に、人間の pane へ続きの指示を送らない。**
// 与える情報: 1回目の turn の終わりにカードが direct chat だった run。次の巡回の前に担当者を別の1人に替える。
// 成功条件: `agent.prompt` が増えず、pane を閉じ、印を外すこと。Status を書かないこと。
func Test_人間がpaneに入って直接続ける_P007_turnの終わりに引き取りを見たあと手を離す巡回では指示を送らない(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	fx.AllowLog("この PC の continuo は手を離します")
	issue := turnEndSeesDirectChat(t, fx, 353)
	sent := fx.Herdr.CountMethod(herdr.MethodAgentPrompt)
	state := fx.Tracker.StateOf(issue.ID)

	fx.Tracker.SetAssignees(issue.ID, "someone-else")
	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "手を離して印を外す", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
	assertNoPromptFor(t, fx, sent, "手を離す途中で人間の pane へ指示を送った")
	if n := fx.Herdr.CountMethod(herdr.MethodPaneClose); n == 0 {
		t.Error("手を離したのに pane を閉じていない")
	}
	if got := fx.Tracker.StateOf(issue.ID); got != state {
		t.Errorf("手を離すときにカードを動かした: %q から %q へ", state, got)
	}
}

// {"RUCM-PATH": "P009"}
//
// Test_人間がpaneに入って直接続ける_P009_担当者が自分1人ならpaneを用意し指示は送らない は、設計 3-83c と 3-83d の用意の段1〜段3 を確かめる。
//
// 目的: 印を持たない issue を direct chat へ動かすと、continuo は worktree と pane を用意して Claude Code を
// 起動し、**指示は1文字も送らず、Status も動かさず、issue へ「話しかけられます」を1件書く。**
// そのあと作業中の Status へ戻すと、**`SendFirstPrompt` を下ろしてあるので、送るのは継続の指示（5-4）である。**
// 与える情報: 担当者がこの continuo のアカウント1人で、Status が direct chat の issue。
// 成功条件: `agent.start` が1回、`agent.prompt` が0回、Status は direct chat のまま、案内が1件。
// 戻したあとは、`dispatch_state` から `running_state` へ書き、1回目の本文ではない指示を1回送る。
func Test_人間がpaneに入って直接続ける_P009_担当者が自分1人ならpaneを用意し指示は送らない(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	prompts := recordPrompts(fx)
	id, node := addOwnDirectChatIssue(fx, 320)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "「話しかけられます」の案内が書かれる", func() bool {
		return commentsContaining(fx.Tracker, node, readyCommentMarker) == 1
	})

	if n := fx.Herdr.CountMethod(herdr.MethodAgentStart); n != 1 {
		t.Fatalf("agent.start が1回ではない: %d 回", n)
	}
	if got := prompts(); len(got) != 0 {
		t.Fatalf("direct chat の pane へ指示を送った: %v", got)
	}
	if got := fx.Tracker.StateOf(id); got != humanState {
		t.Fatalf("direct chat の用意で Status を動かした: %q", got)
	}
	if got := fx.Orc.RunningIdentifiers(); len(got) != 1 {
		t.Fatalf("用意した run が印に入っていない: %v", got)
	}

	// 人間が着手待ちへ戻す（continuo へ返す）。
	fx.Tracker.SetState(id, fx.Config.Tracker.DispatchState)
	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "戻した run の Status を running_state へ書く", func() bool {
		return fx.Tracker.StateOf(id) == fx.Config.Tracker.RunningState
	})
	waitFor(t, 10*time.Second, "戻した run へ指示を送る", func() bool {
		fx.Orc.Tick(context.Background())
		return len(prompts()) >= 1
	})
	if got := prompts()[0]; strings.Contains(got, firstPromptMarker) {
		t.Errorf("用意して入った run を戻したのに1回目の本文を送った（継続の指示であるべき）: %q", got)
	}
	if n := fx.Herdr.CountMethod(herdr.MethodPaneClose); n != 0 {
		t.Errorf("戻すときに pane を閉じた: %d 回", n)
	}
}

// {"RUCM-PATH": "P005"}
//
// Test_人間がpaneに入って直接続ける_P005_引き渡しのStatusへ動かすと指示を送らずにpaneを閉じる は、
// 代替フロー「作業中でも完了でもないStatusへ動かされた」を確かめる（設計 3-83g の `In Review` / `Blocked` の行）。
//
// 目的: direct chat から引き渡しの Status へ動かしたとき、direct chat の印を下ろすだけで、続きの指示は送らないこと。
// 印を下ろしたあとは、その Status の通常の扱い（pane を閉じて印を外す）に乗ること。
// 与える情報: direct chat に入れたあと、Status を `In Review` へ動かした run。
// 成功条件: 指示を1つも送らず、pane を閉じ、印を外し、Status を書き換えていないこと。
func Test_人間がpaneに入って直接続ける_P005_引き渡しのStatusへ動かすと指示を送らずにpaneを閉じる(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
		issue := adoptOwnRun(fx, 189)

		fx.Tracker.SetState(issue.ID, humanState)
		fx.Orc.Tick(context.Background())
		synctest.Wait()
		if ids := fx.Herdr.ClosedPanes(); len(ids) != 0 {
			t.Fatalf("direct chat で pane を閉じた: %v", ids)
		}

		fx.Tracker.ResetCalls()
		fx.Tracker.SetState(issue.ID, "In Review")
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		if got := fx.Herdr.Prompts(); len(got) != 0 {
			t.Fatalf("引き渡しの Status へ動かしたのに指示を送った: %v", got)
		}
		if ids := fx.Herdr.ClosedPanes(); len(ids) == 0 {
			t.Fatal("引き渡しの Status へ動かしたのに pane を閉じていない（direct chat の門が開いていない）")
		}
		if _, ok := viewOf(fx, issue.Identifier); ok {
			t.Fatal("引き渡しの Status へ動かしたのに印から外れていない")
		}
		if n := fx.Tracker.CountCall("UpdateStatus"); n != 0 {
			t.Fatalf("direct chat を抜けるときに Status を書いた: %d 回", n)
		}
		if got := fx.Tracker.StateOf(issue.ID); got != "In Review" {
			t.Fatalf("人間が動かした Status を書き換えた: %q", got)
		}
	})
}
