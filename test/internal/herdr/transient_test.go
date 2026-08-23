package herdr_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// 目的: 応答待ちの途中で ctx を cancel したら、待ち時間の上限を待たずに即座に打ち切られる
// ことを確認する（レビュー指摘「call が ctx を一切見ない」の回帰テスト）。
//
// **conn.SetDeadline だけでは cancel が効かない。**期限が来るまで読み取りが解けないため、
// 待機ありの agent.prompt（Turn は既定1時間）を投げたあとに SIGINT を受けても、
// 停止処理が最大1時間ブロックしていた。
//
// 与える情報: 応答を一切書かない偽サーバと、上限を 30 秒に設定したクライアント。
// 呼び出しの 100 ミリ秒後に ctx を cancel する。
// 成功条件: 2秒以内にエラーが返ること。そのエラーが herdr.ErrCodeCanceled であり、
// errors.Is(err, context.Canceled) で打ち切りだと判定できること。
// 一時的な失敗（IsTransient）としては扱われないこと。
func TestCall_ctxをcancelすると応答待ちが即座に打ち切られる(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		// 意図的に応答を書かない。接続だけ開けたままにする。
		time.Sleep(5 * time.Second)
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: 30 * time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	defer cancel()

	start := time.Now()
	_, err := client.AgentList(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("ctx を cancel したのにエラーが返らなかった")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("cancel が効いていない（%s かかった。上限は30秒に設定してある）", elapsed)
	}
	if !herdr.IsCode(err, herdr.ErrCodeCanceled) {
		t.Fatalf("打ち切りとして分類されていない: err=%v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled) が false になった: err=%v", err)
	}
	if herdr.IsTransient(err) {
		t.Fatalf("打ち切りを一時的な失敗として扱っている（リトライされてしまう）: err=%v", err)
	}
}

// 目的: herdr の socket へ接続できない失敗が、*Error（ErrCodeTransport）として返り、
// 一時的な失敗だと呼び出し側が判定できることを確認する
// （レビュー指摘「herdr のエラーに一時的／恒久的の区別が無い」の回帰テスト）。
//
// **区別が無いと、herdr が一瞬落ちて再起動しただけで run が捨てられる。**
// 呼び出し側は「待ち受けが返ったのに Stop が来ませんでした」という事実と違う理由で
// abandonRun していた。
//
// 与える情報: 存在しないパスの socket を指すクライアント。
// 成功条件: エラーが herdr.ErrCodeTransport であり、herdr.IsTransient が true を返すこと。
func TestCall_socketに接続できない失敗は一時的な失敗として返る(t *testing.T) {
	client := herdr.New(filepath.Join(t.TempDir(), "no-such.sock"), herdr.Timeouts{Read: time.Second})

	_, err := client.AgentList(context.Background())
	if err == nil {
		t.Fatalf("接続できないのにエラーが返らなかった")
	}
	if !herdr.IsCode(err, herdr.ErrCodeTransport) {
		t.Fatalf("接続失敗が %q として分類されていない: err=%v", herdr.ErrCodeTransport, err)
	}
	if !herdr.IsTransient(err) {
		t.Fatalf("接続失敗が一時的な失敗として扱われていない（run が捨てられる）: err=%v", err)
	}
}

// 目的: continuo 側の待ち時間が尽きた場合が、herdr が返す ErrCodeTimeout とは別のコード
// （ErrCodeReadTimeout）になり、かつ一時的な失敗として扱われることを確認する。
//
// **2つを同じコードにすると、herdr へ届いてすらいない呼び出しを「turn が時間切れした」と
// 誤認する。**呼び出し側は ErrCodeTimeout を受けると枠待ちの判定へ進む。
//
// 与える情報: 応答を書かない偽サーバと、上限 50 ミリ秒のクライアント。
// 成功条件: エラーが ErrCodeReadTimeout であり、ErrCodeTimeout ではないこと。
// herdr.IsTransient が true を返すこと。
func TestCall_継続側の待ち時間切れはherdrのtimeoutと区別される(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		time.Sleep(2 * time.Second)
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: 50 * time.Millisecond})

	_, err := client.AgentList(context.Background())
	if err == nil {
		t.Fatalf("応答が来ないのにエラーが返らなかった")
	}
	if !herdr.IsCode(err, herdr.ErrCodeReadTimeout) {
		t.Fatalf("待ち時間切れが %q として分類されていない: err=%v", herdr.ErrCodeReadTimeout, err)
	}
	if herdr.IsCode(err, herdr.ErrCodeTimeout) {
		t.Fatalf("continuo 側の待ち時間切れを herdr の timeout と同じコードで返している: err=%v", err)
	}
	if !herdr.IsTransient(err) {
		t.Fatalf("待ち時間切れが一時的な失敗として扱われていない: err=%v", err)
	}
}

// 目的: herdr が返すエラー応答は「一時的な失敗」ではないことを確認する。
//
// **一時的な失敗にすると、存在しない pane への操作が延々とリトライされる。**
// IsTransient が真になってよいのは continuo 側が付けるコード
// （transport / read_timeout）だけである。
//
// 与える情報: code="pane_not_found" のエラー応答を返す偽サーバ。
// 成功条件: herdr.IsTransient が false を返すこと。nil に対しても false を返すこと。
func TestIsTransient_herdrのエラー応答は一時的な失敗として扱わない(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeErrorResponse(t, conn, req.ID, "pane_not_found", "指定された pane は存在しません")
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})
	_, err := client.PaneClose(context.Background(), herdr.PaneCloseParams{PaneID: "w1:p1"})
	if err == nil {
		t.Fatalf("herdr のエラー応答なのに error が返らなかった")
	}
	if herdr.IsTransient(err) {
		t.Fatalf("herdr のエラー応答を一時的な失敗として扱っている: err=%v", err)
	}
	if herdr.IsTransient(nil) {
		t.Fatalf("nil に対して IsTransient が true を返した")
	}
}
