package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/normalize"
)

// NativeRefDefaultBranch は、herdr.worktree.base が null のときに base として読む
// Issue.NativeRef のキーである（3-22 の段4）。
// **このキーも無ければ、その issue を失敗として扱う。base を推測しない。**
const NativeRefDefaultBranch = "default_branch"

// ErrUnregisteredWorktree は、目的のパスに実体はあるが git の登録が無いことを表す
// （3-22 の段3）。
//
// **乗っ取らずエラーにする。**continuo が作ったものではない可能性があり、
// 空ディレクトリは git が黙って乗っ取るためである。
var ErrUnregisteredWorktree = errors.New("目的のパスに実体があるのに git の worktree として登録されていません")

// ErrCloneNotFound は `ghq list -p -e <owner>/<repo>` で clone を引けなかったことを表す。
// **continuo は勝手に clone しない**（3-22）。その issue を飛ばして人間に知らせる。
var ErrCloneNotFound = errors.New("ghq に対象リポジトリの clone がありません")

// ErrBaseUnknown は base を決められなかったことを表す（3-22 の段4）。
// herdr.worktree.base が null で、Issue.NativeRef["default_branch"] も無い場合に返る。
var ErrBaseUnknown = errors.New("worktree を切る base を決められません")

// PrepareResult は worktree を用意した結果である。
type PrepareResult struct {
	// Path は worktree の絶対パスである（シンボリックリンク解決済み）。
	Path string
	// Branch は描画・正規化した branch 名である。
	Branch normalize.SafeName
	// Base は worktree を作ったときの base である。
	// **片付けの手順2b（upstream が無い側の判定）で使う**ので、呼び出し側が保持すること。
	Base normalize.SafeName
	// RepoPath は ghq で引いた clone の絶対パスである（branch の削除などに使う）。
	RepoPath string
	// Created は worktree を新しく作ったかどうかである。
	// **偽なら再利用である。**workspace_hooks.after_create は真のときだけ実行する（3-16 の段4）。
	Created bool
	// HerdrWorkspaceID は herdr が開いた workspace の ID である。
	// **worktree.remove がこの ID を要求する**（3-9）。身元ファイルへ必ず書くこと。
	// herdr.worktree.create_via_herdr が false のときは空文字になる。
	HerdrWorkspaceID string
	// HerdrPaneID は worktree.open が作った workspace の中の pane の ID である。
	// **pane.split も tab.create も呼ばない**（4-5）。この pane をそのまま使う。
	HerdrPaneID string
	// AlreadyOpen は、その worktree が herdr で既に開かれていたかどうかである。
	AlreadyOpen bool
	// ExistingIdentity は再利用のときに読めた既存の身元ファイルである。
	// **無い・壊れている場合は nil**（新規として扱う。3-18）。
	// MergeForReuse にそのまま渡すことで takeover_count と created_at の扱いが決まる。
	ExistingIdentity *Identity
	// Warnings は識別子の正規化で情報が落ちた場合の警告である（3-7）。
	Warnings []normalize.Warning
}

