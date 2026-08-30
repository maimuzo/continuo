package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/atomicfile"
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

// BoardInfo はボードのロックを握っている continuo が名乗る中身である。
//
// **人間が読むためだけのものである**（3-17e）。**排他の判定には一切使わない。**
// 判定は `flock` 1本だけで行う。
type BoardInfo struct {
	// Owner は `tracker.provider.owner` である。
	Owner string `json:"owner"`
	// ProjectNumber は `tracker.provider.project_number` である。
	ProjectNumber int `json:"project_number"`
	// InstanceID は `--id` に渡された名前である。**既定なら空文字。**
	InstanceID string `json:"instance_id"`
	// PID はロックを握っているプロセスの ID である。
	PID int `json:"pid"`
	// ConfigPath は読み込んだ `WORKFLOW.md` の絶対パスである。
	ConfigPath string `json:"config_path"`
	// LockFile は、そのプロセスが握っている二重起動防止のロックの絶対パスである。
	LockFile string `json:"lock_file"`
	// StartedAt はロックを取った時刻（RFC 3339）である。
	StartedAt string `json:"started_at"`
}

// BoardInfoPath はロックファイルの隣に置く情報ファイルの絶対パスを返す。
//
// **`board.json` という固定の名前にはしない**（設計 3-17e の「隣に board.json を書く」を、
// ボードごとに1つになるよう具体化したものである）。**固定にすると、別のボードを見る
// continuo が互いに上書きし、「誰が握っているか」を読むという目的そのものが果たせない。**
//
// lockPath: BoardLockPath が返したロックファイルの絶対パス。
// 戻り値: 拡張子を `.json` に替えた絶対パス。
func BoardInfoPath(lockPath string) string {
	return strings.TrimSuffix(lockPath, ".lock") + ".json"
}

// WriteBoardInfo は、ロックを握ったことを人間が読める形で残す（3-17e の段4）。
//
// **書けなくても起動を止めてはならない。**これは人間のための覚え書きであって、
// 排他の一部ではない。呼ぶ側は失敗をログに1行残すだけにすること。
//
// lockPath: BoardLockPath が返したロックファイルの絶対パス。
// info: 書き込む中身。**StartedAt が空なら now で埋める。**
// now: 時刻の取得。**nil なら time.Now。**
// 戻り値: 書けなかった場合のエラー。
func WriteBoardInfo(lockPath string, info BoardInfo, now func() time.Time) error {
	if now == nil {
		now = time.Now
	}
	if info.StartedAt == "" {
		info.StartedAt = now().Format(time.RFC3339)
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return i18n.Errorf(i18n.KeyInstanceBoardInfoMarshalFailed, err)
	}
	data = append(data, '\n')
	// **一時ファイルへ書き切ってから差し替える**（CLAUDE.md の「絶対に守る制約」4）。
	return atomicfile.Write(BoardInfoPath(lockPath), data, 0o600)
}

// RemoveBoardInfo は、ロックを手放すときに覚え書きを消す（3-17e の段4）。
//
// **ロックを手放す前に呼ぶこと。**手放したあとに消すと、その隙に起動した continuo が
// 書いた覚え書きを消してしまう。
//
// **消えないと、死んだプロセスの PID を指したまま残る。**
// [docs/FAQ.md](../../docs/FAQ.md) は「誰が握っているかを、この覚え書きで読め」と
// 案内しているので、**残ったままだと、動いていない continuo を探しに行くことになる。**
//
// lockPath: BoardLockPath が返したロックファイルの絶対パス。
// 戻り値: 消せなかった場合のエラー。**最初から無い場合は nil を返す**
// （消えていることが目的であり、誰が消したかは問わない）。
func RemoveBoardInfo(lockPath string) error {
	path := BoardInfoPath(lockPath)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return i18n.Errorf(i18n.KeyInstanceBoardInfoRemoveFailed, path, err)
	}
	return nil
}
