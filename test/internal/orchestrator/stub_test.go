package orchestrator_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/hookserver"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/prompt"
	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// stubHerdr は **socket を使わない** herdr のクライアントである。
//
// **なぜテスト用socket mockではなくこれを使うか。**時間に依存する処理（stall の時計・
// バックオフ・枠待ち）は `testing/synctest` で実時間ゼロで検証する。synctest の中では、
// **network I/O で止まった goroutine があると時計が進まない。**そこで、時間の検査だけは
// 通信を1本も行わないこの stub を使う。
//
// **turn の終わりの判定と着手の13段はテスト用socket mockで検証している**（helpers_test.go）。
type stubHerdr struct {
	mu sync.Mutex
	// status は AgentGet / AgentWait が返す agent の状態である。
	status herdr.AgentStatus
	// revision は AgentGet が返す画面の版である（herdr の pane の revision）。
	// **stall の判定はこの値が増えるかどうかで決まる**（設計 3-21）。
	revision uint64
	// closedPanes は PaneClose に渡された pane の ID である。
	closedPanes []string
	// sentKeys は AgentSendKeys に渡されたキーである。
	sentKeys [][]string
}

// newStubHerdr は stub を作る。
//
// status: AgentGet / AgentWait が返す状態。
// 戻り値: 組み立てた stub。
func newStubHerdr(status herdr.AgentStatus) *stubHerdr {
	return &stubHerdr{status: status}
}

// SetStatus は AgentGet が返す状態を差し替える。
func (s *stubHerdr) SetStatus(status herdr.AgentStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

// BumpRevision は AgentGet が返す画面の版を1つ増やす。
//
// **「エージェントの画面が変わった」ことの再現である。**
func (s *stubHerdr) BumpRevision() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revision++
}

// ClosedPanes は閉じた pane の ID を返す。
func (s *stubHerdr) ClosedPanes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.closedPanes))
	copy(out, s.closedPanes)
	return out
}

// PaneList は pane を1つだけ返す。
func (s *stubHerdr) PaneList(_ context.Context, params herdr.PaneListParams) (*herdr.PaneListResult, error) {
	return &herdr.PaneListResult{
		Type:  "pane_list",
		Panes: []herdr.Pane{{PaneID: params.WorkspaceID + ":p1", WorkspaceID: params.WorkspaceID}},
	}, nil
}

// WorktreeOpen は workspace を1つ返す。
func (s *stubHerdr) WorktreeOpen(_ context.Context, _ herdr.WorktreeOpenParams) (*herdr.WorktreeOpenResult, error) {
	return &herdr.WorktreeOpenResult{Type: "worktree_opened", Workspace: herdr.Workspace{WorkspaceID: "w1"}}, nil
}

// PaneRename は何もせずに成功を返す。
func (s *stubHerdr) PaneRename(_ context.Context, params herdr.PaneRenameParams) (*herdr.PaneRenameResult, error) {
	return &herdr.PaneRenameResult{Type: "pane_info", Pane: herdr.Pane{PaneID: params.PaneID, Label: params.Label}}, nil
}

// PaneClose は閉じた pane を記録する。
func (s *stubHerdr) PaneClose(_ context.Context, params herdr.PaneCloseParams) (*herdr.PaneCloseResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closedPanes = append(s.closedPanes, params.PaneID)
	return &herdr.PaneCloseResult{Type: "pane_closed"}, nil
}

// AgentStartWithRetry は起動できたことにする。
func (s *stubHerdr) AgentStartWithRetry(
	_ context.Context, params herdr.AgentStartParams, _, _ time.Duration,
) (*herdr.AgentStartResult, error) {
	return &herdr.AgentStartResult{
		Type:  "agent_started",
		Agent: herdr.Agent{Name: params.Name.String(), AgentStatus: herdr.AgentStatusIdle, PaneID: params.PaneID},
	}, nil
}

// AgentPrompt は現在の状態のまま返る（turn を終わらせない）。
func (s *stubHerdr) AgentPrompt(_ context.Context, params herdr.AgentPromptParams) (*herdr.AgentPromptResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &herdr.AgentPromptResult{
		Type:  "agent_prompted",
		Agent: herdr.Agent{Name: params.Target.String(), AgentStatus: s.status},
	}, nil
}

// AgentWait は現在の状態を返す。
func (s *stubHerdr) AgentWait(_ context.Context, params herdr.AgentWaitParams) (*herdr.AgentWaitResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &herdr.AgentWaitResult{
		Type:  "agent_info",
		Agent: herdr.Agent{Name: params.Target.String(), AgentStatus: s.status},
	}, nil
}

