// Package daemon_test は、**ビルドした `continuo` のバイナリを実際に起動して**
// 復元 → 巡回 → 終了までを確かめる。
//
// **実 herdr には繋がない。**`net.Listen("unix", ...)` でテスト用socket mockを立てる。
// **本番のボード（project #3）へは接続しない。**`httptest.Server` でテスト用GraphQL mockを
// 立て、環境変数 `CONTINUO_GITHUB_GRAPHQL_ENDPOINT` でバイナリの接続先をそこへ向ける。
// **Claude Code は起動しない。**turn の終わりは、テスト用herdr mock の `agent.prompt` の応答と、
// テストが hook の socket へ直接書き込む `Stop` で再現する。
// **`gh` と `ghq` もmockを PATH の先頭へ置く。**本物の認証情報を読ませない。
package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// ===== 呼び出しの並びを1本にまとめる記録 =====

// timeline はテスト用herdr mock とテスト用GitHub mock の呼び出しを1本の並びに混ぜて記録する。
//
// **これが無いと「復元を終えてから巡回が始まる」ことを確かめられない。**
// 復元の `pane.list` と巡回の候補の取得は別のサーバへ届くので、
// それぞれが自分の記録しか持たないと前後関係を比べる手段が無い。
type timeline struct {
	mu      sync.Mutex
	entries []string
}

// note は呼び出しを1件積む。
//
// name: 積む名前（`herdr.pane.list` / `gql.items("Status":"Ready","In Progress")` など）。
func (tl *timeline) note(name string) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.entries = append(tl.entries, name)
}

// Entries は積んだ名前を積んだ順に返す。
func (tl *timeline) Entries() []string {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	out := make([]string, len(tl.entries))
	copy(out, tl.entries)
	return out
}

// IndexOfPrefix は接頭辞が一致する最初の位置を返す（無ければ -1）。
//
// prefix: 探す接頭辞。
// 戻り値: 位置。
func (tl *timeline) IndexOfPrefix(prefix string) int {
	for i, e := range tl.Entries() {
		if strings.HasPrefix(e, prefix) {
			return i
		}
	}
	return -1
}

// ===== テスト用herdr mock socket サーバ =====

// rpcErr は herdr の socket API のエラー応答である。
type rpcErr struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// herdrRequest はテスト用herdr mock が受け取ったリクエスト1件である。
type herdrRequest struct {
	// Method は呼ばれたメソッド名である。
	Method string
	// Params は params をそのまま map にしたものである。
	Params map[string]any
}

// fakeHerdr は herdr の socket API の代わりに使う偽のサーバである。
type fakeHerdr struct {
	// SocketPath は listen している socket の絶対パスである。
	SocketPath string
	// Protocol は ping が返す protocol 版である。
	Protocol int

	mu       sync.Mutex
	requests []herdrRequest
	handlers map[string]func(params map[string]any) (any, *rpcErr)
	// workspaces は workspace の ID から worktree のパスを引く写像である
	// （`worktree.remove` で実体を消すために持つ）。
	workspaces map[string]string
	timeline   *timeline
	// repoDir は `worktree.remove` のあとに `git worktree prune` を叩くリポジトリである。
	repoDir string
}

// newFakeHerdr はテスト用herdr mock を1本立てる。
//
// t: 呼び出し元のテスト。socket の後始末を t.Cleanup に登録する。
// dir: socket を置くディレクトリ（**短く保つこと。**macOS の上限は103バイト）。
// tl: 呼び出しの並びの記録。
// 戻り値: 起動したテスト用herdr mock。
func newFakeHerdr(t *testing.T, dir string, tl *timeline) *fakeHerdr {
	t.Helper()

	socketPath := filepath.Join(dir, "h.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("テスト用herdr mock の socket を bind できません（%s）: %v", socketPath, err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	fh := &fakeHerdr{
		SocketPath: socketPath,
		Protocol:   19,
		handlers:   map[string]func(map[string]any) (any, *rpcErr){},
		workspaces: map[string]string{},
		timeline:   tl,
	}
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

// Handle はメソッドの台本を差し替える。
//
// method: 対象のメソッド名。
// fn: 応答を決める関数。**接続ごとの goroutine から呼ばれるので t.Fatalf を使ってはならない。**
func (fh *fakeHerdr) Handle(method string, fn func(params map[string]any) (any, *rpcErr)) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.handlers[method] = fn
}

