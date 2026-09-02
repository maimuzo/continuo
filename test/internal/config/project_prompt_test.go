// `config.Load` が PROJECT_SPECIFIC_PROMPT.md をどう読むかの検査である（設計 5-3c）。
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/prompt"
)

// 目的: 固有のプロンプトが無くても Load が通り、置き場所だけが返ることを確かめる。
//
// **固有の指示が要らない project がある。**無いことを誤りにしてはならない。
// **それでもパスは返す。**「どこに置けばよいか」を案内に出すためである。
//
// 与える情報: WORKFLOW.md だけを置いたディレクトリ。
// 成功条件: Load が通り、`ProjectPromptFound` が偽で、`ProjectPromptPath` が
// WORKFLOW.md と同じディレクトリを指すこと。
func TestLoad_固有のプロンプトが無くても通る(t *testing.T) {
	path := writeWorkflow(t, validFrontMatter, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load が落ちました: %v", err)
	}
	if loaded.ProjectPromptFound {
		t.Error("置いていないのに ProjectPromptFound が真です")
	}
	if loaded.ProjectPrompt != "" {
		t.Errorf("置いていないのに中身が入っています: %q", loaded.ProjectPrompt)
	}
	want := filepath.Join(filepath.Dir(path), prompt.ProjectFileName)
	if loaded.ProjectPromptPath != want {
		t.Errorf("置き場所が違います\n  got:  %s\n  want: %s", loaded.ProjectPromptPath, want)
	}
	if loaded.ProjectPromptErr != nil {
		t.Errorf("無いだけで誤りが入っています: %v", loaded.ProjectPromptErr)
	}
}

// 目的: 固有のプロンプトが在れば、その中身が返ることを確かめる。
//
// 与える情報: `## 固有\n` を書いた PROJECT_SPECIFIC_PROMPT.md。
// 成功条件: `ProjectPromptFound` が真で、中身が1バイトも違わないこと。
func TestLoad_固有のプロンプトを読む(t *testing.T) {
	path := writeWorkflow(t, validFrontMatter, "")
	const body = "## 固有\n"
	writeProjectPrompt(t, filepath.Dir(path), body)

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load が落ちました: %v", err)
	}
	if !loaded.ProjectPromptFound {
		t.Error("置いたのに ProjectPromptFound が偽です")
	}
	if loaded.ProjectPrompt != body {
		t.Errorf("中身が違います\n  got:  %q\n  want: %q", loaded.ProjectPrompt, body)
	}
}

// 目的: 固有のプロンプトが在るのに読めなくても、`config.Load` が落ちないことを確かめる
// （設計 5-3c）。
//
// **ここが落ちると、プロンプトを1文字も送らないコマンドまで止まる。**
// `config.Load` を呼ぶのは常駐プロセスの起動だけではなく、
// `continuo trust` / `continuo abandon` / `continuo doctor` も呼ぶ。
// **とくに doctor は、設定の検査が `✗` になるとほぼ全部の検査の記号がそれに引きずられ、
// 原因を調べる道具そのものが使えなくなる。**
//
// 与える情報: PROJECT_SPECIFIC_PROMPT.md という名前のディレクトリ
// （読めないファイルを、権限に頼らずに作る手段である。root で走らせても読めない）。
// 成功条件: Load が通り、`ProjectPromptErr` に理由が入っていること。
func TestLoad_固有のプロンプトが読めなくてもLoadは落ちない(t *testing.T) {
	path := writeWorkflow(t, validFrontMatter, "")
	blocked := filepath.Join(filepath.Dir(path), prompt.ProjectFileName)
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatalf("ディレクトリを作れません: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load が落ちました（落ちてはいけません）: %v", err)
	}
	if loaded.ProjectPromptErr == nil {
		t.Fatal("読めないのに ProjectPromptErr が nil です（起動も doctor も気づけません）")
	}
	if !loaded.ProjectPromptFound {
		t.Error("在るのに ProjectPromptFound が偽です")
	}
}

// writeProjectPrompt は PROJECT_SPECIFIC_PROMPT.md を1つ置く。
//
// t: 呼び出し元のテスト。
// dir: 置く先のディレクトリ。
// body: 書く中身。
func writeProjectPrompt(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, prompt.ProjectFileName), []byte(body), 0o600); err != nil {
		t.Fatalf("%s を書けません: %v", prompt.ProjectFileName, err)
	}
}
