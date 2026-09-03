package abandon_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/abandon"
	"github.com/maimuzo/continuo/internal/instance"
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

// 目的: `continuo abandon --id` が、常駐している側と同じロックを見ることを確かめる
// （設計 3-17b）。
//
// **ロックの場所を読み違えると、動いている continuo を「動いていない」と判定して
// worktree を消す。**`--id e2e` で動いている continuo は
// `~/.continuo/id/e2e/continuo.lock` を握っており、既定の
// `~/.continuo/continuo.lock` は空いているからである。
//
// 与える情報: `--id e2e` と、`Deps.LockPath` を埋めていない入力。
// 成功条件: 取りに行ったロックが、`internal/instance` の Layout から導いたものと
// **1バイトも違わないこと。**
func TestAbandon_idを渡すと常駐している側と同じロックを見る(t *testing.T) {
	fx := newFixture(t)
	home := abandonHome(t)

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
		opts.Instance = &layout
		// **埋めない。**設定から本物を組み立てる経路を通さないと、
		// 常駐している側と同じ関数を呼んでいるかを確かめられない。
		opts.Deps.LockPath = ""
		opts.Deps.AcquireLock = func(path string) (abandon.Unlocker, error) {
			lockedPath = path
			return nil, errLockCaptured
		}
	})

	if want := layout.LockPath(); lockedPath != want {
		t.Errorf("常駐している側と別のロックを見ている: got %q, want %q", lockedPath, want)
	}
	want := filepath.Join(home, instance.DirName, instance.IDDirName, "e2e", instance.LockFileName)
	if lockedPath != want {
		t.Errorf("名前ごとのロックを見ていない: got %q, want %q", lockedPath, want)
	}
}

// 目的: `--id` を渡さなければ既定の1本を見ることを確かめる（設計 3-17b）。
//
// 与える情報: `--id` を渡していない入力。
// 成功条件: ロックが `~/.continuo/continuo.lock` になること。
func TestAbandon_idを渡さなければ既定の1本を見る(t *testing.T) {
	fx := newFixture(t)
	home := abandonHome(t)

	t.Setenv("PATH", filepath.Join(fx.Root, "no-such-bin"))

	var lockedPath string
	fx.Run(t, 999, func(opts *abandon.Options) {
		opts.Deps.LockPath = ""
		opts.Deps.AcquireLock = func(path string) (abandon.Unlocker, error) {
			lockedPath = path
			return nil, errLockCaptured
		}
	})

	if want := filepath.Join(home, instance.DirName, instance.LockFileName); lockedPath != want {
		t.Errorf("既定のロックを見ていない: got %q, want %q", lockedPath, want)
	}
}

// 目的: `~/.continuo` を一度も作っていない機械でも `continuo abandon` が通ることを確かめる
// （設計 3-17）。
//
// **常駐を1度も起動していない人が、いちばん最初に叩くのがこの経路である。**
// ロックの置き場所を用意しないと、`lock.Acquire` が「ロックファイルを開けません」で落ち、
// **何も消さない `--dry-run` すら通らない。**
//
// **ロックを掴む関数を差し替えない。**差し替えると、置き場所が無いことに気づけない
// （置き場所を見に行くだけのテストは、この落ち方を1件も捕まえられなかった）。
//
// 与える情報: `.continuo` が1つも無いホームディレクトリと `--dry-run`。
// 成功条件: 終了コードが 0 で、「ロックファイルを開けません」が出ていないこと。
func TestAbandon_ロックの置き場所が無い機械でも通る(t *testing.T) {
	fx := newFixture(t)
	home := abandonHome(t)

	t.Setenv("PATH", filepath.Join(fx.Root, "no-such-bin"))

	// **何も無いところから始めることを、先に確かめる。**
	// 既に在ると、この試験は何も見ていないことになる。
	root := filepath.Join(home, instance.DirName)
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("この試験は %s が無い状態から始める必要がある: %v", root, err)
	}

	code := fx.Run(t, 999, func(opts *abandon.Options) {
		opts.DryRun = true
		// **本物の経路でロックの置き場所を決めさせる。**
		opts.Deps.LockPath = ""
	})

	out := fx.Output()
	if strings.Contains(out, "ロックファイル") && strings.Contains(out, "開けません") {
		t.Fatalf("ロックの置き場所を用意していない:\n%s", out)
	}
	if code != abandon.ExitOK {
		t.Fatalf("終了コードが 0 でない: %d\n%s", code, out)
	}

	// **置き場所を作ったことも確かめる。**作らずに通ったなら、次の版で同じ落ち方に戻る。
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("ロックの置き場所を作っていない: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("ロックの置き場所の権限が開いている: got %04o, want 0700", got)
	}
}
