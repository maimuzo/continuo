package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/hookserver"
)

// turnOutcome は1つの turn を送って待った結果である。
type turnOutcome int

const (
	// turnEnded は turn が終わったことを表す（herdr の待ち受けが返り、`Stop` を受けていて、
	// `<task-notification>` が続かなかった）。
	turnEnded turnOutcome = iota
	// turnBlocked は権限の確認で止まったことを表す（設計 3-11）。
	turnBlocked
	// turnStalled は待ち受けが返ったのに `Stop` が来なかったことを表す（設計 3-2）。
	// 権限の確認が esc で取り消された場合などである。
	turnStalled
	// turnTimedOut は turn の時間切れである（枠待ちではない）。
	turnTimedOut
	// turnAborted は ctx が終わったことを表す。
	turnAborted
)

// startTurnLoop は run ごとの turn ループの goroutine を起こす（設計 3-8）。
//
// **巡回のループはこれでブロックしない。**`agent.prompt` を wait つきで呼ぶと turn の
// 終わりまで返らない（既定1時間）ので、巡回のループの中で同期的に呼んではならない。
//
// **同じ run に2本目の goroutine を立てない。**既に走っていれば起こさずに false を返す。
//
// **起こせなかったことを黙って捨ててはならない。**stall 検知が worker を止めても、古い
// turn ループは `agent.prompt` の待ち受け（既定1時間）から戻るまで印を下ろさない。その間に
// 再 dispatch が走ると、新しい Claude Code を起動したのに turn ループが1本も立たない。
// **呼び出し側は false を受けたら `setNeedsPrompt` を立て、次の巡回で起こし直す。**
//
// ctx: turn ループへ渡すコンテキスト。
// rs: 対象の run。
// awaitFirst: 真なら、**最初の turn を送らずに、走っている turn の終わりを待つところから入る**
// （復元で `agent_status` が `working` の run を引き継いだ場合。設計 3-4 の段5a2）。
// 戻り値: goroutine を起こしたら true。既に走っていた・run が終わっていたら false。
func (o *Orchestrator) startTurnLoop(ctx context.Context, rs *runState, awaitFirst bool) bool {
	rs.mu.Lock()
	if rs.turnLoopRunning || rs.Finished {
		rs.mu.Unlock()
		return false
	}
	rs.turnLoopRunning = true
	rs.mu.Unlock()

	// **自分が回す worker の世代を覚えておく**（設計 3-21）。世代が変わったあとに
	// この goroutine が目を覚ましても、run を諦めてはならない。
	epoch := rs.workerGeneration()

	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		defer func() {
			rs.mu.Lock()
			rs.turnLoopRunning = false
			rs.mu.Unlock()
		}()
		o.turnLoop(ctx, rs, epoch, awaitFirst)
	}()
	return true
}

