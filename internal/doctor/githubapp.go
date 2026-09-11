package doctor

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/githubapp"
	"github.com/maimuzo/continuo/internal/i18n"
)

// githubAppPortPlaceholder は `server.port` が決まっていないときに、直し方の URL へ入れる語である。
//
// doctor は常駐プロセスの `Addr()` を持たないので、番号を埋められないときはこの語のまま出し、
// 直し方に「server.port を書いて起動する」を1行足す（3-82g）。
const githubAppPortPlaceholder = "<port>"

// checkGitHubApp は GitHub App の資格情報を検査する（見出し語 `GitHub App`。
// docs/plans/impl/issue245_github_app_attribution.md の 3-82c「`continuo doctor` が検査すること」・3-82f）。
//
// **トークンは1度も取らない。回さない。**取ると更新用のトークンが回り、doctor が continuo を
// 起動不能にしうる（回転の書き戻しの直前で落ちれば認可のやり直しになる）。
// 「実際に取れるか」は起動時の検査が受け持つ。
//
//	設定が読めない                                       … `!`（上流が落ちた）
//	github_app_attribution が偽                          … `✓`（GitHub App は使わない）
//	資格情報のファイルが無い                             … `✗`（起動時の文面と同じ4段の手順を直し方に出す）
//	ファイルは在るが読めない（壊れている）               … `✗`（消して作り直す）
//	権限が 0600 でない                                   … `✗`（chmod 600）
//	client_id / client_secret / refresh_token / authorized_login のどれかが欠けている … `✗`（認可をやり直す）
//	更新用のトークンの期限が切れている                   … `✗`（認可をやり直す）
//	認可した人 ≠ `gh api user`                           … `✗`（gh auth switch か、認可のやり直し）
//	`gh api user` が取れない                             … `!`（突き合わせられなかった理由を出す）
//	残りが30日を切っている                               … `!`（期限と残り日数。認可をやり直せば延びる）
//	全部通った                                           … `✓`（認可した人と、更新用のトークンの期限）
//
// **`gh の認証` の下流にはしない。**突き合わせに使う `gh api user` は `gh auth status` と別の
// 呼び出しで、scope が足りなくても持ち主は返る。設定が読めたかどうかだけで記号を分ける。
//
// **`✗` の順序は、直す順序である。**ファイルが無い → 壊れている → 権限 → 欠けている欄 → 期限 →
// 認可した人の順に見て、最初に当たったものだけを出す。同時に2つ出しても、人間は1つずつしか直せない。
//
// ctx: 呼び出しに適用するコンテキスト（`gh api user` の実行に渡す。1項目あたりの期限が掛かっている）。
// opts: ホームディレクトリと `gh api user` の差し替え口を含む入力。
// cfg: 読めた場合の設定。
// configSymbol: 上流（設定ファイル）の記号。
// now: いまの時刻（期限と残り日数を出す）。
// 戻り値: 検査結果。
func checkGitHubApp(ctx context.Context, opts Options, cfg loadedConfig, configSymbol Symbol, now time.Time) Result {
	if configSymbol != SymbolOK {
		return Result{
			Label:    LabelGitHubApp,
			Symbol:   SymbolUnknown,
			Detail:   i18n.T(i18n.KeyDoctorGitHubAppConfigUnreadable),
			Remedies: []string{i18n.T(i18n.KeyDoctorGitHubAppRemedyFixConfig)},
		}
	}
	if !cfg.Config.Tracker.Comments.GitHubAppAttribution {
		return Result{
			Label:  LabelGitHubApp,
			Symbol: SymbolOK,
			Detail: i18n.T(i18n.KeyDoctorGitHubAppDisabled),
		}
	}

	port := cfg.Config.Server.Port
	portText := githubAppPortText(port)

	// **ホームは資格情報の検査（段8）と同じ口から引く**（3-82c）。口が2つあると、テストが
	// 片方だけを渡して「資格情報は一時ディレクトリ、`~/.claude.json` は本物」という混ざった
	// 状態が「たまたま通る」形で現れる。
	home, err := resolveHomeDir(opts.HomeDir)
	if err != nil {
		return Result{
			Label:  LabelGitHubApp,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorGitHubAppReadFailed, err),
		}
	}
	store := githubapp.NewStore(home)

	creds, perm, err := store.Read()
	if err != nil {
		if errors.Is(err, githubapp.ErrNotFound) {
			// **起動を止める文面と同じ4段の手順を出す**（3-82c）。起動しない状態では doctor しか
			// 叩けないので、片方だけに書くと届かない。
			remedies := []string{
				i18n.T(i18n.KeyDoctorGitHubAppRemedyStep1),
				i18n.T(i18n.KeyDoctorGitHubAppRemedyStep2),
				i18n.T(i18n.KeyDoctorGitHubAppRemedyStep3, portText),
				i18n.T(i18n.KeyDoctorGitHubAppRemedyStep4),
			}
			return Result{
				Label:    LabelGitHubApp,
				Symbol:   SymbolMissing,
				Detail:   i18n.T(i18n.KeyDoctorGitHubAppFileMissing, store.Path()),
				Remedies: appendPortRemedy(remedies, port),
			}
		}
		// **在るのに読めない（権限で開けない・JSON として壊れている）。**認可のやり直しでは直らないので、
		// 消して段1（作る）からやり直す。
		return Result{
			Label:    LabelGitHubApp,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorGitHubAppReadFailed, err),
			Remedies: appendPortRemedy([]string{i18n.T(i18n.KeyDoctorGitHubAppRemedyRecreate, store.Path(), portText)}, port),
		}
	}

	if perm != githubapp.CredentialsPerm {
		return Result{
			Label:    LabelGitHubApp,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorGitHubAppPermWrong, store.Path(), perm.String()),
			Remedies: []string{i18n.T(i18n.KeyDoctorGitHubAppRemedyChmod, store.Path())},
		}
	}

	if missing := missingCredentialFields(creds); len(missing) > 0 {
		res := Result{
			Label:    LabelGitHubApp,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorGitHubAppIncomplete, store.Path(), strings.Join(missing, " / ")),
			Remedies: appendPortRemedy([]string{i18n.T(i18n.KeyDoctorGitHubAppRemedyReauthorize, portText)}, port),
		}
		if creds.AuthorizedLogin == "" {
			// **`authorized_login` が欠けていたら、次の行（認可した人の突き合わせ）が比べる相手を失う**（3-82c）。
			res.Notes = []string{i18n.T(i18n.KeyDoctorGitHubAppNoteAuthorizedLogin)}
		}
		return res
	}

	expiresAt := creds.RefreshTokenExpiresAt.UTC().Format(time.RFC3339)
	if creds.RefreshTokenExpired(now) {
		return Result{
			Label:    LabelGitHubApp,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorGitHubAppExpired, expiresAt),
			Remedies: appendPortRemedy([]string{i18n.T(i18n.KeyDoctorGitHubAppRemedyReauthorize, portText)}, port),
		}
	}

	// **認可した人と `gh` の持ち主を突き合わせる**（3-82f）。**トークンは取らない。**
	// 資格情報の `authorized_login` は、認可を通したときに引いた `viewer` のログイン名である。
	ghLogin, err := opts.GHLogin(ctx)
	if err != nil {
		if timedOut(ctx, err) {
			return Result{
				Label:  LabelGitHubApp,
				Symbol: SymbolUnknown,
				Detail: i18n.T(i18n.KeyDoctorGitHubAppGHLoginTimeout, err),
			}
		}
		return Result{
			Label:  LabelGitHubApp,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorGitHubAppGHLoginFailed, creds.AuthorizedLogin, err),
		}
	}
	if !strings.EqualFold(ghLogin, creds.AuthorizedLogin) {
		// **`✗` にする。**違ったまま起動すると、その機械の run が全部、黙って人間へ渡る（3-82f）。
		// 起動時の検査も同じ理由で止める。
		return Result{
			Label:  LabelGitHubApp,
			Symbol: SymbolMissing,
			Detail: i18n.T(i18n.KeyDoctorGitHubAppLoginMismatch, ghLogin, creds.AuthorizedLogin),
			Remedies: appendPortRemedy([]string{
				i18n.T(i18n.KeyDoctorGitHubAppRemedyLoginSwitch, ghLogin, creds.AuthorizedLogin),
				i18n.T(i18n.KeyDoctorGitHubAppRemedyLoginReauthorize, ghLogin, portText),
			}, port),
		}
	}

	if creds.RefreshTokenExpiresSoon(now) {
		// **`!` にする。**動くが、181日目に突然止まる前に知らせる（3-82c）。
		daysLeft := int(creds.RefreshTokenExpiresAt.Sub(now) / (24 * time.Hour))
		return Result{
			Label:    LabelGitHubApp,
			Symbol:   SymbolUnknown,
			Detail:   i18n.T(i18n.KeyDoctorGitHubAppExpiresSoon, expiresAt, daysLeft),
			Remedies: appendPortRemedy([]string{i18n.T(i18n.KeyDoctorGitHubAppRemedyExtend, portText)}, port),
		}
	}

	return Result{
		Label:  LabelGitHubApp,
		Symbol: SymbolOK,
		Detail: i18n.T(i18n.KeyDoctorGitHubAppOK, creds.AuthorizedLogin, expiresAt),
	}
}

