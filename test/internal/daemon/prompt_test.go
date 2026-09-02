// 起動の段で送るプロンプトを組み立て、変数を確かめる部分の検査である（設計 5-3c / 5-3d）。
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

// 目的: 固有のプロンプトに知らない変数を書いたら、起動の時点で止まることを確かめる
// （設計 5-3c）。
//
// **いままでは着手の時点で1件ずつ失敗していた。**常駐プロセスは健康に見えたまま、
// issue が全部落ちる。**起動で止めれば、人間はすぐ気づく。**
//
// 与える情報: `{{.issue.nope}}` を書いた PROJECT_SPECIFIC_PROMPT.md。
// 成功条件: 起動の段のエラー（daemon.ErrStartup）で止まり、
// 文言がファイルの名前と `continuo prompt --show` の案内を含むこと。
func TestRun_固有のプロンプトに知らない変数があれば起動を止める(t *testing.T) {
	root := wiringRoot(t)
	path := writeWiringWorkflow(t, root, "", "")
	// **本文を消す。**本文が残っていると互換の経路になり、検査は警告に留まる（設計 5-3d）。
	dropBody(t, path)
	writeProjectPromptAt(t, root, "{{.issue.nope}}\n")
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
	if !strings.Contains(err.Error(), prompt.ProjectFileName) {
		t.Errorf("どのファイルの話かが文言に出ていない: %v", err)
	}
	if !strings.Contains(err.Error(), "continuo prompt --show") {
		t.Errorf("直し方の案内が文言に出ていない: %v", err)
	}
}

// 目的: 固有のプロンプトが在るのに読めなければ、起動を止めることを確かめる（設計 5-3c）。
//
// **黙って無視しない。**無視すると、書いたはずの流儀が効かないまま無人で回り続ける。
//
// 与える情報: PROJECT_SPECIFIC_PROMPT.md という名前のディレクトリ。
// 成功条件: 起動の段のエラーで止まり、文言がファイルの名前を含むこと。
func TestRun_固有のプロンプトが読めなければ起動を止める(t *testing.T) {
	root := wiringRoot(t)
	path := writeWiringWorkflow(t, root, "", "")
	dropBody(t, path)
	if err := os.Mkdir(filepath.Join(root, prompt.ProjectFileName), 0o755); err != nil {
		t.Fatalf("ディレクトリを作れません: %v", err)
	}
	setWiringEnv(t, root)

	err := daemon.Run(context.Background(), daemon.Options{
		ConfigPath: path,
		Logger:     slog.New(slog.DiscardHandler),
	})
	if err == nil {
		t.Fatal("読めない固有のプロンプトがあるのに起動できてしまった")
	}
	if !errors.Is(err, daemon.ErrStartup) {
		t.Fatalf("起動の段の失敗として印が付いていない: %v", err)
	}
	if !strings.Contains(err.Error(), prompt.ProjectFileName) {
		t.Errorf("どのファイルの話かが文言に出ていない: %v", err)
	}
}

// 目的: WORKFLOW.md に残っている本文の変数の誤りでは、起動を止めないことを確かめる
// （設計 5-3d）。
//
// **いままで本文は着手のたびに解釈されており、`{{if .attempt}}` の中の誤りは
// やり直しが起きるまで表に出なかった。**その状態の人が版を上げたときに、
// **いままで動いていた continuo が起動しなくなってはいけない。**
//
// 与える情報: `{{if .attempt}}{{.issue.nope}}{{end}}` を本文に書いた WORKFLOW.md。
// 成功条件: プロンプトが理由で止まらないこと（この先の段で別の理由により止まるのは構わない）。
func TestRun_残った本文の変数の誤りでは起動を止めない(t *testing.T) {
	root := wiringRoot(t)
	path := writeWiringWorkflow(t, root, "", "")
	appendToFile(t, path, "{{if .attempt}}{{.issue.nope}}{{end}}\n")
	setWiringEnv(t, root)

	err := daemon.Run(context.Background(), daemon.Options{
		ConfigPath: path,
		Logger:     slog.New(slog.DiscardHandler),
	})
	// **この先の段（herdr への接続など）で止まるのは構わない。**
	// **プロンプトを理由に止まっていないこと**だけを確かめる。
	if err != nil && strings.Contains(err.Error(), "送るプロンプトに誤りがあります") {
		t.Fatalf("残った本文の誤りで起動を止めている（版を上げた瞬間に動かなくなる）: %v", err)
	}
}

// dropBody は WORKFLOW.md の閉じの "---" より下を消す（設計 5-3c の状態にする）。
//
// t: 呼び出し元のテスト。
// path: WORKFLOW.md の絶対パス。
func dropBody(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s を読めません: %v", path, err)
	}
	lines := strings.Split(string(raw), "\n")
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") == "---" {
			out := strings.Join(lines[:i+1], "\n") + "\n"
			if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
				t.Fatalf("%s を書けません: %v", path, err)
			}
			return
		}
	}
	t.Fatalf("%s に front matter の終端行がありません", path)
}

// writeProjectPromptAt は root の直下に PROJECT_SPECIFIC_PROMPT.md を置く。
//
// t: 呼び出し元のテスト。
// root: 置く先のディレクトリ。
// body: 書く中身。
func writeProjectPromptAt(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, prompt.ProjectFileName), []byte(body), 0o600); err != nil {
		t.Fatalf("%s を書けません: %v", prompt.ProjectFileName, err)
	}
}

// appendToFile はファイルの末尾へ書き足す。
//
// t: 呼び出し元のテスト。
// path: 書き足す先の絶対パス。
// body: 書き足す中身。
func appendToFile(t *testing.T, path, body string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s を読めません: %v", path, err)
	}
	if err := os.WriteFile(path, append(raw, []byte(body)...), 0o600); err != nil {
		t.Fatalf("%s を書けません: %v", path, err)
	}
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
