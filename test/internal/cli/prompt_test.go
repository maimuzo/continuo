// `continuo prompt` と `continuo init` の検査である（設計 5-3c / 5-3d / 5-3f / 5-3g）。
//
// **外部へ1回も接続しない。**ファイルを置いて CLI を呼ぶだけである。
package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/cli"
	"github.com/maimuzo/continuo/internal/prompt"
)

// runInitOffline は `continuo init` を、gh を1回も起動しない形で呼ぶ。
//
// **`--owner` と `--project` を渡しても gh は止まらない。**`continuo init` は
// `trust.repositories` に並べる owner/repo をカンバンから拾うために
// `gh project item-list` を叩く（internal/scaffold の detectRepositories）。
// **差し替えないと `go test` のたびに github.com へ出ていく。**回線が切れれば落ち、
// このリポジトリを clone した人の API の枠も減る。**同じコードで通ったり落ちたりする。**
//
// **差し替え先は fixedDetection である**（cli_test.go にある。`continuo setup` の検査が
// 同じ理由で既に使っている）。owner に octocat、カンバンの番号に 3 が埋まった検出結果を返す。
//
// args: `continuo init` に続けて渡す引数。
// 戻り値: 終了コードと stdout / stderr。
func runInitOffline(args ...string) (int, string, string) {
	deps := cli.Deps{ScaffoldDetect: fixedDetection}
	return runCLIWith(deps, append([]string{"init"}, args...), "")
}

// 目的: `continuo prompt --show` が、送る文面だけを標準出力へ出すことを確かめる（設計 5-3f）。
//
// **内訳を標準出力へ混ぜてはならない。**
// `continuo prompt --show > out.md` が、送る文面と1バイトも違わないファイルになる必要がある。
//
// 与える情報: 本文を空にした WORKFLOW.md。
// 成功条件: 終了コードが 0 で、標準出力が組み込みの全文と1バイトも違わないこと。
func TestPrompt_showは送る文面だけを標準出力へ出す(t *testing.T) {
	dir := writeWorkflowFor(t)
	setBody(t, dir, "")

	code, stdout, stderr := runCLI([]string{"prompt", "--show", dir}, "")
	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）", code, stderr)
	}
	if stdout != prompt.Builtin() {
		t.Errorf("標準出力が組み込みの全文と違います\n  出た長さ: %d\n  組み込み: %d",
			len(stdout), len(prompt.Builtin()))
	}
	if !strings.Contains(stderr, "内訳") && !strings.Contains(stderr, "Breakdown") {
		t.Errorf("内訳が標準エラーに出ていません: %q", stderr)
	}
}

// 目的: `--show` が、システムプロンプトと最初のプロンプトを区切りの行で分けて出し、
// WORKFLOW.md の本文をどちらにも「このプロジェクトの決まり」の下に入れることを確かめる
// （設計 5-3c / 5-3f。issue #288）。
//
// **本文は「組み込みを全部差し替えるもの」ではなく「組み込みへ足す固有の指示」である。**
// **全文の差し替えとして扱っていたら、組み込みの見出しが1つも出てこない。**
//
// 与える情報: `## 固有の目印` だけを本文に書いた WORKFLOW.md。
// 成功条件: 区切りの行がちょうど1つあり、その前後のどちらでも、組み込みの「このプロジェクトの決まり」の見出しの
// あとに本文が並ぶこと。内訳に WORKFLOW.md の絶対パスが出ること。
func TestPrompt_showは2つの文面を区切って本文を両方に入れる(t *testing.T) {
	dir := writeWorkflowFor(t)
	setBody(t, dir, "## 固有の目印\n\nこのリポジトリでだけ効く決まりです。\n")

	code, stdout, stderr := runCLI([]string{"prompt", "--show", dir}, "")
	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）", code, stderr)
	}
	system, first, ok := strings.Cut(stdout, "\n"+prompt.Separator+"\n")
	if !ok || strings.Contains(first, prompt.Separator) {
		t.Fatalf("区切りの行がちょうど1つありません:\n%s", stdout)
	}
	for _, c := range []struct{ label, text, heading string }{
		{"システムプロンプト", system, "\n# このプロジェクトの決まり\n"},
		{"最初のプロンプト", first, "\n## このプロジェクトの決まり\n"},
	} {
		head := strings.Index(c.text, c.heading)
		mid := strings.Index(c.text, "## 固有の目印")
		if head < 0 || mid < 0 || head > mid {
			t.Errorf("%s: 本文が「このプロジェクトの決まり」の下に入っていません（head=%d mid=%d）", c.label, head, mid)
		}
	}
	if want := filepath.Join(dir, "WORKFLOW.md"); !strings.Contains(stderr, want) {
		t.Errorf("内訳に %s が出ていません: %q", want, stderr)
	}
}

