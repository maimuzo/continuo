package orchestrator

import (
	"context"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// directChatSetupFailure は、direct chat の用意（設計 3-82d の用意の段2）が落ちた記録である。
//
// **通常の着手の失敗の記録（`failureNote`）とは別の器である**（設計 3-82d）。
type directChatSetupFailure struct {
	// Count は続けて落ちた回数である。
	Count int
	// LastAt は最後に落ちた時刻である（次に試すまでの間隔の起点。設計 3-82c の門7）。
	LastAt time.Time
	// Reason は最後に落ちた理由の要約である（上限に達したときのコメントに載せる）。
	Reason string
}

// assigneeVerdict は、direct chat のカードの担当者を 3-82h の判定の表に当てた答えである。
type assigneeVerdict int

const (
	// assigneeInvalidCount は表の順1（担当者が0人か2人以上）である。**`failure_state` を書く。**
	assigneeInvalidCount assigneeVerdict = iota
	// assigneeLoginUnknown は表の順2（自分のログイン名が取れない）である。
	assigneeLoginUnknown
	// assigneeOther は表の順3（1人で、自分ではない）である。
	assigneeOther
	// assigneeSelf は表の順4（1人で、自分）である。
	assigneeSelf
)

// judgeDirectChatAssignees は、direct chat のカードの担当者を 3-82h の判定の表に当てる（設計 3-82h）。
//
// **direct chat に入れるかを決めるのはここだけである。**
// **上から順に当てる。**担当者の人数の判定はログイン名を要らないので、先に行う。
//
// **issue のコメントは1件も読まない。**材料はカンバンから読んだ担当者と、自分のログイン名の2つだけである。
// **入札もしない。**
//
// ctx: 自分のログイン名を取りに行くときに使う（一度取れたら覚えているので、ふつうは通信しない）。
// issue: 判定する issue（候補の取得か、実行中の issue の取り直しの値）。
// 戻り値: 表のどの行に当たったか。
func (o *Orchestrator) judgeDirectChatAssignees(ctx context.Context, issue tracker.Issue) assigneeVerdict {
	logins := assigneeLogins(issue)
	if len(logins) != 1 {
		return assigneeInvalidCount
	}
	viewer, ok := o.viewerIdentity(ctx)
	if !ok {
		return assigneeLoginUnknown
	}
	if strings.EqualFold(strings.TrimSpace(logins[0]), strings.TrimSpace(viewer.Login)) {
		return assigneeSelf
	}
	return assigneeOther
}

// splitDirectChatCandidates は、候補を direct chat のものとそれ以外に分ける（設計 3-82b）。
//
// **並び順は保つ。**どちらの側も、カンバンの並び順のまま処理する。
//
// candidates: 候補の取得の結果。
// 戻り値の1つ目: Status が `direct_chat_state` の候補。
// 戻り値の2つ目: それ以外の候補。
func (o *Orchestrator) splitDirectChatCandidates(candidates []tracker.Issue) ([]tracker.Issue, []tracker.Issue) {
	var directChat, others []tracker.Issue
	for _, issue := range candidates {
		if config.IsDirectChatState(o.cfg.Tracker, issue.State) {
			directChat = append(directChat, issue)
			continue
		}
		others = append(others, issue)
	}
	return directChat, others
}

// prepareDirectChatPanes は direct chat の候補について pane を用意するかを決める（設計 3-82b / 3-82c）。
//
// **「いつ pane を用意するか」を決めるのは、この関数の門だけである**（設計 3-82c）。
// **上から順に見て、1つでも当たったら次の候補へ移る。**
//
//	門1 既に印を持っている                 … 何も出さない（巡回の側が direct chat へ入れる）
//	門2 draft issue である                  … Debug
//	門3 担当者が自分1人ではない             … 0人か2人以上なら `failure_state` を書く。1人で他人なら Debug
//	門4 この worktree に pane が1枚でもある  … Debug（「Claude Code が居るか」は判定しない）
//	門5 空きスロットが無い                  … Debug（人間の決定で、専用の知らせは作らない）
//	門6 着手の直前の検査（`preflight`）      … `preflight` が自分で出す
//	門7 用意が直前に落ちてから間隔が空いていない … Debug
//
// **この一覧に無い門は1つも通らない**（設計 3-82d の「この一覧に無い門」）。
// `rate_limit.pause_above_percent`・担当の持ち回り（`handoffGate`）・`skipByFailure`・
// `tracker.required_labels`・`Dispatchable` である。
//
// **issue のコメントを1本も読まない。**コメントを読む枠には触らない（設計 3-82b）。
//
// **飛ばすたびに関門の記録を消す**（`clearGate`。設計 6-1）。
//
// ctx: 呼び出しに適用するコンテキスト。
// candidates: Status が `direct_chat_state` の候補（カンバンの並び順）。
func (o *Orchestrator) prepareDirectChatPanes(ctx context.Context, candidates []tracker.Issue) {
	// **専用の記録は、この巡回の候補に無い issue の分を消す**（設計 3-82d の用意の段2）。
	// **このパスが走った巡回でだけ消す。**候補の取得に失敗した巡回では、ここへ来ない。
	o.forgetDirectChatSetupFailuresNotIn(candidates)
	if len(candidates) == 0 {
		return
	}

	// **pane の写像は、この1パスで1回だけ作る**（設計 3-82b）。`pane.list` は機械中の pane を
	// 全部返すので、候補ごとに引き直すと巡回1回で候補の数だけ飛ぶ。**`agent.list` は投げない。**
	var panes map[string]herdr.Pane
	var panesErr error
	panesDone := false

	var claimed []claimedRun
	for _, issue := range candidates {
		if ctx.Err() != nil {
			break
		}
		// 門1: 既に印を持っている。**pane を持っているかは見ない**（設計 3-82c）。
		// 用意し直すには、印を持つ run に着手の段1 をもう一度踏ませることになる。
		if _, taken := o.lookupRunByID(issue.ID); taken {
			o.clearGate(issue.ID)
			continue
		}
		// 門2: draft issue は worktree を作れないので、用意は必ず用意の段2 で落ちる。
		// **`preflight` は落とさない**（信頼登録を要求しない設定では素通りする）ので、
		// ここで落とさないと30秒ごとに枠を取っては落ちるのを永久に繰り返す。
		if issue.Owner == "" || issue.Repo == "" {
			o.logger.Debug("draft issue なので、direct chat の pane は用意できません",
				"identifier", issue.Identifier)
			o.clearGate(issue.ID)
			continue
		}
		// 門3: 担当者が自分1人ではない（設計 3-82h）。
		switch o.judgeDirectChatAssignees(ctx, issue) {
		case assigneeInvalidCount:
			o.writeDirectChatAssigneeFailureAsync(ctx, issue)
			o.clearGate(issue.ID)
			continue
		case assigneeLoginUnknown:
			// **この巡回では何もしない**（設計 3-82h の順2 の「印を持っていない機械」）。
			o.logger.Debug("gh の持ち主が分からないので、この巡回では direct chat の pane を用意しません",
				"identifier", issue.Identifier)
			o.clearGate(issue.ID)
			continue
		case assigneeOther:
			o.logger.Debug("担当者がこの PC の continuo のアカウントではないので、direct chat の pane を用意しません",
				"identifier", issue.Identifier, "担当者", strings.Join(assigneeLogins(issue), ", "))
			o.clearGate(issue.ID)
			continue
		}
		// 門4: この worktree に pane が1枚でもある。**「Claude Code が居るか」は判定しない**（設計 3-82c）。
		if !panesDone {
			panes, panesErr = o.directChatPanes(ctx)
			panesDone = true
		}
		if panesErr != nil {
			// **「引けなかった」と「pane が無い」を混ぜない**（設計 3-82b）。混ぜると、herdr の socket が
			// 一瞬落ちただけで、人間が話している pane の隣に2枚目を開くことになる。
			o.logger.Warn("pane の一覧を取れないので、direct chat の pane は用意しません（次の巡回で見ます）",
				"identifier", issue.Identifier, "error", panesErr)
			o.clearGate(issue.ID)
			continue
		}
		exists, err := o.directChatPaneExists(panes, issue)
		if err != nil {
			o.logger.Warn("direct chat の worktree の置き場所を決められないので、この巡回では用意しません",
				"identifier", issue.Identifier, "error", err)
			o.clearGate(issue.ID)
			continue
		}
		if exists {
			o.logger.Debug("direct chat の worktree に pane が既にあるので、用意しません",
				"identifier", issue.Identifier)
			o.clearGate(issue.ID)
			continue
		}
		// 門5: 空きスロット。**人間の決定で、枠が尽きたことを知らせる仕組みは作らない**（設計 3-82c）。
		// **出すのは Debug 1行だけである。**`clearGate` はこの分岐の外（下の共通の後始末）でも呼ぶ。
		if free, blocker, limit := o.freeSlotBlocker(); !free {
			o.logger.Debug("空きスロットが無いので、direct chat の pane を用意しません",
				"identifier", issue.Identifier, "上限に達した設定", blocker, "その上限", limit)
			o.clearGate(issue.ID)
			continue
		}
		// 門6: 着手の直前の検査。**信頼登録の判定をこの門より前に置いてはならない**（設計 3-82c）。
		// 先に落とすと、未信頼のリポジトリで direct chat を頼んだ人へ、直し方のコメントが1件も出ない。
		if !o.preflight(ctx, issue) {
			o.clearGate(issue.ID)
			continue
		}
		// 門7: 用意が直前に落ちてから、間隔が空いていない。
		if wait, ok := o.directChatSetupBackoff(issue.ID); ok {
			o.logger.Debug("direct chat の用意が直前に落ちたので、間隔を空けます",
				"identifier", issue.Identifier, "あと", wait)
			o.clearGate(issue.ID)
			continue
		}
		o.clearGate(issue.ID)

		// 用意の段1: 印を付け、「用意中」の記録を立てる（着手の段1）。
		// **`claimForDispatch` ではなく `o.claim` を直に呼ぶ**（設計 3-82d）。あれは印を付けたあとに
		// 写しの Status を `running_state` へ書き換える。**写しの Status は書き換えない。**
		// 書き換えると状態ごとの上限の勘定に入り、direct chat のカードを1枚置いただけで
		// 通常の着手を1件ぶん失う。
		rs, ok := o.claim(issue.ID, issue)
		if !ok {
			continue
		}
		rs.beginPreparing()
		// **閉じる集合にその worktree があれば外す**（設計 3-82f）。印を持った worktree には、
		// 閉じる規則を当てない。
		o.removeFromCloseSet(issue.ID)
		claimed = append(claimed, claimedRun{rs: rs, issue: issue})
	}
	if len(claimed) == 0 {
		return
	}

	// **用意の段2 と段3 は別の goroutine で回す**（設計 3-82d / 3-8）。用意の段2 は git の worktree 作成・
	// 利用者が書いた `workspace_hooks`・起動の待ちを順に通るので、同期に踏むと巡回が最大2分返らない。
	// **goroutine は1本だけ立てる。**印を付けた順に1本で処理する（`dispatchCandidates` と同じ扱い）。
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		for _, c := range claimed {
			if ctx.Err() != nil {
				return
			}
			o.setUpDirectChat(ctx, c.rs, c.issue)
		}
	}()
}

