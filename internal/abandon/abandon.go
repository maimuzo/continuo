// Package abandon は `continuo abandon` の実体である。
//
// **間違えて着手した issue を、着手する前の状態へ戻す。**worktree・pane・
// herdr の workspace・branch を消し、必要なら Status を人間が指定した値へ動かす。
//
// **段の順番が仕様である。**
//
//	段1 continuo が動いているかを調べ、動いていれば手を離させる
//	段2 issue の URL から worktree を探す（**身元ファイルの issue_url で照合し、パスで検算する**）
//	段2の後 これから書きうる Status の値を確かめる（**消す前に確かめる。--dry-run でも通る**）
//	段3 失われるものを調べて見せる（--dry-run はここで終わる）
//	段4 消す（**その前に、動いていないのに pane が生きていないかを確かめる**）
//	段5 Status を動かす（--to があるときだけ）
//
// **実行の順は段2 が段1 の後半より先である。**段1 の後半は「その worktree を cwd に
// 持つ pane が消えるまで待つ」ので、**待つ相手が決まっていなければ始められない。**
// worktree が1つも見つからなければ、消すものも待つものも無いので、そこで終わる。
//
// **`--dry-run` は段1 の後半を通らない。**段1 の後半はボードへ書き込み、エージェントに
// 手を離させる。**見せるだけの実行が副作用を起こしてはならない。**代わりに段3 で
// 「実行したら Status をどこへ動かすか」を1行で予告する。
//
// **書きうる Status の値は、消す前に確かめる**（段2 の直後の verifyTargets）。
// `--to` の綴り違いが分かるのが `UpdateStatus` を呼ぶ段5 だと、**worktree と branch を
// 失ったうえに Status も動かない。**確かめるのは読み取りだけで、ボードへは1文字も書かない。
//
// **判断を書き直さない。**未コミット・未 push の判定は internal/workspace の
// Inspect（Cleanup と同じ関数を呼ぶ）、片付けは Cleanup、Status は internal/tracker、
// 二重起動の判定は internal/lock である。
//
// **本番のボードへ書き込む経路は2つだけである**（段1 の手を離させる書き込みと、
// 段5 の `--to`）。どちらも人間が明示した実行でしか通らない。テストは
// httptest.Server で立てたテスト用GraphQL mock サーバに向けること。
package abandon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/lock"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// 終了コードである。
//
// **2（引数の指定の誤り）は internal/cli が返す。**このパッケージは引数を解釈しない。
const (
	// ExitOK は消した・消すものが無かった・`--dry-run` で見せ終わった場合である。
	ExitOK = 0
	// ExitStopped は**何も消さずに止まった**か、消す途中で失敗した場合である。
	ExitStopped = 1
)

// PaneWaitTimeoutFactor は pane が閉じるのを待つ上限を `herdr.read_timeout_ms` の
// 何倍にするかである。
//
// **`read_timeout_ms` そのものを使わない。**あれは「1回の問い合わせの応答を待つ上限」
// であって、「継続監視のプロセスが turn を終えて pane を手放すまでの時間」ではない。
// 既定（5秒）の10倍で50秒になる。
const PaneWaitTimeoutFactor = 10

// DefaultPaneWaitInterval は pane がまだあるかを問い合わせ直す間隔である。
const DefaultPaneWaitInterval = time.Second

// Options は Run の入力である。
type Options struct {
	// ConfigPath は読み込む WORKFLOW.md の絶対パスである。必須。
	ConfigPath string
	// IssueURL は片付ける issue の URL である。必須。
	// 例 `https://github.com/octocat/hello-world/issues/42`。
	IssueURL string
	// DryRun は「何を消すかを見せるだけで、消さない」を表す（段3 で終わる）。
	DryRun bool
	// Force は「失うものがあっても消す」を表す。
	// **これが無ければ、失うものがあった時点で何も消さずに止まる。**
	Force bool
	// ToState は片付けたあとに Status を動かす先である。
	// **空なら動かさない**（ボードでどうするかは人間が決める）。
	ToState string
	// ParkState は continuo に手を離させるために一時的に動かす先である。
	// **空なら tracker.failure_state を使う。**
	ParkState string
	// GraphQLEndpoint は GitHub の GraphQL API の接続先である。
	// **空なら本番の GitHub を使う。**テストは httptest.Server の URL を渡すこと。
	GraphQLEndpoint string
	// Out は人間に見せる出力先である。nil なら os.Stdout。
	Out io.Writer
	// Err は止まった理由の出力先である。nil なら os.Stderr。
	Err io.Writer
	// Logger は途中経過のログの出力先である。nil なら何も出力しない。
	Logger *slog.Logger
	// PaneWaitTimeout は pane が閉じるのを待つ上限である。
	// **0 なら `herdr.read_timeout_ms` の PaneWaitTimeoutFactor 倍を使う。**
	PaneWaitTimeout time.Duration
	// PaneWaitInterval は pane がまだあるかを問い合わせ直す間隔である。
	// **0 なら DefaultPaneWaitInterval を使う。**
	PaneWaitInterval time.Duration
	// Deps は外部へ繋ぐ処理である。**ゼロ値なら設定から本物を組み立てる。**
	Deps Deps
}

