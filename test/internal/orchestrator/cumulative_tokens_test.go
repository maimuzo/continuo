package orchestrator_test

import (
	"context"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/tracker"
)

// oneResponse は assistantLine 1件ぶんのトークンである。
//
// **`assistantLine` が入れる `usage` と揃えてある**（input 10 / cache_creation 20 /
// cache_read 30 / output 40）。ずれたらこの定数を直すこと。
var oneResponse = orchestrator.TokenUsage{
	APICalls: 1, Input: 10, CacheCreation: 20, CacheRead: 30, Output: 40,
}

// timesResponse は assistantLine n 件ぶんのトークンを返す。
//
// n: 件数。
// 戻り値: n 件ぶんの合計。
func timesResponse(n int) orchestrator.TokenUsage {
	var out orchestrator.TokenUsage
	for i := 0; i < n; i++ {
		out = out.Add(oneResponse)
	}
	return out
}

// TestTokenTotals_turnを重ねても同じtranscriptを二重に数えない は、
// run をまたぐ累計が差分で足されていることを確かめる（issue #238）。
//
// 目的: **`ReadTranscript` が返すのは「その transcript 1ファイルの絶対値」であり、
// turn を重ねるたびに単調に増える。**そのまま毎回足すと、10 turn 回った run は
// 10回ぶん足される。`SPEC.md` 13.5 が「絶対値の合計を扱うときは、二重計上を避けるため、
// 最後に報告した合計との差分を追うこと」と求めている。
//
// 与える情報: 1回目の turn は API 応答1件の transcript で終わり、2回目の turn は
// **同じファイルが2件目まで伸びた状態**で終わる。3回目以降は `Stop` を返さない。
//
// 成功条件: 累計が API 応答2件ぶんであること。**1件目を二重に数えて3件ぶんに
// なっていないこと。**
func TestTokenTotals_turnを重ねても同じtranscriptを二重に数えない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "作業を続けています。\nCONTINUO-STATUS: working", false),
	})

	var mu sync.Mutex
	calls := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		switch n {
		case 1:
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		case 2:
			// **同じファイルが伸びる。**1件目も入ったままである。
			writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
				typedUserLine("p1", "実装してください"),
				assistantLine("req1", "作業を続けています。\nCONTINUO-STATUS: working", false),
				typedUserLine("p2", "続けてください"),
				assistantLine("req2", "まだ作業しています。\nCONTINUO-STATUS: working", false),
			})
			fx.Orc.OnHook(stopEvent("session-1", path, "p2"))
		default:
			// **`Stop` を返さない。**turn を延々と回さないようにする。
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 30*time.Second, "2回目の turn の集計が載る", func() bool {
		v, ok := viewOfFixture(fx, "octocat/hello-world#188")
		return ok && v.Tokens.APICalls >= 2
	})

	if got, want := fx.Orc.TokenTotals(), timesResponse(2); got != want {
		t.Fatalf("同じ transcript を二重に数えている: got %+v, want %+v", got, want)
	}
}

// TestTokenTotals_違うファイル名を名乗られても二重に数えない は、
// 台帳の判定に hook の値を使っていないことを確かめる（issue #238 のレビューの指摘）。
//
// 目的: **台帳は「前に計上したのと同じ transcript か」を判定して差分を取る。**
// **その判定に hook の `transcript_path` を使うと、エージェントが毎 turn 違うファイル名を
// 名乗るだけで、同じ中身を何度でも新しい鍵として全額計上させられる。**
// hook の中身はエージェントが書き換えられる外部入力であり
// （`internal/orchestrator/hookinput.go` がそう書いている）、
// **`acceptTranscriptPath` はセッション UUID との突き合わせを1行も行っていない。**
// **判定はセッション UUID で行う。**あれは continuo が決める値である。
//
// 与える情報: 1回目の turn は `session-1.jsonl` で終わり、2回目の turn は
// **同じ中身を写した別名のファイル `decoy.jsonl` を名乗って**終わる。
// **どちらも同じセッション UUID の hook である。**
//
// 成功条件: 累計が API 応答2件ぶんであること。**別名を名乗った分を二重に数えて
// 4件ぶんになっていないこと。**
func TestTokenTotals_違うファイル名を名乗られても二重に数えない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	lines := []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "作業を続けています。\nCONTINUO-STATUS: working", false),
		typedUserLine("p2", "続けてください"),
		assistantLine("req2", "まだ作業しています。\nCONTINUO-STATUS: working", false),
	}
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", lines[:2])
	// **中身が同じで名前だけ違うファイル。**エージェントが用意できる。
	decoy := writeTranscript(t, transcriptDir, "decoy.jsonl", lines)

	var mu sync.Mutex
	calls := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		switch n {
		case 1:
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		case 2:
			// **同じセッションのまま、別名のファイルを名乗る。**
			fx.Orc.OnHook(stopEvent("session-1", decoy, "p2"))
		default:
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 30*time.Second, "2回目の turn の集計が載る", func() bool {
		v, ok := viewOfFixture(fx, "octocat/hello-world#188")
		return ok && v.Tokens.APICalls >= 2
	})

	if got, want := fx.Orc.TokenTotals(), timesResponse(2); got != want {
		t.Fatalf("別のファイル名を名乗られて二重に数えた: got %+v, want %+v", got, want)
	}
}

