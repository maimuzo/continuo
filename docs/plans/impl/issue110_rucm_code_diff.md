# issue #110: コードとユースケース記述（RUCM）の突き合わせの結果

issue #110（ユースケース記述（RUCM）6本から、テストが1本も生成されていない）の作業記録である。
2026-10-02 に、記述を1本ずつ実装と突き合わせた。**実装が正である。直すのは記述の側である。**
読むだけの役（subagent）が挙げた食い違いを、記述ごとに並べる。「確かめた」と書いていないものは、役が読んだだけで、まだ自分では確かめていない。

行番号は 2026-10-02 の commit `36fb9530` のものである。

## issueを1件処理する

`cycle` の経路8本（P011・P014・P015・P026・P028・P029・P031・P032）は、8本とも実装が本当にやり直すか待ち直す。戻り先の番号の誤りによる `cycle` は0本。

| 名前 | 記述 | 実装 | 状態 |
| --- | --- | --- | --- |
| D-1 `復帰の失敗` の分岐元 | `BRANCH FROM BASIC FLOW 24,25` | 確認の画面と期限切れは `confirmStartup`（段26）の中で起き、`internal/orchestrator/dispatch.go` の `startRunFromWorktree` の枝へ落ちる。段26 から `復帰の失敗` へ入る経路が CFG に無い | 未確認 |
| D-2 `復帰の失敗` の WHEN | 「理由を問わず」。根拠資料は `startRun` | `ErrStartupBusy` を除く。関数は `startRunFromWorktree`。`ErrStartupBusy` の着地（本文を送らず正常に戻る）の代替フローが無い | 未確認 |
| D-3 `起動の待ち直し` と `起動の断念` | 段3 が無条件。`起動の断念` は `failure_state` の1本 | もう一度起動するのは `agent_not_found` で hook が届いていないときだけ。リトライが残っていれば `addRetry` して `running_state` のまま。`working` のままの期限切れは `failRun` へ直行 | 未確認 |
| D-4 `騙りのhook` の段の並び | 段32（Stop が届く）→ 33 → 34（cwd を見る） | cwd の検査は `OnHook` の入口。捨てた hook は「届いた」に数えない。唯一の Stop なら `turnの終わりの取りこぼし` になる | 未確認 |
| D-5 turn の終わりの順番 | 37 表明 → 38・39 担当 → 40・41 Status 取り直し → 42 `UNTIL 表明の値が working でない` → 43・44 書く | `internal/orchestrator/lifecycle.go` の `handleTurnEnd`: 担当 → 表明 → Status を書く（毎 turn）→ 取り直す → `decideAfterTurn`。抜ける条件は取り直した Status が `active_states` に入っているか | 未確認 |
| D-6 `担当が移った` の段3 | `after_run` を実行する | `internal/orchestrator/handoff.go` の `stopHandoffLostClaimed` は `after_run` を走らせない（設計 3-77h も同じ） | 未確認 |
| D-7 `failRun` / `finishRun` を通るフローの段 | `起動直後の確認画面`・`paneの断念`・`消さないref`・`上限での打ち切り`・`権限の確認` に `after_run` などの段が無い | `failRun` は常に Status → 引き渡しのコメント → `ensureAgentComment` → `after_run` → pane を閉じる → 印を外す | 未確認 |
| D-8 記述に無い分岐 | 無い | 会話の記録が無い UUID なら復帰しない（`mayResumeSession`）。`after_create` と `before_run` の hook。`confirmTurnEnd` の3つ（読めない Stop・空の Stop のあとの待ち直し・レートリミット待ち）。`ensureAgentComment` の3つ | 未確認 |
| D-9 設計文書の数字 | 6-18 は「35本・42本」、6-18b は `BRANCH FROM BASIC FLOW 23,24`、6-18e は P013 以降が1つ古い。フロー名 `カンバンから消えたissue` | いまは36本・43本、`24,25`、`ボードから消えたissue` | 未確認 |

役が確かめていないもの: 冒頭の検査（段3〜13）、`無音の打ち切り`、POSTCONDITION の逐語、判断ログの出典。

