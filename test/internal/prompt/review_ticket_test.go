// 組み込みの指示書が書かせる判断票の形（件数の遷移表と、畳んだ周ごとの中身）と、
// 実装レビューが収まったあとに draft を外させる段の検査である（設計 5-3u）。
//
// **人間が「必ず」と言った2つを守る。**「critical/high/medium/lowの数の遷移表を必ず付けろ」と
// 「実装レビューが終わったら必ずdraft PRのdraftを外せ」である。
//
// **外部へ1回も接続しない。**見本のシェルを走らせる検査は、偽の gh を関数として先に定義し、
// 接続先も存在しないホストに向けてから叩く。
package prompt_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/prompt"
)

// changeReviewHeading は、実装レビューと draft の外し方を書かせる節の見出しである。
const changeReviewHeading = "## 3-6. pull request のレビューを受ける"

// transitionHeader は、判断票の先頭に置かせる件数の遷移表の見出しの行である。
const transitionHeader = "| 周 | CRITICAL | HIGH | MEDIUM | LOW |"

// findSampleMark は、段0（前の判断票を探し、今回の周の番号を出す）の見本を見分ける文字列である。
const findSampleMark = "LAST=$(tr -d"

// reviewLoopHeading は、レビューの回し方（何周回すか・止まり方）を書かせる節の見出しである。
const reviewLoopHeading = "## 5-6. レビューの回し方"

// 目的: 実装レビューが収まったら、誰が作った draft でも必ず外させることを固定する（設計 5-3u）。
//
// **なぜ要るか。**人間は「実装レビューが終わったら必ずdraft PRのdraftを外せ」と指示した。
// 以前の文面は「この issue のためにあなたが draft で作った pull request は」と限っていたので、
// 人間や別の試行が作った draft は外されないまま残った。
// **draft でない pull request に `gh pr ready` を打つと `ready_for_review` が起きない**ので、
// 先に `isDraft` を確かめさせる。
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 3-6 の節が「必ず外して」と言い、`isDraft` の確かめと `gh pr ready` を持ち、
// 作った人を問わないと書いている。流れ図の段も「必ず外す」で、旧い限定の文がどこにも無い。
func TestTemplate_実装レビューが収まったらdraftを必ず外させる(t *testing.T) {
	body := prompt.Builtin()
	section := sectionOf(t, body, changeReviewHeading)

	for _, want := range []struct{ needle, why string }{
		{"draft を、必ず外してから次へ進んでください", "人間は「必ず外せ」と指示しています"},
		{"誰が draft で作ったかは問いません", "作った人で限ると、人間や前の試行が作った draft が残ります"},
		{"--json isDraft --jq .isDraft", "draft でない pull request に gh pr ready を打つと検査が回り直りません"},
		{"gh pr ready <PR番号> --repo {{.issue.owner}}/{{.issue.repo}}", "外すコマンドが無いと、外し方が分かりません"},
		{"「draft のまま人間へ渡す」とはっきり書いてあるとき", "外さない場合を限らないと、4-4 の「draft で作る」で外さなくなります"},
		{"3-7 の報告の `### 詳細` に書いてください", "外せなかったことが人間に届きません"},
	} {
		if !strings.Contains(section, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", changeReviewHeading, want.needle, want.why)
		}
	}

	if strings.Contains(body, "あなたが draft で作った") {
		t.Error("組み込みに「あなたが draft で作った」の限定が残っています。人間の指示は「必ず外せ」です")
	}
	flow := false
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, `-- "収まった" --> P[`) {
			flow = true
			if !strings.Contains(line, "必ず外す") {
				t.Errorf("流れ図の draft を外す段が「必ず外す」になっていません: %q", line)
			}
		}
	}
	if !flow {
		t.Error("流れ図に、実装レビューが収まったあとの段（P）がありません")
	}
}

// 目的: 判断票の見本が、件数の遷移表を先頭に置き、周ごとの中身を畳む形であることを固定する（設計 5-3u）。
//
// **なぜ要るか。**人間は「最初は畳んだ状態にしろ」「critical/high/medium/lowの数の遷移表を必ず付けろ」と指示した。
// 人間が判断票で見るのは、貼ってあるかどうかと件数の移り変わりで、中身はほとんど読まない。
// **見本から遷移表が落ちると、エージェントは周ごとの中身だけを貼り、スクロールが減らない。**
//
// 与える情報: prompt.Builtin() の 3-2 と 3-6 の節。
// 成功条件: どちらの節の判断票の見本も、題名 → 遷移表の見出し → `<details>` → `<summary>1周目</summary>`
// の順に並ぶ。「<何周目か>周目」を書かせる旧い行がどこにも無い。
func TestTemplate_判断票の見本は遷移表を先頭に置き中身を畳む(t *testing.T) {
	body := prompt.Builtin()
	for _, tc := range []struct{ heading, title string }{
		{planReviewHeading, planTicketHeading},
		{changeReviewHeading, changeTicketHeading},
	} {
		section := sectionOf(t, body, tc.heading)
		steps := []string{tc.title, transitionHeader, "| --- | --- | --- | --- | --- |", "| 1周目 |", "<details>", "<summary>1周目</summary>", "</details>"}
		next := 0
		for _, line := range strings.Split(section, "\n") {
			if next < len(steps) && strings.HasPrefix(strings.TrimSpace(line), steps[next]) {
				next++
			}
		}
		if next < len(steps) {
			t.Errorf("%q の判断票の見本に、%q が順に並んでいません（%d 個目で止まりました）。"+
				"遷移表を題名の直後に置き、周ごとの中身を <details> で畳ませてください",
				tc.heading, steps[next], next+1)
		}
	}
	if strings.Contains(body, "<何周目か>周目") {
		t.Error("組み込みに「<何周目か>周目」を書かせる行が残っています。周の番号は遷移表と <summary> が持ちます")
	}
}

