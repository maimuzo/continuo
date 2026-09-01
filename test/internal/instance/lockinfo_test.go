package instance_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maimuzo/continuo/internal/instance"
)

// 目的: 覚え書きが1つも無いときに `not_running` と答えることを確認する（設計 3-17i）。
//
// **無いことは「そのロックを取ったプロセスが1つも居ない」ことである。**
// 覚え書きを書くのはロックを取った側だけなので、無いなら誰も取っていない。
//
// 与える情報: 覚え書きを1つも置いていないロックファイルのパス。
// 成功条件: `LockStateNotRunning` が返り、`Blocks()` が偽で、エラーが nil であること。
func TestReadLockState_覚え書きが無ければ動いていないと答える(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "continuo.lock")

	state, _, err := instance.ReadLockState(lockPath)
	if err != nil {
		t.Fatalf("ReadLockState がエラーを返した: %v", err)
	}
	if state != instance.LockStateNotRunning {
		t.Fatalf("状態が not_running ではない: %s", state)
	}
	if state.Blocks() {
		t.Fatal("not_running が「止まれ」を返している")
	}
}

// 目的: 生きている PID が書いてあれば `running` と答えることを確認する（設計 3-17i）。
//
// **自分自身の PID を書く。**必ず生きているので、環境に依存しない。
//
// 与える情報: 自分の PID を書いた覚え書き。
// 成功条件: `LockStateRunning` が返り、`Blocks()` が真であること。
func TestReadLockState_生きているPIDなら動いていると答える(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "continuo.lock")
	if err := instance.WriteLockInfo(lockPath, instance.LockInfo{PID: os.Getpid()}, nil); err != nil {
		t.Fatalf("覚え書きを書けない: %v", err)
	}

	state, info, err := instance.ReadLockState(lockPath)
	if err != nil {
		t.Fatalf("ReadLockState がエラーを返した: %v", err)
	}
	if state != instance.LockStateRunning {
		t.Fatalf("状態が running ではない: %s", state)
	}
	if !state.Blocks() {
		t.Fatal("running が「止まれ」を返していない")
	}
	if info.PID != os.Getpid() {
		t.Fatalf("読んだ PID が違う: %d", info.PID)
	}
}

// 目的: 死んだ PID が書いてあれば `stale` と答え、進んでよいと言うことを確認する（設計 3-17i）。
//
// **`SIGKILL` や電源断の残骸がこれである。**進んでよいが、残骸があることは画面に出す。
//
// 与える情報: すぐ終わらせた子プロセスの PID を書いた覚え書き。
// 成功条件: `LockStateStale` が返り、`Blocks()` が偽であること。
func TestReadLockState_死んだPIDなら残骸と答える(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "continuo.lock")
	if err := instance.WriteLockInfo(lockPath, instance.LockInfo{PID: deadPID(t)}, nil); err != nil {
		t.Fatalf("覚え書きを書けない: %v", err)
	}

	state, _, err := instance.ReadLockState(lockPath)
	if err != nil {
		t.Fatalf("ReadLockState がエラーを返した: %v", err)
	}
	if state != instance.LockStateStale {
		t.Fatalf("状態が stale ではない: %s", state)
	}
	if state.Blocks() {
		t.Fatal("stale が「止まれ」を返している")
	}
}

// 目的: 覚え書きが壊れていたら `unknown` と答え、止まれと言うことを確認する（設計 3-17i）。
//
// **これが4つ目の値が要る理由である。**覚え書きは「書けなくても起動を止めない」ものなので、
// **読めないことは「動いていない」の証拠にならない。**
// 3値しか無いと、ここが `not_running` に丸められ、
// **生きている continuo の worktree を消しにいく。**
//
// 与える情報: JSON として壊れている覚え書きと、pid を持たない覚え書き。
// 成功条件: どちらも `LockStateUnknown` が返り、`Blocks()` が真で、エラーが非 nil であること。
func TestReadLockState_読めなければ分からないと答えて止める(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "JSON が壊れている", body: "{ pid: "},
		{name: "pid が入っていない", body: `{"owner":"octocat"}`},
		{name: "pid が 0", body: `{"pid":0}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lockPath := filepath.Join(t.TempDir(), "continuo.lock")
			if err := os.WriteFile(instance.LockInfoPath(lockPath), []byte(tc.body), 0o600); err != nil {
				t.Fatalf("覚え書きを置けない: %v", err)
			}

			state, _, err := instance.ReadLockState(lockPath)
			if state != instance.LockStateUnknown {
				t.Fatalf("状態が unknown ではない: %s", state)
			}
			if !state.Blocks() {
				t.Fatal("unknown が「止まれ」を返していない")
			}
			if err == nil {
				t.Fatal("unknown なのに理由が返っていない")
			}
		})
	}
}

// 目的: 手放すときに覚え書きが消えることを確認する（設計 3-17i）。
//
// **消えないと、死んだ PID を指したまま残る。**次に起動しようとした側が
// `running` と読んで止まる。
//
// 与える情報: 書いたあとの覚え書き。
// 成功条件: RemoveLockInfo のあと `not_running` に戻ること。2回呼んでもエラーにならないこと。
func TestRemoveLockInfo_消したら動いていないに戻る(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "continuo.lock")
	if err := instance.WriteLockInfo(lockPath, instance.LockInfo{PID: os.Getpid()}, nil); err != nil {
		t.Fatalf("覚え書きを書けない: %v", err)
	}
	if err := instance.RemoveLockInfo(lockPath); err != nil {
		t.Fatalf("覚え書きを消せない: %v", err)
	}
	state, _, err := instance.ReadLockState(lockPath)
	if err != nil {
		t.Fatalf("ReadLockState がエラーを返した: %v", err)
	}
	if state != instance.LockStateNotRunning {
		t.Fatalf("消したのに not_running ではない: %s", state)
	}
	// **最初から無い場合もエラーにしない**（消えていることが目的である）。
	if err := instance.RemoveLockInfo(lockPath); err != nil {
		t.Fatalf("2回目の RemoveLockInfo がエラーを返した: %v", err)
	}
}

// deadPID は、確実に終了している子プロセスの PID を返す。
//
// **`/bin/true` を起動して待つ。**終了を待ってから PID を返すので、
// **そのプロセスは必ず居ない。**
//
// **PID の使い回しは、この検査の範囲では起きない。**待った直後に同じ番号が
// 別のプロセスへ配られることは、実用上ほぼ無い。
//
// t: 呼び出し元のテスト。
// 戻り値: 終了済みのプロセスの PID。
func deadPID(t *testing.T) int {
	t.Helper()
	proc, err := os.StartProcess("/bin/sh", []string{"sh", "-c", "exit 0"}, &os.ProcAttr{})
	if err != nil {
		t.Fatalf("子プロセスを起動できない: %v", err)
	}
	pid := proc.Pid
	if _, err := proc.Wait(); err != nil {
		t.Fatalf("子プロセスを待てない: %v", err)
	}
	return pid
}
