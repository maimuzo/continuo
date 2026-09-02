// `continuo prompt` と、2枚を置くようになった `continuo init` の検査である
// （設計 5-3c / 5-3d / 5-3f / 5-3g）。
//
// **外部へ1回も接続しない。**ファイルを置いて CLI を呼ぶだけである。
package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/prompt"
)

// 目的: `continuo prompt --show` が、送る文面だけを標準出力へ出すことを確かめる（設計 5-3f）。
//
// **内訳を標準出力へ混ぜてはならない。**
// `continuo prompt --show > out.md` が、送る文面と1バイトも違わないファイルになる必要がある。
//
// 与える情報: 固有のプロンプトを置いていない WORKFLOW.md。
// 成功条件: 終了コードが 0 で、標準出力が組み込みの全文と1バイトも違わないこと。
func TestPrompt_showは送る文面だけを標準出力へ出す(t *testing.T) {
	dir := writeWorkflowFor(t)

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

// 目的: 固有のプロンプトが、送る文面の真ん中に入って出ることを確かめる（設計 5-3c / 5-3f）。
//
// 与える情報: `## 固有の目印` だけを書いた PROJECT_SPECIFIC_PROMPT.md。
// 成功条件: 標準出力の中で、固有が組み込みの前半と後半のあいだに並ぶこと。
func TestPrompt_showは固有のプロンプトを真ん中に入れて出す(t *testing.T) {
	dir := writeWorkflowFor(t)
	writeProjectPrompt(t, dir, "## 固有の目印\n")

	code, stdout, stderr := runCLI([]string{"prompt", "--show", dir}, "")
	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）", code, stderr)
	}
	head := strings.Index(stdout, "## この issue に紐づく PR も読むこと")
	mid := strings.Index(stdout, "## 固有の目印")
	tail := strings.Index(stdout, "## 終わったらやること")
	if head < 0 || mid < 0 || tail < 0 || !(head < mid && mid < tail) {
		t.Errorf("固有が真ん中に入っていません（head=%d mid=%d tail=%d）", head, mid, tail)
	}
	if !strings.Contains(stderr, prompt.ProjectFileName) {
		t.Errorf("内訳に %s が出ていません: %q", prompt.ProjectFileName, stderr)
	}
}

// 目的: `--builtin` が WORKFLOW.md を1バイトも読まずに組み込みだけを出すことを確かめる
// （設計 5-3d の移行の段2）。
//
// **本文が残っている利用者は、`--show` だけでは自分の本文しか読めない。**
// **比べる相手を読む道が要る。**設定が壊れていても読めなければならない。
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

// 目的: 本文が残っているとき、`--show` がその本文で組み立てたものを出し、警告を添えることを
// 確かめる（設計 5-3d / 5-3f）。
//
// 与える情報: 本文を書き足した WORKFLOW.md。
// 成功条件: 標準出力に本文が入り、組み込みの見出しが入らないこと。標準エラーに警告と
// `--builtin` の案内が出ること。
func TestPrompt_本文が残っていれば警告して本文を出す(t *testing.T) {
	dir := writeWorkflowFor(t)
	appendBody(t, dir, "\n残っている本文です。\n")

	code, stdout, stderr := runCLI([]string{"prompt", "--show", dir}, "")
	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）", code, stderr)
	}
	if !strings.Contains(stdout, "残っている本文です。") {
		t.Error("標準出力に残っている本文が入っていません")
	}
	if strings.Contains(stdout, "## 終わったらやること") {
		t.Error("本文が残っているのに組み込みのプロンプトが出ています")
	}
	if !strings.Contains(stderr, "--builtin") {
		t.Errorf("警告に --builtin の案内がありません: %q", stderr)
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
// **部分的な文面を出すほうが危ない。**固有の断片が抜けた文面は、送る文面ではない。
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

// 目的: `continuo init` が2枚を置くことを確かめる（設計 5-3g）。
//
// 与える情報: 空のディレクトリ。
// 成功条件: 終了コードが 0 で、WORKFLOW.md と PROJECT_SPECIFIC_PROMPT.md の両方が在ること。
func TestInit_2枚を置く(t *testing.T) {
	dir := t.TempDir()

	code, stdout, stderr := runCLI([]string{"init", "--owner", "octocat", "--project", "3", dir}, "")
	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）", code, stderr)
	}
	for _, name := range []string{"WORKFLOW.md", prompt.ProjectFileName} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s が置かれていません: %v", name, err)
		}
		if !strings.Contains(stdout, name) {
			t.Errorf("標準出力に %s の行がありません: %q", name, stdout)
		}
	}
}