// setUpDirectChat は用意の段2 と段3 を踏む（設計 3-82d）。
//
// **巡回のループから同期で呼んではならない**（設計 3-8）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 用意の段1 で印を付けた run。
// issue: 対象の issue。
func (o *Orchestrator) setUpDirectChat(ctx context.Context, rs *runState, issue tracker.Issue) {
	// 用意の段2: 着手の段3〜段10 を踏む（**着手の段2 と段11 は踏まない**）。
	//
	// **`startRun` から入ってはならない**（設計 3-82d）。あれは着手の段2 を踏むので、
	// 用意の段2 が `ErrStatusNotWritten` で落ち、pane が1枚もできない。
	if err := o.startRunFromWorktree(ctx, rs, issue, false, true); err != nil {
		o.failDirectChatSetup(ctx, rs, issue, err)
		return
	}
	// **用意が成功したら、専用の記録を消す**（設計 3-82d の用意の段2）。
	o.forgetDirectChatSetupFailure(issue.ID)
	o.finishDirectChatSetup(ctx, rs, issue)
}

// directChatSetupOutcome は用意の段3 の外れ方である（設計 3-82d の外れ方の表）。
type directChatSetupOutcome int

const (
	// setupEnter は direct chat へ入れたことを表す（issue へ1件書く）。
	setupEnter directChatSetupOutcome = iota
	// setupReturned は Status が作業中の Status になっていたことを表す（人間が戻した）。
	setupReturned
	// setupLost は印がもうこの run のものではないことを表す（自分で開いた pane を閉じる）。
	setupLost
	// setupAbandon は、担当者が自分1人ではなくなった・作業中でも direct chat でもない Status になった・
	// カードを取り直せなかった、のどれかである（自分で開いた pane を閉じ、印を外す）。
	setupAbandon
)

