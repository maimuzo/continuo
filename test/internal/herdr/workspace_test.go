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
