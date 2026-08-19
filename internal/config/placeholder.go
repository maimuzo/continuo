package config

import (
	"fmt"
	"strings"
)

// Placeholder は `continuo init` が置く雛形で、埋めないと動かない文字列のキーに入れる値である
// （設計 3-32 の表）。この値のまま起動したら、キーを名指しして止める。
const Placeholder = "__FILL_ME__"

// PlaceholderProjectNumber は tracker.provider.project_number のプレースホルダである。
// 雛形は 0 を置く。YAML の数値でなければ読み込み自体が落ちるので、文字列は使えない。
//
// キーそのものが書かれていない場合も 0 になるが、区別しない。区別するにはフィールドを
// *int にする必要があり、そこまでする価値が無い。どちらも「まだ埋めていない」である。
const PlaceholderProjectNumber = 0

// validatePlaceholders は、雛形のプレースホルダが埋められないまま残っていないかを検査する。
//
// validate よりも先に呼ぶこと（設計 3-32）。project_number の 0 は validate の
// 「0より大きい整数にすること」でも落ちるため、順序が逆だと「まだ埋めていない」という
// 本当の原因ではなく、値域の説明が出てしまう。
//
// cfg: unmarshal 済みの Config（展開の前後を問わない。プレースホルダは展開の対象外である）。
// 戻り値: 残っているプレースホルダを全部並べた1つのエラー。残っていなければ nil。
// 1つずつ直させると、埋め忘れの数だけ起動をやり直すことになるので、まとめて返す。
func validatePlaceholders(cfg *Config) error {
	var remaining []string

	if cfg.Tracker.Provider.Owner == Placeholder {
		remaining = append(remaining, placeholderItem("tracker.provider.owner", Placeholder))
	}
	if cfg.Tracker.Provider.ProjectNumber == PlaceholderProjectNumber {
		remaining = append(remaining, placeholderItem("tracker.provider.project_number", fmt.Sprint(PlaceholderProjectNumber)))
	}

	if len(remaining) == 0 {
		return nil
	}
	return fmt.Errorf(
		"埋めていない設定が %d 件あります。値を埋めてください: %s",
		len(remaining), strings.Join(remaining, " / "),
	)
}

// placeholderItem はプレースホルダが1件残っていることを表す文言を作る。
//
// key: 設定キーの名前（ドット区切り）。
// value: 雛形が置いているプレースホルダの値。
// 戻り値: 「<キー> がプレースホルダ（<値>）のままです」という1件分の文言。
func placeholderItem(key, value string) string {
	return fmt.Sprintf("%s がプレースホルダ（%s）のままです", key, value)
}
