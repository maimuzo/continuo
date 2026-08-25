package i18n

// このファイルは画面に出す文言のキーを1箇所に集めたものである。
//
// **キーはここでしか宣言しない。**呼ぶ側に文字列リテラルを書くと、打ち間違いが
// 実行するまで見つからない。**messages/ja.json と1対1で対応する**ことを
// test/internal/i18n が機械的に確かめている（宣言したのに文言が無い／
// 文言があるのに宣言が無い、のどちらも落とす）。
//
// キーの名前は "<画面>.<場所>" の形にする。"." より前が画面（doctor / cli / dashboard）、
// あとがその画面の中の場所である。

// doctor の見出し語（`continuo doctor` の記号の右に出る語）。
const (
	// KeyDoctorLabelConfig はWORKFLOW.md を読めたかの検査の見出し語に出る。
	KeyDoctorLabelConfig Key = "doctor.label.config"
	// KeyDoctorLabelHerdr はherdr の protocol を照合する検査の見出し語に出る。
	KeyDoctorLabelHerdr Key = "doctor.label.herdr"

	// KeyDoctorLabelClaude は doctor の「claude」の見出し語である。
	KeyDoctorLabelClaude Key = "doctor.label.claude"

	// KeyDoctorLabelRuntimeDir は doctor の「hook の置き場所」の見出し語である。
	KeyDoctorLabelRuntimeDir Key = "doctor.label.runtime_dir"
	// KeyDoctorRuntimeDirOK は hook の socket を用意できたときに出る。
	KeyDoctorRuntimeDirOK Key = "doctor.runtime_dir.ok"
	// KeyDoctorRuntimeDirFailed は hook の socket を用意できなかったときに出る。
	KeyDoctorRuntimeDirFailed Key = "doctor.runtime_dir.failed"
	// KeyDoctorRuntimeDirRemedy は hook の socket を用意できなかったときの直し方である。
	KeyDoctorRuntimeDirRemedy Key = "doctor.runtime_dir.remedy"

	// KeyDoctorClaudeNotFound は claude が PATH に無いときの文言である。
	KeyDoctorClaudeNotFound Key = "doctor.claude.not_found"

	// KeyDoctorClaudeFound は claude が見つかったときに、その場所を出す文言である。
	KeyDoctorClaudeFound Key = "doctor.claude.found"

	// KeyDoctorClaudeRemedyInstall は claude が無いときの直し方である。
	KeyDoctorClaudeRemedyInstall Key = "doctor.claude.remedy_install"
	// KeyDoctorLabelGHAuth はgh の scope を見る検査の見出し語に出る。
	KeyDoctorLabelGHAuth Key = "doctor.label.gh_auth"
	// KeyDoctorLabelBoard はボードを読む検査の見出し語に出る。
	KeyDoctorLabelBoard Key = "doctor.label.board"
	// KeyDoctorLabelClone はclone が手元にあるかの検査の見出し語に出る。
	KeyDoctorLabelClone Key = "doctor.label.clone"
	// KeyDoctorLabelTrust はリポジトリが承認済みかの検査の見出し語に出る。
	KeyDoctorLabelTrust Key = "doctor.label.trust"
	// KeyDoctorLabelCredentials は枠の判定に使う資格情報の検査の見出し語に出る。
	KeyDoctorLabelCredentials Key = "doctor.label.credentials"
	// KeyDoctorLabelClaudeHome は Claude Code の設定ディレクトリに書けるかの検査の見出し語である。
	KeyDoctorLabelClaudeHome Key = "doctor.label.claude_home"
	// KeyDoctorLabelWorkspaceRoot は worktree の置き場所に書けるかの検査の見出し語である。
	KeyDoctorLabelWorkspaceRoot Key = "doctor.label.workspace_root"
)

// doctor の集計の行（検査結果の末尾に出る1行）。
const (
	// KeyDoctorSummaryAllOK はすべて通ったときの集計に出る。
	KeyDoctorSummaryAllOK Key = "doctor.summary.all_ok"
	// KeyDoctorSummaryUnknownOnly は確かめられなかったものだけがあるときの集計に出る。
	KeyDoctorSummaryUnknownOnly Key = "doctor.summary.unknown_only"
	// KeyDoctorSummaryProblems は足りないものがあるときの集計に出る。
	KeyDoctorSummaryProblems Key = "doctor.summary.problems"
)

// doctor の検査「設定ファイル」。
const (
	// KeyDoctorConfigUnreadable はWORKFLOW.md を読めなかったときの説明に出る。
	KeyDoctorConfigUnreadable Key = "doctor.config.unreadable"
	// KeyDoctorConfigRemedyInit は設定ファイルが無かったときの直し方に出る。
	KeyDoctorConfigRemedyInit Key = "doctor.config.remedy_init"
	// KeyDoctorConfigRemedyPermission は権限が足りなくて読めなかったときの直し方に出る。
	KeyDoctorConfigRemedyPermission Key = "doctor.config.remedy_permission"
	// KeyDoctorConfigRemedyFrontMatter は読めたが front matter が通らなかったときの直し方に出る。
	KeyDoctorConfigRemedyFrontMatter Key = "doctor.config.remedy_front_matter"
	// KeyDoctorConfigOK は読めたときの説明に出る。
	KeyDoctorConfigOK Key = "doctor.config.ok"
)

// doctor の「ファイルシステムが壊れている」ときの案内（issue #11）。
//
// **EIO と EROFS を掴んだときだけ出す。**設定を作り直させる案内をしてはならない。
// 直る見込みのない作業を利用者にさせることになる。
const (
	// KeyDoctorFilesystemFault はファイルシステムの異常を掴んだときの理由に出る。
	KeyDoctorFilesystemFault Key = "doctor.filesystem.fault"
	// KeyDoctorFilesystemRemedyMount はマウントの状態を確かめる直し方に出る。
	KeyDoctorFilesystemRemedyMount Key = "doctor.filesystem.remedy_mount"
	// KeyDoctorFilesystemRemedyDmesg はカーネルログを確かめる直し方に出る。
	KeyDoctorFilesystemRemedyDmesg Key = "doctor.filesystem.remedy_dmesg"
	// KeyDoctorFilesystemRemedyDisk は Windows 側の空き容量を確かめる直し方に出る。
	KeyDoctorFilesystemRemedyDisk Key = "doctor.filesystem.remedy_disk"
	// KeyDoctorFilesystemRemedyRestart は WSL を止めて再起動する直し方に出る。
	KeyDoctorFilesystemRemedyRestart Key = "doctor.filesystem.remedy_restart"
	// KeyDoctorWriteRemedyPermission は書けなかった場所の所有者と権限を確かめる直し方に出る。
	KeyDoctorWriteRemedyPermission Key = "doctor.write.remedy_permission"
	// KeyDoctorDefaultUsed は設定を読めないまま既定値で確かめたときの理由に出る。
	KeyDoctorDefaultUsed Key = "doctor.default_used"
)

// doctor の検査「Claude の設定」（Claude Code の設定ディレクトリに書けるか）。
const (
	// KeyDoctorClaudeHomeOK は書けたときの説明に出る。
	KeyDoctorClaudeHomeOK Key = "doctor.claude_home.ok"
	// KeyDoctorClaudeHomeFailed は書けなかったときの説明に出る。
	KeyDoctorClaudeHomeFailed Key = "doctor.claude_home.failed"
	// KeyDoctorClaudeHomeReason は、なぜここが書けないと困るのかの説明に出る。
	KeyDoctorClaudeHomeReason Key = "doctor.claude_home.reason"
)

// doctor の検査「worktree の場所」（workspace.root に書けるか）。
const (
	// KeyDoctorWorkspaceRootOK は書けたときの説明に出る。
	KeyDoctorWorkspaceRootOK Key = "doctor.workspace_root.ok"
	// KeyDoctorWorkspaceRootFailed は書けなかったときの説明に出る。
	KeyDoctorWorkspaceRootFailed Key = "doctor.workspace_root.failed"
	// KeyDoctorWorkspaceRootConfigUnreadable は設定を読めず置き場所が決まらないときの説明に出る。
	KeyDoctorWorkspaceRootConfigUnreadable Key = "doctor.workspace_root.config_unreadable"
	// KeyDoctorWorkspaceRootReason は、なぜここが書けないと困るのかの説明に出る。
	KeyDoctorWorkspaceRootReason Key = "doctor.workspace_root.reason"
)

// internal/fsprobe（その場所に本当に書けるかを実際に書いて確かめる）。
const (
	// KeyFsprobeDirEmpty は確かめる場所が空文字だったときのエラーに出る。
	KeyFsprobeDirEmpty Key = "fsprobe.dir_empty"
	// KeyFsprobeDirNotAbsolute は確かめる場所が絶対パスでなかったときのエラーに出る。
	KeyFsprobeDirNotAbsolute Key = "fsprobe.dir_not_absolute"
	// KeyFsprobeMkdirFailed は場所そのものを用意できなかったときのエラーに出る。
	KeyFsprobeMkdirFailed Key = "fsprobe.mkdir_failed"
	// KeyFsprobeWriteFailed は使い捨てのディレクトリを作れなかったときのエラーに出る。
	KeyFsprobeWriteFailed Key = "fsprobe.write_failed"
	// KeyFsprobeCleanupFailed は使い捨てのディレクトリを消せなかったときのエラーに出る。
	KeyFsprobeCleanupFailed Key = "fsprobe.cleanup_failed"
	// KeyFsprobeHomeDirFailed はホームディレクトリを特定できなかったときのエラーに出る。
	KeyFsprobeHomeDirFailed Key = "fsprobe.home_dir_failed"
	// KeyFsprobeClaudeHomeFailed は Claude Code の設定ディレクトリに書けなかったときのエラーに出る。
	KeyFsprobeClaudeHomeFailed Key = "fsprobe.claude_home_failed"
	// KeyFsprobeWorkspaceRootFailed は worktree の置き場所に書けなかったときのエラーに出る。
	KeyFsprobeWorkspaceRootFailed Key = "fsprobe.workspace_root_failed"
)

// doctor の検査「herdr」。
const (
	// KeyDoctorHerdrConfigUnreadable は上流の設定ファイルが落ちたときの説明に出る。
	KeyDoctorHerdrConfigUnreadable Key = "doctor.herdr.config_unreadable"
	// KeyDoctorHerdrSocketUnresolved はsocket のパスを決められなかったときの説明に出る。
	KeyDoctorHerdrSocketUnresolved Key = "doctor.herdr.socket_unresolved"
	// KeyDoctorHerdrRemedySocketAbs は同じときの直し方に出る。
	KeyDoctorHerdrRemedySocketAbs Key = "doctor.herdr.remedy_socket_abs"
	// KeyDoctorHerdrTimeout は期限内に ping が返らなかったときの説明に出る。
	KeyDoctorHerdrTimeout Key = "doctor.herdr.timeout"
	// KeyDoctorHerdrRemedyTimeout は同じときの直し方に出る。
	KeyDoctorHerdrRemedyTimeout Key = "doctor.herdr.remedy_timeout"
	// KeyDoctorHerdrRemedyNotListening はsocket へ届かなかったときの直し方に出る。
	KeyDoctorHerdrRemedyNotListening Key = "doctor.herdr.remedy_not_listening"
	// KeyDoctorHerdrRemedyProtocol はprotocol が食い違ったときの直し方に出る。
	KeyDoctorHerdrRemedyProtocol Key = "doctor.herdr.remedy_protocol"
	// KeyDoctorHerdrOK は一致したときの説明に出る。
	KeyDoctorHerdrOK Key = "doctor.herdr.ok"
)

// doctor の検査「gh の認証」。
const (
	// KeyDoctorGHAuthConfigUnreadable は上流の設定ファイルが落ちたときの説明に出る。
	KeyDoctorGHAuthConfigUnreadable Key = "doctor.gh_auth.config_unreadable"
	// KeyDoctorGHAuthRemedyFixConfig は同じときの直し方に出る。
	KeyDoctorGHAuthRemedyFixConfig Key = "doctor.gh_auth.remedy_fix_config"
	// KeyDoctorGHAuthRemedyInstall はgh が無いときの直し方に出る。
	KeyDoctorGHAuthRemedyInstall Key = "doctor.gh_auth.remedy_install"
	// KeyDoctorGHAuthTimeout は期限内に gh が返らなかったときの説明に出る。
	KeyDoctorGHAuthTimeout Key = "doctor.gh_auth.timeout"
	// KeyDoctorGHAuthRemedyTimeout は同じときの直し方に出る。
	KeyDoctorGHAuthRemedyTimeout Key = "doctor.gh_auth.remedy_timeout"
	// KeyDoctorGHAuthRemedyLogin はscope が足りないときの直し方に出る。
	KeyDoctorGHAuthRemedyLogin Key = "doctor.gh_auth.remedy_login"
	// KeyDoctorGHAuthOK は通ったときの説明に出る。
	KeyDoctorGHAuthOK Key = "doctor.gh_auth.ok"
)

// doctor の検査「ボード」。
const (
	// KeyDoctorBoardConfigUnreadable は上流の設定ファイルが落ちたときの説明に出る。
	KeyDoctorBoardConfigUnreadable Key = "doctor.board.config_unreadable"
	// KeyDoctorBoardGHMissing は上流の gh の認証が足りなかったときの説明に出る。
	KeyDoctorBoardGHMissing Key = "doctor.board.gh_missing"
	// KeyDoctorBoardGHUnknown は上流の gh の認証を確かめられなかったときの説明に出る。
	KeyDoctorBoardGHUnknown Key = "doctor.board.gh_unknown"
	// KeyDoctorBoardTokenUnresolved はトークンを取れなかったときの説明に出る。
	KeyDoctorBoardTokenUnresolved Key = "doctor.board.token_unresolved"
	// KeyDoctorBoardRemedyTokenSource は同じときの直し方に出る。
	KeyDoctorBoardRemedyTokenSource Key = "doctor.board.remedy_token_source"
	// KeyDoctorBoardAdapterFailed はアダプタを作れなかったときの説明に出る。
	KeyDoctorBoardAdapterFailed Key = "doctor.board.adapter_failed"
	// KeyDoctorBoardRemedyTracker は同じときの直し方に出る。
	KeyDoctorBoardRemedyTracker Key = "doctor.board.remedy_tracker"
	// KeyDoctorBoardWhatBootstrap は何をしようとして落ちたかの語（Bootstrap）に出る。
	KeyDoctorBoardWhatBootstrap Key = "doctor.board.what_bootstrap"
	// KeyDoctorBoardWhatFetchIssues は何をしようとして落ちたかの語（候補の取得）に出る。
	KeyDoctorBoardWhatFetchIssues Key = "doctor.board.what_fetch_issues"
	// KeyDoctorBoardOK は読めたときの説明に出る。
	KeyDoctorBoardOK Key = "doctor.board.ok"
	// KeyDoctorBoardEndpointNote は接続先を差し替えているときに添える1行に出る。
	KeyDoctorBoardEndpointNote Key = "doctor.board.endpoint_note"
	// KeyDoctorBoardTimeout は期限内に返らなかったときの説明に出る。
	KeyDoctorBoardTimeout Key = "doctor.board.timeout"
	// KeyDoctorBoardRemedyRetryConnection は同じときの直し方に出る。
	KeyDoctorBoardRemedyRetryConnection Key = "doctor.board.remedy_retry_connection"
	// KeyDoctorBoardRateLimited はレートリミットに当たったときの説明に出る。
	KeyDoctorBoardRateLimited Key = "doctor.board.rate_limited"
	// KeyDoctorBoardRemedyWait は同じときの直し方に出る。
	KeyDoctorBoardRemedyWait Key = "doctor.board.remedy_wait"
	// KeyDoctorBoardRemedyProvider は落ち方が分からないときの直し方に出る。
	KeyDoctorBoardRemedyProvider Key = "doctor.board.remedy_provider"
	// KeyDoctorBoardRemedyStatusOptions は選択肢名が食い違ったときの直し方に出る。
	KeyDoctorBoardRemedyStatusOptions Key = "doctor.board.remedy_status_options"
	// KeyDoctorBoardRemedyTokenInvalid はトークンが失効していたときの直し方に出る。
	KeyDoctorBoardRemedyTokenInvalid Key = "doctor.board.remedy_token_invalid"
	// KeyDoctorBoardFailed はそのほかの理由で落ちたときの説明に出る。
	KeyDoctorBoardFailed Key = "doctor.board.failed"
)

// doctor の検査「clone」。
const (
	// KeyDoctorCloneBinNotFound は ghq / git が PATH に無いときの説明に出る。
	KeyDoctorCloneBinNotFound Key = "doctor.clone.bin_not_found"
	// KeyDoctorCloneRemedyInstallBin は同じときの直し方に出る。
	KeyDoctorCloneRemedyInstallBin Key = "doctor.clone.remedy_install_bin"
	// KeyDoctorCloneBoardUnreadable は上流のボードが落ちたときの説明に出る。
	KeyDoctorCloneBoardUnreadable Key = "doctor.clone.board_unreadable"
	// KeyDoctorCloneNoTargets はボードが空のときの説明に出る。
	KeyDoctorCloneNoTargets Key = "doctor.clone.no_targets"
	// KeyDoctorCloneNoteGhqFailed はghq を起動できなかったリポジトリの内訳に出る。
	KeyDoctorCloneNoteGhqFailed Key = "doctor.clone.note_ghq_failed"
	// KeyDoctorCloneNoteMissing はclone が無いリポジトリの内訳に出る。
	KeyDoctorCloneNoteMissing Key = "doctor.clone.note_missing"
	// KeyDoctorCloneRemedyGhqGet は同じときの直し方に出る。
	KeyDoctorCloneRemedyGhqGet Key = "doctor.clone.remedy_ghq_get"
	// KeyDoctorCloneNoteFound はclone が見つかったリポジトリの内訳に出る。
	KeyDoctorCloneNoteFound Key = "doctor.clone.note_found"
	// KeyDoctorCloneDetailOK はすべて揃っているときの説明に出る。
	KeyDoctorCloneDetailOK Key = "doctor.clone.detail_ok"
	// KeyDoctorCloneDetailMissing は足りないものがあるときの説明に出る。
	KeyDoctorCloneDetailMissing Key = "doctor.clone.detail_missing"
	// KeyDoctorCloneDetailUnknown は確かめられなかったものがあるときの説明に出る。
	KeyDoctorCloneDetailUnknown Key = "doctor.clone.detail_unknown"
)

// doctor の内訳の件数に添える語。
const (
	// KeyDoctorCountUnknownSuffix は足りないものと確かめられなかったものが混ざったときに添える語に出る。
	KeyDoctorCountUnknownSuffix Key = "doctor.count.unknown_suffix"
)

// doctor の検査「信頼登録」。
const (
	// KeyDoctorTrustBoardUnreadable は上流のボードが落ちたときの説明に出る。
	KeyDoctorTrustBoardUnreadable Key = "doctor.trust.board_unreadable"
	// KeyDoctorTrustNoTargets はボードが空のときの説明に出る。
	KeyDoctorTrustNoTargets Key = "doctor.trust.no_targets"
	// KeyDoctorTrustHomeUnresolved はホームディレクトリを決められなかったときの説明に出る。
	KeyDoctorTrustHomeUnresolved Key = "doctor.trust.home_unresolved"
	// KeyDoctorTrustNoteNoClone はclone が無いリポジトリの内訳に出る。
	KeyDoctorTrustNoteNoClone Key = "doctor.trust.note_no_clone"
	// KeyDoctorTrustNoteUndecidable は判定できなかったリポジトリの内訳に出る。
	KeyDoctorTrustNoteUndecidable Key = "doctor.trust.note_undecidable"
	// KeyDoctorTrustNoteReason は判定できたリポジトリの内訳に出る。
	KeyDoctorTrustNoteReason Key = "doctor.trust.note_reason"
	// KeyDoctorTrustRemedyRunTrust は未承認のときの直し方に出る。
	KeyDoctorTrustRemedyRunTrust Key = "doctor.trust.remedy_run_trust"
	// KeyDoctorTrustDetailOK はすべて承認済みのときの説明に出る。
	KeyDoctorTrustDetailOK Key = "doctor.trust.detail_ok"
	// KeyDoctorTrustDetailMissing は未承認があるときの説明に出る。
	KeyDoctorTrustDetailMissing Key = "doctor.trust.detail_missing"
	// KeyDoctorTrustDetailUnknown は確かめられなかったものがあるときの説明に出る。
	KeyDoctorTrustDetailUnknown Key = "doctor.trust.detail_unknown"
)

// doctor の検査「資格情報」。
const (
	// KeyDoctorCredentialsConfigUnreadable は上流の設定ファイルが落ちたときの説明に出る。
	KeyDoctorCredentialsConfigUnreadable Key = "doctor.credentials.config_unreadable"
	// KeyDoctorCredentialsRemedyFixConfig は同じときの直し方に出る。
	KeyDoctorCredentialsRemedyFixConfig Key = "doctor.credentials.remedy_fix_config"
	// KeyDoctorCredentialsNone は枠の判定を行わない設定のときの説明に出る。
	KeyDoctorCredentialsNone Key = "doctor.credentials.none"
	// KeyDoctorCredentialsTokenEnvEmpty は読む環境変数名が空のときの説明に出る。
	KeyDoctorCredentialsTokenEnvEmpty Key = "doctor.credentials.token_env_empty"
	// KeyDoctorCredentialsRemedyTokenEnv は同じときの直し方に出る。
	KeyDoctorCredentialsRemedyTokenEnv Key = "doctor.credentials.remedy_token_env"
	// KeyDoctorCredentialsEnvOK は環境変数から取れたときの説明に出る。
	KeyDoctorCredentialsEnvOK Key = "doctor.credentials.env_ok"
	// KeyDoctorCredentialsEnvMissing は環境変数が無いときの説明に出る。
	KeyDoctorCredentialsEnvMissing Key = "doctor.credentials.env_missing"
	// KeyDoctorCredentialsRemedySetEnv は同じときの直し方に出る。
	KeyDoctorCredentialsRemedySetEnv Key = "doctor.credentials.remedy_set_env"
	// KeyDoctorCredentialsHomeUnresolved はホームディレクトリを決められなかったときの説明に出る。
	KeyDoctorCredentialsHomeUnresolved Key = "doctor.credentials.home_unresolved"
	// KeyDoctorCredentialsFileFound は資格情報のファイルがあったときの説明に出る。
	KeyDoctorCredentialsFileFound Key = "doctor.credentials.file_found"
	// KeyDoctorCredentialsFileMissing は資格情報のファイルが無いときの説明に出る。
	KeyDoctorCredentialsFileMissing Key = "doctor.credentials.file_missing"
	// KeyDoctorCredentialsRemedySkipped は同じときの案内に出る。
	KeyDoctorCredentialsRemedySkipped Key = "doctor.credentials.remedy_skipped"
	// KeyDoctorCredentialsKeychainOK はKeychain から読めたときの説明に出る。
	KeyDoctorCredentialsKeychainOK Key = "doctor.credentials.keychain_ok"
	// KeyDoctorCredentialsKeychainFailed はKeychain を読めなかったときの説明に出る。
	KeyDoctorCredentialsKeychainFailed Key = "doctor.credentials.keychain_failed"
	// KeyDoctorCredentialsRemedyKeychain は同じときの直し方に出る。
	KeyDoctorCredentialsRemedyKeychain Key = "doctor.credentials.remedy_keychain"
	// KeyDoctorCredentialsKeychainTimeout はKeychain の読み取りが期限内に終わらなかったときの説明に出る。
	KeyDoctorCredentialsKeychainTimeout Key = "doctor.credentials.keychain_timeout"
	// KeyDoctorCredentialsKeychainNoAccessToken はKeychain は読めたが accessToken が無いときの説明に出る。
	KeyDoctorCredentialsKeychainNoAccessToken Key = "doctor.credentials.keychain_no_access_token"
	// KeyDoctorCredentialsRemedyKeychainTimeout は同じときの直し方に出る。
	KeyDoctorCredentialsRemedyKeychainTimeout Key = "doctor.credentials.remedy_keychain_timeout"
	// KeyDoctorCredentialsRemedyUseKeychain は資格情報のファイルが無い macOS で、
	// Keychain へ切り替える案内に出る。
	KeyDoctorCredentialsRemedyUseKeychain Key = "doctor.credentials.remedy_use_keychain"
)

