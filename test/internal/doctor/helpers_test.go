// Package doctor_test は `continuo doctor`（設計 3-32）の検査を確かめる。
//
// **本番のボード（project #3）へは1リクエストも送らない。**`httptest.Server` で偽の
// GraphQL サーバを立て、その URL を doctor へ渡す。
// **実 herdr には繋がない。**`net.Listen("unix", ...)` でテスト用socket mockを立てる。
// **本物の `gh` と `ghq` も使わない。**PATH の先頭へmockを置き、本物の認証情報を読ませない。
// **本物のホームディレクトリも読まない。**`~/.claude.json` と `~/.claude/.credentials.json`
// は一時ディレクトリの下に作る。
package doctor_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/maimuzo/continuo/internal/config"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/doctor"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// ===== テスト用herdr mock socket サーバ =====

// fakeHerdr は herdr の socket API の代わりに使う偽のサーバである。
//
// **ping にだけ答える。**doctor が呼ぶのは ping（CheckProtocol）だけである。
type fakeHerdr struct {
	// SocketPath は listen している socket の絶対パスである。
	SocketPath string

	mu sync.Mutex
	// protocol は ping が返す protocol 版である。
	protocol int
	// pings は ping を受けた回数である。
	pings int
}

// newFakeHerdr はテスト用herdr mock を1本立てる。
//
// t: 呼び出し元のテスト。socket の後始末を t.Cleanup に登録する。
// dir: socket を置くディレクトリ（**短く保つこと。**macOS の上限は103バイト）。
// protocol: ping が返す protocol 版。
// 戻り値: 起動したテスト用herdr mock。
func newFakeHerdr(t *testing.T, dir string, protocol int) *fakeHerdr {
	t.Helper()

	socketPath := filepath.Join(dir, "h.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("テスト用herdr mock の socket を bind できません（%s）: %v", socketPath, err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	fh := &fakeHerdr{SocketPath: socketPath, protocol: protocol}
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

// SetProtocol は ping が返す protocol 版を差し替える。
//
// protocol: 返す protocol 版。
func (fh *fakeHerdr) SetProtocol(protocol int) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.protocol = protocol
}

// Pings は ping を受けた回数を返す。
func (fh *fakeHerdr) Pings() int {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return fh.pings
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
		ID     string `json:"id"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(line, &req); err != nil {
		return
	}

	fh.mu.Lock()
	protocol := fh.protocol
	if req.Method == "ping" {
		fh.pings++
	}
	fh.mu.Unlock()

	var resp map[string]any
	if req.Method == "ping" {
		resp = map[string]any{"id": req.ID, "result": map[string]any{
			"type": "pong", "version": "0.8.0-fake", "protocol": protocol,
			"capabilities": map[string]any{"live_handoff": true},
		}}
	} else {
		resp = map[string]any{"id": req.ID,
			"error": map[string]any{"code": "unknown_method", "message": req.Method}}
	}
	encoded, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_, _ = conn.Write(append(encoded, '\n'))
}

// ===== テスト用GitHub GraphQL mock サーバ =====

// boardItem は偽ボードの project item 1件である。
type boardItem struct {
	// ItemID は project item の ID である。
	ItemID string
	// NameWithOwner は `<owner>/<repo>` である。**draft issue では空にする。**
	NameWithOwner string
	// Number は issue 番号である。
	Number int
	// State は Status の値である。
	State string
}

// boardFailure は偽ボードの落ち方である。
type boardFailure string

const (
	// failureNone は正常に応答する。
	failureNone boardFailure = ""
	// failureRateLimit は 429（レートリミット）を返す。**doctor はこれだけ `!` にする。**
	failureRateLimit boardFailure = "rate_limit"
	// failureNoProject は project が見つからない応答（null）を返す。
	failureNoProject boardFailure = "no_project"
	// failureBadCredentials は 401（トークンが無効・失効）を返す。
	failureBadCredentials boardFailure = "bad_credentials"
)

// fakeGitHub は GitHub の GraphQL API の代わりに使う偽のサーバである。
//
// **本番のボードへは1リクエストも送らない。**
type fakeGitHub struct {
	// URL は偽サーバのエンドポイントである。
	URL string
	// Owner はボードの所有者名である。
	Owner string

	mu sync.Mutex
	// items はボードに載っている item である。
	items []boardItem
	// statusOptions はボード側の Status の選択肢名である。
	statusOptions []string
	// failure は落ち方である。
	failure boardFailure
	// delay は応答を返すまでにわざと待つ時間である（期限の検査に使う）。
	delay time.Duration
	// queries は受け取ったクエリの種別を受け取った順に記録したものである。
	queries []string
}

// newFakeGitHub はテスト用GraphQL mockを1本立てる。
//
// t: 呼び出し元のテスト。後始末を t.Cleanup に登録する。
// owner: ボードの所有者名。
// items: 最初にボードへ載せる item。
// 戻り値: 起動した偽サーバ。
func newFakeGitHub(t *testing.T, owner string, items ...boardItem) *fakeGitHub {
	t.Helper()
	fg := &fakeGitHub{
		Owner:         owner,
		items:         items,
		statusOptions: []string{"Ice Box", "Ready", "In Progress", "Blocked", "In Review", "Done"},
		failure:       failureNone,
	}
	srv := httptest.NewServer(http.HandlerFunc(fg.handle))
	t.Cleanup(srv.Close)
	fg.URL = srv.URL
	return fg
}

// SetItems はボードに載っている item を差し替える。
//
// items: 載せる item。
func (fg *fakeGitHub) SetItems(items ...boardItem) {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	fg.items = items
}

// SetStatusOptions はボード側の Status の選択肢名を差し替える。
//
// options: 選択肢名。
func (fg *fakeGitHub) SetStatusOptions(options ...string) {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	fg.statusOptions = options
}

// SetFailure は落ち方を差し替える。
//
// failure: 落ち方。
func (fg *fakeGitHub) SetFailure(failure boardFailure) {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	fg.failure = failure
}

// SetDelay は応答を返すまでにわざと待つ時間を差し替える。
//
// **期限を過ぎても返らないサーバを再現するために使う。**
//
// d: 待つ時間。
func (fg *fakeGitHub) SetDelay(d time.Duration) {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	fg.delay = d
}

// Queries は受け取ったクエリの種別を受け取った順に返す。
func (fg *fakeGitHub) Queries() []string {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	out := make([]string, len(fg.queries))
	copy(out, fg.queries)
	return out
}

// handle は1件の GraphQL リクエストに答える。
//
// w: 応答の書き出し先。
// r: 受け取ったリクエスト。
func (fg *fakeGitHub) handle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	fg.mu.Lock()
	failure := fg.failure
	delay := fg.delay
	kind := "unknown"
	switch {
	case strings.Contains(req.Query, "field(name: $statusField)"):
		kind = "bootstrap"
	case strings.Contains(req.Query, "items(first: 100"):
		kind = "items"
	}
	fg.queries = append(fg.queries, kind)
	fg.mu.Unlock()

	if delay > 0 {
		// **要求が取り消されたら、その時点でやめる。**期限を切ったテストが
		// 待ち時間ぶん止まらないようにする。
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
	}

	if failure == failureBadCredentials {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
		return
	}

	if failure == failureRateLimit {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
		return
	}

	var data map[string]any
	switch kind {
	case "bootstrap":
		data = fg.bootstrapPayload(failure)
	case "items":
		data = fg.itemsPayload(failure)
	default:
		data = map[string]any{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

// bootstrapPayload は Bootstrap のクエリへの応答を組み立てる。
//
// failure: 落ち方。
// 戻り値: 応答の data。
func (fg *fakeGitHub) bootstrapPayload(failure boardFailure) map[string]any {
	if failure == failureNoProject {
		return map[string]any{"repositoryOwner": nil}
	}
	fg.mu.Lock()
	defer fg.mu.Unlock()
	options := make([]any, 0, len(fg.statusOptions))
	for i, name := range fg.statusOptions {
		options = append(options, map[string]any{"id": fmt.Sprintf("opt%d", i), "name": name})
	}
	return map[string]any{
		"repositoryOwner": map[string]any{
			"projectV2": map[string]any{
				"id": "PVT_board",
				"field": map[string]any{
					"__typename": "ProjectV2SingleSelectField",
					"id":         "PVTSSF_status",
					"options":    options,
				},
			},
		},
	}
}

// itemsPayload は候補の取得のクエリへの応答を組み立てる。
//
// failure: 落ち方。
// 戻り値: 応答の data。
func (fg *fakeGitHub) itemsPayload(failure boardFailure) map[string]any {
	if failure == failureNoProject {
		return map[string]any{"repositoryOwner": nil}
	}
	fg.mu.Lock()
	defer fg.mu.Unlock()
	nodes := make([]any, 0, len(fg.items))
	for _, it := range fg.items {
		nodes = append(nodes, fg.itemPayload(it))
	}
	return map[string]any{
		"repositoryOwner": map[string]any{
			"projectV2": map[string]any{
				"items": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil},
					"nodes":    nodes,
				},
			},
		},
	}
}

// itemPayload は project item 1件の応答を組み立てる。
//
// **NameWithOwner が空の item は draft issue にする**（リポジトリを持たない）。
//
// it: 対象の item。
// 戻り値: item 1件の応答。
func (fg *fakeGitHub) itemPayload(it boardItem) map[string]any {
	var content map[string]any
	if it.NameWithOwner == "" {
		content = map[string]any{
			"__typename": "DraftIssue",
			"id":         "DI_" + it.ItemID,
			"title":      "下書きの issue",
			"body":       "本文",
		}
	} else {
		content = map[string]any{
			"__typename": "Issue",
			"id":         "I_" + it.ItemID,
			"number":     it.Number,
			"title":      fmt.Sprintf("テスト用の issue %d", it.Number),
			"body":       "本文",
			"url":        fmt.Sprintf("https://github.com/%s/issues/%d", it.NameWithOwner, it.Number),
			"state":      "OPEN",
			"repository": map[string]any{
				"nameWithOwner":    it.NameWithOwner,
				"defaultBranchRef": map[string]any{"name": "main"},
			},
			"labels":         map[string]any{"nodes": []any{}},
			"assignees":      map[string]any{"nodes": []any{}},
			"blockedBy":      map[string]any{"nodes": []any{}},
			"linkedBranches": map[string]any{"nodes": []any{}},
			"comments":       map[string]any{"totalCount": 0},
		}
	}
	return map[string]any{
		"id":         it.ItemID,
		"isArchived": false,
		"fieldValueByName": map[string]any{
			"__typename": "ProjectV2ItemFieldSingleSelectValue",
			"name":       it.State,
			"optionId":   "opt1",
		},
		"content": content,
	}
}

// ===== テスト用gh / ghq mock =====

// ghAuthStatusWithProject は `gh auth status` の合格する出力である
// （`Active account: true` のブロックに `project` が単独の scope として並ぶ）。
const ghAuthStatusWithProject = `github.com
  ✓ Logged in to github.com account tester (keyring)
  - Active account: true
  - Token scopes: 'gist', 'project', 'read:org', 'repo', 'workflow'
`

// writeFakeGH は PATH の先頭へ置く偽の `gh` を作る。
//
// **本物の認証情報を読ませない。**
//
// t: 呼び出し元のテスト。
// dir: 実行ファイルを置くディレクトリ。
// authStatus: `gh auth status` が出力する内容。
// exitCode: `gh auth status` の終了コード（未ログインの再現に使う）。
func writeFakeGH(t *testing.T, dir, authStatus string, exitCode int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("テスト用gh mock を置くディレクトリを作れません: %v", err)
	}
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  cat <<'CONTINUO_EOF'
%s
CONTINUO_EOF
  exit %d
fi
exit 0
`, strings.TrimRight(authStatus, "\n"), exitCode)
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatalf("テスト用gh mock を書けません: %v", err)
	}
}

