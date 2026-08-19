package hookserver

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/maimuzo/continuo/internal/socketpath"
)

// 既定の待ち時間と上限である。設定から上書きできるよう Options に持たせてある。
const (
	// DefaultReadTimeout は1つの接続から1行を読み切るまでの上限である。
	// continuo hook は「1行書いて閉じる」だけなので（設計 3-2）、これを超える接続は
	// 相手が書き終えられていない。放置すると goroutine と file descriptor が溜まるので切る。
	//
	// **この長さが後続の hook の配送を止めることは無い。**配送の順番を揃える待ち合わせには
	// DefaultOrderWait という別の（ずっと短い）上限があるためである（enqueueInOrder を参照）。
	DefaultReadTimeout = 10 * time.Second

	// DefaultOrderWait は「自分より前に受け付けた接続がキューへ積み終わる」のを待つ上限である。
	//
	// **settle_ms（既定2000ms。設計 3-2）より十分小さくしなければならない。**
	// turn の終わりの判定は「idle/done で返ったら Stop hook が来ているかを確かめ、
	// 来ていなければ settle_ms のあいだ待つ。それでも来なければ stall として扱う」である。
	// 順番待ちで Stop の配送が settle_ms より長く遅れると、正常に終わった turn が
	// stall と誤判定される。
	//
	// 200ms にする根拠。順番が入れ替わって困るのは、Stop の 0.033〜0.037 秒後に来る
	// <task-notification> だけである（設計 1-3 の実測 8/8）。その窓の5倍以上を取りつつ、
	// settle_ms の 1/10 に収める。この上限を超えたら順番を揃えるのは諦め、警告を出して積む。
	DefaultOrderWait = 200 * time.Millisecond

	// DefaultMaxMessageBytes は1件の hook の JSON として受け付ける最大バイト数である。
	//
	// **書く側（internal/hookclient の DefaultMaxInputBytes。8 MiB）より必ず大きくする。**
	// 同じ値にすると、hookclient が「送れる」と判断した上限ちょうどの1行を
	// bufio.Scanner が ErrTooLong で捨ててしまい、hook がどこにも残らずに消える。
	// 受け口のほうを広く取ることで、hookclient が通したものは必ず受け取れる。
	DefaultMaxMessageBytes = 16 << 20

	// DefaultMaxEventsPerConn は1つの接続から受け付ける hook の最大件数である。
	//
	// 設計 3-2 の約束は「1コネクション1メッセージ・応答なし」なので、本来は1件で足りる。
	// 16 にしているのは、書く側が将来まとめて書く形に変わっても取りこぼさないための余裕である。
	//
	// **上限に達したら、その接続の読み取りをそこで打ち切り、そこまでに読めた分は必ずキューへ積む。**
	// 読めた分を捨てる形にすると、turn の終わりを知らせる Stop を落としかねない
	// （落とすと stall_timeout_ms（既定30分）まで誰も気づかない。設計 3-19）。
	DefaultMaxEventsPerConn = 16

	// DefaultMaxConnBytes は1つの接続から読む累計バイト数の上限である。
	//
	// 上限が無いと、同じ利用者のプロセスが1接続で延々と行を流し込むだけでメモリを使い切れる
	// （読み取りの期限は既定10秒あり、その間ずっと貯め続けてしまう）。
	// 1件の上限（MaxMessageBytes）と同じ値にしてあるので、**上限ちょうどの hook 1件は必ず通る。**
	// MaxMessageBytes を大きくした場合は New が自動でこちらも引き上げる。
	DefaultMaxConnBytes = DefaultMaxMessageBytes

	// DefaultMaxConcurrentConns は同時に読み取る接続の最大数である。
	//
	// accept ごとに goroutine を作るだけだと、並列に張られた接続の数だけ
	// 上の2つの上限が掛け算になり、file descriptor も枯渇する。
	// **上限に達しても接続を閉じない。**accept を止めて待たせるだけにする。
	// 閉じると、書く側は「書けた」と思ったまま hook が消える。
	// 待たせるだけなら、書く側の接続の待ち（hookclient.DefaultDialTimeout。2秒）の中で繋がる。
	DefaultMaxConcurrentConns = 32

	// DefaultMaxQueueEvents は配送待ちのキューに積んでおける最大件数である。
	//
	// 配送先（orchestrator）が詰まっている間も socket からは届き続けるので、上限が無いと
	// キューがメモリを使い切る。上限を超えたときに落とすのは**古いほうである。**
	// turn の終わりの判定に要るのは最新の Stop なので、新しいほうを残す。
	DefaultMaxQueueEvents = 10000

	// initialScanBufferBytes は1行を読む bufio.Scanner の初期バッファの大きさである。
	// 実測の hook は数 KiB なので、ほとんどの接続はこの大きさのまま読み切れる。
	initialScanBufferBytes = 64 * 1024

	// acceptErrorBackoff は Accept が「リスナーが閉じられた」以外の理由で失敗したときに
	// 待つ時間である。file descriptor の枯渇などで失敗し続けるとき、待たずに繰り返すと
	// CPU を使い切ってしまう。
	acceptErrorBackoff = 100 * time.Millisecond
)

