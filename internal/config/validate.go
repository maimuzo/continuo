package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/i18n"
)

// issueNumberPlaceholder は herdr.worktree.branch_template が必ず含んでいなければならない
// 変数の参照である（3-37-9d）。
//
// **`{{` と `}}` を含めず、変数の参照だけを見る。**text/template は
// `{{ .issue.number }}` のように空白を挟んだ書き方も受け付けるので、
// 波括弧ごと照合すると、正しい設定を弾いてしまう。
const issueNumberPlaceholder = ".issue.number"

// validate は front matter をパースした直後の Config に対して、YAML としては正しいが
// 値として不正なものが無いかを検査する。ここでの不正は「起動を止める」対象である
// （設計「その2」。CLAUDE.md にも明示されている絶対条件）。
//
// unmarshal 自体（未知のキー・型の不一致）は goccy/go-yaml の yaml.Strict() が検出し、
// 行・桁・ソース抜粋つきのエラーを返す。この関数はその後、goccy では検出できない
// 「値として妥当かどうか」（enum に収まっているか・必須値が空でないか）だけを見る。
//
// cfg: unmarshal 済みの Config（5-5 の展開はまだ行っていない状態でよい）。
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
	if err := validateHandoff(cfg.Tracker.Provider.Handoff); err != nil {
		return err
	}
	if len(cfg.Tracker.ActiveStates) == 0 {
		return requiredValueError("tracker.active_states")
	}
	if len(cfg.Tracker.TerminalStates) == 0 {
		return requiredValueError("tracker.terminal_states")
	}
	if cfg.Tracker.RunningState == "" {
		return requiredValueError("tracker.running_state")
	}
	if cfg.Tracker.DispatchState == "" {
		return requiredValueError("tracker.dispatch_state")
	}
	if cfg.Tracker.FailureState == "" {
		return requiredValueError("tracker.failure_state")
	}
	// dispatch したときに書く先が active_states に無いと、書いた直後に自分の worker を
	// 候補から外してしまう（設計 3-10）。
	if !containsStateFold(cfg.Tracker.ActiveStates, cfg.Tracker.RunningState) {
		return invalidValueError("tracker.running_state", cfg.Tracker.RunningState, "tracker.active_states に含まれる値にすること")
	}
	if !containsStateFold(cfg.Tracker.ActiveStates, cfg.Tracker.DispatchState) {
		return invalidValueError("tracker.dispatch_state", cfg.Tracker.DispatchState, "tracker.active_states に含まれる値にすること")
	}
	// 状態の集合どうしが互いに素であることを見る（設計 3-9 / 3-10 / 4-1）。
	// ここを検査しないと、完了として片付けた issue を次の巡回で作業中として拾い直す、
	// 打ち切った issue が永久に再 dispatch される、といった無限の往復が起動を通ってしまう。
	for _, state := range cfg.Tracker.ActiveStates {
		if containsStateFold(cfg.Tracker.TerminalStates, state) {
			return invalidValueError(
				"tracker.active_states", state,
				"tracker.terminal_states と同じ値を含めないこと（完了として片付けた issue を、次の巡回で作業中として拾い直す。3-9 / 3-10）",
			)
		}
	}
	if containsStateFold(cfg.Tracker.ActiveStates, cfg.Tracker.FailureState) {
		return invalidValueError(
			"tracker.failure_state", cfg.Tracker.FailureState,
			"tracker.active_states に含まれない値にすること（打ち切った issue が永久に再 dispatch される。4-1）",
		)
	}
	// running_state が terminal_states に入る場合は、上の active_states と terminal_states の
	// 重なりとして必ず先に落ちる（running_state は active_states に含まれることを既に要求して
	// いるため）。同じことを二重に検査しない。

	// cleanup.on_states は「片付けを始める状態」である。作業中の状態を書くと、
	// 走っている worktree を消してしまう（設計 3-9）。
	for _, state := range cfg.Cleanup.OnStates {
		if containsStateFold(cfg.Tracker.ActiveStates, state) {
			return invalidValueError(
				"cleanup.on_states", state,
				"tracker.active_states と同じ値を含めないこと（作業中の worktree を片付けてしまう。3-9）",
			)
		}
	}

	if cfg.Tracker.StatusSignalPrefix == "" {
		return requiredValueError("tracker.status_signal_prefix")
	}
	// 0 は「起動時だけ照合する」という意味を持つので許す。負の値は意味を持たない。
	if cfg.Tracker.VerifyStatesEvery < 0 {
		return invalidValueError("tracker.verify_states_every", cfg.Tracker.VerifyStatesEvery, "0以上の整数にすること（0 なら起動時だけ照合する）")
	}
	// 0 は「猶予を置かない」という意味を持つので許す。負の値は意味を持たない（3-50）。
	if cfg.Tracker.UnknownStateGraceMs < 0 {
		return invalidValueError("tracker.unknown_state_grace_ms", cfg.Tracker.UnknownStateGraceMs,
			"0以上の整数にすること（0 なら猶予を置かずにその巡回で止める）")
	}
	// automated_state_rewrite は「自動化が書いた Status → 戻す先の Status」の対応表である（3-54）。
	// **空文字は Status 名として存在しない。**そして**キーと値が同じ値だと1バイトも動かない**
	// （同じ値の書き込みは省かれるので、知らない Status のまま巡回のたびに書きに行き続ける）。
	// 名前がカンバンに実在するかどうかは、起動時に Status の選択肢名と照合して検査する（3-6）。
	//
	// **キーを名前順に見る。**map の反復順は決まらないので、そのまま回すと
	// **同じ設定ファイルなのに、実行のたびに違う行のエラーが出る。**
	if err := validateAutomatedStateRewrite(cfg); err != nil {
		return err
	}

	// status_signal_map の値は「動かす先の Status 名」である。null は「Status を動かさない」
	// という意味を持つので許すが、空文字の Status 名は存在しないので誤りとして止める。
	// 名前がカンバンに実在するかどうかは、起動時に Status の選択肢名と照合して検査する（3-6）。
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
	if cfg.Workspace.IdentityFile == "" {
		return requiredValueError("workspace.identity_file")
	}
	// **知らない値を黙って既定に丸めない**（3-49）。`halt` や `abort` と書いた利用者は
	// 「止まる」つもりでいるので、丸めた側が偶然一致しても、次に `continue` と
	// 書いたときには**飛ばすつもりが止まる**。書いた値が効いていないことに気づけない。
	switch cfg.Workspace.OnBrokenWorktree {
	case OnBrokenWorktreeStop, OnBrokenWorktreeSkip:
	default:
		return invalidValueError(
			"workspace.on_broken_worktree", cfg.Workspace.OnBrokenWorktree,
			fmt.Sprintf("%q（壊れた worktree を見つけたら起動を止める）か %q"+
				"（その worktree だけ飛ばして起動を続ける）のどちらかにすること",
				OnBrokenWorktreeStop, OnBrokenWorktreeSkip),
		)
	}

	if cfg.Agent.MaxConcurrentAgents <= 0 {
		return invalidValueError("agent.max_concurrent_agents", cfg.Agent.MaxConcurrentAgents, "0より大きい整数にすること")
	}
	if cfg.Agent.MaxDispatchTurns <= 0 {
		return invalidValueError("agent.max_dispatch_turns", cfg.Agent.MaxDispatchTurns, "0より大きい整数にすること")
	}
	// **状態ごとの上限に 0 以下を書かせない。**`In Progress: 0` と書くと空きスロットの
	// 判定（hasFreeSlot）が常に偽になり、カンバン全体の dispatch が永久に止まる。
	// ログにも何も出ないので、無人運用では止まっていることに誰も気づけない。
	// `SPEC.md` 5.3.5 は "Invalid entries (non-positive or non-numeric) are ignored"
	// （**訳:** 不正な項目（非正の値・数値でない値）は無視する）と定めるが、
	// **黙って無視すると「書いたつもりの設定が効いていない」ことに気づけない。**弾く。
	for state, limit := range cfg.Agent.MaxConcurrentAgentsByState {
		if state == "" {
			return requiredValueError("agent.max_concurrent_agents_by_state のキー（Status 名）")
		}
		if limit <= 0 {
			return invalidValueError(
				fmt.Sprintf("agent.max_concurrent_agents_by_state.%s", state), limit,
				"0より大きい整数にすること（0 を書くとその Status の dispatch が永久に止まり、ログにも何も出ない）")
		}
	}
	if cfg.Agent.MaxTakeover <= 0 {
		return invalidValueError("agent.max_takeover", cfg.Agent.MaxTakeover, "0より大きい整数にすること")
	}
	if cfg.Agent.MaxRetries < 0 {
		return invalidValueError("agent.max_retries", cfg.Agent.MaxRetries, "0以上の整数にすること（0 ならリトライしない）")
	}

	if cfg.Claude.Kind == "" {
		return requiredValueError("claude.kind")
	}
	if cfg.Claude.PermissionMode != "dontAsk" {
		return invalidValueError("claude.permission_mode", cfg.Claude.PermissionMode, `無人運用で入力を待たない唯一のモードである "dontAsk" のみサポートする（設計 3-11）`)
	}

	// 時間を表す値をまとめて検査する。**ここを検査しないと待ちが成立しない。**
	// たとえば claude.poll_wait_ms: 0 は turn の待ち受けの待ち時間を 0 にするため、
	// herdr の socket を無停止で叩き続けるループになる。settle_ms: 0 なら逆に、
	// 正常な turn が最初の確認で必ず stall と判定される。
	// claude.turn_timeout_ms だけは「0 以下で打ち切りを行わない」と決めてある（3-21。
	// `SPEC.md` 8.4 の "If stall_timeout_ms <= 0, skip stall detection entirely"）ので
	// この一覧に入れない。
	for _, item := range []struct {
		key   string
		value int
	}{
		{"claude.poll_wait_ms", cfg.Claude.PollWaitMs},
		{"claude.settle_ms", cfg.Claude.SettleMs},
		{"herdr.read_timeout_ms", cfg.Herdr.ReadTimeoutMs},
		{"herdr.startup_timeout_ms", cfg.Herdr.StartupTimeoutMs},
		{"agent.max_retry_backoff_ms", cfg.Agent.MaxRetryBackoffMs},
		{"rate_limit.poll_interval_ms", cfg.RateLimit.PollIntervalMs},
		{"workspace_hooks.timeout_ms", cfg.WorkspaceHooks.TimeoutMs},
	} {
		if item.value <= 0 {
			return invalidValueError(item.key, item.value, "0より大きい整数（ミリ秒）にすること")
		}
	}
	// 待ちの大小関係も見る。打ち切りまでの時間より1回の待ちが長い、1回の待ちより猶予が長い、
	// といった書き間違いは、起動時に落としたほうが原因が分かる。
	// **turn_timeout_ms が 0 以下のときは比べない**（打ち切りを行わない設定であり、
	// 「上限」として意味を持たない）。
	if cfg.Claude.TurnTimeoutMs > 0 && cfg.Claude.PollWaitMs > cfg.Claude.TurnTimeoutMs {
		return invalidValueError(
			"claude.poll_wait_ms", cfg.Claude.PollWaitMs,
			"claude.turn_timeout_ms 以下にすること"+
				"（1回の待ちが「画面が止まったとみなす時間」より長いと、打ち切りの判定より待ちのほうが粗くなる）",
		)
	}
	if cfg.Claude.SettleMs > cfg.Claude.PollWaitMs {
		return invalidValueError(
			"claude.settle_ms", cfg.Claude.SettleMs,
			"claude.poll_wait_ms 以下にすること（turn の終わりを確かめる猶予が、1回の待ちより長い）",
		)
	}

	// **綴りを検査する。**herdr は wait_until の文字列をそのまま受け取るので、
	// 綴りを間違えても起動は通り、turn の終わりを拾えないまま時間切れまで待つことになる。
	if err := validateWaitUntil(cfg.Claude.WaitUntil); err != nil {
		return err
	}

	// **綴りを検査する。**mode の値は settings.json へそのまま出るのではなく
	// continuo 自身の分岐に使うので、知らない値のまま起動すると
	// 「掛けたつもりの判定が1度も走らない」に化ける（設計 3-64 / 8-1）。
	if err := validateClaudeToolGate(cfg.Claude.ToolGate); err != nil {
		return err
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
	// **issue の番号を必ず含める**（3-37-9d）。番号が入っていないと、issue が違っても
	// 同じ branch 名になる。**そのとき `continuo abandon` は、名指しされた issue とは
	// 別の issue の branch を消す**（worktree が無い経路は、この規則だけを頼りに
	// 消す相手を決める）。
	if !strings.Contains(cfg.Herdr.Worktree.BranchTemplate, issueNumberPlaceholder) {
		return invalidValueError("herdr.worktree.branch_template", cfg.Herdr.Worktree.BranchTemplate,
			i18n.T(i18n.KeyConfigValidateBranchTemplateNeedsIssueNumber, issueNumberPlaceholder))
	}

	if cfg.Cleanup.Enabled && len(cfg.Cleanup.OnStates) == 0 {
		return requiredValueError("cleanup.on_states（cleanup.enabled が true のとき必須）")
	}

	// none を受理する。usage API がトークンを消費するかどうかを判別できていないため、
	// この経路を切って運用できる必要がある（設計 3-27 / 第6節）。
	// none のときは枠の判定を行わず、stall 検知だけに頼る。
	switch cfg.RateLimit.Source {
	case "oauth_usage_api", "none":
	default:
		return invalidValueError("rate_limit.source", cfg.RateLimit.Source, `"oauth_usage_api" か "none" のどちらか（設計 3-27）`)
	}
	switch cfg.RateLimit.TokenSource {
	case RateLimitTokenSourceClaudeCredentials:
		// ~/.claude/.credentials.json を読む。token_env は参照しない。
	case RateLimitTokenSourceKeychain:
		// **macOS でだけ選べる。**Keychain を読む `security` は macOS の標準コマンドであり、
		// ほかの OS には無い。ここで弾かないと、Linux の運用者は起動時ではなく5分ごとの
		// 取得で毎回失敗し、枠の判定が黙って無効化される（5-5 と同じ理由）。
		if runtime.GOOS != "darwin" {
			return invalidValueError("rate_limit.token_source", cfg.RateLimit.TokenSource,
				fmt.Sprintf(`"keychain" は macOS でだけ使える（いまの OS: %s）。"claude_credentials" か "env" にすること`, runtime.GOOS))
		}
	case RateLimitTokenSourceEnv:
		// tracker.provider.token_env と同じ扱いにする。空のまま起動を通すと、
		// 5分ごとの取得が毎回 ErrNoCredentials になり、枠の判定が黙って無効化される（5-5）。
		if cfg.RateLimit.TokenEnv == "" {
			return requiredValueError("rate_limit.token_env（rate_limit.token_source が env のとき必須）")
		}
	default:
		return invalidValueError("rate_limit.token_source", cfg.RateLimit.TokenSource,
			`"claude_credentials" か "keychain"（macOS のみ）か "env" のいずれか（読み取りだけで書き換えない。3-27）`)
	}
	if cfg.RateLimit.PauseAbovePercent < 0 || cfg.RateLimit.PauseAbovePercent > 100 {
		return invalidValueError("rate_limit.pause_above_percent", cfg.RateLimit.PauseAbovePercent, "0以上100以下にすること")
	}
	// **負の値を通してはならない**（issue #197）。負だと「待つ先の時刻 − いま」が必ず上回るので、
	// **枠待ちに入った瞬間に担当を手放す。**1週間の枠を1%でも使い切れば、走っている run が全部止まる。
	//
	// **上限を切りたい人は 0 を書く**（`claude.turn_timeout_ms` と
	// `tracker.provider.handoff.recheck_interval_ms` と同じ向き）。
	//
	// **`tracker.provider.handoff.idle_timeout_ms` との大小は検査しない。**
	// 1台で動かしている人には他の機械がいないので、18時間より長くても正しく効く。
	// **弾くと、その人が起動できなくなる。**案内は雛形のコメントに書いてある。
	if cfg.RateLimit.WeeklyWaitLimitMinutes < 0 {
		return invalidValueError("rate_limit.weekly_wait_limit_minutes",
			cfg.RateLimit.WeeklyWaitLimitMinutes, i18n.T(i18n.KeyConfigValidateRateLimitWeeklyWaitRange))
	}

	switch cfg.Trust.OnUntrusted {
	case "skip_and_comment":
	default:
		return invalidValueError("trust.on_untrusted", cfg.Trust.OnUntrusted, `"skip_and_comment" のみサポートする（設計 4-3）`)
	}
	// **ここに書かれた文字列は `~/.claude.json` へ書き込む鍵の元になる**（3-33）。
	// 形を検査せずに `ghq` や `git` へ渡すと、打ち間違いが「clone が無い」として
	// 静かに握り潰される。**起動時に名指しで落とす。**
	if err := validateTrustRepositories(cfg.Trust.Repositories); err != nil {
		return err
	}

	switch cfg.Restart.OrphanRunningAction {
	case "redispatch", "to_dispatch_state", "to_failure_state":
	default:
		return invalidValueError("restart.orphan_running_action", cfg.Restart.OrphanRunningAction, `"redispatch" / "to_dispatch_state" / "to_failure_state" のいずれか`)
	}

	if cfg.Server.Port != nil && (*cfg.Server.Port < 0 || *cfg.Server.Port > 65535) {
		return invalidValueError("server.port", *cfg.Server.Port, "0以上65535以下にすること")
	}

	// **資源の無い言語を黙って日本語に落とさない**（3-35）。書いたつもりの設定が
	// 効いていないことに、無人運用では誰も気づけない。
	if err := validateLanguage(cfg.Language); err != nil {
		return err
	}

	return nil
}

