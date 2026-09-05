// `continuo prompt` と `continuo init` の検査である（設計 5-3c / 5-3d / 5-3f / 5-3g）。
//
// **外部へ1回も接続しない。**ファイルを置いて CLI を呼ぶだけである。
package cli_test

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/cli"
	"github.com/maimuzo/continuo/internal/prompt"
	"github.com/maimuzo/continuo/internal/scaffold"
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

// 目的: WORKFLOW.md の本文が、送る文面の真ん中に入って出ることを確かめる（設計 5-3c / 5-3f）。
//
// **これが、本文の意味を変えた変更の要である。**本文は「組み込みを全部差し替えるもの」
// ではなく「組み込みの真ん中へ挟む固有の指示」である。
// **全文の差し替えとして扱っていたら、組み込みの見出しが1つも出てこない。**
//
// 与える情報: `## 固有の目印` だけを本文に書いた WORKFLOW.md。
// 成功条件: 標準出力の中で、本文が組み込みの前半と後半のあいだに並ぶこと。
// 内訳に WORKFLOW.md の絶対パスが出ること。
func TestPrompt_showは本文を真ん中に入れて出す(t *testing.T) {
	dir := writeWorkflowFor(t)
	setBody(t, dir, "## 固有の目印\n\nこのリポジトリでだけ効く決まりです。\n")

	code, stdout, stderr := runCLI([]string{"prompt", "--show", dir}, "")
	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）", code, stderr)
	}
	head := strings.Index(stdout, "## 4-2. 紐づく pull request を読む")
	mid := strings.Index(stdout, "## 固有の目印")
	tail := strings.Index(stdout, "# 5. 共通ルール")
	if head < 0 || mid < 0 || tail < 0 || !(head < mid && mid < tail) {
		t.Errorf("本文が真ん中に入っていません（head=%d mid=%d tail=%d）", head, mid, tail)
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
	for _, want := range []string{"書き足した本文です。", "# 5. 共通ルール"} {
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

// 目的: `continuo init` が置くのはちょうど2枚であることを確かめる（設計 5-3o）。
//
// **WORKFLOW.md が設定で、continuo-ci.yaml は CI へ移すための見本である。**
// **設定は1枚のままである**（設計 5-3g）。2枚目は front matter を持たず、
// **continuo は起動時に1バイトも読まない。**
//
// **3枚目が増えていないことも見る。**置くものが増えるたびに、
// 利用者は「何を .github/workflows/ へ移すのか」を毎回考えることになる。
//
// 与える情報: 空のディレクトリ。
// 成功条件: 終了コードが 0 で、2枚だけが在り、WORKFLOW.md に本文が入っており、
// 画面に配置の案内が出ていること。
func TestInit_置くのは設定とCIの雛形の2枚(t *testing.T) {
	dir := t.TempDir()

	code, stdout, stderr := runInitOffline(dir)
	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）", code, stderr)
	}
	if !strings.Contains(stdout, "WORKFLOW.md") {
		t.Errorf("標準出力に WORKFLOW.md の行がありません: %q", stdout)
	}
	if !strings.Contains(stdout, scaffold.CIFileName()) {
		t.Errorf("標準出力に %s の行がありません: %q", scaffold.CIFileName(), stdout)
	}
	// **配置の案内を出すこと。**置いただけでは何も起きないので、
	// **移すのは人間である**ことを画面で伝える（設計 5-3o）。
	if !strings.Contains(stdout, ".github/workflows/") {
		t.Errorf("標準出力に配置の案内がありません: %q", stdout)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ディレクトリを読めません: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	want := []string{scaffold.CIFileName(), "WORKFLOW.md"}
	sort.Strings(want)
	if !slices.Equal(names, want) {
		t.Errorf("置かれたファイルが %v ではありません: %v", want, names)
	}

	got := readFile(t, filepath.Join(dir, "WORKFLOW.md"))
	if !strings.Contains(got, "## テストの走らせ方") {
		t.Error("WORKFLOW.md に本文の雛形が入っていません（固有の指示を書く場所が消えています）")
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
// 標準出力に組み込みの前半・本文の見出し・組み込みの後半が、この順に並ぶこと。
func TestInit_置いたWORKFLOWmdはそのまま送れる(t *testing.T) {
	dir := t.TempDir()
	if code, _, stderr := runInitOffline(dir); code != 0 {
		t.Fatalf("continuo init の終了コードが %d です（stderr: %s）", code, stderr)
	}

	code, stdout, stderr := runCLI([]string{"prompt", "--show", dir}, "")
	if code != 0 {
		t.Fatalf("continuo prompt --show の終了コードが %d です（stderr: %s）", code, stderr)
	}
	head := strings.Index(stdout, "## 4-2. 紐づく pull request を読む")
	mid := strings.Index(stdout, "### 何をする作業か")
	tail := strings.Index(stdout, "# 5. 共通ルール")
	if head < 0 || mid < 0 || tail < 0 || !(head < mid && mid < tail) {
		t.Errorf("雛形の本文が真ん中に入っていません（head=%d mid=%d tail=%d）", head, mid, tail)
	}
}

// 目的: 既に WORKFLOW.md が在るときは、終了コード 1 で `--force` を勧めることを確かめる
// （設計 5-3g）。
//
// 与える情報: WORKFLOW.md を置いたディレクトリ。
// 成功条件: 終了コードが 1 で、`--force` の案内が出ること。
//
// **2枚とも在るときだけ 1 で終える**（設計 5-3o）。
// **WORKFLOW.md だけが在るときは、足りない continuo-ci.yaml を置いて 0 で終える。**
// そちらは下の `TestInit_片方だけ在るなら足りないほうを置いて0で終える` が見る。
func TestInit_2枚とも在るなら終了コード1(t *testing.T) {
	dir := writeWorkflowFor(t)
	// **2枚目も置いてから叩く。**1枚目だけだと、足りないほうを置いて 0 で終わる。
	if _, err := scaffold.WriteCIWorkflowWithValues(dir, false, scaffold.Values{}); err != nil {
		t.Fatalf("%s を置けません: %v", scaffold.CIFileName(), err)
	}

	code, _, stderr := runInitOffline(dir)
	if code != 1 {
		t.Errorf("終了コードが %d です（1 であるべきです）", code)
	}
	if !strings.Contains(stderr, "--force") {
		t.Errorf("--force の案内がありません: %q", stderr)
	}
}

// 目的: 片方だけ在るときは、足りないほうを置いて 0 で終えることを確かめる（設計 5-3o）。
//
// **これが移行の唯一の手順である。**版を上げた利用者が `continuo init` を叩くと、
// 足りない continuo-ci.yaml だけが増える。
// **`--force` を要求してはならない。**要求すると、利用者が手で書いた本文を潰す
// `--force` を打たせることになる（設計 5-3g）。
//
// 与える情報: WORKFLOW.md だけを置いたディレクトリ。**本文に人間が足した行を入れておく。**
// 成功条件: 終了コードが 0 で、continuo-ci.yaml が増え、
// WORKFLOW.md に足した行が1バイトも消えていないこと。
func TestInit_片方だけ在るなら足りないほうを置いて0で終える(t *testing.T) {
	dir := writeWorkflowFor(t)
	path := filepath.Join(dir, "WORKFLOW.md")
	const mark = "\n# 人間が手で足した行\n"
	before := readFile(t, path)
	if err := os.WriteFile(path, []byte(before+mark), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	code, stdout, stderr := runInitOffline(dir)
	if code != 0 {
		t.Fatalf("終了コードが %d です（0 であるべきです。stderr: %s）", code, stderr)
	}
	if !strings.Contains(stdout, scaffold.CIFileName()) {
		t.Errorf("標準出力に %s の行がありません: %q", scaffold.CIFileName(), stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, scaffold.CIFileName())); err != nil {
		t.Errorf("%s が置かれていません: %v", scaffold.CIFileName(), err)
	}
	if got := readFile(t, path); !strings.HasSuffix(got, mark) {
		t.Error("人間が足した行が消えています")
	}
}

// 目的: `WORKFLOW.md` が symlink のとき、辿らずに止めることを確かめる（設計 5-3g）。
//
// **辿ると、指定されたディレクトリの外にあるリンク先を雛形で潰す。**
// `--force` でも辿ってはならない。
//
// 与える情報: 別のディレクトリにある target.md を指す symlink を
// `WORKFLOW.md` として置いたディレクトリ。`--force` の有無の両方。
// 成功条件: どちらも終了コードが 1 で、リンク先の中身が1バイトも変わっておらず、
// symlink が実体のファイルに置き換わっていないこと。
func TestInit_書き出す先がsymlinkならリンク先を書き換えずに止まる(t *testing.T) {
	for _, force := range []bool{false, true} {
		dir, target, link := dirWithWorkflowSymlink(t)

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

// 目的: 書き出せなかったときの文言が、書き出せなかった当のファイルを名乗ることを
// 確かめる（設計 5-3g）。
//
// **文言の側にファイルの名前を書くと、別のファイルが落ちたときに無事なほうを名乗る。**
// 読む人は、名乗られたほうを消しに行く。
//
// 与える情報: `WORKFLOW.md` という名前の**ディレクトリ**を置いたディレクトリと、
// `--force`（--force のときだけ writeOne が名指しして止める経路へ入る）。
// 成功条件: 終了コードが 1 で、標準エラーに `WORKFLOW.md` と絶対パスの両方が出ていること。
func TestInit_書けないとき落ちた当のファイルを名乗る(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("テスト用のディレクトリを作れません: %v", err)
	}

	code, _, stderr := runInitOffline("--force", dir)

	if code != 1 {
		t.Errorf("終了コードが %d です（1 であるべきです）: %q", code, stderr)
	}
	if !strings.Contains(stderr, "WORKFLOW.md") {
		t.Errorf("書けなかったファイルの名前が出ていません: %q", stderr)
	}
	if !strings.Contains(stderr, path) {
		t.Errorf("どこで落ちたかのパスが出ていません: %q", stderr)
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
