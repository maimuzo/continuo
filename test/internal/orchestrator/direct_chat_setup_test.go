// direct chat の pane の用意（設計 3-83c / 3-83d）・再起動（3-83j）・閉じる集合（3-83f）の検査である。
//
// **本物の git で worktree を作る。**用意の段2 は着手の段3〜段10 を踏むので、
// 通信をしない stub（stub_test.go）では worktree を用意できない。
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
	"github.com/maimuzo/continuo/internal/tracker"
)

// readyCommentMarker は、direct chat の pane を用意したときに issue へ書く案内にだけ出る文字列である（設計 3-83d）。
const readyCommentMarker = "pane を用意しました"

// firstPromptMarker は、1回目の本文（5-3）にだけ出る文字列である（samplePromptTemplate の書き出し）。
const firstPromptMarker = "を実装してください"

// newDirectChatFixture は `tracker.direct_chat_state` を設定し、カンバンの選択肢にそれを入れた fixture を作る。
//
// t: 呼び出し元のテスト。
// mutate: 設定をさらに書き換える関数（nil でよい）。
// 戻り値: 組み立てた fixture。
func newDirectChatFixture(t *testing.T, mutate func(cfg *config.Config)) *fixture {
	t.Helper()
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.DirectChatState = humanState
			if mutate != nil {
				mutate(cfg)
			}
		},
	})
	fx.Tracker.SetStatusOptions(directChatBoardOptions...)
	return fx
}

// addOwnDirectChatIssue は、担当者がこの continuo のアカウント1人で、Status が direct chat の issue を足す。
//
// fx: fixture。
// number: issue の番号。
// 戻り値: project item の ID と、issue のノード ID。
func addOwnDirectChatIssue(fx *fixture, number int) (string, string) {
	issue := sampleIssue(number, humanState)
	fx.Tracker.AddIssue(issue)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)
	return issue.ID, nodeIDOfIssue(issue)
}

// holdAgentStart は、`agent.start` を返り値の関数を呼ぶまで返さないようにする。
//
// **用意の段2 の最中にカードや担当者を動かす場面を作るためにある**（設計 3-83d の用意の段3）。
//
// fx: fixture。
// 戻り値の1つ目: 待たせている `agent.start` を進ませる関数。
// 戻り値の2つ目: `agent.start` が届いたら閉じるチャネル。
func holdAgentStart(t *testing.T, fx *fixture) (func(), <-chan struct{}) {
	t.Helper()
	gate := make(chan struct{})
	entered := make(chan struct{})
	var once, enterOnce sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	fx.Herdr.Handle(herdr.MethodAgentStart, func(params map[string]any) (any, *rpcErr) {
		enterOnce.Do(func() { close(entered) })
		<-gate
		return map[string]any{
			"type":  "agent_started",
			"agent": map[string]any{"name": params["name"], "agent_status": "idle", "interactive_ready": true, "pane_id": params["pane_id"]},
		}, nil
	})
	return release, entered
}

// TestDirectChat_担当者が自分1人ならpaneを用意し指示は送らない は、設計 3-83c と 3-83d の用意の段1〜段3 を確かめる。
//
// 目的: 印を持たない issue を direct chat へ動かすと、continuo は worktree と pane を用意して Claude Code を
// 起動し、**指示は1文字も送らず、Status も動かさず、issue へ「話しかけられます」を1件書く。**
// そのあと作業中の Status へ戻すと、**`SendFirstPrompt` を下ろしてあるので、送るのは継続の指示（5-4）である。**
// 与える情報: 担当者がこの continuo のアカウント1人で、Status が direct chat の issue。
// 成功条件: `agent.start` が1回、`agent.prompt` が0回、Status は direct chat のまま、案内が1件。
// 戻したあとは、`dispatch_state` から `running_state` へ書き、1回目の本文ではない指示を1回送る。
func TestDirectChat_担当者が自分1人ならpaneを用意し指示は送らない(t *testing.T) {
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

// TestDirectChat_用意中に作業中へ戻されたら1回目の本文を送る は、設計 3-83d の外れ方の表の1行目を確かめる。
//
// 目的: 用意は最大2分かかり、巡回は30秒である。その間に人間がカードを戻すことがある。
// **確かめずに direct chat へ入れると、1回目の本文を1度も受け取っていないエージェントへ継続の指示だけが届く。**
// 与える情報: `agent.start` の最中にカードを `dispatch_state` へ戻した用意。
// 成功条件: 案内のコメントを書かず、`running_state` を書き、1回目の本文を送ること。
func TestDirectChat_用意中に作業中へ戻されたら1回目の本文を送る(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	release, entered := holdAgentStart(t, fx)
	prompts := recordPrompts(fx)
	id, node := addOwnDirectChatIssue(fx, 321)

	fx.Orc.Tick(context.Background())
	<-entered
	fx.Tracker.SetState(id, fx.Config.Tracker.DispatchState)
	release()

	waitFor(t, 10*time.Second, "戻された run の Status を running_state へ書く", func() bool {
		return fx.Tracker.StateOf(id) == fx.Config.Tracker.RunningState
	})
	waitFor(t, 10*time.Second, "1回目の指示を送る", func() bool {
		fx.Orc.Tick(context.Background())
		return len(prompts()) >= 1
	})
	if got := prompts()[0]; !strings.Contains(got, firstPromptMarker) {
		t.Errorf("用意中に戻された run へ1回目の本文を送っていない: %q", got)
	}
	if n := commentsContaining(fx.Tracker, node, readyCommentMarker); n != 0 {
		t.Errorf("カードはもう direct chat に無いのに「話しかけられます」を書いた: %d 件", n)
	}
}

// TestDirectChat_用意中に担当者が替わったら自分で開いたpaneを閉じて印を外す は、設計 3-83d の外れ方の表の3行目を確かめる。
//
// 目的: 用意の最中に担当者が別の1人に替わったら、その machine は pane を持ってはならない（3-83h）。
// **自分で開いた pane を ID で閉じ、印を外す。**失敗としては数えず、コメントも書かない。
// 与える情報: `agent.start` の最中に担当者を他人1人に替えた用意。
// 成功条件: pane を閉じ、印を外し、案内のコメントも Status の書き込みも無いこと。
func TestDirectChat_用意中に担当者が替わったら自分で開いたpaneを閉じて印を外す(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	release, entered := holdAgentStart(t, fx)
	id, node := addOwnDirectChatIssue(fx, 322)

	fx.Orc.Tick(context.Background())
	<-entered
	fx.Tracker.SetAssignees(id, "someone-else")
	release()

	waitFor(t, 10*time.Second, "自分で開いた pane を閉じる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodPaneClose) == 1
	})
	waitFor(t, 5*time.Second, "印を外す", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
	if n := commentsContaining(fx.Tracker, node, readyCommentMarker); n != 0 {
		t.Errorf("担当者が替わったのに「話しかけられます」を書いた: %d 件", n)
	}
	if got := fx.Tracker.StateOf(id); got != humanState {
		t.Errorf("担当者が替わっただけでカードを動かした: %q", got)
	}
}

// TestDirectChat_用意の失敗が上限を超えたらfailure_stateへ動かして理由を書く は、設計 3-83d の用意の段2 を確かめる。
//
// 目的: 用意が落ちたら、カンバンへは書かず、自分で開いた pane を閉じ、印を外す。
// **通常の着手と同じ回数の上限（`agent.max_retries`）を超えたら、書く経路で `failure_state` を書き、
// 落ちた理由をコメントする**（人間が了承した形）。担当者を直せとは書かない。
// **比べ方は通常の着手と同じ「超えたら」なので、`agent.max_retries: 0` なら1回目の失敗で書く。**
// **書くのは次の巡回の門7 である**（用意の段2 は数えるだけ。書く場所を1つにして重ならないようにする）。
// 与える情報: `agent.max_retries` を0にし、`agent.start` が必ず失敗する用意。
// 成功条件: 落ちた巡回ではカードを動かさない。次の巡回で pane を閉じ、印を外し、Status が `failure_state` になり、
// 「用意できませんでした」が1件。
func TestDirectChat_用意の失敗が上限を超えたらfailure_stateへ動かして理由を書く(t *testing.T) {
	fx := newDirectChatFixture(t, func(cfg *config.Config) { cfg.Agent.MaxRetries = 0 })
	fx.AllowLog("direct chat の pane を用意できませんでした", "起動できません")
	fx.Herdr.Handle(herdr.MethodAgentStart, func(map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "agent_start_failed", Message: "起動できませんでした"}
	})
	id, node := addOwnDirectChatIssue(fx, 323)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "1回目の用意が落ちて印が外れる", func() bool {
		return strings.Contains(fx.Logs.String(), "direct chat の pane を用意できませんでした") &&
			len(fx.Orc.RunningIdentifiers()) == 0
	})
	if got := fx.Tracker.StateOf(id); got != humanState {
		t.Fatalf("用意の段2 が自分でカードを動かした（書くのは次の巡回の門7 だけのはず）: %q", got)
	}

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "上限を超えて failure_state へ動かす", func() bool {
		return fx.Tracker.StateOf(id) == fx.Config.Tracker.FailureState
	})
	waitFor(t, 5*time.Second, "理由のコメントを書く", func() bool {
		return commentsContaining(fx.Tracker, node, "用意できませんでした") == 1
	})
	if n := commentsContaining(fx.Tracker, node, assigneesInvalidMarker); n != 0 {
		t.Errorf("担当者に問題が無いのに「担当者を1人に」と書いた: %d 件", n)
	}
	if got := fx.Orc.RunningIdentifiers(); len(got) != 0 {
		t.Errorf("用意に失敗した run の印を外していない: %v", got)
	}
	if n := fx.Herdr.CountMethod(herdr.MethodPaneClose); n == 0 {
		t.Error("用意に失敗したのに自分で開いた pane を閉じていない")
	}
}