// CLI の共通の文言（複数のサブコマンドが同じ文面を出す）。
const (
	// KeyCLIErrGetwd は作業ディレクトリを引けなかったときに出る。
	KeyCLIErrGetwd Key = "cli.err_getwd"
	// KeyCLIErrResolveConfigPath はWORKFLOW.md の場所を決められなかったときに出る。
	KeyCLIErrResolveConfigPath Key = "cli.err_resolve_config_path"
	// KeyCLIErrLoadConfig はWORKFLOW.md を読めなかったときに出る。
	KeyCLIErrLoadConfig Key = "cli.err_load_config"
	// KeyCLIErrGeneric は理由をそのまま出すときに出る。
	KeyCLIErrGeneric Key = "cli.err_generic"
)

// `continuo init` の文言。
const (
	// KeyCLIInitFlagForce は--force の説明に出る。
	KeyCLIInitFlagForce Key = "cli.init.flag_force"
	// KeyCLIInitFlagOwner は--owner の説明に出る。
	KeyCLIInitFlagOwner Key = "cli.init.flag_owner"
	// KeyCLIInitFlagProject は--project の説明に出る。
	KeyCLIInitFlagProject Key = "cli.init.flag_project"
	// KeyCLIInitErrOwnerInvalid は--owner の値が形として不正なときに出る。
	KeyCLIInitErrOwnerInvalid Key = "cli.init.err_owner_invalid"
	// KeyCLIInitErrProjectPositive は--project の値が0以下のときに出る。
	KeyCLIInitErrProjectPositive Key = "cli.init.err_project_positive"
	// KeyCLIInitErrTooManyPositional は位置引数が2つ以上あるときに出る。
	KeyCLIInitErrTooManyPositional Key = "cli.init.err_too_many_positional"
	// KeyCLIInitOverwritten は--force で上書きしたときに出る。
	KeyCLIInitOverwritten Key = "cli.init.overwritten"
	// KeyCLIInitCreated は新しく書き出したときに出る。
	KeyCLIInitCreated Key = "cli.init.created"
	// KeyCLIInitErrAlreadyExists は既に WORKFLOW.md があるときに出る。
	KeyCLIInitErrAlreadyExists Key = "cli.init.err_already_exists"
	// KeyCLIInitErrDirNotFound は置き場所のディレクトリが無いときに出る。
	KeyCLIInitErrDirNotFound Key = "cli.init.err_dir_not_found"
	// KeyCLIInitErrNotADirectory は置き場所がディレクトリでないときに出る。
	KeyCLIInitErrNotADirectory Key = "cli.init.err_not_a_directory"
	// KeyCLIInitErrSymlink は置き場所が symlink のときに出る。
	KeyCLIInitErrSymlink Key = "cli.init.err_symlink"
	// KeyCLIInitErrWriteFailed はそのほかの理由で書き出せないときに出る。
	KeyCLIInitErrWriteFailed Key = "cli.init.err_write_failed"
	// KeyCLIInitDetectFilled は雛形の値を埋められたときの1行に出る。
	KeyCLIInitDetectFilled Key = "cli.init.detect_filled"
	// KeyCLIInitDetectUnfilled は雛形の値を埋められなかったときの1行に出る。
	KeyCLIInitDetectUnfilled Key = "cli.init.detect_unfilled"
	// KeyCLIInitDetectCandidate は候補のボードを並べる1行に出る。
	KeyCLIInitDetectCandidate Key = "cli.init.detect_candidate"
	// KeyCLIInitDetectAdvice はそのあとに人間がやることの案内に出る。
	KeyCLIInitDetectAdvice Key = "cli.init.detect_advice"
	// KeyCLIInitDetectPlaceholderNote は埋まらなかった値があるときの締めの1行に出る。
	KeyCLIInitDetectPlaceholderNote Key = "cli.init.detect_placeholder_note"
)

// `continuo setup` が5つの役割を説明するときの文言。
//
// **役割の名前より説明が先に出る。**初見の利用者は「どの Status がどの役割か」を
// 知らないので、Status の名前で尋ねても選べない（RUCM の判断11）。
const (
	// KeySetupRoleDispatchDesc は着手待ちの役割の説明に出る。
	KeySetupRoleDispatchDesc Key = "setup.role.dispatch_desc"
	// KeySetupRoleRunningDesc は作業中の役割の説明に出る。
	KeySetupRoleRunningDesc Key = "setup.role.running_desc"
	// KeySetupRoleReviewDesc はレビュー待ちの役割の説明に出る。
	KeySetupRoleReviewDesc Key = "setup.role.review_desc"
	// KeySetupRoleBlockedDesc は保留の役割の説明に出る。
	KeySetupRoleBlockedDesc Key = "setup.role.blocked_desc"
	// KeySetupRoleDoneDesc は完了の役割の説明に出る。
	KeySetupRoleDoneDesc Key = "setup.role.done_desc"
)

// `continuo setup` の対話の文言。
const (
	// KeySetupPromptOptionsHeader は選択肢の一覧の見出しに出る。
	KeySetupPromptOptionsHeader Key = "setup.prompt.options_header"
	// KeySetupPromptOptionLine は選択肢1つを番号付きで並べる行に出る。
	KeySetupPromptOptionLine Key = "setup.prompt.option_line"
	// KeySetupPromptIntroCount はこれから何回尋ねるかの案内に出る。
	KeySetupPromptIntroCount Key = "setup.prompt.intro_count"
	// KeySetupPromptIntroZero は番号 0 の意味の案内に出る。
	KeySetupPromptIntroZero Key = "setup.prompt.intro_zero"
	// KeySetupPromptIntroInterrupt はCtrl+C で中断できることの案内に出る。
	KeySetupPromptIntroInterrupt Key = "setup.prompt.intro_interrupt"
	// KeySetupPromptAsk はいま割り当てる役割の説明に出る。
	KeySetupPromptAsk Key = "setup.prompt.ask"
	// KeySetupPromptInput は番号の入力を待つ行に出る。
	KeySetupPromptInput Key = "setup.prompt.input"
	// KeySetupPromptAssigned は割り当てが決まったときに出る。
	KeySetupPromptAssigned Key = "setup.prompt.assigned"
	// KeySetupErrNotANumber は入力が番号でなかったときに出る。
	KeySetupErrNotANumber Key = "setup.err.not_a_number"
	// KeySetupErrOutOfRange は番号が一覧の範囲外だったときに出る。
	KeySetupErrOutOfRange Key = "setup.err.out_of_range"
	// KeySetupErrDuplicate は選んだ選択肢が別の役割に割り当て済みのときに出る。
	KeySetupErrDuplicate Key = "setup.err.duplicate"
	// KeySetupSummaryHeader は5つの割り当ての一覧の見出しに出る。
	KeySetupSummaryHeader Key = "setup.summary.header"
	// KeySetupSummaryLine は割り当て1件を並べる行に出る。
	KeySetupSummaryLine Key = "setup.summary.line"
	// KeySetupAbortTooFew は選択肢が5個に満たないときに出る。
	KeySetupAbortTooFew Key = "setup.abort.too_few"
	// KeySetupAbortNoOption は番号 0 が入力されたときに出る。
	KeySetupAbortNoOption Key = "setup.abort.no_option"
	// KeySetupAbortRemedyUI は選択肢を GitHub の画面から足す手順に出る。
	KeySetupAbortRemedyUI Key = "setup.abort.remedy_ui"
	// KeySetupAbortRemedyNoAPI はAPI で選択肢を足してはならない理由に出る。
	KeySetupAbortRemedyNoAPI Key = "setup.abort.remedy_no_api"
	// KeySetupAbortInterrupted はCtrl+C で中断したときに出る。
	KeySetupAbortInterrupted Key = "setup.abort.interrupted"
	// KeySetupAbortInputClosed は入力が終わってしまったときに出る。
	KeySetupAbortInputClosed Key = "setup.abort.input_closed"
)

// `continuo setup` の CLI の文言（対話の中身は setup.* にある）。
const (
	// KeyCLISetupFlagOwner は--owner の説明に出る。
	KeyCLISetupFlagOwner Key = "cli.setup.flag_owner"
	// KeyCLISetupFlagProject は--project の説明に出る。
	KeyCLISetupFlagProject Key = "cli.setup.flag_project"
	// KeyCLISetupFlagStatusField は--status-field の説明に出る。
	KeyCLISetupFlagStatusField Key = "cli.setup.flag_status_field"
	// KeyCLISetupErrOwnerInvalid は--owner の値が形として不正なときに出る。
	KeyCLISetupErrOwnerInvalid Key = "cli.setup.err_owner_invalid"
	// KeyCLISetupErrProjectPositive は--project の値が0以下のときに出る。
	KeyCLISetupErrProjectPositive Key = "cli.setup.err_project_positive"
	// KeyCLISetupErrStatusFieldEmpty は--status-field が空のときに出る。
	KeyCLISetupErrStatusFieldEmpty Key = "cli.setup.err_status_field_empty"
	// KeyCLISetupErrTooManyPositional は位置引数が2つ以上あるときに出る。
	KeyCLISetupErrTooManyPositional Key = "cli.setup.err_too_many_positional"
	// KeyCLISetupErrNotFound は書き換える WORKFLOW.md が無いときに出る。
	KeyCLISetupErrNotFound Key = "cli.setup.err_not_found"
	// KeyCLISetupErrNotFoundRemedy は同じときの直し方に出る。
	KeyCLISetupErrNotFoundRemedy Key = "cli.setup.err_not_found_remedy"
	// KeyCLISetupErrKeysNotFound は書き換える対象のキーが WORKFLOW.md に無いときに出る。
	KeyCLISetupErrKeysNotFound Key = "cli.setup.err_keys_not_found"
	// KeyCLISetupErrDirNotFound は置き場所のディレクトリが無いときに出る。
	KeyCLISetupErrDirNotFound Key = "cli.setup.err_dir_not_found"
	// KeyCLISetupErrNotADirectory は置き場所がディレクトリでないときに出る。
	KeyCLISetupErrNotADirectory Key = "cli.setup.err_not_a_directory"
	// KeyCLISetupErrSymlink は置き場所が symlink のときに出る。
	KeyCLISetupErrSymlink Key = "cli.setup.err_symlink"
	// KeyCLISetupErrWriteFailed はそのほかの理由で書き出せないときに出る。
	KeyCLISetupErrWriteFailed Key = "cli.setup.err_write_failed"
	// KeyCLISetupUpdated はStatus の割り当てを書き換えたときに出る。
	KeyCLISetupUpdated Key = "cli.setup.updated"
	// KeyCLISetupUpdatedKeysNote は書き換えたのがどのキーだけかの説明に出る。
	KeyCLISetupUpdatedKeysNote Key = "cli.setup.updated_keys_note"
	// KeyCLISetupUpdatedKey は書き換えたキーを1つずつ並べる行に出る。
	KeyCLISetupUpdatedKey Key = "cli.setup.updated_key"
	// KeyCLISetupBoardErrOwner はowner を引けなかったときに出る。
	KeyCLISetupBoardErrOwner Key = "cli.setup.board_err_owner"
	// KeyCLISetupBoardRemedyOwner は同じときの直し方に出る。
	KeyCLISetupBoardRemedyOwner Key = "cli.setup.board_remedy_owner"
	// KeyCLISetupBoardErrProject はボードの番号が決まらなかったときに出る。
	KeyCLISetupBoardErrProject Key = "cli.setup.board_err_project"
	// KeyCLISetupBoardRemedyProject は同じときの直し方に出る。
	KeyCLISetupBoardRemedyProject Key = "cli.setup.board_remedy_project"
	// KeyCLISetupBoardCandidate は候補のボードを並べる1行に出る。
	KeyCLISetupBoardCandidate Key = "cli.setup.board_candidate"
	// KeyCLISetupBoardErr はボードの Status フィールドを読めなかった理由に出る。
	KeyCLISetupBoardErr Key = "cli.setup.board_err"
	// KeyCLISetupBoardRemedyScope はgh の scope に project が無いときの直し方に出る。
	KeyCLISetupBoardRemedyScope Key = "cli.setup.board_remedy_scope"
	// KeyCLISetupBoardRemedyStatusField はStatus フィールドが見つからないときの直し方に出る。
	KeyCLISetupBoardRemedyStatusField Key = "cli.setup.board_remedy_status_field"
	// KeyCLISetupBoardRemedyRateLimited はレートリミットに当たったときの直し方に出る。
	KeyCLISetupBoardRemedyRateLimited Key = "cli.setup.board_remedy_rate_limited"
	// KeyCLISetupBoardRemedyGeneric は上のどれにも当てはまらないときの直し方に出る。
	KeyCLISetupBoardRemedyGeneric Key = "cli.setup.board_remedy_generic"
)

// `continuo trust` の文言。
const (
	// KeyCLITrustFlagDryRun は--dry-run の説明に出る。
	KeyCLITrustFlagDryRun Key = "cli.trust.flag_dry_run"
	// KeyCLITrustErrTooManyPositional は位置引数が2つ以上あるときに出る。
	KeyCLITrustErrTooManyPositional Key = "cli.trust.err_too_many_positional"
	// KeyCLITrustFetchingClone は clone を取りに行く直前に出る。
	KeyCLITrustFetchingClone Key = "cli.trust.fetching_clone"
	// KeyCLITrustFetchedClone は clone を取り終えた直後に出る。
	KeyCLITrustFetchedClone Key = "cli.trust.fetched_clone"
	// KeyCLITrustErrHomeDir はホームディレクトリを引けなかったときに出る。
	KeyCLITrustErrHomeDir Key = "cli.trust.err_home_dir"
	// KeyCLITrustErrPlan は登録の対象を調べられなかったときに出る。
	KeyCLITrustErrPlan Key = "cli.trust.err_plan"
	// KeyCLITrustErrWriteRequirements は要求内容を出せなかったときに出る。
	KeyCLITrustErrWriteRequirements Key = "cli.trust.err_write_requirements"
	// KeyCLITrustDryRunNote は--dry-run の締めの1行に出る。
	KeyCLITrustDryRunNote Key = "cli.trust.dry_run_note"
	// KeyCLITrustWarnConcurrent は書き込む前の警告の1行目に出る。
	KeyCLITrustWarnConcurrent Key = "cli.trust.warn_concurrent"
	// KeyCLITrustWarnCloseClaude は書き込む前の警告の2行目に出る。
	KeyCLITrustWarnCloseClaude Key = "cli.trust.warn_close_claude"
	// KeyCLITrustErrApply は登録に失敗したときに出る。
	KeyCLITrustErrApply Key = "cli.trust.err_apply"
	// KeyCLITrustErrWriteResult は結果を出せなかったときに出る。
	KeyCLITrustErrWriteResult Key = "cli.trust.err_write_result"
)

// `continuo doctor` の文言（検査の中身は doctor.* にある）。
const (
	// KeyCLIDoctorErrTooManyPositional は位置引数が2つ以上あるときに出る。
	KeyCLIDoctorErrTooManyPositional Key = "cli.doctor.err_too_many_positional"
	// KeyCLIDoctorWarnPathUnresolved はWORKFLOW.md の場所を決められなかったときに出る。
	KeyCLIDoctorWarnPathUnresolved Key = "cli.doctor.warn_path_unresolved"
	// KeyCLIDoctorErrWriteReport は検査結果を出せなかったときに出る。
	KeyCLIDoctorErrWriteReport Key = "cli.doctor.err_write_report"
)

// `continuo allow-keychain-access` の文言（macOS の Keychain へのアクセスを1回許可させる）。
//
// **失敗の案内は「何が起きたか・確かめ方・よくある原因・対処」の4行で書く**（設計 3-34b）。
const (
	// KeyCLIAllowKeychainAccessErrTooManyPositional は位置引数が1つ以上あるときに出る。
	KeyCLIAllowKeychainAccessErrTooManyPositional Key = "cli.allow_keychain_access.err_too_many_positional"
	// KeyCLIAllowKeychainAccessNotDarwin はmacOS 以外で実行したときに出る。
	KeyCLIAllowKeychainAccessNotDarwin Key = "cli.allow_keychain_access.not_darwin"
	// KeyCLIAllowKeychainAccessBefore は読みに行く直前の案内に出る。
	KeyCLIAllowKeychainAccessBefore Key = "cli.allow_keychain_access.before"
	// KeyCLIAllowKeychainAccessBeforeDialog は同じ案内の2行目（ダイアログの答え方）に出る。
	KeyCLIAllowKeychainAccessBeforeDialog Key = "cli.allow_keychain_access.before_dialog"
	// KeyCLIAllowKeychainAccessOK は読めたときの1行目に出る。
	KeyCLIAllowKeychainAccessOK Key = "cli.allow_keychain_access.ok"
	// KeyCLIAllowKeychainAccessFields は読めた項目の名前を並べる行に出る。**値は出さない。**
	KeyCLIAllowKeychainAccessFields Key = "cli.allow_keychain_access.fields"
	// KeyCLIAllowKeychainAccessNoAccessToken は読めたが accessToken が無いときの1行目に出る。
	KeyCLIAllowKeychainAccessNoAccessToken Key = "cli.allow_keychain_access.no_access_token"
	// KeyCLIAllowKeychainAccessErrHeadline は読めなかったときの1行目に出る。
	KeyCLIAllowKeychainAccessErrHeadline Key = "cli.allow_keychain_access.err_headline"
	// KeyCLIAllowKeychainAccessErrHowTo は同じときの【確かめ方】に出る。
	KeyCLIAllowKeychainAccessErrHowTo Key = "cli.allow_keychain_access.err_how_to"
	// KeyCLIAllowKeychainAccessErrCauses は同じときの【よくある原因】に出る。
	KeyCLIAllowKeychainAccessErrCauses Key = "cli.allow_keychain_access.err_causes"
	// KeyCLIAllowKeychainAccessErrRemedy は同じときの【対処】に出る。
	KeyCLIAllowKeychainAccessErrRemedy Key = "cli.allow_keychain_access.err_remedy"
	// KeyCLIAllowKeychainAccessTimeoutHeadline は期限内に返らなかったときの1行目に出る。
	KeyCLIAllowKeychainAccessTimeoutHeadline Key = "cli.allow_keychain_access.timeout_headline"
	// KeyCLIAllowKeychainAccessTimeoutHowTo は同じときの【確かめ方】に出る。
	KeyCLIAllowKeychainAccessTimeoutHowTo Key = "cli.allow_keychain_access.timeout_how_to"
	// KeyCLIAllowKeychainAccessTimeoutCauses は同じときの【よくある原因】に出る。
	KeyCLIAllowKeychainAccessTimeoutCauses Key = "cli.allow_keychain_access.timeout_causes"
	// KeyCLIAllowKeychainAccessTimeoutRemedy は同じときの【対処】に出る。
	KeyCLIAllowKeychainAccessTimeoutRemedy Key = "cli.allow_keychain_access.timeout_remedy"
)

// `continuo abandon` の引数とフラグの文言（internal/cli が出す分）。
const (
	// KeyCLIAbandonFlagDryRun は--dry-run の説明に出る。
	KeyCLIAbandonFlagDryRun Key = "cli.abandon.flag_dry_run"
	// KeyCLIAbandonFlagForce は--force の説明に出る。
	KeyCLIAbandonFlagForce Key = "cli.abandon.flag_force"
	// KeyCLIAbandonFlagTo は--to の説明に出る。
	KeyCLIAbandonFlagTo Key = "cli.abandon.flag_to"
	// KeyCLIAbandonFlagPark は--park の説明に出る。
	KeyCLIAbandonFlagPark Key = "cli.abandon.flag_park"
	// KeyCLIAbandonUsage は `continuo abandon --help` の冒頭に出る。
	KeyCLIAbandonUsage Key = "cli.abandon.usage"
	// KeyCLIAbandonErrIssueURLRequired はissue の URL が渡されなかったときに出る。
	KeyCLIAbandonErrIssueURLRequired Key = "cli.abandon.err_issue_url_required"
	// KeyCLIAbandonErrTooManyPositional は位置引数が3つ以上あるときに出る。
	KeyCLIAbandonErrTooManyPositional Key = "cli.abandon.err_too_many_positional"
)

