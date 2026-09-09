// Package abandon_test は `continuo abandon`（設計 3-4 の段1〜段5）の振る舞いを確かめる。
//
// **本番のカンバン（project #3）へは1リクエストも送らない。**カンバンは
// abandon.Tracker を満たす偽の実装（fakeTracker）に差し替え、GraphQL の口は使わない。
// **実 herdr には繋がない。**`net.Listen("unix", ...)` でテスト用herdr mock を立て、
// pane の一覧・worktree の open / remove をそこに答えさせる。
// **git は本物を使う。**一時ディレクトリに bare リポジトリと clone を作り、そこから
// worktree を切る。未コミットの変更の判定・worktree と branch の削除は、mockでは確かめられない。
// **二重起動の判定も本物を使う。**`internal/lock` の flock をテストの中で先に掴み、
// 「continuo が動いている」状態を作る。
package abandon_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/abandon"
	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/lock"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// ===== テスト用herdr mock socket サーバ =====

// fakeHerdr は herdr の socket API の代わりに使う偽のサーバである。
//
// **答えるのは5つのメソッドだけである。**`pane.list`（段1 の待ちと段3 の一覧）、
// `worktree.open`（片付けの前の検算）、`worktree.remove`（片付け本体）、
// `workspace.list` と `workspace.close`（`worktree.open` が答えられなかったときの
// 探し直しと、閉じ残した workspace の後始末）。
type fakeHerdr struct {
	// SocketPath は listen している socket の絶対パスである。
	SocketPath string

	// listener は受け口である。**止めて「herdr が落ちている」状態を作るのに持っておく。**
	listener net.Listener

	mu sync.Mutex
	// repoDir は worktree を切った clone の絶対パスである。
	// **`worktree.remove` を受けたときに、本物の herdr と同じく実体を消すのに使う。**
	repoDir string
	// workspaceIDs は開いた worktree のパスから herdr workspace の ID への対応である。
	// **同じパスには同じ ID を返す**（Prepare と Cleanup が同じ worktree を開くため）。
	workspaceIDs map[string]string
	// paths は herdr workspace の ID から worktree のパスへの逆引きである。
	paths map[string]string
	// paneListScript は `pane.list` が何回目の呼び出しで何を返すかを決める。
	// nil なら常に空（pane は無い）。
	paneListScript func(call int) []map[string]any
	// paneListCalls は `pane.list` を受けた回数である。
	paneListCalls int
	// methods は受け取ったメソッド名を受け取った順に記録したものである。
	methods []string
	// removeErr は `worktree.remove` にエラーを返させるための応答である。
	// nil なら消す（成功を返す）。
	removeErr *rpcError
}

// rpcError は herdr が返すエラー応答である。
type rpcError struct {
	// Code は herdr のエラーコードである。
	Code string
	// Message は人間に見せる理由である。
	Message string
}

// newFakeHerdr はテスト用herdr mock を1本立てる。
//
// t: 呼び出し元のテスト。socket の後始末を t.Cleanup に登録する。
// dir: socket を置くディレクトリ（**短く保つこと。**macOS の上限は103バイト）。
// repoDir: worktree を切った clone の絶対パス。
// 戻り値: 起動したテスト用herdr mock。
func newFakeHerdr(t *testing.T, dir, repoDir string) *fakeHerdr {
	t.Helper()

	socketPath := filepath.Join(dir, "h.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("テスト用herdr mock の socket を bind できません（%s）: %v", socketPath, err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	fh := &fakeHerdr{
		SocketPath:   socketPath,
		listener:     ln,
		repoDir:      repoDir,
		workspaceIDs: map[string]string{},
		paths:        map[string]string{},
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go fh.serve(conn)
		}
	}()
	return fh
}

// Client は偽サーバを向いた本物の herdr クライアントを返す。
//
// 戻り値: テスト用herdr mock へ繋ぐクライアント。
func (fh *fakeHerdr) Client() *herdr.Client {
	return herdr.New(fh.SocketPath, herdr.Timeouts{Read: 5 * time.Second, Startup: 5 * time.Second})
}

// SetPaneListScript は `pane.list` の応答を、呼ばれた回数で決める関数を登録する。
//
// **段1 の「pane が消えるまで待つ」を確かめるために要る。**回数で応答を変えられないと、
// 「待っているうちに消えた」も「いつまでも消えない」も作れない。
//
// fn: 何回目（1 始まり）の呼び出しで、どの pane を返すかを決める関数。
func (fh *fakeHerdr) SetPaneListScript(fn func(call int) []map[string]any) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.paneListScript = fn
}

// PaneListCalls は `pane.list` を受けた回数を返す。
//
// 戻り値: 受けた回数。
func (fh *fakeHerdr) PaneListCalls() int {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return fh.paneListCalls
}

// Methods は受け取ったメソッド名を受け取った順に返す。
//
// 戻り値: メソッド名の並び。
func (fh *fakeHerdr) Methods() []string {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	out := make([]string, len(fh.methods))
	copy(out, fh.methods)
	return out
}

