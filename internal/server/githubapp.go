package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/maimuzo/continuo/internal/githubapp"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/tracker"
)

// GitHub App を作る導線の経路である
// （docs/plans/impl/issue245_github_app_attribution.md の 3-82g。**5本とも GET である**）。
//
// **`Options.GitHubApp` が nil なら1本も張らない**（`newMux`）。
const (
	// GitHubAppPath は入口である。資格情報のファイルを読んで、段1（作る）・段2（install）・
	// 段3（認可）・「設定済み」のどれを出すかを自分で見分ける。人間に選ばせない。
	GitHubAppPath = "/github-app"
	// GitHubAppCreatedPath は manifest の流れで GitHub App を作ったあとに GitHub が戻す先である
	// （`?code=…&state=…`）。`code` を `client_id` / `client_secret` に交換して書き、段2 を出す。
	GitHubAppCreatedPath = "/github-app/created"
	// GitHubAppInstalledPath は install のあとに GitHub が戻す先である（`?installation_id=…`）。
	// **`installation_id` は読み捨てる。**段3 を出すだけである。
	GitHubAppInstalledPath = "/github-app/installed"
	// GitHubAppAuthorizePath は再認可の入口である。段3 を直に出す。
	GitHubAppAuthorizePath = "/github-app/authorize"
	// GitHubAppAuthorizedPath は認可のあとに GitHub が戻す先である（`?code=…&state=…`）。
	// `code` を更新用のトークンに交換して書き、段4 を出す。
	GitHubAppAuthorizedPath = "/github-app/authorized"
)

const (
	// githubAppStateTTL は `state` の期限である（3-82g「`state` を必ず突き合わせる」）。
	//
	// **30分。**段1 では sudo mode の再認証（実測 7-6）が挟まり、パスキーが別の端末にある人は
	// 10分を超えうる。
	githubAppStateTTL = 30 * time.Minute

	// githubAppStateBytes は `state` に使う乱数の長さである（`crypto/rand` で32バイト）。
	githubAppStateBytes = 32

	// githubAppGHLoginTimeout は `gh api user` を叩く外部プロセスに掛ける期限である
	// （internal/orchestrator の `ghLoginTimeout` と同じ値）。
	githubAppGHLoginTimeout = 10 * time.Second

	// githubAppNamePrefix は GitHub App の既定の名前の頭である（`continuo-<gh api user のログイン名>`。3-82c）。
	// ログイン名が取れなければ、この値だけを名前にする。
	githubAppNamePrefix = "continuo"

	// githubAppNameQuery は段1 の名前の入れ直しの form が送る問い合わせの名前である
	// （`GET /github-app?name=…`。POST の経路は張らない。3-82g「CSP を、この5本の経路だけ緩める」）。
	githubAppNameQuery = "name"
)

// githubAppPermissions は manifest の `default_permissions` である（3-82b。**`Issues` の読み書きと
// `Metadata` の読み取りだけ**。`Pull requests` を足さない）。
var githubAppPermissions = map[string]string{"issues": "write", "metadata": "read"}

// 画面の段を表す値である（テンプレートの `{{ if eq .Stage "…" }}` が見る）。
const (
	githubAppStageCreate     = "create"
	githubAppStageInstall    = "install"
	githubAppStageAuthorize  = "authorize"
	githubAppStageDone       = "done"
	githubAppStageConfigured = "configured"
	githubAppStageError      = "error"
)

// oauthState は、GitHub から戻ってくる要求が「本当に自分が始めた流れの続きか」を確かめる値を
// 1本だけ持つ（3-82g「`state` を必ず突き合わせる」）。
//
// **段1 用と段3 用を別々に1本ずつ持つ**（`Server.createState` / `Server.authorizeState`）。
// 段1 の待ちの最中に段3 の画面が描かれても、段1 の `state` は消えない。
//
// **専用の mutex で守る。**`net/http` はハンドラを並行に走らせる。`Server.mu` は `ln` と
// `closed` 用なので使わない。
//
// **メモリだけに持つ。**ファイルにも資格情報にも書かない。continuo を再起動すると消えるので、
// そのときは画面をもう一度開いてもらう（段1 と段3 の画面が「途中で止めたらこの段から」と案内する）。
type oauthState struct {
	mu        sync.Mutex
	value     string
	expiresAt time.Time
}