// AgentGet は現在の状態を返す。
func (s *stubHerdr) AgentGet(_ context.Context, params herdr.AgentGetParams) (*herdr.AgentGetResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &herdr.AgentGetResult{
		Type: "agent_info",
		Agent: herdr.Agent{
			Name:        params.Target.String(),
			AgentStatus: s.status,
			Revision:    s.revision,
		},
	}, nil
}

// AgentList は空の一覧を返す。
func (s *stubHerdr) AgentList(_ context.Context) (*herdr.AgentListResult, error) {
	return &herdr.AgentListResult{Type: "agent_list"}, nil
}

// AgentSendKeys は送られたキーを記録する。
func (s *stubHerdr) AgentSendKeys(_ context.Context, params herdr.AgentSendKeysParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sentKeys = append(s.sentKeys, params.Keys)
	return nil
}

// stubFixture は stubHerdr を使う軽い検査対象である（通信を1本も行わない）。
type stubFixture struct {
	// Orc は検査対象である。
	Orc *orchestrator.Orchestrator
	// Tracker はテスト用トラッカー mockである。
	Tracker *fakeTracker
	// Herdr は通信しない stub である。
	Herdr *stubHerdr
	// Config は Orchestrator に渡した設定である。
	Config config.Config
}

// stubFixtureOptions は newStubFixture の任意の入力である。
type stubFixtureOptions struct {
	// Mutate は設定を書き換える関数である。nil なら既定のまま。
	Mutate func(cfg *config.Config)
	// AgentStatus は stub が返す agent の状態である。空なら idle。
	AgentStatus herdr.AgentStatus
	// RateLimit は枠の読み取りである。nil なら枠の判定を行わない。
	RateLimit *ratelimit.Reader
	// GHAuthCheck は `gh` の認証の検査である。nil なら検査しない。
	GHAuthCheck func(ctx context.Context) error
	// GHLogin は「continuo が使う gh の持ち主」を取る関数である（設計 3-65）。
	//
	// **nil なら testGHLogin を返す偽物を渡す。**渡さないと本物の `gh` が起動する
	// （bubble の中では外部プロセスを起こせない）。
	GHLogin func(ctx context.Context) (string, error)
}

// newStubFixture は通信を行わない検査対象を組み立てる。
//
// **`testing/synctest` の中から呼んでよい。**socket も外部プロセスも使わないので、
// bubble の中の goroutine が network I/O で止まることがない。
//
// t: 呼び出し元のテスト。
// opts: 任意の入力。
// 戻り値: 組み立てた stubFixture。
func newStubFixture(t *testing.T, opts stubFixtureOptions) *stubFixture {
	t.Helper()

	status := opts.AgentStatus
	if status == "" {
		status = herdr.AgentStatusIdle
	}
	stub := newStubHerdr(status)
	ft := newFakeTracker(time.Now)

	root := t.TempDir()
	cfg := *config.DefaultConfig()
	cfg.Workspace.Root = filepath.Join(root, "wt")
	cfg.Claude.TurnTimeoutMs = 60000
	// **入札の締め切りを待たない**（設計 3-77）。
	cfg.Tracker.Provider.Handoff.BidWindowMs = 0
	cfg.RateLimit.Source = "none"
	if opts.Mutate != nil {
		opts.Mutate(&cfg)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr, err := workspace.New(workspace.Options{
		Config:  cfg,
		Logger:  logger,
		HomeDir: root,
		GhqList: func(context.Context, string, string) (string, error) { return "", nil },
	})
	if err != nil {
		t.Fatalf("workspace.New に失敗した: %v", err)
	}

	orc, err := orchestrator.New(orchestrator.Options{
		Config:         cfg,
		Prompt:         prompt.Build(samplePromptTemplate, "/tmp/WORKFLOW.md"),
		Tracker:        ft,
		Herdr:          stub,
		Workspace:      mgr,
		RateLimit:      opts.RateLimit,
		HookSocketPath: filepath.Join(root, "hooks.sock"),
		ContinuoPath:   "/opt/continuo/bin/continuo",
		Logger:         logger,
		GHAuthCheck:    opts.GHAuthCheck,
		// **本物の `gh` を起動させない**（設計 3-65）。
		GHLogin: ghLoginForTest(opts.GHLogin),
	})
	if err != nil {
		t.Fatalf("orchestrator.New に失敗した: %v", err)
	}
	return &stubFixture{Orc: orc, Tracker: ft, Herdr: stub, Config: cfg}
}

// adoptRun は turn を送らずに run を印の集合へ入れる（設計 3-4 の段6 と同じ入口）。
//
// **時間に依存する処理（stall の時計・バックオフ・枠待ち）を、着手の13段を通さずに
// 検査するために使う。**
//
// fx: 対象の stubFixture。
// number: issue の番号。
// 戻り値: 入れた issue。
func adoptRun(fx *stubFixture, number int) tracker.Issue {
	issue := sampleIssue(number, "In Progress")
	fx.Tracker.AddIssue(issue)
	fx.Orc.Adopt(issue, orchestrator.AdoptedRun{
		AgentName:        normalize.SafeName("continuo-hello-world-" + strconv.Itoa(number)),
		PaneID:           "w1:p1",
		SessionUUID:      "session-" + strconv.Itoa(number),
		HerdrWorkspaceID: "w1",
	}, false)
	return issue
}

// viewOf は識別子で RunView を引く。
//
// t: 呼び出し元のテスト。
// fx: 対象の stubFixture。
// identifier: 探す issue の識別子。
// 戻り値の1つ目: 見つかった写し。
// 戻り値の2つ目: 印を持っていれば true。
func viewOf(fx *stubFixture, identifier string) (orchestrator.RunView, bool) {
	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == identifier {
			return v, true
		}
	}
	return orchestrator.RunView{}, false
}

