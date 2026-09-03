package workspace

import (
	"context"
	"errors"
	"strings"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/normalize"
)

// ErrBranchUsedByWorktree は、`git branch -D` が「worktree が使っている」と断ったことを表す
// （3-37-9b）。
//
// **呼び出し側はこれを見て、案内の文を切り替える。**「手で `git branch -D` を叩いてください」
// という一般の案内は、この場合**同じ理由で断られる**ので誤りになる。
// 叩くべきは `git worktree prune` であり、それを叩くかどうかは利用者が決める。
var ErrBranchUsedByWorktree = errors.New("git がこの branch を消しませんでした")

// IssueBranch は、issue 1件に対応する branch の現況である（issue #27）。
//
// **worktree が1つも無いときに使う。**片付けの途中で失敗して branch だけが残ると、
// worktree を起点に探す `continuo abandon` は「この issue の worktree はありません」で
// 終わってしまい、**利用者は手で消すしかなくなる。**
type IssueBranch struct {
	// Name は `herdr.worktree.branch_template` を変数展開して正規化した branch 名である。
	Name normalize.SafeName
	// RepoDir は `ghq list -p -e <owner>/<repo>` が答えた clone の作業ディレクトリである。
	RepoDir string
	// Exists は、その branch が RepoDir に実在するかどうかである。
	Exists bool
	// Tip は実在するときの commit の SHA である（**引けなければ空文字**）。
	//
	// **消す前に控えて人間へ見せるためにある。**削除は `git branch -D`
	// （マージ状態を見ない強制削除）なので、`git branch <名前> <SHA>` で戻せる
	// 手掛かりを渡しておかないと、reflog を掘るしかなくなる。
	Tip string
	// Unpushed は、どの remote にも載っていない commit の数である（3-37-9）。
	//
	// **`--force` を求める前に人間へ見せる。**worktree が無くても
	// `git rev-list --count <branch> --not --remotes` は答えるので、
	// **「調べる手立てが無い」は成り立たない。**
	Unpushed int
	// UnpushedErr は未 push の commit を数えられなかった理由である（数えられたら nil）。
	//
	// **数えられなかったことを黙って 0 として見せてはならない。**「0 件」と
	// 「調べられなかった」は別である。
	UnpushedErr error
}

// FindIssueBranch は、issue に対応する branch が clone に残っていないかを調べる（issue #27）。
//
// **身元ファイルを1バイトも読まない。**読む worktree がもう無いからである。
// 代わりに、**利用者が打った issue の URL** と **設定の `branch_template`** から名前を
// 組み立て、**`ghq` が答えた clone** を宛先にする。
// **どれもエージェントが書き換えられない**ので、身元ファイルを検算するより根拠が強い
// （身元ファイルは worktree の直下にあり、そこでエージェントが動く。3-16 の段9）。
//
// **接頭辞の検査はしない。**検査が要るのは「与えられた名前が continuo の作ったものか」を
// 疑う場面であり、ここは continuo 自身のテンプレートから名前を作っている。
//
// **1文字も書き換えない。**消すのは DeleteIssueBranch である。
//
// ctx: 実行に適用するコンテキスト。
// issue: issue の URL から取り出した owner・リポジトリ名・番号。
// 戻り値の1つ目: branch 名・clone の場所・実在するか・実在するときの SHA。
// 戻り値の2つ目: テンプレートを変数展開できない場合・`ghq` を実行できない場合・
// **clone が無い場合**（ErrCloneNotFound）・branch の有無を引けない場合のエラー。
// **エラーのときは「残っている」とも「無い」とも言ってはならない。**
func (m *Manager) FindIssueBranch(ctx context.Context, issue IssueRef) (IssueBranch, error) {
	name, warnings, err := RenderBranch(m.cfg.Herdr.Worktree.BranchTemplate, issue)
	if err != nil {
		return IssueBranch{}, err
	}
	m.logWarnings(warnings)

	clone, err := m.clonePath(ctx, issue.Owner, issue.Repo)
	if err != nil {
		return IssueBranch{Name: name}, err
	}
	if clone == "" {
		return IssueBranch{Name: name}, i18n.Errorf(
			i18n.KeyWorkspaceIssueBranchCloneNotFound,
			ErrCloneNotFound, issue.Owner, issue.Repo)
	}

	found := IssueBranch{Name: name, RepoDir: clone}
	exists, err := gitBranchExists(ctx, clone, name)
	if err != nil {
		return found, err
	}
	found.Exists = exists
	if !exists {
		return found, nil
	}
	tip, tipErr := gitBranchTip(ctx, clone, name)
	if tipErr != nil {
		m.logger.Warn("残った branch の SHA を控えられませんでした",
			"repo", clone, "branch", name.String(), "error", tipErr)
	}
	found.Tip = tip

	// **失うものを数える。**worktree が無くても、どの remote にも載っていない commit は
	// リポジトリの中だけで数えられる（gitUnpushedCommits）。数えずに `--force` を
	// 求めると、**利用者は何を失うのかを知らないまま押し切ることになる。**
	unpushed, unpushedErr := gitUnpushedCommits(ctx, clone, name)
	if unpushedErr != nil {
		m.logger.Warn("残った branch の未 push の commit を数えられませんでした",
			"repo", clone, "branch", name.String(), "error", unpushedErr)
		found.UnpushedErr = unpushedErr
		return found, nil
	}
	found.Unpushed = unpushed
	return found, nil
}

