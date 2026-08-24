// Package fsprobe_test は internal/fsprobe を公開 API を通して検証する。
//
// 確かめたいことは issue #11 の2つである。
//
//	落ちた理由を errors.Is で読み分けられること（EIO と EROFS を「壊れている」と判ずること）
//	書けるかどうかを、実際に作って消すことで確かめられること
package fsprobe_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/maimuzo/continuo/internal/fsprobe"
	"github.com/maimuzo/continuo/internal/i18n"
)

// TestClassify_落ち方を読み分ける は、案内を分けるための読み分けを確かめる。
//
// 目的: **`EIO` と `EROFS` を「ファイルシステムの異常」と判ずること。**
// この2つを「無い」や「権限が足りない」と同じ扱いにすると、
// **直る見込みのない作業（設定の作り直し）を利用者にさせることになる**（issue #11）。
// 与える情報: 素の syscall の値と、`%w` で包んだ値の両方。
// 成功条件: 包んでも読み分けが変わらないこと。
func TestClassify_落ち方を読み分ける(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want fsprobe.Fault
	}{
		{"エラーが無い", nil, fsprobe.FaultNone},
		{"無い", fs.ErrNotExist, fsprobe.FaultNotExist},
		{"権限が足りない", fs.ErrPermission, fsprobe.FaultPermission},
		{"読み取り専用のファイルシステム", syscall.EROFS, fsprobe.FaultFilesystem},
		{"入出力のエラー", syscall.EIO, fsprobe.FaultFilesystem},
		{"それ以外", errors.New("front matter が不正です"), fsprobe.FaultOther},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := fsprobe.Classify(c.err); got != c.want {
				t.Fatalf("%v の読み分けが %v ではなく %v だった", c.err, c.want, got)
			}
		})
	}
}

// TestClassify_包んでも読み分けが変わらない は、継ぎ足しの文言に依存していないことを確かめる。
//
// 目的: continuo のエラーは i18n.Errorf が `%w` で何段も包む。
// **包んだあとでも syscall の値まで辿れること。**
// 与える情報: `os.PathError` に入れ、さらに文言で2段包んだ `EROFS`。
// 成功条件: 「ファイルシステムの異常」と読み分けられること。
func TestClassify_包んでも読み分けが変わらない(t *testing.T) {
	inner := &os.PathError{Op: "mkdir", Path: "/home/tester/.claude/session-env", Err: syscall.EROFS}
	wrapped := i18n.Errorf(i18n.KeyFsprobeMkdirFailed, "/home/tester/.claude/session-env", inner)
	wrapped = i18n.Errorf(i18n.KeyFsprobeClaudeHomeFailed, "/home/tester/.claude/session-env", wrapped)

	if got := fsprobe.Classify(wrapped); got != fsprobe.FaultFilesystem {
		t.Fatalf("2段包んだ EROFS の読み分けが %v になった: %v", got, wrapped)
	}
}

// TestProbeWritable_書けるなら通り、使い捨ての跡を残さない は、検査の後始末を確かめる。
//
// 目的: 実際にディレクトリを作って消すところまでやり、**跡を残さない**こと。
// 与える情報: 書ける一時ディレクトリ。
// 成功条件: エラーが返らず、確かめた場所の中身が0件であること。
func TestProbeWritable_書けるなら通り使い捨ての跡を残さない(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "worktrees")

	if err := fsprobe.ProbeWritable(dir); err != nil {
		t.Fatalf("書けるはずの場所で落ちた: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("確かめた場所を読めません: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("検査に使ったものが残っている: %v", entries)
	}
}

