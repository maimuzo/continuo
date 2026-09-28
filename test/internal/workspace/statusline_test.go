// herdr の workspace の開け閉めの順を、本物の loop を渡した Manager の公開の口だけで確かめる
// （issue #284）。
//
// **statusline取得の workspace を開いている間に、同じ clone で `worktree.open` をすると、
// herdr はその workspace を issue の親にしてしまい、閉じられなくなる**（実測: 2026-09-28、
// herdr 0.9.1）。そこで Manager は、statusline取得の workspace を作った仕事でその clone を押さえ、
// `worktree.open` を含む仕事（着手の段7・片付けの ID の引き直し）を閉じるまで後に回す。
package workspace_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/loop"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/workspace"
)

// holdWait は、押さえで後に回った仕事が動かないことを見届けるために待つ時間である。
// 後に回らなければ、この間に herdr へ `worktree.open` が届く。
const holdWait = 200 * time.Millisecond

// finishWait は、待っていた呼び出しが返るのを待つ上限である。
const finishWait = 20 * time.Second

// ===== loop を通った仕事を控える Runner =====

// runnerCall は loop に積まれた仕事1つの控えである。
type runnerCall struct {
	// Try は TryDo で積まれたかである。
	Try bool
	// Key は押さえの名前である（空なら押さえを見ない仕事）。
	Key string
	// Methods は、その仕事の中で herdr へ送ったメソッドである（仕事が動かなければ nil）。
	Methods []string
	// Err は Do / TryDo が返した誤りである。
	Err error
}

// recordingRunner は本物の loop を包み、積まれた仕事と、その中で herdr へ送ったメソッドを控える。
//
// **仕事の中の herdr の呼び出しを数えられるのは、loop が仕事を1つずつしか動かさないためである。**
// 仕事の始めと終わりで偽の herdr の受信の数を見れば、その間の呼び出しはその仕事のものになる。
type recordingRunner struct {
	inner loop.Runner
	fake  *fakeHerdr

	mu    sync.Mutex
	calls []runnerCall
	// keyed は key を持つ仕事が積まれたときに、その key を受ける（積む直前に送る）。
	keyed chan string
}

// Do は仕事を控えてから本物の loop に積む。
func (r *recordingRunner) Do(ctx context.Context, key string, fn loop.Func) error {
	return r.submit(ctx, key, false, fn)
}

// TryDo は仕事を控えてから本物の loop に積む。
func (r *recordingRunner) TryDo(ctx context.Context, key string, fn loop.Func) error {
	return r.submit(ctx, key, true, fn)
}

// submit は Do と TryDo の共通の本体である。
func (r *recordingRunner) submit(ctx context.Context, key string, try bool, fn loop.Func) error {
	if key != "" {
		select {
		case r.keyed <- key:
		default:
		}
	}
	var methods []string
	wrapped := func(ctx context.Context, j *loop.Job) error {
		before := len(r.fake.Requests())
		err := fn(ctx, j)
		after := r.fake.Methods()
		methods = append([]string{}, after[before:]...)
		return err
	}
	var err error
	if try {
		err = r.inner.TryDo(ctx, key, wrapped)
	} else {
		err = r.inner.Do(ctx, key, wrapped)
	}
	r.mu.Lock()
	r.calls = append(r.calls, runnerCall{Try: try, Key: key, Methods: methods, Err: err})
	r.mu.Unlock()
	return err
}

// Calls は控えた仕事を、返った順に返す。
func (r *recordingRunner) Calls() []runnerCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]runnerCall{}, r.calls...)
}

// waitKeyed は key を持つ仕事が積まれるまで待ち、その key を返す。
func (r *recordingRunner) waitKeyed(t *testing.T) string {
	t.Helper()
	select {
	case key := <-r.keyed:
		return key
	case <-time.After(finishWait):
		t.Fatal("key を持つ仕事が loop に積まれませんでした")
		return ""
	}
}

// ===== 本物の loop を渡す fixture =====

// statuslineFixture は、本物の loop を渡した Manager と、その周辺の値である。
type statuslineFixture struct {
	*managerFixture
	// Loop は起動した本物の loop である（テストから直に仕事を積み、loop を止めるのに使う）。
	Loop *loop.Loop
	// Runner は Manager に渡した、Loop を包んだ控えである。
	Runner *recordingRunner
}

// newStatuslineHerdr は、statusline取得の workspace を作る応答も返すテスト用herdr mock を立てる。
//
// workspace.create は w20 と root の pane w20:p1 を返す。workspace.list は既定で1件も返さない。
func newStatuslineHerdr(t *testing.T) *fakeHerdr {
	t.Helper()
	return newFakeHerdr(t, map[string]any{
		herdr.MethodWorktreeOpen:    worktreeOpenResult("w9", "w9:p1"),
		herdr.MethodWorktreeRemove:  worktreeRemoveResult("w9", ""),
		herdr.MethodWorkspaceRename: workspaceRenameResult("w9"),
		herdr.MethodWorkspaceList:   workspaceListResult(),
		herdr.MethodWorkspaceClose:  workspaceCloseResult(),
		herdr.MethodWorkspaceCreate: workspaceCreateResult("w20", "w20:p1"),
	})
}

// workspaceCreateResult は workspace.create の成功応答の写しである（実測: 2026-09-25、herdr 0.9.1）。
//
// workspaceID: 作った workspace の ID。
// paneID: root の pane の ID。
// 戻り値: JSON 化して result に載せる値。
func workspaceCreateResult(workspaceID, paneID string) map[string]any {
	return map[string]any{
		"type":      "workspace_created",
		"workspace": map[string]any{"workspace_id": workspaceID, "label": herdr.StatuslineFetchLabel},
		"tab":       map[string]any{"tab_id": workspaceID + ":t1", "workspace_id": workspaceID},
		"root_pane": map[string]any{"pane_id": paneID, "workspace_id": workspaceID},
	}
}

// statuslineEntry は workspace.list の1件として、statusline取得の workspace を作る。
//
// workspaceID: workspace の ID。
// label: 貼られている label。
// parentOf: 空でなければ、その clone の親にされた形（`worktree` 欄の checkout_path と
// repo_root がどちらも clone）にする。空なら `worktree` 欄を持たない（作った直後の形）。
// 戻り値: workspaceListResult に渡す1件。
func statuslineEntry(workspaceID, label, parentOf string) map[string]any {
	entry := map[string]any{"workspace_id": workspaceID, "label": label}
	if parentOf != "" {
		entry["worktree"] = map[string]any{
			"checkout_path":      parentOf,
			"repo_root":          parentOf,
			"is_linked_worktree": false,
		}
	}
	return entry
}

// RemoveResult は、登録してある応答を外す（そのメソッドは unknown_method の誤りを返すようになる）。
//
// method: 外すメソッド名。
func (fh *fakeHerdr) RemoveResult(method string) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	delete(fh.results, method)
}

// newStatuslineFixture は、本物の loop を起動して Manager に渡す。
//
// **Manager には loop を包んだ recordingRunner を渡す。**押さえは本物の loop が行う。
// テストの終わりに loop を閉じる。
//
// t: 呼び出し元のテスト。
// opts: Manager の組み立てに渡す入力（Herdr と Loop は上書きする）。
// 戻り値: 組み立てた fixture。
func newStatuslineFixture(t *testing.T, opts fixtureOptions) *statuslineFixture {
	t.Helper()
	l := loop.New(nil)
	l.Start()
	t.Cleanup(l.Close)
	fake := opts.Herdr
	if fake == nil {
		fake = newStatuslineHerdr(t)
	}
	runner := &recordingRunner{inner: l, fake: fake, keyed: make(chan string, 16)}
	opts.Herdr = fake
	opts.Loop = runner
	fx := newFixture(t, opts)
	return &statuslineFixture{managerFixture: fx, Loop: l, Runner: runner}
}

