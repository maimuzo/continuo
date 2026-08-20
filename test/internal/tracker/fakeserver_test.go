// Package tracker_test は internal/tracker の GitHub Projects v2 アダプタを検証する。
//
// **本番のボード（project #3）へは絶対に接続しない。**httptest.Server でテスト用GraphQL mock
// サーバを立て、決まった JSON を返させることでアダプタの挙動を検証する
// （タスクの絶対制約。CLAUDE.md も同じ制約を課している）。
package tracker_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// capturedRequest は偽サーバが受け取った GraphQL リクエストの写しである。
type capturedRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// fakeGraphQLResponse は偽サーバが1回の呼び出しに対して返す応答である。
type fakeGraphQLResponse struct {
	// Status は HTTP ステータスコードである。0 の場合は http.StatusOK として扱う。
	Status int
	// Header は応答に追加で載せるヘッダである。nil でよい。
	Header map[string]string
	// Body は JSON エンコードして返す本体である
	// （`{"data": ...}` や `{"errors": [...]}` の形をそのまま渡すこと）。
	Body any
}

// fakeGraphQLServer は GitHub の GraphQL API の代わりに使う偽のサーバである。
// 受け取ったリクエストをすべて記録し、responder が返した応答をそのまま返す。
type fakeGraphQLServer struct {
	t         *testing.T
	server    *httptest.Server
	responder func(n int, req capturedRequest) fakeGraphQLResponse

	mu       sync.Mutex
	requests []capturedRequest
}

// newFakeGraphQLServer は httptest.Server を1本立てたテスト用GraphQL mockを起動する。
//
// t: 呼び出し元のテスト。サーバの後始末を t.Cleanup に登録する。
// responder: リクエストのたびに呼ばれ、返す応答を決める関数。n はこの呼び出しが
// 何回目か（1始まり）。応答が固定の場合は n を無視してよい。
// **responder はテスト本体とは別の goroutine（httptest.Server が接続ごとに起動する）から
// 呼ばれるので、その中で t.Fatalf を使ってはならない。**失敗を記録したい場合は t.Errorf を
// 使い、フォールバックの応答を返すこと。
// 戻り値: 起動した *fakeGraphQLServer。URL() を Adapter のコンストラクタへ渡すこと。
func newFakeGraphQLServer(
	t *testing.T,
	responder func(n int, req capturedRequest) fakeGraphQLResponse,
) *fakeGraphQLServer {
	t.Helper()

	fs := &fakeGraphQLServer{t: t, responder: responder}
	fs.server = httptest.NewServer(http.HandlerFunc(fs.handle))
	t.Cleanup(fs.server.Close)
	return fs
}

func (fs *fakeGraphQLServer) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		fs.t.Errorf("偽サーバがリクエスト本文を読み取れませんでした: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var req capturedRequest
	if err := json.Unmarshal(body, &req); err != nil {
		fs.t.Errorf("偽サーバがリクエストを GraphQL の形として解析できませんでした: %v (body=%s)", err, string(body))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	fs.mu.Lock()
	fs.requests = append(fs.requests, req)
	n := len(fs.requests)
	fs.mu.Unlock()

	resp := fs.responder(n, req)
	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}
	for k, v := range resp.Header {
		w.Header().Set(k, v)
	}
	w.WriteHeader(status)
	if resp.Body != nil {
		if err := json.NewEncoder(w).Encode(resp.Body); err != nil {
			fs.t.Errorf("偽サーバが応答を JSON 化できませんでした: %v", err)
		}
	}
}

// URL は偽サーバのエンドポイント URL を返す。Adapter のコンストラクタの endpoint 引数に渡す。
func (fs *fakeGraphQLServer) URL() string {
	return fs.server.URL
}

// Requests はこれまでに受け取ったリクエストを、受け取った順に返す。
func (fs *fakeGraphQLServer) Requests() []capturedRequest {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	out := make([]capturedRequest, len(fs.requests))
	copy(out, fs.requests)
	return out
}

// RequestCount はこれまでに受け取ったリクエストの件数を返す。
func (fs *fakeGraphQLServer) RequestCount() int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return len(fs.requests)
}

// single は毎回同じ応答を返す responder を作るヘルパーである。
func single(resp fakeGraphQLResponse) func(n int, req capturedRequest) fakeGraphQLResponse {
	return func(n int, req capturedRequest) fakeGraphQLResponse {
		return resp
	}
}

// dataResponse は成功応答 `{"data": data}` を作るヘルパーである。
func dataResponse(data any) fakeGraphQLResponse {
	return fakeGraphQLResponse{Body: map[string]any{"data": data}}
}
