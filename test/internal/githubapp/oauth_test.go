package githubapp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/githubapp"
)

// fakeGitHub は github.com と api.github.com の代わりである。
//
// **本物を叩かない。**トークンの交換（`/login/oauth/access_token`）・`GET /user`・
// manifest の変換（`/app-manifests/<code>/conversions`）を持つ。
type fakeGitHub struct {
	srv *httptest.Server
	mu  sync.Mutex
	// tokenForms は `/login/oauth/access_token` へ届いた form である（届いた順）。
	tokenForms []url.Values
	// tokenResponses は `/login/oauth/access_token` が返す本文である（届いた順に消費する。
	// 尽きたら最後のものを繰り返す）。
	tokenResponses []string
	// tokenStatus は `/login/oauth/access_token` のステータス。0 なら 200。
	tokenStatus int
	// userAuth は `GET /user` へ届いた Authorization ヘッダである。
	userAuth []string
	// conversionCodes は manifest の変換へ届いた code である。
	conversionCodes []string
}

// newFakeGitHub は偽の GitHub を立てる。
func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.tokenForms = append(f.tokenForms, r.PostForm)
		var body string
		if len(f.tokenResponses) > 0 {
			body = f.tokenResponses[0]
			if len(f.tokenResponses) > 1 {
				f.tokenResponses = f.tokenResponses[1:]
			}
		}
		status := f.tokenStatus
		f.mu.Unlock()
		if r.Header.Get("Accept") != "application/json" {
			// **JSON を求めていないと GitHub は form の形で返す。**そこを写す。
			http.Error(w, "Accept: application/json が無い", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.userAuth = append(f.userAuth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"octocat","id":1}`))
	})
	mux.HandleFunc("POST /app-manifests/{code}/conversions", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.conversionCodes = append(f.conversionCodes, r.PathValue("code"))
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1,"slug":"continuo-octocat","name":"continuo-octocat",` +
			`"client_id":"Iv23liexample","client_secret":"secret-example",` +
			`"webhook_secret":"hook-secret","pem":"-----BEGIN RSA PRIVATE KEY-----\nAAAA\n-----END RSA PRIVATE KEY-----\n",` +
			`"html_url":"https://github.com/apps/continuo-octocat"}`))
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// client は偽の GitHub へ向けた Client を返す。
func (f *fakeGitHub) client() githubapp.Client {
	return githubapp.Client{
		HTTP:      f.srv.Client(),
		Endpoints: githubapp.Endpoints{Web: f.srv.URL, API: f.srv.URL},
	}
}

// rotatedResponse は回転が通ったときの GitHub の応答である（実測の値。設計 7-7）。
const rotatedResponse = `{"access_token":"ghu_new","expires_in":28800,"refresh_token":"ghr_new",` +
	`"refresh_token_expires_in":15638399,"token_type":"bearer","scope":""}`

// 目的: 回転が、設計 1-2 のとおりの form を送り、新しい更新用のトークンと期限を資格情報へ写すことを確認する。
// 与える情報: 認可済みの資格情報と、回転が通る偽の GitHub。
// 成功条件: form に client_id / client_secret / grant_type=refresh_token / refresh_token が入り、
// 返ったアクセストークンが `ghu_new`、資格情報の refresh_token が `ghr_new`、期限が now + 15638399 秒であること。
func TestRotate_更新用のトークンを回して期限を写す(t *testing.T) {
	f := newFakeGitHub(t)
	f.tokenResponses = []string{rotatedResponse}
	now := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)

	token, updated, err := f.client().Rotate(context.Background(), fullCredentials(), now)
	if err != nil {
		t.Fatalf("Rotate に失敗した: %v", err)
	}
	if token != "ghu_new" {
		t.Errorf("アクセストークンが %q（want ghu_new）", token)
	}
	if updated.RefreshToken != "ghr_new" {
		t.Errorf("更新用のトークンが %q（want ghr_new）", updated.RefreshToken)
	}
	if want := now.Add(15638399 * time.Second); !updated.RefreshTokenExpiresAt.Equal(want) {
		t.Errorf("期限が %v（want %v）", updated.RefreshTokenExpiresAt, want)
	}
	// 変わらない欄。
	if updated.ClientID != "Iv23liexample" || updated.AuthorizedLogin != "octocat" || updated.Slug != "continuo-octocat" {
		t.Errorf("回転で他の欄が変わった: %+v", updated)
	}
	if len(f.tokenForms) != 1 {
		t.Fatalf("トークンの要求が %d 回（want 1）", len(f.tokenForms))
	}
	form := f.tokenForms[0]
	want := map[string]string{
		"client_id": "Iv23liexample", "client_secret": "secret-example",
		"grant_type": "refresh_token", "refresh_token": "ghr_example",
	}
	for k, v := range want {
		if form.Get(k) != v {
			t.Errorf("form の %s が %q（want %q）", k, form.Get(k), v)
		}
	}
}

