# 判断ログ: 起動時に終わった worktree と孤児 branch を掃除する

- 対象: `docs/spec/usecases/particular_case/起動時に終わった worktree と孤児 branch を掃除する.rucm.md`
- 作成日 / 作成モデル: 2026-10-02 / Claude Opus 5.5。`再起動して実行中の issue を引き継ぐ` から、起動時の掃除の段を分けて作った
- 参照した根拠資料: `docs/plans/continuo_design.md`（3-9 / 3-4）、`internal/daemon/daemon.go`、`internal/orchestrator/sweep.go`、`internal/orchestrator/lifecycle.go`、`internal/orchestrator/restore.go`、`internal/workspace/sweep.go`

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | 配置先ディレクトリ | `particular_case/` | 起動のたびに1回だけ走る、掃除という1つの処理である | `internal/orchestrator/sweep.go` の `SweepOnStartup` | 95% |
| 2 | `再起動して実行中の issue を引き継ぐ` から分けたこと | 別の記述にし、`INCLUDE USE CASE` で引かせる | 掃除は設定の値で3通りに分かれ、どの結果でも起動は続く。引き継ぎの判断と1本に書くと、引き継ぎの結末の数と掃除の3通りの掛け算で経路が増える（90本になった）。掃除の結果は引き継ぎの判断に影響しないので、掛け合わせた経路に新しい情報は無い。人間の決定（2026-10-02） | `internal/daemon/daemon.go` の `Run`、`internal/orchestrator/sweep.go` の `SweepOnStartup` | 95% |
| 3 | USE CASE NAME | 起動時に終わった worktree と孤児 branch を掃除する | 動詞で終わる名詞句にする規約に合わせた。実装がやることの2つ（終わった issue の worktree の片付けと、孤児 branch の削除）と、いつ走るか（起動時）を名前に入れた。`worktree と branch を片付ける` は issue 1件の片付けなので、契機の違いが名前で分かるようにした | `internal/orchestrator/sweep.go` の `SweepOnStartup` のコメント（手順6 と 6b） | 85% |
| 4 | PRIMARY ACTOR と基本フロー 1 | 利用者。利用者はシステムに continuo の常駐の開始を要求する | 掃除を起こすのは起動であり、起動するのは利用者である。巡回タイマーではない（巡回はまだ始まっていない） | `internal/daemon/daemon.go` の `Run` | 85% |
| 5 | PRECONDITION | 利用者は continuo を起動している。システムは起動時の復元を終えている | 実装は復元のあとにしか掃除を呼ばない。先に走らせると、これから引き継ぐ run の branch を孤児と判定して消す | `internal/daemon/daemon.go` の `Run` | 100% |
| 6 | 基本フロー 2 と、代替フロー 掃除の無効 | cleanup.enabled と cleanup.sweep_on_startup がどちらも真である。偽なら記録に残して ABORT | 連言である。片方でも偽なら、実装はログを1行出してその場で返る。呼び出し元の起動は続くが、この記述はそこで終わるので ABORT にした（ABORT は正常な終わり方を含む） | `internal/orchestrator/sweep.go` の `SweepOnStartup` | 95% |
| 7 | 基本フロー 4 と、代替フロー 一覧の取得の失敗 | 一覧を応答する。偽なら理由を記録して worktree の片付けを飛ばし、`RESUME STEP 6` | 実装は一覧を取れなければ WARN を出して worktree を全部残し、孤児 branch の掃除へ進む。次に行う段は `cleanup.delete_branch` の判定である | `internal/orchestrator/sweep.go` の `sweepFinishedWorktrees`、`SweepOnStartup` | 95% |
| 8 | 基本フロー 5 | 一覧に在って印の集合に入っていない issue の worktree を片付ける | 印に入っている worktree は実行中の照合が見るので触らない。身元ファイルを読めない worktree と、一覧に無い issue の worktree も残す | `internal/orchestrator/sweep.go` の `sweepFinishedWorktrees` | 95% |
| 9 | 基本フロー 6 と、代替フロー branchを残す設定 | cleanup.delete_branch が真である。偽なら記録に残して ABORT | 片付けが設定を見て残した branch は掃除の3条件を全部満たす。設定を見ない掃除は、次に起動しただけでその branch を強制削除で消す。壊れた ref だけは消す、という例外も作らない | `internal/workspace/sweep.go` の `SweepOrphanBranches` | 95% |
| 10 | 基本フロー 7 と、代替フロー 接頭辞を決められない | branch_template から接頭辞を決められる。偽なら記録に残して ABORT | テンプレートに変数が1つも無いと接頭辞が空になり、全部の branch が対象になる。実装は WARN を出して1本も消さない | `internal/workspace/sweep.go` の `SweepOrphanBranches` | 90% |
| 11 | 基本フロー 8 と 9 | 片付けずに残った worktree が属するリポジトリで、3条件を満たす branch を消し、消した branch の名前を記録に残す | 掃除が見るリポジトリは、片付けずに残った worktree から引く。残った worktree が1件も無い起動では、どのリポジトリも見ず、1本も消さない。基本フローを実装に在る段で終えた。消した branch が1本も無ければ、記録は残さない | `internal/orchestrator/sweep.go` の `SweepOnStartup`（`Worktrees: remaining`）、`internal/workspace/sweep.go` の `repoDirsOf`、`sweepRepoBranches` | 90% |
| 12 | 基本フローの POSTCONDITION | 「片付けの条件を満たす worktree は消えている」と条件を付けた | 一覧の取得の失敗 から戻った経路では、片付けを1件も行っていない。条件を付けておけば、その経路でも偽にならない | `internal/orchestrator/sweep.go` の `sweepFinishedWorktrees` | 80% |
| 13 | 経路に出さなかった失敗 | リポジトリごとの失敗（branch の一覧を引けない、など）と、片付けの条件を満たさない worktree | どれも WARN を出して次へ進み、後ろの段が変わらない | `internal/workspace/sweep.go` の `sweepRepoBranches`、`repoDirsOf` | 80% |
