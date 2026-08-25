// 壊れた ref（`reference broken`）の始末の検査である（issue #28、設計 3-22b）。
//
// **本物の git を使い、本物の壊れた ref を作る。**`<clone>/.git/refs/heads/<branch>` を
// 0バイトのファイルにすると、git は
// `fatal: cannot lock ref '<名前>': unable to resolve reference '<名前>': reference broken`
// を返す（実測: 2026-08-25、git 2.50.1）。**この状態は `git update-ref -d` でも
// `git branch -D` でも解消できない。**ファイルとして消すしかない。
//
// **消す守りを外したら必ず落ちる形で書く。**「消したあと同じ commit から作り直された」
// 状態と「そもそも消していない」状態は SHA の比較では区別できないので、
// **消されたら復元できないもの**（元の worktree・元のファイルの中身）を見る。
package workspace_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/workspace"
)

// refFilePath は clone の loose な ref のファイルのパスを組み立てる。
//
// t: 呼び出し元のテスト。
// repoDir: clone のパス。
// branch: branch 名（スラッシュ区切り）。
// 戻り値: `<clone>/.git/refs/heads/<branch>` の絶対パス。
func refFilePath(t *testing.T, repoDir, branch string) string {
	t.Helper()
	return filepath.Join(repoDir, ".git", "refs", "heads", filepath.FromSlash(branch))
}

// writeRefFile は loose な ref のファイルに任意の中身を書く。
//
// t: 呼び出し元のテスト。
// repoDir: clone のパス。
// branch: 対象の branch 名。
// content: 書き込む中身。
// 戻り値: 書いた ref のファイルの絶対パス。
func writeRefFile(t *testing.T, repoDir, branch string, content []byte) string {
	t.Helper()
	refPath := refFilePath(t, repoDir, branch)
	if err := os.MkdirAll(filepath.Dir(refPath), 0o755); err != nil {
		t.Fatalf("ref の置き場所を作れない: %v", err)
	}
	if err := os.WriteFile(refPath, content, 0o644); err != nil {
		t.Fatalf("ref を書けない: %v", err)
	}
	return refPath
}

// breakBranchRef は clone の loose な ref を0バイトにして、壊れた ref を作る。
//
// t: 呼び出し元のテスト。
// repoDir: clone のパス。
// branch: 壊す branch 名（スラッシュ区切り）。
// 戻り値: 壊した ref のファイルの絶対パス。
func breakBranchRef(t *testing.T, repoDir, branch string) string {
	t.Helper()
	refPath := writeRefFile(t, repoDir, branch, nil)
	// **本当に壊れていることを git に言わせてから先へ進む。**
	// 壊れていない状態で「直った」と判定すると、この検査は何も守らない。
	if out, err := gitTry(t, repoDir, "show-ref", "--verify", "refs/heads/"+branch); err == nil {
		t.Fatalf("ref を壊せていない（`git show-ref --verify` が通った）: %s", out)
	}
	return refPath
}

// gitTry はテストの中で git を実行し、失敗してもテストを止めない。
//
// t: 呼び出し元のテスト。
// dir: `-C` に渡す作業ディレクトリ。
// args: git の引数。
// 戻り値の1つ目: 標準出力と標準エラー出力を混ぜたもの。
// 戻り値の2つ目: 非 0 で終わった場合のエラー。
func gitTry(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// requireRefFileUnchanged は ref のファイルが1バイトも変わっていないことを確かめる。
//
// **SHA の比較では足りない。**消したあと同じ commit から作り直されると SHA は一致する。
// ファイルの中身をそのまま突き合わせれば、「消して作り直した」も「別の値を書いた」も捕まる。
//
// t: 呼び出し元のテスト。
// refPath: 対象の ref のファイル。
// want: あるべき中身。
func requireRefFileUnchanged(t *testing.T, refPath string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatalf("消してはならない ref のファイルを読めない（%s）: %v", refPath, err)
	}
	if string(got) != string(want) {
		t.Fatalf("消してはならない ref が書き換わっている（%s）: %q → %q", refPath, want, got)
	}
}

