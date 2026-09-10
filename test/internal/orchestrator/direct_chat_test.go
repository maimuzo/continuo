// 人間が pane で直接エージェントと話しているあいだの振る舞いの検査である（設計 3-82）。
//
// **守るのは1つだけである。「direct chat の run に対して `pane.close` を呼ばない」。**
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

// withDirectChatState は `tracker.direct_chat_state` を設定した検査対象を作る。
//
// t: 呼び出し元のテスト。
// 戻り値: 組み立てた stubFixture。
func withDirectChatState(t *testing.T) *stubFixture {
	t.Helper()
	return newStubFixture(t, stubFixtureOptions{
		AgentStatus: herdr.AgentStatusWorking,
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.DirectChatState = humanState
			cfg.Claude.TurnTimeoutMs = int(stallTimeout / time.Millisecond)
		},
	})
}

// TestDirectChatMode_direct chat のあいだは画面が止まっていても打ち切らない は、
// この機能がいちばん守りたいものを確かめる。
//
// 目的: **人間は画面の前で考えるので、画面の版は何時間も変わらない。**
// stall 検知（設計 3-21）はそれを「止まった」と読んで pane を閉じ、
// **人間が話していた画面ごと消す。**direct chat ではその判定を飛ばす。
//
// 与える情報: `tracker.direct_chat_state` を設定し、Status をその値にした run。
// **画面の版は一度も増やさない。**閾値を50回またぐ（50分ぶん）。
// 成功条件: pane が1つも閉じられず、印にも残り、turn も1つも送られていないこと。
//
// **実時間はゼロである。**`testing/synctest` の bubble の中で時計を進める。
func TestDirectChatMode_directChatのあいだは画面が止まっていても打ち切らない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
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

// TestDirectChatMode_作業中のStatusへ戻すと同じpaneへ続きの指示を送る は、戻し方を確かめる。
//
// 目的: 設計 3-82 の「`active_states` へ戻したら、**同じ pane・同じセッションのまま**
// 続きの指示を1回送る」を示す。**pane を閉じて作り直さない。**
//
// 与える情報: direct chat に入れたあと、Status を `In Progress` へ戻した run。
// 成功条件: `agent.prompt` が飛び、その本文が継続の指示（1回目の本文ではない）であり、
// pane が1つも閉じられていないこと。
func TestDirectChatMode_作業中のStatusへ戻すと同じpaneへ続きの指示を送る(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
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
			t.Fatalf("direct chat へ入れた巡回で指示を送った: %v", got)
		}

		// ★ エージェントは応答を書き終えている（人間が切りのいいところで戻す、が前提）。
		fx.Herdr.SetStatus(herdr.AgentStatusIdle)

		// 人間が continuo へ返す。
		fx.Tracker.SetState(issue.ID, fx.Config.Tracker.RunningState)
		// **戻すときの後始末は巡回のループの外で走る**（設計 3-82）。
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

// TestDirectChatMode_エージェントが動いている最中に戻したら指示を送らない は、turn の混ざりを防ぐ。
//
// 目的: **人間が話しかけた直後（エージェントが応答を書いている最中）にカードを戻すのは
// 自然な操作である。**そこへ `agent.prompt` を投げると turn が混ざる。
// 復元の段5a2（設計 3-4）が同じ場面で「送らずに turn の終わりを待つ」と決めているので、
// **direct chat から戻すときも同じ判断にする。**
//
// 与える情報: direct chat に入れたあと、`agent_status` が `working` のまま
// Status を `In Progress` へ戻した run。
// 成功条件: 指示が1つも飛ばないこと（turn の終わりを待つ側へ倒れていること）。
func TestDirectChatMode_エージェントが動いている最中に戻したら指示を送らない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t) // AgentStatus は working のまま
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

