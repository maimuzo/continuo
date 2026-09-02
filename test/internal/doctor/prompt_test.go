// `continuo doctor` が足した2つの見出し語の検査である
// （`プロンプトの変数` と `残った本文`。設計 5-3c / 5-3d）。
package doctor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/doctor"
	"github.com/maimuzo/continuo/internal/prompt"
)

// 目的: 固有のプロンプトが無くても `プロンプトの変数` が `✓` になることを確かめる。
//
// **無いことは誤りではない。**固有の指示が要らない project がある。
//
// 与える情報: `continuo init` の雛形どおりの WORKFLOW.md。
// 成功条件: `プロンプトの変数` が `✓` で、内訳に置き場所が出ること。
func TestDoctor_固有のプロンプトが無くてもプロンプトの変数は通る(t *testing.T) {
	fx := newFixture(t)
	fx.WriteWorkflow(t, "")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelPromptVariables, doctor.SymbolOK)
	if !strings.Contains(res.Detail, prompt.ProjectFileName) {
		t.Errorf("内訳に固有のプロンプトの置き場所が出ていません: %q", res.Detail)
	}
}

// 目的: 固有のプロンプトに知らない変数を書いたら `✗` になることを確かめる（設計 5-3c）。
//
// **`✗` にする。**この誤りがあると **issue が1件も着手できない。**
// `未記入の項目` と違い、既定値で代わりが利かない。
//
// 与える情報: `{{.issue.nope}}` を書いた PROJECT_SPECIFIC_PROMPT.md。
// 成功条件: `プロンプトの変数` が `✗` で、内訳がファイルの名前を名指しし、
// 直し方が `continuo prompt --show` を案内し、終了コードが 1 になること。
func TestDoctor_固有のプロンプトの知らない変数は足りないと出る(t *testing.T) {
	fx := newFixture(t)
	fx.WriteWorkflow(t, "")
	writeProjectPromptFor(t, fx, "{{.issue.nope}}\n")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelPromptVariables, doctor.SymbolMissing)
	if !strings.Contains(res.Detail, prompt.ProjectFileName) {
		t.Errorf("内訳がどのファイルの話かを言っていません: %q", res.Detail)
	}
	remedies := strings.Join(res.Remedies, "\n")
	if !strings.Contains(remedies, "continuo prompt --show") {
		t.Errorf("直し方に continuo prompt --show の案内がありません: %q", remedies)
	}
	if report.ExitCode() == 0 {
		t.Error("issue が1件も着手できない誤りなのに、終了コードが 0 です")
	}
}

// 目的: 固有のプロンプトが在るのに読めないとき、doctor がそこだけを `✗` にして
// 残りの検査を続けることを確かめる（設計 5-3c）。
//
// **`config.Load` を落としてはならない。**落とすと設定の検査が `✗` になり、
// ほぼ全部の検査の記号がそれに引きずられて、**原因を調べる道具そのものが使えなくなる。**
//
// 与える情報: PROJECT_SPECIFIC_PROMPT.md という名前のディレクトリ。
// 成功条件: `設定ファイル` が `✓` のまま、`プロンプトの変数` だけが `✗` になること。
func TestDoctor_固有のプロンプトが読めなくても設定の検査は通る(t *testing.T) {
	fx := newFixture(t)
	fx.WriteWorkflow(t, "")
	blocked := filepath.Join(filepath.Dir(fx.WorkflowPath), prompt.ProjectFileName)
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatalf("ディレクトリを作れません: %v", err)
	}

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelConfig, doctor.SymbolOK)
	res := assertSymbol(t, report, doctor.LabelPromptVariables, doctor.SymbolMissing)
	if !strings.Contains(res.Detail, prompt.ProjectFileName) {
		t.Errorf("内訳がどのファイルの話かを言っていません: %q", res.Detail)
	}
}

// 目的: 本文が残っていない WORKFLOW.md で `残った本文` が `✓` になることを確かめる。
//
// 与える情報: `continuo init` の雛形どおりの WORKFLOW.md。
// 成功条件: `残った本文` が `✓` であること。
func TestDoctor_本文が残っていなければ残った本文は通る(t *testing.T) {
	fx := newFixture(t)
	fx.WriteWorkflow(t, "")

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelLeftoverBody, doctor.SymbolOK)
}

// 目的: 本文が残っていたら `残った本文` が `!` になり、移行の手順を出すことを確かめる
// （設計 5-3d）。
//
// **`✗` にしない。**残っていても continuo は動く。いままでと同じ文面が送られるだけである。
// **黙って通してもいけない。**残っている限り、組み込みの直しはこの利用者に届かない。
//
// 与える情報: 本文を3行書き足した WORKFLOW.md。
// 成功条件: `残った本文` が `!`、内訳に行数が出て、直し方が
// `continuo prompt --show --builtin` と移し先を示し、終了コードが 0 のままであること。
func TestDoctor_本文が残っていれば注意を出す(t *testing.T) {
	fx := newFixture(t)
	fx.WriteWorkflow(t, "")
	appendBodyTo(t, fx.WorkflowPath, "\n1行目\n2行目\n3行目\n")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelLeftoverBody, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "3") {
		t.Errorf("内訳に本文の行数が出ていません: %q", res.Detail)
	}
	notes := strings.Join(res.Notes, "\n")
	if notes == "" {
		t.Error("組み込みが送られないことが内訳に出ていません")
	}
	remedies := strings.Join(res.Remedies, "\n")
	for _, want := range []string{"continuo prompt --show --builtin", prompt.ProjectFileName} {
		if !strings.Contains(remedies, want) {
			t.Errorf("直し方に %q がありません: %q", want, remedies)
		}
	}
	// **`✗` にしない。**残っていても continuo は起動して走る。
	if report.ExitCode() != 0 {
		t.Errorf("注意だけなのに終了コードが %d です", report.ExitCode())
	}
}

// writeProjectPromptFor は fixture のディレクトリに PROJECT_SPECIFIC_PROMPT.md を置く。
//
// t: 呼び出し元のテスト。
// fx: 使っている fixture。
// body: 書く中身。
func writeProjectPromptFor(t *testing.T, fx *fixture, body string) {
	t.Helper()
	path := filepath.Join(filepath.Dir(fx.WorkflowPath), prompt.ProjectFileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("%s を書けません: %v", prompt.ProjectFileName, err)
	}
}

// appendBodyTo は WORKFLOW.md の末尾へ本文を書き足す（互換の経路を作る）。
//
// t: 呼び出し元のテスト。
// path: WORKFLOW.md の絶対パス。
// body: 書き足す本文。
func appendBodyTo(t *testing.T, path, body string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	if err := os.WriteFile(path, append(raw, []byte(body)...), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
}
