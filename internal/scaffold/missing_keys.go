package scaffold

// 雛形にあって WORKFLOW.md に書かれていない設定項目を見つけ、それを足す差分を組み立てる
// （設計 3-74。issue #85）。
//
// **正は雛形（template.go の front matter）である。**設定の型（internal/config）ではない。
// 型には Go の既定値しか無く、**その項目に何を書けるのかを説明する文が1文字も無い。**
// 雛形はキーの右にコメントを持っているので、**足したときに人間が意味を判断できる。**
//
// **判定は「書かれているか」だけである。**値が正しいかは見ない（それは config.Load の仕事）。
//
// **YAML として読み直さない。**行として扱い、キーの行を探して、その下へ雛形の行を差し込む。
// YAML へ読み込んで書き戻すと、**コメントも並び順も空行も全部消える**
// （update.go が `continuo setup` で同じ判断をしている）。

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/maimuzo/continuo/internal/i18n"
)

// diffContext は組み立てる unified diff の前後に置く文脈の行数である。
//
// **3行にするのは diff の既定に揃えるためである。**`patch` も `git apply` も
// この形をそのまま受け取る。
const diffContext = 3

// keyNamePattern は front matter のキーとして数える名前の形である。
//
// **引用符付きのキーは数えない。**`tracker.automated_state_rewrite` の下に書く
// Status 名（`"In Progress": "Ready"` など）は利用者が決めるものであり、
// **雛形が正になる項目ではない。**
var keyNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// freeFormMapPaths は、その下に並ぶ名前を利用者が決める対応表のキーである。
//
// **その下の行は設定項目ではない。**名前を決めるのは利用者（または利用者のボード）であり、
// **continuo は特定の名前を探さない。**雛形が並べている名前は例にすぎないので、
// **書いていない人に「足りない」と言ってはならない。**黙らせる手段は無いので、
// 意図して外した人が永久に `!` を出され続けることになる。
//
// **`tracker.status_signal_map` はここに入れない。**あの対応表のキー
// （`review` / `blocked` / `working`）は、雛形の下半分のプロンプトが
// **エージェントに書かせる語そのもの**であり、名前は continuo 側で決まっている。
// **消せば、その表明が一度も効かなくなる。**だから足りなければ言う。
var freeFormMapPaths = [][]string{
	// 環境変数の名前は利用者が決める（雛形の `CLAUDE_CODE_RETRY_WATCHDOG` は例である）。
	{"claude", "env"},
	// キーはボードの Status 名である。
	{"tracker", "automated_state_rewrite"},
	// キーはボードの Status 名である。
	{"concurrency", "max_concurrent_agents_by_state"},
}

// isUnderFreeFormMap は、そのキーが freeFormMapPaths のどれかの下にあるかを返す。
//
// p: 調べるキーのパス。
// 戻り値: 対応表そのものではなく、その下にあるなら真。
func isUnderFreeFormMap(p []string) bool {
	for _, base := range freeFormMapPaths {
		if len(p) > len(base) && samePath(p[:len(base)], base) {
			return true
		}
	}
	return false
}

// MissingKeysResult は MissingKeys が返す結果である。
type MissingKeysResult struct {
	// Keys は書かれていないキーの名前である（`tracker.automated_state_rewrite` のような
	// ドット区切り。**雛形に書かれている順**に並ぶ）。
	//
	// **親ごと書かれていない場合は、いちばん外側のキーだけを入れる。**`restart` そのものが
	// 無いときに `restart` と `restart.orphan_running_action` の両方を並べても、
	// 直す手は1つしかない。
	Keys []string
	// Patch は Keys を足すための unified diff である。**Keys が空なら空文字。**
	//
	// **`patch -p0` にそのまま渡せる形にしてある**（`---` と `+++` の行に
	// WORKFLOW.md の絶対パスを書く）。
	Patch string
	// Total は雛形の front matter にあるキーの総数である。
	Total int
}