// 目的: GitHub が `error` を返したとき、その値を持つ DeniedError になることを確認する
// （設計 3-82c の2通り目の文面が「<GitHub が返した error の値>」を出す）。
// 与える情報: 200 で `{"error":"bad_refresh_token",…}` を返す偽の GitHub。
// 成功条件: errors.As で *DeniedError が取れ、Code が `bad_refresh_token` であること。
func TestRotate_断られたら_errorの値を持つDeniedError(t *testing.T) {
	f := newFakeGitHub(t)
	f.tokenResponses = []string{`{"error":"bad_refresh_token","error_description":"The refresh token passed is incorrect or expired."}`}

	_, _, err := f.client().Rotate(context.Background(), fullCredentials(), time.Now())
	var denied *githubapp.DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("DeniedError ではない: %v", err)
	}
	if denied.Code != "bad_refresh_token" {
		t.Errorf("Code が %q（want bad_refresh_token）", denied.Code)
	}
	if !strings.Contains(err.Error(), "bad_refresh_token") {
		t.Errorf("文言に error の値が無い: %v", err)
	}
}

// 目的: 非 2xx と、アクセストークンの無い応答が、それぞれエラーになることを確認する。
// 与える情報: 503 を返す偽の GitHub と、空の JSON を返す偽の GitHub。
// 成功条件: どちらもエラーで、DeniedError ではないこと。
func TestRotate_非2xxとトークン無しはエラー(t *testing.T) {
	f := newFakeGitHub(t)
	f.tokenStatus = http.StatusServiceUnavailable
	f.tokenResponses = []string{`{}`}
	_, _, err := f.client().Rotate(context.Background(), fullCredentials(), time.Now())
	if err == nil {
		t.Fatal("503 なのにエラーにならない")
	}
	var denied *githubapp.DeniedError
	if errors.As(err, &denied) {
		t.Errorf("503 が DeniedError になっている: %v", err)
	}

	g := newFakeGitHub(t)
	g.tokenResponses = []string{`{"token_type":"bearer"}`}
	_, _, err = g.client().Rotate(context.Background(), fullCredentials(), time.Now())
	if err == nil {
		t.Fatal("アクセストークンが無いのにエラーにならない")
	}
}

// 目的: 更新用のトークンを持たない資格情報では、GitHub を叩かずに落ちることを確認する。
// 与える情報: 認可の前の資格情報。
// 成功条件: エラーが返り、偽の GitHub に要求が1つも届かないこと。
func TestRotate_未認可なら叩かずに落ちる(t *testing.T) {
	f := newFakeGitHub(t)
	_, _, err := f.client().Rotate(context.Background(),
		githubapp.Credentials{ClientID: "id", ClientSecret: "secret"}, time.Now())
	if err == nil {
		t.Fatal("未認可なのに落ちない")
	}
	if len(f.tokenForms) != 0 {
		t.Errorf("未認可なのに GitHub を %d 回叩いた", len(f.tokenForms))
	}
}

// 目的: 認可の code の交換が、設計 1-1 のとおりの form を送ることを確認する。
// 与える情報: GitHub App を作った直後の資格情報と、code。
// 成功条件: form に client_id / client_secret / code が入り、grant_type が無いこと。
// 返った資格情報に更新用のトークンと期限が入っていること。
func TestExchange_codeを更新用のトークンへ交換する(t *testing.T) {
	f := newFakeGitHub(t)
	f.tokenResponses = []string{rotatedResponse}
	now := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	creds := githubapp.Credentials{ClientID: "Iv23liexample", ClientSecret: "secret-example", Slug: "continuo-octocat"}

	token, updated, err := f.client().Exchange(context.Background(), creds, "code-1", now)
	if err != nil {
		t.Fatalf("Exchange に失敗した: %v", err)
	}
	if token != "ghu_new" || updated.RefreshToken != "ghr_new" || updated.RefreshTokenExpiresAt.IsZero() {
		t.Errorf("交換の結果が違う: token=%q creds=%+v", token, updated)
	}
	form := f.tokenForms[0]
	if form.Get("code") != "code-1" || form.Get("client_id") != "Iv23liexample" || form.Get("client_secret") != "secret-example" {
		t.Errorf("form が違う: %v", form)
	}
	if form.Get("grant_type") != "" {
		t.Errorf("交換なのに grant_type が付いている: %v", form)
	}
}