// turnLoop は1つの run の turn を、終わるまで送り続ける（設計 3-8）。
//
//	1回目   … 設定の本文（5-3）を text/template で描画したもの
//	2回目〜 … 継続の指示のみ（5-4）。1回目の本文は送り直さない
//	打ち切り … max_turns（既定20）に達したら failure_state へ落とす
//
// **turn 数は continuo が送った回数だけで数える**（設計 3-14）。Claude Code 自身が投入する
// `<task-notification>` は数えない。**枠が明けたときに送る継続の指示は数える**（設計 3-27）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// epoch: この turn ループが回す worker の世代（設計 3-21）。**世代が変わっていたら
// run を諦めずに黙って抜ける。**巡回の stall 検知が先に諦めた run を二重に諦めないためである。
// awaitFirst: 真なら、1周目は turn を送らずに turn の終わりを待つ（設計 3-4 の段5a2）。
func (o *Orchestrator) turnLoop(ctx context.Context, rs *runState, epoch int, awaitFirst bool) {
	for {
		if ctx.Err() != nil || !rs.currentWorker(epoch) {
			return
		}

		snap := rs.snapshot()

		var outcome turnOutcome
		if awaitFirst {
			// **引き継いだ run である。turn を送らずに、走っている turn の終わりを待つ**
			// （設計 3-4 の段5a2「hook を待ち、来なければ stall 検知で拾う」の前半）。
			// **送ると turn が混ざる。**待ち切れなければ turn の時間切れとして扱い、
			// 巡回の stall 検知と同じ経路（abandonRun）へ落とす。
			awaitFirst = false
			o.logger.Info("引き継いだ run の turn の終わりを待ちます（turn は送りません）",
				"identifier", snap.Identifier)
			outcome = o.confirmTurnEnd(ctx, rs, o.turnDeadline(), false)
		} else {
			if snap.TurnCount >= o.cfg.Agent.MaxTurns {
				o.finishRun(ctx, rs, o.cfg.Tracker.FailureState,
					fmt.Sprintf("turn の上限（max_turns=%d）に達しました", o.cfg.Agent.MaxTurns))
				return
			}

			text, err := o.buildTurnText(rs, snap)
			if err != nil {
				o.logger.Warn("プロンプトを組み立てられません", "identifier", snap.Identifier, "error", err)
				o.failRun(ctx, rs, fmt.Sprintf("プロンプトを組み立てられません: %v", err))
				return
			}

			outcome = o.sendTurn(ctx, rs, text)
		}
		if !rs.currentWorker(epoch) {
			// **待っている間に、巡回の stall 検知などが先にこの run を諦めていた。**
			// ここで諦め直すと RetryCount が2倍の速さで消費され、引き渡しのコメントも
			// 二重に投稿される（設計 3-21）。
			o.logger.Debug("待ち受けから戻ったときには別の経路が run を終わらせていました",
				"identifier", snap.Identifier)
			return
		}
		switch outcome {
		case turnAborted:
			return
		case turnBlocked:
			// **次を投げる前に必ず esc を送る**（設計 3-11。送らずに投げると保留中の
			// 権限要求が承認されて実行される。3/3 で再現）。
			o.sendEscape(ctx, rs)
			o.finishRun(ctx, rs, o.cfg.Tracker.FailureState,
				"権限の確認で止まりました（esc を送りました。人間の判断が要ります）")
			return
		case turnStalled:
			o.abandonRun(ctx, rs, "待ち受けが返ったのに Stop が来ませんでした")
			return
		case turnTimedOut:
			o.abandonRun(ctx, rs,
				fmt.Sprintf("turn が時間切れになりました（turn_timeout_ms=%d）", o.cfg.Claude.TurnTimeoutMs))
			return
		case turnQuotaRecovered:
			// 枠が明けた。**次の turn を送る（この送信は turn 数に数える。設計 3-27）。**
			continue
		case turnEnded:
			if done := o.handleTurnEnd(ctx, rs); done {
				return
			}
		}
	}
}

// buildTurnText はこの turn で送る本文を決める（設計 3-8 / 5-3 / 5-4）。
//
// **分岐の基準は turn 数ではなく「いまのセッションに会話履歴があるか」である。**
//
//	FreshSession が真  … 1回目の本文（5-3）。新しい UUID で起動した直後の
//	                     セッションには会話履歴が無いので、issue の URL も完了の作法も
//	                     ここで渡さないと1文字も伝わらない
//	FreshSession が偽  … 継続の指示（5-4）のみ。本文は送り直さない（`SPEC.md` 7.1）
//
// **turn 数で分岐してはならない。**復元で引き継いだ run は turn 数を 1 から数え直すが
// （設計 3-4 の段7）、セッションは引き継いでいるので **1回目をやり直してはならない**
// （段5c）。逆に、バックオフ明けの再 dispatch は turn 数を引き継ぐが**セッションは
// 新しい**ので、継続の指示だけでは通じない。
//
// **試行回数（`.attempt`）は再 dispatch で埋まる。**1回目の着手では nil である
// （`RetryCount` が 0 のため）。
//
// rs: 対象の run。
// snap: 判定に使う写し。
// 戻り値の1つ目: 送る本文。
// 戻り値の2つ目: 1回目のテンプレートの描画に失敗した場合のエラー
// （`missingkey=error` なので、5-3 の一覧に無い変数を書くとここで落ちる）。
func (o *Orchestrator) buildTurnText(rs *runState, snap runSnapshot) (string, error) {
	if !snap.FreshSession {
		return BuildContinuationPrompt(
			snap.TurnCount+1,
			o.cfg.Agent.MaxTurns,
			rs.missingSignal(),
			o.cfg.Tracker.RunningState,
			o.cfg.Tracker.StatusSignalPrefix,
		), nil
	}
	var attempt *int
	if snap.RetryCount > 0 {
		// 再 dispatch である。**前回は完了せずに終わっている**ことを本文へ渡す（5-3）。
		n := snap.RetryCount + 1
		attempt = &n
	}
	return o.renderFirstPrompt(rs.issue(), attempt)
}

