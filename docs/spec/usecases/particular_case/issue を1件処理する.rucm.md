# ユースケース: issue を1件処理する

## 根拠資料

- `docs/plans/continuo_design.md#3-2`（turn の終わりの判定の規則。settle_ms と task-notification）
- `docs/plans/continuo_design.md#3-5`（完了検知の3層と、1つの turn で何が起きるか）
- `docs/plans/continuo_design.md#3-6`（dispatch の直前に issue ごとに検査するもの）
- `docs/plans/continuo_design.md#3-8`（turn ループ。1回目の本文と継続の指示、max_dispatch_turns）
- `docs/plans/continuo_design.md#3-16`（着手の手順の順番。段-1 から段11）
- `docs/plans/continuo_design.md#3-18`（worktree の身元ファイル）
- `docs/plans/continuo_design.md#3-21`（打ち切りは「画面の版」で測る）
- `docs/plans/continuo_design.md#3-25`（表明を transcript から読む。コメントが無かったらセッションを復元して書かせる）
- `docs/plans/continuo_design.md#3-34`（候補の絞り込みはサーバ側の検索であり、書いた値の反映が遅れる）
- `docs/plans/continuo_design.md#4-1`（誰がどの遷移を起こすか）
- `internal/orchestrator/dispatch.go` の `dispatchCandidates`、`claimForDispatch`、`preflight`、`startRun`、`confirmStartup`
- `internal/orchestrator/failure.go` の `noteFailure`、`skipByFailure`
- `internal/orchestrator/turn.go` の `turnLoop`、`sendTurn`、`afterWaitTimeout`、`confirmTurnEnd`、`turnSendFailed` と `turnTransient`
- `internal/orchestrator/lifecycle.go` の `handleTurnEnd`、`readSignals`、`applySignals`、`finishRun`
- `internal/orchestrator/comment.go` の `ensureAgentComment`
- `internal/orchestrator/signal.go` の `ParseSignals`
- `internal/workspace/prepare.go` の `CheckWorktreeUsable`、`checkBranchFree`、`Prepare`
- `internal/tracker/adapter.go` の `dropUnrequestedStates`、`UpdateStatus`

## RUCM

