package herdr_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// worktreeCreateSchemaKeys は worktree.create の実スキーマ（WorktreeCreateParams）に
// 定義されている引数の全集合である。
var worktreeCreateSchemaKeys = []string{
	"path", "branch", "base", "cwd", "focus", "label", "workspace_id",
}

// worktreeOpenSchemaKeys は worktree.open の実スキーマ（WorktreeOpenParams）に
// 定義されている引数の全集合である。**base が無いのが worktree.create との違いである。**
var worktreeOpenSchemaKeys = []string{
	"path", "branch", "cwd", "focus", "label", "workspace_id",
}

// worktreeRemoveSchemaKeys は worktree.remove の実スキーマ（WorktreeRemoveParams）に
// 定義されている引数の全集合である。
var worktreeRemoveSchemaKeys = []string{"workspace_id", "force"}

// 目的: worktree.remove を呼ぶとき、渡す引数が path や branch ではなく herdr workspace
// の ID であることを確認する（設計 3-9: 「引数は path でも branch でもなく
// herdr workspace の ID である（実測）」）。
// 与える情報: WorktreeRemoveParams に workspace ID を1つだけ設定して呼ぶ。
// 成功条件: 偽サーバが受け取った params に "workspace_id" キーがあり、その値が渡した
// ID と一致すること。かつ実スキーマに無いキー（path / branch）を持たないこと。
// 応答の workspace_id / path / forced を読み取れること。
func TestWorktreeRemove_workspaceIDを渡す(t *testing.T) {
	const wantWorkspaceID = "w9"
	const wantPath = "/home/tester/worktrees/github.com/octocat/hello-world/continuo-octocat-hello-world-188"

	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.WorktreeRemoveResult{
			Type:        "worktree_removed",
			WorkspaceID: wantWorkspaceID,
			Path:        wantPath,
			Forced:      false,
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second, Startup: time.Second, Turn: time.Second})
	result, err := client.WorktreeRemove(context.Background(), herdr.WorktreeRemoveParams{
		WorkspaceID: wantWorkspaceID,
	})
	if err != nil {
		t.Fatalf("WorktreeRemove が失敗した: %v", err)
	}
	if result.WorkspaceID != wantWorkspaceID {
		t.Fatalf("応答の workspace_id が想定と違う: got %q, want %q", result.WorkspaceID, wantWorkspaceID)
	}
	if result.Path != wantPath {
		t.Fatalf("応答の path が想定と違う: got %q, want %q", result.Path, wantPath)
	}

	params := sentParams(t, fs, herdr.MethodWorktreeRemove)
	if got := params["workspace_id"]; got != wantWorkspaceID {
		t.Fatalf("workspace_id が渡した値と一致しない: got %v, want %q", got, wantWorkspaceID)
	}
	if _, hasPath := params["path"]; hasPath {
		t.Fatalf("worktree.remove の params に path が含まれている（herdr workspace の ID だけを渡すべき）: %v", params)
	}
	if _, hasBranch := params["branch"]; hasBranch {
		t.Fatalf("worktree.remove の params に branch が含まれている（herdr workspace の ID だけを渡すべき）: %v", params)
	}
	assertSchemaKeys(t, params, herdr.MethodWorktreeRemove, worktreeRemoveSchemaKeys)
}

// 目的: worktree.create に path を指定でき、片付けに要る herdr workspace の ID が
// 応答の workspace の中から取れることを確認する（設計 3-3 / 3-22: 「worktree.create の
// path 指定が効くことを実測で確認した」、3-9: 「片付けに要る herdr workspace の ID は
// ここで手に入る」）。
// 与える情報: WorktreeCreateParams に worktree の絶対パスを設定して呼ぶ。
// 成功条件: 偽サーバが受け取った params の "path" が渡したパスと一致し、応答の
// workspace.workspace_id と worktree.path を読み取れること。実スキーマに無いキーを
// 送っていないこと。
func TestWorktreeCreate_pathを指定できる(t *testing.T) {
	const wantPath = "/home/tester/worktrees/github.com/octocat/hello-world/continuo-octocat-hello-world-188"

	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.WorktreeCreateResult{
			Type:      "worktree_created",
			Workspace: herdr.Workspace{WorkspaceID: "w9", Label: "continuo-octocat-hello-world-188"},
			Tab:       herdr.Tab{TabID: "w9:t1"},
			RootPane:  herdr.Pane{PaneID: "w9:p1", Cwd: wantPath},
			Worktree:  herdr.Worktree{Path: wantPath, Branch: "continuo/octocat/hello-world/188"},
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second, Startup: time.Second, Turn: time.Second})
	result, err := client.WorktreeCreate(context.Background(), herdr.WorktreeCreateParams{Path: wantPath})
	if err != nil {
		t.Fatalf("WorktreeCreate が失敗した: %v", err)
	}
	if result.Workspace.WorkspaceID != "w9" {
		t.Fatalf("片付けに使う workspace の ID を読み取れていない: got %q", result.Workspace.WorkspaceID)
	}
	if result.Worktree.Path != wantPath {
		t.Fatalf("worktree の path が想定と違う: got %q, want %q", result.Worktree.Path, wantPath)
	}
	if result.RootPane.PaneID != "w9:p1" {
		t.Fatalf("root_pane の pane_id を読み取れていない: got %q", result.RootPane.PaneID)
	}

	params := sentParams(t, fs, herdr.MethodWorktreeCreate)
	if got := params["path"]; got != wantPath {
		t.Fatalf("path が渡した値と一致しない: got %v, want %q", got, wantPath)
	}
	assertSchemaKeys(t, params, herdr.MethodWorktreeCreate, worktreeCreateSchemaKeys)
}

