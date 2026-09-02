// 起動の段で送るプロンプトを組み立て、変数を確かめる部分の検査である（設計 5-3c）。
package daemon_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/daemon"
	"github.com/maimuzo/continuo/internal/prompt"
)

// 目的: WORKFLOW.md の本文に知らない変数を書いたら、起動の時点で止まることを確かめる
// （設計 5-3c）。
//
// **本文は「組み込みの真ん中へ挟む固有の指示」であり、組み込みと同じ扱いで検査する。**
// 着手の時点で1件ずつ失敗させると、常駐プロセスは健康に見えたまま issue が全部落ちる。
// **起動で止めれば、人間はすぐ気づく。**
//
// 与える情報: `{{.issue.nope}}` を本文に書いた WORKFLOW.md。
// 成功条件: 起動の段のエラー（daemon.ErrStartup）で止まり、
// 文言が本文の断片の名前と `continuo prompt --show` の案内を含むこと。
func TestRun_本文に知らない変数があれば起動を止める(t *testing.T) {
	root := wiringRoot(t)
	path := writeWiringWorkflow(t, root, "", "")
	setBodyOf(t, path, "{{.issue.nope}}\n")
	setWiringEnv(t, root)

	err := daemon.Run(context.Background(), daemon.Options{
		ConfigPath: path,
		Logger:     slog.New(slog.DiscardHandler),
	})
	if err == nil {
		t.Fatal("知らない変数があるのに起動できてしまった")
	}
	if !errors.Is(err, daemon.ErrStartup) {
		t.Fatalf("起動の段の失敗として印が付いていない: %v", err)
	}
	if !strings.Contains(err.Error(), prompt.NameWorkflowBody) {
		t.Errorf("どの断片の話かが文言に出ていない（%s が無い）: %v", prompt.NameWorkflowBody, err)
	}
	if !strings.Contains(err.Error(), "continuo prompt --show") {
		t.Errorf("直し方の案内が文言に出ていない: %v", err)
	}
}

// 目的: `{{if .attempt}}` の中だけに書いた誤りでも、起動の時点で止まることを確かめる
// （設計 5-3c）。
//
// **1回しか変数展開しないと、この誤りは見つからない。**`.attempt` は1回目が空なので、
// 中は一度も解釈されず、**やり直しが起きるまで表に出ない。**
//
// 与える情報: `{{if .attempt}}{{.issue.nope}}{{end}}` を本文に書いた WORKFLOW.md。
// 成功条件: 起動の段のエラーで止まること。
func TestRun_attemptの中の誤りでも起動を止める(t *testing.T) {
	root := wiringRoot(t)
	path := writeWiringWorkflow(t, root, "", "")
	setBodyOf(t, path, "{{if .attempt}}{{.issue.nope}}{{end}}\n")
	setWiringEnv(t, root)

	err := daemon.Run(context.Background(), daemon.Options{
		ConfigPath: path,
		Logger:     slog.New(slog.DiscardHandler),
	})
	if err == nil {
		t.Fatal("{{if .attempt}} の中の誤りがあるのに起動できてしまった")
	}
	if !errors.Is(err, daemon.ErrStartup) {
		t.Fatalf("起動の段の失敗として印が付いていない: %v", err)
	}
}

// 目的: 本文が正しければ、プロンプトを理由に起動が止まらないことを確かめる（設計 5-3c）。
//
// **組み込みそのものが壊れていたら、利用者の側では直しようが無い。**
//
// 与える情報: 一覧にある変数だけを使った本文。
// 成功条件: プロンプトが理由で止まらないこと（この先の段で別の理由により止まるのは構わない）。
func TestRun_正しい本文ならプロンプトを理由に止まらない(t *testing.T) {
	root := wiringRoot(t)
	path := writeWiringWorkflow(t, root, "", "")
	setBodyOf(t, path, "{{.issue.owner}}/{{.issue.repo}} の決まりに従ってください。\n")
	setWiringEnv(t, root)

	err := daemon.Run(context.Background(), daemon.Options{
		ConfigPath: path,
		Logger:     slog.New(slog.DiscardHandler),
	})
	// **この先の段（herdr への接続など）で止まるのは構わない。**
	// **プロンプトを理由に止まっていないこと**だけを確かめる。
	if err != nil && strings.Contains(err.Error(), "送るプロンプトに誤りがあります") {
		t.Fatalf("正しい本文なのにプロンプトを理由に起動を止めている: %v", err)
	}
}

// setBodyOf は WORKFLOW.md の本文（front matter の閉じの `---` より下）を置き換える。
//
// **front matter は1文字も触らない。**設定を変えずに、送る文面だけを変えるためである。
//
// t: 呼び出し元のテスト。
// path: WORKFLOW.md の絶対パス。
// body: 置き換える本文。空文字なら本文を消す。
func setBodyOf(t *testing.T, path, body string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s を読めません: %v", path, err)
	}
	lines := strings.Split(string(raw), "\n")
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") != "---" {
			continue
		}
		out := strings.Join(lines[:i+1], "\n") + "\n" + body
		if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
			t.Fatalf("%s を書けません: %v", path, err)
		}
		return
	}
	t.Fatalf("%s に front matter の終端行がありません", path)
}

// setWiringEnv は、実行時ディレクトリと GraphQL の接続先の環境変数を、
// テストの一時ディレクトリへ向ける。
//
// t: 呼び出し元のテスト。
// root: 一時ディレクトリ。
func setWiringEnv(t *testing.T, root string) {
	t.Helper()
	runtimeDir := filepath.Join(root, "rt")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("実行時ディレクトリを作れません: %v", err)
	}
	t.Setenv(daemon.EnvRuntimeDir, runtimeDir)
	t.Setenv(daemon.EnvGraphQLEndpoint, "")
}
