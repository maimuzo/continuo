// Package config_test のうち、このファイルは
// 「人間ではなく機械が書いた」の印（`config.AIMarker`）と、
// その印を本文へ足す `config.WithAIMarker` の検査を固定する（設計 3-82。issue #245）。
//
// **なぜ要るか。**エージェントも continuo も人間も、同じ GitHub アカウントで投稿する。
// **投稿者でも `author_association` でも見分けられない。**
// **印を先頭へ割り込ませてはならない。**本文の先頭から読む判定が本番に13あり（設計 3-82b）、
// うち CI の3本は利用者のリポジトリへ置いたきりで、continuo の版を上げても書き換わらない。
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
	// **issue #245 が名指ししたのは、この2つである。**
	// `<!-- continuo:agent -->` を流用すると `FetchComments` が `IsAgent` を立て、
	// `<!-- continuo:self -->` を流用すると、そのコメントが次の turn の入力から丸ごと外れる。
	defaults := config.DefaultConfig().Tracker.Comments
	for _, other := range []string{
		defaults.Marker, defaults.SelfMarker,
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
// `handoff.IsMarked`、CI の3本の正規表現が、どれも本文の先頭を見ている。
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

// 目的: 先頭に空白があっても、印を本物の印より前へ置かないことを固定する（設計 3-82）。
//
// **読む側は全部 `strings.TrimSpace(body)` してから先頭を見る**
// （`handoff.IsMarked` / `payloadAfterMarker` / `FetchComments`）。
// **ここで落とさないと、先頭に空行が1つあるだけで印が本物の印より前に入る。**
// そのとき `IsMarked` が偽になり、**continuo は自分が書いた入札も hold も読み戻せない。**
// **担当を人間のものと読み、その issue を二度と拾わない。**
//
// 与える情報: 先頭に空行や空白がある本文。
// 成功条件: 印が持ち回りの印の後ろに入り、TrimSpace したあとの先頭が持ち回りの印のままであること。
func TestWithAIMarker_先頭に空白があっても印を前へ置かない(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"先頭に空行", "\n" + config.HandoffBidMarker + "\n{\"score\":190}\n"},
		{"先頭に空白", "  " + config.HandoffBidMarker + "\n{\"score\":190}\n"},
		{"先頭に全角空白", "　" + config.HandoffBidMarker + "\n{\"score\":190}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := config.WithAIMarker(tc.body)
			if !strings.HasPrefix(strings.TrimSpace(got), config.HandoffBidMarker) {
				t.Fatalf("印が持ち回りの印より前に入りました:\n%q", got)
			}
			if !strings.Contains(got, config.AIMarker) {
				t.Fatalf("印が入っていません:\n%q", got)
			}
		})
	}
}

// 目的: 末尾に改行が無い本文でも、印が前の行に繋がらないことを固定する（設計 3-82）。
//
// **繋がると、1行目が印そのものでなくなる。**
// `FetchComments` の先頭一致も `handoff.IsMarked` も、そこで外れる。
//
// 与える情報: 印だけで、末尾に改行が無い本文。
// 成功条件: 2行になり、1行目が元の印、2行目が機械の印であること。
func TestWithAIMarker_末尾に改行が無くても行を分ける(t *testing.T) {
	const self = "<!-- continuo:self -->"
	want := self + "\n" + config.AIMarker
	if got := config.WithAIMarker(self); got != want {
		t.Fatalf("印が前の行に繋がりました:\n got %q\nwant %q", got, want)
	}
}

// 目的: 印と印のあいだに空行があっても、いちばん後ろの印の直後へ足すことを固定する（設計 3-82）。
//
// **`handoff.StartsAsProgressReport` は空行では止めない。**そちらと決まりを揃える。
// **ただし、後ろの空行までは越えない。**越えると、本文の1行目が印になる。
//
// 与える情報: 印のあいだに空行がある本文と、印のあとに空行がある本文。
// 成功条件: どちらも、最後の印の行の直後に入ること。
func TestWithAIMarker_印のあいだの空行では止めない(t *testing.T) {
	const self = "<!-- continuo:self -->"
	const gated = "<!-- continuo:gated:human_assigned -->"

	got := config.WithAIMarker(self + "\n\n" + gated + "\n担当者が付いています")
	want := self + "\n\n" + gated + "\n" + config.AIMarker + "\n担当者が付いています"
	if got != want {
		t.Errorf("印のあいだの空行で止まりました:\n got %q\nwant %q", got, want)
	}

	// **印のあとに空行が続くときは、その空行を越えない。**
	got = config.WithAIMarker(self + "\n\n本文")
	want = self + "\n" + config.AIMarker + "\n\n本文"
	if got != want {
		t.Errorf("印のあとの空行を越えました:\n got %q\nwant %q", got, want)
	}
}

