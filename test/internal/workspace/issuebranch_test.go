package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/workspace"
)

// 目的: worktree が1つも無いときに、規則から組み立てた branch の現況を答えられることを
// 確認する（設計 3-37-9）。**未 push の commit を数えて返すこと**もあわせて確かめる。
// **数えられるのに数えないと、`--force` を求められた利用者は何を失うのかを知らないまま
// 押し切ることになる。**
// 与える情報: main から切った branch と、そこへ積んだ push していない commit 1件。
// 成功条件: 実在すると答え、clone のパスと先頭の commit と未 push の commit の数が
// 埋まっていること。
func TestFindIssueBranch_残ったbranchと未pushのcommitを答える(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(4000)

	branch := orphanBranchWithUnpushedCommit(t, fx, issue)

	found, err := fx.Manager.FindIssueBranch(context.Background(), issue)
	if err != nil {
		t.Fatalf("残った branch を調べられなかった: %v", err)
	}
	if !found.Exists {
		t.Fatalf("branch %s があるのに「無い」と答えた", branch)
	}
	if found.Name.String() != branch {
		t.Fatalf("branch 名が %q ではなく %q だった", branch, found.Name.String())
	}
	if found.RepoDir != fx.Repo.Dir {
		t.Fatalf("clone のパスが %q ではなく %q だった", fx.Repo.Dir, found.RepoDir)
	}
	if found.Tip == "" {
		t.Fatal("消す前に控えるはずの先頭の commit が空だった")
	}
	if found.UnpushedErr != nil {
		t.Fatalf("未 push の commit を数えられなかった: %v", found.UnpushedErr)
	}
	if found.Unpushed != 1 {
		t.Fatalf("未 push の commit の数が 1 ではなく %d だった", found.Unpushed)
	}
}

// 目的: clone が手元に無いときに、「branch は無い」ではなくエラーを返すことを確認する
// （設計 3-37-9）。**「調べられなかった」を「無い」に丸めると、残っている branch が
// 片付いたものとして扱われる。**
// 与える情報: `ghq list -p -e` が何も答えないリポジトリ。
// 成功条件: エラーが返り、Exists が偽のまま「無い」と主張していないこと
// （呼び出し側はエラーを見て「調べられなかった」と言う）。
func TestFindIssueBranch_cloneが無ければ調べられなかったことを返す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		GhqList: func(_ context.Context, _, _ string) (string, error) { return "", nil },
	})

	found, err := fx.Manager.FindIssueBranch(context.Background(), sampleIssue(4001))
	if err == nil {
		t.Fatalf("clone が無いのにエラーが返らなかった（answered=%+v）", found)
	}
	if found.Exists {
		t.Fatal("clone を引けていないのに「branch がある」と答えた")
	}
}

// 目的: リポジトリが壊れていて `git show-ref` が終了コード 128 で断ったとき、
// 「branch は無い」ではなくエラーを返すことを確認する（設計 3-37-9）。
// **「無い」と「調べられない」は別である。**非 0 を全部「無い」に丸めると、
// 調べられなかっただけの branch が「もう無い」ものとして扱われ、
// `continuo abandon` は「branch も残っていません」と報告して終わる。
// 与える情報: `ghq list -p -e` が答える場所に、`.git` が git のファイルでない
// ディレクトリを置いた状態（`git show-ref` は `invalid gitfile format` で 128 を返す）。
// 成功条件: エラーが返り、Exists が偽のまま「無い」と主張していないこと。
// エラーの文面に branch 名と終了コード 128 が入っていること。
func TestFindIssueBranch_リポジトリが壊れていれば無いと言わない(t *testing.T) {
	broken := filepath.Join(t.TempDir(), "壊れた-clone")
	if err := os.MkdirAll(broken, 0o700); err != nil {
		t.Fatalf("壊れた clone の置き場所を作れません: %v", err)
	}
	// **`.git` を git が読めない中身にする。**ディレクトリの外側に何があっても
	// `git -C <ここ> show-ref` は `invalid gitfile format` で終了コード 128 を返す。
	if err := os.WriteFile(filepath.Join(broken, ".git"), []byte("これは git のファイルではない\n"), 0o600); err != nil {
		t.Fatalf("壊れた .git を書けません: %v", err)
	}
	fx := newFixture(t, fixtureOptions{
		GhqList: func(_ context.Context, _, _ string) (string, error) { return broken, nil },
	})
	issue := sampleIssue(4003)

	found, err := fx.Manager.FindIssueBranch(context.Background(), issue)
	if err == nil {
		t.Fatalf("リポジトリが壊れているのにエラーが返らなかった（answered=%+v）", found)
	}
	if found.Exists {
		t.Fatal("調べられていないのに「branch がある」と答えた")
	}
	if !strings.Contains(err.Error(), "128") {
		t.Errorf("git が断った終了コードが分からない: %v", err)
	}
	if !strings.Contains(err.Error(), found.Name.String()) {
		t.Errorf("どの branch を調べられなかったのか分からない: %v", err)
	}
}