// RegisterWorkspace は workspace の ID と worktree のパスの対応を登録する
// （`worktree.remove` で実体を消すために要る）。
//
// id: herdr の workspace の ID。
// path: worktree の絶対パス。
func (fh *fakeHerdr) RegisterWorkspace(id, path string) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.workspaces[id] = path
}

// SetRepoDir は `worktree.remove` のあとに prune を叩くリポジトリを設定する。
//
// dir: clone の絶対パス。
func (fh *fakeHerdr) SetRepoDir(dir string) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.repoDir = dir
}

// serve は1本の接続を処理する。
//
// **接続ごとの goroutine から呼ばれるので t.Fatalf を使ってはならない。**
func (fh *fakeHerdr) serve(t *testing.T, conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))

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
	fh.requests = append(fh.requests, herdrRequest{Method: req.Method, Params: req.Params})
	handler := fh.handlers[req.Method]
	fh.mu.Unlock()
	fh.timeline.note("herdr." + req.Method)

	result, rerr := fh.dispatch(req.Method, req.Params, handler)

	var resp map[string]any
	if rerr != nil {
		resp = map[string]any{"id": req.ID, "error": map[string]any{"code": rerr.Code, "message": rerr.Message}}
	} else {
		resp = map[string]any{"id": req.ID, "result": result}
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

// dispatch はメソッドに対する応答を決める。
//
// method: メソッド名。
// params: 受け取った params。
// handler: 差し替えられた台本（nil なら既定の応答）。
// 戻り値: 返す result と、エラー応答。
func (fh *fakeHerdr) dispatch(
	method string,
	params map[string]any,
	handler func(map[string]any) (any, *rpcErr),
) (any, *rpcErr) {
	if handler != nil {
		return handler(params)
	}
	switch method {
	case "ping":
		return map[string]any{
			"type": "pong", "version": "0.8.0-fake", "protocol": fh.Protocol,
			"capabilities": map[string]any{"live_handoff": true},
		}, nil
	case "pane.close":
		return map[string]any{"type": "pane_closed"}, nil
	case "worktree.open":
		// **本物と同じく、path と branch は片方だけ受け付ける**（実測: 2026-08-20）。
		openPath, _ := params["path"].(string)
		openBranch, _ := params["branch"].(string)
		if (openPath == "") == (openBranch == "") {
			return nil, &rpcErr{Code: "invalid_request", Message: "exactly one of path or branch is required"}
		}
		// **本物の herdr と同じく、開いた worktree のパスと workspace を答える。**
		// 片付けは消す直前にこれを呼び、返ってきた workspace の ID だけを
		// `worktree.remove` の宛先にする（設計 3-9 の段3）。
		path := fmt.Sprint(params["path"])
		id := fh.workspaceIDForPath(path)
		return map[string]any{
			"type":      "worktree_opened",
			"workspace": map[string]any{"workspace_id": id},
			"tab":       map[string]any{"tab_id": id + ":t1"},
			"root_pane": map[string]any{"pane_id": "p-" + id, "workspace_id": id},
			"worktree":  map[string]any{"path": path},
		}, nil
	case "worktree.remove":
		id := fmt.Sprint(params["workspace_id"])
		fh.mu.Lock()
		path := fh.workspaces[id]
		repo := fh.repoDir
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
	case "agent.get", "agent.wait":
		return map[string]any{
			"type":  "agent_info",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle"},
		}, nil
	default:
		return nil, &rpcErr{Code: "unknown_method", Message: method}
	}
}

// workspaceIDForPath は worktree のパスから herdr workspace の ID を引く。
//
// **登録が無ければその場で1つ作って覚える。**本物の herdr の `worktree.open` は、
// 既に開いていればその workspace を返し、開いていなければ開いて返すためである。
//
// path: worktree の絶対パス。
// 戻り値: herdr workspace の ID。
func (fh *fakeHerdr) workspaceIDForPath(path string) string {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	for id, registered := range fh.workspaces {
		if registered == path {
			return id
		}
	}
	id := fmt.Sprintf("ws%d", len(fh.workspaces)+1)
	fh.workspaces[id] = path
	return id
}

// Requests は受け取ったリクエストを受け取った順に返す。
func (fh *fakeHerdr) Requests() []herdrRequest {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	out := make([]herdrRequest, len(fh.requests))
	copy(out, fh.requests)
	return out
}

// ClosedPanes は `pane.close` に渡された pane_id を、受け取った順に返す。
func (fh *fakeHerdr) ClosedPanes() []string {
	var ids []string
	for _, r := range fh.Requests() {
		if r.Method == "pane.close" {
			id, _ := r.Params["pane_id"].(string)
			ids = append(ids, id)
		}
	}
	return ids
}

// CountMethod は method が呼ばれた回数を返す。
//
// method: 数えるメソッド名。
// 戻り値: 回数。
func (fh *fakeHerdr) CountMethod(method string) int {
	n := 0
	for _, r := range fh.Requests() {
		if r.Method == method {
			n++
		}
	}
	return n
}

// ===== テスト用GitHub GraphQL mock サーバ =====

// boardItem は偽ボードの project item 1件である。
type boardItem struct {
	// ItemID は project item の ID である。
	ItemID string
	// NodeID は下敷きの GitHub issue のノード ID である。
	NodeID string
	// Number は issue 番号である。
	Number int
	// State は Status の値である。
	State string
}

// boardComment は issue に付いたコメント1件である。
type boardComment struct {
	// Body は本文である。
	Body string
	// CreatedAt は作成時刻である。
	CreatedAt time.Time
}

// fakeGitHub は GitHub の GraphQL API の代わりに使う偽のサーバである。
//
// **本番のボードへは1リクエストも送らない。**
type fakeGitHub struct {
	// URL は偽サーバのエンドポイントである。
	URL string
	// Owner はボードの所有者名である。
	Owner string

	mu       sync.Mutex
	items    map[string]*boardItem
	comments map[string][]boardComment
	timeline *timeline
	// queries は受け取ったクエリの種別を受け取った順に記録したものである。
	queries []string
}

// statusOptions は偽ボードの Status の選択肢である（設定と一致させる）。
var statusOptions = []string{"Ready", "In Progress", "In Review", "Blocked", "Done"}

// newFakeGitHub はテスト用GraphQL mockを1本立てる。
//
// t: 呼び出し元のテスト。後始末を t.Cleanup に登録する。
// owner: ボードの所有者名。
// tl: 呼び出しの並びの記録。
// items: 最初にボードへ載せる item。
// 戻り値: 起動した偽サーバ。
func newFakeGitHub(t *testing.T, owner string, tl *timeline, items ...*boardItem) *fakeGitHub {
	t.Helper()
	fg := &fakeGitHub{
		Owner:    owner,
		items:    map[string]*boardItem{},
		comments: map[string][]boardComment{},
		timeline: tl,
	}
	for _, it := range items {
		fg.items[it.ItemID] = it
	}
	srv := httptest.NewServer(http.HandlerFunc(fg.handle))
	t.Cleanup(srv.Close)
	fg.URL = srv.URL
	return fg
}

// SetState は item の Status を書き換える（エージェントが gh で動かした状況の再現）。
//
// itemID: project item の ID。
// state: 新しい Status。
func (fg *fakeGitHub) SetState(itemID, state string) {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	if it, ok := fg.items[itemID]; ok {
		it.State = state
	}
}

// StateOf は item の現在の Status を返す。
//
// itemID: project item の ID。
// 戻り値: Status。見つからなければ空文字。
func (fg *fakeGitHub) StateOf(itemID string) string {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	if it, ok := fg.items[itemID]; ok {
		return it.State
	}
	return ""
}

// AddComment は issue にコメントを足す。
//
// nodeID: 下敷きの GitHub issue のノード ID。
// body: 本文。
func (fg *fakeGitHub) AddComment(nodeID, body string) {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	fg.comments[nodeID] = append(fg.comments[nodeID], boardComment{Body: body, CreatedAt: time.Now()})
}

// handle は1件の GraphQL リクエストに答える。
func (fg *fakeGitHub) handle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	kind, body := fg.respond(req.Query, req.Variables)

	fg.mu.Lock()
	fg.queries = append(fg.queries, kind)
	fg.mu.Unlock()
	fg.timeline.note("gql." + kind)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": body})
}

