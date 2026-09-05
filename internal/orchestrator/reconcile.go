package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// resumeBackoff はバックオフが明けた run を拾い直す（設計 3-21 / 3-25）。
//
// **巡回の先頭で印の集合を1回走査する。**候補の取得より前に行うのは、空きスロットの計算
// （3-16 の段-1）にこの結果が影響するためである。
//
//	BackoffUntil がゼロ値、または未来  … 何もしない
//	BackoffUntil を過ぎている          … その run を再 dispatch する（段0 から入り直す）
//
// ctx: 呼び出しに適用するコンテキスト。
// dispatchAllowed: この巡回で dispatch してよいか（偽なら再 dispatch も見送る）。
func (o *Orchestrator) resumeBackoff(ctx context.Context, dispatchAllowed bool) {
	if !dispatchAllowed {
		// **この巡回は dispatch を見送ると決まっている**（Status の選択肢名か gh の認証が
		// 検査に落ちた）。再 dispatch も着手の段0 から入り直す dispatch なので同じく見送る。
		// **バックオフの印は残す**ので、次の巡回でまた拾える。
		return
	}
	now := o.now()
	for _, rs := range o.snapshotRuns() {
		snap := rs.snapshot()
		if snap.BackoffUntil.IsZero() || now.Before(snap.BackoffUntil) {
			continue
		}
		o.logger.Info("バックオフが明けたので再 dispatch します",
			"identifier", snap.Identifier, "retry_count", snap.RetryCount)
		o.redispatch(ctx, rs)
	}
}