// TestProbeWritable_書けないなら落ちる は、読むだけでは見抜けないものを見抜くことを確かめる。
//
// 目的: **`os.Stat` で見るだけでは足りない。**読めるが書けない場所を落とすこと。
// 与える情報: 権限を読み取りと実行だけにしたディレクトリ。
// 成功条件: エラーが返り、「権限が足りない」と読み分けられること。
func TestProbeWritable_書けないなら落ちる(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root は権限に関係なく書けるので、この検査は成立しない")
	}
	dir := filepath.Join(t.TempDir(), "readonly")
	if err := os.MkdirAll(dir, 0o500); err != nil {
		t.Fatalf("検査に使うディレクトリを作れません: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	err := fsprobe.ProbeWritable(dir)
	if err == nil {
		t.Fatal("書けないのに通った")
	}
	if got := fsprobe.Classify(err); got != fsprobe.FaultPermission {
		t.Fatalf("読み分けが「権限が足りない」ではなく %v だった: %v", got, err)
	}
}

// TestProbeWritable_絶対パスでなければ落ちる は、起動したディレクトリで結果が変わらないことを確かめる。
//
// 目的: 相対パスを受け取らないこと。
// 与える情報: 相対パス。
// 成功条件: エラーが返ること。
func TestProbeWritable_絶対パスでなければ落ちる(t *testing.T) {
	if err := fsprobe.ProbeWritable("worktrees"); err == nil {
		t.Fatal("相対パスなのに通った")
	}
}

// TestProbeClaudeSessionEnv_ホームの下の決まった場所を確かめる は、
// 見る場所が Claude Code の書く場所と同じであることを確かめる（issue #11）。
//
// 目的: `<home>/.claude/session-env` を確かめ、そのパスを返すこと。
// 与える情報: 一時ディレクトリをホームとして渡す。
// 成功条件: 返るパスが `<home>/.claude/session-env` であり、そこが空のまま残ること。
func TestProbeClaudeSessionEnv_ホームの下の決まった場所を確かめる(t *testing.T) {
	home := t.TempDir()

	dir, err := fsprobe.ProbeClaudeSessionEnv(home)
	if err != nil {
		t.Fatalf("書けるはずのホームで落ちた: %v", err)
	}

	want := filepath.Join(home, ".claude", fsprobe.SessionEnvDirName)
	if dir != want {
		t.Fatalf("確かめた場所が %s ではなく %s だった", want, dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("確かめた場所を読めません: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("検査に使ったものが残っている: %v", entries)
	}
}

// TestCheckWritablePlaces_どちらか一方でも書けなければ落ちる は、
// 起動時の検査が使う入口を確かめる（設計 3-6）。
//
// 目的: Claude Code の設定ディレクトリと `workspace.root` の両方を確かめ、
// **片方でも書けなければ落ちること。**
// 与える情報: 書けるホームと、権限を落とした `workspace.root`。
// 成功条件: 両方書けるなら通り、`workspace.root` が書けないなら落ちること。
func TestCheckWritablePlaces_どちらか一方でも書けなければ落ちる(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root は権限に関係なく書けるので、この検査は成立しない")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("ホームディレクトリを作れません: %v", err)
	}
	worktrees := filepath.Join(root, "worktrees")

	if err := fsprobe.CheckWritablePlaces(home, worktrees); err != nil {
		t.Fatalf("両方書けるはずなのに落ちた: %v", err)
	}

	if err := os.Chmod(worktrees, 0o500); err != nil {
		t.Fatalf("worktree の置き場所の権限を変えられません: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(worktrees, 0o700) })

	if err := fsprobe.CheckWritablePlaces(home, worktrees); err == nil {
		t.Fatal("worktree の置き場所に書けないのに通った")
	}
}

// TestFilesystemの案内がWSLで確かめる手順を持っている は、
// 案内の中身が「利用者がその場で叩けるもの」であることを確かめる（issue #11）。
//
// 目的: ファイルシステムが壊れたときの案内が、マウントの状態・カーネルログ・
// Windows 側の空き容量・WSL の停止と再起動の4つを含むこと。
// 与える情報: なし（文言の資源を直に引く）。
// 成功条件: 4つの案内にそれぞれの手がかりが入っていること。
func TestFilesystemの案内がWSLで確かめる手順を持っている(t *testing.T) {
	cases := []struct {
		key  i18n.Key
		want string
	}{
		{i18n.KeyDoctorFilesystemRemedyMount, "mount | grep"},
		{i18n.KeyDoctorFilesystemRemedyDmesg, "dmesg | grep -i ext4"},
		{i18n.KeyDoctorFilesystemRemedyDisk, "空き容量"},
		{i18n.KeyDoctorFilesystemRemedyRestart, "wsl --shutdown"},
	}
	for _, c := range cases {
		got := i18n.T(c.key)
		if got == "" {
			t.Fatalf("%s の文言が無い", c.key)
		}
		if !strings.Contains(got, c.want) {
			t.Fatalf("%s の案内に %q が入っていない: %s", c.key, c.want, got)
		}
	}
}
