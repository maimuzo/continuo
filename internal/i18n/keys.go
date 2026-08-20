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
	// KeyDoctorConfigRemedyInit は同じときの直し方に出る。
	KeyDoctorConfigRemedyInit Key = "doctor.config.remedy_init"
	// KeyDoctorConfigOK は読めたときの説明に出る。
	KeyDoctorConfigOK Key = "doctor.config.ok"
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
)

// CLI の共通の文言（複数のサブコマンドが同じ文面を出す）。
const (
	// KeyCLIErrFlagAfterPositional は位置引数のあとにフラグが書かれたときに出る。
	KeyCLIErrFlagAfterPositional Key = "cli.err_flag_after_positional"
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
	// KeySetupRoleDispatchName は着手待ちの役割の名前に出る。
	KeySetupRoleDispatchName Key = "setup.role.dispatch_name"
	// KeySetupRoleDispatchDesc は着手待ちの役割の説明に出る。
	KeySetupRoleDispatchDesc Key = "setup.role.dispatch_desc"
	// KeySetupRoleRunningName は作業中の役割の名前に出る。
	KeySetupRoleRunningName Key = "setup.role.running_name"
	// KeySetupRoleRunningDesc は作業中の役割の説明に出る。
	KeySetupRoleRunningDesc Key = "setup.role.running_desc"
	// KeySetupRoleReviewName はレビュー待ちの役割の名前に出る。
	KeySetupRoleReviewName Key = "setup.role.review_name"
	// KeySetupRoleReviewDesc はレビュー待ちの役割の説明に出る。
	KeySetupRoleReviewDesc Key = "setup.role.review_desc"
	// KeySetupRoleBlockedName は保留の役割の名前に出る。
	KeySetupRoleBlockedName Key = "setup.role.blocked_name"
	// KeySetupRoleBlockedDesc は保留の役割の説明に出る。
	KeySetupRoleBlockedDesc Key = "setup.role.blocked_desc"
	// KeySetupRoleDoneName は完了の役割の名前に出る。
	KeySetupRoleDoneName Key = "setup.role.done_name"
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

// `continuo`（常駐プロセス本体）の文言。
const (
	// KeyCLIMainFlagLogLevel は--log-level の説明に出る。
	KeyCLIMainFlagLogLevel Key = "cli.main.flag_log_level"
	// KeyCLIMainFlagPort は--port の説明に出る。
	KeyCLIMainFlagPort Key = "cli.main.flag_port"
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

// allKeys は宣言済みのキーを全部並べたものである。
//
// **新しいキーを足したらここにも足すこと。**test/internal/i18n がこの一覧と
// messages/ja.json を突き合わせるので、足し忘れるとテストが落ちる。
var allKeys = []Key{
	KeyDoctorLabelConfig,
	KeyDoctorLabelHerdr,
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
	KeyDoctorConfigOK,
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
	KeyCLIErrFlagAfterPositional,
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
	KeySetupRoleDispatchName,
	KeySetupRoleDispatchDesc,
	KeySetupRoleRunningName,
	KeySetupRoleRunningDesc,
	KeySetupRoleReviewName,
	KeySetupRoleReviewDesc,
	KeySetupRoleBlockedName,
	KeySetupRoleBlockedDesc,
	KeySetupRoleDoneName,
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
	KeyCLIMainFlagLogLevel,
	KeyCLIMainFlagPort,
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
