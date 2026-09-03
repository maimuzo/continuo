package prompt_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/prompt"
)

// progressCommentHeading は、途中の状況をコメントさせる節の見出しである。
const progressCommentHeading = "## 長くかかるときは、途中でも状況を書くこと"

// finishedHeading は、turn の終わりにやることを教える節の見出しである。
const finishedHeading = "## 終わったらやること"

// 目的: 組み込みのプロンプトが、長い作業の途中でも状況をコメントさせることを固定する
// （#153（待機中に continuo が定期的にコメントを書く仕組みが無く、18時間の時計が進まない）。設計 5-3h）。
//
// **なぜ要るか。**同じカンバンを複数の機械で見張るとき、担当は issue の担当者（assignee）で持つ。
// **担当者の進捗報告が18時間現れないと、担当が外れて入札をやり直す**
// （`tracker.provider.handoff.idle_timeout_ms`。既定 64800000 ミリ秒。設計 3-77b）。
// **その時計を進めるのは、`<!-- continuo:progress -->` が付いたコメントだけである**（設計 5-3l）。
// **判定する側は internal/handoff にあるが、書く側はこの節しか無い。**
// 節が消えると、長い作業のあいだ時計が1秒も進まなくなる。
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 節があり、コメントの印・投稿するコマンド・push・18時間の理由の4つを教えていること。
func TestTemplate_組み込みのプロンプトは途中でも状況を書かせる(t *testing.T) {
	body := prompt.Builtin()

	if !strings.Contains(body, "\n"+progressCommentHeading+"\n") {
		t.Fatalf("組み込みのプロンプトに %q の節がありません。"+
			"この節が無いと、長い作業のあいだ持ち回りの18時間の時計が進まず、"+
			"生きて働いている機械から担当が外れます", progressCommentHeading)
	}

	// 節の中身だけを見る。**本文の別の場所に同じ語があっても、この節が教えていることにはならない。**
	// とくに `gh issue comment` と `git push -u origin HEAD` は
	// 「## 終わったらやること」にもあるので、全文への contains では素通りする。
	section := sectionOf(t, body, progressCommentHeading)

	for _, want := range []struct {
		needle string
		why    string
	}{
		{"1時間", "間隔を書かないと、エージェントは「長く」がどれくらいかを決められません"},
		{"date -u", "エージェントは時間の経過に自分では気づけません。" +
			"時刻を引くコマンドを渡さないと、決めた間隔を測る手立てがありません"},
		{"gh issue comment", "コメントの投稿のしかたを書かないと、書けと言われても手段が分かりません"},
		{"<!-- continuo:agent -->", "印が無いコメントは、continuo がエージェントの発言として見分けられません"},
		{"{{.issue.url}}", "issue の URL を渡さないと、エージェントは別の issue へ書きかねません"},
		{"git push -u origin HEAD", "push させないと、担当が外れた時点で手元の commit が他の機械から見えなくなります"},
		{"18時間", "なぜ書くのかを書かないと、忙しいときに真っ先に落とされます"},
	} {
		if !strings.Contains(section, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", progressCommentHeading, want.needle, want.why)
		}
	}
}