// finishDirectChatSetup は用意の段3 を踏む（設計 3-82d）。
//
// **カードを取り直し、`o.mu` を取ってから「用意中」を下ろす。**同じロックの中で、自分の取り直しと、
// 巡回が用意中に書いた記録（設計 3-82b の段2）のうち、**見た時刻が新しいほうの Status** で判定する。
//
// **ロックの中で行うのは、判定と印の出し入れだけである。**issue への書き込み・`running_state`・
// hold・`pane.close`・印を外す（`release`）はロックを放してから行う（判断票6周目。
// `o.release` は自分で `o.mu` を取るので、持ったまま呼ぶと固まる）。
//
// **hold のコメントはここでは書かない**（設計 3-82h）。書くのは作業中の Status へ戻ったときだけである。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 用意の段2 を通った run。
// issue: 対象の issue。
func (o *Orchestrator) finishDirectChatSetup(ctx context.Context, rs *runState, issue tracker.Issue) {
	// **「誰が Status を書いたか」は取らない**（設計 3-61）。見るのは Status と担当者だけである。
	fetched, err := o.tracker.FetchIssuesByIDsWithoutTimeline(ctx, []string{issue.ID})
	ownAt := o.now()
	var current tracker.Issue
	haveCurrent := err == nil && len(fetched) > 0
	if haveCurrent {
		current = fetched[0]
	} else if err != nil {
		o.logger.Warn("direct chat の用意を終える前にカードを取り直せません（pane を閉じ、次の巡回でやり直します）",
			"identifier", issue.Identifier, "error", err)
	}

	// **自分のログイン名はロックの外で取る**（取りに行くと GraphQL を1本投げうる）。
	verdict := assigneeInvalidCount
	if haveCurrent {
		verdict = o.judgeDirectChatAssignees(ctx, current)
	}

	outcome := setupAbandon
	decided := ""
	o.mu.Lock()
	seenState, seenAt := rs.finishPreparing()
	if cur, ok := o.runs[issue.ID]; !ok || cur != rs || rs.isFinished() {
		// **印がもうこの run のものではない**（用意の最中に巡回が手を離した・`failure_state` へ落とした）。
		outcome = setupLost
	} else if haveCurrent {
		decided = current.State
		// **見た時刻が新しいほうの Status で判定する**（設計 3-82d）。素早く往復すると
		// 1回余計にやり直すだけで、害は無い。
		if !seenAt.IsZero() && seenAt.After(ownAt) {
			decided = seenState
		}
		switch {
		case config.IsDirectChatState(o.cfg.Tracker, decided) &&
			(verdict == assigneeSelf || verdict == assigneeLoginUnknown):
			// **自分のログイン名が取れないときは、3-82h の順2 の「印を持っている機械」と同じく入れる。**
			outcome = setupEnter
			rs.clearSendFirstPrompt()
			rs.enterDirectChatMode()
		case containsFold(o.cfg.Tracker.ActiveStates, decided):
			outcome = setupReturned
			// **direct chat へ入っていたら下ろす。**巡回の段2 が既に下ろしているはずだが、
			// 印が立ったままだと `wakeRuns` が1回目の本文を送らない。
			rs.leaveDirectChatMode()
		default:
			outcome = setupAbandon
		}
	}
	o.mu.Unlock()

	switch outcome {
	case setupEnter:
		o.logger.Info("direct chat の pane を用意しました（ここから先は人間が話しかけます。continuo は指示を送りません）",
			"identifier", issue.Identifier, "状態", decided)
		o.postDirectChatReady(ctx, issue)
	case setupReturned:
		// **用意の最中に人間が作業中の Status へ戻した**（設計 3-82d の外れ方の表の1行目）。
		// **印は残し、`SendFirstPrompt` を立てたまま、送る印を立てる。**1回目の本文（5-3）が送られる。
		// **書き込みが終わってから送る印を立てる**（判断票6周目）。
		o.logger.Info("direct chat の pane を用意している最中に作業中の Status へ戻されたので、1回目の指示を送ります",
			"identifier", issue.Identifier, "状態", decided)
		returned := current
		returned.State = decided
		o.writeRunningStateOnReturn(ctx, rs, returned)
		o.postDirectChatHold(ctx, returned)
		rs.setNeedsPrompt()
	case setupLost:
		// **印はもう無いので、閉じないと誰も管理しない Claude Code が残る。**
		o.logger.Info("direct chat の用意を終える前に印が外れていたので、自分で開いた pane を閉じます",
			"identifier", issue.Identifier)
		o.closeDirectChatSetupPane(ctx, rs)
	default:
		// **用意の段2 が落ちたときと同じ後始末である。失敗としては数えない**（設計 3-82d）。
		// `ensureAgentComment` も `after_run` も通らない。
		o.logger.Info("direct chat の用意を終える前にカードが外れたので、自分で開いた pane を閉じて印を外します",
			"identifier", issue.Identifier, "状態", decided)
		o.abandonDirectChatSetup(ctx, rs)
	}
}

