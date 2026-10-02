// {"RUCM-CFG-SHA256": "c031b7a3b30d694a678b90f4cb7ffca94d175c3697058eef566c6ccae83d08c1", "SOURCE": "docs/spec/usecases/particular_case/再起動して実行中の issue を引き継ぐ.cfg.json"}
//
// **ユースケース記述「再起動して実行中の issue を引き継ぐ」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package orchestrator_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/hookserver"
	"github.com/maimuzo/continuo/internal/orchestrator"
)

// {"RUCM-PATH": "P003"}
//
// Test_再起動して実行中のissueを引き継ぐ_P003_生きているpaneを引き継いで印と実行中の一覧へ入れ直す は、
// 復元の中心の経路を1本で確かめる。
//
// 目的: 「引き継いだ run を印の集合へ入れ直す」（設計 3-4 の段6）ことと、
// hook の受け口が listen → 読み戻し → 配送の順で呼ばれること（段5d / 5e / 6b）を確かめる。
//
// 与える情報:
//   - `In Progress` の issue が1件。その worktree と身元ファイルがディスクにある
//   - その worktree を cwd に持つ pane が生きていて、agent_status は idle
//
// 成功条件:
//   - 印（実行中の一覧）に入る
//   - hook の受け口が Start → ReplayPending → StartDelivery の順で呼ばれる
//   - **復元の中で `agent.prompt` を1回も呼ばない**（段5c）
//   - 引き継いだ回数が身元ファイルへ書き戻される（段5b）
//   - セッション UUID の索引が復元され、hook を受け取れる
//   - turn 数は 1 から数え直す（引き継いだ直後は 0 回）
func Test_再起動して実行中のissueを引き継ぐ_P003_生きているpaneを引き継いで印と実行中の一覧へ入れ直す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{SessionUUID: "sess-188"})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})

	result, hs := restore(t, fx)

	if got := fx.Orc.RunningIdentifiers(); len(got) != 1 || got[0] != issue.Identifier {
		t.Fatalf("引き継いだ run が印に入っていない: got %v", got)
	}
	if len(result.Adopted) != 1 || result.Adopted[0] != issue.Identifier {
		t.Fatalf("復元の記録に引き継ぎが残っていない: got %v", result.Adopted)
	}
	if want := []string{"Start", "ReplayPending", "StartDelivery"}; !equalStrings(hs.Calls(), want) {
		t.Fatalf("hook の受け口の呼び順が違う: got %v, want %v", hs.Calls(), want)
	}
	if n := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); n != 0 {
		t.Fatalf("復元の中で agent.prompt を呼んだ（wait つきの呼び出しは1時間返らない）: %d 回", n)
	}
	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("引き継いだ run の pane を閉じてしまった: %v", ids)
	}

	identity, err := fx.Workspace.ReadIdentity(wt.Path)
	if err != nil {
		t.Fatalf("身元ファイルを読めない: %v", err)
	}
	if identity.TakeoverCount != 1 {
		t.Fatalf("引き継いだ回数を身元ファイルへ書き戻していない: got %d, want 1", identity.TakeoverCount)
	}

	// セッション UUID の索引が復元されている（hook の対応づけ）。
	if !fx.Orc.OnHook(stopEvent("sess-188", "", "")) {
		t.Fatalf("引き継いだ run のセッション UUID で hook を受け取れない")
	}

	views := fx.Orc.RunViews()
	if len(views) != 1 || views[0].TurnCount != 0 {
		t.Fatalf("turn 数を 1 から数え直していない: got %+v", views)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_再起動して実行中のissueを引き継ぐ_P001_引き継いだrunにはstallの時計が引き継いだ時刻から始まる は、
// `runState.LastSeenAt` に引き継いだ時刻が入ることを確かめる。
//
// 目的: ゼロ値のままだと、引き継いだ直後の巡回で即座に stall と判定されて worker が
// 止められる（設計 3-4 の段5c）。
//
// 与える情報: 引き継げる run を1件と、`claude.turn_timeout_ms` が 60 秒の設定。
//
// 成功条件: 引き継いだ直後に巡回を1回回しても、pane が閉じられず印に残っている。
func Test_再起動して実行中のissueを引き継ぐ_P001_引き継いだrunにはstallの時計が引き継いだ時刻から始まる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Claude.TurnTimeoutMs = 60000
	}})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusWorking, SessionUUID: "sess-188",
	})

	restore(t, fx)
	fx.Orc.Tick(context.Background())

	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("引き継いだ直後に stall と判定されて pane が閉じられた: %v", ids)
	}
	if got := fx.Orc.RunningIdentifiers(); len(got) != 1 {
		t.Fatalf("引き継いだ run が印から外れた: got %v", got)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_再起動して実行中のissueを引き継ぐ_P001_agent_statusがworkingならNeedsPromptを立てない は、
// 走っている turn に別の turn を投げないことを確かめる。
//
// 目的: 設計 3-4 の段5a2。走っている最中に投げると turn が混ざる。
//
// 与える情報: 引き継げる run を1件。agent_status は working。
//
// 成功条件: 引き継いだあと巡回を1回回しても `agent.prompt` を送らない。
func Test_再起動して実行中のissueを引き継ぐ_P001_agent_statusがworkingならNeedsPromptを立てない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusWorking, SessionUUID: "sess-188",
	})

	restore(t, fx)
	fx.Orc.Tick(context.Background())
	time.Sleep(200 * time.Millisecond)

	if n := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); n != 0 {
		t.Fatalf("working の run へ turn を送った（turn が混ざる）: %d 回", n)
	}
	if got := fx.Orc.RunningIdentifiers(); len(got) != 1 {
		t.Fatalf("working の run を引き継いでいない: got %v", got)
	}
}

// {"RUCM-PATH": "P003"}
//
// Test_再起動して実行中のissueを引き継ぐ_P003_idleなら継続の指示を送る は、引き継いだ run へ送る本文を確かめる。
//
// 目的: 設計 3-4 の段5c。**送るのは継続の指示（5-4）であり、1回目の本文（5-3）ではない。**
// セッションは引き継いでいるので、エージェントは issue の URL も作法も既に知っている。
//
// 与える情報: 引き継げる run を1件（agent_status は idle）。
//
// 成功条件: 巡回の turn ループが送った本文が「続けてください」で始まり、
// 1回目のテンプレートの文言を含まない。
func Test_再起動して実行中のissueを引き継ぐ_P003_idleなら継続の指示を送る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})

	prompted := &stringBox{}
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		text, _ := params["text"].(string)
		prompted.Set(text)
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	restore(t, fx)
	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "継続の指示が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	if got := prompted.Get(); !strings.HasPrefix(got, "続けてください。") {
		t.Fatalf("継続の指示（5-4）ではない本文を送った: %q", got)
	}
	if got := prompted.Get(); strings.Contains(got, "を実装してください") {
		t.Fatalf("1回目の本文（5-3）を送り直してしまった: %q", got)
	}
}

