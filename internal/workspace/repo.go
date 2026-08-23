package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
)

// clonePathCacheTTL は `ghq list -p -e <owner>/<repo>` の答えを覚えておく時間である。
//
// **信頼の判定と、破壊的な git コマンドの宛先の検算が、issue ごとに ghq を起動する**
// （3-6 / 3-9）。ボードの件数ぶんプロセスを起こすと巡回1回の費用が跳ね上がるので、
// 短い間だけ覚える。clone の場所が動くのは人間が移したときだけである。
const clonePathCacheTTL = 60 * time.Second

// samePath は2つのパスが同じ場所を指すかを返す。
//
// **シンボリックリンクを解決してから比べる。**この機械の `~/ghq` の下は
// シンボリックリンクなので、素朴な文字列比較では必ず食い違う（3-22）。
// 解決できない側は Clean しただけの値で比べる。
//
// a: 比べるパス。
// b: 比べるパス。
// 戻り値: 同じ場所を指していれば true。
func samePath(a, b string) bool {
	return resolveOrClean(a) == resolveOrClean(b)
}

// resolveOrClean はシンボリックリンクを解決した絶対パスを返す。解決できなければ Clean する。
//
// path: 対象のパス。
// 戻り値: 比較に使えるパス。
func resolveOrClean(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

// ownerRepoFromWorktreePath は worktree の絶対パスから owner と repo を取り出す。
//
// **置き場所は `<root>/<host>/<owner>/<repo>/<スラグ>` の固定4階層である**（3-22）。
// **この2つを身元ファイルから読んではならない。**身元ファイルは worktree の直下にあり、
// その worktree ではエージェントが `--permission-mode dontAsk` で動く（3-16 の段9）ので、
// 中身は書き換えられる。**パスは封じ込め検査（3-20）を通ったものなので、書き換えられない。**
//
// resolvedRoot: EnsureRoot が返した解決済みの置き場所。
// worktreePath: worktree の絶対パス（置き場所の内側であること）。
// 戻り値の1つ目: 所有者名（置き場所の2階層目）。
// 戻り値の2つ目: リポジトリ名（置き場所の3階層目）。
// 戻り値の3つ目: 置き場所の規則に合わない場合のエラー。
func ownerRepoFromWorktreePath(resolvedRoot, worktreePath string) (string, string, error) {
	rel, err := filepath.Rel(filepath.Clean(resolvedRoot), filepath.Clean(worktreePath))
	if err != nil {
		return "", "", i18n.Errorf(
			i18n.KeyWorkspaceOwnerRepoFromWorktreePathRelFailed, worktreePath, resolvedRoot, err)
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	if len(parts) != scanDepth || parts[0] == ".." {
		return "", "", i18n.Errorf(
			i18n.KeyWorkspaceOwnerRepoFromWorktreePathLayoutMismatch, worktreePath)
	}
	return parts[1], parts[2], nil
}

// clonePath は `ghq list -p -e <owner>/<repo>` の答えを、短い間だけ覚えながら返す。
//
// ctx: 実行に適用するコンテキスト。
// owner: リポジトリの所有者名。
// repo: リポジトリ名。
// 戻り値の1つ目: clone の絶対パス（**clone が無ければ空文字**）。
// 戻り値の2つ目: ghq を実行できない場合のエラー（**エラーは覚えない**）。
func (m *Manager) clonePath(ctx context.Context, owner, repo string) (string, error) {
	key := owner + "/" + repo
	if cached, ok := m.clonePaths.get(key); ok {
		return cached, nil
	}
	path, err := m.ghqList(ctx, owner, repo)
	if err != nil {
		return "", err
	}
	m.clonePaths.put(key, path)
	return path, nil
}

// verifiedRepo は worktree が属するリポジトリを、**worktree のパスを根拠に検算して**返す。
//
// **なぜ検算が要るか。**worktree の `.git` はディレクトリではなく `gitdir: …` と書かれた
// だけのファイルであり、**その worktree で動くエージェントが書き換えられる。**
// 書き換えられると `git rev-parse --git-common-dir` は無関係のリポジトリを答え、
// continuo はそこへ `git branch -D`（強制削除）と `info/exclude` への書き込みを撃つ。
//
// そこで、**置き場所のパスから引いた `<owner>/<repo>`**（3-22 の4階層。エージェントが
// 書き換えられない）を `ghq list -p -e` に通した clone のパスと、git が答えた共通
// ディレクトリが同じリポジトリを指すことを確かめる。一致しなければ何もしない。
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 対象の worktree の絶対パス（**置き場所の内側であること。**まだ消していないこと）。
// 戻り値の1つ目: 共通ディレクトリの絶対パス（`info/exclude` を書く先）。
// 戻り値の2つ目: `git -C` に渡せるリポジトリのディレクトリ（`git branch -D` の宛先）。
// 戻り値の3つ目: 共通ディレクトリを引けない・置き場所の規則に合わない・clone を引けない・
// **git の答えと ghq の答えが食い違う**場合のエラー。
func (m *Manager) verifiedRepo(ctx context.Context, worktreePath string) (string, string, error) {
	commonDir, err := gitCommonDir(ctx, worktreePath)
	if err != nil {
		return "", "", err
	}
	repoDir := commonDir
	if filepath.Base(commonDir) == ".git" {
		repoDir = filepath.Dir(commonDir)
	}

	owner, repo, err := ownerRepoFromWorktreePath(m.resolvedRoot, worktreePath)
	if err != nil {
		return "", "", err
	}
	clone, err := m.clonePath(ctx, owner, repo)
	if err != nil {
		return "", "", err
	}
	if clone == "" {
		return "", "", i18n.Errorf(
			i18n.KeyWorkspaceVerifiedRepoCloneNotFound,
			ErrCloneNotFound, owner, repo, worktreePath)
	}
	if !samePath(repoDir, clone) {
		return "", "", i18n.Errorf(
			i18n.KeyWorkspaceVerifiedRepoRepoMismatch,
			worktreePath, repoDir, owner, repo, clone)
	}
	return commonDir, repoDir, nil
}
