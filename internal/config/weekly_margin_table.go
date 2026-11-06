package config

import (
	"sort"

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

// UnmarshalYAML は、表を読む。値の無い日があれば誤りを返す（goccy/go-yaml の `InterfaceUnmarshaler`）。
//
// unmarshal: goccy が渡す、この欄の中身を読む関数。
// 戻り値: 値の無い日があれば、その日（小さい順で最初のもの）を名指す誤り。
func (t *WeeklyMarginTable) UnmarshalYAML(unmarshal func(any) error) error {
	var raw map[int]*int
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
		v := raw[d]
		if v == nil {
			return i18n.Errorf(i18n.KeyConfigValidateHandoffMarginDaysNoValue, d)
		}
		out[d] = *v
	}
	*t = out
	return nil
}
