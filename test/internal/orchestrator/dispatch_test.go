package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// holdPrompt は turn を終わらせない agent.prompt の台本を入れる。
//
// 着手そのものを見るテストで使う（turn の終わりの判定は turn_test.go が見る）。
//
// fx: 対象の fixture。
func holdPrompt(fx *fixture) {
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "working"},
		}, nil
	})
}

// TestDispatch_ReadyとInProgressの両方を候補にする は、候補の取り方を確かめる。
//
// 目的: 設計 3-10 / 4-2 の「`status:Ready` だけで絞ってはならない。`In Progress` が候補に
// 含まれないと、再起動後に取り残された issue を誰も拾えなくなる」を守っていることを示す。
// 与える情報: `Ready` の issue と `In Progress` の issue が1件ずつ。同時実行の上限は2。
// 成功条件: 2件とも印が付く。
func TestDispatch_ReadyとInProgressの両方を候補にする(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.AddIssue(sampleIssue(189, "In Progress"))

	fx.Orc.Tick(context.Background())

	waitFor(t, 5*time.Second, "2件とも dispatch される", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 2
	})
}

// TestDispatch_空きスロットが尽きたらそれ以上dispatchしない は、段-1 を確かめる。
//
// 目的: 設計 3-16 の段-1「空きスロットを数える。0 なら、その巡回では以降の候補を1件も
// dispatch しない」と「この検査は印を付ける前に行う（印を付けてから弾くと、印が残る）」を示す。
// 与える情報: `Ready` の issue が3件、`agent.max_concurrent_agents` は1。
// 成功条件: 印が1件しか付かず、**弾かれた issue の Status が書き換えられていない**
// （印を付けてから弾いていれば、段2 で Status が書かれてしまう）。
func TestDispatch_空きスロットが尽きたらそれ以上dispatchしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Agent.MaxConcurrentAgents = 1
	}})
	holdPrompt(fx)
	for _, n := range []int{188, 189, 190} {
		fx.Tracker.AddIssue(sampleIssue(n, "Ready"))
	}

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1件が dispatch される", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Fatalf("印の件数が想定と違う: got %d, want 1 (%v)", got, fx.Orc.RunningIdentifiers())
	}
	inProgress := 0
	for _, n := range []string{"PVTI_item188", "PVTI_item189", "PVTI_item190"} {
		if fx.Tracker.StateOf(n) == "In Progress" {
			inProgress++
		}
	}
	if inProgress != 1 {
		t.Fatalf("空きスロットが尽きたあとの issue にも Status を書いている: In Progress が %d 件", inProgress)
	}
}

// TestDispatch_既に印を持っているissueは二重にdispatchしない は、印の役目を確かめる。
//
// 目的: 設計 3-10 の「『この issue は自分が取った』という印で防ぐ。状態の絞り込みでは防がない」を示す。
// 与える情報: `Ready` の issue を1件 dispatch したあと、もう一度巡回する。
// 成功条件: `agent.start` が1回しか呼ばれない。
func TestDispatch_既に印を持っているissueは二重にdispatchしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の dispatch が済む", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	fx.Orc.Tick(context.Background())
	time.Sleep(200 * time.Millisecond)

	if got := fx.Herdr.CountMethod(herdr.MethodAgentStart); got != 1 {
		t.Fatalf("同じ issue に2つ目の Claude Code を立てている: agent.start が %d 回", got)
	}
}

// TestTick_巡回1回のリクエストが3本を超えない は、GraphQL のコストの見積りを守ることを確かめる。
//
// 目的: 設計 3-31 / 3-6 の「巡回1回のリクエストが3本を超えない（候補の取得・実行中の照合・
// worktree の照合）」「Status の選択肢名の照合と gh の認証の検査は毎巡回では行わない」を示す。
// 与える情報: 1件 dispatch 済みの状態から、2回目の巡回を回す。
// 成功条件: 2回目の巡回での読み取りが3本以内で、`VerifyStatusOptions` が呼ばれない。
func TestTick_巡回1回のリクエストが3本を超えない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の dispatch が済む", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	fx.Tracker.ResetCalls()
	fx.Orc.Tick(context.Background())

	reads := fx.Tracker.CountCall("FetchIssuesByStates") + fx.Tracker.CountCall("FetchIssuesByIDs")
	if reads > 3 {
		t.Fatalf("巡回1回の読み取りが3本を超えた: got %d (%v)", reads, fx.Tracker.Calls())
	}
	if fx.Tracker.CountCall("VerifyStatusOptions") != 0 {
		t.Fatalf("毎巡回で Status の選択肢名を照合している: %v", fx.Tracker.Calls())
	}
}

