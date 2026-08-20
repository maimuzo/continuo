// Package daemon は continuo の常駐プロセスの結線である
// （docs/plans/continuo_design.md 3-4 の「起動から復元までの順序」）。
//
// **順序が仕様である。**
//
//	1 設定を読んで検証する      … 起動を止める。**pane には触らない**
//	2 flock を取る             … 二重起動なので即座に終了する
//	3 3-6 の起動時検査を全部通す … **起動を止める。生きている pane は閉じずに放置する**
//	4 復元（3-4 の段2〜段9）    … 段ごとの規則に従う
//	4b 起動時の掃除（3-9 の手順6 / 6b）… **復元のあとに走らせる**
//	4c ダッシュボードを開く      … **`server.port` が null なら開かない**（設計 5-2。任意）。
//	                              **開けなくても起動は止めない**（任意の機能の失敗で
//	                              引き継いだ pane を放置しない）
//	5 巡回を始める              … poll_interval_ms ごとに Tick を回す
//
// **巡回より先に復元を終える。**先に巡回を始めると、これから引き継ぐ run の worktree に
// 2つ目の Claude Code が立つ。
//
// **終了の作法。**`SIGINT` / `SIGTERM` を受けたら、**巡回を止め、hook の受け口を閉じ、
// 走行中の turn ループの終了を待ってから抜ける。pane は閉じない**（次の起動で引き継ぐ）。
//
// `cmd/continuo` はこのパッケージを呼ぶだけである（`package main` の非公開関数は
// `test/` から呼べないため、実体をここに置く）。
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/hookserver"
	"github.com/maimuzo/continuo/internal/lock"
	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/internal/server"
	"github.com/maimuzo/continuo/internal/socketpath"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// EnvGraphQLEndpoint は GitHub の GraphQL API の URL を差し替える環境変数である。
//
// **運用者の逃げ道であり、テストの接続先でもある**（`CONTINUO_RUNTIME_DIR` と同じ位置づけ）。
// **空なら本番の GitHub GraphQL API を使う。**設定ファイルには置かない
// （設計 5-2 に無いキーを勝手に足さない）。
const EnvGraphQLEndpoint = "CONTINUO_GITHUB_GRAPHQL_ENDPOINT"

// EnvRuntimeDir は実行時ディレクトリを差し替える環境変数である（設計 3-23 の探索順の1番目）。
const EnvRuntimeDir = "CONTINUO_RUNTIME_DIR"

// 外向きの呼び出しに与える期限である。
//
// **設定ファイルのキーにはしない**（設計 5-2 に無いキーを足さない）。
// **`claude.read_timeout_ms` を流用しない。**あれは herdr の socket API の応答を待つ
// 上限であり（設計 8-1）、相手が違うものを同じつまみで動かすと、herdr が遅い環境に
// 合わせて値を上げたときに GitHub への待ちまで一緒に伸びる。
const (
	// DefaultTrackerTimeout は GitHub の GraphQL API への1リクエストの上限である。
	//
	// **これが無いと巡回ループごと無期限に止まる。**応答ヘッダを返さない相手に当たると、
	// `http.DefaultClient`（`Timeout` 0）では待ち続けてしまう。
	DefaultTrackerTimeout = 30 * time.Second

	// DefaultStartupCheckTimeout は起動時検査（設計 3-6）全体の上限である。
	//
	// `gh` の起動・herdr の socket・GitHub の GraphQL を順に叩くので、
	// **1つが返らないと復元にも巡回にも進めない。**
	DefaultStartupCheckTimeout = 60 * time.Second

	// DefaultTurnLoopWait は終了時に turn ループの終了を待つ上限である。
	//
	// **待ち切れなくても pane は閉じない。**次の起動で復元が引き継ぐ（設計 3-4 の段5）ので、
	// ここで無期限に待つより、期限を切って抜けたほうが運用の妨げにならない。
	DefaultTurnLoopWait = 30 * time.Second
)

// ErrStartup は「起動の段（設定の読み込みから巡回を始めるまで）で落ちた」ことを表す。
//
// **巡回が始まったあとの異常終了と言い分けるためにある。**両方を「起動できません」と
// 記録すると、無人運用のログを後から読む人間が、起動失敗と実行中の異常終了を取り違える。
// 呼び出し側は `errors.Is(err, daemon.ErrStartup)` で切り分けること。
var ErrStartup = errors.New("起動できませんでした")