// Options は Server を作るときの入力である。
type Options struct {
	// SocketPath は hook を受ける Unix domain socket の絶対パスである。
	// 第1段階の socketpath.ResolveHookSocketPath が解決したものを渡すこと。
	// 実行時ディレクトリ（逃がし先の探索の起点）は filepath.Dir(SocketPath) である（設計 3-23）。
	SocketPath string
	// Sink は受け取った hook の届け先である（orchestrator が実装する）。必須。
	Sink HookSink
	// Logger は警告・情報の出力先である。必須（無人運用で警告を捨てないため）。
	Logger *slog.Logger
	// ReadTimeout は1接続から1行を読み切るまでの上限である。0 なら DefaultReadTimeout。
	//
	// **`claude.read_timeout_ms` を流用してはならない。**あちらは herdr の socket API の
	// 応答を待つ上限であり（設計 8-1「read_timeout_ms 一本ですべてを打ち切ってはならない」）、
	// 相手が違う。herdr が遅い環境に合わせてあちらを伸ばすと、hook の接続を掴んだままにする
	// 時間まで一緒に伸びる。設定から変えたくなったら専用のキーを立てること。
	ReadTimeout time.Duration
	// OrderWait は配送の順番を揃えるために先行の接続を待つ上限である。
	// 0 なら DefaultOrderWait。**settle_ms より十分小さい値にすること**（設計 3-2）。
	OrderWait time.Duration
	// MaxMessageBytes は1件の hook の JSON として受け付ける最大バイト数である。
	// 0 なら DefaultMaxMessageBytes。
	MaxMessageBytes int
	// MaxEventsPerConn は1つの接続から受け付ける hook の最大件数である。
	// 0 なら DefaultMaxEventsPerConn。
	MaxEventsPerConn int
	// MaxConnBytes は1つの接続から読む累計バイト数の上限である。
	// 0 なら DefaultMaxConnBytes。MaxMessageBytes を下回る値を渡した場合は
	// MaxMessageBytes まで引き上げる（上限ちょうどの hook 1件を必ず通すため）。
	MaxConnBytes int
	// MaxConcurrentConns は同時に読み取る接続の最大数である。
	// 0 なら DefaultMaxConcurrentConns。
	MaxConcurrentConns int
	// MaxQueueEvents は配送待ちのキューに積んでおける最大件数である。
	// 0 なら DefaultMaxQueueEvents。
	MaxQueueEvents int
}

