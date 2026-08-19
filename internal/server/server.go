// Package server は任意の HTTP ダッシュボードである
// （`docs/plans/continuo_design.md` 5-2 / 8-2。`SPEC.md` 13.7 の任意拡張）。
//
// **任意の機能である。`server.port` が `null` なら listen しない。**
// 起動するかどうかは `New` が決める。null を渡すと `New` は `nil` を返し、
// **socket も goroutine も1つも作らない。**
//
// **読むだけの窓である。**やることは3つだけである。
//
//	実行中の run の一覧を出す   … issue / Status / turn 数 / 最後に hook を受けた時刻
//	トークンの集計を出す        … `requestId` で重複排除済みの累計（設計 3-15）
//	それを HTML と JSON で返す  … GET だけ
//
// **書き込みの経路は作らない。**run を止める・Status を書くといった操作は
// 一切受け付けない。**このサーバは認証を持たない**ので、操作を受け付けたら
// 同じマシンの任意のプロセスから continuo を動かせてしまう。
//
// **待ち受けるアドレスは 127.0.0.1 に固定である**（`LoopbackHost`）。設定から変えられない。
// run の中身（issue の URL・worktree のパス・トークンの消費）は外へ晒すものではない。
//
// **orchestrator の内部状態には触らない。**`RunSource`（＝`orchestrator.RunViews`）が返す
// 写しだけを読む。ダッシュボードの都合で run の状態が変わることはない。
//
// **待ち受け先を変えるには continuo の再起動が要る。**`New` はポート番号を値として
// 写し取り、設定への参照を持たない。設定の読み直しは実装していないうえ、読み直しを
// 入れるとしても `server.port` は反映しない（設計 3-24 の「読み直しても反映しないもの」）。
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maimuzo/continuo/internal/orchestrator"
)

// LoopbackHost は待ち受けるアドレスである。
//
// **設定から変えられない。ここを `0.0.0.0` にしてはならない。**
// ダッシュボードが出すのは issue の URL・Status・トークンの消費であり、
// **同じマシンの人間だけが見るもの**である。認証を持たないので、
// 外から届く経路を作った時点で丸見えになる。
const LoopbackHost = "127.0.0.1"

// APIStatePath は JSON でいまの状態を返す経路である（`SPEC.md` 13.7.2 の最低限の要求）。
//
// **仕様が `/api/v1/*` の下に置くことを求めているので、その形に合わせる。**
const APIStatePath = "/api/v1/state"

// 応答に関する既定値である。
const (
	// DefaultReadHeaderTimeout はリクエストのヘッダを読み切るまでの上限である。
	// **無指定のまま放置しない。**接続だけ張って何も送らない相手に goroutine を
	// 占有されるのを防ぐ。
	DefaultReadHeaderTimeout = 5 * time.Second

	// DefaultReadTimeout はリクエストを本文まで読み切るまでの上限である。
	// **ヘッダの期限だけでは、本文をだらだら送り続ける接続を切れない。**
	// 受けるのは GET だけなので短くてよい。
	DefaultReadTimeout = 10 * time.Second

	// DefaultWriteTimeout は応答を書き終えるまでの上限である。
	// **応答を読まずに放置する相手に goroutine を占有させない。**
	DefaultWriteTimeout = 10 * time.Second

	// DefaultIdleTimeout は keep-alive の接続を次の要求まで待つ上限である。
	DefaultIdleTimeout = 60 * time.Second

	// DefaultMaxHeaderBytes はリクエストのヘッダの上限である。
	// **`Host` と URL はログに流れる**ので、量そのものを抑える。
	DefaultMaxHeaderBytes = 64 * 1024

	// DefaultShutdownTimeout は Close が処理中の応答を待つ上限である。
	DefaultShutdownTimeout = 5 * time.Second

	// refreshSeconds は HTML の自動再読み込みの間隔（秒）である。
	// **JavaScript は使わない**（`<meta http-equiv="refresh">` で行う）。
	// 巡回の既定（`polling.interval_ms` = 30000）より短くして、
	// 見ている間に状態が古いままにならないようにする。
	refreshSeconds = 10
)

