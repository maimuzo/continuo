// Package normalize は、外部コマンド（git・gh・herdr の socket API 等）へ渡す識別子を
// 安全な文字列へ正規化する処理を一元化する（docs/plans/continuo_design.md 3-7）。
//
// herdr-symphony は正規化を迂回できる経路が残っており、コロンを含む識別子で失敗していた
// （3-7）。continuo では「外部へ渡す名前を作る関数はここ1本だけ」とし、その戻り値の型
// （SafeName）を通さない文字列は外部コマンドの引数として使えないようにする。
package normalize

import (
	"fmt"
	"strings"
)

// SafeName は Normalize を通った文字列だけが持てる型である。
// 外部コマンドの引数はこの型からしか組み立てられないようにすることで、
// 正規化を迂回する経路が生まれるのを防ぐ（3-7）。
type SafeName string

// String は SafeName の中身をそのまま返す。
func (n SafeName) String() string {
	return string(n)
}

// Warning は Normalize が情報を落としたときに返す警告である。
// 「正規化で情報が落ちる場合は警告として記録する。黙って別名にしない」という
// 3-7 の要求どおり、呼び出し側が人間に見せられる形で持たせる。
type Warning struct {
	// Original は正規化する前の元の文字列である。
	Original string
	// Result は正規化した後の SafeName である。
	Result SafeName
	// Message は人間に見せる警告文である。
	Message string
}

// allowedRune は SafeName が許容する1文字を判定する。
// 許容するのは英数字・アンダースコア・ハイフン・ドット・スラッシュだけである。
//
// なぜこの集合か:
//   - 英数字・アンダースコア・ハイフンは herdr の agent 名の許容文字集合
//     （`^[a-z][a-z0-9_-]{0,31}$`。3-3）のスーパーセットになる
//   - ドット・スラッシュは git の branch 名（`continuo/{{.issue.owner}}/...`。3-22）に必要
//   - コロンは含めない。herdr-symphony はコロンを含む識別子で失敗した実例がある（3-7）
//   - シェルやコマンドラインパーサでオプションの開始・区切りに使われる文字
//     （空白・引用符・`;` `|` `&` など）はすべて対象外にする
func allowedRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '_' || r == '-' || r == '.' || r == '/':
		return true
	default:
		return false
	}
}

// Normalize は raw を外部コマンドへ渡しても安全な SafeName に変換する。
//
// raw: 変換前の識別子（issue の URL・branch 名の材料など）。
// 戻り値の1つ目: 変換後の SafeName。allowedRune が許可しない文字はすべて "_" に
// 置き換える。結果が空文字になる場合は "_" 1文字にする。結果の先頭が "-" になる場合は
// コマンドラインオプションとして誤解釈されるのを防ぐため、先頭に "_" を1文字補う。
// 戻り値の2つ目: 情報が落ちた場合の警告のスライス。1文字でも置換・補完が起きれば
// 必ず1件積む。何も落ちていなければ nil を返す。黙って別名にせず、呼び出し側が
// 警告を人間（ログ・issue のコメント等）に見せられるようにするためである（3-7）。
func Normalize(raw string) (SafeName, []Warning) {
	var b strings.Builder
	lost := false
	for _, r := range raw {
		if allowedRune(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
			lost = true
		}
	}

	result := b.String()
	if result == "" {
		result = "_"
		lost = true
	}
	if strings.HasPrefix(result, "-") {
		result = "_" + result
		lost = true
	}

	if !lost {
		return SafeName(result), nil
	}

	return SafeName(result), []Warning{{
		Original: raw,
		Result:   SafeName(result),
		Message:  fmt.Sprintf("識別子の正規化で情報が落ちました: 元の文字列 %q は %q に変換されました", raw, result),
	}}
}

// CommandArgs は SafeName のスライスから、外部コマンドへ渡す引数の文字列スライスを作る。
//
// 3-7 が求める「外部コマンドへ渡す引数は SafeName の型しか受け付けない」を実現するための
// 唯一の変換経路である。worktree の管理・herdr クライアント等、後続の段階で git や herdr の
// コマンドライン引数・API 引数を組み立てるときは、必ずこの関数を経由すること。
// 生の string を直接引数へ混ぜる経路を作らない。
//
// names: 外部コマンドへ渡す SafeName の並び。
// 戻り値: names をそのまま string へ変換した並び。
func CommandArgs(names ...SafeName) []string {
	args := make([]string, len(names))
	for i, n := range names {
		args[i] = string(n)
	}
	return args
}
