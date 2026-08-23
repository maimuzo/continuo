// Package workspace_test は internal/workspace の worktree の用意・身元ファイル・
// 信頼の検査・後始末を検証する。
//
// **git は本物を使う。**一時ディレクトリに bare リポジトリと clone を作り、そこから
// worktree を切る。worktree の作成と削除・`git branch -D`・`git status --porcelain` と
// `git diff --quiet` の判定は、mockでは確かめられない。
//
// **herdr はテスト用socket mockで通す。**実 herdr を使うとテストが落ちたときに pane が残る。
package workspace_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/workspace"
)

// ===== テスト用herdr mock socket サーバ =====

// recordedRequest は偽サーバが受け取ったリクエスト1件である。
type recordedRequest struct {
	// Method は呼ばれたメソッド名である。
	Method string
	// Params は params をそのまま map にしたものである
	// （構造体で受けると、実スキーマに無いキーが混ざっても気づけない）。
	Params map[string]any
}

// fakeHerdr は herdr の socket API の代わりに使う偽のサーバである。
// 受け取ったリクエストをすべて記録し、メソッド名で決め打ちの result を返す。
type fakeHerdr struct {
	socketPath string

	mu       sync.Mutex
	requests []recordedRequest
	results  map[string]any
	// onRequest はメソッド名ごとの副作用である。**本物の herdr が worktree.remove で
	// worktree の実体を消すこと**を偽サーバでも再現するために使う（そうしないと
	// `git branch -D` が「checkout 中の branch は消せない」で必ず失敗し、
	// 片付けの段4 を検証できない）。
	// **接続ごとの goroutine から呼ばれるので、この中で t.Fatalf を呼んではならない。**
	onRequest map[string]func(params map[string]any)
}

// SetOnRequest はメソッドを受け取ったときの副作用を登録する。
//
// method: 対象のメソッド名。
// fn: 応答を返す前に呼ぶ関数。**t.Fatalf を使ってはならない。**
func (fh *fakeHerdr) SetOnRequest(method string, fn func(params map[string]any)) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	if fh.onRequest == nil {
		fh.onRequest = map[string]func(params map[string]any){}
	}
	fh.onRequest[method] = fn
}

// SetResult は、既に立っているテスト用herdr mock の応答を後から差し替える。
//
// **Prepare と Cleanup で違う応答を返させるために要る。**`Prepare` は
// 「herdr が別の場所を開いたら止める」検査を持つので（設計 6-2）、
// Cleanup の検算だけを試したいテストは、Prepare を通してから差し替える。
//
// method: 対象のメソッド名。
// result: 返す result。
func (fh *fakeHerdr) SetResult(method string, result any) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.results[method] = result
}

