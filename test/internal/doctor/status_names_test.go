package doctor_test

import (
	"os"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/doctor"
)

// TestDoctor_ボードに紛らわしいStatusが並んでいれば注意を出す は、
// **Bootstrap を通ってしまう取り違えを捕まえる**ことを確かめる。
//
// 目的: ボードに `In Progress` と `AI In Progress` が並んでいるとき、
// 見出し語 `Status の名前` が `!` になり、どの設定キーのどの名前と、
// ボードのどの選択肢が紛らわしいのかを内訳に出すこと。
// **`✗` にしない**（continuo は動く）ので、終了コードは 0 のままであること。
//
// 与える情報: 既定の設定（`active_states` は `Ready` と `In Progress`）と、
// `AI In Progress` を足したボード。
// 成功条件: `Status の名前` が `!`、内訳に両方の名前が出て、
// 直し方に `active_states` の副作用が出て、終了コードが 0 であること。
func TestDoctor_ボードに紛らわしいStatusが並んでいれば注意を出す(t *testing.T) {
	fx := newFixture(t)
	fx.GitHub.SetStatusOptions("Ice Box", "Ready", "AI In Progress", "In Progress", "Blocked", "In Review", "Done")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelStatusNames, doctor.SymbolUnknown)
	notes := strings.Join(res.Notes, "\n")
	if !strings.Contains(notes, "tracker.active_states") {
		t.Fatalf("どの設定キーに書いた名前かが内訳に出ていない: %q", notes)
	}
	if !strings.Contains(notes, `"In Progress"`) || !strings.Contains(notes, `"AI In Progress"`) {
		t.Fatalf("紛らわしい組の両方が内訳に出ていない: %q", notes)
	}
	remedies := strings.Join(res.Remedies, "\n")
	if !strings.Contains(remedies, "誰が置いた issue かを問わず着手の対象になります") {
		t.Fatalf("active_states に足したときの副作用が直し方に出ていない: %q", remedies)
	}
	if !strings.Contains(remedies, "cleanup.on_states") {
		t.Fatalf("片付ける Status との取り違えが直し方に出ていない: %q", remedies)
	}
	// **`✗` にしない。**紛らわしいだけでは continuo は動く。
	if report.ExitCode() != 0 {
		t.Fatalf("注意だけなのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
	// **ボードを読んだときの応答を使い回す。**この検査のためにリクエストを増やさない。
	if got := fx.GitHub.Queries(); !equalStrings(got, []string{"bootstrap", "items"}) {
		t.Fatalf("ボードへ送ったクエリが想定と違う: %v", got)
	}
}

// TestDoctor_区切りと大文字小文字だけが違うStatusも注意を出す は、
// **見た目が同じ選択肢**を捕まえることを確かめる。
//
// 目的: ボードに `In Progress` と `InProgress` が並んでいるとき、
// 見出し語 `Status の名前` が `!` になり、理由が「同じ綴り」であること。
// 与える情報: `InProgress` を足したボード。
// 成功条件: `Status の名前` が `!` で、内訳の理由が「大文字小文字と空白・記号を無視すると同じ綴り」であること。
func TestDoctor_区切りと大文字小文字だけが違うStatusも注意を出す(t *testing.T) {
	fx := newFixture(t)
	fx.GitHub.SetStatusOptions("Ice Box", "Ready", "In Progress", "InProgress", "Blocked", "In Review", "Done")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelStatusNames, doctor.SymbolUnknown)
	notes := strings.Join(res.Notes, "\n")
	if !strings.Contains(notes, "大文字小文字と空白・記号を無視すると同じ綴り") {
		t.Fatalf("同じ綴りであることが理由に出ていない: %q", notes)
	}
	if !strings.Contains(notes, `"InProgress"`) {
		t.Fatalf("ボード側の選択肢名が内訳に出ていない: %q", notes)
	}
}

// TestDoctor_語の途中でたまたま一致するStatusは注意を出さない は、
// **警告そのものが読まれなくなる形**を弾く。
//
// 目的: `Abandoned` は文字の並びとして `Done` を含む（a-b-a-n-**d-o-n-e**-d）が、
// **語としては別物である。**ボードに `Done` と `Abandoned` を並べるのはごく普通なので、
// ここを警告してはならない。
// 与える情報: `Abandoned` と `Ready for Review` を足したボード（`terminal_states` は `Done`）。
// 成功条件: `Status の名前` が `✓` であること。
func TestDoctor_語の途中でたまたま一致するStatusは注意を出さない(t *testing.T) {
	fx := newFixture(t)
	fx.GitHub.SetStatusOptions("Ice Box", "Ready", "In Progress", "Blocked", "In Review", "Done", "Abandoned")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelStatusNames, doctor.SymbolOK)
	if !strings.Contains(res.Detail, "紛らわしい選択肢はありません") {
		t.Fatalf("紛らわしい組が無いことが説明に出ていない: %q", res.Detail)
	}
}

// TestDoctor_ボードを読めなければStatusの名前は確かめられなかったになる は、
// 上流が落ちたときの記号と理由を固定する。
//
// 目的: ボードが `✗` か `!` のとき、`Status の名前` を `!` にし、
// **ボードを読めなかったことを理由に出す**こと（照合する選択肢を持っていないため）。
// 与える情報: gh の scope から project を外したテスト用gh mock（ボードは `!` になる）。
// 成功条件: `Status の名前` が `!` で、理由がボードを読めなかったことであること。
func TestDoctor_ボードを読めなければStatusの名前は確かめられなかったになる(t *testing.T) {
	fx := newFixture(t)
	writeFakeGH(t, fx.BinDir, `github.com
  ✓ Logged in to github.com account tester (keyring)
  - Active account: true
  - Token scopes: 'gist', 'repo'
`, 0)

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolUnknown)
	res := assertSymbol(t, report, doctor.LabelStatusNames, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "カンバンを読めなかったため") {
		t.Fatalf("ボードを読めなかったことが理由に出ていない: %q", res.Detail)
	}
}

// TestDoctor_設定ファイルを読めなければStatusの名前は確かめられなかったになる は、
// 上流の設定ファイルが落ちたときの理由を固定する。
//
// 目的: 設定を読めていないときは、**照合する Status 名そのものが決まらない。**
// ボードが落ちたときとは直す先が違うので、理由の文言も分けること。
// 与える情報: WORKFLOW.md を消した状態。
// 成功条件: `Status の名前` が `!` で、理由が設定ファイルを読めなかったことであること。
func TestDoctor_設定ファイルを読めなければStatusの名前は確かめられなかったになる(t *testing.T) {
	fx := newFixture(t)
	if err := os.Remove(fx.WorkflowPath); err != nil {
		t.Fatalf("WORKFLOW.md を消せません: %v", err)
	}

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelStatusNames, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "照合する Status 名が決まりません") {
		t.Fatalf("設定を読めなかったことが理由に出ていない: %q", res.Detail)
	}
}