// TestDirectChat_用意が落ちた直後の巡回ではやり直さない は、設計 3-83c の門7 を確かめる。
//
// 目的: 用意が落ちたら、次に試すまで通常の着手のバックオフと同じ間隔を空ける。
// **空けないと、30秒ごとに枠を取っては落ちるのを繰り返す。**
// 与える情報: `agent.start` が必ず失敗する用意。上限は既定（3回）。
// 成功条件: 1回落ちたあとの巡回で `agent.start` を投げ直さず、Status も動かさないこと。
func TestDirectChat_用意が落ちた直後の巡回ではやり直さない(t *testing.T) {
	fx := newDirectChatFixture(t, func(cfg *config.Config) { cfg.Agent.MaxRetryBackoffMs = 600000 })
	fx.AllowLog("direct chat の pane を用意できませんでした", "起動できません")
	fx.Herdr.Handle(herdr.MethodAgentStart, func(map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "agent_start_failed", Message: "起動できませんでした"}
	})
	id, _ := addOwnDirectChatIssue(fx, 324)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "1回目の用意が落ちて印が外れる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentStart) >= 1 && len(fx.Orc.RunningIdentifiers()) == 0
	})
	starts := fx.Herdr.CountMethod(herdr.MethodAgentStart)

	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 5*time.Second)
	if n := fx.Herdr.CountMethod(herdr.MethodAgentStart); n != starts {
		t.Fatalf("用意が落ちた直後の巡回でやり直した: agent.start が %d 回から %d 回へ", starts, n)
	}
	if got := fx.Tracker.StateOf(id); got != humanState {
		t.Fatalf("上限に達していないのにカードを動かした: %q", got)
	}
}

// TestDirectChat_バックオフ明けに人間が引き取っていたら書いた担当者を消し戻さない は、
// 設計 3-83c の門1 の表の下と 3-77g の例外を確かめる。
//
// 目的: バックオフを挟んだやり直し（`redispatch`）で、着手の段2 が取り直した Status が direct chat なら、
// **書いた担当者を消し戻さない。**消すと担当者が0人になり、次の巡回で `failure_state` へ落ち、
// 「担当者を1人に」という事実と違うコメントが残る。**released のコメントも書かない。**
// 与える情報: 入札して担当者になったあと stall で打ち切られ、バックオフ中の run。その間に人間が
// カードを direct chat へ動かす。
// 成功条件: 担当者が自分のまま残り、released のコメントが無いこと。
// 印が外れ、次の巡回の direct chat の1パスが pane を用意し直すこと（「話しかけられます」が1件）。
func TestDirectChat_バックオフ明けに人間が引き取っていたら書いた担当者を消し戻さない(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{
		Now: clock.Now,
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.DirectChatState = humanState
			cfg.Agent.MaxRetryBackoffMs = 10000
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	fx.Tracker.SetStatusOptions(directChatBoardOptions...)
	issue := sampleIssue(325, "Ready")
	fx.Tracker.AddIssue(issue)

	// 1回目: 入札して担当者になり、待ち受けは返るが `Stop` が来ないので stall として打ち切られる。
	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "1回目の turn のあとリトライが積まれる", func() bool {
		v, ok := viewOfFixture(fx, issue.Identifier)
		return ok && v.RetryCount == 1
	})
	if got, _ := fx.Tracker.IssueByID(issue.ID); len(got.Assignees) != 1 || got.Assignees[0].Login != fakeViewerLogin {
		t.Fatalf("前提が崩れている（入札で担当者になっていない）: %+v", got.Assignees)
	}

	// バックオフのあいだに人間が direct chat へ引き取る。
	fx.Tracker.SetState(issue.ID, humanState)
	clock.Advance(30 * time.Second)
	// **着手の段2 で印が外れ、次の巡回の direct chat の1パスが pane を用意し直す**（設計 3-83c の門1 の表の段4〜段5）。
	// **巡回は待ちの中で回す。**`redispatch` の段2 は別の goroutine なので、印が外れるのが
	// 同じ巡回の direct chat の1パスより後になりうる（1回だけ回すと、`-race -cpu 1,2 -count=10` で20回中10回落ちた）。
	waitFor(t, 10*time.Second, "direct chat の pane が用意される", func() bool {
		fx.Orc.Tick(context.Background())
		return commentsContaining(fx.Tracker, nodeIDOfIssue(issue), readyCommentMarker) == 1
	})

	got, _ := fx.Tracker.IssueByID(issue.ID)
	if len(got.Assignees) != 1 || got.Assignees[0].Login != fakeViewerLogin {
		t.Fatalf("人間が direct chat へ引き取ったのに、書いた担当者を消し戻した: %+v", got.Assignees)
	}
	if n := len(fx.Tracker.MarkedHandoffCommentsOf(nodeIDOfIssue(issue), config.HandoffReleasedMarker)); n != 0 {
		t.Fatalf("担当者を消していないのに released を書いた: %d 件", n)
	}
	if st := fx.Tracker.StateOf(issue.ID); st != humanState {
		t.Fatalf("人間が置いた direct chat のカードを動かした: %q", st)
	}
}

