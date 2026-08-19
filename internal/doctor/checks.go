package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
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
			Detail: fmt.Sprintf("%s を読めません: %v", path, err),
			Remedies: []string{
				"`continuo init` で雛形を置けます（既にある場合は front matter を直してください）",
			},
		}, loadedConfig{}
	}
	return Result{
		Label:  LabelConfig,
		Symbol: SymbolOK,
		Detail: fmt.Sprintf("%s を読めました（front matter の検証も通りました）", loaded.Path),
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
			Detail: "設定ファイルを読めなかったため、照合する herdr.protocol が決まりません",
		}
	}

	socketPath, err := herdr.ResolveSocketPath(cfg.Config.Herdr.Socket)
	if err != nil {
		return Result{
			Label:    LabelHerdr,
			Symbol:   SymbolMissing,
			Detail:   fmt.Sprintf("socket の場所を決められません: %v", err),
			Remedies: []string{"WORKFLOW.md の herdr.socket に絶対パスを書いてください"},
		}
	}

	client := herdr.New(socketPath, herdr.Timeouts{
		Read: time.Duration(cfg.Config.Claude.ReadTimeoutMs) * time.Millisecond,
	})
	ping, err := client.CheckProtocol(ctx, cfg.Config.Herdr.Protocol)
	if err != nil {
		if timedOut(ctx, err) {
			// **期限切れは「足りない」ではない。**herdr が遅いだけかもしれないので、
			// 終了コードを 1 にせず「確かめられなかった」として残りの検査へ進む。
			return Result{
				Label:    LabelHerdr,
				Symbol:   SymbolUnknown,
				Detail:   fmt.Sprintf("時間内に応答がありませんでした（socket %s）: %v", socketPath, err),
				Remedies: []string{"herdr が応答しているかを確認してから、もう一度実行してください"},
			}
		}
		detail := fmt.Sprintf("%v", err)
		remedy := fmt.Sprintf("herdr が動いていて %s で待ち受けているかを確認してください", socketPath)
		if ping != nil {
			// **protocol が食い違っただけの場合は、socket には届いている。**
			// 直すのは herdr の起動ではなく、continuo と herdr の版の組み合わせである。
			remedy = fmt.Sprintf(
				"WORKFLOW.md の herdr.protocol を %d にするか、herdr を設定に合う版へ更新してください",
				ping.Protocol)
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
		Detail: fmt.Sprintf("protocol %d（設定と一致）／herdr %s／socket %s",
			ping.Protocol, ping.Version, socketPath),
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
			Detail: "設定ファイルを読めなかったため、gh の認証を検査しませんでした",
			Remedies: []string{
				"WORKFLOW.md を直してから `continuo doctor` をもう一度実行してください",
			},
		}
	}
	if err := tracker.CheckGHAvailable(); err != nil {
		return Result{
			Label:    LabelGHAuth,
			Symbol:   SymbolMissing,
			Detail:   fmt.Sprintf("%v", err),
			Remedies: []string{"gh をインストールして PATH に入れてください"},
		}
	}
	if err := tracker.CheckGHProjectScope(ctx, opts.GHAuthStatus); err != nil {
		if timedOut(ctx, err) {
			return Result{
				Label:    LabelGHAuth,
				Symbol:   SymbolUnknown,
				Detail:   fmt.Sprintf("時間内に `gh auth status` の応答がありませんでした: %v", err),
				Remedies: []string{"`gh auth status` が単体で返るかを確認してから、もう一度実行してください"},
			}
		}
		return Result{
			Label:  LabelGHAuth,
			Symbol: SymbolMissing,
			Detail: fmt.Sprintf("%v", err),
			Remedies: []string{
				"`gh auth login -s project` を実行してください" +
					"（既にログイン済みなら `gh auth refresh -h github.com -s project`）",
			},
		}
	}
	return Result{
		Label:  LabelGHAuth,
		Symbol: SymbolOK,
		Detail: "scope に project が含まれる（github.com の有効なアカウント）",
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
			Detail: "設定ファイルを読めなかったため、どの project を見るか決まりません",
		}, nil
	}
	if ghSymbol != SymbolOK {
		// **上流の記号によって文言を分ける。**`✗`（足りない）を「確かめられなかった」と
		// 書くと、人間が直す先を取り違える。
		reason := "gh の認証が通らなかったため、ボードを読みませんでした"
		if ghSymbol == SymbolUnknown {
			reason = "gh の認証を確かめられなかったため、ボードを読みませんでした"
		}
		return Result{Label: LabelBoard, Symbol: SymbolUnknown, Detail: reason}, nil
	}

	token, err := tracker.ResolveToken(ctx, cfg.Config.Tracker.Provider, opts.GHAuthToken)
	if err != nil {
		return Result{
			Label:    LabelBoard,
			Symbol:   SymbolMissing,
			Detail:   fmt.Sprintf("ボードを読むトークンを取り出せません: %v", err),
			Remedies: []string{"WORKFLOW.md の tracker.provider.token_source と token_env を確認してください"},
		}, nil
	}

	adapter, err := tracker.NewAdapter(
		cfg.Config.Tracker, opts.GraphQLEndpoint, token, opts.HTTPClient, opts.Logger, nil)
	if err != nil {
		return Result{
			Label:    LabelBoard,
			Symbol:   SymbolMissing,
			Detail:   fmt.Sprintf("トラッカーのアダプタを組み立てられません: %v", err),
			Remedies: []string{"WORKFLOW.md の tracker の設定を確認してください"},
		}, nil
	}

	if err := adapter.Bootstrap(ctx, cfg.Config.Tracker); err != nil {
		return boardFailure(ctx, "ボードを読めません", err, opts.GraphQLEndpoint), nil
	}

	issues, err := adapter.FetchIssuesByStates(ctx, cfg.Config.Tracker.ActiveStates)
	if err != nil {
		return boardFailure(ctx, "active_states の issue を読めません", err, opts.GraphQLEndpoint), nil
	}

	repos := collectRepos(issues)
	return Result{
		Label:  LabelBoard,
		Symbol: SymbolOK,
		Detail: fmt.Sprintf(
			"%s の project #%d を読めました（Status の選択肢は設定と一致。active_states の issue %d件／対象リポジトリ %d件）%s",
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
	return fmt.Sprintf("（接続先を差し替えています: %s。本番の GitHub ではありません）", endpoint)
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
			Detail:   fmt.Sprintf("%s（時間内に応答がありませんでした）: %v%s", what, err, endpointNote(endpoint)),
			Remedies: []string{"接続を確かめてから `continuo doctor` をもう一度実行してください"},
		}
	}
	if tracker.IsCategory(err, tracker.CategoryRateLimited) {
		return Result{
			Label:    LabelBoard,
			Symbol:   SymbolUnknown,
			Detail:   fmt.Sprintf("%s（レートリミットに当たりました。一時的なものです）: %v", what, err),
			Remedies: []string{"時間をおいてから `continuo doctor` をもう一度実行してください"},
		}
	}
	remedy := "WORKFLOW.md の tracker.provider（owner / project_number / status_field）を確認してください"
	switch {
	case tracker.IsCategory(err, tracker.CategoryInvalidConfig):
		remedy = "GitHub の画面で Status の選択肢名を確認し、WORKFLOW.md の Status 名と合わせてください" +
			"（選択肢の追加・改名は人間が画面から行います）"
	case tracker.IsCategory(err, tracker.CategoryMissingSecret):
		remedy = "ボードを読むトークンが無効か失効しています。" +
			"`gh auth refresh -h github.com -s project` を実行するか（token_source が gh_auth のとき）、" +
			"token_env に指定した環境変数のトークンを入れ直してください"
	}
	return Result{
		Label:    LabelBoard,
		Symbol:   SymbolMissing,
		Detail:   fmt.Sprintf("%s: %v%s", what, err, endpointNote(endpoint)),
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
			Detail: "ボードを読めなかったため、対象のリポジトリを特定できませんでした",
		}, nil
	}
	if len(repos) == 0 {
		// **ボードが空なのは設定の誤りではない**（設計 3-32）。終了コードに影響させない。
		return Result{
			Label:  LabelClone,
			Symbol: SymbolUnknown,
			Detail: "active_states の issue が0件なので、検査する対象がありません",
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
			notes = append(notes, fmt.Sprintf("%s: ghq を実行できませんでした（%v）", repo, err))
		case path == "":
			symbol = worse(symbol, SymbolMissing)
			missing++
			notes = append(notes, fmt.Sprintf("%s が見つからない", repo))
			remedies = append(remedies, fmt.Sprintf("ghq get %s を実行してください", repo))
		default:
			paths[repo.String()] = path
			notes = append(notes, fmt.Sprintf("%s: %s", repo, path))
		}
	}

	return Result{
		Label:  LabelClone,
		Symbol: symbol,
		Detail: countDetail(symbol,
			fmt.Sprintf("対象 %d件がすべて手元にあります", len(repos)),
			fmt.Sprintf("対象 %d件のうち %d件が見つかりません", len(repos), missing),
			fmt.Sprintf("対象 %d件のうち %d件を確かめられませんでした", len(repos), unknown),
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
			return missing + fmt.Sprintf("（ほかに %d件は確かめられませんでした）", unknownCount)
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
			Detail: "ボードを読めなかったため、対象のリポジトリを特定できませんでした",
		}
	}
	if len(repos) == 0 {
		return Result{
			Label:  LabelTrust,
			Symbol: SymbolUnknown,
			Detail: "active_states の issue が0件なので、検査する対象がありません",
		}
	}

	home, err := resolveHomeDir(opts.HomeDir)
	if err != nil {
		return Result{
			Label:  LabelTrust,
			Symbol: SymbolUnknown,
			Detail: fmt.Sprintf("ホームディレクトリを特定できません（%s を読めない）: %v",
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
			notes = append(notes, fmt.Sprintf("%s: clone が無いので信頼登録を確かめられません", repo))
			continue
		}
		trusted, reason, err := workspace.CheckTrustForClonePath(path, home)
		switch {
		case err != nil:
			symbol = worse(symbol, SymbolUnknown)
			unknown++
			notes = append(notes, fmt.Sprintf("%s: 判定できませんでした（%v）", repo, err))
		case !trusted:
			symbol = worse(symbol, SymbolMissing)
			untrusted++
			notes = append(notes, fmt.Sprintf("%s: %s", repo, reason))
			remedies = append(remedies, fmt.Sprintf(
				"%s を Claude Code で一度開き、信頼のダイアログを承認してください", path))
		default:
			notes = append(notes, fmt.Sprintf("%s: %s", repo, reason))
		}
	}

	return Result{
		Label:  LabelTrust,
		Symbol: symbol,
		Detail: countDetail(symbol,
			fmt.Sprintf("対象 %d件がすべて承認済みです", len(repos)),
			fmt.Sprintf("対象 %d件のうち %d件が未承認です", len(repos), untrusted),
			fmt.Sprintf("対象 %d件のうち %d件を確かめられませんでした", len(repos), unknown),
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
//
// **Keychain は読まない**（設計 3-32）。読むと確認の画面が出て、無人のプロセスが固まる。
// macOS では `~/.claude/.credentials.json` が無いのが普通である。
//
// opts: 環境変数を引く関数とホームディレクトリを含む入力。
// cfg: 読めた場合の設定。
// configSymbol: 上流（設定ファイル）の記号。
// 戻り値: 検査結果。
func checkCredentials(opts Options, cfg loadedConfig, configSymbol Symbol) Result {
	if configSymbol != SymbolOK {
		return Result{
			Label:  LabelCredentials,
			Symbol: SymbolUnknown,
			Detail: "rate_limit の設定が読めないので、何を見るべきか決まりません",
			Remedies: []string{
				"設定を直してからもう一度実行してください",
			},
		}
	}

	rl := cfg.Config.RateLimit
	if rl.Source == ratelimit.SourceNone {
		return Result{
			Label:  LabelCredentials,
			Symbol: SymbolOK,
			Detail: "枠の判定を行わない設定です（rate_limit.source: none）。資格情報は要りません",
		}
	}

	if rl.TokenSource == ratelimit.TokenSourceEnv {
		// **`config.Load` が先に弾く経路だが、判定はここにも残す。**この検査は
		// 「設定を読めたあとに、資格情報が実際に取れるか」を見るものであり、
		// 空の環境変数名で先へ進むと、下の LookupEnv が空文字を引いて意味の違う文言になる。
		if rl.TokenEnv == "" {
			return Result{
				Label:    LabelCredentials,
				Symbol:   SymbolMissing,
				Detail:   "rate_limit.token_source が env ですが、rate_limit.token_env が空です",
				Remedies: []string{"WORKFLOW.md の rate_limit.token_env に環境変数名を書いてください"},
			}
		}
		if value, ok := opts.LookupEnv(rl.TokenEnv); ok && value != "" {
			return Result{
				Label:  LabelCredentials,
				Symbol: SymbolOK,
				Detail: fmt.Sprintf("環境変数 %s から取れます（rate_limit.token_source: env）", rl.TokenEnv),
			}
		}
		return Result{
			Label:  LabelCredentials,
			Symbol: SymbolMissing,
			Detail: fmt.Sprintf(
				"環境変数 %s が未設定または空です（rate_limit.token_source が env なので枠の判定ができません）",
				rl.TokenEnv),
			Remedies: []string{fmt.Sprintf("環境変数 %s を設定してください", rl.TokenEnv)},
		}
	}

	// **ここへ来るのは `claude_credentials` のときだけである。**
	// この関数が cfg を見るのは configSymbol が `✓` のとき、つまり config.Load の検証を
	// 通ったときだけで、その検証は rate_limit.token_source を `claude_credentials` か `env` に
	// 限っている（internal/config/validate.go）。**不正値の分岐は到達しないので置かない。**
	home, err := resolveHomeDir(opts.HomeDir)
	if err != nil {
		return Result{
			Label:  LabelCredentials,
			Symbol: SymbolUnknown,
			Detail: fmt.Sprintf("ホームディレクトリを特定できません（%s を探せない）: %v",
				ratelimit.CredentialsRelPath, err),
		}
	}
	path := filepath.Join(home, ratelimit.CredentialsRelPath)
	// **中身は読まない。**在るかどうかだけで判定する（設計 3-32）。
	if _, err := os.Stat(path); err == nil {
		return Result{
			Label:  LabelCredentials,
			Symbol: SymbolOK,
			Detail: fmt.Sprintf("%s があります", path),
		}
	}
	return Result{
		Label:  LabelCredentials,
		Symbol: SymbolUnknown,
		Detail: fmt.Sprintf("%s がありません（macOS では Keychain に入っています）", path),
		Remedies: []string{
			"判定を飛ばしました。continuo の起動には影響しません（Keychain は読みません）",
		},
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
