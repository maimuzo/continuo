package orchestrator

import (
	"context"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// maxHandoffFetchesPerPoll は、1回の巡回で持ち回りのためにコメントを読む issue の数の上限である
// （設計 3-77a）。
//
// **上限を置かないと、巡回が返らなくなる。**担当者のいない候補1件につき
// `FetchAllComments` が走り、コメントの多い issue では最大 maxCommentPages 回の
// GraphQL になる。**入札に負け続ける機械は空きスロットを埋めないので、
// `dispatchCandidates` のループは候補の最後まで止まらない。**
// 104件のカンバンでは、30秒の巡回1回が数百リクエストになる。
//
// **使い切ったらその巡回は打ち切る。**候補はカンバンの並び順で来るので、
// **上から順に見ることは保たれる。**次の巡回で続きを見る。
const maxHandoffFetchesPerPoll = 10

// quotaReleaseWriteBudget は、1週間の枠の上限で担当を手放すときに GitHub へ書く上限である
// （設計 3-27。issue #197）。
//
// **`herdr.read_timeout_ms`（既定5秒）を使い回さない。**あちらは socket の応答を待つ上限で、
// **GraphQL を2本（担当者を外す・`released` を書く）投げるには足りない。**初回は
// `gh` の持ち主の取得も乗る。**足りないと毎巡回でやり直し、run はスロットと pane を握ったまま残る。**
//
// **30秒にする。**`internal/ratelimit` が HTTP の全体の上限に使っている値と同じで、
// **GitHub が遅いときの1回ぶんとして実測のある長さである。**
const quotaReleaseWriteBudget = 30 * time.Second

// quotaReleaseCheckBudget は、手放してよいかを確かめるあいだの上限である（issue #197）。
//
// **段0 は `gh` を2回叩く**（`viewerIdentity` と `refreshIssue`）。
// **`context.WithoutCancel` で切られないようにしてあるので、上限を付けないと期限が1つも無くなる。**
// **`gh` が死んだ回線で固まると、この goroutine は永久に返らない。**
// `releaseBecauseQuotaWaitAsync` が `wg.Add(1)` しているので、
// **`Close` の `wg.Wait()` もそこで永久に止まり、continuo が終われなくなる。**
//
// **書き込みの側（`quotaReleaseWriteBudget`）と同じ値にしてある。**
// 読み取り2回なので、書き込み2回より長くかかる理由が無い。
const quotaReleaseCheckBudget = 30 * time.Second

// handoffDecision は handoffGate の答えである。
type handoffDecision struct {
	// proceed は、この機械が着手してよいかである。
	proceed bool
	// acquired は、**この巡回でこの機械が担当者になった**かである。
	//
	// **着手を取りやめるときに、書いた担当者を消し戻すために持つ**（設計 3-77c）。
	// 消し戻さないと、着手しなかった issue に担当者と hold だけが残り、
	// **ほかの機械は「期限内の担当」と読んで idle_timeout のあいだ触らない。**
	acquired bool
	// stop は、この巡回でこれ以上候補を見ないかである（コメントを読む予算を使い切った）。
	stop bool
}

// handoffGate は「この機械がこの issue を処理してよいか」を、着手の前に決める
// （設計 3-77 / 3-77a / 3-77b / 3-77c）。
//
// **見るのは担当者（assignee）とコメントの全件だけである。**カンバンに新しい欄は足さない。
//
//	担当者が2人以上                                触らない。WARN を出す
//	担当者が無い                                   入札する。勝ったら自分を担当者に加えて hold を書く
//	自分のアカウント1人                            そのまま着手・引き継ぎへ進む（入札しない）
//	他人1人 ＋ hold が1件も無い                     触らない。人間が付けた担当である。WARN を出す
//	他人1人 ＋ hold あり ＋ 期限内                   触らない。入札もしない
//	他人1人 ＋ hold あり ＋ 期限切れ                 担当を外し、released を書いてから入札をやり直す
//
// **担当者のアカウントが自分なら、担当しているのも自分である**（設計 3-77-0）。
// **同じ GitHub アカウントを複数の機械で使うことはサポートしない**ので、
// **アカウント1つにつき continuo は1つである。**
//
// **締め切りを待つあいだ、巡回はブロックしない。**入札を1件書いたら偽を返して次の巡回へ譲り、
// 締め切りが過ぎた巡回で勝敗を決める。**締め切りは issue のコメントから読める**
// （いちばん古い入札の時刻 + `bid_window_ms`）ので、待ちを記憶に持たなくてよい。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 着手しようとしている issue。
// 戻り値: 着手してよいか・この巡回で担当者になったか・この巡回を打ち切るか。
func (o *Orchestrator) handoffGate(ctx context.Context, issue tracker.Issue) handoffDecision {
	nodeID := issueNodeID(issue)
	if nodeID == "" {
		// **draft issue にはコメントも担当者も書けない。**そもそも Dispatchable が偽なので
		// ここへは来ないが、来たら止めない（判定できないものを判定したことにしない）。
		return handoffDecision{proceed: true}
	}

	// **戻り口は9つある。1つずつ後始末を書かない**（設計 6-2）。
	//
	// judged は「この巡回でこの issue を判定できたか」である。**既定は真。**
	// **判定できなかった経路だけが偽を入れる**（gh の持ち主を取れない・
	// 読み取りの枠を使い切った・コメントを読めない）。そこで記録を消すと、
	// 材料が揃わなかっただけの巡回で数え直しになる。
	// noted は「担当者の関門で止めたか」である。真なら noteGate が記録を書き直している。
	judged := true
	noted := false
	defer func() {
		if judged && !noted {
			o.clearGate(issue.ID)
		}
	}()

	logins := assigneeLogins(issue)
	// **コメントを読まずに答えが出るものを先に処理する。**
	//
	// **コメントの取得は issue 1件につき1本以上の GraphQL である。**候補が多いカンバンで
	// 全件に掛けると、巡回1回のリクエストが候補の数だけ増える（設計 3-31）。
	if len(logins) >= 2 {
		o.logger.Warn("担当者が2人以上いるので触りません（人間が触っています）",
			"identifier", issue.Identifier, "担当者の人数", issue.AssigneeCount,
			"担当者", strings.Join(logins, ", "))
		// **gh の持ち主が混じっていないことを先に確かめる**（設計 8-3）。
		// **混じっているなら、その1人はこの continuo が自分で書いた担当者である。**
		// そこで「担当者をすべて外してください」と案内すると、
		// **人間は、いま走っているこの continuo の担当まで外すことになる。**
		// **外れると、この issue はほかのアカウントから「担当者のいない issue」に見え、
		// 入札で別の continuo が取りに来る。**同じ branch に2つの作業が乗る。
		//
		// **`viewerIdentity` は一度取れたら覚える**ので、定常状態でリクエストは0本である。
		// **読み取りの枠（maxHandoffFetchesPerPoll）はコメントの取得だけを数えるので、1件も使わない。**
		viewer, ok := o.viewerIdentity(ctx)
		switch {
		case !ok:
			// **gh の持ち主が分からない。**別の機械の担当かどうかを切り分けられないので、
			// 案内も記録も作らない。**記録は消さない**（判定していない。設計 6-2）。
			judged = false
		case containsFold(logins, viewer.Login):
			// **continuo のアカウントが混じっている。**別の機械が担当している見込みなので、
			// **issue へは1バイトも書かない**（設計 8-3）。
			// **記録は作る。**いちばん切り分けの難しい状態を、ダッシュボードから消さない。
			noted = true
			if o.noteGate(issue, GateReasonManyAssigneesWithSelf, logins) {
				o.markGateNoticeSkipped(issue.ID, GateReasonManyAssigneesWithSelf, GateNoticeUnclearOwner)
			}
		default:
			noted = true
			if o.noteGate(issue, GateReasonManyAssignees, logins) {
				o.postGateNotice(ctx, issue, nodeID, GateReasonManyAssignees)
			}
		}
		return handoffDecision{}
	}

	// **入札できない機械は、担当者のいない issue のコメントを読まない**（設計 3-77a）。
	// 枠を読めない・枠を使い過ぎた・余裕値がマイナス、のどれかなら、この issue で
	// **できることは「黙る」だけである。**読んでから黙るのは、リクエストの無駄でしかない。
	bid, skip := o.evaluateBid()
	if len(logins) == 0 && skip != handoff.SkipNone {
		o.logger.Debug("入札しません（この issue は他の機械に任せます）",
			"identifier", issue.Identifier, "理由", skip.String())
		return handoffDecision{}
	}

	viewer, ok := o.viewerIdentity(ctx)
	if !ok {
		// **自分が誰か分からないまま担当のある issue に触らない**（設計 3-65 と同じ立場）。
		o.logger.Warn("gh の持ち主を取れないので、この巡回では着手しません（担当の持ち回りを判定できません）",
			"identifier", issue.Identifier)
		judged = false
		return handoffDecision{}
	}

	if !o.takeHandoffFetch() {
		o.logger.Info("持ち回りの判定に使うコメントの読み取りが、この巡回の上限に達しました（続きは次の巡回で見ます）",
			"identifier", issue.Identifier, "1回の巡回の上限", maxHandoffFetchesPerPoll)
		judged = false
		return handoffDecision{stop: true}
	}
	comments, truncated, err := o.tracker.FetchAllComments(ctx, nodeID, o.cfg.Tracker.Provider.Comments)
	if err != nil {
		o.logger.Warn("コメントを読めないので、この巡回では着手しません（担当の持ち回りを判定できません）",
			"identifier", issue.Identifier, "error", err)
		judged = false
		return handoffDecision{}
	}
	views := toCommentViews(comments)

	assessment := handoff.Assess(handoff.Situation{
		Assignees:   logins,
		Comments:    views,
		SelfLogin:   viewer.Login,
		Now:         o.now(),
		IdleTimeout: o.handoffIdleTimeout(),
	})

	switch assessment.Action {
	case handoff.ActionProceed:
		return handoffDecision{proceed: true}
	case handoff.ActionSkipManyAssignees:
		// **上の早い戻りで既に処理している。**ここへは来ない。
		return handoffDecision{}
	case handoff.ActionSkipHumanAssigned:
		// **WARN で出す**（issue #131）。**INFO だと、ログを見ていても異常だと気づけない。**
		// この issue は Status も動かず、ダッシュボードにも出ず、issue にも何も書かれない。
		// **人間が「なぜ着手されないのか」を知る手がかりが、この1行しか無い。**
		// だから「なぜ触らないか」と「どうすれば動くか」の両方をここに書く。
		//
		// **この1行では「担当者を外す」だけを案内する**（3-77b）。付け替えの道は
		// **issue へ書くコメントの側にだけ書く**（[prompt.go](prompt.go) の `buildGatedComment`）。
		// 付け替えると「自分のアカウント1人」の行に落ちて着手へ進むが、
		// **付け替える先は、その continuo が使っている gh の持ち主でなければならない。**
		// **この条件つきの説明は1行のログに収まらない。**収めようとすると、
		// **条件が落ちて「付け替えれば動く」だけが残る。**だからログには書かない。
		o.logger.Warn("担当者が付いているので着手しません（continuo が付けたものではありません）。"+
			"着手させるには、GitHub の画面でその担当者を外してください",
			"identifier", issue.Identifier, "担当者", assessment.Assignee)
		noted = true
		if o.noteGate(issue, GateReasonHumanAssigned, logins) {
			// **手元のコメントも見る**（設計 7）。メモリの印は再起動で消えるが、
			// この経路はコメントを既に読んでいるので、前の起動で書いた案内を見つけられる。
			at, found := gateNoticedIn(comments, viewer.Login, GateReasonHumanAssigned)
			switch {
			case found:
				// **前の起動で書いてある。**印だけ立てて、二度と書かない。
				// **`found` を先に見る。**新しい側に案内が残っているなら、
				// 古い側が切れていても答えは出ている。
				o.markGateNoticed(issue.ID, GateReasonHumanAssigned, at)
			case truncated:
				// **古い側を読み切れていない。**前に書いたかどうかを確かめられないので書かない。
				// **書けないことより、同じ案内を2件書くことのほうが困る**（消す手段が無い）。
				o.logger.Warn("コメントが多すぎて、前に案内を書いたかどうかを確かめられないので書きません",
					"identifier", issue.Identifier)
				o.markGateNoticeSkipped(issue.ID, GateReasonHumanAssigned, GateNoticeTooManyComments)
			default:
				o.postGateNotice(ctx, issue, nodeID, GateReasonHumanAssigned)
			}
		}
		return handoffDecision{}
	case handoff.ActionSkipSelfUnknown:
		o.logger.Warn("gh の持ち主が分からないので、担当の付いた issue には触りません",
			"identifier", issue.Identifier, "担当者", assessment.Assignee)
		return handoffDecision{}
	case handoff.ActionSkipHeld:
		o.logger.Debug("ほかの機械が期限内で担当しているので触りません（入札もしません）",
			"identifier", issue.Identifier, "担当者", assessment.Assignee,
			"最後の進捗報告（無ければ担当を取った時刻）", assessment.LastProgress)
		return handoffDecision{}
	case handoff.ActionRelease:
		released, ok := o.releaseExpiredAssignee(ctx, issue, nodeID, assessment)
		if !ok {
			return handoffDecision{}
		}
		// **担当を外したので、担当者が1人もいない状態から入札をやり直す**
		// （RUCM「他人の担当」のステップ6 → 基本フローのステップ6）。
		//
		// **いま書いた released を、読んだコメントの写しへ足す**（設計 3-77e）。
		// 足さないと、入札の回の区切りが**古い hold の時刻のまま**になり、
		// **前の回の遅い入札が今の回のものとして数えられる。**
		// もう動いていない機械の入札に負けて、この巡回の入札が1件無駄になる。
		views = append(views, released)
	case handoff.ActionBid:
	}

	if skip != handoff.SkipNone {
		// **担当を外したあとに、この機械が入札できないことがある。**
		// 外したこと自体は残るので、次に入札できる機械が拾う。
		o.logger.Info("入札しません（この issue は他の機械に任せます）",
			"identifier", issue.Identifier, "理由", skip.String())
		return handoffDecision{}
	}
	return o.bidForIssue(ctx, issue, nodeID, viewer, bid, views)
}

// evaluateBid は、この機械が書く入札の中身と、入札してよいかを決める（設計 3-77）。
//
// **GitHub を1バイトも読まない。**枠の写しだけで決まるので、
// **コメントを読む前に呼んで、入札できない機械を先に落とせる。**
//
// 戻り値の1つ目: 組み立てた入札（入札しないときの中身は使わない）。
// 戻り値の2つ目: 入札しないと決めた理由。SkipNone なら入札してよい。
func (o *Orchestrator) evaluateBid() (handoff.Bid, handoff.SkipReason) {
	return handoff.Evaluate(
		o.quotaForBid(),
		// **`rate_limit.source: none` は「読めなかった」ではない**（設計 3-27 の逃げ道）。
		// 運用者が枠で判定しないと決めた状態なので、そこで黙らせると
		// **その機械は1件も処理しなくなる。**
		o.cfg.RateLimit.Source != ratelimit.SourceNone,
		o.bidMargins(),
		o.now(),
	)
}

// releaseExpiredAssignee は期限の切れた担当を外し、released のコメントを1件書く（設計 3-77c）。
//
// **外すのは名指しした1人だけである。**人間が別の担当者を足していたら、その人は残る。
// **released のコメントの `from` には、外した担当者のログイン名を書く**（設計 3-77-0）。
// **投稿者では代われない。**このコメントを書くのは外した側で、`from` に入るのは外された側である。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 対象の issue。
// nodeID: 下敷きの GitHub issue のノード ID。
// assessment: 期限切れと判定した結果（外す担当者と、読んだ hold が入っている）。
// 戻り値の1つ目: 書いた released のコメントの写し（入札の回の区切りに使う）。
// 戻り値の2つ目: 外せたら true。**外せなければ false**（コメントも書かない）。
func (o *Orchestrator) releaseExpiredAssignee(
	ctx context.Context, issue tracker.Issue, nodeID string,
	assessment handoff.Assessment,
) (handoff.CommentView, bool) {
	id, ok := assigneeIDOf(issue, assessment.Assignee)
	if !ok {
		o.logger.Warn("外す担当者のノード ID を引けないので、担当を外しません",
			"identifier", issue.Identifier, "担当者", assessment.Assignee)
		return handoff.CommentView{}, false
	}
	if _, err := o.tracker.RemoveAssignees(ctx, nodeID, []string{id}); err != nil {
		o.logger.Warn("期限の切れた担当を外せません（次の巡回でやり直します）",
			"identifier", issue.Identifier, "担当者", assessment.Assignee, "error", err)
		return handoff.CommentView{}, false
	}
	o.logger.Info("期限の切れた担当を外しました（入札をやり直します）",
		"identifier", issue.Identifier, "外した担当者", assessment.Assignee,
		"最後の進捗報告（無ければ担当を取った時刻）", assessment.LastProgress,
		"期限", o.handoffIdleTimeout())

	now := o.now()
	body := handoff.FormatReleased(handoff.Released{
		From:   assessment.Assignee,
		Branch: assessment.Hold.Branch,
		At:     now,
	})
	// **いま書いたばかりなので、作成時刻と更新時刻は同じである。**
	// **投稿者は入れない。**この写しは回の区切りを決めるためだけのもので、
	// **`RoundStart` は本文と作成時刻しか見ない。**
	// **`CollectBids` は印の照合で先に落とす**ので、投稿者まで辿り着かない。
	view := handoff.CommentView{Body: body, CreatedAt: now, UpdatedAt: now}
	if err := o.postOwnMarkedComment(ctx, nodeID, body); err != nil {
		// **担当は既に外れている。**コメントを書けなかったことで入札を止めない
		// （止めると、担当者のいない issue が誰にも拾われなくなる）。
		o.logger.Warn("担当を外したことを issue へ書けませんでした",
			"identifier", issue.Identifier, "error", err)
		// **書けなかったコメントを回の区切りに使わない。**issue には残っていないので、
		// ほかの機械はその区切りを読めない。**同じ列を読んだ機械が同じ答えに行き着かなくなる。**
		return handoff.CommentView{}, true
	}
	return view, true
}

// bidForIssue は担当者のいない issue に入札し、勝っていれば担当者になる（設計 3-77）。
//
// **巡回1回でここを通り抜けるとは限らない。**入札を書いた巡回は偽を返し、
// 締め切りが過ぎた巡回でもう一度ここへ来て勝敗を決める。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 対象の issue。
// nodeID: 下敷きの GitHub issue のノード ID。
// viewer: この機械が使っている gh の持ち主。
// bid: この機械が書く入札（evaluateBid が組み立てたもの）。
// comments: issue に付いているコメントの全件。
// 戻り値: 着手してよいか。**hold を書けなかったときは、勝っていても着手しない**
// （書いた担当者を消し戻すので acquired も立てない。設計 3-77g）。
func (o *Orchestrator) bidForIssue(
	ctx context.Context,
	issue tracker.Issue,
	nodeID string,
	viewer tracker.Assignee,
	bid handoff.Bid,
	comments []handoff.CommentView,
) handoffDecision {
	// **数えるのは、いまの回の入札だけである**（設計 3-77e）。
	// **前の回の入札は issue に残り続ける**（1回ごとに新しいコメントを書くので消えない）。
	// 数に入れると、締め切りが常にその古い時刻から数えられ、**次の回が1度も始まらない。**
	// **巡回のたびに入札のコメントだけが増え、担当者は永久に決まらない。**
	// **書く側の識別子は、ここで埋める**（設計 3-77-0）。`evaluateBid` は
	// `viewerIdentity` より先に呼ばれるので、入札を組み立てた時点では持ち主が分かっていない。
	//
	// **埋め忘れてはならない。**下で `posted` をそのまま勝敗の判定へ混ぜるので、
	// **空のままだと、その巡回では自分が勝っても「負けた」と読む。**
	// **次の巡回で GitHub から読み直せば勝てる**（`CollectBids` が投稿者から埋める）が、
	// **`bid_window_ms: 0` は「締め切りを待たない」設定なのに、1巡回ぶん待たされる。**
	bid.Author = viewer.Login

	window := o.handoffBidWindow()
	bids := handoff.RoundBids(comments, o.now(), window)
	if _, already := handoff.HasBidBy(bids, viewer.Login); !already {
		posted, ok := o.postBid(ctx, issue, nodeID, bid)
		if !ok {
			return handoffDecision{}
		}
		bids = append(bids, posted)
	}

	deadline, ok := handoff.Deadline(bids, window)
	if !ok {
		// **入札が1件も無い。**自分の入札を書けなかったということなので、次の巡回でやり直す。
		return handoffDecision{}
	}
	if o.now().Before(deadline) {
		o.logger.Debug("入札の締め切りを待ちます",
			"identifier", issue.Identifier, "締め切り", deadline, "届いている入札", len(bids))
		return handoffDecision{}
	}

	winner, ok := handoff.Winner(handoff.BidsBefore(bids, deadline))
	if !ok {
		return handoffDecision{}
	}
	if !strings.EqualFold(winner.Author, viewer.Login) {
		o.logger.Info("入札に負けたので着手しません",
			"identifier", issue.Identifier, "勝ったアカウント", winner.Author,
			"勝った判定スコア", winner.Score, "この continuo の判定スコア", bid.Score)
		return handoffDecision{}
	}

	if _, err := o.tracker.AddAssignees(ctx, nodeID, []string{viewer.ID}); err != nil {
		o.logger.Warn("入札に勝ちましたが担当者を書けません（次の巡回でやり直します）",
			"identifier", issue.Identifier, "error", err)
		return handoffDecision{}
	}
	o.logger.Info("入札に勝ったので担当者になりました",
		"identifier", issue.Identifier, "担当者", viewer.Login,
		"判定スコア", winner.Score, "届いた入札", len(bids))

	body := handoff.FormatHold(handoff.Hold{
		Assignee: viewer.Login,
		Branch:   o.branchNameFor(issue),
		At:       o.now(),
	})
	if err := o.postOwnMarkedComment(ctx, nodeID, body); err != nil {
		// **hold を書けないまま着手させてはならない**（設計 3-77g）。
		//
		// **「次の巡回で書き直せる」は成り立たない。**hold を書く箇所はここ1箇所だけであり、
		// この issue は既に担当者が付いている（claim 済み）ので、次の巡回では
		// `dispatchCandidates` 冒頭の `lookupRunByID` で弾かれ、`handoffGate` はもう呼ばれない。
		//
		// **「hold が無くても、この continuo が続ければよい」も成り立たない。**
		// 担当者はあるが hold が無い状態は、**別のアカウントの continuo からは
		// `ActionSkipHumanAssigned`（人間が付けた担当）に見える**
		// （[assess.go](../handoff/assess.go) の `!hasHold` の行）。
		// **hold は「この担当者は機械である」の唯一の証拠であり、それが無いと期限で外せない。**
		// **この機械が落ちても、誰も引き継げなくなる。**
		// しかも他の continuo は、その issue へ「担当者を外してください」の案内を書きに行く。
		//
		// **だから着手しない。**`undoHandoffAcquire` で書いた担当者を消し戻し、
		// 誰も担当していない状態から次の巡回で入札をやり直す。
		o.logger.Warn("hold のコメントを書けないので、着手を見送って担当者を消し戻します",
			"identifier", issue.Identifier, "error", err)
		o.undoHandoffAcquire(ctx, issue)
		return handoffDecision{}
	}
	return handoffDecision{proceed: true, acquired: true}
}

// undoHandoffAcquire は、この巡回で書いた担当者を消し戻す（設計 3-77c）。
//
// **着手を取りやめたときに呼ぶ。**担当者と hold を書いたあとで着手をやめると、
// **ほかの機械はそれを「期限内の担当」と読み、`idle_timeout_ms`（既定18時間）触らない。**
// **この機械では通らない検査（信頼していないリポジトリ）でも、別の機械では通ることがある。**
// 消し戻さないと、その issue は誰にも着手されないまま塞がる。
//
// **released のコメントを書く。**担当者を外したことを、ほかの機械と人間の両方が読めるようにする。
// **入札の回もそこで区切られる**（設計 3-77e）。
//
// **`RemoveAssignees` 自体が失敗したときは、担当者を残したまま何もせず戻る。**released は書かない
// （担当を外せていないのに外したと書くと嘘になる）。**そのときの扱いは設計 3-77g に書いてある。**
// 要点だけ言うと、担当者はあるが hold は無い状態のまま残る。
// **この continuo は次の巡回で「自分のアカウント1人」と読んで着手するが、hold は無いままである。**
// **だから、この機械が落ちたときに別のアカウントの continuo が引き継げない**
// （向こうからは「人間が付けた担当」に見えて、期限で外せない）。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 対象の issue。
func (o *Orchestrator) undoHandoffAcquire(ctx context.Context, issue tracker.Issue) {
	login, ok := o.releaseOwnAssignee(ctx, issue, "着手を取りやめましたが")
	if !ok {
		return
	}
	o.logger.Info("着手を取りやめたので、書いた担当者を消し戻しました",
		"identifier", issue.Identifier, "担当者", login)
}

// releaseOwnAssignee は、この機械が書いた担当者を外し、`released` のコメントを1件書く
// （設計 3-77c / 3-77g）。
//
// **外すのは自分1人だけである。**人間が別の担当者を足していたら、その人は残る。
//
// **`assigneeIDOf` は使えない。**自分のノード ID を引くには結局 `viewerIdentity` を呼ぶので、
// **そこから同時に取れる。**issue の写しから探すと、写しの新しさに答えが依存してしまう。
//
// **`viewerIdentity` を取れなければ、何もせずに戻る。**外す相手を間違えない。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 対象の issue。
// failurePrefix: 外せなかったときの警告の頭に付ける語（呼び出しの文脈で変わる）。
// 戻り値の1つ目: 外した担当者のログイン名。
// 戻り値の2つ目: 外せたら true。**外せなければ false**（released も書いていない）。
func (o *Orchestrator) releaseOwnAssignee(
	ctx context.Context, issue tracker.Issue, failurePrefix string,
) (string, bool) {
	nodeID, viewer, ok := o.releaseTargetFor(ctx, issue, failurePrefix)
	if !ok {
		return "", false
	}
	// **理由は空で渡す。**ここは着手を取りやめる経路で、`workspace_hooks.after_run` を
	// 走らせていない。**3-77c の「この branch へ push しないでください」がそのまま正しい。**
	return o.removeOwnAssignee(ctx, issue, nodeID, viewer, failurePrefix, "")
}

// releaseTargetFor は「誰を、どの issue から外すか」を先に決める（設計 3-77c / 3-77g）。
//
// **書き込みを1バイトも行わない。**外せるかどうかだけを先に知りたい経路が使う
// （`after_run` を走らせる前に、外す相手が分かっているかを確かめる）。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 対象の issue。
// failurePrefix: 決められなかったときの警告の頭に付ける語。
// 戻り値の1つ目: issue のノード ID。
// 戻り値の2つ目: この機械の担当者。
// 戻り値の3つ目: 2つとも決まれば true。
func (o *Orchestrator) releaseTargetFor(
	ctx context.Context, issue tracker.Issue, failurePrefix string,
) (string, tracker.Assignee, bool) {
	nodeID := issueNodeID(issue)
	if nodeID == "" {
		// **draft issue には担当者もコメントも書けない。****黙って戻らない。**
		// 呼び出し側は毎巡回でここへ来るので、1行も出さないと理由が分からないまま止まる。
		o.logger.Warn(failurePrefix+"、この issue には担当者を書けません（draft issue です）",
			"identifier", issue.Identifier)
		return "", tracker.Assignee{}, false
	}
	viewer, ok := o.viewerIdentity(ctx)
	if !ok {
		o.logger.Warn(failurePrefix+"、gh の持ち主を取れないので担当者を消せません",
			"identifier", issue.Identifier)
		return "", tracker.Assignee{}, false
	}
	return nodeID, viewer, true
}

// removeOwnAssignee は担当者を外し、`released` のコメントを1件書く（設計 3-77c / 3-77g）。
//
// **外す相手は `releaseTargetFor` が決めてある。**この関数は書き込みだけを行う。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 対象の issue。
// nodeID: issue のノード ID。
// viewer: 外す担当者（この機械）。
// failurePrefix: 外せなかったときの警告の頭に付ける語。
// 戻り値の1つ目: 外した担当者のログイン名。
// 戻り値の2つ目: 外せたら true。
func (o *Orchestrator) removeOwnAssignee(
	ctx context.Context, issue tracker.Issue, nodeID string, viewer tracker.Assignee,
	failurePrefix, reason string,
) (string, bool) {
	if _, err := o.tracker.RemoveAssignees(ctx, nodeID, []string{viewer.ID}); err != nil {
		// **担当者が残る。**hold も released も無いので、次にこの issue を見る機械は
		// 「hold の無い自分の担当」として待たずに着手を試みる（設計 3-77g / 3-77b）。
		// **「次の巡回で入札し直す」わけではない。**担当者が消えていない以上、
		// 入札からはやり直されない。
		o.logger.Warn(failurePrefix+"、書いた担当者を消し戻せません（担当者が残ります）",
			"identifier", issue.Identifier, "error", err)
		return "", false
	}
	body := handoff.FormatReleased(handoff.Released{
		From:   viewer.Login,
		Branch: o.branchNameFor(issue),
		At:     o.now(),
		Reason: reason,
	})
	if err := o.postOwnMarkedComment(ctx, nodeID, body); err != nil {
		o.logger.Warn("担当者を消し戻したことを issue へ書けませんでした",
			"identifier", issue.Identifier, "error", err)
	}
	return viewer.Login, true
}

// weeklyWaitExceeded は「1週間の枠が明けるのを待つ上限を超えたか」を返す
// （設計 3-27。issue #197）。
//
// **満杯の1週間の枠を見た時刻を、この中で記録する。**判定と記録を分けると、
// **呼ぶ側が記録を忘れたときに、経過が永久に0のままになる。**
//
// **測り方は2通りある。**
//
//	満杯の1週間の枠が全部 `resets_at` を持つ  … いちばん遅い時刻までの残りで測る
//	1つでも持たない                            … 満杯になってからの経過で測る
//
// **5時間の枠だけが満杯なら、いつまでも待つ**（2026-08-26 の人間の決定
// 「5時間枠 → 待つ。担当は変えない」）。
//
// **`quotaResetAt()` を使ってはならない。**あれは種別を選ばないので、
// **1週間の枠を待っているのに5時間の枠の時刻で判定してしまう。**
// 5時間の枠が明けるたびに枠待ちの印が外れるため、上限に届くのがそのぶん遅れる。
//
// **直前の読み取りに失敗しているときは、手放さない。**この判定は GitHub へ2回書き、
// pane を閉じる。**古い写しで動かすと、既に明けている枠を根拠に担当を外すことになる。**
// **経過の時計は止めない。**読めるようになった時点で、そのまま効く。
//
// **判定の軸は「リセットまでの残り時間」である。**2026-08-26 に人間が承認した表が、
// 1週間の枠を「上限以内なら待つ／上限より先なら引き渡す」で分けている。
// **その上限が `rate_limit.weekly_wait_limit_minutes`（既定300分＝5時間）である。**
//
// rs: 対象の run。
// 戻り値: 上限を超えていれば true。
func (o *Orchestrator) weeklyWaitExceeded(rs *runState) bool {
	// **1回のロックで取る**（設計 3-77j）。**`quotaSnapshot()` と `quotaForBid()` を
	// 続けて呼んではならない。**2回のロックの間に `pollQuota` が割り込むと、
	// **「読めている」と答えた新しい写しではなく、古い写しで手放すことになる。**
	snap, stale := o.quotaSnapshotWithStale()
	// **「使い切っている」ではなく「余裕が無い」で数える**（人間の決定。2026-09-06。issue #197）。
	// **入札に使う余裕値と同じ線である**（`handoff.Short`）。
	// **線を1本にしないと、使用率90〜99の帯で run が枠待ちにならないまま
	// 手放しの条件だけを満たし、打ち切りと手放しが競走する。**
	shortWeekly := handoff.ShortWeekly(o.bidMargins())
	weeklyShort := snap.AnySelected(shortWeekly)
	since := rs.noteWeeklyShort(weeklyShort, o.now())

	// **読めなくなった写しで手放さない。**この判定は GitHub へ2回書き、pane を閉じる。
	// **資格情報が切れた機械は、切れる直前の値を1日中返し続ける**（設計 3-77i）。
	// **時計は上で進めてある。**読めるようになった時点で、経過はそのまま効く。
	//
	// **同じ読み取りから判定する。**`quotaForBid()` を呼び直すと2回目のロックになり、
	// **その間に `pollQuota` が新しい写しへ差し替えると、「読めている」と答えたうえで
	// 古い写しの数字で手放す。**
	if snap == nil || stale {
		return false
	}

	limit := time.Duration(o.cfg.RateLimit.WeeklyWaitLimitMinutes) * time.Minute
	if limit <= 0 || !weeklyShort {
		// **0 以下は「上限を設けない」**（`claude.turn_timeout_ms` と
		// `tracker.provider.handoff.recheck_interval_ms` と同じ向き）。
		// **5時間の枠だけならいつまでも待つ。**
		return false
	}
	// **人間が書いた式そのものである**（2026-08-26 / 2026-09-06）。
	//
	//	現在時刻 + weekly_wait_limit_minutes < 1週間の枠のリセット時刻
	//
	// 移項すると `リセット時刻 − 現在時刻 > 上限` になる。**待っても明けないという意味である。**
	//
	// **選ぶ枠は `shortWeekly` と同じものにする。**`weeklyShort` を数えた集合と
	// **リセット時刻を引く集合がずれると、「余裕が無い」と答えた枠の明ける時刻を見ないまま手放す。**
	if resetAt, ok := snap.LatestResetForWaitLimit(shortWeekly); ok {
		return resetAt.Sub(o.now()) > limit
	}
	// **リセット時刻を1つでも読めないときは、経過で測る。**
	// **消してはならない。**`resets_at` が `null` の枠だけが逼迫している機械は、永久に待つことになる。
	return !since.IsZero() && o.now().Sub(since) > limit
}

// releaseBecauseQuotaWaitAsync は、待つ上限を超えた run を止めて担当を手放す
// （設計 3-27 / 3-77c。issue #197）。
//
// **巡回のループから呼んでよいのはこちらである。**`RemoveAssignees` とコメントの投稿と
// pane を閉じる要求が乗るので、**同じ巡回で複数の run が超えると直列に積まれる。**
//
// **印は同期に確保してから goroutine を起こす。**確保できなければ何もしない
// （書き戻しの最中なら、次の巡回でやり直す）。**二重には走らない。**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
func (o *Orchestrator) releaseBecauseQuotaWaitAsync(ctx context.Context, rs *runState) {
	if rs.beginTerminal() != terminalClaimed {
		return
	}
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		_ = o.releaseBecauseQuotaWaitClaimed(ctx, rs)
	}()
}

