package orchestrator_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
)

// toolGateSettings は、issue ごとの設定ファイルの `PreToolUse` に載った hook を読み出す形である
// （設計 3-64）。**`command` と `prompt` の両方を1つの形で受ける。**片方だけの形にすると、
// 「turn の終わりを知る hook が消えていた」という壊れ方をテストが素通りさせる。
type toolGateSettings struct {
	Hooks map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Type            string `json:"type"`
			Command         string `json:"command"`
			Prompt          string `json:"prompt"`
			Model           string `json:"model"`
			ContinueOnBlock bool   `json:"continueOnBlock"`
			Async           bool   `json:"async"`
		} `json:"hooks"`
	} `json:"hooks"`
}

// writeSettingsForToolGate は、tool_gate の設定とリポジトリの公開・非公開を与えて着手させ、
// 書かれた settings.json を読んで返す。
//
// t: 呼び出し元のテスト。
// gate: `claude.tool_gate` に入れる設定。
// repoIsPrivate: issue のリポジトリが非公開かどうか。nil は「取れなかった」である。
// 戻り値の1つ目: 読み出した設定ファイルの中身。
// 戻り値の2つ目: 設定ファイルの原文（キーが「書かれていない」ことを見るのに使う）。
func writeSettingsForToolGate(t *testing.T, gate config.ClaudeToolGateConfig, repoIsPrivate *bool) (toolGateSettings, []byte) {
	t.Helper()

	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Claude.ToolGate = gate
	}})
	issue := sampleIssue(188, "Ready")
	issue.RepoIsPrivate = repoIsPrivate
	fx.Tracker.AddIssue(issue)

	settingsPath := filepath.Join(fx.RuntimeDir, "issues", "octocat-hello-world-188", "settings.json")
	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "issue ごとの設定ファイルが書かれる", func() bool {
		_, err := os.Stat(settingsPath)
		return err == nil
	})

	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("設定ファイルを読めません: %v", err)
	}
	var parsed toolGateSettings
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("設定ファイルを JSON として解析できません: %v\n%s", err, raw)
	}
	return parsed, raw
}

// countPromptHooks は `PreToolUse` に載った `type: "prompt"` の hook の数を数える。
//
// s: 読み出した設定ファイルの中身。
// 戻り値: 判定を頼む hook の数。
func countPromptHooks(s toolGateSettings) int {
	n := 0
	for _, m := range s.Hooks["PreToolUse"] {
		for _, h := range m.Hooks {
			if h.Type == "prompt" {
				n++
			}
		}
	}
	return n
}

// 目的: 公開リポジトリの issue に着手したとき、危ない道具の呼び出しを判定させる hook が
// `PreToolUse` に載ること、そして **turn の終わりを知るための `command` の hook が
// 消えていないこと**を確かめる（設計 3-64）。
//
// **`continueOnBlock` が真であることを必ず見る。**偽だと、判定が断った時点で turn が
// そこで終わり、無人運用が壊れる。
//
// 与える情報: `mode: public_only` の設定と、公開リポジトリ（`RepoIsPrivate` が false）の issue。
// 成功条件: `prompt` の hook が1件あり、matcher が `Bash`、`model` が `haiku`、
// `continueOnBlock` が真、指示文に `$ARGUMENTS` が入っていること。`command` の hook も残ること。
func TestToolGate_公開リポジトリのissueには判定のhookを足す(t *testing.T) {
	public := false
	got, _ := writeSettingsForToolGate(t, config.ClaudeToolGateConfig{
		Mode:  config.ClaudeToolGateModePublicOnly,
		Model: "haiku",
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
	if h.Model != "haiku" {
		t.Fatalf("判定させるモデルが違います: got %q, want %q", h.Model, "haiku")
	}
	if !strings.Contains(h.Prompt, "$ARGUMENTS") {
		t.Fatalf("指示文に $ARGUMENTS がありません（判定する呼び出しが渡らない）: %q", h.Prompt)
	}
	if h.Command != "" {
		t.Fatalf("prompt の hook に command が書かれています: %q", h.Command)
	}
}

// 目的: `public_only` の判定が、リポジトリの公開・非公開でどう分かれるかを固定する（設計 3-64）。
//
// **取れなかったとき（nil）は掛ける側へ倒す。**分からないものを「公開ではない」と決めない。
//
// 与える情報: `mode: public_only` と、公開・非公開・取れなかった、の3通りの issue。
// 成功条件: 非公開のときだけ判定の hook が載らないこと。
func TestToolGate_公開かどうかで判定を掛けるかが決まる(t *testing.T) {
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
				Model: "haiku",
				Tools: []string{"Bash"},
			}, c.repoIsPrivate)
			n := countPromptHooks(got)
			if c.wantGate && n != 1 {
				t.Fatalf("判定の hook が載っていません: %d 件", n)
			}
			if !c.wantGate && n != 0 {
				t.Fatalf("判定の hook が載ってしまっています: %d 件", n)
			}
		})
	}
}

