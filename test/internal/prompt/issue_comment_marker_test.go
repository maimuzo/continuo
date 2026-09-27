package prompt_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/prompt"
)

// skillPath は、人間が自分で起動した Claude Code へ配るスキルの置き場所である
// （リポジトリの直下からのパス。設計 3-82c）。
const skillPath = "plugins/continuo-issue-comments/skills/marking-and-trusting-issue-comments/SKILL.md"

// markerCommand は、コメントや本文を読むコマンドの1本である。
//
// builtin: continuo専用プロンプトでのコマンドの書き出し（テンプレートの変数のまま）。
// skill: スキルでのコマンドの書き出し（置き換え語のまま）。
type markerCommand struct {
	name    string
	builtin string
	skill   string
}

// markerCommands は、`written_by` と `trusted_comment`（または `trusted_body`）を足して読むコマンドの全部である
// （設計 3-82b）。**continuo専用プロンプトとスキルで、`--jq` の式が1文字も違ってはならない。**
var markerCommands = []markerCommand{
	{
		name:    "issue のコメント",
		builtin: "    gh issue view {{.issue.number}} --repo {{.issue.owner}}/{{.issue.repo}} --json comments --jq '",
		skill:   "    gh issue view <number> --repo <owner>/<repo> --json comments --jq '",
	},
	{
		name:    "issue の本文",
		builtin: "    gh api repos/{{.issue.owner}}/{{.issue.repo}}/issues/{{.issue.number}} --jq '",
		skill:   "    gh api repos/<owner>/<repo>/issues/<number> --jq '",
	},
	{
		name:    "pull request の会話のコメント",
		builtin: "    gh pr view <PR番号> --repo {{.issue.owner}}/{{.issue.repo}} --json comments --jq '",
		skill:   "    gh pr view <number> --repo <owner>/<repo> --json comments --jq '",
	},
	{
		name:    "pull request の行に紐づくレビューコメント",
		builtin: "    gh api repos/{{.issue.owner}}/{{.issue.repo}}/pulls/<PR番号>/comments --paginate --jq '",
		skill:   "    gh api repos/<owner>/<repo>/pulls/<number>/comments --paginate --jq '",
	},
	{
		name:    "pull request のレビュー",
		builtin: "    gh api repos/{{.issue.owner}}/{{.issue.repo}}/pulls/<PR番号>/reviews --paginate --jq '",
		skill:   "    gh api repos/<owner>/<repo>/pulls/<number>/reviews --paginate --jq '",
	},
}

// jqAfter は、本文の中で prefix で始まる行を1本だけ探し、`--jq '` の後ろから行末の `'` の手前までを返す。
//
// t: テスト。
// label: 失敗したときに出す、どの文書かの名前。
// body: 探す文書の全文。
// prefix: 行の書き出し（`--jq '` まで含む）。
// 戻り値: `--jq` に渡す式。見つからない・2本以上ある・行末が `'` でないときはテストを失敗させる。
func jqAfter(t *testing.T, label, body, prefix string) string {
	t.Helper()

	var found []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, prefix) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s に %q で始まる行が %d 本ある（1本のはず）", label, prefix, len(found))
	}
	rest := strings.TrimPrefix(found[0], prefix)
	if !strings.HasSuffix(rest, "'") {
		t.Fatalf("%s の %q の行が `'` で終わっていない:\n  %s", label, prefix, found[0])
	}
	return strings.TrimSuffix(rest, "'")
}

// readSkill はスキルの SKILL.md を読む。
func readSkill(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("..", "..", "..", skillPath))
	if err != nil {
		t.Fatalf("%s を読めません: %v", skillPath, err)
	}
	return string(body)
}

