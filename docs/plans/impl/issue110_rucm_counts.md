# ユースケース記述ごとの経路の数（2026-10-02 の実測。issue #110 と issue #57 の作業のあと）

`.cfg.json` の `paths` を数えた。「直す前」は origin/main の同じ名前のファイルの値で、「—」は新しく分けて作った記述である。
「テスト」は、その記述の経路に対応づけたテストの関数の数、「テストの在る経路」は、そのテストが指す経路の数である。

| 記述 | 直す前 | 経路 | end | abort | cycle | 代替フロー | テスト | テストの在る経路 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `particular_case/Keychainの読み取りを許可する` | — | 7 | 1 | 6 | 0 | 6 | 10 | 7 |
| `particular_case/branchを始末する` | — | 9 | 9 | 0 | 0 | 4 | 8 | 4 |
| `particular_case/continuoを入れる` | 15 | 8 | 1 | 7 | 0 | 7 | 9 | 4 |
| `particular_case/directchatのpaneを用意する` | — | 20 | 1 | 19 | 0 | 7 | 25 | 18 |
| `particular_case/issueの担当を入札で決める` | 15 | 38 | 5 | 33 | 0 | 15 | 16 | 9 |
| `particular_case/issueを1件処理する` | 43 | 61 | 2 | 47 | 12 | 42 | 48 | 41 |
| `particular_case/runを終えてworkerを止める` | — | 8 | 2 | 6 | 0 | 5 | 8 | 6 |
| `particular_case/worktreeとbranchを片付ける` | 35 | 17 | 2 | 15 | 0 | 11 | 21 | 12 |
| `particular_case/まとめて直したissueへ成果を書く` | — | 9 | 9 | 0 | 0 | 4 | 0 | 0 |
| `particular_case/リポジトリの親workspaceを閉じる` | — | 6 | 6 | 0 | 0 | 3 | 6 | 4 |
| `particular_case/レビューを回す` | — | 15 | 4 | 7 | 4 | 4 | 0 | 0 |
| `particular_case/レートリミットで待って再開する` | 11 | 11 | 1 | 8 | 2 | 8 | 2 | 2 |
| `particular_case/人間がpaneに入って直接続ける` | — | 15 | 4 | 11 | 0 | 6 | 14 | 8 |
| `particular_case/人間に判断を渡す` | 15 | 12 | 1 | 5 | 6 | 10 | 5 | 5 |
| `particular_case/信頼登録の対象を調べる` | — | 26 | 13 | 0 | 13 | 6 | 9 | 4 |
| `particular_case/再起動して実行中のissueを引き継ぐ` | 21 | 48 | 21 | 27 | 0 | 24 | 37 | 20 |
| `particular_case/再起動でpaneが残っていないrunを扱う` | — | 8 | 1 | 7 | 0 | 7 | 5 | 5 |
| `particular_case/前提が揃っているかを検査する` | 177 | 20 | 1 | 19 | 0 | 11 | 11 | 11 |
| `particular_case/前提の道具を調べて入れる` | — | 18 | 9 | 0 | 9 | 7 | 6 | 6 |
| `particular_case/対象リポジトリを信頼登録する` | 6 | 24 | 1 | 23 | 0 | 18 | 19 | 8 |
| `particular_case/巡回が回っているあいだに常駐を止める` | — | 2 | 1 | 1 | 0 | 1 | 2 | 2 |
| `particular_case/成果をpushしてpullrequestを出す` | — | 6 | 4 | 2 | 0 | 5 | 0 | 0 |
| `particular_case/指示書に沿ってissueを1件仕上げる` | 74 | 36 | 4 | 22 | 10 | 20 | 0 | 0 |
| `particular_case/指示書の文面を組み立てて送る` | — | 7 | 3 | 4 | 0 | 4 | 3 | 3 |
| `particular_case/既存のボードのStatusを割り当てる` | 11 | 32 | 2 | 25 | 5 | 26 | 28 | 18 |
| `particular_case/本家のリポジトリへPRを出す` | 13 | 13 | 2 | 7 | 4 | 7 | 4 | 3 |
| `particular_case/枠待ちのrunの打ち切りを止める` | — | 10 | 1 | 6 | 3 | 8 | 10 | 7 |
| `particular_case/画面に出す文言の言語を決める` | 27 | 27 | 24 | 3 | 0 | 3 | 4 | 2 |
| `particular_case/着手を取り消す` | 34 | 40 | 6 | 34 | 0 | 21 | 72 | 31 |
| `particular_case/表明を読んでStatusを動かす` | — | 7 | 1 | 6 | 0 | 6 | 3 | 3 |
| `particular_case/見出し語を1つずつ検査する` | — | 14 | 7 | 0 | 7 | 4 | 15 | 6 |
| `particular_case/設定に書く値をghから引く` | — | 42 | 42 | 0 | 0 | 7 | 13 | 7 |
| `particular_case/設定ファイルを作る` | 160 | 19 | 4 | 15 | 0 | 16 | 12 | 9 |
| `particular_case/起動時に終わったworktreeと孤児branchを掃除する` | — | 7 | 2 | 5 | 0 | 4 | 2 | 2 |
| `particular_case/進捗報告を書く` | — | 3 | 3 | 0 | 0 | 2 | 0 | 0 |
| `particular_case/配布物を取って置く` | — | 13 | 1 | 12 | 0 | 11 | 12 | 8 |
| `scenario/issueを着手から片付けまで見届ける` | 4 | 12 | 1 | 4 | 7 | 10 | 0 | 0 |
| `scenario/はじめてcontinuoを動かせるようにする` | 13 | 16 | 1 | 14 | 1 | 4 | 0 | 0 |
| `scenario/夜に機械を落として翌朝に担当を続ける` | 3 | 3 | 2 | 1 | 0 | 2 | 1 | 1 |
| 合計（39本） | 677 | 689 | 205 | 401 | 83 | | 440 | 276 |

消した記述 `ボードを新規に用意する`（直す前の経路 11本）は、表に無い。直す前の合計 677 は、それを除いた数である（含めると 688）。
