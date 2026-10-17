package orchestrator

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/atomicfile"
	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/marginstatus"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// usableQuotaWindow は、最後に使用率を読めてから、判定に使ってよい長さである（issue #262。設計 3-27a）。
//
// **新しさの幅の既定（5分）に読み取りの失敗を数回はさんでも収まり、60分の打ち切りより短い長さとして決めた。**
// **新しい値（`quotaForBid`）だけで判定すると、どれか1つの期間のリセットの時刻を過ぎるたびに、次に読むまで
// 判定が全部止まる。**そのあいだ着手の前の判定が効かず、「行き先を待つ」run の印が下りる。
// **逆に長くしすぎると、アカウントを替えた直後に、前のアカウントの古い値で切り上げ続ける。**
// この長さで、その害も最長30分に抑える。
const usableQuotaWindow = 30 * time.Minute

// quotaReads は、保管値の錠を1回だけ取って作る3つの写しである（issue #262。設計 3-27a）。
//
// **3つを別々の錠で読んではならない。**`OnStatusline` は statusline の受け口の goroutine から
// 同じ錠を取って保管値を差し替えるので、読むあいだに差し替わると、3つが食い違う
// （`quota.go` の `quotaForPoll` の説明と同じ理由。巡回はいまはこちらを使い、`quotaForPoll` は呼ばない）。
type quotaReads struct {
	// usable は「使える値」である。最後に読めてから `usableQuotaWindow` 以内なら、保管値のうち、
	// リセットの時刻を過ぎていない期間と、リセットの時刻を読めない期間（仮の期限を過ぎていても）を持つ。
	// **それより古いとき（立て直してからまだ読めていないときも）は nil**（使用率を読めていない）。
	//
	// **マージンを切ったかの判定（turn の終わり・着手の前・巡回の「行き先を待つ」run・`margin-status`・
	// ダッシュボード・打ち切りの文面）は、全部これを使う。**
	usable *ratelimit.Snapshot
	// fresh は新しい値（今の `quotaForBid` と同じもの）である。新しくなければ nil。
	//
	// **担当を外す判定は、これでしか行わない。**担当を外すと戻せないためである。
	fresh *ratelimit.Snapshot
	// stale は新しさを問わない写し（今の `quotaForPoll` の1つ目と同じもの）である。
	//
	// **使用率100%の待ち（`isQuotaWaitingWith` と `clearQuotaWaitWhenBack`）は、今どおりこれを使う。**
	// 30分の切れ目を当てると、使用率を30分以上読めない機械で、使用率100%で待っていた run が
	// 「固まった」として打ち切られる（設計 3-77i の「読めないことを理由に走行中の run を捨てない」）。
	stale *ratelimit.Snapshot
	// lastReadAt は新しさの時刻（最後に使用率を読めた時刻）である。立て直してからまだ読めていなければゼロ。
	lastReadAt time.Time
}

// readQuota は、保管値の錠を1回だけ取って3つの写しを作る（issue #262。設計 3-27a）。
//
// **`quota.go` には触らない。**同じ package の別ファイルから保管値を読むだけである。
//
// **仮の期限の期間を「使える値」に残すのは、ここだけである。**`snapshotOf` は、仮の期限
// （既定でおよそ5分30秒）を過ぎた期間を落とすので、usage API が数分読めないだけで、リセットの時刻を
// 読めない期間（ふつうはモデル別の1週間の期間）が判定から消える。**その期間だけでマージンを切っていた run が、
// 30分を待たずに作業へ戻ってしまう。**
//
// 戻り値: 3つの写し。
func (o *Orchestrator) readQuota() quotaReads {
	now := o.now()
	o.quotaMu.Lock()
	defer o.quotaMu.Unlock()
	var r quotaReads
	r.lastReadAt = o.quota.freshAt
	r.stale = snapshotOf(o.quota.windows, now, o.quota.freshAt)
	if o.quotaFreshLocked(now) {
		r.fresh = snapshotOf(o.quota.windows, now, now)
	}
	if !o.quota.freshAt.IsZero() && now.Sub(o.quota.freshAt) <= usableQuotaWindow {
		r.usable = usableSnapshotOf(o.quota.windows, now, o.quota.freshAt)
	}
	return r
}

