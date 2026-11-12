//go:build unix

// ネットワークインストーラー（install.sh）の検査である（設計 3-36）。
//
// **利用者が continuo に対して最初に叩くのが install.sh である。**
// ここが壊れていると、continuo が正しくても誰も使い始められない。
//
// **偽の release サーバを立てて、実際に `sh install.sh` を走らせる。**
// GitHub に release を作る前に、取得・照合・展開・配置が通ることを確かめられる。
// 取得先は `--base-url` と `--feed-url` で差し替える（**テスト専用のフラグである**）。
package install_test

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
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
	"time"
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
//
// **本物の github.com と同じ形で答える。**
//   - `/feed` は release の更新フィード（releases.atom）を返す
//   - `/dl/<版>/<ファイル>` は、本物の `releases/download` と同じく 302 で `/assets/<版>/<ファイル>` へ転送する
//   - `/assets/` は書庫と checksums.txt を返す。**`Range` に 206 で答える**（本物は 206。2026-11-12 に実測）
type fakeRelease struct {
	// Server は立てた HTTP サーバである。
	Server *httptest.Server
	// Tag は最新として返す版である。
	Tag string
	// asset は配る書庫の中身である。**mu が守る**（サーバを立てたあとに差し替えるテストがある）。
	asset []byte
	// Checksums は checksums.txt の中身である。空なら 404 を返す。
	Checksums string
	// mu は以下の欄を守る。
	//
	// **handler は別の goroutine で走る。**テスト本体が配る版を足すのと同時に読むので、
	// 排他が無いと `-race` が競合を報告する（実測で2件出た）。
	mu sync.Mutex
	// available は実際に配っている版である。
	//
	// **これを持たないと、どの版を要求しても同じ書庫を返してしまう。**
	// 「指定した版が無い」を試せなくなる（実際、最初の版では試せていなかった）。
	// **配っていない版は、release の無いタグと同じに見える**（書庫が 404 になる）。
	available map[string]bool
	// feed は release の更新フィード（releases.atom）として返す本文である。**mu が守る。**
	//
	// **空なら、`Tag` を公開から 200 時間前の release 1件だけ並べた更新フィードを返す。**
	// install.sh は版を指定しない実行で、この更新フィードから入れる版を選ぶ（クールダウン。設計 3-36）。
	// 200 時間は既定のクールダウン（168 時間）から十分に離してある。
	feed []byte
	// feedFail が真なら、更新フィードに 404 を返す。**mu が守る。**リポジトリが無い状況を作る。
	feedFail bool
	// feedStatus が 0 でなければ、更新フィードにその状態コードを返す。**mu が守る。**
	// GitHub の不調（5xx）や中継の拒否（403）の状況を作る。
	feedStatus int
	// archiveStatus が 0 でなければ、書庫にその状態コードを返す。**mu が守る。**
	// 書庫の在りかの確かめ（1バイトの GET）が 206・200・404 のどれでもない状況を作る。
	archiveStatus int
	// feedHits は更新フィードが要求された回数である。
	feedHits atomic.Int64
	// redirects は `/dl/` が転送（302）を返した回数である。
	redirects atomic.Int64
	// rangeHits は書庫に `Range` 付きで要求された回数である。
	rangeHits atomic.Int64
}

// releaseNote は、偽の release サーバが返す release 1件である。
type releaseNote struct {
	// Tag はその release の版（タグ）である。
	//
	// **試験版（prerelease）は、タグに `-` を付けて表す**（`v1.0.0-rc1`）。更新フィードには
	// prerelease かどうかが載らないので、install.sh は `-` を含むタグを試験版とみなす。
	// **draft は更新フィードに載らない**（持ち主にしか見えない）ので、並べないことで表す。
	Tag string
	// Title は release の名前（更新フィードの `<title>`）である。空なら Tag と同じにする。
	//
	// **install.sh は題名を読まない。**release の名前はタグと違う名前を付けられるためである。
	Title string
	// Breaking は破壊的変更の説明である。空なら本文に見出し「破壊的変更」を置かない。
	Breaking []string
	// HTML が空でなければ、本文の HTML をこれに差し替える（見出しや箇条書きの形を崩した本文を作る）。
	HTML string
	// Age は最終更新から経った時間である。0 なら 200 時間（既定のクールダウンを越える）とする。
	//
	// **時刻は、テストを走らせた今からの差で作る。**固定の時刻を書くと、
	// 日が経つにつれて既定の 168 時間の境界を越え、テストが黙って意味を失う。
	// 境界からは1時間以上離して使う（秒の進みで揺れないように）。
	Age time.Duration
	// TagOnly が真なら、release の無いタグとして載せる（cooldownRelease は、その版の書庫を配らない）。
	//
	// **本物の更新フィードには、release の無いタグも同じ形で載る**（fork の更新フィードには、
	// 元のリポジトリのタグが release の無いまま載る。2026-11-12 に実測）。
	TagOnly bool
}