// 目的: 進捗の報告を「いちばん下にある自分のコメントへ書き足す」手順を、組み込みが教えていることを
// 固定する（#194（進捗コメントの間隔と重ね方が、人間の決定と逆に実装されている）。設計 5-3j）。
//
// **なぜ要るか。**毎回新しいコメントを投稿すると、**18時間で18件並んで issue が読めなくなる。**
// 人間が決めた形は「**いちばん下が自分の進捗報告なら、その1件へ書き足す。
// 間に別のコメントが入っていたら新しく投稿する**」である。
// **この分岐はプロンプトにしか無い。**continuo の側は1バイトも書かないので、
// 節から手順が落ちると、そのまま元の「毎回新規投稿」へ戻る。
//
// 与える情報: prompt.Builtin() の、途中の状況を書かせる節。
// 成功条件: 書き足す先を引くコマンド・書き足すコマンド・新しく投稿する側の印の3つが在ること。
func TestTemplate_組み込みのプロンプトは進捗報告を書き足させる(t *testing.T) {
	section := sectionOf(t, prompt.Builtin(), progressCommentHeading)

	for _, want := range []struct {
		needle string
		why    string
	}{
		{"<!-- continuo:progress -->", "進捗の報告だけに付ける印がないと、" +
			"最後の成果報告と区別できず、成果報告に書き足してしまいます"},
		{".comments[-1:][]", "いちばん下の1件だけを見る書き方を渡さないと、" +
			"コメントが0件の issue で jq が落ちます"},
		{".viewerDidAuthor", "自分が書いたものかを見ないと、" +
			"人間が書いたコメントを進捗報告と読み違えて書き潰します"},
		{"--method PATCH", "書き足すコマンドを渡さないと、" +
			"エージェントは書き足す手段を知らないまま新しいコメントを投稿します"},
		{"issues/comments/", "書き足す先の API のパスを渡さないと、" +
			"エージェントは自分でパスを組み立てることになります"},
	} {
		if !strings.Contains(section, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", progressCommentHeading, want.needle, want.why)
		}
	}

	// **`--edit-last` を教えてはならない。**gh の help は
	// `Edit the last comment of the current user`（訳: いまの利用者の最後のコメントを編集する）で、
	// **「その issue の最後のコメント」ではない。**エージェント・continuo・人間は
	// 同じ GitHub アカウントで投稿するので、**進捗報告のあとに人間が書いたコメントを黙って上書きする。**
	if strings.Contains(section, "--edit-last") {
		t.Errorf("%q の節が --edit-last を教えています。"+
			"あれは「いまの利用者の最後のコメント」を編集するので、"+
			"進捗報告のあとに人間が書いたコメントを上書きします", progressCommentHeading)
	}

	// **印を落とすと持ち回りの期限が進まないことを、節が言っていること。**
	// **エージェントは「コメントが1件増えるだけ」と読んで印を省きうる。**
	// **実際には、印の無いコメントは18時間の時計を1秒も進めない**（設計 5-3l）。
	if !strings.Contains(section, "この印が付いたコメントだけ") {
		t.Errorf("%q の節が、印を落とすと持ち回りの期限が進まないことを言っていません。"+
			"「コメントが1件増えるだけ」と読まれると、エージェントは印を省き、"+
			"書き続けているのに18時間で担当が外れます", progressCommentHeading)
	}

	// **最後の成果報告に進捗の印を付けさせてはならない。**付けると、
	// **次の進捗報告が成果報告に書き足して、読む人には別の話が1件に混ざって見える。**
	finished := sectionOf(t, prompt.Builtin(), finishedHeading)
	if strings.Contains(finished, "<!-- continuo:progress -->") {
		t.Errorf("%q の節が、最後の成果報告に進捗の印を付けさせています。"+
			"付けると、次の進捗報告がその成果報告に書き足します", finishedHeading)
	}
}

// 目的: 途中の状況を書かせる節が、「終わったらやること」より前に在ることを固定する（設計 5-3h）。
//
// **後ろに置くと、turn の終わりの手順を読み終えたあとに目に入る。**
// **turn の途中で読ませたい指示なので、間に合わない。**
//
// 与える情報: prompt.Builtin() の全文と、prompt.Build() が組み立てた全文。
// 成功条件: どちらでも、途中の状況を書かせる節が「終わったらやること」より前に在ること。
func TestTemplate_途中の状況を書かせる節は終わったらやることより前にある(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"組み込みだけ", prompt.Builtin()},
		{"本文を挟んだもの", prompt.Build("## 固有の目印\n", "/tmp/WORKFLOW.md").Text()},
	} {
		progress := strings.Index(tc.body, "\n"+progressCommentHeading+"\n")
		finished := strings.Index(tc.body, "\n"+finishedHeading+"\n")
		if progress < 0 || finished < 0 {
			t.Fatalf("%s: 見出しが揃っていません（%q=%d / %q=%d）",
				tc.name, progressCommentHeading, progress, finishedHeading, finished)
		}
		if progress > finished {
			t.Errorf("%s: %q が %q より後ろにあります。"+
				"turn の終わりの手順を読み終えたあとでは、途中で書かせる指示が間に合いません",
				tc.name, progressCommentHeading, finishedHeading)
		}
	}
}

// sectionOf は、本文から見出しの節（見出しの次の行から次の見出しの手前まで）を取り出す。
//
// t: テストコンテキスト。
// body: 組み込みのプロンプトの全文。
// heading: 取り出す節の見出しの行。
// 戻り値: 見出しの次の行から、次の "## " で始まる行の手前までの中身。
func sectionOf(t *testing.T, body, heading string) string {
	t.Helper()

	lines := strings.Split(body, "\n")
	start := -1
	for i, line := range lines {
		if line == heading {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("本文から %q の見出しを取り出せません", heading)
	}
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			return strings.Join(lines[start:i], "\n")
		}
	}
	return strings.Join(lines[start:], "\n")
}
