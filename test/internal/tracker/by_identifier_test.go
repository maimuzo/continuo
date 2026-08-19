package tracker_test

import (
	"strings"
	"testing"
)

// 目的: 識別子で1件引けること、そのときに **Status で絞らない**ことを確認する
// （設計 3-25 / 3-26: グループの他の issue は `Ice Box` に置かれるので、`active_states` で
// 絞ると表明が1件も反映されない）。
// 与える情報: `Ice Box` の item を含むボードの応答。
// 成功条件: `Ice Box` の issue が引けて、送ったクエリに Status の絞り込み（`query:` 変数）が
// 入っていないこと。
func TestFetchIssueByIdentifier_IceBoxのissueもStatusで絞らずに引ける(t *testing.T) {
	item := issueItemJSON(testIssueItemOpts{
		ItemID: "item-45", Status: "Ice Box", Owner: "maimuzo", Repo: "koetsumugi", Number: 45,
		Title: "グループの別の issue", URL: "https://github.com/maimuzo/koetsumugi/issues/45",
	})
	fs := newFakeGraphQLServer(t, single(dataResponse(
		candidateItemsPayload([]map[string]any{item}, false, ""))))
	a := newAdapterForFetch(t, fs)

	issue, ok, err := a.FetchIssueByIdentifier(t.Context(), "maimuzo/koetsumugi#45")
	if err != nil {
		t.Fatalf("識別子で引くのに失敗した: %v", err)
	}
	if !ok {
		t.Fatalf("ボードに載っている issue を引けなかった")
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

// 目的: ボードに載っていない識別子を渡しても**エラーにしない**ことを確認する
// （06_orchestrator.md:「bool は『ボードに載っているか』である。載っていなければ
// (ゼロ値, false, nil) を返す。エージェントが存在しない issue 番号を書くことはありうるので、
// それをエラーにしない」）。
// 与える情報: 別の識別子の item だけが載っているボードの応答。
// 成功条件: エラーが nil、bool が false、Issue がゼロ値であること。
func TestFetchIssueByIdentifier_載っていなければエラーにせずfalseを返す(t *testing.T) {
	item := issueItemJSON(testIssueItemOpts{
		ItemID: "item-10", Status: "Ready", Owner: "maimuzo", Repo: "koetsumugi", Number: 10,
		Title: "別の issue", URL: "https://github.com/maimuzo/koetsumugi/issues/10",
	})
	fs := newFakeGraphQLServer(t, single(dataResponse(
		candidateItemsPayload([]map[string]any{item}, false, ""))))
	a := newAdapterForFetch(t, fs)

	issue, ok, err := a.FetchIssueByIdentifier(t.Context(), "maimuzo/koetsumugi#999")
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

// 目的: ページをまたいでも引けることを確認する（104件のボードは1リクエストで収まるが、
// それを超えたときに黙って取り逃さないこと）。
// 与える情報: 1ページ目に別の item、2ページ目に目的の item がある応答。
// 成功条件: 2ページ目の issue を引けること。
func TestFetchIssueByIdentifier_ページをまたいでも引ける(t *testing.T) {
	first := issueItemJSON(testIssueItemOpts{
		ItemID: "item-10", Status: "Ready", Owner: "maimuzo", Repo: "koetsumugi", Number: 10, Title: "1ページ目",
	})
	second := issueItemJSON(testIssueItemOpts{
		ItemID: "item-47", Status: "Ice Box", Owner: "maimuzo", Repo: "koetsumugi", Number: 47, Title: "2ページ目",
	})
	fs := newFakeGraphQLServer(t, func(n int, _ capturedRequest) fakeGraphQLResponse {
		if n == 1 {
			return dataResponse(candidateItemsPayload([]map[string]any{first}, true, "CURSOR1"))
		}
		return dataResponse(candidateItemsPayload([]map[string]any{second}, false, ""))
	})
	a := newAdapterForFetch(t, fs)

	issue, ok, err := a.FetchIssueByIdentifier(t.Context(), "maimuzo/koetsumugi#47")
	if err != nil {
		t.Fatalf("識別子で引くのに失敗した: %v", err)
	}
	if !ok || issue.ID != "item-47" {
		t.Fatalf("2ページ目の issue を引けなかった: ok=%v issue=%+v", ok, issue)
	}
}
