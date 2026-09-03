// **「描画」がリポジトリへ戻ってこないことを機械で確かめる。**
//
// **`text/template` の `{{...}}` を issue の値に置き換えることを「描画」と呼ばない**、
// という決めごとがある（[docs/spec/translation-glossary.md](../../../docs/spec/translation-glossary.md) の2）。
// **「変数展開」と書く。**
//
// **人が気をつけるだけでは崩れた。**#195（「描画」をやめて「変数展開」に統一する決定が、
// コードと文書に反映されていない）を直した時点で、リポジトリには75箇所の「描画」が残っていた。
// **決めた当人が書いた文書にも残っていた。**同じことがもう一度起きる。
//
// **語そのものを全部禁じることはできない。**規則そのものが「『描画』と書かない」と
// 引用しなければならないからである。**引用してよい場所と件数を下の表に書き、
// それ以外に1つでも出たら落とす。**
//
// **表の数は上限ではなく実数である。**増えたら「新しく書いた」、減ったら「規則の文を
// 変えたのに表を直していない」として落ちる（`no_japanese_messages_test.go` の
// `japaneseAllowance` と同じ扱いである）。
package testdesign_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// renderingWord は、使ってはならない語である。
const renderingWord = "描画"

// renderingWordAllowance は、`renderingWord` を書いてよい場所と、そこにある件数である。
//
// **キーはリポジトリの root からの相対パスである。**どちらも「この語を使わない」という
// 決めごとそのものを書いている文書なので、語を引用せずには書けない。
var renderingWordAllowance = map[string]int{
	// 規則の本体。「『描画』と書かない」「『描画』が戻ってこないことは機械が見ている」を含む。
	"docs/spec/translation-glossary.md": 3,
	// 決定の記録（8-3）。**出し終えた版の計画なので、規則の本体はここには無い。**
	// 消すと何を決めたのかが読めなくなるので残してある。
	"docs/plans/release_v0113.md": 2,
}

// renderingWordSkipDirs は走査しないディレクトリである（リポジトリの root からの相対パス）。
//
// **どれも `.gitignore` 済みか、git の内部である。**手元にしか無いものを見ると、
// 同じ commit でも人によって結果が変わる。
var renderingWordSkipDirs = map[string]bool{
	".git": true,
	// 準拠する仕様（openai/symphony の SPEC.md）。リポジトリに同梱しない。
	"docs/spec/symphony": true,
	// エージェントの並列実行が作る worktree。
	".claude/worktrees": true,
	// 人間の指示の原文の覚え書き。**指示そのものに「描画」が出てくる。**
	".claude/requests": true,
	// 作業用の一時ファイルと、配布物の組み立て先。
	"tmp":  true,
	"dist": true,
}

// renderingWordSkipFiles は走査しないファイルである（リポジトリの root からの相対パス）。
//
// **どちらも `.gitignore` 済みの、個人の環境の設定である。**
var renderingWordSkipFiles = map[string]bool{
	".claude/settings.local.json": true,
	".claude/local-guidelines.md": true,
}

// renderingWordExtensions は走査する拡張子である。
//
// **人が読む文字が入るものだけを見る。**実行ファイルや画像を読み込むと、
// 走査が遅くなるうえに、たまたま同じバイト列が並んだだけで落ちる。
var renderingWordExtensions = map[string]bool{
	".go": true, ".md": true, ".json": true, ".yaml": true, ".yml": true,
	".sh": true, ".toml": true, ".html": true, ".txt": true, ".py": true,
}

// TestDesign_描画という語がリポジトリに戻っていない は、訳語集の決めごとを機械で守る。
//
// 目的: `renderingWord` が、`renderingWordAllowance` に書いた場所と件数のほかに無いこと。
// 与える情報: リポジトリの全文書とソース（`.gitignore` 済みのものを除く）。
// 成功条件: 表に無い場所で1件も見つからず、表の件数がいまの実数と一致すること。
// **走査したファイルが0件でないこと**も確かめる。
func TestDesign_描画という語がリポジトリに戻っていない(t *testing.T) {
	root := filepath.Join("..", "..", "..")

	checked := 0
	found := map[string]int{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			if renderingWordSkipDirs[rel] {
				return fs.SkipDir
			}
			return nil
		}
		if renderingWordSkipFiles[rel] {
			return nil
		}
		// このファイル自身は、禁じた語を表と文言に書いている。
		if strings.HasSuffix(rel, "rendering_word_glossary_test.go") {
			return nil
		}
		if !renderingWordExtensions[filepath.Ext(rel)] {
			return nil
		}
		checked++

		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		text := string(body)
		if !strings.Contains(text, renderingWord) {
			return nil
		}
		if _, allowed := renderingWordAllowance[rel]; allowed {
			found[rel] += strings.Count(text, renderingWord)
			return nil
		}
		for i, line := range strings.Split(text, "\n") {
			if !strings.Contains(line, renderingWord) {
				continue
			}
			t.Errorf("%s:%d に「%s」が書かれています。\n  %s\n"+
				"  **`text/template` の `{{...}}` を issue の値に置き換えることは"+
				"「変数展開」と書きます**（docs/spec/translation-glossary.md の2）。\n"+
				"  設定値の `${NAME}` の置き換えは別の仕組みで、そちらは「環境変数の展開」です。",
				rel, i+1, renderingWord, strings.TrimSpace(line))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("%s を走査できません: %v", root, err)
	}
	if checked == 0 {
		t.Fatalf("走査したファイルが0件です（対象: %s）。パスを確かめてください", root)
	}

	for _, rel := range sortedKeys(renderingWordAllowance) {
		want := renderingWordAllowance[rel]
		got := found[rel]
		switch {
		case got > want:
			t.Errorf("%s の「%s」が %d 件に増えました（表に書いてあるのは %d 件）。\n"+
				"  **ここは規則そのものを書いている場所なので、引用は増やさないでください。**\n"+
				"  規則を説明する文が要るなら、語を引かずに書けないかを先に試してください。",
				rel, renderingWord, got, want)
		case got < want:
			t.Errorf("%s の「%s」が %d 件に減りました（表に書いてあるのは %d 件）。\n"+
				"  test/internal/testdesign の renderingWordAllowance の数を %d へ下げてください。\n"+
				"  **0 になったら行ごと消すこと。**",
				rel, renderingWord, got, want, got)
		}
	}
}