// failDirectChatSetup は、direct chat の用意（用意の段2）が落ちたときの後始末である（設計 3-82d）。
//
// **カンバンへは1バイトも書かない。**通常の着手の失敗は `failure_state` を書くか、バックオフして
// 再 dispatch する。**どちらも Status を動かすので、人間が置いたカードが `direct_chat_state` から外れる。**
//
// **自分が開いた pane は pane ID で閉じ、印を外す。**ここへ来るのは「用意を始める前に pane が
// 1枚も無かった」場合だけなので（門4）、**閉じる相手は continuo がたったいま開いたものである。**
//
// **落ちたことを専用の記録へ数え、次に試すまで間隔を空ける**（門7）。
// **通常の着手と同じ回数の上限（`agent.max_retries`）に達したら、書く経路で `failure_state` を書き、
// 落ちた理由をコメントする**（人間が了承した形）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 用意に失敗した run。
// issue: 対象の issue。
// err: 失敗の内容。
func (o *Orchestrator) failDirectChatSetup(ctx context.Context, rs *runState, issue tracker.Issue, err error) {
	count := o.noteDirectChatSetupFailure(issue.ID, err.Error())
	o.logger.Warn("direct chat の pane を用意できませんでした（カンバンは触りません。間隔を空けてやり直します）",
		"identifier", issue.Identifier, "続けて落ちた回数", count, "上限", o.cfg.Agent.MaxRetries, "error", err)
	o.mu.Lock()
	rs.finishPreparing()
	o.mu.Unlock()
	o.abandonDirectChatSetup(ctx, rs)
	if o.cfg.Agent.MaxRetries > 0 && count >= o.cfg.Agent.MaxRetries {
		body := i18n.T(i18n.KeyOrchestratorDirectChatSetupLimit,
			count, summaryLine(err.Error()), o.cfg.Tracker.DirectChatState, o.cfg.Tracker.FailureState)
		o.writeDirectChatFailure(ctx, issue, body)
	}
}

// abandonDirectChatSetup は、用意した run の自分で開いた pane を閉じ、印を外す（設計 3-82d）。
//
// **`stopWorker` を通さない。**あれは direct chat の門で必ず止まるので、自分で開いた pane を
// 1枚も閉じられない。**pane の ID を直接閉じる。**
//
// **`claimTerminal` を通す**（設計 3-56）。書き戻しが飛んでいたら終わるまで待つ。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
func (o *Orchestrator) abandonDirectChatSetup(ctx context.Context, rs *runState) {
	if rs.claimTerminal(ctx) {
		o.closeDirectChatSetupPane(ctx, rs)
		o.release(rs)
	}
}

// closeDirectChatSetupPane は、direct chat の用意か打ち切りで、自分が開いた pane を閉じる（設計 3-82d / 3-82f）。
//
// **`stopWorker` を通らない。**あれは direct chat の run では必ず門で止まる。
//
// **`markWorkerStopped` は呼ぶ。**この run を待っている turn ループがあった場合に返らなくなるのを防ぐ。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
func (o *Orchestrator) closeDirectChatSetupPane(ctx context.Context, rs *runState) {
	rs.mu.Lock()
	paneID := rs.PaneID
	rs.PaneID = ""
	rs.mu.Unlock()
	rs.markWorkerStopped()
	if paneID == "" {
		return
	}
	// **期限は付ける。**herdr が応答しないときに停止が永久に返らなくなるのを防ぐ（`stopWorker` と同じ扱い）。
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx),
			time.Duration(o.cfg.Herdr.ReadTimeoutMs)*time.Millisecond)
		defer cancel()
	}
	if _, err := o.herdr.PaneClose(ctx, herdr.PaneCloseParams{PaneID: paneID}); err != nil {
		o.logger.Warn("direct chat のために開いた pane を閉じられませんでした",
			"identifier", rs.issue().Identifier, "pane_id", paneID, "error", err)
		return
	}
	o.logger.Info("direct chat のために開いた pane を閉じました",
		"identifier", rs.issue().Identifier, "pane_id", paneID)
}

// noteDirectChatSetupFailure は用意の失敗を専用の記録へ1つ数える（設計 3-82d）。
//
// issueID: project item の ID。
// reason: 落ちた理由。
// 戻り値: 数え終わったあとの回数。
func (o *Orchestrator) noteDirectChatSetupFailure(issueID, reason string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	note, ok := o.directChatSetupFailures[issueID]
	if !ok {
		note = &directChatSetupFailure{}
		o.directChatSetupFailures[issueID] = note
	}
	note.Count++
	note.LastAt = o.now()
	note.Reason = summaryLine(reason)
	return note.Count
}

