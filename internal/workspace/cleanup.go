package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/normalize"
)

// CleanupResult は片付けを試みた結果である（3-9）。
type CleanupResult struct {
	// Removed は worktree と branch を消したかどうかである。
	Removed bool
	// Deferred は消さずに見送ったかどうかである。
	//
	// **cleanup.enabled が false のときも真になる**（3-9 の手順5）。
	// 「設定で無効にしてある」も見送りの一種であり、Reasons にその旨が入る。
	// **Reasons が空でないのに Deferred が偽、という状態は作らない。**
	// 呼び出し側は ShouldComment で「issue へ書くべき見送り」だけを選ぶ
	// （設定で無効のときは偽になるので、コメントは出ない）。
	Deferred bool
	// Reasons は見送った理由である（人間が読む文。issue のコメントとログに出す）。
	Reasons []string
	// ShouldComment は、この見送りを issue へコメントすべきかどうかである。
	// **身元ファイルの CleanupDeferredAt がゼロ値のときだけ true になる**（3-9 の手順2c）。
	// 真のときだけ orchestrator がコメントし、**投稿に成功したあとで**
	// MarkCleanupDeferred を呼ぶ。投稿の前に書くと、投稿が失敗したときに
	// コメントが永久に出なくなる。
	ShouldComment bool
	// Identity は読み取った身元ファイルである。コメント本文の組み立てに使う。
	Identity *Identity
}

// CleanupRequest は片付けの入力である。
type CleanupRequest struct {
	// WorktreePath は片付ける worktree の絶対パスである。
	WorktreePath string
	// Base は worktree を作ったときの base である（3-9 の手順2b）。
	// **upstream が無いときの `git diff --quiet <base>...HEAD` に使う。**
	// 空文字だと upstream が無い branch の判定ができないので、
	// cleanup.require_pushed が真なら「判定できないので消さない」として見送る。
	Base normalize.SafeName
}

// ShouldCleanup は、その Status が cleanup.on_states に入っているかを返す（3-9 の手順1）。
//
// **「active でなくなった時点」ではなく、この一覧に入った時点で片付ける。**
// In Review と Blocked は active_states に入らないが、そこで消すと、人間が回答して
// Ready へ戻したときに作業成果が失われる。
//
// **照合は大文字小文字を無視する**（トラッカーの綴りをそのまま保ち、比較のときだけ
// 無視するという 3-13 / SPEC.md 11.3 の規則に合わせる）。
//
// state: トラッカーから取り直した現在の Status。
// 戻り値: cleanup.on_states に入っていれば true。
func (m *Manager) ShouldCleanup(state string) bool {
	for _, target := range m.cfg.Cleanup.OnStates {
		if strings.EqualFold(strings.TrimSpace(target), strings.TrimSpace(state)) {
			return true
		}
	}
	return false
}

