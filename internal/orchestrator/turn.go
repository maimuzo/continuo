package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/hookserver"
	"github.com/maimuzo/continuo/internal/ratelimit"
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
	// turnAborted は ctx が終わったことを表す。
	turnAborted
	// turnSendFailed は herdr への呼び出しそのものが失敗したことを表す。
	//
	// **`turnStalled` と混ぜてはならない。**混ぜると、turn を1文字も送れていないのに
	// 「herdr は agent が待機状態になったと答えたが Stop hook の通知が届かなかった」という、
	// **起きていないことを断定した文面が issue に残る**（`agent_not_found` /
	// `agent_not_ready` / socket 断のいずれでもそうなる）。
	turnSendFailed
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
		// **Close でも終われるようにする。**turn ループの待ちには期限が無い
		// （`claude.turn_timeout_ms` は turn の総実行時間の上限ではない。設計 3-21）ので、
		// 呼び出し側の ctx が終わらないまま Close だけを呼ばれると永久に返らない。
		turnCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		defer context.AfterFunc(o.shutdown, cancel)()
		o.turnLoop(turnCtx, rs, epoch, awaitFirst)
	}()
	return true
}

// turnLoop は1つの run の turn を、終わるまで送り続ける（設計 3-8）。
//
//	1回目   … 設定の本文（5-3）を text/template で変数展開したもの
//	2回目〜 … 継続の指示のみ（5-4）。1回目の本文は送り直さない
//	打ち切り … max_dispatch_turns（既定20）に達したら failure_state へ落とす
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
	// **continuo 自身が worker を止めたら、herdr の待ちだけをやめる**（設計 3-51）。
	//
	// **`ctx` そのものを切ってはならない。**turn の終わりの処理（表明の適用・
	// コメントの確認・`worktree` の片付け）はこの `ctx` で動いており、`finishRun` の
	// 中から `stopWorker` が呼ばれる。切ると、**自分で自分の後片付けを道連れにする。**
	waitCtx, waitCancel := context.WithCancel(ctx)
	defer waitCancel()
	defer context.AfterFunc(rs.workerStopContext(), waitCancel)()

	for {
		if ctx.Err() != nil || !rs.currentWorker(epoch) {
			return
		}

		snap := rs.snapshot()

		var outcome turnOutcome
		// **送信そのものが失敗したときの原因を握っておく。**握らないと、issue に
		// 残す理由が「Stop hook が届かなかった」という別の話にすり替わる。
		var sendErr error
		if awaitFirst {
			// **引き継いだ run である。turn を送らずに、走っている turn の終わりを待つ**
			// （設計 3-4 の段5a2「hook を待ち、来なければ stall 検知で拾う」の前半）。
			// **送ると turn が混ざる。**待ち切れないことはここでは判定しない。
			// **画面が止まったかどうかは巡回の stall 検知（checkStalls）だけが決める。**
			awaitFirst = false
			o.logger.Info("引き継いだ run の turn の終わりを待ちます（turn は送りません）",
				"identifier", snap.Identifier)
			outcome, sendErr = o.confirmTurnEnd(waitCtx, rs, false)
		} else {
			if snap.TurnCount >= o.cfg.Agent.MaxDispatchTurns {
				o.finishRun(ctx, rs, o.cfg.Tracker.FailureState,
					fmt.Sprintf(
						"continuo がこの issue へ送った指示の回数が、上限の %d 回に達しました。"+
							"**Claude Code は動いていましたが、作業が終わったという表明を出しませんでした。**"+
							"\n【確かめ方】worktree の中身（下記）と、この issue に残っているエージェントのコメントを見てください。"+
							"\n【よくある原因】issue の内容が大きすぎる / 指示が曖昧で終わりが判断できない。"+
							"\n【対処】issue を分けるか、内容を具体的にしてから Status を着手待ちへ戻してください。"+
							"上限は WORKFLOW.md の `agent.max_dispatch_turns` で変えられます（いまは %d）。",
						o.cfg.Agent.MaxDispatchTurns, o.cfg.Agent.MaxDispatchTurns))
				return
			}

			text, err := o.buildTurnText(rs, snap)
			if err != nil {
				o.logger.Warn("プロンプトを組み立てられません", "identifier", snap.Identifier, "error", err)
				o.failRun(ctx, rs, fmt.Sprintf(
					"Claude Code へ送る指示の文面を組み立てられませんでした。"+
						"**この turn の指示は送れていません。**"+
						"\n【確かめ方】WORKFLOW.md の front matter（先頭の `---` で挟まれた部分）より**下の本文**を見てください。"+
						"ここが1回目の指示のテンプレートです（YAML のキーではありません）。"+
						"\n【よくある原因】テンプレートに、一覧に無い変数を `{{.名前}}` の形で書いた。"+
						"\n【対処】テンプレートを直してから Status を着手待ちへ戻してください。"+
						"\n元のエラー: %v", err))
				return
			}

			outcome, sendErr = o.sendTurn(waitCtx, rs, text)
		}
		if !rs.currentWorker(epoch) {
			// **待っている間に、巡回の stall 検知などが先にこの run を諦めていた。**
			// ここで諦め直すと RetryCount が2倍の速さで消費され、引き渡しのコメントも
			// 二重に投稿される（設計 3-21）。
			o.logger.Debug("待ち受けから戻ったときには別の経路が run を終わらせていました",
				"identifier", snap.Identifier)
			return
		}
		// **止められただけなら、run を諦めない。**
		//
		// `Close` は `shutdownCancel()` を呼んでから待つ。turn の待ち受けはそこで
		// 中断され、`turnStalled` として返る。**そのまま諦めると pane を閉じにいき、
		// ctx が死んでいるので失敗して「pane を閉じられませんでした」が出る。**
		// `Close` 自身は「pane は閉じない」と決めているので、食い違っていた。
		//
		// **走行中の run は、次の起動で引き継ぐ**（設計 3-4 / 第7段階の復元）。
		// ここで諦めると RetryCount を無駄に消費し、引き渡しのコメントまで投稿される。
		if ctx.Err() != nil {
			o.logger.Debug("止められたので、この run はそのままにします（次の起動で引き継ぎます）",
				"identifier", snap.Identifier)
			return
		}
		switch outcome {
		case turnAborted:
			return
		case turnBlocked:
			// **esc を送る前に、走っている subagent が終わるのを待つ**（設計 3-11）。
			// **待つのは「別の subagent が書き終えるのを待つ」ためである。**
			// **`blocked` が解けるからではない。**確認の画面は自分では消えないので、
			// 待っても解けない。引き渡しは直後に pane を閉じる（`finishRun`）ので、
			// 待たずに esc を送ると、そのとき書きかけだった編集がまるごと消える。
			o.waitForRunningSubagents(waitCtx, rs)
			if ctx.Err() != nil || !rs.currentWorker(epoch) {
				// **待っている間に、別の経路がこの run を終わらせていた**（上の分岐と同じ理由）。
				// ここで諦め直すと RetryCount が2倍の速さで消費され、引き渡しの
				// コメントも二重に投稿される（設計 3-21）。
				o.logger.Debug("サブエージェントを待っている間に、別の経路が run を終わらせていました",
					"identifier", snap.Identifier)
				return
			}
			// **理由の文面と【調べるところ】を、同じ時点で数える**（設計 3-11）。
			// 通知を投稿するのは esc の数百ミリ秒あとであり、その間に `SubagentStop` が
			// 届くと、**「N 件を止めました」と書きながら記録は1件も載らない**が起きる。
			stillRunning := rs.freezeHandoffSubagents()
			// **次を投げる前に必ず esc を送る**（設計 3-11。送らずに投げると保留中の
			// 権限要求が承認されて実行される。3/3 で再現）。
			o.sendEscape(ctx, rs)
			// **原因を断定しない。**何が確認の画面を出したかは continuo の側に残らない
			// （設計 3-11。`Notification` hook は出ず、拒否は静かに起きる）。
			// 書けるのは「記録を見て確かめてください」までである。
			o.finishRun(ctx, rs, o.cfg.Tracker.FailureState, blockedHandoffReason(stillRunning))
			return
		case turnStalled:
			o.abandonRun(ctx, rs, "Claude Code の turn が終わったことを検知できませんでした。"+
				"herdr は「agent が待機状態になった」と答えましたが、"+
				"**Claude Code の Stop hook から continuo へ通知が届きませんでした。**"+
				"\n【確かめ方】下記の「continuo が渡した設定」のファイルを開き、"+
				"`hooks` の `Stop` に continuo の hook が書かれているかを見てください。"+
				"**この設定は worktree の中ではなく、continuo の実行時ディレクトリにあります。**"+
				"\n【よくある原因】hook を受ける socket のパスが変わった / "+
				"エージェントが `/hooks` などで設定を上書きした。"+
				"\n【対処】Status を着手待ちへ戻してください。次の着手で設定を書き直します。")
			return
		case turnStopUnreadable:
			// **「届かなかった」と書いてはならない。**届いている。読めなかっただけである。
			//
			// **continuo のログを案内してはならない**（設計 3-34b の「持っていないものは
			// 案内しない」）。continuo は hook の中身をどこにも残していない。
			// 配送できた hook は逃がし先（pending ディレクトリ）にも残らないので、
			// 案内すると読んだ人は存在しない行を探しにいくことになる。
			//
			// **「JSON が途中で切れた」も原因に挙げてはならない。**途中で切れた JSON は
			// `decodeEvent` が弾き、hook そのものが orchestrator まで届かない
			// （internal/hookserver/server.go の decodeEvent）。この文面が出る経路では
			// JSON は読めている。読めなかったのは `background_tasks` の項目だけである。
			o.abandonRun(ctx, rs, "Claude Code の turn が終わったことを検知できませんでした。"+
				"**Stop hook は continuo へ届きましたが、`background_tasks` の項目が入っていなかったため、"+
				"バックグラウンド処理がまだ残っているのかどうかを判断できませんでした。**"+
				"\n【確かめ方】**continuo は hook の中身を残していないので、届いた `Stop` を"+
				"あとから読み返すことはできません。**下記の【調べるところ】に "+
				"「Claude Code の会話の記録」があれば、それを開いて、"+
				"turn がどこまで進んでいたかを見てください。"+
				"\n【よくある原因】Claude Code の版が上がって `background_tasks` の項目が無くなった / "+
				"`background_tasks` が null で届いた。"+
				"\n【対処】Status を着手待ちへ戻してください。"+
				"作業そのものは worktree に残っています（下記）。")
			return
		case turnSendFailed:
			// **turn は1文字も届いていない。**Stop hook を疑わせる文面を出してはならない。
			o.abandonRun(ctx, rs, fmt.Sprintf(
				"continuo が herdr へ指示を送れませんでした。"+
					"**この turn の指示は Claude Code に届いていません。**"+
					"\n【確かめ方】herdr の画面で、この issue の pane がまだ開いているかを見てください。"+
					"pane が消えていれば、そこで動いていた Claude Code はもう居ません。"+
					"\n【よくある原因】人間が herdr の画面から pane を閉じた（agent_not_found） / "+
					"herdr が応答しない（socket が落ちている） / agent がまだ指示を受けられない（agent_not_ready）。"+
					"\n【対処】herdr が動いていることを確かめてから、Status を着手待ちへ戻してください。"+
					"\n元のエラー: %v", sendErr))
			return
		case turnTransient:
			// **run を諦めない**（設計 3-48）。herdr の再起動・socket の一瞬の不通・
			// 応答の遅れである。Claude Code は pane の中でそのまま動いている。
			//
			// **`agent.prompt` を送り直さない。**届いていたかどうかは分からず、
			// 届いていた場合に送り直すと turn が二重に投入される。だから
			// `NeedsPrompt` ではなく `awaitTurnEnd` を立てる。次の巡回は
			// 「turn を送らずに turn の終わりを待つ」ところから入る（設計 3-4 の段5a2）。
			//
			// **黙って止まりはしない。**画面が動かないままなら、巡回の stall 検知
			// （checkStalls）が `claude.turn_timeout_ms` の沈黙で拾う。
			o.logger.Info("herdr との通信が一時的に失敗したので、次の巡回で turn の終わりを待ち直します",
				"identifier", snap.Identifier, "error", sendErr)
			rs.setAwaitTurnEnd()
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

// subagentPollInterval は「走っている subagent が終わったか」を覗きに行く間隔である。
//
// **hook の受け口（`hookCh`）は使わない。**`SubagentStart` / `SubagentStop` は turn の
// 終わりの判定に使わないので、そこへ流すと `confirmTurnEnd` の待ちが横取りして取りこぼす
// （設計 3-2 の `isTurnBoundaryHook`）。**覗くだけにする。**待つのは長くても
// `claude.poll_wait_ms` の1回ぶんなので、この間隔で起きる回数はたかが知れている。
const subagentPollInterval = 50 * time.Millisecond

// waitForRunningSubagents は、走っている subagent が終わるのを猶予いっぱいまで待つ
// （設計 3-11）。
//
// **待つ理由は1つだけである。「別の subagent が書き終えるのを待つ」ためである。**
// **`blocked` が解けるのを待っているのではない。**確認の画面は自分では消えないので、
// 待っても解けない。**「待てば復帰する」ものではない。**
// 引き渡しは通知の直後に pane を閉じるので、走っていたものは途中で終わる。
// **待たなければ、そのとき書きかけだった編集がまるごと消える**（報告の回は 4件中3件まで
// 書き終えていて、4件目で止まった）。
//
// **猶予は `claude.poll_wait_ms` である。**新しい設定は足さない。
// **長くしても救えない。**上のとおり `blocked` は解けないので、伸ばすぶんだけ
// 人間へ渡すのが遅れるだけである。
//
// **待ち終えた結果は返さない。**通知に載せる集合は、呼び出し側が esc の直前に
// `freezeHandoffSubagents` で凍結する。**ここで返した値を使うと、凍結の時点と
// 数え方がずれる。**
//
// ctx: 待ちを打ち切るコンテキスト。
// rs: 対象の run。
func (o *Orchestrator) waitForRunningSubagents(ctx context.Context, rs *runState) {
	running := rs.runningSubagentList()
	if len(running) == 0 {
		return
	}
	grace := time.Duration(o.cfg.Claude.PollWaitMs) * time.Millisecond
	if grace <= 0 {
		return
	}
	o.logger.Info("走っているサブエージェントが終わるのを待ってから esc を送ります",
		"identifier", rs.issue().Identifier, "サブエージェント", running, "猶予", grace)

	deadline := time.NewTimer(grace)
	defer deadline.Stop()
	tick := time.NewTicker(subagentPollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			if left := rs.runningSubagentList(); len(left) > 0 {
				o.logger.Warn("猶予のあいだにサブエージェントが終わらなかったので、走行中のまま esc を送ります",
					"identifier", rs.issue().Identifier, "サブエージェント", left)
			}
			return
		case <-tick.C:
			if len(rs.runningSubagentList()) == 0 {
				o.logger.Info("走っていたサブエージェントが終わったので esc を送ります",
					"identifier", rs.issue().Identifier)
				return
			}
		}
	}
}

// blockedHandoffReason は `blocked` で人間へ渡すときの理由の文面を組み立てる（設計 3-11）。
//
// **原因を断定しない。**何が確認の画面を出したかは continuo の側に残らない
// （`Notification` hook は出ず、拒否は静かに起きる）。書けるのは
// 「記録を見て確かめてください」までである。
//
// **走行中の subagent を止めたなら、そう書く。**引き渡しの直後に pane を閉じるので、
// 走っていたものは途中で終わる。**書かないと、人間は worktree に書きかけの変更が
// 残っていることに気づけない。**
//
// **名前は `handoffSubagentLimit` 件までしか並べない。**件数は最大
// `maxTrackedSubagents` の2倍（2つの申告を足し合わせるため）まで増えうるので、
// **全部並べるとコメントが名前で埋まる。**記録のパスと同じ上限で切り、
// 残りは件数だけ書く。**「動いていた件数」そのものは切らずに出す。**
//
// stillRunning: esc を送る時点でまだ走っていた subagent の名前の並び。空なら1件も無い。
// 戻り値: 引き渡しの通知に載せる理由。
func blockedHandoffReason(stillRunning []string) string {
	var b strings.Builder
	b.WriteString("Claude Code が作業の途中で確認の画面に止まりました。" +
		"continuo は esc を送って画面を閉じましたが、" +
		"**この issue は人間が見ないと進みません。**")
	if len(stillRunning) > 0 {
		shown := stillRunning
		omitted := 0
		if len(shown) > handoffSubagentLimit {
			omitted = len(shown) - handoffSubagentLimit
			shown = shown[:handoffSubagentLimit]
		}
		quoted := make([]string, 0, len(shown))
		for _, name := range shown {
			quoted = append(quoted, "`"+name+"`")
		}
		names := strings.Join(quoted, " / ")
		if omitted > 0 {
			names += fmt.Sprintf(" ほか %d 件", omitted)
		}
		b.WriteString(fmt.Sprintf(
			"\n【走行中のサブエージェントを止めました】esc を送った時点で %d 件が動いていました（%s）。"+
				"**worktree には書きかけの変更が残っている可能性があります。**"+
				"下記の【調べるところ】の worktree を確かめてください。",
			len(stillRunning), names))
	}
	b.WriteString("\n【確かめ方】下記の【調べるところ】に挙げた記録を開き、" +
		"末尾で何をしようとしていたかを見てください。" +
		"**サブエージェントの記録も見てください。**" +
		"親の記録の末尾には何も残っていないことがあります。" +
		"\n【よくある原因】herdr が `blocked`（確認の画面で入力を待っている状態）を返しました。" +
		"**何の確認だったかは continuo の側には残りません。**" +
		"\n【dontAsk について】continuo は `--permission-mode dontAsk` で起動しており、" +
		"許可の一覧に無いツールは確認を出さずにその場で拒否されるので、" +
		"**この停止は拒否とは別の原因のことがあります。**" +
		"\n【対処】記録を見て、許してよい操作だと分かったときだけ " +
		"WORKFLOW.md の `claude.permissions.allow` に足してください。" +
		"そのうえで Status を着手待ちへ戻してください。")
	return b.String()
}

// buildTurnText はこの turn で送る本文を決める（設計 3-8 / 5-3 / 5-4）。
//
// **分岐の基準は turn 数でも会話履歴の有無でもなく、`SendFirstPrompt` だけである。**
//
//	SendFirstPrompt が真  … 1回目の本文（5-3）。着手と再着手の最初の turn
//	SendFirstPrompt が偽  … 継続の指示（5-4）のみ。本文は送り直さない（`SPEC.md` 7.1）
//
// **会話履歴の有無で分岐してはならない**（設計 3-3b の「送る本文の選び分けに、会話履歴の
// 有無を使わない」。**同じ節が「起動フラグの選び分けには使う」とも書いているが、
// それは着手の段5b の話であり、ここには当てはまらない**）。再着手はセッションへ `--resume` で
// 復帰するので会話履歴があるが、**それでも送るのは1回目の本文である。**
// `In Review` から `In Progress` へ差し戻される場面では人間が PR にレビューを書いており、
// **「issue を読むこと」「紐づく PR も読むこと」が入っているのは1回目の本文だけだからである。**
// 会話は残っているので、エージェントは前回どこまでやったかを分かったうえでレビューを読める。
//
// **turn 数で分岐してもならない。**復元で引き継いだ run は turn 数を 1 から数え直すが
// （設計 3-4 の段7）、送るのは継続の指示である（段5c）。逆に、バックオフ明けの再着手は
// turn 数を引き継ぐが、送るのは1回目の本文である。
//
// **試行回数（`.attempt`）は再着手で埋まる。**1回目の着手では nil である
// （`RetryCount` が 0 のため）。
//
// rs: 対象の run。
// snap: 判定に使う写し。
// 戻り値の1つ目: 送る本文。
// 戻り値の2つ目: 1回目のテンプレートの変数展開に失敗した場合のエラー
// （`missingkey=error` なので、5-3 の一覧に無い変数を書くとここで落ちる）。
func (o *Orchestrator) buildTurnText(rs *runState, snap runSnapshot) (string, error) {
	if !snap.SendFirstPrompt {
		return BuildContinuationPrompt(
			snap.TurnCount+1,
			o.cfg.Agent.MaxDispatchTurns,
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
//     （**agent.prompt は再送しない。**二重に投入される）。
//     **枠待ちでなくても打ち切らない。**待ち直す（turn の総実行時間に上限は無い）
//  3. blocked で返ったら turnBlocked
//  4. idle / done で返ったら、Stop hook を受けているかを確かめる
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// text: 送る本文。
// 戻り値の1つ目: turn の結果。
// 戻り値の2つ目: **`turnSendFailed` と `turnTransient` のときだけ入る** herdr が返した
// 元のエラー。**これを捨ててはならない。**捨てると、issue に残す理由から本当の原因
// （`agent_not_found` / `agent_not_ready` / socket 断）が消える。
func (o *Orchestrator) sendTurn(ctx context.Context, rs *runState, text string) (turnOutcome, error) {
	turnCount := rs.beginTurn(o.now())
	o.logger.Info("turn を送ります",
		"identifier", rs.issue().Identifier, "turn", turnCount, "max_dispatch_turns", o.cfg.Agent.MaxDispatchTurns)

	res, err := o.herdr.AgentPrompt(ctx, herdr.AgentPromptParams{
		Target: rs.agentName(),
		Text:   text,
		Wait: &herdr.AgentWaitOptions{
			TimeoutMs: o.cfg.Claude.TurnTimeoutMs,
			Until:     waitUntilStatuses(o.cfg.Claude.WaitUntil),
		},
	})
	if err != nil {
		if outcome, handled := o.selfStoppedTurn(rs, err); handled {
			return outcome, nil
		}
		if ctx.Err() != nil {
			return turnAborted, nil
		}
		if herdr.IsCode(err, herdr.ErrCodeTimeout) {
			if outcome, waitErr := o.afterWaitTimeout(ctx, rs); outcome != turnWaitAgain {
				return outcome, waitErr
			}
			// **打ち切らずに待ち直す。**`agent.prompt` は再送しない（二重に投入される）。
			// 画面が止まったかどうかは巡回の stall 検知（checkStalls）だけが決める。
			return o.confirmTurnEnd(ctx, rs, false)
		}
		// **一時的な失敗なら run を捨てない**（herdr/errors.go の IsTransient の約束）。
		// herdr の再起動・socket の一瞬の不通・応答の遅れがこれに当たる。
		// **ここで諦めると、herdr を再起動しただけで走行中の run がリトライを消費し、
		// 使い切ると issue が failure_state へ落ちる。**
		if herdr.IsTransient(err) {
			o.logger.Warn("herdr へ届かなかったので、この turn は次の巡回で待ち直します（run は諦めません）",
				"identifier", rs.issue().Identifier, "error", err)
			return turnTransient, err
		}
		// **`turnStalled` を返してはならない。**turn は1文字も届いていない。
		// 「Stop hook が届かなかった」は、待ち受けが返ったあとにだけ言えることである。
		o.logger.Warn("turn を送れませんでした", "identifier", rs.issue().Identifier, "error", err)
		return turnSendFailed, err
	}

	switch res.Agent.AgentStatus {
	case herdr.AgentStatusBlocked:
		return turnBlocked, nil
	case herdr.AgentStatusIdle, herdr.AgentStatusDone:
		return o.confirmTurnEnd(ctx, rs, true)
	default:
		// working / unknown のまま返るのは想定外である。Stop を確かめてから判断する。
		return o.confirmTurnEnd(ctx, rs, true)
	}
}

// selfStoppedTurn は「その失敗は continuo 自身が worker を止めたせいか」を判定する
// （設計 3-51）。
//
// **`turn を送れませんでした（agent is no longer running）` は外の障害ではない。**
// continuo が1秒前に自分で `pane.close` を呼んだために起きている。**そのまま WARN で
// 出すと、読んだ人は herdr か Claude Code を疑って原因を探しにいく。**
//
// **止めたことと結び付けて1行だけ出し、run は諦めない。**この run は既に別の経路
// （引き渡し・打ち切り・知らない Status）で終わらせている最中である。
//
// rs: 対象の run。
// err: herdr が返したエラー。
// 戻り値の1つ目: 自分で止めていた場合の turn の結果（`turnAborted`）。
// 戻り値の2つ目: 自分で止めていたら true。偽なら呼び出し側が通常どおり分類する。
func (o *Orchestrator) selfStoppedTurn(rs *runState, err error) (turnOutcome, bool) {
	if !rs.stoppedByContinuo() {
		return turnAborted, false
	}
	o.logger.Info("continuo がこの worker を止めたので、待っていた turn はここで終わりにします",
		"identifier", rs.issue().Identifier, "herdr の応答", err)
	return turnAborted, true
}

// afterWaitTimeout は待ち受けが timeout で返ったときの分岐である（設計 3-2 の段3 / 3-27）。
//
// **枠待ちなら agent.wait で待ち直す。**`agent.prompt` は再送しない（二重に投入される）。
// **枠待ちでなければ `turnWaitAgain` を返す。**呼び出し側が待ち直す。
// **ここで turn を打ち切ってはならない。**`claude.turn_timeout_ms` は turn の総実行時間の
// 上限ではなく「画面が変わらないまま待てる時間」であり、その判定は巡回の checkStalls が持つ。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値の1つ目: turn の結果。枠待ちでなければ `turnWaitAgain`。
// 戻り値の2つ目: **`turnSendFailed` と `turnTransient` のときだけ入る** herdr が返した
// 元のエラー。
func (o *Orchestrator) afterWaitTimeout(ctx context.Context, rs *runState) (turnOutcome, error) {
	if !o.isQuotaWaiting(rs) {
		return turnWaitAgain, nil
	}

	// **ここでは手放さない**（人間の決定。2026-09-06。issue #197）。
	// **手放しの入口は巡回の1本だけである。**
	//
	// **なぜ待ちループから外したか。**手放すかどうかを決めるには、
	// **pane が止まっているか**（画面の版・`agent_status`・走っているサブエージェント）を読む必要がある。
	// **それを読むのは巡回だけである。**読まない側に手放させると、
	// **動いている run を、動いていることを確かめないまま止めることになる。**
	//
	// **待ちが遅れることはない。**ここでは枠待ちの印を立てるだけで、
	// **巡回は既定30秒ごとに回り、印の立った run を `releaseQuotaWaitExceeded` が拾う。**
	// **`claude.turn_timeout_ms` を0以下にしている機械でも取り残されない。**
	// `checkStalls` は無音の閾値による早い戻りより**前**に `releaseQuotaWaitExceeded` を呼ぶ。

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
			return turnAborted, nil
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
				return turnBlocked, nil
			case herdr.AgentStatusIdle, herdr.AgentStatusDone:
				if _, seen := rs.stopSeen(); seen {
					rs.clearWaitingQuota(o.now())
					return o.confirmTurnEnd(ctx, rs, true)
				}
			}
		} else if !herdr.IsCode(err, herdr.ErrCodeTimeout) {
			if outcome, handled := o.selfStoppedTurn(rs, err); handled {
				return outcome, nil
			}
			if ctx.Err() != nil {
				return turnAborted, nil
			}
			// **一時的な失敗なら run を捨てない**（sendTurn と同じ判断）。
			//
			// **枠待ちの印は外さない。**枠が明けたかどうかは分かっていない。外すと
			// stall の時計が動き出し、枠が明けるより先に stall として諦めることになる。
			// 印は checkStalls が `resets_at` を過ぎた時点で外す（設計 3-27）。
			if herdr.IsTransient(err) {
				o.logger.Warn("枠待ちの待ち直しが herdr へ届かないので、次の巡回で待ち直します（run は諦めません）",
					"identifier", rs.issue().Identifier, "error", err)
				return turnTransient, err
			}
			// **これも「送れなかった」側である。**`agent.wait` が答えないことと、
			// Stop hook が届かないことは別の話なので、`turnStalled` に混ぜない。
			o.logger.Warn("枠待ちの待ち直しに失敗しました", "identifier", rs.issue().Identifier, "error", err)
			rs.clearWaitingQuota(o.now())
			return turnSendFailed, err
		}

		// 枠が明けたか。**標識を外す契機は2つある**（設計 3-27）。
		// **1つ目は `resets_at` を過ぎたこと。**2つ目は下の `quotaFull` で見る。
		// **2つ目を落としてはならない。**`resets_at` が `null` の枠だけが満杯だと、
		// **1つ目では永久に外れない。**
		if ok && !o.now().Before(resetAt) {
			rs.clearWaitingQuota(o.now())
			return o.afterQuotaReset(ctx, rs)
		}
		o.pollQuota(ctx)
		if !o.quotaFull() {
			rs.clearWaitingQuota(o.now())
			return o.afterQuotaReset(ctx, rs)
		}

		// **下限の待ち。**`agent.wait` が即返っても1周あたり poll_wait_ms は必ず空ける。
		if rest := pollWait - o.now().Sub(iterationStart); rest > 0 {
			if !sleepCtx(ctx, rest) {
				return turnAborted, nil
			}
		}
	}
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
// 戻り値の1つ目: turn の結果。`turnQuotaRecovered` なら turn ループが次の turn を送る
// （**この送信は turn 数に数える。**設計 3-27。数えないと、枠待ちと復帰を繰り返す間に
// max_dispatch_turns が一度も発火しない）。
// 戻り値の2つ目: **`turnSendFailed` のときだけ入る** herdr が返した元のエラー。
func (o *Orchestrator) afterQuotaReset(ctx context.Context, rs *runState) (turnOutcome, error) {
	status, err := o.agentStatus(ctx, rs)
	if err != nil {
		o.logger.Warn("枠明けの agent_status を読めません（継続の指示を送ります）",
			"identifier", rs.issue().Identifier, "error", err)
		return turnQuotaRecovered, nil
	}

	switch status {
	case herdr.AgentStatusBlocked:
		return turnBlocked, nil
	case herdr.AgentStatusWorking:
		// **Claude Code が自分で継続している。**送らずに hook を待つ（設計 3-27）。
		// Claude Code 2.1.234 以降、枠のリセット時にセッションを自動継続する機能が
		// 既定で有効である。そのまま送ると二重投入になり、投げた本文が消えて turn が混ざる。
		// **その turn は continuo が送っていないので turn 数に入らない。**
		o.logger.Info("枠が明けて Claude Code が自分で継続しました（継続の指示は送りません）",
			"identifier", rs.issue().Identifier)
		return o.confirmTurnEnd(ctx, rs, false)
	default:
		o.logger.Info("枠が明けたので継続の指示を1回送ります（この送信は turn 数に数えます）",
			"identifier", rs.issue().Identifier)
		return turnQuotaRecovered, nil
	}
}

// turnQuotaRecovered は枠が明けたので継続の指示を送り直すべきことを表す。
//
// **turnLoop はこれを受けると次の turn を送る**（TurnCount が1つ増える。設計 3-27）。
const turnQuotaRecovered turnOutcome = 100

// turnWaitAgain は「待ち受けが timeout で返ったが、打ち切らずに待ち直す」ことを表す。
//
// **`afterWaitTimeout` だけが返し、その呼び出し元だけが受け取る。**turnLoop まで
// 届くことはない（届いたら turn を二重に送ることになるので、呼び出し元は必ず消費すること）。
const turnWaitAgain turnOutcome = 101

// turnTransient は herdr の呼び出しが**一時的な理由で**失敗したことを表す
// （`herdr.IsTransient` が真。herdr の再起動・socket の一瞬の不通・応答の遅れ）。
//
// **`turnSendFailed` と混ぜてはならない。**混ぜると、herdr を再起動しただけで
// 走行中の run が諦められ、リトライを消費し、使い切ると issue が failure_state へ落ちる。
// herdr は何も答えていないのに「herdr は agent が待機状態になったと答えました」という
// 文面がカンバンへ投稿される。
//
// **turnLoop はこれを受けても run を諦めない。**`awaitTurnEnd` を立てて抜け、
// 次の巡回で「turn を送らずに turn の終わりを待つ」ところから入り直す（設計 3-48）。
const turnTransient turnOutcome = 102

// turnStopUnreadable は「`Stop` は届いたが `background_tasks` の項目が欠けていて、
// turn が終わったのかどうかを決められなかった」ことを表す（設計 3-2 の「判定不能」）。
//
// **`turnStalled` と混ぜてはならない。**混ぜると、hook は届いていたのに
// 「Stop hook から continuo へ通知が届きませんでした」という**事実と逆の文面**が
// issue に残り、読んだ人は届いていた hook の配線を疑って探しにいく。
const turnStopUnreadable turnOutcome = 103

// isQuotaWaiting は「この run は枠待ちである」を判定する（設計 3-27）。
//
// **2条件の連言である。**
//
//	条件その1  使い切っている枠がある（使用率100。`handoff.Full`）
//	条件その2  その run から claude.turn_timeout_ms のあいだ hook が1件も来ていない
//
// **条件その1 を「余裕値が0以下」へ移した時期があったが、取り下げた**
// （2026-09-06。6段の段4 で issue #197 のコメントへ記録した）。
// **使用率90%では Claude Code は普通に応答する。**そこで打ち切りの時計を止めると、
// **本当に固まった run が、5時間の枠が90%を割るまで殺されない。**
// **既定では最大で6時間、スロットと pane を握り続ける。**
//
// **1週間の枠を待つ上限の判定は、この印に紐づいていない。**あちらは余裕値で効く
// （`releaseQuotaWaitExceeded`）。**同じ問いではないので、線も別である。**
//
// **`severity` は見ない。**上限を示す値が何かを実測できていない。
//
// rs: 判定する run。
// 戻り値: 枠待ちなら true。
func (o *Orchestrator) isQuotaWaiting(rs *runState) bool {
	snap, _ := o.quotaSnapshotWithStale()
	return o.isQuotaWaitingWith(snap, rs)
}

// isQuotaWaitingWith は、渡された写しで枠待ちかどうかを判定する（設計 3-27。issue #197）。
//
// **巡回はこちらを使う。**`isQuotaWaiting` は run ごとに写しを取り直すので、
// **同じ巡回の中で run ごとに違う答えが返る**（`pollQuota` は turn の goroutine から
// 並行に走り、途中で `o.quota` を差し替える）。
//
// quotaSnap: この巡回で1回だけ読んだ枠の写し。
// rs: 判定する run。
// 戻り値: 枠待ちなら true。
func (o *Orchestrator) isQuotaWaitingWith(quotaSnap *ratelimit.Snapshot, rs *runState) bool {
	if !quotaSnap.AnySelected(handoff.Full()) {
		return false
	}
	// **条件その2。**枠を使い切っていても、別の run は動いていることがある。
	// 枠の状態だけで全部の run の時計を止めると、固まった run を見逃す。
	return o.runIdleForTurnTimeout(rs)
}

// runIdleForTurnTimeout は「その run から `claude.turn_timeout_ms` のあいだ
// hook が1件も来ていないか」を返す（設計 3-27。issue #197）。
//
// **枠を1バイトも見ない。**「この run は進んでいるか」だけを答える。
//
// **2箇所で使う。**
//
//	枠待ちの印を立てるか       … isQuotaWaitingWith の条件その2
//	1週間の枠の上限で手放すか   … releaseQuotaWaitExceeded
//
// **手放しの側にも要る。**pane の見た目だけで「止まっている」と決めると、
// **turn と turn のあいだのふつうの間や、進捗のコメントを書いている最中の run まで拾う。**
//
// **この turn で hook を1件も受けていない run は、無音の長さを見ずに真を返す。**
// **`LastSeenAt` は turn を始めた時刻のままなので、そこから測っても意味が無い。**
//
// rs: 判定する run。
// 戻り値: 進んでいなければ true。
func (o *Orchestrator) runIdleForTurnTimeout(rs *runState) bool {
	snap := rs.snapshot()
	if !snap.hookSeenThisTurn {
		return true
	}
	silence := time.Duration(o.cfg.Claude.TurnTimeoutMs) * time.Millisecond
	if silence <= 0 {
		// **0 以下は「無音では打ち切らない」という設定である**（`SPEC.md` 8.4）。
		// **測る物差しが無いので、この関数は「進んでいない」と言えない。**
		return false
	}
	return o.now().Sub(snap.LastSeenAt) >= silence
}

// stallDetectionOff は、無音による打ち切りを切っているかを返す（issue #197）。
//
// **切っている機械では `runIdleForTurnTimeout` が「進んでいない」を言えない。**
// **手放しの側は、そのとき画面の版と `agent_status` だけで判断する。**
// **言えないことを理由に手放さないでいると、`weekly_wait_limit_minutes` が
// その設定の機械で一度も効かない。**
//
// 戻り値: 打ち切りを切っていれば true。
func (o *Orchestrator) stallDetectionOff() bool {
	return time.Duration(o.cfg.Claude.TurnTimeoutMs)*time.Millisecond <= 0
}

// quotaFull は使い切っている枠があるかを返す（設計 3-27 の条件その1）。
//
// **線は「使用率100」である。**入札の線（余裕値が0以下）とは別物である。
// **理由は `isQuotaWaiting` の説明にある。**
//
// 戻り値: 使用率100の枠があれば true。枠を読めていなければ false。
func (o *Orchestrator) quotaFull() bool {
	return o.quotaSnapshot().AnySelected(handoff.Full())
}

// quotaResetAt は枠待ちの印を外す時刻を返す（設計 3-27 の「どの枠の時刻を見るか」）。
//
// **条件その1（使い切っている枠）を満たしたもののうち、`resets_at` がいちばん遅いものである。**
// **`resets_at` が null の枠は黙って飛ばす。**印を外す契機はもう1つあり
// （余裕の無い枠が1つも無くなること）、**そちらが受け持つ。**
//
// 戻り値の1つ目: 外す時刻。
// 戻り値の2つ目: 時刻が分かれば true。
func (o *Orchestrator) quotaResetAt() (time.Time, bool) {
	snap, _ := o.quotaSnapshotWithStale()
	return o.quotaResetAtOf(snap)
}

// quotaResetAtOf は、渡された写しから枠待ちの印を外す時刻を返す（設計 3-27）。
//
// **巡回はこちらを使う。**理由は `isQuotaWaitingWith` と同じである。
//
// quotaSnap: この巡回で1回だけ読んだ枠の写し。
// 戻り値の1つ目: 外す時刻。
// 戻り値の2つ目: 時刻が分かれば true。
func (o *Orchestrator) quotaResetAtOf(quotaSnap *ratelimit.Snapshot) (time.Time, bool) {
	return quotaSnap.LatestResetForClearing(handoff.Full())
}

// confirmTurnEnd は turn の終わりを確定させる（設計 3-2 の hook 側の規則）。
//
//	background_tasks が空でない        … まだ動いている。turn の終わりとして扱わない。
//	                                    **待ち時間を仕切り直して待ち直す**
//	background_tasks の項目が欠けている  … 判定不能。turn の終わりとみなさない
//	background_tasks が空配列          … settle_ms のあいだ待ち、
//	                                    `<task-notification>` も「空でない Stop」も
//	                                    来なければ turn の終わりとする
//
// **空でない `Stop` を捨ててはならない。**捨てると、待ち時間が尽きた時点で
// 「Stop hook が届かなかった」として pane を閉じる。**「まだ動いています」と
// 名乗ってきた相手を、その2秒後に殺すことになる。**
//
// **`Stop` が1件も来ていなければ、settle_ms 待ってから stall として扱う**
// （権限の確認が esc で取り消された場合など。設計 3-11）。
// **届いたのに読めなかった場合は `turnStopUnreadable` で、届かなかった場合と書き分ける。**
//
// **`Stop` が来るまで何時間でも待つ。**turn の総実行時間に上限は無い（`SPEC.md` 10.6）。
// 画面が止まったかどうかは巡回の stall 検知（checkStalls）だけが決める。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// strictFirstWait: 「待ち受けが返った直後」なら真。真のときは、settle_ms のあいだに
// `Stop` が来なければ stall として扱う。**枠明けに Claude Code が自分で継続した場合は
// 偽を渡す**（まだ走り出したばかりで `Stop` は来ないため。設計 3-27）。
// 戻り値の1つ目: turn の結果。
// 戻り値の2つ目: **`turnSendFailed` と `turnTransient` のときだけ入る** herdr が返した
// 元のエラー（枠待ちの待ち直しが答えなかった場合。それ以外では nil）。
func (o *Orchestrator) confirmTurnEnd(
	ctx context.Context,
	rs *runState,
	strictFirstWait bool,
) (turnOutcome, error) {
	settle := time.Duration(o.cfg.Claude.SettleMs) * time.Millisecond
	pollWait := time.Duration(o.cfg.Claude.PollWaitMs) * time.Millisecond
	// firstWait が真の間は「待ち受けが返った直後」である。ここで Stop が来なければ stall。
	firstWait := strictFirstWait
	// rewriteWait が真の間は「空の `Stop` を受けたが herdr がまだ `working` を返したので
	// 待ち直している」最中である（設計 3-79）。
	//
	// **これは推測である。**`background_tasks` が空でない `Stop` は Claude Code 自身の
	// 申告だが、こちらは herdr の見え方から「差し戻されて書き直している」と当てているだけで、
	// **書き直し以外の理由で `working` に見えることがありうる**（遅い `Stop` hook が
	// まだ走っている、など）。**当たっているうちは新しい `Stop` が必ず来る。**
	// **待ち直している間は `settle_ms` ごとに `agent.get` を読む。**新しい `Stop` が
	// 来ないまま、そのときエージェントが動いていなければ、
	// **推測が外れているので turn の終わりとして進む。**
	rewriteWait := false

	for {
		if ctx.Err() != nil {
			return turnAborted, nil
		}

		stopAt, seen := rs.stopSeen()
		if !seen {
			// **書き直しを待っている間だけは settle_ms で刻む**（設計 3-79）。
			//
			// **ここで `poll_wait_ms`（既定30秒）を待つと、遅い `Stop` hook を持つ
			// 利用者は毎 turn ちょうど30秒を捨てる。**`settle_ms`（既定2秒）より遅い
			// hook は、その窓が閉じる時点でまだ走っており、herdr からは `working` に
			// 見える。**だが差し戻してはいないので、新しい `Stop` は二度と来ない。**
			// 下の出口（`rewriteWait` が真のまま動いていなかったとき）が回ってくるのを
			// 待つだけになる。`max_dispatch_turns`（既定20）を掛けると1 run あたり10分である。
			//
			// **刻んでも本物の書き直しを取り逃がさない。**
			// [docs/evidence/stop_hook_block_20260902.md](../../docs/evidence/stop_hook_block_20260902.md)
			// は 0.1 秒ごとに `agent.get` を読み、**書き直しの最中に `idle` が返った
			// 瞬間は1度も無かった**と記録している。**費用も無視できる**
			// （同じ記録で n=168、中央 1.13ms）。
			patience := settle
			if !firstWait && !rewriteWait {
				// `<task-notification>` を受けたあとは turn が続いている最中である。
				// ここで settle_ms しか待たないと、正常な turn を stall と誤判定する。
				patience = pollWait
			}
			got := o.awaitStop(ctx, rs, patience)
			if got == stopWaitRunning {
				// **まだ動いている**（設計 3-2 / 1-7）。**この `Stop` を捨てて
				// 待ち時間を使い切ってはならない。**捨てると settle_ms の後に
				// 「Stop hook が届かなかった」として pane を閉じることになり、
				// **「まだ動いています」と名乗った2秒後に殺す。**
				//
				// **待ち直す。**待っていれば `background_tasks` が空の `Stop` が来る
				// （設計 1-7）。**総時間では打ち切らない。**打ち切るかどうかは
				// 巡回の stall 検知（checkStalls）だけが決める。
				o.logger.Info("バックグラウンド処理が残っていると名乗る Stop を受けたので、turn の終わりとせずに待ち直します",
					"identifier", rs.issue().Identifier)
				firstWait = false
				rewriteWait = false
				continue
			}
			if got != stopWaitEmpty {
				if firstWait {
					// **ここだけが「Stop の届き方」を issue に書いてよい場所である。**
					// herdr の待ち受けが返ったあとで、実際に見たことしか書けない。
					if got == stopWaitUnreadable {
						// **届いてはいる。**「届かなかった」と書くと事実と逆になる。
						return turnStopUnreadable, nil
					}
					return turnStalled, nil
				}
				// **枠待ちなら時計を止めて待ち直す**（設計 3-27）。
				// **総時間では打ち切らない。**打ち切るかどうかは checkStalls が決める。
				if o.isQuotaWaiting(rs) {
					return o.afterWaitTimeout(ctx, rs)
				}
				st, stErr := o.agentStatus(ctx, rs)
				if stErr == nil && st == herdr.AgentStatusBlocked {
					return turnBlocked, nil
				}
				if rewriteWait && (stErr != nil || st != herdr.AgentStatusWorking) {
					// **書き直しを当てにいったが、外れていた**（設計 3-79）。
					// 新しい `Stop` は来ず、エージェントも動いていない。
					// **ここで待ち続けると、巡回の stall 検知が `turn_timeout_ms`
					// （既定1時間）で拾うまで run が空転する。**
					o.logger.Info("書き直しを待ちましたが Stop も来ずエージェントも動いていないので、turn の終わりとします",
						"identifier", rs.issue().Identifier, "agent_status", string(st))
					return turnEnded, nil
				}
				continue
			}
			stopAt, _ = rs.stopSeen()
		}
		rewriteWait = false

		// settle_ms のあいだ「turn がまだ続いている」しるしが来ないことを確かめる
		// （設計 1-3 / 3-2）。
		//
		// **この窓でも「まだ動いている」と名乗る `Stop` を捨ててはならない**（設計 3-2）。
		// `<task-notification>` だけを待つと、空の `Stop` の直後に届いた
		// 「`background_tasks` が空でない `Stop`」が読み捨てられ、settle_ms の経過で
		// turn の終わりとして pane を閉じる。**issue #77 が塞いだのと同じ形である。**
		remaining := settle - o.now().Sub(stopAt)
		ev, got := o.awaitHook(ctx, rs, remaining, turnContinues)
		if !got {
			if !o.stillWorkingAfterStop(ctx, rs) {
				return turnEnded, nil
			}
			// **差し戻されて応答を書き直している最中である**（設計 3-79）。
			// 空の `Stop` は「turn が終わった」ではなく「止まってよいか hook に尋ねた」で、
			// **尋ねられた hook の答えは continuo に届かない。**
			o.logger.Info("空の Stop のあともエージェントが動いているので、turn の終わりとせずに待ち直します",
				"identifier", rs.issue().Identifier)
			rs.clearStopSeen()
			firstWait = false
			rewriteWait = true
			continue
		}
		// 来た。turn は続いている。待ち直す。
		if isRunningStop(ev) {
			o.logger.Info("空の Stop のあとにバックグラウンド処理が残ると名乗る Stop を受けたので、turn の終わりとせずに待ち直します",
				"identifier", rs.issue().Identifier)
		}
		rs.clearStopSeen()
		firstWait = false
	}
}

// stopWait は「`Stop` を待った結果」である（設計 3-2 の hook 側の規則）。
//
// **3つの `Stop` を1つに潰してはならない。**潰すと、まだ動いていると名乗ってきたものと
// 1件も届かなかったものが同じ扱いになり、issue に事実と逆の理由が残る。
type stopWait int

const (
	// stopWaitNone は `Stop` が1件も届かなかったことを表す。
	stopWaitNone stopWait = iota
	// stopWaitUnreadable は `Stop` は届いたが `background_tasks` の項目が欠けていたことを
	// 表す（判定不能）。**届いてはいる。**
	stopWaitUnreadable
	// stopWaitRunning は `background_tasks` が空でない `Stop` が届いたことを表す
	// （まだ動いている。設計 3-2 / 1-7）。
	stopWaitRunning
	// stopWaitEmpty は `background_tasks` が空配列の `Stop` が届いたことを表す
	// （turn の終わりの判定の起点）。
	stopWaitEmpty
)

// awaitStop は run の受け口から `Stop` が届くのを待ち、その中身を見分ける（設計 3-2）。
//
// **`background_tasks` の項目が欠けた `Stop` では待つのをやめない。**判定不能なので、
// そこで返しても呼び出し側は何も決められない。**届いたことだけを覚えて待ち続け、
// 待ち時間が尽きたときに `stopWaitUnreadable` として返す。**こうすることで、
// 「届かなかった」と「届いたが読めなかった」を issue の文面で書き分けられる。
//
// **`Stop` 以外の hook は読み捨てる。**`<task-notification>` の待ちは `awaitHook` が持つ。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// d: 待つ長さ。0以下なら、既に届いているぶんだけを見る。
// 戻り値: 待った結果。
func (o *Orchestrator) awaitStop(ctx context.Context, rs *runState, d time.Duration) stopWait {
	if d < 0 {
		d = 0
	}
	result := stopWaitNone
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case ev := <-rs.hookCh:
			switch {
			case isEmptyStop(ev):
				return stopWaitEmpty
			case isRunningStop(ev):
				return stopWaitRunning
			case ev.HookEventName == hookStop:
				// `background_tasks` の項目が欠けている。**判定不能なので待ち続ける。**
				result = stopWaitUnreadable
			}
		case <-timer.C:
			return result
		case <-ctx.Done():
			return result
		}
	}
}