// RunSource は実行中の run の写しを供給する。
//
// ***orchestrator.Orchestrator がこれを満たす**（`RunViews`）。
// **写しを受け取るだけで、内部状態には触らない**（設計 3-25 の印の集合を壊さない）。
type RunSource interface {
	// RunViews は印の集合に入っている run の写しを返す（順序は不定）。
	RunViews() []orchestrator.RunView
}

// **本番の実装がこのインタフェースを満たすことを、コンパイル時に確かめる。**
var _ RunSource = (*orchestrator.Orchestrator)(nil)

// Options は Server を組み立てるための入力である。
type Options struct {
	// Port は待ち受けるポート番号である（`server.port`）。
	// **`nil` なら listen しない**（`New` が `nil` を返す。設計 5-2）。
	// `0` を渡すと OS が空きポートを選ぶ（実際の番号は `Addr` で取れる）。
	Port *int
	// Source は実行中の run の写しの供給元である。Port が nil でなければ必須。
	Source RunSource
	// Logger はログの出力先である。nil なら slog.Default() を使う。
	Logger *slog.Logger
	// Now は現在時刻を返す関数である。nil なら time.Now を使う。
	Now func() time.Time
}

// Server は HTTP ダッシュボードの listener と応答の組み立てを持つ。
type Server struct {
	// port は `New` の時点で写し取ったポート番号である。
	// **設定への参照は持たない**（起動後に `server.port` を書き換えても、
	// ここの値は変わらないことを型で保証する。設計 3-24）。
	port   int
	source RunSource
	logger *slog.Logger
	now    func() time.Time
	http   *http.Server

	// mu は ln と closed を守る。
	mu     sync.Mutex
	ln     net.Listener
	closed bool
	// wg は Serve の goroutine を数える。
	wg sync.WaitGroup
}

// New はダッシュボードを組み立てる。**この時点では listen しない**（`Start` が行う）。
//
// **`opts.Port` が `nil` なら `(nil, nil)` を返す**（設計 5-2。任意の機能である）。
// 呼び出す側は戻り値の nil を「起動しない」と読むこと。
//
// opts: ポート番号・run の供給元・ログの出力先。
// 戻り値の1つ目: 組み立てた Server。Port が nil なら nil。
// 戻り値の2つ目: ポート番号が範囲外の場合と、Source が nil の場合のエラー。
func New(opts Options) (*Server, error) {
	if opts.Port == nil {
		// **`server.port` が null。listen しない。**
		return nil, nil
	}
	port := *opts.Port
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("server.port が範囲外です（0以上65535以下にすること）: %d", port)
	}
	if opts.Source == nil {
		return nil, errors.New("ダッシュボードに run の供給元が渡されていません")
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	s := &Server{port: port, source: opts.Source, logger: logger, now: now}
	// **期限を4つとも埋める。**このサーバは認証を持たないので、同じマシンの
	// どのプロセスからでも接続できる。1本の接続で goroutine を握られ続けないようにする。
	s.http = &http.Server{
		Handler:           s.newMux(),
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
		ReadTimeout:       DefaultReadTimeout,
		WriteTimeout:      DefaultWriteTimeout,
		IdleTimeout:       DefaultIdleTimeout,
		MaxHeaderBytes:    DefaultMaxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
	return s, nil
}

// Start は待ち受けを始める。**127.0.0.1 にしか bind しない**（`LoopbackHost`）。
//
// **この関数は待たない。**listen に成功したら応答の goroutine を起こして返る。
//
// **2回呼んではならない。**2本目は listen せずにエラーを返す。`server.port` が 0 のとき、
// 2本目を素通しにすると別のポートで待ち受けてしまい、**ログに出した待ち受け先と
// 実際に開いているポートが食い違う。**
//
// 戻り値: listen できなかった場合のエラー（ポートの重複など）。
// **エラーを返した場合、goroutine は1つも残らない。**
func (s *Server) Start() error {
	addr := net.JoinHostPort(LoopbackHost, strconv.Itoa(s.port))
	// **`tcp` + 127.0.0.1 なので IPv4 のループバックだけに bind する。**
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("ダッシュボードの待ち受けを開始できません（%s）: %w", addr, err)
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = ln.Close()
		return errors.New("ダッシュボードは既に閉じられています")
	}
	if s.ln != nil {
		s.mu.Unlock()
		_ = ln.Close()
		return fmt.Errorf("ダッシュボードは既に %s で待ち受けています", s.Addr())
	}
	s.ln = ln
	s.wg.Add(1)
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Warn("ダッシュボードの応答を続けられません", "error", err)
		}
	}()

	s.logger.Info("ダッシュボードを開きました", "addr", s.Addr())
	return nil
}

