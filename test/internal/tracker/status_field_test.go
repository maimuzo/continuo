package tracker_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/tracker"
)

// customStatusFieldName は「既定の Status 以外」「空白を含む」の両方を満たすフィールド名である。
// docs/trying_it_out.md が案内している専用フィールドの名前と同じものを使う。
const customStatusFieldName = "continuo Status"

// customStatusFieldConfig は status_field を customStatusFieldName にした設定を返す。
func customStatusFieldConfig() config.TrackerConfig {
	cfg := testTrackerConfig()
	cfg.Provider.StatusField = customStatusFieldName
	return cfg
}

// bootstrapProjectPayloadWithCounts は Bootstrap の応答に、絞り込みキーの検査に使う
// 3つの件数（全件・値あり・値なし）を載せたものを組み立てる。
//
// total: ボード上の item の全件数（totalItems）。
// withValue: `-no:"<status_field>"` が返す件数（itemsWithStatus）。
// withoutValue: `no:"<status_field>"` が返す件数（itemsWithoutStatus）。
func bootstrapProjectPayloadWithCounts(
	options []map[string]any,
	total, withValue, withoutValue int,
) map[string]any {
	return map[string]any{
		"repositoryOwner": map[string]any{
			"projectV2": map[string]any{
				"id": "PVT_test",
				"field": map[string]any{
					"__typename": "ProjectV2SingleSelectField",
					"id":         "PVTSSF_test",
					"options":    options,
				},
				"totalItems":         map[string]any{"totalCount": total},
				"itemsWithStatus":    map[string]any{"totalCount": withValue},
				"itemsWithoutStatus": map[string]any{"totalCount": withoutValue},
			},
		},
	}
}

// 目的: 候補の絞り込みが `status:` の決め打ちではなく tracker.provider.status_field を
// キーに使うことを確認する（設計 3-34）。
// **これが回帰すると、専用フィールドを設定していても組み込みの Status を絞り込む。**
// 与える情報: status_field を "continuo Status"（空白を含む、既定以外の名前）にした設定で
// FetchIssuesByStates を呼ぶ。
// 成功条件: 送られた GraphQL 変数 `q` が `"continuo Status":"Ready","In Progress"` と
// 完全に一致すること（キーが引用符で囲まれ、値がカンマ区切りで並ぶこと）。
func TestFetchIssuesByStates_status_fieldを絞り込みのキーに使う(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nil, false, ""))))
	a, err := tracker.NewAdapter(customStatusFieldConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	if _, err := a.FetchIssuesByStates(t.Context(), []string{"Ready", "In Progress"}); err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}

	reqs := fs.Requests()
	if len(reqs) != 1 {
		t.Fatalf("リクエスト件数が想定と違う: got %d, want 1", len(reqs))
	}
	q, _ := reqs[0].Variables["q"].(string)
	want := `"continuo Status":"Ready","In Progress"`
	if q != want {
		t.Fatalf("検索クエリが status_field を使っていない:\n got %q\nwant %q", q, want)
	}
}

// 目的: 既定以外の status_field を設定したとき、組み込みの `status:` をキーにした
// クエリを送らないことを確認する（設計 3-34）。
// **`status:` で始まるクエリは、フィールド名の綴りに関わらず組み込みの Status を見る。**
// 与える情報: status_field を "continuo Status" にした設定。
// 成功条件: 送られた `q` が `status:` で始まらないこと。
func TestFetchIssuesByStates_既定以外なら組み込みのstatusキーを使わない(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nil, false, ""))))
	a, err := tracker.NewAdapter(customStatusFieldConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	if _, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"}); err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}

	q, _ := fs.Requests()[0].Variables["q"].(string)
	if strings.HasPrefix(q, "status:") {
		t.Fatalf("組み込みの status: をキーにしたクエリを送っている: %q", q)
	}
}

