package tracker_test

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// 目的: 巡回ループ（VerifyStatusOptions）と turn ループ（UpdateStatus）を別の goroutine から
// 同時に叩いても、Adapter が覚えている値の読み書きが競合しないことを確認する
// （レビュー指摘「Adapter のフィールドが排他なしで書き換えられる」の回帰テスト）。
//
// **重なりは常態である。**turn は run が終わるまで生き続け、巡回は30秒ごとに走る。
// 排他が無いと、lookupOptionID が「新しい正式名」と「古い選択肢 ID」を組み合わせて、
// 誤った optionId を updateProjectV2ItemFieldValue へ渡しうる。
//
// **このテストは `go test -race` で走らせたときに意味を持つ。**
// これまで本物の Adapter を複数 goroutine から叩くテストが無かったため、-race でも出なかった。
//
// 与える情報: Bootstrap 済みの Adapter。片方の goroutine が VerifyStatusOptions を、
// もう片方が UpdateStatus を繰り返し呼ぶ。偽サーバは呼ばれたクエリの種類で応答を切り替える。
// 成功条件: どちらもエラーを返さないこと（-race を付けた実行で競合が報告されないこと）。
// 書き込まれた optionId が、カンバンが返した選択肢 ID と必ず一致していること。
func TestAdapter_巡回とturnから同時に呼んでも競合しない(t *testing.T) {
	const itemID = "item-188"

	var mu sync.Mutex
	badOptionIDs := []string{}

	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		switch {
		case strings.Contains(req.Query, "updateProjectV2ItemFieldValue"):
			// 書き込まれた optionId が、カンバンが返した選択肢 ID の集合に入っているかを見る。
			optionID, _ := req.Variables["optionId"].(string)
			known := false
			for _, opt := range testStatusOptions {
				if opt["id"] == optionID {
					known = true
					break
				}
			}
			if !known {
				mu.Lock()
				badOptionIDs = append(badOptionIDs, optionID)
				mu.Unlock()
			}
			return dataResponse(map[string]any{
				"updateProjectV2ItemFieldValue": map[string]any{
					"projectV2Item": map[string]any{"id": itemID},
				},
			})
		case strings.Contains(req.Query, "nodes(ids:"):
			// UpdateStatus が書き込み前に行う取り直し。
			item := issueItemJSON(testIssueItemOpts{
				ItemID: itemID, Status: "In Progress", Owner: "octocat", Repo: "hello-world",
				Number: 188, Title: "同時実行の検査", URL: "https://github.com/octocat/hello-world/issues/188",
			})
			return dataResponse(byIDsPayload([]any{asProjectV2ItemNode(item)}))
		default:
			// Bootstrap / VerifyStatusOptions。
			return dataResponse(bootstrapProjectPayload(testStatusOptions))
		}
	})

	a := newBootstrappedAdapter(t, fs)

	const rounds = 30
	var wg sync.WaitGroup
	errs := make(chan error, rounds*2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			if err := a.VerifyStatusOptions(context.Background(), testTrackerConfig()); err != nil {
				errs <- err
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			if _, err := a.UpdateStatus(context.Background(), itemID, "In Review", []string{"Done"}); err != nil {
				errs <- err
				return
			}
		}
	}()

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("同時に呼んだ結果エラーになった: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(badOptionIDs) > 0 {
		t.Fatalf("カンバンに無い選択肢 ID を書き込んだ（名前と ID を別の世代から読んでいる）: %v", badOptionIDs)
	}
}
