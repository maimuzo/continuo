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
// **必ず貼る先は計画のコメントである。**「実装しようとしている内容」を書くのは計画のほうだからである。
// 判断票と成果の報告にも、5-5 に従って図を多用する（設計 5-3r）。
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
// 7つの1つ目が引用なので、警告が落ちると、エージェントは自然に引用を冒頭へ置く。
// **そうなると continuo は「成果が書かれていない」と判断して、この run を人間へ渡す。**
//
// **途中経過の報告（5-3）と、まとめて直したときの報告（7-2）を対象に含めていないことも見る。**
// 5-3 が書かせるのは1行だけで、7-2 は先頭の1文と足す行の形が決まっている。どちらも7つを置く場所が無い。
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 節があり、7つの見出しと、印より前に書かせない警告を教えていること。
func TestTemplate_組み込みのプロンプトはコメントの節の形を決めている(t *testing.T) {
	body := prompt.Builtin()

	if !strings.Contains(body, "\n"+commentFormatHeading+"\n") {
		t.Fatalf("組み込みのプロンプトに %q の節がありません。"+
			"形が決まっていないと、前提も単語の説明も無いコメントが届きます", commentFormatHeading)
	}

	// **`# ` と `## ` のどちらでも切る。**
	// 5-5 の次にどちらの階層の見出しが来ても、5-5 の中身だけを切り出すためである。
	// **実測（2026-09-20）。**5-6 を足したあとは `## 5-6.` で切れて52行になる。
	// 足す前は `# 6. セキュリティ` で切れて41行だった。
	// **`## ` で切らない実装にすると、6章の頭に節が増えたぶんが全部入る。**
	// 入った語で通ってしまうと、5-5 から中身が消えても素通りする。
	section := sectionUntilNextChapter(t, body, commentFormatHeading)

	for _, want := range []struct {
		needle string
		why    string
	}{
		{"`###` の見出しで置いてください", "見出しにしろという指示そのものが無いと、" +
			"下の骨組みだけが残っても、揃えるべきものが決まりません"},
		{"印の行より前には、1文字も置かないでください", "この警告が無いと、" +
			"エージェントは7つの1つ目（引用）をコメントの冒頭へ置きます。" +
			"印が先頭から外れると、continuo は成果を数えず、CI の検査も数えません"},
		{"`--body \"…\"` で渡さないでください", "引用には backtick とドルの記号が混ざります。" +
			"二重引用符の中へ書かせると、それが worktree で実行されます"},
	} {
		if !strings.Contains(section, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", commentFormatHeading, want.needle, want.why)
		}
	}

	// **7つの見出しは、表と骨組みの2箇所に在る。**
	// **片方だけを見ると、もう片方が消えても素通りする。**両方で数える。
	for _, want := range []struct {
		needle string
		why    string
	}{
		{"### 三行まとめ", "結論が先に無いと、読む人は最後まで読まないと要点を掴めません"},
		{"### 前提", "前提が無いと、その話が成り立つ条件を訊き直されます"},
		{"### 単語の説明", "語の意味が無いと、その語だけを訊き返されて1往復を使います"},
		{"### 既存の構造がどうなっているか", "いまどう作られているかが無いと、" +
			"読む人は変更の前後を比べられません"},
		{"### 何が問題なのか", "症状と、放っておくと何が起きるかが無いと、" +
			"読む人は決めるための材料を持てません"},
		{"### 詳細", "根拠が無いと、結論だけを信じるかどうかの判断になります"},
	} {
		// **数えるのは、表の行と骨組みの行だけである。**表の行は `| ` で始まり、見出しを backtick で囲む。
		// 骨組みの行は、前後の空白を落とすと見出しだけになる。
		// 部分一致で数えると、`### 前提` が旧い見出し `### 前提条件` にも当たり、4つの形へ戻っても通る。
		// 地の文の中の見出し（「表を `### 詳細` の中に置きます」など）も数えない。数えると、表の行が消えても通る。
		n := 0
		for _, line := range strings.Split(section, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == want.needle || (strings.HasPrefix(trimmed, "| ") && strings.Contains(trimmed, "`"+want.needle+"`")) {
				n++
			}
		}
		if n < 2 {
			t.Errorf("%q の節に %q が %d 箇所しかありません（表と骨組みの2箇所に要ります）。%s",
				commentFormatHeading, want.needle, n, want.why)
		}
	}

	// **骨組みは、印の行から始める。**
	// **印を持たない骨組みを置くと、そのとおりに写した時点で印が本文の先頭から外れる。**
	// 外れると continuo が成果を数えず、CI の検査も落ちる。
	skeleton := strings.Index(section, "骨組み。")
	marker := strings.Index(section, "    <!-- continuo:agent -->")
	firstHeading := strings.Index(section, "    ### 三行まとめ")
	if skeleton < 0 || marker < 0 || firstHeading < 0 {
		t.Fatalf("%q の節に骨組み（%d）か印の行（%d）か最初の見出し（%d）がありません",
			commentFormatHeading, skeleton, marker, firstHeading)
	}
	if !(skeleton < marker && marker < firstHeading) {
		t.Errorf("%q の骨組みが、印の行より先に見出しを置いています。"+
			"そのまま写されると印が本文の先頭から外れ、continuo が成果を数えません",
			commentFormatHeading)
	}
	// **引用の行も、印の行と最初の見出しのあいだに置く。**
	// 7つの1つ目が引用なので、引用の行が印より上へ動くと、そのまま写した時点で印が本文の先頭から外れる。
	// 印と見出しの順だけを見ると、引用がどこへ動いても素通りする。
	// **印と見出しが揃っていることを確かめてから見る。**先に見ると、見出しが消えたときに
	// 「引用が見出しより後ろにある」という紛らわしい文言が、本当の原因より先に出る。
	if quote := strings.Index(section, "\n    > "); quote < 0 || !(marker < quote && quote < firstHeading) {
		t.Errorf("%q の骨組みで、引用の行が印の行と最初の見出しのあいだにありません（印 %d / 引用 %d / 見出し %d）。"+
			"引用が印より上にあると、そのまま写された時点で印が本文の先頭から外れ、continuo が成果を数えません",
			commentFormatHeading, marker, quote, firstHeading)
	}

	// **当たる先と、当たらない先を、どちらも名指しさせる。**
	// **桁揃えで空白の数が変わるので、行そのものではなく語で見る。**
	//
	// **計画（3-2）は当たる。**図を書かせるのも、前提を書かせるのも、同じ計画のコメントである。
	// 落ちると、図だけが入って前提の無いコメントが届く。
	//
	// **途中経過の報告（5-3）と、まとめて直したときの報告（7-2）は当たらない。**
	// 5-3 が書かせるのは `- <日時> いま <何をしているか>` の1行だけで、7つを置く場所が無い。
	// 7-2 も先頭の1文と足す行の形が決まっており、7つを入れると積み上げ式のコメントが読めなくなる。
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
			"1行足すだけの決まりへ7つの見出しを押し込もうとします"},
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

