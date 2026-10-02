// {"RUCM-CFG-SHA256": "c95cc6ae81b01869b4037925ac9d04807880d4b7d789014c3d9eedf94473da35", "SOURCE": "docs/spec/usecases/particular_case/runを終えてworkerを止める.cfg.json"}
//
// **ユースケース記述「runを終えてworkerを止める」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package orchestrator_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/orchestrator"
)

// {"RUCM-PATH": "P003"}
//
// Test_runを終えてworkerを止める_P003_打ち切るときはworkerを止める前にコメントを確かめる は、
// 設計 3-25 の「いつ走らせるか」の表を確かめる。
//
// 目的: 「`max_dispatch_turns` に達した / stall で打ち切った → **走らせる。worker を止める前に
// 確認する**」を示す。**確かめないと、その run の成果が issue に何も残らない。**
//
// 与える情報: `agent.max_retries` が 0 の設定で stall した run（1回目の stall で
// リトライを使い切り、人間へ渡す分岐に入る）。コメントは1件も付いていない。
// 成功条件:
//   - セッションの復元（`agent.start --resume`）が走る
//   - それでも書かれないので Status が `failure_state` へ落ちる
func Test_runを終えてworkerを止める_P003_打ち切るときはworkerを止める前にコメントを確かめる(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{
		Now: clock.Now,
		Mutate: func(cfg *config.Config) {
			cfg.Claude.TurnTimeoutMs = 1000
			cfg.Agent.MaxRetries = 0
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	blockFirstPrompt(t, fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	clock.Advance(5 * time.Second)
	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "打ち切りの前にセッションの復元が走る", func() bool {
		for _, r := range fx.Herdr.Requests() {
			if r.Method != herdr.MethodAgentStart {
				continue
			}
			args, _ := r.Params["args"].([]any)
			if strings.Contains(joinAny(args), "--resume") {
				return true
			}
		}
		return false
	})
	waitFor(t, 20*time.Second, "Status が failure_state へ落ちる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})
}

// reviewWithoutComment は、成果のコメントを書かずに `review` を表明して終わる run を1件走らせる。
//
// **`finishRunClaimed` を `failureState` が空のまま通る道である。**打ち切り（`abandonRunClaimed`）や
// 着手の失敗（`failRun`）の道は、書かせ直しより先に自分で Status と通知を書くので、
// **書かせ直しの準備が失敗したときの動きを確かめられない。**
//
// **1回目の `agent.prompt` を受けた時点で `sabotage` を呼ぶ。**そこから先が書かせ直しの準備である。
//
// t: 呼び出し元のテスト。
// fx: fixture。
// issue: 対象の issue（`Ready` で置く）。
// sabotage: 書かせ直しの準備を失敗させる仕込み。引数は worktree の絶対パス。
func reviewWithoutComment(t *testing.T, fx *fixture, issue int, sabotage func(worktreePath string)) {
	t.Helper()
	fx.Tracker.AddIssue(sampleIssue(issue, "Ready"))

	path := writeTranscript(t, t.TempDir(), "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "CONTINUO-STATUS: review", false),
	})
	var mu sync.Mutex
	prompts := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		prompts++
		n := prompts
		mu.Unlock()
		if n == 1 {
			// **コメントは書かない。**書かせ直しの準備を失敗させてから turn を終える。
			worktreePath := ""
			for _, r := range fx.Herdr.Requests() {
				if r.Method == herdr.MethodWorktreeOpen {
					worktreePath, _ = r.Params["path"].(string)
					break
				}
			}
			sabotage(worktreePath)
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
}

// recoveryPrepFailure は、成果の報告を書かせ直す準備を失敗させる仕込み1件である。
type recoveryPrepFailure struct {
	// name はサブテストの名前である。
	name string
	// log は、その入口が出す WARN の目印である。
	log string
	// cause は、引き渡しの通知に載る原因の文の一部である。
	cause string
	// sabotage は失敗の仕込みである。
	sabotage func(t *testing.T, fx *fixture, worktreePath string)
}

// {"RUCM-PATH": "P007"}
//
// Test_runを終えてworkerを止める_P007_身元ファイルから材料を読めなければ人間へ渡す は、
// 代替フロー「復元の断念」の、身元ファイルから復元の材料を読めない側を検査する。
//
// 目的: 成果の報告を書かせ直す準備（身元ファイルを読む・設定ファイルのパスを読む）が失敗したとき、
// **黙って片付けへ進まず、Status を `failure_state` へ書き、引き渡しの通知を1件付ける**ことを示す。
// **黙って進むと、成果の報告が1件も無い issue が `In Review` に並び、書かれていないことが誰にも伝わらない。**
//
// 与える情報: 成果のコメントを書かずに `review` を表明して終わる run（Status は `In Review` になる）。
// 1回目の turn のあいだに、入口ごとの失敗を仕込む。
// 成功条件（入口ごとに）: `assertRecoveryPrepFailure` のとおり。
func Test_runを終えてworkerを止める_P007_身元ファイルから材料を読めなければ人間へ渡す(t *testing.T) {
	assertRecoveryPrepFailure(t, []recoveryPrepFailure{
		{
			name:  "身元ファイルを読めない",
			log:   "身元ファイルを読めないので復元できません",
			cause: "身元ファイル（`.continuo.json`）を読めなかった",
			sabotage: func(_ *testing.T, fx *fixture, worktreePath string) {
				_ = os.Remove(fx.Workspace.IdentityPath(worktreePath))
			},
		},
		{
			name:  "設定ファイルのパスが無い",
			log:   "復帰に使うセッション UUID か、設定ファイルのパスがありません",
			cause: "設定ファイルのパスか会話の ID",
			sabotage: func(_ *testing.T, fx *fixture, worktreePath string) {
				identity, err := fx.Workspace.ReadIdentity(worktreePath)
				if err != nil {
					return
				}
				identity.SettingsPath = ""
				_ = fx.Workspace.WriteIdentity(context.Background(), worktreePath, *identity)
			},
		},
	})
}

// {"RUCM-PATH": "P005"}
//
// Test_runを終えてworkerを止める_P005_workspaceを開き直す準備が失敗したら人間へ渡す は、
// 代替フロー「復元の断念」の、worktree を workspace として開き直す側を検査する。
//
// 目的: 成果の報告を書かせ直す準備（workspace を開き直す・pane を引く・agent 名を決める）が失敗したとき、
// **黙って片付けへ進まず、Status を `failure_state` へ書き、引き渡しの通知を1件付ける**ことを示す。
//
// 与える情報: 成果のコメントを書かずに `review` を表明して終わる run（Status は `In Review` になる）。
// 1回目の turn のあいだに、入口ごとの失敗を仕込む。
// 成功条件（入口ごとに）: `assertRecoveryPrepFailure` のとおり。
func Test_runを終えてworkerを止める_P005_workspaceを開き直す準備が失敗したら人間へ渡す(t *testing.T) {
	assertRecoveryPrepFailure(t, []recoveryPrepFailure{
		{
			name:  "workspace を開き直せない",
			log:   "復元のための workspace を開けません",
			cause: "herdr の workspace として開き直せなかった",
			sabotage: func(_ *testing.T, fx *fixture, _ string) {
				fx.Herdr.Handle(herdr.MethodWorktreeOpen, func(map[string]any) (any, *rpcErr) {
					return nil, &rpcErr{Code: "worktree_open_failed", Message: "テストが開かせない"}
				})
			},
		},
		{
			name:  "pane を引けない",
			log:   "復元のための pane を引けません",
			cause: "pane を引けなかった",
			sabotage: func(_ *testing.T, fx *fixture, _ string) {
				fx.Herdr.Handle(herdr.MethodPaneList, func(map[string]any) (any, *rpcErr) {
					return map[string]any{"type": "pane_list", "panes": []any{}}, nil
				})
			},
		},
		{
			name:  "agent 名を決められない",
			log:   "復元のための agent 名を決められません",
			cause: "agent 名を決められなかった",
			sabotage: func(_ *testing.T, fx *fixture, _ string) {
				fx.Herdr.Handle(herdr.MethodAgentList, func(map[string]any) (any, *rpcErr) {
					return nil, &rpcErr{Code: "internal_error", Message: "テストが一覧を返さない"}
				})
			},
		},
	})
}

// assertRecoveryPrepFailure は、書かせ直しの準備が失敗した run が人間へ渡ることを、入口ごとに確かめる。
//
// 成功条件（入口ごとに）:
//   - Status が `failure_state`（`Blocked`）になる
//   - 引き渡しの通知がちょうど1件付き、その入口の原因の文を含む
//   - 復元のための `agent.start --resume` を呼ばない
//   - run が実行中の一覧から外れる（片付けへ進んでいる）
//
// t: 呼び出し元のテスト。
// cases: 入口ごとの仕込み。
func assertRecoveryPrepFailure(t *testing.T, cases []recoveryPrepFailure) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newFixture(t, fixtureOptions{})
			fx.AllowLog(tc.log)
			reviewWithoutComment(t, fx, 188, func(worktreePath string) { tc.sabotage(t, fx, worktreePath) })

			waitFor(t, 30*time.Second, "Status が failure_state へ落ちる", func() bool {
				return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
			})
			fx.WaitRunsDrained(t, 20*time.Second)

			if !strings.Contains(fx.Logs.String(), tc.log) {
				t.Errorf("仕込んだ入口を通っていない（%q がログに無い）", tc.log)
			}
			handoffs := fx.Tracker.HandoffCommentsOf("I_node188")
			if len(handoffs) != 1 {
				t.Fatalf("引き渡しの通知が1件ではない: %d 件", len(handoffs))
			}
			if !strings.Contains(handoffs[0].Body, tc.cause) {
				t.Errorf("引き渡しの通知に、この入口の原因の文 %q が無い:\n%s", tc.cause, handoffs[0].Body)
			}
			for _, r := range fx.Herdr.Requests() {
				if r.Method != herdr.MethodAgentStart {
					continue
				}
				args, _ := r.Params["args"].([]any)
				if strings.Contains(joinAny(args), "--resume") {
					t.Errorf("準備が失敗したのに、復元のための agent.start を呼んでいる")
				}
			}
			if got := fx.Tracker.StateOf("PVTI_item188"); got != "Blocked" {
				t.Errorf("片付けのあとの Status が Blocked ではない: %q", got)
			}
		})
	}
}

