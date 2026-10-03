package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/workspace"
	"github.com/maimuzo/continuo/test/testlang"
)

// daemonEnv はバイナリを起動するための一式である。
type daemonEnv struct {
	// Root は一時ディレクトリの根である（socket のパスを短く保つため浅くする）。
	Root string
	// RuntimeDir は実行時ディレクトリである（socket・逃がし先・ロックファイルの置き場所）。
	RuntimeDir string
	// SocketPath は hook を受ける socket の絶対パスである。
	SocketPath string
	// WorktreeRoot は worktree の置き場所である。
	WorktreeRoot string
	// RepoDir は本物の git の clone である。
	RepoDir string
	// Herdr はテスト用herdr mock である。
	Herdr *fakeHerdr
	// GitHub はテスト用GitHub mock である。
	GitHub *fakeGitHub
	// Binary はビルドした continuo の絶対パスである。
	Binary string
	// BinDir はテスト用gh / ghq mock を置いたディレクトリである。
	BinDir string
	// Home は子プロセスへ渡す HOME である。
	Home string
	// WorkflowPath は WORKFLOW.md の絶対パスである。
	WorkflowPath string
	// ServerPort は WORKFLOW.md に書く `server.port` である。
	// **nil なら書かない**（既定どおりダッシュボードを開かない）。
	ServerPort *int
	// HerdrReadTimeoutMs は WORKFLOW.md に書く `herdr.read_timeout_ms` である。
	// **0 なら 3000 を書く。**相手は herdr の socket API の応答である（設計 8-1）。
	HerdrReadTimeoutMs int
	// Timeline はテスト用herdr mock とテスト用GitHub mock の呼び出しを混ぜた1本の並びである。
	Timeline *timeline
}

// newDaemonEnv はテスト用herdr mock・テスト用GitHub mock・本物の git のリポジトリ・WORKFLOW.md を用意し、
// continuo のバイナリをビルドする。
//
// t: 呼び出し元のテスト。
// 戻り値: 起動に必要な一式。
func newDaemonEnv(t *testing.T) *daemonEnv {
	t.Helper()

	// **socket のパスを短く保つ**（macOS の Unix domain socket の上限は103バイト）。
	root, err := os.MkdirTemp("", "cd")
	if err != nil {
		t.Fatalf("一時ディレクトリを作れません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("一時ディレクトリを解決できません: %v", err)
	}

	runtimeDir := filepath.Join(root, "rt")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("実行時ディレクトリを作れません: %v", err)
	}
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("ホームディレクトリを作れません: %v", err)
	}
	binDir := filepath.Join(root, "bin")

	tl := &timeline{}
	fh := newFakeHerdr(t, root, tl)

	// 本物の git のリポジトリ（worktree の作成と削除はmockでは確かめられない）。
	origin := filepath.Join(root, "origin.git")
	runGit(t, "", "init", "--quiet", "--bare", "--initial-branch=main", origin)
	repoDir := filepath.Join(root, "repo")
	runGit(t, "", "clone", "--quiet", origin, repoDir)
	runGit(t, repoDir, "config", "user.email", "continuo@example.test")
	runGit(t, repoDir, "config", "user.name", "continuo test")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("初期の中身\n"), 0o644); err != nil {
		t.Fatalf("初期ファイルを書けません: %v", err)
	}
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "--quiet", "-m", "初期コミット")
	runGit(t, repoDir, "push", "--quiet", "-u", "origin", "main")
	fh.SetRepoDir(repoDir)
	writeFakeGH(t, binDir, repoDir)
	writeTrustFile(t, home, repoDir)

	worktreeRoot := filepath.Join(root, "wt")
	env := &daemonEnv{
		Root:         root,
		RuntimeDir:   runtimeDir,
		SocketPath:   filepath.Join(runtimeDir, "hooks.sock"),
		WorktreeRoot: worktreeRoot,
		RepoDir:      repoDir,
		Herdr:        fh,
		Binary:       buildBinary(t, root),
		BinDir:       binDir,
		Home:         home,
		WorkflowPath: filepath.Join(root, "WORKFLOW.md"),
		Timeline:     tl,
	}
	env.writeWorkflow(t)
	return env
}

