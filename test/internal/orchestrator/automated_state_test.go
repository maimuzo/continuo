// {"RUCM-CFG-SHA256": "2131e9a8ee86af8fdba4d479fe94444550b398f19e23439716928ec0c7ee73eb", "SOURCE": "docs/spec/usecases/particular_case/人間に判断を渡す.cfg.json"}
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
	"errors"
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

// tickUntil は、条件が満たされるまで巡回を打ち直す。
//
// **巡回を1回打てば必ず効く、と思ってはならない。**巡回からの書き戻しは別の goroutine で
// 走り、**ボードへ書いて記録を投稿したあとで書き戻しの印を返す**（`endRewrite`）。
// 印が返る前に次の巡回が来ると、continuo はその巡回では何もしない
// （書き戻しは `beginRewrite` が `rewriteBusy`、終わらせる処理は `beginTerminal` が
// `terminalRewriting` を返す）。**実運用では30秒後の巡回が拾い直すので何も起きないが、
// テストが巡回を1回しか打たないと、そこから先へ永久に進まない。**
//
// **打ち直しても押し合いの数え方は壊れない。**書き戻しが飛んでいる間の巡回は
// `beginRewrite` に弾かれ、確保した書き戻しの枠をその場で返す（`rewriteClaim.release`）。
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
// d: 待つ上限。
// message: 満たされなかったときに出す説明。
// cond: 満たされたかを返す関数。
func tickUntil(t *testing.T, fx *fixture, d time.Duration, message string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		fx.Orc.Tick(context.Background())
		// **1回の巡回の効き目を見届けてから打ち直す。**
		next := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(next) {
			if cond() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		if cond() {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("%s（%s 以内に条件を満たしませんでした）", message, d)
		}
	}
}

// tickRewriteOnce は、自動化が動かした Status への書き戻しを**ちょうど1回**走らせる。
//
// **書き込みを関門で止めてから巡回を打つ。**書き戻しが `UpdateStatus` に入った時点で
// `beginRewrite` が塞がるので、**打ち直した巡回は必ず空振りする。**
// これが無いと、打ち直しと書き戻しの終わりが競り、**1回のつもりが2回書きに行く。**
// 失敗の回数を数えているテスト（`maxAutomatedRewriteFailures`）では、それが
// そのまま誤判定になる。
//
// **関門は抜けてから帰る。**呼び出し側は、このあと書き戻しの着地を待てばよい。
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
func tickRewriteOnce(t *testing.T, fx *fixture) {
	t.Helper()
	release, entered := fx.Tracker.HoldUpdate()
	defer release()
	started := false
	tickUntil(t, fx, 10*time.Second, "巡回が自動化の書き戻しを始める", func() bool {
		if started {
			return true
		}
		select {
		case <-entered:
			started = true
			return true
		default:
			return false
		}
	})
}

// waitRunsDrainedByTick は、巡回を打ち直しながら走行中の run が無くなるのを待つ。
//
// **`fixture.WaitRunsDrained` との違いは、巡回を自分で打ち直すことである。**
// 書き戻しが飛んでいる間の巡回は「終わらせる処理」の印を取れずに何もしないので
// （`beginTerminal` が `terminalRewriting`）、1回打っただけでは run が終わらない。
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
// d: 待つ上限。
func waitRunsDrainedByTick(t *testing.T, fx *fixture, d time.Duration) {
	t.Helper()
	tickUntil(t, fx, d, "走行中の run が無くなる（後始末まで終わる）", func() bool {
		return len(fx.Orc.RunViews()) == 0
	})
}