// 目的: 実体の無い worktree の登録が残っているとき、**その登録を掃除せずに** branch を
// 残すことを確認する（設計 3-37-9b）。
// **git は登録を根拠に branch を守っている。**`git worktree prune` で登録を落とすと
// git は断らなくなり、**push していない commit が「消しました」と一緒に失われる。**
// 与える情報: Prepare で作った worktree のディレクトリだけを消したリポジトリ。
// 成功条件: DeleteIssueBranch がエラーを返し、branch が残っており、
// **worktree の登録も残っており**、エラー文に登録のパスと prune の案内が入っていること。
func TestDeleteIssueBranch_登録が残っていれば消さずに在りかを返す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(4002)

	prepared, err := fx.Manager.Prepare(context.Background(), issue)
	if err != nil {
		t.Fatalf("worktree を用意できません: %v", err)
	}
	// **ディレクトリだけを消す。**git の登録は残るので、git は branch を守り続ける。
	if err := os.RemoveAll(prepared.Path); err != nil {
		t.Fatalf("worktree のディレクトリを消せません: %v", err)
	}

	found, err := fx.Manager.FindIssueBranch(context.Background(), issue)
	if err != nil {
		t.Fatalf("残った branch を調べられなかった: %v", err)
	}
	if !found.Exists {
		t.Fatalf("branch %s があるのに「無い」と答えた", prepared.Branch.String())
	}

	deleteErr := fx.Manager.DeleteIssueBranch(context.Background(), found)
	if deleteErr == nil {
		t.Fatalf("git が守っている branch %s を消してしまった", prepared.Branch.String())
	}
	if !strings.Contains(deleteErr.Error(), prepared.Path) {
		t.Fatalf("エラー文に登録のパス %q が入っていない:\n%s", prepared.Path, deleteErr)
	}
	if !strings.Contains(deleteErr.Error(), "worktree prune") {
		t.Fatalf("エラー文に prune の案内が入っていない:\n%s", deleteErr)
	}
	if !localBranchExists(t, fx.Repo.Dir, prepared.Branch.String()) {
		t.Fatalf("消してはならない branch %s が消えている", prepared.Branch.String())
	}
	// **登録が残っていることが、prune を撃っていないことの証拠である。**
	registered := runGit(t, fx.Repo.Dir, "worktree", "list", "--porcelain")
	if !strings.Contains(registered, prepared.Path) {
		t.Fatalf("worktree の登録を掃除してしまった（prune を撃っている）:\n%s", registered)
	}
}

// 目的: worktree のディレクトリを**移しただけ**のとき、branch も移した先も壊さないことを
// 確認する（設計 3-37-9b）。
// **これが prune を撃っていた頃に起きていたことである。**移した先には push していない
// commit が載っており、prune で登録を落とすと `git branch -D` が通ってしまう。
// 与える情報: Prepare で作った worktree を置き場所の外へ移したリポジトリ。
// 成功条件: DeleteIssueBranch がエラーを返し、branch が残り、移した先のファイルも
// 残っていること。
func TestDeleteIssueBranch_移されたworktreeのbranchを消さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(4003)

	prepared, err := fx.Manager.Prepare(context.Background(), issue)
	if err != nil {
		t.Fatalf("worktree を用意できません: %v", err)
	}
	// **中身がある状態で移す。**利用者が置き場所を変えただけの状態である。
	work := filepath.Join(prepared.Path, "work.txt")
	if err := os.WriteFile(work, []byte("まだ push していない成果\n"), 0o600); err != nil {
		t.Fatalf("成果のファイルを書けません: %v", err)
	}
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(prepared.Path, moved); err != nil {
		t.Fatalf("worktree を移せません: %v", err)
	}

	found, err := fx.Manager.FindIssueBranch(context.Background(), issue)
	if err != nil {
		t.Fatalf("残った branch を調べられなかった: %v", err)
	}
	if err := fx.Manager.DeleteIssueBranch(context.Background(), found); err == nil {
		t.Fatalf("移しただけの worktree の branch %s を消してしまった", prepared.Branch.String())
	}
	if !localBranchExists(t, fx.Repo.Dir, prepared.Branch.String()) {
		t.Fatalf("消してはならない branch %s が消えている", prepared.Branch.String())
	}
	if _, err := os.Stat(filepath.Join(moved, "work.txt")); err != nil {
		t.Fatalf("移した先の成果が失われている（%s）: %v", moved, err)
	}
}

