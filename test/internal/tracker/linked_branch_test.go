package tracker_test

// {"RUCM-CFG-SHA256": "5ba01b4a174c146c45e05e581754e126e476934e66380f1e243886859c4b3419", "SOURCE": "docs/spec/usecases/particular_case/issue にリンクされた branch を起点にして着手する.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**リンクの本数で base の決まり方が
// 変わる分岐と、着手しない分岐に印を付けてある。

import (
	"strings"
	"testing"
)

// linkedRef は `linkedBranches.nodes[].ref` 1件を組み立てる。
//
// name: branch の名前。
// nameWithOwner: その branch が在るリポジトリ（`<owner>/<repo>`）。
// url: そのリポジトリの URL（置き場所の1階層目を取る元）。
// defaultBranch: そのリポジトリの既定 branch。**空なら `defaultBranchRef` を返さない。**
// parent: fork の派生元（`<owner>/<repo>`）。**空なら `parent` を返さない。**
// 戻り値: 応答の1件。
func linkedRef(name, nameWithOwner, url, defaultBranch, parent string) map[string]any {
	repository := map[string]any{"nameWithOwner": nameWithOwner, "url": url}
	if defaultBranch != "" {
		repository["defaultBranchRef"] = map[string]any{"name": defaultBranch}
	}
	if parent != "" {
		repository["parent"] = map[string]any{"nameWithOwner": parent}
	}
	return map[string]any{"ref": map[string]any{"name": name, "repository": repository}}
}

// withLinkedBranches は issueItemJSON の戻り値の `linkedBranches` を差し替える。
//
// item: issueItemJSON の戻り値。
// totalCount: `linkedBranches.totalCount`。**窓に収まらない本数を再現するために別に渡す。**
// refs: linkedRef の並び。
// 戻り値: 差し替えた item。
func withLinkedBranches(item map[string]any, totalCount int, refs ...map[string]any) map[string]any {
	content, _ := item["content"].(map[string]any)
	content["linkedBranches"] = map[string]any{"totalCount": totalCount, "nodes": refs}
	return item
}

// 目的: 候補の取得のクエリが、リンクされた branch のリポジトリまで要求していることを確認する
// （#144（worktree の branch は変えず push 先だけ分ける）の設計 11）。
//
// **要求していなければ、コードのリポジトリは永久に issue のリポジトリのままになる。**
// **`totalCount` も要る。**取らないと、窓（`first: 5`）に入らなかった6本目が
// 別のリポジトリを指していても気づけない。
//
// 与える情報: 通常の FetchIssuesByStates の呼び出し。
// 成功条件: 偽サーバが受け取った query 文字列に
// `linkedBranches(first: 5)` と `totalCount` と `parent` が入っていること。
func TestFetchIssuesByStates_リンクされたbranchのリポジトリまで取る(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nil, false, ""))))
	a := newAdapterForFetch(t, fs)

	if _, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"}); err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}

	reqs := fs.Requests()
	if len(reqs) != 1 {
		t.Fatalf("リクエスト件数が想定と違う: got %d, want 1", len(reqs))
	}
	for _, want := range []string{"linkedBranches(first: 5)", "totalCount", "parent"} {
		if !strings.Contains(reqs[0].Query, want) {
			t.Fatalf("クエリが %q を要求していない: %s", want, reqs[0].Query)
		}
	}
}

