// 人間が pane で直接エージェントと話しているあいだの振る舞いの検査である（設計 3-82）。
//
// **守るのは1つだけである。「direct chat の run に対して `pane.close` を呼ばない」。**
// 呼ばれた瞬間に、人間が話していた画面が消える。
package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/tracker"
)

// humanState は、この検査で「人間が引き取っている」を表す Status である。
//
// **`tracker.active_states` にも `terminal_states` にも入っていない名前にする。**
const humanState = "Human"

// directChatBoardOptions はカンバンの Status の選択肢である（`humanState` を含む）。
//
// **書く経路（設計 3-82h）は、この写しから拒否リストを作る。**写しが空なら書かない。
var directChatBoardOptions = []string{"Ready", "In Progress", "In Review", "Blocked", "Done", humanState}

// withDirectChatState は `tracker.direct_chat_state` を設定した検査対象を作る。
//
// **カンバンの選択肢に `humanState` を入れる。**入れないと、候補の一覧へ足されず、
// 書く経路も写しが空として書かない。
//
// t: 呼び出し元のテスト。
// 戻り値: 組み立てた stubFixture。
func withDirectChatState(t *testing.T) *stubFixture {
	t.Helper()
	return withDirectChatStateOn(t, nil)
}

// withDirectChatStateOn は、テスト用トラッカー mock を指定して withDirectChatState と同じものを作る。
//
// **同じカンバンを2台の continuo で見張る場面を作るために使う**（設計 3-82h）。
//
// t: 呼び出し元のテスト。
// ft: 使うトラッカー。nil なら新しく作る。
// 戻り値: 組み立てた stubFixture。
func withDirectChatStateOn(t *testing.T, ft *fakeTracker) *stubFixture {
	t.Helper()
	fx := newStubFixture(t, stubFixtureOptions{
		AgentStatus: herdr.AgentStatusWorking,
		Tracker:     ft,
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.DirectChatState = humanState
			cfg.Claude.TurnTimeoutMs = int(stallTimeout / time.Millisecond)
		},
	})
	fx.Tracker.SetStatusOptions(directChatBoardOptions...)
	return fx
}

// adoptOwnRun は、担当者がこの continuo のアカウント1人の run を印の集合へ入れる（設計 3-82h）。
//
// **direct chat に居られるのは、担当者が自分1人のときだけである。**担当者を付けずに
// direct chat へ動かすと、3-82h の判定の表の順1 に当たり `failure_state` へ落ちる。
//
// fx: 対象の stubFixture。
// number: issue の番号。
// 戻り値: 入れた issue。
func adoptOwnRun(fx *stubFixture, number int) tracker.Issue {
	issue := adoptRun(fx, number)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)
	return issue
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
		issue := adoptOwnRun(fx, 188)
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
		issue := adoptOwnRun(fx, 188)

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
	fx.Tracker.SetStatusOptions()

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
		issue := adoptOwnRun(fx, 191)

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
		issue := adoptOwnRun(fx, 192)

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

// TestDirectChat_カードがdirectChatになった巡回では何もしない は、
// 巡回の分岐を確かめる（設計 3-82）。
//
// 目的: **カードが `direct_chat_state` になった最初の巡回で、pane も印も触らない**ことを示す。
//
// **この検査は、実装レビュー2周目で出た Critical そのものは再現していない。**
// あれは「pane の用意が走っている最中は印を立てない」という印を足していたときにだけ起きた。
// **その印はやめたので、いまは `updateDirectChatMode` が同じ巡回の中で印を立てる。**
// **だから印で判定してもカードの Status で判定しても、この検査は通る。**
// **不変条件（巡回はカードの Status だけを見る）を機械で押さえられてはいない。**
// 押さえるには着手の13段を通す必要があり、この stub には git の worktree が無い。
//
// 与える情報: 実行中の一覧へ入れた run と、`direct_chat_state` になったカード。
// 成功条件: pane が1つも閉じられず、印にも残っていること。
func TestDirectChat_カードがdirectChatになった巡回では何もしない(t *testing.T) {
	fx := withDirectChatState(t)
	issue := adoptOwnRun(fx, 193)

	fx.Tracker.SetState(issue.ID, humanState)

	// **巡回を2回まわす。**1回目で印が立ち、2回目は印が立った状態で通る。
	for range 2 {
		fx.Orc.Tick(context.Background())
	}

	if ids := fx.Herdr.ClosedPanes(); len(ids) != 0 {
		t.Errorf("カードが direct chat なのに pane を閉じた: %v", ids)
	}
	if _, ok := viewOf(fx, issue.Identifier); !ok {
		t.Errorf("カードが direct chat なのに印から外れた（実行中: %v）", fx.Orc.RunningIdentifiers())
	}
}

