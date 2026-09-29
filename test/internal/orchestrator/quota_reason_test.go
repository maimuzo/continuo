// {"RUCM-CFG-SHA256": "28c7411d34dd0445e061bcaebcc7256bc7b7b1a7643cdff5ddbe6dcafe24fa44", "SOURCE": "docs/spec/usecases/particular_case/issue の担当を入札で決める.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**
//
// **入札の要る issue を取らないと決めたとき、その理由が既定のログの水準で出ることを検査する**
// （issue #173）。**以前は `Debug` にしか出ておらず、既定（`--log-level info`）では1行も出なかった。**
package orchestrator_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// **CFG のパスに対応づけない。**このユースケース記述は「走っている run が枠明けを待って
// 再開するまで」を書いたもので、**新しい issue を取るかどうかの門は1段も持っていない。**
// 対応づけると、無関係なパスに代表を立てたことになる。
//
// TestQuota_枠を読めなければ入札の要るissueには着手しない は、2つの門を1つに揃えたことを
// 確かめる（設計 3-77j。issue #173）。
//
// 目的: **枠を読めないとき、入札は「黙る」、新規 dispatch は「止めない」で逆を向いていた。**
// 入札が先に効くので後ろは一度も効かず、**ボードが1件も進まないのに出るのは `Debug` の1行だけ**
// だった。**判定を1つに揃え、既定の水準で理由を出すことを示す。**
//
// 与える情報: usage API が 500 を返す（枠を読めない）。担当者のいない `Ready` の issue が1件。
// 成功条件: その issue が dispatch されず、`Info` で理由が出ること。
func TestQuota_枠を読めなければ入札の要るissueには着手しない(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	reader := newUsageReader(t, srv.URL, "CONTINUO_TEST_OAUTH_TOKEN_UNREADABLE")

	fx := newStubFixture(t, stubFixtureOptions{
		// **ログを溜める。**止めた理由が既定の水準で出ることを検査する（issue #173）。
		Logs:      true,
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.Trust.RequireRepoTrusted = false
		},
	})
	fx.Tracker.AddIssue(sampleIssue(190, "Ready"))

	fx.Orc.Tick(context.Background())

	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == "octocat/hello-world#190" {
			t.Fatalf("枠を読めないのに入札の要る issue へ着手している: %+v", v)
		}
	}
	// **止まったことが人間に見えなければ、直したことにならない。**
	got := fx.Logs.String()
	if !strings.Contains(got, "level=INFO") || !strings.Contains(got, "枠を読めないので") {
		t.Fatalf("止めたことを INFO で出していない:\n%s", got)
	}
	if !strings.Contains(got, "枠を読めない") {
		t.Fatalf("止めた理由を出していない:\n%s", got)
	}
	// **直し方を取り違えさせない。**枠を読めないのは資格情報の話であって、
	// **マージンをいくら下げても動き出さない。**
	if !strings.Contains(got, "マージンを下げても動き出しません") {
		t.Fatalf("枠を読めないときに、マージンでは直らないと書いていない:\n%s", got)
	}
}

// TestQuota_枠を読めなくても自分が担当のissueには着手する は、巡回を打ち切っていないことを
// 確かめる（設計 3-77j。issue #173）。
//
// 目的: **枠を読めないだけで巡回を打ち切ってはならない。**打ち切ると、
// **この機械が既に担当者になっている issue まで着手されなくなる**（印が無いのでこの経路からしか
// 拾えない）。**期限切れの担当を外す経路も通らない。**
//
// 与える情報: usage API が 500 を返す。**この機械（gh の持ち主）が担当者の `Ready` の issue が1件。**
// 成功条件: その issue が dispatch されること。
func TestQuota_枠を読めなくても自分が担当のissueには着手する(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	reader := newUsageReader(t, srv.URL, "CONTINUO_TEST_OAUTH_TOKEN_MINE")

	fx := newStubFixture(t, stubFixtureOptions{
		// **ログを溜める。**止めた理由が既定の水準で出ることを検査する（issue #173）。
		Logs:      true,
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.Trust.RequireRepoTrusted = false
		},
	})
	fx.Tracker.AddIssue(assignedIssue(191, "Ready", testGHLogin))

	fx.Orc.Tick(context.Background())

	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == "octocat/hello-world#191" {
			return
		}
	}
	t.Fatalf("既に自分が担当の issue にまで着手していない（枠を読めないだけで巡回を打ち切っている）:\n%s",
		fx.Logs.String())
}