// ===== 小さな道具 =====

// asyncResult は別の goroutine で呼んだ処理の結果を受ける channel を返す。
func asyncResult(fn func() error) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- fn() }()
	return ch
}

// waitResult は非同期の結果を待つ。finishWait を過ぎたら落とす。
func waitResult(t *testing.T, ch <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(finishWait):
		t.Fatalf("%s が返りませんでした（押さえが放されていない疑い）", what)
		return nil
	}
}

// assertNotReturned は、非同期の処理がまだ返っていないことを確かめる。
func assertNotReturned(t *testing.T, ch <-chan error, what string) {
	t.Helper()
	select {
	case err := <-ch:
		t.Fatalf("押さえられている clone なのに %s が返った: %v", what, err)
	default:
	}
}

// methodsAfter は、index 番目より後に herdr へ送ったメソッドを返す。
func methodsAfter(fake *fakeHerdr, index int) []string {
	all := fake.Methods()
	if index >= len(all) {
		return nil
	}
	return all[index:]
}

// openStatusline は statusline取得の workspace を開き、そのときの受信の数を返す。
func openStatusline(t *testing.T, fx *statuslineFixture, clonePath string) (workspace.StatuslineWorkspace, int) {
	t.Helper()
	ws, err := fx.Manager.OpenStatuslineWorkspace(context.Background(), clonePath)
	if err != nil {
		t.Fatalf("OpenStatuslineWorkspace に失敗した: %v", err)
	}
	return ws, len(fx.Herdr.Requests())
}

// forceCleanup は、見送りの判定を飛ばした片付けの入力を作る（押さえの振る舞いだけを見るため）。
//
// **封じ込め検査と、branch・herdr workspace の検算は Force でも飛ばない。**
func forceCleanup(path string, noWait bool) workspace.CleanupRequest {
	return workspace.CleanupRequest{
		WorktreePath: path,
		Base:         normalize.SafeName("main"),
		Force:        true,
		NoWait:       noWait,
	}
}

// issueFor は、別の repo 名の issue を作る（別の clone に着手させるため）。
func issueFor(repo string, number int) workspace.IssueRef {
	issue := sampleIssue(number)
	issue.Repo = repo
	issue.URL = fmt.Sprintf("https://github.com/octocat/%s/issues/%d", repo, number)
	issue.Identifier = fmt.Sprintf("octocat/%s#%d", repo, number)
	return issue
}

// ===== statusline取得の workspace を作る =====

// 目的: statusline取得の workspace を作るとき、clone を cwd に、statusline取得の label を、
// focus 偽で渡し、応答の workspace の ID と root の pane の ID を返すことを確かめる。
// 与える情報: 本物の loop を渡した Manager と、w20・w20:p1 を返す workspace.create。
// 成功条件: 返り値の WorkspaceID が w20、PaneID が w20:p1、ClonePath が渡したパスで、
// herdr へ送った workspace.create の params が cwd・label・focus（偽）であること。
// 作る仕事は key を持たない（key を付けると、押さえが残ったときに自分で止まる）こと。
func TestOpenStatuslineWorkspace_cloneをcwdにlabelとfocus偽で作る(t *testing.T) {
	fx := newStatuslineFixture(t, fixtureOptions{})

	ws, _ := openStatusline(t, fx, fx.Repo.Dir)

	if ws.WorkspaceID != "w20" || ws.PaneID != "w20:p1" || ws.ClonePath != fx.Repo.Dir {
		t.Fatalf("返った workspace = %+v", ws)
	}
	var create *recordedRequest
	for _, r := range fx.Herdr.Requests() {
		if r.Method == herdr.MethodWorkspaceCreate {
			create = &r
		}
	}
	if create == nil {
		t.Fatalf("workspace.create を送っていない: %v", fx.Herdr.Methods())
	}
	if create.Params["cwd"] != fx.Repo.Dir {
		t.Fatalf("cwd = %v, want %q", create.Params["cwd"], fx.Repo.Dir)
	}
	if create.Params["label"] != herdr.StatuslineFetchLabel {
		t.Fatalf("label = %v, want %q", create.Params["label"], herdr.StatuslineFetchLabel)
	}
	if focus, ok := create.Params["focus"]; !ok || focus != false {
		t.Fatalf("focus = %v（送った: %v）, want false", focus, ok)
	}
	for _, c := range fx.Runner.Calls() {
		if slices.Contains(c.Methods, herdr.MethodWorkspaceCreate) && c.Key != "" {
			t.Fatalf("作る仕事が key %q を持っている", c.Key)
		}
	}
}

// 目的: workspace.create が失敗で返っても herdr が workspace を作っていたとき、作る前と後の一覧の差から、
// 増えた statusline取得の label の workspace を StatuslineCreateError の Orphans に載せて返すことを確かめる
// （載せないと ID を知る者が居なくなり、誰も閉じない）。
// 与える情報: 作る前の一覧は statusline取得の label の w30 だけ。workspace.create は誤りを返すか ID の無い応答を返し、
// 受けた時点で一覧を「w30・w40（statusline取得の label）・w41（別の label）」へ差し替える。
// 成功条件: 誤りが *workspace.StatuslineCreateError で、Orphans がちょうど [w40] であること
// （前から在った w30 と、label の違う w41 を載せない）。作る前の一覧が引けないときは Orphans が空であること。
func TestOpenStatuslineWorkspace_作るのに失敗しても増えたworkspaceを閉じ残しとして返す(t *testing.T) {
	after := workspaceListResult(
		statuslineEntry("w30", herdr.StatuslineFetchLabel, ""),
		statuslineEntry("w40", herdr.StatuslineFetchLabel, ""),
		statuslineEntry("w41", "octocat/hello-world/issues/1", ""),
	)
	cases := []struct {
		name   string
		setup  func(fh *fakeHerdr)
		orphan []string
	}{
		{"作る呼び出しが誤りを返した", func(fh *fakeHerdr) {
			fh.RemoveResult(herdr.MethodWorkspaceCreate)
		}, []string{"w40"}},
		{"作る呼び出しが ID の無い応答を返した", func(fh *fakeHerdr) {
			fh.SetResult(herdr.MethodWorkspaceCreate, workspaceCreateResult("", ""))
		}, []string{"w40"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newStatuslineFixture(t, fixtureOptions{})
			fx.Herdr.SetResult(herdr.MethodWorkspaceList, workspaceListResult(
				statuslineEntry("w30", herdr.StatuslineFetchLabel, ""),
			))
			tc.setup(fx.Herdr)
			fx.Herdr.SetOnRequest(herdr.MethodWorkspaceCreate, func(map[string]any) {
				fx.Herdr.SetResult(herdr.MethodWorkspaceList, after)
			})
			_, err := fx.Manager.OpenStatuslineWorkspace(context.Background(), fx.Repo.Dir)
			var createErr *workspace.StatuslineCreateError
			if !errors.As(err, &createErr) {
				t.Fatalf("誤り = %v（%T）, want *workspace.StatuslineCreateError", err, err)
			}
			if !slices.Equal(createErr.Orphans, tc.orphan) {
				t.Fatalf("Orphans = %v, want %v", createErr.Orphans, tc.orphan)
			}
		})
	}
	t.Run("作る前の一覧が引けないときは差を取らない", func(t *testing.T) {
		fx := newStatuslineFixture(t, fixtureOptions{})
		fx.Herdr.RemoveResult(herdr.MethodWorkspaceList)
		fx.Herdr.RemoveResult(herdr.MethodWorkspaceCreate)
		fx.Herdr.SetOnRequest(herdr.MethodWorkspaceCreate, func(map[string]any) {
			fx.Herdr.SetResult(herdr.MethodWorkspaceList, after)
		})
		_, err := fx.Manager.OpenStatuslineWorkspace(context.Background(), fx.Repo.Dir)
		var createErr *workspace.StatuslineCreateError
		if !errors.As(err, &createErr) {
			t.Fatalf("誤り = %v（%T）, want *workspace.StatuslineCreateError", err, err)
		}
		if len(createErr.Orphans) != 0 {
			t.Fatalf("Orphans = %v, want 空（前の一覧が無いので差を取れない）", createErr.Orphans)
		}
	})
}

