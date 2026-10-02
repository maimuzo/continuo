// **件数は `.cfg.json` を数えた実測である**（2026-09-29。実装レビュー3周目の MEDIUM で直した。
// **以前は「15本・6通り」と書いていたが、それは `origin/main` の時点の値である**）。
package orchestrator_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// TestQuota_余裕値が0以下なら新規のdispatchだけを止める は、
// 「新規を止める線」と「この run は枠待ちである」を分けていることを確かめる。
//
// 目的: 設計 3-27 の「使用率が 100 に達していなければ枠待ちとみなさない。
// **走行中の turn は止めないし、時計も止めない**」を守っていることを示す。
//
// **新規を止める線は入札の余裕値1本だけである**（人間の決定。2026-09-06。issue #173）。
// 使用率96・マージン既定10なので、5時間余裕値は `100 − 96 − 10 = −6` で0以下になる。
// **`rate_limit.pause_above_percent` はキーごと消えた**（この test は触らない）。
//
// **RUCM のパス印は付けない。**この判定は「issueの担当を入札で決める」の側にあり、
// この file の SOURCE（レートリミットで待って再開する）のパスには当たらない。
//
// 与える情報: ステータスラインから届いた使用率が 96%（100 には達していない。issue #284）。
// `Ready` の issue が1件。
// 成功条件: 新規の dispatch が起きず、既にある run は枠待ちにならない。
func TestQuota_余裕値が0以下なら新規のdispatchだけを止める(t *testing.T) {
	fx := newStubFixture(t, stubFixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceStatusline
			cfg.Trust.RequireRepoTrusted = false
		},
	})
	t.Cleanup(fx.Close)
	feedFreshQuota(fx.Orc, "pane-a", time.Now(), 96, 10)
	running := adoptRun(fx, 188)
	fx.Tracker.AddIssue(sampleIssue(190, "Ready"))

	fx.Orc.Tick(context.Background())

	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == "octocat/hello-world#190" {
			t.Fatalf("余裕値が0以下なのに新規を dispatch している: %+v", v)
		}
		if v.Identifier == running.Identifier && v.WaitingQuota {
			t.Fatalf("使用率が 100 に達していないのに走行中の run の時計を止めている: %+v", v)
		}
	}
}

// TestQuota_source_noneなら使用率を読まずに0として入札に参加する は、`rate_limit.source: none` の意味を
// 確かめる（issue #284。計画の「source: none のとき」）。
//
// 目的: `none` は「読めなかった」ではなく、運用者が使用率で判定しないと決めた状態である
// （設計 3-27 の逃げ道）。**statusline取得を開かず、quota.json を読まず、届いた行も保管しない。**
//
// 与える情報: `rate_limit.source: none`。実行時ディレクトリに 100% を書いた quota.json。
// 起動時の準備（PrepareStatusline）と巡回3回。ステータスラインの行を1行。
// 成功条件: 保管値が空のまま（quota.json を読まず、枠待ちの判定に使う写しが nil）で、
// statusline取得を1度も開かないこと。
func TestQuota_source_noneなら使用率を読まずに0として入札に参加する(t *testing.T) {
	root := t.TempDir()
	quota := `{"session":{"percent":100,"resets_at":"` + time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339) + `"}}`
	if err := os.WriteFile(filepath.Join(root, "quota.json"), []byte(quota), 0o600); err != nil {
		t.Fatalf("quota.json を書けません: %v", err)
	}
	fx := newStubFixture(t, stubFixtureOptions{Root: root, Logs: true})
	t.Cleanup(fx.Close)

	fx.Orc.PrepareStatusline(context.Background())
	adoptRun(fx, 188)
	for i := 0; i < 3; i++ {
		fx.Orc.Tick(context.Background())
	}

	if snap := fx.Orc.QuotaSnapshotForTest(); snap != nil {
		t.Errorf("source が none なのに quota.json を読んだ: %+v", snap)
	}
	if fx.Orc.StatuslineFetchRunningForTest() || fx.countLog("statusline取得") != 0 {
		t.Errorf("source が none なのに statusline取得を開いた:\n%s", fx.Logs.String())
	}
	if got := len(fx.Herdr.SLStarts()); got != 0 {
		t.Errorf("source が none なのに statusline取得の Claude Code を %d 回起動した", got)
	}
}

// TestQuota_resets_atの無い期間は保管しない は、リセット時刻の扱いを確かめる（issue #284）。
//
// **いつ明けるか分からないものを「待つ」と決めると、永久に待つ run ができる**（設計 3-27）。
// ステータスラインの `resets_at` が欠けた期間は、`resets_at` が 0（1970年）として届く。
// **今より前の `resets_at` の期間は取り込まない**（計画の「値の形」）ので、保管値にも入らない。
//
// 目的: `resets_at` の無い 100% の期間で、枠待ちにしないこと。
// 与える情報: `used_percentage: 100` かつ `resets_at` の無い5時間の期間と、hook を送らない run。
// 成功条件: 落ちずに巡回が回りきり、保管値が空で、run が枠待ちにならないこと。
func TestQuota_resets_atの無い期間は保管しない(t *testing.T) {
	fx := newStubFixture(t, stubFixtureOptions{
		AgentStatus: herdr.AgentStatusUnknown,
		Mutate: func(cfg *config.Config) {
			cfg.Claude.TurnTimeoutMs = 50
			cfg.RateLimit.Source = ratelimit.SourceStatusline
		},
	})
	t.Cleanup(fx.Close)
	fx.Orc.OnStatusline(slLine("pane-a", 100, slWin(100, time.Unix(0, 0)), nil))
	fx.Orc.OnStatusline(slLine("pane-a", 200, slWin(100, time.Unix(0, 0)), nil))
	adoptRun(fx, 188)

	time.Sleep(120 * time.Millisecond)
	// **落ちないことを確かめる。**時刻を決められないまま進むと、ここで panic するか固まる。
	fx.Orc.Tick(context.Background())

	if snap := fx.Orc.QuotaSnapshotForTest(); snap != nil {
		t.Errorf("resets_at の無い期間を保管した: %+v", snap)
	}
	if v, ok := viewOf(fx, "octocat/hello-world#188"); !ok || v.WaitingQuota {
		t.Errorf("resets_at の無い期間で枠待ちにした: ok=%v %+v", ok, v)
	}
}
