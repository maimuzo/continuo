// Package ratelimit は Claude の OAuth usage APIを読み、5時間枠と週次枠の使用率と
// リセット時刻を取得する（docs/plans/continuo_design.md 3-15 / 3-27 / issue #284）。
//
// **この API はメッセージを送る API ではない。**枠の残量とリセット時刻を返すだけなので、
// 「`claude -p` を使わない（従量課金にしない）」という絶対制約には触れない。
//
// **`rate_limit.source` が `oauth_usage_api` のときだけ叩く。**`statusline` と `none` では
// Enabled が偽を返し、Fetch は1本も HTTP リクエストを出さない。
//
// **資格情報の出所は `rate_limit.token_source` で決まる。**
//
//	claude_credentials … `~/.claude/.credentials.json` を読む
//	keychain           … macOS の Keychain を `security` で読む（**macOS でだけ選べる**）
//	env                … `rate_limit.token_env` に書かれた環境変数を読む
//
// **macOS では `~/.claude/.credentials.json` が無いのが普通で、資格情報は Keychain にある**
// （2026-08-21 に実測）。そのため macOS の既定は `keychain` である（internal/config）。
//
// **Keychain を読むと確認のダイアログが出ることがある。**答えられないまま無人のプロセスが
// 固まらないよう、`security` の呼び出しには必ず上限を置く（DefaultKeychainTimeout）。
// 先に `continuo allow-keychain-access` を1回実行しておけば、以後ダイアログは出ない。
//
// **この package は諦めない。**読めなかったら、そのつど誤りを種類つきで返す
// （*CredentialError / *StatusError / *RateLimitedError / それ以外）。次にいつ試すか・
// statusline取得へ切り替えるかは、呼び出し側（internal/orchestrator）が決める。
package ratelimit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/i18n"
)

// SourceNone は rate_limit.source が「使用率を読まない」を意味する値である
// （usage API も statusline も使わない）。
const SourceNone = "none"

// SourceOAuthUsageAPI は rate_limit.source が「OAuth の usage APIを主に読み、
// エラーのときは statusline取得へ切り替える」を意味する値である（既定。issue #284）。
// **internal/config の RateLimitSourceOAuthUsageAPI と同じ文字列である。**
const SourceOAuthUsageAPI = "oauth_usage_api"

// SourceStatusline は rate_limit.source が「ステータスラインから使用率を受ける」を意味する値である
// （issue #284）。**internal/config の RateLimitSourceStatusline と同じ文字列である。**
// **この値では usage API を1回も叩かない。**
const SourceStatusline = "statusline"

// 枠の種別である（usage API が `kind` に返す値。internal/handoff の LimitKind* と同じ文字列）。
// internal/handoff は internal/ratelimit を読むので、ここで持つ。
const (
	// KindSession は5時間の枠である。
	KindSession = "session"
	// KindWeeklyAll は1週間全体の枠である。
	KindWeeklyAll = "weekly_all"
	// KindWeeklyScoped は1週間のモデル別の枠である。**usage API しか運ばない**（ステータスラインには無い）。
	KindWeeklyScoped = "weekly_scoped"
)

// TokenSourceClaudeCredentials は資格情報を `~/.claude/.credentials.json` から読むことを表す。
const TokenSourceClaudeCredentials = "claude_credentials"

// TokenSourceEnv は資格情報を環境変数から読むことを表す。
const TokenSourceEnv = "env"

// DefaultEndpoint は Claude の OAuth usage API の URL である（設計 3-15）。
//
// **設定から差し替えられるようにしてある**（Options.Endpoint）。テストは httptest.Server の
// URL を渡し、本番の API へは接続しない。
const DefaultEndpoint = "https://api.anthropic.com/api/oauth/usage"

// betaHeaderValue は usage API が要求する anthropic-beta ヘッダの値である（設計 3-15）。
// **これを落とすと 401 になる。**
const betaHeaderValue = "oauth-2025-04-20"

// defaultUserAgent は claude のバージョンを取れなかったときに送る User-Agent である（設計 3-15）。
const defaultUserAgent = "claude-code/2.0.0"

