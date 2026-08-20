package orchestrator

import (
	"context"
	"fmt"
	"time"

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
//	それ以外（引き渡し・見えない） … **workspace を掃除せずに** worker を止める
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
			// **同期で呼んではならない**（設計 3-8）。片付けの前にコメントを確かめる
			// 経路（3-25 の9段）は `agent.prompt` を待ち受けつきで呼び、既定では最大
			// 1時間返らない。巡回のループがそこで止まると、dispatch も stall 検知も
			// 全部止まる。
			o.finishRunAsync(ctx, rs, "", fmt.Sprintf("Status が %s になっていました", issue.State))
		case containsFold(o.cfg.Tracker.ActiveStates, issue.State) && issue.Dispatchable:
			// まだ作業中で routable である。スナップショットの更新だけ。
		default:
			o.logger.Info("作業中でも完了でもない状態になったので worker を止めます（worktree は残します）",
				"identifier", issue.Identifier, "状態", issue.State)
			o.stopAndReleaseAsync(ctx, rs)
		}
	}

	for id, rs := range byID {
		if seen[id] {
			continue
		}
		o.logger.Warn("issue がボードから見えなくなったので印から外します（continuo は面倒を見ません）",
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

	issues, err := o.tracker.FetchIssuesByIDs(ctx, ids)
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
			o.closeOrphanPane(ctx, orph.identity)
		}
	}
}

// closeOrphanPane は印に入っていない worktree に付いている pane を閉じる
// （設計 3-9 の手順7b）。
//
// **閉じないと、次の巡回で同じ worktree に2つ目の Claude Code が立つ。**
//
// ctx: 呼び出しに適用するコンテキスト。
// identity: worktree の身元ファイル。
func (o *Orchestrator) closeOrphanPane(ctx context.Context, identity *workspace.Identity) {
	if identity.HerdrWorkspaceID == "" {
		return
	}
	list, err := o.herdr.PaneList(ctx, herdr.PaneListParams{WorkspaceID: identity.HerdrWorkspaceID})
	if err != nil {
		return
	}
	for _, p := range list.Panes {
		if p.Agent == "" {
			continue
		}
		o.logger.Warn("印に入っていない worktree に生きた pane があったので閉じます",
			"identifier", identity.IssueIdentifier, "pane_id", p.PaneID)
		if _, err := o.herdr.PaneClose(ctx, herdr.PaneCloseParams{PaneID: p.PaneID}); err != nil {
			o.logger.Warn("pane を閉じられませんでした", "pane_id", p.PaneID, "error", err)
		}
	}
}

// checkStalls は stall を判定する（設計 3-21 / 3-27 の評価順）。
//
// **stall の閾値に達した run について、上から順に見る。**
//
//  1. 枠待ちか（percent が 100 かつ この run から hook が来ていない）
//     → 枠待ちなら「時計を止めている」印を付けて終わり。**殺さない**
//  2. herdr の agent_status が working か
//     → working なら猶予を1回だけ与える（LastSeenAt を現在時刻にして、もう一度待つ）。**2回目は殺す**
//  3. どちらでもない
//     → worker を止め、リトライを積む
//
// **枠待ちの run は判定そのものを飛ばす**（`WaitingQuota` が立っている間は時計が止まっている）。
// **`LastSeenAt` は進めない**（進めると、枠が明けたあとに「最後に動いていた時刻」が分からなくなる）。
//
// ctx: 呼び出しに適用するコンテキスト。
func (o *Orchestrator) checkStalls(ctx context.Context) {
	stall := time.Duration(o.cfg.Claude.StallTimeoutMs) * time.Millisecond
	if stall <= 0 {
		return
	}
	now := o.now()

	for _, rs := range o.snapshotRuns() {
		snap := rs.snapshot()
		if snap.WaitingQuota {
			// 枠が明けたら印を外す。**外す契機は「resets_at を過ぎたこと」だけである。**
			if !snap.QuotaResetAt.IsZero() && !now.Before(snap.QuotaResetAt) {
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
		if now.Sub(snap.LastSeenAt) < stall {
			continue
		}

		// 1. 枠待ちを先に見る。
		if o.isQuotaWaiting(rs) {
			resetAt, _ := o.quotaResetAt()
			rs.setWaitingQuota(resetAt)
			o.logger.Info("枠待ちと判定したので stall の時計を止めます",
				"identifier", snap.Identifier, "resets_at", resetAt)
			continue
		}

		// 2. agent_status を1回だけ見る。working なら猶予を1回。
		status, err := o.agentStatus(ctx, rs)
		if err == nil && status == herdr.AgentStatusWorking {
			if rs.grantGrace(now) {
				o.logger.Info("agent_status が working なので猶予を1回だけ与えます",
					"identifier", snap.Identifier, "stall_timeout_ms", o.cfg.Claude.StallTimeoutMs)
				continue
			}
			o.logger.Warn("working のまま2度目の閾値に達したので止めます", "identifier", snap.Identifier)
		}

		// 3. worker を止め、リトライを積む。
		// **同期で呼んではならない**（設計 3-8）。打ち切りになった場合は 3-25 の9段を
		// 通り、`agent.prompt` の待ち受けで既定1時間返らない。
		o.abandonRunAsync(ctx, rs, fmt.Sprintf(
			"Claude Code が %d ミリ秒のあいだ何も進めませんでした"+
				"（continuo が最後に見た状態: %s）。**continuo は止まったものと判断して打ち切りました。**"+
				"\n【確かめ方】下記の「Claude Code の会話の記録」を開き、末尾で何をしていたかを見てください。"+
				"\n【よくある原因】確認の画面が出て人間の入力を待っていた / 応答を待ち続けていた。"+
				"\n【対処】原因を直してから Status を着手待ちへ戻してください。"+
				"止まったとみなすまでの時間は WORKFLOW.md の `claude.stall_timeout_ms` で変えられます（いまは %d）。",
			o.cfg.Claude.StallTimeoutMs, status, o.cfg.Claude.StallTimeoutMs))
	}
}
