package tracker_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/tracker"
)

// newAdapterForFetch は FetchIssuesByStates / FetchIssuesByIDs だけを使うテスト用に、
// Bootstrap を呼ばずに Adapter を作る（この2つのメソッドは Bootstrap を前提にしない）。
func newAdapterForFetch(t *testing.T, fs *fakeGraphQLServer) *tracker.Adapter {
	t.Helper()
	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	return a
}

// 目的: FetchIssuesByStates が active_states に入っている Status すべてを対象にした
// 検索クエリを1リクエストで送ること（`status:Ready` のように一部だけに絞らないこと）を
// 確認する（設計 4-2: 「status:Ready だけで絞ってはならない。In Progress が候補に
// 含まれないと、再起動後に取り残された issue を誰も拾えなくなる」）。
// 与える情報: active_states = ["Ready", "In Progress"] で FetchIssuesByStates を呼ぶ。
// 成功条件: 偽サーバが受け取ったリクエストが1件だけであり、その GraphQL 変数 `q` に
// "Ready" と "In Progress" の両方が含まれること。
func TestFetchIssuesByStates_active_states全部を対象にする(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nil, false, ""))))
	a := newAdapterForFetch(t, fs)

	if _, err := a.FetchIssuesByStates(t.Context(), []string{"Ready", "In Progress"}); err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}

	reqs := fs.Requests()
	if len(reqs) != 1 {
		t.Fatalf("リクエスト件数が想定と違う: got %d, want 1（候補の取得は1リクエストで済むはず）", len(reqs))
	}
	q, _ := reqs[0].Variables["q"].(string)
	if !strings.Contains(q, "Ready") || !strings.Contains(q, "In Progress") {
		t.Fatalf("検索クエリに active_states の両方が含まれていない: %q", q)
	}
}

// 目的: 送っているクエリ本文が Priority に類する GraphQL フィールドを一切要求していないこと
// （継続的に Priority を読まないことの保証）を確認する（設計 4-2）。
// 与える情報: 通常の FetchIssuesByStates 呼び出し。
// 成功条件: 偽サーバが受け取ったリクエストの query 文字列（大文字小文字を無視）に
// "riority" という部分文字列が含まれていないこと。
func TestFetchIssuesByStates_Priorityを読まない(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nil, false, ""))))
	a := newAdapterForFetch(t, fs)

	if _, err := a.FetchIssuesByStates(t.Context(), []string{"Ready", "In Progress"}); err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}

	reqs := fs.Requests()
	if len(reqs) != 1 {
		t.Fatalf("リクエスト件数が想定と違う: got %d, want 1", len(reqs))
	}
	if strings.Contains(strings.ToLower(reqs[0].Query), "riority") {
		t.Fatalf("クエリに Priority 相当のフィールドが含まれている: %s", reqs[0].Query)
	}
}

// 目的: 返ってきた配列の順序をそのまま使い、自前で並べ替えないことを確認する
// （設計 4-2: 「返ってきた配列の順にそのまま dispatch する。自前で並べ替えない」）。
// 与える情報: issue 番号の昇順でも辞書順でもない、あえて崩した順序
// （301, 099, 205）で items を返す偽サーバ。
// 成功条件: FetchIssuesByStates が返す Issue の Identifier の並びが、
// 偽サーバが返した順序（301 → 099 → 205）と完全に一致すること
// （ソートされていたら 099 → 205 → 301 になってしまうので、それとの違いで検出できる）。
func TestFetchIssuesByStates_並び順を保つ(t *testing.T) {
	nodes := []map[string]any{
		issueItemJSON(testIssueItemOpts{ItemID: "item-301", Status: "Ready", Owner: "octocat", Repo: "hello-world", Number: 301, Title: "3番目に大きい番号"}),
		issueItemJSON(testIssueItemOpts{ItemID: "item-099", Status: "Ready", Owner: "octocat", Repo: "hello-world", Number: 99, Title: "一番小さい番号"}),
		issueItemJSON(testIssueItemOpts{ItemID: "item-205", Status: "In Progress", Owner: "octocat", Repo: "hello-world", Number: 205, Title: "真ん中の番号"}),
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nodes, false, ""))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Ready", "In Progress"})
	if err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}
	if len(issues) != 3 {
		t.Fatalf("件数が想定と違う: got %d, want 3", len(issues))
	}
	wantOrder := []string{
		"octocat/hello-world#301",
		"octocat/hello-world#99",
		"octocat/hello-world#205",
	}
	for i, want := range wantOrder {
		if issues[i].Identifier != want {
			t.Fatalf("並び順が保たれていない: index=%d got %q, want %q（全体: %v）", i, issues[i].Identifier, want, identifiersOf(issues))
		}
	}
}

func identifiersOf(issues []tracker.Issue) []string {
	out := make([]string, len(issues))
	for i, iss := range issues {
		out[i] = iss.Identifier
	}
	return out
}

