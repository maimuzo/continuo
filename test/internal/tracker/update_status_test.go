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
// UpdateStatus が (true, nil) を返すこと。
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

	written, err := a.UpdateStatus(t.Context(), "item-1", "In Review", []string{"Done"})
	if err != nil {
		t.Fatalf("UpdateStatus が失敗した: %v", err)
	}
	if !written {
		t.Fatalf("written が false になっている（書き込まれたはず）")
	}
	if fs.RequestCount() != 3 {
		t.Fatalf("リクエスト件数が想定と違う: got %d, want 3", fs.RequestCount())
	}
}

// 目的: 取り直した結果が blockedStates に含まれている（＝エージェントが自分で gh を叩いて
// 既に Done へ動かしていた等）場合は、書き込まないことを確認する（設計 3-4:
// 「取り直した結果が terminal_states に入っていたら書かない」）。
// 与える情報: 取り直し応答の State が "Done"（blockedStates=["Done"] に含まれる）である偽サーバ。
// 成功条件: 書き込みミューテーションのリクエストが送られないこと（Bootstrap + 取り直しの
// 2リクエストだけで終わること）。UpdateStatus が (false, nil) を返すこと（エラーではない）。
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

	written, err := a.UpdateStatus(t.Context(), "item-1", "In Review", []string{"Done"})
	if err != nil {
		t.Fatalf("UpdateStatus がエラーを返した（エラーではなく書かないだけのはず）: %v", err)
	}
	if written {
		t.Fatalf("written が true になっている（書き込まれてはいけない）")
	}
	if fs.RequestCount() != 2 {
		t.Fatalf("書き込みリクエストが送られてしまった: リクエスト件数 got %d, want 2", fs.RequestCount())
	}
}

// 目的: 取り直した結果、item がもう見えない（nodes(ids:) が null を返す）場合も
// 書き込まないことを確認する。
// 与える情報: 取り直し応答が null の偽サーバ。
// 成功条件: 書き込みリクエストが送られないこと。UpdateStatus が (false, nil) を返すこと。
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

	written, err := a.UpdateStatus(t.Context(), "item-1", "In Review", []string{"Ready", "In Progress"})
	if err != nil {
		t.Fatalf("UpdateStatus がエラーを返した: %v", err)
	}
	if written {
		t.Fatalf("written が true になっている")
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
