// Package config_test のうち、このファイルは
// `tracker.provider.handoff.progress_interval_ms`（エージェントに進捗報告を書かせる間隔。
// ミリ秒。既定 3600000 = 1時間）の検査を固定する。
//
// **この設定は、continuo が測る値ではない。**送る文面へ分に直して埋めるだけである
// （[docs/plans/continuo_design.md](../../../docs/plans/continuo_design.md) の 5-3n）。
// **測っているのは `idle_timeout_ms`（担当を外すまでの待ち時間。既定18時間）のほうだけである。**
package config_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// 目的: 進捗報告の間隔が、2つの決まりで弾かれることを固定する（設計 5-3n。#194）。
//
// **2つ目がこの検査の本体である。**1つ目（1以上）だけなら、他の数値の検査と同じ形で足りる。
// **2つ目（`idle_timeout_ms` より短い）は、2つのキーの関係を見る唯一の検査である。**
//
// **なぜ関係を見るか。**間隔のほうが長いと、**エージェントが指示どおりに書いていても、
// 書く前に担当が外れる。**そのとき別の機械が入札をやり直し、
// **push していない変更が失われる。**設定を書いた人には、なぜそうなるかが分からない。
//
// 与える情報: 9通りの組み合わせを書いた front matter。
// 成功条件: 既定と短い値は通り、0 と `idle_timeout_ms` 以上は起動が止まること。
func TestLoad_進捗報告の間隔は1以上で期限より短くなければならない(t *testing.T) {
	for _, tc := range []struct {
		name string
		// interval は progress_interval_ms に書く値である。
		interval string
		// idle は idle_timeout_ms に書く値である。
		idle string
		// wantErr は、読み込みが止まってほしいかどうかである。
		wantErr bool
	}{
		{"既定", "3600000", "64800000", false},
		{"短くする", "1800000", "64800000", false},
		{"ちょうど1分は通る", "60000", "64800000", false},
		{"0 は弾く", "0", "64800000", true},
		{"負は弾く", "-1", "64800000", true},
		// **1分に満たないものを弾く。**送る文面へは分に直して埋めるので、
		// **59999 までは全部「0分以上黙らない」になる。**
		{"1分に満たないものは弾く", "59999", "64800000", true},
		{"30秒は弾く", "30000", "64800000", true},
		{"期限と同じは弾く", "64800000", "64800000", true},
		{"期限より長いのは弾く", "64800001", "64800000", true},
		// **期限が 0 のときは、実行時に効く18時間と比べる。**
		// 0 のまま比べると、「0 なら既定の18時間」と案内されて 0 を書いた人だけが検査を失う。
		{"期限が0でも18時間と比べる", "86400000", "0", true},
		{"期限が0で、18時間より短ければ通る", "3600000", "0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// **`validFrontMatter` は既に `tracker:` を持っている。**
			// もう1つ足すと YAML が「同じキーが2度ある」で落ちるので、
			// その下へ入れ子で書き足す。
			front := validFrontMatter +
				"    handoff:\n" +
				"      progress_interval_ms: " + tc.interval + "\n" +
				"      idle_timeout_ms: " + tc.idle + "\n"
			path := writeWorkflow(t, front, "")

			_, err := config.Load(path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("弾かれるはずの組み合わせが通りました（間隔=%s / 期限=%s）", tc.interval, tc.idle)
				}
				if !strings.Contains(err.Error(), "progress_interval_ms") {
					t.Errorf("止めた理由にキーの名前がありません。"+
						"どこを直せばよいかが分かりません: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("通るはずの組み合わせが弾かれました（間隔=%s / 期限=%s）: %v",
					tc.interval, tc.idle, err)
			}
		})
	}
}

// 目的: 設定した値が、送る文面へ分として届くことを固定する（設計 5-3n。#194）。
//
// **この検査が無いと、この変更の目的そのものに番人がいない。**
// `internal/orchestrator/prompt.go` の `/ 60000` を `/ 1000` に書き換えても、
// **`go test ./...` は全部緑のままだった**（敵対的レビューが実測で示した）。
//
// **キーを書き忘れたときの既定も、あわせて確かめる。**
// `docs/upgrading.md` が「何もしなければ、既定の1時間が使われます」と約束している。
// **既存の利用者全員に効く約束である。**
//
// 与える情報: 間隔を書いた WORKFLOW.md と、書かなかった WORKFLOW.md。
// 成功条件: 読み込んだ値が、書いた値（書かなければ既定の3600000）になること。
func TestLoad_進捗報告の間隔は設定した値になる(t *testing.T) {
	for _, tc := range []struct {
		name string
		// yaml は handoff の下へ書き足す行である。空なら書かない。
		yaml string
		// want は読み込んだあとの ProgressIntervalMs である。
		want int
		// wantMinutes は、送る文面へ埋まる分である。
		wantMinutes int
	}{
		{"書かなければ既定の1時間", "", 3600000, 60},
		{"30分にする", "    handoff:\n      progress_interval_ms: 1800000\n", 1800000, 30},
		{"ちょうど1分にする", "    handoff:\n      progress_interval_ms: 60000\n", 60000, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeWorkflow(t, validFrontMatter+tc.yaml, "")
			loaded, err := config.Load(path)
			if err != nil {
				t.Fatalf("読み込みに失敗した: %v", err)
			}
			got := loaded.Config.Tracker.Provider.Handoff.ProgressIntervalMs
			if got != tc.want {
				t.Errorf("progress_interval_ms が %d です（%d のはずです）", got, tc.want)
			}
			// **送る文面へ埋まるのは分である。**ここが割り算の定数を固定する。
			if min := got / 60000; min != tc.wantMinutes {
				t.Errorf("送る文面へ埋まる分が %d です（%d のはずです）。"+
					"internal/orchestrator/prompt.go の割り算を確かめてください", min, tc.wantMinutes)
			}
		})
	}
}
