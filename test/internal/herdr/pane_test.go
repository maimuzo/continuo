package herdr_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// paneSplitSchemaKeys は pane.split の実スキーマ（PaneSplitParams）に定義されている
// 引数の全集合である。
var paneSplitSchemaKeys = []string{
	"direction", "cwd", "env", "focus", "ratio", "target_pane_id", "workspace_id",
}

// paneRenameSchemaKeys は pane.rename の実スキーマ（PaneRenameParams）に定義されている
// 引数の全集合である。
var paneRenameSchemaKeys = []string{"pane_id", "label"}

// paneReportAgentSchemaKeys は pane.report_agent の実スキーマ
// （PaneReportAgentParams）に定義されている引数の全集合である。
var paneReportAgentSchemaKeys = []string{
	"pane_id", "source", "agent", "state",
	"agent_session_id", "agent_session_path", "message", "seq",
}

// 目的: pane を作るときに、実スキーマどおりの引数（direction / cwd / env / focus）で
// 送られることを確認する（設計 2-1 の「socket API の実在するメソッドと引数」。
// **pane.split に label という引数は無い**ので、送っていないことも確かめる）。
// 与える情報: direction に "down"、cwd に worktree のパス、env に
// CLAUDE_CODE_RETRY_WATCHDOG、focus に偽を設定した PaneSplitParams。
// 成功条件: 偽サーバが受け取った params の direction / cwd / env が届いており、
// focus が **偽として明示的に送られている**こと（bool の既定値が落ちていないこと）。
// 実スキーマに無いキーを1つも送っていないこと。
func TestPaneSplit_実スキーマの引数で送られる(t *testing.T) {
	const worktreePath = "/Users/tester/.continuo/worktrees/maimuzo-continuo-87"

	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.PaneSplitResult{
			Type: "pane_info",
			Pane: herdr.Pane{PaneID: "w1:p2", Cwd: worktreePath},
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})

	noFocus := false
	result, err := client.PaneSplit(context.Background(), herdr.PaneSplitParams{
		Direction: "down",
		Cwd:       worktreePath,
		Env:       map[string]string{"CLAUDE_CODE_RETRY_WATCHDOG": "1"},
		Focus:     &noFocus,
	})
	if err != nil {
		t.Fatalf("PaneSplit が失敗した: %v", err)
	}
	if result.Pane.PaneID != "w1:p2" {
		t.Fatalf("応答の pane_id を読み取れていない: got %q", result.Pane.PaneID)
	}

	params := sentParams(t, fs, herdr.MethodPaneSplit)
	if got := params["direction"]; got != "down" {
		t.Fatalf("direction が想定と違う: got %v", got)
	}
	if got := params["cwd"]; got != worktreePath {
		t.Fatalf("cwd に worktree のパスが送られていない: got %v", got)
	}
	focus, ok := params["focus"]
	if !ok {
		t.Fatalf("focus が送られていない（明示的に偽を送る必要がある）: %v", params)
	}
	if focus != false {
		t.Fatalf("focus が偽として送られていない: got %v", focus)
	}
	env, ok := params["env"].(map[string]any)
	if !ok {
		t.Fatalf("env がオブジェクトとして送られていない: %v", params["env"])
	}
	if got := env["CLAUDE_CODE_RETRY_WATCHDOG"]; got != "1" {
		t.Fatalf("env の CLAUDE_CODE_RETRY_WATCHDOG が送られていない: got %v", got)
	}
	assertSchemaKeys(t, params, herdr.MethodPaneSplit, paneSplitSchemaKeys)
	if _, exists := params["label"]; exists {
		t.Fatalf("pane.split に label を送っている（label は pane.rename で書く）: %v", params)
	}
}