```rucm
USE CASE NAME: issue を1件処理する
BRIEF DESCRIPTION: 巡回タイマーが巡回を起こす。システムはボードから候補を取り、着手できることを確かめてから先頭の1件に印を付けて worktree と worker を用意する。システムは turn を送り、Stop hook で turn の終わりを判定し、transcript の表明を読む。システムは表明の値どおりにボードの Status を書き、worker を止める。
PRECONDITION: システムは常駐している。システムはロックファイルの flock を取っている。ボードの Status の選択肢名は設定と一致する。ボードの dispatch_state の Status に issue が1件以上ある。herdr は待ち受けている。
PRIMARY ACTOR: 巡回タイマー
SECONDARY ACTORS: GitHub Projects v2、herdr、Claude Code
DEPENDENCY: なし
GENERALIZATION: なし

BASIC FLOW:
1. 巡回タイマーはシステムに巡回の開始を要求する。
2. システムはボードから active_states の issue の一覧を取る。
3. システムは VALIDATES THAT 先頭の issue の Status が active_states に入っている。
4. システムは VALIDATES THAT 先頭の issue の失敗の回数が max_retries を超えていない。
5. システムは VALIDATES THAT 空きスロットが1つ以上ある。
6. システムは VALIDATES THAT 先頭の issue の対象リポジトリが Claude Code に信頼登録されている。
7. システムは VALIDATES THAT 先頭の issue の worktree の置き場所をそのまま使える。
8. システムは VALIDATES THAT 先頭の issue の branch を置き場所以外の worktree が使っていない。
9. システムは先頭の issue に印を付ける。
10. システムは VALIDATES THAT ID 指定で取り直したボードの issue の Status が active_states に入っている。
11. システムはボードの issue の Status に running_state の選択肢を書く。
12. システムは Status を動かした記録を issue にコメントする。
13. システムは workspace.root の下に issue の worktree を作る。
14. システムは worktree の絶対パスとリポジトリ本体の作業ディレクトリを渡して workspace として開き、その label に owner/repo/issues/N を書く。
15. システムは Claude Code の設定ファイルを worktree の外に書く。
16. システムは worktree の中に身元ファイルを書く。
17. システムは herdr に workspace の pane の一覧を要求する。
18. システムは pane の label に owner/repo/issues/N を書く。
19. システムは VALIDATES THAT pane が Claude Code の起動を受け付ける。
20. システムは pane で Claude Code を起動する。
21. システムは VALIDATES THAT Claude Code の agent_status が idle または done であり、かつ interactive_ready が真である。
22. DO
23.   システムは VALIDATES THAT turn 数が max_dispatch_turns に達していない。
24.   システムは Claude Code に turn の本文を送る。
25.   システムは Claude Code の Stop hook を受ける。
26.   システムは settle_ms のあいだ待つ。
27.   システムは VALIDATES THAT settle_ms のあいだに task-notification で始まる UserPromptSubmit が届かない。
28.   システムは transcript から表明の行を読む。
29.   システムはボードの issue の Status を ID 指定で取り直す。
30. UNTIL 表明の値が working でない
31. システムはボードの issue の Status に表明の値の遷移先の選択肢を書く。
32. システムは Status を動かした記録を issue にコメントする。
33. システムは VALIDATES THAT issue に今回の run が書いたコメントがある。
34. システムは workspace_hooks の after_run を実行する。
35. システムは herdr の pane を閉じる。
36. システムは印を外す。
POSTCONDITION: issue の Status は表明の値の遷移先の選択肢である。issue にエージェントが書いたコメントが1件以上ある。herdr の pane は閉じている。印は外れている。worktree と branch は残っている。

SPECIFIC ALTERNATIVE FLOW 頼んでいないStatus:
RFS BASIC FLOW 3
1. システムはこの issue を dispatch の対象から外す。
2. システムは頼んだ Status に無い候補が返ったことを記録に残す。
3. ABORT
POSTCONDITION: issue の Status はボードにある選択肢のままである。ボードへは1バイトも書いていない。他の候補の dispatch は続いている。

SPECIFIC ALTERNATIVE FLOW 失敗の繰り返し:
RFS BASIC FLOW 4
1. システムはこの issue を dispatch の対象から外す。
2. ABORT
POSTCONDITION: issue の Status はボードにある選択肢のままである。worktree は作られていない。印は付いていない。

SPECIFIC ALTERNATIVE FLOW 空きスロット不足:
RFS BASIC FLOW 5
1. システムはこの巡回で issue を1件も dispatch しない。
2. ABORT
POSTCONDITION: 印の件数は変わっていない。issue の Status は dispatch_state の選択肢のままである。worktree は作られていない。

SPECIFIC ALTERNATIVE FLOW 未信頼のリポジトリ:
RFS BASIC FLOW 6
1. システムは issue を dispatch の対象から外す。
2. システムは issue に信頼登録の承認を促すコメントを1件書く。
3. システムは対象リポジトリを通知済みとして記録する。
4. ABORT
POSTCONDITION: issue の Status は dispatch_state の選択肢のままである。worktree は作られていない。issue に信頼登録の承認を促すコメントが1件ある。

SPECIFIC ALTERNATIVE FLOW 使えないworktree:
RFS BASIC FLOW 7
1. システムはこの issue を dispatch の対象から外す。
2. システムは置き場所をそのまま使えない理由を記録に残す。
3. ABORT
POSTCONDITION: issue の Status は dispatch_state の選択肢のままである。ボードへは1バイトも書いていない。worktree は作られていない。

SPECIFIC ALTERNATIVE FLOW 使われているbranch:
RFS BASIC FLOW 8
1. システムはこの issue を dispatch の対象から外す。
2. システムは branch を使っている worktree の場所と片付けの手順を記録に残す。
3. ABORT
POSTCONDITION: issue の Status は dispatch_state の選択肢のままである。ボードへは1バイトも書いていない。worktree は作られていない。branch を使っている worktree は残っている。

SPECIFIC ALTERNATIVE FLOW 書かずに取りやめる:
RFS BASIC FLOW 10
1. システムは印を外す。
2. ABORT
POSTCONDITION: issue の Status はボードにある選択肢のままである。worktree は作られていない。issue にコメントは付いていない。

SPECIFIC ALTERNATIVE FLOW 起動直後の確認画面:
RFS BASIC FLOW 21
1. システムは pane に esc のキー入力を送る。
2. システムはボードの issue の Status に failure_state の選択肢を書く。
3. システムは herdr の pane を閉じる。
4. システムは印を外す。
5. ABORT
POSTCONDITION: issue の Status は failure_state の選択肢である。turn の本文は Claude Code に届いていない。worktree は残っている。

SPECIFIC ALTERNATIVE FLOW 起動の待ち直し:
RFS BASIC FLOW 21
1. システムは 500 ミリ秒待つ。
2. システムは VALIDATES THAT 起動を待ち始めてから herdr.startup_timeout_ms が経っていない。
3. システムは pane で Claude Code をもう一度起動する。
4. RESUME STEP 21
POSTCONDITION: Claude Code が入力を受け付けられるようになるまで待ち続けている。turn の本文はまだ送っていない。

SPECIFIC ALTERNATIVE FLOW 起動の断念:
RFS 起動の待ち直し 2
1. システムは agent.max_retries の回数まで、バックオフしてから着手をやり直す。
2. システムはボードの issue の Status に failure_state の選択肢を書く。
3. システムは issue に起動できなかった理由を1件コメントする。
4. システムは herdr の pane を閉じる。
5. システムは印を外す。
6. ABORT
POSTCONDITION: issue の Status は failure_state の選択肢である。turn の本文は Claude Code に届いていない。worktree は残っている。

SPECIFIC ALTERNATIVE FLOW paneがまだ使えない:
RFS BASIC FLOW 19
1. システムは 500 ミリ秒待つ。
2. システムは VALIDATES THAT pane を待ち始めてから 30 秒が経っていない。
3. RESUME STEP 19
POSTCONDITION: pane が起動を受け付けるまで待ち続けている。Claude Code はまだ起動していない。

SPECIFIC ALTERNATIVE FLOW paneの断念:
RFS paneがまだ使えない 2
1. システムはボードの issue の Status に failure_state の選択肢を書く。
2. システムは issue に pane が使えなかった理由を1件コメントする。
3. システムは herdr の pane を閉じる。
4. システムは印を外す。
5. ABORT
POSTCONDITION: issue の Status は failure_state の選択肢である。Claude Code は起動していない。worktree は残っている。

SPECIFIC ALTERNATIVE FLOW 上限での打ち切り:
RFS BASIC FLOW 23
1. システムはボードの issue の Status に failure_state の選択肢を書く。
2. システムは issue に打ち切りの理由を1件コメントする。
3. システムは herdr の pane を閉じる。
4. システムは印を外す。
5. ABORT
POSTCONDITION: issue の Status は failure_state の選択肢である。turn 数は max_dispatch_turns と等しい。worktree は残っている。issue に打ち切りの理由のコメントが1件ある。

SPECIFIC ALTERNATIVE FLOW turnの継続:
RFS BASIC FLOW 27
1. システムは turn がまだ続いているとみなす。
2. RESUME STEP 25
POSTCONDITION: turn 数は増えていない。システムは次の Stop hook を待っている。issue の Status は running_state の選択肢のままである。

SPECIFIC ALTERNATIVE FLOW コメントの取り戻し:
RFS BASIC FLOW 33
1. システムは herdr の pane を閉じる。
2. システムは身元ファイルからセッション UUID と設定ファイルのパスを読む。
3. システムは worktree の絶対パスとリポジトリ本体の作業ディレクトリを渡して workspace として開き直し、その中の pane を pane.list で引く。
4. システムは pane で Claude Code をセッション UUID の復帰つきで起動する。
5. システムは Claude Code に作業の内容の issue のコメントへの記録を要求する。
6. システムは issue のコメントを読み直す。
7. システムは VALIDATES THAT issue に今回の run が書いたコメントがある。
8. RESUME STEP 34
POSTCONDITION: issue にエージェントが書いたコメントが1件以上ある。turn 数は増えていない。issue の Status は表明の値の遷移先の選択肢である。

SPECIFIC ALTERNATIVE FLOW コメントの取り戻しの失敗:
RFS コメントの取り戻し 7
1. システムはボードの issue の Status に failure_state の選択肢を書く。
2. システムは herdr の pane を閉じる。
3. システムは印を外す。
4. ABORT
POSTCONDITION: issue の Status は failure_state の選択肢である。issue にエージェントが書いたコメントがない。worktree は残っている。

GLOBAL ALTERNATIVE FLOW 壊れたref:
BRANCH FROM BASIC FLOW 13
WHEN branch の ref が読めず git が worktree を作れず、まだその ref のファイルを消していない場合
1. システムは VALIDATES THAT 壊れた ref が branch_template の接頭辞で始まり refs/heads の下の通常のファイルであり中身が ref として読めない。
2. システムは壊れた ref のファイルを1つ消す。
3. システムは消したファイルのパスと消した理由を記録に残す。
4. RESUME STEP 13
POSTCONDITION: 壊れた ref のファイルは消えている。packed-refs は書き換えていない。issue の Status は running_state の選択肢のままである。

SPECIFIC ALTERNATIVE FLOW 消さないref:
RFS 壊れたref 1
1. システムはボードの issue の Status に failure_state の選択肢を書く。
2. システムは issue に worktree を用意できなかった理由を1件コメントする。
3. システムは印を外す。
4. ABORT
POSTCONDITION: ref のファイルは1バイトも消えていない。issue の Status は failure_state の選択肢である。worktree は作られていない。

GLOBAL ALTERNATIVE FLOW 権限の確認:
BRANCH FROM BASIC FLOW 24
WHEN herdr の待ち受けが blocked を返した場合
1. システムは pane に esc のキー入力を送る。
2. システムはボードの issue の Status に failure_state の選択肢を書く。
3. システムは herdr の pane を閉じる。
4. システムは印を外す。
5. ABORT
POSTCONDITION: issue の Status は failure_state の選択肢である。保留中の権限の要求は取り消されている。worktree は残っている。

GLOBAL ALTERNATIVE FLOW 送信の失敗:
BRANCH FROM BASIC FLOW 24
WHEN herdr が指示の送信そのものを断った場合
1. システムは herdr の pane を閉じる。
2. システムはリトライの回数を1つ増やす。
3. システムはバックオフの期限を印に書く。
4. ABORT
POSTCONDITION: turn の本文は Claude Code に届いていない。herdr の pane は閉じている。印は残っている。issue の Status は running_state の選択肢のままである。worktree は残っている。

GLOBAL ALTERNATIVE FLOW 無音の打ち切り:
BRANCH FROM BASIC FLOW 25
WHEN turn_timeout_ms のあいだ hook が1件も届かず、画面の版も増えない場合
1. システムは herdr に agent_status と pane の画面の版を要求する。
2. システムは herdr の pane を閉じる。
3. システムはリトライの回数を1つ増やす。
4. システムはバックオフの期限を印に書く。
5. ABORT
POSTCONDITION: herdr の pane は閉じている。印は残っている。issue の Status は running_state の選択肢のままである。worktree は残っている。

GLOBAL ALTERNATIVE FLOW 既に同じStatus:
BRANCH FROM BASIC FLOW 31
WHEN 取り直した Status が表明の値の遷移先の選択肢と同じ場合
1. システムはボードへ書き込まない。
2. RESUME STEP 33
POSTCONDITION: issue の Status は表明の値の遷移先の選択肢である。ボードへは1バイトも書いていない。

GLOBAL ALTERNATIVE FLOW 一時的な送信の失敗:
BRANCH FROM BASIC FLOW 24
WHEN herdr の呼び出しが一時的な理由で失敗した場合
1. システムは turn の本文が Claude Code に届いたかどうかを判断しない。
2. システムは turn の本文を送り直さない。
3. システムは run に turn の終わりを待ち直す印を立てる。
4. ABORT
POSTCONDITION: 印は残っている。リトライの回数は増えていない。herdr の pane は閉じていない。issue の Status は running_state の選択肢のままである。worktree は残っている。
```

