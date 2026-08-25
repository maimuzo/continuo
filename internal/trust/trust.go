// Package trust は `continuo trust` の実体である。
//
// **人間が `WORKFLOW.md` の `trust.repositories` に列挙したリポジトリだけを対象に、
// `~/.claude.json` の `projects["<パス>"].hasTrustDialogAccepted` を `true` にする**（3-33）。
//
// **ボードから自動で集めて登録しない。**ボードは他人が編集できるので、そこから集めると
// issue を足せる人が「continuo に信頼させるリポジトリ」を増やせてしまう。
// `continuo init` はボードから拾った一覧を `WORKFLOW.md` に並べるだけで、
// **要らない行を消すのは人間である。**
//
// **巡回のループはこの経路を持たない**（4-3）。dispatch の直前の検査は読むだけのままである。
package trust

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/workspace"
)

// DefaultTimeout は外部コマンド（ghq / git）1回あたりの制限時間である。
//
// 期限が無いと、clone の置き場所が NFS 等で固まったときに `continuo trust` が返らない。
const DefaultTimeout = 15 * time.Second

// BackupPrefix はバックアップのファイル名の、時刻より前の部分である。
//
// 実際のファイル名は `~/.claude.json.continuo-backup-<RFC3339>` になる（3-33）。
// **消さない。**continuo は作るだけで、古いバックアップの削除は人間が行う。
const BackupPrefix = ".claude.json.continuo-backup-"

// ErrNoClaudeConfig は `~/.claude.json` が無いことを表す。
//
// **無いときは作らずに止める。**Claude Code を一度も起動していない状態でこのファイルを
// 先に作ると、Claude Code が「設定済み」と見なして初回の案内を出さない可能性がある。
// 先に Claude Code を1度起動してもらう。
var ErrNoClaudeConfig = errors.New("~/.claude.json がありません")

// ErrUnexpectedShape は `~/.claude.json` の形が想定と違うことを表す。
//
// **このエラーのときは1バイトも書かない。**読めない形のまま書き戻すと、
// 認証情報を含む利用者の全設定を失いうる（4-3）。
var ErrUnexpectedShape = errors.New("~/.claude.json の形が想定と違います")

// CloneResolver は owner と repo から clone の絶対パスを引く関数である。
//
// **テストで本物の ghq を叩かずに済むよう、グローバル変数ではなく引数で差し替える。**
// clone が無ければ空文字を返す（エラーにしない）。
type CloneResolver func(ctx context.Context, owner, repo string) (string, error)

// KeyResolver は clone の絶対パスから、信頼を引く鍵を求める関数である。
//
// **鍵は `git rev-parse --path-format=absolute --show-toplevel` の出力である**（3-6）。
type KeyResolver func(ctx context.Context, clonePath string) (string, error)

// fetchCloneTimeout は `ghq get` にかける制限時間である。
//
// **他の外部コマンドより長い。**clone はネットワークとリポジトリの大きさに左右され、
// DefaultTimeout（数十秒）では大きなリポジトリが必ず途中で切れる。
const fetchCloneTimeout = 10 * time.Minute

// Options は Plan と Apply の入力である。
type Options struct {
	// Repositories は `WORKFLOW.md` の `trust.repositories` の値である（"owner/repo" の並び）。
	Repositories []string
	// HomeDir は `~/.claude.json` を探すホームディレクトリの絶対パスである。
	//
	// **テストが実物の `~/.claude.json` を触らないよう、必ず引数で受け取る。**
	HomeDir string
	// ResolveClone は clone の絶対パスを引く関数である。nil なら ghq を使う。
	ResolveClone CloneResolver
	// ResolveKey は信頼を引く鍵を求める関数である。nil なら git を使う。
	ResolveKey KeyResolver
	// Timeout は外部コマンド1回あたりの制限時間である。0 以下なら DefaultTimeout を使う。
	Timeout time.Duration
	// Now は現在時刻を返す関数である。nil なら time.Now を使う（バックアップの名前に使う）。
	Now func() time.Time
	// FetchClone は clone が無いときに取ってくる関数である。
	//
	// **nil なら取りに行かない。**`--dry-run` と、この関数を渡さない呼び出し元は
	// 「clone がありません」のまま止まる（設計 3-22 / 3-33）。
	// **無断でディスクを使わないため、既定では取らない側に倒してある。**
	FetchClone CloneFetcher
	// OnFetch は clone を取りに行く直前に呼ばれる（画面へ知らせるため）。nil なら何もしない。
	OnFetch func(repository string)
	// OnFetched は clone を取り終えた直後に呼ばれる。nil なら何もしない。
	//
	// **取得は最長10分かかる。**始めたことだけを知らせて終わりを知らせないと、
	// 利用者は固まったのか進んでいるのかを判断できない。
	OnFetched func(clonePath string)
}

