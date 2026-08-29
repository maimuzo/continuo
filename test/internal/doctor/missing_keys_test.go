package doctor_test

import (
	"os"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/doctor"
)

// dropKeyFromWorkflow は、fixture が置いた WORKFLOW.md から設定項目を1つ落とす。
//
// **版を上げる前の利用者のファイルを作るために使う。**その項目が増えた版へ上げると、
// 利用者のファイルはちょうどこの形になる。
//
// t: 呼び出し元のテスト。
// fx: 使っている fixture。
// prefix: 落とす行の書き出し（`  automated_state_rewrite:` のような形）。
func dropKeyFromWorkflow(t *testing.T, fx *fixture, prefix string) {
	t.Helper()

	raw, err := os.ReadFile(fx.WorkflowPath)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	var out []string
	dropping, dropIndent, dropped := false, 0, false
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		if dropping {
			// **下にぶら下がる説明のコメント行も一緒に落とす。**
			if trimmed != "" && indent > dropIndent {
				continue
			}
			dropping = false
		}
		if strings.HasPrefix(line, prefix) {
			dropping, dropIndent, dropped = true, indent, true
			continue
		}
		out = append(out, line)
	}
	if !dropped {
		t.Fatalf("落とす行が見つかりません: %q", prefix)
	}
	if err := os.WriteFile(fx.WorkflowPath, []byte(strings.Join(out, "\n")), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
}

// TestDoctor_書かれていない設定項目があれば差分と当てる手順を出す は、
// **版を上げて増えた設定項目に気づく手段**を確かめる（設計 3-73。issue #85）。
//
// 目的: 雛形にあって WORKFLOW.md に無い設定項目があるとき、見出し語 `未記入の項目` が
// `!` になり、**それを足す差分**と**その差分を当てるコマンド**の2つを出すこと。
// **`✗` にしない**（書かれていなくても continuo は走る）ので、終了コードは 0 のままであること。
//
// **リリースノートでは代わりにならない。**ノートは1回きりで、読み飛ばした人にも、
// あとから来た人にも、**その項目があること自体を知る手段が無い。**
//
// 与える情報: `tracker.automated_state_rewrite`（v0.1.9 で増えた項目）を落とした WORKFLOW.md。
// 成功条件: `未記入の項目` が `!`、内訳が unified diff の形をしていて、
// 直し方に `patch` へ渡すコマンドが出て、終了コードが 0 であること。
func TestDoctor_書かれていない設定項目があれば差分と当てる手順を出す(t *testing.T) {
	fx := newFixture(t)
	dropKeyFromWorkflow(t, fx, "  automated_state_rewrite:")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelMissingKeys, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "書かれていない設定項目があります") {
		t.Fatalf("何が足りないのかが説明に出ていない: %q", res.Detail)
	}
	notes := strings.Join(res.Notes, "\n")
	// **差分そのものを出す。**キーの名前だけを並べても、何を書ける項目なのかが分からない。
	if !strings.Contains(notes, "@@ ") || !strings.Contains(notes, "+  automated_state_rewrite:") {
		t.Fatalf("足す差分が内訳に出ていない: %q", notes)
	}
	if !strings.Contains(notes, "自動化が書く Status 名") {
		t.Fatalf("差分に雛形の説明のコメントが入っていない: %q", notes)
	}
	remedies := strings.Join(res.Remedies, "\n")
	if !strings.Contains(remedies, "--missing-keys-patch") || !strings.Contains(remedies, "patch -p0") {
		t.Fatalf("差分を当てるコマンドが出ていない: %q", remedies)
	}
	if !strings.Contains(remedies, fx.WorkflowPath) {
		t.Fatalf("どのファイルに当てるのかがコマンドに出ていない: %q", remedies)
	}
	// **`✗` にしない。**版を上げた瞬間に、いま動いている人の起動が止まってはならない。
	if report.ExitCode() != 0 {
		t.Fatalf("注意だけなのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
	// **ボードを1バイトも読まない。**この検査のためにリクエストを増やさない。
	if got := fx.GitHub.Queries(); !equalStrings(got, []string{"bootstrap", "items"}) {
		t.Fatalf("ボードへ送ったクエリが想定と違う: %v", got)
	}
}

// TestDoctor_値を書き換えただけなら未記入の項目にはならない は、
// **誤って毎回注意を出さない**ことを確かめる。
//
// 目的: 雛形の値を自分の環境に合わせて書き換えただけの WORKFLOW.md で、
// 見出し語 `未記入の項目` が `✓` になること。**見ているのは値ではなくキーである。**
// 与える情報: fixture が置いた WORKFLOW.md（雛形から作り、値だけを差し替えてある）。
// 成功条件: `未記入の項目` が `✓` で、説明に雛形の項目数が出ること。
func TestDoctor_値を書き換えただけなら未記入の項目にはならない(t *testing.T) {
	fx := newFixture(t)

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelMissingKeys, doctor.SymbolOK)
	if !strings.Contains(res.Detail, "すべて書かれています") {
		t.Fatalf("全部書かれていることが説明に出ていない: %q", res.Detail)
	}
	if len(res.Notes) != 0 {
		t.Fatalf("足りないものが無いのに内訳を出している: %v", res.Notes)
	}
}

// TestDoctor_節ごと書かれていなければ節ごと足す差分を出す は、
// **内訳が水増しされない**ことを確かめる。
//
// 目的: `restart` の節そのものが無いとき、`restart` と `restart.orphan_running_action` を
// 2件として数えず、**節ごと足す差分**を1つ出すこと。直す手は1つしかない。
// 与える情報: `restart:` の節を落とした WORKFLOW.md。
// 成功条件: `未記入の項目` が `!` で、説明が1件と出て、差分が節ごと足していること。
func TestDoctor_節ごと書かれていなければ節ごと足す差分を出す(t *testing.T) {
	fx := newFixture(t)
	dropKeyFromWorkflow(t, fx, "restart:")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelMissingKeys, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "（1件") {
		t.Fatalf("親と子を別々に数えている: %q", res.Detail)
	}
	notes := strings.Join(res.Notes, "\n")
	if !strings.Contains(notes, "+restart:") || !strings.Contains(notes, "+  orphan_running_action:") {
		t.Fatalf("差分が節ごと足していない: %q", notes)
	}
}
