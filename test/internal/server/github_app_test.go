package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/githubapp"
	"github.com/maimuzo/continuo/internal/server"
)

// fakeGitHub は、GitHub App を作る導線（docs/plans/impl/issue245_github_app_attribution.md の 3-82g）が
// 叩く GitHub の3本の経路だけを持つ偽の GitHub である。
//
// **本物の GitHub を叩かない。**`GitHubAppOptions.Endpoints` の `Web` と `API` を両方この URL に向ける。
//
//	POST /app-manifests/{code}/conversions  … manifest の流れの `code` を資格情報へ変換する（段1）
//	POST /login/oauth/access_token          … 認可の `code` をトークンへ交換する（段3）
//	GET  /user                              … アクセストークンの持ち主のログイン名（3-82f）
type fakeGitHub struct {
	srv *httptest.Server
	// login は `GET /user` が返すログイン名である。
	login string

	mu sync.Mutex
	// convertCalls / exchangeCalls / viewerCalls は各経路が叩かれた回数である。
	convertCalls, exchangeCalls, viewerCalls int
	// lastExchangeForm は最後の `POST /login/oauth/access_token` の本文である。
	lastExchangeForm url.Values
	// lastAuthorization は最後の `GET /user` の Authorization ヘッダである。
	lastAuthorization string
}

// 偽の GitHub が返す値である。**`pem` と `webhook_secret` は、ファイルのどこにも現れてはならない。**
const (
	fakeClientID      = "Iv23li-test-client-id"
	fakeClientSecret  = "test-client-secret-do-not-store-elsewhere"
	fakeSlug          = "continuo-octocat"
	fakeAppName       = "continuo-octocat"
	fakePEM           = "-----BEGIN RSA PRIVATE KEY-----\nFAKE-PRIVATE-KEY\n-----END RSA PRIVATE KEY-----\n"
	fakeWebhookSecret = "fake-webhook-secret"
	fakeAccessToken   = "ghu_fake_access_token"
	fakeRefreshToken  = "ghr_fake_refresh_token"
	// fakeRefreshTokenExpiresIn は実測値（3-82b。15638399秒＝約181日）である。
	fakeRefreshTokenExpiresIn = 15638399
)

// newFakeGitHub は偽の GitHub を立てる。テストの終わりに閉じる。
//
// t: テストの制御。
// login: `GET /user` が返すログイン名。
// 戻り値: 偽の GitHub。
func newFakeGitHub(t *testing.T, login string) *fakeGitHub {
	t.Helper()
	fg := &fakeGitHub{login: login}
	fg.srv = httptest.NewServer(http.HandlerFunc(fg.handle))
	t.Cleanup(fg.srv.Close)
	return fg
}

// handle は3本の経路を捌く。それ以外は 404。
func (fg *fakeGitHub) handle(w http.ResponseWriter, r *http.Request) {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/app-manifests/") && strings.HasSuffix(r.URL.Path, "/conversions"):
		fg.convertCalls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":             1,
			"client_id":      fakeClientID,
			"client_secret":  fakeClientSecret,
			"slug":           fakeSlug,
			"name":           fakeAppName,
			"pem":            fakePEM,
			"webhook_secret": fakeWebhookSecret,
			"html_url":       fg.srv.URL + "/settings/apps/" + fakeSlug,
			"permissions":    map[string]string{"issues": "write", "metadata": "read"},
			"events":         []string{},
		})
	case r.Method == http.MethodPost && r.URL.Path == "/login/oauth/access_token":
		fg.exchangeCalls++
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fg.lastExchangeForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":             fakeAccessToken,
			"expires_in":               28800,
			"refresh_token":            fakeRefreshToken,
			"refresh_token_expires_in": fakeRefreshTokenExpiresIn,
			"token_type":               "bearer",
		})
	case r.Method == http.MethodGet && r.URL.Path == "/user":
		fg.viewerCalls++
		fg.lastAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"login": fg.login})
	default:
		http.NotFound(w, r)
	}
}

// counts は叩かれた回数を返す。
func (fg *fakeGitHub) counts() (convert, exchange, viewer int) {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	return fg.convertCalls, fg.exchangeCalls, fg.viewerCalls
}

// githubAppFixture は GitHub App の経路を検証するための組み立てである。
type githubAppFixture struct {
	s  *server.Server
	gh *fakeGitHub
	// store は一時ディレクトリの下の資格情報の置き場所である。**本物の `~/.continuo/` は触らない。**
	store githubapp.Store
	// now はダッシュボードの時計である。**書き換えると `state` の期限を進められる。**
	now time.Time
	mu  sync.Mutex
}

// clock はダッシュボードへ渡す現在時刻の関数である。
func (f *githubAppFixture) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// advance は時計を進める。
func (f *githubAppFixture) advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