// Prepare は issue のための worktree を用意する（3-22 の手順7段を、その順で実行する）。
//
//  1. git worktree prune
//  2. 目的のパスに worktree があり git にも登録されていれば再利用する
//  3. 実体はあるが登録が無ければエラーにする（乗っ取らない）
//  4. 無ければ作る（branch があればチェックアウト、無ければ base から作る）
//  5. 作成に失敗したらその場で孤児 branch を消す
//  6. 封じ込め検査を通す（3-20）
//  7. herdr の worktree.open を呼ぶ（create ではない）
//
// **封じ込め検査は作る前と作ったあとの2回行う**（3-20）。作る前はパスがまだ無いので
// 解決できず、作ったあとに EvalSymlinks で解決して比較し直す。
// **食い違ったら worktree を消さずに残してエラーを返す**（消してよい対象か判断できない）。
//
// **成功したら BeginRun を呼ぶ。**用意が済んだ時点から新しい run が始まるので、
// after_run の「1回だけ」の印をここで消す（3-18。再利用＝再び dispatch された）。
//
// ctx: 実行に適用するコンテキスト。
// issue: 対象の issue。
// 戻り値の1つ目: 用意した worktree の情報。
// 戻り値の2つ目: clone を引けない（ErrCloneNotFound）・base を決められない
// （ErrBaseUnknown）・登録の無い実体がある（ErrUnregisteredWorktree）・
// 封じ込め検査に落ちた・git や herdr の実行に失敗した場合のエラー。
func (m *Manager) Prepare(ctx context.Context, issue IssueRef) (*PrepareResult, error) {
	loc, warnings, err := Locate(m.resolvedRoot, m.cfg.Herdr.Worktree.BranchTemplate, issue)
	if err != nil {
		return nil, err
	}

	// 3-20 の段3: 作る前の封じ込め検査。パスはまだ無いので解決せずに比較する。
	if err := CheckContainment(m.resolvedRoot, loc.Path); err != nil {
		return nil, err
	}

	repoPath, err := m.ghqList(ctx, issue.Owner, issue.Repo)
	if err != nil {
		return nil, err
	}
	if repoPath == "" {
		return nil, fmt.Errorf("%w: %s/%s", ErrCloneNotFound, issue.Owner, issue.Repo)
	}

	// 段1: 「登録は残っているが実体が消えている」を先に解消する。
	if err := gitWorktreePrune(ctx, repoPath); err != nil {
		return nil, err
	}

	result := &PrepareResult{
		Path:     loc.Path,
		Branch:   loc.Branch,
		RepoPath: repoPath,
		Warnings: warnings,
	}

	registered, err := m.isRegisteredWorktree(ctx, repoPath, loc.Path)
	if err != nil {
		return nil, err
	}
	_, statErr := os.Stat(loc.Path)
	exists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("worktree のパス %s を確認できません: %w", loc.Path, statErr)
	}

	switch {
	case exists && registered:
		// 段2: 再利用する。既存の身元ファイルを先に読む（3-18）。
		result.Created = false
		if identity, err := m.ReadIdentity(loc.Path); err != nil {
			// 無い・壊れているときは新規として扱う（3-18）。**消さない。**
			m.logger.Info("既存の身元ファイルを使えないので新規として扱います",
				"worktree", loc.Path, "error", err)
		} else {
			result.ExistingIdentity = identity
		}
	case exists && !registered:
		// 段3: 乗っ取らない。
		return nil, fmt.Errorf("%w: %s", ErrUnregisteredWorktree, loc.Path)
	default:
		// 段4・段5。
		base, err := m.resolveBase(issue)
		if err != nil {
			return nil, err
		}
		result.Base = base
		if err := os.MkdirAll(filepath.Dir(loc.Path), rootDirPerm); err != nil {
			return nil, fmt.Errorf("worktree の親ディレクトリ %s を作成できません: %w", filepath.Dir(loc.Path), err)
		}
		if err := gitWorktreeAdd(ctx, repoPath, loc.Path, loc.Branch, base); err != nil {
			return nil, err
		}
		result.Created = true
	}

	if result.Base == "" {
		// 再利用の経路でも、片付けの手順2b が base を要る（3-9）。
		// ここで決められない場合（default_branch が無い等）は空のままにし、
		// 片付け側が「判定できないので消さない」と扱う。
		if base, err := m.resolveBase(issue); err == nil {
			result.Base = base
		}
	}

	// 3-20 の段4: 作ったあとにもう一度解決して比較する。
	resolvedPath, err := CheckContainmentResolved(m.resolvedRoot, loc.Path)
	if err != nil {
		// **worktree を消さずに残す。**置き場所の外側に実体があるので、
		// continuo が消してよい対象か判断できない（3-20）。
		return nil, err
	}
	result.Path = resolvedPath

	// 段7: herdr に開かせる。**create ではない**（実体は段4 で git が作り終えている）。
	if m.cfg.Herdr.Worktree.CreateViaHerdr {
		if m.herdr == nil {
			return nil, fmt.Errorf(
				"herdr.worktree.create_via_herdr が真ですが herdr のクライアントが設定されていません")
		}
		focus := false
		opened, err := m.herdr.WorktreeOpen(ctx, herdr.WorktreeOpenParams{
			Path:   resolvedPath,
			Branch: loc.Branch.String(),
			Cwd:    repoPath,
			Focus:  &focus,
			Label:  issue.URL,
		})
		if err != nil {
			return nil, fmt.Errorf("herdr の worktree.open に失敗しました（%s）: %w", resolvedPath, err)
		}
		result.HerdrWorkspaceID = opened.Workspace.WorkspaceID
		result.HerdrPaneID = opened.RootPane.PaneID
		result.AlreadyOpen = opened.AlreadyOpen
	}

	// **ここから先は新しい run である**（3-18）。after_run の「1回だけ」の印を消す。
	// 消さないと、再利用した worktree の2回目の run で after_run が実行されない。
	m.BeginRun(resolvedPath)

	return result, nil
}

