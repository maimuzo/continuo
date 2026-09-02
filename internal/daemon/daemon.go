// Package daemon は continuo の常駐プロセスの結線である
// （docs/plans/continuo_design.md 3-4 の「起動から復元までの順序」）。
//
// **順序が仕様である。**
//
//	1 設定を読んで検証する      … 起動を止める。**pane には触らない**。
//	                             **噛み合っていない Status の集合は、ここで警告だけ出す**（3-9e）
//	2 flock を取る             … 二重起動なので即座に終了する
//	2b 依存を組み立てる          … **ここで外部プロセスを1つ起こす**（`gh auth token`）。
//	                             **必ず期限を掛ける**（掛けないと無言で永久に止まる）
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
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/hookserver"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/lock"
	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/prompt"
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
// **`herdr.read_timeout_ms` を流用しない。**あれは herdr の socket API の応答を待つ
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

	// DefaultHookServerWait は終了時に hook の受け口が閉じ切るのを待つ上限である。
	//
	// **ここに期限が無かった**（`hookserver.Close` を裸で呼んでいた）。受け口の Close は
	// 配送中の goroutine の終了を待つので、hook を1件でも捌き切れない goroutine が
	// 残ると、終了が永久に返らなくなる。
	//
	// **5秒で足りる。**待っている相手は「受け取り済みの hook を印へ書き終えること」であり、
	// ディスクへの追記しか残っていない。
	DefaultHookServerWait = 5 * time.Second

	// ExitInterrupted は後始末を待たずに割り込みで終わったときの終了コードである。
	//
	// **128 + SIGINT(2) である。**シェルが signal で死んだプロセスに付ける値と揃えて、
	// 呼び出し側のスクリプトが「割り込みで終わった」と読めるようにする。
	ExitInterrupted = 130
)

// ShutdownBudget は終了の後始末に掛かりうる最大の時間である。
//
// **3段は直列なので足し算になる**（ダッシュボード → hook の受け口 → turn ループ）。
// 「Ctrl+C を押してから、最悪どれだけ待たされるのか」を人間へ数字で見せるために公開している。
//
// 戻り値: 3段の期限の合計。
func ShutdownBudget() time.Duration {
	return server.DefaultShutdownTimeout + DefaultHookServerWait + DefaultTurnLoopWait
}

