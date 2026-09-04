// {"RUCM-CFG-SHA256": "5589f5d0008ed5627ae0b490e0e2aabb09079a7d7e267008542c42519ca86ed7", "SOURCE": "docs/spec/usecases/particular_case/画面に出す文言の言語を決める.cfg.json"}
//
// **`continuo setup` が、設定に書かれた言語で対話を出すことの検査である**（設計 3-35）。
//
// **設定が主、環境変数 LANG が従である。**この2つが食い違う状況を作らないと、
// どちらが効いたのかを見分けられない。**package の TestMain（lang_test.go）は
// LANG を `ja_JP.UTF-8` に固定する**ので、ここでは `t.Setenv` で上書きする。
// **それが TestMain の固定を打ち消せる唯一の手段である。**
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
