// Package statuslineserver は、Claude Code のステータスラインから `continuo statusline` が
// 送ってくる使用率の1行を、hook とは別の socket（`sl.sock`）で受けて本体へ渡す（issue #284）。
//
// **hook の受け口（internal/hookserver）は触らない。**別の socket にした理由は、判別子を
// 知らない古い本体が使用率の行を hook として受け取らないためである。
//
// **1接続1行である。**応答は返さない。知らない欄は無視する（欄を足す変更で、新旧の
// 実行ファイルが混ざっても落ちないようにするため）。止めるときは listener を閉じ、
// 配送中の行は待たずに捨てる。
package statuslineserver

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/socketpath"
)

// MaxLineBytes は1行の上限である。これを超える行は捨てる。
const MaxLineBytes = 64 * 1024

// readTimeout は1接続から1行を読み切るまでの上限である。
const readTimeout = 2 * time.Second

// staleDialTimeout は、残骸かどうかを確かめるために接続してみるときの上限である。
const staleDialTimeout = 500 * time.Millisecond

// Window は1つの期間（5時間か7日）の値である。ステータスラインの入力の
// `rate_limits.five_hour` / `rate_limits.seven_day` をそのまま運ぶ。
type Window struct {
	// UsedPercentage は使用率（0〜100。小数のことがある）である。
	UsedPercentage float64 `json:"used_percentage"`
	// ResetsAt は期間が切り替わる時刻の Unix 秒である。
	ResetsAt int64 `json:"resets_at"`
}

// Line は `continuo statusline` が送る1行である。
//
//	{"session_id":"0f3c…","api_ms":10728,"five_hour":{"used_percentage":3,"resets_at":1790338200},"seven_day":{…}}
//
// **`rate_limits` が null の行も送る**（`session_id` と `api_ms` だけ）。「応答はあったが値が無い」を
// 見分けるためである。
type Line struct {
	// SessionID は Claude Code のセッションの ID である。
	SessionID string `json:"session_id"`
	// APIMs は `cost.total_api_duration_ms`（API の応答を待った合計のミリ秒）である。
	// API の応答のたびに増えるので、新しい応答の行かどうかの判定に使う。
	APIMs int64 `json:"api_ms"`
	// FiveHour は5時間の期間である。無ければ nil。
	FiveHour *Window `json:"five_hour,omitempty"`
	// SevenDay は7日の期間である。無ければ nil。
	SevenDay *Window `json:"seven_day,omitempty"`
}

// HasRateLimits は、期間が1つ以上ある行かを返す。
func (l Line) HasRateLimits() bool {
	return l.FiveHour != nil || l.SevenDay != nil
}

// Sink は受けた行を本体へ渡す関数である。受け口の goroutine から呼ばれる。
type Sink func(Line)

// Server は使用率を受ける socket である。
type Server struct {
	socketPath string
	runtimeDir string
	sink       Sink
	logger     *slog.Logger

	mu     sync.Mutex
	ln     net.Listener
	conns  map[net.Conn]struct{}
	closed bool
	wg     sync.WaitGroup
}

// New は受け口を作る。listen は Start で始める。
//
// socketPath: `sl.sock` の絶対パス（socketpath.ResolveStatusline が返したもの）。
// sink: 受けた行を渡す先。
// logger: ログの出力先。nil なら捨てる。
func New(socketPath string, sink Sink, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Server{
		socketPath: socketPath,
		runtimeDir: filepath.Dir(socketPath),
		sink:       sink,
		logger:     logger,
		conns:      map[net.Conn]struct{}{},
	}
}

