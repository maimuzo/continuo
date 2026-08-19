package orchestrator_test

import (
	"testing"

	"github.com/maimuzo/continuo/internal/orchestrator"
)

// signalPrefix はテストで使う表明の印である（設定の tracker.status_signal_prefix の既定値）。
const signalPrefix = "CONTINUO-STATUS:"

// currentIssue はテストで「いま作業している issue」として使う識別子である。
const currentIssue = "maimuzo/koetsumugi#188"

// TestParseSignals_対象を書かない行はいま作業しているissueを指す は、
// 表明の既定の対象がその run の issue になることを確かめる。
//
// 目的: 設計 3-25 の「対象を書かない行は、いま作業している issue」を守っていることを示す。
// 与える情報: `CONTINUO-STATUS: review` の1行だけを含む assistant の本文。
// 成功条件: いま作業している issue の識別子をキーに "review" が入る。
func TestParseSignals_対象を書かない行はいま作業しているissueを指す(t *testing.T) {
	got := orchestrator.ParseSignals([]string{"作業が終わりました。\n\nCONTINUO-STATUS: review"}, signalPrefix, currentIssue)

	if len(got) != 1 {
		t.Fatalf("拾った表明の件数が想定と違う: got %d, want 1 (%v)", len(got), got)
	}
	if got[currentIssue] != "review" {
		t.Fatalf("表明の値が想定と違う: got %q, want %q", got[currentIssue], "review")
	}
}

// TestParseSignals_行に割って探すので他の文と同じブロックでも拾える は、
// 印がブロックの途中にあっても拾えることを確かめる。
//
// 目的: 設計 3-25 の「行に割って探すのが要点である。ブロックの一致では取れない」を示す。
// 与える情報: 1つのブロックの中に説明文と印が混ざった本文。
// 成功条件: 印が拾える。
func TestParseSignals_行に割って探すので他の文と同じブロックでも拾える(t *testing.T) {
	text := "3つとも完了しました。テストも通っています。\n\nCONTINUO-STATUS: review"

	got := orchestrator.ParseSignals([]string{text}, signalPrefix, currentIssue)

	if got[currentIssue] != "review" {
		t.Fatalf("ブロックの中の行を拾えていない: got %v", got)
	}
}

// TestParseSignals_印が複数あればissueごとに最後のものを採る は、
// 同じ issue に複数の表明が書かれたときの優先順位を確かめる。
//
// 目的: 設計 3-25 の段7「複数見つかったら、最後に現れたものを採る」を守っていることを示す。
// 与える情報: 同じ issue について working → review の順に書かれた本文。
// 成功条件: 後に現れた review が採られる。
func TestParseSignals_印が複数あればissueごとに最後のものを採る(t *testing.T) {
	texts := []string{
		"まだ途中です。\nCONTINUO-STATUS: working",
		"終わりました。\nCONTINUO-STATUS: review",
	}

	got := orchestrator.ParseSignals(texts, signalPrefix, currentIssue)

	if got[currentIssue] != "review" {
		t.Fatalf("最後に現れた表明が採られていない: got %q, want %q", got[currentIssue], "review")
	}
}

// TestParseSignals_グループの別issueを番号で指せる は、グループの表明の書式を確かめる。
//
// 目的: 設計 3-26 の「`#<番号>` は代表の issue と同じリポジトリを指す」を示す。
// 与える情報: 対象なし・`#45`・`maimuzo/other#47` の3行。
// 成功条件: 3件が別々のキーに入り、`#45` は代表と同じリポジトリの識別子になる。
func TestParseSignals_グループの別issueを番号で指せる(t *testing.T) {
	text := "CONTINUO-STATUS: review\nCONTINUO-STATUS: #45 review\nCONTINUO-STATUS: maimuzo/other#47 blocked"

	got := orchestrator.ParseSignals([]string{text}, signalPrefix, currentIssue)

	want := map[string]string{
		currentIssue:            "review",
		"maimuzo/koetsumugi#45": "review",
		"maimuzo/other#47":      "blocked",
	}
	if len(got) != len(want) {
		t.Fatalf("拾った表明の件数が想定と違う: got %d, want %d (%v)", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s の表明が想定と違う: got %q, want %q（全体: %v）", k, got[k], v, got)
		}
	}
}

// TestParseSignals_印が無ければ空を返す は、表明が無い turn を検出できることを確かめる。
//
// 目的: 設計 3-25 の第3層（表明が無かったら次の turn で促す）の入口を示す。
// 与える情報: 印を含まない本文。
// 成功条件: 拾った表明が0件になる。
func TestParseSignals_印が無ければ空を返す(t *testing.T) {
	got := orchestrator.ParseSignals([]string{"作業を続けています。"}, signalPrefix, currentIssue)

	if len(got) != 0 {
		t.Fatalf("表明が無いのに拾ってしまった: %v", got)
	}
}
