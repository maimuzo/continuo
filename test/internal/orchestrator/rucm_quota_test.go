// **この2つは症状が似ていて、区別を誤ると被害が正反対になる。**
// 枠待ちを打ち切りと誤れば、待てば再開する run を捨てる。
// 打ち切りを枠待ちと誤れば、止まった run を永久に待ち続ける。
package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// TestRUCMQuota_P007_枠を見ない設定なら枠明けを待たない は、代替フロー「応答のあるrun」の前提を検査する。
//
// **枠待ちの条件は2つある**（設計 3-27）。枠が100%であることと、
// **その run が `turn_timeout_ms` のあいだ hook を1件も受けていないこと。**
// **枠を見ない設定（`source: none`）では、1つ目の条件が永久に成立しない。**
// したがって、どれだけ待っても枠待ちにはならない。
//
// 目的: `rate_limit.source: none` のとき、枠明けを待つ呼び出しを1回も送らないこと。
// 与える情報: 枠を見ない設定と、hook を1件も送らない run。
// 成功条件（RUCM の POSTCONDITION）: run の枠待ちの印が立たないこと
// （`agent.wait` を1回も送らないことで確かめる）。
func TestRUCMQuota_P007_枠を見ない設定なら枠明けを待たない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = "none"
			cfg.Claude.TurnTimeoutMs = 1000
		},
	})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	holdPrompt(fx)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// **打ち切りうる時間だけ巡回を回す。**枠待ちになるなら、ここで agent.wait が飛ぶ。
	for i := 0; i < 5; i++ {
		time.Sleep(400 * time.Millisecond)
		fx.Orc.Tick(context.Background())
	}

	if got := fx.Herdr.CountMethod(herdr.MethodAgentWait); got != 0 {
		t.Errorf("枠を見ない設定なのに枠明けを待っている: agent.wait を %d 回送った", got)
	}
}
