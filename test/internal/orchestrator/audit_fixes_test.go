// {"RUCM-CFG-SHA256": "4a61db11c52f5ba42b23b7180d4dfe2d79b39f257e065f54fe735fd3e48d11e6", "SOURCE": "docs/spec/usecases/particular_case/issue を1件処理する.cfg.json"}
//
// **全コード監査（2026-08-25）で確かめた指摘のうち、着手と turn と復元の7件の検査である。**
//
// **RUCM のパスから生成したものではないが、対応するテストパスには印を付けてある**
// （review_fixes_test.go と同じ扱い）。
// どれも「守りはあるのにテストが1本も検査していなかった」箇所なので、
// **足したテストは、守りを1箇所だけ潰すと必ず落ちることを実測してから置いている。**
package orchestrator_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// TestDispatch_active_statesに無いStatusのissueを着手が上書きしない は、
// 着手の段2 の許可リストを確かめる。
//
// 目的: **ボードの Status は人間が自由に増やせる。**`In Review` は `active_states` にも
// `terminal_states` にも `failure_state` にも入らない（設計 3-9 / 3-10。`In Review` を
// `terminal_states` に入れてはならない）し、設定にまったく出てこない Status も作れる。
// **拒否リストで守ると、そういう Status を全部見落とす。**見落とすと、人間が引き取った
// issue を continuo が `In Progress` へ上書きし、その worktree で Claude Code を起動し直す。
//
// 与える情報: ボードの実体は `active_states` に無い Status なのに、候補の一覧には
// 索引の遅れで `Ready` の写しが載っている issue。**設定に名前が出てくる `In Review` と、
// 設定のどこにも出てこない `Icebox` の両方を見る。**
// 成功条件: Status がそのままで、書き込みを1回も試みず、worktree も開かず、印も残らないこと。
func TestDispatch_active_statesに無いStatusのissueを着手が上書きしない(t *testing.T) {
	cases := []struct {
		name  string
		state string
	}{
		// 人間がレビューのために引き取った状態（設定の status_signal_map には出てくる）。
		{name: "InReview", state: "In Review"},
		// 設定のどこにも名前が出てこない状態。**拒否リストでは決して守れない。**
		{name: "Icebox", state: "Icebox"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newFixture(t, fixtureOptions{})
			holdPrompt(fx)
			fx.Tracker.AddIssue(sampleIssue(188, tc.state))
			// 候補の一覧にだけ、反映が追いついていない Ready の写しが載る。
			fx.Tracker.SetExtraCandidates(sampleIssue(188, "Ready"))

			fx.Orc.Tick(context.Background())

			// **どちらの実装でも待ち合わせが成り立つようにする。**許可リストが効いていれば
			// 取り直しだけが走り、効いていなければ書き込みまで走る。
			// **取り直しは timeline の有無で2本に分かれるので、両方を数える**（設計 3-61）。
			waitFor(t, 10*time.Second, "着手の試みが終わる", func() bool {
				return fx.Tracker.CountIDRefreshes() > 0 ||
					fx.Tracker.CountCall("UpdateStatus") > 0
			})
			time.Sleep(500 * time.Millisecond)

			if got := fx.Tracker.StateOf("PVTI_item188"); got != tc.state {
				t.Errorf("active_states に無い issue を上書きしている: got %q, want %q", got, tc.state)
			}
			if got := fx.Herdr.CountMethod(herdr.MethodWorktreeOpen); got != 0 {
				t.Errorf("着手してはいけない issue で段3 へ進んでいる: worktree.open を %d 回呼んだ", got)
			}
			if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
				t.Errorf("印が残っている: %d 件", got)
			}
		})
	}
}