// TestQuota_枠が逼迫していても担当が自分のissueには着手する は、
// 止める範囲が入札の要る issue だけであることを確かめる（設計 3-27。issue #173）。
//
// 目的: **巡回を丸ごと打ち切ってはならない。**
// **以前は `rate_limit.pause_above_percent` を超えると `dispatchCandidates` が即 `return` していた。**
// **その設定は消えた**（人間の決定。2026-09-06）。**打ち切ると、この機械が既に担当者に
// なっている issue まで着手されなくなる**（印が無いのでこの経路からしか拾えない）。
// **再起動で復元した run も拾えない**（`restart.orphan_running_action` の既定 `redispatch` は
// 復元では何もせず、次の巡回に委ねる）。**`handoffGate` の中にある「期限切れの担当を外す」
// 経路も通らなくなる**ので、詰まったカンバンを誰も解けない。
//
// 与える情報: 1回目は 99% を返し、2回目以降は 500 を返す usage API。
// **この機械が担当者の `Ready` の issue が1件**（入札を要さない経路）。
// 成功条件: その issue に着手すること。
func TestQuota_枠が逼迫していても担当が自分のissueには着手する(t *testing.T) {
	var reads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if reads.Add(1) > 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{"limits": []map[string]any{
			{"kind": "session", "percent": 99, "resets_at": nil, "severity": "normal"},
		}}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Errorf("偽の usage API が応答を書けません: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	reader := newUsageReader(t, srv.URL, "CONTINUO_TEST_OAUTH_TOKEN_STALE")

	fx := newStubFixture(t, stubFixtureOptions{
		// **ログを溜める。**止めた理由が既定の水準で出ることを検査する（issue #173）。
		Logs:      true,
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.Trust.RequireRepoTrusted = false
		},
	})
	fx.Tracker.AddIssue(assignedIssue(192, "Ready", testGHLogin))

	// 1回目で 99% を読み、2回目からは読めなくなる。
	fx.Orc.Tick(context.Background())
	fx.Orc.Tick(context.Background())

	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == "octocat/hello-world#192" {
			return
		}
	}
	t.Fatalf("担当が自分の issue にまで着手していない（巡回を丸ごと打ち切っている）:\n%s",
		fx.Logs.String())
}

// TestQuota_マージンが先に効いて止まり使用率と閾値が出る は、出す1行の中身を確かめる
// （設計 3-77j。issue #173）。
//
// 目的: **新規着手が止まる使用率は `100 − マージン` である。**
// マージン10なら **90% から**である（`rate_limit.pause_above_percent` は消えた。issue #173）。
// **観測した使用率と、枠ごとの閾値の両方を出さないと、どちらの枠が原因かを読めない。**
//
// 与える情報: 1週間の枠が 92%。担当者のいない `Ready` の issue が1件。
// 成功条件: dispatch されず、使用率と閾値が1行に出ること。
func TestQuota_マージンが先に効いて止まり使用率と閾値が出る(t *testing.T) {
	endpoint, _ := newUsageServer(t, []map[string]any{
		{"kind": "session", "percent": 30, "resets_at": nil, "severity": "normal"},
		{"kind": "weekly_all", "percent": 92, "resets_at": nil, "severity": "normal"},
	})
	reader := newUsageReader(t, endpoint, "CONTINUO_TEST_OAUTH_TOKEN_MARGIN")

	fx := newStubFixture(t, stubFixtureOptions{
		// **ログを溜める。**止めた理由が既定の水準で出ることを検査する（issue #173）。
		Logs:      true,
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.Tracker.Provider.Handoff.FiveHourMarginPercent = 10
			cfg.Tracker.Provider.Handoff.WeeklyMarginPercent = 10
			cfg.Trust.RequireRepoTrusted = false
		},
	})
	fx.Tracker.AddIssue(sampleIssue(193, "Ready"))

	fx.Orc.Tick(context.Background())

	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == "octocat/hello-world#193" {
			t.Fatalf("余裕値がマイナスなのに着手している: %+v", v)
		}
	}
	got := fx.Logs.String()
	for _, want := range []string{
		"余裕値が0以下",
		"1週間の枠の使用率=92",
		"5時間の枠の使用率=30",
		`1週間の枠の閾値="90% に達したら止まります"`,
		`5時間の枠の閾値="90% に達したら止まります"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("1行に %q が入っていない:\n%s", want, got)
		}
	}
}

// weeklyWaitFixture は「1週間の枠を待つ上限」の検査で使う一式を組み立てる（issue #197）。
//
// **担当者はこの機械（gh の持ち主）である。**手放す相手が自分でないと、外す対象が見つからない。
// **枠待ちの条件その2（turn_timeout_ms のあいだ hook が来ていない）は、時計を進めて作る。**
//
// t: 呼び出し元のテスト。
// limits: usage API が返す枠の一覧。
// limitMinutes: `rate_limit.weekly_wait_limit_minutes` に入れる値。
// tokenEnv: トークンを入れる環境変数の名前（テストごとに変える）。
// 戻り値: 組み立てた一式・印へ入れた issue・進められる時計。
