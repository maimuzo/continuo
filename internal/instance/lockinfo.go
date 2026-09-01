package instance

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/maimuzo/continuo/internal/atomicfile"
	"github.com/maimuzo/continuo/internal/i18n"
)

// LockInfo は flock を握っている continuo が、そのロックの隣に名乗る中身である
// （設計 3-17i）。
//
// **二重起動防止のロック（`~/.continuo/continuo.lock`）と、ボードのロック
// （`~/.continuo/board/<owner>-<番号>.lock`）の両方で、同じ型・同じ書き手・同じ読み手を使う。**
// 型を2つに分けると、片方だけが直されて必ず食い違う（設計 3-17k）。
//
// **排他の判定には使わない。**排他は flock 1本だけである（設計 3-17）。
// このファイルは「誰が握っているか」を人間へ見せることと、
// **flock を掴めない立場（`continuo abandon --dry-run` / `continuo doctor`）が
// 生死を答えるため**にある。
type LockInfo struct {
	// Owner は `tracker.provider.owner` である。
	Owner string `json:"owner"`
	// ProjectNumber は `tracker.provider.project_number` である。
	ProjectNumber int `json:"project_number"`
	// InstanceID は `--id` に渡された名前である。**既定なら空文字。**
	InstanceID string `json:"instance_id"`
	// PID はロックを握っているプロセスの ID である。
	//
	// **生死の判定はこの1つだけで行う**（設計 3-17i）。**起動時刻とは比べない。**
	// StartedAt はロックを取った時刻であってプロセスの起動時刻ではないので、
	// 比べると秒精度でほぼ必ず食い違い、**生きている continuo を毎回 stale と答える。**
	PID int `json:"pid"`
	// ConfigPath は読み込んだ `WORKFLOW.md` の絶対パスである。
	ConfigPath string `json:"config_path"`
	// LockFile は、そのプロセスが握っている二重起動防止のロックの絶対パスである。
	LockFile string `json:"lock_file"`
	// StartedAt はロックを取った時刻（RFC 3339）である。
	//
	// **人間が読むためだけのものである。**判定には使わない（PID を見よ）。
	StartedAt string `json:"started_at"`
}

// LockState は「そのロックを握っている continuo が生きているか」の答えである
// （設計 3-17i）。
//
// **4値である。**「読めなかった・壊れていた」を持たないと、
// **書けなかった覚え書きが `LockStateNotRunning` に丸められ、
// 生きている continuo の worktree を消しにいく。**
type LockState int

const (
	// LockStateNotRunning は、覚え書きが1つも無いことを表す。
	//
	// **覚え書きを書くのはロックを取った側だけである。**無いということは、
	// そのロックを取ったプロセスが1つも居ないということである。**進んでよい。**
	LockStateNotRunning LockState = iota
	// LockStateRunning は、覚え書きがあり、その PID のプロセスが生きていることを表す。
	// **止まること。**
	LockStateRunning
	// LockStateStale は、覚え書きはあるが、その PID のプロセスが居ないことを表す
	// （`SIGKILL` や電源断の残骸）。**進んでよい。ただし残骸があることを画面に出すこと。**
	LockStateStale
	// LockStateUnknown は、覚え書きを読めなかった・中身が壊れていた・
	// PID の生死を確かめられなかったことを表す。
	//
	// **止まること。**覚え書きは「書けなくても起動を止めない」ものなので
	// （WriteLockInfo を見よ）、**読めないことは「動いていない」の証拠にならない。**
	// **分からないなら止まる**（設計 3-17i）。
	LockStateUnknown
)

