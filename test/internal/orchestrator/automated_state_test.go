// {"RUCM-CFG-SHA256": "6d8adc604f9d9a695450141f57fe45ed8b9cd48e0f096865f8f4e79c69ab985b", "SOURCE": "docs/spec/usecases/particular_case/人間に判断を渡す.cfg.json"}
//
// **ボードの組み込みの自動化が Status を動かしたときの検査である**（設計 3-54。issue #33）。
//
// **エージェントが PR を作ると、ボードの自動化が Status を動かす。**それを「人間が
// 引き渡した」と読んで、continuo が自分のエージェントを turn の途中で殺していた。
// **止めないこと**と、**本来の Status へ戻すこと**、そして
// **人間が動かしたときの扱いを変えていないこと**を見る。
package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// automatedRewriteConfig は「自動化が `In Progress` へ動かしたら `In Progress (AI)` へ戻す」
// 設定を作る。
//
// **`In Progress (AI)` を running_state にする。**継続の指示を送る Status と、戻す先を
// 同じにしないと、書き戻した直後にまた「作業中ではない」と判定されてしまう。
// **`In Progress` は設定のどこにも出てこない**ので、continuo にとっては知らない Status である。
//
// rewrite: 対応表を入れるかどうか。偽なら空のまま（いままでどおり猶予を置いて止まる）。
func automatedRewriteConfig(rewrite bool) func(cfg *config.Config) {
	return func(cfg *config.Config) {
		cfg.Tracker.VerifyStatesEvery = 0
		// **猶予は置かない。**待つ側の挙動は unknown_state_test.go が見ている。
		// ここで見たいのは「自動化なら、猶予に関わらず止めない」ことである。
		cfg.Tracker.UnknownStateGraceMs = 0
		cfg.Tracker.ActiveStates = []string{"Ready", "In Progress (AI)"}
		cfg.Tracker.RunningState = "In Progress (AI)"
		if rewrite {
			cfg.Tracker.AutomatedStateRewrite = map[string]string{
				"In Progress": "In Progress (AI)",
			}
		}
	}
}

// startRunForAutomation は issue を1件着手させ、1回目の turn を待ち受けに入れる。
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
// 戻り値: 着手した issue の project item ID。
func startRunForAutomation(t *testing.T, fx *fixture) string {
	t.Helper()
	blockFirstPrompt(t, fx)
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	return issue.ID
}

// {"RUCM-PATH": "P013"}
//
// TestRUCMHandoff_P013_自動化が動かした知らないStatusではworkerを止めない は、
// 設計 3-54 を確かめる（issue #33 の本体）。
//
// 目的: エージェントが PR を作った3秒後に、ボードの組み込みの自動化が Status を
// `In Progress` へ動かす。**continuo はそれを「人間が引き渡した」と読んで、
// 自分のエージェントを turn の途中で殺していた。**
//
// 与える情報: 1回目の turn が待ち受けに入ったままの run。その間にボードの自動化が
// Status を `In Progress`（設定のどこにも出てこない）へ動かす。猶予は 0。
// 成功条件:
//   - worker を止めない（pane を閉じない・印を外さない）
//   - Status を `In Progress (AI)` へ戻す
//   - 戻したことを issue に1件残す（設計 3-29）
func TestRUCMHandoff_P013_自動化が動かした知らないStatusではworkerを止めない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: automatedRewriteConfig(true)})
	itemID := startRunForAutomation(t, fx)
	closesBefore := fx.Herdr.CountMethod(herdr.MethodPaneClose)

	// ★ エージェントが PR を作り、ボードの組み込みの自動化が Status を動かした。
	fx.Tracker.SetStateByAutomation(itemID, "In Progress")
	fx.Orc.Tick(context.Background())

	waitFor(t, 5*time.Second, "自動化が動かした Status が戻る", func() bool {
		return fx.Tracker.StateOf(itemID) == "In Progress (AI)"
	})
	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Fatalf("自動化が動かしただけなのに印を外している: 印は %d 件", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodPaneClose); got != closesBefore {
		t.Fatalf("自動化が動かしただけなのに pane を閉じている: pane.close が %d 回", got-closesBefore)
	}
	if body := selfCommentBody(fx, "I_node188"); body != "" {
		t.Fatalf("自動化が動かしただけなのに止めた理由を書いている:\n%s", body)
	}

	moves := fx.Tracker.StatusMoveCommentsOf("I_node188")
	if len(moves) == 0 {
		t.Fatal("Status を戻したのに、何から何へ動かしたかを issue に残していない（設計 3-29）")
	}
	last := moves[len(moves)-1].Body
	for _, want := range []string{"In Progress", "In Progress (AI)", "github-project-automation"} {
		if !strings.Contains(last, want) {
			t.Errorf("戻した記録に %q が無い:\n%s", want, last)
		}
	}
	if logs := fx.Logs.String(); !strings.Contains(logs, "continuo が意図した Status へ戻しました") {
		t.Errorf("戻したことをログに残していない")
	}
}

