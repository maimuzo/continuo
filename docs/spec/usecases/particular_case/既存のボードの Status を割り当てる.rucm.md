# ユースケース: 既存のボードの Status を割り当てる

## 根拠資料

- docs/plans/continuo_design.md#3-32
- docs/plans/continuo_design.md#3-34
- docs/plans/continuo_design.md#3-4
- docs/plans/continuo_design.md#3-6
- docs/plans/continuo_design.md#3-9
- docs/plans/continuo_design.md#3-31
- docs/plans/continuo_design.md#4-1
- docs/plans/continuo_design.md#5-1
- docs/plans/continuo_design.md#5-2
- docs/trying_it_out.md
- internal/scaffold/scaffold.go
- internal/scaffold/detect.go
- internal/scaffold/template.go
- internal/doctor/checks.go
- CLAUDE.md

## RUCM

```rucm
USE CASE NAME: 既存のボードの Status を割り当てる
BRIEF DESCRIPTION: 利用者が continuo setup を実行する。システムはボードの Status フィールドの選択肢を番号付きで並べる。システムは continuo の5つの役割を1つずつ説明して選択肢を選ばせる。システムは決まった割り当てを WORKFLOW.md へ書く。
PRECONDITION: 利用者は gh auth login -s project を実行済みである。利用者は GitHub Projects v2 のボードを1枚持っている。ボードは single-select の Status フィールドを持つ。
PRIMARY ACTOR: 利用者
SECONDARY ACTORS: GitHub Projects v2
DEPENDENCY: なし
GENERALIZATION: なし

BASIC FLOW:
1. 利用者はシステムに continuo setup の実行を要求する。
2. システムは VALIDATES THAT WORKFLOW.md が無いか --force が渡されている。
3. システムは gh からボードの owner とボードの番号を引く。
4. システムは VALIDATES THAT ボードの Status フィールドの選択肢を読み取れる。
5. システムは VALIDATES THAT 読み取った選択肢が5個以上ある。
6. システムは利用者に選択肢の一覧を番号付きで応答する。
7. DO
8.   システムは利用者にいま割り当てる役割の説明を応答する。
9.   利用者はシステムに番号を送信する。
10.   システムは VALIDATES THAT 番号が一覧の範囲内である。
11.   システムは VALIDATES THAT 番号が 0 でない。
12.   システムは VALIDATES THAT 番号の選択肢が他の役割に割り当てられていない。
13.   システムは番号の選択肢をいま割り当てる役割へ割り当てる。
14. UNTIL 5つの役割すべてに選択肢が割り当てられている
15. システムは利用者に5つの役割の割り当ての一覧を応答する。
16. システムは割り当てを WORKFLOW.md へ書き出す。
17. システムは利用者に WORKFLOW.md のパスを応答する。
POSTCONDITION: WORKFLOW.md がある。5つの役割それぞれに1つの選択肢が書かれている。同じ選択肢が2つの役割に書かれていない。ボードの選択肢は変わっていない。ボードの item の Status は変わっていない。

SPECIFIC ALTERNATIVE FLOW 上書きの許可が無い:
RFS BASIC FLOW 2
1. システムは利用者に WORKFLOW.md が既にあることを応答する。
2. システムは利用者に --force を付けて実行し直す案内を応答する。
3. ABORT
POSTCONDITION: WORKFLOW.md の中身は変わっていない。システムは役割の割り当てを1つも尋ねていない。終了コードは 1 である。

SPECIFIC ALTERNATIVE FLOW ボードを読めない:
RFS BASIC FLOW 4
1. システムは利用者にボードを読めない理由を応答する。
2. システムは利用者に理由に対応する直し方を応答する。
3. ABORT
POSTCONDITION: WORKFLOW.md は作られていない。システムは役割の割り当てを1つも尋ねていない。終了コードは 1 である。

SPECIFIC ALTERNATIVE FLOW 選択肢が足りない:
RFS BASIC FLOW 5
1. システムは利用者に選択肢の数が5個に満たないことを応答する。
2. システムは利用者に GitHub の画面から選択肢を足す手順を応答する。
3. システムは利用者に API で選択肢を足すと設定済みの Status が全部消えることを応答する。
4. ABORT
POSTCONDITION: WORKFLOW.md は作られていない。ボードの選択肢は変わっていない。システムは役割の割り当てを1つも尋ねていない。終了コードは 1 である。

SPECIFIC ALTERNATIVE FLOW 番号が範囲外:
RFS BASIC FLOW 10
1. システムは利用者に入力が一覧の番号でないことを応答する。
2. システムは利用者に選べる番号の範囲を応答する。
3. RESUME STEP 8
POSTCONDITION: 役割への割り当ては増えていない。システムは同じ役割の番号をもう一度待っている。

SPECIFIC ALTERNATIVE FLOW 該当する選択肢が無い:
RFS BASIC FLOW 11
1. システムは利用者にいま割り当てる役割へ渡せる選択肢がボードに無いことを応答する。
2. システムは利用者に GitHub の画面から選択肢を足す手順を応答する。
3. システムは利用者に API で選択肢を足すと設定済みの Status が全部消えることを応答する。
4. ABORT
POSTCONDITION: WORKFLOW.md は作られていない。ボードの選択肢は変わっていない。それまでに選んだ番号は保存されていない。終了コードは 1 である。

SPECIFIC ALTERNATIVE FLOW 二重割り当て:
RFS BASIC FLOW 12
1. システムは利用者に選んだ選択肢が別の役割に割り当て済みであることを応答する。
2. システムは利用者に割り当て済みの役割の名前を応答する。
3. RESUME STEP 8
POSTCONDITION: 役割への割り当ては増えていない。1つの選択肢は1つの役割だけに割り当てられている。システムは同じ役割の番号をもう一度待っている。

GLOBAL ALTERNATIVE FLOW 中断:
BRANCH FROM BASIC FLOW 15
WHEN 利用者が Ctrl+C を入力した場合
1. システムは利用者に割り当てを保存しないことを応答する。
2. ABORT
POSTCONDITION: WORKFLOW.md は作られていない。5つの役割の割り当ては保存されていない。ボードの選択肢は変わっていない。ボードの item の Status は変わっていない。
```

