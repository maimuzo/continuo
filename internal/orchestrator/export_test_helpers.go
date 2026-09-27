package orchestrator

import "context"

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
// 呼ぶための入り口である（設計 3-82）。
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
// 終わらせる処理の打ち切り（`abortTerminalForHuman`）を1回通す（設計 3-82f）。
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
