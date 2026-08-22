<!-- 目的: ネットワークインストーラーで continuo を導入するまでの流れを RUCM で定義する -->

# ユースケース: continuo を入れる

## 根拠資料

- `docs/plans/continuo_design.md#3-36`（入れ方はネットワークインストーラーの1行にする。3つの部品と、道具が足りないときの扱い）
- `docs/plans/continuo_design.md#3-32b`（Windows ネイティブに対応しない理由）
- `install.sh`（`detect_platform` / `resolve_version` / `install_binary` / `check_deps` / `ask`）
- `.github/workflows/release.yml`（タグを打つとビルドして release へ載せる）
- `README.ja.md`（「入れる」の節）

## RUCM

```rucm
USE CASE NAME: continuo を入れる
BRIEF DESCRIPTION: 利用者はネットワークインストーラーを実行する。システムは利用環境に合う実行ファイルを配布物から取り、チェックサムを照合できたときだけ置き先へ配る。システムは前提の道具の不足を調べ、利用者に尋ねてから入れる。
PRECONDITION: 利用者の手元に curl または wget がある。利用者の手元に tar がある。
PRIMARY ACTOR: 利用者
SECONDARY ACTORS: 配布サーバ、パッケージ管理
DEPENDENCY: なし
GENERALIZATION: なし

BASIC FLOW:
1. 利用者はシステムに導入を要求する。
2. システムは VALIDATES THAT 利用者の OS が対応している。
3. システムは VALIDATES THAT 利用者の命令セットが対応している。
4. システムは VALIDATES THAT 配布された版が1つ以上ある。
5. システムは実行ファイルの書庫を取得する。
6. システムは VALIDATES THAT 書庫を取得できた。
7. システムは VALIDATES THAT チェックサムの一覧を取得できた。
8. システムは書庫のチェックサムを計算する。
9. システムは VALIDATES THAT 計算した値が一覧の値と一致する。
10. システムは書庫を展開する。
11. システムは実行ファイルを置き先へ配る。
12. システムは前提の道具の有無を調べる。
13. システムは VALIDATES THAT 端末から利用者の応答を受け取れる。
14. システムは利用者に足りない道具を入れてよいかを尋ねる。
15. 利用者はシステムに入れるかどうかを答える。
16. システムは利用者が承諾した道具を入れる。
17. システムは利用者に置き先を PATH へ通す方法を応答する。
18. システムは利用者に次に実行するコマンドを応答する。
POSTCONDITION: 置き先に continuo の実行ファイルがある。実行ファイルに実行の権限が付いている。利用者は次に実行するコマンドを読める。一時ディレクトリは残っていない。

SPECIFIC ALTERNATIVE FLOW 対応しないOS:
RFS BASIC FLOW 2
1. システムは利用者に WSL2 の中で実行する案内を応答する。
2. ABORT
POSTCONDITION: 実行ファイルは置かれていない。利用者は WSL2 を使う案内を読める。

SPECIFIC ALTERNATIVE FLOW 対応しない命令セット:
RFS BASIC FLOW 3
1. システムは利用者に対応している命令セットを応答する。
2. ABORT
POSTCONDITION: 実行ファイルは置かれていない。利用者は対応している命令セットを読める。

SPECIFIC ALTERNATIVE FLOW 配布がまだ無い:
RFS BASIC FLOW 4
1. システムは利用者にまだ配布していないことを応答する。
2. システムは利用者にソースから作る手順を応答する。
3. ABORT
POSTCONDITION: 実行ファイルは置かれていない。利用者はソースから作る手順を読める。

SPECIFIC ALTERNATIVE FLOW 書庫を取得できない:
RFS BASIC FLOW 6
1. システムは利用者に配布している一覧の場所を応答する。
2. システムは一時ディレクトリを消す。
3. ABORT
POSTCONDITION: 実行ファイルは置かれていない。一時ディレクトリは残っていない。

SPECIFIC ALTERNATIVE FLOW 照合できない:
RFS BASIC FLOW 7
1. システムは書庫を展開しない。
2. システムは利用者に照合できない理由を応答する。
3. システムは利用者に照合を省く指定の方法を応答する。
4. ABORT
POSTCONDITION: 実行ファイルは置かれていない。一時ディレクトリは残っていない。利用者は照合を省く指定の方法を読める。

SPECIFIC ALTERNATIVE FLOW チェックサムが一致しない:
RFS BASIC FLOW 9
1. システムは書庫を展開しない。
2. システムは利用者に期待した値と計算した値を応答する。
3. システムは一時ディレクトリを消す。
4. ABORT
POSTCONDITION: 実行ファイルは置かれていない。一時ディレクトリは残っていない。利用者は取得をやり直す案内を読める。

SPECIFIC ALTERNATIVE FLOW 端末が無い:
RFS BASIC FLOW 13
1. システムは道具を1つも入れない。
2. システムは利用者に足りない道具の一覧を応答する。
3. RESUME STEP 17
POSTCONDITION: 置き先に continuo の実行ファイルがある。道具は1つも入っていない。利用者は足りない道具の一覧を読める。

GLOBAL ALTERNATIVE FLOW 依存を入れない指定:
BRANCH FROM BASIC FLOW 12
WHEN 利用者が依存を入れない指定をした場合
1. システムは道具を1つも入れない。
2. システムは利用者に足りない道具の一覧を応答する。
3. RESUME STEP 17
POSTCONDITION: 置き先に continuo の実行ファイルがある。道具は1つも入っていない。利用者は足りない道具の一覧を読める。

GLOBAL ALTERNATIVE FLOW 照合を省く指定:
BRANCH FROM BASIC FLOW 7
WHEN 利用者が照合を省く指定をした場合
1. システムは利用者に照合していないことを応答する。
2. RESUME STEP 10
POSTCONDITION: 置き先に continuo の実行ファイルがある。利用者はチェックサムを照合していないことを読める。

GLOBAL ALTERNATIVE FLOW 使えない版の指定:
BRANCH FROM BASIC FLOW 1
WHEN 利用者が使えない文字を含む版を指定した場合
1. システムは利用者に版に使える文字を応答する。
2. ABORT
POSTCONDITION: 実行ファイルは置かれていない。配布サーバへの取得は1度も行われていない。

GLOBAL ALTERNATIVE FLOW 案内だけを求める:
BRANCH FROM BASIC FLOW 1
WHEN 利用者が案内の表示を指定した場合
1. システムは利用者にオプションの一覧を応答する。
2. ABORT
POSTCONDITION: 実行ファイルは置かれていない。利用者はオプションの一覧を読める。

GLOBAL ALTERNATIVE FLOW 知らないオプション:
BRANCH FROM BASIC FLOW 1
WHEN 利用者が知らないオプションを渡した場合
1. システムは利用者に受け付けなかったオプションを応答する。
2. ABORT
POSTCONDITION: 実行ファイルは置かれていない。配布サーバへの取得は1度も行われていない。
```

