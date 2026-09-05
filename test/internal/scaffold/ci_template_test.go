package scaffold_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// 目的: continuo-ci.yaml の雛形が YAML として読めることを確かめる（設計 5-3o）。
//
// **これを見ないと、壊れた YAML がそのまま配られる。**
// 雛形は Go の文字列定数なので、**中身が壊れていても Go のビルドは通り、
// 他のテストも通る。**
//
// **そして GitHub Actions は、読めない workflow を「検査が無い」として扱う。**
// pull request の画面では「まだ走っていない」と見分けが付かない。
// **検査が無いのにマージできる状態が、緑の顔で成立する。**
//
// 与える情報: scaffold.CITemplate() の全文。
// 成功条件: YAML として解釈でき、name と on と jobs が在ること。
func TestCITemplate_YAMLとして読める(t *testing.T) {
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(scaffold.CITemplate()), &parsed); err != nil {
		t.Fatalf("continuo-ci.yaml の雛形を YAML として解釈できません: %v", err)
	}

	// **`on` は YAML 1.1 では真偽値として読まれることがある。**
	// goccy/go-yaml がどちらで読んでも取りこぼさないよう、両方の鍵を見る。
	if _, ok := parsed["name"]; !ok {
		t.Error("雛形に name がありません")
	}
	if _, ok := parsed["on"]; !ok {
		if _, okBool := parsed["true"]; !okBool {
			t.Error("雛形に on がありません（GitHub Actions はこれが無いと1度も走りません）")
		}
	}
	if _, ok := parsed["jobs"]; !ok {
		t.Fatal("雛形に jobs がありません")
	}
}

