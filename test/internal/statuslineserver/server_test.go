// Package statuslineserver_test は、使用率を受ける socket（sl.sock）が、本物の Unix socket で
// 1接続1行を受けて本体へ渡すことを確かめる（issue #284）。
package statuslineserver_test

import (
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/statuslineserver"
)

// waitTimeout は行が届くのを待つ上限である。
const waitTimeout = 5 * time.Second

// recordingSink は受けた行を順に控える。受け口の goroutine から呼ばれる。
type recordingSink struct {
	mu    sync.Mutex
	lines []statuslineserver.Line
	got   chan struct{}
}

// newRecordingSink は控えを作る。
func newRecordingSink() *recordingSink {
	return &recordingSink{got: make(chan struct{}, 16)}
}

// sink は statuslineserver.Sink として渡す関数である。
func (r *recordingSink) sink(line statuslineserver.Line) {
	r.mu.Lock()
	r.lines = append(r.lines, line)
	r.mu.Unlock()
	select {
	case r.got <- struct{}{}:
	default:
	}
}

// Lines は受けた行の写しを返す。
func (r *recordingSink) Lines() []statuslineserver.Line {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]statuslineserver.Line, len(r.lines))
	copy(out, r.lines)
	return out
}

// waitLine は1行届くまで待つ。届かなければテストを落とす。
func (r *recordingSink) waitLine(t *testing.T) statuslineserver.Line {
	t.Helper()
	select {
	case <-r.got:
	case <-time.After(waitTimeout):
		t.Fatal("使用率の行が本体へ届きませんでした")
	}
	lines := r.Lines()
	return lines[len(lines)-1]
}