// 接続と全体のタイムアウトである（設計 3-15）。
//
// **dialTimeout は TCP の接続確立の上限である。**http.Transport.DialContext に渡す。
// ResponseHeaderTimeout に入れてはならない（そちらは応答ヘッダを待つ上限であり、
// 接続には上限が掛からない）。
const (
	dialTimeout    = 10 * time.Second
	overallTimeout = 30 * time.Second
)

// CredentialsRelPath はホームディレクトリからの資格情報ファイルの相対パスである。
// **`token_source: claude_credentials` のときだけ読む。**
var CredentialsRelPath = filepath.Join(".claude", ".credentials.json")

// ErrNoCredentials は資格情報を取れなかったことを表す。
//
// **これはエラーとして扱うが、起動は止めない**（設計 3-27）。Fetch は *CredentialError に
// 包んで返し、呼び出し側が statusline取得へ切り替える（issue #284）。
var ErrNoCredentials = i18n.Sentinel(i18n.KeyRatelimitErrNoCredentials)

// Limit は枠1件である（設計 3-15 の応答のサンプル）。usage API の応答の形であり、
// orchestrator の保管値の写しもこの形で渡す（issue #284）。
type Limit struct {
	// Kind は枠の種別である（"session" / "weekly_all" / "weekly_scoped"）。
	// **"weekly_scoped" は usage API しか運ばない**（ステータスラインには無い）。
	Kind string `json:"kind"`
	// Percent は使用率（整数の百分率）である。
	Percent int `json:"percent"`
	// ResetsAt は枠がリセットされる時刻である。**null のことがある**ので nil を許す。
	ResetsAt *time.Time `json:"resets_at"`
	// Severity は provider が付ける深刻さである。
	//
	// **continuo はこの値を見ない**（設計 3-27）。上限を示す値が何かを実測できていない。
	// 記録とダッシュボードのためだけに保持する。
	Severity string `json:"severity"`
}

// Snapshot は usage API を1回読んだ結果、または保管値の写しである（issue #284）。
type Snapshot struct {
	// Limits は枠の一覧である。
	Limits []Limit
	// FetchedAt は読んだ時刻（保管値の写しなら新しさの時刻）である。
	FetchedAt time.Time
}

// MaxPercent は枠の中でいちばん高い使用率を返す。
//
// 戻り値: 使用率の最大値。枠が1件も無ければ 0。
func (s *Snapshot) MaxPercent() int {
	if s == nil {
		return 0
	}
	max := 0
	for _, l := range s.Limits {
		if l.Percent > max {
			max = l.Percent
		}
	}
	return max
}

// AtFullPercent は、使い切っている（`percent` が 100 に達している）枠が1つでもあるかを返す
// （設計 3-27 の「この run は枠待ちである」の条件その1）。
//
// 戻り値: 100 に達している枠があれば true。
func (s *Snapshot) AtFullPercent() bool {
	if s == nil {
		return false
	}
	for _, l := range s.Limits {
		if l.Percent >= 100 {
			return true
		}
	}
	return false
}

// LatestResetOfFullLimits は、使い切っている枠のうち `resets_at` がいちばん遅いものを返す
// （設計 3-27 の「どの枠の時刻を見るか」）。
//
// **`resets_at` が null の枠は判定から外す。**`weekly_scoped` も、モデルを判別せず
// そのまま見る（continuo は Claude Code が使うモデルを知らない）。
//
// 戻り値の1つ目: いちばん遅いリセット時刻。
// 戻り値の2つ目: 該当する枠が1つでもあれば true。
func (s *Snapshot) LatestResetOfFullLimits() (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	var latest time.Time
	found := false
	for _, l := range s.Limits {
		if l.Percent < 100 || l.ResetsAt == nil {
			continue
		}
		if !found || l.ResetsAt.After(latest) {
			latest = *l.ResetsAt
			found = true
		}
	}
	return latest, found
}

