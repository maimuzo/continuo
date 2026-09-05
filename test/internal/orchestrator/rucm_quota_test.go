// {"RUCM-CFG-SHA256": "6b08aa74c3e0aa05d9ff3e95c47d0f9912cd32969cd3b5750b8050116a074fbe", "SOURCE": "docs/spec/usecases/particular_case/レートリミットで待って再開する.cfg.json"}
//
// **RUCM から生成したテストである。**「レートリミットで待って再開する」のうち、
// **枠待ちと turn の打ち切りを取り違えないこと**を見る経路を検査する。
//
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

// {"RUCM-PATH": "P010"}
//
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

// {"RUCM-PATH": "P021"}
//
// TestRUCMQuota_P015_枠を読めなければ枠待ちにせず打ち切る は、代替フロー「枠を読めない」を検査する。
//
// **枠を読めないときに「枠待ちかもしれない」と待ち続けると、止まった run を永久に抱える。**
// 読めないなら枠の判定は諦め、通常の打ち切りとして扱う。
//
// 目的: usage API が読めない状態で hook も来なければ、`turn_timeout_ms` で打ち切ること。
// 与える情報: 枠の判定を無効にした設定（`source: none`）と、hook を1件も送らない run。
// 成功条件（RUCM の POSTCONDITION）: pane が閉じられ、**印は残り**、
// Status は `running_state` のままであること（リトライで再開するため）。
func TestRUCMQuota_P015_枠を読めなければ枠待ちにせず打ち切る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			// **画面の版が止まったら短い時間で打ち切る。**
			cfg.Claude.TurnTimeoutMs = 1200
		},
	})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	holdPrompt(fx)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// **hook を1件も送らないまま巡回を回す。**画面の版も動かない。
	waitFor(t, 30*time.Second, "pane が閉じられる", func() bool {
		fx.Orc.Tick(context.Background())
		return fx.Herdr.CountMethod(herdr.MethodPaneClose) > 0
	})

	// **枠明けを待っていない**（枠を読めないので待つ根拠がない）。
	if got := fx.Herdr.CountMethod(herdr.MethodAgentWait); got != 0 {
		t.Errorf("枠を読めないのに枠明けを待っている: agent.wait を %d 回送った", got)
	}
	// **Status は running_state のまま。**リトライが残っているので人間へ渡さない。
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "In Progress" {
		t.Errorf("リトライが残っているのに Status を動かしている: %s", got)
	}
}
