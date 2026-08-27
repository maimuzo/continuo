// {"RUCM-CFG-SHA256": "4617dc72733c20141806eb53c3f5534fd0ae158f541e37fc9e82dbd64ecdf1af", "SOURCE": "docs/spec/usecases/particular_case/issue を1件処理する.cfg.json"}
//
// **候補の取り方と、着手を取りやめる経路の検査である。**
//
// **候補の一覧は GitHub のサーバ側の検索結果であり、そのまま信じてはならない**（設計 3-34）。
// 直前に書いた Status が索引へ反映される前に取り直すと、頼んだ Status に無い item が返る。
package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
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

// {"RUCM-PATH": "P041"}
//
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

// {"RUCM-PATH": "P021"}
//
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
	// **後始末まで待つ。**Status は worker を止める前に書かれる（helpers_test.go の WaitRunsDrained）。
	fx.WaitRunsDrained(t, 10*time.Second)
	waitFor(t, 5*time.Second, "印から外れる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
	if fx.Herdr.CountMethod(herdr.MethodAgentPrompt) != 0 {
		t.Fatalf("描画に失敗したのにプロンプトを送っている: %v", fx.Herdr.Methods())
	}
}

// {"RUCM-PATH": "P024"}
//
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
	// **後始末まで待つ。**Status は worker を止める前に書かれる（helpers_test.go の WaitRunsDrained）。
	fx.WaitRunsDrained(t, 10*time.Second)
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
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
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
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
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

// TestDispatch_起動直後のunknownは待って通す は、設計 3-16 の段10 を確かめる。
//
// **これが実運用で issue を止めていた**（2026-08-21、設計 6-2）。
// **herdr の socket API の `agent.start` は起動が終わるのを待たずに返る**ので、
// 返った直後の `agent_status` は必ず `unknown` である。Claude Code はそのあと数秒かけて
// 立ち上がり `idle` になる。**そこで即座に諦めると、正常な起動を毎回「失敗」と呼ぶ。**
//
// 目的: 最初に `unknown` を返しても、そのあと `idle` になれば turn を送ること。
// 与える情報: 1回目だけ `unknown`、2回目以降は `idle` を返す `agent.get` の台本。
// 成功条件: **プロンプトが送られること**（諦めていない）。
func TestDispatch_起動直後のunknownは待って通す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	var calls atomic.Int32
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		status := "idle"
		if calls.Add(1) == 1 {
			// **`agent.start` が返った直後は必ずこれである。**
			status = "unknown"
		}
		return map[string]any{
			"type": "agent_info",
			// **`interactive_ready` は本物の herdr が `idle` と一緒に返す**（設計 6-2）。
			// 台本でこれを落とすと、continuo は「まだ入力を受け付けられない」と判定する。
			"agent": map[string]any{
				"name": params["target"], "agent_status": status,
				"interactive_ready": status == "idle" || status == "done",
			},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 10*time.Second, "turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	if got := fx.Tracker.StateOf("PVTI_item188"); got == "Blocked" {
		t.Fatalf("起動直後の unknown で諦めている: state=%s", got)
	}
}

// TestDispatch_unknownのまま期限を過ぎたら人間へ渡さず試し直す は、打ち切りの側を確かめる。
//
// 目的: `herdr.startup_timeout_ms` を過ぎても `unknown` のままなら諦めること。
// **ただし人間へは渡さない**（`ErrStartupRetryable` を包むので、バックオフして試し直す）。
// 与える情報: `agent.get` が常に `unknown` を返す台本。`agent.max_retries` は既定（3回）。
// 成功条件: worker は止まるが、**Status が `failure_state` へ落ちない**（リトライが残っている）。
func TestDispatch_unknownのまま期限を過ぎたら人間へ渡さず試し直す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type":  "agent_info",
			"agent": map[string]any{"name": params["target"], "agent_status": "unknown"},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	// **worker を止めるところまでは進む**（バックオフに入るため。stopWorker は pane を閉じる）。
	waitFor(t, 5*time.Second, "worker が止まる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodPaneClose) > 0
	})
	// **ここが本題である。**リトライが残っているうちは人間へ渡さない。
	if got := fx.Tracker.StateOf("PVTI_item188"); got == "Blocked" {
		t.Fatalf("unknown を1回受けただけで人間へ渡している（設計 3-16 の段10 は「試し直す」）: state=%s", got)
	}
	if fx.Herdr.CountMethod(herdr.MethodAgentPrompt) != 0 {
		t.Fatalf("unknown のままプロンプトを投げている: %v", fx.Herdr.Methods())
	}
}

