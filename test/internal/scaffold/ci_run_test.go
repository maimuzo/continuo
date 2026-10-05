package scaffold_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// このファイルは、検査の `run:` を**実際に走らせて**確かめる（issue #261）。
//
// **文字列の照合だけでは、判定の動きは確かめられない。**
// 雛形（continuo-ci.yaml）は、このリポジトリの CI では1度も走らない。
// **ここが通す経路だけが、配る前の検証である。**
//
// **やり方。**YAML から job の `run:` を取り出し、偽の `gh` を PATH の先頭に置いて、
// GitHub Actions と同じ `bash -e <ファイル>` で走らせる。
//
// **このテストが押さえていないもの。**偽の `gh` は、渡された `--jq` の式を本物の `jq` で
// 固定の JSON に当てる。**本物の `gh` は jq を内蔵しており、別の実装である。**
// 内蔵の実装との差は見えない（null の出し方だけは、実測に合わせて空文字へ直している）。
// 実機で通した経路と通せなかった経路は、docs/plans/continuo_design.md の 5-3o にある。

// fakeCIGH は偽の `gh` の中身である。
//
// **受ける呼び出しは2つだけである。**
//
//	gh api [--paginate] <パス> --jq <式>
//	gh pr view <番号> --repo <リポジトリ> --json closingIssuesReferences
//
// **応答は FAKE_GH_DIR の下のファイルで決める。**<パス> の英数字以外を `_` に置き換えた名前で引く。
//
//	<名前>.json  在れば、式を当てて標準出力へ出す。終了コード 0
//	<名前>.fail  在れば、中身を標準エラーへ出して終了コード 1。**標準出力へは誤りの JSON を1行出す**
//	             （本物の gh が、失敗したときにそうする。gh 2.100.0 で実測）
//	どちらも無い  HTTP 404 として扱う
//
// **呼ばれた引数を calls.log へ1行ずつ残す。**「権限の照会を叩いていない」ことを確かめるのに使う。
const fakeCIGH = `#!/usr/bin/env bash
set -u
dir="${FAKE_GH_DIR}"
printf '%s\n' "$*" >> "${dir}/calls.log"

fail() { # $1: 応答のファイル。無ければ 404
  if [ -n "$1" ] && [ -f "$1" ]; then
    printf '{"message":"failed","status":"500"}\n'
    cat "$1" >&2
  else
    printf '{"message":"Not Found","status":"404"}\n'
    echo "gh: Not Found (HTTP 404)" >&2
  fi
  exit 1
}

case "${1:-}" in
  api)
    shift
    path="" ; expr=""
    while [ "$#" -gt 0 ]; do
      case "$1" in
        --paginate) shift ;;
        --jq) expr="$2" ; shift 2 ;;
        *) path="$1" ; shift ;;
      esac
    done
    name="$(printf '%s' "${path}" | tr -c 'A-Za-z0-9' '_')"
    if [ -f "${dir}/${name}.fail" ]; then
      fail "${dir}/${name}.fail"
    fi
    if [ ! -f "${dir}/${name}.json" ]; then
      fail ""
    fi
    # 本物の gh は、値が null のとき空文字を出す（本物の jq -r は null と出す）。
    jq -r "${expr} | if . == null then \"\" else . end" "${dir}/${name}.json"
    ;;
  pr)
    if [ -f "${dir}/pr.fail" ]; then
      fail "${dir}/pr.fail"
    fi
    cat "${dir}/pr.json"
    ;;
  *)
    echo "偽の gh が知らない呼び出しです: $*" >&2
    exit 2
    ;;
esac
`

const (
	fakeCIRepo   = "octocat/hello-world"
	fakeCIPR     = "42"
	prComments   = "repos/octocat/hello-world/issues/42/comments?per_page=100"
	permissionOf = "repos/octocat/hello-world/collaborators/%s/permission"
)

// ghComment は、偽の `gh` が返すコメント1件である。
type ghComment struct {
	Body   string
	Login  string
	Assoc  string
	NoUser bool // 投稿者が消えたコメント（.user が null）
}

// ciRun は、1回の実行の準備と結果を持つ。
type ciRun struct {
	t   *testing.T
	dir string // 偽の gh の応答を置く場所
}

