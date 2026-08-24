// {"RUCM-CFG-SHA256": "7d4f46f994df245a902936252426cd1229b70db00e06998f2a7b1395f089a50f", "SOURCE": "docs/spec/usecases/particular_case/issue を1件処理する.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**
// 空きスロットの数え方（`agent.max_concurrent_agents` と
// `agent.max_concurrent_agents_by_state`）の検査である。
//
// **数え間違えると、上限を越えて Claude Code が立つ。**枠を使い切るだけでなく、
// 同じ worktree に2つ立つ事故にも繋がる。
package orchestrator_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// {"RUCM-PATH": "P017"}
//
// TestSlots_上限まで着手したらそれ以上着手しない は、全体の上限を確かめる。
//
// 目的: `max_concurrent_agents` を超えて dispatch しないこと。
// 与える情報: 上限 2 の設定と、Ready の issue 3件。
// 成功条件: **2件だけが着手される**（3件目は Ready のまま）。
func TestSlots_上限まで着手したらそれ以上着手しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Agent.MaxConcurrentAgents = 2 },
	})
	for _, n := range []int{188, 189, 190} {
		fx.Tracker.AddIssue(sampleIssue(n, "Ready"))
	}
	holdPrompt(fx)

	fx.Orc.Tick(context.Background())

	waitFor(t, 15*time.Second, "2件が着手される", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) >= 2
	})
	// **3件目が着手されないことを確かめるので、着手しうる時間を与えてから見る。**
	time.Sleep(2 * time.Second)
	if got := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); got > 2 {
		t.Errorf("上限 2 を越えて着手している: turn を %d 回送った", got)
	}
	running := 0
	for _, n := range []int{188, 189, 190} {
		if fx.Tracker.StateOf(fmt.Sprintf("PVTI_item%d", n)) == "In Progress" {
			running++
		}
	}
	if running != 2 {
		t.Errorf("In Progress の件数が上限と合わない: %d 件", running)
	}
}

// TestSlots_上限が1なら1件ずつ着手する は、直列に回す設定を確かめる。
//
// 目的: `max_concurrent_agents: 1` のとき、同時に2件目へ進まないこと。
// 与える情報: 上限 1 の設定と、Ready の issue 2件。
// 成功条件: 着手が1件だけであること。
func TestSlots_上限が1なら1件ずつ着手する(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Agent.MaxConcurrentAgents = 1 },
	})
	for _, n := range []int{188, 189} {
		fx.Tracker.AddIssue(sampleIssue(n, "Ready"))
	}
	holdPrompt(fx)

	fx.Orc.Tick(context.Background())

	waitFor(t, 15*time.Second, "1件が着手される", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) >= 1
	})
	time.Sleep(2 * time.Second)
	if got := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); got != 1 {
		t.Errorf("上限 1 なのに %d 件へ turn を送っている", got)
	}
}

// TestSlots_状態ごとの上限も効く は、`max_concurrent_agents_by_state` を確かめる。
//
// **これから dispatch する候補は `running_state`（既定 In Progress）の枠を消費するものとして数える**
// （設計 3-16 の段-1）。取得した時点ではまだ Ready だが、dispatch すれば In Progress になるためである。
//
// 目的: 全体の上限に余裕があっても、状態ごとの上限で止まること。
// 与える情報: 全体 5・`In Progress` は 1 の設定と、Ready の issue 3件。
// 成功条件: 着手が1件だけであること。
func TestSlots_状態ごとの上限も効く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Agent.MaxConcurrentAgents = 5
			cfg.Agent.MaxConcurrentAgentsByState = map[string]int{"In Progress": 1}
		},
	})
	for _, n := range []int{188, 189, 190} {
		fx.Tracker.AddIssue(sampleIssue(n, "Ready"))
	}
	holdPrompt(fx)

	fx.Orc.Tick(context.Background())

	waitFor(t, 15*time.Second, "1件が着手される", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) >= 1
	})
	time.Sleep(2 * time.Second)
	if got := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); got != 1 {
		t.Errorf("状態ごとの上限 1 なのに %d 件へ turn を送っている", got)
	}
}

// TestSlots_状態ごとの上限は大文字小文字を無視する は、設定の表記ゆれに強いことを確かめる。
//
// 目的: `in progress` と書いても `In Progress` の枠として扱うこと。
// 与える情報: 小文字で書いた `max_concurrent_agents_by_state`。
// 成功条件: 着手が1件だけであること（キーが一致しないと上限が効かず、複数着手してしまう）。
func TestSlots_状態ごとの上限は大文字小文字を無視する(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Agent.MaxConcurrentAgents = 5
			cfg.Agent.MaxConcurrentAgentsByState = map[string]int{"  in progress  ": 1}
		},
	})
	for _, n := range []int{188, 189} {
		fx.Tracker.AddIssue(sampleIssue(n, "Ready"))
	}
	holdPrompt(fx)

	fx.Orc.Tick(context.Background())

	waitFor(t, 15*time.Second, "1件が着手される", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) >= 1
	})
	time.Sleep(2 * time.Second)
	if got := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); got != 1 {
		t.Errorf("キーが一致せず上限が効いていない: %d 件へ turn を送った", got)
	}
}

// TestSlots_該当しない状態には全体の上限だけを適用する は、キーが無い場合を確かめる。
//
// 目的: `max_concurrent_agents_by_state` に `running_state` のキーが無ければ、
// 全体の上限だけで判定すること。
// 与える情報: 関係ない Status のキーだけを持つ設定と、全体の上限 2。
// 成功条件: 2件が着手されること。
func TestSlots_該当しない状態には全体の上限だけを適用する(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Agent.MaxConcurrentAgents = 2
			cfg.Agent.MaxConcurrentAgentsByState = map[string]int{"In Review": 1}
		},
	})
	for _, n := range []int{188, 189} {
		fx.Tracker.AddIssue(sampleIssue(n, "Ready"))
	}
	holdPrompt(fx)

	fx.Orc.Tick(context.Background())

	waitFor(t, 15*time.Second, "2件とも着手される", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) >= 2
	})
}
