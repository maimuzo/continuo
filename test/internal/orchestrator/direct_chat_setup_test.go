// direct chat の pane の用意（設計 3-82c / 3-82d）・再起動（3-82j）・閉じる集合（3-82f）の検査である。
//
// **本物の git で worktree を作る。**用意の段2 は着手の段3〜段10 を踏むので、
// 通信をしない stub（stub_test.go）では worktree を用意できない。
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

// readyCommentMarker は、direct chat の pane を用意したときに issue へ書く案内にだけ出る文字列である（設計 3-82d）。
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
// **用意の段2 の最中にカードや担当者を動かす場面を作るためにある**（設計 3-82d の用意の段3）。
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

// TestDirectChat_担当者が自分1人ならpaneを用意し指示は送らない は、設計 3-82c と 3-82d の用意の段1〜段3 を確かめる。
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

// TestDirectChat_用意中に作業中へ戻されたら1回目の本文を送る は、設計 3-82d の外れ方の表の1行目を確かめる。
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

// TestDirectChat_用意中に担当者が替わったら自分で開いたpaneを閉じて印を外す は、設計 3-82d の外れ方の表の3行目を確かめる。
//
// 目的: 用意の最中に担当者が別の1人に替わったら、その machine は pane を持ってはならない（3-82h）。
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

// TestDirectChat_用意の失敗が上限に達したらfailure_stateへ動かして理由を書く は、設計 3-82d の用意の段2 を確かめる。
//
// 目的: 用意が落ちたら、カンバンへは書かず、自分で開いた pane を閉じ、印を外す。
// **通常の着手と同じ回数の上限（`agent.max_retries`）に達したら、書く経路で `failure_state` を書き、
// 落ちた理由をコメントする**（人間が了承した形）。担当者を直せとは書かない。
// 与える情報: `agent.max_retries` を1にし、`agent.start` が必ず失敗する用意。
// 成功条件: pane を閉じ、印を外し、Status が `failure_state` になり、「用意できませんでした」が1件。
func TestDirectChat_用意の失敗が上限に達したらfailure_stateへ動かして理由を書く(t *testing.T) {
	fx := newDirectChatFixture(t, func(cfg *config.Config) { cfg.Agent.MaxRetries = 1 })
	fx.AllowLog("direct chat の pane を用意できませんでした", "起動できません")
	fx.Herdr.Handle(herdr.MethodAgentStart, func(map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "agent_start_failed", Message: "起動できませんでした"}
	})
	id, node := addOwnDirectChatIssue(fx, 323)

	fx.Orc.Tick(context.Background())

	waitFor(t, 15*time.Second, "上限に達して failure_state へ動かす", func() bool {
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

// TestDirectChat_用意が落ちた直後の巡回ではやり直さない は、設計 3-82c の門7 を確かめる。
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
// 設計 3-82c の門1 の表の下と 3-77g の例外を確かめる。
//
// 目的: バックオフを挟んだやり直し（`redispatch`）で、着手の段2 が取り直した Status が direct chat なら、
// **書いた担当者を消し戻さない。**消すと担当者が0人になり、次の巡回で `failure_state` へ落ち、
// 「担当者を1人に」という事実と違うコメントが残る。**released のコメントも書かない。**
// 与える情報: 入札して担当者になったあと stall で打ち切られ、バックオフ中の run。その間に人間が
// カードを direct chat へ動かす。
// 成功条件: 担当者が自分のまま残り、released のコメントが無いこと。
// 印が外れ、direct chat の1パスが pane を用意し直すこと（「話しかけられます」が1件）。
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
	fx.Orc.Tick(context.Background())
	// **着手の段2 で印が外れ、同じ巡回の direct chat の1パスが pane を用意し直す**（設計 3-82c の門1 の表の段4〜段5）。
	waitFor(t, 10*time.Second, "direct chat の pane が用意される", func() bool {
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

// TestDirectChat_再起動でdirect_chatのrunを引き取り指示を送らない は、設計 3-4 の段5a と 3-82j の1行目を確かめる。
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
// 設計 3-4 の段3 の例外(2)・3-82f の表の最後の2行を確かめる。
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
// 設計 3-4 の段3 の例外(1)・3-9 の手順7b・3-82f の閉じる集合を確かめる。
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
// 設計 3-82j の「再起動で、direct chat の worktree の2枚目の pane を閉じない」を確かめる。
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
// 設計 3-82j の同じ段落の「agent 名を持つ pane が無ければ、引き継がずに2枚とも残す」を確かめる。
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

// abortedLog は、終わらせる処理を人間の引き取りでやめたときにだけ出るログである（設計 3-82f）。
const abortedLog = "この run を終わらせるのをやめます"

// TestDirectChat_コメントを書かせる途中で引き取って戻すと同じpaneで続く は、
// 設計 3-82f の打ち切りを `ensureAgentComment` の段5〜段7 から通して確かめる。
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
// `decideAfterTurn` の direct chat の枝（設計 3-82f）が送る印を立てることを確かめる。
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

// TestDirectChat_Statusごとの上限に達していてもpaneを用意する は、設計 3-82c の門5 を確かめる。
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

// TestDirectChat_Doneへ直接抜けたら成果のコメントを書かせに行かない は、設計 3-82g の `Done` の行を確かめる。
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

// TestDirectChat_閉じる集合の pane は作業中でない Status では閉じない は、設計 3-82f の閉じる集合を確かめる。
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
