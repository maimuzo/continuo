package scaffold_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// jaStatuses は日本語の選択肢名で埋めた割り当てである。
//
// **ボードの選択肢名は英語とは限らない。**引用符の付け方が壊れていないかを、
// 空白を含む名前（`レビュー 待ち`）で確かめる。
var jaStatuses = scaffold.Statuses{
	Dispatch: "着手待ち",
	Running:  "作業中",
	Review:   "レビュー 待ち",
	Blocked:  "保留",
	Done:     "完了",
}

// 目的: `continuo setup` が決めた割り当てが WORKFLOW.md の7つのキーへ書かれることを確認する。
// 与える情報: 本番のボードの選択肢名で埋めた Statuses。
// 成功条件: dispatch_state / running_state / failure_state / active_states / terminal_states /
// status_signal_map.review / status_signal_map.blocked に、割り当てた選択肢名が入ること。
// 雛形の既定値（`Ready` など）が残っていないこと。
func TestTemplateWithValues_割り当てた選択肢が7つのキーへ書かれる(t *testing.T) {
	out := scaffold.TemplateWithValues(scaffold.Values{
		Owner:         "acme",
		ProjectNumber: 3,
		Statuses: scaffold.Statuses{
			Dispatch: "Ready",
			Running:  "In Progress",
			Review:   "In Review",
			Blocked:  "Blocked",
			Done:     "Done",
		},
	})

	wants := []string{
		`  dispatch_state: "Ready"`,
		`  running_state: "In Progress"`,
		`  failure_state: "Blocked"`,
		`  active_states: ["Ready", "In Progress"]`,
		`  terminal_states: ["Done"]`,
		`    review: "In Review"`,
		`    blocked: "Blocked"`,
	}
	for _, w := range wants {
		if !containsLineWithPrefix(out, w) {
			t.Errorf("書き出しに %q で始まる行が無い", w)
		}
	}
}

// 目的: 日本語で空白を含む選択肢名でも、書き出した WORKFLOW.md が読み込めることを確認する。
// 与える情報: 日本語の選択肢名で埋めた Statuses と、埋めた owner / project_number。
// 成功条件: config.Load が通り、5つの役割の値が割り当てたとおりに読めること。
func TestWriteTemplateWithValues_日本語の選択肢名でも読み込める(t *testing.T) {
	dir := t.TempDir()
	result, err := scaffold.WriteTemplateWithValues(dir, false, scaffold.Values{
		Owner:         "acme",
		ProjectNumber: 3,
		Statuses:      jaStatuses,
	})
	if err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}

	loaded, err := config.Load(result.Path)
	if err != nil {
		t.Fatalf("割り当てを書いた雛形を読み込めなかった: %v", err)
	}
	p := loaded.Config.Tracker
	if p.DispatchState != jaStatuses.Dispatch {
		t.Errorf("dispatch_state が違う: %q（期待 %q）", p.DispatchState, jaStatuses.Dispatch)
	}
	if p.RunningState != jaStatuses.Running {
		t.Errorf("running_state が違う: %q（期待 %q）", p.RunningState, jaStatuses.Running)
	}
	if p.FailureState != jaStatuses.Blocked {
		t.Errorf("failure_state が違う: %q（期待 %q）", p.FailureState, jaStatuses.Blocked)
	}
	if strings.Join(p.ActiveStates, "|") != jaStatuses.Dispatch+"|"+jaStatuses.Running {
		t.Errorf("active_states が違う: %v", p.ActiveStates)
	}
	if strings.Join(p.TerminalStates, "|") != jaStatuses.Done {
		t.Errorf("terminal_states が違う: %v", p.TerminalStates)
	}
	if got := p.StatusSignalMap["review"]; got == nil || *got != jaStatuses.Review {
		t.Errorf("status_signal_map.review が違う: %v（期待 %q）", got, jaStatuses.Review)
	}
	if got := p.StatusSignalMap["blocked"]; got == nil || *got != jaStatuses.Blocked {
		t.Errorf("status_signal_map.blocked が違う: %v（期待 %q）", got, jaStatuses.Blocked)
	}
}

// 目的: 5つの役割のうち1つでも欠けたら、雛形の既定値をそのまま残すことを確認する。
//
// **一部だけ差し替えると、割り当てた Status と既定値のままの Status が混ざる。**
// その WORKFLOW.md は起動時の照合で「ボードに無い Status」として落ちる。
//
// 与える情報: 完了だけ空にした Statuses。
// 成功条件: 雛形の既定値（`Ready` / `In Progress` / `Done`）がそのまま残ること。
func TestTemplateWithValues_役割が1つでも欠けたら既定値を残す(t *testing.T) {
	incomplete := jaStatuses
	incomplete.Done = ""

	out := scaffold.TemplateWithValues(scaffold.Values{Statuses: incomplete})
	wants := []string{
		`  dispatch_state: "Ready"`,
		`  running_state: "In Progress"`,
		`  terminal_states: ["Done"]`,
	}
	for _, w := range wants {
		if !containsLineWithPrefix(out, w) {
			t.Errorf("既定値の行 %q が残っていない", w)
		}
	}
	// 雛形のコメントにも「着手待ち」という語が出るので、**キーの行だけを見る。**
	if containsLineWithPrefix(out, `  dispatch_state: "`+jaStatuses.Dispatch+`"`) {
		t.Errorf("役割が欠けているのに割り当てを書き込んでいる")
	}
}

// containsLineWithPrefix は、prefix で始まる行が全文の中にあるかを返す。
//
// **行の途中に現れただけでは真にしない。**コメントの中の同じ語を拾わないためである。
//
// s: 探す対象の全文。
// prefix: 行の先頭から一致させる文字列。
// 戻り値: そのような行が1つでもあれば真。
func containsLineWithPrefix(s, prefix string) bool {
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}
