// Package e2e_test は docs/trying_it_out.md の段1から段9までを、**mockだけを相手に**
// 最初から最後まで通す。
//
// **本番の GitHub には1リクエストも送らない。**ボードは JSON ファイル1枚で作ったmockで、
// `gh`（PATH の先頭に置いた偽の実行ファイル）とテスト用GraphQL mock（httptest）が
// 同じファイルを読み書きする。**project #3 には読み取りも行わない。**
//
// **実 herdr には繋がない。**`net.Listen("unix", ...)` でテスト用socket mockを立てる。
// **Claude Code は起動しない。**`agent.prompt` を受けたテスト用herdr mock が、エージェントの役
// （worktree で commit して push し、transcript を書き、`continuo hook` で Stop を送る）を演じる。
// **枠は1トークンも消費しない。**
//
// **実物のホームディレクトリは読みも書きもしない。**HOME を一時ディレクトリへ向け、
// その中に `.claude.json`（projects が空）と `.claude/` を置く。
// **実物の ghq の置き場所も触らない。**偽の `ghq` が一時ディレクトリの clone を返す。
//
// **git だけは本物を使う。**worktree の作成・削除・branch の判定はmockでは確かめられないので、
// 一時ディレクトリに bare リポジトリを作り、そこから clone して worktree を切る。
package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// ===== 偽のボードの状態（テスト用gh mock とテスト用GraphQL mockが共有する1枚の JSON） =====

// ghComment は issue に付いたコメント1件である。
type ghComment struct {
	// ID はコメントのノード ID である。
	ID string `json:"id"`
	// Body は本文である。
	Body string `json:"body"`
	// CreatedAt は作成時刻である（RFC3339Nano の文字列。プロセスをまたぐので文字列で持つ）。
	CreatedAt string `json:"created_at"`
	// Author は書き手のログイン名である。
	Author string `json:"author"`
}

// ghIssue はリポジトリの issue 1件である。
//
// **ボードに載っているかどうかもここに持つ。**`gh project item-add` で載せると OnBoard が
// 真になり、`gh project item-list` と GraphQL の候補の取得の両方に出るようになる。
type ghIssue struct {
	// NodeID は GitHub issue のノード ID である（コメントの投稿先）。
	NodeID string `json:"node_id"`
	// Number は issue 番号である。
	Number int `json:"number"`
	// Title は表題である。
	Title string `json:"title"`
	// Body は本文である。
	Body string `json:"body"`
	// Repo は `<owner>/<repo>` である。
	Repo string `json:"repo"`
	// URL は issue の URL である。
	URL string `json:"url"`
	// DefaultBranch はリポジトリの既定 branch 名である（worktree の base になる）。
	DefaultBranch string `json:"default_branch"`
	// OnBoard はボードに載っているかどうかである。
	OnBoard bool `json:"on_board"`
	// ItemID は project item の ID である（ボードに載っているときだけ埋まる）。
	ItemID string `json:"item_id"`
	// State は Status の値である（ボードに載っているときだけ埋まる）。
	State string `json:"state"`
	// CreatedAt は作成時刻である（RFC3339 の文字列）。
	CreatedAt string `json:"created_at"`
	// Assignees は担当者のログイン名である（設計 3-77b）。
	//
	// **ノード ID は `U_` + ログイン名で作る**（このテスト用GraphQL mock の中だけの決まりである）。
	Assignees []string `json:"assignees"`
}