// issue は新しい `state` を作って持ち替え、その値を返す。
//
// **画面を出すたびに作り直す。**前の値は捨てる（同じ段の画面を2回開いたら、古いほうの流れは続けられない）。
//
// now: いまの時刻。
// 戻り値: base64url で符号化した32バイトの乱数と、乱数を取れなかった場合のエラー。
func (st *oauthState) issue(now time.Time) (string, error) {
	b := make([]byte, githubAppStateBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	v := base64.RawURLEncoding.EncodeToString(b)
	st.mu.Lock()
	st.value = v
	st.expiresAt = now.Add(githubAppStateTTL)
	st.mu.Unlock()
	return v, nil
}

// consume は戻ってきた `state` を突き合わせ、合っていれば捨てる。
//
// **1回使ったら捨てる。**合っていても合っていなくても、期限を過ぎていれば捨てる。
// 比べるのは定数時間で行う（`crypto/subtle`）。
//
// got: GitHub から戻ってきた `state`。
// now: いまの時刻。
// 戻り値: 持っている値と一致し、期限内なら真。**空の値は何にも一致しない。**
func (st *oauthState) consume(got string, now time.Time) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.value == "" || got == "" {
		return false
	}
	if !now.Before(st.expiresAt) {
		st.value = ""
		return false
	}
	ok := subtle.ConstantTimeCompare([]byte(st.value), []byte(got)) == 1
	if ok {
		st.value = ""
	}
	return ok
}

// githubAppManifest は GitHub App の manifest である（3-82g「manifest に何を書くか」）。
//
// **`hook_attributes` は書かない。**この設計は webhook を1つも使わない。書くと GitHub が
// `127.0.0.1` の URL を断る（実測 7-6）。
type githubAppManifest struct {
	// Name は GitHub App の名前である（`continuo-<ログイン名>`。GitHub の中で世界に1つ）。
	Name string `json:"name"`
	// URL は GitHub App の説明に出る URL である（`GitHubAppOptions.ProjectURL`）。
	URL string `json:"url"`
	// RedirectURL は作成のあとに戻る先である（`/github-app/created`）。
	RedirectURL string `json:"redirect_url"`
	// SetupURL は install のあとに戻る先である（`/github-app/installed`）。
	SetupURL string `json:"setup_url"`
	// CallbackURLs は認可のあとに戻る先である（`/github-app/authorized`）。
	CallbackURLs []string `json:"callback_urls"`
	// RequestOAuthOnInstall は **false** である。true にすると install と認可が1回で終わり、
	// 認可のときに何が起きるかを人間へ見せる画面が消える（3-82g）。
	RequestOAuthOnInstall bool `json:"request_oauth_on_install"`
	// Public は false である（自分だけが使う）。
	Public bool `json:"public"`
	// DefaultPermissions は `{"issues": "write", "metadata": "read"}` である。
	DefaultPermissions map[string]string `json:"default_permissions"`
}