// {"RUCM-PATH": "P007"}
//
// Test_再起動して実行中のissueを引き継ぐ_P007_agent_statusがblockedなら引き継がずfailure_stateへ落としてpaneを閉じる は、
// 保留中の権限要求が承認されて実行されるのを防ぐ。
//
// 目的: 設計 3-4 の段5a2。**blocked のまま引き継いで turn を送ると、保留中の権限要求が
// 承認されて実行される**（3-11 で実測。3/3）。
//
// 与える情報: `In Progress` の issue と、agent_status が blocked の pane。
//
// 成功条件: 印に入らず、pane が閉じられ、Status が `failure_state`（Blocked）へ落ちる。
// worktree は残る。
func Test_再起動して実行中のissueを引き継ぐ_P007_agent_statusがblockedなら引き継がずfailure_stateへ落としてpaneを閉じる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusBlocked, SessionUUID: "sess-188",
	})

	restore(t, fx)

	if got := fx.Orc.RunningIdentifiers(); len(got) != 0 {
		t.Fatalf("blocked の run を引き継いでしまった: got %v", got)
	}
	if ids := closedPaneIDs(fx); indexOf(ids, "p-188") < 0 {
		t.Fatalf("blocked の run の pane を閉じていない: %v", ids)
	}
	if got := fx.Tracker.StateOf(issue.ID); got != "Blocked" {
		t.Fatalf("failure_state へ落としていない: got %q, want %q", got, "Blocked")
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree を残していない: %v", err)
	}
}

// {"RUCM-PATH": "P009"}
//
// Test_再起動して実行中のissueを引き継ぐ_P009_agent_statusが知らない値ならpaneを閉じてworktreeとStatusを残す は、
// 判断できない run を引き継がないことを確かめる。
//
// 目的: 設計 3-4 の段5a2 の「取れない / 知らない値」。
//
// 与える情報: agent_status が unknown の pane。
//
// 成功条件: 印に入らず、pane が閉じられ、Status は動かず、worktree は残る。
func Test_再起動して実行中のissueを引き継ぐ_P009_agent_statusが知らない値ならpaneを閉じてworktreeとStatusを残す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusUnknown, SessionUUID: "sess-188",
	})

	restore(t, fx)

	if got := fx.Orc.RunningIdentifiers(); len(got) != 0 {
		t.Fatalf("判断できない run を引き継いでしまった: got %v", got)
	}
	if ids := closedPaneIDs(fx); indexOf(ids, "p-188") < 0 {
		t.Fatalf("pane を閉じていない: %v", ids)
	}
	if got := fx.Tracker.StateOf(issue.ID); got != "In Progress" {
		t.Fatalf("Status を動かしてしまった: got %q", got)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree を残していない: %v", err)
	}
}

// {"RUCM-PATH": "P013"}
//
// Test_再起動して実行中のissueを引き継ぐ_P013_agent名の無いpaneは閉じてworktreeとStatusを残す は、段8b を確かめる。
//
// 目的: `agent.prompt` / `agent.wait` の宛先は agent 名である。pane ID では送れないので、
// agent 名が引けない pane は引き継げない（設計 3-4 の段8b）。
//
// 与える情報: `agent.list` に載っていない pane。
//
// 成功条件: 印に入らず、pane が閉じられ、worktree と Status は残る。
func Test_再起動して実行中のissueを引き継ぐ_P013_agent名の無いpaneは閉じてworktreeとStatusを残す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "", // agent.list に載せない
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})

	restore(t, fx)

	if got := fx.Orc.RunningIdentifiers(); len(got) != 0 {
		t.Fatalf("agent 名の無い pane を引き継いでしまった: got %v", got)
	}
	if ids := closedPaneIDs(fx); indexOf(ids, "p-188") < 0 {
		t.Fatalf("pane を閉じていない: %v", ids)
	}
	if got := fx.Tracker.StateOf(issue.ID); got != "In Progress" {
		t.Fatalf("Status を動かしてしまった: got %q", got)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree を残していない: %v", err)
	}
}

// {"RUCM-PATH": "P015"}
//
// Test_再起動して実行中のissueを引き継ぐ_P015_socketのパスが前回と違えば引き継がずpaneを閉じる は、設計 3-23 を確かめる。
//
// 目的: 探索順は環境に依存するので、別の起動方法で立て直すと socket が別のパスに落ちる。
// run 中の Claude Code は前回のパスを持ったままなので、hook をもう届けられない。
//
// 与える情報: 身元ファイルの `socket_path` が今回のパスと違う run。
//
// 成功条件: 引き継がず pane を閉じる。worktree と Status は残る。
// **両方のパスがログに出る**（運用の環境が変わったことに人間が気づけるようにする）。
func Test_再起動して実行中のissueを引き継ぐ_P015_socketのパスが前回と違えば引き継がずpaneを閉じる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{SocketPath: "/tmp/前回の場所/hooks.sock"})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})

	restore(t, fx)

	if got := fx.Orc.RunningIdentifiers(); len(got) != 0 {
		t.Fatalf("socket のパスが違うのに引き継いでしまった: got %v", got)
	}
	if ids := closedPaneIDs(fx); indexOf(ids, "p-188") < 0 {
		t.Fatalf("pane を閉じていない: %v", ids)
	}
	logs := fx.Logs.String()
	if !strings.Contains(logs, "/tmp/前回の場所/hooks.sock") || !strings.Contains(logs, fx.SocketPath) {
		t.Fatalf("両方の socket のパスをログに出していない: %s", logs)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree を残していない: %v", err)
	}
}

// {"RUCM-PATH": "P005"}
//
// Test_再起動して実行中のissueを引き継ぐ_P005_引き継いだ回数が上限ならturnを1回も送らずfailure_stateへ落とす は、
// 設計 3-4 の段5b を確かめる。
//
// 目的: 落ちるたびに turn 数が 1 に戻るので、引き継いだ回数で打ち切らないと
// 打ち切りが永久に発火しない。**判定は turn を送る前に行う。**
//
// 与える情報: `agent.max_takeover` が 2 で、身元ファイルの `takeover_count` が 2 の run。
//
// 成功条件: 印に入らず、`agent.prompt` を1回も送らず、pane を閉じ、
// Status が `failure_state` へ落ちる。worktree は残る。
func Test_再起動して実行中のissueを引き継ぐ_P005_引き継いだ回数が上限ならturnを1回も送らずfailure_stateへ落とす(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Agent.MaxTakeover = 2
	}})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{TakeoverCount: 2})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})

	restore(t, fx)

	if got := fx.Orc.RunningIdentifiers(); len(got) != 0 {
		t.Fatalf("上限に達した run を引き継いでしまった: got %v", got)
	}
	if n := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); n != 0 {
		t.Fatalf("上限に達した run へ turn を送った: %d 回", n)
	}
	if ids := closedPaneIDs(fx); indexOf(ids, "p-188") < 0 {
		t.Fatalf("pane を閉じていない: %v", ids)
	}
	if got := fx.Tracker.StateOf(issue.ID); got != "Blocked" {
		t.Fatalf("failure_state へ落としていない: got %q", got)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree を残していない: %v", err)
	}
}

