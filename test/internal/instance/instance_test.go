package instance_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/instance"
)

// tempHome は、ホームディレクトリの代わりに使う一時ディレクトリを作る。
//
// t: 呼び出し元のテスト。
// 戻り値: 実体のパス（symlink を解決済み）。
func tempHome(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "ci")
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

// 目的: 二重起動を防ぐロックが `~/.continuo/continuo.lock` に固定されることを確かめる
// （設計 3-17）。
//
// **socket の場所から導いてはならない。**socket の場所は `CONTINUO_RUNTIME_DIR` /
// `XDG_RUNTIME_DIR` / `TMPDIR` で動くので、そこから導くと、**同じ機械の同じ利用者が、
// 誰も頼んでいないのに別のロックを握る。**
//
// 与える情報: socket の置き場所を2箇所へ向けた `CONTINUO_RUNTIME_DIR`。
// 成功条件: ロックのパスがどちらでも同じ1本であり、
// それが `<HOME>/.continuo/continuo.lock` であること。
func TestResolve_ロックはsocketをどこへ向けても同じ1本を指す(t *testing.T) {
	home := tempHome(t)
	t.Setenv("HOME", home)

	want := filepath.Join(home, ".continuo", "continuo.lock")

	for _, runtimeDir := range []string{filepath.Join(home, "rt-a"), filepath.Join(home, "rt-b")} {
		t.Setenv("CONTINUO_RUNTIME_DIR", runtimeDir)

		layout, err := instance.Resolve("")
		if err != nil {
			t.Fatalf("既定の置き場所を決められない: %v", err)
		}
		if got := layout.LockPath(); got != want {
			t.Fatalf("ロックが socket の場所に引きずられている: got %q, want %q", got, want)
		}
	}
}

// 目的: `--id <名前>` がロックを名前ごとに分けることを確かめる（設計 3-17b）。
//
// **これが `--id` の唯一の役目である。**worktree の置き場所も socket も branch 名も
// 分けない。そちらはテスト用の `WORKFLOW.md` で書き換える前提である。
//
// 与える情報: `--id e2e` と、`--id` を付けない場合。
// 成功条件: 名前ごとに別のロックになり、名前を付けなければ既定の1本になること。
func TestResolve_idを渡すとロックが名前ごとに分かれる(t *testing.T) {
	home := tempHome(t)
	t.Setenv("HOME", home)

	def, err := instance.Resolve("")
	if err != nil {
		t.Fatalf("既定の置き場所を決められない: %v", err)
	}
	named, err := instance.Resolve("e2e")
	if err != nil {
		t.Fatalf("--id から置き場所を決められない: %v", err)
	}
	other, err := instance.Resolve("e2e-2")
	if err != nil {
		t.Fatalf("--id から置き場所を決められない: %v", err)
	}

	if got, want := named.ID(), "e2e"; got != want {
		t.Errorf("名前が変わっている: got %q, want %q", got, want)
	}
	if got := def.ID(); got != "" {
		t.Errorf("--id を付けていないのに名前が入っている: got %q", got)
	}
	if got, want := def.LockPath(), filepath.Join(home, ".continuo", "continuo.lock"); got != want {
		t.Errorf("既定のロックの場所が違う: got %q, want %q", got, want)
	}
	if got, want := named.LockPath(), filepath.Join(home, ".continuo", "id", "e2e", "continuo.lock"); got != want {
		t.Errorf("ロックが名前ごとに分かれていない: got %q, want %q", got, want)
	}
	if named.LockPath() == other.LockPath() {
		t.Errorf("名前が違うのに同じロックを指している: %q", named.LockPath())
	}
	if named.LockPath() == def.LockPath() {
		t.Errorf("--id を付けたのに既定と同じロックを指している: %q", named.LockPath())
	}
}

