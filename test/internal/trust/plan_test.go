package trust_test

import (
	"strings"
	"testing"
)

// assertSameStrings は文字列の並びが期待どおりであることを確かめる。
//
// t: テストコンテキスト。
// label: 失敗メッセージに出す呼び名。
// want: 期待する並び。
// got: 実際の並び。
func assertSameStrings(t *testing.T, label string, want, got []string) {
	t.Helper()
	if strings.Join(want, "\x00") != strings.Join(got, "\x00") {
		t.Errorf("%s が想定と違う: got %v, want %v", label, got, want)
	}
}

// containsSubstring は文字列の一覧のどれかが、指定した部分文字列を含むかを返す。
//
// lines: 調べる文字列の一覧。
// sub: 含まれていてほしい部分文字列。
// 戻り値: どれか1つでも含んでいれば真。
func containsSubstring(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}
