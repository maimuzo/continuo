package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/i18n"
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

// ErrBranchInUseElsewhere は、目的の branch を**目的のパス以外の worktree** が既に
// チェックアウトしていることを表す（着手の段0。設計 3-16b）。
//
// **git は1つの branch を2つの worktree に出せない。**この状態で `git worktree add` を
// 叩くと `fatal: '<branch>' is already used by worktree at '<別のパス>'` で必ず落ちる。
// **目的のパスにはまだ何も無い**ので、ErrUnregisteredWorktree では拾えない。
var ErrBranchInUseElsewhere = errors.New("目的の branch を別の場所の worktree が使っています")

// ErrCloneNotFound は `ghq list -p -e <owner>/<repo>` で clone を引けなかったことを表す。
// **continuo は勝手に clone しない**（3-22）。その issue を飛ばして人間に知らせる。
var ErrCloneNotFound = errors.New("ghq に対象リポジトリの clone がありません")

// ErrRepoMismatch は worktree の `.git` が指すリポジトリが、置き場所のパスと ghq から
// 決まるリポジトリと食い違うことを表す（3-20 / 3-22 の検算）。
//
// **「調べられない」とは別である。**調べられないだけなら、branch には触らずに worktree の
// 実体を片付けてよい（issue #23）。**食い違っているのは書き換えの痕跡であり、
// 消す相手を取り違えている可能性がある。**そのときは1バイトも消さない。
var ErrRepoMismatch = errors.New("worktree の .git が指すリポジトリが検算に合いません")

// ErrBaseUnknown は base を決められなかったことを表す（3-22 の段4）。
// herdr.worktree.base が null で、Issue.NativeRef["default_branch"] も無い場合に返る。
var ErrBaseUnknown = errors.New("worktree を切る base を決められません")

// ErrWorktreeDetached は worktree がどの branch にも載っていないことを表す
// （detached HEAD。issue #132）。
//
// **ErrUnregisteredWorktree では拾えない。**登録はされていて、branch に載っていないだけである。
// **文面を分けないと、人間は「別の branch にいます: … は "HEAD" をチェックアウトしています」
// という、原因を伝えない案内を読むことになる。**"HEAD" という名前の branch を探しに行く。
//
// **文言は Error() が呼ばれるたびに資源から引く**（i18n.Sentinel）。
// errors.New に日本語で書くと、日本語の文言の件数を数える検査に引っかかる。
var ErrWorktreeDetached = i18n.Sentinel(i18n.KeyWorkspaceErrWorktreeDetached)