// sendTurn は turn を1つ送り、turn の終わりまで待つ（設計 3-2 の「判定の規則」）。
//
//  1. agent.prompt を wait つきで送る（until = wait_until / timeout_ms = turn_timeout_ms）
//     **agent.wait を単独で使わない。**いまの状態が until に含まれると 0.006 秒で即返るため、
//     投入直後の idle を turn の終わりと取り違える
//  2. timeout で返ったら枠待ちかを判定する。枠待ちなら agent.wait で待ち直す
//     （**agent.prompt は再送しない。**二重に投入される）
//  3. blocked で返ったら turnBlocked
//  4. idle / done で返ったら、Stop hook を受けているかを確かめる
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// text: 送る本文。
// 戻り値: turn の結果。
func (o *Orchestrator) sendTurn(ctx context.Context, rs *runState, text string) turnOutcome {
	turnCount := rs.beginTurn(o.now())
	deadline := o.turnDeadline()
	o.logger.Info("turn を送ります",
		"identifier", rs.issue().Identifier, "turn", turnCount, "max_turns", o.cfg.Agent.MaxTurns)

	res, err := o.herdr.AgentPrompt(ctx, herdr.AgentPromptParams{
		Target: rs.agentName(),
		Text:   text,
		Wait: &herdr.AgentWaitOptions{
			TimeoutMs: o.cfg.Claude.TurnTimeoutMs,
			Until:     waitUntilStatuses(o.cfg.Claude.WaitUntil),
		},
	})
	if err != nil {
		if ctx.Err() != nil {
			return turnAborted
		}
		if herdr.IsCode(err, herdr.ErrCodeTimeout) {
			return o.afterWaitTimeout(ctx, rs, deadline)
		}
		o.logger.Warn("turn を送れませんでした", "identifier", rs.issue().Identifier, "error", err)
		return turnStalled
	}

	switch res.Agent.AgentStatus {
	case herdr.AgentStatusBlocked:
		return turnBlocked
	case herdr.AgentStatusIdle, herdr.AgentStatusDone:
		return o.confirmTurnEnd(ctx, rs, deadline, true)
	default:
		// working / unknown のまま返るのは想定外である。Stop を確かめてから判断する。
		return o.confirmTurnEnd(ctx, rs, deadline, true)
	}
}

