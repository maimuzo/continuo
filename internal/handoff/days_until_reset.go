package handoff

import (
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// MaxDaysUntilReset は、1週間の枠のリセットまでの残り日数の最大である（issue #324）。
//
// **1週間の枠の残りは7日を超えない。**ただし時計のずれで7日と数秒になりうるので、
// `DaysUntilReset` は7を超えた分をここへ寄せる。
// **値は `internal/config` の1か所が持つ**（検査が同じ上限でキーを弾く。config は handoff を読めないので、
// こちらが config を読む）。
const MaxDaysUntilReset = config.MaxWeeklyMarginDaysUntilReset

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
// resetsAt: その1週間の枠のリセット時刻。
// now: 数える時刻。**呼ぶ側が判定に使っている時刻を渡す。**
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

// WeeklyMarginFor は、1週間の枠1つから引くマージン（%）を選ぶ（issue #324）。
//
// **残り日数は、その枠自身のリセット時刻から数える。**全体の枠（`weekly_all`）とモデル別の枠
// （`weekly_scoped`）はリセットの時刻が違いうる。全体の枠の最後の日に書いた小さい値を、
// リセットが数日先のモデル別の枠にも当てると、人間が残したいそのモデルの枠を continuo が使い切りうる
// （実装レビュー1周目の HIGH）。
//
// **残り日数の表に、その日の値が書いてあればそれを使う。**書いていない日・その枠のリセット時刻が
// 分からないとき（`resets_at` が null。写しは continuo が付けた仮の期限も nil にして渡す）は
// `base`（`weekly_margin_percent`）を使う。
//
// base: `tracker.provider.handoff.weekly_margin_percent`。
// byDays: `tracker.provider.handoff.weekly_margin_percent_by_days_until_reset`（nil 可）。
// resetsAt: その枠のリセット時刻（nil 可）。
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

// For は、その枠から引くマージン（%）を返す（issue #324）。
//
// 5時間の枠は `FiveHour`、1週間の枠は `WeeklyFor` である。**知らない種別は `FiveHour` を返すが、
// 判定（`Short`）は知らない種別を数えないので効かない。**
//
// l: 枠。
// 戻り値: 引くマージン（%）。
func (m Margins) For(l ratelimit.Limit) int {
	if IsWeeklyKind(l.Kind) {
		return m.WeeklyFor(l)
	}
	return m.FiveHour
}

// WeeklyFor は、1週間の枠1つから引くマージン（%）を返す（issue #324。`WeeklyMarginFor` を参照）。
//
// l: 1週間の枠。
// 戻り値: 引くマージン（%）。
func (m Margins) WeeklyFor(l ratelimit.Limit) int {
	margin, _ := m.WeeklyDaysFor(l)
	return margin
}

// WeeklyDaysFor は、1週間の枠1つから引くマージン（%）と、表から選んだときの残り日数を返す（issue #324）。
//
// **ログが「表の何日目の値か」を出すために、日数も返す。**選び方を2か所に持たないためである。
//
// l: 1週間の枠。
// 戻り値の1つ目: 引くマージン（%）。
// 戻り値の2つ目: 表から選んだときの残り日数。表を使わなかったら 0。
func (m Margins) WeeklyDaysFor(l ratelimit.Limit) (int, int) {
	return WeeklyMarginFor(m.Weekly, m.WeeklyByDaysUntilReset, l.ResetsAt, m.At)
}
