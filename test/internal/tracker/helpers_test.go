package tracker_test

import (
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/tracker"
)

// testStatusOptions は project #3 の実測構成（設計 4-1）に合わせた Status の選択肢である。
// テストはこれを偽サーバの Bootstrap 応答に使う。
var testStatusOptions = []map[string]any{
	{"id": "opt-icebox", "name": "Ice Box"},
	{"id": "opt-ready", "name": "Ready"},
	{"id": "opt-inprogress", "name": "In Progress"},
	{"id": "opt-blocked", "name": "Blocked"},
	{"id": "opt-inreview", "name": "In Review"},
	{"id": "opt-done", "name": "Done"},
}

// testTrackerConfig は設計 5-2 の設定例に合わせた最小限の tracker 設定を返す。
func testTrackerConfig() config.TrackerConfig {
	working := "In Review"
	blocked := "Blocked"
	return config.TrackerConfig{
		Kind: tracker.KindGitHubProjectsV2,
		Provider: config.TrackerProviderConfig{
			Owner:         "octocat",
			ProjectNumber: 3,
			StatusField:   "Status",
			TokenSource:   "gh_auth",
			Comments: config.TrackerProviderCommentsConfig{
				Max:   50,
				Order: "oldest_first",
			},
		},
		Comments: config.TrackerCommentsConfig{
			Marker:     "<!-- continuo:agent -->",
			SelfMarker: "<!-- continuo:self -->",
		},
		RequiredLabels: nil,
		ActiveStates:   []string{"Ready", "In Progress"},
		TerminalStates: []string{"Done"},
		DispatchState:  "Ready",
		FailureState:   "Blocked",
		StatusSignalMap: map[string]*string{
			"review":  &working,
			"blocked": &blocked,
			"working": nil,
		},
	}
}

// bootstrapProjectPayload は Bootstrap 用の偽サーバ応答（`data.repositoryOwner.projectV2`）を
// 組み立てる。options を差し替えれば「選択肢名が食い違っている」ケースも作れる。
func bootstrapProjectPayload(options []map[string]any) map[string]any {
	return map[string]any{
		"repositoryOwner": map[string]any{
			"projectV2": map[string]any{
				"id": "PVT_test",
				"field": map[string]any{
					"__typename": "ProjectV2SingleSelectField",
					"id":         "PVTSSF_test",
					"options":    options,
				},
			},
		},
	}
}

// testIssueItemOpts は issueItemJSON が組み立てる project item（content が Issue）の
// パラメータである。ゼロ値のフィールドはそれぞれ「無し」として扱われる。
type testIssueItemOpts struct {
	ItemID        string
	Status        string // 空文字なら Status 未設定（fieldValueByName が null）として組み立てる
	Archived      bool   // true なら isArchived が真の item（ボード上でもう見えない）を組み立てる
	Owner         string
	Repo          string
	Number        int
	Title         string
	Body          string
	URL           string
	Labels        []string
	LinkedBranch  string
	CommentCount  int
	AssigneeID    string
	AssigneeLogin string
	BlockedBy     []map[string]any
}

// issueItemJSON は content が Issue である project item 1件分の、偽サーバが返す JSON
// （rawItem の形）を組み立てる。
func issueItemJSON(o testIssueItemOpts) map[string]any {
	var fieldValue any
	if o.Status != "" {
		fieldValue = map[string]any{
			"__typename": "ProjectV2ItemFieldSingleSelectValue",
			"name":       o.Status,
			"optionId":   "opt-" + o.Status,
		}
	}

	labelNodes := make([]map[string]any, len(o.Labels))
	for i, l := range o.Labels {
		labelNodes[i] = map[string]any{"name": l}
	}

	var linkedBranchNodes []map[string]any
	if o.LinkedBranch != "" {
		linkedBranchNodes = []map[string]any{{"ref": map[string]any{"name": o.LinkedBranch}}}
	}

	var assigneeNodes []map[string]any
	if o.AssigneeID != "" {
		assigneeNodes = []map[string]any{{"id": o.AssigneeID, "login": o.AssigneeLogin}}
	}

	blockedBy := o.BlockedBy
	if blockedBy == nil {
		blockedBy = []map[string]any{}
	}

	content := map[string]any{
		"__typename": "Issue",
		"id":         "ISSUENODE_" + o.ItemID,
		"number":     o.Number,
		"title":      o.Title,
		"body":       o.Body,
		"url":        o.URL,
		"state":      "OPEN",
		"createdAt":  "2026-08-01T00:00:00Z",
		"updatedAt":  "2026-08-02T00:00:00Z",
		"repository": map[string]any{
			"nameWithOwner":    o.Owner + "/" + o.Repo,
			"defaultBranchRef": map[string]any{"name": "main"},
		},
		"labels":         map[string]any{"nodes": labelNodes},
		"assignees":      map[string]any{"nodes": assigneeNodes},
		"blockedBy":      map[string]any{"nodes": blockedBy},
		"linkedBranches": map[string]any{"nodes": linkedBranchNodes},
		"comments":       map[string]any{"totalCount": o.CommentCount},
	}

	return map[string]any{
		"id":               o.ItemID,
		"isArchived":       o.Archived,
		"fieldValueByName": fieldValue,
		"content":          content,
	}
}