// TestTick_verify_states_everyの頻度で選択肢名とghの認証を検査する は、検査の頻度を確かめる。
//
// 目的: 設計 3-6 の「巡回ごとに検査するもの」を、`tracker.verify_states_every` の頻度で
// 行うこと（**毎巡回で外部プロセスを起動しない**）を示す。
// 与える情報: `verify_states_every` が2、巡回を3回。
// 成功条件: 1回目と3回目に検査が走り、2回目には走らない（合計2回）。
func TestTick_verify_states_everyの頻度で選択肢名とghの認証を検査する(t *testing.T) {
	ghChecks := 0
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.VerifyStatesEvery = 2
		},
		GHAuthCheck: func(context.Context) error {
			ghChecks++
			return nil
		},
	})

	for i := 0; i < 3; i++ {
		fx.Orc.Tick(context.Background())
	}

	if got := fx.Tracker.CountCall("VerifyStatusOptions"); got != 2 {
		t.Fatalf("選択肢名の照合の回数が想定と違う: got %d, want 2 (%v)", got, fx.Tracker.Calls())
	}
	if ghChecks != 2 {
		t.Fatalf("gh の認証の検査の回数が想定と違う: got %d, want 2", ghChecks)
	}
}

// TestTick_選択肢名が合わなければその巡回のdispatchだけを飛ばす は、失敗時の扱いを確かめる。
//
// 目的: 設計 3-6 の「失敗したらその巡回の dispatch を飛ばす。**実行中の照合は止めない**」を示す。
// 与える情報: `VerifyStatusOptions` が失敗する状態で、`Ready` の issue が1件。
// 成功条件: dispatch されず、実行中の照合（`FetchIssuesByIDs`）の経路は動く。
func TestTick_選択肢名が合わなければその巡回のdispatchだけを飛ばす(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Tracker.VerifyStatesEvery = 1
	}})
	fx.Tracker.SetVerifyError(errors.New("Status の選択肢名が設定と一致しません"))
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())
	time.Sleep(200 * time.Millisecond)

	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Fatalf("検査に落ちたのに dispatch している: %v", fx.Orc.RunningIdentifiers())
	}
	if fx.Herdr.CountMethod(herdr.MethodAgentStart) != 0 {
		t.Fatalf("検査に落ちたのに agent を起動している: %v", fx.Herdr.Methods())
	}
}

// TestDispatch_未信頼のリポジトリへのコメントはリポジトリにつき1回だけ は、
// 30秒ごとにコメントが積まれないことを確かめる。
//
// 目的: 設計 3-6 の「**キーは `<owner>/<repo>`。**issue ごとではない。素朴に実装すると
// 30秒ごとに永久に積まれる」を守っていることを示す。
// 与える情報: 信頼登録していないリポジトリの issue が2件、巡回を3回。
// 成功条件: そのリポジトリへのコメントが1件だけになり、dispatch は1件も起きない。
func TestDispatch_未信頼のリポジトリへのコメントはリポジトリにつき1回だけ(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Untrusted: true})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.AddIssue(sampleIssue(189, "Ready"))

	for i := 0; i < 3; i++ {
		fx.Orc.Tick(context.Background())
	}

	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Fatalf("未信頼のリポジトリの issue を dispatch している: %v", fx.Orc.RunningIdentifiers())
	}
	total := len(fx.Tracker.CommentsOf("I_node188")) + len(fx.Tracker.CommentsOf("I_node189"))
	if total != 1 {
		t.Fatalf("未信頼の通知が %d 件ある（リポジトリにつき1回であるべき）", total)
	}
}