// TestDirectChat_再起動でdirect_chatのrunを引き取り指示を送らない は、設計 3-4 の段5a と 3-83j の1行目を確かめる。
//
// 目的: herdr が再起動前のセッションを resume したとき、復元は direct chat の run を引き取る（印にも入れる）。
// **引き取ったあとも、指示は1文字も送らない。**
// 与える情報: Status が direct chat で、pane が生きていて agent 名を持つ run。
// 成功条件: 印に入り、pane を閉じず、そのあとの巡回でも指示を送らないこと。
func TestDirectChat_再起動でdirectChatのrunを引き取り指示を送らない(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	issue := sampleIssue(330, humanState)
	fx.Tracker.AddIssue(issue)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-330", Cwd: wt.Path, AgentName: "continuo-hello-world-330",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-330",
	})

	restore(t, fx)
	if got := fx.Orc.RunningIdentifiers(); len(got) != 1 {
		t.Fatalf("direct chat の run を引き取っていない: %v", got)
	}
	for range 2 {
		fx.Orc.Tick(context.Background())
	}
	// **送らないことを確かめるので、待ち合わせる目印が無い。**turn ループが起きうるだけの間を置く。
	time.Sleep(200 * time.Millisecond)

	if n := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); n != 0 {
		t.Fatalf("引き取った direct chat の run へ指示を送った: %d 回", n)
	}
	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("引き取った direct chat の run の pane を閉じた: %v", ids)
	}
}

// TestDirectChat_再起動で確認の画面ならpaneを閉じず通知も書かず戻したときに閉じる集合が閉じる は、
// 設計 3-4 の段3 の例外(2)・3-83f の表の最後の2行を確かめる。
//
// 目的: 確認の画面（`blocked`）で止まっている direct chat の run は、**`failure_state` へ落とさず、pane も閉じず、
// 引き渡しの通知も投稿しない**（通知の本文は「continuo が pane を閉じたので画面は残っていません」と書いており、
// 閉じないのに投稿すると嘘になる）。見送った pane は閉じる集合が扱い、**作業中の Status へ戻した巡回で閉じる。**
// 与える情報: Status が direct chat で、agent_status が blocked の pane を持つ run。
// 成功条件: 復元で pane を閉じず、コメントも Status の書き込みも無い。作業中へ戻した巡回で、その pane を閉じる。
func TestDirectChat_再起動で確認の画面ならpaneを閉じず通知も書かず戻したときに閉じる集合が閉じる(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	holdPrompt(fx)
	// **閉じたあとの同じ巡回の着手は、この台本の pane の一覧（workspace を名乗らない）では pane を引けずに落ちる。**
	// この検査が見るのは「閉じたか」だけなので、その失敗は許す。
	fx.AllowLog("権限の確認で止まっている", "direct chat のカードなので pane は閉じません", "印に入っていない worktree に生きた pane",
		"着手に失敗しました")
	issue := sampleIssue(331, humanState)
	fx.Tracker.AddIssue(issue)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-331", Cwd: wt.Path, AgentName: "continuo-hello-world-331",
		AgentStatus: herdr.AgentStatusBlocked, SessionUUID: "sess-331",
	})

	restore(t, fx)
	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("direct chat のカードの pane を復元で閉じた: %v", ids)
	}
	if n := len(fx.Tracker.CommentsOf(nodeIDOfIssue(issue))); n != 0 {
		t.Fatalf("pane を閉じていないのに引き渡しの通知を投稿した: %d 件", n)
	}
	if got := fx.Tracker.StateOf(issue.ID); got != humanState {
		t.Fatalf("direct chat のカードを動かした: %q", got)
	}

	// 人間が作業中の Status へ戻した。
	fx.Tracker.SetState(issue.ID, "In Progress")
	fx.Orc.Tick(context.Background())
	if ids := closedPaneIDs(fx); indexOf(ids, "p-331") < 0 {
		t.Fatalf("作業中へ戻したのに、見送った pane を閉じていない: %v", ids)
	}
}

// TestDirectChat_取り直しに失敗した復元のagent名の無いpaneを戻したときに閉じる は、
// 設計 3-4 の段3 の例外(1)・3-9 の手順7b・3-83f の閉じる集合を確かめる。
//
// 目的: 取り直しに失敗した run は Status が読めないので、**閉じずに閉じる集合へ入れる。**
// Status が作業中へ戻った巡回で、**agent 名の無い pane も閉じる。**入れないと、取り残しの処理は
// agent 名の無い pane を飛ばすので、着手がその pane へ `agent.start` を送る。
// 与える情報: 復元の取り直しが失敗し、agent 名を持たない pane（シェルに戻った pane）が残っている worktree。
// 成功条件: 復元では閉じず、取り直せた巡回で Status が作業中なら、その pane を閉じること。
func TestDirectChat_取り直しに失敗した復元のagent名の無いpaneを戻したときに閉じる(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	holdPrompt(fx)
	fx.AllowLog("取り直しに失敗", "印に入っていない worktree に生きた pane", "実行中の issue を取り直せません", "候補の取得に失敗",
		"着手に失敗しました")
	issue := sampleIssue(332, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-332", Cwd: wt.Path, NoAgentKind: true,
		AgentStatus: herdr.AgentStatusIdle,
	})
	fx.Tracker.SetIDsError(errString("レートリミットに達しました"))

	restore(t, fx)
	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("取り直しに失敗しただけで pane を閉じた: %v", ids)
	}

	fx.Tracker.SetIDsError(nil)
	fx.Orc.Tick(context.Background())
	if ids := closedPaneIDs(fx); indexOf(ids, "p-332") < 0 {
		t.Fatalf("作業中の Status なのに、閉じる集合の agent 名の無い pane を閉じていない: %v", ids)
	}
}

