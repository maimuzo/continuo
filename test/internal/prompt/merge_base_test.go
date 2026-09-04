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

	// **身元ファイルを1番に置くことが、この検査の主目的である。**
	// **4-4 は issue をまたいで同じ文言だが、身元ファイルは worktree ごとに continuo が書く。**
	// 4-4 を上に置くと、`herdr.worktree.base` を設定していない利用者が
	// 「分岐元は develop です」と本文へ書いただけで、**既定 branch から切った worktree へ
	// develop の履歴が丸ごと入る。**衝突しないので、誰も気づかない。
	for _, want := range []string{
		`1. worktree の直下にある continuo の身元ファイル（既定 ` + "`.continuo.json`" + `）の "base" の値`,
		"2. 4-4 に指定があれば、それ",
		"4. このリポジトリの既定 branch",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("分岐元の決め方に %q がありません（issue #214）", want)
		}
	}
	if strings.Index(body, "1. worktree の直下にある continuo の身元ファイル") >
		strings.Index(body, "2. 4-4 に指定があれば、それ") {
		t.Error("4-4 の指定が身元ファイルより上にあります（issue #214）")
	}

	// **`{{.push_branch}}` を地の文へ裸で置かない。**
	// **変数展開したあとに日本語として壊れる。**リンクがあれば
	// 「work/issue-42 が空でなければ、その名前」、無ければ「 が空でなければ、その名前」になる。
	if strings.Contains(body, "{{.push_branch}} が空でなければ") {
		t.Error("`{{.push_branch}}` を地の文へ裸で置いています。" +
			"展開後に日本語として壊れます。`{{if .push_branch}}` で囲むこと")
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
	// **付けるのではなく、付いていたら外す。**`git fetch origin <名前>` の `<名前>` は
	// remote 側の branch 名であり、**`git fetch origin origin/main` は
	// `couldn't find remote ref origin/main` で落ちる**（2026-09-05 に実測）。
	// **身元ファイルの `base` は、リンクされた branch のときだけ `origin/` が付く**
	// （`internal/workspace/prepare.go` の `resolveBase`）。
	if !strings.Contains(body, "決まった名前が `origin/` で始まっていたら、`origin/` を外してから取ってきます") {
		t.Error("`origin/` を外す規則がありません（issue #214）。" +
			"付ける向きで書くと、git fetch origin origin/main が毎回落ちます")
	}
	if strings.Contains(body, "`origin/` を前に付けて") {
		t.Error("`origin/` を前に付ける、と書かれています。向きが逆です（issue #214）")
	}

	// **`git fetch` へ辿れること。**取り込み方そのものは 7-1 が持っており、
	// **コマンドを二重に書かない**（片方だけ直すと指示が食い違う）。
	if !strings.Contains(body, "取り込み方は 7-1 と同じ2つのコマンドです") {
		t.Error("分岐元の取り込み方が 7-1 を指していません（issue #214）")
	}
	if !strings.Contains(body, "git fetch origin <その branch>") {
		t.Error("7-1 に git fetch のコマンドがありません（指す先が消えています）")
	}

	// **落ちた場所で扱いを分ける。**
	//
	// **取ってくるところで落ちたら、取り込むものが無いだけである。**`blocked` にする理由が無い。
	// そこで止めると、分岐元が remote に無い利用者（`herdr.worktree.base` にローカルの
	// branch を書いた人など）は、**着手の1手目で毎回人間へ渡ることになる。**
	//
	// **マージで落ちたら、取り込む前へ戻す。**戻さずに `blocked` を出すと、
	// **3-4 が `blocked` の前に commit と push を求めるので、衝突の印ごと push される。**
	// **`git merge --abort` は、マージが始まったときにしか効かない。**
	// **commit していない変更があると、マージは始まる前に断られる**
	// （`Your local changes to the following files would be overwritten by merge`）。
	// そこで `--abort` を打つと `There is no merge to abort` で落ち、**変更も残ったままになる**
	// （2026-09-05 に、空の remote と clone を作って実測）。
	// **やり直しの試行は、前の試行が残した変更ごと worktree を使い回す。**
	// **そこで「commit も push もせずに `blocked`」と言うと、残っている作業を人間へそのまま渡す。**
	for _, want := range []string{
		"取ってくるところで落ちたとき",
		"そのまま次へ進んでください。",
		"**マージを始める前に断られたとき**",
		"先に commit してから、もう一度取り込んでください。",
		"**`git merge --abort` は打たないでください。**",
		"**マージの途中で衝突したとき**",
		"git merge --abort",
		"**戻さずに `blocked` を出さないでください。**",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("取り込めなかったときの扱いに %q がありません（issue #214）", want)
		}
	}
	if !strings.Contains(body, "取り込めなかったことを応答に書いて `CONTINUO-STATUS: blocked` を出してください") {
		t.Error("マージで落ちたときの行き先が書かれていません（issue #214）")
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
