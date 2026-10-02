# 判断ログ: 再起動で pane が残っていない run を扱う

- 対象: `docs/spec/usecases/particular_case/再起動で pane が残っていない run を扱う.rucm.md`
- 作成日 / 作成モデル: 2026-10-02 / Claude Opus 5.5。`再起動して実行中の issue を引き継ぐ` の代替フロー `paneの不在` から、扱いの6通りを分けて作った
- 参照した根拠資料: `docs/plans/continuo_design.md`（3-4 / 3-29）、`internal/orchestrator/restore.go`、`internal/orchestrator/comment.go`

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | 配置先ディレクトリ | `particular_case/` | pane の無い run 1件の扱いを決める、1つの処理である | `internal/orchestrator/restore.go` の `restoreWithoutPane` | 95% |
| 2 | `再起動して実行中の issue を引き継ぐ` から分けたこと | 別の記述にし、代替フロー `paneの不在` から `INCLUDE USE CASE` で引かせる | 扱いは取り直した Status と設定の値で6通りに分かれ、どれも起動を止めない。呼び出し元に書くと代替フローの入れ子が3段になり、戻り先を親の終端へ2回たどる形になる。分けると、6通りが基本フローの検証から1段か2段で読め、呼び出し元の経路は6本から1本に減る（38本から33本）。実装も `restoreWithoutPane` という1つの関数に閉じている | `internal/orchestrator/restore.go` の `Restore`、`restoreWithoutPane` | 85% |
| 3 | USE CASE NAME | 再起動で pane が残っていない run を扱う | 動詞で終わる名詞句にする規約に合わせた。いつ（再起動）、何を（pane が残っていない run）を名前に入れた | `internal/orchestrator/restore.go` の `restoreWithoutPane` のコメント | 80% |
| 4 | PRIMARY ACTOR と基本フロー 1 | 利用者。利用者はシステムに continuo の常駐の開始を要求する | この扱いを起こすのは起動であり、起動するのは利用者である | `internal/daemon/daemon.go` の `Run` | 85% |
| 5 | PRECONDITION | 起動し直している。身元ファイルを持つ worktree がある。その worktree を cwd に持つ pane が無い。溜めた hook の配送を始めている | 実装は、一覧を取れて pane が無いと確かめられた worktree だけを、配送を始めたあとにここへ渡す | `internal/orchestrator/restore.go` の `Restore`、`decideAdoptions` | 95% |
| 6 | 基本フローを `redispatch` の道にしたこと | active_states で redispatch のとき、次の巡回に委ねることを記録に残して終わる | 既定値である。復元の中で dispatch すると、着手の待ちで最大1時間止まる | `internal/orchestrator/restore.go` の `applyOrphanRunningAction` | 95% |
| 7 | 基本フロー 2 と、代替フロー 取り直しの失敗 | 復元のための取り直しが成功している。偽なら次の巡回に委ねることを記録に残して ABORT | 実装は最初にこれを見て、WARN を出して返る。呼び出し元は pane の有無を取り直しの成否より先に見るので、取り直しに失敗して pane も無い worktree は、呼び出し元の `paneの不在` からここへ届く。閉じる集合へは入れない（入れるのは pane が生きているときの `decideOne` だけである） | `internal/orchestrator/restore.go` の `decideAdoptions`、`restoreWithoutPane` | 95% |
| 8 | 基本フロー 3 と、代替フロー issueを確かめられない | 取り直した結果に置き場所の階層と一致する issue がある。偽なら理由を記録して ABORT | 見つからないときと、置き場所と違うリポジトリの issue だったときの2つを1つの検証にまとめた。どちらも何もせずに返り、結果が同じである | `internal/orchestrator/restore.go` の `restoreWithoutPane`、`issueAgreesWithPath` | 85% |
| 9 | 基本フロー 4 と、代替フロー 片付け対象のStatus | Status が cleanup.on_states に入っていない。偽なら worktree と branch を片付けて ABORT | `switch` の最初の枝である。`restart.orphan_running_action` は見ない | `internal/orchestrator/restore.go` の `restoreWithoutPane`、`cleanupInto` | 95% |
| 10 | 基本フロー 5 と、代替フロー 引き渡し状態 | Status が active_states に入っている。偽なら何もせず ABORT | 人間へ引き渡した状態では、Status を巻き戻さない。direct_chat_state もこの枝に入る（pane が無いので引き継ぐ相手がいない） | `internal/orchestrator/restore.go` の `restoreWithoutPane` | 90% |
| 11 | 基本フロー 6 と、代替フロー 着手待ちへ戻す・既に着手待ち・人間へ渡す | redispatch である。偽なら to_dispatch_state かを見て、Status を dispatch_state へ書いて記録を1件書く。Status が既に dispatch_state なら書かない。to_dispatch_state でもなければ failure_state へ書いて引き渡しの通知を1件書く | 3値の分岐である。VALIDATES THAT は偽の側を1本しか持てないので、3つ目を入れ子にした。既定の設定では dispatch_state も active_states に入っているので、Status が既に dispatch_state のことがある。そのとき `UpdateStatus` は書き込みを省いて `Wrote` を偽にし、`postStatusMove` は記録を書かない。事後条件（コメントが1件増えている）が成り立たないので、入れ子の `既に着手待ち` へ出した | `internal/orchestrator/restore.go` の `applyOrphanRunningAction`、`moveToFailure`、`internal/orchestrator/comment.go` の `postStatusMove`、`internal/tracker/adapter.go` の `UpdateStatus`、`internal/config/default.go` | 90% |
| 12 | 代替フローの終わり方 | 7本とも ABORT | どの扱いでもこの run の扱いはそこで終わり、基本フローの最後の段（次の巡回に委ねる記録）は redispatch にしか当てはまらない。起動は止まらず、呼び出し元が次の段へ進む。ABORT は「この記述はここで終わる」だけを意味する | rucm スキルの「`ABORT` と `RESUME STEP` の選び方」 | 85% |
| 13 | 経路に出さなかった失敗 | Status の書き込みの誤り、保護された Status、片付けの条件を満たさない worktree | WARN を出して返るだけで、この記述の中に後ろの段が無い。書き込みを断る Status の一覧は、`to_dispatch_state` では `TerminalStates` だけ、`to_failure_state` では `protectedStates`（terminal_states と direct_chat_state）である | `internal/orchestrator/restore.go` の `applyOrphanRunningAction`、`moveToFailure` | 85% |