## 設定ファイルを作る（2本に分けたあと）

本線の段の順番・代替フローの終わり方と戻り先・終了コードは実装と一致。フローチャートも rucm ブロックと一致。

| 名前 | 食い違い | 状態 |
| --- | --- | --- |
| A-1 | 判断ログが、2本に分ける前の段の番号と DEPENDENCY のまま | 直す |
| A-2 | 根拠資料の `docs/trying_it_out.md` の見出し「設定ファイルを置く」は実在しない。実物は「段3. 設定を置く」 | 直す |
| A-3 | 根拠資料に、INCLUDE した側の関数（`detectOwner` ほか）が残っている | 直す |
| A-4 | `--help` / `-h`（使い方を出して終了コード 0、ファイルは書かない）が記述に無い。`internal/cli/cli.go` の `parseErrorExitCode` | 直す |
| A-5 | `WORKFLOW.md` を書けない場合（force でディレクトリだった、権限が無い、差し替えの失敗など）の代替フローが無い。終了コード 1、2枚目は書かない。`TestInit_書けないとき落ちた当のファイルを名乗る` が既に在る | 直す |
| A-6 | 2枚目を書けない理由が symlink だけになっている。実装は「既にある」以外の失敗を全部同じ扱いにする | 直す |
| A-7 | 出力先（標準出力か標準エラーか）が POSTCONDITION に無い | 直す |
| A-8 | `見本がsymlink` から `RESUME STEP 17` で戻る経路は、基本フローの POSTCONDITION（2枚とも在る）を引き継ぐが、実際には2枚目は書かれていない | 直す（ABORT で終える形にする） |
| A-9 | シーケンス図が rucm ブロックより粗い（失敗の分岐と2枚目の分岐が無い） | 直す |

## 設定に書く値をghから引く

6本の代替フローの終わり方と戻り先（全部 `RESUME STEP 29`）、段の順番は実装と一致。

| 名前 | 食い違い | 状態 |
| --- | --- | --- |
| B-1 | 「プレースホルダのまま残す」は `continuo init` から呼ばれたときだけ当たる。`Detect` は値を決めないまま返すだけ。`continuo setup` は owner か番号が決まらなければ終了コード 1 で止める（`checkDetectionForSetup`） | 直す |
| B-2 | リポジトリを拾えたときの案内は2つ（要らない行を消す・`continuo trust --dry-run`）。読んだ項目が500件以上なら打ち切りの案内を足す | 直す |
| B-3 | `ボードの項目を引けない` は原因が2つ（gh の実行失敗 / JSON として読めない）で、案内が違う | 直す |
| B-4 | organization 1つの失敗は飛ばして残りを探す。organization の一覧を引けなければ空として進む。owner と同じ名前の organization は飛ばす | 直す |
| B-5 | 段14「ログイン名のボードが1件も無い」は、渡された owner の名前でも同じ判定を通る | 直す |
| B-6 | 段21（owner を決め直す）は、ボードの持ち主が owner と違うときだけ値と理由を書き換える | 直す |
| B-7 | 案内の数が記述より多い（`advice_where`・`advice_owner_first`・`advice_write_by_hand_optional`・`advice_rerun`・`advice_pick_owner`） | 直す |
| B-8 | `リポジトリが1件も無い`: 落とすのは draft と、`owner/repo` の形でない名前の両方 | 直す |
| B-9 | シーケンス図の最後の矢印が「利用者へ渡す」。実装は呼び出し元へ返すだけ | 直す |
| B-10 | 冒頭の注記の「30通り × 14通り」は、分ける前の数 | 直す |

対象外で気づいたこと（実装のコメントが古い。記述ではないので、この issue では直さない。人間へ報告する）:
`internal/scaffold/scaffold.go` のパッケージコメント「置くのは1枚である」、`internal/cli/cli.go` の `runInit` のコメント「雛形を1つだけ置く」、`WriteTemplateWithValues` と `Detect` のコメントが `trust.repositories` を挙げていない。

