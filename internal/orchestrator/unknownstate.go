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
// 戻り値: `New` で1度だけ計算した一覧。**呼び出し側は書き換えてはならない。**
func (o *Orchestrator) knownStates() []string {
	return o.knownStateNames
}

// isKnownState は continuo がその Status の意味を知っているかを返す（設計 3-50）。
//
// state: 判定する Status 名。
// 戻り値: 設定に名前が出てくる Status なら true。
func (o *Orchestrator) isKnownState(state string) bool {
	return containsFold(o.knownStateNames, state)
}

// maxAutomatedRewrites は、1つの run が1つの Status について書き戻してよい回数である
// （設計 3-56）。
//
// **上限が無いと止まらない。**書き戻した直後にボードの自動化がまた動く組み合わせがあると、
// continuo とボードが同じ issue を押し合い続け、**GitHub への書き込みが巡回のたびに増え続ける。**
// **3回で足りる。**PR を1本作れば自動化は1回動く。CI の直しで PR を作り直しても数回である。
// **上限に達したら、いままでどおり猶予を置いて worker を止める**（押し合いを人間へ渡す）。
const maxAutomatedRewrites = 3

// maxAutomatedRewriteFailures は、書き戻しが「ボードを1ミリも動かせないまま終わる」ことを
// 続けて何回まで許すかである（設計 3-56）。
//
// **押し合いの上限（`maxAutomatedRewrites`）とは別に数える。**ボードが動かなかったぶんは
// 押し合いの枠を返すので、**返すだけでは上限に永久に届かない。**猶予の時計も、その手前で
// 戻ってしまうので始まらない。**結果、毎回失敗する書き込みを30秒ごとに永久に打ち続け、
// worker は止まらず、人間にも渡らない。**
//
// **これが起きるのは、人間がボードから戻す先の選択肢を消したときである。**
// 起動時の照合（`requiredStatesForBootstrap`）は通っていたのだから、設定は正しかった。
// **走っている最中にボードが変わったのであり、continuo は自力では直せない。**
//
// **3回で足りる。**通信の失敗なら次の巡回で直る。3回続くなら相手側の事情である。
// **待っても直らないと分かっている失敗は、1回で人間へ渡す**（`rewriteAutomatedState` が
// `tracker.CategoryInvalidConfig` にこの値をそのまま足す）。
const maxAutomatedRewriteFailures = 3

// automatedRewriteFailureRetryAfter は、「戻せない」の上限に達してから、もう一度だけ
// 試すまでの間隔である（設計 3-56）。
//
// **数えているのは「続けて何回」である。**成功で 0 に戻す道はあるが、
// **上限に達した run は書き戻しそのものをやめるので、その道へは二度と入れない。**
// 切り直さないと、**通信が回復しても永久に拒み続ける。**
//
// **5分にする。**巡回は30秒ごとなので、打ち直す間隔が10分の1に落ちる
// （「失敗する書き込みを30秒ごとに永久に打ち続ける」は起きない）。
// **知らない Status の猶予の既定（10分）より短くしてある。**猶予より長いと、
// 待っているあいだに一度も試し直せず、この切り直しが死んだ枝になる。
const automatedRewriteFailureRetryAfter = 5 * time.Minute

// handleUnknownState は「continuo が知らない Status」になった run をどうするかを決める
// （設計 3-50 / 3-54）。
//
//	書いたのがボードの自動化で、対応表に戻す先がある … **止めない。**本来の Status へ書き戻す
//	turn が動いていて猶予の内側                      … **止めない。**turn の終わりの表明を読んでから判断する
//	turn が動いていない                              … その場で止める（待っても表明は出てこない）
//	猶予を過ぎた                                    … その場で止める（人間が止めたがっている可能性がある）
//
// **`terminal_states` と引き渡しの Status へ動かされた場合はここへ来ない。**
// そちらは `holdForAutomatedMove` が同じ猶予を掛ける（設計 3-73）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// issue: 取り直した issue。
func (o *Orchestrator) handleUnknownState(ctx context.Context, rs *runState, issue tracker.Issue) {
	if target, claim, ok := o.claimAutomatedRewrite(rs, issue); ok {
		// **ボードの自動化が動かしただけである**（設計 3-54）。人間の引き渡しではないので
		// worker を止めない。**書き戻しは巡回のループを止めないよう別の goroutine で回す。**
		o.rewriteAutomatedStateAsync(ctx, rs, issue, target, claim)
		return
	}

	now := o.now()
	since := rs.noteExternalMove(now, externalMoveUnknownState)
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
		// **片付けるかどうかは `cleanup.enabled` と `cleanup.on_states` の両方で決まる**
		// （設計 3-57b）。`cleanup.enabled` が偽なら `Manager.Cleanup` が
		// 何もせずに戻るので、`cleanup.on_states` に入っていても worktree は残る。
		kept := "（worktree は残します）"
		if o.willCleanupState(issue.State) {
			kept = "（cleanup.on_states の Status なので worktree は片付けます）"
		}
		o.logger.Warn("continuo が知らない Status になったので worker を止めます"+kept,
			"identifier", issue.Identifier, "状態", issue.State,
			"書いたのは", writtenBy, "自動化か", issue.StatusChangedByAutomation)
	}
	o.stopForUnknownStateAsync(ctx, rs, issue.State)
}