// TestDirectChat_再起動でdirect_chatのworktreeの2枚目のpaneを閉じずagent名を持つpaneを引き継ぐ は、
// 設計 3-83j の「再起動で、direct chat の worktree の2枚目の pane を閉じない」を確かめる。
//
// 目的: direct chat の最中に人間がテスト用のシェルを分けていると、再起動でどちらかが消える。
// **Status が direct chat の worktree では段4 で閉じない。引き継ぎの相手には、agent 名を持つ pane を選ぶ**
// （pane ID の小さいほうではない）。
// 与える情報: Status が direct chat の worktree に pane が2枚。ID の小さいほうは agent 名の無いシェル。
// 成功条件: どちらも閉じず、agent 名を持つ pane を引き継ぐこと。
func TestDirectChat_再起動でdirectChatのworktreeの2枚目のpaneを閉じずagent名を持つpaneを引き継ぐ(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	fx.AllowLog("同じ worktree に pane が2つあります")
	issue := sampleIssue(333, humanState)
	fx.Tracker.AddIssue(issue)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx,
		livePane{PaneID: "p-333a", Cwd: wt.Path, NoAgentKind: true, AgentStatus: herdr.AgentStatusIdle},
		livePane{PaneID: "p-333b", Cwd: wt.Path, AgentName: "continuo-hello-world-333",
			AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-333"},
	)

	restore(t, fx)

	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("direct chat の worktree の2枚目の pane を閉じた: %v", ids)
	}
	if got := fx.Orc.RunningIdentifiers(); len(got) != 1 {
		t.Fatalf("agent 名を持つ pane を引き継いでいない: %v", got)
	}
	if !strings.Contains(fx.Logs.String(), "pane_id=p-333b") {
		t.Fatalf("agent 名を持つ pane を引き継ぎの相手に選んでいない: %s", fx.Logs.String())
	}
}

// TestDirectChat_再起動でagent名を持つpaneが無いdirect_chatのworktreeは引き継がず全部残す は、
// 設計 3-83j の同じ段落の「agent 名を持つ pane が無ければ、引き継がずに2枚とも残す」を確かめる。
//
// 与える情報: Status が direct chat の worktree に、agent 名の無い pane が2枚。
// 成功条件: どちらも閉じず、引き継がないこと。
func TestDirectChat_再起動でagent名を持つpaneが無いdirectChatのworktreeは引き継がず全部残す(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	fx.AllowLog("同じ worktree に pane が2つあります", "agent 名を持つものが無い")
	issue := sampleIssue(334, humanState)
	fx.Tracker.AddIssue(issue)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx,
		livePane{PaneID: "p-334a", Cwd: wt.Path, NoAgentKind: true, AgentStatus: herdr.AgentStatusIdle},
		livePane{PaneID: "p-334b", Cwd: wt.Path, NoAgentKind: true, AgentStatus: herdr.AgentStatusIdle},
	)

	restore(t, fx)

	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("direct chat の worktree の pane を閉じた: %v", ids)
	}
	if got := fx.Orc.RunningIdentifiers(); len(got) != 0 {
		t.Fatalf("agent 名を持つ pane が無いのに引き継いだ: %v", got)
	}
}

// abortedLog は、終わらせる処理を人間の引き取りでやめたときにだけ出るログである（設計 3-83f）。
const abortedLog = "この run を終わらせるのをやめます"

// TestDirectChat_コメントを書かせる途中で引き取って戻すと同じpaneで続く は、
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
func TestDirectChat_コメントを書かせる途中で引き取って戻すと同じpaneで続く(t *testing.T) {
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

// TestDirectChat_turnの終わりに引き取りを見たあと巡回より先に戻しても指示が届く は、
// `decideAfterTurn` の direct chat の枝（設計 3-83f）が送る印を立てることを確かめる。
//
// 目的: turn の終わりに取り直したカードが direct chat だったとき、turn ループはそこで終わる。
// **次の巡回より先に人間が作業中へ戻すと、direct chat へは1度も入らない。**
// 送る印を立てておかないと、ループも送る印も無い run が残り、戻しても指示が1つも届かない。
// 与える情報: 1回目の turn の待ちの最中にカードを direct chat へ動かし、表明の無い `Stop` を流した run。
// 巡回を回す前に作業中の Status へ戻す。
// 成功条件: 次の巡回で2回目の指示が届くこと。
func TestDirectChat_turnの終わりに引き取りを見たあと巡回より先に戻しても指示が届く(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	issue := sampleIssue(342, "Ready")
	fx.Tracker.AddIssue(issue)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)
	releasePrompt := blockFirstPrompt(t, fx)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "1回目の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	// ★ 巡回を回さずにカードを direct chat へ動かす。turn の終わりが先に見る。
	fx.Tracker.SetState(issue.ID, humanState)
	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "途中まで進めました。\n\nCONTINUO-STATUS: working", false),
	})
	fx.Orc.OnHook(stopEvent(fx.Sessions[0], path, "p1"))
	releasePrompt()
	waitFor(t, 20*time.Second, "turn の終わりが引き取りを見る", func() bool {
		return strings.Contains(fx.Logs.String(), "この turn の後始末をせずに戻ります")
	})

	// ★ 巡回より先に、人間が作業中へ戻す。
	fx.Tracker.SetState(issue.ID, fx.Config.Tracker.RunningState)
	waitFor(t, 20*time.Second, "戻した run へ2回目の指示を送る", func() bool {
		fx.Orc.Tick(context.Background())
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) >= 2
	})
	if n := fx.Herdr.CountMethod(herdr.MethodPaneClose); n != 0 {
		t.Errorf("pane を閉じた: %d 回", n)
	}
}

// TestDirectChat_Statusごとの上限に達していてもpaneを用意する は、設計 3-83c の門5 を確かめる。
//
// 目的: **門5 は全体の上限（`agent.max_concurrent_agents`）だけを見る。**用意する pane は
// `running_state` の枠を消費しないので、Status ごとの上限を当てると、全体が空いていても pane が来ない。
// 与える情報: `agent.max_concurrent_agents_by_state` の `In Progress` を1にし、1件が `In Progress` で走っている。
// そこへ担当者が自分1人の issue を direct chat へ置く。
// 成功条件: direct chat の pane を用意し、「話しかけられます」の案内を書くこと。
func TestDirectChat_Statusごとの上限に達していてもpaneを用意する(t *testing.T) {
	fx := newDirectChatFixture(t, func(cfg *config.Config) {
		cfg.Agent.MaxConcurrentAgents = 3
		cfg.Agent.MaxConcurrentAgentsByState = map[string]int{"In Progress": 1}
	})
	holdPrompt(fx)
	running := sampleIssue(343, "Ready")
	fx.Tracker.AddIssue(running)
	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "1件目が In Progress で走る", func() bool {
		return fx.Tracker.StateOf(running.ID) == fx.Config.Tracker.RunningState
	})

	_, node := addOwnDirectChatIssue(fx, 344)
	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "Status ごとの上限に関わらず direct chat の pane を用意する", func() bool {
		return commentsContaining(fx.Tracker, node, readyCommentMarker) == 1
	})
}

