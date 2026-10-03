// このファイルは、CLI が接続先ホスト（tracker.provider.host）をどう決めて、どこへ渡すかを固定する
// （設計 3-86。issue #86）。**外部へ1回も接続しない。**gh と GraphQL は `cli.Deps` で差し替える。
package cli_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/cli"
	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/scaffold"
	"github.com/maimuzo/continuo/internal/setup"
	"github.com/maimuzo/continuo/internal/tracker"
)

// initRecording は `continuo init` を走らせ、検出へ渡った接続先ホストと、書かれた WORKFLOW.md を返す。
//
// t: 呼び出し元のテスト。
// args: `init` に続く引数（書き出す先のディレクトリは、この関数が後ろへ足す）。
// 戻り値: 終了コード・検出へ渡った接続先ホスト・WORKFLOW.md の全文・標準エラー。
func initRecording(t *testing.T, args ...string) (int, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	var got scaffold.DetectOptions
	deps := cli.Deps{ScaffoldDetect: func(_ context.Context, opts scaffold.DetectOptions) scaffold.Detection {
		got = opts
		return fixedDetection(context.Background(), opts)
	}}
	code, _, stderr := runCLIWith(deps, append(append([]string{"init"}, args...), dir), "")
	raw, _ := os.ReadFile(filepath.Join(dir, "WORKFLOW.md"))
	return code, got.Host, string(raw), stderr
}

// 目的: `continuo init --host` が、接続先ホストを gh の検出へ渡し、WORKFLOW.md の `host:` に書くことを確認する。
//
// **`continuo init` は WORKFLOW.md を読まない。**ホストを知る手段がフラグしか無い。
// **検出の実装が値を返さなくても書く**（接続先ホストは gh から引くものではない）。
//
// 与える情報: `--host GHE.example.com`（大文字混じり）。
// 成功条件: 終了コードが 0 で、検出へ `ghe.example.com` が渡り、WORKFLOW.md に `host: ghe.example.com` が在ること。
func TestInit_hostを検出へ渡しWORKFLOW_mdへ書く(t *testing.T) {
	t.Setenv("GH_HOST", "")

	code, host, workflow, stderr := initRecording(t, "--host", "GHE.example.com")

	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）", code, stderr)
	}
	if host != "ghe.example.com" {
		t.Errorf("検出へ渡った接続先ホストが違う: %q", host)
	}
	if !strings.Contains(workflow, "\n    host: ghe.example.com ") {
		t.Errorf("WORKFLOW.md の host が書き換わっていない")
	}
}

// 目的: `--host` を付けないときの既定が「環境変数 GH_HOST が在ればその値、無ければ github.com」で
// あることを確認する。
//
// **かつて `continuo init` は gh の環境を組まなかった。**シェルに GH_HOST を置いた機械では、
// gh がそのホストのカンバンを引いていた。**その機械で、いまと同じホストを引く。**
// 決まった値は WORKFLOW.md に書かれるので、どのホストを引いたかが目に見える。
//
// 与える情報: GH_HOST が空の場合と、`ghe.example.com` の場合。どちらも `--host` は付けない。
// 成功条件: それぞれ github.com と `ghe.example.com` が検出へ渡り、WORKFLOW.md の `host:` もその値であること。
func TestInit_hostを省くとGH_HOSTか既定のgithub_comを使う(t *testing.T) {
	t.Setenv("GH_HOST", "")
	code, host, workflow, stderr := initRecording(t)
	if code != 0 || host != "github.com" || !strings.Contains(workflow, "\n    host: github.com ") {
		t.Fatalf("GH_HOST が無いときに github.com になっていない: code=%d host=%q stderr=%s", code, host, stderr)
	}

	t.Setenv("GH_HOST", "ghe.example.com")
	code, host, workflow, stderr = initRecording(t)
	if code != 0 || host != "ghe.example.com" || !strings.Contains(workflow, "\n    host: ghe.example.com ") {
		t.Fatalf("GH_HOST の値を使っていない: code=%d host=%q stderr=%s", code, host, stderr)
	}
}

// 目的: 接続先ホストとして受け付けられない値を、gh へも雛形へも渡さずに断ることを確認する。
// 与える情報: `--host https://ghe.example.com` と、環境変数 `GH_HOST=ghe.example.com:8443`。
// 成功条件: どちらも終了コードが 2 で、WORKFLOW.md が作られず、標準エラーがどちらの指定かを名乗ること。
func TestInit_受け付けられない接続先ホストは引数の誤りで断る(t *testing.T) {
	t.Setenv("GH_HOST", "")
	code, _, workflow, stderr := initRecording(t, "--host", "https://ghe.example.com")
	if code != 2 || workflow != "" || !strings.Contains(stderr, "--host") {
		t.Errorf("--host の誤りを断っていない: code=%d workflow=%d文字 stderr=%s", code, len(workflow), stderr)
	}

	t.Setenv("GH_HOST", "ghe.example.com:8443")
	code, _, workflow, stderr = initRecording(t)
	if code != 2 || workflow != "" || !strings.Contains(stderr, "GH_HOST") {
		t.Errorf("GH_HOST の誤りを断っていない: code=%d workflow=%d文字 stderr=%s", code, len(workflow), stderr)
	}
}

