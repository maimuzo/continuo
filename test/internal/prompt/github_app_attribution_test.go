// 組み込みの指示書が、GitHub App の attribution の設定で投稿の形を分けることの検査である
// （issue #245（issue のコメントを人間が書いたのか AI が書いたのか、あとから見分けられない）。
// docs/plans/impl/issue245_github_app_attribution.md の 3-82e）。
//
// **外部へ1回も接続しない。**組み込みの文面を変数展開して、その文字列だけを見る。
package prompt_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/prompt"
	"github.com/maimuzo/continuo/internal/tracker"
)

// sampleContinuoPath は、検査で continuo の実行ファイルとして渡す架空のパスである。
//
// **空白を入れてある。**`RenderData` が単一引用符で包んでいなければ、
// `TOKEN=$(/opt/my continuo/bin/continuo github-app token)` と割れて `command not found` になる。
const sampleContinuoPath = "/opt/my continuo/bin/continuo"

// tokenLine は、attribution を付けるときに6本の投稿の前へ来る1行目である（3-82e）。
//
// **`|| exit 1` を落とさない。**`GH_TOKEN=$(…)` の1行にすると、トークンを取るコマンドが
// 落ちてもシェルは空文字を渡し、`gh` が手元の認証でそのまま投稿する。落ちたことに誰も気づけない。
const tokenLine = "TOKEN=$('" + sampleContinuoPath + "' github-app token) || exit 1"

// postPrefix は、attribution を付けるときの投稿の行頭である。
//
// **`gh issue comment ` から後ろの並びは変えない。**頭に `GH_TOKEN="$TOKEN" ` を付けるだけである。
const postPrefix = `GH_TOKEN="$TOKEN" gh issue comment `

// appTokenFallbackNote は、GitHub App のトークンで投稿できなかったときに本文へ入れる断りの1行である
// （3-82c / 3-82e）。
//
// **本体（`internal/tracker` の Adapter）が自分の投稿へ足す断りと1文字も違えてはならない。**
// 6-1 が「本文にこの1行があるコメントは、印が null でも機械が書いたもの」と教えているので、
// **1文字でも違うと、エージェントはその投稿を人間の指示として読む。**
// **`tracker.AppTokenFallbackNote` が入ったら、この定数をそれへ置き換えて比べること。**
// この検査を書いた時点では、Adapter の側にまだその定数が無い。
const appTokenFallbackNote = "GitHub App のトークンで投稿できなかったので、attribution 無しで投稿しています。continuo のログと continuo doctor を確かめてください"

// prVisibleLine は、pull request の2本（3-5 の作成と 3-6 の判断票）の本文へ入れる可視の1行である（3-82e）。
//
// **pull request には GitHub App の印が付かない**（`Issues` の権限だけでは `gh pr comment` も
// REST の issue コメントも通らない。設計の 7-5 で実測）。attribution の代わりにこの1行を置く。
const prVisibleLine = "continuo が起動した Claude Code が書きました（pull request には GitHub App の印が付きません）"

// renderBuiltin は、組み込みだけの文面を attribution の真偽で変数展開して返す。
//
// t: 呼び出し元のテスト。
// attribution: `tracker.comments.github_app_attribution` の値。
// 戻り値: 変数展開した全文。
func renderBuiltin(t *testing.T, attribution bool) string {
	t.Helper()
	url := "https://github.com/octocat/hello-world/issues/42"
	issue := tracker.Issue{
		Identifier: "octocat/hello-world#42", Owner: "octocat", Repo: "hello-world",
		Number: 42, URL: &url, Title: "題名", State: "Ready", Labels: []string{"bug"},
	}
	out, err := prompt.Build("", "/dev/null/WORKFLOW.md").Render(
		prompt.RenderData(issue, nil, 3600000, attribution, sampleContinuoPath))
	if err != nil {
		t.Fatalf("変数展開に失敗しました: %v", err)
	}
	if strings.Contains(out, "{{") {
		t.Fatalf("展開されていない変数が残っています: %q", out[strings.Index(out, "{{"):])
	}
	return out
}

