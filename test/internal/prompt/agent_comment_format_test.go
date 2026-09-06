package prompt_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/prompt"
)

// commentFormatHeading は、人間へ質問・報告するコメントの書き方を教える節の見出しである。
const commentFormatHeading = "## 5-5. 人間へ質問・報告するコメントの書き方"

// planReviewHeading は、計画とその判断票を書かせる節の見出しである。図もこの節が書かせる。
const planReviewHeading = "## 3-2. 計画を書き、レビューを受ける"

// 目的: 組み込みのプロンプトが、計画のコメントに mermaid の図を書かせることを固定する
// （#249（エージェントのコメントに図と前提が無く、人間が「詳しく説明して」で1往復を
// 使わされる）。設計 5-3r）。
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
// **貼る先は計画のコメントである。**判断票ではない。
// 「実装しようとしている内容」を書くのは計画のほうで、判断票はレビューの結果だからである。
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 3-2 の節が、計画へ図を書かせる指示・書式（mermaid）・図の種類の3つを教えていること。
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
		{"実装する内容の概要の図", "計画に書くことの一覧へ入れないと、" +
			"図は「あれば良いもの」として落ちます"},
		{"```mermaid", "書式を決めないと、罫線文字で描いたものが届きます。" +
			"GitHub は mermaid だけをそのまま図として表示します"},
		{"`flowchart` か `sequenceDiagram`", "図の種類を挙げないと、" +
			"何を描けばよいのかが決まりません"},
	} {
		if !strings.Contains(section, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", planReviewHeading, want.needle, want.why)
		}
	}

	// **図の書き方は、計画のコメントの見本より前に置く。**
	// 見本より後ろに置くと、見本を読んだ時点では図を書く指示を知らないまま、
	// **その見本のとおりに投稿してしまう。**
	how := strings.Index(section, "**図の書き方。**")
	sample := strings.Index(section, "計画のコメントの形。")
	if how < 0 || sample < 0 {
		t.Fatalf("%q の節に、図の書き方（%d）か計画のコメントの見本（%d）がありません",
			planReviewHeading, how, sample)
	}
	if how > sample {
		t.Errorf("%q の節で、図の書き方が計画のコメントの見本より後ろにあります。"+
			"見本を読んだ時点で図の指示を知らないと、そのまま投稿されます", planReviewHeading)
	}
}

