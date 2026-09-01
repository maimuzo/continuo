package workspace

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/normalize"
)

// 壊れた ref（reference broken）の始末（設計 3-22b）。
//
// **壊れているのは `<clone>/.git/refs/heads/<branch>` という1つのファイルである。**
// 中身を読めないので git 自身がそれを使えず、`git update-ref -d` も `git branch -D` も
// 断る（実測: 2026-08-25、git 2.50.1）。**ファイルとして消すしかない。**
//
// **消してよい条件は brokenBranchRef に全部書いてある。**そこを通らなければ1バイトも消さない。

// brokenRefContentLimit は「壊れているか」を中身で確かめるときに読む上限（バイト）である。
//
// **正常な loose な ref は 41 バイト（40桁の SHA と改行）か、`ref: refs/…` の1行しかない。**
// それより大きいファイルは、その時点で ref として読めない。**全部を読む必要は無い。**
const brokenRefContentLimit = 4096

// brokenRefReflogTailLimit は reflog のファイルの末尾から読む上限（バイト）である。
//
// **最後の1行だけが要る。**reflog は追記されるので、末尾からこれだけ読めば
// 最新の1行は必ず入る（1行は 200 バイト前後である）。
const brokenRefReflogTailLimit = 8192

// brokenRefPolicy は「壊れた ref を消してよいか」を決める材料である。
//
// **prefix は herdr.worktree.branch_template から作った接頭辞である**（BranchPrefix）。
// **空なら1バイトも消さない。**テンプレートに変数が無いと接頭辞を決められず、
// そのときは全部の branch が対象になってしまう（3-9 の段6b と同じ判断）。
type brokenRefPolicy struct {
	// prefix は continuo が作る branch の接頭辞である（既定は `continuo/`）。
	prefix string
	// logger は消したこと・消さなかったことを書き出す先である。nil でもよい。
	logger *slog.Logger
	// notices は**人間の画面へ出す1行**を積む先である。nil でもよい。
	//
	// **ログでは届かない相手がいる。**`continuo abandon` は Logger を渡さないので、
	// ログにだけ書くと「continuo が自分でファイルを1つ消した」ことが
	// 人間に1文字も伝わらない（issue #23 で同じことを CleanupResult.Leftovers で直した）。
	notices *[]string
}

// brokenRef は「消してよい」と判定した壊れた ref のファイル1件である。
//
// **判定した時点の姿を持ち回る。**削除の直前にもう一度読み直して突き合わせ、
// 一致しなければ消さない（判定と削除のあいだに別のプロセスが正常な ref を
// 置き終えていることがあるため）。
type brokenRef struct {
	// path は消してよい loose な ref のファイルの絶対パス（解決済み）である。
	path string
	// size は判定した時点のファイルの大きさである。
	size int64
	// modTime は判定した時点の最終更新時刻である。
	modTime time.Time
	// tip は reflog から読めた「壊れる前に指していた commit」の SHA である。
	// **読めなければ空文字である。**
	tip string
}

// brokenRefPolicy は Manager の設定から作る。
//
// **接頭辞は `BranchPrefixForSweep` から取る**（設計 3-17f）。
// **素の `BranchPrefix` を使ってはならない。**`branch_template` が変数で始まる設定
// （`{{.issue.repo}}-{{.issue.number}}` など）では接頭辞が空で、壊れた ref を1つも消さない。
// **そこへ `--id e2e` を付けると接頭辞が `e2e/` になり、掃除が動き出す。**
// **人間が自分で切った `e2e/spike` の壊れた ref を消すことになる。**
//
// 戻り値: 壊れた ref を消してよいかの判断に使う材料。
func (m *Manager) brokenRefPolicy() brokenRefPolicy {
	return brokenRefPolicy{
		prefix: BranchPrefixForSweep(m.cfg.Herdr.Worktree.BranchTemplate, m.instanceID),
		logger: m.logger,
	}
}

