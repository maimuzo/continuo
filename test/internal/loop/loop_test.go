// loop が、積まれた仕事を1つずつ実行し、押さえを実行する時点で確かめることを確かめる
// （issue #284）。
package loop_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/loop"
)

// waitTimeout はテストが止まったと判断するまでの時間である。
const waitTimeout = 5 * time.Second

// newStarted は起動した loop を返し、テストの終わりに閉じる。
func newStarted(t *testing.T) *loop.Loop {
	t.Helper()
	l := loop.New(nil)
	l.Start()
	t.Cleanup(l.Close)
	return l
}

// doAsync は Do を別の goroutine で呼び、結果を返す channel を返す。
func doAsync(ctx context.Context, l loop.Runner, key string, fn loop.Func) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- l.Do(ctx, key, fn) }()
	return ch
}

// recv は channel から1つ受ける。waitTimeout を過ぎたら落とす。
func recv(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(waitTimeout):
		t.Fatal("結果が返りませんでした")
		return nil
	}
}

// 目的: 仕事が積まれた順に、1つずつ実行され、2つが同時に走らないことを確かめる
// （herdr の開け閉めの一続きの途中に、別の開け閉めが割り込まないため）。
// 与える情報: 1つ目の仕事を門で止めている間に、5つの仕事をこの順に積む。
// 成功条件: 同時に走った仕事の数の最大が1で、動いた順が積んだ順（0..4）であること。
func TestLoop_積んだ順に1つずつ実行し同時に2つ走らない(t *testing.T) {
	l := newStarted(t)
	var running, maxRunning atomic.Int32
	var mu sync.Mutex
	var order []int
	var chans []<-chan error
	// 1つ目の仕事が走っている間に残りを積むため、1つ目を門で止める。
	gate := make(chan struct{})
	first := doAsync(context.Background(), l, "", func(ctx context.Context, _ *loop.Job) error {
		<-gate
		return nil
	})
	time.Sleep(20 * time.Millisecond)
	for i := range 5 {
		chans = append(chans, doAsync(context.Background(), l, "", func(ctx context.Context, _ *loop.Job) error {
			n := running.Add(1)
			if n > maxRunning.Load() {
				maxRunning.Store(n)
			}
			mu.Lock()
			order = append(order, i)
			mu.Unlock()
			time.Sleep(2 * time.Millisecond)
			running.Add(-1)
			return nil
		}))
		// 積まれる順を固定する。
		time.Sleep(5 * time.Millisecond)
	}
	close(gate)
	if err := recv(t, first); err != nil {
		t.Fatal(err)
	}
	for _, ch := range chans {
		if err := recv(t, ch); err != nil {
			t.Fatal(err)
		}
	}
	if maxRunning.Load() != 1 {
		t.Fatalf("同時に走った仕事の数 = %d, want 1", maxRunning.Load())
	}
	for i, got := range order {
		if got != i {
			t.Fatalf("実行の順 = %v, want 0..4 の順", order)
		}
	}
}