// writeWorkflow は WORKFLOW.md を書く。
//
// **書かないキーは既定値のままにする**（設計 5-2 の既定値）。
// 待ち時間だけをテスト向けに短くしてある（判定の意味は変えない）。
//
// t: 呼び出し元のテスト。
func (e *daemonEnv) writeWorkflow(t *testing.T) {
	t.Helper()
	// 書き出す値の順序は content の %%d / %%s の並びに合わせてある。
	content := fmt.Sprintf(`---
tracker:
  provider:
    owner: octocat
    project_number: 3
    status_field: Status
    token_source: env
    token_env: CONTINUO_TEST_TOKEN
polling:
  interval_ms: 300
workspace:
  root: %s
claude:
  poll_wait_ms: 300
  settle_ms: 200
  turn_timeout_ms: 600000
herdr:
  socket: %s
  protocol: 22
  read_timeout_ms: %d
  startup_timeout_ms: 3000
cleanup:
  require_clean_worktree: false
  require_pushed: false
rate_limit:
  source: none
%s---

{{.issue.identifier}} を実装してください。

    gh issue view {{.issue.url}} --comments

作業の区切りがついたら CONTINUO-STATUS: の行を1行書いてください。
`, e.WorktreeRoot, e.Herdr.SocketPath, readTimeoutMs(e.HerdrReadTimeoutMs), serverSection(e.ServerPort))

	if err := os.WriteFile(e.WorkflowPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
}

// readTimeoutMs は WORKFLOW.md に書く `herdr.read_timeout_ms` を決める。
//
// value: daemonEnv に設定された値（0 なら既定を使う）。
// 戻り値: 書き出す値。
func readTimeoutMs(value int) int {
	if value <= 0 {
		return 3000
	}
	return value
}

// prepareRun は「continuo が落ちる前に着手の段6 まで進んでいた」状態をディスクの上に作る。
//
// 本物の git で worktree を切り、その中に身元ファイルを置く。
//
// t: 呼び出し元のテスト。
// number: issue 番号。
// workspaceID: herdr の workspace の ID（片付けの `worktree.remove` が要求する）。
// sessionUUID: Claude Code のセッション UUID。
// 戻り値: 作った worktree の絶対パス。
func (e *daemonEnv) prepareRun(t *testing.T, number int, workspaceID, sessionUUID string) string {
	t.Helper()

	branch := fmt.Sprintf("continuo/octocat/hello-world/%d", number)
	slug := strings.ReplaceAll(branch, "/", "-")
	path := filepath.Join(e.WorktreeRoot, "github.com", "octocat", "hello-world", slug)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("worktree の置き場所を作れません: %v", err)
	}
	runGit(t, e.RepoDir, "worktree", "add", "--quiet", "-b", branch, path, "main")

	identity := workspace.Identity{
		IssueURL:         fmt.Sprintf("https://github.com/octocat/hello-world/issues/%d", number),
		IssueIdentifier:  fmt.Sprintf("octocat/hello-world#%d", number),
		ProjectItemID:    fmt.Sprintf("PVTI_item%d", number),
		Branch:           branch,
		HerdrWorkspaceID: workspaceID,
		SocketPath:       e.SocketPath,
		SettingsPath:     "",
		AgentName:        fmt.Sprintf("continuo-hello-world-%d", number),
		SessionUUID:      sessionUUID,
		CreatedAt:        time.Now(),
	}
	encoded, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		t.Fatalf("身元ファイルを JSON 化できません: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, ".continuo.json"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatalf("身元ファイルを書けません: %v", err)
	}
	e.Herdr.RegisterWorkspace(workspaceID, path)
	return path
}

// start は continuo のバイナリを起動する。
//
// **環境変数は明示的に組み立てる。**本物の `HERDR_SOCKET_PATH` や `GH_TOKEN` を
// 継承させないためである。
//
// t: 呼び出し元のテスト。
// 戻り値の1つ目: 起動したプロセス。
// 戻り値の2つ目: 標準出力と標準エラーを溜める先（失敗したときに中身を出す）。
func (e *daemonEnv) start(t *testing.T) (*exec.Cmd, *syncBuffer) {
	t.Helper()
	return e.startWithArgs(t)
}

// startWithArgs は追加の引数を渡して continuo のバイナリを起動する。
//
// t: 呼び出し元のテスト。
// extra: `--log-level` と WORKFLOW.md のパスの前に置く追加の引数（`--port` など）。
// 戻り値の1つ目: 起動したプロセス。
// 戻り値の2つ目: 標準出力と標準エラーを溜める先。
func (e *daemonEnv) startWithArgs(t *testing.T, extra ...string) (*exec.Cmd, *syncBuffer) {
	t.Helper()
	args := append(append([]string{}, extra...), "--log-level=debug", e.WorkflowPath)
	return e.startProgram(t, e.Binary, args)
}

