// スキル「implementing-and-pushing」の scripts/link-branch.sh を、偽の gh と git で叩く検査である（issue #321）。
//
// **外部へ1回も接続しない。**gh と git は PATH の先頭に置いた偽物で、受け取った引数を控え、
// 環境変数で決めた値を返す。
package prompt_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/agentplugin"
)

// fakeLinkGh は、link-branch.sh が叩く gh の偽物である。
//
// **mutation を受けたら `mutated` を作る。**結びつけたかどうかは、このファイルの有無で見る。
const fakeLinkGh = `#!/bin/sh
printf '%s\n' "$*" >> "$OUT/gh.log"
case "$*" in
  *createLinkedBranch*)
    : > "$OUT/mutated"
    printf '%s\n' "$*" > "$OUT/mutation"
    printf '%s\n' "$FAKE_CREATED"
    ;;
  *'api graphql'*) printf '%s\n' "$FAKE_IDS" ;;
  *nameWithOwner*) printf '%s\n' "$FAKE_ORIGIN" ;;
  *defaultBranchRef*) printf '%s\n' "$FAKE_DEFAULT" ;;
esac
exit 0
`

// fakeLinkGit は、link-branch.sh が叩く git の偽物である。
const fakeLinkGit = `#!/bin/sh
printf '%s\n' "$*" >> "$OUT/git.log"
case "$1" in
  branch) printf '%s\n' "$FAKE_BRANCH" ;;
  ls-remote) exit "$FAKE_LSREMOTE_RC" ;;
  fetch) exit "${FAKE_FETCH_RC:-0}" ;;
  merge-base) printf '%s\n' "$FAKE_MERGEBASE" ;;
esac
exit 0
`

// linkRun は、link-branch.sh を1回叩いた結果である。
type linkRun struct {
	out      string // 標準出力と標準エラー
	mutated  bool   // createLinkedBranch を叩いたか
	mutation string // createLinkedBranch に渡した引数
}

// runLinkBranch は、偽の gh と git を PATH の先頭に置いて link-branch.sh を叩く。
//
// t: テストコンテキスト。
// url: スクリプトへ渡す issue の URL。
// env: 偽物の返す値（FAKE_ で始まる環境変数）。
// 戻り値: 叩いた結果。
func runLinkBranch(t *testing.T, url string, env map[string]string) linkRun {
	t.Helper()
	script, ok := agentplugin.RawFile("skills/implementing-and-pushing/scripts/link-branch.sh")
	if !ok {
		t.Fatal("link-branch.sh が埋め込みにありません")
	}
	dir := t.TempDir()
	for name, body := range map[string]string{"gh": fakeLinkGh, "git": fakeLinkGit} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	scriptFile := filepath.Join(dir, "link-branch.sh")
	if err := os.WriteFile(scriptFile, script, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", scriptFile, url)
	cmd.Env = append(os.Environ(), "OUT="+dir, "PATH="+dir+":"+os.Getenv("PATH"),
		"GH_HOST=invalid.invalid", "GH_TOKEN=invalid")
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("link-branch.sh が終了コード 0 以外で終わりました: %v\n%s", err, b)
	}
	r := linkRun{out: string(b)}
	if _, err := os.Stat(filepath.Join(dir, "mutated")); err == nil {
		r.mutated = true
		m, _ := os.ReadFile(filepath.Join(dir, "mutation"))
		r.mutation = string(m)
	}
	return r
}

// linkBase は、4つの条件が全部そろう値である。各ケースはここから1つだけ崩す。
func linkBase() map[string]string {
	return map[string]string{
		"FAKE_BRANCH":      "continuo/octocat/hello-world/42",
		"FAKE_ORIGIN":      "octocat/hello-world",
		"FAKE_LSREMOTE_RC": "2",
		"FAKE_IDS":         "R_repo I_issue 0",
		"FAKE_DEFAULT":     "main",
		"FAKE_MERGEBASE":   "0123456789abcdef0123456789abcdef01234567",
		"FAKE_CREATED":     "continuo/octocat/hello-world/42",
	}
}

