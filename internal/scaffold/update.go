package scaffold

// `continuo setup` が既にある WORKFLOW.md を書き換える経路である。
//
// **雛形を書き直さない。**`continuo setup` は `continuo init` が置いたあとの
// WORKFLOW.md に対して走るので、雛形で丸ごと上書きすると、利用者がその間に手で直した行
// （`workspace.root`、`agent.max_concurrent_agents`、`trust.repositories` から消した行など）が
// 全部消える。書き換えるのは StatusKeyNames が返す8つのキーの行だけで、
// **他の行・空行・並び順・インデント・行の右側のコメントは1文字も変えない。**

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/maimuzo/continuo/internal/atomicfile"
	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/i18n"
)

// 区別が要る失敗は sentinel error で返す。cmd/continuo はこれを見て終了コードと文言を決める。
var (
	// ErrNotFound は、書き換える先に WORKFLOW.md が無いことを表す。
	//
	// **`continuo setup` は雛形を新規に作らない。**雛形を置くのは `continuo init` の仕事であり、
	// 2つのコマンドが同じファイルを作れると、どちらが正かが決まらない。
	ErrNotFound = i18n.Sentinel(i18n.KeyScaffoldErrNotFound)

	// ErrKeysNotFound は、既にある WORKFLOW.md に書き換える対象のキーが無いことを表す。
	//
	// **黙って何もしないより落とす。**キーごと消された WORKFLOW.md に書き込んだつもりで
	// 進むと、巡回は無言で「対象0件」を返し続ける。
	ErrKeysNotFound = i18n.Sentinel(i18n.KeyScaffoldErrKeysNotFound)

	// ErrStatusesIncomplete は、5つの役割のうち1つでも空のまま渡されたことを表す。
	//
	// **一部だけ書き換えると、割り当てた Status と雛形の既定値が混ざる。**
	ErrStatusesIncomplete = i18n.Sentinel(i18n.KeyScaffoldErrStatusesIncomplete)

	// ErrKeysNotRewritable は、キーはあるが値が下の行にぶら下がっていて書き換えられない
	// ことを表す。
	//
	// **キーの行だけを組み立て直すと、下の行が値の残骸として残る。**そのまま書くと
	// YAML として読めないファイルになり、**画面には「書き換えました」が出る。**
	// 利用者は「setup は成功したのに continuo が起動しない」状態になるので、書かずに止める。
	ErrKeysNotRewritable = i18n.Sentinel(i18n.KeyScaffoldErrKeysNotRewritable)

	// ErrWouldBreakConfig は、書き換えた結果が front matter として読めなくなることを表す。
	//
	// **書き込む前に組み立てた全文を自分で読み直す。**読めなくなる書き換えは行わない。
	ErrWouldBreakConfig = i18n.Sentinel(i18n.KeyScaffoldErrWouldBreakConfig)
)

// CheckUpdatable は、Status の割り当てを書き換えられるかだけを確かめる。
//
// **`continuo setup` が対話を始める前に呼ぶためにある**（RUCM の基本フロー2）。
// どうせ止まる実行で、先に gh を叩いてボードを読む理由が無い。
//
// **これは事前の見立てであり、保証ではない。**実際の書き換えは UpdateStatuses が行う。
//
// dir: WORKFLOW.md があるディレクトリ。空文字なら、いまいるディレクトリ。
// 戻り値: 書き換える WORKFLOW.md の絶対パス（Result.Overwritten は常に真）。
// エラー: ErrDirNotFound / ErrNotADirectory / ErrNotFound / ErrSymlink /
// ErrKeysNotFound / ErrKeysNotRewritable を errors.Is で判定できる形で返す。
func CheckUpdatable(dir string) (Result, error) {
	path, _, err := statTarget(dir)
	if err != nil {
		return Result{Path: path}, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return Result{Path: path}, i18n.Errorf(i18n.KeyScaffoldFileReadFailed, path, err)
	}

	// **尋ねる前にキーの有無を見る。**
	// 5問すべて答えさせたあとで「キーが無い」と落とすと、入力が全部捨てられる。
	// 置き換える値はここでは使わないので、Complete() を満たすだけのダミーを渡す。
	probe := Statuses{Dispatch: "x", Running: "x", Review: "x", Blocked: "x", Done: "x"}
	_, missing, blocked := applyStatuses(string(raw), probe)
	if len(missing) > 0 {
		return Result{Path: path}, fmt.Errorf("%w: %s: %s", ErrKeysNotFound, path, strings.Join(missing, " / "))
	}
	if len(blocked) > 0 {
		return Result{Path: path}, fmt.Errorf("%w: %s: %s", ErrKeysNotRewritable, path, strings.Join(blocked, " / "))
	}

	// **既に書かれている owner とボードの番号を拾って返す。**
	//
	// `continuo init` で埋めたのに `continuo setup` でもう一度 `--project` を
	// 指定させるのは筋が通らない（2026-08-21 に実際に詰まった。設計 6-2）。
	owner, number := readProviderValues(string(raw))
	return Result{Path: path, Overwritten: true, Owner: owner, ProjectNumber: number}, nil
}

