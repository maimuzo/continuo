// `continuo init` が書き出す WORKFLOW.md の中身の検査である。
//
// **雛形は人間が読んで手で直すファイルである。**行の右側のコメントが崩れたり、
// 値が入るべき行がプレースホルダのまま残ったりすると、何を書けばよいか分からなくなる。
package scaffold_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/scaffold"
)

// lineWithKey は、出力の中からそのキーの行を1つ返す。
//
// out: WORKFLOW.md の全文。
// key: 探すキー（`owner` など）。
// 戻り値の1つ目: 見つかった行。
// 戻り値の2つ目: 見つかれば true。
func lineWithKey(out, key string) (string, bool) {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), key+":") {
			return l, true
		}
	}
	return "", false
}

// TestTemplateWithValues_ownerとボードの番号を埋める は、値が入ることを確かめる。
//
// 目的: 渡した owner とボードの番号が、対応する行に書かれること。
// 与える情報: 両方が埋まった Values。
// 成功条件: それぞれの行に値が入り、プレースホルダが残っていないこと。
func TestTemplateWithValues_ownerとボードの番号を埋める(t *testing.T) {
	out := scaffold.TemplateWithValues(scaffold.Values{Owner: "octocat", ProjectNumber: 7})

	line, ok := lineWithKey(out, "owner")
	if !ok || !strings.Contains(line, "octocat") {
		t.Errorf("owner が入っていない: %q", line)
	}
	line, ok = lineWithKey(out, "project_number")
	if !ok || !strings.Contains(line, "7") {
		t.Errorf("ボードの番号が入っていない: %q", line)
	}
	if strings.Contains(out, "ここを埋めること") {
		t.Error("値を渡したのにプレースホルダの案内が残っている")
	}
}

// TestTemplateWithValues_値が無ければプレースホルダのまま残す は、埋まらない場合を確かめる。
//
// **`continuo init` は `gh` から値を引けないことがある。**
// **勝手に空文字や 0 を書き込むと、人間はそれが「決まった値」だと思う。**
// プレースホルダのまま残し、案内を読ませる。
//
// 目的: 値を渡さなければ、その行をプレースホルダのまま残すこと。
// 与える情報: 空の Values。
// 成功条件: 「ここを埋めること」の案内が残ること。
func TestTemplateWithValues_値が無ければプレースホルダのまま残す(t *testing.T) {
	out := scaffold.TemplateWithValues(scaffold.Values{})
	if !strings.Contains(out, "ここを埋めること") {
		t.Error("値が無いのにプレースホルダの案内が消えている")
	}
}

// TestTemplateWithValues_ownerの形が不正なら書き込まない は、YAML を壊さないことを確かめる。
//
// **引用符や改行が混ざった値をそのまま書き出すと、YAML として読めなくなる。**
// **読めない設定ファイルより、埋まっていない設定ファイルのほうが直しやすい。**
//
// 目的: `ValidOwner` を通らない値を書き込まないこと。
// 与える情報: 空白・引用符・改行を含む owner。
// 成功条件: どれも書き込まれず、プレースホルダが残ること。
func TestTemplateWithValues_ownerの形が不正なら書き込まない(t *testing.T) {
	for _, owner := range []string{"has space", `qu"ote`, "line\nbreak", "a/b"} {
		t.Run(strings.ReplaceAll(owner, "\n", "\\n"), func(t *testing.T) {
			out := scaffold.TemplateWithValues(scaffold.Values{Owner: owner})
			if strings.Contains(out, owner) {
				t.Errorf("不正な owner を書き込んでいる: %q", owner)
			}
		})
	}
}

// TestTemplateWithValues_ボードの番号が0以下なら書き込まない は、番号の検査を確かめる。
//
// 目的: 0 と負の数を書き込まないこと（ボードの番号は1以上である）。
// 与える情報: 0 と -1。
// 成功条件: プレースホルダが残ること。
func TestTemplateWithValues_ボードの番号が0以下なら書き込まない(t *testing.T) {
	for _, n := range []int{0, -1} {
		out := scaffold.TemplateWithValues(scaffold.Values{ProjectNumber: n})
		line, ok := lineWithKey(out, "project_number")
		if !ok {
			t.Fatal("project_number の行が無い")
		}
		if !strings.Contains(line, "ここを埋めること") {
			t.Errorf("番号 %d を書き込んでいる: %q", n, line)
		}
	}
}