// CloneFetcher は clone が無いときに取ってくる関数である。
//
// ctx: 呼び出しに適用するコンテキスト。
// owner: リポジトリの所有者名。
// repo: リポジトリ名。
// 戻り値: 取得に失敗した場合のエラー。
type CloneFetcher func(ctx context.Context, owner, repo string) error

// Entry は1つのリポジトリについて調べた結果である。
type Entry struct {
	// Repository は "owner/repo" である。
	Repository string
	// ClonePath は clone の絶対パスである（引けなかった場合は空文字）。
	ClonePath string
	// TrustKey は `~/.claude.json` の projects で使う鍵である（求められなかった場合は空文字）。
	TrustKey string
	// Trusted は、いま既に信頼登録されているかどうかである。
	Trusted bool
	// Requirements は、信頼すると何が効くようになるかである。
	Requirements Requirements
	// Problem は調べられなかった理由である（空なら問題なし）。
	Problem string
	// Unconfirmed は、要求内容を確かめられなかった理由である（空なら確かめられた）。
	//
	// **Problem と分けてある。**Problem はリポジトリに辿り着けなかった場合であり、
	// こちらは辿り着けたが中身を読めなかった場合である。**読めなかったときも
	// 要求内容の一覧は出す**（何を確かめられなかったのかが、そこにしか出ない）。
	Unconfirmed string
}

// Actionable は、この項目を登録の対象にできるかを返す。
//
// **確かめられていないものは対象にしない**（3-33）。`continuo trust` は要求内容を
// 出したあと人間に問い返さずに書き込むので、**中身を見せられていないのに書き込むと、
// 「中身を確かめてから決める」という手順がどこにも無いことになる。**
//
// 戻り値: 鍵まで求められていて、調べられなかった理由も、確かめられなかった理由も無ければ true。
func (e Entry) Actionable() bool {
	return e.Problem == "" && e.Unconfirmed == "" && e.TrustKey != ""
}

// Report は Plan が調べた結果である。
type Report struct {
	// ClaudeConfigPath は読んだ `~/.claude.json` の絶対パスである。
	ClaudeConfigPath string
	// Entries は `trust.repositories` に書かれた順の調査結果である。
	Entries []Entry
}

// Pending は、まだ信頼登録されておらず、登録の対象にできる項目を返す。
//
// 戻り値: 登録すると変わる項目。
func (r *Report) Pending() []Entry {
	var out []Entry
	for _, e := range r.Entries {
		if e.Actionable() && !e.Trusted {
			out = append(out, e)
		}
	}
	return out
}

// Problems は、調べられなかった項目を返す。
//
// 戻り値: 理由つきの項目。
func (r *Report) Problems() []Entry {
	var out []Entry
	for _, e := range r.Entries {
		if !e.Actionable() {
			out = append(out, e)
		}
	}
	return out
}

