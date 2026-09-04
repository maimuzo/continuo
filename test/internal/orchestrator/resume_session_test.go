// {"RUCM-CFG-SHA256": "4a61db11c52f5ba42b23b7180d4dfe2d79b39f257e065f54fe735fd3e48d11e6", "SOURCE": "docs/spec/usecases/particular_case/issue を1件処理する.cfg.json"}
//
// **再着手でセッションに復帰することの検査である。**
//
// **worktree を新しく作る着手は、新しいセッション UUID を採番して `--session-id` で始める。**
// **既存の worktree を再利用する再着手は、身元ファイルのセッション UUID へ `--resume` で戻る。**
// 戻れなかったときは新しいセッションで始め直す（設計 3-3b）。
//
// **どの検査も、印が指す経路の事後条件まで見る。**起動フラグだけを見て turn が送られた
// ところで止めると、**その経路が最後まで通っていなくても通ってしまう。**
package orchestrator_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/tracker"
)

// nodeIDOf は issue のノード ID を取り出す。
//
// **テストが `I_node188` のような値を直書きしてはならない。**引数の issue と別の issue を
// 見に行っても、テストは何も言わずに通る。
//
// t: 呼び出し元のテスト。
// issue: 対象の issue。
// 戻り値: issue のノード ID。
func nodeIDOf(t *testing.T, issue tracker.Issue) string {
	t.Helper()
	nodeID, _ := issue.NativeRef["issue_node_id"].(string)
	if nodeID == "" {
		t.Fatalf("issue にノード ID が無い: %s", issue.Identifier)
	}
	return nodeID
}

// paneIDOf は、その issue のために continuo が使った pane の ID を herdr のリクエストから引く。
//
// **着手は pane の label に `owner/repo/issues/N` を書く**（設計 3-3）ので、
// その `pane.rename` を issue ごとに1件だけ引ける。
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
// issue: 対象の issue。
// 戻り値: pane の ID。
func paneIDOf(t *testing.T, fx *fixture, issue tracker.Issue) string {
	t.Helper()
	label := herdr.IssueLabel(issue.Owner, issue.Repo, issue.Number)
	for _, r := range fx.Herdr.Requests() {
		if r.Method != herdr.MethodPaneRename {
			continue
		}
		if got, _ := r.Params["label"].(string); got != label {
			continue
		}
		id, _ := r.Params["pane_id"].(string)
		return id
	}
	t.Fatalf("label が %q の pane.rename が1件も無い（受け取ったのは %v）", label, fx.Herdr.Methods())
	return ""
}

// closedPane は、その pane を閉じたかを返す。
//
// **fixture 全体の `pane.close` の回数で見てはならない。**issue が2件走っていると、
// **別の run が閉じた1件でこちらの検査も通り、pane を置き去りにした run が合格する。**
//
// fx: 対象の fixture。
// paneID: 見る pane の ID。
// 戻り値: その pane に対する `pane.close` があれば true。
func closedPane(fx *fixture, paneID string) bool {
	for _, r := range fx.Herdr.Requests() {
		if r.Method != herdr.MethodPaneClose {
			continue
		}
		if got, _ := r.Params["pane_id"].(string); got == paneID {
			return true
		}
	}
	return false
}