// linesContaining は、needle を含む行だけを返す。
//
// lines: 全文を行に分けたもの。
// needle: 探す文字列。
// 戻り値: 含む行の添字。
func linesContaining(lines []string, needle string) []int {
	var out []int
	for i, line := range lines {
		if strings.Contains(line, needle) {
			out = append(out, i)
		}
	}
	return out
}

// 目的: `RenderData` が2つの変数を、送る文面が使う形で返すことを固定する（3-82e）。
//
// **`continuo.command` は `RenderData` の中で単一引用符に包む。**呼び出し側で包ませない。
// 二重に包むと、単一引用符が第1語に残って `command not found` で全投稿が落ちる
// （gofmt が doc comment の連続した単一引用符を曲がった引用符へ書き換えるので、ここには形を書かない）。
//
// 与える情報: 空白を含む実行ファイルのパスと、真偽それぞれの設定。
// 成功条件: `github_app_attribution` が渡した真偽そのもの、`continuo.command` が
// 単一引用符で包んだパスであること。
func TestRenderData_GitHubAppの変数を2つ返す(t *testing.T) {
	for _, attribution := range []bool{false, true} {
		got := prompt.RenderData(tracker.Issue{}, nil, 0, attribution, sampleContinuoPath)
		if got["github_app_attribution"] != attribution {
			t.Errorf("github_app_attribution が渡した値と違います: %#v（%v を期待）",
				got["github_app_attribution"], attribution)
		}
		inner, ok := got["continuo"].(map[string]any)
		if !ok {
			t.Fatalf("continuo の中身が map ではありません: %#v", got["continuo"])
		}
		if inner["command"] != "'"+sampleContinuoPath+"'" {
			t.Errorf("continuo.command が単一引用符で包まれていません: %#v", inner["command"])
		}
	}
}

// 目的: 設定が真のとき、新しく投稿する6本が「トークンを取る行 + `GH_TOKEN` 付きの投稿」の2行になることを
// 固定する（3-82e）。
//
// **6本とは、3-2 の計画・3-2 の判断票・3-7 の成果・5-3 の進捗報告・7-2 の2本である。**
// **書き足しの2本（`gh api --method PATCH`）には掛けない**（3-82d）。既存のコメントへの書き足しは
// 元の投稿者の attribution のまま残るので、掛ける意味が無い。
//
// 与える情報: 設定が真で変数展開した組み込みの全文。
// 成功条件: トークンを取る行が6本あり、その次の行がそれぞれ `GH_TOKEN="$TOKEN" gh issue comment `
// で始まること。`gh issue comment` の行は全部 `GH_TOKEN` 付きであること。
// `--method PATCH` の2本には `GH_TOKEN` が付いていないこと。
func TestTemplate_attributionが真なら6本の投稿がトークンを取ってから投稿する(t *testing.T) {
	lines := strings.Split(renderBuiltin(t, true), "\n")

	tokenAt := []int{}
	for i, line := range lines {
		if strings.TrimSpace(line) == tokenLine {
			tokenAt = append(tokenAt, i)
		}
	}
	if len(tokenAt) != 6 {
		t.Fatalf("トークンを取る行 %q が %d 本あります（6本のはず: 3-2 の計画と判断票 / 3-7 / 5-3 / 7-2 の2本）",
			tokenLine, len(tokenAt))
	}
	for _, i := range tokenAt {
		if i+1 >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[i+1]), postPrefix) {
			t.Errorf("%d 行目のトークンを取る行の次が、GH_TOKEN 付きの投稿ではありません: %q",
				i+1, lines[min(i+1, len(lines)-1)])
		}
		// **同じ字下げにする。**塊の外に出た行は、エージェントが実行するコマンドとして読まない。
		indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
		next := len(lines[i+1]) - len(strings.TrimLeft(lines[i+1], " "))
		if indent != next {
			t.Errorf("%d 行目のトークンを取る行と投稿の行の字下げが違います（%d と %d）", i+1, indent, next)
		}
	}

	posts := linesContaining(lines, "gh issue comment ")
	if len(posts) != 6 {
		t.Errorf("`gh issue comment ` を含む行が %d 本あります（6本のはず）", len(posts))
	}
	for _, i := range posts {
		if !strings.Contains(lines[i], postPrefix) {
			t.Errorf("%d 行目の投稿に GH_TOKEN が付いていません: %q", i+1, lines[i])
		}
	}

	patches := linesContaining(lines, "gh api --method PATCH")
	if len(patches) != 2 {
		t.Errorf("書き足しの `gh api --method PATCH` が %d 本あります（2本のはず: 5-3 段2a / 7-2 段2a）", len(patches))
	}
	for _, i := range patches {
		if strings.Contains(lines[i], "GH_TOKEN") {
			t.Errorf("%d 行目の書き足しに GH_TOKEN が付いています（書き足しには掛けない）: %q", i+1, lines[i])
		}
	}
}

