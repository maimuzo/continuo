package config

import (
	"runtime"

	"github.com/maimuzo/continuo/internal/i18n"
)

// 持ち回りのために continuo が issue へ書くコメントの印である（設計 3-77a / 3-77b / 3-77c）。
//
// **設定キーにしない。**この3つは continuo どうしが読み合う印であり、
// **1台でも違う文字列を使うと、その機械の入札も担当も他の機械から見えなくなる。**
// `tracker.comments.marker` / `self_marker`（エージェントと continuo のあいだの印）とは
// 役割が違うので、同じところには置かない。
//
// **この印が先頭に付いたコメントは、エージェントへ渡す入力から外す**（設計 3-77a）。
// **投稿者は問わない。**
const (
	// HandoffBidMarker は入札のコメントの印である。**入札のたびに新しく1件書く。**
	HandoffBidMarker = "<!-- continuo:bid -->"
	// HandoffHoldMarker は担当を取ったことを示すコメントの印である。**勝ったとき1件だけ書く。**
	//
	// **この印が1件でもあることが「その担当者は機械である」の唯一の証拠である**（設計 3-77b）。
	// 印が1件も無い担当は人間が付けたものなので、continuo は奪わない。
	HandoffHoldMarker = "<!-- continuo:hold -->"
	// HandoffReleasedMarker は期限切れの担当を外したことを知らせるコメントの印である。
	HandoffReleasedMarker = "<!-- continuo:released -->"
)

// DefaultHandoffIdleTimeoutMs は、担当を外すまでの待ち時間の既定である（18時間）。
//
// **`tracker.provider.handoff.idle_timeout_ms` に 0 を書くと、この値が使われる。**
// **検査も、実行時と同じ値で比べる。**0 のまま比べると、
// 「0 なら既定の18時間」と案内されて 0 を書いた人だけが、`progress_interval_ms` の
// 大小の検査を失う。
const DefaultHandoffIdleTimeoutMs = 64800000

// ResolveHandoffIdleTimeoutMs は、実行時に効く担当の期限を返す。
//
// ms: 設定に書かれた値。
// 戻り値: 0 以下なら既定の18時間、そうでなければそのまま。
func ResolveHandoffIdleTimeoutMs(ms int) int {
	if ms <= 0 {
		return DefaultHandoffIdleTimeoutMs
	}
	return ms
}

// ProgressMarker は、エージェントが書く進捗の報告だけに付く印である（設計 5-3j / 5-3l）。
//
// **持ち回りの期限（設計 3-77b / 5-3l）を進めるのは、この印が付いたコメントだけである。**
// **エージェントも continuo も人間も、同じ GitHub アカウントで投稿する**
// （[internal/tracker/ghuser.go](../tracker/ghuser.go) の 23-25行）。
// **投稿者だけで数えると、人間が無関係なコメントを1件書いただけで期限が延びる。**
// **黙り込んだエージェントを、別の機械が拾い直せなくなる。**
//
// **上の3つと違い、この印が付いたコメントはエージェントへ渡す入力から外さない**
// （設計 3-77a が外すのは入札・hold・released の3つだけである）。
// **エージェント自身が、この印で書き足す先のコメントを探す**ので、見えなくなると探せない。
//
// **設定キーにしない。**`tracker.provider.comments.marker` は機械ごとに違う値を書けるので、
// **別の機械が書いた進捗報告を数えられなくなる。**この印は固定である。
//
// **組み込みのプロンプト（[internal/prompt/builtin.md](../prompt/builtin.md)）が
// エージェントへ書かせる文字列と、1文字も違ってはならない。**
// **違うと、エージェントは書いているのに数えられず、18時間で担当が外れる。**
const ProgressMarker = "<!-- continuo:progress -->"

// PlanMarker は、エージェントが実装の前に書く計画のコメントに付ける印である（設計 5-3）。
//
// **この印が要る理由は、成果の報告と見分けるためである。**
// 計画のコメントも進捗報告も、本文の先頭は `<!-- continuo:agent -->` なので、
// **印が無いと `hasRunComment` が「この run は成果を書いた」と判定する。**
// **計画は run の最初に書かれるので、判定はほぼ必ず外れる。**
// 外れると、turn が途中で終わった run に「何をしたか」を書かせ直す経路が飛ぶ。
//
// **設定キーにしない。**理由は ProgressMarker と同じである。
//
// **組み込みのプロンプト（[internal/prompt/builtin.md](../prompt/builtin.md)）が
// エージェントへ書かせる文字列と、1文字も違ってはならない。**
const PlanMarker = "<!-- continuo:plan -->"

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
				// 同じボードを複数の機械で持ち回るときの取り決め（設計 3-77）。
				Handoff: TrackerProviderHandoffConfig{
					BidWindowMs:   180000,
					IdleTimeoutMs: 64800000,
					// **1時間。**送る文面へは分に直して埋まるので、「60分以上黙らない」になる（設計 5-3n）。
					ProgressIntervalMs: 3600000,
					RecheckIntervalMs:  3600000,
					// **既定のマージンは 10%。**枠を使い切る手前で入札をやめさせるための余白であり、
					// 「continuo 以外の作業のために残しておく割合」でもある。
					FiveHourMarginPercent: 10,
					WeeklyMarginPercent:   10,
					OnAssigneeGate:        OnAssigneeGateWarnAndComment,
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
		Server: ServerConfig{
			Port: nil,
		},
		// **既定は "auto" である。**環境変数 LANG から決め、決まらなければ**英語**にする（3-35）。
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
