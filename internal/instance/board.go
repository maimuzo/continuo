package instance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/atomicfile"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/normalize"
)

// BoardDirName はボードごとのロックを並べるディレクトリの名前である
// （`~/.continuo/board/`）。
const BoardDirName = "board"

// BoardLockPath はボード1枚ぶんのロックファイルの絶対パスを返し、置き場所を用意する
// （設計 3-17e）。
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
// 戻り値: ロックファイルの絶対パスと、置き場所を用意できなかった場合のエラー。
// **ディレクトリは作られている**（ロックファイルそのものはまだ作らない）。
func BoardLockPath(owner string, projectNumber int) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, BoardDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", i18n.Errorf(i18n.KeyInstanceBoardDirFailed, dir, err)
	}
	return filepath.Join(dir, boardKey(owner, projectNumber)+".lock"), nil
}

// boardKey はボード1枚を表すファイル名の幹を作る（`<owner>-<project_number>`）。
//
// owner: `tracker.provider.owner`。
// projectNumber: `tracker.provider.project_number`。
// 戻り値: 正規化を通し、スラッシュをハイフンへ潰した文字列。
func boardKey(owner string, projectNumber int) string {
	name, _ := normalize.Normalize(owner)
	safe := strings.ReplaceAll(name.String(), "/", "-")
	return fmt.Sprintf("%s-%d", safe, projectNumber)
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
