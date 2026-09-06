package prompt_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/prompt"
)

// commentFormatHeading は、人間へ質問・報告するコメントの書き方を教える節の見出しである。
const commentFormatHeading = "## 5-5. 人間へ質問・報告するコメントの書き方"

// planReviewHeading は、計画の判断票を書かせる節の見出しである。図もここで書かせる。
const planReviewHeading = "## 3-2. 計画を書き、レビューを受ける"

// 目的: 組み込みのプロンプトが、計画の判断票と同じコメントに mermaid の図を書かせることを
// 固定する（#249（エージェントのコメントに図と前提が無く、人間が「詳しく説明して」で1往復を
// 使わされる）。設計 5-3q）。
//
// **設計文書との突き合わせでは、この条件を守れない。**
// TestTemplate_組み込みのプロンプトが設計5_3と一致する は設計 5-3 の markdown ブロックと
// 組み込みのプロンプトを比べるものなので、**両方からこの段が同時に消えても通る。**
// そこで、設計文書を一切読まず、組み込みのプロンプトだけを見て条件を確かめる。
//
// **なぜ要るか。**continuo では、人間とエージェントの1往復に issue のコメント1件と、
// 次の巡回までの待ちがかかる。**文章だけのコメントを受け取った人間は、
// 構造を頭の中で組み立て直すか、「詳しく説明して」で1往復を使うことになる。**
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 3-2 の節が、図を書く指示・書式（mermaid）・図の種類の3つを教えていること。
func TestTemplate_組み込みのプロンプトは計画の概要を図で書かせる(t *testing.T) {
	body := prompt.Builtin()

	if !strings.Contains(body, "\n"+planReviewHeading+"\n") {
		t.Fatalf("組み込みのプロンプトに %q の節がありません", planReviewHeading)
	}

	// 節の中身だけを見る。**`# 1. 概要` が全体の流れを mermaid で描いているので、
	// 全文への contains では、3-2 から図の段が消えても素通りする。**
	section := sectionOf(t, body, planReviewHeading)

	for _, want := range []struct {
		needle string
		why    string
	}{
		{"概要を図で入れてください", "図を書かせる指示が無いと、文章だけのコメントが届きます"},
		{"```mermaid", "書式を決めないと、罫線文字で描いたものが届きます。" +
			"GitHub は mermaid だけをそのまま図として表示します"},
		{"`flowchart` か `sequenceDiagram`", "図の種類を挙げないと、" +
			"何を描けばよいのかが決まりません"},
	} {
		if !strings.Contains(section, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", planReviewHeading, want.needle, want.why)
		}
	}
}

