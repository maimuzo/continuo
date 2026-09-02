package orchestrator_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// ===== 復元のテストで使う道具 =====

// fakeHookServer は復元が呼ぶ hook の受け口の代わりである。
//
// **実物の socket は立てない。**このテストで確かめたいのは
// 「listen（段5d）→ 読み戻し（段5e）→ 配送の開始（段6b）の順で呼ばれること」だけである。
type fakeHookServer struct {
	mu sync.Mutex
	// calls は呼ばれた順のメソッド名である（"Start" / "ReplayPending" / "StartDelivery"）。
	calls []string
	// startErr は Start が返すエラーである。
	startErr error
}

// Start は listen を始めたことにする（段5d）。
func (f *fakeHookServer) Start() error {
	f.note("Start")
	return f.startErr
}

// ReplayPending は逃がし先を読み戻したことにする（段5e）。
func (f *fakeHookServer) ReplayPending() error {
	f.note("ReplayPending")
	return nil
}

// StartDelivery は配送を始めたことにする（段6b）。
func (f *fakeHookServer) StartDelivery() {
	f.note("StartDelivery")
}

// note は呼び出しを1件積む。
//
// name: 積む名前。
func (f *fakeHookServer) note(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
}

// Calls は呼ばれた順のメソッド名を返す。
func (f *fakeHookServer) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

// preparedWorktree はテストが先に作っておいた worktree である。
type preparedWorktree struct {
	// Path は worktree の絶対パスである。
	Path string
	// Branch は worktree が指す branch 名である。
	Branch string
	// WorkspaceID は herdr の workspace の ID である。
	WorkspaceID string
	// PaneID は worktree.open が作った pane の ID である。
	PaneID string
	// Identity は書いた身元ファイルの中身である。
	Identity workspace.Identity
}

// identityOverride は身元ファイルの値を差し替える指定である。
type identityOverride struct {
	// SocketPath は身元ファイルへ書く socket のパスである。空なら fixture の値。
	SocketPath string
	// TakeoverCount は引き継いだ回数である。
	TakeoverCount int
	// SessionUUID はセッション UUID である。空なら `session-<番号>`。
	SessionUUID string
	// CreatedAt は作成時刻である。ゼロ値なら現在時刻。
	CreatedAt time.Time
	// SkipIdentity を真にすると、身元ファイルを書かない（段9 の検査に使う）。
	SkipIdentity bool
}

// prepareWorktree は本物の git で worktree を作り、身元ファイルを書く。
//
// **復元は「continuo が落ちる前に段6 まで進んでいた」状態から始まる**ので、
// テストはその状態をディスクの上に作ってから Restore を呼ぶ。
//
// t: 呼び出し元のテスト。
// fx: fixture。
// issue: 対象の issue。
// ov: 身元ファイルの差し替え。
// 戻り値: 作った worktree。
func prepareWorktree(t *testing.T, fx *fixture, issue tracker.Issue, ov identityOverride) preparedWorktree {
	t.Helper()

	prepared, err := fx.Workspace.Prepare(context.Background(), workspace.IssueRef{
		URL:           *issue.URL,
		Identifier:    issue.Identifier,
		ProjectItemID: issue.ID,
		Owner:         issue.Owner,
		Repo:          issue.Repo,
		Number:        issue.Number,
		NativeRef:     issue.NativeRef,
	})
	if err != nil {
		t.Fatalf("テスト用の worktree を用意できません: %v", err)
	}

	out := preparedWorktree{
		Path:        prepared.Path,
		Branch:      prepared.Branch.String(),
		WorkspaceID: prepared.HerdrWorkspaceID,
		PaneID:      prepared.HerdrPaneID,
	}
	if ov.SkipIdentity {
		return out
	}

	socketPath := ov.SocketPath
	if socketPath == "" {
		socketPath = fx.SocketPath
	}
	sessionUUID := ov.SessionUUID
	if sessionUUID == "" {
		sessionUUID = "session-" + issue.Identifier
	}
	createdAt := ov.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	identity := workspace.Identity{
		IssueURL:         *issue.URL,
		IssueIdentifier:  issue.Identifier,
		ProjectItemID:    issue.ID,
		Branch:           prepared.Branch.String(),
		HerdrWorkspaceID: prepared.HerdrWorkspaceID,
		SocketPath:       socketPath,
		SettingsPath:     "",
		AgentName:        "continuo-agent",
		SessionUUID:      sessionUUID,
		CreatedAt:        createdAt,
		TakeoverCount:    ov.TakeoverCount,
	}
	if err := fx.Workspace.WriteIdentity(context.Background(), prepared.Path, identity); err != nil {
		t.Fatalf("テスト用の身元ファイルを書けません: %v", err)
	}
	out.Identity = identity
	return out
}

