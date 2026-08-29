package config_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// 目的: `claude.tool_gate` の既定値を固定する（設計 3-64）。
//
// **既定は「公開リポジトリの issue にだけ掛ける」である。**公開リポジトリの issue は
// 誰でも書けるので、指示そのものが攻撃になりうる。既定を `off` にすると、
// 何も書かずに使い始めた人だけが守られない。
//
// 与える情報: config.DefaultConfig()。
// 成功条件: mode が `public_only`、判定に回す道具が Bash だけであること。
func TestDefaultConfig_危ない呼び出しの判定は公開リポジトリだけに掛ける(t *testing.T) {
	got := config.DefaultConfig().Claude.ToolGate
	if got.Mode != config.ClaudeToolGateModePublicOnly {
		t.Fatalf("既定の mode が違う: got %q, want %q", got.Mode, config.ClaudeToolGateModePublicOnly)
	}
	if got.Model == "" {
		t.Fatal("既定の判定モデルが空である（雛形に書いた値と食い違う）")
	}
	if !slices.Equal(got.Tools, []string{"Bash"}) {
		t.Fatalf("既定で判定に回す道具が違う: got %v, want [Bash]", got.Tools)
	}
}

// 目的: `claude.tool_gate` の使えない値を、起動する前に弾くこと（設計 3-64 / 8-1）。
//
// **知らない mode を黙って `off` に丸めてはならない。**丸めると、掛けたつもりの判定が
// 1度も走らないまま、無人で走り続ける。
//
// 与える情報: `mode` または `tools` の1行だけを差し替えた WORKFLOW.md。
// 成功条件: エラーになり、どのキーが悪いかがエラーの文面に入っていること。
func TestValidate_危ない呼び出しの判定の設定を弾く(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		line string
		want string
	}{
		{"mode が知らない値", "mode", "    mode: always", "claude.tool_gate.mode"},
		{"mode が空", "mode", `    mode: ""`, "claude.tool_gate.mode"},
		{"道具の名前が空", "tools", `    tools: ["Bash", ""]`, "claude.tool_gate.tools"},
		{"同じ道具が2回", "tools", `    tools: ["Bash", "Bash"]`, "claude.tool_gate.tools"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := loadWithReplaced(t, tc.key, tc.line)
			if err == nil {
				t.Fatalf("%s を弾いていない", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("どのキーが悪いか分からない: %v", err)
			}
		})
	}
}

// 目的: 受け付けると決めた mode は、そのまま起動を通ることを確かめる（設計 3-64）。
//
// **弾く側だけを見ていると、全部弾く実装でもテストが通る。**
//
// 与える情報: `mode` を `off` / `on` / `public_only` に差し替えた WORKFLOW.md。
// 成功条件: どれも読み込めること。
func TestValidate_危ない呼び出しの判定のmodeは3つとも通る(t *testing.T) {
	for _, mode := range config.ClaudeToolGateModes {
		t.Run(mode, func(t *testing.T) {
			if err := loadWithReplaced(t, "mode", "    mode: "+mode); err != nil {
				t.Fatalf("mode: %s を弾いている: %v", mode, err)
			}
		})
	}
}
