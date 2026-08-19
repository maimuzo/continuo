package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/workspace"
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
	// Herdr は偽 herdr である。
	Herdr *fakeHerdr
	// GitHub は偽 GitHub である。
	GitHub *fakeGitHub
	// Binary はビルドした continuo の絶対パスである。
	Binary string
	// BinDir は偽の gh / ghq を置いたディレクトリである。
	BinDir string
	// Home は子プロセスへ渡す HOME である。
	Home string
	// WorkflowPath は WORKFLOW.md の絶対パスである。
	WorkflowPath string
	// Timeline は偽 herdr と偽 GitHub の呼び出しを混ぜた1本の並びである。
	Timeline *timeline
}

// newDaemonEnv は偽 herdr・偽 GitHub・本物の git のリポジトリ・WORKFLOW.md を用意し、
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

	// 本物の git のリポジトリ（worktree の作成と削除は偽物では確かめられない）。
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
	content := fmt.Sprintf(`---
tracker:
  provider:
    owner: maimuzo
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
  turn_timeout_ms: 20000
  read_timeout_ms: 3000
  startup_timeout_ms: 3000
  stall_timeout_ms: 600000
herdr:
  socket: %s
  protocol: 19
cleanup:
  require_clean_worktree: false
  require_pushed: false
rate_limit:
  source: none
---

{{.issue.identifier}} を実装してください。

    gh issue view {{.issue.url}} --comments

作業の区切りがついたら CONTINUO-STATUS: の行を1行書いてください。
`, e.WorktreeRoot, e.Herdr.SocketPath)

	if err := os.WriteFile(e.WorkflowPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
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

	branch := fmt.Sprintf("continuo/maimuzo/koetsumugi/%d", number)
	slug := strings.ReplaceAll(branch, "/", "-")
	path := filepath.Join(e.WorktreeRoot, "github.com", "maimuzo", "koetsumugi", slug)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("worktree の置き場所を作れません: %v", err)
	}
	runGit(t, e.RepoDir, "worktree", "add", "--quiet", "-b", branch, path, "main")

	identity := workspace.Identity{
		IssueURL:         fmt.Sprintf("https://github.com/maimuzo/koetsumugi/issues/%d", number),
		IssueIdentifier:  fmt.Sprintf("maimuzo/koetsumugi#%d", number),
		ProjectItemID:    fmt.Sprintf("PVTI_item%d", number),
		Branch:           branch,
		HerdrWorkspaceID: workspaceID,
		SocketPath:       e.SocketPath,
		SettingsPath:     "",
		AgentName:        fmt.Sprintf("continuo-koetsumugi-%d", number),
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

	logs := &syncBuffer{}
	cmd := exec.Command(e.Binary, "--log-level=debug", e.WorkflowPath)
	cmd.Dir = e.Root
	cmd.Env = []string{
		"PATH=" + e.BinDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + e.Home,
		"CONTINUO_RUNTIME_DIR=" + e.RuntimeDir,
		"CONTINUO_GITHUB_GRAPHQL_ENDPOINT=" + e.GitHub.URL,
		"CONTINUO_TEST_TOKEN=dummy-token-for-the-fake-server",
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

// TestDaemon_復元を終えてから巡回が始まり1件のissueが通る は、
// **ビルドしたバイナリを実際に起動して**第7段階の受け入れの基準を1本で通す。
//
// 目的:
//   - 起動の順序（設定 → `flock` → 3-6 の起動時検査 → 復元 → 巡回）を守ること
//   - **復元を終えてから巡回が始まること**
//   - 引き継いだ1件の issue が、継続の指示 → turn の終わり → 片付けまで通ること
//   - `SIGTERM` で、巡回を止め・hook の受け口を閉じ・turn ループの終了を待って抜けること
//   - **終了時に pane を閉じないこと**（次の起動で引き継ぐ）
//
// 与える情報:
//   - 偽 herdr（実 herdr には繋がない）と偽 GitHub（本番のボードへは接続しない）
//   - `In Progress` の issue が2件。どちらも worktree と身元ファイルがディスクにある
//   - issue #188 の pane は `idle`、issue #189 の pane は `working`
//   - `agent.prompt` を受けたら、エージェントが実装して push しコメントを書き、
//     Status を `Done` へ動かして turn を終えた状態を作り、`Stop` を socket へ送る
//
// 成功条件:
//   - 復元の `pane.list` が、巡回の候補の取得より前に起きる
//   - #188 へ送られるのは継続の指示（5-4）であり、1回目の本文（5-3）ではない
//   - #188 の worktree と branch が実際に消える
//   - **#189（working）へは turn を送らず、その pane を最後まで閉じない**
//   - `SIGTERM` を送ると 20 秒以内に終了コード 0 で終わる
func TestDaemon_復元を終えてから巡回が始まり1件のissueが通る(t *testing.T) {
	env := newDaemonEnv(t)
	env.GitHub = newFakeGitHub(t, "maimuzo", env.Timeline,
		&boardItem{ItemID: "PVTI_item188", NodeID: "I_node188", Number: 188, State: "In Progress"},
		&boardItem{ItemID: "PVTI_item189", NodeID: "I_node189", Number: 189, State: "In Progress"},
	)

	path188 := env.prepareRun(t, 188, "w188", "sess-188")
	path189 := env.prepareRun(t, 189, "w189", "sess-189")

	// 生きている pane の台本。**#189 は working なので turn を送ってはならない。**
	env.Herdr.Handle("pane.list", func(params map[string]any) (any, *rpcErr) {
		wsID, _ := params["workspace_id"].(string)
		panes := []any{}
		add := func(paneID, workspaceID, cwd, status, session string) {
			if wsID != "" && wsID != workspaceID {
				return
			}
			panes = append(panes, map[string]any{
				"pane_id": paneID, "workspace_id": workspaceID, "cwd": cwd,
				"agent_status": status, "agent": "claude",
				"agent_session": map[string]any{
					"source": "herdr:claude", "agent": "claude", "kind": "id", "value": session,
				},
			})
		}
		add("p-188", "w188", path188, "idle", "sess-188")
		add("p-189", "w189", path189, "working", "sess-189")
		return map[string]any{"type": "pane_list", "panes": panes}, nil
	})
	env.Herdr.Handle("agent.list", func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "agent_list", "agents": []any{
			map[string]any{
				"name": "continuo-koetsumugi-188", "agent": "claude", "agent_status": "idle",
				"pane_id": "p-188", "tab_id": "t1", "workspace_id": "w188",
				"terminal_id": "term1", "focused": false, "revision": 1,
			},
			map[string]any{
				"name": "continuo-koetsumugi-189", "agent": "claude", "agent_status": "working",
				"pane_id": "p-189", "tab_id": "t2", "workspace_id": "w189",
				"terminal_id": "term1", "focused": false, "revision": 1,
			},
		}}, nil
	})

	transcriptDir := filepath.Join(env.Root, "tr")
	if err := os.MkdirAll(transcriptDir, 0o700); err != nil {
		t.Fatalf("transcript の置き場所を作れません: %v", err)
	}
	transcriptPath := filepath.Join(transcriptDir, "sess-188.jsonl")

	promptedText := &stringBox{}
	env.Herdr.Handle("agent.prompt", func(params map[string]any) (any, *rpcErr) {
		target, _ := params["target"].(string)
		if target != "continuo-koetsumugi-188" {
			t.Errorf("引き継いだあと turn を送ってはならない相手へ送った: %q", target)
		}
		text, _ := params["text"].(string)
		promptedText.Set(text)

		// エージェントが作業を終えた状態を作る。
		writeTranscript(t, transcriptPath)
		env.GitHub.AddComment("I_node188", "<!-- continuo:agent -->\n実装して push しました")
		// **完了の真実の源はボードである。**エージェントが gh で Done へ動かした状況にする。
		env.GitHub.SetState("PVTI_item188", "Done")
		// `continuo hook` と同じ1行を socket へ書く。
		sendHook(t, env.SocketPath, map[string]any{
			"session_id": "sess-188", "transcript_path": transcriptPath,
			"cwd": path188, "hook_event_name": "Stop",
			"background_tasks": []any{}, "stop_hook_active": false,
		})
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": target, "agent_status": "idle"},
		}, nil
	})

	cmd, logs := env.start(t)
	// 失敗したときと `-v` のときは、子プロセスのログを見せる
	// （**起動の順序を人間が目で確かめられるようにする**）。
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("continuo の出力:\n%s", logs.String())
		}
	})

	// #188 の片付けまで通ることを待つ（worktree の実体が消える）。
	waitFor(t, 60*time.Second, "引き継いだ issue の worktree が片付く", func() bool {
		_, err := os.Stat(path188)
		return os.IsNotExist(err)
	})

	// **復元を終えてから巡回が始まる。**復元の pane.list が候補の取得より前にある。
	entries := env.Timeline.Entries()
	paneListAt := env.Timeline.IndexOfPrefix("herdr.pane.list")
	byIDsAt := env.Timeline.IndexOfPrefix("gql.by_ids")
	candidatesAt := env.Timeline.IndexOfPrefix(`gql.items(status:"Ready","In Progress")`)
	if paneListAt < 0 || byIDsAt < 0 || candidatesAt < 0 {
		t.Fatalf("復元と巡回の呼び出しが揃っていない: %v\n%s", entries, logs.String())
	}
	if !(byIDsAt < candidatesAt && paneListAt < candidatesAt) {
		t.Fatalf("巡回が復元より先に始まっている: %v", entries)
	}

	// 送ったのは継続の指示（5-4）である。1回目の本文（5-3）ではない。
	if got := promptedText.Get(); !strings.HasPrefix(got, "続けてください。") {
		t.Fatalf("継続の指示ではない本文を送った: %q", got)
	}
	if got := promptedText.Get(); strings.Contains(got, "を実装してください") {
		t.Fatalf("1回目の本文（5-3）を送り直してしまった: %q", got)
	}

	// branch も片付いている（**worktree の削除の直後に消すので、少し待つ**）。
	waitFor(t, 20*time.Second, "片付けで branch が消える", func() bool {
		branches := runGit(t, env.RepoDir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
		return !strings.Contains(branches, "continuo/maimuzo/koetsumugi/188")
	})

	// **working の run へは turn を送らない。**
	if got := promptedText.Get(); strings.Contains(got, "189") {
		t.Fatalf("working の run へ turn を送った: %q", got)
	}
	if _, err := os.Stat(path189); err != nil {
		t.Fatalf("working の run の worktree を消してしまった: %v", err)
	}

	// ここまでに閉じた pane は #188 のものだけである（run が終わったときの pane.close）。
	closedBefore := env.Herdr.ClosedPanes()
	for _, id := range closedBefore {
		if id == "p-189" {
			t.Fatalf("引き継いだまま走っている run の pane を閉じた: %v", closedBefore)
		}
	}

	// SIGTERM で終了する。**pane は閉じない。**
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

	closedAfter := env.Herdr.ClosedPanes()
	if len(closedAfter) != len(closedBefore) {
		t.Fatalf("終了時に pane を閉じた（次の起動で引き継げなくなる）: before=%v after=%v",
			closedBefore, closedAfter)
	}
	if _, err := os.Stat(path189); err != nil {
		t.Fatalf("終了時に worktree を消してしまった: %v", err)
	}

	// 終了の作法がログに残っている（巡回を止め → hook の受け口を閉じ → turn ループを待つ）。
	out := logs.String()
	for _, want := range []string{"巡回を止めました", "走行中の turn ループが終わりました"} {
		if !strings.Contains(out, want) {
			t.Fatalf("終了の作法がログに出ていない（%q が無い）:\n%s", want, out)
		}
	}
}

