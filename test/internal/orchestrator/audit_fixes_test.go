// **全コード監査（2026-08-25）で確かめた指摘のうち、着手と turn と復元の7件の検査である。**
//
// **RUCM のパスから生成したものではないが、対応するテストパスには印を付けてある**
// （review_fixes_test.go と同じ扱い）。
// どれも「守りはあるのにテストが1本も検査していなかった」箇所なので、
// **足したテストは、守りを1箇所だけ潰すと必ず落ちることを実測してから置いている。**
package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// TestDispatch_active_statesに無いStatusのissueを着手が上書きしない は、
// 着手の段2 の許可リストを確かめる。
//
// 目的: **カンバンの Status は人間が自由に増やせる。**`In Review` は `active_states` にも
// `terminal_states` にも `failure_state` にも入らない（設計 3-9 / 3-10。`In Review` を
// `terminal_states` に入れてはならない）し、設定にまったく出てこない Status も作れる。
// **拒否リストで守ると、そういう Status を全部見落とす。**見落とすと、人間が引き取った
// issue を continuo が `In Progress` へ上書きし、その worktree で Claude Code を起動し直す。
//
// 与える情報: カンバンの実体は `active_states` に無い Status なのに、候補の一覧には
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
// `--permission-mode auto`（既定）で動く（設計 3-16 の段9）。**`herdr_workspace_id` は
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

// TestComment_復元のworktreeOpenはリポジトリ本体をcwdに渡す は、
// コメントの取り戻し（設計 3-25 の段4）が本物の herdr に断られない呼び方をすることを確かめる。
//
// 目的: `worktree.open` は `cwd` にリポジトリ本体を渡さないと断る
// （実測: 2026-08-25 の herdr 0.8.x は `worktree_not_found: worktree path not found`、
//
//	2026-09-29 の herdr 0.9.1 は `linked_worktree_source`。test/live）。
//
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
// **本物の herdr は `cwd` を省くと断る。**返すコードは版で変わり、herdr 0.8.x は
// `worktree_not_found: worktree path not found`（実測: 2026-08-25）、**herdr 0.9.1 は
// `linked_worktree_source: New and open worktree actions start from the repo parent workspace.`**
// （実測: 2026-09-29）である。test/live。設計 6-10 の表。
// **台本はいまの版に合わせる。**テスト用herdr mock が `cwd` を見ないままだと、
// **本番で1度も通らない呼び方をテストが通してしまう。**
//
// t: 呼び出し元のテスト。
// fx: fixture。
func requireCwdOnWorktreeOpen(t *testing.T, fx *fixture) {
	t.Helper()
	inner := fx.Herdr.HandlerOf(herdr.MethodWorktreeOpen)
	fx.Herdr.Handle(herdr.MethodWorktreeOpen, func(params map[string]any) (any, *rpcErr) {
		if cwd, _ := params["cwd"].(string); strings.TrimSpace(cwd) == "" {
			return nil, &rpcErr{
				Code:    "linked_worktree_source",
				Message: "New and open worktree actions start from the repo parent workspace.",
			}
		}
		return inner(params)
	})
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
// 文面がカンバンへ投稿される。**
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