// reconcileRunning は実行中の issue の Status を ID 指定で取り直して照合する
// （巡回の GraphQL リクエストの2本目。`SPEC.md` 8.5 Part B / 設計 3-10）。
//
//	terminal_states           … worker を止めて workspace を掃除する
//	active_states かつ routable … 手元のスナップショットを更新する
//	active_states だが routable でない … **workspace を掃除せずに** worker を止める
//	それ以外（引き渡し・見えない） … **workspace を掃除せずに** worker を止める
//
// **終端と引き渡しは、書いたのがボードの自動化なら turn の終わりを待つ**
// （`holdForAutomatedMove`。設計 3-74）。**人間が動かしたときはいままでどおり即座に止める。**
//
// **`active_states` のまま routable でなくなった run は、その待ちの対象にしない。**
// Status の引き渡しではなく、リポジトリの信頼登録が外れた等の理由で止めるのだから、
// **Status を誰が書いたかで振る舞いを変えてはならない。**
//
// **バックオフ待ちの run は触らない。**再 dispatch を待っている最中である。
//
// **worker を止める処理は別の goroutine で回す**（設計 3-8）。ここは巡回のループの
// 中であり、**`agent.prompt` を待ち受けつきで呼ぶ経路をブロックしたまま通してはならない。**
//
// ctx: 呼び出しに適用するコンテキスト。
func (o *Orchestrator) reconcileRunning(ctx context.Context) {
	runs := o.snapshotRuns()
	if len(runs) == 0 {
		return
	}

	ids := make([]string, 0, len(runs))
	byID := make(map[string]*runState, len(runs))
	now := o.now()
	for _, rs := range runs {
		snap := rs.snapshot()
		if !snap.BackoffUntil.IsZero() && now.Before(snap.BackoffUntil) {
			continue
		}
		ids = append(ids, snap.IssueID)
		byID[snap.IssueID] = rs
	}
	if len(ids) == 0 {
		return
	}

	// **ここは「誰が Status を書いたか」も取る**（設計 3-61）。知らない Status になったとき、
	// 書き戻すか止めるかをその記録で決める（`handleUnknownState`）。
	issues, err := o.tracker.FetchIssuesByIDs(ctx, ids)
	if err != nil {
		o.logger.Warn("実行中の issue を取り直せません（この巡回では照合しません）", "error", err)
		return
	}

	seen := map[string]bool{}
	for _, issue := range issues {
		seen[issue.ID] = true
		rs, ok := byID[issue.ID]
		if !ok {
			continue
		}
		rs.setIssue(issue)

		switch {
		case containsFold(o.cfg.Tracker.TerminalStates, issue.State):
			// **書いたのがボードの自動化なら、turn の終わりを待つ**（設計 3-74）。
			// 「PR がマージされたら Done」の自動化が turn の途中で走ると、
			// **走っている Claude Code を continuo 自身が殺してしまう。**
			if o.holdForAutomatedMove(rs, issue) {
				continue
			}
			// **同期で呼んではならない**（設計 3-8）。片付けの前にコメントを確かめる
			// 経路（3-25 の9段）は `agent.prompt` を待ち受けつきで呼び、既定では最大
			// 1時間返らない。巡回のループがそこで止まると、dispatch も stall 検知も
			// 全部止まる。
			o.finishRunAsync(ctx, rs, "", fmt.Sprintf("Status が %s になっていました", issue.State))
		case containsFold(o.cfg.Tracker.ActiveStates, issue.State) && issue.Dispatchable:
			// まだ作業中で routable である。スナップショットの更新だけ。
			// **外から動かされていた記録は消す**（設計 3-50 / 3-74）。エージェントが表明で
			// 戻したのだから、猶予の起点も捨てる。
			rs.clearExternalMove()
		case issue.State != "" && !o.isKnownState(issue.State):
			// **continuo が知らない Status である**（設計 3-50）。黙って止めない。
			o.handleUnknownState(ctx, rs, issue)
		case containsFold(o.cfg.Tracker.ActiveStates, issue.State):
			// **Status は作業中のままだが routable でない**（設計 3-13）。リポジトリの信頼
			// 登録が外れた場合などがここへ来る。**Status の引き渡しではないので、書いたのが
			// 自動化かどうかを見ない**（設計 3-74）。待っても routable には戻らない。
			o.logger.Info("作業中の Status のままですが dispatch できなくなったので worker を止めます（worktree は残します）",
				"identifier", issue.Identifier, "状態", issue.State)
			o.stopAndReleaseAsync(ctx, rs)
		default:
			// 引き渡し（`In Review` / `Blocked` など、設定に名前が出てくる Status）。
			// **continuo 自身が書いた Status なら、turn の終わりの経路が処理中である**（設計 3-74c）。
			if o.holdForOwnMove(rs, issue) {
				continue
			}
			// **ここも書いたのが自動化なら turn の終わりを待つ**（設計 3-74）。
			if o.holdForAutomatedMove(rs, issue) {
				continue
			}
			o.logger.Info("作業中でも完了でもない状態になったので worker を止めます（worktree は残します）",
				"identifier", issue.Identifier, "状態", issue.State)
			o.stopAndReleaseAsync(ctx, rs)
		}
	}

	for id, rs := range byID {
		if seen[id] {
			continue
		}
		o.logger.Warn("issue がカンバンから見えなくなったので印から外します（continuo は面倒を見ません）",
			"identifier", rs.issue().Identifier)
		o.stopAndReleaseAsync(ctx, rs)
	}
}

