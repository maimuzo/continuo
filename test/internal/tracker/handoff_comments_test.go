package tracker_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// commentNode は偽サーバが返すコメント1件を組み立てる。
//
// id: コメントのノード ID。
// body: 本文。
// createdAt: 作成時刻（RFC3339）。
// author: 投稿者のログイン名。
// 戻り値: 応答に載せる形。
func commentNode(id, body, createdAt, author string) map[string]any {
	return map[string]any{
		"id": id, "url": "https://example.com/" + id, "body": body,
		"createdAt": createdAt, "author": map[string]any{"login": author},
	}
}

// 目的: 持ち回りの印が付いたコメントを、エージェントへ渡す入力から外すことを確認する
// （設計 3-77a）。
//
// **投稿者は問わない。**入札は巡回のたびに積み上がるので、混ぜるとエージェントへ渡す
// 入力がそれで埋まる。
//
// **外すのは3つだけである。**`continuo:agent` と `continuo:self` は今までどおり扱う。
//
// 与える情報: 入札・hold・released・エージェントのコメント・人間のコメントの5件。
// 成功条件: 結果が2件（エージェントと人間）だけになること。
func TestFetchComments_持ち回りの印のコメントを外す(t *testing.T) {
	cfg := testTrackerConfig()
	markers := cfg.Comments

	nodes := []map[string]any{
		commentNode("c5", config.HandoffReleasedMarker+"\n{\"from\":\"mac-studio\"}", "2026-08-05T00:00:00Z", "continuo-bot"),
		commentNode("c4", config.HandoffHoldMarker+"\n{\"host\":\"mac-studio\"}", "2026-08-04T00:00:00Z", "continuo-bot"),
		commentNode("c3", config.HandoffBidMarker+"\n{\"host\":\"thinkpad\"}", "2026-08-03T00:00:00Z", "other-bot"),
		commentNode("c2", markers.Marker+"\nエージェントが書いた", "2026-08-02T00:00:00Z", "continuo-bot"),
		commentNode("c1", "人間が書いた", "2026-08-01T00:00:00Z", "human-user"),
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(map[string]any{
		"node": map[string]any{"__typename": "Issue", "comments": map[string]any{"nodes": nodes}},
	})))
	a := newAdapterForFetch(t, fs)

	comments, err := a.FetchComments(t.Context(), "ISSUENODE_1", cfg.Provider.Comments, markers, "continuo-bot")
	if err != nil {
		t.Fatalf("FetchComments が失敗した: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("持ち回りの印のコメントが外れていない: %d 件（2件であるべき）", len(comments))
	}
	for _, c := range comments {
		for _, marker := range []string{config.HandoffBidMarker, config.HandoffHoldMarker, config.HandoffReleasedMarker} {
			if strings.HasPrefix(strings.TrimSpace(c.Body), marker) {
				t.Errorf("%s のコメントが結果に残っている: %+v", marker, c)
			}
		}
	}
}

// 目的: 持ち回りの判定に使う取得は、印の付いたコメントも1件残らず返すことを確認する
// （設計 3-77a）。
//
// **外したものを見なければ、誰の担当かも誰が勝ったかも判定できない。**
//
// 与える情報: 入札・hold・エージェントのコメントの3件。
// 成功条件: 3件とも古い順で返ること。
func TestFetchAllComments_印の付いたコメントも落とさない(t *testing.T) {
	cfg := testTrackerConfig()

	nodes := []map[string]any{
		commentNode("c3", cfg.Comments.Marker+"\nエージェントが書いた", "2026-08-03T00:00:00Z", "continuo-bot"),
		commentNode("c2", config.HandoffHoldMarker+"\n{\"host\":\"mac-studio\"}", "2026-08-02T00:00:00Z", "continuo-bot"),
		commentNode("c1", config.HandoffBidMarker+"\n{\"host\":\"thinkpad\"}", "2026-08-01T00:00:00Z", "other-bot"),
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(map[string]any{
		"node": map[string]any{"__typename": "Issue", "comments": map[string]any{"nodes": nodes}},
	})))
	a := newAdapterForFetch(t, fs)

	comments, truncated, err := a.FetchAllComments(t.Context(), "ISSUENODE_1", cfg.Provider.Comments)
	if err != nil {
		t.Fatalf("FetchAllComments が失敗した: %v", err)
	}
	if truncated {
		t.Error("1ページで読み切っているのに、切れたと名乗っている")
	}
	if len(comments) != 3 {
		t.Fatalf("件数が想定と違う: got %d, want 3", len(comments))
	}
	if comments[0].ID != "c1" || comments[2].ID != "c3" {
		t.Errorf("古い順になっていない: got [%s, %s, %s]", comments[0].ID, comments[1].ID, comments[2].ID)
	}
	if comments[0].Author != "other-bot" {
		t.Errorf("投稿者が入っていない: %+v", comments[0])
	}
}