// 目的: 4つの条件がそろうときだけ branch を issue に結びつけることを固定する（issue #321）。
//
// **なぜ要るか。**continuo は issue にリンクされた branch がちょうど1本なら worktree の起点にする（設計 3-22d）。
// 人間が起点にリンクした branch の上に足すと2本になり、起点が既定の branch に倒れる。
// remote に在る branch を渡したときの動きは測っていないので、在れば何もしない。
// 確かめられなかったときに「無い」と扱うと、その測っていない道へ入る。
//
// 与える情報: 偽の gh と git が返す値。
// 成功条件: そろうときは merge-base の commit と今の branch 名で createLinkedBranch を1回叩き、
// どれか1つでも欠けると叩かない。どの場合も終了コード 0。
func TestLinkBranch_条件がそろうときだけ結びつける(t *testing.T) {
	const url = "https://github.com/octocat/hello-world/issues/42"

	t.Run("そろう", func(t *testing.T) {
		r := runLinkBranch(t, url, linkBase())
		if !r.mutated {
			t.Fatalf("条件がそろうのに結びつけていません: %s", r.out)
		}
		for _, want := range []string{
			"c=0123456789abcdef0123456789abcdef01234567",
			"b=continuo/octocat/hello-world/42",
			"i=I_issue",
			"r=R_repo",
		} {
			if !strings.Contains(r.mutation, want) {
				t.Errorf("createLinkedBranch に %q を渡していません: %s", want, r.mutation)
			}
		}
		if !strings.Contains(r.out, "結びつけました（continuo/octocat/hello-world/42）") {
			t.Errorf("結びつけたと出していません: %s", r.out)
		}
	})

	for _, c := range []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{"origin が別のリポジトリ", "FAKE_ORIGIN", "someone/fork", "origin が issue のリポジトリではありません"},
		{"remote に branch が在る", "FAKE_LSREMOTE_RC", "0", "が既に在ります"},
		{"remote を確かめられない", "FAKE_LSREMOTE_RC", "128", "確かめられませんでした"},
		{"issue にリンクが在る", "FAKE_IDS", "R_repo I_issue 1", "既に 1 本あります"},
		{"issue を読めない", "FAKE_IDS", "", "issue を読めませんでした"},
		{"既定の branch を読めない", "FAKE_DEFAULT", "", "既定の branch の名前を読めませんでした"},
		{"取ってこられない", "FAKE_FETCH_RC", "1", "取ってこられませんでした"},
		{"merge-base が無い", "FAKE_MERGEBASE", "", "起点の commit が決まりませんでした"},
		{"branch が分からない", "FAKE_BRANCH", "", "いまの branch が分かりません"},
	} {
		t.Run(c.name, func(t *testing.T) {
			env := linkBase()
			env[c.key] = c.value
			r := runLinkBranch(t, url, env)
			if r.mutated {
				t.Fatalf("%s のに結びつけています: %s", c.name, r.out)
			}
			if !strings.Contains(r.out, c.want) {
				t.Errorf("%s のときに %q と出していません: %s", c.name, c.want, r.out)
			}
		})
	}

	t.Run("GitHub が作らなかった", func(t *testing.T) {
		env := linkBase()
		env["FAKE_CREATED"] = ""
		r := runLinkBranch(t, url, env)
		if !strings.Contains(r.out, "結びつけられませんでした（GitHub が branch を作りませんでした）") {
			t.Errorf("作られなかったことを出していません: %s", r.out)
		}
	})
}

// 目的: 置き換え忘れの引数と、issue でない URL を断ることを固定する。
//
// 与える情報: 山括弧の語のままの引数・pull request の URL・引数なし。
// 成功条件: どれも終了コード 2 で、gh を1回も叩かない。
func TestLinkBranch_引数が違えば何もしない(t *testing.T) {
	script, _ := agentplugin.RawFile("skills/implementing-and-pushing/scripts/link-branch.sh")
	dir := t.TempDir()
	scriptFile := filepath.Join(dir, "link-branch.sh")
	if err := os.WriteFile(scriptFile, script, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(fakeLinkGh), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"<issue の URL>"},
		{"https://github.com/octocat/hello-world/pull/42"},
		{},
	} {
		cmd := exec.Command("sh", append([]string{scriptFile}, args...)...)
		cmd.Env = append(os.Environ(), "OUT="+dir, "PATH="+dir+":"+os.Getenv("PATH"))
		err := cmd.Run()
		ee, ok := err.(*exec.ExitError)
		if !ok || ee.ExitCode() != 2 {
			t.Errorf("引数 %q で終了コード 2 になりません: %v", args, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "gh.log")); err == nil {
		t.Error("引数が違うのに gh を叩いています")
	}
}
