package config_test

import (
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

// 目的: `runtime.lock_file` を書いた `WORKFLOW.md` が**起動できる**ことを固定する（設計 3-17）。
//
// **キーは受け取り、値だけを捨てる。**`lock_file: null` は `continuo init` の雛形に
// 入っていたので、**キーごと弾くと、過去に `continuo init` した全員が次の起動で落ちる**
// （8-1 の front matter は未知のキーを弾く）。
//
// **値は読まない。**読むと `continuo abandon` が別の場所を見て「動いていない」と判定し、
// 走っている worktree を消しに行く（3-17c）。**分けたいなら `--id` である**（3-17b）。
//
// **展開も検査もしない。**相対パスでも、環境変数を含んでいても、起動を止めない
// （捨てる値のために起動を止める理由が無い）。
//
// 与える情報: `runtime.lock_file` に絶対パス・相対パス・null を書いた front matter の3通り。
// 成功条件: どれも config.Load がエラーを返さないこと。
func TestLoad_runtimeのlock_fileを書いても起動できる(t *testing.T) {
	cases := []struct {
		name  string
		extra string
	}{
		{name: "絶対パス", extra: "runtime:\n  lock_file: \"/tmp/continuo.lock\"\n"},
		{name: "相対パス", extra: "runtime:\n  lock_file: \"run/continuo.lock\"\n"},
		{name: "雛形のままのnull", extra: "runtime:\n  lock_file: null\n"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeWorkflow(t, validFrontMatter+c.extra, "")

			if _, err := config.Load(path); err != nil {
				t.Fatalf("runtime.lock_file を書いただけで起動を止めている: %v", err)
			}
		})
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