// xmlEscaper は、GitHub の更新フィードと同じ逃がし方で、XML の側の文字を逃がす。
//
// **Go の標準（`encoding/xml` の `&#34;` や `&#xA;`）を使わない。**本物は `&quot;` と `&#39;` を使い、
// 改行は生のまま置く（2026-11-12 に取った releases.atom で確かめた）。違う形で配ると、
// 本物と違う更新フィードを検査することになる。
var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")

// htmlEscaper は、本文の HTML の中の文字を逃がす（GitHub がリリースノートを HTML へ変換したときと同じ）。
//
// **本文の中の `<` は2段に逃がされる。**ここで `&lt;` にし、XML の側でもう一度逃がして `&amp;lt;` にする。
var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

// noteHTML は、release 1件の本文の HTML を作る。
//
// **見出しと `<ul>` のあいだに空の行を1行置く。**本物の6版とも、そうなっている（2026-11-12 に実測）。
// 破壊的変更の箇条書きのあとに段落を置く（本物の v0.1.15・v0.1.14・v0.1.13 に在る。読まれないこと）。
//
// n: release 1件。
// 戻り値: 本文の HTML（まだ XML の側では逃がしていない）。
func noteHTML(n releaseNote) string {
	if n.HTML != "" {
		return n.HTML
	}
	if n.TagOnly {
		// release の無いタグの件の本文は、commit の文の段落だけである。
		return "<p>いくつか直しました</p>"
	}
	var b strings.Builder
	b.WriteString("<h2>直したこと</h2>\n<ul>\n<li>いくつか直しました</li>\n</ul>")
	if len(n.Breaking) > 0 {
		b.WriteString("\n<h2>破壊的変更</h2>\n\n<ul>\n")
		for _, s := range n.Breaking {
			b.WriteString("<li>" + htmlEscaper.Replace(s) + "</li>\n")
		}
		b.WriteString("</ul>\n<p>上げる前に、動いている continuo を止めてください。</p>")
	}
	return b.String()
}

// releasesFeed は、GitHub の release の更新フィード（releases.atom）と同じ形の XML を作る。
//
// notes: 並べる release（**新しい版から順に**。GitHub の並び順と同じ）。
// 戻り値: 更新フィードの XML。
func releasesFeed(t *testing.T, notes []releaseNote) []byte {
	t.Helper()
	return buildReleasesFeed(notes, false)
}

// buildReleasesFeed は releasesFeed の本体である。
//
// **サーバの handler からも呼ぶので、testing.T を受け取らない。**
//
// **本物と同じ並びにする。**最初の `<entry>` より前に、フィード全体の `<link>` と `<updated>` を置く
// （install.sh がこれを1件目と取り違えないことを確かめる）。`<entry>` の中は
// `<id>`→`<updated>`→`<link>`→`<title>`→`<content>`→`<author>`→`<media:thumbnail>` の順である。
//
// notes: 並べる release（新しい版から順に）。
// oneLine: 真なら、改行を1つも置かずに1行にまとめる。
// 戻り値: 更新フィードの XML。
func buildReleasesFeed(notes []releaseNote, oneLine bool) []byte {
	now := time.Now().UTC()
	nl := "\n"
	ind := func(n int) string { return strings.Repeat(" ", n) }
	if oneLine {
		nl = ""
		ind = func(int) string { return "" }
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + nl)
	b.WriteString(`<feed xmlns="http://www.w3.org/2005/Atom" xmlns:media="http://search.yahoo.com/mrss/" xml:lang="en-US">` + nl)
	b.WriteString(ind(2) + `<id>tag:github.com,2008:https://github.com/octocat/continuo/releases</id>` + nl)
	b.WriteString(ind(2) + `<link type="text/html" rel="alternate" href="https://github.com/octocat/continuo/releases"/>` + nl)
	b.WriteString(ind(2) + `<link type="application/atom+xml" rel="self" href="https://github.com/octocat/continuo/releases.atom"/>` + nl)
	b.WriteString(ind(2) + `<title>Release notes from continuo</title>` + nl)
	b.WriteString(ind(2) + `<updated>` + now.Format("2006-01-02T15:04:05Z") + `</updated>` + nl)
	for _, n := range notes {
		age := n.Age
		if age == 0 {
			age = 200 * time.Hour
		}
		title := n.Title
		if title == "" {
			title = n.Tag
			if n.TagOnly {
				title = n.Tag + ": いくつか直しました"
			}
		}
		body := noteHTML(n)
		if oneLine {
			body = strings.ReplaceAll(body, "\n", "")
		}
		b.WriteString(ind(2) + `<entry>` + nl)
		b.WriteString(ind(4) + `<id>tag:github.com,2008:Repository/1/` + xmlEscaper.Replace(n.Tag) + `</id>` + nl)
		b.WriteString(ind(4) + `<updated>` + now.Add(-age).Format("2006-01-02T15:04:05Z") + `</updated>` + nl)
		b.WriteString(ind(4) + `<link rel="alternate" type="text/html" href="https://github.com/octocat/continuo/releases/tag/` + xmlEscaper.Replace(n.Tag) + `"/>` + nl)
		b.WriteString(ind(4) + `<title>` + xmlEscaper.Replace(title) + `</title>` + nl)
		b.WriteString(ind(4) + `<content type="html">` + xmlEscaper.Replace(body) + `</content>` + nl)
		b.WriteString(ind(4) + `<author>` + nl + ind(6) + `<name>octocat</name>` + nl + ind(4) + `</author>` + nl)
		b.WriteString(ind(4) + `<media:thumbnail height="30" width="30" url="https://avatars.githubusercontent.com/u/1?s=60&amp;v=4"/>` + nl)
		b.WriteString(ind(2) + `</entry>` + nl)
	}
	b.WriteString(`</feed>` + nl)
	return []byte(b.String())
}