// 目的: 続きがある限りコメントを取り直し、issue に付いたコメントを全部返すことを確認する
// （設計 3-77a）。
//
// **入札で押し流されると、エージェントが書いた報告が見えなくなる。**
//
// 与える情報: 1ページ2件・続きありの応答を1回、続き無しの応答を1回返す偽サーバ。
// 成功条件: リクエストが2本送られ、2ページ目に `after` が入っていること。
// 結果が4件で、古い順（c1 → c4）になっていること。
func TestFetchAllComments_続きがある限り取り直す(t *testing.T) {
	cfg := testTrackerConfig().Provider.Comments
	cfg.Max = 2

	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		if n == 1 {
			return dataResponse(map[string]any{"node": map[string]any{
				"__typename": "Issue",
				"comments": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": true, "endCursor": "CURSOR1"},
					"nodes": []map[string]any{
						commentNode("c4", "4件目", "2026-08-04T00:00:00Z", "human-user"),
						commentNode("c3", "3件目", "2026-08-03T00:00:00Z", "human-user"),
					},
				},
			}})
		}
		return dataResponse(map[string]any{"node": map[string]any{
			"__typename": "Issue",
			"comments": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
				"nodes": []map[string]any{
					commentNode("c2", "2件目", "2026-08-02T00:00:00Z", "human-user"),
					commentNode("c1", "1件目", "2026-08-01T00:00:00Z", "human-user"),
				},
			},
		}})
	})
	a := newAdapterForFetch(t, fs)

	comments, truncated, err := a.FetchAllComments(t.Context(), "ISSUENODE_1", cfg)
	if err != nil {
		t.Fatalf("FetchAllComments が失敗した: %v", err)
	}
	if truncated {
		t.Error("hasNextPage が偽で終わったのに、切れたと名乗っている")
	}

	reqs := fs.Requests()
	if len(reqs) != 2 {
		t.Fatalf("続きを取り直していない: リクエストが %d 本（2本であるべき）", len(reqs))
	}
	if got := reqs[1].Variables["after"]; got != "CURSOR1" {
		t.Errorf("2ページ目に続きの位置を渡していない: got %v, want CURSOR1", got)
	}
	if len(comments) != 4 {
		t.Fatalf("全部取れていない: got %d 件, want 4 件", len(comments))
	}
	if comments[0].ID != "c1" || comments[3].ID != "c4" {
		t.Errorf("古い順になっていない: got [%s, %s, %s, %s]",
			comments[0].ID, comments[1].ID, comments[2].ID, comments[3].ID)
	}
}