// 目的: 設定が偽のとき、トークンを取るコマンドと `GH_TOKEN` 付きの投稿が送る文面に1つも出ないことを
// 固定する（3-82e）。
//
// **偽のまま `TOKEN=$(…) || exit 1` を配ると、資格情報を持たない利用者の投稿が全部落ちる。**
// 既定は偽なので、いまの利用者は全員こちらである。
//
// **見るのはコマンドの形だけである。**5-6 の節と、3-2 の「出力を `echo` しない」の文は
// `{{if}}` で囲んでいない（設計レビュー10周目の指摘を人間が直さないと決めた）ので、
// 散文には `GH_TOKEN` と `github-app token` の語が偽でも残る。
//
// 与える情報: 設定が偽で変数展開した組み込みの全文。
// 成功条件: トークンを取るコマンドも `GH_TOKEN` 付きの `gh` も無く、`gh issue comment` の6本が素の形であること。
func TestTemplate_attributionが偽ならトークンの行が出ない(t *testing.T) {
	out := renderBuiltin(t, false)
	lines := strings.Split(out, "\n")

	for _, notWant := range []string{"github-app token) || exit 1", `GH_TOKEN="$TOKEN" gh `} {
		if strings.Contains(out, notWant) {
			at := linesContaining(lines, notWant)
			t.Errorf("設定が偽なのに %q が本文にあります（%d 行目など）。"+
				"資格情報を持たない利用者の投稿が落ちます", notWant, at[0]+1)
		}
	}
	posts := linesContaining(lines, "gh issue comment ")
	if len(posts) != 6 {
		t.Errorf("`gh issue comment ` を含む行が %d 本あります（6本のはず）", len(posts))
	}
	for _, i := range posts {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), "gh issue comment ") {
			t.Errorf("%d 行目の投稿が素の `gh issue comment` で始まっていません: %q", i+1, lines[i])
		}
	}
}

// 目的: 判断票の投稿が、ファイルへ書いてから `--body-file` で渡す形であることを固定する（3-82e）。
//
// **`cat > judgement.md` の段を落とすと、存在しないファイルを渡して投稿が必ず落ち、
// CI（`design-review-result`）が永久に赤になる。**
//
// 与える情報: 3-2 の節。
// 成功条件: `cat > judgement.md <<'JUDGE'` が `--body-file judgement.md` より前にあること。
// 真偽どちらでも同じであること。
func TestTemplate_判断票はファイルへ書いてから投稿する(t *testing.T) {
	for _, attribution := range []bool{false, true} {
		section := sectionOf(t, renderBuiltin(t, attribution), "## 3-2. 計画を書き、レビューを受ける")
		write := strings.Index(section, "cat > judgement.md <<'JUDGE'")
		post := strings.Index(section, "--body-file judgement.md")
		if write < 0 || post < 0 {
			t.Fatalf("attribution=%v: 判断票をファイルへ書く段か投稿が 3-2 にありません（write=%d / post=%d）",
				attribution, write, post)
		}
		if write > post {
			t.Errorf("attribution=%v: 判断票をファイルへ書く段が投稿より後ろにあります", attribution)
		}
		// **印の2行は、ファイルの先頭に、この順である。**CI と continuo の両方が数える。
		if !strings.Contains(section, "cat > judgement.md <<'JUDGE'\n    <!-- continuo:agent -->\n    <!-- design-review-result -->") {
			t.Errorf("attribution=%v: judgement.md の先頭2行が、印の並びになっていません", attribution)
		}
	}
}

