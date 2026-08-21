package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// checkConfig は設定ファイルを読んで検証する（見出し語 `設定ファイル`）。
//
// **`config.Load` をそのまま呼ぶ。**未知キーと不正値の検出はそこに入っている。
//
// path: 読み込む WORKFLOW.md の絶対パス。
// 戻り値の1つ目: 検査結果。
// 戻り値の2つ目: 読めた場合の設定（読めなければ OK が偽）。
func checkConfig(path string) (Result, loadedConfig) {
	loaded, err := config.Load(path)
	if err != nil {
		return Result{
			Label:  LabelConfig,
			Symbol: SymbolMissing,
			Detail: i18n.T(i18n.KeyDoctorConfigUnreadable, path, err),
			Remedies: []string{
				i18n.T(i18n.KeyDoctorConfigRemedyInit),
			},
		}, loadedConfig{}
	}
	return Result{
		Label:  LabelConfig,
		Symbol: SymbolOK,
		Detail: i18n.T(i18n.KeyDoctorConfigOK, loaded.Path),
	}, loadedConfig{OK: true, Config: loaded.Config}
}

// checkHerdr は herdr の socket の ping を呼び、protocol が設定と一致するかを検査する
// （見出し語 `herdr`）。
//
// **`herdr status` の CLI は使わない**（設計 2-1 / 3-32）。internal/herdr の CheckProtocol を呼ぶ。
//
// ctx: 呼び出しに適用するコンテキスト。
// cfg: 読めた場合の設定。
// configSymbol: 上流（設定ファイル）の記号。
// 戻り値: 検査結果。設定が読めていなければ `!` を返す（照合する protocol が決まらない）。
func checkHerdr(ctx context.Context, cfg loadedConfig, configSymbol Symbol) Result {
	if configSymbol != SymbolOK {
		return Result{
			Label:  LabelHerdr,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorHerdrConfigUnreadable),
		}
	}

	socketPath, err := herdr.ResolveSocketPath(cfg.Config.Herdr.Socket)
	if err != nil {
		return Result{
			Label:    LabelHerdr,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorHerdrSocketUnresolved, err),
			Remedies: []string{i18n.T(i18n.KeyDoctorHerdrRemedySocketAbs)},
		}
	}

	client := herdr.New(socketPath, herdr.Timeouts{
		Read: time.Duration(cfg.Config.Herdr.ReadTimeoutMs) * time.Millisecond,
	})
	ping, err := client.CheckProtocol(ctx, cfg.Config.Herdr.Protocol)
	if err != nil {
		if timedOut(ctx, err) {
			// **期限切れは「足りない」ではない。**herdr が遅いだけかもしれないので、
			// 終了コードを 1 にせず「確かめられなかった」として残りの検査へ進む。
			return Result{
				Label:    LabelHerdr,
				Symbol:   SymbolUnknown,
				Detail:   i18n.T(i18n.KeyDoctorHerdrTimeout, socketPath, err),
				Remedies: []string{i18n.T(i18n.KeyDoctorHerdrRemedyTimeout)},
			}
		}
		detail := fmt.Sprintf("%v", err)
		remedy := i18n.T(i18n.KeyDoctorHerdrRemedyNotListening, socketPath)
		if ping != nil {
			// **protocol が食い違っただけの場合は、socket には届いている。**
			// 直すのは herdr の起動ではなく、continuo と herdr の版の組み合わせである。
			remedy = i18n.T(i18n.KeyDoctorHerdrRemedyProtocol, ping.Protocol)
		}
		return Result{
			Label:    LabelHerdr,
			Symbol:   SymbolMissing,
			Detail:   detail,
			Remedies: []string{remedy},
		}
	}
	return Result{
		Label:  LabelHerdr,
		Symbol: SymbolOK,
		Detail: i18n.T(i18n.KeyDoctorHerdrOK, ping.Protocol, ping.Version, socketPath),
	}
}

