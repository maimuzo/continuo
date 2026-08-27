package orchestrator

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/tracker"
)

// knownStates は continuo が意味を知っている Status 名をすべて返す（設計 3-50）。
//
// **設定に名前が出てくる Status がすべてである。**active_states・terminal_states・
// running_state・dispatch_state・failure_state・status_signal_map の遷移先と、
// automated_state_rewrite の**戻す先**を集める。
// **これは起動時にボードと照合する一覧と同じ集合である**（`tracker` の
// `requiredStatesForBootstrap`）。
//
// **`automated_state_rewrite` のキーは入れない**（設計 3-54）。キーは
// 「ボードの自動化が書く、continuo が知らない Status」であり、ここへ入れると
// 知っている Status になって、書き戻しの分岐が二度と通らなくなる。
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
	// **map の反復順は決まらないので、名前順に並べてから足す。**
	// この一覧は issue のコメントとログにそのまま載るので、実行のたびに順序が変わってはならない。
	rewriteTargets := make([]string, 0, len(o.cfg.Tracker.AutomatedStateRewrite))
	for _, target := range o.cfg.Tracker.AutomatedStateRewrite {
		rewriteTargets = append(rewriteTargets, target)
	}
	sort.Strings(rewriteTargets)
	for _, target := range rewriteTargets {
		add(target)
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

// maxAutomatedRewrites は、1つの run が1つの Status について書き戻してよい回数である
// （設計 3-54）。
//
// **上限が無いと止まらない。**書き戻した直後にボードの自動化がまた動く組み合わせがあると、
// continuo とボードが同じ issue を押し合い続け、**GitHub への書き込みが巡回のたびに増え続ける。**
// **3回で足りる。**PR を1本作れば自動化は1回動く。CI の直しで PR を作り直しても数回である。
// **上限に達したら、いままでどおり猶予を置いて worker を止める**（押し合いを人間へ渡す）。
const maxAutomatedRewrites = 3

// handleUnknownState は「continuo が知らない Status」になった run をどうするかを決める
// （設計 3-50 / 3-54）。
//
//	書いたのがボードの自動化で、対応表に戻す先がある … **止めない。**本来の Status へ書き戻す
//	turn が動いていて猶予の内側                      … **止めない。**turn の終わりの表明を読んでから判断する
//	turn が動いていない                              … その場で止める（待っても表明は出てこない）
//	猶予を過ぎた                                    … その場で止める（人間が止めたがっている可能性がある）
//
// **`terminal_states` へ動かされた場合はここへ来ない。**そちらは即座に終わる
// （人間が「終わった」と言っているので、待つ意味が無い）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// issue: 取り直した issue。
func (o *Orchestrator) handleUnknownState(ctx context.Context, rs *runState, issue tracker.Issue) {
	if target, ok := o.claimAutomatedRewrite(rs, issue); ok {
		// **ボードの自動化が動かしただけである**（設計 3-54）。人間の引き渡しではないので
		// worker を止めない。**書き戻しは巡回のループを止めないよう別の goroutine で回す。**
		o.rewriteAutomatedStateAsync(ctx, rs, issue, target)
		return
	}

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

	// **誰がその Status を書いたかを添える**（設計 3-54）。自動化が書いていたのなら、
	// 対応表を1行足すだけで次からは止まらなくなる。**それをログから読み取れるようにする。**
	writtenBy := "(取れませんでした)"
	if issue.StatusChangedBy != "" {
		writtenBy = issue.StatusChangedBy
	}
	if grace > 0 && waited >= grace {
		o.logger.Warn("知らない Status のまま猶予を過ぎたので worker を止めます",
			"identifier", issue.Identifier, "状態", issue.State, "猶予の上限", formatDuration(grace),
			"書いたのは", writtenBy, "自動化か", issue.StatusChangedByAutomation)
	} else {
		o.logger.Warn("continuo が知らない Status になったので worker を止めます（worktree は残します）",
			"identifier", issue.Identifier, "状態", issue.State,
			"書いたのは", writtenBy, "自動化か", issue.StatusChangedByAutomation)
	}
	o.stopForUnknownStateAsync(ctx, rs, issue.State)
}

// claimAutomatedRewrite は「知らない Status を書いたのはボードの自動化で、対応表に
// 戻す先がある」ときに、その戻す先を返す（設計 3-54）。
//
// **4つが揃ったときだけ確保できる。**
//
//	書いたのが自動化である   … `actor.__typename` が `Bot`、または `wasAutomated` が真（設計 2-6）
//	対応表に戻す先がある     … `tracker.automated_state_rewrite` のキーに一致する
//	戻す先が知っている Status … 戻した先でまた「知らない Status」になるのを防ぐ
//	書き戻す回数が残っている  … 1つの Status につき maxAutomatedRewrites 回まで
//
// **確保した時点で1回ぶん数える。**書き込みが終わる前に次の巡回が来ても、同じ書き戻しを
// 二重に立てないためである。
//
// rs: 対象の run。
// issue: 取り直した issue。
// 戻り値の1つ目: 戻す先の Status 名。
// 戻り値の2つ目: 書き戻してよければ true。
func (o *Orchestrator) claimAutomatedRewrite(rs *runState, issue tracker.Issue) (string, bool) {
	if !issue.StatusChangedByAutomation {
		return "", false
	}
	target, ok := lookupStateRewrite(o.cfg.Tracker.AutomatedStateRewrite, issue.State)
	if !ok {
		// **対応表に無ければ書き戻さない。**いままでどおり猶予を置いて止める。
		// **足し方は issue のコメントに書く**（unknownStateReason）。ログに毎巡回出すと
		// 同じ行が流れ続けて他の行が埋もれる。
		return "", false
	}
	if !o.isKnownState(target) {
		// 戻した先がまた「知らない Status」なら、押し合いが終わらない。
		// **設定の誤りなので、書き戻さずに人間へ渡す。**
		o.logger.Warn("自動化が動かした Status の戻す先が、continuo の知らない Status です"+
			"（書き戻しません。WORKFLOW.md の tracker.automated_state_rewrite を直してください）",
			"identifier", issue.Identifier, "自動化が書いた Status", issue.State, "戻す先", target)
		return "", false
	}
	claimed, done := rs.claimAutomatedRewrite(issue.State, maxAutomatedRewrites)
	if !claimed {
		o.logger.Warn("自動化が動かした Status を書き戻す回数が上限に達しました"+
			"（continuo とボードの自動化が押し合っています。ここからは人間へ渡します）",
			"identifier", issue.Identifier, "自動化が書いた Status", issue.State,
			"戻す先", target, "書き戻した回数", done, "上限", maxAutomatedRewrites)
		return "", false
	}
	return target, true
}

// rewriteAutomatedStateAsync は書き戻しを別の goroutine で行う（設計 3-54）。
//
// **巡回のループから呼ぶので同期で書きに行かない**（設計 3-8）。`UpdateStatus` は
// 取り直しと書き込みで2往復するので、巡回の間隔をそのぶん食う。
//
// **`ctx` をそのまま渡す**（`context.WithoutCancel` にしない）。止めろと言われたら
// 書かずに終わってよい。**次の巡回が同じ判定でもう一度書きに行く**ので、
// 書き損ねたぶんは自分で直る。**pane を閉じる経路とは事情が違う**（あちらは閉じ損ねると
// 誰も閉じない）。**期限も付けない。**GraphQL の呼び出しはトラッカーの HTTP クライアントが
// 1リクエスト30秒で切るので、この goroutine は放っておいても終わる。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// issue: 取り直した issue。
// target: 戻す先の Status 名。
func (o *Orchestrator) rewriteAutomatedStateAsync(
	ctx context.Context, rs *runState, issue tracker.Issue, target string,
) {
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		o.rewriteAutomatedState(ctx, rs, issue, target)
	}()
}