// githubAppPage はテンプレートへ渡す値である。**段ごとに使う欄が違う。**
//
// **文言は1つも持たない。**テンプレートが `t "…"` で資源から引く。ここに入るのは
// URL・名前・パスといった値だけである。
type githubAppPage struct {
	// Stage はどの段の画面かである（`githubAppStage*`）。
	Stage string
	// CredentialsPath は資格情報のファイルのパスである（全段で「どこに書くか」を出す）。
	CredentialsPath string
	// SelfPath は入口の経路である（名前の入れ直しの form の送り先と、やり直しのリンク）。
	SelfPath string
	// AuthorizePath は再認可の入口の経路である。
	AuthorizePath string

	// 段1（作る）。
	//
	// Name は manifest に書く名前である（`?name=` で入れ直せる）。
	Name string
	// ManifestAction は manifest を POST する先である（`<Web>/settings/apps/new?state=<state>`）。
	ManifestAction string
	// ManifestJSON は hidden の入力に入れる manifest の JSON である。
	ManifestJSON string
	// RedirectURL / SetupURL / CallbackURL は戻り先の3つである（画面で人間に見せる）。
	RedirectURL string
	SetupURL    string
	CallbackURL string

	// 段2（install）。
	//
	// AppName は GitHub App の名前である。作った直後は GitHub が返した名前、あとから開いたときは slug。
	AppName string
	// AppHTMLURL は GitHub App の設定画面の URL である（作った直後だけ分かる。空ならリンクを出さない）。
	AppHTMLURL string
	// InstallURL は install の画面の URL である（`<Web>/apps/<slug>/installations/new`）。
	InstallURL string
	// JustCreated は `/github-app/created` から来た（いま作った）かである。
	JustCreated bool

	// 段3（認可）。
	//
	// AuthorizeURL は認可の画面の URL である（`<Web>/login/oauth/authorize?client_id=…&redirect_uri=…&state=…`）。
	AuthorizeURL string
	// Slug は GitHub App の slug である（attribution の説明で GitHub App の名前として出す）。
	Slug string
	// ExpiredAt は更新用のトークンが切れていたときの、その期限である（空なら切れていない）。
	ExpiredAt string

	// 段4（完了）と「設定済み」。
	//
	// AuthorizedLogin は認可したアカウントのログイン名である。
	AuthorizedLogin string
	// GHLogin は `gh api user` のログイン名である（取れなければ空）。
	GHLogin string
	// GHLoginUnknown は `gh api user` を叩けなかったことを表す。
	GHLoginUnknown bool
	// Mismatch は AuthorizedLogin と GHLogin が違うことを表す。
	Mismatch bool
	// ExpiresAt は更新用のトークンの期限である。
	ExpiresAt string

	// 続けられなかったとき。
	//
	// ErrorText は画面に出す理由である（資源から引いた文言）。
	ErrorText string
}

// githubAppCSPFor は GitHub App の5本の経路に付ける CSP を返す。
//
// **本番は `githubAppCSP` の定数そのものである。**テストが `Endpoints.Web` を httptest.Server に
// 向けたときだけ、`form-action` の GitHub の部分をその URL に差し替える。
//
// web: `Endpoints.Web`（空なら本番の値）。
// 戻り値: 付ける CSP。
func githubAppCSPFor(web string) string {
	def := githubapp.DefaultEndpoints().Web
	if web == "" || web == def {
		return githubAppCSP
	}
	return strings.Replace(githubAppCSP, def, web, 1)
}

// githubWeb は `Endpoints.Web`（空なら本番の値）を返す。
func (s *Server) githubWeb() string {
	if s.githubApp.Endpoints.Web != "" {
		return s.githubApp.Endpoints.Web
	}
	return githubapp.DefaultEndpoints().Web
}

// githubClient は GitHub と往復する口を組み立てる。
func (s *Server) githubClient() githubapp.Client {
	return githubapp.Client{HTTP: s.githubApp.HTTPClient, Endpoints: s.githubApp.Endpoints}
}

// ghLogin は `gh api user` のログイン名を、期限を付けて取る。
//
// **取れなくても止めない。**既定の名前は `continuo` だけになり、段4 の突き合わせは起動時の
// 検査に任せる（3-82f「`gh api user` が取れなかったときは、突き合わせずに起動する」と同じ立場）。
//
// ctx: 呼び出しに適用するコンテキスト。
// 戻り値: ログイン名と、取れなかった場合のエラー。
func (s *Server) ghLogin(ctx context.Context) (string, error) {
	fn := s.githubApp.GHLogin
	if fn == nil {
		fn = tracker.RunGHAPIUserLogin
	}
	ctx, cancel := context.WithTimeout(ctx, githubAppGHLoginTimeout)
	defer cancel()
	return fn(ctx)
}

