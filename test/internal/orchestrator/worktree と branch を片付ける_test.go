// {"RUCM-CFG-SHA256": "15ac9a60972eee59e304ed177ae6a65a365e6f0785721bb517a3abd859ce9cd1", "SOURCE": "docs/spec/usecases/particular_case/worktree と branch を片付ける.cfg.json"}
//
// **ユースケース記述「worktree と branch を片付ける」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
// **ここに置くのは、巡回（`reconcileWorktrees`）から入る経路のテストである。**`Manager.Cleanup` を
// 直に呼ぶテストは `test/internal/workspace/` の同じ名前のファイルに在る。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package orchestrator_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/tracker"
)

// deferredNoticePrefix は、片付けを見送った通知の本文の書き出しである。
const deferredNoticePrefix = "worktree を片付けずに残しました"

// leftoverOrphan は、巡回が片付けようとして見送る worktree を1つ作る。
//
// **印を持たない worktree で、Status は `Done`、中に commit していないファイルが在る。**
// 巡回は `cleanup.on_states` に入っているので片付けへ進み、失うものが残っているので見送る。
//
// t: 呼び出し元のテスト。
// fx: fixture。
// number: issue の番号。
// 戻り値の1つ目: issue。
// 戻り値の2つ目: 作った worktree。
func leftoverOrphan(t *testing.T, fx *fixture, number int) (tracker.Issue, preparedWorktree) {
	t.Helper()
	issue := sampleIssue(number, "Done")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	if err := os.WriteFile(filepath.Join(wt.Path, "作りかけ.md"), []byte("途中\n"), 0o600); err != nil {
		t.Fatalf("未追跡のファイルを書けない: %v", err)
	}
	// **見送りは、巡回のたびに `Cleanup` が WARN を1行出す。**
	fx.AllowLog("worktree を消さずに残しました")
	return issue, wt
}

// deferredNoticesOf は、issue に付いた「片付けを見送った通知」だけを返す。
//
// fx: fixture。
// nodeID: issue のノード ID。
// 戻り値: 片付けを見送った通知。
func deferredNoticesOf(fx *fixture, nodeID string) []tracker.Comment {
	var out []tracker.Comment
	for _, c := range fx.Tracker.CommentsOf(nodeID) {
		if strings.Contains(c.Body, deferredNoticePrefix) {
			out = append(out, c)
		}
	}
	return out
}

// {"RUCM-PATH": "P006"}
//
// 目的: 巡回が片付けようとして worktree が消えなかったとき、**issue へ1回だけコメントする**ことを示す。
// **コメントしないと、`Done` にした issue の worktree が残り続けていることが、ログを見ない人に伝わらない。**
//
// 与える情報: 印を持たない worktree（Status は `Done`。commit していないファイルが在る）。巡回を2回回す。
// 成功条件:
//   - 片付けを見送った通知がちょうど1件で、理由を含む
//   - 身元ファイルに、見送った時刻が入っている
//   - worktree は残っている
func Test_worktreeとbranchを片付ける_P006_巡回が見送ったらissueへ1回だけコメントする(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	_, wt := leftoverOrphan(t, fx, 188)

	fx.Orc.Tick(context.Background())
	fx.Orc.Tick(context.Background())

	notices := deferredNoticesOf(fx, "I_node188")
	if len(notices) != 1 {
		t.Fatalf("片付けを見送った通知が1件ではない: %d 件", len(notices))
	}
	if !strings.Contains(notices[0].Body, "理由:") {
		t.Errorf("通知に見送った理由が無い:\n%s", notices[0].Body)
	}
	identity, err := fx.Workspace.ReadIdentity(wt.Path)
	if err != nil {
		t.Fatalf("身元ファイルを読めない: %v", err)
	}
	if identity.CleanupDeferredAt.IsZero() {
		t.Errorf("身元ファイルに、見送った時刻が入っていない")
	}
	if _, statErr := os.Stat(wt.Path); statErr != nil {
		t.Errorf("見送ったのに worktree が消えている: %v", statErr)
	}
}

// {"RUCM-PATH": "P006"}
//
// 目的: 巡回からの投稿は**やり直さない**ことを示す（設計 3-85 の「`addComment` は同じものを
// 2回書くことがあるので、やり直さない」）。**エラーが返っても、書かれなかったとは限らない。**
//
// 与える情報: 片付けを見送った通知の投稿だけが失敗するトラッカー。巡回を2回回し、
// 投稿を通るように戻してからもう1回回す。
// 成功条件:
//   - 投稿を試みるのは1回だけである
//   - 投稿が通るようになったあとの巡回でも、通知は付かない
//   - 身元ファイルに、見送った時刻は入っていない（投稿に成功していないため）
func Test_worktreeとbranchを片付ける_P006_巡回からの投稿は失敗してもやり直さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	_, wt := leftoverOrphan(t, fx, 188)
	fx.AllowLog("片付けを見送った通知を投稿できませんでした")
	fx.Tracker.SetPostErrorForMarker(deferredNoticePrefix, errors.New("テストが投稿を失敗させる"))

	fx.Orc.Tick(context.Background())
	fx.Orc.Tick(context.Background())

	if got := fx.Tracker.CountCall("PostComment"); got != 1 {
		t.Fatalf("投稿を試みた回数が1回ではない: %d 回", got)
	}

	fx.Tracker.SetPostErrorForMarker("", nil)
	fx.Orc.Tick(context.Background())

	if notices := deferredNoticesOf(fx, "I_node188"); len(notices) != 0 {
		t.Errorf("投稿に失敗したあとの巡回で、やり直している: %d 件", len(notices))
	}
	if got := strings.Count(fx.Logs.String(), "片付けを見送った通知を投稿できませんでした"); got != 1 {
		t.Errorf("知らせられなかったことの WARN が1行ではない: %d 行", got)
	}
	identity, err := fx.Workspace.ReadIdentity(wt.Path)
	if err != nil {
		t.Fatalf("身元ファイルを読めない: %v", err)
	}
	if !identity.CleanupDeferredAt.IsZero() {
		t.Errorf("投稿に成功していないのに、身元ファイルに見送った時刻が入っている")
	}
}

