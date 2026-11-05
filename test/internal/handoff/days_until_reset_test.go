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

// 目的: 1週間の枠ごとに、その枠自身のリセットまでの残り日数でマージンを選び、入札（`Evaluate`）・余裕が無いか（`Short`）・
// マージンを切ったか（`MarginCut`）の3つが同じ値で判定することを確かめる（issue #324。実装レビュー1周目の HIGH）。
//
// **全体の枠（`weekly_all`）の最後の日の値を、リセットが数日先のモデル別の枠（`weekly_scoped`）に当ててはならない。**
// 当てると、人間が残したいそのモデルの枠を continuo が使い切りうる。
//
// 与える情報: 既定のマージン 10、表 {1: 0}。全体の枠は残り6時間（1日目）で使用率95、モデル別の枠は残り5日で使用率95。
// 成功条件: 全体の枠はマージン0で余裕があり（切っていない）、モデル別の枠はマージン10で余裕が無い（切った）。
// 入札は1週間余裕値が0以下なので黙る。モデル別の枠のリセット時刻が null なら、その枠は既定の10を使う。
func TestMargins_1週間の枠ごとに自分のリセットまでの日数で値を選ぶ(t *testing.T) {
	now := time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC)
	allReset := now.Add(6 * time.Hour)
	scopedReset := now.Add(5 * 24 * time.Hour)
	m := handoff.Margins{FiveHour: 10, Weekly: 10, WeeklyByDaysUntilReset: map[int]int{1: 0}, At: now}
	all := ratelimit.Limit{Kind: handoff.LimitKindWeeklyAll, Percent: 95, ResetsAt: &allReset}
	scoped := ratelimit.Limit{Kind: handoff.LimitKindWeeklyScoped, Percent: 95, ResetsAt: &scopedReset}
	scopedNull := ratelimit.Limit{Kind: handoff.LimitKindWeeklyScoped, Percent: 95}

	if got := m.WeeklyFor(all); got != 0 {
		t.Errorf("全体の枠のマージンが %d（want 0。残り6時間なので1日目）", got)
	}
	if got := m.WeeklyFor(scoped); got != 10 {
		t.Errorf("モデル別の枠のマージンが %d（want 10。残り5日は表に無い）", got)
	}
	if got := m.WeeklyFor(scopedNull); got != 10 {
		t.Errorf("リセット時刻が null のモデル別の枠のマージンが %d（want 10）", got)
	}
	if handoff.Short(m)(all) || handoff.MarginCut(m)(all) {
		t.Error("全体の枠はマージン0で余裕値5なのに、余裕が無い・切ったと答えた")
	}
	if !handoff.Short(m)(scoped) || !handoff.MarginCut(m)(scoped) {
		t.Error("モデル別の枠はマージン10で余裕値-5なのに、余裕がある・切っていないと答えた")
	}

	snap := &ratelimit.Snapshot{Limits: []ratelimit.Limit{all, scoped}}
	if _, skip := handoff.Evaluate(snap, true, m, now); skip != handoff.SkipNoHeadroom {
		t.Errorf("モデル別の枠に余裕が無いのに入札を黙らない: %v", skip)
	}
	onlyAll := &ratelimit.Snapshot{Limits: []ratelimit.Limit{all}}
	bid, skip := handoff.Evaluate(onlyAll, true, m, now)
	if skip != handoff.SkipNone || bid.Weekly != 5 {
		t.Errorf("全体の枠だけなら1週間余裕値は5で入札する（got weekly=%d skip=%v）", bid.Weekly, skip)
	}
}

// 目的: 表を書いていなければ、`Evaluate` の1週間余裕値が今までどおり `100 − いちばん大きい1週間の使用率 − Weekly` になることを
// 確かめる（issue #324 で枠ごとの最小へ書き換えたため）。
//
// 与える情報: 表なしのマージン 10。1週間の枠が2つ（使用率30と55）の写しと、1週間の枠が無い写し。
// 成功条件: 1週間余裕値が 35 と 90 であること。
func TestEvaluate_表が無ければ1週間余裕値は今までどおり(t *testing.T) {
	now := time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC)
	reset := now.Add(50 * time.Hour)
	m := handoff.Margins{FiveHour: 10, Weekly: 10}
	two := &ratelimit.Snapshot{Limits: []ratelimit.Limit{
		{Kind: handoff.LimitKindSession, Percent: 20, ResetsAt: &reset},
		{Kind: handoff.LimitKindWeeklyAll, Percent: 30, ResetsAt: &reset},
		{Kind: handoff.LimitKindWeeklyScoped, Percent: 55},
	}}
	if bid, skip := handoff.Evaluate(two, true, m, now); skip != handoff.SkipNone || bid.Weekly != 35 {
		t.Errorf("1週間余裕値が %d（want 35）skip=%v", bid.Weekly, skip)
	}
	none := &ratelimit.Snapshot{Limits: []ratelimit.Limit{{Kind: handoff.LimitKindSession, Percent: 20, ResetsAt: &reset}}}
	if bid, skip := handoff.Evaluate(none, true, m, now); skip != handoff.SkipNone || bid.Weekly != 90 {
		t.Errorf("1週間の枠が無いときの1週間余裕値が %d（want 90）skip=%v", bid.Weekly, skip)
	}
}