// forgetDirectChatSetupFailure は用意の失敗の記録を消す（用意が成功したとき。設計 3-82d）。
//
// issueID: project item の ID。
func (o *Orchestrator) forgetDirectChatSetupFailure(issueID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.directChatSetupFailures, issueID)
}

// forgetDirectChatSetupFailuresNotIn は、この巡回の direct chat の候補に無い issue の記録を消す（設計 3-82d）。
//
// candidates: この巡回の direct chat の候補。
func (o *Orchestrator) forgetDirectChatSetupFailuresNotIn(candidates []tracker.Issue) {
	keep := make(map[string]bool, len(candidates))
	for _, issue := range candidates {
		keep[issue.ID] = true
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for id := range o.directChatSetupFailures {
		if !keep[id] {
			delete(o.directChatSetupFailures, id)
		}
	}
}

// directChatSetupBackoff は、用意が直前に落ちてから間隔が空いていないかを返す（設計 3-82c の門7）。
//
// **間隔は通常の着手のバックオフと同じ計算である**（`retryBackoff`）。
//
// issueID: project item の ID。
// 戻り値の1つ目: あと何秒待つか。
// 戻り値の2つ目: まだ待つなら true。
func (o *Orchestrator) directChatSetupBackoff(issueID string) (time.Duration, bool) {
	o.mu.Lock()
	note, ok := o.directChatSetupFailures[issueID]
	var count int
	var last time.Time
	if ok {
		count, last = note.Count, note.LastAt
	}
	o.mu.Unlock()
	if !ok || count == 0 {
		return 0, false
	}
	wait := retryBackoff(count-1, time.Duration(o.cfg.Agent.MaxRetryBackoffMs)*time.Millisecond)
	left := last.Add(wait).Sub(o.now())
	if left <= 0 {
		return 0, false
	}
	return left, true
}

// writeDirectChatAssigneeFailureAsync は、担当者が0人か2人以上の direct chat のカードへ
// `failure_state` を書きに行く（設計 3-82h の判定の表の順1）。
//
// **巡回のループの外で書く**（設計 3-82h / 3-8）。巡回のループの中で決めるのは、
// どの表の行に当たったかと、印の出し入れだけである。
//
// **判定に使った担当者は、その巡回の取得の値である。**書く直前に取り直さない（設計 3-82h）。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 対象の issue（その巡回で読んだ値）。
func (o *Orchestrator) writeDirectChatAssigneeFailureAsync(ctx context.Context, issue tracker.Issue) {
	logins := assigneeLogins(issue)
	list := strings.Join(logins, ", ")
	if len(logins) == 0 {
		list = i18n.T(i18n.KeyOrchestratorDirectChatNoAssignees)
	}
	body := i18n.T(i18n.KeyOrchestratorDirectChatAssigneesInvalid,
		len(logins), list, o.cfg.Tracker.DirectChatState, o.cfg.Tracker.FailureState)
	o.logger.Warn("direct chat のカードの担当者が1人ではないので、failure_state へ動かします",
		"identifier", issue.Identifier, "担当者の人数", len(logins), "担当者", list,
		"failure_state", o.cfg.Tracker.FailureState)
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		o.writeDirectChatFailure(ctx, issue, body)
	}()
}

// writeDirectChatFailure は、direct chat のカードへ `failure_state` を書く（設計 3-82h の「書く経路」）。
//
// **3-82e の不変条件2（`direct_chat_state` のカードへ Status を書かない）の、唯一の例外である。**
// **呼ぶ場面は2つある。**担当者が0人か2人以上のときと、用意の失敗が上限に達したとき。
//
// **取り直した Status が `direct_chat_state` のときだけ書く（許可リスト）。**
// `UpdateStatus` は書く前に取り直し、取り直した値が拒否リストにあれば書かない。
// **そこで拒否リストを「カンバンの選択肢のうち `direct_chat_state` 以外の全部」にする。**
// 人間が既に `Ready` などへ動かしていれば書かない。**拒否リストを `terminal_states` などにすると、
// 人間が戻した直後のカードを遅れた機械が上書きする。**
//
// **取り直した値が未設定（空）なら書かない。**選択肢の一覧では表せないので、拒否リストへ空文字を
// 1つ足して表す（`UpdateStatus` は取り直した値と拒否リストを同じ正規化で比べる）。
//
// **選択肢の写しが空なら書かず、WARN を1行出す。**起動直後に写しが取れていないと、
// 0人のカードが黙って残るためである。
//
// **コメントは `UpdateStatus` が実際に書いたとき（`Wrote`）だけ書く。**見張っている全台が
// 書こうとするが、実際に書けるのは取り直しの時点で先に書いた1台である。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 対象の issue。
// body: 書けたときに issue へ書くコメント（`<!-- continuo:self -->` は `postComment` が足す）。
func (o *Orchestrator) writeDirectChatFailure(ctx context.Context, issue tracker.Issue, body string) {
	options := o.tracker.StatusOptionNames()
	if len(options) == 0 {
		o.logger.Warn("カンバンの Status の選択肢をまだ読めていないので、direct chat のカードへ failure_state を書けません"+
			"（次の巡回でやり直します）",
			"identifier", issue.Identifier)
		return
	}
	blocked := make([]string, 0, len(options)+1)
	for _, opt := range options {
		if config.IsDirectChatState(o.cfg.Tracker, opt) {
			continue
		}
		blocked = append(blocked, opt)
	}
	// **未設定（空）の Status へは書かない**（上の説明）。
	blocked = append(blocked, "")
	moved, err := o.tracker.UpdateStatus(ctx, issue.ID, o.cfg.Tracker.FailureState, blocked)
	if err != nil {
		o.logger.Warn("direct chat のカードへ failure_state を書けませんでした（次の巡回でやり直します）",
			"identifier", issue.Identifier, "failure_state", o.cfg.Tracker.FailureState, "error", err)
		return
	}
	if !moved.Wrote {
		// **既にその値だった（`Reached`）・人間が動かしていた・別の機械が先に書いた、のどれかである。**
		// **コメントは書かない。**
		o.logger.Info("direct chat のカードへ failure_state を書きませんでした（取り直すと direct chat ではありませんでした）",
			"identifier", issue.Identifier, "取り直した Status", moved.Previous)
		return
	}
	nodeID := issueNodeID(issue)
	if nodeID == "" {
		return
	}
	if err := o.postComment(ctx, nodeID, body); err != nil {
		o.logger.Warn("direct chat のカードを failure_state へ動かした理由を issue へ書けませんでした",
			"identifier", issue.Identifier, "error", err)
	}
}