// brokenRefPolicyFor は、消したことを人間の画面へ届ける先を添えた材料を作る。
//
// result: 通知を積む先の片付けの結果。
// 戻り値: notices を result.Notices に向けた材料。
func (m *Manager) brokenRefPolicyFor(result *CleanupResult) brokenRefPolicy {
	policy := m.brokenRefPolicy()
	if result != nil {
		policy.notices = &result.Notices
	}
	return policy
}

// log は logger が nil でも落ちないログの出し口である。
//
// level: 出力する重大度。
// msg: 本文。
// args: 構造化ログのキーと値の並び。
func (p brokenRefPolicy) log(level slog.Level, msg string, args ...any) {
	if p.logger == nil {
		return
	}
	p.logger.Log(context.Background(), level, msg, args...)
}

// notice は人間の画面へ出す1行を積む。
//
// msg: 積む文（i18n を通した完成した文）。
func (p brokenRefPolicy) notice(msg string) {
	if p.notices == nil {
		return
	}
	*p.notices = append(*p.notices, msg)
}

// brokenBranchRef は、branch の loose な ref のファイルが「壊れていて、continuo が
// 消してよいもの」かを判定し、消してよければその姿を返す。
//
// **通す条件は次の7つを全部満たすことである。1つでも欠けたら nil を返す。**
//
//   - branch_template から決めた接頭辞が空でなく、branch 名がその接頭辞で始まること
//     （**continuo が作る branch の ref だけを対象にする**）
//   - `git check-ref-format refs/heads/<名前>` が通ること
//     （**`<名前>.lock` のような、refname として不正な名前を先に弾く。**
//     `.lock` で終わる名前は ref ではなく、**別の git プロセスが握っている lock ファイル**でありうる）
//   - `git show-ref --verify refs/heads/<名前>` が失敗すること
//     （**成功するなら正常な branch である。消してはならない**）
//   - `git rev-parse --verify refs/heads/<名前>` も失敗すること
//     （**commit に解決できるなら壊れていない**）
//   - **途中のシンボリックリンクを解決したうえで**、そのパスが
//     `<共通ディレクトリ>/refs/heads` の内側に収まっていること
//     （文字列の前方一致だけでは、`refs/heads/continuo/<何か>` を別のディレクトリへの
//     シンボリックリンクにされると `.git` の外の任意の1ファイルを消せてしまう）
//   - そのパスが実在し、**通常のファイルである**こと（ディレクトリとシンボリックリンクは触らない）
//   - **中身が ref として読めないこと。**40桁／64桁の16進でも `ref: ` 始まりでもないこと
//     （**読める中身があるなら、消せばその情報が失われる**）
//
// **packed-refs は1バイトも触らない。**loose な ref を消したあとに packed-refs 側の
// 同名の ref が生き返ることがあるので、**消したあとに branch が残っていないかを
// 呼び出し側が確かめること**（gitBranchDelete）。
//
// ctx: 実行に適用するコンテキスト。
// repoDir: リポジトリの作業ディレクトリ。
// branch: 対象の branch 名。
// policy: 消してよいかの判断に使う材料。
// 戻り値の1つ目: 消してよい loose な ref のファイルの姿。
// **消してはならない・壊れていない・そもそも無い場合は nil である。**
// 戻り値の2つ目: git を起動できなかった場合・ファイルの状態を見に行けなかった場合のエラー。
func brokenBranchRef(
	ctx context.Context,
	repoDir string,
	branch normalize.SafeName,
	policy brokenRefPolicy,
) (*brokenRef, error) {
	if policy.prefix == "" {
		policy.log(slog.LevelWarn,
			"herdr.worktree.branch_template に変数が無いので壊れた ref を消しません",
			"branch", branch.String())
		return nil, nil
	}
	if !strings.HasPrefix(branch.String(), policy.prefix) {
		policy.log(slog.LevelWarn,
			"continuo の接頭辞で始まらない branch なので壊れた ref を消しません",
			"branch", branch.String(), "prefix", policy.prefix)
		return nil, nil
	}

	refName := "refs/heads/" + branch.String()

	// **refname として不正な名前を先に弾く。**`<名前>.lock` は git が refname として
	// 拒むので show-ref も rev-parse も必ず落ちる。だがその名前のファイルは、
	// **別の git プロセスが今まさに握っている lock ファイル**として実在しうる。
	code, err := gitExitCode(ctx, repoDir, "check-ref-format", refName)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		policy.log(slog.LevelWarn,
			"refname として不正な名前なので壊れた ref として扱いません",
			"branch", branch.String(), "ref", refName)
		return nil, nil
	}

	// **正常な branch を消してはならない。**show-ref が通るなら壊れていない。
	code, err = gitExitCode(ctx, repoDir, "show-ref", "--verify", "--quiet", refName)
	if err != nil {
		return nil, err
	}
	if code == 0 {
		return nil, nil
	}
	// commit に解決できるなら壊れていない（packed-refs 側で生きている場合を含む）。
	code, err = gitExitCode(ctx, repoDir, "rev-parse", "--verify", "--quiet", refName)
	if err != nil {
		return nil, err
	}
	if code == 0 {
		return nil, nil
	}

	commonDir, err := gitCommonDir(ctx, repoDir)
	if err != nil {
		return nil, err
	}
	headsDir := filepath.Join(commonDir, "refs", "heads")
	refPath, err := resolvedPathUnder(headsDir, branch.String(), policy, "ref")
	if err != nil {
		return nil, err
	}
	if refPath == "" {
		return nil, nil
	}

	info, err := os.Lstat(refPath)
	if errors.Is(err, os.ErrNotExist) {
		// **loose な ref のファイルが無い。**packed-refs 側だけが壊れている、あるいは
		// reftable のリポジトリである。**ここでできることは無い。**
		return nil, nil
	}
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyWorkspaceBrokenRefStatFailed, refPath, err)
	}
	if !info.Mode().IsRegular() {
		// ディレクトリ・シンボリックリンクは触らない。
		policy.log(slog.LevelWarn,
			"ref の置き場所が通常のファイルではないので消しません",
			"branch", branch.String(), "ref_path", refPath, "mode", info.Mode().String())
		return nil, nil
	}
	readable, err := readableRefContent(refPath, info.Size())
	if err != nil {
		return nil, err
	}
	if readable != "" {
		// **読める中身があるなら、消せばその情報が失われる。**
		policy.log(slog.LevelWarn,
			"ref の中身が読めるので消しません",
			"branch", branch.String(), "ref_path", refPath, "content", readable)
		return nil, nil
	}

	return &brokenRef{
		path:    refPath,
		size:    info.Size(),
		modTime: info.ModTime(),
		tip:     brokenRefTip(commonDir, branch, policy),
	}, nil
}

