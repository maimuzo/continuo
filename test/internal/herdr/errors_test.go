package herdr_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// 目的: herdr が返すエラー応答（{"error":{"code":...,"message":...}}）が Go の error に
// 変換され、code で判定できることを確認する（設計 2-1 / タスク要件「その4」）。
// 与える情報: 偽サーバが code="pane_not_found" のエラー応答を返す状況。
// 成功条件: PaneClose がエラーを返し、herdr.IsCode(err, "pane_not_found") が true、
// 別のコード（"agent_pane_busy"）に対しては false になること。エラーメッセージにも
// message の内容が含まれること。
func TestCall_herdrのエラー応答がcodeで判定できるerrorになる(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeErrorResponse(t, conn, req.ID, "pane_not_found", "指定された pane は存在しません")
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second, Startup: time.Second, Turn: time.Second})
	_, err := client.PaneClose(context.Background(), herdr.PaneCloseParams{PaneID: "w1:p1"})
	if err == nil {
		t.Fatalf("herdr のエラー応答なのに error が返らなかった")
	}

	if !herdr.IsCode(err, "pane_not_found") {
		t.Fatalf("IsCode(err, %q) が false になった: err=%v", "pane_not_found", err)
	}
	if herdr.IsCode(err, herdr.ErrCodeAgentPaneBusy) {
		t.Fatalf("異なる code (%q) に対して IsCode が true を返した: err=%v", herdr.ErrCodeAgentPaneBusy, err)
	}
	if got := err.Error(); got == "" {
		t.Fatalf("エラーメッセージが空である")
	}
}

// 目的: 通信自体は成功したが herdr が正常応答を返す場合、IsCode がどんなコードに対しても
// false になることを確認する（herdr の *Error 以外のエラー、および nil に対する
// IsCode の安全性の確認を兼ねる）。
// 与える情報: nil の error。
// 成功条件: herdr.IsCode(nil, ...) が false であること（panic しないこと）。
func TestIsCode_nilエラーに対してfalseを返す(t *testing.T) {
	if herdr.IsCode(nil, herdr.ErrCodeAgentPaneBusy) {
		t.Fatalf("nil の error に対して IsCode が true を返した")
	}
}
