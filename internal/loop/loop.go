// Package loop は、渡された仕事を1つの goroutine で、積まれた順に1つずつ実行する
// queue と loop である（issue #284。設計 3-4）。
//
// **この issue で積むのは、herdr の workspace を開け閉めする呼び出しだけである。**
// statusline取得の workspace を開いている間に、同じ clone で issue の worktree を開くと、
// herdr はその workspace を issue の「親」にしてしまい、閉じられなくなる
// （実測: 2026-09-28、herdr 0.9.1）。開け閉めを1つの loop に通し、statusline取得の
// workspace が開いている clone を「押さえる」ことで、その clone の `worktree.open` を
// 閉じるまで後に回す。
//
// **仕事は関数である。**loop は herdr を知らない。仕事の中で何をするかは積む側が決める。
// continuo 全体を1つの loop で動かす形（状態を書き換えるのも herdr を呼ぶのも1つの loop だけに
// する形）へ広げるときは、状態を閉じ込めた関数を積み、並列の処理が結果を積む口（待たずに積む
// `Post`）と、積まれた仕事を流し切ってから閉じる口を足す。`New`・`Do`・`TryDo`・`Job`・
// `Runner` の形は変えずに使える。
//
// **仕事の中から Do を呼ばない。**loop は1つなので、中から自分へ積んで待つと返らない。
// 包むのは呼び出し側だけにし、共用の関数の中では包まない。
package loop

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

// ErrBusy は TryDo が、key が押さえられていたために仕事を実行しなかったことを表す。
var ErrBusy = errors.New("loop: key is held")

// ErrClosed は、loop が閉じられたために仕事を実行しなかったことを表す。
var ErrClosed = errors.New("loop: closed")

// Func は loop に積む仕事である。
//
// ctx: Do / TryDo に渡したもの。
// j: 仕事の中からだけ使える押さえの口。
// 戻り値: Do / TryDo がそのまま返す誤り。
type Func func(ctx context.Context, j *Job) error

// Runner は、仕事を積んで実行し終えるまで待つ口である。
//
// *Loop と Inline が満たす。internal/workspace の Manager はこれで受ける。
type Runner interface {
	// Do は仕事を積み、実行し終えるまで待って、fn の誤りを返す。
	// key が空でなく押さえられていたら、放されるまで後に回す。
	Do(ctx context.Context, key string, fn Func) error
	// TryDo は Do と同じだが、実行する時点で key が押さえられていたら、
	// 後に回さずに ErrBusy を返す。
	TryDo(ctx context.Context, key string, fn Func) error
}

// Job は、仕事の中から押さえを取ったり放したりする口である。
//
// **仕事の外で使ってはならない。**押さえの表は loop の goroutine だけが書き換える前提で、
// 仕事の中（= loop の goroutine の上）からだけ触る。
type Job struct {
	// l は仕事を実行している loop である。nil なら Inline の仕事で、押さえは何もしない。
	l *Loop
}

// Hold は key を押さえる。押さえている間、その key を持つ Do の仕事は後に回る。
//
// key: 押さえる名前。空なら何もしない。
func (j *Job) Hold(key string) {
	if j == nil || j.l == nil || key == "" {
		return
	}
	j.l.hold(key)
}

// Release は key の押さえを外し、その key で後に回した仕事を queue の先頭へ戻す
// （積まれた順は保つ）。押さえていない key なら何もしない。
//
// key: 放す名前。
func (j *Job) Release(key string) {
	if j == nil || j.l == nil || key == "" {
		return
	}
	j.l.release(key)
}

// 仕事の状態である。遷移は1つの錠（Loop.mu）の下でだけ行う。
const (
	// statePending は、積まれて（または後に回されて）まだ始まっていない状態である。
	statePending = iota
	// stateRunning は、loop の goroutine の上で fn が動いている状態である。
	stateRunning
	// stateDone は、結果を返し終えた状態である（実行した・取り消した・閉じた・ErrBusy）。
	stateDone
)

// item は queue に積まれた1つの仕事である。
type item struct {
	// ctx は Do に渡したもの。仕事へそのまま渡す。
	ctx context.Context
	// key は押さえの名前。空なら押さえを見ない。
	key string
	// try が真なら、押さえられていたときに後に回さず ErrBusy を返す。
	try bool
	// fn は実行する仕事。
	fn Func
	// state は statePending / stateRunning / stateDone のどれか。mu の下で読み書きする。
	state int
	// result は結果を1つだけ受ける（容量1）。
	result chan error
}

