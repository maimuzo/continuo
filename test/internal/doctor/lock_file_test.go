// **ロックの場所の検査だけを集めたファイルである。**
//
// **この検査は「doctor が全項目 ✓ なのに起動だけが落ちる」（issue #9）と同じ形の穴を
// 塞ぐために足された。**ロックは `~/.continuo`（`--id` を付けたなら
// `~/.continuo/id/<名前>`）に機械で固定してあり、**socket の場所からは導かない**（設計 3-17）。
// **別の場所なので、socket が置けてもロックが置けるとは限らない。**
package doctor_test

import (
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
	want := filepath.Join(fx.Home, instance.DirName, socketpath.LockFileName)
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
// 成功条件: `✓` で、説明が「既に continuo が握っています」であること。
func TestDoctorLockFile_既にcontinuoが握っていれば通る(t *testing.T) {
	fx := newFixture(t)

	lockPath := filepath.Join(fx.Home, instance.DirName, socketpath.LockFileName)
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
	if !strings.Contains(res.Detail, "握っています") && !strings.Contains(res.Detail, "already held") {
		t.Fatalf("既に握られていることが出ていない: %q", res.Detail)
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
	wantLock := filepath.Join(base, socketpath.LockFileName)
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