// ErrStartup は「起動の段（設定の読み込みから巡回を始めるまで）で落ちた」ことを表す。
//
// **巡回が始まったあとの異常終了と言い分けるためにある。**両方を「起動できません」と
// 記録すると、無人運用のログを後から読む人間が、起動失敗と実行中の異常終了を取り違える。
// 呼び出し側は `errors.Is(err, daemon.ErrStartup)` で切り分けること。
//
// **文言は Error() が呼ばれるたびに資源から引く**（i18n.Sentinel）。
// errors.New に書くと、その文字列は package の初期化の時点で固まる。**言語が決まるのは
// 設定を読んだあと**なので、英語を選んでもこの1文だけ日本語のまま出る。
// **この値は `daemon.run.*` の文言の `%w` に入って画面へ出る**ので、そこだけ日本語になる。
// **errors.Is が見るのはこの変数の identity なので、資源から引いても切り分けは壊れない。**
var ErrStartup = i18n.Sentinel(i18n.KeyDaemonErrStartup)

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
		return i18n.Errorf(i18n.KeyDaemonRunConfigLoadFailed, ErrStartup, opts.ConfigPath, err)
	}
	cfg := loaded.Config
	logger.Info("設定ファイルを読み込みました", "path", loaded.Path)

	// **起動は止めずに、噛み合っていない Status の集合だけを知らせる**（設計 3-9e。issue #35）。
	// **段1 の中に置く。**flock より前なので、二重起動で落ちる経路でも必ず1回出る。
	WarnCleanupStates(cfg, logger)

	// 段1b: 送るプロンプトを組み立て、変数の名前を確かめる（設計 5-3c / 5-3d）。
	frag, err := buildPrompt(loaded, logger)
	if err != nil {
		return err
	}

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

	// **`Prepare` に1本化してある。**
	// 「決める」と「用意する」を別々に呼んでいたときは、その継ぎ目を通すテストが
	// 1本も無く、実在しない `XDG_RUNTIME_DIR` がそのまま通り抜けた（issue #9）。
	sockPath, err := socketpath.Prepare(os.Getenv(EnvRuntimeDir), cfg.Claude.HookBridge.Listen)
	if err != nil {
		return i18n.Errorf(i18n.KeyDaemonRunSocketDirFailed, ErrStartup, err)
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
			return i18n.Errorf(i18n.KeyDaemonRunAlreadyRunning, ErrStartup, lockPath, err)
		}
		return i18n.Errorf(
			i18n.KeyDaemonRunLockFileFailed,
			ErrStartup, lockPath, err)
	}
	defer func() {
		if err := l.Release(); err != nil {
			logger.Warn("ロックの解放に失敗しました", "error", err)
		}
	}()
	logger.Info("二重起動防止のロックを獲得しました", "lock_file", lockPath)

	// 段2b: 依存を組み立てる。**ここで `gh auth token` が走る**（`token_source` の既定は
	// `gh_auth`）。外部プロセスを起こす段なので、起動時検査と同じ期限を掛ける。
	deps, err := build(ctx, cfg, frag, sockPath, runtimeDir, opts.ContinuoPath,
		endpoint, opts.TrackerTimeout, opts.StartupCheckTimeout, logger)
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
		return i18n.Errorf(i18n.KeyDaemonRunStartupChecksFailed, ErrStartup, err)
	}

	// 段4: 復元（3-4 の段2〜段9）。**巡回より先に終える。**
	restored, err := deps.Orchestrator.Restore(ctx, deps.HookServer)
	if err != nil {
		shutdown()
		return i18n.Errorf(i18n.KeyDaemonRunRestoreFailed, ErrStartup, err)
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
	logger.Info("巡回を止めました。後始末を始めます"+
		"（ダッシュボード → hook の受け口 → turn ループの順に閉じます）",
		"max_wait", ShutdownBudget(), "pid", os.Getpid())
	shutdown()

	if runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, context.DeadlineExceeded) {
		return runErr
	}
	return nil
}

// WarnCleanupStates は、片付けを始める Status に「終わったとみなさない Status」が
// 混ざっていたら、起動時に1回だけ警告を出す（設計 3-9e。issue #35）。
//
// **起動を止めない。**止めると、いま動いている人の continuo が版を上げた瞬間に
// 起動しなくなる。報告された `WORKFLOW.md` は `tracker.terminal_states: ["AI Done"]` と
// `cleanup.on_states: ["Done"]` で、**まさにこの検査に引っかかる形である。**
// **壊れるものは無く、片付けの筋が通らないだけである**ので、警告に留める。
//
// **`cleanup.on_states` と `tracker.active_states` の重なりとは扱いが違う。**
// あちらは走っている worktree を消すので、`config.Validate` が起動前に止める。
//
// **どのキーのどの値かを必ず本文に出す。**「食い違っています」とだけ言われても、
// 人間はどの行を直せばよいか分からない（`continuo doctor` の見出し語 `Status の名前` と
// 同じ流儀である）。
//
// cfg: 検証を通った設定。
// logger: ログの出力先。**nil を渡してはならない**（呼び出し元が既に解決している）。
func WarnCleanupStates(cfg config.Config, logger *slog.Logger) {
	outside := config.CleanupStatesOutsideTerminal(cfg)
	if len(outside) == 0 {
		return
	}
	logger.Warn(fmt.Sprintf(
		"cleanup.on_states の %s が tracker.terminal_states にありません"+
			"（終わったとみなさない Status で worktree を片付けます）。"+
			"tracker.terminal_states に %s を足すか、cleanup.on_states から外してください",
		quoteStates(outside), quoteStates(outside)),
		"cleanup.on_states", quoteStates(cfg.Cleanup.OnStates),
		"tracker.terminal_states", quoteStates(cfg.Tracker.TerminalStates))
}

