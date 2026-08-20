// Package herdr_test は internal/herdr の socket クライアントを検証する。
//
// 実際の herdr には接続しない。net.Listen("unix", ...) でテスト用herdr mock サーバを立て、
// 決まった JSON を返させることで、herdr の socket API の性質
// （改行区切り JSON・1コネクション=1リクエスト・エラー応答の形）をテストする
// （docs/plans/continuo_design.md 2-1）。
package herdr_test

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// rpcRequest は herdr の socket API のリクエストの wire format を、テスト側で
// そのまま検査できるようにした写しである（internal/herdr の非公開型とは独立している）。
type rpcRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// rpcError は herdr の socket API のエラー応答の wire format の写しである。
type rpcError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// rpcResponse は herdr の socket API の応答の wire format の写しである。
type rpcResponse struct {
	ID     string          `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

// fakeServer は herdr の socket API の代わりに使う偽のサーバである。
// 受け取った生のリクエスト行をすべて記録し、接続回数を数える。
type fakeServer struct {
	socketPath string
	ln         net.Listener

	connCount atomic.Int32

	mu    sync.Mutex
	lines [][]byte
}

// newFakeServer は Unix domain socket を1本立て、接続を受けるたびに handle を
// 別 goroutine で呼び出す偽サーバを起動する。
//
// t: 呼び出し元のテスト。socket ファイルとリスナーの後始末を t.Cleanup に登録する。
// handle: 接続ごとに呼ばれる関数。読み取った1行（末尾の改行を含む）と net.Conn を渡す。
// 応答を書くかどうか・いつ書くかは handle が決める。handle が conn を閉じない場合は
// このヘルパーが呼び出し後に閉じる。
// **handle はテスト本体とは別の goroutine（接続ごとに1本）で走るので、その中で
// t.Fatalf・t.FailNow・t.Fatal を呼んではならない。**失敗は t.Errorf で記録して
// return すること（理由は writeResult の GoDoc を参照）。
// 戻り値: 起動した *fakeServer。SocketPath() を herdr.New に渡すこと。
//
// socket のパスは意図的に短く保つ（os.MkdirTemp の既定の接頭辞だけを使う）。
// Go の t.TempDir() はテスト名を含む長いパスになりやすく、macOS の Unix domain
// socket のパス長上限（103バイト。internal/socketpath の実測）に触れかねないため、
// ここでは使わない。
func newFakeServer(t *testing.T, handle func(t *testing.T, n int32, line []byte, conn net.Conn)) *fakeServer {
	t.Helper()

	dir, err := os.MkdirTemp("", "herdrtest")
	if err != nil {
		t.Fatalf("偽サーバ用の一時ディレクトリを作成できません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	socketPath := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("偽サーバの Unix domain socket を bind できません（%s）: %v", socketPath, err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	fs := &fakeServer{socketPath: socketPath, ln: ln}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				// リスナーが閉じられた（テスト終了）。
				return
			}
			n := fs.connCount.Add(1)
			go func(n int32, conn net.Conn) {
				defer func() { _ = conn.Close() }()

				// テストが失敗して応答を書き忘れても、この goroutine が
				// プロセス終了までブロックし続けないようにする安全弁。
				_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

				reader := bufio.NewReader(conn)
				line, err := reader.ReadBytes('\n')
				if err != nil && len(line) == 0 {
					return
				}

				fs.mu.Lock()
				fs.lines = append(fs.lines, append([]byte(nil), line...))
				fs.mu.Unlock()

				handle(t, n, line, conn)
			}(n, conn)
		}
	}()

	return fs
}

// SocketPath は偽サーバが listen している socket の絶対パスを返す。
func (fs *fakeServer) SocketPath() string {
	return fs.socketPath
}

// ConnCount はこれまでに受け付けた接続回数を返す。
// 「1リクエストごとに接続し直していること」の検査に使う。
func (fs *fakeServer) ConnCount() int32 {
	return fs.connCount.Load()
}

// Lines はこれまでに受け取った生のリクエスト行（末尾の改行を含む）を、受け取った順に返す。
func (fs *fakeServer) Lines() [][]byte {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	out := make([][]byte, len(fs.lines))
	copy(out, fs.lines)
	return out
}

// sentParams は偽サーバが受け取ったちょうど1件のリクエストから、method を照合したうえで
// params を汎用の map として取り出す。
//
// 「実スキーマに無い引数を送っていないこと」を検査したいので、構造体ではなく map で受ける
// （構造体で受けると、余計なキーが混ざっていても気づけない）。
//
// t: 呼び出し元のテスト。**テスト本体の goroutine から呼ぶこと**（t.Fatalf を使う）。
// fs: 検査対象の偽サーバ。
// wantMethod: 送られているはずのメソッド名。
// 戻り値: params を map にしたもの。JSON の数値は float64 になる。
func sentParams(t *testing.T, fs *fakeServer, wantMethod string) map[string]any {
	t.Helper()

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
	if sent.Method != wantMethod {
		t.Fatalf("method が想定と違う: got %q, want %q", sent.Method, wantMethod)
	}
	return sent.Params
}

// assertSchemaKeys は params のキーが、実スキーマ
// （`herdr api schema --json` の `schemas.request.$defs`）に定義されたキーの集合に
// 収まっていることを検査する。
//
// **実スキーマに無いキーを送っていないことを確かめるための関数である。**構造体で
// 受けると余計なキーに気づけないので、呼び出し側は sentParams で map として受け取ること。
//
// t: 呼び出し元のテスト。**テスト本体の goroutine から呼ぶこと**（t.Fatalf を使う）。
// params: 偽サーバが受け取った params。
// method: エラーメッセージに出すメソッド名。
// schemaKeys: 実スキーマに定義されているキーの全集合（必須・任意を問わず並べる）。
func assertSchemaKeys(t *testing.T, params map[string]any, method string, schemaKeys []string) {
	t.Helper()

	known := make(map[string]bool, len(schemaKeys))
	for _, key := range schemaKeys {
		known[key] = true
	}
	for key := range params {
		if !known[key] {
			t.Fatalf("%s に実スキーマに無い引数 %q を送っている: %v", method, key, params)
		}
	}
}

// writeResult は id に対する成功応答を conn へ書く。
//
// **この関数は接続ごとの goroutine（newFakeServer が起動する handle）から呼ばれるので、
// t.Fatalf（FailNow）を使ってはならない。**testing の規約は
// 「FailNow はテスト本体を実行している goroutine から呼ばなければならず、テスト中に
// 作った別の goroutine から呼んではならない。FailNow を呼んでも、それらの別の goroutine は
// 止まらない」と定めている（go doc testing.T.FailNow）。実際に踏むとテストが失敗として
// 記録されず、クライアント側は応答待ちのまま残る。失敗は t.Errorf で記録して return する。
func writeResult(t *testing.T, conn net.Conn, id string, result any) {
	t.Helper()
	resultJSON, err := json.Marshal(result)
	if err != nil {
		t.Errorf("テスト用応答の result を JSON 化できません: %v", err)
		return
	}
	writeResponse(t, conn, rpcResponse{ID: id, Result: resultJSON})
}

// writeErrorResponse は id に対するエラー応答を conn へ書く。
//
// writeResult と同じく接続ごとの goroutine から呼ばれるので、t.Fatalf を使ってはならない。
func writeErrorResponse(t *testing.T, conn net.Conn, id, code, message string) {
	t.Helper()
	writeResponse(t, conn, rpcResponse{ID: id, Error: &rpcError{Code: code, Message: message}})
}

// writeResponse は resp を改行区切り JSON として conn へ書く。
//
// writeResult と同じく接続ごとの goroutine から呼ばれるので、t.Fatalf を使ってはならない。
func writeResponse(t *testing.T, conn net.Conn, resp rpcResponse) {
	t.Helper()
	out, err := json.Marshal(resp)
	if err != nil {
		t.Errorf("テスト用応答を JSON 化できません: %v", err)
		return
	}
	out = append(out, '\n')
	if _, err := conn.Write(out); err != nil {
		// クライアント側が既にタイムアウトして接続を閉じたあとに書こうとした場合など。
		// テストの意図によっては起こりうるので Fatal にしない。
		t.Logf("テスト用応答の書き込みに失敗しました（無視する）: %v", err)
	}
}
