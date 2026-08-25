package workspace

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/normalize"
)

// gitBinary は実行する git の名前である。PATH から解決する。
const gitBinary = "git"

// ghqBinary は実行する ghq の名前である。PATH から解決する。
const ghqBinary = "ghq"

// gitOutputLimit は git の標準出力を読む上限（バイト）である。
//
// **上限が無いと、エージェントが worktree に作った大量のファイルの一覧が、
// そのまま常駐プロセスのメモリに載る。**ここを超えた出力は「読み切れなかった」として
// エラーにする（**切り詰めた出力を解析して、無いものを無いと判断しないため**）。
const gitOutputLimit = 4 * 1024 * 1024

// gitStderrLimit は git の標準エラー出力をエラー文に載せる上限（バイト）である。
const gitStderrLimit = 8 * 1024

// gitWaitDelay は、コンテキストが切れて git を殺したあと、出力の読み取りを諦めるまでの
// 猶予である。**これが無いと、git が起こした子プロセスが pipe を握ったままのときに
// Cmd.Wait が返らない**（workspace_hooks と同じ理由。hooks.go の hookWaitDelay を見よ）。
const gitWaitDelay = 5 * time.Second

// runGit は `git -C <dir> <args...>` を実行し、標準出力を返す。
//
// **引数に生の文字列を混ぜないこと。**branch 名など利用者の入力に由来する値は
// normalize.SafeName を通してから渡す（3-7）。
//
// **標準出力は gitOutputLimit までしか読まない。**超えた場合はエラーにする
// （切り詰めた出力を解析すると、branch や worktree の取りこぼしが起きる）。
//
// ctx: 実行に適用するコンテキスト。
// dir: `-C` に渡す作業ディレクトリ。空文字なら `-C` を付けない。
// args: git のサブコマンド以降の引数。
// 戻り値の1つ目: 標準出力（前後の空白を落とす）。
// 戻り値の2つ目: 実行に失敗した場合・出力が上限を超えた場合のエラー。
// 標準エラー出力の内容を必ず含める。
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	out, truncated, err := runGitLimited(ctx, dir, gitOutputLimit, args...)
	if err != nil {
		return out, err
	}
	if truncated {
		full := args
		if dir != "" {
			full = append([]string{"-C", dir}, args...)
		}
		return out, i18n.Errorf(
			i18n.KeyWorkspaceRunGitOutputTooLarge,
			strings.Join(full, " "), gitOutputLimit)
	}
	return out, nil
}

