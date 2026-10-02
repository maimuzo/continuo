# 判断ログ: 既存のボードの Status を割り当てる

- 対象: `docs/spec/usecases/particular_case/既存のボードの Status を割り当てる.rucm.md`
- 作成日 / 作成モデル: 2026-08-20 / Claude Opus 5 (1M context)。2026-10-02 に Claude Opus 5.5 が、いまの実装（`continuo setup`）から起こし直した
- 参照した根拠資料: `docs/plans/continuo_design.md`（3-32 / 3-34 / 3-83）、`docs/trying_it_out.md`（段4）、`internal/cli/cli.go`、`internal/setup/setup.go`、`internal/setup/assign.go`、`internal/setup/board.go`、`internal/scaffold/update.go`、`internal/scaffold/fill.go`、`internal/scaffold/scaffold.go`、`internal/i18n/messages/ja.json`

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | USE CASE NAME | 既存のボードの Status を割り当てる | 依頼で指定された名前をそのまま使う。実装は `runSetup` のコメントでこの記述の名前を引いている | `internal/cli/cli.go` の `runSetup` | 100% |
| 2 | 配置先ディレクトリ | `particular_case/` | 1つのコマンドの1回の実行である | `internal/cli/cli.go` の `runSetup` | 100% |
| 3 | 「ボード」と「カンバン」 | 記述の中では「ボード」に統一する | 実装と設計は「カンバン」と呼ぶ。記述の名前と、引いている記述（`設定に書く値を gh から引く`）は「ボード」を使う。同じ記述の中で2つの語を混ぜない（R9）。食い違いは本文の冒頭に断ってある | `internal/setup/setup.go` のパッケージコメント | 80% |
| 4 | PRECONDITION | 利用者は continuo の実行ファイルを実行できる | 前の版は「gh のログイン済み」「ボードを1枚持っている」「WORKFLOW.md がある」を事前条件にしていた。実装はどれも実行の中で確かめ、満たさなければ理由を出して止まる。事前条件に置くと、止まる経路が記述から消える | `internal/cli/cli.go` の `runSetup` / `checkDetectionForSetup` | 95% |
| 5 | SECONDARY ACTORS | gh | 実装が外へ出すのは gh の実行だけである。前の版の「GitHub Projects v2」は、実装が直接は叩かない | `internal/setup/board.go` の `FetchStatusField`、`internal/scaffold/detect.go` の `RunGH` | 95% |
| 6 | DEPENDENCY | INCLUDE USE CASE 設定に書く値を gh から引く | 実装は フラグ → WORKFLOW.md の値 → `scaffold.Detect` の順に値を決める。`Detect` の中身は別の記述に在る。1段にまとめた前の版は、決まらないときに止まる経路を持たなかった | `internal/cli/cli.go` の `runSetup` | 95% |
| 7 | 基本フロー 2・3 | 使い方の表示と引数の誤りを、ファイルの検査より前に置く | `fs.Parse` と引数の値の検査は `scaffold.CheckUpdatable` より前に在る。`--help` は 0、それ以外の誤りは 2 を返す。使い方は `fs.SetOutput(stderr)` により標準エラーへ出る（2026-10-02 に実測。標準出力は 0 バイト） | `internal/cli/cli.go` の `runSetup` / `parseErrorExitCode` | 100% |
| 8 | 引数指定エラーを1本にまとめたこと | 知らないフラグ・`--owner` の形・`--project` が0以下・`--status-field` が空・位置引数が2個以上を1本にする | 終わり方（標準エラー、終了コード 2、何も読まない）が同じである。違うのは文言だけである | `internal/cli/cli.go` の `runSetup` | 90% |
| 9 | 基本フロー 4〜8 の順番 | パスが在る → ディレクトリである → symlink でない → 通常のファイルが在る → 読める | `resolveTarget` がパスとディレクトリを見て、`statTarget` が `os.Lstat` の結果を 無い → symlink → 通常のファイルでない の順に見て、`CheckUpdatable` が読み込む。「無い」と「symlink」は同時に起きないので、symlink を先に書いても実装と食い違わない。先例（`設定ファイルを作る`）と同じ順にした | `internal/scaffold/scaffold.go` の `resolveTarget`、`internal/scaffold/update.go` の `statTarget` / `CheckUpdatable` | 90% |
| 10 | `WORKFLOWmdが無い` に通常のファイルでない場合を含めること | 含める | `statTarget` は通常のファイルでないものを `ErrNotFound` で包んで返す。`printScaffoldError` は同じ文言（無い）と同じ案内（`continuo init`）を出す（2026-10-02 に実測。WORKFLOW.md という名前のディレクトリで確かめた） | `internal/scaffold/update.go` の `statTarget`、`internal/cli/cli.go` の `printScaffoldError` | 95% |
| 11 | `WORKFLOWmdを読めない` | 読み込みの失敗と、状態を調べられない失敗を1本にする | どれも `printScaffoldError` の `default` へ落ち、同じ文言の頭（書き換えられない）と理由を標準エラーへ出して 1 を返す | `internal/cli/cli.go` の `printScaffoldError` | 90% |
| 12 | 基本フロー 9・10 を対話より前に置いたこと | 対話の前に検査する | `CheckUpdatable` が、尋ねる前にキーの有無と値の形を見る。前の版は、対話のあとの検査（段16）として書いていた | `internal/scaffold/update.go` の `CheckUpdatable` | 100% |
| 13 | 基本フロー 9 と 10 を分けたこと | 「キーが無い」と「値がキーの行に無い」を別の検査にする | 実装は無いキーを先に見て止め、次に値の形を見る。文言も違う（前者は1行、後者は理由と直し方の2行） | `internal/scaffold/update.go` の `CheckUpdatable`、`internal/cli/cli.go` の `printScaffoldError` | 95% |
| 14 | 「必ず書き換える8つのキー」 | 9つのうち `tracker.direct_chat_state` を除いた8つ | `statusKeys` は9つで、`optional` が真なのは `tracker.direct_chat_state` だけである。無くても止めない。front matter を切り出せない場合は、8つ全部を無いものとして返す（2026-10-02 に実測） | `internal/scaffold/fill.go` の `statusKeys` / `applyStatuses` / `requiredStatusKeyNames` | 100% |
| 15 | 基本フロー 12（言語を決める）を分岐にしないこと | 素の段にする。設定を読めないときの警告は本文に書く | 読めても読めなくても先へ進む。分岐にすると、後ろの経路が全部2倍になる。言語の決め方は `画面に出す文言の言語を決める` の記述が持つ | `internal/cli/cli.go` の `runSetup` | 85% |
| 16 | 基本フロー 11・13 | WORKFLOW.md の値を読み、引数の値を優先する | `CheckUpdatable` が owner と番号を拾って返す。`runSetup` はフラグが空のときだけ WORKFLOW.md の値を使う。プレースホルダは拾わない | `internal/scaffold/update.go` の `readProviderValues`、`internal/cli/cli.go` の `runSetup` | 95% |
| 17 | 基本フロー 15・16 | owner の検査を先、ボードの番号の検査をあとにする | `checkDetectionForSetup` の順である。番号が決まらないときは候補の一覧も出す。どちらも標準エラー、終了コード 1 | `internal/cli/cli.go` の `checkDetectionForSetup` | 100% |
| 18 | `ボードの番号が決まらない` の POSTCONDITION | 「ボードを選ばせる問い合わせを出していない」と書く | 実装は候補を並べるだけで、選ばせない。対話するのは役割の割り当てだけである | `internal/cli/cli.go` の `checkDetectionForSetup` | 95% |
| 19 | 基本フロー 17 | 選択肢を読むボードを標準出力へ出す | WORKFLOW.md の値と gh から引いた値が食い違っても、利用者がその場で気づけるように出している。前の版には無かった | `internal/cli/cli.go` の `runSetup` | 100% |
| 20 | 基本フロー 19 を1つの検証にしたこと | 「選択肢を読み取れる」の1つ。直し方の違いは本文の表に書く | gh の失敗・応答を読めない・フィールドが無い・single-select でない は、どれも理由1行と直し方1行を標準エラーへ出して 1 を返す。直し方は4通りである | `internal/cli/cli.go` の `runSetup`、`internal/setup/board.go` の `FetchStatusField` / `classifyGHError` | 90% |
| 21 | 基本フロー 20 | 5個以上 | 数えるのは `RequiredRoleCount`（5）である。6つ目の役割は飛ばせるので、選択肢がちょうど5個でも対話に入る | `internal/setup/assign.go` の `Assign`、`internal/setup/setup.go` の `RequiredRoleCount` | 100% |
| 22 | `選択肢が足りない` と `該当する選択肢が無い` の出力先 | 標準出力 | `Assign` は理由を `Out`（`runSetup` が渡す標準出力）へ出し終える。`runSetup` は何も足さずに 1 を返す | `internal/setup/assign.go` の `Assign`、`internal/cli/cli.go` の `runSetup` | 100% |
| 23 | 役割の数 | 6つ。6つ目（`direct_chat_state`）は番号 0 で飛ばせる | `RoleCount` は 6 で、`IsOptional` が真なのは `RoleDirectChat` だけである。前の版と図は5つだった | `internal/setup/setup.go` の `RoleCount` / `IsOptional` / `roleOrder` | 100% |
| 24 | DO と UNTIL | 同じ問い方を6回繰り返す。抜ける条件は「6つの役割すべてを尋ね終えている」 | 実装は `roleOrder` を順に回る。飛ばした役割には割り当てが無いので、「すべてに割り当てられている」は条件にならない | `internal/setup/assign.go` の `Assign` | 95% |
| 25 | 基本フロー 26〜30 の順番 | 長さ → 整数として読める → 範囲 → 0 → 重複 | `Assign` の中の順である。長すぎる1行は `reader.read` が返した時点で扱う | `internal/setup/assign.go` の `Assign` | 100% |
| 26 | 尋ね直す4本の戻り先 | `RESUME STEP 24`（同じ役割の説明から） | 実装は `continue` で、同じ役割の問いをもう一度出す。CFG では cycle になる | `internal/setup/assign.go` の `Assign` | 100% |
| 27 | `長すぎる1行` と `番号として読めない入力` を `番号が範囲外` から分けたこと | 3本にする | 文言が違う。長すぎる1行は改行まで読み捨てる。前の版は「番号が一覧の範囲内である」の1つにまとめていた | `internal/setup/assign.go` の `Assign` / `readLimitedLine` / `parseNumber` | 90% |
| 28 | `飛ばせる役割を飛ばす` の戻り先 | `RESUME STEP 34`（割り当ての一覧を応答する段） | 飛ばせる役割は最後に尋ねる6つ目だけである。飛ばしたあとに実装が次に行うのは `writeSummary` である。UNTIL の段へ戻すと、「飛ばしたあとに次の役割を尋ねる」という実装が通れない経路が CFG に出る | `internal/setup/setup.go` の `roleOrder`、`internal/setup/assign.go` の `Assign` | 90% |
| 29 | `該当する選択肢が無い` の分岐元 | `飛ばせる役割を飛ばす` の段1 | 番号が 0 で、役割が飛ばせない場合である。実装は 0 を見てから役割が飛ばせるかを見る | `internal/setup/assign.go` の `Assign` | 95% |
| 30 | `中断` の分岐元と WHEN | `BRANCH FROM BASIC FLOW 25`。番号を待つシステムに Ctrl+C を入力した場合 | 実装が取り消しを見るのは `lineReader` の `read` の中だけである。前の版の分岐元（割り当ての一覧を応答した直後）では、実装は中断しない。書き換えは最後まで進む | `internal/setup/assign.go` の `lineReader` の `read`、`internal/cli/cli.go` の `runSetup` | 95% |
| 31 | `入力の終わり` と `入力の読み取り失敗` | 任意時点の代替フローとして足す。分岐元は `中断` と同じ段25 | 番号を待つあいだに起きる出来事で、どの役割の問いでも起きる。実装は3つで文言を変え、どれも標準出力へ出して 1 を返す | `internal/setup/assign.go` の `Assign` | 90% |
| 32 | 中断の3本の終了コード | 1 | `Assign` がエラーを返すと、`runSetup` は 1 を返す。前の版には終了コードが無かった | `internal/cli/cli.go` の `runSetup` | 100% |
| 33 | 基本フロー 35（書き換える直前の検査） | 1つの検証にまとめる | `UpdateStatuses` は WORKFLOW.md を読み直し、`CheckUpdatable` と同じ検査をもう一度行う。対話のあいだにファイルが変わった場合だけ止まる。検査ごとに分けると、対話の前の7本と同じ代替フローがもう7本要る | `internal/scaffold/update.go` の `UpdateStatuses` | 85% |
| 34 | 基本フロー 37 の条件 | 元の front matter が読めないか、組み立てた全文を読み直せる | 元から読めなかった WORKFLOW.md では、読み直しの検査を通す。setup が壊したものではない | `internal/scaffold/update.go` の `UpdateStatuses` | 95% |
| 35 | `書き換えると読めなくなる` の段 | 理由と「1文字も書いていない」の2つ。直し方の案内は書かない | 実装の文言は1行で、理由と「WORKFLOW.md には1文字も書いていません」だけである。前の版の「手で直すか continuo init で作り直す案内」は実装に無い | `internal/cli/cli.go` の `printScaffoldError`、`internal/i18n/messages/ja.json` の `cli.setup.err_would_break_config` | 100% |
| 36 | `書き込みの失敗` | 足す。標準エラー、終了コード 1 | `atomicfile.Write` の失敗は `printScaffoldError` の `default` へ落ちる。一時ファイルへ書いてから差し替えるので、元の WORKFLOW.md は残る | `internal/scaffold/update.go` の `UpdateStatuses` | 90% |
| 37 | `direct_chat_stateの行が無い` を代替フローにしたこと | 基本フロー 40 の検証から分け、`ABORT` で終える。終了コード 0 | 正常に終わるが、基本フローへ戻る段が無い。書き換えたキーの一覧も違う（8つ。書けなかったキーを一覧へ混ぜない）。基本フローの末尾に IF を置くと、偽の枝の先に段が無く、CFG に経路が出ない | `internal/cli/cli.go` の `runSetup`、`internal/scaffold/fill.go` の `applyStatuses` / `StatusKeyLine` | 90% |
| 38 | 基本フロー 40 の位置 | 差し替えのあと、パスの応答の前 | 行が無いことは全文を組み立てるときに決まるが、利用者に見える違いは応答の段からである。書き換えの失敗の経路と掛け合わせないために、差し替えのあとへ置いた | `internal/scaffold/update.go` の `UpdateStatuses` | 80% |
| 39 | 基本フローの POSTCONDITION | owner と project_number の行が変わっていないことを書く | `Detect` が引き直した値で上書きしない。書き換えるのは `statusKeys` の行だけである | `internal/cli/cli.go` の `runSetup`、`internal/scaffold/fill.go` の `statusKeys` | 95% |
| 40 | 飛ばした役割の書き方 | `tracker.direct_chat_state` の行に空文字を書く | 行に触らないと雛形の既定の値が残る。実装は飛ばしたときに `""` を書く | `internal/scaffold/fill.go` の `statusKeys` | 100% |
| 41 | 書き込みを1つの段にしたこと | 「WORKFLOW.md を組み立てた全文で差し替える」 | 同じディレクトリの一時ファイルへ書いてから差し替える。途中で落ちても半分書かれたファイルは残らない | `internal/scaffold/update.go` の `UpdateStatuses` | 95% |
| 42 | 対話の前の Ctrl+C と、対話のあとの Ctrl+C | 代替フローにしない。本文に書く | 対話の前は Go の既定の動作でプロセスが終わる（応答は無い）。対話のあとは `signal.NotifyContext` が受け取るだけで、誰も見ない。どちらも WORKFLOW.md の状態を変えない | `internal/cli/cli.go` の `runSetup` | 80% |
| 43 | 経路の数 | 32本（前の版は11本） | 対話より前の止まり方（引数・ファイル・値）を12本、番号待ちの出来事を5本、書き込みのあとを2本足した。飛ばす経路は書き換えの結末5通りと掛け合わさる（5本）。飛ばした場合は書く値と貼る1行が変わるので、掛け合わせた経路には違いが在る | `docs/spec/usecases/particular_case/既存のボードの Status を割り当てる.cfg.json` | 85% |
| 44 | 根拠資料の設計 3-34 の見出し | 「3-34. カンバンは既存のものに合わせる」と書いた | 前は「3-34. Status の選択肢が揃っていないカンバンの扱い」と書いていたが、設計文書にその見出しは無い。3-34 の見出しは「カンバンは既存のものに合わせる」で、足りない選択肢を画面から足すことはその節の中に書いてある | `docs/plans/continuo_design.md`（3-34） | 95% |