// releaseBecauseQuotaWaitClaimed は、終わらせる印を確保したあとの本体である。
//
// **順番を入れ替えてはならない。**
//
//  0. 外す相手を確かめる                    … **決まらなければ after_run を走らせずに戻る**
//  1. workspace_hooks.after_run を走らせる … 利用者が書いた push がここで動く
//  2. 自分の担当者を外し、released を書く   … **失敗したら pane を閉じずに戻る**
//  3. worker を止める                        … pane を閉じる
//  4. 印から外す                             … スロットを空ける
//
// **段0 を置く理由。**`RunAfterRunOnce` は実行の前に印を立て、**印を消すのは着手と片付けだけである。**
// **draft issue と「`gh` の持ち主を取れない」の2つは、走らせる前に分かる。**
// 先に確かめれば、**その2つで after_run の印を無駄に消費しない。**
//
// **段2 を段3 より先に置く。**逆にすると、`viewerIdentity` を取れなかったときに
// **pane だけ閉じて担当が残り、`idle_timeout_ms` のあいだ誰も動かない issue になる。**
//
// **段1 を段2 より先に置く。**逆にすると、`released` を読んだ次の機械が、
// **こちらの `git push` が終わる前に同じ branch で作業を始めうる**
// （`workspace_hooks.timeout_ms` を入札の締め切りより長くしている設定で起きる）。
//
// **段1 で `after_run` を走らせる。**担当を失ったときの止め方（3-77c）は走らせないが、
// **あちらは「担当が既に別の機械へ移っている」ので、push すると新しい担当の続きと衝突する。**
// **こちらは自分から手放す側で、まだ担当者である。**衝突する相手がいない。
// **人間の決定「push して引き渡す」は、この経路で果たす。**
//
// **段2 が失敗したときの後始末は無い。**`after_run` は既に走っており、
// `RunAfterRunOnce` は印を戻さない。**push は済んでいるので、失われるのはそのあとに
// 積んだ commit だけである。**そのことを `Warn` で1行出す。
// **残るのは `RemoveAssignees` が一時的に落ちる場合だけである**（段0 で残り2つを潰した）。
//
// **段4 を落としてはならない。**`beginTerminal` は印を立てたまま返らないので、
// **`release` を呼ばないと、その run は印に残り続けてスロットを永久に埋める。**
//
// **カンバンへは1バイトも書かない**（設計 3-77c）。**worktree も消さない。**
// push していない変更が残っているかもしれず、人間が中身を見て判断できる状態のまま置く。
//
// ctx: 呼び出しに適用するコンテキスト。**この中で作り直すので、期限切れでもよい。**
// rs: 対象の run。
func (o *Orchestrator) releaseBecauseQuotaWaitClaimed(ctx context.Context, rs *runState) bool {
	issue := rs.issue()
	// **どの枠に余裕が無いかを控える。**下の `Info` と `Warn` に載せる。
	// **載せないと、`weekly_scoped`（1週間のモデル別の枠）が原因のときに、
	// 人間が claude.ai の画面と突き合わせても食い違って見える。**
	// **理由が既定の水準で出ないのは、issue #173 が直そうとしている症状そのものである。**
	shortSnap, _ := o.quotaSnapshotWithStale()
	shortKinds := strings.Join(shortSnap.SelectedKinds(handoff.Short(o.bidMargins())), ", ")

	// **後片付けは「止めろ」と言われても最後までやる**（`stopBecauseHandoffLost` と同じ理由）。
	// **`stopWorker` は待ちの ctx を殺す**ので、そのまま使うと後続の書き込みが打ち切られる。
	//
	// **段0 から段4 まで、全部これを使う。**
	// **段0 だけ生の `ctx` を渡してはならない。**この関数は非同期の goroutine から
	// 呼ばれるので、**巡回の ctx は先に切れる。**切れた ctx で段0a を呼ぶと
	// 「確かめられなかった」に落ち、切れた ctx で段0b を呼ぶと毎巡回で `Warn` が出る。
	keepCtx := context.WithoutCancel(ctx)

	// **段0a。まだ自分が担当かを確かめる。**
	// **枠待ちのあいだ、担当は自分の意思と無関係に外れる**（設計 3-77c。
	// `tracker.provider.handoff.idle_timeout_ms` は既定18時間で、
	// **枠待ち中は hook が来ないので進捗のコメントも増えない**）。
	// **3-77c は「担当を外されたら、その branch へ push してはならない」と決めている。**
	// **確かめずに段1 の `after_run` を走らせると、利用者が書いた `git push` が
	// 別の continuo の branch へ飛ぶ。**
	//
	// **既存の確かめでは間に合わない。**`handleTurnEnd` は turn の終わりでしか走らず、
	// **枠待ちの run には turn の終わりが来ない。**
	//
	// **3-77c の穴を塞いだと読んではならない。**この段は `weekly_wait_limit_minutes` の
	// 内側にしかない。**`0`（上限を設けない）にした機械では、枠待ちの run は
	// 担当が移ったことに気づかないままである。**塞いだのは
	// 「自分から手放すときに push 先を間違えない」ことだけである。
	// **期限を付ける**（issue #197）。`keepCtx` は切られないので、
	// **ここで上限を置かないと `gh` が固まったときに誰も止められない。**
	checkCtx, cancelCheck := context.WithTimeout(keepCtx, quotaReleaseCheckBudget)
	defer cancelCheck()
	mine, known, newAccount := o.mayReleaseOwnWork(checkCtx, rs)
	switch {
	case !known:
		// **分からないなら手放さない。**次の巡回でやり直す。
		// **`verifyHandoff` と向きを変える。**あちらは「走っている run を止めてよいか」なので、
		// **分からないときは止めない側へ倒す。**こちらは「push してよいか」なので、
		// **分からないときは push しない側へ倒す。**
		o.logger.Warn("枠の上限で担当を手放そうとしましたが、いまの担当を確かめられないので見送ります"+
			"（次の巡回でやり直します）",
			"identifier", issue.Identifier)
		rs.endTerminal()
		return false
	case !mine:
		// **`after_run` を走らせず、カンバンへも書かず、担当者にも触らない**（3-77c）。
		// **終わらせる最中の標識は確保したまま渡す。**いったん戻すと、その隙に別の goroutine が掴む。
		o.stopHandoffLostClaimed(keepCtx, rs, newAccount)
		return true
	}

	// **段0b。外す相手を先に確かめる。**`after_run` は `RunAfterRunOnce` が実行の前に印を立て、
	// **印を消すのは着手と片付けだけである。**先に走らせて外す相手が分からなかった場合、
	// **この run が枠明けに完走しても `finishRun` の `after_run` が印に弾かれて1回も走らない。**
	// **draft issue と「`gh` の持ち主を取れない」の2つは、走らせる前に分かる。**
	nodeID, viewer, ready := o.releaseTargetFor(checkCtx, issue, "枠の上限で担当を手放そうとしましたが")
	if !ready {
		rs.endTerminal()
		return false
	}

	// **段1。`after_run` を、担当を外す前に走らせる。**
	// **順番を入れ替えてはならない。**外してから push すると、`released` を読んだ次の機械が
	// **こちらの `git push` が終わる前に、同じ branch で作業を始めうる**
	// （`workspace_hooks.timeout_ms` を入札の締め切りより長くしている設定で起きる）。
	//
	// **止められても走らせる。**ここで `ctx` をそのまま渡すと、Ctrl+C の直後に
	// **`git push` が1バイトも走らずに終わる。**
	//
	// **持ち時間は足さない。**`workspace.RunHook` が `workspace_hooks.timeout_ms`
	// （既定60秒）を自分で掛ける。**ここで同じ長さをもう1枚重ねると、
	// 外側のほうが先に始まっているぶん先に切れ、hook の側の後始末を通らずに
	// `context deadline exceeded` だけが残る。**
	afterRunOK := o.runAfterRunOK(keepCtx, rs)

	// **段2。GitHub への書き込みには、herdr の持ち時間を使わない。**
	// `herdr.read_timeout_ms`（既定5秒）は**socket の応答を待つ上限**であり、
	// **担当者を外す・`released` を書くという2本の GraphQL に足りない。**
	// **足りないと毎巡回でやり直し、run はスロットと pane を握ったまま残る。**
	writeCtx, cancelWrite := context.WithTimeout(keepCtx, quotaReleaseWriteBudget)
	// **`after_run` が走らなかったときは、理由を分ける。**
	// **`released` の本文が「実行済みです。remote の続きから始めてください」と断言する**ので、
	// **走っていないのにその文を出すと、次に拾う機械が入っていない commit の続きから始める。**
	reason := handoff.ReleaseReasonWeeklyWaitLimit
	if !afterRunOK {
		reason = handoff.ReleaseReasonWeeklyWaitLimitNoPush
	}
	login, ok := o.removeOwnAssignee(writeCtx, issue, nodeID, viewer,
		"枠の上限で担当を手放そうとしましたが", reason)
	cancelWrite()
	if !ok {
		// **pane を閉じない。**閉じてしまうと、担当がこの機械のまま誰も動かなくなる。
		// **印も外さない。**次の巡回でやり直す。
		//
		// **`after_run` は既に走っている。**`RunAfterRunOnce` は実行の前に印を立てるので、
		// **この run が枠明けに完走しても、`finishRun` の `after_run` は走らない。**
		// **push そのものは、いまの段1 で済んでいる。**そのあとに積んだ commit だけが
		// push されないまま残る。**黙って進めない。**次に何を見ればよいかを1行で出す。
		o.logger.Warn("枠の上限で担当を手放せませんでした（次の巡回でやり直します）。"+
			"workspace_hooks.after_run は既に走らせたので、この run が完走しても再実行されません",
			"identifier", issue.Identifier,
			"after_run が成功したか", afterRunOK,
			"weekly_wait_limit_minutes", o.cfg.RateLimit.WeeklyWaitLimitMinutes,
			"余裕の無い枠", shortKinds)
		rs.endTerminal()
		return false
	}

	// **段3。pane を閉じるのは herdr の持ち時間で足りる。**socket の応答を1回待つだけである。
	cleanupCtx, cancel := context.WithTimeout(
		keepCtx, time.Duration(o.cfg.Herdr.ReadTimeoutMs)*time.Millisecond)
	defer cancel()
	o.stopWorker(cleanupCtx, rs)
	// **段4。run の登録から外す。**落とすとスロットを永久に埋める。
	o.release(rs)
	o.logger.Info("1週間の枠が明けるのを待つ上限を超えたので、担当を手放しました"+
		"（次の担当は入札で決め直します。worktree は残します。カンバンへは書きません）",
		"identifier", issue.Identifier, "外した担当者", login,
		"after_run が成功したか", afterRunOK,
		"weekly_wait_limit_minutes", o.cfg.RateLimit.WeeklyWaitLimitMinutes,
		"余裕の無い枠", shortKinds)
	return true
}

