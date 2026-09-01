package fsprobe

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
)

// ProbeInside は、既にあるディレクトリの中に本当に書けるかを確かめる。
//
// **使い捨ての名前で作って消す。**本番が使う名前は1つも作らない。
//
// **`ProbeWritable` との違いは、`dir` を作らないことである。**あちらは
// `os.MkdirAll` で `dir` そのものを作るので、**`continuo doctor` から呼ぶと
// 検査する側が本番の置き場所を作ってしまう**（設計 3-17h）。
//
// dir: 既にあるディレクトリの絶対パス。
// 戻り値: 書けなかった場合のエラー。**`dir` が無い場合もエラーである。**
func ProbeInside(dir string) error {
	if dir == "" {
		return i18n.Errorf(i18n.KeyFsprobeDirEmpty)
	}
	if !filepath.IsAbs(dir) {
		return i18n.Errorf(i18n.KeyFsprobeDirNotAbsolute, dir)
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

// NearestExisting は、そのパスから上へ辿って最初に実在するディレクトリを返す。
//
// **`os.MkdirAll` と同じ見方をする。**途中に symlink があっても辿った先を見る
// （`MkdirAll` もそうするので、**ここだけ厳しくすると、起動できる設定を `✗` と答える**）。
//
// path: 調べたい絶対パス（実在しなくてよい）。
// 戻り値の1つ目: 最初に実在するディレクトリの絶対パス。
// 戻り値の2つ目: **実在するものがディレクトリでなかった場合のエラー**
// （そこには何も作れないので、`MkdirAll` も落ちる）。
func NearestExisting(path string) (string, error) {
	cur := filepath.Clean(path)
	for {
		info, err := os.Stat(cur)
		switch {
		case err == nil && info.IsDir():
			return cur, nil
		case err == nil:
			return "", i18n.Errorf(i18n.KeyFsprobeNotADirectory, cur)
		case !errors.Is(err, fs.ErrNotExist):
			return "", i18n.Errorf(i18n.KeyFsprobeStatFailed, cur, err)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", i18n.Errorf(i18n.KeyFsprobeNoExistingAncestor, path)
		}
		cur = parent
	}
}

// ProbeSocketInside は、そのディレクトリに unix socket を本当に置けるかを確かめる。
//
// **本番の socket の名前では listen しない**（設計 3-17h）。
// **listen したあとに `os.Remove` すると、その隙に bind し直した常駐の socket を
// 消しうる。**そうなると hook は1件も届かなくなる。
//
// **パスの長さはここでは見ない。**本番のパスの長さは
// `internal/socketpath` の `checkPathLen` が別に見ている。
// **使い捨ての名前は本番より短くする**ので、ここが `ENAMETOOLONG` で落ちることはない。
//
// dir: 既にあるディレクトリの絶対パス。
// 戻り値: listen できなかった場合のエラー。
func ProbeSocketInside(dir string) error {
	if dir == "" {
		return i18n.Errorf(i18n.KeyFsprobeDirEmpty)
	}
	if !filepath.IsAbs(dir) {
		return i18n.Errorf(i18n.KeyFsprobeDirNotAbsolute, dir)
	}
	// **短い名前にする。**`hooks.sock`（10文字）より短い9文字までに収める。
	name := "." + strconv.FormatInt(time.Now().UnixNano()%1_000_000, 36) + ".sock"
	probe := filepath.Join(dir, name)
	// **前の検査の残骸があれば先に消す。**消すのは自分の使い捨ての名前だけである。
	_ = os.Remove(probe)
	ln, err := net.Listen("unix", probe)
	if err != nil {
		return i18n.Errorf(i18n.KeyFsprobeSocketFailed, dir, err)
	}
	_ = ln.Close()
	// **`net.Listener.Close` は socket のファイルも消す。**消えていなければ消す。
	if err := os.Remove(probe); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return i18n.Errorf(i18n.KeyFsprobeCleanupFailed, probe, err)
	}
	return nil
}

// ProbePlaceable は、そのディレクトリに本当に書けるかを、**作らずに**確かめる。
//
// **`ProbeWritable` の「作らない」版である。**あちらは `os.MkdirAll` で `dir` を作るので、
// **`continuo doctor` から呼ぶと、検査する側が本番の置き場所を作ってしまう**（設計 3-17h）。
//
// **権限は見ない。**`workspace.root` は利用者が普通に作るディレクトリであり、
// **0755 が普通である。**`~/.continuo` のような 0700 を要求する置き場所は
// `socketpath.CheckDirPlaceable` を使うこと。
//
// dir: 検査するディレクトリの絶対パス。**実在しなくてよい。**
// 戻り値: 書けない場合のエラー。
func ProbePlaceable(dir string) error {
	if dir == "" {
		return i18n.Errorf(i18n.KeyFsprobeDirEmpty)
	}
	if !filepath.IsAbs(dir) {
		return i18n.Errorf(i18n.KeyFsprobeDirNotAbsolute, dir)
	}
	target, err := NearestExisting(dir)
	if err != nil {
		return err
	}
	return ProbeInside(target)
}
