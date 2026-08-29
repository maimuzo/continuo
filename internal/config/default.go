package config

import (
	"runtime"

	"github.com/maimuzo/continuo/internal/i18n"
)

// DefaultConfig は front matter に書かれなかったキーへ入る既定値を返す。
// front matter のパースはこの構造体へ上書きする形で行う（yaml.UnmarshalWithOptions は
// 与えられた値へフィールド単位で上書きするため、front matter に書かれなかったキーは
// ここで設定した既定値のまま残る）。
//
// 既定値は docs/plans/continuo_design.md の 5-2 の設定例をそのまま Go の値にしたものである。
// 例を変えるときは、この関数も合わせて直すこと。
func DefaultConfig() *Config {
	return &Config{
		Tracker: TrackerConfig{
			Kind: "github_projects_v2",
			Provider: TrackerProviderConfig{
				TokenSource: "gh_auth",
				TokenEnv:    "GITHUB_TOKEN",
				Comments: TrackerProviderCommentsConfig{
					Max:   50,
					Order: "oldest_first",
				},
			},
			Comments: TrackerCommentsConfig{
				Marker:     "<!-- continuo:agent -->",
				SelfMarker: "<!-- continuo:self -->",
			},
			RequiredLabels: []string{},
			// In Progress を必ず含める（3-10）。ここで欠かすと dispatch 直後に自分の worker を殺す。
			ActiveStates:      []string{"Ready", "In Progress"},
			TerminalStates:    []string{"Done"},
			RunningState:      "In Progress",
			DispatchState:     "Ready",
			FailureState:      "Blocked",
			VerifyStatesEvery: 20,
			// 知らない Status を見つけてから worker を止めるまでの猶予（設計 3-50）。
			// **既定は10分。**turn 1回ぶんの表明を読めれば足りる長さにしてある。
			UnknownStateGraceMs: 600000,
			// ボードの自動化が動かした Status を戻す先の対応表（設計 3-54）。
			// **既定は空である。**書かなければ、いままでどおり猶予を置いて worker を止める。
			AutomatedStateRewrite: map[string]string{},
			// エージェントが最終応答に書く表明の印と、その値から Status への対応（3-25）。
			// "working" は null（＝Status を動かさない）である。
			StatusSignalPrefix: "CONTINUO-STATUS:",
			StatusSignalMap: map[string]*string{
				"review":  stringPtr("In Review"),
				"blocked": stringPtr("Blocked"),
				"working": nil,
			},
		},
		Polling: PollingConfig{
			IntervalMs: 30000,
		},
		Workspace: WorkspaceConfig{
			Root:         "~/worktrees",
			IdentityFile: ".continuo.json",
			// **既定は止める側である**（3-49）。壊れた worktree を飛ばして走り続けると、
			// その issue はボード上で running_state のまま何時間も放置される。
			OnBrokenWorktree: OnBrokenWorktreeStop,
		},
		WorkspaceHooks: WorkspaceHooksConfig{
			AfterCreate:  nil,
			BeforeRun:    nil,
			AfterRun:     nil,
			BeforeRemove: nil,
			TimeoutMs:    60000,
		},
		Agent: AgentConfig{
			MaxConcurrentAgents:        2,
			MaxConcurrentAgentsByState: map[string]int{},
			MaxDispatchTurns:           20,
			MaxTakeover:                5,
			MaxRetryBackoffMs:          300000,
			MaxRetries:                 3,
		},
		Claude: ClaudeConfig{
			Kind:           "claude",
			PermissionMode: "dontAsk",
			Permissions: ClaudePermissionsConfig{
				// Bash は引数を限定せずツール名だけで許可する。
				// Bash(gh:*) のように限定すると、許可リストに載らない書き込み系
				// （touch / rm など）が dontAsk で拒否され、作業が途中で止まる（設計 3-11）。
				// subagent を起動する Agent ツールは、許可リストが空でも動いたため書かない。
				Allow: []string{
					"Bash",
					"Read",
					"Glob",
					"Grep",
					"Edit",
					"Write",
				},
				Deny: []string{},
			},
			Env: map[string]string{
				"CLAUDE_CODE_RETRY_WATCHDOG": "1",
			},
			PollWaitMs: 30000,
			SettleMs:   2000,
			WaitUntil:  []string{"idle", "done", "blocked"},
			// 画面の版が増えないまま待てる上限。`SPEC.md` 10.6 の既定値と同じ 1 時間である。
			TurnTimeoutMs: 3600000,
			HookBridge: ClaudeHookBridgeConfig{
				Listen: nil,
			},
			// **既定で公開リポジトリの issue にだけ判定を掛ける**（設計 3-64）。
			// 判定に回すのは Bash だけにしてある。読み書きの道具まで回すと、
			// 道具1回ごとにモデルの呼び出しが乗る。
			//
			// **Model は既定で空にする**（設計 3-64）。判定に使えるモデルの名前の一覧は
			// 公式文書に無く（"Model to use for evaluation. Defaults to a fast model" しか
			// 書かれていない）、**通らない名前を書いたときにどう倒れるかを確かめていない。**
			// 空なら settings.json へ `model` を書かず、Claude Code の既定に任せる。
			ToolGate: ClaudeToolGateConfig{
				Mode:  ClaudeToolGateModePublicOnly,
				Model: "",
				Tools: []string{"Bash"},
			},
		},
		Herdr: HerdrConfig{
			// herdr の socket の既定のパス（設計 2-1 / 5-2）。
			// 環境変数で切り替えたい利用者は WORKFLOW.md に ${HERDR_SOCKET_PATH} と書く。
			// その場合、未定義なら起動を止める（既定値へは落ちない。設計 5-5）。
			Socket:           "~/.config/herdr/herdr.sock",
			Protocol:         20,
			ReadTimeoutMs:    5000,
			StartupTimeoutMs: 60000,
			Worktree: HerdrWorktreeConfig{
				CreateViaHerdr: true,
				BranchTemplate: "continuo/{{.issue.owner}}/{{.issue.repo}}/{{.issue.number}}",
				Base:           nil,
			},
		},
		Naming: NamingConfig{
			WarnOnInformationLoss: true,
		},
		Cleanup: CleanupConfig{
			Enabled:              true,
			OnStates:             []string{"Done"},
			RequireCleanWorktree: true,
			RequirePushed:        true,
			DeleteBranch:         true,
			SweepOnStartup:       true,
		},
		RateLimit: RateLimitConfig{
			Source: "oauth_usage_api",
			// **既定は OS で分かれる。**分かれるのはこのキーだけである（defaultRateLimitTokenSource）。
			TokenSource:       defaultRateLimitTokenSource(),
			TokenEnv:          "CLAUDE_CODE_OAUTH_TOKEN",
			PauseAbovePercent: 95,
			PollIntervalMs:    300000,
		},
		Trust: TrustConfig{
			RequireRepoTrusted: true,
			OnUntrusted:        "skip_and_comment",
			// **既定は空である。**`continuo trust` は列挙されたものしか登録しないので、
			// 何も書かなければ何も登録しない（3-33）。
			Repositories: []string{},
		},
		Restart: RestartConfig{
			OrphanRunningAction: "redispatch",
		},
		Runtime: RuntimeConfig{
			LockFile: nil,
		},
		Server: ServerConfig{
			Port: nil,
		},
		// **既定は "auto" である。**環境変数 LANG から決め、決まらなければ日本語にする（3-35）。
		Language: i18n.LangConfigAuto,
	}
}

