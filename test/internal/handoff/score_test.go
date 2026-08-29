// Package handoff_test は、複数の機械で担当を持ち回るときの判定を確かめる
// （設計 3-77 / 3-77a / 3-77b / 3-77c）。
//
// **確かめたいことは4つある。**
//
//	余裕値と判定スコアの式が設計のとおりであること
//	投稿しない3つの条件が、そのとおりに黙ること
//	同点の決着が「いちばん最初に投稿した機械」であること
//	担当を外す判定が、担当者の最後のコメントからの経過で決まること
package handoff_test

import (
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// testHost はテストで使う機械の名前である（架空の名前）。
const testHost = "test-host"

// at はテストで使う固定の時刻を返す。
//
// **その機械のタイムゾーンで書くことを確かめたいので、UTC にしない**（設計 3-77a）。
//
// 戻り値: 固定の時刻（+09:00）。
func at() time.Time {
	return time.Date(2026, 8, 29, 16, 45, 0, 0, time.FixedZone("JST", 9*60*60))
}

// snapshot は枠の写しを組み立てる。
//
// limits: `kind` と `percent` の組。
// 戻り値: 組み立てた写し。
func snapshot(limits ...[2]any) *ratelimit.Snapshot {
	out := &ratelimit.Snapshot{}
	for _, l := range limits {
		out.Limits = append(out.Limits, ratelimit.Limit{
			Kind:    l[0].(string),
			Percent: l[1].(int),
		})
	}
	return out
}

// 目的: 余裕値と判定スコアが設計 3-77 の式のとおりに出ることを確認する。
//
//	5時間余裕値 = 100 − 5時間の使用率 − 5時間マージン
//	1週間余裕値 = 100 − 1週間の使用率（いちばん大きいもの）− 1週間マージン
//	判定スコア  = 5時間余裕値 × 2 + 1週間余裕値
//
// 与える情報: 5時間の枠が 3%、1週間全体の枠が 74%、モデル別の枠が 80% の写しと、
// マージンが両方 10%。
// 成功条件: 5時間余裕値が 87、1週間余裕値が 10、判定スコアが 184 になること
// （**1週間はモデル別の 80% を採る。74% ではない**）。
func TestEvaluate_余裕値と判定スコアが式のとおりに出る(t *testing.T) {
	snap := snapshot(
		[2]any{handoff.LimitKindSession, 3},
		[2]any{handoff.LimitKindWeeklyAll, 74},
		[2]any{handoff.LimitKindWeeklyScoped, 80},
	)

	bid, skip := handoff.Evaluate(snap, true, handoff.Margins{FiveHour: 10, Weekly: 10}, 95, testHost, at())

	if skip != handoff.SkipNone {
		t.Fatalf("入札してよいはずなのに黙った: %v", skip)
	}
	if bid.FiveHour != 87 {
		t.Errorf("5時間余裕値が式と違う: got %d, want 87（100 − 3 − 10）", bid.FiveHour)
	}
	if bid.Weekly != 10 {
		t.Errorf("1週間余裕値が式と違う: got %d, want 10（100 − 80 − 10。モデル別の枠のほうが大きい）", bid.Weekly)
	}
	if bid.Score != 184 {
		t.Errorf("判定スコアが式と違う: got %d, want 184（87 × 2 + 10）", bid.Score)
	}
	if bid.Host != testHost {
		t.Errorf("機械の名前が入っていない: got %q, want %q", bid.Host, testHost)
	}
	if _, offset := bid.At.Zone(); offset != 9*60*60 {
		t.Errorf("時刻がその機械のタイムゾーンで入っていない（Z に直してはならない）: %s", bid.At.Format(time.RFC3339))
	}
}

// 目的: 1週間の使用率が「1週間全体の枠とモデル別の枠のいちばん大きいもの」であることを確認する。
//
// **モデル別の枠は一定量を使うまで現れない。**現れないものは判定に入らない。
//
// 与える情報: 1週間全体の枠だけがある写しと、モデル別の枠のほうが大きい写し。
// 成功条件: どちらもいちばん大きい値を返すこと。
func TestWeeklyPercent_いちばん大きい1週間の枠を採る(t *testing.T) {
	only := snapshot([2]any{handoff.LimitKindWeeklyAll, 16})
	if got, ok := handoff.WeeklyPercent(only); !ok || got != 16 {
		t.Errorf("モデル別の枠が無いときに1週間全体の枠を採っていない: got %d ok=%v, want 16 true", got, ok)
	}

	both := snapshot(
		[2]any{handoff.LimitKindWeeklyAll, 16},
		[2]any{handoff.LimitKindWeeklyScoped, 44},
	)
	if got, ok := handoff.WeeklyPercent(both); !ok || got != 44 {
		t.Errorf("いちばん大きい1週間の枠を採っていない: got %d ok=%v, want 44 true", got, ok)
	}
}

// 目的: 投稿しない3つの条件が、そのとおりに黙ることを確認する（設計 3-77 の表）。
//
// **黙る理由を取り違えてはならない。**枠を読めないのに「余裕値がマイナス」と記録すると、
// 人間は資格情報ではなく使用率を疑うことになる。
//
// 与える情報: 3つの条件それぞれを満たす写しと設定。
// 成功条件: それぞれの理由で黙ること。
func TestEvaluate_投稿しない3つの条件(t *testing.T) {
	cases := []struct {
		name              string
		snap              *ratelimit.Snapshot
		margins           handoff.Margins
		pauseAbovePercent int
		want              handoff.SkipReason
	}{
		{
			name:              "枠を読めない",
			snap:              nil,
			margins:           handoff.Margins{FiveHour: 10, Weekly: 10},
			pauseAbovePercent: 95,
			want:              handoff.SkipQuotaUnreadable,
		},
		{
			name:              "枠が1件も返らない",
			snap:              snapshot(),
			margins:           handoff.Margins{FiveHour: 10, Weekly: 10},
			pauseAbovePercent: 95,
			want:              handoff.SkipQuotaUnreadable,
		},
		{
			name: "枠の使い過ぎ",
			snap: snapshot(
				[2]any{handoff.LimitKindSession, 96},
				[2]any{handoff.LimitKindWeeklyAll, 10},
			),
			margins:           handoff.Margins{},
			pauseAbovePercent: 95,
			want:              handoff.SkipPauseThreshold,
		},
		{
			name: "余裕値がマイナス",
			snap: snapshot(
				[2]any{handoff.LimitKindSession, 92},
				[2]any{handoff.LimitKindWeeklyAll, 10},
			),
			margins:           handoff.Margins{FiveHour: 10, Weekly: 10},
			pauseAbovePercent: 95,
			want:              handoff.SkipNoHeadroom,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, skip := handoff.Evaluate(c.snap, true, c.margins, c.pauseAbovePercent, testHost, at())
			if skip != c.want {
				t.Errorf("黙る理由が違う: got %v, want %v", skip, c.want)
			}
		})
	}
}

