// **ロックの場所の検査だけを集めたファイルである。**
//
// **この検査は「doctor が全項目 ✓ なのに起動だけが落ちる」（issue #9）と同じ形の穴を
// 塞ぐために足された。**ロックは `~/.continuo`（`--id` を付けたなら
// `~/.continuo/id/<名前>`）に機械で固定してあり、**socket の場所からは導かない**（設計 3-17）。
// **別の場所なので、socket が置けてもロックが置けるとは限らない。**
package doctor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/doctor"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/instance"
	"github.com/maimuzo/continuo/internal/lock"
	"github.com/maimuzo/continuo/internal/socketpath"
)

// 目的: 置ける場所ならロックの検査が通ることを確かめる（設計 3-17）。
//
// 与える情報: 一時ディレクトリへ向けたホームディレクトリ。
// 成功条件: `✓` で、説明が `<HOME>/.continuo/continuo.lock` を指すこと。
func TestDoctorLockFile_置ける場所なら通る(t *testing.T) {
	fx := newFixture(t)

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelLockFile, doctor.SymbolOK)
	want := filepath.Join(fx.Home, instance.DirName, instance.LockFileName)
	if !strings.Contains(res.Detail, want) {
		t.Fatalf("固定した場所のロックを見ていない: got %q, want %q を含むこと", res.Detail, want)
	}
}

// 目的: `~/.continuo` を作れない環境を `✗` にすることを確かめる（設計 3-17）。
//
// **ここが無かったとき、doctor は全部 `✓` を出すのに `daemon.Run` が落ちた。**
// **socket の置き場所を確かめても、ロックの置き場所は分からない**（別の場所である）。
//
// 与える情報: `~/.continuo` と同じ名前の**ファイル**（ディレクトリを作れない状態）。
// 成功条件: `✗` になり、終了コードが 1 になること。
func TestDoctorLockFile_ホームに同じ名前のファイルが在れば落とす(t *testing.T) {
	fx := newFixture(t)

	blocker := filepath.Join(fx.Home, instance.DirName)
	if err := os.MkdirAll(fx.Home, 0o700); err != nil {
		t.Fatalf("ホームディレクトリを作れません: %v", err)
	}
	if err := os.WriteFile(blocker, []byte("これはディレクトリではありません\n"), 0o600); err != nil {
		t.Fatalf("邪魔をするファイルを置けません: %v", err)
	}

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelLockFile, doctor.SymbolMissing)
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった", report.ExitCode())
	}
}

// 目的: 既に continuo がロックを握っていても `✓` にすることを確かめる（設計 3-17）。
//
// **握られているのは「動いている」ことであって、場所が使えないことではない。**
//
// 与える情報: テストが先に握った `<HOME>/.continuo/continuo.lock`。
// 成功条件: `✓` になり、説明がそのロックのパスを指すこと。
func TestDoctorLockFile_既にcontinuoが握っていれば通る(t *testing.T) {
	fx := newFixture(t)

	lockPath := filepath.Join(fx.Home, instance.DirName, instance.LockFileName)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatalf("ロックの置き場所を作れません: %v", err)
	}
	held, err := lock.Acquire(lockPath)
	if err != nil {
		t.Fatalf("ロックを先に握れません: %v", err)
	}
	t.Cleanup(func() { _ = held.Release() })

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelLockFile, doctor.SymbolOK)
	if !strings.Contains(res.Detail, lockPath) {
		t.Fatalf("固定した場所のロックを見ていない: got %q, want %q を含むこと", res.Detail, lockPath)
	}
}

