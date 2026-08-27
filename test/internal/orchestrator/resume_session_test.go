// {"RUCM-CFG-SHA256": "6894d2e2f32b6ce2d08afb087e8d399ac45b30b51d037b0ce5c9d6fabf9ae430", "SOURCE": "docs/spec/usecases/particular_case/issue を1件処理する.cfg.json"}
//
// **再着手でセッションに復帰することの検査である。**
//
// **worktree を新しく作る着手は、新しいセッション UUID を採番して `--session-id` で始める。**
// **既存の worktree を再利用する再着手は、身元ファイルのセッション UUID へ `--resume` で戻る。**
// 戻れなかったときは新しいセッションで始め直す（設計 3-3b）。
package orchestrator_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// {"RUCM-PATH": "P049"}
//
// TestDispatch_新規の着手は新しいセッションを立てる は、設計 3-3b の「新規」側を確かめる。
//
// 目的: 「**新規の着手（worktree を新しく作る）は、いままでどおり新しい UUID を採番して
// `--session-id` を渡す**」を示す。**復帰する相手が無いのに `--resume` を渡すと、
// `No conversation found with session ID` で1文字も起動しない。**
//
// 与える情報: worktree がまだ無い `Ready` の issue 1件。
// 成功条件: `agent.start` の起動フラグに `--session-id` が入り、`--resume` が1つも入らない。
func TestDispatch_新規の着手は新しいセッションを立てる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prompts := recordPrompts(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

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
// `In Progress` の issue 1件。
// 成功条件:
//   - `agent.start` の起動フラグが `--resume sess-188` であり、`--session-id` が無い
//   - 身元ファイルの `session_uuid` が `sess-188` のまま変わっていない
//   - 送られた本文が1回目の本文（5-3）である
func TestDispatch_既存のworktreeがあれば前回のセッションに復帰する(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prompts := recordPrompts(fx)

	issue := sampleIssue(188, "In Progress")
	prepareWorktree(t, fx, issue, identityOverride{SessionUUID: "sess-188"})
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
}

// {"RUCM-PATH": "P025"}
//
// TestDispatch_復帰に失敗したら新しいセッションで始め直す は、設計 3-3b の後始末を確かめる。
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
// 成功条件:
//   - `--resume` の失敗のあと、`--session-id` つきの `agent.start` が呼ばれる
//   - 身元ファイルの `session_uuid` が新しい UUID へ書き直されている
//     （書き直さないと、次の再着手も同じ死んだ UUID へ復帰しにいく）
//   - turn が送られている（run が失敗として落ちていない）
func TestDispatch_復帰に失敗したら新しいセッションで始め直す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.AllowLog("前回のセッションへ復帰できなかったので")
	prompts := recordPrompts(fx)

	issue := sampleIssue(188, "In Progress")
	prepareWorktree(t, fx, issue, identityOverride{SessionUUID: "sess-188"})
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
	fresh := ""
	for _, id := range starts {
		if id != "" {
			fresh = id
			break
		}
	}
	if fresh == "" {
		t.Fatalf("復帰に失敗したあと --session-id つきで起動し直していない: %v", starts)
	}
	if fresh == "sess-188" {
		t.Fatalf("死んだセッションの UUID を --session-id に使い回している: %q", fresh)
	}
	if got := identitySessionUUID(t, fx, 188); got != fresh {
		t.Fatalf("身元ファイルの session_uuid を書き直していない（次の再着手もまた復帰を試みる）: got %q, want %q",
			got, fresh)
	}
}