// 目的: `continuo setup` が、WORKFLOW.md に書かれた接続先ホストを、検出と Status の取得の両方へ渡すことを確認する。
//
// **`continuo setup` に `--host` は無い。**WORKFLOW.md が在ることが前提だからである。
// 渡し忘れると、`host: ghe.example.com` と書いた利用者の `continuo setup` が、
// 断りなく github.com のカンバンを読みに行く。
//
// 与える情報: `host: ghe.example.com` と owner と番号を書いた WORKFLOW.md。
// 成功条件: 検出と Status の取得の両方へ `ghe.example.com` が渡ること。
func TestSetup_WORKFLOW_mdの接続先ホストを検出とStatusの取得へ渡す(t *testing.T) {
	dir := t.TempDir()
	out := scaffold.TemplateWithValues(scaffold.Values{Host: "ghe.example.com", Owner: "octocat", ProjectNumber: 3})
	if err := os.WriteFile(filepath.Join(dir, "WORKFLOW.md"), []byte(out), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
	var detectHost, fetchHost string
	deps := cli.Deps{
		ScaffoldDetect: func(_ context.Context, opts scaffold.DetectOptions) scaffold.Detection {
			detectHost = opts.Host
			return fixedDetection(context.Background(), opts)
		},
		SetupFetchStatusField: func(_ context.Context, opts setup.FetchOptions) (setup.StatusField, error) {
			fetchHost = opts.Host
			return setup.StatusField{}, errors.New("検査ではカンバンを読みません")
		},
	}

	runCLIWith(deps, []string{"setup", dir}, "")

	if detectHost != "ghe.example.com" || fetchHost != "ghe.example.com" {
		t.Fatalf("接続先ホストが渡っていない: 検出=%q Status の取得=%q", detectHost, fetchHost)
	}
}

// 目的: WORKFLOW.md の `host:` が受け付けられない形のとき、`continuo setup` が gh を1回も叩かずに
// 止まることを確認する。**黙って github.com のカンバンを読みに行かない。**
// 与える情報: `host:` に URL を書いた WORKFLOW.md。
// 成功条件: 終了コードが 1 で、検出が呼ばれず、標準エラーが `tracker.provider.host` を名指しすること。
func TestSetup_接続先ホストの形が合わなければghを叩かずに止まる(t *testing.T) {
	dir := t.TempDir()
	out := scaffold.TemplateWithValues(scaffold.Values{Owner: "octocat", ProjectNumber: 3})
	out = strings.Replace(out, "    host: github.com ", "    host: https://ghe.example.com ", 1)
	if err := os.WriteFile(filepath.Join(dir, "WORKFLOW.md"), []byte(out), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
	called := false
	deps := cli.Deps{ScaffoldDetect: func(_ context.Context, opts scaffold.DetectOptions) scaffold.Detection {
		called = true
		return fixedDetection(context.Background(), opts)
	}}

	code, _, stderr := runCLIWith(deps, []string{"setup", dir}, "")

	if code != 1 || called {
		t.Fatalf("止まっていない: code=%d 検出を呼んだ=%v", code, called)
	}
	if !strings.Contains(stderr, "tracker.provider.host") {
		t.Errorf("何が悪いのかを名指ししていない: %s", stderr)
	}
}

// 目的: `continuo prompt --show --url` が、接続先ホストと違うホストの URL を断ることを確認する。
//
// **URL から作る識別子（`<owner>/<repo>#<番号>`）はホストを持たない。**断らないと、接続先の
// カンバンに在る同じ番号の別の issue の文面を、何の断りも無く出す。
// 「本当に送られる文面を事前に確かめる」というこのコマンドの目的が外れる。
//
// 与える情報: 接続先が github.com の WORKFLOW.md と、`https://ghe.example.com/…/issues/42`。
// 成功条件: 終了コードが 1 で、issue を引く処理が呼ばれず、標準出力が空で、標準エラーに両方のホストが出ること。
func TestPromptURL_接続先ホストと違うURLを断る(t *testing.T) {
	dir := writeWorkflowFor(t)
	setBody(t, dir, "")
	called := false
	deps := cli.Deps{PromptFetchIssue: func(
		ctx context.Context, cfg config.TrackerConfig, endpoint, identifier string,
	) (tracker.Issue, bool, error) {
		called = true
		return promptFetchOK(ctx, cfg, endpoint, identifier)
	}}

	code, stdout, stderr := runCLIWith(deps,
		[]string{"prompt", "--show", "--url", "https://ghe.example.com/octocat/hello-world/issues/42", dir}, "")

	if code != 1 || called || stdout != "" {
		t.Fatalf("断っていない: code=%d 引いた=%v stdout=%d文字", code, called, len(stdout))
	}
	for _, want := range []string{"ghe.example.com", "github.com", "tracker.provider.host"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("標準エラーに %q が無い: %s", want, stderr)
		}
	}
}
