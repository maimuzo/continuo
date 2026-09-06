package orchestrator

import (
	"context"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/tracker"
)

// GateReason は、着手の関門が止めた理由の種類である（issue #134 / #136 / #140）。
//
// **文字列にしてある。**issue のコメントの印に埋め込み、再起動をまたいで照合するため。
type GateReason string

const (
	// GateReasonHumanAssigned は、continuo が付けたのではない担当者が1人付いていることを表す。
	GateReasonHumanAssigned GateReason = "human_assigned"
	// GateReasonManyAssignees は、担当者が2人以上付いていて、
	// そこに gh の持ち主が混じっていないことを表す。
	GateReasonManyAssignees GateReason = "many_assignees"
	// GateReasonManyAssigneesWithSelf は、担当者が2人以上付いていて、
	// そこに gh の持ち主が混じっていることを表す（設計 8-3）。
	//
	// **この理由では issue へ1バイトも書かない。**「人間が2人」と
	// 「人間1人＋別の機械が hold を持っている」を、この分岐は区別できないためである。
	// **だから案内の印も持たない。**ダッシュボードにだけ出す。
	GateReasonManyAssigneesWithSelf GateReason = "many_assignees_with_self"
)

// GateNoticeSkip は、案内を書かないと決めた理由である（issue #134 / #140）。
type GateNoticeSkip string

const (
	// GateNoticeOffByConfig は `on_assignee_gate: warn_only` で切ってあることを表す。
	GateNoticeOffByConfig GateNoticeSkip = "off_by_config"
	// GateNoticeTooManyComments は、手元のコメントが取得の上限で切れていて、
	// 前の起動で書いたかどうかを確かめられなかったことを表す（設計 7）。
	GateNoticeTooManyComments GateNoticeSkip = "too_many_comments"
	// GateNoticeUnclearOwner は、担当者に gh の持ち主が混じっていて、
	// 「人間が2人」と「人間1人＋別の機械が hold を持っている」を
	// 切り分けられなかったことを表す（設計 8-3）。
	GateNoticeUnclearOwner GateNoticeSkip = "unclear_owner"
	// GateNoticeNoBody は、その理由に issue へ書く本文が用意されていないことを表す。
	//
	// **いまの3つの理由では立たない。**理由を1つ足して本文を書き忘れたときにだけ立つ。
	// **黙って既定の本文を投稿しない。**書いたコメントを消す手段が無いので、
	// 中身の無い案内を1件残すより、画面に「本文が無い」と出すほうがよい。
	GateNoticeNoBody GateNoticeSkip = "no_body"
)

// gateNoticeMarker は、案内のコメントの本文に埋め込む印を作る（設計 3）。
//
// **理由ごとに違う印になる。**「1回だけ」は理由ごとの約束なので、
// 印が共通だと片方の理由の案内がもう一方を塞ぐ。
//
// reason: 止めた理由の種類。
// 戻り値: `<!-- continuo:gated:human_assigned -->` のような印。
func gateNoticeMarker(reason GateReason) string {
	return "<!-- continuo:gated:" + string(reason) + " -->"
}

// noticeMinCount は、案内を書くまでに同じ理由で止めた回数である（設計 3-68）。
//
// **1回目では書かない。**人間が担当者を付け替えている最中の1巡回で書くと、
// 数秒で解消する状態に永久に残るコメントを1件足すことになる。
const noticeMinCount = 3

// noticeMinAge は、最初に止めてから案内を書くまでに空ける時間である（設計 3-68）。
//
// **回数だけでは足りない。**`polling.interval_ms` を短くした環境では
// 3回が数秒で埋まる。**時間だけでも足りない。**巡回が止まっていた間に経った時間は
// 「止まり続けている」の証拠にならない。
const noticeMinAge = 60 * time.Second