// pointWorktreeHead は、worktree の private な git ディレクトリの HEAD を、
// **指定した branch を指す symref に書き換える。**
//
// **`git worktree list --porcelain` が branch も detached も答えない状態を作るためにある。**
// その branch の ref が解決できないと、git はその worktree について
// `HEAD 0000000000000000000000000000000000000000` の行だけを出す（実測: 2026-08-25）。
// **エージェントが rebase の途中で落ちた worktree と同じ見え方である。**
//
// t: 呼び出し元のテスト。
// worktreePath: 対象の worktree のパス。
// branch: HEAD が指す branch 名。
func pointWorktreeHead(t *testing.T, worktreePath, branch string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(worktreePath, ".git"))
	if err != nil {
		t.Fatalf("worktree の .git を読めない: %v", err)
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir: ")
	if !ok {
		t.Fatalf("worktree の .git が gitdir を指していない: %q", raw)
	}
	head := []byte("ref: refs/heads/" + branch + "\n")
	if err := os.WriteFile(filepath.Join(strings.TrimSpace(gitDir), "HEAD"), head, 0o644); err != nil {
		t.Fatalf("worktree の HEAD を書けない: %v", err)
	}
}

// requireHeadUnanswerable は、git がその worktree について branch も detached も
// 答えないことを確かめる。
//
// **この前提が崩れると、壊れた ref の経路に入らないまま検査が通ってしまう。**
//
// t: 呼び出し元のテスト。
// repoDir: リポジトリのパス。
// worktreePath: 対象の worktree のパス。
func requireHeadUnanswerable(t *testing.T, repoDir, worktreePath string) {
	t.Helper()
	out, err := gitTry(t, repoDir, "worktree", "list", "--porcelain")
	if err != nil {
		t.Fatalf("worktree の一覧を引けない: %v\n%s", err, out)
	}
	block := ""
	for _, chunk := range strings.Split(out, "worktree ") {
		if strings.HasPrefix(chunk, worktreePath+"\n") {
			block = chunk
		}
	}
	if block == "" {
		t.Fatalf("対象の worktree が一覧に出ない（%s）:\n%s", worktreePath, out)
	}
	if strings.Contains(block, "branch refs/heads/") || strings.Contains(block, "\ndetached") {
		t.Fatalf("git が branch か detached を答えており、この検査の前提が崩れている:\n%s", block)
	}
}

// 目的: 着手の経路が、壊れた ref に出会ったらその ref のファイルを消して worktree の作成を
// やり直すことを確認する（issue #28、設計 3-22b）。
// 与える情報: `continuo/octocat/hello-world/188` の loose な ref が0バイトの clone。
// 成功条件: Prepare が成功し、worktree がその branch をチェックアウトしており、
// ref のファイルが正しい SHA を持つ状態に作り直されていること。
func TestPrepare_壊れたrefを消してやり直す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	branch := "continuo/octocat/hello-world/188"
	refPath := breakBranchRef(t, fx.Repo.Dir, branch)

	result, err := fx.Manager.Prepare(context.Background(), sampleIssue(188))
	if err != nil {
		t.Fatalf("壊れた ref を始末して worktree を作れていない: %v", err)
	}
	if result.Branch.String() != branch {
		t.Fatalf("branch 名が違う: %q（期待は %q）", result.Branch.String(), branch)
	}
	if head := runGit(t, result.Path, "rev-parse", "--abbrev-ref", "HEAD"); head != branch {
		t.Fatalf("worktree が %q を出している（期待は %q）", head, branch)
	}
	// **消しっぱなしではなく、作り直されていること。**
	runGit(t, fx.Repo.Dir, "show-ref", "--verify", "refs/heads/"+branch)
	content, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatalf("作り直された ref を読めない: %v", err)
	}
	if len(strings.TrimSpace(string(content))) != 40 {
		t.Fatalf("ref の中身が SHA になっていない: %q", string(content))
	}
}

// 目的: 壊れた ref を消す経路が、**continuo が作る branch の ref だけ**を対象にすることを
// 確認する（設計 3-22b の守るべきこと）。
// 与える情報: `herdr.worktree.branch_template` に変数が1つも無い設定
// （接頭辞を決められないので、continuo が作った branch かどうかを名前から判定できない）。
// 成功条件: Prepare が失敗し、**壊れた ref のファイルが1バイトも消えていない**こと。
func TestPrepare_接頭辞を決められないなら壊れたrefを消さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Herdr.Worktree.BranchTemplate = "shared-branch"
	}})
	refPath := breakBranchRef(t, fx.Repo.Dir, "shared-branch")

	if _, err := fx.Manager.Prepare(context.Background(), sampleIssue(188)); err == nil {
		t.Fatal("壊れた ref のままなのに Prepare が成功した")
	}
	if _, err := os.Stat(refPath); err != nil {
		t.Fatalf("消してはならない ref が消えている（%s）: %v", refPath, err)
	}
}