// Plan は列挙されたリポジトリを調べる。**何も書き込まない。**
//
// 1つのリポジトリにつき、次を順に行う（3-6 の「信頼を引く鍵の作り方」と同じ3段）。
//
//  1. `ghq list -p -e <owner>/<repo>` で clone のパスを引く
//  2. `git -C <そのパス> rev-parse --path-format=absolute --show-toplevel` で鍵を求める
//  3. その鍵で `~/.claude.json` を読み、いま信頼済みかを見る
//
// あわせて clone の中の `.claude/settings.json` と `.mcp.json` を読み、
// **信頼すると何が効くようになるか**を集める（3-33）。
//
// ctx: 呼び出し全体に適用するコンテキスト。
// opts: 対象と、ホームディレクトリと、差し替え可能な外部コマンド。
// 戻り値の1つ目: 調べた結果。
// 戻り値の2つ目: 入力が不正な場合のエラー（1件ごとの失敗は Entry.Problem に入れて返す）。
func Plan(ctx context.Context, opts Options) (*Report, error) {
	if !filepath.IsAbs(opts.HomeDir) {
		return nil, i18n.Errorf(i18n.KeyTrustOptionsHomeDirNotAbsolute, opts.HomeDir)
	}
	resolveClone, resolveKey, timeout := opts.resolvers()

	report := &Report{
		ClaudeConfigPath: claudeConfigPath(opts.HomeDir),
		Entries:          make([]Entry, 0, len(opts.Repositories)),
	}
	for _, name := range opts.Repositories {
		report.Entries = append(report.Entries,
			inspect(ctx, name, opts.HomeDir, resolveClone, resolveKey, timeout, opts.FetchClone, opts.OnFetch, opts.OnFetched))
	}
	return report, nil
}

// inspect は1つのリポジトリを調べる。
//
// ctx: 呼び出しに適用するコンテキスト。
// name: "owner/repo"。
// homeDir: `~/.claude.json` を探すホームディレクトリ。
// resolveClone: clone のパスを引く関数。
// resolveKey: 信頼を引く鍵を求める関数。
// timeout: 外部コマンド1回あたりの制限時間。
// fetchClone: clone が無いときに取ってくる関数。nil なら取りに行かない。
// onFetch: 取りに行く直前に呼ぶ関数。nil なら何もしない。
// onFetched: 取り終えた直後に呼ぶ関数。nil なら何もしない。
// 戻り値: 調べた結果。
func inspect(ctx context.Context, name, homeDir string, resolveClone CloneResolver, resolveKey KeyResolver, timeout time.Duration, fetchClone CloneFetcher, onFetch func(string), onFetched func(string)) Entry {
	e := Entry{Repository: name}

	owner, repo, ok := strings.Cut(name, "/")
	if !ok || owner == "" || repo == "" {
		e.Problem = fmt.Sprintf("%q は \"owner/repo\" の形ではありません", name)
		return e
	}

	cloneCtx, cancel := context.WithTimeout(ctx, timeout)
	clonePath, err := resolveClone(cloneCtx, owner, repo)
	cancel()
	if err != nil {
		e.Problem = fmt.Sprintf("clone のパスを引けませんでした（%v）", err)
		return e
	}
	if clonePath == "" {
		if fetchClone == nil {
			e.Problem = fmt.Sprintf(
				"clone がありません（`ghq list -p -e %s` の出力が空。--dry-run では取りに行きません）", name)
			return e
		}
		// **ここが唯一、continuo がディスクへ書きに行く場所である。**
		// 取ったあとに引き直す。取れても引けないなら、置き場所の設定が食い違っている。
		if onFetch != nil {
			onFetch(name)
		}
		fetchCtx, cancelFetch := context.WithTimeout(ctx, fetchCloneTimeout)
		err := fetchClone(fetchCtx, owner, repo)
		cancelFetch()
		if err != nil {
			e.Problem = fmt.Sprintf("clone を取れませんでした（%v）", err)
			return e
		}
		reCtx, cancelRe := context.WithTimeout(ctx, timeout)
		clonePath, err = resolveClone(reCtx, owner, repo)
		cancelRe()
		if err != nil || clonePath == "" {
			e.Problem = fmt.Sprintf(
				"clone を取ったのにパスを引けませんでした（`ghq list -p -e %s` の出力が空）", name)
			return e
		}
		if onFetched != nil {
			onFetched(clonePath)
		}
	}
	e.ClonePath = clonePath

	keyCtx, cancel := context.WithTimeout(ctx, timeout)
	key, err := resolveKey(keyCtx, clonePath)
	cancel()
	if err != nil {
		e.Problem = fmt.Sprintf("clone %s のパスを git で確定できませんでした（%v）", clonePath, err)
		return e
	}
	e.TrustKey = key

	// **判定は巡回のループと同じ関数で行う**（internal/workspace）。
	// ここに別の判定を書くと、`continuo trust` が「登録した」と言ったのに
	// 巡回のループは「未信頼」と見る、という食い違いが起こりうる。
	trusted, _, err := workspace.CheckTrustForClonePath(clonePath, homeDir)
	if err != nil {
		e.Problem = fmt.Sprintf("いまの信頼の状態を読めませんでした（%v）", err)
		return e
	}
	e.Trusted = trusted
	e.Requirements = readRequirements(clonePath)
	// **確かめられなかったものは登録の対象から外す**（3-33）。
	//
	// `continuo trust` は要求内容を出したあと、人間に問い返さずに書き込む。
	// **中身を見せられていないのに書き込むと、「中身を確かめてから決める」という手順が
	// どこにも無いことになる。**
	if e.Requirements.Unconfirmed() {
		e.Unconfirmed = fmt.Sprintf(
			"要求内容を確かめられなかったので登録しません（%s）。原因を直してから実行し直してください",
			strings.Join(e.Requirements.Notes, " / "))
	}
	return e
}