// TestDirectChat_段2の拒否リストにdirectChatのStatusが入っている は、
// 着手の最後の砦を確かめる（設計 3-82）。
//
// 目的: **`dispatchBlockedStates` は拒否リストであって「`active_states` の外を全部」ではない。**
// **足さないと、人間が着手の隙間にカードを direct chat へ動かしたとき、
// 段2 が `running_state` で上書きする。**
//
// 与える情報: `direct_chat_state` を設定した設定。
// 成功条件: `dispatchBlockedStates` の戻りに direct chat の Status が入っていること。
func TestDirectChat_段2の拒否リストにdirectChatのStatusが入っている(t *testing.T) {
	fx := withDirectChatState(t)
	got := fx.Orc.DispatchBlockedStatesForTest()
	if !containsFoldStr(got, humanState) {
		t.Errorf("段2 の拒否リストに direct chat の Status が入っていない: %v", got)
	}
}

// assigneesInvalidMarker は、担当者が1人でないときに issue へ書くコメントにだけ出る文字列である（設計 3-82h）。
const assigneesInvalidMarker = "担当者を1人だけにしてください"

// commentsContaining は、issue のコメントのうち本文に substr を含むものの数を返す。
//
// fx: 対象の stubFixture。
// nodeID: issue のノード ID。
// substr: 探す文字列。
// 戻り値: 見つかった件数。
func commentsContaining(ft *fakeTracker, nodeID, substr string) int {
	n := 0
	for _, c := range ft.CommentsOf(nodeID) {
		if strings.Contains(c.Body, substr) {
			n++
		}
	}
	return n
}

// TestDirectChat_担当者が0人の候補はfailure_stateへ動かしてコメントを1件書く は、
// 3-82h の判定の表の順1（印を持っていない機械）を確かめる。
//
// 目的: 人間の決定「担当者が1人だけの状態以外で direct chat に移したら、エラーとして blocked に遷移して良い。
// その際、担当者を1人だけ設定する旨をコメントに書いておいて」を示す。
//
// 与える情報: Status が direct chat で担当者が0人の候補（continuo は印を持っていない）。
// 成功条件: Status が `failure_state` になり、「担当者を1人に」のコメントがちょうど1件あり、
// 印を1つも付けていない（pane を用意しにいかない）こと。
func TestDirectChat_担当者が0人の候補はfailure_stateへ動かしてコメントを1件書く(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
		issue := sampleIssue(301, humanState)
		fx.Tracker.AddIssue(issue)

		fx.Orc.Tick(context.Background())
		synctest.Wait()

		if got, want := fx.Tracker.StateOf(issue.ID), fx.Config.Tracker.FailureState; got != want {
			t.Fatalf("担当者が0人なのに failure_state へ動かしていない: %q（期待 %q）", got, want)
		}
		if n := commentsContaining(fx.Tracker, "I_node301", assigneesInvalidMarker); n != 1 {
			t.Fatalf("「担当者を1人に」のコメントが1件ではない: %d 件", n)
		}
		if got := fx.Orc.RunningIdentifiers(); len(got) != 0 {
			t.Fatalf("担当者が0人なのに印を付けた（pane を用意しにいった）: %v", got)
		}
	})
}

