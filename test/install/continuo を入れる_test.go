// {"RUCM-CFG-SHA256": "38304317df64e136f9c85edd93f421e76e66cf432856bf4bb01fcfe833aa535a", "SOURCE": "docs/spec/usecases/particular_case/continuo を入れる.cfg.json"}
//
// **ユースケース記述「continuo を入れる」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。

//go:build unix

package install_test

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// {"RUCM-PATH": "P007"}
//
// Test_continuoを入れる_P007_版に使えない文字があれば取得の前に弾く は、URL への混入を防ぐことを確かめる。
//
// **`--version` の値は URL のパスに入る。**`../` を含む値を通すと、curl が送信前に
// パスを正規化し、**別のリポジトリの release に到達できる**（安全性のレビューで、
// 本物の github.com が 200 を返すことが確かめられた）。
//
// 目的: 使えない文字を含む版を、配布サーバへ繋ぐ前に弾くこと。
// 与える情報: `../` や空白を含む版。
// 成功条件: どれも終了コードが 0 でなく、偽サーバへ1度も繋がないこと。
func Test_continuoを入れる_P007_版に使えない文字があれば取得の前に弾く(t *testing.T) {
	fr := newFakeRelease(t, "v1.2.3", "#!/bin/sh\necho ok\n", true)
	// **1度でも繋がれたら記録する。**
	var reached atomic.Bool
	fr.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
		http.NotFound(w, r)
	})
	dir := t.TempDir()

	for _, bad := range []string{"../../other/v1", "v1.0.0/../../x", "v1 0", "$(id)"} {
		code, out := runInstaller(t, fr, dir, "--no-deps", "--version", bad)
		if code == 0 {
			t.Errorf("%q を受け付けています:\n%s", bad, out)
		}
		if !strings.Contains(out, "使える文字") {
			t.Errorf("%q で、使える文字を示していません:\n%s", bad, out)
		}
	}
	if reached.Load() {
		t.Error("弾いたはずなのに配布サーバへ繋いでいます")
	}
	if _, err := os.Stat(filepath.Join(dir, "continuo")); err == nil {
		t.Error("使えない版なのに実行ファイルを置いています")
	}
}

// {"RUCM-PATH": "P004"}
//
// Test_continuoを入れる_P004_helpはパイプ経由でも出る は、`curl … | sh -s -- --help` を確かめる。
//
// **`$0` は "sh" になるので、自分自身を読み直す作りだと壊れる。**
// 実際、`sed -n '2,30p' "$0"` で書いたときは `sed: sh: No such file or directory` になった。
//
// 目的: 標準入力からスクリプトを流し込んでも、案内が出ること。
// 与える情報: install.sh の中身を標準入力から流す。
// 成功条件: 終了コードが 0 で、オプションの一覧が出ること。
func Test_continuoを入れる_P004_helpはパイプ経由でも出る(t *testing.T) {
	body, err := os.ReadFile(scriptPath(t))
	if err != nil {
		t.Fatalf("install.sh を読めません: %v", err)
	}
	cmd := exec.Command(installShell, "-s", "--", "--help")
	cmd.Stdin = strings.NewReader(string(body))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("終了コードが 0 ではありません: %v\n%s", err, out)
	}
	text := string(out)
	for _, want := range []string{"--no-deps", "--dir", "--version", "herdr と claude は入れません"} {
		if !strings.Contains(text, want) {
			t.Errorf("案内に %q がありません:\n%s", want, text)
		}
	}
	if strings.Contains(text, "No such file") {
		t.Errorf("自分自身を読み直そうとして壊れています:\n%s", text)
	}
}