// 目的: 頼んだ Status に無い item が**大半を占めた**ら、設定の誤りとしてエラーにすることを
// 確認する（設計 3-34 の「無言の失敗を無くす」）。
// **絞り込みのキーが別のフィールドに解決されると、GraphQL は条件ごと無かったことにして
// ボードのほぼ全件を返す。**気づかないと Ice Box の issue を着手可能とみなして走らせる。
// 与える情報: Ready / In Progress を頼んだのに、4件のうち3件が "Ice Box" の item を返す偽サーバ。
// 成功条件: FetchIssuesByStates がエラーを返し、カテゴリが CategoryResponse であり、
// メッセージに返ってきた Status 名（"Ice Box"）が含まれること。
func TestFetchIssuesByStates_頼んでいないStatusが大半なら設定の誤りとしてエラーにする(t *testing.T) {
	nodes := []map[string]any{
		issueItemJSON(testIssueItemOpts{ItemID: "item-1", Status: "Ready", Owner: "maimuzo", Repo: "continuo", Number: 1, Title: "頼んだとおりの item"}),
		issueItemJSON(testIssueItemOpts{ItemID: "item-2", Status: "Ice Box", Owner: "maimuzo", Repo: "continuo", Number: 2, Title: "頼んでいない Status の item"}),
		issueItemJSON(testIssueItemOpts{ItemID: "item-3", Status: "Ice Box", Owner: "maimuzo", Repo: "continuo", Number: 3, Title: "頼んでいない Status の item"}),
		issueItemJSON(testIssueItemOpts{ItemID: "item-4", Status: "Ice Box", Owner: "maimuzo", Repo: "continuo", Number: 4, Title: "頼んでいない Status の item"}),
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nodes, false, ""))))
	a := newAdapterForFetch(t, fs)

	_, err := a.FetchIssuesByStates(t.Context(), []string{"Ready", "In Progress"})
	if err == nil {
		t.Fatalf("頼んでいない Status の item が大半なのに FetchIssuesByStates が成功した")
	}
	if !tracker.IsCategory(err, tracker.CategoryResponse) {
		t.Fatalf("エラーのカテゴリが CategoryResponse ではない: %v", err)
	}
	if !strings.Contains(err.Error(), "Ice Box") {
		t.Fatalf("エラーメッセージに返ってきた Status 名が含まれていない: %v", err)
	}
}

// 目的: 頼んだ Status に無い item が**少数**なら、その item だけを落として続けることを
// 確認する（設計 3-34）。
//
// **これは continuo 自身の書き込みが原因で起きる。**`items(query:)` の絞り込みは
// サーバ側の検索であり、Status を書いた直後に同じ巡回で取り直すと、索引は古い値で当たり、
// フィールドの読み出しは新しい値を返す。**1件の食い違いで一覧ごとエラーにすると、
// 正しく絞り込めていた他の issue の dispatch までその巡回で止まる。**
//
// 与える情報: Ready / In Progress を頼んだのに、4件のうち1件だけ "Blocked" の item を返す偽サーバ。
// 成功条件: エラーにならず、"Blocked" の item だけが結果から落ち、残り3件がそのまま返ること。
func TestFetchIssuesByStates_頼んでいないStatusが少数なら落として続ける(t *testing.T) {
	nodes := []map[string]any{
		issueItemJSON(testIssueItemOpts{ItemID: "item-1", Status: "Ready", Owner: "maimuzo", Repo: "continuo", Number: 1, Title: "頼んだとおりの item"}),
		issueItemJSON(testIssueItemOpts{ItemID: "item-2", Status: "Blocked", Owner: "maimuzo", Repo: "continuo", Number: 2, Title: "直前に Blocked へ落とした item"}),
		issueItemJSON(testIssueItemOpts{ItemID: "item-3", Status: "In Progress", Owner: "maimuzo", Repo: "continuo", Number: 3, Title: "走行中の item"}),
		issueItemJSON(testIssueItemOpts{ItemID: "item-4", Status: "Ready", Owner: "maimuzo", Repo: "continuo", Number: 4, Title: "次に着手する item"}),
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nodes, false, ""))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Ready", "In Progress"})
	if err != nil {
		t.Fatalf("1件の食い違いで一覧ごとエラーになった（他の issue の dispatch が止まる）: %v", err)
	}
	if len(issues) != 3 {
		t.Fatalf("件数が想定と違う: got %d, want 3", len(issues))
	}
	for _, issue := range issues {
		if issue.ID == "item-2" {
			t.Fatalf("頼んだ Status に無い item を候補に残している: %s（Status=%q）", issue.ID, issue.State)
		}
	}
}

