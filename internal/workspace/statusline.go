package workspace

import (
	"context"
	"errors"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/loop"
)

// statusline取得の workspace を作る・閉じる・閉じ残しを片付ける（issue #284）。
//
// **statusline取得**は、使用率を受け取るために、利用者が信頼している clone の中で
// 短い haiku の Claude Code を開き、`hello` を1回送って閉じることである。
// Claude Code を起動して値を待つのは orchestrator の仕事で、ここは herdr の workspace の
// 開け閉めだけを受け持つ。**開け閉めは、段7 と片付けと同じ loop に通す**（serial.go）。

// StatuslineWorkspace は、作った statusline取得の workspace である。
//
// **押さえた key を持ち回る。**閉じるときにパスから key を作り直すと、シンボリックリンクの
// 解決が変わったとき（clone を消した・動かした・リンクの先を張り替えた）に別の key になり、
// 押さえが残る。押さえを放すのは閉じる仕事だけなので、残るとその clone の着手が止まり続ける。
type StatuslineWorkspace struct {
	// WorkspaceID は作った herdr workspace の ID である。
	WorkspaceID string
	// PaneID は root の pane の ID である（`agent.start` の宛先）。
	PaneID string
	// ClonePath は cwd に渡した clone のパスである。
	ClonePath string
	// key は押さえた名前である（cloneKey）。
	key string
}

// StatuslineCreateError は、statusline取得の workspace を作れなかったことを表す。
//
// **作るのに失敗しても、herdr が workspace を作っていることがある**（応答が期限に間に合わなかった・
// ID の無い応答が返った）。その workspace は誰も ID を知らないので、閉じ残しの一覧に載らず、
// 誰も閉じない。そこで、作る前と後の一覧の差から、増えた statusline取得の label の workspace を
// Orphans に載せて返す。**呼び出し側は Orphans を閉じ残しの一覧へ足す。**
//
// herdr の一覧は workspace の cwd を返さないので、label と差で見分ける。同じ機械の別の continuo
// （`--id` を分けたもの）が同じ瞬間に statusline取得の workspace を作ったときだけ、それを取り違えうる。
// 取り違えても、その workspace は次の閉じ残しの片付けで閉じられ、向こうの試行が1回失敗して
// やり直すだけである。
type StatuslineCreateError struct {
	// Err は作れなかった理由である。
	Err error
	// Orphans は、作る前には無く、作ったあとに増えていた statusline取得の label の workspace の ID である。
	Orphans []string
}

// Error は作れなかった理由を返す。
func (e *StatuslineCreateError) Error() string { return e.Err.Error() }

// Unwrap は作れなかった理由を返す。
func (e *StatuslineCreateError) Unwrap() error { return e.Err }

// StatuslineCloseOutcome は、statusline取得の workspace を閉じた結果である。
type StatuslineCloseOutcome int

const (
	// StatuslineClosed は、閉じた（または既に無かった）ことを表す。閉じ残しの一覧から外してよい。
	StatuslineClosed StatuslineCloseOutcome = iota
	// StatuslineParentWithChild は、issue の親にされていて、下に issue の worktree の
	// workspace が居るので閉じなかったことを表す。**閉じると herdr 0.8.x では下の issue の
	// pane まで消え、0.9.x では断られる。**子が居なくなれば、次の閉じ残しの片付けで閉じる。
	StatuslineParentWithChild
	// StatuslineCloseFailed は、一覧を引けなかったか閉じるのに断られたので閉じられなかったことを表す。
	StatuslineCloseFailed
)

// withoutDeadline は、親の ctx の取り消しは引き継ぎ、期限だけを外した ctx を作る。
//
// **期限を外す理由。**herdr のクライアントは、ctx に期限があると呼び出しごとの期限ではなく
// ctx の期限を使う（internal/herdr/client.go の call）。statusline取得1回の全体の上限
// （既定5分）の ctx がそのまま流れ込むと、herdr が応答しないときに loop が最長約5分止まり、
// ほかの clone の着手と片付けも全部待つ。
//
// **取り消しは生かす。**止めるときに順番待ちの作る仕事まで動かすと、止め始めたあとに
// workspace を作ってしまう。**親が期限切れで終わったときは子を取り消さない**
// （`context.AfterFunc` は期限切れでも呼ばれるので、理由を見て分ける）。
//
// parent: 呼び出し側の ctx。
// 戻り値: 期限の無い ctx と、後始末の関数（必ず呼ぶこと）。
func withoutDeadline(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	stop := context.AfterFunc(parent, func() {
		if errors.Is(parent.Err(), context.Canceled) {
			cancel()
		}
	})
	return ctx, func() {
		stop()
		cancel()
	}
}