// livePane は「生きている pane」1件の台本である。
type livePane struct {
	// PaneID は pane の ID である。
	PaneID string
	// Cwd は pane の作業ディレクトリである（worktree のパス）。
	Cwd string
	// AgentName は agent.list が返す agent 名である。空なら agent.list に載せない。
	AgentName string
	// AgentStatus は agent の状態である（段5a2 の分岐）。
	AgentStatus herdr.AgentStatus
	// SessionUUID は pane の agent_session が返すセッション UUID である。
	// 空なら agent_session を返さない（身元ファイル側へ落ちる経路の検査に使う）。
	SessionUUID string
	// Label は pane に貼られている label である。
	// **引き継ぎの照合には使わない**（照合は Cwd で行う。設計 3-3）。
	// 旧い形の label が付いた pane でも引き継げることを固定するために置いてある。
	Label string
}

// installPanes はテスト用herdr mock の `pane.list` と `agent.list` を、生きている pane の台本で置き換える。
//
// **`pane.list` は workspace_id を指定しない呼び出し（復元の段4）にも答える。**
//
// fx: fixture。
// panes: 生きている pane の一覧。
func installPanes(fx *fixture, panes ...livePane) {
	fx.Herdr.Handle(herdr.MethodPaneList, func(params map[string]any) (any, *rpcErr) {
		wsID, _ := params["workspace_id"].(string)
		out := []any{}
		for _, p := range panes {
			if wsID != "" && !strings.HasPrefix(p.PaneID, wsID+":") {
				continue
			}
			pane := map[string]any{
				"pane_id":      p.PaneID,
				"cwd":          p.Cwd,
				"agent_status": string(p.AgentStatus),
				"agent":        "claude",
			}
			if p.Label != "" {
				pane["label"] = p.Label
			}
			if p.SessionUUID != "" {
				pane["agent_session"] = map[string]any{
					"source": "herdr:claude", "agent": "claude",
					"kind": "id", "value": p.SessionUUID,
				}
			}
			out = append(out, pane)
		}
		return map[string]any{"type": "pane_list", "panes": out}, nil
	})
	fx.Herdr.Handle(herdr.MethodAgentList, func(map[string]any) (any, *rpcErr) {
		out := []any{}
		for _, p := range panes {
			if p.AgentName == "" {
				continue
			}
			out = append(out, map[string]any{
				"name": p.AgentName, "agent": "claude",
				"agent_status": string(p.AgentStatus), "pane_id": p.PaneID,
				"tab_id": "t1", "workspace_id": "w1", "terminal_id": "term1",
				"focused": false, "revision": 1,
			})
		}
		return map[string]any{"type": "agent_list", "agents": out}, nil
	})
}

// restore は復元を1回だけ走らせる。
//
// t: 呼び出し元のテスト。
// fx: fixture。
// 戻り値の1つ目: 復元の記録。
// 戻り値の2つ目: 偽の hook の受け口（呼ばれた順を検査する）。
func restore(t *testing.T, fx *fixture) (*orchestrator.RestoreResult, *fakeHookServer) {
	t.Helper()
	hs := &fakeHookServer{}
	result, err := fx.Orc.Restore(context.Background(), hs)
	if err != nil {
		t.Fatalf("Restore に失敗した: %v", err)
	}
	return result, hs
}

