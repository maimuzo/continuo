package prompt_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/prompt"
)

// このファイルは、issue と pull request を URL で指させる決まりを固定する（issue #330。設計 5-3zb）。
//
// **なぜ要るか。**GitHub は本文の `#100` を、その本文が在るリポジトリの100番へ自動でリンクする。
// 複数のリポジトリにまたがる作業で、別のリポジトリの番号を指すつもりで番号だけを書くと、
// 無関係な issue や pull request を指し、人間は違うものを開いてレビューする。

// urlRuleSkillHeading は、スキル writing-comments-for-humans の中で決まりを置く節の見出しである。
const urlRuleSkillHeading = "### issue と pull request は URL で指す"

// urlRuleNeedles は、決まりの2か所（システムプロンプトの「概要」とスキルの節）が両方とも持つ語である。
//
// **2か所に同じ中身を置く。**continuo が起動した Claude Code には「概要」が compaction のあとも届き、
// 人間と直接やりとりする AI はスキルだけを読む。片方だけ直すと、例外が食い違う。
var urlRuleNeedles = []struct {
	needle string
	why    string
}{
	{"必ず URL で書き、その題名を添えます", "決まりの本体が無いと、番号だけで書く書き方が残ります"},
	{"`github.com` と決めつけません", "GitHub Enterprise の利用者で、公開の github.com の同じ名前の別リポジトリを指します"},
	{"`<issue の URL>` と同じです", "continuo が扱う issue と pull request のホストが決まりません"},
	{"`#<番号>` だけでも `<owner>/<repo>#<番号>` でも書きません", "書かない形を名指ししないと、`owner/repo#N` の形が残ります"},
	{"commit メッセージ（1行目を含む）", "人間が「commit メッセージにも当てて」と答えています（issue #330 のコメント）"},
	{"リポジトリの中のファイル（コード・文書）には当てません", "範囲を決めないと、設計文書やコードのコメントの番号まで書き換え始めます"},
	{"- GitHub の閉じるキーワードの行（書き方は「finishing-pull-request」のとおり）", "`Closes` の行を URL にすると、紐づけの判定と CI の検査が頼る形が崩れます"},
	{"- 応答の最後の `CONTINUO-STATUS:` の行（書き方は「finishing-run」のとおり。別のリポジトリの issue は `<owner>/<repo>#<番号>`）",
		"表明の対象は Go の targetPattern が `#<番号>` と `<owner>/<repo>#<番号>` だけで読みます。URL にすると読めません"},
	{"- スキルのスクリプトが書く文と、git と GitHub が自動で作るメッセージ（merge commit の1行目など）", "自分で書かない文まで書き換えようとします"},
	{"- 人間の原文の引用と、コマンドの出力の貼り付け", "原文を引く決まり（8つの見出しの1つ目）とぶつかり、人間の言葉が変わったまま残ります"},
	{"- 「このプロジェクトの決まり」が番号の形を決めている行", "利用者が `Fixes #123` の形を決めていると、決まりどうしが食い違って止まります"},
	{"「公開の場へ書かないもの」が写さないと決めたもの（公開範囲の違うリポジトリへの issue の URL など）は、そちらに従います",
		"private な issue の URL を public な pull request へ写します"},
}

