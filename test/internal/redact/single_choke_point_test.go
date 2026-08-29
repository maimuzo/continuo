// **issue へ書く本文を組み立てる場所は6箇所ある。**そのどれかが `o.tracker.PostComment` を
// 直に呼ぶと、手元の絶対パスがそのまま公開される（issue #75 / 設計 3-73）。
//
// **この検査は、その迂回を構文木で落とす。**縮めるのは `Orchestrator.postComment` の1箇所
// だけであり、そこ以外からトラッカーのコメント投稿を呼んではならない。
//
// **文字列ではなく構文木を見る。**「`o.tracker.PostComment` を直に呼んではならない」と
// 書いた GoDoc のコメントで落ちてしまうと、なぜそうするのかを説明できなくなる。
//
// **落とせるのは `o.tracker.PostComment(…)` の形だけである。**受け手を別の変数へ写して
// から呼ぶ形（`tk := o.tracker; tk.PostComment(…)`）は素通りする。型を解く仕組みが要り、
// いま internal/orchestrator にその書き方は1つも無い。
package redact_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// orchestratorDir は検査する範囲である。
const orchestratorDir = "../../../internal/orchestrator"

// chokePointFile は `o.tracker.PostComment` を呼んでよい唯一のファイルである。
const chokePointFile = "comment.go"

// TestPostComment_縮める1箇所を迂回していない は、
// issue へのコメントが必ず `postComment` を通ることを確かめる。
//
// 目的: issue #75 の「組み立てる場所ごとに書くと漏れる」を、あとから戻せないようにする。
// 与える情報: internal/orchestrator の Go のファイル全部。
// 成功条件: `o.tracker.PostComment(…)` の呼び出しが comment.go の中にしか無いこと。
func TestPostComment_縮める1箇所を迂回していない(t *testing.T) {
	entries, err := os.ReadDir(orchestratorDir)
	if err != nil {
		t.Fatalf("internal/orchestrator を読めません: %v", err)
	}

	found := 0
	scanned := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		scanned++
		path := filepath.Join(orchestratorDir, e.Name())
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("%s を構文解析できません: %v", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isTrackerPostComment(call.Fun) {
				return true
			}
			found++
			if e.Name() != chokePointFile {
				t.Errorf("%s:%d で o.tracker.PostComment を直に呼んでいます。"+
					"手元の絶対パスが縮まりません。Orchestrator.postComment を呼んでください",
					e.Name(), fset.Position(call.Pos()).Line)
			}
			return true
		})
	}

	if scanned == 0 {
		t.Fatalf("internal/orchestrator の Go のファイルを1つも読めていません（検査が空振りしています）")
	}
	// **1件も見つからないのは、検査が的を外した合図である。**縮める1箇所そのものが
	// comment.go に無ければ、この検査は何も守っていない。
	if found == 0 {
		t.Fatalf("o.tracker.PostComment の呼び出しが1つも見つかりません（検査が的を外しています）")
	}
}

// isTrackerPostComment は、式が `o.tracker.PostComment` かどうかを返す。
//
// fun: 呼び出しの関数の式。
// 戻り値: `o.tracker.PostComment` であれば真。
func isTrackerPostComment(fun ast.Expr) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "PostComment" {
		return false
	}
	recv, ok := sel.X.(*ast.SelectorExpr)
	if !ok || recv.Sel.Name != "tracker" {
		return false
	}
	ident, ok := recv.X.(*ast.Ident)
	return ok && ident.Name == "o"
}
