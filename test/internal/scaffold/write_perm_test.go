// `continuo init` が置く WORKFLOW.md の権限を確かめる（設計 3-59）。
//
// **見るのは2つである。**
//
//	新しく作るときは umask が効く（差し替えにしていないので、権限は open の段で決まる）
//	force で置き換えるときは元のファイルの権限がそのまま残る（利用者が変えた権限を塗り潰さない）
package scaffold_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/maimuzo/continuo/internal/scaffold"
)

// fixUmask は、このテストの間だけ umask を mask に固定し、終わったら元へ戻す。
//
// **実行環境の umask に任せてはならない。**多くの機械の既定は 0022 で、
// 0644 から引いても 0644 のまま（＝ WriteTemplate が open へ渡している権限そのもの）になる。
// その値では、新しく作る経路を差し替えへ寄せ替えても同じ権限が出るので、テストが素通りする。
//
// syscall.Umask は「新しい値を入れて、古い値を返す」しかできないので、返ってきた古い値を
// t.Cleanup で戻す。**umask はプロセス全体に効く。**このパッケージのテストは t.Parallel を
// 使っていないので並びは1本であり、固定している間に他のテストがファイルを作ることは無い。
//
// t: テストコンテキスト。
// mask: このテストの間だけ使う umask。
func fixUmask(t *testing.T, mask fs.FileMode) {
	t.Helper()
	old := syscall.Umask(int(mask))
	t.Cleanup(func() { syscall.Umask(old) })
}

// 目的: 新しく WORKFLOW.md を作るときは umask が効くことを確認する。
// 与える情報: umask を 0027 に固定した状態と、空の一時ディレクトリ。force は偽の場合と真の場合の両方。
// 成功条件: できたファイルの権限が 0640（0644 から umask 0027 を引いたもの）であること。
//
// **新しく作る経路は差し替えにしていない**（設計 3-59）。差し替えにすると権限を chmod で
// 決めることになり、umask が効かなくなる。この検査は、そこが差し替えへ寄せられていないことを守る。
//
// **umask に 0027 を選ぶのは、0644 とも 0600 とも違う値を作るためである。**差し替えへ寄せると
// 権限は chmod で 0644（WriteTemplate が渡す値）になり、一時ファイルの既定は 0600 なので、
// 0640 を期待しておけばどちらとも見分けられる。
func TestWriteTemplate_新しく作るときはumaskが効く(t *testing.T) {
	const mask fs.FileMode = 0o027
	fixUmask(t, mask)
	const want = fs.FileMode(0o644) &^ mask

	for _, force := range []bool{false, true} {
		dir := t.TempDir()

		result, err := scaffold.WriteTemplate(dir, force)
		if err != nil {
			t.Fatalf("force=%v: 雛形を書き出せなかった: %v", force, err)
		}
		info, err := os.Stat(result.Path)
		if err != nil {
			t.Fatalf("force=%v: 書き出したファイルを確認できない: %v", force, err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("force=%v: umask が効いていない: got %o, want %o",
				force, info.Mode().Perm(), want)
		}
	}
}

// 目的: force で既にある WORKFLOW.md を置き換えても、元のファイルの権限が残ることを確認する。
// 与える情報: 権限 0600・中身 "既存の内容\n" の WORKFLOW.md がある一時ディレクトリ。force は真。
// 成功条件: 中身が雛形に置き換わり、権限が 0600 のままで、一時ファイルが残っていないこと。
func TestWriteTemplate_forceで置き換えても元の権限が残る(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte("既存の内容\n"), 0o600); err != nil {
		t.Fatalf("テスト用の既存ファイルを置けない: %v", err)
	}
	// umask に左右されないよう、権限は書いたあとに明示して揃える。
	const want fs.FileMode = 0o600
	if err := os.Chmod(path, want); err != nil {
		t.Fatalf("テスト用の既存ファイルの権限を揃えられない: %v", err)
	}

	if _, err := scaffold.WriteTemplate(dir, true); err != nil {
		t.Fatalf("force を付けたのに書き出せなかった: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("置き換えたファイルを確認できない: %v", err)
	}
	if info.Mode().Perm() != want {
		t.Errorf("元の権限が残っていない: got %o, want %o", info.Mode().Perm(), want)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("置き換えたファイルを読めない: %v", err)
	}
	if strings.Contains(string(got), "既存の内容") {
		t.Error("force を付けたのに既存の内容が残っている")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ディレクトリを読めない: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".WORKFLOW.md.") {
			t.Errorf("一時ファイルが残っている: %s", filepath.Join(dir, e.Name()))
		}
	}
}
