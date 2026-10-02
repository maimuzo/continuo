// {"RUCM-CFG-SHA256": "8e3c0293a933691f445c37ddb3a773d6e1128ac94ecc2fbb70466b101814a43d", "SOURCE": "docs/spec/usecases/particular_case/directchatのpaneを用意する.cfg.json"}
//
// **ユースケース記述「directchatのpaneを用意する」の経路に対応づけたテストである。**
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
// Test_directchatのpaneを用意する_P001_Statusごとの上限に達していてもpaneを用意する は、設計 3-83c の門5 を確かめる。
//
// 目的: **門5 は全体の上限（`agent.max_concurrent_agents`）だけを見る。**用意する pane は
// `running_state` の枠を消費しないので、Status ごとの上限を当てると、全体が空いていても pane が来ない。
// 与える情報: `agent.max_concurrent_agents_by_state` の `In Progress` を1にし、1件が `In Progress` で走っている。
// そこへ担当者が自分1人の issue を direct chat へ置く。
// 成功条件: direct chat の pane を用意し、「話しかけられます」の案内を書くこと。
func Test_directchatのpaneを用意する_P001_Statusごとの上限に達していてもpaneを用意する(t *testing.T) {
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

// {"RUCM-PATH": "P002"}
//
// Test_directchatのpaneを用意する_P002_用意中に担当者が替わったら自分で開いたpaneを閉じて印を外す は、設計 3-83d の外れ方の表の3行目を確かめる。
//
// 目的: 用意の最中に担当者が別の1人に替わったら、その machine は pane を持ってはならない（3-83h）。
// **自分で開いた pane を ID で閉じ、印を外す。**失敗としては数えず、コメントも書かない。
// 与える情報: `agent.start` の最中に担当者を他人1人に替えた用意。
// 成功条件: pane を閉じ、印を外し、案内のコメントも Status の書き込みも無いこと。
func Test_directchatのpaneを用意する_P002_用意中に担当者が替わったら自分で開いたpaneを閉じて印を外す(t *testing.T) {
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

// {"RUCM-PATH": "P003"}
//
// Test_directchatのpaneを用意する_P003_用意中に戻されたときClaudeCodeが既に動いていればturnの終わりを待ってから1回目の本文を送る は、
// 設計 3-83d の外れ方の表の1行目を確かめる。
//
// 目的: 用意の段2 が「Claude Code は既に動いている」（`ErrStartupBusy`）に着地した run を、用意の最中に人間が
// 作業中の Status へ戻すことがある。**送る印を立てると、走っている turn へ1回目の本文が投げられ、turn が混ざる。**
// 通常の着手のその道と同じく、turn の終わりを待ってから送る。
// 与える情報: `agent.get` が `agent_not_found` を返しながら作業中の hook を流す用意。起動の確認の最中にカードを
// `dispatch_state` へ戻す。
// 成功条件: `running_state` を書いたあと、巡回を回しても指示を送らないこと。`Stop` が届いたら1回目の本文を送ること。
func Test_directchatのpaneを用意する_P003_用意中に戻されたときClaudeCodeが既に動いていればturnの終わりを待ってから1回目の本文を送る(t *testing.T) {
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

// {"RUCM-PATH": "P004"}
//
// Test_directchatのpaneを用意する_P004_用意中に作業中へ戻されたら1回目の本文を送る は、設計 3-83d の外れ方の表の1行目を確かめる。
//
// 目的: 用意は最大2分かかり、巡回は30秒である。その間に人間がカードを戻すことがある。
// **確かめずに direct chat へ入れると、1回目の本文を1度も受け取っていないエージェントへ継続の指示だけが届く。**
// 与える情報: `agent.start` の最中にカードを `dispatch_state` へ戻した用意。
// 成功条件: 案内のコメントを書かず、`running_state` を書き、1回目の本文を送ること。
func Test_directchatのpaneを用意する_P004_用意中に作業中へ戻されたら1回目の本文を送る(t *testing.T) {
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

// {"RUCM-PATH": "P009"}
//
// Test_directchatのpaneを用意する_P009_用意の段2が段8より前で落ちたらworktreeOpenが開いたpaneを閉じる は、設計 3-83d の用意の段2 を確かめる。
//
// 目的: `worktree.open`（着手の段3）の時点で herdr は pane を開いている。**着手の段8 まで控えないと、
// 段4〜段8 で落ちたときに後始末が閉じる相手を知らず、シェルの pane が残る。**残った pane は門4 に当たり続けるので、
// 用意し直されず、上限の書く経路にも届かない。
// 与える情報: 着手の段8 の `resolvePane` が落ちる用意（workspace の中に pane が2枚）。
// 成功条件: 印を外し、`worktree.open` が開いた pane を `pane.close` で閉じること。
func Test_directchatのpaneを用意する_P009_用意の段2が段8より前で落ちたらworktreeOpenが開いたpaneを閉じる(t *testing.T) {
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

// {"RUCM-PATH": "P010"}
//
// Test_directchatのpaneを用意する_P010_用意が落ちた直後の巡回ではやり直さない は、設計 3-83c の門7 を確かめる。
//
// 目的: 用意が落ちたら、次に試すまで通常の着手のバックオフと同じ間隔を空ける。
// **空けないと、30秒ごとに枠を取っては落ちるのを繰り返す。**
// 与える情報: `agent.start` が必ず失敗する用意。上限は既定（3回）。
// 成功条件: 1回落ちたあとの巡回で `agent.start` を投げ直さず、Status も動かさないこと。
func Test_directchatのpaneを用意する_P010_用意が落ちた直後の巡回ではやり直さない(t *testing.T) {
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

// {"RUCM-PATH": "P013"}
//
// Test_directchatのpaneを用意する_P013_用意の失敗が上限を超えたらfailure_stateへ動かして理由を書く は、設計 3-83d の用意の段2 を確かめる。
//
// 目的: 用意が落ちたら、カンバンへは書かず、自分で開いた pane を閉じ、印を外す。
// **通常の着手と同じ回数の上限（`agent.max_retries`）を超えたら、書く経路で `failure_state` を書き、
// 落ちた理由をコメントする**（人間が了承した形）。担当者を直せとは書かない。
// **比べ方は通常の着手と同じ「超えたら」なので、`agent.max_retries: 0` なら1回目の失敗で書く。**
// **書くのは次の巡回の門7 である**（用意の段2 は数えるだけ。書く場所を1つにして重ならないようにする）。
// 与える情報: `agent.max_retries` を0にし、`agent.start` が必ず失敗する用意。
// 成功条件: 落ちた巡回ではカードを動かさない。次の巡回で pane を閉じ、印を外し、Status が `failure_state` になり、
// 「用意できませんでした」が1件。
func Test_directchatのpaneを用意する_P013_用意の失敗が上限を超えたらfailure_stateへ動かして理由を書く(t *testing.T) {
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

// {"RUCM-PATH": "P013"}
//
// Test_directchatのpaneを用意する_P013_上限を超えたときに書けなかったら次の巡回で用意せずに書き直す は、設計 3-83c の門7 を確かめる。
//
// 目的: 用意の失敗が上限を超えた issue へ門7 が書く `failure_state` の書き込みが失敗しても、**次の巡回で書き直す。**
// **書き直すまで用意はやり直さない**（やり直すと、上限を超えたあとも pane を開いては閉じる）。
// 与える情報: `agent.max_retries` を0にし、`agent.start` が必ず失敗する用意。2回目の巡回（門7 が初めて書く）では
// `UpdateStatus` が失敗する。
// 成功条件: 3回目の巡回で Status が `failure_state` になり、理由のコメントが1件で、`agent.start` は1回のまま。
func Test_directchatのpaneを用意する_P013_上限を超えたときに書けなかったら次の巡回で用意せずに書き直す(t *testing.T) {
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

// {"RUCM-PATH": "P013"}
//
// Test_directchatのpaneを用意する_P013_上限を超えたissueへ書いている最中の巡回では2本目を立てない は、設計 3-83c の門7 を確かめる。
//
// 目的: 上限を超えた issue へ書く経路は門7 の1箇所だけが走らせ、**書いている最中は次の巡回で2本目を立てない。**
// 書き込みが巡回の間隔より長くかかると、2本が並んで書き、コメントが2件付きうる。
// 与える情報: `agent.max_retries` を0にし、`agent.start` が必ず失敗する用意。門7 の1本目の `UpdateStatus` を止めておき、
// その間にもう1回巡回を回す。
// 成功条件: 止めている間に `UpdateStatus` が1回も記録されないこと（1本目は関門の手前で待っているので記録されない）。
// 放したあと Status が `failure_state` になり、理由のコメントが1件であること。
func Test_directchatのpaneを用意する_P013_上限を超えたissueへ書いている最中の巡回では2本目を立てない(t *testing.T) {
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

// {"RUCM-PATH": "P013"}
//
// Test_directchatのpaneを用意する_P013_上限を超えた書き込みに成功したら記録を消す は、設計 3-83d の用意の段2 を確かめる。
//
// 目的: 門7 の書く経路が実際に書けたら、用意の失敗の記録を消す。**残すと、索引の遅れでカードがまだ候補に見える間に
// 人間が direct chat へ戻したとき、1回も用意し直さずにまた `failure_state` へ落とす。**
// 与える情報: `agent.max_retries` を0にし、1回目の用意を落とす。門7 が `failure_state` を書いたあと、
// 候補から外れる巡回を挟まずに人間が direct chat へ戻す。そのときは用意が通る。
// 成功条件: 戻した巡回で用意し（「話しかけられます」が1件）、「用意できませんでした」は1件のままであること。
func Test_directchatのpaneを用意する_P013_上限を超えた書き込みに成功したら記録を消す(t *testing.T) {
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

// {"RUCM-PATH": "P014"}
//
// Test_directchatのpaneを用意する_P014_worktreeのworkspaceにcwdの違うpaneがあれば用意しない は、設計 3-83c の門4 を確かめる。
//
// 目的: 門4 は、用意の段2 が pane を引くのと同じ見方で「pane が1枚でもあるか」を見る。用意の段2 の `resolvePane` は
// `worktree.open` が返した workspace の中の1枚を cwd を見ずに使う。**門4 が cwd だけで見ると、人間がその workspace の
// pane で別のディレクトリへ移っていたときに門4 を通り抜け、その pane へ `agent.start` が届く。**
// 与える情報: 1回目の用意が段8 で落ちて worktree の workspace が開いたまま残った issue。その workspace に、
// cwd が別のディレクトリの pane が1枚ある。
// 成功条件: `agent.start` を投げないこと。その pane が無くなった巡回では用意すること（検査が空振りでない証拠）。
func Test_directchatのpaneを用意する_P014_worktreeのworkspaceにcwdの違うpaneがあれば用意しない(t *testing.T) {
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

// {"RUCM-PATH": "P019"}
//
// Test_directchatのpaneを用意する_P019_担当者が1人でないカードへ書いている最中の巡回では2本目を立てない は、設計 3-83h の書く経路を確かめる。
//
// 目的: 担当者の人数による書き込みは、門3 と巡回の段1 が巡回ごとに立てる。**書いている最中は次の巡回で2本目を立てない。**
// 書き込みが巡回の間隔より長くかかると、2本が並んで書き、同じ機械がコメントを2件付けうる。
// 与える情報: 担当者が0人の direct chat のカード。1本目の `UpdateStatus` を止めておき、その間にもう1回巡回を回す。
// 成功条件: 止めている間に `UpdateStatus` が1回も記録されないこと。放したあと Status が `failure_state` になり、
// 「担当者を1人に」が1件であること。
func Test_directchatのpaneを用意する_P019_担当者が1人でないカードへ書いている最中の巡回では2本目を立てない(t *testing.T) {
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

// {"RUCM-PATH": "P017"}
//
// Test_directchatのpaneを用意する_P017_担当者が1人で他人の候補には何もしない は、3-83h の判定の表の順3（印を持っていない機械）を確かめる。
//
// 目的: どの機械が pane を持つかは担当者だけで決まる。**他人のアカウントが担当なら、この機械は何もしない。**
// 与える情報: Status が direct chat で担当者が1人（他人）の候補。
// 成功条件: Status が動かず、コメントも書かず、印も付けないこと。
func Test_directchatのpaneを用意する_P017_担当者が1人で他人の候補には何もしない(t *testing.T) {
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

// {"RUCM-PATH": "P018"}
//
// Test_directchatのpaneを用意する_P018_ログイン名が取れない巡回では候補に何もしない は、3-83h の判定の表の順2 を確かめる。
//
// 目的: 自分が誰か分からないまま pane を用意しない（印を持っていない機械は、この巡回では何もしない）。
// 与える情報: gh の持ち主を取れない状態で、担当者1人の direct chat の候補。
// 成功条件: 印を付けず、Status も動かさないこと。
func Test_directchatのpaneを用意する_P018_ログイン名が取れない巡回では候補に何もしない(t *testing.T) {
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

// {"RUCM-PATH": "P019"}
//
// Test_directchatのpaneを用意する_P019_担当者が0人の候補はfailure_stateへ動かしてコメントを1件書く は、
// 3-83h の判定の表の順1（印を持っていない機械）を確かめる。
//
// 目的: 人間の決定「担当者が1人だけの状態以外で direct chat に移したら、エラーとして blocked に遷移して良い。
// その際、担当者を1人だけ設定する旨をコメントに書いておいて」を示す。
//
// 与える情報: Status が direct chat で担当者が0人の候補（continuo は印を持っていない）。
// 成功条件: Status が `failure_state` になり、「担当者を1人に」のコメントがちょうど1件あり、
// 印を1つも付けていない（pane を用意しにいかない）こと。
func Test_directchatのpaneを用意する_P019_担当者が0人の候補はfailure_stateへ動かしてコメントを1件書く(t *testing.T) {
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

// {"RUCM-PATH": "P019"}
//
// Test_directchatのpaneを用意する_P019_担当者が2人の候補もfailure_stateへ動かす は、3-83h の判定の表の順1（2人以上）を確かめる。
//
// 目的: 「複数人は NG」（人間の決定）を示す。自分が含まれていても NG である。
// 与える情報: Status が direct chat で担当者が2人（自分と他人）の候補。
// 成功条件: Status が `failure_state` になり、コメントが1件あること。
func Test_directchatのpaneを用意する_P019_担当者が2人の候補もfailure_stateへ動かす(t *testing.T) {
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

// {"RUCM-PATH": "P019"}
//
// Test_directchatのpaneを用意する_P019_2台が同時に書いてもコメントは実際に書いた1台だけ は、3-83h の「`Wrote` のときだけコメント」を確かめる。
//
// 目的: 見張っている全台が書こうとするが、実際に書けるのは取り直しの時点で先に書いた1台である。
// **`Reached`（既にその値だった）や、取り直すと direct chat でなかったときにはコメントを書かない。**
// 与える情報: 同じカンバンを見張る2台。担当者0人の direct chat の候補。1台目の書き込みを止めておき、
// そのあいだに2台目が書き、1台目を進ませる。
// 成功条件: Status が `failure_state` になり、「担当者を1人に」のコメントがちょうど1件であること。
func Test_directchatのpaneを用意する_P019_2台が同時に書いてもコメントは実際に書いた1台だけ(t *testing.T) {
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

// {"RUCM-PATH": "P019"}
//
// Test_directchatのpaneを用意する_P019_未設定のStatusへは書く経路は書かない は、3-83e の「取り直した値が未設定なら書かない」を確かめる。
//
// 目的: 人間が Status を外した item に `Blocked` を付けない。
// 与える情報: 担当者0人の direct chat の候補。書き込みを止めているあいだに人間が Status を外す。
// 成功条件: Status が未設定のままで、コメントも書かないこと。
func Test_directchatのpaneを用意する_P019_未設定のStatusへは書く経路は書かない(t *testing.T) {
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

// assertNotPrepared は、巡回のあとに候補へ印を付けておらず、pane も用意していないことを確かめる。
//
// **印は巡回のループの中で同期に付く**（用意の段1）ので、巡回が返った時点で見れば足りる。
//
// fx: fixture。
// id: 候補の project item の ID。
// runs: 巡回の前から在った run の数。
// starts: 巡回の前の `agent.start` の回数。
func assertNotPrepared(t *testing.T, fx *fixture, id string, runs, starts int) {
	t.Helper()
	if got := fx.Orc.RunningIdentifiers(); len(got) != runs {
		t.Fatalf("用意しないはずの候補に印を付けた: %v", got)
	}
	if n := fx.Herdr.CountMethod(herdr.MethodAgentStart); n != starts {
		t.Fatalf("用意しないはずの候補に Claude Code を起動した: agent.start が %d 回から %d 回へ", starts, n)
	}
	if got := fx.Tracker.StateOf(id); got != humanState {
		t.Fatalf("用意しないだけの候補のカードを動かした: %q", got)
	}
}

// assertGateLogged は、候補がその門で止まったことを、門が出すログの文面で確かめる。
//
// **印が付いていないことだけを見ると、手前の別の門で止まっていても通る。**どの門で止まったかを見分ける。
//
// fx: fixture。
// want: その門だけが出すログの文面の一部。
func assertGateLogged(t *testing.T, fx *fixture, want string) {
	t.Helper()
	if !strings.Contains(fx.Logs.String(), want) {
		t.Fatalf("前提が崩れている（狙った門に当たっていない。探した文面: %s）:\n%s", want, fx.Logs.String())
	}
}

// {"RUCM-PATH": "P020"}
//
// Test_directchatのpaneを用意する_P020_draftIssueの候補には印を付けない は、設計 3-83c の門2 を確かめる。
//
// 目的: draft issue は worktree を作れないので、用意は必ず落ちる。門で落とさないと、巡回のたびに枠を取っては落ちる。
// 与える情報: 担当者が自分1人で、Status が direct chat の、Owner と Repo が空の候補。
// 成功条件: 印を付けず、`agent.start` を投げず、カードを動かさないこと。
func Test_directchatのpaneを用意する_P020_draftIssueの候補には印を付けない(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	issue := sampleIssue(370, humanState)
	issue.Owner, issue.Repo = "", ""
	fx.Tracker.AddIssue(issue)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)

	fx.Orc.Tick(context.Background())

	assertNotPrepared(t, fx, issue.ID, 0, 0)
	assertGateLogged(t, fx, "draft issue なので、direct chat の pane は用意できません")
	if n := fx.Tracker.CountCall("UpdateStatus"); n != 0 {
		t.Fatalf("draft issue の候補へ書きに行った: %d 回", n)
	}
}

// {"RUCM-PATH": "P016"}
//
// Test_directchatのpaneを用意する_P016_paneの一覧を引けない巡回では用意しない は、設計 3-83c の門4 を確かめる。
//
// 目的: 「引けなかった」と「pane が無い」を混ぜない。混ぜると、herdr の socket が一瞬落ちただけで、
// 人間が話している pane の隣に2枚目を開く。
// 与える情報: `pane.list` が誤りを返す巡回。そのあと `pane.list` が返るようになった巡回。
// 成功条件: 引けない巡回では印を付けないこと。引けるようになった巡回では用意すること（検査が空振りでない証拠）。
func Test_directchatのpaneを用意する_P016_paneの一覧を引けない巡回では用意しない(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	fx.AllowLog("pane か workspace の一覧を取れない", "一覧を引けません", "pane の一覧")
	var mu sync.Mutex
	failing := true
	fx.Herdr.Handle(herdr.MethodPaneList, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		defer mu.Unlock()
		if failing {
			return nil, &rpcErr{Code: "internal_error", Message: "一覧を引けません"}
		}
		panes := []any{}
		if id, _ := params["workspace_id"].(string); id != "" {
			panes = append(panes, map[string]any{"pane_id": id + ":p1", "workspace_id": id, "agent_status": "idle"})
		}
		return map[string]any{"type": "pane_list", "panes": panes}, nil
	})
	id, node := addOwnDirectChatIssue(fx, 371)

	fx.Orc.Tick(context.Background())
	assertNotPrepared(t, fx, id, 0, 0)
	assertGateLogged(t, fx, "pane か workspace の一覧を取れないので、direct chat の pane は用意しません")

	mu.Lock()
	failing = false
	mu.Unlock()
	waitFor(t, 15*time.Second, "一覧を引けるようになった巡回で用意する", func() bool {
		fx.Orc.Tick(context.Background())
		return commentsContaining(fx.Tracker, node, readyCommentMarker) == 1
	})
}

// {"RUCM-PATH": "P015"}
//
// Test_directchatのpaneを用意する_P015_worktreeの置き場所が決まらない候補は用意しない は、設計 3-83c の門4 を確かめる。
//
// 目的: 置き場所を決められないときは「分からない」なので、触らない側に倒す。
// 与える情報: 展開すると誤りになる `herdr.worktree.branch_template`（存在しない項目を引く）。
// 成功条件: 印を付けず、`agent.start` を投げず、カードを動かさないこと。
func Test_directchatのpaneを用意する_P015_worktreeの置き場所が決まらない候補は用意しない(t *testing.T) {
	fx := newDirectChatFixture(t, func(cfg *config.Config) {
		cfg.Herdr.Worktree.BranchTemplate = "continuo/{{.issue.number}}/{{.issue.missing.field}}"
	})
	fx.AllowLog("worktree の置き場所を決められない")
	id, _ := addOwnDirectChatIssue(fx, 372)

	fx.Orc.Tick(context.Background())

	assertNotPrepared(t, fx, id, 0, 0)
	assertGateLogged(t, fx, "direct chat の worktree の置き場所を決められない")
}

// {"RUCM-PATH": "P012"}
//
// Test_directchatのpaneを用意する_P012_空きスロットが無ければ用意しない は、設計 3-83c の門5 を確かめる。
//
// 目的: direct chat の pane も `agent.max_concurrent_agents` の枠を1つ使う。枠が尽きていたら用意しない。
// 与える情報: `agent.max_concurrent_agents` を1にし、1件が走っている。そこへ担当者が自分1人の direct chat の候補を置く。
// 成功条件: 候補に印を付けず、`agent.start` を投げず、カードを動かさないこと。
func Test_directchatのpaneを用意する_P012_空きスロットが無ければ用意しない(t *testing.T) {
	fx := newDirectChatFixture(t, func(cfg *config.Config) { cfg.Agent.MaxConcurrentAgents = 1 })
	holdPrompt(fx)
	running := sampleIssue(373, "Ready")
	fx.Tracker.AddIssue(running)
	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "1件目が走る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	starts := fx.Herdr.CountMethod(herdr.MethodAgentStart)

	id, _ := addOwnDirectChatIssue(fx, 374)
	fx.Orc.Tick(context.Background())

	assertNotPrepared(t, fx, id, 1, starts)
	assertGateLogged(t, fx, "空きスロットが無いので、direct chat の pane を用意しません")
}

// {"RUCM-PATH": "P011"}
//
// Test_directchatのpaneを用意する_P011_信頼登録の無いリポジトリの候補は用意しない は、設計 3-83c の門6 を確かめる。
//
// 目的: 着手の直前の検査（`preflight`）に落ちた候補には印を付けない。
// 与える情報: リポジトリを信頼登録していない fixture と、担当者が自分1人の direct chat の候補。
// 成功条件: 印を付けず、`agent.start` を投げず、カードを動かさないこと。
func Test_directchatのpaneを用意する_P011_信頼登録の無いリポジトリの候補は用意しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Untrusted: true,
		Mutate:    func(cfg *config.Config) { cfg.Tracker.DirectChatState = humanState },
	})
	fx.Tracker.SetStatusOptions(directChatBoardOptions...)
	fx.AllowLog("信頼")
	id, _ := addOwnDirectChatIssue(fx, 375)

	fx.Orc.Tick(context.Background())

	assertNotPrepared(t, fx, id, 0, 0)
	assertGateLogged(t, fx, "リポジトリが Claude Code に信頼登録されていません")
}

// {"RUCM-PATH": "P007"}
//
// Test_directchatのpaneを用意する_P007_用意を終える前にカードを取り直せなければpaneを閉じて印を外す は、
// 設計 3-83d の用意の段3 を確かめる。
//
// 目的: 用意を終える前の取り直しが失敗したら、カードがまだ direct chat に在るかを確かめられない。
// 確かめずに入れず、自分で開いた pane を閉じて印を外す（次の巡回でやり直す）。
// 与える情報: `agent.start` の最中に、ID 指定の取り直しが失敗するようにした用意。巡回は回さない。
// 成功条件: pane を閉じ、印を外し、案内のコメントを書かず、カードを動かさないこと。
func Test_directchatのpaneを用意する_P007_用意を終える前にカードを取り直せなければpaneを閉じて印を外す(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	fx.AllowLog("カードを取り直せません")
	release, entered := holdAgentStart(t, fx)
	id, node := addOwnDirectChatIssue(fx, 376)

	fx.Orc.Tick(context.Background())
	<-entered
	fx.Tracker.SetIDsError(errors.New("GraphQL が一時的に失敗しました"))
	release()

	waitFor(t, 10*time.Second, "自分で開いた pane を閉じる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodPaneClose) == 1
	})
	waitFor(t, 5*time.Second, "印を外す", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
	assertGateLogged(t, fx, "direct chat の用意を終える前にカードを取り直せません")
	if n := commentsContaining(fx.Tracker, node, readyCommentMarker); n != 0 {
		t.Errorf("カードを取り直せなかったのに「話しかけられます」を書いた: %d 件", n)
	}
	if got := fx.Tracker.StateOf(id); got != humanState {
		t.Errorf("カードを取り直せなかっただけでカードを動かした: %q", got)
	}
	if n := fx.Tracker.CountCall("UpdateStatus"); n != 0 {
		t.Errorf("カードを取り直せなかっただけで Status を書いた: %d 回", n)
	}
}

// {"RUCM-PATH": "P006"}
//
// Test_directchatのpaneを用意する_P006_用意中にrunning_stateへ戻されたらStatusを書かずに1回目の本文を送る は、
// 設計 3-83d の外れ方の表の1行目のうち、戻した先が `running_state` の場合を確かめる。
//
// 目的: 戻した先が `dispatch_state` でなければ、`running_state` を書きに行かない（書く先が既にその値である）。
// 1回目の本文は、戻した先がどちらでも送る。
// 与える情報: `agent.start` の最中にカードを `running_state` へ戻した用意。
// 成功条件: `UpdateStatus` を1回も呼ばず、1回目の本文を送り、案内のコメントを書かないこと。
func Test_directchatのpaneを用意する_P006_用意中にrunning_stateへ戻されたらStatusを書かずに1回目の本文を送る(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	release, entered := holdAgentStart(t, fx)
	prompts := recordPrompts(fx)
	id, node := addOwnDirectChatIssue(fx, 377)

	fx.Orc.Tick(context.Background())
	<-entered
	fx.Tracker.SetState(id, fx.Config.Tracker.RunningState)
	release()

	waitFor(t, 10*time.Second, "1回目の指示を送る", func() bool {
		fx.Orc.Tick(context.Background())
		return len(prompts()) >= 1
	})
	if got := prompts()[0]; !strings.Contains(got, firstPromptMarker) {
		t.Errorf("用意中に戻された run へ1回目の本文を送っていない: %q", got)
	}
	if n := fx.Tracker.CountCall("UpdateStatus"); n != 0 {
		t.Errorf("戻した先が running_state なのに Status を書きに行った: %d 回", n)
	}
	if n := commentsContaining(fx.Tracker, node, readyCommentMarker); n != 0 {
		t.Errorf("カードはもう direct chat に無いのに「話しかけられます」を書いた: %d 件", n)
	}
}
