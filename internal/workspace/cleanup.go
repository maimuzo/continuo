package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/normalize"
)

// CleanupResult は片付けを試みた結果である（3-9）。
type CleanupResult struct {
	// Removed は worktree を消したかどうかである。
	Removed bool
	// BranchDeleted は branch も消したかどうかである。
	//
	// **Removed が真でも偽になりうる。**cleanup.delete_branch が偽のとき、
	// branch の検算（deletableBranch）に落ちたとき、`git branch -D` が失敗したときである。
	// **呼び出し側は「worktree を消した＝branch も消えた」と書いてはならない**
	// （`continuo abandon` が人間に見せる1行がこれを読む）。
	BranchDeleted bool
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
	// Force は「人間が明示的に消せと言った」ことを表す（`continuo abandon --force`）。
	//
	// **真のとき、cleanup.enabled と、未コミット・未 push による見送りを飛ばす。**
	// 飛ばしてよいのは、`continuo abandon` が消す前に Inspect の結果を人間へ見せ、
	// **その人間が `--force` を付け直したときだけ**である。
	//
	// **巡回（orchestrator）からは渡さない。**渡すと、cleanup.enabled を偽にしてある
	// 環境で worktree が黙って消える。**ゼロ値は偽なので、既存の呼び出しは何も変わらない。**
	//
	// **飛ばさないものもある。**封じ込め検査（3-20）・身元ファイルの読み取り・
	// branch と herdr workspace の検算は、Force が真でもそのまま通す。
	// あれは「消してよいか」ではなく「正しい対象を消しているか」の検査である。
	Force bool
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
// **`req.Force` が真なら、手順2 と 2b と cleanup.enabled を飛ばす**
// （`continuo abandon --force` だけが渡す。CleanupRequest.Force を見よ）。
// **封じ込め検査と、branch・herdr workspace の検算は飛ばさない。**
//
// ctx: 実行に適用するコンテキスト。
// req: 片付ける worktree と、その worktree を作ったときの base。
// 戻り値の1つ目: 片付けた／見送った結果。
// 戻り値の2つ目: 封じ込め検査に落ちた・身元ファイルを読めない・
// worktree の削除に失敗した場合のエラー。**封じ込め検査に落ちた場合は何も消さない**
// （3-20。「消す直前」がいちばん危ない検査点である）。
func (m *Manager) Cleanup(ctx context.Context, req CleanupRequest) (*CleanupResult, error) {
	result := &CleanupResult{}

	if !m.cfg.Cleanup.Enabled && !req.Force {
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
	// **以後は検査を通ったパスだけを使う。**検査したパスと操作したパスが違うと、
	// 検査と操作の間でパスが差し替わったときに、検査の保証がそのまま切れる。
	req.WorktreePath = resolvedPath

	identity, err := m.ReadIdentity(resolvedPath)
	if err != nil {
		return nil, err
	}
	result.Identity = identity

	// worktree を消したあとでは共通ディレクトリを引けないので、先に引いておく。
	// **どのリポジトリかは検算する**（worktree の .git はエージェントが書き換えられる）。
	_, repoDir, err := m.verifiedRepo(ctx, resolvedPath)
	if err != nil {
		return nil, err
	}

	// base は身元ファイルにも書いてある（3-18）。**再起動をまたぐと呼び出し側は
	// base を持っていない**ので、渡されなかったときはそこから補う。
	req.Base = m.effectiveBase(req.Base, identity)

	// **`--force` のときは見送りの判定を通さない。**通すと、判定に使う git を
	// 起動するだけ遅くなり、結果は使われない（下の分岐が Force で無効になる）。
	var reasons []string
	if !req.Force {
		reasons, err = m.leftoverReasons(ctx, req)
		if err != nil {
			return nil, err
		}
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
	branch, branchDeletable := m.deletableBranch(ctx, resolvedPath, identity)

	// 段3 の準備: 消す宛先の herdr workspace も、**worktree がまだ開いているうちに**
	// herdr に答えさせて検算する（resolveWorkspaceID を見よ）。
	workspaceID, err := m.resolveWorkspaceID(ctx, resolvedPath, repoDir, identity)
	if err != nil {
		return nil, err
	}

	// 2d: 失敗しても記録して続ける（片付けを止めない）。
	if err := m.RunHook(ctx, HookBeforeRemove, resolvedPath); err != nil {
		m.logger.Warn("workspace_hooks.before_remove が失敗しました（片付けは続けます）",
			"worktree", resolvedPath, "error", err)
	}

	// 3: worktree を消す。
	if err := m.removeWorktree(ctx, workspaceID, repoDir, resolvedPath); err != nil {
		return nil, err
	}

	// 4: branch は herdr が消さないので自分で叩く。
	if m.cfg.Cleanup.DeleteBranch && branchDeletable {
		if err := gitBranchDelete(ctx, repoDir, branch); err != nil {
			m.logger.Warn("branch を消せませんでした", "branch", branch.String(), "error", err)
		} else {
			result.BranchDeleted = true
		}
	}

	// issue ごとの設定ファイル（3-12）も一緒に消す。
	m.removeSettingsFile(identity.SettingsPath)

	// **after_run の印も落とす。**worktree はもう無いので、この印は二度と使われない。
	// 常駐プロセスなので、残すとプロセスの寿命のあいだ単調に増える
	// （BeginRun は印を消すだけなので、そのまま使える）。
	m.BeginRun(resolvedPath)

	result.Removed = true
	m.logger.Info("worktree と branch を片付けました",
		"worktree", resolvedPath, "branch", identity.Branch, "issue", identity.IssueIdentifier)
	return result, nil
}

// effectiveBase は片付けの手順2b で使う base を決める（3-9）。
//
// **呼び出し側が渡した値を優先し、無ければ身元ファイルの base を使う。**
// 巡回の手順7（3-9）と起動時の掃除は、再起動をまたぐと base を手元に持っていない。
// そこを補わないと、commit したが push していない worktree が
// 「base が分からないので判定できない」という、人間には原因の分からない理由で
// 永久に見送られる。
//
// **身元ファイルの値は正規化を通す。**そこはエージェントが書き換えられる場所なので、
// 正規化で変わる値（情報が落ちる値）は continuo が書いたものではないとみなして使わない。
//
// requested: 呼び出し側が渡した base（空のこともある）。
// identity: 読み取った身元ファイル。
// 戻り値: 手順2b に渡す base。決められなければ空文字。
func (m *Manager) effectiveBase(requested normalize.SafeName, identity *Identity) normalize.SafeName {
	if requested != "" || identity == nil || identity.Base == "" {
		return requested
	}
	base, warnings := normalize.Normalize(identity.Base)
	m.logWarnings(warnings)
	if base.String() != identity.Base {
		m.logger.Warn("身元ファイルの base が正規化で変わるので使いません",
			"identity_base", identity.Base, "normalized", base.String())
		return ""
	}
	return base
}

// resolveWorkspaceID は worktree.remove に渡す herdr workspace の ID を確定する（3-9 の段3）。
//
// **なぜ検算が要るか。**身元ファイルは worktree の直下にあり、その worktree では
// エージェントが `--permission-mode dontAsk` で動く（3-16 の段9）。
// **つまり herdr_workspace_id はエージェントが書き換えられる。**検算せずに
// `worktree.remove`（force）へ渡すと、**同じ機械で動いている別の run の worktree を
// 消させられる。**封じ込め検査（3-20）も未コミットの検査（3-9 の手順2）も、
// 消される側の worktree に対しては1つも走らない。
//
// **そこで herdr に現物を答えさせる。**`worktree.open` は既に開いていればその workspace を
// 返す（`already_open`）ので、**「このパスを開いている workspace はどれか」を herdr 自身に
// 言わせられる。**返ってきた ID だけを消す宛先にする。身元ファイルの値と食い違ったら、
// 現物のほうを採り、食い違ったことを警告としてログに残す。
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 封じ込め検査を通った worktree の絶対パス。
// repoDir: 検算済みのリポジトリの作業ディレクトリ（worktree.open の cwd に渡す）。
// identity: 読み取った身元ファイル。
// 戻り値の1つ目: 消してよい herdr workspace の ID。
// **herdr.worktree.create_via_herdr が偽なら空文字**（herdr を使わないので要らない）。
// 戻り値の2つ目: herdr のクライアントが無い場合・`worktree.open` に失敗した場合・
// **herdr が別のパスを答えた場合**のエラー。
func (m *Manager) resolveWorkspaceID(
	ctx context.Context,
	worktreePath, repoDir string,
	identity *Identity,
) (string, error) {
	if !m.cfg.Herdr.Worktree.CreateViaHerdr {
		return "", nil
	}
	if m.herdr == nil {
		return "", fmt.Errorf("herdr.worktree.create_via_herdr が真ですが herdr のクライアントが設定されていません")
	}

	focus := false
	opened, err := m.herdr.WorktreeOpen(ctx, herdr.WorktreeOpenParams{
		Path:  worktreePath,
		Cwd:   repoDir,
		Focus: &focus,
	})
	if err != nil {
		return "", i18n.Errorf(
			i18n.KeyWorkspaceResolveWorkspaceIDWorktreeOpenFailed, worktreePath, err)
	}
	if opened.Worktree.Path != "" && !samePath(opened.Worktree.Path, worktreePath) {
		return "", i18n.Errorf(
			i18n.KeyWorkspaceResolveWorkspaceIDPathMismatch, opened.Worktree.Path, worktreePath)
	}
	workspaceID := opened.Workspace.WorkspaceID
	if workspaceID == "" {
		return "", i18n.Errorf(
			i18n.KeyWorkspaceResolveWorkspaceIDWorkspaceIDMissing, worktreePath)
	}
	if identity.HerdrWorkspaceID != "" && identity.HerdrWorkspaceID != workspaceID {
		m.logger.Warn("身元ファイルの herdr_workspace_id が herdr の現物と一致しないので、現物のほうを消します",
			"identity_path", m.IdentityPath(worktreePath),
			"identity_workspace_id", identity.HerdrWorkspaceID,
			"herdr_workspace_id", workspaceID)
	}
	return workspaceID, nil
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
	if err := checkUnder(m.settingsRoot, settingsPath, i18n.T(i18n.KeyWorkspaceLabelIssueSettingsFile)); err != nil {
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
		// **continuo 自身が置いた身元ファイルとその一時ファイルは数から外す**（3-18）。
		// 外さないと、`info/exclude` への登録に失敗した worktree が永久に片付かず、
		// しかも issue へ「コミットされていない変更が残っている」という誤った理由が投稿される。
		status, err := gitStatusPorcelain(ctx, req.WorktreePath, m.identityStatusExcludes()...)
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
				"push されていないか、worktree を作ったときの base を確かめられないので消せない"+
					"（エージェントに push させると片付けられる）")
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

// identityStatusExcludes は `git status --porcelain` の数から外す名前を返す。
//
// **`info/exclude` へ登録する行と同じものを、先頭のスラッシュを外して返す。**
// 登録が失敗していても片付けが成立するように、判定の側でも外す（3-9 の手順2）。
//
// 戻り値: worktree の直下の、数から外すファイル名。
func (m *Manager) identityStatusExcludes() []string {
	lines := m.excludeLines()
	names := make([]string, 0, len(lines))
	for _, line := range lines {
		names = append(names, strings.TrimPrefix(line, "/"))
	}
	return names
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
// **渡す workspace の ID は resolveWorkspaceID が herdr に答えさせたものである。**
// 身元ファイルの値をそのまま渡してはならない（エージェントが書き換えられる）。
//
// ctx: 実行に適用するコンテキスト。
// workspaceID: 検算済みの herdr workspace の ID（herdr を使わない設定なら空文字）。
// repoDir: 検算済みのリポジトリの作業ディレクトリ（herdr を使わないときに使う）。
// worktreePath: 消す worktree の絶対パス。
// 戻り値: 削除に失敗した場合・herdr を使う設定なのに ID やクライアントが無い場合のエラー。
func (m *Manager) removeWorktree(
	ctx context.Context,
	workspaceID string,
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
	if workspaceID == "" {
		return i18n.Errorf(
			i18n.KeyWorkspaceRemoveWorktreeWorkspaceIDUnknown, worktreePath)
	}
	if _, err := m.herdr.WorktreeRemove(ctx, herdr.WorktreeRemoveParams{
		WorkspaceID: workspaceID,
		Force:       true,
	}); err != nil {
		return i18n.Errorf(
			i18n.KeyWorkspaceRemoveWorktreeWorktreeRemoveFailed, workspaceID, err)
	}
	return nil
}