// afterWaitTimeout は待ち受けが timeout で返ったときの分岐である（設計 3-2 の段3 / 3-27）。
//
// **枠待ちなら agent.wait で待ち直す。**`agent.prompt` は再送しない（二重に投入される）。
// **枠待ちでなければ turn の時間切れとして打ち切る。**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// deadline: この turn の期限。
// 戻り値: turn の結果。
func (o *Orchestrator) afterWaitTimeout(ctx context.Context, rs *runState, deadline time.Time) turnOutcome {
	if !o.isQuotaWaiting(rs) {
		return turnTimedOut
	}

	resetAt, ok := o.quotaResetAt()
	rs.setWaitingQuota(resetAt)
	o.logger.Info("枠待ちと判定しました（stall の時計と turn の時計を止めます）",
		"identifier", rs.issue().Identifier, "resets_at", resetAt)

	// **1周あたり必ず poll_wait_ms は空ける**（下の待ち）。`agent.wait` は
	// **いまの状態が until に含まれると 0.006 秒で即返る**（設計 3-2 の実測）。
	// 枠を使い切って Claude Code が idle に落ちていると、`Stop` を取りこぼした場合に
	// どの return にも当たらないまま resets_at まで RPC を投げ続けることになる。
	pollWait := time.Duration(o.cfg.Claude.PollWaitMs) * time.Millisecond

	for {
		if ctx.Err() != nil {
			return turnAborted
		}
		iterationStart := o.now()
		// 枠待ちの間は agent.wait で待ち直す（設計 3-2 の段3）。
		res, err := o.herdr.AgentWait(ctx, herdr.AgentWaitParams{
			Target:    rs.agentName(),
			TimeoutMs: o.cfg.Claude.PollWaitMs,
			Until:     waitUntilStatuses(o.cfg.Claude.WaitUntil),
		})
		if err == nil {
			switch res.Agent.AgentStatus {
			case herdr.AgentStatusBlocked:
				rs.clearWaitingQuota(o.now())
				return turnBlocked
			case herdr.AgentStatusIdle, herdr.AgentStatusDone:
				if _, seen := rs.stopSeen(); seen {
					rs.clearWaitingQuota(o.now())
					return o.confirmTurnEnd(ctx, rs, deadline, true)
				}
			}
		} else if !herdr.IsCode(err, herdr.ErrCodeTimeout) {
			if ctx.Err() != nil {
				return turnAborted
			}
			o.logger.Warn("枠待ちの待ち直しに失敗しました", "identifier", rs.issue().Identifier, "error", err)
			rs.clearWaitingQuota(o.now())
			return turnStalled
		}

		// 枠が明けたか。**印を外す契機は「枠の resets_at を過ぎたこと」だけである**（設計 3-27）。
		if ok && !o.now().Before(resetAt) {
			rs.clearWaitingQuota(o.now())
			return o.afterQuotaReset(ctx, rs)
		}
		o.pollQuota(ctx)
		if !o.quotaAtFull() {
			rs.clearWaitingQuota(o.now())
			return o.afterQuotaReset(ctx, rs)
		}

		// **下限の待ち。**`agent.wait` が即返っても1周あたり poll_wait_ms は必ず空ける。
		if rest := pollWait - o.now().Sub(iterationStart); rest > 0 {
			if !sleepCtx(ctx, rest) {
				return turnAborted
			}
		}
	}
}

// turnDeadline はこの turn の期限を返す（`claude.turn_timeout_ms`）。
//
// 戻り値: 期限の時刻。
func (o *Orchestrator) turnDeadline() time.Time {
	return o.now().Add(time.Duration(o.cfg.Claude.TurnTimeoutMs) * time.Millisecond)
}

// sleepCtx は ctx を見張りながら d だけ待つ。
//
// ctx: 待ちを打ち切るコンテキスト。
// d: 待つ長さ。0以下なら待たない。
// 戻り値: 待ち切れたら true。ctx が終わったら false。
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// afterQuotaReset は枠が明けたときに、継続の指示を送ってよいかを決める（設計 3-27）。
//
// **Claude Code 2.1.234 は「枠のリセット時にセッションを自動継続する」機能を既定で持つ。**
// continuo がそこへ継続の指示を送ると**二重投入**になり、`blocked` のときと同じ構造で
// **投げた本文が消え、turn が混ざる。**そこで、送る前に `agent_status` を見る。
//
//	idle / done  … 送る（Claude Code は継続していない）
//	working      … **送らない。**Claude Code が自分で継続している。**hook を待つ**
//	blocked      … **送らない**（esc を送ってから failure_state へ。設計 3-11）
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値: turn の結果。`turnQuotaRecovered` なら turn ループが次の turn を送る
// （**この送信は turn 数に数える。**設計 3-27。数えないと、枠待ちと復帰を繰り返す間に
// max_turns が一度も発火しない）。
func (o *Orchestrator) afterQuotaReset(ctx context.Context, rs *runState) turnOutcome {
	status, err := o.agentStatus(ctx, rs)
	if err != nil {
		o.logger.Warn("枠明けの agent_status を読めません（継続の指示を送ります）",
			"identifier", rs.issue().Identifier, "error", err)
		return turnQuotaRecovered
	}

	switch status {
	case herdr.AgentStatusBlocked:
		return turnBlocked
	case herdr.AgentStatusWorking:
		// **Claude Code が自分で継続している。**送らずに hook を待つ（設計 3-27）。
		// Claude Code 2.1.234 以降、枠のリセット時にセッションを自動継続する機能が
		// 既定で有効である。そのまま送ると二重投入になり、投げた本文が消えて turn が混ざる。
		// **その turn は continuo が送っていないので turn 数に入らない。**
		o.logger.Info("枠が明けて Claude Code が自分で継続しました（継続の指示は送りません）",
			"identifier", rs.issue().Identifier)
		return o.confirmTurnEnd(ctx, rs, o.turnDeadline(), false)
	default:
		o.logger.Info("枠が明けたので継続の指示を1回送ります（この送信は turn 数に数えます）",
			"identifier", rs.issue().Identifier)
		return turnQuotaRecovered
	}
}

