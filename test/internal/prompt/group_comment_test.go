package prompt_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/prompt"
)

// groupHeading は、まとめて直したときのことを教える節の見出しである。
const groupHeading = "## 7-2. まとめて直したとき"

// groupMarker は、グループの他の issue へ書く成果報告だけに付く印である。
//
// **リテラルで書く。**`config` にこの印の定数は無い。**Go のコードが1バイトも読まないためである**
// （設計 6-27「なぜ印を `tracker.comments.marker` と分けるのか」）。
// 読まない定数を置くと、使われていないのに「continuo が見ている」と読める。
// **したがって、この印の正は組み込みのプロンプトだけであり、ここはその写しである。**
const groupMarker = "<!-- continuo:group -->"

// 目的: 組み込みのプロンプトが、グループの他の issue にも「何をしたか」を書かせることを固定する
// （#237（グループの代表を直しても、代表以外の issue には「Status を動かしました」の1行しか残らない）。
// 設計 6-27）。
//
// **なぜ要るか。**代表以外の issue に残るのは、continuo が書く「Status を動かしました」の1行だけである。
// **何が直ったのかを知っているのはエージェントだけで、continuo は代筆しない**（設計 3-25 / 3-29）。
// **この節が落ちると、代表以外を報告した人は、自分の issue を開いても何も分からない状態へ戻る。**
//
// 与える情報: prompt.Builtin() と prompt.Build() の、まとめて直したときの節。
// 成功条件: どちらでも節があり、既にあるかを確かめる手順・書き足す手順・投稿する手順・
// 代表へリンクを並べる手順が在ること。
func TestTemplate_組み込みのプロンプトはグループの各issueへ成果を書かせる(t *testing.T) {
	// **本文を挟んだものも見る**（既存の進捗報告の検査と同じ作法）。
	// 組み込みの前半・本文・後半を継ぎ合わせたあとに空の見出しを落とす処理（prompt.Build）を
	// 通しても、**中身を持つこの節が落ちてはならない。**
	for _, tc := range []struct {
		name string
		body string
	}{
		{"組み込みだけ", prompt.Builtin()},
		{"本文を挟んだもの", prompt.Build("### 固有の目印\n\n固有の中身です。\n", "/tmp/WORKFLOW.md").Text()},
	} {
		if !strings.Contains(tc.body, "\n"+groupHeading+"\n") {
			t.Fatalf("%s: 組み込みのプロンプトに %q の節がありません。"+
				"この節が無いと、代表以外の issue には「Status を動かしました」の1行しか残りません",
				tc.name, groupHeading)
		}

		// 節の中身だけを見る。**本文の別の場所に同じ語があっても、この節が教えていることにはならない。**
		// とくに `gh issue comment` と `--method PATCH` は 5-3 の進捗報告の節にもあるので、
		// 全文への contains では素通りする。
		section := sectionOf(t, tc.body, groupHeading)

		for _, want := range []struct {
			needle string
			why    string
		}{
			{groupMarker, "グループの成果報告だけに付く印がないと、" +
				"次に書くときに自分の成果報告を見つけられず、issue にコメントが積み上がります"},
			{".viewerDidAuthor", "自分が書いたものかを見ないと、" +
				"人間が書いたコメントを自分の成果報告と読み違えて書き潰します"},
			// **`.[-1]` で取らせてはならない。**空の配列に当てると `null` が返るので
			// （jq 1.8.1 で実測）、**エージェントはそれを URL と読んで書き足しへ進む。**
			// `.[-1:][]` は空のとき1文字も出さない（同じく実測）。
			{".[-1:][]", "いちばん下の1件だけを取り出す書き方を渡さないと、" +
				"成果報告が1件も無い issue で jq が null を返し、それを URL と読んで書き足しへ進みます"},
			{"gh issue view", "既にあるかを確かめる手順を渡さないと、" +
				"エージェントは turn のたびに新しいコメントを投稿します"},
			{"--method PATCH", "書き足すコマンドを渡さないと、" +
				"エージェントは書き足す手段を知らないまま新しいコメントを投稿します"},
			{"gh issue comment", "投稿のしかたを書かないと、書けと言われても手段が分かりません"},
			{"pull request: ", "pull request の URL を書かせないと、" +
				"読む人はどの変更でその issue が直ったのかを辿れません"},
			{"相対パス", "手元の絶対パスは、エージェントが直接書くコメントでは縮められません。" +
				"利用者名と worktree の置き場所が、取り消せない形で公開の issue に残ります"},
			// **段3 を守る。**ここを見ないと、段3 を丸ごと消しても全部緑になり、
			// **3-7 が「7-2 を先に通せ」と言った先に、並べる手順が無い状態になる。**
			{"に書きました: ", "代表へリンクを並べる手順が無いと、" +
				"代表を開いた人は、どの issue に何が書かれたのかを辿れません"},
		} {
			if !strings.Contains(section, want.needle) {
				t.Errorf("%s: %q の節に %q がありません。%s", tc.name, groupHeading, want.needle, want.why)
			}
		}
	}
}