// TestDaemon_flockが取れなければ即座に終了する は、二重起動の防止を確かめる。
//
// 目的: 設計 3-17。**continuo の状態はメモリにしかないので、2つ目のプロセスが立つと
// 1つ目が処理中の issue を平気で掴む。**
//
// 与える情報: 1つ目が走っている状態で、同じ設定で2つ目を起動する。
//
// 成功条件: 2つ目が 20 秒以内に終了コード 1 で終わり、二重起動を検出したと出る。
// **2つ目は pane を1つも閉じない。**
func TestDaemon_flockが取れなければ即座に終了する(t *testing.T) {
	env := newDaemonEnv(t)
	env.GitHub = newFakeGitHub(t, "maimuzo", env.Timeline,
		&boardItem{ItemID: "PVTI_item188", NodeID: "I_node188", Number: 188, State: "In Review"},
	)
	env.Herdr.Handle("pane.list", func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "pane_list", "panes": []any{}}, nil
	})
	env.Herdr.Handle("agent.list", func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "agent_list", "agents": []any{}}, nil
	})

	first, firstLogs := env.start(t)
	waitFor(t, 30*time.Second, "1つ目が巡回を始める", func() bool {
		return strings.Contains(firstLogs.String(), "巡回を始めます")
	})

	second, secondLogs := env.start(t)
	code, finished := waitProcess(context.Background(), second, 20*time.Second)
	if !finished {
		t.Fatalf("2つ目が終了しなかった（即座に終わるべきである）\n%s", secondLogs.String())
	}
	if code != 1 {
		t.Fatalf("2つ目の終了コードが 1 ではない: got %d\n%s", code, secondLogs.String())
	}
	if !strings.Contains(secondLogs.String(), "二重起動") {
		t.Fatalf("二重起動を検出したことがログに出ていない:\n%s", secondLogs.String())
	}
	if ids := env.Herdr.ClosedPanes(); len(ids) != 0 {
		t.Fatalf("起動を止めたのに pane を閉じた: %v", ids)
	}

	_ = first.Process.Signal(syscall.SIGTERM)
	if _, ok := waitProcess(context.Background(), first, 20*time.Second); !ok {
		t.Fatalf("1つ目が終了しなかった\n%s", firstLogs.String())
	}
}