// isDetachedHead は、その worktree が detached HEAD かどうかをリポジトリ側に答えさせる。
//
// **`git rev-parse --abbrev-ref HEAD` の戻り値を "HEAD" と文字列比較しない。**
// あちらは detached でも壊れた ref でも同じ答えを返しうるし、
// **同じ問いに対する答えが package の中で2通りになる。**
// `gitWorktreeHeadAt` が `worktree list --porcelain` の `detached` の行を読んで
// 正確に答えるので、それを使う（設計 3-9 の段4 と同じ見分け方）。
//
// **判定できないときは false を返す。**判定できないことを理由に着手を止めない
// （3-34b）。呼び出し元は今までどおり branch 名の食い違いとして扱う。
func (m *Manager) isDetachedHead(ctx context.Context, repoPath, worktreePath string) bool {
	_, detached, err := gitWorktreeHeadAt(ctx, repoPath, worktreePath)
	if err != nil {
		return false
	}
	return detached
}

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
	// HerdrRepoWorkspaceID は、**この呼び出しが開かせてしまったリポジトリの親 workspace の
	// ID** である（issue #19。Identity.HerdrRepoWorkspaceID を見よ）。
	// **`worktree.open` を呼ぶ前から親 workspace があった場合は空文字である**
	// （人間が開いたものなので閉じてはならない）。身元ファイルへ必ず書くこと。
	HerdrRepoWorkspaceID string
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
//  2. 目的のパスに worktree があり git にも登録されていて、**その worktree が
//     目的の branch をチェックアウトしていれば**再利用する
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
// （ErrBaseUnknown）・**どの branch にも載っていない**（ErrWorktreeDetached）・
// 登録の無い実体がある／**別の branch を出している**
// （ErrUnregisteredWorktree）・
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
		// **コマンドは実値で組み立てる。**`<owner>/<repo>` のまま出すと、
		// 人間がコピーしても叩けない（設計 3-34b）。
		return nil, i18n.Errorf(
			i18n.KeyWorkspacePrepareCloneNotFound,
			ErrCloneNotFound, issue.Owner, issue.Repo, issue.Owner, issue.Repo)
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
		return nil, i18n.Errorf(i18n.KeyWorkspacePrepareStatFailed, loc.Path, statErr)
	}

	switch {
	case exists && registered:
		// 段2: 再利用する。
		//
		// **再利用する前に、その worktree が本当に目的の branch を出しているかを
		// git に答えさせる。**人間が同じ worktree で別の branch へ切り替えていた場合、
		// そのまま再利用するとエージェントが意図しない branch の上で作業し、
		// 食い違いに気づくのは片付けのとき（成果が別 branch に積まれたあと）になる。
		head, err := gitCurrentBranch(ctx, loc.Path)
		if err != nil {
			return nil, err
		}
		if head != loc.Branch.String() && m.isDetachedHead(ctx, repoPath, loc.Path) {
			// **detached HEAD は、別の branch にいるのとは原因も直し方も違う**（issue #132）。
			// 文面を分けないと、人間が "HEAD" という名前の branch を探しに行く。
			return nil, i18n.Errorf(
				i18n.KeyWorkspacePrepareDetachedHead,
				ErrWorktreeDetached, loc.Path, loc.Path,
				loc.Path, loc.Branch.String(), loc.Path, loc.Branch.String())
		}
		if head != loc.Branch.String() {
			// 段3 と同じ判断である。**乗っ取らない。**
			return nil, i18n.Errorf(
				i18n.KeyWorkspacePrepareBranchMismatch,
				ErrUnregisteredWorktree, loc.Path, head, loc.Branch.String())
		}
		// 既存の身元ファイルを先に読む（3-18）。
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
		return nil, i18n.Errorf(
			i18n.KeyWorkspacePrepareUnregisteredWorktree,
			ErrUnregisteredWorktree, loc.Path, loc.Path, loc.Path)
	default:
		// 段4・段5。
		base, err := m.resolveBase(issue)
		if err != nil {
			return nil, err
		}
		result.Base = base
		if err := os.MkdirAll(filepath.Dir(loc.Path), rootDirPerm); err != nil {
			return nil, i18n.Errorf(i18n.KeyWorkspacePrepareParentDirCreateFailed, filepath.Dir(loc.Path), err)
		}
		if err := gitWorktreeAdd(ctx, repoPath, loc.Path, loc.Branch, base, m.brokenRefPolicy()); err != nil {
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
		// **開く前に、リポジトリの親 workspace が既にあるかを見ておく**（issue #19）。
		// 無かったのに開いたあとにあれば、それは**この呼び出しが開かせたもの**であり、
		// 片付けで閉じる責任が continuo にある。**先にあったなら人間のものなので触らない。**
		repoWorkspaceExisted := m.repoWorkspaceOpen(ctx, repoPath)
		// **`path` と `branch` は片方だけ渡す。**両方渡すと herdr が
		// `invalid_request: exactly one of path or branch is required` で弾く（実測: 2026-08-20）。
		// **worktree は直前に git で作ってあるので、パスで開く。**
		//
		// **`cwd` はリポジトリ本体を渡す。外せない。**worktree のパスを渡すと herdr は
		// `linked_worktree_source: New and open worktree actions start from the repo parent workspace.`
		// で断り、`cwd` を省くと `worktree_not_found: worktree path not found` で断る
		// （実測: 2026-08-25、test/live）。**リポジトリの親 workspace は herdr の必須の親である。**
		//
		// **label は `owner/repo/issues/N` の形である**（設計 3-3）。
		// 組み立ては herdr.IssueLabel に寄せてある（orchestrator 側と形がずれないため）。
		label := herdr.IssueLabel(issue.Owner, issue.Repo, issue.Number)
		opened, err := m.herdr.WorktreeOpen(ctx, herdr.WorktreeOpenParams{
			Path:  resolvedPath,
			Cwd:   repoPath,
			Focus: &focus,
			Label: label,
		})
		if err != nil {
			return nil, i18n.Errorf(i18n.KeyWorkspacePrepareWorktreeOpenFailed, resolvedPath, err)
		}
		result.HerdrWorkspaceID = opened.Workspace.WorkspaceID
		if !repoWorkspaceExisted {
			result.HerdrRepoWorkspaceID = m.repoWorkspaceID(ctx, repoPath)
		}
		result.HerdrPaneID = opened.RootPane.PaneID
		result.AlreadyOpen = opened.AlreadyOpen
		// **herdr が開いたものが、いま作った worktree と同じかを必ず確かめる。**
		//
		// **実運用で、clone のほうを開いた workspace が返ってきた**（2026-08-21、設計 6-2）。
		// そうなると pane の cwd が clone を指し、そこで Claude Code が起動する。
		// 気づかないまま進むと、**別の issue の作業を同じ場所で始めることになる。**
		if opened.Worktree.Path != "" && !samePath(opened.Worktree.Path, resolvedPath) {
			return nil, i18n.Errorf(i18n.KeyWorkspacePrepareWorktreePathMismatch,
				resolvedPath, opened.Worktree.Path, opened.Workspace.WorkspaceID)
		}
		// **label は worktree.open では上書きされない。**既に開かれていた workspace
		// （already_open）には作成時の label が残るので、開き直すたびに書き直す。
		// **IssueLabel が空文字を返したら呼ばない**（draft issue で壊れた label を書かない）。
		if label != "" {
			if _, err := m.herdr.WorkspaceRename(ctx, herdr.WorkspaceRenameParams{
				WorkspaceID: opened.Workspace.WorkspaceID,
				Label:       label,
			}); err != nil {
				// **致命にしない。**label は人間が herdr の画面で見分けるためのもので
				// あり、復元の照合は pane の cwd で行う（設計 3-3）。
				m.logger.Warn("herdr workspace の label を書き直せませんでした",
					"workspace_id", opened.Workspace.WorkspaceID, "label", label, "error", err)
			}
		}
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
		return "", i18n.Errorf(
			i18n.KeyWorkspaceResolveBaseDefaultBranchMissing, ErrBaseUnknown, issue.Identifier)
	}
	s, ok := raw.(string)
	if !ok || s == "" {
		return "", i18n.Errorf(
			i18n.KeyWorkspaceResolveBaseDefaultBranchNotString,
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

// CheckWorktreeUsable は「その issue に着手したら段2 と段3 で必ず落ちるか」を、
// **1バイトも書かずに**判定する（3-22 の段2・段3 と同じ判断を、着手の段0 で行う）。
//
// **なぜ Prepare と同じ判断を2箇所に持つのか。**Prepare（着手の段3）は、
// ボードの Status を running_state へ書いたあと（段2）に走る。目的のパスに
// 実体があるのに git の worktree として登録されていない場合、着手は必ず失敗するのに
// **その前に Status だけが書き換わる。**`In Progress` は active_states なので
// 次の巡回でまた候補に上がり、`In Progress` と `Blocked` の往復が永久に続く。
// **この判定を段0（preflight）へ置くことで、失敗が確定している issue は
// Status を1バイトも動かさずに飛ばせる。**Prepare 側の検査は保険として残す。
//
// **読み取りだけを行う。**`git worktree prune` も `git worktree add` も呼ばない。
// prune は「登録が残っているのに実体が消えている」を解消するものであり、
// ここで見る「実体があるのに登録が無い」には効かない。
//
// **判定できない事情では落とさない。**clone を引けない・git を実行できないといった
// 事情は、**段3 に任せて `failure_state` と issue のコメントで人間へ渡す**（3-34b）。
// ここで落とすと、その issue は候補に残ったままログにしか出ず、人間へ届かない。
// **この関数がエラーを返すのは「何度やっても必ず失敗する」と言い切れる形だけである。**
//
// **見るのは3つである。**
//
//	目的のパスに実体があるのに git の登録が無い          → ErrUnregisteredWorktree
//	目的のパスの worktree が別の branch を出している    → ErrUnregisteredWorktree
//	目的の branch を目的のパス以外の worktree が使っている → ErrBranchInUseElsewhere
//
// **3つ目は目的のパスに何も無くても起きる**（実機で1件通して見つかった。設計 3-16b）。
//
// ctx: 実行に適用するコンテキスト。
// issue: 検査する issue。
// 戻り値: 上の3つのいずれかに当たった場合の ErrUnregisteredWorktree または
// ErrBranchInUseElsewhere。置き場所を決められない場合と封じ込め検査に落ちた場合は
// そのエラー。**それ以外はすべて nil を返す**（まだ何も無い、正しく再利用できる、
// 判定できない、のいずれか）。
func (m *Manager) CheckWorktreeUsable(ctx context.Context, issue IssueRef) error {
	loc, _, err := Locate(m.resolvedRoot, m.cfg.Herdr.Worktree.BranchTemplate, issue)
	if err != nil {
		return err
	}
	if err := CheckContainment(m.resolvedRoot, loc.Path); err != nil {
		return err
	}

	// **clone の場所は短い間だけ覚える**（clonePath）。判定のたびに ghq のプロセスを
	// 起こすと、ボードの件数ぶん外部プロセスが立つ。
	repoPath, err := m.clonePath(ctx, issue.Owner, issue.Repo)
	if err != nil || repoPath == "" {
		m.logger.Debug("着手できるかを段0 で判定できませんでした（段3 に任せます）",
			"identifier", issue.Identifier, "worktree", loc.Path, "error", err)
		return nil
	}

	// **目的のパスに何も無くても落ちる経路がある。**branch を別の場所の worktree が
	// 使っていると、段4 の `git worktree add` が必ず失敗する。**os.Stat より前に見る。**
	if err := m.checkBranchFree(ctx, repoPath, loc, issue); err != nil {
		return err
	}

	if _, statErr := os.Stat(loc.Path); statErr != nil {
		// まだ何も無い（あるいは見に行けない）。段4 が作るので、ここでは何も言わない。
		return nil
	}

	registered, err := m.isRegisteredWorktree(ctx, repoPath, loc.Path)
	if err != nil {
		m.logger.Debug("worktree の登録を段0 で確かめられませんでした（段3 に任せます）",
			"identifier", issue.Identifier, "worktree", loc.Path, "error", err)
		return nil
	}
	if !registered {
		// 3-22 の段3 と同じ判断である。**乗っ取らない。**
		return i18n.Errorf(
			i18n.KeyWorkspacePrepareUnregisteredWorktree,
			ErrUnregisteredWorktree, loc.Path, loc.Path, loc.Path)
	}

	// 3-22 の段2 と同じ判断である。**人間が別の branch へ切り替えていたら再利用しない。**
	head, err := gitCurrentBranch(ctx, loc.Path)
	if err != nil {
		m.logger.Debug("worktree の branch を段0 で確かめられませんでした（段3 に任せます）",
			"identifier", issue.Identifier, "worktree", loc.Path, "error", err)
		return nil
	}
	if head != loc.Branch.String() && m.isDetachedHead(ctx, repoPath, loc.Path) {
		// 段2 と同じ判断である（issue #132）。
		return i18n.Errorf(
			i18n.KeyWorkspacePrepareDetachedHead,
			ErrWorktreeDetached, loc.Path, loc.Path,
			loc.Path, loc.Branch.String(), loc.Path, loc.Branch.String())
	}
	if head != loc.Branch.String() {
		return i18n.Errorf(
			i18n.KeyWorkspacePrepareBranchMismatch,
			ErrUnregisteredWorktree, loc.Path, head, loc.Branch.String())
	}
	return nil
}

// checkBranchFree は、目的の branch を**目的のパス以外の worktree** が使っていないかを
// 見る（着手の段0。設計 3-16b）。
//
// **なぜ要るか。**git は1つの branch を2つの worktree に出せない。目的のパスに何も
// 無くても、その branch を別の場所の worktree が出していれば、段4 の
// `git worktree add <目的のパス> <branch>` は
// `fatal: '<branch>' is already used by worktree at '<別のパス>'` で必ず落ちる。
// **目的のパスしか見ない検査（ErrUnregisteredWorktree）ではこの経路を拾えない。**
//
// **目的のパス自身が出している場合は問題ない**（3-22 の段2 の再利用の経路）。除外する。
//
// **読み取りだけを行う。**`git worktree list --porcelain` を読むだけで、
// `prune` も `add` も呼ばない。
//
// **判定できない事情では落とさない。**git を実行できない等は段3 に任せて、
// `failure_state` と issue のコメントで人間へ渡す（3-34b）。
//
// ctx: 実行に適用するコンテキスト。
// repoPath: リポジトリの作業ディレクトリ。
// loc: 目的の置き場所（パスと branch）。
// issue: 検査する issue（対処の案内に URL を載せる）。
// 戻り値: 目的のパス以外の worktree がその branch を使っている場合の
// ErrBranchInUseElsewhere。それ以外は nil。
func (m *Manager) checkBranchFree(
	ctx context.Context, repoPath string, loc *Location, issue IssueRef,
) error {
	entries, err := gitWorktreeEntries(ctx, repoPath)
	if err != nil {
		m.logger.Debug("branch の使われ方を段0 で確かめられませんでした（段3 に任せます）",
			"identifier", issue.Identifier, "worktree", loc.Path, "error", err)
		return nil
	}
	for _, entry := range entries {
		if entry.Branch != loc.Branch.String() {
			continue
		}
		if samePath(entry.Path, loc.Path) {
			// 目的のパス自身である。**再利用の経路なので問題ない。**
			continue
		}
		return i18n.Errorf(
			i18n.KeyWorkspacePrepareBranchInUseElsewhere,
			ErrBranchInUseElsewhere, loc.Branch.String(), entry.Path,
			repoPath, IssueURLForHuman(issue), entry.Path)
	}
	return nil
}

// IssueURLForHuman は、人間が `continuo abandon` に貼れる issue の URL を返す。
//
// **IssueRef.URL は空のことがある**（トラッカーが URL を持たない場合）。
// そのときは置き場所と同じ規則（`<scheme>://<ホスト>/<owner>/<repo>/issues/<番号>`）で
// 組み立てる。**ホストは HostFromIssueURL が決める**（空なら DefaultHost）。
//
// issue: 対象の issue。
// 戻り値: issue の URL。
func IssueURLForHuman(issue IssueRef) string {
	if issue.URL != "" {
		return issue.URL
	}
	return fmt.Sprintf("https://%s/%s/%s/issues/%d",
		HostFromIssueURL(issue.URL), issue.Owner, issue.Repo, issue.Number)
}