// respond はクエリの種別を判別して応答の data を組み立てる。
//
// query: 受け取った GraphQL のクエリ本文。
// vars: 受け取った変数。
// 戻り値の1つ目: クエリの種別（記録に使う名前）。
// 戻り値の2つ目: 応答の data。
func (fg *fakeGitHub) respond(query string, vars map[string]any) (string, map[string]any) {
	switch {
	case strings.Contains(query, "field(name: $statusField)"):
		return "bootstrap", map[string]any{
			"repositoryOwner": map[string]any{
				"projectV2": map[string]any{
					"id":    "PVT_board",
					"field": fg.statusFieldPayload(),
				},
			},
		}
	case strings.Contains(query, "nodes(ids: $ids)"):
		return "by_ids", map[string]any{"nodes": fg.itemsByIDs(vars)}
	case strings.Contains(query, "items(first: 100"):
		q, _ := vars["q"].(string)
		return "items(" + q + ")", map[string]any{
			"repositoryOwner": map[string]any{
				"projectV2": map[string]any{
					"items": map[string]any{
						"pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil},
						"nodes":    fg.itemsByQuery(q),
					},
				},
			},
		}
	case strings.Contains(query, "updateProjectV2ItemFieldValue"):
		return "update_status", fg.updateStatus(vars)
	case strings.Contains(query, "comments(first: $first"):
		return "comments", map[string]any{"node": fg.commentsPayload(vars)}
	case strings.Contains(query, "addComment"):
		return "add_comment", fg.addComment(vars)
	default:
		return "unknown", map[string]any{}
	}
}