// startIgnoringSIGINT は **`SIGINT` を「無視」に設定した親から** continuo を起動する。
//
// **`nohup` / `setsid` / job control の無いシェルのバックグラウンド起動が作る状態である。**
// この状態だと、`signal.Stop` で「元の動作」へ戻すやり方は戻る先が「無視」になり、
// **2回目以降の Ctrl+C が何も起こさない。**その筋を実際に踏むための入口である。
//
// `trap "" INT` は `SIGINT` を SIG_IGN にし、**`exec` を跨いでも残る**（POSIX）。
// `exec` するので、返る `*exec.Cmd` の PID は continuo 自身の PID である。
//
// t: 呼び出し元のテスト。
// extra: `--log-level` と WORKFLOW.md のパスの前に置く追加の引数。
// 戻り値の1つ目: 起動したプロセス。
// 戻り値の2つ目: 標準出力と標準エラーを溜める先。
func (e *daemonEnv) startIgnoringSIGINT(t *testing.T, extra ...string) (*exec.Cmd, *syncBuffer) {
	t.Helper()
	args := append(append([]string{}, extra...), "--log-level=debug", e.WorkflowPath)
	shArgs := append([]string{"-c", `trap "" INT; exec "$0" "$@"`, e.Binary}, args...)
	return e.startProgram(t, "/bin/sh", shArgs)
}

// startProgram は用意した環境変数で任意のコマンドを起動する。
//
// t: 呼び出し元のテスト。
// name: 起動する実行ファイル。
// args: 渡す引数。
// 戻り値の1つ目: 起動したプロセス。
// 戻り値の2つ目: 標準出力と標準エラーを溜める先。
func (e *daemonEnv) startProgram(t *testing.T, name string, args []string) (*exec.Cmd, *syncBuffer) {
	t.Helper()

	logs := &syncBuffer{}
	cmd := exec.Command(name, args...)
	cmd.Dir = e.Root
	cmd.Env = []string{
		"PATH=" + e.BinDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + e.Home,
		"CONTINUO_RUNTIME_DIR=" + e.RuntimeDir,
		"CONTINUO_GITHUB_GRAPHQL_ENDPOINT=" + e.GitHub.URL,
		"CONTINUO_TEST_TOKEN=dummy-token-for-the-fake-server",
		testlang.EnvEntry(),
	}
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("continuo を起動できません: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	return cmd, logs
}

// writeTranscript はテスト用の transcript の JSONL を書く。
//
// **形は設計 3-25 / 3-15 の実測に合わせてある**（`promptSource` / `isSidechain` /
// `message.content[].text` / `requestId` / `message.usage`）。
//
// t: 呼び出し元のテスト。**接続ごとの goroutine から呼ばれうるので t.Errorf を使う。**
// path: 書き出すファイルの絶対パス。
func writeTranscript(t *testing.T, path string) {
	lines := []any{
		map[string]any{
			"type": "user", "promptSource": "typed", "promptId": "p1", "isSidechain": false,
			"message": map[string]any{"content": "続けてください。"},
		},
		map[string]any{
			"type": "assistant", "isSidechain": false, "requestId": "req1",
			"message": map[string]any{
				"content": []any{map[string]any{
					"type": "text",
					"text": "実装して commit と push をしました。\n\nCONTINUO-STATUS: review",
				}},
				"usage": map[string]any{
					"input_tokens": 10, "cache_creation_input_tokens": 20,
					"cache_read_input_tokens": 30, "output_tokens": 40,
				},
			},
		},
	}
	var b strings.Builder
	for _, line := range lines {
		encoded, err := json.Marshal(line)
		if err != nil {
			t.Errorf("transcript の行を JSON 化できません: %v", err)
			return
		}
		b.Write(encoded)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Errorf("transcript を書けません: %v", err)
	}
}

// serverSection は WORKFLOW.md に書く `server` 節を作る。
//
// port: 書く `server.port`。nil なら節そのものを書かない。
// 戻り値: front matter へ挿し込む文字列（nil なら空文字）。
func serverSection(port *int) string {
	if port == nil {
		return ""
	}
	return fmt.Sprintf("server:\n  port: %d\n", *port)
}

// dashboardAddr はログに出た「ダッシュボードを開きました」の待ち受け先を取り出す。
//
// **ポート番号をテストに書かないためである**（`--port=0` は OS に空きポートを選ばせる）。
//
// logs: continuo の出力。
// 戻り値の1つ目: `127.0.0.1:<ポート>`。
// 戻り値の2つ目: 見つかれば true。
func dashboardAddr(logs *syncBuffer) (string, bool) {
	for _, line := range strings.Split(logs.String(), "\n") {
		if !strings.Contains(line, "ダッシュボードを開きました") {
			continue
		}
		_, rest, ok := strings.Cut(line, "addr=")
		if !ok {
			continue
		}
		return strings.TrimSpace(strings.Fields(rest)[0]), true
	}
	return "", false
}

// TestDaemon_ダッシュボードが開けなくても起動を止めない は、
// 任意の機能の失敗が本体を止めないことを確かめる。
//
// 目的: 設計 5-2 / 8-2 と `SPEC.md` 13.7 の「ダッシュボードは orchestrator の正しさに
// 必要ではない」を守っていることを示す。**ここで起動を止めると、直前の復元で引き継いだ
// pane の Claude Code が誰にも見張られないまま残る。**
//
// 与える情報: 別のプロセスが既に掴んでいるポートを `server.port` に書いた WORKFLOW.md と、
// 引き継ぐ対象の worktree が1件。
//
// 成功条件: 起動が止まらず巡回まで進むこと。開けなかったことが警告としてログに出ること。
// `SIGTERM` で終了コード 0 で終わること。
func TestDaemon_ダッシュボードが開けなくても起動を止めない(t *testing.T) {
	// **先にポートを塞ぐ。**別のアプリが同じ番号を掴んでいる状況そのものである。
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ポートを塞げません: %v", err)
	}
	defer func() { _ = blocker.Close() }()
	_, portStr, err := net.SplitHostPort(blocker.Addr().String())
	if err != nil {
		t.Fatalf("塞いだポートを解釈できません: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("塞いだポートを数値にできません: %v", err)
	}

	env := newDaemonEnv(t)
	env.ServerPort = &port
	env.writeWorkflow(t)
	env.GitHub = newFakeGitHub(t, "octocat", env.Timeline,
		&boardItem{ItemID: "PVTI_item188", NodeID: "I_node188", Number: 188, State: "In Review"},
	)
	env.Herdr.Handle("pane.list", func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "pane_list", "panes": []any{}}, nil
	})
	env.Herdr.Handle("agent.list", func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "agent_list", "agents": []any{}}, nil
	})

	cmd, logs := env.start(t)
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("continuo の出力:\n%s", logs.String())
		}
	})

	waitFor(t, 30*time.Second, "ダッシュボードが開けなくても巡回まで進む", func() bool {
		return strings.Contains(logs.String(), "巡回を始めます")
	})
	if !strings.Contains(logs.String(), "ダッシュボードを開けないので、ダッシュボード無しで続けます") {
		t.Fatalf("開けなかったことが警告として出ていない:\n%s", logs.String())
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM を送れません: %v", err)
	}
	code, finished := waitProcess(context.Background(), cmd, 20*time.Second)
	if !finished {
		t.Fatalf("SIGTERM を受けても 20 秒以内に終了しなかった\n%s", logs.String())
	}
	if code != 0 {
		t.Fatalf("終了コードが 0 ではない: got %d\n%s", code, logs.String())
	}
}

