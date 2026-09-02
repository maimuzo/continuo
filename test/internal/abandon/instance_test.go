package abandon_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/maimuzo/continuo/internal/abandon"
	"github.com/maimuzo/continuo/internal/instance"
)

// errLockCaptured は、**ロックをどこに取りに行ったかだけを見て実行を止める**ための番兵である。
//
// **`lock.ErrAlreadyRunning` を包まない。**包むと abandon が「継続監視が動いている」と
// 判断して手を離させる段へ進み、この試験が見たいもの（場所）と関係のない経路を通る。
var errLockCaptured = errors.New("lock path captured")

// captureLockPath は、abandon が実際に取りに行ったロックのパスを返す。
//
// **`Deps.LockPath` を空にして走らせる。**埋めてしまうと、設定から本物を組み立てる経路を
// 通らないので、常駐している側と同じ関数を呼んでいるかを確かめられない。
//
// t: 呼び出し元のテスト。
// fx: 用意済みの一式。
// mutate: 入力を書き換える関数（nil 可）。
// 戻り値: abandon が取りに行ったロックの絶対パス。
func captureLockPath(t *testing.T, fx *fixture, mutate func(opts *abandon.Options)) string {
	t.Helper()

	// **外部のコマンドを1つも見つけさせない。**残った branch を探す段が `ghq` を
	// 起動しうるので、実行のたびに結果が変わらないようにする。
	// **この試験が見るのは「どこを見に行ったか」だけである。**
	t.Setenv("PATH", filepath.Join(fx.Root, "no-such-bin"))

	var lockedPath string
	fx.Run(t, 999, func(opts *abandon.Options) {
		opts.Deps.LockPath = ""
		opts.Deps.AcquireLock = func(path string) (abandon.Unlocker, error) {
			lockedPath = path
			return nil, errLockCaptured
		}
		if mutate != nil {
			mutate(opts)
		}
	})
	return lockedPath
}

// 目的: `continuo abandon --id <名前>` が、常駐している側と同じロックを見ることを
// 確かめる（設計 3-17c）。
//
// **ロックの場所を読み違えると、動いている continuo を「動いていない」と判定して
// worktree を消す。**
//
// 与える情報: `--id e2e` と、`Deps.LockPath` を埋めていない入力。
// 成功条件: 取りに行くロックが `<HOME>/.continuo/id/e2e/continuo.lock` であり、
// **`internal/instance` の Layout が返すものと1バイトも違わないこと。**
func TestAbandon_idを渡すと常駐している側と同じロックを見る(t *testing.T) {
	fx := newFixture(t)
	home := filepath.Join(fx.Root, "home")
	t.Setenv("HOME", home)

	layout, err := instance.Resolve("e2e")
	if err != nil {
		t.Fatalf("--id から置き場所を決められない: %v", err)
	}

	got := captureLockPath(t, fx, func(opts *abandon.Options) { opts.Instance = &layout })

	if want := layout.LockPath(); got != want {
		t.Errorf("常駐している側と別のロックを見ている: got %q, want %q", got, want)
	}
	if want := filepath.Join(home, ".continuo", "id", "e2e", "continuo.lock"); got != want {
		t.Errorf("名前ごとのロックを見ていない: got %q, want %q", got, want)
	}
}

// 目的: `--id` を渡さなければ既定の1本を見ることを確かめる（設計 3-17c）。
//
// 与える情報: `--id` を渡していない入力。
// 成功条件: ロックが `<HOME>/.continuo/continuo.lock` になること。
func TestAbandon_idを渡さなければ既定の1本を見る(t *testing.T) {
	fx := newFixture(t)
	home := filepath.Join(fx.Root, "home")
	t.Setenv("HOME", home)

	got := captureLockPath(t, fx, nil)

	if want := filepath.Join(home, ".continuo", "continuo.lock"); got != want {
		t.Errorf("既定のロックを見ていない: got %q, want %q", got, want)
	}
}

// 目的: `runtime.lock_file` を書いてあれば abandon もそれを見ること、
// **`--id` を渡したときは `--id` が勝つこと**を確かめる（設計 3-17）。
//
// **常駐している側と優先順位が食い違ってはならない。**食い違うと、abandon が
// 空いているほうのロックを見て「動いていない」と判定し、worktree を消しにいく。
//
// 与える情報: `runtime.lock_file` を書いた `WORKFLOW.md` と、`--id` の有無。
// 成功条件: `--id` 無しなら設定に書いたパス、`--id e2e` なら名前ごとのロックを見ること。
func TestAbandon_lock_fileよりidが勝つ(t *testing.T) {
	root, err := os.MkdirTemp("", "cabl")
	if err != nil {
		t.Fatalf("一時ディレクトリを作れません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	configured := filepath.Join(root, "configured.lock")

	fx := newFixtureWithConfig(t, fmt.Sprintf("runtime:\n  lock_file: %s\n", configured))
	home := filepath.Join(fx.Root, "home")
	t.Setenv("HOME", home)

	if got := captureLockPath(t, fx, nil); got != configured {
		t.Errorf("runtime.lock_file を見ていない: got %q, want %q", got, configured)
	}

	layout, err := instance.Resolve("e2e")
	if err != nil {
		t.Fatalf("--id から置き場所を決められない: %v", err)
	}
	got := captureLockPath(t, fx, func(opts *abandon.Options) { opts.Instance = &layout })
	if want := layout.LockPath(); got != want {
		t.Errorf("--id が runtime.lock_file に負けている: got %q, want %q", got, want)
	}
}