// {"RUCM-PATH": "P035"}
//
// Test_再起動して実行中のissueを引き継ぐ_P035_同じissueのworktreeが2つあるとき新しいほうを採り古いほうのpaneを段4で閉じる は、
// 設計 3-4 の段2 と段4 を確かめる。
//
// 目的: 段2 で決めるのは「どちらを採るか」だけであり、pane を閉じるのは段4 である
// （段2 の時点では誰が生きているかを知らない）。
//
// 与える情報: 同じ project item の ID を持つ worktree が2つ。
// 片方は `created_at` が古く、両方に pane が付いている。
//
// 成功条件: 新しいほうを引き継ぎ、古いほうの pane だけを閉じる。
// **古いほうの worktree は消さない**（どちらに成果があるか判断できない）。
func Test_再起動して実行中のissueを引き継ぐ_P035_同じissueのworktreeが2つあるとき新しいほうを採り古いほうのpaneを段4で閉じる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)

	newer := prepareWorktree(t, fx, issue, identityOverride{
		SessionUUID: "sess-new", CreatedAt: time.Now(),
	})
	// 古いほうは、別の issue 番号で worktree を作り、身元ファイルだけ同じ ID にする
	// （「同じ issue の worktree が2つある」状態をディスクの上に作る）。
	other := sampleIssue(999, "In Progress")
	older := prepareWorktree(t, fx, other, identityOverride{
		SessionUUID: "sess-old", CreatedAt: time.Now().Add(-2 * time.Hour),
	})
	rewriteIdentityItemID(t, fx, older.Path, issue.ID, issue.Identifier)

	installPanes(fx,
		livePane{PaneID: "p-new", Cwd: newer.Path, AgentName: "continuo-hello-world-188",
			AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-new"},
		livePane{PaneID: "p-old", Cwd: older.Path, AgentName: "continuo-hello-world-999",
			AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-old"},
	)

	restore(t, fx)

	if got := fx.Orc.RunningIdentifiers(); len(got) != 1 {
		t.Fatalf("引き継いだ run が1件でない: got %v", got)
	}
	ids := closedPaneIDs(fx)
	if indexOf(ids, "p-old") < 0 {
		t.Fatalf("古いほうの pane を閉じていない（同じ issue に2つの Claude Code が居る）: %v", ids)
	}
	if indexOf(ids, "p-new") >= 0 {
		t.Fatalf("採ったほうの pane まで閉じてしまった: %v", ids)
	}
	if _, err := os.Stat(older.Path); err != nil {
		t.Fatalf("採らなかったほうの worktree を消してしまった: %v", err)
	}
}

// {"RUCM-PATH": "P021"}
//
// Test_再起動して実行中のissueを引き継ぐ_P021_In_Reviewのrunはpaneもworktreeも残して何もしない は、
// 設計 3-4 の段5a の「引き渡し」を確かめる。
//
// 目的: 再起動の直後は、その pane が「人間のレビュー待ちで正常に止まっているもの」なのか
// 「取り残されたもの」なのかを区別できない（8-1）。
//
// 与える情報: Status が `In Review` の run と、その pane。
//
// 成功条件: pane を閉じず、worktree を消さず、Status を巻き戻さず、印にも入れない。
func Test_再起動して実行中のissueを引き継ぐ_P021_In_Reviewのrunはpaneもworktreeも残して何もしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Review")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})

	restore(t, fx)

	if got := fx.Orc.RunningIdentifiers(); len(got) != 0 {
		t.Fatalf("引き渡し状態の run を印へ入れてしまった: got %v", got)
	}
	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("引き渡し状態の pane を閉じてしまった: %v", ids)
	}
	if got := fx.Tracker.StateOf(issue.ID); got != "In Review" {
		t.Fatalf("Status を巻き戻してしまった: got %q", got)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree を消してしまった: %v", err)
	}
}

// {"RUCM-PATH": "P021"}
//
// Test_再起動して実行中のissueを引き継ぐ_P021_Doneでもcleanup_on_statesに入っていなければ片付けない は、
// 上のテストの裏返しである。
//
// 目的: `terminal_states` を見て片付けてしまう取り違えを検出する。
//
// 与える情報: `cleanup.on_states` が `Archived` だけの設定と、Status が `Done` の run。
//
// 成功条件: worktree が残る（`Done` は `terminal_states` だが `cleanup.on_states` ではない）。
func Test_再起動して実行中のissueを引き継ぐ_P021_Doneでもcleanup_on_statesに入っていなければ片付けない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Cleanup.OnStates = []string{"Archived"}
		cfg.Tracker.TerminalStates = []string{"Done"}
	}})
	issue := sampleIssue(188, "Done")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})

	restore(t, fx)

	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("cleanup.on_states に無いのに片付けてしまった: %v", err)
	}
}

// {"RUCM-PATH": "P023"}
//
// Test_再起動して実行中のissueを引き継ぐ_P023_cleanup_on_statesならpaneを閉じて片付ける は、
// 片付けの条件が `cleanup.on_states` であることを確かめる。
//
// 目的: 設計 3-4 の段5a。**`terminal_states` ではない。**既定値はどちらも `["Done"]` だが
// 別のキーであり、取り違えると片付けが起きない／余計に起きる。
//
// 与える情報: `cleanup.on_states` に `Archived`、`terminal_states` に `Done` を入れた設定と、
// Status が `Archived` の run。**既定値のままだと取り違えを検出できないので別の値にする。**
//
// 成功条件: pane を閉じ、worktree が実際に消える。印には入れない。
func Test_再起動して実行中のissueを引き継ぐ_P023_cleanup_on_statesならpaneを閉じて片付ける(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Cleanup.OnStates = []string{"Archived"}
		cfg.Tracker.TerminalStates = []string{"Done"}
		cfg.Cleanup.RequirePushed = false
	}})
	issue := sampleIssue(188, "Archived")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})

	result, _ := restore(t, fx)

	if got := fx.Orc.RunningIdentifiers(); len(got) != 0 {
		t.Fatalf("片付ける run を印へ入れてしまった: got %v", got)
	}
	if ids := closedPaneIDs(fx); indexOf(ids, "p-188") < 0 {
		t.Fatalf("片付ける前に pane を閉じていない: %v", ids)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree を片付けていない: %v", err)
	}
	if len(result.Cleaned) != 1 {
		t.Fatalf("片付けた記録が残っていない: got %v", result.Cleaned)
	}
}

