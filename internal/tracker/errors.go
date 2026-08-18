package tracker

import (
	"errors"
	"fmt"
	"time"
)

// ErrorCategory はアダプタが返すエラーを分類するための値である
// （SPEC.md 11.4「RECOMMENDED adapter error categories」）。
// 値は仕様のカテゴリ名をそのまま使う。
type ErrorCategory string

const (
	// CategoryUnsupportedKind は tracker.kind が "github_projects_v2" 以外のときに返す。
	CategoryUnsupportedKind ErrorCategory = "unsupported_tracker_kind"
	// CategoryInvalidConfig は設定が不正なときに返す。
	// 起動時の検査（Bootstrap）で Status の選択肢名が設定と一致しない場合もこれに含める
	// （設計 3-6: 「合わないと GraphQL はエラーを出さずに0件を返す。これが最大の落とし穴」）。
	CategoryInvalidConfig ErrorCategory = "invalid_tracker_config"
	// CategoryMissingSecret は認証が無い・取得できないときに返す
	// （`gh auth token` の失敗、環境変数未設定、GraphQL からの 401 応答を含む）。
	CategoryMissingSecret ErrorCategory = "missing_tracker_secret"
	// CategoryRequest は HTTP リクエストそのものが失敗した（接続できない・タイムアウトした等の
	// トランスポート層の失敗）ときに返す。
	CategoryRequest ErrorCategory = "tracker_request"
	// CategoryStatus は HTTP が非成功のステータスコードを返したときに返す
	// （401 は CategoryMissingSecret、レートリミット由来の 403 は CategoryRateLimited に
	// それぞれ分類するため、それ以外の非 2xx がここに入る）。
	CategoryStatus ErrorCategory = "tracker_status"
	// CategoryResponse は応答が壊れている・意味的に不正なときに返す
	// （JSON として解析できない、GraphQL の errors 配列にレートリミット以外のエラーが
	// 入っている、想定した形の値が無い、等）。
	CategoryResponse ErrorCategory = "tracker_response"
	// CategoryPagination はページングの整合性が壊れたときに返す
	// （例: hasNextPage が真なのに endCursor が空、ページを跨いで想定外に件数が変わった等）。
	CategoryPagination ErrorCategory = "tracker_pagination"
	// CategoryRateLimited はレートリミットに達したときに返す。
	CategoryRateLimited ErrorCategory = "tracker_rate_limited"
)

// Error は tracker パッケージが返すエラーである。SPEC.md 11.4 が求める
// 「安定したカテゴリ」と「人間可読なメッセージ」の組を herdr.Error と同じ形で表現する。
type Error struct {
	// Category は安定したエラー分類である。
	Category ErrorCategory
	// Message は人間可読なエラーメッセージである。
	Message string
	// Retryable は呼び出し側がリトライしてよいかどうかのヒントである。
	// tracker パッケージ自身はリトライしない（呼び出し側の責務）。
	Retryable bool
	// RetryAfter はレートリミットの場合の推奨待機時間である。0 なら不明。
	RetryAfter time.Duration
	// Err はラップした元のエラーである（無ければ nil）。errors.Unwrap で辿れる。
	Err error
}

// Error は error インターフェースを満たす。
func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("tracker エラー [%s]: %s: %v", e.Category, e.Message, e.Err)
	}
	return fmt.Sprintf("tracker エラー [%s]: %s", e.Category, e.Message)
}

// Unwrap は errors.Is / errors.As がラップした元のエラーを辿れるようにする。
func (e *Error) Unwrap() error {
	return e.Err
}

// IsCategory は err が tracker の *Error であり、かつその Category が category と一致するか
// どうかを判定する。err がラップされたエラーであっても errors.As で辿って判定する
// （internal/herdr の IsCode と同じ考え方）。
//
// err: 判定対象のエラー。
// category: 期待するカテゴリ（例: CategoryRateLimited）。
// 戻り値: 一致すれば true。err が tracker の *Error でない場合、または nil の場合は false。
func IsCategory(err error, category ErrorCategory) bool {
	var trackerErr *Error
	if errors.As(err, &trackerErr) {
		return trackerErr.Category == category
	}
	return false
}
