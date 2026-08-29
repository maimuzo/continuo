// {"RUCM-CFG-SHA256": "09cc6cc92ec349aca287a8364c8dfef08ae9b91a49cf522e6612d6f7be0af4ff", "SOURCE": "docs/spec/usecases/particular_case/continuo を入れる.cfg.json"}
//go:build unix

// ネットワークインストーラー（install.sh）の検査である（設計 3-36）。
//
// **利用者が continuo に対して最初に叩くのが install.sh である。**
// ここが壊れていると、continuo が正しくても誰も使い始められない。
//
// **偽の release サーバを立てて、実際に `sh install.sh` を走らせる。**
// GitHub に release を作る前に、取得・照合・展開・配置が通ることを確かめられる。
// 取得先は `--base-url` と `--api-url` で差し替える（**テスト専用のフラグである**）。
package install_test

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
)

// scriptPath は install.sh の絶対パスを返す。
//
// t: 呼び出し元のテスト。
// 戻り値: リポジトリ直下の install.sh のパス。
func scriptPath(t *testing.T) string {
	t.Helper()
	// このファイルは test/install/ にある。2つ上がリポジトリの直下である。
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("作業ディレクトリを引けません: %v", err)
	}
	p := filepath.Join(wd, "..", "..", "install.sh")
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("絶対パスにできません: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("install.sh がありません: %v", err)
	}
	return abs
}