// Server は hook を受ける socket と、配送待ちのキューを持つ。
//
// 溜める → 逃がし先を読み戻してキューの先頭へ積む → 索引ができたら流す、の順で使う
// （設計 3-4 の段5d / 5e / 6b）。listen を後回しにすると、読み戻しから listen までの窓に
// 落ちた hook を誰も読まないので、Start は必ず ReplayPending より先に呼ぶこと。
type Server struct {
	socketPath       string
	runtimeDir       string
	sink             HookSink
	logger           *slog.Logger
	readTimeout      time.Duration
	orderWait        time.Duration
	maxMessageBytes  int
	maxEventsPerConn int
	maxConnBytes     int
	maxQueueEvents   int

	// connSlots は同時に読み取る接続の数を抑える札である。accept の前に1つ取り、
	// その接続を読み終えたら返す。**札が無いときは accept を止めて待つ**（接続は閉じない）。
	connSlots chan struct{}
	// done は Close で閉じる。札待ちで止まっている accept ループを起こすために使う
	// （sync.Cond では accept ループの select と混ぜられない）。
	done chan struct{}

	// mu は下のすべてのフィールドを守る。cond と seqCond は mu に紐づく。
	mu         sync.Mutex
	cond       *sync.Cond
	seqCond    *sync.Cond
	ln         net.Listener
	queue      []HookEvent
	delivering bool
	closed     bool

	// conns は accept 済みでまだ読み取り中の接続である。
	// Close がここを一斉に閉じないと、handleConn が読み取りの期限（既定10秒）まで
	// 戻らず、wg.Wait がその間ずっと返らない（continuo の終了・再起動が止まる）。
	conns map[net.Conn]struct{}

	// replayed は ReplayPending（段5e の1回目の走査）が読み戻した hook である。
	// **キューへ積むのは StartDelivery（段6b）である。**2回目の走査を配送の直前に行い、
	// 「1回目の走査を始めてから配送を始めるまでの間に書かれたもの」まで拾うためである。
	replayed []HookEvent

	// acceptSeq は接続を受け付けた順に振る通し番号である（accept ループだけが増やす）。
	acceptSeq uint64
	// enqueueSeq は次にキューへ積んでよい接続の通し番号である。
	// 接続ごとの goroutine は自分の番が来るまで待ってから積む（順番を保つため）。
	enqueueSeq uint64

	// wg は accept ループ・接続ごとの goroutine・配送 goroutine の全部を数える。
	// **Add は必ず mu を持ったまま、closed が false であることを確かめてから行う。**
	// Close は closed を立ててから wg.Wait を呼ぶので、この順序を守らないと
	// カウンタが 0 の状態で Add と Wait が競合する（sync.WaitGroup の誤用になる）。
	wg sync.WaitGroup
}

// New は Server を組み立てる。socket の作成はまだ行わない（Start が行う）。
//
// opts: 上記 Options。SocketPath は絶対パス、Sink と Logger は非 nil であること。
// 戻り値: 組み立てた *Server。次の場合はエラーを返す。
//   - SocketPath が空、または絶対パスでない（実行時ディレクトリを決められないため）
//   - Sink が nil（受け取った hook の届け先が無い）
//   - Logger が nil（警告を捨てる実装にしないため）
func New(opts Options) (*Server, error) {
	if opts.SocketPath == "" {
		return nil, errors.New("hook を受ける socket のパスが空です（socketpath.ResolveHookSocketPath が解決したパスを渡してください）")
	}
	if !filepath.IsAbs(opts.SocketPath) {
		return nil, fmt.Errorf(
			"hook を受ける socket のパス %q が絶対パスではありません"+
				"（実行時ディレクトリを filepath.Dir で決めるため、相対パスでは逃がし先の場所が定まらない）",
			opts.SocketPath,
		)
	}
	if opts.Sink == nil {
		return nil, errors.New("hook の届け先（HookSink）が nil です")
	}
	if opts.Logger == nil {
		return nil, errors.New("ログの出力先（*slog.Logger）が nil です")
	}

	s := &Server{
		socketPath:       opts.SocketPath,
		runtimeDir:       filepath.Dir(opts.SocketPath),
		sink:             opts.Sink,
		logger:           opts.Logger,
		readTimeout:      opts.ReadTimeout,
		orderWait:        opts.OrderWait,
		maxMessageBytes:  opts.MaxMessageBytes,
		maxEventsPerConn: opts.MaxEventsPerConn,
		maxConnBytes:     opts.MaxConnBytes,
		maxQueueEvents:   opts.MaxQueueEvents,
		conns:            map[net.Conn]struct{}{},
		done:             make(chan struct{}),
	}
	if s.readTimeout <= 0 {
		s.readTimeout = DefaultReadTimeout
	}
	if s.orderWait <= 0 {
		s.orderWait = DefaultOrderWait
	}
	if s.maxMessageBytes <= 0 {
		s.maxMessageBytes = DefaultMaxMessageBytes
	}
	if s.maxEventsPerConn <= 0 {
		s.maxEventsPerConn = DefaultMaxEventsPerConn
	}
	if s.maxConnBytes <= 0 {
		s.maxConnBytes = DefaultMaxConnBytes
	}
	// 1件の上限を下回っていると、上限ちょうどの hook 1件が累計の上限に引っかかって
	// 途中で切られる。受け口が「通す」と決めた大きさは必ず通す。
	if s.maxConnBytes < s.maxMessageBytes {
		s.maxConnBytes = s.maxMessageBytes
	}
	if s.maxQueueEvents <= 0 {
		s.maxQueueEvents = DefaultMaxQueueEvents
	}
	maxConns := opts.MaxConcurrentConns
	if maxConns <= 0 {
		maxConns = DefaultMaxConcurrentConns
	}
	s.connSlots = make(chan struct{}, maxConns)
	s.cond = sync.NewCond(&s.mu)
	s.seqCond = sync.NewCond(&s.mu)
	return s, nil
}

