// 引き渡しの通知を投稿する3本の経路が、リポジトリの公開・非公開を正しく渡していることの検査である
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
// **非公開の場合を必ず試す。**`sampleIssue` は `RepoIsPrivate` を立てないので、nil しか試さないと
// **「正しく渡している」と「リテラルの nil を渡している」が同じ文面になり、区別できない。**
// 非公開なら文面が変わるので、渡し忘れはそこで落ちる。
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

// remedyCase は、公開・非公開のどちらとして issue を置くかと、そのとき本文に何が出るべきかである。
type remedyCase struct {
	name          string
	repoIsPrivate *bool
	wantPrivate   bool // true なら非公開の案内が出て、公開の断りが出ないこと
}

// remedyCases は、試す公開・非公開の組み合わせを返す。
//
// 戻り値: 非公開（true）と、取れなかった（nil。公開として扱う）の2通り。
func remedyCases() []remedyCase {
	private := true
	return []remedyCase{
		{name: "非公開", repoIsPrivate: &private, wantPrivate: true},
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

// assertRemedyFor は、引き渡しの本文が、公開・非公開に合った【対処】を持つことを確かめる。
//
// t: テスト。
// where: どの経路か（落ちたときの案内に出す）。
// body: 投稿された本文。
// wantPrivate: 非公開の案内を期待するなら true。
func assertRemedyFor(t *testing.T, where, body string, wantPrivate bool) {
	t.Helper()
	if body == "" {
		t.Fatalf("%s: 引き渡しの通知が投稿されていない", where)
	}
	// **どちらでも、continuo 自身は許可の文を書かない。**
	if strings.Contains(body, grantSentence) {
		t.Errorf("%s: continuo 自身が許可の文を書いている:\n%s", where, body)
	}
	hasGuidance := strings.Contains(body, commentGrantGuidance)
	hasRefusal := strings.Contains(body, publicRefusal)
	if wantPrivate {
		if !hasGuidance || hasRefusal {
			t.Errorf("%s: 非公開なのに、非公開の案内になっていない"+
				"（RepoIsPrivate を渡し損ねている疑い。案内=%v 断り=%v）:\n%s", where, hasGuidance, hasRefusal, body)
		}
		return
	}
	if hasGuidance || !hasRefusal {
		t.Errorf("%s: 公開として扱うべきなのに、公開の断りになっていない（案内=%v 断り=%v）:\n%s",
			where, hasGuidance, hasRefusal, body)
	}
}

// 目的: 経路2（復元した run が blocked）が、issue の `RepoIsPrivate` を【対処】まで渡していることを固定する。
//
// 与える情報: `In Progress` の issue（非公開 / 取れなかった）と、agent_status が blocked の pane。
// 成功条件: 投稿された引き渡しの本文が、公開・非公開に合った【対処】を持つこと。
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
			assertRemedyFor(t, "経路2（復元）", handoffBodyOf(fx, "I_node188"), tc.wantPrivate)
		})
	}
}

// 目的: 経路3（起動直後の確認で blocked）が、issue の `RepoIsPrivate` を【対処】まで渡していることを固定する。
//
// **この経路は `internal/orchestrator/dispatch.go` が `permissionRemedyText` を呼ぶ。**
// **呼び忘れていた時期があり、そのとき公開リポジトリへ許可の出し方を投稿していた。**
//
// 与える情報: `Ready` の issue（非公開 / 取れなかった）と、`agent.get` が blocked を返す台本。
// 成功条件: 投稿された引き渡しの本文が、公開・非公開に合った【対処】を持つこと。
func TestHandoff_起動直後のblockedなら公開かどうかで対処を変える(t *testing.T) {
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
			assertRemedyFor(t, "経路3（起動直後）", handoffBodyOf(fx, "I_node188"), tc.wantPrivate)
		})
	}
}