// 目的: 1つの run で書くコメントを、特に理由のない限り1件にさせることを固定する（設計 5-3u）。
//
// **なぜ要るか。**人間は「特に理由のない限り1つのrunでは1つのコメントだけを書いて」と指示した。
// 例外は、理由とともに 5-5 の表に置く。**「対応しない」と決めた理由と削除の記録は、別のコメントにさせない。**
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 5-5 に1件の決まりがあり、3-2 と 5-6 が別のコメントへ書かせていない。
func TestTemplate_1つのrunでは1つのコメントだけを書かせる(t *testing.T) {
	body := prompt.Builtin()
	format := sectionUntilNextChapter(t, body, commentFormatHeading)
	if !strings.Contains(format, "特に理由のない限り、1つの run では1つのコメントだけを書いてください") {
		t.Errorf("%q に、1つの run では1つのコメントだけ、の決まりがありません", commentFormatHeading)
	}
	for _, old := range []string{
		"その理由を issue のコメントへ書いてから",
		"削除した内容を issue のコメントへ残す",
		"削除の記録         5-6（issue へ）",
	} {
		if strings.Contains(body, old) {
			t.Errorf("組み込みに %q が残っています。別のコメントを1件増やさせます", old)
		}
	}
}

// fencedBashBlock は、組み込みの指示書の ```bash の囲みのうち、mark を含む最初のものを返す。
//
// t: テストコンテキスト。
// mark: 囲みの中身に含まれる文字列。
// 戻り値: 囲みの中身（開きと閉じの行を除く）。見つからなければテストを失敗させる。
func fencedBashBlock(t *testing.T, mark string) string {
	t.Helper()

	lines := strings.Split(prompt.BuiltinRaw(), "\n")
	for i := 0; i < len(lines); i++ {
		if lines[i] != "```bash" {
			continue
		}
		var buf []string
		for k := i + 1; k < len(lines) && !isFenceLine(lines[k]); k++ {
			buf = append(buf, lines[k])
		}
		block := strings.Join(buf, "\n") + "\n"
		if strings.Contains(block, mark) {
			return block
		}
	}
	t.Fatalf("組み込みに %q を含む ```bash の囲みがありません", mark)
	return ""
}

// fakeGh は、試しに使う偽の gh である。読み取りは $BODY を返し、書き込みは $OUT の下へ写す。
// `issue view` と `pr view` は、探す段の jq を通した結果として $FIND_ID を返す。
// $VIEW_FAIL が空でなければ、`issue view` と `pr view` を終了コード 1 で落とす。
// $API_FAIL が空でなければ、PATCH でない `api`（前の判断票の読み取り）を、本文を2行だけ出して終了コード 1 で落とす。
// 受け取った引数は、1回につき1行で $OUT/args へ足す。
const fakeGh = `#!/bin/sh
echo "$*" >> "$OUT/args"
if [ -n "$VIEW_FAIL" ]; then
  case "$1 $2" in "issue view"|"pr view") echo "HTTP 502: Bad Gateway" >&2; exit 1 ;; esac
fi
if [ -n "$API_FAIL" ] && [ "$1" = api ] && [ "$2" != --method ]; then
  head -n 2 "$BODY"; echo "HTTP 502: Bad Gateway" >&2; exit 1
fi
case "$1 $2" in
  "api --method")
    for a in "$@"; do case "$a" in body=@*) cp "${a#body=@}" "$OUT/patched";; esac; done
    exit "${PATCH_RC:-0}" ;;
  "api "*)
    cat "$BODY" ;;
  "issue comment"|"pr comment")
    while [ $# -gt 0 ]; do [ "$1" = --body-file ] && cp "$2" "$OUT/posted"; shift; done ;;
  "issue view"|"pr view")
    echo "$FIND_ID" ;;
esac
`

// sampleRun は、見本を1回走らせるときの入力である。
type sampleRun struct {
	shell  string // bash か zsh
	script string // 走らせる見本（テンプレートの変数は置き換え済み）
	body   string // 偽の gh が前の判断票として返す本文
	findID string // 偽の gh が探す段の結果として返す ID
	env    []string
}

// runSample は、偽の gh を関数として先に定義してから見本を走らせる。
//
// t: テストコンテキスト。
// r: 走らせる内容。
// 戻り値: 標準出力と標準エラーをまとめたもの、PATCH に渡した本文、新しく貼った本文（無ければ空）。
func runSample(t *testing.T, r sampleRun) (out, patched, posted string) {
	t.Helper()
	out, patched, posted, _ = runSampleWithArgs(t, r)
	return out, patched, posted
}

