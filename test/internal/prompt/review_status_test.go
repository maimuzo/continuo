// issue #332（報告から、レビューが収まったか・周ごとの件数・draft か・マージできるか・残りの手順が分からない）
// で足した決まりと、その中身を出すスクリプト review-status.sh の検査である（設計 5-3zc）。
//
// **人間は「レビューを回したことは当たり前であり、なんの情報価値もない」と言っている。**
// 決まりの文がスキルから落ちると、報告はまた「レビューに回しました」だけになる。
//
// **外部へ1回も接続しない。**スクリプトを走らせる検査は、偽の gh を PATH の先頭に置き、
// 接続先も存在しないホストに向けてから叩く。
package prompt_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// reviewStatusHeading は、報告に置かせる節の見出しである。
const reviewStatusHeading = "## 報告: レビューの結果と、pull request の状態"

// 目的: finishing-run の段4 が、節を置く条件・叩く時点・2段の見本・`review` の表を持つことを固定する。
//
// 与える情報: スキル finishing-run の全文。
// 成功条件: 決まりの文がそれぞれ在ること。
func TestReviewStatus_報告の決まりが節と残りの表を持つ(t *testing.T) {
	body := skillText(t, "finishing-run")
	for _, want := range []struct{ needle, why string }{
		{"この branch の開いた pull request があるとき", "節を置く条件が無いと、置いたり置かなかったりします"},
		{"`review` でも `blocked` でも", "blocked で止まった報告から、収まったかが落ちます"},
		{"「posting-review-verdict」の「貼ったら、検査を回し直す」の段3（検査の待ち）を済ませてから叩きます", "検査が走っている最中に読むと、ほぼ毎回 BLOCKED と出ます"},
		{"CONTINUO_PLUGIN_DIR/skills/finishing-run/scripts/review-status.sh", "スクリプトの叩き方が無いと、記憶から書き写します"},
		{"スクリプトは1回だけ叩きます", "2回叩くと、節と残りが食い違います"},
		{"`exit=` が 0 でなければ", "引数の誤りで止まったとき、空の節が投稿されます"},
		{"`PRREPO=` を出した先", "別のリポジトリへ出したとき、無関係な pull request の状態を出します"},
		{"公開範囲が issue と違う", "private 側の検査の名前が public の issue に残ります"},
		{"「レビューに回しました」「In Review にしました」だけの題名や節を書きません", "情報の無い報告が続きます（issue #332 の本文）"},
		{"最初に当たった1行の手だけを書きます", "当たる行を全部書くと、逆の手が並びます"},
		{"その手は「人間が次にすること」にも書きます", "戻らない AI を人間が待ちます"},
		{"閉じるまでの手（この順）: pull request をマージする → issue が閉じたかを確かめ", "issue を閉じるまでの手順が落ちます"},
		{"`blocked` と `working` で終えるときは、この表を当てません", "質問で止まった報告に、マージの手が出ます"},
		{"作者は自分の pull request を承認できません", "1人で運用している利用者が行き止まりになります"},
	} {
		if !strings.Contains(body, want.needle) {
			t.Errorf("finishing-run に %q がありません。%s", want.needle, want.why)
		}
	}
	// 段B の見本: 受け入れ目標の節 → 段A の出力をつなぐ行 → 8つの見出しの案内、の順で並ぶ。
	b := fencedBashBlock(t, "cat '<段A が出した S のパス>' >> \"$F\"")
	g := strings.Index(b, goalsSectionHeading)
	c := strings.Index(b, "cat '<段A が出した S のパス>' >> \"$F\"")
	e := strings.Index(b, "ここに「writing-comments-for-humans」の8つの見出しで、何をしたかを書く（印は上の1行だけ）")
	if g < 0 || c < 0 || e < 0 || !(g < c && c < e) {
		t.Errorf("段B の見本で、受け入れ目標の節（%d）・段A の出力をつなぐ行（%d）・8つの見出しの案内（%d）がこの順に並んでいません\n%s", g, c, e, b)
	}
}

