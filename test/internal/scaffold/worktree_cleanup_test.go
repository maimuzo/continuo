package scaffold_test

import (
	"os"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/scaffold"
)

// worktreeCleanupHeading は、自分で作った worktree の片付けを教える節の見出しである。
const worktreeCleanupHeading = "## 自分で作った worktree は、自分で消すこと"

// 目的: `continuo init` が置く WORKFLOW.md が、エージェントに worktree の片付けを教えることを固定する
// （#147（continuo が起動するエージェントに、worktree の片付けを教える）。設計 5-3）。
//
// **設計文書との突き合わせでは、この条件を守れない。**
// TestTemplate_雛形の本文が設計5_3の本文と一致する は設計 5-3 の markdown ブロックと
// 雛形を比べるものなので、**両方からこの節が同時に消えても通る。**
// そこで、設計文書を一切読まず、雛形の本文だけを見て条件を確かめる。
//
// **なぜ要るか。**continuo が片付けるのは continuo が用意した worktree だけである。
// エージェントが自分で足したものは消されず、**登録だけが残る。**
// `--force` で消させると、commit していない変更が確認なしに消える。
//
// 与える情報: scaffold.Template() の本文。
// 成功条件: 片付けの節があり、その節が消し方・消す前の確認・`--force`・`prune` の4つを教えていること。
func TestTemplate_雛形は自分で作ったworktreeを片付けさせる(t *testing.T) {
	body := bodyOf(t, "雛形", scaffold.Template())

	if !strings.Contains(body, "\n"+worktreeCleanupHeading+"\n") {
		t.Fatalf("雛形の本文に %q の節がありません。"+
			"エージェントが足した worktree は continuo の片付けでは落ちないので、"+
			"自分で消させないと登録だけが残ります", worktreeCleanupHeading)
	}

	// 節の中身だけを見る。本文の別の場所に同じ語があっても、この節が教えていることにはならない。
	section := worktreeCleanupSection(t, body)

	// 4つとも「無いと片付けが成立しない」ものである。
	// 消し方だけを書いても、確認を教えなければ commit していない変更ごと消える。
	for _, want := range []struct {
		needle string
		why    string
	}{
		{"git worktree remove", "消し方を書かないと、エージェントは消す手段を知らないまま終わります"},
		{"status --short", "commit していない変更を確かめさせないと、消したあとに取り戻せません"},
		// --branches にすると、その worktree とは関係の無い別の branch の commit まで出る。
		// HEAD で絞らないと、完全に push 済みの worktree でも必ず引っかかる。
		{"log --oneline HEAD --not --remotes", "push していない commit を、その worktree の HEAD だけで確かめさせないと、" +
			"関係の無い branch に引っかかって消せなくなります"},
		{"--force を付けないでください", "`--force` は commit していない変更を確認なしに消します"},
		{"git worktree prune は片付けの手段ではありません", "`prune` を片付けだと思うと、実体が残ったまま終わります"},
		// 一覧から選ばせると、continuo が別の issue のために用意した worktree が候補に並ぶ。
		// commit していない変更が無ければ `--force` 無しでも消えるので、確認も警告も出ない。
		{"消してよいのは、あなた自身が git worktree add で作った worktree だけです",
			"消してよい範囲を書かないと、エージェントは手元にある worktree を全部候補にします"},
		{"git worktree list で一覧を出して、そこから消すものを選ばないでください",
			"一覧から選ばせると、別のエージェントが使っている worktree を消します"},
		{"自分で git worktree add した覚えが無いなら、1つも消さないでください",
			"迷ったときに何もしないと決めさせないと、エージェントは1つ選んで消します"},
	} {
		if !strings.Contains(section, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", worktreeCleanupHeading, want.needle, want.why)
		}
	}

	// git log --branches は repository の全 local branch を見る。-C でパスを渡しても対象は絞られない。
	// 完全に push 済みの worktree でも、手元に古い branch が1本あるだけで必ず引っかかり、
	// エージェントは無関係な branch を push しにいくか、消せないまま作業を終える。
	if strings.Contains(section, "log --branches") {
		t.Errorf("%q の節が git log --branches を教えています。"+
			"これは repository の全 local branch を見るので、その worktree が push 済みでも"+
			"手元に別の branch が残っているだけで引っかかります。"+
			"HEAD で絞った log --oneline HEAD --not --remotes を書いてください", worktreeCleanupHeading)
	}

	// 「一覧から消したいものを選ぶ」という前提が、パスの表記に出てはならない。
	if strings.Contains(section, listPickingPathPlaceholder) {
		t.Errorf("%q の節が %q という表記を使っています。"+
			"この書き方は「一覧から消したいものを選ぶ」ことを前提にしています。"+
			"自分が git worktree add に渡したパスだけを指す表記に直してください",
			worktreeCleanupHeading, listPickingPathPlaceholder)
	}
}

// worktreeCleanupSection は、本文から片付けの節（見出しから次の見出しの手前まで）を取り出す。
//
// t: テストコンテキスト。
// body: WORKFLOW.md の本文（front matter より後ろ）。
// 戻り値: 見出しの次の行から、次の "## " で始まる行の手前までの中身。
func worktreeCleanupSection(t *testing.T, body string) string {
	t.Helper()

	lines := strings.Split(body, "\n")
	start := -1
	for i, line := range lines {
		if line == worktreeCleanupHeading {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("本文から %q の見出しを取り出せません", worktreeCleanupHeading)
	}
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			return strings.Join(lines[start:i], "\n")
		}
	}
	return strings.Join(lines[start:], "\n")
}

// upgradingDocPath は既存の利用者向けの案内の場所である。Go のテストはパッケージの
// ディレクトリを作業ディレクトリとして走るので、test/internal/scaffold からの相対パスで指す。
const upgradingDocPath = "../../../docs/upgrading.md"

