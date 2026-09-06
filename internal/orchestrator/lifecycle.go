package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// handleTurnEnd は turn が終わったあとの処理をひとまとめに行う（設計 3-5 の図）。
//
//  1. transcript を読んで表明とトークンを取る（設計 3-15 / 3-25。**1回開いて両方を取る**）
//  2. 表明どおりに Status を動かす（グループの他の issue も動かす。設計 3-26）
//  3. Status を ID 指定で取り直し、その値で分岐する
//     terminal_states           … コメントを確かめてから worktree と branch を片付ける
//     active_states             … max_dispatch_turns に未到達なら次の turn、到達なら failure_state
//     知らない Status（自動化が書いた）… 本来の Status へ戻し、**書き込みの結果で判定し直す**（設計 3-54 / 3-56）
//     どちらでもない（引き渡し） … コメントを確かめてから worker を止める。**worktree は消さない**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値: この run が終わったら true（turn ループを止める）。
func (o *Orchestrator) handleTurnEnd(ctx context.Context, rs *runState) bool {
	// **担当が自分でなくなっていないかを、turn の終わりで確かめる**（設計 3-77c）。
	// **確かめるのは `recheck_interval_ms` に1回だけである**（既定1時間）。
	// **移っていたらここで止める。push しない。**
	if lost, newAccount := o.handoffLostOnTurnEnd(ctx, rs); lost {
		o.stopBecauseHandoffLost(ctx, rs, newAccount)
		return true
	}

	signals := o.readSignals(ctx, rs)
	rs.setMissingSignal(len(signals) == 0)
	o.applySignals(ctx, rs, signals)

	// **ここだけは「誰が Status を書いたか」も取る**（設計 3-61）。この写しを `rs.setIssue` で
	// 控え、`decideAfterTurn` が「カンバンの自動化が書いたのか」を判定する。
	current, ok, _ := o.refreshIssue(ctx, rs, true)
	if !ok {
		// 見つからない。continuo は面倒を見ない（設計 3-10 の「いつ手放すか」）。
		o.abandonRun(ctx, rs, "この issue がカンバンから見えなくなりました。"+
			"**turn が終わったので issue を ID 指定で取り直したところ、カンバンから返ってきませんでした。**"+
			"\n【確かめ方】カンバンでこの issue を探してください。archive されているか、"+
			"カンバンから外されているはずです。"+
			"\n【よくある原因】人間がカンバンから外した / issue を archive した。"+
			"\n【対処】続きを進めたいならカンバンへ戻し、Status を着手待ちにしてください。"+
			"worktree は残してあります（下記）。")
		return true
	}
	rs.setIssue(current)
	return o.decideAfterTurn(ctx, rs, current, true)
}

// decideAfterTurn は取り直した Status を見て、turn ループを続けるかどうかを決める
// （設計 3-5 の図）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// current: 取り直した issue。
// mayRewrite: カンバンの自動化が書いた Status を書き戻してよいか。
// **書き戻したあとの判定し直しでは偽で呼ぶ**（同じ turn で二度書きに行かないため）。
// 戻り値: この run が終わったら true（turn ループを止める）。
func (o *Orchestrator) decideAfterTurn(
	ctx context.Context, rs *runState, current tracker.Issue, mayRewrite bool,
) bool {
	switch {
	case containsFold(o.cfg.Tracker.TerminalStates, current.State):
		o.finishRun(ctx, rs, "", fmt.Sprintf("Status が %s になりました", current.State))
		return true
	case containsFold(o.cfg.Tracker.ActiveStates, current.State):
		// 次の turn へ。打ち切りの判定は turnLoop の先頭で行う。
		// **外から動かされていた記録は消す**（設計 3-50 / 3-74）。表明で戻ったのだから、
		// 猶予の起点も捨てる。
		rs.clearExternalMove()
		return false
	case current.State != "" && !o.isKnownState(current.State):
		if mayRewrite {
			if target, claim, ok := o.claimAutomatedRewrite(rs, current); ok {
				return o.rewriteAndDecide(ctx, rs, current, target, claim)
			}
		}
		// **猶予を置いて待った先である**（設計 3-50）。turn の終わりまで待ったが、
		// エージェントは正しい Status への表明を出さなかった。**黙って終えない。**
		o.finishRunUnknownState(ctx, rs, current.State)
		return true
	default:
		// 引き渡し（In Review / Blocked）。**worker は止めるが worktree は残す**（設計 3-5）。
		o.finishRun(ctx, rs, "", fmt.Sprintf("Status が %s になりました（人間へ引き渡します）", current.State))
		return true
	}
}