// 目的: WORKFLOW.md だけが在るときに、足りない1枚だけを置いて 0 で終えることを確かめる
// （設計 5-3g。5-3d の移行の段1）。
//
// **ここが移行の唯一の手順である。**`--force` を要求すると、
// 手で直した WORKFLOW.md を潰すフラグを打たせることになる。
//
// 与える情報: WORKFLOW.md だけを置いたディレクトリ。
// 成功条件: 終了コードが 0 で、PROJECT_SPECIFIC_PROMPT.md が置かれ、
// WORKFLOW.md の中身が1バイトも変わっていないこと。
func TestInit_片方だけ在るなら足りないほうを置く(t *testing.T) {
	dir := writeWorkflowFor(t)
	before := readFile(t, filepath.Join(dir, "WORKFLOW.md"))

	code, stdout, stderr := runCLI([]string{"init", "--owner", "octocat", "--project", "3", dir}, "")
	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）。"+
			"足りない %s を --force 無しで置けなければ、移行の手順が成り立ちません",
			code, stderr, prompt.ProjectFileName)
	}
	if _, err := os.Stat(filepath.Join(dir, prompt.ProjectFileName)); err != nil {
		t.Errorf("%s が置かれていません: %v", prompt.ProjectFileName, err)
	}
	if got := readFile(t, filepath.Join(dir, "WORKFLOW.md")); got != before {
		t.Error("既にあった WORKFLOW.md が書き換わっています")
	}
	if !strings.Contains(stdout, "WORKFLOW.md") {
		t.Errorf("触っていない WORKFLOW.md のことが出ていません: %q", stdout)
	}
}

// 目的: 2枚とも在るときは、いままでどおり終了コード 1 で `--force` を勧めることを確かめる
// （設計 5-3g）。
//
// 与える情報: 2枚とも置いたディレクトリ。
// 成功条件: 終了コードが 1 であること。
func TestInit_2枚とも在るなら終了コード1(t *testing.T) {
	dir := writeWorkflowFor(t)
	writeProjectPrompt(t, dir, "## 固有\n")

	code, _, stderr := runCLI([]string{"init", "--owner", "octocat", "--project", "3", dir}, "")
	if code != 1 {
		t.Errorf("終了コードが %d です（1 であるべきです）", code)
	}
	if !strings.Contains(stderr, "--force") {
		t.Errorf("--force の案内がありません: %q", stderr)
	}
}

// writeProjectPrompt は PROJECT_SPECIFIC_PROMPT.md を1つ置く。
//
// t: 呼び出し元のテスト。
// dir: 置く先のディレクトリ。
// body: 書く中身。
func writeProjectPrompt(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, prompt.ProjectFileName), []byte(body), 0o600); err != nil {
		t.Fatalf("%s を書けません: %v", prompt.ProjectFileName, err)
	}
}

// appendBody は WORKFLOW.md の末尾へ本文を書き足す（互換の経路を作る）。
//
// t: 呼び出し元のテスト。
// dir: WORKFLOW.md があるディレクトリ。
// body: 書き足す本文。
func appendBody(t *testing.T, dir, body string) {
	t.Helper()
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte(readFile(t, path)+body), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
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
