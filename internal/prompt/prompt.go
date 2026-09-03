// Package prompt は、continuo が Claude Code へ最初に送る指示書を組み立てる
// （設計 5-3 / 5-3c。#40（WORKFLOW.md のプロンプトを、仕組みの部分とプロジェクト固有の部分に分ける））。
//
// **送る文面は3つの断片からできている。**
//
//	組み込みの前半   builtin.md の、目印の行より上。continuo の実行ファイルの中にある
//	本文            WORKFLOW.md の閉じの --- より下。利用者が書く。空でもよい
//	組み込みの後半   builtin.md の、目印の行より下
//
// **本文を真ん中に挟む。**仕組みの締めくくり（表明の1行の説明）が必ず最後に来るようにする。
// 末尾に足す形にすると、利用者の文が仕組みの説明より後ろに来て、打ち消しやすくなる。
//
// **3つは別々に解釈して、別々に変数展開してから連結する。**1つのテンプレートへ
// 連結してから解釈しない。理由は2つある。
//
//   - **誤りがどのファイルの何行目かを名指しできる。**連結すると行番号が合算されて意味を失う
//   - **本文が `{{if}}` を開いたまま終えても、組み込みの後半を飲み込めない**
//
// **テンプレートを作る口はこのパッケージの newTemplate だけである。**
// `missingkey=error` と、未知の変数へ回り込める組み込み関数の封じ込めを、
// 1箇所で掛けるためである（設計 5-3c）。**ここ以外で `template.New` を呼んではならない。**
package prompt

import (
	_ "embed"
	"strings"
	"text/template"

	"github.com/maimuzo/continuo/internal/i18n"
)

// Marker は builtin.md を前半と後半に切る目印の行である。
//
// **この行そのものは、送る文面には残らない。**
//
// **HTML のコメントにしてある。**markdown として読んだときに見えず、
// builtin.md を1枚の指示書として人間が通して読める。
const Marker = "<!-- continuo:project-specific-prompt -->"

// builtinName は組み込みのプロンプトのファイル名である。エラーの文言に出す。
const builtinName = "builtin.md"

// workflowName は WORKFLOW.md のファイル名である。本文の誤りの文言に出す。
const workflowName = "WORKFLOW.md"

// 断片の名前である。テンプレートの名前としてそのまま使うので、
// `text/template` のエラー（`template: <名前>:<行>: …`）にこの文字列が出る。
const (
	// NameBuiltinHead は組み込みの前半の名前である。**行番号は builtin.md の行番号と一致する。**
	NameBuiltinHead = builtinName + "#head"
	// NameBuiltinTail は組み込みの後半の名前である。
	// **行番号は目印の行の次を1行目として数えたものである**（builtin.md の行番号ではない）。
	NameBuiltinTail = builtinName + "#tail"
	// NameWorkflowBody は WORKFLOW.md の本文の名前である（設計 5-3c）。
	//
	// **行番号は front matter の閉じの `---` の次を1行目として数えたものである**
	// （WORKFLOW.md の行番号ではない）。
	NameWorkflowBody = workflowName + "#body"
)

//go:embed builtin.md
var builtinRaw string

// builtinHead / builtinTail は builtin.md を目印の行で切った結果である。
//
// **パッケージの初期化で1回だけ切る。**送るたびに切ると、切り方の誤りが
// 着手のときまで表に出ない。
var builtinHead, builtinTail = splitBuiltin(builtinRaw)

// StripComments は、送る文面から HTML のコメントを取り除く（設計 5-3m）。
//
// **取り除くのは、行頭が `<!--` で始まる行だけである。**
// 字下げしてあるものは残す。組み込みのプロンプトは、エージェントに書かせる印
// （`<!-- continuo:agent -->` など）を4桁の字下げでコード片として見せており、
// **落とすとエージェントが印を書けなくなる。**
//
// **``` で囲んだ中も残す。**利用者が「これは送りたい」と決めたものを囲む口である。
//
// **なぜ取り除くか。**`WORKFLOW.md` の雛形は、利用者へ書き方を説明するために
// HTML のコメントを使う。**それはエージェントへ送る情報ではない。**
// 送ると、送る文面の1割近くが「利用者向けの説明」で埋まる（実測: 36個 / 410行）。
//
// s: 取り除く前の文字列。
// 戻り値: 行頭のコメントを落とした文字列。**落とした行のぶん、空行は詰めない**
// （詰めると、節と節のあいだの空行が消えて markdown が壊れる）。
func StripComments(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	inComment := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			out = append(out, line)
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}
		if inComment {
			if strings.Contains(line, "-->") {
				inComment = false
			}
			continue
		}
		// **行頭でなければ落とさない。**字下げしたものはコード片である。
		if strings.HasPrefix(line, "<!--") {
			if !strings.Contains(line, "-->") {
				inComment = true
			}
			continue
		}
		out = append(out, line)
	}
	return squeezeBlank(strings.Join(out, "\n"))
}

