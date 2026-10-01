package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// TestResumeBackoff_バックオフが明けた巡回で同じrunをもう1度打ち切らない は、
// 巡回の先頭の拾い直しと、同じ巡回の打ち切りの判定が、同じ run を取り合わないことを確かめる
// （設計 3-21 / 3-25）。
//
// 目的: **打ち切りで積んだバックオフが明けた run は、前の attempt の `LastSeenAt` を持ったままである。**
// 巡回は先頭で拾い直し（`resumeBackoff`）、同じ巡回の後ろで打ち切りの判定（`checkStalls`）を回す。
// **後ろの判定が、拾い直したばかりの run を「閾値を超えて止まっている」と読むと、
// 1回の停止でリトライが2つ減り、起こし直した pane を閉じる。**
//
// 与える情報: stall でリトライを1つ積んだ run。バックオフが明けるまで時計を進めて巡回を2回まわす。
// 成功条件: 拾い直した巡回と、その次の巡回を回したあと、打ち切りの1行が1回しか出ていないこと。
//
// **実時間はゼロである。**
func TestResumeBackoff_バックオフが明けた巡回で同じrunをもう1度打ち切らない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newStubFixture(t, stubFixtureOptions{
			Logs:        true,
			AgentStatus: herdr.AgentStatusUnknown,
			Mutate: func(cfg *config.Config) {
				cfg.Claude.TurnTimeoutMs = int(stallTimeout / time.Millisecond)
				cfg.Agent.MaxRetryBackoffMs = 10000
				cfg.Trust.RequireRepoTrusted = false
				cfg.Tracker.VerifyStatesEvery = 0
			},
		})
		adoptRun(fx, 188)

		time.Sleep(stallTimeout + time.Second)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		v, ok := viewOf(fx, "octocat/hello-world#188")
		if !ok || v.RetryCount != 1 {
			t.Fatalf("stall でリトライが積まれていない: %+v (ok=%v)", v, ok)
		}

		// バックオフが明けるまで進め、拾い直す巡回を回す。そのあと、もう1回まわす。
		time.Sleep(20 * time.Second)
		fx.Orc.Tick(context.Background())
		synctest.Wait()
		time.Sleep(5 * time.Second)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		// **この一式は通信を行わない stub なので、拾い直した着手そのものは worktree を用意できずに失敗する。**
		// **見るのは、打ち切りの1行が2回出ていないことである。**
		// 2回出るなら、拾い直した run を、前の attempt の時計で打ち切っている。
		logs := fx.Logs.String()
		if !strings.Contains(logs, "バックオフが明けたので再 dispatch します") {
			t.Fatalf("拾い直しが走っていない:\n%s", logs)
		}
		if got := strings.Count(logs, "run を諦めてリトライを積みました"); got != 1 {
			t.Fatalf("打ち切りが %d 回出た, want 1（拾い直した run を、同じ巡回でもう1度打ち切っている）:\n%s", got, logs)
		}
	})
}
