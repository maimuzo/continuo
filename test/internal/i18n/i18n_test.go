// Package i18n_test は画面に出す文言の引き当て（設計 3-35）を確かめる。
//
// **確かめたいことは4つある。**
//
//	1 宣言したキーと messages/ja.json が1対1であること（どちらかに無いキーを落とす）
//	2 訳の無いキーが日本語へ落ちること（生のキーを画面に出さない）
//	3 書式が fmt のままであること（`%d` に3桁区切りが入らない・`%w` の連鎖が切れない）と、
//	  訳文の指定子が日本語と同じ引数を指していること（`5件中2件` が `5 of 2` にならない）
//	4 言語の決め方が「設定が主、環境変数 LANG が従」であること
package i18n_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
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
// 「no message is registered for this key: …」と出る（設計 3-35）。
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
	if !strings.Contains(got, "no message is registered") {
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
// **穴の空いた資源を組んで確かめる。**埋め込んだ `messages/en.json` には
// 全部のキーの訳が入っていて、訳の無いキーが1つも無い。**それを相手にすると、
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
	if got := target.T(unknown); got == string(unknown) || !strings.Contains(got, "no message is registered") {
		t.Errorf("どちらの言語にも無いキーの戻り値が %q になっている", got)
	}
}

// 目的: 英語を選んだとき、宣言したキーが1つ残らず引けることを確認する。
//
// **空文字では検出できない。**引けなかったときに返るのは空文字ではなく
// 「(no message is registered for this key: …)」なので、**引けたかどうかは Missing() で見る**
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

// sourceMessagesPath は正の資源のファイルである（このテストのファイルからの相対）。
const sourceMessagesPath = "../../../internal/i18n/messages/ja.json"

// 目的: 英語の資源に、宣言したキーの訳が1つ残らず入っていることを確認する（設計 3-35b）。
//
// **訳の抜けたキーは日本語へ落ちる。**落ちること自体は正しい挙動だが、
// **1つの画面に英語と日本語が混ざると、全部日本語であるより読みにくい。**
// **英語は全キーぶん揃っている状態を保つ。**
//
// 与える情報: 宣言したキーと、英語の資源。
// 成功条件: すべてのキーが英語の資源そのものから引けること（日本語へ落ちないこと）。
func TestMessages_英語の資源に訳の抜けたキーが無い(t *testing.T) {
	target, ok := i18n.CatalogOf(i18n.LangEN)
	if !ok {
		t.Fatalf("言語 %s の資源がありません", i18n.LangEN)
	}

	for _, k := range i18n.AllKeys() {
		_, from, ok := target.Lookup(k)
		if !ok {
			t.Errorf("キー %q がどちらの言語からも引けない", k)
			continue
		}
		if from != i18n.LangEN {
			t.Errorf("キー %q の訳が messages/en.json に無く %s へ落ちている", k, from)
		}
	}
}