// {"RUCM-PATH": "P027"}
//
// Test_再起動して実行中のissueを引き継ぐ_P027_取り直しで見つからないrunはpaneもworktreeも残して印から外す は、
// 設計 3-4 の段5a の「取り直しで見つからなかった」を確かめる。
//
// 目的: カンバンから外された・archive された issue を勝手に消さない。
//
// 与える情報: 身元ファイルはあるが、カンバンに載っていない issue。
//
// 成功条件: pane も worktree も残り、印に入らず、ログに残る。
func Test_再起動して実行中のissueを引き継ぐ_P027_取り直しで見つからないrunはpaneもworktreeも残して印から外す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	// **カンバンには足さない。**
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})

	restore(t, fx)

	if got := fx.Orc.RunningIdentifiers(); len(got) != 0 {
		t.Fatalf("カンバンに無い run を印へ入れてしまった: got %v", got)
	}
	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("カンバンに無い run の pane を閉じてしまった: %v", ids)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree を消してしまった: %v", err)
	}
	if !strings.Contains(fx.Logs.String(), "取り直しで見つからなかった") {
		t.Fatalf("見つからなかったことをログに出していない: %s", fx.Logs.String())
	}
}

// {"RUCM-PATH": "P029"}
//
// Test_再起動して実行中のissueを引き継ぐ_P029_取り直しに失敗しても起動を続けpaneは閉じない は、設計 3-4 の段3 を確かめる。
//
// 目的: 認証切れ・ネットワーク断・レートリミットで取り直せなくても起動は続ける。
// **引き継がないが、pane も閉じない。**
//
// **閉じない理由**（設計 3-83）。**ここでは Status がまだ読めていない。**
// カードが `tracker.direct_chat_state` だったかどうかを知る手立てが1つも無いので、
// **GitHub が一瞬落ちただけで、人間が pane で話している会話が消えることになる。**
// **閉じなくても取り残さない。**Status が読めた次の巡回で、3-9 の手順7b が
// 「印に入っていない worktree の pane」として扱う。
//
// 与える情報: ID 指定の取り直しが必ず失敗するトラッカー
// （`SetIDsError` は記録を取る側と取らない側の両方に効く。復元が呼ぶのは取らない側である）。
//
// 成功条件: Restore がエラーを返さず、**pane は1つも閉じられず**、worktree は残る。
func Test_再起動して実行中のissueを引き継ぐ_P029_取り直しに失敗しても起動を続けpaneは閉じない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})
	fx.Tracker.SetIDsError(errors.New("レートリミットに達しました"))

	_, hs := restore(t, fx)

	if want := []string{"Start", "ReplayPending", "StartDelivery"}; !equalStrings(hs.Calls(), want) {
		t.Fatalf("取り直しに失敗したのに起動を続けていない: got %v", hs.Calls())
	}
	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("取り直しに失敗しただけなのに pane を閉じた: %v", ids)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree を消してしまった: %v", err)
	}
}

// {"RUCM-PATH": "P041"}
//
// Test_再起動して実行中のissueを引き継ぐ_P041_身元ファイルの無いworktreeのpaneは閉じずにログへ残す は、段9 を確かめる。
//
// 目的: continuo のものと断定できないので、閉じずに人間へ見せる（設計 3-4 の段9）。
//
// 与える情報: 置き場所の中にあるが身元ファイルを持たない worktree と、その pane。
// **その issue はカンバンに載せない。**載せると復元（設計 3-49）が身元ファイルを
// 書き直してしまい、段9 へ入らない。**飛ばす設定にして、起動が止まらないようにする。**
//
// 成功条件: pane を閉じず、ログに残る。
func Test_再起動して実行中のissueを引き継ぐ_P041_身元ファイルの無いworktreeのpaneは閉じずにログへ残す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Workspace.OnBrokenWorktree = config.OnBrokenWorktreeSkip
	}})
	fx.AllowLog(
		"復元のために引いた issue がカンバンにありません",
		"手掛かりから issue を確かめられないので復元できません",
		"身元を確かめられない worktree があります",
		"次にこれをしてください",
		"workspace.on_broken_worktree が skip なので",
	)
	issue := sampleIssue(188, "In Progress")
	wt := prepareWorktree(t, fx, issue, identityOverride{SkipIdentity: true})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})

	restore(t, fx)

	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("continuo のものと断定できない pane を閉じてしまった: %v", ids)
	}
	if !strings.Contains(fx.Logs.String(), "身元ファイルの無い worktree に pane がありました") {
		t.Fatalf("人間へ見せるログが出ていない: %s", fx.Logs.String())
	}
}

// {"RUCM-PATH": "P041"}
//
// Test_再起動して実行中のissueを引き継ぐ_P041_壊れた身元ファイルは無視してログに出す は、設計 3-4 の段2 を確かめる。
//
// 目的: 段6 の書き込み途中で落ちた場合に起こる。**消してはならない。**
//
// 与える情報: JSON が壊れた身元ファイルを持つ worktree。
// **その issue はカンバンに載せない。**載せると復元（設計 3-49）が身元ファイルを
// 書き直してしまう。**飛ばす設定にして、起動が止まらないようにする。**
//
// 成功条件: Restore が落ちず、worktree が残り、ログに出る。
func Test_再起動して実行中のissueを引き継ぐ_P041_壊れた身元ファイルは無視してログに出す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Workspace.OnBrokenWorktree = config.OnBrokenWorktreeSkip
	}})
	fx.AllowLog(
		"復元のために引いた issue がカンバンにありません",
		"手掛かりから issue を確かめられないので復元できません",
		"身元を確かめられない worktree があります",
		"次にこれをしてください",
		"workspace.on_broken_worktree が skip なので",
	)
	issue := sampleIssue(188, "In Progress")
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	if err := os.WriteFile(
		filepath.Join(wt.Path, fx.Config.Workspace.IdentityFile), []byte("{壊れている"), 0o600); err != nil {
		t.Fatalf("壊れた身元ファイルを書けません: %v", err)
	}
	installPanes(fx)

	restore(t, fx)

	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("壊れた身元ファイルの worktree を消してしまった: %v", err)
	}
	if !strings.Contains(fx.Logs.String(), "身元ファイルを読めない worktree を飛ばしました") {
		t.Fatalf("壊れた身元ファイルをログに出していない: %s", fx.Logs.String())
	}
}

