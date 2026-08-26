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
	// Herdr は herdr の socket API との接続方法・待ち時間と、worktree の作り方を決める
	// （仕様に対応物が無い独自区分）。
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
	// Language は画面に出す文言の言語である（3-35）。
	// "auto" なら環境変数 LANG から決める。"ja" / "en" を直接書いてもよい。
	// **設定が主、環境変数が従である。**ここに書いた値は環境変数より優先する。
	Language string `yaml:"language"`
}

// TrackerProviderCommentsConfig は GitHub からコメントをどう取ってくるかを決める。
//
// **ここに置くのは GitHub 固有の制約に縛られる項目だけである。**取得の件数と並び順は
// GitHub の GraphQL の connection（first の上限が 100）に縛られる。
// マーカーは GitHub 固有ではないので TrackerCommentsConfig（tracker.comments）に置く。
//
// 読むのは「エージェントがコメントを書いたかどうかを判別するため」であって、
// プロンプトへ渡すためではない（設計 3-29）。issue の中身はプロンプトに埋め込まず、
// エージェントが gh issue view --comments で自分で読む。
//
// **取得そのものを止める設定は持たない。**取得しないと、成功した run も
// 「エージェントがコメントを書いていない」と判定されて failure_state へ落ちる。
type TrackerProviderCommentsConfig struct {
	// Max は取得する件数の上限である。
	Max int `yaml:"max"`
	// Order は並び順である。想定する値は "oldest_first" のみ。
	Order string `yaml:"order"`
}

// TrackerCommentsConfig は continuo とエージェントのあいだのコメントの取り決めである。
//
// **GitHub 固有ではない。**どのトラッカーを相手にしても、continuo は「先頭に印がある
// コメント」でエージェントの発言と自分の発言を見分ける。だから provider の下ではなく
// tracker の直下に置く。
type TrackerCommentsConfig struct {
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
	// Comments は GitHub からコメントを取ってくるときの件数と並び順である。
	Comments TrackerProviderCommentsConfig `yaml:"comments"`
}

