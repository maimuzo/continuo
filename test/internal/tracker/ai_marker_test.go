package tracker_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// 目的: PostComment が「人間ではなく機械が書いた」の印を、
// 先頭の印の後ろへ足すことを固定する（設計 3-82。issue #245）。
//
// **なぜ要るか。**エージェントも continuo も人間も、同じ GitHub アカウントで投稿する。
// **投稿者でも `author_association` でも見分けられない。**
// **先頭へ割り込ませてはならない。**`FetchComments` は本文の先頭が `self_marker` かどうかで
// **次の turn の入力から外すかを決めている。**外れると、continuo 自身の通知が
// エージェントへの入力に混ざる。
//
// 与える情報: 素の本文と self_marker。
// 成功条件: 投稿した本文が `self_marker` → 機械の印 → 素の本文 の順であること。
func TestPostComment_機械の印をselfMarkerの次の行に足す(t *testing.T) {
	const selfMarker = "<!-- continuo:self -->"
	const rawBody = "作業内容の要約"
	want := selfMarker + "\n" + config.AIMarker + "\n" + rawBody

	var got string
	fs := newFakeGraphQLServer(t, func(_ int, req capturedRequest) fakeGraphQLResponse {
		got, _ = req.Variables["body"].(string)
		return dataResponse(map[string]any{
			"addComment": map[string]any{
				"commentEdge": map[string]any{
					"node": map[string]any{
						"id": "c-new", "url": "https://example.com/c-new", "body": got,
						"createdAt": "2026-09-06T00:00:00Z",
						"author":    map[string]any{"login": "continuo-bot"},
					},
				},
			},
		})
	})
	a := newAdapterForFetch(t, fs)

	if _, err := a.PostComment(t.Context(), "ISSUENODE_1", rawBody, selfMarker); err != nil {
		t.Fatalf("PostComment が失敗した: %v", err)
	}
	if got != want {
		t.Fatalf("投稿した本文が想定と違う:\n got %q\nwant %q", got, want)
	}
	if !strings.HasPrefix(got, selfMarker) {
		t.Errorf("self_marker が本文の先頭から外れています。"+
			"FetchComments が continuo 自身のコメントを外せなくなります: %q", got)
	}
}

// 目的: 本文が自分で印を持つコメント（持ち回りの入札・hold・released）でも、
// 先頭の印を動かさずに機械の印を足すことを固定する（設計 3-82）。
//
// **持ち回りのコメントは `selfMarker` を空で渡してくる**
// （internal/orchestrator の `postOwnMarkedComment`）。
// **本文の先頭が `<!-- continuo:bid -->` などでなくなると、別の機械がその入札を読めなくなる。**
//
// 与える情報: 先頭に持ち回りの印を持つ本文と、空の self_marker。
// 成功条件: 投稿した本文が 持ち回りの印 → 機械の印 → JSON の順であること。
func TestPostComment_持ち回りの印は先頭のまま動かさない(t *testing.T) {
	body := config.HandoffBidMarker + "\n{\"score\":190}\n\n立候補しています。\n"
	want := config.HandoffBidMarker + "\n" + config.AIMarker + "\n{\"score\":190}\n\n立候補しています。\n"

	var got string
	fs := newFakeGraphQLServer(t, func(_ int, req capturedRequest) fakeGraphQLResponse {
		got, _ = req.Variables["body"].(string)
		return dataResponse(map[string]any{
			"addComment": map[string]any{
				"commentEdge": map[string]any{
					"node": map[string]any{
						"id": "c-new", "url": "https://example.com/c-new", "body": got,
						"createdAt": "2026-09-06T00:00:00Z",
						"author":    map[string]any{"login": "continuo-bot"},
					},
				},
			},
		})
	})
	a := newAdapterForFetch(t, fs)

	if _, err := a.PostComment(t.Context(), "ISSUENODE_1", body, ""); err != nil {
		t.Fatalf("PostComment が失敗した: %v", err)
	}
	if got != want {
		t.Fatalf("投稿した本文が想定と違う:\n got %q\nwant %q", got, want)
	}
}

// 目的: 機械の印を足しても、エージェントが書いたコメントの判別が変わらないことを固定する
// （設計 3-82 / 3-65）。
//
// **`FetchComments` は本文の先頭が `marker` かどうかで `IsAgent` を決めている。**
// **印がその前に入ると、成果を書いた run が「書いていない」と判定され、人間へ渡る。**
//
// 与える情報: 印を足したエージェントのコメントと、印を足した continuo 自身のコメント。
// 成功条件: 前者が IsAgent=true で残り、後者が結果から外れること。
func TestFetchComments_機械の印を足しても判別が変わらない(t *testing.T) {
	cfg := testTrackerConfig()
	markers := cfg.Comments

	agentBody := config.WithAIMarker(markers.Marker + "\n実装しました")
	selfBody := config.WithAIMarker(markers.SelfMarker + "\nStatus を動かしました")

	agent := map[string]any{
		"id": "c1", "url": "https://example.com/c1", "body": agentBody,
		"createdAt": "2026-09-06T00:00:01Z", "author": map[string]any{"login": "human-user"},
	}
	self := map[string]any{
		"id": "c2", "url": "https://example.com/c2", "body": selfBody,
		"createdAt": "2026-09-06T00:00:02Z", "author": map[string]any{"login": "human-user"},
	}

	fs := newFakeGraphQLServer(t, single(dataResponse(map[string]any{
		"node": map[string]any{
			"__typename": "Issue",
			"comments":   map[string]any{"nodes": []map[string]any{self, agent}},
		},
	})))
	a := newAdapterForFetch(t, fs)

	got, err := a.FetchComments(t.Context(), "ISSUENODE_1", cfg.Provider.Comments, markers, "human-user")
	if err != nil {
		t.Fatalf("FetchComments が失敗した: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("continuo 自身のコメントが外れていません: %d 件 %+v", len(got), got)
	}
	if !got[0].IsAgent {
		t.Errorf("印を足したら、エージェントが書いたコメントとして数えられなくなりました: %q", got[0].Body)
	}
	if !strings.Contains(got[0].Body, config.AIMarker) {
		t.Errorf("本文から機械の印が消えています: %q", got[0].Body)
	}
}