// assetName は、いま走っている環境に合う配布物の名前を返す。
//
// **install.sh が uname から組み立てる名前と同じものを作る。**
// 食い違うと、テストは 404 を見て落ちる（それも検査のうちである）。
func assetName() string {
	return fmt.Sprintf("continuo_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
}

// makeTarGz は、中に1つの実行ファイルを持つ書庫を作る。
//
// name: 書庫の中でのファイル名。
// body: そのファイルの中身。
// 戻り値: gzip 圧縮した tar の中身。
func makeTarGz(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf strings.Builder
	gz := gzip.NewWriter(&stringWriter{&buf})
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{
		Name: name,
		Mode: 0o755,
		Size: int64(len(body)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar のヘッダを書けません: %v", err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatalf("tar の中身を書けません: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar を閉じられません: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip を閉じられません: %v", err)
	}
	return []byte(buf.String())
}

// stringWriter は strings.Builder を io.Writer として使うための薄い包みである。
type stringWriter struct{ b *strings.Builder }

func (w *stringWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

// installShell は、いま試しているシェルである。
//
// **TestMain がシェルごとに全テストを走らせる。**`sh` と `dash` で落ちる条件が違うので、
// 片方だけでは足りない。
var installShell = "sh"

// TestMain は、使えるシェルそれぞれで全テストを走らせる。
func TestMain(m *testing.M) {
	list := []string{"sh"}
	if p, err := exec.LookPath("dash"); err == nil {
		list = append(list, p)
	}
	for _, sh := range list {
		installShell = sh
		if code := m.Run(); code != 0 {
			fmt.Fprintf(os.Stderr, "シェル %s で落ちました\n", sh)
			os.Exit(code)
		}
	}
	os.Exit(0)
}

// fakeRelease は偽の release サーバである。
type fakeRelease struct {
	// Server は立てた HTTP サーバである。
	Server *httptest.Server
	// Tag は最新として返す版である。
	Tag string
	// asset は配る書庫の中身である。**mu が守る**（サーバを立てたあとに差し替えるテストがある）。
	asset []byte
	// Checksums は checksums.txt の中身である。空なら 404 を返す。
	Checksums string
	// mu は Available を守る。
	//
	// **handler は別の goroutine で走る。**テスト本体が配る版を足すのと同時に読むので、
	// 排他が無いと `-race` が競合を報告する（実測で2件出た）。
	mu sync.Mutex
	// available は実際に配っている版である。
	//
	// **これを持たないと、どの版を要求しても同じ書庫を返してしまう。**
	// 「指定した版が無い」を試せなくなる（実際、最初の版では試せていなかった）。
	available map[string]bool
	// releases は release の一覧として返す JSON である。空なら 404 を返す。**mu が守る。**
	releases []byte
}

// releaseNote は、偽の release サーバが返す release 1件である。
type releaseNote struct {
	// Tag はその release の版である。
	Tag string
	// Breaking は破壊的変更の説明である。空なら本文に印を置かない。
	Breaking []string
}

// releaseJSONEntry は、応答の中の release 1件である。
//
// **並び順に意味がある。**GitHub は1つの release の中で `tag_name` を `body` より先に返し、
// **install.sh はその順番を頼りに、印がどの版のものかを決めている**
// （api.github.com の応答で、どの release でも tag_name の行が body の行より前に来ることを確かめた）。
// map で作ると鍵の名前の順に並び替えられ、**本物と違う並びの応答を検査することになる。**
type releaseJSONEntry struct {
	// TagName はその release の版である。
	TagName string `json:"tag_name"`
	// Name は release の題名である。
	Name string `json:"name"`
	// Body は release の本文である。破壊的変更の印はここに入る。
	Body string `json:"body"`
}

// releasesJSON は、GitHub API の release の一覧と同じ形の JSON を作る。
//
// **改行は `\r\n` にする。**GitHub が返す本文はその形であり、
// install.sh は JSON の中の `\r\n` を数えて1行ずつに分けている。
//
// **印の `<` を unicode の書き方へ逃がさない。**Go の encoding/json は既定で
// `<` `>` `&` を `\u003c` のような形に書き換えるが、**GitHub の API はそのままの `<` を返す**
// （api.github.com の応答に `\u003c` が1つも無いことを数えて確かめた）。
// 逃がした形で配ると、本物と違う本文を検査することになる。
//
// notes: 並べる release（**新しい版から順に**。GitHub の並び順と同じ）。
// 戻り値: 一覧の JSON。
func releasesJSON(t *testing.T, notes []releaseNote) []byte {
	t.Helper()
	list := make([]releaseJSONEntry, 0, len(notes))
	for _, n := range notes {
		body := "## 直したこと\r\n\r\n- いくつか直しました\r\n"
		if len(n.Breaking) > 0 {
			body += "\r\n## 破壊的変更\r\n\r\n<!-- breaking:start -->\r\n"
			for _, b := range n.Breaking {
				body += "- " + b + "\r\n"
			}
			body += "<!-- breaking:end -->\r\n"
		}
		list = append(list, releaseJSONEntry{TagName: n.Tag, Name: n.Tag, Body: body})
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(list); err != nil {
		t.Fatalf("偽の release の一覧を作れません: %v", err)
	}
	return []byte(buf.String())
}

// SetReleases は release の一覧を差し替える。
//
// **サーバを立てたあとに呼べる。**破壊的変更の印がある状況と無い状況を作り分ける。
//
// body: 一覧の JSON。
func (fr *fakeRelease) SetReleases(body []byte) {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	fr.releases = body
}

// releasesBody は、いま配っている一覧を返す。
func (fr *fakeRelease) releasesBody() []byte {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	return fr.releases
}

// placeInstalled は、置き先に「いま入っているもの」を置く。
//
// **install.sh は置き換える前に、これへ `version` を訊く。**
//
// dir: 置き先。
// version: そのものが名乗る版（`dev` を渡せば、版を名乗らないものになる）。
func placeInstalled(t *testing.T, dir, version string) {
	t.Helper()
	script := "#!/bin/sh\necho " + version + "\n"
	if err := os.WriteFile(filepath.Join(dir, "continuo"), []byte(script), 0o755); err != nil {
		t.Fatalf("いま入っているものを置けません: %v", err)
	}
}

// Allow は、その版を配ることにする。
//
// tag: 配る版。
func (fr *fakeRelease) Allow(tag string) {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	fr.available[tag] = true
}

// SetAsset は配る書庫を差し替える。
//
// **サーバを立てたあとに呼べる。**チェックサムが合わない状況を作るのに使う。
//
// body: 新しい書庫の中身。
func (fr *fakeRelease) SetAsset(body []byte) {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	fr.asset = body
}

// assetBody は、いま配っている書庫を返す。
func (fr *fakeRelease) assetBody() []byte {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	return fr.asset
}

// serves は、その版を配っているかを返す。
//
// tag: 問い合わせる版。
// 戻り値: 配っていれば true。
func (fr *fakeRelease) serves(tag string) bool {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	return fr.available[tag]
}

// newFakeRelease は偽の release サーバを立てる。
//
// tag: 最新として返す版。
// body: 配る実行ファイルの中身（シェルスクリプトでよい）。
// withSums: 真なら checksums.txt を正しい値で配る。
// 戻り値: 立てたサーバ。t.Cleanup で閉じる。
func newFakeRelease(t *testing.T, tag, body string, withSums bool) *fakeRelease {
	t.Helper()
	asset := makeTarGz(t, "continuo", body)

	fr := &fakeRelease{Tag: tag, asset: asset, available: map[string]bool{tag: true}}
	if withSums {
		sum := sha256.Sum256(asset)
		fr.Checksums = fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), assetName())
	}

	mux := http.NewServeMux()
	// 最新の版を返す（install.sh は tag_name だけを読む）。
	mux.HandleFunc("/api/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name": %q, "name": "%s"}`, fr.Tag, fr.Tag)
	})
	// release の一覧を返す。**install.sh は `/latest` を落とした URL を引く。**
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		body := fr.releasesBody()
		if len(body) == 0 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		// パスは /dl/<版>/<ファイル名> である。**版を見て、配っていなければ 404 を返す。**
		rest := strings.TrimPrefix(r.URL.Path, "/dl/")
		ver, file, ok := strings.Cut(rest, "/")
		if !ok || !fr.serves(ver) {
			http.NotFound(w, r)
			return
		}
		switch file {
		case assetName():
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(fr.assetBody())
		case "checksums.txt":
			if fr.Checksums == "" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(fr.Checksums))
		default:
			http.NotFound(w, r)
		}
	})
	fr.Server = httptest.NewServer(mux)
	t.Cleanup(fr.Server.Close)
	return fr
}

// detachTerminal は、そのプロセスから制御端末を外す。
//
// **`cmd.Stdin = nil` では制御端末は外れない。**install.sh は標準入力ではなく
// `/dev/tty` を直接開くので、標準入力を塞いでも端末があれば読めてしまう
// （擬似端末を与えて実測したところ、`Stdin = nil` のまま入力を読めた）。
//
// **これを付けないと、端末から `go test` を走らせた Linux の開発者のところで、
// テストが人間の入力を待って止まる。**
//
// cmd: 端末を外すコマンド。
func detachTerminal(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// runInstaller は install.sh を走らせる。
//
// fr: 偽の release サーバ。nil なら取得先を差し替えない。
// dir: 置き先。
// args: install.sh に渡す引数。
// 戻り値: 終了コードと、標準出力・標準エラーを混ぜたもの。
func runInstaller(t *testing.T, fr *fakeRelease, dir string, args ...string) (int, string) {
	t.Helper()
	full := []string{scriptPath(t)}
	if fr != nil {
		// **取得先の差し替えは、環境変数ではなくフラグである。**
		// 環境変数にしていた版では、`curl … | CONTINUO_BASE_URL=http://… sh` の1行を
		// 貼らせるだけで偽の実行ファイルを置けた（安全性のレビューで実証された）。
		full = append(full,
			"--api-url", fr.Server.URL+"/api/latest",
			"--base-url", fr.Server.URL+"/dl",
		)
	}
	full = append(full, args...)
	cmd := exec.Command(installShell, full...)
	// **端末を与えない。**`curl … | sh` と同じく、対話できない状況を作る。
	cmd.Stdin = nil
	detachTerminal(cmd)
	cmd.Env = append(os.Environ(), "CONTINUO_INSTALL_DIR="+dir)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("install.sh を起動できません: %v", err)
	}
	return code, string(out)
}