// upgradingPasteHeading は、既存の利用者が WORKFLOW.md へ貼る文面が載っている節の見出しである。
//
// **版を出すたびに、この定数を差し替える。**docs/upgrading.md は版ごとに節を積む文書であり
// （docs/releasing.md の「5. 版ごとの節を書く」）、**打った版の節はもう書き換えない。**
// 雛形のこの節を直したら、新しい版の節を docs/upgrading.md の先頭側へ足し、
// **この定数と upgradingCheckHeading を、その新しい節の見出しへ向けること。**
// **古い版の節を書き換えて通してはならない。**その版から上げる利用者が貼る文面が変わる。
const upgradingPasteHeading = "### 差し替え方（自分で作った worktree は自分で消せ）"

// upgradingCheckHeading は、貼れたかどうかを grep で確かめる手順が載っている節の見出しである。
// upgradingPasteHeading と同じ版の節にあるので、差し替えるときは2つ一緒に動かす。
const upgradingCheckHeading = "### 当たったかどうかの確かめ方（自分で作った worktree は自分で消せ）"

// listPickingPathPlaceholder は「一覧から消したいものを選ぶ」ことを前提にしたパスの表記である。
// 消してよいのは自分が git worktree add に渡したパスだけなので、雛形にこの表記を置かない。
const listPickingPathPlaceholder = "<消したい worktree のパス>"

// 目的: docs/upgrading.md が貼らせる文面と、`continuo init` が置く雛形の当該節が
// 一字一句そろっていることを固定する（#147（continuo が起動するエージェントに、worktree の片付けを教える））。
//
// **なぜ要るか。**docs/upgrading.md の ```text ブロックは、既存の利用者が
// WORKFLOW.md へそのまま貼る文面である。**雛形だけを直しても、既存の利用者には届かない。**
// **食い違ったまま出すと、上げた利用者のエージェントだけが、
// 別のエージェントが使っている worktree を消しにいく案内で動く。**
//
// 与える情報: docs/upgrading.md の ```text ブロックと、scaffold.Template() の当該節。
// 成功条件: 2つが完全に一致し、確かめ方の grep が、この節でいちばん効く2行を数えていること。
func TestTemplate_案内が貼らせる文面が雛形の節と一致する(t *testing.T) {
	body := bodyOf(t, "雛形", scaffold.Template())
	want := strings.TrimRight(worktreeCleanupHeading+"\n"+worktreeCleanupSection(t, body), "\n")

	doc := readUpgradingDoc(t)
	got := strings.TrimRight(fencedBlockAfter(t, doc, upgradingPasteHeading, "```text"), "\n")

	if got != want {
		t.Errorf("%s の %q が貼らせる文面が、雛形の %q の節と違います。\n"+
			"貼らせる文面:\n%s\n\n雛形の節:\n%s\n\n"+
			"既存の利用者はこのブロックをそのまま貼ります。片方だけ直すと、"+
			"上げた利用者のエージェントだけが古い案内で動きます。\n"+
			"雛形を直して版を上げるのなら、いま指している節は書き換えず、"+
			"新しい版の節を %s へ足して、upgradingPasteHeading をその見出しへ向けてください",
			upgradingDocPath, upgradingPasteHeading, worktreeCleanupHeading, got, want,
			upgradingDocPath)
	}

	// 見出しのように、途中で切れて貼られても残る文字列しか数えていないと、
	// 貼り損ねたままでも「当たっています」になる。だから、この節でいちばん効く2行を数えさせる。
	check := fencedBlockAfter(t, doc, upgradingCheckHeading, "```bash")
	for _, needle := range []string{
		"git worktree list で一覧を出して、そこから消すものを選ばないでください",
		"log --oneline HEAD --not --remotes",
	} {
		if !strings.Contains(check, needle) {
			t.Errorf("%s の確かめ方が %q を数えていません。"+
				"見出しの行は途中で切れて貼られていても 1 を返すので、"+
				"それだけでは貼り損ねを見つけられません",
				upgradingDocPath, needle)
		}
	}
}

// readUpgradingDoc は docs/upgrading.md を読む。
//
// t: テストコンテキスト。
// 戻り値: docs/upgrading.md の中身。
func readUpgradingDoc(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile(upgradingDocPath)
	if err != nil {
		t.Fatalf("%s を読めません: %v", upgradingDocPath, err)
	}
	return string(raw)
}

// fencedBlockAfter は、見出しの後ろに最初に現れる指定の fence のブロックの中身を返す。
//
// t: テストコンテキスト。
// doc: 対象の markdown。
// heading: 探し始める見出しの行。
// fence: 開き fence の行（"```text" など）。
// 戻り値: 開き fence の次の行から、閉じ fence の手前までの中身。
func fencedBlockAfter(t *testing.T, doc, heading, fence string) string {
	t.Helper()

	lines := strings.Split(doc, "\n")
	start := -1
	for i, line := range lines {
		if line == heading {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s に %q の見出しがありません", upgradingDocPath, heading)
	}

	open := -1
	for i := start; i < len(lines); i++ {
		if lines[i] == fence {
			open = i + 1
			break
		}
		// 次の見出しまで来たら、その節には目的の fence が無い。
		if strings.HasPrefix(lines[i], "### ") || strings.HasPrefix(lines[i], "## ") {
			break
		}
	}
	if open < 0 {
		t.Fatalf("%s の %q の節に %s のブロックがありません", upgradingDocPath, heading, fence)
	}

	for i := open; i < len(lines); i++ {
		if lines[i] == "```" {
			return strings.Join(lines[open:i], "\n")
		}
	}
	t.Fatalf("%s の %q の節の %s が閉じていません", upgradingDocPath, heading, fence)
	return ""
}
