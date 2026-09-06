// Package prompt_test のうち、このファイルは
// 組み込みの指示書が「人間ではなく機械が書いた」の印（`config.AIMarker`）を
// **どこへ書かせるか**を固定する（設計 3-82。issue #245）。
//
// **見るのは `prompt.Builtin()` である。**`prompt.BuiltinRaw()` ではない。
// **`Builtin()` は行頭ちょうどの `<!--` を落としてからエージェントへ送る**
// （internal/prompt の `stripComments`）。
// **`BuiltinRaw()` を見ると、送る文面から印が消えていても素通りする。**
package prompt_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/prompt"
)

// 印より前に置いてはならない目印の一覧である。
//
// **どれも「本文の先頭がこの目印で始まっているか」で数えられている。**
// **前へ1行入れると、その数え方が外れる。**
// CI の3本（`design-review-result` / `code-review-result` / `design-review-skipped`）は
// `continuo init` が利用者のリポジトリへ置いたきりで、**continuo の版を上げても書き換わらない。**
var markersThatMustComeFirst = []string{
	"<!-- continuo:agent -->",
	"<!-- continuo:group -->",
	"<!-- code-review-result -->",
	"<!-- design-review-result -->",
	config.PlanMarker,
}

// **進捗の印は、この一覧に入れない。**
// `handoff.StartsAsProgressReport` は先頭に並ぶ印を全部辿るので、
// **進捗の印と機械の印は、どちらが先でも判定は変わらない**（組み込みの 5-3 がそう書いている）。
// **見本の並びは、[progress_comment_test.go](progress_comment_test.go) が行の位置で固定している。**

// 目的: エージェントがコメントを書く節すべてに、印を書かせていることを固定する（設計 3-82）。
//
// **1箇所でも落ちると、そこだけ「人間が書いた」と読まれる。**
// **印の値は `config.AIMarker` から取る。**リテラルで書くと、定数を変えたときに
// **エージェントは古い印を書き続けるのに、この検査は落ちない。**
//
// 与える情報: prompt.Builtin() の、コメントを書かせる節。
// 成功条件: どの節にも印が入っていること。
func TestTemplate_コメントを書かせる節すべてに機械の印がある(t *testing.T) {
	body := prompt.Builtin()

	for _, heading := range []string{
		"## 3-2. 計画を書き、レビューを受ける",
		"## 3-6. pull request のレビューを受ける",
		"## 3-7. 終わりを書く",
		"## 5-3. {{.progress_interval_minutes}}分以上黙らない",
		// **5-5 は、人間へ読ませるコメントの骨組みを決めている節である。**
		// **その骨組みに印が無いと、骨組みから書いたコメントだけ印が落ちる。**
		"## 5-5. 人間へ質問・報告するコメントの書き方",
		"## 5-6. あなたが書くコメントには、機械が書いた印を付ける",
		"## 7-2. まとめて直したとき",
	} {
		section := sectionOf(t, body, heading)
		if !strings.Contains(section, config.AIMarker) {
			t.Errorf("%q の節が %s を書かせていません。"+
				"この節が書かせるコメントだけ、人間が書いたものと見分けが付かなくなります",
				heading, config.AIMarker)
		}
	}
}

// 目的: 印を、既にある目印より前へ書かせていないことを固定する（設計 3-82）。
//
// **これがこの issue でいちばん壊しやすいところである。**
// 印を先頭へ置くと、continuo の先頭一致も、CI の3本の正規表現も同時に外れる。
// **CI は利用者のリポジトリの中にあり、continuo の版を上げても書き換わらない。**
// **その project の pull request が全部赤になる。**
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 印が現れる行より前に、その見本の目印が全部出ていること。
func TestTemplate_機械の印は既存の目印より後ろに書かせる(t *testing.T) {
	lines := strings.Split(prompt.Builtin(), "\n")

	found := 0
	for i, line := range lines {
		// **注釈が付いた行も見る。**5-6 の図は `<!-- continuo:ai -->         ← ここへ足す` と書いており、
		// **完全一致で絞ると、置き場所を教えている唯一の図が検査から漏れる。**
		if !strings.HasPrefix(strings.TrimSpace(line), config.AIMarker) {
			continue
		}
		found++
		// **同じ見本の中で、この行より後ろに出ている目印を探す。**
		// 見本は空行で切れるので、直前の空行までを1つの見本とみなす。
		start := i
		for start > 0 && strings.TrimSpace(lines[start-1]) != "" {
			start--
		}
		for _, marker := range markersThatMustComeFirst {
			// この見本がその目印を後ろで使っていなければ、順序を問わない。
			// **空行で止めない。**印と印のあいだに空行を挟む書き方があり、
			// **止めると、その並びが検査から漏れる。**
			// **本文が始まったら止める**（空行でも印の行でもない行）。
			usedAfter := false
			for j := i + 1; j < len(lines); j++ {
				trimmed := strings.TrimSpace(lines[j])
				if trimmed != "" && !strings.HasPrefix(trimmed, "<!--") {
					break
				}
				if strings.Contains(lines[j], marker) {
					usedAfter = true
					break
				}
			}
			if !usedAfter {
				continue
			}
			// **末尾を越えて切らない。**印が最後の3行に入っていると panic する。
			end := min(i+3, len(lines))
			t.Errorf("%d 行目の %s が %s より前にあります。"+
				"先頭の目印を数えている continuo と CI が、同時に外れます\n見本:\n%s",
				i+1, config.AIMarker, marker, strings.Join(lines[start:end], "\n"))
		}
	}
	if found == 0 {
		t.Fatal("組み込みの指示書に " + config.AIMarker + " が1行もありません（検査が的を外しています）")
	}
}

