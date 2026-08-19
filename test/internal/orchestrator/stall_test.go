package orchestrator_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// stallTimeout はこのファイルのテストで使う stall の閾値である（fake の時計で進める）。
const stallTimeout = 60 * time.Second

// TestCheckStalls_workingなら猶予を1回だけ与えて2回目で止める は、stall 検知の段階を確かめる。
//
// 目的: 設計 3-21 / 3-27 の「閾値を超えたら `agent_status` を1回見て、`working` なら猶予を
// 1回だけ与える（`LastSeenAt` を現在時刻にして、もう一度 `stall_timeout_ms` だけ待つ）。
// **与えたことを記録し、2回目は与えない**（`working` のまま固まる場合があるため）」を示す。
//
// 与える情報: `agent_status` が常に `working` の run。閾値を2回またぐ。
// 成功条件: 1回目の閾値では止めず（リトライ0）、2回目でリトライが1つ積まれる。
//
// **実時間はゼロである。**`testing/synctest` の bubble の中で時計を進める。
func TestCheckStalls_workingなら猶予を1回だけ与えて2回目で止める(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newStubFixture(t, stubFixtureOptions{
			AgentStatus: herdr.AgentStatusWorking,
			Mutate: func(cfg *config.Config) {
				cfg.Claude.StallTimeoutMs = int(stallTimeout / time.Millisecond)
			},
		})
		adoptRun(fx, 188)

		// 1回目: 閾値を超えたが working なので猶予を1回だけ与える。
		time.Sleep(stallTimeout + time.Second)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		v, ok := viewOf(fx, "maimuzo/koetsumugi#188")
		if !ok {
			t.Fatalf("猶予を与えるべき run が印から外れている")
		}
		if v.RetryCount != 0 {
			t.Fatalf("猶予を与えずに止めている: retry_count = %d", v.RetryCount)
		}

		// 2回目: もう一度閾値を超えた。**2回目は与えない。**
		time.Sleep(stallTimeout + time.Second)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		v, ok = viewOf(fx, "maimuzo/koetsumugi#188")
		if !ok {
			t.Fatalf("バックオフ中の run を印から外している（30秒後の巡回で即座に拾い直される）")
		}
		if v.RetryCount != 1 {
			t.Fatalf("2回目の閾値でリトライを積んでいない: retry_count = %d", v.RetryCount)
		}
		if v.BackoffUntil.IsZero() {
			t.Fatalf("バックオフの期限を入れていない")
		}
		if len(fx.Herdr.ClosedPanes()) == 0 {
			t.Fatalf("stall で止めたのに pane を閉じていない（pane.close が唯一の手段である）")
		}
	})
}

// TestCheckStalls_PreToolUseで時計がリセットされる は、生きていることの確認を確かめる。
//
// 目的: 設計 3-21 の「`PreToolUse` と `PostToolUse` を全ツールに張る。届くたびに時計を
// リセットする。**turn の終わりの判定には使わない。生きていることの確認だけに使う**」を示す。
//
// 与える情報: 閾値の手前で `PreToolUse` が1件届き、そのあと閾値未満だけ時間が進む。
// 成功条件: stall と判定されない（リトライが積まれない）。
//
// **実時間はゼロである。**
func TestCheckStalls_PreToolUseで時計がリセットされる(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newStubFixture(t, stubFixtureOptions{
			AgentStatus: herdr.AgentStatusUnknown,
			Mutate: func(cfg *config.Config) {
				cfg.Claude.StallTimeoutMs = int(stallTimeout / time.Millisecond)
			},
		})
		adoptRun(fx, 188)

		// 閾値の手前で道具の hook が1件届く。
		time.Sleep(stallTimeout - 5*time.Second)
		if !fx.Orc.OnHook(toolHook("session-188", "PreToolUse")) {
			t.Fatalf("PreToolUse を知らない run のものとして捨てた")
		}

		// そこから閾値未満だけ進める。
		time.Sleep(stallTimeout - 5*time.Second)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		v, ok := viewOf(fx, "maimuzo/koetsumugi#188")
		if !ok {
			t.Fatalf("時計がリセットされていれば止まらないはずの run が印から外れている")
		}
		if v.RetryCount != 0 {
			t.Fatalf("PreToolUse で時計がリセットされていない: retry_count = %d", v.RetryCount)
		}
	})
}

// TestResumeBackoff_バックオフが明けた run を巡回の先頭で拾い直す は、再 dispatch を確かめる。
//
// 目的: 設計 3-21 / 3-25 の「巡回の先頭で印の集合を走査する。`BackoffUntil` を過ぎている
// → その run を再 dispatch する。**候補の取得より前に走査する**（空きスロットの計算に効く）」を示す。
//
// 与える情報: stall でリトライを積んだ run。バックオフが明けるまで時計を進める。
// 成功条件: 再 dispatch が走る（Status が `running_state` へ書き直される）。
//
// **セッション UUID の採り直しと、送る本文が1回目の本文（5-3）になることは、
// [TestResumeBackoff_再dispatchはUUIDを採り直して1回目の本文を送る] が確かめる。**
// このテストは通信を1本も行わない stub を使うので、`agent.start` の引数を観測できない。
//
// **実時間はゼロである。**
func TestResumeBackoff_バックオフが明けたrunを巡回の先頭で拾い直す(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newStubFixture(t, stubFixtureOptions{
			AgentStatus: herdr.AgentStatusUnknown,
			Mutate: func(cfg *config.Config) {
				cfg.Claude.StallTimeoutMs = int(stallTimeout / time.Millisecond)
				cfg.Agent.MaxRetryBackoffMs = 10000
				// 信頼の検査は helpers_test.go の偽 socket サーバ側で確かめている。
				// ここで見たいのは「バックオフが明けたら段0 から入り直す」ことだけである。
				cfg.Trust.RequireRepoTrusted = false
				cfg.Tracker.VerifyStatesEvery = 0
			},
		})
		adoptRun(fx, 188)

		time.Sleep(stallTimeout + time.Second)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		v, ok := viewOf(fx, "maimuzo/koetsumugi#188")
		if !ok || v.RetryCount != 1 {
			t.Fatalf("stall でリトライが積まれていない: %+v (ok=%v)", v, ok)
		}
		before := fx.Tracker.CountCall("UpdateStatus")

		// バックオフが明けるまで進める。
		time.Sleep(20 * time.Second)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		if fx.Tracker.CountCall("UpdateStatus") <= before {
			t.Fatalf("バックオフが明けたのに再 dispatch していない: %v", fx.Tracker.Calls())
		}
	})
}
