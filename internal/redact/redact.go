// Package redact は、continuo が issue へ書く文面から手元の絶対パスを取り除く
// （docs/plans/continuo_design.md 3-73）。
//
// **issue は公開のリポジトリにあることがある。**`/home/<利用者名>/…` をそのまま書くと、
// 利用者名とその機械の構成が公開される。**issue のコメントは編集履歴が残るので、
// あとから消しても取り消せない。**
//
// **縮めるのは、continuo を動かしている機械の home で始まるパスだけである。**
// **綴り方は3つある。**home をそのまま書いた形、home を symlink 越しに解決した形、
// **home の `/` を `-` に置き換えた形**（Claude Code の会話の記録の置き場所の名前）。
// **3つとも縮める。**
//
// **home の外にあるパスはそのまま出す。**縮めようが無いうえ、伏せてしまうと
// 引き渡しの通知の【調べるところ】が「どこを見ればよいか分からない」ものになる。
// 利用者名が入るのは home の下であり、そこは縮まる。
package redact

import (
	"os"
	"path/filepath"
	"strings"
)

// homeMark は縮めたあとに置く印である。
const homeMark = "~"

// Paths は body の中の絶対パスのうち、continuo を動かしている機械の home で始まるものを
// `~` に縮める。
//
// **continuo が issue へ書く本文は、必ずこの関数を通す**（設計 3-73）。
// 組み立てる場所ごとに縮めると、必ずどこかが漏れる。
//
// **home を引けなければ、body をそのまま返し、エラーを添える。**縮められないことを
// 理由に投稿そのものを止めると、人間は「なぜ止まったのか」を知る手立てを失う。
// **その代わり、黙って素通りさせない。**呼び出し側は必ずログへ1行残すこと。
// Unix の `os.UserHomeDir` は `$HOME` しか見ないので、`HOME` を渡さない起動のされ方
// （環境を絞った常駐の仕組みから起こす、別の利用者へ切り替えて起こす）をすると、
// **警告が無ければ絶対パスが公開の issue へ出る。**
//
// body: issue へ書く本文。
// 戻り値の1つ目: home を `~` に縮めた本文。縮められなければ body そのまま。
// 戻り値の2つ目: home を引けなかったときのエラー。
func Paths(body string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return body, err
	}
	return PathsWithHome(body, home), nil
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
	for _, spelling := range homeSpellings(home) {
		body = replaceBounded(body, spelling, slashBounded)
		if dash := dashSpelling(spelling); dash != "" {
			body = replaceBounded(body, dash, dashBounded)
		}
	}
	return body
}

// homeSpellings は縮める対象の home の綴りを、長いものから順に返す。
//
// **2通りある。**渡された綴りそのものと、**symlink を解いた綴り**である。
//
// **解いた綴りが要る理由。**引き渡しの通知に載る会話の記録のパスは
// `filepath.EvalSymlinks` で解決済みである（internal/orchestrator/transcript.go の
// `subagentDirOf` / `SubagentTranscriptsFor`）。一方 `os.UserHomeDir` は Unix では
// `$HOME` をそのまま返す。**home が symlink 越しに指されている機械では、
// この2つが一致しない。**解いた側を足しておかないと、解決済みのパスが縮まらないまま
// 公開される。
//
// **長い綴りを先に返す。**片方がもう片方の前置きになっている場合
// （`/var/home/alice` と `/private/var/home/alice` など）に、短いほうを先に当てて
// 中途半端に縮めることを避ける。
//
// home: 縮める対象の home。
// 戻り値: 使える綴りの並び。使えなければ nil。
func homeSpellings(home string) []string {
	home = normalizeHome(home)
	if home == "" {
		return nil
	}
	out := []string{home}
	// **実在しない home は解けない。**テストが渡す `/home/alice` がこれに当たる。
	// 解けないことは失敗ではないので、そのまま1つだけで返す。
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		resolved = normalizeHome(resolved)
		if resolved != "" && resolved != home {
			out = append(out, resolved)
			if len(resolved) > len(home) {
				out[0], out[1] = out[1], out[0]
			}
		}
	}
	return out
}

