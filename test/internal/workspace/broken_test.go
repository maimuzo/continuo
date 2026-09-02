package workspace_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/workspace"
)

// makeDir は置き場所の下にディレクトリを1つ作る。
//
// t: 呼び出し元のテスト。
// dir: 作るディレクトリの絶対パス。
func makeDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("%s を作れない: %v", dir, err)
	}
}

// 目的: 身元ファイルはあるのに読めない worktree を、壊れたものとして数えることを確認する
// （設計 3-49）。**Scan は Err を付けて返すが、身元ファイルの無いものは返さないので、
// 「壊れたもの」を1つの口で数えられる関数が別に要る。**
//
// 与える情報: 置き場所の4階層目に置いた、JSON が壊れた身元ファイル。
//
// 成功条件: 1件だけ拾い、種類が「読めない」であり、**ディレクトリが消えていないこと。**
func TestScanBroken_身元ファイルを読めないworktreeを数える(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	root := fx.Manager.ResolvedRoot()

	dir := filepath.Join(root, "github.com", "octocat", "hello-world", "continuo-octocat-hello-world-188")
	putIdentityFile(t, dir, "{壊れている")

	found, err := fx.Manager.ScanBroken(nil)
	if err != nil {
		t.Fatalf("ScanBroken に失敗した: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("壊れた worktree を1件だけ拾っていない: %+v", found)
	}
	if found[0].Path != dir {
		t.Fatalf("拾ったパスが違う: got %q, want %q", found[0].Path, dir)
	}
	if found[0].Kind != workspace.BrokenIdentityUnreadable {
		t.Fatalf("種類が違う: got %q, want %q", found[0].Kind, workspace.BrokenIdentityUnreadable)
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		t.Fatalf("壊れた worktree を消してしまった: %v", statErr)
	}
}

// 目的: 身元ファイルが無いディレクトリのうち、**置き場所の命名に一致するものだけ**を
// 壊れたものとして数えることを確認する（設計 3-49）。
//
// **既定の `workspace.on_broken_worktree` は `stop` である。**人間が置いたディレクトリを
// 数えてしまうと、**そのディレクトリがあるだけで continuo が二度と起動しなくなる。**
//
// 与える情報: 置き場所の4階層目に置いた2つのディレクトリ。
// 片方は `branch_template` の形（issue の番号を切り出せる）、もう片方は人間が付けた名前。
//
// 成功条件: `branch_template` の形のものだけを1件拾い、issue の番号まで引けていること。
func TestScanBroken_置き場所の命名に一致するものだけを数える(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	root := fx.Manager.ResolvedRoot()

	continuoLike := filepath.Join(root, "github.com", "octocat", "hello-world",
		"continuo-octocat-hello-world-188")
	human := filepath.Join(root, "github.com", "octocat", "hello-world", "人間の作業場")
	makeDir(t, continuoLike)
	makeDir(t, human)

	found, err := fx.Manager.ScanBroken(nil)
	if err != nil {
		t.Fatalf("ScanBroken に失敗した: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("拾った件数が1件でない（人間が置いたものを巻き込んでいないか）: %+v", found)
	}
	if found[0].Path != continuoLike {
		t.Fatalf("拾ったパスが違う: got %q, want %q", found[0].Path, continuoLike)
	}
	if found[0].Kind != workspace.BrokenIdentityMissing {
		t.Fatalf("種類が違う: got %q, want %q", found[0].Kind, workspace.BrokenIdentityMissing)
	}
	if found[0].Clue == nil || found[0].Clue.Number != 188 {
		t.Fatalf("置き場所から issue の番号を引けていない: %+v", found[0].Clue)
	}
	if _, statErr := os.Stat(human); statErr != nil {
		t.Fatalf("人間のディレクトリが消えている: %v", statErr)
	}
}

// 目的: 置き場所のパスから issue の番号と URL を引けることを確認する（設計 3-49）。
//
// **これが復元の入口である。**引けなければ、身元ファイルの無い worktree は
// 「人間が置いたもの」として扱われ、復元も止まりも起きない。
//
// 与える情報: 既定の `branch_template` で作られた置き場所のパス。
//
// 成功条件: owner・リポジトリ名・スラグ・番号が引け、issue の URL が組み立つこと。
func TestPathClueOf_置き場所からissueの番号を切り出す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	root := fx.Manager.ResolvedRoot()
	dir := filepath.Join(root, "github.com", "octocat", "hello-world",
		"continuo-octocat-hello-world-188")

	clue, err := fx.Manager.PathClueOf(dir, "", "")
	if err != nil {
		t.Fatalf("PathClueOf に失敗した: %v", err)
	}
	if clue.Owner != "octocat" || clue.Repo != "hello-world" {
		t.Fatalf("owner とリポジトリ名が違う: %+v", clue)
	}
	if clue.Slug != "continuo-octocat-hello-world-188" {
		t.Fatalf("スラグが違う: got %q", clue.Slug)
	}
	if clue.Number != 188 {
		t.Fatalf("issue の番号を切り出せていない: got %d, want 188", clue.Number)
	}
	if got, want := clue.IssueURL(), "https://github.com/octocat/hello-world/issues/188"; got != want {
		t.Fatalf("issue の URL が違う: got %q, want %q", got, want)
	}
	if got, want := clue.Identifier(), "octocat/hello-world#188"; got != want {
		t.Fatalf("識別子が違う: got %q, want %q", got, want)
	}
}

// 目的: 人間へ出す案内が「消し方」だけで終わらないことを確認する（設計 3-49）。
//
// **壊れた worktree には、まだ push していない成果が残っていることがある。**
// いきなり `continuo abandon --force` だけを見せると、その成果ごと消える。
//
// 与える情報: worktree のパスと issue の URL。
//
// 成功条件: 「中を調べる」「控える」「消す」の3行が、この順で出ること。
func TestNextSteps_調べ方と控え方を消し方より先に出す(t *testing.T) {
	steps := workspace.NextSteps("/tmp/wt/188", "https://github.com/octocat/hello-world/issues/188")
	if len(steps) != 3 {
		t.Fatalf("案内が3行でない: %+v", steps)
	}
	if !strings.Contains(steps[0], "ls -la /tmp/wt/188") || !strings.Contains(steps[0], "git -C /tmp/wt/188") {
		t.Fatalf("1行目に中の調べ方が無い: %q", steps[0])
	}
	if !strings.Contains(steps[1], "cp -a /tmp/wt/188") {
		t.Fatalf("2行目に控え方が無い: %q", steps[1])
	}
	if !strings.Contains(steps[2], "continuo abandon --force https://github.com/octocat/hello-world/issues/188") {
		t.Fatalf("3行目に消し方が無い: %q", steps[2])
	}
}

// 目的: issue の URL を引けなかったときに、**URL を捏造しない**ことを確認する（設計 3-49）。
//
// **URL を書いてしまうと、人間はその URL の issue（＝別の issue）の worktree を消しに行く。**
//
// 与える情報: worktree のパスだけ（issue の URL は空）。
//
// 成功条件: 3行目に URL が入らず、自分で調べるよう促すこと。
func TestNextSteps_issueのURLを引けないなら捏造しない(t *testing.T) {
	steps := workspace.NextSteps("/tmp/wt/188", "")
	if len(steps) != 3 {
		t.Fatalf("案内が3行でない: %+v", steps)
	}
	if strings.Contains(steps[2], "https://") {
		t.Fatalf("引けていない issue の URL を書いている: %q", steps[2])
	}
	if !strings.Contains(steps[2], "どの issue のものかを確かめて") {
		t.Fatalf("どの issue のものかを人間に確かめさせる案内が無い: %q", steps[2])
	}
	if !strings.Contains(steps[2], "continuo abandon --force") {
		t.Fatalf("3行目に消し方が無い: %q", steps[2])
	}
}
