// `continuo init` が置く雛形の「書く言語」が、front matter の language に連動することの検査である
// （issue #187（日本語を読まない利用者の手元でも、エージェントが日本語でコメントと
// commit メッセージを書く））。
//
// **外部へ1回も接続しない。**雛形の全文と、そこから組み立てた送る文面を読むだけである。
package scaffold_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/prompt"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// writeLanguageHeading は、何語で書かせるかを書く節の見出しである。
const writeLanguageHeading = "### 書く言語"

// writeLanguageSection は、雛形の全文から `### 書く言語` の節の中身を取り出す。
//
// t: 呼び出し元のテスト。
// raw: 雛形の全文（front matter と本文）。
// 戻り値: 見出しの次の行から、次の `### ` の直前までの中身。
func writeLanguageSection(t *testing.T, raw string) string {
	t.Helper()
	body := bodyOf(t, "雛形", raw)
	at := strings.Index(body, "\n"+writeLanguageHeading+"\n")
	if at < 0 {
		t.Fatalf("雛形の本文に %q がありません（設計 5-3d の表と食い違っています）", writeLanguageHeading)
	}
	rest := body[at+len("\n"+writeLanguageHeading+"\n"):]
	if end := strings.Index(rest, "\n### "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// 目的: 雛形の `### 書く言語` の節が、どの言語の指示も持たないことを固定する（issue #187）。
//
// **この節に限った話である。**雛形の本文には日本語の見出しと指示が他にもあり、
// **それらは `language` によらずそのままエージェントへ送られる。**
// **本文ごと言語で切り替えるのは #226 の範囲である。**
//
// **なぜ要るか。**continuo は OSS として配る。**日本語を読み書きしない人も `continuo init` を叩く。**
// 雛形が「すべて日本語で書いてください」を持っていると、
// **その人の手元でも、エージェントは issue のコメントと commit メッセージを日本語で書く。**
// **利用者は、その節を消すか書き換えるまで気づけない。**
//
// **指示を持つのは資源（`internal/i18n/messages/`）の側である。**
// 書き出すときに `language` から選んで差し込む（`applyWriteLanguage`）。
// **雛形へ直接書くと、選ぶ余地が無くなる。**
//
// 与える情報: scaffold.Template()（プレースホルダを埋める前の全文）。
// 成功条件: `### 書く言語` の節の中身が、HTML のコメントだけであること。
func TestTemplate_雛形そのものは書く言語の指示を持たない(t *testing.T) {
	for _, line := range strings.Split(writeLanguageSection(t, scaffold.Template()), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "<!--") {
			continue
		}
		t.Errorf("雛形そのものに、書く言語の指示があります（issue #187）。"+
			"雛形へ直接書くと、その言語を読まない利用者の手元でも、"+
			"エージェントがその言語でコメントと commit メッセージを書きます\n  その行: %q", line)
	}
}

// 目的: 書き出す全文の「書く言語」が、いま選ばれている言語のものになることを固定する（issue #187）。
//
// **なぜ要るか。**issue #187 のコメントで人間が決めたのは、
// **「`WORKFLOW.md` の `language` の設定に連動させ、『日本語で書け』『英語で書け』を切り替える」**である。
// **節ごと空にする案は、そのコメントで取り下げられている。**
// 空にすると、日本語の利用者の手元でも指示が1つも届かず、
// **エージェントの既定（英語で書きがち）に任されることになる。**
//
// 与える情報: 言語を切り替えて呼んだ scaffold.TemplateWithValues。
// 成功条件: 日本語では日本語の指示、英語では英語の指示が入り、目印が残っていないこと。
func TestTemplate_書く言語はlanguageに連動する(t *testing.T) {
	t.Cleanup(func() { i18n.Use(i18n.DefaultLang) })

	cases := []struct {
		lang i18n.Lang
		want string
	}{
		{i18n.LangJA, "すべて日本語で書いてください"},
		{i18n.LangEN, "commit messages and code comments in English"},
	}
	for _, c := range cases {
		i18n.Use(c.lang)
		section := writeLanguageSection(t, scaffold.TemplateWithValues(scaffold.Values{}))
		if !strings.Contains(section, c.want) {
			t.Errorf("言語が %s のとき、書く言語の節に %q がありません:\n%s", c.lang, c.want, section)
		}
		// **目印が残っていたら、差し替えが1度も効いていない。**
		if strings.Contains(section, "continuo:write-language") {
			t.Errorf("言語が %s のとき、差し込む場所の目印が残っています:\n%s", c.lang, section)
		}
	}
}

// 目的: 差し込んだ1行が、実際に送る文面まで届くことを固定する（issue #187）。
//
// **なぜ要るか。**中身がコメントだけの節は、送る前に落ちる（設計 5-3m。
// `internal/prompt` の `dropEmptySections`）。**指示を差し込めていなければ、この節ごと落ちる。**
// **落ちたことは、送る文面を見るまで分からない。**
//
// 与える情報: 雛形の本文から組み立てた、送る文面の全文。
// 成功条件: 見出しと、その言語の指示の両方が残っていること。
func TestTemplate_書く言語の1行は送る文面まで届く(t *testing.T) {
	t.Cleanup(func() { i18n.Use(i18n.DefaultLang) })
	i18n.Use(i18n.LangJA)

	body := bodyOf(t, "雛形", scaffold.TemplateWithValues(scaffold.Values{}))
	text := prompt.Build(body, "/dev/null/WORKFLOW.md").Text()

	if !strings.Contains(text, writeLanguageHeading) {
		t.Errorf("送る文面から %q が落ちています（issue #187）。"+
			"1行を差し込めていないと、中身が無い節として落とされます", writeLanguageHeading)
	}
	if !strings.Contains(text, "すべて日本語で書いてください") {
		t.Error("送る文面に、書く言語の指示が入っていません（issue #187）")
	}
	// **落とす仕掛けそのものは効いている。**同じ形で中身がコメントだけの節は落ちる。
	if strings.Contains(text, "### テストの走らせ方") {
		t.Error("送る文面に `### テストの走らせ方` が残っています。" +
			"中身が無い見出しを落とす仕掛けが効いていません（設計 5-3m）")
	}
	// **中身のある節は残る。**全部落ちているのでは検査にならない。
	if !strings.Contains(text, "### 何をする作業か") {
		t.Error("送る文面から `### 何をする作業か` まで落ちています。落としすぎです")
	}
}
