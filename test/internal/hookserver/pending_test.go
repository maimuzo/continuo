package hookserver_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/hookserver"
)

// 目的: 逃がし先の hook が受信時刻（ファイル名）の昇順に読まれ、読んだファイルが消えること、
// そして socket で先に届いていた hook より先に配送されること（キューの先頭へ積むこと）を
// 確認する（設計 3-4 の段5e）。
// 与える情報: 受信時刻が古い順に3件並んだ逃がし先のファイルと、Start のあとに socket で
// 届いた hook 1件。
// 成功条件: 逃がし先の3件が時刻の昇順で先に配送され、そのあとに socket の1件が配送されること。
// 読み終えた逃がし先のファイルが1つも残っていないこと。
func TestReplayPending_受信時刻順に読んでキューの先頭へ積む(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	dir := ts.pendingDir(t, "maimuzo-koetsumugi-188")

	// わざと時刻の昇順とは違う順で置く。読む側が名前で並べ替えることを確かめる。
	writePendingFile(t, dir, "1787057953362308-Stop.json", stopEventJSON("pending-3", "[]"))
	writePendingFile(t, dir, "1787057953362306-Stop.json", stopEventJSON("pending-1", "[]"))
	writePendingFile(t, dir, "1787057953362307-SubagentStop.json", stopEventJSON("pending-2", "[]"))

	ts.start(t)
	ts.send(t, stopEventJSON("from-socket", "[]"))
	// socket で届いた1件がキューへ入るのを待ってから読み戻す。
	waitForQueueLen(t, ts.server, 1)

	if err := ts.server.ReplayPending(); err != nil {
		t.Fatalf("逃がし先の読み戻しに失敗しました: %v", err)
	}
	ts.server.StartDelivery()

	got := sink.waitFor(t, 4)
	want := []string{"pending-1", "pending-2", "pending-3", "from-socket"}
	for i, w := range want {
		if got[i].SessionID != w {
			t.Fatalf("%d 件目の配送が %q ではなく %q でした（配送順: %v）", i+1, w, got[i].SessionID, sessionIDs(got))
		}
	}

	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("逃がし先を読めません: %v", err)
	}
	for _, e := range left {
		t.Fatalf("読み終えた逃がし先のファイルが残っています: %s", e.Name())
	}
}

// sessionIDs は配送されたイベントの session_id を並べて返す（失敗したときの表示用）。
//
// evs: 配送されたイベント。
// 戻り値: session_id を配送順に並べたもの。
func sessionIDs(evs []hookserver.HookEvent) []string {
	out := make([]string, 0, len(evs))
	for _, ev := range evs {
		out = append(out, ev.SessionID)
	}
	return out
}

// 目的: 走査するのは *.json だけであり、書き込み中を表す .json.tmp は読まないこと、
// そして取り残された .tmp は起動時に消され、消したことがログに残ることを確認する
// （設計 3-19。読んでしまうと Stop を1件失う）。
// 与える情報: 正しい .json 1件と、中身が途中で切れた .json.tmp 1件。
// 成功条件: 配送されるのは .json の1件だけで、.tmp のファイルが消えており、
// 消したことが警告としてログに残っていること。
func TestReplayPending_tmpは読まずに起動時に消す(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	dir := ts.pendingDir(t, "maimuzo-koetsumugi-188")

	writePendingFile(t, dir, "1787057953362306-Stop.json", stopEventJSON("from-json", "[]"))
	tmpPath := writePendingFile(t, dir, "1787057953362307-Stop.json.tmp", `{"hook_event_name":"Stop","sess`)

	ts.start(t)
	if err := ts.server.ReplayPending(); err != nil {
		t.Fatalf("逃がし先の読み戻しに失敗しました: %v", err)
	}
	ts.server.StartDelivery()

	got := sink.waitFor(t, 1)
	if got[0].SessionID != "from-json" {
		t.Fatalf("配送されたのが .json の1件ではありません: %+v", got[0])
	}
	// .tmp まで読んでいれば2件目がキューに残っている。時間で様子を見ずに件数で判定する。
	if n := ts.server.QueueLen(); n != 0 {
		t.Fatalf(".tmp まで読み戻されました（配送待ちが %d 件残っています）", n)
	}
	if n := len(sink.Events()); n != 1 {
		t.Fatalf(".tmp まで配送されました（配送件数 %d）", n)
	}

	if _, err := os.Lstat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("取り残された .tmp が消えていません: %s (err=%v)", tmpPath, err)
	}
	logs := ts.logs.String()
	if !strings.Contains(logs, "書きかけ") || !strings.Contains(logs, "level=WARN") {
		t.Fatalf(".tmp を消したことがログに残っていません: %s", logs)
	}
}

