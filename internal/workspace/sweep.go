package workspace

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/maimuzo/continuo/internal/normalize"
)

// OrphanBranchSweepRequest は孤児 branch の掃除の入力である（3-9 の手順6b）。
//
// **対象のリポジトリはボードを読まずに決まる。**置き場所の走査（3-4 の段2）で見つかった
// worktree が属するリポジトリだけを見る。ボードに載っていないリポジトリの branch を
// 消さないためである。
type OrphanBranchSweepRequest struct {
	// Worktrees は置き場所の走査で見つかった worktree の絶対パスである。
	// ここから `git rev-parse --git-common-dir` でリポジトリを引く。
	// **消したあとの worktree のパスを渡してはならない**（リポジトリを引けない）。
	Worktrees []string
	// KeepBranches は消してはならない branch 名である。
	// **復元後の印の集合（実行中の一覧）から作る。**引き継いだ run の branch を
	// 「実行中の issue が無い」と誤判定して消さないためである。
	KeepBranches []string
}

// SweepOrphanBranches は孤児 branch を消す（3-9 の手順6b）。
//
// **起動時の掃除の一部であり、復元の手順が終わったあとに呼ぶこと**（3-4 の段9 のあと）。
// 先に呼ぶと、これから引き継ぐ run の branch を孤児と判定して消す。
//
// **`cleanup.delete_branch` が偽なら1本も消さない。**片付け（cleanup.go の段4）は
// この設定を見て branch を残し、「branch は残しました」と人間へ言う。**その branch は
// (1) 接頭辞に一致し (2) どの worktree もチェックアウトしておらず (3) 実行中の run も
// 無いので、下の3条件を全部満たす。**設定を見ない掃除は、次に continuo を起動した
// だけでその branch を `git branch -D`（強制削除）で消す。
// **`continuo abandon --force` で片付けた worktree の branch には、未 push の commit が
// 載っていることがある。**消えれば reflog を掘る以外に戻す手立ては無い。
//
// 消す条件は次の3つを全部満たすことである。
//
//   - `herdr.worktree.branch_template` の接頭辞（既定 `continuo/`）で始まること。
//     **テンプレートに変数が1つも無ければ接頭辞を決められないので、掃除を1件も行わない**
//     （全部の branch が対象になってしまう）
//   - その branch をチェックアウトしている worktree が無いこと
//   - KeepBranches に入っていないこと（＝実行中の issue が無いこと）
//
// **消す前に SHA をログへ残す。**削除は `git branch -D`（マージ状態を見ない強制削除）なので、
// 未 push の commit が載ったままの branch も消える。ログの `restore` に、そのまま実行すれば
// 戻せるコマンドを書く。
//
// **壊れた ref も同じ条件で掃除する**（設計 3-22b）。`git for-each-ref refs/heads` は
// 壊れた ref を一覧に出さないので、branch の一覧だけを見ていると1件も掃除できない。
// `<共通ディレクトリ>/refs/heads` の下を歩いて名前を拾い、brokenBranchRef の条件で選別する。
//
// ctx: 実行に適用するコンテキスト。
// req: 対象の worktree と、消してはならない branch 名。
// 戻り値の1つ目: 実際に消した branch 名（`<リポジトリ>: <branch>` の形）。
// 戻り値の2つ目: 接頭辞を決められない場合と `cleanup.delete_branch` が偽の場合は
// nil を返し、エラーにはしない（掃除を行わないだけである）。リポジトリごとの失敗はログに残して次のリポジトリへ進むので、
// **この関数がエラーを返すことは無い。**戻り値の型は将来の拡張のために残してある。
func (m *Manager) SweepOrphanBranches(ctx context.Context, req OrphanBranchSweepRequest) ([]string, error) {
	// **設定で「branch は消すな」と言われているなら、壊れた ref も含めて1本も消さない。**
	// 片付けが残した branch を、起動しただけで消してしまわないためである。
	// **壊れた ref だけは掃除する、という例外を作らない。**壊れているかどうかは
	// 利用者から見えず、「消すなと言ったのに消えた」という結果だけが同じである。
	if !m.cfg.Cleanup.DeleteBranch {
		m.logger.Info("cleanup.delete_branch が偽なので孤児 branch の掃除を行いません",
			"branch_template", m.cfg.Herdr.Worktree.BranchTemplate)
		return nil, nil
	}

	prefix := BranchPrefix(m.cfg.Herdr.Worktree.BranchTemplate)
	if prefix == "" {
		m.logger.Warn("herdr.worktree.branch_template に変数が無いので孤児 branch の掃除を行いません",
			"branch_template", m.cfg.Herdr.Worktree.BranchTemplate)
		return nil, nil
	}

	keep := map[string]bool{}
	for _, b := range req.KeepBranches {
		if b != "" {
			keep[b] = true
		}
	}

	var deleted []string
	for _, repoDir := range m.repoDirsOf(ctx, req.Worktrees) {
		deleted = append(deleted, m.sweepRepoBranches(ctx, repoDir, prefix, keep)...)
	}
	return deleted, nil
}