// validateClaudeToolGate は claude.tool_gate の値を検査する（3-64）。
//
// **見るのは3つである。**mode の綴り、道具の名前が空でないこと、道具の名前が重複して
// いないこと。**道具の名前はそのまま hook の matcher になる**ので、空の要素があると
// `Bash|` のような matcher ができ、何に掛かるのかが読めなくなる。
//
// **model は綴りを検査しない。**受け付ける名前を決めているのは Claude Code であり、
// こちらに一覧が無い。空なら settings.json へ書かず、Claude Code の既定に任せる。
//
// gate: claude.tool_gate に書かれた設定。
// 戻り値: 受け付けられない値があったときのエラー。すべて正しければ nil。
func validateClaudeToolGate(gate ClaudeToolGateConfig) error {
	if !slices.Contains(ClaudeToolGateModes, gate.Mode) {
		labels := make([]string, 0, len(ClaudeToolGateModes))
		for _, m := range ClaudeToolGateModes {
			labels = append(labels, fmt.Sprintf("%q", m))
		}
		return invalidValueError("claude.tool_gate.mode", gate.Mode,
			fmt.Sprintf("%s のいずれかにすること", strings.Join(labels, " / ")))
	}

	seen := make(map[string]struct{}, len(gate.Tools))
	for i, name := range gate.Tools {
		key := fmt.Sprintf("claude.tool_gate.tools[%d]", i)
		if strings.TrimSpace(name) == "" {
			return invalidValueError(key, name,
				"道具の名前を書くこと（空の要素があると、何に掛かるのか読めない matcher になる）")
		}
		if _, dup := seen[name]; dup {
			return invalidValueError(key, name, "同じ道具が2回書かれている（重複した行を消すこと）")
		}
		seen[name] = struct{}{}
	}
	return nil
}