// 目的: **接頭辞は決まったが、branch 名がその接頭辞で始まらない**ときに、壊れた ref を
// 消さないことを確認する（設計 3-22b の守るべきこと）。
// 与える情報: `continuo:{{.issue.number}}` というテンプレート。接頭辞は `continuo:` に
// なるが、branch 名は正規化でコロンが `_` に変わって `continuo_188` になる（3-7）。
// **接頭辞の材料はテンプレートの生の文字列、branch 名は正規化後**なので、両者は食い違う。
// 成功条件: Prepare が失敗し、壊れた ref のファイルが1バイトも消えていないこと。
func TestPrepare_接頭辞と食い違うbranchのrefは消さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Herdr.Worktree.BranchTemplate = "continuo:{{.issue.number}}"
	}})
	refPath := breakBranchRef(t, fx.Repo.Dir, "continuo_188")

	if _, err := fx.Manager.Prepare(context.Background(), sampleIssue(188)); err == nil {
		t.Fatal("壊れた ref のままなのに Prepare が成功した")
	}
	if _, err := os.Stat(refPath); err != nil {
		t.Fatalf("消してはならない ref が消えている（%s）: %v", refPath, err)
	}
}

// 目的: worktree の作成が壊れた ref 以外の理由で失敗したとき、**正常な branch の ref を
// 消さない**ことを確認する（設計 3-22b の守るべきこと）。
// 与える情報: 目的の branch を別の場所の worktree が既にチェックアウトしており、
// **その worktree だけが持つ commit を1つ積んである** clone
// （`git worktree add` は `already used by worktree at` で必ず失敗する）。
// 成功条件: Prepare が失敗し、**元の worktree が今も git のコマンドに答えられ**、
// ref のファイルが1バイトも変わっていないこと。
//
// **なぜ SHA の比較だけでは足りないか。**ref を消しても直後に同じ base（main）から
// branch が作り直されるので、SHA を比べるだけの検査は通ってしまう。
// **元の worktree だけが持つ commit を積んでおけば**、作り直された branch は
// その commit を指さないので、消したことがそのまま現れる。
func TestPrepare_正常なbranchのrefは消さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	branch := "continuo/octocat/hello-world/188"
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	runGit(t, fx.Repo.Dir, "worktree", "add", "-b", branch, elsewhere, "main")
	if err := os.WriteFile(filepath.Join(elsewhere, "作業中.txt"), []byte("未 push の成果\n"), 0o644); err != nil {
		t.Fatalf("元の worktree にファイルを書けない: %v", err)
	}
	runGit(t, elsewhere, "add", ".")
	runGit(t, elsewhere, "commit", "--quiet", "-m", "この branch だけが持つ commit")

	before := runGit(t, fx.Repo.Dir, "rev-parse", "refs/heads/"+branch)
	if before == runGit(t, fx.Repo.Dir, "rev-parse", "refs/heads/main") {
		t.Fatalf("branch を main より先へ進められていない（%s）", before)
	}
	refPath := refFilePath(t, fx.Repo.Dir, branch)
	refBefore, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatalf("正常な ref を読めない（%s）: %v", refPath, err)
	}

	if _, err := fx.Manager.Prepare(context.Background(), sampleIssue(188)); err == nil {
		t.Fatal("branch が別の worktree に出ているのに Prepare が成功した")
	}

	// **元の worktree が壊れていないこと。**HEAD が指す ref のファイルを消されると、
	// その worktree では `git log` すら通らなくなる。
	out, logErr := gitTry(t, elsewhere, "log", "-1", "--format=%H")
	if logErr != nil {
		t.Fatalf("元の worktree で `git log` が通らない（%s）: %v\n%s", elsewhere, logErr, out)
	}
	if out != before {
		t.Fatalf("元の worktree の HEAD が変わっている: %q（期待は %q）", out, before)
	}
	requireRefFileUnchanged(t, refPath, refBefore)
}

// 目的: **commit に解決できる ref は消さない**ことを確認する（設計 3-22b の守るべきこと）。
// 与える情報: 中身が「実在しない object の40桁」の loose な ref。
// この状態では `git show-ref --verify` は `bad ref` で落ちるが、
// **`git rev-parse --verify` は成功する**（実測: 2026-08-25、git 2.50.1）。
// つまり `show-ref` の判定だけでは「壊れている」と誤って見えるので、
// **rev-parse の判定がこの ref を守る唯一の守りである。**
// 成功条件: Prepare が失敗し、ref のファイルの中身が1バイトも変わっていないこと。
func TestPrepare_commitに解決できるrefは消さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	branch := "continuo/octocat/hello-world/188"
	// **人間が `git branch <名前> <SHA>` で作り、object をまだ持っていない branch**
	// と同じ形である。指していた SHA が読めるので、消せば情報が失われる。
	dangling := []byte("0123456789012345678901234567890123456789\n")
	refPath := writeRefFile(t, fx.Repo.Dir, branch, dangling)
	if _, err := gitTry(t, fx.Repo.Dir, "show-ref", "--verify", "refs/heads/"+branch); err == nil {
		t.Fatal("`git show-ref --verify` が通ってしまい、この検査の前提が崩れている")
	}
	if _, err := gitTry(t, fx.Repo.Dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		t.Fatalf("`git rev-parse --verify` が通らず、この検査の前提が崩れている: %v", err)
	}

	if _, err := fx.Manager.Prepare(context.Background(), sampleIssue(188)); err == nil {
		t.Fatal("commit に解決できる ref のままなのに Prepare が成功した")
	}
	requireRefFileUnchanged(t, refPath, dangling)
}

