// **リポジトリの親 workspace を閉じるかどうか**の分岐を確かめるテストである。
// ユースケース記述「リポジトリの親workspaceを閉じる」の経路に対応づけたテストは `リポジトリの親workspaceを閉じる_test.go` に在る。
package workspace_test

import (
	"context"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/workspace"
)

// リポジトリの親 workspace の検査（issue #19）。
//
// **`worktree.open` は herdr の workspace を2つ開く。**worktree のぶんと、`cwd` に渡した
// リポジトリのぶん（**リポジトリの親 workspace**）である。`worktree.remove` は後者を
// 閉じないので、閉じるのは continuo の仕事になる。
//
// **閉じてよい条件は2つあり、両方満たすときだけ閉じる。**
//
//	1 continuo がその親を開かせたこと（身元ファイルの herdr_repo_workspace_id が空でない）
//	2 その親の下に worktree の workspace が1つも残っていないこと
//
// **どちらを落としても人の pane が消える。**1 を落とすと人間が自分で開いた workspace を、
// 2 を落とすと別の issue が使っている worktree の workspace を閉じる
// （**herdr 0.8.x では、親を閉じると配下も一緒に消えた**（2026-08-25 に本物で確認）。
// **0.9.0 以降は workspace_group_close_required で断られ、何も閉じない。**
// 本物での確認は test/live/herdr_test.go の TestLive_WorkspaceClose_配下があると親は断られ何も消えない）。

// repoWorkspaceFixture は「親 workspace を閉じるか」の検査1件分の状態である。
type repoWorkspaceFixture struct {
	// cleanupFixture は worktree と身元ファイルを用意した状態である。
	*cleanupFixture
	// RepoDir は worktree を切った元のリポジトリの作業ディレクトリである。
	RepoDir string
}

// newRepoWorkspaceFixture は片付けの検査用の worktree を用意し、身元ファイルの
// herdr_repo_workspace_id を指定した値に書き換える。
//
// t: 呼び出し元のテスト。
// repoWorkspaceID: 身元ファイルへ書く親 workspace の ID（空文字なら書かない）。
// 戻り値: 検査に使う状態。
func newRepoWorkspaceFixture(t *testing.T, repoWorkspaceID string) *repoWorkspaceFixture {
	t.Helper()

	cf := newCleanupFixture(t, nil)
	identity, err := cf.Manager.ReadIdentity(cf.Prepared.Path)
	if err != nil {
		t.Fatalf("身元ファイルを読めない: %v", err)
	}
	identity.HerdrRepoWorkspaceID = repoWorkspaceID
	if err := cf.Manager.WriteIdentity(context.Background(), cf.Prepared.Path, *identity); err != nil {
		t.Fatalf("身元ファイルを書けない: %v", err)
	}
	return &repoWorkspaceFixture{cleanupFixture: cf, RepoDir: cf.Repo.Dir}
}

// closedWorkspaceIDs は herdr へ送った workspace.close の宛先を送った順に返す。
//
// t: 呼び出し元のテスト。
// fx: 検査に使う状態。
// 戻り値: 閉じるよう頼んだ workspace の ID。
func closedWorkspaceIDs(t *testing.T, fx *repoWorkspaceFixture) []string {
	t.Helper()

	ids := []string{}
	for _, r := range fx.Herdr.Requests() {
		if r.Method != herdr.MethodWorkspaceClose {
			continue
		}
		id, _ := r.Params["workspace_id"].(string)
		ids = append(ids, id)
	}
	return ids
}

// writeSecondIdentity は、同じリポジトリの2件目の worktree に身元ファイルを置く。
//
// **herdr_repo_workspace_id は空にする。**2件目は「親は自分より先からあった」と
// 見るので、着手はここに何も書かない（prepare.go の repoWorkspaceExisted）。
//
// t: 呼び出し元のテスト。
// fx: 検査に使う状態。
// second: 2件目の worktree。
func writeSecondIdentity(t *testing.T, fx *repoWorkspaceFixture, second *workspace.PrepareResult) {
	t.Helper()
	identity := workspace.Identity{
		IssueURL:         sampleIssue(189).URL,
		IssueIdentifier:  sampleIssue(189).Identifier,
		ProjectItemID:    "PVTI_test",
		Branch:           second.Branch.String(),
		HerdrWorkspaceID: "wOther",
		CreatedAt:        time.Now(),
	}
	if err := fx.Manager.WriteIdentity(context.Background(), second.Path, identity); err != nil {
		t.Fatalf("2件目の身元ファイルを書けない: %v", err)
	}
}