// reconcileWorktrees は worktree を走査して身元ファイルを読み、Status を ID 指定で
// 取り直して照合する（巡回の GraphQL リクエストの3本目。設計 3-9 の手順7）。
//
//	cleanup.on_states に入っている            … worktree と branch を片付ける
//	active_states に戻っていて pane が生きている … その pane を閉じる（手順7b）
//	それ以外（引き渡し・見えない）             … 何もしない。**pane も worktree も残す**
//
// **印に入っている worktree はここでは触らない。**実行中の照合（reconcileRunning）が見る。
//
// ctx: 呼び出しに適用するコンテキスト。
func (o *Orchestrator) reconcileWorktrees(ctx context.Context) {
	scanned, err := o.ws.Scan()
	if err != nil {
		o.logger.Warn("worktree の置き場所を走査できません", "error", err)
		return
	}

	type orphan struct {
		path     string
		identity *workspace.Identity
	}
	var orphans []orphan
	ids := make([]string, 0, len(scanned))
	for _, w := range scanned {
		if w.Identity == nil {
			continue
		}
		if _, claimed := o.lookupRunByID(w.Identity.ProjectItemID); claimed {
			continue
		}
		orphans = append(orphans, orphan{path: w.Path, identity: w.Identity})
		ids = append(ids, w.Identity.ProjectItemID)
	}
	if len(orphans) == 0 {
		return
	}

	// **「誰が Status を書いたか」は取らない**（設計 3-61）。見るのは `State` が
	// `cleanup.on_states` に入っているかだけである。
	issues, err := o.tracker.FetchIssuesByIDsWithoutTimeline(ctx, ids)
	if err != nil {
		o.logger.Warn("worktree の照合で issue を取り直せません", "error", err)
		return
	}
	states := make(map[string]tracker.Issue, len(issues))
	for _, issue := range issues {
		states[issue.ID] = issue
	}

	for _, orph := range orphans {
		issue, ok := states[orph.identity.ProjectItemID]
		if !ok {
			// もう見えない issue の worktree。**勝手に消さない**（設計 3-4）。
			continue
		}
		if o.ws.ShouldCleanup(issue.State) {
			// 手順7: `cleanup.on_states` に入っていれば片付ける。
			// **ここで pane を閉じない。**`worktree.remove` の応答は workspace ごと
			// 閉じるので、その中の pane も一緒に消える（設計 3-9 の手順3）。
			result, err := o.ws.Cleanup(ctx, workspace.CleanupRequest{WorktreePath: orph.path})
			if err != nil {
				o.logger.Warn("取り残された worktree を片付けられません",
					"identifier", issue.Identifier, "path", orph.path, "error", err)
				continue
			}
			if result.Removed {
				o.logger.Info("取り残された worktree を片付けました",
					"identifier", issue.Identifier, "path", orph.path)
			}
			continue
		}
		// 手順7b: **Status が `active_states` に戻ったときだけ** pane を閉じる。
		// この条件を外してはならない。**`In Review` / `Blocked` の run は、復元が
		// 「pane も worktree も残す」と決めて印に入れていない**（設計 3-4 の段5a）。
		// 条件なしに閉じると、復元の直後の巡回が、人間のレビュー待ちで正常に
		// 止まっている Claude Code を毎巡回で落とす。
		if containsFold(o.cfg.Tracker.ActiveStates, issue.State) {
			o.closeOrphanPane(ctx, orph.path, orph.identity)
		}
	}
}

// releaseQuotaWaitExceeded は、1週間の枠を待つ上限を超えた run を手放す
// （設計 3-27。issue #197）。
//
// **枠待ちの印が立っている run について、毎巡回で必ず通る唯一の場所である。**
// turn 側の待ちループは、herdr が一時的に届かないと**印を外さずに goroutine を畳む**ので、
// 走っていない窓がある。
//
// **`checkStalls` の `claude.turn_timeout_ms` の門より前で呼ぶ。**
// **0 以下でも枠待ちの印は立つ**ので、門のあとに置くとその設定の機械で一度も効かない。
//
// **非同期に手放す。**担当者を外す要求とコメントの投稿と pane を閉じる要求が乗るので、
// **同じ巡回で複数の run が超えると直列に積まれる**（設計 3-8）。
//
// ctx: 呼び出しに適用するコンテキスト。
func (o *Orchestrator) releaseQuotaWaitExceeded(ctx context.Context) {
	for _, rs := range o.snapshotRuns() {
		if !rs.snapshot().WaitingQuota {
			continue
		}
		if !o.weeklyWaitExceeded(rs) {
			continue
		}
		// **画面の判定は `releaseBecauseQuotaWaitClaimed` の中で行う。**
		// **ここでやってはならない。**画面は herdr、担当は GitHub と、別々の外部に依存する。
		// **前に置くと、herdr が落ちているあいだ担当の確認が1回も走らない。**
		// **枠待ちの run に turn の終わりは来ない**ので、担当を確かめる経路は他に無い。
		o.releaseBecauseQuotaWaitAsync(ctx, rs)
	}
}

