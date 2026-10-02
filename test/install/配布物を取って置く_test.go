// {"RUCM-CFG-SHA256": "2b1a54cf11dcdbff9447c0871679a7abb1888f566c78ddedddefebdb87ef4b73", "SOURCE": "docs/spec/usecases/particular_case/配布物を取って置く.cfg.json"}
//
// **ユースケース記述「配布物を取って置く」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。

//go:build unix

package install_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

// {"RUCM-PATH": "P008"}
//
// Test_配布物を取って置く_P008_releaseが1つも無ければ作り方を案内する は、配布前の状態を確かめる。
//
// **タグを打つまで release は1つも無い。**そのとき利用者に見えるものが、
// 「404」ではなく「まだ配布していません。ソースから作れます」であること。
//
// 目的: release が無いとき、理由と代わりの手順を示して止まること。
// 与える情報: 空の応答を返す偽サーバ。
// 成功条件: ソースから作る手順が出ること。
func Test_配布物を取って置く_P008_releaseが1つも無ければ作り方を案内する(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/latest", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
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

// {"RUCM-PATH": "P011"}
//
// Test_配布物を取って置く_P011_WindowsではWSL2を案内して止まる は、対応しない OS の扱いを確かめる。
//
// **herdr の Windows 版が安定していないため、continuo は Windows ネイティブに対応しない**
// （設計 3-32b）。**黙って失敗させず、代わりに何を使えばよいかを示す。**
//
// 目的: Windows と見分けたら、何も置かずに WSL2 を案内すること。
// 与える情報: `uname -s` が MINGW64_NT を返す環境。
// 成功条件: 終了コードが 0 でなく、案内に WSL2 が入り、置き先に何も無いこと。
func Test_配布物を取って置く_P011_WindowsではWSL2を案内して止まる(t *testing.T) {
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

// {"RUCM-PATH": "P010"}
//
// Test_配布物を取って置く_P010_対応しない命令セットは対応表を出して止まる は、命令セットの検査を確かめる。
//
// 目的: 対応していない命令セットで、何も置かずに止まること。
// 与える情報: `uname -m` が i386 を返す環境。
// 成功条件: 終了コードが 0 でなく、対応している命令セットを示すこと。
func Test_配布物を取って置く_P010_対応しない命令セットは対応表を出して止まる(t *testing.T) {
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
