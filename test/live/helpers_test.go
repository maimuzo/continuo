// Package live_test は**本物の外部サービスを叩くテスト**を置く場所である（issue #4）。
//
// **叩くのは herdr だけである。**それ以外は本物を叩かない。
//
//	| 相手                   | どうするか     | なぜ |
//	| ---------------------- | -------------- | ---- |
//	| herdr                  | 本物で叩く     | 常駐していれば無料で速い。ずれが実機を壊した実績がある |
//	| Claude Code            | 叩かない       | 枠を消費する。自動テストからは絶対に起動しない |
//	| GitHub の GraphQL / gh | 叩かない       | 認証と本番のボードが要る |
//	| git / ghq              | 既に本物を使う | test/internal/workspace が担当済み |
//
// **なぜ要るのか。**test/e2e の偽 herdr は「continuo が正しいと思っている振る舞い」しか
// 返さない。そのため worktree.open の引数のずれ（path と branch を両方渡していた）が
// テストを素通りし、実機の着手が全件落ちた（2026-08-20）。
// **ここは、その手のずれを手元で捕まえるための唯一の経路である。**
//
// **走るのは手元だけである。**herdr が PATH に無い・socket が無い・socket へ繋がらない
// のいずれかなら t.Skip で飛ばす。CI（GitHub の runner）には herdr が居ないので必ず飛ぶ。
// scripts/test-like-ci.sh も PATH から herdr を隠すので、同じく飛ぶ。
// **build タグは使わない。**タグにすると「付け忘れて一度も走らない」が起きるが、
// skip なら go test の出力に理由が残る。
//
// **後始末の規則は3つである。**
//  1. worktree は t.TempDir() の下に作る。continuo の巡回が拾う範囲（workspace.root）の外に出す
//  2. workspace を作った時点で後始末に登録する。**アサーションより先に登録する**
//  3. 後始末に失敗したら t.Errorf で落とす。**ゴミを黙って残さない**
package live_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// livePingTimeout は「herdr が生きているか」を確かめる ping の待ち時間である。
// 生きていれば即答するので短くてよい。返らなければテストを飛ばす。
const livePingTimeout = 10 * time.Second

// liveCleanupTimeout は後始末（worktree.remove と pane.list）に使う待ち時間である。
// worktree.remove は git を動かすので、Timeouts.Startup（既定60秒）に合わせて長めに取る。
const liveCleanupTimeout = 2 * time.Minute

// liveLabelPrefix はテストが herdr へ書く label と branch 名の頭に必ず付ける文字列である。
// **消し残しを人間が herdr の画面で見分けられるようにするため**に付ける。
const liveLabelPrefix = "continuo-live-test"

// requireLiveHerdr は本物の herdr へ繋がる Client を返す。繋がらなければテストを飛ばす。
//
// **黙って通さない。**t.Skip はテストの一覧に理由つきで残るので、
// 何が確かめられなかったかが後から分かる（test/internal/trust の requireCommands と同じ考え方）。
//
// t: 呼び出し元のテスト。
// 戻り値: ping が通った herdr の Client。
func requireLiveHerdr(t *testing.T) *herdr.Client {
	t.Helper()

	if _, err := exec.LookPath("herdr"); err != nil {
		t.Skipf("herdr が PATH に無いので、本物を叩く検査は実行できません: %v", err)
	}
	socket, err := herdr.ResolveSocketPath("")
	if err != nil {
		t.Skipf("herdr の socket のパスを決められないので、本物を叩く検査は実行できません: %v", err)
	}
	if _, err := os.Stat(socket); err != nil {
		t.Skipf("herdr の socket が無いので、本物を叩く検査は実行できません: %v", err)
	}

	client := herdr.New(socket, herdr.Timeouts{})
	ctx, cancel := context.WithTimeout(context.Background(), livePingTimeout)
	defer cancel()
	if _, err := client.Ping(ctx); err != nil {
		t.Skipf("herdr の socket へ繋がらないので、本物を叩く検査は実行できません: %v", err)
	}
	return client
}

// liveRepo はテストが使う、使い捨ての git リポジトリである。
type liveRepo struct {
	// Root は t.TempDir() を symlink 解決した絶対パスである。
	// **worktree はこの下に作る。**continuo の置き場所（workspace.root）とは重ならない。
	Root string
	// Path はリポジトリ本体（worktree を切る元）の絶対パスである。
	Path string
}

