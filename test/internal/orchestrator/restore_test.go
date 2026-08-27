// {"RUCM-CFG-SHA256": "72f9472fcdb4c37c29c829202b58a7f601161257172517df5464cc1a221c1996", "SOURCE": "docs/spec/usecases/particular_case/再起動して実行中の issue を引き継ぐ.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**「再起動して実行中の issue を引き継ぐ」の
// 11本のパスに、それぞれ対応するテストがある（既存のテストへマーカーを付けた）。
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

// TestRestore_生きているpaneを引き継いで印と実行中の一覧へ入れ直す は、
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
func TestRestore_生きているpaneを引き継いで印と実行中の一覧へ入れ直す(t *testing.T) {
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

// TestRestore_引き継いだrunにはstallの時計が引き継いだ時刻から始まる は、
// `runState.LastSeenAt` に引き継いだ時刻が入ることを確かめる。
//
// 目的: ゼロ値のままだと、引き継いだ直後の巡回で即座に stall と判定されて worker が
// 止められる（設計 3-4 の段5c）。
//
// 与える情報: 引き継げる run を1件と、`claude.turn_timeout_ms` が 60 秒の設定。
//
// 成功条件: 引き継いだ直後に巡回を1回回しても、pane が閉じられず印に残っている。
func TestRestore_引き継いだrunにはstallの時計が引き継いだ時刻から始まる(t *testing.T) {
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

// {"RUCM-PATH": "P003"}
//
// TestRestore_agent_statusがworkingならNeedsPromptを立てない は、
// 走っている turn に別の turn を投げないことを確かめる。
//
// 目的: 設計 3-4 の段5a2。走っている最中に投げると turn が混ざる。
//
// 与える情報: 引き継げる run を1件。agent_status は working。
//
// 成功条件: 引き継いだあと巡回を1回回しても `agent.prompt` を送らない。
func TestRestore_agent_statusがworkingならNeedsPromptを立てない(t *testing.T) {
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

// {"RUCM-PATH": "P006"}
//
// TestRestore_idleなら継続の指示を送る は、引き継いだ run へ送る本文を確かめる。
//
// 目的: 設計 3-4 の段5c。**送るのは継続の指示（5-4）であり、1回目の本文（5-3）ではない。**
// セッションは引き継いでいるので、エージェントは issue の URL も作法も既に知っている。
//
// 与える情報: 引き継げる run を1件（agent_status は idle）。
//
// 成功条件: 巡回の turn ループが送った本文が「続けてください」で始まり、
// 1回目のテンプレートの文言を含まない。
func TestRestore_idleなら継続の指示を送る(t *testing.T) {
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

// {"RUCM-PATH": "P010"}
//
// TestRestore_agent_statusがblockedなら引き継がずfailure_stateへ落としてpaneを閉じる は、
// 保留中の権限要求が承認されて実行されるのを防ぐ。
//
// 目的: 設計 3-4 の段5a2。**blocked のまま引き継いで turn を送ると、保留中の権限要求が
// 承認されて実行される**（3-11 で実測。3/3）。
//
// 与える情報: `In Progress` の issue と、agent_status が blocked の pane。
//
// 成功条件: 印に入らず、pane が閉じられ、Status が `failure_state`（Blocked）へ落ちる。
// worktree は残る。
func TestRestore_agent_statusがblockedなら引き継がずfailure_stateへ落としてpaneを閉じる(t *testing.T) {
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

// {"RUCM-PATH": "P011"}
//
// TestRestore_agent_statusが知らない値ならpaneを閉じてworktreeとStatusを残す は、
// 判断できない run を引き継がないことを確かめる。
//
// 目的: 設計 3-4 の段5a2 の「取れない / 知らない値」。
//
// 与える情報: agent_status が unknown の pane。
//
// 成功条件: 印に入らず、pane が閉じられ、Status は動かず、worktree は残る。
func TestRestore_agent_statusが知らない値ならpaneを閉じてworktreeとStatusを残す(t *testing.T) {
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

// TestRestore_agent名の無いpaneは閉じてworktreeとStatusを残す は、段8b を確かめる。
//
// 目的: `agent.prompt` / `agent.wait` の宛先は agent 名である。pane ID では送れないので、
// agent 名が引けない pane は引き継げない（設計 3-4 の段8b）。
//
// 与える情報: `agent.list` に載っていない pane。
//
// 成功条件: 印に入らず、pane が閉じられ、worktree と Status は残る。
func TestRestore_agent名の無いpaneは閉じてworktreeとStatusを残す(t *testing.T) {
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

// TestRestore_socketのパスが前回と違えば引き継がずpaneを閉じる は、設計 3-23 を確かめる。
//
// 目的: 探索順は環境に依存するので、別の起動方法で立て直すと socket が別のパスに落ちる。
// run 中の Claude Code は前回のパスを持ったままなので、hook をもう届けられない。
//
// 与える情報: 身元ファイルの `socket_path` が今回のパスと違う run。
//
// 成功条件: 引き継がず pane を閉じる。worktree と Status は残る。
// **両方のパスがログに出る**（運用の環境が変わったことに人間が気づけるようにする）。
func TestRestore_socketのパスが前回と違えば引き継がずpaneを閉じる(t *testing.T) {
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

// {"RUCM-PATH": "P009"}
//
// TestRestore_引き継いだ回数が上限ならturnを1回も送らずfailure_stateへ落とす は、
// 設計 3-4 の段5b を確かめる。
//
// 目的: 落ちるたびに turn 数が 1 に戻るので、引き継いだ回数で打ち切らないと
// 打ち切りが永久に発火しない。**判定は turn を送る前に行う。**
//
// 与える情報: `agent.max_takeover` が 2 で、身元ファイルの `takeover_count` が 2 の run。
//
// 成功条件: 印に入らず、`agent.prompt` を1回も送らず、pane を閉じ、
// Status が `failure_state` へ落ちる。worktree は残る。
func TestRestore_引き継いだ回数が上限ならturnを1回も送らずfailure_stateへ落とす(t *testing.T) {
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

// TestRestore_同じissueのworktreeが2つあるとき新しいほうを採り古いほうのpaneを段4で閉じる は、
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
func TestRestore_同じissueのworktreeが2つあるとき新しいほうを採り古いほうのpaneを段4で閉じる(t *testing.T) {
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

// {"RUCM-PATH": "P012"}
//
// TestRestore_In_Reviewのrunはpaneもworktreeも残して何もしない は、
// 設計 3-4 の段5a の「引き渡し」を確かめる。
//
// 目的: 再起動の直後は、その pane が「人間のレビュー待ちで正常に止まっているもの」なのか
// 「取り残されたもの」なのかを区別できない（8-1）。
//
// 与える情報: Status が `In Review` の run と、その pane。
//
// 成功条件: pane を閉じず、worktree を消さず、Status を巻き戻さず、印にも入れない。
func TestRestore_In_Reviewのrunはpaneもworktreeも残して何もしない(t *testing.T) {
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

// TestRestore_cleanup_on_statesならpaneを閉じて片付ける は、
// 片付けの条件が `cleanup.on_states` であることを確かめる。
//
// 目的: 設計 3-4 の段5a。**`terminal_states` ではない。**既定値はどちらも `["Done"]` だが
// 別のキーであり、取り違えると片付けが起きない／余計に起きる。
//
// 与える情報: `cleanup.on_states` に `Archived`、`terminal_states` に `Done` を入れた設定と、
// Status が `Archived` の run。**既定値のままだと取り違えを検出できないので別の値にする。**
//
// 成功条件: pane を閉じ、worktree が実際に消える。印には入れない。
func TestRestore_cleanup_on_statesならpaneを閉じて片付ける(t *testing.T) {
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

// TestRestore_Doneでもcleanup_on_statesに入っていなければ片付けない は、
// 上のテストの裏返しである。
//
// 目的: `terminal_states` を見て片付けてしまう取り違えを検出する。
//
// 与える情報: `cleanup.on_states` が `Archived` だけの設定と、Status が `Done` の run。
//
// 成功条件: worktree が残る（`Done` は `terminal_states` だが `cleanup.on_states` ではない）。
func TestRestore_Doneでもcleanup_on_statesに入っていなければ片付けない(t *testing.T) {
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

// TestRestore_取り直しで見つからないrunはpaneもworktreeも残して印から外す は、
// 設計 3-4 の段5a の「取り直しで見つからなかった」を確かめる。
//
// 目的: ボードから外された・archive された issue を勝手に消さない。
//
// 与える情報: 身元ファイルはあるが、ボードに載っていない issue。
//
// 成功条件: pane も worktree も残り、印に入らず、ログに残る。
func TestRestore_取り直しで見つからないrunはpaneもworktreeも残して印から外す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	// **ボードには足さない。**
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})

	restore(t, fx)

	if got := fx.Orc.RunningIdentifiers(); len(got) != 0 {
		t.Fatalf("ボードに無い run を印へ入れてしまった: got %v", got)
	}
	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("ボードに無い run の pane を閉じてしまった: %v", ids)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree を消してしまった: %v", err)
	}
	if !strings.Contains(fx.Logs.String(), "取り直しで見つからなかった") {
		t.Fatalf("見つからなかったことをログに出していない: %s", fx.Logs.String())
	}
}

// {"RUCM-PATH": "P016"}
//
// TestRestore_取り直しに失敗しても起動を続けpaneを閉じる は、設計 3-4 の段3 を確かめる。
//
// 目的: 認証切れ・ネットワーク断・レートリミットで取り直せなくても起動は続ける。
// ただし引き継げないので pane は閉じる（残すと次の巡回で2つ目が立つ）。
//
// 与える情報: ID 指定の取り直しが必ず失敗するトラッカー
// （`SetIDsError` は記録を取る側と取らない側の両方に効く。復元が呼ぶのは取らない側である）。
//
// 成功条件: Restore がエラーを返さず、pane が閉じられ、worktree は残る。
func TestRestore_取り直しに失敗しても起動を続けpaneを閉じる(t *testing.T) {
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
	if ids := closedPaneIDs(fx); indexOf(ids, "p-188") < 0 {
		t.Fatalf("引き継げなかった run の pane を閉じていない: %v", ids)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree を消してしまった: %v", err)
	}
}

// {"RUCM-PATH": "P013"}
//
// TestRestore_paneが無い実行中のrunは既定では次の巡回に委ねる は、
// `restart.orphan_running_action` の既定（`redispatch`）を確かめる。
//
// 目的: 設計 3-4。**復元の中で dispatch すると、着手の段11 の待ちで最大1時間止まる。**
//
// 与える情報: 身元ファイルはあるが pane が無い、`In Progress` の run。
//
// 成功条件: 印にも実行中の一覧にも入らず、Status も動かない。worktree は残る。
func TestRestore_paneが無い実行中のrunは既定では次の巡回に委ねる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx) // pane は1つも無い

	restore(t, fx)

	if got := fx.Orc.RunningIdentifiers(); len(got) != 0 {
		t.Fatalf("復元の中で dispatch してしまった: got %v", got)
	}
	if got := fx.Tracker.StateOf(issue.ID); got != "In Progress" {
		t.Fatalf("Status を動かしてしまった: got %q", got)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree を消してしまった: %v", err)
	}
}

// TestRestore_paneが無い実行中のrunをdispatch_stateへ戻せる は、
// `restart.orphan_running_action: to_dispatch_state` を確かめる。
//
// 目的: 設計 3-4 の3値の分岐。
//
// 与える情報: `to_dispatch_state` の設定と、pane の無い `In Progress` の run。
//
// 成功条件: Status が `dispatch_state`（Ready）へ戻る。
func TestRestore_paneが無い実行中のrunをdispatch_stateへ戻せる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Restart.OrphanRunningAction = "to_dispatch_state"
	}})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx)

	restore(t, fx)

	if got := fx.Tracker.StateOf(issue.ID); got != "Ready" {
		t.Fatalf("dispatch_state へ戻していない: got %q, want %q", got, "Ready")
	}
}

// TestRestore_paneが無い実行中のrunをfailure_stateへ落とせる は、
// `restart.orphan_running_action: to_failure_state` を確かめる。
//
// 目的: 設計 3-4 の3値の分岐。
//
// 与える情報: `to_failure_state` の設定と、pane の無い `In Progress` の run。
//
// 成功条件: Status が `failure_state`（Blocked）へ落ち、worktree は残る。
func TestRestore_paneが無い実行中のrunをfailure_stateへ落とせる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Restart.OrphanRunningAction = "to_failure_state"
	}})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx)

	restore(t, fx)

	if got := fx.Tracker.StateOf(issue.ID); got != "Blocked" {
		t.Fatalf("failure_state へ落としていない: got %q", got)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree を消してしまった: %v", err)
	}
}