// screenState は、手放してよいかを画面の版から見た答えである（設計 3-27。issue #197）。
type screenState int

const (
	// screenStill は、画面が窓のあいだ止まっていることを表す。**手放してよい。**
	screenStill screenState = iota
	// screenMoving は、画面が動いていることを表す。
	//
	// **手放さない。**枠を待っているのではなく、長い1つのツール呼び出しの最中である。
	// **枠待ちの標識も外す。**枠を待っていない run に「枠待ち」と名乗らせたまま
	// stall の時計を止めると、そのあと本当に固まっても誰も止めない。
	screenMoving
	// screenUnknown は、画面の版を読めなかったことを表す。
	//
	// **手放さない。**ただし**枠待ちの標識はそのままにする。**
	// 外すと、その run は枠待ちでも stall の対象でもなくなり、どちらの経路からも見えなくなる。
	screenUnknown
)

// screenStillWindow は、手放してよいと判断するために画面が止まっていなければならない長さを返す
// （設計 3-27。issue #197）。
//
// **`claude.turn_timeout_ms`（既定1時間）である。巡回の間隔（既定30秒）ではない。**
// この段の根拠は「1時間を超える1回のツール呼び出し」なので、
// **30秒動かない窓を1つ拾っただけで手放すと、90分のビルドの最中に殺すことになる。**
//
// **0 以下のときは `rate_limit.weekly_wait_limit_minutes` を使う。**
// **窓を外してはならない。**外すと、打ち切りの判定を切った機械だけが
// 「30秒動かない窓を1つ拾って手放す」形になる。
// **新しい定数を置かない。**運用者が既に決めた長さのうち、この判定に近いのはこちらである。
//
// 戻り値: 画面が止まっていなければならない長さ。
func (o *Orchestrator) screenStillWindow() time.Duration {
	if ms := o.cfg.Claude.TurnTimeoutMs; ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return time.Duration(o.cfg.RateLimit.WeeklyWaitLimitMinutes) * time.Minute
}

// checkScreenForRelease は、手放してよいかを画面の版から判定する（設計 3-27。issue #197）。
//
// **枠待ちの条件は「使用率が100」と「hook が来ていない」の2つで、
// これは「枠を待っている」と「長い1つの仕事をしている」を区別できない。**
// hook はツールが終わってから飛ぶので、**1時間を超える1回のツール呼び出しの最中は1件も来ない。**
// そこへ `weekly_scoped`（使っていないモデルの週次の枠）が100%だと条件が両方そろい、
// **正常に走っている run を手放して pane を閉じることになる。**
//
// **区別できる道具は画面の版である**（設計 3-21。`checkStalls` の段2 が同じものを見る）。
// **あちらは枠待ちの枝の後ろにあるので、この経路には届かない。**だからここで見る。
//
// **読めなかったときは `screenUnknown` を返す。**`checkStalls` の段2 とは向きを変える。
// **あちらが失うのは「worker を止めてリトライを積む」までで、担当も worktree もこの機械に残る。**
// **こちらが失うのは `git push`・担当者・pane・会話の文脈で、取り返しがつかない。**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値: 画面から見た答え。
func (o *Orchestrator) checkScreenForRelease(ctx context.Context, rs *runState) screenState {
	identifier := rs.issue().Identifier
	agent, err := o.agentInfo(ctx, rs)
	if err != nil {
		o.logger.Warn("手放す前に画面の版を読めませんでした（手放さずに次の巡回でやり直します）",
			"identifier", identifier, "error", err)
		return screenUnknown
	}
	still, changed := rs.noteQuotaScreen(agent.Revision, o.now())
	if changed {
		o.logger.Info("画面が変わっているので、1週間の枠の上限を超えていても手放しません"+
			"（枠待ちではなく、長い1つの turn です）",
			"identifier", identifier,
			"revision", agent.Revision,
			"agent_status", string(agent.AgentStatus))
		return screenMoving
	}
	window := o.screenStillWindow()
	if window > 0 && still < window {
		// **「まだ足りない」であって「動いている」ではない。**
		// **`screenMoving` を返してはならない。**あちらは枠待ちの標識を外すので、
		// **初回の観測と、窓に届いていない観測が、待ちに入った run を毎回追い出す。**
		o.logger.Debug("画面が止まってからの長さが窓に届かないので、"+
			"1週間の枠の上限を超えていても手放しません",
			"identifier", identifier,
			"revision", agent.Revision,
			"止まってからの長さ", still.Round(time.Second).String(),
			"窓", window.Round(time.Second).String())
		return screenUnknown
	}
	return screenStill
}

