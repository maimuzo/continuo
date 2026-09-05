// **2枚の README が、同じ節構成と同じ訳語を保っていることを機械で確かめる。**
//
// **人が気をつけるだけでは崩れた。**#104（README の最初に「何が手に入るか」を並べる）と
// #114（worktree の置き場所が gwq の規則に合わせてあることを README に書く）は、
// どちらも英語と日本語の両方へ同じ内容を足す作業である。**片方だけ直すと、
// 読む言語によって書いてあることが違う README になる。**
//
// 訳語は [docs/spec/translation-glossary.md] が正である。**カンバンの英語は
// 2語で `kanban board`、日本語は「カンバン」**と決まっており、
// **単独の `board` と「カンバン」は使わない。**#127（英語版 README と en.json の
// board を kanban board に統一した変更を、マージ後にレビューする）で
// 置き換えたばかりなので、戻るのをここで止める。
//
// **語そのものを禁じるのではなく、許す形を先に取り除いてから探す。**
// `dashboard` と `docs/images/board.png` は正しい語なので、取り除いてから残りを見る。
package testdesign_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readmeEN は英語の README のリポジトリ内のパスである。
const readmeEN = "README.md"

// readmeJA は日本語の README のリポジトリ内のパスである。
const readmeJA = "README.ja.md"

// glossaryPath は訳語集のリポジトリ内のパスである。
const glossaryPath = "docs/spec/translation-glossary.md"

// allowedBoardForms は、英語の README で `board` を含んでよい形である。
//
// **長いものから先に取り除く。**`kanban board` を先に消さないと、
// `dashboard` を消しただけでは `kanban board` の `board` が残る。
var allowedBoardForms = []string{
	"docs/images/board.png",
	"kanban board",
	"Kanban board",
	"dashboard",
	"Dashboard",
	"keyboard",
}

// allowedKatakanaBoardForms は、日本語の README で「カンバン」を含んでよい形である。
//
// **どれも別の物の名前である。**カンバンを指す語ではないので、取り除いてから探す。
var allowedKatakanaBoardForms = []string{
	"ダッシュボード",
	"キーボード",
	"クリップカンバン",
}

// TestDesign_英語のREADMEが単独のboardを使っていない は、訳語集の決めごとを機械で守る。
//
// 目的: カンバンを指す語が `kanban board` の2語にそろっていること。
// 与える情報: README.md の全行。
// 成功条件: 許す形を取り除いたあとに `board` が1つも残らないこと。
func TestDesign_英語のREADMEが単独のboardを使っていない(t *testing.T) {
	lines := readmeLines(t, readmeEN)
	for i, line := range lines {
		stripped := line
		for _, form := range allowedBoardForms {
			stripped = strings.ReplaceAll(stripped, form, "")
		}
		if !strings.Contains(strings.ToLower(stripped), "board") {
			continue
		}
		t.Errorf("%s:%d に単独の `board` が残っています。\n  %s\n"+
			"  **カンバンを指す語は2語で `kanban board` にそろえます**"+
			"（docs/spec/translation-glossary.md）。",
			readmeEN, i+1, strings.TrimSpace(line))
	}
}

// TestDesign_日本語のREADMEがカンバンをカンバンと書いていない は、訳語集の決めごとを機械で守る。
//
// 目的: カンバンを指す語が「カンバン」にそろっていること。
// 与える情報: README.ja.md の全行。
// 成功条件: 許す形を取り除いたあとに「カンバン」が1つも残らないこと。
func TestDesign_日本語のREADMEがカンバンをカンバンと書いていない(t *testing.T) {
	lines := readmeLines(t, readmeJA)
	for i, line := range lines {
		stripped := line
		for _, form := range allowedKatakanaBoardForms {
			stripped = strings.ReplaceAll(stripped, form, "")
		}
		if !strings.Contains(stripped, "カンバン") {
			continue
		}
		t.Errorf("%s:%d に「カンバン」が残っています。\n  %s\n"+
			"  **カンバンを指す語は「カンバン」にそろえます**"+
			"（docs/spec/translation-glossary.md）。",
			readmeJA, i+1, strings.TrimSpace(line))
	}
}

// TestDesign_2枚のREADMEの節構成が一致している は、片方だけ直したことを機械で弾く。
//
// **見出しの文言は言語ごとに違うので、突き合わせるのは並びと数だけである。**
// 節を1つ足したのに片方へ入れ忘れると、ここで落ちる。
//
// 目的: README.md と README.ja.md が同じ数の節を、同じ深さの並びで持つこと。
// 与える情報: 2枚の README の見出しの行。
// 成功条件: `##` と `###` の並びが完全に一致すること。**節が0件でないこと**も確かめる。
func TestDesign_2枚のREADMEの節構成が一致している(t *testing.T) {
	en := headingLevels(t, readmeEN)
	ja := headingLevels(t, readmeJA)

	if len(en) == 0 {
		t.Fatalf("%s から節を1つも読めませんでした。テストの走査が壊れています。", readmeEN)
	}
	if len(en) != len(ja) {
		t.Fatalf("節の数が食い違います。%s は %d 個、%s は %d 個です。\n"+
			"  **両方の README へ同じ節を入れてください。**"+
			"片方だけに足すと、読む言語で書いてあることが変わります。",
			readmeEN, len(en), readmeJA, len(ja))
	}
	for i := range en {
		if en[i] == ja[i] {
			continue
		}
		t.Errorf("%d 番目の節の深さが食い違います。%s は %s、%s は %s です。",
			i+1, readmeEN, en[i], readmeJA, ja[i])
	}
}