// MissingKeys は、雛形の front matter にあって WORKFLOW.md に書かれていないキーと、
// それを足す unified diff を返す（設計 3-74。issue #85）。
//
// **「要らないから書いていない」と「知らないから書いていない」を区別しない。**
// 機械には区別できないので、**足りないものは足りないものとして扱う。**
//
// **差し込む位置は、利用者の WORKFLOW.md の並びから決める。**雛形の並び順で
// 直前にあるキーを利用者のファイルの中から探し、その次に差し込む。
// **並び順を変えていても当たる**のはこのためである。直前のキーが無ければ直後のキーの前へ、
// どちらも無ければ親のブロックの先頭へ差し込む。
//
// **差し込むのは雛形の行そのものである。**キーの右のコメントも、その下に続く説明の
// コメント行も、まとめて持っていく。**キーと値だけを足すと、足したことは分かっても
// 何を書ける項目なのかが分からない。**
//
// path: WORKFLOW.md の絶対パス（差分の `---` と `+++` の行に書く）。
// content: WORKFLOW.md の全文。
// 戻り値: 書かれていないキーと、それを足す差分。
// エラー: content から front matter を切り出せない場合。
func MissingKeys(path, content string) (MissingKeysResult, error) {
	lines, hadFinalNewline := splitLines(content)
	start, end, ok := frontMatterRange(lines)
	if !ok {
		return MissingKeysResult{}, i18n.Errorf(i18n.KeyScaffoldMissingKeysNoFrontMatter, path, frontMatterDelimiter)
	}

	tmpl, _ := splitLines(workflowTemplate)
	tStart, tEnd, ok := frontMatterRange(tmpl)
	if !ok {
		// **雛形を壊したときにここへ来る。**test/internal/scaffold/missing_keys_test.go が落とす。
		return MissingKeysResult{}, i18n.Errorf(i18n.KeyScaffoldMissingKeysTemplateBroken)
	}
	paths := keyPathsIn(tmpl, tStart, tEnd)

	result := MissingKeysResult{Total: len(paths)}
	missing := make(map[string]bool)
	// suppressed は「無いが、足りないとは数えないキー」である（親がその行で値を決めている）。
	suppressed := make(map[string]bool)
	planned := make(map[string]int)
	insertions := make(map[int][]string)

	for _, p := range paths {
		// **親ごと無いキーは、親の1件にまとめる。**親の行を差し込むときに
		// 子の行も一緒に持っていくので、子を別の1件として数える意味が無い。
		if hasMissingAncestor(missing, p) || hasMissingAncestor(suppressed, p) {
			continue
		}
		if _, found := findKeyLine(lines, start, end, p); found {
			continue
		}
		name := strings.Join(p, ".")
		// **親がその行で値を決めているなら、子は「書かれていない」ではなく「空だと決めてある」。**
		// `env: {}` と書いた人に「`env` の下の項目が足りない」と言うのは筋が通らない。
		// **足そうとしてもいけない。**`env: {}` の下へ行を足すと YAML として読めなくなる。
		if len(p) > 1 {
			parentIdx, ok := findKeyLine(lines, start, end, p[:len(p)-1])
			if !ok || hasInlineValue(lines[parentIdx]) {
				suppressed[name] = true
				continue
			}
		}
		missing[name] = true
		result.Keys = append(result.Keys, name)

		pos, block := planInsertion(lines, start, end, tmpl, tStart, tEnd, paths, p, planned)
		insertions[pos] = append(insertions[pos], block...)
		planned[name] = pos
	}

	if len(result.Keys) == 0 {
		return result, nil
	}
	result.Patch = unifiedDiff(path, lines, insertions, hadFinalNewline, strings.Contains(content, "\r\n"))
	return result, nil
}