// checkGHAuth は `gh` の有無と `gh auth status` の scope を検査する（見出し語 `gh の認証`）。
//
// 読み方は internal/tracker の CheckGHProjectScope が1つに決めてある（設計 3-32）。
// **対象のホストは github.com に固定**し、**`Active account: true` の行を持つブロックだけ**を読み、
// **`Token scopes:` をカンマで区切って前後の空白と引用符を落とし**、**`project` が1つの要素として
// 在ること**を合格とする。`read:project` は不可である。
//
// **設定ファイルの下流である**（設計 3-32 の依存の図）。上流が `✗` か `!` なら
// 検査せずに `!` にして理由を出す。読む値そのものは設定に無い（ホストは github.com に固定）が、
// **依存の図と「上流が `✗` か `!` なら下流を `!` にする」の規則を実装で曲げない。**
//
// ctx: 呼び出しに適用するコンテキスト。
// opts: `gh auth status` の差し替え口を含む入力。
// configSymbol: 上流（設定ファイル）の記号。
// 戻り値: 検査結果。gh が無い場合・未ログインの場合・scope が足りない場合は `✗`。
// 設定ファイルが `✓` でなければ `!`。
func checkGHAuth(ctx context.Context, opts Options, configSymbol Symbol) Result {
	if configSymbol != SymbolOK {
		return Result{
			Label:  LabelGHAuth,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorGHAuthConfigUnreadable),
			Remedies: []string{
				i18n.T(i18n.KeyDoctorGHAuthRemedyFixConfig),
			},
		}
	}
	if err := tracker.CheckGHAvailable(); err != nil {
		return Result{
			Label:    LabelGHAuth,
			Symbol:   SymbolMissing,
			Detail:   fmt.Sprintf("%v", err),
			Remedies: []string{i18n.T(i18n.KeyDoctorGHAuthRemedyInstall)},
		}
	}
	if err := tracker.CheckGHProjectScope(ctx, opts.GHAuthStatus); err != nil {
		if timedOut(ctx, err) {
			return Result{
				Label:    LabelGHAuth,
				Symbol:   SymbolUnknown,
				Detail:   i18n.T(i18n.KeyDoctorGHAuthTimeout, err),
				Remedies: []string{i18n.T(i18n.KeyDoctorGHAuthRemedyTimeout)},
			}
		}
		return Result{
			Label:  LabelGHAuth,
			Symbol: SymbolMissing,
			Detail: fmt.Sprintf("%v", err),
			Remedies: []string{
				i18n.T(i18n.KeyDoctorGHAuthRemedyLogin),
			},
		}
	}
	return Result{
		Label:  LabelGHAuth,
		Symbol: SymbolOK,
		Detail: i18n.T(i18n.KeyDoctorGHAuthOK),
	}
}

