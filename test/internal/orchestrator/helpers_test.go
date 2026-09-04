// Package orchestrator_test は internal/orchestrator の巡回・dispatch・turn ループ・
// 照合・リトライ・stall 検知を検証する。
//
// **実 herdr は使わない。**第2段階と同じく net.Listen("unix", ...) でテスト用socket mockを
// 立て、台本として応答を書く（実 herdr を使うとテストが落ちたときに workspace と pane が
// 残る。偽サーバなら blocked や working も再現できる）。
//
// **git は本物を使う。**一時ディレクトリに bare リポジトリと clone を作り、そこから
// worktree を切る。worktree の作成と削除・`git branch -D`・`git status --porcelain` と
// `git diff --quiet` の判定は、mockでは確かめられない。
//
// **Claude Code は起動しない。**turn の終わりは、偽サーバの `agent.prompt` の応答と、
// テストが Orchestrator.OnHook へ直接流す hook で再現する。
//
// **本番のボード（project #3）へは接続しない。**トラッカーは in-memory のmockである。
package orchestrator_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/hookserver"
	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/prompt"
	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// testGHLogin はテストで使う「continuo が使う gh の持ち主」である（設計 3-65）。
//
// **実在のアカウント名を書かない。**公開リポジトリなので octocat を使う。
const testGHLogin = "octocat"

// testHostName はテストで使う「この機械の名前」である（設計 3-77）。
//
// **架空の名前を使う。**入札と hold のコメントに書かれる値である。
const testHostName = "test-host"

// ghLoginForTest は「gh の持ち主」を取る偽物を返す（設計 3-65）。
//
// **本物を渡すと `gh api user` が起動する。**`testing/synctest` の bubble の中では
// 外部プロセスを起こせないので、テストでは必ず偽物を渡す。
//
// fn: テストが指定した関数。nil なら testGHLogin を返すだけの関数にする。
// 戻り値: 組み立てた関数。
func ghLoginForTest(fn func(ctx context.Context) (string, error)) func(ctx context.Context) (string, error) {
	if fn != nil {
		return fn
	}
	return func(context.Context) (string, error) { return testGHLogin, nil }
}

// ===== 呼び出しの並びを1本にまとめる記録 =====

// timeline はテスト用トラッカー mockとテスト用herdr mock の呼び出しを、**1本の並び**に混ぜて記録する。
//
// **これが無いと、着手の段2（Status の書き込み）と段3（`worktree.open`）の前後関係を
// 比べられない。**それぞれが自分の記録しか持たないと、2つの並びを突き合わせる手段が無い。
type timeline struct {
	mu      sync.Mutex
	entries []string
}

// newTimeline は空の記録を作る。
func newTimeline() *timeline {
	return &timeline{}
}

// note は呼び出しを1件積む。
//
// **nil レシーバでも安全である**（記録を付けない fixture があるため）。
//
// name: 積む名前（`tracker.UpdateStatus` / `herdr.worktree.open` のように前置きを付ける）。
func (tl *timeline) note(name string) {
	if tl == nil {
		return
	}
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.entries = append(tl.entries, name)
}

// Entries は積んだ名前を積んだ順に返す。
func (tl *timeline) Entries() []string {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	out := make([]string, len(tl.entries))
	copy(out, tl.entries)
	return out
}

// IndexOf は name が最初に現れた位置を返す（無ければ -1）。
//
// name: 探す名前。
// 戻り値: 位置。
func (tl *timeline) IndexOf(name string) int {
	return indexOf(tl.Entries(), name)
}

// ===== 手で進められる時計 =====

// testClock は手で進められる時計である。
//
// **バックオフが明けるまでの待ちを実時間ゼロで作る。**`testing/synctest` を使わないのは、
// このファイルの fixture がテスト用socket mock（network I/O）と本物の git を使うためである。
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

// newTestClock は現在時刻から始まる時計を作る。
func newTestClock() *testClock {
	return &testClock{now: time.Now()}
}

// Now はいまの時刻を返す（Orchestrator の Now に渡す）。
func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance は時計を進める。
//
// d: 進める長さ。
func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// ===== テスト用herdr mock socket サーバ =====

// rpcErr は herdr の socket API のエラー応答である。
type rpcErr struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// recordedRequest は偽サーバが受け取ったリクエスト1件である。
type recordedRequest struct {
	// Method は呼ばれたメソッド名である。
	Method string
	// Params は params をそのまま map にしたものである
	// （構造体で受けると、実スキーマに無いキーが混ざっても気づけない）。
	Params map[string]any
}

// herdrHandler は1つのメソッドに対する台本である。
//
// **接続ごとの goroutine から呼ばれるので、この中で t.Fatalf を呼んではならない**
// （FailNow はテスト本体の goroutine からしか呼べない）。失敗は t.Errorf で記録すること。
//
// params: 受け取った params。
// 戻り値の1つ目: 返す result。
// 戻り値の2つ目: エラー応答を返したい場合のエラー。nil なら成功応答になる。
type herdrHandler func(params map[string]any) (any, *rpcErr)

// fakeHerdr は herdr の socket API の代わりに使う偽のサーバである。
//
// **worktree は本物である。**`worktree.open` は渡されたパスに workspace の ID を割り当て、
// `worktree.remove` は実体を消して `git worktree prune` を叩く（本物の herdr と同じ結果に
// なるようにする。そうしないと `git branch -D` が「checkout 中の branch は消せない」で
// 必ず失敗し、片付けの段4 を検証できない）。
type fakeHerdr struct {
	socketPath string

	mu       sync.Mutex
	requests []recordedRequest
	handlers map[string]herdrHandler
	// timeline はテスト用トラッカー mockと共有する呼び出しの並びである（nil なら記録しない）。
	timeline *timeline
	// gitRepoDir は worktree.remove のあとに `git worktree prune` を叩くリポジトリである。
	gitRepoDir string
	// workspaces は workspace の ID から、その workspace が開いているものを引く写像である。
	workspaces map[string]fakeWorkspace
	// nextWS は次に払い出す workspace の通し番号である。
	nextWS int
	// drops は「応答を返さずに接続を切る」メソッドの集合である（herdr の再起動の再現）。
	drops map[string]bool
}

// DropConnection は、そのメソッドを受けたら応答を書かずに接続を切る台本を入れる。
//
// **herdr が再起動した・socket が一瞬切れた場面の再現である。**herdr は
// `{"error":{"code":...}}` を返さない（そもそも答える側が居ない）ので、
// `Handle` で作れるエラーでは再現できない。呼び出し側には continuo が付ける
// `ErrCodeTransport`（`Retryable` が真）が返る。
//
// method: 対象のメソッド名。
func (fh *fakeHerdr) DropConnection(method string) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.drops[method] = true
}

// fakeWorkspace はテスト用herdr mock が持つ workspace 1件である。
//
// **本物と同じく、リポジトリの親 workspace も持つ**（issue #19）。`worktree.open` に
// `cwd` を渡すと、herdr は worktree のぶんに加えて `cwd` のリポジトリのぶんも開く。
// 親は `Checkout` と `RepoRoot` が同じ値になる。
type fakeWorkspace struct {
	// Checkout はその workspace が開いている作業ディレクトリである。
	Checkout string
	// RepoRoot はそのリポジトリ本体のパスである。
	RepoRoot string
}

// SetGitRepoDir は worktree.remove のあとに prune を叩くリポジトリを設定する。
//
// dir: clone の絶対パス。
func (fh *fakeHerdr) SetGitRepoDir(dir string) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.gitRepoDir = dir
}

// workspaceFor はパスに対応する workspace の ID を返す（無ければ払い出す）。
//
// path: その workspace が開く作業ディレクトリ。
// repoRoot: そのリポジトリ本体のパス。**path と同じなら親 workspace である。**
// 戻り値: workspace の ID。
func (fh *fakeHerdr) workspaceFor(path, repoRoot string) string {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	for id, ws := range fh.workspaces {
		if ws.Checkout == path {
			return id
		}
	}
	fh.nextWS++
	id := fmt.Sprintf("w%d", fh.nextWS)
	fh.workspaces[id] = fakeWorkspace{Checkout: path, RepoRoot: repoRoot}
	return id
}

// pathOf は workspace の ID からパスを引く。
//
// id: workspace の ID。
// 戻り値: 作業ディレクトリの絶対パス。分からなければ空文字。
func (fh *fakeHerdr) pathOf(id string) string {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return fh.workspaces[id].Checkout
}

// forgetWorkspace は workspace を一覧から落とす（worktree.remove と workspace.close）。
//
// id: 落とす workspace の ID。
func (fh *fakeHerdr) forgetWorkspace(id string) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	delete(fh.workspaces, id)
}

// OpenWorkspaces は、いま開いている workspace を ID 順に返す（検査から使う）。
//
// 戻り値: workspace の ID から中身への写像の写し。
func (fh *fakeHerdr) OpenWorkspaces() map[string]fakeWorkspace {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	out := make(map[string]fakeWorkspace, len(fh.workspaces))
	for id, ws := range fh.workspaces {
		out[id] = ws
	}
	return out
}

// repoDir は prune を叩くリポジトリを返す。
func (fh *fakeHerdr) repoDir() string {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return fh.gitRepoDir
}

// newFakeHerdr はテスト用herdr mock サーバを1本立て、着手の13段が通るだけの既定の台本を入れる。
//
// t: 呼び出し元のテスト。socket とリスナーの後始末を t.Cleanup に登録する。
// 戻り値: 起動した *fakeHerdr。
//
// socket のパスは意図的に短く保つ（macOS の Unix domain socket のパス長上限は103バイト）。
func newFakeHerdr(t *testing.T) *fakeHerdr {
	t.Helper()

	dir, err := os.MkdirTemp("", "orcherdr")
	if err != nil {
		t.Fatalf("テスト用herdr mock 用の一時ディレクトリを作成できません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	socketPath := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("テスト用herdr mock の socket を bind できません（%s）: %v", socketPath, err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	fh := &fakeHerdr{
		socketPath: socketPath,
		handlers:   map[string]herdrHandler{},
		workspaces: map[string]fakeWorkspace{},
		drops:      map[string]bool{},
	}
	fh.installDefaults()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go fh.serve(t, conn)
		}
	}()
	return fh
}

