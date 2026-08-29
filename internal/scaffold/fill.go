package scaffold

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/maimuzo/continuo/internal/config"
)

// commentColumn は front matter のコメント（`#`）を揃える桁である（0 起点）。
// 雛形の全行がこの桁でそろっているので、値を埋めたあともここに合わせる。
const commentColumn = 44

// ownerFilledComment は tracker.provider.owner を埋めたあとに残すコメントである。
// 「ここを埋めること」は消す。埋まっているのに埋めろと書いてあると、読む人が迷う。
const ownerFilledComment = "例: https://github.com/maimuzo なら maimuzo"

// projectFilledComment は tracker.provider.project_number を埋めたあとに残すコメントである。
const projectFilledComment = "例: https://github.com/users/maimuzo/projects/3 なら 3"

// ownerPlaceholderCode は雛形の owner の行の、コメントより前の部分である。
// 行の先頭から一致させることで、branch_template の中の {{.issue.owner}} のような
// 別の場所を取り違えないようにする。
const ownerPlaceholderCode = "    owner: " + config.Placeholder

// projectPlaceholderCode は雛形の project_number の行の、コメントより前の部分である。
const projectPlaceholderCode = "    project_number: 0"

// repositoriesPlaceholderCode は雛形の trust.repositories の行の、コメントより前の部分である。
const repositoriesPlaceholderCode = "  repositories: []"

// tokenSourceCode は雛形の rate_limit.token_source の行の、コメントより前の部分である。
//
// **雛形が持つ値は `claude_credentials` である**（どの OS でも読める値）。
// 書き出すときだけ、走っている OS の既定へ差し替える（tokenSourceLine）。
const tokenSourceCode = "  token_source: " + config.RateLimitTokenSourceClaudeCredentials

// tokenSourceKeychainComment は rate_limit.token_source を keychain にしたときのコメントである。
//
// **先に `continuo allow-keychain-access` を実行させる案内を必ず残す。**これが無いと、
// 確認のダイアログが出たまま無人のプロセスが枠を読めずに終わる。
const tokenSourceKeychainComment = "macOS の Keychain から読む。先に continuo allow-keychain-access を1回実行すること。" +
	"claude_credentials なら ~/.claude/.credentials.json、env なら下の token_env から読む"

// repositoriesFilledComment は trust.repositories を埋めたあとに残すコメントの1行目である。
//
// **「要らない行を消すこと」を必ず残す。**ボードから拾った一覧をそのまま信頼させないための
// 一文であり、これが消えると人間が削る手順そのものが伝わらない（設計 3-33）。
const repositoriesFilledComment = "continuo trust が信頼を登録してよいリポジトリ。ボードから拾って並べた。"

// repositoriesFilledComment2 は trust.repositories を埋めたあとに残すコメントの2行目である。
const repositoriesFilledComment2 = "**要らない行は消すこと。**ここに残ったものだけが登録の対象になる"

// repositoriesFilledComment3 は trust.repositories を埋めたあとに残すコメントの3行目である。
//
// **ボードに載っていないリポジトリは自動では入らない。**これから issue を作る
// リポジトリは、この時点ではボードに無いので拾えない。手で足す必要がある。
const repositoriesFilledComment3 = "**これから issue を作るリポジトリは、まだボードに無いので入っていない。**手で足すこと"

// ownerPattern は tracker.provider.owner として受け付ける文字の範囲である。
//
// GitHub の user / organization 名は英数字とハイフンだけで、39文字以内である。
// **ハイフンで始めることも終わることもできない**（GitHub 側で弾かれる）。
// ここで弾く目的は2つある。1つは打ち間違いをその場で知らせること、もう1つは
// gh の出力をそのまま YAML へ書くときに、引用符や改行を混ぜられないようにすることである。
var ownerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

