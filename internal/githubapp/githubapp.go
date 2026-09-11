// Package githubapp は GitHub App の資格情報（`~/.continuo/github-app-credentials.json`）を
// 読み書きし、更新用のトークンを回してアクセストークンを取る
// （docs/plans/impl/issue245_github_app_attribution.md の 3-82b / 3-82d / 3-82g）。
//
// **使う側は4つある。**continuo 本体（`PostComment` の投稿）・`continuo github-app token`・
// `continuo doctor`・ダッシュボードの `/github-app` の画面である。**読み書きと回転の処理は
// ここにしか置かない。**写しを持つと、書き戻しの形が片方だけずれて資格情報が死ぬ。
//
// **置くのは `client_id` / `client_secret` / 更新用のトークンだけである。秘密鍵は置かない。**
// 人間の代理として投稿する経路（user-to-server token）は秘密鍵を1度も使わない。
// 漏れたときの被害は「約6か月・`Issues` の権限の範囲」で止まる（3-82b）。
//
// **アクセストークンはファイルにもメモリにも残さない。**更新用のトークンを1回転させると、
// それまでに配ったアクセストークンは即座に死ぬ（2026-09-09 に実測。回転の直後の
// `GET /user` が 401 を返した）。だから「取る → 使う → 捨てる」しかできない。
//
// **ホームディレクトリは呼ぶ側が渡す。**この package は `os.UserHomeDir()` を呼ばない。
// 呼ぶと、テストが本物の資格情報を読み書きし、本物の更新用のトークンを回す。
package githubapp

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/maimuzo/continuo/internal/atomicfile"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/instance"
	"github.com/maimuzo/continuo/internal/lock"
)

const (
	// CredentialsFileName は資格情報のファイル名である（`~/.continuo/` の下に置く）。
	CredentialsFileName = "github-app-credentials.json"

	// LockFileName は資格情報のロックファイル名である（`~/.continuo/` の下に置く）。
	//
	// **二重起動を止めるロック（`continuo.lock`）とは別のファイルである。**あちらは `--id` で
	// 分かれるが、資格情報は人間1人につき1つの認可なので、`--id` で分けない（3-82b）。
	LockFileName = "github-app-credentials.lock"

	// CredentialsPerm は資格情報のファイルに付ける権限である。
	CredentialsPerm fs.FileMode = 0o600

	// dirPerm は `~/.continuo/` を**新しく作るときに**付ける権限である。
	//
	// **既にあるディレクトリの権限は変えない**（`os.MkdirAll` の振る舞い）。
	// internal/instance の `lockDirPerm` と同じ立場である。
	dirPerm fs.FileMode = 0o700

	// DefaultLockTimeout は資格情報のロックを待つ上限である。
	//
	// **測るまでは60秒を置く**（3-82d「同時に叩かれたとき」）。囲うのは
	// 「読む → GitHub と1往復 → 書き戻す」の全体なので、GitHub への1往復（既定30秒）より長くする。
	DefaultLockTimeout = 60 * time.Second

	// RefreshTokenWarnBefore は更新用のトークンの期限が近いと警告する残り日数である
	// （3-82c「`continuo doctor` が検査すること」）。
	RefreshTokenWarnBefore = 30 * 24 * time.Hour
)

// ErrNotFound は資格情報のファイルが無いことを表す番兵である。
//
// 「読めない」（権限・壊れた JSON）と区別するために置く。呼び出し側は
// errors.Is(err, githubapp.ErrNotFound) で「まだ作っていない」と「壊れている」を言い分ける。
// 起動時の検査はこの2つで出す文面が違う（3-82c）。
//
// **文言は Error() が呼ばれるたびに資源から引く**（i18n.Sentinel。internal/lock と同じ理由）。
var ErrNotFound = i18n.Sentinel(i18n.KeyGitHubAppCredentialsNotFound)

// Credentials は `~/.continuo/github-app-credentials.json` の中身である（3-82b）。
//
//	{
//	  "client_id": "Iv23li…",
//	  "client_secret": "…",
//	  "refresh_token": "ghr_…",
//	  "refresh_token_expires_at": "2027-03-09T00:00:00Z",
//	  "authorized_login": "octocat",
//	  "slug": "continuo-octocat"
//	}
//
// **2回に分けて書かれる。**GitHub App を作った直後は `client_id` / `client_secret` / `slug` だけ、
// 認可を通した直後に残りの3つが入る。**回転のたびに `refresh_token` と
// `refresh_token_expires_at` が書き戻される。**
type Credentials struct {
	// ClientID は GitHub App の client ID である。
	ClientID string
	// ClientSecret は GitHub App の client secret である。**漏れると、更新用のトークンと
	// 組で、人間の代理として動くトークンを約6か月ぶん作り放題になる**（3-82b）。
	ClientSecret string
	// RefreshToken は更新用のトークン（`ghr_` で始まる）である。**1回使うと無効になる。**
	RefreshToken string
	// RefreshTokenExpiresAt は更新用のトークンの期限である。ゼロ値なら未認可。
	//
	// **回転のたびに書き戻す。**書き戻さないと、最初の認可から181日目に、
	// 資格情報が生きているのに全部止まる。
	RefreshTokenExpiresAt time.Time
	// AuthorizedLogin は認可を通したときに引いた `viewer` のログイン名である。
	//
	// **`continuo doctor` と起動時の検査が、`gh api user` と突き合わせる相手である**（3-82f）。
	// これを持たないと、突き合わせのためにトークンを取る（＝更新用のトークンを回す）ことになる。
	AuthorizedLogin string
	// Slug は GitHub App の slug である。install の URL
	// （`https://github.com/apps/<slug>/installations/new`）を組み直すために要る（3-82g）。
	Slug string
}

