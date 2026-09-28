package herdr_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// workspaceRenameSchemaKeys は workspace.rename の実スキーマ（WorkspaceRenameParams）に
// 定義されている引数の全集合である。**2つとも必須である。**
var workspaceRenameSchemaKeys = []string{"workspace_id", "label"}

// workspaceCloseSchemaKeys は workspace.close の実スキーマ（WorkspaceTarget）に
// 定義されている引数の全集合である。**workspace_id ひとつだけで、必須である。**
var workspaceCloseSchemaKeys = []string{"workspace_id"}

// 目的: workspace.list が引数を1つも送らず、一覧を読み取れることを確認する。
// 与える情報: workspace を2件返す偽サーバ。
// 成功条件: 送られた method が "workspace.list" で params が空であり、
// 応答の workspaces を2件とも読み取れること。
func TestWorkspaceList_引数なしで一覧を取る(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.WorkspaceListResult{
			Type: "workspace_list",
			Workspaces: []herdr.Workspace{
				{WorkspaceID: "w1", Label: "octocat/hello-world/issues/188"},
				{WorkspaceID: "w2"},
			},
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})
	result, err := client.WorkspaceList(context.Background())
	if err != nil {
		t.Fatalf("WorkspaceList が失敗した: %v", err)
	}
	if result.Type != "workspace_list" {
		t.Fatalf("応答の type が想定と違う: got %q, want %q", result.Type, "workspace_list")
	}
	if len(result.Workspaces) != 2 {
		t.Fatalf("応答の workspaces を読み取れていない: got %d 件, want 2 件", len(result.Workspaces))
	}
	if result.Workspaces[0].WorkspaceID != "w1" {
		t.Fatalf("1件目の workspace_id が想定と違う: got %q, want %q",
			result.Workspaces[0].WorkspaceID, "w1")
	}

	if params := sentParams(t, fs, herdr.MethodWorkspaceList); len(params) != 0 {
		t.Fatalf("workspace.list に引数を送っている: %v", params)
	}
}

// 目的: workspace.close が workspace_id だけを送ることを確認する。
// **worktree.remove では閉じない workspace を閉じる唯一の経路である**
// （worktree.open に cwd を渡すと、cwd のリポジトリの workspace も開くため）。
// 与える情報: 閉じる workspace の ID。
// 成功条件: 送られた method が "workspace.close" で、引数が workspace_id ひとつだけであり、
// 応答の type を読み取れること。
func TestWorkspaceClose_workspace_idだけを送る(t *testing.T) {
	const workspaceID = "w9"

	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.WorkspaceCloseResult{Type: "ok"})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})
	result, err := client.WorkspaceClose(context.Background(), herdr.WorkspaceCloseParams{
		WorkspaceID: workspaceID,
	})
	if err != nil {
		t.Fatalf("WorkspaceClose が失敗した: %v", err)
	}
	if result.Type != "ok" {
		t.Fatalf("応答の type が想定と違う: got %q, want %q", result.Type, "ok")
	}

	params := sentParams(t, fs, herdr.MethodWorkspaceClose)
	if len(params) != 1 {
		t.Fatalf("workspace.close の引数が workspace_id ひとつになっていない: %v", params)
	}
	if got := params["workspace_id"]; got != workspaceID {
		t.Fatalf("workspace_id が想定と違う: got %v, want %q", got, workspaceID)
	}
	assertSchemaKeys(t, params, herdr.MethodWorkspaceClose, workspaceCloseSchemaKeys)
}

// 目的: workspace.rename が必須の2つの引数（workspace_id / label）を送ることを確認する
// （設計 2-1 の表に載っているメソッド。3-3 は herdr workspace の label に issue の URL を
// 書くと定めており、開いたあとに書き換える経路がこれである）。
// 与える情報: workspace の ID と、issue の URL を label に設定した WorkspaceRenameParams。
// 成功条件: 送られた method が "workspace.rename" で、workspace_id と label の2つが
// そのまま届くこと。実スキーマに無いキー（name / title など）を送っていないこと。
func TestWorkspaceRename_必須の2つを送る(t *testing.T) {
	const workspaceID = "w9"
	const issueURL = "https://github.com/octocat/hello-world/issues/188"

	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.WorkspaceRenameResult{
			Type:      "workspace_info",
			Workspace: herdr.Workspace{WorkspaceID: workspaceID, Label: issueURL},
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})
	result, err := client.WorkspaceRename(context.Background(), herdr.WorkspaceRenameParams{
		WorkspaceID: workspaceID,
		Label:       issueURL,
	})
	if err != nil {
		t.Fatalf("WorkspaceRename が失敗した: %v", err)
	}
	if result.Workspace.Label != issueURL {
		t.Fatalf("応答の label を読み取れていない: got %q, want %q", result.Workspace.Label, issueURL)
	}

	params := sentParams(t, fs, herdr.MethodWorkspaceRename)
	if len(params) != 2 {
		t.Fatalf("workspace.rename の引数が workspace_id と label の2つになっていない: %v", params)
	}
	if got := params["workspace_id"]; got != workspaceID {
		t.Fatalf("workspace_id が想定と違う: got %v, want %q", got, workspaceID)
	}
	if got := params["label"]; got != issueURL {
		t.Fatalf("label が想定と違う: got %v, want %q", got, issueURL)
	}
	assertSchemaKeys(t, params, herdr.MethodWorkspaceRename, workspaceRenameSchemaKeys)
}

