// 「書き込む先をその場で空にしてから書く」書き方が internal と cmd へ戻らないための検査である
// （CLAUDE.md の「絶対に守る制約」4 / 設計 3-59）。
//
// **落とすのは、書き込む先をその場で切り詰める書き方すべてである。**
//
//	os.WriteFile / ioutil.WriteFile … 中で O_TRUNC を付ける
//	os.Create                       … O_TRUNC 付きで開く
//	os.OpenFile の O_TRUNC          … syscall 側も、import の別名も、dot import も
//	os.Truncate / f.Truncate        … 開いたあとに中身を切り詰める
//	os.OpenFile の flag の数値      … O_TRUNC を数値で書いた形
//
// どれも書いている途中で落ちると、元の内容が失われる。
//
// **文字列ではなく構文木を見る。**コメントや文字列に書かれた `os.WriteFile` で落とすと、
// 「なぜその書き方をしないのか」を説明したコメントが書けなくなる。import の別名
// （`import ostd "os"` の `ostd.WriteFile`）も dot import（`import . "os"` の `WriteFile`）も、
// 構文木なら取り違えずに追える。
//
// **flag を数値で書くこと自体を落とす。**`os.O_TRUNC` は macOS で 0x400、Linux で 0x200 と
// 値が違ううえ、`1024` とも `1<<10` とも書ける。数値のままでは「O_TRUNC が入っているか」を
// 構文木から決められないので、**flag は名前で書く**ことを規則にして、数値が現れたら落とす。
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

// bannedIdent は落とす識別子である。import されたパッケージのパスから、その名前の集合を引く。
var bannedIdent = map[string]map[string]bool{
	"os":        {"WriteFile": true, "O_TRUNC": true, "Create": true, "Truncate": true},
	"io/ioutil": {"WriteFile": true},
	"syscall":   {"O_TRUNC": true, "Truncate": true, "Ftruncate": true},
}

// bannedMethod は、受け手が何であっても落とすメソッドの名前である。
// `f.Truncate(0)` は開いたファイルの中身をその場で捨てる。受け手の型は構文木だけでは
// 決まらないので、名前で落とす。
var bannedMethod = map[string]bool{"Truncate": true}

// flagPkgs は open の flag の定数を持つパッケージである。この中の `O_` で始まる定数と
// 数値が同じ `|` の並びに入っていたら、その数値を落とす。
var flagPkgs = map[string]bool{"os": true, "syscall": true}

// openFlagArg は、flag を数値で書いていないかを見る関数と、flag が何番目の引数かである。
var openFlagArg = map[string]map[string]int{
	"os":      {"OpenFile": 1},
	"syscall": {"Open": 1},
}

// allowed は落とさない場所である（`<パス>:<パッケージ>.<名前>` の形）。
//
// **空である。**揃えられない2箇所（internal/lock の flock は差し替えるとロックが切れる、
// internal/workspace の `.git/info/exclude` は追記のみ）は、どちらも上の書き方を使わないので
// ここへ書く必要が無い。理由は、それぞれのコードのコメントに書いてある。
var allowed = map[string]bool{}