// Loop は、積まれた仕事を1つの goroutine で1つずつ実行する。
//
// **押さえは実行する時点で確かめる。**積んだ時点で確かめると、先に並んだ作る仕事が
// 押さえたあとに `worktree.open` が走りうる。
type Loop struct {
	// logger はログの出力先である。
	logger *slog.Logger

	// mu は下の欄を守る。
	mu sync.Mutex
	// queue はこれから実行する仕事の列である。
	queue []*item
	// parked は、押さえで後に回した仕事を key ごとに積まれた順で持つ。
	parked map[string][]*item
	// held は押さえられている key の集合である。
	held map[string]struct{}
	// closed は Close が呼ばれたかである。
	closed bool
	// started は Start が呼ばれたかである。
	started bool

	// wake は、queue に仕事が積まれたか閉じられたことを loop の goroutine に知らせる（容量1）。
	wake chan struct{}
	// done は、loop の goroutine が終わると閉じる。
	done chan struct{}
}

// New は loop を作る。goroutine は Start で起こす。
//
// logger: ログの出力先。nil なら捨てる。
func New(logger *slog.Logger) *Loop {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Loop{
		logger: logger,
		parked: map[string][]*item{},
		held:   map[string]struct{}{},
		wake:   make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
}

// Start は仕事を実行する goroutine を起こす。2回目以降の呼び出しは何もしない。
func (l *Loop) Start() {
	l.mu.Lock()
	// **閉じたあとは起こさない。**Close は goroutine を起こしていなければ done を自分で閉じるので、
	// ここで起こすと run の終わりが done をもう一度閉じて panic する。
	if l.started || l.closed {
		l.mu.Unlock()
		return
	}
	l.started = true
	l.mu.Unlock()
	go l.run()
}

// Close は loop を閉じる。
//
// **積まれた仕事と後に回した仕事は ErrClosed で返す。**実行中の仕事は最後まで走らせるが、
// Close はその終わりを待たない（止める段の期限を変えないため）。
// Close のあとの Do / TryDo はすぐ ErrClosed を返す。2回呼んでもよい。
func (l *Loop) Close() {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	for _, it := range l.queue {
		l.finishLocked(it, ErrClosed)
	}
	l.queue = nil
	for key, items := range l.parked {
		for _, it := range items {
			l.finishLocked(it, ErrClosed)
		}
		delete(l.parked, key)
	}
	started := l.started
	l.mu.Unlock()
	l.signal()
	if !started {
		// goroutine を起こしていなければ、done を閉じる者が居ない。
		close(l.done)
	}
}

// Do は仕事を積み、実行し終えるまで待って、fn の誤りを返す。
//
// **実行を始めるか取り消すかは、loop の側で1つの錠の下で1度だけ決める。**
// 実行が始まる前に ctx が終わったら、仕事を外して ctx.Err() を返す（その仕事は動かない）。
// **実行が始まったら、ctx が終わっても Close されても、fn が返るまで待ち、fn の結果を返す。**
// 動き始めた仕事を待たずに返すと、押さえた結果が呼び出し側に届かず、押さえが永久に残る。
//
// ctx: 取り消しに使う。仕事へそのまま渡す。
// key: 押さえの名前。空なら押さえを見ない。
// fn: 実行する仕事。
// 戻り値: fn の誤り。取り消したら ctx.Err()、閉じたら ErrClosed。
func (l *Loop) Do(ctx context.Context, key string, fn Func) error {
	return l.submit(ctx, key, false, fn)
}

// TryDo は Do と同じだが、実行する時点で key が押さえられていたら、後に回さずに
// ErrBusy を返す（巡回の中から呼ぶ片付けを押さえで待たせないため）。
// queue の順番は待つ。
func (l *Loop) TryDo(ctx context.Context, key string, fn Func) error {
	return l.submit(ctx, key, true, fn)
}

// submit は仕事を積んで結果を待つ。
func (l *Loop) submit(ctx context.Context, key string, try bool, fn Func) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	it := &item{ctx: ctx, key: key, try: try, fn: fn, result: make(chan error, 1)}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return ErrClosed
	}
	l.queue = append(l.queue, it)
	l.mu.Unlock()
	l.signal()

	select {
	case err := <-it.result:
		return err
	case <-ctx.Done():
	}

	// **取り消すか、動き始めたものを待つかを、錠の下で1度だけ決める。**
	l.mu.Lock()
	if it.state == statePending {
		l.removeLocked(it)
		l.finishLocked(it, ctx.Err())
	}
	l.mu.Unlock()
	return <-it.result
}

