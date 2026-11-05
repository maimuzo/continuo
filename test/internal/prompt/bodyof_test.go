package prompt_test

// prompt.BodyOf（送る本文を WORKFLOW.md と WORKFLOW.local.md から選ぶ。設計 5-1a / issue #325）を確かめる。

import (
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/prompt"
)

// 目的: WORKFLOW.local.md の本文の扱いを確かめる。中身があれば丸ごと差し替え、
// 送る前に取り除く HTML コメントと空の見出しだけなら WORKFLOW.md の本文を残す（設計レビュー1周目の HIGH）。
// 与える情報: WORKFLOW.local.md の有無と本文の4通り。
// 成功条件: 表のとおりの本文とパスが返ること。
func TestBodyOf_WORKFLOW_local_mdの本文に中身があれば差し替える(t *testing.T) {
	const team, mine = "/w/WORKFLOW.md", "/w/WORKFLOW.local.md"
	for _, c := range []struct {
		name      string
		localPath string
		localBody string
		wantBody  string
		wantPath  string
	}{
		{"WORKFLOW.local.md が無い", "", "", "チームの本文\n", team},
		{"本文が空", mine, "\n\n", "チームの本文\n", team},
		{"案内のコメントと空の見出しだけ", mine, "<!-- 本文はチームのものを使う -->\n## 個人の決まり\n", "チームの本文\n", team},
		{"中身がある", mine, "## 個人の決まり\n\n日本語で書く。\n", "## 個人の決まり\n\n日本語で書く。\n", mine},
	} {
		t.Run(c.name, func(t *testing.T) {
			loaded := &config.Loaded{
				PromptTemplate: "チームの本文\n", Path: team,
				LocalPath: c.localPath, LocalPromptTemplate: c.localBody,
			}
			body, path := prompt.BodyOf(loaded)
			if body != c.wantBody || path != c.wantPath {
				t.Errorf("BodyOf = (%q, %q)、want (%q, %q)", body, path, c.wantBody, c.wantPath)
			}
			if got := prompt.BuildLoaded(loaded).BodyPath(); got != c.wantPath {
				t.Errorf("BuildLoaded の本文のパス = %q、want %q", got, c.wantPath)
			}
		})
	}
}
