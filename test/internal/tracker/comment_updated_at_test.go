package tracker_test

import (
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
)

// 目的: コメントを取る GraphQL が `updatedAt` を要求し、返ってきた値が `Comment.UpdatedAt` に入ることを
// 固定する（#194（進捗コメントの間隔と重ね方が、人間の決定と逆に実装されている）。設計 5-3k）。
//
// **なぜ要るか。**エージェントは進捗の報告を新しいコメントにせず、
// **いちばん下にある自分の進捗報告へ書き足す**（設計 5-3j）。
// **本文を編集しても GitHub は `createdAt` を動かさない**（2026-09-03 に実測）。
// **`updatedAt` を要求していないと、1時間おきに書き続けている機械の持ち回りの期限が1秒も進まず、
// 18時間で担当が外れて別の機械が同じ issue を最初からやり直す。**
//
// **クエリの文字列も見る。**構造体に欄があっても、要求していなければ応答に入らない。
// **偽サーバは何を訊かれても答えるので、値の照合だけでは素通りする。**
//
// 与える情報: `createdAt` より新しい `updatedAt` を持つコメント1件を返す偽サーバ。
// 成功条件: 送ったクエリに `updatedAt` が入っていること。`Comment.UpdatedAt` がその値になっていること。
func TestFetchComments_更新時刻を要求して持ち帰る(t *testing.T) {
	cfg := testTrackerConfig()

	const created = "2026-09-03T05:40:34Z"
	const updated = "2026-09-03T05:40:56Z"

	fs := newFakeGraphQLServer(t, single(dataResponse(map[string]any{
		"node": map[string]any{
			"__typename": "Issue",
			"comments": map[string]any{"nodes": []map[string]any{{
				"id": "c1", "url": "https://example.com/c1",
				"body":      cfg.Comments.Marker + "\n<!-- continuo:progress -->\nまだ作業中です。",
				"createdAt": created, "updatedAt": updated,
				"author": map[string]any{"login": "octocat"},
			}}},
		},
	})))
	a := newAdapterForFetch(t, fs)

	comments, err := a.FetchComments(t.Context(), "ISSUENODE_1", cfg.Provider.Comments, cfg.Comments, "")
	if err != nil {
		t.Fatalf("FetchComments が失敗した: %v", err)
	}

	reqs := fs.Requests()
	if len(reqs) != 1 {
		t.Fatalf("リクエストが %d 件（1件送られるはず）", len(reqs))
	}
	if !strings.Contains(reqs[0].Query, "updatedAt") {
		t.Errorf("コメントを取るクエリが updatedAt を要求していない。"+
			"要求しないと、進捗の報告を書き足している機械の持ち回りの期限が進まない:\n%s", reqs[0].Query)
	}

	if len(comments) != 1 {
		t.Fatalf("コメントの件数が違う: got %d, want 1", len(comments))
	}
	want, err := time.Parse(time.RFC3339, updated)
	if err != nil {
		t.Fatalf("テストの時刻を解釈できない: %v", err)
	}
	if !comments[0].UpdatedAt.Equal(want) {
		t.Errorf("更新時刻が持ち帰られていない: got %v, want %v", comments[0].UpdatedAt, want)
	}
}

// 目的: `FetchAllComments`（持ち回りの判定が読む経路）も更新時刻を持ち帰ることを固定する（設計 5-3k）。
//
// **持ち回りの期限を数えるのはこちらである。**`FetchComments` は
// エージェントへ渡す入力を作る経路で、**印の付いたコメントを外す**ので判定には使えない。
// **こちらが更新時刻を落とすと、期限は作成時刻だけで数えられる。**
//
// 与える情報: `createdAt` より新しい `updatedAt` を持つコメント1件を返す偽サーバ。
// 成功条件: `Comment.UpdatedAt` がその値になっていること。
func TestFetchAllComments_更新時刻を持ち帰る(t *testing.T) {
	const created = "2026-09-03T05:40:34Z"
	const updated = "2026-09-03T05:40:56Z"

	fs := newFakeGraphQLServer(t, single(dataResponse(map[string]any{
		"node": map[string]any{
			"__typename": "Issue",
			"comments": map[string]any{"nodes": []map[string]any{{
				"id": "c1", "url": "https://example.com/c1", "body": "まだ作業中です。",
				"createdAt": created, "updatedAt": updated,
				"author": map[string]any{"login": "octocat"},
			}}},
		},
	})))
	a := newAdapterForFetch(t, fs)

	comments, _, err := a.FetchAllComments(t.Context(), "ISSUENODE_1", config.TrackerProviderCommentsConfig{})
	if err != nil {
		t.Fatalf("FetchAllComments が失敗した: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("コメントの件数が違う: got %d, want 1", len(comments))
	}
	want, err := time.Parse(time.RFC3339, updated)
	if err != nil {
		t.Fatalf("テストの時刻を解釈できない: %v", err)
	}
	if !comments[0].UpdatedAt.Equal(want) {
		t.Errorf("更新時刻が持ち帰られていない: got %v, want %v", comments[0].UpdatedAt, want)
	}
}

// 目的: 応答に `updatedAt` が無いとき、作成時刻で埋め戻さずゼロ値のままにすることを固定する（設計 5-3k）。
//
// **「編集されていない」と「取れなかった」は別の状態である。**
// **埋め戻すと、応答からフィールドが落ちたことに誰も気づけない。**
// 新しいほうを採るのは判定する側（`handoff.CommentView.LastTouched`）の仕事である。
//
// 与える情報: `updatedAt` を返さない偽サーバ。
// 成功条件: `Comment.UpdatedAt` がゼロ値で、`Comment.CreatedAt` は埋まっていること。
func TestFetchComments_更新時刻が無い応答では作成時刻で埋め戻さない(t *testing.T) {
	cfg := testTrackerConfig()

	fs := newFakeGraphQLServer(t, single(dataResponse(map[string]any{
		"node": map[string]any{
			"__typename": "Issue",
			"comments": map[string]any{"nodes": []map[string]any{{
				"id": "c1", "url": "https://example.com/c1", "body": "人間が書いたコメント",
				"createdAt": "2026-09-03T05:40:34Z",
				"author":    map[string]any{"login": "octocat"},
			}}},
		},
	})))
	a := newAdapterForFetch(t, fs)

	comments, err := a.FetchComments(t.Context(), "ISSUENODE_1", cfg.Provider.Comments, cfg.Comments, "")
	if err != nil {
		t.Fatalf("FetchComments が失敗した: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("コメントの件数が違う: got %d, want 1", len(comments))
	}
	if !comments[0].UpdatedAt.IsZero() {
		t.Errorf("更新時刻が無いのに埋まっている: got %v（ゼロ値のはず）", comments[0].UpdatedAt)
	}
	if comments[0].CreatedAt.IsZero() {
		t.Error("作成時刻まで空になっている")
	}
}
