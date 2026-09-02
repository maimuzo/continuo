// {"RUCM-CFG-SHA256": "b5cdee62809a11dd51093149b06eba6a835ce3d6326900510463169ac3d95fc5", "SOURCE": "docs/spec/usecases/particular_case/本家のリポジトリへ PR を出す.cfg.json"}
//
// **「本家のリポジトリへ PR を出す」の、判定の hook を足すかどうかの段を固定する。**
// このユースケースの issue は非公開のリポジトリにある。**既定の `public_only` では
// 判定の hook が足されない**ので、エージェントは fork への push も本家への PR も
// 待ち時間なしで叩ける。**公開のリポジトリの issue では逆に足される。**
package orchestrator_test

import (
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// {"RUCM-PATH": "P001"}
//
// 目的: **issue のリポジトリが非公開なら、既定の `public_only` で判定の hook を足さないこと**を
// 確かめる（設計 3-64）。このユースケースでは、エージェントが worktree の外の clone で
// `git` と `gh` を何度も叩く。**1回ごとに判定を挟むと待ち時間だけが積み上がる。**
//
// **turn の終わりを知るための `command` の hook は消えないことも見る。**
// 判定を足さない側でも、そちらは要る（設計 3-2）。
//
// 与える情報: `mode: public_only` の設定と、非公開リポジトリ（`RepoIsPrivate` が true）の issue。
// 成功条件: `prompt` の hook が0件で、`PreToolUse` に `command` の hook が残ること。
func TestUpstreamPR_非公開のissueには判定のhookを足さない(t *testing.T) {
	private := true
	got, _ := writeSettingsForToolGate(t, config.ClaudeToolGateConfig{
		Mode:  config.ClaudeToolGateModePublicOnly,
		Tools: []string{"Bash"},
	}, &private)

	if n := countPromptHooks(got); n != 0 {
		t.Fatalf("非公開のリポジトリなのに判定の hook が足されている: %d 件", n)
	}

	commands := 0
	for _, m := range got.Hooks["PreToolUse"] {
		for _, h := range m.Hooks {
			if h.Type == "command" {
				commands++
			}
		}
	}
	if commands == 0 {
		t.Fatal("turn の終わりを知るための command の hook まで消えている")
	}
}

// {"RUCM-PATH": "P007"}
//
// 目的: **issue のリポジトリが公開なら判定の hook を足すこと**を確かめる（設計 3-64）。
// 本家へ PR を出す形は公開のリポジトリでも起こりうる。**そのときは誰でも issue を書けるので、
// 指示そのものが攻撃になりうる。**判定を掛ける側へ倒す。
//
// **`continueOnBlock` が真であることを必ず見る。**偽だと、判定が断った時点で turn が終わり、
// 本家の PR のレビューを読む段まで届かない。
//
// 与える情報: `mode: public_only` の設定と、公開リポジトリ（`RepoIsPrivate` が false）の issue。
// 成功条件: `prompt` の hook が1件あり、`continueOnBlock` が真であること。
func TestUpstreamPR_公開のissueには判定のhookを足す(t *testing.T) {
	public := false
	got, _ := writeSettingsForToolGate(t, config.ClaudeToolGateConfig{
		Mode:  config.ClaudeToolGateModePublicOnly,
		Tools: []string{"Bash"},
	}, &public)

	if n := countPromptHooks(got); n != 1 {
		t.Fatalf("公開のリポジトリなのに判定の hook が1件でない: %d 件", n)
	}
	for _, m := range got.Hooks["PreToolUse"] {
		for _, h := range m.Hooks {
			if h.Type == "prompt" && !h.ContinueOnBlock {
				t.Fatal("continueOnBlock が偽になっている（断られた時点で turn が終わる）")
			}
		}
	}
}