// TestDirectChat_Doneへ直接抜けたら成果のコメントを書かせに行かない は、設計 3-83g の `Done` の行を確かめる。
//
// 目的: 人間が Claude Code を終了させてから `Done` へ動かすのは、人間が名指しした出口である。
// **書かせに行くと、終了させたものを `--resume` で立て直すことになる。**
// 与える情報: 1回目の turn を送ったあと direct chat へ入れ、コメントを1件も書かずに `Done` へ動かした run。
// 成功条件: 立て直しの `agent.start` も指示も無く、pane を閉じ、印から外すこと。
func TestDirectChat_Doneへ直接抜けたら成果のコメントを書かせに行かない(t *testing.T) {
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

// TestDirectChat_閉じる集合の pane は作業中でない Status では閉じない は、設計 3-83f の閉じる集合を確かめる。
//
// 目的: **閉じるのは、印を持たずに Status が `active_states` へ戻った巡回だけである。**
// それ以外の Status で閉じると、人間が `In Review` へ動かして見返している画面が消える。
// 与える情報: 復元で確認の画面のまま見送った direct chat の pane（閉じる集合に入る）。
// カードを `In Review` へ動かした巡回と、そのあと作業中へ戻した巡回。
// 成功条件: `In Review` の巡回では閉じず、作業中へ戻した巡回で閉じること。
func TestDirectChat_閉じる集合のpaneは作業中でないStatusでは閉じない(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	holdPrompt(fx)
	fx.AllowLog("権限の確認で止まっている", "direct chat のカードなので pane は閉じません", "印に入っていない worktree に生きた pane",
		"着手に失敗しました")
	issue := sampleIssue(346, humanState)
	fx.Tracker.AddIssue(issue)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-346", Cwd: wt.Path, AgentName: "continuo-hello-world-346",
		AgentStatus: herdr.AgentStatusBlocked, SessionUUID: "sess-346",
	})
	restore(t, fx)

	fx.Tracker.SetState(issue.ID, "In Review")
	fx.Orc.Tick(context.Background())
	if ids := closedPaneIDs(fx); indexOf(ids, "p-346") >= 0 {
		t.Fatalf("作業中でない Status なのに、閉じる集合の pane を閉じた: %v", ids)
	}

	fx.Tracker.SetState(issue.ID, "In Progress")
	fx.Orc.Tick(context.Background())
	if ids := closedPaneIDs(fx); indexOf(ids, "p-346") < 0 {
		t.Fatalf("作業中へ戻したのに、閉じる集合の pane を閉じていない: %v", ids)
	}
}

// errString はテストで返すエラーである。
type errString string

// Error はエラーの文面を返す。
func (e errString) Error() string { return string(e) }

// TestDirectChat_上限を超えたときに書けなかったら次の巡回で用意せずに書き直す は、設計 3-83c の門7 を確かめる。
//
// 目的: 用意の失敗が上限を超えた issue へ門7 が書く `failure_state` の書き込みが失敗しても、**次の巡回で書き直す。**
// **書き直すまで用意はやり直さない**（やり直すと、上限を超えたあとも pane を開いては閉じる）。
// 与える情報: `agent.max_retries` を0にし、`agent.start` が必ず失敗する用意。2回目の巡回（門7 が初めて書く）では
// `UpdateStatus` が失敗する。
// 成功条件: 3回目の巡回で Status が `failure_state` になり、理由のコメントが1件で、`agent.start` は1回のまま。
func TestDirectChat_上限を超えたときに書けなかったら次の巡回で用意せずに書き直す(t *testing.T) {
	fx := newDirectChatFixture(t, func(cfg *config.Config) { cfg.Agent.MaxRetries = 0 })
	fx.AllowLog("direct chat の pane を用意できませんでした", "起動できません",
		"direct chat のカードへ failure_state を書けませんでした")
	fx.Herdr.Handle(herdr.MethodAgentStart, func(map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "agent_start_failed", Message: "起動できませんでした"}
	})
	id, node := addOwnDirectChatIssue(fx, 351)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "1回目の用意が落ちて印が外れる", func() bool {
		return strings.Contains(fx.Logs.String(), "direct chat の pane を用意できませんでした") &&
			len(fx.Orc.RunningIdentifiers()) == 0
	})
	fx.Tracker.SetUpdateError(errors.New("GraphQL が一時的に失敗しました"))
	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "門7 の書き込みが失敗する", func() bool {
		return strings.Contains(fx.Logs.String(), "direct chat のカードへ failure_state を書けませんでした") &&
			len(fx.Orc.RunningIdentifiers()) == 0
	})
	if got := fx.Tracker.StateOf(id); got != humanState {
		t.Fatalf("書き込みが失敗したのにカードが動いた: %q", got)
	}
	starts := fx.Herdr.CountMethod(herdr.MethodAgentStart)

	fx.Tracker.SetUpdateError(nil)
	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "次の巡回で failure_state を書き直す", func() bool {
		return fx.Tracker.StateOf(id) == fx.Config.Tracker.FailureState
	})
	waitFor(t, 5*time.Second, "理由のコメントを書く", func() bool {
		return commentsContaining(fx.Tracker, node, "用意できませんでした") == 1
	})
	if n := fx.Herdr.CountMethod(herdr.MethodAgentStart); n != starts {
		t.Fatalf("上限を超えたあとに用意をやり直した: agent.start が %d 回から %d 回へ", starts, n)
	}
}

// turnEndSeesDirectChat は、1回目の turn の終わりがカードを direct chat と読むところまで進める。
//
// **巡回は回さない。**`decideAfterTurn` の direct chat の枝が送る印を立て、控えの Status を
// direct chat にしたところで止める（巡回の段1 はまだ印を立てていない）。
//
// fx: fixture。
// number: issue の番号。
// 戻り値: 対象の issue。
func turnEndSeesDirectChat(t *testing.T, fx *fixture, number int) tracker.Issue {
	t.Helper()
	issue := sampleIssue(number, "Ready")
	fx.Tracker.AddIssue(issue)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)
	releasePrompt := blockFirstPrompt(t, fx)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "1回目の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	fx.Tracker.SetState(issue.ID, humanState)
	path := writeTranscript(t, t.TempDir(), "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "途中まで進めました。\n\nCONTINUO-STATUS: working", false),
	})
	fx.Orc.OnHook(stopEvent(fx.Sessions[0], path, "p1"))
	releasePrompt()
	waitFor(t, 20*time.Second, "turn の終わりが引き取りを見る", func() bool {
		return strings.Contains(fx.Logs.String(), "この turn の後始末をせずに戻ります")
	})
	return issue
}

