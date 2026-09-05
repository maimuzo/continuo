// {"RUCM-CFG-SHA256": "5589f5d0008ed5627ae0b490e0e2aabb09079a7d7e267008542c42519ca86ed7", "SOURCE": "docs/spec/usecases/particular_case/画面に出す文言の言語を決める.cfg.json"}
//
// **`continuo setup` が、設定に書かれた言語で対話を出すことの検査である**（設計 3-35）。
//
// **設定が主、環境変数 LANG が従である。**この2つが食い違う状況を作らないと、
// どちらが効いたのかを見分けられない。**package の TestMain（lang_test.go）は
// LANG を `ja_JP.UTF-8` に固定する**ので、環境変数を英語にしたい検査では
// `t.Setenv` で置き直す。**`t.Setenv` は検査の終わりに元の値へ戻すので、
// あとに走る検査へ持ち越さない。**
//
// **環境変数を日本語のままにする検査でも `t.Setenv` を呼んでいる。**
// そちらは置き直しではなく、**その検査が LANG に何を仮定しているかを、その場で読めるようにするため**である。
package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// writeWorkflowWithLanguage は、language を書き換えた WORKFLOW.md を1つ置く。
//
// **雛形の既定は `auto` である**（internal/scaffold/template.go）。
// `auto` のままだと環境変数 LANG から決まるので、設定が効いたことを確かめられない。
//
// t: 呼び出し元のテスト。
// lang: `language` に書く値（`ja` / `en`）。
// 戻り値: 置いたディレクトリ。
func writeWorkflowWithLanguage(t *testing.T, lang string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	out := scaffold.TemplateWithValues(scaffold.Values{Owner: "octocat", ProjectNumber: 42})
	replaced := strings.Replace(out, "language: auto", "language: "+lang, 1)
	if replaced == out {
		t.Fatalf("雛形に `language: auto` の行がありません。書き換える相手を見失っています")
	}
	if err := os.WriteFile(path, []byte(replaced), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
	return dir
}

// TestRunSetup_設定の言語で対話を出す は、**利用者が continuo で最初に見る画面**の言語を確かめる。
//
// **`continuo init` → `continuo setup` は最初にやる手順である。**ここだけ環境変数の言語で
// 出ると、`WORKFLOW.md` に `language: ja` と書いた人が、いきなり英語の対話を渡される。
//
// 目的: `language: ja` と書いた WORKFLOW.md があるとき、環境変数 LANG が英語を指していても
// 対話が日本語で出ること。
// 与える情報: `language: ja` を書いた WORKFLOW.md と、`LANG=en_US.UTF-8`。
// 成功条件: 決まった言語が日本語で、どのボードを読むかの1行が日本語の原文で出ること。
// {"RUCM-PATH": "P001"}
func TestRunSetup_設定の言語で対話を出す(t *testing.T) {
	// **`i18n.FromEnv(os.Getenv)` で戻してはならない。**開発者の手元の LANG 次第で
	// 戻り先が変わり、あとに走る検査の相手の言語が変わる。
	// **この package は TestMain で正の言語に固定してあるので、そこへ戻す。**
	t.Cleanup(func() { i18n.Use(i18n.SourceLang) })
	t.Setenv(i18n.EnvLangName, "en_US.UTF-8")

	var got scaffold.DetectOptions
	dir := writeWorkflowWithLanguage(t, "ja")

	_, stdout, _ := runCLIWith(recordingDetect(&got), []string{"setup", dir}, "")

	if lang := i18n.Current(); lang != i18n.LangJA {
		t.Errorf("設定の language が効いていない: %v", lang)
	}
	if !strings.Contains(stdout, "使うカンバン") {
		t.Errorf("どのボードを読むかの1行が日本語で出ていない:\n%s", stdout)
	}
}

// TestRunSetup_設定が英語なら環境変数が日本語でも英語で出す は、逆向きを確かめる。
//
// **片方向だけ確かめると、設定を丸ごと無視して常に日本語にする実装でも通ってしまう。**
//
// 目的: `language: en` と書いた WORKFLOW.md があるとき、環境変数 LANG が日本語を指していても
// 対話が英語で出ること。
// 与える情報: `language: en` を書いた WORKFLOW.md と、`LANG=ja_JP.UTF-8`。
// 成功条件: 決まった言語が英語で、日本語の原文がどのボードを読むかの1行に出ないこと。
// {"RUCM-PATH": "P001"}
func TestRunSetup_設定が英語なら環境変数が日本語でも英語で出す(t *testing.T) {
	t.Cleanup(func() { i18n.Use(i18n.SourceLang) })
	t.Setenv(i18n.EnvLangName, "ja_JP.UTF-8")

	var got scaffold.DetectOptions
	dir := writeWorkflowWithLanguage(t, "en")

	_, stdout, _ := runCLIWith(recordingDetect(&got), []string{"setup", dir}, "")

	if lang := i18n.Current(); lang != i18n.LangEN {
		t.Errorf("設定の language が効いていない: %v", lang)
	}
	// **英語の訳文を相手にしない**（設計 3-35d）。訳の言い回しを直すたびに落ちる。
	// **日本語の原文が出ないことだけを見る。**
	if strings.Contains(stdout, "使うカンバン") {
		t.Errorf("設定が英語なのに日本語の原文が出ている:\n%s", stdout)
	}
}

// TestRunSetup_設定を読めなければ理由を出して対話は続ける は、**黙って読み飛ばさない**ことを確かめる。
//
// **`continuo setup` は止まらない。**`continuo init` が gh から値を引けず、プレースホルダの
// 残った WORKFLOW.md に対しても走るコマンドである。止めると、その利用者が Status の
// 割り当てを1回も終えられなくなる。
//
// **だが黙らない。**RUCM の代替フロー「設定ファイルを読み取れない」と「対応していない言語の指定」が、
// どちらも「コマンドが自分の文言で報告する」「language に書ける値の一覧が出力されている」を
// 事後条件にしている。**黙ると、`language` の綴りを誤った人が、常駐プロセスを起動するまで気づけない。**
//
// 目的: `language` に資源の無い値を書いたとき、理由を出したうえで対話へ進むこと。
// 与える情報: `language: jp` を書いた WORKFLOW.md と、`LANG=ja_JP.UTF-8`。
// 成功条件: 書ける値の一覧が出ていて、そのあとどのボードを読むかの1行も出ること。
// {"RUCM-PATH": "P007"}
func TestRunSetup_設定を読めなければ理由を出して対話は続ける(t *testing.T) {
	t.Cleanup(func() { i18n.Use(i18n.SourceLang) })
	t.Setenv(i18n.EnvLangName, "ja_JP.UTF-8")

	var got scaffold.DetectOptions
	dir := writeWorkflowWithLanguage(t, "jp")

	_, stdout, stderr := runCLIWith(recordingDetect(&got), []string{"setup", dir}, "")

	if !strings.Contains(stderr, "設定ファイルを読めません") {
		t.Errorf("設定を読めなかったことを報告していない:\n%s", stderr)
	}
	// **書ける値の一覧が出ていること。**理由だけでは、何を書けばよいかが分からない。
	//
	// **並びを丸ごと相手にしない。**`i18n.Available` は資源のある言語を名前順に返すので、
	// 3つ目の言語が増えると並びが変わる。**いま資源のある2つが載っていることだけを見る。**
	for _, want := range []string{`"en"`, `"ja"`, `"auto"`} {
		if !strings.Contains(stderr, want) {
			t.Errorf("language に書ける値 %s が一覧に出ていない:\n%s", want, stderr)
		}
	}
	// **止まっていないこと。**対話へ進んでいれば、どのボードを読むかの1行が出る。
	if !strings.Contains(stdout, "使うカンバン") {
		t.Errorf("設定を読めなかっただけで対話を止めている:\n%s", stdout)
	}
}