## 割り当てる5つの役割

**尋ねる順序はこの表の上から下である。**システムは役割の名前を先に出さず、**continuo がその Status で何をするかの説明を先に出してから番号を待つ。**

| 役割 | 画面に出す説明 | WORKFLOW.md に書くキー |
| --- | --- | --- |
| 着手待ち | continuo はここから issue を取ります | `dispatch_state`、`active_states` の1つめ |
| 作業中 | continuo は issue を取ったときにここへ動かします | `running_state`、`active_states` の2つめ |
| レビュー待ち | エージェントが終わったと表明したらここへ動かします。人間が見ます | `status_signal_map.review` |
| 保留 | エージェントが判断を仰ぐとき、打ち切ったときにここへ動かします | `failure_state`、`status_signal_map.blocked` |
| 完了 | 人間がここへ動かすと continuo が worktree と branch を片付けます | `terminal_states` の1つめ |

**番号 `0` は「この役割に使える選択肢がボードに無い」を表す。**5つの役割は continuo の動作に全部必要なので、
`0` が入ったら割り当てを打ち切る（`該当する選択肢が無い` のフロー）。

## ボードを読めないときの案内

| 読めない理由 | システムが出す直し方 |
| --- | --- |
| ボードの候補が複数あって番号が決まらない | `--project <番号>` を付けて実行し直す |
| owner を引けない | `--owner <名前>` を付けて実行し直す |
| gh の scope に project が無い | `gh auth login -s project` を実行する |
| Status フィールドが見つからない | `--status-field <名前>` でフィールドの名前を渡す |
| レートリミットに当たった | 時間をおいて実行し直す |

## フローチャート

