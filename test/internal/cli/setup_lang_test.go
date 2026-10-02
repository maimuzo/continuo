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
