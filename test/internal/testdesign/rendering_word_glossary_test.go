// **「描画」と「レンダリング」がリポジトリへ戻ってこないことを機械で確かめる。**
//
// **`text/template` の `{{...}}` を issue の値に置き換えることを「描画」と呼ばない**、
// という決めごとがある（[docs/spec/translation-glossary.md](../../../docs/spec/translation-glossary.md) の2）。
// **「変数展開」と書く。**「レンダリング」も使わない（同じものを2つの呼び方で呼ばない）。
//
// **人が気をつけるだけでは崩れた。**#195（「描画」をやめて「変数展開」に統一する決定が、
// コードと文書に反映されていない）を直した時点で、リポジトリには75箇所の「描画」が残っていた。
// **決めた当人が書いた文書にも残っていた。**同じことがもう一度起きる。
//
// **「レンダリング」も同じ扱いにする。**"render" の直音写なので、日本語を書く人の手が
// いちばん自然に伸びる。**origin/main には実際に3箇所あった**ので、戻る見込みは実測で分かっている。
//
// **語そのものを全部禁じることはできない。**規則そのものが「『描画』と書かない」と
// 引用しなければならないからである。**引用してよい場所と件数を下の表に書き、
// それ以外に1つでも出たら落とす。**
//
// **表の数は上限ではなく実数である。**増えたら「新しく書いた」、減ったら「規則の文を
// 変えたのに表を直していない」として落ちる（`no_japanese_messages_test.go` の
// `japaneseAllowance` と同じ扱いである）。
//
// **見る対象は `git ls-files` が返すものだけである。**`filepath.WalkDir` でリポジトリ全体を
// 歩くと、**追跡していない書きかけのメモ1枚で `go test ./...` が落ちる。**
// この issue に取り組む人は、issue の題名（「描画」を含む）を手元のメモへ貼りがちで、
// **落ちるのはその人が触ってもいない `./...` 全体になる。**CI は追跡しているものしか
// checkout しないので緑のまま通り、手元だけが赤くなる。
// **`git ls-files` に切り替えると、`.gitignore` 済みのものを手で数え上げる表も要らなくなる。**
package testdesign_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// renderingWordAllowance は、禁じた語を書いてよい場所と、そこにある件数である。
//
// **外側のキーが禁じた語、内側のキーがリポジトリの root からの相対パスである。**
// どれも「この語を使わない」という決めごとそのものを書いている文書なので、
// 語を引用せずには書けない。
//
// **このファイル自身は表に入れない。**下の走査が、自分のパスを完全一致で外す。
var renderingWordAllowance = map[string]map[string]int{
	"描画": {
		// 規則の本体。「『描画』と書かない」「『描画』が戻ってこないことは機械が見ている」を含む。
		"docs/spec/translation-glossary.md": 3,
		// 決定の記録（8-3）。**出し終えた版の計画なので、規則の本体はここには無い。**
		// 消すと何を決めたのかが読めなくなるので残してある。
		"docs/plans/release_v0113.md": 2,
		// v0.1.14 に入れたものの記録。**issue #195 と PR #200 の題名に、この語がそのまま入っている。**
		// 題名は GitHub 側の文字列なので書き換えられない。
		// **言い換えると、どの issue の話なのかを引けなくなる。**
		"docs/plans/release_v0114.md": 2,
	},
	"レンダリング": {
		// 規則の本体。「『レンダリング』も使わない」と、その理由。
		"docs/spec/translation-glossary.md": 2,
	},
}

// renderingWordSelf は、この検査のファイル自身のパスである。
//
// **完全一致で外す。**末尾一致にすると、同じ名前のファイルをどこに置いても見逃す。
const renderingWordSelf = "test/internal/testdesign/rendering_word_glossary_test.go"

