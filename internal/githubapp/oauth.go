package githubapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
)

// Endpoints は GitHub の接続先である。
//
// **テストは httptest.Server の URL を渡す。**本番は DefaultEndpoints。
type Endpoints struct {
	// Web は `https://github.com` である（manifest の POST 先・認可の画面・トークンの交換）。
	Web string
	// API は `https://api.github.com` である（manifest の変換・`GET /user`）。
	API string
}

// DefaultEndpoints は本番の GitHub の接続先を返す。
func DefaultEndpoints() Endpoints {
	return Endpoints{Web: "https://github.com", API: "https://api.github.com"}
}

// withDefaults は空の欄を本番の値で埋める。
func (e Endpoints) withDefaults() Endpoints {
	d := DefaultEndpoints()
	if e.Web == "" {
		e.Web = d.Web
	}
	if e.API == "" {
		e.API = d.API
	}
	return e
}

// Client は GitHub と往復する口である。
//
// **`http.DefaultClient` を渡さない。**`Timeout` が 0 なので、応答ヘッダを返さない相手に
// 当たると、ロックを掴んだまま無期限に止まる（ロックの上限で他のプロセスも道連れになる）。
type Client struct {
	// HTTP はリクエストを送るクライアントである。nil なら全体30秒のクライアントを組み立てる。
	HTTP *http.Client
	// Endpoints は接続先である。空の欄は本番の値で埋める。
	Endpoints Endpoints
}

// defaultHTTPTimeout は HTTP を渡されなかったときに組み立てるクライアントの全体の待ち時間である。
const defaultHTTPTimeout = 30 * time.Second

// http は使うクライアントを返す。
func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: defaultHTTPTimeout}
}

// maxBodyForError はエラーの文言に載せる応答本文の上限である。
const maxBodyForError = 300

// truncate は応答本文をエラーの文言に載せる長さへ切る。
//
// **トークンを運ぶ端点（`POST /login/oauth/access_token`）の応答には使ってはならない。**
// そちらは contentType を載せる（postToken）。
func truncate(b []byte) string {
	if len(b) > maxBodyForError {
		return string(b[:maxBodyForError]) + "…"
	}
	return string(b)
}

// contentTypeOf は応答の `Content-Type` を返す。空なら `dashboard.none` の文言（`—`）を返す。
//
// **トークンを運ぶ端点で、応答本文の代わりにエラーの文言へ載せるものである。**
// 本文には `access_token` と `refresh_token` がそのまま入っており、
// GitHub が `Accept: application/json` を無視して form 形式
// （`access_token=ghu_…&refresh_token=ghr_…`）で返すと、本文を載せた文言に
// トークンが乗る。その文言は Adapter の Warn・`continuo github-app token` の標準エラー・
// 起動時の検査の文面・ダッシュボードの画面の4つへ流れるので、
// **設計 3-82d「トークンは1文字も出さない」がその1行で破れる。**
// **`Content-Type` だけなら、何が返ったか（JSON か form か HTML か）は分かる。**
//
// resp: 読んだ応答。
// 戻り値: `Content-Type` の値。無ければ `dashboard.none` の文言。
//
//	**日本語を直に書かない。**この値は英語の文言の引数にも入るので、直に書くと
//	`language: en` の利用者の画面に日本語が混じる（しかも Go に埋まっているので、
//	文言で検索しても資源に出てこない）。
func contentTypeOf(resp *http.Response) string {
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		return ct
	}
	return i18n.T(i18n.KeyDashboardNone)
}

