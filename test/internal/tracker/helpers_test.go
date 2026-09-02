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
	ItemID   string
	Status   string // 空文字なら Status 未設定（fieldValueByName が null）として組み立てる
	Archived bool   // true なら isArchived が真の item（ボード上でもう見えない）を組み立てる
	Owner    string
	Repo     string
	Number   int
	Title    string
	Body     string
	URL      string
	Labels   []string
	// LinkedBranch は「issue と同じリポジトリの branch が、ちょうど1本リンクされている」
	// 形を組み立てる近道である（`totalCount` は 1、`ref.repository.nameWithOwner` は
	// Owner/Repo）。**別のリポジトリを指す形や2本以上の形は LinkedBranches で組む。**
	LinkedBranch string
	// LinkedBranches は linkedBranches の nodes をそのまま指定する（設計 3-22d の
	// 「別のリポジトリを指すリンク」「2本以上」を組み立てるためにある）。
	// **指定したら LinkedBranch は無視される。**linkedBranchNodeJSON で1件ずつ作る。
	LinkedBranches []map[string]any
	// LinkedBranchTotalCount は linkedBranches の `totalCount` である。
	// **0 なら nodes の件数を使う。**「窓（first: 5）の外に6本目がある」形を作るときだけ
	// 明示する。
	LinkedBranchTotalCount int
	CommentCount           int
	AssigneeID             string
	AssigneeLogin          string
	BlockedBy              []map[string]any
	// StatusEvents は timelineItems（ProjectV2ItemStatusChangedEvent）の nodes である
	// （設計 3-54）。**nil なら `timelineItems` そのものを付けない**
	// （候補の取得のクエリが要求していない状態の再現）。statusEventJSON で1件ずつ作る。
	StatusEvents []map[string]any
}

// statusEventJSON は ProjectV2ItemStatusChangedEvent 1件分の JSON を組み立てる（設計 3-54）。
//
// createdAt: イベントの時刻（RFC3339）。
// status: このイベントで書き込まれた Status 名。
// actorType: `actor.__typename`（`Bot` なら自動化、`User` なら人間か continuo 自身）。
// 空文字なら actor そのものを付けない。
// actorLogin: `actor.login`。
// wasAutomated: `wasAutomated` の値。**組み込みの自動化でも false が返る**（設計 2-6）。
// projectNumber: そのイベントが起きたボードの番号。
func statusEventJSON(
	createdAt, status, actorType, actorLogin string, wasAutomated bool, projectNumber int,
) map[string]any {
	ev := map[string]any{
		"createdAt":    createdAt,
		"status":       status,
		"wasAutomated": wasAutomated,
		"project":      map[string]any{"number": projectNumber},
	}
	if actorType != "" {
		ev["actor"] = map[string]any{"__typename": actorType, "login": actorLogin}
	}
	return ev
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

	linkedBranchNodes := o.LinkedBranches
	if linkedBranchNodes == nil && o.LinkedBranch != "" {
		linkedBranchNodes = []map[string]any{
			linkedBranchNodeJSON(o.LinkedBranch, o.Owner+"/"+o.Repo),
		}
	}
	linkedBranchTotal := o.LinkedBranchTotalCount
	if linkedBranchTotal == 0 {
		linkedBranchTotal = len(linkedBranchNodes)
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
		"labels":    map[string]any{"nodes": labelNodes},
		"assignees": map[string]any{"nodes": assigneeNodes},
		"blockedBy": map[string]any{"nodes": blockedBy},
		"linkedBranches": map[string]any{
			"totalCount": linkedBranchTotal,
			"nodes":      linkedBranchNodes,
		},
		"comments": map[string]any{"totalCount": o.CommentCount},
	}
	if o.StatusEvents != nil {
		content["timelineItems"] = map[string]any{"nodes": o.StatusEvents}
	}

	return map[string]any{
		"id":               o.ItemID,
		"isArchived":       o.Archived,
		"fieldValueByName": fieldValue,
		"content":          content,
	}
}

// linkedBranchNodeJSON は linkedBranches の node 1件分の JSON を組み立てる（設計 3-22d）。
//
// name: branch の名前（`work/issue-42`）。
// nameWithOwner: その branch が在るリポジトリ（`octocat/hello-world`）。
// **空文字なら `repository` そのものを付けない**（リポジトリ名が取れなかった応答の再現）。
func linkedBranchNodeJSON(name, nameWithOwner string) map[string]any {
	ref := map[string]any{"name": name}
	if nameWithOwner != "" {
		ref["repository"] = map[string]any{"nameWithOwner": nameWithOwner}
	}
	return map[string]any{"ref": ref}
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
