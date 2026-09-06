package config

import (
	"strings"
	"unicode"
)

// AIMarker は、人間ではなく機械が書いたコメントに付ける印である（設計 3-82）。
//
// **この印が言うのは「この本文を打鍵したのは機械である」ことだけである。**
// **continuo が付けるのは2つの経路である。**continuo 本体（Go のプログラム）が書くコメントと、
// continuo が起動した Claude Code が書くコメント（組み込みの指示書が書かせる）。
//
// **人間が自分で動かした Claude Code には、continuo は届かない。**
// そちらへ同じ印を付けさせたい project は、`CLAUDE.md` へ規則を書く
// （文面は [docs/FAQ.md](../../docs/FAQ.md) にある）。
// **だから「印が無ければ人間」は成り立たない。**印が無いことは、
// **人間が打鍵したか、continuo の外の Claude Code が書いたか、この仕組みより前のものか、のどれかである。**
//
// **なぜ要るか。**エージェントも continuo も人間も、同じ GitHub アカウントで投稿する
// （[internal/tracker/ghuser.go](../tracker/ghuser.go) の 23-25行）。
// **投稿者でも `author_association` でも見分けられない。**
// **人間が前に出した指示を探して、その下から読み直す**という読み方が、いまはできない。
//
// **印が言わないことが2つある。**
//
//	書いてある内容が誰の決定か … 人間の決定を AI が記録したコメントにも、この印は付く
//	偽れないこと           … 印は誰でも書ける文字列であり、認証ではない（設計 3-65）
//
// **設定キーにしない。**理由は ProgressMarker と同じである。
// `tracker.comments.marker` も `tracker.comments.self_marker` も機械ごとに違う値を書けるので、
// **別の機械や別の project が書いた印を読めなくなる。**
// **綴りが固定であること自体が、この印の値打ちである。**
// project ごとの設定を知らない読み手でも、機械の本文を当てられる。
//
// **組み込みのプロンプト（[internal/prompt/builtin.md](../prompt/builtin.md)）が
// エージェントへ書かせる文字列と、1文字も違ってはならない。**
const AIMarker = "<!-- continuo:ai -->"

// aiMarkerCommentOpen と aiMarkerCommentClose は HTML のコメントの囲みである。
//
// **行頭ちょうどの `<!--` だけを印の行とみなす。**字下げした行は本文である
// （[internal/handoff/assess.go](../handoff/assess.go) の `StartsAsProgressReport` と同じ決まり）。
//
// **1行目だけは例外になる。**本文全体の先頭の空白を先に落とすので、
// **字下げした1行目の印は、印として通る。**
// **`handoff.IsMarked` も `FetchComments` も `TrimSpace(body)` してから先頭を見るので、
// そちらでも同じように通る。**ここだけ通さないと、その2つと判定がずれる。
//
// **同じ前提に立つ定数が、他に3つある**（`StartsAsProgressReport` の説明にある一覧）。
// **印の形を変えるときは4つとも動かすこと。**
const (
	aiMarkerCommentOpen  = "<!--"
	aiMarkerCommentClose = "-->"
)

// WithAIMarker は、本文の先頭に並ぶ印の、いちばん後ろへ AIMarker を1行足す（設計 3-82）。
//
// **既にある印を1つも動かさない。**先頭へ割り込ませてはならない。
// **本文の先頭から読む判定が、本番に13ある**（一覧は設計 3-82b）。
// **12が「先頭が特定の印で始まるか」、1つが「先頭に並ぶ印を辿るか」である。**
// とくに CI の3本（`design-review-result` / `code-review-result` / `design-review-skipped`）は
// `continuo init` が利用者のリポジトリへ置いたきりで、**continuo の版を上げても書き換わらない。**
// **先頭へ入れると、その project の pull request が全部赤になり、continuo からは直せない。**
//
// **先頭の印の並びに既に印があれば、そのまま返す。**二重に付けない。
// **2行目以降で字下げして引用した印は、この並びに数えない。**そちらは本文であり、
// **印について説明する報告には、名乗りの印を別に足す。**
// **1行目だけは字下げしていても数える。**本文全体の先頭の空白を先に落とすためで、
// **読む側（`TrimSpace` してから先頭を見る）と同じ扱いである。**
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
// 戻り値: 印を1行足した本文。**元の本文は1文字も書き換えない**（差し込むだけである）。
// **足す先の直前が改行でなければ、改行を1つ補う。**補わないと、
// 末尾に改行の無い本文（印だけのコメント）で、2つの印が1行に繋がる。
// **足す行の改行は LF である。**CRLF の本文へ足すと、その1行だけ LF になる。
// **読む側は先頭の1行しか見ないので、判定は変わらない。**
// **行の区切りは LF だけで数える。**CR だけで改行する本文は、continuo のどの経路も作らない。
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
		case !isMarkerLine(line):
			// **本文が始まった。**ここから先の `<!--` は引用であって名乗りではない。
			return spliceAIMarker(body, insert)
		case trimmed == AIMarker:
			// **先頭の印の並びに、既に印がある。**二重に付けない。
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

// isMarkerLine は、その行が1行で閉じた HTML のコメント（＝印の行）かを返す。
//
// **開きだけでは足りない。**`<!--` で始まり、同じ行に `-->` が無い行は、
// **複数行の HTML コメントの開きである。**印の行として数えると、
// **その中へ印を差し込むことになる。**issue の画面では見えず、
// 本文は `<!--` で始まったまま残る。
//
// **いまの経路では、その本文は作れない。**`WithAIMarker` を呼ぶのは
// `tracker.ComposeCommentBody` 1つで、そこへ来る本文は continuo 自身の文言か、
// 持ち回りの印そのもので始まる。**この判定は、呼び出し元が増えたときのための備えである。**
//
// **閉じも見るのは、[internal/prompt/prompt.go](../prompt/prompt.go) の
// `stripCommentsFromLine` が既にそうしているからである。**
//
// **閉じの後ろに本文が続く行も、印の行ではない。**
// `<!-- 方針 --> production へは push しないでください。` のような1行の書き方があり
// （[internal/prompt/prompt.go](../prompt/prompt.go) の `stripCommentsFromLine` がそう書いている）、
// **数えると、印が本文の1行の下へ入る。**
//
// line: 見る行（行末の改行は含まない）。
// 戻り値: 行頭ちょうどの `<!--` で始まり、`-->` で終わっていれば真。
func isMarkerLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(line, aiMarkerCommentOpen) {
		return false
	}
	if !strings.HasSuffix(trimmed, aiMarkerCommentClose) {
		return false
	}
	// **開きと閉じが同じものでないこと。**`<!--` だけの行は複数行コメントの開きである。
	return len(trimmed) >= len(aiMarkerCommentOpen)+len(aiMarkerCommentClose)
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
