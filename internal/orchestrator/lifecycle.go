package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// handleTurnEnd は turn が終わったあとの処理をひとまとめに行う（設計 3-5 の図）。
//
//  1. transcript を読んで表明とトークンを取る（設計 3-15 / 3-25。**1回開いて両方を取る**）
//  2. 表明どおりに Status を動かす（グループの他の issue も動かす。設計 3-26）
//  3. Status を ID 指定で取り直し、その値で分岐する
//     terminal_states           … コメントを確かめてから worktree と branch を片付ける
//     active_states             … max_turns に未到達なら次の turn、到達なら failure_state
//     どちらでもない（引き渡し） … コメントを確かめてから worker を止める。**worktree は消さない**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値: この run が終わったら true（turn ループを止める）。
func (o *Orchestrator) handleTurnEnd(ctx context.Context, rs *runState) bool {
	signals := o.readSignals(ctx, rs)
	rs.setMissingSignal(len(signals) == 0)
	o.applySignals(ctx, rs, signals)

	current, ok := o.refreshIssue(ctx, rs)
	if !ok {
		// 見つからない。continuo は面倒を見ない（設計 3-10 の「いつ手放すか」）。
		o.abandonRun(ctx, rs, "ボードから見えなくなりました")
		return true
	}
	rs.setIssue(current)

	switch {
	case containsFold(o.cfg.Tracker.TerminalStates, current.State):
		o.finishRun(ctx, rs, "", fmt.Sprintf("Status が %s になりました", current.State))
		return true
	case containsFold(o.cfg.Tracker.ActiveStates, current.State):
		// 次の turn へ。打ち切りの判定は turnLoop の先頭で行う。
		return false
	default:
		// 引き渡し（In Review / Blocked）。**worker は止めるが worktree は残す**（設計 3-5）。
		o.finishRun(ctx, rs, "", fmt.Sprintf("Status が %s になりました（人間へ引き渡します）", current.State))
		return true
	}
}

// readSignals は turn が終わったあとに transcript を読んで表明を拾う（設計 3-25）。
//
// **turn が終わってから 0.5 秒待って読む。**`Stop` hook が走る時点では、その turn の
// 最後の text ブロックがまだ JSONL に書かれていない（13件すべてで未書き込み）。
// **見つからなければ 0.1 秒間隔で最大5回読み直す。5回で諦める**
// （待つ合計は 0.5 + 0.5 = 1.0 秒）。それでも無ければ「表明なし」として扱い、次の turn で促す。
//
// **トークンも同じ1回の読み取りで取る**（設計 3-15。2回開かない）。
// **結果は runState に持たず、その場でログに出す。**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値: 拾った表明（対象の識別子 → 値）。
func (o *Orchestrator) readSignals(ctx context.Context, rs *runState) map[string]string {
	snap := rs.snapshot()
	rs.mu.Lock()
	path := rs.TranscriptPath
	promptID := rs.PromptID
	rs.mu.Unlock()

	if path == "" {
		o.logger.Warn("transcript のパスが分からないので表明を読めません", "identifier", snap.Identifier)
		return nil
	}

	waits := make([]time.Duration, 0, transcriptRetryCount+1)
	waits = append(waits, transcriptFirstWait)
	for i := 0; i < transcriptRetryCount; i++ {
		waits = append(waits, transcriptRetryWait)
	}

	var last *TranscriptReadResult
	for _, d := range waits {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(d):
		}

		result, err := ReadTranscript(path, promptID, o.cfg.Tracker.StatusSignalPrefix, snap.Identifier)
		if err != nil {
			o.logger.Warn("transcript を読めません", "identifier", snap.Identifier, "path", path, "error", err)
			continue
		}
		last = result
		if len(result.Signals) > 0 {
			o.logTokens(snap.Identifier, result.Usage)
			return result.Signals
		}
	}

	if last != nil {
		o.logTokens(snap.Identifier, last.Usage)
	}
	o.logger.Info("この turn には表明がありませんでした（次の turn で促します）", "identifier", snap.Identifier)
	return nil
}

// logTokens は集計したトークンをログに出す（設計 3-15）。
//
// **`runState` には持たない。**ダッシュボード（第9段階）が使うなら、そのとき置き場所を決める。
//
// identifier: issue の識別子。
// usage: 集計したトークン。
func (o *Orchestrator) logTokens(identifier string, usage TokenUsage) {
	o.logger.Info("トークンを集計しました",
		"identifier", identifier,
		"api_calls", usage.APICalls,
		"input", usage.Input,
		"cache_creation", usage.CacheCreation,
		"cache_read", usage.CacheRead,
		"output", usage.Output,
	)
}