// Values は雛形のプレースホルダに書き込む値である。
//
// ゼロ値は「決まらなかった」を表す。Owner が空文字、ProjectNumber が 0 の場合、
// その行はプレースホルダのまま残る（continuo の起動時に config が名指しで落とす）。
type Values struct {
	// Owner は tracker.provider.owner に書く GitHub の user / organization 名である。
	Owner string
	// ProjectNumber は tracker.provider.project_number に書くボードの番号である。
	ProjectNumber int
	// Repositories は trust.repositories に並べる "owner/repo" である。
	//
	// **空なら雛形の `repositories: []` をそのまま残す。**空の一覧を書き下すより、
	// 何も拾えなかったことが見て取れる形のほうがよい。
	Repositories []string
	// Statuses は continuo の5つの役割へ割り当てたボードの Status の選択肢名である。
	//
	// **決めるのは `continuo setup` だけである。**`continuo init` は空のまま渡すので、
	// 雛形の既定値（`Ready` / `In Progress` / `In Review` / `Blocked` / `Done`）が残る。
	Statuses Statuses
}

// ValidOwner は owner として受け付けられる文字列かどうかを返す。
//
// name: 検査する user / organization 名。
// 戻り値: 英数字で始まり、英数字とハイフンだけからなる39文字以内の文字列なら真。
func ValidOwner(name string) bool {
	return ownerPattern.MatchString(name)
}

// TemplateWithValues は雛形のプレースホルダを values で埋めた全文を返す。
//
// values: 書き込む値。Owner が空文字、または ProjectNumber が 0 以下なら、
// その行はプレースホルダのまま残す。Owner が ValidOwner を通らない場合も書き込まない
// （YAML を壊す文字が混ざったまま書き出すより、プレースホルダのまま残したほうが直しやすい）。
// Statuses は5つの役割が全部埋まっているときだけ書き込む（Statuses.Complete）。
// 戻り値: WORKFLOW.md の全文。
func TemplateWithValues(values Values) string {
	out := applyRateLimitTokenSource(workflowTemplate)
	if values.Owner != "" && ValidOwner(values.Owner) {
		out = replaceLine(out, ownerPlaceholderCode, "    owner: "+values.Owner, ownerFilledComment)
	}
	if values.ProjectNumber > 0 {
		out = replaceLine(out, projectPlaceholderCode,
			fmt.Sprintf("    project_number: %d", values.ProjectNumber), projectFilledComment)
	}
	if len(values.Repositories) > 0 {
		out = replaceLineWithBlock(out, repositoriesPlaceholderCode, repositoriesBlock(values.Repositories))
	}
	if values.Statuses.Complete() {
		// **雛形には8つのキーが必ずあり、どれも値を同じ行に持つので、
		// 見つからないことも書き換えられないことも起こらない。**
		// 雛形を壊したときは test/internal/scaffold/statuses_test.go が落とす。
		out, _, _ = applyStatuses(out, values.Statuses)
	}
	return out
}

// applyRateLimitTokenSource は rate_limit.token_source の行を、走っている OS の既定へ差し替える。
//
// **雛形は文字列を1つしか持てないので、書き出すときに差し替える。**
// **`continuo init` が書いた値は、そのファイルを読むときの既定値より強い。**macOS で
// `claude_credentials` と書き出すと、`~/.claude/.credentials.json` が無いのが普通の環境で
// 枠の判定が黙って効かなくなる（config.DefaultConfig の既定を macOS で keychain にした意味が消える）。
//
// **差し替えるのは値とコメントだけである。**キーは動かないので、設計 5-2 とのキーの照合には
// 影響しない。
//
// s: 差し替える対象の全文。
// 戻り値: 走っている OS の既定を書いた全文。既定が雛形の値と同じ（macOS 以外）ならそのまま返す。
func applyRateLimitTokenSource(s string) string {
	want := config.DefaultConfig().RateLimit.TokenSource
	if want == config.RateLimitTokenSourceClaudeCredentials {
		return s
	}
	return replaceLine(s, tokenSourceCode, "  token_source: "+want, tokenSourceKeychainComment)
}