// SetFeed は更新フィードの本文を差し替える。
//
// **サーバを立てたあとに呼べる。**破壊的変更がある状況と無い状況を作り分ける。
//
// body: 更新フィードの本文（更新フィードでない本文も渡せる）。
func (fr *fakeRelease) SetFeed(body []byte) {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	fr.feed = body
}

// FailFeed は、更新フィードに 404 を返すようにする。
//
// **リポジトリが無い状況を作る。**版を指定しない実行は止まり、`--version` の実行は止まらない
// （設計 3-36）。
func (fr *fakeRelease) FailFeed() {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	fr.feedFail = true
}

// SetFeedStatus は、更新フィードにその状態コードを返すようにする。
//
// status: 返す状態コード（例: 503）。
func (fr *fakeRelease) SetFeedStatus(status int) {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	fr.feedStatus = status
}

// SetArchiveStatus は、書庫にその状態コードを返すようにする。
//
// status: 返す状態コード（例: 503）。
func (fr *fakeRelease) SetArchiveStatus(status int) {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	fr.archiveStatus = status
}

// statusOverrides は、更新フィードと書庫に返す状態コードの差し替えを返す。0 なら差し替えていない。
func (fr *fakeRelease) statusOverrides() (feed, archive int) {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	return fr.feedStatus, fr.archiveStatus
}