// TestDirectChat_担当者が2人の候補もfailure_stateへ動かす は、3-82h の判定の表の順1（2人以上）を確かめる。
//
// 目的: 「複数人は NG」（人間の決定）を示す。自分が含まれていても NG である。
// 与える情報: Status が direct chat で担当者が2人（自分と他人）の候補。
// 成功条件: Status が `failure_state` になり、コメントが1件あること。
func TestDirectChat_担当者が2人の候補もfailure_stateへ動かす(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
		issue := sampleIssue(302, humanState)
		fx.Tracker.AddIssue(issue)
		fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin, "someone-else")

		fx.Orc.Tick(context.Background())
		synctest.Wait()

		if got, want := fx.Tracker.StateOf(issue.ID), fx.Config.Tracker.FailureState; got != want {
			t.Fatalf("担当者が2人なのに failure_state へ動かしていない: %q（期待 %q）", got, want)
		}
		if n := commentsContaining(fx.Tracker, "I_node302", assigneesInvalidMarker); n != 1 {
			t.Fatalf("「担当者を1人に」のコメントが1件ではない: %d 件", n)
		}
	})
}

// TestDirectChat_担当者が1人で他人の候補には何もしない は、3-82h の判定の表の順3（印を持っていない機械）を確かめる。
//
// 目的: どの機械が pane を持つかは担当者だけで決まる。**他人のアカウントが担当なら、この機械は何もしない。**
// 与える情報: Status が direct chat で担当者が1人（他人）の候補。
// 成功条件: Status が動かず、コメントも書かず、印も付けないこと。
func TestDirectChat_担当者が1人で他人の候補には何もしない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
		issue := sampleIssue(303, humanState)
		fx.Tracker.AddIssue(issue)
		fx.Tracker.SetAssignees(issue.ID, "someone-else")

		fx.Orc.Tick(context.Background())
		synctest.Wait()

		if got := fx.Tracker.StateOf(issue.ID); got != humanState {
			t.Fatalf("他人が担当している direct chat のカードを動かした: %q", got)
		}
		if n := fx.Tracker.CountCall("UpdateStatus"); n != 0 {
			t.Fatalf("他人が担当している direct chat のカードへ書きに行った: %d 回", n)
		}
		if got := fx.Orc.RunningIdentifiers(); len(got) != 0 {
			t.Fatalf("他人が担当している issue に印を付けた: %v", got)
		}
		if n := len(fx.Tracker.CommentsOf("I_node303")); n != 0 {
			t.Fatalf("他人が担当している issue にコメントを書いた: %d 件", n)
		}
	})
}

// TestDirectChat_ログイン名が取れない巡回では候補に何もしない は、3-82h の判定の表の順2 を確かめる。
//
// 目的: 自分が誰か分からないまま pane を用意しない（印を持っていない機械は、この巡回では何もしない）。
// 与える情報: gh の持ち主を取れない状態で、担当者1人の direct chat の候補。
// 成功条件: 印を付けず、Status も動かさないこと。
func TestDirectChat_ログイン名が取れない巡回では候補に何もしない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
		fx.Tracker.SetViewerError(errors.New("gh が認証されていません"))
		issue := sampleIssue(304, humanState)
		fx.Tracker.AddIssue(issue)
		fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)

		fx.Orc.Tick(context.Background())
		synctest.Wait()

		if got := fx.Orc.RunningIdentifiers(); len(got) != 0 {
			t.Fatalf("自分が誰か分からないのに印を付けた: %v", got)
		}
		if got := fx.Tracker.StateOf(issue.ID); got != humanState {
			t.Fatalf("自分が誰か分からないのにカードを動かした: %q", got)
		}
	})
}