// Options は Run の入力である。
type Options struct {
	// ConfigPath は読み込む WORKFLOW.md の絶対パスである。必須。
	ConfigPath string
	// Logger はログの出力先である。nil なら slog.Default() を使う。
	Logger *slog.Logger
	// ContinuoPath は `continuo hook` を起動する実行ファイルの絶対パスである。
	// 空なら os.Executable() の結果を使う。
	ContinuoPath string
	// Port は CLI の `--port` で指定されたダッシュボードのポート番号である
	// （`SPEC.md` 13.7 の「CLI `--port` overrides `server.port`」）。
	//
	// **`nil` なら `server.port` に従う。**`nil` でなければ `server.port` を上書きし、
	// 設定に `server.port` が無くてもダッシュボードを開く。
	Port *int
	// StartupCheckTimeout は起動時検査（設計 3-6）全体の上限である。
	// **0 なら DefaultStartupCheckTimeout を使う。**テストが短い期限を与えるための口である。
	StartupCheckTimeout time.Duration
	// TrackerTimeout は GitHub の GraphQL API への1リクエストの上限である。
	// **0 なら DefaultTrackerTimeout を使う。**テストが短い期限を与えるための口である。
	TrackerTimeout time.Duration
}

// Run は continuo の常駐ループを回す。ctx が終わるまで返らない。
//
// ctx: 巡回を止めるコンテキスト。**`SIGINT` / `SIGTERM` で終わるものを渡すこと。**
// opts: 設定ファイルのパスとログの出力先。
// 戻り値: 起動を止めた理由。**正常に終了した場合（ctx のキャンセル）は nil を返す。**
func Run(ctx context.Context, opts Options) error {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// 段1: 設定を読んで検証する。**ここで落ちても pane には触らない**（まだ何も発見していない）。
	//
	// **設定ファイルは起動時に1回だけ読む。**読み直しは実装していない（設計 3-24 が求める
	// 「最後に正常だった設定で動き続ける」仕組みはまだ無い）。編集を反映するには再起動が要る。
	loaded, err := config.Load(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("%w: 設定ファイルの読み込みに失敗しました（%s）: %w", ErrStartup, opts.ConfigPath, err)
	}
	cfg := loaded.Config
	logger.Info("設定ファイルを読み込みました", "path", loaded.Path)

	// **トークンを載せる前に接続先を確かめる**（設計 3-23 の環境変数）。
	// ここを飛ばすと、環境変数に書かれたどんな宛先へも `Authorization: Bearer` が飛ぶ。
	endpoint := os.Getenv(EnvGraphQLEndpoint)
	if err := ValidateGraphQLEndpoint(endpoint); err != nil {
		return fmt.Errorf("%w: %w", ErrStartup, err)
	}
	if endpoint != "" {
		// **差し替えたことを必ず1行残す。**本番のボードへ繋いだのかどうかを、
		// ログを読む人間が判断できるようにする。
		logger.Warn("GitHub の GraphQL の接続先を差し替えています（本番の GitHub ではありません）",
			"env", EnvGraphQLEndpoint, "endpoint", endpoint)
	}

	// **CLI の `--port` は `server.port` を上書きする**（`SPEC.md` 13.7）。
	// **ここで1回だけ写し取る。**設定の読み直しは実装していないので、起動後に
	// `server.port` を書き換えても待ち受け先は変わらない（設計 3-24 の「読み直しても
	// 反映しないもの」に挙がっている3つのうちの1つである）。
	if opts.Port != nil {
		cfg.Server.Port = opts.Port
		logger.Info("CLI の --port でダッシュボードのポートを上書きしました", "port", *opts.Port)
	}

	sockPath, err := socketpath.ResolveHookSocketPath(cfg.Claude.HookBridge.Listen, os.Getenv(EnvRuntimeDir))
	if err != nil {
		return fmt.Errorf("%w: hook を受ける socket の場所を決められません: %w", ErrStartup, err)
	}
	if err := socketpath.EnsureDir(filepath.Dir(sockPath)); err != nil {
		return fmt.Errorf("%w: hook を受ける socket のディレクトリを準備できません: %w", ErrStartup, err)
	}
	runtimeDir := filepath.Dir(sockPath)
	logger.Info("hook を受ける socket の場所を決めました", "socket", sockPath)

	// 段2: flock を取る。**取れなければ即座に終了する**（設計 3-17）。
	lockPath := ResolveLockFilePath(cfg, sockPath)
	l, err := lock.Acquire(lockPath)
	if err != nil {
		// **「二重起動」と「ロックファイルを開けない」を言い分ける。**
		// 両方を二重起動と報告すると、`runtime.lock_file` のパスを打ち間違えた運用者が、
		// 動いてもいない2つ目の continuo を探しに行くことになる。
		if errors.Is(err, lock.ErrAlreadyRunning) {
			return fmt.Errorf("%w: 二重起動を検出しました（ロックファイル %s）: %w", ErrStartup, lockPath, err)
		}
		return fmt.Errorf(
			"%w: ロックファイルを用意できません（runtime.lock_file の指定と、その親ディレクトリの有無・権限を確認してください。%s）: %w",
			ErrStartup, lockPath, err)
	}
	defer func() {
		if err := l.Release(); err != nil {
			logger.Warn("ロックの解放に失敗しました", "error", err)
		}
	}()
	logger.Info("二重起動防止のロックを獲得しました", "lock_file", lockPath)

	deps, err := build(ctx, cfg, loaded.PromptTemplate, sockPath, runtimeDir, opts.ContinuoPath,
		endpoint, opts.TrackerTimeout, logger)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrStartup, err)
	}
	// **組み立てたあとは、どの経路で抜けても同じ手順で閉じる。**早期 return で閉じ忘れると、
	// hook の socket と応答の goroutine が掴まれたまま残る（`deps.close` は2回呼んでも安全である）。
	shutdown := func() { deps.close(ctx, logger) }

	// 段3: 3-6 の起動時検査を全部通す。
	// **ここで落ちて起動を止めるとき、生きている pane は閉じずに放置する。**
	// 落ちる原因は continuo 側の前提が揃っていないことであって、エージェントの側に
	// 問題があるわけではない。**設定の誤りで、動いているエージェントの作業を殺さない。**
	// 人間が直して起動し直せば、復元の段5 で引き継げる。
	if err := runStartupChecks(ctx, cfg, deps, opts.StartupCheckTimeout, logger); err != nil {
		shutdown()
		return fmt.Errorf("%w: 起動時の検査に落ちました（生きている pane は閉じずに残します）: %w", ErrStartup, err)
	}

	// 段4: 復元（3-4 の段2〜段9）。**巡回より先に終える。**
	restored, err := deps.Orchestrator.Restore(ctx, deps.HookServer)
	if err != nil {
		shutdown()
		return fmt.Errorf("%w: 復元に失敗しました: %w", ErrStartup, err)
	}

	// 段4b: 起動時の掃除。**復元が終わったあとに走らせる**（設計 3-9 の手順6 / 6b）。
	// 先に走らせると、これから引き継ぐ run の branch を孤児と判定して消す。
	deps.Orchestrator.SweepOnStartup(ctx, restored)

	// 段4c: ダッシュボードを開く（設計 5-2 / 8-2。**任意の機能である**）。
	// **`server.port` が null なら deps.Dashboard は nil であり、socket を1つも作らない。**
	// **ここで開いたポートは continuo の再起動でしか変わらない。**設定の読み直しは
	// 実装していないうえ、読み直しを入れるとしても `server.port` は反映しない
	// （自前のリソースを掴んでいる。設計 3-24 の「読み直しても反映しないもの」）。
	//
	// **listen に失敗しても起動は止めない。**ダッシュボードは run の面倒を見る仕事に
	// 一切関わらない任意の機能であり（`SPEC.md` 13.7 の「MUST NOT become REQUIRED for
	// orchestrator correctness」）、ここで返ると、**直前の復元で引き継いだ pane の
	// Claude Code が誰にも見張られないまま残る**（hook の受け口も開いたままになる）。
	// 別のアプリが同じポートを掴んでいるだけで、そうなってはならない。
	if deps.Dashboard != nil {
		if err := deps.Dashboard.Start(); err != nil {
			// 待ち受け先はエラーの文言に入っている（`server.Start` が付ける）。
			logger.Warn("ダッシュボードを開けないので、ダッシュボード無しで続けます", "error", err)
		}
	} else {
		logger.Info("ダッシュボードは開きません（server.port が未設定）")
	}

	// 段5: 巡回を始める。
	logger.Info("巡回を始めます", "poll_interval_ms", cfg.Polling.IntervalMs)
	runErr := deps.Orchestrator.Run(ctx)

	// 終了の作法。**ダッシュボードを閉じ → hook の受け口を閉じ → turn ループの終了を待つ。**
	// **pane は閉じない**（次の起動で引き継ぐ。設計 3-4 の段5）。
	// **ダッシュボードを先に閉じる。**閉じかけの状態を人間に見せても意味が無く、
	// 応答の goroutine が orchestrator の写しを取り続ける理由も無い。
	logger.Info("巡回を止めました（hook の受け口を閉じて turn ループの終了を待ちます）")
	shutdown()

	if runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, context.DeadlineExceeded) {
		return runErr
	}
	return nil
}