// rewriteAndDecide は、カンバンの自動化が動かした Status を書き戻し、
// **書き込みの結果が示す Status で「終わりかどうか」を判定し直す**（設計 3-56）。
//
// **戻す先が `terminal_states` になることはない。**`tracker.automated_state_rewrite` の
// 戻す先は `active_states` に入っていることを設定の検査が起動前に要求している
// （`validateAutomatedStateRewrite`。設計 3-55）。**書けたときの行き先は必ず「次の turn へ」である。**
// **`"Done": "AI Done"` のような終端への書き戻しは、そもそも起動しない。**
//
// **`UpdateStatus` は書いたあとに読み直さない。**返る `Previous` は
// **書きに行く直前**のカンバンの値である。だから「書いた直後にさらに動かされた値」は、
// この経路のどこにも現れない。**判定し直すのは、書けなかったときのためである。**
//
//	書けた（Wrote）             … カンバンは target になっている。target は active_states なので次の turn へ
//	既に target だった（Reached）… 同上
//	Previous が返って Reached が偽 … **人間が `terminal_states` へ動かしていた**ので書き戻しを断られた。
//	                              **その値で判定し直す**（終わった issue へ次の指示を送らない）
//	Previous も空              … item がもう見えない。次の巡回が拾い直す
//
// **書き込みのあいだだけ `beginRewrite` で書き戻しの印を取る**（設計 3-56）。
// turn の終わりの処理は表明を読む1秒ほどの待ちと2往復の書き込みを含む。
// **その間に巡回が「人間が動かした」と判断して run を手放すと、印が消えたあとに
// 「作業中」の Status がカンバンへ書かれる。**次の巡回はそれを候補として拾い直し、
// **同じ worktree に2本目の Claude Code を立てる。**
// **書き終えたら必ず印を返す**（`endRewrite`）。返さないと、このあと続く
// `decideAfterTurn` の `finishRun` が書き戻しの終わりを永久に待つ。
//
// **書き戻しの印は「終わらせる処理」の印とは別である。**同じ印にすると、
// **巡回からの書き戻しが飛んでいるだけの run を「終わりに向かっている」と読んで、
// turn ループが宙に浮く**（誰も終わらせていないのに turn ループだけが抜ける）。
//
//	別の書き戻しが飛んでいる（rewriteBusy）  … **turn ループは続ける。**着地する書き込みが Status を直す
//	終わりに向かっている（rewriteEnding）    … turn ループを止める
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// current: 書き戻す前の issue。
// target: 戻す先の Status 名。
// claim: 確保した書き戻しの枠。
// 戻り値: この run が終わったら true（turn ループを止める）。
func (o *Orchestrator) rewriteAndDecide(
	ctx context.Context, rs *runState, current tracker.Issue, target string, claim *rewriteClaim,
) bool {
	switch rs.beginRewrite() {
	case rewriteEnding:
		// 既に終わらせる処理が走っている（または run が終わっている）。
		// **確保した枠を返し、turn ループを止める。**
		claim.release()
		o.logger.Info("自動化が動かした Status を戻しませんでした（この run は既に終わりに向かっています）",
			"identifier", current.Identifier, "自動化が書いた Status", current.State, "戻す先", target)
		return true
	case rewriteBusy:
		// **巡回からの書き戻しが既に飛んでいる。**この run は終わっていないので、
		// **turn ループを止めてはならない。**着地する書き込みが Status を直す。
		claim.release()
		o.logger.Info("自動化が動かした Status は、飛んでいる書き戻しに任せます（turn は続けます）",
			"identifier", current.Identifier, "自動化が書いた Status", current.State, "戻す先", target)
		return false
	}
	// **turn ループの goroutine なので、ここは同期で書きに行ってよい。**
	moved, err := o.rewriteAutomatedState(ctx, rs, current, target, claim)
	// **印はここで返す。**このあとの `decideAfterTurn` は `finishRun` を通り、
	// そこで終わらせる処理の印を取る。持ったままだと、その取得が書き戻しの終わりを待って
	// 返らなくなる。
	rs.endRewrite()

	next := target
	switch {
	case err != nil:
		// **書き込みが失敗しても run は続ける。**失敗したのは continuo であって、
		// 人間が引き渡したわけではない。次の巡回が同じ判定でもう一度書きに行く。
		// **「戻せない」が続いたときは `claimAutomatedRewrite` が枠を渡さなくなり、
		// 猶予の時計が始まって人間へ渡る**（設計 3-56）。
		rs.clearExternalMove()
		return false
	case !moved.Reached && moved.Previous == "":
		// item がもう見えない。次の巡回が取り直して判断する。
		rs.clearExternalMove()
		return false
	case !moved.Reached:
		// **書きに行く直前のカンバンは `terminal_states` に入っていた。**
		// 人間が「終わった」にしたということなので、**その値で判定し直す。**
		next = moved.Previous
	}

	movedIssue := current
	movedIssue.State = next
	if next == target {
		// **書いたのは continuo である。**自動化が書いたという印を残したままにすると、
		// このあと止める経路が「カンバンの自動化が書きました」という的外れな案内を出す。
		movedIssue.StatusChangedBy = ""
		movedIssue.StatusChangedByAutomation = false
	}
	rs.setIssue(movedIssue)
	rs.clearExternalMove()
	return o.decideAfterTurn(ctx, rs, movedIssue, false)
}

// readSignals は turn が終わったあとに transcript を読んで表明を拾う（設計 3-25）。
//
// **turn が終わってから 0.5 秒待って読む。**`Stop` hook が走る時点では、その turn の
// 最後の text ブロックがまだ JSONL に書かれていない（13件すべてで未書き込み）。
// **見つからなければ 0.1 秒間隔で最大5回読み直す。5回で諦める**
// （待つ合計は 0.5 + 0.5 = 1.0 秒）。それでも無ければ「表明なし」として扱い、次の turn で促す。
//
// **トークンも同じ1回の読み取りで取る**（設計 3-15。2回開かない）。
// **結果はログに出し、あわせて `runState` にも控える**（ダッシュボードが読む。3-15 / 5-2）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値: 拾った表明（対象の識別子 → 値）。
func (o *Orchestrator) readSignals(ctx context.Context, rs *runState) map[string]string {
	snap := rs.snapshot()
	rs.mu.Lock()
	path := rs.TranscriptPath
	promptID := rs.PromptID
	// **セッション UUID を、パスと同じ区間で写し取る**（issue #238）。
	// **別々に読むと、その間に `restartWithNewSession` がセッションを採り直したときに、
	// 別のセッションのパスと UUID の対を台帳へ入れることになる。**
	sessionUUID := rs.SessionUUID
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
			o.logTokens(rs, path, sessionUUID, result.Usage)
			return result.Signals
		}
	}

	if last != nil {
		o.logTokens(rs, path, sessionUUID, last.Usage)
	}
	o.logger.Info("この turn には表明がありませんでした（次の turn で促します）", "identifier", snap.Identifier)
	return nil
}

// logTokens は集計したトークンをログに出し、`runState` と run をまたぐ累計に控える
// （設計 3-15 / issue #238）。
//
// **控えるのはダッシュボード（第9段階）が読むためだけである。**判断には使わない。
// **HTTP の要求ごとに transcript を開き直さない**ので、turn の終わりに1回だけ書く。
//
// **書き込みは `addTokenUsage` が1つの `o.mu` の区間でまとめて行う。**
// 累計と run ごとの値を別々に書くと、その隙間にダッシュボードが両方を読み切ったときに
// **「累計が走行中の run の合計より小さい」写しができる。**
// **ここで順序を守る必要は無い**（`addTokenUsage` の doc コメントを見よ）。
//
// **`addTokenUsage` は `rs.mu` の外で呼ぶ。**中で `o.mu` → `rs.mu` の順に取るので、
// `rs.mu` を持ったまま呼ぶと逆向きの入れ子ができる。
//
// **`path` は、ログに出すためだけに受け取る。**台帳が使うのはセッション UUID である
// （hook の `transcript_path` はエージェントが書き換えられる外部入力なので、鍵にしない）。
//
// rs: 対象の run。
// path: `usage` を読み出した transcript のパス（ログに出す）。
// sessionUUID: そのパスのセッション UUID。**`readSignals` がパスと同じ区間で写し取ったもの。**
// usage: 集計したトークン（その transcript 1ファイルの絶対値）。
func (o *Orchestrator) logTokens(rs *runState, path, sessionUUID string, usage TokenUsage) {
	identifier := rs.issue().Identifier
	clamped := o.addTokenUsage(rs, identifier, sessionUUID, usage, o.now())
	o.logger.Info("トークンを集計しました",
		"identifier", identifier,
		"api_calls", usage.APICalls,
		"input", usage.Input,
		"cache_creation", usage.CacheCreation,
		"cache_read", usage.CacheRead,
		"output", usage.Output,
	)
	if clamped {
		// **黙って丸めない。**累計が実際より小さくなったことを、あとから確かめられるようにする。
		o.logger.Warn("transcript の集計のうち、前回より小さかった項目の差分を0にしました",
			"identifier", identifier, "transcript_path", path)
	}
}