// splitLines は全文を行に分ける。
//
// **末尾の改行で空の行が1つ増えるのを落とす。**落とさないと、差分の最後に
// 中身の無い文脈の行が1本増える。
//
// s: 分ける全文。
// 戻り値の1つ目: 行の並び（行末の CR は落とさない）。
// 戻り値の2つ目: 全文が改行で終わっていたかどうか。
func splitLines(s string) ([]string, bool) {
	lines := strings.Split(s, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		return lines[:n-1], true
	}
	return lines, false
}

// keyPathsIn は front matter の中に書かれているキーを、書かれている順に返す。
//
// **入れ子は行頭のインデントで辿る**（findKeyLine と同じ判断である）。
// リストの項目（`- "Bash"`）とコメント行と空行は数えない。
//
// **利用者が名前を決める対応表の中身も数えない**（freeFormMapPaths）。
// **あれは設定項目ではなく値である。**
//
// lines: WORKFLOW.md か雛形を改行で分けた行の並び。
// start, end: 探す範囲（front matter の中。end は含まない）。
// 戻り値: ルートから辿るキーの並びの一覧。
func keyPathsIn(lines []string, start, end int) [][]string {
	type frame struct {
		indent int
		key    string
	}
	var stack []frame
	var paths [][]string

	for i := start; i < end; i++ {
		line := trimEOL(lines[i])
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") {
			continue
		}
		key, sep, _ := splitKeyValue(trimmed)
		if sep == "" || !keyNamePattern.MatchString(key) {
			continue
		}
		indent := len(line) - len(trimmed)
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		stack = append(stack, frame{indent: indent, key: key})

		p := make([]string, 0, len(stack))
		for _, f := range stack {
			p = append(p, f.key)
		}
		// **利用者が名前を決める対応表の中身は、設定項目として数えない。**
		if isUnderFreeFormMap(p) {
			continue
		}
		paths = append(paths, p)
	}
	return paths
}

// hasInlineValue は、そのキーの行に値が書いてあるかを返す。
//
// **書いてあれば、下の行はそのキーの値ではない。**`env: {}` の下へ行を足すと、
// YAML として読めないファイルができる。
//
// line: 調べる1行。
// 戻り値: `:` の右に（コメントを除いて）何か書いてあれば真。
func hasInlineValue(line string) bool {
	trimmed := strings.TrimLeft(trimEOL(line), " \t")
	_, sep, rest := splitKeyValue(trimmed)
	if sep == "" {
		return false
	}
	return strings.TrimSpace(stripComment(rest)) != ""
}

// hasMissingAncestor は、そのキーの親（またはさらに上）が既に「無い」と分かっているかを返す。
//
// missing: 「無い」と分かったキーの名前（ドット区切り）の集合。
// p: 調べるキーのパス。
// 戻り値: 上のどれかが既に「無い」なら真。
func hasMissingAncestor(missing map[string]bool, p []string) bool {
	for i := 1; i < len(p); i++ {
		if missing[strings.Join(p[:i], ".")] {
			return true
		}
	}
	return false
}