// 目的: worktree.open に path を指定できることを確認する（設計 2-1: worktree.open は
// **既にある** worktree を herdr の workspace として開くメソッドであり、
// worktree.create とは別物である）。
// 与える情報: WorktreeOpenParams に worktree の絶対パスと label を設定して呼ぶ。
// 成功条件: 送られた method が "worktree.open" で、path と label がそのまま届くこと。
// worktree.open に無い引数（base）を送っていないこと。応答の workspace の ID と
// already_open を読み取れること。
func TestWorktreeOpen_pathを送れる(t *testing.T) {
	const wantPath = "/home/tester/worktrees/github.com/octocat/hello-world/continuo-octocat-hello-world-188"
	const wantLabel = "https://github.com/octocat/hello-world/issues/188"

	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.WorktreeOpenResult{
			Type:        "worktree_opened",
			Workspace:   herdr.Workspace{WorkspaceID: "w9", Label: wantLabel},
			Tab:         herdr.Tab{TabID: "w9:t1"},
			RootPane:    herdr.Pane{PaneID: "w9:p1", Cwd: wantPath},
			Worktree:    herdr.Worktree{Path: wantPath, OpenWorkspaceID: "w9"},
			AlreadyOpen: true,
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})
	result, err := client.WorktreeOpen(context.Background(), herdr.WorktreeOpenParams{
		Path:  wantPath,
		Label: wantLabel,
	})
	if err != nil {
		t.Fatalf("WorktreeOpen が失敗した: %v", err)
	}
	if result.Workspace.WorkspaceID != "w9" {
		t.Fatalf("workspace の ID が想定と違う: got %q", result.Workspace.WorkspaceID)
	}
	if !result.AlreadyOpen {
		t.Fatalf("already_open を読み取れていない（既に開かれていた場合の判定に使う）")
	}

	params := sentParams(t, fs, herdr.MethodWorktreeOpen)
	if got := params["path"]; got != wantPath {
		t.Fatalf("path が渡した値と一致しない: got %v, want %q", got, wantPath)
	}
	if got := params["label"]; got != wantLabel {
		t.Fatalf("label が渡した値と一致しない: got %v, want %q", got, wantLabel)
	}
	if _, ok := params["base"]; ok {
		t.Fatalf("worktree.open に無い引数 base を送っている（worktree を作らないメソッドである）: %v", params)
	}
	assertSchemaKeys(t, params, herdr.MethodWorktreeOpen, worktreeOpenSchemaKeys)
}

// 目的: worktree 系の呼び出しが Read（既定5秒）ではなく Startup（既定60秒）で待つことを
// 確認する（レビュー指摘「worktree.create / open / remove が Read を使っている」の回帰テスト）。
//
// **これらは待ちを伴う呼び出しである。**herdr 側で workspace・tab・pane を作り、
// `git worktree` を動かす。5秒で切れると、着手の段7（worktree.open）が原因の分かりにくい
// 形で失敗し、しかも herdr 側では workspace が作られている可能性がある（片付けの取りこぼし）。
//
// 与える情報: 応答を 300 ミリ秒遅らせる偽サーバと、Read=50ミリ秒・Startup=5秒のクライアント。
// 成功条件: WorktreeOpen / WorktreeCreate / WorktreeRemove のいずれも成功すること
// （Read を使っていれば 50 ミリ秒で打ち切られて失敗する）。
func TestWorktree_Readではなく起動用の長い待ち時間を使う(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		var sent struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(line, &sent); err != nil {
			t.Errorf("偽サーバが method を解析できませんでした: %v", err)
			return
		}
		// Read（50ミリ秒）より長く、Startup（5秒）より短い時間だけ待たせる。
		time.Sleep(300 * time.Millisecond)
		switch sent.Method {
		case herdr.MethodWorktreeCreate:
			writeResult(t, conn, req.ID, herdr.WorktreeCreateResult{Type: "worktree_created"})
		case herdr.MethodWorktreeOpen:
			writeResult(t, conn, req.ID, herdr.WorktreeOpenResult{Type: "worktree_opened"})
		default:
			writeResult(t, conn, req.ID, herdr.WorktreeRemoveResult{Type: "worktree_removed"})
		}
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: 50 * time.Millisecond, Startup: 5 * time.Second})

	if _, err := client.WorktreeOpen(context.Background(), herdr.WorktreeOpenParams{Path: "/tmp/wt"}); err != nil {
		t.Fatalf("WorktreeOpen が Read の上限で打ち切られた（Startup を使っていない）: %v", err)
	}
	if _, err := client.WorktreeCreate(context.Background(), herdr.WorktreeCreateParams{Path: "/tmp/wt"}); err != nil {
		t.Fatalf("WorktreeCreate が Read の上限で打ち切られた（Startup を使っていない）: %v", err)
	}
	if _, err := client.WorktreeRemove(context.Background(), herdr.WorktreeRemoveParams{WorkspaceID: "w1"}); err != nil {
		t.Fatalf("WorktreeRemove が Read の上限で打ち切られた（Startup を使っていない）: %v", err)
	}
}