// Addr は実際に待ち受けているアドレスを返す。
//
// **`server.port` に 0 を指定した場合、OS が選んだ番号はここでしか分からない。**
//
// 戻り値: `127.0.0.1:<ポート>`。**まだ Start していない場合と、Close したあとは空文字。**
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Close は待ち受けを閉じ、処理中の応答が終わるのを待つ。
//
// **`nil` レシーバでも安全である**（`New` が返した nil をそのまま渡してよい）。
//
// ctx: 処理中の応答を待つ上限。**期限を持つものを渡すこと**（`DefaultShutdownTimeout` が
// その目安である）。期限が無いと、応答が返らない相手を待って終了が止まる。
// 戻り値: 閉じられなかった場合のエラー。
func (s *Server) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	listening := s.ln != nil
	// **閉じたら Addr は待ち受け先を返さない。**閉じたあとも古いアドレスを返すと、
	// もう繋がらない宛先を人間にもログにも見せることになる。
	s.ln = nil
	s.mu.Unlock()

	if !listening {
		return nil
	}

	err := s.http.Shutdown(ctx)
	s.wg.Wait()
	if err != nil {
		return fmt.Errorf("ダッシュボードを閉じられません: %w", err)
	}
	s.logger.Info("ダッシュボードを閉じました")
	return nil
}

// ResponseTimeouts は応答に掛けている期限を返す。
//
// **`test/internal/server` から「期限が抜けていないか」を確かめるために公開している。**
// 値は `New` が決めるもので、外から変えられない。
//
// 戻り値の1つ目: ヘッダを読み切るまでの上限。
// 戻り値の2つ目: 本文まで読み切るまでの上限。
// 戻り値の3つ目: 応答を書き終えるまでの上限。
// 戻り値の4つ目: keep-alive の接続を次の要求まで待つ上限。
func (s *Server) ResponseTimeouts() (readHeader, read, write, idle time.Duration) {
	return s.http.ReadHeaderTimeout, s.http.ReadTimeout, s.http.WriteTimeout, s.http.IdleTimeout
}

// Handler は応答の組み立てだけを返す（listen を伴わない）。
//
// **テストが `httptest` から直接叩くためのものである。**
//
// 戻り値: 経路を張ったハンドラ。
func (s *Server) Handler() http.Handler {
	return s.http.Handler
}

// newMux は経路を張る。
//
// **GET（と、その裏返しの HEAD）しか受けない。**`net/http` の ServeMux は
// メソッド付きのパターンに一致しないリクエストへ 405 を返すので、
// **POST / PUT / DELETE はハンドラまで届かない。**書き込みの経路は存在しない。
//
// 経路は2本である（`SPEC.md` 13.7.1 / 13.7.2）。
//
//	GET /               人間が読む HTML（13.7.1）
//	GET /api/v1/state   いまの状態の JSON（13.7.2 が最低限として挙げるもの）
//
// 戻り値: 経路を張ったハンドラ。
func (s *Server) newMux() http.Handler {
	mux := http.NewServeMux()
	// `{$}` は「そのパスちょうど」を意味する。これが無いと `/` が全部の経路を飲み込む。
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET "+APIStatePath, s.handleAPIState)
	// **安全側のヘッダは外側で付ける。**断った応答（421）にも同じヘッダを載せる。
	return withSafetyHeaders(s.withHostCheck(mux))
}

