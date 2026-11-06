// issue #321（全てのコメントに作業目標と残作業を書かせる）で足した3つの決まりを見張る検査である（設計 5-3za）。
//
//  1. エージェントが書くどのコメントにも、受け入れ目標・いまの作業とその寄与・残りを書かせる
//  2. pull request を issue の Development にリンクさせる
//  3. 1つの issue に pull request は1つにする
//
// **人間は「スキル化したあたりで明示的に指示を与えないとこのように動いてくれなくなった」と言っている。**
// 決まりの文がスキルから落ちると、また黙って書かなくなる。
package prompt_test

import (
	"strings"
	"testing"
)

// goalsSectionHeading は、どのコメントにも置かせる1節の見出しである。
const goalsSectionHeading = "## 報告: この issue の受け入れ目標と、いまの作業と、残り"

// 目的: writing-comments-for-humans が、目標・寄与・残りを書かせる決まりと形を持つことを固定する。
//
// **8つの見出しを当てない例外も見る。**例外が無いと、「## の節には8つ」の決まりとぶつかり、
// エージェントはこの1節にも8つの見出しを押し込む。
//
// 与える情報: スキル writing-comments-for-humans の全文。
// 成功条件: 決まりの節・3つの語・形の見出し・例外・印より前に置かない文が在ること。
func TestGoals_コメントの書き方が目標と寄与と残りを書かせる(t *testing.T) {
	body := skillText(t, "writing-comments-for-humans")
	for _, want := range []struct{ needle, why string }{
		{"### どのコメントにも、受け入れ目標・いまの作業・残りを書く", "決まりの節が無いと、明示しない限り書かれません（issue #321 の本文）"},
		{"受け入れ目標", "issue を閉じてよい条件が書かれないと、何が片付いたかが分かりません"},
		{"それが受け入れ目標のどれに寄与するか", "いまの作業がどの目標に効くかが書かれません"},
		{"誰の番か", "残りに誰の番かが無いと、人間は自分が何をすればよいか分かりません"},
		{"進める順に番号を付けた詳しい手順", "残りが一語だと、何をどうするのかが人間に伝わりません（人間のコメント 6599057291）"},
		{"人間が次にすること", "人間が次に何をすればよいかが無いと、人間はコメントを読んでも動けません（人間のコメント 6599057291）"},
		{goalsSectionHeading, "形の見出しが無いと、置き場所と形がコメントごとにばらつきます"},
		{"この節には8つの見出しを当てません", "例外が無いと、「## の節には8つ」とぶつかります"},
		{"印より前には置きません", "印が先頭から外れると、continuo も CI もそのコメントを数えません"},
		{"レビューで直さないと決めた指摘は、残りに入れません", "直さない指摘を残りに並べると、マージのあとに2つ目の pull request が生まれます"},
	} {
		if !strings.Contains(body, want.needle) {
			t.Errorf("writing-comments-for-humans に %q がありません。%s", want.needle, want.why)
		}
	}
}