// TestReconcile_身元ファイルのworkspaceIDを信じて別のrunのpaneを閉じない は、
// 巡回の worktree の照合（設計 3-9 の手順7b）が身元ファイルを検算することを確かめる。
//
// 目的: 身元ファイルは worktree の直下にあり、その worktree ではエージェントが
// `--permission-mode dontAsk` で動く（設計 3-16 の段9）。**`herdr_workspace_id` は
// エージェントが書き換えられる。**検算せずに `pane.close` へ渡すと、
// **無関係の issue で走っている Claude Code を turn の途中で殺せる。**
//
// 与える情報: 印に入っていない worktree の身元ファイルに、別の run の pane を持つ
// workspace の ID を書き込んでおく。その pane の cwd は別の worktree を指す。
// 成功条件: 別の run の pane を閉じないこと。
func TestReconcile_身元ファイルのworkspaceIDを信じて別のrunのpaneを閉じない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.VerifyStatesEvery = 0
			// **この巡回では1件も着手させない。**見たいのは worktree の照合だけである。
			cfg.Tracker.RequiredLabels = []string{"never-attached"}
		},
	})
	// 人間へ引き渡し済みの worktree（この中でエージェントが身元ファイルを書き換える）。
	forgedIssue := sampleIssue(188, "In Review")
	fx.Tracker.AddIssue(forgedIssue)
	forged := prepareWorktree(t, fx, forgedIssue, identityOverride{})

	// 別の run の worktree と、その中で走っている Claude Code の pane。
	victimIssue := sampleIssue(189, "In Review")
	fx.Tracker.AddIssue(victimIssue)
	victim := prepareWorktree(t, fx, victimIssue, identityOverride{})

	// 手順7b に入らせるための、active_states の issue（worktree は持たない）。
	fx.Tracker.AddIssue(sampleIssue(190, "In Progress"))

	// **攻撃。**引き渡し済みの worktree の身元ファイルへ、active_states の issue の ID と、
	// 別の run の workspace の ID を書く。どちらもエージェントが書ける場所である。
	identity, err := fx.Workspace.ReadIdentity(forged.Path)
	if err != nil {
		t.Fatalf("身元ファイルを読めません: %v", err)
	}
	identity.ProjectItemID = "PVTI_item190"
	identity.IssueIdentifier = "octocat/hello-world#190"
	identity.HerdrWorkspaceID = victim.WorkspaceID
	if err := fx.Workspace.WriteIdentity(context.Background(), forged.Path, *identity); err != nil {
		t.Fatalf("身元ファイルを書けません: %v", err)
	}

	// 生きている pane は別の run のものだけ（cwd は別の worktree を指す）。
	victimPane := victim.WorkspaceID + ":p-189"
	installPanes(fx, livePane{
		PaneID: victimPane, Cwd: victim.Path,
		AgentName: "continuo-hello-world-189", AgentStatus: herdr.AgentStatusIdle,
		SessionUUID: "sess-189",
	})
	fx.AllowLog("印に入っていない worktree に生きた pane があったので閉じます")

	fx.Orc.Tick(context.Background())
	time.Sleep(500 * time.Millisecond)

	for _, id := range closedPaneIDs(fx) {
		if id == victimPane {
			t.Fatalf("身元ファイルの workspace ID を信じて別の run の pane を閉じた: %v", closedPaneIDs(fx))
		}
	}
}

// {"RUCM-PATH": "P020"}
//
// TestTurn_turnを送れなかったときStopHookのせいにしない は、
// 送信の失敗と「Stop hook が届かない」を混ぜないことを確かめる。
//
// 目的: `agent.prompt` が `agent_not_found` などで断ると、**turn は1文字も届いていない。**
// それを `turnStalled` に混ぜると、issue には「herdr は agent が待機状態になったと答えたが
// **Stop hook から通知が届かなかった**」という**起きていないことを断定した文面**が残り、
// 人間は正常な設定ファイルを確かめに行かされる。
//
// 与える情報: `agent.prompt` が `agent_not_found` を返す（人間が pane を閉じた直後）。
// `agent.max_retries` は 0 なので、1回目の失敗でそのまま人間へ渡る。
// 成功条件: issue に残る理由が「送れませんでした」であり、Stop hook にも
// 設定ファイルにも言及しないこと。
func TestTurn_turnを送れなかったときStopHookのせいにしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Agent.MaxRetries = 0
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "agent_not_found", Message: "agent は登録されていません"}
	})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	// **コメントの取り戻しも同じ台本で落ちる。**この検証は agent.prompt を全部
	// agent_not_found にするので、引き渡しの直前に走るコメントの取り戻しも届かない。
	// 走る順番は機械の速さで前後するため、許可しておかないと環境によって落ちる。
	fx.AllowLog("turn を送れませんでした", "リトライの回数を使い切りました",
		"turn を1回も送っていないので", "コメントを書かせるプロンプトを送れません")

	fx.Orc.Tick(context.Background())

	// **着手の記録（Status を動かした記録）とは別物である。**引き渡しの通知だけを待つ。
	waitFor(t, 20*time.Second, "引き渡しの通知が issue に残る", func() bool {
		return len(fx.Tracker.HandoffCommentsOf("I_node188")) > 0
	})

	body := fx.Tracker.HandoffCommentsOf("I_node188")[0].Body
	if !strings.Contains(body, "herdr へ指示を送れませんでした") {
		t.Errorf("送れなかったことが issue に書かれていない:\n%s", body)
	}
	if strings.Contains(body, "Stop hook") {
		t.Errorf("送れていないのに Stop hook のせいにしている:\n%s", body)
	}
	if !strings.Contains(body, "agent_not_found") {
		t.Errorf("herdr が返した本当の原因が issue に書かれていない:\n%s", body)
	}
}