// 目的: 壊れた JSON（rename 済みの .json）が消されず pending/broken/ へ移り、ログに残ること、
// そして残りの hook の配送が続くことを確認する（設計 3-19）。
// 与える情報: 壊れた .json 1件と、正しい .json 1件。
// 成功条件: 正しい1件だけが配送され、壊れたファイルが pending/broken/ に同じ名前で存在し、
// 元の場所から消えていること。
func TestReplayPending_壊れたJSONはbrokenへ移す(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	dir := ts.pendingDir(t, "maimuzo-koetsumugi-188")

	brokenName := "1787057953362306-Stop.json"
	brokenPath := writePendingFile(t, dir, brokenName, `{"hook_event_name":"Stop"`)
	writePendingFile(t, dir, "1787057953362307-Stop.json", stopEventJSON("ok", "[]"))

	ts.start(t)
	if err := ts.server.ReplayPending(); err != nil {
		t.Fatalf("逃がし先の読み戻しに失敗しました: %v", err)
	}
	ts.server.StartDelivery()

	got := sink.waitFor(t, 1)
	if got[0].SessionID != "ok" {
		t.Fatalf("配送されたのが正しい1件ではありません: %+v", got[0])
	}
	if _, err := os.Lstat(brokenPath); !os.IsNotExist(err) {
		t.Fatalf("壊れた JSON が元の場所に残っています: %s (err=%v)", brokenPath, err)
	}
	moved := filepath.Join(dir, "broken", brokenName)
	if _, err := os.Lstat(moved); err != nil {
		t.Fatalf("壊れた JSON が broken/ へ移っていません: %s (err=%v)", moved, err)
	}
	if !strings.Contains(ts.logs.String(), "隔離しました") {
		t.Fatalf("壊れた JSON を隔離したことがログに残っていません: %s", ts.logs.String())
	}
}

// 目的: 走査するのが <実行時ディレクトリ>/issues/*/pending/ の全部であることを確認する
// （1つの issue の分だけを読んで終わらない）。
// 与える情報: 別々の issue のスラグを持つ2つの逃がし先に、それぞれ1件ずつ置いた hook。
// 成功条件: 2件とも読み戻され、受信時刻の昇順に配送されること。
func TestReplayPending_複数のissueの逃がし先を全部走査する(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)

	dirA := ts.pendingDir(t, "maimuzo-koetsumugi-188")
	dirB := ts.pendingDir(t, "maimuzo-continuo-7")
	writePendingFile(t, dirA, "1787057953362307-Stop.json", stopEventJSON("issue-a", "[]"))
	writePendingFile(t, dirB, "1787057953362306-Stop.json", stopEventJSON("issue-b", "[]"))

	ts.start(t)
	if err := ts.server.ReplayPending(); err != nil {
		t.Fatalf("逃がし先の読み戻しに失敗しました: %v", err)
	}
	ts.server.StartDelivery()

	got := sink.waitFor(t, 2)
	if got[0].SessionID != "issue-b" || got[1].SessionID != "issue-a" {
		t.Fatalf("複数 issue の逃がし先が受信時刻順に読まれていません: %q, %q", got[0].SessionID, got[1].SessionID)
	}
}

// 目的: 逃がし先を2回走査すること、つまり1回目の走査を始めてから配送を始めるまでの間に
// continuo hook が書いたファイルも、同じ ReplayPending で拾えることを確認する（設計 3-4 の段5e）。
// 与える情報: 1回目の走査を必ず途中で止められるよう、逃がし先の1件目を名前付きパイプ
// （FIFO）にしておく。読む側はこれを開いた時点で書き手が現れるまで待つので、その間に
// 2件目のファイルを普通のファイルとして置ける。
// 成功条件: FIFO から読んだ1件目と、1回目の走査の最中に置いた2件目の、両方が配送されること。
func TestReplayPending_2回走査して途中で届いた分も拾う(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	dir := ts.pendingDir(t, "maimuzo-koetsumugi-188")

	fifoPath := filepath.Join(dir, "1787057953362306-Stop.json")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Skipf("名前付きパイプを作れない環境のため、この検査は行いません: %v", err)
	}

	ts.start(t)

	replayErr := make(chan error, 1)
	go func() { replayErr <- ts.server.ReplayPending() }()

	// 書き手として開く。1回目の走査が FIFO を読み始めるまで、ここで待たされる。
	// つまりこの行を抜けた時点で、1回目の走査は確実に始まっている。
	w, err := os.OpenFile(fifoPath, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("名前付きパイプを書き手として開けません: %v", err)
	}

	// 1回目の走査が止まっている間に、2件目を普通のファイルとして置く。
	writePendingFile(t, dir, "1787057953362307-Stop.json", stopEventJSON("second", "[]"))

	if _, err := w.WriteString(stopEventJSON("first", "[]")); err != nil {
		t.Fatalf("名前付きパイプへ書き込めません: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("名前付きパイプを閉じられません: %v", err)
	}

	select {
	case err := <-replayErr:
		if err != nil {
			t.Fatalf("逃がし先の読み戻しに失敗しました: %v", err)
		}
	case <-time.After(deliverWaitTimeout):
		t.Fatalf("ReplayPending が返りませんでした")
	}

	ts.server.StartDelivery()

	got := sink.waitFor(t, 2)
	if got[0].SessionID != "first" || got[1].SessionID != "second" {
		t.Fatalf("2回の走査で2件とも拾えていません: %v", sessionIDs(got))
	}
}

// 目的: 逃がし先のディレクトリがまだ1つも無い状態（初回起動）でも、ReplayPending が
// エラーにならないことを確認する。
// 与える情報: issues ディレクトリを作っていない hookserver。
// 成功条件: ReplayPending がエラーを返さず、配送も始められること。
func TestReplayPending_逃がし先がまだ無くてもエラーにしない(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	ts.start(t)

	if err := ts.server.ReplayPending(); err != nil {
		t.Fatalf("逃がし先が無いだけでエラーになりました: %v", err)
	}
	ts.server.StartDelivery()

	ts.send(t, stopEventJSON("session-1", "[]"))
	if got := sink.waitFor(t, 1); got[0].SessionID != "session-1" {
		t.Fatalf("配送された hook が想定と違います: %+v", got[0])
	}
}
