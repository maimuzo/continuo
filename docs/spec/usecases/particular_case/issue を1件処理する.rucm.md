<!-- 目的: continuo が issue を1件取り、worker を立て、turn ループを回して表明どおりに Status を動かすまでを RUCM で定義する -->

# ユースケース: issue を1件処理する

## 根拠資料

- `docs/plans/continuo_design.md#3-2`（turn の終わりの判定の規則。settle_ms と task-notification）
- `docs/plans/continuo_design.md#3-5`（完了検知の3層と、1つの turn で何が起きるか）
- `docs/plans/continuo_design.md#3-6`（dispatch の直前に issue ごとに検査するもの）
- `docs/plans/continuo_design.md#3-8`（turn ループ。1回目の本文と継続の指示、max_turns）
- `docs/plans/continuo_design.md#3-16`（着手の手順の順番。段-1 から段11）
- `docs/plans/continuo_design.md#3-18`（worktree の身元ファイル）
- `docs/plans/continuo_design.md#3-21`（打ち切りは「画面の版」で測る）
- `docs/plans/continuo_design.md#3-25`（表明を transcript から読む。コメントが無かったらセッションを復元して書かせる）
- `docs/plans/continuo_design.md#4-1`（誰がどの遷移を起こすか）
- `internal/orchestrator/dispatch.go` の `dispatchCandidates`、`claimForDispatch`、`preflight`、`startRun`、`confirmStartup`
- `internal/orchestrator/turn.go` の `turnLoop`、`sendTurn`、`confirmTurnEnd`
- `internal/orchestrator/lifecycle.go` の `handleTurnEnd`、`readSignals`、`applySignals`、`finishRun`
- `internal/orchestrator/comment.go` の `ensureAgentComment`
- `internal/orchestrator/signal.go` の `ParseSignals`

## RUCM

