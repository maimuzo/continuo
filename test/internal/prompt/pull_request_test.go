package prompt_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/prompt"
)

// pullRequestHeading は、PR を出させる節の見出しである。
const pullRequestHeading = "## PR を出すこと"

// 目的: 組み込みのプロンプトが、`review` を出す前に PR を作らせることを固定する
// （#184（エージェントが作業を終えても pull request を作らず、人間が代わりに作ることになっている）。
// 設計 5-3i）。
//
// **設計文書との突き合わせでは、この条件を守れない。**
// TestTemplate_組み込みのプロンプトが設計5_3と一致する は設計 5-3 の markdown ブロックと
// 組み込みのプロンプトを比べるものなので、**両方からこの節が同時に消えても通る。**
// そこで、設計文書を一切読まず、組み込みのプロンプトだけを見て条件を確かめる。
//
// **なぜ要るか。**この仕組みで人間がするのは、issue で何をしてほしいかを伝えることと、
// 出てきたものをレビューすることの2つだけである。
// **エージェントが push で止まると、branch を自分で見つけて PR を作る仕事が人間に生える。**
// **組み込みの `## この issue に紐づく PR も読むこと` は、誰かが PR を作っていることを前提にしている。**
// 作る指示が無いと、あの節は一度も役に立たない。
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 節があり、作るコマンド・issue との結びつけ・重複を作らないこと・
// push が先であること・作れなかったときの行き先・本文からの打ち消しの6つを教えていること。
func TestTemplate_組み込みのプロンプトはPRを出させる(t *testing.T) {
	body := prompt.Builtin()

	if !strings.Contains(body, "\n"+pullRequestHeading+"\n") {
		t.Fatalf("組み込みのプロンプトに %q の節がありません。"+
			"push で止められると、PR を作るのは人間になります", pullRequestHeading)
	}

	// 節の中身だけを見る。**本文の別の場所に同じ語があっても、この節が教えていることにはならない。**
	// とくに `git push -u origin HEAD` と `CONTINUO-STATUS: blocked` は
	// 「## 終わったらやること」にもあるので、全文への contains では素通りする。
	section := sectionOf(t, body, pullRequestHeading)

	for _, want := range []struct {
		needle string
		why    string
	}{
		{"gh pr create", "作るコマンドを書かないと、エージェントは作る手段を知らないまま終わります"},
		{"Closes #{{.issue.number}}", "この1行が PR と issue を結びつけます。" +
			"落とすと、次の turn が `## この issue に紐づく PR も読むこと` で拾えず、" +
			"レビューの指摘を読む先が消えます"},
		// **既にある PR は、いま居る branch から引かせる。**
		// `## この issue に紐づく PR も読むこと` の一覧から選ばせてはならない（下の否定の検査）。
		{`--head "$(git branch --show-current)"`, "既にある PR を、いま居る branch から引かせないと、" +
			"turn のたびに2本目・3本目ができます"},
		{"新しく作らないでください", "既にある PR を使わせないと、turn のたびに2本目・3本目ができます"},
		{"gh が「どこへ push するか」を対話で聞いてきて", "push していない branch で叩くと、" +
			"gh が push 先を対話で聞いてきて、そこで止まります"},
		{"CONTINUO-STATUS: blocked", "作れなかったときの行き先を書かないと、" +
			"push だけして黙って終わります。人間にはどこを見ればよいのか分かりません"},
		// **雛形はこの口があることを案内している**（`## PR の決まり` の HTML のコメント）。
		// **組み込みから消えると、雛形だけが在りもしない逃げ道を約束することになる。**
		// **調査だけ・レビューだけを頼む運用は、commit する成果が無いので PR を作れない。**
		// 口が閉じると、その運用は毎回 `blocked` で止まる。
		{"「PR を作らない」と書いてあるとき", "本文からの打ち消しを受け付けないと、" +
			"雛形が案内している調査だけ・レビューだけの運用が、毎回 blocked で止まります"},
	} {
		if !strings.Contains(section, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", pullRequestHeading, want.needle, want.why)
		}
	}

	// **`## この issue に紐づく PR も読むこと` の一覧から行き先を選ばせてはならない。**
	//
	// **あの一覧は「この issue を閉じる PR」ではなく「この issue に言及があった PR」を返す。**
	// 実測（2026-09-03、maimuzo/continuo）: issue #60 の timeline は PR #112 を返すが、
	// `gh pr view 112 --json closingIssuesReferences` は `{"closes":[87]}` であり、
	// **PR #112 が閉じるのは issue #87 である。**PR #112 の本文に `#60` は0件だった。
	// **その branch へ push させると、別の issue のために動いているエージェントの作業が消える。**
	//
	// **`state` の綴りも2通りある。**同じ PR について
	// `gh pr list --json state` は `"OPEN"`、`gh api …/timeline` は `"open"` を返す。
	// **綴りで拾う書き方に戻すと、取りこぼして2本目を作る。**
	for _, notWant := range []struct {
		needle string
		why    string
	}{
		{`"state": "OPEN"`, "`state` の綴りは、取り方によって OPEN と open の2通りになります。" +
			"綴りで拾うと取りこぼして2本目を作ります"},
		{`"state":"OPEN"`, "同上"},
	} {
		if strings.Contains(section, notWant.needle) {
			t.Errorf("%q の節に %q があります。%s", pullRequestHeading, notWant.needle, notWant.why)
		}
	}
}