// 目的: `tracker.provider.comments.max` の意味が変わっていないことを確認する（設計 5-2 / 3-77f）。
//
// **`max` は「判別のために何件まで遡るか」である。**1ページの件数ではない。
// **数えるのは持ち回りの印を外したあとの件数である。**入札は巡回のたびに積み上がるので、
// **印の付いたものを数に入れると、エージェントが書いた報告が窓から押し出される。**
//
// 与える情報: `max: 2` の設定。1ページ目は入札2件だけ（続きあり）、2ページ目に人間の
// コメント2件と、さらに古い1件。
// 成功条件: 印を外した2件だけが返ること（古い順で c2 → c3）。
// **入札しか無い1ページ目で打ち切らず、続きを取り直していること。**
func TestFetchComments_maxは印を外したあとの件数を数える(t *testing.T) {
	cfg := testTrackerConfig()
	cfg.Provider.Comments.Max = 2

	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		if n == 1 {
			return dataResponse(map[string]any{"node": map[string]any{
				"__typename": "Issue",
				"comments": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": true, "endCursor": "CURSOR1"},
					"nodes": []map[string]any{
						commentNode("b2", config.HandoffBidMarker+"\n{\"host\":\"thinkpad\"}", "2026-08-06T00:00:00Z", "other-bot"),
						commentNode("b1", config.HandoffBidMarker+"\n{\"host\":\"mac-studio\"}", "2026-08-05T00:00:00Z", "continuo-bot"),
					},
				},
			}})
		}
		return dataResponse(map[string]any{"node": map[string]any{
			"__typename": "Issue",
			"comments": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
				"nodes": []map[string]any{
					commentNode("c3", "3件目", "2026-08-03T00:00:00Z", "human-user"),
					commentNode("c2", "2件目", "2026-08-02T00:00:00Z", "human-user"),
					commentNode("c1", "1件目", "2026-08-01T00:00:00Z", "human-user"),
				},
			},
		}})
	})
	a := newAdapterForFetch(t, fs)

	comments, err := a.FetchComments(t.Context(), "ISSUENODE_1", cfg.Provider.Comments, cfg.Comments, "continuo-bot")
	if err != nil {
		t.Fatalf("FetchComments が失敗した: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("max の件数どおりに絞れていない: got %d 件, want 2 件", len(comments))
	}
	if comments[0].ID != "c2" || comments[1].ID != "c3" {
		t.Errorf("新しい方から2件を古い順で返していない: got [%s, %s]", comments[0].ID, comments[1].ID)
	}
	if got := len(fs.Requests()); got != 2 {
		t.Errorf("入札しか無いページで打ち切っている: リクエストが %d 本（2本であるべき）", got)
	}
}

// 目的: 印の付いていないコメントが `max` 件揃ったら、そこで取るのをやめることを確認する
// （設計 3-77f）。
//
// **`max: 50` と書いて費用を抑えている人の問い合わせを増やさない。**
// 入札の積まれていない issue では、いままでどおり1回の問い合わせで終わる。
//
// 与える情報: `max: 2` の設定。1ページ目に人間のコメント3件（続きあり）。
// 成功条件: リクエストが1本だけであること。
func TestFetchComments_max件が揃ったら取るのをやめる(t *testing.T) {
	cfg := testTrackerConfig()
	cfg.Provider.Comments.Max = 2

	fs := newFakeGraphQLServer(t, single(dataResponse(map[string]any{"node": map[string]any{
		"__typename": "Issue",
		"comments": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": true, "endCursor": "CURSOR1"},
			"nodes": []map[string]any{
				commentNode("c3", "3件目", "2026-08-03T00:00:00Z", "human-user"),
				commentNode("c2", "2件目", "2026-08-02T00:00:00Z", "human-user"),
				commentNode("c1", "1件目", "2026-08-01T00:00:00Z", "human-user"),
			},
		},
	}})))
	a := newAdapterForFetch(t, fs)

	comments, err := a.FetchComments(t.Context(), "ISSUENODE_1", cfg.Provider.Comments, cfg.Comments, "continuo-bot")
	if err != nil {
		t.Fatalf("FetchComments が失敗した: %v", err)
	}
	if got := len(fs.Requests()); got != 1 {
		t.Fatalf("要る件数が揃ったのに取り直している: リクエストが %d 本（1本であるべき）", got)
	}
	if len(comments) != 2 {
		t.Fatalf("max の件数どおりに絞れていない: got %d 件, want 2 件", len(comments))
	}
	if comments[0].ID != "c2" || comments[1].ID != "c3" {
		t.Errorf("新しい方から2件を古い順で返していない: got [%s, %s]", comments[0].ID, comments[1].ID)
	}
}

