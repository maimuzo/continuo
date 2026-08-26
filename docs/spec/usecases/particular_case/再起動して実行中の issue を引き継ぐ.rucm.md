# ユースケース: 再起動して実行中の issue を引き継ぐ

## 根拠資料

- `docs/plans/continuo_design.md#3-3`（run を指す識別子。セッション UUID は `--resume` で戻す）
- `docs/plans/continuo_design.md#3-4`（状態は in-memory。再起動時の復元手順の段1 から段9）
- `docs/plans/continuo_design.md#3-6`（起動時の検査。落ちても pane を閉じない）
- `docs/plans/continuo_design.md#3-17`（二重起動は flock で防ぐ）
- `docs/plans/continuo_design.md#3-18`（身元ファイルと引き継いだ回数）
- `docs/plans/continuo_design.md#3-19`（落ちている間に届かなかった通知を取り戻す）
- `docs/plans/continuo_design.md#8-1`（再起動後は引き渡し状態の worker を止めない）
- `internal/orchestrator/restore.go` の `Restore`、`scanIdentities`、`refetchByIdentities`、`matchPanes`、`decideOne`、`restoreWithoutPane`、`applyOrphanRunningAction`
- `internal/orchestrator/turn.go` の `startTurnLoop`
- `internal/workspace/scan.go` と `internal/workspace/identity.go`
- `internal/lock/lock.go`
- `internal/orchestrator/sweep.go` の `SweepOnStartup`、`sweepFinishedWorktrees`
- `internal/workspace/sweep.go` の `SweepOrphanBranches`

## RUCM