// usableSnapshotOf は保管値から「使える値」の写しを作る（issue #262。設計 3-27a）。
//
// **`snapshotOf` との違いは、仮の期限の期間を、期限を過ぎていても残すことだけである。**
// 本物のリセットの時刻を過ぎた期間は除く（その期間はリセットされている）。
// **仮の期限の期間は `ResetsAt` を nil にして出す**（`snapshotOf` と同じ。リセットの時刻を読めない）。
//
// windows: 保管値。
// now: いまの時刻。
// fetchedAt: 写しに載せる時刻（新しさの時刻）。
// 戻り値: 写し。期間が1つも無ければ nil。
func usableSnapshotOf(windows map[string]quotaWindow, now, fetchedAt time.Time) *ratelimit.Snapshot {
	kinds := make([]string, 0, len(windows))
	for kind, w := range windows {
		if w.Standin || w.ResetsAt.After(now) {
			kinds = append(kinds, kind)
		}
	}
	if len(kinds) == 0 {
		return nil
	}
	sort.Strings(kinds)
	snap := &ratelimit.Snapshot{FetchedAt: fetchedAt}
	for _, kind := range kinds {
		w := windows[kind]
		lim := ratelimit.Limit{Kind: kind, Percent: w.Percent}
		if !w.Standin {
			resetsAt := w.ResetsAt
			lim.ResetsAt = &resetsAt
		}
		snap.Limits = append(snap.Limits, lim)
	}
	return snap
}

// weeklyWaitLimit は、1週間の期間のリセットを担当を持ったまま待つ上限である（0 以下は上限なし）。
func (o *Orchestrator) weeklyWaitLimit() time.Duration {
	return time.Duration(o.cfg.RateLimit.WeeklyWaitLimitMinutes) * time.Minute
}

// marginCutForTurn は、走っている run に切り上げさせるかを決める（issue #262。設計 3-27a の判定の式）。
//
// **マージンを切った期間が在り、かつ Claude Code が動ける（5時間の期間と全体の1週間の期間が
// 使用率100%でない）ときだけ真である。**動けないときは、今の動き（使用率100%の待ち）に任せる
// （人間の回答「リセットを待つで良い。100%になってるのだからリセットまで何もできない」。2026-10-17）。
//
// **`margin-status` を書かない機械（`rate_limit.source: none`）では、いつも偽である。**
// もともと使用率が無い。
//
// usable: 使える値。nil なら偽（使用率を読めていないときは止めない。設計 3-77i）。
// 戻り値: 切り上げさせるなら true。
func (o *Orchestrator) marginCutForTurn(usable *ratelimit.Snapshot) bool {
	if o.marginStatusPath == "" || usable == nil {
		return false
	}
	if usable.AnySelected(handoff.CannotWork()) {
		return false
	}
	return usable.AnySelected(handoff.MarginCut(o.bidMargins()))
}

// marginCutKinds は、マージンを切った期間の種別を返す（ログと理由のコメントに使う）。
func (o *Orchestrator) marginCutKinds(usable *ratelimit.Snapshot) []string {
	if usable == nil {
		return nil
	}
	return usable.SelectedKinds(handoff.MarginCut(o.bidMargins()))
}

// marginPeriods は、使える値から、マージンを切った期間の一覧を作る（`margin-status` と理由のコメントに使う）。
func (o *Orchestrator) marginPeriods(usable *ratelimit.Snapshot) []marginstatus.Period {
	if usable == nil {
		return nil
	}
	m := o.bidMargins()
	cut := handoff.MarginCut(m)
	var periods []marginstatus.Period
	for _, l := range usable.Limits {
		if !cut(l) {
			continue
		}
		margin := m.FiveHour
		if handoff.IsWeeklyKind(l.Kind) {
			margin = m.Weekly
		}
		periods = append(periods, marginstatus.Period{Kind: l.Kind, Used: l.Percent, Margin: margin, ResetsAt: l.ResetsAt})
	}
	return periods
}

