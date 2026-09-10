// `claude.permission_mode` の既定値と、受け付ける値の検査である（設計 3-11。issue #259）。
//
// **既定は `auto` である。**判定役が会話の流れを読むので、issue のコメントで出した許可が通り、
// `.claude/` 配下と `.mcp.json`（保護対象パス）へも書ける。
// **`dontAsk` はこれらがどうやっても通らない**（許可の一覧に足しても、`PreToolUse` hook が
// `allow` を返しても、`Bash` のリダイレクトでも拒否される。2026-09-09 に Claude Code 2.1.266 で実測）。
//
// **`dontAsk` を捨てない。**入力を待たないことが保証される唯一のモードなので、
// 無人で回すことを最優先する利用者は選べたままにする。
package config_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// 目的: `claude.permission_mode` の既定値を固定する（設計 3-11）。
//
// **既定を変えると、これから `continuo init` する人の挙動が変わる。**
// 既に `WORKFLOW.md` を持っている人には届かない（front matter に書いてある値が勝つ）ので、
// **移行の手順は docs/upgrading.md が受け持つ。**
//
// 与える情報: config.DefaultConfig()。
// 成功条件: PermissionMode が `auto` であること。
func TestDefaultConfig_権限モードの既定はauto(t *testing.T) {
	got := config.DefaultConfig().Claude.PermissionMode
	if got != config.ClaudePermissionModeAuto {
		t.Fatalf("既定の permission_mode が違う: got %q, want %q", got, config.ClaudePermissionModeAuto)
	}
}

// 目的: 既定の拒否リストに `AskUserQuestion` が入っていることを固定する（設計 3-11）。
//
// **これはエージェント自身が人間に選択肢を出す道具で、判定役とは関係が無い。**
// `dontAsk` は元から拒否するが、**`auto` にすると拒否が外れる。**
// 外れたまま無人で走らせると、スキルなどから呼ばれた瞬間に質問の画面が出て pane が止まり、
// **continuo が次に送る指示が、その質問への回答として消費される**（2026-09-09 に実測）。
//
// 与える情報: config.DefaultConfig()。
// 成功条件: Deny に `AskUserQuestion` が入っていること。
func TestDefaultConfig_既定でAskUserQuestionを禁じる(t *testing.T) {
	got := config.DefaultConfig().Claude.Permissions.Deny
	if !slices.Contains(got, "AskUserQuestion") {
		t.Fatalf("既定の deny に AskUserQuestion が無い: %v", got)
	}
}

// 目的: 許可リストを据え置いたことを固定する（設計 3-11）。
//
// **`auto` では、任意のコード実行を許す広いルールが落とされる**と公式が書いているが、
// **何が落とされるかの一覧は示されていない。**だから測っていないことを根拠にはしない。
// **据え置く理由は1つで足りる。消すと `dontAsk` を選び直した利用者だけが壊れる。**
//
// 与える情報: config.DefaultConfig()。
// 成功条件: allow が6つの道具を持つこと。
func TestDefaultConfig_許可リストは据え置く(t *testing.T) {
	got := config.DefaultConfig().Claude.Permissions.Allow
	want := []string{"Bash", "Read", "Glob", "Grep", "Edit", "Write"}
	if !slices.Equal(got, want) {
		t.Fatalf("既定の allow が違う: got %v, want %v", got, want)
	}
}

// 目的: `permission_mode` に書ける値を固定する（設計 3-11）。
//
// **2つだけである。**`default` と `acceptEdits` は保護対象パスへの書き込みで確認の画面を出すので
// （公式の permission modes の表）、無人運転では止まる。
// **`bypassPermissions` は使わない**（設計 2-1 が `--dangerously-skip-permissions` を使わないと決めている）。
//
// 与える情報: `permission_mode` を `auto` / `dontAsk` に差し替えた WORKFLOW.md。
// 成功条件: どちらも読み込めること。
func TestValidate_権限モードは2つとも通る(t *testing.T) {
	for _, mode := range config.ClaudePermissionModes {
		t.Run(mode, func(t *testing.T) {
			if err := loadWithReplaced(t, "permission_mode", "  permission_mode: "+mode); err != nil {
				t.Fatalf("permission_mode: %s を弾いている: %v", mode, err)
			}
		})
	}
}

// 目的: 一覧に無い値と空文字を弾くことを固定する（設計 3-11）。
//
// **空文字を通してはならない。**空だと orchestrator が `--permission-mode` を付けずに起動するため、
// **利用者の手元の Claude Code の既定で走る。**どのモードになるかは continuo から分からない。
//
// **黙って無視しない。**書いたつもりの設定が効いていないことに、利用者は気づけない。
//
// **値を書かずにキーだけ置いた場合**（`permission_mode:`）**は、ここでは弾かれない。**
// YAML の null になり、front matter が上書きしないので既定値の `auto` が残るためである。
// **それは「書いたつもりの設定が効いていない」には当たらない**（既定と同じ値で走る）。
//
// 与える情報: 一覧に無い値（`bypassPermissions` / `default` / でたらめ）と、明示的な空文字。
// 成功条件: 4つとも弾かれ、エラーにキーの名前が出ること。
func TestValidate_一覧に無い権限モードと空文字を弾く(t *testing.T) {
	for _, mode := range []string{"bypassPermissions", "default", "sonomama", `""`} {
		t.Run("mode="+mode, func(t *testing.T) {
			err := loadWithReplaced(t, "permission_mode", "  permission_mode: "+mode)
			if err == nil {
				t.Fatalf("permission_mode: %s を通している", mode)
			}
			if !strings.Contains(err.Error(), "claude.permission_mode") {
				t.Errorf("エラーに設定キーの名前が無い: %v", err)
			}
		})
	}
}