// ===== 同じ clone の段7 と片付けは、閉じるまで worktree.open を呼ばない =====

// 目的: statusline取得の workspace を開いている間、同じ clone の着手の段7 が `worktree.open` を
// 呼ばず、閉じたあとに呼ぶことを確かめる（開いている間に呼ぶと、その workspace が issue の親にされる）。
// 与える情報: statusline取得の workspace を clone に開いたあと、同じ clone の issue に Prepare する。
// 成功条件: 段7 の仕事が loop に積まれてから holdWait 待っても Prepare が返らず、herdr へ
// `worktree.open` が届かないこと。CloseStatuslineWorkspace が StatuslineClosed を返したあとに
// Prepare が返り、`worktree.open` が `workspace.close` より後に届くこと。
func TestStatusline_開いている間は同じcloneの段7がworktree_openを呼ばず閉じたあとに呼ぶ(t *testing.T) {
	fx := newStatuslineFixture(t, fixtureOptions{})
	ws, openedAt := openStatusline(t, fx, fx.Repo.Dir)
	// 閉じる判定で一覧に在る形にする（無ければ閉じたものとして workspace.close を送らない）。
	fx.Herdr.SetResult(herdr.MethodWorkspaceList,
		workspaceListResult(statuslineEntry("w20", herdr.StatuslineFetchLabel, "")))

	prepared := asyncResult(func() error {
		_, err := fx.Manager.Prepare(context.Background(), sampleIssue(188))
		return err
	})
	fx.Runner.waitKeyed(t)
	time.Sleep(holdWait)

	assertNotReturned(t, prepared, "Prepare")
	if got := methodsAfter(fx.Herdr, openedAt); slices.Contains(got, herdr.MethodWorktreeOpen) {
		t.Fatalf("statusline取得の workspace が開いている間に worktree.open を送った: %v", got)
	}

	// 閉じたら、一覧から消えた形にする（段7 の一覧の引き直しで親を控えないように）。
	fx.Herdr.SetOnRequest(herdr.MethodWorkspaceClose, func(map[string]any) {
		fx.Herdr.SetResult(herdr.MethodWorkspaceList, workspaceListResult())
	})
	outcome, err := fx.Manager.CloseStatuslineWorkspace(context.Background(), ws)
	if err != nil || outcome != workspace.StatuslineClosed {
		t.Fatalf("CloseStatuslineWorkspace = (%v, %v), want (StatuslineClosed, nil)", outcome, err)
	}
	if err := waitResult(t, prepared, "Prepare"); err != nil {
		t.Fatalf("閉じたあとの Prepare が失敗した: %v", err)
	}
	after := methodsAfter(fx.Herdr, openedAt)
	closeAt := slices.Index(after, herdr.MethodWorkspaceClose)
	openAt := slices.Index(after, herdr.MethodWorktreeOpen)
	if closeAt < 0 || openAt < 0 || openAt < closeAt {
		t.Fatalf("worktree.open が workspace.close より後に届いていない: %v", after)
	}
}

// 目的: statusline取得の workspace を開いている間も、別の clone の着手の段7 は待たないことを確かめる
// （押さえは clone ごとで、ほかの clone の着手を止めない）。
// 与える情報: clone A に statusline取得の workspace を開いたまま、clone B の issue に Prepare する。
// 成功条件: Prepare が閉じるのを待たずに成功し、`worktree.open` の cwd が clone B であること。
func TestStatusline_開いている間も別のcloneの段7は待たない(t *testing.T) {
	repoA := newTestRepo(t)
	repoB := newTestRepo(t)
	fx := newStatuslineFixture(t, fixtureOptions{
		Repo: repoA,
		GhqList: func(_ context.Context, _, repo string) (string, error) {
			if repo == "other" {
				return repoB.Dir, nil
			}
			return repoA.Dir, nil
		},
	})
	_, openedAt := openStatusline(t, fx, repoA.Dir)

	prepared := asyncResult(func() error {
		_, err := fx.Manager.Prepare(context.Background(), issueFor("other", 7))
		return err
	})
	if err := waitResult(t, prepared, "別の clone の Prepare"); err != nil {
		t.Fatalf("別の clone の Prepare が失敗した: %v", err)
	}
	var opened *recordedRequest
	for _, r := range fx.Herdr.Requests()[openedAt:] {
		if r.Method == herdr.MethodWorktreeOpen {
			opened = &r
		}
	}
	if opened == nil {
		t.Fatalf("別の clone の worktree.open を送っていない: %v", fx.Herdr.Methods())
	}
	if opened.Params["cwd"] != repoB.Dir {
		t.Fatalf("worktree.open の cwd = %v, want clone B（%q）", opened.Params["cwd"], repoB.Dir)
	}
}

// 目的: statusline取得の workspace を開いている間、同じ clone の片付け（待つもの）が ID の引き直しの
// `worktree.open` を呼ばずに待ち、閉じたあとに片付けを終えることを確かめる。
// 与える情報: worktree を1つ用意して身元ファイルを書いたあと、statusline取得の workspace を
// 同じ clone に開き、NoWait の無い Cleanup を呼ぶ。
// 成功条件: 引き直しの仕事が loop に積まれてから holdWait 待っても Cleanup が返らず、
// `worktree.open` も `worktree.remove` も届かず、worktree が残っていること。閉じたあとに
// Cleanup が Removed で返り、`worktree.open` が `workspace.close` より後に届くこと。
func TestStatusline_開いている間は同じcloneの片付けが待ち閉じたあとに片付く(t *testing.T) {
	l := loop.New(nil)
	l.Start()
	t.Cleanup(l.Close)
	fake := newStatuslineHerdr(t)
	runner := &recordingRunner{inner: l, fake: fake, keyed: make(chan string, 16)}
	cf := newCleanupFixtureWith(t, fixtureOptions{Herdr: fake, Loop: runner})
	fx := &statuslineFixture{managerFixture: cf.managerFixture, Loop: l, Runner: runner}
	drainKeyed(runner)

	ws, openedAt := openStatusline(t, fx, cf.Repo.Dir)
	cleaned := asyncResult(func() error {
		result, err := cf.Manager.Cleanup(context.Background(), forceCleanup(cf.Prepared.Path, false))
		if err == nil && !result.Removed {
			return fmt.Errorf("片付けたのに Removed が偽: %+v", *result)
		}
		return err
	})
	runner.waitKeyed(t)
	time.Sleep(holdWait)

	assertNotReturned(t, cleaned, "Cleanup")
	got := methodsAfter(fake, openedAt)
	if slices.Contains(got, herdr.MethodWorktreeOpen) || slices.Contains(got, herdr.MethodWorktreeRemove) {
		t.Fatalf("押さえられている間に worktree.open か worktree.remove を送った: %v", got)
	}
	if _, err := os.Stat(cf.Prepared.Path); err != nil {
		t.Fatalf("押さえられている間に worktree が消えた: %v", err)
	}

	fake.SetResult(herdr.MethodWorkspaceList,
		workspaceListResult(statuslineEntry("w20", herdr.StatuslineFetchLabel, "")))
	fake.SetOnRequest(herdr.MethodWorkspaceClose, func(map[string]any) {
		fake.SetResult(herdr.MethodWorkspaceList, workspaceListResult())
	})
	if outcome, err := cf.Manager.CloseStatuslineWorkspace(context.Background(), ws); err != nil || outcome != workspace.StatuslineClosed {
		t.Fatalf("CloseStatuslineWorkspace = (%v, %v)", outcome, err)
	}
	if err := waitResult(t, cleaned, "Cleanup"); err != nil {
		t.Fatalf("閉じたあとの Cleanup が失敗した: %v", err)
	}
	after := methodsAfter(fake, openedAt)
	closeAt := slices.Index(after, herdr.MethodWorkspaceClose)
	openAt := slices.Index(after, herdr.MethodWorktreeOpen)
	if closeAt < 0 || openAt < 0 || openAt < closeAt {
		t.Fatalf("worktree.open が workspace.close より後に届いていない: %v", after)
	}
}

