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

// TestInstall_端末が無ければ道具を1つも入れない は、無人の実行の安全を確かめる。
//
// **`curl … | sh` を無人で流されたとき、勝手に `sudo` を走らせてはならない。**
//
// 目的: 端末が無ければ、足りない道具を並べるだけで、1つも入れないこと。
// 与える情報: 標準入力を与えずに走らせる。
// 成功条件: 実行ファイルは置かれ、`sudo` を1度も呼ばないこと。
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

// stubBrokenBreakingAwk は、印を読み出す awk だけが落ちる PATH を作る。
//
// **「awk が無い」ではなく「足した awk のプログラムがその処理系で通らない」を模す。**
// install.sh は checksums.txt の照合にも awk を使うので、awk を丸ごと落とすと
// **確かめたい経路より前で止まってしまう。**だから `breaking:start` を含むプログラムだけを落とす。
//
// t: 呼び出し元のテスト。
// 戻り値: 偽の awk を置いたディレクトリ。PATH の先頭へ足して使う。
func stubBrokenBreakingAwk(t *testing.T) string {
	t.Helper()
	real, err := exec.LookPath("awk")
	if err != nil {
		t.Skipf("awk がありません: %v", err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do\n" +
		"\tcase \"$a\" in\n" +
		"\t\t*breaking:start*) exit 2 ;;\n" +
		"\tesac\n" +
		"done\n" +
		"exec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "awk"), []byte(script), 0o755); err != nil {
		t.Fatalf("偽の awk を置けません: %v", err)
	}
	return dir
}
