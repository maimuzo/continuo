package tracker_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/tracker"
)

// 目的: FetchComments が、continuo 自身が代筆したコメント（self_marker で始まる）を
// 次の turn の入力から除外し、エージェントが書いたコメント（marker で始まる）は
// IsAgent=true として残すことを確認する（設計「その7」/ 5-2）。
// 与える情報: 人間が書いたコメント・エージェントが marker 付きで書いたコメント・
// continuo が self_marker 付きで代筆したコメントの3件。
// 成功条件: 結果が2件（self_marker のものが除外される）で、marker 付きのものが
// IsAgent=true、人間のコメントは IsAgent=false であること。
func TestFetchComments_selfMarkerのコメントは除外しmarkerは判別する(t *testing.T) {
	cfg := testTrackerConfig()
	commentsCfg := cfg.Provider.Comments
	markers := cfg.Comments

	human := map[string]any{"id": "c1", "url": "https://example.com/c1", "body": "人間が書いたコメント", "createdAt": "2026-08-01T00:00:00Z", "author": map[string]any{"login": "human-user"}}
	agent := map[string]any{"id": "c2", "url": "https://example.com/c2", "body": markers.Marker + "\nエージェントが書いた", "createdAt": "2026-08-02T00:00:00Z", "author": map[string]any{"login": "human-user"}}
	self := map[string]any{"id": "c3", "url": "https://example.com/c3", "body": markers.SelfMarker + "\ncontinuo が代筆した", "createdAt": "2026-08-03T00:00:00Z", "author": map[string]any{"login": "continuo-bot"}}

	// **偽サーバは新しい順（DESC）で返す。**FetchComments は「新しい方から max 件」を
	// 要求するので、GitHub もこの順で返す。FetchComments が古い順へ並べ替えて返す。
	fs := newFakeGraphQLServer(t, single(dataResponse(map[string]any{
		"node": map[string]any{
			"__typename": "Issue",
			"comments":   map[string]any{"nodes": []map[string]any{self, agent, human}},
		},
	})))
	a := newAdapterForFetch(t, fs)

	// **持ち主を空文字で渡す**（= `gh api user` で取れなかったとき。設計 3-65）。
	// そのときは投稿者を照合せず、印だけで判定する。
	comments, err := a.FetchComments(t.Context(), "ISSUENODE_1", commentsCfg, markers, "")
	if err != nil {
		t.Fatalf("FetchComments が失敗した: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("件数が想定と違う: got %d, want 2（self_marker のコメントは除外されるはず）", len(comments))
	}
	if comments[0].IsAgent {
		t.Fatalf("人間のコメントが IsAgent=true になっている")
	}
	if !comments[1].IsAgent {
		t.Fatalf("marker 付きのコメントが IsAgent=true になっていない")
	}
	for _, c := range comments {
		if c.IsSelf {
			t.Fatalf("除外されるべき self_marker のコメントが結果に残っている: %+v", c)
		}
	}
}

// 目的: 設定が既定値（ゼロ値）でも、コメントの取得を止める経路が無いことを確認する。
//
// **取得を止められると、成功した run も含めて全件が failure_state へ落ちる。**
// FetchComments が nil を返す → internal/orchestrator の hasRunComment が常に false →
// failCommentRecovery が failure_state を書く、という経路になるためである。
// 取得を止める設定キーは消してあるので、ゼロ値の設定でも必ず GraphQL を叩く。
//
// 与える情報: 何も書いていない（ゼロ値の）tracker.provider.comments の設定。
// 成功条件: 偽サーバへのリクエストが1件送られること。
func TestFetchComments_設定がゼロ値でも取得を止めない(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(map[string]any{
		"node": map[string]any{"__typename": "Issue", "comments": map[string]any{"nodes": []map[string]any{}}},
	})))
	a := newAdapterForFetch(t, fs)

	if _, err := a.FetchComments(t.Context(), "ISSUENODE_1",
		config.TrackerProviderCommentsConfig{}, config.TrackerCommentsConfig{}, ""); err != nil {
		t.Fatalf("ゼロ値の設定なのにエラーになった: %v", err)
	}
	if fs.RequestCount() != 1 {
		t.Fatalf("取得が止まっている: リクエストが %d 件（1件送られるはず）", fs.RequestCount())
	}
}