// {"RUCM-PATH": "P003"}
//
// Test_再起動して実行中のissueを引き継ぐ_P003_逃がし先に溜まったhookは索引ができてから配送される は、
// 段5d / 5e / 6 / 6b の順番を、**本物の hookserver を通して**確かめる。
//
// 目的: 設計 3-4。段6 で索引ができる前に配送を始めると、引き継いだ run の hook まで
// 「知らない session_id」として捨てられる。
//
// 与える情報:
//   - 引き継げる run（セッション UUID は `sess-188`）
//   - 逃がし先に置かれた `Stop` が2件。1件は `sess-188`、もう1件は知らない session_id
//
// 成功条件: 知らない session_id のほうだけが「捨てました」とログに出る。
// **引き継いだ run のほうは捨てられない。**
func Test_再起動して実行中のissueを引き継ぐ_P003_逃がし先に溜まったhookは索引ができてから配送される(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{SessionUUID: "sess-188"})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})

	pendingDir := filepath.Join(
		fx.RuntimeDir, hookserver.IssuesDirName,
		orchestrator.IssueSlug(issue.Identifier), hookserver.PendingDirName)
	if err := os.MkdirAll(pendingDir, 0o700); err != nil {
		t.Fatalf("逃がし先を作れません: %v", err)
	}
	writePendingStop(t, pendingDir, "1787057953362306", "sess-188")
	writePendingStop(t, pendingDir, "1787057953362307", "sess-unknown")

	logs := &syncLog{}
	hs, err := hookserver.New(hookserver.Options{
		SocketPath: fx.SocketPath,
		Sink:       fx.Orc,
		Logger:     slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatalf("hookserver.New に失敗した: %v", err)
	}
	t.Cleanup(func() { _ = hs.Close() })

	if _, err := fx.Orc.Restore(context.Background(), hs); err != nil {
		t.Fatalf("Restore に失敗した: %v", err)
	}

	waitFor(t, 5*time.Second, "逃がし先の hook が配送される", func() bool {
		return strings.Contains(logs.String(), "sess-unknown")
	})
	if strings.Contains(logs.String(), "sess-188") {
		t.Fatalf("引き継いだ run の hook を捨てた（段6 の索引より先に配送している）:\n%s", logs.String())
	}
}

// {"RUCM-PATH": "P021"}
//
// Test_再起動して実行中のissueを引き継ぐ_P021_In_Reviewで残したpaneを直後の巡回が閉じない は、
// 復元と巡回の手順7b（設計 3-9）が食い違っていないことを確かめる。
//
// 目的: 復元は `In Review` の run を「pane も worktree も残す」と決めて印に入れない
// （設計 3-4 の段5a）。手順7b が `active_states` の条件なしに pane を閉じると、
// **復元の直後の巡回が、人間のレビュー待ちで正常に止まっている Claude Code を
// 毎巡回で落とす。**
//
// 与える情報: Status が `In Review` の run と、その worktree を cwd に持つ生きた pane。
// 復元のあとに巡回を2回回す。
//
// 成功条件: `pane.close` が1回も呼ばれず、worktree も残る。
func Test_再起動して実行中のissueを引き継ぐ_P021_In_Reviewで残したpaneを直後の巡回が閉じない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Review")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	// **手順7b は workspace_id を指定して pane を引く**ので、その workspace の pane にする。
	paneID := wt.WorkspaceID + ":p1"
	installPanes(fx, livePane{
		PaneID: paneID, Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})

	restore(t, fx)
	fx.Orc.Tick(context.Background())
	fx.Orc.Tick(context.Background())

	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("引き渡し状態の pane を巡回が閉じてしまった: %v", ids)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree を消してしまった: %v", err)
	}
}

// {"RUCM-PATH": "P003"}
//
// Test_再起動して実行中のissueを引き継ぐ_P003_同じworktreeにpaneが2つあるとき1つだけ引き継ぎ残りを閉じる は、
// 設計 3-4 の段4 の「2つ目を残さない」を確かめる。
//
// 目的: 段2 の「同じ issue の worktree が2つ」の対称形である。
// **写像に後勝ちで入れると、上書きされた pane は引き継がれも閉じられも記録もされず、
// 同じ worktree に Claude Code が2つ居る状態がそのまま残る。**
//
// 与える情報: `In Progress` の run 1件と、その worktree を cwd に持つ pane が2つ
// （`p-500a` と `p-500b`。どちらも agent 名とセッション UUID を持つ）。
//
// 成功条件: 引き継ぐのは pane の ID が小さいほう1つだけで、もう1つは閉じられ、
// 復元の記録にも載る。
func Test_再起動して実行中のissueを引き継ぐ_P003_同じworktreeにpaneが2つあるとき1つだけ引き継ぎ残りを閉じる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(500, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx,
		livePane{PaneID: "p-500b", Cwd: wt.Path, AgentName: "continuo-hello-world-500-dup",
			AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-500b"},
		livePane{PaneID: "p-500a", Cwd: wt.Path, AgentName: "continuo-hello-world-500",
			AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-500a"},
	)

	result, _ := restore(t, fx)

	if got := fx.Orc.RunningIdentifiers(); len(got) != 1 {
		t.Fatalf("引き継いだ run が1件でない: got %v", got)
	}
	if !equalStrings(closedPaneIDs(fx), []string{"p-500b"}) {
		t.Fatalf("2つ目の pane を1つだけ閉じていない: %v", closedPaneIDs(fx))
	}
	if !equalStrings(result.ClosedPanes, []string{"p-500b"}) {
		t.Fatalf("閉じた pane が復元の記録に載っていない: %v", result.ClosedPanes)
	}
	if !strings.Contains(fx.Logs.String(), "pane_id=p-500a") {
		t.Fatalf("pane の ID が小さいほうを引き継いでいない: %s", fx.Logs.String())
	}
}

// {"RUCM-PATH": "P033"}
//
// Test_再起動して実行中のissueを引き継ぐ_P033_agentの一覧を取れなくてもpaneを1つも閉じない は、
// `agent.list` の失敗の扱いが `pane.list` の失敗と対称であることを確かめる。
//
// 目的: agent 名を引けないまま段8b へ流すと、**引き継げたはずの run の pane が
// 全件閉じられる。**herdr の一時的な失敗1回で走っている全部の作業を捨てないため、
// `pane.list` の失敗と同じく「引き継ぎを諦めて次の巡回に委ねる」に倒す。
//
// 与える情報: `In Progress` の run と生きた pane。`agent.list` はエラーを返す。
//
// 成功条件: pane を1つも閉じず、Status も worktree もそのまま。起動は続く。
func Test_再起動して実行中のissueを引き継ぐ_P033_agentの一覧を取れなくてもpaneを1つも閉じない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})
	fx.Herdr.Handle(herdr.MethodAgentList, func(map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "internal_error", Message: "herdr が一時的に落ちています"}
	})
	fx.AllowLog("agent の一覧を取れないので", "判断を保留します")

	_, hs := restore(t, fx)

	if want := []string{"Start", "ReplayPending", "StartDelivery"}; !equalStrings(hs.Calls(), want) {
		t.Fatalf("agent の一覧を取れないのに起動を続けていない: got %v", hs.Calls())
	}
	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("agent の一覧を取れないだけで pane を閉じてしまった: %v", ids)
	}
	if got := fx.Tracker.StateOf(issue.ID); got != "In Progress" {
		t.Fatalf("Status を動かしてしまった: got %q", got)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree を消してしまった: %v", err)
	}
}