// holdForAutomatedMove は、終端の Status と引き渡しの Status へ動かされた run を、
// この巡回では止めずに turn の終わりまで待つかどうかを決める（設計 3-73）。
//
// **見るのは「誰が書いたか」と「turn が動いているか」の2つだけである。**
//
//	人間が書いた             … **待たない。**人間は自分で動かした結果を分かっている
//	自動化が書き、turn が動いていて猶予の内側 … **待つ。**turn の終わりまで止めない
//	自動化が書いたが turn が動いていない       … 待たない（待っても turn は終わらない）
//	猶予（`tracker.unknown_state_grace_ms`）を過ぎた … 待たない
//
// **`active_states` のままの run へは呼ばない。**呼び出し側（`reconcileRunning`）が
// 別の分岐で受ける。`active_states` に入ったまま止まる run は「Status を引き渡された」の
// ではなく `Dispatchable` が偽になった run であり（リポジトリの信頼登録が外れた等）、
// **Status を誰が書いたかは、止める理由と何の関係も無い。**
//
// **待つ理由は、走っている Claude Code を continuo 自身が殺さないためである。**
// エージェントが turn の途中で自分の PR をマージすると、ボードの組み込みの自動化が
// `Done` を書く。**次の巡回はそれを「終わった」と読み、走っている turn ごと片付けにいく。**
// 書いたのが人間でないと分かっているのだから、知らない Status と同じく猶予を置く（3-50）。
//
// **書き戻しの対応表（`tracker.automated_state_rewrite`）はここでは引かない。**
// 対応表のキーは「設定のどこにも名前が出てこない Status」でなければならず
// （`validateAutomatedStateRewrite`。設計 3-55）、**終端も引き渡しも設定に名前が出てくる。**
// **引ける行は1つも作れないので、引く経路を持たせない。**
//
// **猶予の起点は知らない Status と同じ場所に持つ**（`runState.externalMoveSince`）。
// **ただし種類が変わったら起点を切り直す**（`externalMoveAutomatedHandoff`）。
// 知らない Status で待っていた run が続けて自動化に動かされることがあり、
// 起点を繰り越すと、そこから測る猶予が前回ぶんだけ短くなる。
//
// rs: 対象の run。
// issue: 取り直した issue。
// 戻り値: この巡回では止めないなら true。
func (o *Orchestrator) holdForAutomatedMove(rs *runState, issue tracker.Issue) bool {
	if !issue.StatusChangedByAutomation {
		// **人間が動かした。**引き渡しも終端も、そのまま受け取るのが正しい振る舞いである。
		return false
	}
	grace := time.Duration(o.cfg.Tracker.UnknownStateGraceMs) * time.Millisecond
	if grace <= 0 {
		// **猶予を 0 にしてある。**その場で止めると決めた設定なので、待たない。
		return false
	}
	now := o.now()
	since := rs.noteExternalMove(now, externalMoveAutomatedHandoff)
	waited := now.Sub(since)
	if waited >= grace || !rs.turnLoopActive() {
		return false
	}
	writtenBy := issue.StatusChangedBy
	if writtenBy == "" {
		writtenBy = "ボードの自動化"
	}
	// **黙って遅らせない。**人間がこのあと自分で止めたくなったとき、
	// 何を待っているのかがログから読める状態にしておく。
	o.logger.Info("ボードの自動化が Status を動かしましたが turn の終わりを待っています"+
		"（走っている Claude Code をここで止めると、書きかけの turn が捨てられます）",
		"identifier", issue.Identifier,
		"状態", issue.State,
		"書いたのは", writtenBy,
		"待っている時間", formatDuration(waited),
		"猶予の上限", formatDuration(grace))
	return true
}

