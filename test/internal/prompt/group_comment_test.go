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
// （設計 3-26「なぜ印を `tracker.comments.marker` と分けるのか」）。
// 読まない定数を置くと、使われていないのに「continuo が見ている」と読める。
// **したがって、この印の正は組み込みのプロンプトだけであり、ここはその写しである。**
const groupMarker = "<!-- continuo:group -->"

// 目的: 組み込みのプロンプトが、グループの他の issue にも「何をしたか」を書かせることを固定する
// （#237（グループの代表を直しても、代表以外の issue には「Status を動かしました」の1行しか残らない）。
// 設計 3-26）。
//
// **なぜ要るか。**代表以外の issue に残るのは、continuo が書く「Status を動かしました」の1行だけである。
// **何が直ったのかを知っているのはエージェントだけで、continuo は代筆しない**（設計 3-25 / 3-29）。
// **この節が落ちると、代表以外を報告した人は、自分の issue を開いても何も分からない状態へ戻る。**
//
// 与える情報: prompt.Builtin() の、まとめて直したときの節。
// 成功条件: 書かせる指示・既にあるかを確かめる手順・書き足す手順・投稿する手順・
// 代表へリンクを並べる手順の5つが在ること。
func TestTemplate_組み込みのプロンプトはグループの各issueへ成果を書かせる(t *testing.T) {
	body := prompt.Builtin()

	if !strings.Contains(body, "\n"+groupHeading+"\n") {
		t.Fatalf("組み込みのプロンプトに %q の節がありません。"+
			"この節が無いと、代表以外の issue には「Status を動かしました」の1行しか残りません", groupHeading)
	}

	// 節の中身だけを見る。**本文の別の場所に同じ語があっても、この節が教えていることにはならない。**
	// とくに `gh issue comment` と `--method PATCH` は 5-3 の進捗報告の節にもあるので、
	// 全文への contains では素通りする。
	section := sectionOf(t, body, groupHeading)

	for _, want := range []struct {
		needle string
		why    string
	}{
		{groupMarker, "グループの成果報告だけに付く印がないと、" +
			"次に書くときに自分の成果報告を見つけられず、issue にコメントが積み上がります"},
		{".viewerDidAuthor", "自分が書いたものかを見ないと、" +
			"人間が書いたコメントを自分の成果報告と読み違えて書き潰します"},
		{".[-1:][]", "いちばん下の1件だけを取り出す書き方を渡さないと、" +
			"成果報告が1件も無い issue で jq が落ちます"},
		{"gh issue view", "既にあるかを確かめる手順を渡さないと、" +
			"エージェントは turn のたびに新しいコメントを投稿します"},
		{"--method PATCH", "書き足すコマンドを渡さないと、" +
			"エージェントは書き足す手段を知らないまま新しいコメントを投稿します"},
		{"gh issue comment", "投稿のしかたを書かないと、書けと言われても手段が分かりません"},
		{"pull request: ", "pull request の URL を書かせないと、" +
			"読む人はどの変更でその issue が直ったのかを辿れません"},
		{"相対パス", "手元の絶対パスは、エージェントが直接書くコメントでは縮められません。" +
			"利用者名と worktree の置き場所が、取り消せない形で公開の issue に残ります"},
	} {
		if !strings.Contains(section, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", groupHeading, want.needle, want.why)
		}
	}
}

// 目的: グループの成果報告の印が、エージェントの印とも進捗の印とも分かれていることを固定する
// （#237。設計 3-26「なぜ印を `tracker.comments.marker` と分けるのか」）。
//
// **なぜ要るか。**`FetchComments` は、gh の持ち主が書いた本文が `tracker.comments.marker` で
// 始まっていれば `IsAgent` を立て、`hasRunComment` は `StartedAt` より後の `IsAgent` を
// 1件でも見つければ真を返す。**エージェント・continuo・人間は同じ GitHub アカウントで投稿する**
// （設計 5-3l）。
// **番号を1つ書き間違えて、別の run が担当している issue へその印を付けて書くと、
// その run のセッションの復元も、書かせ直しも、`failCommentRecovery` も1つも走らなくなる。**
// **「別の run が担当中なら書かない」は Status の書き込みしか止められない。**書くのはエージェントである。
//
// 与える情報: prompt.Builtin() の、まとめて直したときの節。
// 成功条件: 投稿させる本文の印が `<!-- continuo:group -->` であり、
// エージェントの印と進捗の印を付けさせていないこと。理由も書いてあること。
func TestTemplate_グループの成果報告の印はエージェントの印と分かれている(t *testing.T) {
	section := sectionOf(t, prompt.Builtin(), groupHeading)

	// **投稿させる本文の先頭は、グループの印である。**
	if !strings.Contains(section, `--body "`+groupMarker) {
		t.Errorf("%q の節が、投稿する本文の先頭に %q を置かせていません。"+
			"別の印を付けさせると、その issue を担当している別の Claude Code の書かせ直しが黙って走らなくなります",
			groupHeading, groupMarker)
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

// 目的: グループの成果報告の対象から、いま作業している issue と Status の名前を外すことを固定する
// （#237。設計 3-26）。
//
// **なぜ要るか。**2つある。
//
// **1つ。**いま作業している issue を対象に含めると、3-7 が書く成果報告と合わせて
// **代表にコメントが2件付く。**設計 3-26 が決めた「代表へは新しいコメントを1件も増やさない」が崩れる。
//
// **2つ。**`In Review` と `Blocked` は `tracker.status_signal_map` と `tracker.failure_state` で
// 変えられる名前である。**節がその名前を書くと、列名を変えたボードで指示だけが古いまま残る。**
// エージェントが見るのは自分の表明の値（`review` / `blocked`）だけでよい。
//
// 与える情報: prompt.Builtin() の、まとめて直したときの節。
// 成功条件: 対象を絞る文があり、Status の既定の名前が1つも書かれていないこと。
func TestTemplate_グループの成果報告は対象を絞りStatus名を書かない(t *testing.T) {
	section := sectionOf(t, prompt.Builtin(), groupHeading)

	if !strings.Contains(section, "いま作業している issue は、ここでは書きません") {
		t.Errorf("%q の節が、いま作業している issue を対象から外していません。"+
			"外さないと、3-7 の成果報告と合わせて代表にコメントが2件付きます", groupHeading)
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