// feedBody は、いま配っている更新フィードを返す。
//
// 戻り値: 更新フィードの本文。nil なら 404 を返す。
func (fr *fakeRelease) feedBody() []byte {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	if fr.feedFail {
		return nil
	}
	if len(fr.feed) == 0 {
		return buildReleasesFeed([]releaseNote{{Tag: fr.Tag}}, false)
	}
	return fr.feed
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
	// **API の口（`/api`）は置かない。**install.sh は認証なしの GitHub の API を呼ばず、
	// 更新フィードから版を選ぶ（設計 3-36）。置くと、それに頼る実装を見逃す。
	mux.HandleFunc("/feed", func(w http.ResponseWriter, r *http.Request) {
		fr.feedHits.Add(1)
		if code, _ := fr.statusOverrides(); code != 0 {
			http.Error(w, "unavailable", code)
			return
		}
		body := fr.feedBody()
		if len(body) == 0 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
		_, _ = w.Write(body)
	})
	// **本物の `releases/download` は、配布用のホストへ 302 で転送する**（2026-11-12 に実測）。
	// 転送を追わない実装を見逃さないよう、偽サーバも必ず転送する。
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		fr.redirects.Add(1)
		http.Redirect(w, r, "/assets/"+strings.TrimPrefix(r.URL.Path, "/dl/"), http.StatusFound)
	})
	mux.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		// パスは /assets/<版>/<ファイル名> である。**版を見て、配っていなければ 404 を返す。**
		rest := strings.TrimPrefix(r.URL.Path, "/assets/")
		ver, file, ok := strings.Cut(rest, "/")
		if !ok || !fr.serves(ver) {
			http.NotFound(w, r)
			return
		}
		switch file {
		case assetName():
			if _, code := fr.statusOverrides(); code != 0 {
				http.Error(w, "unavailable", code)
				return
			}
			if r.Header.Get("Range") != "" {
				fr.rangeHits.Add(1)
			}
			// **`Range` に 206 で答える**（本物と同じ）。http.ServeContent が `Range` を解釈する。
			w.Header().Set("Content-Type", "application/gzip")
			http.ServeContent(w, r, file, time.Time{}, bytes.NewReader(fr.assetBody()))
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

// sourceArgs は、取得先を偽サーバへ差し替える引数を返す。
//
// **取得先の差し替えは、環境変数ではなくフラグである。**
// 環境変数にしていた版では、`curl … | CONTINUO_BASE_URL=http://… sh` の1行を
// 貼らせるだけで偽の実行ファイルを置けた（安全性のレビューで実証された）。
//
// fr: 偽の release サーバ。
// 戻り値: `--feed-url` と `--base-url` の4つ。
func sourceArgs(fr *fakeRelease) []string {
	return []string{
		"--feed-url", fr.Server.URL + "/feed",
		"--base-url", fr.Server.URL + "/dl",
	}
}

// runInstaller は install.sh を走らせる。
//
// fr: 偽の release サーバ。nil なら取得先を差し替えない。
// dir: 置き先。
// args: install.sh に渡す引数。
// 戻り値: 終了コードと、標準出力・標準エラーを混ぜたもの。
func runInstaller(t *testing.T, fr *fakeRelease, dir string, args ...string) (int, string) {
	t.Helper()
	return runInstallerEnv(t, fr, dir, nil, args...)
}

// runInstallerEnv は、環境変数を足して install.sh を走らせる。
//
// fr: 偽の release サーバ。nil なら取得先を差し替えない。
// dir: 置き先。
// env: 足す環境変数（`NAME=値`。後ろの重複が勝つので、PATH なども上書きできる）。
// args: install.sh に渡す引数。
// 戻り値: 終了コードと、標準出力・標準エラーを混ぜたもの。
func runInstallerEnv(t *testing.T, fr *fakeRelease, dir string, env []string, args ...string) (int, string) {
	t.Helper()
	full := []string{scriptPath(t)}
	if fr != nil {
		full = append(full, sourceArgs(fr)...)
	}
	full = append(full, args...)
	cmd := exec.Command(installShell, full...)
	// **端末を与えない。**`curl … | sh` と同じく、対話できない状況を作る。
	cmd.Stdin = nil
	detachTerminal(cmd)
	// **後ろの重複が勝つ**（os/exec が env を後勝ちで畳む）。
	cmd.Env = append(append(os.Environ(), "CONTINUO_INSTALL_DIR="+dir), env...)
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
	cmd := exec.Command(installShell, append([]string{scriptPath(t)}, sourceArgs(fr)...)...)
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

// TestInstall_認証なしのAPIを呼ぶ行が無い は、install.sh が api.github.com を呼ばないことを確かめる。
//
// **認証なしの GitHub の API は、接続元のアドレスごとに1時間60回までである。**アドレスを大勢で共有する
// ネットワークでは、インストーラーを動かす前から尽きていて、そこで止まる（issue の報告）。
// install.sh は release の更新フィードから版を選び、API を1回も呼ばない（設計 3-36）。
// 偽サーバのテストは取得先を差し替えるので、既定の取得先に API が残っても見つからない。だから行を数える。
//
// 目的: install.sh のコメント以外の行に `api.github.com` が無いこと。
// 与える情報: install.sh の中身。
// 成功条件: `#` で始まる行（コメント）を除いて、`api.github.com` を含む行が0件であること。
func TestInstall_認証なしのAPIを呼ぶ行が無い(t *testing.T) {
	body, err := os.ReadFile(scriptPath(t))
	if err != nil {
		t.Fatalf("install.sh を読めません: %v", err)
	}
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.Contains(line, "api.github.com") {
			t.Errorf("%d 行目が認証なしの GitHub の API を指しています: %s", n, line)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("install.sh を読み切れません: %v", err)
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

// stubBrokenBreakingAwk は、破壊的変更を読み出す awk だけが落ちる PATH を作る。
//
// **「awk が無い」ではなく「足した awk のプログラムがその処理系で通らない」を模す。**
// install.sh は版を選ぶときと checksums.txt の照合にも awk を使うので、awk を丸ごと落とすと
// **確かめたい経路より前で止まってしまう。**だから、破壊的変更を読み出すプログラムだけが持つ
// 印 `continuo-breaking-reader` を含むものだけを落とす（install.sh の breaking_lines）。
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
		"\t\t*continuo-breaking-reader*) exit 2 ;;\n" +
		"\tesac\n" +
		"done\n" +
		"exec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "awk"), []byte(script), 0o755); err != nil {
		t.Fatalf("偽の awk を置けません: %v", err)
	}
	return dir
}