// OpenStatuslineWorkspace は、statusline取得の workspace を clone の中に作り、その clone を押さえる。
//
// **押さえるのは `workspace.create` が成功したあとの最後の一手である。**そのあとに失敗する
// 処理を置かない（押さえたのに呼び出し側が ID を受け取れないと、押さえが永久に残る）。
// 閉じ残しの一覧へ足すのは呼び出し側である（ファイルの書き込みは loop の仕事に入れない）。
//
// **作る前に一覧を引いておく。**作るのに失敗したら一覧を引き直し、増えた statusline取得の
// label の workspace を StatuslineCreateError の Orphans に載せて返す（herdr が作ったのに
// 応答が届かなかったものを、閉じ残しの一覧へ載せるため）。前の一覧を引けなければ差は取らない。
//
// ctx: 取り消しに使う（期限は外す。withoutDeadline）。
// clonePath: cwd に渡す clone のパス。
// 戻り値の1つ目: 作った workspace。
// 戻り値の2つ目: 作れなかった理由。herdr が無い・作れなかった・ID が返らなかった・取り消した・
// loop が閉じた。作れなかった・ID が返らなかったときは *StatuslineCreateError である。
func (m *Manager) OpenStatuslineWorkspace(ctx context.Context, clonePath string) (StatuslineWorkspace, error) {
	if m.herdr == nil || m.loop == nil {
		return StatuslineWorkspace{}, i18n.Errorf(i18n.KeyWorkspaceStatuslineHerdrMissing)
	}
	jobCtx, cancel := withoutDeadline(ctx)
	defer cancel()
	key := cloneKey(clonePath)
	var ws StatuslineWorkspace
	err := m.loop.Do(jobCtx, "", func(ctx context.Context, j *loop.Job) error {
		before, beforeErr := m.statuslineLabelIDs(ctx)
		focus := false
		created, err := m.herdr.WorkspaceCreate(ctx, herdr.WorkspaceCreateParams{
			Cwd:   clonePath,
			Label: herdr.StatuslineFetchLabel,
			Focus: &focus,
		})
		if err == nil && created.Workspace.WorkspaceID == "" {
			err = i18n.Errorf(i18n.KeyWorkspaceStatuslineCreateNoID, clonePath)
		} else if err != nil {
			err = i18n.Errorf(i18n.KeyWorkspaceStatuslineCreateFailed, clonePath, err)
		}
		if err != nil {
			createErr := &StatuslineCreateError{Err: err}
			if beforeErr == nil {
				createErr.Orphans = m.statuslineOrphans(ctx, before)
			}
			return createErr
		}
		ws = StatuslineWorkspace{
			WorkspaceID: created.Workspace.WorkspaceID,
			PaneID:      created.RootPane.PaneID,
			ClonePath:   clonePath,
			key:         key,
		}
		// **最後の一手。**ここから先で失敗する処理を置かない。
		j.Hold(key)
		return nil
	})
	if err != nil {
		return StatuslineWorkspace{}, err
	}
	return ws, nil
}

// statuslineLabelIDs は、いま herdr にある statusline取得の label の workspace の ID を集める。
// loop の仕事の中で呼ぶ。
func (m *Manager) statuslineLabelIDs(ctx context.Context) (map[string]struct{}, error) {
	list, err := m.herdr.WorkspaceList(ctx)
	if err != nil {
		return nil, err
	}
	ids := map[string]struct{}{}
	for _, ws := range list.Workspaces {
		if ws.Label == herdr.StatuslineFetchLabel {
			ids[ws.WorkspaceID] = struct{}{}
		}
	}
	return ids, nil
}