// 目的: PR を出させる節が「終わったらやること」より前に在ることを固定する（設計 5-3i）。
//
// **後ろに置くと、表明の1行の書き方を読み終えたあとに目に入る。**
// **`review` を出す前にやらせたい作業なので、間に合わない。**
//
// **本文（WORKFLOW.md）より後ろに在ることも同時に確かめる。**
// 本文は組み込みの前半と後半のあいだに挟まるので（設計 5-3c）、
// **後半に置いてある限り、本文に何を書いても最後に読まれるのはこちらである。**
//
// 与える情報: prompt.Builtin() の全文と、prompt.Build() が組み立てた全文。
// 成功条件: どちらでも「終わったらやること」より前に在り、
// 本文を挟んだ側では本文より後ろに在ること。
func TestTemplate_PRを出させる節は本文より後ろで終わったらやることより前にある(t *testing.T) {
	const needle = "## 固有の目印"

	for _, tc := range []struct {
		name string
		body string
	}{
		{"組み込みだけ", prompt.Builtin()},
		{"本文を挟んだもの", prompt.Build(needle+"\n", "/tmp/WORKFLOW.md").Text()},
	} {
		pr := strings.Index(tc.body, "\n"+pullRequestHeading+"\n")
		finished := strings.Index(tc.body, "\n"+finishedHeading+"\n")
		if pr < 0 || finished < 0 {
			t.Fatalf("%s: 見出しが揃っていません（%q=%d / %q=%d）",
				tc.name, pullRequestHeading, pr, finishedHeading, finished)
		}
		if pr > finished {
			t.Errorf("%s: %q が %q より後ろにあります。"+
				"表明の1行の書き方を読み終えたあとでは、PR を作らせる指示が間に合いません",
				tc.name, pullRequestHeading, finishedHeading)
		}
	}

	withBody := prompt.Build(needle+"\n", "/tmp/WORKFLOW.md").Text()
	mid := strings.Index(withBody, needle)
	pr := strings.Index(withBody, "\n"+pullRequestHeading+"\n")
	if mid < 0 || pr < 0 {
		t.Fatalf("見出しが揃っていません（本文=%d / %q=%d）", mid, pullRequestHeading, pr)
	}
	if pr < mid {
		t.Errorf("%q が WORKFLOW.md の本文より前にあります。"+
			"本文で打ち消せる位置に置くと、雛形を書き換えた人のところだけ PR が出なくなります",
			pullRequestHeading)
	}
}
