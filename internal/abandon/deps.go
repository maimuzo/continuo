package abandon

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/daemon"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/hookserver"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/instance"
	"github.com/maimuzo/continuo/internal/lock"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// Unlocker は獲得した二重起動防止のロックを手放す口である。
//
// **`*lock.Lock` がこれを満たす。**abandon はロックを「取れたかどうか」を知るために取り、
// **取れたら実行が終わるまで握り続ける。**その場で手放すと、直後に起動した継続監視の
// 足元から worktree を消してしまう（abandon は git と RPC を何度も叩くので窓は秒単位で開く）。
type Unlocker interface {
	// Release はロックを解放する。
	Release() error
}

// Tracker は abandon が使うボードの読み書きの部分集合である。
//
// **`*tracker.Adapter` がこれを満たす。**Status を読む・動かす以外は使わない。
type Tracker interface {
	// Bootstrap は project と Status フィールドの ID を解決する。
	// **UpdateStatus を呼ぶ前に必ず通す。**
	Bootstrap(ctx context.Context, cfg config.TrackerConfig) error
	// FetchIssueByIdentifier は `<owner>/<repo>#<番号>` でボードの issue を1件引く。
	FetchIssueByIdentifier(ctx context.Context, identifier string) (tracker.Issue, bool, error)
	// UpdateStatus は project item の Status を書き換える。
	// **見るのは Reached（目的の Status になったか）だけである。**abandon は issue へ
	// コメントを書かないので、Wrote と Previous は使わない。
	UpdateStatus(ctx context.Context, itemID, targetState string, blockedStates []string) (tracker.StatusWrite, error)
	// VerifyKnownStates は、渡した Status 名がすべてボードの選択肢にあるかを確かめる。
	// **Bootstrap を通してから呼ぶこと。**
	VerifyKnownStates(states []string) error
}

// PaneLister は herdr の pane の一覧を取る部分集合である。
//
// **`*herdr.Client` がこれを満たす。**abandon が herdr へ問い合わせるのは、
// 「その worktree を cwd に持つ pane がまだあるか」の1点だけである。
type PaneLister interface {
	// PaneList は pane の一覧を取る。
	PaneList(ctx context.Context, params herdr.PaneListParams) (*herdr.PaneListResult, error)
}

// Workspace は abandon が使う worktree の走査・検査・片付けの部分集合である。
//
// **`*workspace.Manager` がこれを満たす。**判断を書き直さず、既にあるものを呼ぶ。
type Workspace interface {
	// Scan は置き場所を走査し、身元ファイルを持つ worktree の一覧を返す。
	Scan() ([]workspace.ScannedWorktree, error)
	// ScanUnidentified は置き場所を走査し、**身元ファイルが無いディレクトリ**を返す。
	// **Scan の結果には入らないものである。**着手の途中で落ちるとこの状態ができるので、
	// 「判断できないもの」として数える（issue #27）。
	ScanUnidentified() ([]string, error)
	// Inspect は消さずに、失われるものだけを調べる。
	Inspect(ctx context.Context, req workspace.CleanupRequest) (*workspace.Leftover, error)
	// Cleanup は worktree・pane・herdr workspace・branch を片付ける。
	Cleanup(ctx context.Context, req workspace.CleanupRequest) (*workspace.CleanupResult, error)
	// IdentityPath は worktree の身元ファイルの絶対パスを返す。
	IdentityPath(worktreePath string) string
	// OwnerRepoOf は worktree のパスから owner とリポジトリ名を取り出す。
	// **身元ファイルを読まない**（エージェントが書き換えられるため）。
	OwnerRepoOf(worktreePath string) (string, string, error)
	// ExpectedSlugFor は、その issue の worktree が置かれるはずの最下層のディレクトリ名を返す。
	// **拾った候補の裏を取るために使う**（既定の branch_template ではここに issue 番号が入る）。
	ExpectedSlugFor(issue workspace.IssueRef) (string, error)
	// FindIssueBranch は、issue に対応する branch が clone に残っていないかを調べる。
	// **worktree が1つも無いときに使う**（issue #27）。1文字も書き換えない。
	FindIssueBranch(ctx context.Context, issue workspace.IssueRef) (workspace.IssueBranch, error)
	// DeleteIssueBranch は FindIssueBranch が実在すると答えた branch を消す。
	DeleteIssueBranch(ctx context.Context, branch workspace.IssueBranch) error
}

