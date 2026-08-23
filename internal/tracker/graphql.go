package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// defaultGraphQLEndpoint は GitHub の GraphQL API v4 のエンドポイントである。
const defaultGraphQLEndpoint = "https://api.github.com/graphql"

// defaultHTTPTimeout は httpClient を渡されなかったときに組み立てるクライアントの
// 全体の待ち時間である。
//
// **無期限にしてはならない。**巡回ループは Tick を同期で呼ぶので、TCP は張れたが応答が
// 返らない状態（不安定な回線・captive portal）になると、候補の取得・実行中の照合・
// stall の判定が全部止まったまま復帰しない。internal/ratelimit と同じ値にしてある。
const defaultHTTPTimeout = 30 * time.Second

// defaultDialTimeout は httpClient を渡されなかったときに組み立てるクライアントの
// 接続（TCP の確立）の待ち時間である。
const defaultDialTimeout = 10 * time.Second

// graphqlClient は GitHub の GraphQL API を呼び出す薄いクライアントである。
// **GraphQL 専用のライブラリは使わない。**標準の net/http と encoding/json だけで組み立てる
// （設計「その1」）。
//
// 呼び出しごとの待ち時間は ctx の期限に従うが、**ctx に期限が無い呼び出しに備えて
// *http.Client 側にも上限を持たせる**（呼び出し側の規約だけに頼らない）。
type graphqlClient struct {
	// endpoint は GraphQL API の URL である。テストでは httptest.Server の URL に差し替える。
	endpoint string
	// token は Authorization ヘッダに載せる認証トークンである。
	token string
	// httpClient はリクエストを送るクライアントである。
	httpClient *http.Client
}

// isLoopbackHost は URL のホスト部（ポートを含みうる）が loopback を指しているかを返す。
//
// **テストの httptest.Server は http の loopback である。**そこだけは https を要求しない。
//
// host: url.URL の Host（例 "127.0.0.1:8080"、"localhost"）。
// 戻り値: 127.0.0.0/8・::1・localhost のいずれかなら true。
func isLoopbackHost(host string) bool {
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	name = strings.Trim(name, "[]")
	if strings.EqualFold(name, "localhost") {
		return true
	}
	if ip := net.ParseIP(name); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// validateEndpoint は接続先の URL が、トークンを載せて送ってよい相手かを検査する。
//
// **検査しないと、環境変数（CONTINUO_GITHUB_GRAPHQL_ENDPOINT）に http:// の任意のホストを
// 書くだけで、`gh auth token` で取った GitHub のトークンが平文で第三者へ渡る。**
// herdr の socket は「環境変数を勝手に優先しない」と決めてあるのに（設計 2-1）、
// 資格情報が付いてまわるこちらだけが無検査なのは非対称である。
//
// endpoint: 検査する URL。
// 戻り値: URL として解析できない、scheme が https でも loopback の http でもない、
// ホストが空、のいずれかなら CategoryInvalidConfig の *Error。
func validateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return &Error{
			Category: CategoryInvalidConfig,
			Message:  fmt.Sprintf("GraphQL の接続先 %q を URL として解析できません", endpoint),
			Err:      err,
		}
	}
	if u.Host == "" {
		return &Error{
			Category: CategoryInvalidConfig,
			Message:  fmt.Sprintf("GraphQL の接続先 %q にホスト名がありません", endpoint),
		}
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Host) {
			// httptest.Server（テストの偽サーバ）だけがここを通る。
			return nil
		}
	}
	return &Error{
		Category: CategoryInvalidConfig,
		Message: fmt.Sprintf(
			"GraphQL の接続先 %q は https ではありません（GitHub のトークンを Authorization ヘッダに"+
				"載せて送るため、平文の http は loopback 以外では受け付けません）",
			endpoint,
		),
	}
}

