// 組み込みのプロンプトが、作業を始める前に worktree の分岐元を取り込ませることの検査である
// （issue #214（エージェントが、分岐元に既に入っている変更を取り込まずに古いコードの上で作業する））。
//
// **外部へ1回も接続しない。**組み込みの全文を読むだけである。
package prompt_test

import (
	"strings"
	"testing"
)

// mergeSectionHeading は、分岐元を取り込ませる段の見出しである。
//
// **見出しに番号は無い**（人間の指示で消した）。読む段より前に置くことがこの issue の要点なので、
// **並びは TestTemplate_組み込みのプロンプトは読む前に分岐元を取り込ませる が位置で見る。**
const mergeSectionHeading = "## 分岐元を取り込み、読む"

// 目的: 組み込みのプロンプトが、読む段より前に worktree の分岐元を取り込ませることを固定する
// （issue #214）。
//
// **なぜ要るか。**worktree は issue に着手したときの分岐元から切られる。
// **その issue が時間をおいて再開されると、分岐元はその間に進んでいる。**
// 取り込ませないと、エージェントは**既に直っているものを直し直し**、
// **分岐元で構造が変わったファイルを、変わる前の形で書き換える。**
// その作業は pull request を出したあとのマージで捨てられる。
//
// **読む段（「分岐元を取り込み、読む」の段4）と計画の段（「計画を書き、レビューを受ける」）より前に置く。**
// あとに置くと、その2段が古いコードの上で終わる。
//
// 与える情報: instructionsText(t) の全文。
// 成功条件: 見出しがあり、それが計画の段（「計画を書き、レビューを受ける」）と実装の段（「実装する」）より前にあること。
// **取り込む段が、同じ節の読む段より前にあること。**
func TestTemplate_組み込みのプロンプトは読む前に分岐元を取り込ませる(t *testing.T) {
	body := instructionsText(t)

	at := strings.Index(body, "\n"+mergeSectionHeading+"\n")
	if at < 0 {
		t.Fatalf("組み込みのプロンプトに %q がありません。"+
			"分岐元を取り込ませないと、エージェントは古いコードの上で作業します（issue #214）",
			mergeSectionHeading)
	}
	plan := strings.Index(body, "\n## 計画を書き、レビューを受ける\n")
	impl := strings.Index(body, "\n## 実装する\n")
	// **見出しから番号を消したので、節の中の段の並びも位置で見る。**
	section := sectionOf(t, body, mergeSectionHeading)
	if merge, read := strings.Index(section, "### 段3. 取り込む"), strings.Index(section, "### 段4. 読む"); merge < 0 || read < 0 || merge > read {
		t.Errorf("%q の中で、取り込む段（%d）が読む段（%d）より前にありません（issue #214）",
			mergeSectionHeading, merge, read)
	}
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
// 「このプロジェクトの決まり」と `<リンクされた branch>` と既定 branch の3段では当たらない。**
// その worktree は設定した branch から切られているのに、**3段では既定 branch を引いてくる。**
// **衝突しなければ merge は成功するので、この issue と関係の無い差分が黙って入る。**
//
// **当てられる材料は worktree の中にある。**continuo は worktree の直下の身元ファイル
// （既定 `.continuo.json`。`workspace.identity_file` で名前を変えられる）へ、
// 実際に使った分岐元を `"base"` として書いている。
//
// 与える情報: instructionsText(t) の全文。
// 成功条件: 4段の手掛かりが全部あること。
func TestTemplate_分岐元の名前は4段で決まる(t *testing.T) {
	body := instructionsText(t)

	// **身元ファイルを1番に置くことが、この検査の主目的である。**
	// **「このプロジェクトの決まり」は issue をまたいで同じ文言だが、身元ファイルは worktree ごとに continuo が書く。**
	// 「このプロジェクトの決まり」を上に置くと、`herdr.worktree.base` を設定していない利用者が
	// 「分岐元は develop です」と本文へ書いただけで、**既定 branch から切った worktree へ
	// develop の履歴が丸ごと入る。**衝突しないので、誰も気づかない。
	for _, want := range []string{
		`1. worktree の直下にある continuo の身元ファイル（既定 ` + "`.continuo.json`" + `）の "base" の値`,
		"2. 「このプロジェクトの決まり」に指定があれば、それ",
		"4. このリポジトリの既定 branch",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("分岐元の決め方に %q がありません（issue #214）", want)
		}
	}
	if strings.Index(body, "1. worktree の直下にある continuo の身元ファイル") >
		strings.Index(body, "2. 「このプロジェクトの決まり」に指定があれば、それ") {
		t.Error("「このプロジェクトの決まり」の指定が身元ファイルより上にあります（issue #214）")
	}

	// **`<リンクされた branch>` を地の文へ裸で置かない。**
	// **変数展開したあとに日本語として壊れる。**リンクがあれば
	// 「work/issue-42 が空でなければ、その名前」、無ければ「 が空でなければ、その名前」になる。
	if strings.Contains(body, "<リンクされた branch> が空でなければ") {
		t.Error("`<リンクされた branch>` を地の文へ裸で置いています。" +
			"展開後に日本語として壊れます。`{{if .push_branch}}` で囲むこと")
	}
	// **`origin/<リンクされた branch>` と書かない。**
	// **変数展開したあとの本文に `origin/<リンクされた branch>` が現れると、
	// エージェントが push 先だと読み違える**（`.push_branch` は生の名前で渡す約束である。設計 3-22d）。
	// `test/internal/orchestrator/push_branch_prompt_test.go` の
	// `TestPrompt_リンクされたbranchの名前をpush_branchで渡す` が、それを本文の全体で見張っている。
	if strings.Contains(body, "origin/<リンクされた branch>") {
		t.Error("分岐元の決め方が `origin/<リンクされた branch>` を組み立てています。" +
			"変数展開後の本文に origin/<branch> が現れ、push 先と読み違えられます（設計 3-22d）")
	}
	// **付けるのではなく、付いていたら外す。**`git fetch origin <名前>` の `<名前>` は
	// remote 側の branch 名であり、**`git fetch origin origin/main` は
	// `couldn't find remote ref origin/main` で落ちる**（2026-09-05 に実測）。
	// **身元ファイルの `base` は、リンクされた branch のときだけ `origin/` が付く**
	// （`internal/workspace/prepare.go` の `resolveBase`）。
	if !strings.Contains(body, "決まった名前が `origin/` で始まっていたら、`origin/` を外します") {
		t.Error("`origin/` を外す規則がありません（issue #214）。" +
			"付ける向きで書くと、git fetch origin origin/main が毎回落ちます")
	}
	if strings.Contains(body, "`origin/` を前に付けて") {
		t.Error("`origin/` を前に付ける、と書かれています。向きが逆です（issue #214）")
	}

	// **`git fetch` へ辿れること。**取り込み方そのものは「worktree と branch は切り替えない」が持っており、
	// **コマンドを二重に書かない**（片方だけ直すと指示が食い違う）。
	if !strings.Contains(body, "取り込みのコマンドは「implementing-and-pushing」の「worktree と branch は切り替えない」の2つと同じです") {
		t.Error("分岐元の取り込み方が「worktree と branch は切り替えない」を指していません（issue #214）")
	}
	if !strings.Contains(body, "git fetch origin <その branch>") {
		t.Error("「worktree と branch は切り替えない」に git fetch のコマンドがありません（指す先が消えています）")
	}

	// **落ちた場所で扱いを分ける。**
	//
	// **取ってくるところで落ちたら、取り込むものが無いだけである。**`blocked` にする理由が無い。
	// そこで止めると、分岐元が remote に無い利用者（`herdr.worktree.base` にローカルの
	// branch を書いた人など）は、**着手の1手目で毎回人間へ渡ることになる。**
	//
	// **マージで落ちたら、取り込む前へ戻す。**戻さずに `blocked` を出すと、
	// **「commit して push する」が `blocked` の前に commit と push を求めるので、衝突の印ごと push される。**
	// **`git merge --abort` は、マージが始まったときにしか効かない。**
	// **commit していない変更があると、マージは始まる前に断られる**
	// （`Your local changes to the following files would be overwritten by merge`）。
	// そこで `--abort` を打つと `There is no merge to abort` で落ち、**変更も残ったままになる**
	// （2026-09-05 に、空の remote と clone を作って実測）。
	// **やり直しの試行は、前の試行が残した変更ごと worktree を使い回す。**
	// **そこで「commit も push もせずに `blocked`」と言うと、残っている作業を人間へそのまま渡す。**
	// **起きたことごとの扱いは「分岐元を取り込み、読む」の段3 の表の行で書いてある。**
	// 行の頭（起きたこと）と、すること の欄を見る。
	for _, want := range []string{
		"| 取ってくるところで落ちた",
		"取り込むものがありません。段4へ進みます",
		"| マージを始める前に断られた",
		"commit してから取り込み直します。",
		"`git merge --abort` は打ちません",
		"| 衝突した | 衝突した箇所を全部読み",
		// **衝突のうち、両方の変更を残せるものは解いて進ませる**（issue #289）。
		// 中身を見ずに全部の衝突で止めると、両方の行を残すだけで解ける衝突でも人間の1往復を使う。
		"解いてよいのは、衝突した箇所の全部で、両方の branch の変更をそのまま残せるときだけです",
		// **印の検査は、解いたファイルに絞る。終了コードは見ない。**
		// `git diff --cached --check` は行末の空白でも 0 以外を返し、絞らないと分岐元が足した
		// `=======` だけの行にも当たる（2026-10-05 に git 2.54.0 で実測）。
		// 文面は見本の2行目（`git diff --cached --check`）を「2行目」と呼んで扱いを決めている。
		"git diff --cached --check -- <解いたファイル> | grep 'leftover conflict marker'",
		"2行目には、あなたが解いたファイルだけを渡します",
		"終了コードは見ません（行末の空白だけでも 0 以外になります）",
		"git commit --no-edit",
		// **迷ったら止まる。**以前は「判断に迷ったら止める」の節を指していたが、いまは同じ文の中に入っている。
		"1箇所でも次に当たるとき、迷ったとき、",
		"解かずに戻して止まります。",
		"git merge --abort",
		"**戻さずに `blocked` を出さないでください。**",
		// **戻しても、その手前で作った commit は残る。**
		// 「commit するものが無い」と言い切ると、エージェントは push を飛ばす。
		// **push していない commit が残ると `cleanup.require_pushed` が片付けを見送り、
		// 成果は worktree の中だけに閉じ込められる。**
		"**残っていたら push してください**",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("取り込めなかったときの扱いに %q がありません（issue #214）", want)
		}
	}
	if !strings.Contains(body, "どのファイルのどこを、なぜ決められなかったかを報告（「finishing-run」の「runの終了を宣言する」）に書いて、`CONTINUO-STATUS: blocked` を出します") {
		t.Error("マージで落ちたときの行き先が書かれていません（issue #214）")
	}
}