// assertNoPromptFor は、しばらく待っても `agent.prompt` の回数が増えないことを確かめる。
//
// **送らないことは、待つ以外に確かめようがない。**送る側は巡回の中で turn ループを起こすだけなので、
// 起こされたなら数十ミリ秒で届く。
//
// fx: fixture。
// want: 増えていてはならない回数。
// message: 増えていたときに出す文。
func assertNoPromptFor(t *testing.T, fx *fixture, want int, message string) {
	t.Helper()
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if n := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); n != want {
			t.Fatalf("%s: agent.prompt が %d 回から %d 回へ", message, want, n)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestDirectChat_turnの終わりに引き取りを見たあと取り直しに失敗した巡回では指示を送らない は、
// 設計 3-83f の `wakeRuns` と turn ループの先頭の行を確かめる。
//
// 目的: turn の終わりがカードを direct chat と読んで送る印を立てたあと、**巡回の取り直しが失敗すると
// direct chat の印は立たない。**送る側が印しか見ないと、カンバンでは Direct Chat のまま
// 人間の pane へ続きの指示が届く。**控えの Status でも見るので送らない。**
// 作業中へ戻したあとは送る（送る印を下ろしていない）。
// 与える情報: 1回目の turn の終わりにカードが direct chat だった run。次の巡回では ID 指定の取り直しが失敗する。
// 成功条件: 取り直しに失敗した巡回で `agent.prompt` が増えないこと。取り直せる巡回でも増えないこと。
// 作業中へ戻した巡回で2回目の指示が届くこと。pane を閉じないこと。
func TestDirectChat_turnの終わりに引き取りを見たあと取り直しに失敗した巡回では指示を送らない(t *testing.T) {
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

// TestDirectChat_turnの終わりに引き取りを見たあと手を離す巡回では指示を送らない は、
// 設計 3-83h の「手を離す経路」と 3-83f の `wakeRuns` の行を確かめる。
//
// 目的: turn の終わりが送る印を立てたあと、次の巡回で担当者が別の1人に替わっていたら手を離す。
// **手を離す途中（印を外すまで）に、人間の pane へ続きの指示を送らない。**
// 与える情報: 1回目の turn の終わりにカードが direct chat だった run。次の巡回の前に担当者を別の1人に替える。
// 成功条件: `agent.prompt` が増えず、pane を閉じ、印を外すこと。Status を書かないこと。
func TestDirectChat_turnの終わりに引き取りを見たあと手を離す巡回では指示を送らない(t *testing.T) {
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

// TestDirectChat_上限を超えたissueへ書いている最中の巡回では2本目を立てない は、設計 3-83c の門7 を確かめる。
//
// 目的: 上限を超えた issue へ書く経路は門7 の1箇所だけが走らせ、**書いている最中は次の巡回で2本目を立てない。**
// 書き込みが巡回の間隔より長くかかると、2本が並んで書き、コメントが2件付きうる。
// 与える情報: `agent.max_retries` を0にし、`agent.start` が必ず失敗する用意。門7 の1本目の `UpdateStatus` を止めておき、
// その間にもう1回巡回を回す。
// 成功条件: 止めている間に `UpdateStatus` が1回も記録されないこと（1本目は関門の手前で待っているので記録されない）。
// 放したあと Status が `failure_state` になり、理由のコメントが1件であること。
func TestDirectChat_上限を超えたissueへ書いている最中の巡回では2本目を立てない(t *testing.T) {
	fx := newDirectChatFixture(t, func(cfg *config.Config) { cfg.Agent.MaxRetries = 0 })
	fx.AllowLog("direct chat の pane を用意できませんでした", "起動できません")
	fx.Herdr.Handle(herdr.MethodAgentStart, func(map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "agent_start_failed", Message: "起動できませんでした"}
	})
	id, node := addOwnDirectChatIssue(fx, 354)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "1回目の用意が落ちて印が外れる", func() bool {
		return strings.Contains(fx.Logs.String(), "direct chat の pane を用意できませんでした") &&
			len(fx.Orc.RunningIdentifiers()) == 0
	})

	releaseUpdate, entered := fx.Tracker.HoldUpdate()
	fx.Orc.Tick(context.Background())
	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("門7 が書き込みを始めない")
	}
	fx.Tracker.ResetCalls()

	fx.Orc.Tick(context.Background())
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if n := countString(fx.Tracker.Calls(), "UpdateStatus"); n != 0 {
			releaseUpdate()
			t.Fatalf("書いている最中の巡回で2本目の書き込みを立てた: UpdateStatus %d 回", n)
		}
		time.Sleep(20 * time.Millisecond)
	}

	releaseUpdate()
	waitFor(t, 15*time.Second, "1本目が failure_state を書く", func() bool {
		return fx.Tracker.StateOf(id) == fx.Config.Tracker.FailureState
	})
	waitFor(t, 5*time.Second, "理由のコメントを書く", func() bool {
		return commentsContaining(fx.Tracker, node, "用意できませんでした") == 1
	})
}

// countString は、並びの中に want がいくつあるかを返す。
//
// list: 数える並び。
// want: 数える値。
// 戻り値: 一致した数。
func countString(list []string, want string) int {
	n := 0
	for _, v := range list {
		if v == want {
			n++
		}
	}
	return n
}

// TestDirectChat_turnの終わりに引き取りを見たあと終わらせる処理が走っているあいだは指示を送らない は、
// 設計 3-83f の `wakeRuns` の行（終端の権利を取った run）を確かめる。
//
// 目的: turn の終わりがカードを direct chat と読んで送る印を立てたあと、**巡回より先に人間が引き渡しの Status へ
// 動かすと、direct chat へは入らないまま終わらせる処理が始まる。**そこへ続きの指示を送ると、
// 終わらせる処理（`after_run`・`pane.close`）と並んで turn が走る。
// 与える情報: 1回目の turn の終わりにカードが direct chat だった run。巡回を回す前に `Blocked` へ動かす。
// `workspace_hooks.after_run` は1秒かかる（終わらせる処理が走っている時間を作る。後片付けの期限
// `herdr.read_timeout_ms` は fixture では2秒なので、それより短くする）。
// 成功条件: `agent.prompt` が増えないこと。pane を閉じ、印を外すこと。
func TestDirectChat_turnの終わりに引き取りを見たあと終わらせる処理が走っているあいだは指示を送らない(t *testing.T) {
	slow := "sleep 1"
	fx := newDirectChatFixture(t, func(cfg *config.Config) { cfg.WorkspaceHooks.AfterRun = &slow })
	issue := turnEndSeesDirectChat(t, fx, 355)
	sent := fx.Herdr.CountMethod(herdr.MethodAgentPrompt)

	fx.Tracker.SetState(issue.ID, "Blocked")
	fx.Orc.Tick(context.Background())
	assertNoPromptFor(t, fx, sent, "終わらせる処理が走っている run へ続きの指示を送った")
	waitFor(t, 15*time.Second, "pane を閉じて印を外す", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
	if n := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); n != sent {
		t.Fatalf("終わらせる処理のあいだに続きの指示を送った: agent.prompt が %d 回から %d 回へ", sent, n)
	}
	if n := fx.Herdr.CountMethod(herdr.MethodPaneClose); n == 0 {
		t.Error("引き渡しへ動かしたのに pane を閉じていない")
	}
}

