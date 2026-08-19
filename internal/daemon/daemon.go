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

	// 段5: 巡回を始める。
	logger.Info("巡回を始めます", "poll_interval_ms", cfg.Polling.IntervalMs)
	runErr := deps.Orchestrator.Run(ctx)

	// 終了の作法。**巡回を止め → hook の受け口を閉じ → turn ループの終了を待つ。**
	// **pane は閉じない**（次の起動で引き継ぐ。設計 3-4 の段5）。
	logger.Info("巡回を止めました（hook の受け口を閉じて turn ループの終了を待ちます）")
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

	return &deps{Herdr: hc, Tracker: adapter, Orchestrator: orc, HookServer: hs}, nil
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