## フローチャート

```mermaid
flowchart TD
    START([開始]) --> B1["1. 利用者はシステムに導入を要求する"]
    B1 -. "WHEN 利用者が案内の表示を指定した場合" .-> H1["案内だけを求める 1. オプションの一覧を応答する"]
    H1 --> HEND([ABORT])
    B1 -. "WHEN 利用者が知らないオプションを渡した場合" .-> X1["知らないオプション 1. 受け付けなかったオプションを応答する"]
    X1 --> XEND([ABORT])
    B1 -. "WHEN 利用者が使えない文字を含む版を指定した場合" .-> W1["使えない版の指定 1. 版に使える文字を応答する"]
    W1 --> WEND([ABORT])
    B1 --> B2{"2. VALIDATES THAT OS が対応している"}
    B2 -- 偽 --> O1["対応しないOS 1. WSL2 の中で実行する案内を応答する"]
    O1 --> OEND([ABORT])
    B2 -- 真 --> B3{"3. VALIDATES THAT 命令セットが対応している"}
    B3 -- 偽 --> A1["対応しない命令セット 1. 対応している命令セットを応答する"]
    A1 --> AEND([ABORT])
    B3 -- 真 --> B4{"4. VALIDATES THAT 配布された版が1つ以上ある"}
    B4 -- 偽 --> N1["配布がまだ無い 1. まだ配布していないことを応答する"]
    N1 --> N2["配布がまだ無い 2. ソースから作る手順を応答する"]
    N2 --> NEND([ABORT])
    B4 -- 真 --> B5["5. 実行ファイルの書庫を取得する"]
    B5 --> B6{"6. VALIDATES THAT 書庫を取得できた"}
    B6 -- 偽 --> D1["書庫を取得できない 1. 配布している一覧の場所を応答する"]
    D1 --> D2["書庫を取得できない 2. 一時ディレクトリを消す"]
    D2 --> DEND([ABORT])
    B6 -- 真 --> B7{"7. VALIDATES THAT チェックサムの一覧を取得できた"}
    B7 -. "WHEN 利用者が照合を省く指定をした場合" .-> K1["照合を省く指定 1. 照合していないことを応答する"]
    K1 -. "RESUME STEP 10" .-> B10
    B7 -- 偽 --> L1["照合できない 1. 書庫を展開しない"]
    L1 --> L2["照合できない 2. 照合できない理由を応答する"]
    L2 --> L3["照合できない 3. 照合を省く指定の方法を応答する"]
    L3 --> LEND([ABORT])
    B7 -- 真 --> B8["8. 書庫のチェックサムを計算する"]
    B8 --> B9{"9. VALIDATES THAT 計算した値が一覧の値と一致する"}
    B9 -- 偽 --> S1["チェックサムが一致しない 1. 書庫を展開しない"]
    S1 --> S2["チェックサムが一致しない 2. 期待した値と計算した値を応答する"]
    S2 --> S3["チェックサムが一致しない 3. 一時ディレクトリを消す"]
    S3 --> SEND([ABORT])
    B9 -- 真 --> B10["10. 書庫を展開する"]
    B10 --> B11["11. 実行ファイルを置き先へ配る"]
    B11 --> B12["12. 前提の道具の有無を調べる"]
    B12 -. "WHEN 利用者が依存を入れない指定をした場合" .-> G1["依存を入れない指定 1. 道具を1つも入れない"]
    G1 --> G2["依存を入れない指定 2. 足りない道具の一覧を応答する"]
    G2 -. "RESUME STEP 17" .-> B17
    B12 --> B13{"13. VALIDATES THAT 端末から利用者の応答を受け取れる"}
    B13 -- 偽 --> T1["端末が無い 1. 道具を1つも入れない"]
    T1 --> T2["端末が無い 2. 足りない道具の一覧を応答する"]
    T2 -. "RESUME STEP 17" .-> B17
    B13 -- 真 --> B14["14. 足りない道具を入れてよいかを尋ねる"]
    B14 --> B15["15. 利用者は入れるかどうかを答える"]
    B15 --> B16["16. 承諾した道具を入れる"]
    B16 --> B17["17. 置き先を PATH へ通す方法を応答する"]
    B17 --> B18["18. 次に実行するコマンドを応答する"]
    B18 --> END([終了])
```

