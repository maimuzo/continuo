package instance

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/socketpath"
)

// BoardDirName はボードごとのロックを並べるディレクトリの名前である
// （`~/.continuo/board/`）。
const BoardDirName = "board"

// BoardLockPath はボード1枚ぶんのロックファイルの絶対パスを返す（設計 3-17e）。
//
// **置き場所は作らない。**作るのは EnsureBoardDir である。
// **`continuo abandon --dry-run` は「何も書かない」と README で約束している**ので、
// 「パスを決める」と「置き場所を作る」を1つにしておくと、下見のつもりの実行が
// `~/.continuo/board` を作る。
//
// **`--id` を付けてもボードだけは名前から導けない。**同じボードを2つの continuo が
// 見ると、同じ issue を2つが拾う。**だからボード1枚につきロック1本を取り、
// 取れなければ起動を止める。**
//
// **ロックを読み合う形にしてはならない。**「生きているロックの隣の情報ファイルを読む」
// やり方は、同時に起動した2つが互いに相手の書き終わりを待たずに読む。
// **`flock` が同じ瞬間に2つへ渡らないので、ロック1本なら順序も競合も無い。**
//
// owner: `tracker.provider.owner`。**正規化を通してからファイル名にする**（3-7）。
// 設定に何が書かれていても `~/.continuo/board` の外を指さないようにするためである。
// projectNumber: `tracker.provider.project_number`。
// 戻り値の1つ目: ロックファイルの絶対パス（**ホームディレクトリを引けなかったときだけ空文字**）。
// 戻り値の2つ目: 正規化で情報が落ちた場合の警告。**呼ぶ側は1行ログに残すこと**（3-7）。
// 落としたまま黙ると、`my org` と `my_org` が同じロックになった理由を誰も説明できない。
// 戻り値の3つ目: ホームディレクトリを引けなかった場合のエラー。
func BoardLockPath(owner string, projectNumber int) (string, []normalize.Warning, error) {
	root, err := Root()
	if err != nil {
		return "", nil, err
	}
	key, warnings := boardKey(owner, projectNumber)
	return filepath.Join(root, BoardDirName, key+".lock"), warnings, nil
}

// EnsureBoardDir はボードのロックを置くディレクトリ（`~/.continuo/board`）を用意する。
//
// **`lock.Acquire` を呼ぶ前に必ず通すこと。**親ディレクトリが無いと、
// 重なってもいないのに「ロックファイルを開けません」で止まる。
//
// **socket の置き場所と同じ検査を通す**（symlink・種類・権限）。
// **素の `os.MkdirAll` では、`~/.continuo/board` が symlink に差し替えられていても
// 気づかない。**辿った先に flock と覚え書きが落ちる。
//
// **`--dry-run` の実行では呼んではならない。**この関数はディレクトリを作る。
//
// lockPath: BoardLockPath が返したロックファイルの絶対パス。
// 戻り値: 作成・検査に失敗した場合のエラー。
// **ロックファイルそのものはまだ作らない。**
func EnsureBoardDir(lockPath string) error {
	dir := filepath.Dir(lockPath)
	if err := socketpath.EnsureDir(dir); err != nil {
		return i18n.Errorf(i18n.KeyInstanceBoardDirFailed, dir, err)
	}
	return nil
}

// boardKey はボード1枚を表すファイル名の幹を作る（`<owner>-<project_number>`）。
//
// **owner は小文字へ揃える。**GitHub のログイン名は大文字小文字を区別しないので、
// **`owner: Octocat` と `owner: octocat` は同じボードである。**揃えないと
// ロックが2本に分かれ、**同じボードを2つの continuo が見る。**
//
// owner: `tracker.provider.owner`。
// projectNumber: `tracker.provider.project_number`。
// 戻り値の1つ目: 正規化を通し、スラッシュをハイフンへ潰し、小文字へ揃えた文字列。
// 戻り値の2つ目: 正規化で情報が落ちた場合の警告。
func boardKey(owner string, projectNumber int) (string, []normalize.Warning) {
	name, warnings := normalize.Normalize(owner)
	safe := strings.ToLower(strings.ReplaceAll(name.String(), "/", "-"))
	return fmt.Sprintf("%s-%d", safe, projectNumber), warnings
}
