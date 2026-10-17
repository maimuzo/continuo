package orchestrator_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// ダッシュボードのレートリミットの区画が読む写し（`QuotaView`。issue #273）を確かめる。
//
// **行は OnStatusline で直に入れ、時計は testing/synctest の偽の時計で進める**（quota_store_test.go と同じ）。

// quotaWindowOf は写しから種別の期間を引く。無ければ ok が偽。
func quotaWindowOf(v orchestrator.QuotaView, kind string) (orchestrator.QuotaWindowView, bool) {
	for _, w := range v.Windows {
		if w.Kind == kind {
			return w, true
		}
	}
	return orchestrator.QuotaWindowView{}, false
}

// 目的: 写しが、判定と同じ「使える値」を、マージンを切ったか・使い切っているかと一緒に出すことを確かめる。
// 与える情報: 5時間の期間が 93%（マージン 10）、1週間の期間が 50% の新しい応答の行。
// 成功条件: 期間が2つ出て、5時間の期間だけ short が真、どちらも full が偽、マージンが 10、
// リセットの時刻が行の値と同じ、新しい値で、新しい着手を止めていて（余裕値が0以下）、最後に読めた時刻がいまであること。
func TestQuotaView_使える値をマージンの判定と一緒に出す(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, nil)
		defer fx.Close()
		now := time.Now()
		feedFreshQuota(fx.Orc, "s", now, 93, 50)

		v := fx.Orc.QuotaView()
		if v.Source != ratelimit.SourceStatusline {
			t.Errorf("source が %q（want %q）", v.Source, ratelimit.SourceStatusline)
		}
		if !v.Fresh {
			t.Error("新しい応答の行を受けた直後なのに、新しい値になっていない")
		}
		if !v.NewWorkBlocked {
			t.Error("5時間の期間の余裕値が0以下なのに、新しい着手を止めていない")
		}
		if !v.LastReadAt.Equal(now) {
			t.Errorf("最後に読めた時刻が %v（want %v）", v.LastReadAt, now)
		}
		if len(v.Windows) != 2 {
			t.Fatalf("期間が %d 個（want 2）: %+v", len(v.Windows), v.Windows)
		}
		s, ok := quotaWindowOf(v, handoff.LimitKindSession)
		if !ok || s.Percent != 93 || !s.Short || s.Full || s.MarginPercent != 10 {
			t.Errorf("5時間の期間が違う: %+v", s)
		}
		if want := time.Unix(now.Add(2*time.Hour).Unix(), 0); s.ResetsAt == nil || !s.ResetsAt.Equal(want) {
			t.Errorf("5時間の期間のリセットの時刻が %v（want %v）", s.ResetsAt, want)
		}
		w, ok := quotaWindowOf(v, handoff.LimitKindWeeklyAll)
		if !ok || w.Percent != 50 || w.Short || w.Full || w.MarginPercent != 10 {
			t.Errorf("1週間の期間が違う: %+v", w)
		}
	})
}

// 目的: 新しさの幅を過ぎても30分までは期間を出し続け、30分を過ぎたら期間を出さずに、最後に読めた時刻だけを残すことを確かめる。
// **判定（turn の終わり・着手の前）と同じ「使える値」を使っているかの検査である。**
// 与える情報: 新しい応答の行を受けたあと、時計を10分と、さらに21分（合わせて31分）進める。
// 成功条件: 10分後は新しい値でないが期間が2つ出る。31分後は期間が0個で、最後に読めた時刻は受けた時刻のままであること。
// どちらも新しい着手は止めている（新しい値が無いときは、入札は使用率を読めないとして黙る）。
func TestQuotaView_30分を過ぎたら期間を出さず最後に読めた時刻だけ残す(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, nil)
		defer fx.Close()
		readAt := time.Now()
		feedFreshQuota(fx.Orc, "s", readAt, 14, 73)

		time.Sleep(10 * time.Minute)
		v := fx.Orc.QuotaView()
		if v.Fresh {
			t.Error("新しさの幅（既定5分）を過ぎたのに、新しい値のままになっている")
		}
		if len(v.Windows) != 2 {
			t.Errorf("10分後の期間が %d 個（want 2。30分以内は使える値）", len(v.Windows))
		}
		if !v.NewWorkBlocked {
			t.Error("新しい値が無いのに、新しい着手を止めていない")
		}

		time.Sleep(21 * time.Minute)
		v = fx.Orc.QuotaView()
		if len(v.Windows) != 0 {
			t.Errorf("31分後の期間が %d 個（want 0。30分を過ぎた値は使わない）", len(v.Windows))
		}
		if !v.LastReadAt.Equal(readAt) {
			t.Errorf("最後に読めた時刻が %v（want %v）", v.LastReadAt, readAt)
		}
	})
}

// 目的: マージンが0の期間は、使用率100でも short を偽にし、full だけを真にすることを確かめる（`handoff.MarginCut`）。
// 与える情報: 5時間のマージンを0にした設定で、5時間の期間が 100% の行。
// 成功条件: 5時間の期間の short が偽、full が真、マージンが 0 であること。
func TestQuotaView_マージン0の期間は使い切ってもshortにしない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, func(cfg *config.Config) {
			cfg.Tracker.Provider.Handoff.FiveHourMarginPercent = 0
		})
		defer fx.Close()
		feedFreshQuota(fx.Orc, "s", time.Now(), 100, 20)

		s, ok := quotaWindowOf(fx.Orc.QuotaView(), handoff.LimitKindSession)
		if !ok {
			t.Fatal("5時間の期間が出ていない")
		}
		if s.Short || !s.Full || s.MarginPercent != 0 {
			t.Errorf("マージン0・使用率100の期間が違う（want short=false full=true margin=0）: %+v", s)
		}
	})
}

// 目的: `rate_limit.source: none` の機械では、保管値に何が残っていても期間を出さないことを確かめる。
// 与える情報: source を none にした設定で、使用率を持つ行を入れる。
// 成功条件: source が none、期間が0個、新しい値でなく、新しい着手を止めておらず、最後に読めた時刻がゼロ値であること。
func TestQuotaView_sourceがnoneなら期間を出さない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceNone
		})
		defer fx.Close()
		feedFreshQuota(fx.Orc, "s", time.Now(), 93, 50)

		v := fx.Orc.QuotaView()
		if v.Source != ratelimit.SourceNone || len(v.Windows) != 0 || v.Fresh || v.NewWorkBlocked || !v.LastReadAt.IsZero() {
			t.Errorf("source: none の写しが違う: %+v", v)
		}
	})
}
