// Package config は WORKFLOW.md（front matter + 本文）を読み込み、検証し、
// 環境変数・チルダの展開まで済ませた設定を提供する。
//
// front matter のキー構成は docs/plans/continuo_design.md の 5-2 を正とする。
// キー名を変えるときは必ずその節も合わせて直すこと。
package config

// Config は WORKFLOW.md の front matter をパースした結果である。
// フィールドは docs/plans/continuo_design.md の 5-2 に定義された YAML キーと1対1に対応する。
// 各サブ構造体のコメントも同じ節の説明をそのまま転記している。
type Config struct {
	// Tracker は GitHub Projects v2 のボードをどう見るかを決める（SPEC.md 5.3 由来。名前は変えない）。
	Tracker TrackerConfig `yaml:"tracker"`
	// Polling は巡回の間隔を決める。
	Polling PollingConfig `yaml:"polling"`
	// Workspace は worktree の置き場所と身元ファイルの名前を決める。
	Workspace WorkspaceConfig `yaml:"workspace"`
	// WorkspaceHooks は worktree のライフサイクルに沿って呼ぶ外部コマンドを決める。
	// 仕様の hooks（SPEC.md 5.3）を Claude Code の hook と混同しないよう改名してある（設計 8-1）。
	WorkspaceHooks WorkspaceHooksConfig `yaml:"workspace_hooks"`
	// Agent は同時実行数・turn 数・引き継ぎ回数の上限を決める。
	Agent AgentConfig `yaml:"agent"`
	// Claude は Claude Code の起動方法を決める（仕様の codex セクションの全面差し替え。設計 8-1）。
	Claude ClaudeConfig `yaml:"claude"`
	// Herdr は herdr の socket API との接続方法と worktree の作り方を決める（仕様に対応物が無い独自区分）。
	Herdr HerdrConfig `yaml:"herdr"`
	// Naming は識別子の正規化の挙動を決める（3-7）。
	Naming NamingConfig `yaml:"naming"`
	// Cleanup は worktree と branch の後始末の挙動を決める（3-9）。
	Cleanup CleanupConfig `yaml:"cleanup"`
	// RateLimit は Claude Code のレートリミット待機の挙動を決める（3-27。仕様の範囲外）。
	RateLimit RateLimitConfig `yaml:"rate_limit"`
	// Trust はリポジトリの信頼確認をどう扱うかを決める（3-11 / 4-3）。
	Trust TrustConfig `yaml:"trust"`
	// Restart は再起動時に孤児となった実行中 issue をどう扱うかを決める（3-4）。
	Restart RestartConfig `yaml:"restart"`
	// Runtime は二重起動防止のロックファイルの場所を決める（3-17）。
	Runtime RuntimeConfig `yaml:"runtime"`
	// Server は任意の HTTP ダッシュボードの起動を決める（SPEC.md 13.7 の任意拡張）。
	Server ServerConfig `yaml:"server"`
}

// TrackerCommentsConfig は issue のコメントの読み方を決める。
//
// 読むのは「エージェントがコメントを書いたかどうかを判別するため」であって、
// プロンプトへ渡すためではない（設計 3-29）。issue の中身はプロンプトに埋め込まず、
// エージェントが gh issue view --comments で自分で読む。
type TrackerCommentsConfig struct {
	// Fetch は既存コメントを取得するかどうかである。
	Fetch bool `yaml:"fetch"`
	// Max は取得する件数の上限である。
	Max int `yaml:"max"`
	// Order は並び順である。想定する値は "oldest_first" のみ。
	Order string `yaml:"order"`
	// Marker はエージェントが書くコメントの先頭に必ず書かせる固定の印である（2-2）。
	Marker string `yaml:"marker"`
	// SelfMarker は continuo 自身が書くコメントの印である。次の turn の入力からはこの印のコメントを外す。
	SelfMarker string `yaml:"self_marker"`
}

