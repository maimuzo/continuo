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
// 与える情報: 正しい .json 1件と、中身が途中で切れた .json.tmp 1件。**.tmp の更新時刻は
// 1時間前にする**（いま書いている最中のものは消さない仕様なので、取り残された残骸にする）。
// 成功条件: 配送されるのは .json の1件だけで、.tmp のファイルが消えており、
// 消したことが警告としてログに残っていること。
func TestReplayPending_tmpは読まずに起動時に消す(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	dir := ts.pendingDir(t, "maimuzo-koetsumugi-188")

	writePendingFile(t, dir, "1787057953362306-Stop.json", stopEventJSON("from-json", "[]"))
	tmpPath := writePendingFile(t, dir, "1787057953362307-Stop.json.tmp", `{"hook_event_name":"Stop","sess`)
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(tmpPath, old, old); err != nil {
		t.Fatalf("書きかけのファイルの更新時刻を変えられません: %v", err)
	}

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

// 目的: 逃がし先に通常ファイルでないもの（名前付きパイプ）が置かれても、読まずに隔離し、
// ReplayPending が止まらないことを確認する。読みに行くと書き手が現れるまで開いたまま待つので、
// ReplayPending が無期限に返らず、復元が段5e で止まる（Close でも中断できない）。
// 逃がし先は同じ利用者が書ける場所なので、この形は実際に起こしうる。
// 与える情報: 逃がし先に置いた名前付きパイプ 1件（.json の名前）と、正しい .json 1件。
// 成功条件: ReplayPending が deliverWaitTimeout 以内に返り、正しい1件だけが配送され、
// 名前付きパイプが pending/broken/ へ移っていること。
func TestReplayPending_通常ファイルでないものは読まずに隔離する(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	dir := ts.pendingDir(t, "maimuzo-koetsumugi-188")

	fifoName := "1787057953362306-Stop.json"
	fifoPath := filepath.Join(dir, fifoName)
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Skipf("名前付きパイプを作れない環境のため、この検査は行いません: %v", err)
	}
	writePendingFile(t, dir, "1787057953362307-Stop.json", stopEventJSON("regular", "[]"))

	ts.start(t)

	replayErr := make(chan error, 1)
	go func() { replayErr <- ts.server.ReplayPending() }()

	select {
	case err := <-replayErr:
		if err != nil {
			t.Fatalf("逃がし先の読み戻しに失敗しました: %v", err)
		}
	case <-time.After(deliverWaitTimeout):
		t.Fatalf("名前付きパイプを読みに行って ReplayPending が返りませんでした")
	}

	ts.server.StartDelivery()

	got := sink.waitFor(t, 1)
	if got[0].SessionID != "regular" {
		t.Fatalf("配送されたのが通常ファイルの1件ではありません: %+v", got[0])
	}
	if _, err := os.Lstat(fifoPath); !os.IsNotExist(err) {
		t.Fatalf("名前付きパイプが逃がし先に残っています: %s (err=%v)", fifoPath, err)
	}
	moved := filepath.Join(dir, "broken", fifoName)
	if _, err := os.Lstat(moved); err != nil {
		t.Fatalf("名前付きパイプが broken/ へ移っていません: %s (err=%v)", moved, err)
	}
}

// 目的: 逃がし先のファイルの大きさに上限があり、超えたものは読まずに隔離することを確認する。
// 上限が無いと、逃がし先へ巨大なファイルを1つ置かれるだけで ReplayPending が丸ごとメモリへ
// 読み込む。落ちてもそのファイルは残るので、起動するたびに落ちる輪になる。
// 与える情報: 受け口の1件の上限（MaxMessageBytes）を 256 バイトに縮めた hookserver と、
// それを超える .json 1件、そして上限に収まる正しい .json 1件。
// 成功条件: 上限に収まる1件だけが配送され、超えたファイルが pending/broken/ へ移っていること。
func TestReplayPending_大きすぎるファイルは読まずに隔離する(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServerWith(t, sink, func(o *hookserver.Options) {
		o.MaxMessageBytes = 256
	})
	dir := ts.pendingDir(t, "maimuzo-koetsumugi-188")

	hugeName := "1787057953362306-Stop.json"
	huge := `{"hook_event_name":"Stop","session_id":"` + strings.Repeat("x", 1024) + `"}`
	writePendingFile(t, dir, hugeName, huge)
	writePendingFile(t, dir, "1787057953362307-Stop.json", stopEventJSON("small", "[]"))

	ts.start(t)
	if err := ts.server.ReplayPending(); err != nil {
		t.Fatalf("逃がし先の読み戻しに失敗しました: %v", err)
	}
	ts.server.StartDelivery()

	got := sink.waitFor(t, 1)
	if got[0].SessionID != "small" {
		t.Fatalf("配送されたのが上限に収まる1件ではありません: %+v", got[0])
	}
	if n := ts.server.QueueLen(); n != 0 {
		t.Fatalf("大きすぎるファイルまで読み戻されました（配送待ちが %d 件残っています）", n)
	}
	moved := filepath.Join(dir, "broken", hugeName)
	if _, err := os.Lstat(moved); err != nil {
		t.Fatalf("大きすぎるファイルが broken/ へ移っていません: %s (err=%v)", moved, err)
	}
	if !strings.Contains(ts.logs.String(), "上限") {
		t.Fatalf("大きさの上限で隔離したことがログに残っていません: %s", ts.logs.String())
	}
}

