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
		//
		// **この検査の限界**（実装レビュー3周目の LOW。2026-10-02 に測った）。
		// **`redispatch` の時計の数え直しを外した版を100回流すと、落ちるのは92回である**（入れた版は100回とも通る）。
		// 拾い直した着手の失敗の後始末が、同じ巡回の打ち切りの判定より先に終わらせる印を取ると、
		// 外した版でも打ち切りは1回しか出ない。goroutine の順番は固定できない。
		// **時計そのもの（`RunView.StallClockAt`）を見る形も試したが、成り立たなかった。**
		// 着手に失敗した run は印から消えるので、巡回のあとに読む相手がいない。
		// 着手が成功する一式（ghq の clone と worktree が要る）は、この stub では作れない。
		logs := fx.Logs.String()
		if !strings.Contains(logs, "バックオフが明けたので再 dispatch します") {
			t.Fatalf("拾い直しが走っていない:\n%s", logs)
		}
		if got := strings.Count(logs, "run を諦めてリトライを積みました"); got != 1 {
			t.Fatalf("打ち切りが %d 回出た, want 1（拾い直した run を、同じ巡回でもう1度打ち切っている）:\n%s", got, logs)
		}
	})
}