// validateWaitUntil は claude.wait_until の綴りを検査する（3-2）。
//
// **受け付ける値は internal/herdr の AgentStatus の定数だけである。**ここに一覧を
// 書き写さず herdr.AgentStatuses() を引くのは、herdr の enum が増えたときに
// この検査だけが古い一覧のまま残るのを防ぐためである。
//
// **重複も弾く。**同じ状態を2回書いても効果は変わらないので、書き間違いである。
//
// names: claude.wait_until に書かれた値の並び。
// 戻り値: 受け付けられない値・重複があったときのエラー。すべて正しければ nil。
func validateWaitUntil(names []string) error {
	allowed := herdr.AgentStatuses()
	labels := make([]string, 0, len(allowed))
	for _, s := range allowed {
		labels = append(labels, fmt.Sprintf("%q", string(s)))
	}
	requirement := fmt.Sprintf("herdr が受け付ける状態名（%s）のいずれかにすること", strings.Join(labels, " / "))

	seen := make(map[string]struct{}, len(names))
	for i, n := range names {
		key := fmt.Sprintf("claude.wait_until[%d]", i)
		if !containsStatus(allowed, herdr.AgentStatus(n)) {
			return invalidValueError(key, n, requirement)
		}
		if _, dup := seen[n]; dup {
			return invalidValueError(key, n, "同じ状態が2回書かれている（重複した行を消すこと）")
		}
		seen[n] = struct{}{}
	}
	return nil
}

