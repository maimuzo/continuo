// {"RUCM-CFG-SHA256": "96cbf29ae8081dd552a28e9a89189d1f7b9cec6846d6e3af5256607113005694", "SOURCE": "docs/spec/usecases/particular_case/リポジトリの親 workspace を閉じる.cfg.json"}
//
// **ユースケース記述「リポジトリの親 workspace を閉じる」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
// テストは `Manager.Cleanup` を直に呼ぶので、巡回が片付けの対象を選ぶ段は通らない。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package workspace_test

import (
	"context"
	"slices"
	"testing"

	"github.com/maimuzo/continuo/internal/herdr"
)

// {"RUCM-PATH": "P001"}
//
// 目的: continuo が開かせたリポジトリの親 workspace を、配下の worktree が無くなった
// あとに閉じることを確認する（issue #19）。
// 与える情報: 身元ファイルに herdr_repo_workspace_id として "wRepo" を書いた worktree と、
// 親 workspace 1件だけを返す workspace.list。
// 成功条件: worktree.remove のあとに workspace.close が "wRepo" 宛に1回だけ送られること。
func Test_リポジトリの親workspaceを閉じる_P001_continuoが開かせた親workspaceを閉じる(t *testing.T) {
	fx := newRepoWorkspaceFixture(t, "wRepo")
	fx.Herdr.SetResult(herdr.MethodWorkspaceList, workspaceListResult(
		workspaceEntry("wRepo", fx.RepoDir, fx.RepoDir),
	))

	result, err := fx.Manager.Cleanup(context.Background(), cleanupRequest(fx.cleanupFixture))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("worktree を消していない: %+v", *result)
	}
	if got := closedWorkspaceIDs(t, fx); !slices.Equal(got, []string{"wRepo"}) {
		t.Fatalf("閉じた親 workspace が想定と違う: got %v, want [wRepo]", got)
	}
	// **worktree の workspace を workspace.close で閉じてはならない**（worktree.remove が
	// 応答ごと閉じる。二重に閉じると別のものを閉じかねない）。
	if slices.Contains(closedWorkspaceIDs(t, fx), "w9") {
		t.Fatalf("worktree の workspace まで workspace.close で閉じている: %v", closedWorkspaceIDs(t, fx))
	}
}

// {"RUCM-PATH": "P006"}
//
// 目的: 人間が先に開いていたリポジトリの親 workspace を閉じないことを確認する（issue #19）。
// 与える情報: herdr_repo_workspace_id を書いていない worktree と、
// 親 workspace 1件だけを返す workspace.list。
// 成功条件: workspace.close を1回も送らないこと。
//
// **これを閉じると、その人が使っている pane ごと消える。**continuo が開かせたと
// 記録していない親には触らない。
func Test_リポジトリの親workspaceを閉じる_P006_人間が開いた親workspaceは閉じない(t *testing.T) {
	fx := newRepoWorkspaceFixture(t, "")
	fx.Herdr.SetResult(herdr.MethodWorkspaceList, workspaceListResult(
		workspaceEntry("wHuman", fx.RepoDir, fx.RepoDir),
	))

	if _, err := fx.Manager.Cleanup(context.Background(), cleanupRequest(fx.cleanupFixture)); err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if got := closedWorkspaceIDs(t, fx); len(got) != 0 {
		t.Fatalf("continuo が開かせていない親 workspace を閉じている: %v", got)
	}
}

// {"RUCM-PATH": "P003"}
//
// 目的: 同じリポジトリの別の worktree がまだ開いていれば、親 workspace を閉じないことを
// 確認する（issue #19）。
// 与える情報: herdr_repo_workspace_id を書いた worktree と、親 workspace に加えて
// 別の worktree の workspace も返す workspace.list。
// 成功条件: workspace.close を1回も送らないこと。
//
// **herdr 0.8.x では親を閉じると配下の worktree の workspace と pane も一緒に消える**ので、
// ここで閉じると別の issue の Claude Code が動いている pane が落ちる（0.9.0 以降は断られる）。
func Test_リポジトリの親workspaceを閉じる_P003_同じリポジトリのworktreeが残っていれば親workspaceを閉じない(t *testing.T) {
	fx := newRepoWorkspaceFixture(t, "wRepo")
	fx.Herdr.SetResult(herdr.MethodWorkspaceList, workspaceListResult(
		workspaceEntry("wRepo", fx.RepoDir, fx.RepoDir),
		workspaceEntry("wOther", fx.RepoDir+"/../other-worktree", fx.RepoDir),
	))

	if _, err := fx.Manager.Cleanup(context.Background(), cleanupRequest(fx.cleanupFixture)); err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if got := closedWorkspaceIDs(t, fx); len(got) != 0 {
		t.Fatalf("別の worktree がまだ開いているのに親 workspace を閉じている: %v", got)
	}
}

