// このファイルは、接続先ホスト（tracker.provider.host）を雛形へ書く・既にある WORKFLOW.md から拾う・
// `gh` へ渡す、の3つを固定する（設計 3-86。issue #86）。
package scaffold_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/scaffold"
)

// 目的: 雛形の `host:` の行が、渡した接続先ホストで書き換わることを確認する。
// 与える情報: `Host` が空・github.com・GitHub Enterprise のホスト名・形の合わない値の4通り。
// 成功条件: GitHub Enterprise のホスト名のときだけ行が書き換わり、それ以外は `host: github.com` が残ること。
// **形の合わない値は書き込まない**（YAML と `gh` の引数を壊す値を、雛形へ残さない）。
func TestTemplateWithValues_接続先ホストを書く(t *testing.T) {
	cases := []struct {
		host string
		want string
	}{
		{"", "    host: github.com "},
		{"github.com", "    host: github.com "},
		{"ghe.example.com", "    host: ghe.example.com "},
		{"GHE.Example.com", "    host: ghe.example.com "},
		{"https://ghe.example.com", "    host: github.com "},
	}
	for _, tc := range cases {
		out := scaffold.TemplateWithValues(scaffold.Values{Host: tc.host})
		if !strings.Contains(out, tc.want) {
			t.Errorf("Host %q のとき、雛形に %q が無い", tc.host, tc.want)
		}
		if n := strings.Count(out, "\n    host: "); n != 1 {
			t.Errorf("Host %q のとき、`host:` の行が %d 行ある（1行のはず）", tc.host, n)
		}
	}
}

// 目的: `continuo setup` が、既にある WORKFLOW.md から接続先ホストを拾えることを確認する。
//
// **設定として読み込まずに、原文から拾う。**`continuo setup` は、プレースホルダの残った
// WORKFLOW.md に対しても走る。設定として読み込むと、そのとき `host` を読めず、
// 断りなく github.com のカンバンを読みに行く。
//
// 与える情報: 雛形のまま（owner がプレースホルダ）・`host` を書き換えたもの・引用符つきのもの。
// 成功条件: それぞれ github.com・書いたホスト名・引用符を外したホスト名が Result.Host に入ること。
func TestCheckUpdatable_既に書かれている接続先ホストを返す(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"", "github.com"},
		{"    host: ghe.example.com", "ghe.example.com"},
		{`    host: "ghe.example.com"`, "ghe.example.com"},
		{"    host: 'Octocorp.GHE.com'   # 接続先", "octocorp.ghe.com"},
	}
	for _, tc := range cases {
		got, err := scaffold.CheckUpdatable(writeWorkflow(t, tc.line))
		if err != nil {
			t.Fatalf("%q を読めなかった: %v", tc.line, err)
		}
		if got.Host != tc.want {
			t.Errorf("%q から拾った接続先ホストが違う: got %q, want %q", tc.line, got.Host, tc.want)
		}
	}
}

// 目的: `host:` の行が在るのに形が合わないとき、黙って github.com に倒さないことを確認する。
// 与える情報: `host:` に URL を書いた WORKFLOW.md と、値を空にした WORKFLOW.md。
// 成功条件: errors.Is で ErrHostInvalid と判定できるエラーが返ること。
func TestCheckUpdatable_接続先ホストの形が合わなければ止める(t *testing.T) {
	for _, line := range []string{"    host: https://ghe.example.com", "    host:"} {
		_, err := scaffold.CheckUpdatable(writeWorkflow(t, line))
		if !errors.Is(err, scaffold.ErrHostInvalid) {
			t.Errorf("%q で ErrHostInvalid が返っていない: %v", line, err)
		}
	}
}