// 目的: 雛形が持つ job の名前を固定する（設計 5-3o）。
//
// **job の名前は branch protection の必須の検査として登録される名前である。**
// **名前を変えると、その登録が宙に浮き、検査が無いのにマージできる状態になる。**
// 同じ警告を .github/workflows/review-gate.yml も書いている。
//
// 与える情報: scaffold.CITemplate() を YAML として読んだもの。
// 成功条件: jobs の鍵が design-review-result と code-review-result のちょうど2つであること。
func TestCITemplate_jobの名前が2つとも変わっていない(t *testing.T) {
	var parsed struct {
		Jobs map[string]any `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(scaffold.CITemplate()), &parsed); err != nil {
		t.Fatalf("continuo-ci.yaml の雛形を YAML として解釈できません: %v", err)
	}
	if len(parsed.Jobs) != 2 {
		t.Fatalf("job が %d 個あります（2個であるべきです）: %v", len(parsed.Jobs), keysOf(parsed.Jobs))
	}
	for _, want := range []string{"design-review-result", "code-review-result"} {
		if _, ok := parsed.Jobs[want]; !ok {
			t.Errorf("job %q がありません（必須の検査に登録する名前です）: %v", want, keysOf(parsed.Jobs))
		}
	}
}

// 目的: 雛形が数える目印が、組み込みの指示書と同じ文字列であることを確かめる（設計 5-3o）。
//
// **目印は、エージェントに書かせる文字列と対でしか意味を持たない。**
// **片方だけ変えると、CI が探す目印とエージェントが書く目印が食い違い、
// 誰にも気づけないまま検査が永久に赤くなる。**
//
// **組み込みの側は internal/prompt/builtin.md にあり、利用者は変えられない。**
// ここでは雛形の側に3つの目印が全部あることを見る。
//
// 与える情報: scaffold.CITemplate() の全文。
// 成功条件: 3つの目印が全部含まれていること。
func TestCITemplate_目印が組み込みと揃っている(t *testing.T) {
	got := scaffold.CITemplate()
	for _, want := range []string{
		"<!-- design-review-result -->",
		"<!-- code-review-result -->",
		"<!-- design-review-skipped -->",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("雛形に目印 %q がありません", want)
		}
	}
}

// 目的: 目印を数える条件が、雛形の中で既存の3箇所と1文字も違わないことを確かめる。
//
// **`\s` を使ってはならない。**Python の re と jq（Oniguruma）で当たる範囲が違い、
// 全角空白 U+3000 を前に置いた本文が、片方だけ通る（2026-09-02 に実測）。
// **当たる文字を並べて書く。**この並びは
// .claude/hooks/block-merge-without-review.py の MARKER_SPACE_CLASS と、
// .github/workflows/review-gate.yml と、scripts/check-release-ready.sh に揃えてある。
//
// 与える情報: scaffold.CITemplate() の全文。
// 成功条件: 目印の判定が「先頭 + 並べた空白文字」の形で書かれ、`\s` が1つも無いこと。
func TestCITemplate_目印を数える条件が既存と揃っている(t *testing.T) {
	got := scaffold.CITemplate()

	// **jq のソースの中なので、バックスラッシュは2文字で書かれている。**
	for _, want := range []string{
		// **設計のレビューの目印だけ、`<!-- continuo:agent -->` の直後も許す。**
		// **continuo は「エージェントが書いたか」を本文の先頭ちょうどで見ている**
		// （internal/tracker/adapter.go の FetchComments）。目印を先頭に置かせると、
		// **判断票だけを書いた turn が「成果なし」と判定され、その run が人間へ渡される。**
		// **実装のレビューの目印（下）は先頭ちょうどのままである。**あちらが数えるのは
		// pull request のコメントで、continuo は pull request のコメントを読まない。
		`test("^[ \\t\\r\\n]*(<!-- continuo:agent -->[ \\t\\r\\n]*)?<!-- design-review-result -->")`,
		`test("^[ \\t\\r\\n]*<!-- code-review-result -->")`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("雛形に目印の判定 %s がありません（条件が既存の3箇所とずれています）", want)
		}
	}

	// **`\s` を使っていないこと。**jq のソースでは `\\s` と2文字で書かれる。
	if strings.Contains(got, `\\s`) {
		t.Error(`雛形の正規表現が \s を使っています（engine で当たる範囲が変わります）`)
	}

	// **投稿者の絞り込みを落としていないこと。**
	// **誰でもコメントできるので、外部の人が目印を貼れば通る状態にしない。**
	const assoc = `.author_association == "OWNER" or .author_association == "MEMBER" or .author_association == "COLLABORATOR"`
	if n := strings.Count(got, assoc); n != 3 {
		t.Errorf("投稿者の絞り込みが %d 箇所です（3箇所であるべきです。"+
			"設計のレビュー結果・飛ばす断り・実装のレビュー結果）", n)
	}
}

// reviewGatePath はこのリポジトリ自身の CI の場所である。
// Go のテストはパッケージのディレクトリを作業ディレクトリとして走るので、
// test/internal/scaffold からの相対パスで指す。
const reviewGatePath = "../../../.github/workflows/review-gate.yml"

// 目的: 配る雛形と、このリポジトリ自身の CI が、同じ条件で数えていることを確かめる。
//
// **2つは別のファイルで、互いのコメントで「同じ条件である」と名乗っている。**
// **名乗っているだけでは揃わない。**同じことが既に1度起きた（2026-09-02、
// 目印の前に全角空白を置いたコメントを、CI は数え、hook は数えなかった）。
//
// **.claude/hooks/tests/test_marker_pattern_parity.py が3箇所を見張っているが、
// あちらは code-review-result しか見ない。**雛形は4箇所目であり、
// design-review-result はあちらの検査の対象に入っていない。**ここで両方を見る。**
//
// 与える情報: scaffold.CITemplate() と .github/workflows/review-gate.yml の全文。
// 成功条件: 3つの判定の式が、どちらのファイルにもそのまま在ること。
func TestCITemplate_このリポジトリのCIと同じ条件で数えている(t *testing.T) {
	raw, err := os.ReadFile(reviewGatePath)
	if err != nil {
		t.Fatalf("このリポジトリの CI を読めません（%s）: %v", reviewGatePath, err)
	}
	gate := string(raw)
	tmpl := scaffold.CITemplate()

	// **jq のソースの中なので、バックスラッシュは2文字で書かれている。**
	for _, want := range []string{
		// **設計のレビューの目印だけ、`<!-- continuo:agent -->` の直後も許す。**
		// **continuo は「エージェントが書いたか」を本文の先頭ちょうどで見ている**
		// （internal/tracker/adapter.go の FetchComments）。目印を先頭に置かせると、
		// **判断票だけを書いた turn が「成果なし」と判定され、その run が人間へ渡される。**
		// **実装のレビューの目印（下）は先頭ちょうどのままである。**あちらが数えるのは
		// pull request のコメントで、continuo は pull request のコメントを読まない。
		`test("^[ \\t\\r\\n]*(<!-- continuo:agent -->[ \\t\\r\\n]*)?<!-- design-review-result -->")`,
		`test("^[ \\t\\r\\n]*<!-- code-review-result -->")`,
		`test("^[ \\t\\r\\n]*<!-- design-review-skipped -->[ \\t\\r\\n]*[^ \\t\\r\\n]")`,
	} {
		if !strings.Contains(gate, want) {
			t.Errorf("このリポジトリの CI に判定 %s がありません", want)
		}
		if !strings.Contains(tmpl, want) {
			t.Errorf("配る雛形に判定 %s がありません", want)
		}
	}

	// **job の名前も揃える。**必須の検査として登録する名前である。
	for _, want := range []string{"design-review-result:", "code-review-result:"} {
		if !strings.Contains(gate, "  "+want) {
			t.Errorf("このリポジトリの CI に job %q がありません", want)
		}
		if !strings.Contains(tmpl, "  "+want) {
			t.Errorf("配る雛形に job %q がありません", want)
		}
	}
}

// 目的: 雛形に backtick が1文字も無いことを固定する。
//
// **Go の raw string には backtick を置けない。**書こうとすると文字列を何度も連結することになり、
// **雛形そのものが読めなくなる。**実際、この雛形を最初に書いたときに壊れた
// （連結の途中でバックスラッシュが混入し、コンパイルが通らなかった）。
//
// **markdown のコード表記は使わず、地の文で書く。**GITHUB_STEP_SUMMARY はそれでも読める。
//
// 与える情報: scaffold.CITemplate() の全文。
// 成功条件: backtick が1文字も無いこと。
func TestCITemplate_backtickを含まない(t *testing.T) {
	if i := strings.IndexByte(scaffold.CITemplate(), '`'); i >= 0 {
		t.Errorf("雛形の %d バイト目に backtick があります（Go の raw string に置けません）", i)
	}
}