// planInsertion は、書かれていないキーを利用者の WORKFLOW.md のどこへ差し込むかを決める。
//
// **雛形の並び順で直前にあるキーを、利用者のファイルの中から探す。**見つかれば
// そのブロックの直後へ置く。**これが「並び順を変えていても当たる」の中身である。**
//
// user: 利用者の WORKFLOW.md を改行で分けた行の並び。
// uStart, uEnd: 利用者のファイルの front matter の範囲（uEnd は含まない）。
// tmpl: 雛形を改行で分けた行の並び。
// tStart, tEnd: 雛形の front matter の範囲（tEnd は含まない）。
// paths: 雛形のキーの一覧（keyPathsIn の戻り値）。
// p: 差し込むキーのパス。
// planned: 既に差し込むと決めたキーの名前（ドット区切り）と、その位置。
// 戻り値の1つ目: 差し込む位置（user のこの添字の行の前へ入る）。
// 戻り値の2つ目: 差し込む行の並び。
func planInsertion(
	user []string, uStart, uEnd int,
	tmpl []string, tStart, tEnd int,
	paths [][]string, p []string, planned map[string]int,
) (int, []string) {
	tIdx, found := findKeyLine(tmpl, tStart, tEnd, p)
	if !found {
		// **keyPathsIn が返したパスは必ず雛形にある。**ここへは来ない。
		return uEnd, nil
	}
	bStart, bEnd := blockRange(tmpl, tStart, tEnd, tIdx)
	block := append([]string(nil), tmpl[bStart:bEnd]...)
	block = dropLeadingCommentsAlreadyThere(block, user, uStart, uEnd)
	// **節と節のあいだの空行も持っていく。**top-level の節を丸ごと足すとき、
	// 前の節の最後の行にくっついて読めなくなるのを防ぐ。
	if indentOf(tmpl[tIdx]) == 0 && bStart > tStart && strings.TrimSpace(tmpl[bStart-1]) == "" {
		block = append([]string{""}, block...)
	}

	// 親のブロックの位置と、差し込む行が持つべき字下げを求める。
	scopeStart := uStart
	wantIndent := indentOf(tmpl[tIdx])
	if len(p) > 1 {
		parent := p[:len(p)-1]
		uParent, ok := findKeyLine(user, uStart, uEnd, parent)
		if !ok {
			// **親が無いキーは、親ごと足す1件にまとめてある。**ここへは来ない。
			return uEnd, block
		}
		scopeStart = uParent + 1
		if childIndent, ok := firstChildIndent(user, uParent, uEnd); ok {
			wantIndent = childIndent
		} else {
			// **親に子が1行も無い。**深さを決める手がかりが利用者のファイルに無いので、
			// 雛形の親子の差をそのまま使う。
			step := indentOf(tmpl[tIdx])
			if tParent, ok := findKeyLine(tmpl, tStart, tEnd, parent); ok {
				step -= indentOf(tmpl[tParent])
			}
			wantIndent = indentOf(user[uParent]) + step
		}
	}
	block = shiftIndent(block, wantIndent-indentOf(tmpl[tIdx]))

	siblings := siblingsOf(paths, p)
	self := indexOfPath(siblings, p)

	// 直前の兄弟を探す。**近いほうから見る。**
	for i := self - 1; i >= 0; i-- {
		name := strings.Join(siblings[i], ".")
		if pos, ok := planned[name]; ok {
			// **同じ位置へ後ろから積む。**insertions が積んだ順に並べるので、
			// 雛形の並び順がそのまま保たれる。
			return pos, block
		}
		if idx, ok := findKeyLine(user, uStart, uEnd, siblings[i]); ok {
			_, end := blockRange(user, uStart, uEnd, idx)
			return end, block
		}
	}
	// 直後の兄弟を探す。**その説明のコメント行より前へ置く。**
	for i := self + 1; i < len(siblings); i++ {
		if idx, ok := findKeyLine(user, uStart, uEnd, siblings[i]); ok {
			start, _ := blockRange(user, uStart, uEnd, idx)
			return start, block
		}
	}
	return scopeStart, block
}

// firstChildIndent は、そのキーのブロックの子が書かれている深さを返す。
//
// **差し込む行の字下げは、ここで返す深さに合わせる。**親の行どうしを比べても子の深さは
// 決まらない。**利用者が親を雛形と同じ深さで、子だけ深く書いていることがある**
// （`tracker:` の子を4スペースで書いた `WORKFLOW.md` がその形である）。
// 親の行から計算すると、雛形の2スペースのまま差し込まれ、
// **当てたあとの front matter が YAML として読めなくなる。**
//
// **子として数えるのは、最初に見つかった、親より深い行である**（findKeyLine と同じ判断）。
// 空行とコメント行は飛ばす。
//
// lines: 行の並び。
// parentIdx: 親のキーの行の添字。
// end: 探す範囲の終わり（含まない）。
// 戻り値の1つ目: 子の深さ。
// 戻り値の2つ目: 子が1行も無ければ偽。
func firstChildIndent(lines []string, parentIdx, end int) (int, bool) {
	parentIndent := indentOf(lines[parentIdx])
	for i := parentIdx + 1; i < end; i++ {
		trimmed := strings.TrimLeft(trimEOL(lines[i]), " \t")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := indentOf(lines[i])
		if indent <= parentIndent {
			return 0, false
		}
		return indent, true
	}
	return 0, false
}

