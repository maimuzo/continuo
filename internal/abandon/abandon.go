// Package abandon は `continuo abandon` の実体である。
//
// **間違えて着手した issue を、着手する前の状態へ戻す。**worktree・pane・
// herdr の workspace・branch を消し、必要なら Status を人間が指定した値へ動かす。
//
// **段の順番が仕様である。**
//
//	段1 continuo が動いているかを調べ、動いていれば手を離させる
//	段2 issue の URL から worktree を探す（**身元ファイルの issue_url で照合し、パスで検算する**）
//	段2b worktree が0件なら、規則から組み立てた branch が残っていないかを見て片付ける
//	段2の後 これから書きうる Status の値を確かめる（**消す前に確かめる。--dry-run でも通る**）
//	段3 失われるものを調べて見せる（--dry-run はここで終わる）
//	段4 消す（**その前に、手を離させていない実行では pane が無いことを確かめる。**
//	    pane が残っているときも、herdr が答えず確かめられないときも `--force` が要る）
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
// **外の道具が答えられないことを理由に止まらない**（issue #23）。worktree の `.git` が
// 壊れていれば git は1つも通らず、herdr が落ちていれば pane の生死も引けない。
// **そこで止まると、まさに片付けたい状態で片付けられなくなる。**調べられなかったことは
// 段3 で1件ずつ見せ、**「失うものが無い」とは決して言わない。**消す実行では
// `--force` を要求する（中身の分からないものを黙って消さないため）。
// **例外は、worktree の `.git` が別のリポジトリを指していたときだけである**
// （書き換えの痕跡なので1バイトも消さない。3-20 / 3-22）。
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
	// parkedTo は手を離させるために実際に書き込んだ Status の値である。空なら書いていない。
	//
	// **書いたあとに何も消さずに止まったとき、Status がその値のまま残ることを
	// 人間へ言うのに使う。**continuo は元へ戻さない（戻すと、書いた瞬間に継続監視が
	// 拾い直しうる。戻す先は `tracker.active_states` の値である）。
	parkedTo string
	// statusSettled は、段5 が Status について応答し終えたかどうかである。
	//
	// **消せたかどうかで決めてはならない。**片付けに成功しても `--to` の書き込みに
	// 失敗すれば段5 は Status の在りかを言わずに終わるので、そこで park の言い添えを
	// 止めると、**Status が park の値のまま残ったことを誰も言わない。**
	statusSettled bool
	// noWorktree は、この issue に一致する worktree が1つも無かったことを表す。
	// **段2b（残った branch の片付け）へ進む合図である**（issue #27）。
	noWorktree bool
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
	// **手を離させる書き込みを済ませたあとで止まったら、そのことを必ず言う。**
	// どの段で止まっても Status は park の値のまま残る。**どこで止まっても同じ1行が
	// 出るように、段ごとに書かず、ここで1度だけ仕掛ける**（書き漏らす段が出ない）。
	defer r.reportParkLeftBehind()
	// **「先に手を離させます」は、手を離させる段を通る実行にだけ言う。**
	// **`--dry-run` では通らない**ので、そこで言うと**しない約束をしたことになる。**
	// README は「先に `--dry-run` を叩け」と勧めており、勧めた手順で嘘を出すことになる。
	switch {
	case running && !r.opts.DryRun:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonRunning, r.deps.LockPath))
	case running:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonRunningDryRun, r.deps.LockPath))
	default:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonNotRunning))
	}

	// 段2: worktree を探す。**待つ相手も消す相手も、ここで決まる。**
	found, code := r.find()
	if found == nil {
		// 段2b: worktree が無くても、残った branch は片付ける（issue #27）。
		if r.noWorktree {
			return r.abandonOrphanBranch(ctx)
		}
		return code
	}

	// 段2 の直後: これから書きうる Status の値を確かめる。**消す前に確かめる。**
	if code := r.verifyTargets(ctx, running); code != ExitOK {
		return code
	}

	// 段1 の後半: 動いているなら手を離させ、pane が消えるまで待つ。
	// **`--dry-run` では通らない。**ボードへ書き込み、エージェントに手を離させるからである。
	//
	// **手を離させる書き込みが入らなかったときは待たない。**Status が
	// `tracker.active_states` に入っていなければ park は何も書かずに戻り、
	// **継続監視は active に戻った pane しか閉じない**（3-37-3）。
	// つまり誰もその pane を閉じないので、**待てば必ず時間切れになる。**
	// そのときは、継続監視が動いていなかったときと同じ検査（stopIfPaneAlive）へ落とす。
	switch {
	case running && r.opts.DryRun:
		r.parkDeferred = true
	case running:
		if code := r.park(ctx, found); code != ExitOK {
			return code
		}
		if r.parkedTo != "" {
			if code := r.waitPaneGone(ctx, found.Path); code != ExitOK {
				return code
			}
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
		// **「失うものがある」と「調べられなかった」を言い分ける。**前者は中身を見て
		// 決められるが、後者は見る手立てが無い。同じ文言で出すと、利用者は
		// 「何が残っているのか」を探しに行ってしまう（issue #23）。
		if len(leftover.Undetermined) > 0 {
			fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrUndeterminedWithoutForce))
		} else {
			fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrLossWithoutForce))
		}
		return ExitStopped
	}
	// **手を離させる書き込みが入らなかったときこそ pane を確かめる。**書き込みが
	// 入っていなければ pane 待ち（waitPaneGone）を飛ばしているので、確かめる口がここしか無い。
	//
	// **入らない理由は2つある。**継続監視が動いていなかった場合と、動いてはいるが
	// ボードの Status が `tracker.active_states` の外だった場合である。
	// **前者はロックの判定を疑う理由になる。**ロックファイルの場所は環境変数
	// （CONTINUO_RUNTIME_DIR / XDG_RUNTIME_DIR / TMPDIR）で決まるので、launchd から
	// 起動した継続監視と端末から叩いた abandon で食い違いうる。
	// **herdr の socket は設定で決まって環境変数では動かないので、ロックより信用できる。**
	if r.parkedTo == "" {
		if code := r.stopIfPaneAlive(ctx, found.Path, running); code != ExitOK {
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
// **身元ファイルが1つも無いディレクトリも同じ扱いである**（ScanUnidentified）。
//
// 戻り値の1つ目: 見つかった worktree。見つからなかった場合と2つ以上あった場合は nil。
// 戻り値の2つ目: nil を返すときの終了コード。**0件かつ保留も0件なら ExitOK である**
// （消すものが本当に無い。runner.noWorktree を真にして、呼び出し側が段2b へ進む）。
// **0件でも保留が1件以上あれば ExitStopped である**（有無を確かめられていない）。
func (r *runner) find() (*workspace.ScannedWorktree, int) {
	scanned, err := r.deps.Workspace.Scan()
	if err != nil {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrScan, err))
		return nil, ExitStopped
	}

	var matched []workspace.ScannedWorktree
	// **判断を保留した worktree があったかどうかを数える。**あったなら、この issue の
	// branch はその worktree のものかもしれない。**段2b（残った branch の片付け）へ
	// 進んではならない**（issue #27）。
	undecided := 0

	// **身元ファイルが1つも無いディレクトリも、判断できないものとして数える。**
	// 着手は worktree を作ってから身元ファイルを書くので（3-16 の段6〜段9）、
	// **その間で落ちるとこの状態ができる。**Scan はこれを結果に含めない
	// （人間が置いた worktree かもしれないため）ので、別の口で数えなければ
	// 「保留したものは1つも無い」と見えてしまう。
	unidentified, err := r.deps.Workspace.ScanUnidentified()
	if err != nil {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrScan, err))
		return nil, ExitStopped
	}
	for _, path := range unidentified {
		// **黙って飛ばさない。**身元ファイルを読めない worktree と同じ扱いである。
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonIdentityMissing,
			path, r.deps.Workspace.IdentityPath(path)))
		r.logger.Warn("身元ファイルが無いディレクトリがあります（消しません）",
			"worktree", path, "identity_path", r.deps.Workspace.IdentityPath(path))
		undecided++
	}

	for _, w := range scanned {
		if w.Err != nil {
			// **黙って飛ばさない。**飛ばしたことを言わないと、その worktree を消しに来た
			// 人間には「worktree はありません」としか見えず、**目の前にあるものが
			// 無いことにされる。**消せない理由まで見せて、手で片付けられるようにする。
			fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonIdentityUnreadable,
				w.Path, r.deps.Workspace.IdentityPath(w.Path), w.Err))
			r.logger.Warn("身元ファイルを読めない worktree があります（消しません）",
				"worktree", w.Path, "error", w.Err)
			undecided++
			continue
		}
		if w.Identity == nil {
			undecided++
			continue
		}
		if !r.issue.SameIssue(w.Identity.IssueURL) {
			continue
		}
		if !r.pathAgrees(w) {
			undecided++
			continue
		}
		matched = append(matched, w)
	}

	switch len(matched) {
	case 0:
		// **判断を保留した worktree が1件でもあれば「ありません」と断言しない。**
		// 断言すると、目の前に worktree・branch・herdr workspace が残っているのに
		// 「もう無い」と読める1行が出て、しかも終了コード 0 で後続の手順が進む。
		// **飛ばした1行は出るが、その直後に正反対の断定が並ぶので、どちらが本当かを
		// 人間が判断できない。**確かめられなかったことを、確かめられなかったと言う。
		if undecided > 0 {
			fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrUndecided, undecided, r.issue.URL))
			r.logger.Warn("候補にできなかった worktree があるので、この issue の worktree の有無を確かめられませんでした",
				"issue", r.issue.URL, "undecided", undecided)
			r.reportToSkipped()
			return nil, ExitStopped
		}
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonNotFound, r.issue.URL))
		// **worktree が無くても、まだ終わりではない**（issue #27）。**片付けの途中で
		// 失敗すると branch だけが残る。**worktree を起点にしか探さないと、
		// そのあと何度叩いてもここで終わり、利用者は手で消すしかなくなる。
		r.noWorktree = true
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

