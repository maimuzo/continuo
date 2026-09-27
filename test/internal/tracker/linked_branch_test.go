package tracker_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/tracker"
)

// fetchOneIssue は linkedBranches の形を1件だけ返す偽サーバを立て、その1件を返す。
//
// t: テスト。
// opts: 組み立てる project item のパラメータ。
// 戻り値: FetchIssuesByStates が返した Issue。
func fetchOneIssue(t *testing.T, opts testIssueItemOpts) tracker.Issue {
	t.Helper()
	nodes := []map[string]any{issueItemJSON(opts)}
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nodes, false, ""))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"})
	if err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("issue の件数が想定と違う: got %d, want 1", len(issues))
	}
	return issues[0]
}

// fetchOneIssueLog は fetchOneIssue と同じ形を取りに行き、そのあいだのログも返す。
//
// **捨てた記録が残っているかを見るためのものである。**リンクを捨てても dispatch は
// 止めないので、**気づく手掛かりはこのログの1行しかない**（docs/FAQ.md の
// 「issue に branch をリンクしたのに、worktree が既定 branch から始まる」）。
//
// t: テスト。
// opts: 組み立てる project item のパラメータ。
// 戻り値の1つ目: FetchIssuesByStates が返した Issue。
// 戻り値の2つ目: 取得のあいだに出たログの全文。
func fetchOneIssueLog(t *testing.T, opts testIssueItemOpts) (tracker.Issue, string) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	nodes := []map[string]any{issueItemJSON(opts)}
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nodes, false, ""))))
	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, logger, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"})
	if err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("issue の件数が想定と違う: got %d, want 1", len(issues))
	}
	return issues[0], buf.String()
}

// baseOpts は「Ready の issue が1件だけカンバンに載っている」形の共通のパラメータを返す。
func baseOpts() testIssueItemOpts {
	return testIssueItemOpts{
		ItemID: "item-42",
		Status: "Ready",
		Owner:  "octocat",
		Repo:   "hello-world",
		Number: 42,
		Title:  "リンクされた branch を起点にする",
		URL:    "https://github.com/octocat/hello-world/issues/42",
	}
}

// 目的: linkedBranches のクエリが `totalCount` と `ref.repository.nameWithOwner` を
// 要求していることを確認する（設計 3-22d）。
// **`totalCount` を取らないと「窓（first: 5）の外に6本目がある」ことに気づけず、
// `nameWithOwner` を取らないと別のリポジトリを指すリンクを見分けられない。**
// 与える情報: 通常の FetchIssuesByStates 呼び出し。
// 成功条件: 送られたクエリ本文に `linkedBranches(first: 5)` と `totalCount`、
// および ref の中の `repository { nameWithOwner }` が含まれること。
func TestFetchIssuesByStates_リンクされたbranchのクエリが総数とリポジトリ名を要求する(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nil, false, ""))))
	a := newAdapterForFetch(t, fs)

	if _, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"}); err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}

	reqs := fs.Requests()
	if len(reqs) != 1 {
		t.Fatalf("リクエスト件数が想定と違う: got %d, want 1", len(reqs))
	}
	want := "linkedBranches(first: 5) { totalCount nodes { ref { name repository { nameWithOwner } } } }"
	if !strings.Contains(reqs[0].Query, want) {
		t.Fatalf("クエリが想定の形を含んでいない\nwant を含むこと: %s\ngot: %s", want, reqs[0].Query)
	}
}

// 目的: リンクが0本のとき BranchName が nil のままであることを確認する（設計 3-22d）。
// **これが今までどおりの挙動であり、base は issue のリポジトリの既定 branch になる。**
// 与える情報: linkedBranches の nodes が空・totalCount が 0 の issue。
// 成功条件: Issue.BranchName が nil であること。
func TestFetchIssuesByStates_リンクが0本ならbranch名を持たない(t *testing.T) {
	issue := fetchOneIssue(t, baseOpts())

	if issue.BranchName != nil {
		t.Fatalf("リンクが0本なのに BranchName が入っている: %q", *issue.BranchName)
	}
}

// 目的: リンクがちょうど1本で、issue と同じリポジトリの branch なら、その名前が
// BranchName に入ることを確認する（設計 3-22d）。**この1本だけが base に使える形である。**
// 与える情報: `octocat/hello-world` の issue に、同じ `octocat/hello-world` の
// `work/issue-42` が1本だけリンクされている形。
// 成功条件: Issue.BranchName が `work/issue-42` であること。
func TestFetchIssuesByStates_同じリポジトリのリンク1本をbranch名にする(t *testing.T) {
	opts := baseOpts()
	opts.LinkedBranch = "work/issue-42"

	issue := fetchOneIssue(t, opts)

	if issue.BranchName == nil {
		t.Fatalf("BranchName が nil である（同じリポジトリのリンク1本は base に使えるはず）")
	}
	if *issue.BranchName != "work/issue-42" {
		t.Fatalf("BranchName が想定と違う: got %q, want %q", *issue.BranchName, "work/issue-42")
	}
}

// 目的: **別のリポジトリを指すリンクを無視する**ことを確認する（設計 3-22d）。
// **入れ忘れると全 issue が落ちる門である。**issue のリポジトリの clone には
// `origin/<その名前>` が存在しないので、base に据えると fetch と `git worktree add` が
// 落ち、その issue が failure_state へ行く。
// 与える情報: `octocat/hello-world` の issue に、fork の `contributor/hello-world` の
// branch が1本だけリンクされている形。
// 成功条件: Issue.BranchName が nil であること（今までどおり既定 branch を base にする）。
func TestFetchIssuesByStates_別のリポジトリを指すリンクを無視する(t *testing.T) {
	opts := baseOpts()
	opts.LinkedBranches = []map[string]any{
		linkedBranchNodeJSON("work/issue-42", "contributor/hello-world"),
	}

	issue := fetchOneIssue(t, opts)

	if issue.BranchName != nil {
		t.Fatalf("別のリポジトリを指すリンクが base に採られている: %q", *issue.BranchName)
	}
}

