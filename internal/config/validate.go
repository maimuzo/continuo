package config

import (
	"fmt"
	"path/filepath"
)

// validate は front matter をパースした直後の Config に対して、YAML としては正しいが
// 値として不正なものが無いかを検査する。ここでの不正は「起動を止める」対象である
// （設計「その2」。CLAUDE.md にも明示されている絶対条件）。
//
// unmarshal 自体（未知のキー・型の不一致）は goccy/go-yaml の yaml.Strict() が検出し、
// 行・桁・ソース抜粋つきのエラーを返す。この関数はその後、goccy では検出できない
// 「値として妥当かどうか」（enum に収まっているか・必須値が空でないか）だけを見る。
//
// cfg: unmarshal 済みの Config（5-4 の展開はまだ行っていない状態でよい）。
// 戻り値: 最初に見つかった不正な値についてのエラー。複数箇所が不正でも1つずつ直せるよう、
// エラーメッセージには必ず設定キーの名前と、実際に入っていた値を含める。
func validate(cfg *Config) error {
	if cfg.Tracker.Kind != "github_projects_v2" {
		return invalidValueError("tracker.kind", cfg.Tracker.Kind, `"github_projects_v2" のみサポートする`)
	}
	if cfg.Tracker.Provider.Owner == "" {
		return requiredValueError("tracker.provider.owner")
	}
	if cfg.Tracker.Provider.ProjectNumber <= 0 {
		return invalidValueError("tracker.provider.project_number", cfg.Tracker.Provider.ProjectNumber, "0より大きい整数にすること")
	}
	if cfg.Tracker.Provider.StatusField == "" {
		return requiredValueError("tracker.provider.status_field")
	}
	switch cfg.Tracker.Provider.TokenSource {
	case "gh_auth":
		// gh のログイン情報を使う。token_env は参照しない。
	case "env":
		if cfg.Tracker.Provider.TokenEnv == "" {
			return requiredValueError("tracker.provider.token_env（token_source が env のとき必須）")
		}
	default:
		return invalidValueError("tracker.provider.token_source", cfg.Tracker.Provider.TokenSource, `"gh_auth" か "env" のどちらか`)
	}
	// **GitHub の connection は first の上限が 100 である。**101 を要求すると
	// EXCESSIVE_PAGINATION のエラーになる（2026-08-18 に実測）。ここで弾かないと、
	// `max: 200` と書いた運用者は起動時ではなく毎回のコメント取得で失敗する（3-6）。
	if cfg.Tracker.Provider.Comments.Max < 0 || cfg.Tracker.Provider.Comments.Max > 100 {
		return invalidValueError(
			"tracker.provider.comments.max",
			cfg.Tracker.Provider.Comments.Max,
			"0以上100以下にすること（0 なら既定の50件。GitHub の connection は一度に100件までしか返さない）",
		)
	}
	// order は「新しい方から max 件を取り、古い順に並べて渡す」以外の実装を持たない。
	// 想定外の値を黙って無視すると、書いたつもりの設定が効いていないことに気づけない。
	switch cfg.Tracker.Provider.Comments.Order {
	case "", "oldest_first":
	default:
		return invalidValueError(
			"tracker.provider.comments.order",
			cfg.Tracker.Provider.Comments.Order,
			`"oldest_first" のみサポートする`,
		)
	}
	if len(cfg.Tracker.ActiveStates) == 0 {
		return requiredValueError("tracker.active_states")
	}
	if len(cfg.Tracker.TerminalStates) == 0 {
		return requiredValueError("tracker.terminal_states")
	}
	if cfg.Tracker.DispatchState == "" {
		return requiredValueError("tracker.dispatch_state")
	}
	if cfg.Tracker.FailureState == "" {
		return requiredValueError("tracker.failure_state")
	}
	if !containsString(cfg.Tracker.ActiveStates, cfg.Tracker.DispatchState) {
		return invalidValueError("tracker.dispatch_state", cfg.Tracker.DispatchState, "tracker.active_states に含まれる値にすること")
	}
	if cfg.Tracker.StatusSignalPrefix == "" {
		return requiredValueError("tracker.status_signal_prefix")
	}
	// status_signal_map の値は「動かす先の Status 名」である。null は「Status を動かさない」
	// という意味を持つので許すが、空文字の Status 名は存在しないので誤りとして止める。
	// 名前がボードに実在するかどうかは、起動時に Status の選択肢名と照合して検査する（3-6）。
	for signal, state := range cfg.Tracker.StatusSignalMap {
		if signal == "" {
			return requiredValueError("tracker.status_signal_map のキー（表明の値）")
		}
		if state != nil && *state == "" {
			return invalidValueError(
				fmt.Sprintf("tracker.status_signal_map.%s", signal),
				`""`,
				"Status 名を書くか、Status を動かさないなら null にすること",
			)
		}
	}

	if cfg.Polling.IntervalMs <= 0 {
		return invalidValueError("polling.interval_ms", cfg.Polling.IntervalMs, "0より大きい整数にすること")
	}

	if cfg.Workspace.Root == "" {
		return requiredValueError("workspace.root")
	}
	if cfg.Workspace.Layout != "gwq" {
		return invalidValueError("workspace.layout", cfg.Workspace.Layout, `"gwq" のみサポートする`)
	}
	if cfg.Workspace.IdentityFile == "" {
		return requiredValueError("workspace.identity_file")
	}

	if cfg.Agent.MaxConcurrentAgents <= 0 {
		return invalidValueError("agent.max_concurrent_agents", cfg.Agent.MaxConcurrentAgents, "0より大きい整数にすること")
	}
	if cfg.Agent.MaxTurns <= 0 {
		return invalidValueError("agent.max_turns", cfg.Agent.MaxTurns, "0より大きい整数にすること")
	}
	if cfg.Agent.MaxTakeover <= 0 {
		return invalidValueError("agent.max_takeover", cfg.Agent.MaxTakeover, "0より大きい整数にすること")
	}
	if cfg.Agent.MaxRetries < 0 {
		return invalidValueError("agent.max_retries", cfg.Agent.MaxRetries, "0以上の整数にすること（0 ならリトライしない）")
	}

	if cfg.Claude.PermissionMode != "dontAsk" {
		return invalidValueError("claude.permission_mode", cfg.Claude.PermissionMode, `無人運用で入力を待たない唯一のモードである "dontAsk" のみサポートする（設計 3-11）`)
	}
	// worktree_local（worktree の中に .claude/settings.local.json を置く経路）は受理しない。
	// 設計 3-12 が --settings の経路に決めており、worktree_local を選んだときの仕様
	// （置き場所・.git/info/exclude への登録・片付け）がどこにも無いためである。
	// 起動は通るのに実装が無い経路へ入るのを防ぐ。
	if cfg.Claude.HookBridge.Mode != "settings_flag" {
		return invalidValueError("claude.hook_bridge.mode", cfg.Claude.HookBridge.Mode, `"settings_flag" のみサポートする（設計 3-12）`)
	}

	if cfg.Herdr.Socket == "" {
		return requiredValueError("herdr.socket")
	}
	if cfg.Herdr.Protocol <= 0 {
		return invalidValueError("herdr.protocol", cfg.Herdr.Protocol, "0より大きい整数にすること")
	}
	if cfg.Herdr.Worktree.BranchTemplate == "" {
		return requiredValueError("herdr.worktree.branch_template")
	}

	if cfg.Cleanup.Enabled && len(cfg.Cleanup.OnStates) == 0 {
		return requiredValueError("cleanup.on_states（cleanup.enabled が true のとき必須）")
	}

	if cfg.RateLimit.Source != "oauth_usage_api" {
		return invalidValueError("rate_limit.source", cfg.RateLimit.Source, `"oauth_usage_api" のみサポートする`)
	}
	switch cfg.RateLimit.TokenSource {
	case "claude_credentials", "env":
	default:
		return invalidValueError("rate_limit.token_source", cfg.RateLimit.TokenSource, `"claude_credentials" か "env" のどちらか（読み取りだけで書き換えない。3-27）`)
	}
	if cfg.RateLimit.PauseAbovePercent < 0 || cfg.RateLimit.PauseAbovePercent > 100 {
		return invalidValueError("rate_limit.pause_above_percent", cfg.RateLimit.PauseAbovePercent, "0以上100以下にすること")
	}

	switch cfg.Trust.OnUntrusted {
	case "skip_and_comment":
	default:
		return invalidValueError("trust.on_untrusted", cfg.Trust.OnUntrusted, `"skip_and_comment" のみサポートする（設計 4-3）`)
	}

	switch cfg.Restart.OrphanRunningAction {
	case "redispatch", "to_dispatch_state", "to_failure_state":
	default:
		return invalidValueError("restart.orphan_running_action", cfg.Restart.OrphanRunningAction, `"redispatch" / "to_dispatch_state" / "to_failure_state" のいずれか`)
	}

	if cfg.Server.Port != nil && (*cfg.Server.Port < 0 || *cfg.Server.Port > 65535) {
		return invalidValueError("server.port", *cfg.Server.Port, "0以上65535以下にすること")
	}

	return nil
}