// rewriteAutomatedState は、ボードの自動化が動かした Status を本来の Status へ戻す
// （設計 3-54）。
//
// **書き戻すのは、ボードを見た人間が読み違えないようにするためである。**止めないだけだと、
// 人間の列（`In Progress`）に continuo が担当中の issue が居座り、列を分けた意味が消える。
//
// **失敗しても run は止めない。**Status の書き込みは失敗しうるので、次の巡回で拾い直す。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// issue: 取り直した issue。
// target: 戻す先の Status 名。
func (o *Orchestrator) rewriteAutomatedState(
	ctx context.Context, rs *runState, issue tracker.Issue, target string,
) {
	by := issue.StatusChangedBy
	if by == "" {
		by = "(ログイン名を取れませんでした)"
	}
	// **`terminal_states` は渡す。**その issue を人間が「終わった」にしていたら、
	// 書き戻しで巻き戻してはならない（`UpdateStatus` の blockedStates）。
	moved, err := o.tracker.UpdateStatus(ctx, issue.ID, target, o.cfg.Tracker.TerminalStates)
	if err != nil {
		o.logger.Warn("自動化が動かした Status を戻せませんでした（次の巡回で拾い直します）",
			"identifier", issue.Identifier, "自動化が書いた Status", issue.State,
			"戻す先", target, "書いたのは", by, "error", err)
		return
	}
	if !moved.Wrote {
		// 既にその値だった・item がもう見えない・終わったとみなす状態だった、のいずれか。
		o.logger.Info("自動化が動かした Status は戻しませんでした（ボードは動いていません）",
			"identifier", issue.Identifier, "自動化が書いた Status", issue.State,
			"戻す先", target, "取り直した状態", moved.Previous)
		return
	}
	o.logger.Info("ボードの自動化が動かした Status を、continuo が意図した Status へ戻しました"+
		"（人間が動かしたものは戻しません）",
		"identifier", issue.Identifier, "何から", moved.Previous, "何へ", target, "書いたのは", by)
	rs.setLastWrittenState(target)
	// **知らない Status だった記録を消す**（設計 3-50）。戻したのだから猶予の起点も捨てる。
	rs.clearUnknownState()
	o.postStatusMove(ctx, issue.Identifier, issueNodeID(issue), newStatusMove(moved, target),
		automatedMoveReason(moved.Previous, by))
}

