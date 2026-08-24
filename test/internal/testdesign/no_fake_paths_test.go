// **テストが「実在しないパス」を環境変数に入れることを禁じる。**
//
// これを禁じないと、次のようなテストが書ける。
//
//	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")   // ← 実在しない
//	got, _ := socketpath.RuntimeDir("")
//	want := filepath.Join("/run/user/1000", "continuo")
//
// **このテストが確かめているのは `filepath.Join` の結果だけである。**
// そのディレクトリが使えるかは1度も試していない。
//
// **実際に利用者へ届いた**（issue #9）。`RuntimeDir` が実在しない値をそのまま返し、
// 呼び出し側が `MkdirAll` して `permission denied` で落ちた。
// **`continuo doctor` は8項目すべて通るのに、起動だけが落ちた。**
//
// **同じ形の欠陥が、同じファイルの隣の枝（TMPDIR）にも残っていた。**
// 人が気をつけるだけでは止まらないので、機械で弾く。
package testdesign_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// watchedEnvs は、値がそのままファイルシステムのパスとして使われる環境変数である。
//
// **これらに絶対パスのリテラルを渡してはならない。**`t.TempDir()` か
// `os.MkdirTemp` の戻り値を使うこと。
var watchedEnvs = map[string]bool{
	"XDG_RUNTIME_DIR":      true,
	"TMPDIR":               true,
	"HOME":                 true,
	"CONTINUO_RUNTIME_DIR": true,
}

// TestDesign_テストが実在しないパスを環境変数に入れていない は、issue #9 の形を機械で弾く。
//
// 目的: `t.Setenv` の第2引数に、絶対パスの文字列リテラルが渡っていないこと。
// 与える情報: `test/` 配下の全 `_test.go`。
// 成功条件: 1件も見つからないこと。**走査したファイルが0件でないこと**も確かめる。
func TestDesign_テストが実在しないパスを環境変数に入れていない(t *testing.T) {
	root := filepath.Join("..", "..")

	checked := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// このファイル自身は、禁じた形を例として書いている。
		if strings.HasSuffix(path, "no_fake_paths_test.go") {
			return nil
		}
		checked++

		fset := token.NewFileSet()
		// **コメントも読む。**許可マーカーを探すため。
		file, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			t.Fatalf("%s を解析できません: %v", path, perr)
		}
		allowed := allowedLines(fset, file)

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Setenv" {
				return true
			}
			name, ok := literalString(call.Args[0])
			if !ok || !watchedEnvs[name] {
				return true
			}
			value, ok := literalString(call.Args[1])
			if !ok {
				// 変数を渡している。**それが正しい形である。**
				return true
			}
			if !strings.HasPrefix(value, "/") {
				// 空文字は「未設定にする」という意味なので許す。
				return true
			}
			line := fset.Position(call.Pos()).Line
			if allowed[line] {
				// **そのパスを continuo が作らない場合は、実在しなくてよい。**
				// 例: herdr の socket のパスを組み立てるだけのテスト。
				// マーカーで「意図してそうしている」ことを記録させる。
				return true
			}
			t.Errorf("%s:%d %s に絶対パスのリテラル %q を渡しています。\n"+
				"  **実在しない値でテストが通ると、「実在しなくてよい」が仕様になります**（issue #9）。\n"+
				"  `t.TempDir()` か `os.MkdirTemp` の戻り値を使ってください。",
				path, line, name, value)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("%s を走査できません: %v", root, err)
	}
	if checked == 0 {
		t.Fatalf("走査したファイルが0件です（対象: %s）。パスを確かめてください", root)
	}
	t.Logf("走査したファイル: %d 件", checked)
}

// allowMarker は、実在しないパスを渡してよいことを示すコメントである。
//
// **そのパスを continuo が作らない場合にだけ使う。**
// 例: herdr が作る socket のパスを組み立てるだけのテスト。
const allowMarker = "test-design:allow-fake-path"

// allowedLines は、許可マーカーが付いた行の番号を集める。
//
// **マーカーと同じ行か、その1行前にある `t.Setenv` を許す。**
//
// fset: 位置を引くための情報。
// file: 解析した1ファイル。
// 戻り値: 許してよい行番号の集合。
func allowedLines(fset *token.FileSet, file *ast.File) map[int]bool {
	out := map[int]bool{}
	for _, group := range file.Comments {
		for _, c := range group.List {
			if !strings.Contains(c.Text, allowMarker) {
				continue
			}
			line := fset.Position(c.Pos()).Line
			out[line] = true
			out[line+1] = true
		}
	}
	return out
}

// literalString は、式が文字列リテラルならその中身を返す。
//
// e: 調べる式。
// 戻り値: 中身と、文字列リテラルだったかどうか。
func literalString(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	// 引用符を落とす（生文字列リテラルも同じ扱いでよい）。
	s := lit.Value
	if len(s) >= 2 {
		s = s[1 : len(s)-1]
	}
	return s, true
}