// 目的: 組み込みのプロンプトが、人間へ読ませるコメントの節の形を教えることを固定する
// （#249（エージェントのコメントに図と前提が無く、人間が「詳しく説明して」で1往復を
// 使わされる）。設計 5-3q）。
//
// **印の行より前に書かせない、という警告も同時に見る。**
// 判断票と成果の報告は、**本文の先頭が HTML のコメントの印でなければ数えられない。**
// 4つの1つ目が引用なので、警告が落ちると、エージェントは自然に引用を冒頭へ置く。
// **そうなると continuo は「成果が書かれていない」と判断して、この run を人間へ渡す。**
//
// **途中経過の報告を対象に含めていないことも見る。**
// あちらが書かせるのは1行だけで、4つを置く場所が無い。
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 節があり、4つの見出しと、印より前に書かせない警告を教えていること。
func TestTemplate_組み込みのプロンプトはコメントの節の形を決めている(t *testing.T) {
	body := prompt.Builtin()

	if !strings.Contains(body, "\n"+commentFormatHeading+"\n") {
		t.Fatalf("組み込みのプロンプトに %q の節がありません。"+
			"形が決まっていないと、前提も単語の説明も無いコメントが届きます", commentFormatHeading)
	}

	// **sectionOf は使えない。**あれは次の `## ` までを切るが、
	// **5-5 の次の見出しは `# 6. セキュリティ` で、井桁が1つである。**
	// そのまま使うと 6章と7章まで含んでしまい、**5-5 から中身が消えても素通りする。**
	section := sectionUntilNextChapter(t, body, commentFormatHeading)

	for _, want := range []struct {
		needle string
		why    string
	}{
		{"### 何に対する返答か", "何への返答かが無いと、読む人はどの話かを探すことになります"},
		{"### 前提条件", "前提が無いと、その話が成り立つ条件を訊き直されます"},
		{"### 単語の説明", "語の意味が無いと、その語だけを訊き返されて1往復を使います"},
		{"### 何が問題なのか", "症状と、放っておくと何が起きるかが無いと、" +
			"読む人は決めるための材料を持てません"},
		{"印の行より前には、1文字も置かないでください", "この警告が無いと、" +
			"エージェントは4つの1つ目（引用）をコメントの冒頭へ置きます。" +
			"印が先頭から外れると、continuo は成果を数えず、CI の検査も数えません"},
	} {
		if !strings.Contains(section, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", commentFormatHeading, want.needle, want.why)
		}
	}

	// **途中経過の報告を対象へ入れてはならない。**
	// 5-3 が書かせるのは `- <日時> いま <何をしているか>` の1行だけで、4つを置く場所が無い。
	if !strings.Contains(section, "途中経過の報告（5-3）は当たりません") {
		t.Errorf("%q の節が、途中経過の報告を対象から外していません。"+
			"あちらは1行足すだけの決まりなので、4つを置く場所がありません", commentFormatHeading)
	}
}

// sectionUntilNextChapter は、指定した見出しから次の見出しの直前までを切り出す。
//
// **sectionOf との違いは、切る相手である。**あちらは `## ` だけで切るが、こちらは
// `# ` でも切る。**節が章の最後に在るときは、`## ` で切ると次の章まで丸ごと入る。**
// 入ったまま検査すると、**その節から中身が消えても、別の章に同じ語があれば通ってしまう。**
//
// t: テストコンテキスト。
// body: 切り出す元の文面。
// heading: 探す見出しの行そのもの。
// 戻り値: 見出しの次の行から、次の `# ` か `## ` の直前までの中身。
func sectionUntilNextChapter(t *testing.T, body, heading string) string {
	t.Helper()

	lines := strings.Split(body, "\n")
	start := -1
	for i, line := range lines {
		if line == heading {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("本文から %q の見出しを取り出せません", heading)
	}
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "# ") || strings.HasPrefix(lines[i], "## ") {
			return strings.Join(lines[start:i], "\n")
		}
	}
	return strings.Join(lines[start:], "\n")
}

// 目的: コメントの形を決める節が、組み込みの「後半」に在ることを固定する（設計 5-3c）。
//
// **前半へ移すと、利用者の本文（4-4）より前に読まれる。**
// この節は「4-4 と、4-4 が読ませている文書が形を決めているなら、それにも従う」と書いているので、
// **4-4 より前に置くと、まだ読んでいないものを指すことになる。**
//
// 与える情報: prompt.Build() が本文を挟んで組み立てた全文。
// 成功条件: 4-4 の見出しより後ろに在ること。
func TestTemplate_コメントの形を決める節は本文より後ろにある(t *testing.T) {
	// **中身のある本文を渡す。**見出しだけだと、中身が無い節として落ちる。
	body := prompt.Build("### 固有の目印\n\n固有の中身です。\n", "/tmp/WORKFLOW.md").Text()

	projectSpecific := strings.Index(body, "\n## 4-4. このプロジェクトの決まり\n")
	format := strings.Index(body, "\n"+commentFormatHeading+"\n")
	if projectSpecific < 0 || format < 0 {
		t.Fatalf("見出しが揃っていません（4-4=%d / %q=%d）", projectSpecific, commentFormatHeading, format)
	}
	if format < projectSpecific {
		t.Errorf("%q が「## 4-4. このプロジェクトの決まり」より前にあります。"+
			"この節は 4-4 の決まりにも従えと書いているので、先に読ませると指す先がありません",
			commentFormatHeading)
	}
}
