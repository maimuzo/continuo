package orchestrator

import "github.com/maimuzo/continuo/internal/ratelimit"

// PermissionRemedyTextForTest は permissionRemedyText を test/internal/orchestrator から呼ぶための入り口である
// （設計 3-11。issue #259）。
//
// **本体を公開しない。**この文面は引き渡しの通知の一部でしかなく、
// 外から組み立てさせる用途は無い。**検査だけが要る。**
//
// mode: `claude.permission_mode` の値。
// 戻り値: permissionRemedyText と同じ文面。
func PermissionRemedyTextForTest(mode string) string {
	return permissionRemedyText(mode)
}

// 以下は使用率の保管値と statusline取得（issue #284）を test/internal/orchestrator から
// 確かめるための入り口である。**本体の振る舞いは変えない。**読むか、既存の関数を呼ぶだけである。

// NotifyStatuslineForTest は、statusline取得で値が届いたときの知らせを巡回のループへ送る
// （notifyStatusline と同じ。容量1で、置いてあれば足さない）。
//
// **巡回の順（知らせで1回すぐ回り、刻みを数え直す）を、statusline取得を走らせずに確かめる**
// ために使う。
func (o *Orchestrator) NotifyStatuslineForTest() {
	o.notifyStatusline()
}

// PendingStatuslineNotifyForTest は、巡回のループがまだ受け取っていない知らせの数を返す（0 か 1）。
// **受け取らない**（channel を空にしない）。
func (o *Orchestrator) PendingStatuslineNotifyForTest() int {
	return len(o.statuslineNotify)
}

// QuotaForBidForTest は quotaForBid を呼ぶ（入札が読む写し。新しくなければ nil）。
func (o *Orchestrator) QuotaForBidForTest() *ratelimit.Snapshot {
	return o.quotaForBid()
}

// QuotaSnapshotForTest は quotaSnapshot を呼ぶ（回復待ちと閾値が読む写し。新しさは問わない）。
func (o *Orchestrator) QuotaSnapshotForTest() *ratelimit.Snapshot {
	return o.quotaSnapshot()
}

// StatuslineFetchRunningForTest は、statusline取得の goroutine が走っているかを返す。
func (o *Orchestrator) StatuslineFetchRunningForTest() bool {
	o.quotaMu.Lock()
	defer o.quotaMu.Unlock()
	return o.quota.fetchRunning
}

// QuotaSessionKnownForTest は、そのセッションの記録（基準の api_ms）を保管値が持っているかを返す。
func (o *Orchestrator) QuotaSessionKnownForTest(sessionID string) bool {
	o.quotaMu.Lock()
	defer o.quotaMu.Unlock()
	_, ok := o.quota.sessions[sessionID]
	return ok
}
