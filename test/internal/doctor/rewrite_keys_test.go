package doctor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/doctor"
)

// writeRewriteKeysWorkflow は、`tracker.automated_state_rewrite` を指定した
// WORKFLOW.md を書き直す。
//
// **fixture の WriteWorkflow は使えない。**あちらは `tracker:` ブロックを固定で書くので、
// 対応表を差し込む口が無い（同じ階層に `tracker:` を2つ書くと YAML のキー重複になる）。
//
// **キーは設定のどこにも名前が出てこない Status にすること。**`config.Validate` は
// 既知の Status をキーに書いた行を弾く（その行は1度も発火しないため）。
// **戻す先は `tracker.active_states` の中から選ぶこと**（既定は `Ready` と `In Progress`）。
//
// t: 呼び出し元のテスト。
// rewriteBlock: `automated_state_rewrite:` の下に置く YAML（インデント4文字から始め、
// 末尾は "\n" で終えること）。
// fx: 使っている fixture。
func writeRewriteKeysWorkflow(t *testing.T, fx *fixture, rewriteBlock string) {
	t.Helper()

	content := fmt.Sprintf(`---
tracker:
  provider:
    owner: octocat
    project_number: 3
    status_field: Status
    token_source: env
    token_env: CONTINUO_TEST_TOKEN
  automated_state_rewrite:
%sworkspace:
  root: %s
herdr:
  socket: %s
  protocol: 20
  read_timeout_ms: 3000
rate_limit:
  source: none
---

{{.issue.identifier}} を実装してください。
`, rewriteBlock, filepath.Join(fx.Root, "wt"), fx.Herdr.SocketPath)

	if err := os.WriteFile(fx.WorkflowPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
}

// TestDoctor_対応表のキーがボードに無ければ注意を出す は、
// **綴りを打ち間違えた行が黙って死ぬ**のを捕まえることを確かめる（設計 3-57。issue #67）。
//
// 目的: `tracker.automated_state_rewrite` のキーがボードの Status の選択肢に無いとき、
// 見出し語 `対応表のキー` が `!` になり、**どのキーかを名前で**内訳に出すこと。
// **`✗` にしない**（キーはボードに実在しなくてよい）ので、終了コードは 0 のままであること。
//
// **起動時の警告では代わりにならない。**その警告は tracker のアダプタが logger へ出すが、
// **`continuo doctor` はその logger を捨てる**（`Options.Logger` の既定は `io.Discard`）。
//
// 与える情報: `In Progres`（`s` が1つ足りない）をキーにした対応表と、既定のボード。
// 成功条件: `対応表のキー` が `!`、内訳にキー名と設定キーが出て、
// 直し方が両方向（綴りを直す・行を消す）を示し、終了コードが 0 であること。
func TestDoctor_対応表のキーがボードに無ければ注意を出す(t *testing.T) {
	fx := newFixture(t)
	writeRewriteKeysWorkflow(t, fx, "    \"In Progres\": \"In Progress\"\n")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelRewriteKeys, doctor.SymbolUnknown)
	notes := strings.Join(res.Notes, "\n")
	if !strings.Contains(notes, "tracker.automated_state_rewrite") {
		t.Fatalf("どの設定キーの話かが内訳に出ていない: %q", notes)
	}
	if !strings.Contains(notes, `"In Progres"`) {
		t.Fatalf("ボードに無いキーが名前で内訳に出ていない: %q", notes)
	}
	remedies := strings.Join(res.Remedies, "\n")
	if !strings.Contains(remedies, "綴りの打ち間違いなら") || !strings.Contains(remedies, "行を消してください") {
		t.Fatalf("直し方が両方向を示していない: %q", remedies)
	}
	// **`✗` にしない。**キーはボードに実在しなくてよい（実在を要求すると、
	// ボードの自動化をやめて選択肢を消した人が抜け出せなくなる）。
	if report.ExitCode() != 0 {
		t.Fatalf("注意だけなのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
	// **ボードを読んだときの応答を使い回す。**この検査のためにリクエストを増やさない。
	if got := fx.GitHub.Queries(); !equalStrings(got, []string{"bootstrap", "workflows", "items"}) {
		t.Fatalf("ボードへ送ったクエリが想定と違う: %v", got)
	}
}

// TestDoctor_打ち間違えたキーは紛らわしさの検査では拾えない は、
// **見出し語 `Status の名前` が代わりにならない**ことを固定する（設計 3-57）。
//
// 目的: `Status の名前` が拾うのは「区切りを落とすと同じ綴り」か
// 「一方が他方を語の並びとして丸ごと含む」だけである。**`In Progres` と `In Progress` は
// どちらにも当たらない。**この test が落ちるようになったら、
// **見出し語 `対応表のキー` を消してよいかを考え直すこと。**
//
// 与える情報: `In Progres` をキーにした対応表と、既定のボード。
// 成功条件: `Status の名前` は `✓` のままで、`対応表のキー` だけが `!` になること。
func TestDoctor_打ち間違えたキーは紛らわしさの検査では拾えない(t *testing.T) {
	fx := newFixture(t)
	writeRewriteKeysWorkflow(t, fx, "    \"In Progres\": \"In Progress\"\n")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelStatusNames, doctor.SymbolOK)
	if !strings.Contains(res.Detail, "紛らわしい選択肢はありません") {
		t.Fatalf("紛らわしい組が無いことが説明に出ていない: %q", res.Detail)
	}
	assertSymbol(t, report, doctor.LabelRewriteKeys, doctor.SymbolUnknown)
}

// TestDoctor_対応表のキーがボードにあれば通る は、**注意を出してはいけない形**を固定する。
//
// 目的: 対応表のキーがすべてボードの Status の選択肢にあるとき、
// 見出し語 `対応表のキー` が `✓` になり、内訳も直し方も1件も無いこと。
// 与える情報: ボードにある `Ice Box` をキーにした対応表（戻す先は `Ready`）。
// 成功条件: `対応表のキー` が `✓` で、内訳も直し方も1件も無いこと。
func TestDoctor_対応表のキーがボードにあれば通る(t *testing.T) {
	fx := newFixture(t)
	writeRewriteKeysWorkflow(t, fx, "    \"Ice Box\": \"Ready\"\n")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelRewriteKeys, doctor.SymbolOK)
	if len(res.Notes) != 0 || len(res.Remedies) != 0 {
		t.Fatalf("ボードにあるのに注意が出ている: %+v", res)
	}
}