// Deps は abandon が外部へ繋ぐ処理である。
//
// **ゼロ値のフィールドは設定から本物を組み立てる**（resolve）。
// 検査は差し替えたいものだけを埋めればよい。**この形にしてあるのは、
// 本番のボード・本物の herdr・利用者の worktree に触らずに検査するためである。**
type Deps struct {
	// LockPath は二重起動防止のロックファイルの絶対パスである。
	// **空なら常駐している側と同じ関数から決める**（internal/instance の Layout）。
	LockPath string
	// BoardLockPath はボード1枚ぶんのロックファイルの絶対パスである（設計 3-17e）。
	//
	// **空なら `tracker.provider` から決める**（常駐している側と同じ
	// `instance.BoardLockPath`）。**`--id` に依らない唯一の合図である。**
	// `--id` を付け忘れて叩かれたとき、動いている continuo を「止まっている」と
	// 判定しないために見る。
	BoardLockPath string
	// AcquireLock はロックを試みる。nil なら internal/lock を呼ぶ。
	//
	// **取れたら continuo は動いていない。**`lock.ErrAlreadyRunning` を包んだエラーが
	// 返れば動いている。それ以外のエラーはロックファイルそのものを開けないことを表す。
	//
	// **取れたロックは実行の最後まで握る。**握っている間に起動しようとした継続監視は
	// 「既に起動しています」で止まる。消されるより望ましい。
	//
	// **`--dry-run` では呼ばない**（ReadLockState を見よ）。
	AcquireLock func(path string) (Unlocker, error)
	// ReadLockState は覚え書きを読んで、そのロックを握っている continuo が生きているかを
	// 答える。nil なら internal/instance の ReadLockState。
	//
	// **`--dry-run` はこちらを使う。**`AcquireLock` は `O_CREATE` でロックファイルを作り、
	// その前に置き場所も作らせる。**README は「`--dry-run` は何も書かない」と約束している。**
	//
	// **flock には触らない**（設計 3-17i）。一瞬でも掴むと、**その瞬間に起動した
	// continuo が「二重起動」で落ちる。**
	//
	// **答えは4値である。**「読めなかった」を持たないと、書けなかった覚え書きが
	// 「動いていない」に丸められ、**生きている continuo の worktree を消しにいく。**
	ReadLockState func(path string) (instance.LockState, instance.LockInfo, error)
	// InstanceID は `--id` に渡された名前である（**既定なら空文字**）。
	//
	// **ロックの覚え書きに書く**（設計 3-17i）。書かないと、誰が握っているかを読んだ人が
	// **どの `--id` を止めればよいのか分からない。**
	InstanceID string
	// Herdr は pane の一覧を取る口である。nil なら設定から本物を組み立てる。
	Herdr PaneLister
	// Workspace は worktree の走査・検査・片付けである。nil なら設定から本物を組み立てる。
	Workspace Workspace
	// NewTracker はボードのアダプタを作る。nil なら本物を組み立てる（`gh` のトークンを引く）。
	//
	// **遅延して呼ぶ。**ボードを読まずに済む実行（worktree が無かった場合）で、
	// `gh` を起動して API 枠を使わないためである。
	NewTracker func(ctx context.Context) (Tracker, error)
	// Now は現在時刻を返す。nil なら time.Now。
	Now func() time.Time
	// Sleep は pane が閉じるのを待つあいだの待機である。nil なら time.Sleep 相当。
	// **ctx が終わったら偽を返すこと。**
	Sleep func(ctx context.Context, d time.Duration) bool
}