// dropEmptySections は、中身が1行も無くなった見出しを落とす。
//
// **コメントを落とすと、見出しだけが残ることがある。**
// WORKFLOW.md の雛形は「ここに書いてください」を HTML のコメントで案内しており、
// **利用者が何も書かなければ、その節は見出しだけになる。**
// **見出しだけの節は、エージェントへ渡す情報を1つも持たない。**
//
// **深い見出しを持つ見出しは残す。**`# 5. 共通ルール` の直後が `## 5-1. …` でも、
// 5 は空ではない。
//
// s: 落とす前の文字列。
// 戻り値: 中身のある見出しだけを残した文字列。
func dropEmptySections(s string) string {
	lines := strings.Split(s, "\n")
	keep := make([]bool, len(lines))
	inFence := make([]bool, len(lines))
	fence := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fence = !fence
			inFence[i] = true
			continue
		}
		inFence[i] = fence
	}
	for i, line := range lines {
		keep[i] = true
		if inFence[i] || !strings.HasPrefix(line, "#") {
			continue
		}
		level := headingLevel(line)
		keep[i] = false
		for j := i + 1; j < len(lines); j++ {
			if !inFence[j] && strings.HasPrefix(lines[j], "#") {
				// **深い見出しなら中身とみなす。**同じか浅ければ、この節は終わり。
				if headingLevel(lines[j]) > level {
					keep[i] = true
				}
				break
			}
			if strings.TrimSpace(lines[j]) != "" {
				keep[i] = true
				break
			}
		}
	}
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if keep[i] {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// headingLevel は markdown の見出しの深さを返す。
//
// line: 行頭が `#` で始まる行。
// 戻り値: 先頭の `#` の数。
func headingLevel(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	return n
}

// squeezeBlank は、3つ以上続く改行を2つへ畳む。
//
// **コメントを落とした跡に空行が並ぶのを防ぐ。**
//
// s: 畳む前の文字列。
// 戻り値: 空行が2つ以上続かない文字列。
func squeezeBlank(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}

// splitBuiltin は builtin.md を目印の行で前半と後半に切る。
//
// raw: builtin.md の全文。
// 戻り値: 目印の行より上と、目印の行より下。**目印の行そのものはどちらにも入らない。**
// 目印が無ければ全文が前半になり、後半は空文字になる（その状態は
// TestBuiltin_目印がちょうど1つある が落とす）。
func splitBuiltin(raw string) (string, string) {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == Marker {
			return strings.Join(lines[:i], "\n"), strings.Join(lines[i+1:], "\n")
		}
	}
	return raw, ""
}

// BuiltinRaw は builtin.md の全文を、目印の行を含めたまま返す。
//
// **テストと、目印の数を数える検査のためにある。**送る文面が要るなら Builtin を使う。
//
// 戻り値: builtin.md の全文。
func BuiltinRaw() string { return builtinRaw }

// BuiltinHead は組み込みの前半を返す。
//
// 戻り値: builtin.md の、目印の行より上。
func BuiltinHead() string { return builtinHead }

// BuiltinTail は組み込みの後半を返す。
//
// 戻り値: builtin.md の、目印の行より下。
func BuiltinTail() string { return builtinTail }

// Builtin は組み込みだけで組み立てた全文を返す（本文を挟まない形）。
//
// **`continuo prompt --show --builtin` が出すのはこれである。**
// **利用者が、自分が書いた本文と仕組みの側を見比べる相手**として読む（設計 5-3f）。
// 組み込みが既に言っていることを、本文に二重に書かずに済む。
//
// 戻り値: 前半と後半を連結した、変数展開していない全文。
func Builtin() string {
	return join([]string{StripComments(builtinHead), StripComments(builtinTail)})
}