// applySignals は拾った表明どおりに Status を動かす（設計 3-25 / 3-26）。
//
// **対象を書かない行は、いま作業している issue を指す。**対象付きの行は
// `FetchIssueByIdentifier` で item を引く（その issue は `Ice Box` にあるので、巡回で
// 読んだ候補には入っていない）。
//
// **カンバンに載っていなかったら、その行を捨て、issue のコメントに
// 「カンバンに無いので動かせなかった」と書く**（人間が気づけるようにする）。
//
// **この機械の別の run が印を持っている issue にも書き込まない**（設計 3-26 の「安全のための制約」）。
// **書き間違えた1行で、別のエージェントが turn の途中で止まる**ためである。
// **守れるのは1台の中だけである。**別の機械が持ち回り（設計 3-77）で担当している issue は
// この機械の印を持たないので、この検査を抜ける。
//
// **`terminal_states` の issue は動かさない**（UpdateStatus が書く前に取り直して弾く）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// signals: 拾った表明。
func (o *Orchestrator) applySignals(ctx context.Context, rs *runState, signals map[string]string) {
	// **カンバンに載っていなかった対象は溜めて、1 turn につき1件のコメントにまとめる。**
	// 対象ごとに投稿すると、表明の行数ぶんだけ issue へコメントを書くことになる。
	var missing []string
	// **別の run が担当している対象も同じように溜める。**理由が違うので別のコメントにする。
	var claimed []string
	defer func() {
		o.noteSignalTargetsMissing(ctx, rs, missing)
		o.noteSignalTargetsClaimed(ctx, rs, claimed)
	}()

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
		nodeID := ""
		if strings.EqualFold(target, rs.issue().Identifier) {
			itemID = rs.issue().ID
			nodeID = issueNodeID(rs.issue())
		} else {
			found, ok, err := o.tracker.FetchIssueByIdentifier(ctx, target)
			if err != nil {
				o.logger.Warn("表明が指す issue を引けません",
					"identifier", rs.issue().Identifier, "対象", target, "error", err)
				continue
			}
			if !ok {
				o.logger.Warn("表明が指す issue がカンバンに載っていません（この行を捨てます）",
					"identifier", rs.issue().Identifier, "対象", target)
				missing = append(missing, target)
				continue
			}
			// **別の run が印を持っていたら、書かずに捨てる**（設計 3-26 の「安全のための制約」）。
			// **書き込むと、その run から見て「引き渡しの Status」になり、turn の途中でも
			// 即座に止められる**（reconcile の既定の枝が stopAndReleaseAsync を呼ぶ）。
			// **猶予はカンバンの自動化が書いたときだけ効く**ので、continuo 自身の書き込みは待ってもらえない。
			//
			// **引くのは、このプロセスの印（`o.runs`）だけである。**別の機械が持ち回り（設計 3-77）で
			// 担当している issue は印を持たないので、ここを抜けて Status が動く。
			// **hold のコメントを引けば分かるが、対象1件ごとにコメント全件の取得が要り**
			// （`FetchAllComments` はページを繰る）、**読んだ次の瞬間に別の機械が入札に勝つ余地も残る。**
			// **だから1台の中だけを守る**（設計 3-26 の「担当中の issue へは、表明から書かない」）。
			if other, held := o.lookupRunByID(found.ID); held && other != rs && !other.isFinished() {
				o.logger.Warn("表明が指す issue は別の run が担当中です（この行を捨てます）",
					"identifier", rs.issue().Identifier, "対象", target,
					"担当中の run", other.issue().Identifier, "遷移先", *next)
				claimed = append(claimed, target)
				continue
			}
			itemID = found.ID
			// **記録は動かされた issue へ書く。**代表の issue に何件も並べない（設計 3-26）。
			nodeID = issueNodeID(found)
		}

		moved, err := o.tracker.UpdateStatus(ctx, itemID, *next, o.cfg.Tracker.TerminalStates)
		if err != nil {
			o.logger.Warn("表明どおりに Status を動かせません",
				"identifier", rs.issue().Identifier, "対象", target, "遷移先", *next, "error", err)
			continue
		}
		o.logger.Info("表明どおりに Status を動かしました",
			"identifier", rs.issue().Identifier, "対象", target, "遷移先", *next, "書き込んだか", moved.Wrote)
		if moved.Reached && itemID == rs.IssueID {
			// **いま作業している issue へ書けたときだけ控える**（設計 3-50）。
			// 知らない Status になったときに「元は何だったか」を書くために要る。
			rs.setLastWrittenState(*next)
		}
		o.postStatusMove(ctx, rs.issue().Identifier, nodeID, newStatusMove(moved, *next),
			signalMoveReason(rs.issue().Identifier, target, value, itemID == rs.IssueID))
	}
}

// signalMoveReason は、表明で Status を動かしたときの「なぜ」の文を作る（設計 3-29）。
//
// **表明を書いたのは、いま作業している issue のエージェントである。**対象が別の issue の
// ときは、誰の表明で動いたのかが分かるように、その issue の識別子を添える（設計 3-26）。
//
// identifier: いま作業している issue の識別子。
// target: 表明が指していた対象。
// value: 表明の値（`review` / `blocked` など）。
// self: 対象がいま作業している issue 自身かどうか。
// 戻り値: 「〜ためです」で終わる1文。
func signalMoveReason(identifier, target, value string, self bool) string {
	if self {
		return fmt.Sprintf("担当している Claude Code が `CONTINUO-STATUS: %s` と表明したためです", value)
	}
	return fmt.Sprintf("`%s` を担当している Claude Code が `CONTINUO-STATUS: %s %s` と表明したためです",
		identifier, target, value)
}

// noteSignalTargetsMissing は、表明が指す issue がカンバンに載っていなかったことを
// issue のコメントに残す（設計 3-25）。
//
// **1 turn につき1件にまとめる。**対象ごとに投稿すると、エージェントが印を並べた
// 行数ぶんだけコメントが積まれる。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// targets: 表明が指していた識別子の並び。空なら何もしない。
func (o *Orchestrator) noteSignalTargetsMissing(ctx context.Context, rs *runState, targets []string) {
	if len(targets) == 0 {
		return
	}
	nodeID := issueNodeID(rs.issue())
	if nodeID == "" {
		return
	}
	body := fmt.Sprintf("表明に書かれた %s は、このカンバンに載っていないので Status を動かせませんでした。",
		strings.Join(targets, " / "))
	if err := o.postComment(ctx, nodeID, body); err != nil {
		o.logger.Warn("表明の取りこぼしを投稿できませんでした", "identifier", rs.issue().Identifier, "error", err)
	}
}

