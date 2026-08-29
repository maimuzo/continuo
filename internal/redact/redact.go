// Package redact は、continuo が issue へ書く文面から手元の絶対パスを取り除く
// （docs/plans/continuo_design.md 3-63）。
//
// **issue は公開のリポジトリにあることがある。**`/home/<利用者名>/…` をそのまま書くと、
// 利用者名とその機械の構成が公開される。**issue のコメントは編集履歴が残るので、
// あとから消しても取り消せない。**
//
// **縮めるのは、continuo を動かしている機械の home で始まるパスだけである。**
//
// **home の外にあるパスはそのまま出す。**縮めようが無いうえ、伏せてしまうと
// 引き渡しの通知の【調べるところ】が「どこを見ればよいか分からない」ものになる。
// 利用者名が入るのは home の下であり、そこは縮まる。
package redact

import (
	"os"
	"strings"
)

// homeMark は縮めたあとに置く印である。
const homeMark = "~"

// Paths は body の中の絶対パスのうち、continuo を動かしている機械の home で始まるものを
// `~` に縮める。
//
// **continuo が issue へ書く本文は、必ずこの関数を通す**（設計 3-63）。
// 組み立てる場所ごとに縮めると、必ずどこかが漏れる。
//
// **home を引けなければ、body をそのまま返す。**縮められないことを理由に投稿そのものを
// 止めると、人間は「なぜ止まったのか」を知る手立てを失う。
//
// body: issue へ書く本文。
// 戻り値: home を `~` に縮めた本文。
func Paths(body string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return body
	}
	return PathsWithHome(body, home)
}

// PathsWithHome は home を明示して Paths と同じことを行う。
//
// **テストから呼ぶために分けてある。**環境変数を差し替えなくても、任意の home で
// 縮め方を確かめられる。
//
// **`/` を home として渡しても何もしない。**すべての絶対パスが `~` に化けるためである。
//
// body: issue へ書く本文。
// home: 縮める対象の home の絶対パス。末尾のスラッシュは無視する。
// 戻り値: home を `~` に縮めた本文。
func PathsWithHome(body, home string) string {
	home = strings.TrimRight(home, "/")
	if home == "" || !strings.HasPrefix(home, "/") {
		return body
	}

	var b strings.Builder
	rest := body
	for {
		i := strings.Index(rest, home)
		if i < 0 {
			break
		}
		end := i + len(home)
		if !boundedMatch(rest, i, end) {
			// 別のパスの一部である（`/mnt/home/alice` や `/home/alice2` など）。
			// **そこまでを出して、続きから探し直す。**
			b.WriteString(rest[:end])
			rest = rest[end:]
			continue
		}
		b.WriteString(rest[:i])
		b.WriteString(homeMark)
		rest = rest[end:]
	}
	b.WriteString(rest)
	return b.String()
}

// boundedMatch は rest[i:end] が「home そのもの」を指しているかを返す。
//
// **前後を見ずに置き換えてはならない。**`/home/alice` を縮める場面で、
// `/home/alice2/…` を `~2/…` に、`/mnt/home/alice` を `/mnt~` にしてしまう。
//
// rest: 走査中の文字列。
// i: home に一致した先頭の位置。
// end: 一致の終端（次の位置）。
// 戻り値: home そのものを指していれば真。
func boundedMatch(rest string, i, end int) bool {
	if i > 0 && (rest[i-1] == '/' || isNameByte(rest[i-1])) {
		return false
	}
	if end < len(rest) && rest[end] != '/' && isNameByte(rest[end]) {
		return false
	}
	return true
}

// isNameByte は、そのバイトがファイル名の一部として続きうる ASCII かを返す。
//
// **多バイト文字は数えない。**このリポジトリの文面は日本語であり、パスは
// `（/home/alice/x）` のように全角の括弧で囲まれる。多バイトを「名前の続き」に
// 数えると、その形が1つも縮まらなくなる。
//
// **その代わり、`/home/aliceさん` のような名前は `~さん` に縮んでしまう。**
// home のパスをそのまま前置きに持つ別のディレクトリが、続けて多バイト文字で
// 始まる場合だけであり、**縮みすぎる側に外れるので情報は漏れない。**
//
// c: 調べるバイト。
// 戻り値: ファイル名の一部として続きうるなら真。
func isNameByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '_' || c == '-' || c == '.':
		return true
	}
	return false
}