// TestDaemon_起動時の検査に落ちたら生きているpaneを閉じずに起動を止める は、
// 設計 3-4 の「起動から復元までの順序」の段3 を確かめる。
//
// 目的: **設定の誤りで、動いているエージェントの作業を殺さない。**落ちる原因は
// continuo 側の前提が揃っていないことであって、エージェントの側の問題ではない。
// 人間が直して起動し直せば、復元の段5 で引き継げる。
//
// 与える情報: `herdr.protocol` が設定と一致しない偽 herdr と、生きている pane。
//
// 成功条件: 終了コード 1 で起動を止め、**`pane.close` を1回も呼ばない。**
// **復元（`pane.list`）にも進まない。**
func TestDaemon_起動時の検査に落ちたら生きているpaneを閉じずに起動を止める(t *testing.T) {
	env := newDaemonEnv(t)
	env.GitHub = newFakeGitHub(t, "maimuzo", env.Timeline,
		&boardItem{ItemID: "PVTI_item188", NodeID: "I_node188", Number: 188, State: "In Progress"},
	)
	path188 := env.prepareRun(t, 188, "w188", "sess-188")
	// **protocol が合わない。**起動時の検査で止まるべきである。
	env.Herdr.Handle("ping", func(map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type": "pong", "version": "0.9.0-fake", "protocol": 21,
			"capabilities": map[string]any{"live_handoff": true},
		}, nil
	})
	env.Herdr.Handle("pane.list", func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "pane_list", "panes": []any{
			map[string]any{
				"pane_id": "p-188", "workspace_id": "w188", "cwd": path188,
				"agent_status": "idle", "agent": "claude",
			},
		}}, nil
	})

	cmd, logs := env.start(t)
	code, finished := waitProcess(context.Background(), cmd, 30*time.Second)
	if !finished {
		t.Fatalf("起動時の検査に落ちたのに終了しなかった\n%s", logs.String())
	}
	if code != 1 {
		t.Fatalf("終了コードが 1 ではない: got %d\n%s", code, logs.String())
	}
	if ids := env.Herdr.ClosedPanes(); len(ids) != 0 {
		t.Fatalf("起動を止めたのに生きている pane を閉じた: %v", ids)
	}
	if n := env.Herdr.CountMethod("pane.list"); n != 0 {
		t.Fatalf("起動時の検査に落ちたのに復元へ進んだ（pane.list を %d 回呼んだ）", n)
	}
	if _, err := os.Stat(path188); err != nil {
		t.Fatalf("起動を止めたのに worktree を消した: %v", err)
	}
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
