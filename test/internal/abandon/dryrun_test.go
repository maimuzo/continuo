package abandon_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/abandon"
	"github.com/maimuzo/continuo/internal/instance"
	"github.com/maimuzo/continuo/internal/workspace"
)

// 目的: `continuo abandon --dry-run` がファイルシステムを1つも変えないことを確かめる
// （設計 3-17g）。
//
// **[README.md](../../../README.md) は「`--dry-run` writes nothing at all」と書いている。**
// **実際には5つ作っていた。**
//
//	~/.continuo/id/typo/run                      HookSocketPath → socketpath.EnsureDir
//	<workspace.root>/typo                        workspace.New → EnsureRoot
//	<workspace.root>/typo/.continuo-instance     WriteInstanceMarker
//	~/.continuo/id/typo                          EnsureLockDir
//	~/.continuo/board                            BoardLockPath
//
// **そのうえで「--dry-run なので何も消していません」と表示していた。**
// **とくに悪いのは名乗り（`.continuo-instance`）である。**打ち間違えた `--id` の
// 置き場所がそこで名乗り、**既定側の走査から永久に隠れる。**
//
// 与える情報: 1度も使ったことのない `--id typo` と、外部へ繋ぐ処理を1つも差し替えない入力。
// 成功条件: 5つのどれも作られていないこと。**`~/.continuo` そのものも作られないこと。**
func TestAbandon_dryRunはファイルシステムを1つも変えない(t *testing.T) {
	fx := newFixture(t)

	// **外部のコマンドを1つも見つけさせない。**残った branch を探す段が `ghq` を
	// 起動しうるので、実行のたびに結果が変わらないようにする。
	t.Setenv("PATH", filepath.Join(fx.Root, "no-such-bin"))

	layout, err := instance.Resolve("typo")
	if err != nil {
		t.Fatalf("--id から置き場所を決められない: %v", err)
	}

	// **本物を組み立てる経路を通す。**差し替えると、作っているのがどこかを確かめられない。
	code := fx.Run(t, 999, func(opts *abandon.Options) {
		opts.DryRun = true
		opts.Instance = &layout
		opts.Deps = abandon.Deps{}
	})
	if code != abandon.ExitOK {
		t.Fatalf("--dry-run が途中で止まった（作らないことを確かめられない）: 終了コード %d\n%s",
			code, fx.Output())
	}

	idRoot := filepath.Join(fx.Config.Workspace.Root, "typo")
	for _, path := range []string{
		filepath.Join(fx.Home, instance.DirName),
		filepath.Join(fx.Home, instance.DirName, instance.IDDirName, "typo"),
		filepath.Join(fx.Home, instance.DirName, instance.IDDirName, "typo", instance.RunDirName),
		filepath.Join(fx.Home, instance.DirName, instance.BoardDirName),
		layout.LockPath(),
		idRoot,
		filepath.Join(idRoot, workspace.InstanceMarkerName),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("--dry-run が %s を作っている（err=%v）", path, err)
		}
	}
}

// 目的: `--dry-run` でもロックが握られていれば「動いています」と答えることを確かめる
// （設計 3-17g）。
//
// **作らずに見るだけにしたので、判定まで落ちていないかを確かめる必要がある。**
// **置き場所が無いことは「その continuo が1度も動いていない」ことであり、
// 「握られていない」が正しい答えである。**
//
// 与える情報: 先に握った既定のロックと、ロックファイルの無い状態。
// 成功条件: 握っていれば「動いています」、無ければ「動いていません」と出ること。
func TestAbandon_dryRunでも動いているかは正しく答える(t *testing.T) {
	fx := newFixture(t)
	fx.Prepare(t, 42)

	// **握られていないときは「動いていません」。**
	if code := fx.Run(t, 42, func(opts *abandon.Options) { opts.DryRun = true }); code != abandon.ExitOK {
		t.Fatalf("ロックが空いているのに止まった: 終了コード %d\n%s", code, fx.Output())
	}
	if !containsRunningNotice(fx.Output(), false) {
		t.Fatalf("動いていないことを出していない:\n%s", fx.Output())
	}

	// **握られているときは「動いています」。**
	holdLock(t, fx)

	fx.Out.Reset()
	fx.Err.Reset()
	if code := fx.Run(t, 42, func(opts *abandon.Options) { opts.DryRun = true }); code != abandon.ExitOK {
		t.Fatalf("--dry-run が止まった: 終了コード %d\n%s", code, fx.Output())
	}
	if !containsRunningNotice(fx.Output(), true) {
		t.Fatalf("動いていることを出していない:\n%s", fx.Output())
	}
}

