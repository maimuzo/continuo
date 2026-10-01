package handoff_test

import (
	"testing"

	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// 目的: 「余裕が無い」と「使い切っている」の線を、種別ごとに確認する（設計 3-27 / 3-77j。issue #173 / #197）。
//
// **線は2本ある。**入札と手放しは「余裕値が0以下」（`Short` / `ShortWeekly`）、
// 枠待ちの印は「使用率100」（`Full`）である。**取り違えると、90%で止まるはずの入札が100%まで止まらないか、
// 100%でしか立たないはずの枠待ちの印が90%で立つ。**
//
// 与える情報: マージンは5時間・1週間とも10。種別と使用率の組。
// 成功条件: 余裕値 `100 − 使用率 − マージン` が0以下のときだけ `Short` が真。
// `ShortWeekly` は1週間の種別のときだけ真。`Full` は使用率100のときだけ真。
// **知らない種別は、どの述語でも偽**（`Evaluate` が見ない種別を数えると、線が2本に割れる）。
func TestShort_余裕が無いと使い切っているの線を種別ごとに確かめる(t *testing.T) {
	margins := handoff.Margins{FiveHour: 10, Weekly: 10}
	cases := []struct {
		name            string
		kind            string
		percent         int
		short, weekly   bool
		full, weeklyTyp bool
	}{
		{"5時間が89なら余裕がある", handoff.LimitKindSession, 89, false, false, false, false},
		{"5時間が90なら余裕が無い", handoff.LimitKindSession, 90, true, false, false, false},
		{"5時間が100なら使い切っている", handoff.LimitKindSession, 100, true, false, true, false},
		{"1週間の全体が89なら余裕がある", handoff.LimitKindWeeklyAll, 89, false, false, false, true},
		{"1週間の全体が90なら余裕が無い", handoff.LimitKindWeeklyAll, 90, true, true, false, true},
		{"1週間のモデル別が90なら余裕が無い", handoff.LimitKindWeeklyScoped, 90, true, true, false, true},
		{"1週間のモデル別が100なら使い切っている", handoff.LimitKindWeeklyScoped, 100, true, true, true, true},
		{"知らない種別は100でも数えない", "something_new", 100, false, false, false, false},
	}
	for _, c := range cases {
		l := ratelimit.Limit{Kind: c.kind, Percent: c.percent}
		if got := handoff.Short(margins)(l); got != c.short {
			t.Errorf("%s: Short = %v, want %v", c.name, got, c.short)
		}
		if got := handoff.ShortWeekly(margins)(l); got != c.weekly {
			t.Errorf("%s: ShortWeekly = %v, want %v", c.name, got, c.weekly)
		}
		if got := handoff.Full()(l); got != c.full {
			t.Errorf("%s: Full = %v, want %v", c.name, got, c.full)
		}
		if got := handoff.IsWeeklyKind(c.kind); got != c.weeklyTyp {
			t.Errorf("%s: IsWeeklyKind = %v, want %v", c.name, got, c.weeklyTyp)
		}
	}
}

// 目的: ログへ出す「止まる使用率」が、マージンから正しく出ることを確認する（設計 3-77j。issue #173）。
//
// 与える情報: マージン 0 / 10 / 99。
// 成功条件: `100 − マージン` が返ること。**`Short` が真になり始める使用率と一致すること。**
func TestThresholdPercent_止まる使用率はマージンから決まる(t *testing.T) {
	for _, margin := range []int{0, 10, 99} {
		got := handoff.ThresholdPercent(margin)
		if want := 100 - margin; got != want {
			t.Errorf("ThresholdPercent(%d) = %d, want %d", margin, got, want)
		}
		short := handoff.Short(handoff.Margins{FiveHour: margin, Weekly: margin})
		at := ratelimit.Limit{Kind: handoff.LimitKindSession, Percent: got}
		below := ratelimit.Limit{Kind: handoff.LimitKindSession, Percent: got - 1}
		if !short(at) {
			t.Errorf("マージン %d: 使用率 %d で余裕が無いと判定されない", margin, got)
		}
		if got > 0 && short(below) {
			t.Errorf("マージン %d: 使用率 %d で余裕が無いと判定された", margin, got-1)
		}
	}
}