// TestTokenTotals_runが終わっても累計に残る は、この issue の症状そのものを確かめる
// （issue #238）。
//
// 目的: **run が終わると印の集合から消え、そのトークンも一緒に消えていた。**
// 画面の合計は「いま走っている run」だけを足すので、**長い turn が並んでいる間、
// 合計はほぼ常に0だった。**run をまたぐ累計は、run が消えても残らなければ意味が無い。
//
// 与える情報: 1回の turn で `review` を表明して終わる run を1件。
//
// 成功条件: 走行中の run が0件になったあとも、累計に API 応答1件ぶんが残っていること。
func TestTokenTotals_runが終わっても累計に残る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	var mu sync.Mutex
	calls := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})
	fx.AllowLog("コメントを書かせる", "セッションを復元できません", "Status を落とせません")

	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 30*time.Second)

	if n := len(fx.Orc.RunViews()); n != 0 {
		t.Fatalf("run が印から外れていない: %d 件", n)
	}
	if got, want := fx.Orc.TokenTotals(), oneResponse; got != want {
		t.Fatalf("run が終わったら累計まで消えた: got %+v, want %+v", got, want)
	}
}

// TestTokenTotals_引き継いだrunはtranscript全体を累計へ入れる は、
// 画面に出す文言が約束していることを確かめる（issue #238）。
//
// 目的: continuo が再起動しても pane の Claude Code は生きたままなので、
// **引き継いだ run の transcript には、起動より前に書かれた分が残っている。**
// 台帳は空なので、最初の turn の終わりに**そのファイルの全部**が累計へ入る。
// **画面の注記（`dashboard.note_cumulative`）が「引き継いだ run では、起動より前に
// 書かれた分も含む」と人間へ約束しているので、そのとおりになることを確かめる。**
//
// 与える情報: API 応答を2件持つ transcript を先に置き、その run を `Adopt` で引き継ぐ。
// 引き継いだあとの turn が、その transcript で終わる。
//
// 成功条件: 累計が API 応答2件ぶんであること（**引き継ぐ前に書かれた分も入っている**）。
func TestTokenTotals_引き継いだrunはtranscript全体を累計へ入れる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)

	// **引き継ぐ前に、既に2件の API 応答が書かれている。**
	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-188.jsonl", []any{
		typedUserLine("p0", "実装してください"),
		assistantLine("req1", "起動より前に書かれた分です。", false),
		assistantLine("req2", "これも起動より前です。", false),
	})

	var mu sync.Mutex
	calls := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			fx.Orc.OnHook(stopEvent("session-188", path, "p0"))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	if !fx.Orc.Adopt(issue, orchestrator.AdoptedRun{
		AgentName:        normalize.SafeName("continuo-hello-world-" + strconv.Itoa(188)),
		PaneID:           "w1:p1",
		SessionUUID:      "session-188",
		HerdrWorkspaceID: "w1",
	}, true) {
		t.Fatal("引き継げなかった")
	}

	fx.Orc.Tick(context.Background())
	waitFor(t, 30*time.Second, "引き継いだ run の集計が載る", func() bool {
		v, ok := viewOfFixture(fx, "octocat/hello-world#188")
		return ok && !v.TokensAt.IsZero()
	})

	if got, want := fx.Orc.TokenTotals(), timesResponse(2); got != want {
		t.Fatalf("引き継ぐ前に書かれた分が累計に入っていない: got %+v, want %+v", got, want)
	}
}

