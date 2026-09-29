// Claude Code を閉じた記録（設計 3-84。issue #246）を、書く道・書かない道ごとに確かめる検査である。
//
// **relay_flow_test.go は、ふつうの閉じ方で1件書くことを見ている。**ここでは、閉じ損ねたあとのやり直し・
// 担当が移ったとき・人間が引き取ったとき・巡回が孤立した pane を閉じたとき・`pane_not_found`・
// 継続の指示・direct chat の用意の打ち切りの道を1本ずつ通す。
//
// **外部へ1回も接続しない。**偽の tracker と偽の herdr だけを使う。
package orchestrator_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// promptedReply は、偽の herdr が `agent.prompt` に返す応答である。
//
// params: 受け取った引数。
// 戻り値: `agent_prompted` の応答。
func promptedReply(params map[string]any) map[string]any {
	return map[string]any{
		"type":  "agent_prompted",
		"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
	}
}

// 目的: 報告の書かせ直しの段2 で pane を閉じ損ねたあと、同じ run が次に pane を閉じるときに、
// 閉じ損ねた pane を `pane.list` で確かめてからもう一度閉じ、全部無くなったら閉じた記録を書くことを固定する。
// あわせて、閉じ損ねた pane の ID が worktree の外の pane に使われていたら、その ID へ `pane.close` を送らないことを固定する。
//
// **やり直さないと、閉じ損ねた run は最後まで記録を書かない。**巡回は run が受け持っている worktree を見ないので、
// 境目が前の run に残り、その run の途中で Claude Code が書いたものまで人間のコメントとして渡りうる。
// **ID だけで閉じると、herdr が ID を使い回したときに別の issue の Claude Code を殺す。**
//
// 与える情報: 1回目の turn で成果のコメントを書かずに review を表明する run。`pane.close` は1回目だけ失敗する。
// 段4 で引く pane は、閉じ損ねた pane（`<ws>:p1`）とは別の `<ws>:p2` にする。
// 絞り込みなしの `pane.list` が返す閉じ損ねた pane は、次の3通り。
//
//	残っている   … cwd が worktree の `<ws>:p1` を返す
//	消えている   … 1枚も返さない
//	別の場所     … cwd が worktree の外の `<ws>:p1` を返す
//
// 成功条件:
//   - 残っている・消えている: run が終わったあと、閉じた記録が1件以上付くこと
//   - 残っている: 閉じ損ねた pane の ID へ、もう一度 `pane.close` が届くこと（合わせて2回）
//   - 別の場所: 閉じ損ねた pane の ID への `pane.close` が、閉じ損ねた1回だけであること
func TestClosedRecord_閉じ損ねたpaneをやり直しで閉じたら記録を書く(t *testing.T) {
	for _, mode := range []string{"残っている", "消えている", "別の場所"} {
		t.Run(mode, func(t *testing.T) {
			fx := newFixture(t, fixtureOptions{})
			fx.AllowLog("pane を閉じられませんでした", "閉じ損ねた pane の ID が別の場所の pane に使われているので、閉じません")
			fx.Tracker.AddIssue(sampleIssue(351, "Ready"))
			elsewhere := t.TempDir()

			transcriptDir := t.TempDir()
			path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
				typedUserLine("p1", "実装してください"),
				assistantLine("req1", "CONTINUO-STATUS: review", false),
			})

			var mu sync.Mutex
			prompts := 0
			closeFailed := false
			closes := 0
			wsID := ""
			fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
				mu.Lock()
				prompts++
				n := prompts
				mu.Unlock()
				if n == 1 {
					fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
				} else {
					// **書かせ直しの指示を受けて、エージェントが成果を書く。**
					fx.Tracker.AddComment("I_node351", "<!-- continuo:agent -->\nこの run でやったこと", true, time.Now().Add(time.Hour))
				}
				return promptedReply(params), nil
			})
			fx.Herdr.Handle(herdr.MethodPaneClose, func(map[string]any) (any, *rpcErr) {
				mu.Lock()
				defer mu.Unlock()
				closes++
				if closes == 1 {
					closeFailed = true
					return nil, &rpcErr{Code: "internal", Message: "閉じられませんでした"}
				}
				return map[string]any{"type": "pane_closed"}, nil
			})
			fx.Herdr.Handle(herdr.MethodPaneList, func(params map[string]any) (any, *rpcErr) {
				id, _ := params["workspace_id"].(string)
				mu.Lock()
				failed := closeFailed
				if id != "" && wsID == "" {
					wsID = id
				}
				ws := wsID
				mu.Unlock()
				if id != "" {
					// **閉じ損ねたあとに引く pane は、閉じ損ねた pane とは別の ID にする。**
					pane := id + ":p1"
					if failed {
						pane = id + ":p2"
					}
					return map[string]any{"type": "pane_list", "panes": []any{
						map[string]any{"pane_id": pane, "workspace_id": id, "cwd": fx.Herdr.pathOf(id),
							"agent_status": "idle", "interactive_ready": true},
					}}, nil
				}
				panes := []any{}
				switch {
				case ws == "" || mode == "消えている":
				case mode == "残っている":
					panes = append(panes, map[string]any{"pane_id": ws + ":p1", "workspace_id": ws,
						"cwd": fx.Herdr.pathOf(ws), "agent": "claude", "agent_status": "idle"})
				case mode == "別の場所":
					panes = append(panes, map[string]any{"pane_id": ws + ":p1", "workspace_id": ws,
						"cwd": elsewhere, "agent": "claude", "agent_status": "idle"})
				}
				return map[string]any{"type": "pane_list", "panes": panes}, nil
			})

			fx.Orc.Tick(context.Background())
			waitFor(t, 30*time.Second, "書かせ直しの指示が送られる", func() bool {
				mu.Lock()
				defer mu.Unlock()
				return prompts >= 2
			})
			fx.WaitRunsDrained(t, 30*time.Second)
			time.Sleep(300 * time.Millisecond)

			mu.Lock()
			failedPane := wsID + ":p1"
			mu.Unlock()
			retried := countString(closedPaneIDs(fx), failedPane)
			records := len(fx.Tracker.ClosedRecordsOf("I_node351"))
			switch mode {
			case "残っている":
				if retried != 2 {
					t.Errorf("閉じ損ねた pane %s へもう一度 pane.close が届いていない（%d 回。閉じた pane: %v）",
						failedPane, retried, closedPaneIDs(fx))
				}
				if records < 1 {
					t.Errorf("閉じ損ねた pane をやり直しで閉じたのに、閉じた記録が無い")
				}
			case "消えている":
				if records < 1 {
					t.Errorf("閉じ損ねた pane が一覧から消えたのに、閉じた記録が無い")
				}
			case "別の場所":
				if retried != 1 {
					t.Errorf("worktree の外の pane に使われている ID %s へ pane.close を送った（%d 回。閉じた pane: %v）",
						failedPane, retried, closedPaneIDs(fx))
				}
			}
		})
	}
}