// newLiveRepo は初期コミットを1つ持つ使い捨ての git リポジトリを t.TempDir() の下に作る。
//
// **置き場所を t.TempDir() にするのは、continuo の巡回が拾う範囲の外に出すためである。**
// 置き場所（workspace.root）の下に作ると、消し残したときに本物の continuo が
// それを run として拾いにかかる。
//
// t: 呼び出し元のテスト。失敗したらテストを止める。
// 戻り値: 作ったリポジトリ。既定 branch は "main"。
func newLiveRepo(t *testing.T) liveRepo {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("一時ディレクトリの symlink を解決できません: %v", err)
	}
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("リポジトリのディレクトリを作れません: %v", err)
	}

	runGit(t, "", "init", "-b", "main", repo)
	runGit(t, repo, "config", "user.email", "live-test@example.invalid")
	runGit(t, repo, "config", "user.name", "continuo live test")
	runGit(t, repo, "config", "commit.gpgsign", "false")
	runGit(t, repo, "commit", "--allow-empty", "-m", "初期コミット")

	return liveRepo{Root: root, Path: repo}
}

// addWorktree は使い捨ての worktree を1つ切る。
//
// t: 呼び出し元のテスト。失敗したらテストを止める。
// repo: 元になるリポジトリ。
// 戻り値の1つ目: 切った worktree の絶対パス。
// 戻り値の2つ目: その worktree が指す branch 名。
func addWorktree(t *testing.T, repo liveRepo) (string, string) {
	t.Helper()

	suffix := randomSuffix(t)
	path := filepath.Join(repo.Root, "worktree-"+suffix)
	branch := liveLabelPrefix + "/" + suffix
	runGit(t, repo.Path, "worktree", "add", "-b", branch, path)
	return path, branch
}

