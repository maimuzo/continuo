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
	return out
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

// alignComment は「値の部分」と「コメントの中身」から、桁をそろえた1行を組み立てる。
//
// code: コメントより前の部分。
// comment: コメントの中身（`# ` は含まない）。
// 戻り値: code とコメントの間を空白で埋め、`#` が commentColumn 桁に来るようにした1行。
// code が長すぎて桁に収まらない場合は、空白3つで区切る（雛形の中の長い行と同じ書き方）。
func alignComment(code, comment string) string {
	pad := commentColumn - len(code)
	if pad < 1 {
		pad = 3
	}
	return code + strings.Repeat(" ", pad) + "# " + comment
}