// drainKeyed は、fixture の組み立て（Prepare）で積まれた key の知らせを捨てる。
func drainKeyed(r *recordingRunner) {
	for {
		select {
		case <-r.keyed:
		default:
			return
		}
	}
}

// 目的: 巡回の中の片付け（NoWait）が、押さえられた clone では何も消さずに ErrCloneBusy を返し、
// 閉じたあとの次の巡回で片付くことを確かめる（巡回が押さえを待つと、止まった run の検知と着手が止まる）。
// 与える情報: worktree を1つ用意したあと、statusline取得の workspace を同じ clone に開き、
// NoWait の Cleanup を呼ぶ。閉じたあとにもう一度 NoWait の Cleanup を呼ぶ。
// 成功条件: 1回目は待たずに ErrCloneBusy が返り、`worktree.open`・`worktree.remove` が届かず、
// worktree も設定ファイルも残ること。引き直しの仕事が TryDo で積まれていること。
// 閉じたあとの2回目は Removed で返り、worktree が消えること。
func TestStatusline_NoWaitの片付けは押さえられたcloneで何も消さずErrCloneBusyを返す(t *testing.T) {
	l := loop.New(nil)
	l.Start()
	t.Cleanup(l.Close)
	fake := newStatuslineHerdr(t)
	runner := &recordingRunner{inner: l, fake: fake, keyed: make(chan string, 16)}
	cf := newCleanupFixtureWith(t, fixtureOptions{Herdr: fake, Loop: runner})
	fx := &statuslineFixture{managerFixture: cf.managerFixture, Loop: l, Runner: runner}

	ws, openedAt := openStatusline(t, fx, cf.Repo.Dir)
	busy := asyncResult(func() error {
		_, err := cf.Manager.Cleanup(context.Background(), forceCleanup(cf.Prepared.Path, true))
		return err
	})
	if err := waitResult(t, busy, "NoWait の Cleanup"); !errors.Is(err, workspace.ErrCloneBusy) {
		t.Fatalf("Cleanup = %v, want ErrCloneBusy", err)
	}
	got := methodsAfter(fake, openedAt)
	if slices.Contains(got, herdr.MethodWorktreeOpen) || slices.Contains(got, herdr.MethodWorktreeRemove) {
		t.Fatalf("押さえられた clone で worktree.open か worktree.remove を送った: %v", got)
	}
	if _, err := os.Stat(cf.Prepared.Path); err != nil {
		t.Fatalf("ErrCloneBusy なのに worktree が消えた: %v", err)
	}
	if _, err := os.Stat(cf.SettingsPath); err != nil {
		t.Fatalf("ErrCloneBusy なのに設定ファイルが消えた: %v", err)
	}
	sawTry := false
	for _, c := range runner.Calls() {
		if c.Try && c.Key != "" && errors.Is(c.Err, loop.ErrBusy) {
			sawTry = true
		}
	}
	if !sawTry {
		t.Fatalf("引き直しの仕事が TryDo で積まれていない: %+v", runner.Calls())
	}

	if outcome, err := cf.Manager.CloseStatuslineWorkspace(context.Background(), ws); err != nil || outcome != workspace.StatuslineClosed {
		t.Fatalf("CloseStatuslineWorkspace = (%v, %v)", outcome, err)
	}
	result, err := cf.Manager.Cleanup(context.Background(), forceCleanup(cf.Prepared.Path, true))
	if err != nil {
		t.Fatalf("閉じたあとの NoWait の Cleanup が失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("閉じたあとの Cleanup で片付かなかった: %+v", *result)
	}
	if _, err := os.Stat(cf.Prepared.Path); !os.IsNotExist(err) {
		t.Fatalf("片付けたのに worktree が残っている: %v", err)
	}
}

// ===== 閉じる判定と、押さえを放すこと =====

// 目的: `herdr.worktree.create_via_herdr` が偽の人の片付けは、statusline取得の workspace が同じ clone で
// 開いていても待たないことを確かめる（`worktree.open` を1度も呼ばないので守るものが無い）。
// 与える情報: create_via_herdr を偽にした Manager で worktree を1つ用意し、statusline取得の workspace を
// 同じ clone に開いたまま、NoWait の Cleanup を呼ぶ。
// 成功条件: ErrCloneBusy で戻らずに Removed で返り、`worktree.open` が1度も届かないこと。
func TestStatusline_create_via_herdrが偽なら片付けは押さえを待たない(t *testing.T) {
	l := loop.New(nil)
	l.Start()
	t.Cleanup(l.Close)
	fake := newStatuslineHerdr(t)
	runner := &recordingRunner{inner: l, fake: fake, keyed: make(chan string, 16)}
	cf := newCleanupFixtureWith(t, fixtureOptions{
		Herdr:  fake,
		Loop:   runner,
		Mutate: func(cfg *config.Config) { cfg.Herdr.Worktree.CreateViaHerdr = false },
	})
	fx := &statuslineFixture{managerFixture: cf.managerFixture, Loop: l, Runner: runner}

	ws, _ := openStatusline(t, fx, cf.Repo.Dir)
	t.Cleanup(func() { _, _ = cf.Manager.CloseStatuslineWorkspace(context.Background(), ws) })

	result, err := cf.Manager.Cleanup(context.Background(), forceCleanup(cf.Prepared.Path, true))
	if err != nil {
		t.Fatalf("create_via_herdr が偽なのに片付けが押さえで止まった: %v", err)
	}
	if !result.Removed {
		t.Fatalf("片付けたのに Removed が偽: %+v", *result)
	}
	if slices.Contains(fake.Methods(), herdr.MethodWorktreeOpen) {
		t.Fatalf("create_via_herdr が偽なのに worktree.open を送った: %v", fake.Methods())
	}
}

// 目的: statusline取得の workspace が issue の親にされ、下に同じ clone の issue の worktree の
// workspace が居るなら、閉じずに StatuslineParentWithChild を返し、それでも押さえは放すことを確かめる
// （閉じると herdr 0.8.x では下の issue の pane まで消える）。
// 与える情報: 開いたあと、workspace.list が「w20 は clone の親（worktree 欄あり）・w9 は同じ clone の
// linked worktree」を返す形にしてから CloseStatuslineWorkspace を呼ぶ。そのあと同じ clone に Prepare する。
// 成功条件: StatuslineParentWithChild が返り、workspace.close を送らないこと。直後の Prepare が
// 待たずに成功する（押さえが放されている）こと。
func TestStatusline_親にされて子が居たら閉じず押さえは放す(t *testing.T) {
	fx := newStatuslineFixture(t, fixtureOptions{})
	ws, openedAt := openStatusline(t, fx, fx.Repo.Dir)
	child := filepath.Join(fx.Root, "child")
	fx.Herdr.SetResult(herdr.MethodWorkspaceList, workspaceListResult(
		statuslineEntry("w20", herdr.StatuslineFetchLabel, fx.Repo.Dir),
		workspaceEntry("w9", child, fx.Repo.Dir),
	))

	outcome, err := fx.Manager.CloseStatuslineWorkspace(context.Background(), ws)
	if err != nil || outcome != workspace.StatuslineParentWithChild {
		t.Fatalf("CloseStatuslineWorkspace = (%v, %v), want (StatuslineParentWithChild, nil)", outcome, err)
	}
	if got := methodsAfter(fx.Herdr, openedAt); slices.Contains(got, herdr.MethodWorkspaceClose) {
		t.Fatalf("子が居る親を閉じた: %v", got)
	}

	// 押さえが放されていれば、同じ clone の段7 はすぐ動く。
	fx.Herdr.SetResult(herdr.MethodWorkspaceList, workspaceListResult())
	prepared := asyncResult(func() error {
		_, err := fx.Manager.Prepare(context.Background(), sampleIssue(188))
		return err
	})
	if err := waitResult(t, prepared, "閉じなかったあとの Prepare"); err != nil {
		t.Fatalf("閉じなかったあとの Prepare が失敗した: %v", err)
	}
}

// 目的: `worktree` 欄を持っていても、同じ clone の linked worktree の workspace が居なければ閉じることを
// 確かめる（子の居なくなった親は閉じてよい）。
// 与える情報: workspace.list が「w20 は clone の親（worktree 欄あり）」だけを返す形。
// 成功条件: StatuslineClosed が返り、workspace.close を w20 に送ること。
func TestStatusline_worktree欄を持っていても子が居なければ閉じる(t *testing.T) {
	fx := newStatuslineFixture(t, fixtureOptions{})
	ws, openedAt := openStatusline(t, fx, fx.Repo.Dir)
	fx.Herdr.SetResult(herdr.MethodWorkspaceList, workspaceListResult(
		statuslineEntry("w20", herdr.StatuslineFetchLabel, fx.Repo.Dir),
	))

	outcome, err := fx.Manager.CloseStatuslineWorkspace(context.Background(), ws)
	if err != nil || outcome != workspace.StatuslineClosed {
		t.Fatalf("CloseStatuslineWorkspace = (%v, %v), want (StatuslineClosed, nil)", outcome, err)
	}
	var closed *recordedRequest
	for _, r := range fx.Herdr.Requests()[openedAt:] {
		if r.Method == herdr.MethodWorkspaceClose {
			closed = &r
		}
	}
	if closed == nil || closed.Params["workspace_id"] != "w20" {
		t.Fatalf("w20 に workspace.close を送っていない: %+v", fx.Herdr.Requests()[openedAt:])
	}
}

// 目的: 閉じられなかったとき（一覧が引けない・workspace.close が断られた）も、StatuslineCloseFailed と
// 理由を返し、押さえは放すことを確かめる（押さえを放すのは閉じる仕事だけなので、残すと
// その clone の着手が止まり続ける）。一覧に無い ID は閉じたものとして扱うことも確かめる。
// 与える情報: workspace.list が誤りを返す形・workspace.close が誤りを返す形・一覧に w20 が無い形。
// 成功条件: 前の2つは StatuslineCloseFailed と誤りが返り、直後の同じ clone の Prepare が待たずに
// 進む（一覧が引けない形では、段7 の一覧の引き直しは失敗しても進む）こと。一覧に無い形は
// StatuslineClosed が返り、workspace.close を送らないこと。
func TestStatusline_閉じられなくても押さえは放す(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(fx *statuslineFixture)
	}{
		{"一覧が引けない", func(fx *statuslineFixture) {
			fx.Herdr.RemoveResult(herdr.MethodWorkspaceList)
		}},
		{"閉じるのを断られた", func(fx *statuslineFixture) {
			fx.Herdr.SetResult(herdr.MethodWorkspaceList,
				workspaceListResult(statuslineEntry("w20", herdr.StatuslineFetchLabel, "")))
			fx.Herdr.RemoveResult(herdr.MethodWorkspaceClose)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newStatuslineFixture(t, fixtureOptions{})
			ws, _ := openStatusline(t, fx, fx.Repo.Dir)
			tc.arrange(fx)

			outcome, err := fx.Manager.CloseStatuslineWorkspace(context.Background(), ws)
			if outcome != workspace.StatuslineCloseFailed || err == nil {
				t.Fatalf("CloseStatuslineWorkspace = (%v, %v), want (StatuslineCloseFailed, 誤り)", outcome, err)
			}

			prepared := asyncResult(func() error {
				_, err := fx.Manager.Prepare(context.Background(), sampleIssue(188))
				return err
			})
			if err := waitResult(t, prepared, "閉じられなかったあとの Prepare"); err != nil {
				t.Fatalf("閉じられなかったあとの Prepare が失敗した: %v", err)
			}
		})
	}

	t.Run("一覧に無い", func(t *testing.T) {
		fx := newStatuslineFixture(t, fixtureOptions{})
		ws, openedAt := openStatusline(t, fx, fx.Repo.Dir)
		outcome, err := fx.Manager.CloseStatuslineWorkspace(context.Background(), ws)
		if err != nil || outcome != workspace.StatuslineClosed {
			t.Fatalf("CloseStatuslineWorkspace = (%v, %v), want (StatuslineClosed, nil)", outcome, err)
		}
		if got := methodsAfter(fx.Herdr, openedAt); slices.Contains(got, herdr.MethodWorkspaceClose) {
			t.Fatalf("一覧に無い workspace に workspace.close を送った: %v", got)
		}
	})
}

