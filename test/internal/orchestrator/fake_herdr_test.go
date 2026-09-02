package orchestrator_test

import (
	"io"
	"net"
	"testing"
	"time"
)

// TestFakeHerdr_途中で切れたリクエストでテストを落とさない は、テスト用herdr mock が
// 「改行まで届かなかったリクエスト」をどう扱うかを確かめる。
//
// 目的: **書いている途中で切れた接続で、無関係なテストが落ちるのを防ぐ。**
//
// **何が起きていたか。**continuo は herdr へ送っている最中に接続を切ることがある。
// herdr のクライアントは ctx が終わると conn を閉じ
// （[internal/herdr/client.go](../../../internal/herdr/client.go) の `context.AfterFunc`）、
// `conn.SetDeadline` の期限が来ても書き込みを打ち切る。**走行中の run は、テスト本体が
// 終わったあとに `agent.prompt` を送ることがある**ので、片付けの `orc.Close` が
// ちょうどその書き込みを切る。
//
// **macOS では、それが「途中まで届いた」という形で表に出る。**Unix domain socket の
// 送信バッファが 8192 バイトしかないため（`sysctl net.local.stream.sendspace`）、
// **それより大きいリクエストは受け手が読み進めるまで書き込みが止まる。**
// `agent.prompt` は [internal/prompt/builtin.md](../../../internal/prompt/builtin.md)
// （14583 バイト）を含むので 15119 バイトあり、必ずここで止まる。
// **Linux は既定のバッファがずっと大きく、書き込みが止まらないので起きない。**
//
// 与える情報: 改行の前で切れた、途中までのリクエスト。
// 成功条件: テストが落ちず、そのリクエストが受け取った一覧にも入らないこと。
func TestFakeHerdr_途中で切れたリクエストでテストを落とさない(t *testing.T) {
	fh := newFakeHerdr(t)

	// **`net.Pipe` を使う。**書き手と読み手が直結するので、
	// 「改行まで届く前に切れた」を待ち時間なしで確実に作れる。
	writer, reader := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		// 途中まで書いて切る（**閉じの `"}}` と改行を書かない**）。
		_, _ = io.WriteString(writer, `{"id":"x","method":"agent.prompt","params":{"prompt":"あ`)
		_ = writer.Close()
	}()

	fh.serve(t, reader)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("書き手が終わらない")
	}

	// **途中で切れたものは、届いたリクエストとして数えない。**
	// 数えると、呼び出しの回数を見ているテストが1件多く数える。
	if got := fh.Requests(); len(got) != 0 {
		t.Errorf("途中で切れたリクエストを受け取った扱いにしている: %+v", got)
	}
}
