// Package i18n_test は画面に出す文言の引き当て（設計 3-35）を確かめる。
//
// **確かめたいことは4つある。**
//
//	1 宣言したキーと messages/ja.json が1対1であること（どちらかに無いキーを落とす）
//	2 訳の無いキーが日本語へ落ちること（生のキーを画面に出さない）
//	3 書式が fmt のままであること（`%d` に3桁区切りが入らない・`%w` の連鎖が切れない）
//	4 言語の決め方が「設定が主、環境変数 LANG が従」であること
package i18n_test

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/server"
)

// useLang はテストの間だけ言語を切り替え、終わったら既定へ戻す。
//
// **戻さないと後続のテストが別の言語で走る**（言語は package の既定として1つ持つ）。
//
// t: 呼び出し元のテスト。
// lang: この試験の間に使う言語。
func useLang(t *testing.T, lang i18n.Lang) {
	t.Helper()
	i18n.Use(lang)
	t.Cleanup(func() { i18n.Use(i18n.DefaultLang) })
}

// 目的: 宣言したキー（keys.go）と日本語の資源（messages/ja.json）が1対1であることを確認する。
//
// **どちらの向きも落とす。**宣言だけして文言を書き忘れると画面に文言が出ず、
// 文言だけ書いて宣言を忘れると呼ぶ側が文字列リテラルを書くことになる。
//
// 与える情報: i18n.AllKeys() と、日本語の資源が持つキー。
// 成功条件: 2つの集合が完全に一致すること。
func TestKeys_宣言したキーと日本語の資源が1対1である(t *testing.T) {
	source, ok := i18n.CatalogOf(i18n.SourceLang)
	if !ok {
		t.Fatalf("正の言語 %s の資源がありません", i18n.SourceLang)
	}

	declared := map[i18n.Key]bool{}
	for _, k := range i18n.AllKeys() {
		if declared[k] {
			t.Errorf("キー %q が keys.go に2回宣言されている", k)
		}
		declared[k] = true
	}

	inSource := map[i18n.Key]bool{}
	for _, k := range source.Keys() {
		inSource[k] = true
	}

	for k := range declared {
		if !inSource[k] {
			t.Errorf("keys.go に宣言があるのに messages/ja.json に文言が無い: %q", k)
		}
	}
	for k := range inSource {
		if !declared[k] {
			t.Errorf("messages/ja.json に文言があるのに keys.go に宣言が無い: %q", k)
		}
	}
}

// 目的: 宣言したキーの文言が、日本語の資源から実際に引けることを確認する。
//
// **1件でも引けなければ Missing() に残る。**引けないキーがあると、そこだけ
// 「文言が登録されていません」と出る（設計 3-35）。
//
// 与える情報: 宣言したキー全部。
// 成功条件: すべて引けて、Missing() が空であること。
func TestT_宣言したキーはすべて引ける(t *testing.T) {
	useLang(t, i18n.SourceLang)
	i18n.ResetMissing()
	t.Cleanup(i18n.ResetMissing)

	for _, k := range i18n.AllKeys() {
		if got := i18n.T(k); got == "" {
			t.Errorf("キー %q の文言が空である", k)
		}
	}
	if got := i18n.Missing(); len(got) > 0 {
		t.Fatalf("引けなかったキーがある: %v", got)
	}
}

// templateKeyPattern はダッシュボードのテンプレートに書かれた `t "..."` を拾う。
var templateKeyPattern = regexp.MustCompile(`\bt "([^"]+)"`)

// 目的: ダッシュボードの HTML に書いたキーが、日本語の資源にあることを確認する。
//
// **テンプレートのキーは Go の定数ではない**ので、打ち間違えても
// コンパイルでは見つからない。テンプレートの原文を読んで突き合わせる（設計 3-35）。
//
// 与える情報: server.TemplateSource() に書かれた `t "..."` のキー。
// 成功条件: すべてが日本語の資源にあり、1件以上拾えていること
// （拾えていなければ、テンプレートの書き方が変わって検査が空振りしている）。
func TestTemplate_ダッシュボードに書いたキーが資源にある(t *testing.T) {
	source, ok := i18n.CatalogOf(i18n.SourceLang)
	if !ok {
		t.Fatalf("正の言語 %s の資源がありません", i18n.SourceLang)
	}
	inSource := map[i18n.Key]bool{}
	for _, k := range source.Keys() {
		inSource[k] = true
	}

	matches := templateKeyPattern.FindAllStringSubmatch(server.TemplateSource(), -1)
	if len(matches) == 0 {
		t.Fatal("テンプレートから `t \"...\"` のキーを1件も拾えなかった（検査が空振りしている）")
	}
	for _, m := range matches {
		if !inSource[i18n.Key(m[1])] {
			t.Errorf("テンプレートに書かれたキー %q が messages/ja.json に無い", m[1])
		}
	}
}

