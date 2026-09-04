package tracker_test

import (
	"testing"

	"github.com/maimuzo/continuo/internal/tracker"
)

// bootstrapPayloadWithWorkflows は、Bootstrap の応答へカンバンの自動化を載せたものを作る。
//
// **`bootstrapProjectPayload` を作り直さない。**あちらは他の test が使っている形なので、
// 返ってきた map の `projectV2` へ1つ足すだけにする。
//
// t: 呼び出し元のテスト。
// workflows: 載せる自動化（`{"number":…, "name":…, "enabled":…}` の並び）。
// 戻り値: 偽サーバの応答の data。
func bootstrapPayloadWithWorkflows(t *testing.T, workflows []map[string]any) map[string]any {
	t.Helper()
	payload := bootstrapProjectPayload(testStatusOptions)
	owner, ok := payload["repositoryOwner"].(map[string]any)
	if !ok {
		t.Fatalf("偽の応答の形が変わっています: %+v", payload)
	}
	project, ok := owner["projectV2"].(map[string]any)
	if !ok {
		t.Fatalf("偽の応答の形が変わっています: %+v", owner)
	}
	nodes := make([]any, 0, len(workflows))
	for _, w := range workflows {
		nodes = append(nodes, w)
	}
	project["workflows"] = map[string]any{"nodes": nodes}
	return payload
}

// 目的: Bootstrap の応答に載ったカンバンの自動化を、`ProjectWorkflows` がそのまま返すことを
// 確かめる（設計 3-32 の見出し語 `自動化`。issue #209）。
//
// **`continuo doctor` だけが使う値だが、起動時の検査の応答から取る。**
// doctor 専用のクエリを別に足すと、「doctor がカンバンを1回読む」と書いてある3箇所
// （設計 3-32・docs/plans/impl/08_doctor.md・`test/internal/doctor` のクエリの並びの検査）が
// 食い違うためである。
//
// 与える情報: 有効な自動化1件と無効な自動化1件を載せた偽サーバ応答。
// 成功条件: 2件とも、名前・番号・有効かどうかが GitHub の応答のまま返ること。
func TestProjectWorkflows_応答に載った自動化をそのまま返す(t *testing.T) {
	payload := bootstrapPayloadWithWorkflows(t, []map[string]any{
		{"number": 5, "name": "Pull request linked to issue", "enabled": true},
		{"number": 2, "name": "Pull request merged", "enabled": false},
	})
	fs := newFakeGraphQLServer(t, single(dataResponse(payload)))

	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	if err := a.Bootstrap(t.Context(), testTrackerConfig()); err != nil {
		t.Fatalf("Bootstrap が失敗した: %v", err)
	}

	got := a.ProjectWorkflows()
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
// 与える情報: `workflows` を含まない偽サーバ応答（既存の `bootstrapProjectPayload`）。
// 成功条件: `ProjectWorkflows` が nil を返すこと。
func TestProjectWorkflows_応答に無ければnilを返す(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(bootstrapProjectPayload(testStatusOptions))))

	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	if err := a.Bootstrap(t.Context(), testTrackerConfig()); err != nil {
		t.Fatalf("Bootstrap が失敗した: %v", err)
	}

	if got := a.ProjectWorkflows(); got != nil {
		t.Fatalf("応答に無いのに nil ではない: %+v", got)
	}
}

// 目的: 自動化が1件も無いカンバンでは、nil ではなく長さ0を返すことを確かめる。
//
// **「1件も無い」と「読めなかった」を分けるのがこの型の役目である。**
//
// 与える情報: `workflows.nodes` が空の偽サーバ応答。
// 成功条件: `ProjectWorkflows` が nil ではない長さ0の並びを返すこと。
func TestProjectWorkflows_1件も無いカンバンでは長さ0を返す(t *testing.T) {
	payload := bootstrapPayloadWithWorkflows(t, nil)
	fs := newFakeGraphQLServer(t, single(dataResponse(payload)))

	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	if err := a.Bootstrap(t.Context(), testTrackerConfig()); err != nil {
		t.Fatalf("Bootstrap が失敗した: %v", err)
	}

	got := a.ProjectWorkflows()
	if got == nil {
		t.Fatal("1件も無いだけなのに nil を返した（「読めなかった」と区別が付かない）")
	}
	if len(got) != 0 {
		t.Fatalf("1件も無いはずなのに %d件 返った: %+v", len(got), got)
	}
}

// 目的: Bootstrap を通していなければ nil を返すことを確かめる。
//
// 与える情報: Bootstrap を呼んでいないアダプタ。
// 成功条件: `ProjectWorkflows` が nil を返すこと。
func TestProjectWorkflows_Bootstrapを通していなければnilを返す(t *testing.T) {
	a, err := tracker.NewAdapter(testTrackerConfig(), "http://127.0.0.1:1", "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	if got := a.ProjectWorkflows(); got != nil {
		t.Fatalf("Bootstrap を通していないのに nil ではない: %+v", got)
	}
}
