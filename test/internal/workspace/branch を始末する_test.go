// {"RUCM-CFG-SHA256": "e72622f895097213edbebf6b7e232170979c65956f6bafd1024f3e4a9572f308", "SOURCE": "docs/spec/usecases/particular_case/branch を始末する.cfg.json"}
//
// **ユースケース記述「branch を始末する」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
// テストは `Manager.Cleanup` を直に呼ぶので、巡回が片付けの対象を選ぶ段は通らない。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package workspace_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/workspace"
)

// {"RUCM-PATH": "P001"}
//
// 目的: 身元ファイルに書かれた branch が**リポジトリに実在しない**とき、
// 残ったものとして数えないことを確認する（issue #27）。
// **着手が `git worktree add` で失敗し続けると、ディレクトリだけが残って
// branch は1度も作られない。**そこで「消せませんでした」と積むと、
// **利用者は存在しないものを探して消しに行く。**
// 与える情報: 失うものが無い worktree と、リポジトリに1度も作られていない branch 名を
// 書いた身元ファイル。
// 成功条件: Removed が真、BranchAbsent が真、BranchDeleted が偽、
// **Leftovers が空**であること。
func Test_branchを始末する_P001_実在しないbranchを残ったものとして数えない(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	// **接頭辞は continuo のままにする。**接頭辞で弾かれたのではなく、
	// 「リポジトリに実在しない」経路を通すためである。
	missing := cf.Prepared.Branch.String() + "-missing"
	setIdentityBranch(t, cf, missing)

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("失うものが無いのに片付けていない: %+v", *result)
	}
	if !result.BranchAbsent {
		t.Fatalf("実在しない branch なのに BranchAbsent が偽になっている: %+v", *result)
	}
	if result.BranchDeleted {
		t.Fatalf("消していない branch を「消した」と返している: %+v", *result)
	}
	if len(result.Leftovers) != 0 {
		t.Fatalf("実在しない branch を残ったものとして数えている: %v", result.Leftovers)
	}
}

// {"RUCM-PATH": "P001"}
//
// 目的: branch が実在しないなら、**`cleanup.delete_branch` が偽でも**残ったものに数えない
// ことを確認する（RUCM の基本フローで、実在の判定が設定の判定より先にある理由である）。
// **元から無いものを「設定で消さないので残しました」と言う理由は無い。**
// そう言われた利用者は、存在しない branch を探して消しに行く。
// 与える情報: `cleanup.delete_branch` を偽にした設定と、リポジトリに1度も作られていない
// branch 名を書いた身元ファイル。
// 成功条件: BranchAbsent が真で、Leftovers が空であること。
func Test_branchを始末する_P001_実在しないbranchはdeleteBranchが偽でも残ったものに数えない(t *testing.T) {
	cf := newCleanupFixture(t, func(cfg *config.Config) { cfg.Cleanup.DeleteBranch = false })
	missing := cf.Prepared.Branch.String() + "-missing"
	setIdentityBranch(t, cf, missing)

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.BranchAbsent {
		t.Fatalf("実在しない branch なのに BranchAbsent が偽になっている: %+v", *result)
	}
	if len(result.Leftovers) != 0 {
		t.Fatalf("元から無い branch を残ったものとして数えている: %v", result.Leftovers)
	}
}