// statusFieldPayload は Status フィールドの応答を組み立てる。
func (fg *fakeGitHub) statusFieldPayload() map[string]any {
	options := make([]any, 0, len(statusOptions))
	for i, name := range statusOptions {
		options = append(options, map[string]any{"id": fmt.Sprintf("opt%d", i), "name": name})
	}
	return map[string]any{
		"__typename": "ProjectV2SingleSelectField",
		"id":         "PVTSSF_status",
		"options":    options,
	}
}

// itemsByIDs は ID 指定の取り直しの応答を組み立てる。
//
// **見つからない ID には null を返す**（本物と同じ。「もう見えない」を表す）。
func (fg *fakeGitHub) itemsByIDs(vars map[string]any) []any {
	raw, _ := vars["ids"].([]any)
	fg.mu.Lock()
	defer fg.mu.Unlock()
	out := make([]any, 0, len(raw))
	for _, v := range raw {
		id, _ := v.(string)
		it, ok := fg.items[id]
		if !ok {
			out = append(out, nil)
			continue
		}
		out = append(out, fg.itemPayload(it, true))
	}
	return out
}

// itemsByQuery は候補の取得の応答を組み立てる。
//
// q: `items(query:)` に渡された検索クエリ（`status:"Ready","In Progress"` の形）。
func (fg *fakeGitHub) itemsByQuery(q string) []any {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	var out []any
	for _, it := range fg.sortedItems() {
		if !strings.Contains(q, `"`+it.State+`"`) {
			continue
		}
		out = append(out, fg.itemPayload(it, false))
	}
	return out
}

