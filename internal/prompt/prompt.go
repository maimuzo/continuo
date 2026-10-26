// Package prompt は、continuo が Claude Code へ渡すシステムプロンプトと最初のプロンプトを組み立てる
// （設計 5-3 / 5-3c。issue #288（指示書を、フローチャートだけのシステムプロンプトと plugin のスキルに分ける））。
//
// **渡す文面は2つある。**
//
//	システムプロンプト  builtin.md（雛形）+ WORKFLOW.md の本文。`--append-system-prompt-file` で渡す。
//	                    その issue のあいだ変わらない地図と決まり（フローチャート・対応表・この issue の値）だけを置く。
//	                    compaction で要約されず、`--system-prompt-snapshot off` で起動のたびに新しい版へ差し替わる
//	最初のプロンプト    first_prompt.md（雛形）+ WORKFLOW.md の本文。herdr の `agent.prompt` で送る。
//	                    着手の指示・頼む作業の範囲・何回目の試行か。auto の判定役が読むのはこちらである
//
// **手順の本文はどちらにも無い。**plugins/continuo-agent-procedures/ のスキルにある（`internal/agentplugin` が書き出す）。
//
// **雛形はどちらも、目印の行で前半と後半に切り、本文をその位置に挟む。**
//
//	組み込みの前半   雛形の、目印の行より上。continuo の実行ファイルの中にある
//	本文            WORKFLOW.md の閉じの --- より下。利用者が書く。空でもよい
//	組み込みの後半   雛形の、目印の行より下（いまはどちらも空である）
//
// **断片は別々に解釈して、別々に変数展開してから連結する。**1つのテンプレートへ
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
	// **`internal/tracker` を import している。**`RenderData` を1箇所へ寄せるために要る
	// （issue #183）。**その代わり、`internal/config` はこの package を import できない。**
	// `prompt → tracker → config` になるためである。
	// **つまり `Fragments.Validate()` を `config.Load` の中で呼ぶ道は閉じている。**
	// いま `Validate` を呼ぶのは `internal/daemon`（常駐の起動）と `internal/doctor` だけで、
	// **`continuo prompt` はどの形でも呼ばない。**
	// **それでも、壊れたテンプレートが見過ごされることは無い。**常駐は起動時に落ち、
	// `continuo doctor` は `prompt vars` を赤にする。
	"github.com/maimuzo/continuo/internal/tracker"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/marginstatus"
	"github.com/maimuzo/continuo/plugins"
)

// Marker は雛形を前半と後半に切る目印の行である。**2つの雛形で同じ行を使う。**
//
// **この行そのものは、送る文面には残らない。**
//
// **HTML のコメントにしてある。**markdown として読んだときに見えず、
// 雛形を1枚の文書として人間が通して読める。
const Marker = "<!-- continuo:project-specific-prompt -->"

// Separator は、`continuo prompt --show` がシステムプロンプトと最初のプロンプトのあいだに置く区切りの行である。
//
// **送る文面には入らない。**システムプロンプトはファイルで、最初のプロンプトは `agent.prompt` で、別々に渡す。
const Separator = "======== ここから下は最初のプロンプト（上はシステムプロンプト） ========"

// builtinName と firstName は雛形のファイル名である。エラーの文言に出す。
const (
	builtinName = "builtin.md"
	firstName   = "first_prompt.md"
)

// workflowName は WORKFLOW.md のファイル名である。本文の誤りの文言に出す。
const workflowName = "WORKFLOW.md"

// 断片の名前である。テンプレートの名前としてそのまま使うので、
// `text/template` のエラー（`template: <名前>:<行>: …`）にこの文字列が出る。
const (
	// NameBuiltinHead はシステムプロンプトの組み込みの前半の名前である。
	// **行番号は、生成した表と図を差し込んだあとの builtin.md の行番号である。**
	NameBuiltinHead = builtinName + "#head"
	// NameBuiltinTail はシステムプロンプトの組み込みの後半の名前である。
	// **行番号は目印の行の次を1行目として数えたものである**（builtin.md の行番号ではない）。
	NameBuiltinTail = builtinName + "#tail"
	// NameFirstHead は最初のプロンプトの組み込みの前半の名前である。**行番号は first_prompt.md の行番号と一致する。**
	NameFirstHead = firstName + "#head"
	// NameFirstTail は最初のプロンプトの組み込みの後半の名前である。
	NameFirstTail = firstName + "#tail"
	// NameWorkflowBody は WORKFLOW.md の本文の名前である（設計 5-3c）。**2つの文面に同じ本文が入る。**
	//
	// **行番号は front matter の閉じの `---` の次を1行目として数えたものである**
	// （WORKFLOW.md の行番号ではない）。
	NameWorkflowBody = workflowName + "#body"
)