// Change は書き込みの対象1件である。
type Change struct {
	// Repository は "owner/repo" である。
	Repository string
	// TrustKey は `~/.claude.json` の projects に書いた鍵である。
	TrustKey string
}

// ApplyResult は Apply が何をしたかである。
type ApplyResult struct {
	// ClaudeConfigPath は書き換えた `~/.claude.json` の絶対パスである。
	ClaudeConfigPath string
	// BackupPath は取ったバックアップの絶対パスである（**書き込まなかった場合は空文字**）。
	BackupPath string
	// Changed は `true` を書き込んだ項目である。
	Changed []Change
	// Skipped は既に `true` だったので触らなかった項目である。
	Skipped []Change
	// Verified は書き込んだあとに読み直して、信頼済みと確認できた項目である。
	Verified []Change
	// VerifyProblems は書き込んだのに確認できなかった項目の理由である。
	VerifyProblems []string
}

// Apply は、Plan が調べた項目のうち登録の対象にできるものを `~/.claude.json` へ書き込む。
//
// **守ること**（3-33）。
//
//   - **書き込みの直前にもう一度読み直す。**起動中の Claude Code のセッションが
//     同じファイルを書き戻しているため、Plan が読んだ内容で上書きしてはならない
//   - **形が想定と違ったら1バイトも書かない**（ErrUnexpectedShape）
//   - **既に `true` のものは触らない。**変える項目が1つも無ければ、
//     バックアップも書き込みもしない
//   - **他のリポジトリの記述を1つも変えない。**値は生のバイト列のまま持ち回し、
//     キーの並び順も保つ
//   - **バックアップを取ってから、一時ファイルへ書いて `os.Rename` で置き換える**
//
// ctx: 呼び出し全体に適用するコンテキスト。
// opts: Plan に渡したものと同じ入力。
// report: Plan が返した結果。
// 戻り値の1つ目: 何をしたか。
// 戻り値の2つ目: 読み書きに失敗した場合のエラー（ErrNoClaudeConfig / ErrUnexpectedShape を含む）。
func Apply(ctx context.Context, opts Options, report *Report) (*ApplyResult, error) {
	if !filepath.IsAbs(opts.HomeDir) {
		return nil, i18n.Errorf(i18n.KeyTrustOptionsHomeDirNotAbsolute, opts.HomeDir)
	}
	path := claudeConfigPath(opts.HomeDir)
	result := &ApplyResult{ClaudeConfigPath: path}

	var targets []Change
	for _, e := range report.Entries {
		if e.Actionable() {
			targets = append(targets, Change{Repository: e.Repository, TrustKey: e.TrustKey})
		}
	}
	if len(targets) == 0 {
		return result, nil
	}

	// **ここで読み直すのが要点である。**Plan が読んでから今までの間に、
	// 起動中の Claude Code のセッションが書き戻している可能性がある。
	original, info, err := readClaudeConfig(path)
	if err != nil {
		return result, err
	}

	root, err := parseOrderedObject(original)
	if err != nil {
		return result, fmt.Errorf("%w: %s: %v", ErrUnexpectedShape, path, err)
	}
	projects, err := projectsObject(root, path)
	if err != nil {
		return result, err
	}

	for _, t := range targets {
		changed, err := markTrusted(projects, t.TrustKey, path)
		if err != nil {
			return result, err
		}
		if changed {
			result.Changed = append(result.Changed, t)
		} else {
			result.Skipped = append(result.Skipped, t)
		}
	}
	if len(result.Changed) == 0 {
		// **変えるものが無ければ、バックアップも書き込みもしない。**
		return result, nil
	}

	projectsRaw, err := projects.marshalIndent(1)
	if err != nil {
		return result, i18n.Errorf(i18n.KeyTrustApplyProjectsMarshalFailed, path, err)
	}
	root.set(projectsKey, projectsRaw)
	updated, err := root.marshalIndent(0)
	if err != nil {
		return result, i18n.Errorf(i18n.KeyTrustApplyRootMarshalFailed, path, err)
	}
	// 元のファイルが改行で終わっていたなら、そろえる（差分の最終行が動かないようにする）。
	if len(original) > 0 && original[len(original)-1] == '\n' {
		updated = append(updated, '\n')
	}

	backupPath, err := writeBackup(opts.HomeDir, path, original, info.Mode().Perm(), opts.now())
	if err != nil {
		return result, err
	}
	result.BackupPath = backupPath

	if err := replaceFile(path, updated, info.Mode().Perm()); err != nil {
		return result, i18n.Errorf(i18n.KeyTrustApplyReplaceFailed, path, backupPath, err)
	}

	// **書いたものが、巡回のループから信頼済みに見えるかを確かめる。**
	// 鍵の作り方がずれていると「書いたのに効かない」が静かに起きる。
	for _, c := range result.Changed {
		clonePath := clonePathOf(report, c.Repository)
		if clonePath == "" {
			// **空のパスで git を起動しない。**`-C` を付けずに git が走ると、
			// いまいるディレクトリのリポジトリを answer にしてしまう。
			result.VerifyProblems = append(result.VerifyProblems,
				fmt.Sprintf("%s: clone のパスが分からないので、書き込んだあとの確認ができませんでした", c.Repository))
			continue
		}
		trusted, reason, err := workspace.CheckTrustForClonePath(clonePath, opts.HomeDir)
		switch {
		case err != nil:
			result.VerifyProblems = append(result.VerifyProblems,
				fmt.Sprintf("%s: 書き込んだあとの確認に失敗しました（%v）", c.Repository, err))
		case !trusted:
			result.VerifyProblems = append(result.VerifyProblems,
				fmt.Sprintf("%s: 書き込んだのに信頼済みになっていません（%s）", c.Repository, reason))
		default:
			result.Verified = append(result.Verified, c)
		}
	}
	return result, nil
}

