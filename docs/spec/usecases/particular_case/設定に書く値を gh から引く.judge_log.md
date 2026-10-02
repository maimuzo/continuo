# 判断ログ: 設定に書く値を gh から引く

- 対象: `docs/spec/usecases/particular_case/設定に書く値を gh から引く.rucm.md`
- 作成日 / 作成モデル: 2026-10-02 / Claude Opus 5.5。`設定ファイルを作る` から、値を決める段を分けて作った
- 参照した根拠資料: `docs/plans/continuo_design.md`（3-32 / 3-33）、`internal/scaffold/detect.go`、`internal/scaffold/fill.go`、`internal/cli/cli.go`

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | 配置先ディレクトリ | `particular_case/` | 値を決める1つの処理である。`continuo init` と `continuo setup` の両方が同じ関数を呼ぶ | `internal/cli/cli.go` の `runInit` / `runSetup` | 95% |
| 2 | `設定ファイルを作る` から分けたこと | 別の記述にし、`INCLUDE USE CASE` で引かせる | 値を決める段は失敗しても止まらない。ファイルを書き出す段と1本に書くと、値の決まり方と書き出しの結末の掛け算で経路が増える。2つの段は互いに影響しないので、掛け合わせた経路に新しい情報は無い。人間の決定（2026-10-02） | `internal/scaffold/detect.go` の `Detect` | 95% |
| 3 | USE CASE NAME | 設定に書く値を gh から引く | 動詞で終わる名詞句にする規約に合わせた | - | 85% |
| 4 | PRECONDITION | 利用者は continuo init か continuo setup を実行している | `scaffold.Detect` の呼び出し元はこの2つだけである | `internal/cli/cli.go` の `runInit` / `runSetup` | 95% |
| 5 | PRIMARY ACTOR / SECONDARY ACTORS | 利用者 / gh | どちらの呼び出し元も、人間が明示的に叩くコマンドである。値を引く相手は gh だけである | `internal/scaffold/detect.go` の `RunGH` | 95% |
| 6 | 「プレースホルダのまま残す」と書かないこと | 「値を決めないまま残す」と書く | `Detect` は値を決めないまま返すだけで、プレースホルダは書かない。プレースホルダが残るのは `continuo init` が雛形へ埋めるときである。`continuo setup` は owner か番号が決まらなければ終了コード 1 で止まる | `internal/scaffold/detect.go` の `Detect`、`internal/cli/cli.go` の `checkDetectionForSetup` | 95% |
| 7 | 基本フロー 2 の IF | owner の名前が渡されているかで分ける | `DetectOptions.Owner` が空でなければ gh を叩かない。どちらの枝も正常なので基本フロー内の IF にした | `internal/scaffold/detect.go` の `detectOwner` | 100% |
| 8 | 基本フロー 6 の VALIDATES THAT | user / organization 名として受け付けられるログイン名を応答する | 実装は「実行が失敗」「空文字」「`ValidOwner` を通らない」の3つを同じ扱い（決めない）にしている | `internal/scaffold/detect.go` の `detectOwner`、`internal/scaffold/fill.go` の `ValidOwner` | 95% |
| 9 | 基本フロー 9 の IF | ボードの番号が渡されているかで分ける | `DetectOptions.ProjectNumber` が0より大きければ gh を叩かない | `internal/scaffold/detect.go` の `detectProject` | 100% |
| 10 | 基本フローに「owner が決まっている」の検証を置かないこと | 置かない。owner を決められなかった実行は `ログイン名取得失敗` の中で最後まで扱う | owner が空のとき、`detectProject` も `detectRepositories` も gh を叩かずに理由だけを返す。基本フローに検証を置くと、「owner は決まらなかったのに検証は通る」という、実装が通れない経路が CFG に出る | `internal/scaffold/detect.go` の `detectProject` / `detectRepositories` | 95% |
| 11 | 基本フロー 13 の VALIDATES THAT | gh がボードの一覧を応答する | `gh project list` の実行失敗と、出力を JSON として解釈できない場合をまとめた。案内が同じ形である | `internal/scaffold/detect.go` の `detectProject` / `listProjects` / `parseProjectList` | 90% |
| 12 | 基本フロー 14 の IF の条件 | tracker.provider.owner の閉じていないボードが1件も無い | 実装は、渡された owner の名前でもログイン名でも同じ判定を通る。「ログイン名の」と書くと、渡された名前の場合を落とす | `internal/scaffold/detect.go` の `detectProject` | 95% |
| 13 | 基本フロー 17（一覧を引けなかった organization を候補に数えない） | 検証にせず、素の段として書く | 実装は、organization 1つで一覧を引けなくても残りを探す。organization の一覧そのものを引けなければ、空として進む。どちらも結果は「候補が0件」の検証（19）へ落ちるので、代替フローを立てても終わり方が変わらない | `internal/scaffold/detect.go` の `detectProject` / `listOrgs` | 85% |
| 14 | 基本フロー 19 と 20 を2つに割ったこと | 「1件以上あるか」と「1件だけか」に割る | 実装は候補0件・1件・複数の3つで案内を変える | `internal/scaffold/detect.go` の `detectProject` | 95% |
| 15 | 閉じたボードの扱い | 候補から落とす | `parseProjectList` が閉じたボードを落としてから件数を数える | `internal/scaffold/detect.go` の `parseProjectList` | 100% |
| 16 | 基本フロー 22 の書き方 | 「ボードの持ち主の名前を owner の値にする」（無条件） | 実装は、ボードの持ち主が owner と違うときだけ値と理由を書き換える。同じときは値が変わらないので、「持ち主の名前にする」と書けばどちらの場合も真である。IF にすると、organization を探していないのに持ち主が違う、という実装が通れない経路が出る | `internal/scaffold/detect.go` の `Detect` | 85% |
| 17 | 基本フロー 25・26・27 を3つに割ったこと | 「gh が応答する」「応答を読める」「リポジトリに属する issue が1件以上ある」 | 実装は3つで理由と案内を変える。gh の実行失敗は scope の案内と「手で書いてもよい」、JSON として読めない場合は「手で書く」だけ、0件は「信頼させたいリポジトリを手で書く」 | `internal/scaffold/detect.go` の `detectRepositories` / `parseItemRepositories` | 95% |
| 18 | 基本フロー 30〜34（リポジトリを集められたあとの案内） | 要らない行を消す案内、dry-run の案内、上限に達していれば打ち切りの案内 | 実装は案内を常に2つ記録し、読んだ項目の数が1回に読む上限（500件）以上なら3つ目を足す。どちらの枝も正常なので IF にした | `internal/scaffold/detect.go` の `detectRepositories` | 95% |
| 19 | 基本フロー 35 | 決めた値と記録した理由と案内を呼び出し元へ渡す | `Detect` は `Detection` を返すだけで、利用者へは出さない。応答するのは呼び出し元である | `internal/scaffold/detect.go` の `Detect` | 100% |
| 20 | 代替フロー6本の終わり方 | 全部 `RESUME STEP 35` | 実装は値を決められなくても打ち切らず、`Detection` を組み立てて返す。次に実際に行う処理は、呼び出し元へ渡すことである | `internal/scaffold/detect.go` の `Detect` | 95% |
| 21 | 代替フロー ログイン名取得失敗 の中身 | owner・ボード・リポジトリの3つの扱いをこの中で済ませる | owner が空なので、`detectProject` は番号が渡されていなければ gh を叩かずに返り、`detectRepositories` も gh を叩かずに返る | `internal/scaffold/detect.go` の `detectProject` / `detectRepositories` | 95% |
| 22 | ボードを決められない3本（一覧取得失敗・候補なし・候補が複数）の末尾 | trust.repositories を決めないまま残す段を、それぞれの末尾に持つ | 番号が 0 のままなので、`detectRepositories` は gh を叩かずに「owner とボードの番号を決めてから」と「手で書いてもよい」の案内だけを返す。別の代替フローにすると、「ボードは決まらなかったのに検証は通る」経路が出る | `internal/scaffold/detect.go` の `detectRepositories` | 95% |
| 23 | 代替フロー ボード候補が複数 の POSTCONDITION | 「利用者にボードを選ばせる問い合わせを出していない」と書く | 設計が「選ばせない」「標準入力を握らない」と明示している。対話を足す変更が入ったら落ちるようにした | `docs/plans/continuo_design.md`（3-32 の「対話で選ばせない」） | 100% |
| 24 | 案内の数 | 実装が記録する案内を1つずつ段にする | 実装の案内は、ログイン名で3つ、owner 未確定のボードで2つ、候補なしで3つ、候補が複数で2つ、リポジトリの前提が無いときで2つである | `internal/scaffold/detect.go` の `ownerAdvice` / `detectProject` / `detectRepositories` | 90% |
| 25 | 代替フロー リポジトリが1件も無い の POSTCONDITION | draft issue と、owner/repo の形でない名前を数えない | `parseItemRepositories` は、リポジトリを持たない項目と、`validOwnerRepo` を通らない名前の両方を落とす | `internal/scaffold/detect.go` の `parseItemRepositories` / `validOwnerRepo` | 95% |
| 26 | 「記録する」と「応答する」を分けたこと | この記述では全部「記録する」 | 実装は理由と案内を `Field` に溜めて返す。出すのは呼び出し元である | `internal/scaffold/detect.go` の `Field` | 95% |
| 27 | 用語の統一 | キーは `tracker.provider.owner` などのフルパスで書く | 雛形の中では短い名前だが、`branch_template` の中にも `owner` が出るため、短い名前では指すものが一意にならない | `internal/scaffold/fill.go` の `ownerPlaceholderCode` のコメント | 90% |