// 目的: `blocked` を出した issue に、直したという記録を書かせないことを固定する
// （#237。設計 6-27「採る形」の表）。
//
// **なぜ要るか。**`blocked` は「判断を仰ぎたい、または失敗した」である
// （組み込みの 3-7）。**直していないことも、pull request が無いこともある。**
// **`review` と同じ書式を使わせると、直していない issue に「まとめて直しました」と
// 無い pull request の URL が残る。**
// **#237 の症状（その issue を開いても分からない）を、事実と違う記録という別の形で作り直すことになる。**
//
// 与える情報: prompt.Builtin() の、まとめて直したときの節。
// 成功条件: `blocked` のときの書式が別に用意されていて、
// そこで「まとめて直しました」と書かせないことを言っていること。
func TestTemplate_blockedのissueには直したという記録を書かせない(t *testing.T) {
	section := sectionOf(t, prompt.Builtin(), groupHeading)

	for _, want := range []struct {
		needle string
		why    string
	}{
		{"`blocked` を出した issue には、直せていません", "`blocked` の意味を言わないと、" +
			"エージェントは `review` と同じ書式を使い、直していない issue に「直しました」と書きます"},
		{"「まとめて直しました」と書かないでください", "打ち消さないと、" +
			"すぐ上にある `review` の書式をそのまま写します"},
		{"この issue は直していません", "`blocked` のときに書かせる本文が無いと、" +
			"エージェントは自分で書式を作ることになり、直したのか直していないのかが読み取れなくなります"},
		{"なぜ止まったか", "止まった理由を書かせないと、" +
			"人間はその issue を開いても、何を決めればよいのかが分かりません"},
		// **書き足す段（段2a）も分ける。**分けるのを新しく投稿する段だけにすると、
		// **成果報告が1件でも先にある issue では `blocked` でも `review` の書式へ流れ、
		// 無い pull request の URL が書き足される。**
		{"`blocked` を出した issue では、pull request の URL を書かないでください",
			"書き足す段を分けないと、先に成果報告がある issue で無い pull request の URL が足されます"},
	} {
		if !strings.Contains(section, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", groupHeading, want.needle, want.why)
		}
	}
}