// gateNotice は、1つの理由についての案内の状態である（issue #134 / #140）。
//
// **NoticedAt と Skip が同時に入ることは無い。**どちらかが入っていれば noteGate は偽を返す。
type gateNotice struct {
	// NoticedAt は issue へ案内を書いた時刻である。ゼロ値ならまだ書いていない。
	// **`warn_only` では立てない**（設計 7）。立てると「書いた」と読める。
	NoticedAt time.Time
	// Skip は、案内を書かないと決めた理由である。空なら「まだ書いていない」。
	Skip GateNoticeSkip
	// Failed は、案内を投稿しようとして失敗したことを表す。
	//
	// **NoticedAt は立てたままにする**（設計 8-2）。投稿の成否で印を分けると、
	// 投稿が失敗し続ける issue へ巡回のたびにコメントを積むことになる。
	// **そのうえで、画面には別の印を出す。**
	// 「書いた」と「書こうとして失敗した」が見分けられないと、
	// **issue に1件も無いのに「案内済み」と読める行がダッシュボードに残る。**
	Failed bool
}

// done は、この理由についてもう案内を決め終えているかを返す。
//
// 戻り値: 書いたか、書かないと決めていれば真。
func (n gateNotice) done() bool { return !n.NoticedAt.IsZero() || n.Skip != "" }

// gateNote は、着手の関門で止めた issue 1件の記録である（設計 3-68）。
//
// **永続化しない。**再起動すると消えるが、次の巡回で作り直される。
// **判定には1度も使わない。**ダッシュボードと「案内を1回だけ」のためだけに持つ。
type gateNote struct {
	// Identifier は `<owner>/<repo>#<番号>` である。
	Identifier string
	// Title は issue の題名である。**外部から来る文字列である。**
	Title string
	// URL は issue の URL である。**draft issue はここへ来ない**
	// （`handoffGate` の入口がノード ID の無い issue を `proceed: true` で返して抜ける）。
	// **空文字になるのは URL を取れなかったときだけで、そのときは画面をリンクにしない。**
	URL string
	// Reason は、最後に止めたときの理由の種類である。
	Reason GateReason
	// Assignees は担当者のログイン名である。**巡回のたびに写し直す。**
	// **コメントには書かない**（設計 8-1）。ダッシュボードだけが出す。
	Assignees []string
	// Count は、**いまの Reason で**止めた回数である（案内を書く条件に使う。設計 3-68）。
	// **理由が変わったら1へ戻す**（設計 6-5）。
	Count int
	// FirstSeenAt は、**いまの Reason で**最初に止めた時刻である。**同じ理由なら更新しない。**
	// **理由が変わったら、その巡回の時刻で置き換える**（設計 6-5）。
	FirstSeenAt time.Time
	// LastSeenAt は、理由を問わず最後に止めた時刻である。
	LastSeenAt time.Time
	// notices は、**理由ごとの**案内の状態である（設計 6-5）。
	//
	// **理由が変わっても消さない。**案内は `<!-- continuo:gated:<理由> -->` の印を持つので、
	// 「1回だけ」は理由ごとに数える。
	// **理由が変わるたびに消すと、担当者を2人と1人で往復させただけで同じ案内が積まれる。**
	// **逆に issue ごとに1つしか持たないと、先に書いたほうの理由が、もう一方の案内を永久に塞ぐ。**
	notices map[GateReason]gateNotice
}

