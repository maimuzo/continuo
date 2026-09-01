package instance_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/instance"
	"github.com/maimuzo/continuo/internal/socketpath"
)

// shortHome は、ホームディレクトリの代わりに使う短い一時ディレクトリを作る。
//
// **短くなければならない。**`~/.continuo/id/<名前>/run/hooks.sock` は
// socketpath.MaxPathLen（103バイト）に収まらなければならず、macOS の `TMPDIR` は
// それだけで66文字前後ある。**そこへ作ると、32文字の名前が長さの上限で弾かれてしまい、
// 名前の検査を確かめられない。**
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
// `XDG_RUNTIME_DIR` / `TMPDIR` で動くので、そこから導くと、**同じ機械の同じ利用者が、
// 誰も頼んでいないのに別のロックを握る。**
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

		sock, err := layout.HookSocketPath(runtimeDir, nil)
		if err != nil {
			t.Fatalf("socket の場所を決められない: %v", err)
		}
		socks = append(socks, sock)
	}

	if socks[0] == socks[1] {
		t.Fatalf("socket の場所が分かれていない（検査が空振りしている）: %q", socks[0])
	}
}

// 目的: `--id <名前>` が4つを名前から機械的に導くことを確かめる（設計 3-17b）。
//
// **別々に導いてはならない。**片方だけを直すと食い違い、常駐している側と
// `continuo abandon` が別の場所を見る。
//
// 与える情報: `--id e2e` と、既定の `workspace.root` / `herdr.worktree.branch_template`。
// 成功条件: ロック・実行時ディレクトリ・worktree の置き場所・branch 名の4つが、
// すべて `e2e` ごとに分かれること。
func TestResolve_idを渡すと4つが名前ごとに分かれる(t *testing.T) {
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
	if got, want := layout.RuntimeDir(), filepath.Join(home, ".continuo", "id", "e2e", "run"); got != want {
		t.Errorf("実行時ディレクトリが名前ごとに分かれていない: got %q, want %q", got, want)
	}

	cfg := *config.DefaultConfig()
	cfg.Workspace.Root = filepath.Join(home, "worktrees")
	applied := layout.Apply(cfg)

	if got, want := applied.Workspace.Root, filepath.Join(home, "worktrees", "e2e"); got != want {
		t.Errorf("worktree の置き場所が名前ごとに分かれていない: got %q, want %q", got, want)
	}
	wantBranch := "e2e/" + cfg.Herdr.Worktree.BranchTemplate
	if got := applied.Herdr.Worktree.BranchTemplate; got != wantBranch {
		t.Errorf("branch 名が名前ごとに分かれていない: got %q, want %q", got, wantBranch)
	}
}

// 目的: `--id` を付けたときに、`claude.hook_bridge.listen` を使わないことを確かめる
// （設計 3-17b / 3-23）。
//
// **使うと、同じ `WORKFLOW.md` から2本立てたときに、issue ごとの設定と hook の
// 逃がし先を共有する。**片方がもう片方の hook を食べて捨てる。
//
// 与える情報: `claude.hook_bridge.listen` を書いた設定と `--id e2e`。
// 成功条件: socket が `<HOME>/.continuo/id/e2e/run/hooks.sock` になり、
// **`OverridesListen` が真を返すこと**（黙って握り潰さないため）。
func TestHookSocketPath_idを付けたらlistenの指定を使わない(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", home)

	layout, err := instance.Resolve("e2e")
	if err != nil {
		t.Fatalf("--id から置き場所を決められない: %v", err)
	}

	listen := filepath.Join(home, "elsewhere", "hooks.sock")
	cfg := *config.DefaultConfig()
	cfg.Claude.HookBridge.Listen = &listen

	if !layout.OverridesListen(cfg) {
		t.Fatal("listen の指定を使わなかったことを名乗っていない")
	}

	sock, err := layout.HookSocketPath("", cfg.Claude.HookBridge.Listen)
	if err != nil {
		t.Fatalf("socket の場所を決められない: %v", err)
	}
	want := filepath.Join(home, ".continuo", "id", "e2e", "run", socketpath.HookSocketFileName)
	if sock != want {
		t.Fatalf("listen の指定に引きずられている: got %q, want %q", sock, want)
	}
}

// 目的: `--id` に書ける名前を絞ることを確かめる（設計 3-17d）。
//
// **この文字列はパスにも branch 名にも socket のパスにも入る。**
// 絞らないと `--id ../../etc` が `~/.continuo` の外を指す。
//
// 与える情報: 大文字・`..`・空白・スラッシュ・先頭のハイフン・空文字・33文字と、
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

			// **Resolve も同じ判定でなければならない。**フラグを読んだ直後に呼ぶのは
			// Resolve のほうなので、そちらが素通りしたら意味が無い。
			if _, err := instance.Resolve(tc.id); tc.wantErr && err == nil {
				t.Fatalf("Resolve が使えない名前 %q を通してしまった", tc.id)
			}
		})
	}
}