// postBid は入札のコメントを1件書く（設計 3-77a）。
//
// **入札のたびに新しいコメントを書く。**編集して使い回さない。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 対象の issue。
// nodeID: 下敷きの GitHub issue のノード ID。
// bid: 書く入札。
// 戻り値の1つ目: 書いた入札（投稿の時刻を埋めたもの）。
// 戻り値の2つ目: 書けたら true。
func (o *Orchestrator) postBid(
	ctx context.Context, issue tracker.Issue, nodeID string, bid handoff.Bid,
) (handoff.Bid, bool) {
	// **締め切りまでの長さを渡す。**コメントに「担当は約何分後に決まるか」を書くためである
	// （設計 3-77a）。issue を開いた人が、待てばよいのかを読めるようにする。
	if err := o.postOwnMarkedComment(ctx, nodeID, handoff.FormatBid(bid, o.handoffBidWindow())); err != nil {
		o.logger.Warn("入札のコメントを書けません（次の巡回でやり直します）",
			"identifier", issue.Identifier, "error", err)
		return handoff.Bid{}, false
	}
	o.logger.Info("入札しました",
		"identifier", issue.Identifier, "アカウント", bid.Author,
		"5時間余裕値", bid.FiveHour, "1週間余裕値", bid.Weekly, "判定スコア", bid.Score)
	// **投稿の時刻は自分の時計で埋める。**GitHub が付けた時刻は次の巡回で読み直す。
	// **この写しを使うのはこの巡回の締め切りの計算だけである。**
	bid.PostedAt = bid.At
	return bid, true
}