// newGitHubAppFixture は GitHub App の経路を張ったダッシュボードを組み立てる。
//
// **listen はしない**（`Handler` を通して叩く）。listen が要る検証は自分で `Start` する。
//
// t: テストの制御。
// gh: 偽の GitHub。
// ghLogin: `gh api user` の代わりの関数。nil なら `octocat` を返す。
// 戻り値: 組み立て。
func newGitHubAppFixture(t *testing.T, gh *fakeGitHub, ghLogin func(context.Context) (string, error)) *githubAppFixture {
	t.Helper()
	if ghLogin == nil {
		ghLogin = func(context.Context) (string, error) { return "octocat", nil }
	}
	f := &githubAppFixture{gh: gh, store: githubapp.NewStore(t.TempDir()), now: testTime}
	port := 0
	s, err := server.New(server.Options{
		Port:   &port,
		Source: &fakeSource{views: sampleViews(), tokenCallOrder: -1},
		Logger: slog.New(slog.DiscardHandler),
		Now:    f.clock,
		GitHubApp: &server.GitHubAppOptions{
			Store:      f.store,
			HTTPClient: gh.srv.Client(),
			Endpoints:  githubapp.Endpoints{Web: gh.srv.URL, API: gh.srv.URL},
			GHLogin:    ghLogin,
			ProjectURL: "https://github.com/octocat/hello-world",
		},
	})
	if err != nil {
		t.Fatalf("ダッシュボードを組み立てられなかった: %v", err)
	}
	f.s = s
	return f
}

// getFull は Handler へリクエストを1件通し、状態コード・本文・ヘッダを返す。
func getFull(t *testing.T, s *server.Server, path string) (int, string, http.Header) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = server.LoopbackHost
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	res := rec.Result()
	defer func() { _ = res.Body.Close() }()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := res.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return res.StatusCode, sb.String(), res.Header
}

// manifestStatePattern は段1 の form の送り先から `state` を拾う。
var manifestStatePattern = regexp.MustCompile(`settings/apps/new\?state=([A-Za-z0-9_-]+)`)

// authorizeStatePattern は段3 の認可のリンクから `state` を拾う。
var authorizeStatePattern = regexp.MustCompile(`login/oauth/authorize\?[^"]*state=([A-Za-z0-9_-]+)`)

// manifestValuePattern は段1 の hidden の入力から manifest の JSON（HTML エスケープ済み）を拾う。
var manifestValuePattern = regexp.MustCompile(`name="manifest" value="([^"]*)"`)

// extractState は本文から `state` を拾う。
func extractState(t *testing.T, body string, pattern *regexp.Regexp) string {
	t.Helper()
	m := pattern.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("本文から state を拾えなかった（%s）:\n%s", pattern, body)
	}
	return m[1]
}

// extractManifest は段1 の本文から manifest を JSON として読み出す。
func extractManifest(t *testing.T, body string) map[string]any {
	t.Helper()
	m := manifestValuePattern.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("本文に manifest の hidden の入力が無い:\n%s", body)
	}
	raw := html.UnescapeString(m[1])
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("manifest を JSON として読めない: %v\n%s", err, raw)
	}
	return out
}

// readCredentialsFile は資格情報のファイルを生の JSON として読む（`githubapp.Store.Read` を通さない。
// **ファイルに何が書かれているかそのもの**を見るため）。
func readCredentialsFile(t *testing.T, store githubapp.Store) (map[string]any, string, os.FileMode) {
	t.Helper()
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatalf("資格情報のファイルを stat できない: %v", err)
	}
	b, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("資格情報のファイルを読めない: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("資格情報のファイルを JSON として読めない: %v\n%s", err, b)
	}
	return out, string(b), info.Mode().Perm()
}

// writeCredentials は資格情報を直接書く（前提の状態を作るため）。
func writeCredentials(t *testing.T, store githubapp.Store, c githubapp.Credentials) {
	t.Helper()
	if err := store.Write(c); err != nil {
		t.Fatalf("資格情報を書けない: %v", err)
	}
}

// 目的: `Options.GitHubApp` が nil なら `/github-app` の経路が1本も無いことを確認する
// （3-82g。既にあるダッシュボードのテストは渡さないので変わらない）。
// 与える情報: GitHubApp を渡さずに組み立てたダッシュボード。
// 成功条件: 5本とも 404 で、既存の `/` と `/api/v1/state` は 200 のままであること。
func TestGitHubApp_口が無ければ経路を張らない(t *testing.T) {
	s, _ := newTestServer(t, sampleViews())
	for _, p := range []string{
		server.GitHubAppPath, server.GitHubAppCreatedPath, server.GitHubAppInstalledPath,
		server.GitHubAppAuthorizePath, server.GitHubAppAuthorizedPath,
	} {
		if code, _ := get(t, s, http.MethodGet, p); code != http.StatusNotFound {
			t.Errorf("GitHubApp が nil なのに %s が %d を返した（404 のはず）", p, code)
		}
	}
	for _, p := range []string{"/", server.APIStatePath} {
		if code, _ := get(t, s, http.MethodGet, p); code != http.StatusOK {
			t.Errorf("既存の経路 %s が %d を返した", p, code)
		}
	}
}

