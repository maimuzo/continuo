// {"RUCM-CFG-SHA256": "3604427e4f9b11445c8095a767711511d937a95d502844f4894e3fd53994e26f", "SOURCE": "docs/spec/usecases/particular_case/issue を1件処理する.cfg.json"}
//
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
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// TestExternalFailure_カンバンを読めなくても巡回は止まらない は、候補の取得の失敗を確かめる。
//
// 目的: `FetchIssuesByStates` が失敗しても、continuo が落ちないこと。
// 与える情報: 常に失敗する候補の取得。
// 成功条件: 巡回が返り、**worktree も pane も作らない**こと。
func TestExternalFailure_カンバンを読めなくても巡回は止まらない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.SetStatesError(errors.New("GitHub へ繋がりません"))

	// **落ちないことを確かめる。**panic すればここで止まる。
	fx.Orc.Tick(context.Background())

	if got := fx.Herdr.CountMethod(herdr.MethodWorktreeOpen); got != 0 {
		t.Errorf("カンバンを読めないのに worktree を開いている: %d 回", got)
	}
}

// TestExternalFailure_Statusの選択肢が食い違ったら着手しない は、起動時検査の失敗を確かめる。
//
// **人間がカンバンの Status の選択肢を改名することがある。**
// **設定と食い違ったまま着手すると、continuo は存在しない選択肢へ書こうとして毎回失敗する。**
//
// 目的: 選択肢の照合に失敗したら、その巡回では着手しないこと。
// 与える情報: 常に失敗する `VerifyStatusOptions`。
// 成功条件: worktree を開かず、Status も動かさないこと。
func TestExternalFailure_Statusの選択肢が食い違ったら着手しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.SetVerifyError(errors.New("Status の選択肢名が設定と一致しません"))

	fx.Orc.Tick(context.Background())
	time.Sleep(1500 * time.Millisecond)

	if got := fx.Herdr.CountMethod(herdr.MethodWorktreeOpen); got != 0 {
		t.Errorf("選択肢が食い違っているのに worktree を開いている: %d 回", got)
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "Ready" {
		t.Errorf("着手していないのに Status を動かしている: %s", got)
	}
}

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

// {"RUCM-PATH": "P034"}
//
// TestExternalFailure_paneのlabelを書けなければ着手しない は、段8 の失敗を確かめる。
//
// **pane の label は issue の URL である。**再起動時にどの pane がどの issue かを
// 見分ける手がかりなので、書けないまま進むと復元できない pane ができる。
//
// 目的: `pane.rename` が失敗したら着手を止めること。
// 与える情報: 常に失敗する `pane.rename`。
// 成功条件: agent を起動せず、turn も送らないこと。
func TestExternalFailure_paneのlabelを書けなければ着手しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Herdr.Handle(herdr.MethodPaneRename, func(_ map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "internal", Message: "label を書けません"}
	})

	fx.Orc.Tick(context.Background())
	time.Sleep(2 * time.Second)

	if got := fx.Herdr.CountMethod(herdr.MethodAgentStart); got != 0 {
		t.Errorf("label を書けないのに agent を起動している: %d 回", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); got != 0 {
		t.Errorf("label を書けないのに turn を送っている: %d 回", got)
	}
}

// TestExternalFailure_paneを引けなければ着手しない は、段8 の pane の解決の失敗を確かめる。
//
// 目的: `pane.list` が失敗したら着手を止めること。
// 与える情報: 常に失敗する `pane.list`。
// 成功条件: agent を起動しないこと。
func TestExternalFailure_paneを引けなければ着手しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Herdr.Handle(herdr.MethodPaneList, func(_ map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "internal", Message: "pane の一覧を取れません"}
	})

	fx.Orc.Tick(context.Background())
	time.Sleep(2 * time.Second)

	if got := fx.Herdr.CountMethod(herdr.MethodAgentStart); got != 0 {
		t.Errorf("pane を引けないのに agent を起動している: %d 回", got)
	}
}

// TestExternalFailure_Statusを書けなくても着手を続ける は、段2 の失敗の扱いを確かめる。
//
// **Status を書けないことは、着手を諦める理由になる。**
// 書けないまま worktree を作ると、**カンバンからは Ready のままに見えるのに実体が動く。**
// 次の巡回で二重に着手される。
//
// 目的: `UpdateStatus` が失敗したら worktree を作らないこと。
// 与える情報: 常に失敗する `UpdateStatus`。
// 成功条件: worktree を開かないこと。
func TestExternalFailure_Statusを書けなければworktreeを作らない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.SetUpdateError(errors.New("Status を書けません"))

	fx.Orc.Tick(context.Background())
	time.Sleep(2 * time.Second)

	if got := fx.Herdr.CountMethod(herdr.MethodWorktreeOpen); got != 0 {
		t.Errorf("Status を書けないのに worktree を開いている: %d 回", got)
	}
}

// {"RUCM-PATH": "P012"}
//
// TestExternalFailure_turnの終わりに issue が消えていたら手放す は、設計 3-10 を確かめる。
//
// **turn が終わってから issue を取り直したとき、カンバンから返ってこないことがある**
// （人間がカンバンから外した、archive した）。**continuo はその issue の面倒を見ない。**
//
// 目的: 取り直しで見つからない issue を、印から外して手放すこと。
// 与える情報: turn の途中でカンバンから消える issue。
// 成功条件: 印から外れ、**worktree は残る**こと（人間が成果を見られる）。
func TestExternalFailure_turnの終わりにissueが消えていたら手放す(t *testing.T) {
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

	// **カンバンから消す。**取り直しは「見つからない」を返す。
	fx.Tracker.RemoveIssue("PVTI_item188")

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\n\nCONTINUO-STATUS: review", false),
	})
	fx.Orc.OnHook(stopEvent(fx.Sessions[0], path, "p1"))
	// **`Stop` を積んでから返す。**ここから turn の終わりの判定が始まる。
	releasePrompt()

	// **手放したことをログで確かめる。**手放した run は印に残したままバックオフへ入る
	// （設計 3-21）ので、**印から外れたかどうかでは確かめられない。**
	waitFor(t, 20*time.Second, "手放したことがログに出る", func() bool {
		return strings.Contains(fx.Logs.String(), "カンバンから見えなくなりました")
	})
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