// String は状態を1語で返す（ログに出す）。
//
// 戻り値: 状態を表す語。
func (s LockState) String() string {
	switch s {
	case LockStateNotRunning:
		return "not_running"
	case LockStateRunning:
		return "running"
	case LockStateStale:
		return "stale"
	case LockStateUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// Blocks は「この状態で先へ進んではならないか」を返す。
//
// **呼び出し側が switch を書き写さないためにある。**書き写すと、
// 4つ目を足したときに片方だけが直されて、**分からないまま進む経路が残る。**
//
// 戻り値: 止まるべきなら true（LockStateRunning と LockStateUnknown）。
func (s LockState) Blocks() bool {
	return s == LockStateRunning || s == LockStateUnknown
}

// LockInfoPath はロックファイルの隣に置く覚え書きの絶対パスを返す。
//
// **`~/.continuo/continuo.lock` なら `~/.continuo/continuo.json`、
// `~/.continuo/board/<owner>-<番号>.lock` なら同じ幹の `.json` である。**
//
// **`board.json` のような固定の名前にはしない。**固定にすると、別のボードを見る
// continuo が互いに上書きし、「誰が握っているか」を読むという目的そのものが果たせない。
//
// lockPath: ロックファイルの絶対パス。
// 戻り値: 拡張子を `.json` に替えた絶対パス。
func LockInfoPath(lockPath string) string {
	return strings.TrimSuffix(lockPath, ".lock") + ".json"
}

// WriteLockInfo は、ロックを握ったことを覚え書きに残す（設計 3-17i）。
//
// **flock を握った直後に呼ぶこと。**握る前に書くと、握れなかったプロセスの PID が
// 残る。**握ってから書くまでのあいだ、観測は `LockStateNotRunning` と答える。**
// この窓のあいだに `continuo abandon` が入っても、あちらは flock を取りにいって
// 「既に動いています」で止まるので、**worktree は消えない。**
//
// **書けなくても起動を止めてはならない。**これは排他の一部ではない。
// 呼ぶ側は失敗をログに1行残すだけにすること。**止まるのは読む側である**
// （読めなければ `LockStateUnknown` になり、そちらが止まる）。
//
// lockPath: ロックファイルの絶対パス。
// info: 書き込む中身。**StartedAt が空なら now で埋める。**
// now: 時刻の取得。**nil なら time.Now。**
// 戻り値: 書けなかった場合のエラー。
func WriteLockInfo(lockPath string, info LockInfo, now func() time.Time) error {
	if now == nil {
		now = time.Now
	}
	if info.StartedAt == "" {
		info.StartedAt = now().Format(time.RFC3339)
	}
	if info.PID == 0 {
		info.PID = os.Getpid()
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return i18n.Errorf(i18n.KeyInstanceLockInfoMarshalFailed, err)
	}
	data = append(data, '\n')
	// **一時ファイルへ書き切ってから差し替える**（CLAUDE.md の「絶対に守る制約」4）。
	// **途中まで書いたファイルを観測が読むと、壊れた JSON として
	// `LockStateUnknown` になり、動いているのに「分からない」と答える。**
	return atomicfile.Write(LockInfoPath(lockPath), data, 0o600)
}

// RemoveLockInfo は、ロックを手放す前に覚え書きを消す（設計 3-17i）。
//
// **握ったまま消すこと。**手放してから消すと、その隙に起動した continuo が書いた
// 覚え書きを消してしまい、**生きている continuo が `LockStateNotRunning` に見える。**
//
// **消えないと、死んだプロセスの PID を指したまま残る。**
// [docs/FAQ.md](../../docs/FAQ.md) は「誰が握っているかを、この覚え書きで読め」と
// 案内しているので、**残ったままだと、動いていない continuo を探しに行くことになる。**
//
// lockPath: ロックファイルの絶対パス。
// 戻り値: 消せなかった場合のエラー。**最初から無い場合は nil を返す**
// （消えていることが目的であり、誰が消したかは問わない）。
func RemoveLockInfo(lockPath string) error {
	path := LockInfoPath(lockPath)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return i18n.Errorf(i18n.KeyInstanceLockInfoRemoveFailed, path, err)
	}
	return nil
}

// ReadLockState は、そのロックを握っている continuo が生きているかを答える
// （設計 3-17i）。
//
// **flock には触らない。**`flock` は「掴んでみる」以外に状態を知る方法が無く、
// 一瞬でも掴むと、**その瞬間に起動した continuo が「二重起動」で落ちる。**
// だから観測は覚え書きだけを読む。
//
// **1バイトも書かない。**`continuo abandon --dry-run` と `continuo doctor` が呼ぶ。
//
// lockPath: ロックファイルの絶対パス（覚え書きのパスはここから導く）。
// 戻り値の1つ目: 4値の答え。**LockStateUnknown なら止まること**（Blocks を見よ）。
// 戻り値の2つ目: 読めた覚え書き（**LockStateNotRunning と、読めなかったときはゼロ値**）。
// 戻り値の3つ目: 読めなかった・壊れていた理由。**LockStateUnknown のときだけ非 nil。**
// **「無い」はエラーにしない**（それは LockStateNotRunning である）。
func ReadLockState(lockPath string) (LockState, LockInfo, error) {
	path := LockInfoPath(lockPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return LockStateNotRunning, LockInfo{}, nil
		}
		return LockStateUnknown, LockInfo{}, i18n.Errorf(i18n.KeyInstanceLockInfoReadFailed, path, err)
	}

	var info LockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return LockStateUnknown, LockInfo{}, i18n.Errorf(i18n.KeyInstanceLockInfoBroken, path, err)
	}
	if info.PID <= 0 {
		return LockStateUnknown, info, i18n.Errorf(i18n.KeyInstanceLockInfoNoPID, path)
	}

	alive, err := processAlive(info.PID)
	if err != nil {
		return LockStateUnknown, info, i18n.Errorf(i18n.KeyInstanceLockInfoPIDUnknown, info.PID, path, err)
	}
	if alive {
		return LockStateRunning, info, nil
	}
	return LockStateStale, info, nil
}

// processAlive は、その PID のプロセスが存在するかを返す。
//
// **`signal 0` を送る。**送らずにプロセスの一覧を引くと、実行ファイル名の照合になり、
// **hook を届けるサブコマンド（`continuo hook ...`）が本体と同じ名前で
// 一瞬だけ現れる**ので誤って一致する（設計 3-17）。
//
// **`EPERM` は「居る」である。**別の利用者のプロセスが同じ PID を持っている場合に返る。
// **居ないことの証拠ではないので、生きている側へ倒す**（設計 3-17i の「分からないなら止まる」）。
//
// **PID の使い回しは判別できない。**覚え書きは手放す前に消すので、
// **残っているのは異常終了したときだけである。**そこで PID が使い回されていれば
// `LockStateRunning` に倒れるが、**倒れる先は「止まる」なので安全側である。**
// 人間は覚え書きのパスと PID を画面で読めるので、確かめて消せる。
//
// pid: 調べる PID（正の値であること）。
// 戻り値の1つ目: 居れば true。
// 戻り値の2つ目: 生死を確かめられなかった場合のエラー。
func processAlive(pid int) (bool, error) {
	// **os.FindProcess は Unix では必ず成功する**ので、これだけでは判定にならない。
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, err
	}
	err = proc.Signal(syscall.Signal(0))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrProcessDone), errors.Is(err, syscall.ESRCH):
		return false, nil
	case errors.Is(err, syscall.EPERM):
		return true, nil
	default:
		return false, err
	}
}