// baseURL は GitHub から戻ってくる先の土台（`http://127.0.0.1:<port>`）を返す。
//
// **`<port>` は設定の値ではなく `Addr()` が返す実際のポートである**（3-82g「manifest に何を書くか」）。
// `server.port: 0` は OS が空きポートを選ぶので、設定の値を埋めると戻り先が `127.0.0.1:0` になり、
// GitHub から戻れない。
//
// **`Addr()` が空（listen していない。テストが `Handler()` を直に叩くとき）なら `r.Host` を使う。**
// 本番で空になるのは Close のあとだけで、そのとき応答はもう出ない。
//
// r: 受け取ったリクエスト。
// 戻り値: scheme と host:port まで（末尾に `/` は付かない）。
func (s *Server) baseURL(r *http.Request) string {
	if addr := s.Addr(); addr != "" {
		return "http://" + addr
	}
	return "http://" + r.Host
}

// newGitHubAppPage は全段で共通の欄を埋めた値を返す。
func (s *Server) newGitHubAppPage(stage string) githubAppPage {
	return githubAppPage{
		Stage:           stage,
		CredentialsPath: s.githubApp.Store.Path(),
		SelfPath:        GitHubAppPath,
		AuthorizePath:   GitHubAppAuthorizePath,
	}
}

// renderGitHubApp は GitHub App の画面を書き出す。
//
// **応答を書く前に CSP を自分の版で `Set` し直す**（3-82g「CSP を、この5本の経路だけ緩める」）。
// 外側の `withSafetyHeaders` が先に `form-action 'none'` を付けているので、ここで上書きしないと
// manifest を GitHub へ POST する form がブラウザ側で止まる。**変えるのは `form-action` の1指令だけ**である。
//
// w: 応答の書き出し先。
// r: 受け取ったリクエスト。
// status: 状態コード。
// page: テンプレートへ渡す値。
func (s *Server) renderGitHubApp(w http.ResponseWriter, r *http.Request, status int, page githubAppPage) {
	h := w.Header()
	h.Set("Content-Security-Policy", githubAppCSPFor(s.githubApp.Endpoints.Web))
	h.Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := githubAppTemplate.Execute(w, page); err != nil {
		// **ここでステータスコードは変えられない**（本文を書き始めているため）。記録だけ残す。
		s.logger.Warn("GitHub App の画面の HTML を書き出せません", "error", err, "path", r.URL.Path)
	}
}

// renderGitHubAppError は続けられなかった理由を出す。
//
// **資格情報は1バイトも書いていない状態で呼ぶこと**（書いたあとに呼ぶのは viewer の失敗だけで、
// その文言は「書きました」と名乗る）。
//
// w: 応答の書き出し先。
// r: 受け取ったリクエスト。
// status: 状態コード。
// key: 理由の文言のキー。
// args: 文言に当てる値。
func (s *Server) renderGitHubAppError(w http.ResponseWriter, r *http.Request, status int, key i18n.Key, args ...any) {
	page := s.newGitHubAppPage(githubAppStageError)
	page.ErrorText = i18n.T(key, args...)
	s.renderGitHubApp(w, r, status, page)
}

// readCredentials は資格情報を読む。無ければ空の資格情報を返す。
//
// **権限が 0600 でなくても読む。**WARN を1行出すだけで止めない（3-82b の表の「continuo 本体」と同じ）。
//
// 戻り値の1つ目: 読めた資格情報（ファイルが無ければゼロ値）。
// 戻り値の2つ目: 読めない・JSON として壊れている場合のエラー。**無いことはエラーにしない。**
func (s *Server) readCredentials() (githubapp.Credentials, error) {
	creds, perm, err := s.githubApp.Store.Read()
	if err != nil {
		if errors.Is(err, githubapp.ErrNotFound) {
			return githubapp.Credentials{}, nil
		}
		return githubapp.Credentials{}, err
	}
	if perm != githubapp.CredentialsPerm {
		s.logger.Warn("GitHub App の資格情報の権限が 0600 ではありません（そのまま読みます。chmod 600 で直してください）",
			"path", s.githubApp.Store.Path(), "perm", perm.String())
	}
	return creds, nil
}