// UpdateStatuses は、既にある WORKFLOW.md の Status に関する8行だけを書き換える。
//
// **書き換えるのは StatusKeyNames が返すキーの行だけである。**行の右側のコメントは
// 原文のまま残し、他の行には触れない。YAML を組み立て直さないので、並び順・空行・
// インデントも変わらない。
//
// **書き込みは不可分にする。**同じディレクトリの一時ファイルへ全文を書いてから
// os.Rename で置き換える。途中で落ちても、WORKFLOW.md が半分書かれた状態にはならない。
//
// dir: WORKFLOW.md があるディレクトリ。空文字なら、いまいるディレクトリ。
// st: 割り当てた選択肢名。5つとも埋まっていること。
// 戻り値: 書き換えたファイルの絶対パス（Result.Overwritten は常に真）。
//
// エラー:
//   - ErrStatusesIncomplete: 5つの役割のうち1つでも空である
//   - ErrDirNotFound: dir が存在しない
//   - ErrNotADirectory: dir が存在するがディレクトリではない
//   - ErrNotFound: dir の直下に WORKFLOW.md が無い
//   - ErrSymlink: WORKFLOW.md が symlink である（辿ると dir の外を書き換えるため）
//   - ErrKeysNotFound: 書き換える対象のキーが WORKFLOW.md に無い
//   - ErrKeysNotRewritable: キーはあるが、値が下の行にぶら下がっていて書き換えられない
//   - ErrWouldBreakConfig: 書き換えると front matter を読めなくなる
//   - 上記以外: 読み込み・書き込みに失敗した
//
// いずれの sentinel error も errors.Is で判定できる形で返す。
func UpdateStatuses(dir string, st Statuses) (Result, error) {
	if !st.Complete() {
		return Result{}, ErrStatusesIncomplete
	}

	path, info, err := statTarget(dir)
	if err != nil {
		return Result{Path: path}, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return Result{Path: path}, i18n.Errorf(i18n.KeyScaffoldFileReadFailed, path, err)
	}

	updated, missing, blocked := applyStatuses(string(raw), st)
	if len(missing) > 0 {
		return Result{Path: path}, fmt.Errorf("%w: %s: %s", ErrKeysNotFound, path, strings.Join(missing, " / "))
	}
	if len(blocked) > 0 {
		return Result{Path: path}, fmt.Errorf("%w: %s: %s", ErrKeysNotRewritable, path, strings.Join(blocked, " / "))
	}

	// **書き込む前に、組み立てた全文を自分で読み直す**（設計 3-32-1）。
	// 読めなくする書き換えを「成功しました」と報告しないための最後の関門である。
	// **元から読めなかった場合は止めない。**それは setup が壊したものではないし、
	// ここで止めると Status の割り当てを直す手立てが無くなる。
	if config.CheckFrontMatterSyntax(string(raw)) == nil {
		if err := config.CheckFrontMatterSyntax(updated); err != nil {
			return Result{Path: path}, fmt.Errorf("%w: %s: %v", ErrWouldBreakConfig, path, err)
		}
	}

	if err := atomicfile.Write(path, []byte(updated), info.Mode().Perm()); err != nil {
		return Result{Path: path}, err
	}
	return Result{Path: path, Overwritten: true}, nil
}

// statTarget は書き換える WORKFLOW.md のパスを決め、それが書き換えてよいものかを確かめる。
//
// dir: WORKFLOW.md があるディレクトリ。空文字なら、いまいるディレクトリ。
// 戻り値: WORKFLOW.md の絶対パスと、その os.Lstat の結果。
// エラー: ErrDirNotFound / ErrNotADirectory / ErrNotFound / ErrSymlink、
// または os.Lstat が失敗した理由。
func statTarget(dir string) (string, fs.FileInfo, error) {
	path, err := resolveTarget(dir)
	if err != nil {
		return "", nil, err
	}

	// symlink そのものの有無を見たいので os.Stat ではなく os.Lstat を使う。
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return path, nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return path, nil, i18n.Errorf(i18n.KeyScaffoldFileStatFailed, path, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		// WriteTemplateWithValues と同じ判断で、辿らずに止める。
		return path, nil, i18n.Errorf(i18n.KeyScaffoldUpdateSymlinkNotFollowed, ErrSymlink, path)
	}
	if !info.Mode().IsRegular() {
		return path, nil, i18n.Errorf(i18n.KeyScaffoldUpdateNotRegularFile, ErrNotFound, path)
	}
	return path, info, nil
}

// providerOwnerRe と providerProjectRe は front matter に書かれた値を拾う。
//
// **YAML として解釈し直さない。**この時点の WORKFLOW.md は、ほかのキーが
// プレースホルダのままでも構わない（`continuo setup` は Status を割り当てる前に呼ばれる）。
// 全体の検証を通そうとすると、埋めていないキーで落ちて先へ進めなくなる。
var (
	providerOwnerRe   = regexp.MustCompile(`(?m)^[ \t]*owner:[ \t]*([^\s#]+)`)
	providerProjectRe = regexp.MustCompile(`(?m)^[ \t]*project_number:[ \t]*([0-9]+)`)
)

// readProviderValues は front matter から owner とボードの番号を拾う。
//
// raw: WORKFLOW.md の全文。
// 戻り値の1つ目: owner。プレースホルダ（`<` で始まる形）なら空文字。
// 戻り値の2つ目: ボードの番号。プレースホルダ（0）なら 0。
func readProviderValues(raw string) (string, int) {
	owner := ""
	if m := providerOwnerRe.FindStringSubmatch(raw); len(m) == 2 {
		v := strings.TrimSpace(m[1])
		// **雛形のプレースホルダは採らない。**`<GitHubのアカウント名>` の形で入っている。
		if !strings.HasPrefix(v, "<") && ValidOwner(v) {
			owner = v
		}
	}
	number := 0
	if m := providerProjectRe.FindStringSubmatch(raw); len(m) == 2 {
		if n, err := strconv.Atoi(strings.TrimSpace(m[1])); err == nil && n > 0 {
			number = n
		}
	}
	return owner, number
}