// validateExpanded は 5-4 の展開（環境変数・チルダ）を通したあとの Config を検査する。
//
// 展開の前に検査すると "~/run/continuo.lock" のような値を「絶対パスでない」と誤って
// 弾いてしまうため、絶対パスの検査だけはここに分けてある。
//
// 検査するもの:
//   - claude.hook_bridge.listen が非 null なら絶対パスであること。相対パスだと
//     continuo をどのディレクトリから起動したかで socket の場所が変わり、身元ファイルに
//     書いたパスとの一致検査（3-23 / 3-18）が成立しない
//   - runtime.lock_file が非 null なら絶対パスであること。相対パスだと二重起動の判定が
//     起動ディレクトリごとに別ファイルになり、flock による排他が成立しない（3-17）
//
// cfg: expandConfig を通したあとの Config。
// 戻り値: 最初に見つかった不正な値についてのエラー。エラーメッセージには設定キーの名前と
// 実際に入っていた値（展開後の文字列）を含める（5-4）。
func validateExpanded(cfg *Config) error {
	if cfg.Claude.HookBridge.Listen != nil && *cfg.Claude.HookBridge.Listen != "" {
		if !filepath.IsAbs(*cfg.Claude.HookBridge.Listen) {
			return invalidValueError(
				keyClaudeHookListen,
				*cfg.Claude.HookBridge.Listen,
				"絶対パスにすること（相対パスだと continuo を起動したディレクトリによって socket の場所が変わる。null にすれば 3-23 の探索順で決まる）",
			)
		}
	}
	if cfg.Runtime.LockFile != nil && *cfg.Runtime.LockFile != "" {
		if !filepath.IsAbs(*cfg.Runtime.LockFile) {
			return invalidValueError(
				keyRuntimeLockFile,
				*cfg.Runtime.LockFile,
				"絶対パスにすること（相対パスだと起動したディレクトリごとに別のロックファイルになり、二重起動を防げない）",
			)
		}
	}
	return nil
}

// containsString は ss の中に target と完全一致する要素があるかどうかを返す。
func containsString(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}

// invalidValueError は「値は入っているが不正である」ことを表すエラーを作る。
// key: 設定キーの名前（ドット区切り）。
// value: 実際に入っていた値（そのまま %v で埋め込む＝元の文字列を含める）。
// requirement: 期待する値の説明。
func invalidValueError(key string, value any, requirement string) error {
	return fmt.Errorf("設定キー %s の値 %v が不正です: %s", key, value, requirement)
}

// requiredValueError は「値が空・未設定である」ことを表すエラーを作る。
// key: 設定キーの名前（ドット区切り）。
func requiredValueError(key string) error {
	return fmt.Errorf("設定キー %s は必須ですが空です", key)
}
