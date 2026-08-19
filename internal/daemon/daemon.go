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
	loaded, err := config.Load(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("設定ファイルの読み込みに失敗しました（%s）: %w", opts.ConfigPath, err)
	}
	cfg := loaded.Config
	logger.Info("設定ファイルを読み込みました", "path", loaded.Path)

	// **CLI の `--port` は `server.port` を上書きする**（`SPEC.md` 13.7）。
	// 設定を読み直しても待ち受け先が変わらないのと同じ理由で、ここで1回だけ写し取る（設計 3-24）。
	if opts.Port != nil {
		cfg.Server.Port = opts.Port
		logger.Info("CLI の --port でダッシュボードのポートを上書きしました", "port", *opts.Port)
	}

	sockPath, err := socketpath.ResolveHookSocketPath(cfg.Claude.HookBridge.Listen, os.Getenv(EnvRuntimeDir))
	if err != nil {
		return fmt.Errorf("hook を受ける socket の場所を決められません: %w", err)
	}
	if err := socketpath.EnsureDir(filepath.Dir(sockPath)); err != nil {
		return fmt.Errorf("hook を受ける socket のディレクトリを準備できません: %w", err)
	}
	runtimeDir := filepath.Dir(sockPath)
	logger.Info("hook を受ける socket の場所を決めました", "socket", sockPath)

	// 段2: flock を取る。**取れなければ即座に終了する**（設計 3-17）。
	lockPath := ResolveLockFilePath(cfg, sockPath)
	l, err := lock.Acquire(lockPath)
	if err != nil {
		return fmt.Errorf("二重起動を検出しました（ロックファイル %s）: %w", lockPath, err)
	}
	defer func() {
		if err := l.Release(); err != nil {
			logger.Warn("ロックの解放に失敗しました", "error", err)
		}
	}()
	logger.Info("二重起動防止のロックを獲得しました", "lock_file", lockPath)

	deps, err := build(ctx, cfg, loaded.PromptTemplate, sockPath, runtimeDir, opts.ContinuoPath, logger)
	if err != nil {
		return err
	}

	// 段3: 3-6 の起動時検査を全部通す。
	// **ここで落ちて起動を止めるとき、生きている pane は閉じずに放置する。**
	// 落ちる原因は continuo 側の前提が揃っていないことであって、エージェントの側に
	// 問題があるわけではない。**設定の誤りで、動いているエージェントの作業を殺さない。**
	// 人間が直して起動し直せば、復元の段5 で引き継げる。
	if err := runStartupChecks(ctx, cfg, deps, logger); err != nil {
		return fmt.Errorf("起動時の検査に落ちました（生きている pane は閉じずに残します）: %w", err)
	}

	// 段4: 復元（3-4 の段2〜段9）。**巡回より先に終える。**
	restored, err := deps.Orchestrator.Restore(ctx, deps.HookServer)
	if err != nil {
		return fmt.Errorf("復元に失敗しました: %w", err)
	}

	// 段4b: 起動時の掃除。**復元が終わったあとに走らせる**（設計 3-9 の手順6 / 6b）。
	// 先に走らせると、これから引き継ぐ run の branch を孤児と判定して消す。
	deps.Orchestrator.SweepOnStartup(ctx, restored)

	// 段4c: ダッシュボードを開く（設計 5-2 / 8-2。**任意の機能である**）。
	// **`server.port` が null なら deps.Dashboard は nil であり、socket を1つも作らない。**
	// **ここで開いたポートは、設定を読み直しても変わらない**（設計 3-24。自前のリソースを
	// 掴んでいるので、変えるには continuo の再起動が要る）。
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
	// **期限を付ける。**応答を返しきらない相手が居ても、そこで終了が止まらないようにする。
	shutdownCtx, cancelShutdown := context.WithTimeout(
		context.WithoutCancel(ctx), server.DefaultShutdownTimeout)
	if err := deps.Dashboard.Close(shutdownCtx); err != nil {
		logger.Warn("ダッシュボードを閉じられませんでした", "error", err)
	}
	cancelShutdown()
	if err := deps.HookServer.Close(); err != nil {
		logger.Warn("hook の受け口を閉じられませんでした", "error", err)
	}
	deps.Orchestrator.Close()
	logger.Info("走行中の turn ループが終わりました（pane は閉じていません）")

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

// build は依存を組み立てる。**この関数は検査を行わない**（検査は runStartupChecks）。
//
// ctx: トークンの取得に適用するコンテキスト。
// cfg: 検証済みの設定。
// promptTemplate: WORKFLOW.md の本文（1回目のプロンプトのテンプレート）。
// sockPath: 解決済みの hook の socket の絶対パス。
// runtimeDir: 実行時ディレクトリ（`filepath.Dir(sockPath)`）。
// continuoPath: `continuo hook` を起動する実行ファイルのパス。空なら os.Executable()。
// logger: ログの出力先。
// 戻り値: 組み立てた依存と、組み立てに失敗した場合のエラー。
func build(
	ctx context.Context,
	cfg config.Config,
	promptTemplate, sockPath, runtimeDir, continuoPath string,
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

	token, err := tracker.ResolveToken(ctx, cfg.Tracker.Provider, nil)
	if err != nil {
		return nil, fmt.Errorf("ボードを読むためのトークンを取得できません: %w", err)
	}
	adapter, err := tracker.NewAdapter(
		cfg.Tracker, os.Getenv(EnvGraphQLEndpoint), token, nil, logger, ws.TrustFunc())
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

	hs, err := hookserver.New(hookserver.Options{
		SocketPath:  sockPath,
		Sink:        orc,
		Logger:      logger,
		ReadTimeout: time.Duration(cfg.Claude.ReadTimeoutMs) * time.Millisecond,
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
