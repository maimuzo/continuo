package orchestrator

import (
	"context"
	"strings"
	"time"

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
// 104件のボードでは、30秒の巡回1回が数百リクエストになる。
//
// **使い切ったらその巡回は打ち切る。**候補はボードの並び順で来るので、
// **上から順に見ることは保たれる。**次の巡回で続きを見る。
const maxHandoffFetchesPerPoll = 10

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
// **見るのは担当者（assignee）とコメントの全件だけである。**ボードに新しい欄は足さない。
//
//	担当者が2人以上                                触らない。WARN を出す
//	担当者が無い                                   入札する。勝ったら自分を担当者に加えて hold を書く
//	自分1人 ＋ この機械の hold                      そのまま着手・引き継ぎへ進む（入札しない）
//	自分1人 ＋ hold が1件も無い                     そのまま着手・引き継ぎへ進む（人間が付けた担当である）
//	自分1人 ＋ 別の機械の hold ＋ 期限内             触らない。入札もしない
//	自分1人 ＋ 別の機械の hold ＋ 期限切れ           担当を外し、released を書いてから入札をやり直す
//	他人1人 ＋ hold が1件も無い                     触らない。人間が付けた担当である
//	他人1人 ＋ hold あり ＋ 期限内                   触らない。入札もしない
//	他人1人 ＋ hold あり ＋ 期限切れ                 担当を外し、released を書いてから入札をやり直す
//
// **担当者のアカウントだけで「自分の担当」と決めない**（設計 3-77b）。
// **1人が2台の機械を1つのアカウントで動かすのが、この機能のいちばん自然な使い方である。**
// **どの機械のものかは hold のコメントの `host` が答える。**
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

	logins := assigneeLogins(issue)
	// **コメントを読まずに答えが出るものを先に処理する。**
	//
	// **コメントの取得は issue 1件につき1本以上の GraphQL である。**候補が多いボードで
	// 全件に掛けると、巡回1回のリクエストが候補の数だけ増える（設計 3-31）。
	if len(logins) >= 2 {
		o.logger.Warn("担当者が2人以上いるので触りません（人間が触っています）",
			"identifier", issue.Identifier, "担当者の人数", issue.AssigneeCount,
			"担当者", strings.Join(logins, ", "))
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
		return handoffDecision{}
	}

	if !o.takeHandoffFetch() {
		o.logger.Info("持ち回りの判定に使うコメントの読み取りが、この巡回の上限に達しました（続きは次の巡回で見ます）",
			"identifier", issue.Identifier, "1回の巡回の上限", maxHandoffFetchesPerPoll)
		return handoffDecision{stop: true}
	}
	comments, err := o.tracker.FetchAllComments(ctx, nodeID, o.cfg.Tracker.Provider.Comments)
	if err != nil {
		o.logger.Warn("コメントを読めないので、この巡回では着手しません（担当の持ち回りを判定できません）",
			"identifier", issue.Identifier, "error", err)
		return handoffDecision{}
	}
	views := toCommentViews(comments)

	assessment := handoff.Assess(handoff.Situation{
		Assignees:   logins,
		Comments:    views,
		SelfLogin:   viewer.Login,
		SelfHost:    o.hostName,
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
		o.logger.Info("この担当者の hold が1件も無いので触りません（人間が付けた担当です）",
			"identifier", issue.Identifier, "担当者", assessment.Assignee)
		return handoffDecision{}
	case handoff.ActionSkipSelfUnknown:
		o.logger.Warn("gh の持ち主が分からないので、担当の付いた issue には触りません",
			"identifier", issue.Identifier, "担当者", assessment.Assignee)
		return handoffDecision{}
	case handoff.ActionSkipHeld:
		o.logger.Debug("ほかの機械が期限内で担当しているので触りません（入札もしません）",
			"identifier", issue.Identifier, "担当者", assessment.Assignee,
			"担当者の最後のコメント", assessment.LastActivity)
		return handoffDecision{}
	case handoff.ActionSkipOtherMachine:
		o.logger.Info("担当者は自分のアカウントですが、担当しているのは別の機械なので触りません（入札もしません）",
			"identifier", issue.Identifier, "担当者", assessment.Assignee,
			"担当している機械", assessment.Hold.Host, "この機械", o.hostName,
			"担当者の最後のコメント", assessment.LastActivity)
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
		handoff.Margins{
			FiveHour: o.cfg.Tracker.Provider.Handoff.FiveHourMarginPercent,
			Weekly:   o.cfg.Tracker.Provider.Handoff.WeeklyMarginPercent,
		},
		o.cfg.RateLimit.PauseAbovePercent,
		o.hostName,
		o.now(),
	)
}

// releaseExpiredAssignee は期限の切れた担当を外し、released のコメントを1件書く（設計 3-77c）。
//
// **外すのは名指しした1人だけである。**人間が別の担当者を足していたら、その人は残る。
// **released のコメントの `from` は hold のコメントの `host` から引く。**担当者のログイン名
// しか分からないと、どの機械を止めればよいかが人間に読めない。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 対象の issue。
// nodeID: 下敷きの GitHub issue のノード ID。
// assessment: 期限切れと判定した結果（外す担当者と、読んだ hold が入っている）。
// 戻り値の1つ目: 書いた released のコメントの写し（入札の回の区切りに使う）。
// 戻り値の2つ目: 外せたら true。**外せなければ false**（コメントも書かない）。
func (o *Orchestrator) releaseExpiredAssignee(
	ctx context.Context, issue tracker.Issue, nodeID string, assessment handoff.Assessment,
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
		"外した機械", assessment.Hold.Host,
		"担当者の最後のコメント", assessment.LastActivity,
		"期限", o.handoffIdleTimeout())

	now := o.now()
	body := handoff.FormatReleased(handoff.Released{
		From:   assessment.Hold.Host,
		Branch: assessment.Hold.Branch,
		At:     now,
	})
	view := handoff.CommentView{Body: body, CreatedAt: now}
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
// 戻り値: 勝って担当者になれたか（勝てたときは acquired も真になる）。
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
	window := time.Duration(o.cfg.Tracker.Provider.Handoff.BidWindowMs) * time.Millisecond
	bids := handoff.RoundBids(comments, o.now(), window)
	if _, already := handoff.HasBidBy(bids, o.hostName); !already {
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
	if !strings.EqualFold(winner.Host, o.hostName) {
		o.logger.Info("入札に負けたので着手しません",
			"identifier", issue.Identifier, "勝った機械", winner.Host,
			"勝った判定スコア", winner.Score, "この機械の判定スコア", bid.Score)
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
		Host:     o.hostName,
		Assignee: viewer.Login,
		Branch:   o.branchNameFor(issue),
		At:       o.now(),
	})
	if err := o.postOwnMarkedComment(ctx, nodeID, body); err != nil {
		// **担当者は既に書けている。**hold を書けなかったことで着手を止めない。
		// **次の巡回で書き直せる**（担当者が自分なので、他の機械は触らない）。
		o.logger.Warn("hold のコメントを書けませんでした（担当者は書けています）",
			"identifier", issue.Identifier, "error", err)
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
// ctx: 呼び出しに適用するコンテキスト。
// issue: 対象の issue。
func (o *Orchestrator) undoHandoffAcquire(ctx context.Context, issue tracker.Issue) {
	nodeID := issueNodeID(issue)
	if nodeID == "" {
		return
	}
	viewer, ok := o.viewerIdentity(ctx)
	if !ok {
		o.logger.Warn("gh の持ち主を取れないので、書いた担当者を消し戻せません",
			"identifier", issue.Identifier)
		return
	}
	if _, err := o.tracker.RemoveAssignees(ctx, nodeID, []string{viewer.ID}); err != nil {
		o.logger.Warn("着手を取りやめましたが、書いた担当者を消し戻せません（次の巡回で入札し直します）",
			"identifier", issue.Identifier, "error", err)
		return
	}
	body := handoff.FormatReleased(handoff.Released{
		From:   o.hostName,
		Branch: o.branchNameFor(issue),
		At:     o.now(),
	})
	if err := o.postOwnMarkedComment(ctx, nodeID, body); err != nil {
		o.logger.Warn("担当者を消し戻したことを issue へ書けませんでした",
			"identifier", issue.Identifier, "error", err)
	}
	o.logger.Info("着手を取りやめたので、書いた担当者を消し戻しました",
		"identifier", issue.Identifier, "担当者", viewer.Login)
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
	if err := o.postOwnMarkedComment(ctx, nodeID, handoff.FormatBid(bid)); err != nil {
		o.logger.Warn("入札のコメントを書けません（次の巡回でやり直します）",
			"identifier", issue.Identifier, "error", err)
		return handoff.Bid{}, false
	}
	o.logger.Info("入札しました",
		"identifier", issue.Identifier, "機械", bid.Host,
		"5時間余裕値", bid.FiveHour, "1週間余裕値", bid.Weekly, "判定スコア", bid.Score)
	// **投稿の時刻は自分の時計で埋める。**GitHub が付けた時刻は次の巡回で読み直す。
	// **この写しを使うのはこの巡回の締め切りの計算だけである。**
	bid.PostedAt = bid.At
	return bid, true
}

// handoffIdleTimeout は担当を外すまでの長さを返す。
//
// **0 なら既定の18時間を使う**（設定に書かなくても効く）。
//
// 戻り値: 担当者の最後のコメントからこれだけ経つと担当を外す長さ。
func (o *Orchestrator) handoffIdleTimeout() time.Duration {
	ms := o.cfg.Tracker.Provider.Handoff.IdleTimeoutMs
	if ms <= 0 {
		ms = defaultHandoffIdleTimeoutMs
	}
	return time.Duration(ms) * time.Millisecond
}

// defaultHandoffIdleTimeoutMs は `idle_timeout_ms` が未設定のときに使う長さである（18時間）。
const defaultHandoffIdleTimeoutMs = 64800000

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
// 104件のボードでは、30秒の巡回1回が数百リクエストになる。
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
// comments: issue に付いているコメントの全件。
// 戻り値: 判定に渡す形の写し。
func toCommentViews(comments []tracker.Comment) []handoff.CommentView {
	out := make([]handoff.CommentView, 0, len(comments))
	for _, c := range comments {
		out = append(out, handoff.CommentView{Author: c.Author, Body: c.Body, CreatedAt: c.CreatedAt})
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
// turn の終わりごとに issue のコメントを全部読み直すと、巡回のリクエストが
// run の数だけ増える。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値の1つ目: 担当が移っていれば true。
// 戻り値の2つ目: いま担当になっている機械の名前（読めなければ空文字）。
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
// 戻り値の2つ目: いま担当になっている機械の名前（読めなければ空文字）。
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
// **担当者のアカウントが自分でも、担当しているのは別の機械かもしれない**（設計 3-77b）。
// **1人が2台の機械を1つのアカウントで動かすと、そうなる。**hold のコメントの `host` で見分ける。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値の1つ目: 担当が移っていれば true。
// 戻り値の2つ目: いま担当になっている機械の名前（読めなければ空文字）。
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

	current, found := o.refreshIssue(ctx, rs, false)
	if !found {
		// **issue が見えない。**別の経路（`handleTurnEnd`）が同じ判定で拾う。
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

	// **誰が引き継いだかを、issue の上から読む。**hold のコメントの `host` が答えである。
	// **released のコメントも一緒に記録に残す**（RUCM「担当が移った」のステップ1）。
	// **これが無いと「担当が移った」としか残らず、いつ・どの機械が外されたのかを辿れない。**
	comments, err := o.tracker.FetchAllComments(ctx, nodeID, o.cfg.Tracker.Provider.Comments)
	if err != nil {
		// **読めないなら止めない。**判定の材料が揃っていない。**時計も進めない**
		// （進めると、次の確かめが1時間後になる）。
		o.logger.Warn("担当を確かめ直すためのコメントを読めません（この run は止めません）",
			"identifier", issue.Identifier, "error", err)
		return false, ""
	}
	views := toCommentViews(comments)
	rs.markHandoffChecked(o.now())

	selfAssigned := false
	for _, l := range logins {
		if strings.EqualFold(l, viewer.Login) {
			selfAssigned = true
			break
		}
	}
	if r, ok := handoff.LatestReleased(views); ok && strings.EqualFold(strings.TrimSpace(r.From), o.hostName) {
		o.logger.Info("この機械の担当が外された記録が issue にあります",
			"identifier", issue.Identifier, "外された機械", r.From,
			"branch", r.Branch, "外した時刻", r.At)
	}

	hold, hasHold := handoff.LatestHoldFor(views, logins[0])
	if selfAssigned {
		// **担当者は自分のアカウントである。**担当しているのがこの機械かどうかは、
		// **hold のコメントの `host` でしか分からない。**
		holdHost := strings.TrimSpace(hold.Host)
		if !hasHold || holdHost == "" || strings.EqualFold(holdHost, o.hostName) {
			return false, ""
		}
		return true, holdHost
	}

	newHost := strings.TrimSpace(hold.Host)
	if newHost == "" {
		newHost = logins[0]
	}
	return true, newHost
}

// stopBecauseHandoffLost は、担当が移った run を止める（設計 3-77c）。
//
// **ボードへは1バイトも書かない。**Status を動かすと、新しい担当の機械が着手しようと
// しているボードを、**外された機械が横から書き換える**ことになる。
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
// newHost: いま担当になっている機械の名前（読めなければ空文字）。
func (o *Orchestrator) stopBecauseHandoffLost(ctx context.Context, rs *runState, newHost string) {
	if !rs.claimTerminal(ctx) {
		return
	}
	who := newHost
	if who == "" {
		who = i18n.T(i18n.KeyHandoffLostUnknownHost)
	}
	o.logger.Warn("担当が移ったので、この turn の終わりで止めます（push しません。ボードへは書きません。after_run も走らせません）",
		"identifier", rs.issue().Identifier, "いまの担当", who,
		"理由", i18n.T(i18n.KeyHandoffLostReason, who, o.handoffIdleTimeout()))

	// **後片付けは「止めろ」と言われても最後までやる**（`stopAndReleaseAsync` と同じ理由）。
	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx), time.Duration(o.cfg.Herdr.ReadTimeoutMs)*time.Millisecond)
	defer cancel()
	o.stopWorker(cleanupCtx, rs)
	o.release(rs)
}
