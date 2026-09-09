package doctor_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/doctor"
)

// TestReport_出力の形と終了コードが設計どおりである は、報告の見た目を固定する。
//
// 目的: 記号・見出し語・説明が1行に並び、内訳と直し方が説明の桁位置に揃って続き、
// 末尾に集計の行が出ること。**`✗` があれば終了コードは 1** であること（設計 3-32）。
// 与える情報: `✓` / `✗` / `!` を1件ずつ持つ検査結果。
// 成功条件: 出力の各行が期待どおりで、直し方の行が `→ ` で始まり、
// 集計が「2件に問題があります（✗ 1件 / ! 1件）」であること。
func TestReport_出力の形と終了コードが設計どおりである(t *testing.T) {
	report := doctor.Report{Results: []doctor.Result{
		{Label: doctor.LabelHerdr, Symbol: doctor.SymbolOK, Detail: "protocol 19（設定と一致）"},
		{
			Label:    doctor.LabelClone,
			Symbol:   doctor.SymbolMissing,
			Detail:   "対象 1件のうち 1件が見つかりません",
			Notes:    []string{"octocat/hello-world が見つからない"},
			Remedies: []string{"ghq get octocat/hello-world を実行してください"},
		},
		{
			Label:    doctor.LabelCredentials,
			Symbol:   doctor.SymbolUnknown,
			Detail:   "資格情報のファイルがありません（macOS では Keychain に入っています）",
			Remedies: []string{"判定を飛ばしました。continuo の起動には影響しません"},
		},
	}}

	var b strings.Builder
	if err := report.Write(&b); err != nil {
		t.Fatalf("検査結果を書き出せません: %v", err)
	}
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")

	if len(lines) != 8 {
		t.Fatalf("出力の行数が想定と違う（%d行）:\n%s", len(lines), b.String())
	}
	if !strings.HasPrefix(lines[0], "✓ "+doctor.LabelText(doctor.LabelHerdr)+" ") ||
		!strings.HasSuffix(lines[0], "protocol 19（設定と一致）") {
		t.Fatalf("1行目が「記号 見出し語 説明」になっていない: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "✗ "+doctor.LabelText(doctor.LabelClone)+" ") {
		t.Fatalf("2行目が ✗ の行になっていない: %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "  ") || !strings.Contains(lines[2], "octocat/hello-world が見つからない") {
		t.Fatalf("内訳の行が説明の桁位置に揃っていない: %q", lines[2])
	}
	if !strings.Contains(lines[3], "→ ghq get octocat/hello-world を実行してください") {
		t.Fatalf("直し方の行が `→ ` で続いていない: %q", lines[3])
	}
	if !strings.HasPrefix(lines[4], "! "+doctor.LabelText(doctor.LabelCredentials)+" ") {
		t.Fatalf("5行目が ! の行になっていない: %q", lines[4])
	}
	if lines[6] != "" {
		t.Fatalf("集計の前に空行が無い: %q", lines[6])
	}
	if lines[7] != "2件に問題があります（✗ 1件 / ! 1件）" {
		t.Fatalf("集計の行が想定と違う: %q", lines[7])
	}
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった", report.ExitCode())
	}
}

// TestReport_確かめられなかっただけなら終了コードは0で問題ありと書かない は、
// 3値の重みと集計の文言を固定する。
//
// 目的: `!` だけのときは終了コードを 0 にし（動くかもしれないので手を止めない）、
// **集計の行を「問題があります」と書かない**こと。カンバンが空なだけのときもここへ来るので
// （設計 3-32）、最後の1行だけを読んで「問題がある」と読めてはならない。
// 与える情報: `✓` 1件と `!` 1件だけの検査結果。
// 成功条件: 終了コードが 0 で、集計が「1件は確かめられなかったか、注意が要ります
// （✗ 0件 / ! 1件）。足りないものはありません」であること。
//
// **`!` は「確かめられなかった」だけではない。**見出し語 `Status の名前` は、
// 確かめたうえで「紛らわしいので取り違えやすい」と言うために `!` を使う。
// **集計の行が「確かめられませんでした」だけを言うと、その行と食い違う。**
func TestReport_確かめられなかっただけなら終了コードは0で問題ありと書かない(t *testing.T) {
	report := doctor.Report{Results: []doctor.Result{
		{Label: doctor.LabelConfig, Symbol: doctor.SymbolOK, Detail: "読めました"},
		{Label: doctor.LabelClone, Symbol: doctor.SymbolUnknown, Detail: "検査する対象がありません"},
	}}

	if report.ExitCode() != 0 {
		t.Fatalf("! だけなのに終了コードが %d だった", report.ExitCode())
	}
	var b strings.Builder
	if err := report.Write(&b); err != nil {
		t.Fatalf("検査結果を書き出せません: %v", err)
	}
	if !strings.Contains(b.String(), "1件は確かめられなかったか、注意が要ります（✗ 0件 / ! 1件）。足りないものはありません") {
		t.Fatalf("集計の行が想定と違う:\n%s", b.String())
	}
	if strings.Contains(b.String(), "問題があります") {
		t.Fatalf("✗ が0件なのに「問題があります」と出ている:\n%s", b.String())
	}
}

// TestReport_すべて通ったときは問題なしと出る は、成功時の集計を固定する。
//
// 目的: `✗` も `!` も無いときに、件数つきで「揃っている」と出すこと。
// 与える情報: `✓` だけの検査結果2件。
// 成功条件: 集計の行が「前提はすべて揃っています（✓ 2件）」であること。
func TestReport_すべて通ったときは問題なしと出る(t *testing.T) {
	report := doctor.Report{Results: []doctor.Result{
		{Label: doctor.LabelConfig, Symbol: doctor.SymbolOK, Detail: "読めました"},
		{Label: doctor.LabelHerdr, Symbol: doctor.SymbolOK, Detail: "protocol 19（設定と一致）"},
	}}

	var b strings.Builder
	if err := report.Write(&b); err != nil {
		t.Fatalf("検査結果を書き出せません: %v", err)
	}
	if !strings.Contains(b.String(), "前提はすべて揃っています（✓ 2件）") {
		t.Fatalf("集計の行が想定と違う:\n%s", b.String())
	}
}