// 目的: 計画・成果の報告・判断票の見本が、印と題名のあとに目標の1節を持つことを固定する。
//
// **見本に無いと、決まりの節を読んでも写す形が見本のほうに引きずられる。**
// **印の行より前に置かれていないことも見る。**前に置くと continuo が成果を数えない。
//
// 与える情報: 各スキルの全文。
// 成功条件: 見本ごとに、印の行 < 目標の見出し < 最初の返信の節 の順で並ぶこと。
func TestGoals_見本が目標の節を印のあとに置く(t *testing.T) {
	for _, c := range []struct {
		skill  string
		marker string // 見本の印の行
		next   string // 目標の節のあとに来る行
	}{
		{"writing-plan", "<!-- continuo:plan -->", "## 報告: <一言で中身が想像できる節の題名>"},
		{"writing-comments-for-humans", "<!-- continuo:plan -->", "## 報告: <一言で中身が想像できる節の題名>"},
		{"finishing-run", "# <何をしたかを一言で>", "ここに「writing-comments-for-humans」の8つの見出しで、何をしたかを書く（印は上の1行だけ）"},
		{"posting-review-verdict", "<summary>1周目</summary>", "## 報告: <一言で中身が想像できる節の題名>"},
		{"posting-review-verdict", "cat > \"$ROUND\" <<'ROUND'", "## 報告: <一言で中身が想像できる節の題名>"},
	} {
		body := skillText(t, c.skill)
		m := strings.Index(body, c.marker)
		if m < 0 {
			t.Errorf("%s に見本の %q がありません", c.skill, c.marker)
			continue
		}
		rest := body[m:]
		g := strings.Index(rest, goalsSectionHeading)
		n := strings.Index(rest, c.next)
		if g < 0 || n < 0 || !(g < n) {
			t.Errorf("%s の見本（%q から）で、目標の節（%d）が %q（%d）より前にありません", c.skill, c.marker, g, c.next, n)
		}
	}
}

// 目的: 途中経過とまとめて直したときの報告にも、目標・寄与・残りを書かせることを固定する。
//
// **この2つは8つの見出しの対象外である**ので、形が別に要る。途中経過は1行のまま書かせる。
//
// 与える情報: スキル reporting-progress と finishing-run の全文。
// 成功条件: 途中経過の1行の形と、まとめて直したときの review と blocked の見本の行が在ること。
func TestGoals_途中経過とまとめて直したときの報告にも書かせる(t *testing.T) {
	progress := skillText(t, "reporting-progress")
	if !strings.Contains(progress, "<いま何をしているか>。受け入れ目標「<目標を全部短く並べる>」のうち「<いまの作業が寄与するもの>」に寄与。残りの作業: <進める順に、誰の番かを添えて>。人間が次にすること: <無ければ「ありません」>") {
		t.Error("reporting-progress に、途中経過の1行の形がありません。途中経過だけを読んでも目標と残りが分かりません")
	}
	finishing := skillText(t, "finishing-run")
	for _, want := range []struct {
		needle string
		n      int
	}{
		{"- この issue の受け入れ目標: <この issue を閉じてよい条件>", 2},
		{"- 人間が次にすること: <人間が次にする操作。", 2},
		{"人間が次にすること: <無ければ「ありません」>）`", 2},
	} {
		if got := strings.Count(finishing, want.needle); got < want.n {
			t.Errorf("finishing-run に %q が %d 箇所しかありません（%d 箇所要ります。まとめて直したときの見本）", want.needle, got, want.n)
		}
	}
}

