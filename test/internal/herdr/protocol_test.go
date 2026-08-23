package herdr_test

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// pingResultJSON は 2026-08-18 に実機の herdr（0.8.0）から得た ping の応答の原文である
// （設計 2-1）。テストではこの原文をそのまま返させ、フィールドの読み取りを検証する。
const pingResultJSON = `{"type": "pong", "version": "0.8.0", "protocol": 19,
	"capabilities": {"live_handoff": true, "detached_server_daemon": false}}`

// 目的: protocol 版を取るメソッドが ping であり、引数を1つも送らないことを確認する
// （設計 2-1: 「protocol.version というメソッドは存在しない」「ping の引数は無い」）。
// 与える情報: 実測の ping 応答をそのまま返す偽サーバ。
// 成功条件: 送られた method が "ping" で、params が空オブジェクトであること。
func TestPing_引数なしでpingを呼ぶ(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResponse(t, conn, rpcResponse{ID: req.ID, Result: json.RawMessage(pingResultJSON)})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})
	if _, err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping が失敗した: %v", err)
	}

	lines := fs.Lines()
	if len(lines) != 1 {
		t.Fatalf("偽サーバが受け取った行数が想定と違う: got %d, want 1", len(lines))
	}
	var sent struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(lines[0], &sent); err != nil {
		t.Fatalf("送られたリクエストを解析できない: %v", err)
	}
	if sent.Method != herdr.MethodPing {
		t.Fatalf("method が想定と違う: got %q, want %q", sent.Method, herdr.MethodPing)
	}
	if len(sent.Params) != 0 {
		t.Fatalf("ping に引数を送ってしまっている: %v", sent.Params)
	}
}

// 目的: ping の応答から protocol 版だけでなく herdr のバージョンと機能一覧も読めることを
// 確認する（設計 2-1 の実測値。version は人間へのログに出す）。
// 与える情報: 実測の ping 応答の原文をそのまま返す偽サーバ。
// 成功条件: type が "pong"、version が "0.8.0"、protocol が 19、capabilities の
// live_handoff が真・detached_server_daemon が偽として読み取れること。
func TestPing_実測の応答からversionとcapabilitiesを読める(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResponse(t, conn, rpcResponse{ID: req.ID, Result: json.RawMessage(pingResultJSON)})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})
	result, err := client.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping が失敗した: %v", err)
	}

	if result.Type != "pong" {
		t.Fatalf("type が想定と違う: got %q, want %q", result.Type, "pong")
	}
	if result.Version != "0.8.0" {
		t.Fatalf("version が想定と違う: got %q, want %q", result.Version, "0.8.0")
	}
	if result.Protocol != 19 {
		t.Fatalf("protocol が想定と違う: got %d, want 19", result.Protocol)
	}
	if got := result.Capabilities["live_handoff"]; got != true {
		t.Fatalf("capabilities の live_handoff が想定と違う: got %v, want true", got)
	}
	if got := result.Capabilities["detached_server_daemon"]; got != false {
		t.Fatalf("capabilities の detached_server_daemon が想定と違う: got %v, want false", got)
	}
}

// 目的: ping が返す protocol 版が設定の期待値と一致する場合、CheckProtocol が
// エラーを返さないことを確認する（設計 5-2: herdr.protocol）。
// 与える情報: 偽サーバが実測の応答（protocol=19）を返す状況。expected は 19。
// 成功条件: CheckProtocol がエラーを返さず、herdr のバージョンも一緒に返すこと
// （人間へのログに出せるようにするため）。
func TestCheckProtocol_版が一致すれば成功する(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResponse(t, conn, rpcResponse{ID: req.ID, Result: json.RawMessage(pingResultJSON)})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second, Startup: time.Second, Turn: time.Second})
	result, err := client.CheckProtocol(context.Background(), 19)
	if err != nil {
		t.Fatalf("版が一致しているのにエラーになった: %v", err)
	}
	if result == nil {
		t.Fatalf("成功したのに ping の応答が返らなかった")
	}
	if result.Version != "0.8.0" {
		t.Fatalf("herdr のバージョンを読み取れていない: got %q", result.Version)
	}
}

// 目的: ping が返す protocol 版が設定の期待値と異なる場合、CheckProtocol が
// エラーを返し、起動を止められることを確認する（設計 5-2）。
// 与える情報: 偽サーバが protocol=18 の応答を返す状況。expected は 19。
// 成功条件: CheckProtocol がエラーを返し、そのメッセージに herdr 側の版（18）と
// 設定側の期待値（19）の両方が含まれること（運用者が原因を特定できるようにするため）。
func TestCheckProtocol_版が不一致ならエラーになる(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.PingResult{Type: "pong", Version: "0.8.0", Protocol: 18})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second, Startup: time.Second, Turn: time.Second})
	_, err := client.CheckProtocol(context.Background(), 19)
	if err == nil {
		t.Fatalf("版が不一致なのにエラーが返らなかった")
	}
	msg := err.Error()
	if !strings.Contains(msg, "18") || !strings.Contains(msg, "19") {
		t.Fatalf("エラーメッセージに両方の版が含まれていない: %s", msg)
	}
}

// 目的: herdr がエラー応答を返した場合、CheckProtocol が起動を止められるエラーを
// 返すことを確認する（設計 5-2 の照合は ping が成功することが前提である）。
// 与える情報: 偽サーバが code="invalid_request" のエラー応答を、**id を空文字にして**
// 返す状況（2-1 の実測どおりの形）。
// 成功条件: CheckProtocol がエラーを返し、herdr.IsCode で code を判定できること。
func TestCheckProtocol_herdrがエラーを返せば起動を止められる(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		writeErrorResponse(t, conn, "", "invalid_request", "unknown method")
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})
	result, err := client.CheckProtocol(context.Background(), 19)
	if err == nil {
		t.Fatalf("herdr がエラーを返したのにエラーが返らなかった")
	}
	if result != nil {
		t.Fatalf("ping に失敗したのに結果が返った: %+v", result)
	}
	if !herdr.IsCode(err, "invalid_request") {
		t.Fatalf("code で判定できるエラーになっていない: %v", err)
	}
}