// writeMarginStatus は、`margin-status` を1行で書き直す（issue #262。設計 3-27a）。
//
// **巡回のたびに呼ぶ。**Claude Code は組み込みの指示書の 5-8 に従って、作業の区切りでこれを `cat` で読み、
// `MARGIN: cut` で `expires_at` が未来なら、自分で切り上げる。
//
// **一時ファイルへ書いてから差し替える**（`atomicfile.Write`）。Claude Code が書きかけの1行を読まないためである。
// **書けなくても巡回は止めない。**turn の終わりの判定が、Claude Code が読めなかったぶんを止める。
//
// usable: この巡回で読んだ使える値。
func (o *Orchestrator) writeMarginStatus(usable *ratelimit.Snapshot) {
	if o.marginStatusPath == "" {
		return
	}
	now := o.now()
	state := marginstatus.StateUnknown
	var periods []marginstatus.Period
	if usable != nil {
		periods = o.marginPeriods(usable)
		state = marginstatus.StateOK
		if len(periods) > 0 {
			state = marginstatus.StateCut
		}
	}
	poll := time.Duration(o.cfg.Polling.IntervalMs) * time.Millisecond
	line := marginstatus.Format(state, periods, marginstatus.ExpiresAt(now, poll, periods))
	if err := atomicfile.Write(o.marginStatusPath, []byte(line), marginStatusPerm); err != nil {
		o.marginStatusWarnOnce.Do(func() {
			o.logger.Warn("margin-status を書けません（Claude Code は自分で切り上げられませんが、turn の終わりの判定が止めます）",
				"path", o.marginStatusPath, "error", err)
		})
	}
}

// marginStatusPerm は margin-status の権限である。**Claude Code が同じ利用者で読むだけなので、他人には見せない。**
const marginStatusPerm = 0o600

// marginCutBlockedReason は、マージンを切って切り上げた run を `Blocked` へ戻すときの理由を組み立てる
// （issue #262。設計 3-27a）。
//
// **切った期間とリセットの時刻を必ず書く。**人間は、いつ着手待ちへ戻せばよいかをこれで決める。
// リセットの前に戻すと、もう1度 `Blocked` へ戻る。**リセットの時刻を読めない期間は「不明」と書く。**
//
// **文面は、同じ package のほかの理由と同じく日本語で直に書く。**`internal/orchestrator` は、まだ
// 資源（i18n）へ移していない（`test/internal/testdesign/no_japanese_messages_test.go` の台帳）。
//
// usable: 判定に使った使える値。
// 戻り値: 人間へ見せる理由。
func (o *Orchestrator) marginCutBlockedReason(usable *ratelimit.Snapshot) string {
	return fmt.Sprintf(
		"レートリミットのマージンを切ったので、Claude Code に作業を切り上げさせ、Status を戻しました。"+
			"**担当者はこの機械のままです。**%s"+
			"\n【確かめ方】この issue に、Claude Code が書いた引き継ぎのコメント（どこまで進んだか・次に何をするか）があるはずです。"+
			"\n【対処】リセットのあとに Status を着手待ちへ戻してください。リセットの前に戻すと、もう1度この Status へ戻ります。"+
			"マージンは WORKFLOW.md の `tracker.provider.handoff.five_hour_margin_percent` と "+
			"`tracker.provider.handoff.weekly_margin_percent` で変えられます。",
		describeMarginPeriods(o.marginPeriods(usable)))
}