// finishRunOnPrompt は、`agent.prompt` を受けたら「エージェントが作業を終えて
// `CONTINUO-STATUS: review` を書き、issue にコメントを残した」状態を作って Stop hook を流す。
//
// **これを入れないと、基本フローは turn を送ったところで止まる。**
// 事後条件（Status・コメント・pane・印）は、そこから先でしか決まらない。
//
// **台本の中では `t` を使わない。**`t.Fatalf` は呼んだ goroutine で `runtime.Goexit` を
// 起こす。台本は `fakeHerdr.serve` の goroutine で走るので、**応答を書かないまま
// `defer conn.Close()` が走り、continuo 側は EOF を受け取って herdr の呼び出しが失敗する。**
// **落ちる理由が「台本が見つけたかったこと」から「herdr の呼び出しが切れた」へすり替わる。**
// `t.Errorf` も、テスト本体が返ったあとに走ると panic する。
// **だから transcript は先に書き、hook を捨てられたことは変数に控えて後片付けで見る。**
//
// **応答は既定の台本に返させる**（`fakeHerdr.HandlerOf` で包む）。写して書き直すと、
// 既定の応答が変わったときにこの2本だけが古い形のまま通り続ける。
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
// issue: turn を回す issue（コメントの宛先をここから導く）。
// sessionUUID: 起動したセッションの UUID（transcript の名前と hook の名乗りに使う）。
// 戻り値: 送られた本文を送られた順に返す関数。
func finishRunOnPrompt(t *testing.T, fx *fixture, issue tracker.Issue, sessionUUID string) func() []string {
	t.Helper()
	nodeID := nodeIDOf(t, issue)
	// **記録は記録の根の直下1階層へ置く**（設計 3-3b）。着手の段5b がここを探し、
	// **無ければ `--resume` を渡さない。**`t.TempDir()` は2階層下なので当たらない。
	transcriptPath := seedSessionTranscript(t, fx, sessionUUID, []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "実装して commit と push をしました。\n\nCONTINUO-STATUS: review", false),
	})
	base := fx.Herdr.HandlerOf(herdr.MethodAgentPrompt)
	if base == nil {
		t.Fatalf("agent.prompt の既定の台本が入っていない")
	}

	var mu sync.Mutex
	var texts []string
	hookDropped := false
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		text, _ := params["text"].(string)
		mu.Lock()
		texts = append(texts, text)
		mu.Unlock()

		// **何をしたかはエージェントが書く**（continuo は代筆しない。設計 3-25 / 3-29）。
		fx.Tracker.AddComment(nodeID, "<!-- continuo:agent -->\n実装しました", true, time.Now())
		if !fx.Orc.OnHook(stopEvent(sessionUUID, transcriptPath, "p1")) {
			mu.Lock()
			hookDropped = true
			mu.Unlock()
		}
		return base(params)
	})
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		if hookDropped {
			t.Errorf("continuo が %s の hook を知らない run のものとして捨てた", sessionUUID)
		}
	})
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(texts))
		copy(out, texts)
		return out
	}
}

// assertBasicFlowPostcondition は、基本フローの事後条件をそのまま検査する。
//
//	issue の Status は表明の値の遷移先の選択肢である
//	issue にエージェントが書いたコメントが1件以上ある
//	herdr の pane は閉じている
//	印は外れている
//	worktree と branch は残っている
//
// **見に行く先は、引数の issue から導く。**別の issue の番号を直書きすると、
// **どの issue を渡しても同じ1件だけを見る検査**になり、事後条件を確かめたことにならない。
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
// issue: 対象の issue。
func assertBasicFlowPostcondition(t *testing.T, fx *fixture, issue tracker.Issue) {
	t.Helper()

	if got := fx.Tracker.StateOf(issue.ID); got != "In Review" {
		t.Errorf("Status が表明の値の遷移先になっていない: got %q, want %q", got, "In Review")
	}
	agentComments := 0
	for _, c := range fx.Tracker.CommentsOf(nodeIDOf(t, issue)) {
		if c.IsAgent {
			agentComments++
		}
	}
	if agentComments == 0 {
		t.Errorf("issue にエージェントが書いたコメントが1件も無い")
	}
	if strings.Contains(fx.Logs.String(), "セッションを復元して書かせます") {
		t.Errorf("コメントがあるのに取り戻しへ入っている（基本フローの経路から外れている）")
	}
	paneID := paneIDOf(t, fx, issue)
	if !closedPane(fx, paneID) {
		t.Errorf("この issue の pane を閉じていない: pane_id=%s（受け取ったのは %v）", paneID, fx.Herdr.Methods())
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Errorf("印が外れていない: %d 件", got)
	}
	worktreePath := worktreePathOf(t, fx, issue)
	if _, err := os.Stat(worktreePath); err != nil {
		t.Errorf("worktree が残っていない: %s (err=%v)", worktreePath, err)
	}
	branch := fmt.Sprintf("continuo/%s/%s/%d", issue.Owner, issue.Repo, issue.Number)
	if runGit(t, fx.Repo.Dir, "branch", "--list", branch) == "" {
		t.Errorf("branch が残っていない: %s", branch)
	}
}

