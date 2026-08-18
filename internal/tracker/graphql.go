package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// defaultGraphQLEndpoint は GitHub の GraphQL API v4 のエンドポイントである。
const defaultGraphQLEndpoint = "https://api.github.com/graphql"

// graphqlClient は GitHub の GraphQL API を呼び出す薄いクライアントである。
// **GraphQL 専用のライブラリは使わない。**標準の net/http と encoding/json だけで組み立てる
// （設計「その1」）。
//
// herdr.Client（internal/herdr/client.go）と同じく、呼び出しの待ち時間は ctx の期限に
// 委ねる。固定の *http.Client タイムアウトを持たせない。
type graphqlClient struct {
	// endpoint は GraphQL API の URL である。テストでは httptest.Server の URL に差し替える。
	endpoint string
	// token は Authorization ヘッダに載せる認証トークンである。
	token string
	// httpClient はリクエストを送るクライアントである。
	httpClient *http.Client
}

// newGraphQLClient は graphqlClient を作る。
//
// endpoint: GraphQL API の URL。空文字なら defaultGraphQLEndpoint を使う。
// token: Authorization ヘッダに載せる認証トークン（ResolveToken で取得した値）。
// httpClient: リクエストを送るクライアント。nil なら http.DefaultClient を使う。
func newGraphQLClient(endpoint, token string, httpClient *http.Client) *graphqlClient {
	if endpoint == "" {
		endpoint = defaultGraphQLEndpoint
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &graphqlClient{endpoint: endpoint, token: token, httpClient: httpClient}
}

// gqlRequestBody は GraphQL へ送るリクエストボディの wire format である。
type gqlRequestBody struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// gqlErrorEntry は GraphQL の応答に含まれる errors 配列の1件である。
// "type" はレートリミット（"RATE_LIMITED"）や NOT_FOUND のような判別に使う。
type gqlErrorEntry struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// gqlEnvelope は GraphQL の応答全体の wire format である。
type gqlEnvelope struct {
	Data   json.RawMessage `json:"data"`
	Errors []gqlErrorEntry `json:"errors"`
}

// rateLimitedErrorType は GraphQL がレートリミット超過を示すときの errors[].type の値である。
const rateLimitedErrorType = "RATE_LIMITED"

// do は GraphQL を1回呼び出す。query と variables を送り、成功すれば応答の data フィールドを
// out へ解析する。
//
// ctx: 呼び出しに適用するコンテキスト。期限が無ければ待ち時間は無制限になる
// （呼び出し側が必ず期限を設定すること）。
// query: 送る GraphQL のクエリ／ミューテーション文字列。
// variables: クエリの変数。nil でもよい。
// out: 成功したときに data フィールドを解析する先。nil なら解析しない
// （書き込み系ミューテーションで応答の中身を使わない場合に使う）。
// 戻り値: 分類された *Error。ネットワーク層の失敗は CategoryRequest、
// 401 は CategoryMissingSecret、レートリミットは CategoryRateLimited、
// それ以外の非2xxは CategoryStatus、応答の解析失敗や GraphQL の errors は
// CategoryResponse に分類する。
func (c *graphqlClient) do(ctx context.Context, query string, variables map[string]any, out any) error {
	reqBody, err := json.Marshal(gqlRequestBody{Query: query, Variables: variables})
	if err != nil {
		return &Error{Category: CategoryResponse, Message: "GraphQL リクエストの組み立てに失敗しました", Err: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return &Error{Category: CategoryRequest, Message: "GraphQL リクエストを組み立てられません", Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &Error{
			Category:  CategoryRequest,
			Message:   fmt.Sprintf("GitHub の GraphQL API への接続に失敗しました: %s", c.endpoint),
			Retryable: true,
			Err:       err,
		}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &Error{Category: CategoryRequest, Message: "GraphQL の応答本文を読み取れません", Retryable: true, Err: err}
	}

	if err := classifyHTTPStatus(resp, respBody); err != nil {
		return err
	}

	var envelope gqlEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return &Error{
			Category: CategoryResponse,
			Message:  fmt.Sprintf("GraphQL の応答を JSON として解析できません（body=%s）", truncate(respBody, 500)),
			Err:      err,
		}
	}

	if len(envelope.Errors) > 0 {
		if entry, ok := findRateLimitedError(envelope.Errors); ok {
			return &Error{
				Category:   CategoryRateLimited,
				Message:    fmt.Sprintf("GraphQL がレートリミット超過を返しました: %s", entry.Message),
				Retryable:  true,
				RetryAfter: rateLimitRetryAfter(resp),
			}
		}
		return &Error{
			Category: CategoryResponse,
			Message:  fmt.Sprintf("GraphQL がエラーを返しました: %s", joinGQLErrors(envelope.Errors)),
		}
	}

	if out != nil {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return &Error{
				Category: CategoryResponse,
				Message:  fmt.Sprintf("GraphQL の data を解析できません（data=%s）", truncate(envelope.Data, 500)),
				Err:      err,
			}
		}
	}
	return nil
}

// classifyHTTPStatus は HTTP レベルの応答を検査し、非成功のステータスコードを
// アダプタのエラー分類に変換する。成功（2xx）なら nil を返す。
func classifyHTTPStatus(resp *http.Response, body []byte) error {
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusUnauthorized:
		return &Error{
			Category: CategoryMissingSecret,
			Message: fmt.Sprintf(
				"GitHub の GraphQL API が認証エラー（401）を返しました。トークンが無効か失効しています: %s",
				truncate(body, 300),
			),
		}
	case resp.StatusCode == http.StatusForbidden:
		if isRateLimitBody(resp, body) {
			return &Error{
				Category:   CategoryRateLimited,
				Message:    fmt.Sprintf("GitHub の GraphQL API がレートリミット超過（403）を返しました: %s", truncate(body, 300)),
				Retryable:  true,
				RetryAfter: rateLimitRetryAfter(resp),
			}
		}
		return &Error{
			Category: CategoryStatus,
			Message:  fmt.Sprintf("GitHub の GraphQL API が 403 を返しました: %s", truncate(body, 300)),
		}
	case resp.StatusCode == http.StatusTooManyRequests:
		return &Error{
			Category:   CategoryRateLimited,
			Message:    fmt.Sprintf("GitHub の GraphQL API が 429 を返しました: %s", truncate(body, 300)),
			Retryable:  true,
			RetryAfter: rateLimitRetryAfter(resp),
		}
	default:
		return &Error{
			Category:  CategoryStatus,
			Message:   fmt.Sprintf("GitHub の GraphQL API が想定外のステータス %d を返しました: %s", resp.StatusCode, truncate(body, 300)),
			Retryable: resp.StatusCode >= 500,
		}
	}
}

