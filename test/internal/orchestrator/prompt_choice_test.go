package orchestrator_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// recordPrompts は `agent.prompt` の本文を記録する台本を入れる（turn は終わらせない）。
//
// **turn を終わらせないので、待ち受けが返ったあと settle_ms で stall と判定される。**
// このファイルの検査は「何を送ったか」だけを見るので、そのあとの扱いは問わない。
//
// fx: 対象の fixture。
// 戻り値: 送られた本文を送られた順に返す関数。
func recordPrompts(fx *fixture) func() []string {
	var mu sync.Mutex
	var texts []string
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		text, _ := params["text"].(string)
		mu.Lock()
		texts = append(texts, text)
		mu.Unlock()
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(texts))
		copy(out, texts)
		return out
	}
}

// TestAdopt_復元で引き継いだrunには継続の指示を送る は、設計 3-4 の段5c を確かめる。
//
// 目的: 「**送るのは継続の指示（5-4）である。1回目の本文（5-3）ではない。**セッションは
// 引き継いでいるので、エージェントは issue の URL も作法も既に知っている。**turn 数を 1 から
// 数え直すのは打ち切りの計算のためであって、1回目をやり直すことではない**」を示す。
//
// 与える情報: `NeedsPrompt` を立てて引き継いだ run（turn 数は 0 から数え直す）。
// 成功条件: 巡回が送る本文が継続の指示（5-4）であり、**1回目の本文（5-3）の中身が
// 1文字も入っていない**。
func TestAdopt_復元で引き継いだrunには継続の指示を送る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prompts := recordPrompts(fx)

	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	if !fx.Orc.Adopt(issue, orchestrator.AdoptedRun{
		AgentName:        normalize.SafeName("continuo-hello-world-188"),
		PaneID:           "w1:p1",
		SessionUUID:      "session-restored",
		HerdrWorkspaceID: "w1",
	}, true) {
		t.Fatalf("引き継ぎの run を印の集合へ入れられなかった")
	}

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "引き継いだ run に turn が送られる", func() bool {
		return len(prompts()) > 0
	})

	got := prompts()[0]
	if !strings.Contains(got, "続けてください") {
		t.Fatalf("継続の指示（5-4）を送っていない: %q", got)
	}
	if !strings.Contains(got, "1 回目") {
		t.Fatalf("turn 数を 1 から数え直していない: %q", got)
	}
	for _, ng := range []string{"gh issue view", "を実装してください"} {
		if strings.Contains(got, ng) {
			t.Fatalf("1回目の本文（5-3）を送り直している（%q が入っている）: %q", ng, got)
		}
	}
}

// TestResumeBackoff_再着手はセッションに復帰して1回目の本文を送る は、
// バックオフ明けの再開を確かめる。
//
// 目的: 設計 3-3b の「**既存の worktree を再利用していて、身元ファイルにセッション UUID が
// あるなら、それを `--resume` に渡す。新しい UUID を採番しない**」と、
// 「**それでも送るのは1回目の本文（5-3）である**」を示す。
// 継続の指示（5-4）には「issue を読むこと」「紐づく PR も読むこと」が無いので、それだけを
// 送ると、**差し戻しで新しく付いたレビューを読まないまま進む。**
//
// 与える情報: 1回目の turn が stall で打ち切られ、バックオフが明けた run。
// 時計は `testClock` で手で進める（実時間では待たない）。
// 成功条件:
//   - 2回目の `agent.start` に `--session-id` が無く、`--resume` に1回目の UUID が渡る
//   - 身元ファイルの `session_uuid` が1回目のまま変わっていない
//   - 2回目に送る本文が1回目の本文（5-3）の変数展開の結果であり、
//     **「2 回目の試行です」が入っている**
func TestResumeBackoff_再着手はセッションに復帰して1回目の本文を送る(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{
		Now: clock.Now,
		Mutate: func(cfg *config.Config) {
			cfg.Agent.MaxRetryBackoffMs = 10000
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	prompts := recordPrompts(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	// 1回目: 待ち受けは返るが `Stop` が来ないので stall として打ち切られ、リトライが1つ積まれる。
	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "1回目の turn のあとリトライが積まれる", func() bool {
		v, ok := viewOfFixture(fx, "octocat/hello-world#188")
		return ok && v.RetryCount == 1
	})

	// **1回目のセッションの記録を置く。**着手の段5b は、記録が無い UUID へ
	// `--resume` を投げない（設計 3-3b）。**実機では1回目の turn を送った時点で
	// Claude Code が書いているので、ここで置くのが実機に近い。**
	first := startSessionIDs(fx)
	if len(first) == 0 || first[0] == "" {
		t.Fatalf("1回目の起動に --session-id が無い: %v", first)
	}
	seedSessionTranscript(t, fx, first[0], []any{
		typedUserLine("p1", "1回目の本文"),
		assistantLine("req1", "作業中です", false),
	})

	// バックオフが明けるまで時計を進める。
	clock.Advance(30 * time.Second)
	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "再着手で2回目の turn が送られる", func() bool {
		return len(prompts()) >= 2
	})

	// **1回目のセッションへ復帰している**（新しい UUID を採番していない）。
	starts := startSessionIDs(fx)
	resumes := startResumeUUIDs(fx)
	if len(starts) < 2 {
		t.Fatalf("再着手で agent を起動し直していない: %v", starts)
	}
	if starts[0] == "" {
		t.Fatalf("1回目の起動に --session-id が無い: %v", starts)
	}
	if starts[1] != "" {
		t.Fatalf("再着手で新しいセッションを採番している（--session-id が渡っている）: %v", starts)
	}
	if resumes[1] != starts[0] {
		t.Fatalf("再着手が1回目のセッションへ復帰していない: --resume=%q, 1回目=%q", resumes[1], starts[0])
	}

	// **身元ファイルの session_uuid は書き換わっていない。**
	if got := identitySessionUUID(t, fx, 188); got != starts[0] {
		t.Fatalf("身元ファイルの session_uuid が変わっている: got %q, want %q", got, starts[0])
	}

	// **セッションへ復帰しても1回目の本文（5-3）を送る。**
	got := prompts()[1]
	if !strings.Contains(got, "gh issue view") || !strings.Contains(got, "octocat/hello-world#188") {
		t.Fatalf("再着手で1回目の本文（5-3）を送っていない: %q", got)
	}
	if !strings.Contains(got, "2 回目の試行") {
		t.Fatalf("試行回数（.attempt）が本文に入っていない: %q", got)
	}
	if strings.Contains(got, "続けてください") {
		t.Fatalf("再着手なのに継続の指示（5-4）だけを送っている（新しいレビューを読ませられない）: %q", got)
	}
}

// viewOfFixture は識別子で RunView を引く（テスト用socket mockを使う fixture 用）。
//
// fx: 対象の fixture。
// identifier: 探す issue の識別子。
// 戻り値の1つ目: 見つかった写し。
// 戻り値の2つ目: 印を持っていれば true。
func viewOfFixture(fx *fixture, identifier string) (orchestrator.RunView, bool) {
	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == identifier {
			return v, true
		}
	}
	return orchestrator.RunView{}, false
}

