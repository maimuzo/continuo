// Package doctor は `continuo doctor` の実体である（docs/plans/continuo_design.md 3-32）。
//
// **前提が多くあり、どれが欠けても continuo は静かに失敗する。**機械的に検査して、
// 足りないものと直し方を人間に出すのがこのパッケージの仕事である。
//
// 検査するものと見出し語は report.go の Label 定数で固定する。
// **検査がいくつあるかを、この doc コメントにも文言にも書かない。**
// 数を2箇所に書けば必ずずれる。数えられる場所は Label 定数の一覧だけにする。
//
//	設定ファイル      … WORKFLOW.md が読めて、front matter が検証を通るか
//	片付けの状態      … `cleanup.on_states` が `tracker.terminal_states` に収まっているか
//	未記入の項目      … 雛形にある設定項目が WORKFLOW.md に全部書かれているか
//	claude           … `claude.kind` の実行ファイルが PATH にあるか
//	hook の置き場所    … hook を受ける socket を実際に置けるか
//	Claude の設定      … Claude Code の設定ディレクトリに実際に書けるか
//	worktree の場所    … `workspace.root` に実際に書けるか
//	herdr            … socket の ping の応答の protocol が herdr.protocol と一致するか
//	gh の認証         … `gh auth status` の Token scopes に project が単独で並んでいるか
//	ボード            … Bootstrap が通り、active_states の選択肢名が全部あるか
//	Status の名前      … 設定に書いた Status と紛らわしい選択肢がボードに無いか
//	対応表のキー       … `tracker.automated_state_rewrite` のキーがボードの選択肢にあるか
//	clone            … 対象リポジトリが `ghq list -p -e` で見つかるか
//	信頼登録          … 対象リポジトリの clone のパスが `~/.claude.json` で承認済みか
//	資格情報          … rate_limit の設定に応じて、環境変数・ファイル・Keychain のいずれかから取れるか
//
// **1つ失敗しても残りを全部検査する。**最初の失敗で止めない。
//
// **設定が読めなくても、既定値だけで成立する検査は走らせる**（issue #11）。
// claude の PATH・hook の置き場所・Claude Code の設定ディレクトリの3つがそれである。
// **設定が読めないという理由で全部を `!` にすると、本当の原因を1つも指摘できない。**
//
// **検査の実体は既にあるものを呼ぶ。**gh は internal/tracker の CheckGHAvailable /
// CheckGHProjectScope、herdr は internal/herdr の CheckProtocol、ボードは
// internal/tracker の Bootstrap、clone は internal/workspace の RunGhqList、信頼は
// internal/workspace の CheckTrustForClonePath である。**判定をこのパッケージで書き直さない。**
// internal/daemon の起動時検査（3-6）も同じ関数を呼んでいる。違うのは落ち方だけで、
// 起動時検査は最初の失敗で起動を止め、doctor は全部調べて記号で並べる。
//
// **本番のボードへ書き込む経路は1つも無い。**このパッケージが呼ぶのは読み取りだけである
// （Bootstrap と FetchIssuesByStates）。テストは httptest.Server で立てたテスト用GraphQL mock
// サーバに向けること。
package doctor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// 検査に与える期限である。
//
// **doctor は「前提を機械的に検査する」道具である。**どれか1つが返らないだけで
// 人間の手が止まらないように、全体にも1項目にも上限を置く。
const (
	// DefaultTimeout は検査全体の上限である。
	DefaultTimeout = 30 * time.Second

	// DefaultCheckTimeout は外部に触る検査1つあたりの上限である。
	//
	// **1項目が固まっても残りの検査を捨てない**ために、全体とは別に切る。
	DefaultCheckTimeout = 10 * time.Second
)