// repositoriesBlock は trust.repositories の行を、拾った一覧を並べた複数行に組み立てる。
//
// **値は引用符で囲む。**owner/repo は英数字とハイフン・ドット・アンダースコアだけなので
// 素で書いても YAML としては読めるが、`8.0` のような repo 名が数値として読まれるのを防ぐ。
//
// repos: 並べる "owner/repo"。**呼び出し側が config の検査（trust.repositories の形）を
// 通した値だけを渡すこと。**
// 戻り値: 差し替えたあとの行の並び。
func repositoriesBlock(repos []string) []string {
	lines := []string{
		alignComment("  repositories:", repositoriesFilledComment),
		strings.Repeat(" ", commentColumn) + "# " + repositoriesFilledComment2,
		strings.Repeat(" ", commentColumn) + "# " + repositoriesFilledComment3,
	}
	for _, r := range repos {
		lines = append(lines, fmt.Sprintf("    - %q", r))
	}
	return lines
}

// replaceLine は oldCode で始まる行を、newCode とコメントで組み立て直した1行に差し替える。
//
// s: 差し替える対象の全文。
// oldCode: 探す行の、コメントより前の部分（行頭から完全に一致させる）。
// newCode: 差し替えたあとの、コメントより前の部分。
// comment: 差し替えたあとのコメント（`# ` は付けずに中身だけ渡す）。
// 戻り値: 差し替えた全文。該当する行が1つも無ければ、元の全文をそのまま返す。
func replaceLine(s, oldCode, newCode, comment string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		// oldCode の直後が行末か空白であることを確かめる。前方一致だけで判定すると、
		// 同じ書き出しで始まる別のキー（project_number: 0 と project_number: 03 など）を拾う。
		if line != oldCode && !strings.HasPrefix(line, oldCode+" ") {
			continue
		}
		lines[i] = alignComment(newCode, comment)
		return strings.Join(lines, "\n")
	}
	return s
}

// replaceLineWithBlock は oldCode で始まる行と、その直後に続くコメントだけの行を、
// newLines の並びで丸ごと差し替える。
//
// **直後のコメント行も一緒に消すのが要点である。**雛形は1つのキーの説明を複数行に
// またがって書いているので（`repositories: []` の下に2行の補足がある）、キーの行だけを
// 差し替えると、埋めたあとの値に古い説明が残る。
//
// s: 差し替える対象の全文。
// oldCode: 探す行の、コメントより前の部分（行頭から完全に一致させる）。
// newLines: 差し替えたあとの行の並び。
// 戻り値: 差し替えた全文。該当する行が1つも無ければ、元の全文をそのまま返す。
func replaceLineWithBlock(s, oldCode string, newLines []string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		// replaceLine と同じ理由で、oldCode の直後が行末か空白であることを確かめる。
		if line != oldCode && !strings.HasPrefix(line, oldCode+" ") {
			continue
		}
		end := i + 1
		for end < len(lines) && strings.HasPrefix(strings.TrimLeft(lines[end], " "), "#") {
			end++
		}
		out := make([]string, 0, len(lines)-(end-i)+len(newLines))
		out = append(out, lines[:i]...)
		out = append(out, newLines...)
		out = append(out, lines[end:]...)
		return strings.Join(out, "\n")
	}
	return s
}

// alignComment は「値の部分」と「コメントの中身」から、桁をそろえた1行を組み立てる。
//
// code: コメントより前の部分。
// comment: コメントの中身（`# ` は含まない）。
// 戻り値: code とコメントの間を空白で埋め、`#` が commentColumn 桁に来るようにした1行。
// code が長すぎて桁に収まらない場合は、空白3つで区切る（雛形の中の長い行と同じ書き方）。
func alignComment(code, comment string) string {
	return joinComment(code, "# "+comment)
}

// joinComment は「値の部分」と「`#` から始まるコメントの原文」を、桁をそろえた1行にする。
//
// **原文のまま受け取るのは、既にある WORKFLOW.md のコメントを消さないためである。**
// `continuo setup` は利用者が書き換えたコメントもそのまま残す。
//
// code: コメントより前の部分。
// rawComment: `#` から始まるコメントの原文。
// 戻り値: code とコメントの間を空白で埋め、`#` が commentColumn 桁に来るようにした1行。
// code が長すぎて桁に収まらない場合は、空白3つで区切る（雛形の中の長い行と同じ書き方）。
func joinComment(code, rawComment string) string {
	pad := commentColumn - displayWidth(code)
	if pad < 1 {
		pad = 3
	}
	return code + strings.Repeat(" ", pad) + rawComment
}

