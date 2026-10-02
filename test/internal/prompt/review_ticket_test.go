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
const fakeGh = `#!/bin/sh
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
	return string(b), read("patched"), read("posted")
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
			if !strings.Contains(second, "- 2周目で削除: "+note) {
				t.Fatalf("削除の行が、周の番号つきでそのまま入っていません（引用符と $ を含む）\n%s", second)
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

// 目的: 前の判断票を探す見本が、遷移表の最後の行から今回の周の番号を出すことを固定する（設計 5-3u）。
//
// **なぜ要るか。**レビュワーへ渡す「何周目か」と、連続10回の上限は、この番号で数える。
// **番号を出せないと、エージェントは周を数え違え、10回で止まれない。**
//
// 与える情報: 段0 の見本と、2周目まで入った前の判断票。
// 成功条件: 「今回は 3 周目です」と出る。前の判断票が無いときは「今回は1周目です」と出る。
func TestTemplate_前の判断票を探す見本は今回の周の番号を出す(t *testing.T) {
	find := fillTemplate.Replace(fencedBashBlock(t, "LAST=$(gh api"))
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