// 目的: ref の置き場所が**通常のファイルでない**なら、1バイトも触らないことを確認する
// （設計 3-22b の守るべきこと）。
// 与える情報: `refs/heads/<branch>` を、リポジトリの外のファイルを指すシンボリックリンクに
// したもの。`git show-ref` も `git rev-parse` も通らないので、
// **`os.Lstat` で通常のファイルかを見る判定だけが、このリンクを守っている。**
// 成功条件: Prepare が失敗し、シンボリックリンクがそのまま残っていること。
func TestPrepare_refがシンボリックリンクなら消さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	branch := "continuo/octocat/hello-world/188"
	target := filepath.Join(t.TempDir(), "他人のファイル")
	if err := os.WriteFile(target, []byte("これは ref ではない\n"), 0o644); err != nil {
		t.Fatalf("リンク先を書けない: %v", err)
	}
	refPath := refFilePath(t, fx.Repo.Dir, branch)
	if err := os.MkdirAll(filepath.Dir(refPath), 0o755); err != nil {
		t.Fatalf("ref の置き場所を作れない: %v", err)
	}
	if err := os.Symlink(target, refPath); err != nil {
		t.Fatalf("ref の場所にシンボリックリンクを張れない: %v", err)
	}

	if _, err := fx.Manager.Prepare(context.Background(), sampleIssue(188)); err == nil {
		t.Fatal("ref の場所がシンボリックリンクなのに Prepare が成功した")
	}
	info, err := os.Lstat(refPath)
	if err != nil {
		t.Fatalf("消してはならないシンボリックリンクが消えている（%s）: %v", refPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("ref の場所がシンボリックリンクでなくなっている（%s）: %s", refPath, info.Mode())
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("リンク先のファイルが消えている（%s）: %v", target, err)
	}
}

// 目的: loose な ref を消したあと、packed-refs 側に同名の生きた ref が残っていれば、
// それは正常な branch なので消さないことを確認する（設計 3-22b の守るべきこと）。
// 与える情報: `git pack-refs --all` で packed-refs へ移した branch と、その上に置いた
// 0バイトの loose な ref。
// 成功条件: Prepare が成功し、worktree が packed-refs 側の commit をそのまま出しており、
// packed-refs のその行が1バイトも変わっていないこと。
func TestPrepare_packedrefsの生きたrefは残す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	branch := "continuo/octocat/hello-world/188"
	runGit(t, fx.Repo.Dir, "branch", branch, "main")
	// **branch を packed-refs へ移す。**loose な ref はここで消える。
	runGit(t, fx.Repo.Dir, "pack-refs", "--all")
	packedBefore, err := os.ReadFile(filepath.Join(fx.Repo.Dir, ".git", "packed-refs"))
	if err != nil {
		t.Fatalf("packed-refs を読めない: %v", err)
	}
	if !strings.Contains(string(packedBefore), branch) {
		t.Fatalf("packed-refs に %q が入っていない:\n%s", branch, packedBefore)
	}
	want := runGit(t, fx.Repo.Dir, "rev-parse", "refs/heads/"+branch)
	breakBranchRef(t, fx.Repo.Dir, branch)

	result, err := fx.Manager.Prepare(context.Background(), sampleIssue(188))
	if err != nil {
		t.Fatalf("壊れた loose な ref を始末して worktree を作れていない: %v", err)
	}
	if got := runGit(t, result.Path, "rev-parse", "HEAD"); got != want {
		t.Fatalf("packed-refs 側の commit を出していない: %q（期待は %q）", got, want)
	}
	packedAfter, err := os.ReadFile(filepath.Join(fx.Repo.Dir, ".git", "packed-refs"))
	if err != nil {
		t.Fatalf("packed-refs を読み直せない: %v", err)
	}
	if string(packedAfter) != string(packedBefore) {
		t.Fatalf("packed-refs が書き換わっている:\n%s\n---\n%s", packedBefore, packedAfter)
	}
}