// 目的: 押さえられた key の仕事は放されるまで動かず、別の key と空の key の仕事は後に回らず、
// TryDo は押さえられていれば ErrBusy を返すことを確かめる。
// 与える情報: clone:/a を押さえたあと、同じ key の Do・別の key の Do・空の key の Do・同じ key の
// TryDo を積み、最後に放す。
// 成功条件: 別の key と空の key の仕事は返り、同じ key の Do は放すまで動かず、TryDo は ErrBusy、
// 放したあと同じ key の Do が動くこと。
func TestLoop_押さえている間は同じkeyの仕事が後に回り放すと動く(t *testing.T) {
	l := newStarted(t)
	if err := l.Do(context.Background(), "", func(ctx context.Context, j *loop.Job) error {
		j.Hold("clone:/a")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var ran atomic.Bool
	parked := doAsync(context.Background(), l, "clone:/a", func(ctx context.Context, _ *loop.Job) error {
		ran.Store(true)
		return nil
	})
	// 別の key と空の key は後に回らない。
	if err := l.Do(context.Background(), "clone:/b", func(ctx context.Context, _ *loop.Job) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := l.Do(context.Background(), "", func(ctx context.Context, _ *loop.Job) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if ran.Load() {
		t.Fatal("押さえられている key の仕事が動きました")
	}
	// TryDo は押さえられていれば ErrBusy を返す。
	if err := l.TryDo(context.Background(), "clone:/a", func(ctx context.Context, _ *loop.Job) error { return nil }); !errors.Is(err, loop.ErrBusy) {
		t.Fatalf("TryDo = %v, want ErrBusy", err)
	}
	if err := l.Do(context.Background(), "", func(ctx context.Context, j *loop.Job) error {
		j.Release("clone:/a")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := recv(t, parked); err != nil {
		t.Fatal(err)
	}
	if !ran.Load() {
		t.Fatal("放したあとも仕事が動きませんでした")
	}
}

// 目的: 押さえを積んだ時点ではなく実行する時点で確かめることを確かめる（積んだ時点で確かめると、
// 先に並んだ作る仕事が押さえたあとに `worktree.open` が走りうる）。
// 与える情報: loop を門で止めた状態で、押さえる仕事、同じ key の仕事の順に積み、門を開ける。
// 成功条件: 押さえる仕事のあとも同じ key の仕事が動かず、放したあとに動くこと。
func TestLoop_押さえは実行する時点で確かめる(t *testing.T) {
	l := newStarted(t)
	gate := make(chan struct{})
	blocker := doAsync(context.Background(), l, "", func(ctx context.Context, _ *loop.Job) error {
		<-gate
		return nil
	})
	time.Sleep(20 * time.Millisecond)
	// 押さえる仕事を先に積み、そのあとに同じ key の仕事を積む（どちらもまだ動かない）。
	holder := doAsync(context.Background(), l, "", func(ctx context.Context, j *loop.Job) error {
		j.Hold("clone:/a")
		return nil
	})
	time.Sleep(5 * time.Millisecond)
	var ran atomic.Bool
	later := doAsync(context.Background(), l, "clone:/a", func(ctx context.Context, _ *loop.Job) error {
		ran.Store(true)
		return nil
	})
	time.Sleep(5 * time.Millisecond)
	close(gate)
	for _, ch := range []<-chan error{blocker, holder} {
		if err := recv(t, ch); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(20 * time.Millisecond)
	if ran.Load() {
		t.Fatal("先に並んだ仕事が押さえたのに、あとの仕事が動きました")
	}
	if err := l.Do(context.Background(), "", func(ctx context.Context, j *loop.Job) error {
		j.Release("clone:/a")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := recv(t, later); err != nil {
		t.Fatal(err)
	}
}

// 目的: 実行が始まる前に ctx が終わると、仕事が外れて ctx.Err() が返り、その仕事は動かないことを
// 確かめる。押さえで後に回った仕事も同じであること。
// 与える情報: clone:/a を押さえたあと、同じ key の仕事を積んで後に回し、その ctx を取り消す。
// 成功条件: Do が context.Canceled を返し、放したあとも仕事が動かないこと。
func TestLoop_始まる前に取り消すと動かず後に回った仕事も外れる(t *testing.T) {
	l := newStarted(t)
	if err := l.Do(context.Background(), "", func(ctx context.Context, j *loop.Job) error {
		j.Hold("clone:/a")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var ran atomic.Bool
	parked := doAsync(ctx, l, "clone:/a", func(ctx context.Context, _ *loop.Job) error {
		ran.Store(true)
		return nil
	})
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := recv(t, parked); !errors.Is(err, context.Canceled) {
		t.Fatalf("Do = %v, want context.Canceled", err)
	}
	// 放しても、外れた仕事は動かない。
	if err := l.Do(context.Background(), "", func(ctx context.Context, j *loop.Job) error {
		j.Release("clone:/a")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if ran.Load() {
		t.Fatal("取り消した仕事が動きました")
	}
}

// 目的: 実行が始まった仕事は、ctx が終わっても Close されても、Do が fn の返るまで待って
// fn の結果を返すことを確かめる（動き始めた仕事を待たずに返すと、押さえた結果が呼び出し側に
// 届かず、押さえが永久に残る）。
// 与える情報: 動き始めた仕事を門で止めた状態で、ctx を取り消し、loop を Close してから門を開ける。
// 成功条件: Do が仕事の返した誤りをそのまま返すこと。
func TestLoop_動き始めた仕事はctxが終わってもCloseされても結果を返す(t *testing.T) {
	l := loop.New(nil)
	l.Start()
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	gate := make(chan struct{})
	want := errors.New("仕事の結果")
	ch := doAsync(ctx, l, "", func(ctx context.Context, _ *loop.Job) error {
		close(started)
		<-gate
		return want
	})
	<-started
	cancel()
	l.Close()
	close(gate)
	if err := recv(t, ch); !errors.Is(err, want) {
		t.Fatalf("Do = %v, want 仕事の結果", err)
	}
}

// 目的: Close のあとの Do と、積まれていた仕事・後に回っていた仕事が ErrClosed で返り、実行中の
// 仕事は最後まで走り、Close を2回呼んでも落ちないことを確かめる（止めるときに、待っている着手を
// 永久に待たせない）。
// 与える情報: 押さえで後に回った仕事・実行中の仕事（門で止める）・積まれた仕事がある状態で Close を2回呼ぶ。
// 成功条件: 後に回った仕事と積まれた仕事が ErrClosed、実行中の仕事が nil、Close のあとの Do が
// ErrClosed を返すこと。
func TestLoop_Closeのあとと積まれていた仕事はErrClosedで返る(t *testing.T) {
	l := loop.New(nil)
	l.Start()
	if err := l.Do(context.Background(), "", func(ctx context.Context, j *loop.Job) error {
		j.Hold("clone:/a")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	parked := doAsync(context.Background(), l, "clone:/a", func(ctx context.Context, _ *loop.Job) error { return nil })
	gate := make(chan struct{})
	started := make(chan struct{})
	running := doAsync(context.Background(), l, "", func(ctx context.Context, _ *loop.Job) error {
		close(started)
		<-gate
		return nil
	})
	<-started
	queued := doAsync(context.Background(), l, "", func(ctx context.Context, _ *loop.Job) error { return nil })
	time.Sleep(20 * time.Millisecond)
	l.Close()
	l.Close()
	for _, ch := range []<-chan error{parked, queued} {
		if err := recv(t, ch); !errors.Is(err, loop.ErrClosed) {
			t.Fatalf("Do = %v, want ErrClosed", err)
		}
	}
	close(gate)
	if err := recv(t, running); err != nil {
		t.Fatalf("実行中の仕事 = %v, want nil", err)
	}
	if err := l.Do(context.Background(), "", func(ctx context.Context, _ *loop.Job) error { return nil }); !errors.Is(err, loop.ErrClosed) {
		t.Fatalf("Close のあとの Do = %v, want ErrClosed", err)
	}
}

// 目的: loop.Inline（`continuo abandon` が使う）が仕事をその場で実行し、押さえを持たないことを確かめる。
// 与える情報: 仕事の中で押さえる・放す Do、押さえた key の TryDo、取り消し済みの ctx の Do。
// 成功条件: TryDo が ErrBusy を返さずに動き、取り消し済みの ctx の Do は仕事を動かさず
// context.Canceled を返すこと。
func TestInline_その場で実行し押さえは何もしない(t *testing.T) {
	var r loop.Runner = loop.Inline{}
	if err := r.Do(context.Background(), "", func(ctx context.Context, j *loop.Job) error {
		j.Hold("clone:/a")
		j.Release("clone:/b")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var ran bool
	if err := r.TryDo(context.Background(), "clone:/a", func(ctx context.Context, _ *loop.Job) error {
		ran = true
		return nil
	}); err != nil || !ran {
		t.Fatalf("TryDo = %v, ran = %v", err, ran)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Do(ctx, "", func(ctx context.Context, _ *loop.Job) error {
		t.Fatal("取り消した ctx で仕事が動きました")
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Do = %v, want context.Canceled", err)
	}
}
