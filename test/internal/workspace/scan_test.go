package workspace_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/maimuzo/continuo/internal/workspace"
)

// putIdentityFile は置き場所の任意の階層に身元ファイルを1つ置く。
//
// t: 呼び出し元のテスト。
// dir: 置き先のディレクトリ（無ければ作る）。
// body: 書き込む中身。
func putIdentityFile(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("%s を作れない: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".continuo.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("%s に身元ファイルを書けない: %v", dir, err)
	}
}

// 目的: 置き場所の走査が固定の4階層だけを見て、それより深くは掘らないことを確認する
// （設計 3-4 の段2）。
// 与える情報: 4階層目（正しい位置）と5階層目（深すぎる位置）に置いた身元ファイル。
// 成功条件: 4階層目だけが結果に入り、5階層目のものは拾われないこと。
func TestScan_固定の4階層だけを走査する(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	root := fx.Manager.ResolvedRoot()

	correct := filepath.Join(root, "github.com", "maimuzo", "koetsumugi", "continuo-maimuzo-koetsumugi-188")
	putIdentityFile(t, correct, `{"issue_identifier":"maimuzo/koetsumugi#188"}`)

	tooDeep := filepath.Join(correct, "さらに下")
	putIdentityFile(t, tooDeep, `{"issue_identifier":"深すぎる"}`)

	found, err := fx.Manager.Scan()
	if err != nil {
		t.Fatalf("Scan に失敗した: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("走査の結果が1件でない: %+v", found)
	}
	if found[0].Path != correct {
		t.Fatalf("拾ったパスが違う: got %q, want %q", found[0].Path, correct)
	}
	if found[0].Identity == nil || found[0].Identity.IssueIdentifier != "maimuzo/koetsumugi#188" {
		t.Fatalf("身元ファイルの中身が読めていない: %+v", found[0])
	}
}

// 目的: 身元ファイルが無いディレクトリを無視することを確認する（設計 3-4 の段2。
// 人間が置いた worktree かもしれない）。
// 与える情報: 4階層目にある、身元ファイルを持たないディレクトリ。
// 成功条件: 走査の結果が空になり、そのディレクトリが消えていないこと。
func TestScan_身元ファイルが無いディレクトリは無視する(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	root := fx.Manager.ResolvedRoot()

	human := filepath.Join(root, "github.com", "maimuzo", "koetsumugi", "人間の作業場")
	if err := os.MkdirAll(human, 0o700); err != nil {
		t.Fatalf("ディレクトリを作れない: %v", err)
	}

	found, err := fx.Manager.Scan()
	if err != nil {
		t.Fatalf("Scan に失敗した: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("身元ファイルが無いのに拾っている: %+v", found)
	}
	if _, statErr := os.Stat(human); statErr != nil {
		t.Fatalf("人間のディレクトリが消えている: %v", statErr)
	}
}

// 目的: 身元ファイルの JSON が壊れていても、消さずにエラー付きで返すことを確認する
// （設計 3-4 の段2。段6 の書き込み途中で落ちた場合）。
// 与える情報: 壊れた JSON の身元ファイル。
// 成功条件: 結果に Err 付きで含まれ、Identity が nil で、ファイルが残っていること。
func TestScan_壊れた身元ファイルはエラー付きで返す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	root := fx.Manager.ResolvedRoot()

	broken := filepath.Join(root, "github.com", "maimuzo", "koetsumugi", "continuo-1")
	putIdentityFile(t, broken, `{"issue_url": "https://exa`)

	found, err := fx.Manager.Scan()
	if err != nil {
		t.Fatalf("Scan に失敗した: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("走査の結果が1件でない: %+v", found)
	}
	if found[0].Identity != nil {
		t.Fatalf("壊れているのに中身が読めたことになっている: %+v", found[0])
	}
	if !errors.Is(found[0].Err, workspace.ErrIdentityBroken) {
		t.Fatalf("Err が ErrIdentityBroken でない: %v", found[0].Err)
	}
	if _, statErr := os.Stat(filepath.Join(broken, ".continuo.json")); statErr != nil {
		t.Fatalf("壊れた身元ファイルが消されている: %v", statErr)
	}
}

// 目的: 置き場所が空でも走査が失敗しないことを確認する（起動直後の状態）。
// 与える情報: 何も置いていない置き場所。
// 成功条件: エラーにならず、結果が0件であること。
func TestScan_置き場所が空でも失敗しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})

	found, err := fx.Manager.Scan()
	if err != nil {
		t.Fatalf("Scan に失敗した: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("何も置いていないのに拾っている: %+v", found)
	}
}
