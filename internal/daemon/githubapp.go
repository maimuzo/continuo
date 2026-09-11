package daemon

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/githubapp"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/tracker"
)

// ProjectURL は GitHub App の manifest の `url` に書く、continuo のリポジトリの URL である
// （docs/plans/impl/issue245_github_app_attribution.md の 3-82g）。ダッシュボードの `/github-app` へ渡す。
const ProjectURL = "https://github.com/maimuzo/continuo"

// RefreshTokenCheckInterval は、走行中に更新用のトークンの残りを見る間隔である（3-82c）。
//
// **1日1回までにする。**巡回の既定は30秒なので、毎巡回で出すと30日で8万行を超え、
// 本当に読みたい WARN が埋もれる。doctor でしか見ないと、doctor を叩かない利用者が
// 181日後に突然止まる。起動時の1回は起動時の検査が出す。
const RefreshTokenCheckInterval = 24 * time.Hour

// portPlaceholder は `server.port` が決まっていないときに、起動を止める文面の URL へ入れる語である。
//
// 起動時の検査はダッシュボードが立つ前に走るので `Addr()` が引けない（3-82c）。
// だから番号を埋められないときは、この語のまま出して、末尾の1行で番号を書かせる。
const portPlaceholder = "<port>"

// githubAppWiring は GitHub App の資格情報に触る3つの経路が共有するものである（3-82f）。
//
// **`NewAdapter` へ渡すトークンを取る関数・ダッシュボードの `GitHubAppOptions`・起動時の検査の
// 3つは、全部この1つの Store から作る。**ホームを差し替える口が2つあると、テストが片方だけを
// 渡したときに、落ちるのではなく「たまたま通る」形で現れる（3-82c が doctor について禁じている）。
type githubAppWiring struct {
	// Store は資格情報の置き場所（`<ホーム>/.continuo/`）である。
	Store githubapp.Store
	// Client は GitHub と往復する口である（テストは httptest.Server へ向ける）。
	Client githubapp.Client
	// GHLogin は `gh api user` を叩く関数である（認可した人との突き合わせに使う。3-82f）。
	GHLogin tracker.GHLoginFunc
}

// checkGitHubAppStartup は `github_app_attribution` が真のとき、GitHub App のトークンが実際に取れることと、
// 認可した人が `gh` の持ち主と同じことを確かめる（3-82c「取れないときに止める」・3-82f）。
//
// **偽なら何もしない。**書かない利用者の continuo は、いままでどおり動く。
//
// **トークンは `Adapter` のメソッドを通してだけ取る**（ProbeAppToken）。取る関数は `NewAdapter` へ
// 渡した1つだけなので、検査が別に持つとテストが片方だけ差し替えて「たまたま通る」形になる。
// **取ると更新用のトークンが1回転する**ので、起動1回につき1回しか取らない。突き合わせのために
// もう1回取ることはしない（`authorized_login` を資格情報から読む）。
//
// **`gh api user` が取れなかったら、突き合わせずに起動する**（3-82f）。このコードベースは
// 「取れなくても止めない」を2箇所で決めており（設計 3-65）、恒久的なずれは次に `gh api` が
// 届いた起動で必ず捕まる。WARN を1行出すだけにする。
//
// ctx: 呼び出しに適用するコンテキスト（runStartupChecks の期限が掛かっている）。
// cfg: 検証済みの設定。
// d: 組み立て済みの依存（`d.Tracker.ProbeAppToken` を呼ぶ）。
// ga: 資格情報の置き場所と `gh api user` を叩く関数。
// now: いまの時刻（更新用のトークンの残りを見る）。
// logger: ログの出力先。
// 戻り値: 起動を止める理由。文面は 3-82c の2通りと 3-82f の1つのいずれか。
func checkGitHubAppStartup(
	ctx context.Context,
	cfg config.Config,
	d *deps,
	ga githubAppWiring,
	now time.Time,
	logger *slog.Logger,
) error {
	if !cfg.Tracker.Comments.GitHubAppAttribution {
		return nil
	}
	port := cfg.Server.Port

	// **在るかどうかを先に見る。**無いときの文面（1通り目）は「作る画面へ行く手順」であり、
	// 回せないときの文面（2通り目）は「認可をやり直す手順」で、混ぜると嘘になる（3-82c）。
	exists, err := ga.Store.Exists()
	if err != nil {
		return err
	}
	if !exists {
		return i18n.Errorf(i18n.KeyDaemonStartupGitHubAppCredentialsMissing, serverPortText(port), serverPortHint(port))
	}

	if err := d.Tracker.ProbeAppToken(ctx); err != nil {
		if errors.Is(err, githubapp.ErrNotFound) {
			return i18n.Errorf(i18n.KeyDaemonStartupGitHubAppCredentialsMissing, serverPortText(port), serverPortHint(port))
		}
		// **`<GitHub が返した error の値>` には、GitHub が断ったなら `error` の値そのもの
		// （`bad_refresh_token` など）を、往復の失敗ならその文言を入れる。**
		reason := err.Error()
		var denied *githubapp.DeniedError
		if errors.As(err, &denied) {
			reason = denied.Code
		}
		return i18n.Errorf(i18n.KeyDaemonStartupGitHubAppRotateFailed,
			reason, serverPortText(port), serverPortText(port), serverPortHint(port))
	}

	// **突き合わせる相手は資格情報の `authorized_login` である。**ここでトークンをもう1回取らない。
	creds, _, err := ga.Store.Read()
	if err != nil {
		return err
	}
	if creds.AuthorizedLogin == "" {
		return i18n.Errorf(i18n.KeyDaemonStartupGitHubAppAuthorizedLoginMissing, serverPortText(port))
	}
	ghLogin, err := ga.GHLogin(ctx)
	switch {
	case err != nil:
		logger.Warn("gh api user が取れないので、認可した人と gh の持ち主を突き合わせずに起動します"+
			"（ずれていれば、次に gh api が届いた起動で止まります）",
			"authorized_login", creds.AuthorizedLogin, "error", err)
	case !strings.EqualFold(ghLogin, creds.AuthorizedLogin):
		// **違ったまま起動させない**（3-82f）。あとで気づくと、その間の run が全部、黙って人間へ渡る。
		// 同じ引数の番号を2回使えない（test/internal/i18n の検査）ので、出る順に6つ渡す。
		return i18n.Errorf(i18n.KeyDaemonStartupGitHubAppLoginMismatch,
			ghLogin, creds.AuthorizedLogin, ghLogin, creds.AuthorizedLogin, creds.AuthorizedLogin, ghLogin)
	}

	warnIfRefreshTokenExpiresSoon(creds, now, logger)
	logger.Info("GitHub App のトークンが取れることを確かめました", "authorized_login", creds.AuthorizedLogin)
	return nil
}