```rucm
USE CASE NAME: 再起動して実行中の issue を引き継ぐ
BRIEF DESCRIPTION: 利用者は落ちた continuo を起動し直す。システムは worktree の身元ファイルとボードと herdr の pane を突き合わせ、生きている worker を1件引き継ぐ。システムは hook の socket を listen して逃がし先の hook を読み戻し、引き継いだ run に継続の指示を送る。
PRECONDITION: 前回の continuo のプロセスは終了している。worktree の置き場所に身元ファイルを持つ worktree が1つ以上ある。herdr は待ち受けている。前回の run の pane は生きている。
PRIMARY ACTOR: 利用者
SECONDARY ACTORS: GitHub Projects v2、herdr、Claude Code
DEPENDENCY: なし
GENERALIZATION: なし

BASIC FLOW:
1. 利用者はシステムに continuo の常駐の開始を要求する。
2. システムは VALIDATES THAT ロックファイルの flock を取れる。
3. システムは VALIDATES THAT 起動時の検査をすべて通る。
4. システムは worktree の置き場所を4階層まで走査する。
5. システムは worktree の中の身元ファイルを読む。
6. システムは VALIDATES THAT 身元ファイルが名乗る owner とリポジトリ名が worktree の置き場所の階層と一致する。
7. システムは VALIDATES THAT ボードを project item の ID 指定で取り直せる。
8. システムは VALIDATES THAT 取り直した issue の owner とリポジトリ名が worktree の置き場所の階層と一致する。
9. システムは VALIDATES THAT herdr から pane と agent の一覧を取れる。
10. システムは VALIDATES THAT worktree のパスと cwd が一致する pane がある。
11. システムは VALIDATES THAT 取り直した Status が active_states に入っている。
12. システムは VALIDATES THAT pane の agent_status を読み取れる。
13. システムは VALIDATES THAT agent_status が blocked でない。
14. システムは VALIDATES THAT 身元ファイルの引き継いだ回数が agent.max_takeover に達していない。
15. システムは身元ファイルの引き継いだ回数を1つ増やす。
16. システムは pane の agent_session からセッション UUID を取る。
17. システムは agent の一覧から pane に対応する agent 名を取る。
18. システムは run の実行時状態を組み立てる。
19. システムは hook を受ける socket の listen を始める。
20. システムは逃がし先に溜まった hook を読み戻す。
21. システムは引き継ぐ issue に印を付ける。
22. システムは溜めた hook の配送を始める。
23. システムは run の turn 数を 1 に戻す。
24. IF agent_status が working である THEN
25.   システムは Stop hook の到着を待つ。
26. ELSE
27.   システムは run に次の turn を要する印を立てる。
28.   システムは run に継続の指示を送る。
29. ENDIF
30. IF 設定の cleanup.enabled と cleanup.sweep_on_startup がどちらも真である THEN
31.   システムは Status が cleanup.on_states に入った issue の worktree を片付ける。
32.   IF 設定の cleanup.delete_branch が真である THEN
33.     システムは接頭辞に一致してどの worktree もチェックアウトしておらず印にも入っていない branch を消す。
34.   ELSE
35.     システムは孤児 branch を1本も消さない。
36.   ENDIF
37. ENDIF
38. システムは巡回のループを始める。
POSTCONDITION: 引き継いだ issue は印の集合に入っている。孤児 branch は cleanup.delete_branch が真のときだけ消えている。引き継いだ run の branch は残っている。身元ファイルの引き継いだ回数は1つ増えている。run の turn 数は 1 である。herdr の pane は閉じていない。worktree は残っている。issue の Status は running_state の選択肢のままである。

SPECIFIC ALTERNATIVE FLOW 二重起動:
RFS BASIC FLOW 2
1. システムは利用者に continuo が既に動いていることを応答する。
2. システムは herdr の pane を1つも閉じずに終了する。
3. ABORT
POSTCONDITION: 2つめの continuo のプロセスは終了している。1つめの continuo のプロセスは動いている。herdr の pane は閉じていない。

SPECIFIC ALTERNATIVE FLOW 前提の不足:
RFS BASIC FLOW 3
1. システムは利用者に失敗した検査の名前と直し方を応答する。
2. システムは herdr の pane を1つも閉じずに終了する。
3. ABORT
POSTCONDITION: continuo は常駐していない。herdr の pane は閉じていない。worktree は残っている。issue の Status は変わっていない。

SPECIFIC ALTERNATIVE FLOW 名乗りの食い違い:
RFS BASIC FLOW 6
1. システムはこの worktree を引き継ぎの候補から外す。
2. システムは置き場所の階層と身元ファイルの名乗りが食い違ったことを記録に残す。
3. ABORT
POSTCONDITION: worktree は残っている。ボードへは1バイトも書いていない。herdr の pane は閉じていない。continuo は常駐している。

SPECIFIC ALTERNATIVE FLOW issueの取り違え:
RFS BASIC FLOW 8
1. システムはこの worktree を引き継ぎの候補から外す。
2. システムは取り直した issue と置き場所の階層が食い違ったことを記録に残す。
3. ABORT
POSTCONDITION: worktree は残っている。取り直した issue の Status は変わっていない。herdr の pane は閉じていない。continuo は常駐している。

SPECIFIC ALTERNATIVE FLOW 一覧の取得の失敗:
RFS BASIC FLOW 9
1. システムは pane が生きているかどうかの判断を保留する。
2. システムは herdr の pane を1つも閉じない。
3. システムは worktree と issue の Status を残す。
4. ABORT
POSTCONDITION: herdr の pane は閉じていない。worktree は残っている。issue の Status は running_state の選択肢のままである。印の集合は空である。continuo は常駐している。

SPECIFIC ALTERNATIVE FLOW ボードの取り直しの失敗:
RFS BASIC FLOW 7
1. システムは利用者に取り直しに失敗した理由をログで応答する。
2. システムは引き継げない run の herdr の pane を閉じる。
3. システムは worktree と issue の Status を残す。
4. ABORT
POSTCONDITION: herdr の pane は閉じている。worktree は残っている。issue の Status は running_state の選択肢のままである。continuo は常駐している。

SPECIFIC ALTERNATIVE FLOW paneの不在:
RFS BASIC FLOW 10
1. システムは restart.orphan_running_action の値を読む。
2. システムは worktree と issue の Status を残す。
3. システムは issue を次の巡回に委ねる。
4. ABORT
POSTCONDITION: issue は印の集合に入っていない。worktree は残っている。issue の Status は running_state の選択肢のままである。

SPECIFIC ALTERNATIVE FLOW 引き渡し状態:
RFS BASIC FLOW 11
1. システムは herdr の pane を閉じずに残す。
2. システムは worktree を残す。
3. システムは issue を印の集合に入れない。
4. ABORT
POSTCONDITION: herdr の pane は閉じていない。worktree は残っている。issue の Status は変わっていない。利用者は pane の中身を読める。

SPECIFIC ALTERNATIVE FLOW 状態の不明:
RFS BASIC FLOW 12
1. システムは herdr の pane を閉じる。
2. システムは worktree と issue の Status を残す。
3. システムは issue を印の集合に入れない。
4. ABORT
POSTCONDITION: herdr の pane は閉じている。worktree は残っている。issue の Status は running_state の選択肢のままである。

SPECIFIC ALTERNATIVE FLOW 権限の確認での停止:
RFS BASIC FLOW 13
1. システムはボードの issue の Status に failure_state の選択肢を書く。
2. システムは herdr の pane を閉じる。
3. システムは worktree を残す。
4. ABORT
POSTCONDITION: issue の Status は failure_state の選択肢である。herdr の pane は閉じている。保留中の権限の要求は pane ごと消えている。worktree は残っている。

SPECIFIC ALTERNATIVE FLOW 引き継ぎの上限:
RFS BASIC FLOW 14
1. システムはボードの issue の Status に failure_state の選択肢を書く。
2. システムは herdr の pane を閉じる。
3. システムは worktree を残す。
4. ABORT
POSTCONDITION: issue の Status は failure_state の選択肢である。continuo は turn を1回も送っていない。worktree は残っている。

GLOBAL ALTERNATIVE FLOW 中断:
BRANCH FROM BASIC FLOW 21
WHEN 利用者が continuo を動かしている端末で Ctrl+C を入力する場合
1. システムはボードの巡回を止める。
2. システムは hook を受ける socket を閉じる。
3. システムは herdr の pane を閉じずに終了する。
4. ABORT
POSTCONDITION: continuo は常駐していない。印の集合は失われている。herdr の pane は閉じていない。worktree は残っている。issue の Status は running_state の選択肢のままである。
```

