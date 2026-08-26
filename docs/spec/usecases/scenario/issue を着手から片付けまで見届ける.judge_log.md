# 判断ログ: issue を着手から片付けまで見届ける

- 対象: `docs/spec/usecases/scenario/issue を着手から片付けまで見届ける.rucm.md`
- 作成日 / 作成モデル: 2026-08-20 / Claude Opus 5 (1M context)
- 参照した根拠資料: `docs/plans/continuo_design.md`（3-5 / 3-8 / 3-9 / 3-16 / 3-25 / 3-27 / 4-1）、`docs/spec/usecases/particular_case/` の5件、`internal/orchestrator/orchestrator.go`

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | USE CASE NAME | issue を着手から片付けまで見届ける | 依頼が例に挙げた「issue がボードに載ってから片付くまで」を、**動詞で終わる名詞句**の規則に合わせて言い直した | 依頼文、rucm スキルのテンプレート | 85% |
| 2 | 配置ディレクトリ | `scenario/` | 5件の particular_case を跨ぐ目的達成の時系列である | rucm スキルの粒度ガイド | 100% |
| 3 | 凝集の方針 | **5件すべての particular_case を1本に取り込む** | 「1つの scenario になるべく多くの particular_case をまとめることを高価値とする」に従った。**枠待ちと再起動は基本フローに置けないので任意時点代替フローで取り込んだ** | rucm スキルの scenario の設計方針 | 90% |
| 4 | BRIEF DESCRIPTION | 載せる・処理する・レビューする・片付ける の4文 | 中核の価値経路を4文へ落とした | `docs/plans/continuo_design.md#4-1` | 90% |
| 5 | PRECONDITION | 使い始める用意を終えている。常駐している。選択肢名が一致する。信頼登録が済んでいる。herdr が待ち受けている | 「使い始める用意」は既存の scenario（`はじめて continuo を動かせるようにする`）が受け持つ。**そこを事前条件にすることで2本の scenario が繋がる** | `docs/spec/usecases/scenario/はじめて continuo を動かせるようにする.rucm.md` | 90% |
| 6 | PRIMARY ACTOR | 利用者 | この時系列を始めるのは、issue を作ってボードに載せる人間である | `docs/plans/continuo_design.md#4-1` | 95% |
| 7 | SECONDARY ACTORS | GitHub Projects v2、herdr、Claude Code、Claude の usage API、git | 取り込んだ5件の particular_case の副アクターの和集合である | 取り込んだ5件の rucm ブロック | 85% |
| 8 | DEPENDENCY | 5件の INCLUDE を読点区切りで列挙する | DEPENDENCY とステップ内の INCLUDE 集合を一致させる決まりである | rucm スキルの scenario の書き方 | 100% |
| 9 | GENERALIZATION | なし | 汎化関係にあるユースケースが無い | - | 90% |
| 10 | 利用者がボードを直接操作すること | ステップ1 から4 と7 から9 を、利用者からボードへの直接の操作として書く | **R3（アクター間の直接相互作用の禁止）から外れる。**人間は GitHub の画面でボードを触るのであって、continuo に要求しない。**システムに仲介させて書くと、実在しない自動化を記述することになる。**同じ判断を `人間に判断を渡す` と `対象リポジトリを信頼登録する` でも行っている | `docs/plans/continuo_design.md#4-1`、`docs/spec/usecases/particular_case/対象リポジトリを信頼登録する.rucm.md` | 60% |
| 11 | ステップ3 を置いたこと | ボードに載せた直後は `Ice Box` を書く | `Ice Box` を未着手の置き場として使うと人間が決めている。**continuo はボードに載っていない issue を見ない** | `docs/plans/continuo_design.md#4-1` | 95% |
| 12 | ステップ5 | INCLUDE USE CASE issue を1件処理する | 着手から表明までは1件の particular_case で完結している。**直接ステップで書き直すと同じ内容が2箇所になる** | `docs/spec/usecases/particular_case/issue を1件処理する.rucm.md` | 95% |
| 13 | ステップ6（VALIDATES THAT） | issue の Status が review の遷移先の選択肢である | **INCLUDE の直後に条件ステップを置いた。**ここが「レビューへ進む」と「判断を仰ぐ」の分岐点であり、`RFS` の分岐元にできる条件ステップが要る | rucm スキルの厳密規則6（RFS の対象は条件ステップである） | 90% |
| 14 | ステップ7 と8 を分けたこと | コメントを読むこととレビューすることを別ステップにした | R4（1文1動作）。**成果の記録は issue のコメントにあり、変更そのものは branch にある**ので、読む対象が違う | `docs/plans/continuo_design.md#3-25`、`#3-9` の手順2b | 85% |
| 15 | ステップ10 | INCLUDE USE CASE worktree と branch を片付ける | 人間が `Done` へ動かしたことは巡回の worktree の照合で拾う。**その照合が片付けの particular_case である** | `docs/plans/continuo_design.md#3-9` の手順7 | 95% |
| 16 | 基本フローの POSTCONDITION | Status が terminal_states。コメントがある。worktree が無い。branch が無い。印が外れている。pane が閉じている | 1件の issue が片付いた状態を、外側に残るもので書いた | `docs/plans/continuo_design.md#3-9` | 90% |
| 17 | フロー `判断の依頼` | RFS BASIC FLOW 6。`人間に判断を渡す` を INCLUDE して RESUME STEP 5 | 表明が `review` にならなかった場合の分岐である。**人間が回答して `Ready` へ戻すと、同じ issue がもう一度処理される** | `docs/plans/continuo_design.md#4-1`、`docs/spec/usecases/particular_case/人間に判断を渡す.rucm.md` | 90% |
| 18 | `判断の依頼` の RESUME 先 | RESUME STEP 5（処理をもう一度通す） | `人間に判断を渡す` は Status を `dispatch_state` へ戻したところで終わる。**その先は「もう一度 issue を処理する」であり、ステップ5 に戻るのが正しい** | `docs/spec/usecases/particular_case/人間に判断を渡す.rucm.md` の POSTCONDITION | 85% |
| 19 | フロー `枠の上限` の種別と BRANCH FROM | GLOBAL ALTERNATIVE FLOW。BRANCH FROM BASIC FLOW 5 | 枠を使い切るのは処理のどの時点でも起こりうる。**分岐元はステップ5（issue を処理している時点）である。**枠待ちが起こりうるのはこのステップの中だけであり、ほかの時点では走行中の run が無い | `docs/plans/continuo_design.md#3-27` | 80% |
| 20 | フロー `常駐の再起動` の種別と BRANCH FROM | GLOBAL ALTERNATIVE FLOW。BRANCH FROM BASIC FLOW 5 | プロセスが落ちるのはどの時点でも起こりうる。**分岐元はステップ5 である。**処理の途中で落ちたときだけ「引き継ぐ」が成立する（それ以外の時点では引き継ぐ run が無い） | `docs/plans/continuo_design.md#3-4` | 80% |
| 21 | 2つの任意時点代替フローが同じ分岐元を指すこと | どちらも BRANCH FROM BASIC FLOW 5 | 分岐元は「フローごとに原則1点」であり、**別々のフローが同じ点を指すことは制限されていない**。どちらも「走行中の run がある時点」でしか起こらない | rucm スキルの BRANCH FROM 規約 | 80% |
| 22 | `枠の上限` と `常駐の再起動` の RESUME 先 | どちらも RESUME STEP 5 | 取り込んだ particular_case はどちらも「run が続いている」状態で終わる。**続きは同じ処理である** | `docs/spec/usecases/particular_case/レートリミットで待って再開する.rucm.md`、`docs/spec/usecases/particular_case/再起動して実行中の issue を引き継ぐ.rucm.md` の POSTCONDITION | 75% |
| 23 | 直接ステップと INCLUDE の使い分け | 人間の操作は直接ステップ、システムの一連の処理は INCLUDE | 人間の操作は particular_case にすると1ステップだけのユースケースが増える。**INCLUDE で文脈が飛ぶ箇所は直接ステップで書いてよい**と設計方針が定めている | rucm スキルの scenario の設計方針 | 85% |
| 24 | ステップ11 を置いたこと | 片付けの完了をログで応答する | INCLUDE で終わると、この scenario が「どこで終わったか」を人間が観測できない。**ログを事後条件の確認点にした** | `docs/plans/continuo_design.md#3-9` | 70% |
| 25 | 表を2つ足したこと | 「Status がどう動くか」と「凝集させた particular_case」を本文の表にした | 前者は誰がどの遷移を起こすかの一覧、後者はモード D の手順5 の記録である | `docs/plans/continuo_design.md#4-1`、rucm スキルのモード D | 85% |
| 26 | 溢れた particular_case の扱い | 既存の5件（`ボードを新規に用意する` / `設定ファイルを作る` / `既存のボードの Status を割り当てる` / `対象リポジトリを信頼登録する` / `前提が揃っているかを検査する`）は**この scenario に取り込まない** | 5件はすべて `はじめて continuo を動かせるようにする` が既に取り込んでいる。**同じ particular_case を2本の scenario に重ねて取り込むと、どちらがその経路の正なのかが決まらない**。時系列としても「使い始めるまで」と「使い始めたあと」で切れている | `docs/spec/usecases/scenario/はじめて continuo を動かせるようにする.rucm.md`、rucm スキルのモード D の手順5 | 85% |
| 27 | 抜けている particular_case の有無 | **追加は要らない**と判断した | この scenario のステップのうち INCLUDE でない5つは、すべて人間がボードを操作するだけの1動作であり、テストは「ボードの Status とコメントを確かめる」で書ける。**曖昧なステップは残っていない** | rucm スキルのモード D の手順3 と手順5 の (a) | 75% |
