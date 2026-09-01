// Package socketpath は、Claude Code の hook を受け取る Unix domain socket の
// 置き場所を決める（docs/plans/continuo_design.md 3-23）。
//
// 設定例にあった `/run/continuo/hooks.sock` は macOS では起動できない（`/run` が
// 存在せず、ルートが読み取り専用なので作ることもできない）。そこでここでは
// 実測にもとづく探索順で置き場所を決め、Unix domain socket のパス長の上限も検査する。
package socketpath

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/maimuzo/continuo/internal/fsprobe"
	"github.com/maimuzo/continuo/internal/i18n"
)

// MaxPathLen は continuo が許容する Unix domain socket のパス長の上限（バイト数）である。
//
// macOS は絶対パス103バイトまでで、104バイト以上は bind: invalid argument で失敗する
// （1バイトずつ伸ばして境界を特定した実測値。sockaddr_un の sun_path[104] に対応する）。
// Linux は107バイトまで通る。両対応のため、小さい方である103バイトを上限として採用する（3-23）。
const MaxPathLen = 103

// HookSocketFileName は hook を受ける socket のファイル名である。
const HookSocketFileName = "hooks.sock"

// RuntimeDir は hook を受ける socket を置くディレクトリを、設計 3-23 の探索順で決める。
//
// 探索順（上から順に、最初に見つかったものを使う）:
//  1. envRuntimeDir（環境変数 CONTINUO_RUNTIME_DIR。運用者の逃げ道）
//  2. 環境変数 XDG_RUNTIME_DIR/continuo（Linux の本番。コンテナ内では設定されないことが
//     あるので必須にはしない。**設定されていても、そのディレクトリが実在しなければ使わない** —
//     `/run/user/<uid>` を作るのは systemd であり、アプリが作ってよい場所ではない）
//  3. macOS なら $TMPDIR/continuo（既にユーザー専用で drwx------）
//  4. どれも無ければ ~/.continuo/run
//
// envRuntimeDir: 環境変数 CONTINUO_RUNTIME_DIR の値をそのまま渡す。空文字なら未指定として扱う。
// 戻り値: 決定したディレクトリの絶対パス。ここではまだディレクトリを作らない
// （作成は EnsureDir が担当する）。次のいずれかの場合にエラーを返す。
//   - 環境変数から得た値が絶対パスでない（起動したディレクトリによって socket の場所が
//     変わってしまい、身元ファイルに書いたパスとの一致検査が成立しないため）
//   - os.UserHomeDir が失敗した（既定4 に落ちたときだけ起きうる）
func RuntimeDir(envRuntimeDir string) (string, error) {
	if envRuntimeDir != "" {
		if err := checkAbs(envRuntimeDir, i18n.T(i18n.KeySocketpathSourceEnvRuntimeDir)); err != nil {
			return "", err
		}
		return envRuntimeDir, nil
	}

	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		if err := checkAbs(xdg, i18n.T(i18n.KeySocketpathSourceEnvXDGRuntimeDir)); err != nil {
			return "", err
		}
		// **そのディレクトリが実在するときだけ使う。**
		//
		// `/run/user/<uid>` を作るのは systemd であって、アプリではない。
		// **systemd が動いていない環境（WSL など）では、環境変数だけが設定されていて
		// ディレクトリが無い。**そこへ `MkdirAll` すると `permission denied` で落ちる。
		// 実際、`continuo doctor` が8項目すべて通るのに起動だけが落ちた（issue #9）。
		//
		//	mkdir /run/user/1000: permission denied
		//
		// **無ければ次の候補（~/.continuo/run）へ落とす。**
		if fi, err := os.Stat(xdg); err == nil && fi.IsDir() {
			return filepath.Join(xdg, "continuo"), nil
		}
	}

	if runtime.GOOS == "darwin" {
		if tmp := os.Getenv("TMPDIR"); tmp != "" {
			if err := checkAbs(tmp, i18n.T(i18n.KeySocketpathSourceEnvTMPDir)); err != nil {
				return "", err
			}
			// **XDG と同じく、実在するときだけ使う。**
			// **これが macOS の本番経路である。**手で `TMPDIR` を export している、
			// per-user の一時ディレクトリが掃除された、launchd から起動した、といった場合に
			// 実在しないことがある。**実在を見ないと、起動してから初めて落ちる。**
			if fi, err := os.Stat(tmp); err == nil && fi.IsDir() {
				return filepath.Join(tmp, "continuo"), nil
			}
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", i18n.Errorf(i18n.KeySocketpathRuntimeDirHomeDirFailed, err)
	}
	return filepath.Join(home, ".continuo", "run"), nil
}