// displayWidth は等幅の端末で code が占める桁数を返す。
//
// **バイト数では桁がそろわない。**`continuo setup` はボードの選択肢名をそのまま書き込むので、
// 日本語の選択肢名（`レビュー 待ち` など）が入りうる。UTF-8 の日本語は1文字3バイトだが
// 画面では2桁なので、バイト数で数えるとコメントが左へずれて front matter 全体が崩れる。
//
// **全角として数えるのは、日本語の表記に現れる範囲だけである。**絵文字や結合文字まで
// 正確に数えるには文字幅の表が要る。ここで欲しいのはコメントの桁そろえだけなので、
// 表を持ち込まずに、外した場合でも1桁ずれるだけの範囲に留める。
//
// code: 数える文字列。
// 戻り値: 占める桁数。
func displayWidth(code string) int {
	w := 0
	for _, r := range code {
		if isWideRune(r) {
			w += 2
			continue
		}
		w++
	}
	return w
}

// isWideRune は等幅の端末で2桁を占める文字かどうかを返す。
//
// r: 判定する文字。
// 戻り値: 全角（2桁）なら真。
func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F: // ハングルの字母
		return true
	case r >= 0x2E80 && r <= 0x303E: // 部首・記号・日本語の句読点
		return true
	case r >= 0x3041 && r <= 0x33FF: // ひらがな・カタカナ・ハングル・組文字
		return true
	case r >= 0x3400 && r <= 0x4DBF: // 漢字（拡張A）
		return true
	case r >= 0x4E00 && r <= 0x9FFF: // 漢字
		return true
	case r >= 0xF900 && r <= 0xFAFF: // 互換漢字
		return true
	case r >= 0xFF00 && r <= 0xFF60: // 全角の英数字と記号
		return true
	case r >= 0xFFE0 && r <= 0xFFE6: // 全角の通貨記号など
		return true
	}
	return false
}

// frontMatterDelimiter は front matter の開始・終了を示す行である。
//
// **internal/config の splitFrontMatter と同じ規則で判定する。**判定がずれると、
// `continuo setup` が本文の中の行を書き換えたり、front matter の中の行を見落としたりする。
const frontMatterDelimiter = "---"

// statusKey は `continuo setup` が書き換える front matter のキー1つを表す。
type statusKey struct {
	// path は front matter のルートから辿るキーの並びである。
	path []string
	// value は割り当てから、そのキーへ書く YAML の値を組み立てる。
	value func(Statuses) string
}

// statusKeys は `continuo setup` が書き換える8つのキーである。**ここに無いキーは触らない。**
//
// **値は %q で囲む。**選択肢名は空白を含みうる（`In Progress`）し、`Done` のような
// 素の語でも YAML の別の型として読まれうるので、必ず引用符を付ける。
var statusKeys = []statusKey{
	{
		path:  []string{"tracker", "status_signal_map", "review"},
		value: func(st Statuses) string { return fmt.Sprintf("%q", st.Review) },
	},
	{
		path:  []string{"tracker", "status_signal_map", "blocked"},
		value: func(st Statuses) string { return fmt.Sprintf("%q", st.Blocked) },
	},
	{
		path:  []string{"tracker", "active_states"},
		value: func(st Statuses) string { return fmt.Sprintf("[%q, %q]", st.Dispatch, st.Running) },
	},
	{
		path:  []string{"tracker", "terminal_states"},
		value: func(st Statuses) string { return fmt.Sprintf("[%q]", st.Done) },
	},
	{
		path:  []string{"tracker", "running_state"},
		value: func(st Statuses) string { return fmt.Sprintf("%q", st.Running) },
	},
	{
		path:  []string{"tracker", "dispatch_state"},
		value: func(st Statuses) string { return fmt.Sprintf("%q", st.Dispatch) },
	},
	{
		path:  []string{"tracker", "failure_state"},
		value: func(st Statuses) string { return fmt.Sprintf("%q", st.Blocked) },
	},
	{
		// **片付けを始める Status も割り当てから書く。**ここを雛形の `["Done"]` のまま
		// 残すと、ボードの完了の選択肢が別名（`完了` など）の環境で片付けが一度も走らず、
		// worktree と branch が永久に残る。**その状態は誰も指摘しない**
		// （`Done` は active_states に無いので config の検証を通り、起動時の Status の
		// 照合も cleanup を見ない）。利用者からは「Done にしたのに worktree が消えない」
		// だけが見える。
		path:  []string{"cleanup", "on_states"},
		value: func(st Statuses) string { return fmt.Sprintf("[%q]", st.Done) },
	},
}

