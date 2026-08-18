// Package lock_test は internal/lock の二重起動防止を検証する。
package lock_test

import (
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

// 目的: 存在しないディレクトリの下のロックファイルを指定した場合にエラーになることを確認する
// （open(2) が失敗する経路。人間が読めるエラーであることの確認を兼ねる）。
// 与える情報: 実在しないディレクトリを含むパス。
// 成功条件: Acquire がエラーを返すこと。
func TestAcquire_親ディレクトリが無いとエラーになる(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "continuo.lock")

	_, err := lock.Acquire(path)
	if err == nil {
		t.Fatal("親ディレクトリが無いのにエラーが返らなかった")
	}
}