// isRateLimitBody は 403 応答がレートリミット超過によるものかどうかを、
// ヘッダと応答本文から判定する。GitHub は一次レートリミット超過のとき
// X-RateLimit-Remaining: 0 を返し、二次レートリミット（乱用防止）のときは
// 本文に "rate limit" という語を含める。
func isRateLimitBody(resp *http.Response, body []byte) bool {
	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		return true
	}
	if resp.Header.Get("Retry-After") != "" {
		return true
	}
	return strings.Contains(strings.ToLower(string(body)), "rate limit")
}

// findRateLimitedError は GraphQL の errors 配列からレートリミット超過を示す1件を探す。
func findRateLimitedError(errs []gqlErrorEntry) (gqlErrorEntry, bool) {
	for _, e := range errs {
		if e.Type == rateLimitedErrorType {
			return e, true
		}
	}
	return gqlErrorEntry{}, false
}

// rateLimitRetryAfter は応答ヘッダからレートリミットの推奨待機時間を読み取る。
// Retry-After（秒）を最優先し、無ければ X-RateLimit-Reset（UNIX 秒）から算出する。
// どちらも無ければ 0（不明）を返す。
func rateLimitRetryAfter(resp *http.Response) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
		if unixSeconds, err := strconv.ParseInt(v, 10, 64); err == nil {
			d := time.Until(time.Unix(unixSeconds, 0))
			if d > 0 {
				return d
			}
		}
	}
	return 0
}

// joinGQLErrors は GraphQL の errors 配列を1行のメッセージにまとめる。
func joinGQLErrors(errs []gqlErrorEntry) string {
	parts := make([]string, len(errs))
	for i, e := range errs {
		if e.Type != "" {
			parts[i] = fmt.Sprintf("[%s] %s", e.Type, e.Message)
		} else {
			parts[i] = e.Message
		}
	}
	return strings.Join(parts, "; ")
}

// truncate はログ・エラーメッセージに埋め込む本文を一定の長さで切り詰める。
// 巨大な応答本文をそのままエラーメッセージへ埋め込んでログを膨らませないためである。
func truncate(b []byte, max int) string {
	s := string(b)
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