// 目的: 2行目以降の字下げした行を、印の並びとみなさないことを固定する（設計 3-82）。
//
// **`handoff.StartsAsProgressReport` と同じ決まりに揃える。**
// あちらは本文全体の先頭の空白だけを落とし、**そのあとは行頭ちょうどの `<!--` だけを見る。**
// **だから1行目の字下げは落ち、2行目以降の字下げは落ちない。**
// 読む側（`handoff.IsMarked` / `FetchComments`）も `TrimSpace(body)` してから
// 先頭を見るので、**1行目の字下げは、その3つで同じように落ちる。**
//
// **2行目以降を印とみなすと、印について説明する成果の報告の途中へ、印が差し込まれる。**
//
// 与える情報: 1行目だけが印で、2行目が字下げした HTML コメントである本文。
// 成功条件: 印が1行目の直後に入り、字下げした2行目は本文として扱われること。
func TestWithAIMarker_2行目以降の字下げは印とみなさない(t *testing.T) {
	body := "<!-- continuo:agent -->\n    <!-- continuo:progress -->\nこれは見本です"
	want := "<!-- continuo:agent -->\n" + config.AIMarker +
		"\n    <!-- continuo:progress -->\nこれは見本です"
	if got := config.WithAIMarker(body); got != want {
		t.Fatalf("字下げした2行目を印として数えています:\n got %q\nwant %q", got, want)
	}
}

// 目的: 1行目の字下げは、読む側と同じように落ちることを固定する（設計 3-82）。
//
// **`handoff.IsMarked` も `FetchComments` も `TrimSpace(body)` してから先頭を見る。**
// **つまり、字下げした1行目の印は、その2つでは印として通る。**
// **ここだけ通さないと、印がその行より前に入り、2つの判定が同時に外れる。**
//
// 与える情報: 1行目が4桁字下げの持ち回りの印である本文。
// 成功条件: 印がその行の後ろに入り、TrimSpace したあとの先頭が持ち回りの印のままであること。
func TestWithAIMarker_1行目の字下げは読む側と同じように落ちる(t *testing.T) {
	body := "    " + config.HandoffBidMarker + "\n{\"score\":190}\n"
	got := config.WithAIMarker(body)
	if !strings.HasPrefix(strings.TrimSpace(got), config.HandoffBidMarker) {
		t.Fatalf("印が持ち回りの印より前に入りました:\n%q", got)
	}
}

