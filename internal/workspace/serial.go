package workspace

import (
	"context"
	"errors"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/loop"
)

// herdr の workspace の開け閉めを1つの loop に通す（issue #284）。
//
// **statusline取得の workspace を開いている間に、同じ clone で issue の worktree を開くと、
// herdr はその workspace を issue の「親」にしてしまい、閉じられなくなる**
// （実測: 2026-09-28、herdr 0.9.1。`workspace.list` の `checkout_path` が clone、
// `is_linked_worktree` が偽になり、閉じると `workspace_group_close_required` で断られた）。
//
// そこで、herdr の workspace を開け閉めする一続きを、どれも1つの loop の仕事にする。
// statusline取得の workspace を作った仕事は、その clone の key を押さえる。
// **`worktree.open` を含む仕事（着手の段7・片付けの ID の引き直し）だけが key を持ち、**
// 押さえられている間は後に回る。`worktree.open` だけが、開いている workspace を親に
// 作り替えるためである。
//
// **仕事の中から loop へもう一度積まない。**loop は1つなので、中から自分へ積んで待つと
// 返らない。包むのは呼び出し側だけにし、共用の関数（findWorkspaceIDByPath など）の中では
// 包まない。

// ErrCloneBusy は、statusline取得の workspace が開いている clone の片付けを、待たずに
// 見送ったことを表す（CleanupRequest.NoWait が真のとき）。**Cleanup は何も消さずに返る。**
// 呼び出し側（巡回の中の片付け）は次の巡回でやり直す。
var ErrCloneBusy = i18n.Sentinel(i18n.KeyWorkspaceErrCloneBusy)

// cloneKey は、リポジトリ本体のパスから押さえの key を作る。
//
// **key を作るのはこの関数だけである。**着手は ghq が返すパス、片付けは git の共通
// ディレクトリから作ったパスを使うので、`filepath.Clean` だけでは一致しないことがある。
// シンボリックリンクを解いてから並べる（resolveOrClean）。
//
// repoPath: リポジトリ本体の作業ディレクトリ。
// 戻り値: 押さえの key。repoPath が空なら空（押さえを見ない。cwd の無い `worktree.open` は
// herdr が `worktree_not_found` で断るので、親は作られない）。
func cloneKey(repoPath string) string {
	if repoPath == "" {
		return ""
	}
	return "clone:" + resolveOrClean(repoPath)
}

// run は herdr の開け閉めの一続きを loop に積み、終わるまで待つ。
//
// **loop を持たない Manager（herdr を渡さないもの）は、その場で呼ぶ。**中の関数が
// herdr の無いことを見て戻る（いまと同じ振る舞い）。
//
// ctx: 取り消しに使う。
// key: 押さえの名前（cloneKey）。空なら押さえを見ない。
// fn: 実行する一続き。
// 戻り値: fn の誤り。取り消したら ctx.Err()、loop が閉じていたら loop.ErrClosed。
func (m *Manager) run(ctx context.Context, key string, fn func(ctx context.Context) error) error {
	if m.loop == nil {
		return fn(ctx)
	}
	return m.loop.Do(ctx, key, func(ctx context.Context, _ *loop.Job) error {
		return fn(ctx)
	})
}

// tryRun は run と同じだが、key が押さえられていたら待たずに ErrCloneBusy を返す。
func (m *Manager) tryRun(ctx context.Context, key string, fn func(ctx context.Context) error) error {
	if m.loop == nil {
		return fn(ctx)
	}
	err := m.loop.TryDo(ctx, key, func(ctx context.Context, _ *loop.Job) error {
		return fn(ctx)
	})
	if errors.Is(err, loop.ErrBusy) {
		return ErrCloneBusy
	}
	return err
}