// applySignals は拾った表明どおりに Status を動かす（設計 3-25 / 3-26）。
//
// **対象を書かない行は、いま作業している issue を指す。**対象付きの行は
// `FetchIssueByIdentifier` で item を引く（その issue は `Ice Box` にあるので、巡回で
// 読んだ候補には入っていない）。
//
// **ボードに載っていなかったら、その行を捨て、issue のコメントに
// 「ボードに無いので動かせなかった」と書く**（人間が気づけるようにする）。
//
// **`terminal_states` の issue は動かさない**（UpdateStatus が書く前に取り直して弾く）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// signals: 拾った表明。
func (o *Orchestrator) applySignals(ctx context.Context, rs *runState, signals map[string]string) {
	for target, value := range signals {
		next, known := lookupSignalTarget(o.cfg.Tracker.StatusSignalMap, value)
		if !known {
			o.logger.Warn("表明の値が status_signal_map にありません（無視します）",
				"identifier", rs.issue().Identifier, "対象", target, "値", value)
			continue
		}
		if next == nil {
			// null は「その表明では Status を動かさない」である（既定では working）。
			o.logger.Info("表明を受けましたが Status は動かしません",
				"identifier", rs.issue().Identifier, "対象", target, "値", value)
			continue
		}

		itemID := ""
		if strings.EqualFold(target, rs.issue().Identifier) {
			itemID = rs.issue().ID
		} else {
			found, ok, err := o.tracker.FetchIssueByIdentifier(ctx, target)
			if err != nil {
				o.logger.Warn("表明が指す issue を引けません",
					"identifier", rs.issue().Identifier, "対象", target, "error", err)
				continue
			}
			if !ok {
				o.noteSignalTargetMissing(ctx, rs, target)
				continue
			}
			itemID = found.ID
		}

		written, err := o.tracker.UpdateStatus(ctx, itemID, *next, o.cfg.Tracker.TerminalStates)
		if err != nil {
			o.logger.Warn("表明どおりに Status を動かせません",
				"identifier", rs.issue().Identifier, "対象", target, "遷移先", *next, "error", err)
			continue
		}
		o.logger.Info("表明どおりに Status を動かしました",
			"identifier", rs.issue().Identifier, "対象", target, "遷移先", *next, "書き込んだか", written)
	}
}

// noteSignalTargetMissing は、表明が指す issue がボードに載っていなかったことを
// issue のコメントに残す（設計 3-25）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// target: 表明が指していた識別子。
func (o *Orchestrator) noteSignalTargetMissing(ctx context.Context, rs *runState, target string) {
	o.logger.Warn("表明が指す issue がボードに載っていません（この行を捨てます）",
		"identifier", rs.issue().Identifier, "対象", target)
	nodeID := issueNodeID(rs.issue())
	if nodeID == "" {
		return
	}
	body := fmt.Sprintf("表明に書かれた %s は、このボードに載っていないので Status を動かせませんでした。", target)
	if _, err := o.tracker.PostComment(ctx, nodeID, body, o.cfg.Tracker.Provider.Comments.SelfMarker); err != nil {
		o.logger.Warn("表明の取りこぼしを投稿できませんでした", "identifier", rs.issue().Identifier, "error", err)
	}
}

// lookupSignalTarget は表明の値から遷移先の Status を引く。
//
// m: `tracker.status_signal_map`。
// value: 表明の値（`review` / `blocked` / `working` など）。
// 戻り値の1つ目: 遷移先の Status。nil なら動かさない。
// 戻り値の2つ目: キーがあれば true。
func lookupSignalTarget(m map[string]*string, value string) (*string, bool) {
	for k, v := range m {
		if strings.EqualFold(strings.TrimSpace(k), strings.TrimSpace(value)) {
			return v, true
		}
	}
	return nil, false
}

// refreshIssue は issue を ID 指定で取り直す。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値の1つ目: 取り直した issue。
// 戻り値の2つ目: ボードから見えていれば true。
func (o *Orchestrator) refreshIssue(ctx context.Context, rs *runState) (tracker.Issue, bool) {
	issues, err := o.tracker.FetchIssuesByIDs(ctx, []string{rs.IssueID})
	if err != nil {
		o.logger.Warn("issue を取り直せません", "identifier", rs.issue().Identifier, "error", err)
		return rs.issue(), true
	}
	if len(issues) == 0 {
		return tracker.Issue{}, false
	}
	return issues[0], true
}