// TestRestore_paneが無くIn_Reviewなら何もしない は、段8 の表の「それ以外」を確かめる。
//
// 目的: 設計 3-4。**Status を巻き戻してはならない。**`restart.orphan_running_action` も見ない。
//
// 与える情報: `to_dispatch_state` の設定と、pane の無い `In Review` の run。
//
// 成功条件: Status が `In Review` のまま動かない。
func TestRestore_paneが無くIn_Reviewなら何もしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Restart.OrphanRunningAction = "to_dispatch_state"
	}})
	issue := sampleIssue(188, "In Review")
	fx.Tracker.AddIssue(issue)
	prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx)

	restore(t, fx)

	if got := fx.Tracker.StateOf(issue.ID); got != "In Review" {
		t.Fatalf("引き渡し状態の Status を動かしてしまった: got %q", got)
	}
}

// TestRestore_身元ファイルの無いworktreeのpaneは閉じずにログへ残す は、段9 を確かめる。
//
// 目的: continuo のものと断定できないので、閉じずに人間へ見せる（設計 3-4 の段9）。
//
// 与える情報: 置き場所の中にあるが身元ファイルを持たない worktree と、その pane。
// **その issue はボードに載せない。**載せると復元（設計 3-49）が身元ファイルを
// 書き直してしまい、段9 へ入らない。**飛ばす設定にして、起動が止まらないようにする。**
//
// 成功条件: pane を閉じず、ログに残る。
func TestRestore_身元ファイルの無いworktreeのpaneは閉じずにログへ残す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Workspace.OnBrokenWorktree = config.OnBrokenWorktreeSkip
	}})
	fx.AllowLog(
		"復元のために引いた issue がボードにありません",
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

// TestRestore_壊れた身元ファイルは無視してログに出す は、設計 3-4 の段2 を確かめる。
//
// 目的: 段6 の書き込み途中で落ちた場合に起こる。**消してはならない。**
//
// 与える情報: JSON が壊れた身元ファイルを持つ worktree。
// **その issue はボードに載せない。**載せると復元（設計 3-49）が身元ファイルを
// 書き直してしまう。**飛ばす設定にして、起動が止まらないようにする。**
//
// 成功条件: Restore が落ちず、worktree が残り、ログに出る。
func TestRestore_壊れた身元ファイルは無視してログに出す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Workspace.OnBrokenWorktree = config.OnBrokenWorktreeSkip
	}})
	fx.AllowLog(
		"復元のために引いた issue がボードにありません",
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

// {"RUCM-PATH": "P004"}
//
// TestSweepOnStartup_復元のあとに走り引き継いだbranchを消さない は、
// 起動時の掃除の順番と対象を確かめる。
//
// 目的: 設計 3-9 の手順6b。**先に走らせると、これから引き継ぐ run の branch を
// 孤児と判定して消す。**「実行中の issue も無い」の判定には復元後の印の集合を使う。
//
// 与える情報:
//   - 引き継げる run が1件（その branch は worktree がある）
//   - worktree を持たない孤児 branch `continuo/orphan/1` が1本
//   - 接頭辞に一致しない branch `feature/keep` が1本
//
// 成功条件: 孤児 branch だけが消え、引き継いだ run の branch と接頭辞に一致しない
// branch は残る。
func TestSweepOnStartup_復元のあとに走り引き継いだbranchを消さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})
	runGit(t, fx.Repo.Dir, "branch", "continuo/orphan/1")
	runGit(t, fx.Repo.Dir, "branch", "feature/keep")

	result, _ := restore(t, fx)
	fx.Orc.SweepOnStartup(context.Background(), result)

	branches := runGit(t, fx.Repo.Dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if strings.Contains(branches, "continuo/orphan/1") {
		t.Fatalf("孤児 branch を消していない: %s", branches)
	}
	if !strings.Contains(branches, wt.Branch) {
		t.Fatalf("引き継いだ run の branch を消してしまった: %s", branches)
	}
	if !strings.Contains(branches, "feature/keep") {
		t.Fatalf("接頭辞に一致しない branch を消してしまった: %s", branches)
	}
}

