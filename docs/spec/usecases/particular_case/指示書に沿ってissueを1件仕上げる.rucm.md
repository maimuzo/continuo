# ユースケース: 指示書に沿ってissueを1件仕上げる

> **この記述からテストコードは作らない。**
> 段を行うのはエージェント（Claude Code）で、指示書の文面を読んで動く。エージェントを起動して段を通す自動テストは、レートリミットを使い、結果も毎回同じにならない。
> 文面に何が書いてあるかは `test/internal/prompt/` の `TestTemplate_…` が確かめている。
> `check_update_tests.py` が出す `[W1]`（テスト未生成パス）は、ここでは想定どおりである。

## 根拠資料

- `internal/prompt/builtin.md`（エージェントへ送る指示書の本体。**エージェントの段の根拠は、この文書の節番号で書く**）
- `internal/orchestrator/lifecycle.go` の `handleTurnEnd` / `readSignals` / `applySignals` / `decideAfterTurn` / `finishRunClaimed`
- `internal/scaffold/template.go`（`continuo init` が置く `WORKFLOW.md` の雛形。「始める前に読む文書」の節）
- `docs/plans/continuo_design.md#5-3`（組み込みのプロンプトの全文）
- `docs/plans/continuo_design.md#5-3d`（`WORKFLOW.md` の本文に何を書くか）
- `docs/plans/continuo_design.md#5-3i`（PR はエージェントが出す）
- `docs/plans/continuo_design.md#5-3u`（計画のあとで人間の確認を受け、質問が出たらレビューを打ち切る）
- `docs/plans/continuo_design.md#5-3v`（取り込みの衝突のうち、両方を残せるものは解かせる）
- `docs/plans/continuo_design.md#5-3w`（エージェント自身に担当を確かめさせ、最初の push で draft の pull request を作らせる）
- `docs/plans/continuo_design.md#5-3y`（終わりの報告の前に指示を分けさせ、時刻は機械のタイムゾーンで書かせる）
- `docs/plans/continuo_design.md#3-25`（Status を動かす仕組みを、プロンプト頼みにしない）
- `docs/spec/usecases/particular_case/指示書の文面を組み立てて送る.rucm.md`
- `docs/spec/usecases/particular_case/レビューを回す.rucm.md`
- `docs/spec/usecases/particular_case/まとめて直したissueへ成果を書く.rucm.md`
- `docs/spec/usecases/particular_case/進捗報告を書く.rucm.md`
- `docs/spec/usecases/particular_case/成果をpushしてpullrequestを出す.rucm.md`

## この記述は何のためにあるか

**エージェントが指示書をどう使うかを、記録として残すためである。**

**テストは持たない。**指示書に沿って何をするかを決めるのはエージェントであり、
**continuo の仕組みではない。**Claude Code の中で起きることを、テストから観測する手立てが無い
（`claude -p` は使用禁止のため）。`scripts/check-rucm.sh` は
**テストが1本も無いユースケースを `[W1]` の警告として出すが、落とす理由にはしない。**

## この記述の読み方

**1つの run を書いている。**run は、システムが文面を送ってから、エージェントが表明を書いて、システムが run を終えるまでである。

**1回の run では、計画から設計レビューへ進めない**（`builtin.md` の 3-2）。
エージェントは計画のコメントを書いたあと、報告のコメントで人間に訊いて、`blocked` を表明して止まる。
人間が了承をコメントしてから Status を戻すと、次の run が始まる。
**だから基本フローは、計画が了承されたあとの run を書いている。**
計画を書く run は、代替フロー `計画を書いて人間確認で止まる` であり、`ABORT` で終わる。

**`ABORT` は「この run はここで終わる」を意味する。**失敗の意味ではない。
人間が何を見て、何をすると続きが始まるかは、各フローの POSTCONDITION に書いてある。

**前の run が設計レビューより先まで進んでいた場合は、エージェントはその続きから始める**（`builtin.md` の 1「どこまで進んでいたかを判断して、その続きから始めてください」）。
**段15 が見るのは「次に行う段が設計レビューか」である。**前の run がどこまで進んだかではない。
実装のレビューで質問して止まり、見直した計画が了承された run は、実装が既に在っても、段15 が真で設計レビュー（段16）から回し直す（1 の図と 5-6）。
段15 の偽の側に、続きの入口を3本置いた。`実装の続きから始める`（段18 へ）、`pullrequestを出すところから始める`（段19 へ）、`実装レビューの続きから始める`（段21 へ）である。
人間の回答が次にすることを明示していた場合も、エージェントは指示された段から続ける（1 の図と 5-6）。指示された段がこの3つ以外のときは、経路として書いていない。

**5本の記述を取り込んでいる。**経路が掛け算で増えるので分けた（人間の決定）。

| 取り込む場所 | 取り込む記述 | 中身 |
| --- | --- | --- |
| 基本フローの段1 | 指示書の文面を組み立てて送る | システムが文面を作って送る |
| 基本フローの段16 と段21 | レビューを回す | `builtin.md` の 5-6。段16 は計画、段21 は pull request の差分 |
| 基本フローの段19 | 成果をpushしてpullrequestを出す | `builtin.md` の 3-4 と 3-5 |
| 代替フロー `まとめて直した` | まとめて直したissueへ成果を書く | `builtin.md` の 7-2 |
| 代替フロー `進捗報告の間隔を超える` | 進捗報告を書く | `builtin.md` の 5-3 |

**取り込んだ記述の中で run が終わる出口は、取り込んだ段の直後の検証の段で受けている。**段2 は `文面が届かない`、段17 と段22 は `レビューが止まる`、段20 は `pullrequestが出ていない`、代替フロー `進捗報告の間隔を超える` の段2 は `進捗報告を書く前に止まる` である。

**段29〜31（システムが表明を読んで run を終える）は、粗く書いている。**
表明が無いときの促し、`working` の表明、成果のコメントの書かせ直しは、`issueを1件処理する` と `人間に判断を渡す` が受け持つ。ここには書いていない。

## RUCM