// `continuo abandon` の本体（internal/abandon）が出す文言。
const (
	// KeyAbandonIssueURLEmpty はissue の URL が空のときに出る。
	KeyAbandonIssueURLEmpty Key = "abandon.issue_url.empty"
	// KeyAbandonIssueURLUnparsable はURL として読めないときに出る。
	KeyAbandonIssueURLUnparsable Key = "abandon.issue_url.unparsable"
	// KeyAbandonIssueURLBadScheme はhttp / https でないときに出る。
	KeyAbandonIssueURLBadScheme Key = "abandon.issue_url.bad_scheme"
	// KeyAbandonIssueURLNoHost はホスト名が無いときに出る。
	KeyAbandonIssueURLNoHost Key = "abandon.issue_url.no_host"
	// KeyAbandonIssueURLBadShape はissue の URL の形になっていないときに出る。
	KeyAbandonIssueURLBadShape Key = "abandon.issue_url.bad_shape"
	// KeyAbandonIssueURLBadNumber はissue の番号が正の整数でないときに出る。
	KeyAbandonIssueURLBadNumber Key = "abandon.issue_url.bad_number"

	// KeyAbandonHerdrSocketUnresolved はherdr の socket の場所を決められないときに出る。
	KeyAbandonHerdrSocketUnresolved Key = "abandon.herdr_socket_unresolved"
	// KeyAbandonRuntimeDirFailed は実行時ディレクトリを用意できないときに出る。
	KeyAbandonRuntimeDirFailed Key = "abandon.runtime_dir_failed"
	// KeyAbandonWorkspaceFailed は worktree の置き場所を用意できないときに出る。
	KeyAbandonWorkspaceFailed Key = "abandon.workspace_failed"

	// KeyAbandonErrConfigLoad はWORKFLOW.md を読めないときに出る。
	KeyAbandonErrConfigLoad Key = "abandon.err_config_load"
	// KeyAbandonErrBuild は部品を組み立てられないときに出る。
	KeyAbandonErrBuild Key = "abandon.err_build"
	// KeyAbandonErrLockFile はロックファイルそのものを開けないときに出る。
	KeyAbandonErrLockFile Key = "abandon.err_lock_file"
	// KeyAbandonRunning はcontinuo が動いているときの1行に出る。
	KeyAbandonRunning Key = "abandon.running"
	// KeyAbandonNotRunning はcontinuo が動いていないときの1行に出る。
	KeyAbandonNotRunning Key = "abandon.not_running"

	// KeyAbandonErrScan は置き場所を走査できないときに出る。
	KeyAbandonErrScan Key = "abandon.err_scan"
	// KeyAbandonNotFound はその issue の worktree が無いときに出る。
	KeyAbandonNotFound Key = "abandon.not_found"
	// KeyAbandonOwnerRepoMismatch は身元ファイルの issue_url が
	// worktree のパスから取り出した owner とリポジトリ名と食い違うときに出る。
	KeyAbandonOwnerRepoMismatch Key = "abandon.owner_repo_mismatch"
	// KeyAbandonOwnerRepoUnreadable はworktree のパスから owner とリポジトリ名を
	// 取り出せないときに出る。
	KeyAbandonOwnerRepoUnreadable Key = "abandon.owner_repo_unreadable"
	// KeyAbandonSlugMismatch は身元ファイルの issue_url が、置き場所の最下層の
	// ディレクトリ名（既定の branch_template では issue 番号を含む）と食い違うときに出る。
	KeyAbandonSlugMismatch Key = "abandon.slug_mismatch"
	// KeyAbandonSlugUnknown は、その issue に期待するディレクトリ名を組み立てられない
	// ときに出る（`herdr.worktree.branch_template` を描画できない場合）。
	KeyAbandonSlugUnknown Key = "abandon.slug_unknown"
	// KeyAbandonErrUndecided は、候補にできなかった worktree があるために
	// 「この issue の worktree はありません」と断言できないときに出る。
	KeyAbandonErrUndecided Key = "abandon.err_undecided"
	// KeyAbandonToSkipped は片付ける worktree が無く、`--to` の指定を使わずに
	// 終わるときに出る。**指定を黙って捨てない。**
	KeyAbandonToSkipped Key = "abandon.to_skipped"
	// KeyAbandonIdentityUnreadable は身元ファイルを読めない worktree を候補から
	// 外したときに出る。**黙って飛ばすと「worktree はありません」としか見えない。**
	KeyAbandonIdentityUnreadable Key = "abandon.identity_unreadable"
	// KeyAbandonIdentityMissing は身元ファイルが1つも無いディレクトリを候補から
	// 外したときに出る（issue #27）。**着手が worktree を作った直後に落ちるとこれができる。**
	KeyAbandonIdentityMissing Key = "abandon.identity_missing"
	// KeyAbandonErrMultiple はその issue の worktree が2つ以上あるときに出る。
	KeyAbandonErrMultiple Key = "abandon.err_multiple"
	// KeyAbandonMultipleItem は同じときに候補を1つずつ並べる行に出る。
	KeyAbandonMultipleItem Key = "abandon.multiple_item"

	// KeyAbandonErrTracker はボードのアダプタを作れない・Bootstrap を通せないときに出る。
	KeyAbandonErrTracker Key = "abandon.err_tracker"
	// KeyAbandonBoardNotListed はその issue がボードに載っていないときに出る。
	KeyAbandonBoardNotListed Key = "abandon.board_not_listed"
	// KeyAbandonErrParkStateUnknown はcontinuo が動いているのに Status を確かめられないときに出る。
	KeyAbandonErrParkStateUnknown Key = "abandon.err_park_state_unknown"
	// KeyAbandonParkNotActive はStatus が作業中の状態ではないので動かさないときに出る。
	KeyAbandonParkNotActive Key = "abandon.park_not_active"
	// KeyAbandonParkMoved は手を離させるために Status を動かしたときに出る。
	KeyAbandonParkMoved Key = "abandon.park_moved"
	// KeyAbandonErrParkActive は `--park` に作業中の状態（tracker.active_states の値）が
	// 指定されたときに出る。**そこへ動かしても継続監視は手を離さない。**
	KeyAbandonErrParkActive Key = "abandon.err_park_active"
	// KeyAbandonErrUnknownState は `--to` や `--park` の値がボードの Status の
	// 選択肢に無いときに出る。**worktree を消す前に出す。**
	KeyAbandonErrUnknownState Key = "abandon.err_unknown_state"
	// KeyAbandonErrParkFailed は手を離させる書き込みに失敗したときに出る。
	KeyAbandonErrParkFailed Key = "abandon.err_park_failed"
	// KeyAbandonParkNotWritten は手を離させる書き込みが行われなかったときに出る。
	KeyAbandonParkNotWritten Key = "abandon.park_not_written"
	// KeyAbandonParkLeftBehind は、手を離させる書き込みを済ませたあとに何も消さずに
	// 止まったときに出る。**Status は park の値のままであり、continuo は戻さない。**
	KeyAbandonParkLeftBehind Key = "abandon.park_left_behind"

	// KeyAbandonErrPaneList はherdr へ pane の一覧を問い合わせられないときに出る。
	KeyAbandonErrPaneList Key = "abandon.err_pane_list"
	// KeyAbandonWaitingPane はpane が閉じるのを待っているあいだに出る。
	KeyAbandonWaitingPane Key = "abandon.waiting_pane"
	// KeyAbandonPaneGone はpane が閉じたときに出る。
	KeyAbandonPaneGone Key = "abandon.pane_gone"
	// KeyAbandonErrPaneRemains は上限までに pane が閉じなかったときに出る。
	KeyAbandonErrPaneRemains Key = "abandon.err_pane_remains"
	// KeyAbandonErrPaneAlive は継続監視が動いていないのに pane が生きているときに出る。
	KeyAbandonErrPaneAlive Key = "abandon.err_pane_alive"
	// KeyAbandonPaneCheckSkipped は、herdr へ問い合わせられないまま `--force` が
	// 指定されていて、pane の生死を確かめずに消すときに出る。
	KeyAbandonPaneCheckSkipped Key = "abandon.pane_check_skipped"
	// KeyAbandonPaneAliveForced は、pane が生きているのに `--force` が指定されていて、
	// pane ごと消すときに出る。**err_pane_alive と言い分ける**（あちらは止まる）。
	KeyAbandonPaneAliveForced Key = "abandon.pane_alive_forced"
	// KeyAbandonErrPaneWaitInterrupted は pane が閉じるのを待っている途中で
	// `SIGINT` / `SIGTERM` を受けたときに出る。**時間切れとは言い分ける。**
	KeyAbandonErrPaneWaitInterrupted Key = "abandon.err_pane_wait_interrupted"

	// KeyAbandonErrInspect は失われるものを調べられないときに出る。
	KeyAbandonErrInspect Key = "abandon.err_inspect"
	// KeyAbandonPlanHeader は消すものの一覧の見出しに出る。
	KeyAbandonPlanHeader Key = "abandon.plan_header"
	// KeyAbandonPlanIssue は対象の issue の行に出る。
	KeyAbandonPlanIssue Key = "abandon.plan_issue"
	// KeyAbandonPlanStatus は現在の Status の行に出る。
	KeyAbandonPlanStatus Key = "abandon.plan_status"
	// KeyAbandonPlanStatusUnknown はStatus を読めなかったときの行に出る。
	KeyAbandonPlanStatusUnknown Key = "abandon.plan_status_unknown"
	// KeyAbandonPlanWorktree はworktree のパスの行に出る。
	KeyAbandonPlanWorktree Key = "abandon.plan_worktree"
	// KeyAbandonPlanBranch はbranch と base の行に出る。
	KeyAbandonPlanBranch Key = "abandon.plan_branch"
	// KeyAbandonPlanHerdrWorkspace はherdr の workspace の行に出る。
	KeyAbandonPlanHerdrWorkspace Key = "abandon.plan_herdr_workspace"
	// KeyAbandonPlanPane はherdr の pane の行に出る。
	KeyAbandonPlanPane Key = "abandon.plan_pane"
	// KeyAbandonPlanPaneNone は該当する pane が無いときの行に出る。
	KeyAbandonPlanPaneNone Key = "abandon.plan_pane_none"
	// KeyAbandonPlanPaneUnknown はherdr に問い合わせられなかったときの行に出る。
	KeyAbandonPlanPaneUnknown Key = "abandon.plan_pane_unknown"
	// KeyAbandonPlanDirty はコミットされていない変更のファイル数の行に出る。
	KeyAbandonPlanDirty Key = "abandon.plan_dirty"
	// KeyAbandonPlanDirtyAtLeast は変更のファイル数を数え切れなかったときの行に出る
	// （`git status --porcelain` の読み取りが上限で打ち切られた場合）。
	KeyAbandonPlanDirtyAtLeast Key = "abandon.plan_dirty_at_least"
	// KeyAbandonPlanDirtyUnknown は、git が答えられずに変更のファイル数を
	// 1件も数えられなかったときの行に出る（worktree の `.git` が壊れている場合など）。
	KeyAbandonPlanDirtyUnknown Key = "abandon.plan_dirty_unknown"
	// KeyAbandonPlanUnpushed はpush されていない commit の件数の行に出る。
	KeyAbandonPlanUnpushed Key = "abandon.plan_unpushed"
	// KeyAbandonPlanUnpushedUnknown は、git が答えられずに push されていない成果が
	// あるかを判定できなかったときの行に出る。
	KeyAbandonPlanUnpushedUnknown Key = "abandon.plan_unpushed_unknown"
	// KeyAbandonPlanUndetermined は、調べられなかったことを1件ずつ並べる行に出る。
	KeyAbandonPlanUndetermined Key = "abandon.plan_undetermined"
	// KeyAbandonPlanBaseUnknown はupstream も base も無く判定できないときの行に出る。
	KeyAbandonPlanBaseUnknown Key = "abandon.plan_base_unknown"
	// KeyAbandonPlanDiffFromBase はupstream が無いまま base との差分が残っているときの行に出る。
	KeyAbandonPlanDiffFromBase Key = "abandon.plan_diff_from_base"
	// KeyAbandonPlanNoDiffFromBase はupstream が無く base との差分も無いときの行に出る。
	KeyAbandonPlanNoDiffFromBase Key = "abandon.plan_no_diff_from_base"
	// KeyAbandonPlanParkPending は`--dry-run` で継続監視が動いているとき、
	// 実行したら Status をどこへ動かすかを予告する行に出る。
	KeyAbandonPlanParkPending Key = "abandon.plan_park_pending"

	// KeyAbandonDryRunNote は--dry-run の締めの1行に出る。
	KeyAbandonDryRunNote Key = "abandon.dry_run_note"
	// KeyAbandonErrLossWithoutForce は失うものがあるのに --force が無いときに出る。
	KeyAbandonErrLossWithoutForce Key = "abandon.err_loss_without_force"
	// KeyAbandonErrUndeterminedWithoutForce は、失うものがあるかを調べ切れなかったのに
	// --force が無いときに出る。**「失うものがある」とは言い分ける。**
	KeyAbandonErrUndeterminedWithoutForce Key = "abandon.err_undetermined_without_force"
	// KeyAbandonErrCleanup は片付けに失敗したときに出る。
	KeyAbandonErrCleanup Key = "abandon.err_cleanup"
	// KeyAbandonErrDeferred は片付けが見送られたときに出る。
	KeyAbandonErrDeferred Key = "abandon.err_deferred"
	// KeyAbandonDeferredReason は同じときに理由を1つずつ並べる行に出る。
	KeyAbandonDeferredReason Key = "abandon.deferred_reason"
	// KeyAbandonRemoved は worktree と branch を消し終えたときに出る。
	KeyAbandonRemoved Key = "abandon.removed"
	// KeyAbandonRemovedBranchAbsent は worktree を消し終えたが、身元ファイルに書かれた
	// branch がリポジトリに実在しなかったときに出る。
	// **「消しました」と言わない。**消す対象が元から無かった（issue #27）。
	KeyAbandonRemovedBranchAbsent Key = "abandon.removed_branch_absent"
	// KeyAbandonRemovedWithLeftovers は worktree は消えたが、branch や herdr の workspace が
	// 片付け切れずに残ったときに出る。**残ったものは KeyAbandonLeftover で1件ずつ並べる。**
	KeyAbandonRemovedWithLeftovers Key = "abandon.removed_with_leftovers"
	// KeyAbandonLeftover は片付け切れずに残ったものを1件ずつ並べる行に出る。
	KeyAbandonLeftover Key = "abandon.leftover"
	// KeyAbandonNotice は continuo が片付けの途中で自分で行ったことを1件ずつ並べる行に出る
	// （壊れた ref のファイルを消した、など。issue #28）。
	KeyAbandonNotice Key = "abandon.notice"

	// KeyAbandonOrphanBranchUnknown は worktree が無いときに、残った branch があるかを
	// 調べられなかったときに出る（issue #27）。
	KeyAbandonOrphanBranchUnknown Key = "abandon.orphan_branch.unknown"
	// KeyAbandonOrphanBranchNone は worktree も branch も残っていなかったときに出る。
	KeyAbandonOrphanBranchNone Key = "abandon.orphan_branch.none"
	// KeyAbandonOrphanBranchFound は worktree は無いが branch が残っていたときに出る。
	KeyAbandonOrphanBranchFound Key = "abandon.orphan_branch.found"
	// KeyAbandonOrphanBranchUnpushed は、残った branch にどの remote にも載っていない
	// commit があるときに出る。**`--force` を求める前に出す**（3-37-9）。
	KeyAbandonOrphanBranchUnpushed Key = "abandon.orphan_branch.unpushed"
	// KeyAbandonOrphanBranchUnpushedUnknown は未 push の commit を数えられなかったときに出る。
	// **数えられなかったことを 0 件として見せない。**
	KeyAbandonOrphanBranchUnpushedUnknown Key = "abandon.orphan_branch.unpushed_unknown"
	// KeyAbandonOrphanBranchDisabled は cleanup.delete_branch が偽なので、
	// 残った branch を消さずに終えるときに出る。
	KeyAbandonOrphanBranchDisabled Key = "abandon.orphan_branch.disabled"
	// KeyAbandonErrOrphanBranchWithoutForce は残った branch があるのに --force が無いときに出る。
	KeyAbandonErrOrphanBranchWithoutForce Key = "abandon.orphan_branch.err_without_force"
	// KeyAbandonErrOrphanBranchDeleteFailed は残った branch を消せなかったときに出る。
	KeyAbandonErrOrphanBranchDeleteFailed Key = "abandon.orphan_branch.err_delete_failed"
	// KeyAbandonOrphanBranchRemoved は残った branch を消したときに出る。
	// **戻すためのコマンドを添える**（`git branch -D` はマージ状態を見ない）。
	KeyAbandonOrphanBranchRemoved Key = "abandon.orphan_branch.removed"

	// KeyAbandonStatusLeftAlone は--to が無いのでStatus を動かさないときに出る。
	KeyAbandonStatusLeftAlone Key = "abandon.status_left_alone"
	// KeyAbandonErrStatusTargetUnknown は--to があるのに issue をボードから引けないときに出る。
	KeyAbandonErrStatusTargetUnknown Key = "abandon.err_status_target_unknown"
	// KeyAbandonStatusMoved はStatus を動かしたときに出る。
	KeyAbandonStatusMoved Key = "abandon.status_moved"
	// KeyAbandonErrStatusFailed はStatus の書き込みに失敗したときに出る。
	KeyAbandonErrStatusFailed Key = "abandon.err_status_failed"
	// KeyAbandonStatusNotWritten はStatus の書き込みが行われなかったときに出る。
	KeyAbandonStatusNotWritten Key = "abandon.status_not_written"
)

// `continuo`（常駐プロセス本体）の文言。
const (
	// KeyCLIMainUsage は `continuo --help` の冒頭に出る。
	//
	// **サブコマンドの一覧をここに書く。**flag は自分が知っているフラグしか出さないので、
	// これが無いと `init` も `doctor` も一覧に載らない。
	KeyCLIMainUsage Key = "cli.main.usage"

	// KeyCLIMainFlagLogLevel は--log-level の説明に出る。
	KeyCLIMainFlagLogLevel Key = "cli.main.flag_log_level"
	// KeyCLIMainFlagPort は--port の説明に出る。
	KeyCLIMainFlagPort Key = "cli.main.flag_port"

	// KeyCLIMainErrPortRange は --port の値が 0〜65535 の外だったことを表す。
	KeyCLIMainErrPortRange Key = "cli.main.err_port_range"
	// KeyCLIMainErrTooManyPositional は位置引数が2つ以上あるときに出る。
	KeyCLIMainErrTooManyPositional Key = "cli.main.err_too_many_positional"
	// KeyCLIMainStarting は起動したときの1行に出る。
	KeyCLIMainStarting Key = "cli.main.starting"
)

// `continuo hook` の文言。
const (
	// KeyCLIHookFlagSocket は--socket の説明に出る。
	KeyCLIHookFlagSocket Key = "cli.hook.flag_socket"
	// KeyCLIHookFlagPendingDir は--pending-dir の説明に出る。
	KeyCLIHookFlagPendingDir Key = "cli.hook.flag_pending_dir"
	// KeyCLIHookErrPositional は位置引数が渡されたときに出る。
	KeyCLIHookErrPositional Key = "cli.hook.err_positional"
	// KeyCLIHookErrSocketRequired は--socket が無いときに出る。
	KeyCLIHookErrSocketRequired Key = "cli.hook.err_socket_required"
	// KeyCLIHookErrPendingDirRequired は--pending-dir が無いときに出る。
	KeyCLIHookErrPendingDirRequired Key = "cli.hook.err_pending_dir_required"
	// KeyCLIHookErrSocketAbs は--socket が相対パスのときに出る。
	KeyCLIHookErrSocketAbs Key = "cli.hook.err_socket_abs"
	// KeyCLIHookErrPendingDirAbs は--pending-dir が相対パスのときに出る。
	KeyCLIHookErrPendingDirAbs Key = "cli.hook.err_pending_dir_abs"
	// KeyCLIHookTruncated は標準入力が上限を超えたときに出る。
	KeyCLIHookTruncated Key = "cli.hook.truncated"
	// KeyCLIHookSpilled はsocket へ届かず逃がし先へ書いたときに出る。
	KeyCLIHookSpilled Key = "cli.hook.spilled"
	// KeyCLIHookDropped はsocket にも逃がし先にも書けなかったときに出る。
	KeyCLIHookDropped Key = "cli.hook.dropped"
)

// ダッシュボード（HTML）の文言。
const (
	// KeyDashboardMeta は見出しの下の1行に出る。
	KeyDashboardMeta Key = "dashboard.meta"
	// KeyDashboardCaptionRuns は実行中の run の表の見出しに出る。
	KeyDashboardCaptionRuns Key = "dashboard.caption_runs"
	// KeyDashboardColIssue はissue の列に出る。
	KeyDashboardColIssue Key = "dashboard.col_issue"
	// KeyDashboardColStatus はStatus の列に出る。
	KeyDashboardColStatus Key = "dashboard.col_status"
	// KeyDashboardColTurn はturn の回数の列に出る。
	KeyDashboardColTurn Key = "dashboard.col_turn"
	// KeyDashboardColLastHook は最後に hook を受けた時刻の列に出る。
	KeyDashboardColLastHook Key = "dashboard.col_last_hook"
	// KeyDashboardColTokensTotal はトークン合計の列に出る。
	KeyDashboardColTokensTotal Key = "dashboard.col_tokens_total"
	// KeyDashboardBadgeWaitingQuota は枠待ちの印に出る。
	KeyDashboardBadgeWaitingQuota Key = "dashboard.badge_waiting_quota"
	// KeyDashboardBadgeRetry はリトライ回数の印に出る。
	KeyDashboardBadgeRetry Key = "dashboard.badge_retry"
	// KeyDashboardBadgeResume は再 dispatch の時刻の印に出る。
	KeyDashboardBadgeResume Key = "dashboard.badge_resume"
	// KeyDashboardNoHookYet はhook を1件も受けていないときの添え書きに出る。
	KeyDashboardNoHookYet Key = "dashboard.no_hook_yet"
	// KeyDashboardTokensAt はトークンを集計した時刻の添え書きに出る。
	KeyDashboardTokensAt Key = "dashboard.tokens_at"
	// KeyDashboardTokensNotCounted は一度も集計していないときの添え書きに出る。
	KeyDashboardTokensNotCounted Key = "dashboard.tokens_not_counted"
	// KeyDashboardNoRuns は実行中の run が1件も無いときに出る。
	KeyDashboardNoRuns Key = "dashboard.no_runs"
	// KeyDashboardNoteLastHook は実行中の run の表の下の注記に出る。
	KeyDashboardNoteLastHook Key = "dashboard.note_last_hook"
	// KeyDashboardCaptionTokens はトークンの表の見出しに出る。
	KeyDashboardCaptionTokens Key = "dashboard.caption_tokens"
	// KeyDashboardColAPICalls はAPI 応答の件数の列に出る。
	KeyDashboardColAPICalls Key = "dashboard.col_api_calls"
	// KeyDashboardColInput は入力のトークンの列に出る。
	KeyDashboardColInput Key = "dashboard.col_input"
	// KeyDashboardColCacheCreation はキャッシュ作成のトークンの列に出る。
	KeyDashboardColCacheCreation Key = "dashboard.col_cache_creation"
	// KeyDashboardColCacheRead はキャッシュ読み出しのトークンの列に出る。
	KeyDashboardColCacheRead Key = "dashboard.col_cache_read"
	// KeyDashboardColOutput は出力のトークンの列に出る。
	KeyDashboardColOutput Key = "dashboard.col_output"
	// KeyDashboardTotal はトークンの表の合計の行に出る。
	KeyDashboardTotal Key = "dashboard.total"
	// KeyDashboardNoteTokens はトークンの表の下の注記に出る。
	KeyDashboardNoteTokens Key = "dashboard.note_tokens"
	// KeyDashboardAgoSeconds は1分未満の経過に出る。
	KeyDashboardAgoSeconds Key = "dashboard.ago_seconds"
	// KeyDashboardAgoMinutes は1時間未満の経過に出る。
	KeyDashboardAgoMinutes Key = "dashboard.ago_minutes"
	// KeyDashboardAgoHours は1時間以上の経過に出る。
	KeyDashboardAgoHours Key = "dashboard.ago_hours"
	// KeyDashboardNone は値がまだ無いことを表す印に出る。
	KeyDashboardNone Key = "dashboard.none"
)

// 二重起動を防ぐロック（internal/lock）のエラーの文言。
const (
	// KeyLockAcquireOpenFailed はロックファイルを開けなかったときに出る。
	KeyLockAcquireOpenFailed Key = "lock.acquire.open_failed"
	// KeyLockAcquireAlreadyRunning は別のプロセスが同じロックファイルを掴んでいるときに出る。
	// 先頭の %w には ErrAlreadyRunning を渡す（errors.Is の切り分けを保つため）。
	KeyLockAcquireAlreadyRunning Key = "lock.acquire.already_running"
	// KeyLockReleaseUnlockFailed はflock の解放に失敗したときに出る。
	KeyLockReleaseUnlockFailed Key = "lock.release.unlock_failed"
	// KeyLockReleaseCloseFailed はロックファイルのクローズに失敗したときに出る。
	KeyLockReleaseCloseFailed Key = "lock.release.close_failed"
)