// checkBoard はボードを1回読む（見出し語 `ボード`）。
//
// 2つのことを行う。
//
//	1 Bootstrap … project と Status フィールドを解決し、active_states・terminal_states 等の
//	              選択肢名がボード側に全部あるかを照合する。**不一致は `✗`**（巡回が無言で
//	              0件を返す原因になる。設計 3-6 / 3-32）
//	2 候補の取得 … active_states の issue を読み、対象リポジトリを集める
//
// **記号は落ち方で分ける**（設計 3-32）。レートリミットだけ `!`（一時的である）、
// project が見つからない・トークンの取り出しに失敗・選択肢名の不一致は `✗` である。
//
// **信頼の判定関数はアダプタに渡さない。**渡すと候補の取得のたびに issue ごとの ghq と git が
// 走る。doctor はリポジトリ単位で1回ずつ検査する（段6）ので、ここでは要らない。
//
// ctx: 呼び出しに適用するコンテキスト。
// cfg: 読めた場合の設定。
// opts: GraphQL の接続先・HTTP クライアント・`gh auth token` の差し替え口。
// configSymbol: 上流（設定ファイル）の記号。
// ghSymbol: 上流（gh の認証）の記号。
// 戻り値の1つ目: 検査結果。
// 戻り値の2つ目: ボードから集めた対象リポジトリ（読めなければ nil）。
func checkBoard(
	ctx context.Context,
	cfg loadedConfig,
	opts Options,
	configSymbol, ghSymbol Symbol,
) (Result, []Repo) {
	if configSymbol != SymbolOK {
		return Result{
			Label:  LabelBoard,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorBoardConfigUnreadable),
		}, nil
	}
	if ghSymbol != SymbolOK {
		// **上流の記号によって文言を分ける。**`✗`（足りない）を「確かめられなかった」と
		// 書くと、人間が直す先を取り違える。
		reason := i18n.T(i18n.KeyDoctorBoardGHMissing)
		if ghSymbol == SymbolUnknown {
			reason = i18n.T(i18n.KeyDoctorBoardGHUnknown)
		}
		return Result{Label: LabelBoard, Symbol: SymbolUnknown, Detail: reason}, nil
	}

	token, err := tracker.ResolveToken(ctx, cfg.Config.Tracker.Provider, opts.GHAuthToken)
	if err != nil {
		return Result{
			Label:    LabelBoard,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorBoardTokenUnresolved, err),
			Remedies: []string{i18n.T(i18n.KeyDoctorBoardRemedyTokenSource)},
		}, nil
	}

	adapter, err := tracker.NewAdapter(
		cfg.Config.Tracker, opts.GraphQLEndpoint, token, opts.HTTPClient, opts.Logger, nil)
	if err != nil {
		return Result{
			Label:    LabelBoard,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorBoardAdapterFailed, err),
			Remedies: []string{i18n.T(i18n.KeyDoctorBoardRemedyTracker)},
		}, nil
	}

	if err := adapter.Bootstrap(ctx, cfg.Config.Tracker); err != nil {
		return boardFailure(ctx, i18n.T(i18n.KeyDoctorBoardWhatBootstrap), err, opts.GraphQLEndpoint), nil
	}

	issues, err := adapter.FetchIssuesByStates(ctx, cfg.Config.Tracker.ActiveStates)
	if err != nil {
		return boardFailure(ctx, i18n.T(i18n.KeyDoctorBoardWhatFetchIssues), err, opts.GraphQLEndpoint), nil
	}

	repos := collectRepos(issues)
	return Result{
		Label:  LabelBoard,
		Symbol: SymbolOK,
		Detail: i18n.T(i18n.KeyDoctorBoardOK,
			cfg.Config.Tracker.Provider.Owner, cfg.Config.Tracker.Provider.ProjectNumber,
			len(issues), len(repos), endpointNote(opts.GraphQLEndpoint)),
	}, repos
}

// endpointNote は接続先を差し替えているときに添える1行を作る。
//
// **どこへ繋いだのかを必ず出す。**接続先は環境変数1つで差し替わり、そこへ GitHub の
// トークンが送られる。出力に出さないと、本物の GitHub でない宛先に繋いだことに
// 人間が気づけない。
//
// endpoint: 差し替えた接続先（空なら本番の GitHub）。
// 戻り値: 添える文字列（差し替えていなければ空）。
func endpointNote(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	return i18n.T(i18n.KeyDoctorBoardEndpointNote, endpoint)
}

// boardFailure はボードを読めなかったときの結果を組み立てる（設計 3-32 の「落ち方で分ける」）。
//
// **レートリミットだけ `!` にする。**時間をおけば通るので、直すものが無い。
// それ以外（project が見つからない・Status の選択肢名の不一致・通信の失敗）は `✗` である。
//
// **認証の失効も分ける。**トークンが無効・失効しているのに
// 「owner / project_number / status_field を確認してください」と案内すると、
// 人間が直す先を取り違える。
//
// **期限切れも `!` にする。**時間内に応答が無かっただけで、前提が欠けているとは限らない。
//
// ctx: 検査に渡したコンテキスト（期限切れの判定に使う）。
// what: 何をしようとして落ちたかの説明。
// err: 落ちた原因。
// endpoint: 差し替えた接続先（空なら本番の GitHub）。
// 戻り値: 検査結果。
func boardFailure(ctx context.Context, what string, err error, endpoint string) Result {
	if timedOut(ctx, err) {
		return Result{
			Label:    LabelBoard,
			Symbol:   SymbolUnknown,
			Detail:   i18n.T(i18n.KeyDoctorBoardTimeout, what, err, endpointNote(endpoint)),
			Remedies: []string{i18n.T(i18n.KeyDoctorBoardRemedyRetryConnection)},
		}
	}
	if tracker.IsCategory(err, tracker.CategoryRateLimited) {
		return Result{
			Label:    LabelBoard,
			Symbol:   SymbolUnknown,
			Detail:   i18n.T(i18n.KeyDoctorBoardRateLimited, what, err),
			Remedies: []string{i18n.T(i18n.KeyDoctorBoardRemedyWait)},
		}
	}
	remedy := i18n.T(i18n.KeyDoctorBoardRemedyProvider)
	switch {
	case tracker.IsCategory(err, tracker.CategoryInvalidConfig):
		remedy = i18n.T(i18n.KeyDoctorBoardRemedyStatusOptions)
	case tracker.IsCategory(err, tracker.CategoryMissingSecret):
		remedy = i18n.T(i18n.KeyDoctorBoardRemedyTokenInvalid)
	}
	return Result{
		Label:    LabelBoard,
		Symbol:   SymbolMissing,
		Detail:   i18n.T(i18n.KeyDoctorBoardFailed, what, err, endpointNote(endpoint)),
		Remedies: []string{remedy},
	}
}