// runner は1回の実行のあいだ持ち回る状態である。
//
// **引数で配り歩かない。**設定・出力先・ボードの読み取り結果を段ごとに渡すと、
// 段が増えるたびに引数が伸びる。
type runner struct {
	opts   Options
	cfg    config.Config
	deps   Deps
	issue  IssueRef
	out    io.Writer
	errOut io.Writer
	logger *slog.Logger

	// tr はボードのアダプタである。**最初に必要になったときだけ作る。**
	tr Tracker
	// trErr はアダプタを作れなかった理由である。
	trErr error
	// bootstrapped は Bootstrap を通したかどうかである。
	bootstrapped bool
	// board はボードから引いた issue である。
	board tracker.Issue
	// boardFound はボードにその issue が載っていたかどうかである。
	boardFound bool
	// boardErr はボードを読めなかった理由である。
	boardErr error
	// boardRead はボードを1度でも読んだかどうかである（読み直しを避ける）。
	boardRead bool

	// parkDeferred は「継続監視は動いているが、`--dry-run` なので手を離させなかった」
	// ことを表す。**段3 で予告の1行を出すのに使う。**
	parkDeferred bool
}

// Run は `continuo abandon` の段1〜段5 を通す。
//
// ctx: 実行に適用するコンテキスト。**`SIGINT` / `SIGTERM` で終わるものを渡すこと**
// （段1 の pane 待ちがここで打ち切られる）。
// opts: 対象の issue・設定ファイルのパス・フラグ・外部へ繋ぐ処理。
// 戻り値: 終了コード（ExitOK / ExitStopped）。**理由は opts.Err へ書き出し済みである。**
func Run(ctx context.Context, opts Options) int {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	errOut := opts.Err
	if errOut == nil {
		errOut = os.Stderr
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	issue, err := ParseIssueURL(opts.IssueURL)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return ExitStopped
	}

	loaded, err := config.Load(opts.ConfigPath)
	if err != nil {
		fmt.Fprintln(errOut, i18n.T(i18n.KeyAbandonErrConfigLoad, err))
		return ExitStopped
	}

	deps, err := opts.Deps.resolve(loaded.Config, opts.GraphQLEndpoint, logger)
	if err != nil {
		fmt.Fprintln(errOut, i18n.T(i18n.KeyAbandonErrBuild, err))
		return ExitStopped
	}

	r := &runner{
		opts:   opts,
		cfg:    loaded.Config,
		deps:   deps,
		issue:  issue,
		out:    out,
		errOut: errOut,
		logger: logger,
	}
	return r.run(ctx)
}

