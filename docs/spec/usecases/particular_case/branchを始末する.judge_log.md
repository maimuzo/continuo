# 判断ログ: branchを始末する

- 対象: `docs/spec/usecases/particular_case/branchを始末する.rucm.md`
- 作成日 / 作成モデル: 2026-10-02 / Claude Opus 5.5。`worktreeとbranchを片付ける` から、branch を始末する段を分けて作った
- 参照した根拠資料: `docs/plans/continuo_design.md`（3-9 / 3-22b / 3-22c / 3-37-8）、`internal/workspace/cleanup.go`、`internal/workspace/git.go`、`internal/workspace/brokenref.go`

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | 配置先ディレクトリ | `particular_case/` | branch を始末するという1つの処理である。片付けのどの契機からも同じ `switch` を通る | `internal/workspace/cleanup.go` の `Cleanup` | 95% |
| 2 | `worktreeとbranchを片付ける` から分けたこと | 別の記述にし、`INCLUDE USE CASE` で引かせる | この段は branch が残っても止まらない（残ったことを片付けの結果に記録して先へ進む）。呼び出し元と1本に書くと、branch の結末（8通り）と親 workspace の結末（5通り）と worktree の消え方（2通り）の掛け算で経路が増える。呼び出し元の後ろの段は、この記述の結末で分岐しない（完了のログの文面が変わるだけである）。人間の決定（2026-10-02） | `internal/workspace/cleanup.go` の `Cleanup` | 95% |
| 3 | PRIMARY ACTOR / SECONDARY ACTORS | 巡回タイマー / git | 引く側の記述の主アクターに合わせた。この記述の中で相手にするのは git だけである | `internal/workspace/git.go` の `gitBranchDelete` | 85% |
| 4 | PRECONDITION | worktree の実体を消している。worktree を消す前に、branch を消してよいかを判定している | 判定（`deletableBranch`）は worktree が在るうちにしかできないので、呼び出し元の段14 に残した。この記述は判定の結果を受け取って始める。判定の中身（検算に落ちる6つの場合・壊れた ref の先読み）は、この記述の本文に書いた | `internal/workspace/cleanup.go` の `Cleanup` / `deletableBranch` | 90% |
| 5 | 段1〜10 を4分岐にしたこと | 実在しない / `cleanup.delete_branch` が偽 / 検算に落ちている / 削除を要求する | 前の版は3分岐で、「検算に落ちて残す」が無かった。実装の `switch` は4つの `case` を持つ。順番も実装のとおりにした（実在しないが先、設定が次、検算が3つ目） | `internal/workspace/cleanup.go` の `Cleanup`（branch の `switch`）と `deletableBranch` | 95% |
| 6 | 「残ったものとして利用者に伝える」を「片付けの結果に記録する」へ変えたこと | 段4・6 と、`消さないref`・`生き返ったref` | `Cleanup` は `Leftovers` へ積むだけである。**その並びを画面へ出すのは `continuo abandon` だけで、巡回も `cleanupPath` も読まない。**巡回で利用者に届くのは、完了のログの「branch は残しました」である | `internal/workspace/cleanup.go` の `CleanupResult.Leftovers`、`internal/abandon/abandon.go` の `remove`、`internal/orchestrator/reconcile.go` の `reconcileWorktrees` | 95% |
| 7 | 段9 と、`壊れたref` を SPECIFIC にしたこと | `VALIDATES THAT git が branch を削除している` の偽の側で、壊れた ref の始末を試す | 前の版は GLOBAL（BRANCH FROM は実在の検査の段）で、WHEN に `cleanup.delete_branch` の条件が無かった。**実装で壊れた ref のファイルを消すのは `gitBranchDelete` の中だけで、`git branch -D` が失敗したあとである。**`gitBranchDelete` は `switch` の最後の枝からしか呼ばれないので、設定が偽なら入らない。分岐元を削除の検証にすれば、この条件が構造で表せる | `internal/workspace/git.go` の `gitBranchDelete`、`internal/workspace/cleanup.go` の `Cleanup` | 90% |
| 8 | `消さないref` が受ける範囲 | 壊れた ref の条件を満たさないときと、壊れた ref ではない理由で `git branch -D` が失敗したときの両方 | 実装ではどちらも `pruneBrokenBranchRef` が偽を返し、最初の失敗がそのまま返る。`Cleanup` が WARN を出して `Leftovers` へ積み、設定ファイルの削除へ進む。手がかりの「削除に失敗して残す」は、この経路である | `internal/workspace/git.go` の `gitBranchDelete`、`internal/workspace/brokenref.go` の `pruneBrokenBranchRef` | 90% |
| 9 | `生き返ったref` に IF を入れたこと | もう一度消して、残っていれば理由を記録する。`RESUME STEP 6` で親の代替フローの終端（`RESUME STEP 11`）へ戻る | 撃ち直しが通る場合と通らない場合の2つの結末が在る。入れ子の代替フローの `RESUME STEP` は親の代替フローの段へ戻るので、親の終端を戻り先にした。そこから基本フローの段11 へ続く | `internal/workspace/git.go` の `confirmBranchGone` | 85% |
| 10 | 壊れた ref を消す条件を7つと書いたこと | 接頭辞・refname の正しさ・`show-ref` の失敗・`rev-parse` の失敗・解決後の置き場所・通常のファイルであること・中身が ref として読めないこと | どれか1つでも落とすと、正常な branch・利用者の `main`・`.git` の外のファイルを消す経路ができる。前の版の本文は「5つ」と書いていたが、実装のコメントと条件の数は7つである | `internal/workspace/brokenref.go` の `brokenBranchRef` | 95% |
| 11 | `壊れたref` を `RESUME STEP 11` で終えたこと | 段6 を `RESUME STEP 11`（結果を呼び出し元へ渡す）にし、`消さないref` と `生き返ったref` と `有無を確かめられない` はそこへ `RESUME STEP 6` で合流する | 実装は、branch の `switch` のどの結末でも同じ `switch` を抜けて次の処理（設定ファイルの削除）へ進み、結果を返す。壊れた ref を消せた結末は `BranchDeleted` が真で、基本フローの真の側と同じ状態である。消せなかった結末は `Leftovers` に理由が入る。前の版は ABORT にしていたが、その場で打ち切る実装ではない。基本フローの事後条件は、消せなかった結末でも成り立つ言い方（branch の扱いが片付けの結果に入っている）へ直した | `internal/workspace/cleanup.go` の `Cleanup`、`internal/workspace/git.go` の `gitBranchDelete` | 90% |
| 12 | 段11 を置いたこと | 「branch の扱いを片付けの結果として呼び出し元へ渡す」 | 基本フローを ENDIF で終えると、CFG の生成器が ENDIF で終わる経路を落とす（8本のはずが7本になった）。実装は `BranchDeleted`・`BranchAbsent`・`Leftovers`・`Notices` を結果に入れて返すので、その段を最後に置いた | `internal/workspace/cleanup.go` の `CleanupResult` | 85% |
| 13 | 実在の検査と壊れた ref の判定の順番を本文に書いたこと | `git show-ref` が「無い」と答えたら、「実在しない」と決める前に壊れた ref かどうかを見る | `show-ref` は壊れた ref にも終了コード 1 を返す（実測 2026-08-25、git 2.50.1）。そこを「元から無かった」に丸めると、壊れた ref のファイルが誰にも消されないまま残る | `internal/workspace/cleanup.go` の `deletableBranch` / `brokenRefBranchAt` | 95% |
| 14 | `有無を確かめられない` を足したこと | 壊れた ref のファイルを消したあと、branch が残っているかを git に確かめられなければ、撃ち直さずに理由を記録して親の終端へ戻る | 実装は、branch の有無を確かめる呼び出しが誤りを返したとき、`git branch -D` を撃ち直さずに誤りを返す。前の版は「残っていない」の偽の側が必ず撃ち直し（`生き返ったref`）を通る形で、この経路が無かった。撃ち直したあとの確かめが誤りを返したときも同じ文言で残るが、結末は `生き返ったref` の「残っていれば理由を記録する」と同じなので分けていない | `internal/workspace/git.go` の `confirmBranchGone` | 90% |