// 目的: グループの成果報告の印が、エージェントの印とも進捗の印とも分かれていること、
// および段1 と段2b が同じ印を指していることを固定する
// （#237。設計 6-27「なぜ印を `tracker.comments.marker` と分けるのか」）。
//
// **なぜ要るか。**`FetchComments` は、gh の持ち主が書いた本文が `tracker.comments.marker` で
// 始まっていれば `IsAgent` を立て、`hasRunComment` は `StartedAt` より後の `IsAgent` を
// 1件でも見つければ真を返す。**エージェント・continuo・人間は同じ GitHub アカウントで投稿する**
// （設計 5-3l）。
// **番号を1つ書き間違えて、別の run が担当している issue へその印を付けて書くと、
// その run のセッションの復元も、書かせ直しも、`failCommentRecovery` も1つも走らなくなる。**
// **「別の run が担当中なら書かない」は Status の書き込みしか止められない。**書くのはエージェントである。
//
// **段1 と段2b の一致も見る。**片方だけ別の印へ書き換えると、
// **エージェントは自分の成果報告を毎 turn 見つけられず、「issue 1件につき成果報告1件」が崩れる。**
//
// 与える情報: prompt.Builtin() の、まとめて直したときの節。
// 成功条件: 投稿させる本文の印と、探させる印が同じ `<!-- continuo:group -->` であり、
// エージェントの印と進捗の印を付けさせていないこと。理由も書いてあること。
func TestTemplate_グループの成果報告の印はエージェントの印と分かれている(t *testing.T) {
	section := sectionOf(t, prompt.Builtin(), groupHeading)

	// **投稿させる本文は2つある**（`review` 用と `blocked` 用）。**両方の先頭がグループの印である。**
	// **件数で見る。**`Contains` を1回叩くだけだと、**片方を第三の印へ書き換えても通る。**
	posts := strings.Count(section, `--body "`)
	marked := strings.Count(section, `--body "`+groupMarker)
	if posts < 2 {
		t.Fatalf("%q の節に、投稿する本文が %d 個しかありません。"+
			"`review` と `blocked` で書式を分けているので2つ以上あるはずです（検査が的を外しています）",
			groupHeading, posts)
	}
	if marked != posts {
		t.Errorf("%q の節で、投稿する本文 %d 個のうち %d 個しか %q で始まっていません。"+
			"別の印を付けさせると、その issue を担当している別の Claude Code の書かせ直しが黙って走らなくなります",
			groupHeading, posts, marked, groupMarker)
	}

	// **探させる印も、同じものである。**段1 の jq と段2a の門の両方を見る。
	// **片方だけ書き換えると、書いた成果報告を二度と見つけられない。**
	for _, want := range []string{
		`startswith("` + groupMarker + `")`,
		`*"` + groupMarker + `"*`,
	} {
		if !strings.Contains(section, want) {
			t.Errorf("%q の節に %q がありません。"+
				"投稿する印と探す印がずれると、エージェントは自分の成果報告を見つけられず、"+
				"turn のたびに新しいコメントを投稿します", groupHeading, want)
		}
	}

	// **エージェントの印を投稿させてはならない。**節の中で言及するのは構わないが、
	// **投稿する本文の先頭に置かせてはならない。**
	agentMarker := config.DefaultConfig().Tracker.Comments.Marker
	if agentMarker == "" {
		t.Fatal("既定の tracker.comments.marker が空です（検査が素通りします）")
	}
	if strings.Contains(section, `--body "`+agentMarker) {
		t.Errorf("%q の節が、投稿する本文の先頭に %q を置かせています。"+
			"その印は「いま担当している issue のエージェントが書いた」という意味で、"+
			"continuo が書かせ直しの要否を決めるのに使っています", groupHeading, agentMarker)
	}

	// **進捗の印も付けさせてはならない。**付けると、次の進捗報告がこの成果報告へ書き足す。
	if strings.Contains(section, `--body "`+config.ProgressMarker) {
		t.Errorf("%q の節が、投稿する本文の先頭に %q を置かせています。"+
			"付けると、次の進捗報告がこの成果報告に書き足します", groupHeading, config.ProgressMarker)
	}

	// **分けた理由が書いてあること。**理由が無いと、あとから「印は1つでよい」と揃えられる。
	if !strings.Contains(section, "書かせ直し") {
		t.Errorf("%q の節が、印を分ける理由を言っていません。"+
			"理由が無いと、あとから %q へ揃えられ、別の run の書かせ直しが黙って走らなくなります",
			groupHeading, agentMarker)
	}
}

