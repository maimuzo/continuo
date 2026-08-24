<!-- 目的: Status が cleanup.on_states に入った issue の worktree と branch を continuo が片付けるまでを RUCM で定義する -->

# ユースケース: worktree と branch を片付ける

## 根拠資料

- `docs/plans/continuo_design.md#3-9`（後始末の手順0 から手順7b）
- `docs/plans/continuo_design.md#3-18`（身元ファイル。片付けを見送った時刻）
- `docs/plans/continuo_design.md#3-20`（worktree が置き場所の内側にあることを検査する）
- `docs/plans/continuo_design.md#3-22`（worktree の置き場所は gwq の規則に合わせる）
- `docs/plans/continuo_design.md#4-1`（worktree を消す契機は Done だけにする）
- `docs/plans/continuo_design.md#8-1`（branch を消す。仕様は workspace のディレクトリだけを消す）
- `internal/workspace/cleanup.go` の `ShouldCleanup`、`Cleanup`、`effectiveBase`、`resolveWorkspaceID`
- `internal/workspace/sweep.go` と `internal/workspace/scan.go`
- `internal/orchestrator/reconcile.go` の `reconcileWorktrees`、`closeOrphanPane`
- `internal/orchestrator/lifecycle.go` の `cleanupWorktree`、`cleanupPath`

## RUCM

```rucm
USE CASE NAME: worktree と branch を片付ける
BRIEF DESCRIPTION: 巡回タイマーが巡回を起こす。システムは worktree の身元ファイルを読み、ボードの Status をまとめて取り直す。システムは Status が cleanup.on_states に入った worktree について、失うものが残っていないことを確かめてから worktree と branch と設定ファイルを消す。
PRECONDITION: システムは常駐している。worktree の置き場所に身元ファイルを持つ worktree が1つ以上ある。利用者はボードの issue の Status を cleanup.on_states の選択肢へ動かしている。設定の cleanup.enabled は true である。
PRIMARY ACTOR: 巡回タイマー
SECONDARY ACTORS: GitHub Projects v2、herdr、git
DEPENDENCY: なし
GENERALIZATION: なし

BASIC FLOW:
1. 巡回タイマーはシステムに巡回の開始を要求する。
2. システムは worktree の置き場所を走査する。
3. システムは worktree の中の身元ファイルを読む。
4. システムはボードを project item の ID 指定でまとめて取り直す。
5. システムは VALIDATES THAT 取り直した Status が cleanup.on_states に入っている。
6. システムは VALIDATES THAT worktree が workspace.root の内側にある。
7. システムは VALIDATES THAT worktree にコミットされていない変更がない。
8. システムは VALIDATES THAT branch に push されていない成果がない。
9. システムは workspace_hooks の before_remove を実行する。
10. システムは herdr に workspace の ID を渡して worktree の削除を要求する。
11. IF システムが開かせたリポジトリの親 workspace が身元ファイルに控えてある THEN
12.   システムは herdr に workspace の一覧を要求する。
13.   IF 控えた ID の workspace がそのリポジトリ本体を開いていて、同じリポジトリの worktree の workspace が1つも残っていない THEN
14.     システムは herdr にリポジトリの親 workspace を閉じることを要求する。
15.   ENDIF
16. ENDIF
17. システムは git に branch の削除を要求する。
18. システムは issue ごとの Claude Code の設定ファイルを消す。
19. システムは利用者に片付けの完了をログで応答する。
POSTCONDITION: worktree は置き場所に無い。branch は無い。herdr の workspace は閉じている。システムが開かせたリポジトリの親 workspace は、同じリポジトリの worktree が残っていなければ閉じている。人間が開いたリポジトリの workspace は開いたままである。issue ごとの Claude Code の設定ファイルは無い。issue の Status は変わっていない。印の集合は変わっていない。

SPECIFIC ALTERNATIVE FLOW 片付けの対象外:
RFS BASIC FLOW 5
1. システムは worktree を残す。
2. システムは herdr に workspace の pane の一覧を要求する。
3. IF Status が active_states に入っていて pane に agent がいる THEN
4.   システムは herdr の pane を閉じる。
5. ENDIF
6. ABORT
POSTCONDITION: worktree は残っている。branch は残っている。issue の Status は変わっていない。active_states に戻った worktree の pane は閉じている。

SPECIFIC ALTERNATIVE FLOW 置き場所の外:
RFS BASIC FLOW 6
1. システムは worktree を1つも消さない。
2. システムは利用者に封じ込め検査の失敗をログで応答する。
3. ABORT
POSTCONDITION: worktree は残っている。branch は残っている。置き場所の外のディレクトリは1つも消えていない。

SPECIFIC ALTERNATIVE FLOW 未コミットの変更:
RFS BASIC FLOW 7
1. システムは worktree を消さない。
2. システムは issue に片付けを見送った理由を1件コメントする。
3. システムは身元ファイルに片付けを見送った時刻を書く。
4. ABORT
POSTCONDITION: worktree は残っている。branch は残っている。issue に片付けを見送った理由のコメントが1件ある。身元ファイルに片付けを見送った時刻がある。

SPECIFIC ALTERNATIVE FLOW 未pushの成果:
RFS BASIC FLOW 8
1. システムは worktree を消さない。
2. システムは issue に片付けを見送った理由を1件コメントする。
3. システムは身元ファイルに片付けを見送った時刻を書く。
4. ABORT
POSTCONDITION: worktree は残っている。branch は残っている。issue に片付けを見送った理由のコメントが1件ある。次の巡回では同じ理由のコメントが増えない。

GLOBAL ALTERNATIVE FLOW 片付けの無効:
BRANCH FROM BASIC FLOW 5
WHEN 設定の cleanup.enabled が false である場合
1. システムは片付けを1つも行わない。
2. システムは issue にコメントを書かない。
3. システムは利用者に片付けを行わないことをログで応答する。
4. ABORT
POSTCONDITION: worktree は残っている。branch は残っている。issue にコメントは増えていない。
```