// 目的: 5-6 の節があり、断りの1行が本体の投稿へ足すものと同じであることを固定する（3-82c / 3-82e）。
//
// **人間の決定（2026-09-09）。**「真実を知ってるなら直接コメントを書き換えるか、少なくとも AI に
// 足りないことを伝えて書き換えるように指示出せよ。ログに出しても解決しないだろ。」
// トークンで投稿できなかったとき、黙って落ちるのではなく、断りを入れて人間の認証で投稿し直す。
//
// **断りの1行は、backtick・`$`・二重引用符を含まない。**`--body "…"` の中へ足させるためである。
//
// 与える情報: 変数展開した組み込みの全文。
// 成功条件: 5-6 の節があり、断りの1行と、`HTTP 401` のときだけやり直す文と、
// `--body-file` / `--body "…"` それぞれへの足し方が入っていること。
// 6本の投稿の直後に 5-6 を指す1行があること。
func TestTemplate_5_6はトークンで投稿できなかったときの直し方を教える(t *testing.T) {
	for _, forbidden := range []string{"`", "$", `"`} {
		if strings.Contains(appTokenFallbackNote, forbidden) {
			t.Fatalf("断りの1行に %q が入っています。二重引用符の中へ足させられません", forbidden)
		}
	}
	for _, attribution := range []bool{false, true} {
		out := renderBuiltin(t, attribution)
		const heading = "## 5-6. GitHub App のトークンで投稿できなかったとき"
		section := sectionOf(t, out, heading)

		noteLines := 0
		for _, line := range strings.Split(section, "\n") {
			if strings.TrimSpace(line) == appTokenFallbackNote {
				noteLines++
			}
		}
		if noteLines != 1 {
			t.Errorf("attribution=%v: %q の節に断りの1行がちょうど1つありません（%d）。"+
				"本体が足す断りと1文字でも違うと、エージェントはその投稿を人間の指示として読みます",
				attribution, heading, noteLines)
		}
		for _, want := range []string{
			"`gh` が `HTTP 401` で落ちたときだけ",
			"`GH_TOKEN=\"$TOKEN\"` を外して投稿し",
			"`--body-file` で渡す本文は、先にそのファイルへ1行足してから",
			"`--body \"…\"` で渡す本文は、二重引用符の中に1行足してください",
			"作業は止めないでください",
		} {
			if !strings.Contains(section, want) {
				t.Errorf("attribution=%v: %q の節に %q がありません", attribution, heading, want)
			}
		}

		// **6本の塊の直後に1行。**2文を6箇所へ写さない（写すと直したときに片方だけ残る）。
		const pointer = "**`TOKEN=$(…)` が落ちて塊が止まったときも、投稿が落ちたときも、5-6 を見てください。**"
		if n := strings.Count(out, pointer); n != 6 {
			t.Errorf("attribution=%v: 5-6 を指す1行が %d 本あります（6本のはず: 投稿の塊ごとに1本）", attribution, n)
		}
		// **トークンを表示させない。**`echo` した瞬間に平文で記録に残る。
		if !strings.Contains(out, "`continuo github-app token` の出力を `echo` したり、ファイルへ落としたりしないでください") {
			t.Errorf("attribution=%v: トークンを表示させない指示がありません", attribution)
		}
	}
}