// run は段1〜段5 を順に通す。
//
// ctx: 実行に適用するコンテキスト。
// 戻り値: 終了コード。
func (r *runner) run(ctx context.Context) int {
	// 段1 の前半: continuo が動いているかを調べる。
	running, unlocker, err := r.isRunning()
	if err != nil {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrLockFile, r.deps.LockPath, err))
		return ExitStopped
	}
	// **取れたロックは最後まで握る。**手放すと、その隙に起動した継続監視の足元から
	// worktree を消す。握っている間に起動しようとした継続監視は起動を諦めて止まる。
	if unlocker != nil {
		defer func() {
			if err := unlocker.Release(); err != nil {
				r.logger.Warn("ロックの解放に失敗しました", "lock_file", r.deps.LockPath, "error", err)
			}
		}()
	}
	if running {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonRunning, r.deps.LockPath))
	} else {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonNotRunning))
	}

	// 段2: worktree を探す。**待つ相手も消す相手も、ここで決まる。**
	found, code := r.find()
	if found == nil {
		return code
	}

	// 段2 の直後: これから書きうる Status の値を確かめる。**消す前に確かめる。**
	if code := r.verifyTargets(ctx, running); code != ExitOK {
		return code
	}

	// 段1 の後半: 動いているなら手を離させ、pane が消えるまで待つ。
	// **`--dry-run` では通らない。**ボードへ書き込み、エージェントに手を離させるからである。
	switch {
	case running && r.opts.DryRun:
		r.parkDeferred = true
	case running:
		if code := r.park(ctx, found); code != ExitOK {
			return code
		}
		if code := r.waitPaneGone(ctx, found.Path); code != ExitOK {
			return code
		}
	}

	// 段3: 失われるものを調べて見せる。
	leftover, err := r.deps.Workspace.Inspect(ctx, workspace.CleanupRequest{WorktreePath: found.Path})
	if err != nil {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrInspect, found.Path, err))
		return ExitStopped
	}
	r.printPlan(ctx, leftover)

	if r.opts.DryRun {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonDryRunNote))
		return ExitOK
	}
	if leftover.HasLoss() && !r.opts.Force {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrLossWithoutForce))
		return ExitStopped
	}
	// **動いていないと判定したときこそ pane を確かめる。**ロックファイルの場所は
	// 環境変数（CONTINUO_RUNTIME_DIR / XDG_RUNTIME_DIR / TMPDIR）で決まるので、
	// launchd から起動した継続監視と端末から叩いた abandon で食い違いうる。
	// **herdr の socket は設定で決まって環境変数では動かないので、ロックより信用できる。**
	if !running {
		if code := r.stopIfPaneAlive(ctx, found.Path); code != ExitOK {
			return code
		}
	}

	// 段4: 消す。
	if code := r.remove(ctx, found.Path, leftover); code != ExitOK {
		return code
	}

	// 段5: Status を動かす。
	return r.moveStatus(ctx)
}

// isRunning は continuo が動いているかを、二重起動防止のロックで調べる（3-17）。
//
// **ロックを取れたら動いていない。取れたロックは返す。**呼び出し側が実行の最後まで
// 握り続けること。**その場で手放してはならない。**手放すと直後に継続監視が起動でき、
// その足元から worktree を消す。abandon は git と RPC を何度も叩くので窓は秒単位で開く。
// 握っているあいだに起動しようとした継続監視は「既に起動しています」で止まる。
//
// 戻り値の1つ目: 動いていれば true。
// 戻り値の2つ目: 取れたロック（**動いていたときと開けなかったときは nil**）。
// 戻り値の3つ目: **ロックファイルそのものを開けなかった場合のエラー**
// （二重起動とは言い分ける。設定の `runtime.lock_file` の打ち間違いがこれである）。
func (r *runner) isRunning() (bool, Unlocker, error) {
	l, err := r.deps.AcquireLock(r.deps.LockPath)
	if err != nil {
		if errors.Is(err, lock.ErrAlreadyRunning) {
			return true, nil, nil
		}
		return false, nil, err
	}
	return false, l, nil
}

// find は issue の URL から worktree を1つに絞る（段2）。
//
// **身元ファイルの `issue_url` で照合する。**パスを owner / repo / 番号 から
// 組み立ててはならない（`workspace.root` や `branch_template` を変えている環境で空振りする）。
//
// **照合した結果はパスで検算する。**身元ファイルは worktree の直下にあり、そこで
// エージェントが `--permission-mode dontAsk` で動くので、`issue_url` は書き換えられる。
// 検算しなければ、worktree A のエージェントが自分の `issue_url` を issue B に書き換えるだけで、
// **人間が B を取り消したとき A が消える。**置き場所は `<root>/<host>/<owner>/<repo>/<スラグ>`
// の固定4階層で、パスは封じ込め検査（3-20）を通っているので書き換えられない（3-22）。
//
// **身元ファイルが壊れていた worktree は候補にしない。**照合する鍵が読めないためである。
// **消しもしない**（3-4 の段2）。何があったかは1行残す。
//
// 戻り値の1つ目: 見つかった worktree。見つからなかった場合と2つ以上あった場合は nil。
// 戻り値の2つ目: nil を返すときの終了コード。**0件は ExitOK である**（消すものが無い）。
func (r *runner) find() (*workspace.ScannedWorktree, int) {
	scanned, err := r.deps.Workspace.Scan()
	if err != nil {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrScan, err))
		return nil, ExitStopped
	}

	var matched []workspace.ScannedWorktree
	for _, w := range scanned {
		if w.Err != nil {
			r.logger.Warn("身元ファイルを読めない worktree があります（消しません）",
				"worktree", w.Path, "error", w.Err)
			continue
		}
		if w.Identity == nil {
			continue
		}
		if !r.issue.SameIssue(w.Identity.IssueURL) {
			continue
		}
		if !r.pathAgrees(w) {
			continue
		}
		matched = append(matched, w)
	}

	switch len(matched) {
	case 0:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonNotFound, r.issue.URL))
		// **`--to` を黙って捨てない。**指定した人間は「動いた」と受け取るが、
		// ここで終わるとボードには1文字も書かれない。
		// **代わりに段5 を通すことはしない。**worktree が無い理由は「もう片付けた」とは
		// 限らず、**URL の打ち間違い**でもある。打ち間違えた相手の Status を動かすほうが害が大きい。
		if target := strings.TrimSpace(r.opts.ToState); target != "" {
			fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonToSkipped, target))
		}
		return nil, ExitOK
	case 1:
		return &matched[0], ExitOK
	default:
		// **人間が中身を見て決めること。**同じ issue の worktree が2つあるのは、
		// 置き場所の設定を変えた跡か、手で作った worktree が混ざったかである。
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrMultiple, len(matched), r.issue.URL))
		for _, w := range matched {
			fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonMultipleItem,
				w.Path, r.deps.Workspace.IdentityPath(w.Path)))
		}
		return nil, ExitStopped
	}
}