// resolvedPathUnder は base の下の rel を、**途中のシンボリックリンクを解決したうえで**
// 組み立てる（layout.go の CheckContainmentResolved と同じ扱いである）。
//
// **文字列の前方一致だけでは足りない。**`<base>/continuo` が別のディレクトリへの
// シンボリックリンクなら、前方一致は通るのに、実体は base の外にある。
//
// base: 内側に収まっていなければならないディレクトリの絶対パス。
// rel: base からの相対パス（スラッシュ区切り）。
// policy: 消さなかった理由を書き出す先。
// label: ログで対象を指す呼び名（"ref" など）。
// 戻り値の1つ目: 解決済みの絶対パス。**使ってはならない場合は空文字である。**
// 戻り値の2つ目: 状態を見に行けなかった場合のエラー。
func resolvedPathUnder(base, rel string, policy brokenRefPolicy, label string) (string, error) {
	baseDir := filepath.Clean(base)
	joined := filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(rel)))
	// **まず解決前の文字列で、確実に内側にあることを確かめる。**
	// 一致（joined == baseDir）は通さない。ディレクトリそのものは消す相手ではない。
	if !strings.HasPrefix(joined, baseDir+string(os.PathSeparator)) {
		policy.log(slog.LevelWarn,
			"パスが置き場所の外を指すので触りません",
			"label", label, "path", joined, "base", baseDir)
		return "", nil
	}

	resolvedBase, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", i18n.Errorf(i18n.KeyWorkspaceBrokenRefResolveFailed, baseDir, err)
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(joined))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// 置き場所ごと無い。触る相手が無い。
			return "", nil
		}
		return "", i18n.Errorf(i18n.KeyWorkspaceBrokenRefResolveFailed, filepath.Dir(joined), err)
	}
	resolvedBase = filepath.Clean(resolvedBase)
	resolvedParent = filepath.Clean(resolvedParent)
	if resolvedParent != resolvedBase &&
		!strings.HasPrefix(resolvedParent, resolvedBase+string(os.PathSeparator)) {
		policy.log(slog.LevelWarn,
			"途中のシンボリックリンクを解決すると置き場所の外を指すので触りません",
			"label", label, "path", joined,
			"resolved_parent", resolvedParent, "resolved_base", resolvedBase)
		return "", nil
	}
	return filepath.Join(resolvedParent, filepath.Base(joined)), nil
}

