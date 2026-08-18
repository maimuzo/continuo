package tracker_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/tracker"
)

// 目的: Bootstrap が project の ID・Status フィールドの ID・選択肢の ID を解決し、
// 設定の Status 名（active_states・terminal_states・dispatch_state・failure_state・
// status_signal_map の遷移先）がすべてボード側の選択肢に存在する場合は成功することを
// 確認する（設計 3-6）。
// 与える情報: project #3 の実測構成（Ice Box/Ready/In Progress/Blocked/In Review/Done）と
// 一致する選択肢を返す偽サーバ。
// 成功条件: Bootstrap がエラーを返さないこと。
func TestBootstrap_選択肢名が設定と一致すれば成功する(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(bootstrapProjectPayload(testStatusOptions))))

	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	if err := a.Bootstrap(t.Context(), testTrackerConfig()); err != nil {
		t.Fatalf("Bootstrap が失敗した: %v", err)
	}
}

// 目的: Status の選択肢名が設定と食い違っている場合、Bootstrap がエラーを返すことを確認する
// （設計 3-6: 「合わないと GraphQL はエラーを出さずに0件を返す。これが最大の落とし穴」）。
// 与える情報: "In Progress" が抜け、代わりに無関係な選択肢しか無い偽サーバ応答。
// 成功条件: Bootstrap がエラーを返し、カテゴリが CategoryInvalidConfig であり、
// メッセージに欠けている Status 名（"In Progress"）が含まれること。
func TestBootstrap_選択肢名が食い違うとエラーになる(t *testing.T) {
	missingInProgress := []map[string]any{
		{"id": "opt-icebox", "name": "Ice Box"},
		{"id": "opt-ready", "name": "Ready"},
		{"id": "opt-blocked", "name": "Blocked"},
		{"id": "opt-inreview", "name": "In Review"},
		{"id": "opt-done", "name": "Done"},
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(bootstrapProjectPayload(missingInProgress))))

	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	err = a.Bootstrap(t.Context(), testTrackerConfig())
	if err == nil {
		t.Fatalf("選択肢名が食い違っているのに Bootstrap が成功した")
	}
	if !tracker.IsCategory(err, tracker.CategoryInvalidConfig) {
		t.Fatalf("エラーのカテゴリが CategoryInvalidConfig ではない: %v", err)
	}
	if !strings.Contains(err.Error(), "In Progress") {
		t.Fatalf("エラーメッセージに欠けている Status 名が含まれていない: %v", err)
	}
}

// 目的: Status 名の比較は大文字小文字を無視することを確認する（SPEC.md 11.3）。
// 与える情報: 設定は "Ready" だが、ボード側の選択肢は "ready"（小文字）で返る偽サーバ。
// 成功条件: Bootstrap が成功すること（大文字小文字の違いだけでエラーにならない）。
func TestBootstrap_Status名の比較は大文字小文字を無視する(t *testing.T) {
	lowered := []map[string]any{
		{"id": "opt-icebox", "name": "ice box"},
		{"id": "opt-ready", "name": "ready"},
		{"id": "opt-inprogress", "name": "in progress"},
		{"id": "opt-blocked", "name": "blocked"},
		{"id": "opt-inreview", "name": "in review"},
		{"id": "opt-done", "name": "done"},
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(bootstrapProjectPayload(lowered))))

	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	if err := a.Bootstrap(t.Context(), testTrackerConfig()); err != nil {
		t.Fatalf("大文字小文字だけが違うのに Bootstrap が失敗した: %v", err)
	}
}

// 目的: project そのものが見つからない場合（owner や project_number の設定ミス）に
// Bootstrap がエラーを返すことを確認する。
// 与える情報: repositoryOwner.projectV2 が null で返る偽サーバ応答。
// 成功条件: Bootstrap がエラーを返し、カテゴリが CategoryInvalidConfig であること。
func TestBootstrap_projectが見つからないとエラーになる(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(map[string]any{
		"repositoryOwner": map[string]any{"projectV2": nil},
	})))

	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	err = a.Bootstrap(t.Context(), testTrackerConfig())
	if err == nil {
		t.Fatalf("project が見つからないのに Bootstrap が成功した")
	}
	if !tracker.IsCategory(err, tracker.CategoryInvalidConfig) {
		t.Fatalf("エラーのカテゴリが CategoryInvalidConfig ではない: %v", err)
	}
}

// 目的: NewAdapter が tracker.kind の値を検査し、対応外の値なら
// CategoryUnsupportedKind のエラーを返すことを確認する。
// 与える情報: Kind に "jira" を指定した設定（サーバは呼ばれないはず）。
// 成功条件: NewAdapter がエラーを返し、カテゴリが CategoryUnsupportedKind であること。
func TestNewAdapter_対応外のkindはエラーになる(t *testing.T) {
	cfg := testTrackerConfig()
	cfg.Kind = "jira"

	_, err := tracker.NewAdapter(cfg, "http://example.invalid", "test-token", nil, nil, nil)
	if err == nil {
		t.Fatalf("対応外の kind なのに NewAdapter が成功した")
	}
	if !tracker.IsCategory(err, tracker.CategoryUnsupportedKind) {
		t.Fatalf("エラーのカテゴリが CategoryUnsupportedKind ではない: %v", err)
	}
}

// 目的: NewAdapter が owner・project_number・status_field の必須チェックを行うことを確認する。
// 与える情報: owner を空にした設定。
// 成功条件: NewAdapter がエラーを返し、カテゴリが CategoryInvalidConfig であること。
func TestNewAdapter_owner未設定はエラーになる(t *testing.T) {
	cfg := testTrackerConfig()
	cfg.Provider.Owner = ""

	_, err := tracker.NewAdapter(cfg, "http://example.invalid", "test-token", nil, nil, nil)
	if err == nil {
		t.Fatalf("owner が空なのに NewAdapter が成功した")
	}
	if !tracker.IsCategory(err, tracker.CategoryInvalidConfig) {
		t.Fatalf("エラーのカテゴリが CategoryInvalidConfig ではない: %v", err)
	}
}

// 目的: 認証が無い（GraphQL が 401 を返す）場合、CategoryMissingSecret に分類されることを
// 確認する。
// 与える情報: 常に 401 を返す偽サーバ。
// 成功条件: Bootstrap がエラーを返し、カテゴリが CategoryMissingSecret であること。
func TestBootstrap_認証が無いとMissingSecretに分類される(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(fakeGraphQLResponse{
		Status: http.StatusUnauthorized,
		Body:   map[string]any{"message": "Bad credentials"},
	}))

	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "invalid-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	err = a.Bootstrap(t.Context(), testTrackerConfig())
	if err == nil {
		t.Fatalf("401 なのに Bootstrap が成功した")
	}
	if !tracker.IsCategory(err, tracker.CategoryMissingSecret) {
		t.Fatalf("エラーのカテゴリが CategoryMissingSecret ではない: %v", err)
	}
}