func Test切り詰めて書く書き方がinternalとcmdに無い(t *testing.T) {
	fset := token.NewFileSet()
	var found []string
	seen := map[string]bool{}
	// **同じ場所は1回しか並べない。**`os.OpenFile(p, os.O_WRONLY|0x400, …)` は
	// 「flag の引数に数値がある」と「名前の定数と数値が混ざっている」の両方に当たる。
	report := func(pos token.Pos, what string) {
		where := fset.Position(pos).String()
		if seen[where] {
			return
		}
		seen[where] = true
		found = append(found, where+" で "+what+" を使っています")
	}

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

			// この1ファイルの中で、対象のパッケージがどの名前で見えているかを引けるようにする。
			// dot import は名前が付かない（`WriteFile` と裸で書ける）ので別の集合に入れる。
			localName := map[string]string{}
			dotImported := map[string]bool{}
			for _, imp := range file.Imports {
				p, uerr := strconv.Unquote(imp.Path.Value)
				if uerr != nil {
					continue
				}
				if bannedIdent[p] == nil && !flagPkgs[p] {
					continue
				}
				name := p[strings.LastIndex(p, "/")+1:]
				if imp.Name != nil {
					name = imp.Name.Name
				}
				if name == "." {
					dotImported[p] = true
					continue
				}
				localName[name] = p
			}

			// `pkg.Name` の `Name` の側も *ast.Ident なので、dot import の検査で
			// 二重に数えないよう、先に印を付けておく。
			selIdents := map[*ast.Ident]bool{}
			ast.Inspect(file, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok {
					selIdents[sel.Sel] = true
				}
				return true
			})

			// pkgOf は、呼び出しの Fun からパッケージのパスと関数の名前を取り出す。
			// `os.OpenFile` と、dot import した `OpenFile` の両方を同じ形で返す。
			pkgOf := func(fun ast.Expr) (string, string, bool) {
				switch f := fun.(type) {
				case *ast.SelectorExpr:
					if ident, ok := f.X.(*ast.Ident); ok {
						if pkg, ok := localName[ident.Name]; ok {
							return pkg, f.Sel.Name, true
						}
					}
				case *ast.Ident:
					for p := range dotImported {
						if _, ok := openFlagArg[p][f.Name]; ok {
							return p, f.Name, true
						}
					}
				}
				return "", "", false
			}

			// isFlagConst は、その式が open の flag の定数（`os.O_WRONLY` など）かを返す。
			isFlagConst := func(n ast.Node) bool {
				switch e := n.(type) {
				case *ast.SelectorExpr:
					ident, ok := e.X.(*ast.Ident)
					if !ok {
						return false
					}
					pkg, ok := localName[ident.Name]
					return ok && flagPkgs[pkg] && strings.HasPrefix(e.Sel.Name, "O_")
				case *ast.Ident:
					if selIdents[e] || !strings.HasPrefix(e.Name, "O_") {
						return false
					}
					for p := range dotImported {
						if flagPkgs[p] {
							return true
						}
					}
				}
				return false
			}

			// intLits は、式の中に現れる整数のリテラルを集める。
			intLits := func(n ast.Node) []*ast.BasicLit {
				var lits []*ast.BasicLit
				ast.Inspect(n, func(m ast.Node) bool {
					if lit, ok := m.(*ast.BasicLit); ok && lit.Kind == token.INT {
						lits = append(lits, lit)
					}
					return true
				})
				return lits
			}

			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.SelectorExpr:
					// `pkg.Name` の形（import の別名も追える）。
					if ident, ok := node.X.(*ast.Ident); ok {
						if pkg, ok := localName[ident.Name]; ok {
							if bannedIdent[pkg][node.Sel.Name] && !allowed[path+":"+pkg+"."+node.Sel.Name] {
								report(node.Pos(), pkg+"."+node.Sel.Name)
							}
							return true
						}
					}
					// 受け手がパッケージでないなら、メソッドの呼び出しである（`f.Truncate(0)`）。
					if bannedMethod[node.Sel.Name] && !allowed[path+":."+node.Sel.Name] {
						report(node.Pos(), "."+node.Sel.Name+"（開いたファイルを切り詰めるメソッド）")
					}

				case *ast.Ident:
					// dot import で裸の名前として見えている形（`import . "os"` の `WriteFile`）。
					if selIdents[node] {
						return true
					}
					for p := range dotImported {
						if bannedIdent[p][node.Name] && !allowed[path+":"+p+"."+node.Name] {
							report(node.Pos(), p+"."+node.Name+"（dot import）")
						}
					}

				case *ast.CallExpr:
					// open の flag を数値で書いていないか。
					pkg, name, ok := pkgOf(node.Fun)
					if !ok {
						return true
					}
					idx, ok := openFlagArg[pkg][name]
					if !ok || idx >= len(node.Args) {
						return true
					}
					for _, lit := range intLits(node.Args[idx]) {
						report(lit.Pos(), pkg+"."+name+" の flag に数値 "+lit.Value)
					}

				case *ast.BinaryExpr:
					// `os.O_WRONLY|0x400` のように、名前の定数と数値を混ぜた形。
					// 変数へ入れてから渡されても、この形なら捕まえられる。
					if node.Op != token.OR {
						return true
					}
					hasFlag := false
					ast.Inspect(node, func(m ast.Node) bool {
						if isFlagConst(m) {
							hasFlag = true
						}
						return true
					})
					if !hasFlag {
						return true
					}
					for _, lit := range intLits(node) {
						report(lit.Pos(), "open の flag に数値 "+lit.Value)
					}
				}
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
			"internal/atomicfile の Write で差し替えてください（設計 3-59）。"+
			"flag の数値は名前（os.O_WRONLY など）に直してください:\n%s",
			strings.Join(found, "\n"))
	}
}
