// {"RUCM-CFG-SHA256": "757606f9abc1bcaae33326d5838d1be26e4b4fd106852bfd13d74105a50aa70c", "SOURCE": "docs/spec/usecases/particular_case/本家のリポジトリへ PR を出す.cfg.json"}
//
// **ユースケース記述「本家のリポジトリへ PR を出す」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package orchestrator_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// {"RUCM-PATH": "P001"}
//
// 目的: `public_only` の判定が、リポジトリの公開・非公開でどう分かれるかを固定する（設計 3-64）。
//
// **取れなかったとき（nil）は掛ける側へ倒す。**分からないものを「公開ではない」と決めない。
//
// **「本家のリポジトリへ PR を出す」の基本フローの、issue のリポジトリを非公開だと分かっているかの検査もここに載る。**あちらの issue は
// 非公開のリポジトリにあるので判定が掛からず、**エージェントは fork への push も本家への PR も
// 待ち時間なしで叩ける。**
//
// 与える情報: `mode: public_only` と、公開・非公開・取れなかった、の3通りの issue。
// 成功条件: 非公開のときだけ判定の hook が載らないこと。**どの場合でも `command` の hook は残ること。**
func Test_本家のリポジトリへPRを出す_P001_公開かどうかで判定を掛けるかが決まる(t *testing.T) {
	private := true
	public := false
	cases := []struct {
		name          string
		repoIsPrivate *bool
		wantGate      bool
	}{
		{name: "公開リポジトリなら掛ける", repoIsPrivate: &public, wantGate: true},
		{name: "非公開リポジトリなら掛けない", repoIsPrivate: &private, wantGate: false},
		{name: "公開かどうかを取れなければ掛ける", repoIsPrivate: nil, wantGate: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := writeSettingsForToolGate(t, config.ClaudeToolGateConfig{
				Mode:  config.ClaudeToolGateModePublicOnly,
				Model: "example-fast-model",
				Tools: []string{"Bash"},
			}, c.repoIsPrivate)
			n := countPromptHooks(got)
			if c.wantGate && n != 1 {
				t.Fatalf("判定の hook が載っていません: %d 件", n)
			}
			if !c.wantGate && n != 0 {
				t.Fatalf("判定の hook が載ってしまっています: %d 件", n)
			}
			if countCommandHooks(got) == 0 {
				t.Fatal("turn の終わりを知るための command の hook まで消えています")
			}
		})
	}
}

// {"RUCM-PATH": "P005"}
//
// 目的: 公開リポジトリの issue に着手したとき、危ない道具の呼び出しを判定させる hook が
// `PreToolUse` に載ること、そして **turn の終わりを知るための `command` の hook が
// 消えていないこと**を確かめる（設計 3-64）。
//
// **「本家のリポジトリへ PR を出す」の代替フロー「公開のリポジトリ」もここに載る。**
// 本家へ PR を出す形は公開のリポジトリでも起こりうる。**そのときは誰でも issue を書けるので、
// 指示そのものが攻撃になりうる。**判定を掛ける側へ倒す。
//
// **`continueOnBlock` が真であることを必ず見る。**偽だと、判定が断った時点で turn が
// そこで終わり、無人運用が壊れる。
//
// 与える情報: `mode: public_only` の設定と、公開リポジトリ（`RepoIsPrivate` が false）の issue。
// 成功条件: `prompt` の hook が1件あり、matcher が `Bash`、`model` が設定に書いたとおりの文字列、
// `continueOnBlock` が真、指示文に `$ARGUMENTS` が入っていること。`command` の hook も残ること。
func Test_本家のリポジトリへPRを出す_P005_公開リポジトリのissueには判定のhookを足す(t *testing.T) {
	public := false
	// モデル名は架空である。**受け付ける名前の一覧が公式文書に無いので、実在の名前を書かない**
	// （設計 3-64c）。ここで確かめたいのは「書いた文字列がそのまま settings.json へ通ること」である。
	got, _ := writeSettingsForToolGate(t, config.ClaudeToolGateConfig{
		Mode:  config.ClaudeToolGateModePublicOnly,
		Model: "example-fast-model",
		Tools: []string{"Bash"},
	}, &public)

	entries := got.Hooks["PreToolUse"]
	if len(entries) != 2 {
		t.Fatalf("PreToolUse の塊が2つ（生存の確認と判定）ではありません: %+v", entries)
	}
	// 1つ目は turn の終わりと生存を知るための `command` の hook である（設計 3-2）。
	if entries[0].Hooks[0].Type != "command" || entries[0].Hooks[0].Command == "" {
		t.Fatalf("生存を知るための command の hook が消えています: %+v", entries[0])
	}
	if entries[0].Matcher != "*" {
		t.Fatalf("command の hook の matcher を絞っています: %q", entries[0].Matcher)
	}

	gate := entries[1]
	if gate.Matcher != "Bash" {
		t.Fatalf("判定に回す道具の matcher が違います: got %q, want %q", gate.Matcher, "Bash")
	}
	if len(gate.Hooks) != 1 {
		t.Fatalf("判定の hook が1件ではありません: %+v", gate.Hooks)
	}
	h := gate.Hooks[0]
	if h.Type != "prompt" {
		t.Fatalf("hook の種別が prompt ではありません: %q", h.Type)
	}
	if !h.ContinueOnBlock {
		t.Fatal("continueOnBlock が真ではありません（断った時点で turn が終わり、無人運用が壊れる）")
	}
	if h.Async {
		t.Fatal("async が付いています（非同期の hook は判定を返せない）")
	}
	if h.Model != "example-fast-model" {
		t.Fatalf("判定させるモデルが違います: got %q, want %q", h.Model, "example-fast-model")
	}
	if !strings.Contains(h.Prompt, "$ARGUMENTS") {
		t.Fatalf("指示文に $ARGUMENTS がありません（判定する呼び出しが渡らない）: %q", h.Prompt)
	}
	if h.Command != "" {
		t.Fatalf("prompt の hook に command が書かれています: %q", h.Command)
	}
}