// pathAgrees は、身元ファイルの `issue_url` が worktree の置き場所と辻褄が合うかを返す。
//
// **消す相手を決める値の裏を取るための検算である。**置き場所から取り出した
// owner とリポジトリ名は**エージェントが書き換えられない**ので、書き換えられる
// `issue_url` の側がそれと食い違えば、その worktree は候補にしない。
//
// **候補から外すだけで、消しはしない。**食い違いの原因は書き換えとは限らず、
// 人間が置き場所を移した跡かもしれない。どちらであれ何があったかは1行残す。
//
// w: 身元ファイルの `issue_url` が一致した worktree。
// 戻り値: 置き場所の owner とリポジトリ名が渡された issue と一致すれば true。
func (r *runner) pathAgrees(w workspace.ScannedWorktree) bool {
	owner, repo, err := r.deps.Workspace.OwnerRepoOf(w.Path)
	if err != nil {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonOwnerRepoUnreadable, w.Path, err))
		r.logger.Warn("worktree のパスから owner とリポジトリ名を読めません（消しません）",
			"worktree", w.Path, "error", err)
		return false
	}
	// **GitHub は owner とリポジトリ名の大文字小文字を区別しない**ので、無視して比べる。
	if strings.EqualFold(owner, r.issue.Owner) && strings.EqualFold(repo, r.issue.Repo) {
		return true
	}
	fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonOwnerRepoMismatch,
		w.Path, owner, repo, w.Identity.IssueURL))
	r.logger.Warn("身元ファイルの issue_url が置き場所と食い違います（消しません）",
		"worktree", w.Path, "path_owner", owner, "path_repo", repo,
		"issue_url", w.Identity.IssueURL)
	return false
}

// verifyTargets は、これから書きうる Status の値を消す前に確かめる（段2 の直後）。
//
// **確かめるのは2つである。**
//
//	その値がボードの Status の選択肢にあるか（`--to` と park の先）
//	park の先が `tracker.active_states` に入っていないか
//
// **選択肢の照合を段5 まで遅らせてはならない。**`UpdateStatus` は選択肢に無い名前を
// 断るが、それを呼ぶのは worktree と branch を消したあとである。`--to Dnoe` のような
// 綴り違いで、**worktree を失ったうえに Status も動かない**という結果になる。
//
// **park の先が作業中の状態なら書く前に止める。**そこへ動かしても継続監視は手を離さず、
// pane も閉じない。`pane.list` がたまたま空を返した瞬間に、**手を離していない issue の
// worktree を消す。**`tracker.failure_state` が作業中の状態でないことは設定の検証が
// 保証しているが、**`--park` はその検証を通らない。**
//
// **ボードへは1文字も書かない。**読むだけなので `--dry-run` でも通す。
//
// ctx: 実行に適用するコンテキスト。
// running: 継続監視が動いているか（park の先を確かめるかどうかがこれで決まる）。
// 戻り値: 続けてよければ ExitOK、確かめられなかった場合・値が誤っている場合は ExitStopped。
func (r *runner) verifyTargets(ctx context.Context, running bool) int {
	var targets []string
	if target := strings.TrimSpace(r.opts.ToState); target != "" {
		targets = append(targets, target)
	}
	if running {
		park := r.parkTarget()
		if containsFold(r.cfg.Tracker.ActiveStates, park) {
			fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrParkActive, park))
			return ExitStopped
		}
		targets = append(targets, park)
	}
	if len(targets) == 0 {
		return ExitOK
	}

	tr, err := r.tracker(ctx)
	if err != nil {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrTracker, err))
		return ExitStopped
	}
	if err := r.bootstrap(ctx, tr); err != nil {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrTracker, err))
		return ExitStopped
	}
	if err := tr.VerifyKnownStates(targets); err != nil {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrUnknownState, err))
		return ExitStopped
	}
	return ExitOK
}

