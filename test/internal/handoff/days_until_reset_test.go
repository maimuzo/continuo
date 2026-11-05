package handoff_test

import (
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// 目的: 1週間の枠のリセットまでの残り日数を、24時間単位で切り上げて数えることを確かめる（issue #324）。
//
// **人間の例が入っている。**「今日が金曜日3:00AMで、週間レートリミットのリセットが金曜の9:00AMの時」は1日目である。
//
// 与える情報: 今の時刻とリセットの時刻の組。
// 成功条件: 表の日数を返し、リセットを過ぎていれば数えられない（false）と答えること。
func TestDaysUntilReset_24時間単位で切り上げて1から7に収める(t *testing.T) {
	// 2026-11-06 は金曜日である。
	friday0900 := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		now  time.Time
		days int
		ok   bool
	}{
		{"金曜3時でリセットが金曜9時なら1（残り6時間）", time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC), 1, true},
		{"残り1秒なら1", friday0900.Add(-time.Second), 1, true},
		{"木曜9時ちょうど（残り24時間）なら1", friday0900.Add(-24 * time.Hour), 1, true},
		{"残り24時間と1秒なら2", friday0900.Add(-24*time.Hour - time.Second), 2, true},
		{"木曜8時59分なら2", friday0900.Add(-24*time.Hour - time.Minute), 2, true},
		{"残りちょうど7日なら7", friday0900.Add(-7 * 24 * time.Hour), 7, true},
		{"残り7日と数秒でも7に寄せる", friday0900.Add(-7*24*time.Hour - 5*time.Second), 7, true},
		{"リセットちょうどは数えない", friday0900, 0, false},
		{"リセットを過ぎていれば数えない", friday0900.Add(time.Minute), 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			days, ok := handoff.DaysUntilReset(friday0900, c.now)
			if days != c.days || ok != c.ok {
				t.Errorf("DaysUntilReset = (%d, %v)（want (%d, %v)）", days, ok, c.days, c.ok)
			}
		})
	}
}

// 目的: 写しから `weekly_all` のリセット時刻だけを引き、null の期間とモデル別の枠は起点にしないことを確かめる（issue #324）。
//
// 与える情報: 写しの組（nil・`weekly_all` が無い・`weekly_all` の `resets_at` が null・モデル別の枠が先に並ぶ）。
// 成功条件: `weekly_all` の本物のリセット時刻だけを返し、それ以外は nil を返すこと。
func TestWeeklyAllResetAt_weekly_allの本物のリセット時刻だけを起点にする(t *testing.T) {
	allReset := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	scopedReset := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		snap *ratelimit.Snapshot
		want *time.Time
	}{
		{"写しが無い", nil, nil},
		{"weekly_all が無い", &ratelimit.Snapshot{Limits: []ratelimit.Limit{
			{Kind: handoff.LimitKindWeeklyScoped, Percent: 30, ResetsAt: &scopedReset},
		}}, nil},
		{"weekly_all の resets_at が null", &ratelimit.Snapshot{Limits: []ratelimit.Limit{
			{Kind: handoff.LimitKindWeeklyAll, Percent: 0},
		}}, nil},
		{"モデル別の枠が先に並んでも weekly_all を引く", &ratelimit.Snapshot{Limits: []ratelimit.Limit{
			{Kind: handoff.LimitKindWeeklyScoped, Percent: 30, ResetsAt: &scopedReset},
			{Kind: handoff.LimitKindWeeklyAll, Percent: 40, ResetsAt: &allReset},
		}}, &allReset},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := handoff.WeeklyAllResetAt(c.snap)
			switch {
			case c.want == nil && got != nil:
				t.Errorf("nil のはずが %v", *got)
			case c.want != nil && (got == nil || !got.Equal(*c.want)):
				t.Errorf("リセット時刻が %v（want %v）", got, *c.want)
			}
		})
	}
}

// 目的: 残り日数の表にその日の値があればそれを使い、無い日とリセット時刻が分からないときは
// `weekly_margin_percent` を使うことを確かめる（issue #324）。
//
// 与える情報: 既定のマージン 10、表 {1: 0, 2: 5}、リセット時刻と今の時刻の組。
// 成功条件: 表のとおりのマージンと、表から選んだときだけ残り日数を返すこと。
func TestWeeklyMarginFor_表に書いた日だけ上書きしそれ以外は既定値(t *testing.T) {
	reset := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	table := map[int]int{1: 0, 2: 5}
	cases := []struct {
		name     string
		byDays   map[int]int
		resetsAt *time.Time
		now      time.Time
		margin   int
		days     int
	}{
		{"金曜3時は1日目の0", table, &reset, time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC), 0, 1},
		{"残り30時間は2日目の5", table, &reset, reset.Add(-30 * time.Hour), 5, 2},
		{"残り50時間は表に無いので既定の10", table, &reset, reset.Add(-50 * time.Hour), 10, 0},
		{"リセット時刻が分からなければ既定の10", table, nil, reset.Add(-time.Hour), 10, 0},
		{"リセットを過ぎていれば既定の10", table, &reset, reset.Add(time.Hour), 10, 0},
		{"表が無ければ既定の10", nil, &reset, reset.Add(-time.Hour), 10, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			margin, days := handoff.WeeklyMarginFor(10, c.byDays, c.resetsAt, c.now)
			if margin != c.margin || days != c.days {
				t.Errorf("WeeklyMarginFor = (%d, %d)（want (%d, %d)）", margin, days, c.margin, c.days)
			}
		})
	}
}