```rucm
USE CASE NAME: issue を1件処理する
BRIEF DESCRIPTION: 巡回タイマーが巡回を起こす。システムはボードから候補を取り、先頭の1件に印を付けて worktree と worker を用意する。システムは turn を送り、Stop hook で turn の終わりを判定し、transcript の表明を読む。システムは表明の値どおりにボードの Status を書き、worker を止める。
PRECONDITION: システムは常駐している。システムはロックファイルの flock を取っている。ボードの Status の選択肢名は設定と一致する。ボードの dispatch_state の Status に issue が1件以上ある。herdr は待ち受けている。
PRIMARY ACTOR: 巡回タイマー
SECONDARY ACTORS: GitHub Projects v2、herdr、Claude Code
DEPENDENCY: なし
GENERALIZATION: なし

BASIC FLOW:
1. 巡回タイマーはシステムに巡回の開始を要求する。
2. システムはボードから active_states の issue の一覧を取る。
3. システムは VALIDATES THAT 空きスロットが1つ以上ある。
4. システムは VALIDATES THAT 先頭の issue の対象リポジトリが Claude Code に信頼登録されている。
5. システムは先頭の issue に印を付ける。
6. システムはボードの issue の Status に running_state の選択肢を書く。
7. システムは workspace.root の下に issue の worktree を作る。
8. システムは worktree の絶対パスを渡して、worktree の絶対パスを渡して workspace として開く。
9. システムは Claude Code の設定ファイルを worktree の外に書く。
10. システムは worktree の中に身元ファイルを書く。
11. システムは herdr に workspace の pane の一覧を要求する。
12. システムは pane の label に issue の URL を書く。
13. システムは pane で Claude Code を起動する。
14. システムは VALIDATES THAT Claude Code の agent_status が idle または done である。
15. DO
16.   システムは VALIDATES THAT turn 数が max_turns に達していない。
17.   システムは Claude Code に turn の本文を送る。
18.   システムは Claude Code の Stop hook を受ける。
19.   システムは settle_ms のあいだ待つ。
20.   システムは VALIDATES THAT settle_ms のあいだに task-notification で始まる UserPromptSubmit が届かない。
21.   システムは transcript から表明の行を読む。
22.   システムはボードの issue の Status を ID 指定で取り直す。
23. UNTIL 表明の値が working でない
24. システムはボードの issue の Status に表明の値の遷移先の選択肢を書く。
25. システムは VALIDATES THAT issue に今回の run が書いたコメントがある。
26. システムは workspace_hooks の after_run を実行する。
27. システムは herdr の pane を閉じる。
28. システムは印を外す。
POSTCONDITION: issue の Status は表明の値の遷移先の選択肢である。issue にエージェントが書いたコメントが1件以上ある。herdr の pane は閉じている。印は外れている。worktree と branch は残っている。

SPECIFIC ALTERNATIVE FLOW 空きスロット不足:
RFS BASIC FLOW 3
1. システムはこの巡回で issue を1件も dispatch しない。
2. ABORT
POSTCONDITION: 印の件数は変わっていない。issue の Status は dispatch_state の選択肢のままである。worktree は作られていない。

SPECIFIC ALTERNATIVE FLOW 未信頼のリポジトリ:
RFS BASIC FLOW 4
1. システムは issue を dispatch の対象から外す。
2. システムは issue に信頼登録の承認を促すコメントを1件書く。
3. システムは対象リポジトリを通知済みとして記録する。
4. ABORT
POSTCONDITION: issue の Status は dispatch_state の選択肢のままである。worktree は作られていない。issue に信頼登録の承認を促すコメントが1件ある。

SPECIFIC ALTERNATIVE FLOW 起動直後の確認画面:
RFS BASIC FLOW 14
1. システムは pane に esc のキー入力を送る。
2. システムはボードの issue の Status に failure_state の選択肢を書く。
3. システムは herdr の pane を閉じる。
4. システムは印を外す。
5. ABORT
POSTCONDITION: issue の Status は failure_state の選択肢である。turn の本文は Claude Code に届いていない。worktree は残っている。

SPECIFIC ALTERNATIVE FLOW 上限での打ち切り:
RFS BASIC FLOW 16
1. システムはボードの issue の Status に failure_state の選択肢を書く。
2. システムは issue に打ち切りの理由を1件コメントする。
3. システムは herdr の pane を閉じる。
4. システムは印を外す。
5. ABORT
POSTCONDITION: issue の Status は failure_state の選択肢である。turn 数は max_turns と等しい。worktree は残っている。issue に打ち切りの理由のコメントが1件ある。

SPECIFIC ALTERNATIVE FLOW turnの継続:
RFS BASIC FLOW 20
1. システムは turn がまだ続いているとみなす。
2. RESUME STEP 18
POSTCONDITION: turn 数は増えていない。システムは次の Stop hook を待っている。issue の Status は running_state の選択肢のままである。

SPECIFIC ALTERNATIVE FLOW コメントの取り戻し:
RFS BASIC FLOW 25
1. システムは herdr の pane を閉じる。
2. システムは身元ファイルからセッション UUID と設定ファイルのパスを読む。
3. システムは pane で Claude Code をセッション UUID の復帰つきで起動する。
4. システムは Claude Code に作業の内容の issue のコメントへの記録を要求する。
5. システムは issue のコメントを読み直す。
6. システムは VALIDATES THAT issue に今回の run が書いたコメントがある。
7. RESUME STEP 26
POSTCONDITION: issue にエージェントが書いたコメントが1件以上ある。turn 数は増えていない。issue の Status は表明の値の遷移先の選択肢である。

SPECIFIC ALTERNATIVE FLOW コメントの取り戻しの失敗:
RFS コメントの取り戻し 6
1. システムはボードの issue の Status に failure_state の選択肢を書く。
2. システムは herdr の pane を閉じる。
3. システムは印を外す。
4. ABORT
POSTCONDITION: issue の Status は failure_state の選択肢である。issue にエージェントが書いたコメントがない。worktree は残っている。

GLOBAL ALTERNATIVE FLOW 権限の確認:
BRANCH FROM BASIC FLOW 17
WHEN herdr の待ち受けが blocked を返した場合
1. システムは pane に esc のキー入力を送る。
2. システムはボードの issue の Status に failure_state の選択肢を書く。
3. システムは herdr の pane を閉じる。
4. システムは印を外す。
5. ABORT
POSTCONDITION: issue の Status は failure_state の選択肢である。保留中の権限の要求は取り消されている。worktree は残っている。

GLOBAL ALTERNATIVE FLOW 無音の打ち切り:
BRANCH FROM BASIC FLOW 18
WHEN turn_timeout_ms のあいだ hook が1件も届かず、画面の版も増えない場合
1. システムは herdr に agent_status と pane の画面の版を要求する。
2. システムは herdr の pane を閉じる。
3. システムはリトライの回数を1つ増やす。
4. システムはバックオフの期限を印に書く。
5. ABORT
POSTCONDITION: herdr の pane は閉じている。印は残っている。issue の Status は running_state の選択肢のままである。worktree は残っている。
```

