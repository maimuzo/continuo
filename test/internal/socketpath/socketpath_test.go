// Package socketpath_test は internal/socketpath の hook socket の置き場所解決を検証する。
package socketpath_test

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
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
	// **実在するディレクトリを渡す。**
	// 以前は `/run/user/1000` という**実在しないパス**で通していた。
	// そのため「実在しなくても使う」という挙動がテストに守られ、
	// systemd が動いていない環境で `permission denied` になっていた（issue #9）。
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)

	got, err := socketpath.RuntimeDir("")
	if err != nil {
		t.Fatalf("RuntimeDir に失敗した: %v", err)
	}
	want := filepath.Join(dir, "continuo")
	if got != want {
		t.Fatalf("XDG_RUNTIME_DIR が使われていない: got %q, want %q", got, want)
	}
}

// TestRuntimeDir_XDG_RUNTIME_DIRが実在しなければ使わない は、issue #9 の状況を確かめる。
//
// **`/run/user/<uid>` を作るのは systemd であって、アプリではない。**
// systemd が動いていない環境（WSL など）では、環境変数だけが設定されていて
// ディレクトリが無い。**そこへ `MkdirAll` すると `permission denied` で落ちる。**
//
// 実際、`continuo doctor` が8項目すべて通るのに、起動だけが次で落ちた。
//
//	mkdir /run/user/1000: permission denied
//
// 目的: XDG_RUNTIME_DIR が実在しなければ、次の候補へ落ちること。
// 与える情報: 実在しないパスを指す XDG_RUNTIME_DIR。
// 成功条件: そのパスを使わず、ホームの下へ落ちること。
func TestRuntimeDir_XDG_RUNTIME_DIRが実在しなければ使わない(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "無い", "1000")
	t.Setenv("XDG_RUNTIME_DIR", missing)

	got, err := socketpath.RuntimeDir("")
	if err != nil {
		t.Fatalf("RuntimeDir に失敗した: %v", err)
	}
	if strings.HasPrefix(got, missing) {
		t.Errorf("実在しない XDG_RUNTIME_DIR を使っている: %q", got)
	}
	// **落ち先は OS で変わる**（macOS は $TMPDIR、それ以外はホームの下）。
	want := ""
	if runtime.GOOS == "darwin" && os.Getenv("TMPDIR") != "" {
		want = filepath.Join(os.Getenv("TMPDIR"), "continuo")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("ホームディレクトリを引けないので、落ち先を確かめられません")
		}
		want = filepath.Join(home, ".continuo", "run")
	}
	if got != want {
		t.Errorf("次の候補へ落ちていない: got %q, want %q", got, want)
	}
}

// TestRuntimeDir_XDG_RUNTIME_DIRがファイルなら使わない は、ディレクトリ以外を弾くことを確かめる。
//
// 目的: XDG_RUNTIME_DIR が通常のファイルを指していたら、次の候補へ落ちること。
// 与える情報: ファイルを指す XDG_RUNTIME_DIR。
// 成功条件: そのパスを使わないこと。
func TestRuntimeDir_XDG_RUNTIME_DIRがファイルなら使わない(t *testing.T) {
	f := filepath.Join(t.TempDir(), "ファイル")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatalf("ファイルを置けません: %v", err)
	}
	t.Setenv("XDG_RUNTIME_DIR", f)

	got, err := socketpath.RuntimeDir("")
	if err != nil {
		t.Fatalf("RuntimeDir に失敗した: %v", err)
	}
	if strings.HasPrefix(got, f) {
		t.Errorf("ディレクトリではないのに使っている: %q", got)
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

// 目的: EnsureDir が「自分で作っていない既存ディレクトリ」の権限を書き換えないことを
// 確認する（設計 3-23）。claude.hook_bridge.listen に "~/hooks.sock" と書くと、
// ここへ渡るのは利用者のホームディレクトリそのものになる。無条件に 0700 へ落とすと、
// 誰の許可も取らずにホームの権限を壊してしまう。
// 与える情報: あらかじめ 0755 で作っておいたディレクトリ。
// 成功条件: EnsureDir がエラーを返し（人間に直させる）、権限が 0755 のまま変わらないこと。
func TestEnsureDir_既存ディレクトリの権限を書き換えない(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "already-there")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("テスト用のディレクトリを作れません: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("テスト用のディレクトリの権限を設定できません: %v", err)
	}

	err := socketpath.EnsureDir(dir)
	if err == nil {
		t.Fatal("権限が開いている既存ディレクトリなのにエラーが返らなかった")
	}

	info, statErr := os.Stat(dir)
	if statErr != nil {
		t.Fatalf("stat に失敗した: %v", statErr)
	}
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Fatalf("既存ディレクトリの権限が書き換えられた: got %o, want 755", perm)
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("エラーメッセージに対象のディレクトリが含まれていない: %q", err.Error())
	}
}