// 目的: 片付け（`continuo abandon`）でも、壊れた ref を消して branch を片付け切ることを
// 確認する（issue #28、設計 3-22b）。
// 与える情報: 用意した worktree の branch の loose な ref を0バイトにした状態。
// `git branch -D` は `error: branch '<名前>' not found` で断る。
// 成功条件: BranchDeleted が真で、ref のファイルが消えており、branch が残っていないこと。
func TestCleanup_壊れたrefのbranchも片付ける(t *testing.T) {
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

// 目的: workspace の Manager を通さずに、壊れた ref の状態そのものを記録に残す。
// **「消せない」ことが前提なので、その前提が git の版で変わっていないかを確かめる。**
// 与える情報: 0バイトの loose な ref。
// 成功条件: `git update-ref -d` も `git branch -D` も失敗すること。
func Test壊れたrefはgitのコマンドでは消せない(t *testing.T) {
	repo := newTestRepo(t)
	branch := "continuo/octocat/hello-world/1303"
	breakBranchRef(t, repo.Dir, branch)

	if out, err := gitTry(t, repo.Dir, "update-ref", "-d", "refs/heads/"+branch); err == nil {
		t.Fatalf("`git update-ref -d` が壊れた ref を消せてしまった: %s", out)
	}
	if out, err := gitTry(t, repo.Dir, "branch", "-D", branch); err == nil {
		t.Fatalf("`git branch -D` が壊れた ref を消せてしまった: %s", out)
	}
}

// 目的: **`refs/heads` の下の途中のディレクトリがシンボリックリンクでも、その先の
// ファイルを消さない**ことを確認する（issue #28 の監査。設計 3-22b）。
// 与える情報: `refs/heads/continuo/evil` をリポジトリの外のディレクトリへの
// シンボリックリンクにし、worktree を detached HEAD にしたうえで、身元ファイルの
// branch を `continuo/evil/<犠牲ファイル名>` に書き換えた状態。
// **文字列の前方一致だけで封じ込めを見ていると、`.git` の外の任意の1ファイルが消える。**
// 成功条件: 犠牲ファイルが残っていること。branch を「片付けた」と答えないこと。
func TestCleanup_途中がシンボリックリンクなら外のファイルを消さない(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	outside := t.TempDir()
	victim := filepath.Join(outside, "id_rsa")
	if err := os.WriteFile(victim, []byte("消されてはならない\n"), 0o644); err != nil {
		t.Fatalf("犠牲になるファイルを書けない: %v", err)
	}
	link := filepath.Join(cf.Repo.Dir, ".git", "refs", "heads", "continuo", "evil")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("ref の置き場所を作れない: %v", err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("シンボリックリンクを張れない: %v", err)
	}
	// **worktree の HEAD をその名前へ向ける。**その ref は解決できないので、
	// `worktree list --porcelain` は branch も detached も答えない
	// ＝ **壊れた ref の経路にそのまま入る。**
	pointWorktreeHead(t, cf.Prepared.Path, "continuo/evil/id_rsa")
	requireHeadUnanswerable(t, cf.Repo.Dir, cf.Prepared.Path)
	tamperIdentity(t, cf, func(identity *workspace.Identity) {
		identity.Branch = "continuo/evil/id_rsa"
	})

	req := cleanupRequest(cf)
	req.Force = true
	result, err := cf.Manager.Cleanup(context.Background(), req)
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("`.git` の外のファイルを消している（%s）: %v", victim, err)
	}
	if result.BranchDeleted {
		t.Fatalf("消していない branch を「片付けた」と答えている: %+v", *result)
	}
}

// 目的: **detached HEAD の worktree では、身元ファイルの branch を現物と突き合わせずに
// 消す経路へ入らない**ことを確認する（issue #28 の監査。設計 3-9 の段4）。
// 与える情報: `git checkout --detach` した worktree と、その worktree とは無関係の
// `continuo/octocat/hello-world/999` の壊れた ref。身元ファイルの branch はその名前。
// **`worktree list --porcelain` は detached の worktree について branch を答えない**ので、
// 「branch が空なら壊れた ref とみなす」書き方だと、この ref を消しに行く。
// 成功条件: 壊れた ref のファイルが残り、branch を「片付けた」と答えないこと。
func TestCleanup_detachedなworktreeでは無関係の壊れたrefを消さない(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	other := "continuo/octocat/hello-world/999"
	refPath := breakBranchRef(t, cf.Repo.Dir, other)
	runGit(t, cf.Prepared.Path, "checkout", "--detach")
	tamperIdentity(t, cf, func(identity *workspace.Identity) { identity.Branch = other })

	req := cleanupRequest(cf)
	req.Force = true
	result, err := cf.Manager.Cleanup(context.Background(), req)
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if _, err := os.Stat(refPath); err != nil {
		t.Fatalf("その worktree とは無関係の ref を消している（%s）: %v", refPath, err)
	}
	if result.BranchDeleted {
		t.Fatalf("消していない branch を「片付けた」と答えている: %+v", *result)
	}
}