// Fragment は、送る文面を組み立てる断片1つである。
type Fragment struct {
	// Name は断片の名前である。テンプレートの名前としても使うので、
	// 解釈の誤りのエラーにこの文字列がそのまま出る。
	Name string
	// Text は断片の中身（変数展開していない文字列）である。
	Text string
	// Path は、その断片が来たファイルの絶対パスである。
	// **組み込みの断片は空文字である**（実行ファイルの中にあり、ファイルとして存在しない）。
	Path string
}

// Fragments は、送る文面を組み立てる断片の並びである。
//
// **並んでいる順に連結する。**
type Fragments struct {
	// items は連結する順に並んだ断片である。**空文字の断片は入っていない。**
	items []Fragment
	// bodyPath は本文が来た WORKFLOW.md の絶対パスである。
	//
	// **本文が空でも埋める。**内訳の「本文はありません」に、どのファイルの話かを添えるためである。
	bodyPath string
	// hasBody は本文に中身があったかどうかである。
	//
	// **空白だけなら偽である。**
	hasBody bool
}

// Build は、WORKFLOW.md の本文から送る文面の断片を決める（設計 5-3c）。
//
// **並びは、組み込みの前半 + 本文 + 組み込みの後半で固定である。**
// **本文が空白だけなら、組み込みの前半 + 組み込みの後半になる。**
//
// **本文を「全文の差し替え」として扱わない。**そう扱うと、continuo が仕組みの説明を直しても、
// 本文を書いた利用者には二度と届かない（設計 5-3c）。
//
// body: WORKFLOW.md の front matter より後ろ。空でもよい。
// bodyPath: 本文が来た WORKFLOW.md の絶対パス。**本文が空でも埋める**（内訳に出す）。
// 戻り値: 連結する順に並んだ断片。
func Build(body, bodyPath string) Fragments {
	f := Fragments{bodyPath: bodyPath, hasBody: strings.TrimSpace(body) != ""}
	f.add(NameBuiltinHead, StripComments(builtinHead), "")
	f.add(NameWorkflowBody, StripComments(body), bodyPath)
	f.add(NameBuiltinTail, StripComments(builtinTail), "")
	return f
}

// add は、中身のある断片だけを並びへ足す。
//
// name: 断片の名前。
// text: 断片の中身。**空白だけなら足さない。**
// path: その断片が来たファイルの絶対パス。
func (f *Fragments) add(name, text, path string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	f.items = append(f.items, Fragment{Name: name, Text: text, Path: path})
}

// Items は連結する順に並んだ断片を返す。
//
// 戻り値: 断片の並び。**呼び出し側は書き換えてはならない。**
func (f Fragments) Items() []Fragment { return f.items }

// HasBody は WORKFLOW.md の本文に中身があったかどうかを返す。
//
// 戻り値: 中身があれば真。**空白だけなら偽である。**
func (f Fragments) HasBody() bool { return f.hasBody }

// BodyPath は本文が来た WORKFLOW.md の絶対パスを返す。
//
// 戻り値: 絶対パス。**本文が空でも埋まっている。**
func (f Fragments) BodyPath() string { return f.bodyPath }

// Text は、変数展開していない全文を返す（`continuo prompt --show` が出すもの）。
//
// 戻り値: 断片を連結した文字列。
func (f Fragments) Text() string {
	parts := make([]string, 0, len(f.items))
	for _, it := range f.items {
		parts = append(parts, it.Text)
	}
	return join(parts)
}

// join は断片を連結する。
//
// **前後の改行の数をそろえてから連結する。**本文が改行で終わっていないと、
// 次の見出しが前の行にくっついて markdown として壊れる。逆に改行が3つあると、
// 送る文面が断片の書き方で変わってしまう。**断片のあいだは必ず空行1つにする。**
//
// **落とすのは改行だけである。**行頭の空白は落とさない（組み込みのプロンプトは
// コマンドの例を4桁の字下げで書いており、落とすとコードブロックとして読めなくなる）。
//
// parts: 連結する文字列の並び。**空白だけのものは呼び出し側が落としてある。**
// 戻り値: 空行1つで区切って連結し、末尾に改行を1つ付けた文字列。
func join(parts []string) string {
	trimmed := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(strings.ReplaceAll(p, "\r\n", "\n"), "\n")
		if strings.TrimSpace(p) == "" {
			continue
		}
		trimmed = append(trimmed, p)
	}
	if len(trimmed) == 0 {
		return ""
	}
	return dropEmptySections(strings.Join(trimmed, "\n\n") + "\n")
}