// draftItemJSON は content が DraftIssue である project item 1件分の JSON を組み立てる。
func draftItemJSON(itemID, status, title, body string) map[string]any {
	var fieldValue any
	if status != "" {
		fieldValue = map[string]any{
			"__typename": "ProjectV2ItemFieldSingleSelectValue",
			"name":       status,
			"optionId":   "opt-" + status,
		}
	}
	return map[string]any{
		"id":               itemID,
		"isArchived":       false,
		"fieldValueByName": fieldValue,
		"content": map[string]any{
			"__typename": "DraftIssue",
			"id":         "DRAFTNODE_" + itemID,
			"title":      title,
			"body":       body,
			"createdAt":  "2026-08-01T00:00:00Z",
			"updatedAt":  "2026-08-02T00:00:00Z",
			"assignees":  map[string]any{"nodes": []map[string]any{}},
		},
	}
}

// candidateItemsPayload は fetch_issues_by_states 用の偽サーバ応答
// （`data.repositoryOwner.projectV2.items`）を組み立てる。
func candidateItemsPayload(nodes []map[string]any, hasNextPage bool, endCursor string) map[string]any {
	return map[string]any{
		"repositoryOwner": map[string]any{
			"projectV2": map[string]any{
				"items": map[string]any{
					"pageInfo": map[string]any{
						"hasNextPage": hasNextPage,
						"endCursor":   endCursor,
					},
					"nodes": nodes,
				},
			},
		},
	}
}

// byIDsPayload は fetch_issues_by_ids 用の偽サーバ応答（`data.nodes`）を組み立てる。
// nodes の各要素は issueItemJSON / draftItemJSON の戻り値に "__typename": "ProjectV2Item" を
// 足したもの、または「見つからない」ことを表す nil のいずれかにすること。
func byIDsPayload(nodes []any) map[string]any {
	return map[string]any{"nodes": nodes}
}

// asProjectV2ItemNode は issueItemJSON / draftItemJSON の戻り値に、fetch_issues_by_ids の
// nodes(ids:) クエリが返す __typename を足す。
func asProjectV2ItemNode(item map[string]any) map[string]any {
	out := make(map[string]any, len(item)+1)
	for k, v := range item {
		out[k] = v
	}
	out["__typename"] = "ProjectV2Item"
	return out
}

// newAdapterWithTrust は信頼の判定関数を渡した Adapter を（Bootstrap を呼ばずに）作る。
//
// t: 呼び出し元のテスト。
// fs: 接続先の偽サーバ。
// trusted: `<owner>/<repo>` が信頼登録されているかを返す関数。nil なら全て信頼済み扱い。
func newAdapterWithTrust(t *testing.T, fs *fakeGraphQLServer, trusted tracker.RepoTrustFunc) *tracker.Adapter {
	t.Helper()
	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, nil, trusted)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	return a
}

// newBootstrappedAdapter は偽サーバに対して Bootstrap まで済ませた Adapter を返す。
// 個々のテストは、これに続けて追加の responder を積む代わりに、
// 呼び出し回数（n）で分岐する responder を自前で書くことを想定している。
func newBootstrappedAdapter(
	t *testing.T,
	fs *fakeGraphQLServer,
) *tracker.Adapter {
	t.Helper()
	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	if err := a.Bootstrap(t.Context(), testTrackerConfig()); err != nil {
		t.Fatalf("Bootstrap が失敗した: %v", err)
	}
	return a
}