// stopIfPaneAlive は、継続監視が動いていないと判定したときに pane の生死を確かめる（段4 の前）。
//
// **ロックだけを根拠に消しにいかない。**ロックファイルの場所は環境変数で決まるので、
// launchd から起動した継続監視と端末から叩いた abandon で食い違いうる。食い違えば
// 「動いていない」と判定したまま、生きた pane ごと worktree を消す。
//
// **待たずに止める。**手を離させていない以上、待っても pane は閉じない。
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 対象の worktree の絶対パス。
// 戻り値: pane が無ければ ExitOK、1件でもあれば・herdr に問い合わせられなければ ExitStopped。
func (r *runner) stopIfPaneAlive(ctx context.Context, worktreePath string) int {
	panes, err := r.panesOf(ctx, worktreePath)
	if err != nil {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrPaneList, err))
		return ExitStopped
	}
	if len(panes) == 0 {
		return ExitOK
	}
	fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrPaneAlive,
		strings.Join(paneIDs(panes), " "), r.deps.LockPath))
	return ExitStopped
}

// parkTarget は、手を離させるために Status を動かす先を決める。
//
// 戻り値: `--park` の値、指定が無ければ `tracker.failure_state`。
func (r *runner) parkTarget() string {
	if target := strings.TrimSpace(r.opts.ParkState); target != "" {
		return target
	}
	return r.cfg.Tracker.FailureState
}

// park は、動いている continuo にその issue から手を離させる（段1 の後半）。
//
// **ボードから現在の Status を取り直してから決める。**身元ファイルには Status が
// 書いていないうえ、書いてあっても古い。`tracker.active_states` に入っていなければ、
// continuo はもうその issue を持っていないので何もしない。
//
// 動かす先は `--park`、指定が無ければ `tracker.failure_state` である。
// **その先が `active_states` に入っていないことは段2 の直後で確かめてある**（verifyTargets）
// ので、動かせば必ず active から外れる。
//
// ctx: 実行に適用するコンテキスト。
// found: 対象の worktree。
// 戻り値: 続けてよければ ExitOK、止まるなら ExitStopped。
func (r *runner) park(ctx context.Context, found *workspace.ScannedWorktree) int {
	state, itemID, ok := r.currentState(ctx)
	if !ok {
		// **Status を確かめられないまま消しにいかない。**continuo が掴んでいる worktree を
		// 消すと、走っているエージェントの足元からディレクトリが消える。
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrParkStateUnknown, r.issue.Identifier(), r.boardReason()))
		return ExitStopped
	}
	if !containsFold(r.cfg.Tracker.ActiveStates, state) {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonParkNotActive, state))
		return ExitOK
	}

	target := r.parkTarget()

	tr, err := r.tracker(ctx)
	if err != nil {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrTracker, err))
		return ExitStopped
	}
	if err := r.bootstrap(ctx, tr); err != nil {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrTracker, err))
		return ExitStopped
	}
	// **書かない状態の一覧は渡さない。**人間が「手を離させろ」と言っている実行であり、
	// いまの Status が何であっても park の先へ動かす。
	written, err := tr.UpdateStatus(ctx, itemID, target, nil)
	if err != nil {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrParkFailed, target, err))
		return ExitStopped
	}
	if !written {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonParkNotWritten, target))
		return ExitStopped
	}
	fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonParkMoved, state, target, found.Path))
	return ExitOK
}