// 目的: 閉じる仕事は、呼び出し側の ctx が取り消されていても、前に並んだ仕事が長引いても、
// 消えずに動いて押さえを放すことを確かめる（止めるときにも取り消さない。押さえを放すのは閉じる仕事だけ）。
// 与える情報: 開いたあと loop を門で止め、取り消し済みの ctx で CloseStatuslineWorkspace を呼び、
// しばらくしてから門を開ける。
// 成功条件: CloseStatuslineWorkspace が StatuslineClosed を返し、workspace.close を送り、
// 直後の同じ clone の Prepare が待たずに進むこと。
func TestStatusline_閉じる仕事は取り消されたctxでも前の仕事が長引いても動いて放す(t *testing.T) {
	fx := newStatuslineFixture(t, fixtureOptions{})
	ws, openedAt := openStatusline(t, fx, fx.Repo.Dir)
	fx.Herdr.SetResult(herdr.MethodWorkspaceList,
		workspaceListResult(statuslineEntry("w20", herdr.StatuslineFetchLabel, "")))
	fx.Herdr.SetOnRequest(herdr.MethodWorkspaceClose, func(map[string]any) {
		fx.Herdr.SetResult(herdr.MethodWorkspaceList, workspaceListResult())
	})

	gate := make(chan struct{})
	blocker := asyncResult(func() error {
		return fx.Loop.Do(context.Background(), "", func(context.Context, *loop.Job) error {
			<-gate
			return nil
		})
	})
	time.Sleep(50 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	type closeResult struct {
		outcome workspace.StatuslineCloseOutcome
		err     error
	}
	closed := make(chan closeResult, 1)
	go func() {
		outcome, err := fx.Manager.CloseStatuslineWorkspace(ctx, ws)
		closed <- closeResult{outcome, err}
	}()
	time.Sleep(holdWait)
	close(gate)
	if err := waitResult(t, blocker, "前の仕事"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-closed:
		if got.err != nil || got.outcome != workspace.StatuslineClosed {
			t.Fatalf("CloseStatuslineWorkspace = (%v, %v), want (StatuslineClosed, nil)", got.outcome, got.err)
		}
	case <-time.After(finishWait):
		t.Fatal("閉じる仕事が返りませんでした")
	}
	if got := methodsAfter(fx.Herdr, openedAt); !slices.Contains(got, herdr.MethodWorkspaceClose) {
		t.Fatalf("取り消し済みの ctx で閉じる仕事が消えた: %v", got)
	}
	prepared := asyncResult(func() error {
		_, err := fx.Manager.Prepare(context.Background(), sampleIssue(188))
		return err
	})
	if err := waitResult(t, prepared, "閉じたあとの Prepare"); err != nil {
		t.Fatalf("閉じたあとの Prepare が失敗した: %v", err)
	}
}

// 目的: 作る仕事は、順番待ちの間に呼び出し側の ctx が期限切れになっても取り消されず、
// 取り消されたときだけ外れることを確かめる（statusline取得1回の全体の上限の期限を loop へ流し込まず、
// 止めるときの取り消しは生かす）。
// 与える情報: loop を門で止めた状態で、(1) 50ms で期限の切れる ctx、(2) 取り消す ctx で
// OpenStatuslineWorkspace を呼ぶ。
// 成功条件: (1) は門を開けたあとに w20 を返す。(2) は context.Canceled を返し、workspace.create を送らない。
func TestOpenStatuslineWorkspace_期限切れでは外れず取り消しでは外れる(t *testing.T) {
	block := func(fx *statuslineFixture) (chan struct{}, <-chan error) {
		gate := make(chan struct{})
		blocker := asyncResult(func() error {
			return fx.Loop.Do(context.Background(), "", func(context.Context, *loop.Job) error {
				<-gate
				return nil
			})
		})
		time.Sleep(50 * time.Millisecond)
		return gate, blocker
	}

	t.Run("期限切れ", func(t *testing.T) {
		fx := newStatuslineFixture(t, fixtureOptions{})
		gate, blocker := block(fx)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		type openResult struct {
			ws  workspace.StatuslineWorkspace
			err error
		}
		opened := make(chan openResult, 1)
		go func() {
			ws, err := fx.Manager.OpenStatuslineWorkspace(ctx, fx.Repo.Dir)
			opened <- openResult{ws, err}
		}()
		time.Sleep(holdWait)
		close(gate)
		if err := waitResult(t, blocker, "前の仕事"); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-opened:
			if got.err != nil || got.ws.WorkspaceID != "w20" {
				t.Fatalf("期限の切れた ctx の OpenStatuslineWorkspace = (%+v, %v), want w20", got.ws, got.err)
			}
		case <-time.After(finishWait):
			t.Fatal("OpenStatuslineWorkspace が返りませんでした")
		}
	})

	t.Run("取り消し", func(t *testing.T) {
		fx := newStatuslineFixture(t, fixtureOptions{})
		gate, blocker := block(fx)
		ctx, cancel := context.WithCancel(context.Background())
		opened := asyncResult(func() error {
			_, err := fx.Manager.OpenStatuslineWorkspace(ctx, fx.Repo.Dir)
			return err
		})
		time.Sleep(50 * time.Millisecond)
		cancel()
		if err := waitResult(t, opened, "OpenStatuslineWorkspace"); !errors.Is(err, context.Canceled) {
			t.Fatalf("取り消した OpenStatuslineWorkspace = %v, want context.Canceled", err)
		}
		close(gate)
		if err := waitResult(t, blocker, "前の仕事"); err != nil {
			t.Fatal(err)
		}
		// loop を1周させてから、作る仕事が動いていないことを見る。
		if err := fx.Loop.Do(context.Background(), "", func(context.Context, *loop.Job) error { return nil }); err != nil {
			t.Fatal(err)
		}
		if slices.Contains(fx.Herdr.Methods(), herdr.MethodWorkspaceCreate) {
			t.Fatalf("取り消した作る仕事が workspace.create を送った: %v", fx.Herdr.Methods())
		}
	})
}

