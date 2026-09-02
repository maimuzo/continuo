// 書き出す先が WORKFLOW.md という名前のディレクトリだったときの振る舞いを確かめる（設計 3-60）。
//
// **force の有無で通る経路が違うので、両方を見る。**
//
//	force なし … 新しく作る経路の O_EXCL で止まる（ErrAlreadyExists）
//	force あり … 差し替えに進む前に名指しで止める（os.Rename の失敗をそのまま見せない）
package scaffold_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// makeWorkflowDir は dir の直下に、中身のある WORKFLOW.md という名前のディレクトリを作る。
//
// **空にしない。**空のディレクトリだと os.Rename が成功しうる OS があり、
// 「差し替えに進んでいない」ことを確かめられなくなる。
//
// t: テストコンテキスト。
// dir: 作る場所。
// 戻り値: 作ったディレクトリの絶対パス（symlink は辿った先。Result.Path と揃う）。
func makeWorkflowDir(t *testing.T, dir string) string {
	t.Helper()
	path := wantWorkflowPath(t, dir)
	if err := os.MkdirAll(filepath.Join(path, "中身"), 0o755); err != nil {
		t.Fatalf("テスト用のディレクトリを作れない: %v", err)
	}
	return path
}

// assertWorkflowDirIntact は、WORKFLOW.md という名前のディレクトリが中身ごと残っていて、
// 差し替えの一時ファイル（`.WORKFLOW.md.*`）も残っていないことを確かめる。
//
// t: テストコンテキスト。
// path: WORKFLOW.md という名前のディレクトリの絶対パス。
func assertWorkflowDirIntact(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("書き出す先を確認できない: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("同じ名前のディレクトリが置き換えられている: %s", path)
	}
	if _, err := os.Stat(filepath.Join(path, "中身")); err != nil {
		t.Errorf("ディレクトリの中身が失われている: %v", err)
	}
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ディレクトリを読めない（%s）: %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".WORKFLOW.md.") {
			t.Errorf("一時ファイルが残っている: %s", filepath.Join(dir, e.Name()))
		}
	}
}

// 目的: 書き出す先が WORKFLOW.md という名前のディレクトリのとき、force を付けなければ
// 「既にあります」で止まり、ディレクトリを壊さないことを確認する。
// 与える情報: 中身のある WORKFLOW.md というディレクトリがある一時ディレクトリ。force は偽。
// 成功条件: ErrAlreadyExists で返り、Result.Path が埋まっており、
// ディレクトリとその中身がそのまま残っていること。
func TestWriteTemplate_書き出す先がディレクトリならforceなしは既にあるとして止まる(t *testing.T) {
	dir := t.TempDir()
	path := makeWorkflowDir(t, dir)

	result, err := scaffold.WriteTemplate(dir, false)
	if !errors.Is(err, scaffold.ErrAlreadyExists) {
		t.Fatalf("ErrAlreadyExists で止まっていない: %v", err)
	}
	if result.Path != path {
		t.Errorf("どのパスで落ちたかを返していない: got %q, want %q", result.Path, path)
	}
	if result.Overwritten {
		t.Error("置き換えていないのに Overwritten が真である")
	}
	assertWorkflowDirIntact(t, path)
}

// 目的: 書き出す先が WORKFLOW.md という名前のディレクトリのとき、force を付けても
// 差し替えに進まず、ディレクトリだと名指しして止まることを確認する。
// 与える情報: 中身のある WORKFLOW.md というディレクトリがある一時ディレクトリ。force は真。
// 成功条件: 「WORKFLOW.md を作成できません: <パス>: is a directory」で返り、Result.Path が
// 埋まっており、ディレクトリとその中身がそのまま残り、一時ファイルも残っていないこと。
//
// **文言まで見る。**名指しをやめると差し替えの失敗がそのまま出て、利用者には一時ファイルの
// 名前と rename の失敗が並ぶだけになる（設計 3-60）。エラーが返るかどうかだけでは、
// その違いを検知できない。期待値は同じ i18n のキーから組み立てるので、文言を直しても追随する。
func TestWriteTemplate_書き出す先がディレクトリならforceありでも差し替えずに止まる(t *testing.T) {
	dir := t.TempDir()
	path := makeWorkflowDir(t, dir)

	result, err := scaffold.WriteTemplate(dir, true)
	if err == nil {
		t.Fatal("ディレクトリなのにエラーが返らなかった")
	}
	want := i18n.Errorf(i18n.KeyScaffoldFileCreateFailed, filepath.Base(path), path, syscall.EISDIR)
	if err.Error() != want.Error() {
		t.Errorf("ディレクトリだと名指ししていない: got %q, want %q", err.Error(), want.Error())
	}
	if result.Path != path {
		t.Errorf("どのパスで落ちたかを返していない: got %q, want %q", result.Path, path)
	}
	if result.Overwritten {
		t.Error("置き換えていないのに Overwritten が真である")
	}
	assertWorkflowDirIntact(t, path)
}
