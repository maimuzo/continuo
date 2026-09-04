// 組み込みのプロンプトが、作業を始める前に worktree の分岐元を取り込ませることの検査である
// （issue #214（エージェントが、分岐元に既に入っている変更を取り込まずに古いコードの上で作業する））。
//
// **外部へ1回も接続しない。**組み込みの全文を読むだけである。
package prompt_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/prompt"
)

// mergeSectionHeading は、分岐元を取り込ませる段の見出しである。
//
// **`## 3-1.` の番号ごと見る。**読む段より前に置くことがこの issue の要点であり、
// **番号が動いたら、それは並びが変わったということである。**
const mergeSectionHeading = "## 3-1. 分岐元を取り込み、読む"

// 目的: 組み込みのプロンプトが、読む段より前に worktree の分岐元を取り込ませることを固定する
// （issue #214）。
//
// **なぜ要るか。**worktree は issue に着手したときの分岐元から切られる。
// **その issue が時間をおいて再開されると、分岐元はその間に進んでいる。**
// 取り込ませないと、エージェントは**既に直っているものを直し直し**、
// **分岐元で構造が変わったファイルを、変わる前の形で書き換える。**
// その作業は pull request を出したあとのマージで捨てられる。
//
// **読む段（3-1）と計画の段（3-2）より前に置く。**あとに置くと、その2段が古いコードの上で終わる。
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 見出しがあり、それが計画の段（3-2）と実装の段（3-3）より前にあること。
func TestTemplate_組み込みのプロンプトは読む前に分岐元を取り込ませる(t *testing.T) {
	body := prompt.Builtin()

	at := strings.Index(body, mergeSectionHeading)
	if at < 0 {
		t.Fatalf("組み込みのプロンプトに %q がありません。"+
			"分岐元を取り込ませないと、エージェントは古いコードの上で作業します（issue #214）",
			mergeSectionHeading)
	}
	plan := strings.Index(body, "## 3-2. 計画を書き、レビューを受ける")
	impl := strings.Index(body, "## 3-3. 実装する")
	if plan < 0 || impl < 0 {
		t.Fatalf("計画の段か実装の段が見つかりません（plan=%d impl=%d）", plan, impl)
	}
	if !(at < plan && plan < impl) {
		t.Errorf("分岐元を取り込む段が、計画の段より後ろにあります（at=%d plan=%d impl=%d）。"+
			"後ろに置くと、読む段と計画の段が古いコードの上で終わります", at, plan, impl)
	}
}

// 目的: 分岐元の名前の決め方が4段そろっていることを固定する（issue #214）。
//
// **`herdr.worktree.base`（worktree を切る分岐元。既定 null）を設定した利用者では、
// 4-4 と `{{.push_branch}}` と既定 branch の3段では当たらない。**
// その worktree は設定した branch から切られているのに、**3段では既定 branch を引いてくる。**
// **衝突しなければ merge は成功するので、この issue と関係の無い差分が黙って入る。**
//
// **当てられる材料は worktree の中にある。**continuo は worktree の直下の身元ファイル
// （既定 `.continuo.json`。`workspace.identity_file` で名前を変えられる）へ、
// 実際に使った分岐元を `"base"` として書いている。
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 4段の手掛かりが全部あること。
func TestTemplate_分岐元の名前は4段で決まる(t *testing.T) {
	body := prompt.Builtin()

	// **段2 を落とさないことが、この検査の主目的である。**
	// 落とすと、`herdr.worktree.base` を設定した利用者で黙って別の branch が混ざる。
	for _, want := range []string{
		"1. 4-4 に指定があれば、それ",
		`2. worktree の直下にある continuo の身元ファイル（既定 ` + "`.continuo.json`" + `）の "base" の値`,
		"3. {{.push_branch}} が空でなければ、その名前",
		"4. このリポジトリの既定 branch",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("分岐元の決め方に %q がありません（issue #214）", want)
		}
	}

	// **`origin/{{.push_branch}}` と書かない。**
	// **変数展開したあとの本文に `origin/<リンクされた branch>` が現れると、
	// エージェントが push 先だと読み違える**（`.push_branch` は生の名前で渡す約束である。設計 3-22d）。
	// `test/internal/orchestrator/push_branch_prompt_test.go` の
	// `TestPrompt_リンクされたbranchの名前をpush_branchで渡す` が、それを本文の全体で見張っている。
	if strings.Contains(body, "origin/{{.push_branch}}") {
		t.Error("分岐元の決め方が `origin/{{.push_branch}}` を組み立てています。" +
			"変数展開後の本文に origin/<branch> が現れ、push 先と読み違えられます（設計 3-22d）")
	}
	if !strings.Contains(body, "決まった名前が `origin/` で始まっていなければ、`origin/` を前に付けてから取ってきます") {
		t.Error("`origin/` を前に付ける規則がありません（issue #214）")
	}

	// **`git fetch` へ辿れること。**取り込み方そのものは 7-1 が持っており、
	// **コマンドを二重に書かない**（片方だけ直すと指示が食い違う）。
	if !strings.Contains(body, "取り込み方は 7-1 と同じ2つのコマンドです") {
		t.Error("分岐元の取り込み方が 7-1 を指していません（issue #214）")
	}
	if !strings.Contains(body, "git fetch origin <その branch>") {
		t.Error("7-1 に git fetch のコマンドがありません（指す先が消えています）")
	}

	// **取り込めなかったときの行き先。**衝突したまま続けると、直したものがマージで捨てられる。
	if !strings.Contains(body, "取り込めなかったときは、直さずに `CONTINUO-STATUS: blocked` を出してください") {
		t.Error("取り込めなかったときの扱いが書かれていません（issue #214）")
	}
}

// 目的: 足した文面が、worktree の分岐元と pull request の base を言い分けていることを固定する
// （issue #214）。
//
// **`## 7-4.` の「base にする branch」は pull request の base という意味で使われている**
// （直前が `## 7-3. 別のリポジトリへ pull request を出すとき`）。
// **同じ語で2つのものを指すと、エージェントはどちらの話か決められない。**
//
// **身元ファイルのキーの名前としては `"base"` を使わざるをえない。**
// そのかわり、**それが 7-4 の「base にする branch」とは別だと明示する。**
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 言い分ける1行があり、7-4 の「base にする branch」も残っていること。
func TestTemplate_分岐元とPRのbaseを言い分けている(t *testing.T) {
	body := prompt.Builtin()

	if !strings.Contains(body, "7-4 が言う「base にする branch」（pull request の分岐元）とは別のものです") {
		t.Error("身元ファイルの \"base\" と、7-4 の「base にする branch」を言い分けていません（issue #214）")
	}
	if !strings.Contains(body, "base にする branch") {
		t.Error("7-4 の「base にする branch」が消えています（言い分ける相手が無くなっています）")
	}
}
