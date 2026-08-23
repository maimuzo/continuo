package tracker_test

import (
	"context"
	"errors"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/tracker"
)

// 目的: token_source が "gh_auth" のとき、渡した関数（`gh auth token` の代わり）を呼んで
// その戻り値をトークンとして使うことを確認する。
// 与える情報: 常に固定のトークンを返す偽の関数。
// 成功条件: ResolveToken がその値をそのまま返すこと。
func TestResolveToken_ghAuthで取得した値を使う(t *testing.T) {
	provider := config.TrackerProviderConfig{TokenSource: "gh_auth"}

	token, err := tracker.ResolveToken(context.Background(), provider, func(ctx context.Context) (string, error) {
		return "token-from-gh-auth", nil
	})
	if err != nil {
		t.Fatalf("ResolveToken が失敗した: %v", err)
	}
	if token != "token-from-gh-auth" {
		t.Fatalf("トークンが想定と違う: got %q", token)
	}
}

// 目的: `gh auth token` 相当の関数が失敗した場合、CategoryMissingSecret に分類される
// エラーになることを確認する（認証が無いときのエラー分類）。
// 与える情報: 常にエラーを返す偽の関数。
// 成功条件: ResolveToken がエラーを返し、カテゴリが CategoryMissingSecret であること。
func TestResolveToken_ghAuthが失敗するとMissingSecret(t *testing.T) {
	provider := config.TrackerProviderConfig{TokenSource: "gh_auth"}

	_, err := tracker.ResolveToken(context.Background(), provider, func(ctx context.Context) (string, error) {
		return "", errors.New("gh: not logged in")
	})
	if err == nil {
		t.Fatalf("gh auth token が失敗したのに ResolveToken が成功した")
	}
	if !tracker.IsCategory(err, tracker.CategoryMissingSecret) {
		t.Fatalf("エラーのカテゴリが CategoryMissingSecret ではない: %v", err)
	}
}

// 目的: token_source が "env" のとき、指定した環境変数からトークンを読むことを確認する。
// 与える情報: token_env で指定した環境変数にトークンを設定する。
// 成功条件: ResolveToken がその環境変数の値を返すこと。
func TestResolveToken_envから取得できる(t *testing.T) {
	t.Setenv("CONTINUO_TEST_GH_TOKEN", "token-from-env")
	provider := config.TrackerProviderConfig{TokenSource: "env", TokenEnv: "CONTINUO_TEST_GH_TOKEN"}

	token, err := tracker.ResolveToken(context.Background(), provider, nil)
	if err != nil {
		t.Fatalf("ResolveToken が失敗した: %v", err)
	}
	if token != "token-from-env" {
		t.Fatalf("トークンが想定と違う: got %q", token)
	}
}

// 目的: token_source が "env" なのに環境変数が未設定の場合、CategoryMissingSecret に
// 分類されるエラーになることを確認する（認証が無いときのエラー分類）。
// 与える情報: token_env が指す環境変数を未設定にする。
// 成功条件: ResolveToken がエラーを返し、カテゴリが CategoryMissingSecret であること。
func TestResolveToken_env未設定はMissingSecret(t *testing.T) {
	provider := config.TrackerProviderConfig{TokenSource: "env", TokenEnv: "CONTINUO_TEST_GH_TOKEN_UNSET"}

	_, err := tracker.ResolveToken(context.Background(), provider, nil)
	if err == nil {
		t.Fatalf("環境変数が未設定なのに ResolveToken が成功した")
	}
	if !tracker.IsCategory(err, tracker.CategoryMissingSecret) {
		t.Fatalf("エラーのカテゴリが CategoryMissingSecret ではない: %v", err)
	}
}

// 目的: token_source が "env" なのに token_env 自体が空文字の場合、
// 設定の不備（CategoryInvalidConfig）として区別されることを確認する
// （「環境変数が無い」＝MissingSecret と「どの環境変数を見ればいいか設定されていない」＝
// InvalidConfig は別の問題である）。
// 与える情報: TokenEnv を空文字にした設定。
// 成功条件: ResolveToken がエラーを返し、カテゴリが CategoryInvalidConfig であること。
func TestResolveToken_tokenEnv未指定はInvalidConfig(t *testing.T) {
	provider := config.TrackerProviderConfig{TokenSource: "env", TokenEnv: ""}

	_, err := tracker.ResolveToken(context.Background(), provider, nil)
	if err == nil {
		t.Fatalf("token_env が空なのに ResolveToken が成功した")
	}
	if !tracker.IsCategory(err, tracker.CategoryInvalidConfig) {
		t.Fatalf("エラーのカテゴリが CategoryInvalidConfig ではない: %v", err)
	}
}

// 目的: token_source が未知の値の場合、CategoryInvalidConfig のエラーになることを確認する。
// 与える情報: TokenSource に "oauth" という未対応の値を指定した設定。
// 成功条件: ResolveToken がエラーを返し、カテゴリが CategoryInvalidConfig であること。
func TestResolveToken_未知のtoken_sourceはInvalidConfig(t *testing.T) {
	provider := config.TrackerProviderConfig{TokenSource: "oauth"}

	_, err := tracker.ResolveToken(context.Background(), provider, nil)
	if err == nil {
		t.Fatalf("未知の token_source なのに ResolveToken が成功した")
	}
	if !tracker.IsCategory(err, tracker.CategoryInvalidConfig) {
		t.Fatalf("エラーのカテゴリが CategoryInvalidConfig ではない: %v", err)
	}
}