// tokenResponse は `POST /login/oauth/access_token` の応答である。
//
// **GitHub は失敗を 200 で返すことがある**（`error` と `error_description` を本文に入れて）。
// だからステータスだけでは判定できず、`error` を見る。
type tokenResponse struct {
	AccessToken           string `json:"access_token"`
	ExpiresIn             int    `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
	TokenType             string `json:"token_type"`
	Error                 string `json:"error"`
	ErrorDescription      string `json:"error_description"`
}

// DeniedError は GitHub がトークンの発行を断ったことを表す。
//
// **`Code` は GitHub が返した `error` の値そのままである**（`bad_refresh_token` など）。
// 起動時の検査は「更新用のトークンを回せませんでした（<GitHub が返した error の値>）」と出す（3-82c）。
type DeniedError struct {
	// Code は GitHub の `error` の値である。
	Code string
	// Description は GitHub の `error_description` の値である。
	Description string
}

// Error は error インタフェースを満たす。
func (e *DeniedError) Error() string {
	return i18n.T(i18n.KeyGitHubAppTokenDenied, e.Code, e.Description)
}

// postToken は `POST {Web}/login/oauth/access_token` を1回叩き、応答を読む。
//
// grant: `grant_type=refresh_token` なら回転、`code` なら認可の交換。
func (c Client) postToken(ctx context.Context, form url.Values) (tokenResponse, error) {
	ep := c.Endpoints.withDefaults()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		ep.Web+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, i18n.Errorf(i18n.KeyGitHubAppTokenRequestFailed, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http().Do(req)
	if err != nil {
		return tokenResponse{}, i18n.Errorf(i18n.KeyGitHubAppTokenRequestFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return tokenResponse{}, i18n.Errorf(i18n.KeyGitHubAppTokenRequestFailed, err)
	}
	// **この端点の応答本文は、エラーの文言へ1バイトも載せない**（contentTypeOf）。
	// 本文そのものがトークンだからである。**代わりに状態コードと `Content-Type` を載せる。**
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tokenResponse{}, i18n.Errorf(i18n.KeyGitHubAppTokenStatus, resp.StatusCode, contentTypeOf(resp))
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return tokenResponse{}, i18n.Errorf(i18n.KeyGitHubAppTokenParseFailed, contentTypeOf(resp), err)
	}
	if tr.Error != "" {
		return tokenResponse{}, &DeniedError{Code: tr.Error, Description: tr.ErrorDescription}
	}
	if tr.AccessToken == "" {
		return tokenResponse{}, i18n.Errorf(i18n.KeyGitHubAppTokenEmpty, contentTypeOf(resp))
	}
	// **更新用のトークンが返らなかったら、そこで落とす。**黙って受けると、二重に失う。
	//
	//   - 認可（Exchange）で返らないと、画面は「書きました」と名乗るのに、入口の画面は段2 を出す。
	//     `true` で再起動すると資格情報が欠けていると言われ、認可をやり直しても同じになる（抜け道が無い）。
	//   - 回転（Rotate）で返らないと、**使い終わって無効になった古いトークンを書き戻す。**
	//     期限内のまま永久に回らず、以後の投稿が全部「attribution 無しで投稿しています」の断り付きになる。
	//
	// **設計 3-82b「更新用のトークンとその期限を一緒に書く」と 3-82d「1回使うと無効になる」が、
	// 返ってくることを前提にしている。**前提が崩れたことを、その場で言う。
	if tr.RefreshToken == "" {
		return tokenResponse{}, i18n.Errorf(i18n.KeyGitHubAppTokenNoRefresh, contentTypeOf(resp))
	}
	return tr, nil
}

// applyToken は GitHub が返したトークンの組を資格情報へ写す。
//
// **更新用のトークンとその期限を一緒に書く。**期限を写さないと、最初の認可から181日目に
// 資格情報が生きているのに全部止まる（3-82b）。
func applyToken(c Credentials, tr tokenResponse, now time.Time) Credentials {
	if tr.RefreshToken != "" {
		c.RefreshToken = tr.RefreshToken
	}
	if tr.RefreshTokenExpiresIn > 0 {
		c.RefreshTokenExpiresAt = now.Add(time.Duration(tr.RefreshTokenExpiresIn) * time.Second).UTC()
	}
	return c
}

// Rotate は更新用のトークンを1回転させ、アクセストークンと、書き戻すべき新しい資格情報を返す
// （3-82d「continuo 本体の投稿」・1-2 の図）。
//
// **返った資格情報は、呼ぶ側が必ず Store.Write で書き戻すこと。**古い更新用のトークンは
// この呼び出しで無効になっているので、書き戻さずに落ちると認可のやり直しになる。
// **ロックの中で呼ぶこと**（Store.Lock）。
//
// ctx: 呼び出しに適用するコンテキスト。
// creds: いまの資格情報（`client_id` / `client_secret` / `refresh_token` が要る）。
// now: いまの時刻（新しい期限を計算する）。
// 戻り値の1つ目: アクセストークン（8時間。**使い回してはならない**）。
// 戻り値の2つ目: 新しい更新用のトークンと期限を写した資格情報。
// 戻り値の3つ目: GitHub が断った場合は *DeniedError、往復に失敗した場合はそのエラー。
func (c Client) Rotate(ctx context.Context, creds Credentials, now time.Time) (string, Credentials, error) {
	if !creds.HasApp() || !creds.HasRefreshToken() {
		return "", creds, i18n.Errorf(i18n.KeyGitHubAppCredentialsIncomplete)
	}
	form := url.Values{
		"client_id":     {creds.ClientID},
		"client_secret": {creds.ClientSecret},
		"grant_type":    {"refresh_token"},
		"refresh_token": {creds.RefreshToken},
	}
	tr, err := c.postToken(ctx, form)
	if err != nil {
		return "", creds, err
	}
	return tr.AccessToken, applyToken(creds, tr, now), nil
}

// Exchange は認可の戻り（`/github-app/authorized?code=…`）の `code` をトークンへ交換する
// （3-82g の段3。1-1 の図）。
//
// **`code` は使い捨てである。**交換が通ったのに書き戻せなかったら、認可からやり直しになる。
//
// ctx: 呼び出しに適用するコンテキスト。
// creds: `client_id` と `client_secret` を持つ資格情報。
// code: GitHub から戻った `code`。
// now: いまの時刻。
// 戻り値: Rotate と同じ。
func (c Client) Exchange(ctx context.Context, creds Credentials, code string, now time.Time) (string, Credentials, error) {
	if !creds.HasApp() {
		return "", creds, i18n.Errorf(i18n.KeyGitHubAppCredentialsNoApp)
	}
	form := url.Values{
		"client_id":     {creds.ClientID},
		"client_secret": {creds.ClientSecret},
		"code":          {code},
	}
	tr, err := c.postToken(ctx, form)
	if err != nil {
		return "", creds, err
	}
	return tr.AccessToken, applyToken(creds, tr, now), nil
}

// Viewer はアクセストークンの持ち主のログイン名を `GET {API}/user` で引く（3-82f）。
//
// **`gh api user` と突き合わせるために、認可の直後に1回だけ呼ぶ。**起動時の検査と doctor は
// 呼ばない（資格情報の `authorized_login` を使う）。
//
// ctx: 呼び出しに適用するコンテキスト。
// accessToken: いま取ったアクセストークン。
// 戻り値: ログイン名と、取れなかった場合のエラー。
func (c Client) Viewer(ctx context.Context, accessToken string) (string, error) {
	ep := c.Endpoints.withDefaults()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.API+"/user", nil)
	if err != nil {
		return "", i18n.Errorf(i18n.KeyGitHubAppViewerRequestFailed, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := c.http().Do(req)
	if err != nil {
		return "", i18n.Errorf(i18n.KeyGitHubAppViewerRequestFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", i18n.Errorf(i18n.KeyGitHubAppViewerRequestFailed, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", i18n.Errorf(i18n.KeyGitHubAppViewerStatus, resp.StatusCode, truncate(body))
	}
	var u struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return "", i18n.Errorf(i18n.KeyGitHubAppViewerParseFailed, truncate(body), err)
	}
	if u.Login == "" {
		return "", i18n.Errorf(i18n.KeyGitHubAppViewerEmpty, truncate(body))
	}
	return u.Login, nil
}

// Converted は manifest の変換（`POST /app-manifests/{code}/conversions`）で受け取るもののうち、
// continuo が使う欄である。
//
// **`pem`（秘密鍵）と `webhook_secret` は受け取ったその場で捨てる**（3-82b。構造体に持たない）。
type Converted struct {
	// ClientID は GitHub App の client ID である。
	ClientID string
	// ClientSecret は GitHub App の client secret である。
	ClientSecret string
	// Slug は GitHub App の slug である。
	Slug string
	// Name は GitHub App の名前である（画面に出す）。
	Name string
	// HTMLURL は GitHub App の設定の画面の URL である（画面に出す）。
	HTMLURL string
}

// Convert は manifest の流れで戻った `code` を GitHub App の資格情報へ変換する（3-82g の段1）。
//
// ctx: 呼び出しに適用するコンテキスト。
// code: `/github-app/created?code=…` で戻った値。**使い捨てである。**
// 戻り値: 変換の結果。往復の失敗・非 2xx・欄の欠けはエラー。
func (c Client) Convert(ctx context.Context, code string) (Converted, error) {
	ep := c.Endpoints.withDefaults()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/app-manifests/%s/conversions", ep.API, url.PathEscape(code)), nil)
	if err != nil {
		return Converted{}, i18n.Errorf(i18n.KeyGitHubAppConvertRequestFailed, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.http().Do(req)
	if err != nil {
		return Converted{}, i18n.Errorf(i18n.KeyGitHubAppConvertRequestFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Converted{}, i18n.Errorf(i18n.KeyGitHubAppConvertRequestFailed, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Converted{}, i18n.Errorf(i18n.KeyGitHubAppConvertStatus, resp.StatusCode, truncate(body))
	}
	// **要る欄だけ読む。**`pem` と `webhook_secret` はここで読まず、body ごと捨てる。
	var raw struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		Slug         string `json:"slug"`
		Name         string `json:"name"`
		HTMLURL      string `json:"html_url"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Converted{}, i18n.Errorf(i18n.KeyGitHubAppConvertParseFailed, err)
	}
	if raw.ClientID == "" || raw.ClientSecret == "" || raw.Slug == "" {
		return Converted{}, i18n.Errorf(i18n.KeyGitHubAppConvertIncomplete)
	}
	return Converted{
		ClientID:     raw.ClientID,
		ClientSecret: raw.ClientSecret,
		Slug:         raw.Slug,
		Name:         raw.Name,
		HTMLURL:      raw.HTMLURL,
	}, nil
}
