# ユースケース: 成果を push して pull request を出す

> **この記述からテストコードは作らない。**
> 段を行うのはエージェント（Claude Code）で、指示書の文面を読んで動く。エージェントを起動して段を通す自動テストは、レートリミットを使い、結果も毎回同じにならない。
> 文面に何が書いてあるかは `test/internal/prompt/` の `TestTemplate_…` が確かめている。
> `check_update_tests.py` が出す `[W1]`（テスト未生成パス）は、ここでは想定どおりである。

## 根拠資料

- `internal/prompt/builtin.md` の 3-4（commit して push する）、3-5（pull request を出す）、7-3（別のリポジトリへ pull request を出すとき）、7-4（この指示書が決めていないこと）、6-1（命令として扱ってよいもの）
- `docs/plans/continuo_design.md#5-3i`（PR はエージェントが出す）
- `docs/plans/continuo_design.md#5-3t`（コメントと pull request の本文・題名を、シェルに実行させずに渡す）

## この記述は何のためにあるか

**エージェントが `builtin.md` の 3-4 と 3-5 をどう使うかを、記録として残すためである。テストは持たない。**
主体はエージェントであり、Claude Code の中で起きることをテストから観測する手立てが無い（`claude -p` は使用禁止のため）。

`指示書に沿って issue を1件仕上げる` が、実装の段のあとでこの記述を取り込む。

**`本家のリポジトリへ PR を出す` とは別の記述である。**あちらは、fork で作業した成果を本家へ出す場合である。こちらは、continuo が用意した worktree の branch から pull request を出す、どの issue でも通る段である。

## RUCM

```rucm
USE CASE NAME: 成果を push して pull request を出す
BRIEF DESCRIPTION: エージェントは worktree の変更を commit して push する。エージェントはいま居る branch の pull request が既にあるかを確かめる。エージェントは無ければ issue を閉じる行を本文に持つ pull request を作る。エージェントは行き先の pull request を控える。
PRECONDITION: エージェントは continuo が用意した worktree と branch で実装を終えている。
PRIMARY ACTOR: エージェント
SECONDARY ACTORS: システム、GitHub、利用者
DEPENDENCY: なし
GENERALIZATION: なし

BASIC FLOW:
1. エージェントは commit して push する。
2. エージェントは VALIDATES THAT いま居る branch の open な pull request がまだ無い。
3. エージェントは VALIDATES THAT issue を閉じる行を本文に持つ pull request を作れる。
4. エージェントは行き先の pull request の番号を控える。
POSTCONDITION: commit は remote に載っている。行き先の pull request が1本ある。pull request の本文に issue を閉じる行がある。

SPECIFIC ALTERNATIVE FLOW 既にあるpullrequestを使う:
RFS BASIC FLOW 2
1. エージェントは既にある pull request を行き先にする。
2. RESUME STEP 4
POSTCONDITION: pull request は1本のままである。push した内容は既にある pull request に入っている。

SPECIFIC ALTERNATIVE FLOW 既にあると断られる:
RFS BASIC FLOW 3
1. エージェントは同じ head branch の pull request を行き先にする。
2. RESUME STEP 4
POSTCONDITION: pull request は1本のままである。エージェントは blocked を表明していない。

SPECIFIC ALTERNATIVE FLOW pullrequestを作れない:
RFS BASIC FLOW 3
1. エージェントは pull request を作れなかった理由を報告のコメントとして issue へ書く。
2. エージェントは応答の最後に判断を仰ぐ表明を1行書く。
3. システムはカンバンの issue の Status に blocked の遷移先を書く。
4. ABORT
POSTCONDITION: pull request は出ていない。commit は remote に載っている。issue に理由の報告のコメントがある。issue の Status は blocked の遷移先である。利用者は理由を読む。利用者が原因を直してから Status を dispatch_state の選択肢へ戻すと、次の run が pull request を出し直す。

GLOBAL ALTERNATIVE FLOW 成果がworktreeの外にある:
BRANCH FROM BASIC FLOW 1
WHEN 信頼できる人間が「コードは別のリポジトリにある」と書いていて、WORKFLOW.md の本文に成果の出し方が書いてある場合
1. エージェントは WORKFLOW.md の本文の指示に従って成果を出す。
2. エージェントは VALIDATES THAT WORKFLOW.md の本文に pull request の出し方が書いてある。
3. エージェントは WORKFLOW.md の本文の指示に従って pull request を出す。
4. RESUME STEP 4
POSTCONDITION: 成果は worktree の外に出ている。pull request がある。worktree の branch に commit は無い。

SPECIFIC ALTERNATIVE FLOW 出し方が書かれていない:
RFS 成果がworktreeの外にある 2
1. エージェントは pull request を出せない理由を報告のコメントとして issue へ書く。
2. エージェントは応答の最後に判断を仰ぐ表明を1行書く。
3. システムはカンバンの issue の Status に blocked の遷移先を書く。
4. ABORT
POSTCONDITION: pull request は出ていない。issue に理由の報告のコメントがある。issue の Status は blocked の遷移先である。利用者は WORKFLOW.md の本文に pull request の出し方を書いてから Status を dispatch_state の選択肢へ戻す。
```

## 成果が worktree の外にあると扱ってよい条件

`builtin.md` の 3-4 が決めている。**2つが両方そろっているときだけ**である。片方でも欠けたら、基本フローのとおり commit して push する。

| 順 | 条件 |
| --- | --- |
| 1 | `trusted_comment` が true のコメントか、`trusted_body` が true の issue の本文に、「コードは別のリポジトリにある」と書いてある（6-1） |
| 2 | `WORKFLOW.md` の本文（4-4）に、その成果の出し方が書いてある（7-4） |