// letGoOfDirectChatAsync は、担当者が別の1人に替わった direct chat の run から手を離す
// （設計 3-82h の「手を離す経路」）。
//
//	段1 direct chat を抜けさせる（印を下ろし、捨てるもの3つと時計を処理する）
//	段2 `stopBecauseHandoffLost` と同じ形で片付ける。Status を書かず、`after_run` を走らせず、
//	    コメントを書かない。pane を閉じ、印を外す。worktree は残す
//	段3 巡回のループの外で行う
//
// **抜けたあとは direct chat の run ではないので、`stopWorker` の門は開く。**
// **`after_run` を走らせない理由。**外された機械が push すると、新しい担当者の続きと衝突する（3-77c）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run（用意中ではないもの）。
// issue: 取り直した issue。
func (o *Orchestrator) letGoOfDirectChatAsync(ctx context.Context, rs *runState, issue tracker.Issue) {
	// 段1。
	rs.leaveDirectChatMode()
	rs.resetStallClock(o.now())
	if rs.beginTerminal() != terminalClaimed {
		// **既に終わらせる処理が走っている。**そちらが片付ける。
		return
	}
	o.logger.Warn("direct chat のカードの担当者が別のアカウントに替わったので、この PC の continuo は手を離します"+
		"（pane を閉じます。push しません。カンバンへは書きません。after_run も走らせません。worktree は残します）",
		"identifier", issue.Identifier, "担当者", strings.Join(assigneeLogins(issue), ", "))
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		// **後片付けは「止めろ」と言われても最後までやる**（`stopAndReleaseAsync` と同じ理由）。
		cleanupCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx), time.Duration(o.cfg.Herdr.ReadTimeoutMs)*time.Millisecond)
		defer cancel()
		o.stopWorker(cleanupCtx, rs)
		o.release(rs)
	}()
}

// directChatPanes は、この巡回で使う pane の写像を1回だけ作る（設計 3-82b）。
//
// **候補1件ごとに引き直してはならない。**`pane.list` は機械中の pane を全部返すので、
// 候補が N 件あると巡回1回で N 本になる。
//
// **引けなかったときは、その理由を返す。**呼び出し側は「pane が無い」と混ぜてはならない。
//
// ctx: `pane.list` に適用するコンテキスト。
// 戻り値の1つ目: 解決済みの cwd から pane を引く写像。
// 戻り値の2つ目: 引けなかった理由。
func (o *Orchestrator) directChatPanes(ctx context.Context) (map[string]herdr.Pane, error) {
	// **`agent.list` は投げない**（設計 3-82b）。pane の有無しか要らない。
	return o.paneMapByCwd(ctx)
}

// directChatPaneExists は、その issue の worktree に pane が1枚でもあるかを返す（設計 3-82c の門4）。
//
// **「Claude Code が居るか」では判定しない。**判定する手段が無いためである。
// herdr が agent を登録していないことは、Claude Code が居ないことを意味しない（3-80）。
// **取り違えて `agent.start` を投げると、人間が話している画面へ `claude …` というコマンド行が届く。**
// **だから「pane が1枚でもあれば触らない」に倒す。**取りこぼす側の代償は「人間が自分で立て直す」であり、
// 取り違える側の代償は「会話が汚れる」である。**前者は人間が気づけるが、後者は気づけない。**
//
// panes: この巡回で1回だけ作った pane の写像（`directChatPanes` の戻り値）。
// issue: 対象の issue。
// 戻り値の1つ目: pane が1枚でもあれば true。
// 戻り値の2つ目: worktree の置き場所を決められなかった場合のエラー
// （**エラーのときは「分からない」なので、触らない側に倒すこと**）。
func (o *Orchestrator) directChatPaneExists(panes map[string]herdr.Pane, issue tracker.Issue) (bool, error) {
	loc, warnings, err := workspace.Locate(o.ws.ResolvedRoot(), o.cfg.Herdr.Worktree.BranchTemplate, toIssueRef(issue))
	if err != nil {
		return false, err
	}
	// **正規化で情報が落ちたことを黙って捨てない**（着手の段3 の呼び出しと同じ扱い）。
	for _, w := range warnings {
		o.logger.Warn("worktree の置き場所の正規化で情報が落ちました",
			"identifier", issue.Identifier, "警告", w.Message)
	}
	want, ok := resolvePath(loc.Path)
	if !ok {
		// **まだ worktree が無い。**`resolvePath` は実体のあるパスしか解決しないので、
		// ここは「これから作る」場合に必ず通る。**pane も在りようがない。**
		want = loc.Path
	}
	_, found := panes[want]
	return found, nil
}