// Render は断片ごとに変数展開してから連結する（設計 5-3c）。
//
// data: テンプレートへ渡す変数。**9つの名前だけを持たせること**（設計 5-3）。
// 戻り値の1つ目: 変数展開した全文。
// 戻り値の2つ目: 解釈できなかった、または一覧に無い変数を参照していた断片のエラー。
// **どの断片の何行目かがエラーの文言に入る。**
func (f Fragments) Render(data map[string]any) (string, error) {
	parts := make([]string, 0, len(f.items))
	for _, it := range f.items {
		out, err := renderOne(it, data)
		if err != nil {
			return "", err
		}
		parts = append(parts, out)
	}
	return join(parts), nil
}

// renderOne は断片1つを変数展開する。
//
// it: 変数展開する断片。
// data: テンプレートへ渡す変数。
// 戻り値: 変数展開した文字列と、失敗した理由。
func renderOne(it Fragment, data map[string]any) (string, error) {
	tmpl, err := newTemplate(it.Name).Parse(it.Text)
	if err != nil {
		return "", i18n.Errorf(i18n.KeyPromptParseFailed, it.Name, err)
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		return "", i18n.Errorf(i18n.KeyPromptRenderFailed, it.Name, err)
	}
	return b.String(), nil
}

// Validate は、断片が解釈でき、一覧にある変数だけを使っていることを確かめる（設計 5-3c）。
//
// **作り物の issue で2回変数展開する。**1回目は `.attempt` を空、2回目は 2 にする。
// **`{{if .attempt}}` の中は、空のときには一度も解釈されない**ためである。
//
// **これで全部を捕まえられるわけではない。**`{{if eq .issue.state "Done"}}` のように
// 値そのもので分かれる枝の中までは届かない。**doctor の文言は、そう言い切らない形にしてある。**
//
// 戻り値: 最初に見つけた誤り。誤りが無ければ nil。
func (f Fragments) Validate() error {
	for _, attempt := range []any{nil, 2} {
		data := SampleData()
		data["attempt"] = attempt
		if _, err := f.Render(data); err != nil {
			return err
		}
	}
	return nil
}

// SampleData は、検査に使う作り物の issue の変数を返す（設計 5-3c）。
//
// **値は全部「空でないもの」にする。**空文字だと `{{if .issue.title}}` の中が検査されない。
// **`.attempt` は呼び出し側が入れ直す**（1回目は nil、2回目は回数）。
//
// 戻り値: 9つの名前を全部持つ変数の一覧。
func SampleData() map[string]any {
	return map[string]any{
		"issue": map[string]any{
			"identifier": "octocat/hello-world#42",
			"owner":      "octocat",
			"repo":       "hello-world",
			"number":     42,
			"url":        "https://github.com/octocat/hello-world/issues/42",
			"title":      "検査に使う作り物の issue",
			"state":      "Ready",
			"labels":     []string{"bug"},
		},
		"attempt": nil,
	}
}

// newTemplate はテンプレートを1つ作る。**テンプレートを作る口はここだけである。**
//
// **`missingkey=error` を付ける。**準拠する openai/symphony の SPEC.md 5.4 が
// "Unknown variables MUST fail rendering"（**訳:** 未知の変数は変数展開を失敗させ
// なければならない）を求めている。黙って空文字を埋めない。
//
// **`index` を封じる。**`missingkey=error` が見るのは `.foo` の形だけで、
// `{{index .issue "nope"}}` は誤りにならずに何も出さない。**逃げ道を1つ残すと、
// 「知らない変数は無い」と言えなくなる。**
//
// name: テンプレートの名前。**断片の名前を渡すこと。**エラーの文言にそのまま出る。
// 戻り値: 解釈の前のテンプレート。
func newTemplate(name string) *template.Template {
	return template.New(name).Option("missingkey=error").Funcs(sealedFuncs)
}

// sealedFuncs は、未知の変数へ回り込める組み込み関数を塞ぐ差し替えである。
//
// **塞ぐのは `index` だけである。**他の組み込み関数（`printf` / `len` など）は、
// 引数に書いた名前の解釈が先に走るので `missingkey=error` が効く。
var sealedFuncs = template.FuncMap{
	// index は封じる。**呼ぶと必ず誤りになる。**
	//
	// item: 元の index が取る引数（受け取るだけで使わない）。
	// 戻り値: 常に nil と、使えないことを説明するエラー。
	"index": func(item ...any) (any, error) {
		return nil, i18n.Errorf(i18n.KeyPromptIndexSealed)
	},
}
