// Package lock_test は internal/lock の二重起動防止を検証する。
package lock_test

import (
	"errors"
	"os"
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

// 目的: Probe がロックファイルを作らずに、握られているかだけを答えることを確認する
// （設計 3-17g）。
//
// **`continuo abandon --dry-run` のためにある。**`Acquire` は `O_CREATE` で
// ロックファイルを作るので、**「何も書かない」という約束を破る。**
//
// 与える情報: まだ存在しないロックファイルのパス。
// 成功条件: 「握られていない」と答え、**ファイルが作られていないこと。**
func TestProbe_無ければ握られていないと答えファイルも作らない(t *testing.T) {
	path := filepath.Join(t.TempDir(), "continuo.lock")

	held, err := lock.Probe(path)
	if err != nil {
		t.Fatalf("Probe がエラーを返した: %v", err)
	}
	if held {
		t.Fatal("誰も握っていないのに握られていると答えた")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("Probe がロックファイルを作っている（err=%v）", err)
	}
}

// 目的: Probe が、握られているロックを「握られている」と答えることを確認する
// （設計 3-17g）。
//
// **作らないようにしただけで判定まで落ちていないかを確かめる。**
//
// 与える情報: 先に Acquire したロックファイル。
// 成功条件: 「握られている」と答えること。**手放したあとは「握られていない」に戻ること。**
func TestProbe_握られていれば握られていると答える(t *testing.T) {
	path := filepath.Join(t.TempDir(), "continuo.lock")

	held, err := lock.Acquire(path)
	if err != nil {
		t.Fatalf("Acquire に失敗した: %v", err)
	}

	got, err := lock.Probe(path)
	if err != nil {
		t.Fatalf("Probe がエラーを返した: %v", err)
	}
	if !got {
		t.Fatal("握られているのに握られていないと答えた")
	}

	if err := held.Release(); err != nil {
		t.Fatalf("Release に失敗した: %v", err)
	}
	got, err = lock.Probe(path)
	if err != nil {
		t.Fatalf("Probe がエラーを返した: %v", err)
	}
	if got {
		t.Fatal("手放したあとも握られていると答えた")
	}
}

// 目的: Probe が握り続けないことを確認する（設計 3-17g）。
//
// **見せるだけの実行は1バイトも消さないので、握り続ける理由が無い。**
// **握り続けると、下見のあいだ継続監視が起動できなくなる。**
//
// 与える情報: 誰も握っていないロックファイル。
// 成功条件: Probe のあとに Acquire できること。
func TestProbe_握り続けない(t *testing.T) {
	path := filepath.Join(t.TempDir(), "continuo.lock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("ロックファイルを置けません: %v", err)
	}

	if _, err := lock.Probe(path); err != nil {
		t.Fatalf("Probe がエラーを返した: %v", err)
	}

	held, err := lock.Acquire(path)
	if err != nil {
		t.Fatalf("Probe のあとに Acquire できない（握り続けている）: %v", err)
	}
	_ = held.Release()
}
