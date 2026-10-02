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
	// **記録は記録の根の直下1階層へ置く**（設計 3-3c）。着手の段5b がここを探し、
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
// `Test_issueを1件処理する_P001_既存のworktreeがあれば前回のセッションに復帰する` と同じ P001 である。
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

// TestDispatch_記録の判定は残り2つの場合も設計どおりに倒れる は、設計 3-3c の判定の表の
// 残り2行を確かめる。
//
// 目的: **設計 3-3c の判定の表が並べている場合のうち、検査があるのは「記録が1件も無い」だけ
// だった。**残り2つは、守りの向きを逆に書き換えても緑のまま通る状態にあった。
//
//	UUID がパスに使えない   … 復帰しない（身元ファイルはエージェントが書き換えられる。設計 3-2 / 3-23）
//	記録の置き場所が実在しない … 復帰しない（その下に記録は在りえない）
//
// **2つ目を「決められない」に倒してはならない。**Claude Code を1度も起動していない機械では
// 記録の置き場所がまだ無く、**再着手のたびに `--resume` を投げて空回りすることになる。**
//
// **「根はあるが読めない」（権限・IO の失敗）と「根が決まっていない」は、ここでは通らない。**
// 前者は権限を落とした状態を作る必要があり、後者は `os.UserHomeDir` が失敗したときにしか起きない
// （`fixtureOptions.TranscriptRoot` が空なら、そのテスト専用の根が入る）。
// **どちらも dispatch を通した検査では届かない。**
//
// **1つ目は、当たる記録を置いてから渡す。**置かないと、UUID の検査を消しても
// 「記録が1件も無い」に倒れて `--resume` が渡らず、**守りを消しても緑のまま通る。**
// `filepath.Join` は `..` を畳むので、`sess/../188` は `<記録の根>/<ディレクトリ>/188.jsonl` に当たる。
//
// **この検査にも経路の印を付けない**（設計 6-18e）。通る経路は
// `Test_issueを1件処理する_P001_既存のworktreeがあれば前回のセッションに復帰する` と同じ P001 である。
//
// 与える情報: 身元ファイルつきの worktree と `In Progress` の issue 1件。
// 記録の置き場所と身元ファイルの UUID を、場合ごとに変える。
// 成功条件: 使えない UUID では `--resume` が1つも渡らず、読めない場合では渡ること。
func TestDispatch_記録の判定は残り2つの場合も設計どおりに倒れる(t *testing.T) {
	cases := []struct {
		name string
		// uuid は身元ファイルへ書くセッション UUID である。
		uuid string
		// seedName は、記録を置くときのファイル名（拡張子を除く）である。空なら置かない。
		seedName string
		// missingRoot を真にすると、記録の置き場所として実在しないパスを渡す。
		missingRoot bool
		// wantResume は `--resume` が渡ることを期待するかである。
		wantResume bool
	}{
		{
			// **`/` はパスの区切りである。**通すと、根の外のファイルを見に行く組み立て方が成立する。
			// **`..` を畳んだ先へ実際に記録を置く。**置かないと、この検査を消しても緑のまま通る。
			name:       "UUIDがパスに使えない形なら復帰しない",
			uuid:       "sess/../188",
			seedName:   "188",
			wantResume: false,
		},
		{
			// **根が実在しないなら、その下に記録は在りえない**（設計 3-3c）。
			name:        "記録の置き場所が実在しないなら復帰しない",
			uuid:        "sess-188",
			missingRoot: true,
			wantResume:  false,
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
			if tc.seedName != "" {
				seedSessionTranscript(t, fx, tc.seedName, []any{
					typedUserLine("p0", "別のセッションの1行"),
					assistantLine("req0", "作業中です", false),
				})
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