// {"RUCM-PATH": "P003"}
//
// Test_再起動して実行中のissueを引き継ぐ_P003_身元ファイルが無くても置き場所とカンバンから復元する は、設計 3-49 を確かめる。
//
// 目的: 着手は worktree を作ってから身元ファイルを書く（設計 3-16 の段6〜段9）ので、
// **その間で落ちると身元ファイルの無い worktree ができる。**それは「壊れた」のではなく
// 「書き終える前に落ちた」だけであり、置き場所とカンバンから組み立て直せる。
//
// 与える情報: 身元ファイルを持たない worktree と、その pane と、カンバンに載っている issue。
//
// 成功条件: 身元ファイルが書き直され、その run が引き継がれること。
func Test_再起動して実行中のissueを引き継ぐ_P003_身元ファイルが無くても置き場所とカンバンから復元する(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{SkipIdentity: true})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})

	result, _ := restore(t, fx)

	identity, err := fx.Workspace.ReadIdentity(wt.Path)
	if err != nil {
		t.Fatalf("身元ファイルを復元していない: %v", err)
	}
	if identity.ProjectItemID != issue.ID {
		t.Fatalf("project item の ID が違う: got %q, want %q", identity.ProjectItemID, issue.ID)
	}
	if identity.IssueIdentifier != issue.Identifier {
		t.Fatalf("識別子が違う: got %q, want %q", identity.IssueIdentifier, issue.Identifier)
	}
	if identity.Branch != wt.Branch {
		t.Fatalf("branch 名が違う: got %q, want %q", identity.Branch, wt.Branch)
	}
	if identity.AgentName != "continuo-hello-world-188" {
		t.Fatalf("pane から agent 名を拾っていない: got %q", identity.AgentName)
	}
	if identity.SessionUUID != "sess-188" {
		t.Fatalf("pane からセッション UUID を拾っていない: got %q", identity.SessionUUID)
	}
	if len(result.Adopted) != 1 || result.Adopted[0] != issue.Identifier {
		t.Fatalf("復元した run を引き継いでいない: %+v", result.Adopted)
	}
}

// {"RUCM-PATH": "P043"}
//
// Test_再起動して実行中のissueを引き継ぐ_P043_復元できない壊れたworktreeがあれば起動を止める は、設計 3-49 を確かめる。
//
// 目的: 飛ばして走り続けると、その issue はカンバンの上で running_state のまま誰にも
// 触られず、**人間が気づくのは何時間も後になる。**既定は止める側である。
//
// 与える情報: JSON が壊れた身元ファイルを持つ worktree と、**カンバンに載っていない issue。**
//
// 成功条件: Restore がエラーを返し、**worktree は消えず**、エラーに「何が起きているか」と
// 「次に何をすべきか」の両方が入っていること。
func Test_再起動して実行中のissueを引き継ぐ_P043_復元できない壊れたworktreeがあれば起動を止める(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.AllowLog(
		"復元のために引いた issue がカンバンにありません",
		"手掛かりから issue を確かめられないので復元できません",
		"身元を確かめられない worktree があります",
		"次にこれをしてください",
	)
	issue := sampleIssue(188, "In Progress")
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	if err := os.WriteFile(
		filepath.Join(wt.Path, fx.Config.Workspace.IdentityFile), []byte("{壊れている"), 0o600); err != nil {
		t.Fatalf("壊れた身元ファイルを書けません: %v", err)
	}
	installPanes(fx)

	_, err := fx.Orc.Restore(context.Background(), &fakeHookServer{})
	if err == nil {
		t.Fatal("復元できない壊れた worktree があるのに起動を止めていない")
	}
	if !strings.Contains(err.Error(), wt.Path) {
		t.Errorf("どの worktree が壊れているか分からない: %v", err)
	}
	if !strings.Contains(err.Error(), "continuo abandon --force") {
		t.Errorf("次に何をすべきかが書かれていない: %v", err)
	}
	if _, statErr := os.Stat(wt.Path); statErr != nil {
		t.Fatalf("壊れた worktree を消してしまった: %v", statErr)
	}
}

// {"RUCM-PATH": "P041"}
//
// Test_再起動して実行中のissueを引き継ぐ_P041_paneのlabelが置き場所と食い違えば復元しない は、設計 3-49 の裏取りを確かめる。
//
// 目的: pane の label は herdr の CLI から誰でも書き換えられる。**裏を取らずに使うと、
// label を書き換えるだけで別の issue の worktree として復元させられる。**
// 引き直した issue からスラグを作り直し、目の前のディレクトリ名と一致することを確かめる。
//
// 与える情報: issue 188 の置き場所に立つ、身元ファイルの無い worktree。
// その pane の label は issue 999 を指し、**カンバンには 999 だけが載っている。**
//
// 成功条件: 身元ファイルを書かないこと（別の issue のものとして復元しない）。
func Test_再起動して実行中のissueを引き継ぐ_P041_paneのlabelが置き場所と食い違えば復元しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Workspace.OnBrokenWorktree = config.OnBrokenWorktreeSkip
	}})
	fx.AllowLog(
		"復元のために引いた issue がカンバンにありません",
		"引き直した issue のスラグが置き場所のディレクトリ名と違うので復元しません",
		"手掛かりから issue を確かめられないので復元できません",
		"身元を確かめられない worktree があります",
		"次にこれをしてください",
		"workspace.on_broken_worktree が skip なので",
		"身元ファイルの無い worktree に pane がありました",
	)
	victim := sampleIssue(188, "In Progress")
	attacker := sampleIssue(999, "In Progress")
	fx.Tracker.AddIssue(attacker)
	wt := prepareWorktree(t, fx, victim, identityOverride{SkipIdentity: true})
	installPanes(fx, livePane{
		PaneID: "p-999", Cwd: wt.Path, AgentName: "continuo-hello-world-999",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-999",
		Label: "octocat/hello-world/issues/999",
	})

	restore(t, fx)

	if identity, err := fx.Workspace.ReadIdentity(wt.Path); err == nil {
		t.Fatalf("別の issue のものとして復元してしまった: %+v", identity)
	}
}