## 引き継ぐかどうかを Status で決める

**取り直した Status で分岐する**（設計 3-4 の段5a）。

| 取り直した Status | どうするか |
| --- | --- |
| active_states | 引き継ぐ |
| cleanup.on_states | pane を閉じてから worktree と branch を片付ける |
| 引き渡し（In Review / Blocked） | pane も worktree も残す。印には入れない |
| 取り直しで見つからない | pane も worktree も残す。ログに出す |

## 引き継ぐと決めたあとに agent_status で分岐する

**Status だけで決めてはならない**（設計 3-4 の段5a2）。

| agent_status | どうするか |
| --- | --- |
| idle または done | 引き継いで継続の指示を送る |
| blocked | 引き継がない。failure_state へ落として pane を閉じる |
| working | 引き継ぐ。次の turn を要する印を立てず、Stop hook を待つ |
| 読み取れない | pane を閉じ、worktree と Status を残す |

## 起動時の掃除は、引き継ぎが終わってから走らせる

**言いたいこと。**先に走らせると、**これから引き継ぐ run の branch を孤児と判定して消す。**
だから掃除はステップ30 以降、印を組み立て終えたあとに置く。

**消してよい branch は3条件を全部満たすものだけである。**

| 条件 | 落とすと何が起きるか |
| --- | --- |
| `herdr.worktree.branch_template` の接頭辞（既定 `continuo/`）で始まる | 人間が切った branch を消す |
| どの worktree もチェックアウトしていない | 作業中の worktree の branch を消す |
| 復元後の印の集合に入っていない | いま引き継いだ run の branch を消す |

**`cleanup.delete_branch` が偽なら1本も消さない**（ステップ32〜36）。
**壊れた ref だけは消す、という例外も作らない。**壊れているかどうかは利用者から見えず、
**「消すなと言ったのに消えた」という結果だけが同じである。**
片付けが `cleanup.delete_branch` を見て残した branch は、上の3条件を全部満たすので、
**設定を見ない掃除は次の起動だけでその branch を強制削除で消す。**
`continuo abandon --force` で片付けた worktree の branch には未 push の commit が
載っていることがあり、消えれば reflog を掘る以外に戻す手立ては無い。

## フローチャート

