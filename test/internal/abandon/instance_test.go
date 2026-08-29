package abandon_test

import (
	"errors"
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

// abandonShortHome は、ホームディレクトリの代わりに使う短い一時ディレクトリを作り、
// `HOME` をそこへ向ける。
//
// **短くなければならない。**`--id` を付けたときの socket は
// `~/.continuo/id/<名前>/run/hooks.sock` であり、103バイトに収まらないと決められない
// （設計 3-17d / 3-23）。**macOS の `TMPDIR` はそれだけで66文字前後ある。**
//
// t: 呼び出し元のテスト。
// 戻り値: 実体のパス（symlink を解決済み）。
func abandonShortHome(t *testing.T) string {
	t.Helper()

	for _, base := range []string{"/tmp", ""} {
		dir, err := os.MkdirTemp(base, "ca")
		if err != nil {
			continue
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			resolved = dir
		}
		t.Setenv("HOME", resolved)
		return resolved
	}
	t.Fatal("一時ディレクトリを作れません")
	return ""
}

// 目的: `continuo abandon` が、常駐している側と同じ場所を見ることを確かめる（設計 3-17c）。
//
// **ロックの場所を読み違えると、動いている continuo を「動いていない」と判定して
// worktree を消す。****ロックだけを揃えても足りない。**`--id e2e` で動かしていれば
// worktree は `<workspace.root>/e2e/…` にあるのに、既定の置き場所を走査すると0件になり、
// **手を離させた run を消せないまま終わる。**
//
// 与える情報: `--id e2e` と、`Deps.LockPath` も `Deps.Workspace` も埋めていない入力。
// 成功条件: ロック・実行時ディレクトリ・worktree の置き場所の3つが、
// **`internal/instance` の Layout から導いたものと1バイトも違わないこと。**
func TestAbandon_idを渡すと常駐している側と同じ場所を見る(t *testing.T) {
	fx := newFixture(t)
	home := abandonShortHome(t)

	// **外部のコマンドを1つも見つけさせない。**残った branch を探す段が `ghq` を
	// 起動しうるので、実行のたびに結果が変わらないようにする。
	// **この試験が見るのは「どこを見に行ったか」だけである。**
	t.Setenv("PATH", filepath.Join(fx.Root, "no-such-bin"))

	layout, err := instance.Resolve("e2e")
	if err != nil {
		t.Fatalf("--id から置き場所を決められない: %v", err)
	}

	var lockedPath string
	fx.Run(t, 999, func(opts *abandon.Options) {
		opts.ID = "e2e"
		opts.DryRun = true
		// **埋めない。**設定から本物を組み立てる経路を通さないと、
		// 常駐している側と同じ関数を呼んでいるかを確かめられない。
		opts.Deps.LockPath = ""
		opts.Deps.Workspace = nil
		opts.Deps.AcquireLock = func(path string) (abandon.Unlocker, error) {
			lockedPath = path
			return nil, errLockCaptured
		}
	})

	if want := layout.LockPath(); lockedPath != want {
		t.Errorf("常駐している側と別のロックを見ている: got %q, want %q", lockedPath, want)
	}
	if want := filepath.Join(home, ".continuo", "id", "e2e", "continuo.lock"); lockedPath != want {
		t.Errorf("名前ごとのロックを見ていない: got %q, want %q", lockedPath, want)
	}

	if info, err := os.Stat(layout.RuntimeDir()); err != nil || !info.IsDir() {
		t.Errorf("名前ごとの実行時ディレクトリ %q を用意していない: %v", layout.RuntimeDir(), err)
	}

	wantRoot := filepath.Join(fx.Config.Workspace.Root, "e2e")
	if info, err := os.Stat(wantRoot); err != nil || !info.IsDir() {
		t.Errorf("名前ごとの worktree の置き場所 %q を見ていない: %v", wantRoot, err)
	}
}

// 目的: `--id` を渡さなければ既定の1本を見ることを確かめる（設計 3-17c）。
//
// 与える情報: `--id` を渡していない入力。
// 成功条件: ロックが `~/.continuo/continuo.lock` になること。
func TestAbandon_idを渡さなければ既定の1本を見る(t *testing.T) {
	fx := newFixture(t)
	home := abandonShortHome(t)

	t.Setenv("PATH", filepath.Join(fx.Root, "no-such-bin"))

	var lockedPath string
	fx.Run(t, 999, func(opts *abandon.Options) {
		opts.DryRun = true
		opts.Deps.LockPath = ""
		opts.Deps.AcquireLock = func(path string) (abandon.Unlocker, error) {
			lockedPath = path
			return nil, errLockCaptured
		}
	})

	if want := filepath.Join(home, ".continuo", "continuo.lock"); lockedPath != want {
		t.Errorf("既定のロックを見ていない: got %q, want %q", lockedPath, want)
	}
}
