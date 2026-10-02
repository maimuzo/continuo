# 判断ログ: Keychainの読み取りを許可する

- 対象: `docs/spec/usecases/particular_case/Keychainの読み取りを許可する.rucm.md`
- 作成日 / 作成モデル: 2026-10-02 / Claude Opus 5.5。実装（`continuo allow-keychain-access`）を読んで起こした
- 参照した根拠資料: `docs/plans/continuo_design.md`（3-15 / 3-32 / 3-34b）、`internal/cli/cli.go`、`internal/ratelimit/keychain.go`、`test/internal/cli/cli_test.go`、`test/internal/ratelimit/cli_test.go`

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | 配置先ディレクトリ | `particular_case/` | 利用者が叩くコマンド1つに、記述1本を当てる分け方に合わせた | `docs/plans/rucm_realign_plan.md` の 1節 | 95% |
| 2 | USE CASE NAME | Keychainの読み取りを許可する | コマンド名 `allow-keychain-access` の意味をそのまま日本語にした。名前に空白を入れない決まりに合わせた | `internal/cli/cli.go` の `runAllowKeychainAccess` のコメント | 90% |
| 3 | PRIMARY ACTOR / SECONDARY ACTORS | 利用者 / macOS の Keychain | コマンドを叩くのも、ダイアログに答えるのも利用者である。システムが相手にする外部は `security` 越しの Keychain だけである | `internal/ratelimit/keychain.go` の `runSecurity` | 95% |
| 4 | PRECONDITION | 実行ファイルを実行できることだけ | 設定ファイルを読まない。`WORKFLOW.md` が無い段階でも叩ける | `internal/cli/cli.go` の `runAllowKeychainAccess` のコメント | 95% |
| 5 | 段2 と `使い方の表示` | `--help` は使い方を標準エラーへ出して終了コード 0。ABORT | `flag` が `ErrHelp` を返し、`parseErrorExitCode` が 0 にする。その場で返るので、このユースケースはここで終わる | `internal/cli/cli.go` の `runAllowKeychainAccess` / `parseErrorExitCode` | 95% |
| 6 | 段3 と `引数指定エラー` | 知らないフラグと位置引数は終了コード 2。ABORT | 知らないフラグは `parseErrorExitCode` が 2、位置引数は `runAllowKeychainAccess` が直に 2 を返す。応答の出力先はどちらも標準エラーで、結末が同じなので1本にした。その場で返るので、ここで終わる | `internal/cli/cli.go` の `runAllowKeychainAccess` | 95% |
| 7 | 段4 と `macOS以外` | OS の名前を標準出力へ出して終了コード 0。ABORT | 前提が違うだけなので失敗にしない。`security` を起動する前に返る。その場で返るので、ここで終わる | `internal/cli/cli.go` の `runAllowKeychainAccess` | 100% |
| 8 | 段4 を段3 より後ろに置いたこと | 引数の検査が先、OS の判定が後 | 実装の順である。macOS 以外でも、引数が誤っていれば終了コード 2 が返る | `internal/cli/cli.go` の `runAllowKeychainAccess` | 100% |
| 9 | 段5 | 読む前に案内を出す | 実装は `ProbeKeychain` を呼ぶ前に2行を出す。ダイアログが出てから選び方を探させないためである | `internal/cli/cli.go` の `runAllowKeychainAccess` | 100% |
| 10 | 段7 と `期限内に返らない` | 60秒で打ち切り、終了コード 1。ABORT | `AllowAccessTimeout` は60秒。`runSecurity` が期限で `security` を止め、`ErrKeychainTimeout` を包んで返す。実装は `errors.Is` でこの誤りを先に見て、専用の案内を出す。その場で返るので、ここで終わる | `internal/ratelimit/keychain.go` の `runSecurity` / `AllowAccessTimeout`、`internal/cli/cli.go` の `runAllowKeychainAccess` | 95% |
| 11 | 段8 と `読めない` | 期限切れ以外の誤りは、理由と直し方を出して終了コード 1。ABORT | `switch` の2本目の枝である。理由の4通り（`security` が無い・異常終了・JSON として読めない・`claudeAiOauth` が無い）は応答の中の理由の1行が違うだけで、流れも終了コードも同じなので、段を分けずに本文の表に書いた。その場で返るので、ここで終わる | `internal/cli/cli.go` の `runAllowKeychainAccess` / `printKeychainFailure`、`internal/ratelimit/keychain.go` の `ProbeKeychain` / `runSecurity` | 90% |
| 12 | 段9 と `accessTokenが無い` | 項目の名前の一覧を出してから直し方を出し、終了コード 1。ABORT | `switch` の3本目の枝である。読めた項目を先に出すので、応答の段を2つにした。その場で返るので、ここで終わる | `internal/cli/cli.go` の `runAllowKeychainAccess` | 95% |
| 13 | 段10・段11 と事後条件 | 読めたことと項目の名前の一覧を出し、終了コード 0。値は出さない | `ProbeKeychain` は値を `json.RawMessage` のまま触らず、キーの名前だけを昇順で返す | `internal/ratelimit/keychain.go` の `ProbeKeychain` | 100% |
| 14 | 打ち切り（`ErrKeychainCanceled`）を段にしなかったこと | 段にも代替フローにもしない | `runAllowKeychainAccess` は `context.Background()` を渡すので、呼び出し側の打ち切りは起きない。実装が通れない経路を記述に出さない | `internal/cli/cli.go` の `runAllowKeychainAccess`、`internal/ratelimit/keychain.go` の `runSecurity` | 90% |
| 15 | 応答の書き出しの失敗を段にしなかったこと | 段にしない | 実装は `fmt.Fprintln` の戻り値を見ていない。書き出せなくても流れも終了コードも変わらない | `internal/cli/cli.go` の `runAllowKeychainAccess` | 95% |
| 16 | シーケンス図のダイアログ | `opt` で描いた | ダイアログを出すのは macOS で、continuo からは見えない。rucm ブロックの段にはせず、図にだけ描いた。出るのは初めて読む実行ファイルのときである | `docs/plans/continuo_design.md` の 3-15 | 80% |
| 17 | テストの対応づけ | 7本の経路の全部に、既に在るテスト9本と新しいテスト1本を当てた | `使い方の表示` だけ、当たるテストが無かった。Keychain を読む関数を差し替える既存の形で書けるので、1本足した。ほかは既に在るテストを記述名のファイルへ移した。2つのディレクトリにまたがるので、同じ名前のファイルを1本ずつ置いた | `test/internal/cli/Keychainの読み取りを許可する_test.go`、`test/internal/ratelimit/Keychainの読み取りを許可する_test.go` | 90% |
