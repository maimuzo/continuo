package tracker_test

import (
	"testing"
)

// 目的: 信頼登録されていないリポジトリの issue が、一覧から落とされずに
// Dispatchable=false で残ることを確認する（設計 3-13: 「dispatchable という1つの真偽値に
// 集約する。draft issue でない・Status が設定済み・リポジトリが信頼済み、をすべてここで
// 判定する」／「取得の段では落とさず、dispatch の判定で落とす」）。
// 与える情報: 信頼済みの octocat/hello-world と、未信頼の someone/untrusted の2件。
// 信頼の判定関数は octocat/hello-world だけを true にする。
// 成功条件: 2件とも一覧に残り、信頼済みの方だけ Dispatchable=true であること。
func TestFetchIssuesByStates_未信頼のリポジトリはdispatchableがfalseで残る(t *testing.T) {
	nodes := []map[string]any{
		issueItemJSON(testIssueItemOpts{
			ItemID: "item-trusted", Status: "Ready",
			Owner: "octocat", Repo: "hello-world", Number: 1, Title: "信頼済みのリポジトリ",
		}),
		issueItemJSON(testIssueItemOpts{
			ItemID: "item-untrusted", Status: "Ready",
			Owner: "someone", Repo: "untrusted", Number: 2, Title: "未信頼のリポジトリ",
		}),
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nodes, false, ""))))
	a := newAdapterWithTrust(t, fs, func(owner, repo string) bool {
		return owner == "octocat" && repo == "hello-world"
	})

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"})
	if err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("件数が想定と違う: got %d, want 2（未信頼でも一覧からは落とさないはず）: %v", len(issues), identifiersOf(issues))
	}
	if !issues[0].Dispatchable {
		t.Fatalf("信頼済みのリポジトリの issue が Dispatchable=false になっている: %q", issues[0].Identifier)
	}
	if issues[1].Dispatchable {
		t.Fatalf("未信頼のリポジトリの issue が Dispatchable=true になっている: %q", issues[1].Identifier)
	}
}

// 目的: 信頼の判定関数を渡さなかった場合（nil）は、すべてのリポジトリを信頼済みとして
// 扱うことを確認する（既存の呼び出しを壊さないための既定）。
// 与える情報: 判定関数に nil を渡した Adapter と、通常の issue 1件。
// 成功条件: Dispatchable が true であること。
func TestFetchIssuesByStates_信頼の判定関数がnilなら全て信頼済み扱い(t *testing.T) {
	nodes := []map[string]any{
		issueItemJSON(testIssueItemOpts{
			ItemID: "item-1", Status: "Ready",
			Owner: "someone", Repo: "unknown-repo", Number: 1, Title: "判定関数なし",
		}),
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nodes, false, ""))))
	a := newAdapterWithTrust(t, fs, nil)

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"})
	if err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}
	if len(issues) != 1 || !issues[0].Dispatchable {
		t.Fatalf("判定関数が nil なのに Dispatchable=false になっている: %+v", issues)
	}
}

// 目的: ID 指定の取り直しでも信頼の判定が効くことを確認する（dispatchable の判定が
// 取得経路によって食い違わないこと）。
// 与える情報: 未信頼のリポジトリの item 1件を nodes(ids:) で返す偽サーバ。
// 成功条件: エラーにならず、Dispatchable=false の Issue が1件返ること。
func TestFetchIssuesByIDs_未信頼のリポジトリはdispatchableがfalse(t *testing.T) {
	item := asProjectV2ItemNode(issueItemJSON(testIssueItemOpts{
		ItemID: "item-untrusted", Status: "In Progress",
		Owner: "someone", Repo: "untrusted", Number: 7, Title: "未信頼のリポジトリ",
	}))
	fs := newFakeGraphQLServer(t, single(dataResponse(byIDsPayload([]any{item}))))
	a := newAdapterWithTrust(t, fs, func(owner, repo string) bool { return false })

	issues, err := a.FetchIssuesByIDs(t.Context(), []string{"item-untrusted"})
	if err != nil {
		t.Fatalf("FetchIssuesByIDs が失敗した: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("件数が想定と違う: got %d, want 1", len(issues))
	}
	if issues[0].Dispatchable {
		t.Fatalf("未信頼のリポジトリの issue が Dispatchable=true になっている")
	}
}
