package prompt_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/prompt"
)

// pullRequestHeading は、PR を出させる節の見出しである。
const pullRequestHeading = "## 3-5. pull request を出す"

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
// **組み込みの `## 4-2. 紐づく pull request を読む` は、誰かが PR を作っていることを前提にしている。**
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
	// 「## 3-7. 終わりを書く」にもあるので、全文への contains では素通りする。
	section := sectionOf(t, body, pullRequestHeading)

	for _, want := range []struct {
		needle string
		why    string
	}{
		{"gh pr create", "作るコマンドを書かないと、エージェントは作る手段を知らないまま終わります"},
		{"Closes #{{.issue.number}}", "この1行が PR と issue を結びつけます。" +
			"落とすと、次の turn が `## 4-2. 紐づく pull request を読む` で拾えず、" +
			"レビューの指摘を読む先が消えます"},
		// **既にある PR は、いま居る branch から引かせる。**
		// `## 4-2. 紐づく pull request を読む` の一覧から選ばせてはならない（下の否定の検査）。
		{`--head "$(git branch --show-current)"`, "既にある PR を、いま居る branch から引かせないと、" +
			"turn のたびに2本目・3本目ができます"},
		{"新しく作らないでください", "既にある PR を使わせないと、turn のたびに2本目・3本目ができます"},
		// **`gh` が対話で止まることの説明は、いまも同じ節に在る。**
		// 落ちていると書いていたのは誤りだった（敵対的レビューが実測で反証した）。
		{"gh が「どこへ push するか」を対話で聞いてきて", "push していない branch で叩くと " +
			"gh が対話で止まることを書かないと、エージェントはそこで固まります"},
		// **2026-09-03、人間が送る文面を書き直した**（issue #188（エージェントへ送る指示書が長く、
		// 順序も強調も揃っていないため、初見で読み取れない））。
		// **「本文の『PR を作らない』で作らない」だけが、その文面から落ちている。**
		// **人間の判断は「よって、これはまったく根拠がないだけでなく、害がある。変更するな」である。**
		// **勝手に戻さないこと。**戻すなら、先に人間へ確かめる。
		//
		// **「作れなかったら blocked」も落ちていない。**7-4 へ移っただけである
		// （この検査は 3-5 の節だけを見るので、ここでは確かめられない）。
	} {
		if !strings.Contains(section, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", pullRequestHeading, want.needle, want.why)
		}
	}

	// **`## 4-2. 紐づく pull request を読む` の一覧から行き先を選ばせてはならない。**
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
// **この節は組み込みの「前半」にある。**本文は前半と後半のあいだに挟まるので（設計 5-3c）、
// **3-5 は本文より前に読まれる。**「後半に置いてあるから本文に打ち消されない」は、この節には当たらない。
// **本文からの打ち消しは、3-78b が開けた口（3-4 の例外を使ったときだけ 4-4 へ譲る）1つだけである。**
//
// 与える情報: prompt.Builtin() の全文と、prompt.Build() が組み立てた全文。
// 成功条件: どちらでも「終わったらやること」より前に在ること。
func TestTemplate_PRを出させる節は終わりを書く節より前にある(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"組み込みだけ", prompt.Builtin()},
		// **中身のある本文を渡す。**見出しだけだと、中身が無い節として落ちるので、
		// 「本文を挟んだ」ことにならず、2つの場合分けが同じものを見ることになる。
		{"本文を挟んだもの", prompt.Build("### 固有の目印\n\n固有の中身です。\n", "/tmp/WORKFLOW.md").Text()},
	} {
		pr := strings.Index(tc.body, "\n"+pullRequestHeading+"\n")
		finished := strings.Index(tc.body, "\n## 3-7. 終わりを書く\n")
		if pr < 0 || finished < 0 {
			t.Fatalf("%s: 見出しが揃っていません（%q=%d / 3-7=%d）", tc.name, pullRequestHeading, pr, finished)
		}
		if pr > finished {
			t.Errorf("%s: %q が「## 3-7. 終わりを書く」より後ろにあります。"+
				"表明の1行の書き方を読み終えたあとでは、pull request を作らせる指示が間に合いません",
				tc.name, pullRequestHeading)
		}
	}
}