// checkClone は対象リポジトリの clone が手元にあるかを検査する（見出し語 `clone`）。
//
// **`ghq list -p -e <owner>/<repo>` の出力が空でないかで判定する**（設計 3-6 の3段と同じ呼び方）。
// **exit code は存在の有無にかかわらず 0 を返す**（実測）ので、出力の有無だけを見る。
//
// ctx: 呼び出しに適用するコンテキスト。
// opts: `ghq list` の差し替え口を含む入力。
// repos: ボードから集めた対象リポジトリ。
// boardSymbol: 上流（ボード）の記号。
// 戻り値の1つ目: 検査結果。
// 戻り値の2つ目: リポジトリごとの clone の絶対パス（見つからなかったものは載らない）。
func checkClone(
	ctx context.Context,
	opts Options,
	repos []Repo,
	boardSymbol Symbol,
) (Result, map[string]string) {
	if boardSymbol != SymbolOK {
		return Result{
			Label:  LabelClone,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorCloneBoardUnreadable),
		}, nil
	}
	// **ghq と git が PATH に無ければ、この先を調べても意味が無い。**
	// continuo は worktree を用意するときにこの2つを起動するので、
	// 無いまま段8 へ進むと必ず落ちる。**対象が0件でも先に見る。**段6 の時点ではボードに issue が無いので、
	// ここを後回しにすると段7 まで気づけない。
	for _, bin := range []string{"ghq", "git"} {
		if _, err := exec.LookPath(bin); err != nil {
			return Result{
				Label:    LabelClone,
				Symbol:   SymbolMissing,
				Detail:   i18n.T(i18n.KeyDoctorCloneBinNotFound, bin),
				Remedies: []string{i18n.T(i18n.KeyDoctorCloneRemedyInstallBin, bin)},
			}, nil
		}
	}

	if len(repos) == 0 {
		// **ボードが空なのは設定の誤りではない**（設計 3-32）。終了コードに影響させない。
		return Result{
			Label:  LabelClone,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorCloneNoTargets),
		}, nil
	}

	paths := make(map[string]string, len(repos))
	symbol := SymbolOK
	var notes, remedies []string
	missing, unknown := 0, 0

	for _, repo := range repos {
		path, err := opts.GhqList(ctx, repo.Owner, repo.Name)
		switch {
		case err != nil:
			symbol = worse(symbol, SymbolUnknown)
			unknown++
			notes = append(notes, i18n.T(i18n.KeyDoctorCloneNoteGhqFailed, repo, err))
		case path == "":
			symbol = worse(symbol, SymbolMissing)
			missing++
			notes = append(notes, i18n.T(i18n.KeyDoctorCloneNoteMissing, repo))
			remedies = append(remedies, i18n.T(i18n.KeyDoctorCloneRemedyGhqGet, repo))
		default:
			paths[repo.String()] = path
			notes = append(notes, i18n.T(i18n.KeyDoctorCloneNoteFound, repo, path))
		}
	}

	return Result{
		Label:  LabelClone,
		Symbol: symbol,
		Detail: countDetail(symbol,
			i18n.T(i18n.KeyDoctorCloneDetailOK, len(repos)),
			i18n.T(i18n.KeyDoctorCloneDetailMissing, len(repos), missing),
			i18n.T(i18n.KeyDoctorCloneDetailUnknown, len(repos), unknown),
			unknown),
		Notes:    notes,
		Remedies: remedies,
	}, paths
}