// 目的: どんな本文でも「TrimSpace したあとの先頭の1行」が変わらないことを固定する（設計 3-82）。
//
// **これがこの印のいちばん大事な約束である。**
// 読む側（`handoff.IsMarked` / `payloadAfterMarker` / `FetchComments`）は
// **どれも `TrimSpace(body)` してから先頭を見る。**
// **その1行が変われば、その全部が同時に外れる。**
//
// **1件ずつの検査では、この約束を守り切れない。**空行・空白・字下げ・改行の有無の
// 組み合わせは、思いつくたびに増える。**組み合わせて全部に当てる。**
//
// 与える情報: 印の並び・空白・改行の有無を組み合わせた本文。
// 成功条件: 元の本文が印で始まっていたなら、印を足したあとも同じ印で始まっていること。
func TestWithAIMarker_先頭の1行を変えない(t *testing.T) {
	heads := []string{
		"<!-- continuo:self -->",
		config.HandoffBidMarker,
		config.HandoffHoldMarker,
		config.HandoffReleasedMarker,
		"<!-- continuo:agent -->",
	}
	prefixes := []string{"", " ", "  ", "\n", "\n\n", "　", "    ", "\t"}
	bodies := []string{"本文", "{\"score\":190}\n\n散文。\n", "", "本文\n"}

	for _, head := range heads {
		for _, prefix := range prefixes {
			for _, body := range bodies {
				src := prefix + head
				if body != "" {
					src += "\n" + body
				}
				got := config.WithAIMarker(src)
				if !strings.HasPrefix(strings.TrimSpace(got), head) {
					t.Errorf("先頭の1行が変わりました。読む側の先頭一致が全部外れます\n"+
						" 入力 %q\n 出力 %q", src, got)
				}
				if !strings.Contains(got, config.AIMarker) {
					t.Errorf("印が入っていません\n 入力 %q\n 出力 %q", src, got)
				}
				// **二度通しても増えない。**
				if again := config.WithAIMarker(got); again != got {
					t.Errorf("二度通すと印が増えました\n 1回目 %q\n 2回目 %q", got, again)
				}
			}
		}
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

// 目的: 閉じの後ろに本文が続く1行の、前へ印を入れないことを固定する（設計 3-82）。
//
// **読む側は `TrimSpace(body)` してから先頭を見る。**
// `<!-- continuo:bid --> 立候補` のような1行を先頭に持つ本文は、
// **`handoff.IsMarked` からは持ち回りのコメントに見える。**
// **そこへ印を先に入れると、先頭一致が全部外れる。**
//
// 与える情報: 閉じの後ろに本文が続く1行で始まる本文。
// 成功条件: TrimSpace したあとの先頭が、元の印のままであること。
func TestWithAIMarker_閉じの後ろに本文が続く行の前へ入れない(t *testing.T) {
	body := config.HandoffBidMarker + " 立候補しています。\n{\"score\":190}\n"
	got := config.WithAIMarker(body)
	if !strings.HasPrefix(strings.TrimSpace(got), config.HandoffBidMarker) {
		t.Fatalf("印が持ち回りの印より前に入りました:\n%q", got)
	}
}

// 目的: 先頭の空白だけの行を作らないことを固定する（設計 3-82）。
//
// **`元の本文は1文字も書き換えない` という約束のうち、行の増やし方に当たる。**
// 空白だけの1行目が残ると、issue の画面で空行から始まるコメントになる。
//
// 与える情報: 先頭に空白があり、印を1つも持たない本文。
// 成功条件: 1行目が印であること。
func TestWithAIMarker_空白だけの行を残さない(t *testing.T) {
	got := config.WithAIMarker("  素の本文")
	want := config.AIMarker + "\n素の本文"
	if got != want {
		t.Fatalf("空白だけの行が残りました:\n got %q\nwant %q", got, want)
	}
}

// 目的: CRLF の本文へ足す行も CRLF で終わることを固定する（設計 3-82）。
//
// **git の失敗をそのまま貼る経路があるので、CRLF が混じりうる。**
// **1行だけ LF になると、投稿した本文の改行が揃わない。**
//
// 与える情報: CRLF の本文。
// 成功条件: 印の行が CRLF で終わり、先頭の1行が変わらないこと。
func TestWithAIMarker_CRLFの本文にはCRLFで足す(t *testing.T) {
	body := config.HandoffBidMarker + "\r\n{\"score\":190}\r\n"
	want := config.HandoffBidMarker + "\r\n" + config.AIMarker + "\r\n{\"score\":190}\r\n"
	if got := config.WithAIMarker(body); got != want {
		t.Fatalf("改行の綴りが揃いません:\n got %q\nwant %q", got, want)
	}
}

// 目的: 複数行の HTML コメントの中へ印を差し込まないことを固定する（設計 3-82）。
//
// **`<!--` で始まり、同じ行に `-->` が無い行は、複数行のコメントの開きである。**
// 印の行として数えると、**その中へ印を入れてしまう。**
// **issue の画面には出ず、本文は `<!--` で始まったまま残る。**
//
// 与える情報: 複数行の HTML コメントで始まる本文。
// 成功条件: 印がコメントの中へ入らず、本文の先頭に来ること。
func TestWithAIMarker_複数行のコメントの中へ入れない(t *testing.T) {
	body := "<!--\n覚え書き\n-->\n本文"
	want := config.AIMarker + "\n" + body
	if got := config.WithAIMarker(body); got != want {
		t.Fatalf("複数行のコメントの中へ印を入れました:\n got %q\nwant %q", got, want)
	}
}

// 目的: 字下げして引用した印は、名乗りとして数えないことを固定する（設計 3-82）。
//
// **組み込みの指示書は、印を4桁字下げのコード片として見せている。**
// **それを引用した報告に名乗りの印が付かないと、その報告だけ人間が書いたものと見分けが付かない。**
// **だから、字下げした印は「既に付いている」と数えない。**
//
// 与える情報: 2行目に字下げした印がある本文。
// 成功条件: 名乗りの印が1行目の直後に入り、字下げした行はそのまま残ること。
func TestWithAIMarker_字下げした印は名乗りに数えない(t *testing.T) {
	body := "<!-- continuo:self -->\n  " + config.AIMarker + "\n本文"
	want := "<!-- continuo:self -->\n" + config.AIMarker + "\n  " + config.AIMarker + "\n本文"
	if got := config.WithAIMarker(body); got != want {
		t.Fatalf("字下げした引用を名乗りとして数えました:\n got %q\nwant %q", got, want)
	}
}

// 目的: 改行が CRLF でも、先頭の1行が変わらないことを固定する（設計 3-82）。
//
// **git の失敗をそのまま貼る経路があるので、CRLF が混じりうる**
// （`internal/orchestrator` の `postComment`）。
// **先頭の1行さえ変わらなければ、読む側の先頭一致は全部通る。**
//
// 与える情報: CRLF の本文。
// 成功条件: TrimSpace したあとの先頭が、元の印のままであること。
func TestWithAIMarker_CRLFでも先頭の1行を変えない(t *testing.T) {
	body := config.HandoffBidMarker + "\r\n{\"score\":190}\r\n"
	got := config.WithAIMarker(body)
	if !strings.HasPrefix(strings.TrimSpace(got), config.HandoffBidMarker) {
		t.Fatalf("先頭の1行が変わりました:\n%q", got)
	}
	if !strings.Contains(got, config.AIMarker) {
		t.Fatalf("印が入っていません:\n%q", got)
	}
}