// Prepare は、hook を受ける socket を実際に使える状態にして、そのパスを返す。
//
// **「決める」「用意する」「長さを確かめる」を1つの入口にまとめたものである。**
//
// 分かれていたときは、`RuntimeDir` が返した値を `EnsureDir` へ食わせるテストが
// リポジトリに1本も無かった。**その継ぎ目を、実在しない `XDG_RUNTIME_DIR` が通り抜けて
// 利用者に届いた**（issue #9。`continuo doctor` は8項目すべて通るのに起動だけが
// `mkdir /run/user/1000: permission denied` で落ちた）。
//
// **本番もテストも、この1つの入口を通す。**そうすれば、決めた値が外の世界で
// 通用するかを、必ず誰かが確かめることになる。
//
// envRuntimeDir: 環境変数 CONTINUO_RUNTIME_DIR の値。空文字なら未指定として扱う。
// explicitListen: front matter の claude.hook_bridge.listen。空文字なら既定を使う。
// 戻り値: 実際に listen できる socket の絶対パスと、失敗したときのエラー。
// **ディレクトリは作られている**（socket そのものはまだ作らない）。
func Prepare(envRuntimeDir string, explicitListen *string) (string, error) {
	// **明示されていれば、その置き場所の親を用意する。**
	// 明示されていなければ、探索順で決めた置き場所を用意する。
	sock, err := ResolveHookSocketPath(explicitListen, envRuntimeDir)
	if err != nil {
		return "", err
	}
	if err := EnsureDir(filepath.Dir(sock)); err != nil {
		return "", err
	}
	return sock, nil
}

// checkAbs は socket の置き場所として渡されたパスが絶対パスかどうかを検査する。
//
// path: 検査対象のパス。
// source: エラーメッセージに出す、その値の出どころの説明。
// **呼ぶ側は i18n.T(i18n.KeySocketpathSource*) で引いた文言を渡すこと。**
// 文字列リテラルを直接渡すと、その一文だけが訳されないまま残る。
// 戻り値: 絶対パスでない場合にエラーを返す。
func checkAbs(path, source string) error {
	if !filepath.IsAbs(path) {
		return i18n.Errorf(i18n.KeySocketpathCheckAbsNotAbsolute, source, path)
	}
	return nil
}

// checkPathLen は socket のパス長が MaxPathLen 以内かどうかを検査する。
// path: 検査対象の絶対パス。
// 戻り値: MaxPathLen を超えている場合にエラーを返す。
func checkPathLen(path string) error {
	if len(path) > MaxPathLen {
		return i18n.Errorf(i18n.KeySocketpathCheckPathLenTooLong, len(path), MaxPathLen, path)
	}
	return nil
}

// Resolve は dir の下に置く hook socket の絶対パスを組み立て、パス長を検査する。
// dir: RuntimeDir が返したディレクトリ（またはそれに準じるディレクトリ）。
// 戻り値: <dir>/hooks.sock の絶対パス。MaxPathLen バイトを超える場合はエラーを返す。
func Resolve(dir string) (string, error) {
	p := filepath.Join(dir, HookSocketFileName)
	if err := checkPathLen(p); err != nil {
		return "", err
	}
	return p, nil
}

// ResolveHookSocketPath は、front matter の claude.hook_bridge.listen（展開済み）と
// 環境変数 CONTINUO_RUNTIME_DIR から、hook を受ける socket の絶対パスを1つに決める。
//
// explicitListen: front matter の claude.hook_bridge.listen（5-5 の展開を通した後の値）。
// nil または空文字なら未指定として扱い、3-23 の探索順に従う。
// envRuntimeDir: 環境変数 CONTINUO_RUNTIME_DIR の値。RuntimeDir にそのまま渡す。
// 戻り値: 絶対パス。次のいずれかの場合はエラーを返す。
//   - explicitListen が絶対パスでない（設定の読み込み時にも検査しているが、この関数を
//     直接使う経路のために、ここでも同じ条件を検査する）
//   - いずれの経路でもパス長が MaxPathLen を超える
func ResolveHookSocketPath(explicitListen *string, envRuntimeDir string) (string, error) {
	if explicitListen != nil && *explicitListen != "" {
		p := *explicitListen
		if err := checkAbs(p, i18n.T(i18n.KeySocketpathSourceConfigListen)); err != nil {
			return "", err
		}
		if err := checkPathLen(p); err != nil {
			return "", err
		}
		return p, nil
	}

	dir, err := RuntimeDir(envRuntimeDir)
	if err != nil {
		return "", err
	}
	return Resolve(dir)
}

