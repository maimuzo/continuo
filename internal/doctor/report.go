package doctor

import (
	"fmt"
	"io"
	"strings"
)

// 検査の見出し語である（設計 3-32）。**この語で固定する。**
//
// 出力に出る語をここ以外で組み立ててはならない。人間はこの語で「どの前提が欠けたか」を
// 覚えるので、揺れると同じものが2つの名前で呼ばれることになる。
const (
	// LabelConfig は WORKFLOW.md が読めて front matter が検証を通るかの検査である。
	LabelConfig = "設定ファイル"
	// LabelHerdr は herdr の socket の ping の protocol が設定と一致するかの検査である。
	LabelHerdr = "herdr"
	// LabelGHAuth は `gh auth status` の scope に project が単独で並んでいるかの検査である。
	LabelGHAuth = "gh の認証"
	// LabelBoard は Bootstrap が通り、active_states の選択肢名が全部あるかの検査である。
	LabelBoard = "ボード"
	// LabelClone は対象リポジトリが `ghq list -p -e` で見つかるかの検査である。
	LabelClone = "clone"
	// LabelTrust は対象リポジトリの clone のパスが `~/.claude.json` で承認済みかの検査である。
	LabelTrust = "信頼登録"
	// LabelCredentials は rate_limit の設定に応じて環境変数かファイルがあるかの検査である。
	LabelCredentials = "資格情報"
)

// Symbol は検査1件の結果である（設計 3-32 の3値）。
type Symbol string

const (
	// SymbolOK は「通った」である。終了コードに影響しない。
	SymbolOK Symbol = "✓"
	// SymbolMissing は「足りない」である。**1つでもあれば終了コードは 1 になる。**
	SymbolMissing Symbol = "✗"
	// SymbolUnknown は「確かめられなかった」である。動くかもしれないので終了コードは 0 のままにする。
	SymbolUnknown Symbol = "!"
)

// worse は2つの記号のうち「重いほう」を返す（✗ > ! > ✓）。
//
// **1つの見出し語が対象を複数持つときに使う**（clone と信頼登録はボードに載っている
// リポジトリの数だけ対象がある）。1件でも足りなければ見出し語全体を ✗ にする。
//
// a: 比較する記号。
// b: 比較する記号。
// 戻り値: 重いほうの記号。
func worse(a, b Symbol) Symbol {
	rank := func(s Symbol) int {
		switch s {
		case SymbolMissing:
			return 2
		case SymbolUnknown:
			return 1
		default:
			return 0
		}
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}

// Result は検査1件の結果である。
type Result struct {
	// Label は見出し語である（LabelConfig などの定数のいずれか）。
	Label string
	// Symbol は3値の結果である。
	Symbol Symbol
	// Detail は記号の右に出す1行の説明である。**なぜその記号になったか**を書く。
	Detail string
	// Notes は対象ごとの内訳である（リポジトリが複数あるときの1件ずつの結果）。
	// Detail の下に、同じ桁位置で並べて出す。
	Notes []string
	// Remedies は直し方である。`→ ` を頭に付けて出す。
	Remedies []string
}

// Report は7項目の検査結果をまとめたものである。
type Report struct {
	// Results は検査結果を検査した順に並べたものである。
	Results []Result
}

// add は検査結果を1件積む。
//
// res: 積む検査結果。
func (r *Report) add(res Result) {
	r.Results = append(r.Results, res)
}

// Counts は記号ごとの件数を返す。
//
// 戻り値: 通った件数・足りない件数・確かめられなかった件数。
func (r Report) Counts() (ok, missing, unknown int) {
	for _, res := range r.Results {
		switch res.Symbol {
		case SymbolOK:
			ok++
		case SymbolMissing:
			missing++
		case SymbolUnknown:
			unknown++
		}
	}
	return ok, missing, unknown
}

// ExitCode は終了コードを返す（設計 3-32）。
//
// **`✗` が1つでもあれば 1。`!` だけなら 0 である。**
// 確かめられなかっただけのものは「動くかもしれない」ので、人間の手を止めない。
//
// 戻り値: 終了コード（0 か 1）。
func (r Report) ExitCode() int {
	for _, res := range r.Results {
		if res.Symbol == SymbolMissing {
			return 1
		}
	}
	return 0
}

// labelColumn は見出し語を並べる桁数（端末の表示幅）である。
//
// いちばん長い見出し語（`設定ファイル` = 12 桁）が収まり、説明との間に余白が残る幅にしてある。
const labelColumn = 16

// Write は検査結果を人間が読む形で書き出す（設計 3-32 の出力の形）。
//
//	✓ herdr           protocol 19（設定と一致）
//	✗ clone           maimuzo/koetsumugi が見つからない
//	                  → ghq get maimuzo/koetsumugi を実行してください
//
//	2件に問題があります（✗ 1件 / ! 1件）
//
// w: 書き出す先。
// 戻り値: 書き出しに失敗した場合のエラー。
func (r Report) Write(w io.Writer) error {
	var b strings.Builder
	indent := strings.Repeat(" ", 2+labelColumn)

	for _, res := range r.Results {
		b.WriteString(fmt.Sprintf("%s %s%s%s\n", res.Symbol, res.Label, padding(res.Label), res.Detail))
		for _, note := range res.Notes {
			b.WriteString(indent + note + "\n")
		}
		for _, remedy := range res.Remedies {
			b.WriteString(indent + "→ " + remedy + "\n")
		}
	}

	_, missing, unknown := r.Counts()
	b.WriteString("\n")
	switch {
	case missing+unknown == 0:
		b.WriteString(fmt.Sprintf("前提はすべて揃っています（✓ %d件）\n", len(r.Results)))
	case missing == 0:
		// **`!` だけのときを「問題があります」と書かない。**対象リポジトリが0件のとき
		// （ボードが空）もここへ来る。**ボードが空なのは設定の誤りではない**（設計 3-32）。
		b.WriteString(fmt.Sprintf(
			"%d件を確かめられませんでした（✗ 0件 / ! %d件）。足りないものはありません\n",
			unknown, unknown))
	default:
		b.WriteString(fmt.Sprintf("%d件に問題があります（✗ %d件 / ! %d件）\n",
			missing+unknown, missing, unknown))
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// padding は見出し語のうしろに入れる空白を返す。
//
// **日本語の見出し語が混ざるので、文字数ではなく端末の表示幅で揃える。**
//
// label: 見出し語。
// 戻り値: 見出し語の右に入れる空白（表示幅が labelColumn に届かない場合は最低1つ）。
func padding(label string) string {
	n := labelColumn - displayWidth(label)
	if n < 1 {
		n = 1
	}
	return strings.Repeat(" ", n)
}

// displayWidth は端末に表示したときのおおよその桁数を返す。
//
// **CJK の文字を2桁として数える。**`utf8.RuneCountInString` で数えると、
// `設定ファイル` と `clone` が同じ桁に見えず、説明の開始位置が揃わない。
//
// s: 数える文字列。
// 戻り値: 表示幅（桁数）。
func displayWidth(s string) int {
	width := 0
	for _, r := range s {
		// 0x2E80 以降は CJK の部首補助から始まる全角の領域である。
		// ひらがな・カタカナ・漢字・全角の括弧はすべてここに入る。
		if r >= 0x2E80 {
			width += 2
			continue
		}
		width++
	}
	return width
}