// TrackerProviderConfig は GitHub Projects v2 アダプタが所有する設定である。
// 仕様はこの中身を規定しないので、continuo 独自の構造で持つ。
type TrackerProviderConfig struct {
	// Owner は project を所有する GitHub organization / user 名である。
	Owner string `yaml:"owner"`
	// ProjectNumber は project #3 のような番号である。
	ProjectNumber int `yaml:"project_number"`
	// StatusField はボード上の Status フィールドの名前である。
	StatusField string `yaml:"status_field"`
	// TokenSource は continuo 自身がボードを読むための認証の取り方である。"gh_auth" か "env" のどちらか。
	TokenSource string `yaml:"token_source"`
	// TokenEnv は TokenSource が "env" のときだけ使う、トークンを格納した環境変数名である。
	TokenEnv string `yaml:"token_env"`
	// Comments は着手時に渡す既存コメントの取得方法である。
	Comments TrackerCommentsConfig `yaml:"comments"`
}

// TrackerConfig は GitHub Projects v2 のボードをどう見るかを決める。
type TrackerConfig struct {
	// Kind はトラッカーの種別である。想定する値は "github_projects_v2" のみ。
	Kind string `yaml:"kind"`
	// Provider は GitHub Projects v2 アダプタ固有の設定である。
	Provider TrackerProviderConfig `yaml:"provider"`
	// RequiredLabels は dispatch の条件になる必須ラベルである。空なら制約なし。
	RequiredLabels []string `yaml:"required_labels"`
	// ActiveStates は「作業中の状態」である。In Progress を必ず含めること（3-10）。
	ActiveStates []string `yaml:"active_states"`
	// TerminalStates は「完了」を意味する状態である。In Review を含めてはならない（3-9 / 3-10）。
	TerminalStates []string `yaml:"terminal_states"`
	// RunningState は dispatch したときに書き込む先の状態である（設計 3-16 の段2）。
	// active_states に含まれている必要がある。含まれていないと、dispatch した直後に
	// 自分の worker を候補から外してしまう（設計 3-10）。
	RunningState string `yaml:"running_state"`
	// DispatchState は取り残された issue を戻す先の状態である。
	DispatchState string `yaml:"dispatch_state"`
	// FailureState は打ち切り・失敗のときに落とす先の状態である（4-1）。
	FailureState string `yaml:"failure_state"`
	// VerifyStatesEvery は Status の選択肢名を照合する間隔（巡回の回数）である（設計 3-6）。
	// 毎巡回では行わない。選択肢名が変わるのは人間がボードを触ったときだけなので、
	// 20 巡回に1回で足りる。0 なら起動時の1回だけ行う。
	VerifyStatesEvery int `yaml:"verify_states_every"`
	// WriteIntervalMs は、トラッカーへの書き込みどうしの最小の間隔である（3-31）。
	// GitHub が変更を伴うリクエストの間を1秒以上あけることを推奨しているため、既定は 1000 とする。
	WriteIntervalMs int `yaml:"write_interval_ms"`
	// StatusSignalPrefix は、エージェントが応答に書く表明の印である（3-25）。
	// continuo は turn が終わったと判定したあと transcript を読み、
	// この印で始まる行を探して、続く値を StatusSignalMap で引いて Status を動かす。
	// last_assistant_message は使わない（印のあとに道具を呼ぶと落ちるため）。
	StatusSignalPrefix string `yaml:"status_signal_prefix"`
	// StatusSignalMap は表明の値と Status の対応である（3-25）。
	// キーは表明の値（"review" / "blocked" / "working" など）、値は動かす先の Status 名である。
	// 値が nil（YAML の null）なら「その表明では Status を動かさない」を意味するため、
	// 「対応が無い（キーそのものが無い）」と区別できるようポインタで持つ。
	StatusSignalMap map[string]*string `yaml:"status_signal_map"`
}

// PollingConfig は巡回の間隔を決める。
type PollingConfig struct {
	// IntervalMs は巡回の間隔（ミリ秒）である。
	IntervalMs int `yaml:"interval_ms"`
}

// WorkspaceConfig は worktree の置き場所と身元ファイルの名前を決める。
type WorkspaceConfig struct {
	// Root は worktree を置く置き場所である。チルダは展開する（5-5）。
	Root string `yaml:"root"`
	// Layout は置き場所の構成規則である。想定する値は "gwq" のみ（3-22）。
	Layout string `yaml:"layout"`
	// IdentityFile は worktree の身元を書くファイルの名前である（3-18）。
	IdentityFile string `yaml:"identity_file"`
}

