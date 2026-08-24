// Package fsprobe は「その場所に本当に書けるか」を実際に書いて確かめる（設計 3-32 / 3-6）。
//
// **文字列を組み立てるだけでは足りない。**issue #11 では、利用者の WSL の
// ファイルシステムが壊れてホームが read-only になり、Claude Code が
// `EROFS: read-only file system, mkdir '~/.claude/session-env/<session_id>'` で
// 止まった。**そのとき `continuo doctor` は9項目すべてを `✗` か `!` にして、
// 本当の原因を1つも指摘しなかった。**doctor が書き込みを試す検査を1つしか持たず、
// しかもそれが設定ファイルの下流にあったためである。
//
// **このパッケージは doctor と daemon の両方から呼ぶ。**同じ関数を呼び、落ち方だけを
// 変える（doctor は記号で並べ、起動は止める）。
package fsprobe

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
)

// SessionEnvDirName は Claude Code が SessionStart hook のたびに作るディレクトリの名前である。
//
// **continuo はここを作らないし、消さない。**作るのは Claude Code である。
// continuo は issue ごとの設定ファイルに SessionStart hook を必ず登録するので、
// **ここが書けないと issue は1件も始まらない。**
//
//	~/.claude/session-env/<session_id>/sessionstart-hook-1.sh
//
// 実測の記録は docs/evidence/hooks_probe_20260817.jsonl にある。
const SessionEnvDirName = "session-env"

// claudeDirName は Claude Code の設定ディレクトリの名前である。
const claudeDirName = ".claude"

// probePrefix は使い捨てのディレクトリの名前の頭に付ける文字列である。
//
// **誰が作ったものかが名前だけで分かるようにする。**消し損ねたものが残ったとき、
// 利用者が「これは何か」を調べずに消せる。
const probePrefix = "continuo-probe-"

// Fault は書き込みや読み込みが落ちた理由の分類である。
//
// **理由で案内を変えるためにある。**`continuo init` を勧めてよいのは
// FaultNotExist のときだけであり、**ファイルシステムが壊れているときに
// 設定を作り直させると、直る見込みのない作業を利用者にさせることになる。**
type Fault int

const (
	// FaultNone はエラーが無いことを表す。
	FaultNone Fault = iota
	// FaultNotExist は対象が無いことを表す（ENOENT）。
	FaultNotExist
	// FaultPermission は権限が足りないことを表す（EACCES / EPERM）。
	FaultPermission
	// FaultFilesystem はファイルシステムそのものが異常であることを表す（EIO / EROFS）。
	//
	// **この2つは同時に出ることがある。**WSL の仮想ディスクが壊れると、
	// カーネルが ext4 を read-only へ落とし、そのうえで I/O エラーも返す。
	FaultFilesystem
	// FaultOther は上のどれでもないことを表す（front matter の不備など）。
	FaultOther
)

// Classify はエラーを Fault へ読み分ける。
//
// **`errors.Is` で読む。**継ぎ足しの文言に頼らない。continuo のエラーは
// i18n.Errorf（fmt.Errorf そのまま）が `%w` で包むので、外から包んだあとでも
// syscall の値まで辿れる。
//
// err: 読み分けるエラー。nil なら FaultNone。
// 戻り値: 落ちた理由の分類。
func Classify(err error) Fault {
	if err == nil {
		return FaultNone
	}
	// **ファイルシステムの異常を先に見る。**EROFS は権限の話に見えることがあるが、
	// 直し方はまったく違う（権限を直しても書けない）。
	if errors.Is(err, syscall.EROFS) || errors.Is(err, syscall.EIO) {
		return FaultFilesystem
	}
	if errors.Is(err, fs.ErrNotExist) {
		return FaultNotExist
	}
	if errors.Is(err, fs.ErrPermission) {
		return FaultPermission
	}
	return FaultOther
}

// ClaudeSessionEnvDir は Claude Code が SessionStart hook のたびに書くディレクトリを返す。
//
// home: ホームディレクトリ。空なら os.UserHomeDir() の結果を使う。
// 戻り値: `<home>/.claude/session-env` の絶対パスと、ホームを特定できなかった場合のエラー。
func ClaudeSessionEnvDir(home string) (string, error) {
	resolved, err := resolveHome(home)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, claudeDirName, SessionEnvDirName), nil
}

