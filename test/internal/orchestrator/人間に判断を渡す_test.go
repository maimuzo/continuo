// {"RUCM-CFG-SHA256": "ec59a5d332f78d1efcf7eeff73269419e144583452d84191fb352fb9686680ab", "SOURCE": "docs/spec/usecases/particular_case/人間に判断を渡す.cfg.json"}
//
// **ユースケース記述「人間に判断を渡す」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// {"RUCM-PATH": "P001"}
//
// Test_人間に判断を渡す_P001_reviewの表明で遷移先へ書いて片付ける は、基本フローを検査する。
//
// 目的: `review` の表明を受けたら、対応する Status へ書き、pane を閉じて印を外すこと。
// 与える情報: `CONTINUO-STATUS: review` を含む transcript と、エージェントのコメント。
// 成功条件（RUCM の POSTCONDITION）: Status が `In Review` になり、
// pane が閉じ、印が外れること。**worktree は残る。**
func Test_人間に判断を渡す_P001_reviewの表明で遷移先へ書いて片付ける(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	// **1回目の `agent.prompt` は、`Stop` を流すまで返させない**（`blockFirstPrompt`）。
	// 返った瞬間から `claude.settle_ms`（この fixture では 50ms）の時計が走り出し、
	// **遅い機械では準備が終わる前に run を諦めてしまう。**
	releasePrompt := blockFirstPrompt(t, fx)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\n\nCONTINUO-STATUS: review", false),
	})
	fx.Orc.OnHook(stopEvent(fx.Sessions[0], path, "p1"))
	// **`Stop` を積んでから返す。**ここから turn の終わりの判定が始まる。
	releasePrompt()

	waitFor(t, 20*time.Second, "run が印から外れる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})

	if got := fx.Tracker.StateOf("PVTI_item188"); got != "In Review" {
		t.Errorf("review の表明で遷移先へ書いていない: %s", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodPaneClose); got == 0 {
		t.Error("run が終わったのに pane を閉じていない")
	}
	// **worktree は残す**（人間が成果を見るため）。
	if !strings.Contains(strings.Join(fx.Herdr.Methods(), ","), herdr.MethodPaneClose) {
		t.Error("pane を閉じた記録が無い")
	}
}

// {"RUCM-PATH": "P010"}
//
// Test_人間に判断を渡す_P010_知らないStatusで止めるときはissueに理由を書く は、設計 3-50 を確かめる。
//
// 目的: 設定に名前が出てこない Status へ動かされた issue を、continuo は黙って止めていた。
// **その issue は `active_states` に入らないので二度と拾われず、カンバンにも issue にも
// 何も残らない。**人間には「なぜか止まった issue」だけが残る。
//
// 与える情報: 着手済みの issue を、設定のどこにも名前が出てこない `Icebox` へ動かす。
// 猶予は 0（turn の終わりを待たない）。
// 成功条件: issue に continuo のコメントが1件付き、そこに
// 「どの Status になったか（元の値も）」「なぜ止めたか」「続けるにはどうするか」が書かれていること。
// **Status は continuo が書き換えないこと**（人間の操作を巻き戻さない）。
func Test_人間に判断を渡す_P010_知らないStatusで止めるときはissueに理由を書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.VerifyStatesEvery = 0
			// **猶予を置かない。**待つ側の挙動は別のテストで見る。
			cfg.Tracker.UnknownStateGraceMs = 0
		},
	})
	blockFirstPrompt(t, fx)
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// 人間（またはカンバンの自動化）が、continuo の知らない Status へ動かした。
	fx.Tracker.SetState(issue.ID, "Icebox")
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)

	body := selfCommentBody(fx, "I_node188")
	if body == "" {
		t.Fatalf("知らない Status で止めたのに issue に1文字も残っていない")
	}
	for _, want := range []string{
		// どの Status になったか。
		"Icebox",
		// 元は何だったか（continuo が最後に書いた値）。
		"In Progress",
		// なぜ止めたか。
		"知らない Status",
		// 続けるにはどうするか。
		"active_states",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("止めた理由のコメントに %q が無い:\n%s", want, body)
		}
	}
	if got := fx.Tracker.StateOf(issue.ID); got != "Icebox" {
		t.Errorf("人間が動かした Status を continuo が書き換えている: got %q, want %q", got, "Icebox")
	}
}