// TestInstall_releaseから実行ファイルを取って置く は、基本の流れを確かめる。
//
// 目的: 最新の版を引き、書庫を落とし、展開して置き先へ配ること。
// 与える情報: 偽の release サーバ（checksums.txt つき）。
// 成功条件: 置き先に実行ファイルができ、実行できること。
// {"RUCM-PATH": "P003"}
func TestInstall_releaseから実行ファイルを取って置く(t *testing.T) {
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

// TestInstall_チェックサムが合わなければ置かない は、改竄の検知を確かめる。
//
// **取ってきたものが途中で入れ替わっていたら、置いてはならない。**
//
// 目的: checksums.txt と中身が食い違ったら、実行ファイルを置かずに落ちること。
// 与える情報: 誤ったチェックサムを配る偽サーバ。
// 成功条件: 終了コードが 0 でなく、置き先に何も無いこと。
// {"RUCM-PATH": "P004"}
func TestInstall_チェックサムが合わなければ置かない(t *testing.T) {
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

// TestInstall_照合できなければ既定では置かない は、照合できないときの扱いを確かめる。
//
// **checksums.txt を取れないとき、既定では止まる。**
// 以前は「照合せずに続けます」と警告して置いていたが、**取ってきたものが
// 入れ替わっていないかを確かめられないまま実行ファイルを置くことになる。**
//
// 目的: チェックサムを照合できないとき、実行ファイルを置かずに止まること。
// 与える情報: checksums.txt を配らない偽サーバ。
// 成功条件: 終了コードが 0 でなく、置き先に何も無く、承知のうえで続ける方法を示すこと。
// {"RUCM-PATH": "P005"}
func TestInstall_照合できなければ既定では置かない(t *testing.T) {
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

// TestInstall_照合を省く指定があれば置く は、明示的に許したときの扱いを確かめる。
//
// 目的: `--insecure-no-checksum` を渡したとき、照合せずに置くこと。
// 与える情報: checksums.txt を配らない偽サーバと、照合を省く指定。
// 成功条件: 実行ファイルが置かれ、照合できなかった理由を伝えること。
// {"RUCM-PATH": "P008"}
func TestInstall_照合を省く指定があれば置く(t *testing.T) {
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

// TestInstall_版に使えない文字があれば取得の前に弾く は、URL への混入を防ぐことを確かめる。
//
// **`--version` の値は URL のパスに入る。**`../` を含む値を通すと、curl が送信前に
// パスを正規化し、**別のリポジトリの release に到達できる**（安全性のレビューで、
// 本物の github.com が 200 を返すことが確かめられた）。
//
// 目的: 使えない文字を含む版を、配布サーバへ繋ぐ前に弾くこと。
// 与える情報: `../` や空白を含む版。
// 成功条件: どれも終了コードが 0 でなく、偽サーバへ1度も繋がないこと。
// {"RUCM-PATH": "P013"}
func TestInstall_版に使えない文字があれば取得の前に弾く(t *testing.T) {
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

// TestInstall_版を指定できる は、`--version` を確かめる。
//
// 目的: `--version` を渡したら、最新を引かずにその版を取ること。
// 与える情報: 最新として別の版を返す偽サーバ。
// 成功条件: 指定した版で取りに行くこと。
func TestInstall_版を指定できる(t *testing.T) {
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

// TestInstall_releaseが1つも無ければ作り方を案内する は、配布前の状態を確かめる。
//
// **タグを打つまで release は1つも無い。**そのとき利用者に見えるものが、
// 「404」ではなく「まだ配布していません。ソースから作れます」であること。
//
// 目的: release が無いとき、理由と代わりの手順を示して止まること。
// 与える情報: 空の応答を返す偽サーバ。
// 成功条件: ソースから作る手順が出ること。
// {"RUCM-PATH": "P010"}
func TestInstall_releaseが1つも無ければ作り方を案内する(t *testing.T) {
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

// TestInstall_端末が無ければ道具を1つも入れない は、無人の実行の安全を確かめる。
//
// **`curl … | sh` を無人で流されたとき、勝手に `sudo` を走らせてはならない。**
//
// 目的: 端末が無ければ、足りない道具を並べるだけで、1つも入れないこと。
// 与える情報: 標準入力を与えずに走らせる。
// 成功条件: 実行ファイルは置かれ、`sudo` を1度も呼ばないこと。
// {"RUCM-PATH": "P002"}
func TestInstall_端末が無ければ道具を1つも入れない(t *testing.T) {
	fr := newFakeRelease(t, "v1.2.3", "#!/bin/sh\necho ok\n", true)
	dir := t.TempDir()

	// **PATH を絞って、すべての道具を「無い」ことにする。**
	// それでも1つも入れずに終わることを確かめる。
	cmd := exec.Command(installShell, scriptPath(t),
		"--api-url", fr.Server.URL+"/api/latest",
		"--base-url", fr.Server.URL+"/dl")
	cmd.Stdin = nil
	detachTerminal(cmd)
	cmd.Env = []string{
		"HOME=" + t.TempDir(),
		// **`/usr/bin` だけを残す。**curl と tar は要る（無いと取得すらできない）。
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
	// **足りないものは並べるが、入れてはいない。**
	if !strings.Contains(text, "まだ足りないもの") {
		t.Errorf("足りない道具を並べていません:\n%s", text)
	}
	for _, forbidden := range []string{"を入れました"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("端末が無いのに道具を入れています（%q）:\n%s", forbidden, text)
		}
	}
}

// TestInstall_helpはパイプ経由でも出る は、`curl … | sh -s -- --help` を確かめる。
//
// **`$0` は "sh" になるので、自分自身を読み直す作りだと壊れる。**
// 実際、`sed -n '2,30p' "$0"` で書いたときは `sed: sh: No such file or directory` になった。
//
// 目的: 標準入力からスクリプトを流し込んでも、案内が出ること。
// 与える情報: install.sh の中身を標準入力から流す。
// 成功条件: 終了コードが 0 で、オプションの一覧が出ること。
// {"RUCM-PATH": "P014"}
func TestInstall_helpはパイプ経由でも出る(t *testing.T) {
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

// TestInstall_知らないオプションは弾く は、引数の検査を確かめる。
//
// 目的: 知らないオプションで、何もせずに落ちること。
// 与える情報: `--bogus`。
// 成功条件: 終了コードが 0 でなく、置き先に何も無いこと。
// {"RUCM-PATH": "P015"}
func TestInstall_知らないオプションは弾く(t *testing.T) {
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

// TestInstall_置き先を指定できる は、`--dir` を確かめる。
//
// 目的: `--dir` で置き先を変えられること。
// 与える情報: 既定と違うディレクトリ。
// 成功条件: 指定したところに実行ファイルができること。
func TestInstall_置き先を指定できる(t *testing.T) {
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

// TestInstall_指定した版の配布物が無ければ置かない は、取得の失敗を確かめる。
//
// **利用者が版を打ち間違えることがある。**そのとき 404 の生のメッセージではなく、
// 「どこを見れば配布しているものが分かるか」を示す。
//
// 目的: 書庫を取得できないとき、実行ファイルを置かずに止まること。
// 与える情報: その版の書庫を持たない偽サーバ。
// 成功条件: 終了コードが 0 でなく、置き先に何も無く、一時ディレクトリも残らないこと。
// {"RUCM-PATH": "P009"}
func TestInstall_指定した版の配布物が無ければ置かない(t *testing.T) {
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

// fakeUname は、指定した OS 名と命令セット名を返す偽の uname を PATH の先頭に置く。
//
// **install.sh は uname で環境を見分ける。**本物では Windows や未対応の CPU を試せないので、
// 偽の uname を先に見つけさせる。
//
// osName: `uname -s` が返す名前。
// machine: `uname -m` が返す名前。
// 戻り値: 偽の uname を置いたディレクトリ。
func fakeUname(t *testing.T, osName, machine string) string {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
  -s) echo %q ;;
  -m) echo %q ;;
  *)  echo %q ;;
esac
`, osName, machine, osName)
	p := filepath.Join(dir, "uname")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatalf("偽の uname を置けません: %v", err)
	}
	return dir
}

// runWithFakeUname は、偽の uname を先に見つけさせて install.sh を走らせる。
//
// 戻り値: 終了コードと、標準出力・標準エラーを混ぜたもの。
func runWithFakeUname(t *testing.T, osName, machine, dir string) (int, string) {
	t.Helper()
	fakeDir := fakeUname(t, osName, machine)
	cmd := exec.Command(installShell, scriptPath(t), "--no-deps")
	cmd.Stdin = nil
	detachTerminal(cmd)
	cmd.Env = []string{
		"HOME=" + t.TempDir(),
		"PATH=" + fakeDir + ":/usr/bin:/bin",
		"CONTINUO_INSTALL_DIR=" + dir,
	}
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("install.sh を起動できません: %v", err)
	}
	return code, string(out)
}

// TestInstall_WindowsではWSL2を案内して止まる は、対応しない OS の扱いを確かめる。
//
// **herdr の Windows 版が安定していないため、continuo は Windows ネイティブに対応しない**
// （設計 3-32b）。**黙って失敗させず、代わりに何を使えばよいかを示す。**
//
// 目的: Windows と見分けたら、何も置かずに WSL2 を案内すること。
// 与える情報: `uname -s` が MINGW64_NT を返す環境。
// 成功条件: 終了コードが 0 でなく、案内に WSL2 が入り、置き先に何も無いこと。
// {"RUCM-PATH": "P012"}
func TestInstall_WindowsではWSL2を案内して止まる(t *testing.T) {
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

// TestInstall_対応しない命令セットは対応表を出して止まる は、命令セットの検査を確かめる。
//
// 目的: 対応していない命令セットで、何も置かずに止まること。
// 与える情報: `uname -m` が i386 を返す環境。
// 成功条件: 終了コードが 0 でなく、対応している命令セットを示すこと。
// {"RUCM-PATH": "P011"}
func TestInstall_対応しない命令セットは対応表を出して止まる(t *testing.T) {
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

// TestInstall_照合できず端末も無ければ置くだけで終わる は、2つの欠落が重なる経路を確かめる。
//
// **無人で流したときに、checksums.txt の載せ忘れた配布物を掴むことがある。**
// そのとき continuo は、照合していないことを伝えたうえで置き、道具は1つも入れない。
//
// 目的: チェックサムを照合できず、端末も無いとき、実行ファイルだけを置いて終わること。
// 与える情報: checksums.txt を配らない偽サーバと、端末の無い実行。
// 成功条件: 実行ファイルが置かれ、照合していない注意が出て、道具を1つも入れないこと。
// {"RUCM-PATH": "P007"}
func TestInstall_照合できず端末も無ければ置くだけで終わる(t *testing.T) {
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

// TestInstall_破壊的変更のある版へ上げると名指しで警告する は、上げる前の警告を確かめる。
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
func TestInstall_破壊的変更のある版へ上げると名指しで警告する(t *testing.T) {
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

// TestInstall_破壊的変更が無ければ何も言わない は、余計なことを言わないことを確かめる。
//
// **毎回の更新で警告が出ると、本当に出たときに読まれなくなる。**
//
// 目的: 上げる範囲に印が1つも無ければ、破壊的変更について何も言わないこと。
// 与える情報: 印を1つも持たない release の一覧。
// 成功条件: 終了コードが 0 で、出力に「破壊的変更」が出ないこと。
func TestInstall_破壊的変更が無ければ何も言わない(t *testing.T) {
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

// TestInstall_新規の導入では破壊的変更を言わない は、警告する相手がいない場合を確かめる。
//
// **置き先に何も無ければ、上げるのではなく初めて入れるのである。**
// 直す設定はまだ無いので、言うことは何も無い。
//
// 目的: 置き先に実行ファイルが無いとき、印があっても何も言わないこと。
// 与える情報: 印を持つ release の一覧と、空の置き先。
// 成功条件: 終了コードが 0 で、出力に「破壊的変更」が出ないこと。
func TestInstall_新規の導入では破壊的変更を言わない(t *testing.T) {
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

// TestInstall_版を名乗らないものからは警告しない は、ソースから作ったものの扱いを確かめる。
//
// **`go build` しただけの実行ファイルは `dev` と名乗る**（internal/cli の version）。
// **どの release より新しいのか古いのかを決められないので、範囲を作れない。**
//
// 目的: いま入っているものが `dev` と名乗るとき、誤った警告を出さないこと。
// 与える情報: `dev` と名乗る実行ファイルと、印を持つ release の一覧。
// 成功条件: 終了コードが 0 で、出力に「破壊的変更」が出ないこと。
func TestInstall_版を名乗らないものからは警告しない(t *testing.T) {
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