// StatusKeyNames は `continuo setup` が書き換えるキーの名前を、ドット区切りで返す。
//
// **`continuo setup` は、ここに並んだキー以外の行を1文字も変えない。**画面に
// 「何を書き換えたか」を出すために公開している。
//
// 戻り値: `tracker.dispatch_state` のような名前の並び（呼び出し側が書き換えても内部には影響しない）。
func StatusKeyNames() []string {
	out := make([]string, 0, len(statusKeys))
	for _, k := range statusKeys {
		out = append(out, strings.Join(k.path, "."))
	}
	return out
}

// Statuses は continuo の5つの役割へ割り当てたボードの Status の選択肢名である。
//
// **決めるのは `continuo setup` である**（利用者に番号で選ばせる）。
// ここに値が揃っていると、TemplateWithValues が雛形の既定値
// （`Ready` / `In Progress` / `In Review` / `Blocked` / `Done`）を置き換える。
//
// **5つのうち1つでも空なら、雛形の既定値をそのまま残す。**一部だけ差し替えると、
// 置き換えた Status と既定値のままの Status が混ざった WORKFLOW.md ができる。
type Statuses struct {
	// Dispatch は着手待ちの Status である（dispatch_state と active_states の1つめ）。
	Dispatch string
	// Running は作業中の Status である（running_state と active_states の2つめ）。
	Running string
	// Review はレビュー待ちの Status である（status_signal_map.review）。
	Review string
	// Blocked は保留の Status である（failure_state と status_signal_map.blocked）。
	Blocked string
	// Done は完了の Status である（terminal_states の1つめ）。
	Done string
}

// Complete は5つの役割すべてに選択肢名が入っているかを返す。
//
// 戻り値: 5つとも空文字でなければ真。
func (s Statuses) Complete() bool {
	return s.Dispatch != "" && s.Running != "" && s.Review != "" && s.Blocked != "" && s.Done != ""
}

// applyStatuses は front matter の Status に関する8行を、割り当てた選択肢名で置き換える。
//
// **値の部分だけを組み立て直す。**行の右側のコメントは原文のまま残し、他の行・空行・
// 並び順・インデントは1文字も変えない。**`continuo setup` は既にある WORKFLOW.md へ
// これを当てるので、利用者が手で書き換えた行を消してはならない。**
//
// **探すのは front matter の中だけである。**本文には `CONTINUO-STATUS: review` のように
// 似た形の行があるので、範囲を切らないと本文を書き換えうる。
//
// **値が下の行にぶら下がっているキーは書き換えない。**`active_states:` の下に
// `- "Ready"` が並ぶ形（block 形式）でキーの行だけを組み立て直すと、下の行が残って
// YAML として読めないファイルになる。**そのまま書くと「成功しました」と出たあとに
// continuo が一切起動しなくなる**ので、書き換えずに名前を返して呼び出し側に止めさせる。
//
// s: 差し替える対象の全文（front matter と本文）。
// st: 割り当てた選択肢名（Complete が真であること）。
// 戻り値の1つ目: 差し替えた全文。
// 戻り値の2つ目: 見つからなかったキーの名前（ドット区切り）。
// 戻り値の3つ目: 見つかったが書き換えられない形だったキーの名前（ドット区切り）。
// front matter を切り出せない場合は、全文をそのまま返し、全部を見つからなかったものとして返す。
func applyStatuses(s string, st Statuses) (string, []string, []string) {
	lines := strings.Split(s, "\n")
	start, end, ok := frontMatterRange(lines)
	if !ok {
		return s, StatusKeyNames(), nil
	}

	var missing, blocked []string
	for _, k := range statusKeys {
		i, found := findKeyLine(lines, start, end, k.path)
		if !found {
			missing = append(missing, strings.Join(k.path, "."))
			continue
		}
		if hasNestedValue(lines, i, end) {
			blocked = append(blocked, strings.Join(k.path, "."))
			continue
		}
		lines[i] = rewriteValue(lines[i], k.path[len(k.path)-1], k.value(st))
	}
	// **書き換えられない形が1つでもあれば、1行も書き換えない。**一部だけ書き換えた全文を
	// 返すと、呼び出し側が止めても「途中まで当たった文字列」が残る。
	if len(blocked) > 0 {
		return s, missing, blocked
	}
	return strings.Join(lines, "\n"), missing, blocked
}