// dropLeadingCommentsAlreadyThere は、利用者のファイルに既にある見出しのコメントを
// ブロックの先頭から落とす。
//
// **雛形は `# ===== 後始末・使用量・二重起動の防止 =====` のように、複数の節をまとめて
// 指す見出しのコメントを持っている。**そのコメントは、すぐ下の節（`naming` など）の
// ブロックに入ってしまう。**利用者のファイルにその見出しが既にあると、同じ行が2本並ぶ。**
//
// **落とすのは、文字が1字も違わない行だけである。**利用者が書き換えた行は残す。
//
// block: 差し込む行の並び（最後の行は必ずキーの行なので、全部は落ちない）。
// user: 利用者の WORKFLOW.md を改行で分けた行の並び。
// uStart, uEnd: 利用者のファイルの front matter の範囲（uEnd は含まない）。
// 戻り値: 先頭の重複するコメント行を落としたブロック。
func dropLeadingCommentsAlreadyThere(block, user []string, uStart, uEnd int) []string {
	have := make(map[string]bool)
	for i := uStart; i < uEnd; i++ {
		line := strings.TrimSpace(trimEOL(user[i]))
		if strings.HasPrefix(line, "#") {
			have[line] = true
		}
	}
	i := 0
	for i < len(block) {
		line := strings.TrimSpace(trimEOL(block[i]))
		if !strings.HasPrefix(line, "#") || !have[line] {
			break
		}
		i++
	}
	return block[i:]
}

// siblingsOf は、同じ親を持つキーを雛形の並び順で返す。
//
// paths: 雛形のキーの一覧。
// p: 基準にするキーのパス。
// 戻り値: p と同じ親を持つキーのパス（p 自身を含む）。
func siblingsOf(paths [][]string, p []string) [][]string {
	var out [][]string
	for _, q := range paths {
		if len(q) != len(p) {
			continue
		}
		if !samePath(q[:len(q)-1], p[:len(p)-1]) {
			continue
		}
		out = append(out, q)
	}
	return out
}

// indexOfPath は、パスの一覧の中で同じパスが何番目かを返す。
//
// paths: 探す対象の一覧。
// p: 探すパス。
// 戻り値: 見つかった添字。見つからなければ -1。
func indexOfPath(paths [][]string, p []string) int {
	for i, q := range paths {
		if samePath(q, p) {
			return i
		}
	}
	return -1
}

// samePath は2つのキーのパスが同じかを返す。
//
// a, b: 比べるパス。
// 戻り値: 長さも中身も同じなら真。
func samePath(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// blockRange は、キーの行とそれに属する行の範囲を返す。
//
// **上に続く同じ字下げのコメント行も含める。**雛形は `branch_template` のように、
// キーの説明を上の行に書いているものがある。**キーの行だけを持っていくと説明が落ちる。**
//
// **下は、より深く字下げされた行を含める。**入れ子のキー・リストの項目・
// 右端に揃えた説明の続きが、すべてこの形で書かれている。**空行で切る。**
//
// lines: 行の並び。
// start, end: 探す範囲（end は含まない）。
// idx: キーの行の添字。
// 戻り値: ブロックの最初の行の添字と、ブロックの次の行の添字。
func blockRange(lines []string, start, end, idx int) (int, int) {
	keyIndent := indentOf(lines[idx])

	bStart := idx
	for j := idx - 1; j >= start; j-- {
		line := trimEOL(lines[j])
		trimmed := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trimmed, "#") || len(line)-len(trimmed) != keyIndent {
			break
		}
		bStart = j
	}

	bEnd := idx + 1
	for j := idx + 1; j < end; j++ {
		line := trimEOL(lines[j])
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || len(line)-len(trimmed) <= keyIndent {
			break
		}
		bEnd = j + 1
	}
	return bStart, bEnd
}