// TestDispatch_新規の着手は新しいセッションを立てる は、設計 3-3b の「新規」側を確かめる。
//
// 目的: 「**新規の着手（worktree を新しく作る）は、いままでどおり新しい UUID を採番して
// `--session-id` を渡す**」を示す。**復帰する相手が無いのに `--resume` を渡すと、
// `No conversation found with session ID` で1文字も起動しない。**
//
// **この検査には経路の印を付けない**（設計 6-18e）。通る経路は
// `TestDispatch_既存のworktreeがあれば前回のセッションに復帰する` と同じ P001 である。
// 「起動フラグを決める」の段は条件ステップではないので、CFG に枝が無く、
// 新規と再着手を別の経路として指せない。**同じ印を2本に付けると、片方を消しても
// 集計は満たされたままになる**ので、印は再着手の側1本に絞ってある。
//
// 与える情報: worktree がまだ無い `Ready` の issue 1件。エージェントは1回目の turn で
// `CONTINUO-STATUS: review` を書いてコメントを残し、turn を終える。
// 成功条件:
//   - `agent.start` の起動フラグに `--session-id` が入り、`--resume` が1つも入らない
//   - 身元ファイルに、起動したセッション UUID が書かれている
//   - 基本フローの事後条件（Status・コメント・pane・印・worktree と branch）が揃う
func TestDispatch_新規の着手は新しいセッションを立てる(t *testing.T) {
	// **記録の根は、このテスト専用にする**（`sessionTranscriptDir` の説明）。
	fx := newFixture(t, fixtureOptions{TranscriptRoot: t.TempDir()})
	issue := sampleIssue(188, "Ready")
	prompts := finishRunOnPrompt(t, fx, issue, "session-1")
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "1回目の turn が送られる", func() bool {
		return len(prompts()) > 0
	})

	starts := startSessionIDs(fx)
	resumes := startResumeUUIDs(fx)
	if len(starts) == 0 {
		t.Fatalf("agent.start が1度も呼ばれていない")
	}
	if starts[0] == "" {
		t.Fatalf("新規の着手に --session-id が渡っていない: %v", starts)
	}
	if resumes[0] != "" {
		t.Fatalf("新規の着手に --resume が渡っている（復帰する相手が無い）: %v", resumes)
	}
	if got := identitySessionUUID(t, fx, 188); got != starts[0] {
		t.Fatalf("身元ファイルに、起動したセッション UUID が書かれていない: got %q, want %q", got, starts[0])
	}

	waitFor(t, 10*time.Second, "run が印から外れる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
	assertBasicFlowPostcondition(t, fx, issue)
}

// {"RUCM-PATH": "P001"}
//
// TestDispatch_既存のworktreeがあれば前回のセッションに復帰する は、設計 3-3b の「再着手」側を
// 確かめる。
//
// 目的: 「**既存の worktree を再利用していて、その身元ファイルに `SessionUUID` が入って
// いるなら、それを `--resume` に渡す。新しい UUID を採番しない。身元ファイルの
// `session_uuid` も変えない**」を示す。
//
// **送る本文は1回目の本文（5-3）のままである。**`In Review` から `In Progress` へ
// 戻される場面では人間が PR にレビューを書いており、**「issue を読むこと」「紐づく PR も
// 読むこと」が入っているのは1回目の本文だけだからである。**
//
// 与える情報: セッション UUID `sess-188` を書いた身元ファイルつきの worktree と、
// `In Progress` の issue 1件。エージェントは1回目の turn で `CONTINUO-STATUS: review` を
// 書いてコメントを残し、turn を終える。
// 成功条件:
//   - `agent.start` の起動フラグが `--resume sess-188` であり、`--session-id` が無い
//   - 身元ファイルの `session_uuid` が `sess-188` のまま変わっていない
//   - 送られた本文が1回目の本文（5-3）である
//   - 基本フローの事後条件（Status・コメント・pane・印・worktree と branch）が揃う
func TestDispatch_既存のworktreeがあれば前回のセッションに復帰する(t *testing.T) {
	// **記録の根は、このテスト専用にする**（`sessionTranscriptDir` の説明）。
	fx := newFixture(t, fixtureOptions{TranscriptRoot: t.TempDir()})

	issue := sampleIssue(188, "In Progress")
	prepareWorktree(t, fx, issue, identityOverride{SessionUUID: "sess-188"})
	prompts := finishRunOnPrompt(t, fx, issue, "sess-188")
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "再着手で turn が送られる", func() bool {
		return len(prompts()) > 0
	})

	starts := startSessionIDs(fx)
	resumes := startResumeUUIDs(fx)
	if len(starts) == 0 {
		t.Fatalf("agent.start が1度も呼ばれていない")
	}
	if resumes[0] != "sess-188" {
		t.Fatalf("前回のセッションへ復帰していない: --resume=%q, want %q", resumes[0], "sess-188")
	}
	if starts[0] != "" {
		t.Fatalf("復帰するのに新しい UUID を採番している: --session-id=%q", starts[0])
	}
	if got := identitySessionUUID(t, fx, 188); got != "sess-188" {
		t.Fatalf("身元ファイルの session_uuid が書き換わっている: got %q, want %q", got, "sess-188")
	}

	// **復帰しても1回目の本文（5-3）を送る。**
	got := prompts()[0]
	if !strings.Contains(got, "gh issue view") || !strings.Contains(got, "octocat/hello-world#188") {
		t.Fatalf("復帰した run に1回目の本文（5-3）を送っていない: %q", got)
	}
	if strings.Contains(got, "続けてください") {
		t.Fatalf("復帰した run に継続の指示（5-4）だけを送っている（新しいレビューを読ませられない）: %q", got)
	}

	waitFor(t, 10*time.Second, "run が印から外れる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
	assertBasicFlowPostcondition(t, fx, issue)
}