// runGitLimited は標準出力の読み取り上限を指定して git を実行する。
//
// **「出力が空かどうか」しか要らない検査**（`git status --porcelain`）は、小さい上限で
// 呼んで truncated を無視してよい。切り詰めても「空ではない」という答えは変わらない。
//
// ctx: 実行に適用するコンテキスト。
// dir: `-C` に渡す作業ディレクトリ。空文字なら `-C` を付けない。
// stdoutLimit: 標準出力を読む上限（バイト）。
// args: git のサブコマンド以降の引数。
// 戻り値の1つ目: 標準出力（前後の空白を落とす。上限までしか読んでいない）。
// 戻り値の2つ目: 上限を超えて捨てた分があれば true。
// 戻り値の3つ目: 実行に失敗した場合のエラー。標準エラー出力の内容を必ず含める。
func runGitLimited(
	ctx context.Context,
	dir string,
	stdoutLimit int,
	args ...string,
) (string, bool, error) {
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	cmd := exec.CommandContext(ctx, gitBinary, full...)
	stdout := newCappedBuffer(stdoutLimit)
	stderr := newCappedBuffer(gitStderrLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = gitWaitDelay
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(stdout.String()), stdout.Truncated(), i18n.Errorf(
			i18n.KeyWorkspaceRunGitRunFailed,
			strings.Join(full, " "), stderr.text(), err)
	}
	return strings.TrimSpace(stdout.String()), stdout.Truncated(), nil
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
	cmd.WaitDelay = gitWaitDelay
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return -1, i18n.Errorf(i18n.KeyWorkspaceGitExitCodeStartFailed, strings.Join(full, " "), err)
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

// worktreeEntry は `git worktree list --porcelain` が返す worktree 1件である。
type worktreeEntry struct {
	// Path は worktree の絶対パスである（filepath.Clean 済み）。
	Path string
	// Branch はその worktree がチェックアウトしている branch 名である。
	// **detached HEAD の worktree では空文字になる**（`branch` の行が出ない）。
	Branch string
}

// gitWorktreeEntries は `git worktree list --porcelain` の出力を1件ずつに切って返す。
//
// **出力は worktree ごとの塊であり、`worktree <パス>` の行で始まる。**
// その塊の中に `branch refs/heads/<名前>` の行があれば、その worktree はその branch を
// チェックアウトしている。detached HEAD の塊には `detached` の行が出て `branch` は無い。
//
// ctx: 実行に適用するコンテキスト。
// repoDir: リポジトリの作業ディレクトリ。
// 戻り値の1つ目: 登録されている worktree の一覧（git が返した順）。
// 戻り値の2つ目: 実行に失敗した場合のエラー。
func gitWorktreeEntries(ctx context.Context, repoDir string) ([]worktreeEntry, error) {
	out, err := runGit(ctx, repoDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var entries []worktreeEntry
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if rest, ok := strings.CutPrefix(line, "worktree "); ok {
			entries = append(entries, worktreeEntry{Path: filepath.Clean(strings.TrimSpace(rest))})
			continue
		}
		rest, ok := strings.CutPrefix(line, "branch ")
		if !ok || len(entries) == 0 {
			continue
		}
		entries[len(entries)-1].Branch = strings.TrimPrefix(strings.TrimSpace(rest), "refs/heads/")
	}
	if err := scanner.Err(); err != nil {
		return nil, i18n.Errorf(i18n.KeyWorkspaceGitWorktreeListOutputUnreadable, err)
	}
	return entries, nil
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
	entries, err := gitWorktreeEntries(ctx, repoDir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		paths = append(paths, entry.Path)
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
			return i18n.Errorf(
				i18n.KeyWorkspaceGitWorktreeAddOrphanCheckFailed, err, branch.String(), existsErr)
		}
		if !orphan {
			return err
		}
		if _, delErr := runGit(ctx, repoDir, "branch", "-D", branch.String()); delErr != nil {
			return i18n.Errorf(i18n.KeyWorkspaceGitWorktreeAddOrphanDeleteFailed, err, branch.String(), delErr)
		}
		return i18n.Errorf(i18n.KeyWorkspaceGitWorktreeAddOrphanDeleted, err, branch.String())
	}
	return nil
}