//go:embed builtin.md
var builtinEmbedded string

//go:embed first_prompt.md
var firstRaw string

// builtinRaw は builtin.md に、生成したフローチャートと表を差し込んだものである（flow.go）。
var builtinRaw = fillGenerated(builtinEmbedded)

// builtinHead / builtinTail / firstHead / firstTail は雛形を目印の行で切った結果である。
//
// **パッケージの初期化で1回だけ切る。**送るたびに切ると、切り方の誤りが
// 着手のときまで表に出ない。
var (
	builtinHead, builtinTail = splitBuiltin(builtinRaw)
	firstHead, firstTail     = splitBuiltin(firstRaw)
)

// stripComments は、送る文面から HTML のコメントを取り除く
// （[docs/plans/continuo_design.md] の 5-3m）。
//
// **取り除くのは `<!--` から `-->` までの文字列だけである。行ごとではない。**
// 行ごと落とすと、`<!-- 方針 --> production へは push しないでください。` のような
// **1行にまとめた書き方で、コメントの後ろに書いた本文まで消える。**
//
// **閉じていない `<!--` は、コメントの始まりとみなさない。**
// 断片の残りに `-->` が1つも無ければ、その `<!--` はただの文字として残す。
// **みなしてしまうと、`-->` を1つ打ち忘れただけで、そこから断片の終わりまで全部が消える。**
// 打ち忘れは markdown を書く人がいちばん踏みやすい誤りで、**プレビューでは何も壊れて見えない。**
//
// **ただし、これは打ち忘れの守りにはならない。**見ているのは「断片の残り全部に `-->` が在るか」なので、
// **後ろに別のコメントが1つでもあれば発動しない。**`continuo init` が置く雛形の本文には
// 案内のコメントが何個も並ぶので、**1つ打ち忘れると、その `<!--` は次に見つかった `-->` までを
// 見出しごと飲み込む**（設計 5-3m）。**この穴は塞げていない。**
//
// **バッククォートで囲んだ中は残す。**利用者が「これは送りたい」と決めたものを囲む口である。
// **囲みの長さも数える。**4連バッククォートで開いた囲みは、3連では閉じない。
// 数えないと、markdown の入れ子（外を4連、中を3連）で「囲みの中に居るのに外と判定される」。
//
// **コメントの中にあるバッククォートの囲みは、コメントとして落とす。**
// 利用者が `<!--` で囲んだものは「無効にした」ものであり、**囲みがあることを理由に
// 生かしてはならない。**取り除く仕組みが、取り除くべきものを昇格させることになる。
//
// **組み込みが囲みの中か4桁の字下げで見せている印は残る。**
// エージェントに書かせる `<!-- continuo:agent -->` などは、
// **囲みの中か、字下げしたコード片として置いてある。**
//
// **なぜ取り除くか。**`WORKFLOW.md` の雛形は、利用者へ書き方を説明するために
// HTML のコメントを使う。**それはエージェントへ送る情報ではない。**
//
// s: 取り除く前の文字列。
// 戻り値: コメントを落とした文字列。**もとから空だった行は残す。**
// 中身のあった行がコメントだけになったときは、その行ごと落とす。
func stripComments(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	fence := fenceMask(lines)

	// **`-->` がこの行以降に在るかを、先に数えておく。**
	// 在るかどうかで「コメントの始まり」とみなすかが変わる。
	closerAfter := make([]bool, len(lines)+1)
	for i := len(lines) - 1; i >= 0; i-- {
		closerAfter[i] = closerAfter[i+1] || strings.Contains(lines[i], commentClose)
	}

	out := make([]string, 0, len(lines))
	inComment := false
	for i, line := range lines {
		if fence[i] && !inComment {
			// **囲みの中は、コメントも含めてそのまま残す。**
			out = append(out, line)
			continue
		}
		kept, stillInComment := stripCommentsFromLine(line, inComment, closerAfter[i])
		inComment = stillInComment
		if strings.TrimSpace(line) != "" && strings.TrimSpace(kept) == "" {
			// **中身のあった行が、コメントだけになった。**行ごと落とす。
			continue
		}
		out = append(out, kept)
	}
	return squeezeBlank(strings.Join(out, "\n"))
}

// commentOpen と commentClose は HTML のコメントの開きと閉じである。
const (
	commentOpen  = "<!--"
	commentClose = "-->"
)

