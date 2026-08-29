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

// handoffGate は「この機械がこの issue を処理してよいか」を、着手の前に決める
// （設計 3-77 / 3-77a / 3-77b / 3-77c）。
//
// **見るのは担当者（assignee）とコメントの全件だけである。**ボードに新しい欄は足さない。
//
//	担当者が2人以上                    触らない。WARN を出す
//	担当者が無い                       入札する。勝ったら自分を担当者に加えて hold を書く
//	担当者が自分1人                    そのまま着手・引き継ぎへ進む（入札しない）
//	他人1人 ＋ hold が1件も無い         触らない。人間が付けた担当である
//	他人1人 ＋ hold あり ＋ 期限内       触らない。入札もしない
//	他人1人 ＋ hold あり ＋ 期限切れ     担当を外し、released を書いてから入札をやり直す
//
// **締め切りを待つあいだ、巡回はブロックしない。**入札を1件書いたら偽を返して次の巡回へ譲り、
// 締め切りが過ぎた巡回で勝敗を決める。**締め切りは issue のコメントから読める**
// （いちばん古い入札の時刻 + `bid_window_ms`）ので、待ちを記憶に持たなくてよい。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 着手しようとしている issue。
// 戻り値: この機械が着手してよければ true。
func (o *Orchestrator) handoffGate(ctx context.Context, issue tracker.Issue) bool {
	nodeID := issueNodeID(issue)
	if nodeID == "" {
		// **draft issue にはコメントも担当者も書けない。**そもそも Dispatchable が偽なので
		// ここへは来ないが、来たら止めない（判定できないものを判定したことにしない）。
		return true
	}

	logins := assigneeLogins(issue)
	// **コメントを読まずに答えが出る2つを先に処理する。**
	//
	// **コメントの取得は issue 1件につき1本以上の GraphQL である。**候補が多いボードで
	// 全件に掛けると、巡回1回のリクエストが候補の数だけ増える（設計 3-31）。
	// **担当者が2人以上のときと、担当者が自分1人のときは、コメントを1件も読まずに決まる。**
	if len(logins) >= 2 {
		o.logger.Warn("担当者が2人以上いるので触りません（人間が触っています）",
			"identifier", issue.Identifier, "担当者の人数", issue.AssigneeCount,
			"担当者", strings.Join(logins, ", "))
		return false
	}

	viewer, ok := o.viewerIdentity(ctx)
	if !ok {
		// **自分が誰か分からないまま担当のある issue に触らない**（設計 3-65 と同じ立場）。
		o.logger.Warn("gh の持ち主を取れないので、この巡回では着手しません（担当の持ち回りを判定できません）",
			"identifier", issue.Identifier)
		return false
	}
	if len(logins) == 1 && strings.EqualFold(logins[0], viewer.Login) {
		return true
	}

	comments, err := o.tracker.FetchAllComments(ctx, nodeID, o.cfg.Tracker.Provider.Comments)
	if err != nil {
		o.logger.Warn("コメントを読めないので、この巡回では着手しません（担当の持ち回りを判定できません）",
			"identifier", issue.Identifier, "error", err)
		return false
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
		return true
	case handoff.ActionSkipManyAssignees:
		// **上の早い戻りで既に処理している。**ここへは来ない。
		return false
	case handoff.ActionSkipHumanAssigned:
		o.logger.Info("hold のコメントが1件も無い担当なので触りません（人間が付けた担当です）",
			"identifier", issue.Identifier, "担当者", assessment.Assignee)
		return false
	case handoff.ActionSkipSelfUnknown:
		o.logger.Warn("gh の持ち主が分からないので、担当の付いた issue には触りません",
			"identifier", issue.Identifier, "担当者", assessment.Assignee)
		return false
	case handoff.ActionSkipHeld:
		o.logger.Debug("ほかの機械が期限内で担当しているので触りません（入札もしません）",
			"identifier", issue.Identifier, "担当者", assessment.Assignee,
			"担当者の最後のコメント", assessment.LastActivity)
		return false
	case handoff.ActionRelease:
		if !o.releaseExpiredAssignee(ctx, issue, nodeID, assessment) {
			return false
		}
		// **担当を外したので、担当者が1人もいない状態から入札をやり直す**
		// （RUCM「他人の担当」のステップ6 → 基本フローのステップ6）。
	case handoff.ActionBid:
	}

	return o.bidForIssue(ctx, issue, nodeID, viewer, views)
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
// 戻り値: 外せたら true。**外せなければ false**（コメントも書かない）。
func (o *Orchestrator) releaseExpiredAssignee(
	ctx context.Context, issue tracker.Issue, nodeID string, assessment handoff.Assessment,
) bool {
	id, ok := assigneeIDOf(issue, assessment.Assignee)
	if !ok {
		o.logger.Warn("外す担当者のノード ID を引けないので、担当を外しません",
			"identifier", issue.Identifier, "担当者", assessment.Assignee)
		return false
	}
	if _, err := o.tracker.RemoveAssignees(ctx, nodeID, []string{id}); err != nil {
		o.logger.Warn("期限の切れた担当を外せません（次の巡回でやり直します）",
			"identifier", issue.Identifier, "担当者", assessment.Assignee, "error", err)
		return false
	}
	o.logger.Info("期限の切れた担当を外しました（入札をやり直します）",
		"identifier", issue.Identifier, "外した担当者", assessment.Assignee,
		"外した機械", assessment.Hold.Host,
		"担当者の最後のコメント", assessment.LastActivity,
		"期限", o.handoffIdleTimeout())

	body := handoff.FormatReleased(handoff.Released{
		From:   assessment.Hold.Host,
		Branch: assessment.Hold.Branch,
		At:     o.now(),
	})
	if err := o.postOwnMarkedComment(ctx, nodeID, body); err != nil {
		// **担当は既に外れている。**コメントを書けなかったことで入札を止めない
		// （止めると、担当者のいない issue が誰にも拾われなくなる）。
		o.logger.Warn("担当を外したことを issue へ書けませんでした",
			"identifier", issue.Identifier, "error", err)
	}
	return true
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
// comments: issue に付いているコメントの全件。
// 戻り値: 勝って担当者になれたら true。
func (o *Orchestrator) bidForIssue(
	ctx context.Context,
	issue tracker.Issue,
	nodeID string,
	viewer tracker.Assignee,
	comments []handoff.CommentView,
) bool {
	bid, skip := handoff.Evaluate(
		o.quotaSnapshot(),
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
	if skip != handoff.SkipNone {
		// **黙るだけである。**ほかの機械はこの機械を待たない。
		o.logger.Info("入札しません（この issue は他の機械に任せます）",
			"identifier", issue.Identifier, "理由", skip.String())
		return false
	}

	// **数えるのは、いまの回の入札だけである**（設計 3-77e）。
	// **前の回の入札は issue に残り続ける**（1回ごとに新しいコメントを書くので消えない）。
	// 数に入れると、締め切りが常にその古い時刻から数えられ、**次の回が1度も始まらない。**
	// **巡回のたびに入札のコメントだけが増え、担当者は永久に決まらない。**
	window := time.Duration(o.cfg.Tracker.Provider.Handoff.BidWindowMs) * time.Millisecond
	bids := handoff.RoundBids(comments, o.now(), window)
	if _, already := handoff.HasBidBy(bids, o.hostName); !already {
		posted, ok := o.postBid(ctx, issue, nodeID, bid)
		if !ok {
			return false
		}
		bids = append(bids, posted)
	}

	deadline, ok := handoff.Deadline(bids, window)
	if !ok {
		// **入札が1件も無い。**自分の入札を書けなかったということなので、次の巡回でやり直す。
		return false
	}
	if o.now().Before(deadline) {
		o.logger.Debug("入札の締め切りを待ちます",
			"identifier", issue.Identifier, "締め切り", deadline, "届いている入札", len(bids))
		return false
	}

	winner, ok := handoff.Winner(handoff.BidsBefore(bids, deadline))
	if !ok {
		return false
	}
	if !strings.EqualFold(winner.Host, o.hostName) {
		o.logger.Info("入札に負けたので着手しません",
			"identifier", issue.Identifier, "勝った機械", winner.Host,
			"勝った判定スコア", winner.Score, "この機械の判定スコア", bid.Score)
		return false
	}

	if _, err := o.tracker.AddAssignees(ctx, nodeID, []string{viewer.ID}); err != nil {
		o.logger.Warn("入札に勝ちましたが担当者を書けません（次の巡回でやり直します）",
			"identifier", issue.Identifier, "error", err)
		return false
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
// ctx: 呼び出しに適用するコンテキスト。
// 戻り値の1つ目: 持ち主。
// 戻り値の2つ目: 取れれば true。
func (o *Orchestrator) viewerIdentity(ctx context.Context) (tracker.Assignee, bool) {
	o.mu.Lock()
	cached := o.viewer
	o.mu.Unlock()
	if cached.ID != "" && cached.Login != "" {
		return cached, true
	}

	v, err := o.tracker.FetchViewer(ctx)
	if err != nil || v.ID == "" || v.Login == "" {
		if err != nil {
			o.logger.Warn("gh の持ち主を GraphQL から取れません", "error", err)
		}
		return tracker.Assignee{}, false
	}
	o.mu.Lock()
	o.viewer = v
	o.mu.Unlock()
	return v, true
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

// handoffLost は、走っている最中に担当が自分でなくなっていないかを確かめる（設計 3-77c）。
//
// **確かめるのは `recheck_interval_ms` に1回だけである**（既定1時間）。
// turn の終わりごとに issue のコメントを全部読み直すと、巡回のリクエストが
// run の数だけ増える。
//
// **担当が移っていたら true を返す。**呼び出し側はその turn の終わりで run を止める。
// **push はしない**（設計 3-77c。担当を外された機械が push すると、
// 新しい担当の機械が書いた続きと衝突する）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値の1つ目: 担当が移っていれば true。
// 戻り値の2つ目: いま担当になっている機械の名前（読めなければ空文字）。
func (o *Orchestrator) handoffLost(ctx context.Context, rs *runState) (bool, string) {
	interval := time.Duration(o.cfg.Tracker.Provider.Handoff.RecheckIntervalMs) * time.Millisecond
	if interval <= 0 {
		return false, ""
	}
	if !rs.handoffRecheckDue(o.now(), interval) {
		return false, ""
	}

	issue := rs.issue()
	nodeID := issueNodeID(issue)
	if nodeID == "" {
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
	for _, l := range logins {
		if strings.EqualFold(l, viewer.Login) {
			return false, ""
		}
	}
	if len(logins) == 0 {
		// **担当者が1人もいないだけでは止めない。**
		//
		// **「まだ誰も担当していない」と「担当を外された」を見分けられないからである。**
		// 復元した run・この機能より前に着手した run・hold を書けなかった run は、
		// どれも担当者が付いていない。**そこで止めると、走っている run が片端から捨てられる。**
		//
		// **担当が本当に移ったなら、次の機械が入札に勝って担当者になる。**
		// そのときは「他人が担当者」として、この関数が真を返す。
		return false, ""
	}

	// **誰が引き継いだかを、issue の上から読む。**hold のコメントの `host` が答えである。
	// **released のコメントも一緒に記録に残す**（RUCM「担当が移った」のステップ1）。
	// **これが無いと「担当が移った」としか残らず、いつ・どの機械が外されたのかを辿れない。**
	newHost := ""
	if comments, err := o.tracker.FetchAllComments(ctx, nodeID, o.cfg.Tracker.Provider.Comments); err == nil {
		views := toCommentViews(comments)
		if h, ok := handoff.LatestHold(views); ok {
			newHost = h.Host
		}
		if r, ok := handoff.LatestReleased(views); ok {
			o.logger.Info("担当が外された記録が issue にあります",
				"identifier", issue.Identifier, "外された機械", r.From,
				"branch", r.Branch, "外した時刻", r.At)
		}
	}
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
	o.logger.Warn("担当が移ったので、この turn の終わりで止めます（push しません。ボードへは書きません）",
		"identifier", rs.issue().Identifier, "いまの担当", who,
		"理由", i18n.T(i18n.KeyHandoffLostReason, who, o.handoffIdleTimeout()))

	// **後片付けは「止めろ」と言われても最後までやる**（`stopAndReleaseAsync` と同じ理由）。
	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx), time.Duration(o.cfg.Herdr.ReadTimeoutMs)*time.Millisecond)
	defer cancel()
	o.runAfterRun(cleanupCtx, rs)
	o.stopWorker(cleanupCtx, rs)
	o.release(rs)
}