```rucm
USE CASE NAME: 指示書に沿ってissueを1件仕上げる
BRIEF DESCRIPTION: システムは指示書の文面をエージェントへ送る。エージェントは issue の担当が自分のままかを確かめる。エージェントは worktree の分岐元を取り込む。エージェントは issue と pull request と関連する記録を読む。エージェントは了承された計画を敵対的レビューに掛ける。エージェントは実装して pull request を出す。エージェントは pull request を敵対的レビューに掛ける。エージェントは受けた指示を実行したかどうかで分ける。エージェントは何をしたかを issue へ書く。エージェントは表明の1行で turn を終える。システムは表明どおりに Status を動かす。
PRECONDITION: システムは常駐している。issue はカンバンの active_states に在る。システムは issue の worktree を用意している。システムはエージェントを起動している。WORKFLOW.md は front matter と本文を持つ。
PRIMARY ACTOR: エージェント
SECONDARY ACTORS: システム、GitHub、利用者
DEPENDENCY: INCLUDE USE CASE 指示書の文面を組み立てて送る、INCLUDE USE CASE レビューを回す、INCLUDE USE CASE 成果をpushしてpullrequestを出す、INCLUDE USE CASE まとめて直したissueへ成果を書く、INCLUDE USE CASE 進捗報告を書く
GENERALIZATION: なし

BASIC FLOW:
1. INCLUDE USE CASE 指示書の文面を組み立てて送る
2. エージェントは VALIDATES THAT 指示書の文面を受け取っている。
3. エージェントは VALIDATES THAT issue の担当が自分であるか、担当者が1人もいない。
4. エージェントは worktree の分岐元の名前を、身元ファイルの base の値、WORKFLOW.md の本文の指定、issue にリンクされた branch、リポジトリの既定 branch の順に見て決める。
5. エージェントは分岐元を remote から取ってくる。
6. エージェントは VALIDATES THAT 取り込むものが在る場合に、取ってきた分岐元をマージできる。
7. エージェントは issue の本文とコメントを、1件残らず JSON で読む。
8. エージェントは issue に紐づく pull request の本文とコメントとレビューを、1件残らず JSON で読む。
9. エージェントは issue から辿れるプランファイルと設計文書と過去の issue と過去の pull request を読む。
10. エージェントは WORKFLOW.md の本文が挙げる文書を読む。
11. エージェントは VALIDATES THAT 読む対象を全部読めている。
12. エージェントは会話と issue のコメントと pull request のコメントから、前の run がどこまで進んだかを判断する。
13. エージェントは VALIDATES THAT 計画の印が付いた自分のコメントが issue にある。
14. エージェントは VALIDATES THAT 人間の了承か、次にすることを明示した回答が、自分の問いより後にある。
15. エージェントは VALIDATES THAT 次に行う段が設計レビューである。
16. INCLUDE USE CASE レビューを回す
17. エージェントは VALIDATES THAT 計画のレビューが収まって終わっている。
18. エージェントは実装する。
19. INCLUDE USE CASE 成果をpushしてpullrequestを出す
20. エージェントは VALIDATES THAT 行き先の pull request がある。
21. INCLUDE USE CASE レビューを回す
22. エージェントは VALIDATES THAT 実装のレビューが収まって終わっている。
23. エージェントは、実装のレビューを回した pull request が draft であり、WORKFLOW.md の本文が draft のまま人間へ渡すと書いていなければ、draft を外す。
24. エージェントは VALIDATES THAT この run が直した issue が担当の issue 1件だけである。
25. エージェントは issue と pull request を読み直して、受けた指示を、実行した指示と、まだ実行していない指示と、後の指示で取り消された指示に分ける。
26. エージェントは何をしたかと、実行した指示以外の指示を、issue へ新しいコメントとして書く。
27. エージェントは応答の最後に完了の表明を1行書く。
28. エージェントはシステムに turn の終わりを Stop hook で知らせる。
29. システムはエージェントの会話の記録から表明を読む。
30. システムはカンバンの issue の Status に表明の値の遷移先を書く。
31. システムは run を終える。
POSTCONDITION: pull request がある。issue に計画のコメントと計画の判断票と成果のコメントがある。成果のコメントに、実行した指示以外の指示か、実行していない指示が無いことの1行がある。pull request に実装の判断票がある。commit は remote に載っている。issue の Status は review の遷移先である。システムは run を終えている。

SPECIFIC ALTERNATIVE FLOW 文面が届かない:
RFS BASIC FLOW 2
1. エージェントは作業を始めない。
2. ABORT
POSTCONDITION: エージェントは指示書に沿った作業を始めていない。システムが変数を展開できなかった場合は、issue の Status は failure_state の選択肢であり、利用者が WORKFLOW.md の本文のテンプレートを直して continuo を再起動してから Status を dispatch_state の選択肢へ戻すと、次の run が始まる。herdr が送信を受け付けなかった場合は、issue を1件処理する の送信の失敗か一時的な送信の失敗のフローが続きを受ける。

SPECIFIC ALTERNATIVE FLOW 別の人が担当になっている:
RFS BASIC FLOW 3
1. エージェントは手元の変更を commit する。
2. エージェントは、いまの担当と、push していない commit の branch と短い hash を、報告のコメントとして issue へ書く。
3. エージェントは応答の最後に判断を仰ぐ表明を1行書く。
4. システムはカンバンの issue の Status に blocked の遷移先を書く。
5. ABORT
POSTCONDITION: エージェントは作業を始めていない。エージェントは push していない。push していない commit は worktree の branch に在る。issue に、いまの担当と commit の在りかの報告のコメントがある。issue の Status は blocked の遷移先である。利用者は報告のコメントで、いまの担当と commit の在りかを読む。

SPECIFIC ALTERNATIVE FLOW 担当を確かめられない:
RFS BASIC FLOW 3
1. エージェントは手元の変更を commit する。
2. エージェントは、担当を確かめられなかったことと、push していない commit の branch と短い hash を、報告のコメントとして issue へ書く。
3. エージェントは応答の最後に判断を仰ぐ表明を1行書く。
4. システムはカンバンの issue の Status に blocked の遷移先を書く。
5. ABORT
POSTCONDITION: エージェントは作業を始めていない。エージェントは push していない。push していない commit は worktree の branch に在る。issue に、担当を確かめられなかったことと commit の在りかの報告のコメントがある。報告のコメントは「担当が移った」と書いていない。issue の Status は blocked の遷移先である。

SPECIFIC ALTERNATIVE FLOW マージが始まる前に断られる:
RFS BASIC FLOW 6
1. エージェントは commit していない変更を commit する。
2. RESUME STEP 5
POSTCONDITION: 前の試行が残した変更が commit されている。エージェントは分岐元をもう一度取り込みに行く。

SPECIFIC ALTERNATIVE FLOW 衝突を解かずに止まる:
RFS BASIC FLOW 6
1. エージェントは衝突した箇所を全部読む。
2. エージェントはマージを取り込む前へ戻す。
3. エージェントは、push していない commit が残っていれば、push する。
4. エージェントは、どのファイルのどこを、なぜ決められなかったかを、報告のコメントとして issue へ書く。
5. エージェントは応答の最後に判断を仰ぐ表明を1行書く。
6. システムはカンバンの issue の Status に blocked の遷移先を書く。
7. ABORT
POSTCONDITION: マージの途中の状態は残っていない。衝突の印が付いたファイルは push されていない。commit は remote に載っている。issue に、決められなかった箇所と理由の報告のコメントがある。issue の Status は blocked の遷移先である。利用者は報告のコメントで、決められなかった箇所と理由を読む。利用者が衝突の解き方をコメントしてから Status を dispatch_state の選択肢へ戻すと、次の run が始まる。

SPECIFIC ALTERNATIVE FLOW 読めない:
RFS BASIC FLOW 11
1. エージェントは読めなかったことを応答の最後に書く。
2. エージェントは commit していない変更を commit して push する。
3. エージェントは応答の最後に判断を仰ぐ表明を1行書く。
4. システムはカンバンの issue の Status に blocked の遷移先を書く。
5. ABORT
POSTCONDITION: エージェントは作業を始めていない。issue の Status は blocked の遷移先である。利用者は応答で何を読めなかったかを読む。利用者が読めるようにしてから Status を dispatch_state の選択肢へ戻すと、次の run が始まる。

SPECIFIC ALTERNATIVE FLOW 計画を書いて人間確認で止まる:
RFS BASIC FLOW 13
1. エージェントは VALIDATES THAT issue に対応する必要がある。
2. エージェントは読んだ記録から、同じことが既に決まっていないかを検索する。
3. エージェントは実装の計画を書く。
4. エージェントは計画の印を付けた計画を issue へコメントする。
5. エージェントは設計レビューへ進めてよいかを報告のコメントで issue に訊く。
6. エージェントは commit していない変更を commit して push する。
7. エージェントは応答の最後に判断を仰ぐ表明を1行書く。
8. システムはカンバンの issue の Status に blocked の遷移先を書く。
9. ABORT
POSTCONDITION: issue に計画のコメントがある。issue に、設計レビューへ進めてよいかを訊く報告のコメントがある。設計レビューは始まっていない。issue の Status は blocked の遷移先である。利用者は計画のコメントを読む。利用者が了承をコメントしてから Status を dispatch_state の選択肢へ戻すと、次の run が設計レビューから続ける。

SPECIFIC ALTERNATIVE FLOW 対応しないと決める:
RFS 計画を書いて人間確認で止まる 1
1. エージェントは対応しない理由を issue へコメントする。
2. エージェントは commit していない変更を commit して push する。
3. エージェントは応答の最後に判断を仰ぐ表明を1行書く。
4. システムはカンバンの issue の Status に blocked の遷移先を書く。
5. ABORT
POSTCONDITION: issue に対応しない理由のコメントがある。エージェントは issue を閉じていない。計画は書かれていない。issue の Status は blocked の遷移先である。利用者は理由を読む。利用者は issue を閉じるか、対応させる指示をコメントしてから Status を dispatch_state の選択肢へ戻す。

SPECIFIC ALTERNATIVE FLOW 了承も回答も無い:
RFS BASIC FLOW 14
1. エージェントは了承も回答も無いことを報告のコメントとして issue へ書く。
2. エージェントは commit していない変更を commit して push する。
3. エージェントは応答の最後に判断を仰ぐ表明を1行書く。
4. システムはカンバンの issue の Status に blocked の遷移先を書く。
5. ABORT
POSTCONDITION: エージェントは設計レビューへ進んでいない。issue に了承も回答も無いことを知らせる報告のコメントがある。issue の Status は blocked の遷移先である。利用者が了承か回答をコメントしてから Status を dispatch_state の選択肢へ戻すと、次の run が続ける。

SPECIFIC ALTERNATIVE FLOW 計画の直しを求められる:
RFS BASIC FLOW 14
1. エージェントは計画を直す。
2. エージェントは直した計画を新しい計画のコメントとして issue へ書く。
3. エージェントは設計レビューへ進めてよいかを報告のコメントで issue にもう一度訊く。
4. エージェントは commit していない変更を commit して push する。
5. エージェントは応答の最後に判断を仰ぐ表明を1行書く。
6. システムはカンバンの issue の Status に blocked の遷移先を書く。
7. ABORT
POSTCONDITION: issue に新しい計画のコメントがある。いちばん新しい計画のコメントが正である。設計レビューは始まっていない。issue の Status は blocked の遷移先である。利用者が了承をコメントしてから Status を dispatch_state の選択肢へ戻すと、次の run が設計レビューから続ける。

SPECIFIC ALTERNATIVE FLOW 回答が次を明示していない:
RFS BASIC FLOW 14
1. エージェントは計画だけを見直す。
2. エージェントは見直した計画を新しい計画のコメントとして issue へ書く。
3. エージェントは設計レビューから回し直してよいかを報告のコメントで issue に訊く。
4. エージェントは commit していない変更を commit して push する。
5. エージェントは応答の最後に判断を仰ぐ表明を1行書く。
6. システムはカンバンの issue の Status に blocked の遷移先を書く。
7. ABORT
POSTCONDITION: issue に新しい計画のコメントがある。エージェントは実装を進めていない。issue の Status は blocked の遷移先である。利用者が了承をコメントしてから Status を dispatch_state の選択肢へ戻すと、次の run が設計レビューから回し直す。

SPECIFIC ALTERNATIVE FLOW 実装の続きから始める:
RFS BASIC FLOW 15
1. エージェントは前の run の commit と会話から、実装がどこまで済んでいるかを確かめる。
2. RESUME STEP 18
POSTCONDITION: エージェントは計画を書き直していない。エージェントは計画のレビューを回し直していない。エージェントは実装の続きから作業している。

SPECIFIC ALTERNATIVE FLOW pullrequestを出すところから始める:
RFS BASIC FLOW 15
1. エージェントは前の run の commit が remote に載っていることを確かめる。
2. RESUME STEP 19
POSTCONDITION: エージェントは実装をやり直していない。エージェントは pull request を出すところから作業している。

SPECIFIC ALTERNATIVE FLOW 実装レビューの続きから始める:
RFS BASIC FLOW 15
1. エージェントは pull request の判断票から、実装のレビューが何周目まで済んでいるかを確かめる。
2. RESUME STEP 21
POSTCONDITION: pull request は1本のままである。エージェントは実装のレビューの続きから作業している。

BOUNDED ALTERNATIVE FLOW レビューが止まる:
RFS BASIC FLOW 17,22
1. システムは run を終える。
2. ABORT
POSTCONDITION: レビューは収まっていない。issue の Status は blocked の遷移先である。利用者は issue の報告のコメントか応答で、止まった理由を読む。利用者が回答をコメントしてから Status を dispatch_state の選択肢へ戻すと、次の run が続ける。

SPECIFIC ALTERNATIVE FLOW pullrequestが出ていない:
RFS BASIC FLOW 20
1. システムは run を終える。
2. ABORT
POSTCONDITION: エージェントは行き先の pull request を控えていない。issue に理由の報告のコメントがある。issue の Status は blocked の遷移先である。pull request を作れなかった場合と、出し方が書かれていない場合は、利用者が原因を直してから Status を dispatch_state の選択肢へ戻すと、次の run が pull request を出すところから続ける。別の人が担当になっていた場合と、担当を確かめられなかった場合は、commit は push されていない。

SPECIFIC ALTERNATIVE FLOW まとめて直した:
RFS BASIC FLOW 24
1. DO
2.   INCLUDE USE CASE まとめて直したissueへ成果を書く
3. UNTIL まとめて直した issue を全部書き終えている
4. エージェントは issue と pull request とまとめて直した issue を読み直して、受けた指示を、実行した指示と、まだ実行していない指示と、後の指示で取り消された指示に分ける。
5. エージェントは、まとめて直した issue へ書いたコメントの URL と、実行した指示以外の指示を並べた成果のコメントを、担当の issue へ新しく書く。
6. エージェントは応答の最後に issue ごとの表明を1行ずつ書く。
7. エージェントはシステムに turn の終わりを Stop hook で知らせる。
8. システムはエージェントの会話の記録から表明を読む。
9. システムは表明が指す issue のうち書ける issue ごとに、カンバンの Status に表明の値の遷移先を書く。
10. システムは run を終える。
11. ABORT
POSTCONDITION: pull request の本文に、直した issue ごとの閉じる行がある。まとめて直した同じリポジトリの issue それぞれに、グループの印が付いた成果報告がある。担当の issue の成果のコメントに、成果報告の URL が並んでいる。表明が指す issue のうちシステムが書けた issue の Status は、それぞれの表明の値の遷移先である。書けなかった issue の Status は変わっていない。別のリポジトリの issue は直されていない。システムは run を終えている。

GLOBAL ALTERNATIVE FLOW 進捗報告の間隔を超える:
BRANCH FROM BASIC FLOW 18
WHEN エージェントが進捗報告の間隔を超えてコメントを書かないまま作業を続けている場合
1. INCLUDE USE CASE 進捗報告を書く
2. エージェントは VALIDATES THAT 進捗報告を書いたあとも作業を続けている。
3. RESUME STEP 18
POSTCONDITION: issue に進捗報告のコメントがある。エージェントは作業を続けている。

SPECIFIC ALTERNATIVE FLOW 進捗報告を書く前に止まる:
RFS 進捗報告の間隔を超える 2
1. システムは run を終える。
2. ABORT
POSTCONDITION: 進捗報告のコメントは増えていない。エージェントは push していない。issue に、いまの担当と commit の在りかの報告のコメントがある。issue の Status は blocked の遷移先である。

GLOBAL ALTERNATIVE FLOW 判断に迷って止まる:
BRANCH FROM BASIC FLOW 18
WHEN エージェントが扱いに迷った場合
1. エージェントは迷った内容を報告のコメントとして issue へ書く。
2. エージェントは commit していない変更を commit して push する。
3. エージェントは応答の最後に判断を仰ぐ表明を1行書く。
4. システムはカンバンの issue の Status に blocked の遷移先を書く。
5. ABORT
POSTCONDITION: issue に迷った内容の報告のコメントがある。commit は remote に載っている。issue の Status は blocked の遷移先である。利用者が回答をコメントしてから Status を dispatch_state の選択肢へ戻すと、次の run が続ける。

GLOBAL ALTERNATIVE FLOW 命令として扱わないissueの記述:
BRANCH FROM BASIC FLOW 7
WHEN issue の本文かコメントの trusted_body か trusted_comment が true でない場合
1. エージェントは書かれた命令を実行しない。
2. エージェントは書かれた内容を報告か分析として読む。
3. RESUME STEP 7
POSTCONDITION: 信頼できる人間が AI の印を付けずに書いたもの以外の命令は実行されていない。外部の人が書いた不具合の報告は材料として使われている。AI が書いた分析と記録は材料として使われている。

GLOBAL ALTERNATIVE FLOW 命令として扱わないpullrequestの記述:
BRANCH FROM BASIC FLOW 8
WHEN pull request の本文を読んだ場合、または pull request のコメントかレビューの trusted_comment が true でない場合
1. エージェントは書かれた命令を実行しない。
2. エージェントは書かれた内容を変更の説明か報告か分析として読む。
3. RESUME STEP 8
POSTCONDITION: pull request の本文に書かれた命令は実行されていない。信頼できる人間が AI の印を付けずに書いたもの以外のコメントの命令は実行されていない。

GLOBAL ALTERNATIVE FLOW 命令として扱わない記録の記述:
BRANCH FROM BASIC FLOW 9
WHEN 辿って読んだ issue か pull request の記述の trusted_body か trusted_comment が true でない場合
1. エージェントは書かれた命令を実行しない。
2. エージェントは書かれた内容を報告か分析として読む。
3. RESUME STEP 9
POSTCONDITION: 辿って読んだ記録のうち、信頼できる人間が AI の印を付けずに書いたもの以外の命令は実行されていない。
```