// TestComment_復元のworktreeOpenはリポジトリ本体をcwdに渡す は、
// コメントの取り戻し（設計 3-25 の段4）が本物の herdr に断られない呼び方をすることを確かめる。
//
// 目的: `worktree.open` は `cwd` にリポジトリ本体を渡さないと
// `worktree_not_found: worktree path not found` で断る（実測: 2026-08-25、test/live）。
// **`cwd` が無いと、エージェントに成果を書かせる最後の砦が本番で1度も働かない。**
//
// 与える情報: `worktree.open` を「`cwd` が空なら本物と同じく断る」台本に差し替えた上で、
// リトライを使い切って打ち切る run。
// 成功条件: 復元のための `worktree.open` に `cwd` と `focus` と `label` が載ること。
func TestComment_復元のworktreeOpenはリポジトリ本体をcwdに渡す(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{
		Now: clock.Now,
		Mutate: func(cfg *config.Config) {
			cfg.Claude.TurnTimeoutMs = 1000
			cfg.Agent.MaxRetries = 0
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	requireCwdOnWorktreeOpen(t, fx)
	blockFirstPrompt(t, fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.AllowLog("リトライの回数を使い切りました", "画面が変わらないまま",
		"turn が終わったことを検知できません", "stall")

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	clock.Advance(5 * time.Second)
	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "復元のための worktree.open が走る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodWorktreeOpen) >= 2
	})

	var opens []map[string]any
	for _, r := range fx.Herdr.Requests() {
		if r.Method == herdr.MethodWorktreeOpen {
			opens = append(opens, r.Params)
		}
	}
	last := opens[len(opens)-1]
	if cwd, _ := last["cwd"].(string); cwd == "" {
		t.Errorf("復元の worktree.open に cwd が載っていない: %v", last)
	}
	if _, ok := last["focus"]; !ok {
		t.Errorf("復元の worktree.open に focus が載っていない（人間の画面を奪う）: %v", last)
	}
	if label, _ := last["label"].(string); label != "octocat/hello-world/issues/188" {
		t.Errorf("復元の worktree.open の label が着手のときと揃っていない: got %q", label)
	}
}

// requireCwdOnWorktreeOpen は、テスト用herdr mock の `worktree.open` を本物と同じ厳しさにする。
//
// **本物の herdr は `cwd` を省くと `worktree_not_found: worktree path not found` で断る**
// （実測: 2026-08-25、test/live。設計 6-10 の表）。テスト用herdr mock が `cwd` を見ないままだと、
// **本番で1度も通らない呼び方をテストが通してしまう。**
//
// t: 呼び出し元のテスト。
// fx: fixture。
func requireCwdOnWorktreeOpen(t *testing.T, fx *fixture) {
	t.Helper()
	inner := fx.Herdr.HandlerOf(herdr.MethodWorktreeOpen)
	fx.Herdr.Handle(herdr.MethodWorktreeOpen, func(params map[string]any) (any, *rpcErr) {
		if cwd, _ := params["cwd"].(string); strings.TrimSpace(cwd) == "" {
			return nil, &rpcErr{Code: "worktree_not_found", Message: "worktree path not found"}
		}
		return inner(params)
	})
}

// {"RUCM-PATH": "P018"}
//
// TestAbandon_打ち切りのときissueに残る理由が本当の理由である は、
// 引き渡しの通知の投稿枠を、本当の理由が先に取ることを確かめる。
//
// 目的: 引き渡しの通知は1つの run につき1件しか投稿しない。**コメントの取り戻しの失敗が
// 先に枠を使うと、issue に残るのは「作業を終えたと表明したのに書き残さなかった」だけになる。**
// 実際にはエージェントは完了を表明しておらず、画面が止まって打ち切られている。
//
// 与える情報: `agent.max_retries` が 0 で stall する run。コメントは1件も書かれず、
// セッションの復元も通らない（`agent.start --resume` が断られる）。
// 成功条件: issue に残る本文が**打ち切った理由**（画面が止まった）であり、
// コメントの取り戻しの失敗の文面で置き換わっていないこと。
func TestAbandon_打ち切りのときissueに残る理由が本当の理由である(t *testing.T) {
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
	// セッションの復元を断らせる（コメントの取り戻しが失敗する経路に入れる）。
	var started sync.Once
	fx.Herdr.Handle(herdr.MethodAgentStart, func(params map[string]any) (any, *rpcErr) {
		args, _ := params["args"].([]any)
		if strings.Contains(joinAny(args), "--resume") {
			return nil, &rpcErr{Code: "agent_start_failed", Message: "No conversation found"}
		}
		started.Do(func() {})
		// **既定の台本と同じ形で返す。**画面の版を勝手に載せると、stall の判定が
		// 「版が動いた」と読んで打ち切りに入らない。
		return map[string]any{
			"type":  "agent_started",
			"agent": map[string]any{"name": params["name"], "agent_status": "idle", "interactive_ready": true, "pane_id": params["pane_id"]},
		}, nil
	})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.AllowLog("リトライの回数を使い切りました", "セッションを復元できません",
		"turn が終わったことを検知できません", "画面が変わらないまま", "stall")

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	clock.Advance(5 * time.Second)
	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "引き渡しの通知が issue に残る", func() bool {
		return len(fx.Tracker.HandoffCommentsOf("I_node188")) > 0
	})
	time.Sleep(500 * time.Millisecond)

	comments := fx.Tracker.HandoffCommentsOf("I_node188")
	if len(comments) != 1 {
		t.Fatalf("引き渡しの通知が1件ではない: %d 件", len(comments))
	}
	body := comments[0].Body
	if !strings.Contains(body, "止まったものと判断して打ち切りました") {
		t.Errorf("打ち切った本当の理由が issue に残っていない:\n%s", body)
	}
	if strings.Contains(body, "何をしたのかを issue に書き残しませんでした") {
		t.Errorf("コメントの取り戻しの失敗が投稿枠を先に取り、本当の理由を追い出している:\n%s", body)
	}
}

