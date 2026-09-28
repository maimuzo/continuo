// Package ratelimit_test は internal/ratelimit（使用率の写しを運ぶ型）を検証する。
//
// **internal/ratelimit は値を取りに行かない。**値はステータスラインから届き、
// orchestrator が保管して `Snapshot` に組み立てる（issue #284）。
// ここで見るのは、組み立てた `Snapshot` を突き合わせる判定（設計 3-27）だけである。
package ratelimit_test

import (
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/ratelimit"
)

// mustTime は RFC3339 の文字列を time.Time にする（テスト用）。
func mustTime(t *testing.T, s string) *time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("時刻を解析できません（%s）: %v", s, err)
	}
	return &v
}

// 目的: 複数の枠を突き合わせる判定（MaxPercent / AtFullPercent /
// LatestResetOfFullLimits）が設計 3-27 のとおりであることを確認する。
//
// **`resets_at` が null の枠は判定から外す。**外さないと、リセット時刻が分からない枠に
// 引きずられて「いつまで待つか」を決められない。
//
// 与える情報: 使い切っている枠が2件（片方は resets_at が null）、まだ余裕のある枠が1件。
// 種別の名前は判定に使われない（Snapshot は Kind を見ない）ので、ステータスラインが運ぶ
// "session" と "weekly_all" だけで組む。
// 成功条件: MaxPercent が最大値を返し、AtFullPercent が真になり、
// LatestResetOfFullLimits が **resets_at が入っている枠の中で** いちばん遅い時刻を返すこと。
func TestSnapshot_使い切った枠のうちresets_atがある中でいちばん遅い時刻を返す(t *testing.T) {
	snap := &ratelimit.Snapshot{
		Limits: []ratelimit.Limit{
			{Kind: "session", Percent: 100, ResetsAt: mustTime(t, "2026-08-18T14:09:59Z")},
			{Kind: "weekly_all", Percent: 42, ResetsAt: mustTime(t, "2026-08-24T18:59:59Z")},
			{Kind: "weekly_all", Percent: 100, ResetsAt: nil},
		},
	}

	if got := snap.MaxPercent(); got != 100 {
		t.Fatalf("MaxPercent が想定と違う: got %d, want 100", got)
	}
	if !snap.AtFullPercent() {
		t.Fatalf("100%% の枠があるのに AtFullPercent が偽である")
	}
	got, ok := snap.LatestResetOfFullLimits()
	if !ok {
		t.Fatalf("使い切った枠があるのに LatestResetOfFullLimits が見つからないと返した")
	}
	want := *mustTime(t, "2026-08-18T14:09:59Z")
	if !got.Equal(want) {
		t.Fatalf("リセット時刻が想定と違う（resets_at が null の枠か、使い切っていない枠を混ぜている可能性）: got %s, want %s", got, want)
	}
}

// 目的: 使い切った枠が resets_at を1つも持たない場合に「見つからない」と返すことを確認する。
//
// **ゼロ値の時刻を「いますぐリセットされる」と読ませてはならない。**
//
// 与える情報: percent が 100 だが resets_at が null の枠だけ。
// 成功条件: AtFullPercent は真、LatestResetOfFullLimits の2つ目の戻り値が false であること。
func TestSnapshot_使い切った枠にresets_atが無ければ見つからないと返す(t *testing.T) {
	snap := &ratelimit.Snapshot{
		Limits: []ratelimit.Limit{{Kind: "weekly_all", Percent: 100, ResetsAt: nil}},
	}
	if !snap.AtFullPercent() {
		t.Fatalf("100%% の枠があるのに AtFullPercent が偽である")
	}
	if _, ok := snap.LatestResetOfFullLimits(); ok {
		t.Fatalf("resets_at が1つも無いのに時刻が見つかったと返した")
	}
}

// 目的: nil の Snapshot に対しても panic せず、安全な既定値を返すことを確認する
// （保管値が無いときや値が古いとき、orchestrator は nil の写しを渡してくる）。
// 与える情報: nil の *Snapshot。
// 成功条件: MaxPercent が 0、AtFullPercent が false、LatestResetOfFullLimits が
// 見つからないと返すこと。
func TestSnapshot_nilに対して安全な既定値を返す(t *testing.T) {
	var snap *ratelimit.Snapshot
	if got := snap.MaxPercent(); got != 0 {
		t.Fatalf("nil の MaxPercent が 0 でない: got %d", got)
	}
	if snap.AtFullPercent() {
		t.Fatalf("nil の AtFullPercent が真である")
	}
	if _, ok := snap.LatestResetOfFullLimits(); ok {
		t.Fatalf("nil の LatestResetOfFullLimits が見つかったと返した")
	}
}
