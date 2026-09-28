package orchestrator_test

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/maimuzo/continuo/internal/config"
)

// statusline取得を開く条件と、巡回のループの順を確かめる（issue #284）。
//
// **巡回のループ（`Orchestrator.Run`）を testing/synctest の中で回し、偽の時計で刻みを進める。**
// statusline取得は `trust.repositories` を空にして「使える clone が無い」で即座に終わらせる
// （clone の選び方と statusline取得そのものは statusline_fetch_test.go と
// statusline_fetch_sync_test.go が確かめる）。
//
// **statusline取得を開いた回数は、「使える clone が無い」の WARN の行数で数える。**
// **巡回が回った回数は、偽のトラッカーがカンバンを読まれた回数で数える。**

// noCloneWarn は、statusline取得が「使える clone が無い」で終わったときの WARN の目印である。
const noCloneWarn = "使える clone が無い"

// runLoop は巡回のループを goroutine で起こし、止める関数を返す。
//
// **止める関数は bubble が終わる前に呼ぶこと。**ループを止め、orchestrator と loop を閉じる。
//
// fx: 対象の stub の fixture。
// 戻り値: ループを止める関数。
func runLoop(fx *stubFixture) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = fx.Orc.Run(ctx)
	}()
	return func() {
		cancel()
		<-done
		fx.Close()
	}
}

// ticksOf は巡回が回った回数を返す（カンバンの候補を読んだ回数）。
func ticksOf(fx *stubFixture) int {
	return fx.Tracker.CountCall("FetchIssuesByStates")
}

// TestStatuslineRun_起動して最初の巡回で値が新しくなければ開き前回の試行から間隔の間は開かない は、
// statusline取得を開く条件のうち、新しさと間隔を確かめる。
//
// 目的: 値が新しくなければ statusline取得で取り直す。ただし試行は前回の試行の開始から
// `refresh_interval_ms`（既定5分）に1回までにする（何もしていない機械で1日最大288回）。
// **起動して最初の巡回は前回の試行を問わない。**
// 与える情報: 行が1行も届いていない状態で巡回のループを起こし、30秒の刻みを10回進める。
// 成功条件: 最初の巡回で1回開き、5分の間は開かず、5分ちょうどの巡回で2回目を開くこと。
func TestStatuslineRun_起動して最初の巡回で値が新しくなければ開き前回の試行から間隔の間は開かない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, nil)
		stop := runLoop(fx)
		defer stop()
		interval := time.Duration(fx.Config.Polling.IntervalMs) * time.Millisecond
		refresh := time.Duration(fx.Config.RateLimit.RefreshIntervalMs) * time.Millisecond

		synctest.Wait()
		if got := fx.countLog(noCloneWarn); got != 1 {
			t.Fatalf("起動して最初の巡回で statusline取得を開いた回数が %d（want 1）", got)
		}
		for elapsed := interval; elapsed < refresh; elapsed += interval {
			time.Sleep(interval)
			synctest.Wait()
			if got := fx.countLog(noCloneWarn); got != 1 {
				t.Fatalf("前回の試行から %s しか経っていないのに開いた: %d 回", elapsed, got)
			}
		}
		time.Sleep(interval)
		synctest.Wait()
		if got := fx.countLog(noCloneWarn); got != 2 {
			t.Fatalf("前回の試行から %s 経った巡回で開いていない: %d 回", refresh, got)
		}
	})
}

// TestStatuslineRun_run の画面から値が届いていて新しければ開かない は、値が新しいときを確かめる。
//
// 目的: issue の run の Claude Code もステータスラインから値を送る。**それで新しい値が
// 届いていれば、statusline取得は要らない**（haiku の呼び出しを増やさない）。
// 与える情報: 引き継いだ run のセッションから届いた新しい応答の行。巡回のループを起こし、
// 刻みを4分ぶん進める。
// 成功条件: statusline取得を1度も開かないこと。
func TestStatuslineRun_runの画面から値が届いていて新しければ開かない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, nil)
		adoptRun(fx, 188)
		feedFreshQuota(fx.Orc, "session-188", time.Now(), 10, 20)
		stop := runLoop(fx)
		defer stop()

		time.Sleep(4 * time.Minute)
		synctest.Wait()
		if got := fx.countLog(noCloneWarn); got != 0 {
			t.Errorf("値が新しいのに statusline取得を %d 回開いた", got)
		}
	})
}

// TestStatuslineRun_期限内の保管値に100の期間がある間は開かない は、上限の最中を確かめる。
//
// 目的: **上限の最中は断られるだけで値は変わらない**ので、statusline取得を開かない。
// その期間の resets_at を過ぎると保管値から消え、開く。
// 与える情報: 10分後に明ける 100% の5時間の期間（初めて見るセッションの最初の行なので
// 値は新しくない）。巡回のループを起こし、刻みを進める。
// 成功条件: 9分30秒の時点では1度も開かず、10分30秒の時点で1回開いていること。
func TestStatuslineRun_期限内の保管値に100の期間がある間は開かない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, nil)
		fx.Orc.OnStatusline(slLine("pane-a", 100, slWin(100, time.Now().Add(10*time.Minute)), nil))
		stop := runLoop(fx)
		defer stop()

		time.Sleep(9*time.Minute + 30*time.Second)
		synctest.Wait()
		if got := fx.countLog(noCloneWarn); got != 0 {
			t.Fatalf("上限の最中なのに statusline取得を %d 回開いた", got)
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := fx.countLog(noCloneWarn); got != 1 {
			t.Fatalf("上限の期間が明けたのに statusline取得を開いていない: %d 回", got)
		}
	})
}