// hook を受ける socket の置き場所（internal/socketpath）のエラーの文言。
const (
	// KeySocketpathRuntimeDirHomeDirFailed は既定の ~/.continuo/run を組み立てるための
	// ホームディレクトリを取得できなかったときに出る。
	KeySocketpathRuntimeDirHomeDirFailed Key = "socketpath.runtime_dir.home_dir_failed"
	// KeySocketpathCheckAbsNotAbsolute は置き場所として渡された値が絶対パスでないときに出る。
	// 最初の %s には socketpath.source.* の文言が入る。
	KeySocketpathCheckAbsNotAbsolute Key = "socketpath.check_abs.not_absolute"
	// KeySocketpathCheckPathLenTooLong はsocket のパスが MaxPathLen を超えたときに出る。
	KeySocketpathCheckPathLenTooLong Key = "socketpath.check_path_len.too_long"
	// KeySocketpathEnsureDirParentMkdirFailed は置き場所の親ディレクトリを作れなかったときに出る。
	KeySocketpathEnsureDirParentMkdirFailed Key = "socketpath.ensure_dir.parent_mkdir_failed"
	// KeySocketpathEnsureDirChmodFailed は自分で作ったディレクトリを 0700 にできなかったときに出る。
	KeySocketpathEnsureDirChmodFailed Key = "socketpath.ensure_dir.chmod_failed"
	// KeySocketpathEnsureDirMkdirFailed は置き場所のディレクトリを作れなかったときに出る。
	KeySocketpathEnsureDirMkdirFailed Key = "socketpath.ensure_dir.mkdir_failed"
	// KeySocketpathCheckExistingDirLstatFailed は既にあるディレクトリを Lstat できなかったときに出る。
	KeySocketpathCheckExistingDirLstatFailed Key = "socketpath.check_existing_dir.lstat_failed"
	// KeySocketpathCheckExistingDirSymlink は既にあるものが symlink だったときに出る。
	KeySocketpathCheckExistingDirSymlink Key = "socketpath.check_existing_dir.symlink"
	// KeySocketpathCheckExistingDirNotADirectory は既にあるものがディレクトリでなかったときに出る。
	KeySocketpathCheckExistingDirNotADirectory Key = "socketpath.check_existing_dir.not_a_directory"
	// KeySocketpathCheckExistingDirPermTooOpen は既にあるディレクトリの権限が group / other に
	// 開いていたときに出る。
	KeySocketpathCheckExistingDirPermTooOpen Key = "socketpath.check_existing_dir.perm_too_open"
)

// hook を受ける socket の置き場所を、どこから読んだかを表す語。
//
// **socketpath.check_abs.not_absolute の最初の %s に埋まる。**
// ここを文言にしておかないと、エラーの一文だけが日本語のまま残る。
const (
	// KeySocketpathSourceEnvRuntimeDir は環境変数 CONTINUO_RUNTIME_DIR から読んだことを表す。
	KeySocketpathSourceEnvRuntimeDir Key = "socketpath.source.env_continuo_runtime_dir"
	// KeySocketpathSourceEnvXDGRuntimeDir は環境変数 XDG_RUNTIME_DIR から読んだことを表す。
	KeySocketpathSourceEnvXDGRuntimeDir Key = "socketpath.source.env_xdg_runtime_dir"
	// KeySocketpathSourceEnvTMPDir は環境変数 TMPDIR から読んだことを表す。
	KeySocketpathSourceEnvTMPDir Key = "socketpath.source.env_tmpdir"
	// KeySocketpathSourceConfigListen は設定キー claude.hook_bridge.listen から読んだことを表す。
	KeySocketpathSourceConfigListen Key = "socketpath.source.config_hook_bridge_listen"
)

// WORKFLOW.md の読み込み（internal/config）のエラーの文言。
const (
	// KeyConfigResolvePathWorkDirNotAbsolute は WORKFLOW.md の場所を決める基準に渡された
	// 作業ディレクトリが絶対パスでないときに出る。
	KeyConfigResolvePathWorkDirNotAbsolute Key = "config.resolve_path.work_dir_not_absolute"
	// KeyConfigLoadPathNotAbsolute は読み込む WORKFLOW.md のパスが絶対パスでないときに出る。
	KeyConfigLoadPathNotAbsolute Key = "config.load.path_not_absolute"
	// KeyConfigLoadReadFailed は WORKFLOW.md をファイルとして読めなかったときに出る。
	KeyConfigLoadReadFailed Key = "config.load.read_failed"
	// KeyConfigLoadFrontMatterSplitFailed は front matter と本文に切り分けられなかったときに出る。
	KeyConfigLoadFrontMatterSplitFailed Key = "config.load.front_matter_split_failed"
	// KeyConfigLoadFrontMatterInvalid は front matter の中身が不正だったときに出る。
	KeyConfigLoadFrontMatterInvalid Key = "config.load.front_matter_invalid"
	// KeyConfigLoadExpandFailed は設定値の環境変数展開・チルダ展開に失敗したときに出る。
	KeyConfigLoadExpandFailed Key = "config.load.expand_failed"
)

// front matter の切り出し（internal/config の splitFrontMatter）のエラーの文言。
const (
	// KeyConfigFrontMatterNoStartDelimiter は1行目が開始の区切り行でないときに出る。
	KeyConfigFrontMatterNoStartDelimiter Key = "config.front_matter.no_start_delimiter"
	// KeyConfigFrontMatterNoEndDelimiter は終端の区切り行が見つからないときに出る。
	KeyConfigFrontMatterNoEndDelimiter Key = "config.front_matter.no_end_delimiter"
)

// 雛形のプレースホルダが残っていることを知らせる文言。
const (
	// KeyConfigPlaceholderRemaining は `continuo init` の雛形の値が埋められないまま
	// 残っているときに出る。
	KeyConfigPlaceholderRemaining Key = "config.placeholder.remaining"
)

// front matter の値の検査（internal/config の validate）のエラーの文言。
const (
	// KeyConfigValidateInvalidValue は値は入っているが不正であるときに出る。
	KeyConfigValidateInvalidValue Key = "config.validate.invalid_value"
	// KeyConfigValidateRequired は必須のキーが空・未設定であるときに出る。
	KeyConfigValidateRequired Key = "config.validate.required"
	// KeyConfigValidateBranchTemplateNeedsIssueNumber は
	// herdr.worktree.branch_template に issue の番号が入っていないときに出る。
	// **既に設定している利用者が居るので、なぜ要るのかを書く。**
	KeyConfigValidateBranchTemplateNeedsIssueNumber Key = "config.validate.branch_template_needs_issue_number"
)

// 設定値の環境変数展開・チルダ展開（internal/config の expand）のエラーの文言。
const (
	// KeyConfigExpandTrailingDollar は値が "$" で終わっているときに出る。
	KeyConfigExpandTrailingDollar Key = "config.expand.trailing_dollar"
	// KeyConfigExpandUnclosedBrace は "${" が "}" で閉じられていないときに出る。
	KeyConfigExpandUnclosedBrace Key = "config.expand.unclosed_brace"
	// KeyConfigExpandEmptyEnvName は "${}" のように環境変数名が空のときに出る。
	KeyConfigExpandEmptyEnvName Key = "config.expand.empty_env_name"
	// KeyConfigExpandInvalidDollarForm は "$" が受け付ける3つの形式のいずれでもないときに出る。
	KeyConfigExpandInvalidDollarForm Key = "config.expand.invalid_dollar_form"
	// KeyConfigExpandEnvUndefined は参照した環境変数が定義されていないときに出る。
	KeyConfigExpandEnvUndefined Key = "config.expand.env_undefined"
	// KeyConfigExpandEnvEmpty は参照した環境変数が定義されているが空文字のときに出る。
	KeyConfigExpandEnvEmpty Key = "config.expand.env_empty"
	// KeyConfigExpandHomeDirFailed はチルダ展開のためのホームディレクトリを取得できないときに出る。
	KeyConfigExpandHomeDirFailed Key = "config.expand.home_dir_failed"
	// KeyConfigExpandTildeUserUnsupported は "~user" 形式のチルダ展開が書かれているときに出る。
	KeyConfigExpandTildeUserUnsupported Key = "config.expand.tilde_user_unsupported"
)

// herdr の ping・protocol 版の照合（internal/herdr の Ping / CheckProtocol）のエラーの文言。
const (
	// KeyHerdrPingCallFailed は ping そのものを呼べなかったときに出る。
	KeyHerdrPingCallFailed Key = "herdr.ping.call_failed"
	// KeyHerdrPingUnmarshalFailed は ping の応答を JSON として読めなかったときに出る。
	KeyHerdrPingUnmarshalFailed Key = "herdr.ping.unmarshal_failed"
	// KeyHerdrCheckProtocolPingFailed は照合の前段の ping が失敗したときに出る。
	KeyHerdrCheckProtocolPingFailed Key = "herdr.check_protocol.ping_failed"
	// KeyHerdrCheckProtocolVersionMismatch は herdr の protocol 版が設定と食い違うときに出る。
	KeyHerdrCheckProtocolVersionMismatch Key = "herdr.check_protocol.version_mismatch"
)

// herdr の socket API を1回呼ぶ処理（internal/herdr の call）のエラーの文言。
const (
	// KeyHerdrCallUnmarshalFailed は応答の result を JSON として読めなかったときに出る。
	// pane・agent・worktree・workspace のすべてのメソッドが共有する。
	KeyHerdrCallUnmarshalFailed Key = "herdr.call.unmarshal_failed"
	// KeyHerdrCallRequestIDFailed はリクエスト id 用の乱数を取れなかったときに出る。
	KeyHerdrCallRequestIDFailed Key = "herdr.call.request_id_failed"
	// KeyHerdrCallMarshalParamsFailed は params を JSON へ変換できなかったときに出る。
	KeyHerdrCallMarshalParamsFailed Key = "herdr.call.marshal_params_failed"
	// KeyHerdrCallMarshalRequestFailed はリクエスト全体を JSON へ変換できなかったときに出る。
	KeyHerdrCallMarshalRequestFailed Key = "herdr.call.marshal_request_failed"
)

// herdr の socket のパスを決める処理（internal/herdr の ResolveSocketPath）の文言。
const (
	// KeyHerdrSocketPathNotAbsolute は決まったパスが絶対パスでないときに出る。
	KeyHerdrSocketPathNotAbsolute Key = "herdr.socket_path.not_absolute"
	// KeyHerdrSocketPathHomeDirFailed は既定値へ落ちる際にホームディレクトリを取れないときに出る。
	KeyHerdrSocketPathHomeDirFailed Key = "herdr.socket_path.home_dir_failed"
	// KeyHerdrSocketPathSourceConfig は上の not_absolute の %s に入る、値の出どころの説明である。
	KeyHerdrSocketPathSourceConfig Key = "herdr.socket_path.source_config"
)

// herdr の agent を扱う処理（internal/herdr の ValidateAgentName / AgentSendKeys）の文言。
const (
	// KeyHerdrAgentInvalidName は agent 名が herdr の許容パターンに収まらないときに出る。
	KeyHerdrAgentInvalidName Key = "herdr.agent.invalid_name"
	// KeyHerdrAgentSendKeysEmpty は agent.send_keys に送るキーが1つも無いときに出る。
	KeyHerdrAgentSendKeysEmpty Key = "herdr.agent.send_keys_empty"
)

// ボードを読み書きするためのトークンの取得（internal/tracker の RunGHAuthToken）の文言。
const (
	// KeyTrackerGHAuthTokenRunFailed は `gh auth token` の実行そのものが失敗したときに出る。
	KeyTrackerGHAuthTokenRunFailed Key = "tracker.gh_auth_token.run_failed"
	// KeyTrackerGHAuthTokenEmptyOutput は `gh auth token` が空文字を返したときに出る。
	KeyTrackerGHAuthTokenEmptyOutput Key = "tracker.gh_auth_token.empty_output"
)

// gh の有無と scope の検査（internal/tracker の RunGHAuthStatus / CheckGHAvailable /
// CheckGHProjectScope）の文言。
const (
	// KeyTrackerGHAuthStatusStartFailed は `gh auth status` を起動できなかったときに出る。
	// 終了コードが非 0 なだけの場合はここに来ない（未ログインの判定は出力の中身で行う）。
	KeyTrackerGHAuthStatusStartFailed Key = "tracker.gh_auth_status.start_failed"
	// KeyTrackerGHAvailableNotInPath は gh そのものが PATH に無いときに出る。
	KeyTrackerGHAvailableNotInPath Key = "tracker.gh_available.not_in_path"
	// KeyTrackerGHScopeNoActiveAccount は github.com の有効なアカウントが1つも無いときに出る。
	KeyTrackerGHScopeNoActiveAccount Key = "tracker.gh_scope.no_active_account"
	// KeyTrackerGHScopeMissingScope は有効なアカウントの scope に project が無いときに出る。
	KeyTrackerGHScopeMissingScope Key = "tracker.gh_scope.missing_scope"
)

// hook を受ける socket の listen と後片付け（internal/hookserver の Start /
// removeStaleSocketFile / Close）の文言。
const (
	// KeyHookserverStartListenFailed はsocket を listen できなかったときに出る。
	KeyHookserverStartListenFailed Key = "hookserver.start.listen_failed"
	// KeyHookserverRemoveStaleSocketLstatFailed は残骸かどうかを見るための Lstat が
	// 失敗したときに出る。
	KeyHookserverRemoveStaleSocketLstatFailed Key = "hookserver.remove_stale_socket.lstat_failed"
	// KeyHookserverRemoveStaleSocketAlreadyListening は同じパスで別のプロセスが
	// 既に listen していたときに出る（continuo の二重起動）。
	KeyHookserverRemoveStaleSocketAlreadyListening Key = "hookserver.remove_stale_socket.already_listening"
	// KeyHookserverRemoveStaleSocketRemoveFailed は前回の実行が残した socket ファイルを
	// 消せなかったときに出る。
	KeyHookserverRemoveStaleSocketRemoveFailed Key = "hookserver.remove_stale_socket.remove_failed"
	// KeyHookserverCloseListenerCloseFailed はsocket を閉じられなかったときに出る。
	KeyHookserverCloseListenerCloseFailed Key = "hookserver.close.listener_close_failed"
)

// 受け取った hook の JSON の解釈（internal/hookserver の decodeEvent）の文言。
const (
	// KeyHookserverDecodeEventNotObject はトップレベルが JSON のオブジェクトで
	// なかったときに出る。
	KeyHookserverDecodeEventNotObject Key = "hookserver.decode_event.not_object"
)

// 逃がし先の読み戻し（internal/hookserver の pendingDirs / scanPendingDir /
// readPendingFile）の文言。
//
// **not_regular_file と too_large は隔離の理由として broken へ記録される。**
const (
	// KeyHookserverPendingDirsIssuesDirUnreadable は逃がし先の親ディレクトリ（issues）を
	// 読めなかったときに出る。
	KeyHookserverPendingDirsIssuesDirUnreadable Key = "hookserver.pending_dirs.issues_dir_unreadable"
	// KeyHookserverPendingNotRegularFile は逃がし先に通常ファイルでないものがあったときに出る。
	KeyHookserverPendingNotRegularFile Key = "hookserver.pending.not_regular_file"
	// KeyHookserverReadPendingFileTooLarge は逃がし先のファイルが1件の上限より大きく、
	// 読まずに隔離したときに出る。
	KeyHookserverReadPendingFileTooLarge Key = "hookserver.read_pending_file.too_large"
)

// `continuo hook` が hook を転送しきれなかったときの文言（internal/hookclient の Forward）。
const (
	// KeyHookclientForwardNoPendingDir はsocket へ転送できず、逃がし先も
	// 指定されていなかったときに出る。
	KeyHookclientForwardNoPendingDir Key = "hookclient.forward.no_pending_dir"
	// KeyHookclientForwardSpillFailed はsocket へ転送できず、逃がし先へも書けなかったときに出る。
	KeyHookclientForwardSpillFailed Key = "hookclient.forward.spill_failed"
)

// `continuo hook` の標準入力の読み取りと1行への組み立て（internal/hookclient の
// readInput / truncatedLine / compactLine）の文言。
const (
	// KeyHookclientReadInputReadFailed は標準入力そのものを読めなかったときに出る。
	KeyHookclientReadInputReadFailed Key = "hookclient.read_input.read_failed"
	// KeyHookclientTruncatedLineHeadUnreadable は上限を超えた入力の先頭も
	// JSON として読めなかったときに出る。
	KeyHookclientTruncatedLineHeadUnreadable Key = "hookclient.truncated_line.head_unreadable"
	// KeyHookclientTruncatedLineNotObject は上限を超えた入力が JSON のオブジェクトで
	// 始まっていなかったときに出る。
	KeyHookclientTruncatedLineNotObject Key = "hookclient.truncated_line.not_object"
	// KeyHookclientTruncatedLineNoFields は上限を超えた入力の先頭から hook の項目を
	// 1つも拾えなかったときに出る。
	KeyHookclientTruncatedLineNoFields Key = "hookclient.truncated_line.no_fields"
	// KeyHookclientTruncatedLineMarshalFailed は拾い直した項目を1行の JSON へ
	// 組み立てられなかったときに出る。
	KeyHookclientTruncatedLineMarshalFailed Key = "hookclient.truncated_line.marshal_failed"
	// KeyHookclientCompactLineUnmarshalFailed は標準入力を hook の JSON として
	// 解釈できなかったときに出る。
	KeyHookclientCompactLineUnmarshalFailed Key = "hookclient.compact_line.unmarshal_failed"
	// KeyHookclientCompactLineNotObject は標準入力のトップレベルが JSON の
	// オブジェクトでなかったときに出る。
	KeyHookclientCompactLineNotObject Key = "hookclient.compact_line.not_object"
	// KeyHookclientCompactLineCompactFailed は標準入力の JSON を1行に詰められなかったときに出る。
	KeyHookclientCompactLineCompactFailed Key = "hookclient.compact_line.compact_failed"
)

// `continuo hook` から hook 受け口の socket への転送（internal/hookclient の sendToSocket）の文言。
const (
	// KeyHookclientSendToSocketPathEmpty は --socket が指定されていないときに出る。
	KeyHookclientSendToSocketPathEmpty Key = "hookclient.send_to_socket.path_empty"
	// KeyHookclientSendToSocketDialFailed はsocket へ接続できなかったときに出る。
	KeyHookclientSendToSocketDialFailed Key = "hookclient.send_to_socket.dial_failed"
	// KeyHookclientSendToSocketDeadlineFailed は書き込みの期限を設定できなかったときに出る。
	KeyHookclientSendToSocketDeadlineFailed Key = "hookclient.send_to_socket.deadline_failed"
	// KeyHookclientSendToSocketWriteFailed はsocket へ書き込めなかったときに出る。
	KeyHookclientSendToSocketWriteFailed Key = "hookclient.send_to_socket.write_failed"
)

// `continuo hook` の逃がし先への書き出し（internal/hookclient の spill /
// checkPendingCapacity）の文言。
const (
	// KeyHookclientSpillDirNotAbsolute は --pending-dir が絶対パスでないときに出る。
	KeyHookclientSpillDirNotAbsolute Key = "hookclient.spill.dir_not_absolute"
	// KeyHookclientSpillMkdirFailed は逃がし先のディレクトリを作れなかったときに出る。
	KeyHookclientSpillMkdirFailed Key = "hookclient.spill.mkdir_failed"
	// KeyHookclientSpillCreateFailed は書き込み中のファイル（.json.tmp）を
	// 作れなかったときに出る。
	KeyHookclientSpillCreateFailed Key = "hookclient.spill.create_failed"
	// KeyHookclientSpillWriteFailed は書き込み中のファイルへ書けなかったときに出る。
	KeyHookclientSpillWriteFailed Key = "hookclient.spill.write_failed"
	// KeyHookclientSpillRenameFailed は書き込み中のファイルを最終的な名前へ
	// 変えられなかったときに出る。
	KeyHookclientSpillRenameFailed Key = "hookclient.spill.rename_failed"
	// KeyHookclientSpillNameConflict はファイル名が続けてぶつかり、
	// 空きを見つけられなかったときに出る。
	KeyHookclientSpillNameConflict Key = "hookclient.spill.name_conflict"
	// KeyHookclientCheckPendingCapacityLimitReached は逃がし先が上限に達していて
	// これ以上書かないときに出る。
	KeyHookclientCheckPendingCapacityLimitReached Key = "hookclient.check_pending_capacity.limit_reached"
)

// 枠の判定に使う usage API の読み取り（internal/ratelimit）のエラーの文言。
const (
	// KeyRatelimitNewReaderHomeDirFailed は資格情報のファイルを探すための
	// ホームディレクトリを取得できなかったときに出る。
	KeyRatelimitNewReaderHomeDirFailed Key = "ratelimit.new_reader.home_dir_failed"

	// KeyRatelimitCredentialsFileNotExist は `token_source: claude_credentials` を
	// 選んだのに資格情報のファイルが無いときの警告である。
	// **起動は止めない**（設計 3-27）。代わりに、どう直せばよいかを必ず添える。
	KeyRatelimitCredentialsFileNotExist Key = "ratelimit.credentials_file.not_exist"

	// KeyRatelimitCredentialsRemedyKeychain は macOS での直し方である
	// （資格情報は Keychain にあるので `token_source: keychain` へ変える）。
	KeyRatelimitCredentialsRemedyKeychain Key = "ratelimit.credentials.remedy_keychain"

	// KeyRatelimitCredentialsRemedyEnv は macOS 以外での直し方である
	// （`token_source: env` にして環境変数から読む）。
	KeyRatelimitCredentialsRemedyEnv Key = "ratelimit.credentials.remedy_env"
	// KeyRatelimitFetchRequestBuildFailed はusage API のリクエストを組み立てられなかったときに出る。
	KeyRatelimitFetchRequestBuildFailed Key = "ratelimit.fetch.request_build_failed"
	// KeyRatelimitFetchRequestFailed はusage API へ接続できなかったときに出る。
	KeyRatelimitFetchRequestFailed Key = "ratelimit.fetch.request_failed"
	// KeyRatelimitFetchBodyReadFailed はusage API の応答の本文を読めなかったときに出る。
	KeyRatelimitFetchBodyReadFailed Key = "ratelimit.fetch.body_read_failed"
	// KeyRatelimitFetchUnexpectedStatus はusage API が 200 以外を返したときに出る。
	KeyRatelimitFetchUnexpectedStatus Key = "ratelimit.fetch.unexpected_status"
	// KeyRatelimitFetchParseFailed はusage API の応答を JSON として解析できなかったときに出る。
	KeyRatelimitFetchParseFailed Key = "ratelimit.fetch.parse_failed"
)

// 枠の判定に使う資格情報の取り出し（internal/ratelimit の token /
// tokenFromCredentialsFile）の文言。
//
// **どれも先頭の %w に ErrNoCredentials を渡す**（errors.Is の切り分けを保つため）。
const (
	// KeyRatelimitTokenEnvNameEmpty はrate_limit.token_env が空のときに出る。
	KeyRatelimitTokenEnvNameEmpty Key = "ratelimit.token.env_name_empty"
	// KeyRatelimitTokenEnvValueEmpty は資格情報を読む環境変数が空のときに出る。
	KeyRatelimitTokenEnvValueEmpty Key = "ratelimit.token.env_value_empty"
	// KeyRatelimitCredentialsFileHomeDirUnknown は資格情報のファイルの置き場所を
	// 決められないときに出る。
	KeyRatelimitCredentialsFileHomeDirUnknown Key = "ratelimit.credentials_file.home_dir_unknown"
	// KeyRatelimitCredentialsFileReadFailed は資格情報のファイルを読めなかったときに出る。
	KeyRatelimitCredentialsFileReadFailed Key = "ratelimit.credentials_file.read_failed"
	// KeyRatelimitCredentialsFileNotRegularFile は資格情報のファイルが通常のファイルで
	// なかったときに出る（symlink は辿らない）。
	KeyRatelimitCredentialsFileNotRegularFile Key = "ratelimit.credentials_file.not_regular_file"
	// KeyRatelimitCredentialsFileParseFailed は資格情報のファイルを JSON として
	// 解析できなかったときに出る。
	KeyRatelimitCredentialsFileParseFailed Key = "ratelimit.credentials_file.parse_failed"
	// KeyRatelimitCredentialsFileAccessTokenMissing は資格情報のファイルに
	// claudeAiOauth.accessToken が無いときに出る。
	KeyRatelimitCredentialsFileAccessTokenMissing Key = "ratelimit.credentials_file.access_token_missing"
)