// 目的: Viewer がアクセストークンを Bearer で送り、login を返すことを確認する（設計 3-82f）。
// 与える情報: `GET /user` が octocat を返す偽の GitHub。
// 成功条件: Authorization が `Bearer ghu_x` で、login が octocat。
func TestViewer_アクセストークンで持ち主を引く(t *testing.T) {
	f := newFakeGitHub(t)
	login, err := f.client().Viewer(context.Background(), "ghu_x")
	if err != nil {
		t.Fatalf("Viewer に失敗した: %v", err)
	}
	if login != "octocat" {
		t.Errorf("login が %q（want octocat）", login)
	}
	if len(f.userAuth) != 1 || f.userAuth[0] != "Bearer ghu_x" {
		t.Errorf("Authorization が %v（want [Bearer ghu_x]）", f.userAuth)
	}
}

// 目的: manifest の変換が、code をパスに入れて POST し、要る欄だけを返すことを確認する（設計 3-82g）。
//
// **秘密鍵（`pem`）と `webhook_secret` は返ってくるが、持たない**（設計 3-82b）。
//
// 与える情報: 16 の欄を返す偽の GitHub（実測の欄。設計 7-6）。
// 成功条件: code がパスで届き、client_id / client_secret / slug / name / html_url が入ること。
func TestConvert_manifestの変換で要る欄だけを受け取る(t *testing.T) {
	f := newFakeGitHub(t)
	got, err := f.client().Convert(context.Background(), "code-xyz")
	if err != nil {
		t.Fatalf("Convert に失敗した: %v", err)
	}
	if len(f.conversionCodes) != 1 || f.conversionCodes[0] != "code-xyz" {
		t.Errorf("届いた code が %v（want [code-xyz]）", f.conversionCodes)
	}
	want := githubapp.Converted{
		ClientID: "Iv23liexample", ClientSecret: "secret-example", Slug: "continuo-octocat",
		Name: "continuo-octocat", HTMLURL: "https://github.com/apps/continuo-octocat",
	}
	if got != want {
		t.Errorf("Converted = %+v, want %+v", got, want)
	}
	// **持たないことを型で確かめる。**JSON にして pem が出ないこと。
	data, _ := json.Marshal(got)
	if strings.Contains(string(data), "PRIVATE KEY") || strings.Contains(string(data), "hook-secret") {
		t.Errorf("秘密鍵か webhook_secret を持っている: %s", data)
	}
}

// captureHandler は slog の出力を捕まえる。
type captureHandler struct {
	buf bytes.Buffer
}

func newCaptureLogger() (*slog.Logger, *captureHandler) {
	h := &captureHandler{}
	return slog.New(slog.NewTextHandler(&h.buf, &slog.HandlerOptions{Level: slog.LevelDebug})), h
}

// 目的: AcquireToken が「ロック → 読む → 回す → 書き戻す → 放す」を1回通すことを確認する（設計 1-3）。
//
// **書き戻しが要点である。**古い更新用のトークンは回転で無効になっているので、
// 書き戻さないと次の回転が必ず落ちる（認可のやり直しになる）。
//
// 与える情報: 認可済みの資格情報のファイルと、回転が通る偽の GitHub。
// 成功条件: `ghu_new` が返り、ファイルの refresh_token が `ghr_new` に変わり、古い `ghr_example` が
// ファイルのどこにも無く、ロックが放されていること（直後に Lock が取れる）。
func TestAcquireToken_回して書き戻してロックを放す(t *testing.T) {
	f := newFakeGitHub(t)
	f.tokenResponses = []string{rotatedResponse}
	store := githubapp.NewStore(t.TempDir())
	if err := store.Write(fullCredentials()); err != nil {
		t.Fatal(err)
	}
	logger, captured := newCaptureLogger()

	token, err := githubapp.AcquireToken(context.Background(), store, f.client(), time.Second, nil, logger)
	if err != nil {
		t.Fatalf("AcquireToken に失敗した: %v", err)
	}
	if token != "ghu_new" {
		t.Errorf("アクセストークンが %q（want ghu_new）", token)
	}
	updated, perm, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if updated.RefreshToken != "ghr_new" {
		t.Errorf("書き戻した更新用のトークンが %q（want ghr_new）", updated.RefreshToken)
	}
	if perm != githubapp.CredentialsPerm {
		t.Errorf("書き戻したあとの権限が %o（want 0600）", perm)
	}
	raw, _ := readFile(t, store.Path())
	if strings.Contains(raw, "ghr_example") {
		t.Error("古い更新用のトークンがファイルに残っている")
	}
	if strings.Contains(raw, "ghu_new") {
		t.Error("アクセストークンがファイルに書かれている（設計 3-82b。書かない）")
	}
	if strings.Contains(captured.buf.String(), "ghu_new") || strings.Contains(captured.buf.String(), "ghr_") {
		t.Errorf("トークンがログに出ている: %s", captured.buf.String())
	}
	l, err := store.Lock(200 * time.Millisecond)
	if err != nil {
		t.Fatalf("AcquireToken のあとにロックが放されていない: %v", err)
	}
	_ = l.Release()
}

