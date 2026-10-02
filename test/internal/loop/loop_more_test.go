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

// 目的: 押さえで後に回った複数の仕事が、放されたときに積まれた順で、しかも押さえている間に
// あとから積まれた別の仕事より先に動くことを確かめる（放すと queue の先頭へ戻す）。
// 与える情報: key を押さえたあと、同じ key の仕事 A・B をこの順で積む。次に loop を門で止め、
// その間に押さえを放す仕事と、空の key の仕事 D をこの順で積んでから門を開ける。
// 成功条件: 動いた順が「放す仕事 → A → B → D」であること（A と B は、あとから積まれた D より先に、
// 積まれた順で動く）。
func TestLoop_放すと後に回った仕事が積まれた順で先頭へ戻る(t *testing.T) {
	l := newStarted(t)
	if err := l.Do(context.Background(), "", func(ctx context.Context, j *loop.Job) error {
		j.Hold("clone:/a")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var order []string
	mark := func(name string) loop.Func {
		return func(ctx context.Context, _ *loop.Job) error {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return nil
		}
	}
	a := doAsync(context.Background(), l, "clone:/a", mark("A"))
	time.Sleep(20 * time.Millisecond)
	b := doAsync(context.Background(), l, "clone:/a", mark("B"))
	time.Sleep(20 * time.Millisecond)

	// 放す仕事と、そのあとに積む空の key の仕事 D を、loop を門で止めた状態で積む。
	gate := make(chan struct{})
	blocker := doAsync(context.Background(), l, "", func(ctx context.Context, _ *loop.Job) error {
		<-gate
		return nil
	})
	time.Sleep(20 * time.Millisecond)
	release := doAsync(context.Background(), l, "", func(ctx context.Context, j *loop.Job) error {
		mu.Lock()
		order = append(order, "release")
		mu.Unlock()
		j.Release("clone:/a")
		return nil
	})
	time.Sleep(20 * time.Millisecond)
	d := doAsync(context.Background(), l, "", mark("D"))
	time.Sleep(20 * time.Millisecond)
	close(gate)
	for _, ch := range []<-chan error{blocker, release, a, b, d} {
		if err := recv(t, ch); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"release", "A", "B", "D"}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != len(want) {
		t.Fatalf("動いた順 = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("動いた順 = %v, want %v（後に回った仕事は、あとから積まれた仕事より先に、積まれた順で動く）", order, want)
		}
	}
}

// 目的: TryDo は、押さえが無ければ仕事を実行し、押さえを見ない場合でも queue の順番は待つことを
// 確かめる（巡回の中の片付けは押さえを待たないが、前に並んだ仕事は待つ。計画の「1つの仕事の長さ」）。
// 与える情報: loop を門で止めた状態で TryDo を積む。
// 成功条件: 門を開けるまで TryDo の仕事が動かず、開けたあとに動いて nil を返すこと。
func TestLoop_TryDoは押さえが無ければ実行しqueueの順番は待つ(t *testing.T) {
	l := newStarted(t)
	gate := make(chan struct{})
	blocker := doAsync(context.Background(), l, "", func(ctx context.Context, _ *loop.Job) error {
		<-gate
		return nil
	})
	time.Sleep(20 * time.Millisecond)
	var ran atomic.Bool
	ch := make(chan error, 1)
	go func() {
		ch <- l.TryDo(context.Background(), "clone:/a", func(ctx context.Context, _ *loop.Job) error {
			ran.Store(true)
			return nil
		})
	}()
	time.Sleep(30 * time.Millisecond)
	if ran.Load() {
		t.Fatal("前に並んだ仕事が終わる前に TryDo の仕事が動きました")
	}
	close(gate)
	if err := recv(t, blocker); err != nil {
		t.Fatal(err)
	}
	if err := recv(t, ch); err != nil {
		t.Fatalf("TryDo = %v, want nil", err)
	}
	if !ran.Load() {
		t.Fatal("押さえが無いのに TryDo の仕事が動きませんでした")
	}
}

// 目的: 取り消しと実行の開始が同時に起きても、「動いた」と「ctx.Err() を返した」のどちらか一方だけに
// なることを確かめる（呼び出し側が ctx.Err() を受けたなら、その仕事は動いていない）。
// 与える情報: 何度も、仕事を積むのとほぼ同時に ctx を取り消す。
// 成功条件: どの回も、Do が nil を返したなら仕事は1回動き、ctx.Err() を返したなら仕事は動いていないこと。
func TestLoop_取り消しと開始が同時でもどちらか一方だけになる(t *testing.T) {
	l := newStarted(t)
	var sawCanceled, sawRan int
	for i := range 300 {
		ctx, cancel := context.WithCancel(context.Background())
		var runs atomic.Int32
		ch := doAsync(ctx, l, "", func(ctx context.Context, _ *loop.Job) error {
			runs.Add(1)
			return nil
		})
		if i%2 == 0 {
			time.Sleep(time.Duration(i%7) * time.Microsecond)
		}
		cancel()
		err := recv(t, ch)
		switch {
		case err == nil:
			if runs.Load() != 1 {
				t.Fatalf("%d 回目: Do は nil を返したのに仕事が %d 回動いた", i, runs.Load())
			}
			sawRan++
		case errors.Is(err, context.Canceled):
			// 結果が返ったあとで仕事が動き出さないことを確かめるため、loop を1周させる。
			if err := l.Do(context.Background(), "", func(ctx context.Context, _ *loop.Job) error { return nil }); err != nil {
				t.Fatal(err)
			}
			if runs.Load() != 0 {
				t.Fatalf("%d 回目: Do は取り消しを返したのに仕事が %d 回動いた", i, runs.Load())
			}
			sawCanceled++
		default:
			t.Fatalf("%d 回目: Do = %v", i, err)
		}
	}
	t.Logf("動いた回数 %d・取り消した回数 %d", sawRan, sawCanceled)
}

// 目的: Start を呼ばずに Close しても止まらず、そのあとの Do が ErrClosed を返すことを確かめる
// （daemon の組み立てが途中で失敗したとき、goroutine を起こさないまま後始末する経路）。
// 与える情報: New しただけの loop。
// 成功条件: Close が返り、Do が ErrClosed を返すこと。
// TestLoop_閉じたあとにStartしても落ちない は、Close のあとの Start が goroutine を起こさないことを確かめる。
//
// 目的: Start していない loop の Close は done を自分で閉じる。そのあとに Start が goroutine を
// 起こすと、run の終わりが done をもう一度閉じて panic する。全体を1本の loop に通す形では
// 起動の順が入れ替わりうるので、その順でも落ちないことを押さえる。
// 与える情報: New → Close → Start → Start の順で呼び、そのあと Do を呼ぶ。
// 成功条件: panic せず、Do が ErrClosed を返すこと。
func TestLoop_閉じたあとにStartしても落ちない(t *testing.T) {
	l := loop.New(nil)
	l.Close()
	l.Start()
	l.Start()
	if err := l.Do(context.Background(), "", func(ctx context.Context, _ *loop.Job) error { return nil }); !errors.Is(err, loop.ErrClosed) {
		t.Fatalf("Do = %v, want ErrClosed", err)
	}
	// run が起きていれば done を2度閉じて panic するので、それが起きるだけの時間を置く。
	time.Sleep(50 * time.Millisecond)
}

func TestLoop_Startせずに閉じても止まらない(t *testing.T) {
	l := loop.New(nil)
	done := make(chan struct{})
	go func() {
		l.Close()
		l.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("Start していない loop の Close が返りませんでした")
	}
	if err := l.Do(context.Background(), "", func(ctx context.Context, _ *loop.Job) error { return nil }); !errors.Is(err, loop.ErrClosed) {
		t.Fatalf("Do = %v, want ErrClosed", err)
	}
}