## 対象リポジトリを信頼登録する（`continuo trust`）

実装の入口: `internal/cli/cli.go` の `runTrust`、`internal/trust/trust.go` の `Plan` / `inspect` / `Apply`、`internal/trust/report.go` の `WriteRequirements` / `WriteApplyResult`。根拠資料の欄に、このどれも載っていない。

- `cycle` 1本（P006。`clone不在` の `RESUME STEP 3`）は誤り。実装は clone が無ければ continuo 自身が `ghq get` で取りに行き（`fetchClone`）、取れなければその1件だけ諦めて次のリポジトリへ進み、最後に終了コード 1。`--dry-run` では取りに行かない。利用者が clone を置く段も、段3 へ戻るやり直しも無い
- PRECONDITION が古い（doctor・ボード・gh の scope）。`runTrust` はカンバンも gh も読まない。対象は `trust.repositories` だけ
- 記述に無い分岐: 引数の誤り（2。`--help` は 0）、cwd・設定・ホームの失敗（3）、`trust.repositories` が空（0）、1件ごとの Problem（`owner/repo` の形でない・鍵を git で確定できない・信頼の状態を読めない。残りは続行、1）、`Unconfirmed`（対象から外す、1）、`~/.claude.json` が無い・symlink・通常ファイルでない（1バイトも書かず 3）、写しを作れない・差し替えの失敗（3）、書き込む前の警告（Claude Code を閉じる案内）
- `変えるものが無い` は実装では2つの経路（対象0件は読み直さずに返る / 全件が信頼済みは読み直しと形の検査のあとで返る）
- 段の順番: 記述は 写し → 読み直し → 形の検査。実装（`Apply`）は 読み直し → 形の検査 → 変える項目の判定 → 写し → 差し替え
- 終了コードがどのフローにも無い（0 / 1 / 2 / 3）。要求内容と結果は標準出力、3 の理由は標準エラー
- `docs/trying_it_out.md` の段の見出しが古い（いまは 段3 設定を置く / 段5 対象リポジトリを信頼に登録する / 段6 前提が揃っているかを検査する / 段7 issue を1件用意する）
- 判断ログの 3〜41 は書き直す前の版（doctor を叩いて Claude Code で承認する12段）のまま。合うのは 42〜46 だけ
- シーケンス図は古い版のまま（rucm ブロックに無い段だけで出来ている）。フローチャートは rucm ブロックと一致

## 前提が揃っているかを検査する（`continuo doctor`）

- `cycle` 35本は全部正しい（`DO … UNTIL` の戻りだけ）。ABORT 5本と `RESUME STEP 7` / `RESUME STEP 14` も実装と一致。図も rucm ブロックと一致
- 見出し語が2つ足りない: `未記入の項目`（`checkMissingKeys`）と `プロンプトの変数`（`checkPromptVariables`）。表は16行、実装は18個
- 記述に無い分岐: `--missing-keys-patch`（検査をせず差分だけを出す。0 か 1。このあと実装を直して 0 か 3）、cwd を引けない（標準エラー、1。このあと実装を直して 3）、`--help`（0）、設定のパスを決められないときの警告（続行）
- `worktree の場所`: 書けたあと壊れた worktree を調べ、`on_broken_worktree` が `stop` なら `✗`。`clone`: `ghq` と `git` が PATH に無ければ `✗`。`clone` の期限切れは専用の扱いが無い
- 段7「残りの見出し語の並びを決める」に当たる処理は無い（順はコードに固定）
- 用語: 実装と設計は「カンバン」。記述は「ボード」
- 判断ログの古い箇所: 9（見出し語12件）、「落としたもの」の表（11件）、25（`WorkspaceConfig` は `internal/config/types.go`）

## continuoを入れる（`install.sh`）