// finishRun は run を正常に終える。
//
//  1. コメントを確かめ、無ければ 3-25 の9段で書かせる（**毎 turn ではない。run が終わるときだけ**）
//  2. workspace_hooks.after_run を1回だけ実行する（設計 3-9 の段0）
//  3. worker を止める（pane.close。設計 3-5）
//  4. Status が cleanup.on_states に入っていれば worktree と branch を片付ける（設計 3-9）
//  5. 印から外す
//
// **`failureState` が空でなければ、先に Status をそこへ落とす**（打ち切り・失敗のとき）。
//
// **終わらせる処理は1本に絞る**（`beginTerminal`）。次の巡回が同じ run を
// もう一度終わらせにかかると、`failure_state` への書き込みと引き渡しコメントの投稿が
// 二重になる。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// failureState: 落とす先の Status。空なら落とさない。
// reason: 人間へ見せる理由。
func (o *Orchestrator) finishRun(ctx context.Context, rs *runState, failureState, reason string) {
	if !rs.beginTerminal() {
		return
	}
	o.finishRunClaimed(ctx, rs, failureState, reason)
}

// finishRunAsync は巡回のループから run を終わらせる（設計 3-8）。
//
// **印だけ同期で確保し、実際の処理は別の goroutine で回す。**`finishRun` は
// 3-25 の9段（`agent.prompt` を待ち受けつきで呼ぶ）を通ることがあり、
// **既定では最大1時間返らない。巡回のループの中で同期的に呼んではならない。**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// failureState: 落とす先の Status。空なら落とさない。
// reason: 人間へ見せる理由。
func (o *Orchestrator) finishRunAsync(ctx context.Context, rs *runState, failureState, reason string) {
	if !rs.beginTerminal() {
		return
	}
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		o.finishRunClaimed(ctx, rs, failureState, reason)
	}()
}

// finishRunClaimed は `beginTerminal` を通したあとの本体である。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// failureState: 落とす先の Status。空なら落とさない。
// reason: 人間へ見せる理由。
func (o *Orchestrator) finishRunClaimed(ctx context.Context, rs *runState, failureState, reason string) {
	o.logger.Info("run を終えます", "identifier", rs.issue().Identifier, "理由", reason)

	if failureState != "" {
		if _, err := o.tracker.UpdateStatus(ctx, rs.IssueID, failureState, o.cfg.Tracker.TerminalStates); err != nil {
			o.logger.Warn("Status を落とせません",
				"identifier", rs.issue().Identifier, "遷移先", failureState, "error", err)
		}
		o.postHandoffComment(ctx, rs, reason)
	}

	o.ensureAgentComment(ctx, rs)
	o.runAfterRun(ctx, rs)
	o.stopWorker(ctx, rs)

	current, ok := o.refreshIssue(ctx, rs)
	if ok && o.ws.ShouldCleanup(current.State) {
		o.cleanupWorktree(ctx, rs)
	}
	o.release(rs)
}

// failRun は着手や描画に失敗した run を失敗として扱う。
//
// **worktree は残す。**次の巡回に委ねられる場合があるためである。
//
// **worker を止める前にコメントを確かめる**（設計 3-25 の「いつ走らせるか」）。
// **まだ1回も turn を送っていない run では何も起きない**（`ensureAgentComment` が
// `StartedAt` のゼロ値で抜ける）。書かせる材料が無いためである。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// reason: 人間へ見せる理由。
func (o *Orchestrator) failRun(ctx context.Context, rs *runState, reason string) {
	if !rs.beginTerminal() {
		return
	}
	if _, err := o.tracker.UpdateStatus(ctx, rs.IssueID, o.cfg.Tracker.FailureState, o.cfg.Tracker.TerminalStates); err != nil {
		o.logger.Warn("Status を落とせません",
			"identifier", rs.issue().Identifier, "遷移先", o.cfg.Tracker.FailureState, "error", err)
	}
	o.postHandoffComment(ctx, rs, reason)
	o.ensureAgentComment(ctx, rs)
	o.runAfterRun(ctx, rs)
	o.stopWorker(ctx, rs)
	o.release(rs)
}

// abandonRun は stall や時間切れで run を諦め、リトライを積む（設計 3-21）。
//
// **worker を止め、`max_retry_backoff_ms` の指数バックオフで待ってから再 dispatch する。**
// **リトライの回数が尽きたら `failure_state` へ落として人間へ渡す。**
// **バックオフ中も印には残す**（外すと30秒後の巡回で即座に拾い直され、バックオフが効かない）。
//
// **リトライを使い切って人間へ渡すときは、worker を止める前にコメントを確かめる**
// （設計 3-25 の「いつ走らせるか」の表。`max_turns` に達した / stall で打ち切った、は
// 「走らせる」である）。**確かめないと、その run の成果が issue に何も残らない。**
// **リトライがまだ残っている場合は走らせない。**run はこのあと再 dispatch されて続くので、
// 打ち切りではないためである。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// reason: 人間へ見せる理由。
func (o *Orchestrator) abandonRun(ctx context.Context, rs *runState, reason string) {
	if !rs.beginTerminal() {
		return
	}
	o.abandonRunClaimed(ctx, rs, reason)
}

