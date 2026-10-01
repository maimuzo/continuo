package ratelimit_test

import (
	"testing"

	"github.com/maimuzo/continuo/internal/ratelimit"
)

// 目的: 「選んだ枠のうち、いちばん遅いリセット時刻」の取り出しを確認する（設計 3-27。issue #197）。
//
// **この値で「待っても上限以内に明けないか」を決める。**選んだ枠のうち1つでもリセット時刻が
// 空なら、時刻では決められない（呼び出し側は、余裕が無くなってからの経過で測る）。
// **空の枠を黙って飛ばすと、明ける時刻を見ていない枠があるのに「時刻で判定できた」と答える。**
//
// 与える情報: リセット時刻を持つ枠・持たない枠・述語に当たらない枠の組み合わせ。
// 成功条件: 全部が時刻を持てば、いちばん遅い時刻と真。1つでも空なら偽。1つも選ばれなければ偽。
// **述語に当たらない枠のリセット時刻は、空でも結果に効かない。**
func TestSnapshot_LatestResetForWaitLimitは選んだ枠が全部時刻を持つときだけ答える(t *testing.T) {
	early := mustTime(t, "2026-09-01T00:00:00Z")
	late := mustTime(t, "2026-09-03T00:00:00Z")

	both := &ratelimit.Snapshot{Limits: []ratelimit.Limit{
		{Kind: "weekly_all", Percent: 100, ResetsAt: early},
		{Kind: "weekly_scoped", Percent: 100, ResetsAt: late},
	}}
	if got, ok := both.LatestResetForWaitLimit(atFull); !ok || !got.Equal(*late) {
		t.Errorf("全部が時刻を持つ: got (%v, %v), want (%v, true)", got, ok, *late)
	}

	oneNil := &ratelimit.Snapshot{Limits: []ratelimit.Limit{
		{Kind: "weekly_all", Percent: 100, ResetsAt: early},
		{Kind: "weekly_scoped", Percent: 100, ResetsAt: nil},
	}}
	if _, ok := oneNil.LatestResetForWaitLimit(atFull); ok {
		t.Errorf("選んだ枠の1つがリセット時刻を持たないのに、時刻で答えた")
	}

	unselectedNil := &ratelimit.Snapshot{Limits: []ratelimit.Limit{
		{Kind: "weekly_all", Percent: 100, ResetsAt: early},
		{Kind: "weekly_scoped", Percent: 0, ResetsAt: nil},
	}}
	if got, ok := unselectedNil.LatestResetForWaitLimit(atFull); !ok || !got.Equal(*early) {
		t.Errorf("述語に当たらない枠の空のリセット時刻が効いた: got (%v, %v), want (%v, true)", got, ok, *early)
	}

	none := &ratelimit.Snapshot{Limits: []ratelimit.Limit{
		{Kind: "weekly_all", Percent: 30, ResetsAt: early},
	}}
	if _, ok := none.LatestResetForWaitLimit(atFull); ok {
		t.Errorf("1つも選ばれていないのに、時刻で答えた")
	}

	var nilSnap *ratelimit.Snapshot
	if _, ok := nilSnap.LatestResetForWaitLimit(atFull); ok {
		t.Errorf("写しが無いのに、時刻で答えた")
	}
}