// containsStatus は ss の中に target と完全一致する要素があるかどうかを返す。
//
// **前後の空白を落としたり大文字小文字を無視したりしない。**herdr へはこの文字列が
// そのまま渡るので、検査で通した綴りと herdr が受け取る綴りを一致させる。
func containsStatus(ss []herdr.AgentStatus, target herdr.AgentStatus) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}

// validateLanguage は language の値を検査する（3-35）。
//
// 受け付けるのは、空文字（＝書かれていない）・"auto"・資源のある言語（"ja" / "en"）だけである。
//
// value: language に書かれた値。
// 戻り値: 受け付けられない値だったときのエラー。受け付けられるなら nil。
func validateLanguage(value string) error {
	if value == "" || value == i18n.LangConfigAuto {
		return nil
	}
	if i18n.Supported(i18n.Lang(value)) {
		return nil
	}
	names := make([]string, 0, len(i18n.Available()))
	for _, l := range i18n.Available() {
		names = append(names, fmt.Sprintf("%q", string(l)))
	}
	return invalidValueError("language", value,
		fmt.Sprintf("%q（環境変数 LANG から決める）か、%s のいずれかにすること",
			i18n.LangConfigAuto, strings.Join(names, " / ")))
}

// validateExpanded は 5-5 の展開（環境変数・チルダ）を通したあとの Config を検査する。
//
// 展開の前に検査すると "~/run/hooks.sock" のような値を「絶対パスでない」と誤って
// 弾いてしまうため、絶対パスの検査だけはここに分けてある。
//
// 検査するもの:
//   - workspace.root が絶対パスであること。相対パスは Load が WORKFLOW.md の置き場所を
//     基準に解決済みなので（5-1）、ここに相対パスが残るのは基準が決められなかった場合だけである
//   - claude.hook_bridge.listen が非 null なら絶対パスであること。相対パスだと
//     continuo をどのディレクトリから起動したかで socket の場所が変わり、身元ファイルに
//     書いたパスとの一致検査（3-23 / 3-18）が成立しない
//
// cfg: expandConfig を通したあとの Config。
// 戻り値: 最初に見つかった不正な値についてのエラー。エラーメッセージには設定キーの名前と
// 実際に入っていた値（展開後の文字列）を含める（5-5）。
func validateExpanded(cfg *Config) error {
	if !filepath.IsAbs(cfg.Workspace.Root) {
		return invalidValueError(
			keyWorkspaceRoot,
			cfg.Workspace.Root,
			"絶対パスにすること（相対パスは WORKFLOW.md が置かれているディレクトリを基準に解決する。5-1）",
		)
	}
	if cfg.Claude.HookBridge.Listen != nil && *cfg.Claude.HookBridge.Listen != "" {
		if !filepath.IsAbs(*cfg.Claude.HookBridge.Listen) {
			return invalidValueError(
				keyClaudeHookListen,
				*cfg.Claude.HookBridge.Listen,
				"絶対パスにすること（相対パスだと continuo を起動したディレクトリによって socket の場所が変わる。"+
					"null にすれば 3-23 の探索順で決まる）。**既にある共用のディレクトリ（ホーム直下など）の直下を"+
					"指さないこと。**その親ディレクトリは socket・ロックファイル・issue ごとの逃がし先を置く"+
					"実行時ディレクトリとして使われ、権限が 0700 でなければ起動を止める（3-23）",
			)
		}
	}
	return nil
}