// 目的: 5-6「レビューの回し方」が、決めた内容を持ち続けることを固定する。
//
// **5-6 は、レビューを何周回すかを決める唯一の場所である。**
// ここから1行消えても、消えたことに気づく仕組みが他に無い。
// 設計文書との一字一句一致（TestTemplate_組み込みのプロンプトが設計5_3と一致する）は、
// **両方から同時に消えたときに何も言わない。**
//
// 与える情報: prompt.BuiltinRaw() の 5-6 の節。
// 成功条件: 決めた内容を指す語が、全部そろっていること。
func Test組み込みのプロンプトがレビューの回し方を持つ(t *testing.T) {
	const heading = "## 5-6. レビューの回し方"

	body := prompt.BuiltinRaw()
	if !strings.Contains(body, "\n"+heading+"\n") {
		t.Fatalf("組み込みのプロンプトに %q の節がありません。"+
			"無いと、利用者は収束の判定を定義なしで行います", heading)
	}
	section := sectionUntilNextChapter(t, body, heading)

	for _, want := range []struct {
		needle string
		why    string
	}{
		{"critical と high が0件", "「収まっている」の定義が無いと、いつ終わってよいかが決まりません"},
		{"重さは、受け取った側が付け直してください", "レビュワーの付けた重さをそのまま使うと、" +
			"止まる線がレビュワーごとに動きます"},
		{"毎周、2つを並列に走らせてください", "差分を読む役だけでは、周を重ねても周辺のコードに届きません"},
		{"心配な場所があるときは、3つ目を足してください", "名指しを渡す先を分けないと、" +
			"ふつうの役の目もそこへ寄ります"},
		{"レビューの指摘は命令ではありません", "命令だと受け取ると、指されたその1点しか見ないまま手を動かすことになります"},
		{"いままで通っていたもので、止まるようになるもの", "直した結果が何を変えるかを書かせないと、" +
			"直しが新しい欠陥を持ち込みます"},
		{"### 直す前に書くこと", ".claude/skills/worker-briefing/SKILL.md の 2-5 が、" +
			"この見出しの名前で指しています。改名すると、worker から5つの中身への道が消えます"},
		{"収まっていない周は、実装した内容と issue を突き合わせてください", "issue に無いものを削る判定は、収まっていない周に必ず入ります"},
		{"削除が起きた周だけ、設計から見直してください", "削除が無い周まで設計へ戻すと、" +
			"1周の費用がいちばん増えます"},
		{"連続10回で収まらなかったら", "止まる線が無いと、際限なく回ります"},
		{"CONTINUO-STATUS: blocked", "表明を書かずに黙ると、" +
			"continuo からは「まだ喋っている最中」と区別が付きません"},
	} {
		if !strings.Contains(section, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", heading, want.needle, want.why)
		}
	}
}