// containsRunningNotice は、継続監視の生死をどう出したかを見る。
//
// **この package には TestMain が無いので、既定の言語（英語）で文言が出る。**
// **日本語でも通るようにしておく。**言語を変えただけで落ちる検査にしない。
//
// out: abandon が出した全文。
// running: 「動いている」と出ているはずかどうか。
// 戻り値: 期待どおりの文言が出ていれば true。
func containsRunningNotice(out string, running bool) bool {
	if running {
		return strings.Contains(out, "continuo is running") ||
			strings.Contains(out, "continuo が動いています")
	}
	return strings.Contains(out, "continuo is not running") ||
		strings.Contains(out, "continuo は動いていません")
}

// 目的: 覚え書きを読めないときに、`--dry-run` が「動いていない」と答えないことを確かめる
// （設計 3-17i）。
//
// **覚え書きは「書けなくても起動を止めない」ものである。**だから読めないことは
// 「動いていない」の証拠にならない。**3値しか無かったので、ここが `not_running` に
// 丸められ、生きている continuo の worktree を「消せる」と報告していた。**
//
// 与える情報: JSON として壊れているロックの覚え書き。
// 成功条件: 止まること（`ExitStopped`）と、「動いていません」と言わないこと。
func TestAbandon_dryRunは覚え書きを読めなければ止まる(t *testing.T) {
	fx := newFixture(t)
	fx.Prepare(t, 42)

	if err := os.MkdirAll(filepath.Dir(fx.LockPath), 0o700); err != nil {
		t.Fatalf("ロックの置き場所を作れません: %v", err)
	}
	if err := os.WriteFile(instance.LockInfoPath(fx.LockPath), []byte("{ pid: "), 0o600); err != nil {
		t.Fatalf("壊れた覚え書きを置けません: %v", err)
	}

	code := fx.Run(t, 42, func(opts *abandon.Options) { opts.DryRun = true })

	if code != abandon.ExitStopped {
		t.Fatalf("覚え書きを読めないのに止まらなかった: 終了コード %d\n%s", code, fx.Output())
	}
	if containsRunningNotice(fx.Output(), false) {
		t.Fatalf("覚え書きを読めないのに「動いていません」と言っている:\n%s", fx.Output())
	}
}

// 目的: 残骸の覚え書き（死んだ PID）なら進み、残骸があることを画面に出すことを確かめる
// （設計 3-17i）。
//
// **`stale` を `not_running` と同じ言葉で表示しない。**残骸が残っていることは、
// 前の continuo が正常に終わらなかった合図である。**黙って進むと、次に同じことが起きても
// 気づけない。**
//
// 与える情報: 終了済みのプロセスの PID を書いた覚え書き。
// 成功条件: 進むこと（`ExitOK`）と、残骸があることが出ていること。
func TestAbandon_dryRunは残骸の覚え書きを見つけたら言う(t *testing.T) {
	fx := newFixture(t)
	fx.Prepare(t, 42)

	if err := os.MkdirAll(filepath.Dir(fx.LockPath), 0o700); err != nil {
		t.Fatalf("ロックの置き場所を作れません: %v", err)
	}
	if err := instance.WriteLockInfo(fx.LockPath, instance.LockInfo{PID: deadPID(t)}, nil); err != nil {
		t.Fatalf("覚え書きを置けません: %v", err)
	}

	code := fx.Run(t, 42, func(opts *abandon.Options) { opts.DryRun = true })

	if code != abandon.ExitOK {
		t.Fatalf("残骸なのに止まった: 終了コード %d\n%s", code, fx.Output())
	}
	out := fx.Output()
	if !strings.Contains(out, "leftover note") && !strings.Contains(out, "正常に終わらなかった跡") {
		t.Fatalf("残骸があることを出していない:\n%s", out)
	}
}

// deadPID は、確実に終了している子プロセスの PID を返す。
//
// **`sh -c "exit 0"` を起動して待つ。**終了を待ってから PID を返すので、
// **そのプロセスは必ず居ない。**
//
// t: 呼び出し元のテスト。
// 戻り値: 終了済みのプロセスの PID。
func deadPID(t *testing.T) int {
	t.Helper()
	proc, err := os.StartProcess("/bin/sh", []string{"sh", "-c", "exit 0"}, &os.ProcAttr{})
	if err != nil {
		t.Fatalf("子プロセスを起動できない: %v", err)
	}
	pid := proc.Pid
	if _, err := proc.Wait(); err != nil {
		t.Fatalf("子プロセスを待てない: %v", err)
	}
	return pid
}