// newCIRun は、偽の `gh` と応答の置き場を用意する。
//
// **`bash` か `jq` が無いときは飛ばす。ただし CI では飛ばさずに落とす。**
// CI で黙って飛ぶと、検査の動きを何も確かめていない状態に誰も気づけない。
func newCIRun(t *testing.T) *ciRun {
	t.Helper()
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("CI") != "" {
				t.Fatalf("%s が見つかりません（CI では飛ばしません。検査の動きを確かめられなくなります）", tool)
			}
			t.Skipf("%s が見つからないので飛ばします", tool)
		}
	}
	r := &ciRun{t: t, dir: t.TempDir()}
	// **紐づく issue は、既定では0件にしておく。**要る場面だけ issues で足す。
	r.write("pr.json", `{"closingIssuesReferences":[]}`)
	return r
}

func (r *ciRun) write(name, content string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, name), []byte(content), 0o600); err != nil {
		r.t.Fatalf("応答のファイル %s を書けません: %v", name, err)
	}
}

// fixtureName は、API のパスを偽の `gh` が引く名前へ直す（偽の gh の tr と同じ規則）。
func fixtureName(path string) string {
	return regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(path, "_")
}

// comments は、path のコメントの一覧を置く。
func (r *ciRun) comments(path string, cs ...ghComment) {
	r.t.Helper()
	out := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		m := map[string]any{"id": len(out) + 1, "body": c.Body, "author_association": c.Assoc}
		if c.NoUser {
			m["user"] = nil
		} else {
			m["user"] = map[string]any{"login": c.Login}
		}
		out = append(out, m)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		r.t.Fatalf("コメントを JSON にできません: %v", err)
	}
	r.write(fixtureName(path)+".json", string(raw))
}

// failAt は、path の呼び出しを stderr の文面で失敗させる。
func (r *ciRun) failAt(path, stderr string) {
	r.write(fixtureName(path)+".fail", stderr+"\n")
}

// canPush は、login の権限の照会が push の真偽を返すようにする。
func (r *ciRun) canPush(login string, push bool) {
	v := "false"
	if push {
		v = "true"
	}
	r.write(fixtureName(strings.Replace(permissionOf, "%s", login, 1))+".json",
		`{"permission":"x","user":{"permissions":{"push":`+v+`}}}`)
}

// permissionRaw は、login の権限の照会の応答をそのまま置く。
func (r *ciRun) permissionRaw(login, body string) {
	r.write(fixtureName(strings.Replace(permissionOf, "%s", login, 1))+".json", body)
}

// permissionFails は、login の権限の照会を stderr の文面で失敗させる。
func (r *ciRun) permissionFails(login, stderr string) {
	r.failAt(strings.Replace(permissionOf, "%s", login, 1), stderr)
}

// issues は、pull request に紐づく issue を置く。
func (r *ciRun) issues(numbers ...string) {
	refs := make([]string, 0, len(numbers))
	for _, n := range numbers {
		refs = append(refs, `{"number":`+n+`,"url":"https://github.com/`+fakeCIRepo+`/issues/`+n+`"}`)
	}
	r.write("pr.json", `{"closingIssuesReferences":[`+strings.Join(refs, ",")+`]}`)
}

func issueComments(n string) string {
	return "repos/" + fakeCIRepo + "/issues/" + n + "/comments?per_page=100"
}

// result は、走らせた結果である。
type result struct {
	exit    int
	summary string // GITHUB_STEP_SUMMARY へ書かれたもの
	output  string // 標準出力と標準エラー
	calls   string // 偽の gh が受けた引数（1行に1回）
}

// permissionCalls は、権限の照会が何回呼ばれたかを数える。
func (res result) permissionCalls() int {
	return strings.Count(res.calls, "/collaborators/")
}

