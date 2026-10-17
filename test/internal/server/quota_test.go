package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/server"
)

// ダッシュボードのレートリミットの区画（issue #273）を確かめる。

// sampleQuota は、読めている機械のレートリミットの写しを返す。
//
// 5時間の期間はリセットまで 11102 秒（3時間5分2秒）、1週間の期間は 306302 秒（3日13時間5分2秒）、
// モデル別の1週間の期間はリセットの時刻を読めず、マージンを切っている。
func sampleQuota() orchestrator.QuotaView {
	session := testTime.Add(11102 * time.Second)
	weekly := testTime.Add(306302 * time.Second)
	return orchestrator.QuotaView{
		Source:     "oauth_usage_api",
		LastReadAt: testTime.Add(-42 * time.Second),
		Fresh:      true,
		Windows: []orchestrator.QuotaWindowView{
			{Kind: "session", Percent: 14, ResetsAt: &session, MarginPercent: 10},
			{Kind: "weekly_all", Percent: 73, ResetsAt: &weekly, MarginPercent: 10},
			{Kind: "weekly_scoped", Percent: 95, MarginPercent: 10, Short: true},
		},
	}
}

// apiQuota は `/api/v1/state` の `quota` の鍵を、生の JSON の形のまま取り出す。
func apiQuota(t *testing.T, src *fakeSource) map[string]any {
	t.Helper()
	s, _ := newTestServerFrom(t, src)
	code, body := get(t, s, http.MethodGet, server.APIStatePath)
	if code != http.StatusOK {
		t.Fatalf("状態コードが %d", code)
	}
	var got struct {
		Quota map[string]any `json:"quota"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("JSON を読めなかった: %v\n%s", err, body)
	}
	if got.Quota == nil {
		t.Fatalf("quota の鍵が無い: %s", body)
	}
	raw, _ := json.MarshalIndent(got.Quota, "", "  ")
	t.Logf("quota の鍵:\n%s", raw)
	return got.Quota
}

// 目的: `/api/v1/state` の `quota` が、決めた形（鍵の名前・時刻は UTC・残りは秒の整数・読めない時刻は null）で出ることを確かめる。
// 与える情報: 3つの期間を持つ写し（うち1つはリセットの時刻を読めない）。
// 成功条件: 鍵が決めた6つちょうどで、期間ごとの鍵も決めた7つちょうど。リセットまでの秒が 11102 と 306302、
// 読めない期間は resets_at と resets_in_seconds が null、readable が真であること。
func TestAPIState_レートリミットの区画を決めた形で出す(t *testing.T) {
	q := apiQuota(t, &fakeSource{quota: sampleQuota(), tokenCallOrder: -1})

	wantKeys := []string{"source", "readable", "last_read_at", "fresh", "new_work_blocked", "windows"}
	if len(q) != len(wantKeys) {
		t.Errorf("quota の鍵が %d 個（want %d）: %v", len(q), len(wantKeys), q)
	}
	for _, k := range wantKeys {
		if _, ok := q[k]; !ok {
			t.Errorf("quota に鍵 %q が無い", k)
		}
	}
	if q["source"] != "oauth_usage_api" || q["readable"] != true || q["fresh"] != true || q["new_work_blocked"] != false {
		t.Errorf("quota の値が違う: %v", q)
	}
	if q["last_read_at"] != "2026-08-19T11:59:18Z" {
		t.Errorf("last_read_at が %v（want UTC の 2026-08-19T11:59:18Z）", q["last_read_at"])
	}
	windows, ok := q["windows"].([]any)
	if !ok || len(windows) != 3 {
		t.Fatalf("windows が3個の配列でない: %v", q["windows"])
	}
	wantWindowKeys := []string{"kind", "percent", "resets_at", "resets_in_seconds", "margin_percent", "short", "full"}
	for _, raw := range windows {
		w := raw.(map[string]any)
		if len(w) != len(wantWindowKeys) {
			t.Errorf("期間の鍵が %d 個（want %d）: %v", len(w), len(wantWindowKeys), w)
		}
		for _, k := range wantWindowKeys {
			if _, ok := w[k]; !ok {
				t.Errorf("期間に鍵 %q が無い: %v", k, w)
			}
		}
	}
	session := windows[0].(map[string]any)
	if session["kind"] != "session" || session["resets_at"] != "2026-08-19T15:05:02Z" || session["resets_in_seconds"] != float64(11102) {
		t.Errorf("5時間の期間が違う: %v", session)
	}
	if weekly := windows[1].(map[string]any); weekly["resets_in_seconds"] != float64(306302) {
		t.Errorf("1週間の期間のリセットまでの秒が違う: %v", weekly)
	}
	scoped := windows[2].(map[string]any)
	if scoped["resets_at"] != nil || scoped["resets_in_seconds"] != nil || scoped["short"] != true {
		t.Errorf("リセットの時刻を読めない期間が違う（resets_at と resets_in_seconds は null）: %v", scoped)
	}
}

// 目的: 使用率を読めていない機械と `rate_limit.source: none` の機械で、windows が null でなく空の配列になることを確かめる。
// 与える情報: 期間の無い写しを2つ（起動してからまだ読めていない・source が none）。
// 成功条件: どちらも readable が偽、windows が空の配列、last_read_at が null であること。
func TestAPIState_読めていなければwindowsは空の配列にする(t *testing.T) {
	for _, source := range []string{"statusline", "none"} {
		q := apiQuota(t, &fakeSource{quota: orchestrator.QuotaView{Source: source}, tokenCallOrder: -1})
		windows, ok := q["windows"].([]any)
		if !ok || len(windows) != 0 {
			t.Errorf("source %s: windows が空の配列でない: %v", source, q["windows"])
		}
		if q["readable"] != false || q["last_read_at"] != nil || q["source"] != source {
			t.Errorf("source %s: quota の値が違う: %v", source, q)
		}
	}
}

// 目的: HTML のいちばん上に区画を出し、リセットまでの残りを人間向けの言い回しで出すことを確かめる。
// 与える情報: sampleQuota の写しと、新しい着手を止めている印。
// 成功条件: 区画が実行中の run の表より前にあり、期間の名前・使用率・「あと3時間5分」「あと3日13時間」・
// リセットの時刻を読めない期間の「不明」・マージンを切った印・新しい着手を止めている印・最後に読めた時刻が出ること。
func TestIndex_レートリミットの区画をいちばん上に出す(t *testing.T) {
	q := sampleQuota()
	q.NewWorkBlocked = true
	s, _ := newTestServerFrom(t, &fakeSource{views: sampleViews(), quota: q, tokenCallOrder: -1})
	_, body := get(t, s, http.MethodGet, "/")

	quotaAt := strings.Index(body, "<caption>レートリミット</caption>")
	gatedAt := strings.Index(body, "<caption>着手できずに止まっているもの</caption>")
	if quotaAt < 0 || gatedAt < 0 || quotaAt > gatedAt {
		t.Fatalf("レートリミットの区画がいちばん上にない（区画 %d / 着手の表 %d）", quotaAt, gatedAt)
	}
	for _, want := range []string{
		"5時間", "1週間（全体）", "1週間（モデル別）",
		"14%", "73%", "95%",
		"2026-08-19T15:05:02Z", "あと3時間5分", "あと3日13時間", "不明",
		"マージンを切った", "新しい issue の着手を止めています",
		"最後に読めた時刻 2026-08-19T11:59:18Z（42秒前）",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("区画に %q が出ていない", want)
		}
	}
	if strings.Contains(body, "入札に使うには古い値") {
		t.Error("新しい値なのに、古い値の印が出ている")
	}
}

// 目的: 期間を出せないときに、なぜ出せないかを区画に1行で出すことを確かめる。
// 与える情報: source が none の写し・起動してからまだ読めていない写し・最後に読めてから時間が経ちすぎた写し。
// 成功条件: それぞれ「読まない設定」「まだ読めていない」「判定に使っていない（最後に読めた時刻）」の文言が出ること。
func TestIndex_期間を出せない理由を出す(t *testing.T) {
	cases := []struct {
		name  string
		quota orchestrator.QuotaView
		want  string
	}{
		{"none", orchestrator.QuotaView{Source: "none"}, "使用率を読まない設定です"},
		{"まだ読めていない", orchestrator.QuotaView{Source: "statusline"}, "まだ使用率を読めていません"},
		{"古すぎる", orchestrator.QuotaView{Source: "statusline", LastReadAt: testTime.Add(-31 * time.Minute)},
			"使用率を判定に使っていません（最後に読めた時刻 2026-08-19T11:29:00Z ／ 31分0秒前）"},
	}
	for _, c := range cases {
		s, _ := newTestServerFrom(t, &fakeSource{quota: c.quota, tokenCallOrder: -1})
		_, body := get(t, s, http.MethodGet, "/")
		if !strings.Contains(body, c.want) {
			t.Errorf("%s: 区画に %q が出ていない", c.name, c.want)
		}
	}
}

// 目的: 切り上げを終えて行き先を待っている run を、JSON と HTML の両方で見分けられること、
// そして `counts.running` の数え方を変えていないことを確かめる。
// 与える情報: 行き先を待つ印の立った run 1件（バックオフ中でない）。
// 成功条件: JSON の waiting_handoff が真、counts.running が 1、HTML に印が出ること。
func TestRun_行き先を待っているrunを見分けられる(t *testing.T) {
	views := []orchestrator.RunView{{Identifier: "octocat/hello-world#3", State: "In Progress", WaitingHandoff: true}}
	s, _ := newTestServer(t, views)

	_, body := get(t, s, http.MethodGet, server.APIStatePath)
	var got struct {
		Counts struct {
			Running int `json:"running"`
		} `json:"counts"`
		Runs []struct {
			WaitingHandoff bool `json:"waiting_handoff"`
			WaitingQuota   bool `json:"waiting_quota"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("JSON を読めなかった: %v", err)
	}
	if len(got.Runs) != 1 || !got.Runs[0].WaitingHandoff || got.Runs[0].WaitingQuota {
		t.Errorf("runs が違う: %+v", got.Runs)
	}
	if got.Counts.Running != 1 {
		t.Errorf("counts.running が %d（want 1。行き先を待つ run も running に数える）", got.Counts.Running)
	}
	if !strings.Contains(body, `"waiting_handoff": true`) {
		t.Errorf("JSON に waiting_handoff が出ていない: %s", body)
	}

	_, html := get(t, s, http.MethodGet, "/")
	if !strings.Contains(html, "切り上げ済み・行き先待ち") {
		t.Error("HTML に行き先を待っている印が出ていない")
	}
}