// statuslineOrphans は、作るのに失敗したあとの一覧から、before に無かった statusline取得の
// label の workspace の ID を返す。**作る仕事の ctx が取り消されていても引けるよう、
// 取り消しを外した ctx で引く**（呼び出しごとの期限は herdr のクライアントが掛ける）。
// 一覧を引けなければ nil を返す。loop の仕事の中で呼ぶ。
func (m *Manager) statuslineOrphans(ctx context.Context, before map[string]struct{}) []string {
	after, err := m.statuslineLabelIDs(context.WithoutCancel(ctx))
	if err != nil {
		return nil
	}
	var orphans []string
	for id := range after {
		if _, ok := before[id]; !ok {
			orphans = append(orphans, id)
		}
	}
	return orphans
}

// CloseStatuslineWorkspace は、statusline取得の workspace を閉じ、押さえを放す。
//
// **止めるときにも取り消さない ctx で積む。**押さえを放すのはこの仕事だけなので、
// 順番待ちの間に捨てられると押さえが永久に残る。仕事の中の herdr の呼び出しは、
// 呼び出しごとの期限（`herdr.read_timeout_ms`）で終わる。止めるときは loop の Close が
// この仕事を `loop.ErrClosed` で返し、workspace は閉じ残しの一覧に残って次の起動で閉じる。
//
// **親にされて子が居るなら閉じない。**閉じてよいかは次の順で決める。
//
//	一覧が引けない                          → 閉じない（StatuslineCloseFailed）
//	その ID が一覧に無い                    → 閉じたものとして扱う（StatuslineClosed）
//	`worktree` 欄を持ち、同じ clone の
//	linked worktree の workspace が居る      → 閉じない（StatuslineParentWithChild）
//	それ以外                                → 閉じる（断られたら StatuslineCloseFailed）
//
// **どの場合も、同じ仕事の中で ws の key を放す。**押さえ続けても守れるものが無いためである
// （herdr が応答しないなら、後に回した `worktree.open` も失敗する）。
//
// ctx: 呼び出し側の ctx（取り消しも期限も外す）。
// ws: OpenStatuslineWorkspace が返したもの。
// 戻り値の1つ目: 閉じた結果。
// 戻り値の2つ目: 閉じられなかった理由（StatuslineCloseFailed のとき）か、loop が閉じていたこと。
func (m *Manager) CloseStatuslineWorkspace(
	ctx context.Context, ws StatuslineWorkspace,
) (StatuslineCloseOutcome, error) {
	if m.herdr == nil || m.loop == nil {
		return StatuslineCloseFailed, i18n.Errorf(i18n.KeyWorkspaceStatuslineHerdrMissing)
	}
	outcome := StatuslineCloseFailed
	var closeErr error
	err := m.loop.Do(context.WithoutCancel(ctx), "", func(ctx context.Context, j *loop.Job) error {
		defer j.Release(ws.key)
		outcome, closeErr = m.closeStatuslineWorkspace(ctx, ws.WorkspaceID)
		return nil
	})
	if err != nil {
		return StatuslineCloseFailed, err
	}
	return outcome, closeErr
}

// closeStatuslineWorkspace は、一覧を引いて閉じてよいかを決め、閉じる。loop の仕事の中で呼ぶ。
//
// ctx: 仕事の ctx。
// workspaceID: 閉じる workspace の ID。
// 戻り値の1つ目: 閉じた結果。
// 戻り値の2つ目: 閉じられなかった理由。
func (m *Manager) closeStatuslineWorkspace(
	ctx context.Context, workspaceID string,
) (StatuslineCloseOutcome, error) {
	list, err := m.herdr.WorkspaceList(ctx)
	if err != nil {
		return StatuslineCloseFailed, err
	}
	ws, ok := findWorkspaceByID(list.Workspaces, workspaceID)
	if !ok {
		return StatuslineClosed, nil
	}
	if parentWithChild(list.Workspaces, ws) {
		return StatuslineParentWithChild, nil
	}
	if _, err := m.herdr.WorkspaceClose(ctx, herdr.WorkspaceCloseParams{WorkspaceID: workspaceID}); err != nil {
		return StatuslineCloseFailed, err
	}
	return StatuslineClosed, nil
}