// credentialsWire はファイルに書く形である。
//
// **時刻は RFC 3339 の文字列で持ち、空なら書かない。**`time.Time` をそのまま書くと、
// 認可前のファイルに `0001-01-01T00:00:00Z` が入り、人間が読んで意味を取れない。
type credentialsWire struct {
	ClientID              string `json:"client_id,omitempty"`
	ClientSecret          string `json:"client_secret,omitempty"`
	RefreshToken          string `json:"refresh_token,omitempty"`
	RefreshTokenExpiresAt string `json:"refresh_token_expires_at,omitempty"`
	AuthorizedLogin       string `json:"authorized_login,omitempty"`
	Slug                  string `json:"slug,omitempty"`
}

// MarshalJSON は Credentials をファイルの形へ直す。
func (c Credentials) MarshalJSON() ([]byte, error) {
	w := credentialsWire{
		ClientID:        c.ClientID,
		ClientSecret:    c.ClientSecret,
		RefreshToken:    c.RefreshToken,
		AuthorizedLogin: c.AuthorizedLogin,
		Slug:            c.Slug,
	}
	if !c.RefreshTokenExpiresAt.IsZero() {
		w.RefreshTokenExpiresAt = c.RefreshTokenExpiresAt.UTC().Format(time.RFC3339)
	}
	return json.Marshal(w)
}

// UnmarshalJSON はファイルの形から Credentials を組み立てる。
//
// **期限の文字列が RFC 3339 として読めなければエラーにする。**黙ってゼロ値にすると、
// 期限内の資格情報を「未認可」と読み違える。
func (c *Credentials) UnmarshalJSON(data []byte) error {
	var w credentialsWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	out := Credentials{
		ClientID:        w.ClientID,
		ClientSecret:    w.ClientSecret,
		RefreshToken:    w.RefreshToken,
		AuthorizedLogin: w.AuthorizedLogin,
		Slug:            w.Slug,
	}
	if w.RefreshTokenExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, w.RefreshTokenExpiresAt)
		if err != nil {
			return i18n.Errorf(i18n.KeyGitHubAppCredentialsBadExpiry, w.RefreshTokenExpiresAt, err)
		}
		out.RefreshTokenExpiresAt = t
	}
	*c = out
	return nil
}

// HasApp は GitHub App を作った直後の段（`client_id` と `client_secret` が揃っている）かを返す。
func (c Credentials) HasApp() bool {
	return c.ClientID != "" && c.ClientSecret != ""
}

// HasRefreshToken は認可を通してある（更新用のトークンを持つ）かを返す。
func (c Credentials) HasRefreshToken() bool {
	return c.RefreshToken != ""
}

// RefreshTokenExpired は更新用のトークンの期限が now を過ぎているかを返す。
//
// **期限を持たない（ゼロ値）ときは切れていないとして扱う。**未認可は HasRefreshToken で見る。
//
// now: いまの時刻。
// 戻り値: 期限を過ぎていれば真。
func (c Credentials) RefreshTokenExpired(now time.Time) bool {
	return !c.RefreshTokenExpiresAt.IsZero() && !now.Before(c.RefreshTokenExpiresAt)
}

// RefreshTokenExpiresSoon は更新用のトークンの残りが RefreshTokenWarnBefore を切っているかを返す。
//
// now: いまの時刻。
// 戻り値: 期限を持ち、残りが30日を切っていれば真（切れていても真）。
func (c Credentials) RefreshTokenExpiresSoon(now time.Time) bool {
	return !c.RefreshTokenExpiresAt.IsZero() && c.RefreshTokenExpiresAt.Sub(now) < RefreshTokenWarnBefore
}

// Store は資格情報の置き場所である。
//
// **ホームディレクトリから作る**（NewStore）。`os.UserHomeDir()` はこの package では呼ばない。
type Store struct {
	// dir は `<ホーム>/.continuo` である。
	dir string
}