// {"RUCM-PATH": "P006"}
//
// 目的: 身元ファイルの `project_item_id` が、worktree の置き場所と違うリポジトリの issue を指しているとき、
// **その issue へは投稿しない**ことを示す。**身元ファイルはエージェントが書き換えられる。**
// 照らさないと、無関係の issue にコメントが付く。
//
// 与える情報: 身元ファイルの `project_item_id` を、別のリポジトリの issue（Status は `Done`）へ書き換えた worktree。
// 巡回を2回回す。
// 成功条件:
//   - その issue に、片付けを見送った通知が1件も付かない
//   - 投稿しなかったことの WARN は1行だけである（巡回のたびに繰り返さない）
func Test_worktreeとbranchを片付ける_P006_リポジトリが食い違うissueへは投稿しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	_, wt := leftoverOrphan(t, fx, 188)
	other := sampleIssue(300, "Done")
	other.Owner = "someone"
	other.Repo = "elsewhere"
	other.Identifier = "someone/elsewhere#300"
	fx.Tracker.AddIssue(other)
	rewriteIdentityItemID(t, fx, wt.Path, other.ID, other.Identifier)
	const warn = "片付けを見送った通知は書きません"
	fx.AllowLog(warn)

	fx.Orc.Tick(context.Background())
	fx.Orc.Tick(context.Background())

	if notices := deferredNoticesOf(fx, "I_node300"); len(notices) != 0 {
		t.Errorf("置き場所と違うリポジトリの issue へ投稿している: %d 件", len(notices))
	}
	if notices := deferredNoticesOf(fx, "I_node188"); len(notices) != 0 {
		t.Errorf("身元ファイルが指していない issue へ投稿している: %d 件", len(notices))
	}
	if got := strings.Count(fx.Logs.String(), warn); got != 1 {
		t.Errorf("投稿しなかったことの WARN が1行ではない: %d 行", got)
	}
}

// {"RUCM-PATH": "P006"}
//
// 目的: 投稿を試みた worktree に**再着手したら、次の見送りではもう一度投稿する**ことを示す。
// **再着手は新しい run である。**やり直した issue の2度目の見送りが黙ったままだと、
// 人間は worktree が残っていることを知る手立てを失う。
//
// 与える情報: 1度目の見送りでは、通知の投稿が失敗する。そのあと Status を `Ready` へ戻して着手させ、
// エージェントが成果を書いて `review` を表明して終わる。Status を `Done` にして、もう一度巡回を回す。
// 成功条件: 2度目の見送りで、片付けを見送った通知がちょうど1件付くこと。
func Test_worktreeとbranchを片付ける_P006_再着手のあとの見送りではもう一度投稿する(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue, _ := leftoverOrphan(t, fx, 188)
	fx.AllowLog("片付けを見送った通知を投稿できませんでした")
	fx.Tracker.SetPostErrorForMarker(deferredNoticePrefix, errors.New("テストが投稿を失敗させる"))

	fx.Orc.Tick(context.Background())
	fx.Tracker.SetPostErrorForMarker("", nil)
	fx.Orc.Tick(context.Background())
	if notices := deferredNoticesOf(fx, "I_node188"); len(notices) != 0 {
		t.Fatalf("再着手の前に、やり直している: %d 件", len(notices))
	}

	// **再着手する。**エージェントは成果を書いて `review` を表明する。
	path := writeTranscript(t, t.TempDir(), "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "CONTINUO-STATUS: review", false),
	})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\nこの run でやったこと", true, time.Now().Add(time.Hour))
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})
	fx.Tracker.SetState(issue.ID, "Ready")
	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "再着手した run が In Review で終わる", func() bool {
		return fx.Tracker.StateOf(issue.ID) == "In Review"
	})
	fx.WaitRunsDrained(t, 20*time.Second)

	fx.Tracker.SetState(issue.ID, "Done")
	fx.Orc.Tick(context.Background())

	if notices := deferredNoticesOf(fx, "I_node188"); len(notices) != 1 {
		t.Fatalf("再着手のあとの見送りで、通知が1件ではない: %d 件", len(notices))
	}
}