// {"RUCM-PATH": "P007"}
//
// Test_runを終えてworkerを止める_P007_終端のrunでは準備が失敗してもStatusを書き換えず通知だけ付ける は、
// 代替フロー「復元の断念」の、Status が `terminal_states` に在る場合を検査する。
//
// 目的: 書かせ直しの準備が失敗しても、**人間が `Done` へ動かしたカードを `failure_state` へ書き直さない**
// ことを示す（`protectedStates` が書き込みを断る）。**通知は付ける。**成果の報告が無いことは伝える。
//
// 与える情報: Status が `Done` の issue の run（引き継いだ run。worktree のパスを持たない）。
// コメントは1件も付いていない。
// 成功条件:
//   - Status が `Done` のままである
//   - 引き渡しの通知がちょうど1件付き、worktree のパスが分からなかったことを含む
//   - run が実行中の一覧から外れる
func Test_runを終えてworkerを止める_P007_終端のrunでは準備が失敗してもStatusを書き換えず通知だけ付ける(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	issue := sampleIssue(189, "Done")
	fx.Tracker.AddIssue(issue)
	fx.AllowLog("worktree のパスが分からないので復元できません")
	// **worktree のパスを持たない run を印の集合へ入れる。**
	if !fx.Orc.Adopt(issue, orchestrator.AdoptedRun{
		AgentName:   normalize.SafeName("continuo-hello-world-189"),
		PaneID:      "p-189",
		SessionUUID: "session-1",
	}, false) {
		t.Fatalf("検査用の run を印の集合へ入れられません")
	}

	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "引き渡しの通知が付く", func() bool {
		return len(fx.Tracker.HandoffCommentsOf("I_node189")) > 0
	})
	fx.WaitRunsDrained(t, 20*time.Second)

	if got := fx.Tracker.StateOf(issue.ID); got != "Done" {
		t.Errorf("終端の Status を書き換えている: %q", got)
	}
	handoffs := fx.Tracker.HandoffCommentsOf("I_node189")
	if len(handoffs) != 1 {
		t.Fatalf("引き渡しの通知が1件ではない: %d 件", len(handoffs))
	}
	if !strings.Contains(handoffs[0].Body, "worktree のパスが分からず") {
		t.Errorf("引き渡しの通知に、worktree のパスが分からなかったことが無い:\n%s", handoffs[0].Body)
	}
}

