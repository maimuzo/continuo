// 送る文面へ渡す変数を、1箇所で組み立てていることの検査である
// （issue #183（エージェントへ実際に送られる文面を、事前に確かめられない（変数が展開されない）））。
//
// **外部へ1回も接続しない。**
package prompt_test

import (
	"sort"
	"testing"

	"github.com/maimuzo/continuo/internal/prompt"
	"github.com/maimuzo/continuo/internal/tracker"
)

// 目的: `RenderData` が返す名前が、`SampleData` と1つも違わないことを固定する（issue #183）。
//
// **なぜ要るか。**`RenderData` は continuo が実際に送る経路
// （`internal/orchestrator` の `renderFirstPrompt`）と、人間が事前に確かめる経路
// （`continuo prompt --show --url`）の両方が呼ぶ。
// **`SampleData` は、起動時の検査（`Validate`）と `continuo doctor` が使う。**
//
// **この2つがずれると、検査は通るのに本番で起動しない**（あるいはその逆になる）。
// **同じ形の欠陥が3回起きている**（`push_branch` と `progress_interval_minutes`）。
//
// **入れ子の中まで比べる。**送る文面が使う11個のうち8個は `issue` の中にある
// （`identifier` / `owner` / `repo` / `number` / `url` / `title` / `state` / `labels`）。
// **最上位だけを比べると、片方の入れ子にだけ名前を足しても通ってしまう。**
// そのときに落ちるのは `{{.issue.<新しい名前>}}` を本文へ書いた利用者の手元だけである。
//
// 与える情報: 作り物の issue から作った `RenderData` と、`SampleData`。
// 成功条件: 入れ子まで含めた名前の集合が完全に一致すること。
func TestRenderData_返す名前がSampleDataと一致する(t *testing.T) {
	got := prompt.RenderData(tracker.Issue{}, nil, 0)
	want := prompt.SampleData()

	if diff := nameDiff(got, want); diff != "" {
		t.Errorf("RenderData と SampleData の名前が違います。%s\n"+
			"片方だけ増やすと、検査は通るのに本番で起動しません（またはその逆になります）", diff)
	}
}

// 目的: 渡した値がそのまま変数になることを固定する（issue #183）。
//
// **とくに3つ。**
//
//	`{{.attempt}}`                   1回目は nil。`{{if .attempt}}` が偽になる
//	`{{.push_branch}}`               リンクが無ければ空文字。`origin/` は付けない
//	`{{.progress_interval_minutes}}` ミリ秒を渡し、分に直すのは RenderData の中
//
// **分への変換を呼び出し側でやらせない。**割り算が2箇所に散ると、片方を直したときにずれる。
//
// 与える情報: 値を埋めた issue と、埋めていない issue。
// 成功条件: 変換と、nil / 空文字の扱いが期待どおりであること。
func TestRenderData_渡した値がそのまま変数になる(t *testing.T) {
	url := "https://github.com/octocat/hello-world/issues/42"
	branch := "work/issue-42"
	issue := tracker.Issue{
		Identifier: "octocat/hello-world#42", Owner: "octocat", Repo: "hello-world",
		Number: 42, URL: &url, Title: "題名", State: "Ready",
		Labels: []string{"bug"}, BranchName: &branch,
	}

	// 1回目（attempt は nil）。
	got := prompt.RenderData(issue, nil, 3600000)
	if got["attempt"] != nil {
		t.Errorf("1回目の attempt が nil ではありません: %#v", got["attempt"])
	}
	if got["push_branch"] != "work/issue-42" {
		t.Errorf("push_branch が生の名前ではありません: %#v", got["push_branch"])
	}
	if got["progress_interval_minutes"] != 60 {
		t.Errorf("ミリ秒から分への変換が違います: %#v（60 を期待）", got["progress_interval_minutes"])
	}

	// やり直し（attempt に回数が入る）。
	n := 3
	got = prompt.RenderData(issue, &n, 60000)
	if got["attempt"] != 3 {
		t.Errorf("attempt が回数になっていません: %#v", got["attempt"])
	}
	if got["progress_interval_minutes"] != 1 {
		t.Errorf("ミリ秒から分への変換が違います: %#v（1 を期待）", got["progress_interval_minutes"])
	}

	// リンクも URL も無い issue。**空文字にする。**nil を入れると変数展開で落ちる。
	got = prompt.RenderData(tracker.Issue{}, nil, 0)
	if got["push_branch"] != "" {
		t.Errorf("リンクが無いのに push_branch が空文字ではありません: %#v", got["push_branch"])
	}
	inner, ok := got["issue"].(map[string]any)
	if !ok {
		t.Fatalf("issue の中身が map ではありません: %#v", got["issue"])
	}
	if inner["url"] != "" {
		t.Errorf("URL が無いのに空文字ではありません: %#v", inner["url"])
	}
}

