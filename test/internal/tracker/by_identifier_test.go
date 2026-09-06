package tracker_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/maimuzo/continuo/internal/tracker"
)

// 目的: 識別子で1件引けること、そのときに **Status で絞らない**ことを確認する
// （設計 3-25 / 3-26: グループの他の issue は `Ice Box` に置かれるので、`active_states` で
// 絞ると表明が1件も反映されない）。
// 与える情報: `Ice Box` の item を含むカンバンの応答。
// 成功条件: `Ice Box` の issue が引けて、送ったクエリに Status の絞り込み（`query:` 変数）が
// 入っていないこと。
func TestFetchIssueByIdentifier_IceBoxのissueもStatusで絞らずに引ける(t *testing.T) {
	item := issueItemJSON(testIssueItemOpts{
		ItemID: "item-45", Status: "Ice Box", Owner: "octocat", Repo: "hello-world", Number: 45,
		Title: "グループの別の issue", URL: "https://github.com/octocat/hello-world/issues/45",
	})
	fs := newFakeGraphQLServer(t, single(dataResponse(
		candidateItemsPayload([]map[string]any{item}, false, ""))))
	a := newAdapterForFetch(t, fs)

	issue, ok, err := a.FetchIssueByIdentifier(t.Context(), "octocat/hello-world#45")
	if err != nil {
		t.Fatalf("識別子で引くのに失敗した: %v", err)
	}
	if !ok {
		t.Fatalf("カンバンに載っている issue を引けなかった")
	}
	if issue.ID != "item-45" || issue.State != "Ice Box" {
		t.Fatalf("引いた issue が想定と違う: id=%q state=%q", issue.ID, issue.State)
	}

	reqs := fs.Requests()
	if len(reqs) != 1 {
		t.Fatalf("リクエストの本数が想定と違う: got %d, want 1", len(reqs))
	}
	if _, ok := reqs[0].Variables["q"]; ok {
		t.Fatalf("Status の絞り込みを送っている（Ice Box の issue が引けなくなる）: %v", reqs[0].Variables)
	}
	if strings.Contains(reqs[0].Query, "query: $q") {
		t.Fatalf("クエリに Status の絞り込みが残っている: %s", reqs[0].Query)
	}
}

// 目的: カンバンに載っていない識別子を渡しても**エラーにしない**ことを確認する
// （06_orchestrator.md:「bool は『カンバンに載っているか』である。載っていなければ
// (ゼロ値, false, nil) を返す。エージェントが存在しない issue 番号を書くことはありうるので、
// それをエラーにしない」）。
// 与える情報: 別の識別子の item だけが載っているカンバンの応答。
// 成功条件: エラーが nil、bool が false、Issue がゼロ値であること。
func TestFetchIssueByIdentifier_載っていなければエラーにせずfalseを返す(t *testing.T) {
	item := issueItemJSON(testIssueItemOpts{
		ItemID: "item-10", Status: "Ready", Owner: "octocat", Repo: "hello-world", Number: 10,
		Title: "別の issue", URL: "https://github.com/octocat/hello-world/issues/10",
	})
	fs := newFakeGraphQLServer(t, single(dataResponse(
		candidateItemsPayload([]map[string]any{item}, false, ""))))
	a := newAdapterForFetch(t, fs)

	issue, ok, err := a.FetchIssueByIdentifier(t.Context(), "octocat/hello-world#999")
	if err != nil {
		t.Fatalf("載っていない識別子でエラーになった（エラーは通信の失敗と権限の不足だけに使う）: %v", err)
	}
	if ok {
		t.Fatalf("載っていない issue を見つけたことにしている: %+v", issue)
	}
	if issue.ID != "" {
		t.Fatalf("見つからなかったのにゼロ値以外を返した: %+v", issue)
	}
}

// 目的: ページをまたいでも引けることを確認する（104件のカンバンは1リクエストで収まるが、
// それを超えたときに黙って取り逃さないこと）。
// 与える情報: 1ページ目に別の item、2ページ目に目的の item がある応答。
// 成功条件: 2ページ目の issue を引けること。
func TestFetchIssueByIdentifier_ページをまたいでも引ける(t *testing.T) {
	first := issueItemJSON(testIssueItemOpts{
		ItemID: "item-10", Status: "Ready", Owner: "octocat", Repo: "hello-world", Number: 10, Title: "1ページ目",
	})
	second := issueItemJSON(testIssueItemOpts{
		ItemID: "item-47", Status: "Ice Box", Owner: "octocat", Repo: "hello-world", Number: 47, Title: "2ページ目",
	})
	fs := newFakeGraphQLServer(t, func(n int, _ capturedRequest) fakeGraphQLResponse {
		if n == 1 {
			return dataResponse(candidateItemsPayload([]map[string]any{first}, true, "CURSOR1"))
		}
		return dataResponse(candidateItemsPayload([]map[string]any{second}, false, ""))
	})
	a := newAdapterForFetch(t, fs)

	issue, ok, err := a.FetchIssueByIdentifier(t.Context(), "octocat/hello-world#47")
	if err != nil {
		t.Fatalf("識別子で引くのに失敗した: %v", err)
	}
	if !ok || issue.ID != "item-47" {
		t.Fatalf("2ページ目の issue を引けなかった: ok=%v issue=%+v", ok, issue)
	}
}

