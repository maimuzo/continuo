package tracker_test

import (
	"testing"
)

// 目的: ID 指定の取り直しで archive 済みの item が返ってきたら、「もう見えない」として
// 結果から省くことを確認する。
// **候補の取得（items(...)）は archive 済みを返さないのに、nodes(ids:) はそのまま返す。**
// ここで省かないと、人間が archive した issue を「まだ作業中の状態にある」と誤認し続ける。
// 与える情報: 通常の item 1件と、isArchived が真の item 1件。
// 成功条件: エラーにならず（archive 済みは「壊れている」ではないのでエラーにしない）、
// 結果が通常の item 1件だけであること。
func TestFetchIssuesByIDs_archive済みのitemは省く(t *testing.T) {
	alive := asProjectV2ItemNode(issueItemJSON(testIssueItemOpts{
		ItemID: "item-alive", Status: "In Progress",
		Owner: "octocat", Repo: "hello-world", Number: 10, Title: "生きている",
	}))
	archived := asProjectV2ItemNode(issueItemJSON(testIssueItemOpts{
		ItemID: "item-archived", Status: "In Progress", Archived: true,
		Owner: "octocat", Repo: "hello-world", Number: 11, Title: "archive 済み",
	}))
	fs := newFakeGraphQLServer(t, single(dataResponse(byIDsPayload([]any{alive, archived}))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByIDs(t.Context(), []string{"item-alive", "item-archived"})
	if err != nil {
		t.Fatalf("archive 済みの item があるだけでエラーになった: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("件数が想定と違う: got %d, want 1（archive 済みは省かれるはず）: %v", len(issues), identifiersOf(issues))
	}
	if issues[0].ID != "item-alive" {
		t.Fatalf("残った item が想定と違う: %q", issues[0].ID)
	}
}

// 目的: archive 済みの item は候補の一覧でも省くことを確認する
// （items(...) の既定 archivedStates に頼りきらず、返ってきても弾くこと）。
// 与える情報: 通常の item 1件と、isArchived が真の item 1件を返す偽サーバ。
// 成功条件: エラーにならず、結果が通常の item 1件だけであること。
func TestFetchIssuesByStates_archive済みのitemは省く(t *testing.T) {
	nodes := []map[string]any{
		issueItemJSON(testIssueItemOpts{
			ItemID: "item-alive", Status: "Ready",
			Owner: "octocat", Repo: "hello-world", Number: 1, Title: "生きている",
		}),
		issueItemJSON(testIssueItemOpts{
			ItemID: "item-archived", Status: "Ready", Archived: true,
			Owner: "octocat", Repo: "hello-world", Number: 2, Title: "archive 済み",
		}),
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nodes, false, ""))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"})
	if err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}
	if len(issues) != 1 || issues[0].ID != "item-alive" {
		t.Fatalf("archive 済みの item が候補に残っている: %v", identifiersOf(issues))
	}
}

// 目的: UpdateStatus が archive 済みの item へは書き込まないことを確認する
// （書く前の取り直しで「もう見えない」と判定されるため）。
// 与える情報: Bootstrap 応答 → 取り直しで archive 済みの item を返す偽サーバ。
// 成功条件: 書き込んでいない（Reached が偽）ことと、ミューテーションのリクエストが送られていない
// （リクエストが Bootstrap と取り直しの2件だけである）こと。
func TestUpdateStatus_archive済みのitemには書き込まない(t *testing.T) {
	archived := asProjectV2ItemNode(issueItemJSON(testIssueItemOpts{
		ItemID: "item-archived", Status: "In Progress", Archived: true,
		Owner: "octocat", Repo: "hello-world", Number: 11, Title: "archive 済み",
	}))
	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		if n == 1 {
			return dataResponse(bootstrapProjectPayload(testStatusOptions))
		}
		return dataResponse(byIDsPayload([]any{archived}))
	})
	a := newBootstrappedAdapter(t, fs)

	moved, err := a.UpdateStatus(t.Context(), "item-archived", "In Review", []string{"Ready", "In Progress"})
	if err != nil {
		t.Fatalf("UpdateStatus が失敗した: %v", err)
	}
	if moved.Reached {
		t.Fatalf("archive 済みの item へ書き込んでしまった")
	}
	if fs.RequestCount() != 2 {
		t.Fatalf("リクエスト件数が想定と違う: got %d, want 2（Bootstrap と取り直しだけで、書き込みは送らないはず）", fs.RequestCount())
	}
}
