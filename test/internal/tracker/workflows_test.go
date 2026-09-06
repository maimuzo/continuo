package tracker_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/tracker"
)

// workflowsPayload は自動化を取るクエリへの偽サーバ応答を組み立てる。
//
// workflows: 載せる自動化（`{"number":…, "name":…, "enabled":…}` の並び）。
// **nil を渡すと `workflows` ごと落とす**（応答に入っていない状態）。
// 戻り値: 応答の data。
func workflowsPayload(workflows []map[string]any) map[string]any {
	project := map[string]any{}
	if workflows != nil {
		nodes := make([]any, 0, len(workflows))
		for _, w := range workflows {
			nodes = append(nodes, w)
		}
		project["workflows"] = map[string]any{"nodes": nodes}
	}
	return map[string]any{
		"repositoryOwner": map[string]any{"projectV2": project},
	}
}

// 目的: カンバンの自動化を、GitHub が返した順のまま返すことを確かめる
// （設計 3-32 の見出し語 `自動化`。issue #209）。
//
// 与える情報: 有効な自動化1件と無効な自動化1件を載せた偽サーバ応答。
// 成功条件: 2件とも、名前・番号・有効かどうかが GitHub の応答のまま返ること。
func TestFetchProjectWorkflows_応答に載った自動化をそのまま返す(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(workflowsPayload([]map[string]any{
		{"number": 5, "name": "Pull request linked to issue", "enabled": true},
		{"number": 2, "name": "Pull request merged", "enabled": false},
	}))))

	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	got, err := a.FetchProjectWorkflows(t.Context())
	if err != nil {
		t.Fatalf("FetchProjectWorkflows が失敗した: %v", err)
	}
	want := []tracker.ProjectWorkflow{
		{Number: 5, Name: "Pull request linked to issue", Enabled: true},
		{Number: 2, Name: "Pull request merged", Enabled: false},
	}
	if len(got) != len(want) {
		t.Fatalf("自動化の件数が違う: got %d, want %d（%+v）", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d件目が違う: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// 目的: 応答に `workflows` が入っていなければ nil を返すことを確かめる。
//
// **長さ0の「1件も無い」と取り違えてはならない。**取り違えると、
// `continuo doctor` が確かめていないことを `✓` で通す。
//
// 与える情報: `workflows` を含まない偽サーバ応答。
// 成功条件: エラーを返さず、nil を返すこと。
func TestFetchProjectWorkflows_応答に無ければnilを返す(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(workflowsPayload(nil))))

	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	got, err := a.FetchProjectWorkflows(t.Context())
	if err != nil {
		t.Fatalf("応答に無いだけなのにエラーを返した: %v", err)
	}
	if got != nil {
		t.Fatalf("応答に無いのに nil ではない: %+v", got)
	}
}

// 目的: 自動化が1件も無いカンバンでは、nil ではなく長さ0を返すことを確かめる。
//
// **「1件も無い」と「読めなかった」を分けるのがこの戻り値の役目である。**
//
// 与える情報: `workflows.nodes` が空の偽サーバ応答。
// 成功条件: nil ではない長さ0の並びを返すこと。
func TestFetchProjectWorkflows_1件も無いカンバンでは長さ0を返す(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(workflowsPayload([]map[string]any{}))))

	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	got, err := a.FetchProjectWorkflows(t.Context())
	if err != nil {
		t.Fatalf("FetchProjectWorkflows が失敗した: %v", err)
	}
	if got == nil {
		t.Fatal("1件も無いだけなのに nil を返した（「読めなかった」と区別が付かない）")
	}
	if len(got) != 0 {
		t.Fatalf("1件も無いはずなのに %d件 返った: %+v", len(got), got)
	}
}

// 目的: 自動化を取るクエリが、起動時の検査のクエリと別のリクエストであることを固定する
// （issue #209）。
//
// **混ぜてはならない。**起動時の検査のクエリは `allowNotFound` が偽で送るので、
// **GraphQL が `errors` を1件でも返した時点で Bootstrap ごと落ちる。**
// `workflows` を読む権限が無いトークンや、この field を持たない GitHub Enterprise Server では、
// **いままで起動していた continuo が起動しなくなる。**
// **この test が落ちるようになったら、その回帰が入ったということである。**
//
// 与える情報: 選択肢名が設定と一致する偽サーバ応答。
// 成功条件: Bootstrap が送ったクエリに `workflows` が1文字も入っていないこと。
func TestBootstrap_起動時の検査のクエリに自動化を混ぜない(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(bootstrapProjectPayload(testStatusOptions))))

	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	if err := a.Bootstrap(t.Context(), testTrackerConfig()); err != nil {
		t.Fatalf("Bootstrap が失敗した: %v", err)
	}

	for _, req := range fs.Requests() {
		if strings.Contains(req.Query, "workflows") {
			t.Fatalf("起動時の検査のクエリに自動化が混ざっている:\n%s", req.Query)
		}
	}
}