// abandonRunAsync は巡回のループから run を諦める（設計 3-8）。
//
// **印だけ同期で確保し、実際の処理は別の goroutine で回す。**打ち切りのときは
// 3-25 の9段（`agent.prompt` を待ち受けつきで呼ぶ）を通るので、既定では最大1時間返らない。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// reason: 人間へ見せる理由。
func (o *Orchestrator) abandonRunAsync(ctx context.Context, rs *runState, reason string) {
	if !rs.beginTerminal() {
		return
	}
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		o.abandonRunClaimed(ctx, rs, reason)
	}()
}

// abandonRunClaimed は `beginTerminal` を通したあとの本体である。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// reason: 人間へ見せる理由。
func (o *Orchestrator) abandonRunClaimed(ctx context.Context, rs *runState, reason string) {
	snap := rs.snapshot()
	if snap.RetryCount >= o.cfg.Agent.MaxRetries {
		o.logger.Warn("リトライの回数を使い切りました（人間へ渡します）",
			"identifier", snap.Identifier, "retry_count", snap.RetryCount, "理由", reason)
		// **打ち切りである。worker を止める前にコメントを確かめる**（設計 3-25）。
		o.ensureAgentComment(ctx, rs)
		o.runAfterRun(ctx, rs)
		o.stopWorker(ctx, rs)
		if _, err := o.tracker.UpdateStatus(ctx, rs.IssueID, o.cfg.Tracker.FailureState, o.cfg.Tracker.TerminalStates); err != nil {
			o.logger.Warn("Status を落とせません", "identifier", snap.Identifier, "error", err)
		}
		o.postHandoffComment(ctx, rs, reason)
		o.release(rs)
		return
	}

	o.runAfterRun(ctx, rs)
	o.stopWorker(ctx, rs)

	backoff := retryBackoff(snap.RetryCount, time.Duration(o.cfg.Agent.MaxRetryBackoffMs)*time.Millisecond)
	count := rs.addRetry(o.now(), backoff)
	o.logger.Warn("run を諦めてリトライを積みました（バックオフの間も印には残します）",
		"identifier", snap.Identifier, "retry_count", count, "backoff", backoff, "理由", reason)
	// **run はまだ続く。**印を外しておかないと、次の stall の閾値でリトライが1つも
	// 積まれず、人間へ渡されないまま止まる。
	rs.endTerminal()
}

// stopAndReleaseAsync は worktree を残したまま worker を止め、印から外す
// （`SPEC.md` 8.5 Part B の「workspace を掃除せずに worker を止める」）。
//
// **巡回のループから呼ぶので、印だけ同期で確保して別の goroutine で回す**（設計 3-8）。
// `workspace_hooks.after_run` は利用者が書いた外部コマンドであり、どれだけ時間がかかるか
// continuo には分からない。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
func (o *Orchestrator) stopAndReleaseAsync(ctx context.Context, rs *runState) {
	if !rs.beginTerminal() {
		return
	}
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		o.runAfterRun(ctx, rs)
		o.stopWorker(ctx, rs)
		o.release(rs)
	}()
}

// retryBackoff は指数バックオフの待ち時間を求める（設計 3-21）。
//
// retryCount: これまでのリトライ回数。
// max: `agent.max_retry_backoff_ms` の上限。
// 戻り値: 待つ長さ。
func retryBackoff(retryCount int, max time.Duration) time.Duration {
	d := baseRetryBackoff
	for i := 0; i < retryCount; i++ {
		d *= 2
		if max > 0 && d >= max {
			return max
		}
	}
	if max > 0 && d > max {
		return max
	}
	return d
}

// stopWorker は worker を止める（設計 3-5 の「worker を止める」の1段目）。
//
// **`pane.close` が唯一の手段である。**agent だけを止めるメソッドは herdr に無い。
//
// **止めたことを記録する。**turn ループはこれを見て、既に諦められた run を
// もう一度諦めないようにする（設計 3-21。`runState.currentWorker`）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
func (o *Orchestrator) stopWorker(ctx context.Context, rs *runState) {
	rs.mu.Lock()
	paneID := rs.PaneID
	rs.PaneID = ""
	rs.workerStopped = true
	rs.mu.Unlock()
	if paneID == "" {
		return
	}
	if _, err := o.herdr.PaneClose(ctx, herdr.PaneCloseParams{PaneID: paneID}); err != nil {
		o.logger.Warn("pane を閉じられませんでした", "identifier", rs.issue().Identifier, "pane_id", paneID, "error", err)
		return
	}
	o.logger.Info("pane を閉じました", "identifier", rs.issue().Identifier, "pane_id", paneID)
}