// 目的: 資格情報が無いとき段1（作る）が出て、manifest が設計どおりに組まれることを確認する
// （3-82g「manifest に何を書くか」）。
// 与える情報: 資格情報の無い一時ディレクトリと、実際に listen したダッシュボード。
// 成功条件: 本文に `settings/apps/new?state=` の form と `manifest` の hidden の入力があり、
// manifest の `request_oauth_on_install` が false・`public` が false・`default_permissions` が
// 2つ・`hook_attributes` が無い・戻り先3つに **実際のポート** が入っていること。
// CSP が `form-action 'self' <GitHub>` になり、既存の `/` は `'none'` のままであること。
func TestGitHubApp_資格情報が無ければ段1を出しmanifestを組む(t *testing.T) {
	gh := newFakeGitHub(t, "octocat")
	f := newGitHubAppFixture(t, gh, nil)
	if err := f.s.Start(); err != nil {
		t.Fatalf("待ち受けを開始できなかった: %v", err)
	}
	defer func() { _ = f.s.Close(context.Background()) }()
	base := "http://" + f.s.Addr()

	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Get(base + server.GitHubAppPath)
	if err != nil {
		t.Fatalf("段1 を取得できなかった: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, rerr := res.Body.Read(buf)
		sb.Write(buf[:n])
		if rerr != nil {
			break
		}
	}
	body := sb.String()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("段1 の状態コードが違う: got %d\n%s", res.StatusCode, body)
	}
	if !strings.Contains(body, "段1/3") {
		t.Errorf("段1 の見出しが無い:\n%s", body)
	}
	if !manifestStatePattern.MatchString(body) {
		t.Errorf("manifest を POST する form の送り先に state が無い:\n%s", body)
	}
	if !strings.Contains(body, `method="post"`) {
		t.Errorf("manifest の form が POST ではない:\n%s", body)
	}
	if strings.Contains(body, "<script") {
		t.Errorf("script が混ざっている:\n%s", body)
	}

	m := extractManifest(t, body)
	if v, ok := m["request_oauth_on_install"]; !ok || v != false {
		t.Errorf("request_oauth_on_install が false ではない: %v（ok=%v）", v, ok)
	}
	if v, ok := m["public"]; !ok || v != false {
		t.Errorf("public が false ではない: %v（ok=%v）", v, ok)
	}
	if _, ok := m["hook_attributes"]; ok {
		t.Errorf("hook_attributes を書いている（webhook は使わない）: %v", m["hook_attributes"])
	}
	perms, _ := m["default_permissions"].(map[string]any)
	if len(perms) != 2 || perms["issues"] != "write" || perms["metadata"] != "read" {
		t.Errorf("default_permissions が違う: %v", m["default_permissions"])
	}
	if m["name"] != "continuo-octocat" {
		t.Errorf("name が `continuo-<ログイン名>` ではない: %v", m["name"])
	}
	if m["url"] != "https://github.com/octocat/hello-world" {
		t.Errorf("url が ProjectURL ではない: %v", m["url"])
	}
	if m["redirect_url"] != base+server.GitHubAppCreatedPath {
		t.Errorf("redirect_url に実際のポートが入っていない: %v（want %s）", m["redirect_url"], base+server.GitHubAppCreatedPath)
	}
	if m["setup_url"] != base+server.GitHubAppInstalledPath {
		t.Errorf("setup_url に実際のポートが入っていない: %v", m["setup_url"])
	}
	cb, _ := m["callback_urls"].([]any)
	if len(cb) != 1 || cb[0] != base+server.GitHubAppAuthorizedPath {
		t.Errorf("callback_urls が違う: %v", m["callback_urls"])
	}

	csp := res.Header.Get("Content-Security-Policy")
	wantForm := "form-action 'self' " + gh.srv.URL
	if !strings.Contains(csp, wantForm) {
		t.Errorf("段1 の CSP が form を許していない: %q（want %q を含む）", csp, wantForm)
	}
	for _, must := range []string{"default-src 'none'", "frame-ancestors 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, must) {
			t.Errorf("段1 の CSP から %q が落ちている: %q", must, csp)
		}
	}
	// 既存の `/` は緩めない。
	_, _, h := getFull(t, f.s, "/")
	if got := h.Get("Content-Security-Policy"); !strings.Contains(got, "form-action 'none'") {
		t.Errorf("`/` の CSP まで緩んでいる: %q", got)
	}
}

// 目的: listen していないとき（テストが `Handler` を直に叩くとき）、戻り先が `r.Host` から組まれることを
// 確認する（`Server.baseURL` の GoDoc）。
// 与える情報: listen せずに Handler を叩く。Host は `127.0.0.1`。
// 成功条件: manifest の redirect_url が `http://127.0.0.1/github-app/created` になること。
func TestGitHubApp_listenしていなければ戻り先はHostから組む(t *testing.T) {
	gh := newFakeGitHub(t, "octocat")
	f := newGitHubAppFixture(t, gh, nil)
	code, body, _ := getFull(t, f.s, server.GitHubAppPath)
	if code != http.StatusOK {
		t.Fatalf("段1 の状態コードが違う: got %d\n%s", code, body)
	}
	m := extractManifest(t, body)
	if want := "http://" + server.LoopbackHost + server.GitHubAppCreatedPath; m["redirect_url"] != want {
		t.Errorf("redirect_url が Host から組まれていない: %v（want %s）", m["redirect_url"], want)
	}
}