// RuntimeDir は実行時ディレクトリ（filepath.Dir(socket のパス)）を返す（設計 3-23）。
// 逃がし先も issue ごとの設定ファイルも、すべてこのディレクトリの下にある。
func (s *Server) RuntimeDir() string { return s.runtimeDir }

// QueueLen は配送待ちのキューに積まれている hook の件数を返す。
//
// 溜める段（Start のあと、StartDelivery の前）では、届いた hook がここに積み上がる。
// 配送が始まってからは「配送が詰まっていないか」を見るために使う。
//
// 戻り値: 配送待ちの件数。
func (s *Server) QueueLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

// Start は socket を作って listen を始める（復元の段5d）。
//
// 届いた hook は内部のキューに溜めるだけで、この時点では HookSink へ渡さない。
// 配送を始めるのは StartDelivery である（段6b）。
//
// 戻り値: 次の場合にエラーを返す。
//   - socket を置くディレクトリを 0700 で用意できない
//   - 同じパスで既に別のプロセスが listen している（二重起動。flock で防いでいるが念のため見る）
//   - bind に失敗した（パスが長すぎる場合を含む。上限は socketpath.MaxPathLen）
//   - listen を始める途中で Close された（作った socket は片付けてから返す）
func (s *Server) Start() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("既に Close された hookserver は listen を始められません")
	}
	if s.ln != nil {
		s.mu.Unlock()
		return errors.New("hookserver は既に listen しています")
	}
	s.mu.Unlock()

	if err := socketpath.EnsureDir(s.runtimeDir); err != nil {
		return err
	}
	if err := s.removeStaleSocketFile(); err != nil {
		return err
	}

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("hook を受ける socket を listen できません: %s: %w", s.socketPath, err)
	}
	// ディレクトリの 0700 が本体の防御だが（設計 3-23）、Go が作る socket の権限は
	// umask 次第で 0755 になるので、socket 側も 0600 に落としておく（二重の防御）。
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		s.logger.Warn("hook を受ける socket の権限を 0600 に設定できませんでした（ディレクトリの 0700 で防御は続く）",
			"socket", s.socketPath, "error", err)
	}

	s.mu.Lock()
	// **ここで closed を見直す。**上の EnsureDir → net.Listen → Chmod の間はロックを
	// 持っていないので、その間に Close が走ると Close は ln == nil のまま wg.Wait を
	// 返してしまう。そのまま acceptLoop を起こすと、誰も閉じない listener と goroutine が
	// 残り、socket ファイルも消えない。
	if s.closed {
		s.mu.Unlock()
		_ = ln.Close()
		if err := os.Remove(s.socketPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			s.logger.Warn("listen を始める途中で Close されたため作った socket を消そうとしましたが、消せませんでした",
				"socket", s.socketPath, "error", err)
		}
		return errors.New("listen を始める途中で Close されたので、作った socket を片付けました")
	}
	s.ln = ln
	s.wg.Add(1)
	s.mu.Unlock()

	go s.acceptLoop(ln)

	s.logger.Info("hook を受ける socket の listen を始めました", "socket", s.socketPath)
	return nil
}

