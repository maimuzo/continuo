// {"RUCM-CFG-SHA256": "0778f36d35db41fbe2cb1ab49f1da01e835f6b5391535ea8866bc20abb10b8c2", "SOURCE": "docs/spec/usecases/particular_case/配布物を取って置く.cfg.json"}
//
// **ユースケース記述「配布物を取って置く」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストと、そのテストだけが使う補助関数を置く。**
// 経路に対応しないテストと、ほかのファイルと共有する補助関数は、同じディレクトリの別のファイルに在る。

//go:build unix

package install_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// {"RUCM-PATH": "P001"}
//
// Test_配布物を取って置く_P001_releaseから実行ファイルを取って置く は、基本の流れを確かめる。
//
// 目的: 最新の版を引き、書庫を落とし、展開して置き先へ配ること。
// 与える情報: 偽の release サーバ（checksums.txt つき）。
// 成功条件: 置き先に実行ファイルができ、実行できること。
func Test_配布物を取って置く_P001_releaseから実行ファイルを取って置く(t *testing.T) {
	fr := newFakeRelease(t, "v1.2.3", "#!/bin/sh\necho continuo v1.2.3\n", true)
	dir := t.TempDir()

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}

	bin := filepath.Join(dir, "continuo")
	info, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("実行ファイルが置かれていません: %v\n%s", err, out)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("実行の権限が付いていません: %v", info.Mode())
	}

	// **置いたものが本当に動くか。**展開が壊れていれば、ここで落ちる。
	got, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("置いた実行ファイルを動かせません: %v", err)
	}
	if !strings.Contains(string(got), "v1.2.3") {
		t.Errorf("置いたものが違います: %q", string(got))
	}

	if !strings.Contains(out, "チェックサムを照合しました") {
		t.Errorf("チェックサムを照合していません:\n%s", out)
	}
}