// {"RUCM-PATH": "P028"}
//
// TestDispatch_復帰に失敗したら新しいセッションで始め直す は、代替フロー `復帰の失敗` を確かめる。
//
// 目的: 「**`--resume` に渡した UUID のセッションが、もう存在しないことがある**
// （`~/.claude/projects/` の中身は利用者が消せる）。そのとき新しいセッションで
// 起動し直す」を示す。**ここで諦めると、利用者が履歴を消しただけで issue が
// `failure_state` へ落ちる。**
//
// **実測（2026-08-26）。**`claude --resume <無い UUID>` は終了コード 1 で、標準エラーに
// `No conversation found with session ID: <UUID>` を出して終わる。herdr 経由だと
// `agent.start` が `timeout: timed out waiting for agent startup` を返し、pane は
// シェルのプロンプトへ戻る（**同じ pane で、そのまま起動し直せる**）。台本はこれを再現する。
//
// 与える情報: セッション UUID `sess-188` を書いた身元ファイルつきの worktree と、
// `--resume` つきの `agent.start` だけが `timeout` を返す herdr。
// 成功条件（代替フロー `復帰の失敗` の事後条件をそのまま見る）:
//   - 1回目の起動が `--resume sess-188` で、`--session-id` が1つも入っていない
//   - 立て直しの起動は別の `agent.start` で、`--session-id` が新しい UUID であり、
//     **`--resume` が1つも残っていない**（残ると捨てたはずの会話へまた戻る）
//   - 身元ファイルの `session_uuid` が新しい UUID へ書き直されている
//     （書き直さないと、次の再着手も同じ死んだ UUID へ復帰しにいく）
//   - hook の引き当ての索引が張り替わっていて、前回のセッション UUID を名乗る hook は
//     どの run のものでもないとして捨てられる
//   - herdr の pane は開いたままである（閉じると立て直しの起動先が無くなる）
//   - issue の Status は running_state のままである（`failure_state` へ落とさない）
//   - 印は残っている
//   - worktree は残っている
func TestDispatch_復帰に失敗したら新しいセッションで始め直す(t *testing.T) {
	// **記録の根は、このテスト専用にする**（`sessionTranscriptDir` の説明）。
	fx := newFixture(t, fixtureOptions{TranscriptRoot: t.TempDir()})
	fx.AllowLog("前回のセッションへ復帰できなかったので")
	prompts := recordPrompts(fx)

	issue := sampleIssue(188, "In Progress")
	wt := prepareWorktree(t, fx, issue, identityOverride{SessionUUID: "sess-188"})
	// **記録を置いてから走らせる。**置かないと着手の段5b が「会話の記録が無い」と判定し、
	// **`--resume` を1回も渡さないので、この代替フローの分岐元そのものが起きない**（設計 3-3b）。
	// **ここで再現したいのは「記録はあるのに復帰できない」場合である**
	// （pane がまだシェルを起動しきっていない、など）。
	seeded := seedSessionTranscript(t, fx, "sess-188", []any{
		typedUserLine("p0", "前回の1行"),
		assistantLine("req0", "作業中です", false),
	})
	fx.Tracker.AddIssue(issue)

	var mu sync.Mutex
	resumeAttempts := 0
	base := fx.Herdr.HandlerOf(herdr.MethodAgentStart)
	if base == nil {
		t.Fatalf("agent.start の既定の台本が入っていない")
	}
	fx.Herdr.Handle(herdr.MethodAgentStart, func(params map[string]any) (any, *rpcErr) {
		args, _ := params["args"].([]any)
		resuming := false
		for _, item := range args {
			if s, _ := item.(string); s == "--resume" {
				resuming = true
				break
			}
		}
		if resuming {
			mu.Lock()
			resumeAttempts++
			mu.Unlock()
			return nil, &rpcErr{Code: herdr.ErrCodeTimeout, Message: "timed out waiting for agent startup"}
		}
		return base(params)
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "始め直したセッションで turn が送られる", func() bool {
		return len(prompts()) > 0
	})

	mu.Lock()
	attempts := resumeAttempts
	mu.Unlock()
	if attempts == 0 {
		t.Fatalf("そもそも --resume を試していない")
	}

	starts := startSessionIDs(fx)
	resumes := startResumeUUIDs(fx)
	if len(starts) < 2 {
		t.Fatalf("agent.start が2回に届いていない（復帰と立て直しで2回のはず）: %v", starts)
	}
	// **1回目の起動が、代替フロー `復帰の失敗` の分岐元である。**
	// 死んだ UUID へ復帰しにいったことを、ここで見る。
	if resumes[0] != "sess-188" {
		t.Fatalf("1回目の起動が身元ファイルの UUID へ復帰していない: --resume=%q, want %q", resumes[0], "sess-188")
	}
	if starts[0] != "" {
		t.Fatalf("復帰の起動に --session-id が混ざっている: %q", starts[0])
	}

	// **立て直しの起動は、1回目とは別の `agent.start` である**
	// （事後条件「立て直しの起動はまだ1回も呼んでいない」の裏返しである）。
	// **`--resume` と `--session-id` は排他なので**（`internal/orchestrator/settings.go` の
	// `claudeStartArgs`）、`--session-id` が入っていること自体が
	// 「会話履歴を1文字も読まない起動である」ことを意味する。
	fresh := ""
	freshAt := -1
	for i, id := range starts {
		if id != "" {
			fresh = id
			freshAt = i
			break
		}
	}
	if fresh == "" {
		t.Fatalf("復帰に失敗したあと --session-id つきで起動し直していない: %v", starts)
	}
	// **立て直しの起動には `--resume` が1つも残っていない。**
	// **残ったままだと、`復帰の失敗` が捨てたはずの会話へまた戻りにいく。**
	if resumes[freshAt] != "" {
		t.Fatalf("立て直しの起動に --resume が残っている: --resume=%q (starts=%v, resumes=%v)",
			resumes[freshAt], starts, resumes)
	}
	if fresh == "sess-188" {
		t.Fatalf("死んだセッションの UUID を --session-id に使い回している: %q", fresh)
	}
	if got := identitySessionUUID(t, fx, 188); got != fresh {
		t.Fatalf("身元ファイルの session_uuid を書き直していない（次の再着手もまた復帰を試みる）: got %q, want %q",
			got, fresh)
	}

	// 代替フロー `復帰の失敗` の残りの事後条件。
	//
	// **hook の引き当ての索引の張り替えを見る。**張り替えていないと、pane に残った前の
	// Claude Code が前回のセッション UUID を名乗って Stop hook を送り、
	// **立て直した run の turn が別の会話の transcript で終わる。**
	//
	// **同じ名前の記録を2つ作らない。**根の下に `sess-188.jsonl` が2つあると、
	// 着手の段5b がどちらを見つけるかは `os.ReadDir` の並び順（ディレクトリ名）で決まり、
	// **`os.MkdirTemp` の付ける接尾辞は実行のたびに変わる。**上で置いた1つを書き直す。
	stale := writeTranscript(t, filepath.Dir(seeded), "sess-188.jsonl", []any{
		typedUserLine("p-stale", "前のセッションの1行"),
		assistantLine("req-stale", "CONTINUO-STATUS: review", false),
	})
	if fx.Orc.OnHook(stopEvent("sess-188", stale, "p-stale")) {
		t.Errorf("前回のセッション UUID を名乗る hook を、まだこの run のものとして受けている")
	}
	// **pane は開いたままである。**閉じてしまうと、立て直しの起動先そのものが無くなる。
	if paneID := paneIDOf(t, fx, issue); closedPane(fx, paneID) {
		t.Errorf("立て直しの前に pane を閉じている: pane_id=%s", paneID)
	}
	if got := fx.Tracker.StateOf(issue.ID); got != "In Progress" {
		t.Errorf("Status が running_state のままになっていない: got %q, want %q", got, "In Progress")
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Errorf("印が残っていない: %d 件（1 件のはず）", got)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Errorf("worktree が残っていない: %s (err=%v)", wt.Path, err)
	}
}

// TestDispatch_会話の記録が無いセッションには復帰しない は、設計 3-3b の
// 「記録が無ければ新しいセッションで始める」を確かめる。
//
// 目的: 「**身元ファイルにセッション UUID が入っていても、その会話の記録が
// 記録の置き場所に1件も無ければ `--resume` を渡さない**」を示す。
//
// **なぜ要るか。**着手が段9 の途中で落ちると、身元ファイルには**会話が1度も作られていない
// UUID** が残る（`restartWithNewSession` が採り直した UUID を書いたあとで、立て直しの
// `agent.start` も失敗した場合である）。**そのまま復帰しにいくと、
// `confirmStartupWithRestart` が `herdr.startup_timeout_ms` を使い切るまで
// `agent.start` をやり直し続ける**（利用者の実測で18回・約60秒）。
//
// **`TestDispatch_復帰に失敗したら新しいセッションで始め直す` との違い。**あちらは
// **記録はあるのに起動できなかった**場合で、`--resume` を1回投げてから立て直す。
// こちらは**投げる前に決める**ので、`agent.start` は最初から `--session-id` で1回だけである。
//
// **この検査には経路の印を付けない**（設計 6-18e）。通る経路は
// `TestDispatch_既存のworktreeがあれば前回のセッションに復帰する` と同じ P001 である。
// 起動フラグを決める段は条件ステップではないので CFG に枝が無く、
// **同じ印を2本に付けると、片方を消しても集計は満たされたままになる。**
//
// 与える情報: セッション UUID `sess-188` を書いた身元ファイルつきの worktree と、
// `In Progress` の issue 1件。**その UUID の会話の記録は1件も置かない。**
// 成功条件:
//   - `agent.start` の起動フラグに `--resume` が1つも入らない
//   - `--session-id` に新しい UUID が渡る（`sess-188` を使い回さない）
//   - **`agent.start` が1回で済んでいる**（空回りしていない）
//   - 身元ファイルの `session_uuid` が、その新しい UUID へ書き直されている
func TestDispatch_会話の記録が無いセッションには復帰しない(t *testing.T) {
	// **記録の根は、このテスト専用にする。**既定のままだと、前の実行が残した
	// `sess-188.jsonl` を拾って「記録がある」と判定し、**この検査が黙って通る。**
	fx := newFixture(t, fixtureOptions{TranscriptRoot: t.TempDir()})
	prompts := recordPrompts(fx)

	issue := sampleIssue(188, "In Progress")
	prepareWorktree(t, fx, issue, identityOverride{SessionUUID: "sess-188"})
	// **記録は置かない。**着手が途中で落ちたあとの身元ファイルを再現している。
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "新しいセッションで turn が送られる", func() bool {
		return len(prompts()) > 0
	})

	starts := startSessionIDs(fx)
	resumes := startResumeUUIDs(fx)
	if len(starts) == 0 {
		t.Fatalf("agent.start が1度も呼ばれていない")
	}
	for i, uuid := range resumes {
		if uuid != "" {
			t.Fatalf("会話の記録が無いのに %d 回目の起動へ --resume を渡している: %q", i+1, uuid)
		}
	}
	if starts[0] == "" {
		t.Fatalf("新しい UUID を --session-id で渡していない: %v", starts)
	}
	if starts[0] == "sess-188" {
		t.Fatalf("記録の無いセッションの UUID を --session-id に使い回している: %q", starts[0])
	}
	// **空回りしていないことを、`agent.start` の回数で見る。**死んだ UUID へ復帰しにいくと、
	// `herdr.startup_timeout_ms` の中でやり直しが積み上がる。
	if len(starts) != 1 {
		t.Fatalf("agent.start が1回で済んでいない（空回りしている）: %d 回, %v", len(starts), starts)
	}
	if got := identitySessionUUID(t, fx, 188); got != starts[0] {
		t.Fatalf("身元ファイルの session_uuid を書き直していない（次の着手もまた復帰を試みる）: got %q, want %q",
			got, starts[0])
	}
}

