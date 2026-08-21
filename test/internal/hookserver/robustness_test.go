package hookserver_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/hookserver"
)

// longReadTimeout は「読み取りの期限まで待たされていないこと」を確かめるために使う
// 十分に長い期限である。この長さで待たされる実装なら、下の検査は必ず時間切れになる。
const longReadTimeout = 30 * time.Second

// mustBeFast は「読み取りの期限まで待たされていないこと」を判定する上限である。
// 順番待ちの上限（hookserver.DefaultOrderWait = 200ms）より十分長く、
// longReadTimeout より十分短い。
const mustBeFast = 3 * time.Second

// 目的: 1バイトも書かずに固まった接続が先にあっても、後続の hook がその接続の読み取りの
// 期限まで待たされないことを確認する。待たされると、待ち時間が settle_ms（既定2000ms。
// 設計 3-2）を超えて、正常に終わった turn が stall と誤判定される。
// 与える情報: 読み取りの期限を 30 秒にした hookserver、繋いだだけで何も書かない接続1本、
// そのあとに送る Stop 1件。
// 成功条件: Stop が mustBeFast（3秒）以内に配送されること。
func TestServer_宙ぶらりんの接続は後続hookの配送を止めない(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServerWith(t, sink, func(o *hookserver.Options) {
		o.ReadTimeout = longReadTimeout
	})
	ts.start(t)
	ts.server.StartDelivery()

	// 先に受け付けられるが、1バイトも書かない接続。
	ts.dialAndHold(t)
	ts.send(t, stopEventJSON("after-stuck", "[]"))

	start := time.Now()
	if got := sink.waitFor(t, 1); got[0].SessionID != "after-stuck" {
		t.Fatalf("配送された hook が想定と違います: %+v", got[0])
	}
	if elapsed := time.Since(start); elapsed > mustBeFast {
		t.Fatalf("宙ぶらりんの接続の読み取りの期限まで待たされました（%s かかりました）", elapsed)
	}
	if !strings.Contains(ts.logs.String(), "順番を揃えるのを諦めました") {
		t.Fatalf("順番を諦めたことがログに残っていません: %s", ts.logs.String())
	}
}

// 目的: Close が accept 済みの接続を閉じ、読み取りの期限まで待たされずに返ることを確認する。
// 待たされると continuo の終了・再起動がその都度止まる。
// 与える情報: 読み取りの期限を 30 秒にした hookserver と、繋いだだけで何も書かない接続1本。
// 成功条件: Close が mustBeFast（3秒）以内に返ること。
func TestServer_Closeは宙ぶらりんの接続を待たない(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServerWith(t, sink, func(o *hookserver.Options) {
		o.ReadTimeout = longReadTimeout
	})
	ts.start(t)
	ts.server.StartDelivery()

	ts.dialAndHold(t)
	// 接続が accept され、読み取りに入るのを待つ。ここで待たないと、Close が
	// 「まだ accept されていない接続」を閉じるだけになり、検査にならない。
	ts.send(t, stopEventJSON("warm-up", "[]"))
	ts.sink.waitFor(t, 1)

	start := time.Now()
	if err := ts.server.Close(); err != nil {
		t.Fatalf("Close に失敗しました: %v", err)
	}
	if elapsed := time.Since(start); elapsed > mustBeFast {
		t.Fatalf("Close が宙ぶらりんの接続の読み取りの期限まで待たされました（%s かかりました）", elapsed)
	}
}

// 目的: 配送を始めたあとに ReplayPending を呼んだら拒否されることを確認する。
// 逃がし先の古い hook をキューの先頭へ割り込ませると、socket で先に届いた新しい hook より
// 後に配送されて順序が壊れる（設計 3-4 は段5e を段6b より前と決めている）。
// 与える情報: StartDelivery を呼んだあとの hookserver と、そのあとに逃がし先へ置いた hook 1件。
// 成功条件: ReplayPending がエラーを返し、逃がし先のファイルが消えずに残っていること。
func TestReplayPending_配送を始めたあとは拒否する(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	dir := ts.pendingDir(t, "octocat-hello-world-188")

	ts.start(t)
	ts.server.StartDelivery()

	// StartDelivery が段5e の2回目の走査を済ませたあとに置く。ここから先の分を
	// 拾うのは次の起動の役目であり、いま割り込ませてはならない。
	name := "1787057953362306-Stop.json"
	writePendingFile(t, dir, name, stopEventJSON("too-late", "[]"))

	if err := ts.server.ReplayPending(); err == nil {
		t.Fatalf("配送を始めたあとの ReplayPending が拒否されませんでした")
	}
	if names := pendingEntryNames(t, dir); len(names) != 1 || names[0] != name {
		t.Fatalf("拒否したのに逃がし先のファイルが動いています: %v", names)
	}
}