// clonePathOf は report から、そのリポジトリの clone のパスを引く。
//
// report: Plan が返した結果。
// repository: "owner/repo"。
// 戻り値: clone の絶対パス（見つからなければ空文字）。
func clonePathOf(report *Report, repository string) string {
	for _, e := range report.Entries {
		if e.Repository == repository {
			return e.ClonePath
		}
	}
	return ""
}

// projectsObject は root から projects を取り出す。**無ければ空のものを作る。**
//
// **あるのにオブジェクトでなければ止める。**読めない形のまま書き戻すと、
// 利用者の全設定を失いうる。
//
// root: `~/.claude.json` のトップレベル。
// path: エラー文に出すファイルのパス。
// 戻り値の1つ目: projects のオブジェクト。
// 戻り値の2つ目: 形が想定と違う場合の ErrUnexpectedShape。
func projectsObject(root *orderedObject, path string) (*orderedObject, error) {
	raw, ok := root.get(projectsKey)
	if !ok {
		return newOrderedObject(), nil
	}
	projects, err := parseOrderedObject(raw)
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyTrustProjectsObjectUnparsable, ErrUnexpectedShape, path, projectsKey, err)
	}
	return projects, nil
}

// markTrusted は projects の1件を信頼済みにする。**他の項目には触らない。**
//
// projects: `~/.claude.json` の projects。
// key: 信頼を引く鍵（clone の最上位の絶対パス）。
// path: エラー文に出すファイルのパス。
// 戻り値の1つ目: 実際に変えたなら true（既に `true` だったなら false）。
// 戻り値の2つ目: 形が想定と違う場合の ErrUnexpectedShape。
func markTrusted(projects *orderedObject, key, path string) (bool, error) {
	raw, ok := projects.get(key)
	if !ok {
		entry := newOrderedObject()
		entry.set(trustedKey, json.RawMessage("true"))
		encoded, err := entry.marshalIndent(2)
		if err != nil {
			return false, i18n.Errorf(i18n.KeyTrustMarkTrustedEntryMarshalFailed, path, key, err)
		}
		projects.set(key, encoded)
		return true, nil
	}

	entry, err := parseOrderedObject(raw)
	if err != nil {
		return false, i18n.Errorf(i18n.KeyTrustMarkTrustedEntryUnparsable, ErrUnexpectedShape, path, projectsKey, key, err)
	}
	if current, ok := entry.get(trustedKey); ok {
		var accepted bool
		if err := json.Unmarshal(current, &accepted); err != nil {
			return false, i18n.Errorf(i18n.KeyTrustMarkTrustedFlagNotBool,
				ErrUnexpectedShape, path, projectsKey, key, trustedKey, string(current))
		}
		if accepted {
			// **既に true のものは触らない。**
			return false, nil
		}
	}
	entry.set(trustedKey, json.RawMessage("true"))
	encoded, err := entry.marshalIndent(2)
	if err != nil {
		return false, i18n.Errorf(i18n.KeyTrustMarkTrustedEntryRemarshalFailed, path, key, err)
	}
	projects.set(key, encoded)
	return true, nil
}

