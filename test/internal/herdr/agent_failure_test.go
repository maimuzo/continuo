package herdr_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/normalize"
)

// newBrokenResultServer は「成功応答だが result の形が想定と違う」偽サーバを立てる。
//
// **herdr は JSON-RPC 風の応答を返すが、result の中身の形は保証されていない**
// （internal/herdr の各 Result 型の GoDoc が「メソッドと応答の対応づけは推定である」と
// 書いている）。想定外の形が返ったときに、クライアントが黙って空の値を返さず、
// 「解析できない」と分かるエラーを返すことを確かめるために使う。
//
// t: 呼び出し元のテスト。
// 戻り値: result に JSON 文字列（オブジェクトではない）を返す偽サーバ。
func newBrokenResultServer(t *testing.T) *fakeServer {
	t.Helper()
	return newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		// **オブジェクトではなく文字列を返す。**各 Result 型は構造体なので解析に失敗する。
		writeResult(t, conn, req.ID, "これはオブジェクトではありません")
	})
}

// newErrorServer は毎回同じエラー応答を返す偽サーバを立てる。
//
// t: 呼び出し元のテスト。
// code: 返すエラーコード。
// 戻り値: 起動した偽サーバ。
func newErrorServer(t *testing.T, code string) *fakeServer {
	t.Helper()
	return newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		// エラー応答では id が空文字で返る（2-1 の実測）。実機と同じ形で返す。
		writeErrorResponse(t, conn, "", code, "テスト用のエラー応答")
	})
}

// testTimeouts はこのファイルのテストが使う待ち時間である。
// **どれも実際には待たない経路のテストなので短くてよいが、遅いマシンでも
// 誤って時間切れにならない長さを取る。**
func testTimeouts() herdr.Timeouts {
	return herdr.Timeouts{Read: 30 * time.Second, Startup: 30 * time.Second, Turn: 30 * time.Second}
}

// TestAgent_resultの形が想定と違えば解析できないエラーになる は、
// agent 系の呼び出しが想定外の応答を黙って受け入れないことを確かめる。
//
// 目的: 各 Result 型の GoDoc は「メソッドと応答の対応づけは推定である」と書いている。
// **推定が外れたときに、空の構造体を返して先へ進んではならない。**
// 空の Agent が返ると、呼び出し側は agent_status も pane_id も空のまま処理を続け、
// **どこで壊れたのか分からない失敗**になる。
// 与える情報: result にオブジェクトではなく JSON 文字列を返す偽サーバ。
// 成功条件: agent.start / prompt / read / get / list / wait / rename のすべてが
// エラーを返し、文言にそのメソッド名が入っていること。
func TestAgent_resultの形が想定と違えば解析できないエラーになる(t *testing.T) {
	name := normalize.SafeName("continuo-test")

	cases := []struct {
		name       string
		wantMethod string
		call       func(c *herdr.Client) error
	}{
		{
			name:       "agent.start",
			wantMethod: herdr.MethodAgentStart,
			call: func(c *herdr.Client) error {
				_, err := c.AgentStart(context.Background(), herdr.AgentStartParams{
					Name: name, Kind: "claude", PaneID: "w1:p1",
				})
				return err
			},
		},
		{
			name:       "agent.prompt",
			wantMethod: herdr.MethodAgentPrompt,
			call: func(c *herdr.Client) error {
				_, err := c.AgentPrompt(context.Background(), herdr.AgentPromptParams{
					Target: name, Text: "続けてください",
				})
				return err
			},
		},
		{
			name:       "agent.read",
			wantMethod: herdr.MethodAgentRead,
			call: func(c *herdr.Client) error {
				_, err := c.AgentRead(context.Background(), herdr.AgentReadParams{
					Target: name, Source: herdr.ReadSourceRecentUnwrapped,
				})
				return err
			},
		},
		{
			name:       "agent.get",
			wantMethod: herdr.MethodAgentGet,
			call: func(c *herdr.Client) error {
				_, err := c.AgentGet(context.Background(), herdr.AgentGetParams{Target: name})
				return err
			},
		},
		{
			name:       "agent.list",
			wantMethod: herdr.MethodAgentList,
			call: func(c *herdr.Client) error {
				_, err := c.AgentList(context.Background())
				return err
			},
		},
		{
			name:       "agent.wait",
			wantMethod: herdr.MethodAgentWait,
			call: func(c *herdr.Client) error {
				_, err := c.AgentWait(context.Background(), herdr.AgentWaitParams{Target: name})
				return err
			},
		},
		{
			name:       "agent.rename",
			wantMethod: herdr.MethodAgentRename,
			call: func(c *herdr.Client) error {
				_, err := c.AgentRename(context.Background(), herdr.AgentRenameParams{
					Target: name, Name: normalize.SafeName("continuo-test2"),
				})
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := newBrokenResultServer(t)
			client := herdr.New(fs.SocketPath(), testTimeouts())

			err := tc.call(client)
			if err == nil {
				t.Fatalf("result の形が違うのにエラーにならなかった（%s）", tc.wantMethod)
			}
			if !strings.Contains(err.Error(), tc.wantMethod) {
				t.Fatalf("どのメソッドで壊れたのかが文言に出ていない: %v", err)
			}
		})
	}
}