// 目的: 足した文面が、worktree の分岐元と pull request の base を言い分けていることを固定する
// （issue #214）。
//
// **「この指示書が決めていないこと」の「base にする branch」は pull request の base という意味で使われている**
// （直前が `## 別のリポジトリへ pull request を出すとき`）。
// **同じ語で2つのものを指すと、エージェントはどちらの話か決められない。**
//
// **身元ファイルのキーの名前としては `"base"` を使わざるをえない。**
// そのかわり、**それが「この指示書が決めていないこと」の「base にする branch」とは別だと明示する。**
//
// 与える情報: instructionsText(t) の全文。
// 成功条件: 言い分ける1行があり、「この指示書が決めていないこと」の「base にする branch」も残っていること。
func TestTemplate_分岐元とPRのbaseを言い分けている(t *testing.T) {
	body := instructionsText(t)

	if !strings.Contains(body, "この `\"base\"` は、「implementing-and-pushing」の「base にする branch」（pull request を向ける先）とは別のものです") {
		t.Error("身元ファイルの \"base\" と、「この指示書が決めていないこと」の「base にする branch」を言い分けていません（issue #214）")
	}
	if !strings.Contains(body, "base にする branch") {
		t.Error("「この指示書が決めていないこと」の「base にする branch」が消えています（言い分ける相手が無くなっています）")
	}
}