// TestDispatch_アダプタが未信頼と判定した_issue_にもコメントを1回残す は、
// 本物のアダプタと同じ形（Dispatchable が偽）で届いた issue にも通知が届くことを確かめる。
//
// 目的: 設計 3-33 の「そのコメントの本文に直し方を書く。**人間が実際に読むのは
// doctor の画面ではなくこのコメントである**」を守っていることを示す。
// **本物のアダプタは信頼が無いと Issue.Dispatchable を偽にして返す**ので、
// dispatchCandidates が印を付ける前に弾く。**そこで捨てると通知の経路が無くなる。**
// 与える情報: Dispatchable が偽の issue が1件、巡回を3回。
// 成功条件: コメントが1件だけ残り、dispatch は起きない。
func TestDispatch_アダプタが未信頼と判定した_issue_にもコメントを1回残す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Untrusted: true})
	issue := sampleIssue(190, "Ready")
	// **本物のアダプタが信頼の判定結果をここに畳み込む**（internal/tracker/query.go）。
	issue.Dispatchable = false
	fx.Tracker.AddIssue(issue)

	for i := 0; i < 3; i++ {
		fx.Orc.Tick(context.Background())
	}

	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Fatalf("未信頼のリポジトリの issue を dispatch している: %v", fx.Orc.RunningIdentifiers())
	}
	if got := len(fx.Tracker.CommentsOf("I_node190")); got != 1 {
		t.Fatalf("未信頼の通知が %d 件ある（リポジトリにつき1回であるべき）", got)
	}
}

// TestDispatch_draft_issue_は信頼の検査に掛けない は、
// owner も repo も持たない issue で信頼を検査しにいかないことを確かめる。
//
// 目的: draft issue は Dispatchable が偽で届くが、**リポジトリを持たない**ので
// 「信頼登録されていません」という通知は誤りである（設計 3-13）。
// 与える情報: owner と repo が空で Dispatchable が偽の issue が1件、巡回を1回。
// 成功条件: コメントが1件も出ず、落ちない。
func TestDispatch_draft_issue_は信頼の検査に掛けない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Untrusted: true})
	issue := sampleIssue(191, "Ready")
	issue.Dispatchable = false
	issue.Owner = ""
	issue.Repo = ""
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())

	if got := len(fx.Tracker.CommentsOf("I_node191")); got != 0 {
		t.Fatalf("draft issue に信頼の通知を %d 件出している", got)
	}
}

// TestDispatch_テンプレートに一覧に無い変数を書いたらその issue を失敗にする は、
// 描画の失敗の扱いを確かめる。
//
// 目的: 設計 3-8 の「`missingkey=error` を付ける。**未知の変数を書いたテンプレートは
// 描画に失敗し、その issue を失敗として扱う**（黙って空文字を埋めない）」を示す。
// 与える情報: 5-3 の一覧に無い変数（`.issue.body`）を書いたテンプレート。
// 成功条件: Status が `failure_state`（Blocked）へ落ち、印から外れる。
func TestDispatch_テンプレートに一覧に無い変数を書いたらそのissueを失敗にする(t *testing.T) {
	fx := newFixture(t, fixtureOptions{PromptTemplate: "{{.issue.identifier}} {{.issue.body}}"})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())

	waitFor(t, 5*time.Second, "issue が失敗として扱われる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})
	waitFor(t, 5*time.Second, "印から外れる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
	if fx.Herdr.CountMethod(herdr.MethodAgentPrompt) != 0 {
		t.Fatalf("描画に失敗したのにプロンプトを送っている: %v", fx.Herdr.Methods())
	}
}

// TestDispatch_起動直後にblockedならescを送ってから失敗にする は、段10 の安全弁を確かめる。
//
// 目的: 設計 3-11 の「`blocked` のまま次を投げると、保留中の権限要求が承認されて実行される
// （3/3 で再現）」を防ぐため、**次を投げる前に `agent.send_keys` で `["esc"]` を送る**ことを示す。
// 与える情報: `agent.get` が `blocked` を返す台本。
// 成功条件: `agent.send_keys` に `["esc"]` が送られ、プロンプトは1回も送られず、
// Status が `failure_state` へ落ちる。
func TestDispatch_起動直後にblockedならescを送ってから失敗にする(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type":  "agent_info",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "esc が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentSendKeys) > 0
	})

	keysParams := fx.Herdr.ParamsOf(t, herdr.MethodAgentSendKeys)
	keys, _ := keysParams["keys"].([]any)
	if len(keys) != 1 || keys[0] != "esc" {
		t.Fatalf("送ったキーが想定と違う: got %v, want [esc]", keys)
	}
	if fx.Herdr.CountMethod(herdr.MethodAgentPrompt) != 0 {
		t.Fatalf("blocked のままプロンプトを投げている（保留中の権限要求が承認される）: %v", fx.Herdr.Methods())
	}
	waitFor(t, 5*time.Second, "Status が failure_state へ落ちる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})
}

