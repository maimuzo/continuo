// 成果の報告と途中経過の報告を見分ける判定の検査である
// （issue #178（途中経過を1回書いたエージェントが最後の報告を忘れても、continuo が書き直させない））。
//
// **外部へ1回も接続しない。**文字列を渡して結果を見るだけである。
package handoff_test

import (
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/handoff"
)

// 目的: 途中経過の報告かどうかを、本文の先頭にある印の並びだけで見分けることを固定する
// （issue #178）。
//
// **なぜ要るか。**`hasRunComment` は、これが真になるコメントを「この run の成果の報告」から外す。
// **本文のどこかに印があれば真、という緩い判定をここへ持ち込んではならない。**
// **成果の報告が印を引用しただけで捨てられ、continuo はセッションを復元する。**
// **2度目も引用されれば `failure_state` へ落として人間へ渡す。**
// **書いてあるのに、書かなかったことにされる。**
//
// **印について説明する報告ほど起きやすい。**issue #178 の作業そのもので実際に起きた。
//
// **持ち回りの死活の判定（`IsProgressReport`）は緩いままでよい**（設計 5-3l）。
// **あちらを厳しくすると、書き足し続けている担当が18時間で外れる。**求める向きが逆である。
//
// 与える情報: 組み込みが書かせる形・印を引用した成果の報告・印の無い成果の報告など。
// 成功条件: 先頭の印の並びに印があるものだけが真になること。
func TestStartsAsProgressReport_先頭の印の並びだけを見る(t *testing.T) {
	for _, c := range []struct {
		name string
		body string
		want bool
	}{
		{
			"組み込みが書かせる形",
			"<!-- continuo:agent -->\n" + config.ProgressMarker + "\nまだ作業中です。",
			true,
		},
		{
			"印のあいだに空行がある",
			"<!-- continuo:agent -->\n\n" + config.ProgressMarker + "\nまだ作業中です。",
			true,
		},
		{"印だけ", config.ProgressMarker, true},
		{
			"**成果の報告が印を引用しただけ**",
			"<!-- continuo:agent -->\n進捗報告の印（" + config.ProgressMarker + "）を数えないようにしました。",
			false,
		},
		{
			"成果の報告が印を表の中で引用している",
			"<!-- continuo:agent -->\n## やったこと\n\n| 何 | 中身 |\n| --- | --- |\n" +
				"| 印 | `" + config.ProgressMarker + "` を数えない |",
			false,
		},
		{
			// **4桁の字下げは、組み込みのプロンプトが印を「見せる」ときの書き方そのものである。**
			// **印について説明する成果の報告が、いちばん取りやすい形で引用してくる。**
			// **字下げを許すと、その報告が捨てられて人間へ渡る。**
			"**成果の報告が字下げしたコード片で引用している**",
			"<!-- continuo:agent -->\n\n    " + config.ProgressMarker + "\n\nこの印を数えないようにしました。",
			false,
		},
		{
			"字下げした行のあとに本物の印が来ても、字下げで止まる",
			"<!-- continuo:agent -->\n    見本です\n" + config.ProgressMarker,
			false,
		},
		{
			// **本文全体の先頭の空白は落とす。**`Comment.IsAgent` は
			// `strings.TrimSpace(body)` してから印を見るので、落とさないと2つの判定がずれる。
			// **ずれると、進捗報告が「この run の成果の報告」として数えられ、
			// issue #178 がその形で直らない。**
			"**本文の先頭に空白がある進捗報告**",
			"  <!-- continuo:agent -->\n" + config.ProgressMarker + "\nまだ作業中です。",
			true,
		},
		{
			"本文の先頭が改行から始まる進捗報告",
			"\n\n<!-- continuo:agent -->\n" + config.ProgressMarker + "\nまだ作業中です。",
			true,
		},
		{
			// **`IsAgent` は `strings.TrimSpace` で判定する。**あちらは `unicode.IsSpace` なので、
			// **全角空白も落ちる。**こちらの落とし方が狭いと、全角空白で始まる進捗報告が
			// 「この run の成果の報告」として数えられ、issue #178 がその形のまま再発する。
			// **日本語で書く利用者の手元でいちばん起きやすい。**
			"**本文の先頭が全角空白の進捗報告**",
			"　<!-- continuo:agent -->\n" + config.ProgressMarker + "\nまだ作業中です。",
			true,
		},
		{"ふつうの成果の報告", "<!-- continuo:agent -->\n実装しました", false},
		{"印が無い", "ここはどうなっていますか", false},
		{"空", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := handoff.StartsAsProgressReport(c.body); got != c.want {
				t.Errorf("判定が違う: got %v, want %v（本文 %q）", got, c.want, c.body)
			}
		})
	}
}

// 目的: 2つの判定が別のものであることを固定する（issue #178。設計 5-3l）。
//
// **同じ実装へ寄せてはならない。**寄せた瞬間、どちらかの落とし穴が開く。
//
// | 何を数えるか | 緩さ | 緩すぎると何が起きるか | 厳しすぎると何が起きるか |
// | 持ち回りの死活（`IsProgressReport`） | 本文のどこでも | 人間が印を書くと、その1件で死活が効かない | **書き足している担当が18時間で外れる** |
// | 成果の報告か（`StartsAsProgressReport`） | 先頭の印の並びだけ | **書いた run が人間へ渡る** | 途中経過が成果として数えられ、書き直させない |
//
// 与える情報: 印を引用した成果の報告。
// 成功条件: 死活の判定は真、成果の報告の判定は偽になること。
func TestStartsAsProgressReport_死活の判定とは別物である(t *testing.T) {
	body := "<!-- continuo:agent -->\nこの印（" + config.ProgressMarker + "）を数えないようにしました。"

	if !handoff.IsProgressReport(body) {
		t.Error("死活の判定が偽になっています。" +
			"あちらは本文のどこかに印があれば真であるべきです（設計 5-3l）")
	}
	if handoff.StartsAsProgressReport(body) {
		t.Error("成果の報告の判定が真になっています。" +
			"引用しただけの報告が捨てられ、書いたのに人間へ渡ります（issue #178）")
	}
}