## 着手の段と、落ちたときに外側へ残るもの

**Status を先に書くことが、外部に残る唯一の印である**（設計 3-16）。
**だから、着手が確定して失敗する検査はステップ11 より前に置く**（ステップ3・4・7・8）。

| 落ちた段 | 外側に残るもの | 次の巡回でどうなるか |
| --- | --- | --- |
| ステップ3 から8 | 何も残らない | issue はボードにある Status のままなので、直せばまた候補に上がる |
| ステップ9 の直後 | 何も残らない | issue は dispatch_state のままなので候補に上がる |
| ステップ11 の直後 | ボードの Status だけ | running_state は active_states に入るので候補に上がる |
| ステップ12 の直後 | Status と、動かした記録のコメント | 同上。記録が残るので、誰がいつ動かしたかは追える |
| ステップ13 から19 の途中 | Status と作りかけの worktree | worktree を再利用して着手をやり直す |
| ステップ16 の直後 | 身元ファイル | 再起動したときに身元が分かる |

## ステップ14 がリポジトリ本体も渡す理由

**`worktree.open` の `cwd` は外せない。**省くと herdr が `worktree_not_found` で断り、
worktree のパスを渡すと `linked_worktree_source` で断る（実測: 2026-08-25、
[test/live/herdr_test.go](test/live/herdr_test.go)）。

