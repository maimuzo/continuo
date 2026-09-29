// 利用者のステータスラインの転送先を、着手のときに設定ファイルから決めて issue ごとの設定ファイルの
// env へ書くことを確かめる（設計 3-84a。issue #295）。
package orchestrator_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// forwardEnvName は転送先を運ぶ環境変数の名前である。
const forwardEnvName = "CONTINUO_STATUSLINE_COMMAND"

// writeJSONFile は dir/name へ中身を書く（ディレクトリも掘る）。
func writeJSONFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("ディレクトリを作成できません: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("%s を書けません: %v", name, err)
	}
}

// 目的: 転送先を Claude Code と同じ優先順位（worktree の settings.local.json → worktree の
// settings.json → 利用者の settings.json）で決め、最初に statusLine のキーを持つファイルだけで決めることを
// 確かめる。キーはあるが `type` が command でない・command が空・形が違うときは、下位へ進まず転送しないこと。
// 無い・壊れたファイルは飛ばし、壊れたものだけを知らせること。
// 与える情報: worktree と利用者の設定ディレクトリに、場合ごとの設定ファイルを置く。
// 成功条件: 表のとおりの転送先と、飛ばしたファイルの一覧が返ること。
func TestResolveStatuslineForward_優先順位と条件(t *testing.T) {
	const userCmd = "~/.claude/user-statusline.sh"
	const user = `{"statusLine":{"type":"command","command":"` + userCmd + `"}}`
	cases := []struct {
		name        string
		local       string
		project     string
		user        string
		want        string
		wantSkipped []string
	}{
		{name: "どこにも無い", want: ""},
		{name: "利用者の設定だけ", user: user, want: userCmd},
		{name: "localが最優先", local: `{"statusLine":{"type":"command","command":"local.sh"}}`,
			project: `{"statusLine":{"type":"command","command":"project.sh"}}`, user: user, want: "local.sh"},
		{name: "projectが利用者より先", project: `{"statusLine":{"type":"command","command":"project.sh"}}`,
			user: user, want: "project.sh"},
		{name: "キーの無いファイルは飛ばす", local: `{"model":"haiku"}`, project: `{}`, user: user, want: userCmd},
		{name: "typeがcommandでなければ下位へ進まない", project: `{"statusLine":{"type":"static","command":"x.sh"}}`,
			user: user, want: ""},
		{name: "commandが空なら下位へ進まない", local: `{"statusLine":{"type":"command","command":"  "}}`,
			user: user, want: ""},
		{name: "statusLineの形が違えば下位へ進まない", local: `{"statusLine":"x.sh"}`, user: user, want: ""},
		{name: "壊れたJSONは飛ばす", local: `{壊れた`, user: user, want: userCmd, wantSkipped: []string{"local"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			worktree := filepath.Join(root, "wt")
			configDir := filepath.Join(root, "config")
			if err := os.MkdirAll(worktree, 0o700); err != nil {
				t.Fatalf("worktree を作成できません: %v", err)
			}
			if tc.local != "" {
				writeJSONFile(t, filepath.Join(worktree, ".claude"), "settings.local.json", tc.local)
			}
			if tc.project != "" {
				writeJSONFile(t, filepath.Join(worktree, ".claude"), "settings.json", tc.project)
			}
			if tc.user != "" {
				writeJSONFile(t, configDir, "settings.json", tc.user)
			}

			got, skipped := orchestrator.ResolveStatuslineForwardForTest(worktree, configDir)

			if got != tc.want {
				t.Errorf("転送先 = %q, want %q", got, tc.want)
			}
			var want []string
			for _, s := range tc.wantSkipped {
				if s == "local" {
					want = append(want, filepath.Join(worktree, ".claude", "settings.local.json"))
				}
			}
			if !slices.Equal(skipped, want) {
				t.Errorf("飛ばしたファイル = %v, want %v", skipped, want)
			}
		})
	}
}

// startAndReadIssueSettings は、同じ issue を着手させて issue ごとの設定ファイルを読む。
func startAndReadIssueSettings(t *testing.T, fx *fixture) map[string]json.RawMessage {
	t.Helper()
	holdPrompt(fx)
	if fx.Config.RateLimit.Source != ratelimit.SourceNone {
		// **値を新しくしてから着手させる**（入札させ、statusline取得を開かせない）。
		feedFreshQuota(fx.Orc, "pane-a", time.Now(), 10, 20)
	}
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
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
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("設定ファイルを読めません: %v\n%s", err, raw)
	}
	return m
}

// envOf は設定ファイルの env を読む。
func envOf(t *testing.T, m map[string]json.RawMessage) map[string]string {
	t.Helper()
	var env map[string]string
	if raw, ok := m["env"]; ok {
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("env を読めません: %v（%s）", err, raw)
		}
	}
	return env
}