// deps は組み立てた依存の束である。
type deps struct {
	// Herdr は herdr の socket API のクライアントである。
	Herdr *herdr.Client
	// Tracker は GitHub Projects v2 のアダプタである。
	Tracker *tracker.Adapter
	// Orchestrator は巡回・dispatch・turn ループ・復元である。
	Orchestrator *orchestrator.Orchestrator
	// HookServer は hook を受ける socket である。
	HookServer *hookserver.Server
	// Dashboard は任意の HTTP ダッシュボードである（設計 5-2）。
	// **`server.port` が null なら nil である。**nil のまま Close を呼んでよい。
	Dashboard *server.Server
}

// close は組み立てたものを終了の作法どおりに閉じる（設計 3-4 の段5）。
//
// **順序が仕様である。**ダッシュボード → hook の受け口 → turn ループの終了待ち。
// **ダッシュボードを先に閉じる。**閉じかけの状態を人間に見せても意味が無く、
// 応答の goroutine が orchestrator の写しを取り続ける理由も無い。
//
// **pane は閉じない**（次の起動で引き継ぐ）。
//
// **どの待ちにも期限を付ける。**ダッシュボードだけに期限があって turn ループの待ちが
// 無期限だと、turn ループが1本でも返らなくなった時点で `SIGKILL` でしか止められなくなる。
//
// **2回呼んでも安全である**（`hookserver.Close` と `server.Close` は閉じ済みを見ている）。
//
// ctx: 呼び出し元のコンテキスト。**キャンセル済みでもよい**（期限は付け直す）。
// logger: ログの出力先。
func (d *deps) close(ctx context.Context, logger *slog.Logger) {
	shutdownCtx, cancelShutdown := context.WithTimeout(
		context.WithoutCancel(ctx), server.DefaultShutdownTimeout)
	if err := d.Dashboard.Close(shutdownCtx); err != nil {
		logger.Warn("ダッシュボードを閉じられませんでした", "error", err)
	}
	cancelShutdown()

	if err := d.HookServer.Close(); err != nil {
		logger.Warn("hook の受け口を閉じられませんでした", "error", err)
	}

	if WaitWithTimeout(d.Orchestrator.Close, DefaultTurnLoopWait) {
		logger.Info("走行中の turn ループが終わりました（pane は閉じていません）")
		return
	}
	logger.Warn("走行中の turn ループが期限内に終わらないので待つのをやめます"+
		"（pane は閉じていません。次の起動で引き継ぎます）",
		"timeout", DefaultTurnLoopWait)
}

