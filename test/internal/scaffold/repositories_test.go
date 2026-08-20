// Package scaffold_test のうち、このファイルは `continuo init` が
// trust.repositories をボードから拾って並べる部分を検証する（設計 3-33）。
//
// **拾うだけである。信頼は登録しない。**登録するのは `continuo trust` であり、
// その対象は人間がこの一覧から要らない行を消したあとに残ったものである。
// **ボードは他人が編集できる**ので、拾った一覧をそのまま信頼させてはならない。
package scaffold_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// 目的: ボードに載っているリポジトリを、重複なく辞書順で拾うことを確認する。
// あわせて draft issue を数えないことを見る（リポジトリに属していないので、
// 信頼させる対象が存在しない）。
//
// 与える情報: 同じリポジトリの issue を2件、別のリポジトリの issue を1件、
// draft issue を1件返す `gh project item-list` の差し替え。
// 成功条件: 2件が辞書順で並び、draft issue が数えられていないこと。
func TestDetect_ボードに載っているリポジトリを重複なく並べる(t *testing.T) {
	run, calls := fakeGH(t, map[string]struct {
		out []byte
		err error
	}{
		"api user":          ghResponse("maimuzo\n", nil),
		"project list":      ghResponse(oneProjectJSON, nil),
		"project item-list": ghResponse(twoRepoItemsJSON, nil),
	})

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{RunGH: run})

	want := []string{"maimuzo/continuo", "maimuzo/koetsumugi"}
	if strings.Join(got.Values.Repositories, ",") != strings.Join(want, ",") {
		t.Errorf("拾ったリポジトリが想定と違う: got %v, want %v", got.Values.Repositories, want)
	}
	if !strings.Contains((*calls)[2], "project item-list 3 --owner maimuzo ") {
		t.Errorf("決まったボードの番号と owner で項目を引いていない: %q", (*calls)[2])
	}
}

// 目的: 拾ったあとに「要らない行を消せ」と案内することを確認する。
//
// **拾った一覧をそのまま信頼させてはならない**（設計 3-33）。
// ここで案内が出ないと、人間が削る手順そのものが誰にも伝わらない。
//
// 与える情報: リポジトリを2件返す差し替え。
// 成功条件: 案内に「消して」と `continuo trust --dry-run` が含まれること。
func TestDetect_拾ったあとに要らない行を消せと案内する(t *testing.T) {
	run, _ := fakeGH(t, map[string]struct {
		out []byte
		err error
	}{
		"api user":          ghResponse("maimuzo\n", nil),
		"project list":      ghResponse(oneProjectJSON, nil),
		"project item-list": ghResponse(twoRepoItemsJSON, nil),
	})

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{RunGH: run})

	field := fieldOf(t, got, scaffold.RepositoriesKey)
	if !field.Filled {
		t.Fatalf("拾えているのに埋まった扱いになっていない: %+v", field)
	}
	if !containsSubstring(field.Advice, "消して") {
		t.Errorf("要らない行を消すことを案内していない: %v", field.Advice)
	}
	if !containsSubstring(field.Advice, "continuo trust --dry-run") {
		t.Errorf("何を許すことになるかの確かめ方を案内していない: %v", field.Advice)
	}
}

// 目的: ボードの項目を引けなくても失敗せず、手で書ける案内を出すことを確認する。
//
// **雛形そのものは書けるので、ここで止めない。**
//
// 与える情報: `gh project item-list` がエラーを返す差し替え。
// 成功条件: repositories が埋まらず、案内に trust.repositories が含まれること。
func TestDetect_ボードの項目を引けなくても失敗しない(t *testing.T) {
	run, _ := fakeGH(t, map[string]struct {
		out []byte
		err error
	}{
		"api user":          ghResponse("maimuzo\n", nil),
		"project list":      ghResponse(oneProjectJSON, nil),
		"project item-list": ghResponse("", scaffold.ErrGHNotFound),
	})

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{RunGH: run})

	if len(got.Values.Repositories) != 0 {
		t.Errorf("引けていないのに値が入っている: %v", got.Values.Repositories)
	}
	field := fieldOf(t, got, scaffold.RepositoriesKey)
	if field.Filled {
		t.Error("引けていないのに埋まった扱いになっている")
	}
	if !containsSubstring(field.Advice, "trust.repositories") {
		t.Errorf("手で書けることを案内していない: %v", field.Advice)
	}
}

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
		Owner:         "maimuzo",
		ProjectNumber: 3,
		Repositories:  []string{"maimuzo/continuo", "maimuzo/koetsumugi"},
	})

	if strings.Contains(filled, "repositories: []") {
		t.Error("プレースホルダの repositories: [] が残っている")
	}
	for _, want := range []string{
		`    - "maimuzo/continuo"`,
		`    - "maimuzo/koetsumugi"`,
		"要らない行は消すこと",
	} {
		if !strings.Contains(filled, want) {
			t.Errorf("埋めたあとに次の内容が無い:\n  %q", want)
		}
	}
	// 埋めたあとに、プレースホルダのときの説明が残っていてはならない。
	if strings.Contains(filled, "continuo init がボードから拾って並べるので") {
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
		Owner:         "maimuzo",
		ProjectNumber: 3,
		Repositories:  []string{"maimuzo/continuo", "maimuzo/koetsumugi"},
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
	want := []string{"maimuzo/continuo", "maimuzo/koetsumugi"}
	got := loaded.Config.Trust.Repositories
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("trust.repositories が反映されていない: got %v, want %v", got, want)
	}
}
