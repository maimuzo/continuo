// **訳語集の doctor のラベル一覧と、実際に画面へ出るラベルが一致していることを機械で確かめる。**
//
// **人が気をつけるだけでは崩れた。**#127（英語版 README と en.json の board を
// kanban board に統一した変更を、マージ後にレビューする）で
// [internal/i18n/messages/en.json](../../../internal/i18n/messages/en.json) の
// `doctor.label.board` を `kanban board` へ直したとき、
// [docs/spec/translation-glossary.md](../../../docs/spec/translation-glossary.md) の
// 一覧は `board` のまま取り残された。**古い一覧は、次に doctor を触る人を間違った語へ導く。**
//
// **訳語集の一覧は「使ってよいラベルはこの15語だけ」と宣言している。**
// 宣言である以上、実際の値と集合として一致していなければ意味を持たない。
// 片方だけを直したら、ここで落ちる。
package testdesign_test

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// enMessagesPath は英語の文言ファイルのリポジトリ内のパスである。
const enMessagesPath = "internal/i18n/messages/en.json"

// doctorLabelPrefix は doctor のラベルのキーの接頭辞である。
const doctorLabelPrefix = "doctor.label."

// glossaryLabelHeading は、訳語集で doctor のラベル一覧が始まる見出しである。
const glossaryLabelHeading = "## 8. doctor のラベルは15桁まで"

// TestDesign_訳語集のdoctorのラベル一覧が実際の値と一致している は、片方だけ直したことを弾く。
//
// 目的: 訳語集が挙げるラベルと、`doctor.label.*` の値が集合として一致すること。
// 与える情報: docs/spec/translation-glossary.md の8節と、en.json の `doctor.label.*`。
// 成功条件: 両方の集合が完全に一致すること。**どちらも空でないこと**も確かめる。
func TestDesign_訳語集のdoctorのラベル一覧が実際の値と一致している(t *testing.T) {
	inMessages := doctorLabelsFromMessages(t)
	inGlossary := doctorLabelsFromGlossary(t)

	if len(inMessages) == 0 {
		t.Fatalf("%s から %s* のラベルを1つも読めませんでした。テストの走査が壊れています。",
			enMessagesPath, doctorLabelPrefix)
	}
	if len(inGlossary) == 0 {
		t.Fatalf("%s の「%s」からラベルを1つも読めませんでした。テストの走査が壊れています。",
			glossaryPath, glossaryLabelHeading)
	}

	for _, label := range inGlossary {
		if contains(inMessages, label) {
			continue
		}
		t.Errorf("%s が挙げる `%s` が、実際のラベルにありません。\n"+
			"  **ラベルを消したり書き換えたりしたら、訳語集の8節も直してください。**",
			glossaryPath, label)
	}
	for _, label := range inMessages {
		if contains(inGlossary, label) {
			continue
		}
		t.Errorf("%s の `%s` が、%s の一覧にありません。\n"+
			"  **ラベルを足したら、訳語集の8節にも足してください。**",
			enMessagesPath, label, glossaryPath)
	}
}

// doctorLabelsFromMessages は en.json から `doctor.label.*` の値を取り出す。
//
// t: テスト。
// 戻り値: ラベルの並び。辞書順にそろえる。
func doctorLabelsFromMessages(t *testing.T) []string {
	t.Helper()

	var messages map[string]string
	if err := json.Unmarshal([]byte(readRepoFile(t, enMessagesPath)), &messages); err != nil {
		t.Fatalf("%s を JSON として読めません: %v", enMessagesPath, err)
	}

	labels := make([]string, 0, 16)
	for key, value := range messages {
		if !strings.HasPrefix(key, doctorLabelPrefix) {
			continue
		}
		labels = append(labels, value)
	}
	sort.Strings(labels)
	return labels
}

// doctorLabelsFromGlossary は訳語集の8節からラベルの一覧を取り出す。
//
// **見出しから次の見出しまでを見る。**その中でバッククォートで囲まれた語だけを拾う。
// 一覧は `config` / `cleanup states` / … の形で複数行に折り返して書かれている。
// **`labelColumn` のようなコードの名前も同じ囲みで出てくるので、
// 一覧の行だけを見る**（区切りの半角空白つきの `/` を含む行）。
//
// t: テスト。
// 戻り値: ラベルの並び。辞書順にそろえる。
func doctorLabelsFromGlossary(t *testing.T) []string {
	t.Helper()

	labels := make([]string, 0, 16)
	inSection := false
	for _, line := range strings.Split(readRepoFile(t, glossaryPath), "\n") {
		if strings.HasPrefix(line, "## ") {
			inSection = line == glossaryLabelHeading
			continue
		}
		if !inSection || !strings.Contains(line, " / ") {
			continue
		}
		labels = append(labels, backquoted(line)...)
	}
	sort.Strings(labels)
	return labels
}

// backquoted は1行の中のバッククォートで囲まれた語を取り出す。
//
// line: 訳語集の1行。
// 戻り値: 囲まれていた語の並び。1つも無ければ空。
func backquoted(line string) []string {
	words := make([]string, 0, 8)
	rest := line
	for {
		open := strings.Index(rest, "`")
		if open < 0 {
			break
		}
		rest = rest[open+1:]
		closing := strings.Index(rest, "`")
		if closing < 0 {
			break
		}
		words = append(words, rest[:closing])
		rest = rest[closing+1:]
	}
	return words
}

// contains は並びの中に語があるかを返す。
//
// list: 探す先の並び。
// want: 探す語。
// 戻り値: 見つかれば true。
func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