// 目的: 作成の戻り（`/github-app/created`）で資格情報が 0600 で書かれ、秘密鍵と webhook の secret が
// ファイルのどこにも無いことを確認する（3-82b「置くもの」・3-82g 段1）。
// 与える情報: 段1 を出して得た `state` と、偽の GitHub が返す変換の結果（`pem` と `webhook_secret` を含む）。
// 成功条件: ファイルが 0600 で、`client_id` / `client_secret` / `slug` が入り、`refresh_token` が無く、
// `pem` と `webhook_secret` の値が1文字も無いこと。段2 の画面に install のリンクが出ること。
func TestGitHubApp_作成の戻りで資格情報を0600で書き秘密鍵は捨てる(t *testing.T) {
	gh := newFakeGitHub(t, "octocat")
	f := newGitHubAppFixture(t, gh, nil)
	_, body, _ := getFull(t, f.s, server.GitHubAppPath)
	state := extractState(t, body, manifestStatePattern)

	code, body, _ := getFull(t, f.s, server.GitHubAppCreatedPath+"?code=abc123&state="+state)
	if code != http.StatusOK {
		t.Fatalf("作成の戻りの状態コードが違う: got %d\n%s", code, body)
	}
	if !strings.Contains(body, "段2/3") {
		t.Errorf("段2 の見出しが無い:\n%s", body)
	}
	if !strings.Contains(body, "apps/"+fakeSlug+"/installations/new") {
		t.Errorf("install のリンクが無い:\n%s", body)
	}
	if !strings.Contains(body, `href="`+server.GitHubAppAuthorizePath+`"`) {
		t.Errorf("「install 済みなら認可へ」のリンクが無い:\n%s", body)
	}

	fields, raw, perm := readCredentialsFile(t, f.store)
	if perm != 0o600 {
		t.Errorf("資格情報の権限が 0600 ではない: %o", perm)
	}
	if fields["client_id"] != fakeClientID || fields["client_secret"] != fakeClientSecret || fields["slug"] != fakeSlug {
		t.Errorf("client_id / client_secret / slug が違う: %v", fields)
	}
	if _, ok := fields["refresh_token"]; ok {
		t.Errorf("認可前なのに refresh_token がある: %v", fields)
	}
	if strings.Contains(raw, "PRIVATE KEY") || strings.Contains(raw, fakeWebhookSecret) {
		t.Errorf("秘密鍵か webhook の secret がファイルに書かれている:\n%s", raw)
	}
	if strings.Contains(raw, "pem") {
		t.Errorf("pem の欄がファイルにある:\n%s", raw)
	}
	if c, _, _ := gh.counts(); c != 1 {
		t.Errorf("変換の経路を %d 回叩いた（1回のはず）", c)
	}
}

// 目的: `state` が合わない作成の戻りを、資格情報を1バイトも書かずに断ることを確認する
// （3-82g「`state` を必ず突き合わせる」。攻める側の `code` で資格情報を上書きされる穴を塞ぐ）。
// 与える情報: 段1 を出したあと、違う `state` を付けた `/github-app/created`。
// 成功条件: 400 で「この要求は受け付けられません」が出て、ファイルが無いままで、偽の GitHub が
// 1度も叩かれないこと。CSP は緩めた版のままであること。
func TestGitHubApp_stateが合わなければ何も書かずに断る(t *testing.T) {
	gh := newFakeGitHub(t, "octocat")
	f := newGitHubAppFixture(t, gh, nil)
	_, _, _ = getFull(t, f.s, server.GitHubAppPath)

	code, body, h := getFull(t, f.s, server.GitHubAppCreatedPath+"?code=abc123&state=attacker-supplied-state")
	if code != http.StatusBadRequest {
		t.Fatalf("状態コードが違う: got %d, want %d\n%s", code, http.StatusBadRequest, body)
	}
	if !strings.Contains(body, "この要求は受け付けられません") {
		t.Errorf("断りの文言が無い:\n%s", body)
	}
	if _, err := os.Stat(f.store.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("state が合わないのに資格情報のファイルが作られた（err=%v）", err)
	}
	if c, e, v := gh.counts(); c+e+v != 0 {
		t.Errorf("state が合わないのに GitHub を叩いた: convert=%d exchange=%d viewer=%d", c, e, v)
	}
	if got := h.Get("Content-Security-Policy"); !strings.Contains(got, "form-action 'self'") {
		t.Errorf("断った応答の CSP が緩めた版ではない: %q", got)
	}
}

// 目的: 同じ `state` を2回使えないことを確認する（3-82g「1回使ったら捨てる」）。
// 与える情報: 段1 の `state` で作成の戻りを2回叩く。
// 成功条件: 2回目が 400 で、変換の経路は1回しか叩かれないこと。
func TestGitHubApp_同じstateは2回使えない(t *testing.T) {
	gh := newFakeGitHub(t, "octocat")
	f := newGitHubAppFixture(t, gh, nil)
	_, body, _ := getFull(t, f.s, server.GitHubAppPath)
	state := extractState(t, body, manifestStatePattern)

	if code, _, _ := getFull(t, f.s, server.GitHubAppCreatedPath+"?code=abc123&state="+state); code != http.StatusOK {
		t.Fatalf("1回目の状態コードが違う: got %d", code)
	}
	code, body, _ := getFull(t, f.s, server.GitHubAppCreatedPath+"?code=abc123&state="+state)
	if code != http.StatusBadRequest {
		t.Fatalf("2回目を受け付けた: got %d\n%s", code, body)
	}
	if c, _, _ := gh.counts(); c != 1 {
		t.Errorf("変換の経路を %d 回叩いた（1回のはず）", c)
	}
}

