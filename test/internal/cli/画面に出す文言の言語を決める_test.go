// {"RUCM-CFG-SHA256": "073e7d638ba7b85aaea26852de2badd5e430c18540034931edb82daf737ea43d", "SOURCE": "docs/spec/usecases/particular_case/画面に出す文言の言語を決める.cfg.json"}
//
// **ユースケース記述「画面に出す文言の言語を決める」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
// 関数を直に呼ぶテストは経路の一部だけを通すので、いちばん近い経路の番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/doctor"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// {"RUCM-PATH": "P001"}
//
// Test_画面に出す文言の言語を決める_P001_設定の言語で対話を出す は、**利用者が continuo で最初に見る画面**の言語を確かめる。
//
// **`continuo init` → `continuo setup` は最初にやる手順である。**ここだけ環境変数の言語で
// 出ると、`WORKFLOW.md` に `language: ja` と書いた人が、いきなり英語の対話を渡される。
//
// 目的: `language: ja` と書いた WORKFLOW.md があるとき、環境変数 LANG が英語を指していても
// 対話が日本語で出ること。
// 与える情報: `language: ja` を書いた WORKFLOW.md と、`LANG=en_US.UTF-8`。
// 成功条件: 決まった言語が日本語で、どのカンバンを読むかの1行が日本語の原文で出ること。
func Test_画面に出す文言の言語を決める_P001_設定の言語で対話を出す(t *testing.T) {
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
		t.Errorf("どのカンバンを読むかの1行が日本語で出ていない:\n%s", stdout)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_画面に出す文言の言語を決める_P001_設定が英語なら環境変数が日本語でも英語で出す は、逆向きを確かめる。
//
// **片方向だけ確かめると、設定を丸ごと無視して常に日本語にする実装でも通ってしまう。**
//
// 目的: `language: en` と書いた WORKFLOW.md があるとき、環境変数 LANG が日本語を指していても
// 対話が英語で出ること。
// 与える情報: `language: en` を書いた WORKFLOW.md と、`LANG=ja_JP.UTF-8`。
// 成功条件: 決まった言語が英語で、日本語の原文がどのカンバンを読むかの1行に出ないこと。
func Test_画面に出す文言の言語を決める_P001_設定が英語なら環境変数が日本語でも英語で出す(t *testing.T) {
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

// {"RUCM-PATH": "P001"}
//
// Test_画面に出す文言の言語を決める_P001_設定の言語で検査結果を出す は、設計 3-35 を確かめる。
//
// **設定が主、環境変数 LANG が従である。**
//
// 目的: WORKFLOW.md の `language` に従って見出し語の言語が決まること。
// 与える情報: `language: en` を書いた WORKFLOW.md。
// 成功条件: 呼んだあとの言語が英語になっていること。
func Test_画面に出す文言の言語を決める_P001_設定の言語で検査結果を出す(t *testing.T) {
	// **`i18n.FromEnv(os.Getenv)` で戻してはならない。**開発者の手元の `LANG` 次第で
	// 戻り先が変わり、あとに走る検査の相手の言語が変わる。
	// **この package は TestMain（lang_test.go）で正の言語に固定してあるので、そこへ戻す。**
	t.Cleanup(func() { i18n.Use(i18n.SourceLang) })
	deps := fakeDoctor(doctor.Report{Results: []doctor.Result{
		{Label: doctor.LabelConfig, Symbol: doctor.SymbolOK, Detail: "ok"},
	}})

	dir := writeWorkflowFor(t)
	path := filepath.Join(dir, "WORKFLOW.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	out := strings.Replace(string(body), "language: auto", "language: en", 1)
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	i18n.Use(i18n.LangJA)
	runCLIWith(deps, []string{"doctor", dir}, "")
	if got := i18n.Current(); got != i18n.LangEN {
		t.Errorf("設定の language が効いていない: %v", got)
	}
}

// {"RUCM-PATH": "P007"}
//
// Test_画面に出す文言の言語を決める_P007_設定を読めなければ理由を出して対話は続ける は、**黙って読み飛ばさない**ことを確かめる。
//
// **`continuo setup` は止まらない。**`continuo init` が gh から値を引けず、プレースホルダの
// 残った WORKFLOW.md に対しても走るコマンドである。止めると、その利用者が Status の
// 割り当てを1回も終えられなくなる。
//
// **だが黙らない。**RUCM の代替フロー「設定ファイルを読み取れない」（`language` の値が受け付けられない実行も、このフローに入る）が、
// 「コマンドは、設定ファイルを読めない理由を環境変数 LANG から決めた言語の文言で報告する」を
// 事後条件にしている。**黙ると、`language` の綴りを誤った人が、常駐プロセスを起動するまで気づけない。**
//
// 目的: `language` に資源の無い値を書いたとき、理由を出したうえで対話へ進むこと。
// 与える情報: `language: jp` を書いた WORKFLOW.md と、`LANG=ja_JP.UTF-8`。
// 成功条件: 書ける値の一覧が出ていて、そのあとどのカンバンを読むかの1行も出ること。
func Test_画面に出す文言の言語を決める_P007_設定を読めなければ理由を出して対話は続ける(t *testing.T) {
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
	// **止まっていないこと。**対話へ進んでいれば、どのカンバンを読むかの1行が出る。
	if !strings.Contains(stdout, "使うカンバン") {
		t.Errorf("設定を読めなかっただけで対話を止めている:\n%s", stdout)
	}
}