// writeFakeGhq は PATH の先頭へ置く偽の `ghq` を作る。
//
// **受け取った引数を argsFile へ記録する。**doctor が `ghq list -p -e <owner>/<repo>` と
// 呼ぶこと（設計 3-6 の3段と同じ呼び方）を確かめるためである。
//
// t: 呼び出し元のテスト。
// dir: 実行ファイルを置くディレクトリ。
// output: `ghq list` が標準出力へ返す内容（空文字なら「clone が無い」）。
// argsFile: 受け取った引数を書き出すファイルの絶対パス。
func writeFakeGhq(t *testing.T, dir, output, argsFile string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("テスト用ghq mock を置くディレクトリを作れません: %v", err)
	}
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
if [ -n %q ]; then
  echo %q
fi
exit 0
`, argsFile, output, output)
	if err := os.WriteFile(filepath.Join(dir, "ghq"), []byte(script), 0o755); err != nil {
		t.Fatalf("テスト用ghq mock を書けません: %v", err)
	}
}

// ===== 一式の用意 =====

// fixture は doctor を1回走らせるための一式である。
type fixture struct {
	// Root は一時ディレクトリの根である。
	Root string
	// Home は `~/.claude.json` と `~/.claude/.credentials.json` を置くホームディレクトリである。
	Home string
	// RunDir は hook の socket を置くディレクトリである（CONTINUO_RUNTIME_DIR に張る）。
	RunDir string
	// BinDir はテスト用gh / ghq mock を置いたディレクトリである。
	BinDir string
	// GhqArgsFile はテスト用ghq mock が受け取った引数を書き出すファイルである。
	GhqArgsFile string
	// RepoDir は本物の git の clone である（信頼の鍵を `git rev-parse` で引くため）。
	RepoDir string
	// WorkflowPath は WORKFLOW.md の絶対パスである。
	WorkflowPath string
	// Herdr はテスト用herdr mock である。
	Herdr *fakeHerdr
	// GitHub はテスト用GitHub mock である。
	GitHub *fakeGitHub
	// Env は doctor が引く環境変数である（Options.LookupEnv がこれを引く）。
	Env map[string]string
	// GhqPaths は注入する `ghq list` の結果である（鍵は `<owner>/<repo>`）。
	// **nil なら本物の RunGhqList を使う**（PATH のテスト用ghq mock が答える）。
	GhqPaths map[string]string
	// CheckTimeout は外部に触る検査1つあたりの上限である。
	// **0 なら doctor の既定（10秒）を使う。**返ってこない外部コマンドを待たずに
	// 済ませたいテストだけが短い値を入れる。
	CheckTimeout time.Duration
	// ClaudePath は claude が PATH にあるとしたときの場所である。
	//
	// **newFixture は「入っている」状態で初期化する。**無い状態を試す test は、
	// これを空文字にしてから doctor.Run を呼ぶこと。
	ClaudePath string
}

// newFixture はテスト用herdr mock・テスト用GitHub mock・本物の git のリポジトリ・WORKFLOW.md を用意する。
//
// **前提がすべて揃った状態を作る。**個々のテストは、そこから1つだけ壊して検査する。
//
// t: 呼び出し元のテスト。
// 戻り値: doctor を走らせるための一式。
func newFixture(t *testing.T) *fixture {
	t.Helper()

	// **socket のパスを短く保つ**（macOS の Unix domain socket の上限は103バイト）。
	root, err := os.MkdirTemp("", "cdoc")
	if err != nil {
		t.Fatalf("一時ディレクトリを作れません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("一時ディレクトリを解決できません: %v", err)
	}

	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("ホームディレクトリを作れません: %v", err)
	}
	binDir := filepath.Join(root, "bin")
	repoDir := filepath.Join(root, "repo")

	// 本物の git のリポジトリ（信頼の鍵は `git rev-parse --show-toplevel` で解決する）。
	runGit(t, "", "init", "--quiet", "--initial-branch=main", repoDir)
	runGit(t, repoDir, "config", "user.email", "continuo@example.test")
	runGit(t, repoDir, "config", "user.name", "continuo test")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("初期の中身\n"), 0o644); err != nil {
		t.Fatalf("初期ファイルを書けません: %v", err)
	}
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "--quiet", "-m", "初期コミット")

	ghqArgsFile := filepath.Join(root, "ghq-args.txt")
	writeFakeGH(t, binDir, ghAuthStatusWithProject, 0)
	writeFakeGhq(t, binDir, repoDir, ghqArgsFile)
	writeTrustFile(t, home, repoDir, true)

	// **偽の `claude` を PATH に実ファイルとして置く。**
	//
	// `LookPath` の差し替えは、この process の中でしか効かない。
	// **`TestDoctorCLI_*` は実行ファイルを起動する**ので、サブプロセスからは本物の PATH が見える。
	// 開発者の手元には claude があるので通り、**CI には無いので落ちた**（2026-08-23 に実測）。
	if err := os.WriteFile(filepath.Join(binDir, "claude"),
		[]byte("#!/bin/sh\n# doctor は在ることだけを見る。実行はされない。\n"), 0o755); err != nil {
		t.Fatalf("テスト用の claude を置けません: %v", err)
	}

	fx := &fixture{
		Root:   root,
		Home:   home,
		RunDir: filepath.Join(root, "run"),
		// **既定は「claude が入っている」である。**個々の test はここから1つだけ壊す。
		ClaudePath:   filepath.Join(binDir, "claude"),
		BinDir:       binDir,
		GhqArgsFile:  ghqArgsFile,
		RepoDir:      repoDir,
		WorkflowPath: filepath.Join(root, "WORKFLOW.md"),
		Herdr:        newFakeHerdr(t, root, config.DefaultConfig().Herdr.Protocol),
		GitHub: newFakeGitHub(t, "octocat",
			boardItem{ItemID: "PVTI_1", NameWithOwner: "octocat/hello-world", Number: 188, State: "Ready"}),
		Env:      map[string]string{},
		GhqPaths: map[string]string{"octocat/hello-world": repoDir},
	}
	fx.WriteWorkflow(t, "")

	// **PATH の先頭をテスト用gh / ghq mock にする。**本物の認証情報を読ませない。
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// ボードを読むトークンは環境変数から取る設定にしてある（偽サーバは値を見ない）。
	t.Setenv("CONTINUO_TEST_TOKEN", "dummy-token-for-the-fake-server")
	// **hook の置き場所をこのテストの一時ディレクトリに閉じる。**
	//
	// これを張らないと、hook の置き場所の検査は本番と同じ場所（$TMPDIR/continuo など）を
	// 見る。**開発機で continuo が動いていれば「既に使われている」の近道で ✓ になり、
	// listen も後始末も1行も実行されない。**逆に何も無ければ、テストが本番の置き場所に
	// socket を作って消す。**どちらも、この検査を確かめたことにならない。**
	t.Setenv(envRuntimeDir, fx.RunDir)
	// **ホームディレクトリも本物を見せない。**置き場所の探索の最後の候補が
	// `~/.continuo/run` なので、上の環境変数を消すテストが本物のホームへ落ちる。
	t.Setenv("HOME", home)

	return fx
}

// envRuntimeDir は hook の置き場所を差し替える環境変数である
// （internal/doctor の daemonEnvRuntimeDir と同じ値。**変えるときは両方を直すこと**）。
const envRuntimeDir = "CONTINUO_RUNTIME_DIR"

// SocketPath は、この fixture で hook の socket が置かれるパスを返す。
//
// 戻り値: `<Root>/run/hooks.sock`。
func (fx *fixture) SocketPath() string {
	return filepath.Join(fx.RunDir, "hooks.sock")
}

// assertSocketUnderRoot は、決まった socket のパスがテストの一時ディレクトリの下にあることを見る。
//
// **これが無いと、実機の socket を触ったことに誰も気づけない**
// （test/e2e/walkthrough_test.go に同じ番人がある）。
//
// t: 呼び出し元のテスト。
// fx: 使っている fixture。
// detail: hook の置き場所の検査が返した説明（socket のパスを含む）。
func assertSocketUnderRoot(t *testing.T, fx *fixture, detail string) {
	t.Helper()
	if !strings.Contains(detail, fx.Root) {
		t.Fatalf("テストの一時ディレクトリの外の socket を見ています（実機を触っています）: %s\n  一時ディレクトリ: %s",
			detail, fx.Root)
	}
}

// WriteWorkflow は WORKFLOW.md を書く。
//
// **`continuo init` が置く雛形から作る。**キーを1つも落とさないためである。
// **前提が揃っている状態とは、雛形の設定項目が全部書かれている状態である**
// （見出し語 `未記入の項目` が雛形と1対1で突き合わせる。設計 3-73）。
// 手で短い front matter を書くと、**その検査だけが必ず `!` になる。**
//
// **差し替えるのは値だけである。**キーは1つも消さず、増やさない。
//
// t: 呼び出し元のテスト。
// rateLimit: front matter へ書く `rate_limit` の節（末尾に改行を含めること）。
// 空文字なら「枠の判定を行わない」設定（`source: none`）を書く。
// **書いたキーだけが雛形の値を上書きする。**書かなかったキーは雛形の値のまま残る。
func (fx *fixture) WriteWorkflow(t *testing.T, rateLimit string) {
	t.Helper()
	if rateLimit == "" {
		rateLimit = "rate_limit:\n  source: none\n"
	}

	content := scaffold.TemplateWithValues(scaffold.Values{Owner: "octocat", ProjectNumber: 3})
	overrides := []struct {
		path  []string
		value string
	}{
		// **ボードを読むトークンは環境変数から取る。**本物の `gh auth token` を呼ばせない。
		{[]string{"tracker", "provider", "token_source"}, "env"},
		{[]string{"tracker", "provider", "token_env"}, "CONTINUO_TEST_TOKEN"},
		// **worktree の置き場所も herdr の socket も、テストの一時ディレクトリに閉じる。**
		{[]string{"workspace", "root"}, filepath.Join(fx.Root, "wt")},
		{[]string{"herdr", "socket"}, fx.Herdr.SocketPath},
		{[]string{"herdr", "read_timeout_ms"}, "3000"},
	}
	overrides = append(overrides, parseSectionOverrides(t, rateLimit)...)
	for _, o := range overrides {
		content = setFrontMatterValue(t, content, o.path, o.value)
	}

	if err := os.WriteFile(fx.WorkflowPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
}

// parseSectionOverrides は `rate_limit:\n  source: none\n` の形の節を、
// 「キーのパスと値」の並びに直す。
//
// **節そのものを差し込まない。**差し込むと、その節の残りのキーが雛形から消え、
// 見出し語 `未記入の項目` がテストのたびに `!` になる。
//
// t: 呼び出し元のテスト。
// section: 1行目が節の名前、2行目以降が `  <キー>: <値>` の並び。
// 戻り値: 差し替えるキーのパスと、そこへ書く値。
func parseSectionOverrides(t *testing.T, section string) []struct {
	path  []string
	value string
} {
	t.Helper()
	var out []struct {
		path  []string
		value string
	}
	lines := strings.Split(strings.TrimRight(section, "\n"), "\n")
	if len(lines) == 0 {
		return nil
	}
	name := strings.TrimSuffix(strings.TrimSpace(lines[0]), ":")
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		idx := strings.Index(trimmed, ":")
		if idx < 0 {
			t.Fatalf("節の行に `:` がありません: %q", line)
		}
		out = append(out, struct {
			path  []string
			value string
		}{
			path:  []string{name, strings.TrimSpace(trimmed[:idx])},
			value: strings.TrimSpace(trimmed[idx+1:]),
		})
	}
	return out
}

// setFrontMatterValue は front matter の1行の値を差し替える。
//
// **キーの行を組み立て直すだけである。**行の右側のコメントは落とすが、キーは動かさない。
// **入れ子は行頭のインデントで辿る**（internal/scaffold の findKeyLine と同じ判断）。
//
// t: 呼び出し元のテスト。
// content: WORKFLOW.md の全文。
// path: 差し替えるキーのパス（`["herdr", "socket"]` など）。
// value: 書き込む値（YAML としてそのまま書く）。
// 戻り値: 差し替えた全文。
func setFrontMatterValue(t *testing.T, content string, path []string, value string) string {
	t.Helper()
	lines := strings.Split(content, "\n")

	// front matter の範囲を切る。**本文にも似た形の行があるので、範囲を切らないと本文を書き換える。**
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") == "---" {
			end = i
			break
		}
	}
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" || end < 0 {
		t.Fatalf("front matter を切り出せません")
	}

	lo, parentIndent, childIndent, found := 1, -1, -1, -1
	for _, key := range path {
		idx := -1
		for i := lo; i < end; i++ {
			trimmed := strings.TrimLeft(lines[i], " \t")
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			indent := len(lines[i]) - len(trimmed)
			if indent <= parentIndent {
				break
			}
			if childIndent < 0 {
				childIndent = indent
			}
			if indent != childIndent || !strings.HasPrefix(trimmed, key+":") {
				continue
			}
			idx = i
			break
		}
		if idx < 0 {
			t.Fatalf("雛形に %s がありません", strings.Join(path, "."))
		}
		found = idx
		parentIndent = childIndent
		childIndent = -1
		lo = idx + 1
	}

	indent := lines[found][:len(lines[found])-len(strings.TrimLeft(lines[found], " \t"))]
	lines[found] = indent + path[len(path)-1] + ": " + value
	return strings.Join(lines, "\n")
}

// Options は doctor.Run へ渡す入力を組み立てる。
//
// 戻り値: 偽のサーバと一時ディレクトリだけを見る入力。
func (fx *fixture) Options() doctor.Options {
	opts := doctor.Options{
		ConfigPath:      fx.WorkflowPath,
		GraphQLEndpoint: fx.GitHub.URL,
		HomeDir:         fx.Home,
		LookupEnv: func(key string) (string, bool) {
			v, ok := fx.Env[key]
			return v, ok
		},
		// **本物の PATH を見ない。**見ると、検査の結果が
		// 「テストを走らせたマシンに claude が入っているか」で変わる。
		// **無い状態を試す test は fx.ClaudePath を空にすること。**
		LookPath: func(file string) (string, error) {
			if fx.ClaudePath == "" {
				return "", exec.ErrNotFound
			}
			return fx.ClaudePath, nil
		},
	}
	if fx.GhqPaths != nil {
		paths := fx.GhqPaths
		opts.GhqList = func(_ context.Context, owner, repo string) (string, error) {
			return paths[owner+"/"+repo], nil
		}
	}
	if fx.CheckTimeout > 0 {
		opts.CheckTimeout = fx.CheckTimeout
	}
	return opts
}

// writeFakeSecurity は PATH の先頭へ置く偽の `security` を作る。
//
// **本物の `security` を実行しない。**本物を叩くと、テストの実行中に Keychain の
// 確認のダイアログが出て、答える人がいないまま止まる。
//
// t: 呼び出し元のテスト。
// dir: 実行ファイルを置くディレクトリ（fixture の BinDir。PATH の先頭にある）。
// script: `security` として実行させるシェルスクリプトの中身（`#!/bin/sh` の次の行から）。
func writeFakeSecurity(t *testing.T, dir, script string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("テスト用security mock を置くディレクトリを作れません: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "security"), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("テスト用security mock を書けません: %v", err)
	}
}

