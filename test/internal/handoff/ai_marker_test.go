// Package handoff_test のうち、このファイルは
// 「人間ではなく機械が書いた」の印（`config.AIMarker`）を足しても、
// 持ち回りの読み取りが1つも壊れないことを固定する（設計 3-82。issue #245）。
//
// **なぜ要るか。**印は `internal/tracker` の `PostComment` が投稿の直前に足す。
// **足す場所は、本文の先頭に並ぶ印のいちばん後ろである。**
// 入札・hold・released のコメントは、本文が自分で印を持っているので、
// **印は「持ち回りの印」と JSON のあいだへ入る。**
// **そこを読む側が壊れると、担当が誰なのかを機械どうしが読めなくなる。**
package handoff_test

import (
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/handoff"
)

// 目的: 印を足した入札のコメントを、そのまま読めることを固定する（設計 3-82）。
//
// **`IsMarked` は本文の先頭が持ち回りの印であることを見ている。**
// **`ParseBid` は印の後ろの最初の `{` から最後の `}` を取る。**
// 印を先頭へ割り込ませると前者が、印が中括弧を持つと後者が壊れる。
//
// 与える情報: FormatBid の本文を config.WithAIMarker へ通したもの。
// 成功条件: IsMarked が真で、ParseBid が元の値を返すこと。
func TestWithAIMarker_入札のコメントをそのまま読める(t *testing.T) {
	posted := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	want := handoff.Bid{Author: "octocat-bot-a", FiveHour: 87, Weekly: 16, Score: 190, At: posted}

	body := config.WithAIMarker(handoff.FormatBid(want, 3*time.Minute))

	if !handoff.IsMarked(body) {
		t.Fatalf("印を足したら、入札のコメントとして数えられなくなりました:\n%s", body)
	}
	got, ok := handoff.ParseBid(body, want.Author, posted)
	if !ok {
		t.Fatalf("印を足したら、入札の JSON を読めなくなりました:\n%s", body)
	}
	if got.FiveHour != want.FiveHour || got.Weekly != want.Weekly || got.Score != want.Score {
		t.Errorf("読み取った入札が違います: got %+v, want %+v", got, want)
	}
}

// 目的: 印を足した hold のコメントを、そのまま読めることを固定する（設計 3-82）。
//
// **hold は「その担当者は機械である」ことの唯一の証拠である**（設計 3-77b）。
// **読めなくなると、continuo は自分が取った担当を人間のものと読み、二度と奪わない。**
//
// 与える情報: FormatHold の本文を config.WithAIMarker へ通したもの。
// 成功条件: IsMarked が真で、ParseHold が元の値を返すこと。
func TestWithAIMarker_holdのコメントをそのまま読める(t *testing.T) {
	want := handoff.Hold{
		Assignee: "octocat-bot-a",
		Branch:   "continuo/octocat/hello-world/188",
		At:       time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
	}

	body := config.WithAIMarker(handoff.FormatHold(want))

	if !handoff.IsMarked(body) {
		t.Fatalf("印を足したら、hold のコメントとして数えられなくなりました:\n%s", body)
	}
	got, ok := handoff.ParseHold(body)
	if !ok {
		t.Fatalf("印を足したら、hold の JSON を読めなくなりました:\n%s", body)
	}
	if got.Assignee != want.Assignee || got.Branch != want.Branch {
		t.Errorf("読み取った hold が違います: got %+v, want %+v", got, want)
	}
}

// 目的: 印を足した released のコメントを、そのまま読めることを固定する（設計 3-82）。
//
// 与える情報: FormatReleased の本文を config.WithAIMarker へ通したもの。
// 成功条件: IsMarked が真で、ParseReleased が元の値を返すこと。
func TestWithAIMarker_releasedのコメントをそのまま読める(t *testing.T) {
	want := handoff.Released{
		From:   "octocat-bot-a",
		Branch: "continuo/octocat/hello-world/188",
		At:     time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
	}

	body := config.WithAIMarker(handoff.FormatReleased(want))

	if !handoff.IsMarked(body) {
		t.Fatalf("印を足したら、released のコメントとして数えられなくなりました:\n%s", body)
	}
	got, ok := handoff.ParseReleased(body)
	if !ok {
		t.Fatalf("印を足したら、released の JSON を読めなくなりました:\n%s", body)
	}
	if got.From != want.From || got.Branch != want.Branch {
		t.Errorf("読み取った released が違います: got %+v, want %+v", got, want)
	}
}

// 目的: 進捗の印より後ろに置いたときだけ、途中経過として数えられることを固定する
// （設計 3-82。issue #178 の直しを壊さないため）。
//
// **`StartsAsProgressReport` は、本文の先頭から `<!--` で始まる行を辿って進捗の印を探す。**
// **あいだに印が1行増えても辿り続ける。**
// **だが進捗の印より前へ置くと、組み込みが書かせる形と食い違う。**
// 組み込みは `<!-- continuo:agent -->` の次の行に進捗の印を書かせており、
// **順序を入れ替えると、途中経過の報告が「この run の成果の報告」として数えられる。**
// そのとき、最後の報告を書かないまま終えても continuo は書き直させない。
//
// 与える情報: 印の並びが違う2つの本文。
// 成功条件: どちらも途中経過として数えられること（辿り続けることの確認）。
func TestWithAIMarker_進捗の報告は印を足しても数えられる(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			"組み込みが書かせる並び（agent → progress → ai）",
			"<!-- continuo:agent -->\n" + config.ProgressMarker + "\n" + config.AIMarker +
				"\nまだ作業中です。",
		},
		{
			"印を挟んだ並び（agent → ai → progress）",
			"<!-- continuo:agent -->\n" + config.AIMarker + "\n" + config.ProgressMarker +
				"\nまだ作業中です。",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !handoff.StartsAsProgressReport(tc.body) {
				t.Errorf("途中経過の報告として数えられません:\n%s", tc.body)
			}
			if !handoff.IsProgressReport(tc.body) {
				t.Errorf("18時間の死活の判定に数えられません:\n%s", tc.body)
			}
		})
	}
}