// TestAgent_herdrのエラー応答はそのまま呼び出し側へ返る は、
// agent 系の呼び出しが herdr のエラー応答を握り潰さないことを確かめる。
//
// 目的: **エラーを握り潰すと、呼び出し側は空の結果を正常として扱う。**
// agent.read / agent.wait / agent.rename / agent.send_keys は、いずれも失敗したことが
// 分からないと run の面倒を見られなくなる（画面を読めないまま turn の終わりを判定する等）。
// 与える情報: 常に同じエラーコードを返す偽サーバ。
// 成功条件: それぞれの呼び出しがエラーを返し、IsCode でそのコードを判定できること。
func TestAgent_herdrのエラー応答はそのまま呼び出し側へ返る(t *testing.T) {
	const code = "agent_not_found"
	name := normalize.SafeName("continuo-test")

	cases := []struct {
		name string
		call func(c *herdr.Client) error
	}{
		{
			name: "agent.prompt",
			call: func(c *herdr.Client) error {
				_, err := c.AgentPrompt(context.Background(), herdr.AgentPromptParams{
					Target: name, Text: "続けてください",
				})
				return err
			},
		},
		{
			name: "agent.read",
			call: func(c *herdr.Client) error {
				_, err := c.AgentRead(context.Background(), herdr.AgentReadParams{
					Target: name, Source: herdr.ReadSourceRecentUnwrapped,
				})
				return err
			},
		},
		{
			name: "agent.get",
			call: func(c *herdr.Client) error {
				_, err := c.AgentGet(context.Background(), herdr.AgentGetParams{Target: name})
				return err
			},
		},
		{
			name: "agent.list",
			call: func(c *herdr.Client) error {
				_, err := c.AgentList(context.Background())
				return err
			},
		},
		{
			name: "agent.wait",
			call: func(c *herdr.Client) error {
				_, err := c.AgentWait(context.Background(), herdr.AgentWaitParams{Target: name})
				return err
			},
		},
		{
			name: "agent.rename",
			call: func(c *herdr.Client) error {
				_, err := c.AgentRename(context.Background(), herdr.AgentRenameParams{Target: name})
				return err
			},
		},
		{
			name: "agent.send_keys",
			call: func(c *herdr.Client) error {
				return c.AgentSendKeys(context.Background(), herdr.AgentSendKeysParams{
					Target: name, Keys: []string{"esc"},
				})
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := newErrorServer(t, code)
			client := herdr.New(fs.SocketPath(), testTimeouts())

			err := tc.call(client)
			if err == nil {
				t.Fatal("herdr がエラーを返したのに成功として扱われた")
			}
			if !herdr.IsCode(err, code) {
				t.Fatalf("エラーコードで判定できない: %v", err)
			}
		})
	}
}