// closeOrphanPane は印に入っていない worktree に付いている pane を閉じる
// （設計 3-9 の手順7b）。
//
// **閉じないと、次の巡回で同じ worktree に2つ目の Claude Code が立つ。**
//
// **身元ファイルの `herdr_workspace_id` を宛先にしてはならない。**身元ファイルは
// worktree の直下にあり、その worktree ではエージェントが `--permission-mode dontAsk` で
// 動く（設計 3-16 の段9）。**つまりこの値はエージェントが書き換えられる。**
// 書き換えられた値をそのまま `pane.close` へ渡すと、**同じ機械で走っている別の run の
// Claude Code を turn の途中で殺せる。**
//
// **そこで身元ファイルを1つも使わず、herdr 自身に答えさせる。**`pane.list` を絞り込みなしで
// 引き、**pane の `cwd` がこの worktree のパスと同じ場所を指すものだけ**を閉じる。
// worktree のパスは封じ込め検査（設計 3-20）を通った置き場所の内側の実体であり、
// エージェントには書き換えられない。**照合はシンボリックリンクを解決してから行う**
// （置き場所は解決済みだが、pane の cwd は起動時の文字列がそのまま入りうる。設計 3-4 の段4）。
//
// ctx: 呼び出しに適用するコンテキスト。
// worktreePath: 対象の worktree の絶対パス（走査で得た値）。
// identity: worktree の身元ファイル（**ログに出す issue の名前にだけ使う**）。
func (o *Orchestrator) closeOrphanPane(ctx context.Context, worktreePath string, identity *workspace.Identity) {
	want, ok := resolvePath(worktreePath)
	if !ok {
		// 解決できないパスは突き合わせの対象から外す（設計 3-4 の段4 と同じ判断）。
		o.logger.Warn("worktree のパスを解決できないので pane は閉じません",
			"identifier", identity.IssueIdentifier, "path", worktreePath)
		return
	}
	list, err := o.herdr.PaneList(ctx, herdr.PaneListParams{})
	if err != nil {
		o.logger.Warn("pane の一覧を取れないので pane は閉じません",
			"identifier", identity.IssueIdentifier, "path", worktreePath, "error", err)
		return
	}
	for _, p := range list.Panes {
		if p.Agent == "" {
			continue
		}
		got, ok := resolvePath(p.Cwd)
		if !ok || got != want {
			continue
		}
		o.logger.Warn("印に入っていない worktree に生きた pane があったので閉じます",
			"identifier", identity.IssueIdentifier, "pane_id", p.PaneID, "cwd", p.Cwd)
		if _, err := o.herdr.PaneClose(ctx, herdr.PaneCloseParams{PaneID: p.PaneID}); err != nil {
			o.logger.Warn("pane を閉じられませんでした", "pane_id", p.PaneID, "error", err)
		}
	}
}