// EnsureDir は socket を置くディレクトリを用意する。
//
// **権限を書き換えるのは continuo 自身が作ったディレクトリだけである。**
// 既にあるディレクトリは検査するだけで、権限に手を触れない。
// claude.hook_bridge.listen に `~/hooks.sock` のような値を書くと、ここへ渡るのは
// ホームディレクトリそのものになる。無条件に 0700 へ落とすと、利用者のホームの権限を
// 黙って壊してしまうためである。
//
// 自分で作った場合は umask で削られた分を打ち消すために 0700 へ設定し直す
// （設計 3-23。「Go が作る socket の権限は umask 次第で、既定の環境では 0755
// （誰でも接続できる）になる」ことが実測されているため、ディレクトリの権限を
// 主たる防御にする）。
//
// **socket 以外の置き場所もここを通る。**二重起動防止のロック（`~/.continuo`。設計 3-17）と
// ボードのロック（`~/.continuo/board`。3-17e）が同じ検査を通る。
// **返す文言は「何のためのディレクトリか」を名乗らない。**それを名乗るのは呼ぶ側の
// 文言であり、ここが名乗ると **ロックの失敗が「hook を受ける socket の…」として報告される。**
//
// dir: 用意するディレクトリの絶対パス。
// 戻り値: 次のいずれかの場合にエラーを返す。
//   - 作成に失敗した
//   - 自分で作ったディレクトリの権限を 0700 に設定できなかった
//   - 既にあるものが symlink である（辿った先へ socket と flock が落ちるため）
//   - 既にあるものがディレクトリでない
//   - 既にあるディレクトリの権限が group / other に開いている（continuo は
//     自分が作っていないディレクトリの権限を書き換えないので、人間に直してもらう）
func EnsureDir(dir string) error {
	if parent := filepath.Dir(dir); parent != dir {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return i18n.Errorf(i18n.KeySocketpathEnsureDirParentMkdirFailed, parent, err)
		}
	}

	switch err := os.Mkdir(dir, 0o700); {
	case err == nil:
		// 自分で作った。umask で削られた分を打ち消して 0700 に固定する。
		if err := os.Chmod(dir, 0o700); err != nil {
			return i18n.Errorf(i18n.KeySocketpathEnsureDirChmodFailed, dir, err)
		}
		return nil
	case errors.Is(err, fs.ErrExist):
		return checkExistingDir(dir)
	default:
		return i18n.Errorf(i18n.KeySocketpathEnsureDirMkdirFailed, dir, err)
	}
}

// checkExistingDir は既にあるディレクトリが socket の置き場所として使えるかを検査する。
// **検査するだけで、権限を書き換えない。**
//
// dir: 検査対象の絶対パス。
// 戻り値: symlink である・ディレクトリでない・権限が group / other に開いている場合のエラー。
func checkExistingDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return i18n.Errorf(i18n.KeySocketpathCheckExistingDirLstatFailed, dir, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return i18n.Errorf(i18n.KeySocketpathCheckExistingDirSymlink, dir)
	}
	if !info.IsDir() {
		return i18n.Errorf(i18n.KeySocketpathCheckExistingDirNotADirectory, dir)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return i18n.Errorf(i18n.KeySocketpathCheckExistingDirPermTooOpen, dir, perm)
	}
	return nil
}

// CheckDirPlaceable は、そのディレクトリを本番で置けるかを、**1つも作らずに**確かめる。
//
// **`EnsureDir` の検査だけを行う版である**（設計 3-17h）。
// **`continuo doctor` はこちらを呼ぶ。**あちらが `EnsureDir` を呼んでいたので、
// **`continuo doctor --id typo` が `~/.continuo/id/typo/` を作って残していた。**
// その置き場所の実在を 3-17f が「その `--id` で continuo が実際に動いた裏付け」に
// 使っているので、**検査の道具が、その裏付けを偽造できる状態になっていた。**
//
// **既にあるなら `EnsureDir` と同じ検査を掛け、そのうえで中に使い捨てを作って消す。**
// **無いなら、上へ辿って最初に実在するディレクトリに書けるかを見る**（そこに作れるなら、
// 起動時の `EnsureDir` も作れる）。**そちらには権限の検査を掛けない。**
// ホームディレクトリは 0755 が普通であり、**掛けると起動できる環境を `✗` と答える。**
//
// dir: 検査するディレクトリの絶対パス。**実在しなくてよい。**
// 戻り値: 置けない場合のエラー。
func CheckDirPlaceable(dir string) error {
	if _, err := os.Lstat(dir); err == nil {
		if cerr := checkExistingDir(dir); cerr != nil {
			return cerr
		}
		return fsprobe.ProbeInside(dir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return i18n.Errorf(i18n.KeySocketpathCheckExistingDirLstatFailed, dir, err)
	}

	ancestor, err := fsprobe.NearestExisting(dir)
	if err != nil {
		return err
	}
	return fsprobe.ProbeInside(ancestor)
}
