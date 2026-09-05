// {"RUCM-CFG-SHA256": "5589f5d0008ed5627ae0b490e0e2aabb09079a7d7e267008542c42519ca86ed7", "SOURCE": "docs/spec/usecases/particular_case/画面に出す文言の言語を決める.cfg.json"}
//
// **ユースケース記述（RUCM）「画面に出す文言の言語を決める」から作ったテストである。**
// **このファイルが受け持つのは、設定ファイルを読むコマンドの20経路**
// （P001〜P010・P014〜P023）である。**設定ファイルを読まないコマンドの6経路**
// （P011〜P013・P024〜P026）**は `test/internal/i18n/rucm_language_test.go` にある。**
//
// **経路を分けているのは段9（要求されたコマンドが設定ファイルの language を読むか）である。**
// 読むほうは `config.Load` と `i18n.Resolve` を通るので、WORKFLOW.md を書ける
// この package に置く。
//
// **P027（資源が欠けている）はテストにしていない。**資源は
// `internal/i18n/i18n.go` の `//go:embed messages` でビルド時に固定され、
// 欠けているかどうかの判定は同じファイルの `init` にある。**実行時に取り除けない。**
package config_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/i18n"
)

const (
	// rucmLocaleSupported は資源のある言語を表す LANG の値である（段4 が「はい」になる）。
	rucmLocaleSupported = "ja_JP.UTF-8"
	// rucmLocaleUnsupported は資源の無い言語を表す LANG の値である（段4 が「いいえ」になる）。
	//
	// **決まる言語は既定の言語（英語）である。**資源の無い言語は当て推量に使えない。
	rucmLocaleUnsupported = "fr_FR.UTF-8"
	// rucmPresentKey は日本語にも英語にも文言があるキーである（段20 が「はい」になる）。
	//
	// **2つの言語で文言が違うものを選ぶ。**同じ文言だと、どちらの資源から引いたのかを
	// 見分けられない。
	rucmPresentKey = i18n.KeyDoctorLabelBoard
	// rucmAbsentKey はどの言語の資源にも無いキーである（段20 と段23 が「いいえ」になる）。
	rucmAbsentKey = i18n.Key("rucm.画面に出す文言の言語を決める.存在しないキー")
	// rucmUnreadableFrontMatter は読み取れない設定ファイルの中身である（段11 が「いいえ」になる）。
	//
	// **YAML の並びが閉じていない。**front matter として取り出せるが、そこから先が読めない。
	rucmUnreadableFrontMatter = "tracker:\n  provider: [\n"
)

// rucmLangGetenv は、環境変数 LANG に決まった値を返す偽の関数を作る。
//
// **本物の環境変数を読ませない。**この package の TestMain（lang_test.go）は
// `testlang.Run` で LANG を `ja_JP.UTF-8` に固定するので、
// **LANG が資源の無い言語を指す経路（P014〜P023）を本物の環境変数では作れない。**
//
// value: LANG の値として返す文字列。
// 戻り値: os.Getenv と同じ形の関数。LANG 以外は空文字を返す。
func rucmLangGetenv(value string) func(string) string {
	return func(key string) string {
		if key == i18n.EnvLangName {
			return value
		}
		return ""
	}
}

// rucmUse は段5 / 段7 / 段14 / 段16（画面に出す文言の言語に決める）を再現する。
//
// **終わったら元の言語へ戻す。**言語は package の既定として1つしか持たないので、
// 戻さないと後続のテストが別の言語で走る。
//
// t: 呼び出し元のテスト。
// lang: 決まった言語。
func rucmUse(t *testing.T, lang i18n.Lang) {
	t.Helper()
	before := i18n.Current()
	i18n.Use(lang)
	t.Cleanup(func() { i18n.Use(before) })
}