// describeMarginPeriods は、マージンを切った期間を人間が読む1文にする。
//
// 例: 「5時間の期間が 93%（マージン 10）で、リセットは 2026-10-17 14:00 (JST) です。」
func describeMarginPeriods(periods []marginstatus.Period) string {
	var b strings.Builder
	for _, p := range periods {
		name := "5時間の期間"
		switch p.Kind {
		case handoff.LimitKindWeeklyAll:
			name = "1週間の期間"
		case handoff.LimitKindWeeklyScoped:
			name = "モデル別の1週間の期間"
		}
		resets := "リセットの時刻は不明です"
		if p.ResetsAt != nil {
			// **機械のタイムゾーンで書く**（continuo 自身のほかのコメントと同じ形。issue #258）。
			resets = "リセットは " + p.ResetsAt.Local().Format("2006-01-02 15:04 (MST)") + " です"
		}
		fmt.Fprintf(&b, "%sが %d%%（マージン %d）で、%s。", name, p.Used, p.Margin, resets)
	}
	return b.String()
}

// beforeDispatchPeriods は、着手の前の判定で当てた期間（マージンを切った期間と、Claude Code が動けない期間）を返す
// （issue #262。理由のコメントに使う）。
func (o *Orchestrator) beforeDispatchPeriods(usable *ratelimit.Snapshot) []marginstatus.Period {
	if usable == nil {
		return nil
	}
	m := o.bidMargins()
	cut := handoff.MarginCut(m)
	cannot := handoff.CannotWork()
	var periods []marginstatus.Period
	for _, l := range usable.Limits {
		if !cut(l) && !cannot(l) {
			continue
		}
		margin := m.FiveHour
		if handoff.IsWeeklyKind(l.Kind) {
			margin = m.Weekly
		}
		periods = append(periods, marginstatus.Period{Kind: l.Kind, Used: l.Percent, Margin: margin, ResetsAt: l.ResetsAt})
	}
	return periods
}

// quotaSentenceForStall は、60分の打ち切りの文面の1行目の末尾に足す、使用率の1文を作る（issue #273）。
//
// **1行目に足すのは、ログに出るのが1行目だけだからである**（`summaryLine`）。
// **使用率を読まない設定（`rate_limit.source: none`）では足さない。**読まないと決めた利用者に、
// 故障のような原因を案内しないためである。
//
// reads: この巡回で1回だけ読んだ写し。
// now: いまの時刻。
// 戻り値の1つ目: 足す1文。足さないときは空文字。
// 戻り値の2つ目: 【よくある原因】に「レートリミットに達していたが、continuo が使用率を読めていなかった」を足すなら true。
// **使える値が無かったときだけ足す。**読めていたときに足すと、読み取りが壊れているのかと疑わせる。
func (o *Orchestrator) quotaSentenceForStall(reads quotaReads, now time.Time) (string, bool) {
	if o.cfg.RateLimit.Source == ratelimit.SourceNone {
		return "", false
	}
	if reads.usable == nil {
		if reads.lastReadAt.IsZero() {
			return "そのとき continuo は、立て直してから使用率をまだ読めていませんでした。", true
		}
		// **最後に読めてから30分以内でも、期間が全部リセットを過ぎていると使える値は空になる。**
		// そのときは読み取りが壊れているのではないので、【よくある原因】に読めていなかったことを足さない。
		if now.Sub(reads.lastReadAt) <= usableQuotaWindow {
			return "そのとき continuo が最後に読めた使用率の期間は、どれもリセットの時刻を過ぎていました。", false
		}
		return "そのとき continuo は使用率を読めていませんでした。" +
			"レートリミットで止まっていたのかどうかを、continuo は判定できていません。", true
	}
	var parts []string
	for _, l := range reads.usable.Limits {
		name := "5時間の期間"
		switch l.Kind {
		case handoff.LimitKindWeeklyAll:
			name = "1週間の期間"
		case handoff.LimitKindWeeklyScoped:
			name = "モデル別の1週間の期間"
		}
		parts = append(parts, fmt.Sprintf("%sが %d%%", name, l.Percent))
	}
	ago := formatDuration(now.Sub(reads.lastReadAt)) + "前"
	if now.Sub(reads.lastReadAt) < time.Minute {
		// **「1分未満前」にしない。**`formatDuration` は1分未満を「1分未満」と返す。
		ago = "1分以内"
	}
	return fmt.Sprintf("そのとき continuo が持っていた使用率は、%s でした（最後に読めたのは %s）。",
		strings.Join(parts, "、"), ago), false
}