// runSampleWithArgs は、runSample と同じく見本を走らせ、偽の gh が受け取った引数も返す。
//
// t: テストコンテキスト。
// r: 走らせる内容。
// 戻り値: runSample の3つと、偽の gh が受け取った引数（1回につき1行）。
func runSampleWithArgs(t *testing.T, r sampleRun) (out, patched, posted, ghArgs string) {
	t.Helper()

	dir := t.TempDir()
	gh := filepath.Join(dir, "gh")
	if err := os.WriteFile(gh, []byte(fakeGh), 0o755); err != nil {
		t.Fatal(err)
	}
	bodyFile := filepath.Join(dir, "body.md")
	if err := os.WriteFile(bodyFile, []byte(r.body), 0o644); err != nil {
		t.Fatal(err)
	}
	// **関数は PATH より先に引かれる。**zsh が起動ファイルで PATH を並べ直しても、本物の gh には届かない。
	script := "gh() { '" + gh + "' \"$@\"; }\n" + r.script
	scriptFile := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(scriptFile, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{scriptFile}
	if r.shell == "zsh" {
		args = []string{"-f", scriptFile} // 起動ファイルを読ませない
	}
	cmd := exec.Command(r.shell, args...)
	cmd.Env = append(os.Environ(),
		"OUT="+dir, "BODY="+bodyFile, "FIND_ID="+r.findID,
		"GH_HOST=invalid.invalid", "GH_TOKEN=invalid", "PATH="+dir+":"+os.Getenv("PATH"))
	cmd.Env = append(cmd.Env, r.env...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s で見本が終了コード 0 以外で終わりました: %v\n%s", r.shell, err, b)
	}
	read := func(name string) string {
		c, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return ""
		}
		return string(c)
	}
	return string(b), read("patched"), read("posted"), read("args")
}

// asChangeReview は、見本の `KIND=` と `PR=` と `PRREPO=` を、実装の判断票の値へ置き換える。
func asChangeReview(script, pr, prRepo string) string {
	script = regexp.MustCompile(`(?m)^KIND=計画$`).ReplaceAllLiteralString(script, "KIND=実装")
	script = regexp.MustCompile(`(?m)^PR=$`).ReplaceAllLiteralString(script, "PR="+pr)
	return regexp.MustCompile(`(?m)^PRREPO=.*$`).ReplaceAllLiteralString(script, "PRREPO="+prRepo)
}

// shellsForSample は、見本を試すシェルを返す。bash が無ければ検査を飛ばし、zsh は在れば足す。
func shellsForSample(t *testing.T) []string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash がありません")
	}
	shells := []string{"bash"}
	if _, err := exec.LookPath("zsh"); err == nil {
		shells = append(shells, "zsh")
	}
	return shells
}

// fillTemplate は、見本の中のテンプレートの変数を試し用の値へ置き換える。
var fillTemplate = strings.NewReplacer(
	"{{.issue.owner}}/{{.issue.repo}}", "octocat/hello-world",
	"{{.issue.url}}", "https://github.com/octocat/hello-world/issues/1",
)

// postSample は、段5 の見本の `ID=` と `NO=` と NOTE の中身を埋めて返す。
func postSample(t *testing.T, id, no, note string) string {
	t.Helper()
	s := fillTemplate.Replace(fencedBashBlock(t, "<<'COUNTS'"))
	s = regexp.MustCompile(`(?m)^ID=<.*$`).ReplaceAllLiteralString(s, "ID="+id)
	s = regexp.MustCompile(`(?m)^NO=<.*$`).ReplaceAllLiteralString(s, "NO="+no)
	if note != "" {
		s = strings.Replace(s, "<<'NOTE'\nNOTE\n", "<<'NOTE'\n"+note+"\nNOTE\n", 1)
	}
	return s
}

// countRows は、最初の `<details>` より前にある、遷移表の周の行を数える。
func countRows(body string) int {
	row := regexp.MustCompile(`^\| *[0-9]+周目 *\|`)
	n := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "<details>") {
			break
		}
		if row.MatchString(line) {
			n++
		}
	}
	return n
}

