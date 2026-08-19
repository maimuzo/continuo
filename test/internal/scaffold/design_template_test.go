package scaffold_test

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// designDocPath は設計文書の場所である。Go のテストはパッケージのディレクトリを
// 作業ディレクトリとして走るので、test/internal/scaffold からの相対パスで指す。
const designDocPath = "../../../docs/plans/continuo_design.md"

// designSectionHeading は front matter の設定例が載っている節の見出しである。
const designSectionHeading = "### 5-2. front matter（設定）"

// designBodySectionHeading は本文（プロンプトのテンプレート）が載っている節の見出しである。
const designBodySectionHeading = "### 5-3. 本文（プロンプトのテンプレート）"

// 目的: 雛形の front matter のキー構成が、設計 5-2 の設定例のキー構成と一致することを確認する。
//
// config.Load を通すだけでは、雛形からキーや節を丸ごと削っても DefaultConfig() が
// 既定値で埋めてしまうため素通りする（cleanup: を全部消しても起動が通ることを実測した）。
// そのため「値」ではなく「キーの集合」を設計文書と直接突き合わせる。
// 値は雛形ではプレースホルダになっているので比較しない。
//
// 与える情報: 設計文書 5-2 の ```yaml ブロックと、scaffold.Template() の front matter。
// 成功条件: 入れ子を "." でつないだキーのパスの集合が、両者で完全に一致すること。
func TestTemplate_雛形のキー構成が設計5_2の設定例と一致する(t *testing.T) {
	wantKeys := flattenYAMLKeys(t, "設計 5-2 の設定例", frontMatterOf(t, "設計 5-2 の設定例", readDesignFrontMatterExample(t)))
	gotKeys := flattenYAMLKeys(t, "雛形", frontMatterOf(t, "雛形", scaffold.Template()))

	assertSameKeySet(t, wantKeys, gotKeys)
}

// 目的: 雛形の本文が、設計 5-3 の本文のブロックと一字一句そのまま一致することを確認する。
//
// 雛形は設計 5-3 を機械的に写したものであり、本文にはプレースホルダの差し替えが無い
// （差し替えるのは front matter の値だけである。設計 3-32）。したがって完全一致を求めてよい。
// 「{{.issue.identifier}} が入っているか」のような、テスト側に書いた文字列との照合では、
// 本文が設計から離れても落ちない。設計文書そのものを読んで突き合わせる。
//
// 与える情報: 設計文書 5-3 の ```markdown ブロックと、scaffold.Template() の本文。
// 成功条件: 前後の空行を除いた両者が完全に一致すること。
func TestTemplate_雛形の本文が設計5_3の本文と一致する(t *testing.T) {
	assertSameBody(t, "雛形", bodyOf(t, "雛形", scaffold.Template()))
}

// assertSameBody は、渡した WORKFLOW.md の本文が設計 5-3 の本文と一致することを確かめる。
//
// 一致しない場合は、最初に食い違った行の番号と、その行の設計側・対象側の中身を出す。
// 全文を並べても、どこがずれたのかが読み取れないためである。
//
// t: テストコンテキスト。
// label: 失敗メッセージに出す、検査対象の呼び名。
// body: 突き合わせる側の本文（front matter より後ろ）。
func assertSameBody(t *testing.T, label, body string) {
	t.Helper()

	// 前後の空行だけを落としてから比べる。front matter の終端行の直後の空行や、
	// 末尾の改行の有無は、プロンプトとしての中身の違いではない。
	want := strings.Split(strings.Trim(readDesignBodyExample(t), "\n"), "\n")
	got := strings.Split(strings.Trim(body, "\n"), "\n")

	for i := 0; i < len(want) || i < len(got); i++ {
		w, g := lineAt(want, i), lineAt(got, i)
		if w == g {
			continue
		}
		t.Errorf("%s: 本文が設計 5-3 と食い違っている（設計が正である。雛形の側を設計に合わせること）\n"+
			"  本文の %d 行目\n  設計 5-3: %q\n  %s: %q\n  行数: 設計 5-3=%d / %s=%d",
			label, i+1, w, label, g, len(want), label, len(got))
		return
	}
}

// lineAt は行の一覧から i 行目を取り出す。範囲外なら「その行が無い」ことを示す印を返す。
//
// lines: 行の一覧。
// i: 0 起点の行番号。
// 戻り値: i 行目の中身。範囲外なら "(行が無い)"。
func lineAt(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return "(行が無い)"
}

// assertSameKeySet は2つのキーのパスの集合が一致することを確かめ、
// 食い違いを「雛形に無いキー」「雛形にしか無いキー」に分けて報告する。
//
// t: テストコンテキスト。
// wantKeys: 設計 5-2 の設定例から取ったキーのパス。
// gotKeys: 突き合わせる側（雛形や、書き出したファイル）から取ったキーのパス。
func assertSameKeySet(t *testing.T, wantKeys, gotKeys []string) {
	t.Helper()

	var missing, extra []string
	for _, k := range wantKeys {
		if !slices.Contains(gotKeys, k) {
			missing = append(missing, k)
		}
	}
	for _, k := range gotKeys {
		if !slices.Contains(wantKeys, k) {
			extra = append(extra, k)
		}
	}

	if len(missing) > 0 {
		t.Errorf("設計 5-2 の設定例にあるキーが雛形に無い（雛形から消したなら設計も直すこと）:\n  %s",
			strings.Join(missing, "\n  "))
	}
	if len(extra) > 0 {
		t.Errorf("雛形にしか無いキーがある（雛形に足したなら設計 5-2 にも足すこと）:\n  %s",
			strings.Join(extra, "\n  "))
	}
}

