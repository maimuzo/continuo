// Package lock は continuo の二重起動を flock(2) 1本で防ぐ（docs/plans/continuo_design.md 3-17）。
//
// ps で自分自身を探す方式は採らない。hook を届けるサブコマンド（`continuo hook ...`）が
// 本体と同じ実行ファイル名で起動するため、実行ファイル名の照合では「turn が終わるたびに
// 一瞬だけ現れるこのプロセス」に誤って一致してしまう（3-17）。
package lock

import (
	"errors"
	"os"
	"syscall"

	"github.com/maimuzo/continuo/internal/i18n"
)

// ErrAlreadyRunning は、別のプロセスが同じロックファイルを掴んでいることを表す。
//
// 「ロックファイルを開けない」（runtime.lock_file の親ディレクトリが無い・権限が足りない・
// パスがディレクトリを指している）と区別するために置いてある。両方を同じ文言で報告すると、
// パスを打ち間違えた運用者が「起動しているはずのない2つ目のプロセス」を探すことになる。
// 呼び出し側は errors.Is(err, lock.ErrAlreadyRunning) で切り分けること。
var ErrAlreadyRunning = errors.New("continuo は既に起動しています")

// Lock は flock で獲得した単一プロセスロックを表す。
type Lock struct {
	file *os.File
}

// Acquire は path にあるロックファイルを開き（無ければ作り）、flock(2) の
// LOCK_EX | LOCK_NB で排他ロックを試みる。
//
// path: ロックファイルの絶対パス。親ディレクトリは呼び出し側が事前に作成しておくこと。
// 戻り値: ロックを獲得できれば *Lock を返す。既に別プロセスが同じファイルを
// ロックしている場合は ErrAlreadyRunning を包んだエラーを返す。
// ファイルを開けない場合もエラーを返すが、そちらは ErrAlreadyRunning を包まない
// （設定の誤りと二重起動を呼び出し側が言い分けられるようにするため）。
//
// プロセスが（正常終了であれクラッシュであれ）終了すると OS がロックを自動的に
// 解放するため、「このロックファイルは残骸か本当に使用中か」を判定する処理は
// 一切持たない。これが flock を選ぶ理由そのものである（3-17）。
func Acquire(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyLockAcquireOpenFailed, path, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, i18n.Errorf(i18n.KeyLockAcquireAlreadyRunning, ErrAlreadyRunning, path, err)
	}

	return &Lock{file: f}, nil
}

// Release はロックを解放し、ロックファイルの file descriptor を閉じる。
//
// ロックファイル自体は削除しない。削除すると、削除の直後に別プロセスが同名で
// 新しいファイルを作って先に flock した場合、こちらの Close がその別プロセスの
// ロックを巻き添えで解放してしまう競合が起きるためである。
//
// 戻り値: 解放・クローズに失敗した場合のエラー。l が nil、または既に Release 済みの
// 場合は何もせず nil を返す。
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	f := l.file
	l.file = nil

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		_ = f.Close()
		return i18n.Errorf(i18n.KeyLockReleaseUnlockFailed, err)
	}
	if err := f.Close(); err != nil {
		return i18n.Errorf(i18n.KeyLockReleaseCloseFailed, err)
	}
	return nil
}
