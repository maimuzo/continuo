package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/tracker"
)

// knownStates は continuo が意味を知っている Status 名をすべて返す（設計 3-50）。
//
// **設定に名前が出てくる Status がすべてである。**active_states・terminal_states・
// running_state・dispatch_state・failure_state・status_signal_map の遷移先を集める。
// **これは起動時にボードと照合する一覧と同じ集合である**（`tracker` の
// `requiredStatesForBootstrap`）。
//
// 戻り値: Status 名の並び（重複と空文字は落とす。順序は設定に書かれた順）。
func (o *Orchestrator) knownStates() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(o.cfg.Tracker.ActiveStates)+len(o.cfg.Tracker.TerminalStates)+3)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		key := strings.ToLower(s)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, s)
	}
	for _, s := range o.cfg.Tracker.ActiveStates {
		add(s)
	}
	for _, s := range o.cfg.Tracker.TerminalStates {
		add(s)
	}
	add(o.cfg.Tracker.RunningState)
	add(o.cfg.Tracker.DispatchState)
	add(o.cfg.Tracker.FailureState)
	for _, target := range o.cfg.Tracker.StatusSignalMap {
		if target != nil {
			add(*target)
		}
	}
	return out
}

// isKnownState は continuo がその Status の意味を知っているかを返す（設計 3-50）。
//
// state: 判定する Status 名。
// 戻り値: 設定に名前が出てくる Status なら true。
func (o *Orchestrator) isKnownState(state string) bool {
	return containsFold(o.knownStates(), state)
}

// handleUnknownState は「continuo が知らない Status」になった run をどうするかを決める
// （設計 3-50）。
//
//	turn が動いていて猶予の内側  … **止めない。**turn の終わりの表明を読んでから判断する
//	turn が動いていない          … その場で止める（待っても表明は出てこない）
//	猶予を過ぎた                … その場で止める（人間が止めたがっている可能性がある）
//
// **`terminal_states` へ動かされた場合はここへ来ない。**そちらは即座に終わる
// （人間が「終わった」と言っているので、待つ意味が無い）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// issue: 取り直した issue。
func (o *Orchestrator) handleUnknownState(ctx context.Context, rs *runState, issue tracker.Issue) {
	now := o.now()
	since := rs.noteUnknownState(now)
	grace := time.Duration(o.cfg.Tracker.UnknownStateGraceMs) * time.Millisecond
	waited := now.Sub(since)

	if grace > 0 && waited < grace && rs.turnLoopActive() {
		// **エージェントが表明を書けば、continuo が正しい Status へ戻す。**
		// turn の終わりより前に殺すと、その表明が読まれずに捨てられる。
		//
		// **人間が本気で止めたいときは、止まるのが turn 1回ぶん遅れる。**
		// そのことを毎回ログに出す（黙って遅らせない）。
		o.logger.Info("知らない Status になりましたが turn の終わりを待っています"+
			"（人間が止めたい場合、止まるのは turn が終わってからになります）",
			"identifier", issue.Identifier,
			"状態", issue.State,
			"待っている時間", formatDuration(waited),
			"猶予の上限", formatDuration(grace))
		return
	}

	if grace > 0 && waited >= grace {
		o.logger.Warn("知らない Status のまま猶予を過ぎたので worker を止めます",
			"identifier", issue.Identifier, "状態", issue.State, "猶予の上限", formatDuration(grace))
	} else {
		o.logger.Warn("continuo が知らない Status になったので worker を止めます（worktree は残します）",
			"identifier", issue.Identifier, "状態", issue.State)
	}
	o.stopForUnknownStateAsync(ctx, rs, issue.State)
}

// stopForUnknownStateAsync は知らない Status になった run を、理由を issue に残してから
// 止める（設計 3-50）。
//
// **巡回のループから呼ぶので、印だけ同期で確保して別の goroutine で回す**（設計 3-8）。
// **worktree は残す。**人間が Status を戻せば、そのまま作業を続けられる。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// state: 動かされた先の Status 名。
func (o *Orchestrator) stopForUnknownStateAsync(ctx context.Context, rs *runState, state string) {
	if !rs.beginTerminal() {
		return
	}
	reason := o.unknownStateReason(rs, state)
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		// **後片付けは「止めろ」と言われても最後までやる**（stopAndReleaseAsync と同じ判断）。
		// **期限は付ける。**相手が応答しないときに、停止が永久に返らなくなるのを防ぐ。
		cleanupCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx), time.Duration(o.cfg.Herdr.ReadTimeoutMs)*time.Millisecond)
		defer cancel()
		// **コメントを先に書く。**pane を閉じてから書くと、投稿に失敗したときに
		// 「黙って止まった」状態がそのまま残る。
		// **Status を動かした記録は添えない。**動かしたのは人間であって continuo ではない。
		o.postHandoffComment(cleanupCtx, rs, reason, statusMove{})
		o.runAfterRun(cleanupCtx, rs)
		o.stopWorker(cleanupCtx, rs)
		o.release(rs)
	}()
}