// 目的: continuo専用プロンプトとスキルが、同じ jq の式でコメントを読むことを固定する（issue #245。設計 3-82b・3-82c）。
//
// **なぜ要るか。**同じ式を5本ずつ、2つのファイルへ写している。
// 片方だけ直すと、continuo が起動した Claude Code と人間が起動した Claude Code で、
// 同じコメントの `trusted_comment` が食い違う。**AI の書き込みが、片方でだけ命令として読まれる。**
//
// 与える情報: prompt.BuiltinRaw() と SKILL.md の全文。
// 成功条件: 5本それぞれで、`--jq` の式が1文字も違わないこと。
func TestTemplate_スキルとcontinuo専用プロンプトが同じ式で読む(t *testing.T) {
	builtin := prompt.BuiltinRaw()
	skill := readSkill(t)

	for _, c := range markerCommands {
		want := jqAfter(t, "continuo専用プロンプト", builtin, c.builtin)
		got := jqAfter(t, "スキル", skill, c.skill)
		if want != got {
			t.Errorf("%s を読む式が、continuo専用プロンプトとスキルで違う\n  continuo専用プロンプト: %s\n  スキル: %s",
				c.name, want, got)
		}
		if c.name == "issue の本文" {
			if !strings.Contains(want, "trusted_body:") || !strings.Contains(want, "(.pull_request == null)") {
				t.Errorf("issue の本文を読む式に trusted_body と pull request を弾く条件が無い: %s", want)
			}
			continue
		}
		for _, key := range []string{"written_by:", "trusted_comment:"} {
			if !strings.Contains(want, key) {
				t.Errorf("%s を読む式に %s が無い: %s", c.name, key, want)
			}
		}
	}
}

// markerTestPattern は、jq の式の中の `test("…")` に渡した文字列を取り出す。
var markerTestPattern = regexp.MustCompile(`test\("((?:[^"\\]|\\.)*)"\)`)

// 目的: AI の書き込みと判定する正規表現が、どの式でも同じで、設計 3-82 の表どおりに当たることを固定する（issue #245）。
//
// **なぜ要るか。**jq の文字列の中の正規表現は、シェルの一重引用符・jq の文字列のエスケープ・正規表現の3つをくぐる。
// 1文字崩れると、全件が `written_by: "human"` になり、AI の書き込みが黙って命令として読まれる。
// **gh の `--jq` は gojq で、正規表現は Go の regexp で解く**ので、ここでも Go の regexp で確かめる。
//
// 与える情報: continuo専用プロンプトの、本文を読む1本を除く4本の式から取り出した正規表現。
// 成功条件: 取り出した正規表現が全部同じで、目印・印・印の無い本文・空の本文を表どおりに判定すること。
func TestTemplate_AIの書き込みと判定する正規表現が表どおりに当たる(t *testing.T) {
	builtin := prompt.BuiltinRaw()

	var patterns []string
	for _, c := range markerCommands {
		if c.name == "issue の本文" {
			continue
		}
		expr := jqAfter(t, "continuo専用プロンプト", builtin, c.builtin)
		for _, m := range markerTestPattern.FindAllStringSubmatch(expr, -1) {
			patterns = append(patterns, m[1])
		}
	}
	// issue のコメント・pull request の会話のコメントに1つずつ、REST の2本に2つずつ。
	if len(patterns) != 6 {
		t.Fatalf("正規表現が %d 個しか見つからない（6個のはず）: %q", len(patterns), patterns)
	}
	for _, p := range patterns[1:] {
		if p != patterns[0] {
			t.Fatalf("式によって正規表現が違う\n  %s\n  %s", patterns[0], p)
		}
	}

	// jq の文字列のエスケープ（\\ と \t \r \n）は Go の文字列と同じなので、strconv.Unquote で戻せる。
	raw, err := strconv.Unquote(`"` + patterns[0] + `"`)
	if err != nil {
		t.Fatalf("正規表現を jq の文字列として読めない: %v\n  %s", err, patterns[0])
	}
	re, err := regexp.Compile(raw)
	if err != nil {
		t.Fatalf("正規表現を組めない: %v\n  %s", err, raw)
	}

	cases := []struct {
		body string
		ai   bool
	}{
		{"<!-- continuo:ai -->\n調べた結果です", true},
		{"  \n\t<!-- continuo:ai -->\n字下げと空行のあと", true},
		{"<!-- continuo:agent -->\n<!-- continuo:plan -->\n# 計画", true},
		{"<!-- continuo:self -->\nStatus を動かしました", true},
		{"<!-- continuo:group -->\n一緒に見ました", true},
		{"<!-- code-review-result -->\n<!-- continuo:agent -->\n## 判断票", true},
		{"<!-- design-review-result -->\n## 判断票", true},
		{"<!-- design-review-skipped -->\n文書だけの変更", true},
		{"A 案で進めて", false},
		{"", false},
		{"本文の途中の <!-- continuo:ai --> は印ではない", false},
		{"<!-- design-review-result --x", false},
		{"<!-- 人間が書いたメモ -->\n本文", false},
	}
	for _, tc := range cases {
		if got := re.MatchString(tc.body); got != tc.ai {
			t.Errorf("本文 %q を AI の書き込みと判定したかが %v（%v のはず）", tc.body, got, tc.ai)
		}
	}
}