// 目的: 既に 0700 で存在するディレクトリはそのまま受け入れることを確認する。
// 2回目以降の起動は必ずこの経路を通るので、ここでエラーになってはならない。
// 与える情報: EnsureDir が1度作ったディレクトリ。
// 成功条件: 2回目の EnsureDir が成功し、権限が 0700 のままであること。
func TestEnsureDir_2回目の呼び出しは成功する(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "continuo-run")

	if err := socketpath.EnsureDir(dir); err != nil {
		t.Fatalf("1回目の EnsureDir に失敗した: %v", err)
	}
	if err := socketpath.EnsureDir(dir); err != nil {
		t.Fatalf("2回目の EnsureDir に失敗した: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat に失敗した: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("ディレクトリの権限が 0700 ではない: got %o", perm)
	}
}

// 目的: 置き場所が symlink だったらエラーにすることを確認する。
// 辿った先へ socket とロックファイルが落ちるため、二重起動の判定も hook の受け口も
// 意図しない場所に作られてしまう（設計 3-17 / 3-23）。
// 与える情報: 0700 のディレクトリを指す symlink。
// 成功条件: EnsureDir がエラーを返し、その文に "symlink" が含まれること。
func TestEnsureDir_symlinkはエラーになる(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatalf("テスト用のディレクトリを作れません: %v", err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("テスト用の symlink を作れません: %v", err)
	}

	err := socketpath.EnsureDir(link)
	if err == nil {
		t.Fatal("symlink を渡したのにエラーが返らなかった")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("エラーメッセージが symlink であることを示していない: %q", err.Error())
	}
}

// 目的: 設計 3-23 の探索順のうち、テストが無かった3番目（macOS の $TMPDIR/continuo）と
// 4番目（~/.continuo/run）が効くこと、および上位が設定されている間は下位へ落ちないことを
// 確認する。このマシンは macOS なので、本番で実際に使われるのは3番目である。
// 与える情報: XDG_RUNTIME_DIR と TMPDIR を段ごとに設定・解除した環境。
// 成功条件: それぞれの段で、設計 3-23 が定める置き場所が返ること。
func TestRuntimeDir_探索順が設計3_23のとおりである(t *testing.T) {
	t.Run("XDGが設定されていればdarwinでもTMPDIRへ落ちない", func(t *testing.T) {
		// **実在するディレクトリを渡す。**実在しない XDG_RUNTIME_DIR は使わない
		// （systemd が作る場所であり、アプリが作ってよい場所ではない。issue #9）。
		xdg := t.TempDir()
		t.Setenv("XDG_RUNTIME_DIR", xdg)
		t.Setenv("TMPDIR", shortTempDir(t))

		got, err := socketpath.RuntimeDir("")
		if err != nil {
			t.Fatalf("RuntimeDir に失敗した: %v", err)
		}
		if want := filepath.Join(xdg, "continuo"); got != want {
			t.Fatalf("XDG_RUNTIME_DIR が最優先になっていない: got %q, want %q", got, want)
		}
	})

	t.Run("XDGが無ければdarwinはTMPDIRを使う", func(t *testing.T) {
		if runtime.GOOS != "darwin" {
			t.Skip("darwin 以外では通らない経路である（設計 3-23 の3番目）")
		}
		// **実在するディレクトリを渡す。**実在しない TMPDIR は使わない
		// （macOS の本番経路。issue #9 と同じ形の欠陥がここにも残っていた）。
		tmp := shortTempDir(t)
		t.Setenv("XDG_RUNTIME_DIR", "")
		t.Setenv("TMPDIR", tmp)

		got, err := socketpath.RuntimeDir("")
		if err != nil {
			t.Fatalf("RuntimeDir に失敗した: %v", err)
		}
		if want := filepath.Join(tmp, "continuo"); got != want {
			t.Fatalf("TMPDIR が使われていない: got %q, want %q", got, want)
		}
	})

	t.Run("どれも無ければホーム配下へ落ちる", func(t *testing.T) {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skipf("ホームディレクトリを取得できないためスキップする: %v", err)
		}
		t.Setenv("XDG_RUNTIME_DIR", "")
		t.Setenv("TMPDIR", "")

		got, err := socketpath.RuntimeDir("")
		if err != nil {
			t.Fatalf("RuntimeDir に失敗した: %v", err)
		}
		if want := filepath.Join(home, ".continuo", "run"); got != want {
			t.Fatalf("既定の置き場所になっていない: got %q, want %q", got, want)
		}
	})
}

