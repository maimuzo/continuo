// {"RUCM-CFG-SHA256": "5589f5d0008ed5627ae0b490e0e2aabb09079a7d7e267008542c42519ca86ed7", "SOURCE": "docs/spec/usecases/particular_case/画面に出す文言の言語を決める.cfg.json"}
//
// **ユースケース記述（RUCM）「画面に出す文言の言語を決める」から作ったテストである。**
// **このファイルが受け持つのは、設定ファイルを読まないコマンドの6経路**
// （P011〜P013・P024〜P026）である。**設定ファイルを読むコマンドの20経路**
// （P001〜P010・P014〜P023）**は `test/internal/config/rucm_language_test.go` にある。**
//
// **経路を分けているのは段9（要求されたコマンドが設定ファイルの language を読むか）である。**
// ここは `continuo version` を `cli.RunWith` で叩く。**RUCM の
// 「どのコマンドが設定ファイルの language を読むか」の表で「読まない」側に挙がっている。**
//
// **設定ファイルは置いたうえで叩く。**いま居るディレクトリに、環境変数 LANG とは違う
// 言語を名指しする WORKFLOW.md を置く。**設定を読むコマンドならそちらが勝つ**
// （`internal/cli/cli.go` の `useLanguageFromConfig`）。**読まないコマンドでは
// LANG から決めた言語が残る。**置かないと、この違いを検査できない。
//
// **P027（資源が欠けている）はテストにしていない。**資源は
// `internal/i18n/i18n.go` の `//go:embed messages` でビルド時に固定され、
// 欠けているかどうかの判定は同じファイルの `init` にある。**実行時に取り除けない。**
package i18n_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/cli"
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
	// rucmWorkflowFrontMatter は、いま居るディレクトリに置く WORKFLOW.md の front matter である。
	//
	// **tracker.provider の3つは既定値を持たない必須項目なので書く。**
	// `language` は呼ぶ側が足す。
	rucmWorkflowFrontMatter = "tracker:\n" +
		"  provider:\n" +
		"    owner: acme\n" +
		"    project_number: 1\n" +
		"    status_field: Status\n"
)

// rucmRunWithoutConfig は段1〜段9（いいえ）を再現し、決まった言語を返す。
//
// **設定ファイルの language を読まないコマンドを1つ選んで叩く。**
// `continuo version` は RUCM の表で「読まない」側にある。
//
// t: 呼び出し元のテスト。
// locale: 環境変数 LANG に置く値。
// conflicting: いま居るディレクトリに置く WORKFLOW.md が名指しする言語（LANG と違うものにする）。
// want: 決まるはずの言語。
// 戻り値: 決まった言語。
func rucmRunWithoutConfig(t *testing.T, locale string, conflicting i18n.Lang, want i18n.Lang) i18n.Lang {
	t.Helper()
	// 段2: 埋め込んだ資源に正の言語（日本語）と既定の言語（英語）のファイルがある。
	for _, lang := range []i18n.Lang{i18n.SourceLang, i18n.DefaultLang} {
		if !i18n.Supported(lang) {
			t.Fatalf("埋め込んだ資源に %s のファイルがありません", lang)
		}
	}

	// **言語は package の既定として1つしか持たない。**RunWith が書き換えるので、
	// 終わったら元へ戻す。戻さないと後続のテストが別の言語で走る。
	before := i18n.Current()
	t.Cleanup(func() { i18n.Use(before) })

	// 設定を読むコマンドなら勝つはずの WORKFLOW.md を、いま居るディレクトリに置く。
	dir := t.TempDir()
	content := "---\n" + rucmWorkflowFrontMatter + "language: " + string(conflicting) + "\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "WORKFLOW.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("テスト用の WORKFLOW.md を書き込めません: %v", err)
	}
	t.Chdir(dir)
	t.Setenv(i18n.EnvLangName, locale)

	// **叩く前に、設定が名指しする言語を入れておく。**入れないと、たまたま既定が
	// 期待どおりだっただけで通ってしまい、RunWith が環境変数から決め直したことを
	// 確かめられない（`want` が既定の英語である P024〜P026 で実際にそうなる）。
	i18n.Use(conflicting)

	// 段1〜段8: 起動の直後に環境変数 LANG から言語を決める。段9 は「いいえ」。
	var out, errBuf strings.Builder
	if code := cli.RunWith(cli.Deps{}, []string{"version"}, strings.NewReader(""), &out, &errBuf); code != 0 {
		t.Fatalf("continuo version が終了コード %d で終わった: %s", code, errBuf.String())
	}
	if got := i18n.Current(); got != want {
		t.Fatalf("LANG=%s で決まった言語が %s ではなく %s だった（設定の %s に引きずられていないか）",
			locale, want, got, conflicting)
	}
	return want
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
	//
	// **package の既定を通して引く。**RunWith が決めた言語がそのまま効いていることを
	// 確かめるためである。
	response := i18n.T(rucmPresentKey)
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