// 目的: 隔離先（pending/broken/）に同じ名前のファイルが既にあっても、上書きせずに
// 両方を残すことを確認する。設計 3-19 は壊れた JSON を「消さずに残す」と決めているので、
// 上書きは設計違反である。
// 与える情報: 隔離先に置いてある同名の古いファイルと、逃がし先に置いた壊れた JSON。
// 成功条件: 古いファイルの中身がそのまま残り、新しく隔離されたものが別の名前で増えること。
func TestReplayPending_隔離先の同名ファイルを上書きしない(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	dir := ts.pendingDir(t, "octocat-hello-world-188")

	brokenDir := filepath.Join(dir, "broken")
	if err := os.MkdirAll(brokenDir, 0o700); err != nil {
		t.Fatalf("隔離先を作れません: %v", err)
	}
	name := "1787057953362306-Stop.json"
	older := `{"先に隔離してあった中身`
	writePendingFile(t, brokenDir, name, older)

	newer := `{"あとから隔離される中身`
	writePendingFile(t, dir, name, newer)

	ts.start(t)
	if err := ts.server.ReplayPending(); err != nil {
		t.Fatalf("逃がし先の読み戻しに失敗しました: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(brokenDir, name))
	if err != nil {
		t.Fatalf("先に隔離してあったファイルを読めません: %v", err)
	}
	if string(body) != older {
		t.Fatalf("先に隔離してあったファイルが上書きされました: %q", string(body))
	}

	moved := filepath.Join(brokenDir, "1787057953362306-Stop-1.json")
	body, err = os.ReadFile(moved)
	if err != nil {
		t.Fatalf("あとから隔離されたファイルが連番の名前で見つかりません: %s: %v", moved, err)
	}
	if string(body) != newer {
		t.Fatalf("あとから隔離されたファイルの中身が違います: %q", string(body))
	}
	if names := pendingEntryNames(t, dir); len(names) != 1 || names[0] != "broken" {
		t.Fatalf("逃がし先に壊れた JSON が残っています: %v", names)
	}
}

// 目的: 2回目の走査が StartDelivery の中で行われること、つまり ReplayPending が返ってから
// StartDelivery が呼ばれるまでの窓（段6 の索引作り）に書かれた hook も拾えることを確認する
// （設計 3-4 の段5e は「1回目の走査を始めてから配送を始めるまでの間に書かれたもの」を
// 拾うと決めている）。
// 与える情報: ReplayPending が返ったあと、StartDelivery を呼ぶ前に逃がし先へ置いた hook 1件。
// 成功条件: その1件が配送され、逃がし先から消えていること。
func TestStartDelivery_ReplayPendingが返ったあとに書かれた分も拾う(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	dir := ts.pendingDir(t, "octocat-hello-world-188")
	writePendingFile(t, dir, "1787057953362306-Stop.json", stopEventJSON("before-replay", "[]"))

	ts.start(t)
	if err := ts.server.ReplayPending(); err != nil {
		t.Fatalf("逃がし先の読み戻しに失敗しました: %v", err)
	}

	// ここが段6（索引作り）にあたる窓である。continuo hook はまだ逃がし先へ書きうる。
	writePendingFile(t, dir, "1787057953362307-Stop.json", stopEventJSON("during-index", "[]"))

	ts.server.StartDelivery()

	got := sink.waitFor(t, 2)
	if got[0].SessionID != "before-replay" || got[1].SessionID != "during-index" {
		t.Fatalf("段6 の窓に書かれた分を拾えていません: %v", sessionIDs(got))
	}
	if names := pendingEntryNames(t, dir); len(names) != 0 {
		t.Fatalf("読み終えた逃がし先のファイルが残っています: %v", names)
	}
}
