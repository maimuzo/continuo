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
	"github.com/maimuzo/continuo/internal/socketpath"
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

// Tracker は abandon が使うカンバンの読み書きの部分集合である。
//
// **`*tracker.Adapter` がこれを満たす。**Status を読む・動かす以外は使わない。
type Tracker interface {
	// Bootstrap は project と Status フィールドの ID を解決する。
	// **UpdateStatus を呼ぶ前に必ず通す。**
	Bootstrap(ctx context.Context, cfg config.TrackerConfig) error
	// FetchIssueByIdentifier は `<owner>/<repo>#<番号>` でカンバンの issue を1件引く。
	FetchIssueByIdentifier(ctx context.Context, identifier string) (tracker.Issue, bool, error)
	// UpdateStatus は project item の Status を書き換える。
	// **見るのは Reached（目的の Status になったか）だけである。**abandon は issue へ
	// コメントを書かないので、Wrote と Previous は使わない。
	UpdateStatus(ctx context.Context, itemID, targetState string, blockedStates []string) (tracker.StatusWrite, error)
	// VerifyKnownStates は、渡した Status 名がすべてカンバンの選択肢にあるかを確かめる。
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
// 本番のカンバン・本物の herdr・利用者の worktree に触らずに検査するためである。**
type Deps struct {
	// LockPath は二重起動防止のロックファイルの絶対パスである。
	// **空なら常駐している側と同じ関数から決める**（internal/instance の Layout）。
	LockPath string
	// AcquireLock はロックを試みる。nil なら internal/lock を呼ぶ。
	//
	// **取れたら continuo は動いていない。**`lock.ErrAlreadyRunning` を包んだエラーが
	// 返れば動いている。それ以外のエラーはロックファイルそのものを開けないことを表す。
	//
	// **取れたロックは実行の最後まで握る。**握っている間に起動しようとした継続監視は
	// 「既に起動しています」で止まる。消されるより望ましい。
	AcquireLock func(path string) (Unlocker, error)
	// Herdr は pane の一覧を取る口である。nil なら設定から本物を組み立てる。
	Herdr PaneLister
	// Workspace は worktree の走査・検査・片付けである。nil なら設定から本物を組み立てる。
	Workspace Workspace
	// NewTracker はカンバンのアダプタを作る。nil なら本物を組み立てる（`gh` のトークンを引く）。
	//
	// **遅延して呼ぶ。**カンバンを読まずに済む実行（worktree が無かった場合）で、
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
// cfg: 検証済みの設定。
// inst: `--id` から導いたロックの置き場所。**常駐している側と同じ Layout である**（3-17b）。
// endpoint: GitHub の GraphQL API の接続先（検査済み）。空なら本番の GitHub。
// logger: ログの出力先。
// 戻り値: すべてのフィールドが埋まった Deps と、組み立てに失敗した場合のエラー。
func (d Deps) resolve(
	cfg config.Config,
	inst instance.Layout,
	endpoint string,
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
		settingsRoot, err := resolveSettingsRoot(cfg)
		if err != nil {
			return d, err
		}
		ws, err := workspace.New(workspace.Options{
			Config:       cfg,
			Herdr:        client,
			Logger:       logger,
			SettingsRoot: settingsRoot,
		})
		if err != nil {
			return d, i18n.Errorf(i18n.KeyAbandonWorkspaceFailed, err)
		}
		d.Workspace = ws
	}
	if d.LockPath == "" {
		// **`lock.Acquire` を呼ぶ前に、置き場所を用意する**（internal/instance の
		// `EnsureLockDir` が「必ず通すこと」と決めている）。
		//
		// **用意しないと、`~/.continuo` を一度も作っていない機械で
		// 「ロックファイルを開けません」で止まり、何も消さない `--dry-run` すら通らない。**
		// **常駐を1度も起動していない人が、いちばん最初に叩くのがこの経路である。**
		if err := inst.EnsureLockDir(); err != nil {
			return d, err
		}
		d.LockPath = inst.LockPath()
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
// 戻り値: socket の絶対パスと、置き場所を用意できなかった場合のエラー。
func resolveSocketPath(cfg config.Config) (string, error) {
	sockPath, err := socketpath.Prepare(os.Getenv(daemon.EnvRuntimeDir), cfg.Claude.HookBridge.Listen)
	if err != nil {
		return "", i18n.Errorf(i18n.KeyAbandonRuntimeDirFailed, err)
	}
	return sockPath, nil
}

// resolveSettingsRoot は issue ごとの Claude Code の設定ファイルの置き場所を決める（3-12）。
//
// cfg: 検証済みの設定。
// 戻り値: `<実行時ディレクトリ>/issues` の絶対パスと、決められなかった場合のエラー。
func resolveSettingsRoot(cfg config.Config) (string, error) {
	sockPath, err := resolveSocketPath(cfg)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(sockPath), hookserver.IssuesDirName), nil
}

// newTracker はカンバンを読み書きするアダプタを本物として組み立てる。
//
// **`gh` のトークンを引くので、カンバンを読む必要が確定してから呼ぶこと。**
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