// countDetail は対象が複数ある検査（clone / 信頼登録）の説明を、記号と食い違わない文言で選ぶ。
//
// **記号が `!` のときに「0件が未承認です」のような件数の見出しを出さない。**
// 見出しの行だけを読むと「問題なし」に見えてしまうためである。
// `✗` と `!` が混ざったときは、`✗` の文言に「確かめられなかった件数」を添える。
//
// symbol: その検査の記号（重いほうを採ったあとの値）。
// ok: `✓` のときの説明。
// missing: `✗` のときの説明。
// unknown: `!` のときの説明。
// unknownCount: 確かめられなかった対象の件数（`✗` の説明に添えるかどうかの判定に使う）。
// 戻り値: 説明の1行。
func countDetail(symbol Symbol, ok, missing, unknown string, unknownCount int) string {
	switch symbol {
	case SymbolMissing:
		if unknownCount > 0 {
			return missing + i18n.T(i18n.KeyDoctorCountUnknownSuffix, unknownCount)
		}
		return missing
	case SymbolUnknown:
		return unknown
	default:
		return ok
	}
}

// checkTrust は対象リポジトリが Claude Code に信頼登録されているかを検査する
// （見出し語 `信頼登録`）。
//
// **判定は internal/workspace の CheckTrustForClonePath を呼ぶ。**二重に実装しない。
// **鍵にするのは `ghq list -p -e` が返した clone の絶対パスである**（設計 3-32）。
// worktree のパスでは必ず「未承認」になる。**`~/.claude.json` は読むだけである。**
//
// opts: ホームディレクトリを含む入力。
// repos: ボードから集めた対象リポジトリ。
// clonePaths: リポジトリごとの clone の絶対パス（checkClone の戻り値）。
// boardSymbol: 上流（ボード）の記号。
// 戻り値: 検査結果。
func checkTrust(opts Options, repos []Repo, clonePaths map[string]string, boardSymbol Symbol) Result {
	if boardSymbol != SymbolOK {
		return Result{
			Label:  LabelTrust,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorTrustBoardUnreadable),
		}
	}
	if len(repos) == 0 {
		return Result{
			Label:  LabelTrust,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorTrustNoTargets),
		}
	}

	home, err := resolveHomeDir(opts.HomeDir)
	if err != nil {
		return Result{
			Label:  LabelTrust,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorTrustHomeUnresolved,
				workspace.ClaudeConfigFileName, err),
		}
	}

	symbol := SymbolOK
	var notes, remedies []string
	untrusted, unknown := 0, 0

	for _, repo := range repos {
		path, ok := clonePaths[repo.String()]
		if !ok {
			symbol = worse(symbol, SymbolUnknown)
			unknown++
			notes = append(notes, i18n.T(i18n.KeyDoctorTrustNoteNoClone, repo))
			continue
		}
		trusted, reason, err := workspace.CheckTrustForClonePath(path, home)
		switch {
		case err != nil:
			symbol = worse(symbol, SymbolUnknown)
			unknown++
			notes = append(notes, i18n.T(i18n.KeyDoctorTrustNoteUndecidable, repo, err))
		case !trusted:
			symbol = worse(symbol, SymbolMissing)
			untrusted++
			notes = append(notes, i18n.T(i18n.KeyDoctorTrustNoteReason, repo, reason))
			remedies = append(remedies, i18n.T(i18n.KeyDoctorTrustRemedyRunTrust, path))
		default:
			notes = append(notes, i18n.T(i18n.KeyDoctorTrustNoteReason, repo, reason))
		}
	}

	return Result{
		Label:  LabelTrust,
		Symbol: symbol,
		Detail: countDetail(symbol,
			i18n.T(i18n.KeyDoctorTrustDetailOK, len(repos)),
			i18n.T(i18n.KeyDoctorTrustDetailMissing, len(repos), untrusted),
			i18n.T(i18n.KeyDoctorTrustDetailUnknown, len(repos), unknown),
			unknown),
		Notes:    notes,
		Remedies: remedies,
	}
}