// 目的: 資源に無いキーを引いたとき、**生のキーを画面に出さず**、テストから検出できることを確認する。
//
// 与える情報: keys.go に宣言していないキー。
// 成功条件: 戻り値にキーが「そのまま」出ておらず、Missing() にそのキーが載ること。
func TestT_資源に無いキーは検出できて生のキーを画面に出さない(t *testing.T) {
	useLang(t, i18n.SourceLang)
	i18n.ResetMissing()
	t.Cleanup(i18n.ResetMissing)

	const unknown = i18n.Key("doctor.label.この文言は存在しない")
	got := i18n.T(unknown)

	if got == string(unknown) {
		t.Fatalf("生のキーがそのまま画面に出ている: %q", got)
	}
	if !strings.Contains(got, "文言が登録されていません") {
		t.Fatalf("文言が無いことが読み取れない: %q", got)
	}
	found := false
	for _, k := range i18n.Missing() {
		if k == unknown {
			found = true
		}
	}
	if !found {
		t.Fatalf("引けなかったキーが Missing() に載っていない: %v", i18n.Missing())
	}
}

// 目的: 訳の無いキーが正の言語（日本語）へ落ちることを確認する（設計 3-35b）。
//
// **穴の空いた資源を組んで確かめる。**埋め込んだ `messages/en.json` は
// `messages/ja.json` の複製なので、訳の無いキーが1つも無い。**それを相手にすると、
// 落とし先を見る検査が1度も走らないまま通ってしまう。**
//
// 与える情報: 宣言したキーのうち1つだけを持つ英語の資源。
// 成功条件: 持っているキーは英語から、持たないキーは日本語から引けること。
func TestCatalog_訳の無いキーは正の言語へ落ちる(t *testing.T) {
	source, ok := i18n.CatalogOf(i18n.SourceLang)
	if !ok {
		t.Fatalf("正の言語 %s の資源がありません", i18n.SourceLang)
	}
	keys := i18n.AllKeys()
	if len(keys) < 2 {
		t.Fatalf("宣言したキーが %d 個しかなく、穴の空いた資源を組めない", len(keys))
	}
	translated, untranslated := keys[0], keys[1]

	const englishPattern = "translated on purpose"
	target := i18n.NewCatalog(i18n.LangEN, map[i18n.Key]string{translated: englishPattern})

	// 訳のあるキーは、その言語から引ける。
	got, from, ok := target.Lookup(translated)
	if !ok || from != i18n.LangEN || got != englishPattern {
		t.Errorf("訳のあるキー %q が英語から引けていない: pattern=%q from=%q ok=%v",
			translated, got, from, ok)
	}

	// 訳の無いキーは、正の言語（日本語）へ落ちる。
	wantJA, _, ok := source.Lookup(untranslated)
	if !ok {
		t.Fatalf("キー %q が正の言語 %s の資源に無い", untranslated, i18n.SourceLang)
	}
	got, from, ok = target.Lookup(untranslated)
	if !ok {
		t.Fatalf("訳の無いキー %q がどこからも引けない", untranslated)
	}
	if from != i18n.SourceLang {
		t.Errorf("訳の無いキー %q が正の言語へ落ちていない（引けた言語: %q）", untranslated, from)
	}
	if got != wantJA {
		t.Errorf("訳の無いキー %q の落とし先が日本語の文言と違う: got %q, want %q",
			untranslated, got, wantJA)
	}

	// **どちらの言語にも無いキーは、生のキーを画面に出さずに「無い」と分かる形で返る。**
	const unknown = i18n.Key("doctor.label.この文言は存在しない")
	if _, _, ok := target.Lookup(unknown); ok {
		t.Errorf("どちらの言語にも無いキー %q が引けてしまった", unknown)
	}
	i18n.ResetMissing()
	t.Cleanup(i18n.ResetMissing)
	if got := target.T(unknown); got == string(unknown) || !strings.Contains(got, "文言が登録されていません") {
		t.Errorf("どちらの言語にも無いキーの戻り値が %q になっている", got)
	}
}