// 目的: 本文があっても組み込みが送られることを確かめる（設計 5-3c）。
//
// **本文を「全文の差し替え」として扱っていた頃は、本文があると組み込みを1文字も送らず、
// 警告を出していた。**その扱いをやめたので、**警告も出ない。**
//
// 与える情報: 本文を書いた WORKFLOW.md。
// 成功条件: 標準出力に本文と組み込みの締めくくりの両方が入り、
// 標準エラーに `--builtin` を勧める警告が出ないこと。
func TestPrompt_本文があっても組み込みを送る(t *testing.T) {
	dir := writeWorkflowFor(t)
	setBody(t, dir, "書き足した本文です。\n")

	code, stdout, stderr := runCLI([]string{"prompt", "--show", dir}, "")
	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）", code, stderr)
	}
	// **組み込みの側は、システムプロンプトのフローチャートで見る**（issue #288）。
	for _, want := range []string{"書き足した本文です。", "\n# フローチャート\n"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("標準出力に %q がありません", want)
		}
	}
	if strings.Contains(stderr, "--builtin") {
		t.Errorf("本文があることを警告しています（いまはそれが正しい書き方です）: %q", stderr)
	}
}

// 目的: 本文が空なら、内訳にそう出ることを確かめる（設計 5-3f）。
//
// **「何も書かなくてよい」と「書いたつもりが届いていない」を、利用者が見分けられるようにする。**
//
// 与える情報: 本文を空にした WORKFLOW.md。
// 成功条件: 標準エラーに WORKFLOW.md の絶対パスが出ること。
func TestPrompt_本文が空なら内訳にそう出る(t *testing.T) {
	dir := writeWorkflowFor(t)
	setBody(t, dir, "")

	_, _, stderr := runCLI([]string{"prompt", "--show", dir}, "")
	if want := filepath.Join(dir, "WORKFLOW.md"); !strings.Contains(stderr, want) {
		t.Errorf("本文が無いことを、どのファイルの話かを添えて出していません: %q", stderr)
	}
}

// 目的: `--builtin` が WORKFLOW.md を1バイトも読まずに組み込みだけを出すことを確かめる
// （設計 5-3f）。
//
// **自分が書いた本文と、仕組みの側を見比べるための道である。**
// 設定が壊れていても読めなければならない。
//
// 与える情報: WORKFLOW.md が1つも無いディレクトリ。
// 成功条件: 終了コードが 0 で、標準出力が組み込みの全文と1バイトも違わないこと。
func TestPrompt_builtinはWORKFLOWmdを読まない(t *testing.T) {
	dir := t.TempDir()

	code, stdout, stderr := runCLI([]string{"prompt", "--show", "--builtin", dir}, "")
	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）。"+
			"WORKFLOW.md が無くても組み込みは読めなければなりません", code, stderr)
	}
	if stdout != prompt.Builtin() {
		t.Error("標準出力が組み込みの全文と違います")
	}
}

// 目的: `--show` を付けずに呼んだら、終了コード 2 で案内することを確かめる（設計 5-3f）。
//
// 与える情報: `continuo prompt` だけ。
// 成功条件: 終了コードが 2 で、標準出力が空であること。
func TestPrompt_showを付けなければ終了コード2(t *testing.T) {
	code, stdout, stderr := runCLI([]string{"prompt"}, "")
	if code != 2 {
		t.Errorf("終了コードが %d です（2 であるべきです）", code)
	}
	if stdout != "" {
		t.Errorf("標準出力に何か出ています: %q", stdout)
	}
	if !strings.Contains(stderr, "--show") {
		t.Errorf("案内に --show がありません: %q", stderr)
	}
}

// 目的: WORKFLOW.md を読めなければ、何も出さずに終了コード 1 になることを確かめる（設計 5-3f）。
//
// **部分的な文面を出すほうが危ない。**本文が抜けた文面は、送る文面ではない。
//
// 与える情報: WORKFLOW.md が無いディレクトリ。
// 成功条件: 終了コードが 1 で、標準出力が空であること。
func TestPrompt_WORKFLOWmdを読めなければ何も出さない(t *testing.T) {
	code, stdout, _ := runCLI([]string{"prompt", "--show", t.TempDir()}, "")
	if code != 1 {
		t.Errorf("終了コードが %d です（1 であるべきです）", code)
	}
	if stdout != "" {
		t.Errorf("標準出力に何か出ています: %q", stdout)
	}
}