// removeStaleSocketFile は前回の実行が残した socket ファイルを片付ける。
//
// listen 中のプロセスが本当に居るかどうかは、接続してみて判断する。繋がったら
// 別のプロセスが生きているのでエラーにし、繋がらなければ残骸なので消す。
//
// 戻り値: 別のプロセスが listen していた場合と、残骸を消せなかった場合にエラーを返す。
// ファイルがそもそも無い場合は nil を返す。
func (s *Server) removeStaleSocketFile() error {
	if _, err := os.Lstat(s.socketPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("hook を受ける socket のパスを調べられません: %s: %w", s.socketPath, err)
	}

	conn, err := net.DialTimeout("unix", s.socketPath, s.readTimeout)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf(
			"hook を受ける socket %s には既に別のプロセスが listen しています（continuo の二重起動の可能性があります）",
			s.socketPath,
		)
	}

	if err := os.Remove(s.socketPath); err != nil {
		return fmt.Errorf("前回の実行が残した socket ファイルを消せません: %s: %w", s.socketPath, err)
	}
	s.logger.Info("前回の実行が残した socket ファイルを消しました（誰も listen していませんでした）",
		"socket", s.socketPath)
	return nil
}

// acceptLoop は接続を受け付け続ける。Close でリスナーが閉じられると終わる。
//
// **accept の前に札（connSlots）を取る。**同時に読み取る接続の数を抑えるためである。
// 札が無いときは accept そのものを止めるので、余った接続は OS の待ち行列に積まれたまま残る。
// 受け付けてから閉じる形にすると、書く側は「書けた」と思ったまま hook が消える。
func (s *Server) acceptLoop(ln net.Listener) {
	defer s.wg.Done()
	for {
		select {
		case s.connSlots <- struct{}{}:
		case <-s.done:
			return
		}

		conn, err := ln.Accept()
		if err != nil {
			<-s.connSlots
			if errors.Is(err, net.ErrClosed) || s.isClosed() {
				return
			}
			// file descriptor の枯渇などで失敗し続ける場合に、CPU を使い切らないよう待つ。
			s.logger.Warn("hook の接続を受け付けられませんでした", "socket", s.socketPath, "error", err)
			time.Sleep(acceptErrorBackoff)
			continue
		}
		// 通し番号は accept の順に振る。読み取りは接続ごとに並行して行うが、
		// キューへ積むのはこの順番に揃える（handleConn を参照）。
		seq, ok := s.registerConn(conn)
		if !ok {
			// Close 済み。この接続は誰も読まないので、その場で閉じる。
			<-s.connSlots
			_ = conn.Close()
			return
		}

		go func(c net.Conn, n uint64) {
			defer s.wg.Done()
			defer func() { <-s.connSlots }()
			s.handleConn(c, n)
		}(conn, seq)
	}
}

// registerConn は accept した接続を追跡表へ入れ、通し番号を振り、wg を1つ増やす。
//
// **wg.Add と closed の確認を1つのロックの中で行う。**別々にすると、Close が
// wg.Wait を返したあとに Add が走りうる（sync.WaitGroup の誤用になる）。
//
// conn: accept した接続。
// 戻り値: 振った通し番号と、受け付けてよいかどうか。Close 済みなら false を返す
// （呼び出し側がその場で接続を閉じる）。
func (s *Server) registerConn(conn net.Conn) (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, false
	}
	seq := s.acceptSeq
	s.acceptSeq++
	s.conns[conn] = struct{}{}
	s.wg.Add(1)
	return seq, true
}

// unregisterConn は読み終えた接続を追跡表から外す。
//
// conn: 追跡表から外す接続。
func (s *Server) unregisterConn(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, conn)
}