**その代わり、herdr は workspace を2つ開く。**worktree のぶんと、リポジトリ本体のぶん
（**リポジトリの親 workspace**）である。**`worktree.remove` は後者を閉じない**ので、
閉じるのは continuo の仕事になる（片付け側の条件は
[worktree と branch を片付ける.rucm.md](worktree%20と%20branch%20を片付ける.rucm.md) にある）。

**そのためステップ14 の前後で `workspace.list` を読む。**前は「この呼び出しより前から
親があったか」を見るため、後ろは「無かったなら、いま開いた親の ID」を控えるためである。
**控えた ID はステップ16 の身元ファイルへ書く**（`herdr_repo_workspace_id`）。
**前からあったなら人間が開いたものなので、控えず、二度と触らない。**

## 候補を飛ばす4つの検査

**候補の一覧は GitHub のサーバ側の検索結果であり、そのまま信じてはならない**（設計 3-34）。

| 検査 | 何を見るか | 落ちたらどうするか |
| --- | --- | --- |
| ステップ3 | issue の Status が active_states に入っているか | その issue だけ飛ばす。他の候補は続ける |
| ステップ4 | 失敗の回数が max_retries を超えていないか | 人間が Status を動かすまで拾わない |
| ステップ7 | 目的のパスに実体があるのに git の登録が無いか | Status を1バイトも書かずに飛ばす |
| ステップ8 | その branch を置き場所以外の worktree が使っていないか | Status を1バイトも書かずに飛ばす |

**ステップ8 は、目的のパスに何も無くても落ちる。**git は1つの branch を2つの worktree に
出せないので、別の場所の worktree がその branch を出していると、ステップ13 の
`git worktree add` が `fatal: '<branch>' is already used by worktree at '<別のパス>'` で
必ず失敗する。**片付けは `continuo abandon <issue の URL>` の出番である**
（[着手を取り消す.rucm.md](%E7%9D%80%E6%89%8B%E3%82%92%E5%8F%96%E3%82%8A%E6%B6%88%E3%81%99.rucm.md)）。

## 壊れた ref に出会ったら、その1ファイルを消してやり直す

**言いたいこと。**`refs/heads/<branch>` のファイルが読めない状態になると、
ステップ13 は何度やり直しても `reference broken` で失敗し、その issue には二度と着手できない。
**git のコマンドでは消せないので、continuo がファイルとして1つ消して、1回だけやり直す。**