// sortedItems は item を番号の昇順で返す（ボードの並び順の代わり）。
//
// **呼び出し側が fg.mu を保持していること。**
func (fg *fakeGitHub) sortedItems() []*boardItem {
	out := make([]*boardItem, 0, len(fg.items))
	for _, it := range fg.items {
		out = append(out, it)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Number < out[i].Number {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// itemPayload は project item 1件の応答を組み立てる。
//
// **呼び出し側が fg.mu を保持していること。**
//
// it: 対象の item。
// withTypename: `nodes(ids:)` 経由なら真（`__typename` が要る）。
func (fg *fakeGitHub) itemPayload(it *boardItem, withTypename bool) map[string]any {
	payload := map[string]any{
		"id":         it.ItemID,
		"isArchived": false,
		"fieldValueByName": map[string]any{
			"__typename": "ProjectV2ItemFieldSingleSelectValue",
			"name":       it.State,
			"optionId":   "opt0",
		},
		"content": map[string]any{
			"__typename": "Issue",
			"id":         it.NodeID,
			"number":     it.Number,
			"title":      fmt.Sprintf("テスト用の issue %d", it.Number),
			"body":       "本文",
			"url":        fmt.Sprintf("https://github.com/%s/hello-world/issues/%d", fg.Owner, it.Number),
			"state":      "OPEN",
			"repository": map[string]any{
				"nameWithOwner":    fg.Owner + "/hello-world",
				"defaultBranchRef": map[string]any{"name": "main"},
			},
			"labels":         map[string]any{"nodes": []any{}},
			"assignees":      map[string]any{"nodes": []any{}},
			"blockedBy":      map[string]any{"nodes": []any{}},
			"linkedBranches": map[string]any{"nodes": []any{}},
			"comments":       map[string]any{"totalCount": len(fg.comments[it.NodeID])},
		},
	}
	if withTypename {
		payload["__typename"] = "ProjectV2Item"
	}
	return payload
}

// updateStatus は Status の書き込みに答える。
//
// **選択肢の一覧を書き換える mutation には答えない**（continuo はそれを呼ばない）。
func (fg *fakeGitHub) updateStatus(vars map[string]any) map[string]any {
	itemID, _ := vars["itemId"].(string)
	optionID, _ := vars["optionId"].(string)
	fg.mu.Lock()
	defer fg.mu.Unlock()
	for i, name := range statusOptions {
		if optionID != fmt.Sprintf("opt%d", i) {
			continue
		}
		if it, ok := fg.items[itemID]; ok {
			it.State = name
		}
	}
	return map[string]any{
		"updateProjectV2ItemFieldValue": map[string]any{
			"projectV2Item": map[string]any{"id": itemID},
		},
	}
}

// commentsPayload はコメントの取得に答える（**新しい順で返す**）。
func (fg *fakeGitHub) commentsPayload(vars map[string]any) map[string]any {
	nodeID, _ := vars["issueId"].(string)
	fg.mu.Lock()
	defer fg.mu.Unlock()
	list := fg.comments[nodeID]
	nodes := make([]any, 0, len(list))
	for i := len(list) - 1; i >= 0; i-- {
		nodes = append(nodes, map[string]any{
			"id":        fmt.Sprintf("IC_%d", i),
			"url":       "https://github.com/comment",
			"body":      list[i].Body,
			"createdAt": list[i].CreatedAt.UTC().Format(time.RFC3339Nano),
			"author":    map[string]any{"login": "agent", "id": "U_1"},
		})
	}
	return map[string]any{"__typename": "Issue", "comments": map[string]any{"nodes": nodes}}
}

// addComment はコメントの投稿に答える。
func (fg *fakeGitHub) addComment(vars map[string]any) map[string]any {
	nodeID, _ := vars["subjectId"].(string)
	body, _ := vars["body"].(string)
	now := time.Now()
	fg.mu.Lock()
	fg.comments[nodeID] = append(fg.comments[nodeID], boardComment{Body: body, CreatedAt: now})
	fg.mu.Unlock()
	return map[string]any{
		"addComment": map[string]any{
			"commentEdge": map[string]any{
				"node": map[string]any{
					"id": "IC_self", "url": "https://github.com/comment", "body": body,
					"createdAt": now.UTC().Format(time.RFC3339Nano),
					"author":    map[string]any{"login": "continuo", "id": "U_2"},
				},
			},
		},
	}
}

// ===== バイナリのビルドと起動 =====

// buildBinary は `continuo` をビルドする。
//
// **リポジトリの中には出力しない**（生成物を残さないため、テストの一時ディレクトリへ出す）。
//
// t: 呼び出し元のテスト。
// outDir: 出力先のディレクトリ。
// 戻り値: ビルドしたバイナリの絶対パス。
func buildBinary(t *testing.T, outDir string) string {
	t.Helper()

	goBin, err := exec.LookPath("go")
	if err != nil {
		goBin = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("リポジトリの場所を決められません: %v", err)
	}

	bin := filepath.Join(outDir, "continuo")
	cmd := exec.Command(goBin, "build", "-o", bin, "./cmd/continuo")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("continuo をビルドできません: %v\n%s", err, out)
	}
	return bin
}

// writeFakeGH は PATH の先頭へ置く偽の `gh` と `ghq` を作る。
//
// **本物の認証情報を読ませない。**`gh auth status` は project の scope を持つ出力を返す。
// **`ghq list -p -e` はテスト用の clone のパスを返す**（信頼の検査がこの出力で
// `~/.claude.json` の鍵を引く。設計 3-6）。
//
// t: 呼び出し元のテスト。
// dir: 実行ファイルを置くディレクトリ。
// repoDir: `ghq list` が返す clone の絶対パス。
func writeFakeGH(t *testing.T, dir, repoDir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("テスト用gh mock を置くディレクトリを作れません: %v", err)
	}
	gh := `#!/bin/sh
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  cat <<'EOF'
github.com
  ✓ Logged in to github.com account tester (keyring)
  - Active account: true
  - Token scopes: 'gist', 'project', 'read:org', 'repo', 'workflow'
EOF
  exit 0
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(gh), 0o755); err != nil {
		t.Fatalf("テスト用gh mock を書けません: %v", err)
	}
	ghq := "#!/bin/sh\necho " + repoDir + "\n"
	if err := os.WriteFile(filepath.Join(dir, "ghq"), []byte(ghq), 0o755); err != nil {
		t.Fatalf("テスト用ghq mock を書けません: %v", err)
	}
}

// writeTrustFile は `~/.claude.json` に「このリポジトリは信頼済み」と書く。
//
// **読むだけの経路を通すための入力である。**continuo はこのファイルを書き換えない
// （設計 4-3）。テストが先に置いておく。
//
// t: 呼び出し元のテスト。
// home: 子プロセスへ渡す HOME。
// repoDir: clone の絶対パス。
func writeTrustFile(t *testing.T, home, repoDir string) {
	t.Helper()
	toplevel := runGit(t, repoDir, "rev-parse", "--path-format=absolute", "--show-toplevel")
	doc := map[string]any{
		"projects": map[string]any{
			toplevel: map[string]any{"hasTrustDialogAccepted": true},
		},
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("~/.claude.json を JSON 化できません: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), encoded, 0o600); err != nil {
		t.Fatalf("~/.claude.json を書けません: %v", err)
	}
}

// runGit はテストの中で git を実行し、標準出力を返す。
//
// t: 呼び出し元のテスト。失敗したらテストを止める。
// dir: `-C` に渡す作業ディレクトリ。空文字なら付けない。
// args: git の引数。
// 戻り値: 標準出力（前後の空白を落としたもの）。
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("`git %s` に失敗した: %v\n%s", strings.Join(full, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// sendHook は `continuo hook` と同じことを行う。hook の JSON を1行で socket へ書く。
//
// **`continuo hook` を起動しないのは、テストが送った時刻を制御したいからである。**
// 送る内容は同じ1行の JSON である。
//
// t: 呼び出し元のテスト。**接続ごとの goroutine から呼ばれうるので t.Errorf を使う。**
// socketPath: hook を受ける socket の絶対パス。
// event: 送る hook。
func sendHook(t *testing.T, socketPath string, event map[string]any) {
	conn, err := net.DialTimeout("unix", socketPath, 3*time.Second)
	if err != nil {
		t.Errorf("hook の socket へ繋げません（%s）: %v", socketPath, err)
		return
	}
	defer func() { _ = conn.Close() }()
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Errorf("hook を JSON 化できません: %v", err)
		return
	}
	if _, err := conn.Write(append(encoded, '\n')); err != nil {
		t.Errorf("hook を書けません: %v", err)
	}
}

// waitFor は cond が真になるまで最大 d だけ待つ。
//
// t: 呼び出し元のテスト。**テスト本体の goroutine から呼ぶこと。**
// d: 待つ長さの上限。
// message: 失敗したときに出す説明。
// cond: 判定する関数。
func waitFor(t *testing.T, d time.Duration, message string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s（%s 以内に条件を満たしませんでした）", message, d)
}

// waitProcess はプロセスの終了を最大 d だけ待つ。
//
// ctx: 待ちに適用するコンテキスト。
// cmd: 待つプロセス。
// d: 待つ長さの上限。
// 戻り値の1つ目: 終了コード。
// 戻り値の2つ目: 期限までに終わったかどうか。
func waitProcess(ctx context.Context, cmd *exec.Cmd, d time.Duration) (int, bool) {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if err == nil {
			return 0, true
		}
		if ok := asExit(err, &exitErr); ok {
			return exitErr.ExitCode(), true
		}
		return -1, true
	case <-time.After(d):
		return -1, false
	case <-ctx.Done():
		return -1, false
	}
}

// asExit は err が *exec.ExitError かを判定する。
//
// err: 判定するエラー。
// target: 一致したときに代入する先。
// 戻り値: 一致すれば true。
func asExit(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}

// syncBuffer は子プロセスの出力を溜める。
//
// **`exec.Cmd` は標準出力と標準エラーを別々の goroutine から書く**ので、
// 排他を持たない bytes.Buffer をそのまま渡すと競合する。
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

// Write は io.Writer の実装である。
//
// p: 書き込むバイト列。
// 戻り値: 書き込んだバイト数と、常に nil のエラー。
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String は溜まった出力を返す。
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// stringBox は排他つきの文字列の置き場である。
//
// **テスト用herdr mock の台本（接続ごとの goroutine）が書き、テスト本体が読む**ので、
// 素の変数だと `-race` が競合を報告する。
type stringBox struct {
	mu sync.Mutex
	s  string
}

// Set は値を書き込む。
//
// v: 書き込む値。
func (b *stringBox) Set(v string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.s = v
}

// Get は値を読み出す。
func (b *stringBox) Get() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.s
}