// resolve は埋まっていないフィールドを、読み込み済みの設定から本物で埋める。
//
// **ここで組み立てるのは、設定を読めたあとでなければ作れないものだけである。**
// herdr の socket も worktree の置き場所も設定から決まる。
//
// **`dryRun` が真なら、ディレクトリもファイルも1つも作らない**（設計 3-17g）。
// **これが無かったとき、`continuo abandon --dry-run --id typo` は
// `~/.continuo/id/typo/run`・`~/.continuo/board`・`<workspace.root>/typo` と
// その直下の名乗りを作ったうえで「--dry-run なので何も消していません」と表示していた。**
// **とくに名乗りが残ると、打ち間違えた `--id` の置き場所が既定側の走査から永久に隠れる。**
//
// cfg: 検証済みの設定（**`--id` の Apply を通したもの**）。
// inst: `--id` から導いた置き場所。**常駐している側と同じ Layout である**（3-17c）。
// endpoint: GitHub の GraphQL API の接続先（検査済み）。空なら本番の GitHub。
// dryRun: `--dry-run` かどうか。**真なら置き場所を1つも作らない。**
// logger: ログの出力先。
// 戻り値: すべてのフィールドが埋まった Deps と、組み立てに失敗した場合のエラー。
func (d Deps) resolve(
	cfg config.Config,
	inst instance.Layout,
	endpoint string,
	dryRun bool,
	logger *slog.Logger,
) (Deps, error) {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Sleep == nil {
		d.Sleep = sleepUntil
	}
	if d.AcquireLock == nil {
		d.AcquireLock = func(path string) (Unlocker, error) { return lock.Acquire(path) }
	}
	if d.ReadLockState == nil {
		d.ReadLockState = instance.ReadLockState
	}
	if d.InstanceID == "" {
		d.InstanceID = inst.ID()
	}

	// **herdr のクライアントは1つだけ作り、pane の一覧と片付けの両方に渡す。**
	// 2つ作ると、同じ socket への接続が2本になるだけで得るものが無い。
	var client *herdr.Client
	needClient := d.Herdr == nil || d.Workspace == nil
	if needClient {
		socket, err := herdr.ResolveSocketPath(cfg.Herdr.Socket)
		if err != nil {
			return d, i18n.Errorf(i18n.KeyAbandonHerdrSocketUnresolved, err)
		}
		client = herdr.New(socket, herdr.Timeouts{
			Read:    time.Duration(cfg.Herdr.ReadTimeoutMs) * time.Millisecond,
			Startup: time.Duration(cfg.Herdr.StartupTimeoutMs) * time.Millisecond,
			Turn:    time.Duration(cfg.Claude.TurnTimeoutMs) * time.Millisecond,
		})
	}
	if d.Herdr == nil {
		d.Herdr = client
	}
	if d.Workspace == nil {
		// **issue ごとの設定ファイルの置き場所は、常駐プロセスと同じ決め方にする。**
		// 違う値を渡すと、片付けが `settings_path` を「置き場所の外側」と判定して消し残す。
		settingsRoot, err := resolveSettingsRoot(cfg, inst, dryRun)
		if err != nil {
			return d, err
		}
		ws, err := workspace.New(workspace.Options{
			Config:       cfg,
			Herdr:        client,
			Logger:       logger,
			SettingsRoot: settingsRoot,
			// **常駐している側と同じ名前を渡す**（3-17b）。渡さないと、
			// `--id` の置き場所に目印が置かれず、**既定側の abandon がそこを
			// 「身元ファイルの無いディレクトリ」として数えて止まる。**
			InstanceID: inst.ID(),
			// **見せるだけの実行では、置き場所も名乗りも作らない**（3-17g）。
			NoCreate: dryRun,
		})
		if err != nil {
			return d, i18n.Errorf(i18n.KeyAbandonWorkspaceFailed, err)
		}
		d.Workspace = ws
	}
	if d.LockPath == "" {
		// **常駐している側と同じ Layout から取る**（3-17c）。
		// **ここが1バイトでもずれると、動いている continuo を「動いていない」と
		// 判定して worktree を消しにいく。**
		//
		// **見せるだけの実行では置き場所を作らない**（3-17g）。
		// 置き場所が無いということは、その `--id` の continuo が1度も動いていない
		// ということであり、**ReadLockState は覚え書きが無いことを見て
		// `LockStateNotRunning` と正しく答える。**
		if !dryRun {
			if err := inst.EnsureLockDir(); err != nil {
				return d, err
			}
		}
		d.LockPath = inst.LockPath()
	}
	if d.BoardLockPath == "" {
		// **常駐している側と同じ関数から取る**（3-17e）。
		// **`--id` では分けない。**ボードは名前から導けない。
		path, warnings, err := instance.BoardLockPath(
			cfg.Tracker.Provider.Owner, cfg.Tracker.Provider.ProjectNumber)
		if err != nil {
			return d, err
		}
		// **正規化で情報が落ちたら黙らない**（3-7）。`my org` と `my_org` が
		// 同じロックになる。**理由が分からないまま断られる人が出ないようにする。**
		for _, w := range warnings {
			logger.Warn("カンバンのロックの名前で正規化が情報を落としました",
				"owner", cfg.Tracker.Provider.Owner, "message", w.Message, "board_lock_file", path)
		}
		// **見せるだけの実行では `~/.continuo/board` を作らない**（3-17g）。
		if !dryRun {
			if err := instance.EnsureBoardDir(path); err != nil {
				return d, err
			}
		}
		d.BoardLockPath = path
	}
	if d.NewTracker == nil {
		d.NewTracker = func(ctx context.Context) (Tracker, error) {
			return newTracker(ctx, cfg, endpoint, logger)
		}
	}
	return d, nil
}