// postDirectChatReady は「話しかけられます」を issue へ1件書く（設計 3-82d の用意の段3）。
//
// **これを書かないと、人間はカードを動かしたあと、いつ pane ができたのかを知る手段が無い。**
// continuo は Status を動かさないので、カンバンは1バイトも変わらない。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 対象の issue。
func (o *Orchestrator) postDirectChatReady(ctx context.Context, issue tracker.Issue) {
	nodeID := issueNodeID(issue)
	if nodeID == "" {
		// draft issue にはコメントできない。
		return
	}
	// **先頭の印は付けない。**`postComment` が `self_marker` を付ける。
	body := i18n.T(i18n.KeyOrchestratorDirectChatReady,
		o.cfg.Tracker.StatusSignalPrefix,
		o.cfg.Tracker.FailureState,
		strings.Join(o.cfg.Tracker.ActiveStates, " / "),
	)
	// **`o.tracker.PostComment` を直に呼ばない。**手元の絶対パスを縮める1箇所を迂回する
	// （`test/internal/redact` が機械で止めている）。
	if err := o.postComment(ctx, nodeID, body); err != nil {
		o.logger.Warn("direct chat の案内を issue へ書けませんでした（pane は用意できています）",
			"identifier", issue.Identifier, "error", err)
	}
}

// postDirectChatHold は、direct chat から作業中の Status へ戻したときに hold のコメントを1件書く
// （設計 3-82h の「戻したときに hold を書く」）。
//
// **人間向けの文だけを入札の hold と別に決める。**先頭の印と、そのあとの JSON は入札の hold と同じにする
// （`assignee` に自分のログイン名、`branch` にこの run の branch）。`ParseHold` は JSON を読めないと
// hold として数えず、`LatestHoldFor` は `assignee` で絞るので、**JSON を落とすと hold が無いのと同じになる。**
//
// **書けなかったら WARN を1行出して続ける。**戻した run は止めない。
//
// **書かないと何が起きるか。**用意した run には hold が1件も無いので、別の機械からは
// 「人間が付けた担当者」に見え、3巡回と60秒のあとに「担当者を外してください」という案内を
// 公開の issue へ投稿する。**書けば、戻した時点から18時間（3-77b）を数え直す。**
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 対象の issue。
func (o *Orchestrator) postDirectChatHold(ctx context.Context, issue tracker.Issue) {
	nodeID := issueNodeID(issue)
	if nodeID == "" {
		return
	}
	viewer, ok := o.viewerIdentity(ctx)
	if !ok {
		o.logger.Warn("gh の持ち主が分からないので、direct chat から戻したことを hold に書けません（run は続けます）",
			"identifier", issue.Identifier)
		return
	}
	body := handoff.FormatDirectChatHold(handoff.Hold{
		Assignee: viewer.Login,
		Branch:   o.branchNameFor(issue),
		At:       o.now(),
	})
	if err := o.postOwnMarkedComment(ctx, nodeID, body); err != nil {
		o.logger.Warn("direct chat から戻したことを hold に書けませんでした（run は続けます）",
			"identifier", issue.Identifier, "error", err)
	}
}

// writeRunningStateOnReturn は、direct chat から `dispatch_state` へ戻されたときに `running_state` を書く
// （設計 3-82g の表の2行目）。
//
// **`dispatch_state` そのものとの一致で判定する**（`directChatReturnState`）。
// **3-82g の書き込みと用意の段3 は、この同じ関数を呼ぶ**（`UpdateStatus` の呼び出しを増やさない。設計 3-82d）。
//
// **書けなくても続ける。**指示を送るほうが、Status の見た目より重い。次の巡回で書き直しはしない
// （判断票6周目。既存の 3-82g の書き込みと同じ扱い）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 戻した run。
// issue: 取り直した issue（`State` は戻された先）。
func (o *Orchestrator) writeRunningStateOnReturn(ctx context.Context, rs *runState, issue tracker.Issue) {
	target, need := directChatReturnState(o.cfg.Tracker, issue.State)
	if !need {
		return
	}
	moved, err := o.tracker.UpdateStatus(ctx, issue.ID, target, o.protectedStates())
	switch {
	case err != nil:
		o.logger.Warn("direct chat から戻った issue の Status を書けませんでした（指示は送ります）",
			"identifier", issue.Identifier, "書こうとした Status", target, "error", err)
	case moved.Reached:
		rs.setIssueState(target)
		rs.setLastWrittenState(target)
		o.postStatusMove(ctx, issue.Identifier, issueNodeID(issue),
			newStatusMove(moved, target),
			"direct chat から continuo の管理へ戻ったためです")
	}
}

// directChatReturnState は、direct chat から戻った先で書くべき Status を返す（設計 3-82g）。
//
// **`dispatch_state`（既定 `Ready`）へ戻されたときだけ、`running_state` を書く。**
// 書かないと2つ壊れる。状態ごとの上限（`agent.max_concurrent_agents_by_state`）は
// `running_state` の run だけを数えるので1つ超えて走る。そして着手の段2 はこの run では
// 二度と通らないので、カードは永久に着手待ちに見える。
//
// **「`active_states` にあって `running_state` でない」で判定してはならない。**
// 3つ目の作業中 Status を書いている利用者のカードを勝手に書き換えることになる。
//
// cfg: tracker の設定。
// state: 戻った先の Status。
// 戻り値の1つ目: 書くべき Status 名。
// 戻り値の2つ目: 書く必要があるなら true。
func directChatReturnState(cfg config.TrackerConfig, state string) (string, bool) {
	if !strings.EqualFold(strings.TrimSpace(state), strings.TrimSpace(cfg.DispatchState)) {
		return "", false
	}
	if strings.EqualFold(strings.TrimSpace(cfg.DispatchState), strings.TrimSpace(cfg.RunningState)) {
		// **設定の検査が起動前に弾いているので、ここへは来ない。**来ても書きに行かない。
		return "", false
	}
	return cfg.RunningState, true
}