// 目的: 判定スコアがいちばん大きい機械が勝つことを確認する（設計 3-77）。
//
// 与える情報: 判定スコアの違う入札3件。
// 成功条件: いちばん大きい入札が返ること。
func TestWinner_判定スコアがいちばん大きい機械が勝つ(t *testing.T) {
	base := at()
	bids := []handoff.Bid{
		{Host: "a", Score: 120, PostedAt: base},
		{Host: "b", Score: 190, PostedAt: base.Add(time.Minute)},
		{Host: "c", Score: 150, PostedAt: base.Add(2 * time.Minute)},
	}

	winner, ok := handoff.Winner(bids)
	if !ok {
		t.Fatal("入札があるのに勝者が決まらなかった")
	}
	if winner.Host != "b" {
		t.Errorf("判定スコアがいちばん大きい機械が勝っていない: got %q, want %q", winner.Host, "b")
	}
}

// 目的: 同点なら、いちばん最初に投稿した機械が勝つことを確認する（設計 3-77）。
//
// **比べるのは GitHub が付けた投稿の時刻である。**入札の JSON に書かれた `at` は
// 投稿者が自分で書いた値なので、**時計を戻せば必ず勝ててしまう。**
//
// 与える情報: 判定スコアが同じ入札3件（`at` は逆順に細工してある）。
// 成功条件: いちばん先に投稿された機械が勝つこと。
func TestWinner_同点なら最初に投稿した機械が勝つ(t *testing.T) {
	base := at()
	bids := []handoff.Bid{
		// **`at` は未来を指しているが、投稿は2番目である。**
		{Host: "late", Score: 190, At: base.Add(-time.Hour), PostedAt: base.Add(time.Minute)},
		{Host: "first", Score: 190, At: base.Add(time.Hour), PostedAt: base},
		{Host: "last", Score: 190, At: base, PostedAt: base.Add(2 * time.Minute)},
	}

	winner, ok := handoff.Winner(bids)
	if !ok {
		t.Fatal("入札があるのに勝者が決まらなかった")
	}
	if winner.Host != "first" {
		t.Errorf("同点の決着が投稿の時刻になっていない: got %q, want %q", winner.Host, "first")
	}
}

// 目的: 投稿の時刻まで同じでも、機械ごとに違う勝者を選ばないことを確認する。
//
// **決め手を最後まで用意しないと、2台が同じ issue を掴む。**
//
// 与える情報: 判定スコアも投稿の時刻も同じ入札2件を、並び順を変えて2回。
// 成功条件: どちらの並びでも同じ機械が勝つこと。
func TestWinner_全部同じでも同じ勝者を選ぶ(t *testing.T) {
	base := at()
	a := handoff.Bid{Host: "alpha", Score: 190, PostedAt: base}
	b := handoff.Bid{Host: "beta", Score: 190, PostedAt: base}

	first, _ := handoff.Winner([]handoff.Bid{a, b})
	second, _ := handoff.Winner([]handoff.Bid{b, a})
	if first.Host != second.Host {
		t.Fatalf("並び順で勝者が変わった: %q と %q", first.Host, second.Host)
	}
}

