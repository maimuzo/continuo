// Package scaffold_test のうち、このファイルは `continuo init` が
// trust.repositories をカンバンから拾って並べる部分を検証する（設計 3-33）。
//
// **拾うだけである。信頼は登録しない。**登録するのは `continuo trust` であり、
// その対象は人間がこの一覧から要らない行を消したあとに残ったものである。
// **カンバンは他人が編集できる**ので、拾った一覧をそのまま信頼させてはならない。
package scaffold_test

import (
	"os"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// 目的: 拾った一覧で雛形を埋めたとき、「要らない行は消すこと」がファイルに残ることを確認する。
//
// **WORKFLOW.md を開く人は設計文書を持っていない。**この一文が雛形に残っていなければ、
// 拾った一覧をそのまま登録してよいものだと読まれる。
//
// 与える情報: リポジトリ2件を持つ Values。
// 成功条件: 2件が並び、プレースホルダの `repositories: []` が消え、
// 「要らない行は消すこと」が残ること。
func TestTemplateWithValues_repositoriesを埋めても消せという案内が残る(t *testing.T) {
	filled := scaffold.TemplateWithValues(scaffold.Values{
		Owner:         "octocat",
		ProjectNumber: 3,
		Repositories:  []string{"acme/anvil", "octocat/hello-world"},
	})

	if strings.Contains(filled, "repositories: []") {
		t.Error("プレースホルダの repositories: [] が残っている")
	}
	for _, want := range []string{
		`    - "acme/anvil"`,
		`    - "octocat/hello-world"`,
		"要らない行は消すこと",
	} {
		if !strings.Contains(filled, want) {
			t.Errorf("埋めたあとに次の内容が無い:\n  %q", want)
		}
	}
	// 埋めたあとに、プレースホルダのときの説明が残っていてはならない。
	if strings.Contains(filled, "continuo init がカンバンから拾って並べるので") {
		t.Error("埋めたあとなのに、これから埋める前提の説明が残っている")
	}
}

// 目的: 拾った一覧を埋めた WORKFLOW.md が、そのまま continuo の設定として読めることを確認する。
//
// **雛形が config の検査を通らない値を書いてはならない。**通らないと、
// `continuo init` の直後に `continuo` が起動できない。
//
// 与える情報: リポジトリ2件を埋めて書き出したファイル。
// 成功条件: config.Load が成功し、2件がそのまま読み出せること。
// 雛形として成立していること（設計 5-2 / 5-3 に照らして）もあわせて確かめる。
func TestWriteTemplateWithValues_repositoriesを埋めたファイルはそのまま読み込める(t *testing.T) {
	dir := t.TempDir()

	result, err := scaffold.WriteTemplateWithValues(dir, false, scaffold.Values{
		Owner:         "octocat",
		ProjectNumber: 3,
		Repositories:  []string{"acme/anvil", "octocat/hello-world"},
	})
	if err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}

	raw, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("書き出したファイルを読めない: %v", err)
	}
	assertTemplateFollowsDesign(t, "trust.repositories を埋めた WORKFLOW.md", string(raw))

	loaded, err := config.Load(result.Path)
	if err != nil {
		t.Fatalf("埋めた雛形を読み込めなかった: %v", err)
	}
	want := []string{"acme/anvil", "octocat/hello-world"}
	got := loaded.Config.Trust.Repositories
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("trust.repositories が反映されていない: got %v, want %v", got, want)
	}
}