// 目的: **`<名前>.lock` を指されても、その lock ファイルを消さない**ことを確認する
// （issue #28 の監査。設計 3-22b）。
// 与える情報: 別の git プロセスが握っている lock と同じ形のファイル
// （`refs/heads/continuo/octocat/hello-world/188.lock`）と、その名前を指す身元ファイル。
// **`.lock` で終わる名前は git が refname として拒むので、show-ref も rev-parse も
// 必ず落ちる。**それを「壊れた ref」と読むと、他プロセスの lock を横から消す。
//
// **守りは2枚ある。**正規化（internal/normalize）が `.lock` を `_lock` に変えるので
// 身元ファイルの検算で先に落ち、そこを抜けても `git check-ref-format` が弾く。
// **この検査はどちらか片方が消えても落ちない。**両方が同時に消えたときに落ちる。
// 成功条件: lock ファイルが残っていること。
func TestCleanup_lockファイルは壊れたrefとして消さない(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	locked := cf.Prepared.Branch.String() + ".lock"
	// **git は lock を空のファイルとして作ってから中身を書く。**
	// 中身が読める状態だと別の判定で止まってしまい、refname の検査を1度も通らない。
	lockPath := writeRefFile(t, cf.Repo.Dir, locked, nil)
	// **前提を git に言わせておく。**両方が落ちなければ、この検査は何も守らない。
	if _, err := gitTry(t, cf.Repo.Dir, "show-ref", "--verify", "--quiet", "refs/heads/"+locked); err == nil {
		t.Fatal("`git show-ref --verify` が通ってしまい、この検査の前提が崩れている")
	}
	if _, err := gitTry(t, cf.Repo.Dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+locked); err == nil {
		t.Fatal("`git rev-parse --verify` が通ってしまい、この検査の前提が崩れている")
	}
	pointWorktreeHead(t, cf.Prepared.Path, locked)
	requireHeadUnanswerable(t, cf.Repo.Dir, cf.Prepared.Path)
	tamperIdentity(t, cf, func(identity *workspace.Identity) { identity.Branch = locked })

	req := cleanupRequest(cf)
	req.Force = true
	if _, err := cf.Manager.Cleanup(context.Background(), req); err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("他のプロセスが握りうる lock ファイルを消している（%s）: %v", lockPath, err)
	}
}

// 目的: **packed-refs 側の同名の ref が生き返るなら、「branch を片付けた」と答えない**
// ことを確認する（issue #28 の監査。設計 3-22b）。
// 与える情報: `git pack-refs --all` で packed-refs へ移したうえで、その上に置いた
// 0バイトの loose な ref。**loose を消すと packed 側が生き返る。**
// 成功条件: BranchDeleted が偽で、残ったものが1件以上あり、
// **「片付けました」だけを見せない**こと。
func TestCleanup_packedrefsから生き返るbranchを片付けたと答えない(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	branch := cf.Prepared.Branch.String()
	runGit(t, cf.Repo.Dir, "pack-refs", "--all")
	breakBranchRef(t, cf.Repo.Dir, branch)

	req := cleanupRequest(cf)
	req.Force = true
	result, err := cf.Manager.Cleanup(context.Background(), req)
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if out, gitErr := gitTry(t, cf.Repo.Dir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); gitErr == nil {
		// packed 側が生き返って branch が残ったなら、そう答えていること。
		if result.BranchDeleted {
			t.Fatalf("branch が残っているのに「片付けた」と答えている（%s）: %+v", out, *result)
		}
		if len(result.Leftovers) == 0 {
			t.Fatalf("branch が残ったのに、残ったものを1件も出していない: %+v", *result)
		}
		return
	}
	// 消し切れているなら、BranchDeleted が真であること。
	if !result.BranchDeleted {
		t.Fatalf("branch は消えているのに「片付けた」と答えていない: %+v", *result)
	}
}