// TrackerConfig は GitHub Projects v2 のボードをどう見るかを決める。
type TrackerConfig struct {
	// Kind はトラッカーの種別である。想定する値は "github_projects_v2" のみ。
	Kind string `yaml:"kind"`
	// Provider は GitHub Projects v2 アダプタ固有の設定である。
	Provider TrackerProviderConfig `yaml:"provider"`
	// Comments は continuo とエージェントのあいだのコメントの取り決めである（マーカー）。
	Comments TrackerCommentsConfig `yaml:"comments"`
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
	// UnknownStateGraceMs は「continuo が知らない Status」を見つけてから worker を止めるまでに
	// 置く猶予（ミリ秒）である（設計 3-49）。
	//
	// **エージェントが turn の最後に表明を書けば、continuo が正しい Status へ戻す。**
	// turn の途中で殺すと、その表明が読まれずに捨てられる。だから turn が動いている間は
	// この長さだけ待ち、turn の終わりの表明を読んでから判断する。
	//
	// **0 以下なら猶予を置かない**（見つけた巡回でそのまま止める）。
	// **turn が動いていなければ猶予は使わない**（待っても表明は出てこない）。
	UnknownStateGraceMs int `yaml:"unknown_state_grace_ms"`
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
//
// **置き場所の中の並べ方は gwq の4階層に固定である**（`<root>/<ホスト>/<owner>/<repo>/<branch>`。3-22）。
// 並べ方を選ぶ設定キーは持たない。値を見て処理を変える経路が無いので、
// 設定として置いても「書いた値と違う並べ方になる」ことしか起こらない。
type WorkspaceConfig struct {
	// Root は worktree を置く置き場所である。チルダは展開する（5-5）。
	Root string `yaml:"root"`
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
	//
	// **引かれるのは tracker.running_state（既定 "In Progress"）ただ1つである。**
	// これから dispatch する候補は running_state の枠を消費するものとして数えるため、
	// それ以外の Status 名を書いても参照されない（設計 3-16 の段-1）。
	//
	// **0 以下の値は起動時に弾く。**`In Progress: 0` と書くと空きスロットの判定が常に偽になり、
	// ボード全体の dispatch が永久に止まる。無人運用では、止まっていることに誰も気づけない。
	MaxConcurrentAgentsByState map[string]int `yaml:"max_concurrent_agents_by_state"`
	// MaxDispatchTurns は continuo が指示を送った回数の上限である（3-14）。
	//
	// **`SPEC.md` 5.3.5 の `max_turns` とは数えるものが違うので、同じ名前を使わない。**
	// 仕様の `max_turns` は「1つの worker セッション内でのコーディングエージェントの turn 数」だが、
	// continuo が数えるのは「continuo が指示を送った回数」である。Claude Code が
	// subagent の完了通知などで自分に投入した turn は数えない。
	MaxDispatchTurns int `yaml:"max_dispatch_turns"`
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
//
// **届け方は `--settings` で外部の設定ファイルを指す経路に固定である**（設計 3-12）。
// 届け方を選ぶ設定キーは持たない。"worktree_local"（worktree に
// .claude/settings.local.json を置く）は、置き場所・.git/info/exclude への登録・
// 片付けの仕様がどこにも無いので実装していない。
type ClaudeHookBridgeConfig struct {
	// Listen は hook を受ける socket の絶対パスである。null なら 3-23 の探索順で決める。
	Listen *string `yaml:"listen"`
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
	PollWaitMs int `yaml:"poll_wait_ms"`
	// SettleMs は、background_tasks が空の Stop を受けてから turn の終わりと確定するまでの
	// 猶予（ミリ秒）である（1-3 / 3-2）。空配列の Stop は turn の途中にも発火するため、
	// この時間のあいだ <task-notification> で始まる UserPromptSubmit が来ないことを確かめる。
	// 観測できた4件はいずれも 0.035 秒以内だったが上限は測れていない。運用のログで決め直す。
	SettleMs int `yaml:"settle_ms"`
	// WaitUntil は herdr agent prompt --wait に渡す状態の一覧である（3-2）。
	// blocked を外すと、権限の確認で止まった turn を拾えず時間切れまで待つことになる。
	//
	// **綴りは起動時に検査する。**受け付けるのは herdr の AgentStatus の値
	// （idle / working / blocked / done / unknown）だけである。herdr はこの文字列を
	// そのまま受け取るので、綴りを間違えても起動は通り、turn の終わりを拾えないまま
	// 時間切れまで待つことになる。
	WaitUntil []string `yaml:"wait_until"`
	// TurnTimeoutMs は turn が動いている間に許す「無音の間隔」の上限（ミリ秒）である。
	//
	// **turn の総実行時間の上限ではない。**`SPEC.md` 10.6 が
	// *"maximum silence interval while a turn stream is active; each app-server output
	// resets it, so it is not a total turn runtime cap"*（turn の流れが動いている間の
	// 最大の沈黙の間隔。app-server の出力ごとにリセットされる。総実行時間の上限ではない）
	// と定めている。1回の指示に数時間かかることは普通にあるので、総時間で測ってはならない。
	//
	// **continuo には app-server が無い。**「app-server の出力」に相当するのは
	// 「端末の画面が変わったこと」であり、herdr はそれを pane の revision（画面の版）で表す。
	// **版が増えていれば何時間かかっても待ち続け、版がこの時間だけ増えなければ打ち切る**（3-21）。
	//
	// **0 以下で打ち切りを行わない**（`SPEC.md` 8.4 の
	// *"If stall_timeout_ms <= 0, skip stall detection entirely"* に合わせる）。
	TurnTimeoutMs int `yaml:"turn_timeout_ms"`
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
	// ReadTimeoutMs は herdr の socket API の応答を待つ上限（ミリ秒）である。
	//
	// **`SPEC.md` 5.3.5 の `codex.read_timeout_ms` に相当するが、相手は app-server ではなく
	// herdr である**（設計 8-1）。continuo は app-server を持たないので、この時間で測るのは
	// 「herdr の socket が JSON を返してくるまで」だけである。
	//
	// **待ちを伴う呼び出しには適用しない**（agent の起動は StartupTimeoutMs、
	// turn の待ち受けは claude.turn_timeout_ms。「read_timeout_ms 一本ですべてを
	// 打ち切ってはならない」。設計 8-1）。
	ReadTimeoutMs int `yaml:"read_timeout_ms"`
	// StartupTimeoutMs は herdr の agent 起動を待つ時間の上限（ミリ秒）である。
	//
	// **`SPEC.md` 5.3.5 の `codex.startup_timeout_ms` に相当するが、相手は app-server ではなく
	// herdr である**（設計 8-1）。agent.start は実測で検知まで既定30秒かかるため、
	// ReadTimeoutMs（既定5秒）では必ず足りない。
	StartupTimeoutMs int `yaml:"startup_timeout_ms"`
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
	//
	// 想定する値は3つである。
	//
	//	claude_credentials … `~/.claude/.credentials.json` を読む
	//	keychain           … macOS の Keychain を `security` で読む（**macOS でだけ選べる**）
	//	env                … 下の TokenEnv に書いた環境変数を読む
	//
	// **既定は OS で分かれる**（macOS は keychain。default.go の defaultRateLimitTokenSource）。
	// macOS の Claude Code は資格情報を Keychain に置き、ファイルは無いのが普通である。
	//
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

// TrustConfig はリポジトリの信頼確認をどう扱うかを決める（3-11 / 3-33 / 4-3）。
type TrustConfig struct {
	// RequireRepoTrusted は dispatch の前にリポジトリの信頼登録を検査するかどうかである。
	RequireRepoTrusted bool `yaml:"require_repo_trusted"`
	// OnUntrusted は未信頼のリポジトリを見つけたときの扱いである。想定する値は "skip_and_comment" のみ。
	OnUntrusted string `yaml:"on_untrusted"`
	// Repositories は `continuo trust` が信頼を登録してよいリポジトリの列挙である（3-33）。
	// 要素は "owner/repo" の形で書く。
	//
	// **人間が書いたものだけを対象にする。**`continuo init` はボードから拾った一覧をここへ
	// 並べるが、**要らない行を消すのは人間である。**ボードは他人が編集できるので、
	// 拾った一覧をそのまま登録すると、issue を足せる人が信頼させるリポジトリを増やせてしまう。
	//
	// **巡回のループはここを読まない。**dispatch の直前の検査は `~/.claude.json` を
	// 読むだけであり（4-3）、この列挙を参照する経路を持たない。
	Repositories []string `yaml:"repositories"`
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