// 枠の判定に使う資格情報を macOS の Keychain から読むとき（internal/ratelimit の
// tokenFromKeychain / ProbeKeychain）の文言。
//
// **どれも先頭の %w に ErrNoCredentials を渡す**（errors.Is の切り分けを保つため）。
// **どれにも読み取った値そのものを載せない。**
const (
	// KeyRatelimitKeychainBinaryNotFound はKeychain を読むコマンドが PATH に無いときに出る。
	KeyRatelimitKeychainBinaryNotFound Key = "ratelimit.keychain.binary_not_found"
	// KeyRatelimitKeychainTimeout は期限内にコマンドが返らなかったときに出る。
	KeyRatelimitKeychainTimeout Key = "ratelimit.keychain.timeout"
	// KeyRatelimitKeychainRunFailed はコマンドが異常終了したときに出る。
	KeyRatelimitKeychainRunFailed Key = "ratelimit.keychain.run_failed"
	// KeyRatelimitKeychainParseFailed はKeychain の中身を JSON として解析できなかったときに出る。
	KeyRatelimitKeychainParseFailed Key = "ratelimit.keychain.parse_failed"
	// KeyRatelimitKeychainOauthMissing はKeychain の中身に claudeAiOauth が無いときに出る。
	KeyRatelimitKeychainOauthMissing Key = "ratelimit.keychain.oauth_missing"
	// KeyRatelimitKeychainAccessTokenMissing はKeychain の中身に
	// claudeAiOauth.accessToken が無いときに出る。
	KeyRatelimitKeychainAccessTokenMissing Key = "ratelimit.keychain.access_token_missing"
)

// HTTP ダッシュボード（internal/server）の起動と停止のエラーの文言。
//
// **画面に並べる語は dashboard.* にある。**ここにあるのは、待ち受けの開始と停止、
// および応答の書き出しが失敗したときのものである。
const (
	// KeyServerNewPortOutOfRange はserver.port が 0〜65535 の外だったときに出る。
	KeyServerNewPortOutOfRange Key = "server.new.port_out_of_range"
	// KeyServerStartListenFailed は待ち受けを開始できなかったときに出る（ポートの重複など）。
	KeyServerStartListenFailed Key = "server.start.listen_failed"
	// KeyServerCloseShutdownFailed は待ち受けを閉じられなかったときに出る。
	KeyServerCloseShutdownFailed Key = "server.close.shutdown_failed"
	// KeyServerWriteJSONEncodeFailed は写しを JSON で書き出せなかったときに出る。
	KeyServerWriteJSONEncodeFailed Key = "server.write_json.encode_failed"
)

// WORKFLOW.md の読み書きそのもの（internal/scaffold）の失敗の文言。
//
// **`continuo init` と `continuo setup` の両方が同じ文言を使う。**
// 読む・確かめる・書く・閉じる・作るの5つは、どちらの経路でも同じ失敗である。
const (
	// KeyScaffoldFileReadFailed はWORKFLOW.md を読み込めなかったときに出る。
	KeyScaffoldFileReadFailed Key = "scaffold.file.read_failed"
	// KeyScaffoldFileStatFailed はWORKFLOW.md の有無を確かめられなかったときに出る。
	KeyScaffoldFileStatFailed Key = "scaffold.file.stat_failed"
	// KeyScaffoldFileWriteFailed はWORKFLOW.md へ書き込めなかったときに出る。
	KeyScaffoldFileWriteFailed Key = "scaffold.file.write_failed"
	// KeyScaffoldFileCloseFailed はWORKFLOW.md を閉じられなかったときに出る。
	KeyScaffoldFileCloseFailed Key = "scaffold.file.close_failed"
	// KeyScaffoldFileCreateFailed はWORKFLOW.md を作成できなかったときに出る。
	KeyScaffoldFileCreateFailed Key = "scaffold.file.create_failed"
)

// 書き出す先のディレクトリを決める処理（internal/scaffold の resolveTarget / resolveDir）の文言。
const (
	// KeyScaffoldDirStatFailed は指定されたディレクトリの有無を確かめられなかったときに出る。
	KeyScaffoldDirStatFailed Key = "scaffold.dir.stat_failed"
	// KeyScaffoldDirEvalSymlinksFailed はディレクトリの symlink を辿れなかったときに出る。
	KeyScaffoldDirEvalSymlinksFailed Key = "scaffold.dir.eval_symlinks_failed"
	// KeyScaffoldDirGetwdFailed はいまいるディレクトリを取得できなかったときに出る。
	KeyScaffoldDirGetwdFailed Key = "scaffold.dir.getwd_failed"
	// KeyScaffoldDirAbsFailed は渡されたパスを絶対パスへ直せなかったときに出る。
	KeyScaffoldDirAbsFailed Key = "scaffold.dir.abs_failed"
)

// `continuo init` が雛形を書き出すとき（internal/scaffold の openError）の文言。
//
// **先頭の %w に ErrSymlink を渡す**（errors.Is の切り分けを保つため）。
const (
	// KeyScaffoldWriteSymlinkNotFollowed は書き出す先が symlink で、辿らずに止めたときに出る。
	KeyScaffoldWriteSymlinkNotFollowed Key = "scaffold.write.symlink_not_followed"
)

// `continuo setup` が既にある WORKFLOW.md を書き換えるとき（internal/scaffold の
// statTarget / writeAtomically）の文言。
//
// **symlink_not_followed と not_regular_file は先頭の %w に ErrSymlink / ErrNotFound を渡す**
// （errors.Is の切り分けを保つため）。
const (
	// KeyScaffoldUpdateSymlinkNotFollowed は書き換える先が symlink で、辿らずに止めたときに出る。
	KeyScaffoldUpdateSymlinkNotFollowed Key = "scaffold.update.symlink_not_followed"
	// KeyScaffoldUpdateNotRegularFile は書き換える先が通常のファイルではなかったときに出る。
	KeyScaffoldUpdateNotRegularFile Key = "scaffold.update.not_regular_file"
	// KeyScaffoldUpdateTempCreateFailed は不可分に書き換えるための一時ファイルを作れなかったときに出る。
	KeyScaffoldUpdateTempCreateFailed Key = "scaffold.update.temp_create_failed"
	// KeyScaffoldUpdateChmodFailed は一時ファイルに元の権限を設定できなかったときに出る。
	KeyScaffoldUpdateChmodFailed Key = "scaffold.update.chmod_failed"
	// KeyScaffoldUpdateSyncFailed は一時ファイルをディスクへ書き出せなかったときに出る。
	KeyScaffoldUpdateSyncFailed Key = "scaffold.update.sync_failed"
	// KeyScaffoldUpdateRenameFailed は一時ファイルで WORKFLOW.md を置き換えられなかったときに出る。
	KeyScaffoldUpdateRenameFailed Key = "scaffold.update.rename_failed"
)

// gh コマンドの実行（internal/scaffold の RunGH）の文言。
const (
	// KeyScaffoldGHRunFailed はgh の実行に失敗し、標準エラー出力が空だったときに出る。
	KeyScaffoldGHRunFailed Key = "scaffold.gh.run_failed"
	// KeyScaffoldGHRunFailedWithStderr はgh の実行に失敗し、標準エラー出力があったときに出る。
	KeyScaffoldGHRunFailedWithStderr Key = "scaffold.gh.run_failed_with_stderr"
)

// ボードの Status フィールドを読む処理（internal/setup の FetchStatusField）の文言。
//
// **field_not_single_select と field_not_found は先頭の %w に ErrStatusFieldNotFound を渡す**
// （errors.Is の切り分けを保つため）。
const (
	// KeySetupBoardOwnerMissing はボードの owner が決まっていないときに出る。
	KeySetupBoardOwnerMissing Key = "setup.board.owner_missing"
	// KeySetupBoardProjectNumberMissing はボードの番号が決まっていないときに出る。
	KeySetupBoardProjectNumberMissing Key = "setup.board.project_number_missing"
	// KeySetupBoardFieldListUnparsable は`gh project field-list` の出力を解釈できなかったときに出る。
	KeySetupBoardFieldListUnparsable Key = "setup.board.field_list_unparsable"
	// KeySetupBoardFieldNotSingleSelect は名前は合っていても single-select でなかったときに出る。
	KeySetupBoardFieldNotSingleSelect Key = "setup.board.field_not_single_select"
	// KeySetupBoardFieldNotFound はその名前のフィールドがボードに無かったときに出る。
	KeySetupBoardFieldNotFound Key = "setup.board.field_not_found"
)

// `continuo trust` が `~/.claude.json` を読み書きするときの文言（internal/trust）。
const (
	// KeyTrustParseOrderedObjectInvalidJSON は `~/.claude.json` を JSON として読めなかったときに出る。
	KeyTrustParseOrderedObjectInvalidJSON Key = "trust.parse_ordered_object.invalid_json"
	// KeyTrustParseOrderedObjectNotObject はトップレベルが JSON のオブジェクトでなかったときに出る。
	KeyTrustParseOrderedObjectNotObject Key = "trust.parse_ordered_object.not_object"
	// KeyTrustParseOrderedObjectKeyUnreadable はオブジェクトのキーを読み取れなかったときに出る。
	KeyTrustParseOrderedObjectKeyUnreadable Key = "trust.parse_ordered_object.key_unreadable"
	// KeyTrustParseOrderedObjectKeyNotString はオブジェクトのキーが文字列でなかったときに出る。
	KeyTrustParseOrderedObjectKeyNotString Key = "trust.parse_ordered_object.key_not_string"
	// KeyTrustParseOrderedObjectValueUnreadable はキーに対応する値を読み取れなかったときに出る。
	KeyTrustParseOrderedObjectValueUnreadable Key = "trust.parse_ordered_object.value_unreadable"
	// KeyTrustParseOrderedObjectObjectNotClosed はオブジェクトの閉じ括弧まで読み切れなかったときに出る。
	KeyTrustParseOrderedObjectObjectNotClosed Key = "trust.parse_ordered_object.object_not_closed"
	// KeyTrustMarshalIndentKeyEncodeFailed は書き戻すときにキーを JSON の文字列へ直せなかったときに出る。
	KeyTrustMarshalIndentKeyEncodeFailed Key = "trust.marshal_indent.key_encode_failed"
	// KeyTrustMarshalIndentValueIndentFailed は書き戻すときに値を字下げつきで並べ直せなかったときに出る。
	KeyTrustMarshalIndentValueIndentFailed Key = "trust.marshal_indent.value_indent_failed"
	// KeyTrustRunGitToplevelRunFailed は git を実行できなかった・非 0 で終わったときに出る（標準エラー出力が空の場合）。
	KeyTrustRunGitToplevelRunFailed Key = "trust.run_git_toplevel.run_failed"
	// KeyTrustRunGitToplevelRunFailedWithStderr は同じときに、git の標準エラー出力を添えて出る。
	KeyTrustRunGitToplevelRunFailedWithStderr Key = "trust.run_git_toplevel.run_failed_with_stderr"
	// KeyTrustRunGitToplevelEmptyOutput は git が 0 で終わったのに何も返さなかったときに出る。
	KeyTrustRunGitToplevelEmptyOutput Key = "trust.run_git_toplevel.empty_output"
	// KeyTrustOptionsHomeDirNotAbsolute は Plan / Apply に渡されたホームディレクトリが絶対パスでないときに出る。
	KeyTrustOptionsHomeDirNotAbsolute Key = "trust.options.home_dir_not_absolute"
	// KeyTrustApplyProjectsMarshalFailed は projects を JSON へ組み立て直せなかったときに出る。
	KeyTrustApplyProjectsMarshalFailed Key = "trust.apply.projects_marshal_failed"
	// KeyTrustApplyRootMarshalFailed はトップレベル全体を JSON へ組み立て直せなかったときに出る。
	KeyTrustApplyRootMarshalFailed Key = "trust.apply.root_marshal_failed"
	// KeyTrustApplyReplaceFailed は書き換えた中身でファイルを置き換えられなかったときに出る（バックアップの場所を添える）。
	KeyTrustApplyReplaceFailed Key = "trust.apply.replace_failed"
	// KeyTrustProjectsObjectUnparsable は projects がオブジェクトとして読めなかったときに出る。
	KeyTrustProjectsObjectUnparsable Key = "trust.projects_object.unparsable"
	// KeyTrustMarkTrustedEntryMarshalFailed は新しく足す1件の記述を組み立てられなかったときに出る。
	KeyTrustMarkTrustedEntryMarshalFailed Key = "trust.mark_trusted.entry_marshal_failed"
	// KeyTrustMarkTrustedEntryUnparsable は既にある1件の記述がオブジェクトとして読めなかったときに出る。
	KeyTrustMarkTrustedEntryUnparsable Key = "trust.mark_trusted.entry_unparsable"
	// KeyTrustMarkTrustedFlagNotBool は信頼の承認を表すキーの値が真偽値でなかったときに出る。
	KeyTrustMarkTrustedFlagNotBool Key = "trust.mark_trusted.flag_not_bool"
	// KeyTrustMarkTrustedEntryRemarshalFailed は既にある1件の記述を組み立て直せなかったときに出る。
	KeyTrustMarkTrustedEntryRemarshalFailed Key = "trust.mark_trusted.entry_remarshal_failed"
	// KeyTrustReadClaudeConfigNotFound は `~/.claude.json` が無いときに出る（continuo はこのファイルを作らない）。
	KeyTrustReadClaudeConfigNotFound Key = "trust.read_claude_config.not_found"
	// KeyTrustReadClaudeConfigStatFailed は `~/.claude.json` の有無を確かめられなかったときに出る。
	KeyTrustReadClaudeConfigStatFailed Key = "trust.read_claude_config.stat_failed"
	// KeyTrustReadClaudeConfigSymlink は `~/.claude.json` が symlink だったときに出る（辿った先を置き換えないため書かない）。
	KeyTrustReadClaudeConfigSymlink Key = "trust.read_claude_config.symlink"
	// KeyTrustReadClaudeConfigNotRegularFile は `~/.claude.json` が通常のファイルでなかったときに出る。
	KeyTrustReadClaudeConfigNotRegularFile Key = "trust.read_claude_config.not_regular_file"
	// KeyTrustReadClaudeConfigReadFailed は `~/.claude.json` を読めなかったときに出る。
	KeyTrustReadClaudeConfigReadFailed Key = "trust.read_claude_config.read_failed"
	// KeyTrustWriteBackupCreateFailed はバックアップのファイルを作れなかったときに出る（このとき元のファイルは書き換えない）。
	KeyTrustWriteBackupCreateFailed Key = "trust.write_backup.create_failed"
	// KeyTrustWriteBackupWriteFailed はバックアップへ書き込めなかったときに出る。
	KeyTrustWriteBackupWriteFailed Key = "trust.write_backup.write_failed"
	// KeyTrustWriteBackupSyncFailed はバックアップをディスクへ書き出せなかったときに出る。
	KeyTrustWriteBackupSyncFailed Key = "trust.write_backup.sync_failed"
	// KeyTrustWriteBackupCloseFailed はバックアップを閉じられなかったときに出る。
	KeyTrustWriteBackupCloseFailed Key = "trust.write_backup.close_failed"
	// KeyTrustReplaceFileTempCreateFailed は置き換えに使う一時ファイルを作れなかったときに出る。
	KeyTrustReplaceFileTempCreateFailed Key = "trust.replace_file.temp_create_failed"
	// KeyTrustReplaceFileChmodFailed は一時ファイルの権限を元のファイルにそろえられなかったときに出る。
	KeyTrustReplaceFileChmodFailed Key = "trust.replace_file.chmod_failed"
	// KeyTrustReplaceFileWriteFailed は一時ファイルへ書き込めなかったときに出る。
	KeyTrustReplaceFileWriteFailed Key = "trust.replace_file.write_failed"
	// KeyTrustReplaceFileSyncFailed は一時ファイルをディスクへ書き出せなかったときに出る。
	KeyTrustReplaceFileSyncFailed Key = "trust.replace_file.sync_failed"
	// KeyTrustReplaceFileCloseFailed は一時ファイルを閉じられなかったときに出る。
	KeyTrustReplaceFileCloseFailed Key = "trust.replace_file.close_failed"
	// KeyTrustReplaceFileRenameFailed は一時ファイルで元のファイルを置き換えられなかったときに出る。
	KeyTrustReplaceFileRenameFailed Key = "trust.replace_file.rename_failed"
)