// 目的: continuo-ci.yaml を書き出せることと、書いたものがそのまま読めることを確かめる。
//
// 与える情報: 空のディレクトリ。
// 成功条件: continuo-ci.yaml が置かれ、中身が CITemplate() と一致し、YAML として読めること。
func TestWriteCIWorkflowWithValues_書き出したものがそのまま読める(t *testing.T) {
	dir := t.TempDir()

	result, err := scaffold.WriteCIWorkflowWithValues(dir, false, scaffold.Values{})
	if err != nil {
		t.Fatalf("continuo-ci.yaml を書けません: %v", err)
	}
	if got, want := filepath.Base(result.Path), scaffold.CIFileName(); got != want {
		t.Errorf("置かれたファイルの名前が %q です（%q であるべきです）", got, want)
	}
	if result.Overwritten {
		t.Error("新しく作ったのに Overwritten が真です")
	}

	raw, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("書き出した continuo-ci.yaml を読めません: %v", err)
	}
	if string(raw) != scaffold.CITemplate() {
		t.Error("書き出した中身が雛形と違います")
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("書き出した continuo-ci.yaml を YAML として解釈できません: %v", err)
	}
}

// 目的: 2枚目が既にあるときは、--force が無ければ書き換えないことを確かめる。
//
// **利用者はこのファイルを手で直して .github/workflows/ へ移す。**
// 黙って上書きすると、移す前に直した中身が消える。
//
// 与える情報: continuo-ci.yaml が既にあるディレクトリ。
// 成功条件: ErrAlreadyExists が返り、中身が1バイトも変わらないこと。
func TestWriteCIWorkflowWithValues_既にあるなら書き換えない(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, scaffold.CIFileName())
	const mine = "# 人間が手で直した\n"
	if err := os.WriteFile(path, []byte(mine), 0o600); err != nil {
		t.Fatalf("continuo-ci.yaml を書けません: %v", err)
	}

	if _, err := scaffold.WriteCIWorkflowWithValues(dir, false, scaffold.Values{}); err == nil {
		t.Fatal("既にあるのにエラーを返していません")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("continuo-ci.yaml を読めません: %v", err)
	}
	if string(got) != mine {
		t.Error("人間が手で直した中身が消えています")
	}
}

