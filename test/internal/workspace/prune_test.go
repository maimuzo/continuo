// `git worktree prune` を撃ってよいかの判定を検証する（設計 3-37-9b）。
//
// **prune はリポジトリ全体に効く。**continuo が消した1件だけでなく、
// **利用者がディレクトリごと移した worktree の登録も一緒に落とす。**落とされた側の
// branch は git に守られなくなり、**あとの `git branch -D` が通ってしまう。**
//
// **そこで、アサーションは必ず `git worktree list --porcelain` の実物を見る。**
// 残ったものの文言だけを見ると、prune が撃たれたかどうかを取り違える。
package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/i18n"
)

// keepWorktreeOnRemove は、herdr が「消しました」と答えるのに実体を消さない状態を作る。
//
// **continuo が自分の手で片付ける経路（removeWorktreeByHand）へ落とすためにある。**
// テスト用herdr mock の既定の副作用は本物と同じく `git worktree remove --force` を叩くので、
// そのままでは prune の判定まで届かない。
//
// cf: 片付けの検査に使う状態。
func keepWorktreeOnRemove(cf *cleanupFixture) {
	cf.Herdr.SetOnRequest(herdr.MethodWorktreeRemove, func(_ map[string]any) {})
}

// addStrayWorktree は、**利用者がディレクトリごと移した worktree**を1件作る。
//
// 登録だけがリポジトリに残り、実体は無い状態になる。
//
// t: 呼び出し元のテスト。
// cf: 片付けの検査に使う状態。
// name: worktree のディレクトリ名と branch 名に使う名前（**git の branch 名に使えること**）。
// 戻り値: 消したディレクトリの絶対パス（**git の登録に載っているパスと同じ形**）。
func addStrayWorktree(t *testing.T, cf *cleanupFixture, name string) string {
	t.Helper()
	// **リポジトリと同じ親に置く。**newTestRepo が親のシンボリックリンクを解決済みなので、
	// git が登録に書くパスと、テストが持っている文字列が一致する。
	path := filepath.Join(filepath.Dir(cf.Repo.Dir), name)
	runGit(t, cf.Repo.Dir, "worktree", "add", "-b", name, path, "main")
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("worktree のディレクトリを消せません（%s）: %v", path, err)
	}
	return path
}

// registeredWorktrees は、いまリポジトリに登録されている worktree の一覧を返す。
//
// t: 呼び出し元のテスト。
// cf: 片付けの検査に使う状態。
// 戻り値: `git worktree list --porcelain` の出力そのまま。
func registeredWorktrees(t *testing.T, cf *cleanupFixture) string {
	t.Helper()
	return runGit(t, cf.Repo.Dir, "worktree", "list", "--porcelain")
}

// pruneLeftovers は、残ったもののうち prune の案内を含むものを返す。
//
// result: Cleanup の結果の Leftovers。
// repoDir: リポジトリの作業ディレクトリ。
// 戻り値: `git -C <repoDir> worktree prune` を含む残ったもの。
func pruneLeftovers(leftovers []string, repoDir string) []string {
	var found []string
	for _, left := range leftovers {
		if strings.Contains(left, "git -C "+repoDir+" worktree prune") {
			found = append(found, left)
		}
	}
	return found
}

// 目的: 実体の無い worktree の登録が**ほかにもある**ときは `git worktree prune` を撃たず、
// 残った登録の在りかを人間へ出すことを確認する（設計 3-37-9b）。
// **prune はリポジトリ全体に効く。**利用者がディレクトリごと移した worktree の登録まで
// 落ちると、git はその branch を守らなくなり、`git branch -D` が通ってしまう。
// 与える情報: continuo が消す worktree と、**利用者が移した worktree**（登録だけが残る）
// の2件が登録されたリポジトリ。herdr は「消しました」と答えるのに実体を消さない。
// 成功条件: Cleanup が成功し、**移された側の登録が `git worktree list` に残っている**こと。
// 残ったものに、その登録のパスと prune の案内が入っていること。
func TestCleanup_実体の無い登録がほかにもあればpruneを撃たない(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	keepWorktreeOnRemove(cf)
	stray := addStrayWorktree(t, cf, "moved-by-user")

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("worktree を片付けていない: %+v", *result)
	}

	// **登録が残っていることが、prune を撃っていないことの証拠である。**
	registered := registeredWorktrees(t, cf)
	if !strings.Contains(registered, stray) {
		t.Fatalf("移しただけの worktree の登録 %q を prune で落としてしまった:\n%s", stray, registered)
	}
	want := i18n.T(i18n.KeyWorkspaceLeftoverPruneSkipped, stray, cf.Repo.Dir)
	if !slices.Contains(result.Leftovers, want) {
		t.Fatalf("残った登録の在りかを人間へ出していない。\n欲しい: %q\n出たもの: %v", want, result.Leftovers)
	}
}