// waitPaneGone は、その worktree を cwd に持つ pane が無くなるまで待つ（段1 の後半）。
//
// **消えるまで待たないと、消した worktree の中で Claude Code が動き続ける。**
// 上限を超えたら**何も消さずに止まる。**
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 対象の worktree の絶対パス。
// 戻り値: pane が消えたら ExitOK、上限を超えた場合・herdr に問い合わせられない場合は ExitStopped。
func (r *runner) waitPaneGone(ctx context.Context, worktreePath string) int {
	timeout := r.paneWaitTimeout()
	interval := r.opts.PaneWaitInterval
	if interval <= 0 {
		interval = DefaultPaneWaitInterval
	}

	deadline := r.deps.Now().Add(timeout)
	for {
		panes, err := r.panesOf(ctx, worktreePath)
		if err != nil {
			fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrPaneList, err))
			return ExitStopped
		}
		if len(panes) == 0 {
			fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPaneGone))
			return ExitOK
		}
		if !r.deps.Now().Before(deadline) {
			fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrPaneRemains,
				timeout, strings.Join(paneIDs(panes), " ")))
			return ExitStopped
		}
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonWaitingPane,
			strings.Join(paneIDs(panes), " "), worktreePath))
		if !r.deps.Sleep(ctx, interval) {
			// **中断と時間切れを同じ文言で出さない。**「%v 以内に閉じませんでした」は
			// 待ち切った場合の文であり、`SIGINT` / `SIGTERM` で人間が止めたときに出すと、
			// **上限が短すぎたのかと読み違える。**止めた本人に何が起きたかを言う。
			fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrPaneWaitInterrupted,
				strings.Join(paneIDs(panes), " ")))
			return ExitStopped
		}
	}
}

// paneWaitTimeout は pane が閉じるのを待つ上限を決める。
//
// 戻り値: `Options.PaneWaitTimeout`、指定が無ければ
// `herdr.read_timeout_ms` の PaneWaitTimeoutFactor 倍。
func (r *runner) paneWaitTimeout() time.Duration {
	if r.opts.PaneWaitTimeout > 0 {
		return r.opts.PaneWaitTimeout
	}
	return time.Duration(r.cfg.Herdr.ReadTimeoutMs) * PaneWaitTimeoutFactor * time.Millisecond
}

// panesOf は、その worktree を作業ディレクトリに持つ pane を集める。
//
// **照合は cwd である。**身元ファイルの `herdr_workspace_id` は worktree の中にあり、
// エージェントが書き換えられる（3-9 の resolveWorkspaceID と同じ理由）。
// **`foreground_cwd` も見る。**Claude Code が別のディレクトリへ降りていても、
// pane そのものはその worktree に属している。
//
// **一致だけでなく「worktree の内側」も拾う**（継続監視の hook の判定と同じ形。
// internal/orchestrator/hookinput.go の acceptHookCwd）。**待つ側は広く取るほうが安全である。**
// 多めに拾えば待つか止まるだけだが、少なく拾うと生きている pane ごと消す。
//
// **herdr の口が無ければ「pane は無い」として返す**（`Deps.Herdr` が nil のとき）。
// 差し替えを渡さない検査で、本物の herdr へ接続しにいかないためである。
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 対象の worktree の絶対パス。
// 戻り値の1つ目: 一致した pane。
// 戻り値の2つ目: herdr へ問い合わせられなかった場合のエラー。
func (r *runner) panesOf(ctx context.Context, worktreePath string) ([]herdr.Pane, error) {
	if r.deps.Herdr == nil {
		return nil, nil
	}
	list, err := r.deps.Herdr.PaneList(ctx, herdr.PaneListParams{})
	if err != nil {
		return nil, err
	}
	var matched []herdr.Pane
	for _, p := range list.Panes {
		if sameOrUnder(worktreePath, p.Cwd) || sameOrUnder(worktreePath, p.ForegroundCwd) {
			matched = append(matched, p)
		}
	}
	return matched, nil
}

// remove は worktree・pane・herdr の workspace・branch をまとめて消す（段4）。
//
// **`herdr worktree remove` が pane ごと閉じることは実測で確認済みである**（3-9）。
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 対象の worktree の絶対パス。
// leftover: 段3 で調べた内訳（消せなかったときに何が残ったかを言うのに使う）。
// 戻り値: 消せたら ExitOK、消せなければ ExitStopped。
func (r *runner) remove(ctx context.Context, worktreePath string, leftover *workspace.Leftover) int {
	// **Force を真で渡す。**消してよいかは段3 で人間に見せて決着している
	// （失うものがあれば `--force` が無い限りここへ来ない）。
	// **渡さないと、`cleanup.enabled` が偽の環境や `require_pushed` が真の環境で
	// `--force` が効かない。**
	result, err := r.deps.Workspace.Cleanup(ctx, workspace.CleanupRequest{
		WorktreePath: worktreePath,
		Force:        true,
	})
	if err != nil {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrCleanup, worktreePath, err))
		return ExitStopped
	}
	if !result.Removed {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrDeferred, worktreePath))
		for _, reason := range result.Reasons {
			fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonDeferredReason, reason))
		}
		return ExitStopped
	}

	branch := ""
	if leftover.Identity != nil {
		branch = leftover.Identity.Branch
	}
	// **消えていない branch を「消しました」と書かない。**cleanup.delete_branch が偽のとき、
	// branch の検算に落ちたとき、`git branch -D` が失敗したときは branch が残る。
	if result.BranchDeleted {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonRemoved, worktreePath, branch))
	} else {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonRemovedBranchKept, worktreePath, branch))
	}
	return ExitOK
}