// handleGitHubApp は入口である（`GET /github-app`）。資格情報の状態で段を出し分ける
// （3-82g「順序」の表）。
//
//	何も無い                                   … 段1（作る）
//	client_id と client_secret はある。更新用のトークンが無い … 段2（install）。「install 済みなら認可へ」も出す
//	更新用のトークンが切れている               … 段3（認可）
//	全部ある                                   … 「設定済み」と「認可だけをやり直す」のリンク
//
// `?name=<名前>` が付いていれば段1 の名前の欄にその値を入れる（名前が取られていたときの入れ直し）。
//
// w: 応答の書き出し先。
// r: 受け取ったリクエスト。
func (s *Server) handleGitHubApp(w http.ResponseWriter, r *http.Request) {
	creds, err := s.readCredentials()
	if err != nil {
		s.renderGitHubAppError(w, r, http.StatusInternalServerError, i18n.KeyServerGitHubAppReadFailed, err)
		return
	}
	now := s.now()
	switch {
	case !creds.HasApp():
		s.renderCreate(w, r, r.URL.Query().Get(githubAppNameQuery))
	case !creds.HasRefreshToken():
		page := s.newGitHubAppPage(githubAppStageInstall)
		page.AppName = creds.Slug
		page.Slug = creds.Slug
		page.InstallURL = s.installURL(creds.Slug)
		s.renderGitHubApp(w, r, http.StatusOK, page)
	case creds.RefreshTokenExpired(now):
		s.renderAuthorize(w, r, creds, creds.RefreshTokenExpiresAt)
	default:
		page := s.newGitHubAppPage(githubAppStageConfigured)
		page.Slug = creds.Slug
		page.AuthorizedLogin = creds.AuthorizedLogin
		page.ExpiresAt = formatExpiry(creds.RefreshTokenExpiresAt)
		s.renderGitHubApp(w, r, http.StatusOK, page)
	}
}

// renderCreate は段1（作る）を出す。**`state` を新しく作る。**
//
// w: 応答の書き出し先。
// r: 受け取ったリクエスト。
// name: `?name=` の値。空なら `continuo-<gh api user のログイン名>`（取れなければ `continuo`）。
func (s *Server) renderCreate(w http.ResponseWriter, r *http.Request, name string) {
	if name == "" {
		name = githubAppNamePrefix
		if login, err := s.ghLogin(r.Context()); err == nil {
			name = githubAppNamePrefix + "-" + login
		} else {
			s.logger.Warn("gh api user を叩けなかったので、GitHub App の既定の名前にログイン名を付けません", "error", err)
		}
	}
	state, err := s.createState.issue(s.now())
	if err != nil {
		s.renderGitHubAppError(w, r, http.StatusInternalServerError, i18n.KeyServerGitHubAppStateFailed, err)
		return
	}
	base := s.baseURL(r)
	m := githubAppManifest{
		Name:                  name,
		URL:                   s.githubApp.ProjectURL,
		RedirectURL:           base + GitHubAppCreatedPath,
		SetupURL:              base + GitHubAppInstalledPath,
		CallbackURLs:          []string{base + GitHubAppAuthorizedPath},
		RequestOAuthOnInstall: false,
		Public:                false,
		DefaultPermissions:    githubAppPermissions,
	}
	data, err := json.Marshal(m)
	if err != nil {
		// **map と文字列しか無いので、ここで落ちることは無い。**それでも黙って空の manifest を出さない。
		s.renderGitHubAppError(w, r, http.StatusInternalServerError, i18n.KeyServerGitHubAppStateFailed, err)
		return
	}
	page := s.newGitHubAppPage(githubAppStageCreate)
	page.Name = name
	page.ManifestAction = s.githubWeb() + "/settings/apps/new?" + url.Values{"state": {state}}.Encode()
	page.ManifestJSON = string(data)
	page.RedirectURL = m.RedirectURL
	page.SetupURL = m.SetupURL
	page.CallbackURL = m.CallbackURLs[0]
	s.logger.Info("GitHub App を作る画面を出しました（段1）", "name", name, "redirect_url", m.RedirectURL)
	s.renderGitHubApp(w, r, http.StatusOK, page)
}