## 段の中で決まっていること

どれも、終わり方も通る段も変えない。

| 段 | 決まり |
| --- | --- |
| 段1 | `git push -u origin HEAD`。`-u` を落とさない。commit するものが無ければ要らない |
| 段1 | 既定の branch（main / master）へ直に push しない。別の名前へ push してよいのは、2本目の pull request を出すときと、信頼できる人間が「この branch へ出せ」と書いたときだけである（6-3） |
| 段3 | 題名も本文も、ファイルへ書いてから渡す。本文に `Closes #<issue の番号>` を入れる |
| 段3 | 別のリポジトリへ出すときは `Closes <owner>/<repo>#<issue の番号>` と書く（7-3） |
| 段3 | draft で作るかどうかと、base にする branch は、`WORKFLOW.md` の本文に書いてあればそれに従う（7-4） |
| 段3 | 設計のレビューを飛ばす断り（`<!-- design-review-skipped -->`）を、自分で書かない |

**止まる2つの出口は、commit して push する段を持たない。**`pullrequestを作れない` は、段1 で push を済ませている。`出し方が書かれていない` は、worktree の branch に commit が1つも無い。

## 相互作用

```mermaid
sequenceDiagram
    participant CC as エージェント
    participant GH as GitHub
    participant S as システム
    actor U as 利用者

    alt 成果がこの worktree にある
        CC->>GH: commit して push する
        CC->>GH: いま居る branch の open な pull request を要求する
        alt 既にある
            CC->>CC: 既にある pull request を行き先にする
        else まだ無い
            CC->>GH: issue を閉じる行を本文に持つ pull request の作成を要求する
            alt 作れた
                GH-->>CC: pull request の URL
            else 同じ head branch の pull request が既にあると断られた
                CC->>CC: その pull request を行き先にする
            else それ以外の理由で作れない
                CC->>GH: 理由を報告のコメントとして issue へ書く
                CC->>S: blocked の表明で turn を終える
                S->>GH: Status に blocked の遷移先を書く
                U->>GH: 原因を直し、Status を dispatch_state へ戻す
            end
        end
    else 成果がこの worktree の外にある
        CC->>GH: WORKFLOW.md の本文の指示に従って成果と pull request を出す
    end
    CC->>CC: 行き先の pull request の番号を控える
```

## フローチャート

```mermaid
flowchart TD
    BS1["1 エージェントは commit して push する"]
    BS2{"2 エージェントは VALIDATES THAT いま居る branch の open な pull request がまだ無い"}
    BS3{"3 エージェントは VALIDATES THAT issue を閉じる行を本文に持つ pull request を作れる"}
    BS4["4 エージェントは行き先の pull request の番号を控える"]
    A1S1["既にあるpullrequestを使う 1 エージェントは既にある pull request を行き先にする"]
    A1S2["既にあるpullrequestを使う 2 RESUME STEP 4"]
    A2S1["既にあると断られる 1 エージェントは同じ head branch の pull request を行き先にする"]
    A2S2["既にあると断られる 2 RESUME STEP 4"]
    A3S1["pullrequestを作れない 1 エージェントは pull request を作れなかった理由を報告のコメントとして issue へ書く"]
    A3S2["pullrequestを作れない 2 エージェントは応答の最後に判断を仰ぐ表明を1行書く"]
    A3S3["pullrequestを作れない 3 システムはカンバンの issue の Status に blocked の遷移先を書く"]
    A3S4(["pullrequestを作れない 4 ABORT"])
    A4S1["成果がworktreeの外にある 1 エージェントは WORKFLOW.md の本文の指示に従って成果を出す"]
    A4S2{"成果がworktreeの外にある 2 エージェントは VALIDATES THAT WORKFLOW.md の本文に pull request の出し方が書いてある"}
    A4S3["成果がworktreeの外にある 3 エージェントは WORKFLOW.md の本文の指示に従って pull request を出す"]
    A4S4["成果がworktreeの外にある 4 RESUME STEP 4"]
    A5S1["出し方が書かれていない 1 エージェントは pull request を出せない理由を報告のコメントとして issue へ書く"]
    A5S2["出し方が書かれていない 2 エージェントは応答の最後に判断を仰ぐ表明を1行書く"]
    A5S3["出し方が書かれていない 3 システムはカンバンの issue の Status に blocked の遷移先を書く"]
    A5S4(["出し方が書かれていない 4 ABORT"])
    BS1 --> BS2
    BS1 -. "WHEN 信頼できる人間が「コードは別のリポジトリにある」と書いていて、WORKFLOW.md の本文に成果の出し方が書いてある場合" .-> A4S1
    BS2 -- はい --> BS3
    BS2 -- いいえ --> A1S1
    BS3 -- はい --> BS4
    BS3 -- いいえ --> A2S1
    BS3 -- いいえ --> A3S1
    A1S1 --> A1S2
    A1S2 -. "戻る" .-> BS4
    A2S1 --> A2S2
    A2S2 -. "戻る" .-> BS4
    A3S1 --> A3S2
    A3S2 --> A3S3
    A3S3 --> A3S4
    A4S1 --> A4S2
    A4S2 -- はい --> A4S3
    A4S2 -- いいえ --> A5S1
    A4S3 --> A4S4
    A4S4 -. "戻る" .-> BS4
    A5S1 --> A5S2
    A5S2 --> A5S3
    A5S3 --> A5S4
    BS4 --> END(["終了"])
```