// buildPrompt は、1回目に送る指示書の断片を組み立て、変数の名前を確かめる
// （設計 5-3c / 5-3d）。
//
// **止めるものと、警告に留めるものを分ける。**
//
//	固有のプロンプトが在るのに読めない   … 止める。書いたはずの流儀が効かないまま無人で回る
//	組み込みか固有の変数が誤っている     … 止める。この誤りがあると issue が1件も着手できない
//	WORKFLOW.md の本文の変数が誤っている … 警告に留める。**版を上げた瞬間に起動しなくなるのを避ける**
//
// **本文だけを警告に留める理由。**いままで本文は着手のたびに解釈されており、
// `{{if .attempt}}` の中の誤りは**やり直しが起きるまで表に出なかった。**
// その状態の人が版を上げたときに、いままで動いていた continuo が起動しなくなってはいけない。
// **着手の時点では、いままでどおり失敗する**（renderFirstPrompt が誤りを返す）。
//
// loaded: WORKFLOW.md を読み込んだ結果。
// logger: ログの出力先。**nil を渡してはならない。**
// 戻り値: 組み立てた断片と、起動を止める理由。
func buildPrompt(loaded *config.Loaded, logger *slog.Logger) (prompt.Fragments, error) {
	if loaded.ProjectPromptErr != nil {
		return prompt.Fragments{}, i18n.Errorf(i18n.KeyDaemonRunProjectPromptUnreadable,
			ErrStartup, loaded.ProjectPromptPath, loaded.ProjectPromptErr)
	}

	frag := prompt.Build(
		loaded.PromptTemplate, loaded.ProjectPrompt, loaded.ProjectPromptPath, loaded.ProjectPromptFound)

	if frag.Compat() {
		// **本文が残っている。**組み込みは1文字も送らないので、continuo が仕組みを
		// 直しても、この利用者には届かない。
		logger.Warn(fmt.Sprintf(
			"WORKFLOW.md に本文が %d 行残っているので、組み込みのプロンプトは送りません。"+
				"continuo prompt --show --builtin で組み込みの全文を読み、"+
				"自分で書き足した部分だけを %s へ移してください",
			lineCount(loaded.PromptTemplate), prompt.ProjectFileName),
			"path", loaded.Path)
		if err := frag.Validate(); err != nil {
			// **止めない。**この誤りは版を上げる前から在ったものである。
			logger.Warn(fmt.Sprintf("WORKFLOW.md の本文に誤りがあります: %v", err), "path", loaded.Path)
		}
		return frag, nil
	}

	if err := frag.Validate(); err != nil {
		return prompt.Fragments{}, i18n.Errorf(i18n.KeyDaemonRunPromptInvalid, ErrStartup, err)
	}
	logger.Info("送るプロンプトを組み立てました",
		"project_prompt", loaded.ProjectPromptPath, "project_prompt_found", loaded.ProjectPromptFound)
	return frag, nil
}

