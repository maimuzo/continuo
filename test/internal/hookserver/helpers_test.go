// Package hookserver_test は internal/hookserver（hook の受け口）を検証する。
//
// Claude Code は起動しない。hook の JSON を自分で組み立て、Unix domain socket へ
// 書き込むことで「hook が届いた」状況を作る（docs/plans/impl/04_hook.md の受け入れの基準）。
package hookserver_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/hookserver"
	"github.com/maimuzo/continuo/internal/logging"
)

// deliverWaitTimeout は「配送されるはずのもの」を待つ上限である。
// 実 socket の読み書きを挟むので 0 にはできない。届かないときにテストを
// 固まらせないための上限であり、正常系ではこれより遥かに短い時間で届く。
const deliverWaitTimeout = 5 * time.Second

// queuePollInterval は QueueLen を見に行く間隔である。
// **「まだ配送されていないこと」を時間で確かめない。**一定時間眠って様子を見る書き方は、
// 負荷の高い環境で偽陰性（溜まっているのに配送されていないと判定する）になる。
// 代わりに「キューへ積まれた」という起きるはずの事象を待ち、その時点の配送件数を見る。
const queuePollInterval = 2 * time.Millisecond

// syncBuffer は複数の goroutine から書かれるログの出力先である。
// bytes.Buffer は並行して書けないため、ロックで包む。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write は io.Writer を満たす。
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String はこれまでに書かれたログの全文を返す。
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// recordingSink は orchestrator の代わりに hook を受け取る HookSink である。
// 受け取った順にすべて記録し、テストが待てるようにチャネルへも流す。
type recordingSink struct {
	mu       sync.Mutex
	events   []hookserver.HookEvent
	unknown  map[string]bool // ここに入れた session_id は「知らない」として false を返す
	received chan hookserver.HookEvent
}

// newRecordingSink は recordingSink を作る。
//
// unknownSessionIDs: 「知らない session_id」として false を返す値の一覧。
// 戻り値: 作った recordingSink。
func newRecordingSink(unknownSessionIDs ...string) *recordingSink {
	s := &recordingSink{
		unknown:  map[string]bool{},
		received: make(chan hookserver.HookEvent, 128),
	}
	for _, id := range unknownSessionIDs {
		s.unknown[id] = true
	}
	return s
}

// OnHook は hookserver.HookSink を満たす。
func (s *recordingSink) OnHook(ev hookserver.HookEvent) bool {
	s.mu.Lock()
	s.events = append(s.events, ev)
	s.mu.Unlock()
	s.received <- ev
	return !s.unknown[ev.SessionID]
}

// Events はこれまでに受け取ったイベントを受け取った順に返す。
func (s *recordingSink) Events() []hookserver.HookEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]hookserver.HookEvent, len(s.events))
	copy(out, s.events)
	return out
}

// waitFor は n 件が配送されるまで待つ。
//
// t: 呼び出し元のテスト。
// n: 待つ件数。
// 戻り値: 配送された n 件（受け取った順）。deliverWaitTimeout を超えたら t.Fatalf で落とす。
func (s *recordingSink) waitFor(t *testing.T, n int) []hookserver.HookEvent {
	t.Helper()
	got := make([]hookserver.HookEvent, 0, n)
	deadline := time.After(deliverWaitTimeout)
	for len(got) < n {
		select {
		case ev := <-s.received:
			got = append(got, ev)
		case <-deadline:
			t.Fatalf("hook が %d 件配送されるはずが %d 件しか届きませんでした: %+v", n, len(got), got)
		}
	}
	return got
}

// waitForQueueLen は配送待ちのキューが n 件に達するまで待つ。
//
// 「まだ配送していない」ことを確かめるための同期点である。socket へ書いた hook が
// キューへ積まれた時点を捉えられるので、そこで配送件数が 0 なら「溜めるだけ」が成立する。
//
// t: 呼び出し元のテスト。
// srv: 見る hookserver。
// n: 待つ件数。deliverWaitTimeout を超えたら t.Fatalf で落とす。
func waitForQueueLen(t *testing.T, srv *hookserver.Server, n int) {
	t.Helper()
	deadline := time.Now().Add(deliverWaitTimeout)
	for time.Now().Before(deadline) {
		if srv.QueueLen() >= n {
			return
		}
		time.Sleep(queuePollInterval)
	}
	t.Fatalf("配送待ちのキューが %d 件になりませんでした（いまは %d 件）", n, srv.QueueLen())
}

// testServer はテスト用に組み立てた hookserver 一式である。
type testServer struct {
	server     *hookserver.Server
	sink       *recordingSink
	logs       *syncBuffer
	runtimeDir string
	socketPath string
}