// NewStore は資格情報の置き場所を作る。
//
// homeDir: ホームディレクトリの絶対パス。本番は `os.UserHomeDir()` の結果、テストは一時ディレクトリ。
// 戻り値: `<homeDir>/.continuo/` を指す Store。ディレクトリはまだ作らない（Write が作る）。
func NewStore(homeDir string) Store {
	return Store{dir: filepath.Join(homeDir, instance.DirName)}
}

// Dir は資格情報を置くディレクトリ（`~/.continuo`）を返す。
func (s Store) Dir() string { return s.dir }

// Path は資格情報のファイルのパスを返す。
func (s Store) Path() string { return filepath.Join(s.dir, CredentialsFileName) }

// LockPath は資格情報のロックファイルのパスを返す。
func (s Store) LockPath() string { return filepath.Join(s.dir, LockFileName) }

// Read は資格情報を読む。
//
// **権限が 0600 でなくても読む。**止めるかどうかは呼ぶ側が決める（3-82b の表。本体と
// `continuo github-app token` は WARN を1行出して読み、`continuo doctor` は `✗` を出す）。
// だから権限を一緒に返す。
//
// 戻り値の1つ目: 読めた資格情報。
// 戻り値の2つ目: ファイルの権限（`perm.Perm()` 済み）。
// 戻り値の3つ目: ファイルが無ければ ErrNotFound を包んだエラー。読めない・JSON として壊れて
// いる場合はそれぞれのエラー（ErrNotFound を包まない）。
func (s Store) Read() (Credentials, fs.FileMode, error) {
	path := s.Path()
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Credentials{}, 0, i18n.Errorf(i18n.KeyGitHubAppCredentialsNotFoundAt, ErrNotFound, path)
		}
		return Credentials{}, 0, i18n.Errorf(i18n.KeyGitHubAppCredentialsReadFailed, path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, 0, i18n.Errorf(i18n.KeyGitHubAppCredentialsReadFailed, path, err)
	}
	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return Credentials{}, 0, i18n.Errorf(i18n.KeyGitHubAppCredentialsParseFailed, path, err)
	}
	return c, info.Mode().Perm(), nil
}

// Exists は資格情報のファイルが在るかを返す。
//
// 戻り値: 在れば真。在るかどうかを確かめられなかった（権限など）場合はエラー。
func (s Store) Exists() (bool, error) {
	_, err := os.Stat(s.Path())
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, i18n.Errorf(i18n.KeyGitHubAppCredentialsReadFailed, s.Path(), err)
}

// Write は資格情報を書く。
//
// **同じディレクトリの一時ファイルへ書き切ってから差し替える**（CLAUDE.md の
// 「一時ファイルへ書いてから差し替える」）。途中で落ちても、ファイルは「古い内容のまま」か
// 「新しい内容」のどちらかにしかならない。**権限は 0600 で固定する**（continuo が持ち主の
// ファイルなので、元の権限は読まない）。
//
// **`~/.continuo/` が無ければ 0700 で作る。既にあれば権限は変えない。**
//
// **呼ぶ側はロックの中で呼ぶこと**（Lock）。回転の書き戻しとダッシュボードの書き込みが
// 重なると、片方の書き込みが消える。
//
// c: 書く資格情報。
// 戻り値: ディレクトリを作れない・書けない場合のエラー。
func (s Store) Write(c Credentials) error {
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return i18n.Errorf(i18n.KeyGitHubAppCredentialsDirCreateFailed, s.dir, err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return i18n.Errorf(i18n.KeyGitHubAppCredentialsWriteFailed, s.Path(), err)
	}
	data = append(data, '\n')
	if err := atomicfile.Write(s.Path(), data, CredentialsPerm); err != nil {
		return i18n.Errorf(i18n.KeyGitHubAppCredentialsWriteFailed, s.Path(), err)
	}
	return nil
}

// Lock は資格情報のロックを、timeout を上限に待って取る（3-82d「同時に叩かれたとき」）。
//
// **囲うのは「読む → 回す → 書き戻す」の全体である。**取ったトークンを使う段は囲わない
// （1-3 の図。使う段まで囲うと、投稿1件のあいだ他のプロセスが全部待つ）。
//
// **`~/.continuo/` が無ければ 0700 で作る。**ロックファイルは親ディレクトリが無いと開けない。
//
// timeout: 待つ上限。0 以下なら DefaultLockTimeout。
// 戻り値: 取れたロック（呼ぶ側が Release する）。上限まで待っても取れなければエラー。
func (s Store) Lock(timeout time.Duration) (*lock.Lock, error) {
	if timeout <= 0 {
		timeout = DefaultLockTimeout
	}
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return nil, i18n.Errorf(i18n.KeyGitHubAppCredentialsDirCreateFailed, s.dir, err)
	}
	l, err := lock.AcquireWait(s.LockPath(), timeout)
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyGitHubAppLockFailed, s.LockPath(), err)
	}
	return l, nil
}