// 目的: 「この指示書が決めていないこと」の一覧に、worktree の分岐元が載っていることを固定する
// （issue #214）。
//
// **なぜ要るか。**「分岐元を取り込み、読む」の段2 は「「このプロジェクトの決まり」に指定があれば、それ」と言う。
// **ところが「この指示書が決めていないこと」の一覧に載っていないと、その指定がどんな見出しで書かれるかを誰も決めていない。**
// **「この指示書が決めていないこと」の「base にする branch」は pull request の分岐元で、別のものである**
// （「分岐元を取り込み、読む」が自分でそう書いている）。
// 載せないと、エージェントは一覧の別項目を段2 の指定として拾いかねない。
//
// 与える情報: instructionsText(t) の全文。
// 成功条件: 「この指示書が決めていないこと」の節の中に「この worktree の分岐元」があり、
// それが「分岐元を取り込み、読む」の段2 を指していること。
func TestTemplate_この指示書が決めていないことの一覧にworktreeの分岐元が載っている(t *testing.T) {
	body := instructionsText(t)

	// **囲みを見る sectionOf で切る。**見本は囲みの中に行頭から書いてあり、
	// `## ` で始まる行を持つ。そこで切ると、後ろの文への検査が黙って素通りする。
	// **この節は、項目ごとにスキルへ分かれた**（issue #288）。worktree の分岐元はスキル「starting-run」にある。
	section := sectionOf(t, body, "## この指示書が決めていないこと（この worktree の分岐元）") + "\n" +
		sectionOf(t, body, "## この指示書が決めていないこと")

	if !strings.Contains(section, "この worktree の分岐元（「分岐元を取り込み、読む」の段2）") {
		t.Errorf("「この指示書が決めていないこと」の一覧に worktree の分岐元がありません。"+
			"「分岐元を取り込み、読む」の段2 が「このプロジェクトの決まり」を見に行かせるのに、"+
			"どこにも一覧が無いことになります:\n%s", section)
	}
	// **「base にする branch」と混ざらないよう、別物だと書いてあること。**
	if !strings.Contains(section, "別のもの") {
		t.Errorf("「この指示書が決めていないこと」が、pull request の base と worktree の分岐元を別物だと書いていません:\n%s", section)
	}
}