```mermaid
flowchart TD
    B1["1. 利用者はシステムに continuo setup の実行を要求する"]
    B2{"2. VALIDATES THAT WORKFLOW.md が無いか --force が渡されている"}
    B3["3. システムは gh からボードの owner とボードの番号を引く"]
    B4{"4. VALIDATES THAT ボードの Status フィールドの選択肢を読み取れる"}
    B5{"5. VALIDATES THAT 読み取った選択肢が5個以上ある"}
    B6["6. システムは利用者に選択肢の一覧を番号付きで応答する"]
    B7["7. DO"]
    B8["8. システムは利用者にいま割り当てる役割の説明を応答する"]
    B9["9. 利用者はシステムに番号を送信する"]
    B10{"10. VALIDATES THAT 番号が一覧の範囲内である"}
    B11{"11. VALIDATES THAT 番号が 0 でない"}
    B12{"12. VALIDATES THAT 番号の選択肢が他の役割に割り当てられていない"}
    B13["13. システムは番号の選択肢をいま割り当てる役割へ割り当てる"]
    B14{"14. UNTIL 5つの役割すべてに選択肢が割り当てられている"}
    B15["15. システムは利用者に5つの役割の割り当ての一覧を応答する"]
    B16["16. システムは割り当てを WORKFLOW.md へ書き出す"]
    B17["17. システムは利用者に WORKFLOW.md のパスを応答する"]
    BPOST(["POSTCONDITION 5つの役割それぞれに1つの選択肢が書かれている"])

    B1 --> B2
    B2 -- 真 --> B3
    B2 -- 偽 --> F1S1
    B3 --> B4
    B4 -- 真 --> B5
    B4 -- 偽 --> F2S1
    B5 -- 真 --> B6
    B5 -- 偽 --> F3S1
    B6 --> B7
    B7 --> B8
    B8 --> B9
    B9 --> B10
    B10 -- 真 --> B11
    B10 -- 偽 --> F4S1
    B11 -- 真 --> B12
    B11 -- 偽 --> F5S1
    B12 -- 真 --> B13
    B12 -- 偽 --> F6S1
    B13 --> B14
    B14 -- 偽 --> B8
    B14 -- 真 --> B15
    B15 --> B16
    B16 --> B17
    B17 --> BPOST
    B15 -- "WHEN 利用者が Ctrl+C を入力した場合" --> F7S1

    subgraph SAF1 ["SPECIFIC ALTERNATIVE FLOW 上書きの許可が無い / RFS BASIC FLOW 2"]
        F1S1["1. システムは利用者に WORKFLOW.md が既にあることを応答する"]
        F1S2["2. システムは利用者に --force を付けて実行し直す案内を応答する"]
        F1S3["3. ABORT"]
        F1S1 --> F1S2 --> F1S3
    end

    subgraph SAF2 ["SPECIFIC ALTERNATIVE FLOW ボードを読めない / RFS BASIC FLOW 4"]
        F2S1["1. システムは利用者にボードを読めない理由を応答する"]
        F2S2["2. システムは利用者に理由に対応する直し方を応答する"]
        F2S3["3. ABORT"]
        F2S1 --> F2S2 --> F2S3
    end

    subgraph SAF3 ["SPECIFIC ALTERNATIVE FLOW 選択肢が足りない / RFS BASIC FLOW 5"]
        F3S1["1. システムは利用者に選択肢の数が5個に満たないことを応答する"]
        F3S2["2. システムは利用者に GitHub の画面から選択肢を足す手順を応答する"]
        F3S3["3. システムは利用者に API で選択肢を足すと設定済みの Status が全部消えることを応答する"]
        F3S4["4. ABORT"]
        F3S1 --> F3S2 --> F3S3 --> F3S4
    end

    subgraph SAF4 ["SPECIFIC ALTERNATIVE FLOW 番号が範囲外 / RFS BASIC FLOW 10"]
        F4S1["1. システムは利用者に入力が一覧の番号でないことを応答する"]
        F4S2["2. システムは利用者に選べる番号の範囲を応答する"]
        F4S3["3. RESUME STEP 8"]
        F4S1 --> F4S2 --> F4S3
    end

    subgraph SAF5 ["SPECIFIC ALTERNATIVE FLOW 該当する選択肢が無い / RFS BASIC FLOW 11"]
        F5S1["1. システムは利用者にいま割り当てる役割へ渡せる選択肢がボードに無いことを応答する"]
        F5S2["2. システムは利用者に GitHub の画面から選択肢を足す手順を応答する"]
        F5S3["3. システムは利用者に API で選択肢を足すと設定済みの Status が全部消えることを応答する"]
        F5S4["4. ABORT"]
        F5S1 --> F5S2 --> F5S3 --> F5S4
    end

    subgraph SAF6 ["SPECIFIC ALTERNATIVE FLOW 二重割り当て / RFS BASIC FLOW 12"]
        F6S1["1. システムは利用者に選んだ選択肢が別の役割に割り当て済みであることを応答する"]
        F6S2["2. システムは利用者に割り当て済みの役割の名前を応答する"]
        F6S3["3. RESUME STEP 8"]
        F6S1 --> F6S2 --> F6S3
    end

    subgraph GAF1 ["GLOBAL ALTERNATIVE FLOW 中断 / BRANCH FROM BASIC FLOW 15"]
        F7S1["1. システムは利用者に割り当てを保存しないことを応答する"]
        F7S2["2. ABORT"]
        F7S1 --> F7S2
    end

    F4S3 --> B8
    F6S3 --> B8
```