// ghBoard は偽のボード1枚ぶんの状態である。
//
// **このファイルが唯一の正である。**テスト用gh mock（別プロセス）とテスト用GraphQL mock（テストの
// プロセス）が同じファイルを flock で排他しながら読み書きするので、
// `gh project item-add` で足した issue が次の GraphQL の取得に出るし、
// GraphQL で書き換えた Status が次の `gh project item-list` に出る。
type ghBoard struct {
	// Login は `gh api user --jq .login` が返すログイン名である。
	Login string `json:"login"`
	// Owner はボードの所有者名である。
	Owner string `json:"owner"`
	// ProjectNumber はボードの番号である。
	ProjectNumber int `json:"project_number"`
	// ProjectTitle はボードの表示名である。
	ProjectTitle string `json:"project_title"`
	// ProjectURL はボードの URL である。
	ProjectURL string `json:"project_url"`
	// ProjectID はボードのノード ID である。
	ProjectID string `json:"project_id"`
	// StatusField は Status を読み書きする single-select フィールドの名前である。
	StatusField string `json:"status_field"`
	// StatusOptions はそのフィールドの選択肢名である（並び順が画面の並び順になる）。
	StatusOptions []string `json:"status_options"`
	// Issues はリポジトリの issue の全件である（ボードに載っていないものも持つ）。
	Issues []*ghIssue `json:"issues"`
	// Comments は issue のノード ID から、そこに付いたコメントを引く写像である。
	Comments map[string][]ghComment `json:"comments"`
	// Calls はテスト用gh mock が受け取ったサブコマンドを受け取った順に並べたものである
	// （どの経路を実際に通ったかをテストが確かめるために使う）。
	Calls []string `json:"calls"`
	// NextNumber は次に採番する issue 番号である。
	NextNumber int `json:"next_number"`
	// NextItem は次に採番する project item の連番である。
	NextItem int `json:"next_item"`
	// NextComment は次に採番するコメントの連番である。
	NextComment int `json:"next_comment"`
}

// findIssueByURL は URL で issue を引く。
//
// url: 探す issue の URL。
// 戻り値: 見つかった issue。無ければ nil。
func (b *ghBoard) findIssueByURL(url string) *ghIssue {
	for _, is := range b.Issues {
		if is.URL == url {
			return is
		}
	}
	return nil
}

// findIssueByItemID は project item の ID で issue を引く。
//
// itemID: 探す project item の ID。
// 戻り値: 見つかった issue。無ければ nil。
func (b *ghBoard) findIssueByItemID(itemID string) *ghIssue {
	for _, is := range b.Issues {
		if is.OnBoard && is.ItemID == itemID {
			return is
		}
	}
	return nil
}

// onBoard はボードに載っている issue を、載せた順に返す。
func (b *ghBoard) onBoard() []*ghIssue {
	var out []*ghIssue
	for _, is := range b.Issues {
		if is.OnBoard {
			out = append(out, is)
		}
	}
	return out
}

// optionIndex は Status の選択肢名の位置を返す（見つからなければ -1）。
//
// name: 選択肢名。
// 戻り値: 位置。
func (b *ghBoard) optionIndex(name string) int {
	for i, o := range b.StatusOptions {
		if o == name {
			return i
		}
	}
	return -1
}

// ===== ボードのファイルの読み書き（プロセスをまたぐので flock で排他する） =====

// lockBoardFile はボードのファイルに排他ロックを掛ける。
//
// **テスト用gh mock は別プロセスである。**Go の Mutex では守れないので、ファイルロックを使う。
//
// path: ボードの JSON のパス（ロックは `<path>.lock` に取る）。
// 戻り値の1つ目: ロックを保持しているファイル（unlockBoardFile へ渡すこと）。
// 戻り値の2つ目: 開けない・ロックできない場合のエラー。
func lockBoardFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// unlockBoardFile は lockBoardFile が取ったロックを外す。
//
// f: lockBoardFile が返したファイル。
func unlockBoardFile(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	_ = f.Close()
}

// loadBoardFile はボードの JSON を読む。
//
// path: ボードの JSON のパス。
// 戻り値: 読み取ったボード。読めない場合はエラー。
func loadBoardFile(path string) (*ghBoard, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b ghBoard
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}
	if b.Comments == nil {
		b.Comments = map[string][]ghComment{}
	}
	return &b, nil
}