// {"RUCM-PATH": "P005"}
//
// Test_continuoを入れる_P005_知らないオプションは弾く は、引数の検査を確かめる。
//
// 目的: 知らないオプションで、何もせずに落ちること。
// 与える情報: `--bogus`。
// 成功条件: 終了コードが 0 でなく、置き先に何も無いこと。
func Test_continuoを入れる_P005_知らないオプションは弾く(t *testing.T) {
	dir := t.TempDir()
	code, out := runInstaller(t, nil, dir, "--bogus")
	if code == 0 {
		t.Fatalf("知らないオプションを受け付けています:\n%s", out)
	}
	if !strings.Contains(out, "知らないオプションです") {
		t.Errorf("理由を示していません:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "continuo")); err == nil {
		t.Error("引数が誤っているのに実行ファイルを置いています")
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_破壊的変更のある版へ上げると名指しで警告する は、上げる前の警告を確かめる。
//
// **設定ファイルは、未知のキーがあると起動を止める。**キーが増減した版へ上げると、
// **古い設定のまま起動しようとした時点で落ちる。**インストーラーが黙って上書きすると、
// **上げたあとに初めて気づくことになる。**
//
// 目的: 飛び越えて上げたとき、あいだの版の破壊的変更まで名指しで並べること。
// 与える情報: v0.2.0 を名乗る実行ファイルと、v0.3.0 と v0.10.0 に印がある release の一覧。
// 成功条件: 終了コードが 0 で、実行ファイルが入れ替わり、
//
//	v0.3.0 と v0.10.0 の説明が出て、範囲の外（v0.1.0）の説明が出ないこと。
func Test_continuoを入れる_P001_破壊的変更のある版へ上げると名指しで警告する(t *testing.T) {
	fr := newFakeRelease(t, "v0.10.0", "#!/bin/sh\necho v0.10.0\n", true)
	fr.SetReleases(releasesJSON(t, []releaseNote{
		{Tag: "v0.10.0", Breaking: []string{"WORKFLOW.md に tracker.dispatch_state が要ります"}},
		{Tag: "v0.9.0"},
		{Tag: "v0.3.0", Breaking: []string{"claude.model の既定が変わりました"}},
		{Tag: "v0.2.0"},
		{Tag: "v0.1.0", Breaking: []string{"これは範囲の外なので出てはいけません"}},
	}))
	dir := t.TempDir()
	// **いま入っているものを置く。**install.sh は置き換える前にここへ版を訊く。
	placeInstalled(t, dir, "v0.2.0")

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}

	// **止めない。**入れ替えたうえで警告する。
	got, err := exec.Command(filepath.Join(dir, "continuo")).Output()
	if err != nil {
		t.Fatalf("置いた実行ファイルを動かせません: %v", err)
	}
	if !strings.Contains(string(got), "v0.10.0") {
		t.Errorf("入れ替わっていません: %q", string(got))
	}

	if !strings.Contains(out, "破壊的変更があります") {
		t.Fatalf("破壊的変更を伝えていません:\n%s", out)
	}
	for _, want := range []string{
		"WORKFLOW.md に tracker.dispatch_state が要ります",
		"claude.model の既定が変わりました",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q を名指ししていません:\n%s", want, out)
		}
	}
	// **v0.10.0 と v0.2.0 の大小を、文字列として比べてはならない。**
	// 取り違えると、範囲の外の v0.1.0 まで並ぶ。
	if strings.Contains(out, "これは範囲の外なので出てはいけません") {
		t.Errorf("いま入っている版より前の破壊的変更まで並べています:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_破壊的変更が無ければ何も言わない は、余計なことを言わないことを確かめる。
//
// **毎回の更新で警告が出ると、本当に出たときに読まれなくなる。**
//
// 目的: 上げる範囲に印が1つも無ければ、破壊的変更について何も言わないこと。
// 与える情報: 印を1つも持たない release の一覧。
// 成功条件: 終了コードが 0 で、出力に「破壊的変更」が出ないこと。
func Test_continuoを入れる_P001_破壊的変更が無ければ何も言わない(t *testing.T) {
	fr := newFakeRelease(t, "v0.10.0", "#!/bin/sh\necho v0.10.0\n", true)
	fr.SetReleases(releasesJSON(t, []releaseNote{
		{Tag: "v0.10.0"},
		{Tag: "v0.3.0"},
	}))
	dir := t.TempDir()
	placeInstalled(t, dir, "v0.2.0")

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	// **警告の見出しで見る。**一時ディレクトリのパスにテストの名前が入るので、
	// 「破壊的変更」だけで探すと、警告が出ていなくても当たってしまう。
	if strings.Contains(out, "破壊的変更があります") {
		t.Errorf("破壊的変更が無いのに何か言っています:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_新規の導入では破壊的変更を言わない は、警告する相手がいない場合を確かめる。
//
// **置き先に何も無ければ、上げるのではなく初めて入れるのである。**
// 直す設定はまだ無いので、言うことは何も無い。
//
// 目的: 置き先に実行ファイルが無いとき、印があっても何も言わないこと。
// 与える情報: 印を持つ release の一覧と、空の置き先。
// 成功条件: 終了コードが 0 で、出力に「破壊的変更」が出ないこと。
func Test_continuoを入れる_P001_新規の導入では破壊的変更を言わない(t *testing.T) {
	fr := newFakeRelease(t, "v0.10.0", "#!/bin/sh\necho v0.10.0\n", true)
	fr.SetReleases(releasesJSON(t, []releaseNote{
		{Tag: "v0.10.0", Breaking: []string{"新規の導入では出てはいけません"}},
	}))
	dir := t.TempDir()

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	if strings.Contains(out, "破壊的変更があります") ||
		strings.Contains(out, "新規の導入では出てはいけません") {
		t.Errorf("新規の導入なのに破壊的変更を言っています:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_版を名乗らないものからは警告しない は、ソースから作ったものの扱いを確かめる。
//
// **`go build` しただけの実行ファイルは `dev` と名乗る**（internal/cli の version）。
// **どの release より新しいのか古いのかを決められないので、範囲を作れない。**
//
// 目的: いま入っているものが `dev` と名乗るとき、誤った警告を出さないこと。
// 与える情報: `dev` と名乗る実行ファイルと、印を持つ release の一覧。
// 成功条件: 終了コードが 0 で、出力に「破壊的変更」が出ないこと。
func Test_continuoを入れる_P001_版を名乗らないものからは警告しない(t *testing.T) {
	fr := newFakeRelease(t, "v0.10.0", "#!/bin/sh\necho v0.10.0\n", true)
	fr.SetReleases(releasesJSON(t, []releaseNote{
		{Tag: "v0.10.0", Breaking: []string{"版を比べられないので出てはいけません"}},
	}))
	dir := t.TempDir()
	placeInstalled(t, dir, "dev")

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	if strings.Contains(out, "破壊的変更があります") ||
		strings.Contains(out, "版を比べられないので出てはいけません") {
		t.Errorf("版を比べられないのに破壊的変更を言っています:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_一覧を引けなくても入れるのは止めない は、警告を作れない経路を確かめる。
//
// **警告は付随的なものである。**release の一覧を引けないことで導入を止めてはならない
// （設計 3-36「破壊的変更を集めるときの決めごと」の「何も言わない場合」）。
//
// 目的: 一覧が 404 でも、終了コードが 0 で実行ファイルが入れ替わること。
// 与える情報: v0.2.0 を名乗る実行ファイルと、一覧を配らない偽サーバ（/api は 404 を返す）。
// 成功条件: 終了コードが 0 で、置いたものが v0.10.0 を名乗り、警告が出ないこと。
func Test_continuoを入れる_P001_一覧を引けなくても入れるのは止めない(t *testing.T) {
	// **SetReleases を呼ばない。**releases が空なので /api は 404 を返す。
	fr := newFakeRelease(t, "v0.10.0", "#!/bin/sh\necho v0.10.0\n", true)
	dir := t.TempDir()
	placeInstalled(t, dir, "v0.2.0")

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}

	got, err := exec.Command(filepath.Join(dir, "continuo")).Output()
	if err != nil {
		t.Fatalf("置いた実行ファイルを動かせません: %v\n%s", err, out)
	}
	if !strings.Contains(string(got), "v0.10.0") {
		t.Errorf("入れ替わっていません: %q\n%s", string(got), out)
	}
	if strings.Contains(out, "破壊的変更があります") {
		t.Errorf("一覧を引けていないのに警告を出しています:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_印を読み出せなくても入れるのは止めない は、awk が落ちる経路を確かめる。
//
// **install.sh は `set -eu` で走る。**警告を組み立てる代入を `|| true` で受けていないと、
// **awk が 0 以外で終わった時点でスクリプトごと終わる。**
// collect_breaking は install_binary より先に走るので、**実行ファイルを置く前に、
// 何のメッセージも出さずに落ちる。**
//
// 目的: 印を読み出す awk が落ちても、終了コードが 0 で実行ファイルが入れ替わること。
// 与える情報: v0.2.0 を名乗る実行ファイル、印のある一覧、印を読む awk だけが落ちる PATH。
// 成功条件: 終了コードが 0 で、置いたものが v0.10.0 を名乗り、警告が出ないこと。
func Test_continuoを入れる_P001_印を読み出せなくても入れるのは止めない(t *testing.T) {
	fr := newFakeRelease(t, "v0.10.0", "#!/bin/sh\necho v0.10.0\n", true)
	fr.SetReleases(releasesJSON(t, []releaseNote{
		{Tag: "v0.10.0", Breaking: []string{"読み出せないので出てはいけません"}},
		{Tag: "v0.2.0"},
	}))
	dir := t.TempDir()
	placeInstalled(t, dir, "v0.2.0")

	cmd := exec.Command(installShell, scriptPath(t),
		"--api-url", fr.Server.URL+"/api/latest",
		"--base-url", fr.Server.URL+"/dl",
		"--no-deps")
	cmd.Stdin = nil
	detachTerminal(cmd)
	// **後ろの重複が勝つ**（os/exec が env を後勝ちで畳む）ので、PATH をここで上書きできる。
	cmd.Env = append(os.Environ(),
		"PATH="+stubBrokenBreakingAwk(t)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"CONTINUO_INSTALL_DIR="+dir)
	raw, err := cmd.CombinedOutput()
	out := string(raw)
	if err != nil {
		t.Fatalf("印を読めないだけで導入が止まっています: %v\n%s", err, out)
	}

	got, gerr := exec.Command(filepath.Join(dir, "continuo")).Output()
	if gerr != nil {
		t.Fatalf("置いた実行ファイルを動かせません: %v\n%s", gerr, out)
	}
	if !strings.Contains(string(got), "v0.10.0") {
		t.Errorf("入れ替わっていません: %q\n%s", string(got), out)
	}
	if strings.Contains(out, "破壊的変更があります") ||
		strings.Contains(out, "読み出せないので出てはいけません") {
		t.Errorf("読み出せていないのに警告を出しています:\n%s", out)
	}
}
