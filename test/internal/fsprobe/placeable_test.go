package fsprobe_test

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/fsprobe"
)

// 目的: 使い捨ての socket が、本番のファイル名と同じ長さで作られ、跡を残さないことを
// 確認する（設計 3-17h）。
//
// **長さが違うと、上限ちょうどのパスで判定が食い違う。**長ければ本番なら収まるものを
// 落とし、短ければ収まらないものを見逃す。
//
// 与える情報: 一時ディレクトリの下の `hooks.sock`。
// 成功条件: 通ること。**本番の名前が作られていないこと。**使い捨ての跡も残らないこと。
func TestProbeSocketInside_本番の名前を作らず跡も残さない(t *testing.T) {
	dir := shortTempDir(t)
	sock := filepath.Join(dir, "hooks.sock")

	if err := fsprobe.ProbeSocketInside(sock); err != nil {
		t.Fatalf("置けるはずの場所で落ちた: %v", err)
	}

	if _, err := os.Lstat(sock); !os.IsNotExist(err) {
		t.Fatalf("本番の名前の socket を作っている（err=%v）", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ディレクトリを読めない: %v", err)
	}
	for _, e := range entries {
		t.Fatalf("使い捨ての跡が残っている: %s", e.Name())
	}
}

// 目的: 使い捨ての名前が、本番のファイル名と同じ長さであることを確認する。
//
// **直に確かめられないので、本番の名前と同じ長さの socket を先に置いて、
// それでも通ることを見る。**長さが違えば、パスの上限に当たる環境で判定が変わる。
//
// 与える情報: 既に本番の名前で listen している socket。
// 成功条件: 使い捨ての名前は別物なので、通ること。
func TestProbeSocketInside_本番が待ち受けていても通る(t *testing.T) {
	dir := shortTempDir(t)
	sock := filepath.Join(dir, "hooks.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("本番の socket を置けない: %v", err)
	}
	defer func() { _ = ln.Close() }()

	if err := fsprobe.ProbeSocketInside(sock); err != nil {
		t.Fatalf("本番が待ち受けているだけで落ちた: %v", err)
	}
	// **本番の socket を消していないこと。**
	if _, err := os.Lstat(sock); err != nil {
		t.Fatalf("本番の socket を消している: %v", err)
	}
}

// 目的: 書けないディレクトリでは落ちることを確認する。
//
// **落とす側を確かめないと、いつでも通る実装でも合格してしまう。**
//
// 与える情報: 書き込みの権限を落としたディレクトリ。
// 成功条件: エラーが返ること。
func TestProbeSocketInside_書けなければ落ちる(t *testing.T) {
	dir := shortTempDir(t)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("権限を落とせない: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	err := fsprobe.ProbeSocketInside(filepath.Join(dir, "hooks.sock"))
	if err == nil {
		t.Fatal("書けないのに通ってしまった")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Fatalf("どこで落ちたのかが分からない文言である: %v", err)
	}
}

// 目的: `ProbePlaceable` が、そのディレクトリを作らずに書けるかを答えることを確認する
// （設計 3-17h）。
//
// **`ProbeWritable` は `os.MkdirAll` で作ってしまう。**
// **`continuo doctor` から呼ぶと、検査しただけで本番の置き場所ができる。**
//
// 与える情報: まだ無い2階層下のディレクトリ。
// 成功条件: 通ること。**そのディレクトリが作られていないこと。**
func TestProbePlaceable_無い置き場所を作らずに答える(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "wt", "e2e")

	if err := fsprobe.ProbePlaceable(target); err != nil {
		t.Fatalf("上に書けるのに落ちた: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(base, "wt")); !os.IsNotExist(err) {
		t.Fatalf("置き場所を作っている（err=%v）", err)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("ディレクトリを読めない: %v", err)
	}
	for _, e := range entries {
		t.Fatalf("使い捨ての跡が残っている: %s", e.Name())
	}
}

// 目的: `ProbePlaceable` が、権限を理由に落とさないことを確認する（設計 3-17h）。
//
// **`workspace.root` は利用者が普通に作るディレクトリで、0755 が普通である。**
// **`~/.continuo` と同じ 0700 を要求すると、動いている環境を `✗` と答える。**
//
// 与える情報: 0755 のディレクトリ。
// 成功条件: 通ること。
func TestProbePlaceable_0755でも通る(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("権限を 0755 にできない: %v", err)
	}

	if err := fsprobe.ProbePlaceable(dir); err != nil {
		t.Fatalf("0755 で落ちた: %v", err)
	}
}

// 目的: 途中にファイルが挟まっていたら落ちることを確認する。
//
// **そこには `os.MkdirAll` も作れない。**通してしまうと、起動だけが落ちる。
//
// 与える情報: ディレクトリではなくファイルである親。
// 成功条件: エラーが返ること。
func TestProbePlaceable_親がファイルなら落ちる(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "wt")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("邪魔をするファイルを置けない: %v", err)
	}

	if err := fsprobe.ProbePlaceable(filepath.Join(blocker, "e2e")); err == nil {
		t.Fatal("親がファイルなのに通ってしまった")
	}
}

// shortTempDir は、unix socket を置ける短さの一時ディレクトリを作る。
//
// **`t.TempDir()` はテストの名前をパスに入れる。**日本語のテスト名だと、
// **それだけで unix socket の上限（darwin で 104 バイト）を超える。**
//
// t: 呼び出し元のテスト。
// 戻り値: 実体のパス（symlink を解決済み）。
func shortTempDir(t *testing.T) string {
	t.Helper()

	for _, base := range []string{"/tmp", ""} {
		dir, err := os.MkdirTemp(base, "cf")
		if err != nil {
			continue
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return dir
		}
		return resolved
	}
	t.Fatal("一時ディレクトリを作れません")
	return ""
}
