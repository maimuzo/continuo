package orchestrator

import (
	"context"
	"time"

	"github.com/maimuzo/continuo/internal/ratelimit"
)

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

// BeginAttemptForTest は、印を持つ run について beginAttempt を1回通す
// （issue #197。実装レビュー5周目の MEDIUM）。
//
// **着手とやり直しの入口を、検査から作るための入り口である。**
// **`Adopt` は2経路とも `SendFirstPrompt` を立てない**（`AwaitTurnEnd` の経路は turn を
// 走らせており、`needsPrompt` の経路は次の巡回で継続の指示を受ける。設計 3-4 の段5c）。
// **だから、1回目の指示をまだ送り始めていない run を `Adopt` だけでは作れない。**
//
// **手放しの門の1つが、その状態を見ている**（`releaseQuotaWaitExceeded` の `SendFirstPrompt`）。
// **門を外しても落ちない検査しか無い状態にしないために、ここから作る。**
//
// issueID: 印を持つ run の project item の ID。
// 戻り値: 通したなら true。印を持つ run が無ければ false。
func (o *Orchestrator) BeginAttemptForTest(issueID string) bool {
	rs, ok := o.lookupRunByID(issueID)
	if !ok {
		return false
	}
	rs.beginAttempt(false)
	return true
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

// ResolveStatuslineForwardForTest は、利用者のステータスラインの転送先を決める処理
// （resolveStatuslineForward）を test/internal/orchestrator から呼ぶための入り口である（設計 3-84a）。
//
// **優先順位と「キーはあるが条件に合わない」の扱いは、着手を通すと組み合わせが多すぎる**ので、
// ここで直に確かめる。
//
// worktree: issue の worktree の絶対パス。
// configDir: 利用者の設定ディレクトリ。
// 戻り値の1つ目: 転送先のコマンド。見つからなければ空。
// 戻り値の2つ目: 読めずに飛ばしたファイルのパス。
func ResolveStatuslineForwardForTest(worktree, configDir string) (string, []string) {
	var skipped []string
	cmd := resolveStatuslineForward(statuslineForwardSources(worktree, configDir), func(path string, _ error) {
		skipped = append(skipped, path)
	})
	return cmd, skipped
}
