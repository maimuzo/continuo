package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// attributionExe は、GitHub App の attribution の検査で continuo の実行ファイルとして渡すパスである。
//
// **空白を入れてある。**単一引用符で包まれていなければ、エージェントの手元で
// `TOKEN=$(/opt/my continuo/bin/continuo github-app token)` と割れて `command not found` になる。
const attributionExe = "/opt/my continuo/bin/continuo"

// attributionTokenLine は、attribution を付けるときに投稿の前へ来る1行である
// （docs/plans/impl/issue245_github_app_attribution.md の 3-82e）。
const attributionTokenLine = "TOKEN=$('" + attributionExe + "' github-app token) || exit 1"

// attributionFixture は、attribution の設定と実行ファイルのパスを渡した fixture を組み立てる。
//
// t: 呼び出し元のテスト。
// attribution: `tracker.comments.github_app_attribution` の値。
// 戻り値: 組み立てた fixture。
func attributionFixture(t *testing.T, attribution bool) *fixture {
	t.Helper()
	return newFixture(t, fixtureOptions{
		PromptTemplate: builtinOnlyBody(t),
		ContinuoPath:   attributionExe,
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.VerifyStatesEvery = 0
			cfg.Tracker.Comments.GitHubAppAttribution = attribution
		},
	})
}

// 目的: 1回目の本文が、設定の値と本体の実行ファイルのパスで分岐することを固定する（3-82e）。
//
// **`renderFirstPrompt` は `o.continuoPath` を渡す。**hook のコマンド行に書いているものと同じ値なので、
// エージェントが叩く `continuo github-app token` は、いま動いている本体と同じ実行ファイルを指す。
// **素の `continuo` と書かせない。**worktree の中でビルドした実行ファイルで動かしていて
// その worktree を片付けると、走っている run の投稿だけが `command not found` で落ちる。
//
// 与える情報: 設定が真の fixture と偽の fixture。実行ファイルのパスには空白を入れる。
// 成功条件: 真なら、単一引用符で包んだパスでトークンを取る行と `GH_TOKEN` 付きの投稿が本文にあること。
// 偽なら、コマンドの形ではどちらも無いこと。
func TestPrompt_1回目の本文はattributionの設定と実行ファイルのパスで分岐する(t *testing.T) {
	for _, tc := range []struct {
		name        string
		attribution bool
	}{
		{"真", true},
		{"偽", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := attributionFixture(t, tc.attribution)
			prompts := recordPrompts(fx)
			fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

			fx.Orc.Tick(context.Background())
			waitFor(t, 10*time.Second, "1回目の本文が送られる", func() bool {
				return len(prompts()) > 0
			})
			got := prompts()[0]

			const post = `GH_TOKEN="$TOKEN" gh issue comment https://github.com/octocat/hello-world/issues/188 --body-file plan.md`
			if tc.attribution {
				if !strings.Contains(got, attributionTokenLine) {
					t.Errorf("トークンを取る行 %q が本文にありません（実行ファイルのパスが包まれていないか、渡っていない）", attributionTokenLine)
				}
				if !strings.Contains(got, post) {
					t.Errorf("GH_TOKEN 付きの投稿 %q が本文にありません", post)
				}
				return
			}
			// **見るのはコマンドの形だけである。**5-6 の節は `{{if}}` で囲んでいないので、
			// 散文には `GH_TOKEN` の語が偽でも残る。
			for _, notWant := range []string{"github-app token) || exit 1", `GH_TOKEN="$TOKEN" gh `} {
				if strings.Contains(got, notWant) {
					t.Errorf("設定が偽なのに %q が本文にあります。資格情報を持たない利用者の投稿が落ちます", notWant)
				}
			}
			if strings.Contains(got, "{{") {
				t.Errorf("変数展開されなかったテンプレートの記法が本文に残っている:\n%s", got)
			}
		})
	}
}