// lineCount は、前後の空行を除いた行数を数える。
//
// **警告に出す「本文が n 行残っています」の n である。**
//
// s: 数える文字列。
// 戻り値: 空白だけなら 0、そうでなければ前後の空行を落とした行数。
func lineCount(s string) int {
	s = strings.Trim(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if strings.TrimSpace(s) == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// quoteStates は Status 名の並びを、引用符で囲んで読点でつないだ1つの文字列にする。
//
// **引用符を必ず付ける。**Status 名は空白を含みうる（`In Progress`）ので、
// 裸で並べると、どこまでが1つの名前なのかが読めない。
//
// states: 並べる Status 名。
// 戻り値: `"Done", "In Progress"` の形の文字列（空なら空文字）。
func quoteStates(states []string) string {
	quoted := make([]string, 0, len(states))
	for _, s := range states {
		quoted = append(quoted, fmt.Sprintf("%q", s))
	}
	return strings.Join(quoted, ", ")
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
// **待ちに入る前に、どの段で何秒待つのかを1行ずつ出す。**出さないと、入口の1行のあと
// 最大 ShutdownBudget() のあいだ画面が無反応になり、**止まったのか固まったのかが
// 人間に区別できない。**
//
// **2回呼んでも安全である**（`hookserver.Close` と `server.Close` は閉じ済みを見ている）。
//
// ctx: 呼び出し元のコンテキスト。**キャンセル済みでもよい**（期限は付け直す）。
// logger: ログの出力先。
func (d *deps) close(ctx context.Context, logger *slog.Logger) {
	// 段1: ダッシュボード。**待たずに叩き切る**（読み取り専用なので、途中で切れて
	// 困る書き込みが1つも無い。`server.DefaultShutdownTimeout` を見よ）。
	if d.Dashboard != nil {
		logger.Info("後始末 1/3: ダッシュボードを閉じています"+
			"（処理中の応答をこの時間だけ待ち、過ぎたら接続を切ります）",
			"timeout", server.DefaultShutdownTimeout)
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(
		context.WithoutCancel(ctx), server.DefaultShutdownTimeout)
	if err := d.Dashboard.Close(shutdownCtx); err != nil {
		logger.Warn("ダッシュボードを閉じられませんでした", "error", err)
	}
	cancelShutdown()

	// 段2: hook の受け口。**受け取り済みの hook を印へ書き終えるのを待つ。**
	// ここを待たずに抜けると、Claude Code が送り終えた `Stop` を落としたまま終わり、
	// 次の起動が「turn が終わっていない run」として引き継ぎ直すことになる。
	logger.Info("後始末 2/3: hook の受け口を閉じています"+
		"（受け取り済みの hook を印へ書き終えるまで待ちます）",
		"timeout", DefaultHookServerWait)
	var hookErr error
	if WaitWithTimeout(func() { hookErr = d.HookServer.Close() }, DefaultHookServerWait) {
		if hookErr != nil {
			logger.Warn("hook の受け口を閉じられませんでした", "error", hookErr)
		}
	} else {
		logger.Warn("hook の受け口が期限内に閉じないので待つのをやめます"+
			"（socket のファイルは消してあります）",
			"timeout", DefaultHookServerWait)
	}

	// 段3: turn ループ。**いちばん長い。**相手は Claude Code の1回の応答なので、
	// 送り終えた指示が中途半端に切れないよう、ここだけは長めに待つ。
	logger.Info("後始末 3/3: 走行中の turn ループの終了を待っています"+
		"（送った指示が中途半端に切れないようにするためです）",
		"timeout", DefaultTurnLoopWait)
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

// WatchInterrupt は `SIGINT` / `SIGTERM` を自前で数え、1回目で待たせる理由を出し、
// 2回目で後始末を待たずにプロセスを終わらせる。
//
// **戻り値の stop を呼ぶまで signal の登録は残る。**呼び出し側で `defer` すること。
//
// # なぜ「既定の動作へ戻す」やり方をやめたのか
//
// 以前は `signal.NotifyContext` が返す解除の関数を1回目のあとに呼び、**2回目を
// 既定の動作（プロセスの終了）へ戻していた。**これは効かないことがある。
//
// `signal.Stop` が戻すのは「既定の動作」ではなく、**continuo が起動する前に
// その signal へ設定されていた動作**である。**親が `SIGINT` を無視に設定して
// continuo を起動していると、戻る先が「無視」になり、2回目以降の Ctrl+C は
// 何も起こさない。**`nohup` / `setsid` / job control の無いシェルの
// バックグラウンド起動・一部の supervisor が、この状態を作る。
//
// **自分で数えれば、起動元が何であっても結果は変わらない。**`signal.Notify` は
// 元の動作が無視であっても signal を channel へ届けるので、2回目を必ず捕まえられる。
//
// logger: ログの出力先。nil なら slog.Default()。
// exit: プロセスを終わらせる関数。nil なら os.Exit。**検査はここを差し替える。**
// 戻り値: signal の登録を外す関数。**何回呼んでも安全である。**
func WatchInterrupt(logger *slog.Logger, exit func(int)) func() {
	if logger == nil {
		logger = slog.Default()
	}
	if exit == nil {
		exit = os.Exit
	}
	// **溜められる数に余裕を持たせる。**`signal.Notify` は buffer が埋まっていると
	// signal を捨てる。連打された2回目を捨てると、この仕掛けの意味が無くなる。
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)

	quit := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() { close(quit) })
	}

	go func() {
		defer signal.Stop(ch)
		select {
		case sig := <-ch:
			announceShutdown(logger, sig)
		case <-quit:
			return
		}
		select {
		case sig := <-ch:
			logger.Warn("2回目の割り込みを受けました。後始末を待たずに終了します"+
				"（pane は閉じていません。次の起動で引き継ぎます）",
				"signal", sig.String(), "exit_code", ExitInterrupted)
			exit(ExitInterrupted)
		case <-quit:
		}
	}()
	return stop
}

// announceShutdown は1回目の割り込みで、待たせる理由と次の一手を画面へ出す。
//
// **「何も反応しない」を無くすためにある。**押した直後にここが出るので、
// 受け取ったこと・なぜ待つのか・待ちたくないときにどうするかが、その場で分かる。
//
// logger: ログの出力先。
// sig: 受け取った signal。
func announceShutdown(logger *slog.Logger, sig os.Signal) {
	pid := os.Getpid()
	logger.Warn("割り込みを受けました。走行中の turn ループを壊さないよう、順に閉じてから終わります",
		"signal", sig.String(), "max_wait", ShutdownBudget(), "pid", pid)
	logger.Warn("待ちたくない場合は、もう一度 Ctrl+C を押してください"+
		"（同じ signal をもう一度送っても同じです）。後始末を待たずに即座に終了します",
		"exit_code", ExitInterrupted)
	logger.Warn("それでも終わらない場合は、次のコマンドで全 goroutine のスタックを出して、"+
		"その出力を issue へ貼ってください",
		"command", fmt.Sprintf("kill -QUIT %d", pid))
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
		return i18n.Errorf(i18n.KeyDaemonValidateGraphQLEndpointURLUnparsable, EnvGraphQLEndpoint, raw, err)
	}
	if u.Host == "" {
		return i18n.Errorf(i18n.KeyDaemonValidateGraphQLEndpointHostMissing, EnvGraphQLEndpoint, raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Hostname()) {
			return nil
		}
		return i18n.Errorf(
			i18n.KeyDaemonValidateGraphQLEndpointPlainHTTP, EnvGraphQLEndpoint, raw)
	default:
		return i18n.Errorf(
			i18n.KeyDaemonValidateGraphQLEndpointSchemeNotHTTPS, EnvGraphQLEndpoint, raw)
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
// **ただし外部プロセスは1つ起こす。**`token_source` の既定は `gh_auth` なので、
// ここで `gh auth token` が走る。**その呼び出しには必ず期限を掛ける**
// （tokenTimeout）。掛けないと、`gh` が返らないとき——たとえば Keychain がロックされていて
// 確認のダイアログが出たまま答える人がいないとき——**continuo は何のログも出さずに永久に
// 止まる**（起動時検査にも復元にも巡回にも進まない。flock は握ったままなので、
// 別の端末から起動すると「二重起動」と言われる）。
//
// ctx: 組み立てに適用するコンテキスト。
// cfg: 検証済みの設定。
// frag: 1回目に送る指示書の断片（組み込みの前半・固有・組み込みの後半）。
// sockPath: 解決済みの hook の socket の絶対パス。
// runtimeDir: 実行時ディレクトリ（`filepath.Dir(sockPath)`）。
// continuoPath: `continuo hook` を起動する実行ファイルのパス。空なら os.Executable()。
// graphqlEndpoint: GitHub の GraphQL API の接続先（検査済み）。空なら本番の GitHub。
// trackerTimeout: GraphQL の1リクエストの上限。0 なら DefaultTrackerTimeout。
// tokenTimeout: トークンの取得（`gh auth token`）の上限。0 以下なら
// DefaultStartupCheckTimeout。
// logger: ログの出力先。
// 戻り値: 組み立てた依存と、組み立てに失敗した場合のエラー。
func build(
	ctx context.Context,
	cfg config.Config,
	frag prompt.Fragments,
	sockPath, runtimeDir, continuoPath, graphqlEndpoint string,
	trackerTimeout, tokenTimeout time.Duration,
	logger *slog.Logger,
) (*deps, error) {
	herdrSocket, err := herdr.ResolveSocketPath(cfg.Herdr.Socket)
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyDaemonBuildHerdrSocketUnresolved, err)
	}
	// **Turn は「待ちを伴う1回の呼び出しをどれだけ待つか」である。**
	// `claude.turn_timeout_ms`（画面が変わらないまま待てる時間）をそのまま使う。
	// これより長く待っても、画面が止まっていれば巡回の stall 検知が run を打ち切る。
	hc := herdr.New(herdrSocket, herdr.Timeouts{
		Read:    time.Duration(cfg.Herdr.ReadTimeoutMs) * time.Millisecond,
		Startup: time.Duration(cfg.Herdr.StartupTimeoutMs) * time.Millisecond,
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
		return nil, i18n.Errorf(i18n.KeyDaemonBuildWorkspaceFailed, err)
	}

	// **`gh` の有無は、`gh` を起動する前に見る。**`token_source` の既定は `gh_auth` なので、
	// ここを飛ばすと `gh` が無い環境では「トークンを取得できません」で落ちてしまい、
	// **直し方の書いてある起動時検査（設計 3-6）の文言に辿り着けない。**
	if err := tracker.CheckGHAvailable(); err != nil {
		return nil, err
	}
	// **`gh` が返らないまま無言で止まらないよう、必ず期限を掛ける。**
	if tokenTimeout <= 0 {
		tokenTimeout = DefaultStartupCheckTimeout
	}
	tokenCtx, cancelToken := context.WithTimeout(ctx, tokenTimeout)
	token, err := tracker.ResolveToken(tokenCtx, cfg.Tracker.Provider, nil)
	cancelToken()
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyDaemonBuildTokenFailed, err)
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
		return nil, i18n.Errorf(i18n.KeyDaemonBuildTrackerFailed, err)
	}

	rl, err := ratelimit.NewReader(ratelimit.Options{Config: cfg.RateLimit, Logger: logger})
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyDaemonBuildRateLimitFailed, err)
	}

	orc, err := orchestrator.New(orchestrator.Options{
		Config:         cfg,
		Prompt:         frag,
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
		return nil, i18n.Errorf(i18n.KeyDaemonBuildOrchestratorFailed, err)
	}

	// **`herdr.read_timeout_ms` をここへ流用しない**（設計 8-1）。あれは herdr の
	// socket API の応答を待つ上限であり、hook の接続を掴んでいてよい時間ではない。
	// **`ReadTimeout` を渡さず、hookserver の既定に任せる。**
	hs, err := hookserver.New(hookserver.Options{
		SocketPath: sockPath,
		Sink:       orc,
		Logger:     logger,
	})
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyDaemonBuildHookServerFailed, err)
	}

	// **`server.port` が null なら dash は nil になる**（listen しない。設計 5-2）。
	dash, err := server.New(server.Options{
		Port:   cfg.Server.Port,
		Source: orc,
		Logger: logger,
	})
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyDaemonBuildDashboardFailed, err)
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
