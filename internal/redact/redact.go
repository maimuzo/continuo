// Package redact は、continuo が issue へ書く文面から手元の絶対パスを取り除く
// （docs/plans/continuo_design.md 3-73 / 3-73b / 3-73c）。
//
// **issue は公開のリポジトリにあることがある。**`/home/<利用者名>/…` をそのまま書くと、
// 利用者名とその機械の構成が公開される。**issue のコメントは編集履歴が残るので、
// あとから消しても取り消せない。**
//
// **縮めるのは、continuo を動かしている機械の home で始まるパスだけである。**
// **綴り方は2つの軸の掛け算である。**home をそのまま書いた形と symlink 越しに解決した形の2つに、
// **`/` `.` `_` のそれぞれを `-` に置き換える／置き換えないの8通り**を掛ける
// （Claude Code の会話の記録の置き場所の名前が、この置き換えを受けた綴りである）。
// **全部縮める。**
//
// **home の外にあるパスはそのまま出す。**縮めようが無いうえ、伏せてしまうと
// 引き渡しの通知の【調べるところ】が「どこを見ればよいか分からない」ものになる。
// 利用者名が入るのは home の下であり、そこは縮まる。
package redact

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// homeMark は縮めたあとに置く印である。
const homeMark = "~"

// ErrUnusableHome は、home の値が縮める対象として使えないことを表す
// （空文字列・`/` そのもの・相対のパス）。
//
// **返さないと、何も縮めずに素通りしたことが誰にも伝わらない。**
// `Paths` の呼び出し側はこれを見て警告を1行出す（設計 3-73c）。
var ErrUnusableHome = errors.New("home が縮める対象として使えません")

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
// **引けても使えない値のときも同じである。**`HOME=/` や `HOME=relative/path` は
// `os.UserHomeDir` を素通りするが、縮める対象にはできない。**このときも
// `ErrUnusableHome` を返す。**返さないと、何も縮めなかったことが誰にも伝わらない。
//
// body: issue へ書く本文。
// 戻り値の1つ目: home を `~` に縮めた本文。縮められなければ body そのまま。
// 戻り値の2つ目: home を引けなかった／引けても使えなかったときのエラー。
func Paths(body string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return body, err
	}
	return PathsWithHome(body, home)
}

// PathsWithHome は home を明示して Paths と同じことを行う。
//
// **テストから呼ぶために分けてある。**環境変数を差し替えなくても、任意の home で
// 縮め方を確かめられる。
//
// **`/` を home として渡しても何もしない。**すべての絶対パスが `~` に化けるためである。
// **ただし黙っては通さない。**`ErrUnusableHome` を添えて返す。
//
// body: issue へ書く本文。
// home: 縮める対象の home の絶対パス。末尾のスラッシュは無視する。
// 戻り値の1つ目: home を `~` に縮めた本文。縮められなければ body そのまま。
// 戻り値の2つ目: home が縮める対象として使えないときの `ErrUnusableHome`。
func PathsWithHome(body, home string) (string, error) {
	spellings := homeSpellings(home)
	if len(spellings) == 0 {
		return body, fmt.Errorf("%w: %q", ErrUnusableHome, home)
	}
	for _, spelling := range spellings {
		body = replaceBounded(body, spelling, slashBounded)
		for _, variant := range dashSpellings(spelling) {
			body = replaceBounded(body, variant.Text, variant.Bounded)
		}
	}
	return body, nil
}

// homeSpellings は縮める対象の home の綴りを、長いものから順に返す（設計 3-73b）。
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
// **`/` そのものは使えない。**末尾のスラッシュを落とすと空になるので、ここで弾かれる。
// 通してしまうと、本文中のすべての絶対パスが `~` に化ける。
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

// homeSpellingVariant は、home を綴り直した1つの形である。
type homeSpellingVariant struct {
	// Text は綴り直した文字列である。
	Text string
	// Bounded は、その形が「home そのもの」を指しているかを判定する関数である。
	Bounded func(string, int, int) bool
}

// homeDashChars は、Claude Code が会話の記録の置き場所の名前を作るときに
// `-` へ置き換えうる文字である（設計 3-73b）。
//
// **`/` が置き換わることは確かである。**置き場所の名前は cwd を1つのディレクトリ名に
// 畳んだものなので、`/` が残ることはありえない。
//
// **`.` が置き換わることも確かめてある。**`~/.foo` を cwd にしたときの置き場所の名前が
// `-Users-<利用者名>--foo` になっていることを、`~/.claude/projects/` の実物で確認した
// （2026-08-30）。**先頭の `.` が `-` になり、`/` の分と合わせて `-` が2つ並ぶ。**
//
// **`_` が置き換わることは確かめられていない。**手元の `~/.claude/projects/` には
// `_` を含む cwd から作られた名前が1つも残っておらず、
// **Claude Code の公式文書にも綴り直しの規則が書かれていない。**
// それでも候補に入れるのは、**外れたときに失われるものが左右で釣り合わないから**である。
// 置き換わらないのに縮めれば案内が少し読みにくくなるだけだが、
// 置き換わるのに縮めなければ利用者名が公開の issue へ出る。
var homeDashChars = []string{"/", ".", "_"}