// isRegisteredWorktree は path が git の worktree として登録されているかを返す。
//
// **シンボリックリンクを解決してから比較する。**git は実体に解決したパスを返すので、
// 素朴な文字列比較では一致しない（3-22）。
//
// ctx: 実行に適用するコンテキスト。
// repoPath: リポジトリの作業ディレクトリ。
// path: 検査する worktree のパス。
// 戻り値の1つ目: 登録されていれば true。
// 戻り値の2つ目: `git worktree list` の実行に失敗した場合のエラー。
func (m *Manager) isRegisteredWorktree(ctx context.Context, repoPath, path string) (bool, error) {
	registered, err := gitWorktreePaths(ctx, repoPath)
	if err != nil {
		return false, err
	}
	candidates := map[string]bool{filepath.Clean(path): true}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		candidates[filepath.Clean(resolved)] = true
	}
	for _, entry := range registered {
		if candidates[entry] {
			return true, nil
		}
		if resolved, err := filepath.EvalSymlinks(entry); err == nil && candidates[filepath.Clean(resolved)] {
			return true, nil
		}
	}
	return false, nil
}

// resolveBase は worktree を切る base を決める（3-22 の段4）。
//
// 設定の herdr.worktree.base を使い、null なら Issue.NativeRef["default_branch"] を読む。
// **そのキーも無ければ ErrBaseUnknown を返す。base を推測しない。**
//
// issue: 対象の issue。
// 戻り値の1つ目: 正規化を通った base の名前。
// 戻り値の2つ目: base を決められない場合の ErrBaseUnknown。
func (m *Manager) resolveBase(issue IssueRef) (normalize.SafeName, error) {
	if configured := m.cfg.Herdr.Worktree.Base; configured != nil && *configured != "" {
		name, warnings := normalize.Normalize(*configured)
		m.logWarnings(warnings)
		return name, nil
	}
	raw, ok := issue.NativeRef[NativeRefDefaultBranch]
	if !ok {
		return "", fmt.Errorf(
			"%w: herdr.worktree.base が null で、issue の NativeRef[%q] もありません（%s）",
			ErrBaseUnknown, NativeRefDefaultBranch, issue.Identifier)
	}
	s, ok := raw.(string)
	if !ok || s == "" {
		return "", fmt.Errorf(
			"%w: issue の NativeRef[%q] が文字列ではありません（%s、値 %v）",
			ErrBaseUnknown, NativeRefDefaultBranch, issue.Identifier, raw)
	}
	name, warnings := normalize.Normalize(s)
	m.logWarnings(warnings)
	return name, nil
}

// logWarnings は識別子の正規化で情報が落ちた警告をログへ出す（3-7）。
// naming.warn_on_information_loss が偽なら何もしない。
//
// warnings: 出力する警告。
func (m *Manager) logWarnings(warnings []normalize.Warning) {
	if !m.cfg.Naming.WarnOnInformationLoss {
		return
	}
	for _, w := range warnings {
		m.logger.Warn(w.Message, "original", w.Original, "result", w.Result.String())
	}
}