// handleConn は1つの接続から改行区切り JSON を読み、解釈できたものをキューへ積む。
//
// 設計 3-2 の約束は「1コネクション1メッセージ・応答なし」である。応答は書かない。
// 1接続に複数行が来た場合も、行ごとに1件として扱う（捨てない）。
//
// **読み取りは接続ごとに並行して行い、キューへ積むのは接続を受け付けた順に揃える。**
// 順番が入れ替わると、`Stop` の 0.033〜0.037 秒後に来る `<task-notification>` が
// `Stop` より先に配送されうる（設計 1-3 の判定が成立しなくなる）。
// 並行に読むのは、書き終えない接続が1つあっても後続の hook を待たせないためである。
//
// **1接続あたりの件数（maxEventsPerConn）と累計バイト数（maxConnBytes）に上限がある。**
// 上限に達したらそこで読み取りを打ち切り、**そこまでに読めた分は必ずキューへ積む。**
// 打ち切ったことは警告としてログに残す。読めた分ごと捨てる形にすると、turn の終わりを
// 知らせる Stop を落としかねない（落とすと stall_timeout_ms まで誰も気づかない）。
//
// conn: 受け付けた接続。この関数が閉じる。
// seq: accept の順に振られた通し番号。
func (s *Server) handleConn(conn net.Conn, seq uint64) {
	defer func() {
		s.unregisterConn(conn)
		_ = conn.Close()
	}()

	if err := conn.SetReadDeadline(time.Now().Add(s.readTimeout)); err != nil {
		s.logger.Warn("hook の接続に読み取りの期限を設定できませんでした", "error", err)
	}

	var (
		events    []HookEvent
		readBytes int
		// truncatedReason は上限に達して読み取りを打ち切った理由である（空なら打ち切っていない）。
		truncatedReason string
	)
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, initialScanBufferBytes), s.maxMessageBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		// 改行の1バイトも数える。数えないと、空行だけを延々と流されたときに上限が効かない。
		readBytes += len(line) + 1
		if len(line) > 0 {
			ev, err := decodeEvent(line)
			if err != nil {
				s.logger.Warn("socket に届いた行を hook の JSON として解釈できませんでした（捨てます）",
					"error", err, "bytes", len(line))
			} else {
				events = append(events, ev)
			}
		}
		// **上限の判定は、いま読んだ行を処理し終えてから行う。**先に判定すると、
		// ちょうど上限に届いた1件を読み捨てることになる。
		if len(events) >= s.maxEventsPerConn {
			truncatedReason = "1接続あたりの件数の上限に達しました"
			break
		}
		if readBytes >= s.maxConnBytes {
			truncatedReason = "1接続あたりの累計バイト数の上限に達しました"
			break
		}
	}
	if truncatedReason != "" {
		s.logger.Warn("hook の接続の読み取りを上限で打ち切りました（ここまでに読めた分は配送します）",
			"reason", truncatedReason,
			"events", len(events),
			"read_bytes", readBytes,
			"max_events_per_conn", s.maxEventsPerConn,
			"max_conn_bytes", s.maxConnBytes)
	}
	if err := scanner.Err(); err != nil {
		switch {
		case errors.Is(err, net.ErrClosed):
			// Close が接続を畳んだだけである。畳まないと読み取りの期限まで戻らない。
		case errors.Is(err, bufio.ErrTooLong):
			s.logger.Warn("hook の1行が受け口の上限を超えたので読めませんでした（この hook は失われます）",
				"max_message_bytes", s.maxMessageBytes, "error", err)
		default:
			s.logger.Warn("hook の接続からの読み取りに失敗しました", "error", err)
		}
	}
	s.enqueueInOrder(seq, events)
}

// decodeEvent は hook の JSON 1件分を HookEvent へ変換する。
//
// data: hook の JSON 1件分のバイト列。
// 戻り値: 変換した HookEvent と、JSON として解釈できなかった場合のエラー。
// JSON のオブジェクト以外（配列・数値・null など）もエラーとして扱う。
func decodeEvent(data []byte) (HookEvent, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return HookEvent{}, err
	}
	if probe == nil {
		return HookEvent{}, errors.New("JSON のオブジェクトではありません（null でした）")
	}
	var ev HookEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return HookEvent{}, err
	}
	return ev, nil
}

