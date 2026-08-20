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

// repositoriesFilledComment は trust.repositories を埋めたあとに残すコメントの1行目である。
//
// **「要らない行を消すこと」を必ず残す。**ボードから拾った一覧をそのまま信頼させないための
// 一文であり、これが消えると人間が削る手順そのものが伝わらない（設計 3-33）。
const repositoriesFilledComment = "continuo trust が信頼を登録してよいリポジトリ。ボードから拾って並べた。"

// repositoriesFilledComment2 は trust.repositories を埋めたあとに残すコメントの2行目である。
const repositoriesFilledComment2 = "**要らない行は消すこと。**ここに残ったものだけが登録の対象になる"

// ownerPattern は tracker.provider.owner として受け付ける文字の範囲である。
//
// GitHub の user / organization 名は英数字とハイフンだけで、39文字以内である。
// ここで弾く目的は2つある。1つは打ち間違いをその場で知らせること、もう1つは
// gh の出力をそのまま YAML へ書くときに、引用符や改行を混ぜられないようにすることである。
var ownerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)

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
	out := workflowTemplate
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
		out = applyStatuses(out, values.Statuses)
	}
	return out
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
	pad := commentColumn - displayWidth(code)
	if pad < 1 {
		pad = 3
	}
	return code + strings.Repeat(" ", pad) + "# " + comment
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

// activeStatesPlaceholderCode は雛形の active_states の行の、コメントより前の部分である。
const activeStatesPlaceholderCode = `  active_states: ["Ready", "In Progress"]`

// terminalStatesPlaceholderCode は雛形の terminal_states の行の、コメントより前の部分である。
const terminalStatesPlaceholderCode = `  terminal_states: ["Done"]`

// runningStatePlaceholderCode は雛形の running_state の行の、コメントより前の部分である。
const runningStatePlaceholderCode = `  running_state: "In Progress"`

// dispatchStatePlaceholderCode は雛形の dispatch_state の行の、コメントより前の部分である。
const dispatchStatePlaceholderCode = `  dispatch_state: "Ready"`

// failureStatePlaceholderCode は雛形の failure_state の行の、コメントより前の部分である。
const failureStatePlaceholderCode = `  failure_state: "Blocked"`

// signalReviewPlaceholderCode は雛形の status_signal_map.review の行の、コメントより前の部分である。
const signalReviewPlaceholderCode = `    review: "In Review"`

// signalBlockedPlaceholderCode は雛形の status_signal_map.blocked の行の、コメントより前の部分である。
const signalBlockedPlaceholderCode = `    blocked: "Blocked"`

// 割り当てた Status を書き込んだあとに残すコメント。**雛形の文面をそのまま使う。**
// 値だけが変わって説明が変わらないので、別の文面にすると同じキーの説明が2通りになる。
const (
	activeStatesComment   = "対象にする Status。下の running_state と dispatch_state を必ず含めること"
	terminalStatesComment = "終わったとみなす Status。ここへ移った issue の worktree を片付ける"
	runningStateComment   = "エージェントを起動したときに書き込む Status"
	dispatchStateComment  = "着手待ちの Status。取り残された issue はここへ戻す"
	failureStateComment   = "打ち切ったとき・失敗したときに落とす Status"
	signalReviewComment   = "作業が終わり、人間のレビューに回してよいとき"
	signalBlockedComment  = "判断を仰ぎたいとき、または失敗したとき"
)

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

// applyStatuses は雛形の Status に関する7行を、割り当てた選択肢名で置き換える。
//
// **値は %q で囲む。**選択肢名は空白を含みうる（`In Progress`）し、`Done` のような
// 素の語でも YAML の別の型として読まれうるので、必ず引用符を付ける。
//
// s: 差し替える対象の全文。
// st: 割り当てた選択肢名（Complete が真であること）。
// 戻り値: 差し替えた全文。
func applyStatuses(s string, st Statuses) string {
	s = replaceLine(s, activeStatesPlaceholderCode,
		fmt.Sprintf("  active_states: [%q, %q]", st.Dispatch, st.Running), activeStatesComment)
	s = replaceLine(s, terminalStatesPlaceholderCode,
		fmt.Sprintf("  terminal_states: [%q]", st.Done), terminalStatesComment)
	s = replaceLine(s, runningStatePlaceholderCode,
		fmt.Sprintf("  running_state: %q", st.Running), runningStateComment)
	s = replaceLine(s, dispatchStatePlaceholderCode,
		fmt.Sprintf("  dispatch_state: %q", st.Dispatch), dispatchStateComment)
	s = replaceLine(s, failureStatePlaceholderCode,
		fmt.Sprintf("  failure_state: %q", st.Blocked), failureStateComment)
	s = replaceLine(s, signalReviewPlaceholderCode,
		fmt.Sprintf("    review: %q", st.Review), signalReviewComment)
	s = replaceLine(s, signalBlockedPlaceholderCode,
		fmt.Sprintf("    blocked: %q", st.Blocked), signalBlockedComment)
	return s
}
