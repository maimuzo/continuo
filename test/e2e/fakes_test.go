package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/normalize"
)

// TestE2E_偽のherdrが着手から片付けまでのメソッドに応答する は、テスト用herdr mock 単体を
// **本物の herdr クライアント**（internal/herdr）から叩いて、応答の形を確かめる。
//
// 目的: 手順書を通すのに要る8つのメソッドが揃っていること。
// **`pane.split` は continuo が着手では呼ばない**（設計 4-5）が、本物の herdr は答えるので、
// mockでも答えられることをここで確かめる。
//
// 与える情報: 一時ディレクトリに作った worktree のふりをしたディレクトリ1つ。
//
// 成功条件: ping / worktree.open / pane.split / pane.list / agent.start / agent.prompt /
// agent.wait / pane.close / worktree.remove がすべて成功し、**worktree.remove で
// ディレクトリの実体が消えること**。
func TestE2E_偽のherdrが着手から片付けまでのメソッドに応答する(t *testing.T) {
	// **socket のパスを短く保つ**（macOS の Unix domain socket の上限は103バイト。
	// `t.TempDir()` はテスト名を含む長いパスになるので使わない）。
	root, err := os.MkdirTemp("", "ce2eh")
	if err != nil {
		t.Fatalf("一時ディレクトリを作れません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	fh := newFakeHerdr(t, root)
	client := herdr.New(fh.SocketPath, herdr.Timeouts{
		Read: 5 * time.Second, Startup: 5 * time.Second, Turn: 10 * time.Second,
	})
	ctx := context.Background()

	pong, err := client.Ping(ctx)
	if err != nil {
		t.Fatalf("ping に失敗しました: %v", err)
	}
	if pong.Protocol != 19 {
		t.Fatalf("ping が返した protocol が 19 ではありません: %d", pong.Protocol)
	}

	worktreePath := filepath.Join(root, "wt")
	if err := os.MkdirAll(worktreePath, 0o700); err != nil {
		t.Fatalf("worktree のふりをするディレクトリを作れません: %v", err)
	}
	opened, err := client.WorktreeOpen(ctx, herdr.WorktreeOpenParams{Path: worktreePath})
	if err != nil {
		t.Fatalf("worktree.open に失敗しました: %v", err)
	}
	if opened.Workspace.WorkspaceID == "" || opened.RootPane.PaneID == "" {
		t.Fatalf("worktree.open が workspace と root pane を返していません: %+v", opened)
	}

	split, err := client.PaneSplit(ctx, herdr.PaneSplitParams{
		Direction:   "down",
		Cwd:         worktreePath,
		WorkspaceID: opened.Workspace.WorkspaceID,
	})
	if err != nil {
		t.Fatalf("pane.split に失敗しました: %v", err)
	}
	if split.Pane.PaneID == "" {
		t.Fatalf("pane.split が pane を返していません: %+v", split)
	}

	list, err := client.PaneList(ctx, herdr.PaneListParams{WorkspaceID: opened.Workspace.WorkspaceID})
	if err != nil {
		t.Fatalf("pane.list に失敗しました: %v", err)
	}
	if len(list.Panes) != 2 {
		t.Fatalf("pane.list が返した pane の数が 2 ではありません: %d", len(list.Panes))
	}

	// 分割した pane は片付けて、着手と同じ「workspace に pane が1つ」の形に戻す。
	if _, err := client.PaneClose(ctx, herdr.PaneCloseParams{PaneID: split.Pane.PaneID}); err != nil {
		t.Fatalf("pane.close に失敗しました: %v", err)
	}

	name, _ := normalize.Normalize("continuo-fake-1")
	if _, err := client.AgentStart(ctx, herdr.AgentStartParams{
		Name:   name,
		Kind:   "claude",
		PaneID: opened.RootPane.PaneID,
		Args: []string{
			"--settings", filepath.Join(root, "settings.json"),
			"--session-id", "11111111-2222-3333-4444-555555555555",
			"--permission-mode", "dontAsk",
		},
	}); err != nil {
		t.Fatalf("agent.start に失敗しました: %v", err)
	}

	prompted, err := client.AgentPrompt(ctx, herdr.AgentPromptParams{
		Target: name,
		Text:   "テストの本文",
		Wait:   &herdr.AgentWaitOptions{TimeoutMs: 5000, Until: []herdr.AgentStatus{herdr.AgentStatusIdle}},
	})
	if err != nil {
		t.Fatalf("agent.prompt に失敗しました: %v", err)
	}
	if prompted.Agent.AgentStatus != herdr.AgentStatusIdle {
		t.Fatalf("agent.prompt が idle を返していません: %q", prompted.Agent.AgentStatus)
	}
	if got := fh.Prompts(); len(got) != 1 || got[0] != "テストの本文" {
		t.Fatalf("テスト用herdr mock が受け取った本文が違います: %v", got)
	}

	if _, err := client.AgentWait(ctx, herdr.AgentWaitParams{
		Target: name, TimeoutMs: 5000, Until: []herdr.AgentStatus{herdr.AgentStatusIdle},
	}); err != nil {
		t.Fatalf("agent.wait に失敗しました: %v", err)
	}

	if _, err := client.WorktreeRemove(ctx, herdr.WorktreeRemoveParams{
		WorkspaceID: opened.Workspace.WorkspaceID, Force: true,
	}); err != nil {
		t.Fatalf("worktree.remove に失敗しました: %v", err)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("worktree.remove がディレクトリの実体を消していません: %v", err)
	}
	if panes := fh.LivePanes(); len(panes) != 0 {
		t.Fatalf("worktree.remove のあとに pane が残っています: %v", panes)
	}
}

// TestE2E_偽のghと偽のGraphQLが1枚のボードを共有する は、2つのmockが同じ状態を
// 読み書きしていることを確かめる。
//
// 目的: 手順書を通すのに要る「状態を持つテスト用gh mock」であること。
//   - `gh project item-add` で足した issue が `gh project item-list` に出る
//   - **GraphQL で Status を書き換えると、次の `gh project item-list` に反映される**
//     （continuo は GraphQL でしか Status を書かないので、これが繋がっていないと
//     手順書の段8 を確かめられない）
//   - `gh issue comment` で書いたコメントが GraphQL のコメント取得から見える
//
// 与える情報: 既に issue が1件載っている偽のボードと、そこを向いたテスト用gh mock・テスト用GraphQL mock。
//
// 成功条件: 上の3つがすべて成り立つこと。
func TestE2E_偽のghと偽のGraphQLが1枚のボードを共有する(t *testing.T) {
	root, err := os.MkdirTemp("", "ce2eb")
	if err != nil {
		t.Fatalf("一時ディレクトリを作れません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	binDir := filepath.Join(root, "bin")
	buildFakeGH(t, filepath.Join(root, "ghsrc"), binDir)
	bd := newBoardFile(t, boardPathIn(root), "octofake", "sandbox")
	fg := newFakeGitHub(t, bd.Path)
	gh := filepath.Join(binDir, "gh")
	env := []string{
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"CONTINUO_E2E_BOARD=" + bd.Path,
	}

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(gh, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("`gh %s` に失敗しました: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	// 足す前は1件だけである。
	if got := strings.Count(run("project", "item-list", "7", "--owner", "octofake",
		"--format", "json"), `"number"`); got != 1 {
		t.Fatalf("ボードに載っている issue の件数が 1 ではありません: %d", got)
	}

	url := strings.TrimSpace(run("issue", "create", "--repo", "octofake/sandbox",
		"--title", "足す issue", "--body", "本文"))
	// **作っただけではボードに載らない。**
	if strings.Contains(run("project", "item-list", "7", "--owner", "octofake", "--format", "json"), url) {
		t.Fatalf("`gh issue create` だけでボードに載ってしまいました: %s", url)
	}

	run("project", "item-add", "7", "--owner", "octofake", "--url", url)
	list := run("project", "item-list", "7", "--owner", "octofake", "--format", "json")
	if !strings.Contains(list, url) {
		t.Fatalf("`gh project item-add` で足した issue が item-list に出ません:\n%s", list)
	}

	itemID := bd.Read(t).findIssueByURL(url).ItemID
	nodeID := bd.Read(t).findIssueByURL(url).NodeID

	// **GraphQL で Status を書く**（continuo と同じ経路）。`Ready` は選択肢の2番目である。
	postGraphQL(t, fg.URL, map[string]any{
		"query": "mutation { updateProjectV2ItemFieldValue(input: {}) { projectV2Item { id } } }",
		"variables": map[string]any{
			"itemId": itemID, "optionId": "opt1",
		},
	})
	list = run("project", "item-list", "7", "--owner", "octofake", "--format", "json")
	if !strings.Contains(list, `"status": "Ready"`) {
		t.Fatalf("GraphQL で書いた Status が `gh project item-list` に出ません:\n%s", list)
	}

	// **`gh issue comment` で書いたコメントが GraphQL から見える。**
	run("issue", "comment", url, "--body", "<!-- continuo:agent -->\n書きました")
	got := postGraphQL(t, fg.URL, map[string]any{
		"query":     "query($issueId: ID!, $first: Int!) { node(id: $issueId) { comments(first: $first, orderBy: {}) { nodes { body } } } }",
		"variables": map[string]any{"issueId": nodeID, "first": 50},
	})
	// **JSON の中では `<` が `\u003c` に符号化される**ので、印そのものではなく
	// 印の中の語で確かめる。
	if !strings.Contains(got, "continuo:agent") {
		t.Fatalf("`gh issue comment` で書いたコメントが GraphQL から見えません:\n%s", got)
	}
}

// postGraphQL はテスト用GraphQL mockへ1件のリクエストを送り、応答の本文を返す。
//
// **クエリ本文は種別の判別にだけ使われる**（偽サーバは本物のパーサを持たない）ので、
// 判別に要る断片さえ含んでいればよい。
//
// t: 呼び出し元のテスト。
// url: 偽サーバのエンドポイント。
// body: 送るリクエストの中身。
// 戻り値: 応答の本文。
func postGraphQL(t *testing.T, url string, body map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("リクエストを JSON 化できません: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("テスト用GraphQL mockへ送れません: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out bytes.Buffer
	if _, err := out.ReadFrom(resp.Body); err != nil {
		t.Fatalf("テスト用GraphQL mockの応答を読めません: %v", err)
	}
	return out.String()
}
