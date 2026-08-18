// Package socketpath_test は internal/socketpath の hook socket の置き場所解決を検証する。
package socketpath_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/socketpath"
)

// 目的: 組み立てた socket のパスが103バイトを超える場合にエラーになることを確認する
// （設計 3-23。macOS は絶対パス103バイトまでで、104バイト以上は bind に失敗する実測値）。
// 与える情報: "hooks.sock" というファイル名（10バイト）を足すと103バイトを超える長さの
// ディレクトリ名。
// 成功条件: socketpath.Resolve がエラーを返すこと。
func TestResolve_103バイトを超えるとエラーになる(t *testing.T) {
	// "/" + "hooks.sock"（10バイト）で11バイト消費するので、dir を93バイトにすれば
	// 組み立てた絶対パスは104バイトになり、上限（103バイト）を1バイト超える。
	dir := "/" + strings.Repeat("a", 92)

	_, err := socketpath.Resolve(dir)
	if err == nil {
		t.Fatalf("パス長が103バイトを超えているのにエラーが返らなかった（dir長=%d）", len(dir))
	}
}

// 目的: 組み立てた socket のパスがちょうど103バイトなら成功することを確認する
// （境界値の確認。104バイトからエラーになるべきで、103バイトはまだ許容範囲）。
// 与える情報: 103バイトちょうどになるように調整したディレクトリ名。
// 成功条件: socketpath.Resolve がエラーを返さず、パス長が103バイトであること。
func TestResolve_103バイトちょうどなら成功する(t *testing.T) {
	// dir長を91バイトにすると "<dir>/hooks.sock" は 91 + 1 + 10 = 102バイト。
	// 上限ちょうど103バイトにするには dir長を92バイトにする。
	dir := "/" + strings.Repeat("a", 91)

	got, err := socketpath.Resolve(dir)
	if err != nil {
		t.Fatalf("上限ちょうどのパス長なのにエラーになった: %v", err)
	}
	if len(got) != socketpath.MaxPathLen {
		t.Fatalf("組み立てたパスの長さが上限と一致しない: got %d, want %d（path=%q）", len(got), socketpath.MaxPathLen, got)
	}
}

// 目的: 短いディレクトリなら問題なく解決できることを確認する（通常経路の確認）。
// 与える情報: 十分短いディレクトリ名。
// 成功条件: エラーが返らず、"<dir>/hooks.sock" になっていること。
func TestResolve_短いパスなら成功する(t *testing.T) {
	got, err := socketpath.Resolve("/tmp/continuo")
	if err != nil {
		t.Fatalf("短いパスなのにエラーになった: %v", err)
	}
	want := filepath.Join("/tmp/continuo", socketpath.HookSocketFileName)
	if got != want {
		t.Fatalf("組み立てたパスが一致しない: got %q, want %q", got, want)
	}
}

// 目的: 環境変数 CONTINUO_RUNTIME_DIR が明示されていれば、それが最優先で使われることを
// 確認する（3-23 の探索順の1番目）。
// 与える情報: 空でない envRuntimeDir。
// 成功条件: RuntimeDir が envRuntimeDir をそのまま返すこと。
func TestRuntimeDir_明示指定が最優先される(t *testing.T) {
	got, err := socketpath.RuntimeDir("/explicit/runtime/dir")
	if err != nil {
		t.Fatalf("RuntimeDir に失敗した: %v", err)
	}
	if got != "/explicit/runtime/dir" {
		t.Fatalf("明示指定が優先されていない: got %q", got)
	}
}

