package handoff_test

import (
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// 目的: 「マージンを切った」と「Claude Code が動けない」の線を、種別とマージンごとに確かめる（issue #262。設計 3-27a）。
//
// **マージンが0の期間は「切った」と呼ばない**（人間の決定「マージンが0%ではない場合は、マージンが切れたら作業を
// 切り上げる」）。**モデル別の1週間の期間の100%は「動けない」に数えない**（continuo は使っているモデルを知らない）。
// その100%は、1週間の期間のマージンが1以上なら「切った」に入る。
//
// 与える情報: マージンの組と、種別と使用率の組。
// 成功条件: `MarginCut` と `CannotWork` が表のとおりに答えること。
func TestMarginCut_マージン0の期間とモデル別の期間の扱い(t *testing.T) {
	cases := []struct {
		name    string
		margins handoff.Margins
		kind    string
		percent int
		cut     bool
		cannot  bool
	}{
		{"5時間が89でマージン10なら切っていない", handoff.Margins{FiveHour: 10, Weekly: 10}, handoff.LimitKindSession, 89, false, false},
		{"5時間が90でマージン10なら切った", handoff.Margins{FiveHour: 10, Weekly: 10}, handoff.LimitKindSession, 90, true, false},
		{"5時間が100でマージン10なら切ったうえに動けない", handoff.Margins{FiveHour: 10, Weekly: 10}, handoff.LimitKindSession, 100, true, true},
		{"5時間が100でもマージン0なら切ったとは呼ばない", handoff.Margins{FiveHour: 0, Weekly: 10}, handoff.LimitKindSession, 100, false, true},
		{"1週間の全体が100でマージン0なら動けないだけ", handoff.Margins{FiveHour: 10, Weekly: 0}, handoff.LimitKindWeeklyAll, 100, false, true},
		{"モデル別が100でマージン10なら切ったが動けなくはない", handoff.Margins{FiveHour: 10, Weekly: 10}, handoff.LimitKindWeeklyScoped, 100, true, false},
		{"モデル別が100でマージン0なら何もしない", handoff.Margins{FiveHour: 10, Weekly: 0}, handoff.LimitKindWeeklyScoped, 100, false, false},
		{"知らない種別は100でも数えない", handoff.Margins{FiveHour: 10, Weekly: 10}, "something_new", 100, false, false},
	}
	for _, c := range cases {
		l := ratelimit.Limit{Kind: c.kind, Percent: c.percent}
		if got := handoff.MarginCut(c.margins)(l); got != c.cut {
			t.Errorf("%s: MarginCut = %v, want %v", c.name, got, c.cut)
		}
		if got := handoff.CannotWork()(l); got != c.cannot {
			t.Errorf("%s: CannotWork = %v, want %v", c.name, got, c.cannot)
		}
	}
}

// 目的: 行き先の表を、走っている run と着手の前の両方で確かめる（issue #262。設計 3-27a の行き先の表）。
//
// **1週間の期間は、待つ上限が0 → 読めている期間のどれかで上限を超える → どれか1つでも読めない → 上限以内 の順に当てる。**
// **上限を超えると分かっている期間があれば、読めない期間があっても「超える」を使う**（人間の「blockedに戻すで良い」は、
// 読めないときだけを訊いた答え）。**着手の前に読めなければ Blocked**（run が無いと経過を測る相手が無い）。
//
// 与える情報: 待つ上限は300分。マージンは5時間・1週間とも10（着手の前の動けない期間だけ、マージン0の表も使う）。
// 成功条件: 表のとおりの行き先が返ること。使用率を読めていない（nil）なら DestNone。
func TestDecide_行き先の表(t *testing.T) {
	now := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	soon := now.Add(2 * time.Hour)
	far := now.Add(72 * time.Hour)
	lim := func(kind string, percent int, resets *time.Time) ratelimit.Limit {
		return ratelimit.Limit{Kind: kind, Percent: percent, ResetsAt: resets}
	}
	m10 := handoff.Margins{FiveHour: 10, Weekly: 10}
	cases := []struct {
		name    string
		margins handoff.Margins
		limit   time.Duration
		limits  []ratelimit.Limit
		running handoff.Destination
		before  handoff.Destination
	}{
		{"どの期間も切っていない", m10, 300 * time.Minute,
			[]ratelimit.Limit{lim(handoff.LimitKindSession, 50, &soon), lim(handoff.LimitKindWeeklyAll, 50, &far)},
			handoff.DestNone, handoff.DestNone},
		{"5時間の期間だけ切った", m10, 300 * time.Minute,
			[]ratelimit.Limit{lim(handoff.LimitKindSession, 93, &soon), lim(handoff.LimitKindWeeklyAll, 50, &far)},
			handoff.DestBlocked, handoff.DestBlocked},
		{"1週間の期間を切り、待つ上限が0", m10, 0,
			[]ratelimit.Limit{lim(handoff.LimitKindWeeklyAll, 93, &far)},
			handoff.DestBlocked, handoff.DestBlocked},
		{"1週間の期間を切り、リセットまでが上限以内", m10, 300 * time.Minute,
			[]ratelimit.Limit{lim(handoff.LimitKindWeeklyAll, 93, &soon)},
			handoff.DestBlocked, handoff.DestBlocked},
		{"1週間の期間を切り、リセットまでが上限を超える", m10, 300 * time.Minute,
			[]ratelimit.Limit{lim(handoff.LimitKindWeeklyAll, 93, &far)},
			handoff.DestAwait, handoff.DestRelease},
		{"1週間の期間を切り、リセットの時刻を読めない", m10, 300 * time.Minute,
			[]ratelimit.Limit{lim(handoff.LimitKindWeeklyScoped, 93, nil)},
			handoff.DestAwait, handoff.DestBlocked},
		{"上限を超える期間と読めない期間が両方ある", m10, 300 * time.Minute,
			[]ratelimit.Limit{lim(handoff.LimitKindWeeklyAll, 93, &far), lim(handoff.LimitKindWeeklyScoped, 95, nil)},
			handoff.DestAwait, handoff.DestRelease},
		{"5時間と1週間の両方を切ったら1週間の行を使う", m10, 300 * time.Minute,
			[]ratelimit.Limit{lim(handoff.LimitKindSession, 95, &soon), lim(handoff.LimitKindWeeklyAll, 93, &far)},
			handoff.DestAwait, handoff.DestRelease},
		{"マージン0の5時間が100なら、着手の前だけ当てる", handoff.Margins{FiveHour: 0, Weekly: 10}, 300 * time.Minute,
			[]ratelimit.Limit{lim(handoff.LimitKindSession, 100, &soon)},
			handoff.DestNone, handoff.DestBlocked},
		{"マージン0のモデル別が100なら、どちらでも当てない", handoff.Margins{FiveHour: 10, Weekly: 0}, 300 * time.Minute,
			[]ratelimit.Limit{lim(handoff.LimitKindWeeklyScoped, 100, nil)},
			handoff.DestNone, handoff.DestNone},
	}
	for _, c := range cases {
		snap := &ratelimit.Snapshot{Limits: c.limits}
		if got := handoff.Decide(snap, c.margins, c.limit, now, handoff.PhaseRunning); got != c.running {
			t.Errorf("%s: 走っている run = %v, want %v", c.name, got, c.running)
		}
		if got := handoff.Decide(snap, c.margins, c.limit, now, handoff.PhaseBeforeDispatch); got != c.before {
			t.Errorf("%s: 着手の前 = %v, want %v", c.name, got, c.before)
		}
	}
	if got := handoff.Decide(nil, m10, 300*time.Minute, now, handoff.PhaseBeforeDispatch); got != handoff.DestNone {
		t.Errorf("使用率を読めていないのに止めています: %v（DestNone を期待）", got)
	}
}

// 目的: run を始めずに手放したときの本文が、「外された側」の本文にならないことを確かめる（issue #262）。
//
// **`FormatReleased` は未知の理由の値を「push しないでください」の本文にする。**新しい理由の値の case を
// 落とすと、手放した本人がそう書くことになる。
//
// 与える情報: 理由の値 `weekly_wait_limit_not_started`。
// 成功条件: 本文に理由の値が JSON で載り、「push しないでください」の本文と違うこと。
func TestFormatReleased_runを始めずに手放した本文(t *testing.T) {
	at := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	notStarted := handoff.FormatReleased(handoff.Released{From: "octocat", Branch: "42-x", At: at,
		Reason: handoff.ReleaseReasonWeeklyWaitLimitNotStarted})
	external := handoff.FormatReleased(handoff.Released{From: "octocat", Branch: "42-x", At: at})
	if notStarted == external {
		t.Fatalf("run を始めずに手放した本文が、外された側の本文と同じです:\n%s", notStarted)
	}
	if want := `"reason":"weekly_wait_limit_not_started"`; !strings.Contains(notStarted, want) {
		t.Errorf("本文に %s がありません:\n%s", want, notStarted)
	}
}