// Cleanup は worktree と branch と issue ごとの設定ファイルを片付ける（3-9 の手順を全部通す）。
//
// **呼ぶのは、その issue の Status が cleanup.on_states に入った時点である**
// （「active でなくなった時点」ではない。In Review と Blocked で消すと、
// 人間が回答して Ready へ戻したときに作業成果が失われる）。
//
// 手順は次のとおりで、**2 と 2b は両方通す**（片方だけでは失うものを見落とす）。
//
//	2  git status --porcelain が空でなければ消さない（未追跡も数える）
//	2b upstream があれば git rev-list --count @{u}..HEAD が 0 なら消してよい。
//	   upstream が無ければ git diff --quiet <base>...HEAD が真なら消してよい
//	2d workspace_hooks.before_remove を、消す前の worktree を cwd にして実行する
//	   （失敗しても記録して続ける）
//	3  herdr の worktree.remove を workspace の ID で呼ぶ（path でも branch でもない）
//	4  branch は herdr が消さないので git branch -D を自分で叩く
//
// **worktree.remove のあとに workspace.close を呼ばない。**応答に workspace が入り、
// workspace ごと閉じられる。
//
// ctx: 実行に適用するコンテキスト。
// req: 片付ける worktree と、その worktree を作ったときの base。
// 戻り値の1つ目: 片付けた／見送った結果。
// 戻り値の2つ目: 封じ込め検査に落ちた・身元ファイルを読めない・
// worktree の削除に失敗した場合のエラー。**封じ込め検査に落ちた場合は何も消さない**
// （3-20。「消す直前」がいちばん危ない検査点である）。
func (m *Manager) Cleanup(ctx context.Context, req CleanupRequest) (*CleanupResult, error) {
	result := &CleanupResult{}

	if !m.cfg.Cleanup.Enabled {
		// 「設定で無効」も見送りである。Reasons だけを埋めて Deferred を偽のままにすると、
		// 呼び出し側が「消した」「見送った」「無効」を戻り値から区別できない。
		// **issue へのコメントは出さない**（人間が自分で無効にしたのだから知っている）。
		result.Deferred = true
		result.ShouldComment = false
		result.Reasons = append(result.Reasons, "cleanup.enabled が false なので片付けを行いません")
		return result, nil
	}

	// 3-20: 消す直前の封じ込め検査。ここが最も危ない。
	resolvedPath, err := CheckContainmentResolved(m.resolvedRoot, req.WorktreePath)
	if err != nil {
		return nil, err
	}

	identity, err := m.ReadIdentity(req.WorktreePath)
	if err != nil {
		return nil, err
	}
	result.Identity = identity

	// worktree を消したあとでは共通ディレクトリを引けないので、先に引いておく。
	repoDir, err := m.repoDirOf(ctx, req.WorktreePath)
	if err != nil {
		return nil, err
	}

	reasons, err := m.leftoverReasons(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(reasons) > 0 {
		result.Deferred = true
		result.Reasons = reasons
		result.ShouldComment = identity.CleanupDeferredAt.IsZero()
		m.logger.Warn("worktree を消さずに残しました",
			"worktree", resolvedPath,
			"issue", identity.IssueIdentifier,
			"reasons", fmt.Sprint(reasons),
			"should_comment", result.ShouldComment)
		return result, nil
	}

	// 段4 の準備: 消してよい branch かどうかを、**worktree がまだあるうちに**検算する
	// （git に現物を答えさせる検査が要るため。deletableBranch を見よ）。
	branch, branchDeletable := m.deletableBranch(ctx, req.WorktreePath, identity)

	// 2d: 失敗しても記録して続ける（片付けを止めない）。
	if err := m.RunHook(ctx, HookBeforeRemove, req.WorktreePath); err != nil {
		m.logger.Warn("workspace_hooks.before_remove が失敗しました（片付けは続けます）",
			"worktree", resolvedPath, "error", err)
	}

	// 3: worktree を消す。
	if err := m.removeWorktree(ctx, identity, repoDir, resolvedPath); err != nil {
		return nil, err
	}

	// 4: branch は herdr が消さないので自分で叩く。
	if m.cfg.Cleanup.DeleteBranch && branchDeletable {
		if err := gitBranchDelete(ctx, repoDir, branch); err != nil {
			m.logger.Warn("branch を消せませんでした", "branch", branch.String(), "error", err)
		}
	}

	// issue ごとの設定ファイル（3-12）も一緒に消す。
	m.removeSettingsFile(identity.SettingsPath)

	result.Removed = true
	m.logger.Info("worktree と branch を片付けました",
		"worktree", resolvedPath, "branch", identity.Branch, "issue", identity.IssueIdentifier)
	return result, nil
}

// deletableBranch は、身元ファイルに書かれた branch を `git branch -D` に渡してよいかを
// 検算する（3-9 の段4）。
//
// **なぜ検算が要るか。**身元ファイルは worktree の直下（`<worktree>/.continuo.json`。3-18）に
// あり、その worktree ではエージェントが `--permission-mode dontAsk` で動く（3-16 の段9）。
// **つまり branch の値はエージェントが書き換えられる。**検算せずに渡すと、
// 利用者の clone の別 branch（`main` など）を消させられる。
//
// 通す条件は次の3つを全部満たすことである。
//
//   - 正規化（3-7）を通した結果が、書かれていた文字列とそのまま一致すること
//     （情報が落ちる値は、continuo が書いた値ではない）
//   - herdr.worktree.branch_template の接頭辞（既定は `continuo/`）で始まること。
//     **テンプレートに変数が無く接頭辞を決められないときは消さない**（3-9 の段6b と同じ判断）
//   - **worktree が実際にチェックアウトしている branch と一致すること。**
//     git が答える現物との突き合わせであり、身元ファイルだけでは成立しない
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 対象の worktree のパス（**まだ消していないこと。**HEAD を引くため）。
// identity: 読み取った身元ファイル。
// 戻り値の1つ目: 消してよい branch 名。
// 戻り値の2つ目: 消してよければ true。**偽のときは何も消さない**（理由は警告としてログに出す）。
func (m *Manager) deletableBranch(
	ctx context.Context,
	worktreePath string,
	identity *Identity,
) (normalize.SafeName, bool) {
	if identity.Branch == "" {
		return "", false
	}

	branch, warnings := normalize.Normalize(identity.Branch)
	m.logWarnings(warnings)
	if branch.String() != identity.Branch {
		m.logger.Warn("身元ファイルの branch 名が正規化で変わるので消しません",
			"identity_path", m.IdentityPath(worktreePath),
			"branch", identity.Branch, "normalized", branch.String())
		return "", false
	}

	prefix := BranchPrefix(m.cfg.Herdr.Worktree.BranchTemplate)
	if prefix == "" {
		m.logger.Warn("herdr.worktree.branch_template に変数が無いので branch を消しません",
			"branch_template", m.cfg.Herdr.Worktree.BranchTemplate, "branch", branch.String())
		return "", false
	}
	if !strings.HasPrefix(branch.String(), prefix) {
		m.logger.Warn("身元ファイルの branch が continuo の接頭辞で始まらないので消しません",
			"identity_path", m.IdentityPath(worktreePath),
			"branch", branch.String(), "prefix", prefix)
		return "", false
	}

	head, err := gitCurrentBranch(ctx, worktreePath)
	if err != nil {
		m.logger.Warn("worktree がチェックアウトしている branch を引けないので branch を消しません",
			"worktree", worktreePath, "error", err)
		return "", false
	}
	if head != branch.String() {
		m.logger.Warn("身元ファイルの branch が worktree の現物と一致しないので消しません",
			"identity_path", m.IdentityPath(worktreePath),
			"identity_branch", branch.String(), "checked_out", head)
		return "", false
	}

	return branch, true
}

// removeSettingsFile は issue ごとの設定ファイル（3-12）を消す。
//
// **なぜ検査するか。**settings_path も身元ファイルの中の値であり、
// エージェントが書き換えられる（deletableBranch の説明と同じ理由）。検査せずに
// os.Remove へ渡すと、任意の1ファイルを消させられる。
//
// 3-12 は置き場所を `<実行時ディレクトリ>/issues/<issue>/settings.json` と定めているので、
// **Options.SettingsRoot の内側にあることだけを通す。**
// **SettingsRoot が空なら消さない**（内側かどうかを確かめられないため）。
//
// settingsPath: 身元ファイルに書かれていた設定ファイルのパス。空なら何もしない。
func (m *Manager) removeSettingsFile(settingsPath string) {
	if settingsPath == "" {
		return
	}
	if m.settingsRoot == "" {
		m.logger.Warn("issue ごとの設定ファイルの置き場所が分からないので消しません"+
			"（workspace.Options.SettingsRoot が空）", "settings_path", settingsPath)
		return
	}
	if err := checkUnder(m.settingsRoot, settingsPath, "issue ごとの設定ファイル"); err != nil {
		m.logger.Warn("身元ファイルの settings_path が置き場所の外側なので消しません",
			"settings_path", settingsPath, "settings_root", m.settingsRoot, "error", err)
		return
	}
	if err := os.Remove(settingsPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		m.logger.Warn("issue ごとの設定ファイルを消せませんでした",
			"settings_path", settingsPath, "error", err)
	}
}

// leftoverReasons は「失うものがあるか」を検査し、見送る理由を返す（3-9 の手順2 と 2b）。
//
// **commit の有無では判定しない。**commit していなくても編集したファイルが残っていれば
// 成果はあるので、手順2（git status --porcelain）で拾う。
//
// ctx: 実行に適用するコンテキスト。
// req: 対象の worktree と base。
// 戻り値の1つ目: 見送る理由（空なら消してよい）。
// 戻り値の2つ目: git を実行できない場合のエラー。
func (m *Manager) leftoverReasons(ctx context.Context, req CleanupRequest) ([]string, error) {
	var reasons []string

	if m.cfg.Cleanup.RequireCleanWorktree {
		status, err := gitStatusPorcelain(ctx, req.WorktreePath)
		if err != nil {
			return nil, err
		}
		if status != "" {
			reasons = append(reasons, "コミットされていない変更が残っている（未追跡のファイルを含む）")
		}
	}

	if m.cfg.Cleanup.RequirePushed {
		hasUpstream, err := gitHasUpstream(ctx, req.WorktreePath)
		if err != nil {
			return nil, err
		}
		if hasUpstream {
			ahead, err := gitAheadOfUpstream(ctx, req.WorktreePath)
			if err != nil {
				return nil, err
			}
			if ahead > 0 {
				reasons = append(reasons, fmt.Sprintf("push されていない commit が %d 件残っている", ahead))
			}
			return reasons, nil
		}

		// upstream が無い側。base からの差分を見る。
		if req.Base == "" {
			reasons = append(reasons,
				"upstream が無く、worktree を作ったときの base も分からないので push 済みか判定できない")
			return reasons, nil
		}
		noDiff, err := gitNoDiffFromBase(ctx, req.WorktreePath, req.Base)
		if err != nil {
			reasons = append(reasons, fmt.Sprintf("base %s との差分を判定できない: %v", req.Base.String(), err))
			return reasons, nil
		}
		if !noDiff {
			reasons = append(reasons, fmt.Sprintf(
				"upstream が無いまま base %s との差分が残っている（push されていない成果がある）", req.Base.String()))
		}
	}

	return reasons, nil
}

// repoDirOf は worktree が属するリポジトリの作業ディレクトリを返す。
//
// `git rev-parse --git-common-dir` の結果が `<リポジトリ>/.git` なら親を返し、
// bare リポジトリのようにそれ以外なら共通ディレクトリそのものを返す。
// **worktree では `.git` はファイルである**ので、パスの組み立てで前提にしない。
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 対象の worktree のパス（まだ消していないこと）。
// 戻り値の1つ目: `git -C` に渡せるリポジトリのディレクトリ。
// 戻り値の2つ目: 共通ディレクトリを引けない場合のエラー。
func (m *Manager) repoDirOf(ctx context.Context, worktreePath string) (string, error) {
	commonDir, err := gitCommonDir(ctx, worktreePath)
	if err != nil {
		return "", err
	}
	if filepath.Base(commonDir) == ".git" {
		return filepath.Dir(commonDir), nil
	}
	return commonDir, nil
}

// removeWorktree は worktree の実体を消す（3-9 の手順3）。
//
// herdr.worktree.create_via_herdr が真なら herdr の worktree.remove を呼ぶ。
// **引数は path でも branch でもなく、身元ファイルに書いた herdr workspace の ID である**
// （実測）。**このあと workspace.close を呼んではならない**（応答に workspace が入り、
// workspace ごと閉じられる）。
// 偽なら `git worktree remove` を自分で叩く。
//
// force を真で渡す理由: 消してよいかの判定は手順2 と 2b で済ませてあるので、
// herdr 側の未コミット検査で二重に止められないようにする。
//
// ctx: 実行に適用するコンテキスト。
// identity: 身元ファイル（herdr workspace の ID を読む）。
// repoDir: リポジトリの作業ディレクトリ（herdr を使わないときに使う）。
// worktreePath: 消す worktree の絶対パス。
// 戻り値: 削除に失敗した場合・herdr を使う設定なのに ID やクライアントが無い場合のエラー。
func (m *Manager) removeWorktree(
	ctx context.Context,
	identity *Identity,
	repoDir, worktreePath string,
) error {
	if !m.cfg.Herdr.Worktree.CreateViaHerdr {
		if err := gitWorktreeRemove(ctx, repoDir, worktreePath); err != nil {
			return err
		}
		return nil
	}

	if m.herdr == nil {
		return fmt.Errorf("herdr.worktree.create_via_herdr が真ですが herdr のクライアントが設定されていません")
	}
	if identity.HerdrWorkspaceID == "" {
		return fmt.Errorf(
			"身元ファイルに herdr_workspace_id がありません（%s）。"+
				"herdr workspace として開いていない worktree は worktree.remove では消せません",
			m.IdentityPath(worktreePath))
	}
	if _, err := m.herdr.WorktreeRemove(ctx, herdr.WorktreeRemoveParams{
		WorkspaceID: identity.HerdrWorkspaceID,
		Force:       true,
	}); err != nil {
		return fmt.Errorf(
			"herdr の worktree.remove に失敗しました（workspace_id=%s）: %w", identity.HerdrWorkspaceID, err)
	}
	return nil
}