// ProbeClaudeSessionEnv は Claude Code の設定ディレクトリに本当に書けるかを確かめる。
//
// **使い捨てのディレクトリを実際に作って消す。**Claude Code は SessionStart hook を
// 走らせる前に `~/.claude/session-env/<session_id>/` を作って CLAUDE_ENV_FILE を置く。
// continuo は issue ごとに SessionStart hook を必ず張るので、**ここが書けないと
// issue は1件も始まらない。**
//
// home: ホームディレクトリ。空なら os.UserHomeDir() の結果を使う。
// 戻り値: 確かめた `<home>/.claude/session-env` の絶対パスと、書けなかった場合のエラー。
func ProbeClaudeSessionEnv(home string) (string, error) {
	dir, err := ClaudeSessionEnvDir(home)
	if err != nil {
		return "", err
	}
	if err := ProbeWritable(dir); err != nil {
		return dir, err
	}
	return dir, nil
}

// ProbeWritable は dir を用意し、その下に使い捨てのディレクトリを作って消す。
//
// **作って消すところまでやる。**os.Stat で見るだけでは、read-only で再マウントされた
// ファイルシステムを見抜けない（読めるが書けない）。
//
// **作った使い捨てのディレクトリは必ず消す。**dir 自身は消さない（もともと在ったかも
// しれないし、Claude Code や continuo があとで使う）。
//
// dir: 書けるかを確かめるディレクトリの絶対パス。無ければ作る。
// 戻り値: 用意できなかった場合、または書けなかった場合のエラー。
func ProbeWritable(dir string) error {
	if dir == "" {
		return i18n.Errorf(i18n.KeyFsprobeDirEmpty)
	}
	if !filepath.IsAbs(dir) {
		return i18n.Errorf(i18n.KeyFsprobeDirNotAbsolute, dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return i18n.Errorf(i18n.KeyFsprobeMkdirFailed, dir, err)
	}
	probe := filepath.Join(dir, probeName())
	if err := os.Mkdir(probe, 0o700); err != nil {
		return i18n.Errorf(i18n.KeyFsprobeWriteFailed, dir, err)
	}
	if err := os.Remove(probe); err != nil {
		return i18n.Errorf(i18n.KeyFsprobeCleanupFailed, probe, err)
	}
	return nil
}

// CheckWritablePlaces は起動の前に書けなければならない場所をまとめて確かめる（設計 3-6）。
//
// **doctor が呼ぶのと同じ ProbeClaudeSessionEnv / ProbeWritable を呼ぶ。**
// 違うのは落ち方だけで、doctor は記号で並べ、起動は最初の失敗で止まる。
//
// home: ホームディレクトリ。空なら os.UserHomeDir() の結果を使う。
// workspaceRoot: worktree の置き場所（workspace.root）。空なら確かめない。
// 戻り値: どこかに書けなかった場合のエラー。
func CheckWritablePlaces(home, workspaceRoot string) error {
	dir, err := ProbeClaudeSessionEnv(home)
	if err != nil {
		return i18n.Errorf(i18n.KeyFsprobeClaudeHomeFailed, dir, err)
	}
	if workspaceRoot == "" {
		return nil
	}
	if err := ProbeWritable(workspaceRoot); err != nil {
		return i18n.Errorf(i18n.KeyFsprobeWorkspaceRootFailed, workspaceRoot, err)
	}
	return nil
}

// probeName は使い捨てのディレクトリの名前を作る。
//
// **同時に2つの continuo が検査してもぶつからない名前にする。**プロセス番号と
// 時刻を混ぜる。
//
// 戻り値: 使い捨てのディレクトリの名前。
func probeName() string {
	return probePrefix + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

// resolveHome はホームディレクトリを決める。
//
// configured: 呼び出し側が明示したホームディレクトリ。空なら未指定として扱う。
// 戻り値: ホームディレクトリの絶対パスと、特定できなかった場合のエラー。
func resolveHome(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", i18n.Errorf(i18n.KeyFsprobeHomeDirFailed, err)
	}
	return home, nil
}