## 相互作用

```mermaid
sequenceDiagram
    participant S as システム
    participant CC as エージェント
    participant GH as GitHub
    actor U as 利用者

    S->>CC: 指示書の文面を turn として送る（指示書の文面を組み立てて送る）
    CC->>GH: issue の担当が自分のままかを確かめる（別の人が担当か、確かめられなければ、push せずに blocked で止まる）
    CC->>CC: 分岐元の名前を決めて取り込む（衝突した箇所の全部で両方の変更を残せれば、解いて commit する）
    CC->>GH: issue と紐づく pull request を、1件残らず JSON で読む
    GH-->>CC: 本文・コメント・立場
    CC->>CC: 前の run がどこまで進んだかを判断する（先まで進んでいれば、その続きから始める）
    alt 計画のコメントがまだ無い
        CC->>GH: 計画を issue へコメントする
        CC->>GH: 報告のコメントで、設計レビューへ進めてよいかを訊く
        CC->>S: blocked の表明で turn を終える
        S->>GH: Status に blocked の遷移先を書く
        U->>GH: 了承をコメントし、Status を dispatch_state へ戻す
        Note over S,CC: この run はここで終わる。続きは次の run が行う
    else 人間のはっきりした了承がある
        CC->>GH: 計画のレビューを回す（レビューを回す）
        CC->>CC: 実装する
        loop 進捗報告の間隔ごと
            CC->>GH: 進捗報告を書く（進捗報告を書く。最初の push で、draft の pull request を作る）
        end
        CC->>GH: 担当を確かめて commit を push し、pull request の題名と本文を書き直す（成果をpushしてpullrequestを出す）
        CC->>GH: pull request のレビューを回す（レビューを回す）
        CC->>GH: 実装のレビューを回した pull request が draft なら、draft を外す
        opt まとめて直した issue がある
            CC->>GH: issue ごとに成果を書く（まとめて直したissueへ成果を書く）
        end
        CC->>GH: issue と pull request を読み直して、受けた指示を分ける
        CC->>GH: 何をしたかと、実行した指示以外の指示を、issue へコメントする
        CC->>S: review の表明で turn を終える
        S->>S: 会話の記録から表明を読む
        S->>GH: Status に review の遷移先を書く
        S->>S: run を終える
        U->>GH: pull request をレビューする
    end
    opt レビューの途中で止まる
        CC->>S: blocked の表明で turn を終える
        S->>GH: Status に blocked の遷移先を書く
        S->>S: run を終える
    end
```

