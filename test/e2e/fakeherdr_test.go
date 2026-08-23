package e2e_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// ===== テスト用herdr mock の socket サーバ =====

// rpcErr は herdr の socket API のエラー応答である。
type rpcErr struct {
	// Code はエラーの種別である。
	Code string `json:"code"`
	// Message は説明である。
	Message string `json:"message"`
}

// recordedRequest はテスト用herdr mock が受け取ったリクエスト1件である。
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

// agentSession はテスト用herdr mock が覚えている「起動済みの Claude Code」1件である。
//
// **実プロセスは無い。**agent.start で渡された引数から、テスト用Claude Code mock が
// エージェントの役を演じるのに要る値（設定ファイルとセッション UUID）を控えておく。
type agentSession struct {
	// Name は agent 名である（`agent.prompt` の target になる）。
	Name string
	// PaneID は agent が居る pane の ID である。
	PaneID string
	// WorktreePath はその pane の作業ディレクトリ（worktree）の絶対パスである。
	WorktreePath string
	// SettingsPath は `--settings` で渡された設定ファイルの絶対パスである。
	// **Stop hook のコマンド行はこのファイルから読む。**
	SettingsPath string
	// SessionUUID は `--session-id` で渡されたセッション UUID である。
	SessionUUID string
	// ResumeUUID は `--resume` で渡されたセッション UUID である（復帰のときだけ）。
	ResumeUUID string
}

// fakeHerdr は herdr の socket API の代わりに使う偽のサーバである。
//
// **実 herdr には繋がない。**応答するのは
// ping / worktree.open / worktree.remove / pane.split / pane.list / pane.rename /
// pane.close / agent.list / agent.start / agent.get / agent.wait / agent.prompt /
// agent.send_keys である。
//
// **worktree は本物である。**`worktree.remove` は実体を消して `git worktree prune` を叩く
// （本物の herdr と同じ結果にしないと、`git branch -D` が
// 「checkout 中の branch は消せない」で必ず失敗し、片付けを検証できない）。
type fakeHerdr struct {
	// SocketPath は listen している socket の絶対パスである。
	SocketPath string

	mu       sync.Mutex
	requests []recordedRequest
	handlers map[string]herdrHandler
	// workspaces は workspace の ID から worktree のパスを引く写像である。
	workspaces map[string]string
	// panes は pane の ID から workspace の ID を引く写像である。
	panes map[string]string
	// agents は agent 名から起動済みの agent を引く写像である。
	agents map[string]*agentSession
	// nextWS は次に払い出す workspace の通し番号である。
	nextWS int
	// nextPane は次に払い出す pane の通し番号である。
	nextPane int
	// repoDir は worktree.remove のあとに `git worktree prune` を叩くリポジトリである。
	repoDir string
	// onPrompt は `agent.prompt` を受けたときに呼ぶ「テスト用Claude Code mock」である。
	onPrompt func(sess *agentSession, text string)
	// prompts は `agent.prompt` で送られた本文を送られた順に並べたものである。
	prompts []string
}

// newFakeHerdr はテスト用herdr mock を1本立て、着手が最後まで通るだけの既定の台本を入れる。
//
// t: 呼び出し元のテスト。socket の後始末を t.Cleanup に登録する。
// dir: socket を置くディレクトリ（**短く保つこと。**macOS の上限は103バイト）。
// 戻り値: 起動したテスト用herdr mock。
func newFakeHerdr(t *testing.T, dir string) *fakeHerdr {
	t.Helper()

	socketPath := filepath.Join(dir, "h.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("テスト用herdr mock の socket を bind できません（%s）: %v", socketPath, err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	fh := &fakeHerdr{
		SocketPath: socketPath,
		handlers:   map[string]herdrHandler{},
		workspaces: map[string]string{},
		panes:      map[string]string{},
		agents:     map[string]*agentSession{},
	}
	fh.installDefaults(t)

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

// SetRepoDir は worktree.remove のあとに prune を叩くリポジトリを設定する。
//
// dir: clone の絶対パス。
func (fh *fakeHerdr) SetRepoDir(dir string) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.repoDir = dir
}

// SetOnPrompt は `agent.prompt` を受けたときに演じる「テスト用Claude Code mock」を差し替える。
//
// fn: エージェントの役。**接続ごとの goroutine から呼ばれるので t.Fatalf を使ってはならない。**
func (fh *fakeHerdr) SetOnPrompt(fn func(sess *agentSession, text string)) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.onPrompt = fn
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
//
// method: 数えるメソッド名。
// 戻り値: 回数。
func (fh *fakeHerdr) CountMethod(method string) int {
	n := 0
	for _, m := range fh.Methods() {
		if m == method {
			n++
		}
	}
	return n
}

// Prompts は `agent.prompt` で送られた本文を送られた順に返す。
func (fh *fakeHerdr) Prompts() []string {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	out := make([]string, len(fh.prompts))
	copy(out, fh.prompts)
	return out
}