// 目的: ページングを跨いでも順序を保ったまま全件を集めることを確認する。
// 与える情報: hasNextPage=true で1件返す1ページ目と、hasNextPage=false で1件返す
// 2ページ目を用意した偽サーバ（endCursor の値が2ページ目のリクエストに渡ることも確認する）。
// 成功条件: 2リクエスト発生し、返る Issue が2件・順序どおりであること。
func TestFetchIssuesByStates_ページングを跨いで順序を保つ(t *testing.T) {
	page1 := []map[string]any{
		issueItemJSON(testIssueItemOpts{ItemID: "item-1", Status: "Ready", Owner: "octocat", Repo: "hello-world", Number: 1, Title: "1ページ目"}),
	}
	page2 := []map[string]any{
		issueItemJSON(testIssueItemOpts{ItemID: "item-2", Status: "Ready", Owner: "octocat", Repo: "hello-world", Number: 2, Title: "2ページ目"}),
	}

	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		if n == 1 {
			return dataResponse(candidateItemsPayload(page1, true, "cursor-1"))
		}
		if after, _ := req.Variables["after"].(string); after != "cursor-1" {
			t.Errorf("2回目のリクエストに1ページ目の endCursor が渡っていない: got %v", req.Variables["after"])
		}
		return dataResponse(candidateItemsPayload(page2, false, ""))
	})
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"})
	if err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}
	if fs.RequestCount() != 2 {
		t.Fatalf("リクエスト件数が想定と違う: got %d, want 2", fs.RequestCount())
	}
	want := []string{"octocat/hello-world#1", "octocat/hello-world#2"}
	got := identifiersOf(issues)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("並び順が保たれていない: got %v, want %v", got, want)
	}
}

// 目的: draft issue が dispatchable=false のまま候補の一覧に残ることを確認する
// （設計 3-13: 「取得の段では落とさず、dispatch の判定で落とす」）。
// 与える情報: content が DraftIssue の item を1件含む偽サーバ応答。
// 成功条件: 返ってきた Issue の数が1件で、Dispatchable が false、
// Identifier が "draft:" で始まること。
func TestFetchIssuesByStates_draftIssueはdispatchableがfalseで残る(t *testing.T) {
	nodes := []map[string]any{
		draftItemJSON("item-draft-1", "Ice Box", "下書きのタイトル", "本文"),
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nodes, false, ""))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Ice Box"})
	if err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("draft issue が一覧から落ちている: got %d件, want 1件", len(issues))
	}
	if issues[0].Dispatchable {
		t.Fatalf("draft issue の Dispatchable が true になっている")
	}
	if !strings.HasPrefix(issues[0].Identifier, "draft:") {
		t.Fatalf("draft issue の Identifier が想定の形ではない: %q", issues[0].Identifier)
	}
}

// 目的: Status 未設定の item は一覧の取得では省いてログに残す（エラーにはしない）ことを
// 確認する（設計 3-13: 「一覧の取得では省略してログに残す」）。
// 与える情報: Status が設定された item 1件と、fieldValueByName が無い（未設定の）item 1件。
// 成功条件: 返ってきた Issue が1件だけであり、それが Status 設定済みの item であること
// （エラーにはならないこと）。
func TestFetchIssuesByStates_Status未設定のitemは省く(t *testing.T) {
	withStatus := issueItemJSON(testIssueItemOpts{ItemID: "item-ok", Status: "Ready", Owner: "octocat", Repo: "hello-world", Number: 1, Title: "Status あり"})
	withoutStatus := issueItemJSON(testIssueItemOpts{ItemID: "item-nostatus", Status: "", Owner: "octocat", Repo: "hello-world", Number: 2, Title: "Status 未設定"})

	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload([]map[string]any{withStatus, withoutStatus}, false, ""))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"})
	if err != nil {
		t.Fatalf("Status 未設定の item があるだけでエラーになった: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("件数が想定と違う: got %d, want 1（Status 未設定の item は省かれるはず）", len(issues))
	}
	if issues[0].Identifier != "octocat/hello-world#1" {
		t.Fatalf("残った item が想定と違う: %q", issues[0].Identifier)
	}
}

// 目的: ラベルが正規化されることを確認する（前後の空白を落として小文字にし、
// 空のラベルは捨て、重複は取り除く。設計 3-13 / SPEC.md 11.3）。
// 与える情報: " Bug " "bug"（重複・空白違い）、大文字の "URGENT"、空文字のラベルを持つ item。
// 成功条件: 返ってきた Labels が ["bug", "urgent"] のように、正規化・重複除去された結果に
// なっていること。
func TestFetchIssuesByStates_ラベルを正規化する(t *testing.T) {
	nodes := []map[string]any{
		issueItemJSON(testIssueItemOpts{
			ItemID: "item-labels", Status: "Ready", Owner: "octocat", Repo: "hello-world", Number: 1,
			Title:  "ラベルのテスト",
			Labels: []string{" Bug ", "bug", "URGENT", "", "  "},
		}),
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nodes, false, ""))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"})
	if err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("件数が想定と違う: got %d, want 1", len(issues))
	}
	got := issues[0].Labels
	want := []string{"bug", "urgent"}
	if len(got) != len(want) {
		t.Fatalf("ラベルの正規化結果が想定と違う: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ラベルの正規化結果が想定と違う: got %v, want %v", got, want)
		}
	}
}