// assertTemplateFollowsDesign は、渡した WORKFLOW.md の中身が設計どおりであることを確かめる。
//
// 「scaffold.Template() と同じか」を見ると、雛形をどう壊しても通ってしまう（同語反復になる）。
// そのため中身そのものではなく、設計 5-2（front matter のキー構成）と
// 設計 5-3（本文のブロックそのもの）に照らして検査する。あわせて、本文が
// front matter で宣言した印をそのまま使っていることも見る。
//
// t: テストコンテキスト。
// label: 失敗メッセージに出す、検査対象の呼び名。
// raw: WORKFLOW.md の全文（front matter と本文）。
func assertTemplateFollowsDesign(t *testing.T, label, raw string) {
	t.Helper()

	front := frontMatterOf(t, label, raw)
	body := bodyOf(t, label, raw)

	// 設計 5-2: キー構成が設計文書の設定例と一致すること。
	wantKeys := flattenYAMLKeys(t, "設計 5-2 の設定例", frontMatterOf(t, "設計 5-2 の設定例", readDesignFrontMatterExample(t)))
	assertSameKeySet(t, wantKeys, flattenYAMLKeys(t, label, front))

	// 設計 5-3: 本文は front matter が宣言した印をそのまま使うこと。
	// 片方だけ書き換えると、continuo が探す印とエージェントに書かせる印がずれる。
	values := flattenYAMLValues(t, label, front)
	for _, key := range []string{
		"tracker.status_signal_prefix",
		"tracker.provider.comments.marker",
	} {
		v, ok := values[key]
		if !ok || v == "" {
			t.Errorf("%s: front matter の %s が空である", label, key)
			continue
		}
		if !strings.Contains(body, v) {
			t.Errorf("%s: 本文が front matter の %s（%q）を使っていない。continuo が探す印とエージェントに書かせる印がずれる",
				label, key, v)
		}
	}

	// 設計 5-3: 本文は設計文書の本文のブロックと一致すること。
	// テスト側に書いた文字列（{{.issue.identifier}} など）との照合では、
	// 本文が設計から離れても落ちないので、設計文書そのものと突き合わせる。
	assertSameBody(t, label, body)
}

// readDesignFrontMatterExample は設計文書 5-2 の設定例（```yaml で囲まれたブロック）を
// そのまま取り出す。testdata へ写しを置くのではなく設計文書そのものを読むことで、
// 設計と雛形がずれた瞬間にこのテストが落ちるようにする。
//
// t: テストコンテキスト。
// 戻り値: ```yaml と ``` に挟まれた中身の文字列（前後の区切り行 "---" を含む。
// つまり WORKFLOW.md の front matter 部分そのもの）。
// 見出しが見つからない、または直後に ```yaml のブロックが無い場合はテストを失敗させる。
func readDesignFrontMatterExample(t *testing.T) string {
	t.Helper()
	return readDesignCodeBlock(t, designSectionHeading, "```yaml")
}

// readDesignBodyExample は設計文書 5-3 の本文（```markdown で囲まれたブロック）を
// そのまま取り出す。testdata へ写しを置くのではなく設計文書そのものを読むことで、
// 設計と雛形がずれた瞬間にテストが落ちるようにする。
//
// t: テストコンテキスト。
// 戻り値: ```markdown と ``` に挟まれた中身の文字列（WORKFLOW.md の本文そのもの）。
func readDesignBodyExample(t *testing.T) string {
	t.Helper()
	return readDesignCodeBlock(t, designBodySectionHeading, "```markdown")
}

// readDesignCodeBlock は設計文書から、指定した見出しの直後にある最初のコードブロックを取り出す。
//
// t: テストコンテキスト。
// heading: 探す見出しの行そのもの（例 "### 5-2. front matter（設定）"）。
// fence: コードブロックの開始行そのもの（例 "```yaml"）。
// 戻り値: 開始行と閉じ行に挟まれた中身の文字列（末尾に改行を1つ付ける）。
// 見出しが見つからない、直後に指定のブロックが無い、ブロックが閉じられていない場合はテストを失敗させる。
func readDesignCodeBlock(t *testing.T, heading, fence string) string {
	t.Helper()

	raw, err := os.ReadFile(designDocPath)
	if err != nil {
		t.Fatalf("設計文書を読み込めません（%s）: %v", designDocPath, err)
	}
	lines := strings.Split(string(raw), "\n")

	headingIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == heading {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		t.Fatalf("設計文書に見出し %q が見つかりません。見出しを変えたならこのテストも直すこと", heading)
	}

	fenceStart := -1
	for i := headingIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "### ") {
			break
		}
		if strings.TrimSpace(lines[i]) == fence {
			fenceStart = i
			break
		}
	}
	if fenceStart < 0 {
		t.Fatalf("見出し %q の直後に %s のブロックが見つかりません", heading, fence)
	}

	fenceEnd := -1
	for i := fenceStart + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "```" {
			fenceEnd = i
			break
		}
	}
	if fenceEnd < 0 {
		t.Fatalf("見出し %q の %s のブロックが閉じられていません", heading, fence)
	}

	return strings.Join(lines[fenceStart+1:fenceEnd], "\n") + "\n"
}