// run は、script を GitHub Actions と同じ形で走らせる。
//
// **`bash -e <ファイル>` で走らせる。**`run:` の既定の shell が、その形である。
// **一時ディレクトリの中で走らせる。**検査は作業ディレクトリへ matched.txt などを書くので、
// リポジトリの中で走らせると生成物が残る。
func (r *ciRun) run(script string, draft bool) result {
	r.t.Helper()
	work := r.t.TempDir()
	bin := r.t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeCIGH), 0o700); err != nil { //nolint:gosec // テストが走らせる偽の gh である
		r.t.Fatalf("偽の gh を置けません: %v", err)
	}
	scriptPath := filepath.Join(work, "run.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		r.t.Fatalf("検査のスクリプトを書けません: %v", err)
	}
	summary := filepath.Join(work, "summary.md")

	cmd := exec.Command("bash", "-e", scriptPath)
	cmd.Dir = work
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_GH_DIR="+r.dir,
		"GH_TOKEN=dummy",
		"REPO="+fakeCIRepo,
		"PR_NUMBER="+fakeCIPR,
		"IS_DRAFT="+map[bool]string{true: "true", false: "false"}[draft],
		"GITHUB_STEP_SUMMARY="+summary,
		"GITHUB_SERVER_URL=https://github.com",
	)
	out, err := cmd.CombinedOutput()
	res := result{output: string(out)}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.exit = exitErr.ExitCode()
	default:
		r.t.Fatalf("検査のスクリプトを走らせられません: %v", err)
	}
	if raw, err := os.ReadFile(summary); err == nil {
		res.summary = string(raw)
	}
	if raw, err := os.ReadFile(filepath.Join(r.dir, "calls.log")); err == nil {
		res.calls = string(raw)
	}
	return res
}

// runOf は、YAML の全文から job の最初の step の `run:` を取り出す。
func runOf(t *testing.T, yamlText, job string) string {
	t.Helper()
	var parsed struct {
		Jobs map[string]struct {
			Steps []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(yamlText), &parsed); err != nil {
		t.Fatalf("YAML として解釈できません: %v", err)
	}
	j, ok := parsed.Jobs[job]
	if !ok || len(j.Steps) != 1 || j.Steps[0].Run == "" {
		t.Fatalf("job %q の step が1つではないか、run がありません（関数を step をまたいで使えなくなります）", job)
	}
	return j.Steps[0].Run
}

func templateRun(t *testing.T, job string) string {
	t.Helper()
	return runOf(t, scaffold.CITemplate(), job)
}

func reviewGateRun(t *testing.T, job string) string {
	t.Helper()
	raw, err := os.ReadFile(reviewGatePath)
	if err != nil {
		t.Fatalf("このリポジトリの CI を読めません（%s）: %v", reviewGatePath, err)
	}
	return runOf(t, string(raw), job)
}

const (
	codeMarker   = "<!-- code-review-result -->\n判断票"
	designMarker = "<!-- continuo:agent -->\n<!-- design-review-result -->\n判断票"
	skipMarker   = "<!-- design-review-skipped -->\n文書だけの変更のため"
)

func wantExit(t *testing.T, res result, want int) {
	t.Helper()
	if res.exit != want {
		t.Fatalf("終了コードが %d です（%d のはず）\n--- 出力 ---\n%s\n--- 偽の gh が受けた呼び出し ---\n%s",
			res.exit, want, res.output, res.calls)
	}
}

func wantSummary(t *testing.T, res result, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(res.summary, w) {
			t.Errorf("案内に %q がありません\n--- 案内 ---\n%s", w, res.summary)
		}
	}
}

func wantNoSummary(t *testing.T, res result, ngs ...string) {
	t.Helper()
	for _, ng := range ngs {
		if strings.Contains(res.summary, ng) {
			t.Errorf("案内に %q が出ています（出てはいけません）\n--- 案内 ---\n%s", ng, res.summary)
		}
	}
}