// renderingWordExtensions は走査する拡張子である。
//
// **人が読む文字が入るものだけを見る。**実行ファイルや画像を読み込むと、
// 走査が遅くなるうえに、たまたま同じバイト列が並んだだけで落ちる。
//
// **`.jsonl` を入れているのは、`docs/evidence/` の証跡が日本語の散文を持つためである。**
var renderingWordExtensions = map[string]bool{
	".go": true, ".md": true, ".json": true, ".jsonl": true, ".yaml": true,
	".yml": true, ".sh": true, ".toml": true, ".html": true, ".txt": true,
	".py": true,
}

// renderingWordTrackedFiles は、git が追跡しているファイルのパスを返す。
//
// t: 呼び出し元のテスト。
// root: リポジトリの root への相対パス。
// 戻り値: root からの相対パスの一覧（`/` 区切り）。
func renderingWordTrackedFiles(t *testing.T, root string) []string {
	t.Helper()

	out, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files を実行できません（対象: %s）: %v", root, err)
	}
	trimmed := strings.TrimRight(string(out), "\x00")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\x00")
}

// TestDesign_禁じた語がリポジトリに戻っていない は、訳語集の決めごとを機械で守る。
//
// 目的: 禁じた2語（「描画」「レンダリング」）が、`renderingWordAllowance` に書いた
// 場所と件数のほかに無いこと。
// 与える情報: git が追跡しているファイルのうち、人が読む拡張子のもの。
// 成功条件: 表に無い場所で1件も見つからず、表の件数がいまの実数と一致すること。
// **走査したファイルが0件でないこと**も確かめる。
func TestDesign_禁じた語がリポジトリに戻っていない(t *testing.T) {
	root := filepath.Join("..", "..", "..")

	checked := 0
	// found[語][パス] = 件数。
	found := map[string]map[string]int{}
	for word := range renderingWordAllowance {
		found[word] = map[string]int{}
	}

	for _, rel := range renderingWordTrackedFiles(t, root) {
		if rel == renderingWordSelf {
			// このファイル自身は、禁じた語を表と文言に書いている。
			continue
		}
		if !renderingWordExtensions[filepath.Ext(rel)] {
			continue
		}
		checked++

		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s を読めません: %v", rel, err)
		}
		text := string(body)

		for _, word := range sortedKeys2(renderingWordAllowance) {
			if !strings.Contains(text, word) {
				continue
			}
			if _, allowed := renderingWordAllowance[word][rel]; allowed {
				found[word][rel] += strings.Count(text, word)
				continue
			}
			for i, line := range strings.Split(text, "\n") {
				if !strings.Contains(line, word) {
					continue
				}
				t.Errorf("%s:%d に「%s」が書かれています。\n  %s\n"+
					"  **`text/template` の `{{...}}` を issue の値に置き換えることは"+
					"「変数展開」と書きます**（docs/spec/translation-glossary.md の2）。\n"+
					"  設定値の `${NAME}` の置き換えは別の仕組みで、そちらは「環境変数の展開」です。",
					rel, i+1, word, strings.TrimSpace(line))
			}
		}
	}

	if checked == 0 {
		t.Fatalf("走査したファイルが0件です（対象: %s）。パスを確かめてください", root)
	}

	for _, word := range sortedKeys2(renderingWordAllowance) {
		for _, rel := range sortedKeys(renderingWordAllowance[word]) {
			want := renderingWordAllowance[word][rel]
			got := found[word][rel]
			if got == want {
				continue
			}
			t.Errorf("%s の「%s」が %d 件です（表は %d 件）。"+
				"**増えたなら新しく書いた、減ったなら規則の文を変えて表を直していない、のどちらかです**",
				rel, word, got, want)
		}
	}
}

// sortedKeys2 は、外側のキー（禁じた語）を並べ替えて返す。
//
// **並べ替えるのは、落ちたときの出力の順番を固定するためである。**
// map の走査の順番は実行のたびに変わる。
//
// m: 禁じた語をキーに持つ map。
// 戻り値: 並べ替えたキーの一覧。
func sortedKeys2(m map[string]map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