// noteGate は、着手の関門で止めたことを記録し、issue へ案内を書くべきかを返す（設計 3-68）。
//
// **判定には1度も使わない。**ダッシュボードに出すためと、案内を1回だけにするために持つ。
// **理由が変わったら Count と FirstSeenAt だけを数え直す。**理由ごとの案内の状態
// （notices）は持ち越す（設計 6-5）。
//
// issue: 止めた issue。
// reason: 止めた理由の種類。
// assignees: いま付いている担当者のログイン名。
// 戻り値: 案内をまだ書いておらず、書く条件（noticeMinCount / noticeMinAge）を満たしていれば true。
func (o *Orchestrator) noteGate(issue tracker.Issue, reason GateReason, assignees []string) bool {
	now := o.now()
	url := ""
	if issue.URL != nil {
		url = *issue.URL
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	n := o.gated[issue.ID]
	if n == nil {
		n = &gateNote{notices: map[GateReason]gateNotice{}}
		o.gated[issue.ID] = n
	}
	if n.notices == nil {
		n.notices = map[GateReason]gateNotice{}
	}
	n.Identifier = issue.Identifier
	n.Title = issue.Title
	n.URL = url
	// **受け取ったスライスをそのまま持たない**（設計 10）。
	// `o.mu` の外に出たスライスへ書き込むと、ダッシュボードが読んでいる最中の値が変わる。
	n.Assignees = append([]string(nil), assignees...)
	n.LastSeenAt = now
	if n.Reason != reason || n.Count == 0 {
		// **理由が変わった。**この理由自体が3巡回と60秒持ちこたえてから案内する（設計 6-5）。
		n.Reason = reason
		n.Count = 1
		n.FirstSeenAt = now
	} else {
		n.Count++
	}

	if n.notices[reason].done() {
		return false
	}
	return n.Count >= noticeMinCount && !now.Before(n.FirstSeenAt.Add(noticeMinAge))
}

// clearGate は、着手の関門で止めていることを取り消す（設計 3-68）。
//
// **「判定できた」うえで「関門の理由では止めていない」ときだけ呼ぶ。**
// 「この巡回で見なかった」で呼んではならない（設計 6）。
//
// **案内を書いた事実（notices）は残す**（設計 6-5）。
// **issue に残っているコメントは、この機械が関門より前で飛ばしたくらいでは消えない。**
// ここで消すと、`preflight` が1巡回だけ落ちただけで記録が作り直され、
// **同じ本文の案内の2件目が issue へ書かれる。**書いたコメントを消す手段は無い。
// **notices ごと落ちるのは `forgetGatedNotOnBoard` だけである**（issue がボードから消えたとき）。
//
// issueID: project item の ID。
func (o *Orchestrator) clearGate(issueID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := o.gated[issueID]
	if n == nil {
		return
	}
	if len(n.notices) == 0 {
		// **案内を1件も書いていないなら、残すものが無い。**map を膨らませない。
		delete(o.gated, issueID)
		return
	}
	// **Reason を空にすることが「いまは関門で止めていない」の印である。**
	// `GateViews` はこの状態を1件も返さないので、ダッシュボードからは消える。
	n.Reason = ""
	n.Count = 0
	n.FirstSeenAt = time.Time{}
	n.Assignees = nil
}

// forgetGateNoticesOffByConfig は、「設定で書かないと決めた」記録だけを捨てる（設計 3-24）。
//
// **`tracker.provider.handoff.on_assignee_gate` を読み直しで戻したときに呼ぶ。**
//
// **捨てる理由は、ダッシュボードに文言として出ているからである。**
// `internal/server/view.go` が `GateNoticeOffByConfig` を「設定で書きません」という表示に変える。
// **捨てないと、`warn_and_comment` へ戻したあとも、いまは嘘になった説明を画面が出し続ける。**
// **併せて、`warn_only` のあいだに止まっていた issue へ案内が書かれるようになる。**
//
// **「読み直しが再起動より弱い」だけでは、この関数の理由にならない。**同じ弱さは
// 書き戻しの回数（`runState` の `automatedRewrites`）にもあり、そちらは直していない。
// **違いは、こちらだけが画面に出ていることである。**
//
// **`NoticedAt` が入っている記録は捨てない。**捨てると、同じ本文の案内の2件目が issue へ書かれる。
// **書いたコメントを消す手段は無い。**
func (o *Orchestrator) forgetGateNoticesOffByConfig() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, n := range o.gated {
		for reason, notice := range n.notices {
			if notice.Skip == GateNoticeOffByConfig {
				delete(n.notices, reason)
			}
		}
	}
}

// markGateNoticed は、issue に既に案内があったことを記録する。**投稿はしない。**
//
// issueID: project item の ID。
// reason: どの理由の案内か。**理由ごとに1つずつ持つ**（設計 6-5）。
// at: 見つかった案内のコメントの時刻。
func (o *Orchestrator) markGateNoticed(issueID string, reason GateReason, at time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := o.gated[issueID]
	if n == nil {
		return
	}
	if n.notices == nil {
		n.notices = map[GateReason]gateNotice{}
	}
	n.notices[reason] = gateNotice{NoticedAt: at}
}

