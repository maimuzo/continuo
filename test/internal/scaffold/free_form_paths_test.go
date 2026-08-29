// **数えない対応表のパスが、雛形に実在するキーを指していることを機械で確かめる。**
//
// `internal/scaffold/missing_keys.go` の `freeFormMapPaths` は、
// **その下に並ぶ名前を利用者が決める対応表**を並べたものである。
// **綴りを1語間違えても、テストは1本も落ちない。**そのパスに一度も当たらなくなるだけで、
// 守るはずだった対応表の中身が「足りない」と言われ始める。
//
// **実際に間違っていた**（issue #85）。`agent.max_concurrent_agents_by_state` を
// `concurrency.max_concurrent_agents_by_state` と書いていた。雛形がその行を `{}` の
// 1行で書いているため子が1つも無く、**利用者に見える違いが出ないまま残った。**
// 雛形に例を1行足した瞬間に、消せない `!` が出るようになる。
//
// 人が気をつけるだけでは止まらないので、機械で弾く。
package scaffold_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// missingKeysSourcePath は freeFormMapPaths が書かれているファイルである。
// Go のテストはパッケージのディレクトリを作業ディレクトリとして走るので、
// test/internal/scaffold からの相対パスで指す。
const missingKeysSourcePath = "../../../internal/scaffold/missing_keys.go"

// TestMissingKeys_数えない対応表のパスは雛形に実在する は、
// **`freeFormMapPaths` の綴り間違いを弾く。**
//
// 目的: 並べたパスが1つ残らず、雛形の front matter にあるキーを指していること。
// **指していなければ、その行は一度も効かない。**
// 与える情報: `internal/scaffold/missing_keys.go` の `freeFormMapPaths` と、
// `continuo init` が置く雛形の front matter。
// 成功条件: どのパスも、雛形のキーそのものか、その下にキーを持つこと。
func TestMissingKeys_数えない対応表のパスは雛形に実在する(t *testing.T) {
	paths := readFreeFormMapPaths(t)
	if len(paths) == 0 {
		t.Fatal("freeFormMapPaths を1件も読み取れませんでした")
	}

	front := frontMatterOf(t, "雛形", fullWorkflow(t))
	keys := flattenYAMLValues(t, "雛形", front)

	for _, dotted := range paths {
		if _, ok := keys[dotted]; ok {
			continue
		}
		if hasChildKey(keys, dotted) {
			continue
		}
		t.Errorf("freeFormMapPaths の %q が雛形に無いキーを指しています。"+
			"このままではその行が一度も効きません", dotted)
	}
}

// hasChildKey は、その下にキーがあるかを返す。
//
// **対応表そのものが葉として現れないことがある。**雛形が `claude.env` の下に
// 例の環境変数を1行書いているので、`claude.env` は葉ではなく通り道になる。
//
// keys: 雛形の front matter を "." でつないだキーのパスの一覧。
// dotted: 調べるパス。
// 戻り値: `dotted + "."` で始まるキーが1つでもあれば真。
func hasChildKey(keys map[string]string, dotted string) bool {
	prefix := dotted + "."
	for k := range keys {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// readFreeFormMapPaths は missing_keys.go の freeFormMapPaths を構文木から読み取る。
//
// **写しをこのファイルへ書かない。**書くと、片方だけ直したときに気づけない。
//
// t: 呼び出し元のテスト。
// 戻り値: "." でつないだパスの一覧（例 `["claude.env", ...]`）。
func readFreeFormMapPaths(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, missingKeysSourcePath, nil, 0)
	if err != nil {
		t.Fatalf("%s を読めません: %v", missingKeysSourcePath, err)
	}

	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range spec.Names {
			if name.Name != "freeFormMapPaths" || i >= len(spec.Values) {
				continue
			}
			lit, ok := spec.Values[i].(*ast.CompositeLit)
			if !ok {
				t.Fatalf("freeFormMapPaths が複合リテラルではありません")
			}
			for _, elt := range lit.Elts {
				inner, ok := elt.(*ast.CompositeLit)
				if !ok {
					t.Fatalf("freeFormMapPaths の要素が複合リテラルではありません")
				}
				parts := make([]string, 0, len(inner.Elts))
				for _, e := range inner.Elts {
					basic, ok := e.(*ast.BasicLit)
					if !ok || basic.Kind != token.STRING {
						t.Fatalf("freeFormMapPaths の要素に文字列でないものがあります")
					}
					s, err := strconv.Unquote(basic.Value)
					if err != nil {
						t.Fatalf("freeFormMapPaths の文字列を読めません: %v", err)
					}
					parts = append(parts, s)
				}
				out = append(out, strings.Join(parts, "."))
			}
		}
		return true
	})
	return out
}