// toolHook は「生きていることの確認」に使う hook を作る（設計 3-21）。
//
// sessionID: セッション UUID。
// name: `PreToolUse` か `PostToolUse`。
// 戻り値: hook のイベント。
func toolHook(sessionID, name string) hookserver.HookEvent {
	return hookserver.HookEvent{HookEventName: name, SessionID: sessionID, PromptID: "p1"}
}

// TestOrchestratorNew_扱うStatusが1つも無い設定を弾く は、組み立てのときの検査を確かめる。
//
// 目的: **知っている Status の一覧は組み立てのときに1度だけ計算する。**
// その計算に使う設定が空のまま渡されても、いままでは黙って通っていた。
// **1つも取れないと、continuo はカンバン上のどの Status も「知らない Status」と判定し、
// 着手した run を片端から止める。**しかも止めた理由には「いま知っているのは です」と
// 空欄が出るだけで、人間には原因が読み取れない。
// **他の必須の依存（Tracker / Herdr / Workspace）と同じく、名前つきのエラーで弾く。**
//
// 与える情報: Status 名を1つも持たない設定と、正しい既定の設定。
// 成功条件: 空の設定ではエラーを返し、既定の設定では組み立てが成功すること。
func TestOrchestratorNew_扱うStatusが1つも無い設定を弾く(t *testing.T) {
	root := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cfg := *config.DefaultConfig()
	cfg.Workspace.Root = filepath.Join(root, "wt")
	mgr, err := workspace.New(workspace.Options{
		Config:  cfg,
		Logger:  logger,
		HomeDir: root,
		GhqList: func(context.Context, string, string) (string, error) { return "", nil },
	})
	if err != nil {
		t.Fatalf("workspace.New に失敗した: %v", err)
	}

	build := func(c config.Config) error {
		_, err := orchestrator.New(orchestrator.Options{
			Config:         c,
			Prompt:         prompt.Build(samplePromptTemplate, "/tmp/WORKFLOW.md"),
			Tracker:        newFakeTracker(nil),
			Herdr:          newStubHerdr(herdr.AgentStatusIdle),
			Workspace:      mgr,
			HookSocketPath: filepath.Join(root, "hooks.sock"),
			ContinuoPath:   "/opt/continuo/bin/continuo",
			Logger:         logger,
		})
		return err
	}

	// **設定に Status 名が1つも無い状態を作る。**
	empty := cfg
	empty.Tracker.ActiveStates = nil
	empty.Tracker.TerminalStates = nil
	empty.Tracker.RunningState = ""
	empty.Tracker.DispatchState = ""
	empty.Tracker.FailureState = ""
	empty.Tracker.StatusSignalMap = nil

	err = build(empty)
	if err == nil {
		t.Fatalf("Status 名が1つも無い設定なのに組み立てが成功した" +
			"（この orchestrator はどの Status も知らないので、着手した run を片端から止める）")
	}
	if !strings.Contains(err.Error(), "Status") {
		t.Errorf("エラーが何を弾いたのかを名指ししていない: %v", err)
	}

	// **正しい設定まで弾いてはならない。**
	if err := build(cfg); err != nil {
		t.Fatalf("既定の設定なのに組み立てが失敗した: %v", err)
	}
}