// splitWorkflow は WORKFLOW.md の全文を front matter と本文に分ける。
// internal/config の splitFrontMatter は非公開なので、テストからは同じ規則
// （1行目が "---"、次に現れる "---" までが front matter）で切り出す。
//
// t: テストコンテキスト。
// label: 失敗メッセージに出す、検査対象の呼び名。
// raw: WORKFLOW.md の全文。
// 戻り値: front matter の YAML 本体（区切り行を含まない）と、その下に続く本文。
func splitWorkflow(t *testing.T, label, raw string) (string, string) {
	t.Helper()

	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		t.Fatalf("%s: 1行目が front matter の開始行 \"---\" ではありません", label)
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") == "---" {
			return strings.Join(lines[1:i], "\n") + "\n", strings.Join(lines[i+1:], "\n")
		}
	}
	t.Fatalf("%s: front matter の終端行 \"---\" が見つかりません", label)
	return "", ""
}

// frontMatterOf は WORKFLOW.md の全文から front matter の YAML 本体だけを取り出す。
//
// t: テストコンテキスト。
// label: 失敗メッセージに出す、検査対象の呼び名。
// raw: WORKFLOW.md の全文。
// 戻り値: 区切り行 "---" を含まない YAML 本体。
func frontMatterOf(t *testing.T, label, raw string) string {
	t.Helper()
	front, _ := splitWorkflow(t, label, raw)
	return front
}

// bodyOf は WORKFLOW.md の全文から front matter の下に続く本文だけを取り出す。
//
// t: テストコンテキスト。
// label: 失敗メッセージに出す、検査対象の呼び名。
// raw: WORKFLOW.md の全文。
// 戻り値: 本文（プロンプトのテンプレート文字列）。
func bodyOf(t *testing.T, label, raw string) string {
	t.Helper()
	_, body := splitWorkflow(t, label, raw)
	return body
}

// flattenYAMLKeys は YAML のマッピングを、入れ子を "." でつないだキーのパスの一覧に直す。
// 並びは辞書順にそろえる。値の型（スカラー・リスト）は見ない。リストの中は辿らない。
//
// t: テストコンテキスト。
// label: 失敗メッセージに出す、検査対象の呼び名。
// front: front matter の YAML 本体。
// 戻り値: 例 ["agent.max_turns", "tracker.provider.owner", ...]。
func flattenYAMLKeys(t *testing.T, label, front string) []string {
	t.Helper()

	values := flattenYAMLValues(t, label, front)
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// flattenYAMLValues は YAML のマッピングを「入れ子を "." でつないだキーのパス → 値の文字列表現」に直す。
// 値の比較には使わない（雛形の値はプレースホルダなので設計とは違う）。
// 本文が front matter の宣言どおりの印を使っているかを確かめるために、
// 特定のキーの値だけを引くのに使う。
//
// t: テストコンテキスト。
// label: 失敗メッセージに出す、検査対象の呼び名。
// front: front matter の YAML 本体。
// 戻り値: キーのパスから値の文字列表現への対応。リストと空のマッピングは葉として扱う。
func flattenYAMLValues(t *testing.T, label, front string) map[string]string {
	t.Helper()

	var root map[string]any
	if err := yaml.Unmarshal([]byte(front), &root); err != nil {
		t.Fatalf("%s: front matter を YAML として解釈できません: %v", label, err)
	}

	out := make(map[string]string)
	var walk func(prefix string, node map[string]any)
	walk = func(prefix string, node map[string]any) {
		for k, v := range node {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			child, ok := asMapping(v)
			if ok && len(child) > 0 {
				walk(path, child)
				continue
			}
			// スカラー・リスト・空のマッピングは葉として1件数える。
			out[path] = scalarString(v)
		}
	}
	walk("", root)
	return out
}

// asMapping は YAML から読んだ値がマッピングであれば map[string]any に直して返す。
// goccy/go-yaml は map[string]any で返すが、キーの型が違う形で返っても取りこぼさないようにする。
//
// v: YAML から読んだ値。
// 戻り値: マッピングであればその中身と true、そうでなければ nil と false。
func asMapping(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			out[fmt.Sprint(k)] = val
		}
		return out, true
	default:
		return nil, false
	}
}

// scalarString は葉の値を、比較に使える文字列に直す。
//
// v: 葉の値（スカラー・リスト・空のマッピング）。
// 戻り値: 文字列ならそのまま、null なら空文字、それ以外は %v での表現。
func scalarString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