// serve は1本の接続を処理する。
//
// **接続ごとの goroutine から呼ばれるので t.Fatalf を使ってはならない。**
//
// conn: 受けた接続。
func (fh *fakeHerdr) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return
	}
	var req struct {
		ID     string         `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(line, &req); err != nil {
		return
	}

	result, failure := fh.respond(req.Method, req.Params)
	var resp map[string]any
	if failure == nil {
		resp = map[string]any{"id": req.ID, "result": result}
	} else {
		resp = map[string]any{"id": req.ID,
			"error": map[string]any{"code": failure.Code, "message": failure.Message}}
	}
	encoded, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_, _ = conn.Write(append(encoded, '\n'))
}

// SetWorktreeRemoveError は `worktree.remove` にエラーを返させる。
//
// **片付けそのものに失敗したときの経路を作るために要る。**herdr が消せなければ
// worktree は残るので、そのあとで Status を動かしてはならない。
//
// code: 返すエラーコード。
// message: 返す理由。
func (fh *fakeHerdr) SetWorktreeRemoveError(code, message string) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.removeErr = &rpcError{Code: code, Message: message}
}

// respond はメソッド名に応じた result を組み立てる。
//
// method: 呼ばれたメソッド名。
// params: 受け取った params。
// 戻り値の1つ目: 返す result。
// 戻り値の2つ目: エラーを返す場合のエラー応答（成功なら nil）。
func (fh *fakeHerdr) respond(method string, params map[string]any) (map[string]any, *rpcError) {
	fh.mu.Lock()
	fh.methods = append(fh.methods, method)
	removeErr := fh.removeErr
	fh.mu.Unlock()

	switch method {
	case herdr.MethodPaneList:
		return fh.paneList(), nil
	case herdr.MethodWorktreeOpen:
		return fh.worktreeOpen(params), nil
	case herdr.MethodWorktreeRemove:
		if removeErr != nil {
			return nil, removeErr
		}
		return fh.worktreeRemove(params), nil
	case herdr.MethodWorkspaceList:
		return fh.workspaceList(), nil
	case herdr.MethodWorkspaceClose:
		return fh.workspaceClose(params), nil
	default:
		return nil, &rpcError{Code: "unknown_method", Message: method}
	}
}

// workspaceList は `workspace.list` の応答を組み立てる。
//
// **開いたままの workspace だけを返す。**continuo は「このパスを開いている workspace は
// どれか」をここに答えさせて、閉じる宛先を決める（身元ファイルの値は使わない）。
//
// 戻り値: 変種 workspace_list の result。
func (fh *fakeHerdr) workspaceList() map[string]any {
	fh.mu.Lock()
	defer fh.mu.Unlock()

	workspaces := []map[string]any{}
	for id, path := range fh.paths {
		workspaces = append(workspaces, map[string]any{
			"workspace_id": id,
			"worktree": map[string]any{
				"repo_root":          fh.repoDir,
				"checkout_path":      path,
				"is_linked_worktree": true,
			},
		})
	}
	return map[string]any{"type": "workspace_list", "workspaces": workspaces}
}

// workspaceClose は `workspace.close` の応答を組み立て、**一覧からもその workspace を落とす。**
//
// params: 受け取った params（workspace_id を読む）。
// 戻り値: 変種 ok の result。
func (fh *fakeHerdr) workspaceClose(params map[string]any) map[string]any {
	id, _ := params["workspace_id"].(string)

	fh.mu.Lock()
	if path, ok := fh.paths[id]; ok {
		delete(fh.paths, id)
		delete(fh.workspaceIDs, path)
	}
	fh.mu.Unlock()

	return map[string]any{"type": "ok"}
}

// OpenWorkspaceIDs は、まだ閉じていない workspace の ID を返す。
//
// **「herdr の workspace が残っていないこと」を確かめるのに使う。**
//
// 戻り値: 開いたままの workspace の ID（順序は決まっていない）。
func (fh *fakeHerdr) OpenWorkspaceIDs() []string {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	ids := make([]string, 0, len(fh.paths))
	for id := range fh.paths {
		ids = append(ids, id)
	}
	return ids
}

// paneList は `pane.list` の応答を組み立てる。
//
// 戻り値: 登録した関数が返した pane を載せた result。
func (fh *fakeHerdr) paneList() map[string]any {
	fh.mu.Lock()
	fh.paneListCalls++
	call := fh.paneListCalls
	script := fh.paneListScript
	fh.mu.Unlock()

	panes := []map[string]any{}
	if script != nil {
		panes = script(call)
	}
	return map[string]any{"type": "pane_list", "panes": panes}
}

// worktreeOpen は `worktree.open` の応答を組み立てる。
//
// **同じパスには同じ workspace の ID を返す。**片付けは「このパスを開いている
// workspace はどれか」を herdr に答えさせて検算する（設計 3-9 の段3）ので、
// Prepare のときと違う ID を返すと、その検算が意味を持たなくなる。
//
// params: 受け取った params（path を読む）。
// 戻り値: 変種 worktree_opened の result。
func (fh *fakeHerdr) worktreeOpen(params map[string]any) map[string]any {
	path, _ := params["path"].(string)

	fh.mu.Lock()
	id, ok := fh.workspaceIDs[path]
	if !ok {
		id = fmt.Sprintf("w%d", len(fh.workspaceIDs)+1)
		fh.workspaceIDs[path] = id
		fh.paths[id] = path
	}
	fh.mu.Unlock()

	return map[string]any{
		"type":      "worktree_opened",
		"workspace": map[string]any{"workspace_id": id},
		"tab":       map[string]any{"tab_id": id + ":t1"},
		"root_pane": map[string]any{"pane_id": id + ":p1", "workspace_id": id},
		"worktree":  map[string]any{"path": path},
	}
}

// worktreeRemove は `worktree.remove` の応答を組み立て、**worktree の実体も消す。**
//
// **本物の herdr は worktree を消す。**消さないと `git branch -D` が
// 「checkout 中の branch は消せない」で必ず失敗し、片付けの段4 を検証できない。
//
// **本物と同じく `git worktree remove` に消させ、その成否を隠す**（**成功を返す**）。
// worktree の `.git` が壊れていると git は
// `validation failed, cannot remove working tree` で断るので（実測: 2026-08-25）、
// **「消したと答えたのに実体が残っている」状態がここで再現される**（issue #23）。
// **実体が消えたときだけ workspace も閉じる**（本物の `worktree.remove` は
// worktree の workspace を閉じる）。
//
// params: 受け取った params（workspace_id を読む）。
// 戻り値: 変種 worktree_removed の result。
func (fh *fakeHerdr) worktreeRemove(params map[string]any) map[string]any {
	id, _ := params["workspace_id"].(string)

	fh.mu.Lock()
	path := fh.paths[id]
	repoDir := fh.repoDir
	fh.mu.Unlock()

	if path != "" && repoDir != "" {
		// **接続ごとの goroutine なので t.Fatalf は使わない。**
		_ = exec.Command("git", "-C", repoDir, "worktree", "remove", "--force", path).Run()
	}
	if _, err := os.Lstat(path); path != "" && os.IsNotExist(err) {
		fh.mu.Lock()
		delete(fh.paths, id)
		delete(fh.workspaceIDs, path)
		fh.mu.Unlock()
	}
	return map[string]any{
		"type": "worktree_removed", "workspace_id": id, "path": path, "forced": true,
	}
}

// ===== 偽のカンバン =====

// statusUpdate はカンバンへの Status の書き込み1件である。
type statusUpdate struct {
	// ItemID は書き込み先の project item の ID である。
	ItemID string
	// State は書き込んだ Status の値である。
	State string
}

// fakeTracker は abandon.Tracker を満たす偽のカンバンである。
//
// **本番のカンバンへ1リクエストも送らないためにこれを使う。**Status の読み書きが
// 何回・どの値で起きたかを記録する。
type fakeTracker struct {
	mu sync.Mutex
	// state は FetchIssueByIdentifier が返す現在の Status である。
	state string
	// itemID は FetchIssueByIdentifier が返す project item の ID である。
	itemID string
	// found は issue がカンバンに載っているかどうかである。
	found bool
	// written は UpdateStatus が返す「書けたかどうか」である。
	written bool
	// rejectFrom は「何回目の UpdateStatus から書けなかったことにするか」である
	// （1 始まり。0 なら落とさない）。
	rejectFrom int
	// bootstraps は Bootstrap を受けた回数である。
	bootstraps int
	// fetches は FetchIssueByIdentifier が受け取った identifier である。
	fetches []string
	// updates は UpdateStatus で書き込んだ内容である。
	updates []statusUpdate
	// options は VerifyKnownStates が「カンバンにある」と答える Status の選択肢である。
	options []string
	// verifies は VerifyKnownStates が受け取った値を受け取った順に並べたものである。
	verifies [][]string
}

// newFakeTracker は「カンバンに載っていて、Status を書ける」偽のカンバンを作る。
//
// state: FetchIssueByIdentifier が返す現在の Status。
// 戻り値: 偽のカンバン。
func newFakeTracker(state string) *fakeTracker {
	return &fakeTracker{
		state:   state,
		itemID:  "PVTI_test",
		found:   true,
		written: true,
		// **本物のカンバンと同じく、選択肢に無い値は断る。**ここを「何でも通す」に
		// しておくと、`--to` の綴り違いを消す前に弾く検査が空振りする。
		options: []string{"Ice Box", "Ready", "In Progress", "Blocked", "Done"},
	}
}

// SetStatusOptions は VerifyKnownStates が「カンバンにある」と答える選択肢を差し替える。
//
// options: カンバンにある Status の選択肢。
func (ft *fakeTracker) SetStatusOptions(options ...string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.options = options
}

// VerifyKnownStates は、渡された値がすべて選択肢にあるかを大文字小文字を無視して調べる。
//
// states: 検査する Status 名の一覧。
// 戻り値: 選択肢に無い名前が1つでもあればエラー。
func (ft *fakeTracker) VerifyKnownStates(states []string) error {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.verifies = append(ft.verifies, append([]string(nil), states...))
	var unknown []string
	for _, s := range states {
		known := false
		for _, o := range ft.options {
			if strings.EqualFold(strings.TrimSpace(o), strings.TrimSpace(s)) {
				known = true
				break
			}
		}
		if !known {
			unknown = append(unknown, s)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	return fmt.Errorf("カンバンに無い Status 名です: %s", strings.Join(unknown, ", "))
}

// Verifies は VerifyKnownStates が受け取った値を受け取った順に返す。
//
// 戻り値: 検査を求められた値の並び。
func (ft *fakeTracker) Verifies() [][]string {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	out := make([][]string, len(ft.verifies))
	copy(out, ft.verifies)
	return out
}

// SetState は FetchIssueByIdentifier が返す現在の Status を差し替える。
//
// state: 返す Status の値。
func (ft *fakeTracker) SetState(state string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.state = state
}

// SetNotListed は「その issue はカンバンに載っていない」状態にする。
//
// **Status を確かめられない状態を作るために要る。**
func (ft *fakeTracker) SetNotListed() {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.found = false
}

// SetWriteRejected は UpdateStatus が「書けなかった」を返す状態にする。
//
// **エラーではなく「書かれなかった」である。**カンバンから item が見えないときに
// トラッカーがこれを返す。
func (ft *fakeTracker) SetWriteRejected() {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.written = false
}

// RejectWriteFrom は、n 回目以降の UpdateStatus だけを「書けなかった」にする。
//
// **手を離させる書き込みは通し、片付けたあとの書き込みだけを落とすために要る。**
// SetWriteRejected は1件目から落とすので、park の段で止まって片付けまで進めない。
//
// n: 落とし始める回数（1 始まり）。
func (ft *fakeTracker) RejectWriteFrom(n int) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.rejectFrom = n
}

// Bootstrap は project と Status フィールドの ID の解決を受けたことだけを記録する。
//
// ctx: 実行に適用するコンテキスト（使わない）。
// cfg: トラッカーの設定（使わない）。
// 戻り値: 常に nil。
func (ft *fakeTracker) Bootstrap(_ context.Context, _ config.TrackerConfig) error {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.bootstraps++
	return nil
}

// FetchIssueByIdentifier はカンバンから issue を1件引いたことにする。
//
// ctx: 実行に適用するコンテキスト（使わない）。
// identifier: `<owner>/<repo>#<番号>` の形の名前。
// 戻り値の1つ目: 現在の Status を載せた issue。
// 戻り値の2つ目: カンバンに載っていれば true。
// 戻り値の3つ目: 常に nil。
func (ft *fakeTracker) FetchIssueByIdentifier(
	_ context.Context, identifier string,
) (tracker.Issue, bool, error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.fetches = append(ft.fetches, identifier)
	if !ft.found {
		return tracker.Issue{}, false, nil
	}
	return tracker.Issue{ID: ft.itemID, Identifier: identifier, State: ft.state}, true, nil
}

// UpdateStatus は Status の書き込みを記録する。
//
// ctx: 実行に適用するコンテキスト（使わない）。
// itemID: 書き込み先の project item の ID。
// targetState: 書き込む Status の値。
// blockedStates: 書かない状態の一覧（使わない）。
// 戻り値の1つ目: 何をしたか。**Previous には書き込む直前の Status が入る。**
// 戻り値の2つ目: 常に nil。
func (ft *fakeTracker) UpdateStatus(
	_ context.Context, itemID, targetState string, _ []string,
) (tracker.StatusWrite, error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.updates = append(ft.updates, statusUpdate{ItemID: itemID, State: targetState})
	written := ft.written
	if ft.rejectFrom > 0 && len(ft.updates) >= ft.rejectFrom {
		written = false
	}
	previous := ft.state
	if written {
		ft.state = targetState
	}
	return tracker.StatusWrite{Reached: written, Wrote: written, Previous: previous}, nil
}

// Updates は書き込んだ内容を書き込んだ順に返す。
//
// 戻り値: 書き込みの並び。
func (ft *fakeTracker) Updates() []statusUpdate {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	out := make([]statusUpdate, len(ft.updates))
	copy(out, ft.updates)
	return out
}

// Fetches はカンバンから issue を引いた回数を返す。
//
// 戻り値: 引いた回数。
func (ft *fakeTracker) Fetches() int {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	return len(ft.fetches)
}

// ===== 進まない時計 =====

// fakeClock は実時間を待たずに期限を過ぎさせるための時計である。
//
// **pane が閉じるのを待つ上限を、実時間で待って確かめるわけにはいかない。**
// Sleep のたびにこの時計を進めることで、待ち時間ゼロで上限超過を再現する。
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// Now は現在時刻を返す。
//
// 戻り値: 進めた分だけ進んだ時刻。
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance は時計を進める。
//
// d: 進める長さ。
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
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

// ghqAnswer は `ghq list -p -e <owner>/<repo>` の代わりに返す答えである。
//
// **テストの途中で差し替えられるようにポインタで持つ。**worktree を用意したあとで
// 「clone が手元から無くなった」状態を作るのに要る（残った branch を調べられない経路）。
type ghqAnswer struct {
	// Path は答えるパスである。**空文字は「clone が無い」を表す。**
	Path string
}

// newTestRepo は bare リポジトリと、初期コミットを1つ持つ clone を作る。
//
// t: 呼び出し元のテスト。
// root: リポジトリを置くディレクトリ。
// 戻り値: 作ったリポジトリ。既定 branch は "main"。
func newTestRepo(t *testing.T, root string) *testRepo {
	t.Helper()

	origin := filepath.Join(root, "origin.git")
	runGit(t, "", "init", "--quiet", "--bare", "--initial-branch=main", origin)

	dir := filepath.Join(root, "repo")
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

// ===== 一式の用意 =====

// fixture は abandon を1回走らせるための一式である。
type fixture struct {
	// Root は一時ディレクトリの根である（socket を短く保つため MkdirTemp で作る）。
	Root string
	// Repo は本物の git のリポジトリである。
	Repo *testRepo
	// Herdr はテスト用herdr mock である。
	Herdr *fakeHerdr
	// Manager は本物の worktree の走査・検査・片付けである。
	Manager *workspace.Manager
	// Config は WORKFLOW.md を読み込んだ設定である。
	Config config.Config
	// WorkflowPath は WORKFLOW.md の絶対パスである。
	WorkflowPath string
	// Tracker は偽のカンバンである。
	Tracker *fakeTracker
	// Clock は進まない時計である。
	Clock *fakeClock
	// LockPath は二重起動防止のロックファイルの絶対パスである。
	LockPath string
	// SettingsRoot は issue ごとの設定ファイルの置き場所である（設計 3-12）。
	SettingsRoot string
	// Ghq は `ghq list -p -e` の代わりに返す答えである。
	// **空文字にすると「clone が手元に無い」状態になる**（残った branch を調べられない）。
	Ghq *ghqAnswer
	// Out は abandon が人間に見せた出力である。
	Out bytes.Buffer
	// Err は abandon が止まった理由の出力である。
	Err bytes.Buffer

	// trackerBuilds はカンバンのアダプタを作った回数である。
	// **worktree が無い実行で `gh` を起動しないことを確かめるのに使う。**
	trackerBuilds int
}

// newFixture はテスト用herdr mock・本物の git のリポジトリ・WORKFLOW.md・
// worktree の置き場所を用意する。
//
// **worktree はまだ1つも作らない。**個々のテストが Prepare で必要な数だけ作る。
//
// t: 呼び出し元のテスト。
// 戻り値: abandon を走らせるための一式。
func newFixture(t *testing.T) *fixture {
	t.Helper()
	return newFixtureWithConfig(t, "")
}

// newFixtureWithConfig は WORKFLOW.md に設定を1ブロック足した一式を用意する。
//
// **既定の設定では通らない分岐を確かめるために要る**（`cleanup.delete_branch` を
// 偽にした環境など）。
//
// t: 呼び出し元のテスト。
// extra: WORKFLOW.md の front matter へ足す YAML（最上位のキーから書くこと）。
// 戻り値: abandon を走らせるための一式。
func newFixtureWithConfig(t *testing.T, extra string) *fixture {
	t.Helper()

	// **socket のパスを短く保つ**（macOS の Unix domain socket の上限は103バイト）。
	root, err := os.MkdirTemp("", "cabd")
	if err != nil {
		t.Fatalf("一時ディレクトリを作れません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("一時ディレクトリを解決できません: %v", err)
	}

	repo := newTestRepo(t, root)
	fake := newFakeHerdr(t, root, repo.Dir)

	workflowPath := filepath.Join(root, "WORKFLOW.md")
	writeWorkflow(t, workflowPath, filepath.Join(root, "wt"), fake.SocketPath, extra)
	loaded, err := config.Load(workflowPath)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}

	settingsRoot := filepath.Join(root, "issues")
	ghq := &ghqAnswer{Path: repo.Dir}
	mgr, err := workspace.New(workspace.Options{
		Config:       loaded.Config,
		Herdr:        fake.Client(),
		HomeDir:      filepath.Join(root, "home"),
		GhqList:      func(_ context.Context, _, _ string) (string, error) { return ghq.Path, nil },
		SettingsRoot: settingsRoot,
	})
	if err != nil {
		t.Fatalf("workspace.New に失敗した: %v", err)
	}

	return &fixture{
		Root:         root,
		Repo:         repo,
		Herdr:        fake,
		Manager:      mgr,
		Config:       loaded.Config,
		WorkflowPath: workflowPath,
		Tracker:      newFakeTracker("In Progress"),
		Clock:        &fakeClock{now: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)},
		LockPath:     filepath.Join(root, "continuo.lock"),
		SettingsRoot: settingsRoot,
		Ghq:          ghq,
	}
}

// writeWorkflow は WORKFLOW.md を書く。
//
// **書かないキーは既定値のままにする**（tracker.active_states は Ready と In Progress、
// tracker.failure_state は Blocked、cleanup は全部有効）。
//
// t: 呼び出し元のテスト。
// path: 書き出す WORKFLOW.md の絶対パス。
// worktreeRoot: worktree の置き場所。
// socketPath: テスト用herdr mock の socket のパス。
// extra: front matter へ足す YAML（空文字なら何も足さない）。
func writeWorkflow(t *testing.T, path, worktreeRoot, socketPath, extra string) {
	t.Helper()
	content := fmt.Sprintf(`---
tracker:
  provider:
    owner: octocat
    project_number: 3
    status_field: Status
    token_source: env
    token_env: CONTINUO_TEST_TOKEN
workspace:
  root: %s
herdr:
  socket: %s
  protocol: 20
  read_timeout_ms: 3000
rate_limit:
  source: none
%s---

{{.issue.identifier}} を実装してください。
`, worktreeRoot, socketPath, extra)

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
}

// issueURL はテストで使う issue の URL を組み立てる。
//
// number: issue 番号。
// 戻り値: `https://github.com/octocat/hello-world/issues/<番号>`。
func issueURL(number int) string {
	return issueURLFor("octocat", "hello-world", number)
}

// issueURLFor は owner とリポジトリ名を指定して issue の URL を組み立てる。
//
// **置き場所のパスと身元ファイルの issue_url を食い違わせるために要る。**
// 置き場所は `<root>/<host>/<owner>/<repo>/<スラグ>` なので、別のリポジトリで
// worktree を用意すれば、パスから読める owner とリポジトリ名が変わる。
//
// owner: リポジトリの所有者名。
// repo: リポジトリ名。
// number: issue 番号。
// 戻り値: `https://github.com/<owner>/<repo>/issues/<番号>`。
func issueURLFor(owner, repo string, number int) string {
	return fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, number)
}