// waitRewriteSettled は、巡回に書き戻しを1回走らせ、それがボードへ着地して
// 記録の投稿まで終わるのを待つ。
//
// **ボードの Status だけを見て待ってはならない。**書き戻しの goroutine は Status を書いたあと
// 「戻した」をログに出し、**そのあとで「何から何へ動かしたか」を投稿する。**
// **Status だけで待つと、ログも記録もまだ無いうちに次の検査へ進む。**
//
// **記録は「何件あるか」ではなく「1件増えたか」で見る。**着手のときにも
// 「Ready → In Progress (AI)」の記録が1件積まれているので、
// **絶対の件数で待つと、書き戻しの記録を1件も待たないまま通ってしまう。**
//
// **巡回は `tickRewriteOnce` が打つ。**呼び出し側が `Tick` を打ってはならない。
// 前の書き戻しの印がまだ返っていなければ、その巡回は空振りする。
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
// itemID: 見る project item の ID。
// nodeID: 記録が投稿される issue のノード ID。
// want: 戻ってほしい Status 名。
func waitRewriteSettled(t *testing.T, fx *fixture, itemID, nodeID, want string) {
	t.Helper()
	before := len(fx.Tracker.StatusMoveCommentsOf(nodeID))
	tickRewriteOnce(t, fx)
	waitFor(t, 10*time.Second, "自動化が動かした Status が戻り、記録の投稿まで終わる", func() bool {
		return fx.Tracker.StateOf(itemID) == want &&
			len(fx.Tracker.StatusMoveCommentsOf(nodeID)) > before
	})
}