// issueRef は internal/workspace へ渡す issue の情報を組み立てる。
//
// **人間が打った URL だけから作る。**身元ファイルの値は1つも混ぜない
// （混ぜると、裏を取るための値がエージェントの書き換えの影響を受ける）。
//
// 戻り値: 置き場所と branch 名の組み立てに要る項目を埋めた issue。
func (r *runner) issueRef() workspace.IssueRef {
	return workspace.IssueRef{
		URL:        r.issue.URL,
		Identifier: r.issue.Identifier(),
		Owner:      r.issue.Owner,
		Repo:       r.issue.Repo,
		Number:     r.issue.Number,
	}
}

// pathAgrees は、身元ファイルの `issue_url` が worktree の置き場所と辻褄が合うかを返す。
//
// **消す相手を決める値の裏を取るための検算である。**置き場所から取り出した
// owner・リポジトリ名・スラグ（最下層のディレクトリ名）は**エージェントが
// 書き換えられない**ので、書き換えられる `issue_url` の側がそれと食い違えば、
// その worktree は候補にしない。
//
// **スラグまで比べる。**owner とリポジトリ名だけを比べていたときは、
// **同じリポジトリの中なら別の issue の worktree を消せた。**issue 42 の worktree で
// 動くエージェントが自分の `issue_url` を issue 99 に書き換え、issue 99 の worktree が
// まだ無ければ、`continuo abandon <issue 99 の URL>` は候補1件として 42 の worktree と
// branch を消す。**スラグは `branch_template` から作られ、既定では issue 番号を含む。**
//
// **ホストは比べない。**置き場所の1階層目は issue の URL のホスト部だが、
// 同じ issue が GitHub Enterprise のホスト名と `github.com` の両方で書かれうる
// （`HostFromIssueURL` は URL が空なら `github.com` に倒す）。ホストまで比べると、
// **表記が違うだけの正当な worktree を候補から外す。**owner・リポジトリ名・スラグが
// 全部一致してホストだけが違う worktree が2つあるなら、それは人間が中身を見て
// 決めることであり、段2 の「候補が2件なら止まる」がそのまま効く。
//
// **候補から外すだけで、消しはしない。**食い違いの原因は書き換えとは限らず、
// 人間が置き場所を移した跡や、`branch_template` を後から変えた跡かもしれない。
// どちらであれ何があったかは1行残す。
//
// w: 身元ファイルの `issue_url` が一致した worktree。
// 戻り値: 置き場所の owner・リポジトリ名・スラグが渡された issue と一致すれば true。
func (r *runner) pathAgrees(w workspace.ScannedWorktree) bool {
	owner, repo, err := r.deps.Workspace.OwnerRepoOf(w.Path)
	if err != nil {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonOwnerRepoUnreadable, w.Path, err))
		r.logger.Warn("worktree のパスから owner とリポジトリ名を読めません（消しません）",
			"worktree", w.Path, "error", err)
		return false
	}
	// **GitHub は owner とリポジトリ名の大文字小文字を区別しない**ので、無視して比べる。
	if !strings.EqualFold(owner, r.issue.Owner) || !strings.EqualFold(repo, r.issue.Repo) {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonOwnerRepoMismatch,
			w.Path, owner, repo, w.Identity.IssueURL))
		r.logger.Warn("身元ファイルの issue_url が置き場所と食い違います（消しません）",
			"worktree", w.Path, "path_owner", owner, "path_repo", repo,
			"issue_url", w.Identity.IssueURL)
		return false
	}

	want, err := r.deps.Workspace.ExpectedSlugFor(r.issueRef())
	if err != nil {
		// **組み立てられないなら、裏を取れない。**取れないものを消しにいかない。
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonSlugUnknown, w.Path, err))
		r.logger.Warn("この issue のディレクトリ名を組み立てられません（消しません）",
			"worktree", w.Path, "error", err)
		return false
	}
	got := filepath.Base(filepath.Clean(w.Path))
	if strings.EqualFold(got, want) {
		return true
	}
	fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonSlugMismatch,
		w.Path, got, want, w.Identity.IssueURL, r.cfg.Herdr.Worktree.BranchTemplate))
	r.logger.Warn("身元ファイルの issue_url が置き場所のディレクトリ名と食い違います（消しません）",
		"worktree", w.Path, "path_slug", got, "expected_slug", want,
		"issue_url", w.Identity.IssueURL)
	return false
}