// Run は doctor を1回走らせる。
//
// t: 呼び出し元のテスト。
// 戻り値: 検査結果。
func (fx *fixture) Run(t *testing.T) doctor.Report {
	t.Helper()
	return doctor.Run(context.Background(), fx.Options())
}

// writeTrustFile は `~/.claude.json` に信頼の登録を書く。
//
// **continuo はこのファイルを書き換えない**（設計 4-3）。テストが先に置いておく。
//
// t: 呼び出し元のテスト。
// home: ホームディレクトリ。
// repoDir: clone の絶対パス。
// accepted: 信頼ダイアログを承認済みにするかどうか。
func writeTrustFile(t *testing.T, home, repoDir string, accepted bool) {
	t.Helper()
	toplevel := runGit(t, repoDir, "rev-parse", "--path-format=absolute", "--show-toplevel")
	doc := map[string]any{
		"projects": map[string]any{
			toplevel: map[string]any{"hasTrustDialogAccepted": accepted},
		},
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("~/.claude.json を JSON 化できません: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), encoded, 0o600); err != nil {
		t.Fatalf("~/.claude.json を書けません: %v", err)
	}
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

// ===== 検査結果の取り出し =====

// resultOf は見出し語に対応する検査結果を取り出す。
//
// t: 呼び出し元のテスト。見つからなければテストを止める。
// report: 検査結果。
// label: 見出し語。
// 戻り値: その見出し語の検査結果。
func resultOf(t *testing.T, report doctor.Report, label i18n.Key) doctor.Result {
	t.Helper()
	for _, res := range report.Results {
		if res.Label == label {
			return res
		}
	}
	t.Fatalf("見出し語 %q の検査結果がありません（あるのは %v）", doctor.LabelText(label), labelsOf(report))
	return doctor.Result{}
}

// labelsOf は検査結果の見出し語のキーを並んだ順に返す。
//
// **Result が持つのはキーであって、画面に出る語ではない**（設計 3-35）。
//
// report: 検査結果。
// 戻り値: 見出し語のキーの並び。
func labelsOf(report doctor.Report) []i18n.Key {
	out := make([]i18n.Key, 0, len(report.Results))
	for _, res := range report.Results {
		out = append(out, res.Label)
	}
	return out
}

// assertSymbol は見出し語の記号が期待どおりかを確かめる。
//
// t: 呼び出し元のテスト。
// report: 検査結果。
// label: 見出し語。
// want: 期待する記号。
// 戻り値: その見出し語の検査結果。
func assertSymbol(t *testing.T, report doctor.Report, label i18n.Key, want doctor.Symbol) doctor.Result {
	t.Helper()
	res := resultOf(t, report, label)
	if res.Symbol != want {
		t.Fatalf("%s の記号が %s ではなく %s だった（説明: %s / 内訳: %v）",
			doctor.LabelText(label), want, res.Symbol, res.Detail, res.Notes)
	}
	return res
}

// renderReport は検査結果を文字列にする（出力の中身を確かめるため）。
//
// t: 呼び出し元のテスト。
// report: 検査結果。
// 戻り値: 出力した文字列。
func renderReport(t *testing.T, report doctor.Report) string {
	t.Helper()
	var b strings.Builder
	if err := report.Write(&b); err != nil {
		t.Fatalf("検査結果を書き出せません: %v", err)
	}
	return b.String()
}
