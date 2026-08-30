package workspace

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/maimuzo/continuo/internal/i18n"
)

// scanDepth は置き場所を掘る深さである。
// `<root>/<host>/<owner>/<repo>/<スラグ>` の**固定の4階層**で、それより深くは掘らない
// （3-4 の段2）。
const scanDepth = 4

// ScannedWorktree は置き場所の走査で見つかった worktree 1件である。
type ScannedWorktree struct {
	// Path は worktree の絶対パスである。
	Path string
	// Identity は読めた身元ファイルの中身である。読めなければ nil。
	Identity *Identity
	// Err は身元ファイルを読めなかった理由である（ErrIdentityBroken など）。
	// **Err が付いていても worktree を消してはならない**（3-4 の段2）。ログに出して残す。
	Err error
}

// Scan は置き場所を走査し、身元ファイルを持つ worktree の一覧を返す（3-4 の段2）。
//
// **深さは固定の4階層である**（`<root>/<host>/<owner>/<repo>/<スラグ>`）。
// それより深くは掘らない。**身元ファイルが無いディレクトリは結果に含めない**
// （人間が置いた worktree かもしれない）。**JSON が壊れていたものは Err を付けて含める**
// （呼び出し側がログに出す。消さない）。
//
// 戻り値の1つ目: 見つかった worktree の一覧（パスの昇順）。
// 戻り値の2つ目: 置き場所そのものを読めない場合のエラー。
// 途中の階層が読めない場合はその階層を飛ばし、エラーにはしない。
func (m *Manager) Scan() ([]ScannedWorktree, error) {
	dirs, err := m.scanLevel(m.resolvedRoot, scanDepth)
	if err != nil {
		return nil, err
	}

	found := make([]ScannedWorktree, 0, len(dirs))
	for _, dir := range dirs {
		identity, readErr := m.ReadIdentity(dir)
		if readErr != nil {
			if errors.Is(readErr, ErrIdentityNotFound) {
				// 人間が置いた worktree かもしれないので、結果に含めない。
				continue
			}
			found = append(found, ScannedWorktree{Path: dir, Err: readErr})
			continue
		}
		found = append(found, ScannedWorktree{Path: dir, Identity: identity})
	}
	return found, nil
}

// ScanUnidentified は置き場所の走査で見つかった、**身元ファイルが無いディレクトリ**を返す
// （3-37-9c）。
//
// **Scan が結果に含めないものを、別の口で数えられるようにするためにある。**
// 着手は worktree を作ってから身元ファイルを書くので（3-16 の段6〜段9）、
// **その間で落ちると身元ファイルの無い worktree ができる。**それを「無かったこと」に
// すると、`continuo abandon` は「この issue の worktree はありません」と言って
// 残った branch を消しにいく。**その branch は孤児ではなく、目の前の worktree のもの
// かもしれない。**
//
// **消す判断には使わない。**人間が置いた worktree かもしれないので、continuo は
// 触れない（3-4 の段2）。**数えて止まるためだけの値である。**
//
// 戻り値の1つ目: 身元ファイルが無いディレクトリの絶対パス（パスの昇順）。
// 戻り値の2つ目: 置き場所そのものを読めない場合のエラー。
// 途中の階層が読めない場合はその階層を飛ばし、エラーにはしない。
func (m *Manager) ScanUnidentified() ([]string, error) {
	dirs, err := m.scanLevel(m.resolvedRoot, scanDepth)
	if err != nil {
		return nil, err
	}

	var found []string
	for _, dir := range dirs {
		if _, readErr := m.ReadIdentity(dir); readErr != nil && errors.Is(readErr, ErrIdentityNotFound) {
			found = append(found, dir)
		}
	}
	return found, nil
}

// scanLevel は dir の下を depth 階層だけ掘り、その深さのディレクトリの一覧を返す。
//
// dir: 掘り始めるディレクトリ。
// depth: 残りの階層数。0 になったら dir 自身を返す。
// 戻り値の1つ目: depth 階層下のディレクトリの絶対パス（各階層で名前の昇順）。
// 戻り値の2つ目: 最上位の dir を読めない場合のエラー。
// 途中の階層が読めない場合はその階層を飛ばして続ける。
func (m *Manager) scanLevel(dir string, depth int) ([]string, error) {
	if depth == 0 {
		return []string{dir}, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if depth == scanDepth {
			return nil, i18n.Errorf(i18n.KeyWorkspaceScanLevelRootUnreadable, dir, err)
		}
		m.logger.Warn("置き場所の走査で読めないディレクトリを飛ばしました", "dir", dir, "error", err)
		return nil, nil
	}

	var result []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		child := filepath.Join(dir, entry.Name())
		// **別の continuo（`--id <名前>`）の置き場所へは1階層も入らない**（設計 3-17b）。
		// **入ると、あちらの worktree が「身元ファイルの無いディレクトリ」として数えられ、
		// 既定側の `continuo abandon` が判断を保留したまま止まる。**
		// **見るのは置き場所の直下だけである**（`--id` が足すのは1階層だけなので、
		// それより深くを見ても、当たるのは worktree の中だけになる）。
		if depth == scanDepth && isOtherInstanceRoot(child) {
			m.logger.Debug("別の continuo の置き場所なので走査から外します",
				"dir", child, "marker", InstanceMarkerName)
			continue
		}
		deeper, err := m.scanLevel(child, depth-1)
		if err != nil {
			return nil, err
		}
		result = append(result, deeper...)
	}
	return result, nil
}