// {"RUCM-PATH": "P006"}
//
// Test_runを終えてworkerを止める_P006_記録が無ければ復元せずに人間へ渡す は、設計 3-3c の取り戻し側を確かめる。
//
// 目的: 「**コメントの取り戻しも、記録が無い UUID へ `--resume` を投げない。**
// **ただし黙って抜けてはならない**」を示す。
//
// **抜けると、コメントを1件も書いていない run が `failure_state` へ落ちず、
// 引き渡しの通知も出ないまま `In Review` に並ぶ。**成果がまとめられていないことが
// 誰にも伝わらない。**変わるのは、herdr の待ちを使い切ってから落ちるか、その前に落ちるかだけである。**
//
// **この検査には経路の印を付けない**（設計 6-18e）。通る経路は
// `Test_issueを1件処理する_P001_既存のworktreeがあれば前回のセッションに復帰する` と同じ P001 である。
//
// 与える情報: 記録の置き場所を空にした状態で、turn を送ってから打ち切られる run。
// **エージェントはコメントを1件も書かない。**
// 成功条件:
//   - 復元のための `agent.start` を1回も呼ばない（記録が無いので投げない）
//   - issue の Status が `failure_state` になる
//   - 引き渡しの通知が1件は投稿されている（**黙って抜けていない**）
//   - 復元を飛ばした理由がログに残っている
func Test_runを終えてworkerを止める_P006_記録が無ければ復元せずに人間へ渡す(t *testing.T) {
	clock := newTestClock()
	// **採番したセッションの記録を置かせない。**既定では fixture が置くので
	// （実機では Claude Code が書く）、**そのままでは「記録が無い」状態を作れない。**
	fx := newFixture(t, fixtureOptions{
		Now:                    clock.Now,
		SkipSessionTranscripts: true,
		Mutate: func(cfg *config.Config) {
			cfg.Claude.TurnTimeoutMs = 1000
			cfg.Agent.MaxRetries = 0
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	blockFirstPrompt(t, fx)
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)
	fx.AllowLog("リトライの回数を使い切りました", "画面が変わらないまま",
		"turn が終わったことを検知できません", "stall",
		"復帰する先のセッションに会話の記録が無いので")

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	starts := fx.Herdr.CountMethod(herdr.MethodAgentStart)
	clock.Advance(5 * time.Second)
	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "issue が人間へ渡る", func() bool {
		return fx.Tracker.StateOf(issue.ID) == fx.Config.Tracker.FailureState
	})

	// **復元のための `agent.start` を呼んでいない。**記録が無いので投げない。
	if got := fx.Herdr.CountMethod(herdr.MethodAgentStart); got != starts {
		t.Errorf("記録が無いのに復元のための agent.start を呼んでいる: 着手のとき %d 回 → いま %d 回", starts, got)
	}

	// **引き渡しの通知が1件は出ている。**黙って抜けると、成果が無いことが誰にも伝わらない。
	//
	// **文面までは見ない。**引き渡しの通知は1つの run につき1件しか投稿しないので
	// （`TestHandoff_引き渡しの通知は1件だけ`）、**先に走った打ち切りの通知が枠を取る。**
	// **ここで確かめたいのは「復元を飛ばしても、人間へ渡す道は残っている」ことである。**
	posted := 0
	for _, c := range fx.Tracker.CommentsOf(nodeIDOf(t, issue)) {
		if strings.Contains(c.Body, "【対処】") {
			posted++
		}
	}
	if posted == 0 {
		t.Errorf("記録が無いことを理由に復元を飛ばしたのに、引き渡しの通知が1件も無い")
	}

	// **復元を飛ばしたことがログに残っている。**残らないと、なぜ復元しなかったのかが追えない。
	if !strings.Contains(fx.Logs.String(), "復帰する先のセッションに会話の記録が無い") {
		t.Errorf("復元を飛ばした理由がログに残っていない:\n%s", fx.Logs.String())
	}
}

// {"RUCM-PATH": "P004"}
//
// 目的: 書かせ直しの段2 で閉じたあと、セッションを復元できずに戻っても、閉じた記録を1件書くことを固定する。
//
// **段2 で閉じた Claude Code はもう動いていない。**保留を誰も書かないと、境目が前の run に残る。
//
// 与える情報: リトライを使い切って人間へ渡す run（stall で打ち切る）。書かせ直しの `agent.start --resume` が断られる。
// 成功条件: 引き渡しの通知が残ったあと、閉じた記録がちょうど1件あること。
func Test_runを終えてworkerを止める_P004_段2のあと失敗して戻っても記録を書く(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{
		Now: clock.Now,
		Mutate: func(cfg *config.Config) {
			cfg.Claude.TurnTimeoutMs = 1000
			cfg.Agent.MaxRetries = 0
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	blockFirstPrompt(t, fx)
	fx.Herdr.Handle(herdr.MethodAgentStart, func(params map[string]any) (any, *rpcErr) {
		args, _ := params["args"].([]any)
		if strings.Contains(joinAny(args), "--resume") {
			return nil, &rpcErr{Code: "agent_start_failed", Message: "No conversation found"}
		}
		return map[string]any{
			"type":  "agent_started",
			"agent": map[string]any{"name": params["name"], "agent_status": "idle", "interactive_ready": true, "pane_id": params["pane_id"]},
		}, nil
	})
	fx.Tracker.AddIssue(sampleIssue(309, "Ready"))
	fx.AllowLog("リトライの回数を使い切りました", "セッションを復元できません",
		"turn が終わったことを検知できません", "画面が変わらないまま", "stall")

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	clock.Advance(5 * time.Second)
	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "引き渡しの通知が issue に残る", func() bool {
		return len(fx.Tracker.HandoffCommentsOf("I_node309")) > 0
	})
	waitFor(t, 20*time.Second, "閉じた記録が書かれる", func() bool {
		return len(fx.Tracker.ClosedRecordsOf("I_node309")) > 0
	})
	time.Sleep(500 * time.Millisecond)

	if n := len(fx.Tracker.ClosedRecordsOf("I_node309")); n != 1 {
		t.Fatalf("閉じた記録が1件ではない: %d 件", n)
	}
}

// {"RUCM-PATH": "P002"}
//
// 目的: 報告の書かせ直しを通っても、閉じた記録は1件になることを固定する（段2 は書かずに保留する）。
//
// **段2 で書くと、人間がその記録を見て書いた許可が、段8 のあとの2件目の記録より前になり黙って落ちる。**
//
// 与える情報: 1回目の turn で成果のコメントを書かずに review を表明する run。書かせ直しの指示を受けたら
// エージェントのコメントを書く台本。
// 成功条件: 書かせ直し（`--resume`）が走り、run が終わったあとの閉じた記録がちょうど1件で、
// エージェントのコメントより後に書かれていること。
func Test_runを終えてworkerを止める_P002_書かせ直しでも記録は1件(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(308, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "CONTINUO-STATUS: review", false),
	})
	var mu sync.Mutex
	prompts := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		prompts++
		n := prompts
		mu.Unlock()
		if n == 1 {
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		} else {
			// **書かせ直しの指示を受けて、エージェントが成果を書く。**
			fx.Tracker.AddComment("I_node308", "<!-- continuo:agent -->\nこの run でやったこと", true, time.Now().Add(time.Hour))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 30*time.Second, "書かせ直しの指示が送られる", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return prompts >= 2
	})
	fx.WaitRunsDrained(t, 30*time.Second)

	records := fx.Tracker.ClosedRecordsOf("I_node308")
	if len(records) != 1 {
		t.Fatalf("閉じた記録が1件ではない: %d 件", len(records))
	}
	all := fx.Tracker.CommentsOf("I_node308")
	agentAt, recordAt := -1, -1
	for i, c := range all {
		if strings.Contains(c.Body, "この run でやったこと") {
			agentAt = i
		}
		if isClosedRecord(c) {
			recordAt = i
		}
	}
	if agentAt < 0 || recordAt < agentAt {
		t.Errorf("閉じた記録がエージェントのコメントより後にない（エージェント %d / 記録 %d）", agentAt, recordAt)
	}
}

