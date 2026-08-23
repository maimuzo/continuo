package tracker_test

import (
	"net/http"
	"testing"

	"github.com/maimuzo/continuo/internal/tracker"
)

// 目的: GraphQL がレートリミット超過を HTTP 403 + X-RateLimit-Remaining: 0 で返した場合、
// CategoryRateLimited に分類されることを確認する。
// 与える情報: 403 と X-RateLimit-Remaining: 0、Retry-After: 30 を返す偽サーバ。
// 成功条件: Bootstrap がエラーを返し、カテゴリが CategoryRateLimited であること。
func TestGraphQL_403とX_RateLimit_Remaining0でレートリミットに分類される(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(fakeGraphQLResponse{
		Status: http.StatusForbidden,
		Header: map[string]string{"X-RateLimit-Remaining": "0", "Retry-After": "30"},
		Body:   map[string]any{"message": "API rate limit exceeded"},
	}))

	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	err = a.Bootstrap(t.Context(), testTrackerConfig())
	if err == nil {
		t.Fatalf("レートリミット超過なのに Bootstrap が成功した")
	}
	if !tracker.IsCategory(err, tracker.CategoryRateLimited) {
		t.Fatalf("エラーのカテゴリが CategoryRateLimited ではない: %v", err)
	}
}

// 目的: GraphQL の errors 配列に type="RATE_LIMITED" が含まれる場合（HTTP は 200）も
// CategoryRateLimited に分類されることを確認する。
// 与える情報: HTTP 200 で `{"errors":[{"type":"RATE_LIMITED","message":"..."}]}` を返す偽サーバ。
// 成功条件: Bootstrap がエラーを返し、カテゴリが CategoryRateLimited であること。
func TestGraphQL_errors配列のRATE_LIMITEDでレートリミットに分類される(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(fakeGraphQLResponse{
		Body: map[string]any{
			"errors": []map[string]any{
				{"type": "RATE_LIMITED", "message": "API rate limit exceeded for installation"},
			},
		},
	}))

	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	err = a.Bootstrap(t.Context(), testTrackerConfig())
	if err == nil {
		t.Fatalf("レートリミット超過なのに Bootstrap が成功した")
	}
	if !tracker.IsCategory(err, tracker.CategoryRateLimited) {
		t.Fatalf("エラーのカテゴリが CategoryRateLimited ではない: %v", err)
	}
}

// 目的: 接続自体に失敗した場合（サーバが存在しない）、CategoryRequest に分類される
// ことを確認する（リクエストが失敗したときのエラー分類）。
// 与える情報: 実在しないポートへの URL。
// 成功条件: Bootstrap がエラーを返し、カテゴリが CategoryRequest であること。
func TestGraphQL_接続できないとCategoryRequestに分類される(t *testing.T) {
	a, err := tracker.NewAdapter(testTrackerConfig(), "http://127.0.0.1:1", "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	err = a.Bootstrap(t.Context(), testTrackerConfig())
	if err == nil {
		t.Fatalf("接続できないのに Bootstrap が成功した")
	}
	if !tracker.IsCategory(err, tracker.CategoryRequest) {
		t.Fatalf("エラーのカテゴリが CategoryRequest ではない: %v", err)
	}
}

// 目的: 応答本文が JSON として解析できない場合、CategoryResponse に分類されることを
// 確認する。
// 与える情報: JSON ではない本文を返す偽サーバ（http.HandlerFunc を直接使う）。
func TestGraphQL_壊れた応答はCategoryResponseに分類される(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(fakeGraphQLResponse{
		Body: "not-a-json-object-in-the-expected-shape-but-still-valid-json",
	}))
	// 上の Body は文字列なので `"..."` という妥当な JSON にエンコードされるが、
	// gqlEnvelope（{"data":...,"errors":...}）としては解析できない中身になる。
	// json.Unmarshal はオブジェクト以外を struct へ流し込もうとしてエラーになるはずである。

	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	err = a.Bootstrap(t.Context(), testTrackerConfig())
	if err == nil {
		t.Fatalf("壊れた応答なのに Bootstrap が成功した")
	}
	if !tracker.IsCategory(err, tracker.CategoryResponse) {
		t.Fatalf("エラーのカテゴリが CategoryResponse ではない: %v", err)
	}
}

// 目的: IsCategory が、tracker の *Error でないエラー（例: 標準の errors.New）に対して
// false を返すことを確認する（herdr.IsCode と同じ安全側の挙動）。
func TestIsCategory_tracker以外のエラーはfalse(t *testing.T) {
	if tracker.IsCategory(nil, tracker.CategoryRequest) {
		t.Fatalf("nil に対して true を返した")
	}
}
