// **本番のボードの Status を全消しする mutation が、どこにも書かれていないことを守る。**
//
// `updateProjectV2Field` は、選択肢の指定を**全件の置き換え**として扱う。
// 呼んだ瞬間に、設定済みの Status の値が全部消える。**取り返せない。**
// CLAUDE.md はこれを絶対制約として挙げている。
//
// **走査するのは `internal/` と `cmd/` の全 `.go` である。**
// 以前は `internal/tracker` だけを見ていたが、**この mutation を実際に足しそうなのは
// Status の割り当てを扱う `internal/setup` のほう**であり、そこは1バイトも見ていなかった。
package tracker_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestSource_updateProjectV2Fieldを呼ぶコードがどこにも無い は、本番のボードを守る。
//
// **コメントと文字列を区別する。**`internal/setup` などは、この mutation を
// 「呼んではならない」と**コメントで説明している。**単純な部分文字列一致では、
// その説明のほうが引っかかって落ちる。
//
// **見るのは、識別子（呼び出し）と文字列リテラル（GraphQL のクエリ）だけである。**
//
// 目的: `updateProjectV2Field` が、実際に実行されうる場所に1つも無いこと。
// 与える情報: `internal/` と `cmd/` の全 `.go`（テストと生成物を除く）。
// 成功条件: 1件も見つからないこと。**走査したファイルが0件でないこと**も確かめる。
func TestSource_updateProjectV2Fieldを呼ぶコードがどこにも無い(t *testing.T) {
	const forbidden = "updateProjectV2Field"
	// **これは別の mutation である。**項目1件の値を書き換えるもので、選択肢は消えない。
	const allowed = "updateProjectV2ItemFieldValue"

	roots := []string{
		filepath.Join("..", "..", "..", "internal"),
		filepath.Join("..", "..", "..", "cmd"),
	}

	checked := 0
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			// テストは対象外（このファイル自身が名前を書いている）。
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			checked++

			fset := token.NewFileSet()
			// **コメントを読み込まない。**説明文を誤検知しないため。
			file, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				t.Fatalf("%s を解析できません: %v", path, perr)
			}

			ast.Inspect(file, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.BasicLit:
					// GraphQL のクエリは文字列リテラルに入る。
					if v.Kind == token.STRING {
						report(t, path, fset.Position(v.Pos()).Line, v.Value, forbidden, allowed)
					}
				case *ast.Ident:
					report(t, path, fset.Position(v.Pos()).Line, v.Name, forbidden, allowed)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("%s を走査できません: %v", root, err)
		}
	}

	// **0件だったら、走査そのものが壊れている。**
	// パスの間違いで「1件も見つからない」のと、本当に無いのを区別する。
	if checked == 0 {
		t.Fatalf("走査したファイルが0件です（対象: %v）。パスを確かめてください", roots)
	}
	t.Logf("走査したファイル: %d 件（対象: %v）", checked, roots)
}

// report は、禁じた名前が出てきたらテストを落とす。
//
// t: 呼び出し元のテスト。
// path: そのファイル。
// line: 行番号。
// text: 調べる文字列（識別子か、文字列リテラルの原文）。
// forbidden: 禁じた名前。
// allowed: それを接頭辞に含む、許してよい別の名前。
func report(t *testing.T, path string, line int, text, forbidden, allowed string) {
	t.Helper()
	idx := 0
	for {
		pos := strings.Index(text[idx:], forbidden)
		if pos < 0 {
			return
		}
		at := idx + pos
		// `updateProjectV2ItemFieldValue` は別物なので見逃す。
		if strings.HasPrefix(text[at:], allowed) {
			idx = at + len(allowed)
			continue
		}
		t.Errorf("%s:%d に %q があります。**呼ぶとボードの Status の選択肢が全部消えます。**"+
			"選択肢を足すのは人間が GitHub の画面から行います: %s",
			path, line, forbidden, strings.TrimSpace(text))
		return
	}
}
