package abandon_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// errLockCaptured は、**ロックをどこに取りに行ったかだけを見て実行を止める**ための番兵である。
//
// **`lock.ErrAlreadyRunning` を包まない。**包むと abandon が「継続監視が動いている」と
// 判断して手を離させる段へ進み、この試験が見たいもの（場所）と関係のない経路を通る。
var errLockCaptured = errors.New("lock path captured")

// abandonHome は、ホームディレクトリの代わりに使う一時ディレクトリを作り、
// `HOME` をそこへ向ける。
//
// **ロックは `~/.continuo` を起点に決まる**（設計 3-17）。
// **向けておかないと、テストが利用者の本物のロックを取り合う。**
//
// t: 呼び出し元のテスト。
// 戻り値: 実体のパス（symlink を解決済み）。
func abandonHome(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "ca")
	if err != nil {
		t.Fatalf("一時ディレクトリを作れません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if resolved, rerr := filepath.EvalSymlinks(dir); rerr == nil {
		dir = resolved
	}
	t.Setenv("HOME", dir)
	return dir
}
