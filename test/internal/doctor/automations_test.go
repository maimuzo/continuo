package doctor_test

import (
	"os"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/doctor"
)

// 有効な自動化を並べた偽ボードを作るための名前である。
//
// **GitHub の組み込みの自動化の名前をそのまま使う。**この2つは、issue #209 の報告者の
// カンバンで実際に有効だったものである（`Pull request linked to issue` が
// `In Progress` を書いて、走行中の run を止めた）。
const (
	workflowPRLinked = "Pull request linked to issue"
	workflowPRMerged = "Pull request merged"
)

// TestDoctor_自動化が有効なのに対応表が空なら注意を出す は、
// **止まってから気づくのを、起動する前に前倒しする**ことを確かめる（issue #209）。
//
// 目的: カンバンの自動化が有効で、`tracker.automated_state_rewrite` が空のとき、
// 見出し語 `自動化` が `!` になり、**有効な自動化の名前を内訳に出す**こと。
// **`✗` にしない**ので、終了コードは 0 のままであること。
//
// **なぜこの検査が要るのか。**自動化が Status を書くと continuo はそれを
// 「知らない Status」と読んで走行中の run を止める。**止めずに続けさせる唯一の設定が
// この対応表である。**空のままだと、PR を issue へ紐づけた瞬間に run が止まるのに、
// **利用者がそれを知るのは1件止まったあとである。**
//
// 与える情報: 有効な自動化を2件持つ偽ボードと、対応表が空の既定の設定。
// 成功条件: `自動化` が `!`、内訳に2件の名前と検査していない範囲が出て、
// 直し方が3本（対応表を書く・自動化を切る・無視してよい）出て、終了コードが 0 であること。
func TestDoctor_自動化が有効なのに対応表が空なら注意を出す(t *testing.T) {
	fx := newFixture(t)
	fx.GitHub.SetWorkflows([]fakeWorkflow{
		{Name: workflowPRLinked, Enabled: true},
		{Name: workflowPRMerged, Enabled: true},
	})

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelAutomations, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "tracker.automated_state_rewrite が空です") {
		t.Fatalf("対応表が空であることが説明に出ていない: %q", res.Detail)
	}
	notes := strings.Join(res.Notes, "\n")
	for _, want := range []string{workflowPRLinked, workflowPRMerged} {
		if !strings.Contains(notes, want) {
			t.Fatalf("有効な自動化 %q が名前で内訳に出ていない: %q", want, notes)
		}
	}
	// **この検査が見ていない範囲を必ず添える。**終端の Status や引き渡しの Status を
	// 自動化が書く場合、run は止まるが対応表では救えない（設計 3-55）。
	if !strings.Contains(notes, "terminal_states") {
		t.Fatalf("この検査が見ていない範囲が内訳に出ていない: %q", notes)
	}
	remedies := strings.Join(res.Remedies, "\n")
	for _, want := range []string{"automated_state_rewrite", "Workflows", "無視して構いません"} {
		if !strings.Contains(remedies, want) {
			t.Fatalf("直し方に %q が無い: %q", want, remedies)
		}
	}
	// **`✗` にしない。**対応表が空でも continuo は起動して走る。
	if report.ExitCode() != 0 {
		t.Fatalf("注意だけなのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
	// **リクエストを増やさない。**`workflows` は起動時の検査のクエリに載せてある。
	if got := fx.GitHub.Queries(); !equalStrings(got, []string{"bootstrap", "items"}) {
		t.Fatalf("カンバンへ送ったクエリが想定と違う: %v", got)
	}
}

// TestDoctor_自動化が1つも有効でなければ対応表が空でも通る は、
// **読み飛ばされる注意を作らない**ことを確かめる。
//
// 目的: 自動化が1つも有効でなければ、Status を書かれることが無いので対応表は空でよい。
// **ここで注意を出すと、自動化を使っていない人が毎回読み飛ばす注意を1件抱える。**
// 与える情報: 自動化を切ってある偽ボードと、対応表が空の既定の設定。
// 成功条件: `自動化` が `✓` で、説明が「1つも有効ではありません」であること。
func TestDoctor_自動化が1つも有効でなければ対応表が空でも通る(t *testing.T) {
	fx := newFixture(t)
	fx.GitHub.SetWorkflows([]fakeWorkflow{
		{Name: workflowPRLinked, Enabled: false},
		{Name: workflowPRMerged, Enabled: false},
	})

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelAutomations, doctor.SymbolOK)
	if !strings.Contains(res.Detail, "1つも有効ではありません") {
		t.Fatalf("自動化が1つも有効でないことが説明に出ていない: %q", res.Detail)
	}
	if len(res.Remedies) != 0 {
		t.Fatalf("直す先が無いのに直し方を出している: %+v", res.Remedies)
	}
}

