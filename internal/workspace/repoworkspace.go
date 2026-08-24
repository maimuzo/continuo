package workspace

import (
	"context"

	"github.com/maimuzo/continuo/internal/herdr"
)

// リポジトリの親 workspace の扱い（issue #19）。
//
// **`worktree.open` は herdr の workspace を2つ開く。**worktree のぶんと、`cwd` に渡した
// リポジトリのぶんである。後者を**リポジトリの親 workspace**と呼ぶ。
//
// **実測で分かっていること**（2026-08-25、test/live/herdr_test.go）。
//
//	cwd を省く            … worktree_not_found で断られる。**外せない**
//	cwd に worktree を渡す … linked_worktree_source で断られる。**リポジトリ本体しか渡せない**
//	親を閉じる            … **その下の worktree の workspace と pane も一緒に消える**
//	worktree.remove       … 親は閉じない。**issue 1件につき1つ溜まる**
//
// **だから閉じるのは片付けの最後で、かつ2つの条件を両方満たすときだけである。**
//
//	1 continuo がその親を開かせたこと（身元ファイルの herdr_repo_workspace_id が空でない）
//	2 その親の下に worktree の workspace が1つも残っていないこと
//
// 1 を見ないと**人間が自分で開いたリポジトリの workspace を閉じてしまう。**
// 2 を見ないと**別の issue が使っている worktree の pane ごと消す。**

// repoWorkspaceOpen は、そのリポジトリの親 workspace が既に開かれているかを返す
// （`worktree.open` を呼ぶ前に見る）。
//
// **問い合わせられなかったら「開かれている」と答える。**答えられない状態で
// 「無かった」と記録すると、**人間が開いた workspace を continuo のものとして閉じにいく。**
// 分からないときは触らない側に倒す。
//
// ctx: 実行に適用するコンテキスト。
// repoPath: リポジトリ本体の作業ディレクトリ。
// 戻り値: 親 workspace が既にあれば true。**問い合わせに失敗した場合も true。**
func (m *Manager) repoWorkspaceOpen(ctx context.Context, repoPath string) bool {
	if m.herdr == nil {
		return true
	}
	list, err := m.herdr.WorkspaceList(ctx)
	if err != nil {
		m.logger.Warn("herdr の workspace の一覧を引けないので、リポジトリの親 workspace は触りません",
			"repo", repoPath, "error", err)
		return true
	}
	return findRepoWorkspace(list.Workspaces, repoPath) != ""
}

// repoWorkspaceID はそのリポジトリの親 workspace の ID を引く
// （`worktree.open` を呼んだ直後に見る）。
//
// ctx: 実行に適用するコンテキスト。
// repoPath: リポジトリ本体の作業ディレクトリ。
// 戻り値: 親 workspace の ID。見つからない場合と問い合わせに失敗した場合は空文字
// （**空文字は「閉じる責任を負わない」を意味する**）。
func (m *Manager) repoWorkspaceID(ctx context.Context, repoPath string) string {
	if m.herdr == nil {
		return ""
	}
	list, err := m.herdr.WorkspaceList(ctx)
	if err != nil {
		m.logger.Warn("herdr の workspace の一覧を引けないので、リポジトリの親 workspace を控えません",
			"repo", repoPath, "error", err)
		return ""
	}
	return findRepoWorkspace(list.Workspaces, repoPath)
}

// findRepoWorkspace は、そのリポジトリ本体を開いている workspace を1つ探す。
//
// **worktree の workspace と混ぜない。**リポジトリの親 workspace は
// `checkout_path` がリポジトリ本体そのものを指す（worktree のものは worktree を指す）。
//
// workspaces: `workspace.list` が返した一覧。
// repoPath: リポジトリ本体の作業ディレクトリ。
// 戻り値: 見つかった workspace の ID。無ければ空文字。
func findRepoWorkspace(workspaces []herdr.Workspace, repoPath string) string {
	for _, ws := range workspaces {
		if ws.Worktree == nil {
			continue
		}
		if samePath(ws.Worktree.CheckoutPath, repoPath) {
			return ws.WorkspaceID
		}
	}
	return ""
}