// TestStatuslineRun_値が届いた知らせで巡回が1回すぐ回りその巡回で入札し刻みを数え直す は、
// 知らせで回る巡回を確かめる。
//
// 目的: **巡回の中で値を待たない**（待つあいだ、止まった run の検知・ほかの issue の着手が止まる）。
// 値が届いたら巡回のループへ知らせ、ループは巡回を1回すぐ回して入札する。**直後にふだんの
// 巡回が続けて回らないよう、30秒の刻みを数え直す。**
// 与える情報: `Ready` の issue 1件と、値の無い状態で起こした巡回のループ。20秒後に
// 新しい応答の行を入れて知らせを送る。
// 成功条件: 知らせの直後に巡回が1回回り、その巡回で入札のコメントが1件書かれること。
// 知らせから29秒後までは巡回が回らず、30秒後に回ること。
func TestStatuslineRun_値が届いた知らせで巡回が1回すぐ回りその巡回で入札し刻みを数え直す(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, func(cfg *config.Config) {
			// **preflight が git を起こさないようにする**（internal/orchestrator/dispatch.go の信頼の検査）。
			cfg.Trust.RequireRepoTrusted = false
		})
		fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
		stop := runLoop(fx)
		defer stop()

		synctest.Wait()
		if got := len(fx.Tracker.MarkedHandoffCommentsOf(issueNode(188), config.HandoffBidMarker)); got != 0 {
			t.Fatalf("前提: 値が無いのに入札した: %d 件", got)
		}
		time.Sleep(20 * time.Second)
		synctest.Wait()
		if got := ticksOf(fx); got != 1 {
			t.Fatalf("前提: 20秒の時点で巡回が %d 回（want 1）", got)
		}

		feedFreshQuota(fx.Orc, "sl-fetch", time.Now(), 10, 20)
		fx.Orc.NotifyStatuslineForTest()
		synctest.Wait()
		if got := ticksOf(fx); got != 2 {
			t.Fatalf("値が届いた知らせで巡回が回っていない: %d 回（want 2）", got)
		}
		if got := len(fx.Tracker.MarkedHandoffCommentsOf(issueNode(188), config.HandoffBidMarker)); got != 1 {
			t.Errorf("知らせで回した巡回で入札していない: %d 件", got)
		}

		time.Sleep(29 * time.Second)
		synctest.Wait()
		if got := ticksOf(fx); got != 2 {
			t.Fatalf("刻みを数え直していない（知らせの直後にふだんの巡回が回った）: %d 回", got)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if got := ticksOf(fx); got != 3 {
			t.Fatalf("数え直した刻みで巡回が回っていない: %d 回（want 3）", got)
		}
	})
}

// TestStatuslineRun_巡回の途中に何度知らせが届いても巡回のあとに1回だけ回る は、知らせの畳み方を確かめる。
//
// 目的: 知らせは容量1で、置いてあれば足さない。巡回の途中に届いた知らせは、その巡回のあとに
// 1回だけ巡回を回す（値は届いているので、もう1回で足りる）。
// 与える情報: 最初の巡回の途中（カンバンを読む時点）で知らせを3回送る。
// 成功条件: 最初の巡回のあとに巡回が1回だけ回り、その29秒後まで回らないこと。
func TestStatuslineRun_巡回の途中に何度知らせが届いても巡回のあとに1回だけ回る(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, nil)
		var first atomic.Bool
		fx.Tracker.SetOnStates(func() {
			if first.CompareAndSwap(false, true) {
				for range 3 {
					fx.Orc.NotifyStatuslineForTest()
				}
			}
		})
		stop := runLoop(fx)
		defer stop()

		synctest.Wait()
		if got := ticksOf(fx); got != 2 {
			t.Fatalf("巡回の途中に届いた3回の知らせで、巡回が %d 回（want 最初の1回＋1回）", got)
		}
		time.Sleep(29 * time.Second)
		synctest.Wait()
		if got := ticksOf(fx); got != 2 {
			t.Fatalf("知らせを畳めていない: %d 回", got)
		}
	})
}

// TestStatuslineRun_刻みと知らせが両方溜まっていても続けて2回回らず巡回は同時に走らない は、
// 巡回のループの排他を確かめる。
//
// 目的: 巡回を呼ぶのは Run の goroutine だけで、2つの巡回が同時に走ることは無い。
// 刻みと知らせが両方溜まっていても、どちらで回した巡回のあとでも知らせを空にするので、
// 続けて2回は回らない。
// 与える情報: 最初の巡回を40秒長引かせ（その間に30秒の刻みが1つ溜まる）、途中で知らせを送る。
// 成功条件: 最初の巡回のあとに巡回が1回だけ回ること。同時に走った巡回が無いこと。
func TestStatuslineRun_刻みと知らせが両方溜まっていても続けて2回回らず巡回は同時に走らない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, nil)
		var first atomic.Bool
		var active, maxActive atomic.Int32
		fx.Tracker.SetOnStates(func() {
			n := active.Add(1)
			defer active.Add(-1)
			if n > maxActive.Load() {
				maxActive.Store(n)
			}
			if first.CompareAndSwap(false, true) {
				fx.Orc.NotifyStatuslineForTest()
				time.Sleep(40 * time.Second)
			}
		})
		stop := runLoop(fx)
		defer stop()

		// **最初の巡回が終わる40秒を過ぎるまで進める。**その間に30秒の刻みが1つ溜まる。
		time.Sleep(41 * time.Second)
		synctest.Wait()
		if got := ticksOf(fx); got != 2 {
			t.Fatalf("刻みと知らせが両方溜まったあと、巡回が %d 回（want 最初の1回＋1回）", got)
		}
		if got := maxActive.Load(); got != 1 {
			t.Errorf("巡回が同時に %d 本走った", got)
		}
	})
}
