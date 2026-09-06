// Package config_test のうち、このファイルは
// 「人間ではなく機械が書いた」の印（`config.AIMarker`）と、
// その印を本文へ足す `config.WithAIMarker` の検査を固定する（設計 3-82。issue #245）。
//
// **なぜ要るか。**エージェントも continuo も人間も、同じ GitHub アカウントで投稿する。
// **投稿者でも `author_association` でも見分けられない。**
// **印を先頭へ割り込ませてはならない。**先頭が特定の印であることを見ている判定が本番に7箇所あり、
// うち CI の2本は利用者のリポジトリへ置いたきりで、continuo の版を上げても書き換わらない。
package config_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// 目的: 印の綴りを固定する（設計 3-82）。
//
// **組み込みのプロンプトがエージェントへ書かせる文字列と、1文字も違ってはならない。**
// 違うと、エージェントが書いた印を、あとから読む人も機械も同じものとして数えられない。
//
// 与える情報: config.AIMarker。
// 成功条件: `<!-- continuo:ai -->` であること。既存の印のどれとも一致しないこと。
func TestAIMarker_綴りが固定されている(t *testing.T) {
	if config.AIMarker != "<!-- continuo:ai -->" {
		t.Fatalf("印の綴りが変わっています: got %q, want %q",
			config.AIMarker, "<!-- continuo:ai -->")
	}
	// **既存の印と食い違っていること。**同じ綴りにすると、continuo の判定が変わる
	// （issue #245 の本文が「既にある2つを流用してはいけません」と決めている）。
	for _, other := range []string{
		config.ProgressMarker, config.PlanMarker,
		config.HandoffBidMarker, config.HandoffHoldMarker, config.HandoffReleasedMarker,
	} {
		if config.AIMarker == other {
			t.Errorf("印が既存の印 %q と同じです。流用すると continuo の判定が変わります", other)
		}
	}
}

// 目的: 印を足す位置が「先頭に並ぶ印のいちばん後ろ」であることを固定する（設計 3-82）。
//
// **先頭へ割り込ませてはならない。**`FetchComments` の先頭一致（`internal/tracker`）、
// `handoff.IsMarked`、CI の2本の正規表現が、どれも本文の先頭を見ている。
//
// 与える情報: continuo が実際に投稿する4つの形。
// 成功条件: 先頭の印が1つも動かず、印が2行目（先頭の印が無ければ1行目）に入ること。
func TestWithAIMarker_先頭の印を動かさずに足す(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			"continuo 自身のコメント",
			"<!-- continuo:self -->\nStatus を動かしました",
			"<!-- continuo:self -->\n" + config.AIMarker + "\nStatus を動かしました",
		},
		{
			"入札のコメント（本文が自分で印を持つ）",
			"<!-- continuo:bid -->\n{\"score\":190}\n\n立候補しています。\n",
			"<!-- continuo:bid -->\n" + config.AIMarker + "\n{\"score\":190}\n\n立候補しています。\n",
		},
		{
			"関門の案内（印が2つ並ぶ）",
			"<!-- continuo:self -->\n<!-- continuo:gated:human_assigned -->\n担当者が付いています",
			"<!-- continuo:self -->\n<!-- continuo:gated:human_assigned -->\n" +
				config.AIMarker + "\n担当者が付いています",
		},
		{
			"印が1つも無い本文",
			"素の本文",
			config.AIMarker + "\n素の本文",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := config.WithAIMarker(tc.body); got != tc.want {
				t.Errorf("印を足した本文が想定と違います:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// 目的: 二重に付けないことを固定する（設計 3-82）。
//
// **付けてしまうと、同じ行が巡回のたびに1行ずつ増える形の間違いを、誰も止められない。**
//
// 与える情報: 既に印を持つ本文。
// 成功条件: 1文字も変わらないこと。
func TestWithAIMarker_既に印があれば足さない(t *testing.T) {
	body := "<!-- continuo:self -->\n" + config.AIMarker + "\n本文"
	if got := config.WithAIMarker(body); got != body {
		t.Fatalf("印が2つになりました:\n got %q\nwant %q", got, body)
	}
	// **先頭に印だけがある形でも足さない。**
	only := config.AIMarker + "\n本文"
	if got := config.WithAIMarker(only); got != only {
		t.Fatalf("印が2つになりました:\n got %q\nwant %q", got, only)
	}
}

// 目的: 字下げした行を印の並びとみなさないことを固定する（設計 3-82）。
//
// **`handoff.StartsAsProgressReport` と同じ決まりに揃える。**
// 字下げした `<!--` は、印について説明する本文であって、名乗りではない。
// **揃えないと、印について書いた成果の報告の途中へ、印が差し込まれる。**
//
// 与える情報: 1行目が4桁字下げの HTML コメントである本文。
// 成功条件: 印が本文の先頭（1行目）に入ること。
func TestWithAIMarker_字下げした行は印とみなさない(t *testing.T) {
	body := "    <!-- continuo:agent -->\nこれは見本です"
	want := config.AIMarker + "\n" + body
	if got := config.WithAIMarker(body); got != want {
		t.Fatalf("字下げした行を印として数えています:\n got %q\nwant %q", got, want)
	}
}

// 目的: 印を足しても、入札の JSON を切り出す側が壊れないことを固定する（設計 3-82）。
//
// **`payloadAfterMarker`（internal/handoff）は、印の後ろの最初の `{` から最後の `}` を取る。**
// **印そのものが `{` か `}` を持つと、その切り出しが壊れる。**
// ここは handoff を通さずに、印の綴りだけで確かめる（依存を増やさないため）。
//
// 与える情報: config.AIMarker。
// 成功条件: `{` も `}` も含まないこと。
func TestAIMarker_中括弧を含まない(t *testing.T) {
	if strings.ContainsAny(config.AIMarker, "{}") {
		t.Fatalf("印が中括弧を含んでいます（%q）。"+
			"入札のコメントから JSON を切り出す処理が壊れます", config.AIMarker)
	}
}