// {"RUCM-PATH": "P002"}
//
// 目的: `cleanup.delete_branch` が偽なら、worktree を消しても branch は残し、
// **残ったものとして画面へ出す**ことを確認する（設計 3-9 の段4）。
// **「worktree を消した」と「branch も消えた」を同じ意味に読ませない。**
// 残っているのに消えたことにされると、残骸を探す人はログを疑うところから始める。
// 与える情報: `cleanup.delete_branch` を偽にした設定と、push 済みの worktree。
// 成功条件: Removed が真、BranchDeleted が偽、branch が clone に残っている、
// 残ったものに「設定で消さない」ことが積まれていること。
func Test_branchを始末する_P002_deleteBranchが偽ならbranchを残して残ったものに積む(t *testing.T) {
	cf := newCleanupFixture(t, func(cfg *config.Config) { cfg.Cleanup.DeleteBranch = false })

	if err := os.WriteFile(filepath.Join(cf.Prepared.Path, "成果.md"), []byte("できた\n"), 0o600); err != nil {
		t.Fatalf("成果のファイルを書けない: %v", err)
	}
	runGit(t, cf.Prepared.Path, "add", ".")
	runGit(t, cf.Prepared.Path, "commit", "--quiet", "-m", "成果")
	runGit(t, cf.Prepared.Path, "push", "--quiet", "-u", "origin", "HEAD:"+cf.Prepared.Branch.String())
	runGit(t, cf.Prepared.Path, "branch", "--set-upstream-to=origin/"+cf.Prepared.Branch.String())

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("worktree を片付けていない: %+v", *result)
	}
	if result.BranchDeleted {
		t.Fatal("cleanup.delete_branch が false なのに branch を消したと答えている")
	}
	if branches := runGit(t, cf.Repo.Dir, "branch", "--list", cf.Prepared.Branch.String()); strings.TrimSpace(branches) == "" {
		t.Fatalf("cleanup.delete_branch が false なのに branch %s を消している", cf.Prepared.Branch.String())
	}
	want := i18n.T(i18n.KeyWorkspaceLeftoverBranchDisabled, cf.Prepared.Branch.String())
	if !slices.Contains(result.Leftovers, want) {
		t.Fatalf("残ったものに %q が積まれていない: %v", want, result.Leftovers)
	}
}

// {"RUCM-PATH": "P003"}
//
// 目的: branch が**実在して**現物と食い違うときは、いままでどおり残ったものとして
// 理由を返すことを確認する（設計 3-9 の段4。issue #27 で消さなくなったのは
// 「実在しない」場合だけである）。
// 与える情報: 失うものが無い worktree と、リポジトリに実在するが worktree が
// チェックアウトしていない branch 名を書いた身元ファイル。
// 成功条件: Removed が真、BranchAbsent が偽、BranchDeleted が偽、
// Leftovers にその branch 名と理由が入っていること、その branch が残っていること。
func Test_branchを始末する_P003_実在するbranchを消せなければ理由を返す(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	stale := cf.Prepared.Branch.String() + "-old"
	runGit(t, cf.Repo.Dir, "branch", stale, "main")
	setIdentityBranch(t, cf, stale)

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("失うものが無いのに片付けていない: %+v", *result)
	}
	if result.BranchAbsent {
		t.Fatalf("実在する branch なのに BranchAbsent が真になっている: %+v", *result)
	}
	if result.BranchDeleted {
		t.Fatalf("現物と食い違う branch を消している: %+v", *result)
	}
	found := false
	for _, left := range result.Leftovers {
		if strings.Contains(left, stale) {
			found = true
		}
	}
	if !found {
		t.Fatalf("消せなかった branch %s が残ったものとして返っていない: %v", stale, result.Leftovers)
	}
	if strings.TrimSpace(runGit(t, cf.Repo.Dir, "branch", "--list", stale)) == "" {
		t.Fatalf("現物と食い違う branch %s を消している", stale)
	}
}

// {"RUCM-PATH": "P003"}
//
// 目的: 身元ファイルの branch が書き換えられていても、利用者の別の branch を消さないことを
// 確認する（設計 3-9 の段4。身元ファイルはエージェントが書き換えられる場所にある）。
// 与える情報: branch を "main" に書き換えた身元ファイルと、片付けてよい worktree。
// 成功条件: worktree は消えるが、main が残っていること。
func Test_branchを始末する_P003_身元ファイルのbranchが書き換えられていても他のbranchを消さない(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	tamperIdentity(t, cf, func(identity *workspace.Identity) { identity.Branch = "main" })

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("片付けてよい worktree なのに消していない: %+v", *result)
	}
	if branches := runGit(t, cf.Repo.Dir, "branch", "--list", "main"); strings.TrimSpace(branches) == "" {
		t.Fatal("利用者の main が消されている（身元ファイルの branch をそのまま git branch -D へ渡している）")
	}
}

