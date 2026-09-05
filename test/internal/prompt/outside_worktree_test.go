// 組み込みのプロンプトの「成果がこの worktree の外にあるとき」の逃げ道の検査である
// （issue #186（コードが別のリポジトリにある運用が成立しない
// （設計が実在しない行を指し、組み込みの push の段と衝突する））。設計 3-78b）。
//
// **外部へ1回も接続しない。**組み込みの全文を読むだけである。
package prompt_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/prompt"
)

// 目的: 組み込みの 3-4 が、成果がこの worktree の外にあるときだけ 4-4 へ譲ることを固定する
// （issue #186。設計 3-78b）。
//
// **なぜ要るか。**設計 3-78b は「issue が非公開のリポジトリにあり、コードが public の fork にあり、
// PR を本家へ出す」運用のために、**`WORKFLOW.md` の本文へ「この worktree の中では commit するな」を
// 置けと言っている。**ところが組み込みの 3-4 は「`review` または `blocked` を出す前に、必ず commit して
// push してください」を求める。**譲る口を開けないと、2つの指示が並んで、どちらに従うかがエージェント次第になる。**
//
// **`## 7-4.` は既に「成果がこの worktree の外にあるときの出し方」を 4-4 へ委ねている。**
// **3-4 だけがその口を持っていなかった。**
//
// **[test/internal/prompt/blocked_push_test.go] はこれを見つけられない。**
// あちらは「push を求める文」の**行数**しか数えず、逃げ道の段落はその文字列を含まないので素通りする。
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 逃げ道の段落があり、push を求める段（3-4）の中にあること。
func TestTemplate_成果がworktreeの外にあるときは4_4へ譲る(t *testing.T) {
	body := prompt.Builtin()

	const escape = "**成果がこの worktree の外にあるとき**は、この段の代わりに 4-4 の指示に従います。"
	at := strings.Index(body, escape)
	if at < 0 {
		t.Fatalf("組み込みのプロンプトに逃げ道がありません（%q）。"+
			"無いと、組み込みの「必ず commit して push」と、"+
			"設計 3-78b が本文へ置かせる「この worktree の中では commit するな」が並びます（issue #186）",
			escape)
	}

	// **push を求める段の中に置く。**離すと、その段だけを読んだエージェントが例外を知らない。
	demand := strings.Index(body, "を出す前に、必ず commit して push してください。")
	next := strings.Index(body, "## 3-5. pull request を出す")
	if demand < 0 || next < 0 {
		t.Fatalf("push を求める文か 3-5 の見出しが見つかりません（demand=%d next=%d）", demand, next)
	}
	if !(demand < at && at < next) {
		t.Errorf("逃げ道が 3-4 の中にありません（demand=%d escape=%d next=%d）", demand, at, next)
	}
}

// 目的: 逃げ道の発動を、書いた人の立場と 4-4 の記述の両方で絞っていることを固定する
// （issue #186）。
//
// **なぜ要るか。**発動の条件を「issue にコードの在りかが書いてあるとき」だけにすると、
// **誰が書いたものでも従う。**このリポジトリは public であり、issue は誰でも書ける。
// **外部の人が1行書くだけで、この worktree の commit と push を丸ごと飛ばせる。**
//
// **同じ落とし穴を push 先で一度踏んで直している**
// （[test/internal/prompt/push_upstream_test.go] の
// `TestTemplate_組み込みのプロンプトは別名へのpushを書いた人の立場で絞る`）。
//
// **4-4 の記述も要る。**書いていなければ譲る先が無く、成果の出し方を誰も指示していないことになる。
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 2つの条件が両方要ることと、片方でも欠けたら使わないことが書いてあること。
func TestTemplate_worktreeの外への逃げ道は立場と4_4の両方で絞る(t *testing.T) {
	body := prompt.Builtin()

	for _, want := range []string{
		"1. OWNER / MEMBER / COLLABORATOR が「コードは別のリポジトリにある」と書いている（6-1）",
		"2. 4-4 に、その成果の出し方が書いてある（7-4）",
		"**片方でも欠けていたら、この例外は使いません。**",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("逃げ道の絞り込みに %q がありません（issue #186）。"+
				"絞らないと、外部の人が issue に1行書くだけで commit と push を飛ばせます", want)
		}
	}
}

// 目的: 3-4 の例外を使ったときに、3-5（pull request を出す段）も 4-4 へ譲ることを固定する
// （issue #186。設計 3-78b）。
//
// **なぜ要るか。**3-4 だけに例外を置いても、次の段で止まる。
// **4-4 の見本は「この worktree の中では commit しないでください」と命じるので、
// worktree の branch には commit が1つも無い。**
// ところが 3-5 は「**先に 3-4 の push を済ませてください**」と言い、
// `git branch --show-current` で worktree の branch を引く。
// **push すべきものが無いので、その手順は原理的に済ませられない。**
// 指示どおり読んだエージェントは「pull request を作れなかった」で `blocked` を出し、
// **#186 が成立させようとした運用が、最後の1段で人間へ渡る。**
//
// 与える情報: prompt.Builtin() の 3-5 の節。
// 成功条件: 3-4 の例外を使ったときは 4-4 に従うことと、書いていなければ blocked を出すことが書いてあること。
func TestTemplate_3_5も3_4の例外のときは4_4へ譲る(t *testing.T) {
	body := prompt.Builtin()

	at := strings.Index(body, "## 3-5. pull request を出す")
	if at < 0 {
		t.Fatalf("組み込みのプロンプトに 3-5 の節がありません")
	}
	end := strings.Index(body[at+1:], "\n## ")
	if end < 0 {
		end = len(body) - at - 1
	}
	section := body[at : at+1+end]

	for _, want := range []string{
		"3-4 の例外を使ったときは、この段も 4-4 の指示に従います",
		"CONTINUO-STATUS: blocked",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("3-5 に %q がありません（issue #186）。"+
				"3-4 にだけ例外を置くと、pull request を出す段で止まります:\n%s", want, section)
		}
	}
}