// 目的: **壊れた ref のファイルを消したことを、人間の画面へ出す**ことを確認する
// （issue #28 の監査）。`continuo abandon` は Logger を渡さないので、ログにだけ書くと
// 「continuo が `.git` の中のファイルを1つ消した」ことが1文字も伝わらない。
// 与える情報: 用意した worktree の branch の loose な ref を0バイトにした状態。
// 成功条件: CleanupResult.Notices に、消したファイルの絶対パスを含む行があること。
func TestCleanup_壊れたrefを消したことを画面に出す(t *testing.T) {
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

// 目的: **消す前に、壊れる直前に指していた commit を reflog から控える**ことを確認する
// （issue #28 の監査。sweep.go の孤児 branch の削除と同じ規則）。
// 与える情報: commit を1つ積んでから ref を0バイトにした worktree。
// **`<共通ディレクトリ>/logs/refs/heads/<branch>` の最後の行にその SHA が残っている。**
// 成功条件: 人間の画面へ出す行に、消す前の SHA と `git … branch <名前> <SHA>` が入ること。
func TestCleanup_壊れたrefの消す前のcommitを控える(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	branch := cf.Prepared.Branch.String()
	if err := os.WriteFile(filepath.Join(cf.Prepared.Path, "成果.txt"), []byte("未 push\n"), 0o644); err != nil {
		t.Fatalf("worktree にファイルを書けない: %v", err)
	}
	runGit(t, cf.Prepared.Path, "add", ".")
	runGit(t, cf.Prepared.Path, "commit", "--quiet", "-m", "壊れる直前の commit")
	tip := runGit(t, cf.Repo.Dir, "rev-parse", "refs/heads/"+branch)
	breakBranchRef(t, cf.Repo.Dir, branch)

	req := cleanupRequest(cf)
	req.Force = true
	result, err := cf.Manager.Cleanup(context.Background(), req)
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	found := false
	for _, notice := range result.Notices {
		if strings.Contains(notice, tip) && strings.Contains(notice, "branch "+branch+" "+tip) {
			found = true
		}
	}
	if !found {
		t.Fatalf("消す前の commit（%s）と戻し方が画面へ出ていない: %v", tip, result.Notices)
	}
}

// 目的: **起動時の掃除が、壊れた ref も片付ける**ことを確認する（issue #28 の監査。設計 3-22b）。
// 与える情報: worktree を持たない `continuo/orphan-broken` の loose な ref を0バイトにした clone。
// **`git for-each-ref refs/heads` は壊れた ref を一覧に出さない**ので、branch の一覧だけを
// 見ていると、この ref は起動のたびに素通りして溜まり続ける。
// 成功条件: 掃除が消した一覧にその名前があり、ref のファイルが消えていること。
func TestSweep_壊れたrefも掃除する(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	ctx := context.Background()
	prepared, err := fx.Manager.Prepare(ctx, sampleIssue(188))
	if err != nil {
		t.Fatalf("worktree を用意できません: %v", err)
	}
	orphan := "continuo/orphan-broken"
	refPath := breakBranchRef(t, fx.Repo.Dir, orphan)
	// **前提を git に言わせておく。**一覧に出るなら、この検査は別のものを見ている。
	// **`warning: ignoring broken ref …` の行にも名前が出る**ので、行ごとに突き合わせる。
	listed := runGit(t, fx.Repo.Dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	for _, line := range strings.Split(listed, "\n") {
		if strings.TrimSpace(line) == orphan {
			t.Fatalf("壊れた ref が branch の一覧に出ており、この検査の前提が崩れている: %s", listed)
		}
	}

	deleted, err := fx.Manager.SweepOrphanBranches(ctx, workspace.OrphanBranchSweepRequest{
		Worktrees: []string{prepared.Path},
	})
	if err != nil {
		t.Fatalf("SweepOrphanBranches に失敗した: %v", err)
	}
	found := false
	for _, one := range deleted {
		if strings.HasSuffix(one, orphan) {
			found = true
		}
	}
	if !found {
		t.Fatalf("壊れた ref を掃除していない: %v", deleted)
	}
	if _, err := os.Stat(refPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("壊れた ref のファイルが残っている（%s）: %v", refPath, err)
	}
}

// 目的: **worktree が使っている branch の ref が壊れていても、掃除がそれを消さない**
// ことを確認する（issue #28 の監査）。
// 与える情報: Prepare が作った worktree の branch の ref を0バイトにした clone。
// **`git worktree list --porcelain` は ref が壊れた worktree の branch を答えない**ので、
// 一覧だけを見ていると「worktree の無い孤児」と誤判定する。
// 成功条件: 1本も消さず、ref のファイルが残っていること。
func TestSweep_worktreeが使っている壊れたrefは消さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	ctx := context.Background()
	prepared, err := fx.Manager.Prepare(ctx, sampleIssue(188))
	if err != nil {
		t.Fatalf("worktree を用意できません: %v", err)
	}
	branch := prepared.Branch.String()
	refPath := breakBranchRef(t, fx.Repo.Dir, branch)
	// **前提を git に言わせておく。**branch を答えるなら、この検査は何も守らない。
	out := runGit(t, fx.Repo.Dir, "worktree", "list", "--porcelain")
	if strings.Contains(out, "branch refs/heads/"+branch) {
		t.Fatalf("worktree list が壊れた ref の branch を答えており、前提が崩れている:\n%s", out)
	}

	deleted, err := fx.Manager.SweepOrphanBranches(ctx, workspace.OrphanBranchSweepRequest{
		Worktrees: []string{prepared.Path},
	})
	if err != nil {
		t.Fatalf("SweepOrphanBranches に失敗した: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("worktree が使っている branch を消している: %v", deleted)
	}
	if _, err := os.Stat(refPath); err != nil {
		t.Fatalf("worktree が使っている ref を消している（%s）: %v", refPath, err)
	}
}

// 目的: **中身が SHA として読める ref は消さない**ことを確認する
// （issue #28 の監査。設計 3-22b の「読める中身があるなら、消せばその情報が失われる」）。
// 与える情報: SHA-1 のリポジトリに置いた、64桁（SHA-256 の長さ）の16進の loose な ref。
// **git はこれを `reference broken` として扱い、`show-ref` も `rev-parse` も落ちる**
// （実測: 2026-08-25、git 2.50.1）。refname も正しいので、**中身を見る判定だけが
// この ref を守っている。**指していた SHA が読めるので、消せばその値ごと失われる。
// 成功条件: Prepare が失敗し、ref のファイルの中身が1バイトも変わっていないこと。
func TestPrepare_中身がSHAとして読めるrefは消さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	branch := "continuo/octocat/hello-world/188"
	content := []byte("0123456789012345678901234567890123456789012345678901234567890123\n")
	refPath := writeRefFile(t, fx.Repo.Dir, branch, content)
	if _, err := gitTry(t, fx.Repo.Dir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		t.Fatal("`git show-ref --verify` が通ってしまい、この検査の前提が崩れている")
	}
	if _, err := gitTry(t, fx.Repo.Dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		t.Fatal("`git rev-parse --verify` が通ってしまい、この検査の前提が崩れている")
	}
	if out, err := gitTry(t, fx.Repo.Dir, "check-ref-format", "refs/heads/"+branch); err != nil {
		t.Fatalf("refname の検査で落ちてしまい、この検査の前提が崩れている: %v\n%s", err, out)
	}

	if _, err := fx.Manager.Prepare(context.Background(), sampleIssue(188)); err == nil {
		t.Fatal("中身が読める ref のままなのに Prepare が成功した")
	}
	requireRefFileUnchanged(t, refPath, content)
}

// 目的: **worktree の HEAD が指していない branch の ref は、壊れていても消さない**
// ことを確認する（issue #28 の監査。設計 3-9 の段4 の検算）。
// 与える情報: worktree の HEAD は `continuo/octocat/hello-world/188` を指しているのに、
// 身元ファイルの branch は別の `continuo/octocat/hello-world/999` になっている状態。
// **両方の ref が壊れているので、git のコマンドはどちらの branch も答えない。**
// 身元ファイルだけを信じると、その worktree とは無関係の ref を消しに行く。
// 成功条件: 無関係の ref のファイルが残り、branch を「片付けた」と答えないこと。
func TestCleanup_HEADが指していないbranchの壊れたrefは消さない(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	mine := cf.Prepared.Branch.String()
	other := "continuo/octocat/hello-world/999"
	breakBranchRef(t, cf.Repo.Dir, mine)
	otherRef := breakBranchRef(t, cf.Repo.Dir, other)
	requireHeadUnanswerable(t, cf.Repo.Dir, cf.Prepared.Path)
	tamperIdentity(t, cf, func(identity *workspace.Identity) { identity.Branch = other })

	req := cleanupRequest(cf)
	req.Force = true
	result, err := cf.Manager.Cleanup(context.Background(), req)
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if _, err := os.Stat(otherRef); err != nil {
		t.Fatalf("worktree の HEAD が指していない ref を消している（%s）: %v", otherRef, err)
	}
	if result.BranchDeleted {
		t.Fatalf("消していない branch を「片付けた」と答えている: %+v", *result)
	}
}
