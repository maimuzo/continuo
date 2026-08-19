package hookserver_test

import (
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/hookserver"
)

// 目的: 1つの接続から受け付ける hook の件数に上限があり、上限に達したら**そこで読み取りを
// 打ち切る**こと、そして**そこまでに読めた分は必ず配送される**ことを確認する。
// 上限が無いと、同じ利用者のプロセスが1接続で数百万件を流し込んでメモリを使い切れる。
// 打ち切った分ごと捨てると、turn の終わりを知らせる Stop を落としかねない。
// 与える情報: 1接続あたり2件までに縮めた hookserver と、1つの接続に書いた4行。
// 成功条件: 先頭の2件が配送され、打ち切ったことが警告としてログに残ること。
func TestServer_1接続の件数の上限で読み取りを打ち切る(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServerWith(t, sink, func(o *hookserver.Options) {
		o.MaxEventsPerConn = 2
	})
	ts.start(t)
	ts.server.StartDelivery()

	ts.sendLines(t,
		stopEventJSON("line-1", "[]"),
		stopEventJSON("line-2", "[]"),
		stopEventJSON("line-3", "[]"),
		stopEventJSON("line-4", "[]"),
	)

	got := sink.waitFor(t, 2)
	if got[0].SessionID != "line-1" || got[1].SessionID != "line-2" {
		t.Fatalf("上限までの分が配送されていません: %v", sessionIDs(got))
	}
	waitForLogCount(t, ts, "上限で打ち切りました", 1)
	if n := len(sink.Events()); n != 2 {
		t.Fatalf("上限を超えて %d 件が配送されました", n)
	}
	if n := ts.server.QueueLen(); n != 0 {
		t.Fatalf("上限を超えた分がキューへ積まれています（%d 件）", n)
	}
}

// 目的: 1つの接続から読む累計バイト数に上限があり、上限に達したらそこで読み取りを
// 打ち切ることを確認する（件数の上限だけでは、巨大な行を並べられると防げない）。
// 与える情報: 1件の上限と1接続の累計の上限をどちらも 128 バイトに縮めた hookserver と、
// 1つの接続に書いた3行（1行はおよそ 90 バイト）。
// 成功条件: 2件だけが配送され、打ち切ったことが警告としてログに残ること。
func TestServer_1接続の累計バイト数の上限で読み取りを打ち切る(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServerWith(t, sink, func(o *hookserver.Options) {
		o.MaxMessageBytes = 128
		o.MaxConnBytes = 128
		o.MaxEventsPerConn = 100
	})
	ts.start(t)
	ts.server.StartDelivery()

	ts.sendLines(t,
		stopEventJSON("bytes-1", "[]"),
		stopEventJSON("bytes-2", "[]"),
		stopEventJSON("bytes-3", "[]"),
	)

	got := sink.waitFor(t, 2)
	if got[0].SessionID != "bytes-1" || got[1].SessionID != "bytes-2" {
		t.Fatalf("上限までの分が配送されていません: %v", sessionIDs(got))
	}
	waitForLogCount(t, ts, "累計バイト数の上限", 1)
	if n := len(sink.Events()); n != 2 {
		t.Fatalf("上限を超えて %d 件が配送されました", n)
	}
}

// 目的: 1接続の累計バイト数の上限が、1件の上限（MaxMessageBytes）を下回らないことを確認する。
// 下回ると、受け口が「通す」と決めた大きさちょうどの hook 1件が累計の上限に引っかかり、
// どこにも残らずに消える。
// 与える情報: 1件の上限を 4096 バイト、累計の上限をそれより小さい 16 バイトにした hookserver と、
// 1件の上限に収まる大きさの hook 1件。
// 成功条件: その1件が配送されること。
func TestServer_累計の上限は1件の上限を下回らない(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServerWith(t, sink, func(o *hookserver.Options) {
		o.MaxMessageBytes = 4096
		o.MaxConnBytes = 16
	})
	ts.start(t)
	ts.server.StartDelivery()

	ts.send(t, stopEventJSON("just-under-limit", "[]"))

	if got := sink.waitFor(t, 1); got[0].SessionID != "just-under-limit" {
		t.Fatalf("1件の上限に収まる hook が配送されていません: %+v", got[0])
	}
}

