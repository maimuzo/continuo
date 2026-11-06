// issue #337（コメントのサブセクション毎に報告なのか質問なのか依頼なのかを必ず書かせる）で足した決まりを見張る検査である（設計 5-3zd）。
//
// エージェントが書くコメントの `## ` の節の題名を、`報告: `・`質問: `・`依頼: ` のどれかで始めさせる。
// 決まりの文がスキルから落ちると、人間はまた本文を全部読まないと、どこに返事が要るのかを判断できなくなる。
package prompt_test

import (
	"strings"
	"testing"
)

// sectionKinds は、節の題名の頭に付ける3つの種類である。
var sectionKinds = []string{"## 報告: ", "## 質問: ", "## 依頼: "}

// 目的: writing-comments-for-humans が、種類を書かせる決まり・決め方・訳さないこと・範囲を持つことを固定する。
//
// 与える情報: スキル writing-comments-for-humans の全文。
// 成功条件: 決まりの文がそれぞれ在ること。
func TestSectionKind_コメントの書き方が節の題名に種類を書かせる(t *testing.T) {
	body := skillText(t, "writing-comments-for-humans")
	for _, want := range []struct{ needle, why string }{
		{"### 節の題名の頭に種類を書く", "決まりの節が無いと、題名に種類が書かれません（issue #337 の本文）"},
		{"- **`## ` の節の題名は、`報告: `・`質問: `・`依頼: ` のどれか1つで始めます**", "冒頭の要点に無いと、節を読み飛ばしたときに落ちます"},
		{"上から順に見て、最初に当たった1つだけを付けます", "決め方の順が無いと、答えと操作を両方持つ節（確認の節など）の種類が割れます"},
		{"| 質問 | 人間の答えか決定（コメント）を求め、AI が次の run でそれを読んで進む節。", "質問の定義が無いと、計画の確認が依頼と書かれ、問いの判定から外れます"},
		{"| 依頼 | AI は答えを読まず、人間が自分で行う操作だけを、AI から人間へ頼む節", "依頼の定義が無いと、マージの頼みが報告の中に埋もれます"},
		{"**種類の語は訳しません。**", "英語で書かせているときに `Question:` へ訳され、問いの判定（題名の「質問」の文字）から外れます"},
		{"`質問: ` か `依頼: ` の節を1つ以上立て", "人間の番が受け入れ目標の節の1行の中にしか現れません"},
		{"計画・判断票・pull request の本文は、`報告: ` の節だけで書きます", "計画の中で訊く決まり違反（writing-plan の段2）を、種類の決まりが許すことになります"},
		{"`## 質問: 頼みが要る操作の確認` のまま、後ろに言葉を足しません", "確認の節の題名が揺れると、後の run が題名で確認の節を見分けられません"},
		{"題名を `質問: ` で始め", "人間に決めてほしいことを訊く節に種類が付きません"},
	} {
		if !strings.Contains(body, want.needle) {
			t.Errorf("writing-comments-for-humans に %q がありません。%s", want.needle, want.why)
		}
	}
}

// 目的: スキルの見本（コードの囲みの中）の `## ` の節の題名が、どれも種類で始まることを固定する。
//
// **見本に種類が無いと、決まりを読んでもエージェントは見本を写す。**
//
// 与える情報: 種類の決まりが当たるコメントと本文の見本を持つスキルの全文。
// 成功条件: 囲みの中の `## ` で始まる行が、どれも3つの種類のどれかで始まること。1行以上あること。
func TestSectionKind_見本の節の題名が種類で始まる(t *testing.T) {
	for _, skill := range []string{
		"writing-comments-for-humans",
		"writing-plan",
		"posting-review-verdict",
		"finishing-run",
		"implementing-and-pushing",
		"finishing-pull-request",
	} {
		inFence := false
		count := 0
		for i, line := range strings.Split(skillText(t, skill), "\n") {
			if strings.HasPrefix(line, "```") {
				inFence = !inFence
				continue
			}
			if !inFence || !strings.HasPrefix(line, "## ") {
				continue
			}
			count++
			if !hasKindPrefix(line) {
				t.Errorf("%s の %d 行目の見本の題名 %q が、報告・質問・依頼のどれでも始まっていません", skill, i+1, line)
			}
		}
		if count == 0 {
			t.Errorf("%s の見本に `## ` の節が1つもありません。見本が消えたか、囲みの形が変わっています", skill)
		}
	}
}

// 目的: 人間に訊く場面を名指ししたスキルが、`質問: ` の節で訊かせることを固定する。
//
// **名指ししない場面では、エージェントは依頼へ倒れる。**依頼と書いた問いは「AI の問い」に数えられず、
// 人間の答えが古い確認の節への答えとして読まれうる（trusting-instructions の「AI の問い」）。
//
// 与える情報: スキル writing-plan・running-review-loop・trusting-instructions・finishing-run の全文。
// 成功条件: それぞれの文が在ること。
func TestSectionKind_人間に訊く場面が質問の節で訊く(t *testing.T) {
	for _, c := range []struct{ skill, needle, why string }{
		{"writing-plan", "`質問: ` で始まる節で「この計画で設計レビューへ進めてよいか。", "計画の確認が依頼と書かれます"},
		{"running-review-loop", "報告のコメントの `質問: ` で始まる節で質問して", "打ち切りの質問が依頼と書かれます"},
		{"running-review-loop", "人間へ方針を `質問: ` で始まる節で確認します", "10周で止まったときの質問が依頼と書かれます"},
		{"trusting-instructions", "題名は `## 質問: 頼みが要る操作の確認` で、訊くときだけ使い、訳さず、後ろに言葉を足しません", "確認の節の題名が揺れると、後の run が見分けられません"},
		{"trusting-instructions", "題名が「頼みが要る操作の確認」だけの節も、確認の節として数えます", "種類の決まりより前に立てた確認の節への答えが数えられなくなります"},
		{"trusting-instructions", "報告に `## 質問: 頼みが要る操作の確認` の節を立て", "確認を訊く段が古い題名で節を立てます"},
		{"finishing-run", "コメントしてから Status を Ready へ戻す手は `質問: `、それ以外は `依頼: `", "review で終えるときの人間の手の種類が割れます"},
	} {
		if !strings.Contains(skillText(t, c.skill), c.needle) {
			t.Errorf("%s に %q がありません。%s", c.skill, c.needle, c.why)
		}
	}
}

// 目的: 人間が起動する Claude Code 向けの plugin にも、種類を書かせることを固定する（人間の回答「当てる」）。
//
// 与える情報: plugin continuo-issue-comments の SKILL.md。
// 成功条件: 決まりの文と、3つの種類の定義と、言語ごとの語が在ること。
func TestSectionKind_人間向けのpluginにも種類を書かせる(t *testing.T) {
	plugin := readSkill(t)
	for _, want := range []string{
		"also start the title of every `## ` section in those comments and bodies with its kind",
		"- question: ",
		"- request: ",
		"- report: ",
		"`報告: ` / `質問: ` / `依頼: ` in Japanese",
		"`Report: ` / `Question: ` / `Request: ` in English",
		"give it a `## ` section of its own with that kind",
	} {
		if !strings.Contains(plugin, want) {
			t.Errorf("plugin continuo-issue-comments に %q がありません。人間が起動する Claude Code のコメントだけ種類が無いまま残ります", want)
		}
	}
}

func hasKindPrefix(line string) bool {
	for _, k := range sectionKinds {
		if strings.HasPrefix(line, k) {
			return true
		}
	}
	return false
}