## 分岐元の名前の決め方

`builtin.md` の 3-1 が決めている。上から順に見て、決まった時点で止める。基本フローの段4 が、この4段である。

| 順 | どこを見るか | 決まらないのはどんなときか |
| --- | --- | --- |
| 1 | worktree の直下にある身元ファイル（既定 `.continuo.json`）の `base` の値 | キーが無い。値が空文字である |
| 2 | `WORKFLOW.md` の本文（4-4）の指定 | 指定が書かれていない |
| 3 | issue にリンクされた branch（`{{.push_branch}}`） | branch がリンクされていない |
| 4 | リポジトリの既定 branch | （ここで必ず決まる） |

決まった名前が `origin/` で始まっていたら、`origin/` を外してから取ってくる。

**その名前が remote に無いとき**（段5 が `couldn't find remote ref` などで落ちたとき）**は、取り込むものが無い。**段6 は何もせずに通り、段7 へ進む。終わり方も、そのあと通る段も変わらないので、代替フローにしていない。

## 担当の確かめ方

`builtin.md` の 3-1 が決めている。**基本フローの段3 は、いちばん最初に確かめる時点である。**同じ見本を、push の前（3-4）と、進捗報告を書く前（5-3）にも叩く。push の前は `成果をpushしてpullrequestを出す` に、進捗報告を書く前は `進捗報告を書く` に在る。

| 見本が出すもの | 段3 では | 作業を終える前の push（3-4）の前では | 途中の push（5-3）の前では |
| --- | --- | --- | --- |
| 「担当は自分です。進めます」 | 進む | push する | 進捗報告を書いて push する |
| 「担当者がいません。進めます」 | 進む | push する | 進捗報告を書いて push する |
| 「担当は … です。… 止まります」 | `別の人が担当になっている`。commit だけして、いまの担当と commit の在りか（branch と短い hash）を報告に書き、`blocked` | 同じ | 同じ。進捗報告も書かない |
| 「確かめられませんでした」 | もう1回だけ叩く。それでも同じなら `担当を確かめられない`。push せず、commit の在りかを報告に書き、`blocked` | 同じ | もう1回だけ叩く。それでも同じなら、今回の push を見送って作業を続け、次の push の前に確かめ直す |

「確かめられませんでした」のときは、報告に「担当が移った」と書かない。確かめられなかっただけである。

## 取り込みが衝突したとき

`builtin.md` の 3-1 が決めている。衝突した箇所を全部読んでから、解くか止まるかを決める。別の branch を取り込むとき（7-1）も同じ扱いである。

| 場合 | 段6 はどう通るか |
| --- | --- |
| 衝突した箇所の全部で、両方の branch の変更をそのまま残せる（同じ場所へ互いに関係の無い行を足しただけ。一覧や `.gitignore` の末尾への追加など） | 真の側。解いて commit し、段7 へ進む |
| 1箇所でも、次のどれかに当たる。両方が同じ行を別の内容へ変えていて、どちらを採るかで動きが変わる。片方が消し、もう片方が変えている。両方を残すと構文か意味が壊れる。変更の意図を差分と commit メッセージから読み取れない。生成されたファイル・ロックファイル・バイナリである | 偽の側。`衝突を解かずに止まる` |
| 迷った | 偽の側。`衝突を解かずに止まる`（5-4） |
| 解いたあとの `git commit` が落ちた | 偽の側。取り込む前へ戻して、`衝突を解かずに止まる` |