// 目的: pull request を issue の Development にリンクさせ、確かめさせることを固定する。
//
// **`Closes #<番号>` は、向け先が既定の branch でないと GitHub が読まない。**
// branch を issue に結びつける段と、成果の報告の前に確かめる段の両方が要る。
// **結びつける段は push より前でなければならない。**先に push すると、スクリプトは「remote に在る」で何もしない。
//
// 与える情報: スキル implementing-and-pushing・finishing-run・reporting-progress の全文。
// 成功条件: 決まりの文・スクリプトの叩き方・push を後に回す文・確かめのコマンドが在ること。
func TestGoals_pullrequestをDevelopmentにリンクさせる(t *testing.T) {
	ip := skillText(t, "implementing-and-pushing")
	section := sectionOf(t, instructionsText(t), pullRequestHeading)
	for _, want := range []struct{ where, needle, why string }{
		{section, "**pull request は必ず issue の Development にリンクします。**", "人間が「必ずissueにリンクしろ」と指示しています"},
		{section, "CONTINUO_PLUGIN_DIR/skills/implementing-and-pushing/scripts/link-branch.sh", "結びつけるスクリプトの叩き方が無いと、リンクされません"},
		{section, "### 段2. branch を issue に結びつける", "結びつける段が無いと、向け先が既定でない pull request はリンクされません"},
		{section, "### 段3. push して、draft で作る", "push が段2 より後でないと、結びつけられません"},
		{ip, "ここで push せず、下の「pull request を出す」の段0 へ進みます", "「commit して push する」で先に push すると、結びつけられません。段0 を通らないと、目標を確かめずに PR を作ります"},
		{ip, "最初の push は下の「pull request を出す」の段0〜段3 で行い", "入口が段0 を指さないと、目標を確かめずに PR を作ります"},
		{ip, "段0 から段3 を通り", "入口が段0 を指さないと、目標を確かめずに PR を作ります"},
		{ip, "「pull request を作らずに止まる」と決めたときは、「pull request を出す」の段2（branch を issue に結びつける）を通らずに", "止まるときの push の道が無いと、PR を作る段へ戻るか、push せずに作業を失います"},
	} {
		if !strings.Contains(want.where, want.needle) {
			t.Errorf("implementing-and-pushing に %q がありません。%s", want.needle, want.why)
		}
	}
	if !strings.Contains(skillText(t, "reporting-progress"), "ここで push せず、「implementing-and-pushing」の「pull request を出す」の段0 から進み") {
		t.Error("reporting-progress の途中の push が、開いた pull request が無いときに先に push しています。結びつけられなくなります")
	}
	finishing := skillText(t, "finishing-run")
	for _, want := range []string{
		"closingIssuesReferences",
		"gh issue develop --list <issue の番号> --repo <issue のリポジトリ>",
		"issue の Development 欄で pull request がつながっているかを確かめ、無ければつなぐ",
	} {
		if !strings.Contains(finishing, want) {
			t.Errorf("finishing-run の確かめに %q がありません。リンクされていないことに誰も気づけません", want)
		}
	}
}

// 目的: 1つの issue に pull request を1つにさせることを固定する。
//
// **issue #318 では、直さない指摘を人間に投げた報告のあとに、同じ branch から PR が3つできた。**
// 決まりの文・`--state all` で探すこと・fork を除くこと・2つ目の条件・別の名前で出さないこと・
// 直さない指摘を投げないことを見る。
//
// 与える情報: スキル implementing-and-pushing・trusting-instructions・finishing-run の全文。
// 成功条件: それぞれの文が在ること。
func TestGoals_issueごとのpullrequestを1つにする(t *testing.T) {
	section := sectionOf(t, instructionsText(t), pullRequestHeading)
	for _, want := range []struct{ needle, why string }{
		{"**1つの issue に pull request は1つです。**", "人間が「issueで作るPRは基本的に1つにしろ」と指示しています"},
		{"--state all --json number,url,isCrossRepository,mergedAt,closedAt", "開いたものだけを探すと、マージ済みの PR を見落として黙って2つ目を作ります"},
		{"`isCrossRepository` が `true` のもの", "fork の同じ名前の branch の PR を行き先と取り違えます"},
		{"`mergedAt` と `closedAt` が両方 `null` かで決めます", "state の綴りは取り方で変わります"},
		{"新しい作業を頼んでいるときだけ", "2つ目を作ってよい条件が無いと、特別な理由なしに作ります"},
		{"close 済みの pull request を自分で開き直しません", "人間が閉じた PR を勝手に戻します"},
	} {
		if !strings.Contains(section, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", pullRequestHeading, want.needle, want.why)
		}
	}
	if !strings.Contains(skillText(t, "trusting-instructions"), "2つ目の pull request を出すときも、別の名前にはしません。") {
		t.Error("trusting-instructions が、2つ目の pull request を別の名前で出させています。次の run が見つけられず3つ目を作ります")
	}
	if !strings.Contains(skillText(t, "finishing-run"), "「直してほしければコメントしてください」と人間に投げません") {
		t.Error("finishing-run が、直さない指摘を人間に投げることを止めていません（issue #318 の2つ目の PR の原因）")
	}
}