// TestDispatch_workspaceのpaneが1つでなければその issue を失敗にする は、段8 の検査を確かめる。
//
// 目的: 設計 3-16 の段8「返る pane が1つでなければ、その issue を失敗として扱う
// （人間が触った workspace かもしれない）」を守っていることを示す。
// 与える情報: `pane.list` が2つの pane を返す台本。
// 成功条件: agent を起動せず、Status が `failure_state` へ落ちる。
func TestDispatch_workspaceのpaneが1つでなければそのissueを失敗にする(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Herdr.Handle(herdr.MethodPaneList, func(params map[string]any) (any, *rpcErr) {
		id, _ := params["workspace_id"].(string)
		return map[string]any{
			"type": "pane_list",
			"panes": []any{
				map[string]any{"pane_id": id + ":p1", "workspace_id": id},
				map[string]any{"pane_id": id + ":p2", "workspace_id": id},
			},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 5*time.Second, "Status が failure_state へ落ちる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})
	if fx.Herdr.CountMethod(herdr.MethodAgentStart) != 0 {
		t.Fatalf("pane が2つあるのに agent を起動している: %v", fx.Herdr.Methods())
	}
	if !strings.Contains(fx.Logs.String(), "pane が 2 個あります") {
		t.Fatalf("pane が1つでない理由をログに残していない")
	}
}

// TestDispatch_状態ごとの上限はrunning_stateのバケツで数える は、段-1 の数え方を確かめる。
//
// 目的: 設計 3-16 の段-1「これから dispatch する候補は、`tracker.running_state`（既定
// `In Progress`）の枠を消費するものとして数える。**候補は取得した時点ではまだ `Ready` だが、
// dispatch すれば段2 で running_state へ書く。`Ready` のバケツで数えると、`In Progress` の
// 上限を越えて dispatch できてしまう**」を守っていることを示す。
//
// 与える情報: `Ready` の issue が3件。全体の上限は5、`In Progress` の上限だけが2。
// 成功条件: 印が2件で止まる。
func TestDispatch_状態ごとの上限はrunning_stateのバケツで数える(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Agent.MaxConcurrentAgents = 5
		cfg.Agent.MaxConcurrentAgentsByState = map[string]int{"In Progress": 2}
	}})
	holdPrompt(fx)
	for _, n := range []int{188, 189, 190} {
		fx.Tracker.AddIssue(sampleIssue(n, "Ready"))
	}

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "2件が dispatch される", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) >= 2
	})
	time.Sleep(300 * time.Millisecond)

	if got := len(fx.Orc.RunningIdentifiers()); got != 2 {
		t.Fatalf("状態ごとの上限を越えて dispatch している: 印が %d 件 (%v)", got, fx.Orc.RunningIdentifiers())
	}
}

