// 引き渡しの通知を投稿する3本の経路が、どれも公開リポジトリへ許可の出し方を書かないことの検査である
// （設計 3-11。issue #259）。
//
// **なぜ経路ごとに要るか。**文面を作るのは `permissionRemedyText` 1つだが、
// **そこへ `RepoIsPrivate` を渡しているかどうかは、経路ごとに別のコードが決めている。**
// 文面だけを見る検査では、**渡す側が差し替わっても緑のままである。**
//
//	経路1  turn の終わりに blocked を受け取った      … turn_test.go が見ている
//	経路2  復元した run が blocked だった            … このファイル
//	経路3  起動直後の確認で blocked が返った          … このファイル
//
// **経路1 は既に `TestTurn_blockedで引き渡すときサブエージェントの記録も案内する` が見ている。**
// ここで重ねない。
package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// commentGrantGuidanceInBody は、issue のコメントで許可を出せることの案内である。
// **公開リポジトリの引き渡しには、これが1文字も入ってはならない。**
//
// **`permission_remedy_test.go` の `commentGrantGuidance` と同じ文字列を持たない。**
// あちらは関数の戻り値を直に見ており、**こちらは投稿されたコメントの本文を見る。**
// **2つが同じ定数を共有すると、片方だけが通ることの意味が消える。**
const commentGrantGuidanceInBody = "この issue のコメントに、その操作を許してよいことをあなた自身の言葉で書いてください"

// grantSentenceInBody は、判定役が許可の表明として読みうる文そのものである。
const grantSentenceInBody = "その操作を許可します"

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

// assertNoGrantRecipe は、引き渡しの本文が公開リポジトリ向けであることを確かめる。
//
// t: テスト。
// where: どの経路か（落ちたときの案内に出す）。
// body: 投稿された本文。
func assertNoGrantRecipe(t *testing.T, where, body string) {
	t.Helper()
	if body == "" {
		t.Fatalf("%s: 引き渡しの通知が投稿されていない", where)
	}
	if strings.Contains(body, grantSentenceInBody) {
		t.Errorf("%s: continuo 自身が許可の文を書いている:\n%s", where, body)
	}
	if strings.Contains(body, commentGrantGuidanceInBody) {
		t.Errorf("%s: 公開リポジトリなのに、コメントで許可を出す案内が入っている:\n%s", where, body)
	}
	if !strings.Contains(body, "このリポジトリは公開なので、issue のコメントで許可を出す方法は案内しません") {
		t.Errorf("%s: 公開リポジトリ向けの断りが入っていない:\n%s", where, body)
	}
}

// 目的: 経路2（復元した run が blocked）の引き渡しに、許可の出し方が入らないことを固定する。
//
// **`sampleIssue` は `RepoIsPrivate` を立てない。**nil は「取れなかった」なので、公開として扱われる。
//
// 与える情報: `In Progress` の issue と、agent_status が blocked の pane。
// 成功条件: 投稿された引き渡しの本文が、公開リポジトリ向けであること。
func TestHandoff_復元したrunがblockedでも公開リポジトリへ許可の出し方を書かない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	// **この WARN はテスト自身が起こしている。**blocked の pane を台本で置いているためである。
	fx.AllowLog("権限の確認で止まっているので引き継ぎません")
	issue := sampleIssue(188, "In Progress")
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
	assertNoGrantRecipe(t, "経路2（復元）", handoffBodyOf(fx, "I_node188"))
}

// 目的: 経路3（起動直後の確認で blocked）の引き渡しに、許可の出し方が入らないことを固定する。
//
// **この経路は `internal/orchestrator/dispatch.go` が `permissionRemedyText` を呼ぶ。**
// **呼び忘れていた時期があり、そのとき公開リポジトリへ許可の出し方を投稿していた。**
//
// 与える情報: `Ready` の issue と、`agent.get` が blocked を返す台本。
// 成功条件: 投稿された引き渡しの本文が、公開リポジトリ向けであること。
func TestHandoff_起動直後のblockedでも公開リポジトリへ許可の出し方を書かない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	// **この WARN はテスト自身が起こしている。**`agent.get` が blocked を返す台本のためである。
	fx.AllowLog("着手に失敗しました")
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
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
	assertNoGrantRecipe(t, "経路3（起動直後）", handoffBodyOf(fx, "I_node188"))
}
