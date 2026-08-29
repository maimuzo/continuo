// Package testlang は、テストを**正の言語（日本語）**で走らせるための道具である
// （docs/plans/continuo_design.md 3-35b）。
//
// **文言を確かめる検査は、日本語の原文に対して書く。**
// `messages/ja.json` が正であり、そこに書いてある文（「片付ける状態」「二重起動」など）が
// 検査の相手である。**訳文に対して書いてはならない。**訳は読みやすさのために言い回しが
// 変わるので、訳を1語直すたびに関係のない検査が落ちることになる。
//
// **何もしないと英語で走る。**画面に出す既定は英語で（i18n.DefaultLang）、
// CI は `LANG` を持たない（scripts/test-like-ci.sh）。**手元の `LANG` によって
// 検査の相手の言語が変わるのも困る。**だからテストの側で固定する。
//
// **英語の資源そのものは `test/internal/i18n` が確かめる。**
// 宣言した全キーが英語から引けること・書式の verb の並びが日本語と一致すること・
// 訳が正の資源のいまの版に対して作られていることの3つを見ている。
//
// 使い方。**文言を確かめる検査を持つ package に、これ1つだけのファイルを置く。**
//
//	package doctor_test
//
//	import (
//		"os"
//		"testing"
//
//		"github.com/maimuzo/continuo/test/testlang"
//	)
//
//	func TestMain(m *testing.M) { os.Exit(testlang.Run(m)) }
package testlang

import (
	"os"
	"testing"

	"github.com/maimuzo/continuo/internal/i18n"
)

// SourceLocale は正の言語を選ばせるロケールの文字列である。
//
// **`i18n.Use` だけでは足りない。**呼ぶ側が `i18n.FromEnv` や `i18n.Resolve` で
// 言語を決め直す経路があり（`internal/cli` と `internal/daemon`）、そこは環境変数を見る。
// **子プロセスへも環境変数として渡る**ので、実行ファイルを起動する検査にも効く。
const SourceLocale = "ja_JP.UTF-8"

// EnvEntry は、子プロセスへ正の言語を渡す環境変数の1項目（`LANG=ja_JP.UTF-8`）である。
//
// **`cmd.Env` を自分で組み立てる検査に足すこと。**`os.Environ()` を土台にしない検査では、
// Run が設定した環境変数が子プロセスへ渡らず、**ビルドした実行ファイルが英語で動く。**
//
//	cmd.Env = []string{
//		"PATH=" + binDir,
//		"HOME=" + home,
//		testlang.EnvEntry(),
//	}
//
// 戻り値: `KEY=value` の形の1項目。
func EnvEntry() string { return i18n.EnvLangName + "=" + SourceLocale }

// Run は正の言語を固定してテストを走らせる。
//
// **TestMain から呼ぶ。**戻り値をそのまま os.Exit へ渡すこと
// （そうしないと t.Cleanup の後始末が走る前にプロセスが終わる）。
//
// m: テストの入れ物。
// 戻り値: テストの終了コード。
func Run(m *testing.M) int {
	// **os.Setenv を使う。**t.Setenv はテスト1つの中でしか使えず、TestMain からは呼べない。
	// テストが始まる前の1回だけなので、並行して走るテストと競合しない。
	if err := os.Setenv(i18n.EnvLangName, SourceLocale); err != nil {
		panic("testlang: " + i18n.EnvLangName + " を設定できません: " + err.Error())
	}
	i18n.Use(i18n.SourceLang)
	return m.Run()
}