// {"RUCM-PATH": "P015"}
//
// TestRUCMHandoff_P015_自動化が動かした知らないStatusではworkerを止めない は、
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
func TestRUCMHandoff_P015_自動化が動かした知らないStatusではworkerを止めない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: automatedRewriteConfig(true)})
	itemID := startRunForAutomation(t, fx)
	closesBefore := fx.Herdr.CountMethod(herdr.MethodPaneClose)

	// ★ エージェントが PR を作り、ボードの組み込みの自動化が Status を動かした。
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
		// 次から止まらなくする1行。**貼ればそのまま起動する形であること**（設計 3-57）。
		"automated_state_rewrite",
		`"In Progress": "In Progress (AI)"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("止めた理由のコメントに %q が無い:\n%s", want, body)
		}
	}
	// **案内は1つだけである**（設計 3-57）。「対応表に1行足せ」と
	// 「`active_states` か `status_signal_map` に書き足せ」を並べて出すと、
	// **両方やった設定は設定の検査に落ちて起動しない**（対応表のキーは
	// 設定のどこにも名前が出てこない Status でなければならない）。
	if strings.Contains(body, "`tracker.status_signal_map` にその名前を書き足して") {
		t.Errorf("互いを壊す案内を2つ並べている（両方やると起動しない）:\n%s", body)
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
// 直近50件から溢れた）。**分からないなら「自動化ではない」に倒す。**
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
	for i := 1; i <= 3; i++ {
		fx.Tracker.SetStateByAutomation(itemID, "In Progress")
		waitRewriteSettled(t, fx, itemID, "I_node188", "In Progress (AI)")
	}

	// 4回目。**ここからは戻さず、人間へ渡す。**
	fx.Tracker.SetStateByAutomation(itemID, "In Progress")
	waitRunsDrainedByTick(t, fx, 10*time.Second)

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

// TestAutomatedState_書き戻せなかったときは書き込みが見たStatusで判定し直す は、
// 設計 3-56 を確かめる。
//
// 目的: **戻す先は `active_states` に限られる**（設計 3-55）ので、書き戻しが成功したときの
// 行き先は必ず「次の turn へ」である。**判定し直しが要るのは、書き戻せなかったときである。**
// `UpdateStatus` は書く前にボードを取り直し、**`terminal_states` に入っていたら書かずに返す。**
// そのとき返る「取り直した Status」を捨てると、**人間が終わりにした issue へ次の指示を送る。**
//
// 与える情報: 書き戻しの `UpdateStatus` をテストが放すまで返さないようにしたうえで、
// その最中に人間が Status を `Done`（`terminal_states`）へ動かす。
// 成功条件:
//   - Status を書き換えないこと（人間が終わりにしたものを巻き戻さない）
//   - **2回目の turn を送らないこと**（`agent.prompt` は1回のまま）
//   - 印を外すこと（run を終えること）
func TestAutomatedState_書き戻せなかったときは書き込みが見たStatusで判定し直す(t *testing.T) {
	// **`automatedRewriteConfig` は設定の検査を通る組み合わせである**（設計 3-55）。
	// 戻す先 `In Progress (AI)` は `active_states` にあり、キー `In Progress` は
	// 設定のどこにも名前が出てこない。
	fx := newFixture(t, fixtureOptions{Mutate: automatedRewriteConfig(true)})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	// **1回目の `agent.prompt` は、`Stop` を流すまで返させない**（`blockFirstPrompt`）。
	// 返った瞬間から `claude.settle_ms`（この fixture では 50ms）の時計が走り出し、
	// **遅い機械では準備が終わる前に run を諦めてしまう。**
	releasePrompt := blockFirstPrompt(t, fx)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "1回目の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// ★ エージェントが PR を作り、ボードの組み込みの自動化が `In Progress` を書いた。
	fx.Tracker.SetStateByAutomation("PVTI_item188", "In Progress")
	// **エージェントのコメントを置いておく。**無いと run を終えるときに
	// 「コメントの取り戻し」（設計 3-25 の9段）へ入り、そこでも `agent.prompt` を呼ぶ。
	fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())

	// ★ 書き戻しの書き込みを、テストが放すまで返さないようにする。
	release, entered := fx.Tracker.HoldUpdate()
	defer release()

	// turn が終わる（表明は `working`＝Status を動かさない。書き込みは書き戻しの1回だけになる）。
	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "続けます。\n\nCONTINUO-STATUS: working", false),
	})
	fx.Orc.OnHook(stopEvent(fx.Sessions[0], path, "p1"))
	// **`Stop` を積んでから返す。**ここから turn の終わりの判定が始まる。
	releasePrompt()

	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("書き戻しの書き込みが始まりませんでした")
	}

	// ★ 書き込みが飛んでいる最中に、人間が「終わった」にした。
	fx.Tracker.SetState("PVTI_item188", "Done")
	release()

	waitFor(t, 20*time.Second, "run が印から外れる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "Done" {
		t.Errorf("人間が終わりにした Status を continuo が巻き戻している: got %q, want %q", got, "Done")
	}
	if got := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); got != 1 {
		t.Fatalf("終わった issue へ次の指示を送っている: agent.prompt が %d 回（1回のはず）", got)
	}
}

// TestAutomatedState_書き込みが失敗しても書き戻しの回数を食い潰さない は、設計 3-56 を確かめる。
//
// 目的: **押し合いの上限は「continuo とボードが押し合っている」ことを数えるためにある。**
// 押し合いはボードが実際に動いたときにだけ起きる。**通信の失敗で数えてしまうと、
// GitHub へ書けなかったぶんだけ押し合いの枠が減り、押し合いが1度も起きていない run が
// 早々に止まる。**
//
// 与える情報: `UpdateStatus` が2回続けて失敗する状況。そのあと失敗を止め、
// **押し合いの上限（3回）ぶんの書き戻しを続けて行わせる。**
// 成功条件: 失敗のあとでも3回とも書き戻せること。**上限に達したというログを出さないこと。**
// **枠を食い潰していれば、2回の失敗で残りが1回になり、2回目の書き戻しで止まる。**
func TestAutomatedState_書き込みが失敗しても書き戻しの回数を食い潰さない(t *testing.T) {
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

// TestAutomatedState_戻せない状態が続いたら人間へ渡す は、設計 3-56 を確かめる。
//
// 目的: **人間がボードから戻す先の選択肢を消すと、書き込みが毎回失敗する。**
// 押し合いの枠は失敗のたびに返るので上限には届かず、猶予の時計もその手前で戻るので始まらない。
// **止める仕組みが1つも無いと、30秒ごとに失敗する書き込みを永久に打ち続け、
// worker は止まらず、人間にも渡らない。**
//
// 与える情報: `UpdateStatus` がずっと失敗する状況。巡回を繰り返す。
// 成功条件:
//   - 「戻せない」の上限（3回）を超えたら worker を止め、止めた理由を issue へ書くこと
//   - **その理由に「押し合っている」と書かないこと**（押し合いは1度も起きていない）
//   - **人間へ渡すログを毎巡回出さないこと**（同じ行が流れ続けると他の行が埋もれる）
func TestAutomatedState_戻せない状態が続いたら人間へ渡す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: automatedRewriteConfig(true)})
	// **書き込みの失敗は、このテストが自分で起こしているものである。**
	fx.AllowLog("自動化が動かした Status を戻せませんでした")
	itemID := startRunForAutomation(t, fx)

	// **ボードから戻す先の選択肢が消えた状況である。**次の巡回でも直らない。
	fx.Tracker.SetUpdateError(errors.New("Status の選択肢 \"In Progress (AI)\" が見つかりません"))

	// 上限は3回である（internal/orchestrator の maxAutomatedRewriteFailures）。
	for i := 1; i <= 3; i++ {
		fx.Tracker.SetStateByAutomation(itemID, "In Progress")
		tickRewriteOnce(t, fx)
		want := i
		waitFor(t, 5*time.Second, "書き戻しの失敗が記録される", func() bool {
			return strings.Count(fx.Logs.String(), "自動化が動かした Status を戻せませんでした") >= want
		})
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Fatalf("上限に達する前に印を外している: 印は %d 件", got)
	}

	// 4回目。**ここからは書き戻さず、人間へ渡す。**
	fx.Tracker.SetStateByAutomation(itemID, "In Progress")
	waitRunsDrainedByTick(t, fx, 10*time.Second)

	body := selfCommentBody(fx, "I_node188")
	if body == "" {
		t.Fatal("戻せないまま止めたのに、理由を issue へ1文字も残していない")
	}
	for _, want := range []string{
		// 何が起きたか。
		"書き込めませんでした",
		// どうすればよいか（ボードの選択肢を作り直す）。
		"In Progress (AI)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("止めた理由のコメントに %q が無い:\n%s", want, body)
		}
	}
	if strings.Contains(body, "押し合") {
		t.Errorf("押し合いは1度も起きていないのに、押し合いの話を書いている:\n%s", body)
	}
	logs := fx.Logs.String()
	if got := strings.Count(logs, "戻せない状態が続いたので、ここからは人間へ渡します"); got != 1 {
		t.Errorf("人間へ渡すログの本数が1本ではない: %d 本（巡回のたびに出すと他の行が埋もれる）", got)
	}
	if got := fx.Tracker.StateOf(itemID); got != "In Progress" {
		t.Errorf("書き戻せていないのに Status が変わっている: got %q, want %q", got, "In Progress")
	}
}

// TestAutomatedState_turnの終わりから書き戻すあいだも run を手放させない は、
// 設計 3-56 を確かめる。
//
// 目的: **書き戻しは巡回からも turn の終わりからも走る。**turn の終わりの処理は
// 表明を読む1秒ほどの待ちと2往復の書き込みを含む。**その間に巡回が「人間が動かした」と
// 判断して run を手放すと、印が消えたあとに「作業中」の Status がボードへ書かれる。**
// 次の巡回はそれを候補として拾い直し、**同じ worktree に2本目の Claude Code を立てる。**
//
// 与える情報: turn の終わりから走る書き戻しの `UpdateStatus` を、テストが放すまで
// 返さないようにしたうえで、その最中に Status を `In Review` へ動かして
// 巡回に worker を止めにかからせる。
// 成功条件: 最後まで worker が1本のままであること（`agent.start` が1回のまま）。
func TestAutomatedState_turnの終わりから書き戻すあいだもrunを手放させない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: automatedRewriteConfig(true)})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	// **1回目の `agent.prompt` は、`Stop` を流すまで返させない**（`blockFirstPrompt`）。
	// 返った瞬間から `claude.settle_ms`（この fixture では 50ms）の時計が走り出し、
	// **遅い機械では準備が終わる前に run を諦めてしまう。**
	releasePrompt := blockFirstPrompt(t, fx)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "1回目の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	startsBefore := fx.Herdr.CountMethod(herdr.MethodAgentStart)

	// ★ エージェントが PR を作り、ボードの組み込みの自動化が `In Progress` を書いた。
	fx.Tracker.SetStateByAutomation("PVTI_item188", "In Progress")

	// ★ 書き戻しの書き込みを、テストが放すまで返さないようにする。
	release, entered := fx.Tracker.HoldUpdate()
	defer release()

	// turn が終わる（表明は `working`＝Status を動かさない。書き込みは書き戻しの1回だけになる）。
	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "続けます。\n\nCONTINUO-STATUS: working", false),
	})
	fx.Orc.OnHook(stopEvent(fx.Sessions[0], path, "p1"))
	// **`Stop` を積んでから返す。**ここから turn の終わりの判定が始まる。
	releasePrompt()

	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("書き戻しの書き込みが始まりませんでした")
	}

	// ★ 書き込みが飛んでいる最中に、人間がレビューへ引き取った。
	// **ここで run を手放すと、そのあと着地する書き込みが印の無い issue を作業中にする。**
	fx.Tracker.SetState("PVTI_item188", "In Review")
	fx.Orc.Tick(context.Background())
	// **止める処理は別の goroutine で走る。**走り切る時間を与える。
	time.Sleep(500 * time.Millisecond)

	// 書き込みが着地する。
	release()
	waitFor(t, 10*time.Second, "書き戻しが着地する", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "In Progress (AI)"
	})

	// 次の巡回。**印が外れていれば、ここで2本目の worker が立つ。**
	fx.Orc.Tick(context.Background())
	time.Sleep(500 * time.Millisecond)

	if got := fx.Herdr.CountMethod(herdr.MethodAgentStart) - startsBefore; got != 0 {
		t.Fatalf("同じ worktree に2本目の worker を立てている: agent.start が追加で %d 回", got)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Fatalf("書き戻した issue の印が1件ではない: 印は %d 件", got)
	}
}

// TestAutomatedState_手放した run へは書き戻さない は、設計 3-56 を確かめる。
//
// 目的: **巡回からの書き戻しは別の goroutine で走る。**その最中に run を手放すと、
// **印が消えたあとに「作業中」の Status がボードへ書かれる。**次の巡回はそれを候補として
// 拾い直し、**同じ worktree に2本目の Claude Code を立てる。**
//
// 与える情報: 書き戻しの `UpdateStatus` をテストが放すまで返さないようにしたうえで、
// その最中に Status を `In Review` へ動かして worker を止めにかからせる。
// 成功条件: 最後まで worker が1本のままであること（`agent.start` が1回のまま）。
func TestAutomatedState_手放したrunへは書き戻さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: automatedRewriteConfig(true)})
	itemID := startRunForAutomation(t, fx)
	startsBefore := fx.Herdr.CountMethod(herdr.MethodAgentStart)

	// ★ 書き戻しの書き込みを、テストが放すまで返さないようにする。
	release, entered := fx.Tracker.HoldUpdate()
	defer release()

	fx.Tracker.SetStateByAutomation(itemID, "In Progress")
	fx.Orc.Tick(context.Background())
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("書き戻しの書き込みが始まりませんでした")
	}

	// ★ 書き込みが飛んでいる最中に、人間がレビューへ引き取った。
	// **ここで run を手放すと、そのあと着地する書き込みが印の無い issue を作業中にする。**
	fx.Tracker.SetState(itemID, "In Review")
	fx.Orc.Tick(context.Background())
	// **止める処理は別の goroutine で走る。**走り切る時間を与える。
	time.Sleep(500 * time.Millisecond)

	// 書き込みが着地する。
	release()
	waitFor(t, 5*time.Second, "書き戻しが着地する", func() bool {
		return fx.Tracker.StateOf(itemID) == "In Progress (AI)"
	})

	// 次の巡回。**印が外れていれば、ここで2本目の worker が立つ。**
	fx.Orc.Tick(context.Background())
	time.Sleep(500 * time.Millisecond)

	if got := fx.Herdr.CountMethod(herdr.MethodAgentStart) - startsBefore; got != 0 {
		t.Fatalf("同じ worktree に2本目の worker を立てている: agent.start が追加で %d 回", got)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Fatalf("書き戻した issue の印が1件ではない: 印は %d 件", got)
	}
}

// TestAutomatedState_巡回の書き戻しが飛んでいてもturnループは止まらない は、
// 設計 3-56 を確かめる。
//
// 目的: **書き戻しの印と「終わらせる処理」の印を同じものにしてはならない。**
// 巡回からの書き戻しは2往復の書き込みで1秒ほどかかる。**その最中に turn が終わると、
// turn の終わりの書き戻しが印を取れない。**それを「この run は既に終わりに向かっています」と
// 読んで turn ループを抜けると、**誰も終わらせていないのに turn ループだけが消える。**
// run は印に残り、pane も開いたまま、`claude.turn_timeout_ms`（既定1時間）まで放置される。
//
// 与える情報: 巡回からの書き戻しの `UpdateStatus` をテストが放すまで返さないようにし、
// **その最中に turn を終わらせる。**
// 成功条件: **turn ループが続くこと**（2回目の指示が送られる）。印が1件のままであること。
func TestAutomatedState_巡回の書き戻しが飛んでいてもturnループは止まらない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: automatedRewriteConfig(true)})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	// **1回目の `agent.prompt` は、`Stop` を流すまで返させない**（`blockFirstPrompt`）。
	// 返った瞬間から `claude.settle_ms`（この fixture では 50ms）の時計が走り出し、
	// **遅い機械では準備が終わる前に run を諦めてしまう。**
	releasePrompt := blockFirstPrompt(t, fx)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "1回目の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// ★ エージェントが PR を作り、ボードの組み込みの自動化が `In Progress` を書いた。
	fx.Tracker.SetStateByAutomation("PVTI_item188", "In Progress")

	// ★ 巡回からの書き戻しを、テストが放すまで返さないようにする。
	release, entered := fx.Tracker.HoldUpdate()
	defer release()
	fx.Orc.Tick(context.Background())
	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("巡回からの書き戻しが始まりませんでした")
	}

	// ★ 書き戻しが飛んでいる最中に turn が終わる（表明は `working`＝Status を動かさない）。
	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "続けます。\n\nCONTINUO-STATUS: working", false),
	})
	fx.Orc.OnHook(stopEvent(fx.Sessions[0], path, "p1"))
	// **`Stop` を積んでから返す。**ここから turn の終わりの判定が始まる。
	releasePrompt()

	// **turn ループは続く。**飛んでいる書き戻しが Status を直すので、この run は終わっていない。
	waitFor(t, 20*time.Second, "2回目の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) >= 2
	})
	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Fatalf("書き戻しが飛んでいるだけなのに印が1件ではない: 印は %d 件", got)
	}
}

// TestAutomatedState_書き戻しが飛んでいても終わらせる処理は黙って戻らない は、
// 設計 3-56 を確かめる。
//
// 目的: **`finishRun` / `failRun` / `abandonRun` は、印を取れなければ黙って戻る。**
// それは「別の経路が終わらせている」という意味のときだけ正しい。
// **書き戻しが飛んでいるだけで戻ってしまうと、この run を終わらせる者が誰も居なくなる。**
// Status も動かず、引き渡しのコメントも出ず、印も外れない。
//
// 与える情報: 巡回からの書き戻しの `UpdateStatus` をテストが放すまで返さないようにし、
// その最中に人間が Status を `Done`（`terminal_states`）へ動かして turn を終わらせる。
// 成功条件: 書き戻しが着地したあと、**run が終わること**（印が外れ、pane が閉じること）。
func TestAutomatedState_書き戻しが飛んでいても終わらせる処理は黙って戻らない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: automatedRewriteConfig(true)})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	// **1回目の `agent.prompt` は、`Stop` を流すまで返させない**（`blockFirstPrompt`）。
	// 返った瞬間から `claude.settle_ms`（この fixture では 50ms）の時計が走り出し、
	// **遅い機械では準備が終わる前に run を諦めてしまう。**
	releasePrompt := blockFirstPrompt(t, fx)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "1回目の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	closesBefore := fx.Herdr.CountMethod(herdr.MethodPaneClose)

	// **エージェントのコメントを置いておく。**無いと run を終えるときに
	// 「コメントの取り戻し」（設計 3-25 の9段）へ入り、そこでも `agent.prompt` を呼ぶ。
	fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())

	// ★ ボードの組み込みの自動化が `In Progress` を書いた。
	fx.Tracker.SetStateByAutomation("PVTI_item188", "In Progress")

	// ★ 巡回からの書き戻しを、テストが放すまで返さないようにする。
	release, entered := fx.Tracker.HoldUpdate()
	defer release()
	fx.Orc.Tick(context.Background())
	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("巡回からの書き戻しが始まりませんでした")
	}

	// ★ 書き込みが飛んでいる最中に、人間が「終わった」にした。
	fx.Tracker.SetState("PVTI_item188", "Done")

	// ★ その状態で turn が終わる。**turn の終わりは `finishRun` を呼ぶ。**
	fx.Tracker.ResetCalls()
	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "続けます。\n\nCONTINUO-STATUS: working", false),
	})
	fx.Orc.OnHook(stopEvent(fx.Sessions[0], path, "p1"))
	// **`Stop` を積んでから返す。**ここから turn の終わりの判定が始まる。
	releasePrompt()
	waitFor(t, 20*time.Second, "turn の終わりの取り直しが走る", func() bool {
		return fx.Tracker.CountCall("FetchIssuesByIDs") > 0
	})

	// 書き込みが着地する。**ここから先、この run を終わらせられるのは turn ループだけである。**
	release()

	fx.WaitRunsDrained(t, 20*time.Second)
	if got := fx.Herdr.CountMethod(herdr.MethodPaneClose); got == closesBefore {
		t.Fatalf("run を終えたのに pane を閉じていない: pane.close が %d 回のまま", got)
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "Done" {
		t.Errorf("人間が終わりにした Status を continuo が巻き戻している: got %q, want %q", got, "Done")
	}
}

// assertRewriteKeyHintIsPastable は、対応表に書いてある Status で止めたときのコメントが
// **貼っても continuo が起動する案内だけになっている**ことを確かめる（設計 3-57。issue #67）。
//
// **`active_states` か `status_signal_map` に書き足せ、を出してはならない。**
// 対応表のキーは「設定の他のどこにも名前が出てこない Status」でなければならず、
// **書き足した時点で `config.Validate` がその行を弾く＝continuo が起動しなくなる。**
//
// **代わりに「先に対応表のその行を消す」が出ていること。**それが唯一、貼っても起動する直し方であり、
// **ボードの自動化をやめた人が抜け出す道でもある。**
//
// t: 呼び出し元のテスト。
// body: issue へ書いた「止めた理由」のコメント本文。
func assertRewriteKeyHintIsPastable(t *testing.T, body string) {
	t.Helper()
	if strings.Contains(body, "`tracker.status_signal_map` にその名前を書き足して") {
		t.Errorf("対応表に書いてある Status なのに `active_states` へ足す案内を出している"+
			"（言われたとおりに貼ると continuo が起動しない）:\n%s", body)
	}
	if !strings.Contains(body, "対応表のその行を消してください") {
		t.Errorf("対応表のその行を消す案内が出ていない（抜け出し方がどこにも書かれていない）:\n%s", body)
	}
}

// TestAutomatedState_押し合いで止めても貼ると起動しない案内を出さない は、設計 3-57 を確かめる
// （issue #67 の1件目）。
//
// 目的: **抑止は「対応表へ1行足す案内を出したか」で判定していた。**
// 押し合いで止めた道はその案内を出さないので、**`active_states` へ足す案内がそのまま出ていた。**
// **その Status は既に対応表のキーなので、言われたとおりに足すと continuo は起動しなくなる。**
//
// 与える情報: 対応表に `In Progress` が載っている設定で、押し合いの上限（3回）を超えさせる。
// 成功条件: 止めた理由のコメントに `active_states` へ足す案内が無く、
// 代わりに「対応表のその行を消す」が書かれていること。
func TestAutomatedState_押し合いで止めても貼ると起動しない案内を出さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: automatedRewriteConfig(true)})
	itemID := startRunForAutomation(t, fx)

	// 上限は3回である（internal/orchestrator の maxAutomatedRewrites）。
	for i := 1; i <= 3; i++ {
		fx.Tracker.SetStateByAutomation(itemID, "In Progress")
		fx.Orc.Tick(context.Background())
		waitRewriteSettled(t, fx, itemID, "I_node188", "In Progress (AI)", i)
	}

	// 4回目。**ここからは戻さず、人間へ渡す。**
	fx.Tracker.SetStateByAutomation(itemID, "In Progress")
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)

	body := selfCommentBody(fx, "I_node188")
	if body == "" {
		t.Fatal("押し合いで止めたのに、理由を issue へ1文字も残していない")
	}
	assertRewriteKeyHintIsPastable(t, body)
}

// TestAutomatedState_戻せないまま止めても貼ると起動しない案内を出さない は、設計 3-57 を確かめる
// （issue #67 の1件目）。
//
// 目的: 押し合いの道と同じ欠陥が、**戻せない失敗が続いて止めた道**にもある。
// こちらは「ボードから戻す先の選択肢が消えた」状況であり、**設定を書き足しても直らない。**
// それなのに `active_states` へ足す案内が出ていて、しかも貼ると起動しなくなる。
//
// 与える情報: `UpdateStatus` がずっと失敗する状況で、「戻せない」の上限（3回）を超えさせる。
// 成功条件: 止めた理由のコメントに `active_states` へ足す案内が無く、
// 代わりに「対応表のその行を消す」が書かれていること。
func TestAutomatedState_戻せないまま止めても貼ると起動しない案内を出さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: automatedRewriteConfig(true)})
	// **書き込みの失敗は、このテストが自分で起こしているものである。**
	fx.AllowLog("自動化が動かした Status を戻せませんでした")
	itemID := startRunForAutomation(t, fx)

	// **ボードから戻す先の選択肢が消えた状況である。**次の巡回でも直らない。
	fx.Tracker.SetUpdateError(errors.New("Status の選択肢 \"In Progress (AI)\" が見つかりません"))

	// 上限は3回である（internal/orchestrator の maxAutomatedRewriteFailures）。
	for i := 1; i <= 3; i++ {
		fx.Tracker.SetStateByAutomation(itemID, "In Progress")
		fx.Orc.Tick(context.Background())
		want := i
		waitFor(t, 5*time.Second, "書き戻しの失敗が記録される", func() bool {
			return strings.Count(fx.Logs.String(), "自動化が動かした Status を戻せませんでした") >= want
		})
	}

	// 4回目。**ここからは書き戻さず、人間へ渡す。**
	fx.Tracker.SetStateByAutomation(itemID, "In Progress")
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)

	body := selfCommentBody(fx, "I_node188")
	if body == "" {
		t.Fatal("戻せないまま止めたのに、理由を issue へ1文字も残していない")
	}
	assertRewriteKeyHintIsPastable(t, body)
}

// TestAutomatedState_人間が対応表のキーへ動かしても貼ると起動しない案内を出さない は、
// 設計 3-57 を確かめる（issue #67 の1件目）。
//
// 目的: **人間が動かした場合も同じ道へ入る。**この道では「自動化が書いた」の説明そのものが
// 出ないので、抑止を `automatedStateHint` の中だけに置くと**必ず取りこぼす。**
// 判定は「対応表に書いてある名前か」で行わなければならない。
//
// 与える情報: 対応表に `In Progress` が載っている設定で、**人間が** Status を
// `In Progress` へ動かす（`actor.__typename` が `User`）。
// 成功条件: 止めた理由のコメントに `active_states` へ足す案内が無く、
// 代わりに「対応表のその行を消す」が書かれていること。
func TestAutomatedState_人間が対応表のキーへ動かしても貼ると起動しない案内を出さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: automatedRewriteConfig(true)})
	itemID := startRunForAutomation(t, fx)

	// **SetState は人間が動かした扱いである**（SetStateByAutomation と対になる）。
	fx.Tracker.SetState(itemID, "In Progress")
	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 10*time.Second)

	body := selfCommentBody(fx, "I_node188")
	if body == "" {
		t.Fatal("人間が動かして止めたのに、理由を issue へ1文字も残していない")
	}
	assertRewriteKeyHintIsPastable(t, body)
}
