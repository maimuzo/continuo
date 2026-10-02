// direct chat の pane の用意（設計 3-83c / 3-83d）・再起動（3-83j）・閉じる集合（3-83f）の検査である。
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

// TestDirectChat_閉じる集合はworkspaceの中で別のディレクトリへ移ったシェルも閉じてから着手する は、
// 設計 3-83f の閉じる集合と 3-83c の門4 を確かめる。
//
// 目的: 閉じる集合の worktree を作業中の Status へ戻すと、着手の段8 の `resolvePane` は worktree の workspace の中の
// 1枚を cwd を見ずに使う。**閉じる集合が cwd だけで pane を探すと、人間がその workspace の pane で別のディレクトリへ
// 移っていたときに閉じ損ねて集合から外し、そのシェルへ `agent.start` が届く。**
// 与える情報: 印を持たない direct chat の worktree。その workspace に、agent 名が無く cwd が別のディレクトリの pane が1枚ある。
// 成功条件: direct chat のあいだは閉じないこと。作業中へ戻した巡回で、その pane を閉じてから `agent.start` を投げ、
// `agent.start` の宛先がその pane でないこと。
func TestDirectChat_閉じる集合はworkspaceの中で別のディレクトリへ移ったシェルも閉じてから着手する(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	holdPrompt(fx)
	fx.AllowLog("印に入っていない worktree に生きた pane")
	issue := sampleIssue(360, humanState)
	fx.Tracker.AddIssue(issue)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	if wt.WorkspaceID == "" {
		t.Fatal("前提が崩れている（worktree の workspace が開かれていない）")
	}
	moved := wt.WorkspaceID + ":p9"
	fresh := wt.WorkspaceID + ":p10"
	var mu sync.Mutex
	closed := false
	fx.Herdr.Handle(herdr.MethodPaneClose, func(params map[string]any) (any, *rpcErr) {
		if params["pane_id"] == moved {
			mu.Lock()
			closed = true
			mu.Unlock()
		}
		return map[string]any{"type": "ok"}, nil
	})
	// 人間がその workspace の pane で別のディレクトリへ移っている（Claude Code は居ない）。
	// 閉じたあとの `worktree.open` の pane は、cwd がその worktree の新しい1枚として返す。
	other := t.TempDir()
	fx.Herdr.Handle(herdr.MethodPaneList, func(map[string]any) (any, *rpcErr) {
		mu.Lock()
		defer mu.Unlock()
		pane := map[string]any{"pane_id": moved, "workspace_id": wt.WorkspaceID, "agent_status": "unknown", "cwd": other}
		if closed {
			pane = map[string]any{"pane_id": fresh, "workspace_id": wt.WorkspaceID, "agent_status": "unknown", "cwd": wt.Path}
		}
		return map[string]any{"type": "pane_list", "panes": []any{pane}}, nil
	})

	fx.Orc.Tick(context.Background())
	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("direct chat のあいだに pane を閉じた: %v", ids)
	}
	if n := fx.Herdr.CountMethod(herdr.MethodAgentStart); n != 0 {
		t.Fatalf("前提が崩れている（direct chat の pane を用意した）: agent.start %d 回", n)
	}

	// 人間が作業中の Status へ戻した。
	fx.Tracker.SetState(issue.ID, "In Progress")
	waitFor(t, 15*time.Second, "作業中へ戻した巡回で着手する", func() bool {
		fx.Orc.Tick(context.Background())
		return fx.Herdr.CountMethod(herdr.MethodAgentStart) > 0
	})
	closeAt, startAt := -1, -1
	for i, r := range fx.Herdr.Requests() {
		switch r.Method {
		case herdr.MethodPaneClose:
			if r.Params["pane_id"] == moved && closeAt < 0 {
				closeAt = i
			}
		case herdr.MethodAgentStart:
			if startAt < 0 {
				startAt = i
				if r.Params["pane_id"] == moved {
					t.Fatalf("別のディレクトリへ移ったシェルへ agent.start を投げた: %v", r.Params)
				}
			}
		}
	}
	if closeAt < 0 || closeAt > startAt {
		t.Fatalf("別のディレクトリへ移ったシェルを閉じてから着手していない: pane.close %d 番目・agent.start %d 番目", closeAt, startAt)
	}
	// **着手が1回目の指示を送り終えるまで待ってから返る。**起動の確認の最中に返すと、走っている着手が
	// テスト用の herdr を叩いている最中に後始末が走り、`-race` がデータ競合として落とす（20回に1回、実測）。
	waitFor(t, 15*time.Second, "着手が1回目の指示を送る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
}
