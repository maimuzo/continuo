// Package scaffold_test のうち、このファイルは `continuo init` が書き出す
// rate_limit.token_source の値を確かめる。
//
// **`continuo init` が書いた値は、そのファイルを読むときの既定値より強い。**
// macOS で `claude_credentials` と書き出すと、`~/.claude/.credentials.json` が無いのが
// 普通の環境で枠の判定が黙って効かなくなる。
package scaffold_test

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// 目的: 書き出した WORKFLOW.md の rate_limit.token_source が、走っている OS の既定と
// 一致することを確認する。
//
// 与える情報: owner と project_number を埋めた雛形の書き出し。
// 成功条件: 該当の行の値が config.DefaultConfig() の既定と一致し、config.Load で
// そのまま読み出せること。macOS ではコメントに continuo allow-keychain-access の案内があること。
func TestWriteTemplate_rate_limitのtoken_sourceがOSの既定と一致する(t *testing.T) {
	dir := t.TempDir()
	result, err := scaffold.WriteTemplateWithValues(dir, false, scaffold.Values{Owner: "acme", ProjectNumber: 3})
	if err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}

	raw, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("書き出した雛形を読めない: %v", err)
	}

	want := config.DefaultConfig().RateLimit.TokenSource
	line := rateLimitTokenSourceLine(t, string(raw))
	if !strings.HasPrefix(line, "  token_source: "+want) {
		t.Fatalf("雛形の rate_limit.token_source が OS の既定と違う（OS: %s, 既定: %q）: %q",
			runtime.GOOS, want, line)
	}

	loaded, err := config.Load(result.Path)
	if err != nil {
		t.Fatalf("書き出した雛形を読み込めなかった: %v", err)
	}
	if got := loaded.Config.RateLimit.TokenSource; got != want {
		t.Fatalf("読み込んだ rate_limit.token_source が違う: got %q, want %q", got, want)
	}

	if runtime.GOOS == "darwin" {
		// **先に許可を取らせる案内を残す。**これが無いと、無人のプロセスが確認の
		// ダイアログに当たって枠を読めないまま終わる。
		if !strings.Contains(line, "allow-keychain-access") {
			t.Fatalf("keychain を書いたのに、先に許可を取る案内がコメントに無い: %q", line)
		}
	}
}

// rateLimitTokenSourceLine は WORKFLOW.md の rate_limit.token_source の行を取り出す。
//
// **tracker.provider.token_source と取り違えない。**そちらはインデントが4文字である。
//
// t: テストコンテキスト。見つからなければテストを止める。
// content: WORKFLOW.md の全文。
// 戻り値: 該当する行（改行は含まない）。
func rateLimitTokenSourceLine(t *testing.T, content string) string {
	t.Helper()
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "  token_source: ") {
			return line
		}
	}
	t.Fatal("雛形に rate_limit.token_source の行が無い")
	return ""
}