// indentOf は行頭の空白の数を返す。
//
// line: 数える行。
// 戻り値: 行頭の空白とタブの数。
func indentOf(line string) int {
	l := trimEOL(line)
	return len(l) - len(strings.TrimLeft(l, " \t"))
}

// shiftIndent は行の並びの字下げをまとめてずらす。
//
// **利用者が雛形と違う深さで書いていても当たるようにする。**
//
// lines: ずらす行の並び。
// delta: 足す空白の数（負なら削る）。
// 戻り値: ずらした行の並び。delta が 0 ならそのまま返す。
func shiftIndent(lines []string, delta int) []string {
	if delta == 0 {
		return lines
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		switch {
		case strings.TrimSpace(line) == "":
			out = append(out, line)
		case delta > 0:
			out = append(out, strings.Repeat(" ", delta)+line)
		default:
			n := -delta
			if n > indentOf(line) {
				n = indentOf(line)
			}
			out = append(out, line[n:])
		}
	}
	return out
}

// unifiedDiff は差し込む行の並びから unified diff を組み立てる。
//
// **足すだけの差分である。**1行も消さないので、当てても利用者が手で書いた行は残る。
//
// path: WORKFLOW.md の絶対パス。
// orig: 元の行の並び。
// insertions: 差し込む位置（orig の添字）と、そこへ入れる行。
// hadFinalNewline: 元の全文が改行で終わっていたか。
// crlf: 元の全文が CRLF で書かれているか。**真なら差し込む行にも CR を足す**
// （混ざったファイルにしないため）。
// 戻り値: unified diff。差し込むものが無ければ空文字。
func unifiedDiff(path string, orig []string, insertions map[int][]string, hadFinalNewline, crlf bool) string {
	if len(insertions) == 0 {
		return ""
	}
	positions := make([]int, 0, len(insertions))
	for pos := range insertions {
		positions = append(positions, pos)
	}
	sort.Ints(positions)

	var b strings.Builder
	b.WriteString("--- " + path + "\n")
	b.WriteString("+++ " + path + "\n")

	offset := 0
	for i := 0; i < len(positions); {
		// **文脈が重なる差し込みは1つの塊にまとめる。**離れていれば別の塊にする。
		j := i
		for j+1 < len(positions) && positions[j+1]-positions[j] <= 2*diffContext {
			j++
		}
		hunkStart := positions[i] - diffContext
		if hunkStart < 0 {
			hunkStart = 0
		}
		hunkEnd := positions[j] + diffContext
		if hunkEnd > len(orig) {
			hunkEnd = len(orig)
		}
		added := 0
		for k := i; k <= j; k++ {
			added += len(insertions[positions[k]])
		}
		oldCount := hunkEnd - hunkStart
		b.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n",
			hunkStart+1, oldCount, hunkStart+1+offset, oldCount+added))

		for k := hunkStart; k <= hunkEnd; k++ {
			for _, add := range insertions[k] {
				if crlf {
					add += "\r"
				}
				b.WriteString("+" + add + "\n")
			}
			if k == hunkEnd {
				break
			}
			b.WriteString(" " + orig[k] + "\n")
			if k == len(orig)-1 && !hadFinalNewline {
				b.WriteString("\\ No newline at end of file\n")
			}
		}
		offset += added
		i = j + 1
	}
	return b.String()
}
