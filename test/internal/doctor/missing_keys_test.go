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

// TestDoctor_書かれていない設定項目があれば名前と差分の出し方を出す は、
// **版を上げて増えた設定項目に気づく手段**を確かめる（設計 3-75。issue #85）。
//
// 目的: 雛形にあって WORKFLOW.md に無い設定項目があるとき、見出し語 `未記入の項目` が
// `!` になり、**足りない項目の名前**と**差分を読むコマンド・当てるコマンド**を出すこと。
// **`✗` にしない**（書かれていなくても continuo は走る）ので、終了コードは 0 のままであること。
//
// **リリースノートでは代わりにならない。**ノートは1回きりで、読み飛ばした人にも、
// あとから来た人にも、**その項目があること自体を知る手段が無い。**
//
// 与える情報: `tracker.automated_state_rewrite`（v0.1.9 で増えた項目）を落とした WORKFLOW.md。
// 成功条件: `未記入の項目` が `!`、内訳にその項目の名前が出て、
// 直し方に差分を読むコマンドと `patch` へ渡すコマンドが出て、終了コードが 0 であること。
func TestDoctor_書かれていない設定項目があれば名前と差分の出し方を出す(t *testing.T) {
	fx := newFixture(t)
	dropKeyFromWorkflow(t, fx, "  automated_state_rewrite:")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelMissingKeys, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "書かれていない設定項目があります") {
		t.Fatalf("何が足りないのかが説明に出ていない: %q", res.Detail)
	}
	notes := strings.Join(res.Notes, "\n")
	if notes != "tracker.automated_state_rewrite" {
		t.Fatalf("足りない項目の名前が内訳に出ていない: %q", notes)
	}
	// **差分そのものは内訳に入れない。**30行から156行になり、他の検査結果を画面の外へ押し出す。
	if strings.Contains(notes, "@@ ") {
		t.Fatalf("内訳に差分が入っている: %q", notes)
	}
	remedies := strings.Join(res.Remedies, "\n")
	if !strings.Contains(remedies, "--missing-keys-patch") || !strings.Contains(remedies, "patch -p0") {
		t.Fatalf("差分を当てるコマンドが出ていない: %q", remedies)
	}
	// **読むコマンドと当てるコマンドを分けて出す。**当てる前に読ませるためである。
	if len(res.Remedies) != 2 {
		t.Fatalf("差分を読むコマンドと当てるコマンドが揃っていない: %q", res.Remedies)
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
// 2件として数えず、**節の名前1つ**として挙げること。直す手は1つしかない。
// 与える情報: `restart:` の節を落とした WORKFLOW.md。
// 成功条件: `未記入の項目` が `!` で、説明が1件と出て、内訳が `restart` の1行だけであること。
func TestDoctor_節ごと書かれていなければ節の名前を1件だけ挙げる(t *testing.T) {
	fx := newFixture(t)
	dropKeyFromWorkflow(t, fx, "restart:")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelMissingKeys, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "（1件") {
		t.Fatalf("親と子を別々に数えている: %q", res.Detail)
	}
	if len(res.Notes) != 1 || res.Notes[0] != "restart" {
		t.Fatalf("節の名前を1件だけ挙げていない: %v", res.Notes)
	}
}

// TestDoctor_足りない項目が多くても内訳は上限で切る は、
// **1つの検査が検査結果の並びを画面の外へ押し出さない**ことを固定する。
//
// 目的: 足りない項目が上限（10件）を超えたとき、内訳を上限＋まとめの1行までに切ること。
// **差分をそのまま並べると156行になり、他の14個の検査結果が読めなくなる。**
// 与える情報: 節を12個落とした WORKFLOW.md（`continuo init` を使わずに手で書いた人の形）。
// 成功条件: 内訳が11行以内で、最後の行が残りの件数を伝えていること。
func TestDoctor_足りない項目が多くても内訳は上限で切る(t *testing.T) {
	fx := newFixture(t)
	for _, prefix := range []string{
		"polling:", "workspace_hooks:", "agent:", "naming:", "cleanup:",
		"rate_limit:", "trust:", "restart:", "runtime:", "server:", "language:",
		"  status_signal_prefix:",
	} {
		dropKeyFromWorkflow(t, fx, prefix)
	}

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelMissingKeys, doctor.SymbolUnknown)
	if len(res.Notes) != 11 {
		t.Fatalf("内訳が上限で切られていない（%d行）: %v", len(res.Notes), res.Notes)
	}
	if !strings.Contains(res.Notes[10], "ほか") {
		t.Fatalf("残りの件数を伝えていない: %q", res.Notes[10])
	}
	for _, note := range res.Notes {
		if strings.HasPrefix(note, "+") || strings.HasPrefix(note, "@@") {
			t.Fatalf("内訳に差分が入っている: %v", res.Notes)
		}
	}
}