// finishRunUnknownState は turn の終わりに知らない Status のままだった run を、
// 理由を issue に残してから終える（設計 3-50）。
//
// **猶予を置いて待った先である。**待った結果、エージェントは正しい Status への表明を
// 出さなかった。**ここで黙って終えると、issue には何も残らない。**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// state: 動かされた先の Status 名。
func (o *Orchestrator) finishRunUnknownState(ctx context.Context, rs *runState, state string) {
	if !rs.beginTerminal() {
		return
	}
	reason := o.unknownStateReason(rs, state)
	// **Status を動かした記録は添えない。**動かしたのは人間であって continuo ではない。
	o.postHandoffComment(ctx, rs, reason, statusMove{})
	// **`failureState` は渡さない。**人間が自分で動かした Status を continuo が
	// 上書きしてはならない（設計 3-4 の「人間の操作を巻き戻さない」）。
	o.finishRunClaimed(ctx, rs, "", reason)
}

// unknownStateReason は知らない Status で止めたことを人間へ伝える文面を作る（設計 3-50）。
//
// **3つを必ず書く。**
//
//	どの Status になったか  … 元（continuo が最後に書いた値）と、いまの値
//	なぜ止めたか            … continuo が知らない Status だから
//	続けるにはどうするか    … active_states に入っている Status へ戻す
//
// rs: 対象の run。
// state: 動かされた先の Status 名。
// 戻り値: issue のコメントとログに載せる理由の文字列。
func (o *Orchestrator) unknownStateReason(rs *runState, state string) string {
	from := rs.lastWrittenState()
	moved := fmt.Sprintf("Status が `%s` になっていました。", state)
	if from != "" {
		moved = fmt.Sprintf("Status が `%s` から `%s` へ動いていました"+
			"（`%s` は continuo が最後に書いた値です）。", from, state, from)
	}
	back := strings.Join(o.cfg.Tracker.ActiveStates, " / ")
	if back == "" {
		back = "(WORKFLOW.md の `tracker.active_states` に1つも書かれていません)"
	}
	// **設定していない猶予を「待ちました」と書かない。**0 のときは待っていない。
	grace := fmt.Sprintf(
		"\n【turn の終わりは待ちました】continuo は turn が動いている間、"+
			"`tracker.unknown_state_grace_ms`（いまは %d ミリ秒）まで turn の終わりを待ち、"+
			"エージェントの表明を読んでから判断します。",
		o.cfg.Tracker.UnknownStateGraceMs)
	if o.cfg.Tracker.UnknownStateGraceMs <= 0 {
		grace = "\n【turn の終わりは待っていません】" +
			"WORKFLOW.md の `tracker.unknown_state_grace_ms` が 0 なので、" +
			"continuo は turn の途中でもその場で止めます。" +
			"エージェントの表明を読んでから判断させたいなら、この値を大きくしてください。"
	}
	return fmt.Sprintf(
		"continuo が知らない Status になったので、この issue の作業を止めました。\n%s"+
			"\n【なぜ止めたか】continuo は WORKFLOW.md に書かれた Status しか扱いません"+
			"（いま知っているのは %s です）。`%s` はそのどれでもないので、"+
			"この issue をどう進めればよいかを判断できません。"+
			"\n【続けるには】Status を `tracker.active_states` に入っている Status（%s）のいずれかへ戻してください。"+
			"次の巡回で着手し直します。worktree は残してあります（下記）。"+
			"\n【`%s` も continuo に扱わせたいときは】WORKFLOW.md の `tracker.active_states` か "+
			"`tracker.status_signal_map` にその名前を書き足してから、continuo を再起動してください。"+
			"%s",
		moved,
		strings.Join(o.knownStates(), " / "),
		state,
		back,
		state,
		grace)
}