// installURL は install の画面の URL を返す（`<Web>/apps/<slug>/installations/new`）。
func (s *Server) installURL(slug string) string {
	return s.githubWeb() + "/apps/" + url.PathEscape(slug) + "/installations/new"
}

// renderAuthorize は段3（認可）を出す。**`state` を新しく作る。**
//
// w: 応答の書き出し先。
// r: 受け取ったリクエスト。
// creds: `client_id` を持つ資格情報。
// expiredAt: 更新用のトークンが切れていたときの期限（切れていなければゼロ値）。
func (s *Server) renderAuthorize(w http.ResponseWriter, r *http.Request, creds githubapp.Credentials, expiredAt time.Time) {
	state, err := s.authorizeState.issue(s.now())
	if err != nil {
		s.renderGitHubAppError(w, r, http.StatusInternalServerError, i18n.KeyServerGitHubAppStateFailed, err)
		return
	}
	q := url.Values{
		"client_id":    {creds.ClientID},
		"redirect_uri": {s.baseURL(r) + GitHubAppAuthorizedPath},
		"state":        {state},
	}
	page := s.newGitHubAppPage(githubAppStageAuthorize)
	page.Slug = creds.Slug
	page.AuthorizeURL = s.githubWeb() + "/login/oauth/authorize?" + q.Encode()
	if !expiredAt.IsZero() {
		page.ExpiredAt = formatExpiry(expiredAt)
	}
	s.logger.Info("GitHub App を認可する画面を出しました（段3）", "slug", creds.Slug)
	s.renderGitHubApp(w, r, http.StatusOK, page)
}

// handleGitHubAppCreated は作成のあとの戻り先である（`GET /github-app/created?code=…&state=…`）。
//
// **`state` が段1 用の値と合わなければ、資格情報を1バイトも書かずに断る**（3-82g）。
// 合えば `code` を `client_id` / `client_secret` / `slug` に交換し、**ロックの中で読んで書く。**
// GitHub が一緒に返す秘密鍵（`pem`）と `webhook_secret` は、`githubapp.Client.Convert` が受け取った
// その場で捨てている（3-82b）。
//
// w: 応答の書き出し先。
// r: 受け取ったリクエスト。
func (s *Server) handleGitHubAppCreated(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !s.createState.consume(q.Get("state"), s.now()) {
		s.logger.Warn("GitHub App の作成の戻りの state が合わないので断りました（資格情報は書いていません）", "path", r.URL.Path)
		s.renderGitHubAppError(w, r, http.StatusBadRequest, i18n.KeyServerGitHubAppStateMismatch)
		return
	}
	code := q.Get("code")
	if code == "" {
		s.renderGitHubAppError(w, r, http.StatusBadRequest, i18n.KeyServerGitHubAppCodeMissing)
		return
	}
	converted, err := s.githubClient().Convert(r.Context(), code)
	if err != nil {
		s.logger.Warn("GitHub App の manifest の変換に失敗しました（資格情報は書いていません）", "error", err)
		s.renderGitHubAppError(w, r, http.StatusBadGateway, i18n.KeyServerGitHubAppConvertFailed, err)
		return
	}

	// **ロックの中で読んで書く。**本体の回転の書き戻しと重なると、片方の書き込みが消える。
	l, err := s.githubApp.Store.Lock(githubapp.DefaultLockTimeout)
	if err != nil {
		s.renderGitHubAppError(w, r, http.StatusServiceUnavailable, i18n.KeyServerGitHubAppLockFailed, err)
		return
	}
	defer s.releaseLock(l)
	creds, err := s.readCredentials()
	if err != nil {
		s.renderGitHubAppError(w, r, http.StatusInternalServerError, i18n.KeyServerGitHubAppReadFailed, err)
		return
	}
	creds.ClientID = converted.ClientID
	creds.ClientSecret = converted.ClientSecret
	creds.Slug = converted.Slug
	if err := s.githubApp.Store.Write(creds); err != nil {
		s.renderGitHubAppError(w, r, http.StatusInternalServerError, i18n.KeyServerGitHubAppWriteFailed,
			s.githubApp.Store.Path(), err)
		return
	}
	// **client_secret は1文字も出さない。**
	s.logger.Info("GitHub App を作り、client_id と client_secret と slug を書きました（段1 → 段2）",
		"path", s.githubApp.Store.Path(), "slug", converted.Slug, "name", converted.Name)

	page := s.newGitHubAppPage(githubAppStageInstall)
	page.AppName = converted.Name
	if page.AppName == "" {
		page.AppName = converted.Slug
	}
	page.AppHTMLURL = converted.HTMLURL
	page.Slug = converted.Slug
	page.InstallURL = s.installURL(converted.Slug)
	page.JustCreated = true
	s.renderGitHubApp(w, r, http.StatusOK, page)
}

