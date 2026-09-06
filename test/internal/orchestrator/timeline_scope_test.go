// **ID 指定の取り直しが「誰が Status を書いたか」を取るかどうかを、呼び出し元ごとに固定する**
// （設計 3-61）。
//
// **記録（timeline）を読む呼び出し元は6つのうち2つだけである。**実行中の run の照合と、
// turn の終わりの取り直しである。**残る4つが記録まで取ると、使いもしない50件のイベントを
// 巡回のたび・着手のたび・起動のたびに読むことになる**（GraphQL の点数は返す node の数で
// 決まる。設計 3-31）。
//
// **偽のトラッカーは、記録を取らない経路で `StatusChangedBy` と
// `StatusChangedByAutomation` を落とす**（本物と同じ振る舞い）。だから、この検査は
// 「どちらのメソッドを呼んだか」と「記録が届くか」の両方を同時に押さえている。
package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// withTimelineCall は「誰が Status を書いたか」も取る取り直しの、呼び出しの記録上の名前である。
const withTimelineCall = "FetchIssuesByIDs"

// withoutTimelineCall は「誰が Status を書いたか」を取らない取り直しの、記録上の名前である。
const withoutTimelineCall = "FetchIssuesByIDsWithoutTimeline"

// idRefreshCalls は ID 指定の取り直しの呼び出しだけを、呼ばれた順に取り出す。
//
// **他のメソッドを混ぜない。**見たいのは「どちらの取り直しを、どの順で呼んだか」だけである。
//
// fx: 対象の fixture。
// 戻り値: 取り直しのメソッド名を呼ばれた順に並べたもの。
func idRefreshCalls(fx *fixture) []string {
	var out []string
	for _, c := range fx.Tracker.Calls() {
		if c == withTimelineCall || c == withoutTimelineCall {
			out = append(out, c)
		}
	}
	return out
}

// TestTimelineScope_着手してよいかの判定は誰がStatusを書いたかを取らない は、設計 3-61 を確かめる。
//
// 目的: 着手の段2（`dispatchStatusAllowed`）が見るのは、取り直した Status が
// `active_states` に入っているかどうかだけである。**記録は1つも読まない。**
// **この経路は候補1件ごとに走る**ので、記録を取ると巡回のコストが候補の件数だけ増える。
//
// 与える情報: カンバンの実体は `Blocked` なのに、候補の一覧には索引の遅れで `Ready` の写しが
// 載っている issue（着手の段2 が必ず取り直す状況）。
// 成功条件: 取り直しが1回だけ走り、それが記録を取らない側であること。
func TestTimelineScope_着手してよいかの判定は誰がStatusを書いたかを取らない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Blocked"))
	fx.Tracker.SetExtraCandidates(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "着手の直前の取り直しが走る", func() bool {
		return fx.Tracker.CountIDRefreshes() > 0
	})
	// **そのあと別の取り直しが続かないことも見る。**
	time.Sleep(300 * time.Millisecond)

	if got, want := idRefreshCalls(fx), []string{withoutTimelineCall}; !equalStrings(got, want) {
		t.Fatalf("着手の判定が誰が Status を書いたかまで取っている: got %v, want %v", got, want)
	}
}

// TestTimelineScope_取り残されたworktreeの照合は誰がStatusを書いたかを取らない は、設計 3-61 を確かめる。
//
// 目的: 巡回の手順7（`reconcileWorktrees`）が見るのは、取り直した Status が
// `cleanup.on_states` か `active_states` に入っているかどうかだけである。
// **記録は1つも読まない。****この経路は30秒ごとに走る。**
//
// 与える情報: 印に入っていない worktree が1つ。その issue は候補に上がらない Status
// （`In Review`）にしておく（見たいのは worktree の照合だけである）。
// 成功条件: 取り直しが1回だけ走り、それが記録を取らない側であること。
func TestTimelineScope_取り残されたworktreeの照合は誰がStatusを書いたかを取らない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Tracker.VerifyStatesEvery = 0
	}})
	issue := sampleIssue(188, "In Review")
	fx.Tracker.AddIssue(issue)
	prepareWorktree(t, fx, issue, identityOverride{})
	// 生きている pane は無い（手順7b へは入らない）。
	installPanes(fx)

	fx.Orc.Tick(context.Background())
	time.Sleep(300 * time.Millisecond)

	if got, want := idRefreshCalls(fx), []string{withoutTimelineCall}; !equalStrings(got, want) {
		t.Fatalf("worktree の照合が誰が Status を書いたかまで取っている: got %v, want %v", got, want)
	}
}

