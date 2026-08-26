package tracker_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/tracker"
)

// 目的: UpdateStatus が書き込む前に必ず ID 指定で item を取り直し、取り直した結果の
// State が blockedStates に含まれていない場合に書き込むことを確認する（設計 3-4 / 4-1）。
// 与える情報: Bootstrap 応答 → 取り直し応答（State = "In Progress"、blockedStates に含まれない）
// → 書き込みミューテーション応答、の順で返す偽サーバ。
// 成功条件: 3リクエストが順番に送られること（1: Bootstrap, 2: 取り直し, 3: 書き込み）。
// 3件目のリクエストが updateProjectV2ItemFieldValue を呼んでいること。
// UpdateStatus が Reached / Wrote ともに真で、Previous に取り直した "In Progress" を
// 載せて返すこと（エラーは nil）。
func TestUpdateStatus_取り直してから書き込む(t *testing.T) {
	refetched := asProjectV2ItemNode(issueItemJSON(testIssueItemOpts{
		ItemID: "item-1", Status: "In Progress", Owner: "octocat", Repo: "hello-world", Number: 1, Title: "t",
	}))

	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		switch n {
		case 1:
			return dataResponse(bootstrapProjectPayload(testStatusOptions))
		case 2:
			if !strings.Contains(req.Query, "nodes(ids:") {
				t.Errorf("2回目のリクエストが ID 指定の取り直しになっていない: %s", req.Query)
			}
			return dataResponse(byIDsPayload([]any{refetched}))
		case 3:
			if !strings.Contains(req.Query, "updateProjectV2ItemFieldValue") {
				t.Errorf("3回目のリクエストが Status の書き込みになっていない: %s", req.Query)
			}
			return dataResponse(map[string]any{
				"updateProjectV2ItemFieldValue": map[string]any{
					"projectV2Item": map[string]any{"id": "item-1"},
				},
			})
		default:
			t.Errorf("想定より多くのリクエストが送られた（%d回目）", n)
			return dataResponse(nil)
		}
	})

	a := newBootstrappedAdapter(t, fs)

	moved, err := a.UpdateStatus(t.Context(), "item-1", "In Review", []string{"Done"})
	if err != nil {
		t.Fatalf("UpdateStatus が失敗した: %v", err)
	}
	if !moved.Reached {
		t.Fatalf("Reached が false になっている（書き込まれたはず）")
	}
	if !moved.Wrote {
		t.Fatalf("Wrote が false になっている（mutation を送ったはず）")
	}
	if moved.Previous != "In Progress" {
		t.Fatalf("Previous が取り直した値になっていない: got %q, want \"In Progress\"", moved.Previous)
	}
	if fs.RequestCount() != 3 {
		t.Fatalf("リクエスト件数が想定と違う: got %d, want 3", fs.RequestCount())
	}
}

// countStatusMutations は偽サーバが受け取ったリクエストのうち、Status の書き込み
// （updateProjectV2ItemFieldValue）が何件あったかを数える。
// **「リクエストの総数」ではなく「書き込みの mutation の件数」で数える。**取り直しの
// クエリは飛んでよいので、総数では「書きに行かなかったこと」を確かめられない。
func countStatusMutations(fs *fakeGraphQLServer) int {
	n := 0
	for _, req := range fs.Requests() {
		if strings.Contains(req.Query, "updateProjectV2ItemFieldValue") {
			n++
		}
	}
	return n
}

// 目的: 取り直した値が既に目的の値と同じなら、書き込みの mutation を1回も送らないことを
// 確認する（GitHub は同じ値の書き込みを timeline に残さないので、送っても issue には
// 何も現れない）。
// 与える情報: 取り直し応答の State が "In Review"（目的の値と同じ）である偽サーバ。
// 成功条件: 書き込みミューテーションのリクエストが送られないこと（Bootstrap + 取り直しの
// 2リクエストだけで終わること）。UpdateStatus が Reached だけ真、Wrote は偽を返すこと
// （**目的の Status にはなっているが、ボードは動いていない**）。
func TestUpdateStatus_既に同じ値なら書きに行かない(t *testing.T) {
	refetched := asProjectV2ItemNode(issueItemJSON(testIssueItemOpts{
		ItemID: "item-1", Status: "In Review", Owner: "octocat", Repo: "hello-world", Number: 1, Title: "t",
	}))

	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		switch n {
		case 1:
			return dataResponse(bootstrapProjectPayload(testStatusOptions))
		case 2:
			return dataResponse(byIDsPayload([]any{refetched}))
		default:
			t.Errorf("既に同じ値なのに書き込みリクエストが送られた（%d回目）: %s", n, req.Query)
			return dataResponse(nil)
		}
	})

	a := newBootstrappedAdapter(t, fs)

	moved, err := a.UpdateStatus(t.Context(), "item-1", "In Review", []string{"Done"})
	if err != nil {
		t.Fatalf("UpdateStatus がエラーを返した: %v", err)
	}
	if !moved.Reached {
		t.Fatalf("Reached が false になっている（書かなくても目的の Status にはなっているので true のはず）")
	}
	if moved.Wrote {
		t.Fatalf("Wrote が true になっている（mutation は送っていないはず）")
	}
	if got := countStatusMutations(fs); got != 0 {
		t.Fatalf("書き込みの mutation が飛んでしまった: got %d件, want 0件", got)
	}
	if fs.RequestCount() != 2 {
		t.Fatalf("書き込みリクエストが送られてしまった: リクエスト件数 got %d, want 2", fs.RequestCount())
	}
}