// handleGitHubAppInstalled は install のあとの戻り先である（`GET /github-app/installed?installation_id=…`）。
//
// **`installation_id` は読み捨てる。**install は資格情報を書き換えず、install 済みかどうかは
// 資格情報からは分からない（3-82g「順序」の表）。段3（認可）を出すだけである。
//
// w: 応答の書き出し先。
// r: 受け取ったリクエスト。
func (s *Server) handleGitHubAppInstalled(w http.ResponseWriter, r *http.Request) {
	s.logger.Info("GitHub App の install から戻りました（段2 → 段3）")
	s.handleGitHubAppAuthorize(w, r)
}

// handleGitHubAppAuthorize は再認可の入口である（`GET /github-app/authorize`）。段3 を直に出す。
//
// **資格情報に `client_id` が無ければ断る。**認可のリンクは `client_id` 無しには組めない。
//
// w: 応答の書き出し先。
// r: 受け取ったリクエスト。
func (s *Server) handleGitHubAppAuthorize(w http.ResponseWriter, r *http.Request) {
	creds, err := s.readCredentials()
	if err != nil {
		s.renderGitHubAppError(w, r, http.StatusInternalServerError, i18n.KeyServerGitHubAppReadFailed, err)
		return
	}
	if !creds.HasApp() {
		s.renderGitHubAppError(w, r, http.StatusBadRequest, i18n.KeyServerGitHubAppNoApp)
		return
	}
	s.renderAuthorize(w, r, creds, time.Time{})
}