// shortTempDir は、短い名前の一時ディレクトリを作る。
//
// **`t.TempDir()` は使えない。**テスト名をそのままパスに含めるので、
// 日本語のテスト名だと接頭辞だけで70バイトを超え、**socket のパス長の上限（103バイト）に
// 触れる。**実際、この上限のためにテストが落ちた。
//
// t: 呼び出し元のテスト。テストの終わりに消す。
// 戻り値: 短い絶対パス。
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "s")
	if err != nil {
		t.Fatalf("一時ディレクトリを作れません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return dir
	}
	return resolved
}

// TestPrepare_探索順の各段で実際にlistenできる は、issue #9 が通り抜けた継ぎ目を塞ぐ。
//
// **これまで「決める」（RuntimeDir）と「用意する」（EnsureDir）を別々にしか試していなかった。**
// その継ぎ目を、実在しない `XDG_RUNTIME_DIR` が通り抜けて利用者に届いた。
// `continuo doctor` は8項目すべて通るのに、起動だけが落ちた。
//
//	mkdir /run/user/1000: permission denied
//
// **決めた値を、外の世界（OS）に実際に食わせる。**
// 文字列の組み立てが合っていても、そこに socket を作れなければ意味がない。
//
// 目的: 探索順のどの段に落ちても、実際に unix socket を listen できること。
// 与える情報: 段ごとに環境変数を差し替える。
// 成功条件: `Prepare` が返したパスで `net.Listen("unix", …)` が成功すること。
func TestPrepare_探索順の各段で実際にlistenできる(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T) (env string)
	}{
		{
			name: "環境変数で明示した場所",
			setup: func(t *testing.T) string {
				// **これだけが「先に作っておく」逃げ道ではない。**
				// Prepare が自分で作れることを確かめるため、親までしか作らない。
				return filepath.Join(shortTempDir(t), "runtime")
			},
		},
		{
			name: "XDG_RUNTIME_DIR",
			setup: func(t *testing.T) string {
				t.Setenv("XDG_RUNTIME_DIR", shortTempDir(t))
				return ""
			},
		},
		{
			name: "実在しないXDGから落ちた先",
			setup: func(t *testing.T) string {
				// **issue #9 の状況そのものである。**
				t.Setenv("XDG_RUNTIME_DIR", filepath.Join(shortTempDir(t), "無い", "1000"))
				home := shortTempDir(t)
				t.Setenv("HOME", home)
				t.Setenv("TMPDIR", "")
				return ""
			},
		},
		{
			name: "ホームの下",
			setup: func(t *testing.T) string {
				t.Setenv("XDG_RUNTIME_DIR", "")
				t.Setenv("TMPDIR", "")
				t.Setenv("HOME", shortTempDir(t))
				return ""
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := tc.setup(t)

			sock, err := socketpath.Prepare(env, nil)
			if err != nil {
				t.Fatalf("用意できません: %v", err)
			}
			if !filepath.IsAbs(sock) {
				t.Fatalf("絶対パスではありません: %q", sock)
			}

			// **ここが本題。**決めた場所に、本当に socket を作れるか。
			ln, err := net.Listen("unix", sock)
			if err != nil {
				t.Fatalf("決めた場所に socket を作れません: %v（path=%q, %d バイト）",
					err, sock, len(sock))
			}
			_ = ln.Close()
			_ = os.Remove(sock)
		})
	}
}