// {"RUCM-PATH": "P004"}
//
// Test_配布物を取って置く_P004_チェックサムが合わなければ置かない は、改竄の検知を確かめる。
//
// **取ってきたものが途中で入れ替わっていたら、置いてはならない。**
//
// 目的: checksums.txt と中身が食い違ったら、実行ファイルを置かずに落ちること。
// 与える情報: 誤ったチェックサムを配る偽サーバ。
// 成功条件: 終了コードが 0 でなく、置き先に何も無いこと。
func Test_配布物を取って置く_P004_チェックサムが合わなければ置かない(t *testing.T) {
	fr := newFakeRelease(t, "v1.2.3", "#!/bin/sh\necho ok\n", true)
	// **中身だけを差し替える。**checksums.txt は元のままなので照合が外れる。
	fr.SetAsset(makeTarGz(t, "continuo", "#!/bin/sh\necho 差し替えられた\n"))
	dir := t.TempDir()

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code == 0 {
		t.Fatalf("チェックサムが合わないのに成功しています:\n%s", out)
	}
	if !strings.Contains(out, "チェックサムが合いません") {
		t.Errorf("理由を示していません:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "continuo")); err == nil {
		t.Error("チェックサムが合わないのに実行ファイルを置いています")
	}
}

// {"RUCM-PATH": "P005"}
//
// Test_配布物を取って置く_P005_照合できなければ既定では置かない は、照合できないときの扱いを確かめる。
//
// **checksums.txt を取れないとき、既定では止まる。**
// 以前は「照合せずに続けます」と警告して置いていたが、**取ってきたものが
// 入れ替わっていないかを確かめられないまま実行ファイルを置くことになる。**
//
// 目的: チェックサムを照合できないとき、実行ファイルを置かずに止まること。
// 与える情報: checksums.txt を配らない偽サーバ。
// 成功条件: 終了コードが 0 でなく、置き先に何も無く、承知のうえで続ける方法を示すこと。
func Test_配布物を取って置く_P005_照合できなければ既定では置かない(t *testing.T) {
	fr := newFakeRelease(t, "v1.2.3", "#!/bin/sh\necho ok\n", false)
	dir := t.TempDir()

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code == 0 {
		t.Fatalf("照合できないのに成功しています:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "continuo")); err == nil {
		t.Error("照合できないのに実行ファイルを置いています")
	}
	if !strings.Contains(out, "--insecure-no-checksum") {
		t.Errorf("承知のうえで続ける方法を示していません:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_配布物を取って置く_P001_照合を省く指定があれば置く は、明示的に許したときの扱いを確かめる。
//
// 目的: `--insecure-no-checksum` を渡したとき、照合せずに置くこと。
// 与える情報: checksums.txt を配らない偽サーバと、照合を省く指定。
// 成功条件: 実行ファイルが置かれ、照合できなかった理由を伝えること。
func Test_配布物を取って置く_P001_照合を省く指定があれば置く(t *testing.T) {
	fr := newFakeRelease(t, "v1.2.3", "#!/bin/sh\necho ok\n", false)
	dir := t.TempDir()

	code, out := runInstaller(t, fr, dir, "--no-deps", "--insecure-no-checksum")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "continuo")); err != nil {
		t.Fatalf("実行ファイルが置かれていません: %v\n%s", err, out)
	}
	if !strings.Contains(out, "checksums.txt を取得できません") {
		t.Errorf("照合できなかった理由を伝えていません:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_配布物を取って置く_P001_版を指定できる は、`--version` を確かめる。
//
// 目的: `--version` を渡したら、最新を引かずにその版を取ること。
// 与える情報: 最新として別の版を返す偽サーバ。
// 成功条件: 指定した版で取りに行くこと。
func Test_配布物を取って置く_P001_版を指定できる(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", "#!/bin/sh\necho ok\n", true)
	// **v1.0.0 も配っていることにする。**「最新を引かずに指定を使う」ことだけを確かめる。
	fr.Allow("v1.0.0")
	dir := t.TempDir()

	code, out := runInstaller(t, fr, dir, "--no-deps", "--version", "v1.0.0")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	if !strings.Contains(out, "v1.0.0") {
		t.Errorf("指定した版で取りに行っていません:\n%s", out)
	}
	if strings.Contains(out, "v9.9.9") {
		t.Errorf("最新を引いてしまっています:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_配布物を取って置く_P001_置き先を指定できる は、`--dir` を確かめる。
//
// 目的: `--dir` で置き先を変えられること。
// 与える情報: 既定と違うディレクトリ。
// 成功条件: 指定したところに実行ファイルができること。
func Test_配布物を取って置く_P001_置き先を指定できる(t *testing.T) {
	fr := newFakeRelease(t, "v1.2.3", "#!/bin/sh\necho ok\n", true)
	dir := filepath.Join(t.TempDir(), "別の場所", "bin")

	code, out := runInstaller(t, fr, t.TempDir(), "--no-deps", "--dir", dir)
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	// **無いディレクトリは作る。**利用者に mkdir をさせない。
	if _, err := os.Stat(filepath.Join(dir, "continuo")); err != nil {
		t.Fatalf("指定した置き先にありません: %v\n%s", err, out)
	}
}

// {"RUCM-PATH": "P011"}
//
// Test_配布物を取って置く_P011_releaseが1つも無ければ作り方を案内する は、配布前の状態を確かめる。
//
// **タグを打つまで release は1つも無い。**そのとき利用者に見えるものが、
// 「404」ではなく「まだ配布していません。ソースから作れます」であること。
//
// **一覧の API は、release が1つも無くても 200 と `[]` を返す**（404 はリポジトリが無いとき）。
//
// 目的: release が無いとき、理由と代わりの手順を示して止まること。
// 与える情報: 一覧に 200 の `[]` を返す偽サーバ。
// 成功条件: ソースから作る手順が出ること。
func Test_配布物を取って置く_P011_releaseが1つも無ければ作り方を案内する(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]\n"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	fr := &fakeRelease{Server: srv}
	dir := t.TempDir()

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code == 0 {
		t.Fatalf("release が無いのに成功しています:\n%s", out)
	}
	if !strings.Contains(out, "release がまだ1つもありません") {
		t.Errorf("理由を示していません:\n%s", out)
	}
	if !strings.Contains(out, "go build") {
		t.Errorf("ソースから作る手順を示していません:\n%s", out)
	}
}

// {"RUCM-PATH": "P006"}
//
// Test_配布物を取って置く_P006_指定した版の配布物が無ければ置かない は、取得の失敗を確かめる。
//
// **利用者が版を打ち間違えることがある。**そのとき 404 の生のメッセージではなく、
// 「どこを見れば配布しているものが分かるか」を示す。
//
// 目的: 書庫を取得できないとき、実行ファイルを置かずに止まること。
// 与える情報: その版の書庫を持たない偽サーバ。
// 成功条件: 終了コードが 0 でなく、置き先に何も無く、一時ディレクトリも残らないこと。
func Test_配布物を取って置く_P006_指定した版の配布物が無ければ置かない(t *testing.T) {
	fr := newFakeRelease(t, "v1.2.3", "#!/bin/sh\necho ok\n", true)
	dir := t.TempDir()

	// **その版のパスには何も置かれていない**（偽サーバは v9.0.0 を知らない）。
	code, out := runInstaller(t, fr, dir, "--no-deps", "--version", "v9.0.0")
	if code == 0 {
		t.Fatalf("配布物が無いのに成功しています:\n%s", out)
	}
	if !strings.Contains(out, "がありません") {
		t.Errorf("何が無いのかを示していません:\n%s", out)
	}
	if !strings.Contains(out, "releases") {
		t.Errorf("配布している一覧の場所を示していません:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "continuo")); err == nil {
		t.Error("取得に失敗したのに実行ファイルを置いています")
	}
}

// {"RUCM-PATH": "P016"}
//
// Test_配布物を取って置く_P016_WindowsではWSL2を案内して止まる は、対応しない OS の扱いを確かめる。
//
// **herdr の Windows 版が安定していないため、continuo は Windows ネイティブに対応しない**
// （設計 3-32b）。**黙って失敗させず、代わりに何を使えばよいかを示す。**
//
// 目的: Windows と見分けたら、何も置かずに WSL2 を案内すること。
// 与える情報: `uname -s` が MINGW64_NT を返す環境。
// 成功条件: 終了コードが 0 でなく、案内に WSL2 が入り、置き先に何も無いこと。
func Test_配布物を取って置く_P016_WindowsではWSL2を案内して止まる(t *testing.T) {
	dir := t.TempDir()
	code, out := runWithFakeUname(t, "MINGW64_NT-10.0", "x86_64", dir)
	if code == 0 {
		t.Fatalf("Windows なのに成功しています:\n%s", out)
	}
	if !strings.Contains(out, "WSL2") {
		t.Errorf("WSL2 を案内していません:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "continuo")); err == nil {
		t.Error("Windows なのに実行ファイルを置いています")
	}
}

// {"RUCM-PATH": "P015"}
//
// Test_配布物を取って置く_P015_対応しない命令セットは対応表を出して止まる は、命令セットの検査を確かめる。
//
// 目的: 対応していない命令セットで、何も置かずに止まること。
// 与える情報: `uname -m` が i386 を返す環境。
// 成功条件: 終了コードが 0 でなく、対応している命令セットを示すこと。
func Test_配布物を取って置く_P015_対応しない命令セットは対応表を出して止まる(t *testing.T) {
	dir := t.TempDir()
	code, out := runWithFakeUname(t, "Linux", "i386", dir)
	if code == 0 {
		t.Fatalf("対応しない命令セットなのに成功しています:\n%s", out)
	}
	if !strings.Contains(out, "x86-64") || !strings.Contains(out, "arm64") {
		t.Errorf("対応している命令セットを示していません:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "continuo")); err == nil {
		t.Error("対応しない命令セットなのに実行ファイルを置いています")
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_配布物を取って置く_P001_照合できず端末も無ければ置くだけで終わる は、2つの欠落が重なる経路を確かめる。
//
// **無人で流したときに、checksums.txt の載せ忘れた配布物を掴むことがある。**
// そのとき continuo は、照合していないことを伝えたうえで置き、道具は1つも入れない。
//
// 目的: チェックサムを照合できず、端末も無いとき、実行ファイルだけを置いて終わること。
// 与える情報: checksums.txt を配らない偽サーバと、端末の無い実行。
// 成功条件: 実行ファイルが置かれ、照合していない注意が出て、道具を1つも入れないこと。
func Test_配布物を取って置く_P001_照合できず端末も無ければ置くだけで終わる(t *testing.T) {
	fr := newFakeRelease(t, "v1.2.3", "#!/bin/sh\necho ok\n", false)
	dir := t.TempDir()

	// **PATH を絞ってすべての道具を「無い」ことにする。**それでも1つも入れない。
	cmd := exec.Command(installShell, scriptPath(t),
		"--api-url", fr.Server.URL+"/api/latest",
		"--base-url", fr.Server.URL+"/dl",
		"--insecure-no-checksum")
	cmd.Stdin = nil
	detachTerminal(cmd)
	cmd.Env = []string{
		"HOME=" + t.TempDir(),
		"PATH=/usr/bin:/bin",
		"CONTINUO_INSTALL_DIR=" + dir,
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("終了コードが 0 ではありません: %v\n%s", err, out)
	}
	text := string(out)

	if _, err := os.Stat(filepath.Join(dir, "continuo")); err != nil {
		t.Fatalf("実行ファイルが置かれていません: %v\n%s", err, text)
	}
	if !strings.Contains(text, "checksums.txt を取得できません") {
		t.Errorf("照合できなかったことを伝えていません:\n%s", text)
	}
	if !strings.Contains(text, "まだ足りないもの") {
		t.Errorf("足りない道具を並べていません:\n%s", text)
	}
	if strings.Contains(text, "を入れました") {
		t.Errorf("端末が無いのに道具を入れています:\n%s", text)
	}
}

// pathWithOnly は、名指しした道具だけが在る PATH を作る。
//
// **`/usr/bin` を PATH に入れない。**入れると curl が見つかってしまい、
// 「curl も wget も無い」を作れない。一時ディレクトリへ、要る道具だけを symlink する。
//
// t: 呼び出し元のテスト。
// tools: 置く道具の名前。この機械に無いものは、テストを飛ばす。
// 戻り値: 道具だけを置いたディレクトリ。そのまま PATH の値に使う。
func pathWithOnly(t *testing.T, tools ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range tools {
		real, err := exec.LookPath(name)
		if err != nil {
			t.Skipf("%s がありません: %v", name, err)
		}
		if err := os.Symlink(real, filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s を置けません: %v", name, err)
		}
	}
	return dir
}

// {"RUCM-PATH": "P014"}
//
// Test_配布物を取って置く_P014_curlもwgetも無ければ理由を出して止まる は、取ってこられない環境で黙って終わらないことを確かめる。
//
// **直す前は、理由を1行も出さずに終わっていた。**「curl も wget もありません」は、
// 標準エラーを捨てている呼び出しの中で出ていたので、画面に届かなかった。
// 版を指定しない実行では、代わりに「まだ配布していません」と、事実と違う案内が出ていた。
//
// 目的: curl も wget も無いとき、配布サーバへ繋ぐ前に、理由を出して終了コード 1 で止まること。
// 与える情報: `uname` だけが在る PATH。版を指定した実行と、指定しない実行の両方。
// 成功条件: どちらも終了コードが 1 で、「curl も wget もありません」が出て、
// 「まだ配布していません」は出ず、偽サーバへ1度も繋がず、実行ファイルを置かないこと。
func Test_配布物を取って置く_P014_curlもwgetも無ければ理由を出して止まる(t *testing.T) {
	fr := newFakeRelease(t, "v1.2.3", "#!/bin/sh\necho ok\n", true)
	var reached atomic.Bool
	inner := fr.Server.Config.Handler
	fr.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
		inner.ServeHTTP(w, r)
	})
	// **`uname` は要る。**OS と命令セットを見分ける段が、取ってこられるかの確かめより前に在る。
	path := pathWithOnly(t, "uname")

	cases := []struct {
		name string
		args []string
	}{
		{"版を指定しない", nil},
		{"版を指定する", []string{"--version", "v1.2.3"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			args := append([]string{scriptPath(t),
				"--api-url", fr.Server.URL + "/api/latest",
				"--base-url", fr.Server.URL + "/dl",
				"--no-deps"}, tc.args...)
			// **シェルは絶対パスで起動する。**絞った PATH からは `sh` を引けない。
			shell, err := exec.LookPath(installShell)
			if err != nil {
				t.Fatalf("シェル %s を引けません: %v", installShell, err)
			}
			cmd := exec.Command(shell, args...)
			cmd.Stdin = nil
			detachTerminal(cmd)
			cmd.Env = []string{
				"HOME=" + t.TempDir(),
				"PATH=" + path,
				"CONTINUO_INSTALL_DIR=" + dir,
			}
			out, runErr := cmd.CombinedOutput()
			text := string(out)
			code := 0
			if ee, ok := runErr.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else if runErr != nil {
				t.Fatalf("install.sh を起動できません: %v", runErr)
			}

			if code != 1 {
				t.Errorf("終了コードが 1 ではありません: %d\n%s", code, text)
			}
			if !strings.Contains(text, "エラー: curl も wget もありません。どちらかを入れてください") {
				t.Errorf("理由を出していません:\n%s", text)
			}
			if strings.Contains(text, "まだ配布していません") {
				t.Errorf("取ってこられないだけなのに、配布が無いと案内しています:\n%s", text)
			}
			if _, err := os.Stat(filepath.Join(dir, "continuo")); err == nil {
				t.Error("取ってこられないのに実行ファイルを置いています")
			}
		})
	}
	if reached.Load() {
		t.Error("curl も wget も無いのに配布サーバへ繋いでいます")
	}
}

// installedVersionOf は、置き先の continuo が名乗る版を返す。
//
// dir: 置き先。
// 戻り値: `continuo` を引数無しで動かしたときの出力（前後の空白を落とす）。
func installedVersionOf(t *testing.T, dir string) string {
	t.Helper()
	got, err := exec.Command(filepath.Join(dir, "continuo")).Output()
	if err != nil {
		t.Fatalf("置き先の実行ファイルを動かせません: %v", err)
	}
	return strings.TrimSpace(string(got))
}

// cooldownRelease は、クールダウンを確かめる偽サーバを立てる。
//
// **書庫は1つだけ配る**（どの版を要求しても、中身は `echo <body>` の同じもの）。
// 何を入れたかは、出力の「入れる版:」と、要求された版（Allow で配る版を絞る）で確かめる。
//
// tag: 書庫の中身が名乗る版。notes: 一覧に並べる release（新しい版から順に）。
// 戻り値: 立てたサーバ。notes の版は全部配る。
func cooldownRelease(t *testing.T, tag string, notes []releaseNote) *fakeRelease {
	t.Helper()
	fr := newFakeRelease(t, tag, "#!/bin/sh\necho "+tag+"\n", true)
	for _, n := range notes {
		fr.Allow(n.Tag)
	}
	fr.SetReleases(releasesJSON(t, notes))
	return fr
}

// {"RUCM-PATH": "P001"}
//
// Test_配布物を取って置く_P001_クールダウン_既定では公開から168時間経っていない版を見送る は、クールダウンの基本を確かめる。
//
// 目的: 版を指定しない実行が、公開から 168 時間経っていない版を見送り、経った版を入れること。
// 与える情報: v0.3.1（公開から 30 時間）と v0.3.0（公開から 249 時間）を並べた一覧。
// 成功条件: v0.3.0 を入れ、見送った版と、0 にする1行と、時間の変え方を出すこと。
func Test_配布物を取って置く_P001_クールダウン_既定では公開から168時間経っていない版を見送る(t *testing.T) {
	fr := cooldownRelease(t, "v0.3.0", []releaseNote{
		{Tag: "v0.3.1", Age: 30 * time.Hour},
		{Tag: "v0.3.0", Age: 249 * time.Hour},
	})
	dir := t.TempDir()

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	for _, want := range []string{
		"クールダウン: 公開から 168 時間（7日）経った版だけを入れます。",
		"  見送った版: v0.3.1（",
		"公開から 30 時間。あと 138 時間で入れられます）",
		"  入れる版:   v0.3.0（",
		"公開から 249 時間）",
		"  見送った版をすぐに入れるときは、クールダウンを 0 にします。",
		"--cooldown-hours 0",
		"  時間を変えるときは --cooldown-hours <時間> を付けます（例: --cooldown-hours 24）。",
		"取ってきます: " + assetName() + " (v0.3.0)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が出ていません:\n%s", want, out)
		}
	}
	if got := installedVersionOf(t, dir); got != "v0.3.0" {
		t.Errorf("v0.3.0 を入れていません: %q", got)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_配布物を取って置く_P001_クールダウン_0時間なら最新の正式版を入れる は、クールダウンを外す道を確かめる。
//
// 目的: `--cooldown-hours 0` で、公開直後の版も入れること。
// 与える情報: v0.3.1（公開から 30 時間）と v0.3.0。
// 成功条件: v0.3.1 を取ってくること。
func Test_配布物を取って置く_P001_クールダウン_0時間なら最新の正式版を入れる(t *testing.T) {
	fr := cooldownRelease(t, "v0.3.1", []releaseNote{
		{Tag: "v0.3.1", Age: 30 * time.Hour},
		{Tag: "v0.3.0", Age: 249 * time.Hour},
	})
	dir := t.TempDir()

	code, out := runInstaller(t, fr, dir, "--no-deps", "--cooldown-hours", "0")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	for _, want := range []string{
		"クールダウン: 0 時間を指定されたので、公開からの時間で絞りません。",
		"  入れる版:   v0.3.1（",
		"取ってきます: " + assetName() + " (v0.3.1)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が出ていません:\n%s", want, out)
		}
	}
	if strings.Contains(out, "見送った版") {
		t.Errorf("0 時間なのに版を見送っています:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_配布物を取って置く_P001_クールダウン_時間を変えられる は、24 の倍数でない時間の表示を確かめる。
//
// 目的: `--cooldown-hours 08` を 8 時間と読み、日数の括弧を付けないこと。
// 与える情報: v0.3.1（公開から 30 時間）と v0.3.0。
// 成功条件: v0.3.1 を入れ、「公開から 8 時間経った版だけを入れます。」と出すこと。
func Test_配布物を取って置く_P001_クールダウン_時間を変えられる(t *testing.T) {
	fr := cooldownRelease(t, "v0.3.1", []releaseNote{
		{Tag: "v0.3.1", Age: 30 * time.Hour},
		{Tag: "v0.3.0", Age: 249 * time.Hour},
	})
	dir := t.TempDir()

	code, out := runInstaller(t, fr, dir, "--no-deps", "--cooldown-hours", "08")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	if !strings.Contains(out, "クールダウン: 公開から 8 時間経った版だけを入れます。") {
		t.Errorf("8 時間として読んでいません:\n%s", out)
	}
	if !strings.Contains(out, "  入れる版:   v0.3.1（") {
		t.Errorf("v0.3.1 を入れていません:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_配布物を取って置く_P001_クールダウン_版を指定すれば使わない は、`--version` の実行を確かめる。
//
// 目的: 版を名指ししたときは、公開から時間が経っていなくても、その版を入れること。
// 与える情報: v0.3.1（公開から 30 時間）を名指しする。
// 成功条件: v0.3.1 を入れ、クールダウンを使わないと言うこと。
func Test_配布物を取って置く_P001_クールダウン_版を指定すれば使わない(t *testing.T) {
	fr := cooldownRelease(t, "v0.3.1", []releaseNote{
		{Tag: "v0.3.1", Age: 30 * time.Hour},
		{Tag: "v0.3.0", Age: 249 * time.Hour},
	})
	dir := t.TempDir()

	code, out := runInstaller(t, fr, dir, "--no-deps", "--version", "v0.3.1")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	if !strings.Contains(out, "クールダウン: --version で版を指定されたので、使いません。") {
		t.Errorf("クールダウンを使わないと言っていません:\n%s", out)
	}
	if got := installedVersionOf(t, dir); got != "v0.3.1" {
		t.Errorf("v0.3.1 を入れていません: %q", got)
	}
}

// {"RUCM-PATH": "P010"}
//
// Test_配布物を取って置く_P010_クールダウン_入れてよい版が無ければ止まる は、公開直後の版しか無いときを確かめる。
//
// 目的: 公開から 168 時間経った版が1つも無ければ、何も置かずに止まり、待たずに入れる1行を示すこと。
// 与える情報: v0.1.0（公開から 12 時間）だけの一覧。
// 成功条件: 終了コードが 1 で、いちばん早く入れられる版と 0 にする1行を出し、何も置かないこと。
func Test_配布物を取って置く_P010_クールダウン_入れてよい版が無ければ止まる(t *testing.T) {
	fr := cooldownRelease(t, "v0.1.0", []releaseNote{
		{Tag: "v0.1.0", Age: 12 * time.Hour},
	})
	dir := t.TempDir()

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 1 {
		t.Fatalf("終了コードが 1 ではありません: %d\n%s", code, out)
	}
	for _, want := range []string{
		"クールダウン: 公開から 168 時間（7日）経った版がありません。",
		"  いちばん早く入れられる版: v0.1.0（",
		"公開から 12 時間。あと 156 時間で入れられます）",
		"--cooldown-hours 0",
		"エラー: 公開から 168 時間経った版が無いので、入れませんでした",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が出ていません:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "continuo")); err == nil {
		t.Error("入れてよい版が無いのに実行ファイルを置いています")
	}
}

// {"RUCM-PATH": "P011"}
//
// Test_配布物を取って置く_P011_クールダウン_正式版が無ければ作り方を案内する は、prerelease と draft だけの一覧を確かめる。
//
// 目的: 正式版が1つも無ければ、0 を勧めずに「まだ1つもありません」へ行くこと（0 にしても入らないため）。
// 与える情報: prerelease 1件と draft 1件だけの一覧。
// 成功条件: 「release がまだ1つもありません」を出し、何も置かないこと。
func Test_配布物を取って置く_P011_クールダウン_正式版が無ければ作り方を案内する(t *testing.T) {
	fr := cooldownRelease(t, "v0.2.0-rc1", []releaseNote{
		{Tag: "v0.3.0", Draft: true},
		{Tag: "v0.2.0-rc1", Prerelease: true},
	})
	dir := t.TempDir()

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code == 0 {
		t.Fatalf("正式版が無いのに成功しています:\n%s", out)
	}
	if !strings.Contains(out, "release がまだ1つもありません") {
		t.Errorf("作り方の案内へ行っていません:\n%s", out)
	}
	if strings.Contains(out, "--cooldown-hours 0") {
		t.Errorf("入らない 0 の1行を勧めています:\n%s", out)
	}
}

// {"RUCM-PATH": "P013"}
//
// Test_配布物を取って置く_P013_クールダウン_一覧を引けなければ止まる は、版を指定しない実行の一覧の失敗を確かめる。
//
// 目的: 版を指定しない実行で一覧が 404 なら、リポジトリが見つからないと言って止まること。
// 与える情報: 一覧に 404 を返す偽サーバ（FailList）。
// 成功条件: 終了コードが 0 でなく、理由を出し、何も置かないこと。
func Test_配布物を取って置く_P013_クールダウン_一覧を引けなければ止まる(t *testing.T) {
	fr := newFakeRelease(t, "v0.3.0", "#!/bin/sh\necho v0.3.0\n", true)
	fr.FailList()
	dir := t.TempDir()

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code == 0 {
		t.Fatalf("一覧を引けないのに成功しています:\n%s", out)
	}
	if !strings.Contains(out, "リポジトリが見つかりません") {
		t.Errorf("理由を出していません:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "continuo")); err == nil {
		t.Error("一覧を引けないのに実行ファイルを置いています")
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_配布物を取って置く_P001_クールダウン_選んだ版より小さい版は見送った版に数えない は、古い系列の修正版を確かめる。
//
// 目的: 版の数がいちばん大きい版を選び、それより小さい版は時間が経っていなくても見送った版に数えないこと。
// 与える情報: v0.4.0（公開から 200 時間）と、あとから出した v0.3.9（公開から 10 時間）。
// 成功条件: v0.4.0 を入れ、「見送った版」も 0 の1行も出さないこと。
func Test_配布物を取って置く_P001_クールダウン_選んだ版より小さい版は見送った版に数えない(t *testing.T) {
	fr := cooldownRelease(t, "v0.4.0", []releaseNote{
		{Tag: "v0.3.9", Age: 10 * time.Hour},
		{Tag: "v0.4.0", Age: 200 * time.Hour},
	})
	dir := t.TempDir()

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	if !strings.Contains(out, "  入れる版:   v0.4.0（") {
		t.Errorf("v0.4.0 を入れていません:\n%s", out)
	}
	if strings.Contains(out, "見送った版") || strings.Contains(out, "--cooldown-hours 0") {
		t.Errorf("小さい版を見送った版に数えています:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_配布物を取って置く_P001_クールダウン_1行のJSONでも読める は、一覧の読み方の丈夫さを確かめる。
//
// **本物の応答は整形されているが、1行にまとまっていても同じ読み方で読めることにしている**（設計 3-36）。
// あわせて、題名（`name`）の値が `draft` という文字でも、鍵と取り違えないことを確かめる。
//
// 目的: 1行の JSON から、正式版と prerelease を見分けて版を選ぶこと。
// 与える情報: 1行にまとめた一覧。v0.5.0 は prerelease、v0.4.0 は題名が "draft" の正式版。
// 成功条件: v0.4.0 を入れること。
func Test_配布物を取って置く_P001_クールダウン_1行のJSONでも読める(t *testing.T) {
	fr := newFakeRelease(t, "v0.4.0", "#!/bin/sh\necho v0.4.0\n", true)
	old := time.Now().UTC().Add(-200 * time.Hour).Format("2006-01-02T15:04:05Z")
	list := []releaseJSONEntry{
		{TagName: "v0.5.0", Name: "prerelease", Prerelease: true, PublishedAt: &old, Body: "x"},
		{TagName: "v0.4.0", Name: "draft", PublishedAt: &old, Body: "y"},
	}
	b, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("一覧を作れません: %v", err)
	}
	fr.SetReleases(b)
	dir := t.TempDir()

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	if !strings.Contains(out, "  入れる版:   v0.4.0（") {
		t.Errorf("v0.4.0 を選んでいません:\n%s", out)
	}
}

// {"RUCM-PATH": "P009"}
//
// Test_配布物を取って置く_P009_クールダウン_下がるときは入れ替えない は、既定では版を下げないことを確かめる。
//
// 目的: クールダウンで選んだ版がいま入っている版より古ければ、入れ替えずに終了コード 0 で終わること。
// 与える情報: v0.3.1 を名乗る実行ファイルと、v0.3.1（公開から 30 時間）と v0.3.0 の一覧。
// 成功条件: 置き先は v0.3.1 のまま。下がることと `--force` の1行と、最後の枠を出すこと。
func Test_配布物を取って置く_P009_クールダウン_下がるときは入れ替えない(t *testing.T) {
	fr := cooldownRelease(t, "v0.3.0", []releaseNote{
		{Tag: "v0.3.1", Age: 30 * time.Hour},
		{Tag: "v0.3.0", Age: 249 * time.Hour},
	})
	dir := t.TempDir()
	placeInstalled(t, dir, "v0.3.1")

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	for _, want := range []string{
		"  選んだ版:   v0.3.0（",
		"  いま入っているのは v0.3.1 です。クールダウンを当てると v0.3.1 → v0.3.0 へ版が下がるので、入れ替えません。",
		"  v0.3.0 へ下げるときは、--force を付けます。",
		"--force",
		" 実行ファイルは入れ替えていません。いまは v0.3.1 のままです。",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が出ていません:\n%s", want, out)
		}
	}
	if strings.Contains(out, "取ってきます:") {
		t.Errorf("入れ替えないのに書庫を取っています:\n%s", out)
	}
	if strings.Contains(out, "見送った版をすぐに入れるときは") {
		t.Errorf("いまの版より大きい見送った版が無いのに、0 の案内を出しています:\n%s", out)
	}
	if got := installedVersionOf(t, dir); got != "v0.3.1" {
		t.Errorf("入れ替えています: %q", got)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_配布物を取って置く_P001_クールダウン_forceを付ければ下げる は、`--force` を確かめる。
//
// 目的: `--force` を付ければ、選んだ版がいま入っている版より古くても入れ替えること。
// 与える情報: 上と同じ状況に `--force` を足す。
// 成功条件: v0.3.0 に入れ替わり、下げると言い、「入れ替えません」を出さないこと。
func Test_配布物を取って置く_P001_クールダウン_forceを付ければ下げる(t *testing.T) {
	fr := cooldownRelease(t, "v0.3.0", []releaseNote{
		{Tag: "v0.3.1", Age: 30 * time.Hour},
		{Tag: "v0.3.0", Age: 249 * time.Hour},
	})
	dir := t.TempDir()
	placeInstalled(t, dir, "v0.3.1")

	code, out := runInstaller(t, fr, dir, "--no-deps", "--force")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	if !strings.Contains(out, "  いま入っているのは v0.3.1 です。--force が付いているので、v0.3.1 → v0.3.0 へ版を下げます。") {
		t.Errorf("下げると言っていません:\n%s", out)
	}
	if strings.Contains(out, "入れ替えません") || strings.Contains(out, "入れ替えていません") {
		t.Errorf("--force なのに入れ替えないと言っています:\n%s", out)
	}
	if got := installedVersionOf(t, dir); got != "v0.3.0" {
		t.Errorf("v0.3.0 へ下げていません: %q", got)
	}
}

// {"RUCM-PATH": "P009"}
//
// Test_配布物を取って置く_P009_クールダウン_上の版を見送ったときは0の案内も出す は、上へ行く道を確かめる。
//
// 目的: 見送った版にいま入っている版より大きいものがあれば、0 にする案内も出すこと。
// 与える情報: v0.3.1 を名乗る実行ファイルと、v0.3.2（30 時間）・v0.3.1（100 時間）・v0.3.0（249 時間）。
// 成功条件: 入れ替えず、0 の案内と `--force` の案内の両方を出すこと。
func Test_配布物を取って置く_P009_クールダウン_上の版を見送ったときは0の案内も出す(t *testing.T) {
	fr := cooldownRelease(t, "v0.3.0", []releaseNote{
		{Tag: "v0.3.2", Age: 30 * time.Hour},
		{Tag: "v0.3.1", Age: 100 * time.Hour},
		{Tag: "v0.3.0", Age: 249 * time.Hour},
	})
	dir := t.TempDir()
	placeInstalled(t, dir, "v0.3.1")

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	for _, want := range []string{
		"  見送った版: v0.3.2（",
		"  見送った版: v0.3.1（",
		"  見送った版をすぐに入れるときは、クールダウンを 0 にします。",
		"  v0.3.0 へ下げるときは、--force を付けます。",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が出ていません:\n%s", want, out)
		}
	}
	if strings.Index(out, "見送った版: v0.3.2") > strings.Index(out, "見送った版: v0.3.1") {
		t.Errorf("見送った版を大きい順に並べていません:\n%s", out)
	}
	if got := installedVersionOf(t, dir); got != "v0.3.1" {
		t.Errorf("入れ替えています: %q", got)
	}
}

// {"RUCM-PATH": "P009"}
//
// Test_配布物を取って置く_P009_クールダウン_取り下げられた版が入っていればそう言う は、クールダウンのせいでない下がり方を確かめる。
//
// 目的: いま入っている版が一覧の正式版のどれより大きいとき、クールダウンのせいにせずに入れ替えないこと。
// 与える情報: v0.3.2 を名乗る実行ファイルと、v0.3.1 だけの一覧（v0.3.2 は取り下げられた）。
// 成功条件: 「配布している正式版のどれより新しい版です」と言い、入れ替えないこと。
func Test_配布物を取って置く_P009_クールダウン_取り下げられた版が入っていればそう言う(t *testing.T) {
	fr := cooldownRelease(t, "v0.3.1", []releaseNote{
		{Tag: "v0.3.1", Age: 200 * time.Hour},
	})
	dir := t.TempDir()
	placeInstalled(t, dir, "v0.3.2")

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	if !strings.Contains(out, "  いま入っているのは v0.3.2 です。配布している正式版のどれより新しい版です（取り下げられた版か prerelease の可能性があります）。入れ替えません。") {
		t.Errorf("取り下げの文面を出していません:\n%s", out)
	}
	if strings.Contains(out, "クールダウンを当てると") {
		t.Errorf("クールダウンのせいにしています:\n%s", out)
	}
	if got := installedVersionOf(t, dir); got != "v0.3.2" {
		t.Errorf("入れ替えています: %q", got)
	}
}

// {"RUCM-PATH": "P009"}
//
// Test_配布物を取って置く_P009_クールダウン_案内の1行は置き先を引き継ぐ は、貼らせる1行の組み立てを確かめる。
//
// **置き先を環境変数 `CONTINUO_INSTALL_DIR` で変えた人にも、`--dir` を足した1行を出す。**
// 足さないと、貼った1行が既定の置き先へ入れ、元の版が残る。
// runInstaller は置き先を環境変数で渡すので、このテストはその経路そのものである。
//
// 目的: 案内の1行に、いまの置き先と、既定と違う時間が入ること。
// 与える情報: 環境変数で渡した置き先と `--cooldown-hours 200`、v0.3.1（30 時間）と v0.3.0（249 時間）。
// 成功条件: `--force` の1行に `--dir <置き先>` と `--cooldown-hours 200` が入ること。
func Test_配布物を取って置く_P009_クールダウン_案内の1行は置き先を引き継ぐ(t *testing.T) {
	fr := cooldownRelease(t, "v0.3.0", []releaseNote{
		{Tag: "v0.3.1", Age: 30 * time.Hour},
		{Tag: "v0.3.0", Age: 249 * time.Hour},
	})
	dir := t.TempDir()
	placeInstalled(t, dir, "v0.3.1")

	code, out := runInstaller(t, fr, dir, "--no-deps", "--cooldown-hours", "200")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	// **install.sh は、英数字と `. _ / - +` 以外を含む値を一重引用符で囲む。**
	// t.TempDir の名前にはテストの関数名（日本語）が入るので、ここでは囲まれる。
	quoted := dir
	if strings.Trim(dir, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._/+-") != "" {
		quoted = "'" + strings.ReplaceAll(dir, "'", `'\''`) + "'"
	}
	want := "install.sh | sh -s -- --dir " + quoted + " --cooldown-hours 200 --force"
	if !strings.Contains(out, want) {
		t.Errorf("%q が出ていません:\n%s", want, out)
	}
	if !strings.Contains(out, "https://raw.githubusercontent.com/maimuzo/continuo/main/install.sh") {
		t.Errorf("既定のリポジトリの install.sh を指していません:\n%s", out)
	}
}