// internal/workspace のエラー文（worktree の用意・片付け・身元ファイル・git / ghq の実行）。
const (
	// KeyWorkspaceRunHookOutputFileCreateFailed は workspace_hooks の出力を受け取る一時ファイルを作れなかったときに出る。
	KeyWorkspaceRunHookOutputFileCreateFailed Key = "workspace.run_hook.output_file_create_failed"
	// KeyWorkspaceRunHookRunFailed は workspace_hooks のコマンドが非 0 で終わった・起動できなかった・時間切れになったときに出る。
	KeyWorkspaceRunHookRunFailed Key = "workspace.run_hook.run_failed"
	// KeyWorkspaceResolveWorkspaceIDPathMismatch は herdr が答えた worktree のパスが、消そうとしているパスと違ったときに出る。
	KeyWorkspaceResolveWorkspaceIDPathMismatch Key = "workspace.resolve_workspace_id.path_mismatch"
	// KeyWorkspaceRemoveWorktreeWorkspaceIDUnknown は消す herdr workspace の ID を確かめられなかったときに出る。
	KeyWorkspaceRemoveWorktreeWorkspaceIDUnknown Key = "workspace.remove_worktree.workspace_id_unknown"
	// KeyWorkspaceRemoveWorktreeWorktreeRemoveFailed は herdr の worktree.remove に失敗したときに出る。
	KeyWorkspaceRemoveWorktreeWorktreeRemoveFailed Key = "workspace.remove_worktree.worktree_remove_failed"
	// KeyWorkspaceRemoveWorktreeByHandFailed は、git も herdr も断ったあとに worktree の実体を
	// 自分で消そうとして、それも失敗したときに出る。
	KeyWorkspaceRemoveWorktreeByHandFailed Key = "workspace.remove_worktree.by_hand_failed"
	// KeyWorkspaceRemoveWorktreeStillThere は、削除の要求が断られていないのに worktree の
	// 実体が残っていたときに出る。
	KeyWorkspaceRemoveWorktreeStillThere Key = "workspace.remove_worktree.still_there"
	// KeyWorkspaceRemoveWorktreeRepoUnknown は、worktree が属するリポジトリを名指しできず、
	// `git worktree remove` を撃てなかったときに出る。
	KeyWorkspaceRemoveWorktreeRepoUnknown Key = "workspace.remove_worktree.repo_unknown"

	// ここから下は CleanupResult.Leftovers に積む文である。
	// **ログではなく、人間の画面に1行ずつ出る**（issue #23）。

	// KeyWorkspaceIssueBranchCloneNotFound は、残った branch を調べようとしたが
	// ghq に対象リポジトリの clone が無かったときに出る。
	KeyWorkspaceIssueBranchCloneNotFound Key = "workspace.issue_branch.clone_not_found"
	// KeyWorkspaceLeftoverPruneSkipped は、実体の無い worktree の登録がほかにもあるので
	// `git worktree prune` を撃たなかったときに出る（3-37-9b）。
	KeyWorkspaceLeftoverPruneSkipped Key = "workspace.leftover.prune_skipped"
	// KeyWorkspaceIssueBranchUsedByWorktree は、残った branch を消そうとしたが
	// git が「worktree が使っている」と断ったときに出る（3-37-9b）。
	// **continuo は `git worktree prune` を代行しない。**叩くコマンドを案内するだけである。
	KeyWorkspaceIssueBranchUsedByWorktree Key = "workspace.issue_branch.used_by_worktree"
	// KeyWorkspaceLeftoverBranchDisabled は cleanup.delete_branch が偽で branch を残したときに出る。
	KeyWorkspaceLeftoverBranchDisabled Key = "workspace.leftover.branch_disabled"
	// KeyWorkspaceLeftoverBranchUndeletable は、消してよい branch だと検算できずに残したときに出る。
	KeyWorkspaceLeftoverBranchUndeletable Key = "workspace.leftover.branch_undeletable"
	// KeyWorkspaceLeftoverBranchDeleteFailed は `git branch -D` が失敗して branch が残ったときに出る。
	KeyWorkspaceLeftoverBranchDeleteFailed Key = "workspace.leftover.branch_delete_failed"
	// KeyWorkspaceLeftoverPruneFailed は、実体は消したのに git の worktree の登録を掃除できなかったときに出る。
	KeyWorkspaceLeftoverPruneFailed Key = "workspace.leftover.prune_failed"
	// KeyWorkspaceLeftoverPruneRepoUnknown は、リポジトリを名指しできず git の worktree の登録が残ったときに出る。
	KeyWorkspaceLeftoverPruneRepoUnknown Key = "workspace.leftover.prune_repo_unknown"
	// KeyWorkspaceLeftoverWorkspaceListFailed は、herdr の workspace の一覧を引けず、
	// 閉じるべき workspace を名指しできなかったときに出る。
	KeyWorkspaceLeftoverWorkspaceListFailed Key = "workspace.leftover.workspace_list_failed"
	// KeyWorkspaceLeftoverWorkspaceCloseFailed は、herdr の workspace を閉じられなかったときに出る。
	KeyWorkspaceLeftoverWorkspaceCloseFailed Key = "workspace.leftover.workspace_close_failed"
	// KeyWorkspaceLeftoverBranchReasonNoIdentity は、身元ファイルに branch が書いていないことを表す。
	KeyWorkspaceLeftoverBranchReasonNoIdentity Key = "workspace.leftover.branch_reason.no_identity"
	// KeyWorkspaceLeftoverBranchReasonRepoUnknown は、リポジトリを名指しできないことを表す。
	KeyWorkspaceLeftoverBranchReasonRepoUnknown Key = "workspace.leftover.branch_reason.repo_unknown"
	// KeyWorkspaceLeftoverBranchReasonNormalized は、branch 名が正規化で変わることを表す。
	KeyWorkspaceLeftoverBranchReasonNormalized Key = "workspace.leftover.branch_reason.normalized"
	// KeyWorkspaceLeftoverBranchReasonNoPrefix は、branch_template に変数が無く接頭辞を決められないことを表す。
	KeyWorkspaceLeftoverBranchReasonNoPrefix Key = "workspace.leftover.branch_reason.no_prefix"
	// KeyWorkspaceLeftoverBranchReasonPrefixMismatch は、branch が continuo の接頭辞で始まらないことを表す。
	KeyWorkspaceLeftoverBranchReasonPrefixMismatch Key = "workspace.leftover.branch_reason.prefix_mismatch"
	// KeyWorkspaceLeftoverBranchReasonHeadUnreadable は、worktree がチェックアウトしている branch を引けないことを表す。
	KeyWorkspaceLeftoverBranchReasonHeadUnreadable Key = "workspace.leftover.branch_reason.head_unreadable"
	// KeyWorkspaceLeftoverBranchReasonHeadMismatch は、身元ファイルの branch が現物と一致しないことを表す。
	KeyWorkspaceLeftoverBranchReasonHeadMismatch Key = "workspace.leftover.branch_reason.head_mismatch"
	// KeyWorkspaceGitWorktreeBranchAtNotRegistered は、リポジトリ側の worktree の一覧に
	// その worktree が載っていなかったときに出る。
	KeyWorkspaceGitWorktreeBranchAtNotRegistered Key = "workspace.git_worktree_branch_at.not_registered"
	// KeyWorkspaceVerifiedRepoCommonDirUnreadable は、worktree からも clone からも
	// git の共通ディレクトリを引けなかったときに出る。
	KeyWorkspaceVerifiedRepoCommonDirUnreadable Key = "workspace.verified_repo.common_dir_unreadable"
	// KeyWorkspaceUndeterminedDirty は、コミットされていない変更を git に数えさせられなかったときに出る。
	KeyWorkspaceUndeterminedDirty Key = "workspace.undetermined.dirty"
	// KeyWorkspaceUndeterminedUnpushed は、push されていない成果があるかを git に判定させられなかったときに出る。
	KeyWorkspaceUndeterminedUnpushed Key = "workspace.undetermined.unpushed"
	// KeyWorkspaceRunGitOutputTooLarge は git の標準出力が上限を超えて読み切れなかったときに出る。
	KeyWorkspaceRunGitOutputTooLarge Key = "workspace.run_git.output_too_large"
	// KeyWorkspaceRunGitRunFailed は git が非 0 で終わった・起動できなかったときに出る。
	KeyWorkspaceRunGitRunFailed Key = "workspace.run_git.run_failed"
	// KeyWorkspaceGitExitCodeStartFailed は終了コードだけを見る検査で git を起動できなかったときに出る。
	KeyWorkspaceGitExitCodeStartFailed Key = "workspace.git_exit_code.start_failed"
	// KeyWorkspaceGitWorktreeListOutputUnreadable は `git worktree list --porcelain` の出力を読み切れなかったときに出る。
	KeyWorkspaceGitWorktreeListOutputUnreadable Key = "workspace.git_worktree_list.output_unreadable"
	// KeyWorkspaceGitWorktreeAddOrphanCheckFailed は worktree の作成に失敗したあと、孤児 branch が残っているかを確かめられなかったときに出る。
	KeyWorkspaceGitWorktreeAddOrphanCheckFailed Key = "workspace.git_worktree_add.orphan_check_failed"
	// KeyWorkspaceGitWorktreeAddOrphanDeleteFailed は孤児 branch を消せなかったときに出る。
	KeyWorkspaceGitWorktreeAddOrphanDeleteFailed Key = "workspace.git_worktree_add.orphan_delete_failed"
	// KeyWorkspaceGitWorktreeAddOrphanDeleted は孤児 branch を消せたときに、元の失敗へ添えて出る。
	KeyWorkspaceGitWorktreeAddOrphanDeleted Key = "workspace.git_worktree_add.orphan_deleted"
	// KeyWorkspaceBrokenRefStatFailed は壊れた ref のファイルの状態を見に行けなかったときに出る。
	KeyWorkspaceBrokenRefStatFailed Key = "workspace.broken_ref.stat_failed"
	// KeyWorkspaceBrokenRefRemoveFailed は壊れた ref のファイルを消せなかったときに出る。
	KeyWorkspaceBrokenRefRemoveFailed Key = "workspace.broken_ref.remove_failed"
	// KeyWorkspaceBrokenRefResolveFailed は ref のパスのシンボリックリンクを解決できなかったときに出る。
	KeyWorkspaceBrokenRefResolveFailed Key = "workspace.broken_ref.resolve_failed"
	// KeyWorkspaceBrokenRefRemoved は壊れた ref のファイルを消したことを人間の画面へ出す。
	KeyWorkspaceBrokenRefRemoved Key = "workspace.broken_ref.removed"
	// KeyWorkspaceBrokenRefRemovedWithTip は壊れた ref のファイルを消したことを、
	// 消す前に指していた commit と戻し方を添えて人間の画面へ出す。
	KeyWorkspaceBrokenRefRemovedWithTip Key = "workspace.broken_ref.removed_with_tip"
	// KeyWorkspaceBrokenRefBranchCheckFailed は壊れた ref を消したあとに branch の有無を確かめられなかったときに出る。
	KeyWorkspaceBrokenRefBranchCheckFailed Key = "workspace.broken_ref.branch_check_failed"
	// KeyWorkspaceBrokenRefBranchSurvived は壊れた ref を消しても branch が残っているときに出る
	// （packed-refs 側の同名の ref が生き返った場合）。
	KeyWorkspaceBrokenRefBranchSurvived Key = "workspace.broken_ref.branch_survived"
	// KeyWorkspaceWorktreeHeadRefsUnreadable は worktree の HEAD の一覧を読めなかったときに出る。
	KeyWorkspaceWorktreeHeadRefsUnreadable Key = "workspace.worktree_head_refs.unreadable"
	// KeyWorkspaceGitAheadOfUpstreamCountUnreadable は upstream より先にある commit の数を数値として読めなかったときに出る。
	KeyWorkspaceGitAheadOfUpstreamCountUnreadable Key = "workspace.git_ahead_of_upstream.count_unreadable"
	// KeyWorkspaceGitNoDiffFromBaseUnexpectedExitCode は `git diff --quiet` が 0 でも 1 でもない終了コードを返したときに出る。
	KeyWorkspaceGitNoDiffFromBaseUnexpectedExitCode Key = "workspace.git_no_diff_from_base.unexpected_exit_code"
	// KeyWorkspaceGitBranchExistsUnexpectedExitCode は `git show-ref --verify` が
	// 0 でも 1 でもない終了コードを返したときに出る。**「無い」に丸めないためのものである。**
	KeyWorkspaceGitBranchExistsUnexpectedExitCode Key = "workspace.git_branch_exists.unexpected_exit_code"
	// KeyWorkspaceGitUnpushedCountUnreadable は、どの remote にも載っていない commit の数を
	// 数値として読めなかったときに出る。
	KeyWorkspaceGitUnpushedCountUnreadable Key = "workspace.git_unpushed_commits.count_unreadable"
	// KeyWorkspaceGhqNameInvalid は ghq へ渡す owner 名またはリポジトリ名が
	// GitHub の名前として通らない形だったときに出る。**別名に直さずに断る**ためのものである。
	KeyWorkspaceGhqNameInvalid Key = "workspace.ghq_target.name_invalid"
	// KeyWorkspaceRunGhqListStartFailed は `ghq list` を起動できなかったときに出る。
	KeyWorkspaceRunGhqListStartFailed Key = "workspace.run_ghq_list.start_failed"
	// KeyWorkspaceRunGhqListExitFailed は `ghq list` が「該当が無い」以外の理由で非 0 で終わったときに出る。
	KeyWorkspaceRunGhqListExitFailed Key = "workspace.run_ghq_list.exit_failed"
	// KeyWorkspaceGitLocalBranchesOutputUnreadable は `git for-each-ref` の出力を読み切れなかったときに出る。
	KeyWorkspaceGitLocalBranchesOutputUnreadable Key = "workspace.git_local_branches.output_unreadable"
	// KeyWorkspaceRunGhqGetStartFailed は `ghq get` を起動できなかったときに出る。
	KeyWorkspaceRunGhqGetStartFailed Key = "workspace.run_ghq_get.start_failed"
	// KeyWorkspaceRunGhqGetExitFailed は `ghq get` が非 0 で終わったときに出る。
	KeyWorkspaceRunGhqGetExitFailed Key = "workspace.run_ghq_get.exit_failed"
	// KeyWorkspaceOwnerRepoFromWorktreePathRelFailed は worktree のパスを置き場所からの相対パスにできなかったときに出る。
	KeyWorkspaceOwnerRepoFromWorktreePathRelFailed Key = "workspace.owner_repo_from_worktree_path.rel_failed"
	// KeyWorkspaceOwnerRepoFromWorktreePathLayoutMismatch は worktree のパスが置き場所の4階層の規則に合わなかったときに出る。
	KeyWorkspaceOwnerRepoFromWorktreePathLayoutMismatch Key = "workspace.owner_repo_from_worktree_path.layout_mismatch"
	// KeyWorkspaceVerifiedRepoCloneNotFound は worktree が属するリポジトリの clone が手元に無く、検算できなかったときに出る。
	KeyWorkspaceVerifiedRepoCloneNotFound Key = "workspace.verified_repo.clone_not_found"
	// KeyWorkspaceVerifiedRepoRepoMismatch は git が答えたリポジトリと ghq が答えた clone が食い違ったときに出る。
	KeyWorkspaceVerifiedRepoRepoMismatch Key = "workspace.verified_repo.repo_mismatch"
	// KeyWorkspaceEnsureRootRootEmpty は workspace.root が空だったときに出る。
	KeyWorkspaceEnsureRootRootEmpty Key = "workspace.ensure_root.root_empty"
	// KeyWorkspaceEnsureRootRootNotAbsolute は workspace.root が絶対パスでなかったときに出る。
	KeyWorkspaceEnsureRootRootNotAbsolute Key = "workspace.ensure_root.root_not_absolute"
	// KeyWorkspaceEnsureRootMkdirFailed は workspace.root を作れなかったときに出る。
	KeyWorkspaceEnsureRootMkdirFailed Key = "workspace.ensure_root.mkdir_failed"
	// KeyWorkspaceEnsureRootSymlinkUnresolvable は workspace.root のシンボリックリンクを解決できなかったときに出る。
	KeyWorkspaceEnsureRootSymlinkUnresolvable Key = "workspace.ensure_root.symlink_unresolvable"
	// KeyWorkspaceLabelWorktreePath は封じ込め検査のエラー文で worktree のパスを指す呼び名である。
	KeyWorkspaceLabelWorktreePath Key = "workspace.label.worktree_path"
	// KeyWorkspaceLabelIssueSettingsFile は封じ込め検査のエラー文で issue ごとの設定ファイルを指す呼び名である。
	KeyWorkspaceLabelIssueSettingsFile Key = "workspace.label.issue_settings_file"
	// KeyWorkspaceCheckUnderNotAbsolute は封じ込め検査の対象が絶対パスでなかったときに出る。
	KeyWorkspaceCheckUnderNotAbsolute Key = "workspace.check_under.not_absolute"
	// KeyWorkspaceCheckUnderSameAsRoot は封じ込め検査の対象が置き場所そのものだったときに出る。
	KeyWorkspaceCheckUnderSameAsRoot Key = "workspace.check_under.same_as_root"
	// KeyWorkspaceCheckUnderOutsideRoot は封じ込め検査の対象が置き場所の外側にあったときに出る。
	KeyWorkspaceCheckUnderOutsideRoot Key = "workspace.check_under.outside_root"
	// KeyWorkspaceCheckContainmentResolvedSymlinkUnresolvable は作ったあとの worktree のシンボリックリンクを解決できなかったときに出る。
	KeyWorkspaceCheckContainmentResolvedSymlinkUnresolvable Key = "workspace.check_containment_resolved.symlink_unresolvable"
	// KeyWorkspaceCheckContainmentResolvedOutsideRoot は解決したあとの worktree の実体が置き場所の外側にあったときに出る。
	KeyWorkspaceCheckContainmentResolvedOutsideRoot Key = "workspace.check_containment_resolved.outside_root"
	// KeyWorkspaceRenderBranchTemplateUnparsable は branch 名のテンプレートを解析できなかったときに出る。
	KeyWorkspaceRenderBranchTemplateUnparsable Key = "workspace.render_branch.template_unparsable"
	// KeyWorkspaceRenderBranchTemplateRenderFailed は branch 名のテンプレートを描画できなかったときに出る（未知の変数を含む）。
	KeyWorkspaceRenderBranchTemplateRenderFailed Key = "workspace.render_branch.template_render_failed"
	// KeyWorkspaceRenderBranchRenderedEmpty は branch 名のテンプレートの描画結果が空だったときに出る。
	KeyWorkspaceRenderBranchRenderedEmpty Key = "workspace.render_branch.rendered_empty"
	// KeyWorkspaceScanLevelRootUnreadable は置き場所の最上位を読めなかったときに出る。
	KeyWorkspaceScanLevelRootUnreadable Key = "workspace.scan_level.root_unreadable"
	// KeyWorkspaceCheckTrustForClonePathToplevelFailed は clone のパスから信頼を引く鍵を求められなかったときに出る。
	KeyWorkspaceCheckTrustForClonePathToplevelFailed Key = "workspace.check_trust_for_clone_path.toplevel_failed"
	// KeyWorkspaceCheckTrustForClonePathConfigUnreadable は `~/.claude.json` を読めなかったときに出る（存在しない場合は除く）。
	KeyWorkspaceCheckTrustForClonePathConfigUnreadable Key = "workspace.check_trust_for_clone_path.config_unreadable"
	// KeyWorkspaceCheckTrustForClonePathConfigUnparsable は `~/.claude.json` を JSON として解析できなかったときに出る。
	KeyWorkspaceCheckTrustForClonePathConfigUnparsable Key = "workspace.check_trust_for_clone_path.config_unparsable"
	// KeyWorkspaceValidateIdentityFileNameEmpty は workspace.identity_file が空だったときに出る。
	KeyWorkspaceValidateIdentityFileNameEmpty Key = "workspace.validate_identity_file_name.empty"
	// KeyWorkspaceValidateIdentityFileNameHasSpaces は workspace.identity_file の前後に空白があったときに出る。
	KeyWorkspaceValidateIdentityFileNameHasSpaces Key = "workspace.validate_identity_file_name.has_spaces"
	// KeyWorkspaceValidateIdentityFileNameHasSeparator は workspace.identity_file にパスの区切りが入っていたときに出る。
	KeyWorkspaceValidateIdentityFileNameHasSeparator Key = "workspace.validate_identity_file_name.has_separator"
	// KeyWorkspaceValidateIdentityFileNameNotAFileName は workspace.identity_file が `.` や `..` などファイルの名前でなかったときに出る。
	KeyWorkspaceValidateIdentityFileNameNotAFileName Key = "workspace.validate_identity_file_name.not_a_file_name"
	// KeyWorkspaceReadIdentityReadFailed は身元ファイルを読めなかったときに出る（存在しない場合は除く）。
	KeyWorkspaceReadIdentityReadFailed Key = "workspace.read_identity.read_failed"
	// KeyWorkspaceReadIdentityNotRegular は身元ファイルが通常のファイルでなかったときに出る
	// （symlink やディレクトリ）。
	KeyWorkspaceReadIdentityNotRegular Key = "workspace.read_identity.not_regular"
	// KeyWorkspaceReadIdentityTooLarge は身元ファイルが上限を超えていたときに出る。
	KeyWorkspaceReadIdentityTooLarge Key = "workspace.read_identity.too_large"
	// KeyWorkspaceWriteIdentityMarshalFailed は身元ファイルの中身を JSON にできなかったときに出る。
	KeyWorkspaceWriteIdentityMarshalFailed Key = "workspace.write_identity.marshal_failed"
	// KeyWorkspaceWriteFileAtomicTempCreateFailed は置き換え用の一時ファイルを作れなかったときに出る。
	KeyWorkspaceWriteFileAtomicTempCreateFailed Key = "workspace.write_file_atomic.temp_create_failed"
	// KeyWorkspaceWriteFileAtomicWriteFailed は置き換え用の一時ファイルへ書き込めなかったときに出る。
	KeyWorkspaceWriteFileAtomicWriteFailed Key = "workspace.write_file_atomic.write_failed"
	// KeyWorkspaceWriteFileAtomicChmodFailed は置き換え用の一時ファイルの権限を設定できなかったときに出る。
	KeyWorkspaceWriteFileAtomicChmodFailed Key = "workspace.write_file_atomic.chmod_failed"
	// KeyWorkspaceWriteFileAtomicCloseFailed は置き換え用の一時ファイルを閉じられなかったときに出る。
	KeyWorkspaceWriteFileAtomicCloseFailed Key = "workspace.write_file_atomic.close_failed"
	// KeyWorkspaceWriteFileAtomicRenameFailed は一時ファイルで元のファイルを置き換えられなかったときに出る。
	KeyWorkspaceWriteFileAtomicRenameFailed Key = "workspace.write_file_atomic.rename_failed"
	// KeyWorkspaceRegisterExcludeInfoDirCreateFailed は共通ディレクトリの info ディレクトリを作れなかったときに出る。
	KeyWorkspaceRegisterExcludeInfoDirCreateFailed Key = "workspace.register_exclude.info_dir_create_failed"
	// KeyWorkspaceRegisterExcludeReadFailed は `info/exclude` を読めなかったときに出る（存在しない場合は除く）。
	KeyWorkspaceRegisterExcludeReadFailed Key = "workspace.register_exclude.read_failed"
	// KeyWorkspaceRegisterExcludeOpenFailed は `info/exclude` を追記のために開けなかったときに出る。
	KeyWorkspaceRegisterExcludeOpenFailed Key = "workspace.register_exclude.open_failed"
	// KeyWorkspaceRegisterExcludeWriteFailed は `info/exclude` へ書き込めなかったときに出る。
	KeyWorkspaceRegisterExcludeWriteFailed Key = "workspace.register_exclude.write_failed"
	// KeyWorkspaceRegisterExcludeCloseFailed は `info/exclude` を閉じられなかったときに出る。
	KeyWorkspaceRegisterExcludeCloseFailed Key = "workspace.register_exclude.close_failed"
	// KeyWorkspaceNewSettingsRootNotAbsolute は issue ごとの設定ファイルの置き場所が絶対パスでなかったときに出る。
	KeyWorkspaceNewSettingsRootNotAbsolute Key = "workspace.new.settings_root_not_absolute"
	// KeyWorkspaceNewHomeDirUnknown はホームディレクトリを特定できなかったときに出る。
	KeyWorkspaceNewHomeDirUnknown Key = "workspace.new.home_dir_unknown"
	// KeyWorkspacePrepareCloneNotFound は対象リポジトリの clone が手元に無いときに出る。
	KeyWorkspacePrepareCloneNotFound Key = "workspace.prepare.clone_not_found"
	// KeyWorkspacePrepareStatFailed は worktree のパスの存在を確かめられなかったときに出る。
	KeyWorkspacePrepareStatFailed Key = "workspace.prepare.stat_failed"
	// KeyWorkspacePrepareBranchInUseElsewhere は目的の branch を、目的のパス以外の worktree が既に使っていたときに出る。
	KeyWorkspacePrepareBranchInUseElsewhere Key = "workspace.prepare.branch_in_use_elsewhere"
	// KeyWorkspacePrepareBranchMismatch は再利用しようとした worktree が別の branch をチェックアウトしていたときに出る。
	KeyWorkspacePrepareBranchMismatch Key = "workspace.prepare.branch_mismatch"
	// KeyWorkspacePrepareUnregisteredWorktree は目的のパスに実体があるのに git の worktree として登録されていなかったときに出る。
	KeyWorkspacePrepareUnregisteredWorktree Key = "workspace.prepare.unregistered_worktree"
	// KeyWorkspacePrepareParentDirCreateFailed は worktree の親ディレクトリを作れなかったときに出る。
	KeyWorkspacePrepareParentDirCreateFailed Key = "workspace.prepare.parent_dir_create_failed"
	// KeyWorkspacePrepareWorktreeOpenFailed は herdr の worktree.open に失敗したときに出る。
	KeyWorkspacePrepareWorktreeOpenFailed Key = "workspace.prepare.worktree_open_failed"

	// KeyWorkspacePrepareWorktreePathMismatch は、herdr が別の場所を開いたことを表す。
	KeyWorkspacePrepareWorktreePathMismatch Key = "workspace.prepare.worktree_path_mismatch"
	// KeyWorkspaceResolveBaseDefaultBranchMissing は base を決める手掛かりが設定にもボードの応答にも無かったときに出る。
	KeyWorkspaceResolveBaseDefaultBranchMissing Key = "workspace.resolve_base.default_branch_missing"
	// KeyWorkspaceResolveBaseDefaultBranchNotString はボードが返した既定 branch が文字列でなかったときに出る。
	KeyWorkspaceResolveBaseDefaultBranchNotString Key = "workspace.resolve_base.default_branch_not_string"
)