// missingCredentialFields は資格情報に欠けている欄の名前を、ファイルの欄名で返す。
//
// **`slug` は数えない。**install の URL を組み直すためだけの欄で、投稿にも突き合わせにも要らない。
//
// c: 読んだ資格情報。
// 戻り値: 欠けている欄の名前（`client_id` / `client_secret` / `refresh_token` / `authorized_login` の順）。
func missingCredentialFields(c githubapp.Credentials) []string {
	var missing []string
	if c.ClientID == "" {
		missing = append(missing, "client_id")
	}
	if c.ClientSecret == "" {
		missing = append(missing, "client_secret")
	}
	if c.RefreshToken == "" {
		missing = append(missing, "refresh_token")
	}
	if c.AuthorizedLogin == "" {
		missing = append(missing, "authorized_login")
	}
	return missing
}

// githubAppPortText は直し方の URL に入れるポート番号を返す。
//
// port: `server.port` の値。
// 戻り値: 具体的な番号ならその10進表記。nil か 0 なら `<port>`。
func githubAppPortText(port *int) string {
	if port == nil || *port <= 0 {
		return githubAppPortPlaceholder
	}
	return strconv.Itoa(*port)
}

// appendPortRemedy は、`server.port` が決まっていないときに、その直し方を1行足す（3-82g）。
//
// **`server.port` を書いていない人は、直し方の URL を開けない。**その人には
// 「`server.port` を書いて起動し、`/github-app` を開いてください」と出す。
//
// remedies: ここまでの直し方。
// port: `server.port` の値。
// 戻り値: nil なら「書いて起動する」、0 なら「具体的な番号にする」を足した直し方。
func appendPortRemedy(remedies []string, port *int) []string {
	switch {
	case port == nil:
		return append(remedies, i18n.T(i18n.KeyDoctorGitHubAppRemedyPortUnset))
	case *port <= 0:
		return append(remedies, i18n.T(i18n.KeyDoctorGitHubAppRemedyPortZero))
	default:
		return remedies
	}
}
