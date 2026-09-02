package instance_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/instance"
	"github.com/maimuzo/continuo/internal/socketpath"
)

// shortHome は、ホームディレクトリの代わりに使う短い一時ディレクトリを作る。
//
// **短くしておく。**hook を受ける socket のパスは socketpath.MaxPathLen（103バイト）に
// 収まらなければならず、macOS の `TMPDIR` はそれだけで66文字前後ある。
//
// t: 呼び出し元のテスト。
// 戻り値: 実体のパス（symlink を解決済み）。
func shortHome(t *testing.T) string {
	t.Helper()

	for _, base := range []string{"/tmp", ""} {
		dir, err := os.MkdirTemp(base, "ci")
		if err != nil {
			continue
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return resolved
		}
		return dir
	}
	t.Fatal("一時ディレクトリを作れません")
	return ""
}

// 目的: 二重起動を防ぐロックが `~/.continuo/continuo.lock` に固定されることを確かめる
// （設計 3-17）。
//
// **socket の場所から導いてはならない。**socket の場所は `CONTINUO_RUNTIME_DIR` /
// `XDG_RUNTIME_DIR` / `TMPDIR` と `claude.hook_bridge.listen` で動くので、そこから導くと、
// **socket を分けただけで2本目が黙って起動できてしまう。**
//
// 与える情報: socket の置き場所を2箇所へ向けた `CONTINUO_RUNTIME_DIR`。
// 成功条件: socket のパスは2箇所で違うのに、ロックのパスは同じ1本であり、
// それが `<HOME>/.continuo/continuo.lock` であること。
func TestResolve_ロックはsocketをどこへ向けても同じ1本を指す(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", home)

	want := filepath.Join(home, ".continuo", "continuo.lock")

	first := filepath.Join(home, "rt-a")
	second := filepath.Join(home, "rt-b")

	var socks []string
	for _, runtimeDir := range []string{first, second} {
		t.Setenv("CONTINUO_RUNTIME_DIR", runtimeDir)

		layout, err := instance.Resolve("")
		if err != nil {
			t.Fatalf("既定の置き場所を決められない: %v", err)
		}
		if got := layout.LockPath(); got != want {
			t.Fatalf("ロックが socket の場所に引きずられている: got %q, want %q", got, want)
		}

		sock, err := socketpath.Prepare(runtimeDir, nil)
		if err != nil {
			t.Fatalf("socket の場所を決められない: %v", err)
		}
		socks = append(socks, sock)
	}

	if socks[0] == socks[1] {
		t.Fatalf("socket の場所が分かれていない（検査が空振りしている）: %q", socks[0])
	}
}

// 目的: `--id <名前>` がロックだけを名前ごとに分けることを確かめる（設計 3-17b）。
//
// **`--id` が分けるのはロックだけである。**worktree の置き場所も hook の socket も
// branch 名も、検証用の `WORKFLOW.md` で書き換える。
//
// 与える情報: `--id e2e`。
// 成功条件: ロックが `<HOME>/.continuo/id/e2e/continuo.lock` になり、名前が読み出せること。
func TestResolve_idを渡すとロックが名前ごとに分かれる(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", home)

	layout, err := instance.Resolve("e2e")
	if err != nil {
		t.Fatalf("--id から置き場所を決められない: %v", err)
	}

	if got, want := layout.ID(), "e2e"; got != want {
		t.Errorf("名前が変わっている: got %q, want %q", got, want)
	}
	if got, want := layout.LockPath(), filepath.Join(home, ".continuo", "id", "e2e", "continuo.lock"); got != want {
		t.Errorf("ロックが名前ごとに分かれていない: got %q, want %q", got, want)
	}
}

// 目的: ロックを置くディレクトリを用意できることを確かめる（設計 3-17）。
//
// **これが無いと、二重起動でもないのに「ロックファイルを開けません」で止まる。**
// `~/.continuo/id/<名前>/` は誰も作らないためである。
//
// 与える情報: `--id e2e` と、`~/.continuo` がまだ無いホームディレクトリ。
// 成功条件: ロックの親ディレクトリができ、そこへファイルを作れること。
func TestEnsureLockDir_ロックの親ディレクトリを用意する(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", home)

	layout, err := instance.Resolve("e2e")
	if err != nil {
		t.Fatalf("--id から置き場所を決められない: %v", err)
	}
	if err := layout.EnsureLockDir(); err != nil {
		t.Fatalf("ロックの置き場所を用意できない: %v", err)
	}

	if err := os.WriteFile(layout.LockPath(), nil, 0o600); err != nil {
		t.Fatalf("用意した置き場所にロックファイルを作れない: %v", err)
	}

	// **2回目も通る。**起動のたびに呼ぶので、既にあることをエラーにしてはならない。
	if err := layout.EnsureLockDir(); err != nil {
		t.Fatalf("2回目でエラーになった: %v", err)
	}
}

// 目的: `--id` に書ける名前を絞ることを確かめる（設計 3-17d）。
//
// **この文字列はロックのパスに入る。**
// 絞らないと `--id ../../etc` が `~/.continuo` の外を指す。
//
// 与える情報: 大文字・`..`・空白・スラッシュ・先頭のハイフン・33文字と、
// 通るはずの名前。
// 成功条件: 通してよい名前だけが通ること。
func TestValidateID_使えない名前を弾く(t *testing.T) {
	home := shortHome(t)
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
			if tc.wantErr && !instance.IsInvalidID(err) {
				t.Fatalf("名前の誤りとして見分けられない: %v", err)
			}

			// **Resolve も同じ判定でなければならない。**フラグを読んだ直後に呼ぶのは
			// Resolve のほうなので、そちらが素通りしたら意味が無い。
			if _, err := instance.Resolve(tc.id); tc.wantErr && err == nil {
				t.Fatalf("Resolve が使えない名前 %q を通してしまった", tc.id)
			}
		})
	}
}

// 目的: 名前の検査を、ホームディレクトリを引くより先に通すことを確かめる（設計 3-17d）。
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

// 目的: 名前を渡していないときの失敗を、名前の誤りとして報告しないことを確かめる
// （設計 3-17d）。
//
// **`--id` を1文字も渡していない人に「--id に渡した名前が使えません」と出してはならない。**
//
// 与える情報: ホームディレクトリを引けない環境と、空の名前。
// 成功条件: エラーになるが、名前の誤りとしては見分けられないこと。
func TestResolve_ホームを引けない失敗は名前の誤りではない(t *testing.T) {
	t.Setenv("HOME", "")

	_, err := instance.Resolve("")
	if err == nil {
		t.Skip("この環境ではホームディレクトリを引けてしまう（検査が成立しない）")
	}
	if instance.IsInvalidID(err) {
		t.Fatalf("名前を渡していないのに、名前の誤りとして報告している: %v", err)
	}
}
