package workspace

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/maimuzo/continuo/internal/normalize"
)

// gitBinary は実行する git の名前である。PATH から解決する。
const gitBinary = "git"

// ghqBinary は実行する ghq の名前である。PATH から解決する。
const ghqBinary = "ghq"

// runGit は `git -C <dir> <args...>` を実行し、標準出力を返す。
//
// **引数に生の文字列を混ぜないこと。**branch 名など利用者の入力に由来する値は
// normalize.SafeName を通してから渡す（3-7）。
//
// ctx: 実行に適用するコンテキスト。
// dir: `-C` に渡す作業ディレクトリ。空文字なら `-C` を付けない。
// args: git のサブコマンド以降の引数。
// 戻り値の1つ目: 標準出力（前後の空白を落とす）。
// 戻り値の2つ目: 実行に失敗した場合のエラー。標準エラー出力の内容を必ず含める。
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	cmd := exec.CommandContext(ctx, gitBinary, full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(stdout.String()), fmt.Errorf(
			"`git %s` の実行に失敗しました（stderr: %s）: %w",
			strings.Join(full, " "), strings.TrimSpace(stderr.String()), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// gitExitCode は `git -C <dir> <args...>` を実行し、終了コードを返す。
//
// **終了コードそのものが答えになる検査に使う**（`git diff --quiet` は差分が無ければ 0、
// あれば 1 を返す。`git rev-parse @{u}` は upstream が無ければ非 0 を返す）。
//
// ctx: 実行に適用するコンテキスト。
// dir: `-C` に渡す作業ディレクトリ。
// args: git のサブコマンド以降の引数。
// 戻り値の1つ目: git の終了コード。
// 戻り値の2つ目: git を起動できなかった場合のエラー（終了コードが非 0 なだけなら nil）。
func gitExitCode(ctx context.Context, dir string, args ...string) (int, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, gitBinary, full...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return -1, fmt.Errorf("`git %s` を起動できません: %w", strings.Join(full, " "), err)
}

// gitWorktreePrune は `git worktree prune` を実行する（3-22 の段1）。
//
// 「登録は残っているが実体が消えている」状態を先に解消しないと、
// `fatal: missing but already registered worktree` で worktree の作成が失敗する（実測）。
//
// ctx: 実行に適用するコンテキスト。
// repoDir: リポジトリの作業ディレクトリ。
// 戻り値: 実行に失敗した場合のエラー。
func gitWorktreePrune(ctx context.Context, repoDir string) error {
	_, err := runGit(ctx, repoDir, "worktree", "prune")
	return err
}

// gitWorktreePaths は `git worktree list --porcelain` が返す worktree のパスの一覧を返す。
//
// 「実体はあるが git の登録が無い」（3-22 の段3）の判定に使う。
//
// ctx: 実行に適用するコンテキスト。
// repoDir: リポジトリの作業ディレクトリ。
// 戻り値の1つ目: 登録されている worktree の絶対パスの一覧。
// 戻り値の2つ目: 実行に失敗した場合のエラー。
func gitWorktreePaths(ctx context.Context, repoDir string) ([]string, error) {
	out, err := runGit(ctx, repoDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var paths []string
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if rest, ok := strings.CutPrefix(line, "worktree "); ok {
			paths = append(paths, filepath.Clean(strings.TrimSpace(rest)))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("`git worktree list --porcelain` の出力を読めません: %w", err)
	}
	return paths, nil
}

// gitBranchExists は branch が既にあるかを返す。
//
// ctx: 実行に適用するコンテキスト。
// repoDir: リポジトリの作業ディレクトリ。
// branch: 検査する branch 名。
// 戻り値の1つ目: 存在すれば true。
// 戻り値の2つ目: git を起動できなかった場合のエラー。
func gitBranchExists(ctx context.Context, repoDir string, branch normalize.SafeName) (bool, error) {
	code, err := gitExitCode(ctx, repoDir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch.String())
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

// gitWorktreeAdd は worktree を作る（3-22 の段4・段5）。
//
// branch が既にあればそれをチェックアウトし、無ければ base から新しく作る。
// **`git worktree add -b` は branch を先に作ってからパスを検査するので、
// パスの検査で落ちても branch が残る**（実測）。そこで新しく作る経路が失敗したときは、
// その場で孤児 branch を消す（段5）。
//
// ctx: 実行に適用するコンテキスト。
// repoDir: リポジトリの作業ディレクトリ。
// path: 作る worktree の絶対パス。
// branch: worktree が指す branch 名。
// base: branch を新しく作るときの派生元。branch が既にある場合は使わない。
// 戻り値: 作成に失敗した場合のエラー。孤児 branch の削除も試みた場合は、
// その結果をエラー文に含める。
func gitWorktreeAdd(
	ctx context.Context,
	repoDir, path string,
	branch normalize.SafeName,
	base normalize.SafeName,
) error {
	exists, err := gitBranchExists(ctx, repoDir, branch)
	if err != nil {
		return err
	}
	if exists {
		_, err := runGit(ctx, repoDir, "worktree", "add", path, branch.String())
		return err
	}

	if _, err := runGit(ctx, repoDir, "worktree", "add", "-b", branch.String(), path, base.String()); err != nil {
		// 段5: 先に作られてしまった孤児 branch をその場で消す。
		//
		// **消す前に存在を確かめる。**base を解決できない等で branch が作られる前に
		// 落ちた場合、無条件に `git branch -D` を叩くと「孤児 branch の削除も失敗しました」
		// となり、**孤児 branch が無いだけなのに削除の失敗として見える。**
		// 段5 が求めているのは「先に作られた孤児 branch を消す」ことだけである。
		orphan, existsErr := gitBranchExists(ctx, repoDir, branch)
		if existsErr != nil {
			return fmt.Errorf(
				"%w（孤児 branch %q が残っているかを確かめられませんでした: %v）", err, branch.String(), existsErr)
		}
		if !orphan {
			return err
		}
		if _, delErr := runGit(ctx, repoDir, "branch", "-D", branch.String()); delErr != nil {
			return fmt.Errorf("%w（孤児 branch %q の削除も失敗しました: %v）", err, branch.String(), delErr)
		}
		return fmt.Errorf("%w（先に作られた孤児 branch %q は削除しました）", err, branch.String())
	}
	return nil
}

// gitCurrentBranch は worktree が実際にチェックアウトしている branch 名を返す。
//
// **身元ファイルに書かれた branch 名を検算するために使う**（3-9 の段4）。
// 身元ファイルは worktree の直下にあり、その worktree ではエージェントが
// `--permission-mode dontAsk` で動く（3-16 の段9）ので、**書かれている branch 名は
// エージェントが書き換えられる。**git が答える現物と一致しない値は消さない。
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 対象の worktree のパス（まだ消していないこと）。
// 戻り値の1つ目: チェックアウト中の branch 名。detached HEAD なら "HEAD" が返る。
// 戻り値の2つ目: 実行に失敗した場合のエラー。
func gitCurrentBranch(ctx context.Context, worktreePath string) (string, error) {
	return runGit(ctx, worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
}

// gitBranchDelete は `git branch -D` で branch を消す（3-9 の段4）。
// herdr の worktree.remove は branch を消さないので、continuo が自分で叩く。
//
// ctx: 実行に適用するコンテキスト。
// repoDir: リポジトリの作業ディレクトリ。
// branch: 消す branch 名。
// 戻り値: 削除に失敗した場合のエラー。
func gitBranchDelete(ctx context.Context, repoDir string, branch normalize.SafeName) error {
	_, err := runGit(ctx, repoDir, "branch", "-D", branch.String())
	return err
}

// gitStatusPorcelain は `git status --porcelain` の出力を返す（3-9 の段2）。
//
// **未追跡のファイルも数に入れる**（既定で出力される）。エージェントが作った成果物が
// 消えるのを防ぐため、出力が空でなければ「残っている」とする。
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 検査する worktree のパス。
// 戻り値の1つ目: 標準出力（前後の空白を落としたもの）。
// 戻り値の2つ目: 実行に失敗した場合のエラー。
func gitStatusPorcelain(ctx context.Context, worktreePath string) (string, error) {
	return runGit(ctx, worktreePath, "status", "--porcelain")
}

// gitHasUpstream は現在の branch に upstream があるかを返す（3-9 の段2b）。
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 検査する worktree のパス。
// 戻り値の1つ目: upstream があれば true。
// 戻り値の2つ目: git を起動できなかった場合のエラー。
func gitHasUpstream(ctx context.Context, worktreePath string) (bool, error) {
	code, err := gitExitCode(ctx, worktreePath, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

// gitAheadOfUpstream は `git rev-list --count @{u}..HEAD` の値を返す（3-9 の段2b）。
// **0 なら push 済みである。**
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 検査する worktree のパス。
// 戻り値の1つ目: upstream より先にある commit の数。
// 戻り値の2つ目: 実行に失敗した場合・出力を数値として読めない場合のエラー。
func gitAheadOfUpstream(ctx context.Context, worktreePath string) (int, error) {
	out, err := runGit(ctx, worktreePath, "rev-list", "--count", "@{u}..HEAD")
	if err != nil {
		return 0, err
	}
	n, convErr := strconv.Atoi(strings.TrimSpace(out))
	if convErr != nil {
		return 0, fmt.Errorf("`git rev-list --count @{u}..HEAD` の出力 %q を数値として読めません: %w", out, convErr)
	}
	return n, nil
}

// gitNoDiffFromBase は `git diff --quiet <base>...HEAD` が真（差分なし）かを返す
// （3-9 の段2b の upstream が無い側）。
//
// **commit の有無で判定してはならない。**commit していなくても編集したファイルが
// 残っていれば成果はあるので、その判定は段2（gitStatusPorcelain）が担う。
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 検査する worktree のパス。
// base: worktree を作ったときの base。
// 戻り値の1つ目: 差分が無ければ true（消してよい）。
// 戻り値の2つ目: git を起動できなかった場合・終了コードが 0 でも 1 でもない場合のエラー
// （base が解決できない等。判定できないので呼び出し側は消してはならない）。
func gitNoDiffFromBase(ctx context.Context, worktreePath string, base normalize.SafeName) (bool, error) {
	code, err := gitExitCode(ctx, worktreePath, "diff", "--quiet", base.String()+"...HEAD")
	if err != nil {
		return false, err
	}
	switch code {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, fmt.Errorf(
			"`git diff --quiet %s...HEAD` が想定外の終了コード %d を返しました（base を解決できていない可能性がある）",
			base.String(), code)
	}
}

// gitCommonDir は `git rev-parse --path-format=absolute --git-common-dir` の値を返す。
//
// **worktree では `.git` はファイルである**（ディレクトリではない）。
// 共通ディレクトリはこのコマンドでしか引けない。身元ファイルを `info/exclude` へ
// 登録する先（3-18）と、branch を消すためのリポジトリの位置の特定に使う。
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 対象の worktree のパス。
// 戻り値の1つ目: 共通ディレクトリの絶対パス（通常は `<リポジトリ>/.git`）。
// 戻り値の2つ目: 実行に失敗した場合のエラー。
func gitCommonDir(ctx context.Context, worktreePath string) (string, error) {
	out, err := runGit(ctx, worktreePath, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return filepath.Clean(out), nil
}

// gitToplevel は `git rev-parse --path-format=absolute --show-toplevel` の値を返す。
//
// **信頼を引く鍵はこの出力である**（3-6 の段2）。シンボリックリンクが解決された
// 実体のパスが返る。**worktree のパスで引いてはならない**（信頼はリポジトリ単位で
// 記録されるので必ず「未承認」になる）。
//
// ctx: 実行に適用するコンテキスト。
// dir: リポジトリの中のディレクトリ。
// 戻り値の1つ目: 作業ディレクトリの最上位の絶対パス。
// 戻り値の2つ目: 実行に失敗した場合のエラー。
func gitToplevel(ctx context.Context, dir string) (string, error) {
	out, err := runGit(ctx, dir, "rev-parse", "--path-format=absolute", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return filepath.Clean(out), nil
}

// gitWorktreeRemove は `git worktree remove` で worktree を消す。
//
// **herdr.worktree.create_via_herdr が false のときの経路である**（3-22）。
// 真のときは herdr の worktree.remove を使う（そちらでないと workspace が残る）。
//
// ctx: 実行に適用するコンテキスト。
// repoDir: リポジトリの作業ディレクトリ。
// worktreePath: 消す worktree の絶対パス。
// 戻り値: 削除に失敗した場合のエラー。
func gitWorktreeRemove(ctx context.Context, repoDir, worktreePath string) error {
	_, err := runGit(ctx, repoDir, "worktree", "remove", "--force", worktreePath)
	return err
}

// RunGhqList は実際に `ghq list -p -e <owner>/<repo>` を実行し、clone の絶対パスを返す。
//
// **ghq に worktree を作る機能は無い**（サブコマンドは6つだけ。実測）。
// ここで引くのは「どのリポジトリから worktree を切るか」と「信頼を引く鍵の元」である。
//
// ctx: 実行に適用するコンテキスト。
// owner: リポジトリの所有者名。
// repo: リポジトリ名。
// 戻り値の1つ目: clone の絶対パス。**clone が無ければ空文字を返す**（エラーにしない）。
// 複数行返った場合は1行目を採る。
// 戻り値の2つ目: ghq を起動できなかった場合のエラー。
func RunGhqList(ctx context.Context, owner, repo string) (string, error) {
	ownerName, _ := normalize.Normalize(owner)
	repoName, _ := normalize.Normalize(repo)
	target := ownerName.String() + "/" + repoName.String()

	cmd := exec.CommandContext(ctx, ghqBinary, "list", "-p", "-e", target)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// 該当が無いときも非 0 で終わりうる。出力が空なら「clone が無い」として扱う。
			if strings.TrimSpace(stdout.String()) == "" {
				return "", nil
			}
		} else {
			return "", fmt.Errorf(
				"`ghq list -p -e %s` を起動できません（stderr: %s）: %w",
				target, strings.TrimSpace(stderr.String()), err)
		}
	}

	for line := range strings.SplitSeq(strings.TrimSpace(stdout.String()), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return filepath.Clean(trimmed), nil
		}
	}
	return "", nil
}

// gitLocalBranches はリポジトリの local branch 名の一覧を返す（3-9 の手順6b）。
//
// **`git branch` ではなく `git for-each-ref` を使う。**`git branch` の出力は
// チェックアウト中の branch に `*` や `+` の印を付け、worktree のパスを添えるため、
// 名前だけを取り出すのに文字列の削り取りが要る。
//
// ctx: 実行に適用するコンテキスト。
// repoDir: リポジトリの作業ディレクトリ。
// 戻り値の1つ目: local branch 名の一覧（git が返した順）。
// 戻り値の2つ目: 実行に失敗した場合のエラー。
func gitLocalBranches(ctx context.Context, repoDir string) ([]string, error) {
	out, err := runGit(ctx, repoDir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, err
	}
	var names []string
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if name != "" {
			names = append(names, name)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("`git for-each-ref` の出力を読めません: %w", err)
	}
	return names, nil
}

// gitWorktreeBranches は、いずれかの worktree がチェックアウトしている branch 名の集合を返す
// （3-9 の手順6b の「対応する worktree も無く」の判定に使う）。
//
// `git worktree list --porcelain` の `branch refs/heads/<名前>` の行を読む。
// detached HEAD の worktree にはこの行が無いので、集合に入らない。
//
// ctx: 実行に適用するコンテキスト。
// repoDir: リポジトリの作業ディレクトリ。
// 戻り値の1つ目: チェックアウト中の branch 名の集合。
// 戻り値の2つ目: 実行に失敗した場合のエラー。
func gitWorktreeBranches(ctx context.Context, repoDir string) (map[string]bool, error) {
	out, err := runGit(ctx, repoDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	branches := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		rest, ok := strings.CutPrefix(scanner.Text(), "branch ")
		if !ok {
			continue
		}
		name := strings.TrimPrefix(strings.TrimSpace(rest), "refs/heads/")
		if name != "" {
			branches[name] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("`git worktree list --porcelain` の出力を読めません: %w", err)
	}
	return branches, nil
}