// handleGitHubAppAuthorized は認可のあとの戻り先である（`GET /github-app/authorized?code=…&state=…`）。
//
// **`state` が段3 用の値と合わなければ、資格情報を1バイトも書かずに断る。**
// 合えば `code` を更新用のトークンに交換し、そのアクセストークンで `viewer` を引いて
// `authorized_login` を入れ、**ロックの中で読んで書く**（3-82b「認可を通した直後」）。
// アクセストークンはここで捨てる（ファイルにもメモリにも残さない）。
//
// **認可したアカウントが `gh api user` と違っていても、資格情報は書く**（3-82f）。`code` は
// 使い捨てなので、書かないと認可が丸ごと失われる。違いは画面へ両方並べて出し、起動時の検査が止める。
//
// w: 応答の書き出し先。
// r: 受け取ったリクエスト。
func (s *Server) handleGitHubAppAuthorized(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !s.authorizeState.consume(q.Get("state"), s.now()) {
		s.logger.Warn("GitHub App の認可の戻りの state が合わないので断りました（資格情報は書いていません）", "path", r.URL.Path)
		s.renderGitHubAppError(w, r, http.StatusBadRequest, i18n.KeyServerGitHubAppStateMismatch)
		return
	}
	code := q.Get("code")
	if code == "" {
		s.renderGitHubAppError(w, r, http.StatusBadRequest, i18n.KeyServerGitHubAppCodeMissing)
		return
	}

	l, err := s.githubApp.Store.Lock(githubapp.DefaultLockTimeout)
	if err != nil {
		s.renderGitHubAppError(w, r, http.StatusServiceUnavailable, i18n.KeyServerGitHubAppLockFailed, err)
		return
	}
	defer s.releaseLock(l)
	creds, err := s.readCredentials()
	if err != nil {
		s.renderGitHubAppError(w, r, http.StatusInternalServerError, i18n.KeyServerGitHubAppReadFailed, err)
		return
	}
	if !creds.HasApp() {
		s.renderGitHubAppError(w, r, http.StatusBadRequest, i18n.KeyServerGitHubAppNoApp)
		return
	}
	client := s.githubClient()
	accessToken, updated, err := client.Exchange(r.Context(), creds, code, s.now())
	if err != nil {
		s.logger.Warn("GitHub App の認可の code を交換できませんでした（資格情報は書いていません）", "error", err)
		s.renderGitHubAppError(w, r, http.StatusBadGateway, i18n.KeyServerGitHubAppExchangeFailed, err)
		return
	}
	// **アクセストークンは viewer を引くのに1回使ったら、そのまま捨てる。**ファイルにも構造体にも入れない。
	login, viewerErr := client.Viewer(r.Context(), accessToken)
	updated.AuthorizedLogin = login
	if err := s.githubApp.Store.Write(updated); err != nil {
		s.renderGitHubAppError(w, r, http.StatusInternalServerError, i18n.KeyServerGitHubAppWriteFailed,
			s.githubApp.Store.Path(), err)
		return
	}
	if viewerErr != nil {
		// **更新用のトークンは書いてある。**認可し直せば `authorized_login` も入る。
		s.logger.Warn("認可は通りましたが、認可したアカウント名を引けませんでした（更新用のトークンは書きました）",
			"path", s.githubApp.Store.Path(), "error", viewerErr)
		s.renderGitHubAppError(w, r, http.StatusBadGateway, i18n.KeyServerGitHubAppViewerFailed,
			s.githubApp.Store.Path(), viewerErr)
		return
	}
	// **更新用のトークンは1文字も出さない。**
	s.logger.Info("GitHub App の認可を通し、更新用のトークンと期限と認可したアカウント名を書きました（段3 → 段4）",
		"path", s.githubApp.Store.Path(), "authorized_login", login,
		"refresh_token_expires_at", formatExpiry(updated.RefreshTokenExpiresAt))

	page := s.newGitHubAppPage(githubAppStageDone)
	page.Slug = updated.Slug
	page.AuthorizedLogin = login
	page.ExpiresAt = formatExpiry(updated.RefreshTokenExpiresAt)
	ghLogin, err := s.ghLogin(r.Context())
	switch {
	case err != nil:
		s.logger.Warn("gh api user を叩けなかったので、認可したアカウントとの突き合わせは起動時の検査に任せます", "error", err)
		page.GHLoginUnknown = true
	case ghLogin != login:
		s.logger.Warn("gh の持ち主と GitHub App を認可したアカウントが違います（起動時の検査で止まります）",
			"gh_login", ghLogin, "authorized_login", login)
		page.GHLogin = ghLogin
		page.Mismatch = true
	default:
		page.GHLogin = ghLogin
	}
	s.renderGitHubApp(w, r, http.StatusOK, page)
}

// releaseLock は資格情報のロックを放し、放せなければ記録だけ残す。
func (s *Server) releaseLock(l interface{ Release() error }) {
	if err := l.Release(); err != nil {
		s.logger.Warn("GitHub App の資格情報のロックを放せません", "lock_file", s.githubApp.Store.LockPath(), "error", err)
	}
}

// formatExpiry は更新用のトークンの期限を画面に出す形（RFC 3339。UTC）にする。ゼロ値なら「—」。
func formatExpiry(t time.Time) string {
	if t.IsZero() {
		return i18n.T(i18n.KeyDashboardNone)
	}
	return t.UTC().Format(time.RFC3339)
}