// WorkspaceHooksConfig は worktree のライフサイクルに沿って呼ぶ外部コマンドを決める。
// いずれのコマンド文字列も null（未設定）を許す。null のときはそのフェーズで何も実行しない。
type WorkspaceHooksConfig struct {
	// AfterCreate は worktree を作った直後に実行するコマンドである。失敗したら致命として扱う。
	AfterCreate *string `yaml:"after_create"`
	// BeforeRun は Claude Code を起動する直前に実行するコマンドである。失敗したら致命として扱う。
	// 走るのは dispatch 1回（エージェントの試行1回）につき1度であって、turn ごとではない（SPEC.md 5.3.4）。
	BeforeRun *string `yaml:"before_run"`
	// AfterRun はエージェントの試行が終わったあとに実行するコマンドである。失敗しても記録して続ける。
	// 成功・失敗・時間切れ・中断のいずれでも走る。turn ごとではなく dispatch 1回につき1度である（SPEC.md 5.3.4）。
	AfterRun *string `yaml:"after_run"`
	// BeforeRemove は worktree を消す直前に実行するコマンドである。失敗しても記録して続ける。
	BeforeRemove *string `yaml:"before_remove"`
	// TimeoutMs は各コマンドの実行時間の上限（ミリ秒）である。
	// **0 以下は受理しない。**無人運用では、hook が固まったまま返らないことに誰も気づけない。
	TimeoutMs int `yaml:"timeout_ms"`
}

// AgentConfig は同時実行数・turn 数・引き継ぎ回数の上限を決める。
type AgentConfig struct {
	// MaxConcurrentAgents は同時に走らせる Claude Code の数の上限である。
	MaxConcurrentAgents int `yaml:"max_concurrent_agents"`
	// MaxConcurrentAgentsByState は状態ごとの上限である。空なら MaxConcurrentAgents にフォールバックする。
	MaxConcurrentAgentsByState map[string]int `yaml:"max_concurrent_agents_by_state"`
	// MaxTurns は continuo からの送信回数の上限である（3-14。エージェント自身が投入する turn は数えない）。
	MaxTurns int `yaml:"max_turns"`
	// MaxTakeover は再起動をまたいで引き継いだ回数の上限である（3-4 / 3-18）。
	MaxTakeover int `yaml:"max_takeover"`
	// MaxRetryBackoffMs は指数バックオフの上限（ミリ秒）である。
	MaxRetryBackoffMs int `yaml:"max_retry_backoff_ms"`
	// MaxRetries は stall や異常終了に対するリトライ回数の上限である。
	// 尽きたら tracker.failure_state へ落とす。0 ならリトライしない。
	MaxRetries int `yaml:"max_retries"`
}

// ClaudePermissionsConfig は Claude Code の許可リストである。
// permission_mode が dontAsk のとき、許可リストの外は全部拒否される（3-11）。
type ClaudePermissionsConfig struct {
	// Allow は許可するツール・コマンドのパターンである。
	Allow []string `yaml:"allow"`
	// Deny は明示的に拒否するパターンである。
	Deny []string `yaml:"deny"`
}

// ClaudeHookBridgeConfig は turn 終了検知の実体である hook の届け方を決める（3-12）。
type ClaudeHookBridgeConfig struct {
	// Mode は hook をどう届けるかである。受理するのは "settings_flag"（--settings で外部の
	// 設定ファイルを指す）だけである（設計 3-12）。"worktree_local"（worktree に
	// .claude/settings.local.json を置く）は、置き場所・.git/info/exclude への登録・
	// 片付けの仕様がどこにも無いため受理しない。
	Mode string `yaml:"mode"`
	// Listen は hook を受ける socket の絶対パスである。null なら 3-23 の探索順で決める。
	Listen *string `yaml:"listen"`
	// LivenessHooks は生きていることの確認だけに使う hook 名の一覧である（3-21）。
	// turn の終わりの判定には使わない。
	LivenessHooks []string `yaml:"liveness_hooks"`
}