// returnFromDirectChatAsync は、direct chat から作業中の Status へ戻した run の後始末を行う
// （設計 3-82b の段4・3-82g）。
//
// **巡回のループから同期で呼んではならない。**ここは通信を最大3本以上行う
// （`UpdateStatus` は取り直しと書き込み、hold の `PostComment`、記録の `PostComment`）。
//
// **続きの指示を送る印は、書き込みが終わってから立てる**（設計 3-82b の段4）。
// **送る直前に `agent.get` で応答を書いている最中かを見る**（`busyCheckBeforeSend`。turn ループが見る）。
// 人間が話しかけた直後（応答を書いている最中）に戻すのは自然な操作で、そこへ投げると turn が混ざる。
//
// **turn 数は数え直さない**（設計 3-82j）。人間が2回切り替えるだけで上限が外れる形にはしない。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 戻す run。
// issue: 取り直した issue。
func (o *Orchestrator) returnFromDirectChatAsync(ctx context.Context, rs *runState, issue tracker.Issue) {
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		o.writeRunningStateOnReturn(ctx, rs, issue)
		o.postDirectChatHold(ctx, issue)
		o.logger.Info("continuo の管理へ戻りました（同じ pane へ続きの指示を送ります）",
			"identifier", issue.Identifier, "状態", issue.State)
		rs.setBusyCheckBeforeSend()
		rs.setNeedsPrompt()
	}()
}

// abortTerminalForHuman は「人間が direct chat へ引き取ったので、この run を終わらせるのをやめる」を判定する
// （設計 3-82f）。
//
// **終わらせる処理は、印を取ってから終わるまでに長くかかる。**その間に人間がカードを動かすことがある。
// **`stopWorker` の門だけでは足りない。**あの門は pane を守るが、この経路はそのあとで印まで外す。
//
// **終え方は、その run の `PaneID` と、その pane で `agent.start` が済んでいるかで分ける。**
// **コードの位置では分けない**（`stopWorker` は direct chat の印があると閉じずに戻るので、
// 位置では pane の生死が決まらない）。
//
//	`PaneID` が空でなく `agent.start` が済んでいる … 終わらせる処理をやめ、印を残す（巡回が direct chat へ入れる）
//	`PaneID` が空、または `agent.start` がまだ     … 後者なら自分で開いた pane を ID で閉じ、印を外す
//	                                                 （次の巡回で 3-82c が pane を用意し直す）
//
// **どちらでも、終端の権利（`claimTerminal` で取ったもの）を `endTerminal` で返す。**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// reason: 終わらせようとしていた理由（ログに出す）。
// 戻り値: やめたなら true（呼び出し元はそこで止まる）。
func (o *Orchestrator) abortTerminalForHuman(ctx context.Context, rs *runState, reason string) bool {
	if !rs.inDirectChatMode() {
		return false
	}
	paneID, started := rs.paneState()
	if paneID != "" && started {
		o.logger.Info("人間が引き取ったので、この run を終わらせるのをやめます（pane も印も worktree も残します）",
			"identifier", rs.issue().Identifier, "やめた理由", summaryLine(reason))
		rs.endTerminal()
		return true
	}
	// **pane の無い印を残してはならない**（誰も気づかない。設計 3-82j）。印を外せば、
	// 次の巡回で 3-82c が pane を用意し直す。**Status を書かず、コメントを書かない。**
	o.logger.Info("人間が引き取りましたが、この run の pane はもう Claude Code を持っていないので印を外します"+
		"（次の巡回で pane を用意し直します）",
		"identifier", rs.issue().Identifier, "やめた理由", summaryLine(reason), "pane_id", paneID)
	if paneID != "" {
		// **continuo が開いたばかりのシェルである**（`agent.start` がまだ済んでいない）。
		o.closeDirectChatSetupPane(ctx, rs)
	}
	rs.endTerminal()
	o.release(rs)
	return true
}

// addToCloseSet は、worktree を「agent 名を問わず閉じる worktree の集合」へ入れる（設計 3-82f）。
//
// **閉じるのは、印を持たずに Status が `active_states` へ戻った巡回だけである**（`reconcileWorktrees`）。
// それ以外の Status では、閉じずに集合に残す。
//
// itemID: project item の ID。
// path: worktree の絶対パス。
func (o *Orchestrator) addToCloseSet(itemID, path string) {
	if itemID == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closeSet[itemID] = path
}

// removeFromCloseSet は、閉じる集合から外す（設計 3-82f）。
//
// **外すのは3つの場面だけである。**閉じ終えたとき・走査に出てこなくなったとき・用意の段1 で印を付けたとき。
//
// itemID: project item の ID。
func (o *Orchestrator) removeFromCloseSet(itemID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.closeSet, itemID)
}

// inCloseSet は、閉じる集合に入っているかを返す（設計 3-82f）。
//
// **集合にあるあいだは、通常の候補のループ（`dispatchCandidates`）はその issue を飛ばす。**
// **direct chat の1パスは集合を見ない。**見ると、`Blocked` や `In Review` から入った issue
// （worktree が残り、印が無い）に pane が永久に来ない。
//
// itemID: project item の ID。
// 戻り値: 入っていれば true。
func (o *Orchestrator) inCloseSet(itemID string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, ok := o.closeSet[itemID]
	return ok
}

// pruneCloseSet は、走査に出てこなくなった worktree を閉じる集合から外す（設計 3-82f）。
//
// present: 走査で身元ファイルを読めた worktree の project item の ID。
func (o *Orchestrator) pruneCloseSet(present map[string]bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for id := range o.closeSet {
		if !present[id] {
			delete(o.closeSet, id)
		}
	}
}