// 目的: リンクが0本のとき、コードのリポジトリが issue のリポジトリと同じになることを確認する
// （#144（worktree の branch は変えず push 先だけ分ける）の設計 11a）。
//
// **ここが変わると、既存の worktree が全部別の場所を指す。**リンクを張っていない利用者の
// 置き場所は1つも動いてはならない。
//
// 与える情報: `linkedBranches` が空の issue。
// 成功条件: コードのリポジトリ・ホスト・PR の宛先が issue の側の値になり、
// `BranchName` が nil で、`CodeRepoUndecided` が偽であること。
func TestFetchIssuesByStates_リンクが0本ならコードのリポジトリはissueのものになる(t *testing.T) {
	nodes := []map[string]any{
		issueItemJSON(testIssueItemOpts{
			ItemID: "PVTI_none", Status: "Ready", Owner: "octocat", Repo: "hello-world",
			Number: 1, Title: "リンク無し", URL: "https://github.com/octocat/hello-world/issues/1",
		}),
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nodes, false, ""))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"})
	if err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("issue の件数が想定と違う: got %d, want 1", len(issues))
	}
	got := issues[0]
	if got.CodeRepoNameWithOwner != "octocat/hello-world" {
		t.Errorf("コードのリポジトリが違う: got %q", got.CodeRepoNameWithOwner)
	}
	if got.CodeRepoHost != "github.com" {
		t.Errorf("コードのリポジトリのホストが違う: got %q", got.CodeRepoHost)
	}
	if got.CodeRepoDefaultBranch != "main" {
		t.Errorf("コードのリポジトリの既定 branch が違う: got %q", got.CodeRepoDefaultBranch)
	}
	if got.PRTarget != "octocat/hello-world" {
		t.Errorf("PR の宛先が違う: got %q", got.PRTarget)
	}
	if got.BranchName != nil {
		t.Errorf("リンクが0本なのに BranchName が埋まっている: %q", *got.BranchName)
	}
	if got.CodeRepoUndecided {
		t.Error("リンクが0本なのに着手しない判定になっている")
	}
}

// 目的: リンクが1本のとき、その branch と、そのリポジトリの値が入ることを確認する
// （#144（worktree の branch は変えず push 先だけ分ける）の設計 11 / 11a）。
//
// **fork の branch を private の issue からリンクできる。**そのとき PR の宛先は
// fork の派生元（upstream）である。
//
// 与える情報: issue は `myorg/internal-tasks`、リンクは fork の `myorg/project` の1本。
// 成功条件: コードのリポジトリが fork、PR の宛先が派生元、`BranchName` がその branch であること。
func TestFetchIssuesByStates_リンクが1本ならそのbranchをbaseの手掛かりにする(t *testing.T) {
	nodes := []map[string]any{
		withLinkedBranches(issueItemJSON(testIssueItemOpts{
			ItemID: "PVTI_one", Status: "Ready", Owner: "myorg", Repo: "internal-tasks",
			Number: 42, Title: "fork で直す",
			URL: "https://github.com/myorg/internal-tasks/issues/42",
		}), 1, linkedRef("work/issue-42", "myorg/project",
			"https://github.com/myorg/project", "develop", "upstream-org/project")),
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nodes, false, ""))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"})
	if err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("issue の件数が想定と違う: got %d, want 1", len(issues))
	}
	got := issues[0]
	if got.CodeRepoNameWithOwner != "myorg/project" {
		t.Errorf("コードのリポジトリが違う: got %q", got.CodeRepoNameWithOwner)
	}
	if got.CodeRepoDefaultBranch != "develop" {
		t.Errorf("コードのリポジトリの既定 branch が違う: got %q", got.CodeRepoDefaultBranch)
	}
	if got.PRTarget != "upstream-org/project" {
		t.Errorf("PR の宛先が派生元になっていない: got %q", got.PRTarget)
	}
	if got.BranchName == nil || *got.BranchName != "work/issue-42" {
		t.Errorf("リンクされた branch が入っていない: %v", got.BranchName)
	}
	if got.CodeRepoUndecided {
		t.Error("1本しかリンクされていないのに着手しない判定になっている")
	}
	owner, repo := got.CodeOwnerRepo()
	if owner != "myorg" || repo != "project" {
		t.Errorf("コードのリポジトリを割れていない: got %q / %q", owner, repo)
	}
}

// 目的: 同じリポジトリの branch が2本以上リンクされていたら、リンクを base に使わないことを
// 確認する（#144（worktree の branch は変えず push 先だけ分ける）の設計 11a）。
//
// **どちらを起点にすべきかは continuo には決められない。**だが**どのリポジトリで作業するかは
// 決まっている**ので、着手はする。base は既定 branch へ倒す。
//
// **`BranchName` を nil にすること自体が要件である。**1本目を残すと、
// プロンプトの `.push_branch` にその名前が載り、**押し付けられた branch へ push させてしまう。**
//
// 与える情報: 同じリポジトリの branch が2本リンクされた issue。
// 成功条件: `BranchName` が nil、コードのリポジトリはそのリポジトリ、着手しない判定にならないこと。
// {"RUCM-PATH": "P004"}
func TestFetchIssuesByStates_同じリポジトリに2本リンクされていたらbaseに使わない(t *testing.T) {
	nodes := []map[string]any{
		withLinkedBranches(issueItemJSON(testIssueItemOpts{
			ItemID: "PVTI_two", Status: "Ready", Owner: "octocat", Repo: "hello-world",
			Number: 7, Title: "2本", URL: "https://github.com/octocat/hello-world/issues/7",
		}), 2,
			linkedRef("feature/a", "octocat/hello-world",
				"https://github.com/octocat/hello-world", "main", ""),
			linkedRef("feature/b", "octocat/hello-world",
				"https://github.com/octocat/hello-world", "main", ""),
		),
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nodes, false, ""))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"})
	if err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}
	got := issues[0]
	if got.BranchName != nil {
		t.Errorf("2本あるのに1本目を base の手掛かりにしている: %q", *got.BranchName)
	}
	if got.CodeRepoNameWithOwner != "octocat/hello-world" {
		t.Errorf("コードのリポジトリが違う: got %q", got.CodeRepoNameWithOwner)
	}
	if got.CodeRepoUndecided {
		t.Error("同じリポジトリの2本で着手しない判定になっている")
	}
}