// orchestrator のエラー（着手・pane の解決・起動の確認・transcript の読み取り）。
const (
	// KeyOrchestratorNewExecutablePathUnknown はcontinuo 自身の実行ファイルの場所を決められなかったときに出る（hook のコマンド行に書けない）。
	KeyOrchestratorNewExecutablePathUnknown Key = "orchestrator.new.executable_path_unknown"
	// KeyOrchestratorResolveAgentNameAgentListFailed はherdr の agent の一覧を読めず、agent 名の重複を検査できなかったときに出る。
	KeyOrchestratorResolveAgentNameAgentListFailed Key = "orchestrator.resolve_agent_name.agent_list_failed"
	// KeyOrchestratorResolveAgentNameInvalidName は組み立てた agent 名が herdr の許容パターンに収まらなかったときに出る。
	KeyOrchestratorResolveAgentNameInvalidName Key = "orchestrator.resolve_agent_name.invalid_name"
	// KeyOrchestratorResolveAgentNameNoFreeName は連番を試し尽くしても空いている agent 名が見つからなかったときに出る。
	KeyOrchestratorResolveAgentNameNoFreeName Key = "orchestrator.resolve_agent_name.no_free_name"
	// KeyOrchestratorNewSessionUUIDRandFailed はセッション UUID を作るための乱数を取得できなかったときに出る。
	KeyOrchestratorNewSessionUUIDRandFailed Key = "orchestrator.new_session_uuid.rand_failed"
	// KeyOrchestratorRenderFirstPromptTemplateUnparsable は1回目のプロンプトのテンプレートの構文が誤っていたときに出る。
	KeyOrchestratorRenderFirstPromptTemplateUnparsable Key = "orchestrator.render_first_prompt.template_unparsable"
	// KeyOrchestratorRenderFirstPromptRenderFailed は1回目のプロンプトの描画に失敗したときに出る（一覧に無い変数を参照している場合を含む）。
	KeyOrchestratorRenderFirstPromptRenderFailed Key = "orchestrator.render_first_prompt.render_failed"
	// KeyOrchestratorWriteSettingsFileDirCreateFailed はissue ごとの設定ファイルの置き場所を作れなかったときに出る。
	KeyOrchestratorWriteSettingsFileDirCreateFailed Key = "orchestrator.write_settings_file.dir_create_failed"
	// KeyOrchestratorWriteSettingsFilePendingDirCreateFailed はhook の逃がし先のディレクトリを作れなかったときに出る。
	KeyOrchestratorWriteSettingsFilePendingDirCreateFailed Key = "orchestrator.write_settings_file.pending_dir_create_failed"
	// KeyOrchestratorWriteSettingsFileMarshalFailed はClaude Code の設定ファイルを JSON へ変換できなかったときに出る。
	KeyOrchestratorWriteSettingsFileMarshalFailed Key = "orchestrator.write_settings_file.marshal_failed"
	// KeyOrchestratorWriteSettingsFileWriteFailed はClaude Code の設定ファイルを書き出せなかったときに出る。
	KeyOrchestratorWriteSettingsFileWriteFailed Key = "orchestrator.write_settings_file.write_failed"
	// KeyOrchestratorReadTranscriptReadFailed はtranscript の走査または turn の本文の取り出しに失敗したときに出る。
	KeyOrchestratorReadTranscriptReadFailed Key = "orchestrator.read_transcript.read_failed"
	// KeyOrchestratorOpenRegularFileOpenFailed はtranscript を開けなかったときに出る。
	KeyOrchestratorOpenRegularFileOpenFailed Key = "orchestrator.open_regular_file.open_failed"
	// KeyOrchestratorOpenRegularFileStatFailed は開いた transcript の種別を読めなかったときに出る。
	KeyOrchestratorOpenRegularFileStatFailed Key = "orchestrator.open_regular_file.stat_failed"
	// KeyOrchestratorOpenRegularFileNotRegularFile はtranscript が通常のファイルでなかったときに出る（FIFO などを弾く）。
	KeyOrchestratorOpenRegularFileNotRegularFile Key = "orchestrator.open_regular_file.not_regular_file"
	// KeyOrchestratorStartRunStatusUpdateFailed は着手のときにボードの Status を running_state へ書けなかったときに出る。
	KeyOrchestratorStartRunStatusUpdateFailed Key = "orchestrator.start_run.status_update_failed"
	// KeyOrchestratorStartRunWorktreePrepareFailed は作業用の worktree を用意できなかったときに出る。
	KeyOrchestratorStartRunWorktreePrepareFailed Key = "orchestrator.start_run.worktree_prepare_failed"
	// KeyOrchestratorStartRunAfterCreateHookFailed はworkspace_hooks.after_create が失敗したときに出る。
	KeyOrchestratorStartRunAfterCreateHookFailed Key = "orchestrator.start_run.after_create_hook_failed"
	// KeyOrchestratorStartRunIdentityWriteFailed はworktree の中の身元ファイルを書けなかったときに出る。
	KeyOrchestratorStartRunIdentityWriteFailed Key = "orchestrator.start_run.identity_write_failed"
	// KeyOrchestratorStartRunBeforeRunHookFailed はworkspace_hooks.before_run が失敗したときに出る。
	KeyOrchestratorStartRunBeforeRunHookFailed Key = "orchestrator.start_run.before_run_hook_failed"
	// KeyOrchestratorStartRunPaneRenameFailed はpane の label に owner/repo/issues/N を書けなかったときに出る。
	KeyOrchestratorStartRunPaneRenameFailed Key = "orchestrator.start_run.pane_rename_failed"
	// KeyOrchestratorStartRunAgentStartFailed はpane の中で Claude Code を起動できなかったときに出る。
	KeyOrchestratorStartRunAgentStartFailed Key = "orchestrator.start_run.agent_start_failed"
	// KeyOrchestratorResolvePaneWorkspaceIDEmpty はherdr の workspace の ID が空で pane を引けないときに出る。
	KeyOrchestratorResolvePaneWorkspaceIDEmpty Key = "orchestrator.resolve_pane.workspace_id_empty"
	// KeyOrchestratorResolvePanePaneListFailed はherdr の pane.list に失敗したときに出る。
	KeyOrchestratorResolvePanePaneListFailed Key = "orchestrator.resolve_pane.pane_list_failed"
	// KeyOrchestratorResolvePanePaneCountUnexpected はworkspace の中の pane が1つでなかったときに出る。
	KeyOrchestratorResolvePanePaneCountUnexpected Key = "orchestrator.resolve_pane.pane_count_unexpected"
	// KeyOrchestratorConfirmStartupAgentGetFailed は起動直後の agent の状態を herdr に問い合わせられなかったときに出る。
	KeyOrchestratorConfirmStartupAgentGetFailed Key = "orchestrator.confirm_startup.agent_get_failed"
	// KeyOrchestratorConfirmStartupBlocked は起動直後に確認の画面（blocked）で止まったときに出る。
	KeyOrchestratorConfirmStartupBlocked Key = "orchestrator.confirm_startup.blocked"
	// KeyOrchestratorConfirmStartupWorkingTimeout はstartup_timeout_ms を過ぎても working のままだったときに出る。
	KeyOrchestratorConfirmStartupWorkingTimeout Key = "orchestrator.confirm_startup.working_timeout"
	// KeyOrchestratorConfirmStartupUnknownStatus はherdr が idle / done / blocked / working 以外の状態を返したときに出る。
	KeyOrchestratorConfirmStartupUnknownStatus Key = "orchestrator.confirm_startup.unknown_status"

	// KeyOrchestratorConfirmStartupNotInteractive は、agent は居るが入力を受け付けられない
	// ことを表す（`interactive_ready` が偽）。
	KeyOrchestratorConfirmStartupNotInteractive Key = "orchestrator.confirm_startup.not_interactive"
	// KeyOrchestratorRestoreHookListenFailed は復元の途中で hook を受ける socket の listen を始められなかったときに出る。
	KeyOrchestratorRestoreHookListenFailed Key = "orchestrator.restore.hook_listen_failed"
)

// daemon のエラー（起動の段・起動時の検査・依存の組み立て）。
const (
	// KeyDaemonRunConfigLoadFailed は起動の段1で WORKFLOW.md を読めなかったときに出る。
	KeyDaemonRunConfigLoadFailed Key = "daemon.run.config_load_failed"
	// KeyDaemonRunSocketPathUnresolved はhook を受ける socket の場所を決められなかったときに出る。
	KeyDaemonRunSocketPathUnresolved Key = "daemon.run.socket_path_unresolved"
	// KeyDaemonRunSocketDirFailed はhook を受ける socket を置くディレクトリを準備できなかったときに出る。
	KeyDaemonRunSocketDirFailed Key = "daemon.run.socket_dir_failed"
	// KeyDaemonRunAlreadyRunning はflock が取れず二重起動と判定したときに出る。
	KeyDaemonRunAlreadyRunning Key = "daemon.run.already_running"
	// KeyDaemonRunLockFileFailed は二重起動ではなく、ロックファイルそのものを用意できなかったときに出る。
	KeyDaemonRunLockFileFailed Key = "daemon.run.lock_file_failed"
	// KeyDaemonRunStartupChecksFailed は起動時の検査に落ちたときに出る（生きている pane は閉じない）。
	KeyDaemonRunStartupChecksFailed Key = "daemon.run.startup_checks_failed"
	// KeyDaemonRunRestoreFailed は起動の段4の復元に失敗したときに出る。
	KeyDaemonRunRestoreFailed Key = "daemon.run.restore_failed"
	// KeyDaemonRunStartupChecksHerdrUnreachable は起動時の検査で herdr の socket に到達できないか protocol が想定外だったときに出る。
	KeyDaemonRunStartupChecksHerdrUnreachable Key = "daemon.run_startup_checks.herdr_unreachable"
	// KeyDaemonRunStartupChecksStatusOptionMismatch は起動時の検査でボードの Status の選択肢名が設定と一致しなかったときに出る。
	KeyDaemonRunStartupChecksStatusOptionMismatch Key = "daemon.run_startup_checks.status_option_mismatch"
	// KeyDaemonRunStartupChecksNotWritable は起動時の検査で書けなければならない場所に書けなかったときに出る。
	KeyDaemonRunStartupChecksNotWritable Key = "daemon.run_startup_checks.not_writable"
	// KeyDaemonValidateGraphQLEndpointURLUnparsable はGraphQL の接続先を差し替える環境変数の値が URL として読めなかったときに出る。
	KeyDaemonValidateGraphQLEndpointURLUnparsable Key = "daemon.validate_graphql_endpoint.url_unparsable"
	// KeyDaemonValidateGraphQLEndpointHostMissing は同じ環境変数の値にホスト名が無かったときに出る。
	KeyDaemonValidateGraphQLEndpointHostMissing Key = "daemon.validate_graphql_endpoint.host_missing"
	// KeyDaemonValidateGraphQLEndpointPlainHTTP は同じ環境変数にループバック以外の http を書いたときに出る。
	KeyDaemonValidateGraphQLEndpointPlainHTTP Key = "daemon.validate_graphql_endpoint.plain_http"
	// KeyDaemonValidateGraphQLEndpointSchemeNotHTTPS は同じ環境変数の scheme が https でもループバック宛の http でもなかったときに出る。
	KeyDaemonValidateGraphQLEndpointSchemeNotHTTPS Key = "daemon.validate_graphql_endpoint.scheme_not_https"
	// KeyDaemonBuildHerdrSocketUnresolved は依存の組み立てで herdr の socket の場所を決められなかったときに出る。
	KeyDaemonBuildHerdrSocketUnresolved Key = "daemon.build.herdr_socket_unresolved"
	// KeyDaemonBuildWorkspaceFailed は依存の組み立てで worktree の管理を作れなかったときに出る。
	KeyDaemonBuildWorkspaceFailed Key = "daemon.build.workspace_failed"
	// KeyDaemonBuildTokenFailed は依存の組み立てでボードを読むためのトークンを取れなかったときに出る。
	KeyDaemonBuildTokenFailed Key = "daemon.build.token_failed"
	// KeyDaemonBuildTrackerFailed は依存の組み立てでトラッカーのアダプタを作れなかったときに出る。
	KeyDaemonBuildTrackerFailed Key = "daemon.build.tracker_failed"
	// KeyDaemonBuildRateLimitFailed は依存の組み立てで枠の読み取りを作れなかったときに出る。
	KeyDaemonBuildRateLimitFailed Key = "daemon.build.ratelimit_failed"
	// KeyDaemonBuildOrchestratorFailed は依存の組み立てで orchestrator を作れなかったときに出る。
	KeyDaemonBuildOrchestratorFailed Key = "daemon.build.orchestrator_failed"
	// KeyDaemonBuildHookServerFailed は依存の組み立てで hook の受け口を作れなかったときに出る。
	KeyDaemonBuildHookServerFailed Key = "daemon.build.hookserver_failed"
	// KeyDaemonBuildDashboardFailed は依存の組み立てでダッシュボードを作れなかったときに出る。
	KeyDaemonBuildDashboardFailed Key = "daemon.build.dashboard_failed"
)

