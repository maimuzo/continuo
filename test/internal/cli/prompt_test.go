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

	code, stdout, stderr := runInitOffline(dir)
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

	code, stdout, stderr := runInitOffline(dir)
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

	code, _, stderr := runInitOffline(dir)
	if code != 1 {
		t.Errorf("終了コードが %d です（1 であるべきです）", code)
	}
	if !strings.Contains(stderr, "--force") {
		t.Errorf("--force の案内がありません: %q", stderr)
	}
}

// 目的: `PROJECT_SPECIFIC_PROMPT.md` が symlink のとき、辿らずに止めることを確かめる
// （設計 5-3c。WORKFLOW.md 側の TestWriteTemplate_書き出す先がsymlinkならリンク先を書き換えずに止まる
// と対になる検査である）。
//
// **いまこれを守っているのは、WriteProjectPrompt が WriteTemplateWithValues と
// 同じ writeOne を呼んでいるという実装上の都合だけである。**片方だけ別の書き方に
// 変えた瞬間、指定されたディレクトリの外にあるリンク先を雛形で潰すようになる。
// **偶然を検査で固定する。**
//
// 与える情報: 別のディレクトリにある target.md を指す symlink を
// `PROJECT_SPECIFIC_PROMPT.md` として置いたディレクトリ。`--force` の有無の両方。
// 成功条件: どちらも終了コードが 1 で、リンク先の中身が1バイトも変わっておらず、
// symlink が実体のファイルに置き換わっていないこと。
func TestInit_固有のプロンプトがsymlinkならリンク先を書き換えずに止まる(t *testing.T) {
	for _, force := range []bool{false, true} {
		dir, target, link := dirWithProjectPromptSymlink(t)

		var args []string
		if force {
			args = append(args, "--force")
		}
		code, stdout, stderr := runInitOffline(append(args, dir)...)
		if code != 1 {
			t.Errorf("--force=%v: 終了コードが %d です（1 であるべきです）\n  stdout: %s\n  stderr: %s",
				force, code, stdout, stderr)
		}

		if got := readFile(t, target); got != symlinkTargetBody {
			t.Errorf("--force=%v: symlink を辿って指定ディレクトリの外を書き換えています: got %q, want %q",
				force, got, symlinkTargetBody)
		}

		info, lstatErr := os.Lstat(link)
		if lstatErr != nil {
			t.Fatalf("--force=%v: symlink を確認できません: %v", force, lstatErr)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("--force=%v: symlink が実体のファイルに置き換わっています", force)
		}
	}
}

// 目的: symlink を断る文言が、断った当のファイルを名乗ることを確かめる（設計 5-3c）。
//
// **番兵 ErrSymlink は2枚のどちらからも返る。**その文言に片方の名前を書いていたため、
// `PROJECT_SPECIFIC_PROMPT.md` を断ったのに
// `WORKFLOW.md is a symlink: …/PROJECT_SPECIFIC_PROMPT.md` と出ていた。
// **断る動きは正しくても、読む人は WORKFLOW.md を消しに行く。**
//
// 与える情報: `PROJECT_SPECIFIC_PROMPT.md` だけを symlink にしたディレクトリ。
// 成功条件: 標準エラーに `PROJECT_SPECIFIC_PROMPT.md` が出ており、
// `WORKFLOW.md` が1文字も出ていないこと。
func TestInit_固有のプロンプトがsymlinkのとき別のファイルを名乗らない(t *testing.T) {
	dir, _, _ := dirWithProjectPromptSymlink(t)

	_, _, stderr := runInitOffline(dir)

	if !strings.Contains(stderr, prompt.ProjectFileName) {
		t.Errorf("断ったファイルの名前が出ていません: %q", stderr)
	}
	if strings.Contains(stderr, "WORKFLOW.md") {
		t.Errorf("断っていない WORKFLOW.md を名乗っています（読む人はそちらを消しに行きます）: %q", stderr)
	}
}

// 目的: 書き出せなかったときの文言が、書き出せなかった当のファイルを名乗ることを
// 確かめる（設計 5-3c）。**symlink 以外の失敗も同じでなければならない。**
//
// **symlink だけを直したときには、まだこう出ていた。**
//
//	エラー: WORKFLOW.md の雛形を書き出せません: WORKFLOW.md を作成できません:
//	  /…/PROJECT_SPECIFIC_PROMPT.md: is a directory
//
// **1行に WORKFLOW.md が2回出て、落ちた当のファイルの名前は1度も出ない。**
// 読む人は、無事に置けた WORKFLOW.md のほうを消しに行く。
//
// 与える情報: `PROJECT_SPECIFIC_PROMPT.md` という名前の**ディレクトリ**を置いた
// ディレクトリと、`--force`（--force のときだけ writeOne が名指しして止める経路へ入る）。
// 成功条件: 標準エラーに `PROJECT_SPECIFIC_PROMPT.md` が出ており、
// `WORKFLOW.md` が1文字も出ていないこと。
func TestInit_固有のプロンプトを書けないとき別のファイルを名乗らない(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, prompt.ProjectFileName), 0o755); err != nil {
		t.Fatalf("テスト用のディレクトリを作れません: %v", err)
	}

	code, _, stderr := runInitOffline("--force", dir)

	if code != 1 {
		t.Errorf("終了コードが %d です（1 であるべきです）: %q", code, stderr)
	}
	if !strings.Contains(stderr, prompt.ProjectFileName) {
		t.Errorf("書けなかったファイルの名前が出ていません: %q", stderr)
	}
	if strings.Contains(stderr, "WORKFLOW.md") {
		t.Errorf("書けた WORKFLOW.md を名乗っています（読む人はそちらを消しに行きます）: %q", stderr)
	}
}

// symlinkTargetBody は、symlink の先に置くファイルの中身である。
// **1バイトも変わっていないことを確かめるための目印である。**
const symlinkTargetBody = "指定ディレクトリの外にある大事なファイル\n"

// dirWithProjectPromptSymlink は、PROJECT_SPECIFIC_PROMPT.md が symlink になっている
// ディレクトリを作る。WORKFLOW.md は置かない（`continuo init` に作らせる）。
//
// t: 呼び出し元のテスト。
// 戻り値: `continuo init` に渡すディレクトリ、リンク先のパス、symlink 自身のパス。
func dirWithProjectPromptSymlink(t *testing.T) (dir, target, link string) {
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
	link = filepath.Join(dir, prompt.ProjectFileName)
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("テスト用の symlink を張れません: %v", err)
	}
	return dir, target, link
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