// hasNestedValue は、キーの行の値が下の行にぶら下がっているかを返す。
//
// **YAML では、キーの行に値が無いとき、下のより深い行が値である**（`- "x"` の並びや
// 入れ子のキー）。その形で行を1本だけ組み立て直すと、下の行が値の残骸として残る。
//
// lines: WORKFLOW.md を改行で分けた行の並び。
// i: キーの行の添字。
// end: 探す範囲の終わり（front matter の終端の区切り行の添字。含まない）。
// 戻り値: 値が下の行にぶら下がっていれば真。キーの行に値が書いてあれば偽。
func hasNestedValue(lines []string, i, end int) bool {
	line := trimEOL(lines[i])
	trimmed := strings.TrimLeft(line, " \t")
	keyIndent := len(line) - len(trimmed)
	// キーの行に値が書いてあれば、下の行は別のキーである。
	if _, _, rest := splitKeyValue(trimmed); strings.TrimSpace(stripComment(rest)) != "" {
		return false
	}
	for j := i + 1; j < end; j++ {
		l := trimEOL(lines[j])
		t := strings.TrimLeft(l, " \t")
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if len(l)-len(t) <= keyIndent {
			// 同じ深さかそれより浅い行に当たった。このキーの値は空である。
			return false
		}
		return true
	}
	return false
}

// splitKeyValue は `key: value` の形の行を、キーと区切りと値に分ける。
//
// trimmed: 行頭の空白を落とした1行。
// 戻り値: キー、区切り（`:`）、値の部分。`:` が無ければ3つ目に元の文字列を返す。
func splitKeyValue(trimmed string) (string, string, string) {
	idx := strings.Index(trimmed, ":")
	if idx < 0 {
		return "", "", trimmed
	}
	return trimmed[:idx], ":", trimmed[idx+1:]
}

// stripComment は、値の部分から行の右側のコメントを落とす。
//
// s: 値の部分（キーと `:` より右）。
// 戻り値: コメントを落とした値の部分。
func stripComment(s string) string {
	if c := inlineComment(s); c != "" {
		return strings.TrimSuffix(s, c)
	}
	return s
}

// trimEOL は行末の CR を落とす。
//
// **CRLF の WORKFLOW.md でも同じ判定になるようにする。**internal/config の
// splitFrontMatter は CRLF を LF に直してから読むので、ここで落とさないと
// 「doctor では読めるのに setup ではキーが1つも見つからない」という食い違いが起きる。
//
// line: 1行。
// 戻り値: 行末の CR を落とした1行。
func trimEOL(line string) string {
	return strings.TrimSuffix(line, "\r")
}

// frontMatterRange は行の並びの中で front matter が占める範囲を返す。
//
// lines: WORKFLOW.md を改行で分けた行の並び。
// 戻り値: front matter の最初の行の添字（開始の区切り行の次）、終端の区切り行の添字、
// front matter を切り出せたかどうか。
func frontMatterRange(lines []string) (start, end int, ok bool) {
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t\r") != frontMatterDelimiter {
		return 0, 0, false
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t\r") == frontMatterDelimiter {
			return 1, i, true
		}
	}
	return 0, 0, false
}