// handoffBidWindow は入札を締め切るまでの長さを返す（`tracker.provider.handoff.bid_window_ms`）。
//
// **0 のときも 0 のまま返す。**`idle_timeout_ms` と違って、**0 は未設定ではなく
// 「締め切りを待たずに勝者を決める」という指定である**（設計 3-77 / config の検査もそう書いている）。
// 既定へ差し替えると、その設定にした人が3分待たされる。
//
// 戻り値: 入札を締め切るまでの長さ。
func (o *Orchestrator) handoffBidWindow() time.Duration {
	return time.Duration(o.cfg.Tracker.Provider.Handoff.BidWindowMs) * time.Millisecond
}

// handoffIdleTimeout は担当を外すまでの長さを返す。
//
// **0 なら既定の18時間を使う**（設定に書かなくても効く）。
//
// 戻り値: 担当者の最後の進捗報告からこれだけ経つと担当を外す長さ。
func (o *Orchestrator) handoffIdleTimeout() time.Duration {
	ms := o.cfg.Tracker.Provider.Handoff.IdleTimeoutMs
	if ms <= 0 {
		ms = config.DefaultHandoffIdleTimeoutMs
	}
	return time.Duration(ms) * time.Millisecond
}

// **既定は `config.DefaultHandoffIdleTimeoutMs` が持つ。**
// ここで定数を2つ目に置くと、設定の検査と実行時の値がずれる。