// 目的: 判断票を貼る見本を実際に叩くと、遷移表に今回の周の行が1行ずつ足されることを固定する（設計 5-3u）。
//
// **なぜ要るか。**遷移表は、エージェントが手で書くのではなく、見本が前の判断票から作る。
// **見本のシェルが bash か zsh のどちらかで壊れると、遷移表の無い判断票が貼られるか、何も貼られない。**
// 文面の検査では、この壊れ方を拾えない（zsh では `$M1[...]` が配列の添字と読まれて落ちたことがある）。
//
// 与える情報: 段5 の見本と、偽の gh。前の判断票は、CRLF と先頭の空行を持つ形でも渡す。
// 成功条件: 前の判断票が無いときは1周目の行を持つ判断票を新しく貼る。在るときは書き換えで2周目の行と
// 2つ目の `<details>` を足し、削除の行に周の番号を付ける。60,000 文字を超えるときは、
// 前の周の中身を落とし、遷移表を写して新しく貼る。
func TestTemplate_判断票の見本は遷移表に今回の周を1行足す(t *testing.T) {
	for _, sh := range shellsForSample(t) {
		t.Run(sh, func(t *testing.T) {
			// 前の判断票が無い: 1周目として新しく貼る。
			out, patched, first := runSample(t, sampleRun{shell: sh, script: postSample(t, "", "1", "")})
			if patched != "" || !strings.HasPrefix(first, "<!-- continuo:agent -->\n<!-- design-review-result -->\n"+planTicketHeading+"\n") {
				t.Fatalf("前の判断票が無いのに、2行の印と題名で始まる判断票を新しく貼っていません\n%s\n%s", out, first)
			}
			if !strings.Contains(first, transitionHeader) || countRows(first) != 1 ||
				!strings.Contains(first, "<summary>1周目</summary>") {
				t.Fatalf("新しく貼った判断票に、1周目の遷移表か <summary> がありません\n%s", first)
			}

			// 前の判断票が在る（CRLF・先頭の空行つき）: 書き換えて2周目を足す。
			crlf := "\r\n\r\n" + strings.ReplaceAll(first, "\n", "\r\n")
			note := "don't `date` $HOME"
			out, second, posted := runSample(t, sampleRun{shell: sh, script: postSample(t, "123", "2", note), body: crlf})
			if posted != "" || second == "" {
				t.Fatalf("前の判断票が在るのに、書き換えずに新しく貼りました\n%s", out)
			}
			if countRows(second) != 2 || !strings.Contains(second, "| 2周目 |") ||
				strings.Count(second, "<details>") != 2 || !strings.Contains(second, "<summary>2周目</summary>") {
				t.Fatalf("書き換えた判断票に、2周目の行と2つ目の <details> がありません\n%s", second)
			}
			if !strings.Contains(second, "| 2周目 | <CRITICAL の件数> | <HIGH の件数> | <MEDIUM の件数> | <LOW の件数> |\n\n- 2周目で削除: "+note+"\n\n<details>") {
				t.Fatalf("削除の行が、周の番号つきでそのまま、遷移表と <details> から空行で離れて入っていません（引用符と $ を含む）\n%s", second)
			}

			// 続けて削った周: 削除の行は、前の周の削除の行のすぐ下に、空行を挟まずに並ぶ。
			_, third, _ := runSample(t, sampleRun{shell: sh, script: postSample(t, "123", "3", "y"), body: second})
			if !strings.Contains(third, "- 2周目で削除: "+note+"\n- 3周目で削除: y\n\n<details>") {
				t.Fatalf("2つの周の削除の行が、続けて並んでいません\n%s", third)
			}

			// 長さの上限を超える: 前の周の中身を落とし、遷移表を写して新しく貼る。
			long := second + strings.Repeat("x", 60000) + "\n"
			out, _, copied := runSample(t, sampleRun{shell: sh, script: postSample(t, "123", "3", ""), body: long})
			if countRows(copied) != 3 || strings.Count(copied, "<details>") != 1 ||
				!strings.Contains(copied, "<summary>3周目</summary>") || !strings.Contains(copied, "- 2周目で削除: ") {
				t.Fatalf("上限を超えたのに、遷移表と削除の行を写した新しい判断票になっていません\n%s\n%s", out, copied)
			}
		})
	}
}

// 目的: 前の判断票を探す見本が、gh で探せなかったときに「1周目」と言わずに止めることを固定する（設計 5-3u）。
//
// **なぜ要るか。**探せなかったことを「前の判断票が無い」と取り違えると、エージェントは1周目として新しく貼る。
// 前の判断票の続きが別のコメントに分かれ、周の番号も1に戻って、連続10回の数えが狂う。
// `PR=` を埋め忘れた実装の判断票も、同じ経路で止める。
//
// 与える情報: 段0 の見本。偽の gh は `issue view` と `pr view` を終了コード 1 で落とす。
// 成功条件: 計画でも実装でも「判断票を探せませんでした」と出て、「今回は1周目です」と出ない。
// `PR=` が空のときは gh を呼ばずに「PR= が空です」と出る。
func TestTemplate_前の判断票を探せないときは1周目にしない(t *testing.T) {
	find := fillTemplate.Replace(fencedBashBlock(t, findSampleMark))
	for _, sh := range shellsForSample(t) {
		t.Run(sh, func(t *testing.T) {
			for _, tc := range []struct{ name, script, want string }{
				{"計画で issue view が落ちる", find, "gh issue view が失敗しました"},
				{"実装で pr view が落ちる", asChangeReview(find, "7", "octocat/hello-world"), "gh pr view が失敗しました"},
			} {
				out, _, _ := runSample(t, sampleRun{shell: sh, script: tc.script, findID: "123", env: []string{"VIEW_FAIL=1"}})
				if !strings.Contains(out, "判断票を探せませんでした（"+tc.want+"）") || strings.Contains(out, "今回は1周目です") {
					t.Errorf("%s: 探せなかったのに止まっていないか、1周目と出しています: %q", tc.name, out)
				}
			}
			out, _, _, ghArgs := runSampleWithArgs(t, sampleRun{shell: sh, script: asChangeReview(find, "", "octocat/hello-world"), findID: "123"})
			if !strings.Contains(out, "判断票を探せませんでした（PR= が空です）") || strings.Contains(out, "今回は1周目です") {
				t.Errorf("PR= が空なのに止まっていません: %q", out)
			}
			if strings.Contains(ghArgs, "pr view") {
				t.Errorf("PR= が空なのに gh pr view を呼んでいます: %q", ghArgs)
			}
		})
	}
}

