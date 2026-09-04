package doctor_test

import (
	"os"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/doctor"
)

// setClaudeEnv は WORKFLOW.md の `claude.env` へ1行足す。
//
// **雛形の `env:` の塊へ足す。**塊ごと貼り替えると front matter が重複キーになり、
// `config.Load` が落ちる（この pull request が文書から消している間違いそのものである）。
// 既にある `CLAUDE_CODE_RETRY_WATCHDOG` の行を目印にして、その下へ差し込む。
//
// t: 呼び出し元のテスト。
// fx: 使っている fixture。
// name: 足す環境変数の名前。
// value: 足す値（YAML の二重引用符で囲んで書く）。
func setClaudeEnv(t *testing.T, fx *fixture, name, value string) {
	t.Helper()

	raw, err := os.ReadFile(fx.WorkflowPath)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	const anchor = "CLAUDE_CODE_RETRY_WATCHDOG"
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		if !strings.Contains(line, anchor) {
			continue
		}
		indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
		added := indent + name + ": \"" + value + "\""
		lines = append(lines[:i+1], append([]string{added}, lines[i+1:]...)...)
		if err := os.WriteFile(fx.WorkflowPath, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
			t.Fatalf("WORKFLOW.md を書けません: %v", err)
		}
		return
	}
	t.Fatalf("雛形に %s の行が見つかりません（足す目印にしています）", anchor)
}

// TestDoctor_agentteamsを有効にしていれば注意を出す は、
// **落ちてから気づくのを、起動する前に前倒しする**ことを確かめる（issue #137。設計 3-70）。
//
// 目的: `claude.env` に `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS: "1"` が書いてあるとき、
// 見出し語 `agent teams` が `!` になり、**切り方と、切らないと何が起きるかを出す**こと。
// **`✗` にしない**ので、終了コードは 0 のままであること。
//
// **なぜこの検査が要るのか。**agent teams が有効な環境では、名前つきの `Agent` ツールの
// 呼び出しが teammate として起動し、**その許可の確認がリードの pane に出る。**
// continuo はそれを `blocked` と読み、pane を閉じて issue を `failure_state` へ落とす。
// **いまは文書に書いてあるだけなので、読まなかった人は落ちてから気づく。**
//
// 与える情報: `claude.env` に `"1"` を書いた WORKFLOW.md。
// 成功条件: `agent teams` が `!`、直し方に切り方と落ちる先が出て、
// 読んでいない出どころが内訳に出て、終了コードが 0 であること。
func TestDoctor_agentteamsを有効にしていれば注意を出す(t *testing.T) {
	fx := newFixture(t)
	setClaudeEnv(t, fx, doctor.EnvAgentTeams, "1")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelAgentTeams, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, doctor.EnvAgentTeams) {
		t.Fatalf("どの環境変数の話かが説明に出ていない: %q", res.Detail)
	}
	remedies := strings.Join(res.Remedies, "\n")
	if !strings.Contains(remedies, `"0"`) {
		t.Fatalf("切り方が直し方に出ていない: %q", remedies)
	}
	if !strings.Contains(remedies, "failure_state") {
		t.Fatalf("切らないと何が起きるかが直し方に出ていない: %q", remedies)
	}
	// **読んでいない出どころを必ず出す。**読めた範囲だけを見て断言しない。
	notes := strings.Join(res.Notes, "\n")
	if !strings.Contains(notes, "読んでいない出どころ") {
		t.Fatalf("読んでいない出どころが内訳に出ていない: %q", notes)
	}
	// **`✗` にしない。**agent teams を自分の対話用に有効にしている人が、
	// 版を上げただけで continuo を起動できなくなる、ということを起こさない。
	if report.ExitCode() != 0 {
		t.Fatalf("注意だけなのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
}

// TestDoctor_agentteamsを切ってあれば通る は、`"0"` を正しく読むことを確かめる。
//
// 目的: `claude.env` の `"0"` は、**利用者の設定にもシェルの export にも勝つ**
// （設計 3-70 が引く公式の2文）。**シェルで有効にしてあっても `✓` にすること。**
// 与える情報: `claude.env` に `"0"` を書いた WORKFLOW.md と、シェルで `"1"` を張った環境。
// 成功条件: `agent teams` が `✓` で、説明が「切ってあります」であること。
func TestDoctor_agentteamsを切ってあれば通る(t *testing.T) {
	fx := newFixture(t)
	setClaudeEnv(t, fx, doctor.EnvAgentTeams, "0")
	fx.Env[doctor.EnvAgentTeams] = "1"

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelAgentTeams, doctor.SymbolOK)
	if !strings.Contains(res.Detail, "切ってあります") {
		t.Fatalf("切ってあることが説明に出ていない: %q", res.Detail)
	}
}

// TestDoctor_シェルでagentteamsを有効にしていれば注意を出す は、
// **claude.env に書いていないときだけシェルを見る**ことを確かめる。
//
// 目的: `claude.env` に書かれていないときは、herdr の pane が doctor を叩いたシェルと
// 同じ環境を継いでいる可能性がある。**その可能性を `!` で知らせ、
// 「同じとは限らない」ことも文言に書く**こと。
// 与える情報: `claude.env` には書かず、シェルで `"1"` を張った環境。
// 成功条件: `agent teams` が `!` で、説明に「doctor を叩いたシェル」が出ること。
func TestDoctor_シェルでagentteamsを有効にしていれば注意を出す(t *testing.T) {
	fx := newFixture(t)
	fx.Env[doctor.EnvAgentTeams] = "1"

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelAgentTeams, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "doctor を叩いたシェル") {
		t.Fatalf("どこで見つけたのかが説明に出ていない: %q", res.Detail)
	}
	if !strings.Contains(res.Detail, "herdr の pane") {
		t.Fatalf("同じ環境とは限らないことが説明に出ていない: %q", res.Detail)
	}
}