// 目的: `mode` が `off` なら判定の hook を1件も足さないこと、`on` なら非公開リポジトリの
// issue にも足すことを確かめる（設計 3-64）。
//
// 与える情報: `off` と `on` の設定と、非公開リポジトリの issue。
// 成功条件: `off` では0件、`on` では1件。
func TestToolGate_modeで掛ける範囲が変わる(t *testing.T) {
	private := true
	cases := []struct {
		name     string
		mode     string
		wantGate int
	}{
		{name: "offなら掛けない", mode: config.ClaudeToolGateModeOff, wantGate: 0},
		{name: "onなら非公開リポジトリでも掛ける", mode: config.ClaudeToolGateModeOn, wantGate: 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := writeSettingsForToolGate(t, config.ClaudeToolGateConfig{
				Mode:  c.mode,
				Model: "haiku",
				Tools: []string{"Bash"},
			}, &private)
			if n := countPromptHooks(got); n != c.wantGate {
				t.Fatalf("判定の hook の数が違います: got %d, want %d", n, c.wantGate)
			}
		})
	}
}

// 目的: `tools` を空にしたら全部の道具に掛かること、複数書いたら縦棒でつないだ matcher に
// なることを確かめる（設計 3-64）。
//
// 与える情報: `tools` が空の設定と、2つ書いた設定。
// 成功条件: 空なら matcher が `*`、2つなら `Bash|Write`。
func TestToolGate_判定に回す道具のmatcherを組み立てる(t *testing.T) {
	public := false
	cases := []struct {
		name  string
		tools []string
		want  string
	}{
		{name: "空なら全部の道具に掛ける", tools: nil, want: "*"},
		{name: "複数なら縦棒でつなぐ", tools: []string{"Bash", "Write"}, want: "Bash|Write"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := writeSettingsForToolGate(t, config.ClaudeToolGateConfig{
				Mode:  config.ClaudeToolGateModeOn,
				Model: "haiku",
				Tools: c.tools,
			}, &public)
			entries := got.Hooks["PreToolUse"]
			if len(entries) != 2 {
				t.Fatalf("PreToolUse の塊が2つではありません: %+v", entries)
			}
			if entries[1].Matcher != c.want {
				t.Fatalf("matcher が違います: got %q, want %q", entries[1].Matcher, c.want)
			}
		})
	}
}

// 目的: `model` を空にしたら settings.json に `model` を書かないことを確かめる（設計 3-64）。
//
// **空文字を書いてはならない。**Claude Code は空の名前のモデルを引けない。
// 空のときは書かず、既定の速いモデルに任せる。
//
// 与える情報: `model` が空の設定。
// 成功条件: `PreToolUse` の判定の hook の JSON に `"model"` の文字列が現れないこと。
func TestToolGate_モデルを書かなければ設定にもmodelを出さない(t *testing.T) {
	public := false
	got, raw := writeSettingsForToolGate(t, config.ClaudeToolGateConfig{
		Mode:  config.ClaudeToolGateModeOn,
		Model: "",
		Tools: []string{"Bash"},
	}, &public)

	if n := countPromptHooks(got); n != 1 {
		t.Fatalf("判定の hook が1件ではありません: %d 件", n)
	}
	if strings.Contains(string(raw), `"model"`) {
		t.Fatalf("model を書いていないのに設定ファイルへ出ています: %s", raw)
	}
}
