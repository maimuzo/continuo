// **雛形が示す「例」の行を、行まるごとの完全一致で押さえる。**
//
// **実際に配られた**（issue #81）。`continuo init` が書き出す `WORKFLOW.md` の
// `owner` と `project_number` の行に作者の GitHub アカウント名が例として書いてあり、
// **利用者が自分の手元に作る設定ファイルへ、そのまま焼き込まれた。**
//
// 既にあった検査では止まらなかった。
// `design_template_test.go` は設計文書とキーのパスの集合だけを突き合わせ、コメントの本文を見ない。
// `scaffold_test.go` は `# ここを埋めること` の部分一致だけを見る。
// `detect_test.go` は値を埋めたあとの行しか見ない。
// **未記入の雛形に何と書いてあるかを見るものが、1つも無かった。**
package scaffold_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/scaffold"
)

// templateExampleLines は、雛形の中で利用者へ「例」を示している行である。
//
// **架空の名前だけを使う。**GitHub が説明用に用意している `octocat` と `hello-world` を充てる。
// 実在のアカウント名・リポジトリ名をここへ書かない。
//
// **桁揃えも含めて固定する。**`continuo init` はこの行を先頭から一致させて値を埋めるので
// （internal/scaffold/fill.go の ownerPlaceholderCode / projectPlaceholderCode）、
// 桁が動くと埋められなくなる。
var templateExampleLines = []string{
	"    owner: __FILL_ME__                      # ここを埋めること。例: https://github.com/octocat なら octocat",
	"    project_number: 0                       # ここを埋めること。例: https://github.com/users/octocat/projects/3 なら 3",
}

// TestTemplate_雛形の例の行が架空の名前のまま固定されている は、issue #81 の形を押さえる。
//
// 目的: 未記入の雛形の「例」の行が、1文字も変わっていないこと。
// 与える情報: `scaffold.Template()` の全文。
// 成功条件: templateExampleLines の各行が、そのままの形で雛形の中にあること。
func TestTemplate_雛形の例の行が架空の名前のまま固定されている(t *testing.T) {
	lines := strings.Split(scaffold.Template(), "\n")

	for _, want := range templateExampleLines {
		key := strings.TrimSpace(strings.SplitN(strings.TrimSpace(want), ":", 2)[0])
		found := false
		for _, got := range lines {
			if got == want {
				found = true
				break
			}
		}
		if found {
			continue
		}
		got, ok := lineWithKey(scaffold.Template(), key)
		if !ok {
			t.Errorf("雛形に %s の行がありません。\n  want: %q", key, want)
			continue
		}
		t.Errorf("雛形の %s の行が変わっています。\n  want: %q\n  got:  %q\n"+
			"  **この行は利用者の手元の WORKFLOW.md にそのまま残ります**（issue #81）。\n"+
			"  例に使う名前は octocat / hello-world のままにし、桁揃えも動かさないでください。",
			key, want, got)
	}
}
