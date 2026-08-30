package abandon_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/abandon"
	"github.com/maimuzo/continuo/internal/instance"
	"github.com/maimuzo/continuo/internal/lock"
	"github.com/maimuzo/continuo/internal/workspace"
)

// errLockCaptured は、**ロックをどこに取りに行ったかだけを見て実行を止める**ための番兵である。
//
// **`lock.ErrAlreadyRunning` を包まない。**包むと abandon が「継続監視が動いている」と
// 判断して手を離させる段へ進み、この試験が見たいもの（場所）と関係のない経路を通る。
var errLockCaptured = errors.New("lock path captured")

// 目的: `continuo abandon` が、常駐している側と同じ場所を見ることを確かめる（設計 3-17c）。
//
// **ロックの場所を読み違えると、動いている continuo を「動いていない」と判定して
// worktree を消す。****ロックだけを揃えても足りない。**`--id e2e` で動かしていれば
// worktree は `<workspace.root>/e2e/…` にあるのに、既定の置き場所を走査すると0件になり、
// **手を離させた run を消せないまま終わる。**
//
// **`--dry-run` では走らせない。**あちらは置き場所を1つも作らない（設計 3-17g）ので、
// 「用意したか」を見るこの検査には使えない。
//
// 与える情報: `--id e2e` と、`Deps.LockPath` も `Deps.Workspace` も埋めていない入力。
// 成功条件: ロック・実行時ディレクトリ・worktree の置き場所の3つが、
// **`internal/instance` の Layout から導いたものと1バイトも違わないこと。**
func TestAbandon_idを渡すと常駐している側と同じ場所を見る(t *testing.T) {
	fx := newFixture(t)
	home := fx.Home

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
	home := fx.Home

	t.Setenv("PATH", filepath.Join(fx.Root, "no-such-bin"))

	var lockedPath string
	fx.Run(t, 999, func(opts *abandon.Options) {
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

// 目的: `--id` を1度使ったあとでも、既定の `continuo abandon` が普通に片付けられることを
// 確かめる（設計 3-17f）。
//
// **`--id e2e` の worktree は `<workspace.root>/e2e/<host>/<owner>/<repo>/<スラグ>` にある。**
// 既定側の走査は `<workspace.root>` からちょうど4階層を返すので、名乗りが無ければ
// `<workspace.root>/e2e/<host>/<owner>/<repo>` を「身元ファイルが無いディレクトリ」として
// 拾い、**判断を保留したまま `ExitStopped` で止まる。**
// **そうなると、既定側は二度と何も片付けられない。**
//
// 与える情報: `--id e2e` の Manager が用意した worktree と、既定側で用意した worktree。
// 成功条件: 既定側の実行が `ExitOK` で終わり、保留の文言を出さないこと。
// **名乗りを消すと止まること**（消しても通るなら、この検査は何も守っていない）。
func TestAbandon_idを使ったあとも既定の片付けが動く(t *testing.T) {
	fx := newFixture(t)

	// **常駐している側と同じ経路で `--id e2e` の置き場所を作る**（設計 3-17b）。
	layout, err := instance.Resolve("e2e")
	if err != nil {
		t.Fatalf("--id から置き場所を決められない: %v", err)
	}
	// **`~/.continuo/id/e2e/` も作る。**走査はここの実在を裏付けとして見る（3-17f）。
	// 常駐している側は起動の段2 でこれを通る。
	if err := layout.EnsureLockDir(); err != nil {
		t.Fatalf("--id e2e のロックの置き場所を作れません: %v", err)
	}
	idCfg := layout.Apply(fx.Config)
	idMgr, err := workspace.New(workspace.Options{
		Config:       idCfg,
		Herdr:        fx.Herdr.Client(),
		HomeDir:      fx.Home,
		GhqList:      func(_ context.Context, _, _ string) (string, error) { return fx.Repo.Dir, nil },
		SettingsRoot: filepath.Join(fx.Root, "issues-e2e"),
		InstanceID:   layout.ID(),
	})
	if err != nil {
		t.Fatalf("--id e2e の worktree の置き場所を作れません: %v", err)
	}
	if _, err := idMgr.Prepare(context.Background(), issueRef(777)); err != nil {
		t.Fatalf("--id e2e の worktree を用意できません: %v", err)
	}

	// **既定側に worktree が無い issue を片付けさせる。**保留が1件でもあると
	// 「この issue の worktree はありません」へ進めず、`ExitStopped` で止まる。
	// **`--id` の置き場所を数えてしまうのは、まさにこの経路である。**
	code := fx.Run(t, 999, func(opts *abandon.Options) { opts.DryRun = true })
	if code != abandon.ExitOK {
		t.Fatalf("--id を使ったあとに既定の片付けが止まった: 終了コード %d\n%s", code, fx.Output())
	}
	if strings.Contains(fx.Output(), idCfg.Workspace.Root) {
		t.Fatalf("--id の置き場所 %s を既定側が数えている:\n%s", idCfg.Workspace.Root, fx.Output())
	}

	// **名乗りを消すと止まる。**ここが通ってしまうなら、上の検査は名乗りを確かめていない。
	if err := os.Remove(filepath.Join(idCfg.Workspace.Root, workspace.InstanceMarkerName)); err != nil {
		t.Fatalf("名乗りを消せません: %v", err)
	}
	// **同じ一式でもう一度走らせる。**上の実行は `--dry-run` なので何も消していない。
	fx.Out.Reset()
	fx.Err.Reset()
	if code := fx.Run(t, 999, func(opts *abandon.Options) { opts.DryRun = true }); code != abandon.ExitStopped {
		t.Fatalf("名乗りが無いのに止まらなかった（検査が空振りしている）: 終了コード %d\n%s",
			code, fx.Output())
	}
}

// 目的: `~/.continuo/id/<名前>/` が無ければ、名乗りがあっても走査から外さないことを
// 確かめる（設計 3-17f）。
//
// **worktree の中ではエージェントが `--permission-mode dontAsk` で動く。**
// **名乗りのファイルは、そこから相対パスで書ける。**中身を見ずに飛ばすと、
// **1つ置くだけで `continuo abandon` の目から worktree を隠せる。**
//
// 与える情報: `--id e2e` の置き場所と名乗りだけを作り、`~/.continuo/id/e2e/` は作らない状態。
// 成功条件: いつもどおり数え、判断を保留して止まること。
func TestAbandon_名乗りだけでは走査から外さない(t *testing.T) {
	fx := newFixture(t)

	layout, err := instance.Resolve("e2e")
	if err != nil {
		t.Fatalf("--id から置き場所を決められない: %v", err)
	}
	// **`EnsureLockDir` は呼ばない。**その名前で continuo が動いた証拠が無い状態を作る。
	idCfg := layout.Apply(fx.Config)
	idMgr, err := workspace.New(workspace.Options{
		Config:       idCfg,
		Herdr:        fx.Herdr.Client(),
		HomeDir:      fx.Home,
		GhqList:      func(_ context.Context, _, _ string) (string, error) { return fx.Repo.Dir, nil },
		SettingsRoot: filepath.Join(fx.Root, "issues-e2e"),
		InstanceID:   layout.ID(),
	})
	if err != nil {
		t.Fatalf("--id e2e の worktree の置き場所を作れません: %v", err)
	}
	if _, err := idMgr.Prepare(context.Background(), issueRef(777)); err != nil {
		t.Fatalf("--id e2e の worktree を用意できません: %v", err)
	}
	if _, err := os.Stat(filepath.Join(idCfg.Workspace.Root, workspace.InstanceMarkerName)); err != nil {
		t.Fatalf("名乗りが置かれていない（検査の前提が崩れている）: %v", err)
	}

	if code := fx.Run(t, 999, func(opts *abandon.Options) { opts.DryRun = true }); code != abandon.ExitStopped {
		t.Fatalf("名乗りだけで走査から外してしまった: 終了コード %d\n%s", code, fx.Output())
	}
}

// 目的: `--id` を付け忘れた `continuo abandon` を、ボードのロックで止めることを確かめる
// （設計 3-17e）。
//
// **ロックは `--id` ごとに分かれるので、自分のロックが空いていることは
// 「continuo が動いていない」ことを意味しない。**`--id e2e` で動いている continuo は
// `~/.continuo/id/e2e/continuo.lock` を握っており、既定の `~/.continuo/continuo.lock` は
// 空いている。**ボードのロックは `--id` に依らない唯一の合図である。**
//
// 与える情報: 先に握られたボードのロック。
// 成功条件: 何も消さずに `ExitStopped` で止まり、ロックのパスと `--id` の案内を出すこと。
func TestAbandon_同じボードを見ている継続監視が居たら止まる(t *testing.T) {
	fx := newFixture(t)

	held, err := lock.Acquire(fx.BoardLockPath)
	if err != nil {
		t.Fatalf("ボードのロックを先に握れません: %v", err)
	}
	t.Cleanup(func() { _ = held.Release() })

	prepared := fx.Prepare(t, 42)

	if code := fx.Run(t, 42, nil); code != abandon.ExitStopped {
		t.Fatalf("同じボードを見ている continuo が居るのに止まらなかった: 終了コード %d\n%s",
			code, fx.Output())
	}
	if !strings.Contains(fx.Output(), fx.BoardLockPath) {
		t.Fatalf("ボードのロックのパスを出していない:\n%s", fx.Output())
	}
	if !strings.Contains(fx.Output(), "--id") {
		t.Fatalf("同じ --id を付けるよう案内していない:\n%s", fx.Output())
	}
	if _, err := os.Stat(prepared.Path); err != nil {
		t.Fatalf("止まったのに worktree を消している: %v", err)
	}
}

// 目的: 覚え書きが無いときに、無いファイルを読めと案内しないことを確かめる（設計 3-17e）。
//
// **覚え書き（ロックの隣の JSON）を書くのは常駐している continuo だけである。**
// **`continuo abandon` どうしがぶつかったときは、そのファイルは存在しない。**
// それでも「誰が握っているかは %s に書いてあります」と案内していたので、
// **読みに行った人は、無いファイルを探すことになった。**
//
// 与える情報: 覚え書きの無いボードのロックを、先に握った状態。
// 成功条件: 覚え書きのパスを出さず、`continuo abandon` が同時に動いている可能性を伝えること。
func TestAbandon_覚え書きが無ければそれを読めと言わない(t *testing.T) {
	fx := newFixture(t)

	held, err := lock.Acquire(fx.BoardLockPath)
	if err != nil {
		t.Fatalf("ボードのロックを先に握れません: %v", err)
	}
	t.Cleanup(func() { _ = held.Release() })

	infoPath := instance.BoardInfoPath(fx.BoardLockPath)
	if _, err := os.Stat(infoPath); !os.IsNotExist(err) {
		t.Fatalf("覚え書きが在ってはならない（検査の前提が崩れている）: %v", err)
	}

	fx.Prepare(t, 42)
	if code := fx.Run(t, 42, nil); code != abandon.ExitStopped {
		t.Fatalf("同じボードを見ている continuo が居るのに止まらなかった: 終了コード %d\n%s",
			code, fx.Output())
	}
	if strings.Contains(fx.Output(), infoPath) {
		t.Fatalf("無いファイル %s を読めと案内している:\n%s", infoPath, fx.Output())
	}
	if !strings.Contains(fx.Output(), "abandon") {
		t.Fatalf("もう1つの continuo abandon を疑うよう案内していない:\n%s", fx.Output())
	}

	// **覚え書きが在るときは、いままでどおり名指しする。**
	// ここを確かめないと、いつでも案内しない実装でも通ってしまう。
	if err := instance.WriteBoardInfo(fx.BoardLockPath,
		instance.BoardInfo{Owner: "octocat", ProjectNumber: 3}, nil); err != nil {
		t.Fatalf("覚え書きを書けません: %v", err)
	}
	fx.Out.Reset()
	fx.Err.Reset()
	if code := fx.Run(t, 42, nil); code != abandon.ExitStopped {
		t.Fatalf("同じボードを見ている continuo が居るのに止まらなかった: 終了コード %d\n%s",
			code, fx.Output())
	}
	if !strings.Contains(fx.Output(), infoPath) {
		t.Fatalf("覚え書きが在るのに名指ししていない:\n%s", fx.Output())
	}
}

// 目的: ボードのロックが空いていれば、いままでどおり片付けが進むことを確かめる
// （設計 3-17e）。
//
// **上の検査だけでは、いつでも止まる実装でも通ってしまう。**
//
// 与える情報: 誰も握っていないボードのロック。
// 成功条件: `--dry-run` が `ExitOK` で終わること。
func TestAbandon_ボードのロックが空いていれば進む(t *testing.T) {
	fx := newFixture(t)

	fx.Prepare(t, 42)

	if code := fx.Run(t, 42, func(opts *abandon.Options) { opts.DryRun = true }); code != abandon.ExitOK {
		t.Fatalf("ボードのロックが空いているのに止まった: 終了コード %d\n%s", code, fx.Output())
	}
}
