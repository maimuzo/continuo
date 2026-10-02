// 外部（GitHub・herdr）が失敗したときの検査である。
//
// **continuo は外部が落ちても止まらない。**GitHub が読めなくても、herdr が返さなくても、
// **走行中の run を捨てず、pane も閉じない。**次の巡回でやり直す。
//
// **ここで確かめるのは「落ちないこと」ではなく「何を残すか」である。**
// worktree を消したり Status を巻き戻したりすれば、外部が復旧しても取り返しがつかない。
package orchestrator_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// TestExternalFailure_worktreeを開けなければ着手を諦めて次の巡回に委ねる は、herdr の失敗を確かめる。
//
// 目的: `worktree.open` が失敗したとき、issue を人間へ渡さずに次の巡回へ回すこと。
// 与える情報: 常に失敗する `worktree.open`。
// 成功条件: **turn を1回も送らない**こと。
func TestExternalFailure_worktreeを開けなければ着手を諦めて次の巡回に委ねる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Herdr.Handle(herdr.MethodWorktreeOpen, func(_ map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "internal", Message: "herdr が worktree を開けません"}
	})

	fx.Orc.Tick(context.Background())
	time.Sleep(2 * time.Second)

	if got := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); got != 0 {
		t.Errorf("worktree を開けないのに turn を送っている: %d 回", got)
	}
}

// TestExternalFailure_transcriptを読めなくても turn を終えられる は、表明の読み取りの失敗を確かめる。
//
// **transcript のファイルが消えていることがある**（人間が掃除した、別のプロセスが消した）。
// **読めないことは turn を終えられない理由にならない。**表明が無いものとして扱う。
//
// 目的: transcript を読めなくても、continuo が落ちないこと。
// 与える情報: 存在しない transcript のパスを指す hook。
// 成功条件: 落ちず、**Status を動かさない**こと（表明が読めないので動かす根拠がない）。
func TestExternalFailure_transcriptを読めなくてもturnを終えられる(t *testing.T) {
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

	// **存在しないパスを渡す。**hook 自体は届くが、transcript は読めない。
	missing := filepath.Join(t.TempDir(), "no-such-transcript.jsonl")
	fx.Orc.OnHook(stopEvent(fx.Sessions[0], missing, "p1"))
	// **`Stop` を積んでから返す。**ここから turn の終わりの判定が始まる。
	releasePrompt()
	time.Sleep(2 * time.Second)

	// **表明を読めないので Status を動かさない。**
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "In Progress" {
		t.Errorf("表明を読めないのに Status を動かしている: %s", got)
	}
}