// LivePanes はいま開いている pane の ID を返す。
//
// **`pane.close` と `worktree.remove` で減る。**片付けの検証に使う。
func (fh *fakeHerdr) LivePanes() []string {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	out := make([]string, 0, len(fh.panes))
	for id := range fh.panes {
		out = append(out, id)
	}
	return out
}

// workspaceFor はパスに対応する workspace の ID を返す（無ければ払い出す）。
//
// **呼び出し側が fh.mu を保持していないこと。**
//
// path: worktree の絶対パス。
// 戻り値の1つ目: workspace の ID。
// 戻り値の2つ目: その workspace の root pane の ID。
func (fh *fakeHerdr) workspaceFor(path string) (string, string) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	for id, p := range fh.workspaces {
		if p != path {
			continue
		}
		for paneID, wsID := range fh.panes {
			if wsID == id {
				return id, paneID
			}
		}
		// workspace は残っているが pane が閉じられている。開き直す。
		fh.nextPane++
		paneID := fmt.Sprintf("p%d", fh.nextPane)
		fh.panes[paneID] = id
		return id, paneID
	}
	fh.nextWS++
	id := fmt.Sprintf("w%d", fh.nextWS)
	fh.workspaces[id] = path
	fh.nextPane++
	paneID := fmt.Sprintf("p%d", fh.nextPane)
	fh.panes[paneID] = id
	return id, paneID
}