// Options は Reader を組み立てるための入力である。
type Options struct {
	// Config は WORKFLOW.md の front matter の rate_limit セクションである。
	Config config.RateLimitConfig
	// Endpoint は usage API の URL である。空なら DefaultEndpoint を使う。
	// **テストは httptest.Server の URL を渡すこと**（本番の API へ接続しない）。
	Endpoint string
	// HTTPClient はリクエストを送るクライアントである。nil なら接続10秒・全体30秒の
	// クライアントを組み立てて使う（設計 3-15）。
	HTTPClient *http.Client
	// HomeDir は `~/.claude/.credentials.json` を探すホームディレクトリである。
	// 空なら os.UserHomeDir() の結果を使う。
	HomeDir string
	// UserAgent は送る User-Agent である。空なら defaultUserAgent を使う。
	UserAgent string
	// KeychainTimeout は `token_source: keychain` のときに `security` を待つ上限である。
	// **0 以下なら DefaultKeychainTimeout を使う。**
	// テストは短い値を渡して、返ってこない `security` を待たずに済ませられる。
	KeychainTimeout time.Duration
	// Logger はログの出力先である。nil なら slog.Default() を使う。
	Logger *slog.Logger
	// Now は時計である。nil なら time.Now を使う（Retry-After の HTTP の日付を解く基準）。
	Now func() time.Time
}

// Reader は usage API を読む。
//
// **状態を持たない。**諦めた印も失敗の回数も持たず、読むたびに結果か誤りを返す
// （issue #284。切り替えと次に試す時刻は internal/orchestrator が持つ）。
// **複数の goroutine から同時に呼んでよい。**
type Reader struct {
	cfg             config.RateLimitConfig
	endpoint        string
	client          *http.Client
	homeDir         string
	userAgent       string
	keychainTimeout time.Duration
	logger          *slog.Logger
	// now は時計である（Retry-After の HTTP の日付を解く基準。テストが差し替える）。
	now func() time.Time
}

// CredentialError は資格情報（トークン）を読めなかったことを表す（issue #284）。
//
// **一時的か恒久的かを持つ。**呼び出し側は、恒久的なら立て直すまで usage API を試し直さず、
// 一時的なら `poll_interval_ms` のあとに試し直す。
//
//	一時的 … `security` が期限（10秒）内に返らなかった（ErrKeychainTimeout）。
//	         Keychain の確認のダイアログに誰も答えていないときである
//	恒久的 … それ以外すべて。資格情報のファイルが無い・読めない・壊れている、
//	         `token_env` の環境変数が空、Keychain に項目が無い・拒否された・ロックされている、
//	         `security` が無い、など（やり直しても結果が変わらない）
//
// **打ち切り（ctx の取り消し）はこの型にしない。**資格情報の問題ではないためである。
type CredentialError struct {
	// Permanent は恒久的な失敗かである。
	Permanent bool
	// Err は元の誤りである（ErrNoCredentials を包んでいる）。
	Err error
}

// Error は元の誤りの文面を返す。
func (e *CredentialError) Error() string { return e.Err.Error() }

// Unwrap は元の誤りを返す（errors.Is で ErrNoCredentials / ErrKeychainTimeout を辿れるように）。
func (e *CredentialError) Unwrap() error { return e.Err }

// StatusError は usage API が 200 以外を返したことを表す（issue #284）。
type StatusError struct {
	// StatusCode は HTTP の状態コードである。
	StatusCode int
	// Body はエラーの本文の先頭である（200文字まで）。
	Body string
}

// Error は状態コードと本文の先頭を返す。
func (e *StatusError) Error() string {
	return i18n.T(i18n.KeyRatelimitFetchUnexpectedStatus, e.StatusCode, e.Body)
}

// RateLimitedError は usage API が 429 を返したことを表す（issue #284）。
//
// **Retry-After を持つ。**秒で来たら RetryAfter、HTTP の日付で来たら RetryAt に入れる
// （どちらも無ければ両方ゼロ値）。上限（24時間）を掛けるのは呼び出し側である。
type RateLimitedError struct {
	// Status は状態コードと本文である。errors.As で *StatusError としても取り出せる。
	Status *StatusError
	// RetryAfter は Retry-After が秒で来たときの長さである。
	RetryAfter time.Duration
	// RetryAt は Retry-After が HTTP の日付で来たときの時刻である。
	RetryAt time.Time
	// raw は Retry-After の生の値である（文面に載せる）。
	raw string
}

// Error は状態コードと Retry-After と本文の先頭を返す。
func (e *RateLimitedError) Error() string {
	return i18n.T(i18n.KeyRatelimitFetchRateLimited, e.Status.StatusCode, e.raw, e.Status.Body)
}

// Unwrap は *StatusError を返す。
func (e *RateLimitedError) Unwrap() error { return e.Status }