// 目的: statusline取得1回の全体の上限（長い期限）の ctx で OpenStatuslineWorkspace を呼んでも、
// 中の herdr の呼び出しが呼び出しごとの期限（herdr.read_timeout_ms）で終わることを確かめる
// （herdr のクライアントは ctx に期限があるとそちらを使うので、流し込むと loop が最長約5分止まる）。
// 与える情報: 接続を受けても何も返さない herdr と、呼び出しごとの期限 300ms のクライアント。
// 1時間の期限を持つ ctx。
// 成功条件: OpenStatuslineWorkspace が数秒のうちに誤りを返すこと。
func TestOpenStatuslineWorkspace_全体の上限のctxでも呼び出しごとの期限で終わる(t *testing.T) {
	sock := silentHerdr(t)
	l := loop.New(nil)
	l.Start()
	t.Cleanup(l.Close)
	cfg := *config.DefaultConfig()
	cfg.Workspace.Root = filepath.Join(t.TempDir(), "worktrees")
	mgr, err := workspace.New(workspace.Options{
		Config:  cfg,
		Herdr:   herdr.New(sock, herdr.Timeouts{Read: 300 * time.Millisecond}),
		Loop:    l,
		HomeDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("workspace.New に失敗した: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	started := time.Now()
	_, err = mgr.OpenStatuslineWorkspace(ctx, t.TempDir())
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("応答しない herdr なのに OpenStatuslineWorkspace が成功した")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("OpenStatuslineWorkspace が %v 掛かった（ctx の期限が herdr の呼び出しへ流れ込んでいる）", elapsed)
	}
}

// silentHerdr は、接続を受けて1行読んだまま何も返さない socket を立てる。
//
// 戻り値: socket のパス。テストの終わりに閉じる。
func silentHerdr(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "wssilent")
	if err != nil {
		t.Fatalf("一時ディレクトリを作成できません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("socket を listen できない: %v", err)
	}
	done := make(chan struct{})
	var conns []net.Conn
	var mu sync.Mutex
	t.Cleanup(func() {
		close(done)
		_ = ln.Close()
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
	})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	return path
}

// ===== 閉じ残しの片付け =====

// 目的: 閉じ残しの片付けが、herdr の一覧に在って label が statusline取得のもので、子の居ないものだけを
// 閉じることを確かめる。
// 与える情報: 閉じ残しの一覧 [w30 w31 w32 w33]。herdr の一覧は、w30 が statusline取得の label で
// worktree 欄なし、w31 が別の label（ID が使い回された）、w32 は無い、w33 が statusline取得の label で
// clone の親にされ、下に同じ clone の linked worktree の workspace w9 が居る。
// 成功条件: 残りとして [w33] が返ること。workspace.close は w30 にだけ送ること。
func TestCloseLeftoverStatuslineWorkspaces_labelの合う子の居ないものだけ閉じる(t *testing.T) {
	fx := newStatuslineFixture(t, fixtureOptions{})
	fx.Herdr.SetResult(herdr.MethodWorkspaceList, workspaceListResult(
		statuslineEntry("w30", herdr.StatuslineFetchLabel, ""),
		statuslineEntry("w31", "octocat/hello-world/issues/5", ""),
		statuslineEntry("w33", herdr.StatuslineFetchLabel, fx.Repo.Dir),
		workspaceEntry("w9", filepath.Join(fx.Root, "child"), fx.Repo.Dir),
	))
	before := len(fx.Herdr.Requests())

	remaining := fx.Manager.CloseLeftoverStatuslineWorkspaces(context.Background(), []string{"w30", "w31", "w32", "w33"})

	if !slices.Equal(remaining, []string{"w33"}) {
		t.Fatalf("残り = %v, want [w33]", remaining)
	}
	var closedIDs []string
	for _, r := range fx.Herdr.Requests()[before:] {
		if r.Method == herdr.MethodWorkspaceClose {
			closedIDs = append(closedIDs, fmt.Sprint(r.Params["workspace_id"]))
		}
	}
	if !slices.Equal(closedIDs, []string{"w30"}) {
		t.Fatalf("閉じた workspace = %v, want [w30]", closedIDs)
	}
}

// 目的: 閉じ残しの片付けで、一覧が引けないときは全部を残し、閉じるのを断られたものは残し、
// 一覧が空なら herdr を呼ばないことを確かめる。
// 与える情報: (1) workspace.list が誤りを返す、(2) workspace.close が誤りを返す、(3) 空の一覧。
// 成功条件: (1) は渡した ID が全部残る。(2) は断られた ID が残る。(3) は nil が返り、herdr へ何も送らない。
func TestCloseLeftoverStatuslineWorkspaces_引けないときと断られたときは残す(t *testing.T) {
	t.Run("一覧が引けない", func(t *testing.T) {
		fx := newStatuslineFixture(t, fixtureOptions{})
		fx.Herdr.RemoveResult(herdr.MethodWorkspaceList)
		remaining := fx.Manager.CloseLeftoverStatuslineWorkspaces(context.Background(), []string{"w30", "w31"})
		if !slices.Equal(remaining, []string{"w30", "w31"}) {
			t.Fatalf("残り = %v, want [w30 w31]", remaining)
		}
	})
	t.Run("閉じるのを断られた", func(t *testing.T) {
		fx := newStatuslineFixture(t, fixtureOptions{})
		fx.Herdr.SetResult(herdr.MethodWorkspaceList, workspaceListResult(
			statuslineEntry("w30", herdr.StatuslineFetchLabel, ""),
		))
		fx.Herdr.RemoveResult(herdr.MethodWorkspaceClose)
		remaining := fx.Manager.CloseLeftoverStatuslineWorkspaces(context.Background(), []string{"w30"})
		if !slices.Equal(remaining, []string{"w30"}) {
			t.Fatalf("残り = %v, want [w30]", remaining)
		}
	})
	t.Run("一覧が空", func(t *testing.T) {
		fx := newStatuslineFixture(t, fixtureOptions{})
		before := len(fx.Herdr.Requests())
		if remaining := fx.Manager.CloseLeftoverStatuslineWorkspaces(context.Background(), nil); remaining != nil {
			t.Fatalf("残り = %v, want nil", remaining)
		}
		if got := methodsAfter(fx.Herdr, before); len(got) != 0 {
			t.Fatalf("閉じ残しが無いのに herdr を呼んだ: %v", got)
		}
	})
}

// ===== 段7 が1つの仕事であること =====

// 目的: 着手の段7（リポジトリの親 workspace の有無を見る workspace.list → worktree.open →
// 親の ID を控える workspace.list → workspace.rename）が、clone の key を持つ1つの仕事として
// loop に積まれることを確かめる（一続きの途中に、statusline取得の workspace を作る仕事などが割り込まない）。
// 与える情報: 本物の loop を包んだ控えを渡した Manager で Prepare する。
// 成功条件: key を持つ仕事がちょうど1つで、その仕事の中で上の4つがこの順に送られ、
// Prepare の herdr の呼び出しがほかの仕事にも仕事の外にも無いこと。
func TestPrepare_段7は1つの仕事でcloneのkeyを持つ(t *testing.T) {
	fx := newStatuslineFixture(t, fixtureOptions{})

	prepareWorktree(t, fx.managerFixture, sampleIssue(188))

	var keyed []runnerCall
	inside := 0
	for _, c := range fx.Runner.Calls() {
		inside += len(c.Methods)
		if c.Key != "" {
			keyed = append(keyed, c)
		}
	}
	if len(keyed) != 1 {
		t.Fatalf("key を持つ仕事の数 = %d, want 1: %+v", len(keyed), fx.Runner.Calls())
	}
	want := []string{
		herdr.MethodWorkspaceList, herdr.MethodWorktreeOpen,
		herdr.MethodWorkspaceList, herdr.MethodWorkspaceRename,
	}
	if !slices.Equal(keyed[0].Methods, want) {
		t.Fatalf("段7 の仕事の中の呼び出し = %v, want %v", keyed[0].Methods, want)
	}
	if !strings.HasPrefix(keyed[0].Key, "clone:") {
		t.Fatalf("段7 の key = %q, want clone: で始まる", keyed[0].Key)
	}
	if total := len(fx.Herdr.Requests()); total != inside {
		t.Fatalf("loop の仕事の外で herdr を %d 回呼んだ: %v", total-inside, fx.Herdr.Methods())
	}
}

// ===== シンボリックリンクを挟んだパス =====

// 目的: ghq が返すパスがシンボリックリンクでも、実体のパスで開いた statusline取得の workspace の
// 押さえに段7 が当たる（同じ clone として後に回る）ことを確かめる。逆に、リンクのパスで開いたときに、
// git の共通ディレクトリから作った実体のパスを使う片付け（NoWait）が当たることも確かめる。
// 与える情報: clone の実体を指すシンボリックリンクを作り、ghq はリンクのパスを返す。
// (1) 実体のパスで開いて Prepare、(2) リンクのパスで開いて NoWait の Cleanup。
// 成功条件: (1) は段7 が閉じるまで worktree.open を送らず、閉じたあとに Prepare が成功する。
// (2) は ErrCloneBusy が返る。
func TestStatusline_シンボリックリンクを挟んだパスでも同じcloneとして押さえる(t *testing.T) {
	repo := newTestRepo(t)
	link := filepath.Join(t.TempDir(), "clone-link")
	if err := os.Symlink(repo.Dir, link); err != nil {
		t.Fatalf("シンボリックリンクを作れない: %v", err)
	}
	fx := newStatuslineFixture(t, fixtureOptions{
		Repo:    repo,
		GhqList: func(context.Context, string, string) (string, error) { return link, nil },
	})

	ws, openedAt := openStatusline(t, fx, repo.Dir)
	prepared := asyncResult(func() error {
		_, err := fx.Manager.Prepare(context.Background(), sampleIssue(188))
		return err
	})
	fx.Runner.waitKeyed(t)
	time.Sleep(holdWait)
	assertNotReturned(t, prepared, "リンクのパスの Prepare")
	if got := methodsAfter(fx.Herdr, openedAt); slices.Contains(got, herdr.MethodWorktreeOpen) {
		t.Fatalf("実体のパスで押さえているのに、リンクのパスの段7 が worktree.open を送った: %v", got)
	}
	if outcome, err := fx.Manager.CloseStatuslineWorkspace(context.Background(), ws); err != nil || outcome != workspace.StatuslineClosed {
		t.Fatalf("CloseStatuslineWorkspace = (%v, %v)", outcome, err)
	}
	if err := waitResult(t, prepared, "リンクのパスの Prepare"); err != nil {
		t.Fatalf("閉じたあとの Prepare が失敗した: %v", err)
	}

	// (2) リンクのパスで開き、git から実体のパスを引く片付けが当たるか。
	var worktreePath string
	for _, r := range fx.Herdr.Requests() {
		if r.Method == herdr.MethodWorktreeOpen {
			worktreePath, _ = r.Params["path"].(string)
		}
	}
	if worktreePath == "" {
		t.Fatal("段7 の worktree.open に path が無い")
	}
	if err := fx.Manager.WriteIdentity(context.Background(), worktreePath, workspace.Identity{
		IssueURL:        sampleIssue(188).URL,
		IssueIdentifier: sampleIssue(188).Identifier,
		ProjectItemID:   "PVTI_test",
		CreatedAt:       time.Now(),
	}); err != nil {
		t.Fatalf("WriteIdentity に失敗した: %v", err)
	}
	if _, err := fx.Manager.OpenStatuslineWorkspace(context.Background(), link); err != nil {
		t.Fatalf("リンクのパスで OpenStatuslineWorkspace に失敗した: %v", err)
	}
	_, err := fx.Manager.Cleanup(context.Background(), forceCleanup(worktreePath, true))
	if !errors.Is(err, workspace.ErrCloneBusy) {
		t.Fatalf("リンクのパスで押さえた clone の NoWait の Cleanup = %v, want ErrCloneBusy", err)
	}
}

// 目的: 押さえたあとにシンボリックリンクの先が変わっても、CloseStatuslineWorkspace が押さえた key を
// 放すことを確かめる（パスから key を作り直すと別の key になり、押さえが永久に残る）。
// 与える情報: clone の実体を指すリンクのパスで開いたあと、リンクを別のディレクトリへ張り替えてから閉じる。
// 成功条件: 閉じたあと、実体のパスを返す ghq の Prepare が待たずに成功すること。
func TestStatusline_押さえたあとにリンクの先が変わっても閉じると放す(t *testing.T) {
	repo := newTestRepo(t)
	link := filepath.Join(t.TempDir(), "clone-link")
	if err := os.Symlink(repo.Dir, link); err != nil {
		t.Fatalf("シンボリックリンクを作れない: %v", err)
	}
	fx := newStatuslineFixture(t, fixtureOptions{Repo: repo})

	ws, _ := openStatusline(t, fx, link)
	if err := os.Remove(link); err != nil {
		t.Fatalf("リンクを消せない: %v", err)
	}
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatalf("リンクを張り替えられない: %v", err)
	}
	if outcome, err := fx.Manager.CloseStatuslineWorkspace(context.Background(), ws); err != nil || outcome != workspace.StatuslineClosed {
		t.Fatalf("CloseStatuslineWorkspace = (%v, %v)", outcome, err)
	}

	prepared := asyncResult(func() error {
		_, err := fx.Manager.Prepare(context.Background(), sampleIssue(188))
		return err
	})
	if err := waitResult(t, prepared, "張り替えたあとの Prepare"); err != nil {
		t.Fatalf("張り替えたあとの Prepare が失敗した: %v", err)
	}
}

// ===== 組み立て =====

// 目的: herdr を渡して loop を渡さない Manager は作れないことを確かめる（渡し忘れを黙って
// その場で呼ぶ形にすると、statusline取得の workspace が issue の親にされる守りが黙って消える）。
// 与える情報: Herdr だけを渡し、Loop を渡さない Options。
// 成功条件: workspace.New が誤りを返し、その文言が loop の渡し忘れのものであること。
func TestNew_herdrを渡してloopを渡さないと誤りになる(t *testing.T) {
	cfg := *config.DefaultConfig()
	cfg.Workspace.Root = filepath.Join(t.TempDir(), "worktrees")
	fake := newStatuslineHerdr(t)

	_, err := workspace.New(workspace.Options{Config: cfg, Herdr: fake.Client(), HomeDir: t.TempDir()})
	if err == nil {
		t.Fatal("herdr を渡して loop を渡さないのに workspace.New が成功した")
	}
	if want := i18n.T(i18n.KeyWorkspaceNewLoopMissing); err.Error() != want {
		t.Fatalf("誤りの文言 = %q, want %q", err.Error(), want)
	}
}

// 目的: herdr を渡さない Manager（`continuo doctor`・テストの一部）では、statusline取得の3つの
// メソッドが誤りを返し（閉じ残しは全部残す）、片付けも落ちずに進むことを確かめる。
// 与える情報: Herdr も Loop も渡さず、herdr.worktree.create_via_herdr を偽にした Manager。
// 成功条件: OpenStatuslineWorkspace と CloseStatuslineWorkspace が誤りを返し、
// CloseLeftoverStatuslineWorkspaces が渡した ID をそのまま返すこと。Prepare した worktree を
// Cleanup すると落ちずに Removed で返ること。
func TestStatusline_herdrの無いManagerではstatuslineのメソッドが誤りを返し片付けは進む(t *testing.T) {
	repo := newTestRepo(t)
	cfg := *config.DefaultConfig()
	cfg.Workspace.Root = filepath.Join(t.TempDir(), "worktrees")
	cfg.Herdr.Worktree.CreateViaHerdr = false
	mgr, err := workspace.New(workspace.Options{
		Config:       cfg,
		HomeDir:      t.TempDir(),
		GhqList:      func(context.Context, string, string) (string, error) { return repo.Dir, nil },
		SettingsRoot: filepath.Join(t.TempDir(), "issues"),
	})
	if err != nil {
		t.Fatalf("workspace.New に失敗した: %v", err)
	}

	if _, err := mgr.OpenStatuslineWorkspace(context.Background(), repo.Dir); err == nil {
		t.Fatal("herdr が無いのに OpenStatuslineWorkspace が成功した")
	}
	if outcome, err := mgr.CloseStatuslineWorkspace(context.Background(), workspace.StatuslineWorkspace{WorkspaceID: "w20"}); err == nil || outcome != workspace.StatuslineCloseFailed {
		t.Fatalf("CloseStatuslineWorkspace = (%v, %v), want (StatuslineCloseFailed, 誤り)", outcome, err)
	}
	if remaining := mgr.CloseLeftoverStatuslineWorkspaces(context.Background(), []string{"w30"}); !slices.Equal(remaining, []string{"w30"}) {
		t.Fatalf("CloseLeftoverStatuslineWorkspaces = %v, want [w30]", remaining)
	}

	prepared, err := mgr.Prepare(context.Background(), sampleIssue(188))
	if err != nil {
		t.Fatalf("Prepare に失敗した: %v", err)
	}
	if err := mgr.WriteIdentity(context.Background(), prepared.Path, workspace.Identity{
		IssueURL:        sampleIssue(188).URL,
		IssueIdentifier: sampleIssue(188).Identifier,
		ProjectItemID:   "PVTI_test",
		Branch:          prepared.Branch.String(),
		CreatedAt:       time.Now(),
	}); err != nil {
		t.Fatalf("WriteIdentity に失敗した: %v", err)
	}
	result, err := mgr.Cleanup(context.Background(), forceCleanup(prepared.Path, true))
	if err != nil {
		t.Fatalf("herdr の無い Manager の Cleanup が失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("herdr の無い Manager の Cleanup で片付かなかった: %+v", *result)
	}
}
