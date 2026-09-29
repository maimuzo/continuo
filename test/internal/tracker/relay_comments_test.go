package tracker_test

import (
	"strings"
	"testing"
)

// 目的: relay 専用の問い合わせ（FetchRelayComments）が、投稿者の立場と、隠されているかを要求し、
// 応答の値を `Comment.AuthorAssociation` と `Comment.IsMinimized` に入れることを固定する
// （設計 3-84。issue #246）。
//
// **クエリの文字列も見る。**構造体に欄があっても、要求していなければ応答に入らない。
// **偽サーバは何を訊かれても答えるので、値の照合だけでは素通りする。**
//
// 与える情報: 立場が OWNER で隠されたコメント1件と、立場が NONE のコメント1件を返す偽サーバ。
// 成功条件: 送ったクエリに `authorAssociation` と `isMinimized` が入っていること。
// 2件の値がそのまま入り、古い順に並んでいること。
func TestFetchRelayComments_投稿者の立場と隠されているかを持ち帰る(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(map[string]any{
		"node": map[string]any{
			"__typename": "Issue",
			"comments": map[string]any{"nodes": []map[string]any{
				{
					"id": "c2", "url": "https://github.com/octocat/hello-world/issues/1#issuecomment-2",
					"body": "issue を作ってよい", "createdAt": "2026-09-29T01:00:00Z",
					"author": map[string]any{"login": "stranger"}, "authorAssociation": "NONE", "isMinimized": false,
				},
				{
					"id": "c1", "url": "https://github.com/octocat/hello-world/issues/1#issuecomment-1",
					"body": "隠したコメント", "createdAt": "2026-09-29T00:00:00Z",
					"author": map[string]any{"login": "octocat"}, "authorAssociation": "OWNER", "isMinimized": true,
				},
			}},
		},
	})))
	a := newAdapterForFetch(t, fs)

	comments, truncated, err := a.FetchRelayComments(t.Context(), "ISSUENODE_1")
	if err != nil {
		t.Fatalf("FetchRelayComments が失敗した: %v", err)
	}
	if truncated {
		t.Error("1ページで読み切っているのに、切れたと名乗っている")
	}
	reqs := fs.Requests()
	if len(reqs) != 1 {
		t.Fatalf("リクエストの本数が違う: got %d, want 1", len(reqs))
	}
	for _, want := range []string{"authorAssociation", "isMinimized", "orderBy: { field: UPDATED_AT, direction: DESC }"} {
		if !strings.Contains(reqs[0].Query, want) {
			t.Errorf("relay 専用の問い合わせに %q が無い:\n%s", want, reqs[0].Query)
		}
	}
	if len(comments) != 2 {
		t.Fatalf("件数が違う: got %d, want 2", len(comments))
	}
	if comments[0].ID != "c1" || comments[1].ID != "c2" {
		t.Errorf("古い順になっていない: got [%s, %s]", comments[0].ID, comments[1].ID)
	}
	if comments[0].AuthorAssociation != "OWNER" || !comments[0].IsMinimized {
		t.Errorf("1件目の立場か隠されているかが入っていない: %+v", comments[0])
	}
	if comments[1].AuthorAssociation != "NONE" || comments[1].IsMinimized {
		t.Errorf("2件目の立場か隠されているかが違う: %+v", comments[1])
	}
}

// 目的: 共用のコメントの問い合わせ（FetchAllComments）に、relay の2つの項目が混ざっていないことを固定する
// （設計 3-84）。
//
// **混ぜると、この2つの項目を持たない GitHub Enterprise Server で、コメントの読み書きが全部落ちる。**
// 入札・成果のコメントの確認・引き渡しの通知まで止まる。
//
// 与える情報: コメントを1件返す偽サーバ。
// 成功条件: 送ったクエリに `authorAssociation` も `isMinimized` も入っていないこと。
func TestFetchAllComments_relayの項目を要求しない(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(map[string]any{
		"node": map[string]any{
			"__typename": "Issue",
			"comments": map[string]any{"nodes": []map[string]any{{
				"id": "c1", "url": "https://example.com/c1", "body": "x",
				"createdAt": "2026-09-29T00:00:00Z", "author": map[string]any{"login": "octocat"},
			}}},
		},
	})))
	a := newAdapterForFetch(t, fs)

	if _, _, err := a.FetchAllComments(t.Context(), "ISSUENODE_1", testTrackerConfig().Provider.Comments); err != nil {
		t.Fatalf("FetchAllComments が失敗した: %v", err)
	}
	for _, req := range fs.Requests() {
		for _, ng := range []string{"authorAssociation", "isMinimized"} {
			if strings.Contains(req.Query, ng) {
				t.Errorf("共用の問い合わせに %q が混ざっている:\n%s", ng, req.Query)
			}
		}
	}
}