// 目的: install の戻り（`/github-app/installed?installation_id=…`）が段3（認可）を出し、認可のリンクに
// `client_id` と `state` が入ることを確認する（3-82g。`installation_id` は読み捨てる）。
// 与える情報: `client_id` / `client_secret` / `slug` だけの資格情報。
// 成功条件: 200 で、`login/oauth/authorize?` のリンクに `client_id=<値>`・`redirect_uri`・`state` があり、
// 段3 の説明（attribution の1行・6か月・途中で止めたら）が出ること。
func TestGitHubApp_installの戻りで段3を出す(t *testing.T) {
	gh := newFakeGitHub(t, "octocat")
	f := newGitHubAppFixture(t, gh, nil)
	writeCredentials(t, f.store, githubapp.Credentials{ClientID: fakeClientID, ClientSecret: fakeClientSecret, Slug: fakeSlug})

	code, body, _ := getFull(t, f.s, server.GitHubAppInstalledPath+"?installation_id=1&setup_action=install")
	if code != http.StatusOK {
		t.Fatalf("状態コードが違う: got %d\n%s", code, body)
	}
	if !strings.Contains(body, "段3/3") {
		t.Errorf("段3 の見出しが無い:\n%s", body)
	}
	link := regexp.MustCompile(`href="([^"]*login/oauth/authorize\?[^"]*)"`).FindStringSubmatch(body)
	if link == nil {
		t.Fatalf("認可のリンクが無い:\n%s", body)
	}
	u, err := url.Parse(html.UnescapeString(link[1]))
	if err != nil {
		t.Fatalf("認可のリンクを URL として読めない: %v", err)
	}
	q := u.Query()
	if q.Get("client_id") != fakeClientID {
		t.Errorf("認可のリンクの client_id が違う: %q", q.Get("client_id"))
	}
	if q.Get("state") == "" {
		t.Errorf("認可のリンクに state が無い: %s", link[1])
	}
	if want := "http://" + server.LoopbackHost + server.GitHubAppAuthorizedPath; q.Get("redirect_uri") != want {
		t.Errorf("認可のリンクの redirect_uri が違う: %q（want %q）", q.Get("redirect_uri"), want)
	}
	for _, must := range []string{"– with " + fakeSlug, "約6か月", "途中で continuo を止めたら"} {
		if !strings.Contains(body, must) {
			t.Errorf("段3 の説明 %q が無い:\n%s", must, body)
		}
	}
}

// 目的: 認可の戻り（`/github-app/authorized`）で更新用のトークンと期限と認可したアカウント名が書かれ、
// 段4 にアカウント名が出ることを確認する（3-82b「認可を通した直後」・3-82f）。
// 与える情報: 段3 を出して得た `state` と、偽の GitHub が返すトークン（期限は実測値 15638399 秒）。
// 成功条件: ファイルに `refresh_token` / `refresh_token_expires_at`（RFC 3339。now＋15638399秒）/
// `authorized_login` が入り、`client_id` が残ること。交換の本文に `code` が入り、`GET /user` が
// そのアクセストークンで叩かれること。段4 にアカウント名と「設定が終わりました」が出ること。
func TestGitHubApp_認可の戻りで更新用のトークンと認可したアカウント名を書く(t *testing.T) {
	gh := newFakeGitHub(t, "octocat")
	f := newGitHubAppFixture(t, gh, nil)
	writeCredentials(t, f.store, githubapp.Credentials{ClientID: fakeClientID, ClientSecret: fakeClientSecret, Slug: fakeSlug})
	_, body, _ := getFull(t, f.s, server.GitHubAppAuthorizePath)
	state := extractState(t, body, authorizeStatePattern)

	code, body, _ := getFull(t, f.s, server.GitHubAppAuthorizedPath+"?code=xyz789&state="+state)
	if code != http.StatusOK {
		t.Fatalf("状態コードが違う: got %d\n%s", code, body)
	}
	if !strings.Contains(body, "設定が終わりました") || !strings.Contains(body, "octocat") {
		t.Errorf("段4 にアカウント名が出ていない:\n%s", body)
	}
	if strings.Contains(body, "認可だけをやり直す") {
		t.Errorf("gh と同じアカウントなのに「認可だけをやり直す」が出ている:\n%s", body)
	}

	fields, raw, perm := readCredentialsFile(t, f.store)
	if perm != 0o600 {
		t.Errorf("資格情報の権限が 0600 ではない: %o", perm)
	}
	if fields["refresh_token"] != fakeRefreshToken {
		t.Errorf("refresh_token が違う: %v", fields["refresh_token"])
	}
	wantExpiry := testTime.Add(fakeRefreshTokenExpiresIn * time.Second).UTC().Format(time.RFC3339)
	if fields["refresh_token_expires_at"] != wantExpiry {
		t.Errorf("refresh_token_expires_at が違う: %v（want %s）", fields["refresh_token_expires_at"], wantExpiry)
	}
	if fields["authorized_login"] != "octocat" {
		t.Errorf("authorized_login が違う: %v", fields["authorized_login"])
	}
	if fields["client_id"] != fakeClientID || fields["client_secret"] != fakeClientSecret {
		t.Errorf("認可の書き込みで client_id / client_secret が消えた: %v", fields)
	}
	if strings.Contains(raw, fakeAccessToken) {
		t.Errorf("アクセストークンがファイルに書かれている:\n%s", raw)
	}

	gh.mu.Lock()
	form, auth := gh.lastExchangeForm, gh.lastAuthorization
	gh.mu.Unlock()
	if form.Get("code") != "xyz789" || form.Get("client_id") != fakeClientID || form.Get("client_secret") != fakeClientSecret {
		t.Errorf("交換の本文が違う: %v", form)
	}
	if auth != "Bearer "+fakeAccessToken {
		t.Errorf("GET /user のアクセストークンが違う: %q", auth)
	}
}

