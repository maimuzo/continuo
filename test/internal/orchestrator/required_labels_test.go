// `tracker.required_labels` による dispatch の絞り込みの検査である。
//
// **これを取り違えると、着手してはいけない issue に着手する。**
// 逆に厳しすぎると、着手すべき issue が永久に Ready で止まる。
package orchestrator_test

import (
	"context"
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
