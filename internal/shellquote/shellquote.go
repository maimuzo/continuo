// Package shellquote は shell のコマンド行へ埋め込む1語を単一引用符で包む。
//
// **写しを作らない。**もとは internal/orchestrator/settings.go にあった非公開の `shellQuote` で、
// hook のコマンド行の組み立てだけが使っていた。GitHub App のトークンを取るコマンド
// （`continuo github-app token`）を送る文面へ埋める側（internal/prompt の `RenderData` と
// internal/orchestrator の書かせ直し）にも同じ包み方が要るので、両方が import できる
// package へ移した（docs/plans/impl/issue245_github_app_attribution.md の 3-82e）。
// **internal/prompt から internal/orchestrator は import できない**（逆向きに import 済みで、循環する）。
//
// **hook の挙動は変えない。**包み方は移す前と1バイトも変えていない
// （CLAUDE.md の「hook の挙動が変化する変更」の門には当たらない）。
package shellquote

import "strings"

// Quote は shell のコマンド行へ埋め込む1語を単一引用符で包む。
//
// **単一引用符の中では展開が一切起きない**ので、空白・`$`・バッククォート・`;` を
// そのまま渡せる。語の中に単一引用符があれば、「引用を閉じる・逃がした単一引用符を置く・
// 引用を開き直す」の3つを並べた形へ置き換える。
//
// s: 埋め込む1語。
// 戻り値: 引用した文字列。
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