// 目的: 実装の判断票を、`PRREPO=` に書いたリポジトリで探し・書き換え・貼ることを固定する（設計 5-3u）。
//
// **なぜ要るか。**7-3 で別のリポジトリへ出した pull request でも、issue のリポジトリに固定されていると、
// **同じ番号の無関係な pull request の判断票を探して書き換えうる。**
// 計画の判断票（issue のコメント）は issue のリポジトリのままである。
//
// 与える情報: 段0 と段5 の見本。`KIND=実装`・`PR=7`・`PRREPO=octocat/spoon-knife`。
// 成功条件: 段0 が `pr view 7 --repo octocat/spoon-knife` と `repos/octocat/spoon-knife/issues/comments/123` を読み、
// 段5 が同じリポジトリのコメントを PATCH し、新しく貼るときは `pr comment 7 --repo octocat/spoon-knife` で貼る。
// どれも issue のリポジトリ（octocat/hello-world）へは届かない。計画の段5 は issue のリポジトリを PATCH する。
func TestTemplate_実装の判断票はPRREPOのリポジトリで扱う(t *testing.T) {
	const other = "octocat/spoon-knife"
	find := asChangeReview(fillTemplate.Replace(fencedBashBlock(t, findSampleMark)), "7", other)
	ticket := "<!-- code-review-result -->\n<!-- continuo:agent -->\n" + changeTicketHeading + "\n\n" +
		transitionHeader + "\n| --- | --- | --- | --- | --- |\n| 1周目 | 0 | 1 | 0 | 0 |\n\n" +
		"<details>\n<summary>1周目</summary>\n\nx\n\n</details>\n"
	for _, sh := range shellsForSample(t) {
		t.Run(sh, func(t *testing.T) {
			notIssueRepo := func(step, ghArgs string) {
				if strings.Contains(ghArgs, "octocat/hello-world") {
					t.Errorf("%s が、実装の判断票なのに issue のリポジトリへ届いています\n%s", step, ghArgs)
				}
			}
			out, _, _, ghArgs := runSampleWithArgs(t, sampleRun{shell: sh, script: find, body: ticket, findID: "123"})
			notIssueRepo("段0", ghArgs)
			for _, want := range []string{"pr view 7 --repo " + other + " ", "api repos/" + other + "/issues/comments/123 "} {
				if !strings.Contains(ghArgs, want) {
					t.Errorf("段0 が %q を呼んでいません\n%s\n%s", want, ghArgs, out)
				}
			}
			if !strings.Contains(out, "今回は 2 周目です") {
				t.Errorf("段0 が PRREPO の判断票から周の番号を出していません: %q", out)
			}

			_, patched, _, ghArgs := runSampleWithArgs(t, sampleRun{shell: sh, script: asChangeReview(postSample(t, "123", "2", ""), "7", other), body: ticket})
			notIssueRepo("段5 の書き換え", ghArgs)
			if !strings.Contains(ghArgs, "api --method PATCH repos/"+other+"/issues/comments/123 ") || countRows(patched) != 2 {
				t.Errorf("段5 が PRREPO のコメントを書き換えていません\n%s", ghArgs)
			}

			_, _, posted, ghArgs := runSampleWithArgs(t, sampleRun{shell: sh, script: asChangeReview(postSample(t, "", "1", ""), "7", other)})
			notIssueRepo("段5 の新しく貼る経路", ghArgs)
			if !strings.Contains(ghArgs, "pr comment 7 --repo "+other+" ") || !strings.HasPrefix(posted, "<!-- code-review-result -->\n<!-- continuo:agent -->\n") {
				t.Errorf("段5 が PRREPO の pull request へ新しく貼っていません\n%s", ghArgs)
			}

			_, _, _, ghArgs = runSampleWithArgs(t, sampleRun{shell: sh, script: postSample(t, "123", "2", ""), body: "<!-- continuo:agent -->\n<!-- design-review-result -->\n" + planTicketHeading + "\n\n" +
				transitionHeader + "\n| --- | --- | --- | --- | --- |\n| 1周目 | 0 | 1 | 0 | 0 |\n\n<details>\n<summary>1周目</summary>\n\nx\n\n</details>\n"})
			if !strings.Contains(ghArgs, "api --method PATCH repos/octocat/hello-world/issues/comments/123 ") {
				t.Errorf("計画の段5 が issue のリポジトリのコメントを書き換えていません\n%s", ghArgs)
			}
		})
	}
}