// 目的: 認可したアカウントが `gh api user` と違うとき、両方の名前と「認可だけをやり直す」のリンクを出し、
// **それでも資格情報は書く**ことを確認する（3-82f「認可が終わった直後」。`code` は使い捨てなので、
// 書かないと認可が丸ごと失われる）。
// 与える情報: `gh api user` が `someone-else` を返す関数。
// 成功条件: 段4 に `someone-else` と `octocat` の両方と `/github-app/authorize` のリンクが出て、
// ファイルの `authorized_login` が `octocat` であること。
func TestGitHubApp_認可したアカウントがghと違えば両方を並べて資格情報は書く(t *testing.T) {
	gh := newFakeGitHub(t, "octocat")
	f := newGitHubAppFixture(t, gh, func(context.Context) (string, error) { return "someone-else", nil })
	writeCredentials(t, f.store, githubapp.Credentials{ClientID: fakeClientID, ClientSecret: fakeClientSecret, Slug: fakeSlug})
	_, body, _ := getFull(t, f.s, server.GitHubAppAuthorizePath)
	state := extractState(t, body, authorizeStatePattern)

	code, body, _ := getFull(t, f.s, server.GitHubAppAuthorizedPath+"?code=xyz789&state="+state)
	if code != http.StatusOK {
		t.Fatalf("状態コードが違う: got %d\n%s", code, body)
	}
	for _, must := range []string{"someone-else", "octocat", `href="` + server.GitHubAppAuthorizePath + `"`, "認可だけをやり直す"} {
		if !strings.Contains(body, must) {
			t.Errorf("段4 に %q が無い:\n%s", must, body)
		}
	}
	fields, _, _ := readCredentialsFile(t, f.store)
	if fields["authorized_login"] != "octocat" || fields["refresh_token"] != fakeRefreshToken {
		t.Errorf("違っていても資格情報は書くはずなのに、書かれていない: %v", fields)
	}
}

// 目的: 全部揃った資格情報で `/github-app` を開くと「設定済み」と「認可だけをやり直す」のリンクが出ることを
// 確認する（3-82g「順序」の表）。
// 与える情報: 期限内の更新用のトークンを持つ資格情報。
// 成功条件: 200 で「設定済み」と `/github-app/authorize` へのリンクが出て、段1 の form が無いこと。
func TestGitHubApp_全部あれば設定済みと再認可のリンクを出す(t *testing.T) {
	gh := newFakeGitHub(t, "octocat")
	f := newGitHubAppFixture(t, gh, nil)
	writeCredentials(t, f.store, githubapp.Credentials{
		ClientID: fakeClientID, ClientSecret: fakeClientSecret, Slug: fakeSlug,
		RefreshToken: fakeRefreshToken, RefreshTokenExpiresAt: testTime.Add(100 * 24 * time.Hour),
		AuthorizedLogin: "octocat",
	})
	code, body, _ := getFull(t, f.s, server.GitHubAppPath)
	if code != http.StatusOK {
		t.Fatalf("状態コードが違う: got %d\n%s", code, body)
	}
	if !strings.Contains(body, "設定済み") || !strings.Contains(body, `href="`+server.GitHubAppAuthorizePath+`"`) {
		t.Errorf("設定済みの画面になっていない:\n%s", body)
	}
	if manifestStatePattern.MatchString(body) {
		t.Errorf("設定済みなのに段1 の form が出ている:\n%s", body)
	}
}