// 目的: 資格情報が無いときは ErrNotFound を包んだエラーで、GitHub を叩かないことを確認する。
// 与える情報: 空の一時ディレクトリ。
// 成功条件: errors.Is(err, ErrNotFound) が真で、偽の GitHub に要求が届かないこと。
func TestAcquireToken_資格情報が無ければErrNotFound(t *testing.T) {
	f := newFakeGitHub(t)
	_, err := githubapp.AcquireToken(context.Background(), githubapp.NewStore(t.TempDir()), f.client(), time.Second, nil, nil)
	if !errors.Is(err, githubapp.ErrNotFound) {
		t.Errorf("ErrNotFound を包んでいない: %v", err)
	}
	if len(f.tokenForms) != 0 {
		t.Errorf("資格情報が無いのに GitHub を %d 回叩いた", len(f.tokenForms))
	}
}

// 目的: 権限が 0600 でないときは WARN を1行出して、そのまま読むことを確認する（設計 3-82b の表）。
// 与える情報: 0644 の資格情報。
// 成功条件: トークンが返り、ログに「0600」と「chmod 600」が出ること。
func TestAcquireToken_権限が違えばWARNを出して読む(t *testing.T) {
	f := newFakeGitHub(t)
	f.tokenResponses = []string{rotatedResponse}
	store := githubapp.NewStore(t.TempDir())
	if err := store.Write(fullCredentials()); err != nil {
		t.Fatal(err)
	}
	if err := chmod(store.Path(), 0o644); err != nil {
		t.Fatal(err)
	}
	logger, captured := newCaptureLogger()
	token, err := githubapp.AcquireToken(context.Background(), store, f.client(), time.Second, nil, logger)
	if err != nil {
		t.Fatalf("0644 で止まった: %v", err)
	}
	if token != "ghu_new" {
		t.Errorf("トークンが %q", token)
	}
	if out := captured.buf.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "chmod 600") {
		t.Errorf("権限の WARN が出ていない: %s", out)
	}
}

// 目的: 回転が落ちたら書き戻さず、元の資格情報が残ることを確認する。
// 与える情報: 断る偽の GitHub。
// 成功条件: エラーが返り、ファイルの refresh_token が元のままであること。ロックは放されていること。
func TestAcquireToken_回転が落ちたら書き戻さない(t *testing.T) {
	f := newFakeGitHub(t)
	f.tokenResponses = []string{`{"error":"bad_refresh_token","error_description":"expired"}`}
	store := githubapp.NewStore(t.TempDir())
	if err := store.Write(fullCredentials()); err != nil {
		t.Fatal(err)
	}
	_, err := githubapp.AcquireToken(context.Background(), store, f.client(), time.Second, nil, nil)
	if err == nil {
		t.Fatal("断られたのにエラーにならない")
	}
	got, _, _ := store.Read()
	if got.RefreshToken != "ghr_example" {
		t.Errorf("落ちたのに資格情報が書き換わった: %+v", got)
	}
	l, err := store.Lock(200 * time.Millisecond)
	if err != nil {
		t.Fatalf("落ちたあとにロックが放されていない: %v", err)
	}
	_ = l.Release()
}

// 目的: TokenSource が AcquireToken を同じ引数で包むことを確認する（`tracker.NewAdapter` へ渡す形）。
// 与える情報: 認可済みの資格情報と、回転が通る偽の GitHub。
// 成功条件: 返った関数を呼ぶと `ghu_new` が返り、2回呼ぶと GitHub に2回届くこと。
func TestTokenSource_呼ぶたびに回す(t *testing.T) {
	f := newFakeGitHub(t)
	f.tokenResponses = []string{rotatedResponse, strings.Replace(rotatedResponse, "ghu_new", "ghu_second", 1)}
	store := githubapp.NewStore(t.TempDir())
	if err := store.Write(fullCredentials()); err != nil {
		t.Fatal(err)
	}
	src := githubapp.TokenSource(store, f.client(), time.Second, nil, nil)
	first, err := src(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := src(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first != "ghu_new" || second != "ghu_second" {
		t.Errorf("トークンが %q / %q", first, second)
	}
	if len(f.tokenForms) != 2 {
		t.Errorf("回転が %d 回（want 2）", len(f.tokenForms))
	}
	// 2回目は1回目で書き戻した ghr_new を使っている。
	if got := f.tokenForms[1].Get("refresh_token"); got != "ghr_new" {
		t.Errorf("2回目の回転が古い更新用のトークン %q を使った（want ghr_new）", got)
	}
}