// abandonOrphanBranch は、worktree が1つも無いときに残った branch を片付ける（段2b。issue #27）。
//
// **なぜ要るか。**`abandon` は worktree を起点に対象を探す。**片付けの途中で失敗して
// branch だけが残ると、もう一度叩いても「この issue の worktree はありません」で終わる。**
// 利用者には手で `git branch -D` を叩く以外の手が無くなる。
//
// **消す相手は、身元ファイルではなく規則から決める。**読む worktree がもう無いので、
// 利用者が打った issue の URL と設定の `branch_template` から名前を組み立て、
// `ghq` が答えた clone を宛先にする（workspace.FindIssueBranch）。
// **どれもエージェントが書き換えられない値である。**
//
// **消すには `--force` が要る。**worktree が無いので、コミットしていない編集が
// 残っていたかどうかは調べようがない。**調べられないものを黙って消さない**という
// 段3 と同じ扱いにする。消したときは戻すためのコマンド（`git branch <名前> <SHA>`）を
// 1行で出す。
//
// **未 push の commit は数えて見せる。**worktree が無くても
// `git rev-list --count <branch> --not --remotes` は答える（3-37-9）。
// **`--force` を求める前に出す**ので、`--dry-run` でも見える。
//
// **Status は動かさない。**worktree が無い実行で `--to` を通さないのは、
// **URL の打ち間違いと区別できないから**である（find を見よ）。branch を1本消しても、
// その issue をボードでどこへ置くべきかは決まらない。
// **だが黙って捨てない。**止まる経路も含めて、**どの出口でも reportToSkipped を通す。**
// 指定した人間は「動いた」と受け取るので、言わずに終わるとボードの値を誤解したまま次へ進む。
// **段ごとに書かず、入り口で1度だけ仕掛ける**（run の reportParkLeftBehind と同じ形）。
// **写すと、あとから出口が増えたときに書き漏らし、ビルドもテストも気づかない。**
//
// ctx: 実行に適用するコンテキスト。
// 戻り値: 終了コード。**消した・消すものが無かった・`--dry-run` は ExitOK である。**
// **`--force` が無くて消さなかった場合と、消せなかった場合は ExitStopped である。**
func (r *runner) abandonOrphanBranch(ctx context.Context) int {
	defer r.reportToSkipped()

	branch, err := r.deps.Workspace.FindIssueBranch(ctx, r.issueRef())
	if err != nil {
		// **「残っている」とも「無い」とも言わない。**調べられなかっただけである。
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonOrphanBranchUnknown, err))
		return ExitOK
	}
	if !branch.Exists {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonOrphanBranchNone, branch.Name.String()))
		return ExitOK
	}

	fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonOrphanBranchFound,
		branch.Name.String(), branch.RepoDir, branch.Tip))
	// **失うものを、`--force` を求める前に見せる**（3-37-9）。
	// **数えられなかったことを 0 件として見せない。**
	switch {
	case branch.UnpushedErr != nil:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonOrphanBranchUnpushedUnknown, branch.UnpushedErr))
	case branch.Unpushed > 0:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonOrphanBranchUnpushed, branch.Unpushed))
	}

	switch {
	case r.opts.DryRun:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonDryRunNote))
		return ExitOK
	case !r.cfg.Cleanup.DeleteBranch:
		// **`abandon --force` でも `cleanup.delete_branch` は越えない。**
		// worktree がある経路（workspace.Cleanup）が越えないので、ここだけ越えると
		// 「worktree があると残るが、無いと消える」という筋の通らない差が生まれる。
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonOrphanBranchDisabled,
			branch.Name.String(), branch.RepoDir, branch.Name.String()))
		return ExitOK
	case !r.opts.Force:
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrOrphanBranchWithoutForce, branch.Name.String()))
		return ExitStopped
	}

	if err := r.deps.Workspace.DeleteIssueBranch(ctx, branch); err != nil {
		// **git が「worktree が使っている」と断ったときは、そのまま見せる**（3-37-9b）。
		// **一般の案内を添えてはならない。**手で `git branch -D` を叩いても同じ理由で
		// 断られる。叩くべきコマンド（`git worktree prune`）はエラーの中に入っている。
		if errors.Is(err, workspace.ErrBranchUsedByWorktree) {
			fmt.Fprintln(r.errOut, err)
			return ExitStopped
		}
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrOrphanBranchDeleteFailed,
			branch.Name.String(), err, branch.RepoDir, branch.Name.String()))
		return ExitStopped
	}
	fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonOrphanBranchRemoved,
		branch.Name.String(), branch.RepoDir, branch.RepoDir, branch.Name.String(), branch.Tip))
	return ExitOK
}