## 着手の段と、落ちたときに外側へ残るもの

**Status を先に書くことが、外部に残る唯一の印である**（設計 3-16）。

| 落ちた段 | 外側に残るもの | 次の巡回でどうなるか |
| --- | --- | --- |
| ステップ5 の直後 | 何も残らない | issue は dispatch_state のままなので候補に上がる |
| ステップ6 の直後 | ボードの Status だけ | running_state は active_states に入るので候補に上がる |
| ステップ7 から13 の途中 | Status と作りかけの worktree | worktree を再利用して着手をやり直す |
| ステップ10 の直後 | 身元ファイル | 再起動したときに身元が分かる |

## turn の終わりの判定

| Stop hook の `background_tasks` | どう扱うか |
| --- | --- |
| 空でない | まだ動いている。turn の終わりとして扱わない |
| 項目が欠けている | 判定できない。turn の終わりとみなさない |
| 空配列 | settle_ms のあいだ待ち、task-notification が届かなければ turn の終わりとする |

## フローチャート

```mermaid
flowchart TD
    B1["1. 巡回タイマーが巡回の開始を要求する"]
    B2["2. ボードから active_states の issue の一覧を取る"]
    B3{"3. VALIDATES THAT 空きスロットが1つ以上ある"}
    B4{"4. VALIDATES THAT 対象リポジトリが信頼登録されている"}
    B5["5. 先頭の issue に印を付ける"]
    B6["6. Status に running_state を書く"]
    B7["7. worktree を作る"]
    B8["8. worktree の絶対パスを渡して herdr の workspace として開く"]
    B9["9. 設定ファイルを worktree の外に書く"]
    B10["10. 身元ファイルを書く"]
    B11["11. workspace の pane の一覧を要求する"]
    B12["12. pane の label に issue の URL を書く"]
    B13["13. pane で Claude Code を起動する"]
    B14{"14. VALIDATES THAT agent_status が idle または done"}
    B15["15. DO"]
    B16{"16. VALIDATES THAT turn 数が max_turns に達していない"}
    B17["17. turn の本文を送る"]
    B18["18. Stop hook を受ける"]
    B19["19. settle_ms のあいだ待つ"]
    B20{"20. VALIDATES THAT task-notification が届かない"}
    B21["21. transcript から表明の行を読む"]
    B22["22. Status を ID 指定で取り直す"]
    B23{"23. UNTIL 表明の値が working でない"}
    B24["24. Status に表明の遷移先を書く"]
    B25{"25. VALIDATES THAT 今回の run のコメントがある"}
    B26["26. workspace_hooks の after_run を実行する"]
    B27["27. herdr の pane を閉じる"]
    B28["28. 印を外す"]
    BPOST(["POSTCONDITION 表明どおりに Status が動き worker が止まっている"])

    B1 --> B2 --> B3
    B3 -- 偽 --> F1S1
    B3 -- 真 --> B4
    B4 -- 偽 --> F2S1
    B4 -- 真 --> B5 --> B6 --> B7 --> B8 --> B9 --> B10 --> B11 --> B12 --> B13 --> B14
    B14 -- 偽 --> F3S1
    B14 -- 真 --> B15 --> B16
    B16 -- 偽 --> F4S1
    B16 -- 真 --> B17 --> B18 --> B19 --> B20
    B20 -- 偽 --> F5S1
    B20 -- 真 --> B21 --> B22 --> B23
    B23 -- 偽 --> B16
    B23 -- 真 --> B24 --> B25
    B25 -- 偽 --> F6S1
    B25 -- 真 --> B26 --> B27 --> B28 --> BPOST
    B17 -. "権限の確認: WHEN blocked が返った場合" .-> G1S1
    B18 -. "無音の打ち切り: WHEN hook も画面の版も動かない場合" .-> G2S1

    subgraph SAF1 ["SPECIFIC ALTERNATIVE FLOW 空きスロット不足 / RFS BASIC FLOW 3"]
        F1S1["1. この巡回で1件も dispatch しない"] --> F1S2["2. ABORT"]
    end

    subgraph SAF2 ["SPECIFIC ALTERNATIVE FLOW 未信頼のリポジトリ / RFS BASIC FLOW 4"]
        F2S1["1. dispatch の対象から外す"] --> F2S2["2. 承認を促すコメントを1件書く"] --> F2S3["3. 通知済みとして記録する"] --> F2S4["4. ABORT"]
    end

    subgraph SAF3 ["SPECIFIC ALTERNATIVE FLOW 起動直後の確認画面 / RFS BASIC FLOW 14"]
        F3S1["1. pane に esc を送る"] --> F3S2["2. Status に failure_state を書く"] --> F3S3["3. pane を閉じる"] --> F3S4["4. 印を外す"] --> F3S5["5. ABORT"]
    end

    subgraph SAF4 ["SPECIFIC ALTERNATIVE FLOW 上限での打ち切り / RFS BASIC FLOW 16"]
        F4S1["1. Status に failure_state を書く"] --> F4S2["2. 打ち切りの理由をコメントする"] --> F4S3["3. pane を閉じる"] --> F4S4["4. 印を外す"] --> F4S5["5. ABORT"]
    end

    subgraph SAF5 ["SPECIFIC ALTERNATIVE FLOW turnの継続 / RFS BASIC FLOW 20"]
        F5S1["1. turn がまだ続いているとみなす"] --> F5S2["2. RESUME STEP 18"]
    end

    subgraph SAF6 ["SPECIFIC ALTERNATIVE FLOW コメントの取り戻し / RFS BASIC FLOW 25"]
        F6S1["1. pane を閉じる"] --> F6S2["2. セッション UUID と設定ファイルのパスを読む"] --> F6S3["3. セッションの復帰つきで起動する"] --> F6S4["4. コメントへの記録を要求する"] --> F6S5["5. コメントを読み直す"] --> F6S6{"6. VALIDATES THAT コメントがある"}
        F6S6 -- 真 --> F6S7["7. RESUME STEP 26"]
    end

    subgraph SAF7 ["SPECIFIC ALTERNATIVE FLOW コメントの取り戻しの失敗 / RFS コメントの取り戻し 6"]
        F7S1["1. Status に failure_state を書く"] --> F7S2["2. pane を閉じる"] --> F7S3["3. 印を外す"] --> F7S4["4. ABORT"]
    end

    subgraph GAF1 ["GLOBAL ALTERNATIVE FLOW 権限の確認 / BRANCH FROM BASIC FLOW 17"]
        G1S1["1. pane に esc を送る"] --> G1S2["2. Status に failure_state を書く"] --> G1S3["3. pane を閉じる"] --> G1S4["4. 印を外す"] --> G1S5["5. ABORT"]
    end

    subgraph GAF2 ["GLOBAL ALTERNATIVE FLOW 無音の打ち切り / BRANCH FROM BASIC FLOW 18"]
        G2S1["1. agent_status と画面の版を要求する"] --> G2S2["2. pane を閉じる"] --> G2S3["3. リトライの回数を1つ増やす"] --> G2S4["4. バックオフの期限を印に書く"] --> G2S5["5. ABORT"]
    end

    F5S2 --> B18
    F6S6 -- 偽 --> F7S1
    F6S7 --> B26
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
    S->>S: 空きスロットが1つ以上あることを検証する
    alt 空きスロットがない
        Note over S: ABORT この巡回では1件も dispatch しない
    else 空きスロットがある
        S->>S: 対象リポジトリの信頼登録を検証する
        alt 信頼登録が無い
            S->>GH: 承認を促すコメントの投稿を要求する
            Note over S: ABORT worktree は作らない
        else 信頼登録がある
            S->>S: 先頭の issue に印を付ける
            S->>GH: Status への running_state の書き込みを要求する
            S->>S: worktree を作り設定ファイルと身元ファイルを書く
            S->>H: worktree の workspace としての open を要求する
            H-->>S: workspace と pane を応答する
            S->>H: pane の label への issue の URL の書き込みを要求する
            S->>H: pane での Claude Code の起動を要求する
            H->>CC: Claude Code を起動する
            H-->>S: agent_status を応答する
            S->>S: agent_status が idle または done であることを検証する
            loop 表明の値が working でなくなるまで
                S->>S: turn 数が max_turns に達していないことを検証する
                S->>CC: turn の本文を送る
                CC-->>S: Stop hook を届ける
                S->>S: settle_ms のあいだ待つ
                alt task-notification が届く
                    Note over S: RESUME STEP 18 turn は続いている
                else task-notification が届かない
                    S->>S: transcript から表明の行を読む
                    S->>GH: Status の取り直しを要求する
                    GH-->>S: 現在の Status を応答する
                end
            end
            S->>GH: Status への表明の遷移先の書き込みを要求する
            S->>GH: issue のコメントの取得を要求する
            GH-->>S: コメントの一覧を応答する
            alt 今回の run のコメントがない
                S->>H: pane の close を要求する
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
```
