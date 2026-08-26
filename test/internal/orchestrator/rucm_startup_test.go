// {"RUCM-CFG-SHA256": "9a1b2a5867ba950e6ab15cb6e6d1b0934e5b0d27cd9968f7b03284b1cb2d25ec", "SOURCE": "docs/spec/usecases/particular_case/issue を1件処理する.cfg.json"}
//
// **RUCM から生成したテストである。**「issue を1件処理する」の段13〜段15
// （pane が起動を受け付けるか / Claude Code が入力を受け付けられるか）を通る
// テストパスを1本ずつ検査する。
//
// **この4本は、実運用で issue が着手できなかった経路である**（2026-08-21、設計 6-2）。
// 当時の RUCM には段13 も `interactive_ready` も無く、代替フローも書かれていなかった。
// **RUCM に無いものはテストにもならない**ので、実機で回すまで誰も気づけなかった。
package orchestrator_test

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// {"RUCM-PATH": "P017"}
//
// TestRUCM_P012_paneがまだ使えないなら待ち直す は、段13 の代替フロー「paneがまだ使えない」を検査する。
//
// **`worktree.open` が作った pane は、シェルの起動が終わるまでコマンドを受け取れない。**
// herdr はそれを `agent_pane_busy`（`is not an available shell`）で返す。
//
// 目的: `agent_pane_busy` を受けても諦めず、pane が受け付けるまで待ち直すこと。
// 与える情報: 最初の2回だけ `agent_pane_busy` を返し、3回目から成功する `agent.start` の台本。
// 成功条件（RUCM の POSTCONDITION）: pane が起動を受け付けるまで待ち続けている。
// **ここでは「着手が最後まで進むこと」で確かめる**（待ち続けた結果、turn が送られる）。
func TestRUCM_P012_paneがまだ使えないなら待ち直す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	holdPrompt(fx)

	var starts atomic.Int32
	fx.Herdr.Handle(herdr.MethodAgentStart, func(params map[string]any) (any, *rpcErr) {
		if starts.Add(1) <= 2 {
			return nil, &rpcErr{Code: "agent_pane_busy", Message: "agent target pane is not an available shell"}
		}
		return map[string]any{
			"type": "agent_started",
			"agent": map[string]any{
				"name": params["name"], "agent_status": "idle",
				"interactive_ready": true, "pane_id": params["pane_id"],
			},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 15*time.Second, "待ち直した末に turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	if got := starts.Load(); got < 3 {
		t.Errorf("agent_pane_busy を受けて待ち直していない: agent.start の回数 %d", got)
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got == "Blocked" {
		t.Errorf("待てば通るのに人間へ渡している: state=%s", got)
	}
}

// {"RUCM-PATH": "P018"}
//
// TestRUCM_P013_paneが使えないまま期限を過ぎたら人間へ渡す は、代替フロー「paneの断念」を検査する。
//
// 目的: pane が最後まで使えなければ、`failure_state` へ落として理由を書くこと。
// 与える情報: `agent.start` が常に `agent_pane_busy` を返す台本と、粘りの上限を短くした設定。
// 成功条件（RUCM の POSTCONDITION）: issue の Status は `failure_state` の選択肢である。
// Claude Code は起動していない。worktree は残っている。
func TestRUCM_P013_paneが使えないまま期限を過ぎたら人間へ渡す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		// **リトライを1回で使い切らせる。**既定（3回）のままだと、バックオフの合計が
		// テストの待ち時間を超える。
		Mutate: func(cfg *config.Config) { cfg.Agent.MaxRetries = 0 },
	})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Herdr.Handle(herdr.MethodAgentStart, func(_ map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "agent_pane_busy", Message: "agent target pane is not an available shell"}
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 60*time.Second, "Status が failure_state へ落ちる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})
	// **後始末まで待つ。**Status は worker を止める前に書かれる（helpers_test.go の WaitRunsDrained）。
	fx.WaitRunsDrained(t, 10*time.Second)
	if fx.Herdr.CountMethod(herdr.MethodAgentPrompt) != 0 {
		t.Errorf("起動していないのに turn を送っている: %v", fx.Herdr.Methods())
	}
	worktreePath := filepath.Join(fx.WorktreeRoot, "github.com", "octocat", "hello-world", "continuo-octocat-hello-world-188")
	if _, err := os.Stat(worktreePath); err != nil {
		t.Errorf("起動に失敗しても worktree は残すこと: %s (err=%v)", worktreePath, err)
	}
}