// serverPortText は起動を止める文面の URL に入れるポート番号を返す。
//
// port: `cfg.Server.Port`（CLI の `--port` の上書きは済んでいる）。
// 戻り値: 具体的な番号ならその10進表記。nil か 0 なら `<port>`（末尾の1行で番号を書かせる）。
func serverPortText(port *int) string {
	if port == nil || *port <= 0 {
		return portPlaceholder
	}
	return strconv.Itoa(*port)
}

// serverPortHint は起動を止める文面の末尾に足す、`server.port` についての1行を返す。
//
// **具体的な番号が書いてあるときは足さない。**その人は URL をそのまま開ける。
//
// port: `cfg.Server.Port`。
// 戻り値: nil なら「先に書いてください」、0 なら「具体的な番号にしてください」。空行を1つ挟んで返す。
func serverPortHint(port *int) string {
	switch {
	case port == nil:
		return "\n\n" + i18n.T(i18n.KeyDaemonStartupGitHubAppPortUnset)
	case *port == 0:
		return "\n\n" + i18n.T(i18n.KeyDaemonStartupGitHubAppPortZero)
	default:
		return ""
	}
}

// warnIfRefreshTokenExpiresSoon は更新用のトークンの残りが30日を切っていたら WARN を1行出す（3-82c）。
//
// creds: 読んだ資格情報。
// now: いまの時刻。
// logger: ログの出力先。
func warnIfRefreshTokenExpiresSoon(creds githubapp.Credentials, now time.Time, logger *slog.Logger) {
	if !creds.RefreshTokenExpiresSoon(now) {
		return
	}
	remaining := creds.RefreshTokenExpiresAt.Sub(now)
	logger.Warn("GitHub App の更新用のトークンの期限が近づいています"+
		"（/github-app/authorize で認可をやり直すと、約6か月延びます）",
		"expires_at", creds.RefreshTokenExpiresAt.UTC().Format(time.RFC3339),
		"days_left", int(remaining/(24*time.Hour)))
}

// WatchRefreshTokenExpiry は、走行中に interval ごとに資格情報を読み、更新用のトークンの残りが
// 30日を切っていたら WARN を1行出す（3-82c「更新用のトークンの残りは、起動時と巡回時にも見る」）。
//
// **読むだけで、回さない。**回すと、それまでに配ったアクセストークンが死ぬ（3-82d）。
// **読めなかったときも WARN を1行出して続ける。**走行中に資格情報を消された・壊された場合で、
// 次の投稿が人間の認証で書き直されて断りが付くので、ここでは止めない。
//
// ctx が終わったら止まる。**`Run` は巡回を始める直前にこれを goroutine で起こし、
// 起動時の1回は起動時の検査（checkGitHubAppStartup）が出す。**
//
// ctx: 止めるコンテキスト。
// store: 資格情報の置き場所。
// interval: 見る間隔。0 以下なら RefreshTokenCheckInterval。
// now: いまの時刻を返す関数。nil なら time.Now。
// logger: ログの出力先。nil なら slog.Default()。
func WatchRefreshTokenExpiry(
	ctx context.Context,
	store githubapp.Store,
	interval time.Duration,
	now func() time.Time,
	logger *slog.Logger,
) {
	if interval <= 0 {
		interval = RefreshTokenCheckInterval
	}
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			creds, _, err := store.Read()
			if err != nil {
				logger.Warn("GitHub App の資格情報を読めないので、更新用のトークンの残りを確かめられません",
					"path", store.Path(), "error", err)
				continue
			}
			warnIfRefreshTokenExpiresSoon(creds, now(), logger)
		}
	}
}