// 目的: PostComment が本文の先頭に self_marker を付けて投稿し、返す Comment の
// IsSelf が true になることを確認する（設計 3-25: 「代筆したものには self_marker の印を
// 付けて、エージェントが書いたものと区別する」）。
// 与える情報: 素の本文 "作業内容の要約" と self_marker。
// 成功条件: 偽サーバが受け取ったミューテーションの body 変数が
// "<self_marker>\n作業内容の要約" になっていること。戻り値の IsSelf が true であること。
func TestPostComment_selfMarkerを付けて投稿する(t *testing.T) {
	const selfMarker = "<!-- continuo:self -->"
	const rawBody = "作業内容の要約"

	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		body, _ := req.Variables["body"].(string)
		if !strings.HasPrefix(body, selfMarker) {
			t.Errorf("投稿した本文の先頭に self_marker が付いていない: %q", body)
		}
		if !strings.Contains(body, rawBody) {
			t.Errorf("投稿した本文に元の本文が含まれていない: %q", body)
		}
		return dataResponse(map[string]any{
			"addComment": map[string]any{
				"commentEdge": map[string]any{
					"node": map[string]any{
						"id": "c-new", "url": "https://example.com/c-new", "body": body,
						"createdAt": "2026-08-18T00:00:00Z",
						"author":    map[string]any{"login": "continuo-bot"},
					},
				},
			},
		})
	})
	a := newAdapterForFetch(t, fs)

	comment, err := a.PostComment(t.Context(), "ISSUENODE_1", rawBody, selfMarker)
	if err != nil {
		t.Fatalf("PostComment が失敗した: %v", err)
	}
	if !comment.IsSelf {
		t.Fatalf("投稿したコメントの IsSelf が true になっていない")
	}
	if fs.RequestCount() != 1 {
		t.Fatalf("リクエスト件数が想定と違う: got %d, want 1", fs.RequestCount())
	}
}

// 目的: comments.max が GitHub の上限（100）を超えていても、送信する first が 100 以下に
// 丸められることを確認する。**101 を要求すると EXCESSIVE_PAGINATION のエラーになり、
// コメント取得が毎回失敗する。**
// 与える情報: max: 200 の設定。
// 成功条件: 偽サーバが受け取った GraphQL 変数 first が 100 以下であること。エラーにならないこと。
func TestFetchComments_maxが100を超えても送るfirstは100以下(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(map[string]any{
		"node": map[string]any{"__typename": "Issue", "comments": map[string]any{"nodes": []map[string]any{}}},
	})))
	a := newAdapterForFetch(t, fs)

	cfg := testTrackerConfig().Provider.Comments
	cfg.Max = 200
	if _, err := a.FetchComments(t.Context(), "ISSUENODE_1", cfg, testTrackerConfig().Comments, ""); err != nil {
		t.Fatalf("FetchComments が失敗した: %v", err)
	}

	reqs := fs.Requests()
	if len(reqs) != 1 {
		t.Fatalf("リクエスト件数が想定と違う: got %d, want 1", len(reqs))
	}
	first, ok := reqs[0].Variables["first"].(float64)
	if !ok {
		t.Fatalf("GraphQL 変数 first が数値ではない: %#v", reqs[0].Variables["first"])
	}
	if first > 100 {
		t.Fatalf("送信した first が GitHub の上限を超えている: got %v, want 100 以下", first)
	}
}

// 目的: コメントが max 件を超えるとき、結果に**最新の**コメントが含まれることを確認する
// （設計 5-2: 「max: 50 # 判別のために何件まで遡るか」。遡るとは新しい方から数えることで
// あり、古い方から max 件を取ると最新のコメントが落ちる）。
// 与える情報: max=2 の設定と、GitHub と同じ振る舞いをする偽サーバ
// （orderBy の direction に従って並べ、first 件だけ返す）。コメントは古い順に c1・c2・c3 の3件。
// 成功条件: 結果が2件で、最新の c3 が含まれること。並びは古い順（c2 → c3）であること。
func TestFetchComments_max件を超えるとき最新のコメントが残る(t *testing.T) {
	// 古い順に並べた全コメント。
	all := []map[string]any{
		{"id": "c1", "url": "https://example.com/c1", "body": "一番古い", "createdAt": "2026-08-01T00:00:00Z", "author": map[string]any{"login": "human-user"}},
		{"id": "c2", "url": "https://example.com/c2", "body": "真ん中", "createdAt": "2026-08-02T00:00:00Z", "author": map[string]any{"login": "human-user"}},
		{"id": "c3", "url": "https://example.com/c3", "body": "最新", "createdAt": "2026-08-03T00:00:00Z", "author": map[string]any{"login": "human-user"}},
	}

	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		first := 0
		if f, ok := req.Variables["first"].(float64); ok {
			first = int(f)
		}
		// GitHub と同じく、orderBy の direction に従って並べたうえで先頭から first 件を返す。
		ordered := make([]map[string]any, len(all))
		if strings.Contains(req.Query, "DESC") {
			for i, c := range all {
				ordered[len(all)-1-i] = c
			}
		} else {
			copy(ordered, all)
		}
		if first > 0 && first < len(ordered) {
			ordered = ordered[:first]
		}
		return dataResponse(map[string]any{
			"node": map[string]any{"__typename": "Issue", "comments": map[string]any{"nodes": ordered}},
		})
	})
	a := newAdapterForFetch(t, fs)

	cfg := testTrackerConfig().Provider.Comments
	cfg.Max = 2
	comments, err := a.FetchComments(t.Context(), "ISSUENODE_1", cfg, testTrackerConfig().Comments, "")
	if err != nil {
		t.Fatalf("FetchComments が失敗した: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("件数が想定と違う: got %d, want 2", len(comments))
	}
	if comments[0].ID != "c2" || comments[1].ID != "c3" {
		t.Fatalf("最新のコメントが落ちているか、古い順になっていない: got [%s, %s], want [c2, c3]", comments[0].ID, comments[1].ID)
	}
}

