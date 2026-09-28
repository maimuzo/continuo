// Package ratelimit は、Claude の使用率（5時間枠と7日枠の使用率とリセット時刻）の写しを運ぶ型を
// 持つ（docs/plans/continuo_design.md 3-27 / issue #284）。
//
// **値の出どころはステータスラインである。**continuo が起動する Claude Code のステータスラインが
// 運ぶ `rate_limits` を、`continuo statusline` が `sl.sock` へ送り、orchestrator が保管する
// （internal/orchestrator/quota.go）。**usage API は読まない**（issue #284 で読み取り一式を消した）。
//
// **`rate_limit.source: none` のときは使用率を読まない。**入札は使用率を0として参加する。
package ratelimit

import (
	"time"
)

// SourceNone は rate_limit.source が「使用率を読まない」を意味する値である。
const SourceNone = "none"

// SourceStatusline は rate_limit.source が「ステータスラインから使用率を受ける」を意味する値である
// （issue #284）。**internal/config の RateLimitSourceStatusline と同じ文字列である。**
const SourceStatusline = "statusline"

// Limit は1つの期間（5時間か7日）の使用率である。
type Limit struct {
	// Kind は枠の種別である（"session" = 5時間 / "weekly_all" = 7日）。
	// **"weekly_scoped"（モデル別の週次の上限）は、ステータスラインが運ばないので入らない**（issue #284）。
	Kind string `json:"kind"`
	// Percent は使用率（整数の百分率）である。
	Percent int `json:"percent"`
	// ResetsAt は枠がリセットされる時刻である。**null のことがある**ので nil を許す。
	ResetsAt *time.Time `json:"resets_at"`
	// Severity は provider が付ける深刻さである。
	//
	// **continuo はこの値を見ない**（設計 3-27）。上限を示す値が何かを実測できていない。
	// 記録とダッシュボードのためだけに保持する。
	Severity string `json:"severity"`
}

// Snapshot は、ある時点で保管している使用率の写しである（issue #284）。
type Snapshot struct {
	// Limits は返ってきた枠の一覧である。
	Limits []Limit
	// FetchedAt は写しを取った時刻である。
	FetchedAt time.Time
}

// MaxPercent は枠の中でいちばん高い使用率を返す。
//
// 戻り値: 使用率の最大値。枠が1件も無ければ 0。
func (s *Snapshot) MaxPercent() int {
	if s == nil {
		return 0
	}
	max := 0
	for _, l := range s.Limits {
		if l.Percent > max {
			max = l.Percent
		}
	}
	return max
}

// AtFullPercent は、使い切っている（`percent` が 100 に達している）枠が1つでもあるかを返す
// （設計 3-27 の「この run は枠待ちである」の条件その1）。
//
// 戻り値: 100 に達している枠があれば true。
func (s *Snapshot) AtFullPercent() bool {
	if s == nil {
		return false
	}
	for _, l := range s.Limits {
		if l.Percent >= 100 {
			return true
		}
	}
	return false
}

// LatestResetOfFullLimits は、使い切っている枠のうち `resets_at` がいちばん遅いものを返す
// （設計 3-27 の「どの枠の時刻を見るか」）。
//
// **`resets_at` が null の枠は判定から外す。**`weekly_scoped` も、モデルを判別せず
// そのまま見る（continuo は Claude Code が使うモデルを知らない）。
//
// 戻り値の1つ目: いちばん遅いリセット時刻。
// 戻り値の2つ目: 該当する枠が1つでもあれば true。
func (s *Snapshot) LatestResetOfFullLimits() (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	var latest time.Time
	found := false
	for _, l := range s.Limits {
		if l.Percent < 100 || l.ResetsAt == nil {
			continue
		}
		if !found || l.ResetsAt.After(latest) {
			latest = *l.ResetsAt
			found = true
		}
	}
	return latest, found
}