// 目的: doctor がロックを取らないことを確かめる（設計 3-17）。
//
// **doctor は「置けるか」を答える道具である。**取っていたときは、doctor が動いている
// あいだ continuo を起動できなかった。**検査の道具が本番の起動を止めてはならない。**
//
// 与える情報: `gh auth status`（ロックの検査より後に走る口）の中でロックを取る差し替え。
// 成功条件: そのロックが取れること。**取れないなら doctor が握っている。**
func TestDoctorLockFile_検査の最中でもロックを取れる(t *testing.T) {
	fx := newFixture(t)
	lockPath := filepath.Join(fx.Home, instance.DirName, instance.LockFileName)
	// **置き場所はテストが用意する。**doctor は本番が使う名前を1つも作らない
	// （設計 3-17h）ので、**用意しないと `lock.Acquire` が「親ディレクトリが無い」で
	// 落ち、握っているかどうかを確かめられない。**
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatalf("ロックの置き場所を作れません: %v", err)
	}

	var acquireErr error
	var tried bool
	opts := fx.Options()
	// **`gh の認証` はロックの検査より後に走る**（internal/doctor の Run の段の順）。
	// **そこで取れれば、doctor はロックを握っていない。**
	opts.GHAuthStatus = func(_ context.Context) (string, error) {
		tried = true
		held, err := lock.Acquire(lockPath)
		if err != nil {
			acquireErr = err
			return "", err
		}
		_ = held.Release()
		return ghAuthStatusWithProject, nil
	}
	doctor.Run(t.Context(), opts)

	if !tried {
		t.Fatal("ロックを取りに行く口が呼ばれていない（検査が空振りしている）")
	}
	if acquireErr != nil {
		t.Fatalf("doctor が検査の最中にロックを握っている: %v", acquireErr)
	}
}

// 目的: ボードのロックの置き場所も doctor が見ることを確かめる（設計 3-17e）。
//
// **`~/.continuo/board` がファイルだと、doctor は全部 `✓` を出すのに起動が落ちた。**
// **`continuo abandon` も同じところで落ちる。**これは issue #9 と同じ形である。
//
// 与える情報: `~/.continuo/board` と同じ名前の**ファイル**。
// 成功条件: 見出し語 `ボードのロック` が `✗` になり、終了コードが 1 になること。
func TestDoctorBoardLock_ホームに同じ名前のファイルが在れば落とす(t *testing.T) {
	fx := newFixture(t)

	blocker := filepath.Join(fx.Home, instance.DirName, instance.BoardDirName)
	if err := os.MkdirAll(filepath.Dir(blocker), 0o700); err != nil {
		t.Fatalf("ロックの置き場所を作れません: %v", err)
	}
	if err := os.WriteFile(blocker, []byte("これはディレクトリではありません\n"), 0o600); err != nil {
		t.Fatalf("邪魔をするファイルを置けません: %v", err)
	}

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelBoardLock, doctor.SymbolMissing)
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった", report.ExitCode())
	}
}

// 目的: ボードのロックの置き場所が symlink なら落とすことを確かめる（設計 3-17e）。
//
// **素の `os.MkdirAll` では、差し替えられていても気づかない。**辿った先へ flock と
// 覚え書きが落ちる。
//
// 与える情報: `~/.continuo/board` を別のディレクトリへ向けた symlink。
// 成功条件: 見出し語 `ボードのロック` が `✗` になること。
func TestDoctorBoardLock_置き場所がsymlinkなら落とす(t *testing.T) {
	fx := newFixture(t)

	target := filepath.Join(fx.Root, "elsewhere")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("差し替え先を作れません: %v", err)
	}
	linkPath := filepath.Join(fx.Home, instance.DirName, instance.BoardDirName)
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o700); err != nil {
		t.Fatalf("ロックの置き場所を作れません: %v", err)
	}
	if err := os.Symlink(target, linkPath); err != nil {
		t.Fatalf("symlink を張れません: %v", err)
	}

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelBoardLock, doctor.SymbolMissing)
}

// 目的: ボードのロックの置き場所が使えれば `✓` になることを確かめる（設計 3-17e）。
//
// **落とす側だけを確かめると、いつでも `✗` を出す実装でも通ってしまう。**
//
// 与える情報: 何も邪魔をしていないホームディレクトリ。
// 成功条件: `✓` になり、説明が `<HOME>/.continuo/board/octocat-3.lock` を指すこと。
func TestDoctorBoardLock_置ける場所なら通る(t *testing.T) {
	fx := newFixture(t)

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelBoardLock, doctor.SymbolOK)
	want := filepath.Join(fx.Home, instance.DirName, instance.BoardDirName, "octocat-3.lock")
	if !strings.Contains(res.Detail, want) {
		t.Fatalf("ボード1枚ぶんのロックを見ていない: got %q, want %q を含むこと", res.Detail, want)
	}
}