```mermaid
flowchart TD
    B1["1. 利用者が常駐の開始を要求する"]
    B2{"2. VALIDATES THAT flock を取れる"}
    B3{"3. VALIDATES THAT 起動時の検査をすべて通る"}
    B4["4. 置き場所を4階層まで走査する"]
    B5["5. 身元ファイルを読む"]
    B6{"6. VALIDATES THAT 身元ファイルの名乗りが置き場所と一致する"}
    B7{"7. VALIDATES THAT ボードを ID 指定で取り直せる"}
    B8{"8. VALIDATES THAT 取り直した issue が置き場所と一致する"}
    B9{"9. VALIDATES THAT pane と agent の一覧を取れる"}
    B10{"10. VALIDATES THAT cwd が一致する pane がある"}
    B11{"11. VALIDATES THAT Status が active_states に入っている"}
    B12{"12. VALIDATES THAT agent_status を読み取れる"}
    B13{"13. VALIDATES THAT agent_status が blocked でない"}
    B14{"14. VALIDATES THAT 引き継いだ回数が上限に達していない"}
    B15["15. 引き継いだ回数を1つ増やす"]
    B16["16. agent_session からセッション UUID を取る"]
    B17["17. pane に対応する agent 名を取る"]
    B18["18. run の実行時状態を組み立てる"]
    B19["19. socket の listen を始める"]
    B20["20. 逃がし先の hook を読み戻す"]
    B21["21. 引き継ぐ issue に印を付ける"]
    B22["22. 溜めた hook の配送を始める"]
    B23["23. turn 数を 1 に戻す"]
    B24{"24. IF agent_status が working である"}
    B25["25. Stop hook の到着を待つ"]
    B27["27. 次の turn を要する印を立てる"]
    B28["28. 継続の指示を送る"]
    B29["29. ENDIF"]
    B30{"30. IF cleanup.enabled と cleanup.sweep_on_startup がどちらも真"}
    B31["31. cleanup.on_states の issue の worktree を片付ける"]
    B32{"32. IF cleanup.delete_branch が真"}
    B33["33. 接頭辞に一致し誰も出していない branch を消す"]
    B34["34. ELSE"]
    B35["35. 孤児 branch を1本も消さない"]
    B36["36. ENDIF"]
    B37["37. ENDIF"]
    B38["38. 巡回のループを始める"]
    BPOST(["POSTCONDITION 生きている worker を引き継いでいる"])

    B1 --> B2
    B2 -- 偽 --> F1S1
    B2 -- 真 --> B3
    B3 -- 偽 --> F2S1
    B3 -- 真 --> B4 --> B5 --> B6
    B6 -- 偽 --> F3S1
    B6 -- 真 --> B7
    B7 -- 偽 --> F6S1
    B7 -- 真 --> B8
    B8 -- 偽 --> F4S1
    B8 -- 真 --> B9
    B9 -- 偽 --> F5S1
    B9 -- 真 --> B10
    B10 -- 偽 --> F7S1
    B10 -- 真 --> B11
    B11 -- 偽 --> F8S1
    B11 -- 真 --> B12
    B12 -- 偽 --> F9S1
    B12 -- 真 --> B13
    B13 -- 偽 --> F10S1
    B13 -- 真 --> B14
    B14 -- 偽 --> F11S1
    B14 -- 真 --> B15 --> B16 --> B17 --> B18 --> B19 --> B20 --> B21 --> B22 --> B23 --> B24
    B24 -- 真 --> B25 --> B29
    B24 -- 偽 --> B27 --> B28 --> B29
    B29 --> B30
    B30 -- 偽 --> B37
    B30 -- 真 --> B31 --> B32
    B32 -- 真 --> B33 --> B36
    B32 -- 偽 --> B34 --> B35 --> B36
    B36 --> B37
    B37 --> B38 --> BPOST
    B21 -. "中断: WHEN Ctrl+C を入力する場合" .-> G1S1

    subgraph SAF1 ["SPECIFIC ALTERNATIVE FLOW 二重起動 / RFS BASIC FLOW 2"]
        F1S1["1. 既に動いていることを応答する"] --> F1S2["2. pane を閉じずに終了する"] --> F1S3["3. ABORT"]
    end

    subgraph SAF2 ["SPECIFIC ALTERNATIVE FLOW 前提の不足 / RFS BASIC FLOW 3"]
        F2S1["1. 失敗した検査と直し方を応答する"] --> F2S2["2. pane を閉じずに終了する"] --> F2S3["3. ABORT"]
    end

    subgraph SAF3 ["SPECIFIC ALTERNATIVE FLOW 名乗りの食い違い / RFS BASIC FLOW 6"]
        F3S1["1. 引き継ぎの候補から外す"] --> F3S2["2. 食い違いを記録に残す"] --> F3S3["3. ABORT"]
    end

    subgraph SAF4 ["SPECIFIC ALTERNATIVE FLOW issueの取り違え / RFS BASIC FLOW 8"]
        F4S1["1. 引き継ぎの候補から外す"] --> F4S2["2. 食い違いを記録に残す"] --> F4S3["3. ABORT"]
    end

    subgraph SAF5 ["SPECIFIC ALTERNATIVE FLOW 一覧の取得の失敗 / RFS BASIC FLOW 9"]
        F5S1["1. pane の有無の判断を保留する"] --> F5S2["2. pane を1つも閉じない"] --> F5S3["3. worktree と Status を残す"] --> F5S4["4. ABORT"]
    end

    subgraph SAF6 ["SPECIFIC ALTERNATIVE FLOW ボードの取り直しの失敗 / RFS BASIC FLOW 7"]
        F6S1["1. 理由をログで応答する"] --> F6S2["2. 引き継げない run の pane を閉じる"] --> F6S3["3. worktree と Status を残す"] --> F6S4["4. ABORT"]
    end

    subgraph SAF7 ["SPECIFIC ALTERNATIVE FLOW paneの不在 / RFS BASIC FLOW 10"]
        F7S1["1. orphan_running_action の値を読む"] --> F7S2["2. worktree と Status を残す"] --> F7S3["3. 次の巡回に委ねる"] --> F7S4["4. ABORT"]
    end

    subgraph SAF8 ["SPECIFIC ALTERNATIVE FLOW 引き渡し状態 / RFS BASIC FLOW 11"]
        F8S1["1. pane を閉じずに残す"] --> F8S2["2. worktree を残す"] --> F8S3["3. 印に入れない"] --> F8S4["4. ABORT"]
    end

    subgraph SAF9 ["SPECIFIC ALTERNATIVE FLOW 状態の不明 / RFS BASIC FLOW 12"]
        F9S1["1. pane を閉じる"] --> F9S2["2. worktree と Status を残す"] --> F9S3["3. 印に入れない"] --> F9S4["4. ABORT"]
    end

    subgraph SAF10 ["SPECIFIC ALTERNATIVE FLOW 権限の確認での停止 / RFS BASIC FLOW 13"]
        F10S1["1. Status に failure_state を書く"] --> F10S2["2. pane を閉じる"] --> F10S3["3. worktree を残す"] --> F10S4["4. ABORT"]
    end

    subgraph SAF11 ["SPECIFIC ALTERNATIVE FLOW 引き継ぎの上限 / RFS BASIC FLOW 14"]
        F11S1["1. Status に failure_state を書く"] --> F11S2["2. pane を閉じる"] --> F11S3["3. worktree を残す"] --> F11S4["4. ABORT"]
    end

    subgraph GAF1 ["GLOBAL ALTERNATIVE FLOW 中断 / BRANCH FROM BASIC FLOW 21"]
        G1S1["1. ボードの巡回を止める"] --> G1S2["2. socket を閉じる"] --> G1S3["3. pane を閉じずに終了する"] --> G1S4["4. ABORT"]
    end
```