// markGateNoticeSkipped は、案内を書かないと決めたことを記録する。**投稿はしない。**
//
// issueID: project item の ID。
// reason: どの理由の案内か。**理由ごとに1つずつ持つ**（設計 6-5）。
// skip: 書かないと決めた理由。
func (o *Orchestrator) markGateNoticeSkipped(issueID string, reason GateReason, skip GateNoticeSkip) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := o.gated[issueID]
	if n == nil {
		return
	}
	if n.notices == nil {
		n.notices = map[GateReason]gateNotice{}
	}
	n.notices[reason] = gateNotice{Skip: skip}
}

// postGateNotice は、案内を issue へ1回だけ書く（設計 3-68）。
//
// **設定を先に見る。**`warn_only` のときは NoticedAt を立てない。
// 立てると「issue へ書いた」と読める印が付き、ダッシュボードから
// 「書いていない」ことが読めなくなる。代わりに Skip へ "off_by_config" を入れる。
// **投稿に失敗しても NoticedAt は残す**（設計 8-2）。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 止めた issue。
// nodeID: 下敷きの GitHub issue のノード ID。
// reason: 止めた理由の種類。
func (o *Orchestrator) postGateNotice(ctx context.Context, issue tracker.Issue, nodeID string, reason GateReason) {
	// **この値は走行中に読み直せる**（設計 3-24）。`o.cfg` から読んではならない。
	if o.reloadableConfig().OnAssigneeGate == config.OnAssigneeGateWarnOnly {
		o.logger.Info("着手できずに止まっていますが、設定で issue へは書きません",
			"identifier", issue.Identifier, "理由", string(reason),
			"on_assignee_gate", config.OnAssigneeGateWarnOnly)
		o.markGateNoticeSkipped(issue.ID, reason, GateNoticeOffByConfig)
		return
	}
	body := buildGatedComment(reason)
	if body == "" {
		// **本文を持たない理由がある**（`many_assignees_with_self`。設計 8-3）。
		// **空の本文を投稿しない。**ここへ来るのは呼び出し側の間違いなので、名指しで残す。
		o.logger.Warn("この理由には issue へ書く本文がありません（案内は書きません）",
			"identifier", issue.Identifier, "理由", string(reason))
		o.markGateNoticeSkipped(issue.ID, reason, GateNoticeNoBody)
		return
	}
	// **投稿の前に印を付ける**（設計 8-2）。投稿の成否で印を分けると、
	// 投稿が失敗し続ける issue へ巡回のたびにコメントを積むことになる。
	o.markGateNoticed(issue.ID, reason, o.now())
	if err := o.postComment(ctx, nodeID, body); err != nil {
		o.logger.Warn("着手できずに止まっていることを issue へ書けませんでした",
			"identifier", issue.Identifier, "理由", string(reason), "error", err)
		// **印は残したまま、失敗したことだけを足す**（設計 8-2）。
		o.markGateNoticeFailed(issue.ID, reason)
	}
}

// markGateNoticeFailed は、案内の投稿に失敗したことを記録する。
//
// **NoticedAt は消さない。**消すと、投稿が失敗し続ける issue へ
// 巡回のたびにコメントを積むことになる。
//
// issueID: project item の ID。
// reason: どの理由の案内か。
func (o *Orchestrator) markGateNoticeFailed(issueID string, reason GateReason) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := o.gated[issueID]
	if n == nil {
		return
	}
	if n.notices == nil {
		n.notices = map[GateReason]gateNotice{}
	}
	notice := n.notices[reason]
	notice.Failed = true
	n.notices[reason] = notice
}