- `照合を省く指定`（GLOBAL、`RESUME STEP 10`）は実装と違う。`--insecure-no-checksum` は照合を省かない。照合できなかったとき（`shasum` も `sha256sum` も無い / `checksums.txt` を取れない / 行が無い）だけ、警告を出して続ける。値が合わなければ、指定が在っても止まる。分かれ目は `照合できない` の中
- `対応しないOS`: WSL2 を案内するのは Windows 系だけ
- 記述に無い分岐: 版の問い合わせの失敗（200 でも 404 でもない）、`--version` を渡したとき問い合わせない、出所の確認（`verify_provenance`。止めない）、展開と配置の失敗5つ、`--yes`、足りない道具が0件、入れるコマンドが無い、導入の失敗（警告して続行）、PATH の案内は条件付き、既定でない取得先の確認、破壊的変更の案内、値の無いオプション
- 終了コードと出力先がどのフローにも無い（`die` は 1、`--help` は 0、中断は 130 / 143）
- 判断ログ 8・9・18 が古い。`README.ja.md` の見出しは「インストール」
- シーケンス図に代替フロー5本が無い

## 既存のボードのStatusを割り当てる（`continuo setup`）

- `cycle` 3本（P005・P006・P008）は正しい（`internal/setup/assign.go` の `Assign` が本当に尋ね直す）
- 役割は6つ（`direct_chat_state` が増えた。0 で飛ばせる）。記述と図は「5つ」「5回」
- 値を決める段: 記述は BASIC FLOW 3 の1段。実装は フラグ → WORKFLOW.md の値 → `d.ScaffoldDetect` → `checkDetectionForSetup`（owner 空か番号 0 以下なら標準エラー、1）。`設定に書く値をghから引く` を INCLUDE すべき。決めたあと「使うカンバン」を標準出力へ出す段が無い
- `中断`（BRANCH FROM BASIC FLOW 15）は実装では中断にならない。Ctrl+C を見るのは番号待ち（BASIC FLOW 9）のあいだだけ（`lineReader.read`）。終了コード 1 が書かれていない
- `書き換える対象のキーが無い`（RFS 16）は、実装では対話より前（`scaffold.CheckUpdatable`）で起きる。文言は2つの場合で違う
- `書き換えると読めなくなる` の2段目の案内は実装に無い
- 記述に無い分岐: 引数の誤り（2）、番号として読めない入力、1行が4096バイト超、EOF（1）、ディレクトリ・symlink・読めないなど（1）、書き込みの失敗（1）、`direct_chat_state` の行が無いときの知らせ
- 出力先と、判断ログの「7つのキー」「`cmd/continuo/main.go` の `runSetup`」「front matter を読み込まない」が古い
- `docs/trying_it_out.md` の段4 の「WORKFLOW.md から決めない」も古い（文書の側。記述ではない）

## ボードを新規に用意する（消した）

- 記述の3ファイル（`.rucm.md`・`.cfg.json`・`.judge_log.md`）は消した（人間の決定。2026-10-02）
- 理由: continuo にカンバンを作る機能は無く、この記述は continuo の動きを1つも書いていなかった。同じ手順（`gh project list` で番号を見る・`gh project field-list` で選択肢を見る・足りない選択肢を GitHub の画面から足す）は `docs/trying_it_out.md` の「段2. 使うカンバンを確かめる（作らない）」と `README.ja.md` の「必要なもの」に在る
- 突き合わせで出た食い違い: 手順の根拠（`gh project create`・`gh project field-create`・同名の検査）が、`README.ja.md` と `docs/trying_it_out.md` のどちらにも無かった。設計 3-34 の道は「設定を縮める」と「選択肢を画面で足す」の2つである
- シナリオ `はじめてcontinuoを動かせるようにする` は、この記述を INCLUDE せず、事前条件「利用者は使うボードの番号を控えている」で受ける

## はじめてcontinuoを動かせるようにする（シナリオ）