// dashSpellings は home を綴り直した形を全部返す（設計 3-73b）。
//
// **Claude Code の会話の記録の置き場所が、この綴りを名前に持つ。**
// 置き場所は `~/.claude/projects/<cwd を綴り直したもの>/<セッション UUID>.jsonl` であり、
// **cwd が home の下にある限り、その名前の中に利用者名が丸ごと入る。**
//
//	/Users/john.doe/.claude/projects/-Users-john-doe-worktrees-issue-1/….jsonl
//	→ ~/.claude/projects/~-worktrees-issue-1/….jsonl
//
// **`homeDashChars` の各文字について「置き換える／置き換えない」を全部作る。**
// 2つだけ（全部置き換えた形と `/` だけ置き換えた形）では足りない。
// **`/Users/ann_b.c` のように `.` と `_` が混ざる home では、どちらにも当たらない。**
// `.` だけが置き換わる版なら `-Users-ann_b-c`、`_` だけなら `-Users-ann-b.c` であり、
// **どちらも2つの形の外にある。**綴り直す規則を持っているのは Claude Code であって
// こちらは版を選べないので、**組み合わせを全部当てる。**
//
// **`/` を置き換えない形も作る。**`/Users/ann-b-c` のような、パスの形のまま
// `.` や `_` だけが `-` になった綴りである。**Claude Code の置き場所の名前としては現れないが、
// 作っておく費用は文字列1本分であり、外したときの費用は利用者名の公開である。**
//
// **縮めすぎる側に外れても、失われるのは案内の読みやすさだけである。**
// `/Users/ann-b-c` が実在する別の利用者を指していた場合、その行は `~` に化けるが、
// **公開される情報が増えることはない。**
//
// **home の区切りが1つしか無ければ何も返さない。**`/alice` のような home では
// `-alice` が別の言葉に当たりやすく、縮めすぎて文面が読めなくなる。
//
// home: 整えた home の絶対パス。
// 戻り値: 綴り直した形の並び。縮める対象にしないなら長さ0。
func dashSpellings(home string) []homeSpellingVariant {
	if strings.Count(home, "/") < 2 {
		return nil
	}
	// **home そのものは入れない。**呼び出し側が slashBounded で先に当てている。
	seen := map[string]bool{home: true}
	out := make([]homeSpellingVariant, 0, 1<<len(homeDashChars))
	for mask := 1; mask < 1<<len(homeDashChars); mask++ {
		pairs := make([]string, 0, 2*len(homeDashChars))
		for i, c := range homeDashChars {
			if mask&(1<<i) != 0 {
				pairs = append(pairs, c, "-")
			}
		}
		text := strings.NewReplacer(pairs...).Replace(home)
		if seen[text] {
			continue
		}
		seen[text] = true
		// **`/` が残っている形は、まだパスの形をしている。**境界の見方も
		// パスのものを使う（`-` は名前の続きとして扱う）。
		bounded := dashBounded
		if strings.Contains(text, "/") {
			bounded = slashBounded
		}
		out = append(out, homeSpellingVariant{Text: text, Bounded: bounded})
	}
	return out
}

// replaceBounded は body の中の needle を、境界の検査を通ったものだけ `~` に置き換える。
//
// **走査は body の全体に対して行う。**一致を1つ捨てるたびに文字列を切り詰めると、
// **2つ目以降の一致で「直前の1文字」が読めなくなる。**そうなると
// `/mnt/home/alice/home/alice/x` の2つ目が行頭にあるものとして通り、
// **縮めてはいけない側が `/mnt/home/alice~/x` に縮む。**
// だから位置だけを進め、境界の判定には常に body そのものを渡す。
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
	// written は、まだ b へ書き出していない範囲の先頭である。
	// next は、次に needle を探し始める位置である。どちらも body の先頭からの位置。
	written, next := 0, 0
	for next <= len(body)-len(needle) {
		rel := strings.Index(body[next:], needle)
		if rel < 0 {
			break
		}
		i := next + rel
		end := i + len(needle)
		next = end
		if !bounded(body, i, end) {
			// 別のパスの一部である（`/mnt/home/alice` や `/home/alice2` など）。
			// **書き出さずに位置だけ進める。**前後の文脈は body の中に残る。
			continue
		}
		b.WriteString(body[written:i])
		b.WriteString(homeMark)
		written = end
	}
	b.WriteString(body[written:])
	return b.String()
}

// slashBounded は body[i:end] が「`/` で綴った home そのもの」を指しているかを返す。
//
// **前後を見ずに置き換えてはならない。**`/home/alice` を縮める場面で、
// `/home/alice2/…` を `~2/…` に、`/mnt/home/alice` を `/mnt~` にしてしまう。
//
// body: 走査中の文字列の全体。
// i: 一致した先頭の位置。
// end: 一致の終端（次の位置）。
// 戻り値: home そのものを指していれば真。
func slashBounded(body string, i, end int) bool {
	if i > 0 && (body[i-1] == '/' || isNameByte(body[i-1])) {
		return false
	}
	if end < len(body) && body[end] != '/' && isNameByte(body[end]) {
		return false
	}
	return true
}

// dashBounded は body[i:end] が「`-` で綴った home そのもの」を指しているかを返す。
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
// body: 走査中の文字列の全体。
// i: 一致した先頭の位置。
// end: 一致の終端（次の位置）。
// 戻り値: home そのものを指していれば真。
func dashBounded(body string, i, end int) bool {
	if i > 0 && isDashNameByte(body[i-1]) {
		return false
	}
	if end < len(body) && isDashNameByte(body[end]) {
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