// Options は Run の入力である。
//
// **差し替えられる口は、外部のプロセスと外部のサービスに触るものだけである。**
// テストが本物の gh・ghq・GitHub・herdr・ホームディレクトリに触らずに済むようにしてある。
type Options struct {
	// ConfigPath は読み込む WORKFLOW.md の絶対パスである。必須。
	ConfigPath string
	// GraphQLEndpoint は GitHub の GraphQL API の URL である。
	// **空なら本番の GitHub GraphQL API を使う。**テストは httptest.Server の URL を渡すこと。
	GraphQLEndpoint string
	// HomeDir は `~/.claude.json` と `~/.claude/.credentials.json` を探すホームディレクトリである。
	// **`~/.claude/session-env` に書けるかの検査もここを基準にする。**
	// 空なら os.UserHomeDir() の結果を使う。
	//
	// **テストは必ずこれを渡すこと。**本物のホームディレクトリへ書き込みを試すことになる。
	HomeDir string
	// HTTPClient は GraphQL のリクエストを送るクライアントである。nil なら http.DefaultClient。
	HTTPClient *http.Client
	// GHAuthStatus は `gh auth status` を実行する関数である。nil なら本物を実行する。
	GHAuthStatus tracker.GHAuthStatusFunc
	// GHAuthToken は `gh auth token` を実行する関数である。nil なら本物を実行する。
	GHAuthToken tracker.GHAuthTokenFunc
	// GhqList は `ghq list -p -e <owner>/<repo>` を実行する関数である。nil なら本物を実行する。
	GhqList workspace.GhqListFunc
	// LookupEnv は環境変数を引く関数である。nil なら os.LookupEnv を使う。
	// **資格情報の検査（rate_limit.token_source が env のとき）が使う。**
	LookupEnv func(key string) (string, bool)
	// LookPath は実行ファイルを PATH から探す関数である。nil なら exec.LookPath を使う。
	//
	// **テストは必ずこれを渡すこと。**本物を呼ぶと、検査結果が
	// 「テストを走らせたマシンに claude が入っているか」で変わってしまう。
	LookPath func(file string) (string, error)
	// Timeout は検査全体の上限である。**0 なら DefaultTimeout を使う。**
	Timeout time.Duration
	// CheckTimeout は外部に触る検査1つあたりの上限である。
	// **0 なら DefaultCheckTimeout を使う。**
	CheckTimeout time.Duration
	// Logger は検査の途中経過の出力先である。
	// **nil なら何も出力しない。**doctor の出力は Report だけで完結させる
	// （tracker のアダプタが slog.Default() へ出す情報ログが報告に混ざらないようにする）。
	Logger *slog.Logger
}

// Repo は検査の対象になるリポジトリである。
//
// **設定には書かれていない。**ボードに載っている issue の nameWithOwner から集める（3-32）。
type Repo struct {
	// Owner はリポジトリの所有者名である。
	Owner string
	// Name はリポジトリ名である。
	Name string
}

// String は `<owner>/<repo>` の形の文字列を返す。
func (r Repo) String() string { return r.Owner + "/" + r.Name }