// TestAgent_agent名が不正なら接続せずに弾かれる は、agent 名の検査が
// すべての agent 系の呼び出しに入っていることを確かめる。
//
// 目的: **不正な名前のまま herdr へ送ると、herdr 側の一般的なエラーに化ける。**
// どの名前が悪いのかが分からず、しかも余計な接続が1本増える。
// 検査は agent.start だけでなく、prompt / read / get / wait / rename / send_keys の
// すべてに入っていなければならない。
// 与える情報: 大文字を含む不正な agent 名（`^[a-z][a-z0-9_-]{0,31}$` に収まらない）。
// 偽サーバは接続が来たら失敗として記録する。
// 成功条件: すべての呼び出しがエラーを返し、偽サーバへの接続が1本も起きないこと。
func TestAgent_agent名が不正なら接続せずに弾かれる(t *testing.T) {
	bad := normalize.SafeName("Invalid-Name")

	cases := []struct {
		name string
		call func(c *herdr.Client) error
	}{
		{
			name: "agent.prompt",
			call: func(c *herdr.Client) error {
				_, err := c.AgentPrompt(context.Background(), herdr.AgentPromptParams{Target: bad, Text: "x"})
				return err
			},
		},
		{
			name: "agent.read",
			call: func(c *herdr.Client) error {
				_, err := c.AgentRead(context.Background(), herdr.AgentReadParams{
					Target: bad, Source: herdr.ReadSourceRecentUnwrapped,
				})
				return err
			},
		},
		{
			name: "agent.get",
			call: func(c *herdr.Client) error {
				_, err := c.AgentGet(context.Background(), herdr.AgentGetParams{Target: bad})
				return err
			},
		},
		{
			name: "agent.wait",
			call: func(c *herdr.Client) error {
				_, err := c.AgentWait(context.Background(), herdr.AgentWaitParams{Target: bad})
				return err
			},
		},
		{
			name: "agent.rename の target",
			call: func(c *herdr.Client) error {
				_, err := c.AgentRename(context.Background(), herdr.AgentRenameParams{Target: bad})
				return err
			},
		},
		{
			name: "agent.send_keys",
			call: func(c *herdr.Client) error {
				return c.AgentSendKeys(context.Background(), herdr.AgentSendKeysParams{
					Target: bad, Keys: []string{"esc"},
				})
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
				t.Errorf("agent 名が不正なのに herdr へ接続してしまった（%d 回目）", n)
			})
			client := herdr.New(fs.SocketPath(), testTimeouts())

			if err := tc.call(client); err == nil {
				t.Fatal("不正な agent 名なのにエラーが返らなかった")
			}
			if got := fs.ConnCount(); got != 0 {
				t.Fatalf("接続していないはずなのに接続回数が %d だった", got)
			}
		})
	}
}

// TestAgentStartWithRetry_ctxが終わればリトライの待機中でも打ち切る は、
// リトライの待機がコンテキストを尊重することを確かめる。
//
// 目的: `agent_pane_busy` のリトライは時間で粘る（設計 2-1）。**その待機が
// コンテキストを見ていないと、`SIGINT` を受けても budget を使い切るまで戻らない。**
//
// 与える情報: 常に `agent_pane_busy` を返す偽サーバと、**1回目の呼び出しが終わった
// あとに**終わらせるコンテキスト。budget は 10 分、delay は 5 分と長く取る。
//
// **コンテキストを終わらせる時点を「クライアントが接続を閉じたとき」に固定する。**
// internal/herdr の call は応答を1行読み終えたところで接続を閉じるので、偽サーバ側で
// EOF を観測すれば「1回目の呼び出しは agent_pane_busy として終わった」ことが確実になる。
// 応答を書いた直後に終わらせると、クライアントがまだ読んでいる最中に当たることがあり、
// 「呼び出し自体の打ち切り」という別の経路を測ってしまう。
//
// 成功条件: 5 分（delay）を待たずにエラーで返り、そのエラーがコンテキストの打ち切りであること。
func TestAgentStartWithRetry_ctxが終わればリトライの待機中でも打ち切る(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		writeErrorResponse(t, conn, "", herdr.ErrCodeAgentPaneBusy, "pane がまだ使える状態ではありません")
		// **クライアントが接続を閉じる（＝応答を読み終えた）まで待ってから終わらせる。**
		buf := make([]byte, 1)
		for {
			if _, err := conn.Read(buf); err != nil {
				break
			}
		}
		cancel()
	})

	client := herdr.New(fs.SocketPath(), testTimeouts())
	start := time.Now()
	_, err := client.AgentStartWithRetry(ctx, herdr.AgentStartParams{
		Name: normalize.SafeName("continuo-test"), Kind: "claude", PaneID: "w1:p1",
	}, 10*time.Minute, 5*time.Minute)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("コンテキストが終わったのに成功として返った")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("コンテキストの打ち切りとして返っていない: %v", err)
	}
	if elapsed >= 5*time.Minute {
		t.Fatalf("リトライの待機を打ち切れていない: %v", elapsed)
	}
}
