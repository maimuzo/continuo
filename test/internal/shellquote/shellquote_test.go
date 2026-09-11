// Package shellquote_test は internal/shellquote の包み方を検証する。
package shellquote_test

import (
	"testing"

	"github.com/maimuzo/continuo/internal/shellquote"
)

// 目的: shell のコマンド行へ埋め込む1語が、単一引用符で包まれることを確認する
// （docs/plans/impl/issue245_github_app_attribution.md の 3-82e「変数を2つ足す」）。
//
// **包み方は internal/orchestrator/settings.go にあった `shellQuote` と同じでなければならない。**
// hook のコマンド行と、送る文面の `{{.continuo.command}}` が同じ関数を通るので、
// ここが変わると両方が同時に変わる。
//
// 与える情報: 空白・`$`・バッククォート・単一引用符を含む語。
// 成功条件: 単一引用符で包まれ、語の中の単一引用符だけが `'\”` に置き換わること。
func TestQuote_単一引用符で包む(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"素の語", "/usr/local/bin/continuo", "'/usr/local/bin/continuo'"},
		{"空白を含む", "/Users/octocat/my tools/continuo", "'/Users/octocat/my tools/continuo'"},
		{"ドルとバッククォートは触らない", "a$b`c`", "'a$b`c`'"},
		{"単一引用符は閉じて逃がして開き直す", "it's", `'it'\''s'`},
		{"空文字", "", "''"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shellquote.Quote(tc.in); got != tc.want {
				t.Errorf("Quote(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