// awaitHook は run の受け口から、条件に合う hook が届くのを待つ。
//
// **合致した hook そのものを返す。**呼び出し側は「何が来たから待ち直すのか」を
// 区別できないと、`<task-notification>` と「まだ動いていると名乗る `Stop`」を
// 同じ扱いにしてしまう（設計 3-2）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// d: 待つ長さ。0以下なら、既に届いているぶんだけを見る。
// pred: 合致を判定する関数。
// 戻り値の1つ目: 合致した hook（届かなかった場合はゼロ値）。
// 戻り値の2つ目: 合致する hook が届けば true。
func (o *Orchestrator) awaitHook(
	ctx context.Context,
	rs *runState,
	d time.Duration,
	pred func(hookserver.HookEvent) bool,
) (hookserver.HookEvent, bool) {
	if d < 0 {
		d = 0
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case ev := <-rs.hookCh:
			if pred(ev) {
				return ev, true
			}
		case <-timer.C:
			return hookserver.HookEvent{}, false
		case <-ctx.Done():
			return hookserver.HookEvent{}, false
		}
	}
}

// turnContinues は「turn がまだ終わっていない」と分かる hook かを判定する（設計 3-2）。
//
// **2つある。**どちらも「空の `Stop` を受けたあとの settle_ms の窓」で捨ててはならない。
//
//	<task-notification> の UserPromptSubmit … 次の turn が始まっている（1-3）
//	background_tasks が空でない Stop        … まだ動いていると名乗っている（1-7）
//
// ev: 判定する hook。
// 戻り値: どちらかに当たれば true。
func turnContinues(ev hookserver.HookEvent) bool {
	return isTaskNotification(ev) || isRunningStop(ev)
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

// isRunningStop は「background_tasks が空でない Stop」かを判定する（設計 3-2 / 1-7）。
//
// **これは Claude Code 自身の「まだ動いています」という申告である。**
// turn の終わりとして扱わず、待ち直す材料にする。
//
// **項目が欠けている（nil）ときは偽である。**申告が無いだけで、動いているとは限らない。
//
// ev: 判定する hook。
// 戻り値: 空でない配列を持つ Stop なら true。
func isRunningStop(ev hookserver.HookEvent) bool {
	return ev.HookEventName == hookStop && ev.BackgroundTasks != nil && len(*ev.BackgroundTasks) > 0
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

// agentInfo は agent.get を1回呼び、agent の情報をまるごと返す。
//
// **状態と画面の版（`revision`）を1回の呼び出しで取るためにある。**stall の判定は
// 両方を要る（設計 3-21）ので、2回に分けて呼ぶと別の時点の値を突き合わせることになる。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値の1つ目: agent の情報（状態は AgentStatus、画面の版は Revision）。
// 戻り値の2つ目: 読めなかった場合のエラー。
func (o *Orchestrator) agentInfo(ctx context.Context, rs *runState) (herdr.Agent, error) {
	got, err := o.herdr.AgentGet(ctx, herdr.AgentGetParams{Target: rs.agentName()})
	if err != nil {
		return herdr.Agent{AgentStatus: herdr.AgentStatusUnknown}, err
	}
	return got.Agent, nil
}

// agentStatus は agent の状態を1回読む。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値の1つ目: agent の状態。
// 戻り値の2つ目: 読めなかった場合のエラー。
func (o *Orchestrator) agentStatus(ctx context.Context, rs *runState) (herdr.AgentStatus, error) {
	agent, err := o.agentInfo(ctx, rs)
	return agent.AgentStatus, err
}

// stillWorkingAfterStop は、空の `Stop` から `settle_ms` 待ったあともエージェントが
// 動いているかを herdr へ1回だけ尋ねる（設計 3-79）。
//
// **空の `Stop` は「turn が終わった」ではない。**「止まってよいか `Stop` hook に尋ねた」
// である。止まってよいかを決めるのは hook のほうで、**その答えは continuo に届かない。**
// hook が `{"decision":"block"}` を返すと、Claude Code は turn を終わらせずに応答を
// 書き直す。**書き直しの間、herdr から見たエージェントは `working` である**
// （[docs/evidence/stop_hook_block_20260902.md](../../docs/evidence/stop_hook_block_20260902.md)
// の実測。Stop hook が8秒かかる場合も含め、投入から書き直しの終わりまで `working` のまま）。
//
// **`working` のときだけ真を返す。**読めなかったときは偽を返して従来どおり進む。
// **ここで待ちに倒すと、herdr が答えない間ずっと turn が終わらなくなる。**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値: まだ動いていれば true。
func (o *Orchestrator) stillWorkingAfterStop(ctx context.Context, rs *runState) bool {
	if rs.agentName() == "" {
		// **agent の名前をまだ持っていない run である**（引き継いだ run で、
		// 復元が名前を埋める前に `Stop` が届いた場合。3-4 の段5a2）。
		// **聞く相手がいないので、聞かずに従来どおり進む。**
		// **WARN では出さない。**答えられないことが分かっているものを毎回警告に出すと、
		// 本当に herdr が答えなくなったときの1行が埋もれる。
		o.logger.Debug("agent の名前がまだ無いので、turn の終わりの裏取りをしません",
			"identifier", rs.issue().Identifier)
		return false
	}
	st, err := o.agentStatus(ctx, rs)
	if err != nil {
		o.logger.Warn("turn の終わりの裏取りができませんでした（turn の終わりとして進みます）",
			"identifier", rs.issue().Identifier, "error", err)
		return false
	}
	return st == herdr.AgentStatusWorking
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