// 目的: `continuo init` が置いた WORKFLOW.md が、そのまま送れる形であることを確かめる
// （設計 5-3d / 5-3g）。
//
// **`continuo init` の直後に `continuo prompt --show` が落ちると、
// 利用者は自分が何かを壊したのだと思う。**
//
// 与える情報: `continuo init` を叩いたディレクトリ。
// 成功条件: `continuo prompt --show` の終了コードが 0 で、
// 本文の見出しがシステムプロンプトの「このプロジェクトの決まり」の下と、最初のプロンプトの両方に出ること。
func TestInit_置いたWORKFLOWmdはそのまま送れる(t *testing.T) {
	dir := t.TempDir()
	if code, _, stderr := runInitOffline(dir); code != 0 {
		t.Fatalf("continuo init の終了コードが %d です（stderr: %s）", code, stderr)
	}

	code, stdout, stderr := runCLI([]string{"prompt", "--show", dir}, "")
	if code != 0 {
		t.Fatalf("continuo prompt --show の終了コードが %d です（stderr: %s）", code, stderr)
	}
	// **本文は、システムプロンプトと最初のプロンプトの両方の「このプロジェクトの決まり」の下に入る**（issue #288）。
	head := strings.Index(stdout, "\n# このプロジェクトの決まり\n")
	sep := strings.Index(stdout, "\n"+prompt.Separator+"\n")
	if head < 0 || sep < 0 || strings.Count(stdout, "### 何をする作業か") != 2 ||
		!(head < strings.Index(stdout, "### 何をする作業か") && strings.Index(stdout, "### 何をする作業か") < sep) {
		t.Errorf("雛形の本文が、2つの文面の「このプロジェクトの決まり」の下に入っていません（head=%d sep=%d）", head, sep)
	}
}

// symlinkTargetBody は、symlink の先に置くファイルの中身である。
// **1バイトも変わっていないことを確かめるための目印である。**
const symlinkTargetBody = "指定ディレクトリの外にある大事なファイル\n"

// dirWithWorkflowSymlink は、WORKFLOW.md が symlink になっているディレクトリを作る。
//
// t: 呼び出し元のテスト。
// 戻り値: `continuo init` に渡すディレクトリ、リンク先のパス、symlink 自身のパス。
func dirWithWorkflowSymlink(t *testing.T) (dir, target, link string) {
	t.Helper()
	base := t.TempDir()
	dir = filepath.Join(base, "dir")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{dir, outside} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatalf("テスト用のディレクトリを作れません: %v", err)
		}
	}

	target = filepath.Join(outside, "target.md")
	if err := os.WriteFile(target, []byte(symlinkTargetBody), 0o644); err != nil {
		t.Fatalf("テスト用のリンク先を置けません: %v", err)
	}
	link = filepath.Join(dir, "WORKFLOW.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("テスト用の symlink を張れません: %v", err)
	}
	return dir, target, link
}

// setBody は WORKFLOW.md の本文（front matter の閉じの `---` より下）を置き換える。
//
// **front matter は1文字も触らない。**設定を変えずに、送る文面だけを変えるためである。
//
// t: 呼び出し元のテスト。
// dir: WORKFLOW.md があるディレクトリ。
// body: 置き換える本文。空文字なら本文を消す。
func setBody(t *testing.T, dir, body string) {
	t.Helper()
	path := filepath.Join(dir, "WORKFLOW.md")
	lines := strings.Split(readFile(t, path), "\n")
	seen := 0
	for i, line := range lines {
		if strings.TrimRight(line, " \t") != "---" {
			continue
		}
		seen++
		if seen != 2 {
			continue
		}
		out := strings.Join(lines[:i+1], "\n") + "\n" + body
		if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
			t.Fatalf("WORKFLOW.md を書けません: %v", err)
		}
		return
	}
	t.Fatalf("WORKFLOW.md に front matter の閉じの --- がありません（見つかった --- は %d 行）", seen)
}

// readFile はファイルを読む。
//
// t: 呼び出し元のテスト。
// path: 読むファイルの絶対パス。
// 戻り値: 中身。
func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s を読めません: %v", path, err)
	}
	return string(raw)
}