// 目的: 書かせ直しの文面（設計 3-25 の9段の段7）も、設定の値で分岐することを固定する（3-82e の「7本目」）。
//
// **この文面はテンプレートを1度も通らない。**`{{if}}` と書けばその6文字がそのままエージェントへ届く。
// だから Go の側で分ける。**分岐させないと、成果を書き忘れた run の書かせ直しで、
// attribution の付かないコメントが1件できる。**
//
// **書かせ直しは成果を書かせる最後の経路である。**トークンが取れなかったときの落ち方が最も重いので、
// 組み込みの 5-6 と同じ2文（`HTTP 401` なら1回だけやり直す。それ以外は `GH_TOKEN` を外して投稿し、断りを1行）も付ける。
//
// 与える情報: 進捗報告しか書かずに終えた run（`TestComment_書き直しの文面は囲み付きの印を名指しで禁じる` と同じ）。
// 成功条件: 真なら、トークンを取る行・`GH_TOKEN` 付きの `gh issue comment`・5-6 と同じ2文が送った文面にあること。
// 偽なら、素の `gh issue comment` だけで、トークンの話が無いこと。
func TestComment_書き直しの文面はattributionの設定で分岐する(t *testing.T) {
	for _, tc := range []struct {
		name        string
		attribution bool
	}{
		{"真", true},
		{"偽", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := attributionFixture(t, tc.attribution)
			fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
			fx.Tracker.AddComment("I_node188",
				"<!-- continuo:agent -->\n<!-- continuo:progress -->\nまだ作業中です。",
				true, time.Now().Add(1*time.Hour))

			transcriptDir := t.TempDir()
			path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
				typedUserLine("p1", "実装してください"),
				assistantLine("req1", "CONTINUO-STATUS: review", false),
			})
			prompts := 0
			fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
				prompts++
				if prompts == 1 {
					fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
				}
				return map[string]any{
					"type":  "agent_prompted",
					"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
				}, nil
			})

			fx.Orc.Tick(context.Background())
			sent := ""
			waitFor(t, 30*time.Second, "コメントを書かせる文面が送られる", func() bool {
				for _, r := range fx.Herdr.Requests() {
					if r.Method != herdr.MethodAgentPrompt {
						continue
					}
					text, _ := r.Params["text"].(string)
					if strings.Contains(text, "issue のコメントに書いてください") {
						sent = text
						return true
					}
				}
				return false
			})

			const bare = `    gh issue comment https://github.com/octocat/hello-world/issues/188 --body "<!-- continuo:agent -->`
			const withToken = `    GH_TOKEN="$TOKEN" gh issue comment https://github.com/octocat/hello-world/issues/188 --body "<!-- continuo:agent -->`
			if !tc.attribution {
				if !strings.Contains(sent, bare) {
					t.Errorf("素の `gh issue comment` の行がありません:\n%s", sent)
				}
				for _, notWant := range []string{"github-app token", "GH_TOKEN", "HTTP 401"} {
					if strings.Contains(sent, notWant) {
						t.Errorf("設定が偽なのに %q が文面にあります:\n%s", notWant, sent)
					}
				}
				return
			}
			// **真の枝は2行。**トークンを取る行の直後に、GH_TOKEN 付きの投稿が同じ字下げで来る。
			if !strings.Contains(sent, "    "+attributionTokenLine+"\n"+withToken) {
				t.Errorf("トークンを取る行と GH_TOKEN 付きの投稿が、4字下げの2行になっていません:\n%s", sent)
			}
			if strings.Contains(sent, bare) {
				t.Errorf("GH_TOKEN の無い `gh issue comment` の行が残っています:\n%s", sent)
			}
			for _, want := range []string{
				// 5-6 と同じ2文。
				"`gh` が `HTTP 401` で落ちたときだけ、`TOKEN=$(…)` の行からもう1回だけやり直してください",
				"`GH_TOKEN=\"$TOKEN\"` を外して投稿し、本文の先頭に並ぶ印（`<!--` で始まる行）を全部通したあとの行に",
				// 断りの1行。本体が足すものと1文字も違えない。
				"GitHub App のトークンで投稿できなかったので、attribution 無しで投稿しています。continuo のログと continuo doctor を確かめてください",
				"`--body \"…\"` で渡す本文は、二重引用符の中に1行足してください",
				// **`{{if}}` をそのまま送っていないこと。**
			} {
				if !strings.Contains(sent, want) {
					t.Errorf("書かせ直しの文面に %q がありません:\n%s", want, sent)
				}
			}
			if strings.Contains(sent, "{{") {
				t.Errorf("テンプレートの記法がそのまま送られています（この文面はテンプレートを通らない）:\n%s", sent)
			}
		})
	}
}
