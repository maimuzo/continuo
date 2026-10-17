package orchestrator

import (
	"time"

	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// QuotaView は、continuo がいまレートリミットをどう見ているかを外から観測するための写しである
// （issue #273。ダッシュボードのレートリミットの区画）。
//
// **判断には使わない。**`RunView` と同じく、監視（ダッシュボード）のための読み取り口である。
//
// **期間の一覧は、判定と同じ「使える値」から作る**（`readQuota` の `usable`。issue #262 の設計 3-27a）。
// 画面に出ている使用率と、切り上げ・着手の前の判定が見ている使用率を、同じものにするためである。
// **別の写し（新しい値だけ・新しさを問わない値）から作ると、画面は「余裕がある」と出しているのに
// run が切り上げられる、という食い違いが起きる。**
type QuotaView struct {
	// Source は `rate_limit.source` の値である（`oauth_usage_api` / `statusline` / `none`）。
	Source string
	// LastReadAt は最後に使用率を読めた時刻（保管値の新しさの時刻）である。
	// **ゼロ値なら、立て直してからまだ1度も読めていない**（か、写しから時刻を復元できない。`QuotaView` の説明）。
	LastReadAt time.Time
	// Fresh は、入札に使ってよいほど新しい値を持っているかである（`readQuota` の `fresh != nil`）。
	Fresh bool
	// NewWorkBlocked は、この巡回で入札の要る issue を取らないかである（`newWorkBlockedWith` と同じ判定）。
	//
	// **これは入札の要る issue の話である。**担当が既にこの機械にある issue は、これとは別に、着手の前に
	// レートリミットのマージンを見る（`ownIssueMarginGate`。issue #262）。
	NewWorkBlocked bool
	// Windows は「使える値」の期間の一覧である（種別の昇順）。
	//
	// **空なら、使用率を判定に使えていない**（最後に読めてから30分を過ぎた・立て直してからまだ読めていない・
	// `rate_limit.source: none`）。
	Windows []QuotaWindowView
}

// QuotaWindowView は、1つの期間の写しである（issue #273）。
type QuotaWindowView struct {
	// Kind は期間の種別である（`session` / `weekly_all` / `weekly_scoped`）。
	Kind string
	// Percent は使用率（整数の百分率）である。
	Percent int
	// ResetsAt はリセットの時刻である。**nil なら、リセットの時刻を読めない期間である**
	// （usage API が `resets_at: null` を返した期間。continuo が付けた仮の期限は出さない）。
	ResetsAt *time.Time
	// MarginPercent は、この期間から引くマージン（%）である。5時間の期間は
	// `five_hour_margin_percent`、1週間の期間は `weekly_margin_percent`。
	MarginPercent int
	// Short は、マージンを切ったかである（`handoff.MarginCut`。マージンが1以上で、余裕値が0以下）。
	//
	// **マージンが0の期間は、使用率100でも偽である。**切り上げの判定と画面が同じものを指すためである。
	Short bool
	// Full は、使い切っているか（使用率100）である（`handoff.Full`）。
	Full bool
}

// QuotaView は、レートリミットの写しを返す（issue #273）。
//
// **保管値には何も書かない。**錠は `readQuota` の1回だけ取る。3つの写しと新しさの時刻を
// 別々の錠で読むと、そのあいだに `OnStatusline` が保管値を差し替え、食い違う（`quotaReads` の説明）。
//
// **最後に読めた時刻は、同じ錠で読んだ新しさの時刻（`quotaReads.lastReadAt`）から取る。**
// 写しが nil でも（30分を過ぎ、保管値の期間が全部リセットを過ぎていても）取れる。
//
// **`rate_limit.source: none` の機械では、期間を1つも出さない。**使用率を読まないと決めた利用者の画面に、
// quota.json に残った古い値を出さないためである。
//
// 戻り値: レートリミットの写し。
func (o *Orchestrator) QuotaView() QuotaView {
	v := QuotaView{Source: o.cfg.RateLimit.Source}
	if o.cfg.RateLimit.Source == ratelimit.SourceNone {
		return v
	}
	reads := o.readQuota()
	v.LastReadAt = reads.lastReadAt
	v.Fresh = reads.fresh != nil
	v.NewWorkBlocked = o.newWorkBlockedWith(reads.fresh) != handoff.SkipNone
	if reads.usable == nil {
		return v
	}
	margins := o.bidMargins()
	short := handoff.MarginCut(margins)
	full := handoff.Full()
	v.Windows = make([]QuotaWindowView, 0, len(reads.usable.Limits))
	for _, l := range reads.usable.Limits {
		margin := margins.FiveHour
		if handoff.IsWeeklyKind(l.Kind) {
			margin = margins.Weekly
		}
		w := QuotaWindowView{
			Kind:          l.Kind,
			Percent:       l.Percent,
			MarginPercent: margin,
			Short:         short(l),
			Full:          full(l),
		}
		if l.ResetsAt != nil {
			resetsAt := *l.ResetsAt
			w.ResetsAt = &resetsAt
		}
		v.Windows = append(v.Windows, w)
	}
	return v
}