// {"RUCM-PATH": "P011"}
//
// 目的: 設定ファイルの language を読まないコマンドでは、LANG から決めた日本語が
// そのまま残ることを確認する。
//
// 与える情報: LANG=ja_JP.UTF-8。いま居るディレクトリに `language: en` を書いた WORKFLOW.md。
// 実行するのは `continuo version`（設定ファイルの language を読まない）。
// 成功条件（事後条件）: 決まった言語が LANG から決めた日本語であること。
// 応答が日本語の資源の文言であること。
func TestRUCMLanguage_P011_設定を読まないコマンドではLANGの日本語が残る(t *testing.T) {
	decided := rucmRunWithoutConfig(t, rucmLocaleSupported, i18n.LangEN, i18n.LangJA)
	rucmResponseFromDecidedLang(t, decided)
}

// {"RUCM-PATH": "P012"}
//
// 目的: 設定を読まないコマンドで日本語に決まったあと、その資源に訳が無いキーが
// 正の言語である日本語へ落ちることを確認する。
//
// **決めた言語と落とし先がどちらも日本語になる。**それでも段20 は「いいえ」、
// 段23 は「はい」を通る（決めた言語の資源に文言が無く、正の言語の資源にある）。
//
// 与える情報: LANG=ja_JP.UTF-8。`continuo version`。訳を1つも持たない日本語の資源。
// 成功条件（事後条件）: 決まった言語が日本語であること。応答が日本語の資源の文言であること。
func TestRUCMLanguage_P012_設定を読まず日本語に訳が無いので日本語へ落とす(t *testing.T) {
	decided := rucmRunWithoutConfig(t, rucmLocaleSupported, i18n.LangEN, i18n.LangJA)
	rucmResponseFallsBackToSource(t, decided)
}

// {"RUCM-PATH": "P013"}
//
// 目的: 設定を読まないコマンドで日本語に決まったあと、どの資源にも無いキーで
// 生のキーを画面に出さず、控えに記録することを確認する。
//
// 与える情報: LANG=ja_JP.UTF-8。`continuo version`。どの資源にも無いキー。
// 成功条件（事後条件）: 応答が引けなかったキーだけの文字列ではないこと。
// 引けなかったキーが控えに残っていること。
func TestRUCMLanguage_P013_設定を読まず日本語で文言が登録されていない(t *testing.T) {
	decided := rucmRunWithoutConfig(t, rucmLocaleSupported, i18n.LangEN, i18n.LangJA)
	rucmResponseRecordsMissing(t, decided)
}

// {"RUCM-PATH": "P024"}
//
// 目的: LANG が資源の無い言語を指し、設定ファイルの language も読まないコマンドで、
// 既定の言語である英語が残ることを確認する。
//
// 与える情報: LANG=fr_FR.UTF-8。いま居るディレクトリに `language: ja` を書いた WORKFLOW.md。
// 実行するのは `continuo version`（設定ファイルの language を読まない）。
// 成功条件（事後条件）: 決まった言語が既定の英語であること。応答が英語の資源の文言であること。
func TestRUCMLanguage_P024_LANGが資源の無い言語で設定も読まないので英語が残る(t *testing.T) {
	decided := rucmRunWithoutConfig(t, rucmLocaleUnsupported, i18n.LangJA, i18n.LangEN)
	rucmResponseFromDecidedLang(t, decided)
}

// {"RUCM-PATH": "P025"}
//
// 目的: 設定を読まないコマンドで既定の英語に決まったあと、英語に訳が無いキーが
// 正の言語である日本語へ落ちることを確認する。
//
// 与える情報: LANG=fr_FR.UTF-8。`continuo version`。訳を1つも持たない英語の資源。
// 成功条件（事後条件）: 決まった言語が英語であること。応答が日本語の資源の文言であること。
func TestRUCMLanguage_P025_設定を読まず英語に訳が無いので日本語へ落とす(t *testing.T) {
	decided := rucmRunWithoutConfig(t, rucmLocaleUnsupported, i18n.LangJA, i18n.LangEN)
	rucmResponseFallsBackToSource(t, decided)
}

// {"RUCM-PATH": "P026"}
//
// 目的: 設定を読まないコマンドで既定の英語に決まったあと、どの資源にも無いキーで
// 生のキーを画面に出さず、控えに記録することを確認する。
//
// 与える情報: LANG=fr_FR.UTF-8。`continuo version`。どの資源にも無いキー。
// 成功条件（事後条件）: 応答が引けなかったキーだけの文字列ではないこと。
// 引けなかったキーが控えに残っていること。
func TestRUCMLanguage_P026_設定を読まず英語で文言が登録されていない(t *testing.T) {
	decided := rucmRunWithoutConfig(t, rucmLocaleUnsupported, i18n.LangJA, i18n.LangEN)
	rucmResponseRecordsMissing(t, decided)
}