// Start は listen を始める。
//
// **同じパスで生きている相手がいれば起動を止める**（hookserver と同じ。二重起動の疑い）。
// 残骸なら消してから listen する。socket は 0600 にする。
//
// 戻り値: 置き場所を用意できない・生きている相手がいる・残骸を消せない・listen できない
// 場合のエラー。
func (s *Server) Start() error {
	s.mu.Lock()
	if s.closed || s.ln != nil {
		s.mu.Unlock()
		return i18n.Errorf(i18n.KeyStatuslineserverAlreadyStarted)
	}
	s.mu.Unlock()

	if err := socketpath.EnsureDir(s.runtimeDir); err != nil {
		return err
	}
	if err := s.removeStale(); err != nil {
		return err
	}
	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return i18n.Errorf(i18n.KeyStatuslineserverStartListenFailed, s.socketPath, err)
	}
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		s.logger.Warn("使用率を受ける socket の権限を 0600 に設定できませんでした（ディレクトリの 0700 で防御は続く）",
			"socket", s.socketPath, "error", err)
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = ln.Close()
		_ = os.Remove(s.socketPath)
		return i18n.Errorf(i18n.KeyStatuslineserverAlreadyStarted)
	}
	s.ln = ln
	s.wg.Add(1)
	s.mu.Unlock()

	go s.acceptLoop(ln)
	s.logger.Info("使用率を受ける socket の listen を始めました", "socket", s.socketPath)
	return nil
}

// removeStale は前回の実行が残した socket ファイルを片付ける。
func (s *Server) removeStale() error {
	if _, err := os.Lstat(s.socketPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return i18n.Errorf(i18n.KeyStatuslineserverRemoveStaleLstatFailed, s.socketPath, err)
	}
	conn, err := net.DialTimeout("unix", s.socketPath, staleDialTimeout)
	if err == nil {
		_ = conn.Close()
		return i18n.Errorf(i18n.KeyStatuslineserverRemoveStaleAlreadyListening, s.socketPath)
	}
	if err := os.Remove(s.socketPath); err != nil {
		return i18n.Errorf(i18n.KeyStatuslineserverRemoveStaleRemoveFailed, s.socketPath, err)
	}
	s.logger.Info("前回の実行が残した使用率の socket ファイルを消しました（誰も listen していませんでした）",
		"socket", s.socketPath)
	return nil
}

// acceptLoop は接続を受け付け続ける。Close で listener が閉じられると終わる。
func (s *Server) acceptLoop(ln net.Listener) {
	defer s.wg.Done()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || s.isClosed() {
				return
			}
			s.logger.Warn("使用率の接続を受け付けられませんでした", "socket", s.socketPath, "error", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if !s.track(conn) {
			_ = conn.Close()
			return
		}
		go s.handle(conn)
	}
}

// track は接続を追跡表へ入れる。閉じたあとなら偽を返す。
func (s *Server) track(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[conn] = struct{}{}
	s.wg.Add(1)
	return true
}

// handle は1接続から1行を読み、本体へ渡す。
func (s *Server) handle(conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		_ = conn.Close()
	}()
	_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
	r := bufio.NewReaderSize(io.LimitReader(conn, MaxLineBytes+1), 4096)
	data, err := r.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return
	}
	if len(data) > MaxLineBytes {
		s.logger.Warn("使用率の1行が長すぎるので捨てました", "bytes", len(data))
		return
	}
	var line Line
	if err := json.Unmarshal(data, &line); err != nil {
		s.logger.Debug("使用率の1行を読めないので捨てました", "error", err)
		return
	}
	if line.SessionID == "" {
		return
	}
	if s.isClosed() {
		// **配送中の行は待たずに捨てる。**止めるときに本体へ書かない。
		return
	}
	s.sink(line)
}

// isClosed は Close 済みかを返す。
func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Close は listen を止め、受け付けた接続を閉じ、socket ファイルを消す。2回呼んでもよい。
//
// **配送中の行は待たずに捨てる。**本体の状態は statusline取得でまた取り直せる。
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	ln := s.ln
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
	var err error
	if ln != nil {
		err = ln.Close()
		if rmErr := os.Remove(s.socketPath); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			s.logger.Warn("使用率を受ける socket ファイルを消せませんでした", "socket", s.socketPath, "error", rmErr)
		}
	}
	s.wg.Wait()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