// holdDashboardConnection は、応答を返し終えられない要求をダッシュボードへ1本ぶら下げる。
//
// **終了の1段目を必ず期限まで引き延ばすためにある。**要求行だけ送って空行を送らないと、
// その接続は「処理中」として数えられ、`http.Server.Shutdown` は待ちに入る。
// これが無いと後始末が一瞬で終わってしまい、**期限や2回目の Ctrl+C を確かめられない。**
//
// t: 呼び出し元のテスト。
// addr: ダッシュボードの待ち受け先（`127.0.0.1:<ポート>`）。
func holdDashboardConnection(t *testing.T, addr string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("ダッシュボードへ繋げません: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	// **空行を送らない。**要求はここで途切れたままになる。
	if _, err := conn.Write([]byte("GET /api/v1/state HTTP/1.1\r\nHost: 127.0.0.1\r\n")); err != nil {
		t.Fatalf("要求の途中まで送れません: %v", err)
	}
}

// TestDaemon_hookの受け口はherdrのread_timeout_msで接続を切らない は、
// 期限のつまみが相手ごとに分かれていることを確かめる。
//
// 目的: `herdr.read_timeout_ms` は **herdr の socket API の応答を待つ上限**である
// （設計 8-1。「`read_timeout_ms` 一本ですべてを打ち切ってはならない」）。これを hook の
// 受け口へ流用すると、herdr が遅い環境に合わせて値を上げたときに、hook の接続を
// 掴んだままにする時間まで一緒に動く。
//
// 与える情報: `herdr.read_timeout_ms: 200`（hookserver の既定 10 秒よりずっと短い）で
// 起動した continuo と、繋いだだけで何も送らない接続。
//
// 成功条件: 繋いでから 1 秒たっても受け口が接続を閉じないこと（読み出しが EOF ではなく
// 待ちで返ること）。そのあとに送った hook が受け付けられること。
func TestDaemon_hookの受け口はherdrのread_timeout_msで接続を切らない(t *testing.T) {
	env := newDaemonEnv(t)
	env.HerdrReadTimeoutMs = 200
	env.writeWorkflow(t)
	env.GitHub = newFakeGitHub(t, "octocat", env.Timeline)
	env.Herdr.Handle("pane.list", func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "pane_list", "panes": []any{}}, nil
	})
	env.Herdr.Handle("agent.list", func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "agent_list", "agents": []any{}}, nil
	})

	cmd, logs := env.start(t)
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("continuo の出力:\n%s", logs.String())
		}
	})
	waitFor(t, 30*time.Second, "巡回が始まる", func() bool {
		return strings.Contains(logs.String(), "巡回を始めます")
	})

	conn, err := net.Dial("unix", env.SocketPath)
	if err != nil {
		t.Fatalf("hook の受け口へ繋げません（%s）: %v", env.SocketPath, err)
	}
	defer func() { _ = conn.Close() }()

	// **`herdr.read_timeout_ms` の5倍待つ。**流用されていれば、ここで閉じられている。
	time.Sleep(time.Second)

	if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("読み出しの期限を設定できません: %v", err)
	}
	buf := make([]byte, 1)
	_, readErr := conn.Read(buf)
	var netErr net.Error
	switch {
	case readErr == nil:
		t.Fatal("何も送っていないのに応答が返った")
	case errors.As(readErr, &netErr) && netErr.Timeout():
		// **こちらの読み出しが待ちで返った＝受け口はまだ接続を持っている。**期待どおり。
	default:
		t.Fatalf("herdr.read_timeout_ms（%dms）で hook の接続が切られた: %v",
			env.HerdrReadTimeoutMs, readErr)
	}

	// 切られていないことの裏取りとして、この接続でそのまま hook を1件送れることを見る。
	if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("書き出しの期限を設定できません: %v", err)
	}
	line := `{"hook_event_name":"Stop","session_id":"sess-none","cwd":"` + env.WorktreeRoot + `"}` + "\n"
	if _, err := conn.Write([]byte(line)); err != nil {
		t.Fatalf("待たせたあとの接続へ hook を送れなかった: %v", err)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM を送れません: %v", err)
	}
	if code, finished := waitProcess(context.Background(), cmd, 20*time.Second); !finished || code != 0 {
		t.Fatalf("SIGTERM で正常に終わらなかった（finished=%v, code=%d）\n%s", finished, code, logs.String())
	}
}