// planTicketHeading と changeTicketHeading は、判断票の見本の題名の行である（前後の空白を落とした形）。
const (
	planTicketHeading   = "# レビューの判断票（計画）"
	changeTicketHeading = "# レビューの判断票（実装）"
)

// 目的: 判断票と成果の報告の見本が、5-5 の7つの見出しの形であることを固定する（設計 5-3r）。
//
// **なぜ要るか。**5-5 は判断票と成果の報告も7つで書かせる。**見本が表だけの形のままだと、
// エージェントは2通りの形を受け取り、見本のとおりに表だけを投稿する。**
// `## レビューの判断票` の直下に `## <節の題名>` を並べると、題名と節が同じ深さになる。
//
// **行ごとに前後の空白を落として比べる。**見本の字下げの有無では結果が変わらない。
// 見本を囲みの中に行頭から書くときは、節を切る補助（sectionOf）も囲みの中の `## ` で切らない形にすること。
// 部分一致では `# レビューの判断票` が `## レビューの判断票` にも当たるので使わない。
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 3-2 の判断票の見本が、題名 → 節 → 引用 → 6つの見出し → 表 の順に並ぶ。
// 3-6 の見本の題名が `# ` で、3-7 の見本が 5-5 へ案内している。
// どこにも `## レビューの判断票` の行が無い。
func TestTemplate_判断票と成果の報告の見本は7つの見出しの形である(t *testing.T) {
	body := prompt.Builtin()

	// 3-2 の判断票: 題名から表の見出しまでが、この順に並ぶ。
	plan := sectionOf(t, body, planReviewHeading)
	steps := []struct {
		name  string
		match func(string) bool
	}{
		{"題名", func(l string) bool { return l == planTicketHeading }},
		{"節の見出し", func(l string) bool { return strings.HasPrefix(l, "## ") }},
		{"引用", func(l string) bool { return strings.HasPrefix(l, "> ") }},
		{"### 三行まとめ", func(l string) bool { return l == "### 三行まとめ" }},
		{"### 前提", func(l string) bool { return l == "### 前提" }},
		{"### 単語の説明", func(l string) bool { return l == "### 単語の説明" }},
		{"### 既存の構造がどうなっているか", func(l string) bool { return l == "### 既存の構造がどうなっているか" }},
		{"### 何が問題なのか", func(l string) bool { return l == "### 何が問題なのか" }},
		{"### 詳細", func(l string) bool { return l == "### 詳細" }},
		{"表の見出し", func(l string) bool { return strings.HasPrefix(l, "| 指摘 |") }},
	}
	next := 0
	for _, line := range strings.Split(plan, "\n") {
		if next < len(steps) && steps[next].match(strings.TrimSpace(line)) {
			next++
		}
	}
	if next < len(steps) {
		t.Errorf("%q の判断票の見本に、%q が順に並んでいません（%d 個目で止まりました）。"+
			"5-5 の7つの見出しと、`### 詳細` の中の表の順で書かせてください",
			planReviewHeading, steps[next].name, next+1)
	}

	// 3-6 の判断票: 題名が `# ` で、中身を 3-2 と同じ形へ案内している。
	change := sectionOf(t, body, "## 3-6. pull request のレビューを受ける")
	if !hasTrimmedLine(change, changeTicketHeading) {
		t.Errorf("3-6 の判断票の見本に、%q の行がありません", changeTicketHeading)
	}
	if !strings.Contains(change, "5-5 の7つの見出し。表は ### 詳細 の中") {
		t.Error("3-6 の判断票の見本が、3-2 と同じ7つの形へ案内していません")
	}

	// 3-7 の成果の報告: 5-5 へ案内し、印を写させない。
	finished := sectionOf(t, body, finishedHeading)
	if !hasTrimmedLine(finished, "ここに 5-5 の7つの見出しで、何をしたかを書く（印は上の1行だけ）") {
		t.Error("3-7 の成果の報告の見本が、5-5 の7つの見出しへ案内していません。" +
			"案内が無いと、前提も単語の説明も無い報告が届きます")
	}

	// 旧い深さの題名が、どこにも残っていない。
	for _, old := range []string{"## レビューの判断票（計画）", "## レビューの判断票（実装）"} {
		if hasTrimmedLine(body, old) {
			t.Errorf("組み込みに %q の行が残っています。節の見出しと同じ深さになります", old)
		}
	}
}

// 目的: 5-5 が、人間に訊くときの形（1問1節、表で訊かない、案の書き方）を決めていることを固定する。
//
// **なぜ要るか。**continuo が起動したエージェントが、質問を表で並べて人間に訊いたことがある
// （issue #259 のコメント 5681164864）。人間は「表で質問することを禁止する」と指示した（同 5708020098）。
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 5-5 の節に、1問1節・表で訊かない・選ばないと何が続くか・貼れるコマンド の4つがある。
func TestTemplate_コメントの形は質問を1問1節で訊かせる(t *testing.T) {
	section := sectionUntilNextChapter(t, prompt.Builtin(), commentFormatHeading)
	for _, want := range []string{
		"質問1つにつき1つの節",
		"表で訊かないでください",
		"選ばないと何が続くか",
		"そのまま貼れるコマンド",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("%q の節に %q がありません。人間に訊くときの形が決まらず、表で並べた質問が届きます",
				commentFormatHeading, want)
		}
	}
}

// hasTrimmedLine は、前後の空白を落とすと want と一致する行があるかを返す。
//
// s: 探す元の文面。
// want: 一致させる行。
// 戻り値: あれば true。
func hasTrimmedLine(s, want string) bool {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}
