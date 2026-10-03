// Package config_test のうち、このファイルは tracker.provider.host（接続先ホスト）の検査を固定する。
//
// **ここに書かれた値は、GraphQL の宛先・`gh` と `ghq` の引数・エージェントの環境変数へ
// そのまま入る**（設計 3-86）。形を検査しないと、`-` で始まる値がコマンドのフラグとして読まれる。
package config_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// frontMatterWithHost は validFrontMatter の `provider:` の下へ `host:` の行を足したものを返す。
//
// host: `host:` の右に書く文字列（引用符が要るなら付けて渡す）。
// 戻り値: front matter。
func frontMatterWithHost(host string) string {
	return strings.Replace(validFrontMatter, "  provider:\n", "  provider:\n    host: "+host+"\n", 1)
}

// 目的: `tracker.provider.host` を書かなければ github.com になることを確認する。
// **いまある WORKFLOW.md は、書き換えなくても同じ接続先で動き続ける**（設計 3-86）。
// 与える情報: `host` を書いていない front matter。
// 成功条件: 読み込んだ設定の Host が "github.com" であること。
func TestLoad_hostを書かなければgithub_comになる(t *testing.T) {
	loaded, err := config.Load(writeWorkflow(t, validFrontMatter, ""))
	if err != nil {
		t.Fatalf("読み込めない: %v", err)
	}
	if got := loaded.Config.Tracker.Provider.Host; got != "github.com" {
		t.Fatalf("既定の接続先ホストが github.com ではない: %q", got)
	}
}

// 目的: ホスト名として書いた値が、小文字に直って読めることを確認する。
// 与える情報: GitHub Enterprise Server のホスト名・`<名前>.ghe.com`・ドットの無い名前・大文字混じり・引用符つき。
// 成功条件: どれも読めて、Host が小文字のホスト名になること。
func TestLoad_hostに書いたホスト名を小文字にして読む(t *testing.T) {
	cases := map[string]string{
		"ghe.example.com":     "ghe.example.com",
		"octocorp.ghe.com":    "octocorp.ghe.com",
		"ghe":                 "ghe",
		"GHE.Example.COM":     "ghe.example.com",
		`"ghe-1.example.com"`: "ghe-1.example.com",
	}
	for written, want := range cases {
		loaded, err := config.Load(writeWorkflow(t, frontMatterWithHost(written), ""))
		if err != nil {
			t.Fatalf("host: %s を読み込めない: %v", written, err)
		}
		if got := loaded.Config.Tracker.Provider.Host; got != want {
			t.Errorf("host: %s の読み方が違う: got %q, want %q", written, got, want)
		}
	}
}

// 目的: ホスト名でない値を書くと、起動が止まることを確認する。
//
// **ポート番号も通さない。**worktree の置き場所のホスト（issue の URL から取る）は
// ポート番号を落とすので、通すと設定の値と置き場所のホストが食い違う。
// **`-` で始まる値も通さない。**`gh` と `ghq` がフラグとして読む。
//
// 与える情報: URL・パス付き・ポート番号付き・先頭や末尾が `-` か `.` の値・空文字・空白入り。
// 成功条件: どれも config.Load がエラーを返し、その文に "tracker.provider.host" が含まれること。
func TestLoad_hostがホスト名でなければ落ちる(t *testing.T) {
	for _, bad := range []string{
		`"https://ghe.example.com"`,
		`"ghe.example.com/api/graphql"`,
		`"ghe.example.com:8443"`,
		`"-ghe.example.com"`,
		`"ghe.example.com-"`,
		`".ghe.example.com"`,
		`"ghe.example.com."`,
		`""`,
		`"ghe example.com"`,
		`"ghe_example.com"`,
	} {
		assertLoadFailsWith(t, frontMatterWithHost(bad), "tracker.provider.host")
	}
}

// 目的: `claude.env` の GH_HOST が接続先ホストと違うと、起動が止まることを確認する。
//
// **continuo は issue ごとの設定ファイルの env へ `GH_HOST=<接続先ホスト>` を書く**（設計 3-86）。
// 黙ってどちらかを勝たせると、利用者が書いた値が効いていないことに気づけない。
//
// 与える情報: `host` を書かず（github.com）、`claude.env` に `GH_HOST: ghe.example.com` を書いた front matter。
// 成功条件: エラー文に GH_HOST と、両方の値が含まれること。
func TestLoad_claude_envのGH_HOSTが接続先ホストと違うと落ちる(t *testing.T) {
	front := validFrontMatter + "claude:\n  env:\n    GH_HOST: ghe.example.com\n"

	_, err := config.Load(writeWorkflow(t, front, ""))
	if err == nil {
		t.Fatal("食い違っているのにエラーが返らなかった")
	}
	for _, want := range []string{"GH_HOST", "ghe.example.com", "github.com", "tracker.provider.host"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("エラー文に %q が無い: %v", want, err)
		}
	}
}

// 目的: `claude.env` の GH_HOST が接続先ホストと同じなら、そのまま読めることを確認する。
// **既に GH_HOST を書いている利用者は、`host` を同じ値にすれば起動できる**（docs/upgrading.md）。
// 与える情報: `host: ghe.example.com` と、`claude.env` の `GH_HOST: GHE.example.com`（大文字混じり）。
// 成功条件: 読み込めること。
func TestLoad_claude_envのGH_HOSTが接続先ホストと同じなら読める(t *testing.T) {
	front := frontMatterWithHost("ghe.example.com") + "claude:\n  env:\n    GH_HOST: GHE.example.com\n"

	if _, err := config.Load(writeWorkflow(t, front, "")); err != nil {
		t.Fatalf("同じ値なのに落ちた: %v", err)
	}
}
