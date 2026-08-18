// Package socketpath は、Claude Code の hook を受け取る Unix domain socket の
// 置き場所を決める（docs/plans/continuo_design.md 3-23）。
//
// 設定例にあった `/run/continuo/hooks.sock` は macOS では起動できない（`/run` が
// 存在せず、ルートが読み取り専用なので作ることもできない）。そこでここでは
// 実測にもとづく探索順で置き場所を決め、Unix domain socket のパス長の上限も検査する。
package socketpath

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// MaxPathLen は continuo が許容する Unix domain socket のパス長の上限（バイト数）である。
//
// macOS は絶対パス103バイトまでで、104バイト以上は bind: invalid argument で失敗する
// （1バイトずつ伸ばして境界を特定した実測値。sockaddr_un の sun_path[104] に対応する）。
// Linux は107バイトまで通る。両対応のため、小さい方である103バイトを上限として採用する（3-23）。
const MaxPathLen = 103

// HookSocketFileName は hook を受ける socket のファイル名である。
const HookSocketFileName = "hooks.sock"

// LockFileName は二重起動防止のロックファイル名である（internal/lock が使う）。
// hook の socket と同じディレクトリに置く（設計 5-2 の runtime.lock_file の既定値の説明）。
const LockFileName = "continuo.lock"

// RuntimeDir は hook を受ける socket を置くディレクトリを、設計 3-23 の探索順で決める。
//
// 探索順（上から順に、最初に見つかったものを使う）:
//  1. envRuntimeDir（環境変数 CONTINUO_RUNTIME_DIR。運用者の逃げ道）
//  2. 環境変数 XDG_RUNTIME_DIR/continuo（Linux の本番。コンテナ内では設定されないことが
//     あるので必須にはしない）
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
		if err := checkAbs(envRuntimeDir, "環境変数 CONTINUO_RUNTIME_DIR"); err != nil {
			return "", err
		}
		return envRuntimeDir, nil
	}

	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		if err := checkAbs(xdg, "環境変数 XDG_RUNTIME_DIR"); err != nil {
			return "", err
		}
		return filepath.Join(xdg, "continuo"), nil
	}

	if runtime.GOOS == "darwin" {
		if tmp := os.Getenv("TMPDIR"); tmp != "" {
			if err := checkAbs(tmp, "環境変数 TMPDIR"); err != nil {
				return "", err
			}
			return filepath.Join(tmp, "continuo"), nil
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf(
			"hook を受ける socket の置き場所を決められません（既定の ~/.continuo/run のため"+
				"ホームディレクトリの取得が必要ですが失敗しました）: %w",
			err,
		)
	}
	return filepath.Join(home, ".continuo", "run"), nil
}

// checkAbs は socket の置き場所として渡されたパスが絶対パスかどうかを検査する。
//
// path: 検査対象のパス。
// source: エラーメッセージに出す、その値の出どころの説明（例: "環境変数 CONTINUO_RUNTIME_DIR"）。
// 戻り値: 絶対パスでない場合にエラーを返す。
func checkAbs(path, source string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf(
			"%s の値 %q が絶対パスではありません（相対パスだと continuo を起動したディレクトリによって"+
				"hook を受ける socket の場所が変わり、走行中の Claude Code が持つパスと一致しなくなる）",
			source, path,
		)
	}
	return nil
}

// checkPathLen は socket のパス長が MaxPathLen 以内かどうかを検査する。
// path: 検査対象の絶対パス。
// 戻り値: MaxPathLen を超えている場合にエラーを返す。
func checkPathLen(path string) error {
	if len(path) > MaxPathLen {
		return fmt.Errorf(
			"hook を受ける socket のパスが長すぎます（%d バイト。上限は %d バイト）: %s"+
				"（macOS の Unix domain socket は104バイト以上で bind に失敗するため、設定を読んだ時点で止める）",
			len(path), MaxPathLen, path,
		)
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
// explicitListen: front matter の claude.hook_bridge.listen（5-4 の展開を通した後の値）。
// nil または空文字なら未指定として扱い、3-23 の探索順に従う。
// envRuntimeDir: 環境変数 CONTINUO_RUNTIME_DIR の値。RuntimeDir にそのまま渡す。
// 戻り値: 絶対パス。次のいずれかの場合はエラーを返す。
//   - explicitListen が絶対パスでない（設定の読み込み時にも検査しているが、この関数を
//     直接使う経路のために、ここでも同じ条件を検査する）
//   - いずれの経路でもパス長が MaxPathLen を超える
func ResolveHookSocketPath(explicitListen *string, envRuntimeDir string) (string, error) {
	if explicitListen != nil && *explicitListen != "" {
		p := *explicitListen
		if err := checkAbs(p, "設定キー claude.hook_bridge.listen"); err != nil {
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

// EnsureDir は socket を置くディレクトリを 0700 で作る。
//
// MkdirAll は既存ディレクトリの権限を直さないため、作成の成否によらず必ず Chmod を
// 呼ぶ（設計 3-23。「Go が作る socket の権限は umask 次第で、既定の環境では 0755
// （誰でも接続できる）になる」ことが実測されているため、ディレクトリの権限を
// 主たる防御にする）。
//
// dir: 作成するディレクトリの絶対パス。
// 戻り値: 作成・権限設定に失敗した場合のエラー。
func EnsureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("hook を受ける socket のディレクトリを作成できません: %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("hook を受ける socket のディレクトリの権限を 0700 に設定できません: %s: %w", dir, err)
	}
	return nil
}