// {"RUCM-PATH": "P037"}
//
// Test_再起動して実行中のissueを引き継ぐ_P037_置き場所と食い違う身元ファイルを鍵にしない は、
// 復元の段2 が `project_item_id` を検算することを確かめる。
//
// 目的: `project_item_id` はエージェントが書き換えられる（身元ファイルは worktree の直下にある）。
// 検算しないと、**書き換えた側の worktree が別 issue の run として印に入り、
// 被害者の worktree は『捨てた身元』として pane を閉じられる。**
//
// 与える情報: `octocat/hello-world` の下にある worktree の身元ファイルが、
// 別のリポジトリ（`octocat/other-repo`）の issue を名乗っている。
// 成功条件: その worktree を引き継がず、pane を1つも閉じず、worktree も消さないこと。
func Test_再起動して実行中のissueを引き継ぐ_P037_置き場所と食い違う身元ファイルを鍵にしない(t *testing.T) {
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

// {"RUCM-PATH": "P033"}
//
// Test_再起動して実行中のissueを引き継ぐ_P033_paneの一覧を取れないだけでStatusを人間へ渡さない は、
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
func Test_再起動して実行中のissueを引き継ぐ_P033_paneの一覧を取れないだけでStatusを人間へ渡さない(t *testing.T) {
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

// {"RUCM-PATH": "P017"}
//
// TestDirectChat_再起動でdirect_chatのrunを引き取り指示を送らない は、設計 3-4 の段5a と 3-83j の1行目を確かめる。
//
// 目的: herdr が再起動前のセッションを resume したとき、復元は direct chat の run を引き取る（印にも入れる）。
// **引き取ったあとも、指示は1文字も送らない。**
// 与える情報: Status が direct chat で、pane が生きていて agent 名を持つ run。
// 成功条件: 印に入り、pane を閉じず、そのあとの巡回でも指示を送らないこと。
func Test_再起動して実行中のissueを引き継ぐ_P017_再起動でdirectChatのrunを引き取り指示を送らない(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	issue := sampleIssue(330, humanState)
	fx.Tracker.AddIssue(issue)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-330", Cwd: wt.Path, AgentName: "continuo-hello-world-330",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-330",
	})

	restore(t, fx)
	if got := fx.Orc.RunningIdentifiers(); len(got) != 1 {
		t.Fatalf("direct chat の run を引き取っていない: %v", got)
	}
	for range 2 {
		fx.Orc.Tick(context.Background())
	}
	// **送らないことを確かめるので、待ち合わせる目印が無い。**turn ループが起きうるだけの間を置く。
	time.Sleep(200 * time.Millisecond)

	if n := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); n != 0 {
		t.Fatalf("引き取った direct chat の run へ指示を送った: %d 回", n)
	}
	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("引き取った direct chat の run の pane を閉じた: %v", ids)
	}
}

// {"RUCM-PATH": "P017"}
//
// TestDirectChat_再起動でdirect_chatのworktreeの2枚目のpaneを閉じずagent名を持つpaneを引き継ぐ は、
// 設計 3-83j の「再起動で、direct chat の worktree の2枚目の pane を閉じない」を確かめる。
//
// 目的: direct chat の最中に人間がテスト用のシェルを分けていると、再起動でどちらかが消える。
// **Status が direct chat の worktree では段4 で閉じない。引き継ぎの相手には、agent 名を持つ pane を選ぶ**
// （pane ID の小さいほうではない）。
// 与える情報: Status が direct chat の worktree に pane が2枚。ID の小さいほうは agent 名の無いシェル。
// 成功条件: どちらも閉じず、agent 名を持つ pane を引き継ぐこと。
func Test_再起動して実行中のissueを引き継ぐ_P017_再起動でdirectChatのworktreeの2枚目のpaneを閉じずagent名を持つpaneを引き継ぐ(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	fx.AllowLog("同じ worktree に pane が2つあります")
	issue := sampleIssue(333, humanState)
	fx.Tracker.AddIssue(issue)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx,
		livePane{PaneID: "p-333a", Cwd: wt.Path, NoAgentKind: true, AgentStatus: herdr.AgentStatusIdle},
		livePane{PaneID: "p-333b", Cwd: wt.Path, AgentName: "continuo-hello-world-333",
			AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-333"},
	)

	restore(t, fx)

	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("direct chat の worktree の2枚目の pane を閉じた: %v", ids)
	}
	if got := fx.Orc.RunningIdentifiers(); len(got) != 1 {
		t.Fatalf("agent 名を持つ pane を引き継いでいない: %v", got)
	}
	if !strings.Contains(fx.Logs.String(), "pane_id=p-333b") {
		t.Fatalf("agent 名を持つ pane を引き継ぎの相手に選んでいない: %s", fx.Logs.String())
	}
}

// {"RUCM-PATH": "P019"}
//
// Test_再起動して実行中のissueを引き継ぐ_P019_再起動で確認の画面ならpaneを閉じず通知も書かず戻したときに閉じる集合が閉じる は、
// 設計 3-4 の段3 の例外(2)・3-83f の表の最後の2行を確かめる。
//
// 目的: 確認の画面（`blocked`）で止まっている direct chat の run は、**`failure_state` へ落とさず、pane も閉じず、
// 引き渡しの通知も投稿しない**（通知の本文は「continuo が pane を閉じたので画面は残っていません」と書いており、
// 閉じないのに投稿すると嘘になる）。見送った pane は閉じる集合が扱い、**作業中の Status へ戻した巡回で閉じる。**
// 与える情報: Status が direct chat で、agent_status が blocked の pane を持つ run。
// 成功条件: 復元で pane を閉じず、コメントも Status の書き込みも無い。作業中へ戻した巡回で、その pane を閉じる。
func Test_再起動して実行中のissueを引き継ぐ_P019_再起動で確認の画面ならpaneを閉じず通知も書かず戻したときに閉じる集合が閉じる(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	holdPrompt(fx)
	// **閉じたあとの同じ巡回の着手は、この台本の pane の一覧（workspace を名乗らない）では pane を引けずに落ちる。**
	// この検査が見るのは「閉じたか」だけなので、その失敗は許す。
	fx.AllowLog("権限の確認で止まっている", "direct chat のカードなので pane は閉じません", "印に入っていない worktree に生きた pane",
		"着手に失敗しました")
	issue := sampleIssue(331, humanState)
	fx.Tracker.AddIssue(issue)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-331", Cwd: wt.Path, AgentName: "continuo-hello-world-331",
		AgentStatus: herdr.AgentStatusBlocked, SessionUUID: "sess-331",
	})

	restore(t, fx)
	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("direct chat のカードの pane を復元で閉じた: %v", ids)
	}
	if n := len(fx.Tracker.CommentsOf(nodeIDOfIssue(issue))); n != 0 {
		t.Fatalf("pane を閉じていないのに引き渡しの通知を投稿した: %d 件", n)
	}
	if got := fx.Tracker.StateOf(issue.ID); got != humanState {
		t.Fatalf("direct chat のカードを動かした: %q", got)
	}

	// 人間が作業中の Status へ戻した。
	fx.Tracker.SetState(issue.ID, "In Progress")
	fx.Orc.Tick(context.Background())
	if ids := closedPaneIDs(fx); indexOf(ids, "p-331") < 0 {
		t.Fatalf("作業中へ戻したのに、見送った pane を閉じていない: %v", ids)
	}
}

