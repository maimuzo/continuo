package tracker_test

import (
	"testing"

	"github.com/maimuzo/continuo/internal/tracker"
)

// 目的: 見つからない ID（nodes(ids:) が null を返す item）は、エラーにせず単に結果から
// 省くことを確認する（設計「その3」: 「見つからない ID は『もう見えない』として扱う。
// 合成した状態を作らない」）。
// 与える情報: 1件は正常な item、もう1件は null（削除・archive 等でもう見えない）で返る
// 偽サーバ応答。
// 成功条件: エラーが無く、結果が1件だけ（見つかった方だけ）であること。
func TestFetchIssuesByIDs_見つからないIDは省く(t *testing.T) {
	found := asProjectV2ItemNode(issueItemJSON(testIssueItemOpts{
		ItemID: "item-found", Status: "In Progress", Owner: "octocat", Repo: "hello-world", Number: 10, Title: "見つかる方",
	}))
	fs := newFakeGraphQLServer(t, single(dataResponse(byIDsPayload([]any{found, nil}))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByIDs(t.Context(), []string{"item-found", "item-gone"})
	if err != nil {
		t.Fatalf("見つからない ID があるだけでエラーになった: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("件数が想定と違う: got %d, want 1", len(issues))
	}
	if issues[0].ID != "item-found" {
		t.Fatalf("残った item が想定と違う: %q", issues[0].ID)
	}
}

// 目的: 見つかったのに正規化できない item（Status 未設定）は、一覧取得と違ってエラーに
// することを確認する（SPEC.md 11.1: "An ID-refresh call MUST fail instead of silently
// omitting a malformed requested record, because omission is meaningful."）。
// 与える情報: fieldValueByName が無い（Status 未設定の）item。
// 成功条件: FetchIssuesByIDs がエラーを返すこと。
func TestFetchIssuesByIDs_Status未設定はエラーになる(t *testing.T) {
	noStatus := asProjectV2ItemNode(issueItemJSON(testIssueItemOpts{
		ItemID: "item-nostatus", Status: "", Owner: "octocat", Repo: "hello-world", Number: 4, Title: "Status 未設定",
	}))
	fs := newFakeGraphQLServer(t, single(dataResponse(byIDsPayload([]any{noStatus}))))
	a := newAdapterForFetch(t, fs)

	_, err := a.FetchIssuesByIDs(t.Context(), []string{"item-nostatus"})
	if err == nil {
		t.Fatalf("Status 未設定の item を ID 指定で取り直したのにエラーにならなかった")
	}
	if !tracker.IsCategory(err, tracker.CategoryResponse) {
		t.Fatalf("エラーのカテゴリが CategoryResponse ではない: %v", err)
	}
}

// 目的: ids が空のときは GraphQL へリクエストを送らずに空の結果を返すことを確認する
// （SPEC.md 11.1: "An empty issue_ids list MUST return an empty result without a provider
// request."）。
// 与える情報: 空のスライスで FetchIssuesByIDs を呼ぶ。
// 成功条件: エラーが無く、結果が空であり、偽サーバへのリクエストが0件であること。
func TestFetchIssuesByIDs_空のidsはリクエストを送らない(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(byIDsPayload(nil))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByIDs(t.Context(), nil)
	if err != nil {
		t.Fatalf("空の ids でエラーになった: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("空の ids なのに結果が返った: %v", issues)
	}
	if fs.RequestCount() != 0 {
		t.Fatalf("空の ids なのにリクエストが送られた: %d件", fs.RequestCount())
	}
}

// 目的: draft issue も ID 指定の取り直しで正しく正規化できる（malformed 扱いにならない）
// ことを確認する。
// 与える情報: content が DraftIssue の item。
// 成功条件: エラーが無く、Dispatchable が false の Issue が1件返ること。
func TestFetchIssuesByIDs_draftIssueも取り直せる(t *testing.T) {
	draft := asProjectV2ItemNode(draftItemJSON("item-draft", "Ice Box", "下書き", "本文"))
	fs := newFakeGraphQLServer(t, single(dataResponse(byIDsPayload([]any{draft}))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByIDs(t.Context(), []string{"item-draft"})
	if err != nil {
		t.Fatalf("draft issue の取り直しでエラーになった: %v", err)
	}
	if len(issues) != 1 || issues[0].Dispatchable {
		t.Fatalf("draft issue の取り直し結果が想定と違う: %+v", issues)
	}
}

// TestFetchIssuesByIDs_NOT_FOUNDが混ざっても残りを読める は、部分的な成功の扱いを確かめる。
//
// **GitHub の GraphQL は `nodes(ids:)` に消えた ID が混ざると、
// `data.nodes` にその位置だけ `null` を入れたうえで、`errors` にも `NOT_FOUND` を返す。**
// **これは部分的な成功である。**
//
// **`errors` があるからと `data` を捨てると、生き残っている issue まで読めなくなる。**
// 実運用で、ボードごと消したあとに毎巡回でこのエラーが出続けた（2026-08-21。設計 6-2）。
//
// 目的: `NOT_FOUND` だけが返ったとき、`data` を使って残りを返すこと。
// 与える情報: 1件が正常・1件が null の `data` と、`NOT_FOUND` の `errors` を同時に返す偽サーバ。
// 成功条件: エラーにならず、見つかった1件だけが返ること。
func TestFetchIssuesByIDs_NOT_FOUNDが混ざっても残りを読める(t *testing.T) {
	found := asProjectV2ItemNode(issueItemJSON(testIssueItemOpts{
		ItemID: "item-found", Status: "In Progress", Owner: "octocat", Repo: "hello-world", Number: 10, Title: "生き残った方",
	}))
	fs := newFakeGraphQLServer(t, single(fakeGraphQLResponse{Body: map[string]any{
		"data": byIDsPayload([]any{found, nil}),
		"errors": []any{map[string]any{
			"type":    "NOT_FOUND",
			"message": "Could not resolve to a node with the global id of 'item-gone'.",
		}},
	}}))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByIDs(t.Context(), []string{"item-found", "item-gone"})
	if err != nil {
		t.Fatalf("NOT_FOUND が混ざっただけでエラーになった: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("件数が想定と違う: got %d, want 1", len(issues))
	}
	if issues[0].ID != "item-found" {
		t.Fatalf("残った item が想定と違う: %q", issues[0].ID)
	}
}

// TestFetchIssuesByIDs_NOT_FOUND以外が混ざればエラーにする は、握りつぶさないことを確かめる。
//
// **部分的な成功として扱ってよいのは、「消えた ID があった」だけのときに限る。**
// 権限不足や構文の誤りまで握りつぶすと、**設定の誤りに永久に気づけない。**
//
// 目的: `NOT_FOUND` 以外のエラーが1件でも混ざれば、エラーとして返すこと。
// 与える情報: `NOT_FOUND` と `FORBIDDEN` を同時に返す偽サーバ。
// 成功条件: エラーが返ること。
func TestFetchIssuesByIDs_NOT_FOUND以外が混ざればエラーにする(t *testing.T) {
	found := asProjectV2ItemNode(issueItemJSON(testIssueItemOpts{
		ItemID: "item-found", Status: "In Progress", Owner: "octocat", Repo: "hello-world", Number: 10, Title: "見つかる方",
	}))
	fs := newFakeGraphQLServer(t, single(fakeGraphQLResponse{Body: map[string]any{
		"data": byIDsPayload([]any{found, nil}),
		"errors": []any{
			map[string]any{"type": "NOT_FOUND", "message": "消えた ID があります"},
			map[string]any{"type": "FORBIDDEN", "message": "このボードを読む権限がありません"},
		},
	}}))
	a := newAdapterForFetch(t, fs)

	if _, err := a.FetchIssuesByIDs(t.Context(), []string{"item-found", "item-gone"}); err == nil {
		t.Fatal("FORBIDDEN が混ざっているのにエラーにならなかった")
	}
}
