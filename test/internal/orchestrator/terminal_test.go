package orchestrator_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// blockFirstPrompt は**最初の `agent.prompt` だけ**を、返された関数が呼ばれるまで
// 返さないようにする。
//
// **turn ループを「待ち受けの中」に留めるために使う。**実運用では `agent.prompt` を
// 待ち受けつきで呼ぶと turn の終わりまで返らない（既定1時間）ので、その状態を作る。
// 2回目以降（3-25 の9段でコメントを書かせる送信など）はすぐ返す。
//
// t: 呼び出し元のテスト。後始末で必ず解放する。
// fx: 対象の fixture。
func blockFirstPrompt(t *testing.T, fx *fixture) {
	t.Helper()
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })

	var mu sync.Mutex
	count := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		count++
		first := count == 1
		mu.Unlock()
		if first {
			<-release
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle"},
		}, nil
	})
}

// TestTick_巡回のループはコメントの確認でブロックしない は、設計 3-8 の制約を確かめる。
//
// 目的: 「**`agent.prompt` を wait つきで呼ぶと turn の終わりまで返らない**（既定1時間）ので、
// 巡回のループの中で同期的に呼んではならない」を示す。実行中の照合が `terminal_states` を
// 見つけると、片付ける前にコメントの確認（3-25 の9段）へ入り、そこで `agent.prompt` を
// 待ち受けつきで呼ぶ。**この経路を巡回のループの中で同期的に通すと、dispatch も stall 検知も
// 全部止まる。**
//
// 与える情報: 1回目の turn が `agent.prompt` の待ちに入ったままの run。
// その間にボードの Status が `Done` へ動く。コメントは1件も付いていない。
// 成功条件:
//   - `Done` を見つけた巡回が**すぐ返る**（コメントの確認を待たない）
//   - そのあと別の goroutine でコメントの確認が走り、2回目の `agent.prompt` が出る
func TestTick_巡回のループはコメントの確認でブロックしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	blockFirstPrompt(t, fx)
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// エージェントが自分で `gh` を叩いて Done へ動かした状況を作る（完了の真実の源はボード）。
	fx.Tracker.SetState(issue.ID, "Done")

	started := time.Now()
	fx.Orc.Tick(context.Background())
	elapsed := time.Since(started)
	if elapsed > 2*time.Second {
		t.Fatalf("巡回のループがコメントの確認でブロックしている（%s かかった）", elapsed)
	}

	// 巡回とは別の goroutine で、コメントを書かせる送信が出ている（3-25 の9段の段7）。
	waitFor(t, 10*time.Second, "コメントを書かせる送信が別の goroutine で出る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) >= 2
	})
}

// TestCheckStalls_1回のstallでabandonが2回走らない は、設計 3-21 の二重の打ち切りを確かめる。
//
// 目的: 巡回の stall 検知が run を諦めた直後、まだ `agent.prompt` の待ちに入っていた
// turn ループが同じ run をもう一度諦めると、**RetryCount が2倍の速さで消費され、
// `failure_state` への書き込みと引き渡しコメントの投稿が二重になる**。それを防いでいることを示す。
//
// 与える情報: 1回目の turn が `agent.prompt` の待ちに入ったままの run。
// 時計を stall の閾値の先へ進めて巡回すると、巡回側が先に run を諦める。
// そのあとで待ち受けを返し、turn ループを動かす。
// 成功条件: リトライが1つしか積まれない（2つ積まれない）。
func TestCheckStalls_1回のstallでabandonが2回走らない(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{
		Now: clock.Now,
		Mutate: func(cfg *config.Config) {
			cfg.Claude.StallTimeoutMs = 1000
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		<-release
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle"},
		}, nil
	})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// 巡回側が先に stall と判定して run を諦める。
	clock.Advance(5 * time.Second)
	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "巡回の stall 検知がリトライを1つ積む", func() bool {
		v, ok := viewOfFixture(fx, "maimuzo/koetsumugi#188")
		return ok && v.RetryCount == 1
	})

	// ここで待ち受けが返る。**turn ループは諦め直してはならない。**
	once.Do(func() { close(release) })
	time.Sleep(500 * time.Millisecond)

	v, ok := viewOfFixture(fx, "maimuzo/koetsumugi#188")
	if !ok {
		t.Fatalf("バックオフ中の run が印から外れている")
	}
	if v.RetryCount != 1 {
		t.Fatalf("1回の stall で2回諦めている: retry_count = %d", v.RetryCount)
	}
}

// TestAbandon_打ち切るときはworkerを止める前にコメントを確かめる は、
// 設計 3-25 の「いつ走らせるか」の表を確かめる。
//
// 目的: 「`max_turns` に達した / stall で打ち切った → **走らせる。worker を止める前に
// 確認する**」を示す。**確かめないと、その run の成果が issue に何も残らない。**
//
// 与える情報: `agent.max_retries` が 0 の設定で stall した run（1回目の stall で
// リトライを使い切り、人間へ渡す分岐に入る）。コメントは1件も付いていない。
// 成功条件:
//   - セッションの復元（`agent.start --resume`）が走る
//   - それでも書かれないので Status が `failure_state` へ落ちる
func TestAbandon_打ち切るときはworkerを止める前にコメントを確かめる(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{
		Now: clock.Now,
		Mutate: func(cfg *config.Config) {
			cfg.Claude.StallTimeoutMs = 1000
			cfg.Agent.MaxRetries = 0
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	blockFirstPrompt(t, fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	clock.Advance(5 * time.Second)
	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "打ち切りの前にセッションの復元が走る", func() bool {
		for _, r := range fx.Herdr.Requests() {
			if r.Method != herdr.MethodAgentStart {
				continue
			}
			args, _ := r.Params["args"].([]any)
			if strings.Contains(joinAny(args), "--resume") {
				return true
			}
		}
		return false
	})
	waitFor(t, 20*time.Second, "Status が failure_state へ落ちる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})
}