// saveBoardFile はボードの JSON を書く。
//
// **一時ファイルへ書いてから rename する。**読み手が途中の中身を見ないようにする。
//
// path: ボードの JSON のパス。
// b: 書き出すボード。
// 戻り値: 書けない場合のエラー。
func saveBoardFile(path string, b *ghBoard) error {
	encoded, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(encoded, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// withBoardFile はロックを取ってボードを読み、fn を適用してから書き戻す。
//
// path: ボードの JSON のパス。
// fn: ボードに適用する処理。**この中でファイルを読み書きしないこと**（二重ロックになる）。
// 戻り値: 読み書きに失敗した場合、または fn がエラーを返した場合のエラー。
func withBoardFile(path string, fn func(*ghBoard) error) error {
	lock, err := lockBoardFile(path)
	if err != nil {
		return err
	}
	defer unlockBoardFile(lock)

	b, err := loadBoardFile(path)
	if err != nil {
		return err
	}
	if err := fn(b); err != nil {
		return err
	}
	return saveBoardFile(path, b)
}

// ===== テストからボードを触る入口 =====

// board はテストの中からボードを読み書きするための持ち手である。
type board struct {
	// Path はボードの JSON の絶対パスである。
	Path string
}

// Read はいまのボードを読む。
//
// t: 呼び出し元のテスト。
// 戻り値: 読み取ったボード（写しである。書き換えても反映されない）。
func (bd *board) Read(t *testing.T) *ghBoard {
	t.Helper()
	lock, err := lockBoardFile(bd.Path)
	if err != nil {
		t.Fatalf("ボードのロックを取れません: %v", err)
	}
	defer unlockBoardFile(lock)
	b, err := loadBoardFile(bd.Path)
	if err != nil {
		t.Fatalf("ボードを読めません: %v", err)
	}
	return b
}

// Mutate はボードを書き換える。
//
// t: 呼び出し元のテスト。
// fn: 書き換える処理。
func (bd *board) Mutate(t *testing.T, fn func(*ghBoard)) {
	t.Helper()
	if err := withBoardFile(bd.Path, func(b *ghBoard) error {
		fn(b)
		return nil
	}); err != nil {
		t.Fatalf("ボードを書き換えられません: %v", err)
	}
}

// SetStateByURL は issue の Status を書き換える。
//
// **人間がボードの画面で Status を動かす操作の代わりである。**continuo は
// GraphQL 経由でしか書かないので、画面の操作はここでしか起こせない。
//
// t: 呼び出し元のテスト。
// url: 対象の issue の URL。
// state: 書き込む Status。
func (bd *board) SetStateByURL(t *testing.T, url, state string) {
	t.Helper()
	bd.Mutate(t, func(b *ghBoard) {
		is := b.findIssueByURL(url)
		if is == nil {
			t.Errorf("ボードに %s という issue がありません", url)
			return
		}
		is.State = state
	})
}

// StateOfURL は issue のいまの Status を返す。
//
// t: 呼び出し元のテスト。
// url: 対象の issue の URL。
// 戻り値: Status。ボードに無ければ空文字。
func (bd *board) StateOfURL(t *testing.T, url string) string {
	t.Helper()
	is := bd.Read(t).findIssueByURL(url)
	if is == nil {
		return ""
	}
	return is.State
}

// CommentBodies は issue に付いたコメントの本文を、付いた順に返す。
//
// t: 呼び出し元のテスト。
// nodeID: issue のノード ID。
// 戻り値: 本文の並び。
func (bd *board) CommentBodies(t *testing.T, nodeID string) []string {
	t.Helper()
	var out []string
	for _, c := range bd.Read(t).Comments[nodeID] {
		out = append(out, c.Body)
	}
	return out
}

// GHCalls はテスト用gh mock が受け取ったサブコマンドを受け取った順に返す。
//
// t: 呼び出し元のテスト。
// 戻り値: `project item-add` のような文字列の並び。
func (bd *board) GHCalls(t *testing.T) []string {
	t.Helper()
	return bd.Read(t).Calls
}

// newBoardFile は偽のボードを1枚作る。
//
// **既に issue が1件載っている状態で始める。**continuo は「いま使っているボードに
// 後から足して使う」ものなので、空のボードから始めると `continuo init` が
// `trust.repositories` を埋められず、手順書のとおりに進まない。
//
// t: 呼び出し元のテスト。
// path: 書き出すボードの JSON の絶対パス。
// owner: ボードの所有者名。
// repo: `<owner>/<repo>` の repo の部分。
// 戻り値: ボードの持ち手。
func newBoardFile(t *testing.T, path, owner, repo string) *board {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	b := &ghBoard{
		Login:         owner,
		Owner:         owner,
		ProjectNumber: 7,
		ProjectTitle:  "continuo 試用ボード",
		ProjectURL:    fmt.Sprintf("https://github.com/users/%s/projects/7", owner),
		ProjectID:     "PVT_board",
		StatusField:   "Status",
		// 手順書の段2 と同じ6つ（`Ice Box` は continuo が知らない選択肢の例である）。
		StatusOptions: []string{"Ice Box", "Ready", "In Progress", "Blocked", "In Review", "Done"},
		Issues: []*ghIssue{{
			NodeID:        "I_node1",
			Number:        1,
			Title:         "前から載っている issue",
			Body:          "この issue は continuo の対象ではない（Ice Box にある）。",
			Repo:          owner + "/" + repo,
			URL:           fmt.Sprintf("https://github.com/%s/%s/issues/1", owner, repo),
			DefaultBranch: "main",
			OnBoard:       true,
			ItemID:        "PVTI_item1",
			State:         "Ice Box",
			CreatedAt:     now,
		}},
		Comments:    map[string][]ghComment{},
		NextNumber:  2,
		NextItem:    2,
		NextComment: 1,
	}
	if err := saveBoardFile(path, b); err != nil {
		t.Fatalf("偽のボードを書けません: %v", err)
	}
	return &board{Path: path}
}

// ===== テスト用GitHub GraphQL mock サーバが返す payload の組み立て =====

// itemPayload は project item 1件の GraphQL 応答を組み立てる。
//
// **形は internal/tracker/query.go の itemFieldsFragment に合わせてある。**
// 足りない項目があると、その item は「壊れている」として候補から落ちる。
//
// b: いまのボード。
// is: 対象の issue。
// withTypename: `nodes(ids:)` 経由なら真（`__typename` が要る）。
// 戻り値: item 1件の応答。
func itemPayload(b *ghBoard, is *ghIssue, withTypename bool) map[string]any {
	var fieldValue any
	if is.State != "" {
		fieldValue = map[string]any{
			"__typename": "ProjectV2ItemFieldSingleSelectValue",
			"name":       is.State,
			"optionId":   fmt.Sprintf("opt%d", b.optionIndex(is.State)),
		}
	}
	payload := map[string]any{
		"id":               is.ItemID,
		"isArchived":       false,
		"fieldValueByName": fieldValue,
		"content": map[string]any{
			"__typename": "Issue",
			"id":         is.NodeID,
			"number":     is.Number,
			"title":      is.Title,
			"body":       is.Body,
			"url":        is.URL,
			"state":      "OPEN",
			"createdAt":  is.CreatedAt,
			"updatedAt":  is.CreatedAt,
			"repository": map[string]any{
				"nameWithOwner":    is.Repo,
				"defaultBranchRef": map[string]any{"name": is.DefaultBranch},
			},
			"labels":         map[string]any{"nodes": []any{}},
			"assignees":      assigneesPayload(is),
			"blockedBy":      map[string]any{"nodes": []any{}},
			"linkedBranches": map[string]any{"nodes": []any{}},
			"comments":       map[string]any{"totalCount": len(b.Comments[is.NodeID])},
		},
	}
	if withTypename {
		payload["__typename"] = "ProjectV2Item"
	}
	return payload
}

// assigneesPayload は担当者の connection を組み立てる（設計 3-77b）。
//
// is: 対象の issue。
// 戻り値: `totalCount` と `nodes` を持つ connection。
func assigneesPayload(is *ghIssue) map[string]any {
	nodes := make([]any, 0, len(is.Assignees))
	for _, login := range is.Assignees {
		nodes = append(nodes, map[string]any{"id": "U_" + login, "login": login})
	}
	return map[string]any{"totalCount": len(is.Assignees), "nodes": nodes}
}

// viewerPayload は「いまのトークンの持ち主」を返す（設計 3-77b）。
//
// b: いまのボード。
// 戻り値: 応答の data。
func viewerPayload(b *ghBoard) map[string]any {
	return map[string]any{"viewer": map[string]any{"id": "U_" + b.Login, "login": b.Login}}
}

// changeAssigneesPayload は担当者の書き足し／取り外しに答える（**ボードも書き換える**）。
//
// b: いまのボード。書き換える。
// vars: 受け取った変数。
// key: 応答のキー（`addAssigneesToAssignable` か `removeAssigneesFromAssignable`）。
// add: 書き足しなら真、取り外しなら偽。
// 戻り値: 応答の data。
func changeAssigneesPayload(b *ghBoard, vars map[string]any, key string, add bool) map[string]any {
	nodeID, _ := vars["assignableId"].(string)
	raw, _ := vars["assigneeIds"].([]any)
	logins := make([]string, 0, len(raw))
	for _, v := range raw {
		if id, ok := v.(string); ok {
			logins = append(logins, strings.TrimPrefix(id, "U_"))
		}
	}
	for _, is := range b.Issues {
		if is.NodeID != nodeID {
			continue
		}
		if add {
			for _, l := range logins {
				if !containsLogin(is.Assignees, l) {
					is.Assignees = append(is.Assignees, l)
				}
			}
		} else {
			kept := make([]string, 0, len(is.Assignees))
			for _, a := range is.Assignees {
				if !containsLogin(logins, a) {
					kept = append(kept, a)
				}
			}
			is.Assignees = kept
		}
		return map[string]any{key: map[string]any{
			"assignable": map[string]any{"id": is.NodeID, "assignees": assigneesPayload(is)},
		}}
	}
	return map[string]any{key: map[string]any{"assignable": nil}}
}

// containsLogin は一覧にログイン名があるかを返す。
//
// list: 探す先の一覧。
// login: 探すログイン名。
// 戻り値: あれば真。
func containsLogin(list []string, login string) bool {
	for _, l := range list {
		if l == login {
			return true
		}
	}
	return false
}

// fieldNotFoundErrors は実在しないフィールド名を指定されたときの GraphQL の errors を組み立てる。
//
// **本物の GitHub と同じ文面にしてある。**docs/trying_it_out.md の段2 と段6 が、
// `status_field` の綴りを間違えたときの `continuo doctor` の出力として載せている文面である。
//
// name: 指定された（実在しない）フィールド名。
// 戻り値: GraphQL の errors 配列。
func fieldNotFoundErrors(name string) []any {
	return []any{map[string]any{
		"type": "NOT_FOUND",
		"message": "Could not resolve to a Unions::ProjectV2FieldConfiguration with the name " +
			name,
	}}
}

// bootstrapPayload は起動時の検査（Bootstrap）のクエリへの応答を組み立てる。
//
// **絞り込みキーの検査の3つの件数も返す**（internal/tracker の judgeFilterKeyUsable）。
// 全件・値が入っている件数・値が空の件数を正しく返さないと、
// 「status_field を絞り込みのキーとして使えていない」と判定されて起動が止まる。
//
// b: いまのボード。
// 戻り値: 応答の data。
func bootstrapPayload(b *ghBoard) map[string]any {
	options := make([]any, 0, len(b.StatusOptions))
	for i, name := range b.StatusOptions {
		options = append(options, map[string]any{"id": fmt.Sprintf("opt%d", i), "name": name})
	}
	items := b.onBoard()
	withStatus := 0
	for _, is := range items {
		if is.State != "" {
			withStatus++
		}
	}
	return map[string]any{
		"repositoryOwner": map[string]any{
			"projectV2": map[string]any{
				"id": b.ProjectID,
				"field": map[string]any{
					"__typename": "ProjectV2SingleSelectField",
					"id":         "PVTSSF_status",
					"options":    options,
				},
				"totalItems":         map[string]any{"totalCount": len(items)},
				"itemsWithStatus":    map[string]any{"totalCount": withStatus},
				"itemsWithoutStatus": map[string]any{"totalCount": len(items) - withStatus},
				// **自動化は1件も無いボードにする**（`continuo doctor` の見出し語 `自動化`）。
				// **`workflows` ごと落とすと「読めなかった」になり、`!` が出る。**
				"workflows": map[string]any{"nodes": []any{}},
			},
		},
	}
}

// itemsByQuery は候補の取得のクエリへの応答を組み立てる。
//
// q: `items(query:)` に渡された検索クエリ（`"Status":"Ready","In Progress"` の形）。
// b: いまのボード。
// 戻り値: 応答の data。
func itemsByQuery(b *ghBoard, q string) map[string]any {
	nodes := []any{}
	for _, is := range b.onBoard() {
		if is.State == "" || !strings.Contains(q, `"`+is.State+`"`) {
			continue
		}
		nodes = append(nodes, itemPayload(b, is, false))
	}
	return map[string]any{
		"repositoryOwner": map[string]any{
			"projectV2": map[string]any{
				"items": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil},
					"nodes":    nodes,
				},
			},
		},
	}
}