// {"RUCM-PATH": "P005"}
//
// TestSweepOnStartup_deleteBranchが偽なら孤児branchを1本も消さない は、
// **起動時の掃除が `cleanup.delete_branch` を見る**ことを確かめる。
//
// 目的: 片付け（設計 3-9 の段4）はこの設定を見て branch を残し、「branch は残しました」と
// 人間へ言う。**その branch は掃除の3条件を全部満たす**（接頭辞に一致し、どの worktree も
// チェックアウトしておらず、実行中の run も無い）。**設定を見ない掃除は、次に continuo を
// 起動しただけでその branch を強制削除で消す。**`continuo abandon --force` で片付けた
// worktree の branch には未 push の commit が載っていることがあり、消えれば reflog を
// 掘る以外に戻す手立ては無い。
//
// 与える情報: `cleanup.delete_branch` を偽にした設定と、worktree を持たない孤児 branch
// `continuo/orphan/1`。**掃除そのものは有効のまま**（`cleanup.enabled` と
// `cleanup.sweep_on_startup` は真）なので、掃除の入口までは同じように入る。
// 成功条件: 孤児 branch が残っていること。
func TestSweepOnStartup_deleteBranchが偽なら孤児branchを1本も消さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Cleanup.DeleteBranch = false
	}})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})
	runGit(t, fx.Repo.Dir, "branch", "continuo/orphan/1")

	result, _ := restore(t, fx)
	fx.Orc.SweepOnStartup(context.Background(), result)

	branches := runGit(t, fx.Repo.Dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if !strings.Contains(branches, "continuo/orphan/1") {
		t.Fatalf("cleanup.delete_branch が false なのに孤児 branch を消した: %s", branches)
	}
	if !strings.Contains(branches, wt.Branch) {
		t.Fatalf("引き継いだ run の branch を消してしまった: %s", branches)
	}
}