// 目的: 別々のリポジトリの branch が2本リンクされていたら、着手しない判定になることを確認する
// （#144（worktree の branch は変えず push 先だけ分ける）の設計 11a）。
//
// **勝手にどちらかを選ぶと、別のリポジトリで作業を始めてしまう。**
//
// 与える情報: 別々のリポジトリの branch が2本リンクされた issue。
// 成功条件: `CodeRepoUndecided` が真で、**リンクの中身が人間へ見せられる形で残っている**こと。
// {"RUCM-PATH": "P009"}
func TestFetchIssuesByStates_別々のリポジトリに2本リンクされていたら着手しない(t *testing.T) {
	nodes := []map[string]any{
		withLinkedBranches(issueItemJSON(testIssueItemOpts{
			ItemID: "PVTI_split", Status: "Ready", Owner: "myorg", Repo: "internal-tasks",
			Number: 42, Title: "別々", URL: "https://github.com/myorg/internal-tasks/issues/42",
		}), 2,
			linkedRef("work/issue-42", "myorg/project",
				"https://github.com/myorg/project", "main", ""),
			linkedRef("hotfix/issue-42", "other-org/project",
				"https://github.com/other-org/project", "main", ""),
		),
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nodes, false, ""))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"})
	if err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}
	got := issues[0]
	if !got.CodeRepoUndecided {
		t.Fatal("別々のリポジトリを指す2本があるのに着手しない判定になっていない")
	}
	if got.BranchName != nil {
		t.Errorf("決められないのに base の手掛かりが入っている: %q", *got.BranchName)
	}
	if len(got.LinkedBranches) != 2 {
		t.Fatalf("人間へ見せるリンクの一覧が揃っていない: %+v", got.LinkedBranches)
	}
	if got.LinkedBranches[0].NameWithOwner != "myorg/project" ||
		got.LinkedBranches[1].NameWithOwner != "other-org/project" {
		t.Errorf("リンクの一覧の中身が違う: %+v", got.LinkedBranches)
	}
}

// 目的: リンクが取得の窓に収まらないときも、着手しない判定になることを確認する
// （#144（worktree の branch は変えず push 先だけ分ける）の設計 11a）。
//
// **見えている先頭がたまたま同じリポジトリでも、見えていない6本目が別かもしれない。**
// **「別々でない」と言えないなら、決めてはならない。**
//
// 与える情報: `totalCount` が 6 で、返ってきた node は1件だけの issue。
// 成功条件: `CodeRepoUndecided` が真であること。
// {"RUCM-PATH": "P010"}
func TestFetchIssuesByStates_リンクが窓に収まらなければ着手しない(t *testing.T) {
	nodes := []map[string]any{
		withLinkedBranches(issueItemJSON(testIssueItemOpts{
			ItemID: "PVTI_many", Status: "Ready", Owner: "octocat", Repo: "hello-world",
			Number: 9, Title: "たくさん", URL: "https://github.com/octocat/hello-world/issues/9",
		}), 6, linkedRef("feature/a", "octocat/hello-world",
			"https://github.com/octocat/hello-world", "main", "")),
	}
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nodes, false, ""))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"})
	if err != nil {
		t.Fatalf("FetchIssuesByStates が失敗した: %v", err)
	}
	if !issues[0].CodeRepoUndecided {
		t.Fatal("窓に収まらない本数がリンクされているのに着手しない判定になっていない")
	}
}
