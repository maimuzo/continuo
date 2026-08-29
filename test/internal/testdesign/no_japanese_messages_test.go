// **英語を選んだ画面に日本語が混ざることを、機械で止める。**
//
// **`internal/` の `fmt.Sprintf` / `fmt.Errorf` / `errors.New` に日本語を直に書くと、
// その1行だけは資源から引かれないので、英語を選んでも日本語のまま出る。**
// 実際に `continuo doctor` の credentials の行と `continuo init` の案内でそうなっていた。
//
// **人が目で確かめるだけでは止まらない**（訳を全部入れた回にも6件見落とした）ので、
// 抽象構文木を読んで数える。
//
// **まだ資源へ移していない package がある。**そこは下の表に件数を書いて通す。
// **表の数は上限ではなく実数である。**移し終えたら数を下げ、0 になったら行ごと消すこと。
package testdesign_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// japaneseTargets は日本語を探す対象の関数である。
//
// **この3つだけを見る。**文言を組み立てるのがこの3つで、`i18n.T` / `i18n.Errorf` /
// `i18n.Sentinel` に置き換えられるのもこの3つだからである。
// **`const` に置いた長い文字列は見ない**（`internal/scaffold/template.go` の
// WORKFLOW.md の雛形がそれで、あれは設定ファイルの中身であって画面の文言ではない）。
var japaneseTargets = map[string]bool{
	"fmt.Sprintf": true,
	"fmt.Errorf":  true,
	"errors.New":  true,
}

// japaneseLogMethods はログの出力に使うメソッドの名前である。
//
// **ログは運用者が読むものなので、画面に出す文言と同じ資源には載せていない。**
// 受け手が `logger` か `slog` のときだけ逃がす（`fs.DirEntry.Info()` のような
// 同名のメソッドを巻き込まないため）。
var japaneseLogMethods = map[string]bool{
	"Debug": true, "Info": true, "Warn": true, "Error": true,
	"DebugContext": true, "InfoContext": true, "WarnContext": true, "ErrorContext": true,
	"Log": true, "LogAttrs": true,
}

// japaneseAllowance は、まだ資源へ移していない場所と、そこに残っている件数である。
//
// **キーはリポジトリの root からの相対パスの先頭一致である**（ファイルでもディレクトリでもよい）。
// **値はいまそこに残っている件数そのものである。**増えたら「新しく足した」、
// 減ったら「移したのに表を直していない」として落ちる。**どちらも直し方を出力に書く。**
//
// **なぜ package ごとに逃がすのか。**画面に出す文言のうち、`continuo doctor` と
// `continuo init` と `continuo trust`（の一部）は資源へ移したが、常駐プロセスの側
// （orchestrator / tracker / workspace）はまだである。**中途半端に訳すと、1つの画面に
// 英語と日本語が混ざって、全部日本語であるより読みにくくなる。**package 単位で移す。
var japaneseAllowance = map[string]int{
	// 設定の値が要件を満たさないときの「こうすること」の文。`invalidValueError` の第3引数。
	"internal/config/validate.go": 4,
	// hook の受け口。組み立ての誤りを表す errors.New が中心である。
	"internal/hookserver": 8,
	// 識別子の正規化で情報が落ちたときの警告。
	"internal/normalize": 1,
	// 常駐プロセスの本体。引き渡しコメントの文面もここにあり、まとめて移す必要がある。
	"internal/orchestrator": 27,
	// ダッシュボードの組み立ての誤り。
	"internal/server": 3,
	// `continuo setup` の対話。
	"internal/setup": 8,
	// GitHub の GraphQL のアダプタ。
	"internal/tracker": 29,
	// `continuo trust` の報告と、要求内容の読み取り。
	"internal/trust": 44,
	// worktree の用意と片付け。
	"internal/workspace": 19,
}

// TestDesign_画面に出す文言を日本語で直に書いていない は、英語の画面に日本語が混ざるのを止める。
//
// 目的: `internal/` の非テストの `.go` で、`fmt.Sprintf` / `fmt.Errorf` / `errors.New` の
// 文字列リテラルに日本語が入っていないこと。
// 与える情報: `internal/` 配下の全 `.go`（`_test.go` を除く）。
// 成功条件: 逃がし先の表に無い場所で1件も見つからず、表の件数がいまの実数と一致すること。
// **走査したファイルが0件でないこと**も確かめる。
func TestDesign_画面に出す文言を日本語で直に書いていない(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	target := filepath.Join(root, "internal")

	fset := token.NewFileSet()
	checked := 0
	found := map[string]int{}

	err := filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		checked++

		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return fmt.Errorf("%s を構文解析できません: %w", path, perr)
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)

		exempt := exemptRanges(file)
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, ok := calleeName(call)
			if !ok || !japaneseTargets[name] {
				return true
			}
			if exempt.contains(call.Pos()) {
				return true
			}
			for _, arg := range call.Args {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				text, uerr := strconv.Unquote(lit.Value)
				if uerr != nil || !containsJapanese(text) {
					continue
				}
				owner, allowed := allowanceOwner(rel)
				if !allowed {
					t.Errorf("%s:%d の %s に日本語が直に書かれています。\n  %q\n"+
						"  **ここに書いた文字列は、英語を選んだ画面にもそのまま日本語で出ます。**\n"+
						"  文言は internal/i18n/messages/ja.json と en.json へ入れ、\n"+
						"  i18n.T / i18n.Errorf で引いてください。\n"+
						"  package の変数として持つ番兵エラーは i18n.Sentinel を使ってください。",
						rel, fset.Position(lit.Pos()).Line, name, text)
					continue
				}
				found[owner]++
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("%s を走査できません: %v", target, err)
	}
	if checked == 0 {
		t.Fatalf("走査したファイルが0件です（対象: %s）。パスを確かめてください", target)
	}

	for _, owner := range sortedKeys(japaneseAllowance) {
		want := japaneseAllowance[owner]
		got := found[owner]
		switch {
		case got > want:
			t.Errorf("%s に日本語の文言が %d 件あります（表に書いてあるのは %d 件）。\n"+
				"  **この package はまだ資源へ移していませんが、新しく足すのは駄目です。**\n"+
				"  足したぶんを internal/i18n の資源へ入れてください。",
				owner, got, want)
		case got < want:
			t.Errorf("%s の日本語の文言が %d 件に減りました（表に書いてあるのは %d 件）。\n"+
				"  test/internal/testdesign の japaneseAllowance の数を %d へ下げてください。\n"+
				"  **0 になったら行ごと消すこと。**",
				owner, got, want, got)
		}
	}
	t.Logf("走査したファイル: %d 件 / まだ移していない文言: %d 件", checked, totalOf(found))
}