// TestDirectChat_turnの終わりを待つ印はdirectChatを抜けるときに下ろす は、設計 3-83i を確かめる。
//
// 目的: direct chat へ入る前の turn ループが一時的な失敗で立てた「turn の終わりを待つ印」が残ると、
// 戻したあと `wakeRuns` が送る印より先にそれを取り、**指示を送らずに待つだけの turn ループを起こす。**
// 戻しても指示が届かず、約1時間後に stall で打ち切られる。**抜けるときに下ろせば、送る印で続きの指示が届く。**
// 与える情報: 1回目の `agent.prompt` が herdr へ届かずに切れた run（待つ印が立つ）。そのあと direct chat へ動かし、
// 巡回で入れてから作業中へ戻す。
// 成功条件: 戻したあと、1回目の本文ではない続きの指示が届くこと。
func TestDirectChat_turnの終わりを待つ印はdirectChatを抜けるときに下ろす(t *testing.T) {
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

// failResolvePane は、用意の段2 の着手の段8（`resolvePane`）を落とす台本を入れる。
//
// **worktree の workspace を引く `pane.list` にだけ2枚を返す**（`resolvePane` は1枚でないと落ちる）。
// 全体の `pane.list`（門4 が引くもの）は、all が返す pane を返す。
//
// fx: fixture。
// all: 全体の `pane.list` に返す pane（nil なら0枚）。呼ばれるたびに読むので、あとから差し替えられる。
func failResolvePane(fx *fixture, all func() []any) {
	fx.Herdr.Handle(herdr.MethodPaneList, func(params map[string]any) (any, *rpcErr) {
		id, _ := params["workspace_id"].(string)
		if id == "" {
			panes := []any{}
			if all != nil {
				panes = all()
			}
			return map[string]any{"type": "pane_list", "panes": panes}, nil
		}
		return map[string]any{
			"type": "pane_list",
			"panes": []any{
				map[string]any{"pane_id": id + ":p1", "workspace_id": id, "agent_status": "idle"},
				map[string]any{"pane_id": id + ":p2", "workspace_id": id, "agent_status": "idle"},
			},
		}, nil
	})
}

// worktreeWorkspaceID は、テスト用の herdr が worktree のために開いた workspace の ID を返す
// （リポジトリの親 workspace は除く）。
//
// fx: fixture。
// 戻り値: workspace の ID。無ければ空文字。
func worktreeWorkspaceID(fx *fixture) string {
	for id, ws := range fx.Herdr.OpenWorkspaces() {
		if ws.Checkout != ws.RepoRoot {
			return id
		}
	}
	return ""
}

// TestDirectChat_用意の段2が段8より前で落ちたらworktreeOpenが開いたpaneを閉じる は、設計 3-83d の用意の段2 を確かめる。
//
// 目的: `worktree.open`（着手の段3）の時点で herdr は pane を開いている。**着手の段8 まで控えないと、
// 段4〜段8 で落ちたときに後始末が閉じる相手を知らず、シェルの pane が残る。**残った pane は門4 に当たり続けるので、
// 用意し直されず、上限の書く経路にも届かない。
// 与える情報: 着手の段8 の `resolvePane` が落ちる用意（workspace の中に pane が2枚）。
// 成功条件: 印を外し、`worktree.open` が開いた pane を `pane.close` で閉じること。
func TestDirectChat_用意の段2が段8より前で落ちたらworktreeOpenが開いたpaneを閉じる(t *testing.T) {
	fx := newDirectChatFixture(t, func(cfg *config.Config) { cfg.Agent.MaxRetryBackoffMs = 600000 })
	fx.AllowLog("direct chat の pane を用意できませんでした")
	failResolvePane(fx, nil)
	addOwnDirectChatIssue(fx, 357)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "用意が落ちて印が外れる", func() bool {
		return strings.Contains(fx.Logs.String(), "direct chat の pane を用意できませんでした") &&
			len(fx.Orc.RunningIdentifiers()) == 0
	})
	ws := worktreeWorkspaceID(fx)
	if ws == "" {
		t.Fatal("前提が崩れている（worktree の workspace が開かれていない）")
	}
	if n := fx.Herdr.CountMethod(herdr.MethodPaneClose); n != 1 {
		t.Fatalf("worktree.open が開いた pane を閉じていない: pane.close %d 回", n)
	}
	if got := fx.Herdr.ParamsOf(t, herdr.MethodPaneClose)["pane_id"]; got != ws+":p1" {
		t.Fatalf("閉じた pane が worktree.open の開いたものではない: %v（want %s:p1）", got, ws)
	}
	if n := fx.Herdr.CountMethod(herdr.MethodAgentStart); n != 0 {
		t.Fatalf("前提が崩れている（段8 より先へ進んだ）: agent.start %d 回", n)
	}
}

// TestDirectChat_worktreeのworkspaceにcwdの違うpaneがあれば用意しない は、設計 3-83c の門4 を確かめる。
//
// 目的: 門4 は、用意の段2 が pane を引くのと同じ見方で「pane が1枚でもあるか」を見る。用意の段2 の `resolvePane` は
// `worktree.open` が返した workspace の中の1枚を cwd を見ずに使う。**門4 が cwd だけで見ると、人間がその workspace の
// pane で別のディレクトリへ移っていたときに門4 を通り抜け、その pane へ `agent.start` が届く。**
// 与える情報: 1回目の用意が段8 で落ちて worktree の workspace が開いたまま残った issue。その workspace に、
// cwd が別のディレクトリの pane が1枚ある。
// 成功条件: `agent.start` を投げないこと。その pane が無くなった巡回では用意すること（検査が空振りでない証拠）。
func TestDirectChat_worktreeのworkspaceにcwdの違うpaneがあれば用意しない(t *testing.T) {
	fx := newDirectChatFixture(t, func(cfg *config.Config) { cfg.Agent.MaxRetryBackoffMs = 1 })
	fx.AllowLog("direct chat の pane を用意できませんでした")
	var mu sync.Mutex
	var extra []any
	failResolvePane(fx, func() []any {
		mu.Lock()
		defer mu.Unlock()
		return extra
	})
	_, node := addOwnDirectChatIssue(fx, 358)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "1回目の用意が落ちて印が外れる", func() bool {
		return strings.Contains(fx.Logs.String(), "direct chat の pane を用意できませんでした") &&
			len(fx.Orc.RunningIdentifiers()) == 0
	})
	ws := worktreeWorkspaceID(fx)
	if ws == "" {
		t.Fatal("前提が崩れている（worktree の workspace が開かれていない）")
	}
	// 人間がその workspace の pane で別のディレクトリへ移っている。段8 は通るようにする（直しを外すと用意が進む）。
	mu.Lock()
	extra = []any{map[string]any{"pane_id": ws + ":p9", "workspace_id": ws, "agent_status": "unknown", "cwd": t.TempDir()}}
	mu.Unlock()
	fx.Herdr.Handle(herdr.MethodPaneList, func(params map[string]any) (any, *rpcErr) {
		id, _ := params["workspace_id"].(string)
		mu.Lock()
		defer mu.Unlock()
		if id == "" {
			return map[string]any{"type": "pane_list", "panes": append([]any{}, extra...)}, nil
		}
		return map[string]any{"type": "pane_list", "panes": []any{
			map[string]any{"pane_id": id + ":p9", "workspace_id": id, "agent_status": "idle"},
		}}, nil
	})

	time.Sleep(20 * time.Millisecond) // 門7 の間隔（1ミリ秒）を空ける。
	fx.Orc.Tick(context.Background())
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if n := fx.Herdr.CountMethod(herdr.MethodAgentStart); n != 0 {
			t.Fatalf("worktree の workspace に pane があるのに用意した: agent.start %d 回", n)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// その pane が無くなれば用意する。
	mu.Lock()
	extra = nil
	mu.Unlock()
	waitFor(t, 15*time.Second, "pane が無くなった巡回で用意する", func() bool {
		fx.Orc.Tick(context.Background())
		return commentsContaining(fx.Tracker, node, readyCommentMarker) == 1
	})
}

