package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/prompt"
)

// builtinOnlyBody は、送る文面が組み込みだけになる WORKFLOW.md の本文を返す。
//
// **返すのは空文字である。**`prompt.Build` は組み込みの前半と後半を必ず前後に付けるので
// （設計 5-3c）、**本文を空にすれば、送られるのは組み込みの全文そのものになる。**
//
// **ここで組み込みの全文を返してはならない。**返すと、それが本文として真ん中に挟まり、
// **同じ文面が2回送られる。**コマンドの行を数える検査が、どれも2倍を見ることになる。
//
// **組み込みそのものが空でないことは、ここで確かめておく。**空のまま検査が全部通ると、
// 「コマンドが1本も無い」ことに誰も気づかない。
//
// t: 呼び出し元のテスト。
// 戻り値: 空文字。
func builtinOnlyBody(t *testing.T) string {
	t.Helper()
	if strings.TrimSpace(prompt.Builtin()) == "" {
		t.Fatal("組み込みのプロンプトが空です")
	}
	return ""
}

// TestPrompt_雛形の本文はPRのコメントとレビューを読ませる は、issue #34 を確かめる。
//
// 目的: **PR ができたあと、レビューの指摘は PR に書かれる。**それなのに、
// 雛形が読ませていたのは issue のコメントだけだった。
// **雛形が渡す手順に、その issue に紐づく PR を全部出す段と、行に紐づく
// レビューコメントを読む段が入っている**ことを、**実際に変数展開したプロンプトで**確かめる。
//
// **行に紐づくレビューコメントは `gh pr view --comments` に1件も出ない**
// （cli/cli の PR #3 で実測。`command/pr.go:297` のレビューコメントが出力に無い）。
// **だから `gh api` の `pulls/<番号>/comments` を必ず読ませる。**
//
// 与える情報: `continuo init` が置く雛形の本文そのものと、`octocat/hello-world#188` の issue。
// 成功条件: 送られた本文に、PR の番号を出すコマンドと、
// 行に紐づくレビューコメントを読むコマンドが、**issue の値に置き換わった形で**入っていること。
func TestPrompt_雛形の本文はPRのコメントとレビューを読ませる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		PromptTemplate: builtinOnlyBody(t),
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	prompts := recordPrompts(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "1回目の本文が送られる", func() bool {
		return len(prompts()) > 0
	})

	got := prompts()[0]

	// **その issue に紐づく PR を全部出す。**閉じる指定のある PR と、
	// issue を参照しているだけの PR の両方を拾う。
	wantEach := []string{
		"gh pr list --repo octocat/hello-world --state all --limit 100",
		"closingIssuesReferences[]?; .number == 188",
		"gh api repos/octocat/hello-world/issues/188/timeline",
		`select(.event == "cross-referenced")`,
		// **行に紐づくレビューコメント。**`gh pr view --comments` には出ない。
		"gh api repos/octocat/hello-world/pulls/<PR番号>/comments",
		// **レビューの判定と本文。**
		"gh api repos/octocat/hello-world/pulls/<PR番号>/reviews",
	}
	for _, want := range wantEach {
		if !strings.Contains(got, want) {
			t.Fatalf("PR を読ませる手順が本文に入っていない（%q が無い）:\n%s", want, got)
		}
	}

	// **プレースホルダを残したまま送っていない。**`missingkey=error` は未知の変数だけを
	// 落とすので、**書き間違えた変数名がそのまま本文に残る形も弾いておく。**
	if strings.Contains(got, "{{") {
		t.Fatalf("変数展開されなかったテンプレートの記法が本文に残っている:\n%s", got)
	}
}
