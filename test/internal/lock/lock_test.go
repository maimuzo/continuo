// Package lock_test は internal/lock の二重起動防止を検証する。
package lock_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/lock"
)

// 目的: 同じロックファイルを2回取ろうとすると、2回目の Acquire が失敗することを確認する
// （設計 3-17。ps を使わず flock 1本で二重起動を判定する）。
// 与える情報: 一時ディレクトリの下に置いた1つのロックファイルパス。
// 成功条件: 1回目の Acquire は成功し、1回目を Release する前に行った2回目の Acquire は
// エラーを返すこと。
func TestAcquire_同じロックファイルを2回取ろうとすると2回目が失敗する(t *testing.T) {
	path := filepath.Join(t.TempDir(), "continuo.lock")

	first, err := lock.Acquire(path)
	if err != nil {
		t.Fatalf("1回目の Acquire に失敗した: %v", err)
	}
	defer first.Release()

	_, err = lock.Acquire(path)
	if err == nil {
		t.Fatal("2回目の Acquire が成功してしまった（二重起動を防げていない）")
	}
}

// 目的: Release したあとであれば、同じロックファイルを再度 Acquire できることを確認する
// （プロセスが終了すれば OS がロックを解放するという設計の前提の、プロセス内での相当確認）。
// 与える情報: 一時ディレクトリの下に置いた1つのロックファイルパス。
// 成功条件: 1回目を Acquire → Release したあと、2回目の Acquire が成功すること。
func TestAcquire_Release後は再度Acquireできる(t *testing.T) {
	path := filepath.Join(t.TempDir(), "continuo.lock")

	first, err := lock.Acquire(path)
	if err != nil {
		t.Fatalf("1回目の Acquire に失敗した: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release に失敗した: %v", err)
	}

	second, err := lock.Acquire(path)
	if err != nil {
		t.Fatalf("Release 後の2回目の Acquire に失敗した: %v", err)
	}
	defer second.Release()
}

// 目的: 存在しないディレクトリの下のロックファイルを指定した場合にエラーになり、
// **それが「二重起動」とは区別できる**ことを確認する。
// 両方を同じ文言で報告すると、ロックの置き場所を作れていない運用者が、
// 起動しているはずのない2つ目のプロセスを探すことになる。
// 与える情報: 実在しないディレクトリを含むパス。
// 成功条件: Acquire がエラーを返し、そのエラーが lock.ErrAlreadyRunning ではないこと。
func TestAcquire_親ディレクトリが無いエラーは二重起動と区別できる(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "continuo.lock")

	_, err := lock.Acquire(path)
	if err == nil {
		t.Fatal("親ディレクトリが無いのにエラーが返らなかった")
	}
	if errors.Is(err, lock.ErrAlreadyRunning) {
		t.Fatalf("ファイルを開けないだけなのに二重起動として報告された: %v", err)
	}
}

// 目的: 既に別のプロセスが掴んでいる場合のエラーが lock.ErrAlreadyRunning であることを
// 確認する（設計 3-17）。呼び出し側はこれを見て「二重起動を検出した」と
// 「ロックファイルを用意できない」を言い分ける。
// 与える情報: 1回目の Acquire で掴んだままのロックファイルのパス。
// 成功条件: 2回目の Acquire のエラーが errors.Is で lock.ErrAlreadyRunning と一致すること。
func TestAcquire_二重起動のエラーはErrAlreadyRunningである(t *testing.T) {
	path := filepath.Join(t.TempDir(), "continuo.lock")

	first, err := lock.Acquire(path)
	if err != nil {
		t.Fatalf("1回目の Acquire に失敗した: %v", err)
	}
	defer first.Release()

	_, err = lock.Acquire(path)
	if err == nil {
		t.Fatal("2回目の Acquire が成功してしまった（二重起動を防げていない）")
	}
	if !errors.Is(err, lock.ErrAlreadyRunning) {
		t.Fatalf("二重起動のエラーが lock.ErrAlreadyRunning ではない: %v", err)
	}
}

