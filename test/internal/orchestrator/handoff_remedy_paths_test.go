// 引き渡しの通知を投稿する経路が、それぞれ正しい【対処】を持つことの検査である（設計 3-11。issue #259）。
//
//	経路2  復元した run が blocked だった    … 公開かどうかで【対処】を変える
//	経路3  起動直後の確認で blocked が返った  … コメントに書く許可の文を持たない
//
// **経路1（turn の終わりに blocked）は `TestTurn_blockedで引き渡すときサブエージェントの記録も案内する` が見ている。**
//
// **この package は `lang_test.go` の `TestMain`（`testlang.Run`）で日本語に固定されている。**
// だから日本語の文字列で見る。
//
// **経路2 は非公開の場合を必ず試す。**`sampleIssue` は `RepoIsPrivate` を立てないので、nil しか試さないと
// 「正しく渡している」と「渡し忘れて nil になっている」が同じ文面になり、区別できない。
package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// handoffPublicRefusal は、公開リポジトリ（と、公開かどうかを取れなかったとき）の【対処】にだけ出る断りである。
const handoffPublicRefusal = "このリポジトリは公開なので、issue のコメントで許可を出す方法は案内しません"

// handoffGrantSentence は、コメントに書く許可の文である。起動直後の文言には入ってはならない。
const handoffGrantSentence = "その操作を許可します"

// remedyCase は、公開・非公開のどちらとして issue を置くかと、非公開の案内を期待するかである。
type remedyCase struct {
	name          string
	repoIsPrivate *bool
	wantPrivate   bool
}

// remedyCases は、試す公開・非公開の組み合わせを返す。
//
// **明示的な公開（false）を必ず入れる。**nil だけだと、「値が入っていれば非公開と扱う」誤りを検出できない。
//
// 戻り値: 非公開（true）、公開（false）、取れなかった（nil。公開として扱う）の3通り。
func remedyCases() []remedyCase {
	private, public := true, false
	return []remedyCase{
		{name: "非公開", repoIsPrivate: &private, wantPrivate: true},
		{name: "公開", repoIsPrivate: &public, wantPrivate: false},
		{name: "取れなかった", repoIsPrivate: nil, wantPrivate: false},
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

// 目的: 経路2（復元した run が blocked）が、issue の `RepoIsPrivate` を【対処】まで渡していることを固定する。
//
// **渡し損ねると、非公開リポジトリの利用者に対処が届かないか、公開リポジトリへ許可の出し方が載る。**
//
// 与える情報: `In Progress` の issue（非公開 / 取れなかった）と、agent_status が blocked の pane。
// 成功条件: 非公開なら許可の出し方の案内が入り公開の断りが入らない。取れなかったならその逆。
func TestHandoff_復元したrunがblockedなら公開かどうかで対処を変える(t *testing.T) {
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
			hasGuidance := strings.Contains(body, commentGrantGuidance)
			hasRefusal := strings.Contains(body, handoffPublicRefusal)
			if tc.wantPrivate && (!hasGuidance || hasRefusal) {
				t.Errorf("非公開なのに、非公開の【対処】になっていない"+
					"（RepoIsPrivate を渡し損ねている疑い。案内=%v 断り=%v）:\n%s", hasGuidance, hasRefusal, body)
			}
			if !tc.wantPrivate && (hasGuidance || !hasRefusal) {
				t.Errorf("公開として扱うべきなのに、公開の【対処】になっていない（案内=%v 断り=%v）:\n%s",
					hasGuidance, hasRefusal, body)
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