// noteSignalTargetsClaimed は、表明が指す issue を別の run が担当していたことを
// issue のコメントに残す（設計 3-26）。
//
// **書き込みを止めただけでは、エージェントも人間も気づけない。**
// 番号を書き間違えたのなら、ここを読んで書き直せる。
//
// **書くのは、いま作業している issue のほうである。**担当中の issue へ書くと、
// 何も起きていない run のコメント欄が、他人の書き間違いで埋まる。
//
// **1 turn につき1件にまとめる**（`noteSignalTargetsMissing` と同じ理由）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// targets: 別の run が担当していた識別子の並び。空なら何もしない。
func (o *Orchestrator) noteSignalTargetsClaimed(ctx context.Context, rs *runState, targets []string) {
	if len(targets) == 0 {
		return
	}
	nodeID := issueNodeID(rs.issue())
	if nodeID == "" {
		return
	}
	body := fmt.Sprintf("表明に書かれた %s は、いま別の Claude Code が担当しているので Status を動かしませんでした。"+
		"\n【なぜ止めたか】担当中の issue の Status を外から動かすと、そのエージェントが turn の途中で止まります。"+
		"\n【対処】番号の書き間違いなら、正しい番号で表明を書き直してください。"+
		"意図して動かしたいのであれば、人間がカンバンから動かしてください。",
		strings.Join(targets, " / "))
	if err := o.postComment(ctx, nodeID, body); err != nil {
		o.logger.Warn("担当中の issue への表明を止めたことを投稿できませんでした",
			"identifier", rs.issue().Identifier, "error", err)
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
// **「いまの Status を書いたのは誰か」を取るかどうかは、呼ぶ側が渡す**（設計 3-61）。
// **関数単位では分けられない。**この1本を、記録を読む `handleTurnEnd` と
// 読まない `finishRunClaimed` の両方が通るからである。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// withTimeline: 記録も取るか。**真で呼ぶのは `handleTurnEnd` だけである。**
// そこだけが取り直した issue を `rs.setIssue` で控え、知らない Status の判定
// （`decideAfterTurn` → `claimAutomatedRewrite` / `finishRunUnknownState`）がそれを読む。
// 戻り値の1つ目: 取り直した issue。**取り直せなかったときは、控えてある古い写しである。**
// 戻り値の2つ目: カンバンから見えていれば true。
// 戻り値の3つ目: **本当に GitHub から取り直せたなら true。**
//
//	**偽のときの1つ目は、着手したときの写しである。**担当者もラベルも Status も、そのあと動いている見込みがある。
//	**見るのは `verifyHandoff` だけである。**あそこは「担当が自分から他人へ移ったか」を答えるので、
//	古い写しから答えると、担当を外された run が push まで走り切る（設計 3-77c）。
//	**`handleTurnEnd` と `finishRunClaimed` は、取り直せなくても古い写しで続ける。**
//	止めるほうが害が大きいためである（turn の結果を捨てる／片付けを止める）。
//	**代償は、古い Status からカンバンへ書く場合があることである**（`handleTurnEnd` は
//	`decideAfterTurn` を通って Status を書き直しうる）。**`origin/main` から同じ形である。**
func (o *Orchestrator) refreshIssue(ctx context.Context, rs *runState, withTimeline bool) (tracker.Issue, bool, bool) {
	var (
		issues []tracker.Issue
		err    error
	)
	if withTimeline {
		issues, err = o.tracker.FetchIssuesByIDs(ctx, []string{rs.IssueID})
	} else {
		issues, err = o.tracker.FetchIssuesByIDsWithoutTimeline(ctx, []string{rs.IssueID})
	}
	if err != nil {
		o.logger.Warn("issue を取り直せません", "identifier", rs.issue().Identifier, "error", err)
		return rs.issue(), true, false
	}
	if len(issues) == 0 {
		return tracker.Issue{}, false, true
	}
	return issues[0], true, true
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
// **終わらせる処理は1本に絞る**（`claimTerminal`）。次の巡回が同じ run を
// もう一度終わらせにかかると、`failure_state` への書き込みと引き渡しコメントの投稿が
// 二重になる。
//
// **書き戻しが飛んでいたら、終わるまで待ってから印を取る**（設計 3-56）。
// **待たずに戻ると、この run を終わらせる者が誰も居なくなる。**turn ループはここを
// 呼んだあと戻ってしまうので、Status も動かず、引き渡しのコメントも出ず、印も外れない。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// failureState: 落とす先の Status。空なら落とさない。
// reason: 人間へ見せる理由。
func (o *Orchestrator) finishRun(ctx context.Context, rs *runState, failureState, reason string) {
	if !rs.claimTerminal(ctx) {
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
// **書き戻しが飛んでいたら、この巡回では終わらせない**（`claimTerminal` で待たない）。
// 巡回のループを書き込み1回ぶん止めることになるためである。**次の巡回でやり直せばよい。**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// failureState: 落とす先の Status。空なら落とさない。
// reason: 人間へ見せる理由。
func (o *Orchestrator) finishRunAsync(ctx context.Context, rs *runState, failureState, reason string) {
	if rs.beginTerminal() != terminalClaimed {
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
	o.logger.Info("run を終えます", "identifier", rs.issue().Identifier, "理由", summaryLine(reason))

	// **pane を閉じる前に、turn の終わりと同じ判定を1度通す**（設計 3-81）。
	// **通知より先に置く。**引き渡しの通知は1つの run につき1件しか出せないので
	// （`takeHandoffPost`）、あとに置くと「何を道連れにしたか」を書く先が残らない。
	//
	// **例外が1つある。**`finishRunUnknownState` は通知を投稿してからここへ来るので、
	// **その道の通知に載る一覧は、待つ前の写しである。**待っている間に終わったものも
	// 「止めた時点で走っていた」として並ぶ。**そちらを直すには通知の投稿を
	// この関数へ移すことになり、知らない Status の道が持っている
	// 「Status を動かした記録を添えない」という決まりと噛み合わない**（設計 3-50）。
	o.waitForBackgroundTasks(ctx, rs)

	if failureState != "" {
		moved, err := o.tracker.UpdateStatus(ctx, rs.IssueID, failureState, o.cfg.Tracker.TerminalStates)
		if err != nil {
			o.logger.Warn("Status を落とせません",
				"identifier", rs.issue().Identifier, "遷移先", failureState, "error", err)
		}
		o.postHandoffComment(ctx, rs, reason, newStatusMove(moved, failureState))
	}

	o.ensureAgentComment(ctx, rs)
	o.runAfterRun(ctx, rs)
	o.stopWorker(ctx, rs)

	// **最後まで通った run は、過去の失敗の記録を消す。**次に失敗したら0から数え直す。
	o.forgetFailure(rs.IssueID)

	// **「誰が Status を書いたか」は取らない**（設計 3-61）。ここで見るのは `State` だけであり、
	// **`rs.setIssue` でも控えない**ので、記録を読む経路へ空の写しが流れることも無い。
	current, ok, _ := o.refreshIssue(ctx, rs, false)
	if ok && o.ws.ShouldCleanup(current.State) {
		o.cleanupWorktree(ctx, rs)
	}
	o.release(rs)
}

// failRun は着手やテンプレートの変数展開に失敗した run を失敗として扱う。
//
// **worktree は残す。**次の巡回に委ねられる場合があるためである。
//
// **worker を止める前にコメントを確かめる**（設計 3-25 の「いつ走らせるか」）。
// **まだ1回も turn を送っていない run では何も起きない**（`ensureAgentComment` が
// `StartedAt` のゼロ値で抜ける）。書かせる材料が無いためである。
//
// **書き戻しが飛んでいたら、終わるまで待ってから印を取る**（設計 3-56）。
// **待たずに戻ると、着手に失敗した run が誰にも失敗として扱われないまま残る。**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// reason: 人間へ見せる理由。
func (o *Orchestrator) failRun(ctx context.Context, rs *runState, reason string) {
	if !rs.claimTerminal(ctx) {
		return
	}
	moved, err := o.tracker.UpdateStatus(ctx, rs.IssueID, o.cfg.Tracker.FailureState, o.cfg.Tracker.TerminalStates)
	if err != nil {
		o.logger.Warn("Status を落とせません",
			"identifier", rs.issue().Identifier, "遷移先", o.cfg.Tracker.FailureState, "error", err)
	}
	// **失敗は issue 単位で数える**（設計 3-16）。印はこのあと release で消えるので、
	// 印の中の RetryCount では次の巡回が0回目として拾い直してしまう。
	o.noteFailure(rs.IssueID, reason, moved.Reached && err == nil)
	o.postHandoffComment(ctx, rs, reason, newStatusMove(moved, o.cfg.Tracker.FailureState))
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
// （設計 3-25 の「いつ走らせるか」の表。`max_dispatch_turns` に達した / stall で打ち切った、は
// 「走らせる」である）。**確かめないと、その run の成果が issue に何も残らない。**
// **リトライがまだ残っている場合は走らせない。**run はこのあと再 dispatch されて続くので、
// 打ち切りではないためである。
//
// **書き戻しが飛んでいたら、終わるまで待ってから印を取る**（設計 3-56）。
// **待たずに戻ると、諦めるべき run にリトライが1つも積まれない。**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// reason: 人間へ見せる理由。
func (o *Orchestrator) abandonRun(ctx context.Context, rs *runState, reason string) {
	if !rs.claimTerminal(ctx) {
		return
	}
	o.abandonRunClaimed(ctx, rs, reason)
}

// abandonRunAsync は巡回のループから run を諦める（設計 3-8）。
//
// **印だけ同期で確保し、実際の処理は別の goroutine で回す。**打ち切りのときは
// 3-25 の9段（`agent.prompt` を待ち受けつきで呼ぶ）を通るので、既定では最大1時間返らない。
//
// **書き戻しが飛んでいたら、この巡回では諦めない**（次の巡回でやり直す）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// reason: 人間へ見せる理由。
func (o *Orchestrator) abandonRunAsync(ctx context.Context, rs *runState, reason string) {
	if rs.beginTerminal() != terminalClaimed {
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
			"identifier", snap.Identifier, "retry_count", snap.RetryCount, "理由", summaryLine(reason))
		// **順番は failRun と同じにする。**引き渡しの通知は1件だけ投稿できる
		// （takeHandoffPost）ので、**本当の理由が先に投稿枠を取らなければならない。**
		// 先に ensureAgentComment を呼ぶと、その中の failCommentRecovery が
		// 「作業を終えたと表明したのに何も書き残さなかった」という別の文面で枠を使い切り、
		// **stall で打ち切ったという本当の理由が issue に1文字も残らない。**
		moved, err := o.tracker.UpdateStatus(ctx, rs.IssueID, o.cfg.Tracker.FailureState, o.cfg.Tracker.TerminalStates)
		if err != nil {
			o.logger.Warn("Status を落とせません", "identifier", snap.Identifier, "error", err)
		}
		// **打ち切りも issue 単位で数える**（failRun と同じ器に積む）。
		o.noteFailure(rs.IssueID, reason, moved.Reached && err == nil)
		o.postHandoffComment(ctx, rs, reason, newStatusMove(moved, o.cfg.Tracker.FailureState))
		// **打ち切りである。worker を止める前にコメントを確かめる**（設計 3-25）。
		o.ensureAgentComment(ctx, rs)
		o.runAfterRun(ctx, rs)
		o.stopWorker(ctx, rs)
		o.release(rs)
		return
	}

	o.runAfterRun(ctx, rs)
	o.stopWorker(ctx, rs)

	backoff := retryBackoff(snap.RetryCount, time.Duration(o.cfg.Agent.MaxRetryBackoffMs)*time.Millisecond)
	count := rs.addRetry(o.now(), backoff)
	o.logger.Warn("run を諦めてリトライを積みました（バックオフの間も印には残します）",
		"identifier", snap.Identifier, "retry_count", count, "backoff", backoff, "理由", summaryLine(reason))
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
// **書き戻しが飛んでいたら、この巡回では止めない**（次の巡回でやり直す）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
func (o *Orchestrator) stopAndReleaseAsync(ctx context.Context, rs *runState) {
	if rs.beginTerminal() != terminalClaimed {
		return
	}
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		// **後片付けは「止めろ」と言われても最後までやる。**
		//
		// この run は既に終わったものとして扱われている（Status は動かした、
		// コメントも投稿した）。**そこで pane だけ閉じ損ねると、カンバン上は終わった issue の
		// pane が残り続ける。**`Close` は `wg.Wait()` でここを待つので、
		// 終わるまで待たせてよい。
		//
		// **期限は付ける。**herdr が応答しないときに、停止が永久に返らなくなるのを防ぐ。
		cleanupCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx), time.Duration(o.cfg.Herdr.ReadTimeoutMs)*time.Millisecond)
		defer cancel()
		o.runAfterRun(cleanupCtx, rs)
		o.stopWorker(cleanupCtx, rs)
		o.release(rs)
	}()
}

// waitForBackgroundTasks は run を終える前に、バックグラウンド処理の申告を1度見る
// （設計 3-81）。
//
// **turn の終わりでは同じ判定をしている**（設計 3-2。`background_tasks` が空でない `Stop` を
// 受けたら turn の終わりとせずに待ち直す）。**run の終わりでは1度も見ずに pane を閉じていた。**
// 走っていたものは道連れになり、次に同じセッションへ復帰したときに
// 「前のバックグラウンドコマンドに完了の記録が無い」という通知が発火する。
//
// **待つのは「まだ何かが動いていると分かるとき」だけである。**
//
//	申告が空                  … 何もしない
//	確認の画面で止まっている   … **待たない**（新しい `Stop` は二度と来ない）
//	それ以外                  … 申告が空になるまで poll_wait_ms を上限に待つ
//
// **待たない条件を1つだけ置く理由。**申告が入れ替わるのは新しい `Stop` を受けたときだけである
// （`noteHook`）。**`blocked` で止まった Claude Code は `Stop` を二度と出さない。**
// そこで待つと、直前の `waitForRunningSubagents`（設計 3-11）と合わせて
// `claude.poll_wait_ms` を2回ぶん、人間への引き渡しが遅れるだけになる。
//
// **hook の新しさは条件にしない。**一度は `claude.settle_ms` で切っていたが、
// **その線では1回も待たなかった。**turn の終わりの判定は「空の `Stop` を受けてから
// `settle_ms` を使い切る」ところで `turnEnded` を返すので（`confirmTurnEnd`）、
// **ここへ着く時点で必ず `settle_ms` を超えている。**そのうえ transcript の読み直しと
// カンバンへの問い合わせが挟まる。**「待つ」を足したのに、足した場面で回らなかった。**
//
// **総時間に上限を置く。**turn の終わりの待ちには上限が無いが（設計 3-2）、ここは違う。
// **run は既に終わったものとして扱われている。**何時間も待つと
// `agent.max_concurrent_agents` の枠が空かず、次の issue が着手できない。
//
// **止められたら待つのをやめる。**pane を閉じるほうは `stopWorker` が
// 「止められていても閉じる」と決めているので、そちらは続く。
//
// **何を道連れにしたかを残すのはここではない。**`stopWorker` が pane を閉じる直前に
// 残す。**そちらに置かないと、この関数を通らない道**（`abandonRunClaimed` /
// `stopAndReleaseAsync` / `ensureAgentComment` の段2）**で記録が1行も出ない。**
//
// **新しい設定は足さない。**猶予は `claude.poll_wait_ms`、hook の新しさの線は
// `claude.settle_ms` を使う。
//
// ctx: 待ちを打ち切るコンテキスト。
// rs: 対象の run。
func (o *Orchestrator) waitForBackgroundTasks(ctx context.Context, rs *runState) {
	running := rs.runningBackgroundTasks()
	if len(running) == 0 {
		return
	}
	identifier := rs.issue().Identifier
	grace := time.Duration(o.cfg.Claude.PollWaitMs) * time.Millisecond
	switch {
	case rs.blockedHandoff():
		o.logger.Info("確認の画面で止まっているので、バックグラウンド処理を待ちません（新しい Stop は来ません）",
			"identifier", identifier, "バックグラウンド処理", running)
	case grace <= 0:
		o.logger.Info("猶予が0なので、バックグラウンド処理を待ちません",
			"identifier", identifier, "バックグラウンド処理", running)
	default:
		o.logger.Info("バックグラウンド処理が残っていると申告されているので、pane を閉じる前に待ちます",
			"identifier", identifier, "バックグラウンド処理", running, "猶予", grace)
		o.awaitBackgroundTasksDone(ctx, rs, grace)
	}
}

// awaitBackgroundTasksDone は申告が空になるのを猶予いっぱいまで待つ（設計 3-81）。
//
// **覗くだけにする。**受け口（`hookCh`）から読むと、turn の終わりの判定に使う hook を
// 横取りしてしまう（設計 3-2 の `isTurnBoundaryHook`）。**この時点では turn ループが
// まだ待ち受けに居ることがある。**
//
// ctx: 待ちを打ち切るコンテキスト。
// rs: 対象の run。
// grace: 待つ長さの上限。
func (o *Orchestrator) awaitBackgroundTasksDone(ctx context.Context, rs *runState, grace time.Duration) {
	deadline := time.NewTimer(grace)
	defer deadline.Stop()
	tick := time.NewTicker(subagentPollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-tick.C:
			if len(rs.runningBackgroundTasks()) == 0 {
				o.logger.Info("バックグラウンド処理が終わったので pane を閉じます",
					"identifier", rs.issue().Identifier)
				return
			}
		}
	}
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
// **pane を閉じる前に turn ループへ「待つのをやめろ」と伝える**（設計 3-51）。
// 伝えずに閉じると、待ち受けの中にいた turn ループが
// `turn を送れませんでした（agent is no longer running）` を WARN で印字する。
// **それは外の障害ではなく、continuo が1秒前に自分で pane を閉じた結果である。**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
func (o *Orchestrator) stopWorker(ctx context.Context, rs *runState) {
	rs.mu.Lock()
	paneID := rs.PaneID
	rs.PaneID = ""
	rs.mu.Unlock()
	// **閉じる前に伝える。**順番を入れ替えてはならない。
	rs.markWorkerStopped()
	if paneID == "" {
		return
	}
	// **何を道連れにしたかを残す**（設計 3-81）。**残さないと、次に同じセッションへ
	// 復帰したときに発火する「前のバックグラウンドコマンドに完了の記録が無い」という
	// 通知の出どころを、人間が辿れない。**
	//
	// **`waitForBackgroundTasks` ではなくここに置く。**`stopWorker` の呼び出しは
	// **11箇所・9関数**である（`git grep -n 'o\.stopWorker(' -- internal/`。
	// `finishRunClaimed` / `failRun` / `abandonRunClaimed` / `stopAndReleaseAsync` /
	// `ensureAgentComment` の段2 / `failCommentRecovery` / コメントが書けたので閉じる道 /
	// 知らない Status / 担当が移った / 着手をやめた）。
	// **待つのは1つだけだが、道連れにするのは全部だからである。**
	if left := rs.runningBackgroundTasks(); len(left) > 0 {
		o.logger.Warn("バックグラウンド処理が残ったまま pane を閉じます（走っていたものは途中で終わります）",
			"identifier", rs.issue().Identifier, "バックグラウンド処理", left)
	}
	// **止められていても pane は閉じる。**
	//
	// ここへ来た run は既に終わったものとして扱われている（Status を動かし、
	// コメントも投稿した）。**そこで閉じ損ねると、カンバン上は終わった issue の pane が
	// 残り続ける。**`Close` は `wg.Wait()` でここを待つので、終わるまで待たせてよい。
	//
	// **期限は付ける。**herdr が応答しないときに停止が永久に返らなくなるのを防ぐ。
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx),
			time.Duration(o.cfg.Herdr.ReadTimeoutMs)*time.Millisecond)
		defer cancel()
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
	o.runAfterRunOK(ctx, rs)
}

// runAfterRunOK は `workspace_hooks.after_run` を走らせ、**成功したかどうかを返す。**
//
// **`released` のコメントは「`after_run` は実行済みです」と断言する**（設計 3-27）。
// **失敗したのに断言すると、次に拾う機械が「remote の続きから始めてください」に従い、
// 入っていない commit の続きから始める。**手元の作業が黙って取り残される。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値: `after_run` が走って成功したら true。
// **走らせる相手が無かったとき（worktree のパスが空）も false である。**
func (o *Orchestrator) runAfterRunOK(ctx context.Context, rs *runState) bool {
	snap := rs.snapshot()
	if snap.WorktreePath == "" {
		return false
	}
	// **設定されていないときは、走らせたと言ってはならない**（issue #197）。
	// **`RunAfterRunOnce` は「この worktree で初めて呼ばれたか」を返すだけで、
	// コマンドが空でも真を返す**（`RunHook` は設定が無ければ nil を返して終わる）。
	// **既定の `WORKFLOW.md` は `after_run` を持たない。**
	// **そのまま真として扱うと、1バイトも push していないのに
	// 「実行済みです。remote の続きから始めてください」と issue へ書く。**
	// **次に拾う機械は remote から worktree を作り直し、push していない commit を全部失う。**
	if o.cfg.WorkspaceHooks.AfterRun == nil ||
		strings.TrimSpace(*o.cfg.WorkspaceHooks.AfterRun) == "" {
		return false
	}
	// **1つ目の戻り値を捨ててはならない。**あれは「この worktree でまだ走らせていないので、
	// いま走らせた」を表す。**偽になるのは、既に走らせたときである。**
	// **走らせ切ったことを run が覚えている**（issue #197）。
	// **やり直しのために要る。**担当を外すのに失敗して次の巡回でやり直すと、
	// `RunAfterRunOnce` は「走らせていない」を返すので、
	// **既に push してあるのに「remote に続きが入っていないことがあります」と issue へ書く。**
	if rs.afterRunDone() {
		return true
	}
	// **`ran` は「この worktree で初めて呼ばれたか」であって、成否ではない。**
	// **走って失敗したときも真が返る。**
	ran, err := o.ws.RunAfterRunOnce(ctx, snap.WorktreePath)
	if err != nil {
		// **走ったが失敗した。**「走りませんでした」とは書けないが、
		// **remote に続きが入っていない恐れは同じである**ので、偽を返す。
		// **文面の1文目が事実と違う点は、この1行で人間へ渡す。**
		o.logger.Warn("workspace_hooks.after_run は走りましたが失敗しました"+
			"（issue のコメントには「走りませんでした」と出ます。remote の中身を確かめてください）",
			"identifier", snap.Identifier, "error", err)
		return false
	}
	if ran {
		rs.markAfterRunDone()
	}
	return ran
}

// cleanupWorktree は worktree と branch と設定ファイルを片付ける（設計 3-9）。
//
// **見送った理由は、身元ファイルの cleanup_deferred_at がゼロ値のときだけ issue へ書く**
// （毎巡回で警告を積まない。設計 3-9 の手順2c）。
//
// **実際に消えたときだけ、トークンの台帳からもこの issue を落とす**（issue #238）。
// **worktree が残っているうちは落とせない。**残っていると、人間が Status を戻したときに
// 同じセッションへ `--resume` で復帰し、**同じ transcript が最初から全部足される。**
// **消えなかったとき（`cleanupPath` が false）は落とさない**ので、見送りでも二重に数えない。
//
// **`cleanupPath` を呼ぶ場所は他にも2つある**（`SweepOnStartup` と復元の `cleanupInto`）。
// **そちらは台帳を落としていない。**どちらも起動時にしか走らず、その時点で台帳は空だからである。
// **巡回の最中に走る片付けを新しく足すなら、そこでも `forgetTokenLedger` を呼ぶこと。**
//
// **`reconcileWorktrees` も巡回の最中に worktree を消すが、台帳を落としていない。**
// **これは欠陥ではない。**理由と、そこへ足すときの注意は
// docs/plans/impl/09_dashboard.md の「いつ台帳を消すか」にある（**そちらが正である**）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
func (o *Orchestrator) cleanupWorktree(ctx context.Context, rs *runState) {
	snap := rs.snapshot()
	if snap.WorktreePath == "" {
		return
	}
	if o.cleanupPath(ctx, snap.Identifier, snap.WorktreePath, snap.Base, issueNodeID(rs.issue())) {
		o.forgetTokenLedger(snap.Identifier)
	}
}

// cleanupPath は worktree と branch と設定ファイルを、パスを指定して片付ける（設計 3-9）。
//
// **印を持っていない worktree も片付けられるようにするために、runState から切り離してある**
// （復元の段5a と段8 が、印に入っていない worktree をここへ渡す。設計 3-4）。
//
// ctx: 呼び出しに適用するコンテキスト。
// identifier: ログとコメントに出す issue の識別子。
// worktreePath: 片付ける worktree の絶対パス。
// base: worktree を作ったときの base（3-9 の手順2b）。**分からなければ空でよい。**
// その場合、upstream が無い branch は「判定できない」として見送られる（消さない側に倒す）。
// nodeID: 下敷きの GitHub issue のノード ID。空ならコメントは書かない。
// 戻り値: worktree と branch を実際に消したら true。
func (o *Orchestrator) cleanupPath(
	ctx context.Context,
	identifier, worktreePath string,
	base normalize.SafeName,
	nodeID string,
) bool {
	result, err := o.ws.Cleanup(ctx, workspace.CleanupRequest{
		WorktreePath: worktreePath,
		Base:         base,
	})
	if err != nil {
		o.logger.Warn("worktree を片付けられません", "identifier", identifier, "error", err)
		return false
	}
	if result.Removed {
		// **消えていない branch を「片付けた」と書かない**（CleanupResult.BranchDeleted の規則）。
		// **元から無かった branch を「残しました」とも書かない**（issue #27）。
		switch {
		case result.BranchDeleted:
			o.logger.Info("worktree と branch を片付けました", "identifier", identifier, "path", worktreePath)
		case result.BranchAbsent:
			o.logger.Info("worktree を片付けました（branch は元からありませんでした）",
				"identifier", identifier, "path", worktreePath)
		default:
			o.logger.Info("worktree を片付けました（branch は残しました）",
				"identifier", identifier, "path", worktreePath)
		}
		return true
	}
	o.logger.Warn("worktree を片付けずに残しました",
		"identifier", identifier, "path", worktreePath, "理由", strings.Join(result.Reasons, " / "))
	// **理由だけでは、読んだ人間は次に何をすればよいか分からない**（設計 3-49）。
	// git が1つも答えられなかったなら、その worktree は壊れており、
	// **continuo は二度と自分では片付けられない。**巡回のたびに同じ理由が出続けるので、
	// **人間が手で始末するための3行をその場に出す。**
	for _, step := range result.NextSteps {
		o.logger.Warn("壊れた worktree です。次にこれをしてください",
			"identifier", identifier, "path", worktreePath, "手順", step)
	}

	if !result.ShouldComment || nodeID == "" {
		return false
	}
	body := fmt.Sprintf("worktree を片付けずに残しました（%s）。\n\n理由:\n- %s",
		worktreePath, strings.Join(result.Reasons, "\n- "))
	if err := o.postComment(ctx, nodeID, body); err != nil {
		o.logger.Warn("片付けを見送った通知を投稿できませんでした", "identifier", identifier, "error", err)
		return false
	}
	// **投稿に成功したあとで印を書く。**投稿の前に書くと、投稿が失敗したときに
	// コメントが永久に出なくなる（設計 3-9 の手順2c）。
	if err := o.ws.MarkCleanupDeferred(worktreePath, o.now()); err != nil {
		o.logger.Warn("片付けを見送った印を書けませんでした", "identifier", identifier, "error", err)
	}
	return false
}

// handoffSubagentLimit は引き渡しの通知に載せる subagent の記録の件数の上限である。
//
// **全部は載せない。**1回の run で subagent を何十本も立てることがあり、
// 全件を並べるとコメントが読めなくなる。**新しい順に上から数件あれば、
// 止まった直前に何が動いていたかは辿れる。**
const handoffSubagentLimit = 3

// handoffBackgroundTaskLimit は引き渡しの通知に載せるバックグラウンド処理の件数の上限である
// （設計 3-81）。
//
// **申告は hook から来る外部入力である**（設計 3-2 / 3-23）。控えのほうは
// `maxTrackedSubagents`（64件）で切っているが、**64件をコメントへ並べると読めなくなる。**
// **何が道連れになったかを人間が当たれる件数だけ載せる。**
const handoffBackgroundTaskLimit = 5

// limitStrings は並びの先頭から n 件までを返す。
//
// **元の並びを書き換えない。**呼び出し側が控えをそのまま渡すことがある。
//
// in: 元の並び。
// n: 上限。0以下なら空を返す。
// 戻り値: 先頭から n 件まで。
func limitStrings(in []string, n int) []string {
	if n <= 0 || len(in) == 0 {
		return nil
	}
	if len(in) <= n {
		return in
	}
	return in[:n]
}

// postHandoffComment は人間へ引き渡すときの通知を issue へ書く。
//
// **成果の要約は書かない**（設計 3-29）。continuo が書くのは、この通知と
// Status を動かした記録（`postStatusMove`）の2つだけである。
//
// **1つの run について1件だけ投稿する。**2件目以降は理由をログに残して捨てる。
//
// **Status を動かした記録もこの通知の中に入れる**（設計 3-29）。この経路では
// `postStatusMove` を呼ばない。呼ぶと、同じことを書いたコメントが2件並ぶ。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// reason: 引き渡す理由。
// move: Status を動かした記録。書き込みが起きていなければ1行も出さない。
func (o *Orchestrator) postHandoffComment(ctx context.Context, rs *runState, reason string, move statusMove) {
	nodeID := issueNodeID(rs.issue())
	if nodeID == "" {
		return
	}
	if !rs.takeHandoffPost() {
		// **1つの run について1件だけにする。**failure_state へ落としたあとに
		// コメントを書かせられなかった場合、理由の違う通知が2件並んでしまう。
		o.logger.Info("引き渡しの通知は投稿済みなので重ねて書きません",
			"identifier", rs.issue().Identifier, "理由", summaryLine(reason))
		return
	}
	snap := rs.snapshot()
	// **ファイルの走査はロックの外で行う。**`snapshot` で `rs.mu` は既に外れている。
	//
	// **まず走っている subagent の `agent_id` から組み立てる**（設計 3-11）。
	// `SubagentStart` が `agent_id` を持っているので、置き場所は推測せずに決まる。
	// **1件も走っていない・記録がまだ書かれていないなら、glob に落ちる。**
	// `SubagentStart` を取りこぼした場合と、前の turn の subagent の記録を見たい場合が
	// あるので、**glob の側は消さない。**
	//
	// **`blocked` の道では、esc を送る直前に凍結した集合を使う**（`handoffSubagentIDs`）。
	// **理由の文面と同じ時点で数えるためである。**通知を書くのは esc の数百ミリ秒あとであり、
	// いま走っているものを数え直すと、**「N 件を止めました」と書きながら記録は
	// 1件も載らない**が起きる。
	subagentRunning := false
	subagentDir, subagentTranscripts := SubagentTranscriptsFor(
		snap.TranscriptPath, o.transcriptRoot, rs.handoffSubagentIDs(), handoffSubagentLimit)
	if len(subagentTranscripts) > 0 {
		subagentRunning = true
	} else {
		subagentDir, subagentTranscripts = ListSubagentTranscripts(
			snap.TranscriptPath, o.transcriptRoot, handoffSubagentLimit)
	}
	if subagentDir != "" {
		// **Debug より上で出してはならない。**subagent を1つも使わなかった turn では
		// ディレクトリが作られず、それは正常な並びである。
		o.logger.Debug("引き渡しの通知に subagent の記録を載せます",
			"identifier", rs.issue().Identifier, "置き場所", subagentDir,
			"件数", len(subagentTranscripts), "走行中のものか", subagentRunning)
	}
	// **切り捨てた件数も渡す**（設計 3-81b）。**黙って上から数件だけ出すと、
	// 読んだ人は「道連れになったのはこれで全部だ」と読む。**
	runningBackgroundTasks := rs.runningBackgroundTasks()
	shownBackgroundTasks := limitStrings(runningBackgroundTasks, handoffBackgroundTaskLimit)
	if err := o.postComment(ctx, nodeID,
		buildHandoffComment(rs.issue().Identifier, reason, handoffContext{
			WorktreePath:        snap.WorktreePath,
			TranscriptPath:      snap.TranscriptPath,
			SubagentDir:         subagentDir,
			SubagentTranscripts: subagentTranscripts,
			SubagentRunning:     subagentRunning,
			SettingsPath:        snap.SettingsPath,
			// **止めた時点で走っていたバックグラウンド処理を載せる**（設計 3-81）。
			// **凍結しない。**`blocked` の道の subagent と違い、こちらは
			// `finishRunClaimed` の先頭で待ち終えた直後のものを載せたい。
			BackgroundTasks:        shownBackgroundTasks,
			BackgroundTasksOmitted: len(runningBackgroundTasks) - len(shownBackgroundTasks),
		}, move)); err != nil {
		o.logger.Warn("引き渡しの通知を投稿できませんでした", "identifier", rs.issue().Identifier, "error", err)
	}
}

// containsFold は states に target が（大文字小文字を無視して）含まれるかを返す。
//
// **比較は大文字小文字を無視する**（表示はカンバンの綴りをそのまま保つ。設計 3-13）。
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