// ClaudeConfig は Claude Code の起動方法を決める。
type ClaudeConfig struct {
	// Kind は herdr に渡す agent の種別である。
	Kind string `yaml:"kind"`
	// PermissionMode は Claude Code の権限モードである。入力を待たない唯一のモードである "dontAsk" を使う（3-11）。
	PermissionMode string `yaml:"permission_mode"`
	// Permissions は dontAsk のときに参照される許可・拒否リストである。
	Permissions ClaudePermissionsConfig `yaml:"permissions"`
	// Env は Claude Code の起動時に渡す環境変数である。値は展開しない（5-5）。
	Env map[string]string `yaml:"env"`
	// PollWaitMs は agent.wait 1回あたりの待ち時間（ミリ秒）である（3-2）。
	// turn の待ち受けを短く切り、経過時間を continuo 側で数えるためのもの。
	// herdr に「待ちの時計を止める」手段が無いので、枠待ちの間を数えないためにこの形にする。
	// turn 全体の上限は TurnTimeoutMs のほうである。
	PollWaitMs int `yaml:"poll_wait_ms"`
	// SettleMs は、background_tasks が空の Stop を受けてから turn の終わりと確定するまでの
	// 猶予（ミリ秒）である（1-3 / 3-2）。空配列の Stop は turn の途中にも発火するため、
	// この時間のあいだ <task-notification> で始まる UserPromptSubmit が来ないことを確かめる。
	// 観測できた4件はいずれも 0.035 秒以内だったが上限は測れていない。運用のログで決め直す。
	SettleMs int `yaml:"settle_ms"`
	// WaitUntil は herdr agent prompt --wait に渡す状態の一覧である（3-2）。
	// blocked を外すと、権限の確認で止まった turn を拾えず時間切れまで待つことになる。
	WaitUntil []string `yaml:"wait_until"`
	// TurnTimeoutMs は1つの turn の上限（ミリ秒）である。continuo が turn を送ってから Stop を受けるまでを測る。
	TurnTimeoutMs int `yaml:"turn_timeout_ms"`
	// ReadTimeoutMs は herdr の socket API の応答を待つ上限（ミリ秒）である（8-1。仕様と同名だが相手が違う）。
	ReadTimeoutMs int `yaml:"read_timeout_ms"`
	// StallTimeoutMs は無反応とみなすまでの時間（ミリ秒）である。0 以下で無効（3-21）。
	StallTimeoutMs int `yaml:"stall_timeout_ms"`
	// StartupTimeoutMs は herdr の agent 起動を待つ時間の上限（ミリ秒）である。
	StartupTimeoutMs int `yaml:"startup_timeout_ms"`
	// HookBridge は turn 終了検知の実体である hook の届け方を決める。
	HookBridge ClaudeHookBridgeConfig `yaml:"hook_bridge"`
}

// HerdrWorktreeConfig は herdr を介した worktree の作り方を決める。
type HerdrWorktreeConfig struct {
	// CreateViaHerdr は herdr に worktree を workspace として開かせるかどうかである（3-22）。
	CreateViaHerdr bool `yaml:"create_via_herdr"`
	// BranchTemplate は branch 名のテンプレートである。区切りにスラッシュを使う（3-22）。
	// このキーには 5-5 の展開規則を適用しない（テンプレート文字列であり、環境変数展開の対象ではない）。
	BranchTemplate string `yaml:"branch_template"`
	// Base は派生元の branch 名である。null ならトラッカーが返す既定 branch を使う。
	Base *string `yaml:"base"`
}

// HerdrConfig は herdr の socket API との接続方法と worktree の作り方を決める。
type HerdrConfig struct {
	// Socket は herdr の socket API のパスである。チルダ・環境変数を展開する（5-5）。
	Socket string `yaml:"socket"`
	// Protocol は herdr の socket API の版である。起動時に照合して合わなければ止める。
	Protocol int `yaml:"protocol"`
	// Worktree は herdr を介した worktree の作り方である。
	Worktree HerdrWorktreeConfig `yaml:"worktree"`
}