// TestTokenUsage_Sub は差分の取り方を確かめる（issue #238）。
//
// 目的: **同じ transcript から読んだ絶対値どうしを引く限り、負にはならないはずである。**
// だが transcript が書き換えられないことは測っていないので、**負になったら0へ丸め、
// 丸めたことを呼び出し元へ知らせる。**知らせないと、累計が実際より小さくなったことを
// あとから確かめる手立てが無い。
//
// 与える情報: 増えている場合と、1項目だけ減っている場合。
//
// 成功条件: 増えている場合は差がそのまま出て丸めが起きないこと。減っている場合は
// その項目が0になり、丸めたことが知らされること。
func TestTokenUsage_Sub(t *testing.T) {
	grown := orchestrator.TokenUsage{APICalls: 3, Input: 30, CacheCreation: 60, CacheRead: 90, Output: 120}
	prev := orchestrator.TokenUsage{APICalls: 1, Input: 10, CacheCreation: 20, CacheRead: 30, Output: 40}

	got, clamped := grown.Sub(prev)
	if want := timesResponse(2); got != want {
		t.Errorf("増えている場合の差分が違う: got %+v, want %+v", got, want)
	}
	if clamped {
		t.Error("増えているのに丸めたと知らせた")
	}

	// **1項目だけ減っている。**その項目だけ0になり、丸めたことが知らされる。
	shrunk := orchestrator.TokenUsage{APICalls: 3, Input: 30, CacheCreation: 60, CacheRead: 20, Output: 120}
	got, clamped = shrunk.Sub(prev)
	want := orchestrator.TokenUsage{APICalls: 2, Input: 20, CacheCreation: 40, CacheRead: 0, Output: 80}
	if got != want {
		t.Errorf("減っている項目を0へ丸めていない: got %+v, want %+v", got, want)
	}
	if !clamped {
		t.Error("丸めたのに知らせていない")
	}
}

// resumeLedgerRun は「復帰して turn を1回終える」を1回ぶん組み立てる（issue #238）。
//
// **台帳を落とす条件を確かめるテストは、1つの fixture の中で2回 dispatch する。**
// 台帳は `Orchestrator` の中にあるので、**fixture を作り直すと空になり、何も確かめられない。**
//
// **毎回コメントを書く。**エージェントのコメントは **run ごとに数え直す**（`StartedAt` より
// 後のものだけを数える）ので、**1回目に書いたものは2回目には数えられない。**
// 数えられないと「この run のコメントが無いので、セッションを復元して書かせます」の経路へ落ち、
// 余計な herdr の呼び出しと WARN が出る。
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
// issue: 対象の issue。
// sessionUUID: 復帰先のセッション UUID。
// transcriptPath: その turn の終わりに読ませる transcript のパス。
// promptID: その turn の `prompt_id`。
// onPrompt: `agent.prompt` を受けたときに追加で行うこと（Status を動かすなど）。nil なら何もしない。
func resumeLedgerRun(
	t *testing.T, fx *fixture, issue tracker.Issue,
	sessionUUID, transcriptPath, promptID string, onPrompt func(),
) {
	t.Helper()
	nodeID := nodeIDOf(t, issue)
	var once sync.Once
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		once.Do(func() {
			// **何をしたかはエージェントが書く**（continuo は代筆しない。設計 3-25 / 3-29）。
			fx.Tracker.AddComment(nodeID, "<!-- continuo:agent -->\n実装しました", true, time.Now())
			if onPrompt != nil {
				onPrompt()
			}
			fx.Orc.OnHook(stopEvent(sessionUUID, transcriptPath, promptID))
		})
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})
	fx.Orc.Tick(context.Background())
	// **`Tick` は着手の完了を待たずに返る**（段2以降は別の goroutine）。
	// **`release` のあとに印から外れるので、そこまで待てば台帳の書き込みも終わっている。**
	fx.WaitRunsDrained(t, 30*time.Second)
}

// assertResumedSameSession は、直近の `agent.start` が同じセッションへ復帰したことを確かめる
// （issue #238）。
//
// **これを見ないと、落ちた原因を取り違える。**復帰が壊れると鍵（セッション UUID）が変わり、
// `addTokenUsage` は差分ではなく全額を足す。**台帳を落とす欠陥が入ったときと、
// まったく同じ値が出る。**
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
// want: 復帰先として期待するセッション UUID。
func assertResumedSameSession(t *testing.T, fx *fixture, want string) {
	t.Helper()
	resumes := startResumeUUIDs(fx)
	starts := startSessionIDs(fx)
	if len(resumes) == 0 {
		t.Fatalf("agent.start が1度も呼ばれていない")
	}
	if got := resumes[len(resumes)-1]; got != want {
		t.Fatalf("同じセッションへ復帰していない（累計の検査より前に落とす）: --resume=%q, want %q", got, want)
	}
	if got := starts[len(starts)-1]; got != "" {
		t.Fatalf("復帰するのに新しい UUID を採番している: --session-id=%q", got)
	}
}