// turnQuotaRecovered は枠が明けたので継続の指示を送り直すべきことを表す。
//
// **turnLoop はこれを受けると次の turn を送る**（TurnCount が1つ増える。設計 3-27）。
const turnQuotaRecovered turnOutcome = 100

// isQuotaWaiting は「この run は枠待ちである」を判定する（設計 3-27）。
//
// **2条件の連言である。**
//
//	条件その1  percent が 100 に達している
//	条件その2  その run から stall_timeout_ms のあいだ hook が1件も来ていない
//
// **`severity` は見ない。**上限を示す値が何かを実測できていない。
// **`pause_above_percent`（既定95%）を超えただけでは枠待ちとみなさない**（95%は枠がまだ
// 残っている状態で、走行中の worker は普通に動ける）。
//
// rs: 判定する run。
// 戻り値: 枠待ちなら true。
func (o *Orchestrator) isQuotaWaiting(rs *runState) bool {
	if !o.quotaAtFull() {
		return false
	}
	snap := rs.snapshot()
	if snap.hookSeenThisTurn {
		// **条件その2 を入れる理由。**枠を使い切っていても、別の run は動いていることがある。
		// 枠の状態だけで全部の run の時計を止めると、固まった run を見逃す。
		stall := time.Duration(o.cfg.Claude.StallTimeoutMs) * time.Millisecond
		if stall <= 0 || o.now().Sub(snap.LastSeenAt) < stall {
			return false
		}
	}
	return true
}

// quotaAtFull は使い切っている枠があるかを返す（設計 3-27 の条件その1）。
//
// 戻り値: `percent` が 100 に達している枠があれば true。枠を読めていなければ false。
func (o *Orchestrator) quotaAtFull() bool {
	return o.quotaSnapshot().AtFullPercent()
}

// quotaResetAt は枠待ちを外す時刻を返す（設計 3-27 の「どの枠の時刻を見るか」）。
//
// **条件その1 を満たした枠のうち、`resets_at` がいちばん遅いものである。**
// `resets_at` が null の枠は判定から外す。
//
// 戻り値の1つ目: 外す時刻。
// 戻り値の2つ目: 時刻が分かれば true。
func (o *Orchestrator) quotaResetAt() (time.Time, bool) {
	return o.quotaSnapshot().LatestResetOfFullLimits()
}