**解ける衝突は、代替フローにしていない。**解いて commit すれば、取り込めたことになり、終わり方も、そのあと通る段も変わらない。代替フローにすると、段7 より後ろの全部の経路が2倍になる（分岐元が remote に無い場合と同じ扱いである）。

解く場合にエージェントがすることは、次の4つである。

| 順 | すること |
| --- | --- |
| 1 | 衝突した箇所を全部読む |
| 2 | 衝突した箇所の全部で、両方の branch の変更をそのまま残して解く |
| 3 | `git diff --cached --check` に解いたファイルだけを渡し、`leftover conflict marker` の行を見る。終了コードは見ない。出た行が衝突の印なら、消して `git add` からやり直す。印ではない行（`=======` だけの見出しの下線など）なら、消さずに commit へ進む |
| 4 | `git commit --no-edit` で取り込みを commit する |

どのファイルをどう解いたかは、終わりの報告（基本フローの段26）に書く。

## エージェントが書くコメントと、本文の先頭に置く印

`<!-- continuo:ai -->` は使わない（`builtin.md` の 1）。

| コメント | 書く先 | 本文の先頭の印 | `builtin.md` の節 |
| --- | --- | --- | --- |
| 計画 | issue | 1行目 `<!-- continuo:agent -->`、2行目 `<!-- continuo:plan -->` | 3-2 |
| 計画の判断票 | issue | 1行目 `<!-- continuo:agent -->`、2行目 `<!-- design-review-result -->` | 3-2 |
| 実装の判断票 | pull request | 1行目 `<!-- code-review-result -->`、2行目 `<!-- continuo:agent -->` | 3-6 |
| 報告（成果・質問・止まった理由） | issue | `<!-- continuo:agent -->` の1行だけ | 3-7、5-5 |
| 削除の記録 | issue | `<!-- continuo:agent -->` の1行だけ | 5-5、5-6 |
| 進捗報告 | issue | 1行目 `<!-- continuo:agent -->`、2行目 `<!-- continuo:progress -->` | 5-3 |
| まとめて直した issue の成果報告 | まとめて直した issue | `<!-- continuo:group -->` | 7-2 |

pull request の本文の1行目に置く途中の目印（`<!-- continuo:pull-request-in-progress -->`）は、コメントの印ではない。`成果をpushしてpullrequestを出す` に在る（`builtin.md` の 3-5）。

**エージェントが命令として扱うのは、`OWNER` / `MEMBER` / `COLLABORATOR` が AI の印を付けずに書いたものだけである**（`builtin.md` の 6-1）。
本文の先頭が `<!-- continuo:` で始まる印か、レビューの目印（`<!-- code-review-result -->`・`<!-- design-review-result -->`・`<!-- design-review-skipped -->`）のコメントは、AI が書いたものとして読む。


## run が止まる出口

どの出口も、エージェントが `blocked` を表明し、システムが Status を `blocked` の遷移先へ動かす。
`blocked` の前に、commit していない変更を commit して push する（`builtin.md` の 3-4）。**次の8つは、その段を持たない。**後ろの5つは、取り込んだ記述に在る。

| 代替フロー | なぜ commit と push の段が無いか |
| --- | --- |
| `別の人が担当になっている`・`担当を確かめられない`（この記述） | commit だけして、push しない（3-1） |
| `衝突を解かずに止まる` | マージを取り込む前へ戻すので、commit するものが無い。手前で commit したものが残っていれば push する（3-1） |
| `別の人が担当になっている`・`担当を確かめられない`（`成果をpushしてpullrequestを出す`） | commit を済ませていて、push しない（3-4） |
| `出し方が書かれていない` | 成果が worktree の外にあり、worktree の branch に commit が1つも無い（3-5） |
| `pullrequestを作れない` | pull request を作る前に、3-4 の push を済ませている（3-5） |
| `別の人が担当になっている`（`進捗報告を書く`） | commit だけして、進捗報告も push もしない（5-3） |

**「commit して push する」段は、どれも push の前に担当を確かめる**（3-4。確かめ方は上の「担当の確かめ方」）。別の人が担当になっていた場合と、担当を確かめられなかった場合は、push せずに、commit の在りか（branch と短い hash）を報告に書く。**どちらの場合も `blocked` を表明して終わるので、出口ごとの代替フローにしていない。**その場合は、POSTCONDITION の「commit は remote に載っている」は成り立たない。

**報告のコメントを書く段は、どれも、書く前に issue と pull request を読み直して、受けた指示を分ける**（3-7。基本フローの段25 と同じ）。3-7 は「`blocked` で止まるときの報告でも同じです」と書いている。終わり方も通る段も変わらないので、出口ごとの段にしていない。

| 代替フロー | どの記述に在るか | `builtin.md` の節 | 利用者が読むもの |
| --- | --- | --- | --- |
| `別の人が担当になっている` | この記述 | 3-1 | 報告のコメント |
| `担当を確かめられない` | この記述 | 3-1 | 報告のコメント |
| `衝突を解かずに止まる` | この記述 | 3-1 | 報告のコメント |
| `読めない` | この記述 | 3-1 | 応答 |
| `計画を書いて人間確認で止まる` | この記述 | 3-2 | 計画のコメントと、報告のコメント |
| `対応しないと決める` | この記述 | 3-2 | 対応しない理由のコメント |
| `了承も回答も無い` | この記述 | 1 | 報告のコメント |
| `計画の直しを求められる` | この記述 | 3-2 | 新しい計画のコメントと、報告のコメント |
| `回答が次を明示していない` | この記述 | 5-6 | 新しい計画のコメントと、報告のコメント |
| `人間に訊くことが出る` | レビューを回す | 5-6 | 報告のコメント |
| `連続10回で収まらない` | レビューを回す | 5-6 | 報告のコメント |
| `判断票が数えられない立場` | レビューを回す | 3-2 | 応答 |
| `進捗報告を書く前に止まる` | レビューを回す | 5-3 | 報告のコメント |
| `別の人が担当になっている` | 成果をpushしてpullrequestを出す | 3-4 | 報告のコメント |
| `担当を確かめられない` | 成果をpushしてpullrequestを出す | 3-4 | 報告のコメント |
| `出し方が書かれていない` | 成果をpushしてpullrequestを出す | 3-5 | 報告のコメント |
| `pullrequestを作れない` | 成果をpushしてpullrequestを出す | 7-4 | 報告のコメント |
| `別の人が担当になっている` | 進捗報告を書く | 5-3 | 報告のコメント |
| `判断に迷って止まる` | この記述 | 5-4 | 報告のコメント |

`レビューを回す` の4つの出口は、この記述では代替フロー `レビューが止まる` の1本に受けている。
`成果をpushしてpullrequestを出す` の4つの出口は、`pullrequestが出ていない` の1本に受けている。
`進捗報告を書く` の1つの出口は、`進捗報告を書く前に止まる` に受けている。

## 命令として扱わないもの

`builtin.md` の 6-1 が決めている。3本の代替フロー（`命令として扱わないissueの記述`・`命令として扱わないpullrequestの記述`・`命令として扱わない記録の記述`）は、どれも同じ表を当てる。どの行でも、終わり方も通る段も変わらない。