// TestDirectChatMode_完了のStatusへ動かすとdirect chatを抜けて片付ける は、抜け方を確かめる。
//
// 目的: **抜ける条件を「`active_states` へ戻ったとき」に絞ってはならない**（設計 3-82）。
// 絞ると `Done` へ動かしたときに印が立ったままになり、`stopWorker` の門が pane を守り続けて
// **worktree も片付かない。**
//
// 与える情報: direct chat に入れたあと、Status を `Done`（`terminal_states`）へ動かした run。
// 成功条件: pane が閉じられ、印からも外れること。
func TestDirectChatMode_完了のStatusへ動かすとdirectChatを抜けて片付ける(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
		issue := adoptRun(fx, 188)

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

// TestDirectChatMode_話している最中の表明でカードを奪われない は、issue #263 が名指しした症状を塞ぐ。
//
// 目的: **人間が pane で話しかけた返事に `CONTINUO-STATUS: blocked` の1行が入っていても、
// direct chat のカードを `Blocked` へ書き換えないこと。**
// 書き換えると Status がdirect chat から外れ、そのまま引き渡しとして pane が閉じる。
// **これが「AIの判断でblockedなどに遷移し、チャットが強制切断される」の中身である。**
//
// **巡回がdirect chatを立てるより先に turn の終わりが来る筋も、同時に確かめている。**
// カードを動かしてから巡回を1回も回さずに `Stop` を流すので、continuo がdirect chatを
// 知るのは `decideAfterTurn` が Status を取り直した時点である。
//
// 与える情報: `direct_chat_state` を設定した fixture。着手して1回目の turn が待ち受けに入った run。
// その最中に人間がカードを `Human` へ動かし、エージェントの応答には `blocked` の表明がある。
// 成功条件: Status が `Human` のまま、pane が1つも閉じず、2回目の指示も飛ばないこと。
func TestDirectChatMode_話している最中の表明でカードを奪われない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Tracker.DirectChatState = humanState },
	})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	releasePrompt := blockFirstPrompt(t, fx)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "1回目の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// ★ 人間が pane に入る前に、カードをdirect chat へ動かした（FAQ の手順1）。
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
	// **書きに行っても、direct chat のカードは動かない**（`protectedStates`）。
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

// TestDirectChatMode_設定していなければいままでどおり止める は、既定の振る舞いを守る。
//
// 目的: **`tracker.direct_chat_state` を書かなければ1つも挙動が変わらないこと。**
// 空のときに `Human` という Status へ動かされたら、それは「知らない Status」であり、
// いままでどおり猶予のあとで worker を止める（設計 3-50）。
//
// 与える情報: `direct_chat_state` を空のままにし、Status を `Human` にした run。
// **猶予は0にする**（turn ループが走っていないので、そもそも猶予は使われない）。
// 成功条件: pane が閉じられること。
func TestDirectChatMode_設定していなければいままでどおり止める(t *testing.T) {
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
			t.Fatal("direct_chat_state を設定していないのに pane を閉じていない（既定の振る舞いが変わっている）")
		}
	})
}

