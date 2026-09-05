// Package ratelimit は Claude の OAuth usage API を読み、5時間枠と週次枠の使用率と
// リセット時刻を取得する（docs/plans/continuo_design.md 3-15 / 3-27）。
//
// **この API はメッセージを送る API ではない。**枠の残量とリセット時刻を返すだけなので、
// 「`claude -p` を使わない（従量課金にしない）」という絶対制約には触れない。
//
// **`rate_limit.source: none` のときは1回も叩かない。**Enabled が偽を返し、Fetch は
// 常に nil を返す。
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
// **期限内に返らなくても1回では諦めない。**やり直せば通るかもしれない失敗なので、
// 連続 MaxTemporaryCredentialFailures 回まで粘る。先に `continuo allow-keychain-access` を
// 1回実行しておけば、以後ダイアログは出ない。
//
// **どの出所でも、恒久的に取れないと分かったら枠の判定を諦め、`none` と同じ動きにする。
// 起動は止めない。**
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
	"sync"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/i18n"
)

// SourceNone は rate_limit.source が「usage API を1回も叩かない」を意味する値である。
const SourceNone = "none"

// SourceOAuthUsageAPI は rate_limit.source が「OAuth の usage API を読む」を意味する値である。
const SourceOAuthUsageAPI = "oauth_usage_api"

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
// **これはエラーとして扱うが、起動は止めない**（設計 3-27）。Reader は自分で握りつぶし、
// 以後 Enabled が偽を返すようになる。
var ErrNoCredentials = i18n.Sentinel(i18n.KeyRatelimitErrNoCredentials)