// 目的: Bootstrap が「status_field を絞り込みのキーとして使えるか」を測るクエリを
// 同じリクエストで送ることを確認する（設計 3-34）。
// 与える情報: status_field を "continuo Status" にした設定。
// 成功条件: GraphQL 変数 withStatusQuery が `-no:"continuo Status"`、
// withoutStatusQuery が `no:"continuo Status"` であること
// （空白を含む名前が引用符で囲まれていること）。
func TestBootstrap_絞り込みキーの検査クエリを同じリクエストで送る(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(
		bootstrapProjectPayloadWithCounts(testStatusOptions, 105, 100, 5),
	)))
	a, err := tracker.NewAdapter(customStatusFieldConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	if err := a.Bootstrap(t.Context(), customStatusFieldConfig()); err != nil {
		t.Fatalf("Bootstrap が失敗した: %v", err)
	}

	reqs := fs.Requests()
	if len(reqs) != 1 {
		t.Fatalf("リクエスト件数が想定と違う: got %d, want 1（起動時の検査は1リクエストで済むはず）", len(reqs))
	}
	with, _ := reqs[0].Variables["withStatusQuery"].(string)
	without, _ := reqs[0].Variables["withoutStatusQuery"].(string)
	if with != `-no:"continuo Status"` {
		t.Fatalf("値ありを数えるクエリが想定と違う: got %q", with)
	}
	if without != `no:"continuo Status"` {
		t.Fatalf("値なしを数えるクエリが想定と違う: got %q", without)
	}
}

// 目的: status_field が絞り込みのキーとして解決できない場合、Bootstrap で起動を止めることを
// 確認する（設計 3-34 の「無言の失敗を無くす」）。
// **GitHub は知らないキーを見ると条件ごと無視するので、`no:` と `-no:` が両方とも
// 全件を返す。**この形を見たら、フィールド自体はボードにあっても絞り込みには使えない。
// 与える情報: 全件105・値あり105・値なし105（＝キーが無視されている形）を返す偽サーバ。
// 成功条件: Bootstrap がエラーを返し、カテゴリが CategoryInvalidConfig であり、
// メッセージに status_field の名前が含まれること。
func TestBootstrap_絞り込みのキーに使えないstatus_fieldを弾く(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(
		bootstrapProjectPayloadWithCounts(testStatusOptions, 105, 105, 105),
	)))
	a, err := tracker.NewAdapter(customStatusFieldConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	err = a.Bootstrap(t.Context(), customStatusFieldConfig())
	if err == nil {
		t.Fatalf("絞り込みのキーとして使えないのに Bootstrap が成功した")
	}
	if !tracker.IsCategory(err, tracker.CategoryInvalidConfig) {
		t.Fatalf("エラーのカテゴリが CategoryInvalidConfig ではない: %v", err)
	}
	if !strings.Contains(err.Error(), customStatusFieldName) {
		t.Fatalf("エラーメッセージに status_field の名前が含まれていない: %v", err)
	}
}

