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
// 本番は RunGHAuthToken を使う。テストではコマンドを実際に実行せずに済むよう、
// 別の関数を差し替えて渡す（herdr パッケージの AgentStartWithRetry と同じく、
// グローバル変数ではなく引数で差し替える設計にしてある）。
type GHAuthTokenFunc func(ctx context.Context) (string, error)

// RunGHAuthToken は実際に `gh auth token` を実行し、標準出力をトークンとして返す。
//
// ctx: 実行に適用するコンテキスト。
// 戻り値: 前後の空白を落としたトークン文字列。コマンドの実行に失敗した場合、または
// 出力が空文字だった場合はエラーを返す。
func RunGHAuthToken(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "auth", "token")
	out, err := cmd.Output()
	if err != nil {
		return "", i18n.Errorf(i18n.KeyTrackerGHAuthTokenRunFailed, err)
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", i18n.Errorf(i18n.KeyTrackerGHAuthTokenEmptyOutput)
	}
	return token, nil
}

// ResolveToken は tracker.provider.token_source の設定に従って continuo 自身が
// GitHub Projects v2 のボードを読み書きするためのトークンを取得する（設計「その1」）。
//
// ctx: gh コマンドを実行する場合に適用するコンテキスト。
// provider: tracker.provider の設定（TokenSource / TokenEnv）。
// ghAuthToken: TokenSource が "gh_auth" のときに使う取得関数。nil を渡すと RunGHAuthToken
// （本物のコマンド実行）を使う。テストは偽の関数を渡すことでコマンド実行を避けられる。
// 戻り値: 取得したトークン。TokenSource が未知の値の場合は CategoryInvalidConfig、
// gh_auth の取得に失敗した場合・env の環境変数が未設定または空の場合は
// CategoryMissingSecret の *Error を返す。
func ResolveToken(
	ctx context.Context,
	provider config.TrackerProviderConfig,
	ghAuthToken GHAuthTokenFunc,
) (string, error) {
	if ghAuthToken == nil {
		ghAuthToken = RunGHAuthToken
	}

	switch provider.TokenSource {
	case "gh_auth":
		token, err := ghAuthToken(ctx)
		if err != nil {
			return "", &Error{
				Category: CategoryMissingSecret,
				Message: "tracker.provider.token_source が gh_auth ですが、`gh auth token` で" +
					"トークンを取得できませんでした（gh のログイン状態を確認してください）",
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
