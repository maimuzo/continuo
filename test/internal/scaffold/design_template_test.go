package scaffold_test

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/prompt"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// designDocPath は設計文書の場所である。Go のテストはパッケージのディレクトリを
// 作業ディレクトリとして走るので、test/internal/scaffold からの相対パスで指す。
const designDocPath = "../../../docs/plans/continuo_design.md"

// designSectionHeading は front matter の設定例が載っている節の見出しである。
const designSectionHeading = "### 5-2. front matter（設定）"

// designBodySectionHeading は組み込みのプロンプトが載っている節の見出しである。
const designBodySectionHeading = "### 5-3. 組み込みのプロンプト"

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

// 目的: 雛形の `cleanup.on_states` が `tracker.terminal_states` に収まっていることを固定する
// （設計 3-9。issue #35）。
//
// **値は既に揃っている。**守られていなかったのは「揃っていること」のほうで、
// どちらか片方だけを直しても、この2つが噛み合わなくなったことに誰も気づかなかった。
// **噛み合っていない雛形を配ると、利用者は「終わっていない」と判定された直後に
// worktree を片付けられる。**
//
// **判定は書き直さない。**起動時の警告と `continuo doctor` が呼ぶ
// `config.CleanupStatesOutsideTerminal` をそのまま使う。
//
// 与える情報: `scaffold.Template()` の front matter。
// 成功条件: `cleanup.on_states` に `tracker.terminal_states` の外の値が1つも無いこと。
func TestTemplate_雛形の片付ける状態が終わったとみなす状態に収まっている(t *testing.T) {
	var parsed struct {
		Tracker struct {
			TerminalStates []string `yaml:"terminal_states"`
		} `yaml:"tracker"`
		Cleanup struct {
			Enabled  bool     `yaml:"enabled"`
			OnStates []string `yaml:"on_states"`
		} `yaml:"cleanup"`
	}
	front := frontMatterOf(t, "雛形", scaffold.Template())
	if err := yaml.Unmarshal([]byte(front), &parsed); err != nil {
		t.Fatalf("雛形の front matter を読めません: %v", err)
	}
	// **空のまま通してはならない。**キー名を書き間違えると両方とも空になり、
	// 「食い違いは無い」で素通りする。
	if len(parsed.Tracker.TerminalStates) == 0 {
		t.Fatal("雛形から tracker.terminal_states を取れません")
	}
	if len(parsed.Cleanup.OnStates) == 0 {
		t.Fatal("雛形から cleanup.on_states を取れません")
	}

	cfg := *config.DefaultConfig()
	cfg.Cleanup.Enabled = parsed.Cleanup.Enabled
	cfg.Tracker.TerminalStates = parsed.Tracker.TerminalStates
	cfg.Cleanup.OnStates = parsed.Cleanup.OnStates

	if outside := config.CleanupStatesOutsideTerminal(cfg); len(outside) != 0 {
		t.Fatalf("雛形の cleanup.on_states に tracker.terminal_states の外の値がある: %v"+
			"（terminal_states=%v / on_states=%v）",
			outside, parsed.Tracker.TerminalStates, parsed.Cleanup.OnStates)
	}
}

// 目的: 組み込みのプロンプトが、設計 5-3 のブロックと一字一句そのまま一致することを確認する。
//
// **突き合わせる相手は WORKFLOW.md の本文ではない**（設計 5-3c で本文は空になった）。
// **送る文面は internal/prompt/builtin.md にある。**目印の行も含めて丸ごと比べる。
// 「{{.issue.identifier}} が入っているか」のような、テスト側に書いた文字列との照合では、
// 送る文面が設計から離れても落ちない。設計文書そのものを読んで突き合わせる。
//
// 与える情報: 設計文書 5-3 の ```markdown ブロックと、prompt.BuiltinRaw()。
// 成功条件: 前後の空行を除いた両者が完全に一致すること。
func TestTemplate_組み込みのプロンプトが設計5_3と一致する(t *testing.T) {
	assertSameBody(t, "internal/prompt/builtin.md", prompt.BuiltinRaw())
}