// reportToSkipped は、`--to` を指定されたのに Status を動かさなかったことを言う。
//
// **`--to` を黙って捨てない。**指定した人間は「動いた」と受け取るが、
// worktree が無い経路ではボードに1文字も書かれない。
// **代わりに段5 を通すことはしない。**worktree が無い理由は「もう片付けた」とは限らず、
// **URL の打ち間違い**でもある。打ち間違えた相手の Status を動かすほうが害が大きい。
func (r *runner) reportToSkipped() {
	if target := strings.TrimSpace(r.opts.ToState); target != "" {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonToSkipped, target))
	}
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

// stopIfPaneAlive は、手を離させる書き込みを行っていない実行で pane の生死を確かめる（段4 の前）。
//
// **継続監視が動いていない実行だけではない。**動いていても、ボードの Status が
// `tracker.active_states` の外なら park は何も書かずに戻るので、その実行もここへ来る。
// **どちらの原因かは呼ぶ側が知っているので、running で受け取って文言を選ぶ**
// （i18n の `abandon.err_pane_alive_not_running` と `abandon.err_pane_alive_running`）。
// **1つの文言で受けると「『continuo は動いていません』と表示されていたなら」のような
// 条件付きの案内になり、別の文言の文面を直書きすることになる。**
//
// **ロックだけを根拠に消しにいかない。**ロックファイルの場所は環境変数で決まるので、
// launchd から起動した継続監視と端末から叩いた abandon で食い違いうる。食い違えば
// 「動いていない」と判定したまま、生きた pane ごと worktree を消す。
//
// **待たずに止める。**手を離させていない以上、待っても pane は閉じない。
//
// **herdr に問い合わせられないときは `--force` を要求する**（issue #23）。
// herdr が落ちていると、この確かめ方そのものが成り立たない。**そこで無条件に止まると、
// herdr ごと壊れた状況で worktree を1つも片付けられない。**`--force` は
// 「調べられなくても消せ」という人間の明示であり、Inspect の扱いと同じにする。
//
// **pane が生きていても `--force` なら消す**（issue #23）。
// **continuo が worktree のために開いた herdr workspace には、その worktree を cwd に持つ
// pane が必ず1枚ある**（`worktree.open` が root pane を作る。実測: 2026-08-25）。
// **つまり workspace が開いているかぎり、この検査は必ず引っかかる。**
// そこで無条件に止めると、**`abandon` が消すはずの workspace が、`abandon` を止める。**
// 人間には手が無くなり、herdr workspace を手で閉じてから叩き直すしかなくなる。
//
// **`--force` が無いときは今までどおり止まる。**止まる文言には `--force` で越えられることを
// 書く（越え方が分からなければ、止まったことと詰まったことは同じである）。
// **herdr が答えないときの文言も同じである**（`abandon.err_pane_list_check`）。
// **pane 待ち（waitPaneGone）も同じ鍵を使う。**どちらも `--force` で越えるので、
// **鍵を分けると同じ失敗に越え方を書いた文言と書いていない文言が並ぶ。**
//
// **「herdr が答えられない」より「pane がある」のほうを厳しくしない。**
// 前者で消せて後者で消せないのは筋が通らない。どちらも `--force` で越える。
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 対象の worktree の絶対パス。
// running: 継続監視が動いていると判定したか（**止まる文言の選び分けに使う**）。
// 戻り値: pane が無ければ ExitOK、1件でもあれば `--force` の有無で ExitOK / ExitStopped。
// herdr に問い合わせられない場合も、`--force` があれば ExitOK、無ければ ExitStopped。
func (r *runner) stopIfPaneAlive(ctx context.Context, worktreePath string, running bool) int {
	panes, err := r.panesOf(ctx, worktreePath)
	if err != nil {
		if !r.opts.Force {
			fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrPaneListCheck, err))
			return ExitStopped
		}
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPaneCheckSkipped, err))
		return ExitOK
	}
	if len(panes) == 0 {
		return ExitOK
	}
	ids := strings.Join(paneIDs(panes), " ")
	if r.opts.Force {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPaneAliveForced, ids))
		return ExitOK
	}
	if running {
		// **動いていると判定できている実行に、ロックの食い違いを疑わせない。**
		// 無いものを探しに行かせることになる。
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrPaneAliveRunning, ids))
		return ExitStopped
	}
	fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrPaneAliveNotRunning, ids, r.deps.LockPath))
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
	moved, err := tr.UpdateStatus(ctx, itemID, target, nil)
	if err != nil {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrParkFailed, target, err))
		return ExitStopped
	}
	if !moved.Reached {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonParkNotWritten, target))
		return ExitStopped
	}
	// **持ち回っている Status を書いた値へ更新する。**ボードは1回しか読まないので、
	// 更新しないと段3 の計画表示が park の**前**の値を出す（人間には、これから消す
	// worktree の issue がまだ作業中に見える）。
	r.board.State = target
	r.parkedTo = target
	fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonParkMoved, state, target, found.Path))
	return ExitOK
}