// normalizeHome は home の綴りを整え、縮める対象として使えるかを判定する。
//
// home: 整える綴り。
// 戻り値: 末尾のスラッシュを落とした絶対パス。使えなければ空文字列。
func normalizeHome(home string) string {
	home = strings.TrimRight(home, "/")
	if home == "" || !strings.HasPrefix(home, "/") {
		return ""
	}
	return home
}

// dashSpelling は home の `/` を `-` に置き換えた綴りを返す。
//
// **Claude Code の会話の記録の置き場所が、この綴りを名前に持つ。**
// 置き場所は `~/.claude/projects/<cwd の `/` を `-` に置き換えたもの>/<セッション UUID>.jsonl`
// であり、**cwd が home の下にある限り、その名前の中に利用者名が丸ごと入る。**
// issue #75 が挙げた例そのものである。
//
//	/home/alice/.claude/projects/-home-alice-worktrees-issue-1/….jsonl
//	→ ~/.claude/projects/~-worktrees-issue-1/….jsonl
//
// **home の区切りが1つしか無ければ縮めない。**`/alice` のような home では
// `-alice` が別の言葉に当たりやすく、縮めすぎて文面が読めなくなる。
//
// home: 整えた home の絶対パス。
// 戻り値: `-` で綴り直した形。縮める対象にしないなら空文字列。
func dashSpelling(home string) string {
	if strings.Count(home, "/") < 2 {
		return ""
	}
	return strings.ReplaceAll(home, "/", "-")
}

// replaceBounded は body の中の needle を、境界の検査を通ったものだけ `~` に置き換える。
//
// body: 走査する文字列。
// needle: 探す綴り。
// bounded: 一致が「その綴りそのもの」を指しているかを判定する関数。
// 戻り値: 置き換えたあとの文字列。
func replaceBounded(body, needle string, bounded func(string, int, int) bool) string {
	if needle == "" {
		return body
	}
	var b strings.Builder
	rest := body
	for {
		i := strings.Index(rest, needle)
		if i < 0 {
			break
		}
		end := i + len(needle)
		if !bounded(rest, i, end) {
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

// slashBounded は rest[i:end] が「`/` で綴った home そのもの」を指しているかを返す。
//
// **前後を見ずに置き換えてはならない。**`/home/alice` を縮める場面で、
// `/home/alice2/…` を `~2/…` に、`/mnt/home/alice` を `/mnt~` にしてしまう。
//
// rest: 走査中の文字列。
// i: 一致した先頭の位置。
// end: 一致の終端（次の位置）。
// 戻り値: home そのものを指していれば真。
func slashBounded(rest string, i, end int) bool {
	if i > 0 && (rest[i-1] == '/' || isNameByte(rest[i-1])) {
		return false
	}
	if end < len(rest) && rest[end] != '/' && isNameByte(rest[end]) {
		return false
	}
	return true
}

// dashBounded は rest[i:end] が「`-` で綴った home そのもの」を指しているかを返す。
//
// **`-` は区切りであって名前の続きではない。**`-home-alice-worktrees-issue-1` の
// `-home-alice` は、そのディレクトリ名の先頭にある home の綴りである。
// **だから slashBounded は使えない**（`-` を名前の続きと見て、すべて弾いてしまう）。
//
// **前は `/` でも `-` でもよい。**`~/.claude/projects/-home-alice-…` では `/` が、
// `-tmp-x--home-alice-…`（cwd 自身が別のパスの綴りを名前に持つ場合）では `-` が前に来る。
//
// **後ろが英数字・`_`・`.` なら弾く。**`-home-alice2-…` は別の利用者である。
//
// rest: 走査中の文字列。
// i: 一致した先頭の位置。
// end: 一致の終端（次の位置）。
// 戻り値: home そのものを指していれば真。
func dashBounded(rest string, i, end int) bool {
	if i > 0 && isDashNameByte(rest[i-1]) {
		return false
	}
	if end < len(rest) && isDashNameByte(rest[end]) {
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

// isDashNameByte は、`-` で綴り直した名前の中で「続き」と見なすバイトかを返す。
//
// **isNameByte から `-` を除いたものである。**`-` は綴り直した名前の区切りなので、
// そこで終わっていれば home そのものを指している。
//
// c: 調べるバイト。
// 戻り値: 名前の続きなら真。
func isDashNameByte(c byte) bool {
	return c != '-' && isNameByte(c)
}
