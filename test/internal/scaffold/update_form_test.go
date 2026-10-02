// **書き方が違う WORKFLOW.md を `continuo setup` に渡したときの検査である。**
//
// **`continuo setup` は行を1本ずつ組み立て直す。**だから、値が下の行にぶら下がっていたり、
// 改行が CRLF だったりすると、雛形のままのファイルとは違う結果になる。
// **どちらも「成功しました」と出したまま壊してはならない。**
package scaffold_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// templateFor は、owner とカンバンの番号を埋めた雛形の全文を返す。
//
// t: 呼び出し元のテスト。
// 戻り値: WORKFLOW.md の全文。
func templateFor(t *testing.T) string {
	t.Helper()
	return scaffold.TemplateWithValues(scaffold.Values{Owner: "octocat", ProjectNumber: 3})
}

// blockFormWorkflow は、`active_states` を block 形式で書いた WORKFLOW.md の全文を返す。
//
// t: 呼び出し元のテスト。
// 戻り値: WORKFLOW.md の全文（front matter は YAML として読める形である）。
func blockFormWorkflow(t *testing.T) string {
	t.Helper()
	out := strings.Replace(templateFor(t),
		`  active_states: ["Ready", "In Progress"]`,
		"  active_states:\n    - \"Ready\"\n    - \"In Progress\"", 1)
	if !strings.Contains(out, "\n    - \"Ready\"") {
		t.Fatalf("検査用のファイルを組み立てられなかった:\n%s", frontMatterPart(out))
	}
	if err := config.CheckFrontMatterSyntax(out); err != nil {
		t.Fatalf("検査用のファイルが元から読めない: %v", err)
	}
	return out
}

// readWorkflow は書き換えたあとの WORKFLOW.md を読む。
//
// t: 呼び出し元のテスト。
// dir: WORKFLOW.md があるディレクトリ。
// 戻り値: 全文。
func readWorkflow(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "WORKFLOW.md"))
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	return string(raw)
}

// frontMatterPart は、失敗したときに出す front matter の部分を切り出す。
//
// s: WORKFLOW.md の全文。
// 戻り値: front matter（切り出せなければ全文）。
func frontMatterPart(s string) string {
	parts := strings.SplitN(s, "---", 3)
	if len(parts) < 3 {
		return s
	}
	return parts[1]
}