// TestRestore_置き場所と食い違う身元ファイルを鍵にしない は、
// 復元の段2 が `project_item_id` を検算することを確かめる。
//
// 目的: `project_item_id` はエージェントが書き換えられる（身元ファイルは worktree の直下にある）。
// 検算しないと、**書き換えた側の worktree が別 issue の run として印に入り、
// 被害者の worktree は『捨てた身元』として pane を閉じられる。**
//
// 与える情報: `octocat/hello-world` の下にある worktree の身元ファイルが、
// 別のリポジトリ（`octocat/other-repo`）の issue を名乗っている。
// 成功条件: その worktree を引き継がず、pane を1つも閉じず、worktree も消さないこと。
func TestRestore_置き場所と食い違う身元ファイルを鍵にしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})

	// **攻撃。**置き場所は octocat/hello-world なのに、別のリポジトリの issue を名乗る。
	identity, err := fx.Workspace.ReadIdentity(wt.Path)
	if err != nil {
		t.Fatalf("身元ファイルを読めません: %v", err)
	}
	identity.IssueIdentifier = "octocat/other-repo#188"
	identity.IssueURL = "https://github.com/octocat/other-repo/issues/188"
	if err := fx.Workspace.WriteIdentity(context.Background(), wt.Path, *identity); err != nil {
		t.Fatalf("身元ファイルを書けません: %v", err)
	}

	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})
	fx.AllowLog("身元ファイルの名乗りが worktree の置き場所と食い違う",
		"身元ファイルの無い worktree に pane がありました")

	result, _ := restore(t, fx)

	if len(result.Adopted) != 0 {
		t.Errorf("食い違う身元ファイルの worktree を引き継いだ: %v", result.Adopted)
	}
	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Errorf("食い違いを見つけただけで pane を閉じた: %v", ids)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Errorf("worktree を消してしまった: %v", err)
	}
	if got := fx.Tracker.StateOf(issue.ID); got != "In Progress" {
		t.Errorf("Status を動かしてしまった: got %q", got)
	}
}