// 目的: 接続先が github.com でないときに起動時のカンバンの読み取りが落ちたら、
// `continuo doctor` へ案内することを確認する（設計 3-86。issue #86）。
//
// **スキーマの照会は `continuo doctor` にしか置いていない**（人間の決定）。
// GitHub Enterprise Server 3.19 以下では、起動時の最初の問い合わせが GraphQL の誤りで落ちる。
// 「Status の選択肢名が設定と一致しません」だけを出すと、利用者は Status の名前を直しに行く。
//
// 与える情報: `tracker.provider.host` が `ghe.example.com` で、カンバンに無い Status 名を
// `failure_state` に書いた設定（起動時のカンバンの読み取りが落ちる）。
// 成功条件: 終了コード 1 で起動を止め、出力に接続先ホストと `continuo doctor` と「3.20」が出ること。
func TestDaemon_接続先がGHEで起動時にカンバンを読めなければdoctorへ案内する(t *testing.T) {
	env := newDaemonEnv(t)
	env.GitHub = newFakeGitHub(t, "octocat", env.Timeline)
	raw, err := os.ReadFile(env.WorkflowPath)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	content := strings.Replace(string(raw), "    status_field: Status\n",
		"    status_field: Status\n    host: ghe.example.com\n", 1)
	content = strings.Replace(content, "polling:\n", "  failure_state: カンバンに無い名前\npolling:\n", 1)
	if err := os.WriteFile(env.WorkflowPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	cmd, logs := env.start(t)
	code, finished := waitProcess(context.Background(), cmd, 30*time.Second)
	if !finished {
		t.Fatalf("起動時の検査に落ちたのに終了しなかった\n%s", logs.String())
	}
	if code != 1 {
		t.Fatalf("終了コードが 1 ではない: got %d\n%s", code, logs.String())
	}
	for _, want := range []string{"ghe.example.com", "continuo doctor", "3.20"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("出力に %q が無い:\n%s", want, logs.String())
		}
	}
}
