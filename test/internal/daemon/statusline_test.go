package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/daemon"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/socketpath"
)

// 使用率の出どころをステータスラインへ移したことに伴う、起動の段の結線を確かめる（issue #284）。

// writeStatuslineWorkflow は、rate_limit.source と hook の socket のパスを指定した
// 最小の WORKFLOW.md を書く（daemon.Run をそのまま呼ぶ結線のテスト用）。
//
// t: 呼び出し元のテスト。
// root: WORKFLOW.md を置くディレクトリ（herdr の socket もここに置く）。
// source: rate_limit.source に書く値。
// listen: claude.hook_bridge.listen に書く hook の socket の絶対パス。
// 戻り値: 書いた WORKFLOW.md の絶対パス。
func writeStatuslineWorkflow(t *testing.T, root, source, listen string) string {
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
  protocol: 22
claude:
  hook_bridge:
    listen: %s
rate_limit:
  source: %s
---

{{.issue.identifier}} を実装してください。
`, filepath.Join(root, "wt"), filepath.Join(root, "h.sock"), listen, source)
	path := filepath.Join(root, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
	return path
}

// longRuntimeDir は、`<dir>/h.sock` は 103 バイトに収まるが `<dir>/sl.sock` は 104 バイトになる
// 実行時ディレクトリを作る（hook の socket だけが通り、使用率の socket だけが長すぎる形）。
//
// t: 呼び出し元のテスト。
// root: 親にする短い一時ディレクトリ。
// 戻り値: 作ったディレクトリの絶対パス（長さは 96 バイト）。
func longRuntimeDir(t *testing.T, root string) string {
	t.Helper()
	const want = socketpath.MaxPathLen + 1 - len("/"+socketpath.StatuslineSocketFileName)
	pad := want - len(root) - 1
	if pad < 1 {
		t.Fatalf("一時ディレクトリが長すぎて境界を作れません: %q", root)
	}
	dir := filepath.Join(root, strings.Repeat("r", pad))
	if len(dir) != want {
		t.Fatalf("境界を作れていません: %d バイト（want %d）", len(dir), want)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("実行時ディレクトリを作れません: %v", err)
	}
	return dir
}

// 目的: 使用率の socket（sl.sock）のパスが長すぎると、`rate_limit.source: statusline` なら起動を止め、
// `none` と `oauth_usage_api` なら止めないことを確かめる（none は sl.sock を開かないので、長さで止める
// 理由が無い。oauth_usage_api は usage API が主なので、WARN を出して statusline を使わずに続ける。issue #284）。
// 与える情報: `<dir>/h.sock` は上限に収まり `<dir>/sl.sock` は 104 バイトになる実行時ディレクトリを
// claude.hook_bridge.listen で指定した設定。カンバンは応答を返さず、起動時検査の期限は 200ms。
// 成功条件: statusline では起動の段のエラーで止まり、文言に sl.sock のパスが出て、起動時検査に
// 進んでいないこと。none と oauth_usage_api では sl.sock の長さでは止まらず、起動時検査（カンバンの
// 無応答）で止まること。
func TestRun_sl_sockのパスが長すぎるとstatuslineなら起動を止めnoneとoauth_usage_apiなら止めない(t *testing.T) {
	for _, tc := range []struct {
		source    string
		wantSlErr bool
	}{
		{source: "statusline", wantSlErr: true},
		{source: "none", wantSlErr: false},
		{source: "oauth_usage_api", wantSlErr: false},
	} {
		t.Run(tc.source, func(t *testing.T) {
			root := wiringRoot(t)
			wiringHome(t)
			binDir := filepath.Join(root, "bin")
			writeFakeGH(t, binDir, root)
			newFakeHerdr(t, root, &timeline{})

			blocked := make(chan struct{})
			github := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				select {
				case <-blocked:
				case <-r.Context().Done():
				}
			}))
			t.Cleanup(github.Close)
			t.Cleanup(func() { close(blocked) })

			runtimeDir := longRuntimeDir(t, root)
			listen := filepath.Join(runtimeDir, "h.sock")
			if len(listen) > socketpath.MaxPathLen {
				t.Fatalf("hook の socket まで長すぎる: %d バイト", len(listen))
			}
			slPath := filepath.Join(runtimeDir, socketpath.StatuslineSocketFileName)
			if len(slPath) <= socketpath.MaxPathLen {
				t.Fatalf("sl.sock が上限に収まっている: %d バイト", len(slPath))
			}
			path := writeStatuslineWorkflow(t, root, tc.source, listen)

			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv(daemon.EnvRuntimeDir, "")
			t.Setenv(daemon.EnvGraphQLEndpoint, github.URL)
			t.Setenv("CONTINUO_TEST_TOKEN", "dummy-token-for-the-fake-server")

			err := daemon.Run(context.Background(), daemon.Options{
				ConfigPath:          path,
				Logger:              slog.New(slog.DiscardHandler),
				StartupCheckTimeout: 200 * time.Millisecond,
			})
			if err == nil {
				t.Fatal("起動できてしまった（どちらの場合も、どこかで止まるはず）")
			}
			if !errors.Is(err, daemon.ErrStartup) {
				t.Fatalf("起動の段の失敗として印が付いていない: %v", err)
			}
			mentionsSl := strings.Contains(err.Error(), slPath)
			if tc.wantSlErr {
				if !mentionsSl {
					t.Fatalf("sl.sock が長すぎることが文言に出ていない: %v", err)
				}
				if strings.Contains(err.Error(), "起動時の検査に落ちました") {
					t.Fatalf("sl.sock が長すぎるのに起動時検査まで進んだ: %v", err)
				}
				return
			}
			if mentionsSl {
				t.Fatalf("source: %s なのに sl.sock の長さで止まった: %v", tc.source, err)
			}
			if !strings.Contains(err.Error(), "起動時の検査に落ちました") {
				t.Fatalf("source: %s の起動が起動時検査まで進んでいない: %v", tc.source, err)
			}
		})
	}
}

// 目的: `sl.sock` を開けない（listen に失敗する）とき、`source: statusline` なら起動を止め、
// `source: oauth_usage_api` なら WARN を出して statusline を使わずに起動を続けることを、ビルドした
// バイナリで確かめる（issue #284。PR #294 の実装レビュー1周目）。listen は起動時検査のあとなので、
// 起動時検査を通る環境で回す。
// 与える情報: 実行時ディレクトリの `sl.sock` の場所に、中身のあるディレクトリ（残骸として消せないので
// listen まで進めない）。oauth_usage_api では、トークンを空の環境変数から読む設定（本物の Keychain も
// usage API も読まない）。カンバンは空。
// 成功条件: statusline では、巡回を始めずに0以外で終わり、出力に sl.sock のパスが出ること。
// oauth_usage_api では、「sl.sock）を開けない」の WARN を出して巡回を始め、SIGTERM で 0 で終わること。
func TestDaemon_sl_sockを開けないとstatuslineなら起動を止めoauth_usage_apiなら止めない(t *testing.T) {
	for _, tc := range []struct {
		name      string
		rateLimit string
		wantStop  bool
	}{
		{name: "statusline", rateLimit: "rate_limit:\n  source: statusline\n", wantStop: true},
		{
			name: "oauth_usage_api",
			rateLimit: "rate_limit:\n  source: oauth_usage_api\n  token_source: env\n" +
				"  token_env: CONTINUO_TEST_NO_SUCH_USAGE_TOKEN\n",
			wantStop: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newDaemonEnv(t)
			env.GitHub = newFakeGitHub(t, "octocat", env.Timeline)
			raw, err := os.ReadFile(env.WorkflowPath)
			if err != nil {
				t.Fatalf("WORKFLOW.md を読めません: %v", err)
			}
			content := strings.Replace(string(raw), "rate_limit:\n  source: none\n", tc.rateLimit, 1)
			if content == string(raw) {
				t.Fatal("WORKFLOW.md の rate_limit を書き換えられません")
			}
			if err := os.WriteFile(env.WorkflowPath, []byte(content), 0o600); err != nil {
				t.Fatalf("WORKFLOW.md を書けません: %v", err)
			}
			t.Setenv("CONTINUO_TEST_NO_SUCH_USAGE_TOKEN", "")
			slPath := filepath.Join(env.RuntimeDir, socketpath.StatuslineSocketFileName)
			// **中身のあるディレクトリは os.Remove で消せない**ので、残骸を消す段で Start が失敗する。
			if err := os.MkdirAll(filepath.Join(slPath, "keep"), 0o700); err != nil {
				t.Fatalf("sl.sock の場所にディレクトリを置けません: %v", err)
			}
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

			if tc.wantStop {
				code, finished := waitProcess(context.Background(), cmd, 30*time.Second)
				if !finished {
					t.Fatalf("sl.sock を開けないのに 30 秒以内に終了しなかった\n%s", logs.String())
				}
				if code == 0 {
					t.Fatalf("sl.sock を開けないのに終了コードが 0 だった\n%s", logs.String())
				}
				out := logs.String()
				if !strings.Contains(out, slPath) {
					t.Fatalf("出力に sl.sock のパスが出ていない\n%s", out)
				}
				if strings.Contains(out, "巡回を始めます") {
					t.Fatalf("sl.sock を開けないのに巡回を始めた\n%s", out)
				}
				return
			}

			waitFor(t, 30*time.Second, "巡回が始まる", func() bool {
				return strings.Contains(logs.String(), "巡回を始めます")
			})
			if !strings.Contains(logs.String(), "sl.sock）を開けないので") {
				t.Fatalf("sl.sock を開けないことの WARN が出ていない\n%s", logs.String())
			}
			// **DisableStatusline が効いていること**を、切り替えの WARN の文面で確かめる
			// （statusline を使えないときだけ「statusline も使えないので」になる）。
			waitFor(t, 30*time.Second, "statusline も使えないことの WARN", func() bool {
				return strings.Contains(logs.String(), "statusline も使えないので")
			})
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatalf("SIGTERM を送れません: %v", err)
			}
			code, finished := waitProcess(context.Background(), cmd, 30*time.Second)
			if !finished {
				t.Fatalf("SIGTERM を受けても 30 秒以内に終了しなかった\n%s", logs.String())
			}
			if code != 0 {
				t.Fatalf("終了コードが 0 ではない: got %d\n%s", code, logs.String())
			}
			if _, err := os.Stat(filepath.Join(slPath, "keep")); err != nil {
				t.Fatalf("開けなかった sl.sock の場所のものを消した（別のプロセスのものを消しうる）: %v", err)
			}
		})
	}
}

// 目的: `rate_limit.source: none` でも、起動時に閉じ残しの statusline取得の workspace を片付けること、
// その片付けが復元（`pane.list`）より前に行われることを、ビルドしたバイナリで確かめる
// （閉じ残しは、復元の片付けの `worktree.open` で issue の親にされうる。none へ切り替えた人の
// herdr にも前の版の閉じ残しは残る）。
// 与える情報: 実行時ディレクトリの statusline-fetch/workspaces.json に ["w77","w78"]。herdr の一覧は、
// w77 が statusline取得の label、w78 が別の label（ID が使い回された）。カンバンは空。
// 成功条件: workspace.close が w77 にだけ送られ、それが復元の `pane.list` より前であること。
// 一覧のファイルが空の一覧に書き直されること。SIGTERM で終了コード 0 で終わること。
func TestDaemon_sourceがnoneでも閉じ残しのstatusline取得のworkspaceを復元より前に片付ける(t *testing.T) {
	env := newDaemonEnv(t)
	env.GitHub = newFakeGitHub(t, "octocat", env.Timeline)
	env.Herdr.Handle("pane.list", func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "pane_list", "panes": []any{}}, nil
	})
	env.Herdr.Handle("agent.list", func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "agent_list", "agents": []any{}}, nil
	})
	env.Herdr.Handle(herdr.MethodWorkspaceList, func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "workspace_list", "workspaces": []any{
			map[string]any{"workspace_id": "w77", "label": herdr.StatuslineFetchLabel},
			map[string]any{"workspace_id": "w78", "label": "octocat/hello-world/issues/5"},
		}}, nil
	})
	env.Herdr.Handle(herdr.MethodWorkspaceClose, func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "ok"}, nil
	})

	listDir := filepath.Join(env.RuntimeDir, "statusline-fetch")
	if err := os.MkdirAll(listDir, 0o700); err != nil {
		t.Fatalf("閉じ残しの一覧の置き場所を作れません: %v", err)
	}
	listPath := filepath.Join(listDir, "workspaces.json")
	if err := os.WriteFile(listPath, []byte(`["w77","w78"]`+"\n"), 0o600); err != nil {
		t.Fatalf("閉じ残しの一覧を書けません: %v", err)
	}

	cmd, logs := env.start(t)
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("continuo の出力:\n%s", logs.String())
		}
	})
	waitFor(t, 30*time.Second, "巡回が始まる", func() bool {
		return strings.Contains(logs.String(), "巡回を始めます")
	})

	var closed []string
	for _, r := range env.Herdr.Requests() {
		if r.Method == herdr.MethodWorkspaceClose {
			closed = append(closed, fmt.Sprint(r.Params["workspace_id"]))
		}
	}
	if len(closed) != 1 || closed[0] != "w77" {
		t.Fatalf("閉じた workspace = %v, want [w77]", closed)
	}
	closeAt := env.Timeline.IndexOfPrefix("herdr." + herdr.MethodWorkspaceClose)
	restoreAt := env.Timeline.IndexOfPrefix("herdr.pane.list")
	if closeAt < 0 || restoreAt < 0 || closeAt > restoreAt {
		t.Fatalf("閉じ残しの片付け（%d）が復元の pane.list（%d）より前ではない: %v",
			closeAt, restoreAt, env.Timeline.Entries())
	}
	data, err := os.ReadFile(listPath)
	if err != nil {
		t.Fatalf("閉じ残しの一覧を読めません: %v", err)
	}
	var remaining []string
	if err := json.Unmarshal(data, &remaining); err != nil {
		t.Fatalf("閉じ残しの一覧を JSON として読めません: %v（%q）", err, data)
	}
	if len(remaining) != 0 {
		t.Fatalf("閉じ残しの一覧 = %v, want 空", remaining)
	}
	if _, err := os.Stat(filepath.Join(env.RuntimeDir, socketpath.StatuslineSocketFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source: none なのに sl.sock がある: %v", err)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM を送れません: %v", err)
	}
	code, finished := waitProcess(context.Background(), cmd, 30*time.Second)
	if !finished {
		t.Fatalf("SIGTERM を受けても 30 秒以内に終了しなかった\n%s", logs.String())
	}
	if code != 0 {
		t.Fatalf("終了コードが 0 ではない: got %d\n%s", code, logs.String())
	}
}

// 目的: `rate_limit.source: statusline` のとき、quota.json を読み、そのあとで sl.sock の listen を
// 始めること、それが起動時検査のあとで復元の前であることを、ビルドしたバイナリで確かめる
// （quota.json を先に読むのは、立て直した直後に届いた行を古い写しで上書きしないため。復元の前に
// 開くのは、復元した run の回復待ちの判定に効かせるため）。止めるときに sl.sock を消すことも確かめる。
// 与える情報: WORKFLOW.md の rate_limit.source を statusline にし、実行時ディレクトリに壊れた quota.json を置く。
// herdr の ping（起動時検査）と pane.list（復元）を受けたときに、sl.sock があるかを控える。カンバンは空。
// 成功条件: ping のときには sl.sock が無く、pane.list のときにはあること。ログの順が
// 「quota.json を捨てた WARN → 使用率の socket の listen を始めた → 復元を終えた」であること。
// SIGTERM で終了コード 0 で終わり、sl.sock が消えていること。
func TestDaemon_statuslineならquota_jsonを読んでから起動時検査のあと復元の前にsl_sockを開く(t *testing.T) {
	env := newDaemonEnv(t)
	env.GitHub = newFakeGitHub(t, "octocat", env.Timeline)
	raw, err := os.ReadFile(env.WorkflowPath)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	content := strings.Replace(string(raw), "rate_limit:\n  source: none\n", "rate_limit:\n  source: statusline\n", 1)
	if content == string(raw) {
		t.Fatal("WORKFLOW.md の rate_limit.source を書き換えられません")
	}
	if err := os.WriteFile(env.WorkflowPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
	if err := os.WriteFile(filepath.Join(env.RuntimeDir, "quota.json"), []byte("{壊れた"), 0o600); err != nil {
		t.Fatalf("quota.json を書けません: %v", err)
	}

	slPath := filepath.Join(env.RuntimeDir, socketpath.StatuslineSocketFileName)
	exists := func() bool {
		_, err := os.Stat(slPath)
		return err == nil
	}
	pingSaw := &stringBox{}
	paneListSaw := &stringBox{}
	env.Herdr.Handle("ping", func(map[string]any) (any, *rpcErr) {
		if pingSaw.Get() == "" {
			pingSaw.Set(fmt.Sprint(exists()))
		}
		return map[string]any{
			"type": "pong", "version": "0.8.0-fake", "protocol": env.Herdr.Protocol,
			"capabilities": map[string]any{"live_handoff": true},
		}, nil
	})
	env.Herdr.Handle("pane.list", func(map[string]any) (any, *rpcErr) {
		if paneListSaw.Get() == "" {
			paneListSaw.Set(fmt.Sprint(exists()))
		}
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

	if got := pingSaw.Get(); got != "false" {
		t.Fatalf("起動時検査の ping のとき sl.sock があるか = %q, want false（起動時検査より前に開いている）", got)
	}
	if got := paneListSaw.Get(); got != "true" {
		t.Fatalf("復元の pane.list のとき sl.sock があるか = %q, want true（復元より前に開いていない）", got)
	}
	out := logs.String()
	quotaAt := strings.Index(out, "quota.json")
	listenAt := strings.Index(out, "使用率を受ける socket の listen を始めました")
	restoredAt := strings.Index(out, "復元を終えました")
	if quotaAt < 0 || listenAt < 0 || restoredAt < 0 || quotaAt > listenAt || listenAt > restoredAt {
		t.Fatalf("ログの順が「quota.json → listen → 復元」ではない（%d, %d, %d）\n%s",
			quotaAt, listenAt, restoredAt, out)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM を送れません: %v", err)
	}
	code, finished := waitProcess(context.Background(), cmd, 30*time.Second)
	if !finished {
		t.Fatalf("SIGTERM を受けても 30 秒以内に終了しなかった\n%s", logs.String())
	}
	if code != 0 {
		t.Fatalf("終了コードが 0 ではない: got %d\n%s", code, logs.String())
	}
	if exists() {
		t.Fatal("止めたあとも sl.sock が残っている（次の起動が残骸の確認で遅れる）")
	}
}
