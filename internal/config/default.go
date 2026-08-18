package config

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
				Comments: TrackerCommentsConfig{
					Fetch:      true,
					Max:        50,
					Order:      "oldest_first",
					Marker:     "<!-- continuo:agent -->",
					SelfMarker: "<!-- continuo:self -->",
				},
			},
			RequiredLabels: []string{},
			// In Progress を必ず含める（3-10）。ここで欠かすと dispatch 直後に自分の worker を殺す。
			ActiveStates:   []string{"Ready", "In Progress"},
			TerminalStates: []string{"Done"},
			DispatchState:  "Ready",
			FailureState:   "Blocked",
			// GitHub が変更を伴うリクエストの間を1秒以上あけることを推奨している（設計 3-31）。
			WriteIntervalMs: 1000,
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
			Layout:       "gwq",
			IdentityFile: ".continuo.json",
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
			MaxTurns:                   20,
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
			SettleMs:         2000,
			WaitUntil:        []string{"idle", "done", "blocked"},
			TurnTimeoutMs:    3600000,
			ReadTimeoutMs:    5000,
			StallTimeoutMs:   1800000,
			StartupTimeoutMs: 60000,
			HookBridge: ClaudeHookBridgeConfig{
				Mode:                  "settings_flag",
				Listen:                nil,
				LivenessHooks:         []string{"PreToolUse", "PostToolUse"},
				HookResponseTimeoutMs: 3000,
			},
		},
		Herdr: HerdrConfig{
			// herdr の socket の既定のパス（設計 2-1 / 5-2）。
			// 環境変数で切り替えたい利用者は WORKFLOW.md に ${HERDR_SOCKET_PATH} と書く。
			// その場合、未定義なら起動を止める（既定値へは落ちない。設計 5-5）。
			Socket:   "~/.config/herdr/herdr.sock",
			Protocol: 19,
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
			Source:            "oauth_usage_api",
			TokenSource:       "claude_credentials",
			PauseAbovePercent: 95,
			PollIntervalMs:    300000,
		},
		Trust: TrustConfig{
			RequireRepoTrusted: true,
			OnUntrusted:        "skip_and_comment",
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
	}
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