// 目的: 前の判断票を探す見本が、遷移表の最後の行から今回の周の番号を出すことを固定する（設計 5-3u）。
//
// **なぜ要るか。**レビュワーへ渡す「何周目か」と、連続10回の上限は、この番号で数える。
// **番号を出せないと、エージェントは周を数え違え、10回で止まれない。**
//
// 与える情報: 段0 の見本と、2周目まで入った前の判断票。
// 成功条件: 「今回は 3 周目です」と出る。前の判断票が無いときは「今回は1周目です」と出る。
func TestTemplate_前の判断票を探す見本は今回の周の番号を出す(t *testing.T) {
	find := fillTemplate.Replace(fencedBashBlock(t, findSampleMark))
	ticket := "<!-- continuo:agent -->\n<!-- design-review-result -->\n" + planTicketHeading + "\n\n" +
		transitionHeader + "\n| --- | --- | --- | --- | --- |\n| 1周目 | 0 | 1 | 0 | 0 |\n| 2周目 | 0 | 0 | 0 | 0 |\n\n" +
		"<details>\n<summary>1周目</summary>\n\n| 9周目 | 中の表は数えない |\n\n</details>\n"
	for _, sh := range shellsForSample(t) {
		t.Run(sh, func(t *testing.T) {
			out, _, _ := runSample(t, sampleRun{shell: sh, script: find, body: ticket, findID: "123"})
			if !strings.Contains(out, "今回は 3 周目です") {
				t.Errorf("2周目まで入った判断票から、今回の周を3と出していません: %q", out)
			}
			out, _, _ = runSample(t, sampleRun{shell: sh, script: find, findID: ""})
			if !strings.Contains(out, "今回は1周目です") {
				t.Errorf("前の判断票が無いのに、1周目と出していません: %q", out)
			}
		})
	}
}

// 目的: 書き足した判断票は run の成果に数えられないと正しく書かせ、止まるときは 3-7 の報告を書かせることを固定する（設計 5-3u）。
//
// **なぜ要るか。**continuo は、作成時刻が run の開始より後のコメントだけを成果として数える
// （internal/orchestrator/comment.go の hasRunComment）。**前の判断票へ書き足した判断票は数えられない。**
// 指示書が「判断票だけで成果になる」前提のままだと、連続10回で止まった run が判断票と `blocked` だけで終え、
// continuo はセッションを復元して書かせ直し、2度目も書かれなければ failure_state へ落とす。
//
// 与える情報: prompt.Builtin() の 3-2 と 5-6 の節。
// 成功条件: 3-2 が「新しく貼ったときだけ」「書き足したときは数えられない」と書き、旧い前提の文が無い。
// 5-6 の連続10回で止まる段と打ち切る段が、3-7 の報告を書いてから `blocked` を出させる。
// 印の立場が数えられないときも、応答ではなく 3-7 の報告へ書かせる。
func TestTemplate_書き足した判断票は成果に数えられないので報告を書いてから止めさせる(t *testing.T) {
	body := prompt.Builtin()
	plan := sectionOf(t, body, planReviewHeading)
	for _, want := range []struct{ needle, why string }{
		{"計画の判断票（issue のコメント）がその run の成果に数えられるのは、その run の中で新しく貼ったときだけです", "書き足した判断票が成果になると読めます"},
		{"前の判断票へ書き足したときは、順序が正しくても数えられません", "書き足した判断票が成果になると読めます"},
		{"実装の判断票（3-6。pull request のコメント）は、新しく貼っても数えられません", "実装の判断票なら新しく貼れば成果になると読めます"},
		{"判断票だけを書いて turn を終えないでください", "判断票だけで終えると、書かせ直しから failure_state へ落ちます"},
		{"そのことを 3-7 の報告の `### 詳細` に書いて人間へ渡してください", "印の立場が数えられないときに、判断票だけで止まります"},
	} {
		if !strings.Contains(plan, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", planReviewHeading, want.needle, want.why)
		}
	}
	for _, old := range []string{
		"入れ替えると、判断票だけを書いて turn を終えたときに",
		"そのことを応答に書いて人間へ渡してください",
	} {
		if strings.Contains(body, old) {
			t.Errorf("組み込みに %q が残っています。書き足した判断票は成果に数えられません", old)
		}
	}

	loop := sectionOf(t, body, reviewLoopHeading)
	for _, want := range []struct{ needle, why string }{
		{"止まるときは、3-7 の報告を書いてから、応答の最後に `CONTINUO-STATUS: blocked` を書いてください", "連続10回で止まるときに、判断票と blocked だけで終えます"},
		{"質問を判断票の中だけに書いて止まらないでください", "打ち切るときに、判断票の中の質問だけで止まります"},
	} {
		if !strings.Contains(loop, want.needle) {
			t.Errorf("%q の節に %q がありません。%s", reviewLoopHeading, want.needle, want.why)
		}
	}
	// 止まる段の2か所とも、実装の判断票（pull request のコメント）も数えられないと書く。
	// 「書き足した判断票は」だけだと、実装の判断票を新しく貼れば成果になると読める。
	if n := strings.Count(loop, "判断票は、前の判断票へ書き足したときも、実装の判断票（pull request のコメント）のときも、その run の成果に数えられません"); n != 2 {
		t.Errorf("%q の止まる段で、数えられない判断票の範囲を書き分けた文が %d か所です（2か所のはず）", reviewLoopHeading, n)
	}
	if strings.Contains(loop, "前の判断票へ書き足した判断票は、その run の成果に数えられません") {
		t.Errorf("%q に、計画の判断票にしか当たらない旧い文が残っています", reviewLoopHeading)
	}
}