// Run は report.go の Label 定数の項目を全部検査して結果を返す（設計 3-32）。
//
// **1つ失敗しても残りを全部検査する。**上流が `✗` か `!` になった検査の下流は、
// 検査せずに `!` にして「なぜ確かめられなかったか」を出す（3-32 の依存の表）。
//
//	設定ファイル ─┬─ 片付けの状態（設定の2つのキーを突き合わせる）
//	              ├─ 未記入の項目（雛形と設定の原文を突き合わせる）
//	              ├─ herdr（設定の protocol と照合する）
//	              └─ gh の認証 ── ボード ─┬─ Status の名前
//	                                      ├─ 対応表のキー
//	                                      ├─ clone
//	                                      └─ 信頼登録
//	資格情報（設定が読めたかどうかだけを見る。飛ばさない）
//
// **この線は設計 3-32 の依存の図そのままである。**`gh の認証` が読む値は設定に無い
// （対象のホストは github.com に固定）が、**依存の図と「上流が `✗` か `!` なら下流は `!`」の
// 規則を実装で曲げない。**設定ファイルが `✗` か `!` なら、`gh の認証` も `!` になる。
//
// ctx: 呼び出しに適用するコンテキスト。
// opts: 設定ファイルのパスと、外部に触る口の差し替え。
// 戻り値: Label 定数と同じ順・同じ数の検査結果。**エラーは返さない。**検査に失敗したこと自体が結果である。
func Run(ctx context.Context, opts Options) Report {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opts.LookupEnv == nil {
		opts.LookupEnv = os.LookupEnv
	}
	if opts.GhqList == nil {
		opts.GhqList = workspace.RunGhqList
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.CheckTimeout <= 0 {
		opts.CheckTimeout = DefaultCheckTimeout
	}
	if opts.HTTPClient == nil {
		// **`http.DefaultClient`（`Timeout` 0）に落とさない。**応答ヘッダを返さない
		// 相手に当たると、doctor がそのまま返らなくなる。
		opts.HTTPClient = &http.Client{Timeout: opts.CheckTimeout}
	}

	// **検査全体にも期限を付ける。**外部プロセス（gh / ghq）と外部サービス（herdr /
	// GitHub）に触るので、期限が無いと道具そのものが固まる。
	ctx, cancelAll := context.WithTimeout(ctx, opts.Timeout)
	defer cancelAll()

	var report Report

	// 段1: 設定ファイル。**ここが落ちても打ち切らない**（設計 3-32 / 08_doctor.md）。
	configResult, cfg := checkConfig(opts.ConfigPath)
	report.add(configResult)

	// 段1b: 片付けの状態。**設定を読むだけで済むので、外へ出る検査より先に置く**（設計 3-9e）。
	// `config.Validate` は `cleanup.on_states` と `tracker.active_states` の重なりしか
	// 見ておらず、**`tracker.terminal_states` との関係は誰も見ていなかった**（issue #35）。
	report.add(checkCleanupStates(cfg, configResult.Symbol))

	// 段1c: 未記入の項目。**ここもボードを1バイトも読まない**（雛形と設定の原文を
	// 突き合わせるだけである）。**版を上げて増えた設定項目は、リリースノートを
	// 読まないかぎり存在に気づけない**（issue #85）。**ここが人間に見せる唯一の場所である。**
	report.add(checkMissingKeys(opts, cfg, configResult.Symbol))

	// 段2: claude。**外部へ接続しないので、いちばん軽い検査である。**
	// **ここで落ちると着手は必ず段10 で失敗する**ので、herdr より前に見せる。
	report.add(checkClaude(opts, cfg, configResult.Symbol))
	// **hook を受ける socket を置けるかを、herdr より先に確かめる。**
	// **これが無かったとき、8項目すべてが ✓ なのに起動だけが落ちた**（issue #9）。
	report.add(checkRuntimeDir(cfg, configResult.Symbol))
	// **Claude Code の設定ディレクトリに書けるかを、設定が読めていなくても確かめる。**
	// Claude Code は SessionStart hook を走らせる前に `~/.claude/session-env/<session_id>/`
	// を作り、continuo はその hook を必ず張る。**ここが書けないと issue は1件も始まらない**
	// のに、doctor はこの場所を一度も見ていなかった（issue #11）。
	report.add(checkClaudeHome(opts))
	// **worktree の置き場所に書けるかも確かめる。**書けないと着手は段3 で必ず落ちる。
	// **置き場所は `workspace.root` にしか書いていないので、設定が読めているときだけ走る。**
	report.add(checkWorkspaceRoot(opts, cfg, configResult.Symbol))

	// 段3: herdr。照合する protocol は設定から来るので、設定が読めなければ確かめられない。
	report.add(withCheckTimeout(ctx, opts.CheckTimeout, func(ctx context.Context) Result {
		return checkHerdr(ctx, cfg, configResult.Symbol)
	}))

	// 段4: gh の認証。設定ファイルの下流である（設計 3-32 の依存の図）。
	ghResult := withCheckTimeout(ctx, opts.CheckTimeout, func(ctx context.Context) Result {
		return checkGHAuth(ctx, opts, configResult.Symbol)
	})
	report.add(ghResult)

	// 段5: ボード。設定と gh の認証の両方が通っていないと読めない。
	// **ここだけ期限を2倍にする。**Bootstrap と候補の取得で2リクエスト送るためである。
	var boardResult Result
	var repos []Repo
	var boardStates []string
	boardResult = withCheckTimeout(ctx, 2*opts.CheckTimeout, func(ctx context.Context) Result {
		var res Result
		res, repos, boardStates = checkBoard(ctx, cfg, opts, configResult.Symbol, ghResult.Symbol)
		return res
	})
	report.add(boardResult)

	// 段5b: Status の名前。**ボードを読んだときの応答を使い回すので、リクエストは増えない。**
	// **Bootstrap は「設定に書いた名前がボードに在るか」しか見ない。**ボードに
	// `In Progress` と `AI In Progress` が並んでいても、片方が設定に在れば通る。
	// **取り違えたまま無人で回すと、人間が作業中の issue にエージェントが着手する。**
	report.add(checkStatusNames(cfg, boardStates, boardResult.Symbol))

	// 段5c: 対応表のキー。**同じ応答を使い回すので、ここでもリクエストは増えない。**
	// **起動時の警告（tracker の missingRewriteKeys）は doctor には出てこない。**
	// doctor は tracker のアダプタへ捨てる logger を渡す（Options.Logger の既定）ので、
	// **この項目が無いと、キーの綴りの打ち間違いを人間に見せる場所が1つも無い**（issue #67）。
	report.add(checkRewriteKeys(cfg, boardStates, boardResult.Symbol))

	// 段6: clone。対象リポジトリはボードを読んで決まる。
	var cloneResult Result
	var clonePaths map[string]string
	cloneResult = withCheckTimeout(ctx, opts.CheckTimeout, func(ctx context.Context) Result {
		var res Result
		res, clonePaths = checkClone(ctx, opts, repos, boardResult.Symbol)
		return res
	})
	report.add(cloneResult)

	// 段7: 信頼登録。**鍵にするのは clone の絶対パスである**（worktree のパスではない。3-32）。
	report.add(checkTrust(opts, repos, clonePaths, boardResult.Symbol))

	// 段8: 資格情報。**上流が落ちても飛ばさない。**設定が読めたかどうかだけで記号を分ける。
	// **期限を切る。**`token_source: keychain` のときは外部コマンド（`security`）を起動し、
	// 確認のダイアログが出たまま誰も答えないと返らないためである。
	report.add(withCheckTimeout(ctx, opts.CheckTimeout, func(ctx context.Context) Result {
		return checkCredentials(ctx, opts, cfg, configResult.Symbol)
	}))

	return report
}

// withCheckTimeout は検査1件に上限を切って走らせる。
//
// **1項目が固まっても残りの検査を捨てないためにある。**期限が来ると、その検査が
// 呼んでいる外部の処理は `context.DeadlineExceeded` で返り、記号は `!`（確かめられ
// なかった）になる（各検査が timedOut で判定する）。
//
// ctx: 呼び出しに適用するコンテキスト。
// timeout: この検査の上限。0 以下なら期限を足さない。
// check: 走らせる検査。
// 戻り値: 検査結果。
func withCheckTimeout(ctx context.Context, timeout time.Duration, check func(context.Context) Result) Result {
	if timeout <= 0 {
		return check(ctx)
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return check(checkCtx)
}

// timedOut は「期限切れで返ってきた」かどうかを判定する。
//
// ctx: 検査に渡したコンテキスト。
// err: 検査が返したエラー。
// 戻り値: 期限切れが原因なら true。
func timedOut(ctx context.Context, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded)
}

// collectRepos はボードから返ってきた issue の nameWithOwner を重複なく集める（設計 3-32）。
//
// **draft issue は対象から外す。**リポジトリを持たないので Owner と Repo が空になり、
// clone も信頼登録も引けない（08_doctor.md の受け入れの基準）。
//
// issues: ボードから返ってきた issue。
// 戻り値: 重複を除いたリポジトリの一覧（`<owner>/<repo>` の昇順。出力の順序を安定させるため）。
func collectRepos(issues []tracker.Issue) []Repo {
	seen := make(map[string]bool)
	var repos []Repo
	for _, issue := range issues {
		if issue.Owner == "" || issue.Repo == "" {
			continue
		}
		r := Repo{Owner: issue.Owner, Name: issue.Repo}
		if seen[r.String()] {
			continue
		}
		seen[r.String()] = true
		repos = append(repos, r)
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].String() < repos[j].String() })
	return repos
}

// loadedConfig は設定ファイルを読めたかどうかと、読めた場合の中身を持つ。
//
// **読めなかった場合に config.Config のゼロ値を配り歩かない。**ゼロ値を配ると、
// 「設定に書かれていない」と「設定を読めていない」を下流が区別できなくなる。
type loadedConfig struct {
	// OK は設定ファイルを読んで検証まで通ったかどうかである。
	OK bool
	// Config は読めた場合の設定である。OK が偽のときは意味を持たない。
	Config config.Config
}