// itemsByIDs は ID 指定の取り直しのクエリへの応答を組み立てる。
//
// **見つからない ID には null を返す**（本物と同じ。「もう見えない」を表す）。
//
// b: いまのボード。
// vars: 受け取った変数。
// 戻り値: 応答の data。
func itemsByIDs(b *ghBoard, vars map[string]any) map[string]any {
	raw, _ := vars["ids"].([]any)
	nodes := make([]any, 0, len(raw))
	for _, v := range raw {
		id, _ := v.(string)
		is := b.findIssueByItemID(id)
		if is == nil {
			nodes = append(nodes, nil)
			continue
		}
		nodes = append(nodes, itemPayload(b, is, true))
	}
	return map[string]any{"nodes": nodes}
}

// updateStatusPayload は Status の書き込みに答える（**ボードの中身も書き換える**）。
//
// b: いまのボード。書き換える。
// vars: 受け取った変数。
// 戻り値: 応答の data。
func updateStatusPayload(b *ghBoard, vars map[string]any) map[string]any {
	itemID, _ := vars["itemId"].(string)
	optionID, _ := vars["optionId"].(string)
	for i, name := range b.StatusOptions {
		if optionID != fmt.Sprintf("opt%d", i) {
			continue
		}
		if is := b.findIssueByItemID(itemID); is != nil {
			is.State = name
		}
	}
	return map[string]any{
		"updateProjectV2ItemFieldValue": map[string]any{
			"projectV2Item": map[string]any{"id": itemID},
		},
	}
}