// enqueueInOrder は、自分より前に受け付けた接続がキューへ積み終わるのを待ってから、
// 配送待ちのキューの末尾へ積む。Close 後は捨てる。
//
// **待つ上限は orderWait（既定 200ms）である。読み取りの期限（既定10秒）ではない。**
// 1バイトも書かずに固まった接続が先にあると、後続の hook がその接続の読み取りの期限まで
// 配送されない。それが settle_ms（既定2000ms。設計 3-2）を超えると、正常に終わった turn が
// stall と誤判定される。上限に達したら順番を揃えるのは諦め、警告を出して積む。
//
// **待つのは「自分より前の番号がまだ積み終わっていない」ときだけである。**
// 既に追い越されている（s.enqueueSeq > seq）場合は、待っても自分の番号はもう来ない。
// そこで待つと、その分だけ配送が遅れるだけである（遅れたのが Stop なら turn の
// 終わりの検知がそのまま遅れる）。
//
// seq: この接続の通し番号（accept の順）。
// evs: この接続から読み取れたイベント（0件のこともある）。
func (s *Server) enqueueInOrder(seq uint64, evs []HookEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.enqueueSeq < seq && !s.closed {
		// sync.Cond には期限が無いので、期限が来たら起こす時計を別に仕掛ける。
		deadline := time.Now().Add(s.orderWait)
		timer := time.AfterFunc(s.orderWait, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.seqCond.Broadcast()
		})
		for s.enqueueSeq < seq && !s.closed && time.Now().Before(deadline) {
			s.seqCond.Wait()
		}
		timer.Stop()
	}

	if s.closed {
		return
	}
	if s.enqueueSeq < seq {
		s.logger.Warn("先に受け付けた hook の接続が読み終わらないので、配送の順番を揃えるのを諦めました",
			"seq", seq, "waiting_for", s.enqueueSeq, "order_wait", s.orderWait)
	}

	s.queue = append(s.queue, evs...)
	s.trimQueueLocked()
	// 諦めた場合も含め、次に待つ番号を自分の次まで進める。
	// 進めないと、遅れて戻ってきた先行の接続を後続がもう一度待つことになる。
	if seq >= s.enqueueSeq {
		s.enqueueSeq = seq + 1
	}
	s.seqCond.Broadcast()
	s.cond.Signal()
}

// prependQueue は配送待ちのキューの先頭へ、渡された順のまま積む。
// 逃がし先から読み戻したものを、socket で届いた分より先に流すために使う（設計 3-4 の段5e）。
func (s *Server) prependQueue(evs []HookEvent) {
	if len(evs) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	merged := make([]HookEvent, 0, len(evs)+len(s.queue))
	merged = append(merged, evs...)
	merged = append(merged, s.queue...)
	s.queue = merged
	s.trimQueueLocked()
	s.cond.Broadcast()
}

// trimQueueLocked は配送待ちのキューが上限を超えていたら、**古いほうから**落とす。
//
// 落とすほうを新しい側にしないのは、turn の終わりの判定に要るのが最新の Stop だからである。
// 落としたことは件数つきで警告に出す（無人運用なので、黙って減らすと誰も気づかない）。
//
// **呼び出し側が s.mu を持っていること。**
func (s *Server) trimQueueLocked() {
	if s.maxQueueEvents <= 0 || len(s.queue) <= s.maxQueueEvents {
		return
	}
	over := len(s.queue) - s.maxQueueEvents
	// 元の配列を握り続けないよう、残す分だけを新しいスライスへ写す。
	kept := make([]HookEvent, s.maxQueueEvents)
	copy(kept, s.queue[over:])
	s.queue = kept
	s.logger.Warn("配送待ちのキューが上限を超えたので、古いほうから落としました"+
		"（配送先が詰まっています。落とした hook は取り戻せません）",
		"dropped", over, "max_queue_events", s.maxQueueEvents)
}