// 目的: 担当が別の機械へ移って run を止めたとき（`stopBecauseHandoffLost`）は、pane を閉じても
// 閉じた記録を書かないことを固定する。
//
// **担当を外された機械は issue へ書かない**（設計 3-77c・3-83h）。書くと、新しい担当の機械が
// 着手している issue の境目を、外された機械が後ろへずらす。
// **担当者は2人にする。**1人だと、記録を書く側（`recordWorkerClosed`）が「担当者が他人」で別に止めるので、
// 止める道の `closedRecordSkip` が効いているかを見分けられない。
//
// 与える情報: 1回目の turn の最中に、担当者を別のアカウント2人へ書き換えた run（確かめ直す間隔は 1ms）。
// 成功条件: run が印から外れ、pane を閉じたうえで、閉じた記録が1件も無いこと。
func TestClosedRecord_担当が移って止めたときは書かない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.Provider.Handoff.RecheckIntervalMs = 1
		},
	})
	fx.AllowLog("担当が移ったので")
	fx.Tracker.AddIssue(sampleIssue(352, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "続けます。\nCONTINUO-STATUS: working", false),
	})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// **turn が終わる前に、別の機械が担当を取り上げた状態にする。**
		fx.Tracker.SetAssignees("PVTI_item352", rivalLogin, "octocat-bot-c")
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return promptedReply(params), nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "担当が移った run が止まる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
	time.Sleep(300 * time.Millisecond)

	if n := fx.Herdr.CountMethod(herdr.MethodPaneClose); n == 0 {
		t.Fatalf("担当が移った run の pane を閉じていない")
	}
	if n := len(fx.Tracker.ClosedRecordsOf("I_node352")); n != 0 {
		t.Fatalf("担当が移ったのに閉じた記録を書いた: %d 件", n)
	}
}