**消してよい条件は設計 [3-22b](../../../plans/continuo_design.md) にある7つで、全部を満たすときだけ消す。**
とくに `herdr.worktree.branch_template` から作った接頭辞（既定は `continuo/`）で始まる名前だけを
対象にし、`git show-ref --verify` が通る正常な branch には触らない。
**中身が SHA や `ref: ` として読めるなら消さない。**読めるものを消せば、その情報が失われる。
**途中のシンボリックリンクを解決したうえで** `refs/heads` の内側に収まっていることを確かめる。
**packed-refs は1バイトも触らない。**

**やり直しは1回だけである。**2回目も失敗したら、そのままの失敗として `failure_state` へ落とす。

**packed-refs 側の ref が生き返ることがある。**その branch が packed-refs にも載っていると、
loose を消した瞬間に packed 側が有効になり、**やり直しはその（古いかもしれない）commit の
チェックアウトになる。**どちらだったのかを記録に残す。

**別の branch 名へ逃げる案は採れない。**置き場所も branch 名も issue 番号から決まる（設計 3-22）ので、
名前を変えると片付け・復元・`continuo abandon` がその issue の worktree を引けなくなる。

## turn の終わりの判定

| Stop hook の `background_tasks` | どう扱うか |
| --- | --- |
| 空でない | まだ動いている。turn の終わりとして扱わない |
| 項目が欠けている | 判定できない。turn の終わりとみなさない |
| 空配列 | settle_ms のあいだ待ち、task-notification が届かなければ turn の終わりとする |

## turn を送れなかったときは、2つに分ける

**言いたいこと。**herdr へ送れなかったことと、Stop hook が届かなかったことは別である。
**混ぜると、1文字も届いていないのに「agent が待機状態になったと答えた」と issue に残る。**

| 何が起きたか | どう扱うか | issue と印はどうなるか |
| --- | --- | --- |
| herdr の呼び出しが**一時的な理由**で失敗した（再起動・socket の一瞬の不通・応答の遅れ） | run を諦めない。turn の終わりを待ち直す印を立てて抜ける | 印は残る。リトライは増えない |
| herdr が**送信そのものを断った**（pane が消えている・agent が受け取れない） | run を手放す。届いていないことを明記した理由を残す | 印は残る。リトライを1つ積む |
| 待ち受けが返ったのに **Stop hook が来ない** | 打ち切りの判定へ回す | 巡回の停滞の検知が決める |

**一時的な失敗でも `agent.prompt` を送り直さない。**届いていたかどうかは分からず、
届いていた場合に送り直すと turn が二重に投入される。**黙って止まりもしない。**
画面が動かないままなら、巡回の停滞の検知が `claude.turn_timeout_ms` の沈黙で拾う。

## フローチャート