// TestRestore_paneの一覧を取れないだけでStatusを人間へ渡さない は、
// 復元の段4 の失敗を「pane が無い」と読み替えないことを確かめる。
//
// 目的: `pane.list` が1回失敗しただけで突き合わせが空になると、**生きている pane を持つ
// run が全件『pane が無い』経路（段8）へ流れる。**`restart.orphan_running_action` が
// `to_failure_state` なら、**走っている全部の run が人間へ渡され、
// 「pane が残っていませんでした」という嘘の理由が issue に投稿される。**
//
// 与える情報: `In Progress` の run と生きた pane。`pane.list` はエラーを返す。
// `restart.orphan_running_action` は `to_failure_state`。
// 成功条件: Status が `In Progress` のままで、issue にコメントが1件も付かないこと。
func TestRestore_paneの一覧を取れないだけでStatusを人間へ渡さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Restart.OrphanRunningAction = "to_failure_state" },
	})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	fx.Herdr.Handle(herdr.MethodPaneList, func(map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "internal_error", Message: "herdr が一時的に落ちています"}
	})
	fx.AllowLog("pane の一覧を取れないので", "判断を保留します")

	_, hs := restore(t, fx)

	if want := []string{"Start", "ReplayPending", "StartDelivery"}; !equalStrings(hs.Calls(), want) {
		t.Fatalf("pane の一覧を取れないのに起動を続けていない: got %v", hs.Calls())
	}
	if got := fx.Tracker.StateOf(issue.ID); got != "In Progress" {
		t.Errorf("herdr の一時的な失敗1回で Status を落とした: got %q, want In Progress", got)
	}
	if got := len(fx.Tracker.CommentsOf("I_node188")); got != 0 {
		t.Errorf("pane が生きているのに「pane が残っていない」と issue へ書いた: %d 件", got)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Errorf("worktree を消してしまった: %v", err)
	}
}

// TestTurn_herdrが一瞬落ちただけでrunを捨てない は、
// 一時的な失敗の判定（`herdr.IsTransient`）が turn の失敗の経路で実際に使われていることを
// 確かめる。
//
// 目的: `internal/herdr/errors.go` は「**呼び出し側はこれが真のとき run を捨ててはならない。**
// herdr の再起動・socket の一時的な不通・応答の遅れがこれに当たる。次の巡回へ持ち越すこと」
// と約束している。**その約束を守る分岐が1つも無いと、herdr を再起動しただけで走行中の run が
// 諦められる。**リトライを消費し、使い切ると issue が failure_state へ落ち、
// **herdr が何も答えていないのに「herdr は agent が待機状態になったと答えました」という
// 文面がボードへ投稿される。**
//
// 与える情報: `agent.prompt` を受け取ったところで応答を書かずに接続を切るテスト用herdr mock
// （herdr の再起動そのものである。エラー応答では再現できない）。リトライは 0 回にしてあるので、
// **run を捨てる実装なら1回で打ち切りまで到達する。**
// 成功条件: Status が `In Progress` のままで、issue にコメントが1件も残らず、印も残り、
// **さらに次の巡回で `agent.prompt` を送り直さないこと**（届いていたかどうかは分からず、
// 送り直せば turn が二重に投入される）。
func TestTurn_herdrが一瞬落ちただけでrunを捨てない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Agent.MaxRetries = 0
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	// **herdr が再起動した場面である。**応答を書かずに接続を切ると、
	// continuo 側は ErrCodeTransport（Retryable が真）を受け取る。
	fx.Herdr.DropConnection(herdr.MethodAgentPrompt)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.AllowLog("herdr へ届かなかったので", "herdr との通信が一時的に失敗した")

	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "turn の送信が herdr へ届く", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	// 捨てる実装なら、ここで打ち切りまで走り切る。走り切らせてから見る。
	time.Sleep(2 * time.Second)

	if got := fx.Tracker.StateOf("PVTI_item188"); got != "In Progress" {
		t.Errorf("herdr が一瞬落ちただけで Status を落とした: got %q, want In Progress", got)
	}
	if got := fx.Tracker.HandoffCommentsOf("I_node188"); len(got) != 0 {
		t.Errorf("run を捨てて issue へ引き渡しを書いた: %d 件\n%s", len(got), got[0].Body)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Errorf("走行中の run を手放した: %d 件（1 件のはず）", got)
	}

	// **次の巡回で turn を送り直さない。**届いていたかどうかは分からないので、
	// 送り直すと turn が二重に投入される。待ち直すだけでよい。
	before := fx.Herdr.CountMethod(herdr.MethodAgentPrompt)
	fx.Orc.Tick(context.Background())
	time.Sleep(2 * time.Second)
	if got := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); got != before {
		t.Errorf("次の巡回で agent.prompt を送り直した: %d 回 → %d 回", before, got)
	}
}