// {"RUCM-PATH": "P003"}
//
// 目的: 身元ファイルの branch が worktree の現物と食い違うときは消さないことを確認する
// （設計 3-9 の段4。判定の根拠を git に置く）。
// 与える情報: 接頭辞は正しいが worktree がチェックアウトしていない branch 名。
// 成功条件: その branch が残っていること。
func Test_branchを始末する_P003_worktreeの現物と一致しないbranchは消さない(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	runGit(t, cf.Repo.Dir, "branch", "continuo/octocat/hello-world/999")
	tamperIdentity(t, cf, func(identity *workspace.Identity) {
		identity.Branch = "continuo/octocat/hello-world/999"
	})

	if _, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf)); err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	branches := runGit(t, cf.Repo.Dir, "branch", "--list", "continuo/octocat/hello-world/999")
	if strings.TrimSpace(branches) == "" {
		t.Fatal("worktree がチェックアウトしていない branch を消している")
	}
}

// {"RUCM-PATH": "P005"}
//
// 目的: 片付け（`continuo abandon`）でも、壊れた ref を消して branch を片付け切ることを
// 確認する（issue #28、設計 3-22b）。
// 与える情報: 用意した worktree の branch の loose な ref を0バイトにした状態。
// `git branch -D` は `error: branch '<名前>' not found` で断る。
// 成功条件: BranchDeleted が真で、ref のファイルが消えており、branch が残っていないこと。
func Test_branchを始末する_P005_壊れたrefのbranchも片付ける(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	branch := cf.Prepared.Branch.String()
	refPath := breakBranchRef(t, cf.Repo.Dir, branch)

	req := cleanupRequest(cf)
	// **見送りの判定は通さない。**壊れた ref のせいで worktree 側の git が答えられず、
	// 「判定できないので消さない」で止まるため。`continuo abandon --force` と同じ経路である。
	req.Force = true
	result, err := cf.Manager.Cleanup(context.Background(), req)
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.BranchDeleted {
		t.Fatalf("branch を片付けられていない（残った理由: %v）", result.Leftovers)
	}
	if _, err := os.Stat(refPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("壊れた ref のファイルが残っている（%s）: %v", refPath, err)
	}
	if out, err := gitTry(t, cf.Repo.Dir, "show-ref", "--verify", "refs/heads/"+branch); err == nil {
		t.Fatalf("branch が残っている: %s", out)
	}
}

// {"RUCM-PATH": "P005"}
//
// 目的: **壊れた ref のファイルを消したことを、人間の画面へ出す**ことを確認する
// （issue #28 の監査）。`continuo abandon` は Logger を渡さないので、ログにだけ書くと
// 「continuo が `.git` の中のファイルを1つ消した」ことが1文字も伝わらない。
// 与える情報: 用意した worktree の branch の loose な ref を0バイトにした状態。
// 成功条件: CleanupResult.Notices に、消したファイルの絶対パスを含む行があること。
func Test_branchを始末する_P005_壊れたrefを消したことを画面に出す(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	branch := cf.Prepared.Branch.String()
	refPath := breakBranchRef(t, cf.Repo.Dir, branch)

	req := cleanupRequest(cf)
	req.Force = true
	result, err := cf.Manager.Cleanup(context.Background(), req)
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	found := false
	for _, notice := range result.Notices {
		if strings.Contains(notice, refPath) {
			found = true
		}
	}
	if !found {
		t.Fatalf("消したファイルのパスが人間の画面へ出ていない（%s）: %v", refPath, result.Notices)
	}
}