// 目的: 着手のときに受け入れ目標を並べ、PR を作る前と draft を外す前に全部達成できるかを確かめさせ、
// 達成できない目標を AI が黙って変えないことを固定する（issue #321 の人間のコメント 6597530623）。
//
// **人間は「達成できない場合は作業目標自体が間違っている可能性があるので、変更するなら人間に確認しろ」と言った。**
// 目標を黙って外すと、残りが別の pull request に回り、PR が増える（issue #318）。
//
// 与える情報: スキル starting-run・implementing-and-pushing・finishing-pull-request・writing-comments-for-humans の全文。
// 成功条件: それぞれの段と、例外なしの文と、PR の本文の見本の節が在ること。
func TestGoals_着手とPRを作るときに受け入れ目標を確かめる(t *testing.T) {
	for _, c := range []struct{ skill, needle, why string }{
		{"starting-run", "## 受け入れ目標を確かめる", "着手のときに目標を並べないと、PR を作るときに照らす一覧がありません"},
		{"starting-run", "**一覧の目標を、あなたの判断だけで変えたり外したりしません。**", "達成できない目標を黙って外すと、残りが別の PR に回ります"},
		{"implementing-and-pushing", "### 段0. 受け入れ目標を全部達成できるかを確かめる", "PR を作る前に確かめないと、目標の一部を落としたまま出します"},
		{"implementing-and-pushing", "目標そのものが間違っている可能性があります", "人間に確かめる理由が書かれていません"},
		{"finishing-pull-request", "段0. 外す前に、「starting-run」の「受け入れ目標を確かめる」で並べた目標を1つずつ見て", "draft を外す前に確かめないと、達成していない PR がマージできる状態になります"},
		{"finishing-pull-request", goalsSectionHeading, "PR の本文に目標の節が無いと「例外なし」になりません"},
		{"writing-comments-for-humans", "どれにも次の4つを書きます。例外はありません。", "人間は「例外なし」と指示しています"},
	} {
		if !strings.Contains(skillText(t, c.skill), c.needle) {
			t.Errorf("%s に %q がありません。%s", c.skill, c.needle, c.why)
		}
	}
	if strings.Contains(skillText(t, "writing-comments-for-humans"), "pull request の本文はコメントではないので、この節は当てません") {
		t.Error("writing-comments-for-humans が PR の本文を対象から外しています。人間は「例外なし」と指示しています")
	}
}

// 目的: 前の周の直しの残り（PR を作るときの本文の目標の節・まとめて直す issue の目標・
// 人間が起動する Claude Code 向けの plugin の決まり）を見張る（issue #321）。
//
// 与える情報: 各スキルの全文と、plugin continuo-issue-comments の SKILL.md。
// 成功条件: それぞれの文が在ること。
func TestGoals_作るときの本文とまとめて直すissueと人間向けのpluginにも目標を書かせる(t *testing.T) {
	ip := skillText(t, "implementing-and-pushing")
	m := strings.Index(ip, "<!-- continuo:pull-request-in-progress -->\n作業の途中です。")
	if m < 0 || !strings.Contains(ip[m:], goalsSectionHeading) {
		t.Error("implementing-and-pushing の段3 の本文の見本に、目標の節がありません。作ったときの PR の本文が例外になります")
	}
	if !strings.Contains(skillText(t, "starting-run"), "まとめて直すなら、その issue の受け入れ目標も同じように並べます") {
		t.Error("starting-run が、まとめて直す issue の目標を並べさせていません。一緒に閉じる issue の目標を確かめずに閉じます")
	}
	plugin := readSkill(t)
	for _, want := range []string{"## 3. What every comment you write carries", "the acceptance goals of the issue", "whose turn it is", "what the human should do next"} {
		if !strings.Contains(plugin, want) {
			t.Errorf("plugin continuo-issue-comments に %q がありません。人間が起動する Claude Code のコメントが例外になります", want)
		}
	}
}