// closedPaneIDs はテスト用herdr mock が受け取った `pane.close` の pane_id を、受け取った順に返す。
//
// fx: fixture。
// 戻り値: 閉じられた pane の ID。
func closedPaneIDs(fx *fixture) []string {
	var ids []string
	for _, r := range fx.Herdr.Requests() {
		if r.Method == herdr.MethodPaneClose {
			id, _ := r.Params["pane_id"].(string)
			ids = append(ids, id)
		}
	}
	return ids
}

// rewriteIdentityItemID は身元ファイルの project item の ID と識別子だけを書き換える。
//
// **「同じ issue の worktree が2つある」状態をディスクの上に作るために使う**
// （設計 3-4 の段2 の重複の検査）。
//
// t: 呼び出し元のテスト。
// fx: fixture。
// worktreePath: 書き換える worktree のパス。
// itemID: 新しい project item の ID。
// identifier: 新しい issue の識別子。
func rewriteIdentityItemID(t *testing.T, fx *fixture, worktreePath, itemID, identifier string) {
	t.Helper()
	identity, err := fx.Workspace.ReadIdentity(worktreePath)
	if err != nil {
		t.Fatalf("身元ファイルを読めません: %v", err)
	}
	identity.ProjectItemID = itemID
	identity.IssueIdentifier = identifier
	if err := fx.Workspace.WriteIdentity(context.Background(), worktreePath, *identity); err != nil {
		t.Fatalf("身元ファイルを書けません: %v", err)
	}
}

// putStrayWorktree は、置き場所の4階層目にディレクトリと身元ファイルを手で置く。
//
// **`Prepare` を使わない。**同じ issue の worktree を2つ作るには、
// **2つ目を同じ branch でチェックアウトすることになり、git が断る**
// （1つの branch を出せる worktree は1つだけである）。
//
// **ディレクトリ名は issue から作ったスラグにする。**復元の段2 は
// 「身元ファイルが名乗る issue から作り直したスラグ」と突き合わせるので、
// ここを変えると候補から外れて、この helper を使うテストが何も試せなくなる。
//
// **`WriteIdentity` も使わない。**git の登録が無いディレクトリでは
// `info/exclude` への登録が失敗し、宣言していない WARN が1行出る。
//
// t: 呼び出し元のテスト。
// fx: 出来合いの環境。
// codeOwner: 置き場所の2階層目（コードのリポジトリの所有者名）。
// codeRepo: 置き場所の3階層目（コードのリポジトリ名）。
// slug: 置き場所の4階層目（issue から作ったスラグ）。
// identity: 書き込む身元ファイルの中身。
// 戻り値: 置いたディレクトリの絶対パス。
func putStrayWorktree(
	t *testing.T, fx *fixture, codeOwner, codeRepo, slug string, identity workspace.Identity,
) string {
	t.Helper()
	dir := filepath.Join(fx.Workspace.ResolvedRoot(), "github.com", codeOwner, codeRepo, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("テスト用のディレクトリを作れません: %v", err)
	}
	data, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		t.Fatalf("身元ファイルを組み立てられません: %v", err)
	}
	path := filepath.Join(dir, fx.Workspace.IdentityFileName())
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("身元ファイルを書けません: %v", err)
	}
	return dir
}

// syncLog は排他つきのログの受け皿である。
//
// **配送の goroutine とテスト本体が同時に触る**ので、排他を持たない strings.Builder を
// そのまま渡すと `-race` が競合を報告する。
type syncLog struct {
	mu  sync.Mutex
	buf strings.Builder
}

// Write は io.Writer の実装である。
//
// p: 書き込むバイト列。
// 戻り値: 書き込んだバイト数と、常に nil のエラー。
func (l *syncLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

// String は溜まったログを返す。
func (l *syncLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// stringBox は排他つきの文字列の置き場である。
//
// **テスト用herdr mock の台本（接続ごとの goroutine）が書き、テスト本体が読む**ので、
// 素の変数だと `-race` が競合を報告する。
type stringBox struct {
	mu sync.Mutex
	s  string
}

// Set は値を書き込む。
//
// v: 書き込む値。
func (b *stringBox) Set(v string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.s = v
}

// Get は値を読み出す。
func (b *stringBox) Get() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.s
}
