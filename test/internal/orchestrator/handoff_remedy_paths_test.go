// 引き渡しの通知を投稿する経路が、それぞれ正しい【対処】を持つことの検査である（設計 3-11。issue #259）。
//
//	経路2  復元した run が blocked だった    … 【対処】をそのまま載せる
//	経路3  起動直後の確認で blocked が返った  … コメントに書く許可の文を持たない
//
// **経路1（turn の終わりに blocked）は `TestTurn_blockedで引き渡すときサブエージェントの記録も案内する` が見ている。**
//
// **この package は `lang_test.go` の `TestMain`（`testlang.Run`）で日本語に固定されている。**
// だから日本語の文字列で見る。
//
// **公開・非公開で文面は変わらない。**それでも3通りを回すのは、**値が入っていると別の文面になる、
// という分岐が戻っていないこと**を見るためである。
package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/orchestrator"
)

// handoffGrantSentence は、コメントに書く許可の文である。どの引き渡しにも入ってはならない。
const handoffGrantSentence = "その操作を許可します"

// remedyCase は、公開・非公開のどちらとして issue を置くかである。
type remedyCase struct {
	name          string
	repoIsPrivate *bool
}

// remedyCases は、試す公開・非公開の組み合わせを返す。
//
// **文面は3通りとも同じになる。**分けていたのは「公開の場所へ『ここへ許可を書けば通る』と
// 書くと第三者が同じ文を書ける」ためだったが、**そもそも誰が書いても判定役へ届かない**
// （公式の permission modes のページ。2026-09-18 に取得）。
//
// **それでも3通りを回す。**`RepoIsPrivate` は `tool_gate` の判定が使い続けており
// （internal/orchestrator/settings.go の `toolGateHookMatchers`）、**値が入っていると
// 別の文面になる、という分岐が戻っていないことを見るためである。**
//
// 戻り値: 非公開（true）、公開（false）、取れなかった（nil）の3通り。
func remedyCases() []remedyCase {
	private, public := true, false
	return []remedyCase{
		{name: "非公開", repoIsPrivate: &private},
		{name: "公開", repoIsPrivate: &public},
		{name: "取れなかった", repoIsPrivate: nil},
	}
}

// handoffBodyOf は、その issue に投稿された引き渡しの通知の本文を返す。
//
// fx: 動かしている fixture。
// nodeID: issue のノード ID。
// 戻り値: 見つかった本文（無ければ空文字）。
func handoffBodyOf(fx *fixture, nodeID string) string {
	for _, c := range fx.Tracker.CommentsOf(nodeID) {
		if strings.Contains(c.Body, "人間へ引き渡しました") {
			return c.Body
		}
	}
	return ""
}

// 目的: 経路2（復元した run が blocked）が、【対処】を本文に載せることを固定する。
//
// **載せ損ねると、止まった issue を受け取った人に、何をすればよいかが1行も届かない。**
// **公開・非公開の3通りとも、同じ【対処】が載る。**
//
// **`permissionRemedyText` の中身とまるごと突き合わせる。**
// `internal/orchestrator/restore.go` の呼び出しが消えたら、この検査が落ちる。
// **一部だけを `Contains` で見ると、呼び出しが消えても別の行に同じ語があれば通ってしまう。**
//
// 与える情報: `In Progress` の issue（非公開 / 公開 / 取れなかった）と、agent_status が blocked の pane。
// 成功条件: 3通りとも、`permissionRemedyText` の文面をそのまま含むこと。
func TestHandoff_復元したrunがblockedなら対処を載せる(t *testing.T) {
	for _, tc := range remedyCases() {
		t.Run(tc.name, func(t *testing.T) {
			fx := newFixture(t, fixtureOptions{})
			// **この WARN はテスト自身が起こしている。**blocked の pane を台本で置いているためである。
			fx.AllowLog("権限の確認で止まっているので引き継ぎません")
			issue := sampleIssue(188, "In Progress")
			issue.RepoIsPrivate = tc.repoIsPrivate
			fx.Tracker.AddIssue(issue)
			wt := prepareWorktree(t, fx, issue, identityOverride{})
			installPanes(fx, livePane{
				PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
				AgentStatus: herdr.AgentStatusBlocked, SessionUUID: "sess-188",
			})

			restore(t, fx)

			waitFor(t, 10*time.Second, "引き渡しの通知が投稿される", func() bool {
				return handoffBodyOf(fx, "I_node188") != ""
			})
			body := handoffBodyOf(fx, "I_node188")
			want := orchestrator.PermissionRemedyTextForTest(config.ClaudePermissionModeAuto)
			if !strings.Contains(body, want) {
				t.Errorf("引き渡しの本文が【対処】をそのまま含んでいない"+
					"（restore.go が permissionRemedyText を呼んでいない疑い）。\n期待:\n%s\n本文:\n%s", want, body)
			}
			if strings.Contains(body, commentGrantGuidance) {
				t.Errorf("コメントで許可を出す案内が入っている。判定役はそれを読まない:\n%s", body)
			}
		})
	}
}

// 目的: 経路3（起動直後の確認で blocked）が、公開・非公開のどちらでも、コメントに書く許可の文を持たないことを固定する。
//
// **この文言は公開かどうかを見ずに投稿される。**許可の文が戻ると、公開リポジトリの issue へも載る。
//
// 与える情報: `Ready` の issue（非公開 / 取れなかった）と、`agent.get` が blocked を返す台本。
// 成功条件: 投稿された本文に許可の文が無く、`continuo trust` があること。
func TestHandoff_起動直後のblockedは許可の文を持たず信頼を案内する(t *testing.T) {
	for _, tc := range remedyCases() {
		t.Run(tc.name, func(t *testing.T) {
			fx := newFixture(t, fixtureOptions{})
			// **この WARN はテスト自身が起こしている。**`agent.get` が blocked を返す台本のためである。
			fx.AllowLog("着手に失敗しました")
			issue := sampleIssue(188, "Ready")
			issue.RepoIsPrivate = tc.repoIsPrivate
			fx.Tracker.AddIssue(issue)
			fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
				return map[string]any{
					"type":  "agent_info",
					"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
				}, nil
			})

			fx.Orc.Tick(context.Background())

			waitFor(t, 15*time.Second, "引き渡しの通知が投稿される", func() bool {
				return handoffBodyOf(fx, "I_node188") != ""
			})
			fx.WaitRunsDrained(t, 15*time.Second)
			body := handoffBodyOf(fx, "I_node188")
			for _, ng := range []string{commentGrantGuidance, handoffGrantSentence} {
				if strings.Contains(body, ng) {
					t.Errorf("起動直後の引き渡しに %q が入っている:\n%s", ng, body)
				}
			}
			if !strings.Contains(body, "continuo trust") {
				t.Errorf("起動直後の引き渡しに `continuo trust` の案内が無い:\n%s", body)
			}
		})
	}
}
