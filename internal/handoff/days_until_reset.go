package handoff

import (
	"time"

	"github.com/maimuzo/continuo/internal/ratelimit"
)

// MaxDaysUntilReset は、1週間の枠のリセットまでの残り日数の最大である（issue #324）。
//
// **1週間の枠の残りは7日を超えない。**ただし時計のずれで7日と数秒になりうるので、
// `DaysUntilReset` は7を超えた分をここへ寄せる。
// `tracker.provider.handoff.weekly_margin_percent_by_days_until_reset` のキーの上限でもある。
const MaxDaysUntilReset = 7

// DaysUntilReset は、1週間の枠のリセットまでの残り日数を数える（issue #324）。
//
// **残りを24時間単位で切り上げる。**残りがちょうど24時間なら1、24時間と1秒なら2である。
// 「1」を「リセット前の最後の24時間」と読めるようにするためである（人間の例の
// 「リセット前の最後の24時間は使い切る」）。
//
//	例: 今が金曜 03:00 で、リセットが金曜 09:00 → 残り6時間 → 1
//	    今が木曜 09:00 で、リセットが金曜 09:00 → 残り24時間 → 1
//	    今が木曜 08:59 で、リセットが金曜 09:00 → 残り24時間1分 → 2
//
// **7を超えたら7に寄せる。**
//
// resetsAt: 1週間全体の枠（`weekly_all`）のリセット時刻。
// now: 数える時刻。**呼ぶ側が判定に使っている時刻を渡す**（中で時計を引き直すと、
// 同じ判定の中でマージンを選ぶ時刻と判定する時刻がずれる）。
// 戻り値の1つ目: 残り日数（1〜7）。
// 戻り値の2つ目: 数えられたら true。リセット時刻が now 以前なら false（その期間はもう終わっている）。
func DaysUntilReset(resetsAt, now time.Time) (int, bool) {
	remaining := resetsAt.Sub(now)
	if remaining <= 0 {
		return 0, false
	}
	days := int(remaining / (24 * time.Hour))
	if remaining%(24*time.Hour) != 0 {
		days++
	}
	if days > MaxDaysUntilReset {
		days = MaxDaysUntilReset
	}
	return days, true
}

// WeeklyAllResetAt は、写しから1週間全体の枠（`weekly_all`）のリセット時刻を引く（issue #324）。
//
// **残り日数の起点は `weekly_all` だけである。**モデル別の枠（`weekly_scoped`）も同じ日数の
// マージンを使う。`Evaluate` は2つの1週間の使用率の最大に1つのマージンを引くので、
// 枠ごとに日数を分けるとその形が崩れる。
//
// **写しの `ResetsAt` が nil のもの（usage API が `resets_at: null` を返した期間）は数えない。**
// 写しは continuo が付けた仮の期限を nil にして渡す（orchestrator の `snapshotOf`）。
//
// snap: 判定に使っている写し（nil 可）。
// 戻り値: リセット時刻。分からなければ nil。
func WeeklyAllResetAt(snap *ratelimit.Snapshot) *time.Time {
	if snap == nil {
		return nil
	}
	for _, l := range snap.Limits {
		if matchesKind(l.Kind, weeklyAllKinds) && l.ResetsAt != nil {
			resetsAt := *l.ResetsAt
			return &resetsAt
		}
	}
	return nil
}

// weeklyAllKinds は1週間全体の枠の種別である（`weeklyKinds` と同じく、呼ぶたびに slice を作らない）。
var weeklyAllKinds = []string{LimitKindWeeklyAll}

// WeeklyMarginFor は、その時点で1週間の枠から引くマージン（%）を選ぶ（issue #324）。
//
// **残り日数の表に、その日の値が書いてあればそれを使う。**書いていない日・リセット時刻が
// 分からないとき（写しが無い・`weekly_all` が無い・`resets_at` が null・
// `rate_limit.source: none`）は `base`（`weekly_margin_percent`）を使う。
//
// base: `tracker.provider.handoff.weekly_margin_percent`。
// byDays: `tracker.provider.handoff.weekly_margin_percent_by_days_until_reset`（nil 可）。
// resetsAt: `weekly_all` のリセット時刻（nil 可）。
// now: 数える時刻。
// 戻り値の1つ目: 引くマージン（%）。
// 戻り値の2つ目: 表から選んだときの残り日数。表を使わなかったら 0。
func WeeklyMarginFor(base int, byDays map[int]int, resetsAt *time.Time, now time.Time) (int, int) {
	if len(byDays) == 0 || resetsAt == nil {
		return base, 0
	}
	days, ok := DaysUntilReset(*resetsAt, now)
	if !ok {
		return base, 0
	}
	margin, ok := byDays[days]
	if !ok {
		return base, 0
	}
	return margin, days
}