// installDefaults は着手から片付けまでが通るだけの既定の台本を入れる。
//
// t: 呼び出し元のテスト（**台本の中では t.Errorf だけを使う**）。
func (fh *fakeHerdr) installDefaults(t *testing.T) {
	fh.Handle("ping", func(map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type": "pong", "version": "0.8.0-fake", "protocol": 19,
			"capabilities": map[string]any{"live_handoff": true},
		}, nil
	})

	fh.Handle("worktree.open", func(params map[string]any) (any, *rpcErr) {
		path, _ := params["path"].(string)
		branch, _ := params["branch"].(string)
		// **本物と同じく、path と branch は片方だけ受け付ける。**
		// 両方渡しても通すmockにしていたせいで、本物で
		// `invalid_request: exactly one of path or branch is required` が出るまで
		// 誰も気づけなかった（実測: 2026-08-20）。
		if (path == "") == (branch == "") {
			return nil, &rpcErr{Code: "invalid_request", Message: "exactly one of path or branch is required"}
		}
		id, paneID := fh.workspaceFor(path)
		return map[string]any{
			"type":      "worktree_opened",
			"workspace": map[string]any{"workspace_id": id},
			"tab":       map[string]any{"tab_id": id + ":t1"},
			"root_pane": map[string]any{"pane_id": paneID, "workspace_id": id, "cwd": path},
			"worktree":  map[string]any{"path": path},
		}, nil
	})

	fh.Handle("worktree.remove", func(params map[string]any) (any, *rpcErr) {
		id, _ := params["workspace_id"].(string)
		fh.mu.Lock()
		path := fh.workspaces[id]
		repo := fh.repoDir
		delete(fh.workspaces, id)
		for paneID, wsID := range fh.panes {
			if wsID == id {
				delete(fh.panes, paneID)
			}
		}
		fh.mu.Unlock()
		if path != "" {
			// 本物の herdr と同じ結果にする（実体を消して git の登録を掃除する）。
			_ = os.RemoveAll(path)
			if repo != "" {
				_ = exec.Command("git", "-C", repo, "worktree", "prune").Run()
			}
		}
		return map[string]any{
			"type": "worktree_removed", "workspace_id": id, "path": path, "forced": true,
		}, nil
	})

	// **continuo は着手で pane.split を呼ばない**（worktree.open が作った pane を使う。
	// 設計 4-5）。それでも本物の herdr は答えるので、ここでも答える。
	fh.Handle("pane.split", func(params map[string]any) (any, *rpcErr) {
		wsID, _ := params["workspace_id"].(string)
		cwd, _ := params["cwd"].(string)
		fh.mu.Lock()
		if wsID == "" {
			if target, ok := params["target_pane_id"].(string); ok {
				wsID = fh.panes[target]
			}
		}
		fh.nextPane++
		paneID := fmt.Sprintf("p%d", fh.nextPane)
		fh.panes[paneID] = wsID
		fh.mu.Unlock()
		return map[string]any{
			"type": "pane_info",
			"pane": map[string]any{"pane_id": paneID, "workspace_id": wsID, "cwd": cwd},
		}, nil
	})

	fh.Handle("pane.list", func(params map[string]any) (any, *rpcErr) {
		want, _ := params["workspace_id"].(string)
		fh.mu.Lock()
		defer fh.mu.Unlock()
		panes := []any{}
		for paneID, wsID := range fh.panes {
			if want != "" && want != wsID {
				continue
			}
			pane := map[string]any{
				"pane_id": paneID, "workspace_id": wsID, "cwd": fh.workspaces[wsID],
				"agent_status": "unknown",
			}
			for _, sess := range fh.agents {
				if sess.PaneID != paneID {
					continue
				}
				pane["agent"] = "claude"
				pane["agent_status"] = "idle"
				pane["interactive_ready"] = true
				pane["agent_session"] = map[string]any{
					"source": "herdr:claude", "agent": "claude",
					"kind": "id", "value": sess.SessionUUID,
				}
			}
			panes = append(panes, pane)
		}
		return map[string]any{"type": "pane_list", "panes": panes}, nil
	})

	fh.Handle("pane.rename", func(params map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "pane_info", "pane": map[string]any{
			"pane_id": params["pane_id"], "label": params["label"],
		}}, nil
	})

	fh.Handle("pane.close", func(params map[string]any) (any, *rpcErr) {
		paneID, _ := params["pane_id"].(string)
		fh.mu.Lock()
		delete(fh.panes, paneID)
		for name, sess := range fh.agents {
			if sess.PaneID == paneID {
				delete(fh.agents, name)
			}
		}
		fh.mu.Unlock()
		return map[string]any{"type": "ok"}, nil
	})

	fh.Handle("agent.list", func(map[string]any) (any, *rpcErr) {
		fh.mu.Lock()
		defer fh.mu.Unlock()
		agents := []any{}
		for _, sess := range fh.agents {
			agents = append(agents, map[string]any{
				"name": sess.Name, "agent": "claude", "agent_status": "idle", "interactive_ready": true,
				"pane_id": sess.PaneID, "tab_id": "t1",
				"workspace_id": fh.panes[sess.PaneID], "terminal_id": "term1",
				"focused": false, "revision": 1,
			})
		}
		return map[string]any{"type": "agent_list", "agents": agents}, nil
	})

	fh.Handle("agent.start", func(params map[string]any) (any, *rpcErr) {
		name, _ := params["name"].(string)
		paneID, _ := params["pane_id"].(string)
		sess := &agentSession{Name: name, PaneID: paneID}
		raw, _ := params["args"].([]any)
		args := make([]string, 0, len(raw))
		for _, v := range raw {
			s, _ := v.(string)
			args = append(args, s)
		}
		// **本物の Claude Code と同じ引数から読む。**設定ファイルとセッション UUID は
		// ここでしか渡ってこない（設計 3-12 / 3-16 の段9）。
		sess.SettingsPath = argValue(args, "--settings")
		sess.SessionUUID = argValue(args, "--session-id")
		sess.ResumeUUID = argValue(args, "--resume")
		if sess.SessionUUID == "" {
			sess.SessionUUID = sess.ResumeUUID
		}
		fh.mu.Lock()
		sess.WorktreePath = fh.workspaces[fh.panes[paneID]]
		fh.agents[name] = sess
		fh.mu.Unlock()
		if sess.SettingsPath == "" {
			t.Errorf("agent.start に --settings が渡っていません: %v", args)
		}
		if sess.SessionUUID == "" {
			t.Errorf("agent.start に --session-id も --resume も渡っていません: %v", args)
		}
		return map[string]any{
			"type": "agent_started",
			"agent": map[string]any{
				"name": name, "agent_status": "idle", "interactive_ready": true, "pane_id": paneID,
			},
		}, nil
	})

	agentInfo := func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type":  "agent_info",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	}
	fh.Handle("agent.get", agentInfo)
	fh.Handle("agent.wait", agentInfo)
	fh.Handle("agent.send_keys", func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "keys_sent"}, nil
	})

	fh.Handle("agent.prompt", func(params map[string]any) (any, *rpcErr) {
		target, _ := params["target"].(string)
		text, _ := params["text"].(string)
		fh.mu.Lock()
		sess := fh.agents[target]
		fh.prompts = append(fh.prompts, text)
		onPrompt := fh.onPrompt
		fh.mu.Unlock()
		if sess == nil {
			return nil, &rpcErr{Code: "agent_not_found", Message: target}
		}
		// **ここで「テスト用Claude Code mock」がエージェントの役を演じる。**
		// 本物と同じく、応答を返す前に turn の中身（作業と Stop hook）が終わる。
		if onPrompt != nil {
			onPrompt(sess, text)
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": target, "agent_status": "idle", "interactive_ready": true},
		}, nil
	})
}

// serve は1本の接続を処理する。
//
// **接続ごとの goroutine から呼ばれるので t.Fatalf を使ってはならない。**
//
// t: 呼び出し元のテスト。
// conn: 受けた接続。
func (fh *fakeHerdr) serve(t *testing.T, conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(120 * time.Second))

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
	handler := fh.handlers[req.Method]
	fh.mu.Unlock()

	var resp map[string]any
	if handler == nil {
		resp = map[string]any{"id": req.ID,
			"error": map[string]any{"code": "unknown_method", "message": req.Method}}
	} else {
		result, rerr := handler(req.Params)
		if rerr != nil {
			resp = map[string]any{"id": req.ID,
				"error": map[string]any{"code": rerr.Code, "message": rerr.Message}}
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

// argValue は agent.start の args から `--name <値>` の値を取り出す。
//
// args: 受け取った引数の並び。
// name: 探すフラグ名。
// 戻り値: 値。無ければ空文字。
func argValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