// startSessionIDs は `agent.start` に渡された `--session-id` の値を、呼ばれた順に返す。
//
// **`--session-id` が無い起動（`--resume` での復帰）は空文字で並びに入る。**
//
// fx: 対象の fixture。
// 戻り値: セッション UUID の並び。
func startSessionIDs(fx *fixture) []string {
	return startFlagValues(fx, "--session-id")
}

// startResumeUUIDs は `agent.start` に渡された `--resume` の値を、呼ばれた順に返す。
//
// **`--resume` が無い起動（新しいセッション）は空文字で並びに入る。**
//
// fx: 対象の fixture。
// 戻り値: 復帰先のセッション UUID の並び。
func startResumeUUIDs(fx *fixture) []string {
	return startFlagValues(fx, "--resume")
}

// startFlagValues は `agent.start` の args から、指定した起動フラグの次の語を拾う。
//
// fx: 対象の fixture。
// flag: 拾う起動フラグ（例: `--session-id`）。
// 戻り値: `agent.start` が呼ばれた順の値。**フラグが無い呼び出しは空文字で入る。**
func startFlagValues(fx *fixture, flag string) []string {
	out := []string{}
	for _, r := range fx.Herdr.Requests() {
		if r.Method != herdr.MethodAgentStart {
			continue
		}
		args, _ := r.Params["args"].([]any)
		id := ""
		for i, item := range args {
			s, _ := item.(string)
			if s == flag && i+1 < len(args) {
				id, _ = args[i+1].(string)
				break
			}
		}
		out = append(out, id)
	}
	return out
}

// worktreePathOf は、その issue の worktree の絶対パスを `workspace.Locate` に決めさせる。
//
// **テストがパスを組み立ててはならない。**置き場所の規則が変わったとき、テストだけが
// 古い場所を見に行き、**本物の合格を「worktree が残っていない」と報告する。**
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
// issue: 対象の issue。
// 戻り値: worktree の絶対パス。
func worktreePathOf(t *testing.T, fx *fixture, issue tracker.Issue) string {
	t.Helper()
	loc, _, err := workspace.Locate(
		fx.Workspace.ResolvedRoot(),
		fx.Config.Herdr.Worktree.BranchTemplate,
		workspace.IssueRef{
			URL:           *issue.URL,
			Identifier:    issue.Identifier,
			ProjectItemID: issue.ID,
			Owner:         issue.Owner,
			Repo:          issue.Repo,
			Number:        issue.Number,
			NativeRef:     issue.NativeRef,
		})
	if err != nil {
		t.Fatalf("worktree の置き場所を決められません: %v", err)
	}
	return loc.Path
}

// identitySessionUUID は、その issue の worktree の身元ファイルから `session_uuid` を読む。
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
// number: issue の番号（`sampleIssue` に渡したもの）。
// 戻り値: 身元ファイルの `session_uuid`。
func identitySessionUUID(t *testing.T, fx *fixture, number int) string {
	t.Helper()
	identity, err := fx.Workspace.ReadIdentity(worktreePathOf(t, fx, sampleIssue(number, "In Progress")))
	if err != nil {
		t.Fatalf("身元ファイルを読めません: %v", err)
	}
	return identity.SessionUUID
}
