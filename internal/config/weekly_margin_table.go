package config

import (
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/maimuzo/continuo/internal/i18n"
)

// WeeklyMarginTable は、`tracker.provider.handoff.weekly_margin_percent_by_days_until_reset` の表である（issue #324）。
// **キーは1週間の枠のリセットまでの残り日数（1〜`MaxWeeklyMarginDaysUntilReset`）、値はその日のマージン（%）。**
//
// **値の無い日の行（`1:` だけ）を、読み込みの時点で弾くために型を分けている**（実装レビュー5周目の HIGH）。
// goccy/go-yaml は `map[int]int` の値が null のとき 0 を入れ、`yaml.Strict()` でも誤りにしない。
// 0 は「その日はマージンを切らずに使用率100%まで使う」という、いちばん緩い値なので、
// 値を後で入れるつもりで `1:` だけを書いた人は、気づかないまま最後の日を使い切られる。
//
// **表そのものを書かない行（個人の上書きで `weekly_margin_percent_by_days_until_reset:` だけを書く）は、ここへ来ない。**
// goccy は null の欄に今の値を残すので、`WORKFLOW.md` の表が残る（`test/internal/config/local_test.go`）。
// `{}` は空の表になる。
type WeeklyMarginTable map[int]int

// UnmarshalYAML は、表を読む。値の無い日と、値が整数でない日（小数・文字列）があれば誤りを返す
// （goccy/go-yaml の `InterfaceUnmarshaler`）。
//
// unmarshal: goccy が渡す、この欄の中身を読む関数。
// 戻り値: 値の無い日か整数でない日があれば、その日（小さい順で最初のもの）を名指す誤り。
func (t *WeeklyMarginTable) UnmarshalYAML(unmarshal func(any) error) error {
	// **値は any で読み、型を自分で見る。**`map[int]int`（や `map[int]*int`）へ直に読むと、goccy は
	// 小数を黙って切り捨て（`1: 0.5` が 0 になる）、引用符で囲んだ数字の文字列も数として通す
	// （実装レビュー6周目の LOW）。0 は「使い切る」なので、書いた人が気づかないまま最後の日を使い切られる。
	var raw map[int]any
	if err := unmarshal(&raw); err != nil {
		return err
	}
	days := make([]int, 0, len(raw))
	for d := range raw {
		days = append(days, d)
	}
	sort.Ints(days)
	out := make(WeeklyMarginTable, len(raw))
	for _, d := range days {
		v, ok := integerValue(raw[d])
		if raw[d] == nil {
			return i18n.Errorf(i18n.KeyConfigValidateHandoffMarginDaysNoValue, d)
		}
		if !ok {
			return i18n.Errorf(i18n.KeyConfigValidateHandoffMarginDaysNotInteger, d, valueText(raw[d]))
		}
		out[d] = v
	}
	*t = out
	return nil
}

// integerValue は、YAML から any で読んだ値が整数ならその値を返す。
//
// goccy/go-yaml は、符号の無い整数を uint64、負の整数を int64、小数を float64、文字列を string で返す。
// **範囲は見ない。**0以上100未満かは `validateHandoff` が見る（`weekly_margin_percent` と同じ文面で弾くため）。
//
// v: 読んだ値。
// 戻り値の1つ目: 整数の値。
// 戻り値の2つ目: 整数なら true。
func integerValue(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		if n > uint64(math.MaxInt) {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

// valueText は、誤りの文面に出す値を、書いたとおりに読める形にする。
//
// **文字列は引用符を付けて出す。**`2: "5"` と書いた人に「値 5」と出すと、引用符が原因だと分からない（実装レビュー7周目の LOW）。
//
// v: 読んだ値。
// 戻り値: 文面に出す文字列。
func valueText(v any) string {
	if s, ok := v.(string); ok {
		return strconv.Quote(s)
	}
	return fmt.Sprint(v)
}