// 目的: comments.order に想定外の値が書かれていたら、黙って無視せずエラーにすることを
// 確認する（書いたつもりの設定が効いていないことに気づけないため）。
// 与える情報: order: "newest_first" の設定。
// 成功条件: CategoryInvalidConfig のエラーになり、リクエストが送られないこと。
func TestFetchComments_想定外のorderはエラーにする(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(nil)))
	a := newAdapterForFetch(t, fs)

	cfg := testTrackerConfig().Provider.Comments
	cfg.Order = "newest_first"
	_, err := a.FetchComments(t.Context(), "ISSUENODE_1", cfg, testTrackerConfig().Comments, "")
	if err == nil {
		t.Fatalf("想定外の order を渡したのにエラーにならなかった")
	}
	if !tracker.IsCategory(err, tracker.CategoryInvalidConfig) {
		t.Fatalf("エラーのカテゴリが CategoryInvalidConfig ではない: %v", err)
	}
	if fs.RequestCount() != 0 {
		t.Fatalf("設定が不正なのにリクエストが送られた: %d件", fs.RequestCount())
	}
}

// 目的: 印（marker / self_marker）が付いていても、投稿者が「continuo が使う gh の持ち主」
// でなければ continuo の側が書いたものとして扱わないことを確認する（設計 3-65）。
//
// **印は本文の先頭に置くただの文字列であり、issue にコメントできる人なら誰でも書ける。**
// 投稿者を照合しないと、外部の第三者のコメントを「エージェントが成果を書いた」と読み違え、
// 何も書かれていない issue がそのまま片付く。
//
// 与える情報: 持ち主（octocat）が marker 付きで書いたもの・第三者（outsider）が marker 付きで
// 書いたもの・第三者が self_marker 付きで書いたもの・持ち主の名前が大文字（OCTOCAT）の
// marker 付きのものの4件と、持ち主 "octocat"。
// 成功条件: 4件すべてが結果に残り（第三者の self_marker のコメントは除外されない）、
// IsAgent=true になるのは持ち主が書いた2件だけで、**第三者の2件には MarkedByOther が
// 立っている**こと（呼び出し側が「印はあるが投稿者が違う」を名指しでログに出せる）。
func TestFetchComments_投稿者が持ち主でなければ印を数えない(t *testing.T) {
	cfg := testTrackerConfig()
	commentsCfg := cfg.Provider.Comments
	markers := cfg.Comments
	const selfLogin = "octocat"

	mine := map[string]any{"id": "c1", "url": "https://example.com/c1", "body": markers.Marker + "\nエージェントが書いた", "createdAt": "2026-08-01T00:00:00Z", "author": map[string]any{"login": selfLogin}}
	spoofed := map[string]any{"id": "c2", "url": "https://example.com/c2", "body": markers.Marker + "\n第三者が同じ印で書いた", "createdAt": "2026-08-02T00:00:00Z", "author": map[string]any{"login": "outsider"}}
	spoofedSelf := map[string]any{"id": "c3", "url": "https://example.com/c3", "body": markers.SelfMarker + "\n第三者が continuo の印で書いた", "createdAt": "2026-08-03T00:00:00Z", "author": map[string]any{"login": "outsider"}}
	// **GitHub のログイン名は大文字小文字を区別しない。**畳んで比べる。
	upper := map[string]any{"id": "c4", "url": "https://example.com/c4", "body": markers.Marker + "\n同じ持ち主が書いた", "createdAt": "2026-08-04T00:00:00Z", "author": map[string]any{"login": "OCTOCAT"}}

	// **偽サーバは新しい順（DESC）で返す。**FetchComments が古い順へ並べ替えて返す。
	fs := newFakeGraphQLServer(t, single(dataResponse(map[string]any{
		"node": map[string]any{
			"__typename": "Issue",
			"comments":   map[string]any{"nodes": []map[string]any{upper, spoofedSelf, spoofed, mine}},
		},
	})))
	a := newAdapterForFetch(t, fs)

	comments, err := a.FetchComments(t.Context(), "ISSUENODE_1", commentsCfg, markers, selfLogin)
	if err != nil {
		t.Fatalf("FetchComments が失敗した: %v", err)
	}
	if len(comments) != 4 {
		t.Fatalf("件数が想定と違う: got %d, want 4（第三者の self_marker のコメントは除外されない）", len(comments))
	}
	agents := map[string]bool{}
	for _, c := range comments {
		agents[c.ID] = c.IsAgent
	}
	if !agents["c1"] {
		t.Fatalf("持ち主が marker 付きで書いたコメントが IsAgent=true になっていない")
	}
	if agents["c2"] {
		t.Fatalf("第三者が marker 付きで書いたコメントが IsAgent=true になっている")
	}
	if agents["c3"] {
		t.Fatalf("第三者が self_marker 付きで書いたコメントが IsAgent=true になっている")
	}
	if !agents["c4"] {
		t.Fatalf("持ち主の名前が大文字のコメントが IsAgent=true になっていない（畳んで比べていない）")
	}

	// **落としたことが分かる形で返っている**（設計 3-65）。これが返らないと、
	// 呼び出し側は「印はあるが投稿者が違う」を黙って捨てるしかなく、
	// 人間には「コメントが無い」としか見えなくなる。
	marked := map[string]bool{}
	for _, c := range comments {
		marked[c.ID] = c.MarkedByOther
	}
	for _, id := range []string{"c2", "c3"} {
		if !marked[id] {
			t.Fatalf("第三者が印付きで書いたコメント（%s）に MarkedByOther が立っていない", id)
		}
	}
	for _, id := range []string{"c1", "c4"} {
		if marked[id] {
			t.Fatalf("持ち主が書いたコメント（%s）に MarkedByOther が立っている", id)
		}
	}
}