// TestTurn_枠待ちの待ち直しがherdrへ届かなくてもrunを捨てない は、
// 一時的な失敗の判定が**枠待ちの待ち直しの経路でも**使われていることを確かめる。
//
// 目的: 枠を使い切ると、continuo は `agent.prompt` を再送せずに `agent.wait` で待ち直す
// （設計 3-27）。**その待ち直しの最中に herdr が再起動すると、run を捨ててはならない。**
// 捨てると、枠が明けるのを待っていただけの issue が failure_state へ落ちる。
//
// 与える情報: 着手のときは枠が空いていて（`pause_above_percent` に掛からない）、
// turn を送った瞬間に 100% になる偽の usage API。`agent.prompt` は herdr の `timeout` を返し、
// `agent.wait` は応答を書かずに接続を切る。リトライは 0 回。
// 成功条件: Status が `In Progress` のままで、issue にコメントが1件も残らず、
// **枠待ちの印も残ったままであること**（外すと stall の時計が動き出し、枠が明けるより
// 先に stall として諦めることになる）。
func TestTurn_枠待ちの待ち直しがherdrへ届かなくてもrunを捨てない(t *testing.T) {
	resetsAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	// **着手が済むまでは枠を空けておく。**100% のままだと `pause_above_percent` で
	// dispatch が止まり、turn の経路に1度も入れない。
	var full atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		percent := 0
		if full.Load() {
			percent = 100
		}
		w.Header().Set("Content-Type", "application/json")
		limit := map[string]any{"kind": "session", "percent": percent, "severity": "normal"}
		if percent == 100 {
			limit["resets_at"] = resetsAt
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"limits": []map[string]any{limit}}); err != nil {
			t.Errorf("偽の usage API が応答を書けません: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	reader := newUsageReader(t, srv.URL, "CONTINUO_TEST_OAUTH_TOKEN_TRANSIENT")

	fx := newFixture(t, fixtureOptions{
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.Agent.MaxRetries = 0
			cfg.Tracker.VerifyStatesEvery = 0
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
		},
	})
	// **turn を送った瞬間に枠を使い切る。**herdr の待ち受けは期限までに落ち着かなかった
	// （＝枠待ちの入口。設計 3-27）。
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(map[string]any) (any, *rpcErr) {
		full.Store(true)
		return nil, &rpcErr{Code: herdr.ErrCodeTimeout, Message: "待ち受けが期限までに落ち着きませんでした"}
	})
	// **待ち直しの最中に herdr が再起動した。**
	fx.Herdr.DropConnection(herdr.MethodAgentWait)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.AllowLog("枠待ちの待ち直しが herdr へ届かないので", "herdr との通信が一時的に失敗した")

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "turn の送信が herdr へ届く", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// 枠の写しを取り直させる（`pollQuota` は巡回と枠待ちの待ち直しでしか走らない）。
	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "枠待ちの待ち直しが herdr へ届く", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentWait) > 0
	})
	// 捨てる実装なら、ここで打ち切りまで走り切る。走り切らせてから見る。
	time.Sleep(2 * time.Second)

	if got := fx.Tracker.StateOf("PVTI_item188"); got != "In Progress" {
		t.Errorf("待ち直しが届かなかっただけで Status を落とした: got %q, want In Progress", got)
	}
	if got := fx.Tracker.HandoffCommentsOf("I_node188"); len(got) != 0 {
		t.Errorf("run を捨てて issue へ引き渡しを書いた: %d 件\n%s", len(got), got[0].Body)
	}
	v, ok := viewOfFixture(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatalf("走行中の run を手放した")
	}
	if !v.WaitingQuota {
		t.Errorf("枠待ちの印を外した（stall の時計が動き出し、枠が明ける前に諦めることになる）: %+v", v)
	}
}