// 目的: 雛形の WORKFLOW.md が、固有の指示の見本を本文に持つことを固定する（設計 5-3d）。
//
// **本文は、その project でだけ効く指示を書く場所である。**
// 空で配ると、**利用者はどこに何を書けばよいのかを、文書を読むまで知る手立てが無い。**
//
// **仕組みの説明をここへ書き写してはならない。**書き写すと、continuo が説明を直しても
// 既に配った WORKFLOW.md には届かない。**仕組みの説明は internal/prompt/builtin.md にある。**
//
// 与える情報: scaffold.Template() の全文。
// 成功条件: front matter より後ろに、雛形の節が全部あること。
func TestTemplate_雛形の本文に固有の指示の見本がある(t *testing.T) {
	body := bodyOf(t, "雛形", scaffold.Template())
	if strings.TrimSpace(body) == "" {
		t.Fatal("雛形の WORKFLOW.md に本文がありません。" +
			"利用者が固有の指示を書く場所は、雛形の本文です（設計 5-3d）")
	}
	// **設計 5-3d の表に並べた節である。**表の「レビューの手順」の行は、
	// 実装の前と PR のあとの2つの節に分かれている。**消すなら設計の表も同時に直すこと。**
	// **並びも見る。**設計 5-3d は「節の並びは、作業の順番に合わせる」と決めている。
	// **`## 書く言語` を後ろに置くと、下の節が書かせるコメントの言語が揃わない。**
	want := []string{
		"## 何をする作業か",
		"## 書く言語",
		"## このリポジトリの決まり",
		"## テストの走らせ方",
		"## まとめて直してよい範囲",
		"## 実装を始める前に、計画をレビューしてください",
		"## PR の決まり",
		"## PR を作ったら、レビューしてください",
	}
	at := make([]int, 0, len(want))
	for _, w := range want {
		i := strings.Index(body, "\n"+w+"\n")
		if i < 0 {
			t.Errorf("雛形の本文に %q がありません（設計 5-3d の表と食い違っています）", w)
			continue
		}
		at = append(at, i)
	}
	if len(at) != len(want) {
		return
	}
	for i := 1; i < len(at); i++ {
		if at[i-1] < at[i] {
			continue
		}
		t.Errorf("雛形の本文で %q が %q より後ろにあります（設計 5-3d の表の順に並べること）",
			want[i-1], want[i])
	}
}

// 目的: 雛形の本文に、仕組みの説明を書き写していないことを固定する（設計 5-3d）。
//
// **書き写すと、continuo が説明を直しても、既に配った WORKFLOW.md には二度と届かない。**
// **しかも同じ指示が2回届く**（組み込みの側にも同じ節がある）。
//
// **見出しをテストに書き並べない。**書き並べると、組み込みに節が増えたときに
// その節だけが見張られないまま残る。**組み込みの全文から見出しを取り出して、全件を見る。**
//
// 与える情報: scaffold.Template() の本文と、prompt.BuiltinRaw() から取った見出しの全部。
// 成功条件: 組み込みの見出しが、本文に1つも入っていないこと。
func TestTemplate_雛形の本文に組み込みの説明を書き写していない(t *testing.T) {
	body := bodyOf(t, "雛形", scaffold.Template())
	headings := headingsOf(prompt.BuiltinRaw())
	if len(headings) == 0 {
		t.Fatal("組み込みのプロンプトに見出しが1つもありません（検査が素通りします）")
	}
	for _, notWant := range headings {
		if strings.Contains(body, notWant) {
			t.Errorf("雛形の本文に組み込みの節 %q が書き写されています。"+
				"直しても配った WORKFLOW.md には届かず、同じ指示が2回届きます", notWant)
		}
	}
}

// headingsOf は markdown の文面から `## ` で始まる見出しの行を取り出す。
//
// **行頭のものだけを取る。**字下げした行は、組み込みが例として引用しているコードブロックの
// 中身であり、節の見出しではない。
//
// text: 取り出す元の文面。
// 戻り値: 見出しの行（`## ` を含む。前後の空白は落としてある）。
func headingsOf(text string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "## ") {
			out = append(out, strings.TrimRight(line, " \t"))
		}
	}
	return out
}

// assertSameBody は、渡した文面が設計 5-3 のブロックと一致することを確かめる。
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

	// 設計 5-2: キー構成が設計文書の設定例と一致すること。
	wantKeys := flattenYAMLKeys(t, "設計 5-2 の設定例", frontMatterOf(t, "設計 5-2 の設定例", readDesignFrontMatterExample(t)))
	assertSameKeySet(t, wantKeys, flattenYAMLKeys(t, label, front))

	// 設計 5-3: 送る文面は front matter が宣言した印をそのまま使うこと。
	// 片方だけ書き換えると、continuo が探す印とエージェントに書かせる印がずれる。
	// **見る先は WORKFLOW.md の本文ではない**（設計 5-3c で本文は空になった）。
	// **組み込みのプロンプトを見る。**
	builtin := prompt.Builtin()
	values := flattenYAMLValues(t, label, front)
	for _, key := range []string{
		"tracker.status_signal_prefix",
		"tracker.comments.marker",
	} {
		v, ok := values[key]
		if !ok || v == "" {
			t.Errorf("%s: front matter の %s が空である", label, key)
			continue
		}
		if !strings.Contains(builtin, v) {
			t.Errorf("%s: 組み込みのプロンプトが front matter の %s（%q）を使っていない。"+
				"continuo が探す印とエージェントに書かせる印がずれる", label, key, v)
		}
	}

	// 設計 5-3d: 本文には固有の指示の見本が入っていること。
	if body := bodyOf(t, label, raw); strings.TrimSpace(body) == "" {
		t.Errorf("%s: 本文が消えている。利用者が固有の指示を書く場所は本文である（設計 5-3d）", label)
	}
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
// 戻り値: 例 ["agent.max_dispatch_turns", "tracker.provider.owner", ...]。
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