- `cycle` 2本（`前提の不足` の `RESUME STEP 17`）は正しい（やり直すのは人間で、文書に在る）
- BASIC FLOW 4〜7（ボードを持っていない枝）は、文書に根拠の無い段を引いていた。引いていた記述 `ボードを新規に用意する` は消し、シナリオは事前条件「利用者は使うボードの番号を控えている」で受ける
- BASIC FLOW 12（利用者が clone を作る）は古い。いまは `continuo trust` が clone を取る
- 「7件の見出し語」→ 18件。pane の label は URL ではなく `owner/repo/issues/N`
- 記述に無い: `continuo allow-keychain-access`、`trust.repositories` の要らない行を消す段、起動時検査のほかの項目、着手の段（空きスロット・入札・`after_create`・`before_run` ほか）、`確認の画面` 以外の起動の失敗
- 段13〜15（issue を作る）の位置が文書と逆（文書は trust → doctor → issue）
- `未信頼のリポジトリ` の ABORT: 実装は止まらず、その issue を飛ばして巡回を続ける
- 判断ログ #12・#13・#15・#35〜#38・#43 が古い

## 指示書に沿ってissueを1件仕上げる

- `cycle` 36本は全部正しい（任意時点の代替フロー `進捗報告を書く` が入った段へ戻る）
- 記述は、いまの `internal/prompt/builtin.md` より古い。無いもの: 計画のあとの人間確認（3-2。1回の run では段16 から段17 へ進めない）、レビューループと打ち切り（5-6）、「対応するか」の問いと出口、draft を外す段、検査の回し直し、7-2、止まる出口（読めない・pull request を作れない・立場が OWNER などでない）、`blocked` の前の commit と push、進捗報告のときの push、AI の印の条件、分岐元の決め方の4段
- 段5 と段6 が逆（実装は断片ごとに展開してから継ぎ合わせる）
- 段14（CLAUDE.md ほかを読む）の出どころは `builtin.md` ではなく雛形の本文
- `本文が空になる` の段2（内訳を出す）は、着手の経路では起きない
- システムの側の分岐（変数展開の失敗 → `failRun`、表明が無いときの継続、成果のコメントが無いときの書かせ直し）が無い
- 判断ログの段番号が3つ古い。根拠資料に 5-3u・5-3n・5-3s・5-3j が無い

## issueを着手から片付けまで見届ける（シナリオ）

- `cycle` 3本は全部正しい
- 段6 の検査（review の遷移先か）をするコードは無い（構造上の段）
- 段5 の INCLUDE と `判断の依頼` の INCLUDE が、同じ処理（表明を読む → Status → pane を閉じる）を2回書いている
- 記述に無い: direct chat、continuo が自分で `failure_state` へ落とす経路
- POSTCONDITION の条件付きのもの（`cleanup.delete_branch`、turn 数、引き継いだ回数）

## 着手を取り消す（`continuo abandon`）

- ABORT 17本は全部正しい（`internal/abandon/abandon.go` の `run` がその場で終了コードを返して終わる。やり直して先へ進む経路は無い）
- A1（重い）direct chat の検査3つが記述に無い: いまの Status が direct chat なら `--force` でも止まる / `--to` の先が direct chat / park の先が direct chat（`verifyTargets`。どれも `ExitStopped`）。「いまの Status が direct chat か」は park の先の検査（ステップ13）より前に走る。テスト3本が既に在る
- A2（重い）既定の設定（`DirectChatState: "Direct Chat"`）では、ステップ13〜14 の時点で必ずボードを読む。記述の表は「ボードへ問い合わせずに通す」
- A3 基本フローの POSTCONDITION（workspace は閉じている・branch が無い）が、残ったものがある実行と `cleanup.delete_branch` が偽の実行で偽になる
- A4 フローチャートの中断の WHEN が rucm ブロックと違う
- A5 `残ったbranchを消せない` に3つ目の理由（`git branch -D` のそれ以外の失敗）が無い
- A6・A7 軽い食い違い（身元の無い worktree を数えるだけの枝 / herdr が答えない最中の中断では pane の ID を出せない）
- A8 判断ログ 35・49・69・91 と本文の `--permission-mode dontAsk` が古い

## 再起動して実行中のissueを引き継ぐ

