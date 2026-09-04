package doctor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/doctor"
)

// writeCleanupStatesWorkflow は、終わったとみなす Status と片付けを始める Status を
// 指定した WORKFLOW.md を書き直す。
//
// **fixture の WriteWorkflow は使えない。**あちらは `tracker:` ブロックを固定で書くので、
// `terminal_states` を差し込む口が無い（同じ階層に `tracker:` を2つ書くと YAML のキー重複になる）。
//
// **ボードの選択肢と噛み合わせる。**`terminal_states` に書いた名前がボードに無いと
// Bootstrap が落ち、見出し語 `ボード` が `✗` になる。この検査そのものはボードを読まないが、
// **ほかの見出し語を巻き添えにすると、何を確かめた test なのか分からなくなる。**
// `cleanup.on_states` は Bootstrap の照合の対象ではない（設計 3-6）ので、
// ボードに無い名前を書いてよい。
//
// t: 呼び出し元のテスト。
// fx: 使っている fixture。
// terminalStates: `tracker.terminal_states` に書く YAML の値（`["Done"]` の形）。
// cleanupBlock: `cleanup:` ブロックの中身（インデント2文字から始め、末尾は "\n" で終えること）。
func writeCleanupStatesWorkflow(t *testing.T, fx *fixture, terminalStates, cleanupBlock string) {
	t.Helper()

	content := fmt.Sprintf(`---
tracker:
  provider:
    owner: octocat
    project_number: 3
    status_field: Status
    token_source: env
    token_env: CONTINUO_TEST_TOKEN
  terminal_states: %s
workspace:
  root: %s
herdr:
  socket: %s
  protocol: 20
  read_timeout_ms: 3000
cleanup:
%srate_limit:
  source: none
---

{{.issue.identifier}} を実装してください。
`, terminalStates, filepath.Join(fx.Root, "wt"), fx.Herdr.SocketPath, cleanupBlock)

	if err := os.WriteFile(fx.WorkflowPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
}

// TestDoctor_片付ける状態が終わったとみなす状態の外にあれば注意を出す は、
// **「終わっていないと判定した直後に片付ける」設定**を捕まえることを確かめる（issue #35）。
//
// 目的: `cleanup.on_states` に `tracker.terminal_states` の外の値があるとき、
// 見出し語 `片付けの状態` が `!` になり、**どの設定キーのどの値か**を内訳に出すこと。
// **`✗` にしない**（continuo は起動して走る）ので、終了コードは 0 のままであること。
//
// 与える情報: `terminal_states: ["Done"]` と `cleanup.on_states: ["Archived"]`。
// 成功条件: `片付けの状態` が `!`、内訳に両方のキー名と `"Archived"` が出て、
// 直し方が両方向（足す・外す）を示し、終了コードが 0 であること。
func TestDoctor_片付ける状態が終わったとみなす状態の外にあれば注意を出す(t *testing.T) {
	fx := newFixture(t)
	writeCleanupStatesWorkflow(t, fx, `["Done"]`, "  enabled: true\n  on_states: [\"Archived\"]\n")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelCleanupStates, doctor.SymbolUnknown)
	notes := strings.Join(res.Notes, "\n")
	if !strings.Contains(notes, "cleanup.on_states") || !strings.Contains(notes, "tracker.terminal_states") {
		t.Fatalf("どの設定キーの話かが内訳に出ていない: %q", notes)
	}
	if !strings.Contains(notes, `"Archived"`) {
		t.Fatalf("食い違った値が内訳に出ていない: %q", notes)
	}
	remedies := strings.Join(res.Remedies, "\n")
	if !strings.Contains(remedies, "tracker.terminal_states に") || !strings.Contains(remedies, "外してください") {
		t.Fatalf("直し方が両方向を示していない: %q", remedies)
	}
	// **`✗` にしない。**噛み合っていなくても continuo は起動して走る。
	if report.ExitCode() != 0 {
		t.Fatalf("注意だけなのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
	// **設定を読むだけの検査である。**この検査のためにボードへリクエストを増やさない。
	if got := fx.GitHub.Queries(); !equalStrings(got, []string{"bootstrap", "items", "workflows"}) {
		t.Fatalf("ボードへ送ったクエリが想定と違う: %v", got)
	}
}

// TestDoctor_片付ける状態が全部終わったとみなす状態に入っていれば通る は、
// **注意を出してはいけない形**を固定する。
//
// 目的: `cleanup.on_states` の値がすべて `tracker.terminal_states` にあるとき、
// 見出し語 `片付けの状態` が `✓` になること。
// 与える情報: `terminal_states: ["AI Done", "Done"]` と `cleanup.on_states: ["Done"]`。
// 成功条件: `片付けの状態` が `✓` で、内訳も直し方も1件も無いこと。
func TestDoctor_片付ける状態が全部終わったとみなす状態に入っていれば通る(t *testing.T) {
	fx := newFixture(t)
	// **`AI Done` もボードの選択肢に足す。**Bootstrap は terminal_states を照合する。
	fx.GitHub.SetStatusOptions("Ice Box", "Ready", "In Progress", "Blocked", "In Review", "Done", "AI Done")
	writeCleanupStatesWorkflow(t, fx, `["AI Done", "Done"]`, "  enabled: true\n  on_states: [\"Done\"]\n")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelCleanupStates, doctor.SymbolOK)
	if len(res.Notes) != 0 || len(res.Remedies) != 0 {
		t.Fatalf("噛み合っているのに注意が出ている: %+v", res)
	}
}

// TestDoctor_片付ける状態は大文字小文字と前後の空白だけの違いを同じ値とみなす は、
// **実行時の照合と検査の照合をそろえる**ことを確かめる。
//
// 目的: トラッカーは大文字小文字と前後の空白を無視して Status を引き当てる。
// **ここだけ完全一致で比べると、実行時には噛み合っている設定を「食い違っている」と報告する。**
// 与える情報: `terminal_states: ["Done"]` と `cleanup.on_states: ["  dONE  "]`。
// 成功条件: `片付けの状態` が `✓` であること。
func TestDoctor_片付ける状態は大文字小文字と前後の空白だけの違いを同じ値とみなす(t *testing.T) {
	fx := newFixture(t)
	writeCleanupStatesWorkflow(t, fx, `["Done"]`, "  enabled: true\n  on_states: [\"  dONE  \"]\n")

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelCleanupStates, doctor.SymbolOK)
}