// newGraphQLClient は graphqlClient を作る。
//
// endpoint: GraphQL API の URL。空文字なら defaultGraphQLEndpoint を使う。
// **https 以外は拒否する**（loopback の http だけは例外。validateEndpoint を参照）。
// token: Authorization ヘッダに載せる認証トークン（ResolveToken で取得した値）。
// httpClient: リクエストを送るクライアント。**nil なら接続10秒・全体30秒のクライアントを
// 組み立てて使う**（http.DefaultClient は待ち時間を持たないので使わない）。
// 戻り値: 組み立てた graphqlClient。endpoint が受け付けられない場合は
// CategoryInvalidConfig の *Error。
func newGraphQLClient(endpoint, token string, httpClient *http.Client) (*graphqlClient, error) {
	if endpoint == "" {
		endpoint = defaultGraphQLEndpoint
	}
	if err := validateEndpoint(endpoint); err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: defaultHTTPTimeout,
			Transport: &http.Transport{
				DialContext:       (&net.Dialer{Timeout: defaultDialTimeout}).DialContext,
				ForceAttemptHTTP2: true,
			},
		}
	}
	return &graphqlClient{endpoint: endpoint, token: token, httpClient: httpClient}, nil
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
// ctx: 呼び出しに適用するコンテキスト。**期限が無くても無期限にはならない**
// （httpClient 側に全体30秒の上限がある。newGraphQLClient を参照）。ctx に期限を付ければ
// そちらが先に効く。
// query: 送る GraphQL のクエリ／ミューテーション文字列。
// variables: クエリの変数。nil でもよい。
// out: 成功したときに data フィールドを解析する先。nil なら解析しない
// （書き込み系ミューテーションで応答の中身を使わない場合に使う）。
// 戻り値: 分類された *Error。ネットワーク層の失敗は CategoryRequest、
// 401 は CategoryMissingSecret、レートリミットは CategoryRateLimited、
// それ以外の非2xxは CategoryStatus、応答の解析失敗や GraphQL の errors は
// CategoryResponse に分類する。
func (c *graphqlClient) do(ctx context.Context, query string, variables map[string]any, out any) error {
	return c.doWith(ctx, query, variables, out, false)
}

// doAllowingNotFound は `NOT_FOUND` を部分的な成功として扱う `do` である。
//
// **`nodes(ids:)` で ID を指定して引く呼び出しだけが使う。**
// GitHub は消えた ID が混ざると、`data.nodes` にその位置だけ `null` を入れたうえで、
// `errors` にも `NOT_FOUND` を返す。**そこで `data` を捨てると、生き残っている issue まで
// 読めなくなる**（2026-08-21 に実運用で発生。設計 6-2）。
//
// **ほかの呼び出しでは使ってはならない。**フィールド名やボードの解決に失敗したときも
// `NOT_FOUND` が返るので、握りつぶすと**設定の綴り違いに永久に気づけない。**
//
// ctx / query / variables / out: do と同じ。
// 戻り値: do と同じ。ただしエラーが `NOT_FOUND` だけなら、`data` を書き出して nil を返す。
func (c *graphqlClient) doAllowingNotFound(ctx context.Context, query string, variables map[string]any, out any) error {
	return c.doWith(ctx, query, variables, out, true)
}

// doWith は do と doAllowingNotFound の実体である。
//
// allowNotFound: `NOT_FOUND` だけのエラーを部分的な成功として扱うかどうか。
func (c *graphqlClient) doWith(ctx context.Context, query string, variables map[string]any, out any, allowNotFound bool) error {
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
		// **`NOT_FOUND` だけなら、`data` を捨てずに先へ進む。**
		//
		// **GitHub の GraphQL は `nodes(ids:)` に消えた ID が混ざると、
		// `data.nodes` にその位置だけ `null` を入れたうえで `errors` にも `NOT_FOUND` を返す。**
		// **これは部分的な成功である。**ここで捨てると、生き残っている issue まで読めなくなる。
		//
		// **消えた ID の扱いは呼び出し側が決める**（FetchIssuesByIDs は `null` を
		// 「もう見えない」として省く。SPEC.md 11.1）。
		//
		// 実運用で、ボードごと消したあとに毎巡回でこのエラーが出続けた（2026-08-21。設計 6-2）。
		if !allowNotFound || !onlyNotFound(envelope.Errors) {
			return &Error{
				Category: CategoryResponse,
				Message:  fmt.Sprintf("GraphQL がエラーを返しました: %s", joinGQLErrors(envelope.Errors)),
			}
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

// notFoundErrorType は「その ID のノードが見つからない」ことを表す GraphQL のエラー種別である。
//
// **GitHub は `nodes(ids:)` に消えた ID が混ざると、`data` を返したうえでこれも返す。**
const notFoundErrorType = "NOT_FOUND"

// onlyNotFound は、返ってきたエラーが `NOT_FOUND` だけかどうかを返す。
//
// **1件でも別の種別が混ざっていれば偽である。**部分的な成功として扱ってよいのは、
// 「消えた ID があった」だけのときに限る。
//
// errs: GraphQL が返したエラーの一覧。**空なら偽を返す**（呼び出し側が
// `len(errs) > 0` を確かめてから呼ぶ）。
// 戻り値: すべて NOT_FOUND なら true。
func onlyNotFound(errs []gqlErrorEntry) bool {
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		if e.Type != notFoundErrorType {
			return false
		}
	}
	return true
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
//
// **バイトではなく文字（rune）で切る。**GitHub はエラー本文に日本語を含めうるので、
// バイトで切ると末尾の多バイト文字が割れてログに壊れた文字が出る。
//
// b: 元の本文。
// max: 残す文字数。
// 戻り値: max 文字を超える場合は末尾に "...(truncated)" を付けた文字列。
func truncate(b []byte, max int) string {
	r := []rune(string(b))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "...(truncated)"
}