// 目的: 実装のレビュー結果を、雛形が「立場が当たるか、または push できるか」で数えることを確かめる（issue #261）。
//
// **立場だけで数えると、書き込める人のコメントが数えられないリポジトリがある。**
// 検査の GITHUB_TOKEN からは、その人の立場が MEMBER に見えないことがあるためである。
//
// 与える情報: 偽の gh が返す、pull request のコメントと、投稿者ごとの権限の照会の応答。
// 成功条件: 場面ごとの終了コードと案内が、下の表のとおりであること。
func TestCITemplate_実装のレビュー結果を立場かpushできるかで数える(t *testing.T) {
	script := templateRun(t, "code-review-result")

	t.Run("立場が当たれば数え、権限の照会を1回も叩かない", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments, ghComment{Body: codeMarker, Login: "octocat", Assoc: "MEMBER"})
		res := r.run(script, false)
		wantExit(t, res, 0)
		wantSummary(t, res, "実装のレビュー結果=有り")
		if n := res.permissionCalls(); n != 0 {
			t.Errorf("権限の照会を %d 回叩いています（立場が当たれば叩かないはずです）\n%s", n, res.calls)
		}
	})

	t.Run("立場が当たる人と外れる人が混ざっていても、照会を叩かない", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments,
			ghComment{Body: codeMarker, Login: "hubot", Assoc: "NONE"},
			ghComment{Body: codeMarker, Login: "octocat", Assoc: "OWNER"})
		res := r.run(script, false)
		wantExit(t, res, 0)
		if n := res.permissionCalls(); n != 0 {
			t.Errorf("権限の照会を %d 回叩いています\n%s", n, res.calls)
		}
	})

	t.Run("立場は外れたが push できる人のコメントを数える", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments, ghComment{Body: codeMarker, Login: "hubot", Assoc: "CONTRIBUTOR"})
		r.canPush("hubot", true)
		res := r.run(script, false)
		wantExit(t, res, 0)
		wantSummary(t, res, "実装のレビュー結果=有り")
	})

	t.Run("立場が外れ push もできない人のコメントは数えず、見えた立場を案内に出す", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments, ghComment{Body: codeMarker, Login: "stranger", Assoc: "NONE"})
		r.canPush("stranger", false)
		res := r.run(script, false)
		wantExit(t, res, 1)
		wantSummary(t, res,
			"数える条件に当たる投稿者のものが1件もありません",
			"- stranger（この検査からは NONE と見えています）")
		wantNoSummary(t, res, "実装のレビュー結果=有り")
	})

	t.Run("1人目が404でも、2人目が push できれば数える", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments,
			ghComment{Body: codeMarker, Login: "aaa-gone", Assoc: "NONE"},
			ghComment{Body: codeMarker, Login: "bbb-writer", Assoc: "CONTRIBUTOR"})
		r.canPush("bbb-writer", true) // aaa-gone は応答を置かないので 404 になる
		res := r.run(script, false)
		wantExit(t, res, 0)
	})

	t.Run("照会が404だけなら数えず、原因を言い切らずに名前を出す", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments, ghComment{Body: codeMarker, Login: "gone", Assoc: "NONE"})
		res := r.run(script, false)
		wantExit(t, res, 1)
		wantSummary(t, res, "権限の照会が 404 でした", "存在しないか、この検査のトークンからは見えません", "- gone")
	})

	t.Run("照会が404以外で失敗したら、確かめられなかったと案内する", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments, ghComment{Body: codeMarker, Login: "hubot", Assoc: "CONTRIBUTOR"})
		r.permissionFails("hubot", "gh: Server Error (HTTP 500)")
		res := r.run(script, false)
		wantExit(t, res, 1)
		wantSummary(t, res, "「権限が無い」ではなく「確かめられなかった」", "HTTP 500", "回し直しても同じなら")
	})

	t.Run("照会の値が true でも false でもなければ、黙って数え落とさずに案内する", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments, ghComment{Body: codeMarker, Login: "hubot", Assoc: "CONTRIBUTOR"})
		r.permissionRaw("hubot", `{"permission":"write"}`) // .user.permissions.push が無い応答
		res := r.run(script, false)
		wantExit(t, res, 1)
		wantSummary(t, res, "権限を表す値が true / false ではありませんでした")
	})

	t.Run("push できる人が見つかったら、残りの人は叩かない", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments,
			ghComment{Body: codeMarker, Login: "aaa-writer", Assoc: "CONTRIBUTOR"},
			ghComment{Body: codeMarker, Login: "zzz-other", Assoc: "NONE"})
		r.canPush("aaa-writer", true)
		r.canPush("zzz-other", false)
		res := r.run(script, false)
		wantExit(t, res, 0)
		if strings.Contains(res.calls, "/collaborators/zzz-other/") {
			t.Errorf("push できる人が見つかったあとも照会を叩いています\n%s", res.calls)
		}
	})

	t.Run("同じ人が何件貼っていても、照会は1回", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments,
			ghComment{Body: codeMarker, Login: "stranger", Assoc: "NONE"},
			ghComment{Body: codeMarker, Login: "stranger", Assoc: "NONE"},
			ghComment{Body: codeMarker, Login: "stranger", Assoc: "NONE"})
		r.canPush("stranger", false)
		res := r.run(script, false)
		wantExit(t, res, 1)
		if n := res.permissionCalls(); n != 1 {
			t.Errorf("権限の照会を %d 回叩いています（1回のはず）\n%s", n, res.calls)
		}
	})

	t.Run("投稿者が消えたコメントは数えず、照会も叩かない", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments, ghComment{Body: codeMarker, Assoc: "NONE", NoUser: true})
		res := r.run(script, false)
		wantExit(t, res, 1)
		if n := res.permissionCalls(); n != 0 {
			t.Errorf("権限の照会を %d 回叩いています\n%s", n, res.calls)
		}
	})

	t.Run("目印が本文の途中にあるコメントは数えず、照会も叩かない", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments,
			ghComment{Body: "レビューの話です。\n" + codeMarker, Login: "octocat", Assoc: "OWNER"},
			// 設計の側と違い、continuo:agent の直後は許さない。
			ghComment{Body: "<!-- continuo:agent -->\n" + codeMarker, Login: "octocat", Assoc: "OWNER"})
		res := r.run(script, false)
		wantExit(t, res, 1)
		wantSummary(t, res, "次の目印で始まるものが1件もありません")
		if n := res.permissionCalls(); n != 0 {
			t.Errorf("権限の照会を %d 回叩いています\n%s", n, res.calls)
		}
	})

	t.Run("コメントの取得に失敗したら、数えずに案内を出して落ちる", func(t *testing.T) {
		r := newCIRun(t)
		r.failAt(prComments, "gh: Bad Gateway (HTTP 502)")
		res := r.run(script, false)
		wantExit(t, res, 1)
		wantSummary(t, res, "コメントを読めませんでした")
		wantNoSummary(t, res, "実装のレビュー結果=有り")
	})
}