// TestDoctor_片付けを行わない設定なら片付ける状態の注意を出さない は、
// **読み飛ばされる注意を作らない**ことを確かめる。
//
// 目的: `cleanup.enabled` が偽なら片付けそのものが走らないので、噛み合っていなくても
// 何も起きない。**ここで注意を出すと、片付けを切った人が毎回読み飛ばす注意を1件抱える。**
// 与える情報: `terminal_states: ["Done"]`・`cleanup.enabled: false`・`on_states: ["Archived"]`。
// 成功条件: `片付けの状態` が `✓` で、説明が「片付けを行わない設定」であること。
func TestDoctor_片付けを行わない設定なら片付ける状態の注意を出さない(t *testing.T) {
	fx := newFixture(t)
	writeCleanupStatesWorkflow(t, fx, `["Done"]`, "  enabled: false\n  on_states: [\"Archived\"]\n")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelCleanupStates, doctor.SymbolOK)
	if !strings.Contains(res.Detail, "片付けを行わない設定") {
		t.Fatalf("片付けを行わない設定であることが説明に出ていない: %q", res.Detail)
	}
}

// TestDoctor_設定ファイルを読めなければ片付けの状態は確かめられなかったになる は、
// 上流が落ちたときの記号と理由を固定する。
//
// 目的: 突き合わせる2つのキーはどちらも設定にしか無い。設定を読めていなければ、
// **何と何を比べるのかが決まらない。**
// 与える情報: WORKFLOW.md を消した状態。
// 成功条件: `片付けの状態` が `!` で、理由が設定ファイルを読めなかったことであること。
func TestDoctor_設定ファイルを読めなければ片付けの状態は確かめられなかったになる(t *testing.T) {
	fx := newFixture(t)
	if err := os.Remove(fx.WorkflowPath); err != nil {
		t.Fatalf("WORKFLOW.md を消せません: %v", err)
	}

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelCleanupStates, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "突き合わせられません") {
		t.Fatalf("設定を読めなかったことが理由に出ていない: %q", res.Detail)
	}
}