// WaitWithTimeout は wait が返るのを待ち、期限を過ぎたら待つのをやめる。
//
// **終了処理から無期限の待ちを無くすためにある**（設計 3-4 の終了の作法）。
// 期限切れのときも wait の goroutine は残るが、**呼び出し側はプロセスを終える直前**なので、
// そのまま抜けてよい。
//
// wait: 終わるのを待つ処理（`Orchestrator.Close` など）。
// timeout: 待つ上限。**0 以下なら期限を付けずに待つ。**
// 戻り値: 期限内に wait が返れば true、期限切れなら false。
func WaitWithTimeout(wait func(), timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		wait()
	}()
	if timeout <= 0 {
		<-done
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// RestoreDefaultSignalsOnShutdown は、ctx が終わったら stop を呼んで signal の登録を外す。
//
// **2回目の signal を効かせるためにある。**`signal.NotifyContext` は1回目の signal で
// コンテキストを終わらせるが、**登録は残したまま**なので、そのままだと2回目以降の
// `SIGTERM` / `SIGINT` が buffered channel に吸われて何も起きない。終了処理が長引いたとき、
// 運用者の手段が `SIGKILL` だけになる。登録を外せば、2回目は既定の動作
// （プロセスの終了）に戻る。
//
// **この関数は待たない。**別の goroutine で ctx の終了を待つ。
//
// ctx: `signal.NotifyContext` が返したコンテキスト。
// stop: `signal.NotifyContext` が返した解除の関数。**何回呼んでも安全である。**
func RestoreDefaultSignalsOnShutdown(ctx context.Context, stop func()) {
	go func() {
		<-ctx.Done()
		stop()
	}()
}

// ValidateGraphQLEndpoint は EnvGraphQLEndpoint に書かれた接続先を検査する。
//
// **ここへ `gh auth token` で取ったトークンが `Authorization: Bearer` で送られる。**
// 検査しないと、環境変数を1行足すだけでトークンの送り先を任意の宛先へ変えられる。
//
// 受け付けるのは次の2つだけである。
//
//	https の URL              … 本番の GitHub でも GitHub Enterprise Server でもよい
//	ループバック宛の http     … `127.0.0.1` / `::1` / `localhost`。テストの偽サーバ向け
//
// **ループバック以外の http を拒む。**平文でトークンが流れるためである。
// 設計 3-23 はこの環境変数を「テストの接続先でもある」と定めており、テストは
// `httptest.Server`（`http://127.0.0.1:<ポート>`）を使うので、この2つで足りる。
//
// raw: 環境変数の値。**空なら検査しない**（本番の GitHub GraphQL API を使う）。
// 戻り値: 受け付けられない値の場合のエラー。
func ValidateGraphQLEndpoint(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s の値を URL として解釈できません（%q）: %w", EnvGraphQLEndpoint, raw, err)
	}
	if u.Host == "" {
		return fmt.Errorf("%s の値にホスト名がありません（%q）", EnvGraphQLEndpoint, raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Hostname()) {
			return nil
		}
		return fmt.Errorf(
			"%s には https の URL を書いてください（%q はループバック以外の http なので、"+
				"GitHub のトークンが平文で流れます）", EnvGraphQLEndpoint, raw)
	default:
		return fmt.Errorf(
			"%s の scheme が https ではありません（%q）。https か、ループバック宛の http だけを受け付けます",
			EnvGraphQLEndpoint, raw)
	}
}

