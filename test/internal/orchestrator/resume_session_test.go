// {"RUCM-CFG-SHA256": "89495ef235abec9e025d81f6bd3f442137888f5d75054530717327c3e47de8bc", "SOURCE": "docs/spec/usecases/particular_case/issue を1件処理する.cfg.json"}
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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/tracker"
)

// finishRunOnPrompt は、`agent.prompt` を受けたら「エージェントが作業を終えて
// `CONTINUO-STATUS: review` を書き、issue にコメントを残した」状態を作って Stop hook を流す。
//
// **これを入れないと、基本フローは turn を送ったところで止まる。**
// 事後条件（Status・コメント・pane・印）は、そこから先でしか決まらない。
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
// sessionUUID: 起動したセッションの UUID（transcript の名前と hook の名乗りに使う）。
// nodeID: コメントを書く issue のノード ID。
// 戻り値: 送られた本文を送られた順に返す関数。
func finishRunOnPrompt(t *testing.T, fx *fixture, sessionUUID, nodeID string) func() []string {
	t.Helper()
	transcriptDir := t.TempDir()
	var mu sync.Mutex
	var texts []string
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		text, _ := params["text"].(string)
		mu.Lock()
		texts = append(texts, text)
		mu.Unlock()

		path := writeTranscript(t, transcriptDir, sessionUUID+".jsonl", []any{
			typedUserLine("p1", "実装してください"),
			assistantLine("req1", "実装して commit と push をしました。\n\nCONTINUO-STATUS: review", false),
		})
		// **何をしたかはエージェントが書く**（continuo は代筆しない。設計 3-25 / 3-29）。
		fx.Tracker.AddComment(nodeID, "<!-- continuo:agent -->\n実装しました", true, time.Now())
		if !fx.Orc.OnHook(stopEvent(sessionUUID, path, "p1")) {
			t.Errorf("continuo が %s の hook を知らない run のものとして捨てた", sessionUUID)
		}
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

// assertBasicFlowPostcondition は、基本フローの事後条件をそのまま検査する。
//
//	issue の Status は表明の値の遷移先の選択肢である
//	issue にエージェントが書いたコメントが1件以上ある
//	herdr の pane は閉じている
//	印は外れている
//	worktree と branch は残っている
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
// issue: 対象の issue。
// worktreePath: その issue の worktree の絶対パス。
func assertBasicFlowPostcondition(t *testing.T, fx *fixture, issue tracker.Issue, worktreePath string) {
	t.Helper()

	if got := fx.Tracker.StateOf(issue.ID); got != "In Review" {
		t.Errorf("Status が表明の値の遷移先になっていない: got %q, want %q", got, "In Review")
	}
	agentComments := 0
	for _, c := range fx.Tracker.CommentsOf("I_node188") {
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
	if fx.Herdr.CountMethod(herdr.MethodPaneClose) == 0 {
		t.Errorf("pane を閉じていない: %v", fx.Herdr.Methods())
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Errorf("印が外れていない: %d 件", got)
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Errorf("worktree が残っていない: %s (err=%v)", worktreePath, err)
	}
	if runGit(t, fx.Repo.Dir, "branch", "--list", "continuo/octocat/hello-world/188") == "" {
		t.Errorf("branch が残っていない: continuo/octocat/hello-world/188")
	}
}

// {"RUCM-PATH": "P001"}
//
// TestDispatch_新規の着手は新しいセッションを立てる は、設計 3-3b の「新規」側を確かめる。
//
// 目的: 「**新規の着手（worktree を新しく作る）は、いままでどおり新しい UUID を採番して
// `--session-id` を渡す**」を示す。**復帰する相手が無いのに `--resume` を渡すと、
// `No conversation found with session ID` で1文字も起動しない。**
//
// 与える情報: worktree がまだ無い `Ready` の issue 1件。エージェントは1回目の turn で
// `CONTINUO-STATUS: review` を書いてコメントを残し、turn を終える。
// 成功条件:
//   - `agent.start` の起動フラグに `--session-id` が入り、`--resume` が1つも入らない
//   - 身元ファイルに、起動したセッション UUID が書かれている
//   - 基本フローの事後条件（Status・コメント・pane・印・worktree と branch）が揃う
func TestDispatch_新規の着手は新しいセッションを立てる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "Ready")
	prompts := finishRunOnPrompt(t, fx, "session-1", "I_node188")
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
	worktreePath := filepath.Join(fx.WorktreeRoot,
		"github.com", "octocat", "hello-world", "continuo-octocat-hello-world-188")
	assertBasicFlowPostcondition(t, fx, issue, worktreePath)
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
	fx := newFixture(t, fixtureOptions{})

	issue := sampleIssue(188, "In Progress")
	wt := prepareWorktree(t, fx, issue, identityOverride{SessionUUID: "sess-188"})
	prompts := finishRunOnPrompt(t, fx, "sess-188", "I_node188")
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
	assertBasicFlowPostcondition(t, fx, issue, wt.Path)
}

// {"RUCM-PATH": "P027"}
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
//   - 起動フラグが新しいセッション UUID の指定つきへ差し替わっている
//     （`--session-id` が入り、その起動には `--resume` が1つも無い）
//   - 身元ファイルの `session_uuid` が新しい UUID へ書き直されている
//     （書き直さないと、次の再着手も同じ死んだ UUID へ復帰しにいく）
//   - issue の Status は running_state のままである（`failure_state` へ落とさない）
//   - 印は残っている
//   - worktree は残っている
func TestDispatch_復帰に失敗したら新しいセッションで始め直す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.AllowLog("前回のセッションへ復帰できなかったので")
	prompts := recordPrompts(fx)

	issue := sampleIssue(188, "In Progress")
	wt := prepareWorktree(t, fx, issue, identityOverride{SessionUUID: "sess-188"})
	fx.Tracker.AddIssue(issue)

	var mu sync.Mutex
	resumeAttempts := 0
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
		return map[string]any{
			"type": "agent_started",
			"agent": map[string]any{
				"name": params["name"], "agent_status": "idle",
				"interactive_ready": true, "pane_id": params["pane_id"],
			},
		}, nil
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
	if fresh == "sess-188" {
		t.Fatalf("死んだセッションの UUID を --session-id に使い回している: %q", fresh)
	}
	// **立て直した起動は、前回の会話履歴を1文字も読まない。**
	if resumes[freshAt] != "" {
		t.Fatalf("立て直した起動に --resume が残っている: %q", resumes[freshAt])
	}
	if got := identitySessionUUID(t, fx, 188); got != fresh {
		t.Fatalf("身元ファイルの session_uuid を書き直していない（次の再着手もまた復帰を試みる）: got %q, want %q",
			got, fresh)
	}

	// 代替フロー `復帰の失敗` の残りの事後条件。
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
