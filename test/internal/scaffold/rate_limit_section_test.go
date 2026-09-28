// Package scaffold_test のうち、このファイルは `continuo init` が書き出す
// rate_limit の節を確かめる（issue #284）。
//
// **`continuo init` が書いた値は、そのファイルを読むときの既定値より強い。**
// 雛形に消したキー（token_source / token_env / poll_interval_ms）や消した値
// （oauth_usage_api）が残っていると、書き出した WORKFLOW.md がそのまま起動時の検査で止まる。
package scaffold_test

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// 目的: 書き出した WORKFLOW.md の rate_limit の節が、ステータスラインから使用率を受ける
// 新しい形であり、既定値と一致して、そのまま読み込めることを確認する。
//
// 与える情報: owner と project_number を埋めた雛形の書き出し。
// 成功条件: rate_limit の節に `source: statusline` と、既定値と同じ `refresh_interval_ms` の行が
// あること。節の中に消したキー（token_source / token_env / poll_interval_ms）が無く、
// 雛形のどこにも oauth_usage_api と allow-keychain-access が無いこと。
// config.Load で読み込め、source と refresh_interval_ms が既定値と一致すること。
func TestWriteTemplate_rate_limitの節はstatuslineから受ける形である(t *testing.T) {
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
	wantSource := "  source: " + def.Source
	wantRefresh := "  refresh_interval_ms: " + strconv.Itoa(def.RefreshIntervalMs)
	if def.Source != config.RateLimitSourceStatusline {
		t.Fatalf("既定の rate_limit.source が statusline でない（前提が崩れている）: %q", def.Source)
	}
	if !hasLinePrefix(section, wantSource) {
		t.Errorf("rate_limit の節に %q の行が無い:\n%s", wantSource, strings.Join(section, "\n"))
	}
	if !hasLinePrefix(section, wantRefresh) {
		t.Errorf("rate_limit の節に %q の行が無い:\n%s", wantRefresh, strings.Join(section, "\n"))
	}
	// **tracker.provider にも token_source / token_env がある**（GitHub のトークンの出所）。
	// rate_limit の節の中だけを見て取り違えない。
	for _, removed := range []string{"token_source:", "token_env:", "poll_interval_ms:"} {
		if hasLinePrefix(section, "  "+removed) {
			t.Errorf("rate_limit の節に消したキー %q が残っている:\n%s", removed, strings.Join(section, "\n"))
		}
	}
	for _, removed := range []string{"oauth_usage_api", "allow-keychain-access"} {
		if strings.Contains(content, removed) {
			t.Errorf("雛形に消した %q が残っている", removed)
		}
	}

	loaded, err := config.Load(result.Path)
	if err != nil {
		t.Fatalf("書き出した雛形を読み込めなかった: %v", err)
	}
	if got := loaded.Config.RateLimit.Source; got != def.Source {
		t.Errorf("読み込んだ rate_limit.source が違う: got %q, want %q", got, def.Source)
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