// runAfterRun は workspace_hooks.after_run を実行する（設計 3-9 の段0）。
//
// **run が終わったとき（worker を止める直前）に1回だけ。**turn ごとではない。
// **失敗しても記録して続ける。**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
func (o *Orchestrator) runAfterRun(ctx context.Context, rs *runState) {
	snap := rs.snapshot()
	if snap.WorktreePath == "" {
		return
	}
	if _, err := o.ws.RunAfterRunOnce(ctx, snap.WorktreePath); err != nil {
		o.logger.Warn("workspace_hooks.after_run に失敗しました（記録して続けます）",
			"identifier", snap.Identifier, "error", err)
	}
}

// cleanupWorktree は worktree と branch と設定ファイルを片付ける（設計 3-9）。
//
// **見送った理由は、身元ファイルの cleanup_deferred_at がゼロ値のときだけ issue へ書く**
// （毎巡回で警告を積まない。設計 3-9 の手順2c）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
func (o *Orchestrator) cleanupWorktree(ctx context.Context, rs *runState) {
	snap := rs.snapshot()
	if snap.WorktreePath == "" {
		return
	}
	result, err := o.ws.Cleanup(ctx, workspace.CleanupRequest{
		WorktreePath: snap.WorktreePath,
		Base:         snap.Base,
	})
	if err != nil {
		o.logger.Warn("worktree を片付けられません", "identifier", snap.Identifier, "error", err)
		return
	}
	if result.Removed {
		o.logger.Info("worktree と branch を片付けました", "identifier", snap.Identifier, "path", snap.WorktreePath)
		return
	}
	o.logger.Warn("worktree を片付けずに残しました",
		"identifier", snap.Identifier, "path", snap.WorktreePath, "理由", strings.Join(result.Reasons, " / "))

	if !result.ShouldComment {
		return
	}
	nodeID := issueNodeID(rs.issue())
	if nodeID == "" {
		return
	}
	body := fmt.Sprintf("worktree を片付けずに残しました（%s）。\n\n理由:\n- %s",
		snap.WorktreePath, strings.Join(result.Reasons, "\n- "))
	if _, err := o.tracker.PostComment(ctx, nodeID, body, o.cfg.Tracker.Provider.Comments.SelfMarker); err != nil {
		o.logger.Warn("片付けを見送った通知を投稿できませんでした", "identifier", snap.Identifier, "error", err)
		return
	}
	// **投稿に成功したあとで印を書く。**投稿の前に書くと、投稿が失敗したときに
	// コメントが永久に出なくなる（設計 3-9 の手順2c）。
	if err := o.ws.MarkCleanupDeferred(snap.WorktreePath, o.now()); err != nil {
		o.logger.Warn("片付けを見送った印を書けませんでした", "identifier", snap.Identifier, "error", err)
	}
}

// postHandoffComment は人間へ引き渡すときの通知を issue へ書く。
//
// **成果の要約は書かない**（設計 3-29）。continuo が書くのは引き渡しの通知だけである。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// reason: 引き渡す理由。
func (o *Orchestrator) postHandoffComment(ctx context.Context, rs *runState, reason string) {
	nodeID := issueNodeID(rs.issue())
	if nodeID == "" {
		return
	}
	if _, err := o.tracker.PostComment(ctx, nodeID,
		buildHandoffComment(rs.issue().Identifier, reason),
		o.cfg.Tracker.Provider.Comments.SelfMarker); err != nil {
		o.logger.Warn("引き渡しの通知を投稿できませんでした", "identifier", rs.issue().Identifier, "error", err)
	}
}

// containsFold は states に target が（大文字小文字を無視して）含まれるかを返す。
//
// **比較は大文字小文字を無視する**（表示はボードの綴りをそのまま保つ。設計 3-13）。
//
// states: 照合する Status 名の一覧。
// target: 探す Status 名。
// 戻り値: 含まれていれば true。
func containsFold(states []string, target string) bool {
	t := strings.TrimSpace(target)
	for _, s := range states {
		if strings.EqualFold(strings.TrimSpace(s), t) {
			return true
		}
	}
	return false
}
