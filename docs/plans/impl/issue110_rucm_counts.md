# ユースケース記述ごとの経路の数（2026-10-02 の実測。issue #110 と issue #57 の作業のあと）

`.cfg.json` の `paths` を数えた。「直す前」は origin/main の同じ名前のファイルの値で、「—」は新しく分けて作った記述である。
「テスト」は、その記述の経路に対応づけたテストの関数の数、「テストの在る経路」は、そのテストが指す経路の数である。

| 記述 | 直す前 | 経路 | end | abort | cycle | 代替フロー | テスト | テストの在る経路 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `particular_case/branch を始末する` | — | 8 | 8 | 0 | 0 | 3 | 8 | 4 |
| `particular_case/continuo を入れる` | 15 | 8 | 1 | 7 | 0 | 7 | 9 | 4 |
| `particular_case/issue の担当を入札で決める` | 15 | 38 | 5 | 33 | 0 | 15 | 16 | 9 |
| `particular_case/issue を1件処理する` | 43 | 59 | 2 | 45 | 12 | 40 | 46 | 40 |
| `particular_case/run を終えて worker を止める` | — | 8 | 2 | 6 | 0 | 5 | 6 | 5 |
| `particular_case/worktree と branch を片付ける` | 35 | 17 | 2 | 15 | 0 | 11 | 17 | 12 |
| `particular_case/まとめて直した issue へ成果を書く` | — | 9 | 9 | 0 | 0 | 4 | 0 | 0 |
| `particular_case/ボードを新規に用意する` | 11 | 2 | 2 | 0 | 0 | 0 | 0 | 0 |
| `particular_case/リポジトリの親 workspace を閉じる` | — | 6 | 6 | 0 | 0 | 3 | 6 | 4 |
| `particular_case/レビューを回す` | — | 15 | 4 | 7 | 4 | 4 | 0 | 0 |
| `particular_case/レートリミットで待って再開する` | 11 | 11 | 1 | 8 | 2 | 8 | 2 | 2 |
| `particular_case/人間に判断を渡す` | 15 | 12 | 1 | 5 | 6 | 10 | 5 | 5 |
| `particular_case/信頼登録の対象を調べる` | — | 26 | 13 | 0 | 13 | 6 | 9 | 4 |
| `particular_case/再起動して実行中の issue を引き継ぐ` | 21 | 50 | 21 | 29 | 0 | 26 | 39 | 22 |
| `particular_case/再起動で pane が残っていない run を扱う` | — | 8 | 1 | 7 | 0 | 7 | 5 | 5 |
| `particular_case/前提が揃っているかを検査する` | 177 | 20 | 1 | 19 | 0 | 11 | 8 | 8 |
| `particular_case/前提の道具を調べて入れる` | — | 18 | 9 | 0 | 9 | 7 | 0 | 0 |
| `particular_case/対象リポジトリを信頼登録する` | 6 | 24 | 1 | 23 | 0 | 18 | 18 | 8 |
| `particular_case/成果を push して pull request を出す` | — | 6 | 4 | 2 | 0 | 5 | 0 | 0 |
| `particular_case/指示書に沿って issue を1件仕上げる` | 74 | 36 | 4 | 22 | 10 | 20 | 0 | 0 |
| `particular_case/指示書の文面を組み立てて送る` | — | 7 | 3 | 4 | 0 | 4 | 3 | 3 |
| `particular_case/既存のボードの Status を割り当てる` | 11 | 32 | 2 | 25 | 5 | 26 | 28 | 18 |
| `particular_case/本家のリポジトリへ PR を出す` | 13 | 9 | 2 | 5 | 2 | 5 | 4 | 3 |
| `particular_case/枠待ちの run の打ち切りを止める` | — | 10 | 1 | 6 | 3 | 8 | 10 | 7 |
| `particular_case/画面に出す文言の言語を決める` | 27 | 27 | 24 | 3 | 0 | 3 | 4 | 2 |
| `particular_case/着手を取り消す` | 34 | 40 | 6 | 34 | 0 | 21 | 72 | 31 |
| `particular_case/表明を読んで Status を動かす` | — | 7 | 1 | 6 | 0 | 6 | 3 | 3 |
| `particular_case/見出し語を1つずつ検査する` | — | 14 | 7 | 0 | 7 | 4 | 15 | 6 |
| `particular_case/設定に書く値を gh から引く` | — | 42 | 42 | 0 | 0 | 7 | 13 | 7 |
| `particular_case/設定ファイルを作る` | 160 | 18 | 4 | 14 | 0 | 16 | 12 | 9 |
| `particular_case/起動時に終わった worktree と孤児 branch を掃除する` | — | 7 | 2 | 5 | 0 | 4 | 2 | 2 |
| `particular_case/進捗報告を書く` | — | 3 | 3 | 0 | 0 | 2 | 0 | 0 |
| `particular_case/配布物を取って置く` | — | 11 | 1 | 10 | 0 | 9 | 11 | 7 |
| `scenario/issue を着手から片付けまで見届ける` | 4 | 8 | 1 | 1 | 6 | 6 | 0 | 0 |
| `scenario/はじめて continuo を動かせるようにする` | 13 | 12 | 1 | 10 | 1 | 3 | 0 | 0 |
| `scenario/夜に機械を落として翌朝に担当を続ける` | 3 | 3 | 2 | 1 | 0 | 2 | 1 | 1 |
| 合計（36本） | 688 | 631 | 199 | 352 | 80 | | 372 | 231 |