// 目的: pane に label を書く経路が pane.rename であり、pane_id と label の2つを送ることを
// 確認する（設計 3-3: 「pane の label に issue の URL を書く」が復元の第2の経路である。
// **pane.split の引数に label は無い**ので、作った直後にこれを呼ぶ）。
// 与える情報: pane の ID と、issue の URL を label に設定した PaneRenameParams。
// 成功条件: 送られた method が "pane.rename" で、pane_id と label がそのまま届くこと。
// 実スキーマに無いキー（name / title など）を送っていないこと。
func TestPaneRename_pane_idとlabelを送る(t *testing.T) {
	const paneID = "w1:p2"
	const issueURL = "https://github.com/maimuzo/koetsumugi/issues/188"

	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.PaneRenameResult{
			Type: "pane_info",
			Pane: herdr.Pane{PaneID: paneID, Label: issueURL},
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})
	result, err := client.PaneRename(context.Background(), herdr.PaneRenameParams{
		PaneID: paneID,
		Label:  issueURL,
	})
	if err != nil {
		t.Fatalf("PaneRename が失敗した: %v", err)
	}
	if result.Pane.Label != issueURL {
		t.Fatalf("応答の label を読み取れていない: got %q, want %q", result.Pane.Label, issueURL)
	}

	params := sentParams(t, fs, herdr.MethodPaneRename)
	if got := params["pane_id"]; got != paneID {
		t.Fatalf("pane_id が想定と違う: got %v, want %q", got, paneID)
	}
	if got := params["label"]; got != issueURL {
		t.Fatalf("label が想定と違う: got %v, want %q", got, issueURL)
	}
	assertSchemaKeys(t, params, herdr.MethodPaneRename, paneRenameSchemaKeys)
}

// 目的: pane.close の引数が pane_id という名前で送られることを確認する（設計 2-1）。
// 与える情報: PaneID を設定した PaneCloseParams。
// 成功条件: params が pane_id 1個だけであること（pane という名前で送っていないこと）。
func TestPaneClose_pane_idを送る(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.PaneCloseResult{Type: "ok"})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})
	if _, err := client.PaneClose(context.Background(), herdr.PaneCloseParams{PaneID: "w1:p2"}); err != nil {
		t.Fatalf("PaneClose が失敗した: %v", err)
	}

	params := sentParams(t, fs, herdr.MethodPaneClose)
	if len(params) != 1 {
		t.Fatalf("pane.close の引数が pane_id だけになっていない: %v", params)
	}
	if got := params["pane_id"]; got != "w1:p2" {
		t.Fatalf("pane_id が想定と違う: got %v", got)
	}
}

