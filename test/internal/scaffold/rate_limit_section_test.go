// Package scaffold_test のうち、このファイルは `continuo init` が書き出す
// rate_limit の節を確かめる（issue #284）。
//
// **`continuo init` が書いた値は、そのファイルを読むときの既定値より強い。**
// 雛形の rate_limit の節が既定値とずれていると、書き出した WORKFLOW.md で別の動きになる。
package scaffold_test

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// 目的: 書き出した WORKFLOW.md の rate_limit の節が、usage API を主に読み、エラーのときは
// statusline取得へ切り替える形であり、既定値と一致して、そのまま読み込めることを確認する（issue #284）。
//
// 与える情報: owner と project_number を埋めた雛形の書き出し。
// 成功条件: rate_limit の節に `source: oauth_usage_api` と、既定値と同じ `poll_interval_ms` と
// `refresh_interval_ms` の行と、`token_env` の行があること。コメントに「API キーの機械は none」の
// 案内があること。config.Load で読み込め、source・poll_interval_ms・refresh_interval_ms が既定値と一致すること。
func TestWriteTemplate_rate_limitの節はweb_APIを主に読む形である(t *testing.T) {
	dir := t.TempDir()
	result, err := scaffold.WriteTemplateWithValues(dir, false, scaffold.Values{Owner: "acme", ProjectNumber: 3})
	if err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}

	raw, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("書き出した雛形を読めない: %v", err)
	}
	content := string(raw)
	def := config.DefaultConfig().RateLimit

	section := rateLimitSection(t, content)
	if def.Source != config.RateLimitSourceOAuthUsageAPI {
		t.Fatalf("既定の rate_limit.source が oauth_usage_api でない（前提が崩れている）: %q", def.Source)
	}
	// **tracker.provider にも token_source / token_env がある**（GitHub のトークンの出所）。
	// rate_limit の節の中だけを見て取り違えない。
	for _, want := range []string{
		"  source: " + def.Source,
		"  poll_interval_ms: " + strconv.Itoa(def.PollIntervalMs),
		"  refresh_interval_ms: " + strconv.Itoa(def.RefreshIntervalMs),
		"  token_env: " + def.TokenEnv,
	} {
		if !hasLinePrefix(section, want) {
			t.Errorf("rate_limit の節に %q の行が無い:\n%s", want, strings.Join(section, "\n"))
		}
	}
	// **API キーの機械は none にさせる案内を残す。**しないと、立て直すたびに haiku の会話が
	// 従量で課金されうる（statusline取得へ切り替えるため）。
	if !strings.Contains(strings.Join(section, "\n"), "API キーの機械は none") {
		t.Errorf("rate_limit の節に「API キーの機械は none」の案内が無い:\n%s", strings.Join(section, "\n"))
	}

	loaded, err := config.Load(result.Path)
	if err != nil {
		t.Fatalf("書き出した雛形を読み込めなかった: %v", err)
	}
	if got := loaded.Config.RateLimit.Source; got != def.Source {
		t.Errorf("読み込んだ rate_limit.source が違う: got %q, want %q", got, def.Source)
	}
	if got := loaded.Config.RateLimit.PollIntervalMs; got != def.PollIntervalMs {
		t.Errorf("読み込んだ rate_limit.poll_interval_ms が違う: got %d, want %d", got, def.PollIntervalMs)
	}
	if got := loaded.Config.RateLimit.RefreshIntervalMs; got != def.RefreshIntervalMs {
		t.Errorf("読み込んだ rate_limit.refresh_interval_ms が違う: got %d, want %d", got, def.RefreshIntervalMs)
	}
}

// rateLimitSection は WORKFLOW.md から rate_limit の節の行（見出しの行を除く）を取り出す。
//
// 節は `rate_limit:` の行の次から、インデントの無い次の行（次の節の見出し）の手前までである。
// 空行とコメントだけの行も含めて返す。
//
// t: テストコンテキスト。節が見つからなければテストを止める。
// content: WORKFLOW.md の全文。
// 戻り値: 節の中の行（改行は含まない）。
func rateLimitSection(t *testing.T, content string) []string {
	t.Helper()
	var out []string
	in := false
	for _, line := range strings.Split(content, "\n") {
		if line == "rate_limit:" {
			in = true
			continue
		}
		if !in {
			continue
		}
		if line != "" && !strings.HasPrefix(line, " ") {
			break
		}
		out = append(out, line)
	}
	if !in {
		t.Fatal("雛形に rate_limit の節が無い")
	}
	return out
}

// hasLinePrefix は、行のどれかが prefix で始まるかを返す。
//
// lines: 調べる行。
// prefix: 行の頭に来るべき文字列。
// 戻り値: 1行でも当たれば true。
func hasLinePrefix(lines []string, prefix string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}