// TestHandoff_worktreeを持たない_run_には調べるところを出さない は、
// 着手の途中で落ちた run に、存在しない場所を見せないことを確かめる。
//
// 目的: 設計 3-34b の「空の項目は行ごと出さない」を守っていることを示す。
// **worktree も会話の記録も無い run に空のパスを出すと、人間は存在しない場所を探しに行く。**
// 与える情報: 信頼登録されていないリポジトリの issue（worktree を作る前に弾かれる）。
// 成功条件: 投稿されたコメントに「調べるところ」の見出しが出ないこと。
func TestHandoff_worktreeを持たない_run_には調べるところを出さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Untrusted: true})
	issue := sampleIssue(192, "Ready")
	issue.Dispatchable = false
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())

	comments := fx.Tracker.CommentsOf("I_node192")
	if len(comments) != 1 {
		t.Fatalf("未信頼の通知が %d 件（1件であるべき）", len(comments))
	}
	// **未信頼の通知は引き渡しの通知とは別の経路だが、どちらも worktree を持たない。**
	// 空のパスが本文に混ざっていないことを確かめる。
	if strings.Contains(comments[0].Body, "作業していた場所: ``") {
		t.Errorf("空のパスを見せている:\n%s", comments[0].Body)
	}
	if strings.Contains(comments[0].Body, "会話の記録: ``") {
		t.Errorf("空の記録のパスを見せている:\n%s", comments[0].Body)
	}
}

// TestHandoff_引き渡しの通知に調べるところが出る は、
// worktree を持つ run では場所が示されることを確かめる。
//
// 目的: 設計 3-34b の「器には調べるところを必ず添える」を守っていることを示す。
// **理由だけを読んでも、人間は作業の跡がどこに残っているのかを知る手立てがない。**
// 与える情報: max_dispatch_turns を1にして、1回目の turn の終わりで上限に達する run。
// 成功条件: コメントに「【調べるところ】」と worktree のパスが入っていること。
func TestHandoff_引き渡しの通知に調べるところが出る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Agent.MaxDispatchTurns = 1
	}})
	fx.Tracker.AddIssue(sampleIssue(193, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "まだ途中です", false),
	})

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle"},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 15*time.Second, "引き渡しの通知が投稿される", func() bool {
		for _, c := range fx.Tracker.CommentsOf("I_node193") {
			if strings.Contains(c.Body, "人間へ引き渡しました") {
				return true
			}
		}
		return false
	})

	var body string
	for _, c := range fx.Tracker.CommentsOf("I_node193") {
		if strings.Contains(c.Body, "人間へ引き渡しました") {
			body = c.Body
		}
	}
	if !strings.Contains(body, "【調べるところ】") {
		t.Fatalf("調べるところの見出しが無い:\n%s", body)
	}
	if !strings.Contains(body, "作業していた場所:") {
		t.Errorf("worktree のパスが出ていない:\n%s", body)
	}
}

// TestHandoff_ログには理由の1行目だけを出す は、
// 巡回のたびに数行の案内がログへ流れないことを確かめる。
//
// 目的: 設計 3-34b の「ログは原因まで。対処は主要なものだけ」を守っていることを示す。
// **issue のコメントには【確かめ方】まで載せるが、同じ文字列をログへ流すと他の行が埋もれる。**
// 与える情報: max_dispatch_turns を1にして、1回目の turn の終わりで人間へ引き渡す run。
// 成功条件: ログに【確かめ方】が出ないこと。コメントには出ること。
func TestHandoff_ログには理由の1行目だけを出す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Agent.MaxDispatchTurns = 1
	}})
	fx.Tracker.AddIssue(sampleIssue(194, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "まだ途中です", false),
	})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle"},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 15*time.Second, "引き渡しの通知が投稿される", func() bool {
		for _, c := range fx.Tracker.CommentsOf("I_node194") {
			if strings.Contains(c.Body, "人間へ引き渡しました") {
				return true
			}
		}
		return false
	})

	// **コメントには案内が入る。**
	var body string
	for _, c := range fx.Tracker.CommentsOf("I_node194") {
		if strings.Contains(c.Body, "人間へ引き渡しました") {
			body = c.Body
		}
	}
	if !strings.Contains(body, "【対処】") {
		t.Fatalf("コメントに直し方が入っていない:\n%s", body)
	}

	// **ログには入らない。**1行目だけを出す（summaryLine）。
	if strings.Contains(fx.Logs.String(), "【確かめ方】") || strings.Contains(fx.Logs.String(), "【対処】") {
		t.Errorf("ログに案内の行まで流れている（1行目だけであるべき）:\n%s", fx.Logs.String())
	}
}