// shortSocketPath は、macOS の Unix socket の長さの上限（104バイト）に収まる短い一時ディレクトリに
// sl.sock のパスを作る。t.TempDir() はテストの名前を含んで長くなるので使わない。
func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "slsrv")
	if err != nil {
		t.Fatalf("一時ディレクトリを作成できません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "sl.sock")
}

// startServer は受け口を立てて listen を始め、テストの終わりに閉じる。
func startServer(t *testing.T) (*statuslineserver.Server, *recordingSink, string) {
	t.Helper()
	path := shortSocketPath(t)
	rec := newRecordingSink()
	srv := statuslineserver.New(path, rec.sink, nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start に失敗した: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv, rec, path
}

// sendRaw は socket へ接続して data を書き、接続を閉じる。
func sendRaw(t *testing.T, path string, data string) {
	t.Helper()
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatalf("socket へ接続できない: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte(data)); err != nil {
		t.Fatalf("socket へ書けない: %v", err)
	}
}

// 目的: 本物の Unix socket で送った1行が、4つの欄を保ったまま本体へ届くことを確かめる。
// 与える情報: session_id・api_ms・five_hour・seven_day を持つ1行（used_percentage は小数を含む）。
// 成功条件: sink が1行を受け、4つの欄の値が送ったとおりで、HasRateLimits が真であること。
func TestServer_本物のsocketで1行が本体へ届く(t *testing.T) {
	_, rec, path := startServer(t)

	sendRaw(t, path, `{"session_id":"0f3c9a1b-2d4e-4f60-8a7b-1c2d3e4f5a6b","api_ms":10728,`+
		`"five_hour":{"used_percentage":3.5,"resets_at":1790338200},`+
		`"seven_day":{"used_percentage":34,"resets_at":1790622000}}`+"\n")

	got := rec.waitLine(t)
	if got.SessionID != "0f3c9a1b-2d4e-4f60-8a7b-1c2d3e4f5a6b" {
		t.Fatalf("session_id = %q", got.SessionID)
	}
	if got.APIMs != 10728 {
		t.Fatalf("api_ms = %d, want 10728", got.APIMs)
	}
	if got.FiveHour == nil || got.FiveHour.UsedPercentage != 3.5 || got.FiveHour.ResetsAt != 1790338200 {
		t.Fatalf("five_hour = %+v", got.FiveHour)
	}
	if got.SevenDay == nil || got.SevenDay.UsedPercentage != 34 || got.SevenDay.ResetsAt != 1790622000 {
		t.Fatalf("seven_day = %+v", got.SevenDay)
	}
	if !got.HasRateLimits() {
		t.Fatal("期間を持つ行なのに HasRateLimits が偽になった")
	}
}

// 目的: rate_limits が null の行（session_id と api_ms だけ）も本体へ届くことを確かめる
// （「応答はあったが値が無い」を見分けるため。捨てると statusline取得の判定ができない）。
// 与える情報: session_id と api_ms だけの1行。改行で終わらない（送り手が閉じて EOF になる）形も試す。
// 成功条件: どちらも sink が受け、期間は nil で、HasRateLimits が偽であること。
func TestServer_rate_limitsがnullの行も届く(t *testing.T) {
	_, rec, path := startServer(t)

	sendRaw(t, path, `{"session_id":"s1","api_ms":0}`+"\n")
	got := rec.waitLine(t)
	if got.SessionID != "s1" || got.APIMs != 0 {
		t.Fatalf("行 = %+v", got)
	}
	if got.FiveHour != nil || got.SevenDay != nil || got.HasRateLimits() {
		t.Fatalf("期間の無い行に期間が入っている: %+v", got)
	}

	sendRaw(t, path, `{"session_id":"s2","api_ms":5}`)
	got = rec.waitLine(t)
	if got.SessionID != "s2" || got.APIMs != 5 {
		t.Fatalf("改行の無い行 = %+v", got)
	}
}

// 目的: 知らない欄を無視して受けることを確かめる（送り手が欄を足しても、新旧の実行ファイルが
// 混ざったまま落ちないため）。
// 与える情報: 知らない欄（トップと期間の中の両方）を持つ1行。
// 成功条件: sink が受け、知っている欄の値が正しいこと。
func TestServer_知らない欄を無視する(t *testing.T) {
	_, rec, path := startServer(t)

	sendRaw(t, path, `{"session_id":"s1","api_ms":7,"future_field":{"x":1},`+
		`"five_hour":{"used_percentage":50,"resets_at":1790338200,"extra":true},"spend_limit":{"a":1}}`+"\n")

	got := rec.waitLine(t)
	if got.SessionID != "s1" || got.APIMs != 7 {
		t.Fatalf("行 = %+v", got)
	}
	if got.FiveHour == nil || got.FiveHour.UsedPercentage != 50 {
		t.Fatalf("five_hour = %+v", got.FiveHour)
	}
	if got.SevenDay != nil {
		t.Fatalf("送っていない seven_day が入っている: %+v", got.SevenDay)
	}
}

// 目的: 読めない行と session_id の無い行を捨て、あとから来た正しい行は受けることを確かめる
// （壊れた送り手で受け口が止まらないこと）。
// 与える情報: 壊れた JSON・session_id の無い行・正しい行の3本を別の接続で順に送る。
// 成功条件: sink が受けるのは正しい行の1本だけであること。
func TestServer_読めない行とsession_idの無い行は捨てる(t *testing.T) {
	_, rec, path := startServer(t)

	sendRaw(t, path, "{壊れた\n")
	sendRaw(t, path, `{"api_ms":3}`+"\n")
	sendRaw(t, path, `{"session_id":"ok","api_ms":1}`+"\n")

	got := rec.waitLine(t)
	if got.SessionID != "ok" {
		t.Fatalf("最初に届いた行 = %+v, want session_id ok", got)
	}
	// 捨てた行があとから届かないことを確かめるため、少し待つ。
	time.Sleep(100 * time.Millisecond)
	if lines := rec.Lines(); len(lines) != 1 {
		t.Fatalf("届いた行の数 = %d, want 1: %+v", len(lines), lines)
	}
}

// 目的: 上限（64 KiB）を超える行を捨てることを確かめる（壊れた送り手に本体のメモリを使わせない）。
// 与える情報: session_id を持つが、全体が MaxLineBytes を超える1行。
// 成功条件: sink が受けないこと。
func TestServer_長すぎる行は捨てる(t *testing.T) {
	_, rec, path := startServer(t)

	pad := make([]byte, statuslineserver.MaxLineBytes)
	for i := range pad {
		pad[i] = 'a'
	}
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatalf("socket へ接続できない: %v", err)
	}
	// **書き込みの誤りは見ない。**受け口は上限を超えたところで読むのをやめて閉じるので、
	// 残りの数バイトは broken pipe で断られうる。
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, _ = conn.Write([]byte(`{"session_id":"s1","api_ms":1,"pad":"` + string(pad) + `"}` + "\n"))
	_ = conn.Close()
	time.Sleep(200 * time.Millisecond)
	if lines := rec.Lines(); len(lines) != 0 {
		t.Fatalf("長すぎる行が届いた: %d 本", len(lines))
	}
}

// 目的: 同じパスで生きている相手がいれば起動を止め、残骸なら消して listen することを確かめる
// （二重起動の疑いを見逃さない。hook の受け口と同じ）。
// 与える情報: 1つ目の受け口が listen している同じパスで2つ目を Start する。次に、誰も listen して
// いない socket ファイル（残骸）を置いたパスで Start する。
// 成功条件: 2つ目の Start がエラーを返し、1つ目は引き続き行を受けること。残骸のパスでは Start が
// 成功し、行を受けること。socket の権限が 0600 であること。
func TestServer_生きている相手がいれば止め残骸は消してlistenする(t *testing.T) {
	_, rec, path := startServer(t)

	second := statuslineserver.New(path, func(statuslineserver.Line) {}, nil)
	if err := second.Start(); err == nil {
		_ = second.Close()
		t.Fatal("同じパスで生きている受け口がいるのに Start が成功した")
	}
	sendRaw(t, path, `{"session_id":"still","api_ms":1}`+"\n")
	if got := rec.waitLine(t); got.SessionID != "still" {
		t.Fatalf("1つ目の受け口が行を受けていない: %+v", got)
	}

	// 残骸を作る: listen してから、ファイルを消さずに listener を閉じる。
	stalePath := shortSocketPath(t)
	ln, err := net.Listen("unix", stalePath)
	if err != nil {
		t.Fatalf("残骸を作れない: %v", err)
	}
	if ul, ok := ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	_ = ln.Close()
	if _, err := os.Lstat(stalePath); err != nil {
		t.Fatalf("残骸のファイルが無い: %v", err)
	}

	rec2 := newRecordingSink()
	srv := statuslineserver.New(stalePath, rec2.sink, nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("残骸があるだけなのに Start が失敗した: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	info, err := os.Stat(stalePath)
	if err != nil {
		t.Fatalf("socket ファイルが無い: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket の権限 = %o, want 600", perm)
	}
	sendRaw(t, stalePath, `{"session_id":"fresh","api_ms":1}`+"\n")
	if got := rec2.waitLine(t); got.SessionID != "fresh" {
		t.Fatalf("残骸を消したあとの受け口が行を受けていない: %+v", got)
	}
}

// 目的: Close で socket ファイルが消え、そのあとは接続できず、2回呼んでも落ちないことを確かめる。
// 与える情報: listen を始めた受け口。
// 成功条件: Close が2回とも nil を返し、socket ファイルが無く、接続が断られること。
func TestServer_Closeでsocketを消し2回呼んでもよい(t *testing.T) {
	srv, _, path := startServer(t)

	if err := srv.Close(); err != nil {
		t.Fatalf("1回目の Close = %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("2回目の Close = %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("Close のあとも socket ファイルが残っている: %v", err)
	}
	if conn, err := net.DialTimeout("unix", path, 200*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("Close のあとも接続できた")
	}
	if err := srv.Start(); err == nil {
		t.Fatal("Close のあとの Start が成功した")
	}
}
