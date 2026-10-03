package tracker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/i18n"
)

// GHAuthTokenFunc は `gh auth token` 相当の処理を行う関数の型である。
// 本番は GHAuthTokenForHost が返す関数を使う。テストではコマンドを実際に実行せずに済むよう、
// 別の関数を差し替えて渡す（herdr パッケージの AgentStartWithRetry と同じく、
// グローバル変数ではなく引数で差し替える設計にしてある）。
type GHAuthTokenFunc func(ctx context.Context) (string, error)

// GHAuthTokenForHost は、接続先ホストのトークンを `gh auth token --hostname <ホスト>` で
// 取る関数を返す（設計 3-86）。
//
// **ホストを必ず渡す。**渡さないと `gh` は自分の既定ホストのトークンを返すので、
// github.com と GitHub Enterprise の両方にログインしている機械では、
// **接続先の宛先へ別のホストのトークンを送る。**
//
// **ctx の期限で殺したあとの後始末にも上限を置く**（`cmd.WaitDelay`）。置かないと、
// `gh` が孫プロセスへ標準出力を渡していた場合に `Output` が返らず、**期限を掛けた意味が
// 無くなる**（internal/ratelimit の runSecurity と同じ理由）。
//
// host: 接続先ホスト（`tracker.provider.host`）。空なら github.com として扱う。
// 戻り値: トークンを取る関数。その関数は、前後の空白を落としたトークン文字列を返す。
// コマンドの実行に失敗した場合、または出力が空文字だった場合はエラーを返す。
// **その関数へは、期限を持たせた ctx を渡すこと。**
func GHAuthTokenForHost(host string) GHAuthTokenFunc {
	h := NormalizedHost(host)
	return func(ctx context.Context) (string, error) {
		cmd := exec.CommandContext(ctx, ghBinary, "auth", "token", "--hostname", h)
		cmd.WaitDelay = ghWaitDelay
		out, err := cmd.Output()
		if err != nil {
			return "", i18n.Errorf(i18n.KeyTrackerGHAuthTokenRunFailed, h, err)
		}
		token := strings.TrimSpace(string(out))
		if token == "" {
			return "", i18n.Errorf(i18n.KeyTrackerGHAuthTokenEmptyOutput, h)
		}
		return token, nil
	}
}

// ResolveToken は tracker.provider.token_source の設定に従って continuo 自身が
// GitHub Projects v2 のカンバンを読み書きするためのトークンを取得する（設計「その1」）。
//
// ctx: gh コマンドを実行する場合に適用するコンテキスト。
// provider: tracker.provider の設定（Host / TokenSource / TokenEnv）。
// ghAuthToken: TokenSource が "gh_auth" のときに使う取得関数。nil を渡すと
// GHAuthTokenForHost(provider.Host)（本物のコマンド実行）を使う。テストは偽の関数を渡すことでコマンド実行を避けられる。
// 戻り値: 取得したトークン。TokenSource が未知の値の場合は CategoryInvalidConfig、
// gh_auth の取得に失敗した場合・env の環境変数が未設定または空の場合は
// CategoryMissingSecret の *Error を返す。
func ResolveToken(
	ctx context.Context,
	provider config.TrackerProviderConfig,
	ghAuthToken GHAuthTokenFunc,
) (string, error) {
	if ghAuthToken == nil {
		ghAuthToken = GHAuthTokenForHost(provider.Host)
	}

	switch provider.TokenSource {
	case "gh_auth":
		token, err := ghAuthToken(ctx)
		if err != nil {
			return "", &Error{
				Category: CategoryMissingSecret,
				Message: fmt.Sprintf(
					"tracker.provider.token_source が gh_auth ですが、`gh auth token --hostname %[1]s` で"+
						"トークンを取得できませんでした（`gh auth status --hostname %[1]s` で gh のログイン状態を"+
						"確認してください。接続先は tracker.provider.host で決まります）",
					NormalizedHost(provider.Host),
				),
				Err: err,
			}
		}
		return token, nil
	case "env":
		if provider.TokenEnv == "" {
			return "", &Error{
				Category: CategoryInvalidConfig,
				Message:  "tracker.provider.token_source が env ですが、tracker.provider.token_env が空です",
			}
		}
		token, ok := os.LookupEnv(provider.TokenEnv)
		if !ok || token == "" {
			return "", &Error{
				Category: CategoryMissingSecret,
				Message: fmt.Sprintf(
					"tracker.provider.token_source が env ですが、環境変数 %s が未設定または空です",
					provider.TokenEnv,
				),
			}
		}
		return token, nil
	default:
		return "", &Error{
			Category: CategoryInvalidConfig,
			Message: fmt.Sprintf(
				"tracker.provider.token_source の値が不正です（gh_auth か env のいずれかにしてください）: %q",
				provider.TokenSource,
			),
		}
	}
}
