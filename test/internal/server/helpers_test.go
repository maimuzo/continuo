// Package server_test は internal/server の振る舞いを公開 API を通して検証する
// （テストファイルは test/ 配下に internal/ と同じ構造で置く）。
package server_test

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/server"
)

// fakeSource は実行中の run の写しを返すだけの供給元である。
//
// **orchestrator を起動せずにダッシュボードだけを検証するために使う。**
// ダッシュボードが受け取ってよいのは写しだけであり、実物の内部状態には触れない
// （設計 3-25）。この型が `server.RunSource` を満たすこと自体が、その証拠になる。
type fakeSource struct {
	// views は RunViews が返す写しである。
	views []orchestrator.RunView
	// gates は GateViews が返す写しである（issue #134）。
	gates []orchestrator.GateView
	// newWork は NewWorkStatus が返す写しである（issue #173）。
	newWork orchestrator.NewWorkView
	// calls は RunViews が呼ばれた回数である。
	calls int
}

// RunViews は写しを返す。
//
// 戻り値: 設定しておいた run の写し。
func (f *fakeSource) RunViews() []orchestrator.RunView {
	f.calls++
	return f.views
}

// GateViews は着手の関門で止めた issue の写しを返す（issue #134）。
//
// 戻り値: 設定しておいた写し。
func (f *fakeSource) GateViews() []orchestrator.GateView {
	return f.gates
}

// NewWorkStatus は「新しい issue を取らない」状態の写しを返す（issue #173）。
//
// 戻り値: 設定しておいた写し。
func (f *fakeSource) NewWorkStatus() orchestrator.NewWorkView {
	return f.newWork
}

// testTime はテストで使う固定の時刻である（経過の表示を決定的にするため）。
var testTime = time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)

// newTestServer はテスト用のダッシュボードを組み立てる。
//
// **listen はしない。**`Handler` を通して `httptest` から叩く。
//
// t: テストの制御。
// views: 供給する run の写し。
// 戻り値の1つ目: 組み立てたダッシュボード。
// 戻り値の2つ目: 供給元（呼ばれた回数の確認に使う）。
func newTestServer(t *testing.T, views []orchestrator.RunView) (*server.Server, *fakeSource) {
	t.Helper()
	return newTestServerWithGates(t, views, nil)
}

// newTestServerWithGates は、着手の関門で止めた issue の写しも渡してダッシュボードを組み立てる
// （issue #134）。
//
// t: テストの制御。
// views: 供給する run の写し。
// gates: 供給する「着手できずに止まっているもの」の写し。
// 戻り値の1つ目: 組み立てたダッシュボード。
// 戻り値の2つ目: 供給元（呼ばれた回数の確認に使う）。
func newTestServerWithGates(
	t *testing.T, views []orchestrator.RunView, gates []orchestrator.GateView,
) (*server.Server, *fakeSource) {
	t.Helper()
	src := &fakeSource{views: views, gates: gates}
	port := 0
	s, err := server.New(server.Options{
		Port:   &port,
		Source: src,
		Logger: slog.New(slog.DiscardHandler),
		Now:    func() time.Time { return testTime },
	})
	if err != nil {
		t.Fatalf("ダッシュボードを組み立てられなかった: %v", err)
	}
	if s == nil {
		t.Fatal("ポートを指定したのにダッシュボードが組み立てられなかった")
	}
	return s, src
}

// get は Handler へリクエストを1件通し、状態コードと本文を返す。
//
// **宛先（`Host`）はループバックにする。**ダッシュボードはそれ以外の宛先を断るので、
// 断られること自体を確かめたい場合は getWithHost を使う。
//
// t: テストの制御。
// s: 対象のダッシュボード。
// method: HTTP のメソッド。
// path: 要求するパス。
// 戻り値の1つ目: 状態コード。
// 戻り値の2つ目: 本文。
func get(t *testing.T, s *server.Server, method, path string) (int, string) {
	t.Helper()
	return getWithHost(t, s, method, path, server.LoopbackHost)
}

// getWithHost は宛先（`Host`）を指定して Handler へリクエストを1件通す。
//
// t: テストの制御。
// s: 対象のダッシュボード。
// method: HTTP のメソッド。
// path: 要求するパス。
// host: `Host` ヘッダに入れる値。
// 戻り値の1つ目: 状態コード。
// 戻り値の2つ目: 本文。
func getWithHost(t *testing.T, s *server.Server, method, path, host string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	res := rec.Result()
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("本文を読めなかった: %v", err)
	}
	return res.StatusCode, string(body)
}

// httpGet は実際に listen しているダッシュボードへ HTTP で取りに行く。
//
// t: テストの制御。
// url: 取得先の URL。
// 戻り値の1つ目: 状態コード。
// 戻り値の2つ目: 本文。
func httpGet(t *testing.T, url string) (int, string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Get(url)
	if err != nil {
		t.Fatalf("%s を取得できなかった: %v", url, err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("本文を読めなかった: %v", err)
	}
	return res.StatusCode, string(body)
}

// sampleViews は検証に使う run の写しを2件返す。
//
// 戻り値: run の写し。
func sampleViews() []orchestrator.RunView {
	return []orchestrator.RunView{
		{
			Identifier:   "octocat/hello-world#12",
			Title:        "ダッシュボードを作る",
			URL:          "https://github.com/octocat/hello-world/issues/12",
			State:        "In Progress",
			TurnCount:    3,
			LastHookAt:   testTime.Add(-90 * time.Second),
			StallClockAt: testTime.Add(-1 * time.Second),
			StartedAt:    testTime.Add(-10 * time.Minute),
			Tokens: orchestrator.TokenUsage{
				APICalls: 19, Input: 38, CacheCreation: 14358, CacheRead: 701185, Output: 1216,
			},
			TokensAt: testTime.Add(-80 * time.Second),
		},
		{
			// **こちらが先に並ぶ**（identifier の昇順）。
			Identifier:   "octocat/hello-world#7",
			Title:        "hook の受け口を直す",
			URL:          "https://github.com/octocat/hello-world/issues/7",
			State:        "In Progress",
			TurnCount:    11,
			RetryCount:   1,
			WaitingQuota: true,
			LastHookAt:   testTime.Add(-2 * time.Second),
			StallClockAt: testTime.Add(-2 * time.Second),
			Tokens: orchestrator.TokenUsage{
				APICalls: 1, Input: 2, CacheCreation: 3, CacheRead: 4, Output: 5,
			},
			TokensAt: testTime.Add(-1 * time.Second),
		},
	}
}

// writeTranscript は検証用の transcript（JSONL）を書き出す。
//
// **本物の transcript と同じ形にする。**トークンの重複排除が効いていることを、
// `orchestrator.ReadTranscript` を通して確かめるために使う。
//
// t: テストの制御。
// lines: 1行1件の JSON。
// 戻り値: 書き出したファイルの絶対パス。
func writeTranscript(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("transcript を書き出せなかった: %v", err)
	}
	return path
}

// nonLoopbackIPv4 はこの機材の、ループバックでない IPv4 アドレスを返す。
//
// **待ち受けがループバックに限られていることを確かめるために使う。**
// 1つも無い環境（隔離されたコンテナなど）では空を返し、その検証は行われない。
//
// t: テストの制御。
// 戻り値: ループバックでない IPv4 アドレスの文字列。
func nonLoopbackIPv4(t *testing.T) []string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Logf("ネットワークインタフェースを列挙できなかったので、この検証は飛ばす: %v", err)
		return nil
	}
	var out []string
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if v4 := ipnet.IP.To4(); v4 != nil {
			out = append(out, v4.String())
		}
	}
	return out
}