// stripCommentsFromLine は、1行からコメントの部分だけを取り除く。
//
// **開きとみなすのは、行頭の `<!--` だけである。**字下げしてあるものは残す。
// 組み込みのプロンプトは、エージェントに書かせる印（`<!-- continuo:agent -->` など）を
// **囲みの中か4桁の字下げのコード片として見せており、落とすとエージェントが印を書けなくなる。**
//
// **閉じたあとの、同じ行の残りは残す。**`<!-- 方針 --> production へは push しないでください。`
// のような1行にまとめた書き方で、**コメントの後ろに書いた本文まで消してはならない。**
//
// line: 取り除く前の行。
// inComment: この行の頭が、既にコメントの中かどうか。
// closerAfter: この行以降のどこかに `-->` が在るかどうか。
//
//	**無ければ、この行の `<!--` はコメントの始まりとみなさない。**
//
// 戻り値の1つ目: コメントを落とした行。
// 戻り値の2つ目: この行の終わりでも、まだコメントの中かどうか。
func stripCommentsFromLine(line string, inComment, closerAfter bool) (string, bool) {
	if inComment {
		end := strings.Index(line, commentClose)
		if end < 0 {
			// この行は全部コメントの中である。
			return "", true
		}
		// **閉じたあとの残りを、もう一度見る。**行頭の `<!--` がもう1つ在りうる。
		kept, still := stripCommentsFromLine(line[end+len(commentClose):], false, closerAfter)
		return kept, still
	}
	if !strings.HasPrefix(line, commentOpen) {
		return line, false
	}
	if !closerAfter {
		// **閉じが1つも無い。**コメントの始まりとみなさず、そのまま残す。
		return line, false
	}
	rest := line[len(commentOpen):]
	end := strings.Index(rest, commentClose)
	if end < 0 {
		return "", true
	}
	kept, still := stripCommentsFromLine(rest[end+len(commentClose):], false, closerAfter)
	return kept, still
}

// fenceMask は、バッククォートの囲みの中に居る行に印を付ける。
//
// **囲みを開いた行と閉じた行にも印を付ける。**どちらも中身として残す。
//
// **囲みの長さを数える。**4連で開いた囲みは、3連では閉じない。
// 数えないと、markdown の入れ子で「囲みの中に居るのに外と判定される」。
//
// lines: 見る行の並び。
// 戻り値: 行ごとに、囲みの中かどうか。
func fenceMask(lines []string) []bool {
	mask := make([]bool, len(lines))
	open := 0
	for i, line := range lines {
		n := backtickRun(strings.TrimSpace(line))
		switch {
		case open == 0 && n >= 3:
			open = n
			mask[i] = true
		case open > 0 && n >= open:
			open = 0
			mask[i] = true
		case open > 0:
			mask[i] = true
		}
	}
	return mask
}

// backtickRun は、行頭に並ぶバッククォートの数を返す。
//
// trimmed: 前後の空白を落とした行。
// 戻り値: 行頭のバッククォートの数。並んでいなければ 0。
func backtickRun(trimmed string) int {
	n := 0
	for n < len(trimmed) && trimmed[n] == '`' {
		n++
	}
	return n
}

// dropEmptySections は、中身が1行も無くなった見出しを落とす。
//
// **当てるのは利用者の本文だけである**（[docs/plans/continuo_design.md] の 5-3m）。
// 組み込みの側へ当てると、`## このプロジェクトの決まり` が落ちる。
// **利用者の本文が `##` の見出しで始まっていると、その節は「中身が無い」と読めるからである。**
// この版より前の `continuo init` が置いた `WORKFLOW.md` は、本文の見出しが `##` である。
// **つまり、いま持っている利用者は全員その形である。**
//
// **なぜ落とすか。**`WORKFLOW.md` の雛形は「ここに書いてください」を HTML のコメントで案内する。
// **利用者が何も書かなければ、コメントを落とした時点でその節は見出しだけになる。**
// 見出しだけの節は、エージェントへ渡す情報を1つも持たない。
//
// **変化が無くなるまで繰り返す。**1周では、子を落とした結果として空になった親が残る。
//
// **見出しの判定は CommonMark に合わせる。**`#` が1〜6個で、その次が空白か行末のときだけ
// 見出しとみなす。**`#188 の議論を読んでください` は見出しではなく段落である。**
// みなしてしまうと、issue の番号を行頭に書いた行が消える
// （組み込みの文面自身が、pull request の本文へ書く例として井桁つきの番号を載せている）。
//
// s: 落とす前の文字列。
// 戻り値: 中身のある見出しだけを残した文字列。
func dropEmptySections(s string) string {
	for {
		next := dropEmptySectionsOnce(s)
		if next == s {
			return s
		}
		s = next
	}
}