// resolveSocketPath は hook を受ける socket の絶対パスを、常駐プロセスと同じ経路で決める。
//
// **同じ経路でなければロックファイルの場所がずれる。**ずれると、動いている continuo を
// 「動いていない」と判定して worktree を消しにいく。
//
// cfg: 検証済みの設定。
// inst: `--id` から導いた置き場所。
// dryRun: 真なら置き場所を作らずにパスだけ決める（3-17g）。
// 戻り値: socket の絶対パスと、置き場所を用意できなかった場合のエラー。
func resolveSocketPath(cfg config.Config, inst instance.Layout, dryRun bool) (string, error) {
	env := os.Getenv(daemon.EnvRuntimeDir)
	resolve := inst.HookSocketPath
	if dryRun {
		resolve = inst.ResolveHookSocketPath
	}
	sockPath, err := resolve(env, cfg.Claude.HookBridge.Listen)
	if err != nil {
		return "", i18n.Errorf(i18n.KeyAbandonRuntimeDirFailed, err)
	}
	return sockPath, nil
}

// resolveSettingsRoot は issue ごとの Claude Code の設定ファイルの置き場所を決める（3-12）。
//
// cfg: 検証済みの設定。
// inst: `--id` から導いた置き場所。
// dryRun: 真なら置き場所を作らずにパスだけ決める（3-17g）。
// 戻り値: `<実行時ディレクトリ>/issues` の絶対パスと、決められなかった場合のエラー。
func resolveSettingsRoot(cfg config.Config, inst instance.Layout, dryRun bool) (string, error) {
	sockPath, err := resolveSocketPath(cfg, inst, dryRun)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(sockPath), hookserver.IssuesDirName), nil
}

// newTracker はボードを読み書きするアダプタを本物として組み立てる。
//
// **`gh` のトークンを引くので、ボードを読む必要が確定してから呼ぶこと。**
//
// ctx: トークンの取得に適用するコンテキスト。
// cfg: 検証済みの設定。
// endpoint: GraphQL API の接続先。空なら本番の GitHub。
// logger: ログの出力先。
// 戻り値: 組み立てたアダプタと、`gh` が無い場合・トークンを引けない場合のエラー。
func newTracker(
	ctx context.Context,
	cfg config.Config,
	endpoint string,
	logger *slog.Logger,
) (Tracker, error) {
	if err := tracker.CheckGHAvailable(); err != nil {
		return nil, err
	}
	token, err := tracker.ResolveToken(ctx, cfg.Tracker.Provider, nil)
	if err != nil {
		return nil, err
	}
	// **信頼の判定関数は渡さない。**abandon が使うのは Status だけで、
	// Dispatchable は読まない。渡すと issue 1件を引くたびに ghq と git が起動する。
	adapter, err := tracker.NewAdapter(
		cfg.Tracker, endpoint, token,
		&http.Client{Timeout: daemon.DefaultTrackerTimeout}, logger, nil)
	if err != nil {
		return nil, err
	}
	return adapter, nil
}

// sleepUntil は d だけ待つ。**ctx が先に終わったら待つのをやめる。**
//
// ctx: 待機を打ち切るコンテキスト。
// d: 待つ長さ。
// 戻り値: 待ち切れたら true、ctx が終わって打ち切ったら false。
func sleepUntil(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
