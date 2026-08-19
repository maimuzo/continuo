package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// 目的: claude.hook_bridge.listen に相対パスを書くと起動を止めることを確認する。
// 相対パスのままだと continuo を起動したディレクトリによって socket の場所が変わり、
// 走行中の Claude Code が持つパスとの一致検査（設計 3-23）が成立しなくなるためである。
// 与える情報: claude.hook_bridge.listen に相対パス "run/hooks.sock" を書いた front matter。
// 成功条件: config.Load がエラーを返し、エラーメッセージに設定キー名と書いた値の
// 両方が含まれること。
func TestLoad_hook_bridgeのlistenが相対パスだとエラーになる(t *testing.T) {
	front := validFrontMatter + "claude:\n  hook_bridge:\n    listen: \"run/hooks.sock\"\n"
	path := writeWorkflow(t, front, "")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("相対パスの listen なのにエラーが返らなかった")
	}
	msg := err.Error()
	if !strings.Contains(msg, "claude.hook_bridge.listen") {
		t.Errorf("エラーメッセージに設定キー名が含まれていない: %q", msg)
	}
	if !strings.Contains(msg, "run/hooks.sock") {
		t.Errorf("エラーメッセージに書いた値が含まれていない: %q", msg)
	}
}

// 目的: runtime.lock_file に相対パスを書くと起動を止めることを確認する。
// 相対パスだと起動したディレクトリごとに別のロックファイルになり、二重起動を防げない
// （設計 3-17）ためである。
// 与える情報: runtime.lock_file に相対パス "run/continuo.lock" を書いた front matter。
// 成功条件: config.Load がエラーを返し、エラーメッセージに設定キー名と書いた値の
// 両方が含まれること。
func TestLoad_lock_fileが相対パスだとエラーになる(t *testing.T) {
	front := validFrontMatter + "runtime:\n  lock_file: \"run/continuo.lock\"\n"
	path := writeWorkflow(t, front, "")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("相対パスの lock_file なのにエラーが返らなかった")
	}
	msg := err.Error()
	if !strings.Contains(msg, "runtime.lock_file") {
		t.Errorf("エラーメッセージに設定キー名が含まれていない: %q", msg)
	}
	if !strings.Contains(msg, "run/continuo.lock") {
		t.Errorf("エラーメッセージに書いた値が含まれていない: %q", msg)
	}
}

// 目的: 絶対パスの検査が 5-5 の展開のあとに行われることを確認する。
// チルダで書いたパスは展開前には絶対パスに見えないので、展開より前に検査していると
// 正しい設定を誤って弾いてしまう。
// 与える情報: runtime.lock_file に "~/continuo-test.lock" を書いた front matter。
// 成功条件: config.Load が成功し、値がホームディレクトリ配下の絶対パスへ展開されていること。
func TestLoad_チルダで書いたlock_fileは展開後に絶対パスとして受け付ける(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("ホームディレクトリを取得できないためスキップする: %v", err)
	}

	front := validFrontMatter + "runtime:\n  lock_file: \"~/continuo-test.lock\"\n"
	path := writeWorkflow(t, front, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("チルダで書いた lock_file が受け付けられなかった: %v", err)
	}
	if loaded.Config.Runtime.LockFile == nil {
		t.Fatal("lock_file が null のままになっている")
	}
	want := home + "/continuo-test.lock"
	if *loaded.Config.Runtime.LockFile != want {
		t.Fatalf("lock_file の展開結果が一致しない: got %q, want %q", *loaded.Config.Runtime.LockFile, want)
	}
}

// 目的: tracker.status_signal_prefix を空にすると起動を止めることを確認する。
// 印が空だと、エージェントの最終応答のどの行を表明とみなすか決められない（設計 3-25）。
// 与える情報: tracker.status_signal_prefix に空文字を書いた front matter。
// 成功条件: config.Load がエラーを返し、エラーメッセージに設定キー名が含まれること。
func TestLoad_status_signal_prefixが空だとエラーになる(t *testing.T) {
	front := "tracker:\n" +
		"  provider:\n" +
		"    owner: acme\n" +
		"    project_number: 1\n" +
		"    status_field: Status\n" +
		"  status_signal_prefix: \"\"\n"
	path := writeWorkflow(t, front, "")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("status_signal_prefix が空なのにエラーが返らなかった")
	}
	if !strings.Contains(err.Error(), "tracker.status_signal_prefix") {
		t.Errorf("エラーメッセージに設定キー名が含まれていない: %q", err.Error())
	}
}

// 目的: status_signal_map の値に空文字を書くと起動を止めることを確認する。
// null は「Status を動かさない」という意味を持つので許すが、空文字の Status 名は
// 存在しないため誤りである（設計 3-25）。
// 与える情報: tracker.status_signal_map.review に空文字を書いた front matter。
// 成功条件: config.Load がエラーを返し、エラーメッセージに設定キー名が含まれること。
func TestLoad_status_signal_mapの値が空文字だとエラーになる(t *testing.T) {
	front := "tracker:\n" +
		"  provider:\n" +
		"    owner: acme\n" +
		"    project_number: 1\n" +
		"    status_field: Status\n" +
		"  status_signal_map:\n" +
		"    review: \"\"\n"
	path := writeWorkflow(t, front, "")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("status_signal_map の値が空文字なのにエラーが返らなかった")
	}
	if !strings.Contains(err.Error(), "tracker.status_signal_map.review") {
		t.Errorf("エラーメッセージに設定キー名が含まれていない: %q", err.Error())
	}
}

// 目的: 基準にする作業ディレクトリが絶対パスでないとき、ResolvePath がエラーを返すことを
// 確認する。戻り値は身元ファイルとの一致検査や相対パスの基準に使うため、必ず絶対パスで
// なければならない（設計 5-1 / 3-18）。
// 与える情報: 相対パスの workDir と、位置引数あり・無しの2通り。
// 成功条件: どちらの場合もエラーが返り、位置引数が絶対パスの場合だけはエラーにならないこと。
func TestResolvePath_作業ディレクトリが相対パスならエラーになる(t *testing.T) {
	if _, err := config.ResolvePath("", "relative/dir"); err == nil {
		t.Error("位置引数なし・相対パスの作業ディレクトリなのにエラーが返らなかった")
	}
	if _, err := config.ResolvePath("custom.md", "relative/dir"); err == nil {
		t.Error("相対パスの位置引数・相対パスの作業ディレクトリなのにエラーが返らなかった")
	}

	// 位置引数が絶対パスなら作業ディレクトリを使わないので、相対パスでも解決できる。
	got, err := config.ResolvePath("/abs/path/WORKFLOW.md", "relative/dir")
	if err != nil {
		t.Fatalf("位置引数が絶対パスなのにエラーが返った: %v", err)
	}
	if got != "/abs/path/WORKFLOW.md" {
		t.Fatalf("位置引数の絶対パスがそのまま返らなかった: got %q", got)
	}
}