// TestDirectChat_印を持つrunの担当者が0人になったらfailure_stateへ動かし指示を送らない は、
// 3-82h の判定の表の順1（印を持っている機械）を確かめる。
//
// 目的: **入ったあとも毎巡回同じ判定を当てる**（人間の決定）。direct chat の最中に担当者を外すと
// `failure_state` へ落ちてチャットが切れる。**書けるまでの巡回で turn を送らないよう、印も立てる。**
// 与える情報: 印を持つ run。カードを direct chat へ動かし、担当者を0人にする。
// 成功条件: Status が `failure_state` になり、コメントが1件あり、指示を1つも送っていないこと。
// そのあとの巡回で、`failure_state` の既存の出口が pane を閉じること。
func TestDirectChat_印を持つrunの担当者が0人になったらfailure_stateへ動かし指示を送らない(t *testing.T) {
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

		// **次の巡回で、`failure_state` の既存の出口（3-82g）が pane を閉じる。**
		fx.Orc.Tick(context.Background())
		synctest.Wait()
		if ids := fx.Herdr.ClosedPanes(); len(ids) == 0 {
			t.Fatal("failure_state へ動いたのに pane を閉じていない")
		}
	})
}

// TestDirectChat_印を持つrunの担当者が他人に替わったら手を離す は、3-82h の「手を離す経路」を確かめる。
//
// 目的: 担当者が別の1人に替わったら、印を持つ機械は pane を閉じ、印を外す。
// **Status を書かず、コメントも書かない。**新しい担当者の機械が pane を用意する。
// 与える情報: direct chat に入った run。担当者を他人1人に替える。
// 成功条件: pane が閉じ、印から外れ、`UpdateStatus` を1回も呼ばず、コメントも無いこと。
func TestDirectChat_印を持つrunの担当者が他人に替わったら手を離す(t *testing.T) {
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

// TestDirectChat_印を持つrunはログイン名が取れなくてもdirect_chatへ入れる は、3-82h の順2（印を持っている機械）を確かめる。
//
// 目的: **判定できないあいだは turn を送らない側へ倒す。**
// 与える情報: 印を持つ run。gh の持ち主を取れない状態でカードを direct chat へ動かす。
// 成功条件: pane を閉じず、印も外さず、Status も書かないこと。
func TestDirectChat_印を持つrunはログイン名が取れなくてもdirectChatへ入れる(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
		fx.Tracker.SetViewerError(errors.New("gh が認証されていません"))
		issue := adoptOwnRun(fx, 307)
		fx.Tracker.SetState(issue.ID, humanState)

		fx.Orc.Tick(context.Background())
		synctest.Wait()

		if ids := fx.Herdr.ClosedPanes(); len(ids) != 0 {
			t.Fatalf("自分が誰か分からないだけで pane を閉じた: %v", ids)
		}
		if _, ok := viewOf(fx, issue.Identifier); !ok {
			t.Fatal("自分が誰か分からないだけで印を外した")
		}
		if n := fx.Tracker.CountCall("UpdateStatus"); n != 0 {
			t.Fatalf("自分が誰か分からないだけで Status を書いた: %d 回", n)
		}
	})
}