// {"RUCM-PATH": "P011"}
//
// Test_人間に判断を渡す_P011_turnが動いている間は知らないStatusでもすぐには止めない は、設計 3-50 を確かめる。
//
// 目的: エージェントが turn の最後に `CONTINUO-STATUS:` を書けば、continuo が正しい Status へ
// 戻す（3-25）。**turn が終わる前に殺すと、その表明が読まれずに捨てられる。**
//
// 与える情報: 1回目の turn が `agent.prompt` の待ち受けに入ったままの run。
// その間にカンバンの Status が `Icebox`（設定のどこにも名前が出てこない）へ動く。猶予は1分。
// 成功条件:
//   - 猶予の内側では worker を止めない（pane も閉じない。コメントも書かない）
//   - 待っていることをログに出す（人間が止めたいときに遅れることを黙って隠さない）
//   - 猶予を過ぎたら止めて、理由を issue へ書く
func Test_人間に判断を渡す_P011_turnが動いている間は知らないStatusでもすぐには止めない(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{
		Now: clock.Now,
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.VerifyStatesEvery = 0
			cfg.Tracker.UnknownStateGraceMs = 60000
		},
	})
	blockFirstPrompt(t, fx)
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	closesBefore := fx.Herdr.CountMethod(herdr.MethodPaneClose)

	fx.Tracker.SetState(issue.ID, "Icebox")
	fx.Orc.Tick(context.Background())
	// **止める処理は別の goroutine で走る。**走らないことを見たいので、少し待つ。
	time.Sleep(500 * time.Millisecond)

	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Fatalf("turn の途中なのに印を外している: 印は %d 件", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodPaneClose); got != closesBefore {
		t.Fatalf("turn の途中なのに pane を閉じている: pane.close が %d 回", got-closesBefore)
	}
	if body := selfCommentBody(fx, "I_node188"); body != "" {
		t.Fatalf("turn の途中なのに止めた理由を書いている:\n%s", body)
	}
	if logs := fx.Logs.String(); !strings.Contains(logs, "turn の終わりを待っています") {
		t.Fatalf("待っていることをログに出していない（人間が止めたいときに遅れることを隠している）")
	}

	// 猶予を過ぎた。**ここからは止める。**
	clock.Advance(2 * time.Minute)
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)

	if body := selfCommentBody(fx, "I_node188"); !strings.Contains(body, "Icebox") {
		t.Fatalf("猶予を過ぎても止めた理由を issue へ書いていない:\n%s", body)
	}
}

// {"RUCM-PATH": "P008"}
//
// Test_人間に判断を渡す_P008_自動化が動かした知らないStatusではworkerを止めない は、
// 設計 3-54 を確かめる（issue #33 の本体）。
//
// 目的: エージェントが PR を作った3秒後に、カンバンの組み込みの自動化が Status を
// `In Progress` へ動かす。**continuo はそれを「人間が引き渡した」と読んで、
// 自分のエージェントを turn の途中で殺していた。**
//
// 与える情報: 1回目の turn が待ち受けに入ったままの run。その間にカンバンの自動化が
// Status を `In Progress`（設定のどこにも出てこない）へ動かす。猶予は 0。
// 成功条件:
//   - worker を止めない（pane を閉じない・印を外さない）
//   - Status を `In Progress (AI)` へ戻す
//   - 戻したことを issue に1件残す（設計 3-29）
func Test_人間に判断を渡す_P008_自動化が動かした知らないStatusではworkerを止めない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: automatedRewriteConfig(true)})
	itemID := startRunForAutomation(t, fx)
	closesBefore := fx.Herdr.CountMethod(herdr.MethodPaneClose)

	// ★ エージェントが PR を作り、カンバンの組み込みの自動化が Status を動かした。
	fx.Tracker.SetStateByAutomation(itemID, "In Progress")
	waitRewriteSettled(t, fx, itemID, "I_node188", "In Progress (AI)")
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

// {"RUCM-PATH": "P009"}
//
// Test_人間に判断を渡す_P009_書き込みが失敗しても書き戻しの回数を食い潰さない は、設計 3-56 を確かめる。
//
// 目的: **押し合いの上限は「continuo とカンバンが押し合っている」ことを数えるためにある。**
// 押し合いはカンバンが実際に動いたときにだけ起きる。**通信の失敗で数えてしまうと、
// GitHub へ書けなかったぶんだけ押し合いの枠が減り、押し合いが1度も起きていない run が
// 早々に止まる。**
//
// 与える情報: `UpdateStatus` が2回続けて失敗する状況。そのあと失敗を止め、
// **押し合いの上限（3回）ぶんの書き戻しを続けて行わせる。**
// 成功条件: 失敗のあとでも3回とも書き戻せること。**上限に達したというログを出さないこと。**
// **枠を食い潰していれば、2回の失敗で残りが1回になり、2回目の書き戻しで止まる。**
func Test_人間に判断を渡す_P009_書き込みが失敗しても書き戻しの回数を食い潰さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: automatedRewriteConfig(true)})
	// **書き込みの失敗は、このテストが自分で起こしているものである。**
	fx.AllowLog("自動化が動かした Status を戻せませんでした")
	itemID := startRunForAutomation(t, fx)

	// **「戻せない」の上限（3回）には届かせない**（internal/orchestrator の
	// maxAutomatedRewriteFailures。届くとそこで人間へ渡すのが正しい振る舞いである）。
	fx.Tracker.SetUpdateError(errors.New("GitHub へ書き込めませんでした（通信の失敗）"))
	for i := 1; i <= 2; i++ {
		fx.Tracker.SetStateByAutomation(itemID, "In Progress")
		tickRewriteOnce(t, fx)
		want := i
		waitFor(t, 5*time.Second, "書き戻しの失敗が記録される", func() bool {
			return strings.Count(fx.Logs.String(), "自動化が動かした Status を戻せませんでした") >= want
		})
	}

	// 通信が戻った。**押し合いの上限ぶん（3回）を続けて書き戻せなければ、失敗で枠を食い潰している。**
	fx.Tracker.SetUpdateError(nil)
	for i := 1; i <= 3; i++ {
		fx.Tracker.SetStateByAutomation(itemID, "In Progress")
		waitRewriteSettled(t, fx, itemID, "I_node188", "In Progress (AI)")
	}

	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Fatalf("書き込みに失敗しただけなのに印を外している: 印は %d 件", got)
	}
	if logs := fx.Logs.String(); strings.Contains(logs, "書き戻す回数が上限に達しました") {
		t.Errorf("押し合いが1度も起きていないのに、上限に達したことにしている:\n%s", logs)
	}
}