// 目的: 持ち主を取れなかったとき（空文字）は、投稿者を照合しないので MarkedByOther が
// 1件も立たないことを確認する（設計 3-65）。
//
// **照合していないのに「投稿者が違う」と警告を出しては、人間を無駄に調べさせる。**
//
// 与える情報: 第三者（outsider）が marker 付きで書いたコメント1件と、空文字の持ち主。
// 成功条件: そのコメントが IsAgent=true（印だけの判定）で、MarkedByOther は偽であること。
func TestFetchComments_持ち主が空文字なら投稿者の食い違いを立てない(t *testing.T) {
	cfg := testTrackerConfig()
	markers := cfg.Comments
	spoofed := map[string]any{"id": "c1", "url": "https://example.com/c1", "body": markers.Marker + "\n第三者が同じ印で書いた", "createdAt": "2026-08-02T00:00:00Z", "author": map[string]any{"login": "outsider"}}

	fs := newFakeGraphQLServer(t, single(dataResponse(map[string]any{
		"node": map[string]any{
			"__typename": "Issue",
			"comments":   map[string]any{"nodes": []map[string]any{spoofed}},
		},
	})))
	a := newAdapterForFetch(t, fs)

	comments, err := a.FetchComments(t.Context(), "ISSUENODE_1", cfg.Provider.Comments, markers, "")
	if err != nil {
		t.Fatalf("FetchComments が失敗した: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("件数が想定と違う: got %d, want 1", len(comments))
	}
	if !comments[0].IsAgent {
		t.Fatalf("持ち主を取れていないときは印だけで判定するはずが、IsAgent=false になった")
	}
	if comments[0].MarkedByOther {
		t.Fatalf("投稿者を照合していないのに MarkedByOther が立っている")
	}
}

// 目的: Comment.WrittenBy が、持ち主を取れなかったとき（空文字）は照合を行わず true を
// 返すことを確認する（設計 3-65: 取れなければ印だけで判定する形に落ちる）。
// 与える情報: 投稿者が outsider のコメントと、空文字の持ち主・一致しない持ち主。
// 成功条件: 空文字なら true、一致しない名前なら false になること。
func TestCommentWrittenBy_持ち主が空文字なら照合しない(t *testing.T) {
	c := tracker.Comment{Author: "outsider"}
	if !c.WrittenBy("") {
		t.Fatalf("持ち主が空文字なのに false になった（印だけの判定へ落ちない）")
	}
	if c.WrittenBy("octocat") {
		t.Fatalf("投稿者が違うのに true になった")
	}
}