// Limit は usage API が返す枠1件である（設計 3-15 の応答のサンプル）。
type Limit struct {
	// Kind は枠の種別である（"session" / "weekly_all" / "weekly_scoped"）。
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

// fullPercent は「使い切っている」とみなす使用率である（設計 3-27 の条件その1）。
//
// **`>=` で比べる。**API が 100 を超える値を返しても取りこぼさない。
const fullPercent = 100

// Snapshot は usage API を1回読んだ結果である。
type Snapshot struct {
	// Limits は返ってきた枠の一覧である。
	Limits []Limit
	// FetchedAt は読んだ時刻である。
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
		if l.Percent >= fullPercent {
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
		if l.Percent < fullPercent || l.ResetsAt == nil {
			continue
		}
		if !found || l.ResetsAt.After(latest) {
			latest = *l.ResetsAt
			found = true
		}
	}
	return latest, found
}

// FullLimitKinds は、使い切っている（`percent` が 100 に達している）枠の `kind` を返す
// （設計 3-27。issue #197）。
//
// **並び順は応答のままである。**呼び出し側は「含まれるか」しか見ない。
// **重複は取り除かない。**同じ `kind` が2件返る応答を実測していないので、
// **見ていない形を先回りして畳まない。**
//
// **この package は `kind` の意味を知らない。**どれが1週間の枠かを決めるのは
// `internal/handoff` の `IsWeeklyKind` であり、**そちらを import すると依存の向きが逆になる。**
//
// 戻り値: 使い切っている枠の `kind`。1件も無ければ長さ0。
func (s *Snapshot) FullLimitKinds() []string {
	if s == nil {
		return nil
	}
	var kinds []string
	for _, l := range s.Limits {
		if l.Percent >= fullPercent {
			kinds = append(kinds, l.Kind)
		}
	}
	return kinds
}

// LatestResetOfKinds は、使い切っている枠のうち指定した種別のものから、
// `resets_at` がいちばん遅いものを返す（設計 3-27。issue #197）。
//
// **1つでも `resets_at` を持たないものがあれば「分からない」と返す。**
// 持っているものだけで判定すると、**待つ先の分からない枠を無視して、短いほうへ倒れる。**
// 1週間の枠がいつ明けるか分からないのに「2時間後に明ける」と読むことになる。
//
// **`LatestResetOfFullLimits` とは別物である。**あちらは枠待ちの印を外す時刻を決めるもので、
// **種別を選ばず、`resets_at` の無い枠は黙って飛ばす**（設計 3-27 の「どの枠の時刻を見るか」）。
// **こちらは「1週間の枠を待つ上限」の判定に使う。**混ぜてはならない。
//
// **`kind` の比べ方は呼び出し側が持つ。**この package は `kind` の意味を知らないので、
// **種別の一覧を受け取ると、大文字小文字と前後の空白の扱いを2箇所で決めることになる。**
// **実際にずれた。**`internal/handoff` の `matchesKind` は左辺しか空白を落とさず、
// ここに置いていた写しは両辺を落としていた。**判定する関数を1つ受け取れば、写しは消える。**
//
// match: その `kind` を数えるなら true を返す関数。**nil なら、いつも「分からない」を返す。**
// 戻り値の1つ目: いちばん遅いリセット時刻。
// 戻り値の2つ目: 該当する枠が1つ以上あり、その全部が `resets_at` を持っていれば true。
func (s *Snapshot) LatestResetOfKinds(match func(kind string) bool) (time.Time, bool) {
	if s == nil || match == nil {
		return time.Time{}, false
	}
	var latest time.Time
	found := false
	for _, l := range s.Limits {
		if l.Percent < fullPercent || !match(l.Kind) {
			continue
		}
		if l.ResetsAt == nil {
			// **1つでも待つ先が無ければ、全体として「分からない」である。**
			return time.Time{}, false
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
}

// Reader は usage API を読む。
//
// **複数の goroutine から同時に呼んでよい。**
type Reader struct {
	cfg             config.RateLimitConfig
	endpoint        string
	client          *http.Client
	homeDir         string
	userAgent       string
	keychainTimeout time.Duration
	logger          *slog.Logger

	// mu は disabled と warned と tempFailures を守る。
	mu sync.Mutex
	// disabled は枠の判定を諦めたことを表す。
	// **一度立てたら戻さない。**読めないものを毎回読みに行かない。
	//
	// **立てるのは恒久的な失敗のときだけである。**
	//
	//	資格情報が恒久的に取れない … `security` が PATH に無い / Keychain に項目が無い /
	//	                          ファイルが無い / 環境変数が空 / 中身が壊れている
	//	usage API が 401 / 403     … そのトークンでは以後も読めない
	//	一時的な失敗が連続した      … MaxTemporaryCredentialFailures 回
	//
	// **一時的な失敗（`security` が期限内に返らなかった）1回では立てない。**
	// 立ててしまうと、枠を使い切って黙っただけのエージェントを stall と誤認して
	// pane を閉じることになる（設計 3-27）。
	disabled bool
	// warned は資格情報が取れないことを既に警告したかどうかである（警告は1回だけ。3-15）。
	warned bool
	// tempFailures は資格情報の取得が「一時的な理由で」連続して失敗した回数である。
	// **1回でも成功したら 0 に戻す。**MaxTemporaryCredentialFailures に達したら諦める。
	tempFailures int
}

// MaxTemporaryCredentialFailures は、資格情報の取得が一時的な理由で連続して失敗しても
// 諦めない回数である。
//
// **一時的な失敗で諦めてはならない。**`security` が1回だけ遅れた・確認のダイアログが
// 1回出た・機材が重くて子プロセスの起動が遅れた、のどれでも枠の判定が永久に止まると、
// **枠を使い切って黙っただけのエージェントの pane を continuo が閉じる**（設計 3-27 の
// 枠待ちの判定が働かず、stall と誤認する）。
//
// **かといって永久に叩き続けてもいけない。**読めないものを毎回読みに行くと、
// 巡回のたびに `security` を起こすことになる。5回で諦める（`rate_limit.poll_interval_ms`
// の既定は5分なので、およそ25分ぶん粘る）。
const MaxTemporaryCredentialFailures = 5

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
// **打ち切りは資格情報の問題ではない。**回数にも数えないし、諦める理由にもしない。
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

	return &Reader{
		cfg:             opts.Config,
		endpoint:        endpoint,
		client:          client,
		homeDir:         homeDir,
		userAgent:       userAgent,
		keychainTimeout: keychainTimeout,
		logger:          logger,
	}, nil
}

// Enabled は usage API を読む設定になっているかを返す。
//
// **`rate_limit.source: none` なら常に偽である**（1回も叩かない）。
// 資格情報を取れずに諦めたあとも偽になる。
//
// 戻り値: 読む設定なら true。
func (r *Reader) Enabled() bool {
	if r == nil || r.cfg.Source == SourceNone {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.disabled
}

// Fetch は usage API を1回読む。
//
// **Enabled が偽のときは HTTP リクエストを1本も出さず、(nil, nil) を返す。**
//
// **資格情報の失敗は、恒久的なものと一時的なものを言い分ける。**恒久的なもの
// （`security` が PATH に無い・Keychain に項目が無い・資格情報のファイルが無い・
// 環境変数が空・中身が壊れている）は警告を1回だけ出して以後 Enabled を偽にし、
// (nil, nil) を返す（**起動を止めない**。設計 3-27）。一時的なもの
// （`security` が期限内に返らなかった）は 5xx と同じくエラーで返し、**Enabled は真のまま
// 残す。**連続 MaxTemporaryCredentialFailures 回でようやく諦める。
// **打ち切り（ctx の cancel）は回数にも数えない。**
//
// **401 / 403 を受けたときは諦める。**そのトークンでは以後も読めないので、
// 資格情報が恒久的に取れない場合と同じく警告を1回出して以後 Enabled を偽にし、
// (nil, nil) を返す。それ以外の非 200（5xx 等）は一時的な失敗としてエラーで返す。
//
// ctx: 呼び出しに適用するコンテキスト。
// 戻り値の1つ目: 読み取った枠の一覧。読まなかった場合・資格情報が恒久的に取れない場合・
// 401 / 403 を受けた場合は nil。
// 戻り値の2つ目: HTTP の失敗・応答の解析の失敗・401 / 403 以外の非 200・
// 資格情報の一時的な失敗のときのエラー。
func (r *Reader) Fetch(ctx context.Context) (*Snapshot, error) {
	if !r.Enabled() {
		return nil, nil
	}

	token, err := r.token(ctx)
	if err != nil {
		switch {
		case isCanceledCredentialFailure(err):
			// **打ち切りは資格情報の問題ではない。**数えないし、諦めない。
			return nil, err
		case isTemporaryCredentialFailure(err):
			if n := r.noteTemporaryFailure(); n < MaxTemporaryCredentialFailures {
				// **諦めない。**次の巡回で読み直す。枠の判定は生きたままである。
				return nil, err
			}
			r.disable(i18n.Errorf(i18n.KeyRatelimitCredentialsTemporaryExhausted,
				err, MaxTemporaryCredentialFailures))
			return nil, nil
		default:
			r.disable(err)
			return nil, nil
		}
	}
	r.clearTemporaryFailures()

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
		statusErr := i18n.Errorf(i18n.KeyRatelimitFetchUnexpectedStatus, resp.StatusCode, truncate(body, 200))
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			// **401 / 403 は、このトークンでは以後も読めない。**諦めないと、失効した
			// accessToken を抱えた無人のプロセスが巡回のたび（既定30秒）に叩き直し、
			// ログが同じ頻度で汚れ続ける。資格情報が取れなかった場合と同じ扱いにする
			// （**起動は止めない**。設計 3-27）。
			r.disable(statusErr)
			return nil, nil
		}
		return nil, statusErr
	}

	var parsed struct {
		Limits []Limit `json:"limits"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, i18n.Errorf(i18n.KeyRatelimitFetchParseFailed, err)
	}

	return &Snapshot{Limits: parsed.Limits, FetchedAt: time.Now()}, nil
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

// noteTemporaryFailure は資格情報の一時的な失敗を1回数え、連続した回数を返す。
//
// 戻り値: 直近で成功してから連続した一時的な失敗の回数（1始まり）。
func (r *Reader) noteTemporaryFailure() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tempFailures++
	return r.tempFailures
}

// clearTemporaryFailures は一時的な失敗の連続回数を 0 に戻す。
//
// **資格情報を取れたら必ず呼ぶ。**戻さないと、間隔を空けて起きた単発の失敗が積み上がり、
// 一度も連続していないのに諦めることになる。
func (r *Reader) clearTemporaryFailures() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tempFailures = 0
}

// disable は枠の判定を諦める。警告は1回だけ出す（設計 3-15）。
//
// cause: 諦めた理由。
func (r *Reader) disable(cause error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.disabled = true
	if r.warned {
		return
	}
	r.warned = true
	r.logger.Warn(
		"枠の判定を諦めます（rate_limit.source: none と同じ動きになります。起動は止めません）",
		"error", cause,
	)
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