```mermaid
flowchart TD
    B1["1. 巡回タイマーが巡回の開始を要求する"]
    B2["2. ボードから active_states の issue の一覧を取る"]
    B3{"3. VALIDATES THAT Status が active_states に入っている"}
    B4{"4. VALIDATES THAT 失敗の回数が max_retries を超えていない"}
    B5{"5. VALIDATES THAT 空きスロットが1つ以上ある"}
    B6{"6. VALIDATES THAT 対象リポジトリが信頼登録されている"}
    B7{"7. VALIDATES THAT worktree の置き場所をそのまま使える"}
    B8{"8. VALIDATES THAT branch を置き場所以外の worktree が使っていない"}
    B9["9. 先頭の issue に印を付ける"]
    B10{"10. VALIDATES THAT 取り直した Status が active_states に入っている"}
    B11["11. Status に running_state を書く"]
    B12["12. Status を動かした記録を issue にコメントする"]
    B13["13. worktree を作る"]
    B14["14. worktree の絶対パスとリポジトリ本体を渡して開き label に owner/repo/issues/N を書く"]
    B15["15. 設定ファイルを worktree の外に書く"]
    B16["16. 身元ファイルを書く"]
    B17["17. workspace の pane の一覧を要求する"]
    B18["18. pane の label に owner/repo/issues/N を書く"]
    B19{"19. VALIDATES THAT pane が起動を受け付ける"}
    B20["20. pane で Claude Code を起動する"]
    B21{"21. VALIDATES THAT agent_status が idle か done で interactive_ready が真"}
    B22["22. DO"]
    B23{"23. VALIDATES THAT turn 数が max_dispatch_turns に達していない"}
    B24["24. turn の本文を送る"]
    B25["25. Stop hook を受ける"]
    B26["26. settle_ms のあいだ待つ"]
    B27{"27. VALIDATES THAT task-notification が届かない"}
    B28["28. transcript から表明の行を読む"]
    B29["29. Status を ID 指定で取り直す"]
    B30{"30. UNTIL 表明の値が working でない"}
    B31["31. Status に表明の遷移先を書く"]
    B32["32. Status を動かした記録を issue にコメントする"]
    B33{"33. VALIDATES THAT 今回の run のコメントがある"}
    B34["34. workspace_hooks の after_run を実行する"]
    B35["35. herdr の pane を閉じる"]
    B36["36. 印を外す"]
    BPOST(["POSTCONDITION 表明どおりに Status が動き worker が止まっている"])

    B1 --> B2 --> B3
    B3 -- 偽 --> F12S1
    B3 -- 真 --> B4
    B4 -- 偽 --> F13S1
    B4 -- 真 --> B5
    B5 -- 偽 --> F1S1
    B5 -- 真 --> B6
    B6 -- 偽 --> F2S1
    B6 -- 真 --> B7
    B7 -- 偽 --> F14S1
    B7 -- 真 --> B8
    B8 -- 偽 --> F16S1
    B8 -- 真 --> B9 --> B10
    B10 -- 偽 --> F15S1
    B10 -- 真 --> B11 --> B12 --> B13 --> B14 --> B15 --> B16 --> B17 --> B18 --> B19
    B19 -- 偽 --> F8S1
    B19 -- 真 --> B20 --> B21
    B21 -- 偽 --> F3S1
    B21 -- 真 --> B22 --> B23
    B23 -- 偽 --> F4S1
    B23 -- 真 --> B24 --> B25 --> B26 --> B27
    B27 -- 偽 --> F5S1
    B27 -- 真 --> B28 --> B29 --> B30
    B30 -- 偽 --> B23
    B30 -- 真 --> B31 --> B32 --> B33
    B33 -- 偽 --> F6S1
    B33 -- 真 --> B34 --> B35 --> B36 --> BPOST
    B13 -. "壊れたref: WHEN ref が読めず worktree を作れない場合" .-> G3S1
    B24 -. "権限の確認: WHEN blocked が返った場合" .-> G1S1
    B24 -. "送信の失敗: WHEN herdr が送信そのものを断った場合" .-> G4S1
    B24 -. "一時的な送信の失敗: WHEN herdr の呼び出しが一時的な理由で失敗した場合" .-> G5S1
    B25 -. "無音の打ち切り: WHEN hook も画面の版も動かない場合" .-> G2S1
    B31 -. "既に同じStatus: WHEN 取り直した Status が遷移先と同じ場合" .-> G6S1

    subgraph SAF12 ["SPECIFIC ALTERNATIVE FLOW 頼んでいないStatus / RFS BASIC FLOW 3"]
        F12S1["1. この issue を dispatch の対象から外す"] --> F12S2["2. 頼んだ Status に無い候補が返ったことを記録に残す"] --> F12S3["3. ABORT"]
    end

    subgraph SAF13 ["SPECIFIC ALTERNATIVE FLOW 失敗の繰り返し / RFS BASIC FLOW 4"]
        F13S1["1. この issue を dispatch の対象から外す"] --> F13S2["2. ABORT"]
    end

    subgraph SAF1 ["SPECIFIC ALTERNATIVE FLOW 空きスロット不足 / RFS BASIC FLOW 5"]
        F1S1["1. この巡回で1件も dispatch しない"] --> F1S2["2. ABORT"]
    end

    subgraph SAF2 ["SPECIFIC ALTERNATIVE FLOW 未信頼のリポジトリ / RFS BASIC FLOW 6"]
        F2S1["1. dispatch の対象から外す"] --> F2S2["2. 承認を促すコメントを1件書く"] --> F2S3["3. 通知済みとして記録する"] --> F2S4["4. ABORT"]
    end

    subgraph SAF14 ["SPECIFIC ALTERNATIVE FLOW 使えないworktree / RFS BASIC FLOW 7"]
        F14S1["1. この issue を dispatch の対象から外す"] --> F14S2["2. 使えない理由を記録に残す"] --> F14S3["3. ABORT"]
    end

    subgraph SAF16 ["SPECIFIC ALTERNATIVE FLOW 使われているbranch / RFS BASIC FLOW 8"]
        F16S1["1. この issue を dispatch の対象から外す"] --> F16S2["2. branch を使っている worktree の場所と片付けの手順を記録に残す"] --> F16S3["3. ABORT"]
    end

    subgraph SAF15 ["SPECIFIC ALTERNATIVE FLOW 書かずに取りやめる / RFS BASIC FLOW 10"]
        F15S1["1. 印を外す"] --> F15S2["2. ABORT"]
    end

    subgraph SAF8 ["SPECIFIC ALTERNATIVE FLOW paneがまだ使えない / RFS BASIC FLOW 19"]
        F8S1["1. 500 ミリ秒待つ"] --> F8S2{"2. VALIDATES THAT 30 秒が経っていない"}
        F8S2 -- 真 --> F8S3["3. RESUME STEP 20"]
    end

    subgraph SAF9 ["SPECIFIC ALTERNATIVE FLOW paneの断念 / RFS paneがまだ使えない 2"]
        F9S1["1. Status に failure_state を書く"] --> F9S2["2. pane が使えなかった理由をコメントする"] --> F9S3["3. pane を閉じる"] --> F9S4["4. 印を外す"] --> F9S5["5. ABORT"]
    end

    subgraph SAF3 ["SPECIFIC ALTERNATIVE FLOW 起動直後の確認画面 / RFS BASIC FLOW 21"]
        F3S1["1. pane に esc を送る"] --> F3S2["2. Status に failure_state を書く"] --> F3S3["3. pane を閉じる"] --> F3S4["4. 印を外す"] --> F3S5["5. ABORT"]
    end

    subgraph SAF10 ["SPECIFIC ALTERNATIVE FLOW 起動の待ち直し / RFS BASIC FLOW 21"]
        F10S1["1. 500 ミリ秒待つ"] --> F10S2{"2. VALIDATES THAT startup_timeout_ms が経っていない"}
        F10S2 -- 真 --> F10S3["3. もう一度 Claude Code を起動する"] --> F10S4["4. RESUME STEP 22"]
    end

    subgraph SAF11 ["SPECIFIC ALTERNATIVE FLOW 起動の断念 / RFS 起動の待ち直し 2"]
        F11S1["1. max_retries までバックオフして着手をやり直す"] --> F11S2["2. Status に failure_state を書く"] --> F11S3["3. 起動できなかった理由をコメントする"] --> F11S4["4. pane を閉じる"] --> F11S5["5. 印を外す"] --> F11S6["6. ABORT"]
    end

    subgraph SAF4 ["SPECIFIC ALTERNATIVE FLOW 上限での打ち切り / RFS BASIC FLOW 23"]
        F4S1["1. Status に failure_state を書く"] --> F4S2["2. 打ち切りの理由をコメントする"] --> F4S3["3. pane を閉じる"] --> F4S4["4. 印を外す"] --> F4S5["5. ABORT"]
    end

    subgraph SAF5 ["SPECIFIC ALTERNATIVE FLOW turnの継続 / RFS BASIC FLOW 27"]
        F5S1["1. turn がまだ続いているとみなす"] --> F5S2["2. RESUME STEP 26"]
    end

    subgraph SAF6 ["SPECIFIC ALTERNATIVE FLOW コメントの取り戻し / RFS BASIC FLOW 33"]
        F6S1["1. pane を閉じる"] --> F6S2["2. セッション UUID と設定ファイルのパスを読む"] --> F6S3["3. worktree とリポジトリ本体を渡して開き直し pane を引く"] --> F6S4["4. セッションの復帰つきで起動する"] --> F6S5["5. コメントへの記録を要求する"] --> F6S6["6. コメントを読み直す"] --> F6S7{"7. VALIDATES THAT コメントがある"}
        F6S7 -- 真 --> F6S8["8. RESUME STEP 35"]
    end

    subgraph SAF7 ["SPECIFIC ALTERNATIVE FLOW コメントの取り戻しの失敗 / RFS コメントの取り戻し 7"]
        F7S1["1. Status に failure_state を書く"] --> F7S2["2. pane を閉じる"] --> F7S3["3. 印を外す"] --> F7S4["4. ABORT"]
    end

    subgraph GAF3 ["GLOBAL ALTERNATIVE FLOW 壊れたref / BRANCH FROM BASIC FLOW 13"]
        G3S1{"1. VALIDATES THAT continuo の接頭辞で始まる refs/heads の下の通常のファイルで中身が読めない"}
        G3S1 -- 真 --> G3S2["2. 壊れた ref のファイルを1つ消す"] --> G3S3["3. 消したパスと理由を記録に残す"] --> G3S4["4. RESUME STEP 13"]
    end

    subgraph SAF17 ["SPECIFIC ALTERNATIVE FLOW 消さないref / RFS 壊れたref 1"]
        F17S1["1. Status に failure_state を書く"] --> F17S2["2. 用意できなかった理由をコメントする"] --> F17S3["3. 印を外す"] --> F17S4["4. ABORT"]
    end

    subgraph GAF1 ["GLOBAL ALTERNATIVE FLOW 権限の確認 / BRANCH FROM BASIC FLOW 24"]
        G1S1["1. pane に esc を送る"] --> G1S2["2. Status に failure_state を書く"] --> G1S3["3. pane を閉じる"] --> G1S4["4. 印を外す"] --> G1S5["5. ABORT"]
    end

    subgraph GAF4 ["GLOBAL ALTERNATIVE FLOW 送信の失敗 / BRANCH FROM BASIC FLOW 24"]
        G4S1["1. pane を閉じる"] --> G4S2["2. リトライの回数を1つ増やす"] --> G4S3["3. バックオフの期限を印に書く"] --> G4S4["4. ABORT"]
    end

    subgraph GAF2 ["GLOBAL ALTERNATIVE FLOW 無音の打ち切り / BRANCH FROM BASIC FLOW 25"]
        G2S1["1. agent_status と画面の版を要求する"] --> G2S2["2. pane を閉じる"] --> G2S3["3. リトライの回数を1つ増やす"] --> G2S4["4. バックオフの期限を印に書く"] --> G2S5["5. ABORT"]
    end

    subgraph GAF5 ["GLOBAL ALTERNATIVE FLOW 一時的な送信の失敗 / BRANCH FROM BASIC FLOW 24"]
        G5S1["1. 本文が届いたかどうかを判断しない"] --> G5S2["2. 本文を送り直さない"] --> G5S3["3. turn の終わりを待ち直す印を立てる"] --> G5S4["4. ABORT"]
    end

    subgraph GAF6 ["GLOBAL ALTERNATIVE FLOW 既に同じStatus / BRANCH FROM BASIC FLOW 31"]
        G6S1["1. ボードへ書き込まない"] --> G6S2["2. RESUME STEP 33"]
    end

    F8S2 -- 偽 --> F9S1
    F8S3 --> B19
    F10S2 -- 偽 --> F11S1
    F10S4 --> B21
    F5S2 --> B25
    F6S7 -- 偽 --> F7S1
    F6S8 --> B34
    G3S1 -- 偽 --> F17S1
    G3S4 --> B13
    G6S2 --> B33
```