// 目的: `~/.continuo` が symlink なら、ロックの置き場所の用意を断ることを確かめる
// （設計 3-17）。
//
// **ロックは「continuo が動いているか」の唯一の判定に使う。**差し替えられた先で
// flock を取ると、**置き換えた相手の手の中で判定することになる。**
//
// 与える情報: `~/.continuo` を別のディレクトリへ向けた symlink。
// 成功条件: `EnsureLockDir` がエラーを返すこと。
func TestEnsureLockDir_置き場所がsymlinkなら断る(t *testing.T) {
	home := shortDoctorHome(t)

	target := filepath.Join(home, "elsewhere")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("差し替え先を作れません: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(home, instance.DirName)); err != nil {
		t.Fatalf("symlink を張れません: %v", err)
	}

	layout, err := instance.Resolve("")
	if err != nil {
		t.Fatalf("置き場所を決められない: %v", err)
	}
	if err := layout.EnsureLockDir(); err == nil {
		t.Fatal("symlink に差し替えられているのに通ってしまった")
	}
}

// 目的: `continuo doctor --id <名前>` が、その名前の場所を見ることを確かめる
// （設計 3-17b）。
//
// **`--id` を付けた起動は、socket もロックも `~/.continuo/id/<名前>/` を使う。**
// **既定の場所だけを見て `✓` を出すと、起動だけが落ちる。**
//
// 与える情報: `--id e2e` で解決した置き場所。
// 成功条件: socket もロックも `<HOME>/.continuo/id/e2e/` の下を指し、
// **どちらの内訳にも `--id` の名前が出ること。**
func TestDoctor_idを渡すとその名前の場所を見る(t *testing.T) {
	fx := newFixture(t)
	// **ホームディレクトリを短く保つ。**`~/.continuo/id/e2e/run/hooks.sock` は
	// 103バイトに収まらなければならず（設計 3-17d / 3-23）、macOS の `TMPDIR` は
	// それだけで66文字前後ある。**`opts.HomeDir` はそのままにする**
	// （`~/.claude.json` の信頼の登録は fixture が置いた側にある）。
	home := shortDoctorHome(t)

	layout, err := instance.Resolve("e2e")
	if err != nil {
		t.Fatalf("--id から置き場所を決められない: %v", err)
	}

	opts := fx.Options()
	opts.Instance = &layout
	report := doctor.Run(t.Context(), opts)

	base := filepath.Join(home, instance.DirName, instance.IDDirName, "e2e")

	sock := assertSymbol(t, report, doctor.LabelRuntimeDir, doctor.SymbolOK)
	wantSock := filepath.Join(base, instance.RunDirName, socketpath.HookSocketFileName)
	if !strings.Contains(sock.Detail, wantSock) {
		t.Fatalf("--id の socket を見ていない: got %q, want %q を含むこと", sock.Detail, wantSock)
	}
	assertNoteMentionsID(t, sock.Notes, doctor.LabelRuntimeDir)

	lockRes := assertSymbol(t, report, doctor.LabelLockFile, doctor.SymbolOK)
	wantLock := filepath.Join(base, instance.LockFileName)
	if !strings.Contains(lockRes.Detail, wantLock) {
		t.Fatalf("--id のロックを見ていない: got %q, want %q を含むこと", lockRes.Detail, wantLock)
	}
	assertNoteMentionsID(t, lockRes.Notes, doctor.LabelLockFile)

	// **`workspace.root` も名前ごとに分かれる**（設計 3-17b）。
	// **写さないと、`--id` を付けた起動が実際に使う置き場所を1度も見ない。**
	root := assertSymbol(t, report, doctor.LabelWorkspaceRoot, doctor.SymbolOK)
	if !strings.Contains(root.Detail, filepath.Join("wt", "e2e")) {
		t.Fatalf("--id の worktree の置き場所を見ていない: %q", root.Detail)
	}
}

// assertNoteMentionsID は、内訳に `--id` の名前が出ていることを見る。
//
// **人間が「どちらの continuo を検査したのか」を読めるようにするためである。**
//
// t: 呼び出し元のテスト。
// notes: 検査結果の内訳。
// label: どの見出し語の内訳かを、失敗の文言に出すためのキー。
func assertNoteMentionsID(t *testing.T, notes []string, label i18n.Key) {
	t.Helper()
	for _, note := range notes {
		if strings.Contains(note, "e2e") {
			return
		}
	}
	t.Fatalf("%s の内訳に --id の名前が出ていない: %v", label, notes)
}