// {"RUCM-PATH": "P029"}
//
// Test_再起動して実行中のissueを引き継ぐ_P029_取り直しに失敗した復元のagent名の無いpaneを戻したときに閉じる は、
// 設計 3-4 の段3 の例外(1)・3-9 の手順7b・3-83f の閉じる集合を確かめる。
//
// 目的: 取り直しに失敗した run は Status が読めないので、**閉じずに閉じる集合へ入れる。**
// Status が作業中へ戻った巡回で、**agent 名の無い pane も閉じる。**入れないと、取り残しの処理は
// agent 名の無い pane を飛ばすので、着手がその pane へ `agent.start` を送る。
// 与える情報: 復元の取り直しが失敗し、agent 名を持たない pane（シェルに戻った pane）が残っている worktree。
// 成功条件: 復元では閉じず、取り直せた巡回で Status が作業中なら、その pane を閉じること。
func Test_再起動して実行中のissueを引き継ぐ_P029_取り直しに失敗した復元のagent名の無いpaneを戻したときに閉じる(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	holdPrompt(fx)
	fx.AllowLog("取り直しに失敗", "印に入っていない worktree に生きた pane", "実行中の issue を取り直せません", "候補の取得に失敗",
		"着手に失敗しました")
	issue := sampleIssue(332, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-332", Cwd: wt.Path, NoAgentKind: true,
		AgentStatus: herdr.AgentStatusIdle,
	})
	fx.Tracker.SetIDsError(errString("レートリミットに達しました"))

	restore(t, fx)
	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("取り直しに失敗しただけで pane を閉じた: %v", ids)
	}

	fx.Tracker.SetIDsError(nil)
	fx.Orc.Tick(context.Background())
	if ids := closedPaneIDs(fx); indexOf(ids, "p-332") < 0 {
		t.Fatalf("作業中の Status なのに、閉じる集合の agent 名の無い pane を閉じていない: %v", ids)
	}
}

// {"RUCM-PATH": "P015"}
//
// 目的: 再起動のときに引き継がずに閉じた pane について、閉じた記録を書くこと、
// 担当者が他人のアカウントなら書かないことを固定する。
//
// **agent 名は見ない。**再起動のときは Claude Code が動いていたかが分からないので、書き漏らすより書くほうを取る。
// **担当者が他人なら書かない。**担当を外された機械は issue へ書かない（設計 3-77c・3-83h）。
//
// 与える情報: socket のパスが前回と違う `In Progress` の run の pane（引き継がずに閉じる）。
// 担当者なしと、担当者が別のアカウント1人の2通り。
// 成功条件: 担当者なしでは閉じた記録が1件あり、担当者が他人では1件も無いこと。
func Test_再起動して実行中のissueを引き継ぐ_P015_再起動で閉じたpaneは担当者が他人なら書かない(t *testing.T) {
	for _, other := range []bool{false, true} {
		name := "担当者なし"
		if other {
			name = "担当者が他人"
		}
		t.Run(name, func(t *testing.T) {
			fx := newFixture(t, fixtureOptions{})
			fx.AllowLog("hook を受ける socket のパスが前回と違うので引き継ぎません")
			issue := sampleIssue(310, "In Progress")
			fx.Tracker.AddIssue(issue)
			if other {
				fx.Tracker.SetAssignees(issue.ID, "someone-else")
			}
			wt := prepareWorktree(t, fx, issue, identityOverride{SocketPath: "/tmp/前回の場所/hooks.sock"})
			installPanes(fx, livePane{
				PaneID: "p-310", Cwd: wt.Path, AgentName: "continuo-hello-world-310",
				AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-310",
			})

			restore(t, fx)

			n := len(fx.Tracker.ClosedRecordsOf("I_node310"))
			if other && n != 0 {
				t.Fatalf("担当者が他人なのに閉じた記録を書いた: %d 件", n)
			}
			if !other && n != 1 {
				t.Fatalf("閉じた記録が1件ではない: %d 件", n)
			}
		})
	}
}

// {"RUCM-PATH": "P003"}
//
// Test_再起動して実行中のissueを引き継ぐ_P003_旧い形のlabelが付いたpaneでも引き継げる は、label の形を変えても
// 再起動後の引き継ぎが壊れないことを固定する（issue #12 の受け入れ条件）。
//
// 目的: **label は人間が herdr の画面で pane を見分けるための表示名であり、
// continuo は読み戻さない**（設計 3-3）ことを、テストで動かせない形にする。
// 引き継ぎの照合は pane の cwd と worktree のパスだけで行う。
//
// 与える情報:
//   - `In Progress` の issue が1件。その worktree と身元ファイルがディスクにある
//   - その worktree を cwd に持つ pane が生きていて、**label には issue の URL
//     （label の形を変える前の、continuo が以前書いていた文字列）が入っている**
//
// 成功条件: label の形が新しいものと違っていても、印（実行中の一覧）に入ること。
func Test_再起動して実行中のissueを引き継ぐ_P003_旧い形のlabelが付いたpaneでも引き継げる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{SessionUUID: "sess-188"})

	// **いま continuo が書く label とは違う形である。**
	oldLabel := *issue.URL
	if oldLabel == herdr.IssueLabel(issue.Owner, issue.Repo, issue.Number) {
		t.Fatalf("旧い形と新しい形が同じでは、この検査が何も確かめていない: %q", oldLabel)
	}
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188", Label: oldLabel,
	})

	result, _ := restore(t, fx)

	if got := fx.Orc.RunningIdentifiers(); len(got) != 1 || got[0] != issue.Identifier {
		t.Fatalf("旧い形の label が付いた pane を引き継げていない: got %v", got)
	}
	if len(result.Adopted) != 1 || result.Adopted[0] != issue.Identifier {
		t.Fatalf("復元の記録に引き継ぎが残っていない: got %v", result.Adopted)
	}
	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("引き継げるはずの pane を閉じてしまった: %v", ids)
	}
}

// {"RUCM-PATH": "P003"}
//
// Test_再起動して実行中のissueを引き継ぐ_P003_復元の取り直しは誰がStatusを書いたかを取らない は、設計 3-61 を確かめる。
//
// 目的: 復元の段3（`refetchByIdentities`）が見るのは、取り直した Status と識別子だけである。
// **記録は1つも読まない。**引き継いだ run の記録は、最初の巡回の実行中の照合が入れ直す。
//
// 与える情報: `In Progress` の issue の worktree と身元ファイルがディスクにあり、
// その worktree を cwd に持つ pane が生きている状態（引き継ぎの中心の経路）。
// 成功条件: 取り直しが1回だけ走り、それが記録を取らない側であること。
func Test_再起動して実行中のissueを引き継ぐ_P003_復元の取り直しは誰がStatusを書いたかを取らない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{SessionUUID: "sess-188"})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})

	restore(t, fx)

	if got, want := idRefreshCalls(fx), []string{withoutTimelineCall}; !equalStrings(got, want) {
		t.Fatalf("復元の取り直しが誰が Status を書いたかまで取っている: got %v, want %v", got, want)
	}
}
