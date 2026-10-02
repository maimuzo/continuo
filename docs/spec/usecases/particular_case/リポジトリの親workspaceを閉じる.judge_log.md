# 判断ログ: リポジトリの親workspaceを閉じる

- 対象: `docs/spec/usecases/particular_case/リポジトリの親workspaceを閉じる.rucm.md`
- 作成日 / 作成モデル: 2026-10-02 / Claude Opus 5.5。`worktreeとbranchを片付ける` から、親 workspace を閉じる段を分けて作った
- 参照した根拠資料: `docs/plans/continuo_design.md`（3-9b / 3-9d）、`internal/workspace/repoworkspace.go`、`internal/workspace/cleanup.go`、`test/live/herdr_test.go`

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | 配置先ディレクトリ | `particular_case/` | 親 workspace を閉じるという1つの処理である。片付けのどの契機からも同じ関数が呼ばれる | `internal/workspace/cleanup.go` の `Cleanup` | 95% |
| 2 | `worktreeとbranchを片付ける` から分けたこと | 別の記述にし、`INCLUDE USE CASE` で引かせる | この段は失敗しても止まらない（`closeRepoWorkspace` は値を返さない）。呼び出し元と1本に書くと、親 workspace の結末（5通り）と branch の結末（8通り）と worktree の消え方（2通り）の掛け算で経路が増える。呼び出し元の後ろの段は、この記述の結末で分岐しないので、掛け合わせた経路に新しい情報は無い。人間の決定（2026-10-02） | `internal/workspace/repoworkspace.go` の `closeRepoWorkspace` | 95% |
| 3 | PRIMARY ACTOR / SECONDARY ACTORS | 巡回タイマー / herdr | 引く側の記述（巡回が起こす片付け）の主アクターに合わせた。この記述の中で相手にするのは herdr だけである。身元ファイルへの書き込みはシステム自身の状態の変更である | `internal/workspace/repoworkspace.go` の `closeRepoWorkspaceLocked` / `handOverRepoWorkspace` | 85% |
| 4 | PRECONDITION | worktree の実体を消している。身元ファイルを読んである。herdr のクライアントを持っている | `Cleanup` は `removeWorktree` が通ったあとにだけ `closeRepoWorkspace` を呼ぶ。身元ファイルは worktree と一緒に消えるので、消す前に読んだ値を使う。herdr のクライアントが無いときは ID が無いときと同じく何もしないので、事前条件に寄せて本文に書いた | `internal/workspace/cleanup.go` の `Cleanup`、`internal/workspace/repoworkspace.go` の `closeRepoWorkspace` | 90% |
| 5 | 段1〜11（閉じる条件） | continuo が開かせた親を、一覧を引けて、ID が現物と一致し、同じリポジトリの worktree が残っていないときだけ閉じる。残っていれば ID を書き移す | `worktree.open` は workspace を2つ開くのに `worktree.remove` は1つしか閉じない。本線の IF は、どちらの枝も正常な2つ（ID が身元ファイルに在るか・同じリポジトリの worktree が残っているか）だけにした | `internal/workspace/repoworkspace.go` の `closeRepoWorkspace`、`closeRepoWorkspaceLocked` | 90% |
| 6 | 段7 と `親workspaceを閉じられない` | herdr が close を断ったら、WARN を出して `RESUME STEP 12` | herdr 0.9.0 以降は、配下に worktree の workspace が在る親の close を `workspace_group_close_required` で断る。実装は WARN を出して値を返さずに戻り、一覧を引けない・ID が食い違う場合と同じ流れで呼び出し元が次の段へ進む。その場で打ち切る実装ではないので、前の版の ABORT をやめた。基本フローの事後条件は、閉じられなかった結末でも成り立つ言い方（herdr が close の要求を受け付けていれば閉じている）へ直した | `internal/workspace/repoworkspace.go` の `closeRepoWorkspaceLocked` | 90% |
| 7 | 本文の「親を閉じると配下も消える」を版で書き分けたこと | 0.8.x は配下ごと消える。0.9.0 以降は断られて何も閉じない | 前の版は 0.8.x の動きだけを書いていた。実測の日付とテスト名は実装のコメントと `test/live/herdr_test.go` に在る | `internal/workspace/repoworkspace.go` の冒頭のコメント、`test/live/herdr_test.go` の `TestLive_WorkspaceClose_配下があると親は断られ何も消えない` | 95% |
| 8 | `cwd` を渡すのをやめる案を採らなかったこと | `cwd` は外せない | 本物の herdr で確かめてある。`cwd` を省くと断られ、`cwd` に worktree のパスを渡しても断られる | `test/live/herdr_test.go` の `TestLive_WorktreeOpen_cwdはリポジトリ本体しか受け付けない` | 100% |
| 9 | 段12 を置いたこと | 「リポジトリの親 workspace の始末を終える」 | 基本フローを ENDIF で終えると、CFG の生成器が ENDIF で終わる経路を落とす（5本のはずが2本になった）。呼び出し元へ戻ることを表す段を最後に1つ置いた | `docs/spec/usecases/particular_case/リポジトリの親workspaceを閉じる.cfg.json`（経路5本） | 85% |
| 10 | 一覧を引けない場合と ID が現物と違う場合を代替フローへ出したこと | 段3 と段4 を VALIDATES THAT にし、偽の側を `一覧を引けない`・`IDの食い違い`（どちらも `RESUME STEP 12`）にした | どちらも WARN を出す失敗である。前の版は本線の IF の偽の側へ入れていたが、本線の IF はどちらの枝も正常な場合だけに使う | `internal/workspace/repoworkspace.go` の `closeRepoWorkspaceLocked` | 90% |
| 11 | 引き継ぎの細部を段にしなかったこと | 段8 の1段にし、渡す相手の3条件と、1件も渡せなかったときの WARN は本文の表に書いた | 書けなくても止まらず、分岐もしない | `internal/workspace/repoworkspace.go` の `handOverRepoWorkspace` | 85% |
