// 「書き込む先をその場で空にしてから書く」書き方が internal と cmd へ戻らないための検査である
// （CLAUDE.md の「絶対に守る制約」4 / 設計 3-59）。
//
// **落とすのは2つの書き方だけである。**`os.WriteFile`（内部で O_TRUNC を付ける）と、
// `O_TRUNC` を自分で渡す os.OpenFile である。どちらも書いている途中で落ちると、
// 元の内容が失われる。
//
// **文字列ではなく構文木を見る。**コメントや文字列に書かれた `os.WriteFile` で落とすと、
// 「なぜその書き方をしないのか」を説明したコメントが書けなくなる。import の別名
// （`import ostd "os"` の `ostd.WriteFile`）も、構文木なら取り違えずに追える。
package atomicfile_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// scanRoots は検査する範囲である（test は入れない。テストは壊れた状態を自分で作る）。
var scanRoots = []string{"../../../internal", "../../../cmd"}

// banned は落とす識別子である。import されたパッケージのパスから、その名前の集合を引く。
var banned = map[string]map[string]bool{
	"os":      {"WriteFile": true, "O_TRUNC": true},
	"syscall": {"O_TRUNC": true},
}

// allowed は落とさない場所である（`<パス>:<パッケージ>.<名前>` の形）。
//
// **空である。**揃えられない2箇所（internal/lock の flock は差し替えるとロックが切れる、
// internal/workspace の `.git/info/exclude` は追記のみ）は、どちらも O_TRUNC を使わないので
// ここへ書く必要が無い。理由は、それぞれのコードのコメントに書いてある。
var allowed = map[string]bool{}

func Test切り詰めて書く書き方がinternalとcmdに無い(t *testing.T) {
	fset := token.NewFileSet()
	var found []string

	for _, root := range scanRoots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			file, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return perr
			}

			// この1ファイルの中で、os / syscall がどの名前で見えているかを引けるようにする。
			localName := map[string]string{}
			for _, imp := range file.Imports {
				p, uerr := strconv.Unquote(imp.Path.Value)
				if uerr != nil || banned[p] == nil {
					continue
				}
				name := p
				if imp.Name != nil {
					name = imp.Name.Name
				}
				localName[name] = p
			}

			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				pkg, ok := localName[ident.Name]
				if !ok || !banned[pkg][sel.Sel.Name] {
					return true
				}
				where := path + ":" + pkg + "." + sel.Sel.Name
				if allowed[where] {
					return true
				}
				found = append(found, fset.Position(sel.Pos()).String()+" で "+pkg+"."+sel.Sel.Name+" を使っています")
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("%s を走査できない: %v", root, err)
		}
	}

	if len(found) > 0 {
		t.Errorf("書き込む先をその場で空にしてから書いています。"+
			"internal/atomicfile の Write で差し替えてください（設計 3-59）:\n%s",
			strings.Join(found, "\n"))
	}
}