// isLoopbackHost はホスト名が手元の機械を指すかどうかを判定する。
//
// host: `url.URL.Hostname()` が返す値（ポート番号を含まない）。
// 戻り値: `localhost` か、ループバックの IP アドレスなら true。
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// build は依存を組み立てる。**この関数は検査を行わない**（検査は runStartupChecks）。
//
// ctx: トークンの取得に適用するコンテキスト。
// cfg: 検証済みの設定。
// promptTemplate: WORKFLOW.md の本文（1回目のプロンプトのテンプレート）。
// sockPath: 解決済みの hook の socket の絶対パス。
// runtimeDir: 実行時ディレクトリ（`filepath.Dir(sockPath)`）。
// continuoPath: `continuo hook` を起動する実行ファイルのパス。空なら os.Executable()。
// graphqlEndpoint: GitHub の GraphQL API の接続先（検査済み）。空なら本番の GitHub。
// trackerTimeout: GraphQL の1リクエストの上限。0 なら DefaultTrackerTimeout。
// logger: ログの出力先。
// 戻り値: 組み立てた依存と、組み立てに失敗した場合のエラー。
func build(
	ctx context.Context,
	cfg config.Config,
	promptTemplate, sockPath, runtimeDir, continuoPath, graphqlEndpoint string,
	trackerTimeout time.Duration,
	logger *slog.Logger,
) (*deps, error) {
	herdrSocket, err := herdr.ResolveSocketPath(cfg.Herdr.Socket)
	if err != nil {
		return nil, fmt.Errorf("herdr の socket の場所を決められません: %w", err)
	}
	hc := herdr.New(herdrSocket, herdr.Timeouts{
		Read:    time.Duration(cfg.Claude.ReadTimeoutMs) * time.Millisecond,
		Startup: time.Duration(cfg.Claude.StartupTimeoutMs) * time.Millisecond,
		Turn:    time.Duration(cfg.Claude.TurnTimeoutMs) * time.Millisecond,
	})

	settingsRoot := filepath.Join(runtimeDir, hookserver.IssuesDirName)
	ws, err := workspace.New(workspace.Options{
		Config:       cfg,
		Herdr:        hc,
		Logger:       logger,
		SettingsRoot: settingsRoot,
	})
	if err != nil {
		return nil, fmt.Errorf("worktree の管理を組み立てられません: %w", err)
	}

	// **`gh` の有無は、`gh` を起動する前に見る。**`token_source` の既定は `gh_auth` なので、
	// ここを飛ばすと `gh` が無い環境では「トークンを取得できません」で落ちてしまい、
	// **直し方の書いてある起動時検査（設計 3-6）の文言に辿り着けない。**
	if err := tracker.CheckGHAvailable(); err != nil {
		return nil, err
	}
	token, err := tracker.ResolveToken(ctx, cfg.Tracker.Provider, nil)
	if err != nil {
		return nil, fmt.Errorf("ボードを読むためのトークンを取得できません: %w", err)
	}
	// **`trust.require_repo_trusted` が偽なら、アダプタにも判定を渡さない。**
	// 渡したままだと、アダプタが `Dispatchable` を偽にして返す一方で
	// `preflight` は検査を飛ばすので、**検査を切ったのに issue が取られず、
	// 理由も issue に残らない**という食い違いになる（設計 3-33）。
	var repoTrusted func(owner, repo string) bool
	if cfg.Trust.RequireRepoTrusted {
		repoTrusted = ws.TrustFunc()
	}
	adapter, err := tracker.NewAdapter(
		cfg.Tracker, graphqlEndpoint, token, newTrackerHTTPClient(trackerTimeout), logger, repoTrusted)
	if err != nil {
		return nil, fmt.Errorf("トラッカーのアダプタを組み立てられません: %w", err)
	}

	rl, err := ratelimit.NewReader(ratelimit.Options{Config: cfg.RateLimit, Logger: logger})
	if err != nil {
		return nil, fmt.Errorf("枠の読み取りを組み立てられません: %w", err)
	}

	orc, err := orchestrator.New(orchestrator.Options{
		Config:         cfg,
		PromptTemplate: promptTemplate,
		Tracker:        adapter,
		Herdr:          hc,
		Workspace:      ws,
		RateLimit:      rl,
		HookSocketPath: sockPath,
		ContinuoPath:   continuoPath,
		Logger:         logger,
		// **巡回ごとの `gh` の認証の検査は `tracker.verify_states_every` の頻度で走る**
		// （毎巡回で外部プロセスを起動しない。設計 3-6）。
		GHAuthCheck: func(ctx context.Context) error { return tracker.CheckGHProjectScope(ctx, nil) },
	})
	if err != nil {
		return nil, fmt.Errorf("orchestrator を組み立てられません: %w", err)
	}

	// **`claude.read_timeout_ms` をここへ流用しない**（設計 8-1）。あれは herdr の
	// socket API の応答を待つ上限であり、hook の接続を掴んでいてよい時間ではない。
	// **`ReadTimeout` を渡さず、hookserver の既定に任せる。**
	hs, err := hookserver.New(hookserver.Options{
		SocketPath: sockPath,
		Sink:       orc,
		Logger:     logger,
	})
	if err != nil {
		return nil, fmt.Errorf("hook の受け口を組み立てられません: %w", err)
	}

	// **`server.port` が null なら dash は nil になる**（listen しない。設計 5-2）。
	dash, err := server.New(server.Options{
		Port:   cfg.Server.Port,
		Source: orc,
		Logger: logger,
	})
	if err != nil {
		return nil, fmt.Errorf("ダッシュボードを組み立てられません: %w", err)
	}

	return &deps{Herdr: hc, Tracker: adapter, Orchestrator: orc, HookServer: hs, Dashboard: dash}, nil
}