// posRanges は「この中は見ない」という位置の範囲の集まりである。
type posRanges []struct{ from, to token.Pos }

// contains はその位置が範囲のどれかに入っているかを返す。
//
// pos: 調べる位置。
// 戻り値: 入っていれば true。
func (r posRanges) contains(pos token.Pos) bool {
	for _, span := range r {
		if pos >= span.from && pos < span.to {
			return true
		}
	}
	return false
}

// exemptRanges は逃がす呼び出しの範囲を集める。
//
// **逃がすのは2つだけである。**
//
//	panic(...)          … 開発中にしか出ない。利用者の画面には出ない
//	logger.Warn(...) 等 … 運用者が読むログ。画面に出す文言とは別物である
//
// file: 読み込んだファイル。
// 戻り値: 中を見ない範囲。
func exemptRanges(file *ast.File) posRanges {
	var out posRanges
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isExemptCall(call) {
			return true
		}
		out = append(out, struct{ from, to token.Pos }{call.Pos(), call.End()})
		return true
	})
	return out
}

// isExemptCall はその呼び出しが逃がす対象かを返す。
//
// call: 調べる呼び出し。
// 戻り値: panic かログの出力なら true。
func isExemptCall(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name == "panic"
	case *ast.SelectorExpr:
		if !japaneseLogMethods[fn.Sel.Name] {
			return false
		}
		receiver := rightmostName(fn.X)
		return receiver == "logger" || receiver == "slog"
	}
	return false
}

// calleeName は `fmt.Sprintf` のような呼び出し先の名前を返す。
//
// call: 調べる呼び出し。
// 戻り値の1つ目: `パッケージ名.関数名`。
// 戻り値の2つ目: その形で読み取れたかどうか。
func calleeName(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return pkg.Name + "." + sel.Sel.Name, true
}

// rightmostName は式の一番右の識別子の名前を返す（`o.logger` なら `logger`）。
//
// expr: 調べる式。
// 戻り値: 識別子の名前（読み取れなければ空文字）。
func rightmostName(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	case *ast.CallExpr:
		return rightmostName(v.Fun)
	}
	return ""
}

// containsJapanese はその文字列に日本語の文字が含まれるかを返す。
//
// **ひらがな・カタカナ・漢字と、全角の記号を見る。**`（`）`「`」`。`、` も対象である
// （`%s（%s）` のような、記号だけが日本語のまま残った形を拾うため）。
//
// s: 調べる文字列。
// 戻り値: 含まれていれば true。
func containsJapanese(s string) bool {
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r), unicode.Is(unicode.Han, r):
			return true
		case r >= 0x3000 && r <= 0x303f: // 句読点・かぎ括弧などの記号
			return true
		case r >= 0xff01 && r <= 0xff60: // 全角の英数字と記号
			return true
		}
	}
	return false
}

// allowanceOwner は、そのファイルがどの逃がし先に当たるかを返す。
//
// rel: リポジトリの root からの相対パス（`/` 区切り）。
// 戻り値の1つ目: 当たった逃がし先のキー。
// 戻り値の2つ目: 逃がし先があったかどうか。
func allowanceOwner(rel string) (string, bool) {
	best := ""
	for prefix := range japaneseAllowance {
		if rel != prefix && !strings.HasPrefix(rel, prefix+"/") {
			continue
		}
		// **より長い（より細かい）指定を優先する。**ファイル指定が package 指定に負けない。
		if len(prefix) > len(best) {
			best = prefix
		}
	}
	return best, best != ""
}

// sortedKeys は map のキーを昇順で返す（出力の順番を固定するため）。
//
// m: 並べる map。
// 戻り値: 昇順に並べたキー。
func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// totalOf は件数の合計を返す。
//
// m: 数える map。
// 戻り値: 合計。
func totalOf(m map[string]int) int {
	total := 0
	for _, v := range m {
		total += v
	}
	return total
}
