package tracker_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/tracker"
)

// renamedStatusOptions は「人間が GitHub の画面で Ready を Todo へ改名した」あとの
// 選択肢一覧である（testStatusOptions の Ready だけを差し替えたもの）。
var renamedStatusOptions = []map[string]any{
	{"id": "opt-icebox", "name": "Ice Box"},
	{"id": "opt-ready", "name": "Todo"},
	{"id": "opt-inprogress", "name": "In Progress"},
	{"id": "opt-blocked", "name": "Blocked"},
	{"id": "opt-inreview", "name": "In Review"},
	{"id": "opt-done", "name": "Done"},
}

// 目的: ボード側に存在しない Status 名を渡されたら、0件ではなくエラーを返すことを確認する
// （設計 2-2 / 3-6: 「選択肢名を間違えると、GraphQL はエラーを出さずに 0 件を返す」。
// これを素通しすると「対象0件」が無言で永久に続く）。
// 与える情報: Bootstrap 済みの Adapter に、ボードに無い Status 名 "Redy"（綴り違い）を渡す。
// 成功条件: CategoryInvalidConfig のエラーになり、空スライスを返さないこと。
// 候補取得のリクエストが送られないこと（Bootstrap の1件だけであること）。
func TestFetchIssuesByStates_ボードに無いStatus名はエラーにする(t *testing.T) {
	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		if n == 1 {
			return dataResponse(bootstrapProjectPayload(testStatusOptions))
		}
		t.Errorf("ボードに無い Status 名なのに候補取得のリクエストが送られた: %v", req.Variables)
		return dataResponse(candidateItemsPayload(nil, false, ""))
	})
	a := newBootstrappedAdapter(t, fs)

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Redy", "In Progress"})
	if err == nil {
		t.Fatalf("ボードに無い Status 名を渡したのにエラーにならなかった（返り値: %v件）", len(issues))
	}
	if issues != nil {
		t.Fatalf("エラーなのに結果を返している: %v", issues)
	}
	if !tracker.IsCategory(err, tracker.CategoryInvalidConfig) {
		t.Fatalf("エラーのカテゴリが CategoryInvalidConfig ではない: %v", err)
	}
	if !strings.Contains(err.Error(), "Redy") {
		t.Fatalf("エラーメッセージに、見つからなかった Status 名が含まれていない: %v", err)
	}
	if fs.RequestCount() != 1 {
		t.Fatalf("リクエスト件数が想定と違う: got %d, want 1（Bootstrap の1件だけのはず）", fs.RequestCount())
	}
}

// 目的: Bootstrap も VerifyStatusOptions も呼んでいない Adapter では、Status 名の照合を
// 行わない（照合に使う選択肢の一覧を持っていないため）ことを確認する。
// 与える情報: Bootstrap を呼んでいない Adapter に、任意の Status 名を渡す。
// 成功条件: エラーにならず、候補取得のリクエストが送られること。
func TestFetchIssuesByStates_Bootstrap前は照合しない(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nil, false, ""))))
	a := newAdapterForFetch(t, fs)

	if _, err := a.FetchIssuesByStates(t.Context(), []string{"Redy"}); err != nil {
		t.Fatalf("Bootstrap 前なのに照合が走ってエラーになった: %v", err)
	}
	if fs.RequestCount() != 1 {
		t.Fatalf("リクエスト件数が想定と違う: got %d, want 1", fs.RequestCount())
	}
}

// 目的: 巡回ごとの再照合（VerifyStatusOptions）が、ボード側で Status を改名されたことを
// 検知することを確認する（設計 3-6 の「巡回ごとに検査するもの」: 「人間が GitHub の画面で
// 改名すると、無言で『対象0件』になり続けるため」）。
// 与える情報: Bootstrap の時点では設定どおりの選択肢を返し、2回目（再照合）では
// Ready が Todo に改名された選択肢を返す偽サーバ。
// 成功条件: Bootstrap は成功し、VerifyStatusOptions が CategoryInvalidConfig のエラーを
// 返すこと。エラーメッセージに一致しなかった "Ready" が含まれること。
func TestVerifyStatusOptions_改名を検知する(t *testing.T) {
	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		if n == 1 {
			return dataResponse(bootstrapProjectPayload(testStatusOptions))
		}
		return dataResponse(bootstrapProjectPayload(renamedStatusOptions))
	})
	a := newBootstrappedAdapter(t, fs)

	err := a.VerifyStatusOptions(t.Context(), testTrackerConfig())
	if err == nil {
		t.Fatalf("ボード側で Ready が改名されたのに VerifyStatusOptions が成功した")
	}
	if !tracker.IsCategory(err, tracker.CategoryInvalidConfig) {
		t.Fatalf("エラーのカテゴリが CategoryInvalidConfig ではない: %v", err)
	}
	if !strings.Contains(err.Error(), "Ready") {
		t.Fatalf("エラーメッセージに、一致しなかった Status 名が含まれていない: %v", err)
	}
}

// 目的: 改名が無ければ VerifyStatusOptions は成功し、以後の FetchIssuesByStates も
// これまでどおり動くことを確認する。
// 与える情報: 毎回同じ選択肢を返す偽サーバ。
// 成功条件: VerifyStatusOptions がエラーにならないこと。
func TestVerifyStatusOptions_一致していれば成功する(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(bootstrapProjectPayload(testStatusOptions))))
	a := newBootstrappedAdapter(t, fs)

	if err := a.VerifyStatusOptions(t.Context(), testTrackerConfig()); err != nil {
		t.Fatalf("選択肢が一致しているのに VerifyStatusOptions が失敗した: %v", err)
	}
}
