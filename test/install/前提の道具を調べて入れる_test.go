// {"RUCM-CFG-SHA256": "37a49d28523d2641fa50a3223c18761ad88096fc840791557aa6fce65a88d28a", "SOURCE": "docs/spec/usecases/particular_case/前提の道具を調べて入れる.cfg.json"}
//
// **ユースケース記述「前提の道具を調べて入れる」の経路に対応づけたテストである。**
// 関数名の `P007` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**端末から答えを読む経路（尋ねて承諾する・断る）は、擬似端末が要るので書いていない
// （理由は記述の冒頭に在る）。
//
// **どの経路を通るかは、PATH に何が在るかで決まる。**テストを走らせる機械に入っている道具に左右されないよう、
// `/usr/bin` を PATH に入れず、一時ディレクトリへ要る道具だけを置く（`depsPath`）。
// `git`・`gh`・`ghq`・`herdr`・`claude` とパッケージ管理（`brew`）は、偽物を置くか、置かないかで決める。
//
// **1回の実行で、道具3つ（git → gh → ghq）が同じ経路を続けて通る。**
// 印は、3つ目のあとで段15・16 へ抜ける経路（`end`）の番号を付けている。
// 1つ目と2つ目が通る、次の道具へ戻る経路（`cycle`）は、同じ実行の中で通っている。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。

//go:build unix

package install_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// depsBaseTools は、install.sh が実行ファイルを取って置くまでに使う道具である。
//
// **`git`・`gh`・`ghq`・`herdr`・`claude` とパッケージ管理は、ここに入れない。**
// それらが在るか無いかが、確かめたい経路を決めるためである。
var depsBaseTools = []string{
	"uname", "curl", "tar", "gzip", "mktemp", "mkdir", "mv", "rm", "chmod", "cat",
	"sed", "awk", "grep", "tr", "cut", "head", "tail", "sort", "wc", "basename", "dirname",
	"shasum", "sha256sum", "cp", "ls", "id", "date", "sleep", "env", "expr", "printf", "true",
}

// depsPath は、install.sh が動くのに要る道具と、名指しした偽の道具だけが在る PATH を作る。
//
// **`/usr/bin` を PATH に入れない。**入れると、macOS では `git` が、apt の在る Linux では `apt-get` が見つかり、
// 通る経路がテストを走らせる機械で変わる。
//
// t: 呼び出し元のテスト。
// fakes: 偽の道具。名前から、シェルスクリプトの中身（`#!/bin/sh` の次の行から）への対応。
// 戻り値: 道具を置いたディレクトリ。そのまま PATH の値に使う。
func depsPath(t *testing.T, fakes map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range depsBaseTools {
		real, err := exec.LookPath(name)
		if err != nil {
			// **無い道具は置かない。**`shasum` と `sha256sum` のように、機械によって片方しか無いものがある。
			continue
		}
		if err := os.Symlink(real, filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s を置けません: %v", name, err)
		}
	}
	for _, must := range []string{"uname", "curl", "tar"} {
		if _, err := os.Lstat(filepath.Join(dir, must)); err != nil {
			t.Skipf("%s がありません: %v", must, err)
		}
	}
	for name, body := range fakes {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatalf("偽の %s を置けません: %v", name, err)
		}
	}
	return dir
}

// runDeps は、絞った PATH で、端末を与えずに install.sh を最後まで走らせる。
//
// t: 呼び出し元のテスト。
// path: PATH の値（`depsPath` の戻り値）。
// args: install.sh に足す引数。
// 戻り値: 標準出力と標準エラーを混ぜたもの。終了コードが 0 でなければ、ここでテストを落とす。
func runDeps(t *testing.T, path string, args ...string) string {
	t.Helper()
	fr := newFakeRelease(t, "v1.2.3", "#!/bin/sh\necho ok\n", true)
	dir := t.TempDir()
	full := append([]string{scriptPath(t),
		"--api-url", fr.Server.URL + "/api/latest",
		"--base-url", fr.Server.URL + "/dl"}, args...)
	// **シェルは絶対パスで起動する。**絞った PATH からは `sh` を引けない。
	shell, err := exec.LookPath(installShell)
	if err != nil {
		t.Fatalf("シェル %s を引けません: %v", installShell, err)
	}
	cmd := exec.Command(shell, full...)
	cmd.Stdin = nil
	detachTerminal(cmd)
	cmd.Env = []string{
		"HOME=" + t.TempDir(),
		"PATH=" + path,
		"CONTINUO_INSTALL_DIR=" + dir,
	}
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil {
		t.Fatalf("終了コードが 0 ではありません: %v\n%s", err, text)
	}
	if _, err := os.Stat(filepath.Join(dir, "continuo")); err != nil {
		t.Fatalf("実行ファイルが置かれていません: %v\n%s", err, text)
	}
	return text
}