## シーケンス図

```mermaid
sequenceDiagram
    actor T as 巡回タイマー
    participant S as システム
    participant GH as GitHub Projects v2
    participant H as herdr
    participant CC as Claude Code

    T->>S: 巡回の開始を要求する
    S->>GH: active_states の issue の一覧を要求する
    GH-->>S: 候補を並び順で応答する
    S->>S: 候補の Status が active_states に入っていることを検証する
    S->>S: 失敗の回数が max_retries を超えていないことを検証する
    alt 頼んだ Status に無い、または失敗の回数を使い切っている
        Note over S: ABORT この issue だけ飛ばし、他の候補は続ける
    else 着手してよい候補である
        S->>S: 空きスロットが1つ以上あることを検証する
        alt 空きスロットがない
            Note over S: ABORT この巡回では1件も dispatch しない
        else 空きスロットがある
            S->>S: 対象リポジトリの信頼登録を検証する
            S->>S: worktree の置き場所をそのまま使えることを検証する
            S->>S: branch を置き場所以外の worktree が使っていないことを検証する
            alt 信頼登録が無い、置き場所を使えない、または branch が使われている
                S->>GH: 承認を促すコメントの投稿を要求する
                Note over S: ABORT ボードへは1バイトも書かない
            else 着手できる
                S->>S: 先頭の issue に印を付ける
                S->>GH: Status への running_state の書き込みを要求する
                GH-->>S: 書き込んだかどうかを応答する
                alt 書かなかった
                    S->>S: 印を外す
                    Note over S: ABORT worktree は作らない
                else 書いた
                    S->>GH: 何から何へ動かしたかのコメントの投稿を要求する
                    alt branch の ref が読めず worktree を作れない
                        S->>S: 壊れた ref のファイルを1つ消して worktree の作成を1回だけやり直す
                    end
                    S->>S: worktree を作り設定ファイルと身元ファイルを書く
                    S->>H: worktree の workspace としての open と label の書き込みを要求する
                    H-->>S: workspace と pane を応答する
                    S->>H: pane の label への owner/repo/issues/N の書き込みを要求する
                    S->>H: pane での Claude Code の起動を要求する
                    H->>CC: Claude Code を起動する
                    H-->>S: agent_status を応答する
                    S->>S: agent_status が idle または done であることを検証する
                    loop 表明の値が working でなくなるまで
                        S->>S: turn 数が max_dispatch_turns に達していないことを検証する
                        S->>CC: turn の本文を送る
                        alt herdr が送信そのものを断る
                            S->>H: pane の close を要求する
                            Note over S: ABORT 本文は届いていない。リトライを1つ積む
                        end
                        CC-->>S: Stop hook を届ける
                        S->>S: settle_ms のあいだ待つ
                        alt task-notification が届く
                            Note over S: RESUME STEP 25 turn は続いている
                        else task-notification が届かない
                            S->>S: transcript から表明の行を読む
                            S->>GH: Status の取り直しを要求する
                            GH-->>S: 現在の Status を応答する
                        end
                    end
                    alt 取り直した Status が既に表明の遷移先と同じ
                        Note over S: 書き込みを送らない。記録のコメントも書かない
                    else 取り直した Status が表明の遷移先と違う
                        S->>GH: Status への表明の遷移先の書き込みを要求する
                        S->>GH: 何から何へ動かしたかのコメントの投稿を要求する
                    end
                    S->>GH: issue のコメントの取得を要求する
                    GH-->>S: コメントの一覧を応答する
                    alt 今回の run のコメントがない
                        S->>H: pane の close を要求する
                        S->>H: worktree とリポジトリ本体を渡した workspace の open を要求する
                        H-->>S: workspace と pane を応答する
                        S->>H: セッションの復帰つきの起動を要求する
                        H->>CC: セッションを復帰する
                        S->>CC: 作業の内容のコメントへの記録を要求する
                        CC->>GH: issue にコメントを書く
                    end
                    S->>S: workspace_hooks の after_run を実行する
                    S->>H: pane の close を要求する
                    S->>S: 印を外す
                end
            end
        end
    end
```
