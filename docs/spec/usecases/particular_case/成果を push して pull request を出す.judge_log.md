# 判断ログ: 成果を push して pull request を出す

- 対象: `docs/spec/usecases/particular_case/成果を push して pull request を出す.rucm.md`
- 作成日 / 作成モデル: 2026-10-02 / Claude Opus 5.5
- 参照した根拠資料: `internal/prompt/builtin.md`（3-4 / 3-5 / 6-1 / 6-3 / 7-3 / 7-4）

## この記述を作った経緯

`指示書に沿って issue を1件仕上げる` の経路が、pull request の入口の4通りで掛け算になっていたので、人間の決定（経路が掛け算で増える記述は分ける）に従って、ここへ分けた（issue #110）。

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | USE CASE NAME | 成果を push して pull request を出す | 3-4 と 3-5 の2つを含むことを名前に出した。既に在る `本家のリポジトリへ PR を出す` は fork から本家へ出す場合の記述で、「PR を出す」だけだと紛れる。こちらは「成果を push して」で始め、`pull request` と綴った | `builtin.md` の 3-4 と 3-5 | 80% |
| 2 | PRIMARY ACTOR | エージェント | push するのも pull request を作るのもエージェントである | `builtin.md` の 3-5 | 90% |
| 3 | 段2 の偽 | `既にあるpullrequestを使う`。`RESUME STEP 4` | 「1件でも返ったら、それが行き先です。新しく作らないでください」 | `builtin.md` の 3-5 | 90% |
| 4 | 段3 の偽を2本にしたこと | `既にあると断られる`（RESUME）と `pullrequestを作れない`（ABORT） | 「既にある」と断られたら `blocked` を出さない。それ以外の理由なら、理由を書いて `blocked` を出す | `builtin.md` の 7-4 | 90% |
| 5 | 段4（行き先の番号を控える）を置いたこと | 3つの入口が合流する段 | pull request のレビューは、判断票を貼る先の番号が要る。CFG を作る道具は、基本フローが条件ステップで終わると正常に終わる経路を出さないので、合流の段を最後に置いた | `builtin.md` の 3-6、rucm_to_cfg の出力 | 75% |
| 6 | `成果がworktreeの外にある` | 段1 から分岐する任意時点の代替フロー。`RESUME STEP 4` | 2つの条件が両方そろったときだけ、3-4 と 3-5 の代わりに 4-4 に従う | `builtin.md` の 3-4 と 3-5 | 80% |
| 7 | `出し方が書かれていない` | 入れ子。`ABORT` | 「書いていなければ、理由を書いて `blocked` を出してください」 | `builtin.md` の 3-5 | 90% |
| 8 | 止まる2つの出口に commit と push の段が無いこと | 置かない | `pullrequestを作れない` は段1 で push 済み。`出し方が書かれていない` は branch に commit が無い | `builtin.md` の 3-4 と 3-5 | 85% |
| 9 | push 先・題名と本文の渡し方・draft・base を段にしなかったこと | rucm ブロックの外の表に書いた | どれも、終わり方も通る段も変えない | `builtin.md` の 3-4、3-5、6-3、7-3、7-4 | 80% |
| 10 | テストを生成しないこと | 生成しない | Claude Code の中で起きることを、テストから観測する手立てが無い | `scripts/check-rucm.sh` の `[W1]` の説明 | 90% |