// TestDirectChat_選択肢の写しが空なら書く経路は書かない は、3-82e の書く経路の拒否リストを確かめる。
//
// 目的: 拒否リストは「カンバンの選択肢のうち direct chat 以外の全部」で作る。
// **写しが空なら、書かずに WARN を1行出す**（空の拒否リストで書くと、人間が戻した直後のカードを上書きする）。
// 与える情報: 選択肢を1つも読めていない状態で、担当者0人の direct chat の run。
// 成功条件: `UpdateStatus` を1回も呼ばず、Status が direct chat のままであること。
func TestDirectChat_選択肢の写しが空なら書く経路は書かない(t *testing.T) {
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

// TestDirectChat_2台が同時に書いてもコメントは実際に書いた1台だけ は、3-82h の「`Wrote` のときだけコメント」を確かめる。
//
// 目的: 見張っている全台が書こうとするが、実際に書けるのは取り直しの時点で先に書いた1台である。
// **`Reached`（既にその値だった）や、取り直すと direct chat でなかったときにはコメントを書かない。**
// 与える情報: 同じカンバンを見張る2台。担当者0人の direct chat の候補。1台目の書き込みを止めておき、
// そのあいだに2台目が書き、1台目を進ませる。
// 成功条件: Status が `failure_state` になり、「担当者を1人に」のコメントがちょうど1件であること。
func TestDirectChat_2台が同時に書いてもコメントは実際に書いた1台だけ(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := withDirectChatState(t)
		b := withDirectChatStateOn(t, a.Tracker)
		issue := sampleIssue(309, humanState)
		a.Tracker.AddIssue(issue)

		release, entered := a.Tracker.HoldUpdate()
		a.Orc.Tick(context.Background())
		<-entered
		b.Orc.Tick(context.Background())
		synctest.Wait()
		release()
		synctest.Wait()

		if got, want := a.Tracker.StateOf(issue.ID), a.Config.Tracker.FailureState; got != want {
			t.Fatalf("failure_state へ動いていない: %q（期待 %q）", got, want)
		}
		if n := commentsContaining(a.Tracker, "I_node309", assigneesInvalidMarker); n != 1 {
			t.Fatalf("実際に書いた1台だけがコメントするはずが %d 件ある", n)
		}
	})
}

// TestDirectChat_未設定のStatusへは書く経路は書かない は、3-82e の「取り直した値が未設定なら書かない」を確かめる。
//
// 目的: 人間が Status を外した item に `Blocked` を付けない。
// 与える情報: 担当者0人の direct chat の候補。書き込みを止めているあいだに人間が Status を外す。
// 成功条件: Status が未設定のままで、コメントも書かないこと。
func TestDirectChat_未設定のStatusへは書く経路は書かない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
		issue := sampleIssue(310, humanState)
		fx.Tracker.AddIssue(issue)

		release, entered := fx.Tracker.HoldUpdate()
		fx.Orc.Tick(context.Background())
		<-entered
		fx.Tracker.SetState(issue.ID, "")
		release()
		synctest.Wait()

		if got := fx.Tracker.StateOf(issue.ID); got != "" {
			t.Fatalf("Status を外した item に書いた: %q", got)
		}
		if n := commentsContaining(fx.Tracker, "I_node310", assigneesInvalidMarker); n != 0 {
			t.Fatalf("書いていないのにコメントを書いた: %d 件", n)
		}
	})
}

// TestDirectChat_戻したときにholdを書く は、3-82h の「戻したときに hold を書く」を確かめる。
//
// 目的: 用意した run には hold が1件も無い。**書かないと、別の機械からは「人間が付けた担当者」に見え、
// 「担当者を外してください」という案内を公開の issue へ投稿する。**
// 与える情報: direct chat に入った run を、作業中の Status へ戻す。
// 成功条件: hold の印で始まるコメントが1件あり、JSON に自分のログイン名が入り、
// 人間向けの文が direct chat から戻したことを言っていること。
func TestDirectChat_戻したときにholdを書く(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := withDirectChatState(t)
		defer fx.Orc.Close()
		issue := adoptOwnRun(fx, 311)
		fx.Tracker.SetState(issue.ID, humanState)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		fx.Herdr.SetStatus(herdr.AgentStatusIdle)
		fx.Tracker.SetState(issue.ID, fx.Config.Tracker.RunningState)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		holds := fx.Tracker.MarkedHandoffCommentsOf("I_node311", config.HandoffHoldMarker)
		if len(holds) != 1 {
			t.Fatalf("戻したときの hold が1件ではない: %d 件", len(holds))
		}
		body := holds[0].Body
		if !strings.Contains(body, `"assignee":"`+fakeViewerLogin+`"`) {
			t.Errorf("hold の JSON に自分のログイン名が無い（hold として数えられない）: %q", body)
		}
		if !strings.Contains(body, "direct chat") {
			t.Errorf("hold の人間向けの文が direct chat から戻したことを言っていない: %q", body)
		}
	})
}

