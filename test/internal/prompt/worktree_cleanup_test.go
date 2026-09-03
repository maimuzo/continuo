package prompt_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/prompt"
)

// worktreeCleanupHeading は、自分で作った worktree の片付けを教える節の見出しである。
const worktreeCleanupHeading = "## 7-1. worktree と branch は切り替えない"

// 目的: continuo が送る組み込みのプロンプト が、エージェントに worktree の片付けを教えることを固定する
// （#147（continuo が起動するエージェントに、worktree の片付けを教える）。設計 5-3）。
//
// **設計文書との突き合わせでは、この条件を守れない。**
// TestTemplate_組み込みのプロンプトが設計5_3の本文と一致する は設計 5-3 の markdown ブロックと
// 組み込みのプロンプトを比べるものなので、**両方からこの節が同時に消えても通る。**
// そこで、設計文書を一切読まず、組み込みのプロンプトだけを見て条件を確かめる。
//
// **なぜ要るか。**continuo が片付けるのは continuo が用意した worktree だけである。
// エージェントが自分で足したものは消されず、**登録だけが残る。**
// `--force` で消させると、commit していない変更が確認なしに消える。
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 片付けの節があり、その節が消し方・消す前の確認・`--force`・`prune` の4つと、
// 消してよい範囲（自分が `git worktree add` に渡したパスだけ）を教えていること。
func TestTemplate_組み込みのプロンプトは自分で作ったworktreeを片付けさせる(t *testing.T) {
	body := prompt.Builtin()

	if !strings.Contains(body, "\n"+worktreeCleanupHeading+"\n") {
		t.Fatalf("組み込みのプロンプトに %q の節がありません。"+
			"エージェントが足した worktree は continuo の片付けでは落ちないので、"+
			"自分で消させないと登録だけが残ります", worktreeCleanupHeading)
	}

	// 節の中身だけを見る。本文の別の場所に同じ語があっても、この節が教えていることにはならない。
	section := worktreeCleanupSection(t, body)

	// #147 が挙げた4つと、消してよい範囲。どれも「無いと片付けが成立しない」ものである。
	// 消し方だけを書いても、確認を教えなければ commit していない変更ごと消える。
	for _, want := range []struct {
		needle string
		why    string
	}{
		{"git worktree remove", "消し方を書かないと、エージェントは消す手段を知らないまま終わります"},
		{"status --short", "commit していない変更を確かめさせないと、消したあとに取り戻せません"},
		{"log --oneline HEAD --not --remotes", "push していない commit を、その worktree の HEAD だけで確かめさせないと、" +
			"関係の無い branch に引っかかって消せなくなります"},
		{"`--force` は付けないでください", "`--force` は commit していない変更を確認なしに消します"},
		{"`git worktree prune` は片付けの手段ではありません", "`prune` を片付けだと思うと、実体が残ったまま終わります"},
		// 消してよい範囲を書かないと、continuo が別の issue のために用意した worktree も候補になる。
		// commit していない変更が無ければ `--force` 無しでも消えるので、確認も警告も出ない。
		{"消してよいのは自分が `git worktree add` に渡したパスだけです",
			"消してよい範囲を書かないと、エージェントは手元にある worktree を全部候補にします"},
	} {
		if !strings.Contains(section, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", worktreeCleanupHeading, want.needle, want.why)
		}
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