// 目的: 設計のレビュー結果と、設計のレビューを飛ばす断りを、雛形が同じ条件で数えることを確かめる（issue #261）。
//
// **照合は job の中に2つある（断り・紐づく issue のレビュー結果）。**
// 片方だけ条件を書き間違えても、実装のレビュー結果のテストは通ってしまう。
// **断りの側が緩いと、誰でも断りを貼れば設計のレビューを抜けられる。**
//
// 与える情報: 偽の gh が返す、pull request のコメント・紐づく issue・issue のコメント・権限の照会の応答。
// 成功条件: 場面ごとの終了コードと案内が、下の表のとおりであること。
func TestCITemplate_設計のレビュー結果と断りを立場かpushできるかで数える(t *testing.T) {
	script := templateRun(t, "design-review-result")

	t.Run("立場が当たる人の断りを数え、照会を叩かない", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments, ghComment{Body: skipMarker, Login: "octocat", Assoc: "OWNER"})
		res := r.run(script, false)
		wantExit(t, res, 0)
		wantSummary(t, res, "断りが貼られています")
		if n := res.permissionCalls(); n != 0 {
			t.Errorf("権限の照会を %d 回叩いています\n%s", n, res.calls)
		}
	})

	t.Run("立場は外れたが push できる人の断りを数える", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments, ghComment{Body: skipMarker, Login: "hubot", Assoc: "CONTRIBUTOR"})
		r.canPush("hubot", true)
		res := r.run(script, false)
		wantExit(t, res, 0)
		wantSummary(t, res, "断りが貼られています")
	})

	t.Run("立場が外れ push もできない人の断りは数えず、案内に出す", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments, ghComment{Body: skipMarker, Login: "stranger", Assoc: "NONE"})
		r.canPush("stranger", false)
		res := r.run(script, false)
		wantExit(t, res, 1)
		wantNoSummary(t, res, "断りが貼られています")
		wantSummary(t, res,
			"断り（design-review-skipped）の目印で始まるコメントは在りますが",
			"- stranger（この検査からは NONE と見えています）")
	})

	t.Run("理由の無い断りは、立場が当たっても数えない", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments, ghComment{Body: "<!-- design-review-skipped -->\n", Login: "octocat", Assoc: "OWNER"})
		res := r.run(script, false)
		wantExit(t, res, 1)
		wantNoSummary(t, res, "断りが貼られています")
	})

	t.Run("断りの取得に失敗したら、数えずに案内を出して落ちる", func(t *testing.T) {
		r := newCIRun(t)
		r.failAt(prComments, "gh: Bad Gateway (HTTP 502)")
		res := r.run(script, false)
		wantExit(t, res, 1)
		wantSummary(t, res, "コメントを読めませんでした")
		// **gh は失敗したとき、誤りの JSON を標準出力へ1行出す。**それを1件と数えてはならない。
		wantNoSummary(t, res, "断りが貼られています")
	})

	t.Run("紐づく issue の、立場が当たる人の判断票を数え、照会を叩かない", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments)
		r.issues("7")
		r.comments(issueComments("7"), ghComment{Body: designMarker, Login: "octocat", Assoc: "COLLABORATOR"})
		res := r.run(script, false)
		wantExit(t, res, 0)
		wantSummary(t, res, "設計のレビュー結果=有り（issue #7）")
		if n := res.permissionCalls(); n != 0 {
			t.Errorf("権限の照会を %d 回叩いています\n%s", n, res.calls)
		}
	})

	t.Run("紐づく issue の、立場は外れたが push できる人の判断票を数える", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments)
		r.issues("7")
		r.comments(issueComments("7"), ghComment{Body: designMarker, Login: "hubot", Assoc: "CONTRIBUTOR"})
		r.canPush("hubot", true)
		res := r.run(script, false)
		wantExit(t, res, 0)
		wantSummary(t, res, "設計のレビュー結果=有り（issue #7）")
	})

	t.Run("紐づく issue に push できない人の判断票しか無ければ数えず、断りの分と分けて案内する", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments, ghComment{Body: skipMarker, Login: "skipper", Assoc: "NONE"})
		r.canPush("skipper", false)
		r.issues("7")
		r.comments(issueComments("7"), ghComment{Body: designMarker, Login: "stranger", Assoc: "FIRST_TIME_CONTRIBUTOR"})
		r.canPush("stranger", false)
		res := r.run(script, false)
		wantExit(t, res, 1)
		wantSummary(t, res,
			"数える条件に当たる投稿者のものが1件もありません",
			"断り（design-review-skipped）の目印で始まるコメントは在りますが",
			"- skipper（この検査からは NONE と見えています）",
			"設計のレビュー結果（design-review-result）の目印で始まるコメントは在りますが",
			"- stranger（この検査からは FIRST_TIME_CONTRIBUTOR と見えています）",
			"continuo が起動したエージェントは、この断りを自分で貼りません",
			"gh run rerun")
	})

	t.Run("1件目の issue を読めなくても、2件目の判断票を数える", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments)
		r.issues("7", "8")
		r.failAt(issueComments("7"), "gh: Bad Gateway (HTTP 502)")
		r.comments(issueComments("8"), ghComment{Body: designMarker, Login: "octocat", Assoc: "OWNER"})
		res := r.run(script, false)
		wantExit(t, res, 0)
		wantSummary(t, res, "設計のレビュー結果=有り（issue #8）")
	})

	t.Run("issue を1件も読めなければ、貼られていないではなく確かめられなかったと案内する", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments)
		r.issues("7")
		r.failAt(issueComments("7"), "gh: Bad Gateway (HTTP 502)")
		res := r.run(script, false)
		wantExit(t, res, 1)
		wantSummary(t, res, "このうち 1 件は、コメントを読めませんでした", "「確かめられなかった」")
		wantNoSummary(t, res, "設計のレビュー結果=有り")
	})

	t.Run("紐づく issue が無ければ落ち、draft なら gh pr ready を案内する", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments)
		res := r.run(script, true)
		wantExit(t, res, 1)
		wantSummary(t, res, "紐づく issue が1件もありません", "gh pr ready "+fakeCIPR)
	})
}