// 目的: 大文字小文字と前後の空白しか違わない値も「同じ」とみなして書きに行かないことを
// 確認する（foldStatus で比較する。SPEC.md 11.3）。
// 与える情報: 取り直し応答の State が "  in review  "（目的の値 "In Review" と綴りだけ違う）
// である偽サーバ。
// 成功条件: 書き込みミューテーションのリクエストが送られないこと。
// UpdateStatus が Reached だけ真、Wrote は偽を返すこと。
func TestUpdateStatus_大文字小文字と空白の違いは同じ値とみなす(t *testing.T) {
	refetched := asProjectV2ItemNode(issueItemJSON(testIssueItemOpts{
		ItemID: "item-1", Status: "  in review  ", Owner: "octocat", Repo: "hello-world", Number: 1, Title: "t",
	}))

	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		switch n {
		case 1:
			return dataResponse(bootstrapProjectPayload(testStatusOptions))
		case 2:
			return dataResponse(byIDsPayload([]any{refetched}))
		default:
			t.Errorf("綴りだけが違う同じ値なのに書き込みリクエストが送られた（%d回目）: %s", n, req.Query)
			return dataResponse(nil)
		}
	})

	a := newBootstrappedAdapter(t, fs)

	moved, err := a.UpdateStatus(t.Context(), "item-1", "In Review", []string{"Done"})
	if err != nil {
		t.Fatalf("UpdateStatus がエラーを返した: %v", err)
	}
	if !moved.Reached {
		t.Fatalf("Reached が false になっている（書かなくても目的の Status にはなっているので true のはず）")
	}
	if moved.Wrote {
		t.Fatalf("Wrote が true になっている（mutation は送っていないはず）")
	}
	if got := countStatusMutations(fs); got != 0 {
		t.Fatalf("書き込みの mutation が飛んでしまった: got %d件, want 0件", got)
	}
	if fs.RequestCount() != 2 {
		t.Fatalf("書き込みリクエストが送られてしまった: リクエスト件数 got %d, want 2", fs.RequestCount())
	}
}

// 目的: 取り直した値が目的の値と違う場合は、今までどおり書きに行くことを確認する。
// **「同じなら書かない」を入れたせいで、違うときまで書かなくなっていないか**を見る。
// 与える情報: 取り直し応答の State が "In Progress"（目的の値は "In Review"）である偽サーバ。
// 成功条件: 書き込みの mutation がちょうど1件飛ぶこと。UpdateStatus が Reached / Wrote
// ともに真で、Previous に取り直した "In Progress" を載せて返すこと。
func TestUpdateStatus_値が違えば書きに行く(t *testing.T) {
	refetched := asProjectV2ItemNode(issueItemJSON(testIssueItemOpts{
		ItemID: "item-1", Status: "In Progress", Owner: "octocat", Repo: "hello-world", Number: 1, Title: "t",
	}))

	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		switch n {
		case 1:
			return dataResponse(bootstrapProjectPayload(testStatusOptions))
		case 2:
			return dataResponse(byIDsPayload([]any{refetched}))
		case 3:
			return dataResponse(map[string]any{
				"updateProjectV2ItemFieldValue": map[string]any{
					"projectV2Item": map[string]any{"id": "item-1"},
				},
			})
		default:
			t.Errorf("想定より多くのリクエストが送られた（%d回目）: %s", n, req.Query)
			return dataResponse(nil)
		}
	})

	a := newBootstrappedAdapter(t, fs)

	moved, err := a.UpdateStatus(t.Context(), "item-1", "In Review", []string{"Done"})
	if err != nil {
		t.Fatalf("UpdateStatus が失敗した: %v", err)
	}
	if !moved.Reached {
		t.Fatalf("Reached が false になっている（書き込まれたはず）")
	}
	if !moved.Wrote {
		t.Fatalf("Wrote が false になっている（mutation を送ったはず）")
	}
	if moved.Previous != "In Progress" {
		t.Fatalf("Previous が取り直した値になっていない: got %q, want \"In Progress\"", moved.Previous)
	}
	if got := countStatusMutations(fs); got != 1 {
		t.Fatalf("書き込みの mutation の件数が想定と違う: got %d件, want 1件", got)
	}
}

