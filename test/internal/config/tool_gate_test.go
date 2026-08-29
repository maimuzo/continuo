package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// 目的: `claude.tool_gate` の既定値を固定する（設計 3-64）。
//
// **既定は「公開リポジトリの issue にだけ掛ける」である。**公開リポジトリの issue は
// 誰でも書けるので、指示そのものが攻撃になりうる。既定を `off` にすると、
// 何も書かずに使い始めた人だけが守られない。
//
// **既定のモデルは空である**（設計 3-64）。判定に使えるモデルの名前の一覧は公式文書に無く、
// 通らない名前を書いたときにどう倒れるかを確かめていない。**だから名前を書かず、
// Claude Code の既定に任せる。**
//
// 与える情報: config.DefaultConfig()。
// 成功条件: mode が `public_only`、モデルが空、判定に回す道具が Bash だけであること。
func TestDefaultConfig_危ない呼び出しの判定は公開リポジトリだけに掛ける(t *testing.T) {
	got := config.DefaultConfig().Claude.ToolGate
	if got.Mode != config.ClaudeToolGateModePublicOnly {
		t.Fatalf("既定の mode が違う: got %q, want %q", got.Mode, config.ClaudeToolGateModePublicOnly)
	}
	if got.Model != "" {
		t.Fatalf("既定の判定モデルに名前が入っている（受け付ける名前の一覧が公式文書に無い）: %q", got.Model)
	}
	if !slices.Equal(got.Tools, []string{"Bash"}) {
		t.Fatalf("既定で判定に回す道具が違う: got %v, want [Bash]", got.Tools)
	}
}

// 目的: **`tool_gate` を1行も書いていない `WORKFLOW.md` が、そのまま読めること**を固定する
// （設計 3-64）。
//
// **これが無いと、既存の利用者の設定ファイルが読めなくなったことに気づけない。**
// 他のテストは雛形の1行を差し替える形なので、雛形に `tool_gate` が入っている限り
// 「キーが必須になった」という壊れ方を1件も捕まえられない。
//
// 与える情報: 雛形から `tool_gate` の塊を丸ごと落とした WORKFLOW.md
// （`tool_gate` の文字列が1度も出てこないことを、読ませる前に確かめる）。
// 成功条件: 読み込めて、`claude.tool_gate` が DefaultConfig と同じ値になること。
func TestLoad_toolGateを書いていない設定ファイルが既定のまま読める(t *testing.T) {
	base := scaffold.TemplateWithValues(scaffold.Values{Owner: "octocat", ProjectNumber: 3})
	var b strings.Builder
	dropping := false
	for _, l := range strings.Split(base, "\n") {
		trimmed := strings.TrimLeft(l, " \t")
		if strings.HasPrefix(trimmed, "tool_gate:") {
			// 塊の先頭。ここから、字下げが浅くなるか空行になるまで落とす。
			dropping = true
			continue
		}
		if dropping {
			if strings.TrimSpace(l) == "" || len(l)-len(trimmed) <= 2 {
				dropping = false
			} else {
				continue
			}
		}
		b.WriteString(l + "\n")
	}
	body := b.String()
	if strings.Contains(body, "tool_gate") {
		t.Fatalf("tool_gate の行が落としきれていない（このテストが意味を失う）:\n%s", body)
	}

	path := filepath.Join(t.TempDir(), "WORKFLOW.md")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("tool_gate を書いていない設定ファイルを読めません: %v", err)
	}

	want := config.DefaultConfig().Claude.ToolGate
	got := cfg.Config.Claude.ToolGate
	if got.Mode != want.Mode {
		t.Errorf("mode が既定と違う: got %q, want %q", got.Mode, want.Mode)
	}
	if got.Model != want.Model {
		t.Errorf("model が既定と違う: got %q, want %q", got.Model, want.Model)
	}
	if !slices.Equal(got.Tools, want.Tools) {
		t.Errorf("tools が既定と違う: got %v, want %v", got.Tools, want.Tools)
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