// viewerIdentity は、この機械が使っている gh の持ち主（ログイン名とノード ID）を返す。
//
// **担当者を書き足すにはノード ID が要る。**ログイン名だけでは
// `addAssigneesToAssignable` を呼べない。
//
// **一度取れたら覚えておく。**持ち主が変わるのは `gh auth switch` を人間が叩いたときだけで、
// その操作は continuo を止めずに行うものではない。
//
// **取れなかったときは `ghLoginRetryInterval` を空けてから取り直す**（設計 3-65 と同じ間隔）。
// 空けないと、`gh` が落ちているあいだ**巡回の候補ごとに GraphQL を1本ずつ投げる。**
//
// **取れたログイン名は `selfLogin` と突き合わせる**（設計 3-77f）。
// **同じ問いを2つの経路で聞いている**（`gh api user` と GraphQL の `viewer`）。
// **食い違ったら WARN を1行残す。**印と投稿者を突き合わせる経路（`hasRunComment`）と
// 持ち回りの経路が、別の名前で比べていることに気づけるようにする。
//
// ctx: 呼び出しに適用するコンテキスト。
// 戻り値の1つ目: 持ち主。
// 戻り値の2つ目: 取れれば true。
func (o *Orchestrator) viewerIdentity(ctx context.Context) (tracker.Assignee, bool) {
	o.mu.Lock()
	cached := o.viewer
	lastTried := o.viewerLastTriedAt
	o.mu.Unlock()
	if cached.ID != "" && cached.Login != "" {
		return cached, true
	}
	if !lastTried.IsZero() && o.now().Before(lastTried.Add(ghLoginRetryInterval)) {
		// **前に試して駄目だったばかりである。**候補ごとに投げ直さない。
		return tracker.Assignee{}, false
	}

	v, err := o.tracker.FetchViewer(ctx)
	if err != nil || v.ID == "" || v.Login == "" {
		o.mu.Lock()
		o.viewerLastTriedAt = o.now()
		o.mu.Unlock()
		if err != nil {
			o.logger.Warn("gh の持ち主を GraphQL から取れません", "error", err)
		}
		return tracker.Assignee{}, false
	}
	o.mu.Lock()
	o.viewer = v
	o.mu.Unlock()
	o.warnIfViewerDiffers(v.Login)
	return v, true
}