// installDefaults は着手の13段が最後まで通るだけの既定の台本を入れる。
func (fh *fakeHerdr) installDefaults() {
	fh.Handle(herdr.MethodWorktreeOpen, func(params map[string]any) (any, *rpcErr) {
		path, _ := params["path"].(string)
		cwd, _ := params["cwd"].(string)
		// **本物と同じく workspace を2つ開く**（実測: 2026-08-24。issue #19）。
		// worktree のぶんと、cwd のリポジトリのぶん（＝リポジトリの親 workspace）である。
		// 親を作らないと、片付けが閉じる相手が現れず、閉じ残しの検査が素通りする。
		if cwd != "" {
			fh.workspaceFor(cwd, cwd)
		}
		id := fh.workspaceFor(path, cwd)
		pane := id + ":p1"
		return map[string]any{
			"type":      "worktree_opened",
			"workspace": map[string]any{"workspace_id": id},
			"tab":       map[string]any{"tab_id": id + ":t1"},
			"root_pane": map[string]any{"pane_id": pane, "workspace_id": id},
			"worktree":  map[string]any{"path": path},
		}, nil
	})
	fh.Handle(herdr.MethodWorktreeRemove, func(params map[string]any) (any, *rpcErr) {
		id := fmt.Sprint(params["workspace_id"])
		path := fh.pathOf(id)
		if path != "" {
			// 本物の herdr と同じ結果にする（実体を消して git の登録を掃除する）。
			_ = os.RemoveAll(path)
			if repo := fh.repoDir(); repo != "" {
				_ = exec.Command("git", "-C", repo, "worktree", "prune").Run()
			}
		}
		// **リポジトリの親 workspace は落とさない。**本物の worktree.remove も閉じない。
		fh.forgetWorkspace(id)
		return map[string]any{
			"type":         "worktree_removed",
			"workspace_id": id,
			"path":         path,
			"forced":       true,
		}, nil
	})
	fh.Handle(herdr.MethodWorkspaceList, func(map[string]any) (any, *rpcErr) {
		list := []any{}
		for id, ws := range fh.OpenWorkspaces() {
			list = append(list, map[string]any{
				"workspace_id": id,
				"worktree": map[string]any{
					"checkout_path": ws.Checkout,
					"repo_root":     ws.RepoRoot,
				},
			})
		}
		return map[string]any{"type": "workspace_list", "workspaces": list}, nil
	})
	fh.Handle(herdr.MethodWorkspaceClose, func(params map[string]any) (any, *rpcErr) {
		fh.forgetWorkspace(fmt.Sprint(params["workspace_id"]))
		return map[string]any{"type": "ok"}, nil
	})
	fh.Handle(herdr.MethodPaneList, func(params map[string]any) (any, *rpcErr) {
		id, _ := params["workspace_id"].(string)
		if id == "" {
			id = "w1"
		}
		return map[string]any{
			"type": "pane_list",
			"panes": []any{
				map[string]any{"pane_id": id + ":p1", "workspace_id": id, "agent_status": "idle", "interactive_ready": true},
			},
		}, nil
	})
	fh.Handle(herdr.MethodPaneRename, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type": "pane_info",
			"pane": map[string]any{"pane_id": fmt.Sprint(params["pane_id"]), "label": fmt.Sprint(params["label"])},
		}, nil
	})
	fh.Handle(herdr.MethodWorkspaceRename, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type": "workspace_info",
			"workspace": map[string]any{
				"workspace_id": fmt.Sprint(params["workspace_id"]), "label": fmt.Sprint(params["label"]),
			},
		}, nil
	})
	fh.Handle(herdr.MethodPaneClose, func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "pane_closed"}, nil
	})
	fh.Handle(herdr.MethodAgentList, func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "agent_list", "agents": []any{}}, nil
	})
	fh.Handle(herdr.MethodAgentStart, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type":  "agent_started",
			"agent": map[string]any{"name": params["name"], "agent_status": "idle", "interactive_ready": true, "pane_id": params["pane_id"]},
		}, nil
	})
	fh.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type":  "agent_info",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})
	fh.Handle(herdr.MethodAgentWait, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type":  "agent_info",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})
	fh.Handle(herdr.MethodAgentSendKeys, func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "keys_sent"}, nil
	})
	fh.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})
}

// Handle はメソッドの台本を差し替える。
//
// method: 対象のメソッド名。
// fn: 応答を決める関数。**t.Fatalf を使ってはならない。**
func (fh *fakeHerdr) Handle(method string, fn herdrHandler) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.handlers[method] = fn
}

// HandlerOf はいま入っている台本を返す。
//
// **本物の herdr に近づける「包む」台本を書くために使う。**既定の台本を写し取らずに
// 書き直すと、テストだけで通る挙動が2つに分かれる。
//
// method: 対象のメソッド名。
// 戻り値: いまの台本（無ければ nil）。
func (fh *fakeHerdr) HandlerOf(method string) herdrHandler {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return fh.handlers[method]
}