// TestDispatch_記録の判定は残り3つの場合も設計どおりに倒れる は、設計 3-3b の判定の表の
// 残り3行を確かめる。
//
// 目的: **設計 3-3b の表は4つの結果を並べているのに、検査があるのは「記録が1件も無い」だけ
// だった。**残り3つは、守りの向きを逆に書き換えても全部緑のまま通る状態にあった。
//
//	大きさが0の記録    … 復帰しない（会話が1バイトも書かれていないセッションへ戻っても、引き継げる内容が無い）
//	UUID がパスに使えない … 復帰しない（身元ファイルはエージェントが書き換えられる。設計 3-2 / 3-23）
//	記録の置き場所を読めない … **復帰を試す**（判定できないことで着手が止まってはならない）
//
// **3つ目だけ倒れる向きが逆である。**そこが逆になると、記録の置き場所を作っていない機械で
// **再着手が会話履歴を毎回捨てる。**
//
// **この検査にも経路の印を付けない**（設計 6-18e）。通る経路は
// `TestDispatch_既存のworktreeがあれば前回のセッションに復帰する` と同じ P001 である。
//
// 与える情報: 身元ファイルつきの worktree と `In Progress` の issue 1件。
// 記録の置き場所と身元ファイルの UUID を、場合ごとに変える。
// 成功条件: 復帰しない2つでは `--resume` が1つも渡らず、読めない場合では渡ること。
func TestDispatch_記録の判定は残り3つの場合も設計どおりに倒れる(t *testing.T) {
	cases := []struct {
		name string
		// uuid は身元ファイルへ書くセッション UUID である。
		uuid string
		// missingRoot を真にすると、記録の置き場所として実在しないパスを渡す。
		missingRoot bool
		// emptyTranscript を真にすると、大きさが0の記録を置く。
		emptyTranscript bool
		// wantResume は `--resume` が渡ることを期待するかである。
		wantResume bool
	}{
		{
			name:            "大きさが0の記録には復帰しない",
			uuid:            "sess-188",
			emptyTranscript: true,
			wantResume:      false,
		},
		{
			// **`/` はパスの区切りである。**通すと、根の外のファイルを見に行く組み立て方が成立する。
			name:       "UUIDがパスに使えない形なら復帰しない",
			uuid:       "sess/../188",
			wantResume: false,
		},
		{
			// **判定できないので、いままでどおり復帰を試す。**この検査は空回りを減らす
			// ためのものであり、これが働かないことで着手が止まってはならない。
			name:        "記録の置き場所を読めないときは復帰を試す",
			uuid:        "sess-188",
			missingRoot: true,
			wantResume:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.missingRoot {
				// **作らないディレクトリを指す。**`os.ReadDir` が失敗する状態である。
				root = filepath.Join(root, "まだ作られていない置き場所")
			}
			fx := newFixture(t, fixtureOptions{TranscriptRoot: root})
			prompts := recordPrompts(fx)

			issue := sampleIssue(188, "In Progress")
			prepareWorktree(t, fx, issue, identityOverride{SessionUUID: tc.uuid})
			if tc.emptyTranscript {
				// **`seedSessionTranscript` は使えない。**あれは中身のある記録を書く。
				writeTranscript(t, sessionTranscriptDir(t, fx), tc.uuid+".jsonl", nil)
			}
			fx.Tracker.AddIssue(issue)

			fx.Orc.Tick(context.Background())
			waitFor(t, 10*time.Second, "turn が送られる", func() bool {
				return len(prompts()) > 0
			})

			resumes := startResumeUUIDs(fx)
			if len(resumes) == 0 {
				t.Fatalf("agent.start が1度も呼ばれていない")
			}
			if tc.wantResume {
				if resumes[0] != tc.uuid {
					t.Fatalf("判定できないのに復帰していない: --resume=%q, want %q", resumes[0], tc.uuid)
				}
				return
			}
			for i, uuid := range resumes {
				if uuid != "" {
					t.Fatalf("復帰してはならないのに %d 回目の起動へ --resume を渡している: %q", i+1, uuid)
				}
			}
		})
	}
}