// 目的: 書き換えのコマンドが、印を確かめる門の中に在ることを固定する（#237。設計 6-27）。
//
// **なぜ要るか。**`gh api` は取得に失敗したとき、エラーの JSON を標準出力へ出す。
// **`$OLD` は空にならない。**そのまま書き換えると `<!-- continuo:group -->` の印ごと本文が消え、
// **段1 の `startswith` がその issue で永久に何も返さなくなる。**
// **turn のたびに段2b が走り、成果報告が積み上がる。**
// #237 の受け入れ条件「同じ内容が2回書かれない」を、経路を変えて破る。
//
// **`--method PATCH` が節の中に在るかを見るだけでは足りない。**
// **門の外に置いても、その検査は通る**（実際に3周目の直しで一度そうなった）。
//
// 与える情報: prompt.Builtin() の、まとめて直したときの節。
// 成功条件: `--method PATCH` を含む行より前に印を見る `case` の枝があり、
// その枝から `esac` までの間に `--method PATCH` が在ること。
func TestTemplate_書き換えは印を確かめる門の中で行わせる(t *testing.T) {
	section := sectionOf(t, prompt.Builtin(), groupHeading)

	patch := strings.Index(section, "--method PATCH")
	if patch < 0 {
		t.Fatalf("%q の節に %q がありません（検査が的を外しています）", groupHeading, "--method PATCH")
	}

	// **書き換えの直前に、印を見る枝がある。**`case` の枝は `*"<印>"*)` の形である。
	gate := strings.LastIndex(section[:patch], `*"`+groupMarker+`"*)`)
	if gate < 0 {
		t.Fatalf("%q の節で、書き換えのコマンドより前に %q の枝がありません。"+
			"読み取りに失敗したまま書き換えると、印ごと本文が消えて、"+
			"段1 がその成果報告を二度と見つけられなくなります", groupHeading, `*"`+groupMarker+`"*)`)
	}

	// **その枝が閉じる前に書き換えている。**枝の中に `esac` が挟まっていたら、門の外である。
	if end := strings.Index(section[gate:], "esac"); end >= 0 && gate+end < patch {
		t.Errorf("%q の節で、書き換えのコマンドが %q の枝の外にあります。"+
			"`gh api` は取得に失敗するとエラーの JSON を出すので、"+
			"門の外で書き換えると印ごと本文が消えます", groupHeading, `*"`+groupMarker+`"*)`)
	}

	// **`$ID` を作る塊は、どれも `$URL` を自分で置いている。**
	// **道具は塊ごとに別のシェルで走るので、変数は持ち越されない。**
	// `URL=` を落とすと `ID` が空になり、`gh api` が失敗して段2b が2件目を投稿する。
	const setURL, deriveID = "URL=<段1が返した URL>", "ID=${URL##*#issuecomment-}"
	if got, want := strings.Count(section, setURL), strings.Count(section, deriveID); got != want {
		t.Errorf("%q の節で、%q が %d 個、%q が %d 個です。"+
			"塊ごとに別のシェルで走るので、`$ID` を作る塊は自分で `$URL` を置かないと空になり、"+
			"書き足しに失敗して段2b が2件目の成果報告を投稿します",
			groupHeading, setURL, got, deriveID, want)
	}
}