// 目的: `EnsureLockDir` が、ロックを置くディレクトリを本人だけの権限で用意することを
// 確かめる（設計 3-17）。
//
// **親ディレクトリが無いと、二重起動でもないのに「ロックファイルを開けません」で止まる。**
//
// 与える情報: 何も無いホームディレクトリと `--id e2e`。
// 成功条件: `~/.continuo/id/e2e` が 0700 で出来ること。
func TestEnsureLockDir_ロックの置き場所を本人だけの権限で作る(t *testing.T) {
	home := tempHome(t)
	t.Setenv("HOME", home)

	layout, err := instance.Resolve("e2e")
	if err != nil {
		t.Fatalf("--id から置き場所を決められない: %v", err)
	}
	if err := layout.EnsureLockDir(); err != nil {
		t.Fatalf("ロックの置き場所を作れない: %v", err)
	}

	dir := filepath.Dir(layout.LockPath())
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("ロックの置き場所が出来ていない: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("ロックの置き場所がディレクトリではない: %s", dir)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("ロックの置き場所の権限が開いている: got %04o, want 0700", got)
	}
}

// 目的: `--id` に書ける名前を絞ることを確かめる（設計 3-17b）。
//
// **この文字列はロックファイルのパスに入る。**
// 絞らないと `--id ../../etc` が `~/.continuo` の外を指す。
//
// 与える情報: 大文字・`..`・空白・スラッシュ・先頭のハイフン・33文字と、
// 通るはずの名前。
// 成功条件: 通してよい名前だけが通ること。
func TestValidateID_使えない名前を弾く(t *testing.T) {
	home := tempHome(t)
	t.Setenv("HOME", home)

	cases := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{name: "小文字と数字は通る", id: "e2e", wantErr: false},
		{name: "途中のハイフンは通る", id: "issue-87", wantErr: false},
		{name: "32文字ちょうどは通る", id: strings.Repeat("a", 32), wantErr: false},
		{name: "大文字は弾く", id: "E2E", wantErr: true},
		{name: "ドット2つは弾く", id: "..", wantErr: true},
		{name: "上の階層を指す形は弾く", id: "../../etc", wantErr: true},
		{name: "空白は弾く", id: "my id", wantErr: true},
		{name: "スラッシュは弾く", id: "a/b", wantErr: true},
		{name: "先頭のハイフンは弾く", id: "-e2e", wantErr: true},
		{name: "アンダースコアは弾く", id: "e2e_1", wantErr: true},
		{name: "33文字は弾く", id: strings.Repeat("a", 33), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := instance.ValidateID(tc.id)
			if tc.wantErr && err == nil {
				t.Fatalf("使えない名前 %q が通ってしまった", tc.id)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("使える名前 %q を弾いた: %v", tc.id, err)
			}

			// **Resolve も同じ判定でなければならない。**フラグを読んだ直後に呼ぶのは
			// Resolve のほうなので、そちらが素通りしたら意味が無い。
			resolved, rerr := instance.Resolve(tc.id)
			if tc.wantErr {
				if rerr == nil {
					t.Fatalf("Resolve が使えない名前 %q を通してしまった", tc.id)
				}
				if !instance.IsInvalidID(rerr) {
					t.Fatalf("名前の誤りとして見分けられない: %v", rerr)
				}
				return
			}
			if rerr != nil {
				t.Fatalf("Resolve が使える名前 %q を弾いた: %v", tc.id, rerr)
			}
			// **`~/.continuo` の外へ出ていないことも確かめる。**
			root := filepath.Join(home, ".continuo")
			if !strings.HasPrefix(resolved.LockPath(), root+string(filepath.Separator)) {
				t.Fatalf("ロックが %s の外を指している: %q", root, resolved.LockPath())
			}
		})
	}
}

// 目的: 名前の検査を、ホームディレクトリを引くより先に通すことを確かめる（設計 3-17b）。
//
// **順序を逆にすると、`HOME` を引けない環境で `--id ../../etc` が
// 「ホームディレクトリを取得できません」として報告され、本当の誤りが人間に届かない。**
//
// 与える情報: ホームディレクトリを引けない環境と、使えない名前。
// 成功条件: 名前が使えないことを文言に出し、名前の誤りとして見分けられること。
func TestResolve_名前の検査をホームより先に通す(t *testing.T) {
	t.Setenv("HOME", "")

	_, err := instance.Resolve("../../etc")
	if err == nil {
		t.Fatal("使えない名前が通ってしまった")
	}
	if !strings.Contains(err.Error(), "../../etc") {
		t.Fatalf("名前の誤りとして報告していない: %v", err)
	}
	if !instance.IsInvalidID(err) {
		t.Fatalf("名前の誤りとして見分けられない: %v", err)
	}
}