// issueRef は worktree を用意するための issue の情報を作る。
//
// number: issue 番号。
// 戻り値: owner が octocat、リポジトリが hello-world、既定 branch が main の issue。
func issueRef(number int) workspace.IssueRef {
	return issueRefFor("octocat", "hello-world", number)
}

// issueRefFor は owner とリポジトリ名を指定して issue の情報を作る。
//
// owner: リポジトリの所有者名。
// repo: リポジトリ名。
// number: issue 番号。
// 戻り値: 指定した owner とリポジトリ名を持ち、既定 branch が main の issue。
func issueRefFor(owner, repo string, number int) workspace.IssueRef {
	return workspace.IssueRef{
		URL:           issueURLFor(owner, repo, number),
		Identifier:    fmt.Sprintf("%s/%s#%d", owner, repo, number),
		ProjectItemID: "PVTI_test",
		Owner:         owner,
		Repo:          repo,
		Number:        number,
		NativeRef:     map[string]any{"default_branch": "main"},
	}
}

// Prepare は worktree を1つ作り、身元ファイルと issue ごとの設定ファイルを置く。
//
// **`continuo` が着手したのと同じ状態を作る。**身元ファイルの `issue_url` が
// abandon の照合の鍵なので、必ず書く。
//
// t: 呼び出し元のテスト。
// number: issue 番号。
// 戻り値: 作った worktree。
func (fx *fixture) Prepare(t *testing.T, number int) *workspace.PrepareResult {
	t.Helper()

	prepared, err := fx.Manager.Prepare(context.Background(), issueRef(number))
	if err != nil {
		t.Fatalf("worktree を用意できません: %v", err)
	}

	settingsDir := filepath.Join(fx.SettingsRoot, fmt.Sprintf("octocat-hello-world-%d", number))
	if err := os.MkdirAll(settingsDir, 0o700); err != nil {
		t.Fatalf("issue ごとの設定ファイルの置き場所を作れません: %v", err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("issue ごとの設定ファイルを書けません: %v", err)
	}

	fx.WriteIdentity(t, prepared, issueURL(number), number, settingsPath)
	return prepared
}

// PrepareIn は別のリポジトリの下に worktree を1つ作り、身元ファイルを置く。
//
// **身元ファイルの issue_url を、置き場所のパスと食い違う値にできる。**
// worktree の中でエージェントが issue_url を書き換えた状態を、これで作る。
//
// t: 呼び出し元のテスト。
// owner: 置き場所の2階層目になる所有者名。
// repo: 置き場所の3階層目になるリポジトリ名。
// number: issue 番号。
// identityURL: 身元ファイルへ書く issue の URL。
// 戻り値: 作った worktree。
func (fx *fixture) PrepareIn(
	t *testing.T, owner, repo string, number int, identityURL string,
) *workspace.PrepareResult {
	t.Helper()

	prepared, err := fx.Manager.Prepare(context.Background(), issueRefFor(owner, repo, number))
	if err != nil {
		t.Fatalf("worktree を用意できません: %v", err)
	}
	settingsPath := filepath.Join(fx.SettingsRoot,
		fmt.Sprintf("%s-%s-%d", owner, repo, number), "settings.json")
	fx.WriteIdentity(t, prepared, identityURL, number, settingsPath)
	return prepared
}

// WriteIdentity は worktree の身元ファイルを書く。
//
// **`issueURL` を指定できるようにしてある。**同じ issue の worktree が2つある状態を
// 作るには、別の issue で用意した worktree の `issue_url` を書き換えるほかない。
//
// t: 呼び出し元のテスト。
// prepared: 対象の worktree。
// url: 身元ファイルへ書く issue の URL。
// number: 身元ファイルへ書く issue の番号。
// settingsPath: issue ごとの設定ファイルの絶対パス。
func (fx *fixture) WriteIdentity(
	t *testing.T, prepared *workspace.PrepareResult, url string, number int, settingsPath string,
) {
	t.Helper()
	identity := workspace.Identity{
		IssueURL:         url,
		IssueIdentifier:  fmt.Sprintf("octocat/hello-world#%d", number),
		ProjectItemID:    "PVTI_test",
		Branch:           prepared.Branch.String(),
		Base:             prepared.Base.String(),
		HerdrWorkspaceID: prepared.HerdrWorkspaceID,
		SettingsPath:     settingsPath,
		CreatedAt:        time.Now(),
	}
	if err := fx.Manager.WriteIdentity(context.Background(), prepared.Path, identity); err != nil {
		t.Fatalf("身元ファイルを書けません: %v", err)
	}
}

// SetIdentityBranch は身元ファイルの branch だけを別の名前へ書き換える。
//
// **worktree が現に checkout している branch と食い違う身元ファイルを作る。**
// 身元ファイルは worktree の中にあってエージェントが書き換えられるので、
// 片付けは「現物と一致する branch」しか消さない。
//
// t: 呼び出し元のテスト。
// prepared: 対象の worktree。
// branch: 身元ファイルへ書く branch 名。
func (fx *fixture) SetIdentityBranch(
	t *testing.T, prepared *workspace.PrepareResult, branch string,
) {
	t.Helper()
	identity, err := fx.Manager.ReadIdentity(prepared.Path)
	if err != nil {
		t.Fatalf("身元ファイルを読めません: %v", err)
	}
	identity.Branch = branch
	if err := fx.Manager.WriteIdentity(context.Background(), prepared.Path, *identity); err != nil {
		t.Fatalf("身元ファイルを書けません: %v", err)
	}
}

// orphanBranch は、worktree を作らずに issue の branch だけをリポジトリに残す。
//
// **片付けの途中で失敗して branch だけが残った状態を作る**（issue #27）。
// 名前は continuo が着手のときに使うのと同じ規則（`herdr.worktree.branch_template`）で
// 組み立てる。**手で書いた文字列を使わない。**規則を変えたときに空振りする。
//
// t: 呼び出し元のテスト。
// fx: 走らせる一式。
// number: issue 番号。
// 戻り値の1つ目: 作った branch 名。
// 戻り値の2つ目: その branch が指す commit の SHA（消したことの応答に出る）。
func orphanBranch(t *testing.T, fx *fixture, number int) (string, string) {
	t.Helper()
	name, _, err := workspace.RenderBranch(fx.Config.Herdr.Worktree.BranchTemplate, issueRef(number))
	if err != nil {
		t.Fatalf("branch 名を組み立てられません: %v", err)
	}
	runGit(t, fx.Repo.Dir, "branch", name.String(), "main")
	return name.String(), runGit(t, fx.Repo.Dir, "rev-parse", name.String())
}

// orphanBranchWithUnpushedCommit は、worktree を作らずに issue の branch だけを残し、
// そこへ**どの remote にも載っていない commit** を1つ積む（issue #27）。
//
// **checkout せずに積む。**clone の作業ディレクトリは main のままにしておきたい
// （worktree が1つも無い状態を作るのが目的である）。
//
// t: 呼び出し元のテスト。
// fx: 走らせる一式。
// number: issue 番号。
// 戻り値: 作った branch 名。
func orphanBranchWithUnpushedCommit(t *testing.T, fx *fixture, number int) string {
	t.Helper()
	name, _, err := workspace.RenderBranch(fx.Config.Herdr.Worktree.BranchTemplate, issueRef(number))
	if err != nil {
		t.Fatalf("branch 名を組み立てられません: %v", err)
	}
	tree := runGit(t, fx.Repo.Dir, "rev-parse", "main^{tree}")
	commit := runGit(t, fx.Repo.Dir, "commit-tree", tree, "-p", "main", "-m", "push していない成果")
	runGit(t, fx.Repo.Dir, "branch", name.String(), commit)
	return name.String()
}

// CopyToHost は worktree のディレクトリを、置き場所の**別のホスト名の階層**へ複製する。
//
// **同じ issue の候補を2件にする、ただ1つの現実的な作り方である。**置き場所は
// `<root>/<host>/<owner>/<repo>/<スラグ>` で、スラグは branch 名から作られる。
// owner・リポジトリ名・スラグまで一致する worktree を git で2つ作ることはできない
// （同じ branch を2つの worktree でチェックアウトできない）ので、**人間が手で
// ディレクトリを複製した跡**を作る。GitHub Enterprise から github.com へ移した跡も
// この形になる。
//
// **複製するのは身元ファイルだけである。**abandon は候補が2件あればそこで止まるので、
// git の実体までは要らない。
//
// t: 呼び出し元のテスト。
// prepared: 複製元の worktree。
// host: 複製先のホスト名（置き場所の1階層目）。
// 戻り値: 複製した worktree の絶対パス。
func (fx *fixture) CopyToHost(
	t *testing.T, prepared *workspace.PrepareResult, host string,
) string {
	t.Helper()

	root := fx.Manager.ResolvedRoot()
	rel, err := filepath.Rel(root, prepared.Path)
	if err != nil {
		t.Fatalf("置き場所からの相対パスを作れません: %v", err)
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	if len(parts) != 4 {
		t.Fatalf("置き場所の4階層になっていません: %v", parts)
	}
	dest := filepath.Join(root, host, parts[1], parts[2], parts[3])
	if err := os.MkdirAll(dest, 0o700); err != nil {
		t.Fatalf("複製先のディレクトリを作れません（%s）: %v", dest, err)
	}
	raw, err := os.ReadFile(fx.Manager.IdentityPath(prepared.Path))
	if err != nil {
		t.Fatalf("複製元の身元ファイルを読めません: %v", err)
	}
	if err := os.WriteFile(fx.Manager.IdentityPath(dest), raw, 0o600); err != nil {
		t.Fatalf("複製先へ身元ファイルを書けません: %v", err)
	}
	return dest
}

// RemoveIdentity は worktree の身元ファイルだけを消す。
//
// **着手が worktree を作った直後に止まった状態を作る**（3-16 の段6〜段9）。
// **Scan はこの worktree を結果に含めない**ので、数える口が別に要る。
//
// t: 呼び出し元のテスト。
// prepared: 対象の worktree。
func (fx *fixture) RemoveIdentity(t *testing.T, prepared *workspace.PrepareResult) {
	t.Helper()
	path := fx.Manager.IdentityPath(prepared.Path)
	if err := os.Remove(path); err != nil {
		t.Fatalf("身元ファイルを消せません（%s）: %v", path, err)
	}
}

// Options は abandon.Run へ渡す入力を組み立てる。
//
// **外部へ繋ぐ処理は全部差し替える。**カンバンは偽のカンバン、herdr はテスト用herdr mock、
// worktree は一時ディレクトリの下のものだけを見る。**二重起動の判定だけは本物**
// （`internal/lock` の flock）で、一時ディレクトリのロックファイルを使う。
//
// number: 片付ける issue の番号。
// 戻り値: abandon.Run へ渡す入力。
func (fx *fixture) Options(number int) abandon.Options {
	return abandon.Options{
		ConfigPath: fx.WorkflowPath,
		IssueURL:   issueURL(number),
		Out:        &fx.Out,
		Err:        &fx.Err,
		// **待ち時間は実時間では進めない。**Sleep のたびに fx.Clock を進める。
		PaneWaitTimeout:  3 * time.Second,
		PaneWaitInterval: time.Second,
		Deps: abandon.Deps{
			LockPath:  fx.LockPath,
			Herdr:     fx.Herdr.Client(),
			Workspace: fx.Manager,
			NewTracker: func(_ context.Context) (abandon.Tracker, error) {
				fx.trackerBuilds++
				return fx.Tracker, nil
			},
			Now: fx.Clock.Now,
			Sleep: func(_ context.Context, d time.Duration) bool {
				fx.Clock.Advance(d)
				return true
			},
		},
	}
}

// Run は abandon を1回走らせる。
//
// t: 呼び出し元のテスト。
// number: 片付ける issue の番号。
// mutate: 入力を書き換える関数（nil 可）。フラグはここで立てる。
// 戻り値: 終了コード。
func (fx *fixture) Run(t *testing.T, number int, mutate func(opts *abandon.Options)) int {
	t.Helper()
	opts := fx.Options(number)
	if mutate != nil {
		mutate(&opts)
	}
	return abandon.Run(context.Background(), opts)
}

// Output は abandon が出した文言（標準出力と止まった理由）をつなげて返す。
//
// 戻り値: 出力の全文。
func (fx *fixture) Output() string {
	return fx.Out.String() + fx.Err.String()
}

// TrackerBuilds はカンバンのアダプタを作った回数を返す。
//
// 戻り値: 作った回数。
func (fx *fixture) TrackerBuilds() int {
	return fx.trackerBuilds
}

// ===== 壊れた worktree を作る =====

// gitFileBreakage は worktree の `.git` の壊れ方である（issue #23）。
//
// **worktree の `.git` はディレクトリではなく `gitdir: …` と書かれただけのファイルである。**
// そこでエージェントが `--permission-mode dontAsk` で動くので、**中身は書き換えられるし、
// 消せる。**壊れると `git -C <worktree> …` が1つも通らなくなる。
type gitFileBreakage string

const (
	// gitFileEmpty は `.git` が空のファイルになっている状態である。
	// git は `fatal: invalid gitfile format` で断る。
	gitFileEmpty gitFileBreakage = "空"
	// gitFileGarbage は `.git` に `gitdir:` で始まらない文字列が入っている状態である。
	// git は `fatal: invalid gitfile format` で断る。
	gitFileGarbage gitFileBreakage = "でたらめ"
	// gitFileMissing は `.git` そのものが無い状態である。
	// git は `fatal: not a git repository` で断る。
	gitFileMissing gitFileBreakage = "不在"
)

// BreakGitFile は worktree の `.git` を壊す。
//
// **利用者が実際に踏んだ状態を作る**（issue #23。`continuo abandon --dry-run` が
// `fatal: invalid gitfile format` で落ちて、何もできなくなった）。
//
// t: 呼び出し元のテスト。
// prepared: 対象の worktree。
// how: 壊し方。
func (fx *fixture) BreakGitFile(
	t *testing.T, prepared *workspace.PrepareResult, how gitFileBreakage,
) {
	t.Helper()
	gitFile := filepath.Join(prepared.Path, ".git")
	var err error
	switch how {
	case gitFileEmpty:
		err = os.WriteFile(gitFile, nil, 0o644)
	case gitFileGarbage:
		err = os.WriteFile(gitFile, []byte("でたらめな文字列\n"), 0o644)
	case gitFileMissing:
		err = os.Remove(gitFile)
	default:
		t.Fatalf("知らない壊し方です: %s", how)
	}
	if err != nil {
		t.Fatalf(".git を%sにできません（%s）: %v", how, gitFile, err)
	}
	// **前提が崩れていないことを確かめる。**壊したつもりで git が通っていたら、
	// このテストは何も検証していない。
	out, runErr := exec.Command("git", "-C", prepared.Path, "status", "--porcelain").CombinedOutput()
	if runErr == nil {
		t.Fatalf(".git を%sにしたのに `git -C %s status` が通ってしまった:\n%s",
			how, prepared.Path, out)
	}
}

// CloseHerdr はテスト用herdr mock を止め、以後の問い合わせを必ず失敗させる。
//
// **「herdr ごと落ちている」状態を作るために要る**（issue #23）。
// **worktree を用意したあとで呼ぶこと。**用意の段階では herdr が要る。
//
// t: 呼び出し元のテスト。
// 戻り値: 以後 abandon が受け取るのと同じエラー（**文言の照合に使う**）。
func (fx *fixture) CloseHerdr(t *testing.T) error {
	t.Helper()
	if err := fx.Herdr.listener.Close(); err != nil {
		t.Fatalf("テスト用herdr mock を止められません: %v", err)
	}
	_, err := fx.Herdr.Client().PaneList(context.Background(), herdr.PaneListParams{})
	if err == nil {
		t.Fatal("テスト用herdr mock を止めたのに pane の一覧を引けてしまった（前提が崩れている）")
	}
	return err
}

// holdLock は「continuo が動いている」状態を作る。
//
// **abandon はロックを取れたかどうかで継続監視の生死を判定する**（設計 3-17）ので、
// テストが先に掴んでおけば「動いている」側の経路に入る。
// **掴んだロックはテストの終わりに手放す。**手放さないと、同じロックファイルを使う
// あとのテストが「動いている」状態を引きずる。
//
// **1箇所にまとめてある。**同じ前置きを17箇所へ書き写していたので、掴み方を変えると
// **書き換え漏れが必ず出る。**
//
// t: 呼び出し元のテスト。
// fx: 対象の一式（`LockPath` を使う）。
func holdLock(t *testing.T, fx *fixture) {
	t.Helper()
	held, err := lock.Acquire(fx.LockPath)
	if err != nil {
		t.Fatalf("テストがロックを掴めません: %v", err)
	}
	t.Cleanup(func() { _ = held.Release() })
}

// freezeDir はディレクトリから書き込みの権限を落とし、中身を1つも消せなくする。
//
// **「本当に片付けられない」状態を作るために要る。**herdr にエラーを返させるだけでは、
// continuo が実体を自分で消しにいく（issue #23）ので、片付けは成功してしまう。
//
// **後始末で戻す。**戻さないと一時ディレクトリを消せない。
//
// t: 呼び出し元のテスト。
// dir: 書き込みを止めるディレクトリ。
func freezeDir(t *testing.T, dir string) {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("ディレクトリを調べられません（%s）: %v", dir, err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("ディレクトリの権限を落とせません（%s）: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, info.Mode().Perm()) })
}

// ===== 検査の道具 =====

// assertExit は終了コードが期待どおりかを確かめる。
//
// t: 呼び出し元のテスト。
// fx: 走らせた一式。
// got: 実際の終了コード。
// want: 期待する終了コード。
func assertExit(t *testing.T, fx *fixture, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("終了コードが %d ではなく %d だった\n出力:\n%s", want, got, fx.Output())
	}
}

// assertContains は出力に文言が含まれることを確かめる。
//
// t: 呼び出し元のテスト。
// fx: 走らせた一式。
// want: 含まれるべき文言。
func assertContains(t *testing.T, fx *fixture, want string) {
	t.Helper()
	if !strings.Contains(fx.Output(), want) {
		t.Fatalf("出力に %q が含まれていない\n出力:\n%s", want, fx.Output())
	}
}

// assertNotContains は出力に文言が含まれないことを確かめる。
//
// t: 呼び出し元のテスト。
// fx: 走らせた一式。
// unwanted: 含まれてはならない文言。
func assertNotContains(t *testing.T, fx *fixture, unwanted string) {
	t.Helper()
	if strings.Contains(fx.Output(), unwanted) {
		t.Fatalf("出力に %q が含まれている\n出力:\n%s", unwanted, fx.Output())
	}
}

// assertWorktreeExists は worktree が残っていることを確かめる。
//
// t: 呼び出し元のテスト。
// fx: 走らせた一式。
// path: worktree の絶対パス。
func assertWorktreeExists(t *testing.T, fx *fixture, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("worktree が消えている（%s）: %v\n出力:\n%s", path, err, fx.Output())
	}
}

// assertWorktreeGone は worktree が消えていることを確かめる。
//
// t: 呼び出し元のテスト。
// fx: 走らせた一式。
// path: worktree の絶対パス。
func assertWorktreeGone(t *testing.T, fx *fixture, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("worktree が残っている（%s）: %v\n出力:\n%s", path, err, fx.Output())
	}
}

// assertNoRemoval は herdr へ worktree.remove を1度も送っていないことを確かめる。
//
// **「消えていない」の検査としてはこちらのほうが強い。**worktree が残っていても、
// 消そうとして失敗しただけかもしれない。
//
// t: 呼び出し元のテスト。
// fx: 走らせた一式。
func assertNoRemoval(t *testing.T, fx *fixture) {
	t.Helper()
	for _, method := range fx.Herdr.Methods() {
		if method == herdr.MethodWorktreeRemove {
			t.Fatalf("何も消さないはずなのに herdr へ %s を送っている（%v）\n出力:\n%s",
				herdr.MethodWorktreeRemove, fx.Herdr.Methods(), fx.Output())
		}
	}
}

// branchExists は clone にその branch が残っているかを返す。
//
// t: 呼び出し元のテスト。
// fx: 走らせた一式。
// branch: 調べる branch 名。
// 戻り値: 残っていれば true。
func branchExists(t *testing.T, fx *fixture, branch string) bool {
	t.Helper()
	return runGit(t, fx.Repo.Dir, "branch", "--list", branch) != ""
}

// panesAt は、その worktree を作業ディレクトリに持つ pane を1つ返す。
//
// worktreePath: pane の cwd に入れる worktree の絶対パス。
// 戻り値: `pane.list` の応答に載せる pane。
func panesAt(worktreePath string) []map[string]any {
	return []map[string]any{{
		"pane_id":      "w1:p1",
		"workspace_id": "w1",
		"cwd":          worktreePath,
	}}
}
