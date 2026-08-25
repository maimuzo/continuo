// {"RUCM-CFG-SHA256": "943d20234f97e95ce8320c4f28b7d6b0dbc9f4bdde8ebd2e9198f9cfa1c01349", "SOURCE": "docs/spec/usecases/particular_case/worktree と branch を片付ける.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**「worktree と branch を片付ける」の
// うち、**リポジトリの親 workspace を閉じるかどうか**の分岐（ステップ11〜20）を通る
// パスに、それぞれ対応するテストがある。
package workspace_test

import (
	"context"
	"slices"
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
// （**親を閉じると配下も一緒に消えることは本物の herdr で確認済みである。**
// test/live/herdr_test.go の TestLive_WorkspaceClose_親を閉じると配下のworktreeも消える）。

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

// {"RUCM-PATH": "P001"}
//
// 目的: continuo が開かせたリポジトリの親 workspace を、配下の worktree が無くなった
// あとに閉じることを確認する（issue #19）。
// 与える情報: 身元ファイルに herdr_repo_workspace_id として "wRepo" を書いた worktree と、
// 親 workspace 1件だけを返す workspace.list。
// 成功条件: worktree.remove のあとに workspace.close が "wRepo" 宛に1回だけ送られること。
func TestCleanup_continuoが開かせた親workspaceを閉じる(t *testing.T) {
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

// {"RUCM-PATH": "P016"}
//
// 目的: 人間が先に開いていたリポジトリの親 workspace を閉じないことを確認する（issue #19）。
// 与える情報: herdr_repo_workspace_id を書いていない worktree と、
// 親 workspace 1件だけを返す workspace.list。
// 成功条件: workspace.close を1回も送らないこと。
//
// **これを閉じると、その人が使っている pane ごと消える。**continuo が開かせたと
// 記録していない親には触らない。
func TestCleanup_人間が開いた親workspaceは閉じない(t *testing.T) {
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

// {"RUCM-PATH": "P006"}
//
// 目的: 同じリポジトリの別の worktree がまだ開いていれば、親 workspace を閉じないことを
// 確認する（issue #19）。
// 与える情報: herdr_repo_workspace_id を書いた worktree と、親 workspace に加えて
// 別の worktree の workspace も返す workspace.list。
// 成功条件: workspace.close を1回も送らないこと。
//
// **親を閉じると配下の worktree の workspace と pane も一緒に消える**ので、
// ここで閉じると別の issue の Claude Code が動いている pane が落ちる。
func TestCleanup_同じリポジトリのworktreeが残っていれば親workspaceを閉じない(t *testing.T) {
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

// {"RUCM-PATH": "P006"}
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
func TestCleanup_親workspaceを閉じる責任を残ったworktreeへ渡す(t *testing.T) {
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

// {"RUCM-PATH": "P006"}
//
// 目的: 引き継ぎが**既に持っている値を上書きしない**ことを確認する（issue #19）。
//
// **上書きすると、別のリポジトリの親を閉じにいく身元ファイルを continuo 自身が作る。**
//
// 与える情報: herdr_repo_workspace_id に "wRepo" を書いた issue 188 の worktree と、
// 既に "wSomeoneElse" を持っている issue 189 の worktree。
//
// 成功条件: 189 の身元ファイルが "wSomeoneElse" のままであること。
func TestCleanup_引き継ぎは既にある親workspaceのIDを上書きしない(t *testing.T) {
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

// {"RUCM-PATH": "P011"}
//
// 目的: 身元ファイルの herdr_repo_workspace_id が herdr の現物と食い違えば閉じないことを
// 確認する（issue #19）。
// 与える情報: herdr_repo_workspace_id に "wSomeoneElse" を書いた worktree と、
// このリポジトリの親 workspace が "wRepo" である workspace.list。
// 成功条件: workspace.close を1回も送らないこと。
//
// **身元ファイルは worktree の直下にあり、エージェントが書き換えられる。**検算せずに
// 渡すと、同じ機械で動いている別のリポジトリの workspace を閉じさせられる。
func TestCleanup_親workspaceのIDが現物と食い違えば閉じない(t *testing.T) {
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