// 目的: 起動時の .json.tmp の掃除が、いま `continuo hook` が書いている最中のものを
// 消さないことを確認する。消すと書く側の os.Rename が ENOENT で失敗し、その hook は
// socket にも逃がし先にも残らずに消える（消えたのが Stop なら stall_timeout_ms、
// 既定30分まで誰も気づかない。設計 3-19）。
// 与える情報: 更新時刻がいまの .json.tmp 1件と、更新時刻を1時間前にした .json.tmp 1件。
// 成功条件: 新しいほうが残り、古いほうだけが消えていること。
func TestReplayPending_書き込み中のtmpは消さない(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	dir := ts.pendingDir(t, "maimuzo-koetsumugi-188")

	fresh := writePendingFile(t, dir, "1787057953362306-Stop.json.tmp", `{"hook_event_name":"Stop","sess`)
	stale := writePendingFile(t, dir, "1787057953362307-Stop.json.tmp", `{"hook_event_name":"Stop","sess`)
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("書きかけのファイルの更新時刻を変えられません: %v", err)
	}

	ts.start(t)
	if err := ts.server.ReplayPending(); err != nil {
		t.Fatalf("逃がし先の読み戻しに失敗しました: %v", err)
	}

	if _, err := os.Lstat(fresh); err != nil {
		t.Fatalf("いま書いている最中の .tmp が消されました: %s (err=%v)", fresh, err)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatalf("取り残された .tmp が消えていません: %s (err=%v)", stale, err)
	}
}

// 目的: 起動時の .json.tmp の掃除と `continuo hook` の書き込みがぶつかっても、
// 書く側の os.Rename が成功し、その hook が失われないことを確認する（設計 3-19）。
// 与える情報: 逃がし先へ .json.tmp を書き切って（OpenFile → Write → Sync）、まだ rename して
// いない状態。そこで Start → ReplayPending を通し、そのあとに rename を行う。
// 成功条件: rename が成功し、そのファイルが StartDelivery の2回目の走査で配送されること。
func TestReplayPending_書き込み中のhookをrenameできる(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	dir := ts.pendingDir(t, "maimuzo-koetsumugi-188")

	// internal/hookclient の spill と同じ順序で、rename の直前の状態を作る。
	finalPath := filepath.Join(dir, "1787057953362306-Stop.json")
	tmpPath := finalPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatalf("書きかけのファイルを作れません: %v", err)
	}
	if _, err := f.WriteString(stopEventJSON("mid-write", "[]")); err != nil {
		t.Fatalf("書きかけのファイルへ書けません: %v", err)
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("書きかけのファイルを同期できません: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("書きかけのファイルを閉じられません: %v", err)
	}

	ts.start(t)
	if err := ts.server.ReplayPending(); err != nil {
		t.Fatalf("逃がし先の読み戻しに失敗しました: %v", err)
	}

	// 起動時の掃除に消されていれば、ここが ENOENT で失敗する。
	if err := os.Rename(tmpPath, finalPath); err != nil {
		t.Fatalf("書いている最中の .tmp が消されたため rename に失敗しました（この hook は失われます）: %v", err)
	}

	ts.server.StartDelivery()

	got := sink.waitFor(t, 1)
	if got[0].SessionID != "mid-write" {
		t.Fatalf("書き込み中だった hook が配送されていません: %+v", got[0])
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