// 目的: 変数展開が、実際に通ることを固定する（issue #183）。
//
// **名前が合っているだけでは足りない。**`missingkey=error` を掛けてあるので、
// **渡していない名前を組み込みが使っていれば、ここで落ちる。**
//
// 与える情報: 組み込みだけで組み立てた断片と、`RenderData` が返す変数。
// 成功条件: 変数展開が通り、`{{` が1つも残らないこと。
func TestRenderData_組み込みの文面を実際に展開できる(t *testing.T) {
	url := "https://github.com/octocat/hello-world/issues/42"
	issue := tracker.Issue{
		Identifier: "octocat/hello-world#42", Owner: "octocat", Repo: "hello-world",
		Number: 42, URL: &url, Title: "題名", State: "Ready", Labels: []string{"bug"},
	}
	frag := prompt.Build("", "/dev/null/WORKFLOW.md")

	for _, c := range []struct {
		name    string
		attempt *int
	}{
		{"1回目", nil},
		{"やり直し", func() *int { n := 2; return &n }()},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := frag.Render(prompt.RenderData(issue, c.attempt, 3600000))
			if err != nil {
				t.Fatalf("変数展開に失敗しました: %v", err)
			}
			if got := indexOfTemplate(out); got >= 0 {
				t.Errorf("展開されていない変数が残っています: %q", out[got:min(got+60, len(out))])
			}
		})
	}
}

// nameDiff は2つの変数の一覧の、入れ子まで含めた名前の食い違いを文にする。
//
// **最上位だけを比べてはならない。**送る文面が使う名前の大半は `issue` の中にあり、
// **そこだけを増やしても最上位の集合は変わらない。**
//
// a / b: 比べる変数の一覧。
// 戻り値: 食い違いの説明。一致していれば空文字。
func nameDiff(a, b map[string]any) string {
	inA, inB := onlyIn(a, b, ""), onlyIn(b, a, "")
	if len(inA) == 0 && len(inB) == 0 {
		return ""
	}
	return "RenderData にしかない名前: " + join(inA) + " / SampleData にしかない名前: " + join(inB)
}

// onlyIn は x にあって y に無い名前を、入れ子まで降りて集める。
//
// **両方が入れ子のときだけ降りる。**片方だけが入れ子なら、その名前自体は両方にあるので
// 食い違いとして数えない（値の型の違いは、この検査が見るものではない）。
//
// x / y: 比べる変数の一覧。
// prefix: ここまでに降りてきた名前（`issue.` のような形。最上位では空文字）。
// 戻り値: `issue.labels` のように、点で繋いだ名前の並び。
func onlyIn(x, y map[string]any, prefix string) []string {
	var out []string
	for k, xv := range x {
		yv, ok := y[k]
		if !ok {
			out = append(out, prefix+k)
			continue
		}
		xm, xok := xv.(map[string]any)
		ym, yok := yv.(map[string]any)
		if xok && yok {
			out = append(out, onlyIn(xm, ym, prefix+k+".")...)
		}
	}
	sort.Strings(out)
	return out
}

// join は名前の並びを読める形にする。
//
// names: 名前の並び。
// 戻り値: カンマで繋いだ文字列。空なら "（無し）"。
func join(names []string) string {
	if len(names) == 0 {
		return "（無し）"
	}
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}

// indexOfTemplate は、展開されずに残った変数の位置を返す。
//
// s: 変数展開したあとの文字列。
// 戻り値: `{{` の位置。無ければ -1。
func indexOfTemplate(s string) int {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '{' && s[i+1] == '{' {
			return i
		}
	}
	return -1
}