## シーケンス図

```mermaid
sequenceDiagram
    actor U as 利用者
    participant S as システム
    participant GH as GitHub Projects v2
    participant H as herdr
    participant CC as Claude Code

    U->>S: continuo の常駐の開始を要求する
    S->>S: ロックファイルの flock を取る
    alt flock を取れない
        S-->>U: continuo が既に動いていることを応答する
        Note over S: ABORT pane は1つも閉じない
    else flock を取れる
        S->>S: 起動時の検査をすべて通す
        alt 検査に落ちる
            S-->>U: 失敗した検査と直し方を応答する
            Note over S: ABORT pane は1つも閉じない
        else 検査を通る
            S->>S: 置き場所を走査して身元ファイルを読む
            S->>S: 身元ファイルの名乗りを置き場所の階層と突き合わせる
            alt 名乗りが置き場所と食い違う
                Note over S: ABORT 候補から外す。何も消さない
            else 名乗りが置き場所と一致する
                S->>GH: project item の ID 指定での取り直しを要求する
                GH-->>S: 現在の Status を応答する
                S->>S: 取り直した issue を置き場所の階層と突き合わせる
                alt 取り直した issue が置き場所と食い違う
                    Note over S: ABORT 候補から外す。ボードへ書かない
                else 取り直した issue が置き場所と一致する
                    S->>H: pane と agent の一覧を要求する
                    alt 一覧を取れない
                        Note over S: ABORT 判断を保留し次の巡回へ委ねる
                    else 一覧を取れる
                        H-->>S: pane の cwd と agent 名を応答する
                        S->>S: cwd と worktree のパスを突き合わせる
                        alt Status が引き渡しである
                            Note over S: ABORT pane も worktree も残す
                        else Status が active_states である
                            S->>H: pane の agent_status を要求する
                            H-->>S: agent_status を応答する
                            alt agent_status が blocked である
                                S->>GH: Status への failure_state の書き込みを要求する
                                S->>H: pane の close を要求する
                                Note over S: ABORT 人間の判断が要る
                            else agent_status が idle または done または working である
                                S->>S: 引き継いだ回数を1つ増やして身元ファイルへ書く
                                S->>H: pane の agent_session を要求する
                                H-->>S: セッション UUID を応答する
                                S->>S: socket の listen を始める
                                S->>S: 逃がし先の hook を読み戻す
                                S->>S: 引き継ぐ issue に印を付ける
                                S->>S: 溜めた hook の配送を始める
                                alt agent_status が working である
                                    Note over S: 次の turn を要する印を立てず Stop hook を待つ
                                else agent_status が idle または done である
                                    S->>CC: 継続の指示を送る
                                end
                                S->>S: 巡回のループを始める
                            end
                        end
                    end
                end
            end
        end
    end
```
