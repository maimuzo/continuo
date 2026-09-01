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

// ProbeSocketInside は、その socket を本当に置けるかを、**本番の名前を使わずに**確かめる。
//
// **本番の socket の名前では listen しない**（設計 3-17h）。
// **listen したあとに `os.Remove` すると、その隙に bind し直した常駐の socket を
// 消しうる。**そうなると hook は1件も届かなくなる。
//
// **使い捨ての名前は、本番のファイル名と同じ長さにする。**
// unix socket のパスには上限がある（`socketpath.MaxPathLen`）ので、
// **長くすると、本番なら収まるパスをここが `ENAMETOOLONG` で落としうる。**
// **短くすると、上限ちょうどのパスを見逃す。**同じ長さなら、どちらも起きない。
//
// **名前はプロセスごと・呼び出しごとに変える。**2つの `continuo doctor` が
// 同時に走っても、互いの使い捨ての socket を消さないようにするためである。
//
// sockPath: 本番の socket の絶対パス（**このファイルには一切触れない**）。
// 戻り値: listen できなかった場合のエラー。
func ProbeSocketInside(sockPath string) error {
	if sockPath == "" {
		return i18n.Errorf(i18n.KeyFsprobeDirEmpty)
	}
	if !filepath.IsAbs(sockPath) {
		return i18n.Errorf(i18n.KeyFsprobeDirNotAbsolute, sockPath)
	}
	dir := filepath.Dir(sockPath)
	probe := filepath.Join(dir, probeSocketName(len(filepath.Base(sockPath))))
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

// probeSocketName は、使い捨ての unix socket のファイル名を作る。
//
// **本番のファイル名とちょうど同じ長さにする**（ProbeSocketInside を見よ）。
// **短すぎる長さを渡されたら、その長さを無視して最小の長さにする。**
// 本番の名前は `hooks.sock`（10文字）なので、実際にはここへ来ない。
//
// want: 作りたい長さ（本番のファイル名の長さ）。
// 戻り値: `.` で始まる、`want` 文字（最小 minProbeSocketNameLen 文字）の名前。
func probeSocketName(want int) string {
	if want < minProbeSocketNameLen {
		want = minProbeSocketNameLen
	}
	// **プロセス番号と時刻を混ぜる。**同時に2つの continuo が検査してもぶつからない。
	//
	// **どちらも先頭から採る。**本番の名前は `hooks.sock`（9文字ぶん）なので、
	// **末尾から採ると、時刻だけが残ってプロセス番号が必ず落ちる。**
	// 半分ずつ使えば、同じナノ秒に始まった2つの検査も別の名前になる。
	pid := strconv.FormatInt(int64(os.Getpid()), 36)
	nano := strconv.FormatInt(time.Now().UnixNano(), 36)
	body := take(pid, (want-1)/2) + nano
	for len(body) < want-1 {
		body += "0"
	}
	return "." + body[:want-1]
}

// minProbeSocketNameLen は使い捨ての socket の名前の最小の長さである。
//
// **`.` と、ぶつからない程度の文字数を確保する。**
const minProbeSocketNameLen = 6

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

// take は文字列の先頭から n 文字までを返す。
//
// **短ければそのまま返す。**プロセス番号は環境によって桁数が違う。
//
// s: 元の文字列。
// n: 取りたい長さ。
// 戻り値: 先頭 n 文字（元が短ければ元のまま）。
func take(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