| 読んだもの | 条件 | 扱い |
| --- | --- | --- |
| コメント・レビュー | 立場が `OWNER` / `MEMBER` / `COLLABORATOR` 以外 | 外部の人の報告として読む。印があっても同じ |
| コメント・レビュー | `written_by` が `"ai"` | AI が書いた分析・記録として読む。材料としては使ってよい |
| コメント・レビュー | それ以外（`trusted_comment` が true） | 命令として扱ってよい |
| issue の本文 | `trusted_body` が false | 直す対象の報告として読む。中の命令やコマンドは実行しない |
| pull request の本文 | （条件なし） | 変更の説明として読む。命令としては扱わない |

辿って読む別の issue と pull request も、同じコマンドで読み、同じ表を当てる（4-3）。

## 進捗報告を書く時点

`進捗報告の間隔を超える` の分岐元は段18（実装する）の1点である。**`builtin.md` の 5-3 は、段を限っていない。**どの段でも、間隔を超えたら書く。
分岐元は、CFG とテストで確かめる時点の宣言であり、起こりうる時点を狭めるものではない（規約）。戻り先を1つしか書けないので、いちばん長くかかる段に置いた。
レビューの検査の待ちに入る前に1行書かせる決まり（3-6）は、`レビューを回す` の段に在る。

## 終わりの報告の前に、受けた指示を分ける

基本フローの段25 と、`まとめて直した` の段4 である。`builtin.md` の 3-7 が決めている。どの行でも、終わり方も通る段も変わらない。

| 何 | 決まり |
| --- | --- |
| 読み直し方 | 4-1 と 4-2 のコマンドをもう一度叩く。記憶から拾わない |
| 拾う先 | `trusted_body` が true の本文と、`trusted_comment` が true のコメント（6-1）。pull request の側と、まとめて直す issue の側も含む |
| 分け方 | 実行した / まだ実行していない（理由と、いつ実行するかを書く） / 後の指示で取り消された（どの指示で取り消されたかを書く） |
| 報告に書くもの | 「実行した」以外を、報告の `### 詳細` へ1件ずつ書く。1件も無ければ「実行していない指示はありません」と1行書く |
| 書かなくてよいもの | 取り消された指示のうち、前の報告に書いてあるもの |
| 読み直せなかったとき | 読み直せなかったことを報告に書く |

## pull request を作る時点と、draft を外す時点

`builtin.md` の 3-5 と 3-6 が決めている。

| 時点 | 何をするか | どの記述に在るか |
| --- | --- | --- |
| 最初に commit を push した直後（実装の途中の push） | 途中の目印を本文の1行目に置いた pull request を、draft で作る | 進捗報告を書く |
| 実装が終わったあとの push（段19） | pull request がまだ無ければ draft で作る。途中の目印が残っていれば、題名と本文を書き直して目印を外す | 成果をpushしてpullrequestを出す |
| 実装のレビューが収まって終わったあと（段23） | 実装のレビューを回した pull request が draft なら、外す。誰が draft で作ったかは問わない | この記述 |

段23 で draft を外さないのは、`WORKFLOW.md` の本文（4-4）に「draft のまま人間へ渡す」とはっきり書いてあるときである（7-4）。「draft で作る」とだけ書いてあるときは、外す。
10周で収まらずに止まったときと、人間に訊くことが出て周を打ち切ったときは、`レビューが止まる` を通るので、段23 へ来ない。

## まとめて直した issue の Status を書かない場合

`まとめて直した` の段9 は、表明の行ごとに `applySignals` が判定する。次の場合は、その issue の Status を書かない。どの場合も、残りの行の処理と run の終わり方は変わらない。

| 場合 | システムがすること |
| --- | --- |
| 表明の値の遷移先が null である（既定では `working`） | Status を動かさない |
| 表明が指す issue がカンバンに載っていない | その行を捨て、担当の issue へ「カンバンに無いので動かせなかった」とコメントする |
| 表明が指す issue を、この機械の別の run が担当している | その行を捨て、担当の issue へコメントする |
| 書き込みに失敗した | WARN を出して、次の行へ進む |
| 表明の値が `status_signal_map` に無い | WARN を出して、その行を無視する |
| 表明が指す issue を引く呼び出しが失敗した | WARN を出して、次の行へ進む |
| その issue の Status が `terminal_states` か `direct_chat_state` の選択肢である | 書かない。人間が動かしたカードの上へ書かないためである（`protectedStates`） |
システムが自分で止める出口（`変数を展開できない`）は `指示書の文面を組み立てて送る` に在り、この記述では `文面が届かない` に受けている。

## フローチャート