// 目的: 取り直した結果が blockedStates に含まれている（＝エージェントが自分で gh を叩いて
// 既に Done へ動かしていた等）場合は、書き込まないことを確認する（設計 3-4:
// 「取り直した結果が terminal_states に入っていたら書かない」）。
// 与える情報: 取り直し応答の State が "Done"（blockedStates=["Done"] に含まれる）である偽サーバ。
// 成功条件: 書き込みミューテーションのリクエストが送られないこと（Bootstrap + 取り直しの
// 2リクエストだけで終わること）。UpdateStatus が Reached / Wrote ともに偽を返すこと
// （エラーではない）。
func TestUpdateStatus_終了状態なら書かない(t *testing.T) {
	refetched := asProjectV2ItemNode(issueItemJSON(testIssueItemOpts{
		ItemID: "item-1", Status: "Done", Owner: "octocat", Repo: "hello-world", Number: 1, Title: "t",
	}))

	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		switch n {
		case 1:
			return dataResponse(bootstrapProjectPayload(testStatusOptions))
		case 2:
			return dataResponse(byIDsPayload([]any{refetched}))
		default:
			t.Errorf("終了状態なのに書き込みリクエストが送られた（%d回目）: %s", n, req.Query)
			return dataResponse(nil)
		}
	})

	a := newBootstrappedAdapter(t, fs)

	moved, err := a.UpdateStatus(t.Context(), "item-1", "In Review", []string{"Done"})
	if err != nil {
		t.Fatalf("UpdateStatus がエラーを返した（エラーではなく書かないだけのはず）: %v", err)
	}
	if moved.Reached || moved.Wrote {
		t.Fatalf("Reached / Wrote が true になっている（書き込まれてはいけない）: %+v", moved)
	}
	if fs.RequestCount() != 2 {
		t.Fatalf("書き込みリクエストが送られてしまった: リクエスト件数 got %d, want 2", fs.RequestCount())
	}
}

// 目的: 取り直した結果、item がもう見えない（nodes(ids:) が null を返す）場合も
// 書き込まないことを確認する。
// 与える情報: 取り直し応答が null の偽サーバ。
// 成功条件: 書き込みリクエストが送られないこと。UpdateStatus が Reached / Wrote ともに
// 偽を返し、Previous が空であること。
func TestUpdateStatus_itemが見えなくなっていたら書かない(t *testing.T) {
	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		switch n {
		case 1:
			return dataResponse(bootstrapProjectPayload(testStatusOptions))
		case 2:
			return dataResponse(byIDsPayload([]any{nil}))
		default:
			t.Errorf("item が見えないのに書き込みリクエストが送られた（%d回目）", n)
			return dataResponse(nil)
		}
	})

	a := newBootstrappedAdapter(t, fs)

	moved, err := a.UpdateStatus(t.Context(), "item-1", "In Review", []string{"Ready", "In Progress"})
	if err != nil {
		t.Fatalf("UpdateStatus がエラーを返した: %v", err)
	}
	if moved.Reached || moved.Wrote {
		t.Fatalf("Reached / Wrote が true になっている: %+v", moved)
	}
	if moved.Previous != "" {
		t.Fatalf("item が見えないのに Previous が入っている: %q", moved.Previous)
	}
	if fs.RequestCount() != 2 {
		t.Fatalf("リクエスト件数が想定と違う: got %d, want 2", fs.RequestCount())
	}
}

// 目的: Bootstrap を呼ばずに UpdateStatus を呼ぶとエラーになることを確認する
// （書き込みに要る project ID・Status フィールドの ID が未解決のため）。
// 与える情報: Bootstrap を呼んでいない Adapter。
// 成功条件: UpdateStatus がエラーを返し、カテゴリが CategoryInvalidConfig であること。
// 偽サーバへ一切リクエストが送られないこと。
func TestUpdateStatus_Bootstrap未実行だとエラーになる(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(nil)))
	a := newAdapterForFetch(t, fs)

	_, err := a.UpdateStatus(t.Context(), "item-1", "In Review", []string{"Ready", "In Progress"})
	if err == nil {
		t.Fatalf("Bootstrap 未実行なのに UpdateStatus が成功した")
	}
	if !tracker.IsCategory(err, tracker.CategoryInvalidConfig) {
		t.Fatalf("エラーのカテゴリが CategoryInvalidConfig ではない: %v", err)
	}
	if fs.RequestCount() != 0 {
		t.Fatalf("Bootstrap 未実行なのにリクエストが送られた: %d件", fs.RequestCount())
	}
}