// DeleteIssueBranch は、FindIssueBranch が実在すると答えた branch を消す（issue #27）。
//
// **`git worktree prune` を撃たない**（3-37-9b）。実体の無い worktree の登録が残っていると
// `git branch -D` は `used by worktree` で断るが、**それは git がその branch を守っている
// のであって、片付けの邪魔をしているのではない。**worktree のディレクトリは
// 「消えた」のではなく**移された**だけかもしれず、そこには push していない commit が
// 載っている。prune で登録を落とすと git は断らなくなり、**終了コード 0 の
// 「消しました」と一緒にその commit が失われる**（実測: 2026-08-25）。
//
// **断られたら、その登録が指すパスを人間へ見せて止まる。**prune を叩くかどうかは
// 利用者が決めることであり、continuo が代行しない。
//
// ctx: 実行に適用するコンテキスト。
// branch: FindIssueBranch が返した現況（**Exists が真であること**）。
// 戻り値: 消せなかった場合のエラー。**worktree が使っていることになっている場合は、
// その登録のパスと `git worktree prune` の案内を含める。**
func (m *Manager) DeleteIssueBranch(ctx context.Context, branch IssueBranch) error {
	if err := gitBranchDelete(ctx, branch.RepoDir, branch.Name, m.brokenRefPolicy()); err != nil {
		return m.explainBranchDeleteFailure(ctx, branch, err)
	}
	m.logger.Info("残っていた branch を消しました",
		"repo", branch.RepoDir, "branch", branch.Name.String(), "sha", branch.Tip,
		"restore", "git -C "+branch.RepoDir+" branch "+branch.Name.String()+" "+branch.Tip)
	return nil
}

// explainBranchDeleteFailure は `git branch -D` が断った理由を、利用者が動ける形に直す。
//
// **git が「worktree が使っている」と断ったときは、その登録のパスを見せる。**
// git の文言にもパスは出るが、**そのディレクトリが本当に無いなら何を叩けばよいか**が
// 出ない。continuo は prune を代行しないので、**代わりに叩くコマンドを1行渡す。**
//
// **登録が見つからなければ、git の返答をそのまま返す。**別の理由（リポジトリの異常など）で
// 断られた可能性があり、こちらで解釈を足すと誤った案内になる。
//
// ctx: 実行に適用するコンテキスト。
// branch: 消そうとした branch の現況。
// cause: `git branch -D` が返したエラー。
// 戻り値: 人間に見せるエラー。
func (m *Manager) explainBranchDeleteFailure(
	ctx context.Context, branch IssueBranch, cause error,
) error {
	paths, listErr := gitWorktreesUsingBranch(ctx, branch.RepoDir, branch.Name)
	if listErr != nil {
		m.logger.Warn("branch を使っている worktree の登録を引けませんでした",
			"repo", branch.RepoDir, "branch", branch.Name.String(), "error", listErr)
		return cause
	}
	if len(paths) == 0 {
		return cause
	}
	m.logger.Warn("worktree が使っている branch は消しません（prune は撃ちません）",
		"repo", branch.RepoDir, "branch", branch.Name.String(),
		"worktrees", strings.Join(paths, ", "))
	return i18n.Errorf(i18n.KeyWorkspaceIssueBranchUsedByWorktree,
		ErrBranchUsedByWorktree, branch.Name.String(),
		strings.Join(paths, "\n  "), branch.RepoDir, cause)
}