// TestDirectChat_用意中に戻されたときClaudeCodeが既に動いていればturnの終わりを待ってから1回目の本文を送る は、
// 設計 3-83d の外れ方の表の1行目を確かめる。
//
// 目的: 用意の段2 が「Claude Code は既に動いている」（`ErrStartupBusy`）に着地した run を、用意の最中に人間が
// 作業中の Status へ戻すことがある。**送る印を立てると、走っている turn へ1回目の本文が投げられ、turn が混ざる。**
// 通常の着手のその道と同じく、turn の終わりを待ってから送る。
// 与える情報: `agent.get` が `agent_not_found` を返しながら作業中の hook を流す用意。起動の確認の最中にカードを
// `dispatch_state` へ戻す。
// 成功条件: `running_state` を書いたあと、巡回を回しても指示を送らないこと。`Stop` が届いたら1回目の本文を送ること。
func TestDirectChat_用意中に戻されたときClaudeCodeが既に動いていればturnの終わりを待ってから1回目の本文を送る(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	fx.AllowLog("turn の終わりの裏取りができませんでした")
	prompts := recordPrompts(fx)
	id, _ := addOwnDirectChatIssue(fx, 359)
	var once sync.Once
	fx.Herdr.Handle(herdr.MethodAgentGet, func(map[string]any) (any, *rpcErr) {
		once.Do(func() { fx.Tracker.SetState(id, fx.Config.Tracker.DispatchState) })
		fx.Orc.OnHook(toolHook("session-1", "PreToolUse"))
		return nil, agentNotFoundErr()
	})
	parent := writeTranscript(t, t.TempDir(), "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "戻された run の Status を running_state へ書く", func() bool {
		return fx.Tracker.StateOf(id) == fx.Config.Tracker.RunningState
	})
	for i := 0; i < 3; i++ {
		fx.Orc.Tick(context.Background())
	}
	assertNoPromptFor(t, fx, 0, "Claude Code が動いているのに1回目の本文を投げた")

	fx.Orc.OnHook(stopEvent("session-1", parent, "p1"))
	waitFor(t, 20*time.Second, "turn の終わりのあとに1回目の本文を送る", func() bool {
		fx.Orc.Tick(context.Background())
		return len(prompts()) >= 1
	})
	if got := prompts()[0]; !strings.Contains(got, firstPromptMarker) {
		t.Errorf("用意中に戻された run へ1回目の本文を送っていない: %q", got)
	}
}

// TestDirectChat_担当者が1人でないカードへ書いている最中の巡回では2本目を立てない は、設計 3-83h の書く経路を確かめる。
//
// 目的: 担当者の人数による書き込みは、門3 と巡回の段1 が巡回ごとに立てる。**書いている最中は次の巡回で2本目を立てない。**
// 書き込みが巡回の間隔より長くかかると、2本が並んで書き、同じ機械がコメントを2件付けうる。
// 与える情報: 担当者が0人の direct chat のカード。1本目の `UpdateStatus` を止めておき、その間にもう1回巡回を回す。
// 成功条件: 止めている間に `UpdateStatus` が1回も記録されないこと。放したあと Status が `failure_state` になり、
// 「担当者を1人に」が1件であること。
func TestDirectChat_担当者が1人でないカードへ書いている最中の巡回では2本目を立てない(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	fx.AllowLog("担当者が1人ではない")
	issue := sampleIssue(360, humanState)
	fx.Tracker.AddIssue(issue)
	node := nodeIDOfIssue(issue)

	releaseUpdate, entered := fx.Tracker.HoldUpdate()
	fx.Orc.Tick(context.Background())
	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("担当者の人数による書き込みが始まらない")
	}
	fx.Tracker.ResetCalls()

	fx.Orc.Tick(context.Background())
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if n := countString(fx.Tracker.Calls(), "UpdateStatus"); n != 0 {
			releaseUpdate()
			t.Fatalf("書いている最中の巡回で2本目の書き込みを立てた: UpdateStatus %d 回", n)
		}
		time.Sleep(20 * time.Millisecond)
	}

	releaseUpdate()
	waitFor(t, 15*time.Second, "1本目が failure_state を書く", func() bool {
		return fx.Tracker.StateOf(issue.ID) == fx.Config.Tracker.FailureState
	})
	waitFor(t, 5*time.Second, "理由のコメントを書く", func() bool {
		return commentsContaining(fx.Tracker, node, assigneesInvalidMarker) == 1
	})
}

// TestDirectChat_上限を超えた書き込みに成功したら記録を消す は、設計 3-83d の用意の段2 を確かめる。
//
// 目的: 門7 の書く経路が実際に書けたら、用意の失敗の記録を消す。**残すと、索引の遅れでカードがまだ候補に見える間に
// 人間が direct chat へ戻したとき、1回も用意し直さずにまた `failure_state` へ落とす。**
// 与える情報: `agent.max_retries` を0にし、1回目の用意を落とす。門7 が `failure_state` を書いたあと、
// 候補から外れる巡回を挟まずに人間が direct chat へ戻す。そのときは用意が通る。
// 成功条件: 戻した巡回で用意し（「話しかけられます」が1件）、「用意できませんでした」は1件のままであること。
func TestDirectChat_上限を超えた書き込みに成功したら記録を消す(t *testing.T) {
	fx := newDirectChatFixture(t, func(cfg *config.Config) { cfg.Agent.MaxRetries = 0 })
	fx.AllowLog("direct chat の pane を用意できませんでした", "起動できません")
	var mu sync.Mutex
	failing := true
	fx.Herdr.Handle(herdr.MethodAgentStart, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		defer mu.Unlock()
		if failing {
			return nil, &rpcErr{Code: "agent_start_failed", Message: "起動できませんでした"}
		}
		return map[string]any{
			"type":  "agent_started",
			"agent": map[string]any{"name": params["name"], "agent_status": "idle", "interactive_ready": true, "pane_id": params["pane_id"]},
		}, nil
	})
	id, node := addOwnDirectChatIssue(fx, 361)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "1回目の用意が落ちて印が外れる", func() bool {
		return strings.Contains(fx.Logs.String(), "direct chat の pane を用意できませんでした") &&
			len(fx.Orc.RunningIdentifiers()) == 0
	})
	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "門7 が failure_state を書き、理由を書く", func() bool {
		return fx.Tracker.StateOf(id) == fx.Config.Tracker.FailureState &&
			commentsContaining(fx.Tracker, node, "用意できませんでした") == 1
	})

	mu.Lock()
	failing = false
	mu.Unlock()
	fx.Tracker.SetState(id, humanState)
	waitFor(t, 15*time.Second, "戻した巡回で用意し直す", func() bool {
		fx.Orc.Tick(context.Background())
		return commentsContaining(fx.Tracker, node, readyCommentMarker) == 1
	})
	if n := commentsContaining(fx.Tracker, node, "用意できませんでした"); n != 1 {
		t.Errorf("書けた記録を消さずに、戻したカードをまた落とした: 「用意できませんでした」%d 件", n)
	}
	if got := fx.Tracker.StateOf(id); got != humanState {
		t.Errorf("戻したカードをまた動かした: %q", got)
	}
}