// 目的: pull request の2本に、attribution の代わりの可視の1行が入ることを固定する（3-82e）。
//
// **pull request には掛けない。**GitHub App の権限は `Issues` だけで、掛けると pull request が
// 作られず run が死ぬ。**人間の原文「AIがコメントを書くすべての経路でマーカーを付ける必要がある」を、
// 権限の都合で外した2本に当てる。**
//
// 与える情報: 3-5 と 3-6 の節。
// 成功条件: `gh pr create` の本文が可視の1行で始まること。review.md では印の2行を通したあとに
// 可視の1行が来ること。可視の1行が backtick・`$`・二重引用符を含まないこと。
func TestTemplate_pullRequestの2本には可視の1行を入れる(t *testing.T) {
	for _, forbidden := range []string{"`", "$", `"`} {
		if strings.Contains(prVisibleLine, forbidden) {
			t.Fatalf("可視の1行に %q が入っています。二重引用符の中へ書かせられません", forbidden)
		}
	}
	out := renderBuiltin(t, true)

	create := sectionOf(t, out, "## 3-5. pull request を出す")
	if !strings.Contains(create, `gh pr create --title "<何を直したか>" --body "`+prVisibleLine+"\n") {
		t.Errorf("3-5 の `gh pr create` の本文が可視の1行で始まっていません")
	}
	review := sectionOf(t, out, "## 3-6. pull request のレビューを受ける")
	if !strings.Contains(review, "<!-- code-review-result -->\n    <!-- continuo:agent -->\n    "+prVisibleLine+"\n") {
		t.Errorf("3-6 の review.md で、印の2行の直後に可視の1行がありません")
	}
	// **`gh pr` に `GH_TOKEN` を掛けない。**掛けると `Resource not accessible by integration` で落ちる。
	for i, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "gh pr ") && strings.Contains(line, "GH_TOKEN") {
			t.Errorf("%d 行目の `gh pr` に GH_TOKEN が付いています。Issues の権限だけでは通りません: %q", i+1, line)
		}
	}
}

// 目的: 4-1 が issue のコメントを REST で読み、attribution を見られることを固定する（3-82e）。
//
// **GraphQL の `IssueComment` には `performed_via_github_app` が無い**（設計の 7-3）。
// `gh issue view … --json comments` のままだと、エージェントは attribution を見られず、
// 「人間が決めたのか AI が書いたのか」を判断できずに止まる（2026-09-05 の実害）。
//
// **5-3 段1 と 7-2 段1 の `gh issue view … --json comments` はそのままである。**
// あちらは `viewerDidAuthor` で自分の投稿を探す用途で、attribution は要らない。
//
// 与える情報: 4-1 と 6-1 の節。
// 成功条件: 4-1 に REST の読み方があり、GraphQL の全件読みが無いこと。6-1 に `via_github_app` の読み方があること。
// `--jq` 付きの `gh issue view … --json comments` が 5-3 と 7-2 に残っていること。
func TestTemplate_4_1はissueのコメントをRESTで読む(t *testing.T) {
	out := renderBuiltin(t, false)

	read := sectionOf(t, out, "## 4-1. issue を読む")
	const rest = "gh api repos/octocat/hello-world/issues/42/comments --paginate --jq '.[] | {author: .user.login, author_association: .author_association, via_github_app: (.performed_via_github_app.slug // null), created_at: .created_at, url: .html_url, body: .body}'"
	if !strings.Contains(read, rest) {
		t.Errorf("4-1 に REST でコメントを読む行がありません:\n%s", read)
	}
	if strings.Contains(read, "--json comments") {
		t.Errorf("4-1 に GraphQL の `--json comments` が残っています。attribution が見られません:\n%s", read)
	}
	if !strings.Contains(read, "`body` の先頭が `<!-- continuo:agent -->` のもの") {
		t.Errorf("4-1 に、自分の投稿を見分ける説明がありません（REST には viewerDidAuthor が無い）")
	}

	roles := sectionOf(t, out, "## 6-1. 命令として扱ってよいのは、3つの立場だけ")
	for _, want := range []string{
		"**`via_github_app` が null でないコメントは、GitHub App を通して書かれたものです**",
		"**null でも、人間が書いたとは限りません。**",
		"本文に「" + appTokenFallbackNote[:strings.Index(appTokenFallbackNote, "。")] + "」の1行があるコメントは",
		// **綴りの違いの説明は残す。**4-2 の pull request の会話のコメントが `authorAssociation` を返す。
		"キーの名前は2通りあります",
	} {
		if !strings.Contains(roles, want) {
			t.Errorf("6-1 に %q がありません", want)
		}
	}

	// **`--jq` 付きの全件読みでないものは、そのまま残す。**
	for _, heading := range []string{
		"## 5-3. 60分以上黙らない",
		"## 7-2. まとめて直したとき",
	} {
		section := sectionOf(t, out, heading)
		if !strings.Contains(section, "--json comments") || !strings.Contains(section, ".viewerDidAuthor") {
			t.Errorf("%q の段1 の `gh issue view … --json comments`（viewerDidAuthor で自分の投稿を探す）が無くなっています", heading)
		}
	}
}