// {"RUCM-PATH": "P003"}
//
// 目的: 親 workspace を閉じずに残したとき、**閉じる責任を残っている worktree へ
// 書き移す**ことを確認する（issue #19）。
//
// **これが無いと、親 workspace は誰にも閉じられないまま残る。**リポジトリの親を
// 控えるのは、それを最初に開かせた1つの issue だけである（2件目以降は「先から
// あった」と見て空文字を書く）。**その1件が先に片付くと、ID はどこにも残らない。**
// agent.max_concurrent_agents の既定は2なので、同じリポジトリの issue を2件
// 並行して走らせれば、ふつうに起きる。
//
// 与える情報: herdr_repo_workspace_id に "wRepo" を書いた issue 188 の worktree、
// 同じリポジトリの issue 189 の worktree（その値は空）、親と 189 の workspace を
// 返す workspace.list。
//
// 成功条件: workspace.close を1回も送らず、**189 の身元ファイルの
// herdr_repo_workspace_id が "wRepo" になっている**こと。
func Test_リポジトリの親workspaceを閉じる_P003_親workspaceを閉じる責任を残ったworktreeへ渡す(t *testing.T) {
	fx := newRepoWorkspaceFixture(t, "wRepo")

	// 同じリポジトリの2件目。**親は既にあるので、この worktree は空文字を書く。**
	second := prepareWorktree(t, fx.managerFixture, sampleIssue(189))
	writeSecondIdentity(t, fx, second)

	fx.Herdr.SetResult(herdr.MethodWorkspaceList, workspaceListResult(
		workspaceEntry("wRepo", fx.RepoDir, fx.RepoDir),
		workspaceEntry("wOther", second.Path, fx.RepoDir),
	))

	if _, err := fx.Manager.Cleanup(context.Background(), cleanupRequest(fx.cleanupFixture)); err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if got := closedWorkspaceIDs(t, fx); len(got) != 0 {
		t.Fatalf("別の worktree がまだ開いているのに親 workspace を閉じている: %v", got)
	}

	identity, err := fx.Manager.ReadIdentity(second.Path)
	if err != nil {
		t.Fatalf("残った worktree の身元ファイルを読めない: %v", err)
	}
	if identity.HerdrRepoWorkspaceID != "wRepo" {
		t.Fatalf("親 workspace を閉じる責任を渡していない: got %q, want %q（この親は二度と閉じられない）",
			identity.HerdrRepoWorkspaceID, "wRepo")
	}
}

// {"RUCM-PATH": "P003"}
//
// 目的: 引き継ぎが**既に持っている値を上書きしない**ことを確認する（issue #19）。
//
// **上書きすると、別のリポジトリの親を閉じにいく身元ファイルを continuo 自身が作る。**
//
// 与える情報: herdr_repo_workspace_id に "wRepo" を書いた issue 188 の worktree と、
// 既に "wSomeoneElse" を持っている issue 189 の worktree。
//
// 成功条件: 189 の身元ファイルが "wSomeoneElse" のままであること。
func Test_リポジトリの親workspaceを閉じる_P003_引き継ぎは既にある親workspaceのIDを上書きしない(t *testing.T) {
	fx := newRepoWorkspaceFixture(t, "wRepo")

	second := prepareWorktree(t, fx.managerFixture, sampleIssue(189))
	writeSecondIdentity(t, fx, second)
	if err := fx.Manager.SetRepoWorkspaceID(context.Background(), second.Path, "wSomeoneElse"); err != nil {
		t.Fatalf("2件目の身元ファイルに親 workspace の ID を書けない: %v", err)
	}

	fx.Herdr.SetResult(herdr.MethodWorkspaceList, workspaceListResult(
		workspaceEntry("wRepo", fx.RepoDir, fx.RepoDir),
		workspaceEntry("wOther", second.Path, fx.RepoDir),
	))

	if _, err := fx.Manager.Cleanup(context.Background(), cleanupRequest(fx.cleanupFixture)); err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}

	identity, err := fx.Manager.ReadIdentity(second.Path)
	if err != nil {
		t.Fatalf("残った worktree の身元ファイルを読めない: %v", err)
	}
	if identity.HerdrRepoWorkspaceID != "wSomeoneElse" {
		t.Fatalf("既にある親 workspace の ID を上書きしている: got %q, want %q",
			identity.HerdrRepoWorkspaceID, "wSomeoneElse")
	}
}

// {"RUCM-PATH": "P004"}
//
// 目的: 身元ファイルの herdr_repo_workspace_id が herdr の現物と食い違えば閉じないことを
// 確認する（issue #19）。
// 与える情報: herdr_repo_workspace_id に "wSomeoneElse" を書いた worktree と、
// このリポジトリの親 workspace が "wRepo" である workspace.list。
// 成功条件: workspace.close を1回も送らないこと。
//
// **身元ファイルは worktree の直下にあり、エージェントが書き換えられる。**検算せずに
// 渡すと、同じ機械で動いている別のリポジトリの workspace を閉じさせられる。
func Test_リポジトリの親workspaceを閉じる_P004_親workspaceのIDが現物と食い違えば閉じない(t *testing.T) {
	fx := newRepoWorkspaceFixture(t, "wSomeoneElse")
	fx.Herdr.SetResult(herdr.MethodWorkspaceList, workspaceListResult(
		workspaceEntry("wRepo", fx.RepoDir, fx.RepoDir),
	))

	if _, err := fx.Manager.Cleanup(context.Background(), cleanupRequest(fx.cleanupFixture)); err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if got := closedWorkspaceIDs(t, fx); len(got) != 0 {
		t.Fatalf("身元ファイルの値が現物と食い違うのに閉じている: %v", got)
	}
}