// 目的: 識別子で引くときに、識別子が一致しない item へ信頼の判定関数を呼ばないことを
// 確認する（レビュー指摘「照合の前に item ごとに信頼判定が走る」の回帰テスト）。
//
// **信頼の判定は毎回 ghq と git を起動して `~/.claude.json` を読み直す**（約56ミリ秒／件）。
// 照合の前に全件へ掛けると、表明1行あたりカンバン104件ぶん・外部プロセス208回になる。
//
// 与える情報: 3件の item が載ったカンバンと、呼ばれた `<owner>/<repo>` を記録する判定関数。
// 成功条件: 判定関数が呼ばれたのは一致した1件のリポジトリだけであること。
// 引いた Issue の Dispatchable にその判定結果が反映されていること。
func TestFetchIssueByIdentifier_一致した1件にだけ信頼判定を掛ける(t *testing.T) {
	items := []map[string]any{
		issueItemJSON(testIssueItemOpts{
			ItemID: "item-10", Status: "Ready", Owner: "octocat", Repo: "other-a", Number: 10,
			Title: "別のリポジトリ", URL: "https://github.com/octocat/other-a/issues/10",
		}),
		issueItemJSON(testIssueItemOpts{
			ItemID: "item-45", Status: "Ice Box", Owner: "octocat", Repo: "hello-world", Number: 45,
			Title: "引きたい issue", URL: "https://github.com/octocat/hello-world/issues/45",
		}),
		issueItemJSON(testIssueItemOpts{
			ItemID: "item-99", Status: "Ready", Owner: "octocat", Repo: "other-b", Number: 99,
			Title: "さらに別のリポジトリ", URL: "https://github.com/octocat/other-b/issues/99",
		}),
	}

	var mu sync.Mutex
	var asked []string
	trust := func(owner, repo string) bool {
		mu.Lock()
		asked = append(asked, owner+"/"+repo)
		mu.Unlock()
		return true
	}

	fs := newFakeGraphQLServer(t, single(dataResponse(
		candidateItemsPayload(items, false, ""))))
	a := newAdapterWithTrust(t, fs, trust)

	issue, ok, err := a.FetchIssueByIdentifier(t.Context(), "octocat/hello-world#45")
	if err != nil {
		t.Fatalf("識別子で引くのに失敗した: %v", err)
	}
	if !ok {
		t.Fatalf("カンバンに載っている issue を引けなかった")
	}
	if !issue.Dispatchable {
		t.Fatalf("一致した1件に信頼の判定が反映されていない")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 1 || asked[0] != "octocat/hello-world" {
		t.Fatalf("一致しない item にも信頼の判定を掛けている（外部プロセスが件数ぶん起動する）: %v", asked)
	}
}

// 目的: 一致した item のリポジトリが信頼登録されていなければ Dispatchable が偽になることを
// 確認する（信頼の判定を遅らせても、結果は落とさない）。
// 与える情報: 常に偽を返す信頼判定関数。
// 成功条件: 引いた Issue の Dispatchable が false であること。
func TestFetchIssueByIdentifier_信頼されていなければDispatchableが偽になる(t *testing.T) {
	item := issueItemJSON(testIssueItemOpts{
		ItemID: "item-45", Status: "Ice Box", Owner: "octocat", Repo: "hello-world", Number: 45,
		Title: "引きたい issue", URL: "https://github.com/octocat/hello-world/issues/45",
	})
	fs := newFakeGraphQLServer(t, single(dataResponse(
		candidateItemsPayload([]map[string]any{item}, false, ""))))
	a := newAdapterWithTrust(t, fs, func(owner, repo string) bool { return false })

	issue, ok, err := a.FetchIssueByIdentifier(t.Context(), "octocat/hello-world#45")
	if err != nil {
		t.Fatalf("識別子で引くのに失敗した: %v", err)
	}
	if !ok {
		t.Fatalf("カンバンに載っている issue を引けなかった")
	}
	if issue.Dispatchable {
		t.Fatalf("信頼登録されていないのに Dispatchable が真である")
	}
}

// 目的: カンバンのページ数に上限があり、超えたら CategoryPagination で落ちることを確認する
// （レビュー指摘「両方のページングに上限が無い」の回帰テスト）。
//
// **上限が無いと、カンバンが育つほど1回の呼び出しのコストが黙って増える。**
// 表明の対象1件ごとにカンバンを全ページ読むので、GitHub の API 枠（5,000 point/時。設計 3-31）
// を無言で食い潰す。
//
// 与える情報: 常に hasNextPage が真で、目的の識別子が1件も載っていないカンバンの応答。
// 成功条件: 無限に読み続けず、CategoryPagination のエラーで止まること。
func TestFetchIssueByIdentifier_ページ数の上限を超えたら落とす(t *testing.T) {
	item := issueItemJSON(testIssueItemOpts{
		ItemID: "item-10", Status: "Ready", Owner: "octocat", Repo: "hello-world", Number: 10,
		Title: "別の issue", URL: "https://github.com/octocat/hello-world/issues/10",
	})
	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		return dataResponse(candidateItemsPayload([]map[string]any{item}, true, "cursor"))
	})
	a := newAdapterForFetch(t, fs)

	_, _, err := a.FetchIssueByIdentifier(t.Context(), "octocat/hello-world#45")
	if err == nil {
		t.Fatalf("ページが尽きないのに止まらなかった（上限が効いていない）")
	}
	if !tracker.IsCategory(err, tracker.CategoryPagination) {
		t.Fatalf("エラーの分類が想定と違う: %v", err)
	}
	if fs.RequestCount() > 25 {
		t.Fatalf("上限を超えて読み続けている: %d リクエスト", fs.RequestCount())
	}
}