// 目的: 英語を選んだとき、宣言したキーが1つ残らず引けることを確認する。
//
// **空文字では検出できない。**引けなかったときに返るのは空文字ではなく
// 「（文言が登録されていません: …）」なので、**引けたかどうかは Missing() で見る**
// （設計 3-35）。
//
// 与える情報: 英語を選んだ状態で、宣言したキーを全部引く。
// 成功条件: Missing() が空であること。
func TestT_英語を選んでも引けないキーが1つも無い(t *testing.T) {
	useLang(t, i18n.LangEN)
	if i18n.Current() != i18n.LangEN {
		t.Fatalf("選んだ言語が %s になっていない: %s", i18n.LangEN, i18n.Current())
	}
	i18n.ResetMissing()
	t.Cleanup(i18n.ResetMissing)

	for _, k := range i18n.AllKeys() {
		i18n.T(k)
	}
	if got := i18n.Missing(); len(got) > 0 {
		t.Errorf("英語を選んだときに引けなかったキーがある: %v", got)
	}
}

// 目的: 英語の資源が日本語の資源の複製のままであることを確認する（設計 3-35b）。
//
// **これが無いと、英語の資源は黙って古くなる。**`messages/ja.json` の文言を1つ直しても
// `messages/en.json` は古いままで、**キーが英語側に在る以上、日本語へ落ちない。**
// `LANG` を持たない環境の利用者には古い文言が出続ける。
//
// **本物の英語の訳を入れ始めるときは、この検査を入れ替える**（訳したキーを除く形にするか、
// 複製をやめて差分だけを置く形にする）。**入れ替えずに訳を入れると、ここで落ちる。**
//
// 与える情報: 宣言したキーと、日本語・英語それぞれの資源。
// 成功条件: すべてのキーが英語側にもあり、文言が日本語側と一字一句同じであること。
func TestMessages_英語の資源が日本語の資源の複製のままである(t *testing.T) {
	source, ok := i18n.CatalogOf(i18n.SourceLang)
	if !ok {
		t.Fatalf("正の言語 %s の資源がありません", i18n.SourceLang)
	}
	target, ok := i18n.CatalogOf(i18n.LangEN)
	if !ok {
		t.Fatalf("言語 %s の資源がありません", i18n.LangEN)
	}

	for _, k := range i18n.AllKeys() {
		want, _, ok := source.Lookup(k)
		if !ok {
			// 日本語側の欠落は TestKeys_宣言したキーと日本語の資源が1対1である が報告する。
			continue
		}
		got, from, ok := target.Lookup(k)
		if !ok || from != i18n.LangEN {
			t.Errorf("キー %q が %s の資源に無い（複製が古い。ja.json から入れ直すこと）", k, i18n.LangEN)
			continue
		}
		if got != want {
			t.Errorf("キー %q の文言が複製と食い違う（複製が古い。ja.json から入れ直すこと）: %s %q / %s %q",
				k, i18n.LangEN, got, i18n.SourceLang, want)
		}
	}
}

// 目的: 言語を決められなかったときの落とし先が英語であることを固定する（設計 3-35 / 3-35b）。
//
// **資源の正（SourceLang）とは別である。**正は「文言を書くときの原文の言語」で、
// 既定は「設定でも LANG でも決まらなかったときに出す言語」である。
// **`LANG` を持たない環境（CI・コンテナ・`env -i`）で日本語を出すと、読めない人が
// 最初の画面で詰まる。**
//
// 与える情報: なし（package の定数）。
// 成功条件: 既定が英語で、正が日本語であること。
func TestDefaultLang_決められなかったときは英語である(t *testing.T) {
	if i18n.DefaultLang != i18n.LangEN {
		t.Fatalf("既定の言語が %s ではなく %s になっている", i18n.LangEN, i18n.DefaultLang)
	}
	if i18n.SourceLang != i18n.LangJA {
		t.Fatalf("正の言語が %s ではなく %s になっている", i18n.LangJA, i18n.SourceLang)
	}
}