// 目的: `--id` を足した socket のパスが長すぎるときに、名前の検査と同じところで
// 弾くことを確かめる（設計 3-17d / 3-23）。
//
// **ここで見ないと、起動して bind する段で初めて落ちる。**
//
// 与える情報: 深い階層にあるホームディレクトリと、32文字の名前。
// 成功条件: Resolve がエラーを返し、文言に上限のバイト数が出ること。
func TestResolve_idを足したsocketのパスが長すぎたら弾く(t *testing.T) {
	deep := filepath.Join(shortHome(t), strings.Repeat("d", 40), strings.Repeat("e", 40))
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatalf("深い階層のホームディレクトリを作れません: %v", err)
	}
	t.Setenv("HOME", deep)

	id := strings.Repeat("a", 32)
	if _, err := instance.Resolve(id); err == nil {
		t.Fatal("socket のパスが上限を超えるのに通ってしまった")
	} else if !strings.Contains(err.Error(), "103") {
		t.Fatalf("上限のバイト数が文言に出ていない: %v", err)
	}
}

// 目的: ボードのロックが「ボード1枚につき1本」になることを確かめる（設計 3-17e）。
//
// **`--id` を付けてもボードだけは名前から導けない。**
// **覚え書きの名前を `board.json` に固定してはならない。**固定すると、別のボードを
// 見る continuo が互いに上書きし、「誰が握っているか」を読む目的が果たせない。
//
// 与える情報: 所有者と番号が違う2枚のボード。
// 成功条件: ロックも覚え書きも、ボードごとに別のファイルになること。
func TestBoardLockPath_ボード1枚につき1本になる(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", home)

	first, _, err := instance.BoardLockPath("octocat", 10)
	if err != nil {
		t.Fatalf("ボードのロックの場所を決められない: %v", err)
	}
	want := filepath.Join(home, ".continuo", "board", "octocat-10.lock")
	if first != want {
		t.Fatalf("ボードのロックの場所が違う: got %q, want %q", first, want)
	}

	second, _, err := instance.BoardLockPath("octocat", 3)
	if err != nil {
		t.Fatalf("ボードのロックの場所を決められない: %v", err)
	}
	if first == second {
		t.Fatalf("番号が違うボードで同じロックを指している: %q", first)
	}
	if instance.LockInfoPath(first) == instance.LockInfoPath(second) {
		t.Fatalf("番号が違うボードで同じ覚え書きを指している: %q", instance.LockInfoPath(first))
	}
	if instance.LockInfoPath(first) == first {
		t.Fatal("覚え書きがロックファイルそのものを指している")
	}
}

// 目的: 所有者の名前に何が書かれていても、ボードのロックが `~/.continuo/board` の
// 外へ出ないことを確かめる（設計 3-7 / 3-17e）。
//
// **`tracker.provider.owner` は非空であること以外を検査していない。**
//
// 与える情報: 上の階層を指す所有者の名前。
// 成功条件: ロックのパスが `~/.continuo/board` の下に収まること。
func TestBoardLockPath_所有者の名前で置き場所の外へ出ない(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", home)

	got, _, err := instance.BoardLockPath("../../etc/passwd", 3)
	if err != nil {
		t.Fatalf("ボードのロックの場所を決められない: %v", err)
	}
	boardDir := filepath.Join(home, ".continuo", "board")
	if filepath.Dir(got) != boardDir {
		t.Fatalf("置き場所の外へ出ている: got %q, want %q の直下", got, boardDir)
	}
}

// 目的: 所有者の名前の大文字小文字でボードのロックが分かれないことを確かめる
// （設計 3-17e）。
//
// **GitHub のログイン名は大文字小文字を区別しない。**`owner: Octocat` と
// `owner: octocat` は同じボードである。**分かれると、同じボードを2つの continuo が見る。**
//
// 与える情報: 大文字を混ぜた所有者名と、すべて小文字の所有者名。
// 成功条件: ロックも覚え書きも同じ1本を指すこと。
func TestBoardLockPath_所有者の大文字小文字で分かれない(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", home)

	upper, _, err := instance.BoardLockPath("Octocat", 10)
	if err != nil {
		t.Fatalf("ボードのロックの場所を決められない: %v", err)
	}
	lower, _, err := instance.BoardLockPath("octocat", 10)
	if err != nil {
		t.Fatalf("ボードのロックの場所を決められない: %v", err)
	}
	if upper != lower {
		t.Fatalf("大文字小文字でロックが分かれている: got %q, want %q", upper, lower)
	}
	if instance.LockInfoPath(upper) != instance.LockInfoPath(lower) {
		t.Fatalf("大文字小文字で覚え書きが分かれている: got %q, want %q",
			instance.LockInfoPath(upper), instance.LockInfoPath(lower))
	}
}