## 失うものがあるかを2つの検査で見る

**手順7 と手順8 は両方通す**（設計 3-9）。片方だけでは失うものを見落とす。

| 検査 | 何を見るか | 消してよい条件 |
| --- | --- | --- |
| コミットされていない変更 | `git status --porcelain` の出力 | 出力が空である。未追跡のファイルも数に入れる |
| push されていない成果（upstream がある） | `git rev-list --count @{u}..HEAD` | 出力が 0 である |
| push されていない成果（upstream が無い） | base からの差分 | 差分が無い |

## リポジトリの親 workspace を閉じる条件

**`worktree.open` は herdr の workspace を2つ開く**（issue #19）。worktree のぶんと、
`cwd` に渡したリポジトリのぶん（**リポジトリの親 workspace**）である。
**`worktree.remove` は後者を閉じない**ので、閉じるのは continuo の仕事になる。

**`cwd` を外す案は採れない。**herdr が `worktree_not_found` で断る（実測: 2026-08-25、
[test/live/herdr_test.go](test/live/herdr_test.go)）。`cwd` に worktree のパスを渡す案も
`linked_worktree_source` で断られる。**親は herdr の必須の親である。**

**閉じてよい条件は2つあり、両方満たすときだけ閉じる**（ステップ11〜16）。

| 条件 | 落とすと何が起きるか |
| --- | --- |
| continuo がその親を開かせたこと | 人間が自分で開いた workspace を閉じ、その人の pane が消える |
| 同じリポジトリの worktree の workspace が1つも残っていないこと | 別の issue が使っている pane が消える |

**2つ目が要るのは、親を閉じると配下の worktree の workspace と pane も一緒に消えるからである**
（実測: 2026-08-25）。1つ目の判定は身元ファイルの `herdr_repo_workspace_id` で持つ。
**その値は herdr の現物と突き合わせてから使う**（worktree の直下にあり、エージェントが
書き換えられるため）。

## 片付けが始まる契機は3つある

| 契機 | 誰が起こすか | 参照 |
| --- | --- | --- |
| turn が終わって Status が cleanup.on_states になっていた | システム | 設計 3-9 の手順1 |
| 巡回で worktree の身元ファイルを照合した | システム | 設計 3-9 の手順7 |
| 起動時の掃除で cleanup.on_states の issue を取った | システム | 設計 3-9 の手順6 |

## フローチャート

