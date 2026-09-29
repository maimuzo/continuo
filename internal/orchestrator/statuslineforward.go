package orchestrator

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/maimuzo/continuo/internal/statuslineclient"
)

// claudeConfigDirEnv は Claude Code の利用者の設定ディレクトリを変える環境変数である（設計 3-84a）。
const claudeConfigDirEnv = "CLAUDE_CONFIG_DIR"

// statuslineForwardSources は、転送先を探す設定ファイルを Claude Code と同じ優先順位で並べる（設計 3-84a）。
//
// **continuo 自身の `--settings` と managed settings は並べない。**
//
// worktree: issue の worktree の絶対パス。空なら worktree の2つを並べない。
// configDir: 利用者の設定ディレクトリ。空なら利用者の設定ファイルを並べない。
// 戻り値: 上位から順に並べたパス。
func statuslineForwardSources(worktree, configDir string) []string {
	var paths []string
	if worktree != "" {
		paths = append(paths,
			filepath.Join(worktree, ".claude", "settings.local.json"),
			filepath.Join(worktree, ".claude", "settings.json"))
	}
	if configDir != "" {
		paths = append(paths, filepath.Join(configDir, "settings.json"))
	}
	return paths
}

// userClaudeConfigDir は利用者の設定ディレクトリを返す（設計 3-84a）。
//
// **continuo のプロセスの環境変数 `CLAUDE_CONFIG_DIR` を採る。**`claude.env` からは採らない
// （`--settings` の `env` が効くのは設定ファイルを読んだあとである）。無ければ `~/.claude`。
//
// 戻り値: 設定ディレクトリ。ホームディレクトリが分からなければ空。
func userClaudeConfigDir() string {
	if dir := os.Getenv(claudeConfigDirEnv); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// resolveStatuslineForward は、利用者の `statusLine` のコマンド（転送先）を決める（設計 3-84a）。
//
// **最初に `statusLine` のキーを持つファイルだけで決める。**その値の `type` が `"command"` で
// `command` が空でなければそれを返し、そうでなければ下位のファイルへは進まずに空を返す
// （上位のファイルがキーを持てば、Claude Code は下位のファイルの値を使わない）。
// ファイルが無い・読めない・JSON として読めないものは飛ばす。**着手は止めない。**
//
// paths: 上位から順に並べた設定ファイルのパス（statuslineForwardSources）。
// skipped: 読めない・JSON として読めないファイルを知らせる先（nil なら知らせない）。ファイルが無いだけなら呼ばない。
// 戻り値: 転送先のコマンド。見つからなければ空。
func resolveStatuslineForward(paths []string, skipped func(path string, err error)) string {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) && skipped != nil {
				skipped(path, err)
			}
			continue
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(data, &doc); err != nil {
			if skipped != nil {
				skipped(path, err)
			}
			continue
		}
		raw, ok := doc["statusLine"]
		if !ok {
			continue
		}
		var sl struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		}
		if err := json.Unmarshal(raw, &sl); err != nil {
			return ""
		}
		if sl.Type != "command" || strings.TrimSpace(sl.Command) == "" {
			return ""
		}
		return sl.Command
	}
	return ""
}

// statuslineForwardCommand は、この worktree の run で転送する利用者の `statusLine` のコマンドを返す
// （設計 3-84a）。読めなかったファイルは DEBUG に1行ずつ出す。
//
// worktree: issue の worktree の絶対パス。
// 戻り値: 転送先のコマンド。見つからなければ空。
func (o *Orchestrator) statuslineForwardCommand(worktree string) string {
	return resolveStatuslineForward(statuslineForwardSources(worktree, userClaudeConfigDir()),
		func(path string, err error) {
			o.logger.Debug("ステータスラインの転送先を探す設定ファイルを読めないので飛ばします",
				"path", path, "error", err)
		})
}

// issueSettingsEnv は issue ごとの設定ファイルの `env` を組み立てる（設計 3-84a）。
//
// **`claude.env` の map をそのまま書き換えない。**写した新しい map に書く（着手は並行に走るので、
// ある issue の転送先が別の issue の設定ファイルへ漏れる）。
// **statusLine を書かない着手（`rate_limit.source: none` など）では、転送先も書かない。**
// 書くときは、見つからなくても空文字で書く（pane が受け継いだ同じ名前の変数を拾わないため）。
//
// statusLine: この設定ファイルへ書く statusLine（nil なら書かない）。
// worktree: issue の worktree の絶対パス。
// 戻り値: 設定ファイルの `env`。
func (o *Orchestrator) issueSettingsEnv(statusLine *statusLineSetting, worktree string) map[string]string {
	if statusLine == nil {
		return o.cfg.Claude.Env
	}
	env := make(map[string]string, len(o.cfg.Claude.Env)+1)
	for k, v := range o.cfg.Claude.Env {
		env[k] = v
	}
	env[statuslineclient.EnvForwardCommand] = o.statuslineForwardCommand(worktree)
	return env
}
