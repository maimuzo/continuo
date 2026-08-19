// Package normalize_test は internal/normalize の正規化規則を検証する。
package normalize_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/normalize"
)

// 目的: 許可されていない文字（ここでは "#" と "@"）を含む識別子を正規化すると、
// その文字が "_" に置き換わり、かつ「情報が落ちた」警告が1件以上返ることを確認する
// （3-7 の「正規化で情報が落ちる場合は警告として記録する。黙って別名にしない」という要求）。
// 与える情報: GitHub issue の識別子の実例 "maimuzo/koetsumugi#188"。
// 成功条件: 戻り値の SafeName に "#" が含まれないこと、かつ warnings が空でないこと。
func TestNormalize_許可されない文字があると警告が返る(t *testing.T) {
	raw := "maimuzo/koetsumugi#188"

	name, warnings := normalize.Normalize(raw)

	if strings.Contains(string(name), "#") {
		t.Errorf("正規化後の文字列に \"#\" が残っている: %q", name)
	}
	if len(warnings) == 0 {
		t.Fatal("情報が落ちているのに警告が1件も返らなかった")
	}
	if warnings[0].Original != raw {
		t.Errorf("警告の Original が元の文字列と一致しない: got %q, want %q", warnings[0].Original, raw)
	}
	if warnings[0].Result != name {
		t.Errorf("警告の Result が戻り値の SafeName と一致しない: got %q, want %q", warnings[0].Result, name)
	}
}

// 目的: すべての文字が許可された文字集合に収まる識別子は、1文字も変わらず、
// 警告も返らないことを確認する（「情報が落ちていないのに警告を出す」誤検知が無いことの確認）。
// 与える情報: 英数字・ハイフン・アンダースコア・ドット・スラッシュだけで構成された文字列。
// 成功条件: 戻り値の SafeName が入力と完全一致し、warnings が nil または空であること。
func TestNormalize_許可された文字だけなら変化せず警告も出ない(t *testing.T) {
	raw := "continuo/maimuzo/koetsumugi/188.retry-1_v2"

	name, warnings := normalize.Normalize(raw)

	if string(name) != raw {
		t.Errorf("許可された文字だけなのに変化した: got %q, want %q", name, raw)
	}
	if len(warnings) != 0 {
		t.Errorf("許可された文字だけなのに警告が返った: %v", warnings)
	}
}

// 目的: 入力そのものが空文字の場合でも、空文字ではなく "_" 1文字を返し、
// かつ警告を返すことを確認する（外部コマンドの引数として空文字を渡すことの危険を避ける）。
// 与える情報: 空文字列。
// 成功条件: 戻り値が "_" であり、警告が返ること。
func TestNormalize_入力が空文字なら結果はアンダースコア1文字になる(t *testing.T) {
	name, warnings := normalize.Normalize("")

	if name != "_" {
		t.Errorf("結果が \"_\" になるべきなのに %q だった", name)
	}
	if len(warnings) == 0 {
		t.Fatal("空文字を \"_\" へ補ったのに警告が返らなかった")
	}
}

// 目的: 許可されない文字が複数含まれる場合、それぞれが個別に "_" へ置き換わることを確認する
// （1文字も情報を「消して詰める」のではなく、位置ごとに置換するという方針の確認）。
// 与える情報: 全角の記号3文字（コロン・アットマーク・クエスチョンマーク）。
// 成功条件: 戻り値が入力と同じ文字数の "_" の並びになり、警告が返ること。
func TestNormalize_許可されない文字は1文字ずつアンダースコアへ置き換わる(t *testing.T) {
	raw := "：＠？"

	name, warnings := normalize.Normalize(raw)

	if string(name) != "___" {
		t.Errorf("3文字それぞれが \"_\" に置き換わるはずが %q だった", name)
	}
	if len(warnings) == 0 {
		t.Fatal("情報が落ちているのに警告が返らなかった")
	}
}

// 目的: 正規化の結果が "-" から始まる場合、外部コマンドのオプションと誤解釈されないよう
// 先頭に "_" が補われ、かつ警告が返ることを確認する。
// 与える情報: 先頭が許可されない文字（そのままなら "-" に化ける文字）で始まる識別子。
// 成功条件: 戻り値が "-" で始まらないこと。
func TestNormalize_結果が先頭ハイフンになる場合は補正される(t *testing.T) {
	raw := "#force"

	name, _ := normalize.Normalize(raw)

	if strings.HasPrefix(string(name), "-") {
		t.Errorf("正規化後の文字列がハイフンで始まっている（外部コマンドのオプションと誤解釈されうる）: %q", name)
	}
}

// 目的: CommandArgs が SafeName のスライスをそのまま string のスライスへ変換することを確認する。
// 与える情報: Normalize を通した SafeName を2つ。
// 成功条件: 戻り値の順序と中身が入力と一致すること。
func TestCommandArgs_SafeNameを文字列スライスへ変換する(t *testing.T) {
	a, _ := normalize.Normalize("owner/repo")
	b, _ := normalize.Normalize("branch-name")

	got := normalize.CommandArgs(a, b)

	if len(got) != 2 || got[0] != string(a) || got[1] != string(b) {
		t.Fatalf("CommandArgs の結果が一致しない: got %v", got)
	}
}

// 目的: パスの要素として ".." や "." になる部分が潰され、警告が返ることを確認する（設計 3-7）。
// SafeName は worktree の置き場所の1階層としてそのまま使われるため、".." を通すと
// 置き場所の外を指せてしまう。git も refname に ".." を許さない。
// 与える情報: "..", "../../etc/passwd", "a/../b", "." を含む入力。
// 成功条件: 結果に ".." と "." の要素が1つも残らず、いずれの入力でも警告が1件返ること。
func TestNormalize_パスの要素になるドットは潰されて警告が返る(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"親ディレクトリそのもの", "..", "__"},
		{"親ディレクトリを重ねる", "../../etc/passwd", "__/__/etc/passwd"},
		{"途中に親ディレクトリが混ざる", "a/../b", "a/__/b"},
		{"カレントディレクトリ", ".", "_"},
		{"branch 名に使う形は保つ", "continuo/acme/repo.js/12", "continuo/acme/repo.js/12"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, warnings := normalize.Normalize(c.raw)
			if got.String() != c.want {
				t.Fatalf("正規化の結果が一致しない: raw=%q got=%q want=%q", c.raw, got.String(), c.want)
			}
			for _, seg := range strings.Split(got.String(), "/") {
				if seg == ".." || seg == "." {
					t.Fatalf("パスの要素に %q が残っている: raw=%q got=%q", seg, c.raw, got.String())
				}
			}
			if c.raw == c.want {
				if len(warnings) != 0 {
					t.Fatalf("何も落ちていないのに警告が返った: raw=%q warnings=%v", c.raw, warnings)
				}
				return
			}
			if len(warnings) != 1 {
				t.Fatalf("情報が落ちたのに警告が1件返らなかった: raw=%q warnings=%v", c.raw, warnings)
			}
			if warnings[0].Original != c.raw || warnings[0].Result != got {
				t.Fatalf("警告の中身が一致しない: got %+v", warnings[0])
			}
		})
	}
}