// moveStatus は片付けたあとの Status を決める（段5）。
//
// **`--to` が無ければ動かさない。**どこへ置くかは、その issue をこれからどうするかで
// 決まる。continuo が勝手に決めてよいものではない。
//
// ctx: 実行に適用するコンテキスト。
// 戻り値: ExitOK か、書き込みに失敗した場合の ExitStopped。
func (r *runner) moveStatus(ctx context.Context) int {
	target := strings.TrimSpace(r.opts.ToState)
	if target == "" {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonStatusLeftAlone))
		return ExitOK
	}

	_, itemID, ok := r.currentState(ctx)
	if !ok {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrStatusTargetUnknown, r.issue.Identifier(), r.boardReason()))
		return ExitStopped
	}

	tr, err := r.tracker(ctx)
	if err != nil {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrTracker, err))
		return ExitStopped
	}
	if err := r.bootstrap(ctx, tr); err != nil {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrTracker, err))
		return ExitStopped
	}
	written, err := tr.UpdateStatus(ctx, itemID, target, nil)
	if err != nil {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrStatusFailed, target, err))
		return ExitStopped
	}
	if !written {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonStatusNotWritten, target))
		return ExitStopped
	}
	fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonStatusMoved, target))
	return ExitOK
}

// tracker はボードのアダプタを1回だけ作って使い回す。
//
// **必要になるまで作らない。**作るときに `gh` を起動してトークンを引くので、
// ボードを読まずに済む実行（worktree が無かった場合）で API 枠を使わない。
//
// ctx: トークンの取得に適用するコンテキスト。
// 戻り値: アダプタと、作れなかった場合のエラー。
func (r *runner) tracker(ctx context.Context) (Tracker, error) {
	if r.tr != nil || r.trErr != nil {
		return r.tr, r.trErr
	}
	r.tr, r.trErr = r.deps.NewTracker(ctx)
	return r.tr, r.trErr
}

// bootstrap は project と Status フィールドの ID を1回だけ解決する。
//
// **UpdateStatus はこれを通していないと必ず失敗する**（internal/tracker）。
//
// ctx: 実行に適用するコンテキスト。
// tr: ボードのアダプタ。
// 戻り値: 解決に失敗した場合のエラー。
func (r *runner) bootstrap(ctx context.Context, tr Tracker) error {
	if r.bootstrapped {
		return nil
	}
	if err := tr.Bootstrap(ctx, r.cfg.Tracker); err != nil {
		return err
	}
	r.bootstrapped = true
	return nil
}

// currentState はボードから現在の Status と project item の ID を引く（1回だけ読む）。
//
// **身元ファイルの `project_item_id` は使わない。**worktree の中にあり、
// エージェントが書き換えられる値なので、それを宛先にして Status を書くと
// 別の issue の Status を動かせてしまう。
//
// ctx: 実行に適用するコンテキスト。
// 戻り値の1つ目: 現在の Status 名。
// 戻り値の2つ目: project item の ID。
// 戻り値の3つ目: ボードから引けたなら true。**偽のときは r.boardReason() に理由が入る。**
func (r *runner) currentState(ctx context.Context) (string, string, bool) {
	r.readBoard(ctx)
	if !r.boardFound {
		return "", "", false
	}
	return r.board.State, r.board.ID, true
}

// readBoard はボードから issue を1件だけ引き、結果を持ち回る値へ入れる。
//
// **1回の実行で1度しか読まない。**段1 と段3 と段5 が同じ値を使う。
//
// ctx: 実行に適用するコンテキスト。
func (r *runner) readBoard(ctx context.Context) {
	if r.boardRead {
		return
	}
	r.boardRead = true

	tr, err := r.tracker(ctx)
	if err != nil {
		r.boardErr = err
		return
	}
	issue, found, err := tr.FetchIssueByIdentifier(ctx, r.issue.Identifier())
	if err != nil {
		r.boardErr = err
		return
	}
	if !found {
		r.boardErr = i18n.Errorf(i18n.KeyAbandonBoardNotListed, r.issue.Identifier())
		return
	}
	r.board = issue
	r.boardFound = true
}