// validateAutomatedStateRewrite は `tracker.automated_state_rewrite` を検査する（設計 3-55）。
//
// **5つを見る。**どれも「設定は通るのに、実行時は1度も効かない」か
// 「効いた結果が壊れる」ものである。
//
//	キーが空          … Status 名として存在しない
//	値が空            … 戻す先が無い（書き戻さないなら行ごと消す）
//	キーと値が同じ    … 同じ値の書き込みは省かれるので、巡回のたびに書きに行き続ける
//	キーが既知の Status … その行は1度も発火しない（知らない Status のときだけ引くため）
//	値が active_states の外 … 戻した先で作業を続けられない
//
// **キーが既知の Status だと、その行は死ぬ。**書き戻しを引くのは
// 「continuo が知らない Status になった」ときだけであり（`handleUnknownState`）、
// **既に名前の出てくる Status はそこへ来ない。**設定した人は効いているつもりでいる。
//
// **値は `active_states` に入っていなければならない。**戻す先が `terminal_states` だと
// 書き戻した瞬間に run が終わり、`cleanup.on_states` だと worktree が消える。
// **`running_state` / `dispatch_state` と同じ扱いにする**（どちらも `active_states` に
// 入っていることを要求している）。
//
// **キーは名前順に見る。**map の反復順は決まらないので、そのまま回すと同じ設定ファイルから
// 実行のたびに違う行のエラーが出る。**大文字小文字だけが違うキーが2つあると、
// どちらに当たるかも実行のたびに変わる**ので、それも弾く。
//
// cfg: 検査する設定。
// 戻り値: 誤りがあればそのエラー。無ければ nil。
func validateAutomatedStateRewrite(cfg *Config) error {
	froms := make([]string, 0, len(cfg.Tracker.AutomatedStateRewrite))
	for from := range cfg.Tracker.AutomatedStateRewrite {
		froms = append(froms, from)
	}
	sort.Strings(froms)

	named := KnownStates(cfg.Tracker)
	seen := map[string]string{}
	for _, from := range froms {
		to := cfg.Tracker.AutomatedStateRewrite[from]
		if strings.TrimSpace(from) == "" {
			return requiredValueError("tracker.automated_state_rewrite のキー（自動化が書いた Status 名）")
		}
		key := strings.ToLower(strings.TrimSpace(from))
		if first, dup := seen[key]; dup {
			return invalidValueError(
				fmt.Sprintf("tracker.automated_state_rewrite.%s", from),
				to,
				fmt.Sprintf(
					"キー %q と大文字小文字だけが違うので、どちらの行に当たるかが実行のたびに変わる"+
						"（どちらか1行にすること）", first),
			)
		}
		seen[key] = from
		if strings.TrimSpace(to) == "" {
			return invalidValueError(
				fmt.Sprintf("tracker.automated_state_rewrite.%s", from),
				`""`,
				"戻す先の Status 名を書くこと（書き戻さないなら、この行ごと消すこと）",
			)
		}
		if containsStateFold([]string{from}, to) {
			return invalidValueError(
				fmt.Sprintf("tracker.automated_state_rewrite.%s", from),
				to,
				"キーと違う Status 名にすること（同じ値では Status が動かず、巡回のたびに書きに行き続ける）",
			)
		}
		if containsStateFold(named, from) {
			return invalidValueError(
				"tracker.automated_state_rewrite のキー",
				from,
				"tracker の他のキー（active_states / terminal_states / running_state / "+
					"dispatch_state / failure_state / status_signal_map の遷移先）に無い Status 名にすること"+
					"（既に名前の出てくる Status は「知らない Status」にならないので、この行は1度も効かない）",
			)
		}
		if !containsStateFold(cfg.Tracker.ActiveStates, to) {
			return invalidValueError(
				fmt.Sprintf("tracker.automated_state_rewrite.%s", from),
				to,
				"tracker.active_states に含まれる値にすること"+
					"（戻した先が作業中の Status でないと、書き戻した直後に run が終わるか worktree が消える）",
			)
		}
	}
	return nil
}

