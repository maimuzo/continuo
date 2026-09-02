// `continuo doctor` の見出し語 `プロンプトの変数` の検査である（設計 5-3c / 5-3d）。
package doctor_test

import (
	"os"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/doctor"
	"github.com/maimuzo/continuo/internal/prompt"
)

// 目的: WORKFLOW.md の本文が空でも `プロンプトの変数` が `✓` になることを確かめる。
//
// **本文が空なのは誤りではない。**固有の指示が要らない project がある。
//
// 与える情報: 本文を消した WORKFLOW.md。
// 成功条件: `プロンプトの変数` が `✓` で、内訳に WORKFLOW.md の絶対パスが出ること。
func TestDoctor_本文が空でもプロンプトの変数は通る(t *testing.T) {
	fx := newFixture(t)
	fx.WriteWorkflow(t, "")
	setBodyTo(t, fx.WorkflowPath, "")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelPromptVariables, doctor.SymbolOK)
	if !strings.Contains(res.Detail, fx.WorkflowPath) {
		t.Errorf("内訳がどのファイルの話かを言っていません: %q", res.Detail)
	}
}

// 目的: `continuo init` が置いた雛形の本文のままでも `✓` になることを確かめる（設計 5-3d）。
//
// **`continuo init` の直後に doctor が `✗` を出したら、
// 利用者は自分が何かを壊したのだと思う。**
//
// 与える情報: 雛形どおりの WORKFLOW.md（本文の雛形が入っている）。
// 成功条件: `プロンプトの変数` が `✓` であること。
func TestDoctor_雛形の本文のままでもプロンプトの変数は通る(t *testing.T) {
	fx := newFixture(t)
	fx.WriteWorkflow(t, "")

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelPromptVariables, doctor.SymbolOK)
}

// 目的: 本文に知らない変数を書いたら `✗` になることを確かめる（設計 5-3c）。
//
// **`✗` にする。**この誤りがあると **issue が1件も着手できない。**
// `未記入の項目` と違い、既定値で代わりが利かない。
//
// 与える情報: `{{.issue.nope}}` を本文に書いた WORKFLOW.md。
// 成功条件: `プロンプトの変数` が `✗` で、内訳が本文の断片を名指しし、
// 直し方が `continuo prompt --show` を案内し、終了コードが 1 になること。
func TestDoctor_本文の知らない変数は足りないと出る(t *testing.T) {
	fx := newFixture(t)
	fx.WriteWorkflow(t, "")
	setBodyTo(t, fx.WorkflowPath, "{{.issue.nope}}\n")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelPromptVariables, doctor.SymbolMissing)
	if !strings.Contains(res.Detail, prompt.NameWorkflowBody) {
		t.Errorf("内訳がどの断片の話かを言っていません（%s が出ていません）: %q",
			prompt.NameWorkflowBody, res.Detail)
	}
	remedies := strings.Join(res.Remedies, "\n")
	if !strings.Contains(remedies, "continuo prompt --show") {
		t.Errorf("直し方に continuo prompt --show の案内がありません: %q", remedies)
	}
	if report.ExitCode() == 0 {
		t.Error("issue が1件も着手できない誤りなのに、終了コードが 0 です")
	}
}

// 目的: 本文の `{{if}}` の閉じ忘れも `✗` になることを確かめる（設計 5-3c）。
//
// **断片ごとに解釈しているので、本文の閉じ忘れが組み込みの後半を飲み込まない。**
// 飲み込むと、誤りの行番号がどのファイルのものか分からなくなる。
//
// 与える情報: `{{if .attempt}}` を閉じていない本文。
// 成功条件: `プロンプトの変数` が `✗` で、内訳が本文の断片を名指しすること。
func TestDoctor_本文の閉じ忘れも足りないと出る(t *testing.T) {
	fx := newFixture(t)
	fx.WriteWorkflow(t, "")
	setBodyTo(t, fx.WorkflowPath, "{{if .attempt}}閉じ忘れ\n")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelPromptVariables, doctor.SymbolMissing)
	if !strings.Contains(res.Detail, prompt.NameWorkflowBody) {
		t.Errorf("内訳がどの断片の話かを言っていません（%s が出ていません）: %q",
			prompt.NameWorkflowBody, res.Detail)
	}
}

// setBodyTo は WORKFLOW.md の本文（front matter の閉じの `---` より下）を置き換える。
//
// **front matter は1文字も触らない。**設定を変えずに、送る文面だけを変えるためである。
//
// t: 呼び出し元のテスト。
// path: WORKFLOW.md の絶対パス。
// body: 置き換える本文。空文字なら本文を消す。
func setBodyTo(t *testing.T, path, body string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	lines := strings.Split(string(raw), "\n")
	seen := 0
	for i, line := range lines {
		if strings.TrimRight(line, " \t") != "---" {
			continue
		}
		seen++
		if seen != 2 {
			continue
		}
		out := strings.Join(lines[:i+1], "\n") + "\n" + body
		if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
			t.Fatalf("WORKFLOW.md を書けません: %v", err)
		}
		return
	}
	t.Fatalf("WORKFLOW.md に front matter の閉じの --- がありません（見つかった --- は %d 行）", seen)
}