// 目的: 報告の書かせ直しの段2 で閉じたあと、段5 の前に人間が direct chat へ引き取ったとき、
// run を手放す道（`abortTerminalForHuman` の後半）が、保留していた閉じた記録を1件書くことを固定する。
//
// **この道は `stopWorker` を通らずに run を手放す唯一の道である。**ここで書かないと保留を誰も書かず、
// 境目が前の run に残る（段2 で閉じた Claude Code はもう動いていない）。
//
// 与える情報: 1回目の turn で成果のコメントを書かずに review を表明する run。段2 の `pane.close` のあと、
// 段4 で pane を引く `pane.list` を止め、その間にカードを direct chat へ動かして巡回を回す。
// 成功条件: 手放したログが出たあと、閉じた記録がちょうど1件あること。
func TestClosedRecord_書かせ直しの途中で人間が引き取ったら保留を1件書く(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	issue := sampleIssue(353, "Ready")
	fx.Tracker.AddIssue(issue)
	fx.Tracker.SetAssignees(issue.ID, fakeViewerLogin)

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "CONTINUO-STATUS: review", false),
	})
	var mu sync.Mutex
	prompts := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		prompts++
		first := prompts == 1
		mu.Unlock()
		if first {
			// **コメントは書かない。**run の終わりで成果のコメントを書かせに行く（設計 3-25）。
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		}
		return promptedReply(params), nil
	})
	// **段2 の `pane.close` のあとに来る、絞り込みつきの `pane.list`（段4）を止める。**
	closed := false
	fx.Herdr.Handle(herdr.MethodPaneClose, func(map[string]any) (any, *rpcErr) {
		mu.Lock()
		closed = true
		mu.Unlock()
		return map[string]any{"type": "pane_closed"}, nil
	})
	gate := make(chan struct{})
	entered := make(chan struct{})
	var once, enterOnce sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	defaultList := fx.Herdr.HandlerOf(herdr.MethodPaneList)
	fx.Herdr.Handle(herdr.MethodPaneList, func(params map[string]any) (any, *rpcErr) {
		id, _ := params["workspace_id"].(string)
		mu.Lock()
		afterClose := closed
		mu.Unlock()
		if id != "" && afterClose {
			enterOnce.Do(func() { close(entered) })
			<-gate
		}
		return defaultList(params)
	})

	fx.Orc.Tick(context.Background())
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("段2 で閉じたあとの pane の引き直し（段4）が来ない")
	}
	// ★ 人間が引き取る。巡回が direct chat の印を立てる。
	fx.Tracker.SetState(issue.ID, humanState)
	fx.Orc.Tick(context.Background())
	release()
	waitFor(t, 20*time.Second, "run を手放す", func() bool {
		return strings.Contains(fx.Logs.String(), "この run の pane はもう Claude Code を持っていないので印を外します")
	})
	time.Sleep(300 * time.Millisecond)

	if n := len(fx.Tracker.ClosedRecordsOf(nodeIDOfIssue(issue))); n != 1 {
		t.Fatalf("段2 で閉じたのに、手放すときの閉じた記録が1件ではない: %d 件", n)
	}
}

