package config

import "strings"

// AIMarker は、人間ではなく機械が書いたコメントに付ける印である（設計 3-82）。
//
// **この印が言うのは「この本文を打鍵したのは機械である」ことだけである。**
// continuo 本体（Go のプログラム）も、continuo が起動した Claude Code も、
// **人間と直接やりとりしている Claude Code も、同じこの印を付ける。**
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
// body: 印を足す前の本文。
// 戻り値: 印を1行足した本文。**改行の綴りは変えない**（元の位置へ差し込むだけである）。
func WithAIMarker(body string) string {
	rest := body
	offset := 0
	for rest != "" {
		line := rest
		advance := len(rest)
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			line = rest[:i]
			advance = i + 1
		}
		if !strings.HasPrefix(line, aiMarkerCommentOpen) {
			break
		}
		if strings.TrimSpace(line) == AIMarker {
			// 既に付いている。二重に付けない。
			return body
		}
		offset += advance
		rest = rest[advance:]
	}
	return body[:offset] + AIMarker + "\n" + body[offset:]
}