// warnIfViewerDiffers は、2つの経路で取れた持ち主が食い違っていたら WARN を1行残す
// （設計 3-77f）。
//
// **同じ問いを2つの経路で聞いている。**`ensureGHLogin` は `gh api user` を、
// `viewerIdentity` は GraphQL の `viewer` を引く。
// **どちらも同じ `gh` の認証を使うので、ふつうは一致する。**
// **食い違うのは `gh auth switch` を継続中に叩いたときだけである。**
// そのとき、印と投稿者を突き合わせる経路と持ち回りの経路が違う名前で比べることになる。
//
// **`selfLogin` は書き換えない**（設計 3-65）。あちらは「`gh api user` で取れたか」を
// 表しており、**取れていないあいだは取り直し続ける**という約束が乗っている。
// ここで埋めると、その取り直しが黙って止まる。
//
// login: GraphQL が返した持ち主のログイン名。
func (o *Orchestrator) warnIfViewerDiffers(login string) {
	o.ghLoginMu.Lock()
	current := o.selfLogin
	o.ghLoginMu.Unlock()
	if current == "" || strings.EqualFold(current, login) {
		return
	}
	o.logger.Warn("gh の持ち主が2つの経路で食い違っています（`gh auth switch` を叩きましたか）",
		"gh api user", current, "GraphQL の viewer", login)
}

