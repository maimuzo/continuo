package hookserver_test

import (
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
)

// 目的: listen を始めたあとの socket ファイルの権限が 0600 に落ちていることを確認する。
// ディレクトリの 0700 が本体の防御だが（設計 3-23）、Go が作る socket の権限は umask 次第で
// 0755 になるので、socket 側も 0600 に落とす二重の防御を入れてある。消えても誰も気づかない。
// 与える情報: umask を 0 にして（＝権限を緩める側に振って）から Start した hookserver。
// 成功条件: socket ファイルの権限が 0600 であること。
func TestServer_socketの権限を0600に落とす(t *testing.T) {
	// umask を 0 にすると、Go が作る socket は 0777 & ^umask の権限になる。
	// この状態で 0600 になっていれば、Chmod が効いていることが確かめられる。
	old := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(old) })

	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	ts.start(t)

	info, err := os.Lstat(ts.socketPath)
	if err != nil {
		t.Fatalf("socket ファイルを調べられません: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket ファイルの権限が 0600 ではなく %#o です（同じマシンの他の利用者から書けます）", perm)
	}
}

// 目的: 前回の実行が残した socket ファイル（誰も listen していない残骸）を消して
// listen を始められることを確認する。消せないと continuo が二度と起動しない。
// 与える情報: socket のパスに置いた、誰も listen していないファイル。
// 成功条件: Start が成功し、消したことがログに残り、hook が届いて配送されること。
func TestServer_残骸のsocketファイルを消してlistenする(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)

	// 前回の実行が残した socket ファイルに見立てる（listen しているプロセスは居ない）。
	if err := os.WriteFile(ts.socketPath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("残骸の socket ファイルを置けません: %v", err)
	}

	ts.start(t)
	ts.server.StartDelivery()

	if !strings.Contains(ts.logs.String(), "誰も listen していませんでした") {
		t.Fatalf("残骸を消したことがログに残っていません: %s", ts.logs.String())
	}
	ts.send(t, stopEventJSON("after-stale", "[]"))
	if got := sink.waitFor(t, 1); got[0].SessionID != "after-stale" {
		t.Fatalf("残骸を消したあとの hook が配送されていません: %+v", got[0])
	}
}

// 目的: 同じパスで既に別のプロセスが listen しているとき、Start がエラーになり、
// **先に居るほうを壊さない**ことを確認する（continuo の二重起動）。
// 誤判定すると continuo が二度と起動しないか、先に動いている continuo の socket を奪う。
// 与える情報: 先に立てた別の listener と、同じパスで Start する hookserver。
// 成功条件: Start がエラーを返し、先に立てた listener がそのまま接続を受け付けられること。
func TestServer_別のプロセスがlistenしていたら起動しない(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)

	other, err := net.Listen("unix", ts.socketPath)
	if err != nil {
		t.Fatalf("先に立てる listener を作れません: %v", err)
	}
	defer func() { _ = other.Close() }()

	accepted := make(chan struct{}, 1)
	go func() {
		conn, err := other.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
		accepted <- struct{}{}
	}()

	if err := ts.server.Start(); err == nil {
		t.Fatalf("別のプロセスが listen しているのに Start が成功しました")
	} else if !strings.Contains(err.Error(), "二重起動") {
		t.Fatalf("二重起動を疑うエラーになっていません: %v", err)
	}

	// 先に居るほうが生きていること（socket ファイルを消されていないこと）を確かめる。
	conn, err := net.DialTimeout("unix", ts.socketPath, deliverWaitTimeout)
	if err != nil {
		t.Fatalf("先に立てた listener へ繋げません（socket を奪われました）: %v", err)
	}
	_ = conn.Close()
	<-accepted
}