// 目的: Priority フィールドは Go の Issue 構造体上も常に nil であることを確認する
// （クエリで要求していないので、偽サーバが仮に priority 相当のデータを含めても
// 反映されないはずである。設計 4-2）。
// 与える情報: 通常の item 1件。
// 成功条件: 返ってきた Issue の Priority が nil であること。
func TestFetchIssuesByStates_Priorityフィールドは常にnil(t *testing.T) {
	nodes := []map[string]any{
		issueItemJSON(testIssueItemOpts{ItemID: "item-1", Status: "Ready", Owner: "octocat", Repo: "hello-world", Number: 1, Title: "t"}),
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nodes, false, ""))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"})
	if err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}
	if issues[0].Priority != nil {
		t.Fatalf("Priority が nil ではない: %v", *issues[0].Priority)
	}
}

// 目的: states が空のときは GraphQL へリクエストを送らずに空の結果を返すことを確認する
// （SPEC.md 11.1: "An empty state_names list MUST return an empty result without a provider
// request."）。
// 与える情報: 空のスライスで FetchIssuesByStates を呼ぶ。
// 成功条件: エラーが無く、結果が空であり、偽サーバへのリクエストが0件であること。
func TestFetchIssuesByStates_空のstatesはリクエストを送らない(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nil, false, ""))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByStates(t.Context(), nil)
	if err != nil {
		t.Fatalf("空の states でエラーになった: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("空の states なのに結果が返った: %v", issues)
	}
	if fs.RequestCount() != 0 {
		t.Fatalf("空の states なのにリクエストが送られた: %d件", fs.RequestCount())
	}
}

// 目的: 候補取得のクエリが並び順を明示していること（サーバの既定値に任せていないこと）を
// 確認する（設計 4-2: 「返ってきた配列の順にそのまま dispatch する。自前で並べ替えない」。
// 実行順序の全部をこの順序に賭けているので、既定値が変わったときに黙って崩れてはならない）。
// 与える情報: 通常の FetchIssuesByStates 呼び出し。
// 成功条件: 送信したクエリ本文に "POSITION" と "orderBy" が含まれること。
func TestFetchIssuesByStates_並び順をPOSITIONで明示する(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nil, false, ""))))
	a := newAdapterForFetch(t, fs)

	if _, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"}); err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}

	reqs := fs.Requests()
	if len(reqs) != 1 {
		t.Fatalf("リクエスト件数が想定と違う: got %d, want 1", len(reqs))
	}
	if !strings.Contains(reqs[0].Query, "orderBy") || !strings.Contains(reqs[0].Query, "POSITION") {
		t.Fatalf("候補取得のクエリが並び順を明示していない（サーバの既定値任せになっている）: %s", reqs[0].Query)
	}
}

// 目的: 候補の取得でもカンバンのページ数に上限があることを確認する
// （レビュー指摘「FetchIssuesByStates のページングにも上限が無い」の回帰テスト）。
//
// **巡回は30秒ごとに走る。**ページが尽きない応答（provider 側の異常や、想定外に育った
// カンバン）に当たると、1回の巡回が終わらなくなり GitHub の API 枠を食い潰す。
//
// 与える情報: 常に hasNextPage が真の応答を返す偽サーバ。
// 成功条件: CategoryPagination のエラーで止まること。読み続けないこと。
func TestFetchIssuesByStates_ページ数の上限を超えたら落とす(t *testing.T) {
	item := issueItemJSON(testIssueItemOpts{
		ItemID: "item-1", Status: "Ready", Owner: "octocat", Repo: "hello-world", Number: 1,
		Title: "候補", URL: "https://github.com/octocat/hello-world/issues/1",
	})
	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		return dataResponse(candidateItemsPayload([]map[string]any{item}, true, "cursor"))
	})
	a := newAdapterForFetch(t, fs)

	_, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"})
	if err == nil {
		t.Fatalf("ページが尽きないのに止まらなかった（上限が効いていない）")
	}
	if !tracker.IsCategory(err, tracker.CategoryPagination) {
		t.Fatalf("エラーの分類が想定と違う: %v", err)
	}
	if fs.RequestCount() > 25 {
		t.Fatalf("上限を超えて読み続けている: %d リクエスト", fs.RequestCount())
	}
}