// 目的: 8つの見出しの例外と置き方の表、切り上げの報告と書かせ直しの文面が、新しい節を知っていることを固定する。
//
// **例外が無いと、「`## ` の節には8つ」がこの節にも当たる**（設計レビュー2周目の HIGH）。
//
// 与える情報: スキル writing-comments-for-humans と reporting-progress の全文。
// 成功条件: 例外の括弧と置き方の表と、切り上げの段に、節の名前が在ること。
func TestReviewStatus_ほかのスキルも節を知っている(t *testing.T) {
	humans := skillText(t, "writing-comments-for-humans")
	if !strings.Contains(humans, "報告に置く「レビューの結果と、pull request の状態」の節は除きます") {
		t.Error("writing-comments-for-humans の「節の中に、次の8つを」の例外に、新しい節がありません")
	}
	if !strings.Contains(humans, "開いた pull request があれば、その直後に「レビューの結果と、pull request の状態」の節を置きます") {
		t.Error("writing-comments-for-humans の置き方の表に、新しい節の置き場所がありません")
	}
	if strings.Contains(humans, "| 計画（「writing-plan」）・何をしたかの報告") {
		t.Error("置き方の表で、計画と報告が1行のままです。計画のコメントにも節を置けと読めます")
	}
	progress := skillText(t, "reporting-progress")
	if !strings.Contains(progress, "段4 の段A を叩いて、受け入れ目標の節の直後に「レビューの結果と、pull request の状態」の節を置きます") {
		t.Error("reporting-progress の切り上げの報告に、新しい節がありません")
	}
}

// reviewStatusGh は、review-status.sh を試す偽の gh である。
//
// 環境変数で返すものを決める。
//
//	PLAN_ID / IMPL_ID   find-verdict.sh が探す段で返す ID（空なら判断票が無い）
//	PLAN_BODY / IMPL_BODY  判断票の本文のファイル
//	SKIP_URL            pull request に貼られた、設計レビューを飛ばす断りの URL
//	STATE               `gh pr view --json url,isDraft,…` の出力（タブ区切りの1行）。空なら終了コード 1
//	REQ_OUT / REQ_ERR / REQ_RC  `gh pr checks --required` の標準出力・標準エラー・終了コード
//	ALL_OUT / ALL_ERR / ALL_RC  `gh pr checks` の標準出力・標準エラー・終了コード
const reviewStatusGh = `#!/bin/sh
ARGS="$*"
jq=
prev=
for a in "$@"; do [ "$prev" = --jq ] && jq=$a; prev=$a; done
case "$1 $2" in
  "issue view")
    echo "$PLAN_ID" ;;
  "pr view")
    case "$ARGS" in
      *"--json comments"*)
        case "$jq" in *design-review-skipped*) echo "$SKIP_URL" ;; *) echo "$IMPL_ID" ;; esac ;;
      *)
        [ -n "$STATE" ] || { echo "HTTP 502: Bad Gateway" >&2; exit 1; }
        printf '%b\n' "$STATE" ;;
    esac ;;
  "pr checks")
    case "$ARGS" in
      *--required*) printf '%b' "$REQ_OUT"; printf '%b' "$REQ_ERR" >&2; exit "${REQ_RC:-0}" ;;
      *) printf '%b' "$ALL_OUT"; printf '%b' "$ALL_ERR" >&2; exit "${ALL_RC:-0}" ;;
    esac ;;
  "api "*)
    id=${2##*/}
    f=
    [ -n "$PLAN_ID" ] && [ "$id" = "$PLAN_ID" ] && f=$PLAN_BODY
    [ -n "$IMPL_ID" ] && [ "$id" = "$IMPL_ID" ] && f=$IMPL_BODY
    [ -n "$f" ] || { echo '{"message":"Not Found"}'; exit 1; }
    if [ "$jq" = .body ]; then cat "$f"; else echo "https://github.com/octocat/hello-world/issues/1#issuecomment-$id"; cat "$f"; fi ;;
esac
`

// runReviewStatus は、段A の見本を、偽の gh を PATH の先頭に置いて走らせる。
//
// t: テストコンテキスト。
// shell: bash か zsh。
// env: 偽の gh へ渡す環境変数。
// 戻り値: 標準出力（段A の見本が出す exit= の行と、節の中身）と標準エラー、書き出した plugin のディレクトリ。
func runReviewStatus(t *testing.T, shell string, env ...string) (stdout, stderr, pluginDir string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(reviewStatusGh), 0o755); err != nil {
		t.Fatal(err)
	}
	sample := fillTemplate(t, fencedBashBlock(t, "scripts/review-status.sh'"))
	sample = strings.ReplaceAll(sample, "<PR番号>", "12")
	// 書き出した plugin のディレクトリ（出力に混ざってはいけない絶対パス）を、見本から拾っておく。
	if i := strings.Index(sample, "/skills/finishing-run/scripts/review-status.sh"); i > 0 {
		pluginDir = sample[strings.LastIndex(sample[:i], "'")+1 : i]
	}
	script := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(script, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{script}
	if shell == "zsh" {
		args = []string{"-f", script}
	}
	cmd := exec.Command(shell, args...)
	cmd.Env = append(os.Environ(), "GH_HOST=invalid.invalid", "GH_TOKEN=invalid", "PATH="+dir+":"+os.Getenv("PATH"))
	cmd.Env = append(cmd.Env, env...)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s で段A の見本が終了コード 0 以外で終わりました: %v\n%s\n%s", shell, err, out.String(), errb.String())
	}
	return out.String(), errb.String(), pluginDir
}