// TestDoctor_自動化が有効でも対応表が書いてあれば通る は、**注意を出してはいけない形**を固定する。
//
// 目的: 対応表が1行でも書かれていれば、利用者は書き戻しの仕組みを知っている。
// **書いてある行が正しいかは、この見出し語では見ない**（キーがカンバンの選択肢にあるかは
// 見出し語 `対応表のキー` が、綴りの紛らわしさは見出し語 `Status の名前` が受け持つ）。
// 与える情報: 有効な自動化を1件持つ偽ボードと、`Ice Box` → `Ready` の対応表。
// 成功条件: `自動化` が `✓` で、直し方が1件も無いこと。
func TestDoctor_自動化が有効でも対応表が書いてあれば通る(t *testing.T) {
	fx := newFixture(t)
	fx.GitHub.SetWorkflows([]fakeWorkflow{{Name: workflowPRLinked, Enabled: true}})
	writeRewriteKeysWorkflow(t, fx, "    \"Ice Box\": \"Ready\"\n")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelAutomations, doctor.SymbolOK)
	if len(res.Remedies) != 0 {
		t.Fatalf("対応表が書いてあるのに直し方を出している: %+v", res.Remedies)
	}
}

// TestDoctor_自動化の一覧が応答に無ければ確かめられなかったになる は、
// **確かめていないものを通さない**ことを固定する。
//
// 目的: カンバンの応答に `workflows` が入っていないことがある（GHES や、権限が足りない場合）。
// **そのとき `✓` にすると、確かめていないことを通したことになる。**
// 与える情報: `workflows` を応答から落とした偽ボード。
// 成功条件: `自動化` が `!` で、理由が「応答に入っていなかった」であること。
func TestDoctor_自動化の一覧が応答に無ければ確かめられなかったになる(t *testing.T) {
	fx := newFixture(t)
	fx.GitHub.SetWorkflows(nil)

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelAutomations, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "自動化の一覧が入っていなかった") {
		t.Fatalf("応答に入っていなかったことが理由に出ていない: %q", res.Detail)
	}
	if report.ExitCode() != 0 {
		t.Fatalf("確かめられなかっただけなのに終了コードが %d だった", report.ExitCode())
	}
}

// TestDoctor_カンバンを読めなければ自動化は確かめられなかったになる は、
// 上流が落ちたときの記号と理由を固定する。
//
// 目的: カンバンが `✗` か `!` のとき、`自動化` を `!` にし、
// **カンバンを読めなかったことを理由に出す**こと。**ここで「自動化は無い」と言ってはならない。**
// 与える情報: gh の scope から project を外したテスト用gh mock（カンバンは `!` になる）。
// 成功条件: `自動化` が `!` で、理由がカンバンを読めなかったことであること。
func TestDoctor_カンバンを読めなければ自動化は確かめられなかったになる(t *testing.T) {
	fx := newFixture(t)
	writeFakeGH(t, fx.BinDir, `github.com
  ✓ Logged in to github.com account tester (keyring)
  - Active account: true
  - Token scopes: 'gist', 'repo'
`, 0)

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolUnknown)
	res := assertSymbol(t, report, doctor.LabelAutomations, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "カンバンを読めなかったため") {
		t.Fatalf("カンバンを読めなかったことが理由に出ていない: %q", res.Detail)
	}
}

// TestDoctor_設定ファイルを読めなければ自動化は確かめられなかったになる は、
// 上流の設定ファイルが落ちたときの理由を固定する。
//
// 目的: 設定を読めていないときは、**対応表に何が書いてあるかが決まらない。**
// カンバンが落ちたときとは直す先が違うので、理由の文言も分けること。
// 与える情報: WORKFLOW.md を消した状態。
// 成功条件: `自動化` が `!` で、理由が設定ファイルを読めなかったことであること。
func TestDoctor_設定ファイルを読めなければ自動化は確かめられなかったになる(t *testing.T) {
	fx := newFixture(t)
	if err := os.Remove(fx.WorkflowPath); err != nil {
		t.Fatalf("WORKFLOW.md を消せません: %v", err)
	}

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelAutomations, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "書き戻しの対応表に何が書いてあるか分かりません") {
		t.Fatalf("設定を読めなかったことが理由に出ていない: %q", res.Detail)
	}
}
