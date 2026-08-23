package orchestrator_test

import (
	"testing"

	"github.com/maimuzo/continuo/internal/orchestrator"
)

// TestReadTranscript_typed起点で区切るのでtask_notificationで割れても拾える は、
// 表明を区切る起点が `promptSource == "typed"` であることを確かめる。
//
// 目的: 設計 3-25 の「`prompt_id` で区切ってはならない（17件中3件で取り逃した）。
// `promptSource == "typed"` を起点にすれば 17件中17件で取れた」を守っていることを示す。
// 与える情報: typed の user 行のあとに `<task-notification>`（promptSource が system）が
// 差し込まれ、prompt_id が変わったあとで印が書かれた transcript。
// **Stop hook が渡す prompt_id は、印の入っていない後半のものである。**
// 成功条件: prompt_id で区切ると取り逃す印を、typed 起点なら拾える。
func TestReadTranscript_typed起点で区切るのでtask_notificationで割れても拾える(t *testing.T) {
	dir := t.TempDir()
	path := writeTranscript(t, dir, "session.jsonl", []any{
		typedUserLine("p1", "この issue を実装してください"),
		assistantLine("req1", "調査します", false),
		map[string]any{
			"type": "user", "promptSource": "system", "promptId": "p2", "isSidechain": false,
			"message": map[string]any{"content": "<task-notification><task-id>t1</task-id></task-notification>"},
		},
		assistantLine("req2", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	// Stop hook が渡すのは、印を含む assistant 行の直前の user 行の prompt_id（p2）である。
	got, err := orchestrator.ReadTranscript(path, "p2", signalPrefix, currentIssue)
	if err != nil {
		t.Fatalf("transcript を読めない: %v", err)
	}

	if got.Signals[currentIssue] != "review" {
		t.Fatalf("typed 起点で区切れていない（印を取り逃した）: got %v", got.Signals)
	}
}

// TestReadTranscript_isSidechainの発言は表明として拾わない は、
// subagent の発言を印として拾わないことを確かめる。
//
// 目的: 設計 3-25 の段5「`isSidechain == false` に絞る」を守っていることを示す。
// 与える情報: subagent（isSidechain が真）だけが印を書いた transcript。
// 成功条件: 表明が0件になる。
func TestReadTranscript_isSidechainの発言は表明として拾わない(t *testing.T) {
	dir := t.TempDir()
	path := writeTranscript(t, dir, "session.jsonl", []any{
		typedUserLine("p1", "この issue を実装してください"),
		assistantLine("req1", "CONTINUO-STATUS: review", true),
	})

	got, err := orchestrator.ReadTranscript(path, "p1", signalPrefix, currentIssue)
	if err != nil {
		t.Fatalf("transcript を読めない: %v", err)
	}

	if len(got.Signals) != 0 {
		t.Fatalf("subagent の発言を表明として拾ってしまった: %v", got.Signals)
	}
}

// TestReadTranscript_前のturnの表明は拾わない は、turn の範囲の切り出しを確かめる。
//
// 目的: 設計 3-25 の段4「頭から後ろへ、次の typed の手前までをこの turn の範囲とする」を示す。
// 与える情報: 1回目の turn に review、2回目の turn に印なし、の transcript。
// 成功条件: 2回目の turn の prompt_id で読むと表明が0件になる。
func TestReadTranscript_前のturnの表明は拾わない(t *testing.T) {
	dir := t.TempDir()
	path := writeTranscript(t, dir, "session.jsonl", []any{
		typedUserLine("p1", "1回目"),
		assistantLine("req1", "CONTINUO-STATUS: review", false),
		typedUserLine("p2", "続けてください"),
		assistantLine("req2", "作業を続けています", false),
	})

	got, err := orchestrator.ReadTranscript(path, "p2", signalPrefix, currentIssue)
	if err != nil {
		t.Fatalf("transcript を読めない: %v", err)
	}

	if len(got.Signals) != 0 {
		t.Fatalf("前の turn の表明を拾ってしまった: %v", got.Signals)
	}
}

// TestReadTranscript_トークンをrequestIdで重複排除して集計する は、
// 表明と同じ1回の読み取りでトークンも取れることを確かめる。
//
// 目的: 設計 3-15 の「同じ1回の読み取りで、表明とトークンの両方を取る（2回開かない）。
// 集計は requestId で重複排除する」を守っていることを示す。
// 与える情報: 同じ requestId の assistant 行が2件、別の requestId が1件ある transcript。
// 成功条件: API 応答は2件として数えられ、各トークンは2件ぶんの合計になる。
func TestReadTranscript_トークンをrequestIdで重複排除して集計する(t *testing.T) {
	dir := t.TempDir()
	path := writeTranscript(t, dir, "session.jsonl", []any{
		typedUserLine("p1", "1回目"),
		assistantLine("req1", "その1", false),
		assistantLine("req1", "その1の続き（同じ API 応答）", false),
		assistantLine("req2", "その2\nCONTINUO-STATUS: review", false),
	})

	got, err := orchestrator.ReadTranscript(path, "p1", signalPrefix, currentIssue)
	if err != nil {
		t.Fatalf("transcript を読めない: %v", err)
	}

	if got.Usage.APICalls != 2 {
		t.Fatalf("requestId で重複排除できていない: got %d, want 2", got.Usage.APICalls)
	}
	// assistantLine は1件につき input 10 / cache_creation 20 / cache_read 30 / output 40 を持つ。
	if got.Usage.Input != 20 || got.Usage.CacheCreation != 40 || got.Usage.CacheRead != 60 || got.Usage.Output != 80 {
		t.Fatalf("トークンの合計が想定と違う: %+v", got.Usage)
	}
	if got.Signals[currentIssue] != "review" {
		t.Fatalf("同じ読み取りで表明を取れていない: %v", got.Signals)
	}
}

// TestReadTranscript_壊れた行があっても読める行だけを使う は、
// 1行の障害で残り全部を落とさないことを確かめる。
//
// 目的: 転写の途中を読んでも落ちないことを示す（読む前に 0.5 秒待つ規則の裏返し）。
// 与える情報: JSON として壊れた行が混ざった transcript。
// 成功条件: エラーにならず、読める行から表明を拾える。
func TestReadTranscript_壊れた行があっても読める行だけを使う(t *testing.T) {
	dir := t.TempDir()
	path := writeTranscript(t, dir, "session.jsonl", []any{
		typedUserLine("p1", "1回目"),
		assistantLine("req1", "CONTINUO-STATUS: review", false),
	})
	if err := appendLine(path, `{"type":"assistant","message":{`); err != nil {
		t.Fatalf("壊れた行を足せない: %v", err)
	}

	got, err := orchestrator.ReadTranscript(path, "p1", signalPrefix, currentIssue)
	if err != nil {
		t.Fatalf("壊れた行があるだけでエラーになった: %v", err)
	}
	if got.Signals[currentIssue] != "review" {
		t.Fatalf("読める行から表明を拾えていない: %v", got.Signals)
	}
}
