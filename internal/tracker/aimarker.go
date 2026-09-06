package tracker

import (
	"strings"
	"unicode"

	"github.com/maimuzo/continuo/internal/config"
)

// このファイルは、投稿する本文へ「機械が書いた」の印を差し込む処理である（設計 3-82）。
//
// **`internal/config` には置かない。**あちらは「利用者が何を設定したか」を持つ package であり、
// **コメントの本文を編めるのはこの package だけである**（唯一の呼び出し元が `ComposeCommentBody`）。
// **印の綴り（`config.AIMarker`）だけは `config` に残す。**
// `ProgressMarker` / `PlanMarker` と並べておかないと、印の一覧が2箇所に割れる。

// commentOpen と commentClose は HTML のコメントの囲みである。
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
	commentOpen  = "<!--"
	commentClose = "-->"
)

// withAIMarker は、本文の先頭に並ぶ印の、いちばん後ろへ config.AIMarker を1行足す（設計 3-82）。
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
// 最後の `}` までを取る。**config.AIMarker は `{` も `}` も持たない。**
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
// 戻り値: 印を1行足した本文。**印の行より後ろは1文字も書き換えない。**
// **書き換えるのは2つだけである。**
//
//	足す先の直前が改行でなければ、改行を1つ補う（印だけのコメントで、2つの印が1行に繋がるため）
//	本文の先頭が空白だけなら、その空白を落とす（残すと1行目が空白だけの行になる）
//
// **足す行の改行は、本文に合わせる。**CRLF の本文には CRLF で足す。
// **行の区切りは LF だけで数える。**CR だけで改行する本文は、continuo のどの経路も作らない。
func withAIMarker(body string) string {
	// **先頭の空白を読み飛ばす。**読む側が `TrimSpace` するので、ここも同じところから見る。
	head := len(body) - len(strings.TrimLeftFunc(body, unicode.IsSpace))
	insert := head
	// **位置は offset 1本で持つ。**`rest` を別に持つと、行を読み飛ばすたびに
	// **2つを揃えて進める義務が増え、片方を忘れてもコンパイラは教えてくれない。**
	for offset := head; offset < len(body); {
		rest := body[offset:]
		line := rest
		advance := len(rest)
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			line = rest[:i]
			advance = i + 1
		}
		offset += advance
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			// 空行。印の並びはまだ続きうるので、止めずに読み進める。
		case !isMarkerLine(line):
			// **本文が始まった。**ここから先の `<!--` は引用であって名乗りではない。
			return spliceAIMarker(body, insert)
		case strings.HasPrefix(trimmed, config.AIMarker):
			// **先頭の印の並びに、既に印がある。**二重に付けない。
			// **`<!-- continuo:ai --> 補足` のように後ろへ書き足した行も数える。**
			// `isMarkerLine` がその形を印の行として通すので、
			// **完全一致で見ると、印が2つ並ぶ本文を作ってしまう。**
			return body
		default:
			// 印の行。**この行の直後を、いまの挿入位置とする。**
			insert = offset
		}
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
// **そのときは、そのコメントの前へ印を置く。**
// **読む側は、`<!--` だけの行を印として数えない**（`IsMarked` も `FetchComments` も CI の正規表現も、
// 印そのもので始まっているかを見る）。**だから前へ置いても、どの判定も動かない。**
//
// **いまの経路では、その本文は作れない。**`withAIMarker` を呼ぶのは
// `ComposeCommentBody` 1つで、そこへ来る本文は continuo 自身の文言か、
// 持ち回りの印そのもので始まる。**この判定は、呼び出し元が増えたときのための備えである。**
//
// **閉じの後ろに本文が続く行は、印の行として数える。**
// `<!-- 方針 --> production へは push しないでください。` のような1行の書き方があり
// （[internal/prompt/prompt.go](../prompt/prompt.go) の `stripCommentsFromLine` がそう書いている）、
// **その形だと、印は本文の1行の下へ入る。**見た目は良くない。
//
// **それでも数えるほうを採る。**数えないと、その行より**前**へ印を入れることになるからである。
// **読む側は `TrimSpace(body)` してから先頭を見るので、`<!-- continuo:bid --> 立候補` のような
// 1行を先頭に持つ本文は、`IsMarked` からは持ち回りのコメントに見える。**
// **そこへ印を先に入れると、`IsMarked` も `FetchComments` も CI の正規表現も同時に外れる。**
// **見た目が悪いことと、先頭一致が全部外れることを比べて、後者を避ける。**
//
// line: 見る行（行末の改行は含まない）。
// 戻り値: 行頭ちょうどの `<!--` で始まり、同じ行に `-->` があれば真。
func isMarkerLine(line string) bool {
	if !strings.HasPrefix(line, commentOpen) {
		return false
	}
	return strings.Contains(line[len(commentOpen):], commentClose)
}

// spliceAIMarker は、本文の指定の位置へ config.AIMarker を1行差し込む。
//
// body: 差し込む前の本文。
// at: 差し込む位置（バイト単位）。
// 戻り値: 差し込んだ本文。
func spliceAIMarker(body string, at int) string {
	prefix, suffix := body[:at], body[at:]
	// **改行の綴りは本文全体から見る。**差し込む位置が先頭でも、
	// **本文が CRLF なら CRLF で足す。**直前の行だけを見ると、
	// **先頭に印が1つも無い本文（git の失敗をそのまま貼ったものなど）で LF が混ざる。**
	eol := "\n"
	if strings.Contains(body, "\r\n") {
		eol = "\r\n"
	}
	// **空白だけの前置きは、空行にして残さない。**
	// `"  素の本文"` のような本文で、1行目が空白だけの行になる。
	if strings.TrimSpace(prefix) == "" {
		prefix = ""
	}
	// **行の途中へ足さない。**末尾に改行の無い本文では、印が前の行に繋がる。
	if prefix != "" && !strings.HasSuffix(prefix, "\n") {
		prefix += eol
	}
	// **本文の末尾へ足すときは、改行を増やさない。**
	if suffix == "" {
		return prefix + config.AIMarker
	}
	return prefix + config.AIMarker + eol + suffix
}