// writeVerdicts は、post-verdict.sh の実物で、計画の判断票（2周）を作ってファイルへ書く。
//
// **書く側（post-verdict.sh）と読む側（review-status.sh）の形が合っていることを、実物どうしで確かめる。**
//
// t: テストコンテキスト。
// shell: bash か zsh。
// 戻り値: 判断票の本文を書いたファイルのパス。
func writeVerdicts(t *testing.T, shell string) string {
	t.Helper()
	counts := func(s, c string) string {
		return strings.Replace(s, "| <CRITICAL の件数> | <HIGH の件数> | <MEDIUM の件数> | <LOW の件数> |", c, 1)
	}
	_, _, first := runSample(t, sampleRun{shell: shell, script: counts(postSample(t, "", "1", ""), "| 0 | 2 | 1 | 0 |")})
	_, second, _ := runSample(t, sampleRun{shell: shell, script: counts(postSample(t, "123", "2", ""), "| 0 | 0 | 1 | 1 |"), body: first})
	if second == "" {
		t.Fatalf("post-verdict.sh で2周目を書き足せませんでした\n%s", first)
	}
	f := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(f, []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

// writeBody は、判断票の本文をファイルへ書く。
func writeBody(t *testing.T, body string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

// 目的: 収まった判断票と、マージできる pull request の事実が、節にそのまま出ることを固定する。
//
// 与える情報: post-verdict.sh が書いた計画の判断票（2周目で収まった）と、実装の判断票（3周目で収まった）。
// pull request は draft でなく、検査は全部 pass、`CLEAN`。
// 成功条件: 見出し・2つの判定・遷移表の行・draft・検査の件数・承認・mergeStateStatus の行が出て、
// 終了コードが 0 で、手元の絶対パスが標準出力にも標準エラーにも出ないこと。
func TestReviewStatus_収まってマージできるときの節(t *testing.T) {
	impl := "<!-- code-review-result -->\n<!-- continuo:agent -->\n# レビューの判断票（実装）\n\n" +
		transitionHeader + "\n| --- | --- | --- | --- | --- |\n| 1周目 | 0 | 1 | 2 | 0 |\n| 2周目 | 0 | 1 | 0 | 1 |\n| 3周目 | 0 | 0 | 1 | 0 |\n\n<details>\n<summary>1周目</summary>\n\n| 4周目 | 9 | 9 | 9 | 9 |\n\n</details>\n"
	for _, sh := range shellsForSample(t) {
		t.Run(sh, func(t *testing.T) {
			out, errOut, pluginDir := runReviewStatus(t, sh,
				"PLAN_ID=111", "PLAN_BODY="+writeVerdicts(t, sh),
				"IMPL_ID=222", "IMPL_BODY="+writeBody(t, impl),
				"STATE=https://github.com/octocat/hello-world/pull/12\\tfalse\\tMERGEABLE\\tCLEAN\\t",
				"REQ_OUT=test\\tpass\\t1m\\thttps://x\\t\\nbuild\\tpass\\t2m\\thttps://x\\t\\n",
				"ALL_OUT=test\\tpass\\t1m\\thttps://x\\t\\nbuild\\tpass\\t2m\\thttps://x\\t\\nlint\\tskipping\\t0\\thttps://x\\t\\n",
			)
			for _, want := range []string{
				"exit=0 ",
				reviewStatusHeading,
				"- 設計レビュー: 収まった（2周目）",
				"| 1周目 | 0 | 2 | 1 | 0 |",
				"| 2周目 | 0 | 0 | 1 | 1 |",
				"- 実装レビュー: 収まった（3周目）",
				"| 3周目 | 0 | 0 | 1 | 0 |",
				"- pull request: https://github.com/octocat/hello-world/pull/12",
				"- draft: いいえ",
				"- 必須の検査: pass 2・fail 0・pending 0・skipping 0・cancel 0",
				"- 全部の検査: pass 2・fail 0・pending 0・skipping 1・cancel 0",
				"- 承認（reviewDecision）: 求められていない",
				"- mergeStateStatus: CLEAN（マージできる）",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("節に %q がありません\n%s", want, out)
				}
			}
			if strings.Contains(out, "| 4周目 |") {
				t.Errorf("<details> の中の行を遷移表として写しました\n%s", out)
			}
			if pluginDir == "" || strings.Contains(out, pluginDir) || strings.Contains(errOut, pluginDir) {
				t.Errorf("手元の絶対パス（%q）が出力に混ざっています（節は公開のコメントへ貼られます）\n%s\n%s", pluginDir, out, errOut)
			}
		})
	}
}

// 目的: 収まっていない・途中から始まる・飛ばした・読めない・落ちた検査を、推測で埋めずに出すことを固定する。
//
// **「読めなかった」を「無い」や「0件」と言わない。**gh は pull request が見つからないときも、
// 検査に fail があるときと同じ終了コード 1 を返す（設計レビュー3周目の HIGH）。
//
// 与える情報: 計画の判断票は無く、理由つきの飛ばした断りがある。実装の判断票は3周目から始まり、
// 4周目に削除がある。必須の検査は無く、全部の検査に fail と pending がある。
// 成功条件: 「飛ばした」「収まっていない（…削除あり）」「前の周は、別の判断票」「必須の検査なし」・落ちた検査の名前が出ること。
// 続けて、pull request も検査も読めないときに「読めませんでした」と出ること。
func TestReviewStatus_収まっていないときと読めないときの節(t *testing.T) {
	impl := "<!-- code-review-result -->\n<!-- continuo:agent -->\n# レビューの判断票（実装）\n\n" +
		transitionHeader + "\n| --- | --- | --- | --- | --- |\n| 3周目 | 0 | 1 | 0 | 0 |\n| 4周目 | 0 | 0 | 0 | 1 |\n\n- 4周目で削除: 頼まれていない検査\n\n<details>\n</details>\n"
	for _, sh := range shellsForSample(t) {
		t.Run(sh, func(t *testing.T) {
			out, _, _ := runReviewStatus(t, sh,
				"PLAN_ID=", "SKIP_URL=https://github.com/octocat/hello-world/pull/12#issuecomment-9",
				"IMPL_ID=222", "IMPL_BODY="+writeBody(t, impl),
				"STATE=https://github.com/octocat/hello-world/pull/12\\tfalse\\tMERGEABLE\\tUNSTABLE\\tREVIEW_REQUIRED",
				"REQ_ERR=no required checks reported on the 'x' branch\\n", "REQ_RC=1",
				"ALL_OUT=test\\tfail\\t1m\\thttps://x\\t\\nbuild\\tpending\\t0\\thttps://x\\t\\n", "ALL_RC=1",
			)
			for _, want := range []string{
				"- 設計レビュー: 飛ばした（断り: https://github.com/octocat/hello-world/pull/12#issuecomment-9）",
				"- 実装レビュー: 収まっていない（最後は 4周目。CRITICAL 0・HIGH 0・削除あり）",
				"前の周は、別の判断票にあります",
				"- 4周目で削除: 頼まれていない検査",
				"- 必須の検査: 必須の検査なし",
				"- 全部の検査: pass 0・fail 1・pending 1・skipping 0・cancel 0（落ちたもの: test）",
				"- 承認（reviewDecision）: REVIEW_REQUIRED（承認が要る）",
				"- mergeStateStatus: UNSTABLE（マージできるが、通っていない検査がある）",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("節に %q がありません\n%s", want, out)
				}
			}

			out, _, _ = runReviewStatus(t, sh,
				"PLAN_ID=", "SKIP_URL=", "IMPL_ID=", "STATE=",
				"REQ_ERR=GraphQL: Could not resolve to a PullRequest\\n", "REQ_RC=1",
				"ALL_ERR=GraphQL: Could not resolve to a PullRequest\\n", "ALL_RC=1",
			)
			for _, want := range []string{
				"exit=0 ",
				"- 設計レビュー: 判断票がありません",
				"- 実装レビュー: 判断票がありません",
				"- pull request: 読めませんでした",
				"- 必須の検査: 読めませんでした",
				"- 全部の検査: 読めませんでした",
				"- mergeStateStatus: 読めませんでした",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("読めないときの節に %q がありません\n%s", want, out)
				}
			}
			if strings.Contains(out, "fail 0") {
				t.Errorf("読めなかった検査を「fail 0」と出しました\n%s", out)
			}
		})
	}
}