// 目的: `RunGHForHost` が、接続先ホストを環境変数 GH_HOST として gh へ渡すことを確認する。
//
// **`gh project …` には `--hostname` が無い。**宛先を決められるのは環境変数だけである。
// **親の環境に別の GH_HOST が在っても、渡したホストが勝つこと。**
//
// 与える情報: 環境変数 GH_HOST を標準出力へ出す偽の `gh` と、親の環境の `GH_HOST=other.example.com`。
// 成功条件: 偽の `gh` が見た GH_HOST が、渡した接続先ホストであること。空文字なら github.com。
func TestRunGHForHost_接続先ホストをGH_HOSTとして渡す(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"),
		[]byte("#!/bin/sh\nprintf '%s' \"${GH_HOST:-}\"\n"), 0o755); err != nil {
		t.Fatalf("テスト用gh mock を書けません: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_HOST", "other.example.com")

	for host, want := range map[string]string{"ghe.example.com": "ghe.example.com", "": "github.com"} {
		out, err := scaffold.RunGHForHost(host)(context.Background(), "project", "list")
		if err != nil {
			t.Fatalf("gh の実行に失敗した: %v", err)
		}
		if string(out) != want {
			t.Errorf("host %q のとき gh が見た GH_HOST が違う: got %q, want %q", host, out, want)
		}
	}
}

// 目的: `Detect` が、渡された接続先ホストをそのまま雛形へ書く値に入れ、案内にも入れることを確認する。
// 与える情報: `Host` が `ghe.example.com` で、gh が「ログインしていない」で落ちる状態。
// 成功条件: Values.Host が `ghe.example.com` で、owner の案内に
// `gh auth login --hostname ghe.example.com` と `--host` の両方が出ること。
func TestDetect_接続先ホストを値と案内に入れる(t *testing.T) {
	d := scaffold.Detect(context.Background(), scaffold.DetectOptions{
		Host: "ghe.example.com",
		RunGH: func(context.Context, ...string) ([]byte, error) {
			return nil, errors.New("gh: not logged in")
		},
	})

	if d.Values.Host != "ghe.example.com" {
		t.Fatalf("雛形へ書く接続先ホストが違う: %q", d.Values.Host)
	}
	advice := strings.Join(d.Fields[0].Advice, "\n")
	for _, want := range []string{"gh auth login --hostname ghe.example.com -s project", "--host"} {
		if !strings.Contains(advice, want) {
			t.Errorf("owner の案内に %q が無い: %s", want, advice)
		}
	}
}

// 目的: CI の雛形が、GitHub Enterprise でも紐づく issue を数えられる形になっていることを確認する（issue #86）。
//
// **`https://github.com/` を決め打ちすると、GitHub Enterprise では1件も当たらず、検査が必ず落ちる。**
// **`GH_TOKEN` は残す。**gh は github.com と `<名前>.ghe.com` では GH_TOKEN を、
// GitHub Enterprise Server では GH_ENTERPRISE_TOKEN を読む。
//
// 与える情報: 雛形の全文。
// 成功条件: `https://github.com/` の決め打ちが無く、URL の頭を GITHUB_SERVER_URL から jq へ渡し、
// 2つの job の両方が GH_TOKEN と GH_ENTERPRISE_TOKEN を持ち、GH_HOST を置いていること。
func TestCITemplate_GitHub_Enterpriseでも数えられる形になっている(t *testing.T) {
	tmpl := scaffold.CITemplate()

	if strings.Contains(tmpl, `"https://github.com/"`) {
		t.Error("issue の URL の頭を https://github.com/ と決め打ちしている")
	}
	if n := strings.Count(tmpl, `--arg server "${GITHUB_SERVER_URL}"`); n != 2 {
		t.Errorf("URL の頭を jq へ渡している箇所が %d（2のはず）", n)
	}
	if n := strings.Count(tmpl, `startswith($server + "/" + $repo + "/issues/")`); n != 2 {
		t.Errorf("渡した URL の頭で比べている箇所が %d（2のはず）", n)
	}
	for _, want := range []string{
		"GH_TOKEN: ${{ github.token }}",
		"GH_ENTERPRISE_TOKEN: ${{ github.token }}",
		`GH_HOST="${GITHUB_SERVER_URL#*://}"`,
		"export GH_HOST",
	} {
		if n := strings.Count(tmpl, want); n != 2 {
			t.Errorf("%q が %d 箇所（job ごとに1つで、2のはず）", want, n)
		}
	}
}