// checkCredentials は枠の判定に使う資格情報を検査する（見出し語 `資格情報`）。
//
// **記号は設定が読めたかどうかで分ける**（設計 3-32 の表）。
//
//	設定が読めない                              … `!`（何を見るべきか決まらない）
//	rate_limit.source が none                   … `✓`（token_source は見ない）
//	token_source が env で環境変数がある         … `✓`
//	token_source が env で環境変数が無い         … `✗`
//	token_source が claude_credentials でファイルがある … `✓`
//	token_source が claude_credentials でファイルが無い … `!`
//	token_source が keychain で読めた            … `✓`
//	token_source が keychain で読めない          … `✗`
//	token_source が keychain で期限内に返らない  … `!`
//
// **`token_source: keychain` のときは Keychain を実際に読む**（読めた項目の名前だけを取る。
// **値は受け取らない**）。読まずに `!` を出すと、macOS の利用者はこの検査から何も得られない。
// **doctor は人間が端末で叩く道具である**ので、確認のダイアログが出ても人間がその場で答えられる。
// **固まらないことは仕組みで保証してある。**この検査には doctor の1項目あたりの期限が掛かり、
// `security` は期限が来た時点で殺される（internal/ratelimit の runSecurity）。
// 期限内に返らなければ `!` にして「ダイアログが出たままかもしれない」と案内する。
// **無人の常駐プロセスでダイアログを出さないための手当ては `continuo allow-keychain-access` である。**
//
// ctx: 呼び出しに適用するコンテキスト（`security` の実行に渡す）。
// opts: 環境変数を引く関数とホームディレクトリを含む入力。
// cfg: 読めた場合の設定。
// configSymbol: 上流（設定ファイル）の記号。
// 戻り値: 検査結果。
func checkCredentials(ctx context.Context, opts Options, cfg loadedConfig, configSymbol Symbol) Result {
	if configSymbol != SymbolOK {
		return Result{
			Label:  LabelCredentials,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorCredentialsConfigUnreadable),
			Remedies: []string{
				i18n.T(i18n.KeyDoctorCredentialsRemedyFixConfig),
			},
		}
	}

	rl := cfg.Config.RateLimit
	if rl.Source == ratelimit.SourceNone {
		return Result{
			Label:  LabelCredentials,
			Symbol: SymbolOK,
			Detail: i18n.T(i18n.KeyDoctorCredentialsNone),
		}
	}

	if rl.TokenSource == ratelimit.TokenSourceKeychain {
		return checkKeychainCredentials(ctx)
	}

	if rl.TokenSource == ratelimit.TokenSourceEnv {
		// **`config.Load` が先に弾く経路だが、判定はここにも残す。**この検査は
		// 「設定を読めたあとに、資格情報が実際に取れるか」を見るものであり、
		// 空の環境変数名で先へ進むと、下の LookupEnv が空文字を引いて意味の違う文言になる。
		if rl.TokenEnv == "" {
			return Result{
				Label:    LabelCredentials,
				Symbol:   SymbolMissing,
				Detail:   i18n.T(i18n.KeyDoctorCredentialsTokenEnvEmpty),
				Remedies: []string{i18n.T(i18n.KeyDoctorCredentialsRemedyTokenEnv)},
			}
		}
		if value, ok := opts.LookupEnv(rl.TokenEnv); ok && value != "" {
			return Result{
				Label:  LabelCredentials,
				Symbol: SymbolOK,
				Detail: i18n.T(i18n.KeyDoctorCredentialsEnvOK, rl.TokenEnv),
			}
		}
		return Result{
			Label:    LabelCredentials,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorCredentialsEnvMissing, rl.TokenEnv),
			Remedies: []string{i18n.T(i18n.KeyDoctorCredentialsRemedySetEnv, rl.TokenEnv)},
		}
	}

	// **ここへ来るのは `claude_credentials` のときだけである。**
	// この関数が cfg を見るのは configSymbol が `✓` のとき、つまり config.Load の検証を
	// 通ったときだけで、その検証は rate_limit.token_source を `claude_credentials` /
	// `keychain` / `env` に限っている（internal/config/validate.go）。
	// **不正値の分岐は到達しないので置かない。**
	home, err := resolveHomeDir(opts.HomeDir)
	if err != nil {
		return Result{
			Label:  LabelCredentials,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorCredentialsHomeUnresolved,
				ratelimit.CredentialsRelPath, err),
		}
	}
	path := filepath.Join(home, ratelimit.CredentialsRelPath)
	// **中身は読まない。**在るかどうかだけで判定する（設計 3-32）。
	if _, err := os.Stat(path); err == nil {
		return Result{
			Label:  LabelCredentials,
			Symbol: SymbolOK,
			Detail: i18n.T(i18n.KeyDoctorCredentialsFileFound, path),
		}
	}
	// **macOS では、このファイルが無いのが普通である。**資格情報は Keychain に入っているので、
	// 「飛ばした」で終わらせず、読める設定へ移る道を出す。
	remedies := []string{i18n.T(i18n.KeyDoctorCredentialsRemedySkipped)}
	if runtime.GOOS == "darwin" {
		remedies = append(remedies, i18n.T(i18n.KeyDoctorCredentialsRemedyUseKeychain))
	}
	return Result{
		Label:    LabelCredentials,
		Symbol:   SymbolUnknown,
		Detail:   i18n.T(i18n.KeyDoctorCredentialsFileMissing, path),
		Remedies: remedies,
	}
}