// fakeBrew は、呼ばれた引数を1行ずつ記録する偽の `brew` の中身を返す。
//
// logPath: 引数を書き足すファイル。
// exitCode: 偽の `brew` が返す終了コード。
// 戻り値: `depsPath` の fakes に渡すスクリプトの中身。
func fakeBrew(logPath string, exitCode string) string {
	return `echo "$@" >> '` + logPath + `'` + "\nexit " + exitCode
}

// brewCalls は、偽の `brew` が呼ばれた引数の一覧を返す。1度も呼ばれていなければ空である。
func brewCalls(t *testing.T, logPath string) []string {
	t.Helper()
	b, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("偽の brew の記録を読めません: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// {"RUCM-PATH": "P017"}
//
// Test_前提の道具を調べて入れる_P017_道具が全部在れば何も言わず何も入れない は、代替フロー `道具が既にある` を確かめる。
//
// 目的: `git`・`gh`・`ghq` が在るとき、その道具について何も出さず、パッケージ管理を呼ばないこと。
// 与える情報: 偽の `git`・`gh`・`ghq`・`herdr`・`claude` と、偽の `brew` が在る PATH。
// 成功条件: 「がありません」も「まだ足りないもの」も出ず、偽の `brew` が1度も呼ばれないこと。
func Test_前提の道具を調べて入れる_P017_道具が全部在れば何も言わず何も入れない(t *testing.T) {
	log := filepath.Join(t.TempDir(), "brew.log")
	path := depsPath(t, map[string]string{
		"git": "exit 0", "gh": "exit 0", "ghq": "exit 0", "herdr": "exit 0", "claude": "exit 0",
		"brew": fakeBrew(log, "0"),
	})
	text := runDeps(t, path, "--yes")

	for _, forbidden := range []string{"がありません", "まだ足りないもの", "を入れました"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("道具が全部在るのに %q が出ています:\n%s", forbidden, text)
		}
	}
	if calls := brewCalls(t, log); len(calls) != 0 {
		t.Errorf("道具が全部在るのにパッケージ管理を呼んでいます: %v", calls)
	}
}

// {"RUCM-PATH": "P015"}
//
// Test_前提の道具を調べて入れる_P015_依存を入れない指定なら名前と使い道だけを出す は、代替フロー `依存を入れない指定` を確かめる。
//
// 目的: `--no-deps` のとき、足りない道具の名前と使い道を出すだけで、入れないこと。
// 与える情報: 道具が1つも無く、偽の `brew` だけが在る PATH。`--no-deps` と `--yes` を両方渡す。
// 成功条件: 道具ごとに「がありません」と使い道が出て、足りないままの道具に5つとも並び、偽の `brew` が1度も呼ばれないこと。
func Test_前提の道具を調べて入れる_P015_依存を入れない指定なら名前と使い道だけを出す(t *testing.T) {
	log := filepath.Join(t.TempDir(), "brew.log")
	path := depsPath(t, map[string]string{"brew": fakeBrew(log, "0")})
	// **`--yes` も渡す。**入れてよいと言われていても、`--no-deps` が勝つことを確かめる。
	text := runDeps(t, path, "--no-deps", "--yes")

	for _, want := range []string{
		"git がありません。", "continuo は worktree の作成に git を使います。",
		"gh がありません。", "continuo はカンバンの読み書きに gh を使います。",
		"ghq がありません。", "continuo は clone の置き場所の解決に ghq を使います。",
		"まだ足りないもの: git gh ghq herdr claude",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("%q が出ていません:\n%s", want, text)
		}
	}
	if strings.Contains(text, "を入れました") {
		t.Errorf("入れない指定なのに道具を入れています:\n%s", text)
	}
	if calls := brewCalls(t, log); len(calls) != 0 {
		t.Errorf("入れない指定なのにパッケージ管理を呼んでいます: %v", calls)
	}
}

// {"RUCM-PATH": "P013"}
//
// Test_前提の道具を調べて入れる_P013_入れるコマンドが無ければ自分で入れる案内を出す は、代替フロー `入れるコマンドが無い` を確かめる。
//
// 目的: パッケージ管理が見つからないとき、尋ねずに、自分で入れる案内を出すこと。
// 与える情報: 道具もパッケージ管理も無い PATH。`--yes` を渡す（入れるコマンドが無ければ、承諾していても入れられない）。
// 成功条件: `git` と `gh` は警告、`ghq` は入れ方の案内が出て、足りないままの道具に並び、「を入れました」が出ないこと。
func Test_前提の道具を調べて入れる_P013_入れるコマンドが無ければ自分で入れる案内を出す(t *testing.T) {
	path := depsPath(t, nil)
	text := runDeps(t, path, "--yes")

	for _, want := range []string{
		"警告: パッケージ管理が見つからないので、git は自分で入れてください。",
		"警告: パッケージ管理が見つからないので、gh は自分で入れてください。",
		"この環境のパッケージ管理には ghq がありません。",
		"go install github.com/x-motemen/ghq@latest",
		"herdr がありません。", "導入: https://github.com/herdrdev/herdr",
		"claude がありません。", "導入: https://claude.com/claude-code",
		"まだ足りないもの: git gh ghq herdr claude",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("%q が出ていません:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{"を入れました", "次を実行してよいですか"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("入れるコマンドが無いのに %q が出ています:\n%s", forbidden, text)
		}
	}
}

