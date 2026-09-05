package server_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/server"
)

// TestAPIState_runをまたぐ累計を出す は、JSON に `cumulative_totals` が出ることを確かめる
// （issue #238）。
//
// 目的: **`totals` は「いま走っている run」の和でしかない。**run が終わると印から外れて
// 消えるので、**長い turn が並んでいる間、`totals` はほぼ常に0になる。**
// run をまたぐ合計を読む口が要る。
//
// 与える情報: 走行中の run 1件（API 応答1件ぶん）と、それより大きい累計。
//
// 成功条件: `totals` はいままでどおり走行中の run の和で、`cumulative_totals` に
// 累計が出ること。**`totals` の中身が変わっていないこと**（外から叩いている人が壊れない）。
func TestAPIState_runをまたぐ累計を出す(t *testing.T) {
	views := []orchestrator.RunView{{
		Identifier: "octocat/hello-world#1",
		Tokens:     orchestrator.TokenUsage{APICalls: 1, Input: 10, CacheCreation: 20, CacheRead: 30, Output: 40},
	}}
	cumulative := orchestrator.TokenUsage{APICalls: 7, Input: 70, CacheCreation: 140, CacheRead: 210, Output: 280}
	s, _ := newTestServerWithCumulative(t, views, cumulative)

	status, body := get(t, s, "GET", server.APIStatePath)
	if status != 200 {
		t.Fatalf("状態コードが違う: got %d, want 200", status)
	}

	var got struct {
		Totals struct {
			APICalls int `json:"api_calls"`
			Total    int `json:"total"`
		} `json:"totals"`
		Cumulative struct {
			APICalls      int `json:"api_calls"`
			Input         int `json:"input"`
			CacheCreation int `json:"cache_creation"`
			CacheRead     int `json:"cache_read"`
			Output        int `json:"output"`
			Total         int `json:"total"`
		} `json:"cumulative_totals"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("JSON を読めない: %v\n%s", err, body)
	}

	// **既存の鍵は1つも変わっていない。**
	if got.Totals.APICalls != 1 || got.Totals.Total != 100 {
		t.Errorf("totals が走行中の run の和でなくなっている: got %+v", got.Totals)
	}
	// **累計は別の鍵で出る。**
	if got.Cumulative.APICalls != 7 || got.Cumulative.Input != 70 ||
		got.Cumulative.CacheCreation != 140 || got.Cumulative.CacheRead != 210 ||
		got.Cumulative.Output != 280 || got.Cumulative.Total != 700 {
		t.Errorf("cumulative_totals が違う: got %+v", got.Cumulative)
	}
}

// TestAPIState_累計はrunの写しより後に取る は、写しが割れないことを確かめる（issue #238）。
//
// 目的: 供給元は run の写しと累計を**別々の錠の中で**返すので、2回の読み取りの間に
// turn が1つ終わりうる。**累計は減らないので、run を先に読めば「累計が走行中の run の
// 合計より小さい」写しは作れない。**逆順にすると、その瞬間だけ小さく見え、
// **10秒ごとに再読み込みする画面で目に留まる。**
//
// 与える情報: 呼ばれた順序を控える供給元。
//
// 成功条件: `TokenTotals` が呼ばれた時点で、`RunViews` が既に呼ばれていること。
func TestAPIState_累計はrunの写しより後に取る(t *testing.T) {
	s, src := newTestServerWithCumulative(t, nil, orchestrator.TokenUsage{})

	if status, _ := get(t, s, "GET", server.APIStatePath); status != 200 {
		t.Fatalf("状態コードが違う: got %d, want 200", status)
	}

	if src.tokenCallOrder < 0 {
		t.Fatal("累計を1度も取っていない")
	}
	if src.tokenCallOrder < 1 {
		t.Fatalf("run の写しより先に累計を取っている（累計が小さく見える写しが作れる）: RunViews の呼び出し回数 %d", src.tokenCallOrder)
	}
}

// TestIndex_累計の表を出す は、HTML に累計の表が出ることを確かめる（issue #238）。
//
// 目的: **同じ表に「run ごと」「合計」「累計」の3つを並べると、見出しも注記も
// run ごとの意味を書いたままになり、どれが何なのかを表から決められない。**
// **別の表にして、自分の見出しと注記を持たせる。**
//
// 与える情報: 走行中の run 1件と、それより大きい累計。
//
// 成功条件: 累計の見出しと、3桁区切りにした累計の合計が本文に出ること。
// **run ごとの表の見出しと注記が、いままでどおり残っていること。**
func TestIndex_累計の表を出す(t *testing.T) {
	views := []orchestrator.RunView{{
		Identifier: "octocat/hello-world#1",
		Tokens:     orchestrator.TokenUsage{APICalls: 1, Input: 10, CacheCreation: 20, CacheRead: 30, Output: 40},
	}}
	cumulative := orchestrator.TokenUsage{APICalls: 7, Input: 700000, CacheCreation: 1, CacheRead: 2, Output: 3}
	s, _ := newTestServerWithCumulative(t, views, cumulative)

	status, body := get(t, s, "GET", "/")
	if status != 200 {
		t.Fatalf("状態コードが違う: got %d, want 200", status)
	}

	// 累計の合計は 700000 + 1 + 2 + 3 = 700006。**3桁区切りで出る。**
	if !strings.Contains(body, "700,006") {
		t.Error("累計の合計が本文に出ていない")
	}
	for _, want := range []string{
		// 累計の表の見出しと注記。
		"run をまたぐ累計",
		"終わった run の分も残る",
		// **run ごとの表は1文字も変わっていない。**
		"requestId で重複排除済み",
		"走行中の turn の分はまだ入っていない",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("本文に %q が無い", want)
		}
	}
}