// shellFunc は、script から名前が name の関数の定義を取り出す。
//
// `<name>() {` の行から、行頭の `}` の行までを返す。見つからなければ空文字を返す。
func shellFunc(script, name string) string {
	start := strings.Index(script, name+"() {\n")
	if start < 0 {
		return ""
	}
	end := strings.Index(script[start:], "\n}\n")
	if end < 0 {
		return ""
	}
	return script[start : start+end+len("\n}\n")]
}

// 目的: 2つの job が持つ関数が、同じ中身であることを確かめる（issue #261）。
//
// **YAML の job をまたいで関数を共有できないので、同じものを2つ置いてある。**
// **片方だけ直すと、設計と実装で数え方が食い違う。**雛形のコメントで「両方を直してください」と
// 名乗っているだけでは揃わない。
//
// 与える情報: 雛形の2つの job の run。
// 成功条件: has_pusher・poster_counts・poster_notes の定義が、2つの job で1文字も違わないこと。
func TestCITemplate_2つのjobの関数が同じ中身である(t *testing.T) {
	design := templateRun(t, "design-review-result")
	code := templateRun(t, "code-review-result")
	for _, name := range []string{"has_pusher", "poster_counts", "poster_notes"} {
		d, c := shellFunc(design, name), shellFunc(code, name)
		if d == "" || c == "" {
			t.Errorf("関数 %s の定義を取り出せません（design=%d バイト、code=%d バイト）", name, len(d), len(c))
			continue
		}
		if d != c {
			t.Errorf("関数 %s の中身が、2つの job で違います\n--- design-review-result ---\n%s\n--- code-review-result ---\n%s", name, d, c)
		}
	}
}

