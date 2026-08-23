// Package lock_test は internal/lock の二重起動防止を検証する。
package lock_test

import (
	"errors"
	"path/filepath"
	"testing"

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
// 両方を同じ文言で報告すると、runtime.lock_file を打ち間違えた運用者が、
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