// TestAutomatedState_対応表に無ければいままでどおり止まる は、設計 3-54 を確かめる。
//
// 目的: **書き戻し先が対応表に無ければ書き戻さない。**勝手に元へ戻すと、
// 「人間の操作を巻き戻さない」という土台（設計 3-4）を破る作りに近づく。
//
// 与える情報: 対応表が空の設定。ボードの自動化が Status を `In Progress` へ動かす。
// 成功条件: いままでどおり worker を止め、止めた理由を issue へ書くこと。
// **あわせて「自動化が書いた」ことと、足す1行が書かれていること。**
func TestAutomatedState_対応表に無ければいままでどおり止まる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: automatedRewriteConfig(false)})
	itemID := startRunForAutomation(t, fx)

	fx.Tracker.SetStateByAutomation(itemID, "In Progress")
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)

	body := selfCommentBody(fx, "I_node188")
	if body == "" {
		t.Fatal("対応表に無いのに、止めた理由を issue へ1文字も残していない")
	}
	for _, want := range []string{
		// 誰が書いたか。
		"github-project-automation",
		// 次から止まらなくする1行。
		"automated_state_rewrite",
		"In Progress (AI)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("止めた理由のコメントに %q が無い:\n%s", want, body)
		}
	}
	if got := fx.Tracker.StateOf(itemID); got != "In Progress" {
		t.Errorf("対応表に無いのに Status を書き換えている: got %q, want %q", got, "In Progress")
	}
}

// TestAutomatedState_人間が動かしたときはいままでどおり止まる は、設計 3-54 を確かめる。
//
// 目的: **人間が「止めろ」の意味で Status を動かす操作を、書き戻しで打ち消してはならない。**
// 対応表に載っている Status であっても、動かしたのが人間なら止まる。
//
// 与える情報: 対応表に `In Progress` が載っている設定。**人間が** Status を
// `In Progress` へ動かす（`actor.__typename` が `User`）。
// 成功条件: worker を止め、Status を書き換えないこと。
func TestAutomatedState_人間が動かしたときはいままでどおり止まる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: automatedRewriteConfig(true)})
	itemID := startRunForAutomation(t, fx)

	// **SetState は人間が動かした扱いである**（SetStateByAutomation と対になる）。
	fx.Tracker.SetState(itemID, "In Progress")
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)

	if body := selfCommentBody(fx, "I_node188"); body == "" {
		t.Fatal("人間が動かしたのに止めず、理由も残していない")
	}
	if got := fx.Tracker.StateOf(itemID); got != "In Progress" {
		t.Errorf("人間が動かした Status を continuo が書き戻している: got %q, want %q", got, "In Progress")
	}
}

// TestAutomatedState_誰が動かしたか分からなければいままでどおり止まる は、
// 設計 3-54 を確かめる。
//
// 目的: **timeline のイベントを1件も引けないことがある**（消えた・権限が無い・
// 直近10件から溢れた）。**分からないなら「自動化ではない」に倒す。**
// 倒し方を逆にすると、人間が止めたつもりの操作が黙って巻き戻る。
//
// 与える情報: 対応表に載っている Status へ動かすが、「誰が書いたか」は付けない。
// 成功条件: worker を止め、Status を書き換えないこと。
func TestAutomatedState_誰が動かしたか分からなければいままでどおり止まる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: automatedRewriteConfig(true)})
	itemID := startRunForAutomation(t, fx)

	// **SetStateByAutomation を使わない。**取り直した issue に「誰が書いたか」が入らない。
	fx.Tracker.SetState(itemID, "In Progress")
	fx.Tracker.ClearStatusAuthor(itemID)
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)

	if body := selfCommentBody(fx, "I_node188"); body == "" {
		t.Fatal("誰が書いたか分からないのに止めず、理由も残していない")
	}
	if got := fx.Tracker.StateOf(itemID); got != "In Progress" {
		t.Errorf("誰が書いたか分からないのに Status を書き戻している: got %q, want %q", got, "In Progress")
	}
}

// TestAutomatedState_書き戻しを繰り返しても上限で止まる は、設計 3-54 を確かめる。
//
// 目的: **書き戻した直後にボードの自動化がまた動く組み合わせがある。**上限が無いと、
// continuo とボードが同じ issue を押し合い続け、GitHub への書き込みが巡回のたびに増える。
//
// 与える情報: 自動化が `In Progress` を書くたびに continuo が戻す、を繰り返す。
// 成功条件: 上限（3回）を超えたところで書き戻しをやめ、worker を止めて理由を issue へ書くこと。
func TestAutomatedState_書き戻しを繰り返しても上限で止まる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: automatedRewriteConfig(true)})
	itemID := startRunForAutomation(t, fx)

	// 上限は3回である（internal/orchestrator の maxAutomatedRewrites）。
	for i := 0; i < 3; i++ {
		fx.Tracker.SetStateByAutomation(itemID, "In Progress")
		fx.Orc.Tick(context.Background())
		waitFor(t, 5*time.Second, "自動化が動かした Status が戻る", func() bool {
			return fx.Tracker.StateOf(itemID) == "In Progress (AI)"
		})
	}

	// 4回目。**ここからは戻さず、人間へ渡す。**
	fx.Tracker.SetStateByAutomation(itemID, "In Progress")
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)

	if got := fx.Tracker.StateOf(itemID); got != "In Progress" {
		t.Errorf("上限を超えたのに書き戻している: got %q, want %q", got, "In Progress")
	}
	if body := selfCommentBody(fx, "I_node188"); body == "" {
		t.Fatal("上限を超えたのに、止めた理由を issue へ残していない")
	}
	if logs := fx.Logs.String(); !strings.Contains(logs, "書き戻す回数が上限に達しました") {
		t.Errorf("押し合いが起きていることをログに残していない")
	}
}