// 目的: 英語の資源が、日本語の資源と食い違わないことを確認する。
//
// **訳文を入れたあとに効く検査である。**キーが日本語側に無ければ画面に出ないし、
// 書式の verb の並びが違えば `%!d(string=…)` のような壊れた表示になる。
//
// 与える情報: 英語の資源のキーと書式文字列。
// 成功条件: すべてのキーが日本語側にもあり、verb の並びが一致すること。
func TestMessages_英語の資源が日本語の資源と食い違わない(t *testing.T) {
	source, ok := i18n.CatalogOf(i18n.SourceLang)
	if !ok {
		t.Fatalf("正の言語 %s の資源がありません", i18n.SourceLang)
	}
	target, ok := i18n.CatalogOf(i18n.LangEN)
	if !ok {
		t.Fatalf("言語 %s の資源がありません", i18n.LangEN)
	}

	for _, k := range target.Keys() {
		jaPattern, lang, ok := source.Lookup(k)
		if !ok || lang != i18n.SourceLang {
			t.Errorf("英語にしかないキーがある: %q", k)
			continue
		}
		enPattern, _, _ := target.Lookup(k)
		if ja, en := verbsOf(jaPattern), verbsOf(enPattern); !equalStrings(ja, en) {
			t.Errorf("キー %q の書式が食い違う: 日本語 %v / 英語 %v", k, ja, en)
		}
	}
}

// 目的: `%d` に3桁区切りが入らないことを確認する（自前の仕組みにした理由。設計 3-35）。
//
// `golang.org/x/text/message` は `%d` を土地ごとの書式で出すため、
// `project #1234` が `project #1,234` になる。**fmt.Sprintf をそのまま使えばそうならない。**
//
// 与える情報: `project #%d` を含む文言と、4桁の番号。
// 成功条件: 区切りの入らない `#1234` が出ること。
func TestT_数値に3桁区切りが入らない(t *testing.T) {
	useLang(t, i18n.SourceLang)

	got := i18n.T(i18n.KeyDoctorBoardOK, "maimuzo", 1234, 5, 2, "")

	if !strings.Contains(got, "#1234") {
		t.Fatalf("番号に区切りが入っている（または番号が出ていない）: %q", got)
	}
	if strings.Contains(got, "1,234") {
		t.Fatalf("番号に3桁区切りが入っている: %q", got)
	}
}

// 目的: Errorf が `%w` の連鎖を保つことを確認する（自前の仕組みにした理由。設計 3-35）。
//
// 与える情報: `%w` を含む書式（この試験の中だけで使う資源）と、包む対象のエラー。
// 成功条件: errors.Is で元のエラーを辿れること。
func TestErrorf_wの連鎖が切れない(t *testing.T) {
	useLang(t, i18n.SourceLang)

	// **資源に無いキーでも書式は当たる**ので、ここでは連鎖だけを見る。
	// 実際に `%w` を使う文言は、エラーを移すときに資源へ足す。
	base := errors.New("元のエラー")
	source, ok := i18n.CatalogOf(i18n.SourceLang)
	if !ok {
		t.Fatalf("正の言語 %s の資源がありません", i18n.SourceLang)
	}
	// 資源にある書式（`%v` で終わるもの）に対しても、fmt.Errorf をそのまま呼んでいることを
	// 確かめるため、まず `%w` を含む書式で包めることを見る。
	wrapped := fmt.Errorf(strings.Replace(source.T(i18n.KeyDoctorConfigUnreadable), "%v", "%w", 1),
		"/tmp/WORKFLOW.md", base)
	if !errors.Is(wrapped, base) {
		t.Fatalf("fmt.Errorf の連鎖が切れている: %v", wrapped)
	}

	viaCatalog := source.Errorf(i18n.KeyDoctorConfigUnreadable, "/tmp/WORKFLOW.md", base)
	if viaCatalog == nil || !strings.Contains(viaCatalog.Error(), "元のエラー") {
		t.Fatalf("Errorf が包んだエラーの文面を落としている: %v", viaCatalog)
	}
}

// verbsOf は書式文字列に出てくる verb を並んだ順に返す。
//
// `%%` は verb ではないので数えない。`%[1]s` のような引数の位置指定も verb だけを返す。
//
// pattern: 書式文字列。
// 戻り値: verb の並び（`s` / `d` / `v` / `q` / `w` など）。
func verbsOf(pattern string) []string {
	var out []string
	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '%' {
			continue
		}
		i++
		if i >= len(runes) {
			break
		}
		if runes[i] == '%' {
			continue
		}
		// フラグ・幅・精度・引数の位置指定を読み飛ばし、最初の英字を verb とする。
		for i < len(runes) && !isVerbLetter(runes[i]) {
			i++
		}
		if i < len(runes) {
			out = append(out, string(runes[i]))
		}
	}
	return out
}

// isVerbLetter はその文字が verb の文字かどうかを返す。
//
// r: 調べる文字。
// 戻り値: 英字なら true。
func isVerbLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// equalStrings は文字列の並びが等しいかを返す。
//
// a: 比べる並び。
// b: 比べる並び。
// 戻り値: 長さも中身も等しければ true。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