// signal は loop の goroutine を起こす。既に起こしてあれば何もしない。
func (l *Loop) signal() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// run は loop の goroutine の本体である。
func (l *Loop) run() {
	defer close(l.done)
	for {
		it, ok := l.next()
		if !ok {
			return
		}
		if it == nil {
			<-l.wake
			continue
		}
		job := &Job{l: l}
		err := it.fn(it.ctx, job)
		l.mu.Lock()
		l.finishLocked(it, err)
		l.mu.Unlock()
	}
}

// next は次に実行する仕事を取り出す。
//
// 戻り値の1つ目: 実行する仕事。nil なら今は無い（wake を待つ）。
// 戻り値の2つ目: 偽なら loop を終える（閉じていて、実行するものも無い）。
func (l *Loop) next() (*item, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for len(l.queue) > 0 {
		it := l.queue[0]
		l.queue = l.queue[1:]
		if it.state != statePending {
			continue
		}
		if err := it.ctx.Err(); err != nil {
			l.finishLocked(it, err)
			continue
		}
		if it.key != "" {
			if _, held := l.held[it.key]; held {
				if it.try {
					l.finishLocked(it, ErrBusy)
					continue
				}
				l.parked[it.key] = append(l.parked[it.key], it)
				continue
			}
		}
		it.state = stateRunning
		return it, true
	}
	if l.closed {
		return nil, false
	}
	return nil, true
}

// hold は key を押さえる。
func (l *Loop) hold(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.held[key] = struct{}{}
}

// release は key の押さえを外し、後に回した仕事を queue の先頭へ戻す。
func (l *Loop) release(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, held := l.held[key]; !held {
		return
	}
	delete(l.held, key)
	items := l.parked[key]
	delete(l.parked, key)
	if len(items) == 0 {
		return
	}
	// **積まれた順を保つ。**後に回した仕事は、あとから積まれた仕事より前に積まれている。
	queue := make([]*item, 0, len(items)+len(l.queue))
	queue = append(queue, items...)
	queue = append(queue, l.queue...)
	l.queue = queue
}

// removeLocked は、まだ始まっていない仕事を queue か parked から外す。mu を持って呼ぶ。
func (l *Loop) removeLocked(it *item) {
	for i, q := range l.queue {
		if q == it {
			l.queue = append(l.queue[:i], l.queue[i+1:]...)
			return
		}
	}
	if it.key == "" {
		return
	}
	items := l.parked[it.key]
	for i, q := range items {
		if q == it {
			items = append(items[:i], items[i+1:]...)
			if len(items) == 0 {
				delete(l.parked, it.key)
			} else {
				l.parked[it.key] = items
			}
			return
		}
	}
}

// finishLocked は仕事の結果を1度だけ返す。mu を持って呼ぶ。
func (l *Loop) finishLocked(it *item, err error) {
	if it.state == stateDone {
		return
	}
	it.state = stateDone
	it.result <- err
}

// Inline は、仕事をその場で実行する Runner である。goroutine を持たない。
//
// **押さえは何もしない。**`continuo abandon` は1つの issue を1つずつ片付け、
// statusline取得をしないので、順番を決める相手がいない。goroutine を起こさないので
// 閉じる口も要らない。
type Inline struct{}

// Do は fn をその場で実行する。ctx が終わっていれば実行せずに ctx.Err() を返す。
func (Inline) Do(ctx context.Context, _ string, fn Func) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(ctx, &Job{})
}

// TryDo は Do と同じである（Inline には押さえが無いので ErrBusy を返さない）。
func (i Inline) TryDo(ctx context.Context, key string, fn Func) error {
	return i.Do(ctx, key, fn)
}