// boardReason はボードから引けなかった理由を1行で返す。
//
// 戻り値: 理由の文言。理由が無ければ空文字。
func (r *runner) boardReason() string {
	if r.boardErr == nil {
		return ""
	}
	return r.boardErr.Error()
}

// printPlan は「何が消えて、何が失われるか」を人間に見せる（段3）。
//
// **消す前に必ず全部出す。**`--dry-run` でも `--force` でも同じものを出す。
//
// **`--dry-run` で継続監視が動いているときは、実行したら Status をどこへ動かすかも予告する。**
// `--dry-run` はボードへ1文字も書かないので、書かれる値をここで見せなければ、
// 人間は実行して初めてそれを知ることになる。
//
// ctx: 実行に適用するコンテキスト（Status の読み取りに使う）。
// leftover: Inspect が返した内訳。
func (r *runner) printPlan(ctx context.Context, leftover *workspace.Leftover) {
	fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanHeader))
	fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanIssue, r.issue.Identifier(), r.issue.URL))

	if state, _, ok := r.currentState(ctx); ok {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanStatus, state))
	} else {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanStatusUnknown, r.boardReason()))
	}

	fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanWorktree, leftover.WorktreePath))

	branch, workspaceID := "", ""
	if leftover.Identity != nil {
		branch = leftover.Identity.Branch
		workspaceID = leftover.Identity.HerdrWorkspaceID
	}
	fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanBranch, branch, leftover.Base))
	fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanHerdrWorkspace, workspaceID))

	switch panes, err := r.panesOf(ctx, leftover.WorktreePath); {
	case err != nil:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanPaneUnknown, err))
	case len(panes) == 0:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanPaneNone))
	default:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanPane, strings.Join(paneIDs(panes), " ")))
	}

	// **数え切れなかったことを黙らない。**`git status --porcelain` の読み取りは
	// 上限で打ち切られるので、その先に何ファイルあるかは分からない。
	// 打ち切られた数をそのまま出すと、**失う量を実際より少なく見せる。**
	if leftover.DirtyFilesTruncated {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanDirtyAtLeast, leftover.DirtyFiles))
	} else {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanDirty, leftover.DirtyFiles))
	}
	switch {
	case leftover.HasUpstream:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanUnpushed, leftover.UnpushedCommits))
	case leftover.BaseUnknown:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanBaseUnknown))
	case leftover.DiffFromBase:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanDiffFromBase, leftover.Base))
	default:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanNoDiffFromBase, leftover.Base))
	}

	if r.parkDeferred {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanParkPending, r.parkTarget()))
	}
}

// paneIDs は pane の ID だけを取り出す（人間に見せる1行にするため）。
//
// panes: 対象の pane。
// 戻り値: pane の ID の一覧。
func paneIDs(panes []herdr.Pane) []string {
	ids := make([]string, 0, len(panes))
	for _, p := range panes {
		ids = append(ids, p.PaneID)
	}
	return ids
}

// containsFold は、一覧に値が入っているかを大文字小文字を無視して調べる。
//
// **トラッカーの綴りはそのまま保ち、比べるときだけ無視する**（3-13 / SPEC.md 11.3）。
//
// list: 調べる一覧。
// value: 探す値。
// 戻り値: 入っていれば true。
func containsFold(list []string, value string) bool {
	target := strings.TrimSpace(value)
	for _, item := range list {
		if strings.EqualFold(strings.TrimSpace(item), target) {
			return true
		}
	}
	return false
}

// sameOrUnder は path が root と同じ場所か、その内側かを判定する。
//
// **シンボリックリンクを解決してから比べる。**worktree の置き場所の下は
// シンボリックリンクであることがあり、素朴な文字列比較では一致しない（3-22）。
// 解決できないほう（消えたパスなど）は、Clean しただけの値で比べる。
//
// **内側も同じ扱いにする。**Claude Code が worktree の下の階層へ降りていても、
// その pane はその worktree に属している。**降りた先を取りこぼすと、生きている pane を
// 「もう無い」と判定して worktree ごと消す。**
//
// root: 基準にする worktree のパス。空文字はどれにも一致しない。
// path: 判定するパス。空文字はどれにも一致しない。
// 戻り値: 同じ場所か内側なら true。
func sameOrUnder(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	resolvedRoot := resolveOrClean(root)
	resolved := resolveOrClean(path)
	if resolved == resolvedRoot {
		return true
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..")
}

// resolveOrClean はシンボリックリンクを解決する。解決できなければ Clean した値を返す。
//
// path: 対象のパス。
// 戻り値: 解決済みの絶対パス、または Clean したパス。
func resolveOrClean(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}