// 目的: WriteAll が2枚を独立に扱うことを確かめる（設計 5-3o）。
//
// **片方が既にあっても、もう片方は書く。**
// **版を上げた利用者が continuo init を叩くと、足りない continuo-ci.yaml だけが増える。**
// **これが移行の唯一の手順であり、--force を要求してはならない**
// （要求すると、手で直した WORKFLOW.md の本文を潰す --force を打たせることになる）。
//
// 与える情報: WORKFLOW.md だけが在るディレクトリ。
// 成功条件: continuo-ci.yaml が増え、WORKFLOW.md が1バイトも変わらないこと。
func TestWriteAll_片方だけ在るなら足りないほうを置く(t *testing.T) {
	dir := t.TempDir()
	workflow := filepath.Join(dir, "WORKFLOW.md")
	const mark = "# 人間が手で足した行\n"
	if err := os.WriteFile(workflow, []byte(scaffold.Template()+mark), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	res := scaffold.WriteAll(dir, false, scaffold.Values{})

	if !res.Wrote() {
		t.Error("1枚も書いていません（足りない continuo-ci.yaml を置くべきです）")
	}
	if res.BothExisted() {
		t.Error("2枚とも既にあった扱いになっています")
	}
	if res.WorkflowFailed() {
		t.Errorf("WORKFLOW.md が「既にある」以外の理由で落ちています: %v", res.WorkflowErr)
	}
	if res.CIFailed() {
		t.Errorf("continuo-ci.yaml を置けていません: %v", res.CIErr)
	}
	if res.CIErr != nil {
		t.Errorf("continuo-ci.yaml が置かれていません: %v", res.CIErr)
	}

	got, err := os.ReadFile(workflow)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	if !strings.HasSuffix(string(got), mark) {
		t.Error("人間が足した行が消えています")
	}
	if _, err := os.Stat(filepath.Join(dir, scaffold.CIFileName())); err != nil {
		t.Errorf("continuo-ci.yaml が置かれていません: %v", err)
	}
}

// 目的: 2枚とも既にあるときだけ BothExisted が真になることを確かめる（設計 5-3o）。
//
// **このときだけ --force を勧めて終了コード 1 で終える。**
// 片方でも置けたなら、置けたことを報告して 0 で終える。
//
// 与える情報: 2枚とも在るディレクトリ。
// 成功条件: BothExisted が真で、Wrote が偽であること。
func TestWriteAll_2枚とも在るなら1枚も書かない(t *testing.T) {
	dir := t.TempDir()
	if res := scaffold.WriteAll(dir, false, scaffold.Values{}); !res.Wrote() {
		t.Fatalf("1枚目から書けていません: workflow=%v ci=%v", res.WorkflowErr, res.CIErr)
	}

	res := scaffold.WriteAll(dir, false, scaffold.Values{})
	if !res.BothExisted() {
		t.Errorf("2枚とも在るのに BothExisted が偽です: workflow=%v ci=%v", res.WorkflowErr, res.CIErr)
	}
	if res.Wrote() {
		t.Error("2枚とも在るのに書いた扱いになっています")
	}
	if res.WorkflowFailed() || res.CIFailed() {
		t.Errorf("「既にある」を失敗として扱っています: workflow=%v ci=%v", res.WorkflowErr, res.CIErr)
	}
}

// keysOf は map の鍵を並べる。失敗したときに、何が在るのかを出すために使う。
//
// m: 鍵を取り出す map。
// 戻り値: 鍵の一覧。
func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