// newTrackerHTTPClient は GitHub の GraphQL API を叩くクライアントを作る。
//
// **`http.DefaultClient` を渡さない。**`Timeout` が 0 なので、応答ヘッダを返さない相手に
// 当たると巡回ループごと無期限に止まる（`internal/tracker` の `do` は「呼び出し側が必ず
// 期限を設定すること」と定めている）。
//
// timeout: 1リクエストの上限。0 以下なら DefaultTrackerTimeout を使う。
// 戻り値: 期限を持つ HTTP クライアント。
func newTrackerHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultTrackerTimeout
	}
	return &http.Client{Timeout: timeout}
}

// ResolveLockFilePath は二重起動防止のロックファイルの絶対パスを決める（設計 3-17 / 3-23）。
//
// `runtime.lock_file` が明示されていればそれを使い、無ければ hook の socket と同じ
// ディレクトリ（＝実行時ディレクトリ）に置く。
//
// cfg: 読み込み済みの設定（5-5 の展開を通したもの）。
// sockPath: 解決済みの hook の socket の絶対パス。
// 戻り値: ロックファイルの絶対パス。
func ResolveLockFilePath(cfg config.Config, sockPath string) string {
	if cfg.Runtime.LockFile != nil && *cfg.Runtime.LockFile != "" {
		return *cfg.Runtime.LockFile
	}
	return filepath.Join(filepath.Dir(sockPath), socketpath.LockFileName)
}