// newTestServer は短いパスの一時ディレクトリに hookserver を1台組み立てる。
//
// t.TempDir() を使わないのは、テスト名を含む長いパスになり、macOS の Unix domain
// socket のパス長上限（103バイト。internal/socketpath の実測）に触れかねないためである。
//
// t: 呼び出し元のテスト。後始末を t.Cleanup に登録する。
// sink: hook の届け先。
// 戻り値: 組み立てた testServer（まだ Start は呼んでいない）。
func newTestServer(t *testing.T, sink *recordingSink) *testServer {
	t.Helper()

	dir, err := os.MkdirTemp("", "hooksrv")
	if err != nil {
		t.Fatalf("一時ディレクトリを作成できません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	socketPath := filepath.Join(dir, "hooks.sock")
	logs := &syncBuffer{}
	srv, err := hookserver.New(hookserver.Options{
		SocketPath: socketPath,
		Sink:       sink,
		Logger:     logging.New(logs, slog.LevelDebug),
	})
	if err != nil {
		t.Fatalf("hookserver を組み立てられません: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	return &testServer{server: srv, sink: sink, logs: logs, runtimeDir: dir, socketPath: socketPath}
}

// newTestServerWith は Options を差し替えて hookserver を1台組み立てる。
//
// t: 呼び出し元のテスト。後始末を t.Cleanup に登録する。
// sink: hook の届け先。
// tune: SocketPath / Sink / Logger を埋めた Options を受け取り、必要なところだけ書き換える関数。
// 戻り値: 組み立てた testServer（まだ Start は呼んでいない）。
func newTestServerWith(t *testing.T, sink *recordingSink, tune func(*hookserver.Options)) *testServer {
	t.Helper()

	dir, err := os.MkdirTemp("", "hooksrv")
	if err != nil {
		t.Fatalf("一時ディレクトリを作成できません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	socketPath := filepath.Join(dir, "hooks.sock")
	logs := &syncBuffer{}
	opts := hookserver.Options{
		SocketPath: socketPath,
		Sink:       sink,
		Logger:     logging.New(logs, slog.LevelDebug),
	}
	tune(&opts)

	srv, err := hookserver.New(opts)
	if err != nil {
		t.Fatalf("hookserver を組み立てられません: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	return &testServer{server: srv, sink: sink, logs: logs, runtimeDir: dir, socketPath: socketPath}
}

// dialAndHold は socket へ繋いだだけで1バイトも書かない接続を作る（壊れた hook の再現）。
//
// t: 呼び出し元のテスト。後始末（接続を閉じる）を t.Cleanup に登録する。
func (ts *testServer) dialAndHold(t *testing.T) {
	t.Helper()
	conn, err := net.DialTimeout("unix", ts.socketPath, deliverWaitTimeout)
	if err != nil {
		t.Fatalf("hook の socket へ接続できません: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
}

// start は listen を始める（復元の段5d に相当）。
func (ts *testServer) start(t *testing.T) {
	t.Helper()
	if err := ts.server.Start(); err != nil {
		t.Fatalf("listen を始められません: %v", err)
	}
}

// send は socket へ改行区切り JSON を1行書いて閉じる（continuo hook と同じ振る舞い）。
//
// t: 呼び出し元のテスト。
// line: 送る JSON（末尾の改行はこの関数が付ける）。
func (ts *testServer) send(t *testing.T, line string) {
	t.Helper()
	conn, err := net.DialTimeout("unix", ts.socketPath, deliverWaitTimeout)
	if err != nil {
		t.Fatalf("hook の socket へ接続できません: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte(line + "\n")); err != nil {
		t.Fatalf("hook の socket へ書き込めません: %v", err)
	}
}

// pendingDir は issue のスラグに対応する逃がし先のパスを返し、作る。
//
// t: 呼び出し元のテスト。
// slug: issue のスラグ（例 maimuzo-koetsumugi-188）。
// 戻り値: <実行時ディレクトリ>/issues/<スラグ>/pending の絶対パス。
func (ts *testServer) pendingDir(t *testing.T, slug string) string {
	t.Helper()
	dir := filepath.Join(ts.runtimeDir, "issues", slug, "pending")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("逃がし先を作れません: %v", err)
	}
	return dir
}

// writePendingFile は逃がし先へファイルを1つ置く（continuo hook が rename し終えた状態を作る）。
//
// t: 呼び出し元のテスト。
// dir: 逃がし先のディレクトリ。
// name: ファイル名（例 1787057953362306-Stop.json）。
// content: 中身。
// 戻り値: 置いたファイルの絶対パス。
func writePendingFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("逃がし先のファイルを置けません: %v", err)
	}
	return path
}

// stopEventJSON は Stop hook の JSON を1件組み立てる。
//
// sessionID: session_id に入れる値。
// backgroundTasks: background_tasks に入れる JSON の断片。空文字なら項目そのものを入れない。
// 戻り値: 1行の JSON。
func stopEventJSON(sessionID, backgroundTasks string) string {
	if backgroundTasks == "" {
		return fmt.Sprintf(`{"hook_event_name":"Stop","session_id":%q,"stop_hook_active":false}`, sessionID)
	}
	return fmt.Sprintf(
		`{"hook_event_name":"Stop","session_id":%q,"background_tasks":%s,"stop_hook_active":false}`,
		sessionID, backgroundTasks,
	)
}

// pendingEntryNames は逃がし先に残っているファイル名を返す。
//
// t: 呼び出し元のテスト。
// dir: 逃がし先のディレクトリ。
// 戻り値: ファイル名の一覧（名前の昇順）。
func pendingEntryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("逃がし先を読めません: %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