// 目的: pane.list の応答から Claude Code のセッション UUID を取り出せることを確認する
// （設計 3-4 の復元手順の段5「pane の agent_session から Claude Code のセッション UUID を
// 取り、hook の対応づけを復元する」）。
//
// agent_session の形は実スキーマ（success_response の AgentSessionInfo）と
// `herdr agent list` の実測の両方で確定している。**kind が "id" のときだけ value が
// セッション UUID である**ことを確かめる（"path" のときはセッションファイルのパスなので
// UUID として使ってはならない）。
// 与える情報: kind が "id" の pane、kind が "path" の pane、agent_session が無い pane を
// 含む pane.list の応答を返す偽サーバ。
// 成功条件: kind が "id" の pane からだけ UUID が取れ、他の2つでは「取れない」と分かること。
func TestPaneList_agent_sessionからセッションUUIDを取れる(t *testing.T) {
	const sessionUUID = "0f7a9d1c-3b2e-4a55-9c11-6d8e0b7f2a34"
	const sessionPath = "/Users/tester/.claude/projects/-tmp/0f7a9d1c.jsonl"

	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		// 実機の応答（`herdr pane list` の実測）と同じ形を、生の JSON で組み立てて返す。
		raw := json.RawMessage(`{"type":"pane_list","panes":[
			{"pane_id":"w1:p1","agent_session":{"agent":"claude","kind":"id",
			 "source":"herdr:claude","value":"` + sessionUUID + `"}},
			{"pane_id":"w1:p2","agent_session":{"agent":"claude","kind":"path",
			 "source":"herdr:claude","value":"` + sessionPath + `"}},
			{"pane_id":"w1:p3"}
		]}`)
		writeResponse(t, conn, rpcResponse{ID: req.ID, Result: raw})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})

	result, err := client.PaneList(context.Background(), herdr.PaneListParams{})
	if err != nil {
		t.Fatalf("PaneList が失敗した: %v", err)
	}
	if result.Type != "pane_list" {
		t.Fatalf("応答の判別子が想定と違う: got %q, want %q", result.Type, "pane_list")
	}
	if len(result.Panes) != 3 {
		t.Fatalf("pane の件数が想定と違う: got %d, want 3", len(result.Panes))
	}

	cases := []struct {
		name     string
		pane     herdr.Pane
		wantUUID string
		wantOK   bool
	}{
		{name: "kind が id の pane からは取れる", pane: result.Panes[0], wantUUID: sessionUUID, wantOK: true},
		{name: "kind が path の pane からは取れない", pane: result.Panes[1], wantUUID: "", wantOK: false},
		{name: "agent_session が無い pane からは取れない", pane: result.Panes[2], wantUUID: "", wantOK: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.pane.SessionUUID()
			if ok != tc.wantOK {
				t.Fatalf("取れたかどうかが想定と違う: got %v, want %v（pane=%s）", ok, tc.wantOK, tc.pane.PaneID)
			}
			if got != tc.wantUUID {
				t.Fatalf("セッション UUID が想定と違う: got %q, want %q", got, tc.wantUUID)
			}
		})
	}
}

// 目的: pane.report_agent が必須の4つの引数（pane_id / source / agent / state）を
// 送れることを確認する（設計 2-1: 実プロセスを起動せずに「agent が居る pane」として
// 登録できる。統合テストで、実際に Claude Code を起動せずに状態遷移を再現するために使う）。
// 与える情報: 必須の4つと、任意の agent_session_id を設定した PaneReportAgentParams。
// state には PaneAgentState の値（done を含まない enum）を渡す。
// 成功条件: 4つの必須引数がすべて実スキーマどおりの名前で届き、agent_session_id も
// 届くこと。実スキーマに無いキーを送っていないこと。
func TestPaneReportAgent_必須の4つを送る(t *testing.T) {
	const sessionUUID = "0f7a9d1c-3b2e-4a55-9c11-6d8e0b7f2a34"

	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.PaneReportAgentResult{
			Type: "pane_info",
			Pane: herdr.Pane{PaneID: "w1:p2"},
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})
	result, err := client.PaneReportAgent(context.Background(), herdr.PaneReportAgentParams{
		PaneID:         "w1:p2",
		Source:         "continuo",
		Agent:          "claude",
		State:          herdr.PaneAgentStateWorking,
		AgentSessionID: sessionUUID,
	})
	if err != nil {
		t.Fatalf("PaneReportAgent が失敗した: %v", err)
	}
	if result.Pane.PaneID != "w1:p2" {
		t.Fatalf("応答の pane_id を読み取れていない: got %q", result.Pane.PaneID)
	}

	params := sentParams(t, fs, herdr.MethodPaneReportAgent)
	required := map[string]string{
		"pane_id": "w1:p2",
		"source":  "continuo",
		"agent":   "claude",
		"state":   "working",
	}
	for key, want := range required {
		got, ok := params[key]
		if !ok {
			t.Fatalf("必須の引数 %q が送られていない: %v", key, params)
		}
		if got != want {
			t.Fatalf("引数 %q が想定と違う: got %v, want %q", key, got, want)
		}
	}
	if got := params["agent_session_id"]; got != sessionUUID {
		t.Fatalf("agent_session_id が想定と違う: got %v", got)
	}
	assertSchemaKeys(t, params, herdr.MethodPaneReportAgent, paneReportAgentSchemaKeys)
}
