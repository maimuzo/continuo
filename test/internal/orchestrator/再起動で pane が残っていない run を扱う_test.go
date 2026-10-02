// {"RUCM-CFG-SHA256": "70dbd40753b435f644a9dfff6c32df1c9a19c9d58d332f45f6aa54783462f90d", "SOURCE": "docs/spec/usecases/particular_case/再起動で pane が残っていない run を扱う.cfg.json"}
//
// **ユースケース記述「再起動で pane が残っていない run を扱う」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package orchestrator_test

import (
	"context"
	"os"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// {"RUCM-PATH": "P001"}
//
// Test_再起動でpaneが残っていないrunを扱う_P001_paneが無い実行中のrunは既定では次の巡回に委ねる は、
// `restart.orphan_running_action` の既定（`redispatch`）を確かめる。
//
// 目的: 設計 3-4。**復元の中で dispatch すると、着手の段11 の待ちで最大1時間止まる。**
//
// 与える情報: 身元ファイルはあるが pane が無い、`In Progress` の run。
//
// 成功条件: 印にも実行中の一覧にも入らず、Status も動かない。worktree は残る。
func Test_再起動でpaneが残っていないrunを扱う_P001_paneが無い実行中のrunは既定では次の巡回に委ねる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx) // pane は1つも無い

	restore(t, fx)

	if got := fx.Orc.RunningIdentifiers(); len(got) != 0 {
		t.Fatalf("復元の中で dispatch してしまった: got %v", got)
	}
	if got := fx.Tracker.StateOf(issue.ID); got != "In Progress" {
		t.Fatalf("Status を動かしてしまった: got %q", got)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree を消してしまった: %v", err)
	}
}

// {"RUCM-PATH": "P002"}
//
// Test_再起動でpaneが残っていないrunを扱う_P002_paneが無い実行中のrunをdispatch_stateへ戻せる は、
// `restart.orphan_running_action: to_dispatch_state` を確かめる。
//
// 目的: 設計 3-4 の3値の分岐。
//
// 与える情報: `to_dispatch_state` の設定と、pane の無い `In Progress` の run。
//
// 成功条件: Status が `dispatch_state`（Ready）へ戻る。
func Test_再起動でpaneが残っていないrunを扱う_P002_paneが無い実行中のrunをdispatch_stateへ戻せる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Restart.OrphanRunningAction = "to_dispatch_state"
	}})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx)

	restore(t, fx)

	if got := fx.Tracker.StateOf(issue.ID); got != "Ready" {
		t.Fatalf("dispatch_state へ戻していない: got %q, want %q", got, "Ready")
	}
}

// {"RUCM-PATH": "P004"}
//
// Test_再起動でpaneが残っていないrunを扱う_P004_paneが無い実行中のrunをfailure_stateへ落とせる は、
// `restart.orphan_running_action: to_failure_state` を確かめる。
//
// 目的: 設計 3-4 の3値の分岐。
//
// 与える情報: `to_failure_state` の設定と、pane の無い `In Progress` の run。
//
// 成功条件: Status が `failure_state`（Blocked）へ落ち、worktree は残る。
func Test_再起動でpaneが残っていないrunを扱う_P004_paneが無い実行中のrunをfailure_stateへ落とせる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Restart.OrphanRunningAction = "to_failure_state"
	}})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx)

	restore(t, fx)

	if got := fx.Tracker.StateOf(issue.ID); got != "Blocked" {
		t.Fatalf("failure_state へ落としていない: got %q", got)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree を消してしまった: %v", err)
	}
}

// {"RUCM-PATH": "P005"}
//
// Test_再起動でpaneが残っていないrunを扱う_P005_paneが無くIn_Reviewなら何もしない は、段8 の表の「それ以外」を確かめる。
//
// 目的: 設計 3-4。**Status を巻き戻してはならない。**`restart.orphan_running_action` も見ない。
//
// 与える情報: `to_dispatch_state` の設定と、pane の無い `In Review` の run。
//
// 成功条件: Status が `In Review` のまま動かない。
func Test_再起動でpaneが残っていないrunを扱う_P005_paneが無くIn_Reviewなら何もしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Restart.OrphanRunningAction = "to_dispatch_state"
	}})
	issue := sampleIssue(188, "In Review")
	fx.Tracker.AddIssue(issue)
	prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx)

	restore(t, fx)

	if got := fx.Tracker.StateOf(issue.ID); got != "In Review" {
		t.Fatalf("引き渡し状態の Status を動かしてしまった: got %q", got)
	}
}

// {"RUCM-PATH": "P006"}
//
// Test_再起動でpaneが残っていないrunを扱う_P006_cleanup_on_statesのissueのworktreeを片付ける は、
// 設計 3-9 の手順6 を確かめる。
//
// 目的: 起動時に、終わっている issue の worktree を片付ける。
//
// 与える情報: `cleanup.on_states` が `Archived` の設定と、Status が `Archived` の
// 身元ファイル付き worktree（pane は無い）。
//
// 成功条件: worktree が消える。
//
// **段8 でも同じ worktree が片付く経路があるため、このテストが確かめているのは
// 「起動時の掃除が復元のあとに走っても壊れないこと」でもある。**
func Test_再起動でpaneが残っていないrunを扱う_P006_cleanup_on_statesのissueのworktreeを片付ける(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Cleanup.OnStates = []string{"Archived"}
		cfg.Tracker.TerminalStates = []string{"Done"}
		cfg.Cleanup.RequirePushed = false
	}})
	issue := sampleIssue(188, "Archived")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx)

	result, _ := restore(t, fx)
	fx.Orc.SweepOnStartup(context.Background(), result)

	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("起動時の掃除で worktree を片付けていない: %v", err)
	}
}