// 目的: まだ使っていない枠が応答に現れなくても、「読めなかった」として黙らないことを確認する
// （設計 3-77）。
//
// **モデル別の枠は一定量を使うまで現れない。**現れないことを「読めなかった」と扱うと、
// **週の頭にはどの機械も入札できなくなる。**
//
// 与える情報: 5時間の枠だけが載っている写し。
// 成功条件: 入札してよいと答え、1週間余裕値が「まだ使っていない」ぶんになること。
func TestEvaluate_使っていない枠が現れなくても黙らない(t *testing.T) {
	snap := snapshot([2]any{handoff.LimitKindSession, 3})

	bid, skip := handoff.Evaluate(snap, true, handoff.Margins{FiveHour: 10, Weekly: 10}, 95, testHost, at())

	if skip != handoff.SkipNone {
		t.Fatalf("使っていない枠が無いだけで黙った: %v", skip)
	}
	if bid.Weekly != 90 {
		t.Errorf("1週間余裕値が使用率0として計算されていない: got %d, want 90（100 − 0 − 10）", bid.Weekly)
	}
}

// 目的: 締め切りが「入札が1件も無い issue への最初の投稿」から数えられることを確認する
// （設計 3-77）。
//
// 与える情報: 3分ずれた入札2件と、締め切りまでの長さ3分。
// 成功条件: 締め切りが「いちばん古い入札の投稿時刻 + 3分」になること。
func TestDeadline_最初の入札から数える(t *testing.T) {
	base := at()
	bids := []handoff.Bid{
		{Host: "late", PostedAt: base.Add(3 * time.Minute)},
		{Host: "first", PostedAt: base},
	}

	got, ok := handoff.Deadline(bids, 3*time.Minute)
	if !ok {
		t.Fatal("入札があるのに締め切りが決まらなかった")
	}
	want := base.Add(3 * time.Minute)
	if !got.Equal(want) {
		t.Errorf("締め切りが最初の入札から数えられていない: got %s, want %s",
			got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

// 目的: 終わった回の入札を数え続けないことを確認する（設計 3-77d）。
//
// **勝った機械が担当者を書けないまま落ちると、その機械が永久に勝ち続ける。**
// 担当者がいないので hold の期限も効かず、issue は誰にも着手されないまま止まる。
//
// 与える情報: 3分の締め切りに対して、締め切りからさらに3分を過ぎた入札1件と、
// まだ回の中にある入札1件。
// 成功条件: 回が終わっていれば1件も返らず、回の中なら返ること。
func TestFreshBids_終わった回の入札は数えない(t *testing.T) {
	base := at()
	window := 3 * time.Minute
	bids := []handoff.Bid{{Host: "thinkpad", Score: 300, PostedAt: base}}

	// 締め切り（base+3分）からさらに3分。**回は終わっている。**
	if got := handoff.FreshBids(bids, base.Add(6*time.Minute+time.Second), window); len(got) != 0 {
		t.Errorf("終わった回の入札を数えている: %d 件", len(got))
	}
	// 締め切りは過ぎたが、決着の猶予の中である。
	if got := handoff.FreshBids(bids, base.Add(4*time.Minute), window); len(got) != 1 {
		t.Errorf("いまの回の入札を落としている: %d 件", len(got))
	}
	// 締め切りを待たない設定では回の区切りが無い。
	if got := handoff.FreshBids(bids, base.Add(365*24*time.Hour), 0); len(got) != 1 {
		t.Errorf("締め切りを待たない設定で入札を落としている: %d 件", len(got))
	}
}

// 目的: 締め切りを過ぎて届いた入札を、勝敗の判定に入れないことを確認する（設計 3-77）。
//
// 与える情報: 締め切りちょうどの入札と、締め切りの1秒後の入札。
// 成功条件: 締め切りちょうどは残り、1秒後は落ちること。
func TestBidsBefore_締め切りを過ぎた入札を落とす(t *testing.T) {
	base := at()
	deadline := base.Add(3 * time.Minute)
	bids := []handoff.Bid{
		{Host: "onTime", PostedAt: deadline},
		{Host: "late", PostedAt: deadline.Add(time.Second)},
	}

	got := handoff.BidsBefore(bids, deadline)
	if len(got) != 1 || got[0].Host != "onTime" {
		t.Fatalf("締め切りの扱いが違う: got %+v, want onTime だけ", got)
	}
}