// ErrNoWindows は usage API が 200 を返したのに `session` も `weekly_all` も無かったことを表す
// （issue #284）。**誤りとして扱う。**成功にすると、使用率の無い応答で新しさだけが進み、
// statusline取得へ切り替わらない。
var ErrNoWindows = i18n.Sentinel(i18n.KeyRatelimitFetchNoWindows)

// isTemporaryCredentialFailure は、資格情報を取れなかった原因が一時的なものかを判定する。
//
// **恒久的なものと言い分けるためにある。**`security` が PATH に無い・Keychain に項目が
// 無い・資格情報のファイルが無い・環境変数が空、はどれもやり直しても結果が変わらないので
// 一時的ではない。
//
// err: 資格情報の取得が返したエラー。
// 戻り値: やり直せば取れるかもしれないなら true。
func isTemporaryCredentialFailure(err error) bool {
	return errors.Is(err, ErrKeychainTimeout)
}

// isCanceledCredentialFailure は、資格情報の取得が「打ち切られた」ことによる失敗かを判定する。
//
// **打ち切りは資格情報の問題ではない。**一時的とも恒久的とも数えない。
//
// err: 資格情報の取得が返したエラー。
// 戻り値: 呼び出し側の打ち切りが原因なら true。
func isCanceledCredentialFailure(err error) bool {
	return errors.Is(err, ErrKeychainCanceled) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}

// NewReader は Reader を組み立てる。**この時点では資格情報を読まない。**
//
// opts: 設定・エンドポイント・HTTP クライアント・ホームディレクトリ・ログ。
// 戻り値: 組み立てた Reader。ホームディレクトリを特定できない場合はエラーを返す。
func NewReader(opts Options) (*Reader, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	endpoint := opts.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: overallTimeout,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{Timeout: dialTimeout}).DialContext,
				// 自前の Transport を組み立てると HTTP/2 が既定で無効になるので明示する。
				ForceAttemptHTTP2: true,
			},
		}
	}
	homeDir := opts.HomeDir
	if homeDir == "" && opts.Config.Source != SourceNone && opts.Config.TokenSource == TokenSourceClaudeCredentials {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return nil, i18n.Errorf(i18n.KeyRatelimitNewReaderHomeDirFailed, CredentialsRelPath, err)
		}
	}
	userAgent := opts.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	keychainTimeout := opts.KeychainTimeout
	if keychainTimeout <= 0 {
		keychainTimeout = DefaultKeychainTimeout
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	return &Reader{
		cfg:             opts.Config,
		endpoint:        endpoint,
		client:          client,
		homeDir:         homeDir,
		userAgent:       userAgent,
		keychainTimeout: keychainTimeout,
		logger:          logger,
		now:             now,
	}, nil
}

// Enabled は usage API を読む設定になっているかを返す。
//
// **`rate_limit.source` が `oauth_usage_api` のときだけ真である**（`statusline` と `none` では
// 1回も叩かない）。
//
// 戻り値: 読む設定なら true。
func (r *Reader) Enabled() bool {
	return r != nil && r.cfg.Source == SourceOAuthUsageAPI
}