// 目的: 人間に知らせることと止まる理由を、応答ではなく 3-7 の報告へ書かせることを固定する（設計 5-3u）。
//
// **なぜ要るか。**応答は issue に残らない。報告を書かずに `blocked` を出すと、continuo はセッションを
// 復元して書かせ直し、2度目も書かれなければ failure_state へ落とす。
// **例外は 3-1 で issue を読めなかったときだけである。**gh が落ちていると報告も書けないことがあるので、
// 書けなかったときに限って応答へ書かせる。
//
// 与える情報: prompt.Builtin() の全文。
// 成功条件: 「応答に書」「応答の最後に書いて」を含む行が、3-1 の「書けなかったら」の1行だけである。
// 3-1・3-6・7-2 の該当の段が 3-7 の報告へ書かせている。
func TestTemplate_知らせることは応答ではなく報告へ書かせる(t *testing.T) {
	body := prompt.Builtin()
	re := regexp.MustCompile(`応答に書|応答の最後に書いて`)
	var hits []string
	for _, line := range strings.Split(body, "\n") {
		if re.MatchString(line) {
			hits = append(hits, line)
		}
	}
	if len(hits) != 1 || !strings.Contains(hits[0], "書けなかったら") {
		t.Errorf("応答へ書かせる行は、3-1 の「報告を書けなかったら」の1行だけのはずです。%d 行あります:\n%s",
			len(hits), strings.Join(hits, "\n"))
	}
	for _, want := range []string{
		"取り込めなかったことを 3-7 の報告の `### 詳細` に書いてから、`CONTINUO-STATUS: blocked` を出してください",
		"読めなかったときは、その旨を 3-7 の報告の `### 詳細` に書いてから、`CONTINUO-STATUS: blocked` を出してください",
		"`gh run rerun` を叩かずに、そのことを 3-7 の報告の `### 詳細` に書いてください",
		"書き戻さず、そのことを 3-7 の報告の `### 詳細` に書いてください",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("組み込みに %q がありません。応答に書くと issue に残りません", want)
		}
	}
	if n := strings.Count(body, "`gh run rerun` を叩かずに、そのことを 3-7 の報告の `### 詳細` に書いてください"); n != 2 {
		t.Errorf("回し直す相手が無い2つの場面のうち、報告へ書かせているのが %d か所です", n)
	}
}

// 目的: 7-3 で別のリポジトリへ出したとき、検査の段の `--branch` と workflow の名前も出した先から決めさせることを固定する（設計 5-3u）。
//
// **なぜ要るか。**手元の worktree は issue のリポジトリである。`--repo` だけ置き換えても、
// `git branch --show-current` と `.github/workflows/` の検索は手元を見るので、出した先に無い branch や
// workflow の名前で run を探し、回し直す相手を取り違えるか、見つけられない。
//
// 与える情報: prompt.Builtin() の 3-6 の節の「貼ったら、検査を回し直す」。
// 成功条件: `--repo` だけでは足りないと書き、出した先の branch 名を `headRefName` で読ませ、
// 出した先の workflow を contents API で読ませる。
func TestTemplate_別のリポジトリへ出したときは検査のbranchとworkflowも出した先から決めさせる(t *testing.T) {
	section := sectionOf(t, prompt.Builtin(), changeReviewHeading)
	checks := section[strings.Index(section, "### 貼ったら、検査を回し直す"):]
	for _, want := range []string{
		"**`--repo` だけでは足りません。**",
		"`--branch` には、出した先の pull request の branch 名を書いてください",
		"gh pr view <PR番号> --repo <出した先> --json headRefName --jq .headRefName",
		"手元の `.github/workflows/` ではなく、出した先のリポジトリの workflow で決めてください",
		`gh api "repos/<出した先>/contents/.github/workflows?ref=<branch 名>" --jq '.[].path'`,
		`gh api "repos/<出した先>/contents/<そのパス>?ref=<branch 名>"`,
	} {
		if !strings.Contains(checks, want) {
			t.Errorf("「貼ったら、検査を回し直す」に %q がありません。7-3 のとき手元の branch と workflow を見ます", want)
		}
	}
}

// 目的: 段0 と段5 のあいだに前の判断票が消されたとき、段5 で止まり続けず段0 からやり直させることを固定する（設計 5-3u）。
//
// **なぜ要るか。**段5 は段0 が出した `ID=` を読み直す。消されていると、何度叩いても「判断票を読めませんでした」で止まる。
// **「`ID=` を空にして叩き直さない」と矛盾させない。**`ID=` は段0 が出し直した値に従わせる。
//
// 与える情報: prompt.Builtin() の 3-2 の節。
// 成功条件: 叩き直しても出るなら段0 からやり直す、`ID=` は段0 が出し直した値に従う、
// 段0 からやり直しても出るなら 3-7 の報告に書いて止まる、の3つがある。`ID=` を空にさせない文も残っている。
func TestTemplate_判断票を読めないままなら段0からやり直させる(t *testing.T) {
	plan := sectionOf(t, prompt.Builtin(), planReviewHeading)
	for _, want := range []string{
		"叩き直しても `判断票を読めませんでした` と出るときは、段0 からやり直してください",
		"`ID=` は、段0 が出し直した値に従います",
		"段0 からやり直しても `判断票を読めませんでした` と出るときは、そのことを 3-7 の報告の `### 詳細` に書き",
		"`ID=` を空にして叩き直さないでください",
	} {
		if !strings.Contains(plan, want) {
			t.Errorf("%q の節に %q がありません", planReviewHeading, want)
		}
	}
}