// shortDoctorHome は、ホームディレクトリの代わりに使う短い一時ディレクトリを作り、
// `HOME` をそこへ向ける。
//
// **`--id` を付けたときの socket は `~/.continuo/id/<名前>/run/hooks.sock` である。**
// macOS の Unix domain socket は103バイトまでなので、**深い一時ディレクトリでは
// 名前の検査より先に長さで弾かれ、この検査が空振りする。**
//
// t: 呼び出し元のテスト。
// 戻り値: 実体のパス（symlink を解決済み）。
func shortDoctorHome(t *testing.T) string {
	t.Helper()

	for _, base := range []string{"/tmp", ""} {
		dir, err := os.MkdirTemp(base, "cd")
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

// 目的: `continuo doctor` が、本番が使う名前の資源を1つも作らないことを確かめる
// （設計 3-17h）。
//
// **打ち間違えた `--id` の置き場所が残ると、既定側の走査から永久に隠れる**（3-17f）。
// **さらに悪いのは、`~/.continuo/id/<名前>/` の実在が「その名前で continuo が
// 実際に動いた裏付け」として使われていることである**（3-17f の表）。
// **doctor がそれを作れるなら、検査の道具でその裏付けを偽造できる。**
//
// **socket も作らない。**前は本番の名前で listen してから `os.Remove` していたので、
// **その2つのあいだに常駐が bind し直すと、doctor が生きた socket を消していた。**
//
// 与える情報: 打ち間違えたつもりの `--id typo` で解決した置き場所。
// 成功条件: 5つの本番の名前が1つも作られていないこと。
func TestDoctor_本番が使う名前の資源を作らない(t *testing.T) {
	fx := newFixture(t)
	home := shortDoctorHome(t)

	layout, err := instance.Resolve("typo")
	if err != nil {
		t.Fatalf("--id から置き場所を決められない: %v", err)
	}

	opts := fx.Options()
	opts.Instance = &layout
	doctor.Run(t.Context(), opts)

	idRoot := filepath.Join(home, instance.DirName, instance.IDDirName, "typo")
	for _, path := range []string{
		idRoot,
		filepath.Join(idRoot, instance.LockFileName),
		filepath.Join(idRoot, instance.RunDirName),
		filepath.Join(idRoot, instance.RunDirName, socketpath.HookSocketFileName),
		filepath.Join(home, instance.DirName, instance.BoardDirName),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("doctor が %s を作っている（err=%v）", path, err)
		}
	}
}

// 目的: 置き場所が無くても、上のディレクトリに書けるなら `✓` にすることを確かめる
// （設計 3-17h）。
//
// **作らないと決めた以上、「まだ無い」は `✗` ではない。**起動のときに作られる。
// **ここを `✗` にすると、初回の `continuo doctor` が必ず落ちる。**
//
// 与える情報: `~/.continuo` がまだ1バイトも無いホームディレクトリ。
// 成功条件: `ロックの場所` と `ボードのロック` が `✓` になること。
func TestDoctorLockFile_置き場所がまだ無くても通る(t *testing.T) {
	fx := newFixture(t)

	if _, err := os.Lstat(filepath.Join(fx.Home, instance.DirName)); !os.IsNotExist(err) {
		t.Fatalf("前提が崩れている（~/.continuo が既にある。err=%v）", err)
	}

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelLockFile, doctor.SymbolOK)
	assertSymbol(t, report, doctor.LabelBoardLock, doctor.SymbolOK)
}

// 目的: `workspace.root` に 0700 を要求しないことを確かめる（設計 3-17h）。
//
// **`~/.continuo` は 0700 でなければならないが、`workspace.root` は違う。**
// あちらは利用者が普通に作るディレクトリで、**0755 が普通である。**
// **同じ検査を掛けると、いま動いている環境の `worktree の場所` が `✗` になる。**
//
// 与える情報: 0755 で作った `workspace.root`。
// 成功条件: 見出し語 `worktree の場所` が `✓` になること。
func TestDoctorWorkspaceRoot_権限が0755でも通る(t *testing.T) {
	fx := newFixture(t)

	root := filepath.Join(fx.Root, "wt")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("worktree の置き場所を作れません: %v", err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatalf("権限を 0755 にできません: %v", err)
	}

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelWorkspaceRoot, doctor.SymbolOK)
}