```mermaid
flowchart TD
    B1["1. 巡回タイマーが巡回の開始を要求する"]
    B2["2. worktree の置き場所を走査する"]
    B3["3. 身元ファイルを読む"]
    B4["4. ボードを ID 指定でまとめて取り直す"]
    B5{"5. VALIDATES THAT Status が cleanup.on_states に入っている"}
    B6{"6. VALIDATES THAT worktree が workspace.root の内側にある"}
    B7{"7. VALIDATES THAT コミットされていない変更がない"}
    B8{"8. VALIDATES THAT push されていない成果がない"}
    B9["9. workspace_hooks の before_remove を実行する"]
    B10["10. workspace の ID を渡して worktree の削除を要求する"]
    B11{"11. IF 開かせた親 workspace を控えてある"}
    B12["12. workspace の一覧を要求する"]
    B13{"13. IF 控えた ID が現物と一致し、同じリポジトリの worktree が残っていない"}
    B14["14. リポジトリの親 workspace を閉じることを要求する"]
    B17["17. git に branch の削除を要求する"]
    B18["18. issue ごとの設定ファイルを消す"]
    B19["19. 片付けの完了をログで応答する"]
    BPOST(["POSTCONDITION worktree と branch が無い"])

    B1 --> B2 --> B3 --> B4 --> B5
    B5 -- 偽 --> F1S1
    B5 -- 真 --> B6
    B6 -- 偽 --> F2S1
    B6 -- 真 --> B7
    B7 -- 偽 --> F3S1
    B7 -- 真 --> B8
    B8 -- 偽 --> F4S1
    B8 -- 真 --> B9 --> B10 --> B11
    B11 -- 偽 --> B17
    B11 -- 真 --> B12 --> B13
    B13 -- 偽 --> B17
    B13 -- 真 --> B14 --> B17
    B17 --> B18 --> B19 --> BPOST
    B5 -. "片付けの無効: WHEN cleanup.enabled が false の場合" .-> G1S1

    subgraph SAF1 ["SPECIFIC ALTERNATIVE FLOW 片付けの対象外 / RFS BASIC FLOW 5"]
        F1S1["1. worktree を残す"] --> F1S2["2. workspace の pane の一覧を要求する"] --> F1S3{"3. IF active_states に戻っていて pane に agent がいる"}
        F1S3 -- 真 --> F1S4["4. pane を閉じる"] --> F1S5["5. ENDIF"]
        F1S3 -- 偽 --> F1S5
        F1S5 --> F1S6["6. ABORT"]
    end

    subgraph SAF2 ["SPECIFIC ALTERNATIVE FLOW 置き場所の外 / RFS BASIC FLOW 6"]
        F2S1["1. worktree を1つも消さない"] --> F2S2["2. 封じ込め検査の失敗をログで応答する"] --> F2S3["3. ABORT"]
    end

    subgraph SAF3 ["SPECIFIC ALTERNATIVE FLOW 未コミットの変更 / RFS BASIC FLOW 7"]
        F3S1["1. worktree を消さない"] --> F3S2["2. 見送った理由を1件コメントする"] --> F3S3["3. 身元ファイルに見送った時刻を書く"] --> F3S4["4. ABORT"]
    end

    subgraph SAF4 ["SPECIFIC ALTERNATIVE FLOW 未pushの成果 / RFS BASIC FLOW 8"]
        F4S1["1. worktree を消さない"] --> F4S2["2. 見送った理由を1件コメントする"] --> F4S3["3. 身元ファイルに見送った時刻を書く"] --> F4S4["4. ABORT"]
    end

    subgraph GAF1 ["GLOBAL ALTERNATIVE FLOW 片付けの無効 / BRANCH FROM BASIC FLOW 5"]
        G1S1["1. 片付けを1つも行わない"] --> G1S2["2. issue にコメントを書かない"] --> G1S3["3. 片付けを行わないことをログで応答する"] --> G1S4["4. ABORT"]
    end
```

## シーケンス図

```mermaid
sequenceDiagram
    actor T as 巡回タイマー
    participant S as システム
    participant GH as GitHub Projects v2
    participant G as git
    participant H as herdr

    T->>S: 巡回の開始を要求する
    S->>S: 置き場所を走査して身元ファイルを読む
    S->>GH: project item の ID 指定での取り直しを要求する
    GH-->>S: 現在の Status を応答する
    alt Status が cleanup.on_states に入っていない
        S->>H: workspace の pane の一覧を要求する
        H-->>S: pane と agent を応答する
        opt Status が active_states に戻っていて pane に agent がいる
            S->>H: pane の close を要求する
        end
        Note over S: ABORT worktree は残す
    else Status が cleanup.on_states に入っている
        S->>S: worktree が workspace.root の内側にあることを検証する
        S->>G: コミットされていない変更の有無を要求する
        G-->>S: 変更の一覧を応答する
        S->>G: push されていない成果の有無を要求する
        G-->>S: 差分の件数を応答する
        alt 失うものが残っている
            S->>GH: 見送った理由のコメントの投稿を要求する
            S->>S: 身元ファイルに見送った時刻を書く
            Note over S: ABORT worktree は残す
        else 失うものが残っていない
            S->>S: workspace_hooks の before_remove を実行する
            S->>H: workspace の ID を渡して worktree の削除を要求する
            H-->>S: workspace を閉じたことを応答する
            opt システムが開かせたリポジトリの親 workspace を控えてある
                S->>H: workspace の一覧を要求する
                H-->>S: 開いている workspace を応答する
                opt 控えた ID が現物と一致し、同じリポジトリの worktree が残っていない
                    S->>H: リポジトリの親 workspace の close を要求する
                end
            end
            S->>G: branch の削除を要求する
            S->>S: issue ごとの設定ファイルを消す
            S-->>T: 片付けの完了をログで応答する
        end
    end
```
