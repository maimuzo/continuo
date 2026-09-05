package tracker_test

import (
	"strings"
	"testing"
)

// withRepoIsPrivate は issueItemJSON の戻り値の `content.repository` に `isPrivate` を足す。
//
// **共通の組み立て（issueItemJSON）には足さない。**足すと、`isPrivate` を返さない provider の
// 応答を1件も再現できなくなり、「取れなかったときは掛ける側へ倒す」を確かめられない。
//
// item: issueItemJSON の戻り値。
// isPrivate: 足す値。
// 戻り値: `isPrivate` を足した item。
func withRepoIsPrivate(item map[string]any, isPrivate bool) map[string]any {
	content, _ := item["content"].(map[string]any)
	repo, _ := content["repository"].(map[string]any)
	repo["isPrivate"] = isPrivate
	return item
}

// 目的: 候補の取得のクエリが `isPrivate` を要求していることを確認する（設計 3-64）。
// **要求していなければ、公開かどうかは永久に「取れなかった」になる。**
//
// 与える情報: 通常の FetchIssuesByStates の呼び出し。
// 成功条件: 偽サーバが受け取った query 文字列に `isPrivate` が入っていること。
func TestFetchIssuesByStates_リポジトリが公開かどうかを取る(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nil, false, ""))))
	a := newAdapterForFetch(t, fs)

	if _, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"}); err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}

	reqs := fs.Requests()
	if len(reqs) != 1 {
		t.Fatalf("リクエスト件数が想定と違う: got %d, want 1", len(reqs))
	}
	if !strings.Contains(reqs[0].Query, "isPrivate") {
		t.Fatalf("クエリが isPrivate を要求していない: %s", reqs[0].Query)
	}
}

// 目的: `isPrivate` の値が Issue.RepoIsPrivate へそのまま入ること、そして
// **応答に `isPrivate` が無いときは nil（取れなかった）になる**ことを確認する（設計 3-64）。
//
// **nil を「公開ではない」に丸めてはならない。**丸めると、危ない道具の呼び出しの判定を
// 掛けるかどうかの分岐が、取れなかった issue で静かに外れる。
//
// 与える情報: `isPrivate` が真・偽・無し、の3件が載ったカンバンの応答。
// 成功条件: それぞれ true / false / nil になること。
func TestFetchIssuesByStates_公開かどうかを取れなければnilにする(t *testing.T) {
	nodes := []map[string]any{
		withRepoIsPrivate(issueItemJSON(testIssueItemOpts{
			ItemID: "PVTI_private", Status: "Ready", Owner: "octocat", Repo: "hello-world",
			Number: 1, Title: "非公開", URL: "https://github.com/octocat/hello-world/issues/1",
		}), true),
		withRepoIsPrivate(issueItemJSON(testIssueItemOpts{
			ItemID: "PVTI_public", Status: "Ready", Owner: "octocat", Repo: "hello-world",
			Number: 2, Title: "公開", URL: "https://github.com/octocat/hello-world/issues/2",
		}), false),
		issueItemJSON(testIssueItemOpts{
			ItemID: "PVTI_unknown", Status: "Ready", Owner: "octocat", Repo: "hello-world",
			Number: 3, Title: "取れなかった", URL: "https://github.com/octocat/hello-world/issues/3",
		}),
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nodes, false, ""))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"})
	if err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}
	if len(issues) != 3 {
		t.Fatalf("issue の件数が想定と違う: got %d, want 3", len(issues))
	}

	got := make(map[string]*bool, len(issues))
	for _, iss := range issues {
		got[iss.ID] = iss.RepoIsPrivate
	}
	if v := got["PVTI_private"]; v == nil || !*v {
		t.Fatalf("非公開リポジトリの issue が非公開になっていない: %v", v)
	}
	if v := got["PVTI_public"]; v == nil || *v {
		t.Fatalf("公開リポジトリの issue が公開になっていない: %v", v)
	}
	if v := got["PVTI_unknown"]; v != nil {
		t.Fatalf("応答に isPrivate が無いのに値が入っている（取れなかったことを失っている）: %v", *v)
	}
}