// ledgerTranscript は、そのセッションの記録を「同じファイル」として書き直す（issue #238）。
//
// **`seedSessionTranscript` を2回呼んではならない。**呼ぶたびに新しいディレクトリを作るので、
// **同じ名前の記録が2つでき、着手がどちらを見つけるかは `os.ReadDir` の並び順で決まる。**
// **並び順は実行のたびに変わるので、通ったり落ちたりする。**
//
// t: 呼び出し元のテスト。
// seeded: `seedSessionTranscript` が返した1回目のパス。
// sessionUUID: セッション UUID（ファイル名になる）。
// lines: 書き直す中身。
// 戻り値: 書き直したファイルのパス（`seeded` と同じ）。
func ledgerTranscript(t *testing.T, seeded, sessionUUID string, lines []any) string {
	t.Helper()
	return writeTranscript(t, filepath.Dir(seeded), sessionUUID+".jsonl", lines)
}

// TestTokenTotals_引き渡しのあと復帰しても二重に数えない は、
// **`release` では台帳を落とさない**という決めごとを確かめる（issue #238）。
//
// 目的: **引き渡し（`In Review` / `Blocked`）は worktree を消さない。**
// 人間が Status を戻すと、continuo は**同じセッションへ `--resume` で復帰する。**
// **そのとき台帳を落としていると、その transcript が最初から全部もう一度足される。**
// `SPEC.md` 13.5 の「二重計上を避けるため、最後に報告した合計との差分を追うこと」が破れる。
//
// **この経路は、片付けを1度も通らない。**`cleanup.on_states` の既定は `["Done"]` なので、
// `In Review` では `ShouldCleanup` が偽になり、`release` だけが走る。
//
// 与える情報: 身元ファイルつきの worktree と、API 応答1件の記録。
// 1回目の turn で `review` を表明して `In Review` へ渡し、Status を戻してもう一度 dispatch する。
// **2回目の turn では、同じファイルが2件目まで伸びている。**
//
// 成功条件: 累計が API 応答2件ぶんであること。**3件ぶんなら、`release` が台帳を落としている。**
func TestTokenTotals_引き渡しのあと復帰しても二重に数えない(t *testing.T) {
	// **記録の根は、このテスト専用にする**（`sessionTranscriptDir` の説明）。
	fx := newFixture(t, fixtureOptions{TranscriptRoot: t.TempDir()})
	issue := sampleIssue(188, "In Progress")
	prepareWorktree(t, fx, issue, identityOverride{SessionUUID: "sess-188"})
	fx.Tracker.AddIssue(issue)

	first := []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "実装しました。\n\nCONTINUO-STATUS: review", false),
	}
	path := seedSessionTranscript(t, fx, "sess-188", first)

	resumeLedgerRun(t, fx, issue, "sess-188", path, "p1", nil)
	assertResumedSameSession(t, fx, "sess-188")
	if got, want := fx.Orc.TokenTotals(), oneResponse; got != want {
		t.Fatalf("1回目の turn の集計が累計に入っていない: got %+v, want %+v", got, want)
	}
	if got := fx.Tracker.StateOf(issue.ID); got != "In Review" {
		t.Fatalf("引き渡しの経路を通っていない: Status=%q, want %q", got, "In Review")
	}

	// **同じファイルを伸ばす。**`requestId` を変えないと、重複排除で2件目が落ちる。
	ledgerTranscript(t, path, "sess-188", append(append([]any{}, first...),
		typedUserLine("p2", "続けてください"),
		assistantLine("req2", "続きをやりました。\n\nCONTINUO-STATUS: review", false),
	))
	// **人間が Status を戻した、という状況を作る。**
	fx.Tracker.SetState(issue.ID, "In Progress")

	resumeLedgerRun(t, fx, issue, "sess-188", path, "p2", nil)
	assertResumedSameSession(t, fx, "sess-188")
	if got, want := fx.Orc.TokenTotals(), timesResponse(2); got != want {
		t.Fatalf("release が台帳を落として二重に数えた: got %+v, want %+v", got, want)
	}
}