// commentsPayload はコメントの取得に答える（**新しい順で返す。**本物と同じ）。
//
// b: いまのボード。
// vars: 受け取った変数。
// 戻り値: 応答の data。
func commentsPayload(b *ghBoard, vars map[string]any) map[string]any {
	nodeID, _ := vars["issueId"].(string)
	list := b.Comments[nodeID]
	nodes := make([]any, 0, len(list))
	for i := len(list) - 1; i >= 0; i-- {
		nodes = append(nodes, map[string]any{
			"id":        list[i].ID,
			"url":       "https://github.com/comment/" + list[i].ID,
			"body":      list[i].Body,
			"createdAt": list[i].CreatedAt,
			"author":    map[string]any{"login": list[i].Author, "id": "U_" + list[i].Author},
		})
	}
	return map[string]any{"node": map[string]any{"__typename": "Issue",
		"comments": map[string]any{"nodes": nodes}}}
}

// addCommentPayload はコメントの投稿に答える（**ボードにも積む**）。
//
// b: いまのボード。書き換える。
// vars: 受け取った変数。
// 戻り値: 応答の data。
func addCommentPayload(b *ghBoard, vars map[string]any) map[string]any {
	nodeID, _ := vars["subjectId"].(string)
	body, _ := vars["body"].(string)
	id := fmt.Sprintf("IC_%d", b.NextComment)
	b.NextComment++
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// **投稿者は gh の持ち主である。**continuo は `gh auth token` で取ったトークンで
	// GraphQL を叩くので、書いたコメントは `gh api user --jq .login` が返すのと同じ
	// アカウントで残る。**別の名前にすると、self_marker の付いたコメントが次の turn の
	// 入力から外れなくなる**（設計 3-65。投稿者と印を併せて見るため）。
	b.Comments[nodeID] = append(b.Comments[nodeID], ghComment{
		ID: id, Body: body, CreatedAt: now, Author: b.Login,
	})
	return map[string]any{
		"addComment": map[string]any{
			"commentEdge": map[string]any{
				"node": map[string]any{
					"id": id, "url": "https://github.com/comment/" + id, "body": body,
					"createdAt": now,
					"author":    map[string]any{"login": b.Login, "id": "U_" + b.Login},
				},
			},
		},
	}
}

// ===== 受け取ったクエリの記録 =====

// queryLog はテスト用GraphQL mockが受け取ったクエリの種別を記録する。
type queryLog struct {
	mu   sync.Mutex
	list []string
}

// note は1件積む。
//
// kind: クエリの種別。
func (q *queryLog) note(kind string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.list = append(q.list, kind)
}

// Entries は積んだ種別を積んだ順に返す。
func (q *queryLog) Entries() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]string, len(q.list))
	copy(out, q.list)
	return out
}

// Count は種別が現れた回数を返す。
//
// kind: 数える種別。
// 戻り値: 回数。
func (q *queryLog) Count(kind string) int {
	n := 0
	for _, e := range q.Entries() {
		if e == kind {
			n++
		}
	}
	return n
}

// boardPathIn は一時ディレクトリの中のボードの JSON のパスを返す。
//
// root: 一時ディレクトリの根。
// 戻り値: ボードの JSON の絶対パス。
func boardPathIn(root string) string {
	return filepath.Join(root, "board.json")
}
