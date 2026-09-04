// `continuo init` が置く雛形が、書く言語の既定を持たないことの検査である
// （issue #187（日本語を読まない利用者の手元でも、エージェントが日本語でコメントと
// commit メッセージを書く））。
//
// **外部へ1回も接続しない。**雛形の全文と、そこから組み立てた送る文面を読むだけである。
package scaffold_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/prompt"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// 目的: 雛形の `### 書く言語` が、特定の言語を既定にしていないことを固定する（issue #187）。
//
// **なぜ要るか。**continuo は OSS として配る。**日本語を読み書きしない人も `continuo init` を叩く。**
// 雛形が「すべて日本語で書いてください」を既定にしていると、
// **その人の手元でも、エージェントは issue のコメントと commit メッセージを日本語で書く。**
// **利用者は、その節を消すか書き換えるまで気づけない。**
//
// **front matter の `language`（画面に出す文言の言語。既定 `auto` で環境変数 LANG から決める）とは
// 別物であり、連動しない。**`LANG` が英語の環境で continuo を動かしても、
// **画面は英語になるが、エージェントは日本語で書いていた。**
//
// **`language` へ連動させる案は採っていない**（設計 5-3d）。
// 組み込みの指示書は日本語だけを持つと決まっているので（設計 5-3e）、
// **指示書の大半が日本語のまま「英語で書け」とだけ言う状態になる。**
//
// 与える情報: scaffold.Template() の全文。
// 成功条件: `### 書く言語` の節が在り、その中身が案内のコメントだけであること。
func TestTemplate_雛形は書く言語の既定を持たない(t *testing.T) {
	body := bodyOf(t, "雛形", scaffold.Template())

	const heading = "### 書く言語"
	at := strings.Index(body, "\n"+heading+"\n")
	if at < 0 {
		t.Fatalf("雛形の本文に %q がありません（設計 5-3d の表と食い違っています）", heading)
	}
	// 次の見出しまでが、この節の中身である。
	rest := body[at+len("\n"+heading+"\n"):]
	end := strings.Index(rest, "\n### ")
	if end >= 0 {
		rest = rest[:end]
	}

	for _, line := range strings.Split(rest, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// **案内は HTML のコメントで書く。**そのまま送っても害が無く、節ごと消せる。
		if strings.HasPrefix(trimmed, "<!--") {
			continue
		}
		t.Errorf("雛形の `### 書く言語` に、案内のコメント以外の行があります（issue #187）。"+
			"既定を置くと、その言語を読まない利用者の手元でも、"+
			"エージェントがその言語でコメントと commit メッセージを書きます\n  その行: %q", line)
	}
}

// 目的: 既定を外した結果、送る文面から `### 書く言語` の節ごと消えることを固定する（issue #187）。
//
// **なぜ要るか。**見出しだけが残ると、エージェントは「言語の指定がある」と読んで探しにいく。
// **中身がコメントだけになった見出しは、送る前に落とす**（設計 5-3m。
// `internal/prompt` の `dropEmptySections`）。`### テストの走らせ方` と同じ振る舞いである。
//
// **落ちるのは利用者の本文の側だけである。**組み込みの側へ当てると
// `## 4-4. このプロジェクトの決まり` が落ちるので、`Build` は本文にしか当てていない。
//
// 与える情報: 雛形の本文から組み立てた、送る文面の全文。
// 成功条件: 送る文面に `### 書く言語` が1つも出てこないこと。
func TestTemplate_書く言語の節は送る文面から落ちる(t *testing.T) {
	body := bodyOf(t, "雛形", scaffold.Template())

	text := prompt.Build(body, "/dev/null/WORKFLOW.md").Text()
	if strings.Contains(text, "### 書く言語") {
		t.Error("送る文面に `### 書く言語` の見出しが残っています（issue #187）。" +
			"中身がコメントだけの見出しは、送る前に落ちるはずです（設計 5-3m）")
	}
	// **落ちる仕掛けが効いていることの裏を取る。**同じ形の `### テストの走らせ方` も落ちる。
	if strings.Contains(text, "### テストの走らせ方") {
		t.Error("送る文面に `### テストの走らせ方` が残っています。" +
			"中身が無い見出しを落とす仕掛けが効いていません（設計 5-3m）")
	}
	// **中身のある節は残る。**全部落ちているのでは検査にならない。
	if !strings.Contains(text, "### 何をする作業か") {
		t.Error("送る文面から `### 何をする作業か` まで落ちています。落としすぎです")
	}
}