// waitPaneGone は、その worktree を cwd に持つ pane が無くなるまで待つ（段1 の後半）。
//
// **消えるまで待たないと、消した worktree の中で Claude Code が動き続ける。**
// 上限を超えたら**何も消さずに止まる。ただし `--force` があれば pane ごと消す。**
//
// **呼ぶのは、手を離させる書き込みが実際に入ったときだけである。**書き込みが入って
// いなければ継続監視はその pane を閉じないので、待っても必ず時間切れになる。
//
// **herdr が答えないときも `--force` で越える。**兄弟の検査（stopIfPaneAlive）は同じ失敗を
// 越えさせるので、**ここだけ越えられないと「herdr が答えられない」が「pane がある」より
// 厳しくなる。**しかもここへ来る実行は**ボードを park の値へ動かし終えている**ので、
// 越えられないと**ボードだけ動いた状態のまま、herdr を直すまで取り消せない。**
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: 対象の worktree の絶対パス。
// 戻り値: pane が消えたら ExitOK、上限を超えた場合と herdr に問い合わせられない場合は
// `--force` の有無で ExitOK / ExitStopped。
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
			// **herdr の失敗を `--force` で越えられない壁にしない**（stopIfPaneAlive と同じ扱い）。
			// **ここへ来た実行はボードを park の値へ動かし終えている。**越えられないと、
			// herdr を直すまでボードだけ動いた状態が残る。
			if !r.opts.Force {
				fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrPaneListCheck, err))
				return ExitStopped
			}
			fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPaneCheckSkipped, err))
			return ExitOK
		}
		if len(panes) == 0 {
			fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPaneGone))
			return ExitOK
		}
		if !r.deps.Now().Before(deadline) {
			ids := strings.Join(paneIDs(panes), " ")
			// **`--force` なら pane ごと消す。**止まったままにすると、
			// **herdr workspace を手で閉じるまでその issue を取り消せない。**
			// 手を離させていない側の同じ検査（stopIfPaneAlive）には元から逃げ道があり、
			// **こちらだけ越えられないのは筋が通らない。**
			if r.opts.Force {
				fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPaneAliveForced, ids))
				return ExitOK
			}
			fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrPaneRemains, timeout, ids))
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

