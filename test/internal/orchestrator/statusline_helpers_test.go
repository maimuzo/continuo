package orchestrator_test

import (
	"time"

	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/statuslineserver"
)

// 使用率の保管値へ行を入れるための補助である（issue #284）。
//
// **行は `Orchestrator.OnStatusline` で直に入れる。**本番では `continuo statusline` が
// `sl.sock` へ送った1行を、statusline の受け口がこの関数へ渡す。受け口そのものは
// test/internal/statuslineserver が確かめる。

// slWin は1つの期間の値を作る。
//
// percent: used_percentage。
// resetsAt: 期間が切り替わる時刻（Unix 秒に丸める）。
// 戻り値: 期間の値。
func slWin(percent float64, resetsAt time.Time) *statuslineserver.Window {
	return &statuslineserver.Window{UsedPercentage: percent, ResetsAt: resetsAt.Unix()}
}

// slLine は1行を作る。
//
// session: セッションの ID。
// apiMs: cost.total_api_duration_ms。
// five: 5時間の期間（nil なら持たない）。
// seven: 7日の期間（nil なら持たない）。
// 戻り値: 1行。five も seven も nil なら `rate_limits` が null の行になる。
func slLine(session string, apiMs int64, five, seven *statuslineserver.Window) statuslineserver.Line {
	return statuslineserver.Line{SessionID: session, APIMs: apiMs, FiveHour: five, SevenDay: seven}
}

// feedFreshQuota は、保管値を「新しい」状態にする（issue #284）。
//
// **同じセッションから2行を送る。**1行目は初めて見るセッションの最初の行なので、基準を
// 作るだけで新しさの時刻を進めない。2行目は `api_ms` が増えた `rate_limits` を持つ行なので、
// 新しい応答の行として新しさの時刻を進める。
//
// orc: 行を入れる orchestrator。
// session: セッションの ID（テストの中で他の行と混ざらない名前にする）。
// now: いまの時刻（orchestrator の時計と同じもの）。
// fivePct: 5時間の期間の使用率（resets_at は2時間後）。
// sevenPct: 7日の期間の使用率（resets_at は3日後）。
func feedFreshQuota(orc *orchestrator.Orchestrator, session string, now time.Time, fivePct, sevenPct float64) {
	five := slWin(fivePct, now.Add(2*time.Hour))
	seven := slWin(sevenPct, now.Add(72*time.Hour))
	orc.OnStatusline(slLine(session, 100, five, seven))
	orc.OnStatusline(slLine(session, 200, five, seven))
}