// 目的: 組み込みのプロンプトが、人間へ読ませるコメントの節の形を教えることを固定する
// （#249（エージェントのコメントに図と前提が無く、人間が「詳しく説明して」で1往復を
// 使わされる）。設計 5-3r）。
//
// **印の行より前に書かせない、という警告も同時に見る。**
// 判断票と成果の報告は、**本文の先頭が HTML のコメントの印でなければ数えられない。**
// 4つの1つ目が引用なので、警告が落ちると、エージェントは自然に引用を冒頭へ置く。
// **そうなると continuo は「成果が書かれていない」と判断して、この run を人間へ渡す。**
//
// **途中経過の報告（5-3）と、まとめて直したときの報告（7-2）を対象に含めていないことも見る。**
// 5-3 が書かせるのは1行だけで、7-2 は先頭の1文と足す行の形が決まっている。どちらも4つを置く場所が無い。
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
	// **6章の頭は `# 6. セキュリティ` で、井桁が1つである。**
	// そのまま使うと、次の `## 6-1.` までが入る。
	//
	// **`# ` で切っても、5-5 の後ろの節はこの範囲に入る。**
	// いまは `## 5-6.`（機械が書いた印。issue #245）が後ろに在り、そのぶんも含まれる。
	// **それでも `## ` で切るより狭い。**6章の頭に節が増えても入らないことが、この切り方の値打ちである。
	// **下は `Contains` で見るので、後ろの節が入っても判定は変わらない。**
	// **入った語で通ってしまうと、5-5 から中身が消えても素通りする**ので、
	// **5-5 にしか無い言い回しだけを探すこと。**
	section := sectionUntilNextChapter(t, body, commentFormatHeading)

	for _, want := range []struct {
		needle string
		why    string
	}{
		{"`###` の見出しで置いてください", "見出しにしろという指示そのものが無いと、" +
			"下の骨組みだけが残っても、揃えるべきものが決まりません"},
		{"印の行より前には、1文字も置かないでください", "この警告が無いと、" +
			"エージェントは4つの1つ目（引用）をコメントの冒頭へ置きます。" +
			"印が先頭から外れると、continuo は成果を数えず、CI の検査も数えません"},
		{"`--body \"…\"` で渡さないでください", "引用には backtick とドルの記号が混ざります。" +
			"二重引用符の中へ書かせると、それが worktree で実行されます"},
	} {
		if !strings.Contains(section, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", commentFormatHeading, want.needle, want.why)
		}
	}

	// **4つの見出しは、表と骨組みの2箇所に在る。**
	// **片方だけを見ると、もう片方が消えても素通りする。**両方で数える。
	for _, want := range []struct {
		needle string
		why    string
	}{
		{"### 何に対する返答か", "何への返答かが無いと、読む人はどの話かを探すことになります"},
		{"### 前提条件", "前提が無いと、その話が成り立つ条件を訊き直されます"},
		{"### 単語の説明", "語の意味が無いと、その語だけを訊き返されて1往復を使います"},
		{"### 何が問題なのか", "症状と、放っておくと何が起きるかが無いと、" +
			"読む人は決めるための材料を持てません"},
	} {
		if n := strings.Count(section, want.needle); n < 2 {
			t.Errorf("%q の節に %q が %d 箇所しかありません（表と骨組みの2箇所に要ります）。%s",
				commentFormatHeading, want.needle, n, want.why)
		}
	}

	// **骨組みは、印の行から始める。**
	// **印を持たない骨組みを置くと、そのとおりに写した時点で印が本文の先頭から外れる。**
	// 外れると continuo が成果を数えず、CI の検査も落ちる。
	skeleton := strings.Index(section, "骨組み。")
	marker := strings.Index(section, "    <!-- continuo:agent -->")
	firstHeading := strings.Index(section, "    ### 何に対する返答か")
	if skeleton < 0 || marker < 0 || firstHeading < 0 {
		t.Fatalf("%q の節に骨組み（%d）か印の行（%d）か最初の見出し（%d）がありません",
			commentFormatHeading, skeleton, marker, firstHeading)
	}
	if !(skeleton < marker && marker < firstHeading) {
		t.Errorf("%q の骨組みが、印の行より先に見出しを置いています。"+
			"そのまま写されると印が本文の先頭から外れ、continuo が成果を数えません",
			commentFormatHeading)
	}

	// **当たる先と、当たらない先を、どちらも名指しさせる。**
	// **桁揃えで空白の数が変わるので、行そのものではなく語で見る。**
	//
	// **計画（3-2）は当たる。**図を書かせるのも、前提を書かせるのも、同じ計画のコメントである。
	// 落ちると、図だけが入って前提の無いコメントが届く。
	//
	// **途中経過の報告（5-3）と、まとめて直したときの報告（7-2）は当たらない。**
	// 5-3 が書かせるのは `- <日時> いま <何をしているか>` の1行だけで、4つを置く場所が無い。
	// 7-2 も先頭の1文と足す行の形が決まっており、4つを入れると積み上げ式のコメントが読めなくなる。
	target, exempt := splitAtNextLine(t, section, "**次の2つは当たりません。**")

	for _, want := range []struct {
		where  string
		needle string
		why    string
	}{
		{target, "計画", "計画のコメントが対象から落ちると、" +
			"図を書かせるのと同じコメントなのに、前提の無い図が届きます"},
		{target, "判断票", "判断票が落ちると、レビューの結果だけが理由なしで届きます"},
		{target, "何をしたかの報告", "成果の報告が落ちると、" +
			"何が直ったのかを読む人が組み立て直すことになります"},
		{exempt, "途中経過の報告", "5-3 を除いておかないと、" +
			"1行足すだけの決まりへ4つの見出しを押し込もうとします"},
		{exempt, "まとめて直したときの報告", "7-2 を除いておかないと、" +
			"先頭の1文と足す行の形が決まっているコメントと衝突します"},
	} {
		if !strings.Contains(want.where, want.needle) {
			t.Errorf("%q の節の当たる／当たらないの一覧に %q がありません。%s",
				commentFormatHeading, want.needle, want.why)
		}
	}
}

// splitAtNextLine は、節の中身を目印の行の前と後ろへ切り分ける。
//
// **当たる先の一覧と、当たらない先の一覧を、別々に検査するために要る。**
// 節の全体へ contains を掛けると、**当たらない側にある語でも「在る」と読めてしまう。**
//
// t: テストコンテキスト。
// section: 切り分ける節の中身。
// marker: 切れ目にする行（この行そのものは、どちらにも入らない）。
// 戻り値: 目印より前の部分と、後ろの部分。
func splitAtNextLine(t *testing.T, section, marker string) (string, string) {
	t.Helper()

	i := strings.Index(section, marker)
	if i < 0 {
		t.Fatalf("節の中に切れ目の行 %q がありません。"+
			"当たる先と当たらない先を分けて書いていないと、どちらも検査できません", marker)
	}
	return section[:i], section[i+len(marker):]
}

// sectionUntilNextChapter は、指定した見出しから次の見出しの直前までを切り出す。
//
// **sectionOf との違いは、切る相手である。**あちらは `## ` だけで切るが、こちらは
// `# ` でも切る。**節が章の最後に在ると、`## ` で切ったぶんに次の章の頭が入る。**
// 入ったまま検査すると、**その節から中身が消えても、入ってきた側に同じ語があれば通ってしまう。**
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