// closeRepoWorkspace は、continuo が開かせたリポジトリの親 workspace を閉じる
// （3-9 の段3b。`worktree.remove` の直後に呼ぶ）。
//
// **閉じてよい条件を2つとも確かめてから閉じる。**このファイルの冒頭の表を見よ。
// **どちらか一方でも確かめられなければ閉じない。**閉じ残しは「herdr の画面が
// 見づらい」で済むが、閉じ間違いは**人間や別の issue の pane を消す。**
//
// **失敗してもエラーにしない。**片付けそのものは既に済んでおり、ここで止めると
// 「worktree は消えたのに Cleanup は失敗を返す」という、呼び出し側が扱えない結果になる。
//
// ctx: 実行に適用するコンテキスト。
// repoDir: 検算済みのリポジトリ本体の作業ディレクトリ。
// identity: 読み取った身元ファイル。
func (m *Manager) closeRepoWorkspace(ctx context.Context, repoDir string, identity *Identity) {
	target := identity.HerdrRepoWorkspaceID
	if target == "" || m.herdr == nil {
		return
	}

	list, err := m.herdr.WorkspaceList(ctx)
	if err != nil {
		m.logger.Warn("herdr の workspace の一覧を引けないので、リポジトリの親 workspace を閉じません",
			"repo", repoDir, "workspace_id", target, "error", err)
		return
	}

	// **身元ファイルの値をそのまま herdr へ渡さない。**この値もエージェントが
	// 書き換えられる（deletableBranch / resolveWorkspaceID と同じ理由）。検算せずに
	// 閉じると、**同じ機械で動いている別のリポジトリの workspace を閉じさせられる。**
	// 通す条件は「その ID の workspace が、いま片付けたリポジトリ本体を開いている」ことである。
	if findRepoWorkspace(list.Workspaces, repoDir) != target {
		m.logger.Warn("身元ファイルの herdr_repo_workspace_id がリポジトリの現物と一致しないので閉じません",
			"identity_repo_workspace_id", target, "repo", repoDir)
		return
	}

	// **まだ使っている worktree があれば閉じない。**親を閉じると、その下の worktree の
	// workspace と pane も一緒に消える（実測: 2026-08-25）。
	if id, path := otherWorktreeOf(list.Workspaces, repoDir); id != "" {
		m.logger.Info("同じリポジトリの worktree がまだ開いているので、リポジトリの親 workspace は残します",
			"repo", repoDir, "repo_workspace_id", target,
			"open_workspace_id", id, "open_worktree", path)
		return
	}

	if _, err := m.herdr.WorkspaceClose(ctx, herdr.WorkspaceCloseParams{WorkspaceID: target}); err != nil {
		m.logger.Warn("リポジトリの親 workspace を閉じられませんでした（手で閉じてください）",
			"repo", repoDir, "workspace_id", target, "error", err)
		return
	}
	m.logger.Info("リポジトリの親 workspace を閉じました",
		"repo", repoDir, "workspace_id", target)
}

// otherWorktreeOf は、そのリポジトリに属する worktree の workspace を1つ探す。
//
// **リポジトリ本体そのものを開いている workspace は数えない**（それが親である）。
//
// workspaces: `workspace.list` が返した一覧。
// repoDir: リポジトリ本体の作業ディレクトリ。
// 戻り値の1つ目: 見つかった workspace の ID。無ければ空文字。
// 戻り値の2つ目: その workspace が開いている作業ディレクトリ（ログに出す）。
func otherWorktreeOf(workspaces []herdr.Workspace, repoDir string) (string, string) {
	for _, ws := range workspaces {
		if ws.Worktree == nil {
			continue
		}
		if !samePath(ws.Worktree.RepoRoot, repoDir) {
			continue
		}
		if samePath(ws.Worktree.CheckoutPath, repoDir) {
			continue
		}
		return ws.WorkspaceID, ws.Worktree.CheckoutPath
	}
	return "", ""
}