// runGit はテストの中で git を実行し、標準出力を返す。
//
// t: 呼び出し元のテスト。失敗したらテストを止める。
// dir: `-C` に渡す作業ディレクトリ。空文字なら付けない。
// args: git の引数。
// 戻り値: 標準出力（前後の空白を落としたもの）。
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("`git %s` に失敗した: %v\n%s", strings.Join(full, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// randomSuffix は他の実行と衝突しない短い識別子を作る。
//
// t: 呼び出し元のテスト。失敗したらテストを止める。
// 戻り値: 16進8桁の文字列。
func randomSuffix(t *testing.T) string {
	t.Helper()

	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatalf("乱数を作れません: %v", err)
	}
	return hex.EncodeToString(buf[:])
}

// liveJanitor はテストが herdr に作らせたものを、必ず消して回る後始末係である。
//
// **使い方は「叩く前に登録、作った直後に控える」である。**
//
//	janitor := newLiveJanitor(t, client, repo.Root)      // ← 呼び出しより前に t.Cleanup へ入る
//	opened, err := client.WorktreeOpen(ctx, params)
//	if err != nil { t.Fatalf(...) }
//	janitor.TrackWorkspace(opened.Workspace.WorkspaceID) // ← アサーションより先
//
// **アサーションより先に控えるのは、途中で t.Fatalf しても消えるようにするためである。**
//
// **控えた ID を消すだけでは足りない。**worktree.open に cwd を渡すと、herdr は
// 「その worktree の workspace」に加えて「cwd のリポジトリの workspace」も開く
// （実測: 2026-08-24）。worktree.remove は前者しか閉じない。そこで最後に
// workspace.list を掃き、**roots の下を指す workspace を残らず閉じる。**
type liveJanitor struct {
	// t は後始末の失敗を報告する先である。
	t *testing.T
	// client は herdr の呼び出し口である。
	client *herdr.Client
	// roots は「テストが作ったもの」と見なすディレクトリである（t.TempDir() を
	// symlink 解決したもの）。この下を指す workspace は自分が作ったものと判断して閉じる。
	// **既に走っている herdr の pane / workspace に触らないための境界である。**
	roots []string
	// mu は下の2つを守る。
	mu sync.Mutex
	// workspaceIDs は消すべき herdr workspace の ID である（登録順、重複なし）。
	workspaceIDs []string
	// paneIDs は消えたことを確かめる pane の ID である（登録順、重複なし）。
	paneIDs []string
}

// newLiveJanitor は後始末係を作り、t.Cleanup へ登録する。
//
// **herdr を叩く前に呼ぶこと。**
//
// t: 呼び出し元のテスト。
// client: herdr の呼び出し口。
// roots: テストが作ったディレクトリ（liveRepo.Root）。この下を指す workspace だけを閉じる。
// 戻り値: 後始末係。
func newLiveJanitor(t *testing.T, client *herdr.Client, roots ...string) *liveJanitor {
	t.Helper()

	j := &liveJanitor{t: t, client: client, roots: roots}
	t.Cleanup(j.run)
	return j
}

// isMine は path が roots のどれかの下にあるかどうかを判定する。
//
// path: 判定するパス。
// 戻り値: roots のどれかの下にあれば true。
func (j *liveJanitor) isMine(path string) bool {
	if path == "" {
		return false
	}
	target := resolvedPath(path)
	for _, root := range j.roots {
		if root == "" {
			continue
		}
		rel, err := filepath.Rel(resolvedPath(root), target)
		if err != nil {
			continue
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// strayWorkspaces は roots の下を指す workspace の ID を集める。
//
// ctx: 呼び出しに適用するコンテキスト。
// 戻り値の1つ目: 該当する workspace の ID。
// 戻り値の2つ目: workspace.list に失敗した場合のエラー。
func (j *liveJanitor) strayWorkspaces(ctx context.Context) ([]string, error) {
	list, err := j.client.WorkspaceList(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, ws := range list.Workspaces {
		if ws.Worktree == nil {
			continue
		}
		if j.isMine(ws.Worktree.CheckoutPath) || j.isMine(ws.Worktree.RepoRoot) {
			ids = append(ids, ws.WorkspaceID)
		}
	}
	return ids, nil
}

// TrackWorkspace は消すべき herdr workspace を控える。同じ ID を2度控えても1度しか消さない。
//
// id: worktree.open / worktree.create が返した workspace の ID。空文字は無視する。
func (j *liveJanitor) TrackWorkspace(id string) {
	if id == "" {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, known := range j.workspaceIDs {
		if known == id {
			return
		}
	}
	j.workspaceIDs = append(j.workspaceIDs, id)
}

// TrackPane は消えたことを確かめる pane を控える。
//
// id: pane の ID。空文字は無視する。
func (j *liveJanitor) TrackPane(id string) {
	if id == "" {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, known := range j.paneIDs {
		if known == id {
			return
		}
	}
	j.paneIDs = append(j.paneIDs, id)
}

// Forget は控えた workspace を対象から外す。**テストが自分で worktree.remove を
// 呼び終えたときに使う。**控えたままにすると2度目の呼び出しが失敗し、
// 後始末に失敗したという偽の報告になる。
//
// id: 対象から外す workspace の ID。
func (j *liveJanitor) Forget(id string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	kept := j.workspaceIDs[:0]
	for _, known := range j.workspaceIDs {
		if known != id {
			kept = append(kept, known)
		}
	}
	j.workspaceIDs = kept
}

// run は控えた workspace を消し、控えた pane が残っていないことを確かめる。
//
// **失敗を握りつぶさない。**消し残しに気づけないほうが危険である。消し残すと、
// 本物の continuo の巡回が「同じ worktree に pane が2つ」の経路を踏む。
func (j *liveJanitor) run() {
	j.mu.Lock()
	workspaceIDs := append([]string(nil), j.workspaceIDs...)
	paneIDs := append([]string(nil), j.paneIDs...)
	j.mu.Unlock()

	if len(workspaceIDs) == 0 && len(paneIDs) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), liveCleanupTimeout)
	defer cancel()

	// 段1: 控えた workspace を worktree ごと消す。
	for _, id := range workspaceIDs {
		params := herdr.WorktreeRemoveParams{WorkspaceID: id, Force: true}
		if _, err := j.client.WorktreeRemove(ctx, params); err != nil {
			j.t.Errorf("後始末に失敗しました。herdr に workspace %s が残っています（手で消してください）: %v", id, err)
		}
	}

	// 段2: worktree.remove では閉じない workspace を掃く（cwd のリポジトリの workspace）。
	strays, err := j.strayWorkspaces(ctx)
	if err != nil {
		j.t.Errorf("後始末に失敗しました。workspace.list を呼べません: %v", err)
	}
	for _, id := range strays {
		if _, err := j.client.WorkspaceClose(ctx, herdr.WorkspaceCloseParams{WorkspaceID: id}); err != nil {
			j.t.Errorf("後始末に失敗しました。herdr に workspace %s が残っています（手で閉じてください）: %v", id, err)
		}
	}

	// 段3: 消えたことを herdr に聞き直す。**成功の応答だけを根拠にしない。**
	if remaining, err := j.strayWorkspaces(ctx); err != nil {
		j.t.Errorf("後始末の確認に失敗しました。workspace.list を呼べません: %v", err)
	} else if len(remaining) > 0 {
		j.t.Errorf("後始末が済んでいません。テストが作った workspace %v が残っています（手で閉じてください）", remaining)
	}

	if len(paneIDs) == 0 {
		return
	}
	list, err := j.client.PaneList(ctx, herdr.PaneListParams{})
	if err != nil {
		j.t.Errorf("後始末の確認に失敗しました。pane.list を呼べません: %v", err)
		return
	}
	alive := make(map[string]bool, len(list.Panes))
	for _, pane := range list.Panes {
		alive[pane.PaneID] = true
	}
	for _, id := range paneIDs {
		if alive[id] {
			j.t.Errorf("後始末が済んでいません。テストが作った pane %s が残っています（手で閉じてください）", id)
		}
	}
}

// resolvedPath は比較用にパスの symlink を解決する。解決できなければ元の値をそのまま返す。
//
// **macOS の /var は /private/var への symlink である。**t.TempDir() が返す値と
// herdr が返す値がここでずれるので、比べる前に両方を通す。
//
// path: 解決したいパス。
// 戻り値: 解決できた絶対パス。解決できなければ path そのまま。
func resolvedPath(path string) string {
	if path == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	return path
}