// allKeys は宣言済みのキーを全部並べたものである。
//
// **新しいキーを足したらここにも足すこと。**test/internal/i18n がこの一覧と
// messages/ja.json を突き合わせるので、足し忘れるとテストが落ちる。
var allKeys = []Key{
	KeyDoctorLabelConfig,
	KeyDoctorLabelHerdr,
	KeyDoctorLabelClaude,
	KeyDoctorLabelRuntimeDir,
	KeyDoctorRuntimeDirOK,
	KeyDoctorRuntimeDirFailed,
	KeyDoctorRuntimeDirRemedy,
	KeyDoctorClaudeNotFound,
	KeyDoctorClaudeFound,
	KeyDoctorClaudeRemedyInstall,
	KeyDoctorLabelGHAuth,
	KeyDoctorLabelBoard,
	KeyDoctorLabelClone,
	KeyDoctorLabelTrust,
	KeyDoctorLabelCredentials,
	KeyDoctorSummaryAllOK,
	KeyDoctorSummaryUnknownOnly,
	KeyDoctorSummaryProblems,
	KeyDoctorConfigUnreadable,
	KeyDoctorConfigRemedyInit,
	KeyDoctorConfigRemedyPermission,
	KeyDoctorConfigRemedyFrontMatter,
	KeyDoctorConfigOK,
	KeyDoctorLabelClaudeHome,
	KeyDoctorLabelWorkspaceRoot,
	KeyDoctorFilesystemFault,
	KeyDoctorFilesystemRemedyMount,
	KeyDoctorFilesystemRemedyDmesg,
	KeyDoctorFilesystemRemedyDisk,
	KeyDoctorFilesystemRemedyRestart,
	KeyDoctorWriteRemedyPermission,
	KeyDoctorDefaultUsed,
	KeyDoctorClaudeHomeOK,
	KeyDoctorClaudeHomeFailed,
	KeyDoctorClaudeHomeReason,
	KeyDoctorWorkspaceRootOK,
	KeyDoctorWorkspaceRootFailed,
	KeyDoctorWorkspaceRootConfigUnreadable,
	KeyDoctorWorkspaceRootReason,
	KeyFsprobeDirEmpty,
	KeyFsprobeDirNotAbsolute,
	KeyFsprobeMkdirFailed,
	KeyFsprobeWriteFailed,
	KeyFsprobeCleanupFailed,
	KeyFsprobeHomeDirFailed,
	KeyFsprobeClaudeHomeFailed,
	KeyFsprobeWorkspaceRootFailed,
	KeyDoctorHerdrConfigUnreadable,
	KeyDoctorHerdrSocketUnresolved,
	KeyDoctorHerdrRemedySocketAbs,
	KeyDoctorHerdrTimeout,
	KeyDoctorHerdrRemedyTimeout,
	KeyDoctorHerdrRemedyNotListening,
	KeyDoctorHerdrRemedyProtocol,
	KeyDoctorHerdrOK,
	KeyDoctorGHAuthConfigUnreadable,
	KeyDoctorGHAuthRemedyFixConfig,
	KeyDoctorGHAuthRemedyInstall,
	KeyDoctorGHAuthTimeout,
	KeyDoctorGHAuthRemedyTimeout,
	KeyDoctorGHAuthRemedyLogin,
	KeyDoctorGHAuthOK,
	KeyDoctorBoardConfigUnreadable,
	KeyDoctorBoardGHMissing,
	KeyDoctorBoardGHUnknown,
	KeyDoctorBoardTokenUnresolved,
	KeyDoctorBoardRemedyTokenSource,
	KeyDoctorBoardAdapterFailed,
	KeyDoctorBoardRemedyTracker,
	KeyDoctorBoardWhatBootstrap,
	KeyDoctorBoardWhatFetchIssues,
	KeyDoctorBoardOK,
	KeyDoctorBoardEndpointNote,
	KeyDoctorBoardTimeout,
	KeyDoctorBoardRemedyRetryConnection,
	KeyDoctorBoardRateLimited,
	KeyDoctorBoardRemedyWait,
	KeyDoctorBoardRemedyProvider,
	KeyDoctorBoardRemedyStatusOptions,
	KeyDoctorBoardRemedyTokenInvalid,
	KeyDoctorBoardFailed,
	KeyDoctorCloneBinNotFound,
	KeyDoctorCloneRemedyInstallBin,
	KeyDoctorCloneBoardUnreadable,
	KeyDoctorCloneNoTargets,
	KeyDoctorCloneNoteGhqFailed,
	KeyDoctorCloneNoteMissing,
	KeyDoctorCloneRemedyGhqGet,
	KeyDoctorCloneNoteFound,
	KeyDoctorCloneDetailOK,
	KeyDoctorCloneDetailMissing,
	KeyDoctorCloneDetailUnknown,
	KeyDoctorCountUnknownSuffix,
	KeyDoctorTrustBoardUnreadable,
	KeyDoctorTrustNoTargets,
	KeyDoctorTrustHomeUnresolved,
	KeyDoctorTrustNoteNoClone,
	KeyDoctorTrustNoteUndecidable,
	KeyDoctorTrustNoteReason,
	KeyDoctorTrustRemedyRunTrust,
	KeyDoctorTrustDetailOK,
	KeyDoctorTrustDetailMissing,
	KeyDoctorTrustDetailUnknown,
	KeyDoctorCredentialsConfigUnreadable,
	KeyDoctorCredentialsRemedyFixConfig,
	KeyDoctorCredentialsNone,
	KeyDoctorCredentialsTokenEnvEmpty,
	KeyDoctorCredentialsRemedyTokenEnv,
	KeyDoctorCredentialsEnvOK,
	KeyDoctorCredentialsEnvMissing,
	KeyDoctorCredentialsRemedySetEnv,
	KeyDoctorCredentialsHomeUnresolved,
	KeyDoctorCredentialsFileFound,
	KeyDoctorCredentialsFileMissing,
	KeyDoctorCredentialsRemedySkipped,
	KeyDoctorCredentialsKeychainOK,
	KeyDoctorCredentialsKeychainFailed,
	KeyDoctorCredentialsRemedyKeychain,
	KeyDoctorCredentialsKeychainTimeout,
	KeyDoctorCredentialsKeychainNoAccessToken,
	KeyDoctorCredentialsRemedyKeychainTimeout,
	KeyDoctorCredentialsRemedyUseKeychain,
	KeyCLIErrGetwd,
	KeyCLIErrResolveConfigPath,
	KeyCLIErrLoadConfig,
	KeyCLIErrGeneric,
	KeyCLIInitFlagForce,
	KeyCLIInitFlagOwner,
	KeyCLIInitFlagProject,
	KeyCLIInitErrOwnerInvalid,
	KeyCLIInitErrProjectPositive,
	KeyCLIInitErrTooManyPositional,
	KeyCLIInitOverwritten,
	KeyCLIInitCreated,
	KeyCLIInitErrAlreadyExists,
	KeyCLIInitErrDirNotFound,
	KeyCLIInitErrNotADirectory,
	KeyCLIInitErrSymlink,
	KeyCLIInitErrWriteFailed,
	KeyCLIInitDetectFilled,
	KeyCLIInitDetectUnfilled,
	KeyCLIInitDetectCandidate,
	KeyCLIInitDetectAdvice,
	KeyCLIInitDetectPlaceholderNote,
	KeySetupRoleDispatchDesc,
	KeySetupRoleRunningDesc,
	KeySetupRoleReviewDesc,
	KeySetupRoleBlockedDesc,
	KeySetupRoleDoneDesc,
	KeySetupPromptOptionsHeader,
	KeySetupPromptOptionLine,
	KeySetupPromptIntroCount,
	KeySetupPromptIntroZero,
	KeySetupPromptIntroInterrupt,
	KeySetupPromptAsk,
	KeySetupPromptInput,
	KeySetupPromptAssigned,
	KeySetupErrNotANumber,
	KeySetupErrOutOfRange,
	KeySetupErrDuplicate,
	KeySetupSummaryHeader,
	KeySetupSummaryLine,
	KeySetupAbortTooFew,
	KeySetupAbortNoOption,
	KeySetupAbortRemedyUI,
	KeySetupAbortRemedyNoAPI,
	KeySetupAbortInterrupted,
	KeySetupAbortInputClosed,
	KeyCLISetupFlagOwner,
	KeyCLISetupFlagProject,
	KeyCLISetupFlagStatusField,
	KeyCLISetupErrOwnerInvalid,
	KeyCLISetupErrProjectPositive,
	KeyCLISetupErrStatusFieldEmpty,
	KeyCLISetupErrTooManyPositional,
	KeyCLISetupErrNotFound,
	KeyCLISetupErrNotFoundRemedy,
	KeyCLISetupErrKeysNotFound,
	KeyCLISetupErrDirNotFound,
	KeyCLISetupErrNotADirectory,
	KeyCLISetupErrSymlink,
	KeyCLISetupErrWriteFailed,
	KeyCLISetupUpdated,
	KeyCLISetupUpdatedKeysNote,
	KeyCLISetupUpdatedKey,
	KeyCLISetupBoardErrOwner,
	KeyCLISetupBoardRemedyOwner,
	KeyCLISetupBoardErrProject,
	KeyCLISetupBoardRemedyProject,
	KeyCLISetupBoardCandidate,
	KeyCLISetupBoardErr,
	KeyCLISetupBoardRemedyScope,
	KeyCLISetupBoardRemedyStatusField,
	KeyCLISetupBoardRemedyRateLimited,
	KeyCLISetupBoardRemedyGeneric,
	KeyCLITrustFlagDryRun,
	KeyCLITrustErrTooManyPositional,
	KeyCLITrustFetchingClone,
	KeyCLITrustFetchedClone,
	KeyCLITrustErrHomeDir,
	KeyCLITrustErrPlan,
	KeyCLITrustErrWriteRequirements,
	KeyCLITrustDryRunNote,
	KeyCLITrustWarnConcurrent,
	KeyCLITrustWarnCloseClaude,
	KeyCLITrustErrApply,
	KeyCLITrustErrWriteResult,
	KeyCLIDoctorErrTooManyPositional,
	KeyCLIDoctorWarnPathUnresolved,
	KeyCLIDoctorErrWriteReport,
	KeyCLIAllowKeychainAccessErrTooManyPositional,
	KeyCLIAllowKeychainAccessNotDarwin,
	KeyCLIAllowKeychainAccessBefore,
	KeyCLIAllowKeychainAccessBeforeDialog,
	KeyCLIAllowKeychainAccessOK,
	KeyCLIAllowKeychainAccessFields,
	KeyCLIAllowKeychainAccessNoAccessToken,
	KeyCLIAllowKeychainAccessErrHeadline,
	KeyCLIAllowKeychainAccessErrHowTo,
	KeyCLIAllowKeychainAccessErrCauses,
	KeyCLIAllowKeychainAccessErrRemedy,
	KeyCLIAllowKeychainAccessTimeoutHeadline,
	KeyCLIAllowKeychainAccessTimeoutHowTo,
	KeyCLIAllowKeychainAccessTimeoutCauses,
	KeyCLIAllowKeychainAccessTimeoutRemedy,
	KeyCLIAbandonFlagDryRun,
	KeyCLIAbandonFlagForce,
	KeyCLIAbandonFlagTo,
	KeyCLIAbandonFlagPark,
	KeyCLIAbandonUsage,
	KeyCLIAbandonErrIssueURLRequired,
	KeyCLIAbandonErrTooManyPositional,
	KeyAbandonIssueURLEmpty,
	KeyAbandonIssueURLUnparsable,
	KeyAbandonIssueURLBadScheme,
	KeyAbandonIssueURLNoHost,
	KeyAbandonIssueURLBadShape,
	KeyAbandonIssueURLBadNumber,
	KeyAbandonHerdrSocketUnresolved,
	KeyAbandonRuntimeDirFailed,
	KeyAbandonWorkspaceFailed,
	KeyAbandonErrConfigLoad,
	KeyAbandonErrBuild,
	KeyAbandonErrLockFile,
	KeyAbandonRunning,
	KeyAbandonNotRunning,
	KeyAbandonErrScan,
	KeyAbandonNotFound,
	KeyAbandonOwnerRepoMismatch,
	KeyAbandonOwnerRepoUnreadable,
	KeyAbandonSlugMismatch,
	KeyAbandonSlugUnknown,
	KeyAbandonErrUndecided,
	KeyAbandonIdentityUnreadable,
	KeyAbandonIdentityMissing,
	KeyAbandonErrMultiple,
	KeyAbandonErrUnknownState,
	KeyAbandonToSkipped,
	KeyAbandonMultipleItem,
	KeyAbandonErrTracker,
	KeyAbandonBoardNotListed,
	KeyAbandonErrParkStateUnknown,
	KeyAbandonParkNotActive,
	KeyAbandonParkMoved,
	KeyAbandonErrParkActive,
	KeyAbandonErrParkFailed,
	KeyAbandonParkNotWritten,
	KeyAbandonParkLeftBehind,
	KeyAbandonErrPaneList,
	KeyAbandonWaitingPane,
	KeyAbandonPaneGone,
	KeyAbandonErrPaneRemains,
	KeyAbandonErrPaneAlive,
	KeyAbandonPaneCheckSkipped,
	KeyAbandonPaneAliveForced,
	KeyAbandonErrPaneWaitInterrupted,
	KeyAbandonErrInspect,
	KeyAbandonPlanHeader,
	KeyAbandonPlanIssue,
	KeyAbandonPlanStatus,
	KeyAbandonPlanStatusUnknown,
	KeyAbandonPlanWorktree,
	KeyAbandonPlanBranch,
	KeyAbandonPlanHerdrWorkspace,
	KeyAbandonPlanPane,
	KeyAbandonPlanPaneNone,
	KeyAbandonPlanPaneUnknown,
	KeyAbandonPlanDirty,
	KeyAbandonPlanDirtyAtLeast,
	KeyAbandonPlanDirtyUnknown,
	KeyAbandonPlanUnpushed,
	KeyAbandonPlanUnpushedUnknown,
	KeyAbandonPlanUndetermined,
	KeyAbandonPlanBaseUnknown,
	KeyAbandonPlanDiffFromBase,
	KeyAbandonPlanNoDiffFromBase,
	KeyAbandonPlanParkPending,
	KeyAbandonDryRunNote,
	KeyAbandonErrLossWithoutForce,
	KeyAbandonErrUndeterminedWithoutForce,
	KeyAbandonErrCleanup,
	KeyAbandonErrDeferred,
	KeyAbandonDeferredReason,
	KeyAbandonRemoved,
	KeyAbandonRemovedBranchAbsent,
	KeyAbandonRemovedWithLeftovers,
	KeyAbandonLeftover,
	KeyAbandonNotice,
	KeyAbandonOrphanBranchUnknown,
	KeyAbandonOrphanBranchNone,
	KeyAbandonOrphanBranchFound,
	KeyAbandonOrphanBranchUnpushed,
	KeyAbandonOrphanBranchUnpushedUnknown,
	KeyAbandonOrphanBranchDisabled,
	KeyAbandonErrOrphanBranchWithoutForce,
	KeyAbandonErrOrphanBranchDeleteFailed,
	KeyAbandonOrphanBranchRemoved,
	KeyAbandonStatusLeftAlone,
	KeyAbandonErrStatusTargetUnknown,
	KeyAbandonStatusMoved,
	KeyAbandonErrStatusFailed,
	KeyAbandonStatusNotWritten,
	KeyCLIMainUsage,
	KeyCLIMainFlagLogLevel,
	KeyCLIMainFlagPort,
	KeyCLIMainErrPortRange,
	KeyCLIMainErrTooManyPositional,
	KeyCLIMainStarting,
	KeyCLIHookFlagSocket,
	KeyCLIHookFlagPendingDir,
	KeyCLIHookErrPositional,
	KeyCLIHookErrSocketRequired,
	KeyCLIHookErrPendingDirRequired,
	KeyCLIHookErrSocketAbs,
	KeyCLIHookErrPendingDirAbs,
	KeyCLIHookTruncated,
	KeyCLIHookSpilled,
	KeyCLIHookDropped,
	KeyDashboardMeta,
	KeyDashboardCaptionRuns,
	KeyDashboardColIssue,
	KeyDashboardColStatus,
	KeyDashboardColTurn,
	KeyDashboardColLastHook,
	KeyDashboardColTokensTotal,
	KeyDashboardBadgeWaitingQuota,
	KeyDashboardBadgeRetry,
	KeyDashboardBadgeResume,
	KeyDashboardNoHookYet,
	KeyDashboardTokensAt,
	KeyDashboardTokensNotCounted,
	KeyDashboardNoRuns,
	KeyDashboardNoteLastHook,
	KeyDashboardCaptionTokens,
	KeyDashboardColAPICalls,
	KeyDashboardColInput,
	KeyDashboardColCacheCreation,
	KeyDashboardColCacheRead,
	KeyDashboardColOutput,
	KeyDashboardTotal,
	KeyDashboardNoteTokens,
	KeyDashboardAgoSeconds,
	KeyDashboardAgoMinutes,
	KeyDashboardAgoHours,
	KeyDashboardNone,
	KeyLockAcquireOpenFailed,
	KeyLockAcquireAlreadyRunning,
	KeyLockReleaseUnlockFailed,
	KeyLockReleaseCloseFailed,
	KeySocketpathRuntimeDirHomeDirFailed,
	KeySocketpathCheckAbsNotAbsolute,
	KeySocketpathCheckPathLenTooLong,
	KeySocketpathEnsureDirParentMkdirFailed,
	KeySocketpathEnsureDirChmodFailed,
	KeySocketpathEnsureDirMkdirFailed,
	KeySocketpathCheckExistingDirLstatFailed,
	KeySocketpathCheckExistingDirSymlink,
	KeySocketpathCheckExistingDirNotADirectory,
	KeySocketpathCheckExistingDirPermTooOpen,
	KeySocketpathSourceEnvRuntimeDir,
	KeySocketpathSourceEnvXDGRuntimeDir,
	KeySocketpathSourceEnvTMPDir,
	KeySocketpathSourceConfigListen,
	KeyConfigResolvePathWorkDirNotAbsolute,
	KeyConfigLoadPathNotAbsolute,
	KeyConfigLoadReadFailed,
	KeyConfigLoadFrontMatterSplitFailed,
	KeyConfigLoadFrontMatterInvalid,
	KeyConfigLoadExpandFailed,
	KeyConfigFrontMatterNoStartDelimiter,
	KeyConfigFrontMatterNoEndDelimiter,
	KeyConfigPlaceholderRemaining,
	KeyConfigValidateInvalidValue,
	KeyConfigValidateRequired,
	KeyConfigValidateBranchTemplateNeedsIssueNumber,
	KeyConfigExpandTrailingDollar,
	KeyConfigExpandUnclosedBrace,
	KeyConfigExpandEmptyEnvName,
	KeyConfigExpandInvalidDollarForm,
	KeyConfigExpandEnvUndefined,
	KeyConfigExpandEnvEmpty,
	KeyConfigExpandHomeDirFailed,
	KeyConfigExpandTildeUserUnsupported,
	KeyHerdrPingCallFailed,
	KeyHerdrPingUnmarshalFailed,
	KeyHerdrCheckProtocolPingFailed,
	KeyHerdrCheckProtocolVersionMismatch,
	KeyHerdrCallUnmarshalFailed,
	KeyHerdrCallRequestIDFailed,
	KeyHerdrCallMarshalParamsFailed,
	KeyHerdrCallMarshalRequestFailed,
	KeyHerdrSocketPathNotAbsolute,
	KeyHerdrSocketPathHomeDirFailed,
	KeyHerdrSocketPathSourceConfig,
	KeyHerdrAgentInvalidName,
	KeyHerdrAgentSendKeysEmpty,
	KeyTrackerGHAuthTokenRunFailed,
	KeyTrackerGHAuthTokenEmptyOutput,
	KeyTrackerGHAuthStatusStartFailed,
	KeyTrackerGHAvailableNotInPath,
	KeyTrackerGHScopeNoActiveAccount,
	KeyTrackerGHScopeMissingScope,
	KeyHookserverStartListenFailed,
	KeyHookserverRemoveStaleSocketLstatFailed,
	KeyHookserverRemoveStaleSocketAlreadyListening,
	KeyHookserverRemoveStaleSocketRemoveFailed,
	KeyHookserverCloseListenerCloseFailed,
	KeyHookserverDecodeEventNotObject,
	KeyHookserverPendingDirsIssuesDirUnreadable,
	KeyHookserverPendingNotRegularFile,
	KeyHookserverReadPendingFileTooLarge,
	KeyHookclientForwardNoPendingDir,
	KeyHookclientForwardSpillFailed,
	KeyHookclientReadInputReadFailed,
	KeyHookclientTruncatedLineHeadUnreadable,
	KeyHookclientTruncatedLineNotObject,
	KeyHookclientTruncatedLineNoFields,
	KeyHookclientTruncatedLineMarshalFailed,
	KeyHookclientCompactLineUnmarshalFailed,
	KeyHookclientCompactLineNotObject,
	KeyHookclientCompactLineCompactFailed,
	KeyHookclientSendToSocketPathEmpty,
	KeyHookclientSendToSocketDialFailed,
	KeyHookclientSendToSocketDeadlineFailed,
	KeyHookclientSendToSocketWriteFailed,
	KeyHookclientSpillDirNotAbsolute,
	KeyHookclientSpillMkdirFailed,
	KeyHookclientSpillCreateFailed,
	KeyHookclientSpillWriteFailed,
	KeyHookclientSpillRenameFailed,
	KeyHookclientSpillNameConflict,
	KeyHookclientCheckPendingCapacityLimitReached,
	KeyRatelimitNewReaderHomeDirFailed,
	KeyRatelimitCredentialsFileNotExist,
	KeyRatelimitCredentialsRemedyKeychain,
	KeyRatelimitCredentialsRemedyEnv,
	KeyRatelimitFetchRequestBuildFailed,
	KeyRatelimitFetchRequestFailed,
	KeyRatelimitFetchBodyReadFailed,
	KeyRatelimitFetchUnexpectedStatus,
	KeyRatelimitFetchParseFailed,
	KeyRatelimitTokenEnvNameEmpty,
	KeyRatelimitTokenEnvValueEmpty,
	KeyRatelimitCredentialsFileHomeDirUnknown,
	KeyRatelimitCredentialsFileReadFailed,
	KeyRatelimitCredentialsFileNotRegularFile,
	KeyRatelimitCredentialsFileParseFailed,
	KeyRatelimitCredentialsFileAccessTokenMissing,
	KeyRatelimitKeychainBinaryNotFound,
	KeyRatelimitKeychainTimeout,
	KeyRatelimitKeychainRunFailed,
	KeyRatelimitKeychainParseFailed,
	KeyRatelimitKeychainOauthMissing,
	KeyRatelimitKeychainAccessTokenMissing,
	KeyServerNewPortOutOfRange,
	KeyServerStartListenFailed,
	KeyServerCloseShutdownFailed,
	KeyServerWriteJSONEncodeFailed,
	KeyScaffoldFileReadFailed,
	KeyScaffoldFileStatFailed,
	KeyScaffoldFileWriteFailed,
	KeyScaffoldFileCloseFailed,
	KeyScaffoldFileCreateFailed,
	KeyScaffoldDirStatFailed,
	KeyScaffoldDirEvalSymlinksFailed,
	KeyScaffoldDirGetwdFailed,
	KeyScaffoldDirAbsFailed,
	KeyScaffoldWriteSymlinkNotFollowed,
	KeyScaffoldUpdateSymlinkNotFollowed,
	KeyScaffoldUpdateNotRegularFile,
	KeyScaffoldUpdateTempCreateFailed,
	KeyScaffoldUpdateChmodFailed,
	KeyScaffoldUpdateSyncFailed,
	KeyScaffoldUpdateRenameFailed,
	KeyScaffoldGHRunFailed,
	KeyScaffoldGHRunFailedWithStderr,
	KeySetupBoardOwnerMissing,
	KeySetupBoardProjectNumberMissing,
	KeySetupBoardFieldListUnparsable,
	KeySetupBoardFieldNotSingleSelect,
	KeySetupBoardFieldNotFound,
	KeyTrustParseOrderedObjectInvalidJSON,
	KeyTrustParseOrderedObjectNotObject,
	KeyTrustParseOrderedObjectKeyUnreadable,
	KeyTrustParseOrderedObjectKeyNotString,
	KeyTrustParseOrderedObjectValueUnreadable,
	KeyTrustParseOrderedObjectObjectNotClosed,
	KeyTrustMarshalIndentKeyEncodeFailed,
	KeyTrustMarshalIndentValueIndentFailed,
	KeyTrustRunGitToplevelRunFailed,
	KeyTrustRunGitToplevelRunFailedWithStderr,
	KeyTrustRunGitToplevelEmptyOutput,
	KeyTrustOptionsHomeDirNotAbsolute,
	KeyTrustApplyProjectsMarshalFailed,
	KeyTrustApplyRootMarshalFailed,
	KeyTrustApplyReplaceFailed,
	KeyTrustProjectsObjectUnparsable,
	KeyTrustMarkTrustedEntryMarshalFailed,
	KeyTrustMarkTrustedEntryUnparsable,
	KeyTrustMarkTrustedFlagNotBool,
	KeyTrustMarkTrustedEntryRemarshalFailed,
	KeyTrustReadClaudeConfigNotFound,
	KeyTrustReadClaudeConfigStatFailed,
	KeyTrustReadClaudeConfigSymlink,
	KeyTrustReadClaudeConfigNotRegularFile,
	KeyTrustReadClaudeConfigReadFailed,
	KeyTrustWriteBackupCreateFailed,
	KeyTrustWriteBackupWriteFailed,
	KeyTrustWriteBackupSyncFailed,
	KeyTrustWriteBackupCloseFailed,
	KeyTrustReplaceFileTempCreateFailed,
	KeyTrustReplaceFileChmodFailed,
	KeyTrustReplaceFileWriteFailed,
	KeyTrustReplaceFileSyncFailed,
	KeyTrustReplaceFileCloseFailed,
	KeyTrustReplaceFileRenameFailed,
	KeyWorkspaceRunHookOutputFileCreateFailed,
	KeyWorkspaceRunHookRunFailed,
	KeyWorkspaceResolveWorkspaceIDPathMismatch,
	KeyWorkspaceRemoveWorktreeWorkspaceIDUnknown,
	KeyWorkspaceRemoveWorktreeWorktreeRemoveFailed,
	KeyWorkspaceRemoveWorktreeByHandFailed,
	KeyWorkspaceRemoveWorktreeStillThere,
	KeyWorkspaceRemoveWorktreeRepoUnknown,
	KeyWorkspaceIssueBranchCloneNotFound,
	KeyWorkspaceIssueBranchUsedByWorktree,
	KeyWorkspaceLeftoverPruneSkipped,
	KeyWorkspaceLeftoverBranchDisabled,
	KeyWorkspaceLeftoverBranchUndeletable,
	KeyWorkspaceLeftoverBranchDeleteFailed,
	KeyWorkspaceLeftoverPruneFailed,
	KeyWorkspaceLeftoverPruneRepoUnknown,
	KeyWorkspaceLeftoverWorkspaceListFailed,
	KeyWorkspaceLeftoverWorkspaceCloseFailed,
	KeyWorkspaceLeftoverBranchReasonNoIdentity,
	KeyWorkspaceLeftoverBranchReasonRepoUnknown,
	KeyWorkspaceLeftoverBranchReasonNormalized,
	KeyWorkspaceLeftoverBranchReasonNoPrefix,
	KeyWorkspaceLeftoverBranchReasonPrefixMismatch,
	KeyWorkspaceLeftoverBranchReasonHeadUnreadable,
	KeyWorkspaceLeftoverBranchReasonHeadMismatch,
	KeyWorkspaceGitWorktreeBranchAtNotRegistered,
	KeyWorkspaceVerifiedRepoCommonDirUnreadable,
	KeyWorkspaceUndeterminedDirty,
	KeyWorkspaceUndeterminedUnpushed,
	KeyWorkspaceRunGitOutputTooLarge,
	KeyWorkspaceRunGitRunFailed,
	KeyWorkspaceGitExitCodeStartFailed,
	KeyWorkspaceGitWorktreeListOutputUnreadable,
	KeyWorkspaceGitWorktreeAddOrphanCheckFailed,
	KeyWorkspaceGitWorktreeAddOrphanDeleteFailed,
	KeyWorkspaceGitWorktreeAddOrphanDeleted,
	KeyWorkspaceBrokenRefStatFailed,
	KeyWorkspaceBrokenRefRemoveFailed,
	KeyWorkspaceBrokenRefResolveFailed,
	KeyWorkspaceBrokenRefRemoved,
	KeyWorkspaceBrokenRefRemovedWithTip,
	KeyWorkspaceBrokenRefBranchCheckFailed,
	KeyWorkspaceBrokenRefBranchSurvived,
	KeyWorkspaceWorktreeHeadRefsUnreadable,
	KeyWorkspaceGitAheadOfUpstreamCountUnreadable,
	KeyWorkspaceGitNoDiffFromBaseUnexpectedExitCode,
	KeyWorkspaceGitBranchExistsUnexpectedExitCode,
	KeyWorkspaceGitUnpushedCountUnreadable,
	KeyWorkspaceGhqNameInvalid,
	KeyWorkspaceRunGhqListStartFailed,
	KeyWorkspaceRunGhqListExitFailed,
	KeyWorkspaceGitLocalBranchesOutputUnreadable,
	KeyWorkspaceRunGhqGetStartFailed,
	KeyWorkspaceRunGhqGetExitFailed,
	KeyWorkspaceOwnerRepoFromWorktreePathRelFailed,
	KeyWorkspaceOwnerRepoFromWorktreePathLayoutMismatch,
	KeyWorkspaceVerifiedRepoCloneNotFound,
	KeyWorkspaceVerifiedRepoRepoMismatch,
	KeyWorkspaceEnsureRootRootEmpty,
	KeyWorkspaceEnsureRootRootNotAbsolute,
	KeyWorkspaceEnsureRootMkdirFailed,
	KeyWorkspaceEnsureRootSymlinkUnresolvable,
	KeyWorkspaceLabelWorktreePath,
	KeyWorkspaceLabelIssueSettingsFile,
	KeyWorkspaceCheckUnderNotAbsolute,
	KeyWorkspaceCheckUnderSameAsRoot,
	KeyWorkspaceCheckUnderOutsideRoot,
	KeyWorkspaceCheckContainmentResolvedSymlinkUnresolvable,
	KeyWorkspaceCheckContainmentResolvedOutsideRoot,
	KeyWorkspaceRenderBranchTemplateUnparsable,
	KeyWorkspaceRenderBranchTemplateRenderFailed,
	KeyWorkspaceRenderBranchRenderedEmpty,
	KeyWorkspaceScanLevelRootUnreadable,
	KeyWorkspaceCheckTrustForClonePathToplevelFailed,
	KeyWorkspaceCheckTrustForClonePathConfigUnreadable,
	KeyWorkspaceCheckTrustForClonePathConfigUnparsable,
	KeyWorkspaceValidateIdentityFileNameEmpty,
	KeyWorkspaceValidateIdentityFileNameHasSpaces,
	KeyWorkspaceValidateIdentityFileNameHasSeparator,
	KeyWorkspaceValidateIdentityFileNameNotAFileName,
	KeyWorkspaceReadIdentityReadFailed,
	KeyWorkspaceReadIdentityNotRegular,
	KeyWorkspaceReadIdentityTooLarge,
	KeyWorkspaceWriteIdentityMarshalFailed,
	KeyWorkspaceWriteFileAtomicTempCreateFailed,
	KeyWorkspaceWriteFileAtomicWriteFailed,
	KeyWorkspaceWriteFileAtomicChmodFailed,
	KeyWorkspaceWriteFileAtomicCloseFailed,
	KeyWorkspaceWriteFileAtomicRenameFailed,
	KeyWorkspaceRegisterExcludeInfoDirCreateFailed,
	KeyWorkspaceRegisterExcludeReadFailed,
	KeyWorkspaceRegisterExcludeOpenFailed,
	KeyWorkspaceRegisterExcludeWriteFailed,
	KeyWorkspaceRegisterExcludeCloseFailed,
	KeyWorkspaceNewSettingsRootNotAbsolute,
	KeyWorkspaceNewHomeDirUnknown,
	KeyWorkspacePrepareCloneNotFound,
	KeyWorkspacePrepareStatFailed,
	KeyWorkspacePrepareBranchInUseElsewhere,
	KeyWorkspacePrepareBranchMismatch,
	KeyWorkspacePrepareUnregisteredWorktree,
	KeyWorkspacePrepareParentDirCreateFailed,
	KeyWorkspacePrepareWorktreeOpenFailed,
	KeyWorkspacePrepareWorktreePathMismatch,
	KeyWorkspaceResolveBaseDefaultBranchMissing,
	KeyWorkspaceResolveBaseDefaultBranchNotString,
	KeyOrchestratorNewExecutablePathUnknown,
	KeyOrchestratorResolveAgentNameAgentListFailed,
	KeyOrchestratorResolveAgentNameInvalidName,
	KeyOrchestratorResolveAgentNameNoFreeName,
	KeyOrchestratorNewSessionUUIDRandFailed,
	KeyOrchestratorRenderFirstPromptTemplateUnparsable,
	KeyOrchestratorRenderFirstPromptRenderFailed,
	KeyOrchestratorWriteSettingsFileDirCreateFailed,
	KeyOrchestratorWriteSettingsFilePendingDirCreateFailed,
	KeyOrchestratorWriteSettingsFileMarshalFailed,
	KeyOrchestratorWriteSettingsFileWriteFailed,
	KeyOrchestratorReadTranscriptReadFailed,
	KeyOrchestratorOpenRegularFileOpenFailed,
	KeyOrchestratorOpenRegularFileStatFailed,
	KeyOrchestratorOpenRegularFileNotRegularFile,
	KeyOrchestratorStartRunStatusUpdateFailed,
	KeyOrchestratorStartRunWorktreePrepareFailed,
	KeyOrchestratorStartRunAfterCreateHookFailed,
	KeyOrchestratorStartRunIdentityWriteFailed,
	KeyOrchestratorStartRunBeforeRunHookFailed,
	KeyOrchestratorStartRunPaneRenameFailed,
	KeyOrchestratorStartRunAgentStartFailed,
	KeyOrchestratorResolvePaneWorkspaceIDEmpty,
	KeyOrchestratorResolvePanePaneListFailed,
	KeyOrchestratorResolvePanePaneCountUnexpected,
	KeyOrchestratorConfirmStartupAgentGetFailed,
	KeyOrchestratorConfirmStartupBlocked,
	KeyOrchestratorConfirmStartupWorkingTimeout,
	KeyOrchestratorConfirmStartupUnknownStatus,
	KeyOrchestratorConfirmStartupNotInteractive,
	KeyOrchestratorRestoreHookListenFailed,
	KeyDaemonRunConfigLoadFailed,
	KeyDaemonRunSocketPathUnresolved,
	KeyDaemonRunSocketDirFailed,
	KeyDaemonRunAlreadyRunning,
	KeyDaemonRunLockFileFailed,
	KeyDaemonRunStartupChecksFailed,
	KeyDaemonRunRestoreFailed,
	KeyDaemonRunStartupChecksHerdrUnreachable,
	KeyDaemonRunStartupChecksStatusOptionMismatch,
	KeyDaemonRunStartupChecksNotWritable,
	KeyDaemonValidateGraphQLEndpointURLUnparsable,
	KeyDaemonValidateGraphQLEndpointHostMissing,
	KeyDaemonValidateGraphQLEndpointPlainHTTP,
	KeyDaemonValidateGraphQLEndpointSchemeNotHTTPS,
	KeyDaemonBuildHerdrSocketUnresolved,
	KeyDaemonBuildWorkspaceFailed,
	KeyDaemonBuildTokenFailed,
	KeyDaemonBuildTrackerFailed,
	KeyDaemonBuildRateLimitFailed,
	KeyDaemonBuildOrchestratorFailed,
	KeyDaemonBuildHookServerFailed,
	KeyDaemonBuildDashboardFailed,
}

// AllKeys は宣言済みのキーを全部返す。
//
// **テストが messages/ja.json との対応を確かめるために使う。**
//
// 戻り値: 宣言した順に並んだキーの一覧（呼び出し側が書き換えても内部には影響しない）。
func AllKeys() []Key {
	out := make([]Key, len(allKeys))
	copy(out, allKeys)
	return out
}