// 目的: グループの成果報告の対象から、いま作業している issue と Status の名前を外すことを固定する
// （#237。設計 6-27）。
//
// **なぜ要るか。**2つある。
//
// **1つ。**いま作業している issue を対象に含めると、3-7 が書く成果報告と合わせて
// **代表にコメントが2件付く。**設計 6-27 が決めた「代表へは新しいコメントを1件も増やさない」が崩れる。
//
// **2つ。**`In Review` と `Blocked` は `tracker.status_signal_map` と `tracker.failure_state` で
// 変えられる名前である。**節がその名前を書くと、列名を変えたボードで指示だけが古いまま残る。**
// エージェントが見るのは自分の表明の値（`review` / `blocked`）だけでよい。
//
// 与える情報: prompt.Builtin() の、まとめて直したときの節。
// 成功条件: 対象を絞る文があり、Status の既定の名前が1つも書かれていないこと。
func TestTemplate_グループの成果報告は対象を絞りStatus名を書かない(t *testing.T) {
	section := sectionOf(t, prompt.Builtin(), groupHeading)

	for _, want := range []struct {
		needle string
		why    string
	}{
		{"いま作業している issue は、ここでは書きません",
			"外さないと、3-7 の成果報告と合わせて代表にコメントが2件付きます"},
		{"`working` を出した issue も書きません",
			"まだ終わっていない issue に成果報告を書かせると、途中の状態が成果として残ります"},
		// **別のリポジトリの issue を外す行も見る。**段2b は
		// `--repo {{.issue.owner}}/{{.issue.repo}}` を直に書いているので、
		// **外れると、同じ番号の別のリポジトリの issue へ書き込む。**
		{"別のリポジトリの issue も書きません",
			"段2b は代表と同じリポジトリを直に指すので、外すのをやめると、同じ番号の別の issue へ書き込みます"},
	} {
		if !strings.Contains(section, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", groupHeading, want.needle, want.why)
		}
	}

	// **Status の名前を1文字も書かないこと。**既定の値を設定から引いて確かめる。
	cfg := config.DefaultConfig()
	names := []string{cfg.Tracker.FailureState}
	for _, next := range cfg.Tracker.StatusSignalMap {
		if next != nil {
			names = append(names, *next)
		}
	}
	if len(names) < 2 {
		t.Fatalf("既定の Status の名前を引けません（検査が素通りします）: %v", names)
	}
	for _, notWant := range names {
		if notWant == "" {
			continue
		}
		if strings.Contains(section, notWant) {
			t.Errorf("%q の節に Status の名前 %q が書かれています。"+
				"その名前は tracker.status_signal_map と tracker.failure_state で変えられるので、"+
				"列名を変えたボードでは指示だけが古いまま残ります", groupHeading, notWant)
		}
	}
}

// 目的: 終わりを書く節が、グループの成果報告を先に通させることを固定する（#237。設計 6-27）。
//
// **なぜ要るか。**7-2 の段3 は「3-7 で代表へ書く成果報告の中に URL を並べる」と言う。
// **3-7 を上から読んで先にコメントを投稿してしまうと、並べる先が無くなる。**
// そこで取れる手は、代表へ2件目を投稿するか、投稿済みを編集するかの2つで、
// **前者は設計 6-27 の「代表へは新しいコメントを1件も増やさない」が禁じている状態そのものである。**
//
// **この検査は 7-2 の節ではなく 3-7 の節を見る。**7-2 だけを見る検査では、
// **3-7 の案内が1行残らず消えても全部緑になる。**
//
// 与える情報: prompt.Builtin() の、終わりを書く節。
// 成功条件: 7-2 を先に通させる案内があり、それが投稿するコマンドより前に在ること。
func TestTemplate_終わりを書く節はグループの成果報告を先に通させる(t *testing.T) {
	section := sectionOf(t, prompt.Builtin(), finishedHeading)

	const guide = "下のコメントを書く前に 7-2 を通してください"
	at := strings.Index(section, guide)
	if at < 0 {
		t.Fatalf("%q の節に %q がありません。"+
			"先にこのコメントを投稿すると、7-2 の段3 が URL を並べる先を失います", finishedHeading, guide)
	}
	post := strings.Index(section, "gh issue comment")
	if post < 0 {
		t.Fatalf("%q の節に投稿するコマンドがありません（検査が的を外しています）", finishedHeading)
	}
	if at > post {
		t.Errorf("%q の節で、%q が投稿するコマンドより後ろにあります。"+
			"上から読んだエージェントは、投稿し終えてから「その中に URL を並べろ」と言われます",
			finishedHeading, guide)
	}
}