// 目的: 巡回が印に入っていない worktree の pane を閉じたとき（`reconcileWorktrees` → `closeOrphanPane` →
// `recordOrphanClosed`）、全部閉じられ、かつ1枚以上閉じたときだけ閉じた記録を1件書くことを固定する。
//
// **0枚のときに書くと、印に入っていない active の worktree ごとに、巡回のたびに記録が増える。**
// **1枚でも閉じ損ねたときに書くと、生きた Claude Code があとで書いたものが人間のコメントとして渡りうる。**
//
// 与える情報: `In Progress` の issue の worktree（印に入っていない。着手はさせない）と、その cwd を持つ
// agent 名つきの pane。
//
//	全部閉じる   … pane は1枚。閉じたあとの巡回2回では、pane を1枚も返さない
//	1枚閉じ損ねる … pane は2枚で、片方の `pane.close` だけが失敗する
//
// 成功条件: 全部閉じるでは、1回目の巡回のあとに記録が1件あり、そのあと閉じる pane が0枚の巡回を2回回しても
// 1件のままであること。1枚閉じ損ねるでは、記録が1件も無いこと。
func TestClosedRecord_巡回で孤立したpaneを閉じたら記録を書く(t *testing.T) {
	for _, mode := range []string{"全部閉じる", "1枚閉じ損ねる"} {
		t.Run(mode, func(t *testing.T) {
			fx := newFixture(t, fixtureOptions{
				Mutate: func(cfg *config.Config) {
					cfg.Tracker.VerifyStatesEvery = 0
					// **この巡回では1件も着手させない。**見たいのは worktree の照合だけである。
					cfg.Tracker.RequiredLabels = []string{"never-attached"}
				},
			})
			fx.AllowLog("印に入っていない worktree に生きた pane があったので閉じます", "pane を閉じられませんでした")
			issue := sampleIssue(354, "In Progress")
			fx.Tracker.AddIssue(issue)
			wt := prepareWorktree(t, fx, issue, identityOverride{})
			paneA := wt.WorkspaceID + ":p-354a"
			paneB := wt.WorkspaceID + ":p-354b"
			panes := []livePane{{PaneID: paneA, Cwd: wt.Path, AgentName: "continuo-hello-world-354",
				AgentStatus: herdr.AgentStatusIdle}}
			if mode == "1枚閉じ損ねる" {
				panes = append(panes, livePane{PaneID: paneB, Cwd: wt.Path, AgentStatus: herdr.AgentStatusIdle})
				fx.Herdr.Handle(herdr.MethodPaneClose, func(params map[string]any) (any, *rpcErr) {
					if params["pane_id"] == paneB {
						return nil, &rpcErr{Code: "internal", Message: "閉じられませんでした"}
					}
					return map[string]any{"type": "pane_closed"}, nil
				})
			}
			installPanes(fx, panes...)

			fx.Orc.Tick(context.Background())
			waitFor(t, 10*time.Second, "孤立した pane を閉じる", func() bool {
				return countString(closedPaneIDs(fx), paneA) > 0
			})
			time.Sleep(300 * time.Millisecond)

			node := nodeIDOfIssue(issue)
			if mode == "1枚閉じ損ねる" {
				if n := len(fx.Tracker.ClosedRecordsOf(node)); n != 0 {
					t.Fatalf("1枚閉じ損ねたのに閉じた記録を書いた: %d 件", n)
				}
				return
			}
			if n := len(fx.Tracker.ClosedRecordsOf(node)); n != 1 {
				t.Fatalf("孤立した pane を閉じたのに、閉じた記録が1件ではない: %d 件", n)
			}

			// 閉じたあとは pane が無い。閉じる pane が0枚の巡回を2回回す。
			installPanes(fx)
			for i := 0; i < 2; i++ {
				fx.Orc.Tick(context.Background())
				time.Sleep(200 * time.Millisecond)
			}
			if n := len(fx.Tracker.ClosedRecordsOf(node)); n != 1 {
				t.Fatalf("閉じる pane が無い巡回で閉じた記録が増えた: %d 件", n)
			}
		})
	}
}