// containsStateFold は ss の中に target と同じ状態名があるかどうかを返す。
//
// **大文字小文字を無視し、前後の空白を落として比べる。**トラッカーの Status を
// 照合する側（internal/tracker の foldStatus、internal/orchestrator の containsFold、
// internal/abandon の containsFold）が全部そうしているので、**ここだけ完全一致で
// 比べると、検証を通った設定が実行時には別の意味になる。**
//
// **具体的には `failure_state` の保証が崩れる。**`active_states` に `In Progress`、
// `failure_state` に `in progress` と書いた設定は完全一致では通ってしまうが、
// 実行時は同じ状態として扱われ、**打ち切った issue が永久に再 dispatch される。**
//
// ss: 調べる状態名の一覧。
// target: 探す状態名。
// 戻り値: 同じ状態名があれば true。
func containsStateFold(ss []string, target string) bool {
	t := strings.TrimSpace(target)
	for _, s := range ss {
		if strings.EqualFold(strings.TrimSpace(s), t) {
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
	return i18n.Errorf(i18n.KeyConfigValidateInvalidValue, key, value, requirement)
}

// requiredValueError は「値が空・未設定である」ことを表すエラーを作る。
// key: 設定キーの名前（ドット区切り）。
func requiredValueError(key string) error {
	return i18n.Errorf(i18n.KeyConfigValidateRequired, key)
}

// validateHandoff は持ち回りの設定を検査する（設計 3-77 / 3-77b / 3-77c）。
//
// **負の値を通してはならない。**待ち時間が負だと締め切りが常に過ぎたことになり、
// **入札を書いた直後に自分を勝者と決めてしまう。**期限が負だと、進捗を書いている
// 他の機械の担当を毎回外しにいく。
//
// **マージンは 0〜100 に限る。**余裕値は `100 − 使用率 − マージン` なので、
// **100 を超えるマージンでは、使用率が 0 でも余裕値がマイナスになり、
// その機械は永久に入札しない**（黙って1件も処理しなくなる）。
//
// h: `tracker.provider.handoff` の設定。
// 戻り値: 値が範囲の外にあるときのエラー。
func validateHandoff(h TrackerProviderHandoffConfig) error {
	if h.BidWindowMs < 0 {
		return invalidValueError("tracker.provider.handoff.bid_window_ms", h.BidWindowMs,
			i18n.T(i18n.KeyConfigValidateHandoffBidWindowRange))
	}
	if h.IdleTimeoutMs < 0 {
		return invalidValueError("tracker.provider.handoff.idle_timeout_ms", h.IdleTimeoutMs,
			i18n.T(i18n.KeyConfigValidateHandoffIdleTimeoutRange))
	}
	// **1分（60000ミリ秒）より短いものを弾く。**送る文面へは分に直して埋めるので、
	// **59999 までは全部「0分以上黙らない」になる。**
	// 0 だけを弾いても、防ぎたい状態（0分の文面）は防げない。
	if h.ProgressIntervalMs < 60000 {
		return invalidValueError("tracker.provider.handoff.progress_interval_ms", h.ProgressIntervalMs,
			i18n.T(i18n.KeyConfigValidateHandoffProgressIntervalRange))
	}
	// **実行時に効く値と比べる。**`idle_timeout_ms` は 0 なら既定の18時間になるので、
	// **0 のまま比べると、案内どおり 0 を書いた人だけが、この検査を失う。**
	if idle := ResolveHandoffIdleTimeoutMs(h.IdleTimeoutMs); h.ProgressIntervalMs >= idle {
		// **これより長いと、エージェントが指示どおりに書いていても、書く前に担当が外れる。**
		return invalidValueError("tracker.provider.handoff.progress_interval_ms", h.ProgressIntervalMs,
			i18n.T(i18n.KeyConfigValidateHandoffProgressIntervalTooLong, idle))
	}
	if h.RecheckIntervalMs < 0 {
		return invalidValueError("tracker.provider.handoff.recheck_interval_ms", h.RecheckIntervalMs,
			i18n.T(i18n.KeyConfigValidateHandoffRecheckIntervalRange))
	}
	if h.FiveHourMarginPercent < 0 || h.FiveHourMarginPercent > 100 {
		return invalidValueError("tracker.provider.handoff.five_hour_margin_percent",
			h.FiveHourMarginPercent, i18n.T(i18n.KeyConfigValidateHandoffMarginRange))
	}
	if h.WeeklyMarginPercent < 0 || h.WeeklyMarginPercent > 100 {
		return invalidValueError("tracker.provider.handoff.weekly_margin_percent",
			h.WeeklyMarginPercent, i18n.T(i18n.KeyConfigValidateHandoffMarginRange))
	}
	// **決められた値だけを通す**（issue #134 / #136 / #140）。
	// **知らない値を黙って既定へ倒さない。**`warn_only` のつもりで打ち間違えた設定が
	// 通ってしまうと、切ったはずの案内が issue へ書かれる。**書いたものは消せない。**
	// **空文字はここへ来ない。**`DefaultConfig` が `warn_and_comment` を入れている。
	switch h.OnAssigneeGate {
	case OnAssigneeGateWarnAndComment, OnAssigneeGateWarnOnly:
	default:
		return invalidValueError("tracker.provider.handoff.on_assignee_gate", h.OnAssigneeGate,
			i18n.T(i18n.KeyConfigValidateHandoffOnAssigneeGate))
	}
	return nil
}

// trustRepositoryPattern は trust.repositories の要素として受け付ける形である（3-33）。
//
// **owner は英数字で始まり、英数字とハイフンだけの39文字以内**（GitHub の user /
// organization 名の規則）。**repo は英数字・ハイフン・アンダースコア・ドットの100文字以内**
// （GitHub のリポジトリ名の規則）。
//
// **ここで弾く目的は2つある。**1つは打ち間違いをその場で知らせること、もう1つは
// `ghq list -p -e` と `git -C` へ渡す文字列に、パスの区切りや空白を混ぜられないようにすること
// である（`continuo trust` はこの値を鍵にして `~/.claude.json` を書き換える）。
var trustRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})/[A-Za-z0-9._-]{1,100}$`)

// validateTrustRepositories は trust.repositories の列挙を検査する。
//
// **重複も弾く。**この一覧は人間が手で削る前提のもの（3-33）であり、同じ行が2つあるのは
// 消し損ねか貼り付けの誤りである。黙って読み飛ばすと、消したつもりの行が残る。
//
// repos: trust.repositories に書かれた文字列の並び。
// 戻り値: 最初に見つかった不正な要素についてのエラー。すべて正しければ nil。
func validateTrustRepositories(repos []string) error {
	seen := make(map[string]struct{}, len(repos))
	for i, r := range repos {
		if !trustRepositoryPattern.MatchString(r) {
			return invalidValueError(
				fmt.Sprintf("trust.repositories[%d]", i), r,
				`"owner/repo" の形で書くこと（例 octocat/hello-world）`)
		}
		if _, dup := seen[r]; dup {
			return invalidValueError(
				fmt.Sprintf("trust.repositories[%d]", i), r,
				"同じリポジトリが2回書かれている（重複した行を消すこと）")
		}
		seen[r] = struct{}{}
	}
	return nil
}