// 目的: 実体の無い登録が**自分が消した1件だけ**なら `git worktree prune` を撃つことを
// 確認する（設計 3-37-9b）。**守りが効きすぎて何も掃除しなくなる方向の壊れを殺す。**
// 掃除しないままにすると、次に同じパスへ worktree を作るとき
// `missing but already registered worktree` で着手が失敗する。
// 与える情報: continuo が消す worktree だけが登録されたリポジトリ。
// herdr は「消しました」と答えるのに実体を消さない。
// 成功条件: Cleanup が成功し、**その登録が `git worktree list` から消えている**こと。
// 残ったものに prune の案内が1件も入っていないこと。
func TestCleanup_自分が消した1件だけならpruneを撃つ(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	keepWorktreeOnRemove(cf)

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("worktree を片付けていない: %+v", *result)
	}

	registered := registeredWorktrees(t, cf)
	if strings.Contains(registered, cf.Prepared.Path) {
		t.Fatalf("実体を消した worktree の登録 %q が残っている（prune を撃っていない）:\n%s",
			cf.Prepared.Path, registered)
	}
	if left := pruneLeftovers(result.Leftovers, cf.Repo.Dir); len(left) > 0 {
		t.Fatalf("掃除できているのに登録が残ったと言っている: %v", left)
	}
}

// 目的: worktree の登録の一覧を**引けなかった**ときは `git worktree prune` を撃たず、
// 登録が残ったことを人間へ出すことを確認する（設計 3-37-9b）。
// **「ほかに実体の無い登録は無い」と「引けなかった」は別である。**引けなかったのに撃つと、
// 利用者が移した worktree の登録を、確かめないまま落とすことになる。
// 与える情報: 1行が 64KiB を超える理由で lock した worktree があるリポジトリ
// （`git worktree list --porcelain` は成功するが、continuo はその1行を読み切れない）。
// 成功条件: Cleanup が成功し、**消した worktree の登録が残っている**こと
// （撃っていれば落ちている）。残ったものに prune の案内が入っていること。
func TestCleanup_登録の一覧を引けなければpruneを撃たない(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	keepWorktreeOnRemove(cf)

	// **読み切れない1行を作る。**`git worktree list --porcelain` は lock の理由を
	// `locked <理由>` の1行で出す。continuo はこの出力を1行ずつ読むので、
	// 64KiB を超える1行があると一覧を組み立てられない（`token too long`）。
	// **実体は残す。**残さないと「実体の無い登録がほかにもある」側の判定に落ち、
	// ここで見たい「引けなかった」枝を通らない。
	locked := filepath.Join(filepath.Dir(cf.Repo.Dir), "lock-worktree")
	runGit(t, cf.Repo.Dir, "worktree", "add", "-b", "lock-worktree", locked, "main")
	runGit(t, cf.Repo.Dir, "worktree", "lock", "--reason", strings.Repeat("x", 70000), locked)

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("一覧を引けないだけで Cleanup が止まった: %v", err)
	}
	if !result.Removed {
		t.Fatalf("worktree を片付けていない: %+v", *result)
	}
	if _, err := os.Stat(cf.Prepared.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree の実体が残っている（%s）: %v", cf.Prepared.Path, err)
	}

	// **登録が残っていることが、prune を撃っていないことの証拠である。**
	registered := registeredWorktrees(t, cf)
	if !strings.Contains(registered, cf.Prepared.Path) {
		t.Fatalf("一覧を引けていないのに prune を撃っている（登録 %q が落ちた）", cf.Prepared.Path)
	}
	if left := pruneLeftovers(result.Leftovers, cf.Repo.Dir); len(left) == 0 {
		t.Fatalf("登録が残ったことを人間へ出していない: %v", result.Leftovers)
	}
}