// checkStalls は stall を判定する（設計 3-21 / 3-27 の評価順）。
//
// **測るのは「画面が変わらない時間」であって、turn の総実行時間ではない。**
// `SPEC.md` 10.6 は `turn_timeout_ms` を *"maximum silence interval while a turn stream is
// active; each app-server output resets it, so it is not a total turn runtime cap"*
// （turn の流れが動いている間の最大の沈黙の間隔。app-server の出力ごとにリセットされる。
// 総実行時間の上限ではない）と定めている。continuo には app-server が無いので、
// **「app-server の出力」に相当するものを herdr の pane の `revision`（画面の版）で測る。**
//
// **時計が動いていない run について、上から順に見る。**
//
//  1. 枠待ちか（percent が 100 かつ この run から hook が来ていない）
//     → 枠待ちなら「時計を止めている」印を付けて終わり。**殺さない**
//  2. 画面の版が増えているか（agent.get の `revision`）
//     → 増えていれば時計を起こし直す。**1つの turn に何時間かかっていても打ち切らない**
//  3. 版が増えていない
//     → worker を止め、リトライを積む
//
// **枠待ちの run は判定そのものを飛ばす**（`WaitingQuota` が立っている間は時計が止まっている）。
// **`LastSeenAt` は進めない**（進めると、枠が明けたあとに「最後に動いていた時刻」が分からなくなる）。
//
// **`claude.turn_timeout_ms` が 0 以下なら判定そのものを行わない**（`SPEC.md` 8.4 の
// *"If stall_timeout_ms <= 0, skip stall detection entirely"*）。
//
// ctx: 呼び出しに適用するコンテキスト。
func (o *Orchestrator) checkStalls(ctx context.Context) {
	// **1週間の枠を待つ上限は、打ち切りの判定を切っていても効かせる**（設計 3-27。issue #197）。
	// **下の `silence <= 0` より前に呼ぶ。**`claude.turn_timeout_ms` が 0 以下でも
	// **枠待ちの印は立つ**（`isQuotaWaiting` は hook を1件も受けていない run では
	// 無音の長さを見ない）。**あとに置くと、その設定の機械で上限が一度も効かない。**
	o.releaseQuotaWaitExceeded(ctx)

	silence := time.Duration(o.cfg.Claude.TurnTimeoutMs) * time.Millisecond
	if silence <= 0 {
		return
	}
	now := o.now()
	// **満杯の1週間の枠があるかを、1回のロックで取った写しから見る**（設計 3-27。issue #197）。
	// **run ごとに取り直さない。**同じ巡回の中で違う写しの答えが混ざる。
	quotaSnap, _ := o.quotaSnapshotWithStale()
	weeklyFull := quotaSnap.HasFullKind(handoff.IsWeeklyKind)

	for _, rs := range o.snapshotRuns() {
		// **満杯を最初に見た時刻は、枠待ちの標識の有無によらず控える**（設計 3-27）。
		// **`weeklyWaitExceeded` の中だけで控えてはならない。**あれは標識が立っている run しか
		// 通らないので、**標識が別の経路で外れると、以後どこからも消されない。**
		// **消されないと、何日か普通に動いたあと1週間の枠がもう一度満杯になったときに、
		// 何日も前の時刻との差で「上限を超えた」と判定し、1分も待たずに手放す。**
		rs.noteWeeklyFull(weeklyFull, now)

		snap := rs.snapshot()
		if snap.WaitingQuota {
			// **上限は上の `releaseQuotaWaitExceeded` が見ている。**ここでは見ない。
			//
			// 枠が明けたら印を外す。契機は2つある。
			//
			// **1つ目は `resets_at` を過ぎたこと。**
			// **2つ目は、使い切っている枠が1つも無くなったこと。**
			//
			// **2つ目を落としてはならない。**`resets_at` が `null` で返る枠だけが満杯だと、
			// `QuotaResetAt` はゼロ値のままで、**1つ目では永久に外れない。**
			// turn のループは同じ判定を持っている（`quotaAtFull` を見て外す）が、
			// **herdr が一時的に届かないと、待ちループは印を外さずに goroutine を畳む**
			// （設計 3-27）。**そのとき外す者が1人もいなくなり、run はスロットと pane を
			// continuo の再起動まで握り続ける。**
			//
			// **`weekly_wait_limit_minutes: 0`（上限を設けない）で必ず当たる。**
			// [docs/FAQ.md](../../docs/FAQ.md) が1台で動かす人に勧めている値である。
			switch {
			case !snap.QuotaResetAt.IsZero() && !now.Before(snap.QuotaResetAt):
				rs.clearWaitingQuota(now)
			case !o.quotaAtFull():
				o.logger.Info("使い切っている枠が無くなったので、枠待ちの印を外します",
					"identifier", snap.Identifier)
				rs.clearWaitingQuota(now)
			}
			continue
		}
		if !snap.BackoffUntil.IsZero() && now.Before(snap.BackoffUntil) {
			continue
		}
		if snap.AgentName == "" || snap.LastSeenAt.IsZero() {
			continue
		}
		if now.Sub(snap.LastSeenAt) < silence {
			continue
		}

		// 1. 枠待ちを先に見る。
		if o.isQuotaWaiting(rs) {
			// **待ちに入る前に上限を見る**（設計 3-27。issue #197）。
			// **超えていたら印を立てない。**立てると、手放したあとに印だけが残る。
			//
			// **手放す前に画面の版を見る。**ここは枠待ちと初めて判定する場所で、
			// **下の段2（画面の版）へは進まない。**見ないと、1時間を超える1回のツール呼び出しの
			// 最中の run を、使っていないモデルの週次の枠が満杯なだけで殺すことになる。
			if o.weeklyWaitExceeded(rs) {
				switch o.checkScreenForRelease(ctx, rs) {
				case screenStill:
					o.releaseBecauseQuotaWaitAsync(ctx, rs)
					continue
				case screenMoving:
					// **画面が動いているなら、枠待ちの標識も立てない。**
					// **立てると、長い1つの turn を「枠を待っている」と名乗ったまま
					// stall の時計を止めることになり、そのあと本当に固まっても誰も止めない。**
					continue
				case screenUnknown:
					// **読めなかったときは、枠待ちの標識を立てて下へ進む。**
					// **立てないと、その run は枠待ちでも stall の対象でもなくなり、
					// どちらの経路からも見えなくなる。**
				}
			}
			resetAt, _ := o.quotaResetAt()
			rs.setWaitingQuota(resetAt)
			o.logger.Info("枠待ちと判定したので stall の時計を止めます",
				"identifier", snap.Identifier, "resets_at", resetAt)
			continue
		}

		// 2. agent.get で状態と画面の版を1回で取る。
		// **版が増えていれば、何時間かかっていても待ち続ける。**
		agent, err := o.agentInfo(ctx, rs)
		if err == nil && rs.noteRevision(agent.Revision, now) {
			o.logger.Info("画面が変わっているので待ち続けます（turn の総実行時間では打ち切りません）",
				"identifier", snap.Identifier,
				"revision", agent.Revision,
				"agent_status", string(agent.AgentStatus))
			continue
		}
		if err != nil {
			o.logger.Warn("画面の版を読めませんでした（止まったものとして扱います）",
				"identifier", snap.Identifier, "error", err)
		}

		// 3. 版が止まったまま閾値を超えた。worker を止め、リトライを積む。
		// **同期で呼んではならない**（設計 3-8）。打ち切りになった場合は 3-25 の9段を
		// 通り、`agent.prompt` の待ち受けで既定1時間返らない。
		o.abandonRunAsync(ctx, rs, o.stalledScreenReason(snap, agent, now))
	}
}