// gateNoticedIn は、手元のコメントの中に continuo が書いた案内があるかを探す（設計 3-65 / 7）。
//
// **印だけで「continuo が書いた」と決めない。**投稿者が gh の持ち主であるものだけを数える。
// **持ち主が空文字なら常に偽を返す。**照合できないまま印を信じると、
// 第三者が同じ印を書いただけで案内を止められる。
//
// comments: `FetchAllComments` が返した全件。
// selfLogin: gh の持ち主のログイン名。
// reason: 探す理由の種類。
// 戻り値: 見つかったコメントの時刻と、見つかったかどうか。
func gateNoticedIn(comments []tracker.Comment, selfLogin string, reason GateReason) (time.Time, bool) {
	if strings.TrimSpace(selfLogin) == "" {
		return time.Time{}, false
	}
	marker := gateNoticeMarker(reason)
	for _, c := range comments {
		if !strings.EqualFold(strings.TrimSpace(c.Author), strings.TrimSpace(selfLogin)) {
			continue
		}
		if strings.Contains(c.Body, marker) {
			return c.CreatedAt, true
		}
	}
	return time.Time{}, false
}

// forgetGatedNotOnBoard は、いまのボードの候補に居ない issue の記録を外す（設計 3-68）。
//
// **候補の取得が成功した巡回でだけ呼ぶ。**失敗した巡回で呼ぶと全件が消える。
// **`dispatchCandidates` のループは見ない。**空きスロットが尽きて途中で
// 打ち切られた巡回でも、記録は1件も減らない。
//
// candidates: `FetchIssuesByStates` が返した候補（全件）。
func (o *Orchestrator) forgetGatedNotOnBoard(candidates []tracker.Issue) {
	onBoard := make(map[string]struct{}, len(candidates))
	for _, issue := range candidates {
		onBoard[issue.ID] = struct{}{}
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	for id := range o.gated {
		if _, ok := onBoard[id]; !ok {
			delete(o.gated, id)
		}
	}
}

// GateView は、着手の関門で止めた issue の写しである（issue #134）。
//
// **`o.gated` の中身をそのまま渡さない**（設計 3-25）。呼ばれた時点の値を写して返す。
type GateView struct {
	// Identifier は issue の識別子（`<owner>/<repo>#<番号>`）である。
	Identifier string
	// Title は issue の題名である。**外部から来る文字列である。**
	Title string
	// URL は issue の URL である。**空文字なら画面はリンクにしない**（取れなかったときだけ空になる）。
	URL string
	// Reason は止めた理由の種類である。**文言に直すのは internal/server である。**
	Reason GateReason
	// Assignees は担当者のログイン名である。**新しいスライスへ写したものである。**
	Assignees []string
	// Since は最初にこの理由で止めた時刻である。
	Since time.Time
	// Noticed は、**いまの Reason について** issue へ案内を書き終えているかである
	// （`notices[Reason].NoticedAt` が入っているか。設計 6-5）。
	Noticed bool
	// NoticeSkip は、**いまの Reason について**案内を書かないと決めた理由である
	// （空なら「まだ書いていない」。`notices[Reason].Skip` の写し）。
	NoticeSkip GateNoticeSkip
	// NoticeFailed は、案内を投稿しようとして失敗したことを表す。
	// **Noticed も同時に真になる**（印は残す。設計 8-2）。
	NoticeFailed bool
}

// GateViews は、着手の関門で止めた issue の写しを返す（順序は不定）。
//
// 戻り値: 記録の写し。**呼び出し側が書き換えても o.gated は変わらない。**
func (o *Orchestrator) GateViews() []GateView {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]GateView, 0, len(o.gated))
	for _, n := range o.gated {
		if n.Reason == "" {
			// **いまは関門で止めていない**（`clearGate` が案内の記録だけを残した状態）。
			// **画面には出さない。**止まっていないものを「止まっている」と出さない。
			continue
		}
		notice := n.notices[n.Reason]
		out = append(out, GateView{
			Identifier: n.Identifier,
			Title:      n.Title,
			URL:        n.URL,
			Reason:     n.Reason,
			// **スライスは写し直す。**構造体の代入ではヘッダしか写らない（設計 10）。
			Assignees:    append([]string(nil), n.Assignees...),
			Since:        n.FirstSeenAt,
			Noticed:      !notice.NoticedAt.IsZero(),
			NoticeSkip:   notice.Skip,
			NoticeFailed: notice.Failed,
		})
	}
	return out
}