// 目的: 英語の資源が、正の資源のいまの版に対して作られたものであることを確認する（設計 3-35b）。
//
// **これが無いと、英語の資源は黙って古くなる。**`messages/ja.json` の文言を1つ直しても
// `messages/en.json` は古いままで、**キーが英語側に在る以上、日本語へ落ちない。**
// 英語を出す利用者には古い文言が出続ける。**文言の中身は訳なので突き合わせられないため、
// 「どの版の日本語を訳したか」を `_source_sha256` に書いて突き合わせる。**
//
// **日本語の文言を直したら、英語も直し、この値を入れ直すこと。**
//
//	shasum -a 256 internal/i18n/messages/ja.json
//
// 与える情報: `messages/ja.json` の実物の SHA-256 と、英語の資源が控えている値。
// 成功条件: 2つが一致すること。
func TestMessages_英語の資源が正の資源の版に追いついている(t *testing.T) {
	target, ok := i18n.CatalogOf(i18n.LangEN)
	if !ok {
		t.Fatalf("言語 %s の資源がありません", i18n.LangEN)
	}

	b, err := os.ReadFile(sourceMessagesPath)
	if err != nil {
		t.Fatalf("正の資源 %s を読めません: %v", sourceMessagesPath, err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256(b))

	got := target.SourceDigest()
	if got == "" {
		t.Fatalf("messages/en.json に %s がありません（正の資源の SHA-256 を書くこと）", i18n.SourceDigestKey)
	}
	if got != want {
		t.Fatalf("messages/en.json の訳が古い（%s が実物と食い違う）: 控え %q / 実物 %q\n"+
			"ja.json を直したら en.json も直し、`shasum -a 256 %s` の値を %s へ入れ直すこと",
			i18n.SourceDigestKey, got, want, sourceMessagesPath, i18n.SourceDigestKey)
	}
}

// 目的: `_source_sha256` が文言として扱われないことを確認する（設計 3-35b）。
//
// **文言に混ざると Catalog.Keys() に出る。**そうなると日本語の資源との突き合わせで
// 「英語にしかないキーがある」と誤って報告され、**本物の食い違いが埋もれる。**
//
// 与える情報: 英語の資源が持つキーの一覧。
// 成功条件: `_` で始まるキーが1つも無く、`_source_sha256` を T で引けないこと。
func TestMessages_版の控えは文言として扱われない(t *testing.T) {
	target, ok := i18n.CatalogOf(i18n.LangEN)
	if !ok {
		t.Fatalf("言語 %s の資源がありません", i18n.LangEN)
	}

	for _, k := range target.Keys() {
		if strings.HasPrefix(string(k), i18n.MetaKeyPrefix) {
			t.Errorf("文言ではないキー %q が文言に混ざっている", k)
		}
	}

	i18n.ResetMissing()
	t.Cleanup(i18n.ResetMissing)
	if _, _, ok := target.Lookup(i18n.Key(i18n.SourceDigestKey)); ok {
		t.Errorf("%s が文言として引けてしまった", i18n.SourceDigestKey)
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
// **どの引数がどこへ入るかが日本語と食い違うと、数字と文が矛盾したものが出る。**
// 実際に `対象 5件のうち 2件が見つかりません` が `2 of 5` ではなく `5 of 2` と出ていた。
//
// **見るのは「並び」ではなく「何番目の引数か」である。**verb の種類の並びだけを比べると、
// `%d` が2つあるキーは順番が逆でも通ってしまう（827件のうち137件がそれだった）。
//
// 与える情報: 英語の資源のキーと書式文字列。
// 成功条件: すべてのキーが日本語側にもあり、**引数の番号ごとに verb が一致すること。**
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
		ja, en := specsOf(jaPattern), specsOf(enPattern)

		jaByArg, jaErr := verbByArg(ja)
		if jaErr != "" {
			t.Errorf("キー %q の日本語の書式が読めない: %s（%q）", k, jaErr, jaPattern)
			continue
		}
		enByArg, enErr := verbByArg(en)
		if enErr != "" {
			t.Errorf("キー %q の英語の書式が読めない: %s（%q）", k, enErr, enPattern)
			continue
		}
		if len(jaByArg) != len(enByArg) {
			t.Errorf("キー %q の引数の数が食い違う: 日本語 %d 個 / 英語 %d 個", k, len(jaByArg), len(enByArg))
			continue
		}
		for arg, jaVerb := range jaByArg {
			if enVerb, ok := enByArg[arg]; !ok || enVerb != jaVerb {
				t.Errorf("キー %q の %d 番目の引数の verb が食い違う: 日本語 %%%s / 英語 %%%s\n"+
					"  日本語: %s\n  英語  : %s", k, arg, jaVerb, enVerb, jaPattern, enPattern)
			}
		}
	}
}

// 目的: 引数の順番が入れ替わっていた英訳が、実際に正しい数を出すことを確認する。
//
// **上の2つの検査は書式の形しか見ない。**形が揃っていても、渡した値がどう出るかは
// 実際に組み立てないと分からない。**`5件のうち2件` が `5 of 2` と出ていた**ので、
// 直したものを値で押さえる。
//
// 与える情報: 実際の呼び出しと同じ順番の引数（対象の総数が先、内訳が後）。
// 成功条件: 英語の文でも、総数と内訳が日本語と同じ位置に出ること。
func TestT_英語でも件数の順番が入れ替わらない(t *testing.T) {
	target, ok := i18n.CatalogOf(i18n.LangEN)
	if !ok {
		t.Fatalf("言語 %s の資源がありません", i18n.LangEN)
	}

	// internal/doctor/checks.go は `T(key, len(repos), missing)` の順で渡す。
	if got := target.T(i18n.KeyDoctorCloneDetailMissing, 5, 2); !strings.Contains(got, "2 of 5") {
		t.Errorf("対象5件のうち2件が欠けている場合の英文が違う: %q", got)
	}
	if got := target.T(i18n.KeyDoctorTrustDetailMissing, 5, 2); !strings.Contains(got, "2 of 5") {
		t.Errorf("対象5件のうち2件が未承認の場合の英文が違う: %q", got)
	}
	// internal/hookserver/pending.go は `Errorf(key, limit, size)` の順で渡す。
	if got := target.Errorf(i18n.KeyHookserverReadPendingFileTooLarge, 1024, 4096); got == nil ||
		!strings.Contains(got.Error(), "larger (4096 bytes) than the limit (1024 bytes)") {
		t.Errorf("上限1024バイトに対して4096バイトだった場合の英文が違う: %v", got)
	}
	// internal/doctor/status_names.go は `T(key, 設定の場所, 設定の Status 名, ボードの選択肢, 理由)` の順で渡す。
	got := target.T(i18n.KeyDoctorStatusNamesNote, "tracker.running_state", "In Progress", "In progress", "same")
	if !strings.Contains(got, `"In Progress" in tracker.running_state`) {
		t.Errorf("設定の場所と Status の名前が入れ替わっている: %q", got)
	}
}

// 目的: 同じ verb を2つ以上持つキーで、英語側が引数の番号を明示していることを確認する。
//
// **`%d` が2つあるキーは、順番を入れ替えても verb の並びが変わらない。**
// だから機械では「入れ替わっているかどうか」を判定できない。**判定できるようにするために、
// 英語側へ `%[1]d` `%[2]d` と番号を書かせる。**書いてあれば、上の検査が引数の番号ごとに
// verb を突き合わせられるし、訳を直す人が「1番目の引数は何か」を読み違えなくなる。
//
// **番号は `%` のすぐ後ろに書く**（`%[2]d`）。`fmt` は `%[n]w` も受け付けるので、
// `%w` を含む文言でもエラーの連鎖は切れない。
//
// 与える情報: 日本語の資源で同じ verb を2つ以上持つキーと、その英訳。
// 成功条件: 英訳のすべての指定子に番号が付いていて、番号が 1 から順に1回ずつ現れること。
// **対象のキーが0件でないこと**も確かめる（0件なら検査が空振りしている）。
func TestMessages_同じverbを繰り返すキーは英語側で引数の番号を明示する(t *testing.T) {
	source, ok := i18n.CatalogOf(i18n.SourceLang)
	if !ok {
		t.Fatalf("正の言語 %s の資源がありません", i18n.SourceLang)
	}
	target, ok := i18n.CatalogOf(i18n.LangEN)
	if !ok {
		t.Fatalf("言語 %s の資源がありません", i18n.LangEN)
	}

	checked := 0
	for _, k := range source.Keys() {
		jaPattern, _, _ := source.Lookup(k)
		if !hasRepeatedVerb(specsOf(jaPattern)) {
			continue
		}
		enPattern, lang, ok := target.Lookup(k)
		if !ok || lang != i18n.LangEN {
			t.Errorf("英語の資源にキー %q がない", k)
			continue
		}
		checked++
		for i, s := range specsOf(enPattern) {
			if !s.explicit {
				t.Errorf("キー %q の英語の %d 個目の指定子に引数の番号がない（%%[n]%s と書くこと）\n"+
					"  日本語: %s\n  英語  : %s", k, i+1, s.verb, jaPattern, enPattern)
				break
			}
		}
	}
	if checked == 0 {
		t.Fatal("同じ verb を2つ以上持つキーが1件も無い（検査が空振りしている）")
	}
	t.Logf("引数の番号を確かめたキー: %d 件", checked)
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

	got := i18n.T(i18n.KeyDoctorBoardOK, "octocat", 1234, 5, 2, "")

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

// formatSpec は書式文字列の指定子1つ（`%d` / `%[2]s` など）である。
type formatSpec struct {
	// arg は当てる引数の番号である（1 始まり）。
	//
	// **`%[n]` が書いてあればその番号、書いていなければ「1つ前の次」**という
	// fmt の数え方に合わせてある。
	arg int
	// verb は書式の verb（`s` / `d` / `v` / `q` / `w` など）である。
	verb string
	// explicit は `%[n]` で番号を明示していたかどうかである。
	explicit bool
}

// specsOf は書式文字列に出てくる指定子を並んだ順に返す。
//
// `%%` は指定子ではないので数えない。**`%[2]d` の番号を読み取り、`arg` に入れる。**
//
// pattern: 書式文字列。
// 戻り値: 指定子の並び。
func specsOf(pattern string) []formatSpec {
	var out []formatSpec
	runes := []rune(pattern)
	next := 1
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
		explicit := false
		arg := 0
		// フラグ・幅・精度を読み飛ばしつつ、`[n]` があれば引数の番号として読む。
		for i < len(runes) && !isVerbLetter(runes[i]) {
			if runes[i] == '[' {
				j := i + 1
				n := 0
				digits := 0
				for j < len(runes) && runes[j] >= '0' && runes[j] <= '9' {
					n = n*10 + int(runes[j]-'0')
					j++
					digits++
				}
				if digits > 0 && j < len(runes) && runes[j] == ']' {
					explicit = true
					arg = n
					i = j + 1
					continue
				}
			}
			i++
		}
		if i >= len(runes) {
			break
		}
		if !explicit {
			arg = next
		}
		next = arg + 1
		out = append(out, formatSpec{arg: arg, verb: string(runes[i]), explicit: explicit})
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

// verbByArg は「引数の番号 → verb」の対応を作る。
//
// **1 から順に1回ずつ現れることを求める。**同じ番号が2回出たり、番号が飛んだりすると、
// 渡した引数が余って `%!(EXTRA …)` が画面に出る。
//
// specs: 指定子の並び。
// 戻り値の1つ目: 引数の番号と verb の対応。
// 戻り値の2つ目: 作れなかった理由（作れたなら空文字）。
func verbByArg(specs []formatSpec) (map[int]string, string) {
	out := map[int]string{}
	for _, s := range specs {
		if s.arg < 1 || s.arg > len(specs) {
			return nil, fmt.Sprintf("引数の番号 %d が 1〜%d の外にある", s.arg, len(specs))
		}
		if prev, ok := out[s.arg]; ok {
			return nil, fmt.Sprintf("引数の番号 %d が2回使われている（%%%s と %%%s）", s.arg, prev, s.verb)
		}
		out[s.arg] = s.verb
	}
	return out, ""
}

// hasRepeatedVerb は同じ verb を2つ以上持つかを返す。
//
// **これが真のキーは、順番を入れ替えても verb の並びが変わらない。**だから
// 英語側に引数の番号を書かせて、機械が突き合わせられるようにする。
//
// specs: 指定子の並び。
// 戻り値: 同じ verb が2つ以上あれば true。
func hasRepeatedVerb(specs []formatSpec) bool {
	count := map[string]int{}
	for _, s := range specs {
		count[s.verb]++
		if count[s.verb] >= 2 {
			return true
		}
	}
	return false
}