// 目的: 更新用のトークンが切れている資格情報で `/github-app` を開くと段3（認可）が出ることを確認する
// （3-82g「順序」の表）。
// 与える情報: 期限を過ぎた更新用のトークンを持つ資格情報。
// 成功条件: 200 で段3 の見出しと期限切れの1行が出ること。
func TestGitHubApp_更新用のトークンが切れていれば段3を出す(t *testing.T) {
	gh := newFakeGitHub(t, "octocat")
	f := newGitHubAppFixture(t, gh, nil)
	writeCredentials(t, f.store, githubapp.Credentials{
		ClientID: fakeClientID, ClientSecret: fakeClientSecret, Slug: fakeSlug,
		RefreshToken: fakeRefreshToken, RefreshTokenExpiresAt: testTime.Add(-time.Hour),
		AuthorizedLogin: "octocat",
	})
	code, body, _ := getFull(t, f.s, server.GitHubAppPath)
	if code != http.StatusOK {
		t.Fatalf("状態コードが違う: got %d\n%s", code, body)
	}
	if !strings.Contains(body, "段3/3") || !strings.Contains(body, "切れています") {
		t.Errorf("段3 と期限切れの1行が出ていない:\n%s", body)
	}
	if !authorizeStatePattern.MatchString(body) {
		t.Errorf("認可のリンクが無い:\n%s", body)
	}
}

// 目的: `client_id` はあるが更新用のトークンが無い資格情報で `/github-app` を開くと段2（install）が出ることを
// 確認する（作ったあと install を押さずに離脱した人がここに来る。3-82g「順序」の表）。
// 与える情報: `client_id` / `client_secret` / `slug` だけの資格情報。
// 成功条件: 200 で段2 の見出し・`apps/<slug>/installations/new` のリンク・「install 済みなら認可へ」のリンクが出ること。
func TestGitHubApp_更新用のトークンが無ければ段2を出す(t *testing.T) {
	gh := newFakeGitHub(t, "octocat")
	f := newGitHubAppFixture(t, gh, nil)
	writeCredentials(t, f.store, githubapp.Credentials{ClientID: fakeClientID, ClientSecret: fakeClientSecret, Slug: fakeSlug})
	code, body, _ := getFull(t, f.s, server.GitHubAppPath)
	if code != http.StatusOK {
		t.Fatalf("状態コードが違う: got %d\n%s", code, body)
	}
	for _, must := range []string{"段2/3", "apps/" + fakeSlug + "/installations/new", `href="` + server.GitHubAppAuthorizePath + `"`} {
		if !strings.Contains(body, must) {
			t.Errorf("段2 に %q が無い:\n%s", must, body)
		}
	}
}

// 目的: `?name=` で段1 の名前を入れ直せることを確認する（名前が取られていたときの入れ直し。3-82c）。
// 与える情報: `/github-app?name=foo`。
// 成功条件: 名前の欄の値と manifest の `name` が `foo` になること。名前の form は GET で `/github-app` へ送ること。
func TestGitHubApp_名前を入れ直せる(t *testing.T) {
	gh := newFakeGitHub(t, "octocat")
	f := newGitHubAppFixture(t, gh, nil)
	code, body, _ := getFull(t, f.s, server.GitHubAppPath+"?name=foo")
	if code != http.StatusOK {
		t.Fatalf("状態コードが違う: got %d\n%s", code, body)
	}
	if !strings.Contains(body, `name="name" value="foo"`) {
		t.Errorf("名前の欄に foo が入っていない:\n%s", body)
	}
	if m := extractManifest(t, body); m["name"] != "foo" {
		t.Errorf("manifest の name が foo ではない: %v", m["name"])
	}
	if !strings.Contains(body, `method="get" action="`+server.GitHubAppPath+`"`) {
		t.Errorf("名前の form が GET で /github-app へ送る形になっていない:\n%s", body)
	}
}

// 目的: `state` が30分で期限切れになることを確認する（3-82g「30分で期限切れにする」。sudo mode の
// 再認証が挟まっても10分は超えうるので、短くしない）。
// 与える情報: 段1 を出したあと、時計を進めてから作成の戻りを叩く。
// 成功条件: 29分後は受け付け、31分後は 400 で断り、GitHub を叩かないこと。
func TestGitHubApp_stateは30分で期限切れになる(t *testing.T) {
	t.Run("29分後は受け付ける", func(t *testing.T) {
		gh := newFakeGitHub(t, "octocat")
		f := newGitHubAppFixture(t, gh, nil)
		_, body, _ := getFull(t, f.s, server.GitHubAppPath)
		state := extractState(t, body, manifestStatePattern)
		f.advance(29 * time.Minute)
		if code, body, _ := getFull(t, f.s, server.GitHubAppCreatedPath+"?code=abc123&state="+state); code != http.StatusOK {
			t.Fatalf("29分後に断られた: got %d\n%s", code, body)
		}
	})
	t.Run("31分後は断る", func(t *testing.T) {
		gh := newFakeGitHub(t, "octocat")
		f := newGitHubAppFixture(t, gh, nil)
		_, body, _ := getFull(t, f.s, server.GitHubAppPath)
		state := extractState(t, body, manifestStatePattern)
		f.advance(31 * time.Minute)
		code, body, _ := getFull(t, f.s, server.GitHubAppCreatedPath+"?code=abc123&state="+state)
		if code != http.StatusBadRequest {
			t.Fatalf("31分後に受け付けた: got %d\n%s", code, body)
		}
		if c, _, _ := gh.counts(); c != 0 {
			t.Errorf("期限切れなのに GitHub を叩いた: %d 回", c)
		}
		if _, err := os.Stat(f.store.Path()); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("期限切れなのに資格情報のファイルが作られた（err=%v）", err)
		}
	})
}