// TestDoctor_agentteamsの値が0でも1でもなければ判定できないと出す は、
// **同じものさしを両方の出どころへ当てる**ことを確かめる。
//
// 目的: 公式が意味を決めているのは `0` と `1` の2つだけである。
// **`true` のような値では、有効になるかどうかを判定できない。**
// **片方だけ厳しくすると、同じ検査の中に判定基準が2つできる。**
// 与える情報: `claude.env` に `"true"` を書いた WORKFLOW.md。
// 成功条件: `agent teams` が `!` で、説明に書いてある値が出ること。
func TestDoctor_agentteamsの値が0でも1でもなければ判定できないと出す(t *testing.T) {
	fx := newFixture(t)
	setClaudeEnv(t, fx, doctor.EnvAgentTeams, "true")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelAgentTeams, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, `"true"`) {
		t.Fatalf("書いてある値が説明に出ていない: %q", res.Detail)
	}
}

// TestDoctor_agentteamsに空の値を書いていればclaudeenvの側で答える は、
// **既に在るキーを「足してください」と案内しない**ことを確かめる（issue #137）。
//
// 目的: `claude.env` にキーが在って値が空のとき、シェルの側の枝へ落ちてはならない。
// **落ちると「claude.env には書かれていません」と嘘を言い、既に在るキーを足せと案内する。**
// **その案内どおりに足すと front matter が重複キーになり、continuo が起動しない。**
// 与える情報: `claude.env` に空の値を書いた WORKFLOW.md と、シェルで `"1"` を張った環境。
// 成功条件: `agent teams` が `!` で、説明が `claude.env` の話であること
// （「doctor を叩いたシェル」と言わないこと）。
func TestDoctor_agentteamsに空の値を書いていればclaudeenvの側で答える(t *testing.T) {
	fx := newFixture(t)
	setClaudeEnv(t, fx, doctor.EnvAgentTeams, "")
	fx.Env[doctor.EnvAgentTeams] = "1"

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelAgentTeams, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "claude.env") {
		t.Fatalf("claude.env の話になっていない: %q", res.Detail)
	}
	if strings.Contains(res.Detail, "doctor を叩いたシェル") {
		t.Fatalf("キーが在るのにシェルの側の枝へ落ちている（書かれていないと嘘を言う）: %q", res.Detail)
	}
	// **直し方は「書き換える」と「足す」の両方を案内すること。**
	// キーが在るのに「足してください」だけだと、重複キーになって起動しなくなる。
	remedies := strings.Join(res.Remedies, "\n")
	if !strings.Contains(remedies, "grep -n") {
		t.Fatalf("場所を見つける手立てが直し方に無い: %q", remedies)
	}
}

// TestDoctor_agentteamsを書いていなければ注意を出さない は、
// **書いていないことを警告しない**ことを固定する。
//
// 目的: 設計 3-70 が「**continuo がこれを既定で書き込むことはしない**（2026-08-28、人間の判断）。
// **無効なものを無効にする設定は、読む人を惑わせるだけである。**」と決めている。
// **`1` を見つけたときだけ知らせる。**
// **それでも「読んでいない出どころ」は出す**（読めた範囲だけを見て断言しない）。
// 与える情報: 既定の設定（`claude.env` にこの環境変数は無い）。
// 成功条件: `agent teams` が `✓` で、内訳に読んでいない出どころが出て、直し方が1件も無いこと。
func TestDoctor_agentteamsを書いていなければ注意を出さない(t *testing.T) {
	fx := newFixture(t)

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelAgentTeams, doctor.SymbolOK)
	if len(res.Remedies) != 0 {
		t.Fatalf("書いていないだけなのに直し方を出している: %+v", res.Remedies)
	}
	notes := strings.Join(res.Notes, "\n")
	if !strings.Contains(notes, "~/.claude/settings.json") {
		t.Fatalf("読んでいない出どころが内訳に出ていない: %q", notes)
	}
}

// TestDoctor_設定ファイルを読めなければagentteamsは確かめられなかったになる は、
// 上流の設定ファイルが落ちたときの記号と理由を固定する。
//
// 目的: `claude.env` は設定にしか無いので、設定を読めなければ何も判定できない。
// **`✓` にしてはならない。**
// 与える情報: WORKFLOW.md を消した状態。
// 成功条件: `agent teams` が `!` で、理由が設定ファイルを読めなかったことであること。
func TestDoctor_設定ファイルを読めなければagentteamsは確かめられなかったになる(t *testing.T) {
	fx := newFixture(t)
	if err := os.Remove(fx.WorkflowPath); err != nil {
		t.Fatalf("WORKFLOW.md を消せません: %v", err)
	}

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelAgentTeams, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "claude.env に何が書いてあるか分かりません") {
		t.Fatalf("設定を読めなかったことが理由に出ていない: %q", res.Detail)
	}
}
