package prompt_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/agentplugin"
	"github.com/maimuzo/continuo/internal/prompt"
)

// このファイルは、手順の文面を確かめるテストが読む「指示の全体」を組み立てる（issue #288）。
//
// **手順の本文は、組み込みの指示書（builtin.md）から plugin `continuo-agent-procedures` のスキルへ移った。**
// 指示書の決まりを確かめていたテストは、決まりそのものは変えずに、読む先をスキルへ移す。
//
// **読むのは埋め込んだ中身（置き換える前）である。**`CONTINUO_PLUGIN_DIR` などの置き換える語と、
// エージェントが埋める山括弧の語（`<issue のリポジトリ>` など）が、そのまま入っている。

// skillHeadingPrefix は、指示の全体の中でスキルの境目に置く見出しの頭である。
//
// **`# ` で始める。**節を切り出す補助（`sectionOf`・`sectionUntilNextChapter`）が `# ` で止まるので、
// 1つのスキルの最後の節に、次のスキルの先頭が混ざらない。
const skillHeadingPrefix = "# スキル "

// skillText は、埋め込んだスキルの SKILL.md を、置き換える前の中身のまま返す。
//
// t: テストコンテキスト。
// name: スキルの名前。
// 戻り値: SKILL.md の全文。
func skillText(t *testing.T, name string) string {
	t.Helper()
	data, ok := agentplugin.RawFile("skills/" + name + "/SKILL.md")
	if !ok {
		t.Fatalf("スキル %s の SKILL.md が埋め込みにありません", name)
	}
	return string(data)
}

// skillsText は、スキル11本を対応表の順に、境目の見出しを挟んで連結する。
//
// t: テストコンテキスト。
// 戻り値: 連結した全文。
func skillsText(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, name := range prompt.Skills {
		b.WriteString("\n" + skillHeadingPrefix + name + "\n\n")
		b.WriteString(skillText(t, name))
	}
	return b.String()
}

// instructionsText は、エージェントに届く指示の全体を返す。
//
// **組み込みの2つの文面（システムプロンプトと最初のプロンプト）と、スキル11本の全文である。**
// もとの指示書（builtin.md）の1枚に当たる。
//
// t: テストコンテキスト。
// 戻り値: 連結した全文。
func instructionsText(t *testing.T) string {
	t.Helper()
	return prompt.Builtin() + skillsText(t)
}

// scriptText は、埋め込んだスキルの scripts/ の下のファイルを、置き換える前の中身のまま返す。
//
// **長いシェルの見本は、スキルの scripts/ へ移った**（issue #288）。見本を確かめていたテストは、
// スキルの本文（叩き方）とスクリプト（中身）の両方を読む。
//
// t: テストコンテキスト。
// skill: スキルの名前。
// name: スクリプトのファイル名。
// 戻り値: スクリプトの全文。
func scriptText(t *testing.T, skill, name string) string {
	t.Helper()
	data, ok := agentplugin.RawFile("skills/" + skill + "/scripts/" + name)
	if !ok {
		t.Fatalf("スキル %s のスクリプト %s が埋め込みにありません", skill, name)
	}
	return string(data)
}

// scriptsText は、埋め込んだスキルの scripts/ の下のファイルを全部連結して返す。
//
// t: テストコンテキスト。
// 戻り値: 連結した全文。
func scriptsText(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, rel := range agentplugin.RawFiles() {
		if !strings.Contains(rel, "/scripts/") {
			continue
		}
		data, _ := agentplugin.RawFile(rel)
		b.WriteString("\n" + string(data))
	}
	return b.String()
}