// 目的: `pane.close` が `pane_not_found`（その pane はもう無い）を返したときは、閉じたものとして数え、
// 閉じた記録を1件書くことを固定する。
//
// **無い pane で Claude Code は動いていない。**閉じ損ねたと数えると、人間が pane を先に閉じただけで
// 記録が書かれず、境目が前の run に残る。
//
// 与える情報: `pane.close` が必ず `pane_not_found` を返す herdr と、turn が終わらずに打ち切られる run。
// 成功条件: `pane.close` を試したあと、閉じた記録がちょうど1件あること。
func TestClosedRecord_pane_not_foundは閉じたものとして記録を書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.AllowLog("turn が終わったことを検知できません")
	recordPrompts(fx)
	fx.Herdr.Handle(herdr.MethodPaneClose, func(map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: herdr.ErrCodePaneNotFound, Message: "pane not found"}
	})
	fx.Tracker.AddIssue(sampleIssue(355, "Ready"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "pane を閉じようとする", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodPaneClose) > 0
	})
	time.Sleep(300 * time.Millisecond)

	if n := len(fx.Tracker.ClosedRecordsOf("I_node355")); n != 1 {
		t.Fatalf("pane_not_found を閉じたものとして数えていない（閉じた記録 %d 件）", n)
	}
}

// 目的: 人間のコメントの節は1回目の本文にだけ付け、継続の指示（2回目の turn）には付けないことを固定する。
//
// **継続の指示に付けると、同じ許可を turn のたびに渡し直す**（人間の決定で、継続の指示には付けない）。
//
// 与える情報: 閉じた記録と、そのあとに OWNER が書いた許可を持つ `Ready` の issue。1回目の turn は
// `working` を表明して終わる。
// 成功条件: 1回目に送った本文には節の見出しがあり、2回目に送った本文には無いこと。
func TestRelay_継続の指示には節を付けない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(356, "Ready"))
	seedRelayComments(fx, "I_node356")

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "続けます。\nCONTINUO-STATUS: working", false),
	})
	var mu sync.Mutex
	var texts []string
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		text, _ := params["text"].(string)
		mu.Lock()
		texts = append(texts, text)
		first := len(texts) == 1
		mu.Unlock()
		if first {
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		}
		return promptedReply(params), nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "継続の指示が送られる", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(texts) >= 2
	})

	mu.Lock()
	first, second := texts[0], texts[1]
	mu.Unlock()
	if !strings.Contains(first, relaySectionHeading) {
		t.Errorf("1回目の本文に節が無い:\n%s", first)
	}
	if strings.Contains(second, relaySectionHeading) {
		t.Errorf("継続の指示に節を付けている:\n%s", second)
	}
}

// 目的: direct chat の用意の最中に担当者が他人へ替わり、用意を打ち切って pane を閉じたとき
// （`finishDirectChatSetup` の打ち切りの枝）は、閉じた記録を書かないことを固定する。
//
// **閉じた記録を書くかは控えの担当者で決める。**控えが用意を始めたときの担当者（自分）のままだと、
// 担当を外された機械が issue へ書いてしまう（設計 3-77c・3-83h）。
//
// 与える情報: 担当者がこの continuo のアカウント1人の direct chat の issue。`agent.start` の最中に担当者を
// 他人1人へ替える。
// 成功条件: 自分で開いた pane を閉じ、印を外したあと、閉じた記録が1件も無いこと。
func TestClosedRecord_directChatの用意中に担当者が替わったら書かない(t *testing.T) {
	fx := newDirectChatFixture(t, nil)
	release, entered := holdAgentStart(t, fx)
	id, node := addOwnDirectChatIssue(fx, 357)

	fx.Orc.Tick(context.Background())
	<-entered
	fx.Tracker.SetAssignees(id, "someone-else")
	release()

	waitFor(t, 10*time.Second, "自分で開いた pane を閉じる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodPaneClose) == 1
	})
	waitFor(t, 5*time.Second, "印を外す", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
	time.Sleep(300 * time.Millisecond)

	if n := len(fx.Tracker.ClosedRecordsOf(node)); n != 0 {
		t.Fatalf("担当者が他人へ替わったのに閉じた記録を書いた: %d 件", n)
	}
}
