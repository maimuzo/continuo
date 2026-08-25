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
	// BranchAbsent は、**身元ファイルに書かれた branch がリポジトリに実在しなかった**
	// ことを表す（issue #27）。
	//
	// **BranchDeleted と両立しない。**消す対象が無かっただけなので Leftovers にも積まない。
	// **「消しました」と書いてはならない。**消していないし、元から無かった。
	// 呼び出し側は「消す対象がありませんでした」と言うこと。
	BranchAbsent bool
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
	// Leftovers は**片付けたのに残ってしまったもの**である（人間が読む文。issue #23）。
	//
	// **Reasons とは別物である。**Reasons は「消さなかった」理由、Leftovers は
	// 「消したが、これだけ残った」ものである。branch・git の worktree の登録・
	// herdr の workspace が入る。
	//
	// **ログではなく、呼び出し側が人間の画面へ1行ずつ出すためにある。**
	// `continuo abandon` は Logger を渡さないので、**ログに書いても誰も読めない。**
	// 「ログを見てください」と書いて済ませると、**残ったものが人間に1文字も届かない。**
	//
	// **Removed が真でも空とは限らない。**worktree は消えたが branch と
	// herdr の workspace が残る、という結果はふつうに起こる。
	Leftovers []string
	// Notices は**片付けの途中で continuo が自分で行った、人間へ知らせるべきこと**である
	// （人間が読む文。issue #28）。
	//
	// **Leftovers とは別物である。**Leftovers は「消したが、これだけ残った」もの、
	// Notices は「頼まれていないが、continuo が自分でこれを行った」ものである。
	// 壊れた ref のファイルを1つ消したことが入る。
	//
	// **ログではなく、呼び出し側が人間の画面へ1行ずつ出すためにある。**
	// `continuo abandon` は Logger を渡さないので、**ログに書いても誰も読めない。**
	// **continuo が `.git` の中のファイルを1つ消したことを、人間が知る手立てを残す。**
	Notices []string
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
//	3b continuo が開かせたリポジトリの親 workspace を、条件を満たすときだけ閉じる
//	4  branch は herdr が消さないので git branch -D を自分で叩く
//
// **worktree の workspace に対して workspace.close を呼ばない。**worktree.remove の
// 応答に workspace が入り、workspace ごと閉じられる。
// **閉じるのはリポジトリの親 workspace だけである**（段3b。issue #19）。
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
	//
	// **検算できなくても片付けをあきらめない**（issue #23）。clone を人間が移した・
	// 消した環境では、リポジトリを名指しできない。**そのとき branch には触らない**
	// （どの branch を消してよいかを確かめる手立てが無い）が、
	// **worktree のディレクトリと herdr の workspace は、リポジトリを知らなくても消せる。**
	// できる分を消して、残したものを人間へ言う。
	//
	// **「調べられない」と「食い違っている」は別である。**後者は worktree の `.git` が
	// 書き換えられた痕跡であり、**消す相手を取り違えている可能性がある。**
	// そのときだけは1バイトも消さずに止まる（3-20 / 3-22）。
	repoDir := ""
	if _, verified, repoErr := m.verifiedRepo(ctx, resolvedPath); repoErr != nil {
		if errors.Is(repoErr, ErrRepoMismatch) {
			return nil, repoErr
		}
		m.logger.Warn("worktree が属するリポジトリを検算できないので、branch には触らずに worktree だけを片付けます",
			"worktree", resolvedPath, "error", repoErr)
	} else {
		repoDir = verified
	}

	// base は身元ファイルにも書いてある（3-18）。**再起動をまたぐと呼び出し側は
	// base を持っていない**ので、渡されなかったときはそこから補う。
	req.Base = m.effectiveBase(req.Base, identity)

	// **`--force` のときは見送りの判定を通さない。**通すと、判定に使う git を
	// 起動するだけ遅くなり、結果は使われない（下の分岐が Force で無効になる）。
	var reasons []string
	if !req.Force {
		reasons = m.leftoverReasons(ctx, req)
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
	branch, branchState, branchReason := m.deletableBranch(ctx, repoDir, resolvedPath, identity)

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
	if err := m.removeWorktree(ctx, result, workspaceID, repoDir, resolvedPath); err != nil {
		return nil, err
	}

	// 3b: continuo が開かせたリポジトリの親 workspace を閉じる（issue #19）。
	// **worktree.remove はこれを閉じない**ので、放っておくと issue 1件につき1つ溜まる。
	// **閉じてよい条件は internal/workspace/repoworkspace.go に書いてある。**
	m.closeRepoWorkspace(ctx, repoDir, identity)

	// 4: branch は herdr が消さないので自分で叩く。
	//
	// **消せなかったときは、そのことを人間の画面へ届ける**（issue #23）。
	// ここをログにだけ書くと、`continuo abandon` は Logger を渡さないので誰も読めない。
	switch {
	case branchState == branchAbsent:
		// **リポジトリに実在しない branch を「残っています」と言わない**（issue #27）。
		// 着手が `git worktree add` で失敗し続けた worktree には、ディレクトリだけが
		// 残って branch が1度も作られていないことがある。そこへ「消せませんでした」と
		// 出すと、**利用者は存在しないものを探して消しに行く。**
		// **`cleanup.delete_branch` が偽でも同じである。**消す対象が元から無い。
		result.BranchAbsent = true
	case !m.cfg.Cleanup.DeleteBranch:
		if identity.Branch != "" {
			result.Leftovers = append(result.Leftovers,
				i18n.T(i18n.KeyWorkspaceLeftoverBranchDisabled, identity.Branch))
		}
	case branchState == branchKeep:
		result.Leftovers = append(result.Leftovers,
			i18n.T(i18n.KeyWorkspaceLeftoverBranchUndeletable, identity.Branch, branchReason))
	default:
		if err := gitBranchDelete(ctx, repoDir, branch, m.brokenRefPolicyFor(result)); err != nil {
			m.logger.Warn("branch を消せませんでした", "branch", branch.String(), "error", err)
			result.Leftovers = append(result.Leftovers, i18n.T(
				i18n.KeyWorkspaceLeftoverBranchDeleteFailed, branch.String(), err, repoDir, branch.String()))
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
	// **消えていない branch を「片付けた」と書かない**（CleanupResult.BranchDeleted の規則）。
	// `cleanup.delete_branch` が偽のとき・branch の検算に落ちたとき・`git branch -D` が
	// 失敗したときは branch が残る。ログだけを見て「もう無い」と判断されると、
	// **残った branch を探す人がログを疑うところから始めることになる。**
	switch {
	case result.BranchDeleted:
		m.logger.Info("worktree と branch を片付けました",
			"worktree", resolvedPath, "branch", identity.Branch, "issue", identity.IssueIdentifier)
	case result.BranchAbsent:
		m.logger.Info("worktree を片付けました（branch は元からありませんでした）",
			"worktree", resolvedPath, "branch", identity.Branch, "issue", identity.IssueIdentifier)
	default:
		m.logger.Info("worktree を片付けました（branch は残しました）",
			"worktree", resolvedPath, "branch", identity.Branch, "issue", identity.IssueIdentifier)
	}
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
// **`worktree.open` が答えられなくても、そこで止まらない**（issue #23）。
// `worktree.open` は git を触る（worktree の `.git` が壊れていれば断られうる）ので、
// 断られたら `workspace.list` に「このパスを開いている workspace はどれか」を
// 答えさせ直す。**どちらも herdr 自身の答えなので、検算としての強さは変わらない。**
// **身元ファイルの値へは決して落とさない**（エージェントが書き換えられる）。
// **どちらでも答えが出なければ空文字を返す**（片付けは worktree の実体だけを消して続ける）。
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 封じ込め検査を通った worktree の絶対パス。
// repoDir: 検算済みのリポジトリの作業ディレクトリ（worktree.open の cwd に渡す）。
// identity: 読み取った身元ファイル。
// 戻り値の1つ目: 消してよい herdr workspace の ID。
// **herdr.worktree.create_via_herdr が偽なら空文字**（herdr を使わないので要らない）。
// **herdr が答えられなかった場合も空文字である。**
// 戻り値の2つ目: herdr のクライアントが無い場合・**herdr が別のパスを答えた場合**のエラー。
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
		m.logger.Warn("herdr の worktree.open が断ったので、workspace の一覧から探し直します",
			"worktree", worktreePath, "error", err)
		return m.fallbackWorkspaceID(ctx, worktreePath), nil
	}
	if opened.Worktree.Path != "" && !samePath(opened.Worktree.Path, worktreePath) {
		// **これは「答えられない」ではなく「別の相手を答えた」である。**
		// 落とさずに進むと、無関係の workspace を消すことになる。
		return "", i18n.Errorf(
			i18n.KeyWorkspaceResolveWorkspaceIDPathMismatch, opened.Worktree.Path, worktreePath)
	}
	workspaceID := opened.Workspace.WorkspaceID
	if workspaceID == "" {
		m.logger.Warn("herdr の worktree.open が workspace の ID を返さなかったので、workspace の一覧から探し直します",
			"worktree", worktreePath)
		return m.fallbackWorkspaceID(ctx, worktreePath), nil
	}
	if identity.HerdrWorkspaceID != "" && identity.HerdrWorkspaceID != workspaceID {
		m.logger.Warn("身元ファイルの herdr_workspace_id が herdr の現物と一致しないので、現物のほうを消します",
			"identity_path", m.IdentityPath(worktreePath),
			"identity_workspace_id", identity.HerdrWorkspaceID,
			"herdr_workspace_id", workspaceID)
	}
	return workspaceID, nil
}

// fallbackWorkspaceID は、`worktree.open` が答えられなかったときに `workspace.list` で
// 探し直し、結果をログに残す（3-9 の段3）。
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 封じ込め検査を通った worktree の絶対パス。
// 戻り値: 見つかった workspace の ID。**見つからない場合と問い合わせに失敗した場合は空文字**
// （そのときは worktree の実体だけを片付ける）。
func (m *Manager) fallbackWorkspaceID(ctx context.Context, worktreePath string) string {
	workspaceID, err := m.findWorkspaceIDByPath(ctx, worktreePath)
	if err != nil {
		m.logger.Warn("herdr の workspace の一覧を引けないので、herdr workspace は残ります（手で閉じてください）",
			"worktree", worktreePath, "error", err)
		return ""
	}
	if workspaceID == "" {
		m.logger.Warn("この worktree を開いている herdr workspace が一覧に無いので、worktree の実体だけを片付けます",
			"worktree", worktreePath)
	}
	return workspaceID
}

// findWorkspaceIDByPath は `workspace.list` に、そのパスを開いている workspace を答えさせる。
//
// **`worktree.open` の代わりに使う。**あちらは git を触るので、worktree の `.git` が
// 壊れていると断られうる。こちらは herdr が持っている一覧を読むだけである。
// **照合は checkout_path であり、身元ファイルの値は1つも使わない。**
//
// **ログは出さない。**「答えが無い」ことの意味は呼び出し側の文脈で変わる
// （消す前なら「実体だけ片付ける」、消したあとなら「もう閉じている」）。
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 封じ込め検査を通った worktree の絶対パス。
// 戻り値の1つ目: 見つかった workspace の ID。**見つからなければ空文字。**
// 戻り値の2つ目: 一覧を引けなかった場合のエラー。
func (m *Manager) findWorkspaceIDByPath(ctx context.Context, worktreePath string) (string, error) {
	list, err := m.herdr.WorkspaceList(ctx)
	if err != nil {
		return "", err
	}
	for _, ws := range list.Workspaces {
		if ws.Worktree == nil {
			continue
		}
		if samePath(ws.Worktree.CheckoutPath, worktreePath) {
			return ws.WorkspaceID, nil
		}
	}
	return "", nil
}

// branchVerdict は、身元ファイルに書かれた branch をどう扱うかの判定である（3-9 の段4）。
//
// **「消してよい／いけない」の2値では足りない**（issue #27）。**消す対象が
// リポジトリに実在しない**という3つ目があり、それを「消せなかった」に丸めると、
// 利用者は存在しない branch を探しに行く。
type branchVerdict int

const (
	// branchDeletable は `git branch -D` に渡してよいことを表す。
	branchDeletable branchVerdict = iota
	// branchAbsent は、その branch がリポジトリに実在しないことを表す。
	// **消す対象が無いだけなので、残ったものとして数えない。**
	branchAbsent
	// branchKeep は、実在するかもしれないのに消してよいと検算できないことを表す。
	// **残ったものとして人間の画面へ出す**（理由を添える）。
	branchKeep
)

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
// **その前に「そもそも実在するか」を見る**（issue #27）。`git worktree add` が失敗し続けた
// あとの worktree には、ディレクトリだけが残って branch が1度も作られていないことがある。
// **実在しないものを「消せませんでした」として人間に見せると、利用者は無いものを
// 探して消しに行く。**実在しなければ branchAbsent を返し、画面には何も出さない。
// **リポジトリを名指しできないときは実在するかを確かめられない**ので、そこは
// いままでどおり branchKeep のままにする（「無い」と「調べられない」は別である）。
//
// **現物はリポジトリ側に答えさせる**（`git -C <リポジトリ> worktree list --porcelain`）。
// worktree 側に `git -C <worktree> rev-parse HEAD` を聞くと、**worktree の `.git` が
// 壊れているだけで branch が消せなくなる**（issue #23）。しかも `.git` はエージェントが
// 書き換えられるファイルなので、**その答えは検算の根拠として弱い。**
// リポジトリのパスは ghq と置き場所のパスで検算済みであり（verifiedRepo）、
// `worktree list` は `.git` が壊れていてもその worktree の branch を答える（実測: 2026-08-25）。
//
// ctx: 実行に適用するコンテキスト。
// repoDir: 検算済みのリポジトリの作業ディレクトリ。
// worktreePath: 対象の worktree のパス（**まだ消していないこと。**登録を引くため）。
// identity: 読み取った身元ファイル。
// 戻り値の1つ目: 消してよい branch 名（**branchDeletable のときだけ入る**）。
// 戻り値の2つ目: 判定（branchDeletable / branchAbsent / branchKeep）。
// 戻り値の3つ目: 消さない理由（人間が読む文。**branchKeep のときだけ入る**）。
// **この文は CleanupResult.Leftovers を通って人間の画面へ出る。**ログにだけ書くと、
// `continuo abandon` は Logger を渡さないので誰にも届かない（issue #23）。
func (m *Manager) deletableBranch(
	ctx context.Context,
	repoDir, worktreePath string,
	identity *Identity,
) (normalize.SafeName, branchVerdict, string) {
	if identity.Branch == "" {
		return "", branchKeep, i18n.T(i18n.KeyWorkspaceLeftoverBranchReasonNoIdentity)
	}
	// **リポジトリを名指しできないなら branch には触らない。**空のまま git を呼ぶと
	// `-C` が付かず、**continuo 自身の作業ディレクトリのリポジトリに撃つ。**
	if repoDir == "" {
		m.logger.Warn("worktree が属するリポジトリを名指しできないので branch を消しません",
			"worktree", worktreePath, "branch", identity.Branch)
		return "", branchKeep, i18n.T(i18n.KeyWorkspaceLeftoverBranchReasonRepoUnknown)
	}

	branch, warnings := normalize.Normalize(identity.Branch)
	m.logWarnings(warnings)
	if branch.String() != identity.Branch {
		m.logger.Warn("身元ファイルの branch 名が正規化で変わるので消しません",
			"identity_path", m.IdentityPath(worktreePath),
			"branch", identity.Branch, "normalized", branch.String())
		return "", branchKeep, i18n.T(i18n.KeyWorkspaceLeftoverBranchReasonNormalized, branch.String())
	}

	prefix := BranchPrefix(m.cfg.Herdr.Worktree.BranchTemplate)
	if prefix == "" {
		m.logger.Warn("herdr.worktree.branch_template に変数が無いので branch を消しません",
			"branch_template", m.cfg.Herdr.Worktree.BranchTemplate, "branch", branch.String())
		return "", branchKeep, i18n.T(
			i18n.KeyWorkspaceLeftoverBranchReasonNoPrefix, m.cfg.Herdr.Worktree.BranchTemplate)
	}
	if !strings.HasPrefix(branch.String(), prefix) {
		m.logger.Warn("身元ファイルの branch が continuo の接頭辞で始まらないので消しません",
			"identity_path", m.IdentityPath(worktreePath),
			"branch", branch.String(), "prefix", prefix)
		return "", branchKeep, i18n.T(i18n.KeyWorkspaceLeftoverBranchReasonPrefixMismatch, prefix)
	}

	// **消す対象が実在するかを、現物との突き合わせより先に見る**（issue #27）。
	// 実在しなければ、このあとの検算が通ろうと落ちようと消すものは無い。
	// **git が答えられなければ「無い」とは言わない。**そのまま下の検算へ進み、
	// 消せない理由を人間へ出す（「無い」と「調べられない」は別である）。
	switch exists, err := gitBranchExists(ctx, repoDir, branch); {
	case err != nil:
		m.logger.Warn("branch がリポジトリに実在するかを確かめられませんでした（検算は続けます）",
			"repo", repoDir, "branch", branch.String(), "error", err)
	case !exists:
		// **「無い」と「ref が壊れていて git が答えを出せない」を混ぜない**（issue #28）。
		// `git show-ref --verify --quiet` は**壊れた ref にも終了コード 1 を返す**
		// （実測: 2026-08-25、git 2.50.1）。**そこを branchAbsent に丸めると、
		// 壊れた ref のファイルが誰にも消されないまま残る。**
		if m.brokenRefBranchAt(ctx, repoDir, worktreePath, branch) {
			return branch, branchDeletable, ""
		}
		m.logger.Info("身元ファイルに書かれた branch はリポジトリに実在しないので、消す対象がありません",
			"repo", repoDir, "branch", branch.String(), "worktree", worktreePath)
		return "", branchAbsent, ""
	}

	head, detached, err := gitWorktreeHeadAt(ctx, repoDir, worktreePath)
	if err != nil {
		m.logger.Warn("worktree がチェックアウトしている branch を引けないので branch を消しません",
			"repo", repoDir, "worktree", worktreePath, "error", err)
		return "", branchKeep, i18n.T(i18n.KeyWorkspaceLeftoverBranchReasonHeadUnreadable, err)
	}
	if head == "" && !detached {
		// **git が branch を1つも答えず、detached でもないのは、その ref が壊れているときである**
		// （実測: 2026-08-25。`worktree list --porcelain` は
		// `HEAD 0000000000000000000000000000000000000000` の行だけを出し、
		// `branch` の行も `detached` の行も出さない）。
		//
		// **detached HEAD を混ぜてはならない。**rebase 中・`git checkout <SHA>` のあと・
		// `git bisect` 中の worktree でも branch 名は空になる。**そこまでこの分岐に入れると、
		// 身元ファイルの branch が git の現物と1度も突き合わされないまま削除の対象になる。**
		// detached は `detached` の行で見分けられるので、その場合は下の一致検査で落とす。
		if m.brokenRefBranchAt(ctx, repoDir, worktreePath, branch) {
			return branch, branchDeletable, ""
		}
	}
	if head != branch.String() {
		m.logger.Warn("身元ファイルの branch が worktree の現物と一致しないので消しません",
			"identity_path", m.IdentityPath(worktreePath),
			"identity_branch", branch.String(), "checked_out", head)
		return "", branchKeep, i18n.T(i18n.KeyWorkspaceLeftoverBranchReasonHeadMismatch, head)
	}

	return branch, branchDeletable, ""
}

// brokenRefBranchAt は、その worktree の branch の ref が壊れていて、continuo が
// ref のファイルとして片付けてよいかを返す（設計 3-22b）。
//
// **git の現物との突き合わせを、別の手立てで1本入れる。**ref が壊れていると
// `worktree list --porcelain` は branch を答えないので、身元ファイルの値だけが残る。
// そこで **`<共通ディレクトリ>/worktrees/<名前>/HEAD` の symref を直接読み**、
// その worktree が本当にその branch を指していることを確かめる。
// **HEAD のファイルは symref の1行なので、ref が壊れていても読める。**
//
// ctx: 実行に適用するコンテキスト。
// repoDir: 検算済みのリポジトリの作業ディレクトリ。
// worktreePath: 対象の worktree のパス。
// branch: 身元ファイルの branch 名（接頭辞と正規化の検査は通っていること）。
// 戻り値: ref のファイルとして片付けてよければ true。
func (m *Manager) brokenRefBranchAt(
	ctx context.Context,
	repoDir, worktreePath string,
	branch normalize.SafeName,
) bool {
	commonDir, err := gitCommonDir(ctx, repoDir)
	if err != nil {
		m.logger.Warn("共通ディレクトリを引けないので壊れた ref として扱いません",
			"repo", repoDir, "worktree", worktreePath, "branch", branch.String(), "error", err)
		return false
	}
	heads, err := worktreeHeadRefs(commonDir)
	if err != nil {
		m.logger.Warn("worktree の HEAD を読めないので壊れた ref として扱いません",
			"repo", repoDir, "worktree", worktreePath, "branch", branch.String(), "error", err)
		return false
	}
	found := ""
	for path, name := range heads {
		if samePath(path, worktreePath) {
			found = name
			break
		}
	}
	if found != branch.String() {
		m.logger.Warn("worktree の HEAD が身元ファイルの branch を指していないので壊れた ref として扱いません",
			"repo", repoDir, "worktree", worktreePath,
			"identity_branch", branch.String(), "head_ref", found)
		return false
	}
	target, brokenErr := brokenBranchRef(ctx, repoDir, branch, m.brokenRefPolicy())
	if brokenErr != nil || target == nil {
		return false
	}
	m.logger.Warn("branch の ref が壊れているので、ref のファイルとして片付けます",
		"repo", repoDir, "worktree", worktreePath,
		"branch", branch.String(), "ref_path", target.path, "sha", target.tip)
	return true
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
// **git が答えられないことをエラーにしない**（issue #23）。worktree の `.git` が
// 壊れていると `git -C <worktree> …` が1つも通らないが、それは呼び出し側が扱えない
// エラーではなく「調べられないので消さない」という**見送りの理由**である。
// エラーにすると、`continuo abandon` が壊れた worktree の中身を1行も見せられなくなる。
//
// ctx: 実行に適用するコンテキスト。
// req: 対象の worktree と base。
// 戻り値: 見送る理由（空なら消してよい）。
func (m *Manager) leftoverReasons(ctx context.Context, req CleanupRequest) []string {
	var reasons []string

	if m.cfg.Cleanup.RequireCleanWorktree {
		// **continuo 自身が置いた身元ファイルとその一時ファイルは数から外す**（3-18）。
		// 外さないと、`info/exclude` への登録に失敗した worktree が永久に片付かず、
		// しかも issue へ「コミットされていない変更が残っている」という誤った理由が投稿される。
		// **打ち切りの有無は見ない。**この判定は「空かどうか」しか見ないので、
		// 打ち切られるほど出ているなら、それは空ではない。
		status, _, err := gitStatusPorcelain(ctx, req.WorktreePath, m.identityStatusExcludes()...)
		switch {
		case err != nil:
			reasons = append(reasons, i18n.T(i18n.KeyWorkspaceUndeterminedDirty, err))
		case status != "":
			reasons = append(reasons, "コミットされていない変更が残っている（未追跡のファイルを含む）")
		}
	}

	if m.cfg.Cleanup.RequirePushed {
		hasUpstream, err := gitHasUpstream(ctx, req.WorktreePath)
		if err != nil {
			return append(reasons, i18n.T(i18n.KeyWorkspaceUndeterminedUnpushed, err))
		}
		if hasUpstream {
			ahead, err := gitAheadOfUpstream(ctx, req.WorktreePath)
			if err != nil {
				return append(reasons, i18n.T(i18n.KeyWorkspaceUndeterminedUnpushed, err))
			}
			if ahead > 0 {
				reasons = append(reasons, fmt.Sprintf("push されていない commit が %d 件残っている", ahead))
			}
			return reasons
		}

		// upstream が無い側。base からの差分を見る。
		if req.Base == "" {
			reasons = append(reasons,
				"push されていないか、worktree を作ったときの base を確かめられないので消せない"+
					"（エージェントに push させると片付けられる）")
			return reasons
		}
		noDiff, err := gitNoDiffFromBase(ctx, req.WorktreePath, req.Base)
		if err != nil {
			reasons = append(reasons, fmt.Sprintf("base %s との差分を判定できない: %v", req.Base.String(), err))
			return reasons
		}
		if !noDiff {
			reasons = append(reasons, fmt.Sprintf(
				"upstream が無いまま base %s との差分が残っている（push されていない成果がある）", req.Base.String()))
		}
	}

	return reasons
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
// **消えたことを必ず自分で確かめる**（3-9 の段3。issue #23）。herdr も git も
// 「消した」と答えたのに実体が残ることがある。worktree の `.git` が壊れていると
// `git worktree remove` は `validation failed, cannot remove working tree` で断り
// （実測: 2026-08-25）、herdr の `worktree.remove` はその内側で同じことをする。
// **答えだけを信じると、「消しました」と表示して残す。**
//
// **残っていたら自分の手で片付ける**（removeWorktreeByHand）。
// **そこで諦めると、まさに片付けたい worktree だけが永久に残る。**
//
// ctx: 実行に適用するコンテキスト。
// result: 残ったものを積む先（Leftovers）。
// workspaceID: 検算済みの herdr workspace の ID
// （herdr を使わない設定のとき、および herdr が答えられなかったときは空文字）。
// repoDir: 検算済みのリポジトリの作業ディレクトリ。
// worktreePath: 消す worktree の絶対パス（封じ込め検査を通っていること）。
// 戻り値: 実体を消せなかった場合・herdr を使う設定なのにクライアントが無い場合のエラー。
func (m *Manager) removeWorktree(
	ctx context.Context,
	result *CleanupResult,
	workspaceID string,
	repoDir, worktreePath string,
) error {
	if m.cfg.Herdr.Worktree.CreateViaHerdr && m.herdr == nil {
		return fmt.Errorf("herdr.worktree.create_via_herdr が真ですが herdr のクライアントが設定されていません")
	}

	refused := m.requestWorktreeRemoval(ctx, workspaceID, repoDir, worktreePath)
	gone, statErr := worktreeGone(worktreePath)
	if refused == nil && gone {
		return nil
	}
	if refused == nil {
		// **断られていないのに残っている。**答えと現物が食い違っているので、その旨を残す。
		refused = i18n.Errorf(i18n.KeyWorkspaceRemoveWorktreeStillThere, worktreePath, statErr)
	}
	if err := m.removeWorktreeByHand(ctx, result, repoDir, worktreePath, refused); err != nil {
		return err
	}
	// **herdr workspace が残っていれば閉じる。**閉じ残すと、中身の消えた workspace が
	// herdr の画面に残り続ける。**閉じる宛先は herdr 自身に答えさせる**（身元ファイルは使わない）。
	m.closeWorktreeWorkspace(ctx, result, worktreePath)
	return nil
}

// requestWorktreeRemoval は、設定された経路に worktree の削除を要求する（3-9 の段3）。
//
// **「要求した」だけで、消えたかどうかは見ない。**消えたかを確かめるのは removeWorktree である。
//
// ctx: 実行に適用するコンテキスト。
// workspaceID: 検算済みの herdr workspace の ID（空なら herdr へは要求できない）。
// repoDir: 検算済みのリポジトリの作業ディレクトリ。
// worktreePath: 消す worktree の絶対パス。
// 戻り値: 要求が断られた理由。断られなければ nil。
func (m *Manager) requestWorktreeRemoval(
	ctx context.Context,
	workspaceID string,
	repoDir, worktreePath string,
) error {
	if !m.cfg.Herdr.Worktree.CreateViaHerdr {
		if repoDir == "" {
			// **リポジトリを名指しできない。**空のまま git を呼ぶと `-C` が付かず、
			// continuo 自身の作業ディレクトリのリポジトリに撃つ。実体は自分で消せる。
			return i18n.Errorf(i18n.KeyWorkspaceRemoveWorktreeRepoUnknown, worktreePath)
		}
		return gitWorktreeRemove(ctx, repoDir, worktreePath)
	}
	if workspaceID == "" {
		// **herdr がこの worktree の workspace を答えられなかった。**
		// 消す宛先が分からないだけであり、実体と git の登録は自分で片付けられる。
		return i18n.Errorf(i18n.KeyWorkspaceRemoveWorktreeWorkspaceIDUnknown, worktreePath)
	}
	if _, err := m.herdr.WorktreeRemove(ctx, herdr.WorktreeRemoveParams{
		WorkspaceID: workspaceID,
		Force:       true,
	}); err != nil {
		return i18n.Errorf(i18n.KeyWorkspaceRemoveWorktreeWorktreeRemoveFailed, workspaceID, err)
	}
	return nil
}

// worktreeGone は worktree の実体がもう無いかを返す。
//
// **シンボリックリンクを追わない**（Lstat）。追うと、リンクの先が消えただけで
// 「消えた」と答えてしまう。
//
// worktreePath: 調べる worktree の絶対パス。
// 戻り値の1つ目: 無ければ true。**調べられなかった場合は false**（残っている側に倒す）。
// 戻り値の2つ目: 調べられなかった理由（無ければ nil）。
func worktreeGone(worktreePath string) (bool, error) {
	if _, err := os.Lstat(worktreePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

// removeWorktreeByHand は git にも herdr にも頼らずに worktree を片付ける（issue #23）。
//
// **`continuo abandon` は壊れた状態を片付ける道具である。**worktree の `.git` が壊れて
// いれば `git worktree remove` は断り、herdr の `worktree.remove` も同じ理由で断りうる。
// **断られたまま終わると、利用者には手が無くなる。**そこで実体を消し、
// `git worktree prune` でリポジトリ側の登録を落とす。
//
// **消してよいかの判定は、ここへ来る前に済んでいる**（手順2 と 2b、または `--force`）。
// **パスは封じ込め検査（3-20）を通っている**ので、置き場所の外は1バイトも消さない。
//
// ctx: 実行に適用するコンテキスト。
// result: 残ったものを積む先（Leftovers）。
// repoDir: 検算済みのリポジトリの作業ディレクトリ。
// worktreePath: 消す worktree の絶対パス。
// cause: 通常の経路が断った理由（ログとエラー文に残す）。
// 戻り値: 実体を消せなかった場合のエラー。**登録の掃除に失敗しても止めない**
// （実体はもう無いので、次の `git worktree prune` で落ちる）。
func (m *Manager) removeWorktreeByHand(
	ctx context.Context,
	result *CleanupResult,
	repoDir, worktreePath string,
	cause error,
) error {
	m.logger.Warn("通常の経路で worktree を消せなかったので、実体を消して git の登録を掃除します",
		"worktree", worktreePath, "repo", repoDir, "error", cause)
	if err := os.RemoveAll(worktreePath); err != nil {
		return i18n.Errorf(i18n.KeyWorkspaceRemoveWorktreeByHandFailed, worktreePath, cause, err)
	}
	if repoDir == "" {
		// **リポジトリを名指しできないので登録は掃除できない。**残るのは
		// `git worktree list` の1行だけであり、次に `git worktree prune` が走れば落ちる。
		m.logger.Warn("worktree が属するリポジトリを名指しできないので、git の worktree の登録は残ります",
			"worktree", worktreePath)
		result.Leftovers = append(result.Leftovers,
			i18n.T(i18n.KeyWorkspaceLeftoverPruneRepoUnknown, worktreePath))
		return nil
	}
	// **prune を撃つ前に、実体の無い登録がほかにも無いかを見る**（3-37-9b）。
	// `git worktree prune` はリポジトリ全体に効くので、**利用者がディレクトリごと移した
	// worktree の登録も一緒に落とす。**落とされた側の branch は git に守られなくなり、
	// **あとの `git branch -D` が通ってしまう。**
	//
	// **ここで撃ってよいのは、continuo が自分で消した1件だけが対象のときである。**
	// ほかにもあるなら撃たず、登録が残ったことを人間へ出す（消えるものを決めるのは利用者である）。
	stale, staleErr := gitStaleWorktreesExcept(ctx, repoDir, worktreePath)
	if staleErr != nil {
		m.logger.Warn("worktree の登録の一覧を引けないので prune は撃ちません（実体は消してあります）",
			"repo", repoDir, "worktree", worktreePath, "error", staleErr)
		result.Leftovers = append(result.Leftovers,
			i18n.T(i18n.KeyWorkspaceLeftoverPruneFailed, staleErr, repoDir))
		return nil
	}
	if len(stale) > 0 {
		m.logger.Warn("実体の無い worktree の登録がほかにもあるので prune は撃ちません",
			"repo", repoDir, "worktree", worktreePath, "others", strings.Join(stale, ", "))
		result.Leftovers = append(result.Leftovers,
			i18n.T(i18n.KeyWorkspaceLeftoverPruneSkipped, strings.Join(stale, ", "), repoDir))
		return nil
	}
	if err := gitWorktreePrune(ctx, repoDir); err != nil {
		m.logger.Warn("worktree の登録を掃除できませんでした（実体は消してあります）",
			"repo", repoDir, "worktree", worktreePath, "error", err)
		result.Leftovers = append(result.Leftovers,
			i18n.T(i18n.KeyWorkspaceLeftoverPruneFailed, err, repoDir))
	}
	return nil
}

// closeWorktreeWorkspace は、実体を自分で消した worktree の herdr workspace を閉じる。
//
// **`worktree.remove` が届かなかったときの後始末である。**閉じ残すと、中身の消えた
// workspace が herdr の画面に残る。**閉じる宛先は herdr 自身に答えさせる**
// （`workspace.list` の checkout_path で照合する。身元ファイルの値は1つも使わない）。
// **もう残っていなければ何もしない**（`worktree.remove` が閉じ切っていた場合）。
//
// **失敗してもエラーにしない。**worktree の実体はもう消えており、ここで止めると
// 「消えたのに Cleanup は失敗を返す」という、呼び出し側が扱えない結果になる。
//
// ctx: 実行に適用するコンテキスト。
// result: 残ったものを積む先（Leftovers）。
// worktreePath: 消した worktree の絶対パス。
func (m *Manager) closeWorktreeWorkspace(ctx context.Context, result *CleanupResult, worktreePath string) {
	if !m.cfg.Herdr.Worktree.CreateViaHerdr || m.herdr == nil {
		return
	}
	workspaceID, err := m.findWorkspaceIDByPath(ctx, worktreePath)
	if err != nil {
		m.logger.Warn("herdr の workspace の一覧を引けないので、herdr workspace は残ります（手で閉じてください）",
			"worktree", worktreePath, "error", err)
		result.Leftovers = append(result.Leftovers,
			i18n.T(i18n.KeyWorkspaceLeftoverWorkspaceListFailed, err))
		return
	}
	if workspaceID == "" {
		return
	}
	if _, err := m.herdr.WorkspaceClose(ctx, herdr.WorkspaceCloseParams{WorkspaceID: workspaceID}); err != nil {
		m.logger.Warn("worktree の herdr workspace を閉じられませんでした（手で閉じてください）",
			"worktree", worktreePath, "workspace_id", workspaceID, "error", err)
		result.Leftovers = append(result.Leftovers, i18n.T(
			i18n.KeyWorkspaceLeftoverWorkspaceCloseFailed, workspaceID, err, workspaceID))
		return
	}
	m.logger.Info("worktree の herdr workspace を閉じました",
		"worktree", worktreePath, "workspace_id", workspaceID)
}