// 目的: 担当者を書き足す呼び出しが `addAssigneesToAssignable` を使い、
// 名指ししたノード ID だけを送ることを確認する（設計 3-77b）。
//
// 与える情報: 担当者1人を足す呼び出し。
// 成功条件: 送ったクエリが `addAssigneesToAssignable` を含み、変数に issue のノード ID と
// 担当者のノード ID が入っていること。返る担当者の一覧が応答のとおりであること。
func TestAddAssignees_addAssigneesToAssignableを使う(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(map[string]any{
		"addAssigneesToAssignable": map[string]any{
			"assignable": map[string]any{
				"id": "ISSUENODE_1",
				"assignees": map[string]any{
					"totalCount": 1,
					"nodes":      []map[string]any{{"id": "U_bot", "login": "octocat-bot"}},
				},
			},
		},
	})))
	a := newAdapterForFetch(t, fs)

	got, err := a.AddAssignees(t.Context(), "ISSUENODE_1", []string{"U_bot"})
	if err != nil {
		t.Fatalf("AddAssignees が失敗した: %v", err)
	}
	if len(got) != 1 || got[0].Login != "octocat-bot" {
		t.Fatalf("書き足したあとの担当者が返っていない: %+v", got)
	}

	reqs := fs.Requests()
	if len(reqs) != 1 {
		t.Fatalf("リクエストが %d 本（1本であるべき）", len(reqs))
	}
	if !strings.Contains(reqs[0].Query, "addAssigneesToAssignable") {
		t.Errorf("addAssigneesToAssignable を使っていない:\n%s", reqs[0].Query)
	}
	if got := reqs[0].Variables["assignableId"]; got != "ISSUENODE_1" {
		t.Errorf("issue のノード ID を送っていない: got %v", got)
	}
}

// 目的: 空の一覧で呼ばれたら、GitHub へ1リクエストも送らないことを確認する（設計 3-77b）。
//
// **空の配列を送っても GitHub は成功を返すが、呼んだ側は「書けた」と読む。**
// 書いていないことを、書いていないと返す。
//
// 与える情報: 空の担当者の一覧。
// 成功条件: リクエストが1本も送られないこと。
func TestAddAssignees_空なら1リクエストも送らない(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(nil)))
	a := newAdapterForFetch(t, fs)

	if _, err := a.AddAssignees(t.Context(), "ISSUENODE_1", nil); err != nil {
		t.Fatalf("空で呼んだのにエラーになった: %v", err)
	}
	if got := fs.RequestCount(); got != 0 {
		t.Errorf("空なのにリクエストを送っている: %d 本", got)
	}
}

// 目的: ページ数の上限で古い側を読み切れなかったことを、戻り値で名乗ることを確認する
// （#140（人間が担当者で着手できないことを、issue のコメントとして1回だけ書く））。
//
// **件数では当てられない。**1ページが100件に満たないまま上限を使い切ると、
// 2000件に届かないまま切れる。逆にちょうど2000件で続きが無いこともある。
// **切れているのに気づけないと、前の起動で書いた案内を見落として同じ案内を2件書く。**
// **コメントを消す手段は無い。**
//
// 与える情報: 毎ページ「続きがある」と答え続ける偽サーバ。
// 成功条件: 2つ目の戻り値が真になること。
func TestFetchAllComments_ページ数の上限で切れたら真を返す(t *testing.T) {
	cfg := testTrackerConfig().Provider.Comments

	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		return dataResponse(map[string]any{"node": map[string]any{
			"__typename": "Issue",
			"comments": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": true, "endCursor": "CURSOR"},
				"nodes": []map[string]any{
					commentNode("c"+strconv.Itoa(n), "本文", "2026-08-01T00:00:00Z", "human-user"),
				},
			},
		}})
	})
	a := newAdapterForFetch(t, fs)

	_, truncated, err := a.FetchAllComments(t.Context(), "ISSUENODE_1", cfg)
	if err != nil {
		t.Fatalf("FetchAllComments が失敗した: %v", err)
	}
	if !truncated {
		t.Error("上限まで読んでも続きがあるのに、切れたと名乗っていない")
	}
}