// findKeyLine は、キーのパスで指した行を front matter の中から探す。
//
// **入れ子は行頭のインデントで辿る。**`status_signal_map` の下の `review` を、
// 別の場所にある同じ名前のキーと取り違えないためである。親のキーより浅い行に
// 当たった時点でその親のブロックは終わりなので、そこで探すのをやめる。
//
// **子として数えるのは、その親の最初の子と同じ深さの行だけである。**「親より深ければ子」に
// すると、**孫を子と取り違える。**雛形には `tracker.provider.comments` と
// `tracker.comments` の2つがあり、前者のほうが先に書かれている。深さを見なければ、
// `tracker.comments` を探したときに `tracker.provider.comments` に当たってしまう。
//
// **深さの値は決め打ちにしない**（2文字とは限らない）。最初に見つけた子の深さを、
// そのブロックの子の深さとして扱う。
//
// lines: WORKFLOW.md を改行で分けた行の並び。
// start, end: 探す範囲（front matter の中。end は含まない）。
// path: ルートから辿るキーの並び（`["tracker", "status_signal_map", "review"]` など）。
// 戻り値: 見つかった行の添字と、見つかったかどうか。
func findKeyLine(lines []string, start, end int, path []string) (int, bool) {
	lo, hi := start, end
	parentIndent := -1
	// childIndent は、いま探しているブロックの子の深さである。**-1 は「まだ決まっていない」。**
	childIndent := -1
	found := -1

	for _, key := range path {
		idx := -1
		for i := lo; i < hi; i++ {
			trimmed := strings.TrimLeft(lines[i], " \t")
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			indent := len(lines[i]) - len(trimmed)
			if indent <= parentIndent {
				// 親のブロックが終わった。これより先に子のキーは無い。
				break
			}
			if childIndent < 0 {
				childIndent = indent
			}
			if indent != childIndent {
				// 孫（またはさらに深い行）である。子ではない。
				continue
			}
			if !strings.HasPrefix(trimmed, key+":") {
				continue
			}
			idx = i
			break
		}
		if idx < 0 {
			return 0, false
		}
		found = idx
		parentIndent = childIndent
		childIndent = -1
		lo = idx + 1
	}
	return found, true
}

// rewriteValue は「<インデント><キー>: <値>  # <コメント>」の1行を、値だけ入れ替えて組み立て直す。
//
// line: 元の行。
// key: その行のキー（元の行から取り直さず、探すのに使った名前をそのまま書く）。
// value: 書き込む YAML の値。
// 戻り値: 組み立て直した1行。元の行にコメントが無ければコメントを付けない。
func rewriteValue(line, key, value string) string {
	// **行末の CR は落とさずに戻す。**CRLF のファイルでこの行だけ LF になると、
	// 改行コードが混ざったファイルができる。
	eol := ""
	if trimmed := trimEOL(line); trimmed != line {
		eol = "\r"
		line = trimmed
	}
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	code := indent + key + ": " + value
	comment := inlineComment(line)
	if comment == "" {
		return code + eol
	}
	return joinComment(code, comment) + eol
}

// inlineComment は行の右側にあるコメントを、`#` から行末まで原文のまま返す。
//
// **引用符の中の `#` はコメントの始まりではない。**Status の選択肢名に `#` が入っていても、
// 値の途中で切らないようにする。**`#` の直前は空白かタブか行頭でなければならない**
// （YAML の規則。`a#b` はコメントではなく値の一部である）。
//
// line: 調べる1行。
// 戻り値: `#` から行末までの原文。コメントが無ければ空文字。
func inlineComment(line string) string {
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case inDouble && c == '\\':
			// 次の1バイトはエスケープされた文字なので、引用符の判定に使わない。
			i++
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '#' && !inSingle && !inDouble:
			if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
				return line[i:]
			}
		}
	}
	return ""
}
