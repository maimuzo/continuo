package tracker_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/tracker"
)

// 目的: Bootstrap が project の ID・Status フィールドの ID・選択肢の ID を解決し、
// 設定の Status 名（active_states・terminal_states・dispatch_state・failure_state・
// status_signal_map の遷移先）がすべてカンバン側の選択肢に存在する場合は成功することを
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
// 与える情報: 設定は "Ready" だが、カンバン側の選択肢は "ready"（小文字）で返る偽サーバ。
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

// TestBootstrap_カンバンに無い対応表のキーは起動を止めず名前で知らせる は、設計 3-57 を確かめる
// （issue #67 の2件目）。
//
// 目的: **人間がカンバンの自動化をやめ、使わなくなった Status の選択肢を画面から消す。**
// 対応表のキーをカンバンと照合して起動を止めていたので、**設定は正しいままなのに
// continuo が二度と立ち上がらなくなり、抜け出す方法もどこにも出なかった。**
// **キーは定義上「continuo が知らない Status」であり、カンバンに実在しなくてよい。**
//
// **綴りの打ち間違いも同じ形に見える**ので、起動を止める代わりに名前で知らせる。
//
// 与える情報: カンバンに実在しない `In Progres`（`s` が1つ足りない）をキーに書いた設定。
// 成功条件:
//   - Bootstrap が成功すること（起動を止めない）
//   - ログにそのキーの名前と、対応表から消す案内が出ること
func TestBootstrap_カンバンに無い対応表のキーは起動を止めず名前で知らせる(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := testTrackerConfig()
	// **戻す先は active_states にある実在の Status。**見たい違いをキーだけに絞る。
	cfg.AutomatedStateRewrite = map[string]string{"In Progres": "In Progress"}
	fs := newFakeGraphQLServer(t, single(dataResponse(bootstrapProjectPayload(testStatusOptions))))

	a, err := tracker.NewAdapter(cfg, fs.URL(), "test-token", nil, logger, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	if err := a.Bootstrap(t.Context(), cfg); err != nil {
		t.Fatalf("対応表のキーがカンバンに無いだけで起動を止めている"+
			"（自動化をやめて選択肢を消した人が抜け出せない）: %v", err)
	}
	logs := buf.String()
	if !strings.Contains(logs, "In Progres") {
		t.Fatalf("カンバンに無い対応表のキーの名前がログに出ていない:\n%s", logs)
	}
	if !strings.Contains(logs, "対応表からその行を消してください") {
		t.Fatalf("対応表から行を消す案内がログに出ていない（抜け出し方が分からない）:\n%s", logs)
	}
}

// TestVerifyStatusOptions_対応表のキーがカンバンから消えても巡回の照合は落ちない は、
// 設計 3-57 を確かめる（issue #67 の2件目）。
//
// 目的: **走っている最中に人間が選択肢を消すと、巡回ごとの照合が毎回落ちる。**
// 落ちた巡回は dispatch を丸ごと飛ばすので、**対応表の1行のためにカンバン全体が止まる。**
// 設定を直して再起動しないと戻らない。**キーはカンバンに実在しなくてよいので、落とさない。**
//
// 与える情報: カンバンに `Ice Box` がある状態で Bootstrap を通し、そのあと
// カンバンから `Ice Box` が消えた応答に切り替える（`Ice Box` は対応表のキーだけに出てくる）。
// 成功条件: VerifyStatusOptions がエラーを返さないこと。
func TestVerifyStatusOptions_対応表のキーがカンバンから消えても巡回の照合は落ちない(t *testing.T) {
	cfg := testTrackerConfig()
	cfg.AutomatedStateRewrite = map[string]string{"Ice Box": "In Progress"}
	// **1回目（Bootstrap）は `Ice Box` があり、2回目からは消えている。**
	withoutIceBox := testStatusOptions[1:]
	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		if n == 1 {
			return dataResponse(bootstrapProjectPayload(testStatusOptions))
		}
		return dataResponse(bootstrapProjectPayload(withoutIceBox))
	})

	a, err := tracker.NewAdapter(cfg, fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	if err := a.Bootstrap(t.Context(), cfg); err != nil {
		t.Fatalf("Bootstrap が失敗した: %v", err)
	}

	if err := a.VerifyStatusOptions(t.Context(), cfg); err != nil {
		t.Fatalf("対応表のキーがカンバンから消えただけで巡回の照合が落ちている"+
			"（この巡回の dispatch がカンバンごと飛ぶ）: %v", err)
	}
}

// TestBootstrap_カンバンに在る対応表のキーは起動も知らせも起こさない は、設計 3-57 を確かめる。
//
// 目的: **知らせるようにしたせいで、正しい設定にまで文句を言ってはならない。**
// カンバンに在るキーは打ち間違いでも消し忘れでもないので、何も起きないのが正しい。
//
// 与える情報: カンバンに実在する `Ice Box` をキーに書いた設定
// （`Ice Box` は設定の他のキーには出てこないので、対応表のキーとして正しい）。
// 成功条件: Bootstrap が成功し、**カンバンに無いキーの知らせが出ないこと。**
func TestBootstrap_カンバンに在る対応表のキーは起動も知らせも起こさない(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := testTrackerConfig()
	cfg.AutomatedStateRewrite = map[string]string{"Ice Box": "In Progress"}
	fs := newFakeGraphQLServer(t, single(dataResponse(bootstrapProjectPayload(testStatusOptions))))

	a, err := tracker.NewAdapter(cfg, fs.URL(), "test-token", nil, logger, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	if err := a.Bootstrap(t.Context(), cfg); err != nil {
		t.Fatalf("カンバンに在るキーなのに Bootstrap が失敗した: %v", err)
	}
	logs := buf.String()
	if strings.Contains(logs, "対応表からその行を消してください") {
		t.Fatalf("カンバンに在るキーなのに、対応表から消せと言っている:\n%s", logs)
	}
	// **`Ice Box` は対応表のキーなので、「continuo が知らない Status」にも数えない**
	// （キーの Status へ動かされた issue は書き戻されるのであって、worker は止まらない）。
	if strings.Contains(logs, "カンバンには continuo が知らない Status があります") {
		t.Fatalf("対応表のキーを「知らない Status」として名指ししている:\n%s", logs)
	}
}