// RateLimitTokenSourceKeychain は rate_limit.token_source の「macOS の Keychain から読む」の値である。
//
// **internal/ratelimit の TokenSourceKeychain と同じ文字列である。**
// internal/ratelimit は internal/config を読むので、逆向きに参照すると循環する。
// 値がずれていないことは test/internal/config が確かめる。
const RateLimitTokenSourceKeychain = "keychain"

// RateLimitTokenSourceClaudeCredentials は rate_limit.token_source の
// 「`~/.claude/.credentials.json` から読む」の値である。
const RateLimitTokenSourceClaudeCredentials = "claude_credentials"

// RateLimitTokenSourceEnv は rate_limit.token_source の「環境変数から読む」の値である。
const RateLimitTokenSourceEnv = "env"

// defaultRateLimitTokenSource は rate_limit.token_source の既定値を OS ごとに返す。
//
// **macOS だけ `keychain` である。**macOS の Claude Code は資格情報を Keychain に置き、
// `~/.claude/.credentials.json` は無いのが普通である（2026-08-21 に実測。
// `security find-generic-password -s "Claude Code-credentials" -w` が
// `claudeAiOauth.accessToken` を含む JSON を返した）。
// **既定を `claude_credentials` のままにすると、macOS では枠の判定が黙って効かなくなる。**
//
// 戻り値: darwin なら "keychain"、それ以外は "claude_credentials"。
func defaultRateLimitTokenSource() string {
	if runtime.GOOS == "darwin" {
		return RateLimitTokenSourceKeychain
	}
	return RateLimitTokenSourceClaudeCredentials
}

// stringPtr は文字列リテラルから *string を作る。
// YAML の null と「値が入っている」を区別して持つフィールド（status_signal_map の値など）の
// 既定値を書くために使う。
//
// s: 保持したい文字列。
// 戻り値: s を指す新しいポインタ。呼び出しごとに別の変数を指すので、
// 戻り値を書き換えても他の既定値に影響しない。
func stringPtr(s string) *string {
	return &s
}