// reportParkLeftBehind は、手を離させる書き込みを済ませたあとに何も消さずに止まったとき、
// Status がその値のまま残ることを人間へ言う。
//
// **「何も消していません」だけでは足りない。**ボードは既に書き換わっており、
// **消さなかったのだから元のままだろう**と読まれると、その issue はそこに置き去りになる。
//
// **continuo は元へ戻さない。**戻した瞬間に、動いている継続監視がその issue を
// 拾い直しうる（戻す先は `tracker.active_states` の値である）。戻すかどうかは人間が決める。
//
// **出さないのは、段5 が Status について応答し終えたときだけである。**
// **「消せたかどうか」で決めない。**片付けに成功しても `--to` の書き込みに失敗すると
// 段5 は Status の在りかを言わずに終わるので、そこで黙ると、**worktree は消えたのに
// Status が park の値のまま残ったことを誰も言わない。**
func (r *runner) reportParkLeftBehind() {
	if r.parkedTo == "" || r.statusSettled {
		return
	}
	fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonParkLeftBehind, r.parkedTo))
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
	//
	// **残ったものは1行ずつ画面へ出す**（issue #23）。`continuo abandon` は
	// `Options.Logger` を渡さないので、**ログに書いたものは誰にも届かない。**
	// 「ログを見てください」で済ませると、branch も herdr の workspace も
	// 黙って残ったまま「消しました」だけが見える。
	//
	// **残ったものが1件も無ければ branch も消えている。**branch が残る経路は
	// 3つ（設定で無効・検算に落ちた・`git branch -D` が失敗）あり、**どれも Leftovers を積む。**
	//
	// **例外は「branch がリポジトリに実在しなかった」場合である**（issue #27）。
	// 消す対象が無かっただけなので残ったものには積まないが、**「消しました」とも言わない。**
	//
	// **continuo が自分で行ったことは、残ったものと別に必ず出す**（issue #28）。
	// 壊れた ref のファイルを1つ消したことがここに入る。**`.git` の中のファイルを
	// continuo が消したという事実を、人間が知る手立てを画面に残す。**
	for _, notice := range result.Notices {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonNotice, notice))
	}
	if len(result.Leftovers) == 0 {
		if result.BranchAbsent {
			fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonRemovedBranchAbsent, worktreePath, branch))
			return ExitOK
		}
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonRemoved, worktreePath, branch))
		return ExitOK
	}
	fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonRemovedWithLeftovers, worktreePath))
	for _, left := range result.Leftovers {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonLeftover, left))
	}
	return ExitOK
}

