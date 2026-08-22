// `continuo hook` が「hook をどう扱ったか」をログに残すための表示名の検査である。
//
// **この語がログに出る。**取り違えると、socket へ届いたのか逃がし先へ書いたのかを
// 人間が読み分けられなくなる。
package hookclient_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/hookclient"
)

// TestOutcome_String_扱いを短い語で言い分ける は、3つの結末を言い分けることを確かめる。
//
// 目的: 送れた・逃がした・捨てた をそれぞれ別の語にすること。
// 与える情報: 3つの Outcome と、定義していない値。
// 成功条件: 3つが違う語になり、定義外の値でも中身の分かる文字列を返すこと
// （**空文字を返すと、ログに何も残らない**）。
func TestOutcome_String_扱いを短い語で言い分ける(t *testing.T) {
	got := map[string]string{
		"sent":    hookclient.OutcomeSent.String(),
		"spilled": hookclient.OutcomeSpilled.String(),
		"dropped": hookclient.OutcomeDropped.String(),
	}
	for want, actual := range got {
		if actual != want {
			t.Errorf("%s の表示名が違う: %q", want, actual)
		}
	}
	// **3つとも別の語であること。**同じ語だとログから区別できない。
	seen := map[string]bool{}
	for _, v := range got {
		if seen[v] {
			t.Errorf("表示名が重複している: %q", v)
		}
		seen[v] = true
	}

	// **定義していない値でも中身が分かること。**
	unknown := hookclient.Outcome(99).String()
	if unknown == "" {
		t.Error("定義外の値で空文字を返している（ログに何も残らない）")
	}
	if !strings.Contains(unknown, "99") {
		t.Errorf("定義外の値が何だったか分からない: %q", unknown)
	}
}