// TestDesign_訳語集が引くREADMEの文が実在する は、README を直したのに訳語集を直し忘れたことを弾く。
//
// **訳語集の3列目は README の文を典拠として引いている。**README の文言を変えると、
// 引いたほうが古いまま残る。**古い典拠は、次に README を書く人を間違った語へ導く。**
// 実際に #127（英語版 README と en.json の board を kanban board に統一した変更を、
// マージ後にレビューする）の置き換えで、`board order` を引いた行が取り残された。
//
// **引用の実在だけを見る。**訳語そのものが妥当かは人が決めることなので、機械は見ない。
//
// 目的: 訳語集が `README の "…"` `README が "…"` の形で引く文が、いまも README にあること。
// 与える情報: docs/spec/translation-glossary.md の全行と、2枚の README の全文。
// 成功条件: 引いた文が README.md か README.ja.md のどちらかに現れること。
func TestDesign_訳語集が引くREADMEの文が実在する(t *testing.T) {
	glossary := readRepoFile(t, glossaryPath)
	en := readRepoFile(t, readmeEN)
	ja := readRepoFile(t, readmeJA)

	found := 0
	for i, line := range strings.Split(glossary, "\n") {
		for _, quote := range readmeQuotes(line) {
			found++
			if strings.Contains(en, quote) || strings.Contains(ja, quote) {
				continue
			}
			t.Errorf("%s:%d が引く README の文が見つかりません。\n  \"%s\"\n"+
				"  **README を直したら、その文を引いている訳語集の行も直してください。**",
				glossaryPath, i+1, quote)
		}
	}
	if found == 0 {
		t.Fatalf("%s から README の引用を1つも読めませんでした。テストの走査が壊れています。",
			glossaryPath)
	}
}

// readmeQuotes は、1行の中の `README の "…"` `README が "…"` から引用の中身だけを取り出す。
//
// **`README の ` に続く `"` から次の `"` までを1つの引用として扱う。**
// 訳語集はここで必ず半角の `"` を使っている。バッククォートで囲んだ語
// （`# start the daemon` など）は README の文ではなくコードなので拾わない。
//
// line: 訳語集の1行。
// 戻り値: 引用の中身の並び。1つも無ければ空。
func readmeQuotes(line string) []string {
	quotes := make([]string, 0, 2)
	for _, lead := range []string{`README の "`, `README が "`} {
		rest := line
		for {
			at := strings.Index(rest, lead)
			if at < 0 {
				break
			}
			rest = rest[at+len(lead):]
			end := strings.Index(rest, `"`)
			if end < 0 {
				break
			}
			quotes = append(quotes, rest[:end])
			rest = rest[end+1:]
		}
	}
	return quotes
}

// readRepoFile はリポジトリの直下からの相対パスでファイルを読む。
//
// t: テスト。
// name: リポジトリの直下からのパス。
// 戻り値: ファイルの中身。
func readRepoFile(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("..", "..", "..", name))
	if err != nil {
		t.Fatalf("%s を読めません: %v", name, err)
	}
	return string(body)
}

// readmeLines は README を1行ずつ読む。
//
// t: テスト。
// name: リポジトリの直下からのファイル名。
// 戻り値: 行の並び。
func readmeLines(t *testing.T, name string) []string {
	t.Helper()

	path := filepath.Join("..", "..", "..", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s を読めません: %v", name, err)
	}
	return strings.Split(string(body), "\n")
}

// headingLevels は README の見出しの深さを、出てくる順に並べる。
//
// **`#` 1つ（題名）は数えない。**両方の README に1つずつしか無く、比較の役に立たない。
// **コードブロックの中の `#` はコメントなので数えない。**シェルの例に `# start the daemon`
// のような行があり、見出しと区別が付かない。
//
// t: テスト。
// name: リポジトリの直下からのファイル名。
// 戻り値: `"##"` / `"###"` の並び。
func headingLevels(t *testing.T, name string) []string {
	t.Helper()

	levels := make([]string, 0, 16)
	inFence := false
	for _, line := range readmeLines(t, name) {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		switch {
		case strings.HasPrefix(line, "### "):
			levels = append(levels, "###")
		case strings.HasPrefix(line, "## "):
			levels = append(levels, "##")
		}
	}
	return levels
}
