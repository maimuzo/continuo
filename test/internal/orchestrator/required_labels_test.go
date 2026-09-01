// {"RUCM-CFG-SHA256": "4a61db11c52f5ba42b23b7180d4dfe2d79b39f257e065f54fe735fd3e48d11e6", "SOURCE": "docs/spec/usecases/particular_case/issue を1件処理する.cfg.json"}
//
// `tracker.required_labels` による dispatch の絞り込みの検査である。
//
// **これを取り違えると、着手してはいけない issue に着手する。**
// 逆に厳しすぎると、着手すべき issue が永久に Ready で止まる。
package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/tracker"
)

// issueWithLabels は指定のラベルを持つ issue を作る。
//
// number: issue の番号。
// labels: 付けるラベル。
// 戻り値: ラベルを差し替えた issue。
func issueWithLabels(number int, labels ...string) tracker.Issue {
	issue := sampleIssue(number, "Ready")
	issue.Labels = labels
	return issue
}

// TestRequiredLabels_必須ラベルが空なら全部に着手する は、既定の振る舞いを確かめる。
//
// 目的: `required_labels` を書かなければ、ラベルによる絞り込みを一切しないこと。
// 与える情報: ラベルを1つも持たない issue と、既定の設定。
// 成功条件: 着手して turn が送られること。
func TestRequiredLabels_必須ラベルが空なら全部に着手する(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(issueWithLabels(188))
	holdPrompt(fx)

	fx.Orc.Tick(context.Background())

	waitFor(t, 10*time.Second, "turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
}

// {"RUCM-PATH": "P040"}
//
// TestRequiredLabels_1つでも欠けたら着手しない は、絞り込みが効くことを確かめる。
//
// 目的: `required_labels` に並べたラベルを**全部**持っている issue だけに着手すること。
// 与える情報: 必須2つのうち1つしか持たない issue。
// 成功条件: **worktree も pane も作らない**（herdr を1回も叩かない）。
func TestRequiredLabels_1つでも欠けたら着手しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.RequiredLabels = []string{"bug", "ready-for-ai"}
		},
	})
	fx.Tracker.AddIssue(issueWithLabels(188, "bug"))

	fx.Orc.Tick(context.Background())

	// **「起きない」ことを確かめるので、起きるだけの時間を与えてから見る。**
	time.Sleep(2 * time.Second)
	if got := fx.Herdr.CountMethod(herdr.MethodWorktreeOpen); got != 0 {
		t.Errorf("必須ラベルが欠けているのに worktree を開いている: %d 回", got)
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "Ready" {
		t.Errorf("Status を動かしている: %s", got)
	}
}

// TestRequiredLabels_全部そろっていれば着手する は、絞り込みを通る側を確かめる。
//
// 目的: 必須ラベルを全部持っていれば、余分なラベルがあっても着手すること。
// 与える情報: 必須2つに加えて関係ないラベルを持つ issue。
// 成功条件: turn が送られること。
func TestRequiredLabels_全部そろっていれば着手する(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.RequiredLabels = []string{"bug", "ready-for-ai"}
		},
	})
	fx.Tracker.AddIssue(issueWithLabels(188, "ready-for-ai", "documentation", "bug"))
	holdPrompt(fx)

	fx.Orc.Tick(context.Background())

	waitFor(t, 10*time.Second, "turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
}

// TestRequiredLabels_空白だけの指定は条件にしない は、設定の書き間違いに強いことを確かめる。
//
// **`required_labels: ["bug", ""]` のように空の要素が混ざると、
// 誰も満たせない条件になって全 issue が止まる。**空白だけの要素は無視する。
//
// 目的: 空文字と空白だけの要素を条件から外すこと。
// 与える情報: 空文字と空白を混ぜた `required_labels`。
// 成功条件: `bug` さえ持っていれば着手すること。
func TestRequiredLabels_空白だけの指定は条件にしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.RequiredLabels = []string{"bug", "", "   "}
		},
	})
	fx.Tracker.AddIssue(issueWithLabels(188, "bug"))
	holdPrompt(fx)

	fx.Orc.Tick(context.Background())

	waitFor(t, 10*time.Second, "turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
}

// TestRequiredLabels_大文字小文字を無視して比べる は、ラベルの表記ゆれに強いことを確かめる。
//
// **ラベルはアダプタが正規化済みである**（前後の空白を落として小文字。設計 3-13）。
// 設定側に大文字で書かれていても、同じラベルとして扱う。
//
// 目的: 設定の `Bug` と issue の `bug` を同じものとして扱うこと。
// 与える情報: 大文字で書いた `required_labels`。
// 成功条件: 着手すること。
func TestRequiredLabels_大文字小文字を無視して比べる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.RequiredLabels = []string{"  BUG  "}
		},
	})
	fx.Tracker.AddIssue(issueWithLabels(188, "bug"))
	holdPrompt(fx)

	fx.Orc.Tick(context.Background())

	waitFor(t, 10*time.Second, "turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
}

// 目的: 必須のラベルが足りずに飛ばしたことを、ログから探せることを確かめる（issue #134）。
//
// **docs/FAQ.md が、この文面を grep する手順を公開している。**
// 文面が変わると、その案内が空振りする。**ここで固定する。**
//
// **1回だけ出すことも確かめる。**required_labels は大量の対象外を除ける道具なので、
// 巡回のたびに出すと、同じ節が読ませたい他の行が流れて埋まる。
//
// 与える情報: 必須のラベルが付いていない issue と、2回の巡回。
// 成功条件: 1回目に INFO が出て、足りないラベルの名前が載っていること。2回目は出ないこと。
func TestRequiredLabels_足りないラベルをログから探せる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.RequiredLabels = []string{"Continuo"}
		},
	})
	fx.Tracker.AddIssue(issueWithLabels(188, "bug"))

	fx.Orc.Tick(context.Background())
	first := fx.Logs.String()

	// **FAQ が grep する文面。**変えるなら FAQ も直すこと。
	if !strings.Contains(first, "必須のラベルが揃っていないので飛ばします") {
		t.Fatalf("FAQ が grep する文面が出ていない:\n%s", first)
	}
	// **足りないラベルの名前が出ること。**2つの一覧を並べるだけでは、差分を人が目で取ることになる。
	// **照合は小文字で通るので、ログもその形で出す。**設定は "Continuo" と書いてある。
	if !strings.Contains(first, "continuo") {
		t.Errorf("足りないラベルの名前が出ていない:\n%s", first)
	}

	// 2回目の巡回では出さない。
	before := len(fx.Logs.String())
	fx.Orc.Tick(context.Background())
	second := fx.Logs.String()[before:]
	if strings.Contains(second, "必須のラベルが揃っていないので飛ばします") {
		t.Errorf("2回目の巡回でも出している（1回だけのはず）:\n%s", second)
	}
}