// 目的: 名前を丸めたことを、呼ぶ側へ渡すことを確かめる（設計 3-7 / 3-17e）。
//
// **`owner: "my org"` と `owner: "my_org"` は同じロックになる。**黙って丸めると、
// **別のボードを見ている2本目が、理由の分からないまま断られる。**
//
// 与える情報: 空白を含む所有者名と、丸める必要のない所有者名。
// 成功条件: 丸めたときだけ警告が返り、丸めていないときは返らないこと。
func TestBoardLockPath_名前を丸めたら警告を返す(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", home)

	_, warnings, err := instance.BoardLockPath("my org", 10)
	if err != nil {
		t.Fatalf("ボードのロックの場所を決められない: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("名前を丸めたのに警告を返していない")
	}

	_, none, err := instance.BoardLockPath("octocat", 10)
	if err != nil {
		t.Fatalf("ボードのロックの場所を決められない: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("丸めていないのに警告を返している: %+v", none)
	}
}

// 目的: ボードの覚え書きを消せることを確かめる（設計 3-17e の段4）。
//
// **消えないと、死んだプロセスの PID を指したまま残る。**
// [docs/FAQ.md](../../../docs/FAQ.md) は「誰が握っているかを、この覚え書きで読め」と
// 案内しているので、**残ったままだと、動いていない continuo を探しに行くことになる。**
//
// 与える情報: 書いたばかりの覚え書きと、最初から無い覚え書き。
// 成功条件: 消せること。**最初から無くてもエラーにしないこと**
// （消えていることが目的であり、誰が消したかは問わない）。
func TestRemoveLockInfo_覚え書きを消す(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", home)

	lockPath, _, err := instance.BoardLockPath("octocat", 10)
	if err != nil {
		t.Fatalf("ボードのロックの場所を決められない: %v", err)
	}
	// **`BoardLockPath` は置き場所を作らない**（設計 3-17g）。書く前に用意する。
	if err := instance.EnsureBoardDir(lockPath); err != nil {
		t.Fatalf("ボードのロックの置き場所を作れない: %v", err)
	}
	if err := instance.WriteLockInfo(lockPath, instance.LockInfo{Owner: "octocat", ProjectNumber: 10}, nil); err != nil {
		t.Fatalf("覚え書きを書けない: %v", err)
	}
	if err := instance.RemoveLockInfo(lockPath); err != nil {
		t.Fatalf("覚え書きを消せない: %v", err)
	}
	if _, err := os.Stat(instance.LockInfoPath(lockPath)); !os.IsNotExist(err) {
		t.Fatalf("覚え書きが残っている: %v", err)
	}
	// **2回目もエラーにしない。**
	if err := instance.RemoveLockInfo(lockPath); err != nil {
		t.Fatalf("最初から無い覚え書きでエラーになった: %v", err)
	}
}

// 目的: 名前の検査を、ホームディレクトリを引くより先に通すことを確かめる（設計 3-17d）。
//
// **順序を逆にすると、`HOME` を引けない環境で `--id ../../etc` が
// 「ホームディレクトリを取得できません」として報告され、本当の誤りが人間に届かない。**
//
// 与える情報: ホームディレクトリを引けない環境と、使えない名前。
// 成功条件: 名前が使えないことを文言に出すこと。
func TestResolve_名前の検査をホームより先に通す(t *testing.T) {
	t.Setenv("HOME", "")

	_, err := instance.Resolve("../../etc")
	if err == nil {
		t.Fatal("使えない名前が通ってしまった")
	}
	if !strings.Contains(err.Error(), "../../etc") {
		t.Fatalf("名前の誤りとして報告していない: %v", err)
	}
}

// 目的: `--id` が環境変数 `CONTINUO_RUNTIME_DIR` を使わずに済ませたことを名乗るのを
// 確かめる（設計 3-17b / 3-23）。
//
// **黙って捨てると、socket が思った場所にできない理由を、無人運用のログから引けない。**
//
// 与える情報: `--id` の有無と、環境変数の値の有無。
// 成功条件: 両方そろったときだけ真を返すこと。
func TestOverridesRuntimeDirEnv_idと環境変数がそろったときだけ名乗る(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", home)

	withID, err := instance.Resolve("e2e")
	if err != nil {
		t.Fatalf("--id から置き場所を決められない: %v", err)
	}
	def, err := instance.Resolve("")
	if err != nil {
		t.Fatalf("既定の置き場所を決められない: %v", err)
	}

	if !withID.OverridesRuntimeDirEnv("/tmp/somewhere") {
		t.Error("--id を付けて環境変数もあるのに名乗っていない")
	}
	if withID.OverridesRuntimeDirEnv("") {
		t.Error("環境変数が空なのに名乗っている")
	}
	if def.OverridesRuntimeDirEnv("/tmp/somewhere") {
		t.Error("--id が無いのに名乗っている（環境変数はいままでどおり効く）")
	}
}