// stalledScreenReason は「画面が止まったまま閾値を超えた」ときに人間へ見せる文面を作る
// （設計 3-34b の形。何が起きたか →【確かめ方】→【よくある原因】→【対処】）。
//
// **`herdr agent read` を案内してはならない**（設計 3-34b）。この文面を載せたコメントの
// 直後に `pane.close` を呼ぶので、人間が読むときには agent が消えている。
//
// snap: 対象の run の写し。
// agent: agent.get が返した情報（読めなかった場合はゼロ値に近い）。
// now: いまの時刻。
// 戻り値: issue のコメントとログに載せる理由の文字列。
func (o *Orchestrator) stalledScreenReason(snap runSnapshot, agent herdr.Agent, now time.Time) string {
	status := string(agent.AgentStatus)
	if status == "" {
		status = string(herdr.AgentStatusUnknown)
	}
	// **そのままコピーして叩けるコマンドにする。**worktree のパスを埋め込まないと、
	// 読んだ人はまず「どこで叩くのか」を探すところから始めることになる。
	// **持っていないものは案内しない。**着手の途中で落ちた run は worktree も
	// 会話の記録も持っておらず、その行は【調べるところ】にも出ない（3-34b）。
	var parts []string
	if snap.WorktreePath != "" {
		parts = append(parts, fmt.Sprintf(
			"次のコマンドで、作業がどこまで進んでいたかを見てください。\n"+
				"```sh\ngit -C %q status\ngit -C %q log --oneline -5\n```",
			snap.WorktreePath, snap.WorktreePath))
	}
	if snap.TranscriptPath != "" {
		parts = append(parts,
			"下記の「Claude Code の会話の記録」を開き、末尾で何をしていたかを見てください。")
	}
	check := strings.Join(parts, "\n")
	if check == "" {
		// **worktree も会話の記録もまだ無い**（着手の途中で画面が止まった）。
		// 見に行ける場所が1つも無いので、次の巡回で何が起きるかだけを伝える。
		check = "この run は worktree も会話の記録もまだ持っていません。" +
			"continuo は pane を閉じ、リトライの回数が残っていれば着手からやり直します。"
	}
	return fmt.Sprintf(
		"continuo は herdr へ `agent.get` を投げて Claude Code の画面の版（pane の revision）を"+
			"見比べています。その版が %s のあいだ、1回も増えませんでした"+
			"（最後に見た状態: %s、画面の版: %d）。**止まったものと判断して打ち切りました。**"+
			"\n【確かめ方】%s"+
			"\n【よくある原因】確認の画面が出て人間の入力を待っていた / "+
			"応答の来ない相手を待ち続けていた / 画面を書き換えないコマンドが終わらなかった。"+
			"\n【対処】原因を直してから Status を着手待ちへ戻してください。"+
			"画面が変わらないまま待つ時間は WORKFLOW.md の `claude.turn_timeout_ms` で変えられます"+
			"（いまは %d ミリ秒）。**この値は turn の総実行時間の上限ではありません。**"+
			"画面が変わり続けている限り、1つの指示に何時間かかっても打ち切りません。",
		formatDuration(now.Sub(snap.RevisionAt)), status, agent.Revision,
		check, o.cfg.Claude.TurnTimeoutMs)
}

// formatDuration は経過時間を人間が読める日本語にする（`1時間3分` の形）。
//
// **`time.Duration.String()` を人間に見せない。**`1h3m0.5s` は読み手に伝わらない。
//
// d: 表す長さ。負なら 0 として扱う。
// 戻り値: 日本語の長さ（1分未満は「1分未満」）。
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d / time.Minute)
	if total < 1 {
		return "1分未満"
	}
	if total < 60 {
		return fmt.Sprintf("%d分", total)
	}
	if total%60 == 0 {
		return fmt.Sprintf("%d時間", total/60)
	}
	return fmt.Sprintf("%d時間%d分", total/60, total%60)
}