// TestSweepOnStartup_cleanup_on_statesのissueのworktreeを片付ける は、
// 設計 3-9 の手順6 を確かめる。
//
// 目的: 起動時に、終わっている issue の worktree を片付ける。
//
// 与える情報: `cleanup.on_states` が `Archived` の設定と、Status が `Archived` の
// 身元ファイル付き worktree（pane は無い）。
//
// 成功条件: worktree が消える。
//
// **段8 でも同じ worktree が片付く経路があるため、このテストが確かめているのは
// 「起動時の掃除が復元のあとに走っても壊れないこと」でもある。**
func TestSweepOnStartup_cleanup_on_statesのissueのworktreeを片付ける(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Cleanup.OnStates = []string{"Archived"}
		cfg.Tracker.TerminalStates = []string{"Done"}
		cfg.Cleanup.RequirePushed = false
	}})
	issue := sampleIssue(188, "Archived")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx)

	result, _ := restore(t, fx)
	fx.Orc.SweepOnStartup(context.Background(), result)

	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("起動時の掃除で worktree を片付けていない: %v", err)
	}
}

// TestRestore_逃がし先に溜まったhookは索引ができてから配送される は、
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
func TestRestore_逃がし先に溜まったhookは索引ができてから配送される(t *testing.T) {
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

// writePendingStop は逃がし先へ `Stop` の JSON を1件置く（設計 3-19 の形）。
//
// t: 呼び出し元のテスト。
// dir: 逃がし先のディレクトリ。
// receivedAt: ファイル名に使う受信時刻（マイクロ秒）。
// sessionID: hook の session_id。
func writePendingStop(t *testing.T, dir, receivedAt, sessionID string) {
	t.Helper()
	body := `{"session_id":"` + sessionID + `","hook_event_name":"Stop","background_tasks":[]}`
	path := filepath.Join(dir, receivedAt+"-Stop.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("逃がし先のファイルを書けません: %v", err)
	}
}

// TestRestore_In_Reviewで残したpaneを直後の巡回が閉じない は、
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
func TestRestore_In_Reviewで残したpaneを直後の巡回が閉じない(t *testing.T) {
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

// TestReconcile_active_statesに戻ったらdispatchの前にpaneを閉じる は、
// 設計 3-9 の手順7b の本来の目的を確かめる。
//
// 目的: 復元が引き渡し状態で残した pane が、人間の操作で候補に戻ったとき、
// **dispatch する前に閉じないと同じ worktree に2つ目が立つ。**
//
// 与える情報: `In Review` で復元して pane を残したあと、Status を `Ready` へ戻し、
// 巡回を1回回す。
//
// 成功条件: 残っていた pane が閉じられ、その issue が dispatch されて印に入る。
func TestReconcile_active_statesに戻ったらdispatchの前にpaneを閉じる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	issue := sampleIssue(188, "In Review")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	paneID := wt.WorkspaceID + ":p1"
	installPanes(fx, livePane{
		PaneID: paneID, Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})

	restore(t, fx)
	if ids := closedPaneIDs(fx); len(ids) != 0 {
		t.Fatalf("復元の時点で pane を閉じてしまった: %v", ids)
	}

	// 人間が回答して候補に戻した。
	fx.Tracker.SetState(issue.ID, "Ready")
	fx.Orc.Tick(context.Background())

	if ids := closedPaneIDs(fx); indexOf(ids, paneID) < 0 {
		t.Fatalf("候補に戻ったのに残っていた pane を閉じていない: %v", ids)
	}
	waitFor(t, 5*time.Second, "候補に戻った issue が dispatch される", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 1
	})
}