// TestTokenTotals_片付けを見送ったあと復帰しても二重に数えない は、
// **`cleanupPath` が偽（見送り・失敗）なら台帳を落とさない**という決めごとを確かめる
// （issue #238）。
//
// 目的: **片付けを見送ると worktree は残る。**残っていれば同じセッションへ復帰できるので、
// **台帳を落としていると、その transcript が最初から全部もう一度足される。**
//
// **上のテストとの違いは、片付けの経路を通るかどうかである。**
// あちら（`In Review`）は `ShouldCleanup` が偽で `cleanupWorktree` へ1度も入らない。
// こちら（`Done`）は入るが、`cleanup.enabled` が偽なので `Cleanup` が「消していない」を返し、
// **`cleanupPath` が偽を返して `forgetTokenLedger` へ届かない。**
//
// **変異の検出という尺度では、このテストが上のテストを包含する**（`release` で落とす欠陥も、
// `cleanupPath` の戻り値を見ない欠陥も、どちらもここで落ちる）。
// **それでも上のテストを残すのは、あちらが引き渡しという実際の経路そのものであり、
// レビューで指摘された症状の再現だからである。**こちらは `cleanup.enabled` を偽にした
// 人工的な設定で、実運用では既定ではない。
//
// **この2本は「落とさない」側だけを見る。**「消したときに落とす」側は見ない。
// **`forgetTokenLedger` の呼び出しを丸ごと消しても、この2本は落ちない。**
// worktree を消すと身元ファイルも消えるので、次の着手は必ず新しいセッションを採番し、
// **新しいセッションは台帳の別の鍵になるため、足される額は同じだからである。**
// 失うのは台帳の項目1件ぶんのメモリだけである。
// **同じ理由で、巡回中に worktree を消す `reconcileWorktrees` も台帳を落としていないが、
// 額は変わらないのでこの2本の対象外である。**
//
// 与える情報: `cleanup.enabled` を偽にした fixture と、身元ファイルつきの worktree。
// 1回目の turn で Status が `Done`（`terminal_states`）になり、片付けが見送られる。
//
// 成功条件: 累計が API 応答2件ぶんであること。**3件ぶんなら、台帳を落としている。**
func TestTokenTotals_片付けを見送ったあと復帰しても二重に数えない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		TranscriptRoot: t.TempDir(),
		Mutate:         func(cfg *config.Config) { cfg.Cleanup.Enabled = false },
	})
	// **見送りは必ず WARN を1行出す。**宣言しないと、fixture がテストを落とす。
	fx.AllowLog("worktree を片付けずに残しました")

	issue := sampleIssue(188, "In Progress")
	prepareWorktree(t, fx, issue, identityOverride{SessionUUID: "sess-188"})
	fx.Tracker.AddIssue(issue)

	// **表明で `Done` にはできない。**既定の対応表に `Done` へ落ちる値が1つも無い
	// （`review` / `blocked` / `working` の3つだけ）。**台本の中でカンバンを動かす。**
	first := []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "実装しました。", false),
	}
	path := seedSessionTranscript(t, fx, "sess-188", first)

	resumeLedgerRun(t, fx, issue, "sess-188", path, "p1", func() {
		fx.Tracker.SetState(issue.ID, "Done")
	})
	assertResumedSameSession(t, fx, "sess-188")
	if got := fx.Tracker.StateOf(issue.ID); got != "Done" {
		t.Fatalf("片付けの経路を通っていない: Status=%q, want %q", got, "Done")
	}
	if got, want := fx.Orc.TokenTotals(), oneResponse; got != want {
		t.Fatalf("1回目の turn の集計が累計に入っていない: got %+v, want %+v", got, want)
	}

	ledgerTranscript(t, path, "sess-188", append(append([]any{}, first...),
		typedUserLine("p2", "続けてください"),
		assistantLine("req2", "続きをやりました。", false),
	))
	fx.Tracker.SetState(issue.ID, "In Progress")

	resumeLedgerRun(t, fx, issue, "sess-188", path, "p2", func() {
		fx.Tracker.SetState(issue.ID, "Done")
	})
	assertResumedSameSession(t, fx, "sess-188")
	if got, want := fx.Orc.TokenTotals(), timesResponse(2); got != want {
		t.Fatalf("片付けを見送ったのに台帳を落として二重に数えた: got %+v, want %+v", got, want)
	}
}
