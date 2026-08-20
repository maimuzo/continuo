package e2e_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeGitHub は GitHub の GraphQL API の代わりに使う偽のサーバである。
//
// **本番のボード（project #3）へは1リクエストも送らない。**continuo は環境変数
// `CONTINUO_GITHUB_GRAPHQL_ENDPOINT` でここへ向く。
//
// **状態は偽の gh と共有する。**返す中身も書き込む先も board.json であり、
// `gh project item-add` で足した issue はここから見えるし、
// ここで書き換えた Status は次の `gh project item-list` に出る。
type fakeGitHub struct {
	// URL は偽サーバのエンドポイントである。
	URL string
	// BoardPath は読み書きするボードの JSON の絶対パスである。
	BoardPath string
	// Queries は受け取ったクエリの種別の記録である。
	Queries *queryLog
}

// newFakeGitHub は偽の GraphQL サーバを1本立てる。
//
// t: 呼び出し元のテスト。後始末を t.Cleanup に登録する。
// boardPath: 読み書きするボードの JSON の絶対パス。
// 戻り値: 起動した偽サーバ。
func newFakeGitHub(t *testing.T, boardPath string) *fakeGitHub {
	t.Helper()
	fg := &fakeGitHub{BoardPath: boardPath, Queries: &queryLog{}}
	srv := httptest.NewServer(http.HandlerFunc(fg.handle))
	t.Cleanup(srv.Close)
	fg.URL = srv.URL
	return fg
}

// handle は1件の GraphQL リクエストに答える。
//
// **応答の組み立てはボードのロックを取ったまま行う。**偽の gh（別プロセス）が
// 同時に書き換えていても、途中の状態を返さないようにするためである。
//
// w: 応答の書き出し先。
// r: 受け取ったリクエスト。
func (fg *fakeGitHub) handle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var (
		kind    string
		data    map[string]any
		gqlErrs []any
	)
	err := withBoardFile(fg.BoardPath, func(b *ghBoard) error {
		kind, data, gqlErrs = respond(b, req.Query, req.Variables)
		return nil
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"fake board unavailable"}`))
		return
	}
	fg.Queries.note(kind)

	w.Header().Set("Content-Type", "application/json")
	// **errors があるときは data を返さない。**本物の GitHub もそう返す。
	if len(gqlErrs) > 0 {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": nil, "errors": gqlErrs})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

// respond はクエリの種別を判別して応答の data を組み立てる。
//
// **判別はクエリ本文の断片で行う**（internal/tracker/query.go の各テンプレートに対応する）。
//
// b: いまのボード。書き込み系のクエリではここを書き換える。
// query: 受け取った GraphQL のクエリ本文。
// vars: 受け取った変数。
// 戻り値の1つ目: クエリの種別（記録に使う名前）。
// 戻り値の2つ目: 応答の data。
// 戻り値の3つ目: 応答の errors（空なら data を返す）。
func respond(b *ghBoard, query string, vars map[string]any) (string, map[string]any, []any) {
	switch {
	case strings.Contains(query, "field(name: $statusField)"):
		// **綴りが違うフィールド名には NOT_FOUND を返す。**本物の GitHub と同じ挙動であり、
		// docs/trying_it_out.md の段2 と段6 が載せている `✗ ボード` の出力はこれで出る。
		if name, ok := vars["statusField"].(string); ok && name != b.StatusField {
			return "bootstrap", nil, fieldNotFoundErrors(name)
		}
		return "bootstrap", bootstrapPayload(b), nil
	case strings.Contains(query, "nodes(ids: $ids)"):
		return "by_ids", itemsByIDs(b, vars), nil
	case strings.Contains(query, "items(first: 100"):
		q, _ := vars["q"].(string)
		return "items", itemsByQuery(b, q), nil
	case strings.Contains(query, "updateProjectV2ItemFieldValue"):
		return "update_status", updateStatusPayload(b, vars), nil
	case strings.Contains(query, "comments(first: $first"):
		return "comments", commentsPayload(b, vars), nil
	case strings.Contains(query, "addComment"):
		return "add_comment", addCommentPayload(b, vars), nil
	default:
		return "unknown", map[string]any{}, nil
	}
}