// dropEmptySectionsOnce は、中身が無い見出しを1周ぶん落とす。
//
// s: 落とす前の文字列。
// 戻り値: 1周ぶん落とした文字列。
func dropEmptySectionsOnce(s string) string {
	lines := strings.Split(s, "\n")
	fence := fenceMask(lines)
	keep := make([]bool, len(lines))
	for i, line := range lines {
		keep[i] = true
		if fence[i] || headingLevel(line) == 0 {
			continue
		}
		level := headingLevel(line)
		keep[i] = false
		for j := i + 1; j < len(lines); j++ {
			if !fence[j] && headingLevel(lines[j]) > 0 {
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
// **CommonMark に合わせる。**`#` が1〜6個で、その次が空白か行末のときだけ見出しである。
// **`#188 …` や `#!/bin/sh` は見出しではない。**
//
// line: 見る行。
// 戻り値: 見出しなら先頭の `#` の数。見出しでなければ 0。
func headingLevel(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return 0
	}
	if n == len(line) {
		return n
	}
	if line[n] == ' ' || line[n] == '\t' {
		return n
	}
	return 0
}

// squeezeBlank は、3つ以上続く改行を2つへ畳む。
//
// **コメントを落とした跡に空行が並ぶのを防ぐ。**
//
// **1回の走査で畳む。**`strings.ReplaceAll` を繰り返すと、続く改行が N 個のとき
// 1周で1個しか減らないので、全文の走査を N 回することになる。
//
// s: 畳む前の文字列。
// 戻り値: 空行が2つ以上続かない文字列。
func squeezeBlank(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	run := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			run++
			if run > 2 {
				continue
			}
		} else {
			run = 0
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// splitBuiltin は雛形を目印の行で前半と後半に切る。
//
// raw: 雛形の全文。
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

// BuiltinRaw はシステムプロンプトの雛形の全文を、目印の行を含めたまま返す
// （生成したフローチャートと表は差し込んである）。
//
// **テストと、目印の数を数える検査のためにある。**送る文面が要るなら Builtin を使う。
//
// 戻り値: builtin.md の全文。
func BuiltinRaw() string { return builtinRaw }

// FirstRaw は最初のプロンプトの雛形の全文を、目印の行を含めたまま返す（テストのため）。
//
// 戻り値: first_prompt.md の全文。
func FirstRaw() string { return firstRaw }

// BuiltinHead はシステムプロンプトの組み込みの前半を返す。
//
// 戻り値: builtin.md の、目印の行より上。
func BuiltinHead() string { return builtinHead }

// BuiltinTail はシステムプロンプトの組み込みの後半を返す。
//
// 戻り値: builtin.md の、目印の行より下。
func BuiltinTail() string { return builtinTail }

// BuiltinSystem はシステムプロンプトの組み込みだけを返す（本文を挟まない形。変数は展開しない）。
//
// 戻り値: 前半と後半を連結した全文。
func BuiltinSystem() string {
	return join([]string{stripComments(builtinHead), stripComments(builtinTail)})
}

// BuiltinFirst は最初のプロンプトの組み込みだけを返す（本文を挟まない形。変数は展開しない）。
//
// 戻り値: 前半と後半を連結した全文。
func BuiltinFirst() string {
	return join([]string{stripComments(firstHead), stripComments(firstTail)})
}

// Builtin は組み込みだけで組み立てた2つの文面を、区切りの行で分けて返す（本文を挟まない形）。
//
// **`continuo prompt --show --builtin` が出すのはこれである。**
// **利用者が、自分が書いた本文と仕組みの側を見比べる相手**として読む（設計 5-3f）。
// 組み込みが既に言っていることを、本文に二重に書かずに済む。
//
// 戻り値: システムプロンプト・区切りの行・最初のプロンプトの順に並べた、変数展開していない全文。
func Builtin() string {
	return showJoin(BuiltinSystem(), BuiltinFirst())
}

// showJoin は、`--show` が出す形（システムプロンプト・区切りの行・最初のプロンプト）に並べる。
//
// system / first: それぞれの全文（末尾の改行はどちらでもよい）。
// 戻り値: 並べた全文。末尾に改行を1つ付ける。
func showJoin(system, first string) string {
	return join([]string{system, Separator, first})
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

// Plugin は、システムプロンプトの置き換える語に埋める plugin の値である。
//
// **ゼロ値なら置き換えない。**`continuo prompt --show` は置き換える語のまま出す。
type Plugin struct {
	// Dir は書き出した plugin のディレクトリの絶対パスである（`CONTINUO_PLUGIN_DIR` に入る）。
	Dir string
	// Hash は plugin の中身のハッシュ12桁である（`CONTINUO_PLUGIN_HASH` に入る）。
	Hash string
}

// apply は、組み込みの断片の置き換える語を埋める。
//
// **本文（WORKFLOW.md）には当てない。**利用者が書いた文字列を continuo が書き換えない。
//
// it: 変数展開したあとの断片。
// 戻り値: 置き換えた断片。
func (p Plugin) apply(it Fragment) Fragment {
	if it.Name == NameWorkflowBody {
		return it
	}
	if p.Dir != "" {
		it.Text = strings.ReplaceAll(it.Text, plugins.DirPlaceholder, p.Dir)
	}
	if p.Hash != "" {
		it.Text = strings.ReplaceAll(it.Text, plugins.HashPlaceholder, p.Hash)
	}
	return it
}

// Fragments は、送る2つの文面を組み立てる断片の並びである。
//
// **並んでいる順に連結する。**
type Fragments struct {
	// system はシステムプロンプトの断片である（連結する順）。**空文字の断片は入っていない。**
	system []Fragment
	// first は最初のプロンプトの断片である（連結する順）。**空文字の断片は入っていない。**
	first []Fragment
	// bodyPath は本文が来た WORKFLOW.md の絶対パスである。
	//
	// **本文が空でも埋める。**内訳の「本文はありません」に、どのファイルの話かを添えるためである。
	bodyPath string
	// hasBody は本文に中身があったかどうかである。
	//
	// **空白だけなら偽である。**
	hasBody bool
}

// Build は、WORKFLOW.md の本文から2つの文面の断片を決める（設計 5-3c）。
//
// **並びは、組み込みの前半 + 本文 + 組み込みの後半で固定である。**
// **本文が空白だけなら、組み込みの前半 + 組み込みの後半になる。**
// **本文は2つの文面の両方に入る。**決まりとして compaction のあとも要り（システムプロンプト）、
// auto の判定役にも読ませる（最初のプロンプト）。
//
// **本文を「全文の差し替え」として扱わない。**そう扱うと、continuo が仕組みの説明を直しても、
// 本文を書いた利用者には二度と届かない（設計 5-3c）。
//
// body: WORKFLOW.md の front matter より後ろ。空でもよい。
// bodyPath: 本文が来た WORKFLOW.md の絶対パス。**本文が空でも埋める**（内訳に出す）。
// 戻り値: 連結する順に並んだ断片。
func Build(body, bodyPath string) Fragments {
	// **`hasBody` は、取り除いたあとで決める。**取り除く前で決めると、
	// 本文が案内のコメントだけだったときに「本文はあります」と言いながら
	// 断片は足されず、**内訳から本文の行が丸ごと消える。**
	stripped := dropEmptySections(stripComments(body))
	f := Fragments{bodyPath: bodyPath, hasBody: strings.TrimSpace(stripped) != ""}
	f.system = addFragment(f.system, NameBuiltinHead, stripComments(builtinHead), "")
	f.system = addFragment(f.system, NameWorkflowBody, stripped, bodyPath)
	f.system = addFragment(f.system, NameBuiltinTail, stripComments(builtinTail), "")
	f.first = addFragment(f.first, NameFirstHead, stripComments(firstHead), "")
	f.first = addFragment(f.first, NameWorkflowBody, stripped, bodyPath)
	f.first = addFragment(f.first, NameFirstTail, stripComments(firstTail), "")
	return f
}

// addFragment は、中身のある断片だけを並びへ足す。
//
// items: 足す先の並び。
// name: 断片の名前。
// text: 断片の中身。**空白だけなら足さない。**
// path: その断片が来たファイルの絶対パス。
// 戻り値: 足したあとの並び。
func addFragment(items []Fragment, name, text, path string) []Fragment {
	if strings.TrimSpace(text) == "" {
		return items
	}
	return append(items, Fragment{Name: name, Text: text, Path: path})
}

// Items はシステムプロンプトの断片を、連結する順に返す。
//
// 戻り値: 断片の並び。**呼び出し側は書き換えてはならない。**
func (f Fragments) Items() []Fragment { return f.system }

// FirstItems は最初のプロンプトの断片を、連結する順に返す。
//
// 戻り値: 断片の並び。**呼び出し側は書き換えてはならない。**
func (f Fragments) FirstItems() []Fragment { return f.first }

// HasBody は WORKFLOW.md の本文に中身があったかどうかを返す。
//
// 戻り値: 中身があれば真。**空白だけなら偽である。**
func (f Fragments) HasBody() bool { return f.hasBody }

// BodyPath は本文が来た WORKFLOW.md の絶対パスを返す。
//
// 戻り値: 絶対パス。**本文が空でも埋まっている。**
func (f Fragments) BodyPath() string { return f.bodyPath }

// BodyChanged は、渡された本文が、いま持っている本文と違うかを返す（設計 3-24）。
//
// **設定の読み直しが「本文は効きません」と知らせるために要る。**
// 本文は front matter の外にあるので、`config.Config` の差分には1行も出てこない。
// **知らせないと、本文だけを直した人が「読み直したから効いた」と読む。**
//
// **比べるのは、システムプロンプトの側の本文である**（issue #288）。2つの文面に同じ本文が入っている。
// **比べるのは、取り除いたあとの本文である。**案内のコメントだけを直したときは
// 送る文面が1バイトも変わらないので、「変わった」と言わない（`Build` と同じ手順を通す）。
//
// body: 読み直した WORKFLOW.md の front matter より後ろ。
// 戻り値: 送る文面に効く形で違っていれば真。
func (f Fragments) BodyChanged(body string) bool {
	stripped := dropEmptySections(stripComments(body))
	for _, it := range f.system {
		if it.Name == NameWorkflowBody {
			return it.Text != stripped
		}
	}
	// **いま本文を持っていない。**新しい本文に中身があれば変わったことになる。
	return strings.TrimSpace(stripped) != ""
}

// SystemText はシステムプロンプトの、変数展開していない全文を返す。
//
// 戻り値: 断片を連結した文字列。
func (f Fragments) SystemText() string { return joinItems(f.system) }

// FirstText は最初のプロンプトの、変数展開していない全文を返す。
//
// 戻り値: 断片を連結した文字列。
func (f Fragments) FirstText() string { return joinItems(f.first) }

// Text は、変数展開していない2つの文面を区切りの行で分けて返す（`continuo prompt --show` が出すもの）。
//
// 戻り値: システムプロンプト・区切りの行・最初のプロンプトの順に並べた文字列。
func (f Fragments) Text() string { return showJoin(f.SystemText(), f.FirstText()) }

// joinItems は断片の中身を連結する。
//
// items: 断片の並び。
// 戻り値: 連結した文字列。
func joinItems(items []Fragment) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
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
// **落とすのは改行だけである。**行頭の空白は落とさない（コマンドの例を4桁の字下げで書いた本文を、
// コードブロックとして読めるまま残す）。
//
// parts: 連結する文字列の並び。**空白だけのものは落とす。**
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
	return strings.Join(trimmed, "\n\n") + "\n"
}

// RenderSystem は、システムプロンプトを断片ごとに変数展開してから連結する（issue #288）。
//
// **展開したあとに、組み込みの断片の置き換える語（plugin のディレクトリと12桁）を埋める。**
// **`RenderData` に plugin のパスを足さない。**利用者の本文から使える変数を増やさないためである。
//
// data: テンプレートへ渡す変数。**`SampleData` が返す名前だけを持たせること**（設計 5-3）。
// plugin: 埋める値。ゼロ値なら置き換える語のまま残す。
// 戻り値の1つ目: 変数展開した全文。
// 戻り値の2つ目: 解釈できなかった、または一覧に無い変数を参照していた断片のエラー。
func (f Fragments) RenderSystem(data map[string]any, plugin Plugin) (string, error) {
	items, err := renderItems(f.system, data, plugin)
	if err != nil {
		return "", err
	}
	return joinItems(items), nil
}

// RenderFirst は、最初のプロンプトを断片ごとに変数展開してから連結する（設計 5-3c）。
//
// data: テンプレートへ渡す変数。
// 戻り値の1つ目: 変数展開した全文。**relay の節とマージンの切り上げの文は入らない**（呼び出し側が後ろに付ける）。
// 戻り値の2つ目: 解釈できなかった、または一覧に無い変数を参照していた断片のエラー。
func (f Fragments) RenderFirst(data map[string]any) (string, error) {
	items, err := renderItems(f.first, data, Plugin{})
	if err != nil {
		return "", err
	}
	return joinItems(items), nil
}

// Render は、2つの文面を変数展開し、区切りの行で分けて返す（`continuo prompt --show` の形）。
//
// **plugin の置き換える語は埋めない。**埋めるなら RenderShow を使う。
//
// data: テンプレートへ渡す変数。
// 戻り値の1つ目: 変数展開した全文。
// 戻り値の2つ目: 解釈できなかった、または一覧に無い変数を参照していた断片のエラー。
// **どの断片の何行目かがエラーの文言に入る。**
func (f Fragments) Render(data map[string]any) (string, error) {
	text, _, _, err := f.RenderShow(data, Plugin{})
	return text, err
}

// RenderShow は、`continuo prompt --show --url` が出す全文と、内訳を数える断片を一度に返す（issue #183 / #288）。
//
// **全文は、区切りの行を除けば、常駐が送る2つの文面と1バイトも違わない**（relay の節とマージンの切り上げの文を除く）。
//
// data: テンプレートへ渡す変数。
// plugin: 埋める値。ゼロ値なら置き換える語のまま残す。
// 戻り値の1つ目: 変数展開した全文（システムプロンプト・区切りの行・最初のプロンプト）。
// 戻り値の2つ目: 変数展開したシステムプロンプトの断片。**並びは `Items` と同じ。**
// 戻り値の3つ目: 変数展開した最初のプロンプトの断片。**並びは `FirstItems` と同じ。**
// 戻り値の4つ目: 解釈できなかった、または一覧に無い変数を参照していた断片のエラー。
func (f Fragments) RenderShow(data map[string]any, plugin Plugin) (string, []Fragment, []Fragment, error) {
	// **システムプロンプトは試行回数を空にして展開する。**常駐（orchestrator の writeSystemPrompt）が
	// そう書くので、`--attempt` を渡されても同じにしないと、見せる文面と書く文面がずれる。
	sysData := make(map[string]any, len(data))
	for k, v := range data {
		sysData[k] = v
	}
	sysData["attempt"] = nil
	system, err := renderItems(f.system, sysData, plugin)
	if err != nil {
		return "", nil, nil, err
	}
	first, err := renderItems(f.first, data, Plugin{})
	if err != nil {
		return "", nil, nil, err
	}
	return showJoin(joinItems(system), joinItems(first)), system, first, nil
}

// renderItems は、断片ごとに変数展開した結果を返す（issue #183）。
//
// **`continuo prompt --show --url` の内訳が、これを数える。**
// **展開する前の断片を数えてはならない。**`{{if .attempt}}` のような枝は、
// **展開すると行が消える。**見出しは「送る文面の内訳」なので、
// **送った文面を数えなければ、その行数は嘘になる**（実測で6行ずれた）。
//
// **並びと名前は元の断片のままである。**中身だけが展開後になる。
//
// items: 展開する断片の並び。
// data: テンプレートへ渡す変数。
// plugin: 組み込みの断片に埋める値。
// 戻り値の1つ目: 変数展開した断片。
// 戻り値の2つ目: 解釈できなかった、または一覧に無い変数を参照していた断片のエラー。
func renderItems(items []Fragment, data map[string]any, plugin Plugin) ([]Fragment, error) {
	out := make([]Fragment, 0, len(items))
	for _, it := range items {
		text, err := renderOne(it, data)
		if err != nil {
			return nil, err
		}
		out = append(out, plugin.apply(Fragment{Name: it.Name, Text: text, Path: it.Path}))
	}
	return out, nil
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

// Validate は、2つの文面の断片が解釈でき、一覧にある変数だけを使っていることを確かめる（設計 5-3c）。
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

// RenderData は、送る文面へ渡す変数を組み立てる（設計 5-3 / 5-3f）。
//
// **組み立てる場所はここだけである。**continuo が実際に送る経路
// （`internal/orchestrator` の `renderFirstPrompt` と `writeSystemPrompt`）と、人間が事前に確かめる経路
// （`continuo prompt --show --url`）が、どちらもこの関数を呼ぶ。
//
// **別々に組み立ててはならない。**片方を直したときにもう片方がずれる。
// **ずれた瞬間、`--show --url` は「送られる文面」ではないものを見せることになり、
// そのコマンドの目的そのものを失う。**
//
// **返す名前は `SampleData` と1つも違わないこと。**渡す側と検査する側が食い違うと、
// **その名前を使った文面で continuo が起動しない**（同じ形の欠陥が3回起きている。
// test/internal/prompt の `TestSampleData_送る文面が使える変数の一覧` がその2つを結んでいる）。
//
// issue: 対象の issue。
// attempt: 試行回数。**1回目は nil を渡す**（仕様 12.3。`text/template` は nil を偽として
// 扱うので `{{if .attempt}}` が正しく動く）。**キーごと省いてはならない。**
// progressIntervalMs: 進捗報告を書かせる間隔（ミリ秒）。
//
//	**分へ直すのはこの中である。**呼び出し側で割らせない。
//	割り算が2箇所に散ると、片方を直したときにずれる。
//
// rateLimit: 設定の `rate_limit`（`margin_status_path` を決めるのに使う。issue #262）。
// hookSocketPath: hook を受ける socket の絶対パス。求められなかったときは空文字（`margin_status_path` も空になる）。
//
//	**`margin_status_path` を決めるのはこの中である。**`internal/marginstatus` の `Path` を呼ぶ。
//	常駐が `margin-status` を書くパスも同じ関数で決めるので、指示書が指すパスとずれない。
//
// 戻り値: 送る文面が使う名前を全部持つ変数の一覧。
func RenderData(
	issue tracker.Issue, attempt *int, progressIntervalMs int,
	rateLimit config.RateLimitConfig, hookSocketPath string,
) map[string]any {
	url := ""
	if issue.URL != nil {
		url = *issue.URL
	}
	// push_branch は issue にリンクされた branch の生の名前である（設計 3-22d・5-3）。
	//
	// **push 先の既定ではない。**既定はいつでも `git push -u origin HEAD` であり、
	// これは「別の名前へ出せと issue に書かれていたときの候補」として渡す。
	//
	// **リンクが1本でないとき（0本・2本以上・別のリポジトリを指すとき）は空文字である。**
	branch := ""
	if issue.BranchName != nil {
		branch = *issue.BranchName
	}
	// **1回目は nil のままにする。**`any` のゼロ値ではなく nil を入れる。
	var attemptValue any
	if attempt != nil {
		attemptValue = *attempt
	}
	return map[string]any{
		"issue": map[string]any{
			"identifier": issue.Identifier,
			"owner":      issue.Owner,
			"repo":       issue.Repo,
			"number":     issue.Number,
			"url":        url,
			"title":      issue.Title,
			"state":      issue.State,
			"labels":     issue.Labels,
		},
		"push_branch": branch,
		"attempt":     attemptValue,
		// **ミリ秒ではなく分で渡す。**送る文面は人間が読む日本語であり、
		// **「3600000ミリ秒以上黙らないでください」では通じない。**
		"progress_interval_minutes": progressIntervalMs / 60000,
		// **`margin-status` のパス**（issue #262）。システムプロンプトの「どの段でも」の箱と
		// 「この issue の値」の表が使い、スキル「reporting-progress」が `cat '<マージンのファイル>'` の形で叩かせる。**中の単引用符は `'\''` に置き換えてある**
		// （パスに単引用符を含む利用者でも、単引用符で囲んだ `cat` が壊れない）。
		// **書かない機械（`rate_limit.source: none`）では空文字である。**マージンの1文は `{{if}}` で囲んであるので出ない。
		// **キーは空でも必ず入れる**（`missingkey=error`）。
		"margin_status_path": marginstatus.ShellQuote(marginstatus.Path(rateLimit, hookSocketPath)),
	}
}

// SampleData は、検査に使う作り物の issue の変数を返す（設計 5-3c）。
//
// **値は全部「空でないもの」にする。**空文字だと `{{if .issue.title}}` の中が検査されない。
// **`.attempt` は呼び出し側が入れ直す**（1回目は nil、2回目は回数）。
//
// 戻り値: 送る文面が使う名前を全部持つ変数の一覧。
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
		// **issue にリンクされた branch の名前**（設計 3-22d / 5-3）。
		// **`renderFirstPrompt` が渡すのに、ここに無かった。**
		// そのため `{{.push_branch}}` を本文に書いた利用者だけが起動できなかった
		// （`docs/upgrading.md` は「使えるようになりました」と案内している）。
		"push_branch": "42-example",
		// **進捗報告を書かせる間隔（分）**（設計 5-3n）。
		// 送る文面が `{{.progress_interval_minutes}}` で使うので、ここにも要る。
		// **入れ忘れると `continuo doctor` の `prompt vars` が赤になる。**
		"progress_interval_minutes": 60,
		// **`margin-status` のパス**（issue #262）。空にすると、システムプロンプトのマージンの1文が検査されない。
		"margin_status_path": "/tmp/continuo/margin-status",
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