// 目的: find-verdict.sh が見つけた判断票の本文を読めないとき、「無い」と言わないことを固定する。
//
// 与える情報: 実装の判断票の ID は見つかるが、本文の取得が落ちる。
// 成功条件: 「判断票を読めませんでした」と出て、「判断票がありません」と出ないこと。
func TestReviewStatus_判断票を読めないときは無いと言わない(t *testing.T) {
	for _, sh := range shellsForSample(t) {
		t.Run(sh, func(t *testing.T) {
			// IMPL_BODY を渡さないので、偽の gh の api は Not Found で落ちる。find-verdict.sh は遷移表を読めない文を出す。
			out, _, _ := runReviewStatus(t, sh,
				"PLAN_ID=", "SKIP_URL=", "IMPL_ID=222",
				"STATE=https://github.com/octocat/hello-world/pull/12\\ttrue\\tMERGEABLE\\tDRAFT\\t",
				"REQ_OUT=test\\tpending\\t0\\thttps://x\\t\\n", "REQ_RC=8",
				"ALL_OUT=test\\tpending\\t0\\thttps://x\\t\\n", "ALL_RC=8",
			)
			if !strings.Contains(out, "- 実装レビュー: 判断票を読めませんでした") || strings.Contains(out, "- 実装レビュー: 判断票がありません") {
				t.Errorf("読めなかった判断票を「無い」と出しました\n%s", out)
			}
			for _, want := range []string{"- draft: はい", "- 必須の検査: pass 0・fail 0・pending 1・skipping 0・cancel 0", "- mergeStateStatus: DRAFT（draft なのでマージできない）"} {
				if !strings.Contains(out, want) {
					t.Errorf("節に %q がありません（終了コード 8 の pending も読めたものとして数えます）\n%s", want, out)
				}
			}
		})
	}
}

