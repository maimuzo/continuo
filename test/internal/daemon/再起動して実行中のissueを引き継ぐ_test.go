// {"RUCM-CFG-SHA256": "ff7f94447b17aef4d97e247fc142036a338862bbf6bbbda1e935ba8d12f2e55b", "SOURCE": "docs/spec/usecases/particular_case/再起動して実行中のissueを引き継ぐ.cfg.json"}
//
// **ユースケース記述「再起動して実行中のissueを引き継ぐ」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package daemon_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// {"RUCM-PATH": "P003"}
//
// Test_再起動して実行中のissueを引き継ぐ_P003_復元を終えてから巡回が始まり1件のissueが通る は、
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
//   - テスト用herdr mock（実 herdr には繋がない）とテスト用GitHub mock（本番のカンバンへは接続しない）
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
func Test_再起動して実行中のissueを引き継ぐ_P003_復元を終えてから巡回が始まり1件のissueが通る(t *testing.T) {
	env := newDaemonEnv(t)
	env.GitHub = newFakeGitHub(t, "octocat", env.Timeline,
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
				"name": "continuo-hello-world-188", "agent": "claude", "agent_status": "idle",
				"pane_id": "p-188", "tab_id": "t1", "workspace_id": "w188",
				"terminal_id": "term1", "focused": false, "revision": 1,
			},
			map[string]any{
				"name": "continuo-hello-world-189", "agent": "claude", "agent_status": "working",
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
		if target != "continuo-hello-world-188" {
			t.Errorf("引き継いだあと turn を送ってはならない相手へ送った: %q", target)
		}
		text, _ := params["text"].(string)
		promptedText.Set(text)

		// エージェントが作業を終えた状態を作る。
		writeTranscript(t, transcriptPath)
		env.GitHub.AddComment("I_node188", "<!-- continuo:agent -->\n実装して push しました")
		// **完了の真実の源はカンバンである。**エージェントが gh で Done へ動かした状況にする。
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
	candidatesAt := env.Timeline.IndexOfPrefix(`gql.items("Status":"Ready","In Progress")`)
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
		return !strings.Contains(branches, "continuo/octocat/hello-world/188")
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

// {"RUCM-PATH": "P047"}
//
// Test_再起動して実行中のissueを引き継ぐ_P047_flockが取れなければ即座に終了する は、二重起動の防止を確かめる。
//
// 目的: 設計 3-17。**continuo の状態はメモリにしかないので、2つ目のプロセスが立つと
// 1つ目が処理中の issue を平気で掴む。**
//
// 与える情報: 1つ目が走っている状態で、同じ設定で2つ目を起動する。
//
// 成功条件: 2つ目が 20 秒以内に終了コード 1 で終わり、二重起動を検出したと出る。
// **2つ目は pane を1つも閉じない。**
func Test_再起動して実行中のissueを引き継ぐ_P047_flockが取れなければ即座に終了する(t *testing.T) {
	env := newDaemonEnv(t)
	env.GitHub = newFakeGitHub(t, "octocat", env.Timeline,
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

// {"RUCM-PATH": "P045"}
//
// Test_再起動して実行中のissueを引き継ぐ_P045_起動時の検査に落ちたら生きているpaneを閉じずに起動を止める は、
// 設計 3-4 の「起動から復元までの順序」の段3 を確かめる。
//
// 目的: **設定の誤りで、動いているエージェントの作業を殺さない。**落ちる原因は
// continuo 側の前提が揃っていないことであって、エージェントの側の問題ではない。
// 人間が直して起動し直せば、復元の段5 で引き継げる。
//
// 与える情報: `herdr.protocol` が設定と一致しないテスト用herdr mock と、生きている pane。
//
// 成功条件: 終了コード 1 で起動を止め、**`pane.close` を1回も呼ばない。**
// **復元（`pane.list`）にも進まない。**
func Test_再起動して実行中のissueを引き継ぐ_P045_起動時の検査に落ちたら生きているpaneを閉じずに起動を止める(t *testing.T) {
	env := newDaemonEnv(t)
	env.GitHub = newFakeGitHub(t, "octocat", env.Timeline,
		&boardItem{ItemID: "PVTI_item188", NodeID: "I_node188", Number: 188, State: "In Progress"},
	)
	path188 := env.prepareRun(t, 188, "w188", "sess-188")
	// **protocol が合わない。**起動時の検査で止まるべきである。
	env.Herdr.Handle("ping", func(map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type": "pong", "version": "mismatch-fake", "protocol": 21,
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

// {"RUCM-PATH": "P001"}
//
// Test_再起動して実行中のissueを引き継ぐ_P001_CLIのportでダッシュボードを開いて実行中のrunを出す は、
// ダッシュボードが本物の orchestrator に繋がっていることを確かめる。
//
// 目的: `SPEC.md` 13.7 の「CLI `--port` overrides `server.port`」（訳: 両方あるときは
// CLI の `--port` が `server.port` を上書きする）と、13.7.2 の `GET /api/v1/state` を
// 満たしていることを示す。**引き継いだ run が JSON に出ることまで確かめる**
// （偽の供給元では、この結線が切れていても気づけない）。
//
// 与える情報: `server.port` を書いていない WORKFLOW.md と、`--port=0`（OS に空きポートを
// 選ばせる）。引き継ぐ対象の worktree が1件あり、その pane は `working` である
// （turn を送らないので、run は印に残ったままになる）。
//
// 成功条件: 待ち受け先がログに出ること。`GET /api/v1/state` が 200 を返し、
// 引き継いだ issue の識別子が入っていること。**ループバック以外の宛先は 421 で断ること。**
func Test_再起動して実行中のissueを引き継ぐ_P001_CLIのportでダッシュボードを開いて実行中のrunを出す(t *testing.T) {
	env := newDaemonEnv(t)
	env.GitHub = newFakeGitHub(t, "octocat", env.Timeline,
		&boardItem{ItemID: "PVTI_item189", NodeID: "I_node189", Number: 189, State: "In Progress"},
	)
	path189 := env.prepareRun(t, 189, "w189", "sess-189")
	env.Herdr.Handle("pane.list", func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "pane_list", "panes": []any{
			map[string]any{
				"pane_id": "p-189", "workspace_id": "w189", "cwd": path189,
				"agent_status": "working", "agent": "claude",
				"agent_session": map[string]any{
					"source": "herdr:claude", "agent": "claude", "kind": "id", "value": "sess-189",
				},
			},
		}}, nil
	})
	env.Herdr.Handle("agent.list", func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "agent_list", "agents": []any{
			map[string]any{
				"name": "continuo-hello-world-189", "agent": "claude", "agent_status": "working",
				"pane_id": "p-189", "tab_id": "t1", "workspace_id": "w189",
				"terminal_id": "term1", "focused": false, "revision": 1,
			},
		}}, nil
	})

	cmd, logs := env.startWithArgs(t, "--port=0")
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("continuo の出力:\n%s", logs.String())
		}
	})

	var addr string
	waitFor(t, 30*time.Second, "ダッシュボードが開く", func() bool {
		var ok bool
		addr, ok = dashboardAddr(logs)
		return ok
	})
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("ループバック以外で待ち受けている: %q", addr)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	var body string
	waitFor(t, 30*time.Second, "引き継いだ run が JSON に出る", func() bool {
		res, err := client.Get("http://" + addr + "/api/v1/state")
		if err != nil {
			return false
		}
		defer func() { _ = res.Body.Close() }()
		b, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != http.StatusOK {
			return false
		}
		body = string(b)
		return strings.Contains(body, "octocat/hello-world#189")
	})

	// **ループバック以外の宛先は断る**（DNS rebinding で中身を読み出させない）。
	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/api/v1/state", nil)
	if err != nil {
		t.Fatalf("リクエストを組み立てられません: %v", err)
	}
	req.Host = "attacker.example.com"
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("取得できません: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("ループバック以外の宛先を受け入れた: got %d, want %d",
			res.StatusCode, http.StatusMisdirectedRequest)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM を送れません: %v", err)
	}
	if _, ok := waitProcess(context.Background(), cmd, 20*time.Second); !ok {
		t.Fatalf("SIGTERM を受けても 20 秒以内に終了しなかった\n%s", logs.String())
	}
	if _, err := client.Get("http://" + addr + "/api/v1/state"); err == nil {
		t.Fatal("終了したのにダッシュボードへ接続できた")
	}
}