// {"RUCM-PATH": "P015"}
//
// TestRUCM_P010_入力を受け付けられるまで待ち直す は、段15 の代替フロー「起動の待ち直し」を検査する。
//
// **`agent.start` は起動が終わるのを待たずに返る。**返った直後の `agent_status` は
// `unknown` で、`idle` になったあとも数秒は `interactive_ready` が偽である
// （2026-08-21 に実測。設計 6-2 の表）。**この間に指示を送ると `agent_not_ready` で弾かれる。**
//
// 目的: `interactive_ready` が真になるまで待ってから turn を送ること。
// 与える情報: `unknown` → `idle`（ready=false）→ `idle`（ready=true）と変わる `agent.get` の台本。
// 成功条件（RUCM の POSTCONDITION）: 入力を受け付けられるようになるまで待ち続け、
// **そのあいだ turn の本文を送らない。**
func TestRUCM_P010_入力を受け付けられるまで待ち直す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	holdPrompt(fx)

	var gets atomic.Int32
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		n := gets.Add(1)
		status, ready := "idle", true
		switch {
		case n == 1:
			status, ready = "unknown", false
		case n <= 3:
			// **`idle` でも `interactive_ready` が偽の時間がある。**ここが本題である。
			ready = false
		}
		return map[string]any{
			"type": "agent_info",
			"agent": map[string]any{
				"name": params["target"], "agent_status": status, "interactive_ready": ready,
			},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 15*time.Second, "ready になってから turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	if got := gets.Load(); got < 4 {
		t.Errorf("interactive_ready が偽のうちに進んでいる: agent.get の回数 %d", got)
	}
}

// {"RUCM-PATH": "P016"}
//
// TestRUCM_P011_入力を受け付けないまま期限を過ぎたら人間へ渡す は、代替フロー「起動の断念」を検査する。
//
// 目的: `herdr.startup_timeout_ms` を過ぎても入力を受け付けなければ、
// `agent.max_retries` まで試したうえで `failure_state` へ落とすこと。
// 与える情報: `agent.get` が常に `idle` かつ `interactive_ready: false` を返す台本と、
// 短い `startup_timeout_ms`、リトライ 0 回の設定。
// 成功条件（RUCM の POSTCONDITION）: issue の Status は `failure_state` の選択肢である。
// **turn の本文は Claude Code に届いていない。**worktree は残っている。
func TestRUCM_P011_入力を受け付けないまま期限を過ぎたら人間へ渡す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Herdr.StartupTimeoutMs = 1500
			cfg.Agent.MaxRetries = 0
		},
	})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type": "agent_info",
			"agent": map[string]any{
				"name": params["target"], "agent_status": "idle", "interactive_ready": false,
			},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 60*time.Second, "Status が failure_state へ落ちる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})
	// **後始末まで待つ。**Status は worker を止める前に書かれる（helpers_test.go の WaitRunsDrained）。
	fx.WaitRunsDrained(t, 10*time.Second)
	if fx.Herdr.CountMethod(herdr.MethodAgentPrompt) != 0 {
		t.Errorf("入力を受け付けないのに turn を送っている: %v", fx.Herdr.Methods())
	}
	worktreePath := filepath.Join(fx.WorktreeRoot, "github.com", "octocat", "hello-world", "continuo-octocat-hello-world-188")
	if _, err := os.Stat(worktreePath); err != nil {
		t.Errorf("起動に失敗しても worktree は残すこと: %s (err=%v)", worktreePath, err)
	}
}