## シーケンス図

```mermaid
sequenceDiagram
    actor USER as 利用者
    participant SYS as システム
    participant REL as 配布サーバ
    participant PKG as パッケージ管理

    USER->>SYS: 1. 導入を要求する
    alt 使えない文字を含む版を指定した
        SYS-->>USER: 使えない版の指定 1. 版に使える文字を応答する
        Note over USER,REL: ABORT 配布サーバへ1度も繋がない
    else 版の指定が妥当である
        SYS->>SYS: 2-3. OS と命令セットを見分ける
        alt OS が対応していない
            SYS-->>USER: 対応しないOS 1. WSL2 の中で実行する案内を応答する
            Note over USER,SYS: ABORT 実行ファイルは置かれない
        else OS が対応している
            SYS->>REL: 4. 配布された版を問い合わせる
            REL-->>SYS: 最新の版
            alt 配布された版が1つも無い
                SYS-->>USER: 配布がまだ無い 1-2. まだ配布していないことと、ソースから作る手順を応答する
                Note over USER,SYS: ABORT 実行ファイルは置かれない
            else 配布された版がある
                SYS->>REL: 5. 実行ファイルの書庫を要求する
                REL-->>SYS: 6. 書庫
                SYS->>REL: 7. チェックサムの一覧を要求する
                alt 照合を省く指定をしている
                    SYS-->>USER: 照合を省く指定 1. 照合していないことを応答する
                    Note over SYS,REL: RESUME STEP 10
                else 一覧を取得できない
                    SYS-->>USER: 照合できない 2-3. 理由と、照合を省く指定の方法を応答する
                    Note over USER,SYS: ABORT 実行ファイルは置かれない
                else 一覧を取得できた
                    REL-->>SYS: チェックサムの一覧
                    SYS->>SYS: 8-9. 計算した値と一覧の値を突き合わせる
                    alt 一致しない
                        SYS-->>USER: チェックサムが一致しない 2. 期待した値と計算した値を応答する
                        Note over USER,SYS: ABORT 実行ファイルは置かれない
                    end
                end
                SYS->>SYS: 10-11. 書庫を展開して置き先へ配る
                SYS->>SYS: 12. 前提の道具の有無を調べる
                alt 端末から応答を受け取れない
                    SYS-->>USER: 端末が無い 2. 足りない道具の一覧を応答する
                    Note over USER,PKG: RESUME STEP 17 道具は1つも入らない
                else 端末から応答を受け取れる
                    SYS-->>USER: 14. 足りない道具を入れてよいかを尋ねる
                    USER->>SYS: 15. 入れるかどうかを答える
                    SYS->>PKG: 16. 承諾した道具を入れる
                    PKG-->>SYS: 導入の結果
                end
                SYS-->>USER: 17-18. PATH の通し方と、次に実行するコマンドを応答する
            end
        end
    end
```