// 目的: このリポジトリ自身の検査が、コメントの取得に失敗したときに「通る側」へ倒れないことを確かめる（issue #261）。
//
// **この検査は必須の検査で、逃がし口が無い。**取得の失敗を守る枝は、
// pull request の run では通らない（API が落ちたときにしか通らない）。**ここでしか確かめられない。**
//
// **gh は失敗したとき、誤りの JSON を標準出力へ1行出す。**守りの中で終わらせないと、
// その1行を「1件ある」と数えて緑になる。**レビューを飛ばして通る枝ができる。**
//
// 与える情報: 偽の gh が返す、pull request のコメント（成功・失敗）。
// 成功条件: 取得の失敗では赤になり、立場が当たるコメントでは緑になり、無ければ赤になること。
func TestReviewGate_取得に失敗しても通る側へ倒れない(t *testing.T) {
	design := reviewGateRun(t, "design-review-result")
	code := reviewGateRun(t, "code-review-result")

	t.Run("実装: 取得に失敗したら赤", func(t *testing.T) {
		r := newCIRun(t)
		r.failAt(prComments, "gh: Bad Gateway (HTTP 502)")
		res := r.run(code, false)
		wantExit(t, res, 1)
		wantSummary(t, res, "コメントを読めませんでした")
		wantNoSummary(t, res, "レビュー結果=有り")
	})

	t.Run("実装: 立場が当たるコメントがあれば緑", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments, ghComment{Body: codeMarker, Login: "octocat", Assoc: "OWNER"})
		res := r.run(code, false)
		wantExit(t, res, 0)
		wantSummary(t, res, "レビュー結果=有り")
	})

	t.Run("実装: 立場が外れたコメントしか無ければ赤（ここは立場だけで数える）", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments, ghComment{Body: codeMarker, Login: "hubot", Assoc: "CONTRIBUTOR"})
		r.canPush("hubot", true)
		res := r.run(code, false)
		wantExit(t, res, 1)
		if n := res.permissionCalls(); n != 0 {
			t.Errorf("権限の照会を %d 回叩いています（このリポジトリの検査は照会しません）\n%s", n, res.calls)
		}
	})

	t.Run("設計: 断りの取得に失敗したら赤", func(t *testing.T) {
		r := newCIRun(t)
		r.failAt(prComments, "gh: Bad Gateway (HTTP 502)")
		res := r.run(design, false)
		wantExit(t, res, 1)
		wantSummary(t, res, "コメントを読めませんでした")
		wantNoSummary(t, res, "断りが貼られています")
	})

	t.Run("設計: 立場が当たる人の断りがあれば緑", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments, ghComment{Body: skipMarker, Login: "octocat", Assoc: "OWNER"})
		res := r.run(design, false)
		wantExit(t, res, 0)
		wantSummary(t, res, "断りが貼られています")
	})

	t.Run("設計: 紐づく issue の判断票があれば緑", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments)
		r.issues("7")
		r.comments(issueComments("7"), ghComment{Body: designMarker, Login: "octocat", Assoc: "OWNER"})
		res := r.run(design, false)
		wantExit(t, res, 0)
		wantSummary(t, res, "設計のレビュー結果=有り（issue #7）")
	})

	t.Run("設計: 何も貼っていなければ赤で、draft でないときの回し直し方を案内する", func(t *testing.T) {
		r := newCIRun(t)
		r.comments(prComments)
		r.issues("7")
		r.comments(issueComments("7"))
		res := r.run(design, false)
		wantExit(t, res, 1)
		wantSummary(t, res, "gh run rerun")
	})
}