// moveStatus は片付けたあとの Status を決める（段5）。
//
// **`--to` が無ければ動かさない。**どこへ置くかは、その issue をこれからどうするかで
// 決まる。continuo が勝手に決めてよいものではない。
//
// **ただし park で動かした実行に「動かしていません」と言ってはならない。**
// 手を離させるために動かしたのは continuo である。**そこで「動かしていません」と言うと、
// ボードが park の値になっていることを誰も言わないまま終わる**（この1行が
// reportParkLeftBehind を黙らせる）。**park で動かしたときは、その1行に言わせる。**
//
// ctx: 実行に適用するコンテキスト。
// 戻り値: ExitOK か、書き込みに失敗した場合の ExitStopped。
func (r *runner) moveStatus(ctx context.Context) int {
	target := strings.TrimSpace(r.opts.ToState)
	if target == "" {
		if r.parkedTo != "" {
			// **statusSettled を立てない。**立てると reportParkLeftBehind が黙り、
			// **ボードが park の値のまま残ったことを誰も言わなくなる。**
			return ExitOK
		}
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonStatusLeftAlone))
		r.statusSettled = true
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
	moved, err := tr.UpdateStatus(ctx, itemID, target, nil)
	if err != nil {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonErrStatusFailed, target, err))
		return ExitStopped
	}
	if !moved.Reached {
		fmt.Fprintln(r.errOut, i18n.T(i18n.KeyAbandonStatusNotWritten, target))
		return ExitStopped
	}
	fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonStatusMoved, target))
	r.statusSettled = true
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
	//
	// **git が答えられなかったときは数を出さない。**そこで `0 ファイル` と出すと、
	// **成果の残った worktree が「失うものはありません」に見える**（issue #23）。
	switch {
	case leftover.DirtyFilesUnknown:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanDirtyUnknown))
	case leftover.DirtyFilesTruncated:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanDirtyAtLeast, leftover.DirtyFiles))
	default:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanDirty, leftover.DirtyFiles))
	}
	switch {
	case leftover.UnpushedUnknown:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanUnpushedUnknown))
	case leftover.HasUpstream:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanUnpushed, leftover.UnpushedCommits))
	case leftover.BaseUnknown:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanBaseUnknown))
	case leftover.DiffFromBase:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanDiffFromBase, leftover.Base))
	default:
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanNoDiffFromBase, leftover.Base))
	}

	// **調べられなかった理由を必ず1行ずつ出す。**「失うものが無い」と読まれないように、
	// 何を調べられなかったのかと、git が何と言ったのかをそのまま見せる。
	for _, reason := range leftover.Undetermined {
		fmt.Fprintln(r.out, i18n.T(i18n.KeyAbandonPlanUndetermined, reason))
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