// 目的: 身元ファイルが無いディレクトリを、走査が別の口で数えられることを確認する
// （設計 3-37-9c）。**Scan はこれを結果に含めない**ので、数える口が無いと
// 「判断を保留したものは1つも無い」と見えてしまう。
// 与える情報: 身元ファイルを持つ worktree 1つと、身元ファイルを消した worktree 1つ。
// 成功条件: ScanUnidentified が身元ファイルを消したほうだけを返し、
// Scan の結果はもう1つだけであること。
func TestScanUnidentified_身元ファイルが無いディレクトリを返す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})

	kept, err := fx.Manager.Prepare(context.Background(), sampleIssue(4004))
	if err != nil {
		t.Fatalf("worktree を用意できません: %v", err)
	}
	writeIdentity(t, fx, kept)

	bare, err := fx.Manager.Prepare(context.Background(), sampleIssue(4005))
	if err != nil {
		t.Fatalf("worktree を用意できません: %v", err)
	}

	unidentified, err := fx.Manager.ScanUnidentified()
	if err != nil {
		t.Fatalf("置き場所を走査できなかった: %v", err)
	}
	if len(unidentified) != 1 || unidentified[0] != bare.Path {
		t.Fatalf("身元ファイルが無いディレクトリが %v だった（期待は [%s]）", unidentified, bare.Path)
	}

	scanned, err := fx.Manager.Scan()
	if err != nil {
		t.Fatalf("置き場所を走査できなかった: %v", err)
	}
	if len(scanned) != 1 || scanned[0].Path != kept.Path {
		t.Fatalf("Scan の結果が %d 件だった（期待は身元ファイルを持つ %s の1件）", len(scanned), kept.Path)
	}
}

// writeIdentity は worktree の身元ファイルを書く。
//
// t: 呼び出し元のテスト。
// fx: 走らせる一式。
// prepared: 対象の worktree。
func writeIdentity(t *testing.T, fx *managerFixture, prepared *workspace.PrepareResult) {
	t.Helper()
	identity := workspace.Identity{
		IssueURL: "https://github.com/octocat/hello-world/issues/4004",
		Branch:   prepared.Branch.String(),
		Base:     prepared.Base.String(),
	}
	if err := fx.Manager.WriteIdentity(context.Background(), prepared.Path, identity); err != nil {
		t.Fatalf("身元ファイルを書けません: %v", err)
	}
}

// orphanBranchWithUnpushedCommit は、worktree を作らずに issue の branch だけを残し、
// そこへ push していない commit を1つ積む。
//
// **checkout せずに積む。**clone の作業ディレクトリは main のままにしておきたい
// （worktree が1つも無い状態を作るのが目的である）。
//
// t: 呼び出し元のテスト。
// fx: 走らせる一式。
// issue: branch 名を組み立てる元の issue。
// 戻り値: 作った branch 名。
func orphanBranchWithUnpushedCommit(t *testing.T, fx *managerFixture, issue workspace.IssueRef) string {
	t.Helper()
	name, _, err := workspace.RenderBranch(fx.Config.Herdr.Worktree.BranchTemplate, issue)
	if err != nil {
		t.Fatalf("branch 名を組み立てられません: %v", err)
	}
	tree := runGit(t, fx.Repo.Dir, "rev-parse", "main^{tree}")
	commit := runGit(t, fx.Repo.Dir, "commit-tree", tree, "-p", "main", "-m", "push していない成果")
	runGit(t, fx.Repo.Dir, "branch", name.String(), commit)
	return name.String()
}

// localBranchExists は clone にその branch が残っているかを返す。
//
// t: 呼び出し元のテスト。
// repoDir: リポジトリの作業ディレクトリ。
// branch: 調べる branch 名。
// 戻り値: 残っていれば true。
func localBranchExists(t *testing.T, repoDir, branch string) bool {
	t.Helper()
	return runGit(t, repoDir, "branch", "--list", branch) != ""
}