// readClaudeConfig は `~/.claude.json` を読む。
//
// path: 読むファイルの絶対パス。
// 戻り値の1つ目: 中身。
// 戻り値の2つ目: ファイルの情報（権限を引き継ぐために使う）。
// 戻り値の3つ目: 無い場合の ErrNoClaudeConfig、読めない場合のエラー。
func readClaudeConfig(path string) ([]byte, fs.FileInfo, error) {
	// **symlink を辿った先を書き換えない。**辿ると、ホームディレクトリの外にある
	// 別のファイルを一時ファイルの rename で置き換えることになる。
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, i18n.Errorf(i18n.KeyTrustReadClaudeConfigNotFound, ErrNoClaudeConfig, path)
		}
		return nil, nil, i18n.Errorf(i18n.KeyTrustReadClaudeConfigStatFailed, path, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, nil, i18n.Errorf(i18n.KeyTrustReadClaudeConfigSymlink, ErrUnexpectedShape, path)
	}
	if !info.Mode().IsRegular() {
		return nil, nil, i18n.Errorf(i18n.KeyTrustReadClaudeConfigNotRegularFile, ErrUnexpectedShape, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, i18n.Errorf(i18n.KeyTrustReadClaudeConfigReadFailed, path, err)
	}
	return data, info, nil
}