// takeHandoffFetch は、持ち回りの判定でコメントを読む枠を1つ取る（設計 3-77a）。
//
// **1回の巡回で読める issue の数に上限を置く**（maxHandoffFetchesPerPoll）。
// **入札に負け続ける機械は空きスロットを埋めないので、候補のループが最後まで止まらない。**
// 104件のカンバンでは、30秒の巡回1回が数百リクエストになる。
//
// 戻り値: 枠を取れたら true。**偽ならこの巡回はここで打ち切る。**
func (o *Orchestrator) takeHandoffFetch() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.handoffFetches >= maxHandoffFetchesPerPoll {
		return false
	}
	o.handoffFetches++
	return true
}

// resetHandoffFetchBudget は、コメントを読む枠を巡回1回ぶんに戻す（設計 3-77a）。
//
// **巡回の頭で呼ぶ。**
func (o *Orchestrator) resetHandoffFetchBudget() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.handoffFetches = 0
}

// branchNameFor は、この issue のために使う branch の名前を返す（hold のコメントに書く）。
//
// **issue から一意に決まる**（`herdr.worktree.branch_template`）ので、担当が移っても同じ名前になる。
// **次の機械は、前の機械が push した続きから始められる。**
//
// issue: 対象の issue。
// 戻り値: branch の名前。組み立てられなければ空文字（hold のコメントの branch が空になるだけ）。
func (o *Orchestrator) branchNameFor(issue tracker.Issue) string {
	name, _, err := workspace.RenderBranch(o.cfg.Herdr.Worktree.BranchTemplate, toIssueRef(issue))
	if err != nil {
		o.logger.Warn("hold のコメントに書く branch 名を組み立てられません",
			"identifier", issue.Identifier, "error", err)
		return ""
	}
	return string(name)
}

// toCommentViews は tracker のコメントを、判定に要る形へ写す。
//
// **更新時刻も写す**（設計 5-3k）。エージェントは進捗の報告を
// **いちばん下にある自分のコメントへ書き足す**ので（設計 5-3j）、
// **これを落とすと、書き続けている機械の持ち回りの期限が1秒も進まない。**
//
// comments: issue に付いているコメントの全件。
// 戻り値: 判定に渡す形の写し。
func toCommentViews(comments []tracker.Comment) []handoff.CommentView {
	out := make([]handoff.CommentView, 0, len(comments))
	for _, c := range comments {
		out = append(out, handoff.CommentView{
			Author:    c.Author,
			Body:      c.Body,
			CreatedAt: c.CreatedAt,
			UpdatedAt: c.UpdatedAt,
		})
	}
	return out
}

// assigneeLogins は issue に付いている担当者のログイン名を返す。
//
// **`AssigneeCount` が返ってきた件数より大きいときは、埋め草を足して人数を合わせる。**
// 取得の窓（`assignees(first: 10)`）に収まらないほど付いている issue を
// 「担当者が1人」と読ませないためである。
//
// issue: 対象の issue。
// 戻り値: 担当者のログイン名（人数ぶん）。
func assigneeLogins(issue tracker.Issue) []string {
	out := make([]string, 0, len(issue.Assignees))
	for _, a := range issue.Assignees {
		if strings.TrimSpace(a.Login) != "" {
			out = append(out, a.Login)
		}
	}
	for len(out) < issue.AssigneeCount {
		out = append(out, unknownAssigneeLogin)
	}
	return out
}

// unknownAssigneeLogin は、人数には数えられているのに名前を取れていない担当者の埋め草である。
//
// **GitHub のログイン名に使えない文字を入れる。**実在のアカウントと取り違えられない形にする。
const unknownAssigneeLogin = "(取得できなかった担当者)"

// assigneeIDOf は、そのログイン名の担当者のノード ID を返す。
//
// issue: 対象の issue。
// login: 探す担当者のログイン名。
// 戻り値の1つ目: ノード ID。
// 戻り値の2つ目: 見つかれば true。
func assigneeIDOf(issue tracker.Issue, login string) (string, bool) {
	for _, a := range issue.Assignees {
		if strings.EqualFold(strings.TrimSpace(a.Login), strings.TrimSpace(login)) && a.ID != "" {
			return a.ID, true
		}
	}
	return "", false
}

// handoffLostOnTurnEnd は、走っている最中に担当が自分でなくなっていないかを確かめる（設計 3-77c）。
//
// **確かめるのは `recheck_interval_ms` に1回だけである**（既定1時間）。
// turn の終わりごとに issue を取り直すと、巡回のリクエストが run の数だけ増える。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値の1つ目: 担当が移っていれば true。
// 戻り値の2つ目: いま担当になっているアカウントのログイン名。
// 1つ目が false なら空文字。**1つ目が true のときは必ず1文字以上あるが、実在のアカウントとは限らない。**
// `assigneeLogins` は、人数には数えられているのに名前を取れなかった担当者を
// `unknownAssigneeLogin` で埋めるので、**その埋め草が返ることがある。**
func (o *Orchestrator) handoffLostOnTurnEnd(ctx context.Context, rs *runState) (bool, string) {
	interval := time.Duration(o.cfg.Tracker.Provider.Handoff.RecheckIntervalMs) * time.Millisecond
	if interval <= 0 {
		return false, ""
	}
	if !rs.handoffRecheckDue(o.now(), interval) {
		return false, ""
	}
	return o.verifyHandoff(ctx, rs)
}

// handoffLostOnResume は、作業を再開する前に担当が自分のままかを確かめる（設計 3-77c）。
//
// **1度も確かめていない run にだけ効く。**復元した run と、この機能より前に着手した run が
// それである。**確かめずに turn を送ると、担当が移っていても丸ごと1回ぶん働く。**
//
// **`recheck_interval_ms` が 0 でも行う。**あれは「走っているあいだ確かめ直すか」の設定であり、
// **再開はそれとは別の場面である**（設計 3-77c の「作業を再開するとき」）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値の1つ目: 担当が移っていれば true。
// 戻り値の2つ目: いま担当になっているアカウントのログイン名。
// 1つ目が false なら空文字。**1つ目が true のときは必ず1文字以上あるが、実在のアカウントとは限らない。**
// `assigneeLogins` は、人数には数えられているのに名前を取れなかった担当者を
// `unknownAssigneeLogin` で埋めるので、**その埋め草が返ることがある。**
func (o *Orchestrator) handoffLostOnResume(ctx context.Context, rs *runState) (bool, string) {
	if !rs.handoffNeverChecked() {
		return false, ""
	}
	return o.verifyHandoff(ctx, rs)
}

// verifyHandoff は、いま担当が自分（この機械）のままかを issue から読み直す（設計 3-77c）。
//
// **担当が移っていたら true を返す。**呼び出し側はその run を止める。
// **push はしない**（設計 3-77c。担当を外された機械が push すると、
// 新しい担当の機械が書いた続きと衝突する）。
//
// **答えを出せたときだけ時計を進める。**進めてしまうと、`gh` に一度届かなかっただけで
// **次の確かめが `recheck_interval_ms` のあとになる**（既定1時間）。
//
// **判定の材料は担当者だけである**（設計 3-77-0）。担当者のアカウントが自分なら、担当も自分である。
// **コメントは判定に使わない。**担当が移ったと分かったあとで、記録をログへ残すためだけに読む。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値の1つ目: 担当が移っていれば true。
// 戻り値の2つ目: いま担当になっているアカウントのログイン名。
// 1つ目が false なら空文字。**1つ目が true のときは必ず1文字以上あるが、実在のアカウントとは限らない。**
// `assigneeLogins` は、人数には数えられているのに名前を取れなかった担当者を
// `unknownAssigneeLogin` で埋めるので、**その埋め草が返ることがある。**
func (o *Orchestrator) verifyHandoff(ctx context.Context, rs *runState) (bool, string) {
	issue := rs.issue()
	nodeID := issueNodeID(issue)
	if nodeID == "" {
		// **draft issue には担当者もコメントも書けない。**判定できないものを判定したことにしない。
		return false, ""
	}
	viewer, ok := o.viewerIdentity(ctx)
	if !ok {
		// **分からないなら止めない。**止めると、`gh` に一度届かなかっただけで
		// 走っている run が捨てられる。
		return false, ""
	}

	current, found, fresh := o.refreshIssue(ctx, rs, false)
	if !found {
		// **issue が見えない。**別の経路（`handleTurnEnd`）が同じ判定で拾う。
		return false, ""
	}
	if !fresh {
		// **取り直せなかった。**`refreshIssue` は失敗すると、着手したときの古い写しを返す。
		// **その写しの担当者はまだ自分なので、見分けずに使うと「担当は自分のまま」と答えてしまう。**
		// **時計も進めない。**進めると、次の確かめが `recheck_interval_ms` のあとになり
		// （既定1時間）、そのあいだ担当を外された run が push まで走り切る（設計 3-77c が禁じている）。
		//
		// **代償は分かって選んでいる。**GitHub が落ちているあいだ、この経路は turn の終わりごとに
		// **1本ずつ取り直しを試み続ける**（時計が進まないので `handoffRecheckDue` が真のままである）。
		// **それでも進めないほうを採る。**払うのは失敗するリクエスト1本で、
		// **進めて払うのは、担当を外された run が1時間 push し続けることである。**
		o.logger.Warn("担当を確かめ直すために issue を取り直せないので、判定しません（この run は止めません）",
			"identifier", issue.Identifier)
		return false, ""
	}
	logins := assigneeLogins(current)
	if len(logins) == 0 {
		// **担当者が1人もいないだけでは止めない。**
		//
		// **「まだ誰も担当していない」と「担当を外された」を見分けられないからである。**
		// 復元した run・この機能より前に着手した run・hold を書けなかった run は、
		// どれも担当者が付いていない。**そこで止めると、走っている run が片端から捨てられる。**
		//
		// **担当が本当に移ったなら、次の機械が入札に勝って担当者になる。**
		// そのときは「他人が担当者」として、この関数が真を返す。
		rs.markHandoffChecked(o.now())
		return false, ""
	}

	// **判定はここで終わる**（設計 3-77-0）。**材料は担当者だけである。**
	// 担当者のアカウントが自分なら、担当しているのも自分である。
	// **同じ GitHub アカウントを複数の機械で使うことはサポートしない**ので、
	// hold を読んでどの機械かを見分ける段は無い。
	//
	// **答えが出たので時計を進める。**この先でコメントを読むが、
	// **読めても読めなくても判定は変わらない。**
	rs.markHandoffChecked(o.now())
	for _, l := range logins {
		if strings.EqualFold(l, viewer.Login) {
			return false, ""
		}
	}

	// **担当者が他人のアカウントになっている。**担当が移ったということである。
	// **いま担当しているのは、その担当者のアカウントで動いている continuo である。**
	//
	// **ここまで来てから、はじめてコメントを読む**（設計 3-77f の「巡回を塞がない」）。
	// **判定には使わない。**外された記録をログへ残すためだけである。
	// **判定の前に読むと、コメントを読めなかっただけで「担当は自分のまま」と答えることになり、
	// 担当を外された run が push まで走り切る**（設計 3-77c が禁じている）。
	o.logReleasedRecord(ctx, issue, nodeID, viewer.Login)
	return true, logins[0]
}