// TestDirectChat_カンバンに選択肢が無ければ候補の一覧へ足さない は、
// 「起動はするのに1件も着手されない」を塞ぐ（設計 3-82）。
//
// 目的: **`FetchIssuesByStates` は、カンバンに無い Status 名を渡されると
// 0件ではなくエラーを返す**（`tracker.verifyKnownStates`）。**そのエラーは候補の取得
// そのものを失敗させ、その巡回の dispatch を丸ごと飛ばす。**
// 既定が `"Direct Chat"` である以上、選択肢をまだ作っていない利用者は
// **起動はできるのに1件も着手されない continuo を手に入れることになる。**
//
// 与える情報: `direct_chat_state` を設定し、カンバンの選択肢にそれを**入れない**巡回と、
// **入れた**巡回。
// 成功条件: 入れていない巡回では `active_states` だけが渡り、入れた巡回では
// `direct_chat_state` も渡ること。
func TestDirectChat_カンバンに選択肢が無ければ候補の一覧へ足さない(t *testing.T) {
	fx := withDirectChatState(t)

	// **選択肢を1つも読めていない状態**（Bootstrap の前）。分からないものは足さない。
	fx.Orc.Tick(context.Background())
	if got := fx.Tracker.LastFetchStates(); containsFoldStr(got, humanState) {
		t.Errorf("選択肢を読めていないのに候補の一覧へ足した: %v", got)
	}

	// **選択肢は読めたが、direct chat の名前が無い。**
	fx.Tracker.SetStatusOptions("Ready", "In Progress", "In Review", "Blocked", "Done")
	fx.Orc.Tick(context.Background())
	got := fx.Tracker.LastFetchStates()
	if containsFoldStr(got, humanState) {
		t.Errorf("カンバンに無い Status を候補の一覧へ足した（この巡回の dispatch が丸ごと落ちる）: %v", got)
	}
	for _, want := range fx.Config.Tracker.ActiveStates {
		if !containsFoldStr(got, want) {
			t.Errorf("active_states の %q が候補の一覧に無い: %v", want, got)
		}
	}

	// **選択肢がある。**ここで初めて足す。
	fx.Tracker.SetStatusOptions("Ready", "In Progress", "In Review", "Blocked", "Done", humanState)
	fx.Orc.Tick(context.Background())
	if got := fx.Tracker.LastFetchStates(); !containsFoldStr(got, humanState) {
		t.Errorf("カンバンにあるのに候補の一覧へ足していない: %v", got)
	}
}

// TestDirectChat_着手待ちへ戻したら作業中のStatusを書く は、設計 3-82 の書き込みを確かめる。
//
// 目的: **direct chat の run は、着手の段2 を1度も通っていない。**
// `dispatch_state`（既定 `Ready`）へ戻されたまま放っておくと、
// **エージェントが走っているのにカードは着手待ちに見え、
// `agent.max_concurrent_agents_by_state` の勘定からも外れる。**
// 同じカンバンを見張る別の機械からは、担当者の付いていない着手待ちの issue に見える。
//
// 与える情報: direct chat に入れたあと、Status を `Ready` へ戻した run。
// 成功条件: カンバンの Status が `In Progress` になり、指示も1回送られること。
func TestDirectChat_着手待ちへ戻したら作業中のStatusを書く(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
		defer fx.Orc.Close()
		issue := adoptRun(fx, 191)

		fx.Tracker.SetState(issue.ID, humanState)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		fx.Herdr.SetStatus(herdr.AgentStatusIdle)
		fx.Tracker.SetState(issue.ID, fx.Config.Tracker.DispatchState)
		// **戻すときの後始末は巡回のループの外で走る**（設計 3-82）。
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

// TestDirectChat_作業中のStatusへ戻したときはStatusを書かない は、無駄な書き込みを防ぐ。
//
// 目的: `running_state` へ戻されたときは、書く先が既にその値なので書かない。
//
// 与える情報: direct chat に入れたあと、Status を `In Progress` へ戻した run。
// 成功条件: `UpdateStatus` が1回も呼ばれていないこと。
func TestDirectChat_作業中のStatusへ戻したときはStatusを書かない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
		defer fx.Orc.Close()
		issue := adoptRun(fx, 192)

		fx.Tracker.SetState(issue.ID, humanState)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		fx.Herdr.SetStatus(herdr.AgentStatusIdle)
		fx.Tracker.ResetCalls()
		fx.Tracker.SetState(issue.ID, fx.Config.Tracker.RunningState)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		for _, name := range fx.Tracker.Calls() {
			if name == "UpdateStatus" {
				t.Errorf("同じ値なのに Status を書きに行った: %v", fx.Tracker.Calls())
				break
			}
		}
	})
}

// containsFoldStr は values に target が（大文字小文字を無視して）入っているかを返す。
func containsFoldStr(values []string, target string) bool {
	for _, v := range values {
		if strings.EqualFold(strings.TrimSpace(v), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}