// 目的: 配送待ちのキューに上限があり、超えたら**古いほうから**落とすことを確認する。
// 上限が無いと、配送先が詰まっている間にキューがメモリを使い切る。
// 落とすほうを新しい側にすると、turn の終わりの判定に要る最新の Stop が消える。
// 与える情報: キューの上限を2件に縮め、まだ配送を始めていない hookserver と、送った4件。
// 成功条件: 配送されるのが新しいほうの2件で、落としたことが警告としてログに残ること。
func TestServer_配送待ちのキューは古いほうから落とす(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServerWith(t, sink, func(o *hookserver.Options) {
		o.MaxQueueEvents = 2
	})
	ts.start(t)

	for _, id := range []string{"old-1", "old-2", "new-3", "new-4"} {
		ts.send(t, stopEventJSON(id, "[]"))
	}
	// 3件目と4件目で1件ずつ落ちる。落ちたことを見てから配送を始める
	// （**時間で様子を見ない**。負荷の高い環境で偽陰性になる）。
	waitForLogCount(t, ts, "古いほうから落としました", 2)
	if n := ts.server.QueueLen(); n != 2 {
		t.Fatalf("配送待ちのキューが上限（2件）を超えています: %d 件", n)
	}

	ts.server.StartDelivery()
	got := sink.waitFor(t, 2)
	if got[0].SessionID != "new-3" || got[1].SessionID != "new-4" {
		t.Fatalf("新しいほうの2件が残っていません: %v", sessionIDs(got))
	}
}

// 目的: 順番を追い越された接続が、もう来ない番号を order_wait いっぱい待たないことを確認する。
// 待っても順番は揃わないので、その分だけ配送が遅れるだけである。遅れて届いたのが Stop なら、
// その遅れがそのまま turn の終わりの検知の遅れになる。
// 与える情報: order_wait を2秒に伸ばした hookserver、先に繋いで黙っている接続1本、
// そのあとに送る hook 1件（この1件が order_wait で諦めて追い越す）。
// 成功条件: 追い越された接続が書き終えてから配送されるまでが overtakenMustBeFast 以内であること。
func TestServer_追い越された接続は来ない番号を待たない(t *testing.T) {
	// order_wait はこの検査で「待ってしまったら必ず超える」長さにする。
	const orderWait = 2 * time.Second
	// overtakenMustBeFast は「待っていない」と判定する上限である。order_wait より十分短い。
	const overtakenMustBeFast = time.Second

	sink := newRecordingSink()
	ts := newTestServerWith(t, sink, func(o *hookserver.Options) {
		o.OrderWait = orderWait
	})
	ts.start(t)
	ts.server.StartDelivery()

	// 先に受け付けられるが、まだ1バイトも書かない接続。
	held := ts.dialAndKeep(t)
	ts.send(t, stopEventJSON("overtaker", "[]"))

	// 後続が order_wait で諦め、追い越して配送されるのを待つ。
	if got := sink.waitFor(t, 1); got[0].SessionID != "overtaker" {
		t.Fatalf("追い越した側が先に配送されていません: %+v", got[0])
	}
	if !strings.Contains(ts.logs.String(), "順番を揃えるのを諦めました") {
		t.Fatalf("順番を諦めたことがログに残っていません: %s", ts.logs.String())
	}

	start := time.Now()
	if _, err := held.Write([]byte(stopEventJSON("overtaken", "[]") + "\n")); err != nil {
		t.Fatalf("保持していた接続へ書き込めません: %v", err)
	}
	if err := held.Close(); err != nil {
		t.Fatalf("保持していた接続を閉じられません: %v", err)
	}

	got := sink.waitFor(t, 1)
	elapsed := time.Since(start)
	if got[0].SessionID != "overtaken" {
		t.Fatalf("追い越された側の hook が配送されていません: %+v", got[0])
	}
	if elapsed > overtakenMustBeFast {
		t.Fatalf("追い越された接続がもう来ない番号を待ちました（%s かかりました）", elapsed)
	}
}

// 目的: 同時に読み取る接続の数に上限があっても、上限を超えて繋いだ hook が**捨てられない**
// ことを確認する。上限に達したときに接続を閉じる作りにすると、書く側は「書けた」と思ったまま
// hook が消える。continuo は accept を止めて待たせるだけにしてある。
// 与える情報: 同時に読み取る接続を1本に絞った hookserver と、続けて送る3件。
// 成功条件: 3件とも送信順に配送されること。
func TestServer_同時接続の上限を超えてもhookを捨てない(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServerWith(t, sink, func(o *hookserver.Options) {
		o.MaxConcurrentConns = 1
	})
	ts.start(t)
	ts.server.StartDelivery()

	want := []string{"conn-1", "conn-2", "conn-3"}
	for _, id := range want {
		ts.send(t, stopEventJSON(id, "[]"))
	}

	got := sink.waitFor(t, len(want))
	for i, id := range want {
		if got[i].SessionID != id {
			t.Fatalf("%d 件目が %q ではなく %q でした（配送順: %v）", i+1, id, got[i].SessionID, sessionIDs(got))
		}
	}
}