// readableRefContent は、ref のファイルの中身が「ref として読めるもの」なら、その中身を返す。
//
// **読めるなら壊れていない。**40桁／64桁の16進は commit の SHA であり、
// `ref: ` で始まる1行は symref である。**どちらも、消せば失われる情報である。**
//
// path: 読む ref のファイルの絶対パス。
// size: そのファイルの大きさ。
// 戻り値の1つ目: 読めた中身（前後の空白を落としたもの）。**読めないなら空文字である。**
// 戻り値の2つ目: 読み取りに失敗した場合のエラー。
func readableRefContent(path string, size int64) (string, error) {
	if size > brokenRefContentLimit {
		// ref として読める大きさではない。読まずに「読めない」と扱う。
		return "", nil
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", i18n.Errorf(i18n.KeyWorkspaceBrokenRefStatFailed, path, err)
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, brokenRefContentLimit))
	if err != nil {
		return "", i18n.Errorf(i18n.KeyWorkspaceBrokenRefStatFailed, path, err)
	}
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "ref:") {
		return trimmed, nil
	}
	if isHexOID(trimmed) {
		return trimmed, nil
	}
	return "", nil
}

// isHexOID は文字列が git の object id（40桁の SHA-1 か 64桁の SHA-256）かを返す。
//
// s: 検査する文字列。
// 戻り値: object id として読める形なら true。
func isHexOID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// brokenRefTip は、壊れる前にその branch が指していた commit の SHA を reflog から読む。
//
// **「壊れた ref には読める情報が1バイトも無い」は事実ではない。**
// `<共通ディレクトリ>/logs/refs/heads/<branch>` の最後の行に、最後の SHA がそのまま残る
// （実測: 2026-08-25、git 2.50.1）。git のコマンドからは
// `warning: ignoring broken ref` で読めないが、ファイルとしては読める。
//
// **読めたら控えて、戻すためのコマンドをログに出す**（sweep.go の孤児 branch の削除と同じ）。
//
// commonDir: リポジトリの共通ディレクトリ（`.git`）。
// branch: 対象の branch 名。
// policy: 読めなかった理由を書き出す先。
// 戻り値: 最後に指していた commit の SHA。**読めなければ空文字である。**
func brokenRefTip(commonDir string, branch normalize.SafeName, policy brokenRefPolicy) string {
	logsDir := filepath.Join(commonDir, "logs", "refs", "heads")
	logPath, err := resolvedPathUnder(logsDir, branch.String(), policy, "reflog")
	if err != nil || logPath == "" {
		return ""
	}
	info, err := os.Lstat(logPath)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	file, err := os.Open(logPath)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	offset := info.Size() - brokenRefReflogTailLimit
	if offset < 0 {
		offset = 0
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(file, brokenRefReflogTailLimit))
	if err != nil {
		return ""
	}
	lines := strings.Split(string(raw), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		fields := strings.Fields(lines[i])
		if len(fields) < 2 {
			continue
		}
		// 1つ目が古い SHA、2つ目が新しい SHA である。
		if !isHexOID(fields[1]) || strings.Trim(fields[1], "0") == "" {
			continue
		}
		return fields[1]
	}
	return ""
}