// rucmDecideFromEnv は段2〜段8（資源の有無を確かめ、環境変数 LANG から言語を決める）を再現する。
//
// t: 呼び出し元のテスト。
// locale: 環境変数 LANG の値。
// want: 決まるはずの言語。
// 戻り値: 決まった言語。
func rucmDecideFromEnv(t *testing.T, locale string, want i18n.Lang) i18n.Lang {
	t.Helper()
	// 段2: 埋め込んだ資源に正の言語（日本語）と既定の言語（英語）のファイルがある。
	for _, lang := range []i18n.Lang{i18n.SourceLang, i18n.DefaultLang} {
		if !i18n.Supported(lang) {
			t.Fatalf("埋め込んだ資源に %s のファイルがありません", lang)
		}
	}
	// 段3〜段8。
	got := i18n.FromEnv(rucmLangGetenv(locale))
	if got != want {
		t.Fatalf("LANG=%s から決まった言語が %s ではなく %s だった", locale, want, got)
	}
	rucmUse(t, got)
	if i18n.Current() != want {
		t.Fatalf("画面に出す文言の言語が %s ではなく %s だった", want, i18n.Current())
	}
	return got
}

// rucmRedecideFromConfig は段10〜段17（設定ファイルを読み、language で言語を決め直す）を再現する。
//
// **`internal/cli/cli.go` の `useLanguageFromConfig` と `useLanguage` と同じ順序である。**
// 設定を読み、読めたら `i18n.Resolve` に設定の値と環境変数を渡す。
//
// t: 呼び出し元のテスト。
// language: front matter に書く language の値。
// locale: 環境変数 LANG の値（language が言語を名指ししないときの落とし先）。
// want: 決まり直すはずの言語。
// 戻り値: 決まり直した言語。
func rucmRedecideFromConfig(t *testing.T, language, locale string, want i18n.Lang) i18n.Lang {
	t.Helper()
	// 段10: 設定ファイルを読む。
	path := writeWorkflow(t, validFrontMatter+"language: "+language+"\n", "")
	loaded, err := config.Load(path)
	// 段11: 設定ファイルを読み取れる。
	if err != nil {
		t.Fatalf("設定ファイルを読み取れません: %v", err)
	}
	// 段12: language の値が受け付けられる値である。段13〜段17。
	got, err := i18n.Resolve(loaded.Config.Language, rucmLangGetenv(locale))
	if err != nil {
		t.Fatalf("language: %s を受け付けられません: %v", language, err)
	}
	if got != want {
		t.Fatalf("language: %s と LANG=%s から決まった言語が %s ではなく %s だった",
			language, locale, want, got)
	}
	rucmUse(t, got)
	if i18n.Current() != want {
		t.Fatalf("画面に出す文言の言語が %s ではなく %s だった", want, i18n.Current())
	}
	return got
}

// rucmConfigUnreadable は代替フロー「設定ファイルを読み取れない」の段1を再現する。
//
// **言語は環境変数 LANG から決めたまま残る。**`useLanguageFromConfig` は
// `config.Load` が失敗したら何もせずに戻るので、決め直しが起きない。
//
// t: 呼び出し元のテスト。
// decided: 環境変数 LANG から決めた言語。
func rucmConfigUnreadable(t *testing.T, decided i18n.Lang) {
	t.Helper()
	path := writeWorkflow(t, rucmUnreadableFrontMatter, "")
	if _, err := config.Load(path); err == nil {
		t.Fatal("読み取れないはずの設定ファイルが読み取れてしまった")
	}
	if i18n.Current() != decided {
		t.Fatalf("設定を読み取れないのに言語が %s から %s へ変わった", decided, i18n.Current())
	}
}