// TestDirectChat_打ち切りはClaude_Codeが起動済みのpaneなら印を残す は、3-82f の打ち切りの1通り目を確かめる。
//
// 目的: 終わらせる処理の最中に人間が direct chat へ引き取ったとき、**その pane で `agent.start` が
// 済んでいるなら、終わらせる処理をやめて印を残す。**人間はその pane で話せる。
// 与える情報: 引き取った pane を持つ run（`agent.start` 済み）。
// 成功条件: 打ち切り、pane を閉じず、印も残ること。
func TestDirectChat_打ち切りはClaudeCodeが起動済みのpaneなら印を残す(t *testing.T) {
	fx := withDirectChatState(t)
	issue := adoptOwnRun(fx, 312)

	aborted, ok := fx.Orc.AbortTerminalForHumanForTest(context.Background(), issue.ID, "w1:p1", true)
	if !ok || !aborted {
		t.Fatalf("direct chat の run で終わらせる処理を打ち切らなかった: aborted=%v ok=%v", aborted, ok)
	}
	if ids := fx.Herdr.ClosedPanes(); len(ids) != 0 {
		t.Fatalf("Claude Code が起動済みの pane を閉じた: %v", ids)
	}
	if _, ok := viewOf(fx, issue.Identifier); !ok {
		t.Fatal("Claude Code が起動済みの pane を持つのに印を外した")
	}
}

// TestDirectChat_打ち切りはagent_startが済んでいないpaneなら閉じて印を外す は、3-82f の打ち切りの2通り目を確かめる。
//
// 目的: `PaneID` が立っていても、その pane で `agent.start` が済んでいなければ Claude Code は居ない
// （continuo が開いたばかりのシェル）。**自分で開いた pane を ID で閉じ、印を外す。**
// **印を残すと、pane の無い印になり誰も気づかない**（3-82j）。
// 与える情報: `agent.start` が済んでいない pane を持つ run。
// 成功条件: 打ち切り、その pane を閉じ、印を外すこと。
func TestDirectChat_打ち切りはagentStartが済んでいないpaneなら閉じて印を外す(t *testing.T) {
	fx := withDirectChatState(t)
	issue := adoptOwnRun(fx, 313)

	aborted, ok := fx.Orc.AbortTerminalForHumanForTest(context.Background(), issue.ID, "w1:p9", false)
	if !ok || !aborted {
		t.Fatalf("direct chat の run で終わらせる処理を打ち切らなかった: aborted=%v ok=%v", aborted, ok)
	}
	if ids := fx.Herdr.ClosedPanes(); len(ids) != 1 || ids[0] != "w1:p9" {
		t.Fatalf("agent.start が済んでいない pane を閉じていない: %v", ids)
	}
	if _, ok := viewOf(fx, issue.Identifier); ok {
		t.Fatal("pane の無い印を残した")
	}
}

// TestDirectChat_打ち切りはpaneが既に閉じていれば印を外すだけ は、3-82f の打ち切りの2通り目（`PaneID` が空）を確かめる。
//
// 目的: この処理の `stopWorker` が閉じたあとで当たったら、**閉じる相手は居ないので印を外すだけにする。**
// 与える情報: `PaneID` が空の run。
// 成功条件: 打ち切り、pane を1枚も閉じず、印を外すこと。
func TestDirectChat_打ち切りはpaneが既に閉じていれば印を外すだけ(t *testing.T) {
	fx := withDirectChatState(t)
	issue := adoptOwnRun(fx, 314)

	aborted, ok := fx.Orc.AbortTerminalForHumanForTest(context.Background(), issue.ID, "", false)
	if !ok || !aborted {
		t.Fatalf("direct chat の run で終わらせる処理を打ち切らなかった: aborted=%v ok=%v", aborted, ok)
	}
	if ids := fx.Herdr.ClosedPanes(); len(ids) != 0 {
		t.Fatalf("閉じる相手が居ないのに pane を閉じた: %v", ids)
	}
	if _, ok := viewOf(fx, issue.Identifier); ok {
		t.Fatal("pane の無い印を残した")
	}
}