- ABORT で正しい: `二重起動`・`前提の不足`・`復元できない壊れたworktree` の stop 側・`中断`・`中断の連打`
- R1（重い）`ボードの取り直しの失敗` は中身が実装と逆。実装（`internal/orchestrator/restore.go` の `decideOne`）は pane を閉じず、閉じる集合へ入れるだけ
- R2（重い）その1件だけ諦めて起動を続けるのに ABORT で終わっているフローが10本: `復元できない壊れたworktree`（skip 側）・`名乗りの食い違い`・`ボードの取り直しの失敗`・`issueの取り違え`・`一覧の取得の失敗`・`引き渡し状態`・`状態の不明`・`権限の確認での停止`・`引き継ぎの上限`・`paneの不在`。各フローの POSTCONDITION 自身が「continuo は常駐している」と書いている。実装で実際に続くのは 22 → 23 → 25 → 33〜41（その run に当てはまらない 24・26〜32 は通らない）。`paneの不在` の次は 33
- R3（重い）ステップ14 の偽の側が実装と合わない: `cleanup.on_states` → pane を閉じて片付け（フロー無し）/ `direct_chat_state` → 引き継ぐ側（指示は送らない）/ 取り直しで見つからない → 何もしない
- R4（重い）引き継がない分岐が3つ記述に無い: socket のパスが前回と違う / pane に agent 名が無い / セッション UUID を取れない（どれも pane を閉じて閉じた記録）
- R5 direct chat のカードでは `状態の不明`・`権限の確認での停止`・`引き継ぎの上限` が pane を閉じない
- R6 コメント数（`moveToFailure` が引き渡しの通知も投稿するので最大2件）
- R7 `paneの不在` は既定値（`redispatch`）の場合しか表していない（`to_dispatch_state` / `to_failure_state` / cleanup の Status / 引き渡しの Status）
- R8 段の順番が実装と違う（壊れた worktree の扱いが走査より先、`matchPanes` が照合より先、20 → 19 → 15・16 → 17・18、Stop を待つ・継続の指示は巡回の turn ループが行うので 41 のあと、ダッシュボードの listen が無い）
- R9〜R11 `一覧の取得の失敗` で候補を閉じる集合へ入れること、ステップ34 の取得失敗、図、判断ログのステップ番号（13・14・31〜36・41・44・45・53）と判断31・43

## 夜に機械を落として翌朝に担当を続ける（シナリオ）

- `朝までに担当が移っている` の ABORT は正しい。`期限をまたぐ` の ABORT は要判断（翌朝に利用者が起動するのは変わらず起きる。戻すならステップ4）
- N1（重い）ステップ6 の INCLUDE 先（`再起動して実行中のissueを引き継ぐ`）の PRECONDITION は「pane は生きている」。このシナリオは電源を落とすので合わない。ステップ9 は pane が無いときの再 dispatch（`--resume`）の姿
- N2 `朝までに担当が移っている` のステップ1（released のコメントを記録に残す）は、pane が無い経路では起きない
- N3 POSTCONDITION「コメントを1件も書いていない」が偽になる経路がある
- N4 `期限をまたぐ` に hold を書く段が無い
- N5（未確認）ステップ10「システムは issue に進捗のコメントを1件書く」に当たるコードが見つかっていない

## 本家のリポジトリへPRを出す