// rucmUnsupportedLanguageAborts は代替フロー「対応していない言語の指定」を再現する。
//
// **事後条件は4つある。**言語が環境変数 LANG から決めた言語のままであること・
// 対応していない値を採っていないこと・常駐プロセスが起動しないこと・
// language に書ける値の一覧が出力されていること。
//
// t: 呼び出し元のテスト。
// decided: 環境変数 LANG から決めた言語。
func rucmUnsupportedLanguageAborts(t *testing.T, decided i18n.Lang) {
	t.Helper()
	const unsupported = "fr"
	path := writeWorkflow(t, validFrontMatter+"language: "+unsupported+"\n", "")

	// 常駐プロセスは起動しない（config.Load が起動を止める）。
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("対応していない言語を書いたのに起動が止まらなかった")
	}
	// language に書ける値の一覧が出力されている。
	for _, want := range []string{"language", `"auto"`, `"en"`, `"ja"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("language に書ける値の一覧に %s が無い: %v", want, err)
		}
	}
	// 画面に出す文言の言語は環境変数 LANG から決めた言語である。
	if i18n.Current() != decided {
		t.Fatalf("言語が %s から %s へ変わった", decided, i18n.Current())
	}
	// 対応していない値を画面に出す文言の言語に採っていない。
	if string(i18n.Current()) == unsupported {
		t.Fatalf("対応していない値 %q を画面に出す文言の言語に採っている", unsupported)
	}
}

// rucmResponseFromDecidedLang は段19〜段21と段26（決めた言語の資源の文言を採って応答する）を再現する。
//
// t: 呼び出し元のテスト。
// decided: 決まった言語。
func rucmResponseFromDecidedLang(t *testing.T, decided i18n.Lang) {
	t.Helper()
	catalog, ok := i18n.CatalogOf(decided)
	if !ok {
		t.Fatalf("決めた言語 %s の資源がありません", decided)
	}
	// 段20: 決めた言語の資源にキーの文言がある。
	got, from, found := catalog.Lookup(rucmPresentKey)
	if !found {
		t.Fatalf("%s の資源からキー %q を引けません", decided, rucmPresentKey)
	}
	if from != decided {
		t.Fatalf("決めた言語は %s なのに %s の資源から引いている", decided, from)
	}
	// 段21・段26: 決めた言語の資源の文言を応答する。
	response := catalog.T(rucmPresentKey)
	if response != got {
		t.Fatalf("応答 %q が %s の資源の文言 %q と違う", response, decided, got)
	}
	// **他方の言語の文言と違うことを確かめる。**同じだと、決めた言語の資源から
	// 引いたのかどうかを見分けられない。
	other := i18n.SourceLang
	if decided == i18n.SourceLang {
		other = i18n.DefaultLang
	}
	otherCatalog, ok := i18n.CatalogOf(other)
	if !ok {
		t.Fatalf("%s の資源がありません", other)
	}
	otherText, _, _ := otherCatalog.Lookup(rucmPresentKey)
	if response == otherText {
		t.Fatalf("%s と %s の文言が同じ %q で、どちらから引いたのかを見分けられない",
			decided, other, response)
	}
}

// rucmResponseFallsBackToSource は段19・段20（いいえ）・段23・段24・段26
// （決めた言語に訳が無いので正の言語である日本語へ落とす）を再現する。
//
// **埋め込んだ資源からは作れない。**`messages/en.json` には全キーの訳が入っていて、
// 訳の無いキーが1つも無い。**だから `i18n.NewCatalog` で穴の空いた資源を組む**
// （`internal/i18n/i18n.go` の `NewCatalog` が、その用途で置かれたものだと明記している）。
//
// t: 呼び出し元のテスト。
// decided: 決まった言語。
func rucmResponseFallsBackToSource(t *testing.T, decided i18n.Lang) {
	t.Helper()
	holey := i18n.NewCatalog(decided, nil)
	if holey.Lang() != decided {
		t.Fatalf("組んだ資源の言語が %s ではなく %s だった", decided, holey.Lang())
	}
	// 段23: 正の言語である日本語の資源にキーの文言がある。
	got, from, found := holey.Lookup(rucmPresentKey)
	if !found {
		t.Fatalf("正の言語 %s の資源からキー %q を引けません", i18n.SourceLang, rucmPresentKey)
	}
	if from != i18n.SourceLang {
		t.Fatalf("正の言語 %s ではなく %s の資源から引いている", i18n.SourceLang, from)
	}
	// 段24・段26: 日本語の資源の文言を応答する。
	source, ok := i18n.CatalogOf(i18n.SourceLang)
	if !ok {
		t.Fatalf("正の言語 %s の資源がありません", i18n.SourceLang)
	}
	want, _, _ := source.Lookup(rucmPresentKey)
	if got != want {
		t.Fatalf("落とし先の文言が %q ではなく %q だった", want, got)
	}
	if response := holey.T(rucmPresentKey); response != want {
		t.Fatalf("応答 %q が正の言語の文言 %q と違う", response, want)
	}
}

// rucmResponseRecordsMissing は段19・段20（いいえ）・段23（いいえ）と
// 代替フロー「文言が登録されていない」の段1〜段3を再現する。
//
// **事後条件は代替フロー側のものを採る。**基本フローの事後条件の最後の1文
// （利用者への応答は決まった言語の資源の文言である）は、この経路では成り立たない。
// **どの資源からも引けなかったのだから、応答は資源の文言ではない。**
// 代替フローの事後条件は「画面に出す文言は引けなかったキーだけの文字列ではない。
// 引けなかったキーが控えに残っている。」であり、こちらを検査する。
//
// t: 呼び出し元のテスト。
// decided: 決まった言語。
func rucmResponseRecordsMissing(t *testing.T, decided i18n.Lang) {
	t.Helper()
	i18n.ResetMissing()
	t.Cleanup(i18n.ResetMissing)

	holey := i18n.NewCatalog(decided, nil)
	// 段20 と段23 がどちらも「いいえ」になること。
	if _, _, found := holey.Lookup(rucmAbsentKey); found {
		t.Fatalf("どの資源にも無いはずのキー %q が引けた", rucmAbsentKey)
	}
	// 代替フローの段2・段3（RESUME STEP 26）: 文言が登録されていないことを表す文字列を応答する。
	response := holey.T(rucmAbsentKey)
	if response == string(rucmAbsentKey) {
		t.Fatalf("引けなかったキーだけを画面に出している: %q", response)
	}
	if !strings.Contains(response, string(rucmAbsentKey)) {
		t.Fatalf("応答 %q が、どのキーを引けなかったのかを示していない", response)
	}
	// 代替フローの段1: 引けなかったキーが控えに残っている。
	recorded := false
	for _, k := range i18n.Missing() {
		if k == rucmAbsentKey {
			recorded = true
		}
	}
	if !recorded {
		t.Fatalf("引けなかったキー %q が控えに残っていない: %v", rucmAbsentKey, i18n.Missing())
	}
}

// {"RUCM-PATH": "P001"}
//
// 目的: LANG が日本語を指し、設定の language が英語を名指しし、英語の資源に文言があるときに、
// 応答が英語の資源の文言になることを確認する。
//
// 与える情報: LANG=ja_JP.UTF-8 と `language: en` を書いた WORKFLOW.md。
// 成功条件（事後条件）: 決まった言語が language の名指しする英語であること。
// 応答が英語の資源の文言であること。
func TestRUCMLanguage_P001_設定が名指しする英語で訳のある文言を出す(t *testing.T) {
	rucmDecideFromEnv(t, rucmLocaleSupported, i18n.LangJA)
	decided := rucmRedecideFromConfig(t, "en", rucmLocaleSupported, i18n.LangEN)
	rucmResponseFromDecidedLang(t, decided)
}

// {"RUCM-PATH": "P002"}
//
// 目的: 設定が名指しする英語に訳が無いキーが、正の言語である日本語へ落ちることを確認する。
//
// 与える情報: LANG=ja_JP.UTF-8 と `language: en` を書いた WORKFLOW.md。
// 訳を1つも持たない英語の資源。
// 成功条件（事後条件）: 決まった言語が英語であること。応答が日本語の資源の文言であること。
func TestRUCMLanguage_P002_設定が名指しする英語に訳が無いので日本語へ落とす(t *testing.T) {
	rucmDecideFromEnv(t, rucmLocaleSupported, i18n.LangJA)
	decided := rucmRedecideFromConfig(t, "en", rucmLocaleSupported, i18n.LangEN)
	rucmResponseFallsBackToSource(t, decided)
}

// {"RUCM-PATH": "P003"}
//
// 目的: 設定が名指しする英語にも日本語にも文言が無いキーで、生のキーを画面に出さず、
// 控えに記録することを確認する。
//
// 与える情報: LANG=ja_JP.UTF-8 と `language: en` を書いた WORKFLOW.md。どの資源にも無いキー。
// 成功条件（事後条件）: 応答が引けなかったキーだけの文字列ではないこと。
// 引けなかったキーが控えに残っていること。
func TestRUCMLanguage_P003_設定が名指しする英語で文言が登録されていない(t *testing.T) {
	rucmDecideFromEnv(t, rucmLocaleSupported, i18n.LangJA)
	decided := rucmRedecideFromConfig(t, "en", rucmLocaleSupported, i18n.LangEN)
	rucmResponseRecordsMissing(t, decided)
}

// {"RUCM-PATH": "P004"}
//
// 目的: 設定の language が `auto`（言語を名指ししない）のとき、LANG から決めた日本語が
// そのまま残ることを確認する。
//
// 与える情報: LANG=ja_JP.UTF-8 と `language: auto` を書いた WORKFLOW.md。
// 成功条件（事後条件）: 決まった言語が LANG から決めた日本語であること。
// 応答が日本語の資源の文言であること。
func TestRUCMLanguage_P004_設定がautoならLANGの日本語が残る(t *testing.T) {
	rucmDecideFromEnv(t, rucmLocaleSupported, i18n.LangJA)
	decided := rucmRedecideFromConfig(t, "auto", rucmLocaleSupported, i18n.LangJA)
	rucmResponseFromDecidedLang(t, decided)
}

// {"RUCM-PATH": "P005"}
//
// 目的: `language: auto` で日本語に決まったあと、その資源に訳が無いキーが
// 正の言語である日本語へ落ちることを確認する。
//
// **決めた言語と落とし先がどちらも日本語になる。**それでも段20 は「いいえ」、
// 段23 は「はい」を通る（決めた言語の資源に文言が無く、正の言語の資源にある）。
//
// 与える情報: LANG=ja_JP.UTF-8 と `language: auto` を書いた WORKFLOW.md。
// 訳を1つも持たない日本語の資源。
// 成功条件（事後条件）: 決まった言語が日本語であること。応答が日本語の資源の文言であること。
func TestRUCMLanguage_P005_設定がautoで日本語に訳が無いので日本語へ落とす(t *testing.T) {
	rucmDecideFromEnv(t, rucmLocaleSupported, i18n.LangJA)
	decided := rucmRedecideFromConfig(t, "auto", rucmLocaleSupported, i18n.LangJA)
	rucmResponseFallsBackToSource(t, decided)
}

// {"RUCM-PATH": "P006"}
//
// 目的: `language: auto` で日本語に決まったあと、どの資源にも無いキーで
// 生のキーを画面に出さず、控えに記録することを確認する。
//
// 与える情報: LANG=ja_JP.UTF-8 と `language: auto` を書いた WORKFLOW.md。どの資源にも無いキー。
// 成功条件（事後条件）: 応答が引けなかったキーだけの文字列ではないこと。
// 引けなかったキーが控えに残っていること。
func TestRUCMLanguage_P006_設定がautoで文言が登録されていない(t *testing.T) {
	rucmDecideFromEnv(t, rucmLocaleSupported, i18n.LangJA)
	decided := rucmRedecideFromConfig(t, "auto", rucmLocaleSupported, i18n.LangJA)
	rucmResponseRecordsMissing(t, decided)
}

// {"RUCM-PATH": "P007"}
//
// 目的: LANG が日本語を指すとき、設定の language に資源の無い言語を書くと起動が止まり、
// 言語が LANG から決めた日本語のまま残ることを確認する。
//
// **黙って既定へ落とさない。**落とすと、書いたつもりの設定が効いていないことに
// 無人運用では誰も気づけない。
//
// 与える情報: LANG=ja_JP.UTF-8 と `language: fr` を書いた WORKFLOW.md。
// 成功条件（事後条件）: config.Load がエラーを返すこと（常駐プロセスは起動しない）。
// エラーに language に書ける値の一覧が出ていること。言語が日本語のままであること。
func TestRUCMLanguage_P007_LANGが日本語で設定に資源の無い言語を書くと止まる(t *testing.T) {
	decided := rucmDecideFromEnv(t, rucmLocaleSupported, i18n.LangJA)
	rucmUnsupportedLanguageAborts(t, decided)
}

// {"RUCM-PATH": "P008"}
//
// 目的: 設定ファイルを読み取れないとき、LANG から決めた日本語がそのまま残ることを確認する。
//
// 与える情報: LANG=ja_JP.UTF-8 と、YAML の並びが閉じていない WORKFLOW.md。
// 成功条件（事後条件）: 言語が LANG から決めた日本語であること。
// 設定ファイルの language の値を採っていないこと。応答が日本語の資源の文言であること。
func TestRUCMLanguage_P008_設定を読み取れないのでLANGの日本語が残る(t *testing.T) {
	decided := rucmDecideFromEnv(t, rucmLocaleSupported, i18n.LangJA)
	rucmConfigUnreadable(t, decided)
	rucmResponseFromDecidedLang(t, decided)
}

// {"RUCM-PATH": "P009"}
//
// 目的: 設定ファイルを読み取れずに日本語のまま進んだあと、訳の無いキーが
// 正の言語である日本語へ落ちることを確認する。
//
// 与える情報: LANG=ja_JP.UTF-8 と、YAML の並びが閉じていない WORKFLOW.md。
// 訳を1つも持たない日本語の資源。
// 成功条件（事後条件）: 言語が日本語であること。応答が日本語の資源の文言であること。
func TestRUCMLanguage_P009_設定を読み取れず日本語に訳が無いので日本語へ落とす(t *testing.T) {
	decided := rucmDecideFromEnv(t, rucmLocaleSupported, i18n.LangJA)
	rucmConfigUnreadable(t, decided)
	rucmResponseFallsBackToSource(t, decided)
}

// {"RUCM-PATH": "P010"}
//
// 目的: 設定ファイルを読み取れずに日本語のまま進んだあと、どの資源にも無いキーで
// 生のキーを画面に出さず、控えに記録することを確認する。
//
// 与える情報: LANG=ja_JP.UTF-8 と、YAML の並びが閉じていない WORKFLOW.md。どの資源にも無いキー。
// 成功条件（事後条件）: 応答が引けなかったキーだけの文字列ではないこと。
// 引けなかったキーが控えに残っていること。
func TestRUCMLanguage_P010_設定を読み取れず文言が登録されていない(t *testing.T) {
	decided := rucmDecideFromEnv(t, rucmLocaleSupported, i18n.LangJA)
	rucmConfigUnreadable(t, decided)
	rucmResponseRecordsMissing(t, decided)
}

// {"RUCM-PATH": "P014"}
//
// 目的: LANG が資源の無い言語を指して既定の英語に決まったあと、設定の language が
// 名指しする日本語で決め直されることを確認する（設定が主、環境変数が従）。
//
// 与える情報: LANG=fr_FR.UTF-8 と `language: ja` を書いた WORKFLOW.md。
// 成功条件（事後条件）: 決まった言語が language の名指しする日本語であること。
// 応答が日本語の資源の文言であること。
func TestRUCMLanguage_P014_LANGが資源の無い言語でも設定の日本語が主になる(t *testing.T) {
	rucmDecideFromEnv(t, rucmLocaleUnsupported, i18n.LangEN)
	decided := rucmRedecideFromConfig(t, "ja", rucmLocaleUnsupported, i18n.LangJA)
	rucmResponseFromDecidedLang(t, decided)
}

// {"RUCM-PATH": "P015"}
//
// 目的: 設定が名指しする日本語に訳が無いキーが、正の言語である日本語へ落ちることを確認する。
//
// **決めた言語と落とし先がどちらも日本語になる。**それでも段20 は「いいえ」、
// 段23 は「はい」を通る（決めた言語の資源に文言が無く、正の言語の資源にある）。
//
// 与える情報: LANG=fr_FR.UTF-8 と `language: ja` を書いた WORKFLOW.md。
// 訳を1つも持たない日本語の資源。
// 成功条件（事後条件）: 決まった言語が日本語であること。応答が日本語の資源の文言であること。
func TestRUCMLanguage_P015_設定が名指しする日本語に訳が無いので日本語へ落とす(t *testing.T) {
	rucmDecideFromEnv(t, rucmLocaleUnsupported, i18n.LangEN)
	decided := rucmRedecideFromConfig(t, "ja", rucmLocaleUnsupported, i18n.LangJA)
	rucmResponseFallsBackToSource(t, decided)
}

// {"RUCM-PATH": "P016"}
//
// 目的: 設定が名指しする日本語で、どの資源にも無いキーのときに
// 生のキーを画面に出さず、控えに記録することを確認する。
//
// 与える情報: LANG=fr_FR.UTF-8 と `language: ja` を書いた WORKFLOW.md。どの資源にも無いキー。
// 成功条件（事後条件）: 応答が引けなかったキーだけの文字列ではないこと。
// 引けなかったキーが控えに残っていること。
func TestRUCMLanguage_P016_設定が名指しする日本語で文言が登録されていない(t *testing.T) {
	rucmDecideFromEnv(t, rucmLocaleUnsupported, i18n.LangEN)
	decided := rucmRedecideFromConfig(t, "ja", rucmLocaleUnsupported, i18n.LangJA)
	rucmResponseRecordsMissing(t, decided)
}

// {"RUCM-PATH": "P017"}
//
// 目的: LANG が資源の無い言語を指し、設定の language が `auto` のとき、
// 既定の言語である英語が残ることを確認する。
//
// 与える情報: LANG=fr_FR.UTF-8 と `language: auto` を書いた WORKFLOW.md。
// 成功条件（事後条件）: 決まった言語が既定の英語であること。応答が英語の資源の文言であること。
func TestRUCMLanguage_P017_LANGが資源の無い言語で設定がautoなら英語が残る(t *testing.T) {
	rucmDecideFromEnv(t, rucmLocaleUnsupported, i18n.LangEN)
	decided := rucmRedecideFromConfig(t, "auto", rucmLocaleUnsupported, i18n.LangEN)
	rucmResponseFromDecidedLang(t, decided)
}

// {"RUCM-PATH": "P018"}
//
// 目的: `language: auto` で既定の英語に決まったあと、英語に訳が無いキーが
// 正の言語である日本語へ落ちることを確認する。
//
// 与える情報: LANG=fr_FR.UTF-8 と `language: auto` を書いた WORKFLOW.md。
// 訳を1つも持たない英語の資源。
// 成功条件（事後条件）: 決まった言語が英語であること。応答が日本語の資源の文言であること。
func TestRUCMLanguage_P018_設定がautoで英語に訳が無いので日本語へ落とす(t *testing.T) {
	rucmDecideFromEnv(t, rucmLocaleUnsupported, i18n.LangEN)
	decided := rucmRedecideFromConfig(t, "auto", rucmLocaleUnsupported, i18n.LangEN)
	rucmResponseFallsBackToSource(t, decided)
}

// {"RUCM-PATH": "P019"}
//
// 目的: `language: auto` で既定の英語に決まったあと、どの資源にも無いキーで
// 生のキーを画面に出さず、控えに記録することを確認する。
//
// 与える情報: LANG=fr_FR.UTF-8 と `language: auto` を書いた WORKFLOW.md。どの資源にも無いキー。
// 成功条件（事後条件）: 応答が引けなかったキーだけの文字列ではないこと。
// 引けなかったキーが控えに残っていること。
func TestRUCMLanguage_P019_LANGが資源の無い言語で設定がautoかつ文言が登録されていない(t *testing.T) {
	rucmDecideFromEnv(t, rucmLocaleUnsupported, i18n.LangEN)
	decided := rucmRedecideFromConfig(t, "auto", rucmLocaleUnsupported, i18n.LangEN)
	rucmResponseRecordsMissing(t, decided)
}

// {"RUCM-PATH": "P020"}
//
// 目的: LANG が資源の無い言語を指すとき、設定の language にも資源の無い言語を書くと
// 起動が止まり、言語が既定の英語のまま残ることを確認する。
//
// 与える情報: LANG=fr_FR.UTF-8 と `language: fr` を書いた WORKFLOW.md。
// 成功条件（事後条件）: config.Load がエラーを返すこと（常駐プロセスは起動しない）。
// エラーに language に書ける値の一覧が出ていること。言語が英語のままであること。
func TestRUCMLanguage_P020_LANGが資源の無い言語で設定にも資源の無い言語を書くと止まる(t *testing.T) {
	decided := rucmDecideFromEnv(t, rucmLocaleUnsupported, i18n.LangEN)
	rucmUnsupportedLanguageAborts(t, decided)
}

// {"RUCM-PATH": "P021"}
//
// 目的: LANG が資源の無い言語を指し、設定ファイルも読み取れないとき、
// 既定の英語が残ることを確認する。
//
// 与える情報: LANG=fr_FR.UTF-8 と、YAML の並びが閉じていない WORKFLOW.md。
// 成功条件（事後条件）: 言語が既定の英語であること。
// 設定ファイルの language の値を採っていないこと。応答が英語の資源の文言であること。
func TestRUCMLanguage_P021_設定を読み取れないので既定の英語が残る(t *testing.T) {
	decided := rucmDecideFromEnv(t, rucmLocaleUnsupported, i18n.LangEN)
	rucmConfigUnreadable(t, decided)
	rucmResponseFromDecidedLang(t, decided)
}

// {"RUCM-PATH": "P022"}
//
// 目的: 設定ファイルを読み取れずに英語のまま進んだあと、訳の無いキーが
// 正の言語である日本語へ落ちることを確認する。
//
// 与える情報: LANG=fr_FR.UTF-8 と、YAML の並びが閉じていない WORKFLOW.md。
// 訳を1つも持たない英語の資源。
// 成功条件（事後条件）: 言語が英語であること。応答が日本語の資源の文言であること。
func TestRUCMLanguage_P022_設定を読み取れず英語に訳が無いので日本語へ落とす(t *testing.T) {
	decided := rucmDecideFromEnv(t, rucmLocaleUnsupported, i18n.LangEN)
	rucmConfigUnreadable(t, decided)
	rucmResponseFallsBackToSource(t, decided)
}

// {"RUCM-PATH": "P023"}
//
// 目的: 設定ファイルを読み取れずに英語のまま進んだあと、どの資源にも無いキーで
// 生のキーを画面に出さず、控えに記録することを確認する。
//
// 与える情報: LANG=fr_FR.UTF-8 と、YAML の並びが閉じていない WORKFLOW.md。どの資源にも無いキー。
// 成功条件（事後条件）: 応答が引けなかったキーだけの文字列ではないこと。
// 引けなかったキーが控えに残っていること。
func TestRUCMLanguage_P023_設定を読み取れず英語で文言が登録されていない(t *testing.T) {
	decided := rucmDecideFromEnv(t, rucmLocaleUnsupported, i18n.LangEN)
	rucmConfigUnreadable(t, decided)
	rucmResponseRecordsMissing(t, decided)
}