// newFakeHerdr はテスト用herdr mock サーバを1本立てる。
//
// t: 呼び出し元のテスト。socket とリスナーの後始末を t.Cleanup に登録する。
// results: メソッド名から返す result への対応。未登録のメソッドはエラー応答になる。
// 戻り値: 起動した *fakeHerdr。
//
// socket のパスは意図的に短く保つ（macOS の Unix domain socket のパス長上限は103バイト）。
func newFakeHerdr(t *testing.T, results map[string]any) *fakeHerdr {
	t.Helper()

	dir, err := os.MkdirTemp("", "wsherdr")
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

	fh := &fakeHerdr{socketPath: socketPath, results: results}

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

// serve は1本の接続を処理する。
//
// **接続ごとの goroutine から呼ばれるので t.Fatalf を使ってはならない**
// （FailNow はテスト本体の goroutine からしか呼べない）。失敗は t.Errorf で記録する。
func (fh *fakeHerdr) serve(t *testing.T, conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

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
		t.Errorf("テスト用herdr mock がリクエストを解析できません: %v", err)
		return
	}

	fh.mu.Lock()
	fh.requests = append(fh.requests, recordedRequest{Method: req.Method, Params: req.Params})
	result, ok := fh.results[req.Method]
	sideEffect := fh.onRequest[req.Method]
	fh.mu.Unlock()

	if req.Method == herdr.MethodWorktreeOpen {
		result = withOpenedPath(result, req.Params)
	}

	if sideEffect != nil {
		sideEffect(req.Params)
	}

	var resp map[string]any
	if ok {
		resp = map[string]any{"id": req.ID, "result": result}
	} else {
		resp = map[string]any{
			"id":    req.ID,
			"error": map[string]any{"code": "unknown_method", "message": req.Method},
		}
	}
	encoded, err := json.Marshal(resp)
	if err != nil {
		t.Errorf("テスト用herdr mock が応答を JSON 化できません: %v", err)
		return
	}
	if _, err := conn.Write(append(encoded, '\n')); err != nil {
		t.Errorf("テスト用herdr mock が応答を書けません: %v", err)
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

// Client は偽サーバを向いた本物の herdr クライアントを返す。
func (fh *fakeHerdr) Client() *herdr.Client {
	return herdr.New(fh.socketPath, herdr.Timeouts{Read: 5 * time.Second})
}

// withOpenedPath は worktree.open の応答に、**開いた worktree の絶対パスを載せる。**
//
// **本物の herdr は、開いた worktree のパスを応答に載せる**（`worktree.path`）。
// continuo は消す直前に「この workspace はどのパスを開いているのか」を herdr に
// 答えさせて検算する（設計 3-9 の段3）ので、偽サーバでも同じ形を返さないと
// その検算を検証できない。
//
// result: 登録してある応答。
// params: 受け取った params（path を読む）。
// 戻り値: worktree.path を埋めた応答。
func withOpenedPath(result any, params map[string]any) any {
	base, ok := result.(map[string]any)
	if !ok {
		return result
	}
	// **登録済みの応答が path を持っていれば、そちらを優先する**
	// （わざと食い違わせて検算を検証するテストのため）。
	if existing, ok := base["worktree"].(map[string]any); ok {
		if p, _ := existing["path"].(string); p != "" {
			return result
		}
	}
	path, _ := params["path"].(string)
	if path == "" {
		return result
	}
	copied := map[string]any{}
	for key, value := range base {
		copied[key] = value
	}
	copied["worktree"] = map[string]any{"path": path}
	return copied
}

// worktreeOpenResult は worktree.open の成功応答（変種 worktree_opened）の写しである。
//
// workspaceID: 返す herdr workspace の ID。
// paneID: 返す root pane の ID。
// 戻り値: JSON 化して result に載せる値。
func worktreeOpenResult(workspaceID, paneID string) map[string]any {
	return map[string]any{
		"type":      "worktree_opened",
		"workspace": map[string]any{"workspace_id": workspaceID},
		"tab":       map[string]any{"tab_id": workspaceID + ":t1"},
		"root_pane": map[string]any{"pane_id": paneID, "workspace_id": workspaceID},
		"worktree":  map[string]any{},
	}
}

// worktreeRemoveResult は worktree.remove の成功応答（変種 worktree_removed）の写しである。
//
// workspaceID: 消した workspace の ID。
// path: 消した worktree のパス。
// 戻り値: JSON 化して result に載せる値。
func worktreeRemoveResult(workspaceID, path string) map[string]any {
	return map[string]any{
		"type":         "worktree_removed",
		"workspace_id": workspaceID,
		"path":         path,
		"forced":       true,
	}
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
	cmd := exec.Command("git", full...)
	out, err := cmd.CombinedOutput()
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

	root, err := os.MkdirTemp("", "wsgit")
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

// ===== Manager の組み立て =====

// managerFixture はテスト1件分の Manager と、その周辺の値である。
type managerFixture struct {
	// Manager は検査対象である。
	Manager *workspace.Manager
	// Config は Manager に渡した設定である。
	Config config.Config
	// Root は workspace.root（シンボリックリンク解決前）である。
	Root string
	// Home は `~/.claude.json` を置くホームディレクトリである。
	Home string
	// Repo は本物の git のリポジトリである（nil のこともある）。
	Repo *testRepo
	// Herdr はテスト用herdr mock サーバである（nil のこともある）。
	Herdr *fakeHerdr
	// SettingsRoot は issue ごとの設定ファイルの置き場所として Manager に渡した値である
	// （設計 3-12 の `<実行時ディレクトリ>/issues`）。
	SettingsRoot string
}

// fixtureOptions は newFixture の任意の入力である。
type fixtureOptions struct {
	// Repo は worktree を切る元のリポジトリである。nil なら新しく作る。
	Repo *testRepo
	// Herdr はテスト用herdr mock サーバである。nil なら worktree.open / worktree.remove に
	// 成功応答を返すものを新しく作る。
	Herdr *fakeHerdr
	// Mutate は設定を書き換える関数である。nil なら既定のまま。
	Mutate func(cfg *config.Config)
	// GhqList は ghq の差し替えである。nil ならリポジトリの clone のパスを返す関数を使う。
	GhqList workspace.GhqListFunc
	// SettingsRoot は issue ごとの設定ファイルの置き場所である。
	// **nil なら一時ディレクトリの下の issues を使う。**空文字を渡したい検査
	// （置き場所が分からないときは消さない）のためにポインタで持つ。
	SettingsRoot *string
	// Logger は Manager に渡すロガーである。nil なら何も出力しない。
	// **ログにしか現れない振る舞い**（孤児 branch を消す前に控えた SHA など）を
	// 検証するテストが、出力を受け取るために使う。
	Logger *slog.Logger
}

// newFixture はテスト用の Manager を組み立てる。
//
// t: 呼び出し元のテスト。
// opts: 任意の入力。
// 戻り値: 組み立てた Manager と周辺の値。
func newFixture(t *testing.T, opts fixtureOptions) *managerFixture {
	t.Helper()

	repo := opts.Repo
	if repo == nil {
		repo = newTestRepo(t)
	}
	fake := opts.Herdr
	if fake == nil {
		fake = newFakeHerdr(t, map[string]any{
			herdr.MethodWorktreeOpen:   worktreeOpenResult("w9", "w9:p1"),
			herdr.MethodWorktreeRemove: worktreeRemoveResult("w9", ""),
		})
	}

	root := filepath.Join(t.TempDir(), "worktrees")
	home := t.TempDir()
	settingsRoot := filepath.Join(t.TempDir(), "issues")
	if opts.SettingsRoot != nil {
		settingsRoot = *opts.SettingsRoot
	}

	cfg := *config.DefaultConfig()
	cfg.Workspace.Root = root
	if opts.Mutate != nil {
		opts.Mutate(&cfg)
	}

	ghqList := opts.GhqList
	if ghqList == nil {
		ghqList = func(_ context.Context, _, _ string) (string, error) { return repo.Dir, nil }
	}

	mgr, err := workspace.New(workspace.Options{
		Config:       cfg,
		Herdr:        fake.Client(),
		HomeDir:      home,
		GhqList:      ghqList,
		SettingsRoot: settingsRoot,
		Logger:       opts.Logger,
	})
	if err != nil {
		t.Fatalf("workspace.New に失敗した: %v", err)
	}

	return &managerFixture{
		Manager:      mgr,
		Config:       cfg,
		Root:         root,
		Home:         home,
		Repo:         repo,
		Herdr:        fake,
		SettingsRoot: settingsRoot,
	}
}

// sampleIssue はテストで使う issue の情報である。
//
// number: issue 番号。
// 戻り値: owner が "octocat"、repo が "hello-world"、既定 branch が "main" の issue。
func sampleIssue(number int) workspace.IssueRef {
	return workspace.IssueRef{
		URL:           fmt.Sprintf("https://github.com/octocat/hello-world/issues/%d", number),
		Identifier:    fmt.Sprintf("octocat/hello-world#%d", number),
		ProjectItemID: "PVTI_test",
		Owner:         "octocat",
		Repo:          "hello-world",
		Number:        number,
		NativeRef:     map[string]any{"default_branch": "main"},
	}
}