// 目的: 明示指定が無い場合、環境変数 XDG_RUNTIME_DIR の下の "continuo" が使われることを
// 確認する（3-23 の探索順の2番目）。
// 与える情報: envRuntimeDir は空文字、環境変数 XDG_RUNTIME_DIR を設定した状態。
// 成功条件: RuntimeDir が "<XDG_RUNTIME_DIR>/continuo" を返すこと。
func TestRuntimeDir_XDG_RUNTIME_DIRを使う(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")

	got, err := socketpath.RuntimeDir("")
	if err != nil {
		t.Fatalf("RuntimeDir に失敗した: %v", err)
	}
	want := filepath.Join("/run/user/1000", "continuo")
	if got != want {
		t.Fatalf("XDG_RUNTIME_DIR が使われていない: got %q, want %q", got, want)
	}
}

// 目的: ResolveHookSocketPath が、front matter の claude.hook_bridge.listen が明示されている
// 場合にそれをそのまま使うことを確認する。
// 与える情報: 空でない explicitListen。
// 成功条件: 戻り値が explicitListen と一致すること。
func TestResolveHookSocketPath_明示されたlistenを使う(t *testing.T) {
	listen := "/tmp/continuo/hooks.sock"

	got, err := socketpath.ResolveHookSocketPath(&listen, "")
	if err != nil {
		t.Fatalf("ResolveHookSocketPath に失敗した: %v", err)
	}
	if got != listen {
		t.Fatalf("明示された listen が使われていない: got %q, want %q", got, listen)
	}
}

// 目的: EnsureDir がディレクトリを 0700（所有者だけが読み書き・実行できる権限）で
// 作成することを確認する（設計 3-23。「Go が作る socket の権限は umask 次第で、
// 既定の環境では 0755 になる」ため、ディレクトリ側の権限を主たる防御にする）。
// 与える情報: まだ存在しない一時ディレクトリ配下のパス。
// 成功条件: 作成後の権限ビットが 0700 と一致すること。
func TestEnsureDir_ディレクトリを0700で作成する(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "continuo-run")

	if err := socketpath.EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir に失敗した: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("作成したディレクトリの stat に失敗した: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("ディレクトリの権限が 0700 ではない: got %o", perm)
	}
}

// 目的: 環境変数 CONTINUO_RUNTIME_DIR に相対パスが入っていたら、そのまま採用せずに
// エラーにすることを確認する。相対パスだと continuo を起動したディレクトリによって
// socket の場所が変わり、走行中の Claude Code が持つパスと一致しなくなる（設計 3-23）。
// 与える情報: 相対パス "run/continuo" を CONTINUO_RUNTIME_DIR の値として渡す。
// 成功条件: RuntimeDir がエラーを返し、エラーメッセージに環境変数名と渡した値が含まれること。
func TestRuntimeDir_相対パスの明示指定はエラーになる(t *testing.T) {
	_, err := socketpath.RuntimeDir("run/continuo")
	if err == nil {
		t.Fatal("相対パスを渡したのにエラーが返らなかった")
	}
	msg := err.Error()
	if !strings.Contains(msg, "CONTINUO_RUNTIME_DIR") {
		t.Errorf("エラーメッセージに環境変数名が含まれていない: %q", msg)
	}
	if !strings.Contains(msg, "run/continuo") {
		t.Errorf("エラーメッセージに渡した値が含まれていない: %q", msg)
	}
}

// 目的: claude.hook_bridge.listen に相対パスが入っていたら、socket のパスとして
// 採用せずにエラーにすることを確認する（設定の読み込み時にも検査しているが、
// この関数を直接使う経路のための検査である）。
// 与える情報: 相対パス "run/hooks.sock" を explicitListen として渡す。
// 成功条件: ResolveHookSocketPath がエラーを返し、エラーメッセージに設定キー名が含まれること。
func TestResolveHookSocketPath_相対パスのlistenはエラーになる(t *testing.T) {
	listen := "run/hooks.sock"
	_, err := socketpath.ResolveHookSocketPath(&listen, "")
	if err == nil {
		t.Fatal("相対パスの listen を渡したのにエラーが返らなかった")
	}
	if !strings.Contains(err.Error(), "claude.hook_bridge.listen") {
		t.Errorf("エラーメッセージに設定キー名が含まれていない: %q", err.Error())
	}
}