// Fetch は usage API を1回読む。
//
// **Enabled が偽のときは HTTP リクエストを1本も出さず、(nil, nil) を返す。**
//
// **読めなかったら、そのつど誤りを返す。諦めない**（issue #284。ba24db63 までは恒久的な失敗と
// 401 / 403 で `(nil, nil)` を返して以後叩かなかったが、それでは statusline取得へ切り替える
// 判断を呼び出し側が下せない）。
//
//	資格情報が一時的に取れない … *CredentialError{Permanent: false}
//	資格情報が恒久的に取れない … *CredentialError{Permanent: true}
//	打ち切り                   … 元の誤りのまま（資格情報の問題ではない）
//	429                        … *RateLimitedError（Retry-After を持つ）
//	それ以外の 200 以外        … *StatusError（401 / 403 / 5xx など）
//	`session` も `weekly_all` も無い 200 … ErrNoWindows を包んだ誤り
//	通信の失敗・応答の解析の失敗 … それ以外の誤り
//
// ctx: 呼び出しに適用するコンテキスト。
// 戻り値の1つ目: 読み取った枠の一覧。読まなかった場合と誤りのときは nil。
// 戻り値の2つ目: 読めなかった理由。
func (r *Reader) Fetch(ctx context.Context) (*Snapshot, error) {
	if !r.Enabled() {
		return nil, nil
	}

	token, err := r.token(ctx)
	if err != nil {
		switch {
		case isCanceledCredentialFailure(err):
			// **打ち切りは資格情報の問題ではない。**一時的とも恒久的とも数えない。
			return nil, err
		case isTemporaryCredentialFailure(err):
			return nil, &CredentialError{Permanent: false, Err: err}
		default:
			return nil, &CredentialError{Permanent: true, Err: err}
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.endpoint, nil)
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyRatelimitFetchRequestBuildFailed, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", betaHeaderValue)
	req.Header.Set("User-Agent", r.userAgent)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyRatelimitFetchRequestFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyRatelimitFetchBodyReadFailed, err)
	}
	if resp.StatusCode != http.StatusOK {
		statusErr := &StatusError{StatusCode: resp.StatusCode, Body: truncate(body, 200)}
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, r.rateLimited(statusErr, resp.Header.Get("Retry-After"))
		}
		// **401 / 403 も諦めない**（issue #284）。401 は一時的でもありうる（トークンの更新の途中など）。
		// 次にいつ試すかは呼び出し側が `poll_interval_ms` で決める。
		return nil, statusErr
	}

	var parsed struct {
		Limits []Limit `json:"limits"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, i18n.Errorf(i18n.KeyRatelimitFetchParseFailed, err)
	}
	if !hasMainWindow(parsed.Limits) {
		// **`session` も `weekly_all` も無い 200 は誤りである**（`weekly_scoped` だけでも）。
		// 成功にすると、入札に要る値が無いまま新しさだけが進み、statusline取得へ切り替わらない。
		return nil, ErrNoWindows
	}

	return &Snapshot{Limits: parsed.Limits, FetchedAt: r.now()}, nil
}

// hasMainWindow は、`session` か `weekly_all` が1件以上あるかを返す。
func hasMainWindow(limits []Limit) bool {
	for _, l := range limits {
		if l.Kind == KindSession || l.Kind == KindWeeklyAll {
			return true
		}
	}
	return false
}

// rateLimited は 429 の誤りを、Retry-After を読んで組み立てる。
//
// **Retry-After は秒か HTTP の日付である**（RFC 9110 10.2.3）。どちらでも読めなければ
// 両方ゼロ値のまま返す（呼び出し側は `poll_interval_ms` で試し直す）。
//
// statusErr: 状態コードと本文。
// raw: Retry-After の生の値。
// 戻り値: 429 の誤り。
func (r *Reader) rateLimited(statusErr *StatusError, raw string) *RateLimitedError {
	e := &RateLimitedError{Status: statusErr, raw: raw}
	v := strings.TrimSpace(raw)
	if v == "" {
		return e
	}
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs > 0 {
			e.RetryAfter = time.Duration(secs) * time.Second
		}
		return e
	}
	if t, err := http.ParseTime(v); err == nil {
		e.RetryAt = t
	}
	return e
}

// token は設定に従って OAuth のトークンを取り出す。
//
// ctx: 呼び出しに適用するコンテキスト（`keychain` のとき `security` の実行に渡す）。
// 戻り値の1つ目: トークン。
// 戻り値の2つ目: 取れなかった場合は ErrNoCredentials を包んだエラー。
func (r *Reader) token(ctx context.Context) (string, error) {
	switch r.cfg.TokenSource {
	case TokenSourceKeychain:
		return r.tokenFromKeychain(ctx)
	case TokenSourceEnv:
		name := r.cfg.TokenEnv
		if name == "" {
			return "", i18n.Errorf(i18n.KeyRatelimitTokenEnvNameEmpty, ErrNoCredentials)
		}
		v := os.Getenv(name)
		if v == "" {
			return "", i18n.Errorf(i18n.KeyRatelimitTokenEnvValueEmpty, ErrNoCredentials, name)
		}
		return v, nil
	default:
		return r.tokenFromCredentialsFile()
	}
}

// tokenFromCredentialsFile は `~/.claude/.credentials.json` から accessToken を読む。
//
// **macOS ではこのファイルが無いのが普通である**（資格情報は Keychain に入っている）。
// macOS で枠を読みたいなら `token_source: keychain` を使う。
//
// **通常のファイルであることを確かめてから読む。**symlink は辿らない。
// 権限が group / other に開いている場合は警告を1行残す（読むこと自体は止めない）。
//
// 戻り値の1つ目: `.claudeAiOauth.accessToken` の値。
// 戻り値の2つ目: ファイルが無い・通常のファイルでない・読めない・トークンが空の場合は
// ErrNoCredentials を包んだエラー。
func (r *Reader) tokenFromCredentialsFile() (string, error) {
	if r.homeDir == "" {
		return "", i18n.Errorf(i18n.KeyRatelimitCredentialsFileHomeDirUnknown, ErrNoCredentials)
	}
	path := filepath.Join(r.homeDir, CredentialsRelPath)
	// **開く前にファイルの種別を確かめる。**中身は Claude の OAuth アクセストークンであり、
	// 無人の常駐プロセスがこれを読んで HTTP ヘッダに載せる。symlink を辿ると、
	// 別の場所に置き換えられたファイルを黙って読むことになる（os.Lstat は辿らない）。
	info, err := os.Lstat(path)
	if err != nil {
		// **「無い」と「読めない」を分ける。**
		//
		// **macOS では、このファイルが無いのが普通である**（資格情報は Keychain にある。
		// 2026-08-21 に実測）。`continuo init` が作った古い設定ファイルが
		// `claude_credentials` のまま残っていると、ここに落ちて毎回失敗し続ける。
		// **どう直せばよいかを添えないと、警告が流れるだけで誰も気づけない。**
		if errors.Is(err, os.ErrNotExist) {
			return "", i18n.Errorf(i18n.KeyRatelimitCredentialsFileNotExist,
				ErrNoCredentials, path, remedyForMissingCredentialsFile())
		}
		return "", i18n.Errorf(i18n.KeyRatelimitCredentialsFileReadFailed, ErrNoCredentials, path, err)
	}
	if !info.Mode().IsRegular() {
		return "", i18n.Errorf(i18n.KeyRatelimitCredentialsFileNotRegularFile, ErrNoCredentials, path, info.Mode())
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		// **読むのは止めないが、必ず1行残す。**権限が緩んだことに誰も気づかないまま、
		// 他ユーザーから読める資格情報を使い続けるのを避ける。
		r.logger.Warn(
			"資格情報のファイルが自分以外からも読める権限になっています（chmod 600 を推奨します）",
			"path", path, "mode", fmt.Sprintf("%04o", perm),
		)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", i18n.Errorf(i18n.KeyRatelimitCredentialsFileReadFailed, ErrNoCredentials, path, err)
	}
	// **中身の解釈は Keychain と共有する**（keychain.go の parseAccessToken）。
	// 資格情報の JSON の形は出所によらず同じなので、写しを2つ持たない。
	token, err := parseAccessToken(data)
	if err != nil {
		return "", i18n.Errorf(i18n.KeyRatelimitCredentialsFileParseFailed, ErrNoCredentials, path, err)
	}
	if token == "" {
		return "", i18n.Errorf(i18n.KeyRatelimitCredentialsFileAccessTokenMissing, ErrNoCredentials, path)
	}
	return token, nil
}

// truncate はエラーメッセージへ載せる本文を切り詰める。
//
// **バイトではなく文字（rune）で切る。**usage API のエラー本文に非 ASCII が混ざると、
// バイトで切った末尾の多バイト文字が割れてログに壊れた文字が出る。
//
// b: 元の本文。
// max: 残す文字数。
// 戻り値: max 文字を超える場合は末尾に "…" を付けた文字列。
func truncate(b []byte, max int) string {
	r := []rune(string(b))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "…"
}

// remedyForMissingCredentialsFile は、`claude_credentials` を選んだのにファイルが
// 無いときの直し方を返す。
//
// **OS で答えが変わる。**macOS は Keychain に資格情報があるのが普通なので
// `keychain` へ変えるのが正解であり、ほかの OS に `keychain` は無い。
//
// 戻り値: 設定ファイルに何と書けばよいかを示す1行。
func remedyForMissingCredentialsFile() string {
	if runtime.GOOS == "darwin" {
		return i18n.T(i18n.KeyRatelimitCredentialsRemedyKeychain)
	}
	return i18n.T(i18n.KeyRatelimitCredentialsRemedyEnv)
}