// 目的: AcquireWait が、誰も掴んでいないロックを待たずに取ることを確認する
// （docs/plans/impl/issue245_github_app_attribution.md の 3-82d「同時に叩かれたとき」）。
// 与える情報: 一時ディレクトリの下のロックファイルのパスと、短い上限。
// 成功条件: 即座に *Lock が返ること。
func TestAcquireWait_空いていれば待たずに取れる(t *testing.T) {
	path := filepath.Join(t.TempDir(), "github-app-credentials.lock")
	start := time.Now()
	l, err := lock.AcquireWait(path, 2*time.Second)
	if err != nil {
		t.Fatalf("AcquireWait に失敗した: %v", err)
	}
	defer l.Release()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("空いているのに %v 待った", elapsed)
	}
}

// 目的: AcquireWait が、別の持ち主が放すまで待ってから取ることを確認する。
//
// **Acquire は待たない（LOCK_NB）。**更新用のトークンの回転は「読む → 回す → 書き戻す」を
// 直列化する必要があり、待たないと片方が必ず落ちる。
//
// 与える情報: 先に Acquire で掴んだロックと、200ms 後にそれを放す goroutine。
// 成功条件: AcquireWait がエラーを返さず、放されたあとに *Lock を返すこと。
func TestAcquireWait_放されるまで待って取る(t *testing.T) {
	path := filepath.Join(t.TempDir(), "github-app-credentials.lock")
	first, err := lock.Acquire(path)
	if err != nil {
		t.Fatalf("1回目の Acquire に失敗した: %v", err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = first.Release()
		close(released)
	}()

	second, err := lock.AcquireWait(path, 5*time.Second)
	if err != nil {
		t.Fatalf("放されたあとも取れなかった: %v", err)
	}
	defer second.Release()
	select {
	case <-released:
	default:
		t.Fatal("1回目が放される前に2回目が取れてしまった（待っていない）")
	}
}

// 目的: 上限まで待っても放されなければ、二重起動の番兵を包んだエラーで返ることを確認する。
//
// **上限は必ず効かなければならない。**資格情報のロックを掴んだまま落ちた相手を
// 待ち続けると、本体の投稿も `continuo github-app token` も永久に止まる。
//
// 与える情報: 掴んだままのロックと、300ms の上限。
// 成功条件: エラーが返り、それが lock.ErrAlreadyRunning を包んでいること。
// 上限を大きく超えて待っていないこと。
func TestAcquireWait_上限まで待っても取れなければ番兵を包んだエラーで返る(t *testing.T) {
	path := filepath.Join(t.TempDir(), "github-app-credentials.lock")
	first, err := lock.Acquire(path)
	if err != nil {
		t.Fatalf("1回目の Acquire に失敗した: %v", err)
	}
	defer first.Release()

	start := time.Now()
	_, err = lock.AcquireWait(path, 300*time.Millisecond)
	if err == nil {
		t.Fatal("掴んだままなのに2回目が取れてしまった")
	}
	if !errors.Is(err, lock.ErrAlreadyRunning) {
		t.Errorf("上限切れのエラーが lock.ErrAlreadyRunning を包んでいない: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("上限 300ms のはずが %v 待った", elapsed)
	}
}

// 目的: 親ディレクトリが無いときは、待たずに Acquire と同じエラー（番兵を包まない）で返ることを確認する。
// 与える情報: 実在しないディレクトリを含むパス。
// 成功条件: 即座にエラーが返り、それが lock.ErrAlreadyRunning を包んでいないこと。
func TestAcquireWait_親ディレクトリが無ければ待たずに落ちる(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "github-app-credentials.lock")
	start := time.Now()
	_, err := lock.AcquireWait(path, 5*time.Second)
	if err == nil {
		t.Fatal("親ディレクトリが無いのに取れてしまった")
	}
	if errors.Is(err, lock.ErrAlreadyRunning) {
		t.Errorf("開けないだけなのに二重起動のエラーになっている: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("開けないのに %v 待った", elapsed)
	}
}