// 目的: 引数の誤りでは何も読まず、終了コード 2 で止まり、標準出力に何も出さないことを固定する。
//
// **空の節が投稿されないようにする。**段A の見本は、終了コードが 0 でなければ段B へ進ませない。
//
// 与える情報: 置き換え語が残った引数と、知らない引数。
// 成功条件: どちらも終了コード 2 で、標準出力が空で、標準エラーに手元のパスが出ないこと。
func TestReviewStatus_引数の誤りは終了コード2(t *testing.T) {
	dir := t.TempDir()
	sample := fillTemplate(t, "sh 'CONTINUO_PLUGIN_DIR/skills/finishing-run/scripts/review-status.sh' \"$@\"\n")
	pluginDir := sample[strings.Index(sample, "'")+1 : strings.Index(sample, "/skills/finishing-run/")]
	script := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(script, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"ISSUE_URL=<issue の URL>", "ISSUE_REPO=octocat/hello-world", "PR=12"},
		{"ISSUE_URL=https://github.com/octocat/hello-world/issues/1", "ISSUE_REPO=octocat/hello-world", "PR=12", "KIND=実装"},
		{"ISSUE_URL=https://github.com/octocat/hello-world/issues/1", "ISSUE_REPO=octocat/hello-world", "PR=<PR番号>"},
	} {
		cmd := exec.Command("sh", append([]string{script}, args...)...)
		cmd.Env = append(os.Environ(), "GH_HOST=invalid.invalid", "GH_TOKEN=invalid", "PATH="+dir+":"+os.Getenv("PATH"))
		var out, errb strings.Builder
		cmd.Stdout = &out
		cmd.Stderr = &errb
		err := cmd.Run()
		ee, ok := err.(*exec.ExitError)
		if !ok || ee.ExitCode() != 2 {
			t.Errorf("引数 %q で、終了コード 2 で止まりません: %v", args, err)
		}
		if out.String() != "" {
			t.Errorf("引数 %q で、標準出力に何かを出しました（空の節が投稿されます）\n%s", args, out.String())
		}
		if strings.Contains(errb.String(), pluginDir) {
			t.Errorf("標準エラーに手元の絶対パスが出ています\n%s", errb.String())
		}
	}
}