// overviewSection は、システムプロンプトの「# 概要」の中身を返す。
func overviewSection(t *testing.T) string {
	t.Helper()
	body := prompt.BuiltinSystem()
	start := strings.Index(body, "# 概要\n")
	if start < 0 {
		t.Fatalf("システムプロンプトに「# 概要」がありません")
	}
	rest := body[start+len("# 概要\n"):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// urlRuleSkillSection は、スキル writing-comments-for-humans の決まりの節（次の見出しの手前まで）を返す。
func urlRuleSkillSection(t *testing.T) string {
	t.Helper()
	return sectionsMatching(t, skillText(t, prompt.SkillWritingCommentsForHumans), urlRuleSkillHeading, func(line string) bool {
		return strings.HasPrefix(line, "#")
	})
}

// 目的: 決まりと例外が、システムプロンプトの「概要」とスキルの節の両方に同じ文で在ることを固定する（設計 5-3zb）。
//
// 与える情報: prompt.BuiltinSystem() の「# 概要」と、スキル writing-comments-for-humans の節。
// 成功条件: 2か所とも urlRuleNeedles を全部持つこと。
func TestURLReference_概要とスキルの節が同じ決まりと例外を持つ(t *testing.T) {
	places := []struct {
		name string
		body string
	}{
		{"システムプロンプトの「# 概要」", overviewSection(t)},
		{"スキル writing-comments-for-humans の " + urlRuleSkillHeading, urlRuleSkillSection(t)},
	}
	for _, p := range places {
		if strings.TrimSpace(p.body) == "" {
			t.Errorf("%s が空です", p.name)
			continue
		}
		for _, w := range urlRuleNeedles {
			if !strings.Contains(p.body, w.needle) {
				t.Errorf("%s に %q がありません。%s", p.name, w.needle, w.why)
			}
		}
	}
}

// numberRef は、番号だけの参照になりうる当たり（`#<…>` か `#` の直後の数字）である。
var numberRef = regexp.MustCompile(`#(<|[0-9])`)

// closesBefore は、当たりの直前が閉じるキーワードであることを見る（`Closes #<番号>` と `Closes <リポジトリ>#<番号>`）。
var closesBefore = regexp.MustCompile("Closes (<[^>]*>|[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)?$")

// 目的: スキルの見本と手順の文に、番号だけで issue や pull request を指す書き方が残っていないことを固定する（設計 5-3zb）。
//
// **見本は、エージェントがそのまま写す。**システムプロンプトが「URL で書く」と言っても、
// 見本が `#45` と書いていれば見本に従う。
//
// **通すのは、決まりが例外にした形だけである。**当たり1件ごとに、直前が閉じるキーワード（`Closes `）なら通す。
// `CONTINUO-STATUS:` の行は、直前が `CONTINUO-STATUS: ` か `<owner>/<repo>` なら通す。
// ただし行が別のリポジトリを指していると書いていれば、`<owner>/<repo>` の形でなければ落とす
// （Go の resolveTarget は `#<番号>` を代表の issue と同じリポジトリと読む）。
// 決まりそのものの節（urlRuleSkillHeading）は、書かない形を名指しするので見ない。
//
// 与える情報: スキル11本の SKILL.md の全行（囲みの中も）。
// 成功条件: 例外に当たらない当たりが0件であること。
func TestURLReference_スキルの見本に番号だけの参照が無い(t *testing.T) {
	for _, name := range prompt.Skills {
		lines := strings.Split(skillText(t, name), "\n")
		inRule := false
		for i, line := range lines {
			if strings.HasPrefix(line, "#") {
				inRule = line == urlRuleSkillHeading
			}
			if inRule {
				continue
			}
			for _, loc := range numberRef.FindAllStringIndex(line, -1) {
				before := line[:loc[0]]
				if closesBefore.MatchString(before) {
					continue
				}
				if strings.Contains(line, "CONTINUO-STATUS:") {
					otherRepo := strings.Contains(line, "別リポジトリ") || strings.Contains(line, "別のリポジトリ")
					if strings.HasSuffix(before, "<owner>/<repo>") {
						continue
					}
					if strings.HasSuffix(before, "CONTINUO-STATUS: ") && !otherRepo {
						continue
					}
				}
				t.Errorf("スキル %s の %d 行目が、issue か pull request を番号だけで指しています。"+
					"URL と題名で書いてください（別のリポジトリへの表明なら `<owner>/<repo>#<番号>`）:\n  %s",
					name, i+1, line)
			}
		}
	}
}

// 目的: 人間が自分で起動する Claude Code 向けの plugin にも、同じ決まりがあることを固定する（設計 5-3zb）。
//
// **section 3（continuo の印があるリポジトリだけ）に置く。**人間が「足して」と答えたのは section 3 へ足す案で、
// issue #321 でもこの plugin の決まりを印のあるリポジトリに絞った。
// **commit メッセージに印を付けない決まりは section 2（どのリポジトリにも当たる）に置く。**
// description に commit の場面を足したので、印の無いリポジトリでも commit の前にこのスキルが読まれ、
// section 2 の「1行目は印」だけが当たると、commit の1行目に印が入る。
//
// 与える情報: plugin continuo-issue-comments の SKILL.md の全文。
// 成功条件: description・section 2・section 3 がそれぞれの文を持ち、section 3 の段落が
// 「In other repositories, write as you usually do.」より前に在ること。
func TestURLReference_人間向けのpluginにも決まりがある(t *testing.T) {
	body := readSkill(t)
	head, rest, ok := strings.Cut(body, "\n## 2. When you write\n")
	if !ok {
		t.Fatalf("%s に「## 2. When you write」がありません", skillPath)
	}
	sec2, rest, ok := strings.Cut(rest, "\n## 3. ")
	if !ok {
		t.Fatalf("%s に「## 3. 」がありません", skillPath)
	}
	sec3, _, _ := strings.Cut(rest, "\n## 4. ")

	if !strings.Contains(head, "when writing a commit message that mentions an issue or pull request") {
		t.Errorf("description に commit メッセージを書く場面がありません。commit のときにこのスキルが読まれません")
	}
	if !strings.Contains(sec2, "A commit message never carries a marker.") {
		t.Errorf("section 2 に、commit メッセージに印を付けない決まりがありません。commit の1行目に印が入ります")
	}
	for _, want := range []string{
		"point to every issue and pull request by its full URL, even when it is in the same repository",
		"do not assume `github.com`",
		"Do not write `#<number>` or `<owner>/<repo>#<number>`",
		"This applies to comments, bodies, titles and commit messages",
		"closing keyword lines",
		"quotes of what a human wrote and pasted command output",
	} {
		if !strings.Contains(sec3, want) {
			t.Errorf("section 3 に %q がありません", want)
		}
	}
	rule := strings.Index(sec3, "by its full URL")
	other := strings.Index(sec3, "In other repositories, write as you usually do.")
	if rule < 0 || other < 0 || rule > other {
		t.Errorf("section 3 の URL の段落が「In other repositories, write as you usually do.」より前にありません（段落 %d / その行 %d）。"+
			"後ろに置くと、continuo の印が無いリポジトリにも当たります", rule, other)
	}
}