// {"RUCM-PATH": "P007"}
//
// Test_前提の道具を調べて入れる_P007_端末が無ければ尋ねず入れない は、代替フロー `端末が無い` を確かめる。
//
// **`curl … | sh` を無人で流されたとき、勝手にパッケージ管理を走らせてはならない。**
//
// 目的: 入れるコマンドが在っても、端末を開けなければ、尋ねず、入れないこと。
// 与える情報: 道具が無く、偽の `brew` が在る PATH。端末を与えず、`--yes` も渡さない。
// 成功条件: 偽の `brew` が1度も呼ばれず、尋ねる文言が標準出力にも標準エラーにも出ず、3つとも足りないままの道具に並ぶこと。
func Test_前提の道具を調べて入れる_P007_端末が無ければ尋ねず入れない(t *testing.T) {
	log := filepath.Join(t.TempDir(), "brew.log")
	path := depsPath(t, map[string]string{"brew": fakeBrew(log, "0")})
	text := runDeps(t, path)

	if calls := brewCalls(t, log); len(calls) != 0 {
		t.Errorf("端末が無いのにパッケージ管理を呼んでいます: %v", calls)
	}
	if !strings.Contains(text, "まだ足りないもの: git gh ghq herdr claude") {
		t.Errorf("足りないままの道具が並んでいません:\n%s", text)
	}
	for _, forbidden := range []string{"を入れました", "次を実行してよいですか", "パッケージ管理が見つからない"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("端末が無いのに %q が出ています:\n%s", forbidden, text)
		}
	}
}

// {"RUCM-PATH": "P009"}
//
// Test_前提の道具を調べて入れる_P009_確認を省く指定なら尋ねずに入れる は、代替フロー `確認を省く指定` を確かめる。
//
// 目的: `--yes` のとき、端末が無くても、尋ねずにパッケージ管理へ導入を要求すること。
// 与える情報: 道具が無く、成功する偽の `brew` が在る PATH。端末を与えず、`--yes` を渡す。
// 成功条件: 偽の `brew` が `install git`・`install gh`・`install ghq` の順に呼ばれ、3つとも「を入れました」が出て、
// 足りないままの道具には `herdr` と `claude` だけが並ぶこと。
func Test_前提の道具を調べて入れる_P009_確認を省く指定なら尋ねずに入れる(t *testing.T) {
	log := filepath.Join(t.TempDir(), "brew.log")
	path := depsPath(t, map[string]string{"brew": fakeBrew(log, "0")})
	text := runDeps(t, path, "--yes")

	want := []string{"install git", "install gh", "install ghq"}
	if got := brewCalls(t, log); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("パッケージ管理の呼ばれ方が違います: got=%v want=%v", got, want)
	}
	for _, s := range []string{"git を入れました。", "gh を入れました。", "ghq を入れました。", "まだ足りないもの: herdr claude"} {
		if !strings.Contains(text, s) {
			t.Errorf("%q が出ていません:\n%s", s, text)
		}
	}
	if strings.Contains(text, "次を実行してよいですか") {
		t.Errorf("確認を省く指定なのに尋ねています:\n%s", text)
	}
}

// {"RUCM-PATH": "P011"}
//
// Test_前提の道具を調べて入れる_P011_導入が失敗しても止まらず次の道具へ進む は、`確認を省く指定` から代替フロー `導入の失敗` へ入る経路を確かめる。
//
// 目的: 導入が失敗したら、警告を出して足りないままの道具に数え、止まらずに次の道具を調べること。
// 与える情報: 道具が無く、必ず失敗する偽の `brew` が在る PATH。端末を与えず、`--yes` を渡す。
// 成功条件: 終了コードが 0 で、偽の `brew` が3回呼ばれ、3つとも失敗の警告が出て、足りないままの道具に5つとも並ぶこと。
func Test_前提の道具を調べて入れる_P011_導入が失敗しても止まらず次の道具へ進む(t *testing.T) {
	log := filepath.Join(t.TempDir(), "brew.log")
	path := depsPath(t, map[string]string{"brew": fakeBrew(log, "1")})
	text := runDeps(t, path, "--yes")

	if got := brewCalls(t, log); len(got) != 3 {
		t.Errorf("パッケージ管理は道具ごとに1回ずつ、3回呼ばれるはずです: %v", got)
	}
	for _, s := range []string{
		"警告: git の導入に失敗しました。自分で入れてください。",
		"警告: gh の導入に失敗しました。自分で入れてください。",
		"警告: ghq の導入に失敗しました。自分で入れてください。",
		"まだ足りないもの: git gh ghq herdr claude",
	} {
		if !strings.Contains(text, s) {
			t.Errorf("%q が出ていません:\n%s", s, text)
		}
	}
	if strings.Contains(text, "を入れました") {
		t.Errorf("導入が失敗したのに「入れました」と出ています:\n%s", text)
	}
}