// TestPrepare_実在しないTMPDIRからも落ちる は、macOS の本番経路を確かめる。
//
// **XDG の枝だけ直して、TMPDIR の枝を直し忘れていた。**
// `TMPDIR` は macOS の本番で使う場所である（設計 3-23 の3番目）。
//
// 目的: 実在しない TMPDIR を渡されても、次の候補へ落ちて listen できること。
// 与える情報: 実在しないパスを指す TMPDIR と、空の XDG_RUNTIME_DIR。
// 成功条件: そのパスを使わず、実際に socket を作れること。
func TestPrepare_実在しないTMPDIRからも落ちる(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("TMPDIR を見るのは darwin だけである（設計 3-23 の3番目）")
	}
	// **一時ディレクトリを先に全部作る。**
	// `os.MkdirTemp` は `TMPDIR` を見るので、先に実在しない値を入れると作れなくなる。
	missing := filepath.Join(shortTempDir(t), "無い")
	home := shortTempDir(t)
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", missing)
	t.Setenv("HOME", home)

	sock, err := socketpath.Prepare("", nil)
	if err != nil {
		t.Fatalf("用意できません: %v", err)
	}
	if strings.HasPrefix(sock, missing) {
		t.Errorf("実在しない TMPDIR を使っている: %q", sock)
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("決めた場所に socket を作れません: %v（path=%q）", err, sock)
	}
	_ = ln.Close()
	_ = os.Remove(sock)
}

// TestResolve_上限ちょうどのパスで実際にlistenできる は、パス長の上限に根拠を与える。
//
// **`MaxPathLen` は「bind できる上限」という実測に由来する定数である。**
// それなのに、境界のテストは組み立てた文字列の長さを `MaxPathLen` 自身と
// 比べているだけで、そのパスに socket を1度も開いていなかった。
// **定数が実際の境界とずれていても、全テストが緑になる。**
//
// 目的: 上限ちょうどのパスで listen でき、1バイト超えると失敗すること。
// 与える情報: 長い名前のディレクトリを掘って作った、境界の長さのパス。
// 成功条件: 上限では成功し、超えると失敗すること。
func TestResolve_上限ちょうどのパスで実際にlistenできる(t *testing.T) {
	// **`t.TempDir()` は使えない。**テスト名を含むので、日本語名だと接頭辞だけで
	// 上限に近づく。短い接頭辞で自分で掘る。
	base, err := os.MkdirTemp("", "s")
	if err != nil {
		t.Fatalf("一時ディレクトリを作れません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })

	// socket の名前を足したときに、ちょうど上限になるディレクトリを作る。
	sockName := "/" + socketpath.HookSocketFileName
	pad := socketpath.MaxPathLen - len(base) - len(sockName)
	if pad < 2 {
		t.Skipf("一時ディレクトリが長すぎて境界を作れません（base=%d バイト）", len(base))
	}
	dir := filepath.Join(base, strings.Repeat("d", pad-1))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("境界のディレクトリを作れません: %v", err)
	}

	sock, err := socketpath.Resolve(dir)
	if err != nil {
		t.Fatalf("上限ちょうどなのに弾かれました: %v", err)
	}
	if len(sock) != socketpath.MaxPathLen {
		t.Fatalf("境界を作れていません: %d バイト（want %d）", len(sock), socketpath.MaxPathLen)
	}

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("上限ちょうどのパスで listen できません（MaxPathLen が甘い）: %v", err)
	}
	_ = ln.Close()
	_ = os.Remove(sock)

	// **1バイト超えたら、OS が受け付けないこと。**
	// ここが通ってしまうなら、MaxPathLen は厳しすぎる。
	over := filepath.Join(base, strings.Repeat("d", pad), socketpath.HookSocketFileName)
	if err := os.MkdirAll(filepath.Dir(over), 0o700); err != nil {
		t.Fatalf("超過のディレクトリを作れません: %v", err)
	}
	if ln, err := net.Listen("unix", over); err == nil {
		_ = ln.Close()
		_ = os.Remove(over)
		t.Errorf("上限を1バイト超えても listen できました（MaxPathLen が厳しすぎます）: %d バイト", len(over))
	}
}