```mermaid
flowchart TD
    BS1[["1 INCLUDE USE CASE 指示書の文面を組み立てて送る"]]
    BS2{"2 エージェントは VALIDATES THAT 指示書の文面を受け取っている"}
    BS3{"3 エージェントは VALIDATES THAT issue の担当が自分であるか、担当者が1人もいない"}
    BS4["4 エージェントは worktree の分岐元の名前を、身元ファイルの base の値、WORKFLOW.md の本文の指定、issue にリンクされた branch、リポジトリの既定 branch の順に見て決める"]
    BS5["5 エージェントは分岐元を remote から取ってくる"]
    BS6{"6 エージェントは VALIDATES THAT 取り込むものが在る場合に、取ってきた分岐元をマージできる"}
    BS7["7 エージェントは issue の本文とコメントを、1件残らず JSON で読む"]
    BS8["8 エージェントは issue に紐づく pull request の本文とコメントとレビューを、1件残らず JSON で読む"]
    BS9["9 エージェントは issue から辿れるプランファイルと設計文書と過去の issue と過去の pull request を読む"]
    BS10["10 エージェントは WORKFLOW.md の本文が挙げる文書を読む"]
    BS11{"11 エージェントは VALIDATES THAT 読む対象を全部読めている"}
    BS12["12 エージェントは会話と issue のコメントと pull request のコメントから、前の run がどこまで進んだかを判断する"]
    BS13{"13 エージェントは VALIDATES THAT 計画の印が付いた自分のコメントが issue にある"}
    BS14{"14 エージェントは VALIDATES THAT 人間の了承か、次にすることを明示した回答が、自分の問いより後にある"}
    BS15{"15 エージェントは VALIDATES THAT 次に行う段が設計レビューである"}
    BS16[["16 INCLUDE USE CASE レビューを回す"]]
    BS17{"17 エージェントは VALIDATES THAT 計画のレビューが収まって終わっている"}
    BS18["18 エージェントは実装する"]
    BS19[["19 INCLUDE USE CASE 成果をpushしてpullrequestを出す"]]
    BS20{"20 エージェントは VALIDATES THAT 行き先の pull request がある"}
    BS21[["21 INCLUDE USE CASE レビューを回す"]]
    BS22{"22 エージェントは VALIDATES THAT 実装のレビューが収まって終わっている"}
    BS23["23 エージェントは、実装のレビューを回した pull request が draft であり、WORKFLOW.md の本文が draft のまま人間へ渡すと書いていなければ、draft を外す"]
    BS24{"24 エージェントは VALIDATES THAT この run が直した issue が担当の issue 1件だけである"}
    BS25["25 エージェントは issue と pull request を読み直して、受けた指示を、実行した指示と、まだ実行していない指示と、後の指示で取り消された指示に分ける"]
    BS26["26 エージェントは何をしたかと、実行した指示以外の指示を、issue へ新しいコメントとして書く"]
    BS27["27 エージェントは応答の最後に完了の表明を1行書く"]
    BS28["28 エージェントはシステムに turn の終わりを Stop hook で知らせる"]
    BS29["29 システムはエージェントの会話の記録から表明を読む"]
    BS30["30 システムはカンバンの issue の Status に表明の値の遷移先を書く"]
    BS31["31 システムは run を終える"]
    A1S1["文面が届かない 1 エージェントは作業を始めない"]
    A1S2(["文面が届かない 2 ABORT"])
    A2S1["別の人が担当になっている 1 エージェントは手元の変更を commit する"]
    A2S2["別の人が担当になっている 2 エージェントは、いまの担当と、push していない commit の branch と短い hash を、報告のコメントとして issue へ書く"]
    A2S3["別の人が担当になっている 3 エージェントは応答の最後に判断を仰ぐ表明を1行書く"]
    A2S4["別の人が担当になっている 4 システムはカンバンの issue の Status に blocked の遷移先を書く"]
    A2S5(["別の人が担当になっている 5 ABORT"])
    A3S1["担当を確かめられない 1 エージェントは手元の変更を commit する"]
    A3S2["担当を確かめられない 2 エージェントは、担当を確かめられなかったことと、push していない commit の branch と短い hash を、報告のコメントとして issue へ書く"]
    A3S3["担当を確かめられない 3 エージェントは応答の最後に判断を仰ぐ表明を1行書く"]
    A3S4["担当を確かめられない 4 システムはカンバンの issue の Status に blocked の遷移先を書く"]
    A3S5(["担当を確かめられない 5 ABORT"])
    A4S1["マージが始まる前に断られる 1 エージェントは commit していない変更を commit する"]
    A4S2["マージが始まる前に断られる 2 RESUME STEP 5"]
    A5S1["衝突を解かずに止まる 1 エージェントは衝突した箇所を全部読む"]
    A5S2["衝突を解かずに止まる 2 エージェントはマージを取り込む前へ戻す"]
    A5S3["衝突を解かずに止まる 3 エージェントは、push していない commit が残っていれば、push する"]
    A5S4["衝突を解かずに止まる 4 エージェントは、どのファイルのどこを、なぜ決められなかったかを、報告のコメントとして issue へ書く"]
    A5S5["衝突を解かずに止まる 5 エージェントは応答の最後に判断を仰ぐ表明を1行書く"]
    A5S6["衝突を解かずに止まる 6 システムはカンバンの issue の Status に blocked の遷移先を書く"]
    A5S7(["衝突を解かずに止まる 7 ABORT"])
    A6S1["読めない 1 エージェントは読めなかったことを応答の最後に書く"]
    A6S2["読めない 2 エージェントは commit していない変更を commit して push する"]
    A6S3["読めない 3 エージェントは応答の最後に判断を仰ぐ表明を1行書く"]
    A6S4["読めない 4 システムはカンバンの issue の Status に blocked の遷移先を書く"]
    A6S5(["読めない 5 ABORT"])
    A7S1{"計画を書いて人間確認で止まる 1 エージェントは VALIDATES THAT issue に対応する必要がある"}
    A7S2["計画を書いて人間確認で止まる 2 エージェントは読んだ記録から、同じことが既に決まっていないかを検索する"]
    A7S3["計画を書いて人間確認で止まる 3 エージェントは実装の計画を書く"]
    A7S4["計画を書いて人間確認で止まる 4 エージェントは計画の印を付けた計画を issue へコメントする"]
    A7S5["計画を書いて人間確認で止まる 5 エージェントは設計レビューへ進めてよいかを報告のコメントで issue に訊く"]
    A7S6["計画を書いて人間確認で止まる 6 エージェントは commit していない変更を commit して push する"]
    A7S7["計画を書いて人間確認で止まる 7 エージェントは応答の最後に判断を仰ぐ表明を1行書く"]
    A7S8["計画を書いて人間確認で止まる 8 システムはカンバンの issue の Status に blocked の遷移先を書く"]
    A7S9(["計画を書いて人間確認で止まる 9 ABORT"])
    A8S1["対応しないと決める 1 エージェントは対応しない理由を issue へコメントする"]
    A8S2["対応しないと決める 2 エージェントは commit していない変更を commit して push する"]
    A8S3["対応しないと決める 3 エージェントは応答の最後に判断を仰ぐ表明を1行書く"]
    A8S4["対応しないと決める 4 システムはカンバンの issue の Status に blocked の遷移先を書く"]
    A8S5(["対応しないと決める 5 ABORT"])
    A9S1["了承も回答も無い 1 エージェントは了承も回答も無いことを報告のコメントとして issue へ書く"]
    A9S2["了承も回答も無い 2 エージェントは commit していない変更を commit して push する"]
    A9S3["了承も回答も無い 3 エージェントは応答の最後に判断を仰ぐ表明を1行書く"]
    A9S4["了承も回答も無い 4 システムはカンバンの issue の Status に blocked の遷移先を書く"]
    A9S5(["了承も回答も無い 5 ABORT"])
    A10S1["計画の直しを求められる 1 エージェントは計画を直す"]
    A10S2["計画の直しを求められる 2 エージェントは直した計画を新しい計画のコメントとして issue へ書く"]
    A10S3["計画の直しを求められる 3 エージェントは設計レビューへ進めてよいかを報告のコメントで issue にもう一度訊く"]
    A10S4["計画の直しを求められる 4 エージェントは commit していない変更を commit して push する"]
    A10S5["計画の直しを求められる 5 エージェントは応答の最後に判断を仰ぐ表明を1行書く"]
    A10S6["計画の直しを求められる 6 システムはカンバンの issue の Status に blocked の遷移先を書く"]
    A10S7(["計画の直しを求められる 7 ABORT"])
    A11S1["回答が次を明示していない 1 エージェントは計画だけを見直す"]
    A11S2["回答が次を明示していない 2 エージェントは見直した計画を新しい計画のコメントとして issue へ書く"]
    A11S3["回答が次を明示していない 3 エージェントは設計レビューから回し直してよいかを報告のコメントで issue に訊く"]
    A11S4["回答が次を明示していない 4 エージェントは commit していない変更を commit して push する"]
    A11S5["回答が次を明示していない 5 エージェントは応答の最後に判断を仰ぐ表明を1行書く"]
    A11S6["回答が次を明示していない 6 システムはカンバンの issue の Status に blocked の遷移先を書く"]
    A11S7(["回答が次を明示していない 7 ABORT"])
    A12S1["実装の続きから始める 1 エージェントは前の run の commit と会話から、実装がどこまで済んでいるかを確かめる"]
    A12S2["実装の続きから始める 2 RESUME STEP 18"]
    A13S1["pullrequestを出すところから始める 1 エージェントは前の run の commit が remote に載っていることを確かめる"]
    A13S2["pullrequestを出すところから始める 2 RESUME STEP 19"]
    A14S1["実装レビューの続きから始める 1 エージェントは pull request の判断票から、実装のレビューが何周目まで済んでいるかを確かめる"]
    A14S2["実装レビューの続きから始める 2 RESUME STEP 21"]
    A15S1["レビューが止まる 1 システムは run を終える"]
    A15S2(["レビューが止まる 2 ABORT"])
    A16S1["pullrequestが出ていない 1 システムは run を終える"]
    A16S2(["pullrequestが出ていない 2 ABORT"])
    A17S2[["まとめて直した 2 INCLUDE USE CASE まとめて直したissueへ成果を書く"]]
    A17S3{"まとめて直した 3 UNTIL まとめて直した issue を全部書き終えている"}
    A17S4["まとめて直した 4 エージェントは issue と pull request とまとめて直した issue を読み直して、受けた指示を、実行した指示と、まだ実行していない指示と、後の指示で取り消された指示に分ける"]
    A17S5["まとめて直した 5 エージェントは、まとめて直した issue へ書いたコメントの URL と、実行した指示以外の指示を並べた成果のコメントを、担当の issue へ新しく書く"]
    A17S6["まとめて直した 6 エージェントは応答の最後に issue ごとの表明を1行ずつ書く"]
    A17S7["まとめて直した 7 エージェントはシステムに turn の終わりを Stop hook で知らせる"]
    A17S8["まとめて直した 8 システムはエージェントの会話の記録から表明を読む"]
    A17S9["まとめて直した 9 システムは表明が指す issue のうち書ける issue ごとに、カンバンの Status に表明の値の遷移先を書く"]
    A17S10["まとめて直した 10 システムは run を終える"]
    A17S11(["まとめて直した 11 ABORT"])
    A18S1[["進捗報告の間隔を超える 1 INCLUDE USE CASE 進捗報告を書く"]]
    A18S2{"進捗報告の間隔を超える 2 エージェントは VALIDATES THAT 進捗報告を書いたあとも作業を続けている"}
    A18S3["進捗報告の間隔を超える 3 RESUME STEP 18"]
    A19S1["進捗報告を書く前に止まる 1 システムは run を終える"]
    A19S2(["進捗報告を書く前に止まる 2 ABORT"])
    A20S1["判断に迷って止まる 1 エージェントは迷った内容を報告のコメントとして issue へ書く"]
    A20S2["判断に迷って止まる 2 エージェントは commit していない変更を commit して push する"]
    A20S3["判断に迷って止まる 3 エージェントは応答の最後に判断を仰ぐ表明を1行書く"]
    A20S4["判断に迷って止まる 4 システムはカンバンの issue の Status に blocked の遷移先を書く"]
    A20S5(["判断に迷って止まる 5 ABORT"])
    A21S1["命令として扱わないissueの記述 1 エージェントは書かれた命令を実行しない"]
    A21S2["命令として扱わないissueの記述 2 エージェントは書かれた内容を報告か分析として読む"]
    A21S3["命令として扱わないissueの記述 3 RESUME STEP 7"]
    A22S1["命令として扱わないpullrequestの記述 1 エージェントは書かれた命令を実行しない"]
    A22S2["命令として扱わないpullrequestの記述 2 エージェントは書かれた内容を変更の説明か報告か分析として読む"]
    A22S3["命令として扱わないpullrequestの記述 3 RESUME STEP 8"]
    A23S1["命令として扱わない記録の記述 1 エージェントは書かれた命令を実行しない"]
    A23S2["命令として扱わない記録の記述 2 エージェントは書かれた内容を報告か分析として読む"]
    A23S3["命令として扱わない記録の記述 3 RESUME STEP 9"]
    BS1 --> BS2
    BS2 -- はい --> BS3
    BS2 -- いいえ --> A1S1
    BS3 -- はい --> BS4
    BS3 -- いいえ --> A2S1
    BS3 -- いいえ --> A3S1
    BS4 --> BS5
    BS5 --> BS6
    BS6 -- はい --> BS7
    BS6 -- いいえ --> A4S1
    BS6 -- いいえ --> A5S1
    BS7 --> BS8
    BS7 -. "WHEN issue の本文かコメントの trusted_body か trusted_comment が true でない場合" .-> A21S1
    BS8 --> BS9
    BS8 -. "WHEN pull request の本文を読んだ場合、または pull request のコメントかレビューの trusted_comment が true でない場合" .-> A22S1
    BS9 --> BS10
    BS9 -. "WHEN 辿って読んだ issue か pull request の記述の trusted_body か trusted_comment が true でない場合" .-> A23S1
    BS10 --> BS11
    BS11 -- はい --> BS12
    BS11 -- いいえ --> A6S1
    BS12 --> BS13
    BS13 -- はい --> BS14
    BS13 -- いいえ --> A7S1
    BS14 -- はい --> BS15
    BS14 -- いいえ --> A9S1
    BS14 -- いいえ --> A10S1
    BS14 -- いいえ --> A11S1
    BS15 -- はい --> BS16
    BS15 -- いいえ --> A12S1
    BS15 -- いいえ --> A13S1
    BS15 -- いいえ --> A14S1
    BS16 --> BS17
    BS17 -- はい --> BS18
    BS17 -- いいえ --> A15S1
    BS18 --> BS19
    BS18 -. "WHEN エージェントが進捗報告の間隔を超えてコメントを書かないまま作業を続けている場合" .-> A18S1
    BS18 -. "WHEN エージェントが扱いに迷った場合" .-> A20S1
    BS19 --> BS20
    BS20 -- はい --> BS21
    BS20 -- いいえ --> A16S1
    BS21 --> BS22
    BS22 -- はい --> BS23
    BS22 -- いいえ --> A15S1
    BS23 --> BS24
    BS24 -- はい --> BS25
    BS24 -- いいえ --> A17S2
    BS25 --> BS26
    BS26 --> BS27
    BS27 --> BS28
    BS28 --> BS29
    BS29 --> BS30
    BS30 --> BS31
    A1S1 --> A1S2
    A2S1 --> A2S2
    A2S2 --> A2S3
    A2S3 --> A2S4
    A2S4 --> A2S5
    A3S1 --> A3S2
    A3S2 --> A3S3
    A3S3 --> A3S4
    A3S4 --> A3S5
    A4S1 --> A4S2
    A4S2 -. "戻る" .-> BS5
    A5S1 --> A5S2
    A5S2 --> A5S3
    A5S3 --> A5S4
    A5S4 --> A5S5
    A5S5 --> A5S6
    A5S6 --> A5S7
    A6S1 --> A6S2
    A6S2 --> A6S3
    A6S3 --> A6S4
    A6S4 --> A6S5
    A7S1 -- はい --> A7S2
    A7S1 -- いいえ --> A8S1
    A7S2 --> A7S3
    A7S3 --> A7S4
    A7S4 --> A7S5
    A7S5 --> A7S6
    A7S6 --> A7S7
    A7S7 --> A7S8
    A7S8 --> A7S9
    A8S1 --> A8S2
    A8S2 --> A8S3
    A8S3 --> A8S4
    A8S4 --> A8S5
    A9S1 --> A9S2
    A9S2 --> A9S3
    A9S3 --> A9S4
    A9S4 --> A9S5
    A10S1 --> A10S2
    A10S2 --> A10S3
    A10S3 --> A10S4
    A10S4 --> A10S5
    A10S5 --> A10S6
    A10S6 --> A10S7
    A11S1 --> A11S2
    A11S2 --> A11S3
    A11S3 --> A11S4
    A11S4 --> A11S5
    A11S5 --> A11S6
    A11S6 --> A11S7
    A12S1 --> A12S2
    A12S2 -. "戻る" .-> BS18
    A13S1 --> A13S2
    A13S2 -. "戻る" .-> BS19
    A14S1 --> A14S2
    A14S2 -. "戻る" .-> BS21
    A15S1 --> A15S2
    A16S1 --> A16S2
    A17S2 --> A17S3
    A17S3 -- はい --> A17S4
    A17S3 -. "繰り返す" .-> A17S2
    A17S4 --> A17S5
    A17S5 --> A17S6
    A17S6 --> A17S7
    A17S7 --> A17S8
    A17S8 --> A17S9
    A17S9 --> A17S10
    A17S10 --> A17S11
    A18S1 --> A18S2
    A18S2 -- はい --> A18S3
    A18S2 -- いいえ --> A19S1
    A18S3 -. "戻る" .-> BS18
    A19S1 --> A19S2
    A20S1 --> A20S2
    A20S2 --> A20S3
    A20S3 --> A20S4
    A20S4 --> A20S5
    A21S1 --> A21S2
    A21S2 --> A21S3
    A21S3 -. "戻る" .-> BS7
    A22S1 --> A22S2
    A22S2 --> A22S3
    A22S3 -. "戻る" .-> BS8
    A23S1 --> A23S2
    A23S2 --> A23S3
    A23S3 -. "戻る" .-> BS9
    BS31 --> END(["終了"])
```
