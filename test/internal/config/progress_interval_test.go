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
// 与える情報: 5通りの組み合わせを書いた front matter。
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
		{"0 は弾く", "0", "64800000", true},
		{"負は弾く", "-1", "64800000", true},
		{"期限と同じは弾く", "64800000", "64800000", true},
		{"期限より長いのは弾く", "64800001", "64800000", true},
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