// mayReleaseOwnWork は、自分から担当を手放してよいかを issue から読み直す
// （設計 3-77-0 / 3-27。issue #197）。
//
// **`verifyHandoff` と問いが違う。**あちらは「走っている run を止めてよいか」で、
// **分からないときは止めない側へ倒す**（`gh` に一度届かなかっただけで run を捨てないため）。
// **こちらは「利用者が書いた `git push` を走らせてよいか」で、
// 分からないときは走らせない側へ倒す。**倒す向きが逆なので、関数を分ける。
//
// **担当者が1人もいないときは、手放してよい。**
// **持ち回りで参加者を見分ける値は担当者のログイン名だけである**（設計 3-77-0）ので、
// **担当者が0人なら、その issue は誰のものでもない。**
// 別の continuo が作業を始めるには入札に勝って担当者にならなければならないので、
// **その窓で push しても、誰の作業とも衝突しない。**
// **逆に「自分のものではない」へ倒すと、復元した run・この機能より前に着手した run が、
// `after_run` も `released` も無しに pane を閉じられる**（成果が worktree に残るだけになる）。
//
// **コメントは1件も読まない**（設計 3-77-0 と同じ立場）。読むと、
// **読めなかっただけで答えが変わる。**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値の1つ目: 手放してよいなら true。
// 戻り値の2つ目: 答えを出せたなら true。**偽なら「手放してよい」ではなく「分からない」である。**
// 戻り値の3つ目: 担当が他人になっていたときの、そのアカウントのログイン名。
func (o *Orchestrator) mayReleaseOwnWork(ctx context.Context, rs *runState) (bool, bool, string) {
	issue := rs.issue()
	if issueNodeID(issue) == "" {
		// **draft issue には担当者を書けない。**判定できないものを判定したことにしない。
		return false, false, ""
	}
	viewer, ok := o.viewerIdentity(ctx)
	if !ok {
		return false, false, ""
	}
	current, found, fresh := o.refreshIssue(ctx, rs, false)
	if !found || !fresh {
		// **取り直せなかった写しを使わない。**`refreshIssue` は失敗すると
		// **着手した時点の写しを返す**ので、その担当者はまだ自分である。
		// **見分けずに使うと「担当は自分のまま」という古い答えで push することになる。**
		return false, false, ""
	}
	logins := assigneeLogins(current)
	if len(logins) == 0 {
		return true, true, ""
	}
	for _, l := range logins {
		if strings.EqualFold(l, viewer.Login) {
			return true, true, ""
		}
	}
	return false, true, logins[0]
}

// logReleasedRecord は、この continuo の担当が外された記録が issue にあればログへ残す
// （RUCM「担当が移った」のステップ1）。
//
// **これが無いと「担当が移った」としか残らず、いつ・どのアカウントが外されたのかを辿れない。**
//
// **読めなくても何も止めない。**判定は呼び出し側で既に出ている。
// **切れたかどうかは捨てる。**案内を1回にするための照合はしない（設計 7-1）。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 対象の issue。
// nodeID: 下敷きの GitHub issue のノード ID。
// selfLogin: この continuo が使っている gh の持ち主のログイン名。
func (o *Orchestrator) logReleasedRecord(
	ctx context.Context, issue tracker.Issue, nodeID, selfLogin string,
) {
	comments, _, err := o.tracker.FetchAllComments(ctx, nodeID, o.cfg.Tracker.Provider.Comments)
	if err != nil {
		o.logger.Warn("担当が外された記録を読めません（判定は済んでいるので、この run は止めたままです）",
			"identifier", issue.Identifier, "error", err)
		return
	}
	r, ok := handoff.LatestReleased(toCommentViews(comments))
	if !ok || !strings.EqualFold(strings.TrimSpace(r.From), selfLogin) {
		return
	}
	o.logger.Info("この continuo の担当が外された記録が issue にあります",
		"identifier", issue.Identifier, "外されたアカウント", r.From,
		"branch", r.Branch, "外した時刻", r.At)
}

// stopBecauseHandoffLost は、担当が移った run を止める（設計 3-77c）。
//
// **カンバンへは1バイトも書かない。**Status を動かすと、新しい担当の機械が着手しようと
// しているカンバンを、**外された機械が横から書き換える**ことになる。
//
// **issue へもコメントしない。**この機械はもうこの issue の担当ではない。
// **エージェントに成果を書かせ直す経路（`ensureAgentComment`）も通さない。**
// 通すと、担当でない機械が `failure_state` を書きに行く。
//
// **`workspace_hooks.after_run` も走らせない**（設計 3-77c）。
// **あれは利用者が書いた任意のコマンドであり、`git push` を書いている人がいる。**
// 担当を外された機械が push すると、**新しい担当の機械が書いた続きと衝突する。**
//
// **worktree は消さない。**push していない変更が残っているかもしれず、
// **人間が中身を見て判断できる状態のまま置いておく。**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// newAccount: いま担当になっているアカウントのログイン名。
// **呼び出し元は `verifyHandoff` が真を返したときだけここへ来る**ので、必ず1文字以上ある。
func (o *Orchestrator) stopBecauseHandoffLost(ctx context.Context, rs *runState, newAccount string) {
	if !rs.claimTerminal(ctx) {
		return
	}
	o.stopHandoffLostClaimed(ctx, rs, newAccount)
}

// stopHandoffLostClaimed は、終わらせる最中の標識を確保済みの run について
// `stopBecauseHandoffLost` の中身を行う（issue #197）。
//
// **標識を確保する段だけを外に出したものである。**
// **枠の上限で手放す経路は、既に標識を確保した状態でここへ来る**
// （いったん `endTerminal` で戻して `stopBecauseHandoffLost` を呼び直すと、
// **その隙に別の goroutine が同じ run を掴める**）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// newAccount: いま担当になっているアカウントのログイン名。
func (o *Orchestrator) stopHandoffLostClaimed(ctx context.Context, rs *runState, newAccount string) {
	// **空のときの差し替えは置かない。**`verifyHandoff` は issue に付いている担当者から
	// **`logins[0]` をそのまま返す**ので、真のときに空になる経路が1つも無い。
	// **到達できない差し替えを置くと、読む人が「空になることがある」と読む。**
	o.logger.Warn("担当が移ったので、この turn の終わりで止めます（push しません。カンバンへは書きません。after_run も走らせません）",
		"identifier", rs.issue().Identifier, "いまの担当", newAccount,
		"理由", i18n.T(i18n.KeyHandoffLostReason, newAccount, o.handoffIdleTimeout()))

	// **後片付けは「止めろ」と言われても最後までやる**（`stopAndReleaseAsync` と同じ理由）。
	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx), time.Duration(o.cfg.Herdr.ReadTimeoutMs)*time.Millisecond)
	defer cancel()
	o.stopWorker(cleanupCtx, rs)
	o.release(rs)
}