// pruneBrokenBranchRef は、壊れた loose な ref のファイルを1つ消す。
//
// **消すのは `<共通ディレクトリ>/refs/heads/<名前>` の1ファイルだけである。**
// ディレクトリは消さない（`rm -rf` に相当することは1回も行わない）。
// 判定は brokenBranchRef が全部行う。
//
// **消す直前にファイルの姿をもう一度確かめる。**判定は git を3回起動するので、
// 判定の開始から削除までに数十ミリ秒の窓ができる。そのあいだに別のプロセスが
// 正常な ref を置き終えていると、**作られたばかりの正常な ref を消してしまう。**
// 大きさと最終更新時刻が判定時と食い違ったら消さない。
//
// ctx: 実行に適用するコンテキスト。
// repoDir: リポジトリの作業ディレクトリ。
// branch: 対象の branch 名。
// policy: 消してよいかの判断に使う材料。
// 戻り値の1つ目: 実際に消したら true。**消さなかったら false**（壊れていない・
// 消してよい対象ではない・そもそも無い・判定のあとで書き換わった）。
// 戻り値の2つ目: 判定または削除に失敗した場合のエラー。
func pruneBrokenBranchRef(
	ctx context.Context,
	repoDir string,
	branch normalize.SafeName,
	policy brokenRefPolicy,
) (bool, error) {
	target, err := brokenBranchRef(ctx, repoDir, branch, policy)
	if err != nil {
		return false, err
	}
	if target == nil {
		return false, nil
	}
	// **判定と削除のあいだに割り込まれていないか。**
	again, err := os.Lstat(target.path)
	if errors.Is(err, os.ErrNotExist) {
		// 誰かが先に消した。消す相手はもう無い。
		return false, nil
	}
	if err != nil {
		return false, i18n.Errorf(i18n.KeyWorkspaceBrokenRefStatFailed, target.path, err)
	}
	if !again.Mode().IsRegular() || again.Size() != target.size || !again.ModTime().Equal(target.modTime) {
		policy.log(slog.LevelWarn,
			"判定したあとに ref のファイルが変わったので消しません",
			"branch", branch.String(), "ref_path", target.path,
			"size", again.Size(), "want_size", target.size,
			"mod_time", again.ModTime(), "want_mod_time", target.modTime)
		return false, nil
	}
	if err := os.Remove(target.path); err != nil {
		return false, i18n.Errorf(i18n.KeyWorkspaceBrokenRefRemoveFailed, target.path, err)
	}
	// **何を消したかを必ず残す**（パス・branch・リポジトリ・理由・戻し方）。
	restore := ""
	if target.tip != "" {
		restore = gitBinary + " -C " + repoDir + " branch " + branch.String() + " " + target.tip
	}
	policy.log(slog.LevelWarn, "壊れた ref のファイルを消しました",
		"ref_path", target.path, "branch", branch.String(), "repo", repoDir,
		"sha", target.tip, "restore", restore,
		"reason", "git が読めない壊れた ref（reference broken）で、continuo が作る branch のものだから")
	// **ログでは届かない相手がいる**ので、人間の画面へ出す1行も積む。
	if restore != "" {
		policy.notice(i18n.T(i18n.KeyWorkspaceBrokenRefRemovedWithTip,
			target.path, branch.String(), target.tip, restore))
	} else {
		policy.notice(i18n.T(i18n.KeyWorkspaceBrokenRefRemoved, target.path, branch.String()))
	}
	return true, nil
}
