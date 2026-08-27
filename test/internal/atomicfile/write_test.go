// atomicfile.Write が「一時ファイルへ書き切ってから差し替える」を守れているかを確かめる
// （設計 3-59）。
//
// **確かめるのは4つである。**
//
//	置き換えたあと中身が新しくなっている
//	置き換えても渡した権限がそのまま残る（利用者が変えた権限を塗り潰さない）
//	新しく作るときも渡した権限になる（差し替えなので umask を通らない）
//	差し替えるまでに落ちたら、元の中身が変わらず、一時ファイルも残らない
package atomicfile_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/atomicfile"
)

// assertNoLeftovers は、書き込む先のディレクトリに一時ファイルが残っていないことを確かめる。
//
// atomicfile.Write は `.<書き込む先の名前>.*` という名前で一時ファイルを作る。
// 差し替えに成功しても失敗しても、この名前のものは1つも残ってはいけない。
//
// t: テストコンテキスト。
// dir: 書き込む先のディレクトリ。
// base: 書き込む先のファイル名。
func assertNoLeftovers(t *testing.T, dir, base string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ディレクトリを読めない（%s）: %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "."+base+".") {
			t.Errorf("一時ファイルが残っている: %s", filepath.Join(dir, e.Name()))
		}
	}
}

// 目的: 既にあるファイルを置き換えると、中身が新しくなり、渡した権限がそのまま残ることを確認する。
// 与える情報: 権限 0640・中身 "古い内容\n" のファイルと、新しい中身。
// 成功条件: 中身が新しいものになり、権限が 0640 のままで、一時ファイルが残っていないこと。
func TestWrite_置き換えると中身が新しくなり渡した権限が残る(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte("古い内容\n"), 0o600); err != nil {
		t.Fatalf("テスト用のファイルを置けない: %v", err)
	}
	// umask に左右されないよう、権限は書いたあとに明示して揃える。
	const want fs.FileMode = 0o640
	if err := os.Chmod(path, want); err != nil {
		t.Fatalf("テスト用のファイルの権限を揃えられない: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("テスト用のファイルを確認できない: %v", err)
	}
	if err := atomicfile.Write(path, []byte("新しい内容\n"), info.Mode().Perm()); err != nil {
		t.Fatalf("置き換えに失敗した: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("置き換えたファイルを読めない: %v", err)
	}
	if string(got) != "新しい内容\n" {
		t.Errorf("中身が新しくなっていない: got %q, want %q", string(got), "新しい内容\n")
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("置き換えたファイルを確認できない: %v", err)
	}
	if after.Mode().Perm() != want {
		t.Errorf("元の権限が残っていない: got %o, want %o", after.Mode().Perm(), want)
	}
	assertNoLeftovers(t, dir, "WORKFLOW.md")
}

// 目的: まだ無いファイルへ書くときも、渡した権限のとおりになることを確認する。
// 与える情報: 空の一時ディレクトリと、権限 0640。
// 成功条件: ファイルができ、権限が 0640 であること。
//
// **差し替えなので umask を通らない。**os.CreateTemp が 0600 で作ったものを chmod で
// 揃えてから差し替えるため、渡した権限がそのまま付く。umask を効かせたい経路
// （`continuo init` の新規作成）は、この関数を通していない（設計 3-59）。
//
// **0600 を期待値にしてはならない。**os.CreateTemp が一時ファイルを作るときの権限そのもので、
// 権限を貼り直す処理（f.Chmod）を消しても同じ値が出る。0640 なら貼り直しの有無を見分けられる。
func TestWrite_新しく作るときも渡した権限になる(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	const want fs.FileMode = 0o640

	if err := atomicfile.Write(path, []byte("{}\n"), want); err != nil {
		t.Fatalf("新しく書けなかった: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("書いたファイルを確認できない: %v", err)
	}
	if info.Mode().Perm() != want {
		t.Errorf("渡した権限になっていない: got %o, want %o", info.Mode().Perm(), want)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("書いたファイルを読めない: %v", err)
	}
	if string(got) != "{}\n" {
		t.Errorf("中身が違う: got %q, want %q", string(got), "{}\n")
	}
	assertNoLeftovers(t, dir, "settings.json")
}

// 目的: 差し替えに辿り着く前に落ちても、元のファイルの中身が変わらないことを確認する。
// 与える情報: 中身 "古い内容\n" のファイルと、書き込みを許さないディレクトリ。
// 成功条件: エラーが返り、元の中身がそのまま読め、一時ファイルが残っていないこと。
//
// **ディレクトリを読み取り専用にして落とす。**一時ファイルを作る段で失敗するので、
// 元のファイルには一切触らないまま戻ることを確かめられる。
func TestWrite_差し替える前に落ちても元の中身が残る(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root は書き込み権限の検査を素通りするので、この検査は成り立たない")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	const original = "古い内容\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("テスト用のファイルを置けない: %v", err)
	}

	// t.TempDir の片付けは t.Cleanup の後（後入れ先出し）に走るので、
	// ここで戻しておかないとディレクトリを消せずにテストが落ちる。
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("ディレクトリを読み取り専用にできない: %v", err)
	}

	if err := atomicfile.Write(path, []byte("新しい内容\n"), 0o644); err == nil {
		t.Fatal("書き込めないディレクトリなのにエラーが返らなかった")
	}

	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("ディレクトリの権限を戻せない: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("元のファイルを読めない: %v", err)
	}
	if string(got) != original {
		t.Errorf("失敗したのに元の中身が変わっている: got %q, want %q", string(got), original)
	}
	assertNoLeftovers(t, dir, "WORKFLOW.md")
}

// 目的: 一時ファイルを作ったあとで差し替えに失敗しても、一時ファイルを残さないことを確認する。
// 与える情報: 書き込む先と同じ名前の、空でないディレクトリ。
// 成功条件: エラーが返り、`.WORKFLOW.md.*` が1つも残っておらず、
// 同じ名前のディレクトリとその中身がそのまま残っていること。
//
// **os.Rename は、通常のファイルをディレクトリへ被せられない。**一時ファイルへは
// 書き切れているので、差し替えの段だけを落とせる。
func TestWrite_差し替えに失敗したら一時ファイルを残さない(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.MkdirAll(filepath.Join(path, "中身"), 0o755); err != nil {
		t.Fatalf("テスト用のディレクトリを作れない: %v", err)
	}

	if err := atomicfile.Write(path, []byte("新しい内容\n"), 0o644); err == nil {
		t.Fatal("ディレクトリへ差し替えようとしたのにエラーが返らなかった")
	}

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("書き込む先を確認できない: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("同じ名前のディレクトリが置き換えられている: %s", path)
	}
	if _, err := os.Stat(filepath.Join(path, "中身")); err != nil {
		t.Errorf("ディレクトリの中身が失われている: %v", err)
	}
	assertNoLeftovers(t, dir, "WORKFLOW.md")
}