// claimAutomatedRewrite は「知らない Status を書いたのはボードの自動化で、対応表に
// 戻す先がある」ときに、その戻す先を返す（設計 3-54）。
//
// **4つが揃ったときだけ確保できる。**
//
//	書いたのが自動化である  … `actor.__typename` が `Bot`、または `wasAutomated` が真（設計 2-6）
//	対応表に戻す先がある    … `tracker.automated_state_rewrite` のキーに一致する
//	書き戻す回数が残っている … 1つの Status につき maxAutomatedRewrites 回まで
//	戻せない失敗が続いていない … 1つの Status につき maxAutomatedRewriteFailures 回まで
//
// **戻す先が使える Status かどうかは、ここでは見ない。**設定の検査が起動前に
// 「戻す先は `tracker.active_states` に入っていること」を要求しており（設計 3-55）、
// **ここへ来た時点でその検査は通っている。**
//
// **確保した時点で1回ぶん数える。**書き込みが終わる前に次の巡回が来ても、同じ書き戻しを
// 二重に立てないためである。
//
// **確保できなかったことをログに出すのは、その Status について最初の1回だけである**
// （`noteAutomatedRewriteHandoff`）。**巡回は30秒ごとに同じ判定へ来る**ので、毎回出すと
// 猶予のあいだに同じ行が20回ほど流れ、他の行が埋もれる。**対応表に無かったときの分岐が
// 1行も出さないのと同じ扱いにする。**
//
// rs: 対象の run。
// issue: 取り直した issue。
// 戻り値の1つ目: 戻す先の Status 名。
// 戻り値の2つ目: 確保した枠（呼び出し側は、ボードが動かなかったら必ず `release` する）。
// 戻り値の3つ目: 書き戻してよければ true。
func (o *Orchestrator) claimAutomatedRewrite(
	rs *runState, issue tracker.Issue,
) (string, *rewriteClaim, bool) {
	if !issue.StatusChangedByAutomation {
		return "", nil, false
	}
	target, ok := lookupStateRewrite(o.cfg.Tracker.AutomatedStateRewrite, issue.State)
	if !ok {
		// **対応表に無ければ書き戻さない。**いままでどおり猶予を置いて止める。
		// **足し方は issue のコメントに書く**（unknownStateReason）。ログに毎巡回出すと
		// 同じ行が流れ続けて他の行が埋もれる。
		return "", nil, false
	}
	// **「戻せない」が続いたら、そこで書き戻すのをやめる**（設計 3-54）。
	// やめないと猶予の時計が始まらず、失敗する書き込みを永久に打ち続ける。
	failed := rs.automatedRewriteFailureCount(issue.State)
	if failed >= maxAutomatedRewriteFailures &&
		rs.expireAutomatedRewriteFailures(issue.State, o.now(), automatedRewriteFailureRetryAfter) {
		// **「続けて何回」を時間で切り直す**（設計 3-56）。通信が回復していれば、ここで通る。
		o.logger.Info("自動化が動かした Status の書き戻しを、時間を置いてもう一度試します",
			"identifier", issue.Identifier, "自動化が書いた Status", issue.State,
			"戻す先", target, "置いた時間", formatDuration(automatedRewriteFailureRetryAfter))
		failed = 0
	}
	if failed >= maxAutomatedRewriteFailures {
		if rs.noteAutomatedRewriteHandoff(issue.State, handoffByFailures) {
			o.logger.Warn("自動化が動かした Status を戻せない状態が続いたので、ここからは人間へ渡します"+
				"（ボードから戻す先の選択肢が消えている可能性があります）",
				"identifier", issue.Identifier, "自動化が書いた Status", issue.State,
				"戻す先", target, "戻せなかった回数", failed, "上限", maxAutomatedRewriteFailures)
		}
		return "", nil, false
	}
	claim, done := rs.claimAutomatedRewrite(issue.State, maxAutomatedRewrites)
	if claim == nil {
		if rs.noteAutomatedRewriteHandoff(issue.State, handoffByPushback) {
			o.logger.Warn("自動化が動かした Status を書き戻す回数が上限に達しました"+
				"（continuo とボードの自動化が押し合っています。ここからは人間へ渡します）",
				"identifier", issue.Identifier, "自動化が書いた Status", issue.State,
				"戻す先", target, "書き戻した回数", done, "上限", maxAutomatedRewrites)
		}
		return "", nil, false
	}
	return target, claim, true
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
// **手放した run へは書かない**（`stopForUnknownStateAsync` と同じ守り）。
// この goroutine が飛んでいる間に turn ループが失敗して run を手放すと、
// **印が消えたあとに「作業中」の Status がボードへ書かれる。**次の巡回はその issue を
// 候補として拾い直し、**同じ worktree に2本目の Claude Code を立てる。**
// **印を確保できなければ、書かずに戻る。**確保できたぶんは、書き終えたら必ず返す
// （`endRewrite`）。返さないと、終わらせる処理が書き戻しの終わりを永久に待つ。
//
// **取るのは書き戻し専用の印である**（`beginRewrite`。設計 3-56）。
// 「終わらせる処理」の印を使い回すと、**書き戻しが飛んでいるだけの run が
// 「終わりに向かっている」と読まれ、turn ループが宙に浮く。**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// issue: 取り直した issue。
// target: 戻す先の Status 名。
// claim: 確保した書き戻しの枠。
func (o *Orchestrator) rewriteAutomatedStateAsync(
	ctx context.Context, rs *runState, issue tracker.Issue, target string, claim *rewriteClaim,
) {
	if gate := rs.beginRewrite(); gate != rewriteClaimed {
		// 終わらせる処理が走っている（run が終わっている）か、別の書き戻しが飛んでいる。
		// **確保した書き戻しの枠を返す。**返さないと、止まらなかった run が
		// この issue について1回ぶん損をしたまま次の巡回へ進む。
		claim.release()
		reason := "この run は既に終わりに向かっています"
		if gate == rewriteBusy {
			reason = "別の書き戻しが飛んでいます"
		}
		o.logger.Info("自動化が動かした Status を戻しませんでした（"+reason+"）",
			"identifier", issue.Identifier, "自動化が書いた Status", issue.State, "戻す先", target)
		return
	}
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		// **run はこのあとも続く。**印を返さないと、終わらせる処理が
		// 書き戻しの終わりを待ったまま返らなくなる。
		defer rs.endRewrite()
		// **書き込みの結果は使わない。**巡回から来た経路には「このあとどうするか」が無く、
		// 次の巡回が取り直した Status で判断し直す。**turn ループから来た経路だけが
		// 結果を使う**（`rewriteAndDecide`）。
		_, _ = o.rewriteAutomatedState(ctx, rs, issue, target, claim)
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
// **ボードが動かなかったら、確保した書き戻しの枠を返す**（設計 3-56）。
// 枠は「continuo とボードの自動化が Status を押し合っている」ことを数えるためにある。
// **押し合いは、ボードが実際に動いたときにだけ起きる。**通信の失敗や
// 「既にその値だった」で枠を食い潰すと、**3回の失敗で上限に達し、押し合いが
// 1度も起きていない run が人間へ渡されて worker が止まる。**
//
// **そのかわり、「戻せなかった」は別に数える**（`noteAutomatedRewriteFailure`。設計 3-56）。
// 枠を返すだけだと、**毎回失敗する書き込みを30秒ごとに永久に打ち続ける**
// （猶予の時計はその手前で戻ってしまうので始まらない）。
// **`tracker.CategoryInvalidConfig` は上限をそのまま足して1回で人間へ渡す。**
// 「戻す先の選択肢がボードに無い」がこれであり、待っても直らない（設計 3-56）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// issue: 取り直した issue。
// target: 戻す先の Status 名。
// claim: 確保した書き戻しの枠。**ボードが動かなかったら、この関数が返す。**
// 戻り値の1つ目: `UpdateStatus` の結果。**`Previous` は書きに行く直前のボードの値である**
// （書いたあとに読み直しはしない）。`Reached` が偽なら、ボードは `target` になっていない。
// 戻り値の2つ目: 書き込みそのものが失敗したときのエラー。
func (o *Orchestrator) rewriteAutomatedState(
	ctx context.Context, rs *runState, issue tracker.Issue, target string, claim *rewriteClaim,
) (tracker.StatusWrite, error) {
	by := issue.StatusChangedBy
	if by == "" {
		by = "(ログイン名を取れませんでした)"
	}
	// **`terminal_states` は渡す。**その issue を人間が「終わった」にしていたら、
	// 書き戻しで巻き戻してはならない（`UpdateStatus` の blockedStates）。
	moved, err := o.tracker.UpdateStatus(ctx, issue.ID, target, o.cfg.Tracker.TerminalStates)
	if err != nil {
		// **枠を返す。**ボードは動いていない（押し合いは起きていない）。
		claim.release()
		// **待っても直らない失敗は、1回で人間へ渡す。**戻す先の選択肢がボードから
		// 消えていると、次の巡回も同じところで落ちる。
		add := 1
		if tracker.IsCategory(err, tracker.CategoryInvalidConfig) {
			add = maxAutomatedRewriteFailures
		}
		failed := rs.noteAutomatedRewriteFailure(issue.State, add, o.now())
		o.logger.Warn("自動化が動かした Status を戻せませんでした（次の巡回で拾い直します）",
			"identifier", issue.Identifier, "自動化が書いた Status", issue.State,
			"戻す先", target, "書いたのは", by,
			"戻せなかった回数", failed, "上限", maxAutomatedRewriteFailures, "error", err)
		return moved, err
	}
	if !moved.Wrote {
		// 既にその値だった・item がもう見えない・終わったとみなす状態だった、のいずれか。
		// **どれもボードは動いていないので、枠を返す。**
		claim.release()
		o.logger.Info("自動化が動かした Status は戻しませんでした（ボードは動いていません）",
			"identifier", issue.Identifier, "自動化が書いた Status", issue.State,
			"戻す先", target, "取り直した状態", moved.Previous)
		if !moved.Reached {
			if moved.Previous != "" {
				// **人間が `terminal_states` へ動かしていたので断られた。**
				// **これは「戻せなかった」に数えない。**数えると、
				// 「ボードから戻す先の選択肢が消えている」という的外れな案内が人間へ出る
				// （`automatedStateHint` はその文面を出す前提でこの経路を除いている）。
				// **終わった issue は、このあと終端の判定が拾う**（`rewriteAndDecide`）。
				return moved, nil
			}
			// **item がもう見えない。**これは「戻せなかった」として数える。
			// 数えないと、見えない issue へ永久に書きに行き続ける。
			rs.noteAutomatedRewriteFailure(issue.State, 1, o.now())
			return moved, nil
		}
		// 既にその値だった。**ボードは目的の Status である。**
		// **「最後に書いた値」を揃える。**揃えないと、あとで人間へ渡すときに
		// 書いていない値を「continuo が最後に書いた値」として名指しする（`unknownStateReason`）。
		rs.clearAutomatedRewriteFailures(issue.State)
		rs.setLastWrittenState(target)
		rs.clearExternalMove()
		return moved, nil
	}
	rs.clearAutomatedRewriteFailures(issue.State)
	o.logger.Info("ボードの自動化が動かした Status を、continuo が意図した Status へ戻しました"+
		"（人間が動かしたものは戻しません）",
		"identifier", issue.Identifier, "何から", moved.Previous, "何へ", target, "書いたのは", by)
	rs.setLastWrittenState(target)
	// **知らない Status だった記録を消す**（設計 3-50）。戻したのだから猶予の起点も捨てる。
	rs.clearExternalMove()
	o.postStatusMove(ctx, issue.Identifier, issueNodeID(issue), newStatusMove(moved, target),
		automatedMoveReason(moved.Previous, by))
	return moved, nil
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
// **比べ方はこのリポジトリの他の箇所と同じ `strings.EqualFold` に揃える**
// （`containsFold` / `containsStateFold` / `containsFoldedStatus`）。
//
// **大文字小文字だけが違うキーが2つ並ぶことはない。**`config.Validate` が起動前に弾く
// （どちらに当たるかが map の反復順で決まってしまうため。設計 3-54）。
//
// table: `tracker.automated_state_rewrite`。
// state: 自動化が書いた Status 名。
// 戻り値の1つ目: 戻す先の Status 名（設定に書かれた綴りのまま）。
// 戻り値の2つ目: 対応表にあれば true。
func lookupStateRewrite(table map[string]string, state string) (string, bool) {
	target := strings.TrimSpace(state)
	for from, to := range table {
		if strings.EqualFold(strings.TrimSpace(from), target) {
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
// **書き戻しが飛んでいたら、この巡回では止めない**（次の巡回でやり直す）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// state: 動かされた先の Status 名。
func (o *Orchestrator) stopForUnknownStateAsync(ctx context.Context, rs *runState, state string) {
	if rs.beginTerminal() != terminalClaimed {
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
// **turn ループから呼ぶので、書き戻しが飛んでいたら終わるまで待つ**（`claimTerminal`）。
// 待たずに戻ると、この run を終わらせる者が誰も居なくなる。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// state: 動かされた先の Status 名。
func (o *Orchestrator) finishRunUnknownState(ctx context.Context, rs *runState, state string) {
	if !rs.claimTerminal(ctx) {
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
// **設定の足し方の案内は、必ず1つだけにする**（設計 3-57b）。
// 「`active_states` に足せ」と「`automated_state_rewrite` に足せ」を並べて出すと、
// **両方やった設定は起動しない。**`automated_state_rewrite` のキーは
// 「設定のどこにも名前が出てこない Status」でなければならず、`active_states` へ
// 足した時点で `config.Validate` がその行を弾くためである。
//
// **判定は「対応表に既に書いてある名前か」で行う。**「書き戻しの案内を出したか」で
// 判定すると、**対応表に書いてある Status で止まった道を1本も塞げない**（そこでは
// 案内を出さないので偽になる）。塞ぎ損ねる道は3本ある。
//
//	書き戻す回数が上限に達した … `automatedStateHint` の押し合いの分岐
//	戻せない失敗が続いた       … 同じく、戻す先がボードから消えたときの分岐
//	人間がそのキーの Status へ動かした … `automatedStateHint` は人間には何も返さない
//
// **`cleanup.on_states` に書いてある Status でも同じことが起きる**（設計 3-57b。issue #76）。
// あちらへ書いた名前を `tracker.active_states` へ足すと、`config.Validate` が
// 「作業中の worktree を片付けてしまう」として弾く（設計 3-9）。
// **だから、そこへ足せとは言わない。****貼っても起動する直し方だけを出す。**
//
// **両方に名前がある設定には、専用の分岐を置く**（設計 3-57b）。
// 対応表の案内だけでは足りず、`cleanup.on_states` の案内だけでも足りない。
//
//	対応表の行を消して `active_states` へ足す … `cleanup.on_states` が残るので弾かれる
//	`terminal_states` へ足す                 … 対応表のキーの検査に落ちる
//
// **消す順番と、消す先が2つあることを1つの文で書き切る。**
//
// **片付けるかどうかは `cleanup.enabled` と `cleanup.on_states` の両方で決まる**
// （設計 3-57b）。**両方が揃ったときだけ continuo はこの worktree を片付ける**
// （`finishRunClaimed` が `ShouldCleanup` で消し、猶予 0 の道でも次の巡回の
// `reconcileWorktrees` が消す）。**残ると書いたパスが消えるのも、消えると書いたパスが
// 残るのも、どちらも案内として成り立たない。**
//
// **見送りの条件も設定で切れる。**「コミットしていない変更が残っていれば片付けない」は
// `cleanup.require_clean_worktree` が真のときだけ、「push していない commit が残って
// いれば片付けない」は `cleanup.require_pushed` が真のときだけの話である
// （`internal/workspace/cleanup.go` の `leftoverReasons`）。**偽にしてある設定へ
// その一文を出すと、消えないと読める worktree が消える。**
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
	// **貼ると起動しなくなる案内を出さない**（設計 3-57b）。
	// 対応表に既に書いてある Status なら、`active_states` へ足す案内の代わりに
	// **「先に対応表のその行を消す」を出す。**それが唯一、貼っても起動する直し方である。
	hint, proposesRewrite := o.automatedStateHint(rs, state)
	rewriteTarget, inRewriteTable := lookupStateRewrite(o.cfg.Tracker.AutomatedStateRewrite, state)
	inCleanup := containsFold(o.cfg.Cleanup.OnStates, state)
	teach := fmt.Sprintf(
		"\n【`%s` も continuo に扱わせたいときは】WORKFLOW.md の `tracker.active_states` か "+
			"`tracker.status_signal_map` にその名前を書き足してから、continuo を再起動してください。",
		state)
	switch {
	case proposesRewrite:
		// 対応表へ1行足す案内を出した。**この Status はまだ対応表に無い。**
		teach = ""
	case inRewriteTable && inCleanup:
		// **名前が2箇所にある。**片方だけ消しても起動しないので、両方の消し方を1つの文で出す。
		teach = fmt.Sprintf(
			"\n【`%s` も continuo に扱わせたいときは】この名前は WORKFLOW.md の2箇所にあります。"+
				"`tracker.automated_state_rewrite` のキー（`%s` → `%s`）と、"+
				"`cleanup.on_states`（worktree の片付けを始める Status）です。"+
				"**まず `tracker.automated_state_rewrite` からその行を消してください。**"+
				"消さずに `tracker` の他のキーへ書き足した設定では continuo は起動しません"+
				"（キーは設定の他のどこにも名前が出てこない Status でなければなりません）。"+
				"**そのうえで、終わったとみなしてよい Status なら `tracker.terminal_states` に書き足してください**"+
				"（`cleanup.on_states` は、この一覧の中から選ぶ決まりです）。"+
				"**`%s` でも作業を続けさせたいなら、`cleanup.on_states` からもその行を消してから、"+
				"`tracker.active_states` か `tracker.status_signal_map` へ書き足してください。**"+
				"**`cleanup.on_states` に残したまま `tracker.active_states` へ書き足すと、"+
				"やはり continuo は起動しません**（走っている worktree を片付けてしまうので、設定の検査が弾きます）。"+
				"**ボードの自動化をやめて `%s` を使わなくなったのなら、対応表からその行を消すだけで構いません。**",
			state, state, rewriteTarget, state, state)
	case inRewriteTable:
		teach = fmt.Sprintf(
			"\n【`%s` も continuo に扱わせたいときは】この名前は WORKFLOW.md の "+
				"`tracker.automated_state_rewrite` のキー（`%s` → `%s`）です。"+
				"**`tracker.active_states` や `tracker.status_signal_map` へ書き足す前に、"+
				"対応表のその行を消してください。**両方に書いた設定では continuo は起動しません"+
				"（キーは設定の他のどこにも名前が出てこない Status でなければなりません）。"+
				"**ボードの自動化をやめて `%s` を使わなくなったのなら、対応表からその行を消すだけで構いません。**",
			state, state, rewriteTarget, state)
	case inCleanup:
		teach = fmt.Sprintf(
			"\n【`%s` も continuo に扱わせたいときは】この名前は WORKFLOW.md の "+
				"`cleanup.on_states`（worktree の片付けを始める Status）に書いてあります。"+
				"**`tracker.active_states` へ書き足すと continuo は起動しません**"+
				"（走っている worktree を片付けてしまうので、設定の検査が弾きます）。"+
				"**終わったとみなしてよい Status なら、`tracker.terminal_states` に書き足してください**"+
				"（`cleanup.on_states` は、この一覧の中から選ぶ決まりです）。"+
				"**`%s` でも作業を続けさせたいなら、先に `cleanup.on_states` からその行を消してから、"+
				"`tracker.active_states` か `tracker.status_signal_map` へ書き足してください。**"+
				"どちらの直し方も、そのまま設定へ書いて continuo が起動します。",
			state, state)
	}
	// **片付ける設定でだけ「残りません」と書く。**continuo が worktree と branch を片付けるのは
	// `cleanup.enabled` が真で、かつ Status が `cleanup.on_states` にあるときだけである
	// （`finishRunClaimed` の `ShouldCleanup`、および次の巡回の `reconcileWorktrees`）。
	kept := "worktree は残してあります（下記）。"
	switch {
	case o.willCleanupState(state):
		kept = fmt.Sprintf(
			"**worktree は残りません。**`%s` は `cleanup.on_states`（worktree の片付けを始める Status）"+
				"なので、continuo はこの worktree と branch を片付けます（下記のパスは消えます）。"+
				"%s"+
				"**残したいなら、片付く前に Status を戻すか、`cleanup.on_states` からその行を消してください。**",
			state, o.cleanupGuardSentence())
	case inCleanup:
		// **`cleanup.on_states` にはあるが `cleanup.enabled` が偽である。**片付けは走らない。
		// 何も書かずに既定の一文だけを出すと、設定を直したときに何が起きるかが読めない。
		kept = fmt.Sprintf(
			"worktree は残してあります（下記）。`%s` は `cleanup.on_states`"+
				"（worktree の片付けを始める Status）に書いてありますが、"+
				"`cleanup.enabled` が false なので continuo は片付けを行いません。",
			state)
	}
	return fmt.Sprintf(
		"continuo が知らない Status になったので、この issue の作業を止めました。\n%s"+
			"\n【なぜ止めたか】continuo は WORKFLOW.md に書かれた Status しか扱いません"+
			"（いま知っているのは %s です）。`%s` はそのどれでもないので、"+
			"この issue をどう進めればよいかを判断できません。"+
			"\n【続けるには】Status を `tracker.active_states` に入っている Status（%s）のいずれかへ戻してください。"+
			"次の巡回で着手し直します。%s"+
			"%s%s%s",
		moved,
		strings.Join(o.knownStates(), " / "),
		state,
		back,
		kept,
		teach,
		grace,
		hint)
}

// automatedStateHint は「その Status を書いたのはボードの自動化だった」ことと、
// 次から止まらなくする1行を、issue のコメントへ足す文を作る（設計 3-54）。
//
// **人間が動かしたときは何も足さない。**その場合は止まったことが正しい振る舞いであり、
// 設定を足す話ではない。
//
// **書いたのが自動化なのに対応表に無い、という場合が本題である。**
// PR を作った・PR がマージされた、といった操作でボードの組み込みの自動化が動くことは、
// 設定の既定のまま起きる（設計 2-6）。**足す2行をそのまま書いて見せる。**
//
// **既に対応表にある Status なら、足せとは言わない。**同じ行をもう一度足させても直らない。
// **この案内に来る道は2本しか無い**（どちらも `claimAutomatedRewrite` で枠を取れなかった道である）。
//
//	書き戻す回数が上限に達した … continuo とボードの自動化が押し合っている
//	戻せない失敗が続いた       … 戻す先の選択肢がボードから消えている可能性が高い
//
// **書き込みが1〜2回失敗しただけでは、ここへ来ない。**その場合は run が続き、
// 次の巡回が同じ判定でもう一度書きに行く。**`terminal_states` に入っていて断られた場合も
// ここへ来ない**（枠を返して run が続き、「戻せなかった」にも数えない。
// `rewriteAutomatedState`）。**だから、どちらが起きたのかを数えて書き分ける。**
//
// **提案する戻す先は `active_states` に入っているものに限る**（設計 3-55）。
// 設定の検査が「戻す先は `active_states` に入っていること」を要求しているので、
// **入っていない値を提案すると、貼った利用者の continuo が起動しなくなる。**
// 最後に書いた値は `In Review` のような引き渡しの Status のこともあるので、そのまま使えない。
//
// rs: 対象の run（取り直した issue を持っている）。
// state: 動かされた先の Status 名。
// 戻り値の1つ目: 足す文。足すものが無ければ空文字。
// 戻り値の2つ目: `automated_state_rewrite` へ1行足す案内を出したなら true
// （呼び出し側は、そのとき `active_states` へ足す案内を出さない）。
// **既に対応表にある Status のときは偽である。**その場合に `active_states` の案内を
// 抑えるかどうかは、**呼び出し側が対応表を自分で引いて決める**（`unknownStateReason`）。
// ここで決めさせると、人間が動かして早々に戻る道（この関数の1つ目の分岐）を塞げない。
func (o *Orchestrator) automatedStateHint(rs *runState, state string) (string, bool) {
	issue := rs.issue()
	if !issue.StatusChangedByAutomation {
		return "", false
	}
	by := issue.StatusChangedBy
	if by == "" {
		by = "ボードの自動化"
	}
	written := fmt.Sprintf(
		"\n【この Status を書いたのは人間ではありません】`%s` が書いています"+
			"（ボードの組み込みの自動化です。PR を issue に紐づけた・PR をマージした、"+
			"といった操作で動きます）。", by)

	if target, ok := lookupStateRewrite(o.cfg.Tracker.AutomatedStateRewrite, state); ok {
		written += fmt.Sprintf(
			"**この Status は WORKFLOW.md の `tracker.automated_state_rewrite` に"+
				"（`%s` → `%s` として）既に書かれています。**それでも止まったので、"+
				"足りないのは設定の1行ではありません。", state, target)
		// **戻せない失敗が続いたのなら、押し合いの話をしてはならない。**
		// 押し合いは1度も起きていないので、`Workflows` を切っても直らない。
		if rs.automatedRewriteFailureCount(state) >= maxAutomatedRewriteFailures {
			return written + fmt.Sprintf(
				"\n【何が起きたか】continuo が `%s` へ戻そうとして、%d 回続けて書き込めませんでした。"+
					"\n【いちばんありそうな原因】ボードの Status の選択肢から `%s` が消えています"+
					"（continuo の起動時には在りました）。"+
					"\n【対処】ボードに `%s` の選択肢を作り直すか、"+
					"`tracker.automated_state_rewrite` の戻す先を実在する Status に直して、"+
					"continuo を再起動してください。",
				target, maxAutomatedRewriteFailures, target, target), false
		}
		return written + fmt.Sprintf(
			"\n【何が起きたか】continuo が `%s` へ戻すたびに自動化が `%s` を書き直していて、"+
				"書き戻す回数が上限（%d 回）に達しました。"+
				"\n【対処】ボードの `Workflows` でこの自動化を切るか、"+
				"`%s` を continuo が使わない Status に変えてください。",
			target, state, maxAutomatedRewrites, state), false
	}

	back := o.rewriteTargetSuggestion(rs)
	if back == "" {
		// **提案できる戻す先が1つも無い**（`active_states` が空）。
		// **書けない設定を見せない。**`active_states` へ足す案内だけが残る。
		return written, false
	}
	return written + fmt.Sprintf(
		"**次からは continuo に戻させることができます。**WORKFLOW.md の `tracker:` の下へ"+
			"次の2行を足して、continuo を再起動してください。"+
			"\n```yaml\n  automated_state_rewrite:\n    %q: %q\n```",
		state, back), true
}

// rewriteTargetSuggestion は、対応表へ提案する「戻す先」を1つ選ぶ（設計 3-55）。
//
// **`tracker.active_states` に入っているものしか選ばない。**設定の検査が
// 「戻す先は `active_states` に入っていること」を要求しているので、
// **外の値を提案すると、貼った利用者の continuo が起動しなくなる。**
//
//	continuo が最後に書いた値  … それが `active_states` にあれば、いちばん自然な戻り先
//	`tracker.running_state`    … 無ければこれ（作業中の Status そのものである）
//	`active_states` の先頭      … それも外れているなら、とにかく入っているものを1つ
//
// rs: 対象の run。
// 戻り値: 提案する Status 名。**提案できるものが無ければ空文字。**
func (o *Orchestrator) rewriteTargetSuggestion(rs *runState) string {
	if last := rs.lastWrittenState(); containsFold(o.cfg.Tracker.ActiveStates, last) {
		return last
	}
	if containsFold(o.cfg.Tracker.ActiveStates, o.cfg.Tracker.RunningState) {
		return o.cfg.Tracker.RunningState
	}
	for _, s := range o.cfg.Tracker.ActiveStates {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// willCleanupState は、その Status で止めたときに continuo が worktree と branch を
// 実際に片付けるかどうかを返す（設計 3-57b。issue #76）。
//
// **`cleanup.on_states` に名前があるだけでは片付かない。**`cleanup.enabled` が偽なら
// `workspace.Manager.Cleanup` は `Deferred` を返して1バイトも消さない。
// **「片付ける Status か」と「片付けるか」を混同すると、案内が逆向きの嘘になる。**
// 同じ判定は `config.CleanupStatesOutsideTerminal` も `cleanup.enabled` から始めている。
//
// **見送りの条件（`require_clean_worktree` / `require_pushed`）はここでは見ない。**
// あれは worktree の中身を git に聞かないと決まらず、案内を組み立てる時点では確かめられない。
// **残りものがあれば見送る、という条件付きの言い方を `cleanupGuardSentence` が添える。**
//
// state: 動かされた先の Status 名。
// 戻り値: 片付けが走るなら true。
func (o *Orchestrator) willCleanupState(state string) bool {
	return o.cfg.Cleanup.Enabled && containsFold(o.cfg.Cleanup.OnStates, state)
}

// cleanupGuardSentence は、片付けを見送る条件を人間へ伝える一文を作る（設計 3-57b）。
//
// **真になっているフラグの分しか書かない。**`cleanup.require_clean_worktree` と
// `cleanup.require_pushed` はどちらも設定で偽にできる（既定は両方 true）。
// **偽にしてある設定へ「残っていれば片付けません」と書くと、消えないと読めた worktree が消える。**
// 対応する実装は `internal/workspace/cleanup.go` の `leftoverReasons` である。
//
// 戻り値: 添える一文。**どちらのフラグも偽なら空文字**（見送る条件が無い）。
func (o *Orchestrator) cleanupGuardSentence() string {
	var left []string
	if o.cfg.Cleanup.RequireCleanWorktree {
		left = append(left, "コミットしていない変更")
	}
	if o.cfg.Cleanup.RequirePushed {
		left = append(left, "push していない commit")
	}
	if len(left) == 0 {
		return ""
	}
	return fmt.Sprintf("**%s が残っている worktree は片付けません。**", strings.Join(left, "か、"))
}