// **進捗報告の見本の3行目を見る検査は、ここには置かない。**
// 1行目と2行目を見る検査が
// [test/internal/prompt/progress_comment_test.go](progress_comment_test.go) に既にあり、
// **同じ走査を2つ持つと、見本が動いたときに両方を直すことになる。**
// あちらの `TestTemplate_進捗報告の見本は印を行頭から書かせる` へ、3行目の1行を足してある。

// 目的: 設計のレビューの判断票の並びを、行の位置で固定する（設計 3-82）。
//
// **CI の正規表現は `<!-- continuo:agent -->` を任意の1つとして許すだけで、
// あいだに別の行が入ることは許さない**（`^[ \t\r\n]*(<!-- continuo:agent -->[ \t\r\n]*)?<!-- design-review-result -->`）。
// **つまり印を2行目へ入れると、その pull request の検査が永久に赤になる。**
// **`<!-- design-review-result -->` が1行目でも CI は通る**（人間と直接やりとりする AI 向けの形）。
// **ここが縛っているのは、組み込みの指示書が見せる見本のほうである。**
//
// 与える情報: prompt.Builtin() の、計画を書かせる節。
// 成功条件: `<!-- continuo:agent -->` → `<!-- design-review-result -->` → 機械の印 の順であること。
func TestTemplate_設計レビューの判断票は印を3行目に置く(t *testing.T) {
	section := sectionOf(t, prompt.Builtin(), "## 3-2. 計画を書き、レビューを受ける")
	lines := strings.Split(section, "\n")

	found := false
	for i, line := range lines {
		if strings.TrimSpace(line) != "<!-- design-review-result -->" {
			continue
		}
		found = true
		// **`i == 0` を先に返す。**あとで `lines[i-1]` を読むので、
		// まとめて書くと、見本が節の先頭へ動いたときに範囲外で落ちる。
		if i == 0 {
			t.Errorf("判断票の見本で、設計のレビューの目印が節の先頭にあります。" +
				"直前に <!-- continuo:agent --> が要ります")
		} else if strings.TrimSpace(lines[i-1]) != "<!-- continuo:agent -->" {
			t.Errorf("判断票の見本で、設計のレビューの目印の直前が "+
				"<!-- continuo:agent --> ではありません: %q", lines[i-1])
		}
		if i+1 >= len(lines) || strings.TrimSpace(lines[i+1]) != config.AIMarker {
			t.Errorf("判断票の見本の3行目が %s ではありません", config.AIMarker)
		}
	}
	if !found {
		t.Fatal("設計のレビューの判断票の見本が見つかりません（検査が的を外しています）")
	}
}

// 目的: 飛ばす断りには印を付けさせないことを固定する（設計 3-82）。
//
// **`<!-- design-review-skipped -->` の検査だけ、目印の直後に「空白でない文字」を求めている。**
// **理由を書いたかどうかを、そこで数えている。**
// **印を足すと、印そのものがその1文字に当たり、理由の無い断りが通る。**
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 飛ばす断りの目印の次の行が、機械の印になっていないこと。
func TestTemplate_飛ばす断りには印を付けさせない(t *testing.T) {
	lines := strings.Split(prompt.Builtin(), "\n")
	found := false
	for i, line := range lines {
		if strings.TrimSpace(line) != "<!-- design-review-skipped -->" {
			continue
		}
		found = true
		if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == config.AIMarker {
			t.Errorf("%d 行目で、飛ばす断りに %s を付けさせています。"+
				"CI は目印の直後の1文字で理由の有無を数えているので、"+
				"理由を1文字も書かない断りが通ります", i+1, config.AIMarker)
		}
	}
	// **見本が見つからなければ、この検査は何も守っていない。**
	// 見本の形が変わったときに、黙って通り続けるのを防ぐ。
	if !found {
		t.Fatal("飛ばす断りの見本が見つかりません（検査が的を外しています）")
	}
}
