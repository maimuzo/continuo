package scaffold_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/scaffold"
)

// rewriteKeyLinePrefix は、書き戻しの対応表の行を見分ける目印である。
const rewriteKeyLinePrefix = "automated_state_rewrite:"

// 目的: 雛形の `tracker.automated_state_rewrite` の説明が、「何のための設定か」から始まり、
// 要否をその場で判断できる形になっていることを固定する
// （#156（WORKFLOW.md の automated_state_rewrite に何を書けばよいか判断できない））。
//
// **キー構成の検査では、この条件を守れない。**
// TestTemplate_雛形のキー構成が設計5_2の設定例と一致する はキーのパスだけを見るので、
// **説明のコメントを全部消しても通る。**説明は雛形の値そのものであり、
// **これが「どう動くか」から始まっていると、読んでも自分に要るのかが分からない。**
//
// 与える情報: scaffold.Template() の front matter。
// 成功条件: 対応表の行に説明が付いており、その1行目が設定の目的を名乗り、
// 説明の中に「カンバンのどこを見れば要否が決まるか」と「要らないときにどうするか」があること。
func TestTemplate_書き戻しの説明は何のための設定かから始まる(t *testing.T) {
	front := frontMatterOf(t, "雛形", scaffold.Template())
	comment := rewriteCommentBlock(t, front)

	if len(comment) == 0 {
		t.Fatalf("雛形の %s の行に説明のコメントがありません。"+
			"雛形は人間が読んで手で直すファイルなので、説明が無いと何を書けばよいか分かりません",
			rewriteKeyLinePrefix)
	}

	// 1行目は「何のための設定か」でなければならない。
	// 「どう動くか」から始めると、読み終えるまで自分に要るのかが判断できない。
	if !strings.Contains(comment[0], "ための設定") {
		t.Errorf("%s の説明の1行目が、何のための設定かを名乗っていません。"+
			"仕組みの説明から始めると、読んでも要否を判断できません\n  その行: %q",
			rewriteKeyLinePrefix, comment[0])
	}

	// 要否は「カンバンの自動化を有効にしているか」だけで決まる。見る場所と、
	// 要らないときにどうするかの両方が無いと、読んだ人はその場で判断を終えられない。
	block := strings.Join(comment, "\n")
	for _, want := range []struct {
		needle string
		why    string
	}{
		{"Workflows", "カンバンのどこを見れば要否が決まるのかを書かないと、判断のしようがありません"},
		{"空のままでよい", "要らないときに何もしなくてよいことを書かないと、当てずっぽうで書くことになります"},
	} {
		if !strings.Contains(block, want.needle) {
			t.Errorf("%s の説明に %q がありません。%s", rewriteKeyLinePrefix, want.needle, want.why)
		}
	}
}

// rewriteCommentBlock は front matter から、書き戻しの対応表に付いた説明のコメントを取り出す。
//
// 対応表の行そのものの `#` から後ろと、それに続く「コメントだけの行」を1つの塊として返す。
// 雛形は右側にコメントを寄せて書くので、続きの行はインデントだけを持つ `#` の行になる。
//
// t: テストコンテキスト。
// front: WORKFLOW.md の front matter。
// 戻り値: コメントの中身を1行ずつ（先頭の `#` と空白を落としたもの）。
func rewriteCommentBlock(t *testing.T, front string) []string {
	t.Helper()

	lines := strings.Split(front, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), rewriteKeyLinePrefix) {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("雛形の front matter に %s の行がありません", rewriteKeyLinePrefix)
	}

	var out []string
	if _, after, ok := strings.Cut(lines[start], "#"); ok {
		out = append(out, strings.TrimSpace(after))
	}
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "#") {
			break
		}
		out = append(out, strings.TrimSpace(strings.TrimPrefix(trimmed, "#")))
	}
	return out
}