// NamingConfig は識別子の正規化の挙動を決める（3-7）。
type NamingConfig struct {
	// WarnOnInformationLoss は正規化で情報が落ちたときに警告を出すかどうかである。
	WarnOnInformationLoss bool `yaml:"warn_on_information_loss"`
}

// CleanupConfig は worktree と branch の後始末の挙動を決める（3-9）。
type CleanupConfig struct {
	// Enabled は後始末そのものを行うかどうかである。
	Enabled bool `yaml:"enabled"`
	// OnStates は片付けを始める状態の一覧である。「active でなくなった時点」ではなく、
	// この一覧に入った時点で片付ける（設計文書の矛盾を解消した結論。継続の記録は
	// docs/plans/continuo_impl.md の「実装を止める3件」を参照）。
	OnStates []string `yaml:"on_states"`
	// RequireCleanWorktree は未コミットの変更が残っていたら消さない、という条件である。
	RequireCleanWorktree bool `yaml:"require_clean_worktree"`
	// RequirePushed は push していない commit が残っていたら消さない、という条件である。
	RequirePushed bool `yaml:"require_pushed"`
	// DeleteBranch は branch も一緒に消すかどうかである。
	DeleteBranch bool `yaml:"delete_branch"`
	// SweepOnStartup は起動時に終了状態の worktree と孤児 branch を掃除するかどうかである。
	SweepOnStartup bool `yaml:"sweep_on_startup"`
}

// RateLimitConfig は Claude Code のレートリミット待機の挙動を決める（3-27。仕様の範囲外）。
type RateLimitConfig struct {
	// Source はレートリミットの値をどこから取るかである。"oauth_usage_api" か "none" のどちらか。
	// "none" なら usage API を1回も叩かず、枠の判定を行わない（stall 検知だけに頼る。3-27）。
	// usage API がトークンを消費するかどうかを判別できていないため、"none" は必須の逃げ道である。
	Source string `yaml:"source"`
	// TokenSource はレートリミットを読むための認証情報の出所である（3-27）。
	// 想定する値は "claude_credentials"（Claude Code が使っている資格情報を読む）か "env"。
	// **読み取りだけで、書き換えない**（`~/.claude.json` を書き換えないという絶対制約に従う）。
	TokenSource string `yaml:"token_source"`
	// TokenEnv は TokenSource が "env" のときに読む環境変数の名前である（設計 3-27）。
	// "env" のとき必須。空だとどこからトークンを取ればよいか決まらない。
	TokenEnv string `yaml:"token_env"`
	// PauseAbovePercent はこの割合を超えたら新規の dispatch を止める閾値（0〜100）である。
	PauseAbovePercent int `yaml:"pause_above_percent"`
	// PollIntervalMs はレートリミットの値を確認する間隔（ミリ秒）である。
	PollIntervalMs int `yaml:"poll_interval_ms"`
}

// TrustConfig はリポジトリの信頼確認をどう扱うかを決める（3-11 / 4-3）。
type TrustConfig struct {
	// RequireRepoTrusted は dispatch の前にリポジトリの信頼登録を検査するかどうかである。
	RequireRepoTrusted bool `yaml:"require_repo_trusted"`
	// OnUntrusted は未信頼のリポジトリを見つけたときの扱いである。想定する値は "skip_and_comment" のみ。
	OnUntrusted string `yaml:"on_untrusted"`
}

// RestartConfig は再起動時に孤児となった実行中 issue をどう扱うかを決める（3-4）。
type RestartConfig struct {
	// OrphanRunningAction は "redispatch" / "to_dispatch_state" / "to_failure_state" のいずれかである。
	OrphanRunningAction string `yaml:"orphan_running_action"`
}

// RuntimeConfig は二重起動防止のロックファイルの場所を決める（3-17）。
type RuntimeConfig struct {
	// LockFile はロックファイルの絶対パスである。null なら hook の socket と同じディレクトリに置く。
	LockFile *string `yaml:"lock_file"`
}

// ServerConfig は任意の HTTP ダッシュボードの起動を決める（SPEC.md 13.7 の任意拡張）。
type ServerConfig struct {
	// Port はサーバのポート番号である。null ならサーバを起動しない。
	Port *int `yaml:"port"`
}