// automatedMoveReason は、書き戻したときの「なぜ」の文を作る（設計 3-29）。
//
// from: 自動化が書いていた Status 名。
// by: 書いた主体のログイン名。
// 戻り値: 「〜ためです」で終わる1文。
func automatedMoveReason(from, by string) string {
	return fmt.Sprintf(
		"ボードの組み込みの自動化（`%s`）が Status を `%s` へ動かし、"+
			"WORKFLOW.md の `tracker.automated_state_rewrite` に戻す先が書かれているためです",
		by, from)
}

// lookupStateRewrite は対応表から戻す先を引く（設計 3-54）。
//
// **照合は大文字小文字と前後の空白を無視する。**トラッカーの Status の照合が
// そうしているので（SPEC.md 11.3）、ここだけ完全一致で引くと、設定が黙って効かなくなる。
//
// table: `tracker.automated_state_rewrite`。
// state: 自動化が書いた Status 名。
// 戻り値の1つ目: 戻す先の Status 名（設定に書かれた綴りのまま）。
// 戻り値の2つ目: 対応表にあれば true。
func lookupStateRewrite(table map[string]string, state string) (string, bool) {
	folded := strings.ToLower(strings.TrimSpace(state))
	for from, to := range table {
		if strings.ToLower(strings.TrimSpace(from)) == folded {
			return to, true
		}
	}
	return "", false
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
			"%s%s",
		moved,
		strings.Join(o.knownStates(), " / "),
		state,
		back,
		state,
		grace,
		o.automatedStateHint(rs, state))
}

// automatedStateHint は「その Status を書いたのはボードの自動化だった」ことと、
// 次から止まらなくする1行を、issue のコメントへ足す文を作る（設計 3-54）。
//
// **人間が動かしたときは何も足さない。**その場合は止まったことが正しい振る舞いであり、
// 設定を足す話ではない。
//
// **書いたのが自動化なのに対応表に無い、という場合だけが本題である。**
// PR を作った・PR がマージされた、といった操作でボードの組み込みの自動化が動くことは、
// 設定の既定のまま起きる（設計 2-6）。**足す1行をそのまま書いて見せる。**
//
// rs: 対象の run（取り直した issue を持っている）。
// state: 動かされた先の Status 名。
// 戻り値: 足す文。足すものが無ければ空文字。
func (o *Orchestrator) automatedStateHint(rs *runState, state string) string {
	issue := rs.issue()
	if !issue.StatusChangedByAutomation {
		return ""
	}
	by := issue.StatusChangedBy
	if by == "" {
		by = "ボードの自動化"
	}
	back := rs.lastWrittenState()
	if back == "" {
		back = o.cfg.Tracker.RunningState
	}
	return fmt.Sprintf(
		"\n【この Status を書いたのは人間ではありません】`%s` が書いています"+
			"（ボードの組み込みの自動化です。PR を issue に紐づけた・PR をマージした、"+
			"といった操作で動きます）。"+
			"**次からは continuo に戻させることができます。**WORKFLOW.md の `tracker:` の下へ"+
			"次の3行を足して、continuo を再起動してください。"+
			"\n```yaml\n  automated_state_rewrite:\n    %q: %q\n```",
		by, state, back)
}