// 目的: リンクが2本以上あるときは BranchName を nil にすることを確認する（設計 3-22d）。
// **どれを選ぶか決められないので、1本目を勝手に採らない。**
// 与える情報: 同じリポジトリの branch が2本リンクされている形。
// 成功条件: Issue.BranchName が nil であること。
func TestFetchIssuesByStates_リンクが2本ならbranch名を持たない(t *testing.T) {
	opts := baseOpts()
	opts.LinkedBranches = []map[string]any{
		linkedBranchNodeJSON("work/issue-42", "octocat/hello-world"),
		linkedBranchNodeJSON("work/issue-42-alt", "octocat/hello-world"),
	}

	issue := fetchOneIssue(t, opts)

	if issue.BranchName != nil {
		t.Fatalf("リンクが2本あるのに BranchName が入っている: %q", *issue.BranchName)
	}
}

// 目的: **窓（first: 5）の外に6本目があるとき**も BranchName を nil にすることを
// 確認する（設計 3-22d）。**件数を len(nodes) で数えると見落とす形である。**
// 与える情報: nodes は同じリポジトリの1本だけだが、totalCount が 6 の応答。
// 成功条件: Issue.BranchName が nil であること。
func TestFetchIssuesByStates_総数が窓を超えるならbranch名を持たない(t *testing.T) {
	opts := baseOpts()
	opts.LinkedBranches = []map[string]any{
		linkedBranchNodeJSON("work/issue-42", "octocat/hello-world"),
	}
	opts.LinkedBranchTotalCount = 6

	issue := fetchOneIssue(t, opts)

	if issue.BranchName != nil {
		t.Fatalf("総数が6本なのに BranchName が入っている: %q", *issue.BranchName)
	}
}

// 目的: リンクの ref にリポジトリ名が付いてこなかったときも BranchName を nil に
// することを確認する（設計 3-22d）。**「同じである」と言い切れない以上、
// 今までどおり既定 branch を base にするほうへ倒す。**
// 与える情報: `ref` に `repository` が無いリンクが1本だけある形。
// 成功条件: Issue.BranchName が nil であること。
func TestFetchIssuesByStates_リポジトリ名の無いリンクを無視する(t *testing.T) {
	opts := baseOpts()
	opts.LinkedBranches = []map[string]any{
		linkedBranchNodeJSON("work/issue-42", ""),
	}

	issue := fetchOneIssue(t, opts)

	if issue.BranchName != nil {
		t.Fatalf("リポジトリ名の無いリンクが base に採られている: %q", *issue.BranchName)
	}
}

// 目的: **リンクを捨てたことをログに残す**ことを確認する（設計 3-22d）。
//
// **捨てても dispatch は止めない**（既定 branch へ倒して作業は進む）。
// **だから、リンクした人が気づく手掛かりはこの1行しかない。**
// docs/FAQ.md の「issue に branch をリンクしたのに、worktree が既定 branch から始まる」は
// `grep 'リンクされた branch' <ログの出力先>` を案内しているので、
// **この行が消えると、そこに書いた手順がそのまま空振りする。**
//
// 与える情報: `octocat/hello-world` の issue に、fork の `contributor/hello-world` の
// branch が1本だけリンクされている形。
// 成功条件: WARN の本文・issue の識別子・捨てた理由（別のリポジトリの名前）が
// すべてログに出ること。
func TestFetchIssuesByStates_捨てたリンクの理由をログに残す(t *testing.T) {
	opts := baseOpts()
	opts.LinkedBranches = []map[string]any{
		linkedBranchNodeJSON("work/issue-42", "contributor/hello-world"),
	}

	issue, logs := fetchOneIssueLog(t, opts)

	if issue.BranchName != nil {
		t.Fatalf("別のリポジトリを指すリンクが base に採られている: %q", *issue.BranchName)
	}
	if !strings.Contains(logs, "リンクされた branch を worktree の起点に使いませんでした") {
		t.Fatalf("捨てた記録がログに1行も出ていない（気づく手掛かりが無くなる）:\n%s", logs)
	}
	if !strings.Contains(logs, "octocat/hello-world#42") {
		t.Fatalf("どの issue で捨てたのかがログから分からない:\n%s", logs)
	}
	if !strings.Contains(logs, "contributor/hello-world") {
		t.Fatalf("捨てた理由がログから分からない:\n%s", logs)
	}
}

// 目的: **リンクが無いときは、捨てた記録を出さない**ことを確認する（設計 3-22d）。
//
// **リンクを付けていないカンバンでは、この行が全 issue ぶん毎巡回で流れることになる。**
// そうなると他の行が埋もれ、本当に捨てたときの1行も読まれなくなる。
//
// 与える情報: linkedBranches が0本の issue。
// 成功条件: 捨てた記録の本文がログに1度も出ないこと。
func TestFetchIssuesByStates_リンクが無ければ捨てた記録を出さない(t *testing.T) {
	_, logs := fetchOneIssueLog(t, baseOpts())

	if strings.Contains(logs, "リンクされた branch を worktree の起点に使いませんでした") {
		t.Fatalf("捨てていないのに捨てた記録が出ている:\n%s", logs)
	}
}