// 目的: statusLine を書く着手で、利用者の設定ディレクトリ（CLAUDE_CONFIG_DIR）の statusLine の
// コマンドを env の CONTINUO_STATUSLINE_COMMAND へ書くこと、claude.env の値も残すこと、
// claude.env の map そのものは書き換えないこと（写した新しい map に書く）を確かめる。
// 与える情報: CLAUDE_CONFIG_DIR の settings.json に statusLine、claude.env に架空のプロキシと同じ名前の変数。
// 成功条件: env の CONTINUO_STATUSLINE_COMMAND が利用者のコマンドで上書きされ、プロキシも残ること。
// 設定に渡した claude.env の map が、着手のあとも元のままであること。
func TestSettings_利用者のstatusLineのコマンドをenvへ書きclaude_envのmapは書き換えない(t *testing.T) {
	var claudeEnv map[string]string
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.RateLimit.Source = ratelimit.SourceStatusline
		cfg.Claude.Env = map[string]string{
			"HTTPS_PROXY":  "http://proxy.example.com:8080",
			forwardEnvName: "from-claude-env",
		}
		claudeEnv = cfg.Claude.Env
	}})
	writeJSONFile(t, os.Getenv("CLAUDE_CONFIG_DIR"), "settings.json",
		`{"statusLine":{"type":"command","command":"~/.claude/user-statusline.sh","refreshInterval":5}}`)

	env := envOf(t, startAndReadIssueSettings(t, fx))

	if got := env[forwardEnvName]; got != "~/.claude/user-statusline.sh" {
		t.Errorf("env の %s = %q, want 利用者のコマンド", forwardEnvName, got)
	}
	if env["HTTPS_PROXY"] != "http://proxy.example.com:8080" {
		t.Errorf("claude.env の値が消えた: %v", env)
	}
	if len(claudeEnv) != 2 || claudeEnv[forwardEnvName] != "from-claude-env" {
		t.Errorf("claude.env の map を書き換えた: %v", claudeEnv)
	}
}

// 目的: worktree の .claude/settings.local.json の statusLine が、利用者の設定より優先されることを
// 着手の経路で確かめる（worktree のパスが writeSettingsFile まで渡っていること）。
// 与える情報: after_create で worktree に settings.local.json を書き、CLAUDE_CONFIG_DIR にも別の statusLine を置く。
// 成功条件: env の CONTINUO_STATUSLINE_COMMAND が worktree のほうのコマンドであること。
func TestSettings_worktreeのsettings_local_jsonが利用者の設定より優先される(t *testing.T) {
	hook := `mkdir -p .claude && printf '%s' '{"statusLine":{"type":"command","command":"./local-statusline.sh"}}' > .claude/settings.local.json`
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.RateLimit.Source = ratelimit.SourceStatusline
		cfg.WorkspaceHooks.AfterCreate = &hook
	}})
	writeJSONFile(t, os.Getenv("CLAUDE_CONFIG_DIR"), "settings.json",
		`{"statusLine":{"type":"command","command":"~/.claude/user-statusline.sh"}}`)

	env := envOf(t, startAndReadIssueSettings(t, fx))

	if got := env[forwardEnvName]; got != "./local-statusline.sh" {
		t.Errorf("env の %s = %q, want worktree の settings.local.json のコマンド", forwardEnvName, got)
	}
}

// 目的: 転送先が見つからないときも、statusLine を書く着手では空文字で書くことを確かめる
// （pane が受け継いだ同じ名前の変数を拾わないため）。
// 与える情報: statusLine の無い利用者の設定。
// 成功条件: env に CONTINUO_STATUSLINE_COMMAND があり、値が空であること。
func TestSettings_転送先が無くても空文字で書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.RateLimit.Source = ratelimit.SourceStatusline
	}})
	writeJSONFile(t, os.Getenv("CLAUDE_CONFIG_DIR"), "settings.json", `{"model":"haiku"}`)

	env := envOf(t, startAndReadIssueSettings(t, fx))

	if got, ok := env[forwardEnvName]; !ok || got != "" {
		t.Errorf("env の %s = %q（ある: %v）, want 空文字で書く", forwardEnvName, got, ok)
	}
}

// 目的: statusLine を書かない着手（`rate_limit.source: none`）では、env に CONTINUO_STATUSLINE_COMMAND を
// 書かないことを確かめる（利用者のステータスラインがそのまま出るので、転送は要らない）。
// 与える情報: source が none の設定と、statusLine を持つ利用者の設定。
// 成功条件: 設定ファイルに statusLine が無く、env にも CONTINUO_STATUSLINE_COMMAND が無いこと。
func TestSettings_statusLineを書かない着手では転送先も書かない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	writeJSONFile(t, os.Getenv("CLAUDE_CONFIG_DIR"), "settings.json",
		`{"statusLine":{"type":"command","command":"~/.claude/user-statusline.sh"}}`)

	m := startAndReadIssueSettings(t, fx)

	if _, ok := m["statusLine"]; ok {
		t.Fatalf("source が none なのに statusLine を書いた: %s", m["statusLine"])
	}
	if got, ok := envOf(t, m)[forwardEnvName]; ok {
		t.Errorf("statusLine を書かないのに %s を書いた: %q", forwardEnvName, got)
	}
}
