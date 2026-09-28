package herdr

import "fmt"

// IssueLabel は pane と herdr workspace に貼る label を組み立てる。
//
// **形は `owner/repo/issues/N` である。**issue の URL をそのまま貼ると、herdr の一覧で
// 先頭が全部 `https://github.com/` になり、見分けたい部分が右へ押し出される。
//
// **label は人間が herdr の画面で見分けるためのものである。**continuo は読み戻さない
// （復元の照合は pane の cwd と worktree のパスで行う。設計 3-3）。
// **例外は statusline取得用の workspace だけである**（StatuslineFetchLabel。issue #284）。
// したがって形を変えても復元は壊れない。
//
// **draft issue のためにガードを置く。**着手の経路では
// `internal/orchestrator/dispatch.go` が Dispatchable で弾いているが、弾く条件が
// 将来変わったときに `//issues/0` のような壊れた label が黙って書かれるのを防ぐ。
//
// owner: リポジトリの所有者名。
// repo: リポジトリ名。
// number: issue の番号。
// 戻り値: 組み立てた label。owner か repo が空、または number が0以下なら空文字。
func IssueLabel(owner, repo string, number int) string {
	if owner == "" || repo == "" || number <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/%s/issues/%d", owner, repo, number)
}

// StatuslineFetchLabel は、statusline取得（使用率を受け取るために短い haiku の Claude Code を
// 開くこと。issue #284）の workspace に貼る label である。
//
// **continuo は、この label だけは読み戻す。**閉じ残しの statusline取得の workspace を
// 片付けるとき、閉じ残しの一覧の ID が、いまも statusline取得の workspace を指しているかを
// この label で確かめる（ID が別の workspace に使い回されていたら閉じない）。
const StatuslineFetchLabel = "continuo statusline fetch"