// 目的: label が空でも label というキーを送ることを確認する（実スキーマ上 label は
// **必須**なので、omitempty で落としてはならない）。
// 与える情報: WorkspaceID だけを設定した WorkspaceRenameParams。
// 成功条件: params に label キーが存在し、値が空文字であること。
func TestWorkspaceRename_labelが空でも送る(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.WorkspaceRenameResult{Type: "workspace_info"})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})
	if _, err := client.WorkspaceRename(context.Background(), herdr.WorkspaceRenameParams{
		WorkspaceID: "w9",
	}); err != nil {
		t.Fatalf("WorkspaceRename が失敗した: %v", err)
	}

	params := sentParams(t, fs, herdr.MethodWorkspaceRename)
	got, ok := params["label"]
	if !ok {
		t.Fatalf("label が必須なのに送られていない: %v", params)
	}
	if got != "" {
		t.Fatalf("label が空文字として送られていない: got %v", got)
	}
}

// workspaceCreateSchemaKeys は workspace.create で continuo が送る引数の全集合である
// （issue #284。実測: 2026-09-25、herdr 0.9.1）。
var workspaceCreateSchemaKeys = []string{"cwd", "label", "focus"}

// 目的: workspace.create が cwd・label・focus を送り、応答から workspace の ID と
// root の pane の ID を読むことを確認する（issue #284。statusline取得の workspace を作る経路）。
// **focus は偽を送る。**人間が見ている画面を、statusline取得のたびに奪わないためである。
// 与える情報: clone のパス・statusline取得の label・focus 偽の WorkspaceCreateParams と、
// 本物の herdr と同じ形（workspace・tab・root_pane）の応答を返す偽サーバ。
// 成功条件: 送られた method が "workspace.create" で、引数が cwd・label・focus の3つだけであり、
// それぞれの値がそのまま届くこと（focus は JSON の false）。応答の workspace.workspace_id と
// root_pane.pane_id を読み取れること。
func TestWorkspaceCreate_cwdとlabelとfocusを送りroot_paneのIDを読む(t *testing.T) {
	const cwd = "/tmp/continuo-test/octocat/hello-world"

	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		// **構造体で返さず、本物の herdr の wire の形をそのまま書く。**
		// 構造体の JSON タグを取り違えても、同じ構造体で書いて読むと気づけない。
		writeResult(t, conn, req.ID, map[string]any{
			"type": "workspace_created",
			"workspace": map[string]any{
				"workspace_id": "w20",
				"label":        herdr.StatuslineFetchLabel,
			},
			"tab":       map[string]any{"tab_id": "w20:t1", "workspace_id": "w20"},
			"root_pane": map[string]any{"pane_id": "w20:p1", "workspace_id": "w20"},
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})
	focus := false
	result, err := client.WorkspaceCreate(context.Background(), herdr.WorkspaceCreateParams{
		Cwd:   cwd,
		Label: herdr.StatuslineFetchLabel,
		Focus: &focus,
	})
	if err != nil {
		t.Fatalf("WorkspaceCreate が失敗した: %v", err)
	}
	if result.Workspace.WorkspaceID != "w20" {
		t.Fatalf("応答の workspace_id を読み取れていない: got %q, want %q", result.Workspace.WorkspaceID, "w20")
	}
	if result.RootPane.PaneID != "w20:p1" {
		t.Fatalf("応答の root_pane.pane_id を読み取れていない: got %q, want %q", result.RootPane.PaneID, "w20:p1")
	}

	params := sentParams(t, fs, herdr.MethodWorkspaceCreate)
	if got := params["cwd"]; got != cwd {
		t.Fatalf("cwd が想定と違う: got %v, want %q", got, cwd)
	}
	if got := params["label"]; got != herdr.StatuslineFetchLabel {
		t.Fatalf("label が想定と違う: got %v, want %q", got, herdr.StatuslineFetchLabel)
	}
	got, ok := params["focus"]
	if !ok {
		t.Fatalf("focus を送っていない（省くと herdr が画面を切り替える）: %v", params)
	}
	if got != false {
		t.Fatalf("focus が偽として送られていない: got %v", got)
	}
	if len(params) != len(workspaceCreateSchemaKeys) {
		t.Fatalf("workspace.create の引数が cwd・label・focus の3つになっていない: %v", params)
	}
	assertSchemaKeys(t, params, herdr.MethodWorkspaceCreate, workspaceCreateSchemaKeys)
}

// 目的: statusline取得の workspace に貼る label が、閉じ残しの片付けで照合する値と同じ
// 固定の文字列であることを確認する（issue #284。label が変わると、前の版が残した
// 閉じ残しを「別の workspace」と見なして閉じずに一覧から外してしまう）。
// 与える情報: なし。
// 成功条件: herdr.StatuslineFetchLabel が "continuo statusline fetch" であること。
func TestStatuslineFetchLabel_固定の文字列である(t *testing.T) {
	if herdr.StatuslineFetchLabel != "continuo statusline fetch" {
		t.Fatalf("StatuslineFetchLabel = %q, want %q", herdr.StatuslineFetchLabel, "continuo statusline fetch")
	}
}