// TestDoctor_対応表のキーは大文字小文字と前後の空白だけの違いを同じ値とみなす は、
// **実行時の照合と検査の照合をそろえる**ことを確かめる。
//
// 目的: トラッカーは大文字小文字と前後の空白を無視して Status を引き当てる（SPEC.md 11.3）。
// **ここだけ完全一致で比べると、実行時には引ける行を「一度も効かない」と報告する。**
// 与える情報: ボードの `Ice Box` に対して `  iCE bOX  ` をキーにした対応表。
// 成功条件: `対応表のキー` が `✓` であること。
func TestDoctor_対応表のキーは大文字小文字と前後の空白だけの違いを同じ値とみなす(t *testing.T) {
	fx := newFixture(t)
	writeRewriteKeysWorkflow(t, fx, "    \"  iCE bOX  \": \"Ready\"\n")

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelRewriteKeys, doctor.SymbolOK)
}

// TestDoctor_対応表が空なら対応表のキーの注意を出さない は、
// **読み飛ばされる注意を作らない**ことを確かめる。
//
// 目的: `tracker.automated_state_rewrite` が空なら書き戻しそのものが走らない。
// **ここで注意を出すと、書き戻しを使っていない人が毎回読み飛ばす注意を1件抱える。**
// 与える情報: 既定の設定（対応表は空）。
// 成功条件: `対応表のキー` が `✓` で、説明が「書き戻しを行わない設定」であること。
func TestDoctor_対応表が空なら対応表のキーの注意を出さない(t *testing.T) {
	fx := newFixture(t)

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelRewriteKeys, doctor.SymbolOK)
	if !strings.Contains(res.Detail, "書き戻しを行わない設定") {
		t.Fatalf("書き戻しを行わない設定であることが説明に出ていない: %q", res.Detail)
	}
}

// TestDoctor_ボードを読めなければ対応表のキーは確かめられなかったになる は、
// 上流が落ちたときの記号と理由を固定する。
//
// 目的: ボードが `✗` か `!` のとき、`対応表のキー` を `!` にし、
// **ボードを読めなかったことを理由に出す**こと（照合する選択肢を持っていないため）。
// **ここで「ボードに無い」と言ってはならない。**読めていないだけである。
// 与える情報: gh の scope から project を外したテスト用gh mock（ボードは `!` になる）と、
// ボードに無いキーを持つ対応表。
// 成功条件: `対応表のキー` が `!` で、理由がボードを読めなかったことであること。
func TestDoctor_ボードを読めなければ対応表のキーは確かめられなかったになる(t *testing.T) {
	fx := newFixture(t)
	writeRewriteKeysWorkflow(t, fx, "    \"In Progres\": \"In Progress\"\n")
	writeFakeGH(t, fx.BinDir, `github.com
  ✓ Logged in to github.com account tester (keyring)
  - Active account: true
  - Token scopes: 'gist', 'repo'
`, 0)

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolUnknown)
	res := assertSymbol(t, report, doctor.LabelRewriteKeys, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "カンバンを読めなかったため") {
		t.Fatalf("ボードを読めなかったことが理由に出ていない: %q", res.Detail)
	}
	if len(res.Notes) != 0 {
		t.Fatalf("読めていないのにキーの内訳を出している: %+v", res.Notes)
	}
}

// TestDoctor_設定ファイルを読めなければ対応表のキーは確かめられなかったになる は、
// 上流の設定ファイルが落ちたときの理由を固定する。
//
// 目的: 設定を読めていないときは、**照合するキーそのものが決まらない。**
// ボードが落ちたときとは直す先が違うので、理由の文言も分けること。
// 与える情報: WORKFLOW.md を消した状態。
// 成功条件: `対応表のキー` が `!` で、理由が設定ファイルを読めなかったことであること。
func TestDoctor_設定ファイルを読めなければ対応表のキーは確かめられなかったになる(t *testing.T) {
	fx := newFixture(t)
	if err := os.Remove(fx.WorkflowPath); err != nil {
		t.Fatalf("WORKFLOW.md を消せません: %v", err)
	}

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelRewriteKeys, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "照合する対応表のキーが決まりません") {
		t.Fatalf("設定を読めなかったことが理由に出ていない: %q", res.Detail)
	}
}