// StartDelivery はキューから HookSink.OnHook への配送を始める（復元の段6b）。
//
// 配送を始める直前に、逃がし先をもう一度走査する（段5e の2回目の走査）。
// **1回目の走査を始めてから配送を始めるまでの間に continuo hook が書いたものを、
// ここで拾うためである**（設計 3-4）。2回目を ReplayPending の中で終わらせると、
// ReplayPending が返ってから StartDelivery が呼ばれるまでの窓（段6 の索引作り）が覆われず、
// その間に書かれた hook は次の再起動まで配送されない。
//
// 溜めた分を受信時刻の昇順に流し、そのあと新しく届く分をそのまま流す。
// 索引に無い session_id（OnHook が false を返したもの）は、警告をログに出して捨てる。
// 2回目以降の呼び出しは何もしない。Close 済みなら何もしない。
func (s *Server) StartDelivery() {
	s.mu.Lock()
	if s.closed || s.delivering {
		s.mu.Unlock()
		return
	}
	// ここで立てておくと、この先の走査の間に ReplayPending が割り込むのを防げる。
	s.delivering = true
	replayed := s.replayed
	s.replayed = nil
	s.mu.Unlock()

	// 逃がし先の分は「1回目の走査 → 2回目の走査」の順に並べてキューの先頭へ積む。
	// socket で届いた分より前に流すためである。
	events := append(replayed, s.scanLatePending()...)
	if len(events) > 0 {
		s.logger.Info("逃がし先に溜まっていた hook を配送の先頭へ積みました", "count", len(events))
	}
	s.prependQueue(events)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.wg.Add(1)
	s.mu.Unlock()

	go s.deliverLoop()
	s.logger.Info("hook の配送を始めました")
}

// deliverLoop はキューの先頭から1件ずつ HookSink へ渡す。Close で終わる。
func (s *Server) deliverLoop() {
	defer s.wg.Done()
	for {
		s.mu.Lock()
		for len(s.queue) == 0 && !s.closed {
			s.cond.Wait()
		}
		if s.closed {
			// 残りは捨てる。状態は in-memory なので（設計 3-4）、
			// プロセスを畳む場面で配送先の応答を待つ理由が無い。
			s.mu.Unlock()
			return
		}
		ev := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()

		if !s.sink.OnHook(ev) {
			s.logger.Warn("知らない session_id の hook が届いたので捨てました",
				"hook_event_name", ev.HookEventName,
				"session_id", ev.SessionID,
				"cwd", ev.Cwd)
		}
	}
}

// isClosed は Close 済みかどうかを返す。
func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Close は listen を止め、accept 済みの接続を閉じ、配送の goroutine を終わらせ、
// socket ファイルを消す。
//
// **accept 済みの接続も閉じる。**閉じないと handleConn が読み取りの期限（既定10秒）まで
// 戻らず、wg.Wait がその間ずっと返らない。continuo の終了・再起動がその都度止まる。
//
// 2回以上呼んでも安全である（2回目以降は何もしない）。
// 戻り値: リスナーを閉じられなかった場合のエラー。socket ファイルを消せなかった場合は
// ログに残すだけでエラーにしない（次回の起動が残骸として片付けるため）。
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	ln := s.ln
	s.ln = nil
	conns := make([]net.Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.conns = map[net.Conn]struct{}{}
	s.cond.Broadcast()
	// 順番待ちで止まっている接続ごとの goroutine も起こす（起こさないと wg.Wait が返らない）。
	s.seqCond.Broadcast()
	// 札待ちで止まっている accept ループも起こす（sync.Cond では select と混ぜられない）。
	close(s.done)
	s.mu.Unlock()

	var closeErr error
	if ln != nil {
		if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			closeErr = fmt.Errorf("hook を受ける socket を閉じられません: %s: %w", s.socketPath, err)
		}
	}
	for _, c := range conns {
		// 読み取りの途中の接続を叩き起こす。handleConn 側が同じ接続をもう一度閉じるが、
		// 2回目は「既に閉じている」を返すだけで害は無い。
		_ = c.Close()
	}

	s.wg.Wait()

	if ln != nil {
		if err := os.Remove(s.socketPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			s.logger.Warn("hook を受ける socket のファイルを消せませんでした", "socket", s.socketPath, "error", err)
		}
	}
	return closeErr
}
