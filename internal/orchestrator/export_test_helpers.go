package orchestrator

// PermissionRemedyTextForTest は permissionRemedyText を test/internal/orchestrator から呼ぶための入り口である
// （設計 3-11。issue #259）。
//
// **本体を公開しない。**この文面は引き渡しの通知の一部でしかなく、
// 外から組み立てさせる用途は無い。**検査だけが要る。**
//
// mode: `claude.permission_mode` の値。
// repoIsPrivate: リポジトリが非公開かどうか。nil は「取れなかった」である。
// 戻り値: permissionRemedyText と同じ文面。
func PermissionRemedyTextForTest(mode string, repoIsPrivate *bool) string {
	return permissionRemedyText(mode, repoIsPrivate)
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