// repoDirsOf は worktree の一覧から、それらが属するリポジトリの作業ディレクトリを重複無く返す。
//
// **リポジトリは検算したものだけを返す**（repo.go の verifiedRepo）。worktree の `.git` は
// エージェントが書き換えられるファイルなので、`git rev-parse --git-common-dir` の答えを
// そのまま信じると、**無関係のリポジトリの `continuo/` で始まる branch を
// `git branch -D` で消す。**
//
// ctx: 実行に適用するコンテキスト。
// worktrees: worktree の絶対パス。
// 戻り値: リポジトリの作業ディレクトリ（パスの昇順）。引けなかった・検算に落ちたものは
// 警告を出して飛ばす。
func (m *Manager) repoDirsOf(ctx context.Context, worktrees []string) []string {
	seen := map[string]bool{}
	for _, path := range worktrees {
		_, repoDir, err := m.verifiedRepo(ctx, path)
		if err != nil {
			m.logger.Warn("worktree が属するリポジトリを引けないので孤児 branch の掃除から外します",
				"worktree", path, "error", err)
			continue
		}
		seen[filepath.Clean(repoDir)] = true
	}
	dirs := make([]string, 0, len(seen))
	for dir := range seen {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs
}

// sweepRepoBranches は1つのリポジトリの孤児 branch を消す。
//
// ctx: 実行に適用するコンテキスト。
// repoDir: リポジトリの作業ディレクトリ。
// prefix: continuo が作った branch の接頭辞（空文字ではないこと）。
// keep: 消してはならない branch 名の集合。
// 戻り値: 消した branch を `<リポジトリ>: <branch>` の形で並べたもの。
func (m *Manager) sweepRepoBranches(ctx context.Context, repoDir, prefix string, keep map[string]bool) []string {
	branches, err := gitLocalBranches(ctx, repoDir)
	if err != nil {
		m.logger.Warn("branch の一覧を引けないので、このリポジトリの孤児 branch は消しません",
			"repo", repoDir, "error", err)
		return nil
	}
	inWorktree, err := gitWorktreeBranches(ctx, repoDir)
	if err != nil {
		m.logger.Warn("worktree の一覧を引けないので、このリポジトリの孤児 branch は消しません",
			"repo", repoDir, "error", err)
		return nil
	}
	commonDir, err := gitCommonDir(ctx, repoDir)
	if err != nil {
		m.logger.Warn("共通ディレクトリを引けないので、このリポジトリの孤児 branch は消しません",
			"repo", repoDir, "error", err)
		return nil
	}
	// **`worktree list --porcelain` は ref が壊れた worktree の branch を答えない**
	// （実測: 2026-08-25）。それだけを見ると、**生きている worktree の branch を
	// 孤児と誤判定する。**`<共通ディレクトリ>/worktrees/*/HEAD` の symref を直接読んで補う。
	headRefs, err := worktreeHeadRefs(commonDir)
	if err != nil {
		m.logger.Warn("worktree の HEAD を読めないので、このリポジトリの孤児 branch は消しません",
			"repo", repoDir, "error", err)
		return nil
	}
	for _, name := range headRefs {
		inWorktree[name] = true
	}

	var deleted []string
	for _, raw := range branches {
		if !strings.HasPrefix(raw, prefix) || inWorktree[raw] || keep[raw] {
			continue
		}
		// **正規化で変わる名前は消さない。**continuo が書いた値なら正規化を通しても
		// そのままである（cleanup.go の deletableBranch と同じ判断）。
		name, warnings := normalize.Normalize(raw)
		m.logWarnings(warnings)
		if name.String() != raw {
			m.logger.Warn("branch 名が正規化で変わるので孤児 branch として消しません",
				"repo", repoDir, "branch", raw, "normalized", name.String())
			continue
		}
		// **消す前に SHA を控える。**`git branch -D` はマージ状態を見ないので、
		// 未 push の commit が載ったままの branch も消える。控えがあれば
		// `git branch <名前> <SHA>` で戻せる（無ければ reflog を掘るしかない）。
		tip, tipErr := gitBranchTip(ctx, repoDir, name)
		if tipErr != nil {
			m.logger.Warn("孤児 branch の SHA を控えられませんでした（削除は続けます）",
				"repo", repoDir, "branch", raw, "error", tipErr)
		}
		if err := gitBranchDelete(ctx, repoDir, name, m.brokenRefPolicy()); err != nil {
			m.logger.Warn("孤児 branch を消せませんでした", "repo", repoDir, "branch", raw, "error", err)
			continue
		}
		m.logger.Info("孤児 branch を消しました",
			"repo", repoDir, "branch", raw, "sha", tip,
			"restore", "git -C "+repoDir+" branch "+raw+" "+tip)
		deleted = append(deleted, repoDir+": "+raw)
	}
	deleted = append(deleted, m.sweepBrokenRefs(ctx, repoDir, commonDir, prefix, keep, inWorktree)...)
	return deleted
}

// sweepBrokenRefs は、branch の一覧に出てこない**壊れた ref**を掃除する（設計 3-22b）。
//
// **`git for-each-ref refs/heads` は壊れた ref を一覧に出さない**
// （`warning: ignoring broken ref …` を出して飛ばす。実測: 2026-08-25、git 2.50.1）。
// そのため sweepRepoBranches の本体だけでは、**壊れた ref は起動のたびに素通りして
// 溜まり続ける。**そこで `<共通ディレクトリ>/refs/heads` の下を実ファイルとして歩き、
// 名前を拾ってから brokenBranchRef の条件で選別する。
//
// **歩くのは名前を拾うためだけである。**消してよいかの判定は brokenBranchRef が全部行い、
// 削除は gitBranchDelete を通す（packed-refs 側が生き返っていないかの確認まで含む）。
//
// ctx: 実行に適用するコンテキスト。
// repoDir: リポジトリの作業ディレクトリ。
// commonDir: そのリポジトリの共通ディレクトリ（`.git`）。
// prefix: continuo が作った branch の接頭辞（空文字ではないこと）。
// keep: 消してはならない branch 名の集合。
// inWorktree: worktree が使っている branch 名の集合（HEAD のファイルから補ったもの）。
// 戻り値: 消した branch を `<リポジトリ>: <branch>` の形で並べたもの。
func (m *Manager) sweepBrokenRefs(
	ctx context.Context,
	repoDir, commonDir, prefix string,
	keep, inWorktree map[string]bool,
) []string {
	headsDir := filepath.Join(commonDir, "refs", "heads")
	var candidates []string
	err := filepath.WalkDir(headsDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// 読めないディレクトリは飛ばす。**掃除は最良努力である。**
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			m.logger.Warn("refs/heads の下を歩けないので、その先は見ません",
				"repo", repoDir, "path", path, "error", walkErr)
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		// **シンボリックリンクは1つも辿らない。**IsRegular でないものは名前も拾わない。
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(headsDir, path)
		if relErr != nil {
			return nil
		}
		candidates = append(candidates, filepath.ToSlash(rel))
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		m.logger.Warn("refs/heads の下を歩けないので、壊れた ref の掃除は行いません",
			"repo", repoDir, "heads_dir", headsDir, "error", err)
		return nil
	}

	sort.Strings(candidates)
	var deleted []string
	for _, raw := range candidates {
		if !strings.HasPrefix(raw, prefix) || inWorktree[raw] || keep[raw] {
			continue
		}
		name, warnings := normalize.Normalize(raw)
		m.logWarnings(warnings)
		if name.String() != raw {
			m.logger.Warn("branch 名が正規化で変わるので壊れた ref として消しません",
				"repo", repoDir, "branch", raw, "normalized", name.String())
			continue
		}
		target, brokenErr := brokenBranchRef(ctx, repoDir, name, m.brokenRefPolicy())
		if brokenErr != nil {
			m.logger.Warn("壊れた ref かどうかを判定できませんでした",
				"repo", repoDir, "branch", raw, "error", brokenErr)
			continue
		}
		if target == nil {
			// 壊れていない（＝ for-each-ref が既に見ている）。
			continue
		}
		if err := gitBranchDelete(ctx, repoDir, name, m.brokenRefPolicy()); err != nil {
			m.logger.Warn("壊れた ref の branch を消せませんでした",
				"repo", repoDir, "branch", raw, "error", err)
			continue
		}
		deleted = append(deleted, repoDir+": "+raw)
	}
	return deleted
}
