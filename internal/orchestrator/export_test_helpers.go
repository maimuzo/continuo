package orchestrator

import (
	"context"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/internal/tracker"
)

// PermissionRemedyTextForTest は permissionRemedyText を test/internal/orchestrator から呼ぶための入り口である
// （設計 3-11。issue #259）。
//
// **本体を公開しない。**この文面は引き渡しの通知の一部でしかなく、
// 外から組み立てさせる用途は無い。**検査だけが要る。**
//
// mode: `claude.permission_mode` の値。
// relay: relay が有効かどうか（設計 3-84）。
// 戻り値: permissionRemedyText と同じ文面。
func PermissionRemedyTextForTest(mode string, relay bool) string {
	return permissionRemedyText(mode, relay)
}

// 以下は、人間のコメントを最初のメッセージに付けて渡す機能（relay。設計 3-84。issue #246）を
// test/internal/orchestrator から確かめるための入り口である。**本体の振る舞いは変えない。**

// RelayMaxRunesForTest は、最初のメッセージに付ける節の長さの上限（rune 数）である。
const RelayMaxRunesForTest = relayMaxRunes

// RelayEnabledForTest は relayEnabled を呼ぶ。
//
// cfg: 設定。
// 戻り値: relay が有効なら true。
func RelayEnabledForTest(cfg config.Config) bool {
	return relayEnabled(cfg)
}

// SelectRelayCommentsForTest は、設定から印を組み立てて selectRelayComments を呼ぶ。
//
// cfg: 設定（印を組み立てるのに使う）。
// comments: 読んだコメント。
// truncated: 古い側を読み切れなかったなら true。
// 戻り値の1つ目: 結果の種類（"no_boundary" / "unverified" / "incomplete" / "ok"）。
// 戻り値の2つ目: 渡すコメント（古い順）。
func SelectRelayCommentsForTest(cfg config.Config, comments []tracker.Comment, truncated bool) (string, []tracker.Comment) {
	sel := selectRelayComments(comments, truncated, relayAIMarkers(cfg), relayAgentMarkers(cfg))
	switch sel.Verdict {
	case relayNoBoundary:
		return "no_boundary", nil
	case relayUnverified:
		return "unverified", nil
	case relayIncomplete:
		return "incomplete", nil
	default:
		return "ok", sel.Picked
	}
}

// BuildRelaySectionForTest は buildRelaySection を呼ぶ。
//
// picked: 渡すコメント（古い順）。
// 戻り値: 最初のメッセージに付ける節。
func BuildRelaySectionForTest(picked []tracker.Comment) string {
	return buildRelaySection(picked)
}

// SetRelayTimeoutForTest は、最初のメッセージの直前にコメントを読む処理の期限を差し替える。
//
// d: 新しい期限。
func (o *Orchestrator) SetRelayTimeoutForTest(d time.Duration) {
	o.relayTimeout = d
}

// DispatchBlockedStatesForTest は dispatchBlockedStates を test/internal/orchestrator から
// 呼ぶための入り口である（設計 3-83）。
//
// **着手の段2 の拒否リストは、外から観測する手立てが無い。**
// **`direct_chat_state` が入っているかどうかは、その1件が抜けただけで
// 人間の置いたカードが上書きされるので、機械で押さえておく。**
//
// 戻り値: 段2 が書き込みを断る Status の一覧。
func (o *Orchestrator) DispatchBlockedStatesForTest() []string {
	return o.dispatchBlockedStates()
}

// AbortTerminalForHumanForTest は、印を持つ run を direct chat へ入れ、その pane の状態を差し替えてから、
// 終わらせる処理の打ち切り（`abortTerminalForHuman`）を1回通す（設計 3-83f）。
//
// **打ち切りの終え方は `PaneID` と「その pane で `agent.start` が済んでいるか」で分かれる。**
// **その2つを外から作る手立てが無い**（着手の段8 と `ensureAgentComment` の段4 のあいだの一瞬にしか現れない）
// ので、ここで直に作る。
//
// ctx: 呼び出しに適用するコンテキスト。
// issueID: 印を持つ run の project item の ID。
// paneID: 差し替える `PaneID`（空なら「この処理の `stopWorker` が閉じたあと」）。
// started: その pane で `agent.start` が済んでいるなら true。
// 戻り値の1つ目: 打ち切ったなら true。
// 戻り値の2つ目: 印を持つ run が無ければ false。
func (o *Orchestrator) AbortTerminalForHumanForTest(ctx context.Context, issueID, paneID string, started bool) (bool, bool) {
	rs, ok := o.lookupRunByID(issueID)
	if !ok {
		return false, false
	}
	rs.enterDirectChatMode()
	rs.mu.Lock()
	rs.PaneID = paneID
	if started {
		rs.startedPaneID = paneID
	} else {
		rs.startedPaneID = ""
	}
	rs.mu.Unlock()
	if rs.beginTerminal() != terminalClaimed {
		return false, true
	}
	return o.abortTerminalForHuman(ctx, rs, "テストが終わらせようとしました"), true
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

// QuotaAPIStateForTest は usage API の切り替えの状態の写しである（issue #284）。
type QuotaAPIStateForTest struct {
	// NextAt は usage API を次に試してよい時刻である。
	NextAt time.Time
	// Switched は statusline取得へ切り替えているかである。
	Switched bool
	// LastOK は usage API の直前の試しが成功したかである。
	LastOK bool
	// GaveUp は恒久的な失敗で usage API を試さなくなったかである。
	GaveUp bool
	// EverRead はこの起動のあいだに1度でも使用率を読めたかである。
	EverRead bool
	// FetchStopped は取得止めかである。
	FetchStopped bool
}

// QuotaAPIStateForTest は usage API の切り替えの状態を返す（読むだけ）。
func (o *Orchestrator) QuotaAPIStateForTest() QuotaAPIStateForTest {
	o.quotaMu.Lock()
	defer o.quotaMu.Unlock()
	return QuotaAPIStateForTest{
		NextAt:       o.quota.apiNextAt,
		Switched:     o.quota.apiSwitched,
		LastOK:       o.quota.apiLastOK,
		GaveUp:       o.quota.apiGaveUp,
		EverRead:     o.quota.everRead,
		FetchStopped: o.quota.fetchStopped,
	}
}

// QuotaRefreshIntervalForTest は新しさの幅（quotaRefreshInterval）を返す。
func (o *Orchestrator) QuotaRefreshIntervalForTest() time.Duration {
	o.quotaMu.Lock()
	defer o.quotaMu.Unlock()
	return o.quotaRefreshInterval()
}

// PollAPIForTest は pollAPI を1回呼ぶ（巡回のほかの段を通さずに、usage API の読み取りだけを確かめる）。
func (o *Orchestrator) PollAPIForTest(ctx context.Context) {
	o.pollAPI(ctx)
}