- PR-1（重い）`既定branchが分からない`: 記述は「飛ばす。Status は着手待ちのまま」。実装は先に `running_state` を書き（`internal/orchestrator/dispatch.go` の `startRun`）、`Prepare` が `ErrBaseUnknown` を返すと `failRun` が `failure_state` へ落として引き渡しの通知を投稿する
- PR-2 段3〜8 の順番: 実装は worktree → `after_create` → 設定ファイル → 身元ファイル（設定ファイルが先）。base の決め方は3段（`herdr.worktree.base` → issue にリンクされた branch → 既定 branch）。PRECONDITION に前の2つが無いことが要る。`after_create`・`before_run`・`running_state` を書く段が無い
- PR-3（重い）`作業ディレクトリがworktreeの外`: 記述は `checkStalls`（`turn_timeout_ms`）で拾う。通常の turn では turn ループが `settle_ms` で先に拾う（`confirmTurnEnd` → `turnStalled` → `abandonRun`）。リトライが残っていればコメントを書かず着手からやり直す
- PR-4（重い）片付けを見送ったときのコメント: 巡回の片付け（`internal/orchestrator/reconcile.go` の `reconcileWorktrees`）は、見送っても issue へコメントせず、時刻も書かない。書くのは `cleanupPath` を通る別の契機だけ
- PR-5 `リモート追跡refに載っていない`: upstream が在る branch は base と差分が無くても見送られる。base が空のときの見送りも無い。POSTCONDITION「worktree は残っている」が `RESUME STEP 26` と合わない
- PR-6 小さい抜け: 公開かどうかを取れなかったときも判定の hook を掛ける / pane を閉じる段 / `cleanup.delete_branch` の前提
- シーケンス図が PR-4 と同じ誤りを持つ。判断ログ36 が古い

## worktreeとbranchを片付ける

- 基本フローの段9〜31 の順は `internal/workspace/cleanup.go` の `Cleanup` と一致（branch の判定の時点だけ違う）
- 片付け-1（重い）巡回の道では、見送りのコメントも時刻も書かない（PR-4 と同じ）
- 片付け-2 `片付けの無効` と `材料を取れない` のログは、巡回の道では出ない
- 片付け-3（重い）`片付けの対象外`: pane の一覧を要求するのは Status が `active_states` のときだけ。一覧は全 pane。`direct_chat_state` なら閉じる集合へ入れる、ほか書かれていない分岐
- 片付け-4 `Cleanup` の中で打ち切る分岐が無い: `ErrRepoMismatch`、`ErrCloneBusy`（次の巡回へ回す）、`cleanup.require_clean_worktree` / `cleanup.require_pushed` が偽なら検査を飛ばす、段7 と段8 は両方評価する
- 片付け-5 branch の段: 「検算に落ちて残す」「削除に失敗して残す」が無い。`Leftovers` を画面へ出すのは `continuo abandon` だけ。`壊れたref` の WHEN に `cleanup.delete_branch` が真が無い
- 片付け-6 親 workspace: herdr 0.9.0 以降は、配下が在ると親を閉じるのを断られて何も閉じない（`closeRepoWorkspaceLocked`）。本文の「親を閉じると配下も消える」は 0.8.x の話
- 判断ログ18d のテスト名が実在しない（いまは `TestLive_WorkspaceClose_配下があると親は断られ何も消えない`）。シーケンス図に3つの誤り

## 画面に出す文言の言語を決める

- 段1・3〜8・13〜17・19〜25、`文言が登録されていない` の `RESUME STEP 26`、コマンドの表は実装と一致
- 言語-1（重い）`対応していない言語の指定` の ABORT: 実装では「読み取れない」と同じ1つの失敗（`config.Load` の中の `validateLanguage`）で、止まるかどうかはコマンドで決まる（`doctor` と `setup` は続ける。`continuo`・`trust`・`prompt` は止まる）
- 言語-2 基本フローの POSTCONDITION: 訳が無ければ日本語の資源の文言を返す枝（段22〜24）と合わない
- 言語-3 段2: 日本語の側は「在る」ではなく「空でない」。JSON が壊れている場合が無い
- 判断ログ 32・37・38・40 と根拠資料の 3-35g の添え書きが古い。シーケンス図に打ち切りと代替フロー2本が無い

## レートリミットで待って再開する