## シーケンス図

```mermaid
sequenceDiagram
    actor User as 利用者
    participant Sys as システム
    participant GH as GitHub Projects v2

    User->>Sys: continuo setup の実行を要求する
    Sys->>Sys: WORKFLOW.md が無いか --force が渡されているかを検証する
    alt WORKFLOW.md があり --force が無い
        Sys-->>User: WORKFLOW.md が既にあることと --force の案内を応答する
        Note over Sys: ABORT 終了コード 1
    else 上書きしてよい
        Sys->>GH: gh からボードの owner とボードの番号を引く
        GH-->>Sys: owner とボードの番号を返す
        Sys->>GH: Status フィールドの選択肢を要求する
        alt 選択肢を読み取れない
            GH-->>Sys: エラーを返す
            Sys-->>User: 読めない理由と直し方を応答する
            Note over Sys: ABORT 終了コード 1
        else 選択肢を読み取れる
            GH-->>Sys: 選択肢の一覧を返す
            alt 選択肢が5個に満たない
                Sys-->>User: 選択肢を GitHub の画面から足す手順を応答する
                Note over Sys: ABORT 終了コード 1
            else 選択肢が5個以上ある
                Sys-->>User: 選択肢の一覧を番号付きで応答する
                loop 着手待ち 作業中 レビュー待ち 保留 完了 の順に5回
                    Sys-->>User: いま割り当てる役割の説明を応答する
                    User->>Sys: 番号を送信する
                    alt 番号が一覧の範囲外である
                        Sys-->>User: 選べる番号の範囲を応答する
                        Note over Sys: RESUME STEP 8 同じ役割を尋ね直す
                    else 番号が 0 である
                        Sys-->>User: 選択肢を GitHub の画面から足す手順を応答する
                        Note over Sys: ABORT 終了コード 1
                    else 番号の選択肢が他の役割に割り当て済みである
                        Sys-->>User: 割り当て済みの役割の名前を応答する
                        Note over Sys: RESUME STEP 8 同じ役割を尋ね直す
                    else 番号を受け付ける
                        Sys->>Sys: 番号の選択肢を役割へ割り当てる
                    end
                end
                Sys-->>User: 5つの役割の割り当ての一覧を応答する
                alt 利用者が Ctrl+C を入力する
                    Sys-->>User: 割り当てを保存しないことを応答する
                    Note over Sys: ABORT WORKFLOW.md は作られていない
                else 割り当てを書き出す
                    Sys->>Sys: 割り当てを WORKFLOW.md へ書き出す
                    Sys-->>User: WORKFLOW.md のパスを応答する
                end
            end
        end
    end
```