// gitCurrentBranch は worktree が実際にチェックアウトしている branch 名を返す。
//
// **worktree を作り直してよいかの判定に使う**（3-22 の再利用の判定）。
// **片付けの段4 の検算には使わない。**あちらはリポジトリ側に答えさせる
// （gitWorktreeBranchAt。worktree の `.git` が壊れていても答えが出るため）。
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 対象の worktree のパス（まだ消していないこと）。
// 戻り値の1つ目: チェックアウト中の branch 名。detached HEAD なら "HEAD" が返る。
// 戻り値の2つ目: 実行に失敗した場合のエラー。
func gitCurrentBranch(ctx context.Context, worktreePath string) (string, error) {
	return runGit(ctx, worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
}

// gitWorktreeBranchAt は、そのパスの worktree がチェックアウトしている branch 名を、
// **リポジトリ側に答えさせて**返す（3-9 の段4 の検算）。
//
// **worktree の `.git` を1バイトも読まない。**あれはエージェントが書き換えられるファイル
// であり、壊れていると `git -C <worktree> …` が1つも通らない（issue #23）。
// `git -C <リポジトリ> worktree list --porcelain` は、worktree の `.git` が空でも
// でたらめでも無くても、その worktree の `branch refs/heads/<名前>` を答える
// （実測: 2026-08-25。`.git` を消した場合は `prunable` の行が増えるだけである）。
//
// ctx: 実行に適用するコンテキスト。
// repoDir: 検算済みのリポジトリの作業ディレクトリ。
// worktreePath: 対象の worktree の絶対パス。
// 戻り値の1つ目: チェックアウト中の branch 名。**detached HEAD なら空文字である。**
// 戻り値の2つ目: 一覧を引けない場合・**その worktree が登録されていない場合**のエラー。
func gitWorktreeBranchAt(ctx context.Context, repoDir, worktreePath string) (string, error) {
	entries, err := gitWorktreeEntries(ctx, repoDir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if samePath(entry.Path, worktreePath) {
			return entry.Branch, nil
		}
	}
	return "", i18n.Errorf(i18n.KeyWorkspaceGitWorktreeBranchAtNotRegistered, worktreePath, repoDir)
}

// gitBranchTip は branch が指している commit の SHA を返す。
//
// **強制削除の前に控えておくために使う**（3-9 の手順6b）。`git branch -D` は
// マージ状態を見ないので、消したあと `git branch <名前> <SHA>` で戻せるように、
// 消す前の SHA をログへ残す。控えを取れなくても削除は止めない（戻せる手掛かりが
// 減るだけである）。
//
// ctx: 実行に適用するコンテキスト。
// repoDir: リポジトリの作業ディレクトリ。
// branch: 対象の branch 名。
// 戻り値の1つ目: branch が指す commit の SHA。
// 戻り値の2つ目: 実行に失敗した場合のエラー。
func gitBranchTip(ctx context.Context, repoDir string, branch normalize.SafeName) (string, error) {
	return runGit(ctx, repoDir, "rev-parse", branch.String())
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

// gitStatusPorcelainLimit は `git status --porcelain` の出力を読む上限（バイト）である。
//
// **片付けの判定は「空かどうか」しか見ない**ので、切り詰めても答えは変わらない
// （切り詰められるほど出ているなら、それは空ではない）。エージェントが worktree に
// 大量のファイルを作った状態でも、常駐プロセスのメモリに全量を載せない。
//
// **件数を数える側は、切り詰めたかどうかを一緒に運ぶこと**（Inspect）。
// 打ち切った出力の行数をそのまま見せると、**失う量を実際より少なく見せる。**
const gitStatusPorcelainLimit = 8 * 1024

// gitStatusPorcelain は `git status --porcelain` の出力を返す（3-9 の段2）。
//
// **未追跡のファイルも数に入れる**（既定で出力される）。エージェントが作った成果物が
// 消えるのを防ぐため、出力が空でなければ「残っている」とする。
//
// **excludePaths に渡した名前は数に入れない。**continuo 自身が worktree の直下に置く
// 身元ファイル（3-18）とその一時ファイルを「利用者の成果」と数えると、
// **その worktree が永久に片付かない**（cleanup.require_clean_worktree が真のままになる）。
// `info/exclude` への登録は利用者の `git status` を汚さないための親切であって、
// **片付けの正しさをその成否に依存させない。**
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 検査する worktree のパス。
// excludePaths: 数に入れない、worktree の直下のファイル名（`.continuo.json` など）。
// 戻り値の1つ目: 標準出力（前後の空白を落としたもの。gitStatusPorcelainLimit まで）。
// 戻り値の2つ目: 上限で打ち切ったかどうか（**真なら、この出力の先にまだファイルがある**）。
// 戻り値の3つ目: 実行に失敗した場合のエラー。
func gitStatusPorcelain(
	ctx context.Context, worktreePath string, excludePaths ...string,
) (string, bool, error) {
	args := []string{"status", "--porcelain"}
	if len(excludePaths) > 0 {
		// pathspec は cwd（= worktree の直下）からの相対である。
		args = append(args, "--", ".")
		for _, name := range excludePaths {
			args = append(args, ":(exclude)"+name)
		}
	}
	out, truncated, err := runGitLimited(ctx, worktreePath, gitStatusPorcelainLimit, args...)
	return out, truncated, err
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
		return 0, i18n.Errorf(i18n.KeyWorkspaceGitAheadOfUpstreamCountUnreadable, out, convErr)
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
		return false, i18n.Errorf(
			i18n.KeyWorkspaceGitNoDiffFromBaseUnexpectedExitCode, base.String(), code)
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

// ghqNotFoundExitCode は `ghq list -p -e` が「該当が無い」ときに返す終了コードである。
// **これ以外の非 0 は本当の失敗として扱う**（clone が無いことに丸めない）。
const ghqNotFoundExitCode = 1

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
// 戻り値の2つ目: ghq を起動できなかった場合・**該当が無いこと以外の理由で非 0 で
// 終わった場合**のエラー（標準エラー出力の内容を含める）。
func RunGhqList(ctx context.Context, owner, repo string) (string, error) {
	ownerName, _ := normalize.Normalize(owner)
	repoName, _ := normalize.Normalize(repo)
	target := ownerName.String() + "/" + repoName.String()

	cmd := exec.CommandContext(ctx, ghqBinary, "list", "-p", "-e", target)
	stdout := newCappedBuffer(gitOutputLimit)
	stderr := newCappedBuffer(gitStderrLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = gitWaitDelay
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return "", i18n.Errorf(
				i18n.KeyWorkspaceRunGhqListStartFailed, target, stderr.text(), err)
		}
		// **「該当が無い」と「ghq が失敗した」を区別する。**該当が無いときの終了コードは
		// 1 である。それ以外の非 0 を「clone が無い」に丸めると、設定の誤りや ghq 自身の
		// 異常が「clone を作りに行け」という誤った案内になり、原因も残らない。
		if exitErr.ExitCode() != ghqNotFoundExitCode || strings.TrimSpace(stdout.String()) != "" {
			return "", i18n.Errorf(
				i18n.KeyWorkspaceRunGhqListExitFailed, target, exitErr.ExitCode(), stderr.text())
		}
		return "", nil
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
		return nil, i18n.Errorf(i18n.KeyWorkspaceGitLocalBranchesOutputUnreadable, err)
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
	entries, err := gitWorktreeEntries(ctx, repoDir)
	if err != nil {
		return nil, err
	}
	branches := map[string]bool{}
	for _, entry := range entries {
		if entry.Branch != "" {
			branches[entry.Branch] = true
		}
	}
	return branches, nil
}

// RunGhqGet は `ghq get <owner>/<repo>` を実行して clone を取ってくる。
//
// **これは書き込みを伴う唯一の ghq の呼び出しである。**呼ぶのは `continuo trust` の
// 本番実行だけで、`--dry-run` と巡回のループからは呼ばない（設計 3-22 / 3-33）。
// **巡回から呼ぶと、ボードに載っただけのリポジトリを無断で clone することになる。**
//
// ctx: 呼び出しに適用するコンテキスト（タイムアウトを含める）。
// owner: リポジトリの所有者名。
// repo: リポジトリ名。
// 戻り値: ghq を起動できなかった場合・非 0 で終わった場合のエラー
// （標準エラー出力の内容を含める）。成功したら nil。
func RunGhqGet(ctx context.Context, owner, repo string) error {
	ownerName, _ := normalize.Normalize(owner)
	repoName, _ := normalize.Normalize(repo)
	target := ownerName.String() + "/" + repoName.String()

	cmd := exec.CommandContext(ctx, ghqBinary, "get", target)
	stderr := newCappedBuffer(gitStderrLimit)
	// **標準出力は捨てる。**ghq は進捗を出すが、continuo の画面に混ぜても読めない。
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr
	cmd.WaitDelay = gitWaitDelay
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return i18n.Errorf(
				i18n.KeyWorkspaceRunGhqGetStartFailed, target, stderr.text(), err)
		}
		return i18n.Errorf(
			i18n.KeyWorkspaceRunGhqGetExitFailed, target, exitErr.ExitCode(), stderr.text())
	}
	return nil
}