// confirmTurnEnd は turn の終わりを確定させる（設計 3-2 の hook 側の規則）。
//
//	background_tasks が空でない        … まだ動いている。turn の終わりとして扱わない
//	background_tasks の項目が欠けている  … 判定不能。turn の終わりとみなさない
//	background_tasks が空配列          … settle_ms のあいだ待ち、
//	                                    `<task-notification>` が来なければ turn の終わりとする
//
// **`Stop` が1件も来ていなければ、settle_ms 待ってから stall として扱う**
// （権限の確認が esc で取り消された場合など。設計 3-11）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// deadline: この turn の期限。
// strictFirstWait: 「待ち受けが返った直後」なら真。真のときは、settle_ms のあいだに
// `Stop` が来なければ stall として扱う。**枠明けに Claude Code が自分で継続した場合は
// 偽を渡す**（まだ走り出したばかりで `Stop` は来ないため。設計 3-27）。
// 戻り値: turn の結果。
func (o *Orchestrator) confirmTurnEnd(
	ctx context.Context,
	rs *runState,
	deadline time.Time,
	strictFirstWait bool,
) turnOutcome {
	settle := time.Duration(o.cfg.Claude.SettleMs) * time.Millisecond
	pollWait := time.Duration(o.cfg.Claude.PollWaitMs) * time.Millisecond
	// firstWait が真の間は「待ち受けが返った直後」である。ここで Stop が来なければ stall。
	firstWait := strictFirstWait

	for {
		if ctx.Err() != nil {
			return turnAborted
		}

		stopAt, seen := rs.stopSeen()
		if !seen {
			patience := settle
			if !firstWait {
				// `<task-notification>` を受けたあとは turn が続いている最中である。
				// ここで settle_ms しか待たないと、正常な turn を stall と誤判定する。
				patience = pollWait
			}
			if !o.awaitHook(ctx, rs, patience, isEmptyStop) {
				if firstWait {
					return turnStalled
				}
				if o.now().After(deadline) {
					return o.afterWaitTimeout(ctx, rs, deadline)
				}
				if st, err := o.agentStatus(ctx, rs); err == nil && st == herdr.AgentStatusBlocked {
					return turnBlocked
				}
				continue
			}
			stopAt, _ = rs.stopSeen()
		}

		// settle_ms のあいだ `<task-notification>` が来ないことを確かめる（設計 1-3 / 3-2）。
		remaining := settle - o.now().Sub(stopAt)
		if !o.awaitHook(ctx, rs, remaining, isTaskNotification) {
			return turnEnded
		}
		// 来た。turn は続いている。待ち直す。
		rs.clearStopSeen()
		firstWait = false
	}
}

// awaitHook は run の受け口から、条件に合う hook が届くのを待つ。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// d: 待つ長さ。0以下なら、既に届いているぶんだけを見る。
// pred: 合致を判定する関数。
// 戻り値: 合致する hook が届けば true。
func (o *Orchestrator) awaitHook(
	ctx context.Context,
	rs *runState,
	d time.Duration,
	pred func(hookserver.HookEvent) bool,
) bool {
	if d < 0 {
		d = 0
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case ev := <-rs.hookCh:
			if pred(ev) {
				return true
			}
		case <-timer.C:
			return false
		case <-ctx.Done():
			return false
		}
	}
}

// isEmptyStop は「background_tasks が空配列の Stop」かを判定する（設計 3-2）。
//
// **項目が欠けている（nil）ときは偽である。**「判定不能」なので turn の終わりとみなさない。
//
// ev: 判定する hook。
// 戻り値: 空配列の Stop なら true。
func isEmptyStop(ev hookserver.HookEvent) bool {
	return ev.HookEventName == hookStop && ev.BackgroundTasks != nil && len(*ev.BackgroundTasks) == 0
}

// isTaskNotification は `<task-notification>` で始まる UserPromptSubmit かを判定する
// （設計 1-3 / 1-7）。**これが来たら turn は続いている。**
//
// ev: 判定する hook。
// 戻り値: `<task-notification>` なら true。
func isTaskNotification(ev hookserver.HookEvent) bool {
	return ev.HookEventName == hookUserPromptSubmit &&
		strings.HasPrefix(strings.TrimSpace(ev.Prompt), taskNotificationPrefix)
}

// agentStatus は agent の状態を1回読む。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値の1つ目: agent の状態。
// 戻り値の2つ目: 読めなかった場合のエラー。
func (o *Orchestrator) agentStatus(ctx context.Context, rs *runState) (herdr.AgentStatus, error) {
	got, err := o.herdr.AgentGet(ctx, herdr.AgentGetParams{Target: rs.agentName()})
	if err != nil {
		return herdr.AgentStatusUnknown, err
	}
	return got.Agent.AgentStatus, nil
}

// waitUntilStatuses は設定の文字列を herdr の状態の並びへ直す。
//
// **`blocked` を外すと、権限の確認で止まった turn を拾えず時間切れまで待つことになる**
// （設計 3-2。3/3 で再現）。
//
// names: `claude.wait_until` の値。
// 戻り値: 状態の並び。空なら nil（herdr の既定に任せる）。
func waitUntilStatuses(names []string) []herdr.AgentStatus {
	if len(names) == 0 {
		return nil
	}
	out := make([]herdr.AgentStatus, 0, len(names))
	for _, n := range names {
		out = append(out, herdr.AgentStatus(strings.TrimSpace(n)))
	}
	return out
}
