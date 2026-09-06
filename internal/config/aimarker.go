package config

import (
	"strings"
	"unicode"
)

// AIMarker は、人間ではなく機械が書いたコメントに付ける印である（設計 3-82）。
//
// **この印が言うのは「この本文を打鍵したのは機械である」ことだけである。**
// continuo 本体（Go のプログラム）も、continuo が起動した Claude Code も、
// **人間と直接やりとりしている Claude Code も、同じこの印を付ける。**
//
// **なぜ要るか。**エージェントも continuo も人間も、同じ GitHub アカウントで投稿する
// （[internal/tracker/ghuser.go](../tracker/ghuser.go) の 24-25行）。
// **投稿者でも `author_association` でも見分けられない。**
// **人間が前に出した指示を探して、その下から読み直す**という読み方が、いまはできない。
//
// **印が言わないことが2つある。**
//
//	書いてある内容が誰の決定か … 人間の決定を AI が記録したコメントにも、この印は付く
//	偽れないこと           … 印は誰でも書ける文字列であり、認証ではない（設計 3-65）
//
// **設定キーにしない。**理由は ProgressMarker と同じである。
// `tracker.comments.marker` は機械ごとに違う値を書けるので、
// **別の機械が書いた印を読めなくなる。**「印が無ければ人間」という読み方は、
// **印が1つに固定されていて初めて成り立つ。**
//
// **組み込みのプロンプト（[internal/prompt/builtin.md](../prompt/builtin.md)）が
// エージェントへ書かせる文字列と、1文字も違ってはならない。**
const AIMarker = "<!-- continuo:ai -->"

// aiMarkerCommentOpen は HTML のコメントの開きである。
//
// **行頭ちょうどの `<!--` だけを印の行とみなす。**字下げした行は本文である
// （[internal/handoff/assess.go](../handoff/assess.go) の `StartsAsProgressReport` と同じ決まり）。
//
// **1行目だけは例外になる。**本文全体の先頭の空白を先に落とすので、
// **字下げした1行目の印は、印として通る。**
// **`handoff.IsMarked` も `FetchComments` も `TrimSpace(body)` してから先頭を見るので、
// そちらでも同じように通る。**ここだけ通さないと、その2つと判定がずれる。
const aiMarkerCommentOpen = "<!--"

// WithAIMarker は、本文の先頭に並ぶ印の、いちばん後ろへ AIMarker を1行足す（設計 3-82）。
//
// **既にある印を1つも動かさない。**先頭へ割り込ませてはならない。
// **先頭が特定の印であることを見ている判定が、本番に7箇所ある。**
// とくに CI の2本（`design-review-result` と `code-review-result`）は
// `continuo init` が利用者のリポジトリへ置いたきりで、**continuo の版を上げても書き換わらない。**
// **先頭へ入れると、その project の pull request が全部赤になる。**
//
// **既に印を持っている本文は、そのまま返す。**二重に付けない。
//
// 例。
//
//	"<!-- continuo:self -->\n本文"        → "<!-- continuo:self -->\n<!-- continuo:ai -->\n本文"
//	"<!-- continuo:bid -->\n{…}\n\n散文"  → "<!-- continuo:bid -->\n<!-- continuo:ai -->\n{…}\n\n散文"
//	"本文だけ"                            → "<!-- continuo:ai -->\n本文だけ"
//
// **入札のコメントを壊さない。**`payloadAfterMarker`
// （[internal/handoff/handoff.go](../handoff/handoff.go)）は印の後ろの最初の `{` から
// 最後の `}` までを取る。**AIMarker は `{` も `}` も持たない。**
//
// **本文の先頭の空白は、読む側と同じところから見る。**
// `handoff.IsMarked` も `payloadAfterMarker` も `FetchComments` も
// `strings.TrimSpace(body)` してから先頭を見るので、**ここで落とさないと判定がずれる。**
// **先頭に空行が1つあるだけで、印が本物の印より前に入り、`IsMarked` が偽になる。**
// そうなると入札も hold も読み戻せず、**continuo は自分が取った担当を人間のものと読む。**
//
// **空行では止めない。**印と印のあいだに空行を挟む書き方がありうる
// （`handoff.StartsAsProgressReport` と同じ決まり）。
// **ただし挿入するのは、最後に見つけた印の行の直後である。**後ろの空行までは越えない。
//
// body: 印を足す前の本文。
// 戻り値: 印を1行足した本文。**改行の綴りは変えない**（元の位置へ差し込むだけである）。
// **足す先の直前が改行でなければ、改行を1つ補う。**補わないと、
// 末尾に改行の無い本文（印だけのコメント）で、2つの印が1行に繋がる。
func WithAIMarker(body string) string {
	// **先頭の空白を読み飛ばす。**読む側が `TrimSpace` するので、ここも同じところから見る。
	head := len(body) - len(strings.TrimLeftFunc(body, unicode.IsSpace))
	offset, insert := head, head
	for rest := body[head:]; rest != ""; {
		line := rest
		advance := len(rest)
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			line = rest[:i]
			advance = i + 1
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			// 空行。印の並びはまだ続きうるので、止めずに読み進める。
		case !strings.HasPrefix(line, aiMarkerCommentOpen):
			// **本文が始まった。**ここから先の `<!--` は引用であって名乗りではない。
			return spliceAIMarker(body, insert)
		case trimmed == AIMarker:
			// 既に付いている。二重に付けない。
			return body
		default:
			// 印の行。**この行の直後を、いまの挿入位置とする。**
			insert = offset + advance
		}
		offset += advance
		rest = rest[advance:]
	}
	return spliceAIMarker(body, insert)
}

// spliceAIMarker は、本文の指定の位置へ AIMarker を1行差し込む。
//
// body: 差し込む前の本文。
// at: 差し込む位置（バイト単位）。
// 戻り値: 差し込んだ本文。
func spliceAIMarker(body string, at int) string {
	prefix, suffix := body[:at], body[at:]
	// **行の途中へ足さない。**末尾に改行の無い本文では、印が前の行に繋がる。
	if prefix != "" && !strings.HasSuffix(prefix, "\n") {
		prefix += "\n"
	}
	// **本文の末尾へ足すときは、改行を増やさない。**
	if suffix == "" {
		return prefix + AIMarker
	}
	return prefix + AIMarker + "\n" + suffix
}