// TestTimelineScope_復元の取り直しは誰がStatusを書いたかを取らない は、設計 3-61 を確かめる。
//
// 目的: 復元の段3（`refetchByIdentities`）が見るのは、取り直した Status と識別子だけである。
// **記録は1つも読まない。**引き継いだ run の記録は、最初の巡回の実行中の照合が入れ直す。
//
// 与える情報: `In Progress` の issue の worktree と身元ファイルがディスクにあり、
// その worktree を cwd に持つ pane が生きている状態（引き継ぎの中心の経路）。
// 成功条件: 取り直しが1回だけ走り、それが記録を取らない側であること。
func TestTimelineScope_復元の取り直しは誰がStatusを書いたかを取らない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{SessionUUID: "sess-188"})
	installPanes(fx, livePane{
		PaneID: "p-188", Cwd: wt.Path, AgentName: "continuo-hello-world-188",
		AgentStatus: herdr.AgentStatusIdle, SessionUUID: "sess-188",
	})

	restore(t, fx)

	if got, want := idRefreshCalls(fx), []string{withoutTimelineCall}; !equalStrings(got, want) {
		t.Fatalf("復元の取り直しが誰が Status を書いたかまで取っている: got %v, want %v", got, want)
	}
}

// TestTimelineScope_実行中のrunの照合は誰がStatusを書いたかを取る は、設計 3-61 を確かめる。
//
// 目的: 巡回の実行中の照合（`reconcileRunning`）は、**記録を読む2つのうちの1つである。**
// 知らない Status になったとき、書き戻すか止めるかをその記録で決める（設計 3-54）。
// **軽くしてはならない側を、軽くしていないことを固定する。**
//
// 与える情報: 1件 dispatch 済みの状態から、2回目の巡回を回す
// （2回目には着手も worktree の照合も起きない。印に入った worktree は照合の対象外である）。
// 成功条件: 2回目の巡回の取り直しが1回だけ走り、それが記録を取る側であること。
func TestTimelineScope_実行中のrunの照合は誰がStatusを書いたかを取る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "1回目の dispatch が済む", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	fx.Tracker.ResetCalls()
	fx.Orc.Tick(context.Background())
	time.Sleep(300 * time.Millisecond)

	if got, want := idRefreshCalls(fx), []string{withTimelineCall}; !equalStrings(got, want) {
		t.Fatalf("実行中の run の照合が誰が Status を書いたかを取っていない: got %v, want %v", got, want)
	}
}

// TestTimelineScope_turnの終わりは取るが片付けの判定は取らない は、設計 3-61 を確かめる。
//
// 目的: **`refreshIssue` は1本しかないのに、通る呼び出し元で要否が違う。**
// turn の終わり（`handleTurnEnd`）は記録を読むが、片付けてよいかの判定
// （`finishRunClaimed`）は Status しか見ない。**関数単位では分けられないので、
// 呼ぶ側が渡す。**その受け渡しが両方向とも効いていることを1本で見る。
//
// 与える情報: `Ready` の issue1件と、`CONTINUO-STATUS: review` を書く transcript。
// 引き渡しの Status になるので、turn の終わりの判定はそのまま run の終了へ進む。
// 成功条件: 取り直しが「着手の判定（記録なし）→ turn の終わり（記録あり）→
// 片付けの判定（記録なし）」の順で3回走ること。
func TestTimelineScope_turnの終わりは取るが片付けの判定は取らない(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// **エージェントのコメントを置いておく。**無いと run を終えるときに
		// 「コメントの取り戻し」（設計 3-25 の9段）へ入り、そこでも agent.prompt を呼ぶ。
		fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, clock.Now())
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 30*time.Second)

	want := []string{withoutTimelineCall, withTimelineCall, withoutTimelineCall}
	if got := idRefreshCalls(fx); !equalStrings(got, want) {
		t.Fatalf("取り直しの並びが違う: got %v, want %v（全部の呼び出し: %v）",
			got, want, fx.Tracker.Calls())
	}
}