// 目的: 前の判断票の読み取り（gh api）が失敗したら、段0 も段5 も貼らずに止めることを固定する（設計 5-3u）。
//
// **なぜ要るか。**読み取りをパイプの先へ流すと、gh の失敗が消える。
// 段5 は空の本文を「旧い形」と取り違えて新しく1件貼り、前の判断票の続きが別のコメントに分かれる。
// 段0 は周の番号を出せずに「遷移表を読めません」と言い、段5 の新しく貼る経路へ進ませる。
// **段0 の「探せなかったことと、無いことを分ける」と食い違わせない。**
//
// 与える情報: 段0 と段5 の見本。偽の gh は、前の判断票の読み取りを本文を2行だけ出して終了コード 1 で落とす。
// 成功条件: 計画でも実装でも、段0 は「判断票を読めませんでした」と出し、周の番号を出さない。
// 段5 は「判断票を読めませんでした」と出し、PATCH も新しい投稿もしない。
// 段0 の止まったときの案内は、計画と実装で分かれている。
func TestTemplate_前の判断票を読み取れないときは貼らずに止める(t *testing.T) {
	ticket := "<!-- continuo:agent -->\n<!-- design-review-result -->\n" + planTicketHeading + "\n\n" +
		transitionHeader + "\n| --- | --- | --- | --- | --- |\n| 1周目 | 0 | 1 | 0 | 0 |\n\n" +
		"<details>\n<summary>1周目</summary>\n\nx\n\n</details>\n"
	find := fillTemplate.Replace(fencedBashBlock(t, findSampleMark))
	for _, sh := range shellsForSample(t) {
		t.Run(sh, func(t *testing.T) {
			for _, tc := range []struct{ name, find, post string }{
				{"計画", find, postSample(t, "123", "2", "")},
				{"実装", asChangeReview(find, "7", "octocat/hello-world"), asChangeReview(postSample(t, "123", "2", ""), "7", "octocat/hello-world")},
			} {
				env := []string{"API_FAIL=1"}
				out, _, _ := runSample(t, sampleRun{shell: sh, script: tc.find, body: ticket, findID: "123", env: env})
				if !strings.Contains(out, "判断票を読めませんでした（gh api が失敗しました）") || strings.Contains(out, "周目です") ||
					strings.Contains(out, "遷移表を読めません") {
					t.Errorf("%s の段0: 読み取りが失敗したのに止まっていないか、周の番号を出しています: %q", tc.name, out)
				}

				out, patched, posted := runSample(t, sampleRun{shell: sh, script: tc.post, body: ticket, env: env})
				if patched != "" || posted != "" {
					t.Errorf("%s の段5: 読み取りが失敗したのに貼っています\n%s\npatched=%q\nposted=%q", tc.name, out, patched, posted)
				}
				if !strings.Contains(out, "判断票を読めませんでした（gh api が失敗しました）。貼らずに止めました") {
					t.Errorf("%s の段5: 読み取りが失敗したことを出していません: %q", tc.name, out)
				}
			}
		})
	}

	plan := sectionOf(t, prompt.Builtin(), planReviewHeading)
	for _, want := range []string{
		"`判断票を探せませんでした` か `判断票を読めませんでした` と出たら、段5 へ進まないでください",
		"実装の判断票（`KIND=実装`）なら、`PR=` と `PRREPO=` を確かめてから",
		"計画の判断票（`KIND=計画`）なら、書き換える値はありません",
		"`判断票を読めませんでした` と出たら、何も貼られていません",
	} {
		if !strings.Contains(plan, want) {
			t.Errorf("%q の節に %q がありません。止まったときの案内が計画と実装で分かれていません", planReviewHeading, want)
		}
	}
}

// 目的: 7-3 で別のリポジトリへ出したとき、検査を回し直す3つのコマンドも出した先のリポジトリへ向けさせることを固定する（設計 5-3u）。
//
// **なぜ要るか。**`gh run list`・`gh run rerun`・`gh pr checks` は `--repo {{.issue.owner}}/{{.issue.repo}}` を直に書いている。
// 置き換えさせないと、issue のリポジトリの同じ番号の、無関係な pull request の検査を待つ。
//
// 与える情報: prompt.Builtin() の 3-6 の節。
// 成功条件: draft を外す段の置き換えの文が3つのコマンドも名指しし、検査の段にも置き換えの文がある。
// どちらも `PRREPO=` と同じ値と書く。
func TestTemplate_別のリポジトリへ出したときは検査の段もそのリポジトリへ向けさせる(t *testing.T) {
	section := sectionOf(t, prompt.Builtin(), changeReviewHeading)
	for _, want := range []string{
		"下の「貼ったら、検査を回し直す」の3つのコマンド（`gh run list`・`gh run rerun`・`gh pr checks`）の `--repo` を、出した先のリポジトリ（判断票の `PRREPO=` と同じ値）に置き換えてください",
		"7-3 で別のリポジトリへ出したときは、下の3つのコマンドの `--repo` を、出した先のリポジトリ（判断票の `PRREPO=` と同じ値）に置き換えてください",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("%q の節に %q がありません。7-3 のとき、無関係な pull request の検査を待ちます", changeReviewHeading, want)
		}
	}
	checks := section[strings.Index(section, "### 貼ったら、検査を回し直す"):]
	if !strings.Contains(checks, "下の3つのコマンドの `--repo` を、出した先のリポジトリ") {
		t.Error("「貼ったら、検査を回し直す」の段の中に、7-3 の置き換えの文がありません")
	}
}
