package tracker_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/tracker"
)

// TestBootstrap_ボードにあって設定に無いStatusを起動時に名前で知らせる は、
// 設計 3-50 の「起動時に1回だけ知らせる」を確かめる。
//
// 目的: 起動時の照合は「設定の名前がボードに在るか」の一方向だけである。
// **ボードにあって設定に無いものは件数（`status_options=6`）にしか出ない。**
// 知らない Status へ動かされた issue は worker を止められるので、名前を先に見せておく。
//
// 与える情報: ボードの選択肢は Ice Box / Ready / In Progress / Blocked / In Review / Done。
// 設定が名前を持つのは Ready / In Progress / Done / Blocked / In Review の5つで、
// **`Ice Box` だけが設定のどこにも出てこない。**
// 成功条件: Bootstrap のログに `Ice Box` が名前で出ること。件数だけで済ませないこと。
func TestBootstrap_ボードにあって設定に無いStatusを起動時に名前で知らせる(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	fs := newFakeGraphQLServer(t, single(dataResponse(bootstrapProjectPayload(testStatusOptions))))
	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, logger, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	if err := a.Bootstrap(t.Context(), testTrackerConfig()); err != nil {
		t.Fatalf("Bootstrap が失敗した: %v", err)
	}

	logs := buf.String()
	if !strings.Contains(logs, "ボードには continuo が知らない Status があります") {
		t.Fatalf("知らない Status の知らせが出ていない:\n%s", logs)
	}
	if !strings.Contains(logs, "Ice Box") {
		t.Fatalf("知らない Status の名前（Ice Box）がログに出ていない:\n%s", logs)
	}
}

// TestVerifyStatusOptions_知らないStatusの知らせを巡回ごとに繰り返さない は、
// 設計 3-50 の「巡回ごとの再照合では出さない」を確かめる。
//
// 目的: `verify_states_every`（既定 20 巡回に1回）で同じ行が流れると、他の行が埋もれる。
// 与える情報: Bootstrap を済ませたあとに VerifyStatusOptions を2回呼ぶ。
// 成功条件: 再照合のログに知らせが1度も出ないこと。
func TestVerifyStatusOptions_知らないStatusの知らせを巡回ごとに繰り返さない(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	fs := newFakeGraphQLServer(t, single(dataResponse(bootstrapProjectPayload(testStatusOptions))))
	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, logger, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	if err := a.Bootstrap(t.Context(), testTrackerConfig()); err != nil {
		t.Fatalf("Bootstrap が失敗した: %v", err)
	}
	buf.Reset()

	for i := 0; i < 2; i++ {
		if err := a.VerifyStatusOptions(t.Context(), testTrackerConfig()); err != nil {
			t.Fatalf("VerifyStatusOptions が失敗した: %v", err)
		}
	}

	if logs := buf.String(); strings.Contains(logs, "ボードには continuo が知らない Status があります") {
		t.Fatalf("巡回ごとの再照合で知らせを繰り返している:\n%s", logs)
	}
}