// writeBackup は書き換える前の中身を、消さないバックアップとして残す（3-33）。
//
// **元のファイルと同じ権限で作る。**このファイルには認証情報を含む全設定が入っている
// ので、既定の 0644 で置いてはならない。
//
// homeDir: バックアップを置くディレクトリ。
// path: 元のファイルのパス（エラー文に使う）。
// original: 書き換える前の中身。
// perm: 元のファイルの権限。
// now: バックアップの名前に使う時刻。
// 戻り値の1つ目: 作ったバックアップの絶対パス。
// 戻り値の2つ目: 書けなかった場合のエラー。
func writeBackup(homeDir, path string, original []byte, perm fs.FileMode, now time.Time) (string, error) {
	backupPath := filepath.Join(homeDir, BackupPrefix+now.Format(time.RFC3339))
	// O_EXCL で「無いときだけ作る」。同じ名前のバックアップを黙って潰さない。
	f, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return "", i18n.Errorf(i18n.KeyTrustWriteBackupCreateFailed, path, backupPath, path, err)
	}
	if _, err := f.Write(original); err != nil {
		f.Close()
		return "", i18n.Errorf(i18n.KeyTrustWriteBackupWriteFailed, backupPath, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", i18n.Errorf(i18n.KeyTrustWriteBackupSyncFailed, backupPath, err)
	}
	if err := f.Close(); err != nil {
		return "", i18n.Errorf(i18n.KeyTrustWriteBackupCloseFailed, backupPath, err)
	}
	return backupPath, nil
}

// replaceFile は一時ファイルへ書いてから os.Rename で置き換える。
//
// **途中で落ちても、元のファイルが壊れた状態で残らない。**同じディレクトリに
// 一時ファイルを作るのは、os.Rename が同じファイルシステムの中でしか原子的でないためである。
//
// path: 置き換える先のファイルの絶対パス。
// data: 書き込む中身。
// perm: 元のファイルの権限（引き継ぐ）。
// 戻り値: 書けなかった場合のエラー。
func replaceFile(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".continuo-tmp-*")
	if err != nil {
		return i18n.Errorf(i18n.KeyTrustReplaceFileTempCreateFailed, dir, err)
	}
	tmpPath := tmp.Name()
	// 失敗して抜けたときに一時ファイルを残さない（成功時は rename 済みなので何も起きない）。
	defer os.Remove(tmpPath)

	// **権限を先にそろえる。**os.CreateTemp は 0600 で作るが、元のファイルが
	// 別の権限だった場合に rename でそれが失われる。
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return i18n.Errorf(i18n.KeyTrustReplaceFileChmodFailed, tmpPath, perm, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return i18n.Errorf(i18n.KeyTrustReplaceFileWriteFailed, tmpPath, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return i18n.Errorf(i18n.KeyTrustReplaceFileSyncFailed, tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return i18n.Errorf(i18n.KeyTrustReplaceFileCloseFailed, tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return i18n.Errorf(i18n.KeyTrustReplaceFileRenameFailed, tmpPath, path, err)
	}
	return nil
}

// claudeConfigPath は `~/.claude.json` の絶対パスを組み立てる。
//
// homeDir: ホームディレクトリの絶対パス。
// 戻り値: `~/.claude.json` の絶対パス。
func claudeConfigPath(homeDir string) string {
	return filepath.Join(homeDir, workspace.ClaudeConfigFileName)
}

// resolvers は Options のうち、差し替え可能なものの既定値を埋めて返す。
//
// 戻り値の1つ目: clone のパスを引く関数。
// 戻り値の2つ目: 信頼の鍵を求める関数。
// 戻り値の3つ目: 外部コマンド1回あたりの制限時間。
func (o Options) resolvers() (CloneResolver, KeyResolver, time.Duration) {
	resolveClone := o.ResolveClone
	if resolveClone == nil {
		resolveClone = workspace.RunGhqList
	}
	resolveKey := o.ResolveKey
	if resolveKey == nil {
		resolveKey = RunGitToplevel
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return resolveClone, resolveKey, timeout
}

// now は Options の時刻を返す。
//
// 戻り値: Now が nil なら time.Now() の値。
func (o Options) now() time.Time {
	if o.Now == nil {
		return time.Now()
	}
	return o.Now()
}
