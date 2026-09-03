package prompt_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/prompt"
)

// progressCommentHeading は、途中の状況をコメントさせる節の見出しである。
const progressCommentHeading = "## 5-3. 1時間以上黙らない"

// finishedHeading は、turn の終わりにやることを教える節の見出しである。
const finishedHeading = "## 3-7. 終わりを書く"

// 目的: 組み込みのプロンプトが、長い作業の途中でも状況をコメントさせることを固定する
// （#153（待機中に continuo が定期的にコメントを書く仕組みが無く、18時間の時計が進まない）。設計 5-3h）。
//
// **なぜ要るか。**同じカンバンを複数の機械で見張るとき、担当は issue の担当者（assignee）で持つ。
// **担当者が最後にコメントを書いてから18時間が経つと、担当が外れて入札をやり直す**
// （`tracker.provider.handoff.idle_timeout_ms`。既定 64800000 ミリ秒。設計 3-77b）。
// **その時計を進めるのは、担当者のアカウントが投稿したコメントだけである。**
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

// 目的: 途中の状況を書かせる節と、turn の終わりの節が、本文を挟んだあとも両方残ることを固定する
// （#188（continuo が Claude Code へ渡すプロンプトの品質が低い）。設計 5-3h）。
//
// **並び順は検査しない。**人間が確定させた文面では、手順（3.）のあとに共通ルール（5.）が来るので、
// 「1時間以上黙らない」は「終わりを書く」より後ろに在る。**これは意図した並びである。**
// **検査するのは、どちらの節も消えないことだけである。**
// 組み込みの前半・本文・後半を継ぎ合わせたあとに空の見出しを落とす処理（prompt.Build）を通しても、
// **中身を持つこの2つが落ちてはならない。**
//
// 与える情報: prompt.Builtin() の全文と、prompt.Build() が組み立てた全文。
// 成功条件: どちらでも、2つの見出しが両方在ること。
func TestTemplate_途中の状況を書かせる節と終わりの節は本文を挟んでも残る(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"組み込みだけ", prompt.Builtin()},
		{"本文を挟んだもの", prompt.Build("## 固有の目印\n\nこのプロジェクト固有の決まりです。\n", "/tmp/WORKFLOW.md").Text()},
	} {
		for _, heading := range []string{progressCommentHeading, finishedHeading} {
			if !strings.Contains(tc.body, "\n"+heading+"\n") {
				t.Errorf("%s: %q の節がありません。"+
					"組み立ての途中で節が落ちると、エージェントはその指示を受け取れません",
					tc.name, heading)
			}
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