// 目的: 絞り込みのキーとして解決できている場合は Bootstrap を通すことを確認する
// （検査が誤検知しないこと）。
// 与える情報: 全件105・値あり100・値なし5（合計が全件と一致する形。project #3 の実測値）。
// 成功条件: Bootstrap がエラーを返さないこと。
func TestBootstrap_絞り込みのキーに使えるなら通す(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(
		bootstrapProjectPayloadWithCounts(testStatusOptions, 105, 100, 5),
	)))
	a, err := tracker.NewAdapter(customStatusFieldConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	if err := a.Bootstrap(t.Context(), customStatusFieldConfig()); err != nil {
		t.Fatalf("Bootstrap が失敗した: %v", err)
	}
}

// 目的: 全件が値ありでも（値なしが0なら）誤検知しないことを確認する。
// **「両方が全件と一致するか」で判定しているので、値ありだけが全件と一致する場合は通す。**
// 与える情報: 全件105・値あり105・値なし0（全部の item に Status が入っているボード）。
// 成功条件: Bootstrap がエラーを返さないこと。
func TestBootstrap_全件に値が入っていても誤検知しない(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(
		bootstrapProjectPayloadWithCounts(testStatusOptions, 105, 105, 0),
	)))
	a, err := tracker.NewAdapter(customStatusFieldConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	if err := a.Bootstrap(t.Context(), customStatusFieldConfig()); err != nil {
		t.Fatalf("Bootstrap が失敗した: %v", err)
	}
}

// 目的: item が1件も無いボードでは絞り込みキーの検査をしないことを確認する。
// **全件0のときは、キーを解決できていても件数が全部0になり、区別が付かない。**
// 判定できないものを落とすと、空のボードで起動できなくなる。
// 与える情報: 全件0・値あり0・値なし0。
// 成功条件: Bootstrap がエラーを返さないこと。
func TestBootstrap_item0件のボードでは絞り込みキーを検査しない(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(
		bootstrapProjectPayloadWithCounts(testStatusOptions, 0, 0, 0),
	)))
	a, err := tracker.NewAdapter(customStatusFieldConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	if err := a.Bootstrap(t.Context(), customStatusFieldConfig()); err != nil {
		t.Fatalf("Bootstrap が失敗した: %v", err)
	}
}

// 目的: 巡回ごとの検査（VerifyStatusOptions）では絞り込みキーの判定をしないことを確認する。
// **数えている最中に人間がボードへ item を足すと件数がずれる。**30秒ごとの巡回でこれを
// 判定に使うと、設定は正しいのに巡回が止まる。起動時に1回見れば足りる。
// 与える情報: 起動時なら弾かれる形（全件105・値あり105・値なし105）を返す偽サーバ。
// 成功条件: VerifyStatusOptions がエラーを返さないこと。
func TestVerifyStatusOptions_絞り込みキーの判定はしない(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(
		bootstrapProjectPayloadWithCounts(testStatusOptions, 105, 105, 105),
	)))
	a, err := tracker.NewAdapter(customStatusFieldConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	if err := a.VerifyStatusOptions(t.Context(), customStatusFieldConfig()); err != nil {
		t.Fatalf("VerifyStatusOptions が失敗した: %v", err)
	}
}

// 目的: ID 指定の取り直しと Status の書き込みが、既定以外の status_field でも
// そのフィールド名を GraphQL 変数として渡すことを確認する（回帰の網を絞り込み以外にも張る）。
// 与える情報: status_field を "continuo Status" にした設定で FetchIssuesByIDs を呼ぶ。
// 成功条件: GraphQL 変数 statusField が "continuo Status" であること。
func TestFetchIssuesByIDs_status_fieldをそのまま渡す(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(byIDsPayload([]any{nil}))))
	a, err := tracker.NewAdapter(customStatusFieldConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	if _, err := a.FetchIssuesByIDs(t.Context(), []string{"item-1"}); err != nil {
		t.Fatalf("FetchIssuesByIDs が失敗した: %v", err)
	}

	got, _ := fs.Requests()[0].Variables["statusField"].(string)
	if got != customStatusFieldName {
		t.Fatalf("statusField が想定と違う: got %q, want %q", got, customStatusFieldName)
	}
}
