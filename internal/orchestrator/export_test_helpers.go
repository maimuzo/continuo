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