// {"RUCM-PATH": "P033"}
//
// TestDispatch_failure_stateのissueをrunning_stateへ上書きしない は、段2 の許可リストを確かめる。
//
// **候補の一覧は GitHub のサーバ側の検索結果である。**continuo が直前に書いた Status が
// 索引へ反映される前に取り直すと、failure_state へ落としたばかりの issue が
// そのまま候補として返る。段2 が取り直しをしないと、
// **人間が Blocked に置いた issue を continuo が In Progress へ上書きしてしまう。**
//
// 目的: ボードの Status が failure_state にある issue へ running_state を書かないこと。
// また、書かなかったときに段3 へ進まず、印を静かに外すこと。
// 与える情報: ボードでは Blocked にあるのに、候補の写しでは Ready を名乗る issue。
// 成功条件: Status が Blocked のままで、書き込みそのものを試みず、worktree も開かず、
// 印が残らないこと。
func TestDispatch_failure_stateのissueをrunning_stateへ上書きしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	// ボードの実体は Blocked である（人間が置いた、あるいは直前に落とした）。
	fx.Tracker.AddIssue(sampleIssue(188, "Blocked"))
	// 候補の一覧にだけ、反映が追いついていない Ready の写しが載る。
	fx.Tracker.SetExtraCandidates(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())

	// **段2 の取り直しが走ったことを待ち合わせの目印にする。**許可リストに落ちる場合、
	// continuo は UpdateStatus を1回も呼ばないので、そちらでは待てない。
	waitFor(t, 10*time.Second, "着手の試みが終わる", func() bool {
		return fx.Tracker.CountCall("FetchIssuesByIDs") > 0
	})
	time.Sleep(500 * time.Millisecond)

	if got := fx.Tracker.CountCall("UpdateStatus"); got != 0 {
		t.Errorf("active_states に無い issue へ書き込みを試みている: UpdateStatus を %d 回呼んだ", got)
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "Blocked" {
		t.Errorf("failure_state の issue を上書きしている: got %q, want Blocked", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodWorktreeOpen); got != 0 {
		t.Errorf("Status を書かなかったのに段3 へ進んでいる: worktree.open を %d 回呼んだ", got)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Errorf("印が残っている: %d 件", got)
	}
	if got := len(fx.Tracker.CommentsOf("I_node188")); got != 0 {
		t.Errorf("何も起きていないのに issue へコメントしている: %d 件", got)
	}
}

// {"RUCM-PATH": "P039"}
//
// TestDispatch_同じ理由で失敗し続けるissueは上限を超えたら拾わない は、
// issue 単位の失敗の記録を確かめる。
//
// **印（run）は失敗のたびに消えるので、印の中のリトライの回数では止まらない。**
// 次の巡回が0回目として拾い直し、同じ失敗を30秒ごとに繰り返す。
//
// 目的: 同じ issue が `agent.max_retries` を超えて失敗したら、それ以上拾わないこと。
// 与える情報: ボードへ1バイトも書けない状況（failure_state へも落とせないので、
// issue は Ready のまま候補に上がり続ける）と、`agent.max_retries: 1`。
// 成功条件: 3回目以降の巡回で着手を試みなくなり、そのことが人間へ1度だけ知らされること。
func TestDispatch_同じ理由で失敗し続けるissueは上限を超えたら拾わない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Agent.MaxRetries = 1
	}})
	fx.AllowLog("Status を落とせません", "着手に失敗しました", "これ以上は拾いません")
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.SetUpdateError(errors.New("テストが起こしたボードへの書き込みの失敗"))

	tick := func() {
		fx.Orc.Tick(context.Background())
		fx.WaitRunsDrained(t, 15*time.Second)
	}
	tick()
	tick()
	tick()
	before := fx.Tracker.CountCall("UpdateStatus")
	tick()
	after := fx.Tracker.CountCall("UpdateStatus")

	if after != before {
		t.Errorf("上限を超えても着手をやり直している: UpdateStatus の回数が %d から %d へ増えた", before, after)
	}
	if !strings.Contains(fx.Logs.String(), "これ以上は拾いません") {
		t.Errorf("拾わなくなったことを人間へ知らせていない")
	}
}

// {"RUCM-PATH": "P040"}
//
// TestDispatch_絞り込みの食い違いが1件あっても他のissueのdispatchは続く は、
// 巡回全体を止めないことを確かめる。
//
// **1件の食い違いで巡回の dispatch を丸ごと止めると、無関係の issue まで着手されなくなる。**
// 食い違った item だけを候補から外して続けること。
//
// 目的: 頼んだ Status に無い候補が混ざっても、他の issue の着手が進むこと。
// 与える情報: Ready の issue が1件と、候補の一覧にだけ載る Blocked の写しが1件。
// 成功条件: Ready の issue に turn が送られ、Blocked の issue の Status は動かないこと。
func TestDispatch_絞り込みの食い違いが1件あっても他のissueのdispatchは続く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.AllowLog("頼んだ Status に無い候補が返ったので飛ばします")
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.AddIssue(sampleIssue(189, "Blocked"))
	// **候補の先頭に食い違いを置く。**先頭で巡回が止まると、後続が着手されない。
	fx.Tracker.SetExtraCandidates(sampleIssue(189, "Blocked"))

	fx.Orc.Tick(context.Background())

	waitFor(t, 15*time.Second, "食い違っていない issue に turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	if got := fx.Tracker.StateOf("PVTI_item189"); got != "Blocked" {
		t.Errorf("頼んだ Status に無い候補を着手している: got %q, want Blocked", got)
	}
	ids := fx.Orc.RunningIdentifiers()
	if len(ids) != 1 || !strings.Contains(ids[0], "#188") {
		t.Errorf("着手した issue が想定と違う: %v", ids)
	}
}