// CloseLeftoverStatuslineWorkspaces は、閉じ残しの statusline取得の workspace を片付ける。
//
// **閉じ残しの一覧（`実行時ディレクトリ/statusline-fetch/workspaces.json`）の ID だけを見る。**
// 一覧の読み書きは呼び出し側（orchestrator）が行う。押さえは使わない（閉じ残しは誰も押さえていない）。
//
//	一覧が引けない                           → 全部残す
//	herdr に無い                             → 一覧から外す
//	label が statusline取得のものでない      → 閉じずに一覧から外す（ID が別の workspace に
//	                                           使い回されている。continuo が label を書き換えるのは
//	                                           issue の worktree の workspace だけである）
//	親にされて子が居る                       → 閉じずに残す
//	それ以外                                 → 閉じる（断られたら残す）
//
// ctx: 取り消しに使う（期限は外す。withoutDeadline）。
// ids: 閉じ残しの一覧の ID。
// 戻り値: まだ残っている ID（一覧に書き戻すもの）。
func (m *Manager) CloseLeftoverStatuslineWorkspaces(ctx context.Context, ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	if m.herdr == nil || m.loop == nil {
		return ids
	}
	jobCtx, cancel := withoutDeadline(ctx)
	defer cancel()
	remaining := ids
	err := m.loop.Do(jobCtx, "", func(ctx context.Context, _ *loop.Job) error {
		list, err := m.herdr.WorkspaceList(ctx)
		if err != nil {
			m.logger.Warn("herdr の workspace の一覧を引けないので、閉じ残しの statusline取得の workspace は次に回します",
				"workspace_ids", ids, "error", err)
			return nil
		}
		var keep []string
		for _, id := range ids {
			ws, ok := findWorkspaceByID(list.Workspaces, id)
			if !ok {
				continue
			}
			if ws.Label != herdr.StatuslineFetchLabel {
				m.logger.Info("閉じ残しの一覧の ID が statusline取得の workspace ではなくなっているので、閉じずに一覧から外します",
					"workspace_id", id, "label", ws.Label)
				continue
			}
			if parentWithChild(list.Workspaces, ws) {
				m.logger.Warn("statusline取得の workspace が issue の親にされたので、子の issue の workspace が閉じたあとに閉じます",
					"workspace_id", id)
				keep = append(keep, id)
				continue
			}
			if _, err := m.herdr.WorkspaceClose(ctx, herdr.WorkspaceCloseParams{WorkspaceID: id}); err != nil {
				m.logger.Warn("閉じ残しの statusline取得の workspace を閉じられませんでした（次に閉じ直します。herdr の画面で手で閉じてもかまいません）",
					"workspace_id", id, "error", err)
				keep = append(keep, id)
				continue
			}
			m.logger.Info("閉じ残しの statusline取得の workspace を閉じました", "workspace_id", id)
		}
		remaining = keep
		return nil
	})
	if err != nil {
		return ids
	}
	return remaining
}

// findWorkspaceByID は一覧から ID の一致する workspace を探す。
func findWorkspaceByID(workspaces []herdr.Workspace, id string) (herdr.Workspace, bool) {
	for _, ws := range workspaces {
		if ws.WorkspaceID == id {
			return ws, true
		}
	}
	return herdr.Workspace{}, false
}

// parentWithChild は、その workspace が issue の親にされていて、下に同じ clone の
// linked worktree の workspace が居るかを返す。
//
// **herdr の一覧は、どの workspace がどれの子かを返さない。**そこで「`worktree` 欄を持つ
// （親にされた）」かつ「同じ clone の linked worktree の workspace が居る」で見る
// （otherWorktreeOf）。同じ clone の別の issue が走っている間は、子の居なくなった親も閉じない。
func parentWithChild(workspaces []herdr.Workspace, ws herdr.Workspace) bool {
	if ws.Worktree == nil {
		return false
	}
	repoDir := ws.Worktree.RepoRoot
	if repoDir == "" {
		repoDir = ws.Worktree.CheckoutPath
	}
	id, _ := otherWorktreeOf(workspaces, repoDir)
	return id != ""
}