// checkKeychainCredentials は macOS の Keychain から資格情報を読めるかを検査する。
//
// **読むのは項目の名前だけである。**値（トークン）は受け取らないし、画面にも出さない
// （internal/ratelimit の ProbeKeychain）。
//
// **記号の分け方。**読めたら `✓`、読めたのに accessToken が無い・読めないなら `✗`、
// 期限内に返らなかったら `!` である。`✗` にするのは、利用者が `keychain` を明示して選んだのに
// 取れていない状態だからで、`token_source: env` の環境変数が無いときと同じ扱いにそろえてある。
// **期限切れだけ `!` にする。**返らなかっただけで、資格情報が無いとは限らない。
//
// ctx: 呼び出しに適用するコンテキスト（doctor の1項目あたりの期限が掛かっている）。
// 戻り値: 検査結果。
func checkKeychainCredentials(ctx context.Context) Result {
	probe, err := ratelimit.ProbeKeychain(ctx, 0)
	switch {
	case errors.Is(err, ratelimit.ErrKeychainTimeout):
		return Result{
			Label:    LabelCredentials,
			Symbol:   SymbolUnknown,
			Detail:   i18n.T(i18n.KeyDoctorCredentialsKeychainTimeout, ratelimit.KeychainService, err),
			Remedies: []string{i18n.T(i18n.KeyDoctorCredentialsRemedyKeychainTimeout)},
		}
	case err != nil:
		return Result{
			Label:    LabelCredentials,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorCredentialsKeychainFailed, ratelimit.KeychainService, err),
			Remedies: []string{i18n.T(i18n.KeyDoctorCredentialsRemedyKeychain)},
		}
	case !probe.HasAccessToken:
		return Result{
			Label:    LabelCredentials,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorCredentialsKeychainNoAccessToken, ratelimit.KeychainService),
			Remedies: []string{i18n.T(i18n.KeyDoctorCredentialsRemedyKeychain)},
		}
	default:
		return Result{
			Label:  LabelCredentials,
			Symbol: SymbolOK,
			Detail: i18n.T(i18n.KeyDoctorCredentialsKeychainOK, ratelimit.KeychainService),
		}
	}
}

// resolveHomeDir はホームディレクトリを決める。
//
// configured: Options.HomeDir の値（テストが一時ディレクトリを渡せるようにしてある）。
// 戻り値: ホームディレクトリの絶対パスと、特定できなかった場合のエラー。
func resolveHomeDir(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	return os.UserHomeDir()
}
