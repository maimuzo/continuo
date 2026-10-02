// {"RUCM-CFG-SHA256": "85c7dd3fa4d791cbeb832df9c29187320d95d87f8a35b095f21385daa20656ba", "SOURCE": "docs/spec/usecases/particular_case/設定ファイルを作る.cfg.json"}
//
// **ユースケース記述「設定ファイルを作る」の経路に対応づけたテストである。**
// **421本の経路は、値の決まり方30通りと、書き出しの結末14通りの組み合わせである**（残り1本は引数の誤り）。
// **終端フローごとに代表を1本ずつ**対応づける。組み合わせを全部書いても、
// 同じ経路を何度も通るだけで新しく守れるものが増えない。
//
// **ここに置くのは、scaffold.WriteTemplate を直に呼ぶテストである。**
// WriteTemplate は WORKFLOW.md だけを書く。gh から値を引く段と、2枚目（continuo-ci.yaml）を
// 書く段は通らない。2枚を書く `continuo init` 全体のテストは test/internal/cli に在る。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストは、
// 同じディレクトリの別のファイルへ足す。
package scaffold_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/scaffold"
)

// {"RUCM-PATH": "P001"}
//
// 目的: 位置引数で渡したディレクトリの直下に WORKFLOW.md が1つだけ置かれることを確認する。
// 与える情報: 空の一時ディレクトリ。force は偽。
// 成功条件: エラーにならず、Result.Path が <ディレクトリ>/WORKFLOW.md の絶対パスであり、
// Overwritten が偽で、そのディレクトリの中身が WORKFLOW.md の1件だけであること。
// 書き出した中身が設計 5-2 / 5-3 に照らして雛形として成立していること
// （scaffold.Template() と突き合わせると、雛形を壊しても通ってしまうので照合先にしない）。
func Test_設定ファイルを作る_P001_指定したディレクトリの直下にWORKFLOW_mdだけを置く(t *testing.T) {
	dir := t.TempDir()

	result, err := scaffold.WriteTemplate(dir, false)
	if err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}

	want := wantWorkflowPath(t, dir)
	if result.Path != want {
		t.Errorf("Result.Path が想定と違う: got %q, want %q", result.Path, want)
	}
	if result.Overwritten {
		t.Error("新規に作成したのに Overwritten が真になっている")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("書き出した先を読めない: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "WORKFLOW.md" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("置くのは WORKFLOW.md の1ファイルだけであるべきなのに %v が置かれている", names)
	}

	got, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("書き出したファイルを読めない: %v", err)
	}
	assertTemplateFollowsDesign(t, "書き出した WORKFLOW.md", string(got))
}

// {"RUCM-PATH": "P007"}
//
// Test_設定ファイルを作る_P007_forceなら上書きして上書きしたと返す は、`--force` の経路を確かめる。
//
// 目的: `force` が真なら上書きし、Result.Overwritten を真にすること。
// 与える情報: 既にファイルがあるディレクトリ。
// 成功条件: 雛形の中身になり、Overwritten が真であること。
func Test_設定ファイルを作る_P007_forceなら上書きして上書きしたと返す(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte("# 古いもの\n"), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	got, err := scaffold.WriteTemplate(dir, true)
	if err != nil {
		t.Fatalf("WriteTemplate が失敗した: %v", err)
	}
	if !got.Overwritten {
		t.Error("上書きしたのに Overwritten が偽になっている")
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", readErr)
	}
	if strings.Contains(string(after), "古いもの") {
		t.Error("上書きできていない")
	}
	if !strings.Contains(string(after), "tracker:") {
		t.Error("雛形の中身になっていない")
	}
}

// {"RUCM-PATH": "P014"}
//
// Test_設定ファイルを作る_P014_ディレクトリが無ければエラーを返す は、置き場所の検査を確かめる。
//
// 目的: 存在しないディレクトリを指されたら `ErrDirNotFound` を返すこと。
// 与える情報: 存在しないパス。
// 成功条件: そのエラーで返ること。
func Test_設定ファイルを作る_P014_ディレクトリが無ければエラーを返す(t *testing.T) {
	_, err := scaffold.WriteTemplate(filepath.Join(t.TempDir(), "no-such-dir"), false)
	if !errors.Is(err, scaffold.ErrDirNotFound) {
		t.Fatalf("ErrDirNotFound でない: %v", err)
	}
}

// {"RUCM-PATH": "P013"}
//
// Test_設定ファイルを作る_P013_ディレクトリでなければエラーを返す は、置き場所の種類を確かめる。
//
// 目的: ファイルを指されたら `ErrNotADirectory` を返すこと。
// 与える情報: ファイルのパス。
// 成功条件: そのエラーで返ること。
func Test_設定ファイルを作る_P013_ディレクトリでなければエラーを返す(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("ファイルを作れません: %v", err)
	}

	_, err := scaffold.WriteTemplate(file, false)
	if !errors.Is(err, scaffold.ErrNotADirectory) {
		t.Fatalf("ErrNotADirectory でない: %v", err)
	}
}