// 目的: Prepare が「自分が開かせた親 workspace」だけを控えることを確認する（issue #19）。
// 与える情報: worktree.open の前は空、後は親 workspace 1件を返す workspace.list。
// 成功条件: PrepareResult.HerdrRepoWorkspaceID がその親の ID になること。
func TestPrepare_自分が開かせた親workspaceを控える(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})

	// **1回目（open の前）は空、2回目（open の後）は親を1件返す。**副作用は応答を
	// 決めたあとに走るので、ここで差し替えると次の呼び出しから効く。
	fx.Herdr.SetOnRequest(herdr.MethodWorkspaceList, func(_ map[string]any) {
		fx.Herdr.SetResult(herdr.MethodWorkspaceList, workspaceListResult(
			workspaceEntry("wRepo", fx.Repo.Dir, fx.Repo.Dir),
		))
	})

	result := prepareWorktree(t, fx, sampleIssue(188))
	if result.HerdrRepoWorkspaceID != "wRepo" {
		t.Fatalf("自分が開かせた親 workspace を控えていない: got %q, want %q",
			result.HerdrRepoWorkspaceID, "wRepo")
	}
}

// 目的: Prepare が「先からあった親 workspace」を控えないことを確認する（issue #19）。
// 与える情報: worktree.open の前から親 workspace 1件を返す workspace.list。
// 成功条件: PrepareResult.HerdrRepoWorkspaceID が空文字であること。
// **workspace.list を2回引かないこと**（控える必要が無いので後ろの1回は呼ばない）。
func TestPrepare_先からあった親workspaceは控えない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Herdr.SetResult(herdr.MethodWorkspaceList, workspaceListResult(
		workspaceEntry("wHuman", fx.Repo.Dir, fx.Repo.Dir),
	))

	result := prepareWorktree(t, fx, sampleIssue(188))
	if result.HerdrRepoWorkspaceID != "" {
		t.Fatalf("先からあった親 workspace を控えてしまっている: got %q", result.HerdrRepoWorkspaceID)
	}
	if got := countMethod(fx.Herdr.Methods(), herdr.MethodWorkspaceList); got != 1 {
		t.Fatalf("workspace.list を引いた回数が想定と違う: got %d, want 1", got)
	}
}

// 目的: 再利用のとき、前の run が控えた親 workspace の ID を落とさないことを確認する
// （issue #19。落とすと閉じる相手を忘れる）。
// 与える情報: herdr_repo_workspace_id に "wRepo" を持つ既存の身元ファイルと、
// その項目が空の今回ぶんの身元ファイル。
// 成功条件: MergeForReuse の結果が "wRepo" を保っていること。
func TestMergeForReuse_親workspaceのIDを落とさない(t *testing.T) {
	existing := workspace.Identity{
		HerdrRepoWorkspaceID: "wRepo",
		CreatedAt:            time.Now(),
	}
	fresh := workspace.Identity{CreatedAt: time.Now()}

	merged := workspace.MergeForReuse(fresh, &existing)
	if merged.HerdrRepoWorkspaceID != "wRepo" {
		t.Fatalf("再利用で親 workspace の ID を落としている: got %q, want %q",
			merged.HerdrRepoWorkspaceID, "wRepo")
	}
}

// countMethod は送ったメソッドの中から、その名前のものを数える。
//
// methods: 送った順のメソッド名。
// name: 数えるメソッド名。
// 戻り値: 件数。
func countMethod(methods []string, name string) int {
	n := 0
	for _, m := range methods {
		if m == name {
			n++
		}
	}
	return n
}