// TestRestore_同じworktreeにpaneが2つあるとき1つだけ引き継ぎ残りを閉じる は、
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
func TestRestore_同じworktreeにpaneが2つあるとき1つだけ引き継ぎ残りを閉じる(t *testing.T) {
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

// TestRestore_agentの一覧を取れなくてもpaneを1つも閉じない は、
// `agent.list` の失敗の扱いが `pane.list` の失敗と対称であることを確かめる。
//
// 目的: agent 名を引けないまま段8b へ流すと、**引き継げたはずの run の pane が
// 全件閉じられる。**herdr の一時的な失敗1回で走っている全部の作業を捨てないため、
// `pane.list` の失敗と同じく「引き継ぎを諦めて次の巡回に委ねる」に倒す。
//
// 与える情報: `In Progress` の run と生きた pane。`agent.list` はエラーを返す。
//
// 成功条件: pane を1つも閉じず、Status も worktree もそのまま。起動は続く。
func TestRestore_agentの一覧を取れなくてもpaneを1つも閉じない(t *testing.T) {
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

// TestRestore_身元ファイルが無くても置き場所とボードから復元する は、設計 3-49 を確かめる。
//
// 目的: 着手は worktree を作ってから身元ファイルを書く（設計 3-16 の段6〜段9）ので、
// **その間で落ちると身元ファイルの無い worktree ができる。**それは「壊れた」のではなく
// 「書き終える前に落ちた」だけであり、置き場所とボードから組み立て直せる。
//
// 与える情報: 身元ファイルを持たない worktree と、その pane と、ボードに載っている issue。
//
// 成功条件: 身元ファイルが書き直され、その run が引き継がれること。
func TestRestore_身元ファイルが無くても置き場所とボードから復元する(t *testing.T) {
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

// {"RUCM-PATH": "P018"}
//
// TestRestore_復元できない壊れたworktreeがあれば起動を止める は、設計 3-49 を確かめる。
//
// 目的: 飛ばして走り続けると、その issue はボードの上で running_state のまま誰にも
// 触られず、**人間が気づくのは何時間も後になる。**既定は止める側である。
//
// 与える情報: JSON が壊れた身元ファイルを持つ worktree と、**ボードに載っていない issue。**
//
// 成功条件: Restore がエラーを返し、**worktree は消えず**、エラーに「何が起きているか」と
// 「次に何をすべきか」の両方が入っていること。
func TestRestore_復元できない壊れたworktreeがあれば起動を止める(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.AllowLog(
		"復元のために引いた issue がボードにありません",
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

// {"RUCM-PATH": "P017"}
//
// TestRestore_paneのlabelが置き場所と食い違えば復元しない は、設計 3-49 の裏取りを確かめる。
//
// 目的: pane の label は herdr の CLI から誰でも書き換えられる。**裏を取らずに使うと、
// label を書き換えるだけで別の issue の worktree として復元させられる。**
// 引き直した issue からスラグを作り直し、目の前のディレクトリ名と一致することを確かめる。
//
// 与える情報: issue 188 の置き場所に立つ、身元ファイルの無い worktree。
// その pane の label は issue 999 を指し、**ボードには 999 だけが載っている。**
//
// 成功条件: 身元ファイルを書かないこと（別の issue のものとして復元しない）。
func TestRestore_paneのlabelが置き場所と食い違えば復元しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Workspace.OnBrokenWorktree = config.OnBrokenWorktreeSkip
	}})
	fx.AllowLog(
		"復元のために引いた issue がボードにありません",
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