// 目的: 「runの終了を宣言する」が、成果の報告を新しく1件投稿させることを固定する
// （issue #178（途中経過を1回書いたエージェントが最後の報告を忘れても、continuo が書き直させない））。
//
// **なぜ要るか。**continuo は、先頭が進捗報告の印で始まるコメントを、この run の成果の報告として
// 数えない（`handoff.StartsAsProgressReport`）。
// **一方「<進捗の間隔（分）>分以上黙らない」は「コメントは増やさないでください」と言う。**
// **素直に受け取って進捗報告のコメントへ書き足すと、書いてあるのに「書かれていない」と判定され、**
// **Claude Code が起動し直されてレートリミットの枠を1回ぶん余計に使う。**
// 書き直しを頼むときの文面には断りがあるが、**1回目に送る側にも要る。**守りは2つとも要る。
//
// 与える情報: instructionsText(t) の「runの終了を宣言する」の節。
// 成功条件: 「新しく1件」と、書き足すと数えられないことの両方が書いてあること。
func TestTemplate_runの終了を宣言するは成果の報告を新しく1件投稿させる(t *testing.T) {
	body := instructionsText(t)

	// **囲みを見る sectionUntilNextChapter で切る。**この節は章の最後なので `# ` で切る必要があるが、
	// 成果の報告の見本は囲みの中に `# <何をしたかを一言で>` を行頭から持つ。
	// 素朴に `"\n# "` で切ると、見本の題名で切れて、後ろの文への検査が全部素通りする。
	section := sectionUntilNextChapter(t, body, finishedHeading)

	if !strings.Contains(section, "新しく1件投稿してください") {
		t.Errorf("「runの終了を宣言する」が「新しく1件投稿してください」と言っていません。"+
			"「<進捗の間隔（分）>分以上黙らない」の「コメントは増やさないでください」を素直に受け取ると、"+
			"進捗報告へ書き足して、書いたのに数えられません:\n%s", section)
	}
	if !strings.Contains(section, "成果の報告として数えません") {
		t.Errorf("「runの終了を宣言する」が、書き足すと数えられないことを言っていません:\n%s", section)
	}
}