// {"RUCM-PATH": "P003"}
//
// Test_runを終えてworkerを止める_P003_runが終わるときにコメントが無ければセッションを復元して書かせる は、
// 設計 3-25 の9段を確かめる。
//
// 目的: 「run が終わるときにコメントを確かめ、無ければ 3-25 の9段で書かせる
// （**毎 turn ではない**）」「continuo は代筆しない」を守っていることを示す。
//
// 与える情報: エージェントが `review` を表明するが、issue にコメントを1件も残さない。
// 成功条件:
//   - 先に `pane.close` で worker を止めてから（同じセッション UUID が2つ生きるのを防ぐ）
//   - `worktree.open` → `pane.list` → `agent.start`（`--resume <UUID>`）→ `agent.prompt` の順で復元する
//   - それでも書かれないので Status が `failure_state` へ落ちる
func Test_runを終えてworkerを止める_P003_runが終わるときにコメントが無ければセッションを復元して書かせる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "CONTINUO-STATUS: review", false),
	})
	prompts := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		prompts++
		if prompts == 1 {
			// **コメントは書かない。**
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 30*time.Second, "Status が failure_state へ落ちる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})
	// **後始末まで待つ。**Status は worker を止める前に書かれる（helpers_test.go の WaitRunsDrained）。
	fx.WaitRunsDrained(t, 10*time.Second)

	methods := fx.Herdr.Methods()
	closeIdx := indexOf(methods, herdr.MethodPaneClose)
	if closeIdx < 0 {
		t.Fatalf("復元の前に worker を止めていない: %v", methods)
	}
	// 復元の agent.start は pane.close より後に来る。
	resumeStart := -1
	for i := closeIdx; i < len(methods); i++ {
		if methods[i] == herdr.MethodAgentStart {
			resumeStart = i
			break
		}
	}
	if resumeStart < 0 {
		t.Fatalf("セッションを復元していない（agent.start が pane.close の後に無い）: %v", methods)
	}

	// **`--resume <UUID>` と `--settings` を毎回渡し直す**（復元されないため。設計 3-25）。
	var resumeArgs string
	for _, r := range fx.Herdr.Requests() {
		if r.Method != herdr.MethodAgentStart {
			continue
		}
		args, _ := r.Params["args"].([]any)
		joined := joinAny(args)
		if strings.Contains(joined, "--resume") {
			resumeArgs = joined
		}
	}
	if resumeArgs == "" {
		t.Fatalf("--resume を付けて起動していない: %v", fx.Herdr.Requests())
	}
	for _, want := range []string{"--resume session-1", "--settings", "--permission-mode auto"} {
		if !strings.Contains(resumeArgs, want) {
			t.Fatalf("復元の起動フラグに %q が無い: %q", want, resumeArgs)
		}
	}
	if strings.Contains(resumeArgs, "--session-id") {
		t.Fatalf("復元なのに --session-id を渡している（既に使った UUID は再利用できない）: %q", resumeArgs)
	}
}