// handleIndex は実行中の run の一覧を HTML で返す。
//
// **値の埋め込みは `html/template` に任せる**（テンプレートの外で文字列を組み立てない）。
// issue のタイトルは外部から来る文字列なので、必ずエスケープされる経路を通す。
//
// w: 応答の書き出し先。
// r: 受け取ったリクエスト。
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	snap := s.snapshot()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTemplate.Execute(w, snap); err != nil {
		// **ここでステータスコードは変えられない**（本文を書き始めているため）。
		// 記録だけ残す。
		s.logger.Warn("ダッシュボードの HTML を書き出せません", "error", err, "path", r.URL.Path)
	}
}

// handleAPIState は実行中の run の一覧を JSON で返す（`SPEC.md` 13.7.2 の `GET /api/v1/state`）。
//
// **読み取り専用である。**
//
// w: 応答の書き出し先。
// r: 受け取ったリクエスト。
func (s *Server) handleAPIState(w http.ResponseWriter, r *http.Request) {
	snap := s.snapshot()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := writeJSON(w, snap); err != nil {
		s.logger.Warn("ダッシュボードの JSON を書き出せません", "error", err, "path", r.URL.Path)
	}
}

// snapshot は run の写しを取り、表示用の形に組み替える。
//
// 戻り値: 表示用のスナップショット。
func (s *Server) snapshot() Snapshot {
	return NewSnapshot(s.source.RunViews(), s.now())
}

// withHostCheck は `Host` ヘッダが手元のダッシュボードを指していないリクエストを落とす。
//
// **DNS rebinding を塞ぐためである。**127.0.0.1 に bind するだけでは塞がらない。
// 攻撃者が自分のドメインを 127.0.0.1 へ解決させると、そのページから見て**同一オリジンに
// なるので CORS では止められず、**issue の識別子・タイトル・URL・Status・トークンの消費が
// そのまま読み出される。**ブラウザは `Host` に元のドメイン名を送る**ので、そこで落とせる。
//
// **ホスト名だけを見て、ポート番号は listen 中のときだけ照合する。**`Handler` を
// 直接叩く経路（テスト）では待ち受けていないため、そこでポートを要求すると
// 実物と関係のない値を書かせることになる。ホスト名の検査だけで rebinding は塞がる。
//
// next: 実際の応答を組み立てるハンドラ。
// 戻り値: 検査してから next を呼ぶハンドラ。
func (s *Server) withHostCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allowedHost(r.Host) {
			s.logger.Warn("ループバック以外の宛先で要求が来たので断りました",
				"host", r.Host, "path", r.URL.Path)
			// 421 は「この宛先の要求をこのサーバは扱えない」という意味である。
			http.Error(w, "このダッシュボードは 127.0.0.1 からしか使えません\n", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// allowedHost は `Host` ヘッダが手元のダッシュボードを指しているかを判定する。
//
// host: リクエストの `Host` ヘッダ（`127.0.0.1:8787` や `localhost` の形）。
// 戻り値: ホスト名がループバックで、ポートも食い違っていなければ true。
func (s *Server) allowedHost(host string) bool {
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		// ポートが書かれていない形（`localhost`）。ホスト名だけを見る。
		name, port = host, ""
	}
	if !strings.EqualFold(name, "localhost") {
		ip := net.ParseIP(name)
		if ip == nil || !ip.IsLoopback() {
			return false
		}
	}
	if port == "" {
		return true
	}
	// 待ち受けているときだけ、要求された宛先のポートが自分のものかを確かめる。
	if _, listening, err := net.SplitHostPort(s.Addr()); err == nil {
		return port == listening
	}
	return true
}

// withSafetyHeaders は応答に安全側のヘッダを足す。
//
// **script を一切読み込ませない**（`default-src 'none'`）。ダッシュボードは
// 静的な HTML と inline の CSS だけで出来ており、外部への通信を必要としない。
//
// **`frame-ancestors 'none'` を必ず書く。**この指令は `default-src` に落ちてこないので、
// 書かないと他のページの iframe に埋め込める。
//
// next: 実際の応答を組み立てるハンドラ。
// 戻り値: ヘッダを足すハンドラ。
func withSafetyHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'none'; style-src 'unsafe-inline'; form-action 'none'; base-uri 'none'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// **中身は run の実況なので、途中の proxy にも履歴にも残させない。**
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