// 目的: 段1 用と段3 用の `state` が別々に持たれることを確認する（3-82g「どこに持つか」。段1 の待ちの最中に
// 段3 の画面が描かれても、段1 の `state` は消えない）。
// 与える情報: 段1 を出したあとに段3 を出し、段1 の `state` で作成の戻りを叩く。
// 成功条件: 段1 の `state` がまだ有効で、作成の戻りが 200 になること。段3 の `state` を作成の戻りに
// 使っても通らないこと。
func TestGitHubApp_段1と段3のstateは別々に持つ(t *testing.T) {
	gh := newFakeGitHub(t, "octocat")
	f := newGitHubAppFixture(t, gh, nil)
	_, body, _ := getFull(t, f.s, server.GitHubAppPath)
	createState := extractState(t, body, manifestStatePattern)

	// 段3 を出すには client_id が要るので、直接書く（段1 の state を作ったあとに）。
	writeCredentials(t, f.store, githubapp.Credentials{ClientID: fakeClientID, ClientSecret: fakeClientSecret, Slug: fakeSlug})
	_, body, _ = getFull(t, f.s, server.GitHubAppAuthorizePath)
	authorizeState := extractState(t, body, authorizeStatePattern)

	if code, body, _ := getFull(t, f.s, server.GitHubAppCreatedPath+"?code=abc123&state="+authorizeState); code != http.StatusBadRequest {
		t.Errorf("段3 の state で作成の戻りが通った: got %d\n%s", code, body)
	}
	if code, body, _ := getFull(t, f.s, server.GitHubAppCreatedPath+"?code=abc123&state="+createState); code != http.StatusOK {
		t.Errorf("段3 を出したら段1 の state が消えた: got %d\n%s", code, body)
	}
}

// 目的: GitHub が変換を断ったとき、資格情報を書かずに理由を出すことを確認する。
// 与える情報: `/app-manifests` に 404 を返す偽の GitHub（経路を持たない httptest.Server）。
// 成功条件: 502 で、ファイルが無いままであること。
func TestGitHubApp_変換に失敗したら何も書かない(t *testing.T) {
	broken := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(broken.Close)
	gh := &fakeGitHub{srv: broken, login: "octocat"}
	f := newGitHubAppFixture(t, gh, nil)
	_, body, _ := getFull(t, f.s, server.GitHubAppPath)
	state := extractState(t, body, manifestStatePattern)

	code, body, _ := getFull(t, f.s, server.GitHubAppCreatedPath+"?code=abc123&state="+state)
	if code != http.StatusBadGateway {
		t.Fatalf("状態コードが違う: got %d, want %d\n%s", code, http.StatusBadGateway, body)
	}
	if _, err := os.Stat(f.store.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("変換に失敗したのに資格情報のファイルが作られた（err=%v）", err)
	}
}

// 目的: 資格情報が無いのに認可の入口を開いたら、先に作るよう案内することを確認する
// （認可のリンクは `client_id` 無しには組めない）。
// 与える情報: 資格情報の無い一時ディレクトリで `/github-app/authorize` と `/github-app/installed`。
// 成功条件: 400 で、`/github-app` へのリンクが出ること。
func TestGitHubApp_資格情報が無ければ認可の入口は先に作るよう案内する(t *testing.T) {
	gh := newFakeGitHub(t, "octocat")
	f := newGitHubAppFixture(t, gh, nil)
	for _, p := range []string{server.GitHubAppAuthorizePath, server.GitHubAppInstalledPath + "?installation_id=1"} {
		code, body, _ := getFull(t, f.s, p)
		if code != http.StatusBadRequest {
			t.Errorf("%s の状態コードが違う: got %d\n%s", p, code, body)
		}
		if !strings.Contains(body, "先に GitHub App を作ってください") || !strings.Contains(body, `href="`+server.GitHubAppPath+`"`) {
			t.Errorf("%s が段1 へ案内していない:\n%s", p, body)
		}
	}
}

// 目的: GitHub App の経路も GET しか受けないことを確認する（3-82g「経路は5本だけ増え、全部 GET である」）。
// 与える情報: 5本の経路への POST。
// 成功条件: 全部 405 で、資格情報のファイルが作られないこと。
func TestGitHubApp_POSTは受け付けない(t *testing.T) {
	gh := newFakeGitHub(t, "octocat")
	f := newGitHubAppFixture(t, gh, nil)
	for _, p := range []string{
		server.GitHubAppPath, server.GitHubAppCreatedPath, server.GitHubAppInstalledPath,
		server.GitHubAppAuthorizePath, server.GitHubAppAuthorizedPath,
	} {
		if code, _ := get(t, f.s, http.MethodPost, p); code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s が %d を返した（405 のはず）", p, code)
		}
	}
	if _, err := os.Stat(filepath.Join(f.store.Dir(), githubapp.CredentialsFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("POST で資格情報のファイルが作られた（err=%v）", err)
	}
}