- `cycle` 2本（P004・P008）は正しい。P008（`手放さずに待ち続ける` の `RESUME STEP 3`）は、実装のやり直しの起点が巡回の頭なので、戻り先は厳密には段1
- 1-1 段の順番: 実装（`internal/orchestrator/reconcile.go` の `checkStalls`）は使用率100 の検査より前に `agent_status` を見る。`working` なら印を立てずに待つ
- 1-2 基本フローが2つの実装経路（巡回の `clearQuotaWaitWhenBack` と turn の `afterWaitTimeout` → `afterQuotaReset`）を1本に混ぜている。巡回が継続の指示を送る経路は実装に無い
- 1-3 印を外す契機が1つ足りない（使い切っている枠が無くなった）
- 1-4 段14 の偽の側: `unknown` と読み取り失敗は実装では継続の指示を送る。`blocked` は引き渡し
- 1-5 `枠を読めない` に専用の分岐は無い（`枠の残り` と同じ道）
- 1-6 pane を閉じる3本に `after_run` が無い。1-7 リトライを使い切ったときの分岐が無い。1-8 `応答の途絶` だけリトライとバックオフの段が無い
- 1-9〜1-11 `待つ上限を超えた` の失敗の分岐、WHEN に担当者0人が無い、印を外す段が無い
- 1-12 段15 と16 の順番（turn 数を先に増やす）、`max_dispatch_turns` の検査
- 1-14 フローチャートに `RESUME STEP 3` から段3 へ戻る辺が無い。1-16 判断ログの段番号が古い

## 人間に判断を渡す

- 2-1 `知らない表明` の ABORT: 実装は WARN を出して無視し、次の turn を送る。促す1文は表明の行が1行も無いときだけ。打ち切りではなく段1 へ戻るやり直し
- 2-2 写像の値が null（既定の `working`）の分岐が無い（Status を動かさず次の turn へ）
- 2-3 段7「`failure_state` を書く」は、実装では `status_signal_map` の `blocked` の値
- 2-4 `完了済みのissue` に3つの段が無い（run のコメントの確認・`after_run`・`cleanup.on_states` なら片付け）
- 2-5 `コメントの取り戻し` の成功後、pane を閉じて記録を書くのは取り戻しの中。2-6 失敗のあとも `after_run` などへ合流する。2-7 失敗の入口が段7 だけになっている（ほかに4つ。黙って先へ進む入口も5つ）
- 2-8 基本フローに無い段（direct chat の検査、担当の確かめ、取り直しと `decideAfterTurn`、`waitForBackgroundTasks`）
- 2-9 `知らないStatus` の POSTCONDITION（`cleanup.on_states` に在ると worktree は消える）
- 2-10 `turnの猶予` の ABORT: 実装は次の巡回で同じ判定をやり直す（待ってから同じ段をもう一度）
- 2-11 `自動化の書き戻し` の失敗の分岐。2-12 `レビューの完了` は `cleanup.on_states` で判定する
- 2-14 判断ログの段番号と #44〜#46（IF-ENDIF で書いたと言っているが、いまは代替フロー）

## issueの担当を入札で決める

- 終わり方（`他人の担当` の `RESUME STEP 6`、`期限内の担当`・`入札に負けた`・`枠を読めない`・`余裕値が0以下` の ABORT）は実装と一致
- 3-1 段14「`bid_window_ms` のあいだ待つ」は実装に在る待ちではない。入札を書いた巡回は終わり、締め切りを過ぎた後の巡回が勝敗を決める。締め切りは「その回でいちばん古い入札の時刻 + window」。`締め切り待ちの中断` で止める待ちも無い
- 3-2 段3（コメントを全件取る）の位置: 担当者2人以上と枠の判定は、コメントを読む前
- 3-3 段2 の一覧は `dispatch_state` ではなく `active_states`
- 3-4 `人間が付けた担当` と `担当者が2人以上` の POSTCONDITION「コメントは増えていない」: 既定では案内のコメントを1回書く（`postGateNotice`）
- 3-5 `他人の担当` 段1: いまの担当者を名指しする hold だけを数える
- 3-6 記述に無い失敗の分岐が9つ。3-7 同点の3段目（アカウント名の小さい順）
- 3-8 段23 の位置（印の付いたコメントを外すのは `FetchComments` の中）。3-9 段4 と5 の順番
- 3-10 フローチャートに `RESUME STEP 6` から段6 へ戻る辺が無い。3-11 `evaluateBid` は実在しない（`evaluateBidWith`）。判断ログ #17・#33 が古い