// serve は1本の接続を処理する。
//
// **接続ごとの goroutine から呼ばれるので t.Fatalf を使ってはならない。**
func (fh *fakeHerdr) serve(t *testing.T, conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	// **改行まで読み切れなかったものは、リクエストではない。**
	// `bufio.Reader.ReadBytes` は改行を見つけたときだけ err に nil を返すので、
	// **err が nil でないなら、その時点で中身は必ず途中までである。**
	// 途中までのものを解析すれば、当然「壊れた JSON」に見える。
	// **それは送り手の欠陥ではないので、テストを落としてはならない。**
	//
	// **continuo は、書いている途中で接続を切ることがある。**herdr のクライアントは
	// ctx が終わると conn を閉じ（[internal/herdr/client.go](../../../internal/herdr/client.go) の
	// `context.AfterFunc`）、`conn.SetDeadline` の期限が来ても書き込みを打ち切る。
	// **走行中の run は、テスト本体が終わったあとに `agent.prompt` を送ることがある**ので、
	// 片付けの `orc.Close` がちょうどその書き込みを切る。
	//
	// **macOS では、これが「途中まで届いた」という形で実際に表に出る。**
	// Unix domain socket の送信バッファが 8192 バイトしかないため
	// （`sysctl net.local.stream.sendspace`）、**それより大きいリクエストは
	// 受け手が読み進めるまで書き込みが止まる。**`agent.prompt` は
	// [internal/prompt/builtin.md](../../../internal/prompt/builtin.md)（14583 バイト）を
	// 含むので 15119 バイトあり、必ずここで止まる。切られると 8192 バイトだけが届く。
	// **Linux は既定のバッファがずっと大きく、書き込みが止まらないので同じ commit でも起きない。**
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return
	}
	var req struct {
		ID     string         `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(line, &req); err != nil {
		t.Errorf("テスト用herdr mock がリクエストを解析できません: %v", err)
		return
	}

	fh.mu.Lock()
	fh.requests = append(fh.requests, recordedRequest{Method: req.Method, Params: req.Params})
	handler := fh.handlers[req.Method]
	drop := fh.drops[req.Method]
	tl := fh.timeline
	fh.mu.Unlock()
	// **トラッカーと同じ1本の並びへ積む。**別々の記録では前後関係を比べられない。
	tl.note("herdr." + req.Method)

	// **答えずに切る**（DropConnection。herdr の再起動の再現）。
	// 受け取ったことは上で記録済みなので、何回届いたかは検査できる。
	if drop {
		return
	}

	var resp map[string]any
	if handler == nil {
		resp = map[string]any{"id": req.ID, "error": map[string]any{"code": "unknown_method", "message": req.Method}}
	} else {
		result, rerr := handler(req.Params)
		if rerr != nil {
			resp = map[string]any{"id": req.ID, "error": map[string]any{"code": rerr.Code, "message": rerr.Message}}
		} else {
			resp = map[string]any{"id": req.ID, "result": result}
		}
	}
	encoded, err := json.Marshal(resp)
	if err != nil {
		t.Errorf("テスト用herdr mock が応答を JSON 化できません: %v", err)
		return
	}
	if _, err := conn.Write(append(encoded, '\n')); err != nil {
		t.Logf("テスト用herdr mock が応答を書けません（無視する）: %v", err)
	}
}

// Requests は受け取ったリクエストを受け取った順に返す。
func (fh *fakeHerdr) Requests() []recordedRequest {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	out := make([]recordedRequest, len(fh.requests))
	copy(out, fh.requests)
	return out
}

// Methods は受け取ったリクエストのメソッド名を受け取った順に返す。
func (fh *fakeHerdr) Methods() []string {
	names := []string{}
	for _, r := range fh.Requests() {
		names = append(names, r.Method)
	}
	return names
}

// CountMethod は method が呼ばれた回数を返す。
func (fh *fakeHerdr) CountMethod(method string) int {
	n := 0
	for _, m := range fh.Methods() {
		if m == method {
			n++
		}
	}
	return n
}

// ParamsOf は method に対する最初のリクエストの params を返す。
//
// t: 呼び出し元のテスト。**テスト本体の goroutine から呼ぶこと。**
// method: 探すメソッド名。
// 戻り値: params を map にしたもの。
func (fh *fakeHerdr) ParamsOf(t *testing.T, method string) map[string]any {
	t.Helper()
	for _, r := range fh.Requests() {
		if r.Method == method {
			return r.Params
		}
	}
	t.Fatalf("テスト用herdr mock は %s を受け取っていません（受け取ったのは %v）", method, fh.Methods())
	return nil
}

// Client は偽サーバを向いた本物の herdr クライアントを返す。
func (fh *fakeHerdr) Client() *herdr.Client {
	return herdr.New(fh.socketPath, herdr.Timeouts{
		Read:    5 * time.Second,
		Startup: 5 * time.Second,
		Turn:    20 * time.Second,
	})
}

// ===== テスト用トラッカー mock =====

// fakeTracker は in-memory のボードである。**本番のボードへは接続しない。**
type fakeTracker struct {
	mu sync.Mutex
	// board はボードの並び順そのままの issue の一覧である。
	board []tracker.Issue
	// comments は issue のノード ID ごとのコメントである。
	comments map[string][]tracker.Comment
	// calls は呼ばれたメソッド名を呼ばれた順に記録したものである。
	calls []string
	// verifyErr は VerifyStatusOptions が返すエラーである。
	verifyErr error
	// statesErr は FetchIssuesByStates が返すエラーである。
	statesErr error
	// updateErr は UpdateStatus が返すエラーである。
	//
	// **GitHub へ書けない状況の再現に使う**（認証切れ・レートリミット・ネットワークの断）。
	updateErr error
	// idsErr は ID 指定の取り直しが返すエラーである（復元の段3 の失敗の再現）。
	//
	// **記録を取る側と取らない側の両方に効く。**どちらも `fetchByIDs` を通るからである
	// （`FetchIssuesByIDs` / `FetchIssuesByIDsWithoutTimeline`。設計 3-61）。
	idsErr error
	// commentsErr は FetchComments が返すエラーである。
	//
	// **issue のコメントを読めない状況の再現に使う**（設計 3-25 の段1。
	// 読めなかったときは「書かれていないもの」として扱う）。
	commentsErr error
	// commentsTruncated は FetchAllComments が「古い側を読み切れなかった」と
	// 名乗るかである（issue #140）。
	commentsTruncated bool
	// commentsSelfLogin は FetchComments が最後に受け取った「gh の持ち主」である
	// （設計 3-65）。**印だけで判定していないことを確かめるために記録する。**
	commentsSelfLogin string
	// postErr は PostComment が返すエラーである。
	//
	// **issue へ書けない状況の再現に使う**（片付けを見送った通知・引き渡しの通知）。
	postErr error
	// postErrOnMarker と postErrOnMarkerErr は、本文がその印で始まるコメントだけ
	// PostComment を失敗させる。
	//
	// **`postErr` は全件を止めてしまう。**hold だけ書けない状況（設計 3-77g）を作るには、
	// 先に書く入札のコメントは成功させたまま、hold のコメントだけを失敗させる必要がある。
	postErrOnMarker    string
	postErrOnMarkerErr error
	// updateGate は UpdateStatus を待たせる関門である（nil なら待たせない。HoldUpdate が仕掛ける）。
	updateGate chan struct{}
	// updateEntered は UpdateStatus が関門に着いたことをテストへ知らせるチャネルである。
	updateEntered chan struct{}
	// now は CreatedAt に入れる時刻を返す関数である。
	now func() time.Time
	// timeline はテスト用herdr mock と共有する呼び出しの並びである（nil なら記録しない）。
	timeline *timeline
	// viewer はこのテスト用トラッカー mock が名乗る「gh の持ち主」である（設計 3-77b）。
	viewer tracker.Assignee
	// viewerErr は FetchViewer が返すエラーである。
	viewerErr error
	// assignErr は AddAssignees / RemoveAssignees が返すエラーである。
	assignErr error
	// extraCandidates は FetchIssuesByStates が「頼んだ Status に無いのに返してくる」
	// item である（設計 3-34 の read-after-write の食い違いの再現）。
	//
	// **GitHub の `items(query:)` はサーバ側の検索であり、continuo が直前に書いた値が
	// 索引へ反映される前に取り直すと、古い絞り込みで当たった item がそのまま返る。**
	// ボード（board）の Status とは別に、候補の一覧にだけ載る写しを持たせる。
	extraCandidates []tracker.Issue
}

// fakeViewerLogin はテスト用トラッカー mock が名乗る「gh の持ち主」のログイン名である。
//
// **`testGHLogin` と同じにする。**印の判定（設計 3-65）と担当の持ち回りの判定
// （設計 3-77b）が別の名前を見ていると、**片方だけが「自分の書いたもの」と認める。**
const fakeViewerLogin = testGHLogin

// fakeViewerID は fakeViewerLogin のノード ID である。
const fakeViewerID = "U_octocat"

// newFakeTracker はテスト用トラッカー mockを作る。
//
// now: 現在時刻を返す関数。nil なら time.Now を使う。
// 戻り値: 空のボードを持つテスト用トラッカー mock。
func newFakeTracker(now func() time.Time) *fakeTracker {
	if now == nil {
		now = time.Now
	}
	return &fakeTracker{
		comments: map[string][]tracker.Comment{},
		now:      now,
		// **持ち主を最初から持たせる。**担当の持ち回りの判定（設計 3-77b）は
		// 「自分が誰か」が分からないと、担当のある issue に一切触らない。
		viewer: tracker.Assignee{ID: fakeViewerID, Login: fakeViewerLogin},
	}
}

// record は呼ばれたメソッドを記録する。
//
// **テスト用herdr mock と共有する並びにも同時に積む。**着手の段2（Status の書き込み）が
// 段3（`worktree.open`）より前に起きたことは、1本の並びでしか比べられない。
//
// **呼び出し側が ft.mu を保持したまま呼ぶ。**timeline は別の排他を持つので、
// ここから触ってよい。
func (ft *fakeTracker) record(name string) {
	ft.calls = append(ft.calls, name)
	ft.timeline.note("tracker." + name)
}

// Calls は呼ばれたメソッド名を呼ばれた順に返す。
func (ft *fakeTracker) Calls() []string {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	out := make([]string, len(ft.calls))
	copy(out, ft.calls)
	return out
}

// ResetCalls は呼び出しの記録を消す（巡回1回ぶんを数えるため）。
func (ft *fakeTracker) ResetCalls() {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.calls = nil
}

// CountCall は name が呼ばれた回数を返す。
func (ft *fakeTracker) CountCall(name string) int {
	n := 0
	for _, c := range ft.Calls() {
		if c == name {
			n++
		}
	}
	return n
}

// SetStatesError は FetchIssuesByStates が返すエラーを差し替える
// （ボードを読めない状況の再現）。
//
// err: 返すエラー。nil なら成功にする。
func (ft *fakeTracker) SetStatesError(err error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.statesErr = err
}

// SetUpdateError は UpdateStatus が返すエラーを差し替える
// （Status を書けない状況の再現）。
//
// err: 返すエラー。nil なら成功にする。
func (ft *fakeTracker) SetUpdateError(err error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.updateErr = err
}

// SetVerifyError は VerifyStatusOptions が返すエラーを差し替える
// （Status の選択肢名が人間の改名で食い違った状況の再現）。
//
// err: 返すエラー。nil なら成功にする。
func (ft *fakeTracker) SetVerifyError(err error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.verifyErr = err
}

// SetIDsError は ID 指定の取り直しが返すエラーを差し替える
// （復元の取り直しが認証切れ・レートリミットで落ちる状況の再現。設計 3-4 の段3）。
//
// **記録を取る側と取らない側の両方に効く**（`FetchIssuesByIDs` /
// `FetchIssuesByIDsWithoutTimeline`。設計 3-61）。復元が呼ぶのは取らない側である。
//
// err: 返すエラー。nil なら成功にする。
func (ft *fakeTracker) SetIDsError(err error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.idsErr = err
}

// SetCommentsError は FetchComments が返すエラーを差し替える
// （issue のコメントを読めない状況の再現。設計 3-25 の段1）。
//
// err: 返すエラー。nil なら成功にする。
func (ft *fakeTracker) SetCommentsError(err error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.commentsErr = err
}

// CommentsSelfLogin は FetchComments が最後に受け取った「gh の持ち主」を返す（設計 3-65）。
//
// 戻り値: 受け取ったログイン名。**1度も呼ばれていなければ空文字。**
func (ft *fakeTracker) CommentsSelfLogin() string {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	return ft.commentsSelfLogin
}

// SetPostError は PostComment が返すエラーを差し替える
// （issue へコメントを書けない状況の再現）。
//
// err: 返すエラー。nil なら成功にする。
// SetCommentsTruncated は、FetchAllComments が「古い側を読み切れなかった」と
// 名乗るかを決める（issue #140）。
//
// **件数では作れない状況である。**1ページが100件に満たないまま20ページを使い切ると、
// 2000件に届かないまま切れる。そこをアダプタが真偽値で返すので、ここも真偽値で作る。
//
// truncated: 真なら「古い側を読み切れなかった」と名乗る。
func (ft *fakeTracker) SetCommentsTruncated(truncated bool) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.commentsTruncated = truncated
}

func (ft *fakeTracker) SetPostError(err error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.postErr = err
}

// SetPostErrorForMarker は、本文がその印で始まるコメントだけ PostComment を失敗させる
// （設計 3-77g。hold だけ書けない状況の再現に使う）。
//
// marker: 本文の先頭に付く印（例: `config.HandoffHoldMarker`）。
// err: 返すエラー。
func (ft *fakeTracker) SetPostErrorForMarker(marker string, err error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.postErrOnMarker = marker
	ft.postErrOnMarkerErr = err
}

// HoldUpdate は**次の1回の `UpdateStatus` を、返り値の関数を呼ぶまで返さないようにする。**
//
// **書き込みが飛んでいる最中に別のことが起きる場面を作るためにある**（設計 3-54）。
// 書き込みは巡回のループとは別の goroutine で走るので、テストから
// 「書き込みの途中」を掴む手が無いと、その最中に run を手放す並びを作れない。
//
// 戻り値の1つ目: 待たせている `UpdateStatus` を進ませる関数。**必ず1回呼ぶこと。**
// 戻り値の2つ目: `UpdateStatus` が関門に着いたら閉じるチャネル。
func (ft *fakeTracker) HoldUpdate() (func(), <-chan struct{}) {
	gate := make(chan struct{})
	entered := make(chan struct{})
	ft.mu.Lock()
	ft.updateGate = gate
	ft.updateEntered = entered
	ft.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(gate) }) }, entered
}

// waitAtUpdateGate は、関門が仕掛けられていれば `UpdateStatus` をそこで待たせる。
//
// **`ft.mu` を持ったまま待たない。**持ったまま待つと、ボードを読む呼び出しが全部止まり、
// 巡回のループごと固まる。
func (ft *fakeTracker) waitAtUpdateGate() {
	ft.mu.Lock()
	gate := ft.updateGate
	entered := ft.updateEntered
	ft.updateGate = nil
	ft.updateEntered = nil
	ft.mu.Unlock()
	if gate == nil {
		return
	}
	close(entered)
	<-gate
}

// AddIssue はボードの末尾に issue を足す。
func (ft *fakeTracker) AddIssue(issue tracker.Issue) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.board = append(ft.board, issue)
}

// RemoveIssue はボードから issue を落とす
// （人間がボードから外した・archive した状況の再現。設計 3-10）。
//
// **`FetchIssuesByIDs` からも返らなくなる。**continuo はこれを「もう見えない」として
// 面倒を見るのをやめる。
//
// id: 落とす project item ID。
func (ft *fakeTracker) RemoveIssue(id string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	out := make([]tracker.Issue, 0, len(ft.board))
	for _, issue := range ft.board {
		if issue.ID != id {
			out = append(out, issue)
		}
	}
	ft.board = out
}

// SetState は issue の Status を直接書き換える（エージェントが gh で動かした状況の再現）。
//
// id: project item の ID。
// state: 新しい Status。
func (ft *fakeTracker) SetState(id, state string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	for i := range ft.board {
		if ft.board[i].ID == id {
			ft.board[i].State = state
			// **人間が動かした扱いにする**（設計 3-54 の `actor.__typename` が `User`）。
			ft.board[i].StatusChangedByAutomation = false
			ft.board[i].StatusChangedBy = "octocat"
			return
		}
	}
}

// SetDispatchable は issue の `Dispatchable` を書き換える（設計 3-13）。
//
// **本物のアダプタは、リポジトリが Claude Code に信頼登録されていないと偽にして返す。**
// Status は `active_states` のまま偽になることがあり、そのときの巡回の振る舞いを見るために使う。
//
// id: project item の ID。
// dispatchable: 新しい値。
func (ft *fakeTracker) SetDispatchable(id string, dispatchable bool) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	for i := range ft.board {
		if ft.board[i].ID == id {
			ft.board[i].Dispatchable = dispatchable
			return
		}
	}
}

// ClearStatusAuthor は「いまの Status を誰が書いたか分からない」状況を作る
// （設計 3-54。timeline のイベントが消えた・権限が無い・直近50件から溢れた）。
//
// id: project item の ID。
func (ft *fakeTracker) ClearStatusAuthor(id string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	for i := range ft.board {
		if ft.board[i].ID == id {
			ft.board[i].StatusChangedByAutomation = false
			ft.board[i].StatusChangedBy = ""
			return
		}
	}
}

// SetStateByAutomation は、ボードの組み込みの自動化が Status を動かした状況を作る
// （設計 3-54。PR を issue に紐づけた・PR をマージしたときに起きる）。
//
// **ID 指定で取り直したときだけ「自動化が書いた」と分かる。**候補の取得では分からない
// （FetchIssuesByStates がその欄を落として返す）。
//
// id: project item の ID。
// state: 自動化が書いた Status。
func (ft *fakeTracker) SetStateByAutomation(id, state string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	for i := range ft.board {
		if ft.board[i].ID == id {
			ft.board[i].State = state
			ft.board[i].StatusChangedByAutomation = true
			ft.board[i].StatusChangedBy = "github-project-automation"
			return
		}
	}
}

// isHandoffComment は、そのコメントが引き渡しの通知かどうかを返す。
//
// **continuo が自分で書くコメントは2種類ある**（設計 3-29）。引き渡しの通知と、
// Status を動かした記録である。**どちらにも self_marker が付くので、
// `IsSelf` だけでは区別できない。**本文で選り分ける。
func isHandoffComment(c tracker.Comment) bool {
	return strings.Contains(c.Body, "の作業を人間へ引き渡しました")
}

// isStatusMoveComment は、そのコメントが Status を動かした記録かどうかを返す。
//
// **引き渡しの通知の中にも遷移の1行が入る**ので、そちらを先に除く。
func isStatusMoveComment(c tracker.Comment) bool {
	return !isHandoffComment(c) && strings.Contains(c.Body, "へ動かしました。")
}

// HandoffCommentsOf は issue に付いた引き渡しの通知だけを返す。
func (ft *fakeTracker) HandoffCommentsOf(nodeID string) []tracker.Comment {
	var out []tracker.Comment
	for _, c := range ft.CommentsOf(nodeID) {
		if isHandoffComment(c) {
			out = append(out, c)
		}
	}
	return out
}

// StatusMoveCommentsOf は issue に付いた「Status を動かした記録」だけを返す。
func (ft *fakeTracker) StatusMoveCommentsOf(nodeID string) []tracker.Comment {
	var out []tracker.Comment
	for _, c := range ft.CommentsOf(nodeID) {
		if isStatusMoveComment(c) {
			out = append(out, c)
		}
	}
	return out
}

// StateOf は issue の現在の Status を返す。
func (ft *fakeTracker) StateOf(id string) string {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	for i := range ft.board {
		if ft.board[i].ID == id {
			return ft.board[i].State
		}
	}
	return ""
}

// SetAssignees はボードの issue の担当者を差し替える（設計 3-77b）。
//
// **人間や別の機械が担当を書き換えた状況の再現に使う。**
//
// id: project item の ID。
// logins: 担当者のログイン名（ノード ID は `U_` + ログイン名にする）。
func (ft *fakeTracker) SetAssignees(id string, logins ...string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	for i := range ft.board {
		if ft.board[i].ID != id {
			continue
		}
		out := make([]tracker.Assignee, 0, len(logins))
		for _, l := range logins {
			out = append(out, tracker.Assignee{ID: "U_" + l, Login: l})
		}
		ft.board[i].Assignees = out
		ft.board[i].AssigneeCount = len(out)
		return
	}
}

// IssueByID はボードの issue を1件返す（担当者の変化を確かめるために使う）。
//
// id: project item の ID。
// 戻り値の1つ目: 見つかった issue の写し。
// 戻り値の2つ目: 見つかれば true。
func (ft *fakeTracker) IssueByID(id string) (tracker.Issue, bool) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	for i := range ft.board {
		if ft.board[i].ID == id {
			return ft.board[i], true
		}
	}
	return tracker.Issue{}, false
}

// AddCommentBy は投稿者を指定して issue にコメントを足す。
//
// **担当の持ち回りの判定は投稿者を見る**（設計 3-77b の「担当者の最後のコメント」）ので、
// 投稿者を指定できないと期限の判定を組み立てられない。
//
// nodeID: 下敷きの GitHub issue のノード ID。
// author: 投稿者のログイン名。
// body: コメント本文（印を剥がさずそのまま）。
// createdAt: 作成時刻。
func (ft *fakeTracker) AddCommentBy(nodeID, author, body string, createdAt time.Time) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.comments[nodeID] = append(ft.comments[nodeID], tracker.Comment{
		ID: fmt.Sprintf("C_%d", len(ft.comments[nodeID])+1), Body: body,
		Author: author, CreatedAt: createdAt,
	})
}

// ClearComments は issue に付いたコメントを全部消す。
//
// **入札を書き直せる状態を作るために使う。**入札のコメントが残っていると
// `HasBidBy` が「もう書いた」と読み、2回目の巡回では枠の判定へ進まない。
//
// nodeID: 下敷きの GitHub issue のノード ID。
func (ft *fakeTracker) ClearComments(nodeID string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	delete(ft.comments, nodeID)
}

// AddComment は issue にコメントを足す。
//
// nodeID: 下敷きの GitHub issue のノード ID。
// body: 本文。
// isAgent: エージェントが書いた印が付いているか。
// createdAt: 作成時刻。
func (ft *fakeTracker) AddComment(nodeID, body string, isAgent bool, createdAt time.Time) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.comments[nodeID] = append(ft.comments[nodeID], tracker.Comment{
		ID: fmt.Sprintf("C_%d", len(ft.comments[nodeID])+1), Body: body,
		IsAgent: isAgent, CreatedAt: createdAt,
	})
}

// AddSpoofedComment は「印は付いているが、投稿者が gh の持ち主ではない」コメントを足す
// （設計 3-65）。
//
// **本物の FetchComments が第三者の印付きコメントに対して返す形をそのまま作る。**
// すなわち IsAgent は偽のまま、MarkedByOther だけが立つ。
//
// nodeID: 下敷きの GitHub issue のノード ID。
// body: 本文（印で始まるもの）。
// author: 投稿者のログイン名（持ち主ではない誰か）。
// createdAt: 作成時刻。
func (ft *fakeTracker) AddSpoofedComment(nodeID, body, author string, createdAt time.Time) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.comments[nodeID] = append(ft.comments[nodeID], tracker.Comment{
		ID: fmt.Sprintf("C_%d", len(ft.comments[nodeID])+1), Body: body,
		Author: author, URL: "https://example.test/comment/spoofed",
		MarkedByOther: true, CreatedAt: createdAt,
	})
}

// CommentsOf は issue に付いているコメントを返す。
//
// **持ち回りの印が付いたコメント（入札・hold・released）は除く**（設計 3-77a）。
// あれは continuo どうしが読み合う覚え書きであり、**人間にもエージェントにも見せない。**
// 数に入れると、「何も起きていないのにコメントしている」の判定が入札で崩れる。
// **持ち回りのコメントを数えたいテストは HandoffCommentsOf を使うこと。**
func (ft *fakeTracker) CommentsOf(nodeID string) []tracker.Comment {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	out := make([]tracker.Comment, 0, len(ft.comments[nodeID]))
	for _, c := range ft.comments[nodeID] {
		if handoff.IsMarked(c.Body) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// MarkedHandoffCommentsOf は issue に付いた「機械どうしの持ち回りのコメント」だけを返す
// （設計 3-77a の入札・hold・released）。
//
// **`HandoffCommentsOf` とは別物である。**あちらは「人間への引き渡しの通知」を返す。
//
// nodeID: 対象の issue のノード ID。
// marker: 探す印（`config.HandoffBidMarker` など）。空なら3つの印すべてを返す。
// 戻り値: 印の付いたコメント。
func (ft *fakeTracker) MarkedHandoffCommentsOf(nodeID, marker string) []tracker.Comment {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	out := make([]tracker.Comment, 0, len(ft.comments[nodeID]))
	for _, c := range ft.comments[nodeID] {
		if !handoff.IsMarked(c.Body) {
			continue
		}
		if marker != "" && !strings.HasPrefix(strings.TrimSpace(c.Body), marker) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// FetchIssuesByStates は states に含まれる Status の issue を、ボードの並び順のまま返す。
func (ft *fakeTracker) FetchIssuesByStates(_ context.Context, states []string) ([]tracker.Issue, error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.record("FetchIssuesByStates")
	if ft.statesErr != nil {
		return nil, ft.statesErr
	}
	// **絞り込みの反映待ちの再現。**頼んだ Status に無い item を**先頭に**混ぜる。
	// 先頭に置くのは、そこで巡回が止まると後続の issue が着手されなくなるためである。
	out := append([]tracker.Issue(nil), ft.extraCandidates...)
	for _, issue := range ft.board {
		for _, s := range states {
			if strings.EqualFold(s, issue.State) {
				// **候補の取得は「誰が Status を書いたか」を持たない**（設計 3-54）。
				// 本物のクエリに timeline を足していないので、ここでも落として返す。
				// **落とさないと、候補の側に頼った実装が書けてしまう。**
				issue.StatusChangedByAutomation = false
				issue.StatusChangedBy = ""
				out = append(out, issue)
				break
			}
		}
	}
	return out, nil
}

// SetExtraCandidates は「頼んだ Status に無いのに候補として返ってくる item」を差し替える。
//
// **ボードの Status は動かさない。**候補の一覧にだけ載る写しである
// （GitHub の絞り込みの索引が古いまま当たった状況の再現。設計 3-34）。
//
// issues: 候補の一覧の末尾へ足す写し。
func (ft *fakeTracker) SetExtraCandidates(issues ...tracker.Issue) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.extraCandidates = issues
}

// FetchIssuesByIDs は ID 指定で取り直す。見つからない ID は結果から省く。
// **「いまの Status を書いたのは誰か」も一緒に返す**（設計 3-54）。
func (ft *fakeTracker) FetchIssuesByIDs(_ context.Context, ids []string) ([]tracker.Issue, error) {
	return ft.fetchByIDs("FetchIssuesByIDs", ids, true)
}

// FetchIssuesByIDsWithoutTimeline は ID 指定で取り直すが、
// **「いまの Status を書いたのは誰か」は返さない**（設計 3-61）。
//
// **本物と同じく `StatusChangedBy` と `StatusChangedByAutomation` を落とす。**
// 落とさないと、記録を読む実装がこちらの経路でも通ってしまい、
// **テストが「記録を取らずに済ませた」ことを検出できなくなる**
// （`FetchIssueByIdentifier` と同じ理由）。
func (ft *fakeTracker) FetchIssuesByIDsWithoutTimeline(_ context.Context, ids []string) ([]tracker.Issue, error) {
	return ft.fetchByIDs("FetchIssuesByIDsWithoutTimeline", ids, false)
}

// fetchByIDs は ID 指定の取り直しの本体である。
//
// call: 呼び出しの記録に残す名前。
// ids: 取り直す project item ID の一覧。
// withTimeline: 「いまの Status を書いたのは誰か」を残すか。
func (ft *fakeTracker) fetchByIDs(call string, ids []string, withTimeline bool) ([]tracker.Issue, error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if len(ids) == 0 {
		return nil, nil
	}
	ft.record(call)
	if ft.idsErr != nil {
		return nil, ft.idsErr
	}
	var out []tracker.Issue
	for _, id := range ids {
		for _, issue := range ft.board {
			if issue.ID == id {
				if !withTimeline {
					issue.StatusChangedByAutomation = false
					issue.StatusChangedBy = ""
				}
				out = append(out, issue)
			}
		}
	}
	return out, nil
}

// CountIDRefreshes は ID 指定の取り直しを呼んだ回数を、timeline の有無をまとめて数える。
//
// **リクエストの本数を数える場面と、「取り直しが走ったか」を待ち合わせる場面のためのものである。**
// どちらもクエリの中身ではなく「1本打ったか」を見ている（設計 3-31 / 3-61）。
// **どちらの経路を通ったかを見たいときは `CountCall` を名前で呼ぶこと。**
func (ft *fakeTracker) CountIDRefreshes() int {
	return ft.CountCall("FetchIssuesByIDs") + ft.CountCall("FetchIssuesByIDsWithoutTimeline")
}

// FetchIssueByIdentifier は識別子で1件引く。**見つからないことをエラーにしない。**
func (ft *fakeTracker) FetchIssueByIdentifier(_ context.Context, identifier string) (tracker.Issue, bool, error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.record("FetchIssueByIdentifier")
	for _, issue := range ft.board {
		if strings.EqualFold(issue.Identifier, identifier) {
			// **識別子での照合は「誰が Status を書いたか」を持たない**（設計 3-54）。
			// 候補の取得と同じく、本物のクエリには timeline を足していない
			// （`TestStatusAuthor_識別子での照合はtimelineを要求しない`）。
			// **落とさないと、ここに頼った実装が書けてしまう。**
			issue.StatusChangedByAutomation = false
			issue.StatusChangedBy = ""
			return issue, true, nil
		}
	}
	return tracker.Issue{}, false, nil
}

// UpdateStatus は Status を書き換える。**書く前に取り直し、blockedStates なら書かない。**
//
// **本物と同じ形で返す**（`internal/tracker` の `UpdateStatus`）。
//
//	Previous … 書き込む直前のボードの値。**巡回で読んだ値ではない。**
//	            テストが SetState でボードだけを動かすと、ここに新しい値が入る
//	Reached … 目的の Status になったか。**既に同じ値で書き込みを省いた場合も真である**
//	Wrote   … 書き込みを実際に行ったか。**issue へ記録を書いてよいのはこれが真のときだけ**
func (ft *fakeTracker) UpdateStatus(_ context.Context, itemID, targetState string, blockedStates []string) (tracker.StatusWrite, error) {
	ft.waitAtUpdateGate()
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.record("UpdateStatus")
	if ft.updateErr != nil {
		return tracker.StatusWrite{}, ft.updateErr
	}
	for i := range ft.board {
		if ft.board[i].ID != itemID {
			continue
		}
		previous := ft.board[i].State
		for _, b := range blockedStates {
			if strings.EqualFold(b, previous) {
				return tracker.StatusWrite{Previous: previous}, nil
			}
		}
		if strings.EqualFold(strings.TrimSpace(previous), strings.TrimSpace(targetState)) {
			// **既に同じ値なら書きに行かない**（本物と同じ振る舞い）。
			return tracker.StatusWrite{Reached: true, Previous: previous}, nil
		}
		ft.board[i].State = targetState
		// **continuo が書いたものは自動化ではない**（設計 3-54。本物では
		// `gh auth token` の持ち主として書くので `actor.__typename` は `User` になる）。
		ft.board[i].StatusChangedByAutomation = false
		ft.board[i].StatusChangedBy = "octocat"
		return tracker.StatusWrite{Reached: true, Wrote: true, Previous: previous}, nil
	}
	return tracker.StatusWrite{}, nil
}

// FetchComments は issue のコメントを返す。
//
// selfLogin: continuo が使う gh の持ち主（設計 3-65）。**この偽物は投稿者で絞らず、
// 受け取った値を記録するだけである**（絞り込みそのものは internal/tracker の
// FetchComments の試験で確かめる）。
func (ft *fakeTracker) FetchComments(_ context.Context, issueNodeID string, _ config.TrackerProviderCommentsConfig, _ config.TrackerCommentsConfig, selfLogin string) ([]tracker.Comment, error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.record("FetchComments")
	ft.commentsSelfLogin = selfLogin
	if ft.commentsErr != nil {
		return nil, ft.commentsErr
	}
	out := make([]tracker.Comment, len(ft.comments[issueNodeID]))
	copy(out, ft.comments[issueNodeID])
	return out, nil
}

// PostComment は continuo 自身のコメントを足す。
func (ft *fakeTracker) PostComment(_ context.Context, issueNodeID, body, selfMarker string) (*tracker.Comment, error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.record("PostComment")
	if ft.postErrOnMarker != "" && strings.HasPrefix(body, ft.postErrOnMarker) {
		return nil, ft.postErrOnMarkerErr
	}
	if ft.postErr != nil {
		return nil, ft.postErr
	}
	c := tracker.Comment{
		ID: fmt.Sprintf("C_self_%d", len(ft.comments[issueNodeID])+1),
		// PostComment は self_marker を本文の先頭に付けて投稿する。
		Body: selfMarker + "\n" + body, IsSelf: true, CreatedAt: ft.now(),
	}
	ft.comments[issueNodeID] = append(ft.comments[issueNodeID], c)
	return &c, nil
}

// FetchAllComments は issue のコメントを1件残らず返す（設計 3-77a）。
//
// **印で外す処理を1つも通さない。**担当の持ち回りの判定はこれを読む。
//
// 戻り値の2つ目は「古い側を読み切れなかったか」である（issue #140）。
// `commentsTruncated` に真を入れておくと、そのまま返す。
func (ft *fakeTracker) FetchAllComments(
	_ context.Context, issueNodeID string, _ config.TrackerProviderCommentsConfig,
) ([]tracker.Comment, bool, error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.record("FetchAllComments")
	if ft.commentsErr != nil {
		return nil, false, ft.commentsErr
	}
	out := make([]tracker.Comment, len(ft.comments[issueNodeID]))
	copy(out, ft.comments[issueNodeID])
	return out, ft.commentsTruncated, nil
}

// FetchViewer はこのテスト用トラッカー mock が名乗る「gh の持ち主」を返す（設計 3-77b）。
func (ft *fakeTracker) FetchViewer(_ context.Context) (tracker.Assignee, error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.record("FetchViewer")
	if ft.viewerErr != nil {
		return tracker.Assignee{}, ft.viewerErr
	}
	return ft.viewer, nil
}

// AddAssignees は issue に担当者を書き足す（設計 3-77b）。**置き換えではない。**
func (ft *fakeTracker) AddAssignees(
	_ context.Context, issueNodeID string, assigneeIDs []string,
) ([]tracker.Assignee, error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.record("AddAssignees")
	if ft.assignErr != nil {
		return nil, ft.assignErr
	}
	for i := range ft.board {
		if issueNodeID != nodeIDOfIssue(ft.board[i]) {
			continue
		}
		for _, id := range assigneeIDs {
			if id == "" || hasAssigneeID(ft.board[i].Assignees, id) {
				continue
			}
			ft.board[i].Assignees = append(ft.board[i].Assignees,
				tracker.Assignee{ID: id, Login: ft.loginOfAssigneeID(id)})
		}
		ft.board[i].AssigneeCount = len(ft.board[i].Assignees)
		return append([]tracker.Assignee(nil), ft.board[i].Assignees...), nil
	}
	return nil, nil
}

// RemoveAssignees は issue から担当者を外す（設計 3-77c）。**名指しした1人だけを外す。**
func (ft *fakeTracker) RemoveAssignees(
	_ context.Context, issueNodeID string, assigneeIDs []string,
) ([]tracker.Assignee, error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.record("RemoveAssignees")
	if ft.assignErr != nil {
		return nil, ft.assignErr
	}
	for i := range ft.board {
		if issueNodeID != nodeIDOfIssue(ft.board[i]) {
			continue
		}
		kept := make([]tracker.Assignee, 0, len(ft.board[i].Assignees))
		for _, a := range ft.board[i].Assignees {
			if containsString(assigneeIDs, a.ID) {
				continue
			}
			kept = append(kept, a)
		}
		ft.board[i].Assignees = kept
		ft.board[i].AssigneeCount = len(kept)
		return append([]tracker.Assignee(nil), kept...), nil
	}
	return nil, nil
}

// loginOfAssigneeID は、そのノード ID に対応するログイン名を返す。
//
// **持ち主のノード ID なら持ち主のログイン名を返す。**それ以外は ID をそのまま名前にする
// （テストでは名前の中身より「誰の担当か」が変わることのほうが大事である）。
//
// **呼び出し側が ft.mu を保持したまま呼ぶ。**
func (ft *fakeTracker) loginOfAssigneeID(id string) string {
	if id == ft.viewer.ID {
		return ft.viewer.Login
	}
	return id
}

// hasAssigneeID は、一覧にそのノード ID の担当者がいるかを返す。
func hasAssigneeID(list []tracker.Assignee, id string) bool {
	for _, a := range list {
		if a.ID == id {
			return true
		}
	}
	return false
}

// containsString は一覧に値があるかを返す。
func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// nodeIDOfIssue は issue の下敷きの GitHub issue のノード ID を返す。
func nodeIDOfIssue(issue tracker.Issue) string {
	if v, ok := issue.NativeRef["issue_node_id"].(string); ok {
		return v
	}
	return ""
}

// SetViewer は、このテスト用トラッカー mock が名乗る「gh の持ち主」を差し替える。
func (ft *fakeTracker) SetViewer(v tracker.Assignee) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.viewer = v
}

// SetViewerError は FetchViewer が返すエラーを差し替える。
func (ft *fakeTracker) SetViewerError(err error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.viewerErr = err
}

// SetAssignError は AddAssignees / RemoveAssignees が返すエラーを差し替える。
func (ft *fakeTracker) SetAssignError(err error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.assignErr = err
}

// VerifyStatusOptions は Status の選択肢名の照合である。
func (ft *fakeTracker) VerifyStatusOptions(_ context.Context, _ config.TrackerConfig) error {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.record("VerifyStatusOptions")
	return ft.verifyErr
}

// ===== 本物の git を使うリポジトリ =====

// testRepo は本物の git で作ったテスト用のリポジトリである。
type testRepo struct {
	// Origin は push 先の bare リポジトリのパスである。
	Origin string
	// Dir は worktree を切る元になる clone のパスである。
	Dir string
	// Base は既定 branch の名前である。
	Base string
}

// runGit はテストの中で git を実行し、標準出力を返す。
//
// t: 呼び出し元のテスト。失敗したらテストを止める。
// dir: `-C` に渡す作業ディレクトリ。空文字なら付けない。
// args: git の引数。
// 戻り値: 標準出力（前後の空白を落としたもの）。
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("`git %s` に失敗した: %v\n%s", strings.Join(full, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// newTestRepo は bare リポジトリと、初期コミットを1つ持つ clone を作る。
//
// t: 呼び出し元のテスト。後始末を t.Cleanup に登録する。
// 戻り値: 作ったリポジトリ。既定 branch は "main"。
func newTestRepo(t *testing.T) *testRepo {
	t.Helper()

	root, err := os.MkdirTemp("", "orcgit")
	if err != nil {
		t.Fatalf("git 用の一時ディレクトリを作成できません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("一時ディレクトリを解決できません: %v", err)
	}

	origin := filepath.Join(resolvedRoot, "origin.git")
	runGit(t, "", "init", "--quiet", "--bare", "--initial-branch=main", origin)

	dir := filepath.Join(resolvedRoot, "repo")
	runGit(t, "", "clone", "--quiet", origin, dir)
	runGit(t, dir, "config", "user.email", "continuo@example.test")
	runGit(t, dir, "config", "user.name", "continuo test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("初期の中身\n"), 0o644); err != nil {
		t.Fatalf("初期ファイルを書けません: %v", err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "--quiet", "-m", "初期コミット")
	runGit(t, dir, "push", "--quiet", "-u", "origin", "main")

	return &testRepo{Origin: origin, Dir: dir, Base: "main"}
}

// ===== 検査対象の組み立て =====

// fixture はテスト1件ぶんの Orchestrator と、その周辺の値である。
type fixture struct {
	// Orc は検査対象である。
	Orc *orchestrator.Orchestrator
	// Tracker はテスト用トラッカー mockである。
	Tracker *fakeTracker
	// Herdr はテスト用herdr mock サーバである。
	Herdr *fakeHerdr
	// Workspace は本物の git を触る worktree の管理である。
	Workspace *workspace.Manager
	// Repo は本物の git のリポジトリである。
	Repo *testRepo
	// Config は Orchestrator に渡した設定である。
	Config config.Config
	// RuntimeDir は hook の socket と issue ごとの設定ファイルを置くディレクトリである。
	RuntimeDir string
	// SocketPath は hook を受ける socket の絶対パスである（実際には listen しない）。
	SocketPath string
	// WorktreeRoot は worktree の置き場所である。
	WorktreeRoot string
	// TranscriptRoot は会話の記録の置き場所の根である（本番の既定は `~/.claude/projects`）。
	//
	// **着手の段5b は、この根の直下1階層に `<セッション UUID>.jsonl` があるかを見る**
	// （設計 3-3b）。無ければ `--resume` を渡さないので、**再着手が復帰することを
	// 確かめるテストは、ここへ記録を置いてから走らせる**（`sessionTranscriptDir`）。
	TranscriptRoot string
	// Logs はログの出力先である。
	//
	// **排他つきの syncLog を使う。**run ごとの goroutine（turn ループ・finishRunAsync・
	// abandonRunAsync）がログを書いている最中にテスト本体が String() で読むので、
	// 排他を持たない strings.Builder を渡すと `-race` が競合を報告する。
	Logs *syncLog
	// Sessions は採番したセッション UUID を採番した順に持つ。
	Sessions []string
	// Timeline はテスト用トラッカー mockとテスト用herdr mock の呼び出しを混ぜた1本の並びである。
	Timeline *timeline

	// logMu は allowedLogs を守る。**テスト本体と run の goroutine が同時に触る。**
	logMu sync.Mutex
	// allowedLogs は、そのテストで出てよい WARN / ERROR の目印である。
	//
	// **continuo が動いている間のログは、テストの一部である。**実運用で出た欠陥
	// （消えた issue1件で取り直しが丸ごと落ちる）は、**テストのログにも出ていたのに
	// 誰も見ていなかった。**宣言していない WARN / ERROR が1行でも出たら、そのテストは落ちる。
	allowedLogs []string
}

// AllowLog は、このテストで出てよい WARN / ERROR を宣言する。
//
// **想定して起こしている失敗だけを通すためのものである。**部分一致で照合する。
// 引数は、そのログの msg か、添えられた値のうち特徴のある文字列を渡す。
//
// substrings: 出てよいログに必ず含まれる文字列。1つでも当たれば、その行は許される。
func (fx *fixture) AllowLog(substrings ...string) {
	fx.logMu.Lock()
	defer fx.logMu.Unlock()
	fx.allowedLogs = append(fx.allowedLogs, substrings...)
}

// unexpectedLogLines は、宣言されていない WARN / ERROR の行を返す。
//
// 戻り値: 想定外だった行。1件も無ければ長さ0。
func (fx *fixture) unexpectedLogLines() []string {
	fx.logMu.Lock()
	allowed := append([]string(nil), fx.allowedLogs...)
	fx.logMu.Unlock()

	var bad []string
	for _, line := range strings.Split(fx.Logs.String(), "\n") {
		if !strings.Contains(line, "level=WARN") && !strings.Contains(line, "level=ERROR") {
			continue
		}
		// **止められたことが原因の失敗は、テストでは見ない。**
		//
		// 検査は `orc.Close` のあとに走る（そこでしか見えない欠陥があるため）。
		// **そのとき走行中の goroutine は、片付けの途中で ctx を切られる。**
		// どの呼び出しが切られるかは実行のたびに変わるので、1件ずつ宣言しても収束しない
		// （実際、`agent.start` → `agent.prompt` → `agent.list` → `worktree.open` と
		// CI に4回見つけさせた）。
		//
		// **実運用でこれが出ないようにするのは、テストではなく実装の仕事である。**
		// `internal/orchestrator/comment.go` の `stoppedWhileRecovering` がそれをしている。
		if strings.Contains(line, "[canceled]") || strings.Contains(line, "context canceled") {
			continue
		}
		ok := false
		for _, a := range allowed {
			if a != "" && strings.Contains(line, a) {
				ok = true
				break
			}
		}
		if !ok {
			bad = append(bad, line)
		}
	}
	return bad
}

// fixtureOptions は newFixture の任意の入力である。
type fixtureOptions struct {
	// Mutate は設定を書き換える関数である。nil なら既定のまま。
	Mutate func(cfg *config.Config)
	// Untrusted を真にすると、リポジトリを信頼登録しない
	// （dispatch の段0 で必ず弾かれる状態を作る）。
	Untrusted bool
	// Now は現在時刻を返す関数である。nil なら time.Now を使う。
	Now func() time.Time
	// PromptTemplate は1回目のプロンプトのテンプレートである。空なら samplePromptTemplate。
	PromptTemplate string
	// GHAuthCheck は `gh` の認証の検査である。nil なら検査しない。
	GHAuthCheck func(ctx context.Context) error
	// GHLogin は「continuo が使う gh の持ち主」を取る関数である（設計 3-65）。
	//
	// **nil なら testGHLogin を返す偽物を渡す。**渡さないと本物の `gh` が起動する。
	GHLogin func(ctx context.Context) (string, error)
	// RateLimit は枠の読み取りである。nil なら枠の判定を行わない。
	RateLimit *ratelimit.Reader
	// TranscriptRoot は hook が渡す transcript_path を受け入れる根である。
	// 空なら一時ディレクトリの根（tempRoot）を使う。
	TranscriptRoot string
	// ContinuoPath は hook のコマンド行に書く実行ファイルのパスである。
	// 空なら `/opt/continuo/bin/continuo` を使う。
	ContinuoPath string
}

// newFixture はテスト用の Orchestrator を組み立てる。
//
// 組み立てるのは次の4つである。
//
//	テスト用herdr mock socket サーバ  … 実 herdr は使わない
//	テスト用トラッカー mock            … 本番のボードへは接続しない
//	本物の git のリポジトリ    … worktree の作成と削除はmockでは確かめられない
//	`~/.claude.json`         … 信頼の検査が読む（読むだけ。書き換えない）
//
// t: 呼び出し元のテスト。
// opts: 任意の入力。
// 戻り値: 組み立てた fixture。
func newFixture(t *testing.T, opts fixtureOptions) *fixture {
	t.Helper()

	repo := newTestRepo(t)
	fake := newFakeHerdr(t)
	fake.SetGitRepoDir(repo.Dir)
	nowFunc := opts.Now
	if nowFunc == nil {
		nowFunc = time.Now
	}
	ft := newFakeTracker(nowFunc)

	// **トラッカーと herdr の呼び出しを1本の並びに混ぜる**（着手の段の前後関係を比べるため）。
	tl := newTimeline()
	ft.timeline = tl
	fake.mu.Lock()
	fake.timeline = tl
	fake.mu.Unlock()

	tmp, err := os.MkdirTemp("", "orcfix")
	if err != nil {
		t.Fatalf("一時ディレクトリを作成できません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	tmp, err = filepath.EvalSymlinks(tmp)
	if err != nil {
		t.Fatalf("一時ディレクトリを解決できません: %v", err)
	}

	worktreeRoot := filepath.Join(tmp, "wt")
	runtimeDir := filepath.Join(tmp, "rt")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("実行時ディレクトリを作成できません: %v", err)
	}
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("ホームディレクトリを作成できません: %v", err)
	}

	// 信頼の検査が読む ~/.claude.json（**読むだけで書き換えない**）。
	toplevel := runGit(t, repo.Dir, "rev-parse", "--path-format=absolute", "--show-toplevel")
	trustDoc := map[string]any{
		"projects": map[string]any{
			toplevel: map[string]any{"hasTrustDialogAccepted": !opts.Untrusted},
		},
	}
	trustJSON, err := json.Marshal(trustDoc)
	if err != nil {
		t.Fatalf("~/.claude.json を JSON 化できません: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), trustJSON, 0o600); err != nil {
		t.Fatalf("~/.claude.json を書けません: %v", err)
	}

	cfg := *config.DefaultConfig()
	cfg.Workspace.Root = worktreeRoot
	// テストが実時間で待つ長さを短くする（判定の意味は変えない）。
	cfg.Claude.SettleMs = 50
	cfg.Claude.PollWaitMs = 200
	// **画面が止まったとみなすまでの時間。**短くすると、待たせるだけのテストが
	// 軒並み stall と判定されてしまうので長めに取る。stall を見たいテストは
	// Mutate で短くすること。
	cfg.Claude.TurnTimeoutMs = 600000
	cfg.Herdr.ReadTimeoutMs = 2000
	cfg.Herdr.StartupTimeoutMs = 2000
	cfg.Polling.IntervalMs = 3600000
	// 枠の判定は既定で行わない（usage API を1回も叩かない）。
	cfg.RateLimit.Source = "none"
	// **入札の締め切りを待たない**（設計 3-77）。既定の3分を待つと、
	// **どのテストも1回の巡回では着手できない。**
	//
	// **締め切りと回の区切りは、ここを Mutate で書き換えたテストが見ている。**
	// 既定のままだと締め切りの待ちも回の区切りも通らないので、**この2本を消してはならない。**
	//
	//	TestHandoff_締め切りの前は担当者にならない                    締め切りを待つあいだは担当者にならないこと
	//	TestHandoff_古い入札が残っていても締め切りをまたいで担当者が決まる  巡回をまたいでも入札が増えず、締め切りの後に勝つこと
	cfg.Tracker.Provider.Handoff.BidWindowMs = 0
	if opts.Mutate != nil {
		opts.Mutate(&cfg)
	}

	promptTemplate := opts.PromptTemplate
	if promptTemplate == "" {
		promptTemplate = samplePromptTemplate
	}

	logs := &syncLog{}
	logger := slog.New(slog.NewTextHandler(io.Writer(logs), &slog.HandlerOptions{Level: slog.LevelDebug}))

	settingsRoot := filepath.Join(runtimeDir, hookserver.IssuesDirName)
	mgr, err := workspace.New(workspace.Options{
		Config:       cfg,
		Herdr:        fake.Client(),
		Logger:       logger,
		Now:          nowFunc,
		HomeDir:      home,
		GhqList:      func(context.Context, string, string) (string, error) { return repo.Dir, nil },
		SettingsRoot: settingsRoot,
	})
	if err != nil {
		t.Fatalf("workspace.New に失敗した: %v", err)
	}

	fx := &fixture{
		Tracker:      ft,
		Herdr:        fake,
		Workspace:    mgr,
		Repo:         repo,
		Config:       cfg,
		RuntimeDir:   runtimeDir,
		SocketPath:   filepath.Join(runtimeDir, "hooks.sock"),
		WorktreeRoot: worktreeRoot,
		Logs:         logs,
		Timeline:     tl,
	}

	transcriptRoot := opts.TranscriptRoot
	if transcriptRoot == "" {
		transcriptRoot = tempRoot(t)
	}
	fx.TranscriptRoot = transcriptRoot
	continuoPath := opts.ContinuoPath
	if continuoPath == "" {
		continuoPath = "/opt/continuo/bin/continuo"
	}

	var sessionMu sync.Mutex
	orc, err := orchestrator.New(orchestrator.Options{
		Config: cfg,
		// **1回目に送る文面は、断片の並びとして渡す**（設計 5-3c）。
		// テストが与えるのは1枚のテンプレートなので、それを WORKFLOW.md の本文の
		// 位置に置いた形で組み立てる（組み込みの前半と後半が前後に付く）。
		Prompt:         prompt.Build(promptTemplate, "/tmp/WORKFLOW.md"),
		Tracker:        ft,
		Herdr:          fake.Client(),
		Workspace:      mgr,
		RateLimit:      opts.RateLimit,
		HookSocketPath: fx.SocketPath,
		ContinuoPath:   continuoPath,
		// **機械の名前を固定する**（設計 3-77）。走らせる機械によって
		// 入札のコメントの中身が変わらないようにする。
		HostName: testHostName,
		// **テストの transcript は一時ディレクトリに置く。**hook が渡す
		// transcript_path は許可された根の内側だけを受け入れるので、根をそこへ向ける
		// （本番の既定は `~/.claude/projects`）。
		TranscriptRoot: transcriptRoot,
		Logger:         logger,
		Now:            nowFunc,
		GHAuthCheck:    opts.GHAuthCheck,
		// **本物の `gh` を起動させない**（設計 3-65）。渡さないと `gh api user` が走る。
		GHLogin: ghLoginForTest(opts.GHLogin),
		NewSessionUUID: func() (string, error) {
			sessionMu.Lock()
			defer sessionMu.Unlock()
			id := fmt.Sprintf("session-%d", len(fx.Sessions)+1)
			fx.Sessions = append(fx.Sessions, id)
			return id, nil
		},
	})
	if err != nil {
		t.Fatalf("orchestrator.New に失敗した: %v", err)
	}
	fx.Orc = orc

	// **そのテストが意図して起こす失敗を、expected_warnings_test.go の表から読む。**
	// サブテストは親の名前で引く（`t.Run` の中でも同じ状況を作るため）。
	// **サブテストは、自分の名前と親の名前の両方で引く。**
	// 親の名前だけで引くと、1つのサブテストのための宣言が、
	// **同じ関数の全サブテストで有効になってしまう。**
	name := t.Name()
	fx.AllowLog(expectedWarnings[name]...)
	if i := strings.Index(name, "/"); i >= 0 {
		fx.AllowLog(expectedWarnings[name[:i]]...)
	}
	// **打ち切りの連鎖は既定で許す。**どれが出るかはテストが終わる時点の段で変わり、
	// 1件ずつ宣言しても収束しない。**この連鎖はログではなく、回数と Status で守る**
	// （expected_warnings_test.go の abandonChain の説明を見よ）。
	fx.AllowLog(abandonChain...)
	// **テストが終わったあとの片付けで出るものも許す**（shutdownNoise の説明を見よ）。
	fx.AllowLog(shutdownNoise...)

	// **continuo が動いている間のログを、テストの一部として検査する。**
	//
	// **`orc.Close` より先に登録する。**t.Cleanup は後入れ先出しなので、
	// 先に登録したこれは**あとから**走る。つまり `Close` が `wg.Wait()` で
	// 走行中の goroutine を待ち終えたあとのログまで見られる。
	//
	// 逆にすると、**片付け・撤退・pane を閉じる経路のログを1行も見ない。**
	// そこはいちばん欠陥が出やすいところである。
	t.Cleanup(func() {
		bad := fx.unexpectedLogLines()
		if len(bad) == 0 {
			return
		}
		t.Errorf("宣言していない WARN / ERROR が %d 行出ました。"+
			"想定して起こしている失敗なら fx.AllowLog(\"目印\") で宣言してください。"+
			"想定していないなら、それは実装の欠陥です。\n%s",
			len(bad), strings.Join(bad, "\n"))
	})
	t.Cleanup(orc.Close)
	return fx
}

// samplePromptTemplate は 5-3 の本文を短くしたテンプレートである。
// **issue の本文とコメントは入れない**（設計 3-29）。
const samplePromptTemplate = `{{.issue.identifier}} を実装してください。

    gh issue view {{.issue.url}} --comments

作業の区切りがついたら CONTINUO-STATUS: の行を1行書いてください。
{{if .attempt}}この作業は {{.attempt}} 回目の試行です。{{end}}`

// sampleIssue はテストで使う issue を作る。
//
// number: issue 番号。
// state: ボード上の Status。
// 戻り値: `octocat/hello-world#<number>` の issue。
func sampleIssue(number int, state string) tracker.Issue {
	url := fmt.Sprintf("https://github.com/octocat/hello-world/issues/%d", number)
	return tracker.Issue{
		ID:         fmt.Sprintf("PVTI_item%d", number),
		Identifier: fmt.Sprintf("octocat/hello-world#%d", number),
		Title:      fmt.Sprintf("テスト用の issue %d", number),
		State:      state,
		URL:        &url,
		Labels:     []string{"bug"},
		NativeRef: map[string]any{
			"issue_node_id":  fmt.Sprintf("I_node%d", number),
			"content_type":   "ISSUE",
			"default_branch": "main",
		},
		Dispatchable: true,
		Owner:        "octocat",
		Repo:         "hello-world",
		Number:       number,
	}
}

// stopEvent は「background_tasks が空配列の Stop」を作る（turn の終わりの起点）。
//
// sessionID: セッション UUID。
// transcriptPath: transcript のパス。
// promptID: prompt_id。
// 戻り値: hook のイベント。
func stopEvent(sessionID, transcriptPath, promptID string) hookserver.HookEvent {
	empty := []hookserver.BackgroundTask{}
	return hookserver.HookEvent{
		HookEventName:   "Stop",
		SessionID:       sessionID,
		TranscriptPath:  transcriptPath,
		PromptID:        promptID,
		BackgroundTasks: &empty,
	}
}

// runningShellStopEvent は「background_tasks にバックグラウンドの shell が載っている Stop」
// を作る（設計 3-2 / 1-7）。
//
// **これは Claude Code 自身の「まだ動いています」という申告である。**
// `type` が `shell` なので、走行中の subagent の印（設計 3-11）はこれでは動かない。
// **`Stop` の中身だけで「まだ動いている」と判定できることを確かめるためにこの形にしている**
// （subagent が載る形は turn_test.go の runningStopEvent が作る）。
//
// sessionID: セッション UUID。
// transcriptPath: transcript のパス。
// promptID: prompt_id。
// 戻り値: hook のイベント。
func runningShellStopEvent(sessionID, transcriptPath, promptID string) hookserver.HookEvent {
	running := []hookserver.BackgroundTask{{
		ID:          "bmr1ksf9i",
		Type:        "shell",
		Status:      "running",
		Description: "テストの実行",
		Command:     "sleep 45",
	}}
	ev := stopEvent(sessionID, transcriptPath, promptID)
	ev.BackgroundTasks = &running
	return ev
}

// taskNotificationEvent は `<task-notification>` の UserPromptSubmit を作る
// （**これが来たら turn は続いている**）。
//
// sessionID: セッション UUID。
// taskID: 完了した処理の識別子。
// 戻り値: hook のイベント。
func taskNotificationEvent(sessionID, taskID string) hookserver.HookEvent {
	return hookserver.HookEvent{
		HookEventName: "UserPromptSubmit",
		SessionID:     sessionID,
		PromptID:      "prompt-" + taskID,
		Prompt: fmt.Sprintf("<task-notification><task-id>%s</task-id><status>completed</status></task-notification>",
			taskID),
	}
}

// sessionTranscriptDir は、会話の記録を置くディレクトリを記録の根の直下に1つ作る。
//
// **着手の段5b は `<記録の根>/*/<セッション UUID>.jsonl` を探す**（設計 3-3b）。
// **`t.TempDir()` は使えない。**あれは根から2階層下（`<根>/<テスト名+乱数>/001`）に掘るので、
// **直下1階層しか見ない検査には1件も当たらず、再着手が `--resume` を渡さなくなる。**
//
// **記録を置くテストは、`fixtureOptions.TranscriptRoot` に `t.TempDir()` を渡すこと。**
// **既定の根は機械全体の一時ディレクトリである**（`tempRoot`）。そこを根にすると、
// **`go test` が timeout で殺されて `t.Cleanup` が走らなかった前の実行の置き土産が見える。**
// テストが書いていない記録で `--resume` の判定が変わり、**守りの向きを逆にしても緑のまま通る。**
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture（記録の根を持っている）。
// 戻り値: 作ったディレクトリの絶対パス。
func sessionTranscriptDir(t *testing.T, fx *fixture) string {
	t.Helper()
	dir, err := os.MkdirTemp(fx.TranscriptRoot, "continuo-transcripts-")
	if err != nil {
		t.Fatalf("会話の記録の置き場所を作れません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// seedSessionTranscript は、そのセッションの会話の記録を、着手が見つけられる場所へ置く。
//
// **着手の段5b は、記録が無い UUID へ `--resume` を投げない**（設計 3-3b）。
// **再着手が復帰することを確かめるテストは、これを dispatch の前に呼ぶこと。**
// 呼ばないと、身元ファイルに UUID が入っていても新しいセッションで始まる。
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
// sessionUUID: 記録を置くセッションの UUID。
// lines: 記録の中身。**空にしない**（大きさが0のファイルは「記録が無い」とみなされる）。
// 戻り値: 置いた記録の絶対パス。
func seedSessionTranscript(t *testing.T, fx *fixture, sessionUUID string, lines []any) string {
	t.Helper()
	return writeTranscript(t, sessionTranscriptDir(t, fx), sessionUUID+".jsonl", lines)
}

// writeTranscript はテスト用の transcript の JSONL を書く。
//
// **形は設計 3-25 / 3-15 の実測に合わせてある**（`promptSource` / `isSidechain` /
// `message.content[].text` / `requestId` / `message.usage`）。
//
// t: 呼び出し元のテスト。
// dir: 書き出すディレクトリ。
// name: ファイル名。
// lines: 1行ずつの JSON 化前の値。
// 戻り値: 書いたファイルの絶対パス。
func writeTranscript(t *testing.T, dir, name string, lines []any) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("transcript の置き場所を作れません: %v", err)
	}
	path := filepath.Join(dir, name)
	var b strings.Builder
	for _, line := range lines {
		encoded, err := json.Marshal(line)
		if err != nil {
			t.Fatalf("transcript の行を JSON 化できません: %v", err)
		}
		b.Write(encoded)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("transcript を書けません: %v", err)
	}
	return path
}

// typedUserLine は「turn の頭」（`promptSource == "typed"`）の user 行を作る。
//
// promptID: prompt_id。
// text: 本文。
// 戻り値: transcript の1行。
func typedUserLine(promptID, text string) any {
	return map[string]any{
		"type": "user", "promptSource": "typed", "promptId": promptID, "isSidechain": false,
		"message": map[string]any{"content": text},
	}
}

// assistantLine は assistant の行を作る。
//
// requestID: API 応答の識別子（トークンの重複排除に使う）。
// text: text ブロックの本文。
// sidechain: subagent の会話なら true。
// 戻り値: transcript の1行。
func assistantLine(requestID, text string, sidechain bool) any {
	return map[string]any{
		"type": "assistant", "isSidechain": sidechain, "requestId": requestID,
		"message": map[string]any{
			"content": []any{map[string]any{"type": "text", "text": text}},
			"usage": map[string]any{
				"input_tokens": 10, "cache_creation_input_tokens": 20,
				"cache_read_input_tokens": 30, "output_tokens": 40,
			},
		},
	}
}

// waitFor は cond が真になるまで最大 d だけ待つ。
//
// t: 呼び出し元のテスト。**テスト本体の goroutine から呼ぶこと。**
// d: 待つ長さの上限。
// message: 失敗したときに出す説明。
// cond: 判定する関数。
// WaitRunsDrained は、走行中の run が無くなるまで待つ。
//
// **Status が変わったことだけを待ってはならない。**
// orchestrator は Status を書いてから、コメントを確かめ、worker を止め、worktree を片付ける
// （`finishRunClaimed` / `failRun` の順序）。**Status で待つと、その後始末が起きる前に
// `Methods()` を読んでしまう。**
//
// 手元では間に合っていたが、**CI の `-race` で隙間が開いて落ちた**
// （`TestTurn_blockedが返ったらescを送ってから人間へ渡す` が
// 「人間へ渡すときに worker を止めていない」で落ちた。設計 6-9）。
//
// **run が `o.runs` から外れるのは最後である**（`release`）。だからここが空になれば、
// 後始末は全部済んでいる。
//
// t: 呼び出し元のテスト。
// d: 待つ上限。
func (fx *fixture) WaitRunsDrained(t *testing.T, d time.Duration) {
	t.Helper()
	waitFor(t, d, "走行中の run が無くなる（後始末まで終わる）", func() bool {
		return len(fx.Orc.RunViews()) == 0
	})
}

func waitFor(t *testing.T, d time.Duration, message string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s（%s 以内に条件を満たしませんでした）", message, d)
}

// appendLine はファイルの末尾に1行を足す（壊れた行を混ぜる検査に使う）。
//
// path: 対象のファイル。
// line: 足す1行（改行は付けなくてよい）。
// 戻り値: 書けなかった場合のエラー。
func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(line + "\n")
	return err
}

// tempRoot は `t.TempDir()` が作られる根を、シンボリックリンクを解いた形で返す。
//
// **macOS では `/var/folders/...` が `/private/var/folders/...` の symlink である。**
// 解いておかないと、`transcript_path` の突き合わせが一致しない。
//
// t: 呼び出し元のテスト。
// 戻り値: 解決した一時ディレクトリの根。
func tempRoot(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatalf("一時ディレクトリの根を解決できません: %v", err)
	}
	return resolved
}
