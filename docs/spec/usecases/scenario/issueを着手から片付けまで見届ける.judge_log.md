# 判断ログ: issueを着手から片付けまで見届ける

- 対象: `docs/spec/usecases/scenario/issueを着手から片付けまで見届ける.rucm.md`
- 作成日 / 作成モデル: 2026-08-20 / Claude Opus 5 (1M context)（2026-10-02 に実装に合わせて書き直した。Claude Opus 5.5）
- 参照した根拠資料: `docs/plans/continuo_design.md`（3-5 / 3-8 / 3-9 / 3-16 / 3-25 / 3-27 / 3-83 / 4-1）、`docs/spec/usecases/particular_case/` の5件、`internal/orchestrator/lifecycle.go`、`internal/orchestrator/turn.go`、`internal/orchestrator/reconcile.go`、`internal/orchestrator/directchat.go`、`internal/workspace/cleanup.go`

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | USE CASE NAME | issueを着手から片付けまで見届ける | 「issue がボードに載ってから片付くまで」を、動詞で終わる名詞句の規則に合わせた | 依頼文、rucm スキルのテンプレート | 85% |
| 2 | 配置ディレクトリ | `scenario/` | 5件の particular_case を跨ぐ時系列である | rucm スキルの粒度ガイド | 100% |
| 3 | 取り込む particular_case | 5件。名前だけで引く | 取り込んだ先の段の番号と代替フローの名前に依存すると、取り込んだ先を直すたびにこの記述が古くなる | 依頼文 | 90% |
| 4 | PRECONDITION に `cleanup.enabled` を足したこと | 足す | 基本フローは片付けまで通る。片付けが無効なら通らない | `docs/spec/usecases/particular_case/worktreeとbranchを片付ける.rucm.md` の PRECONDITION | 80% |
| 5 | PRIMARY ACTOR | 利用者 | この時系列を始めるのは、issue を作ってボードに載せる人間である | `docs/plans/continuo_design.md#4-1` | 95% |
| 6 | SECONDARY ACTORS | GitHub Projects v2、herdr、Claude Code、git | 取り込んだ5件の副アクターの和集合である | 取り込んだ5件の rucm ブロック | 85% |
| 7 | 利用者がボードを直接操作すること | 利用者からボードへの直接の操作として書く | R3（アクター間の直接相互作用の禁止）から外れる。人間は GitHub の画面でボードを触るのであって、continuo に要求しない。システムに仲介させて書くと、実在しない自動化を記述することになる | `docs/plans/continuo_design.md#4-1` | 60% |
| 8 | 前の版の段6（Status が review の遷移先かの検査）を消したこと | 消す。基本フローは review を表明した場合だけを書く | この検査をするコードは無い。turn の終わりの処理は、表明を読み、Status を書き、取り直した Status で分岐する。その1回は `issueを1件処理する` の中に在る | `internal/orchestrator/lifecycle.go` の `handleTurnEnd` と `decideAfterTurn` | 85% |
| 9 | `判断の依頼` の種別と分岐元 | 任意時点の代替フロー。BRANCH FROM BASIC FLOW 5 | 前の版は、段5 の INCLUDE が終わったあとで `人間に判断を渡す` を INCLUDE していた。表明を読む・Status を書く・pane を閉じる、が2回書かれていた。実装では1つの run につき1回である。段5 の途中から分岐させ、`人間に判断を渡す` が残りを受け持つ形にした | `internal/orchestrator/lifecycle.go` の `handleTurnEnd` と `finishRunClaimed` | 80% |
| 10 | `判断の依頼` の戻り先 | RESUME STEP 5 | 利用者が Status を `dispatch_state` へ戻すと、次の巡回が同じ issue にもう一度着手する。やり直しである | `docs/plans/continuo_design.md#4-1` | 85% |
| 11 | `システムによる引き渡し` を足したこと | 任意時点の代替フロー。BRANCH FROM BASIC FLOW 5。RESUME STEP 5 | システムは、エージェントの表明が無くても、自分で Status を `failure_state` へ落として人間へ引き渡す通知を書く（指示の回数の上限、確認の画面、文面を組み立てられない、など）。通知は「Status を着手待ちへ戻してください」と案内している | `internal/orchestrator/turn.go` の `turnLoop`、`internal/orchestrator/lifecycle.go` の `failRun` と `finishRunClaimed`、`internal/orchestrator/prompt.go` の `buildHandoffComment` | 80% |
| 12 | `人間が引き取る` を足したこと | 任意時点の代替フロー。BRANCH FROM BASIC FLOW 5。RESUME STEP 5 | 利用者が Status を `direct_chat_state` へ動かすと、システムは turn を送るのをやめ、pane を残す。作業中の Status へ戻すと、hold のコメントを書いてから同じ pane へ継続の指示を送る | `internal/orchestrator/reconcile.go` の `updateDirectChatMode`、`internal/orchestrator/directchat.go` の `returnFromDirectChatAsync` と `postDirectChatHold`、`internal/orchestrator/turn.go` の `turnLoop` | 80% |
| 13 | `人間が引き取る` の WHEN に担当者の条件を入れたこと | 担当者がこの機械の投稿者1人である場合だけを書く | 担当者が0人か2人以上なら `failure_state` を書きに行き、別の1人なら手を離す。この2つは取り込む先の particular_case が無いので、ここには書いていない | `internal/orchestrator/reconcile.go` の `updateDirectChatMode` | 70% |
| 14 | `人間が引き取る` の段4〜10（利用者が戻す先の Status） | 利用者は `direct_chat_state` 以外の選択肢を書く。システムは、書かれた Status が `active_states` に入っていることを検証し、`dispatch_state` の選択肢なら `running_state` を書いて動かした記録をコメントする | 実装は、取り直した Status が `direct_chat_state` 以外なら、どの Status でも direct chat の印を下ろす。`dispatch_state` へ戻されたときは、システムが `running_state` を書く（書かないと、カードが着手待ちに見えたままになる）。`dispatch_state` と一致するかどうかだけで判定するので、ほかの作業中の Status へ戻されたときは何も書かない。どちらも正常なので IF にした。前は「利用者が `running_state` を書く」の1段に絞っていて、システムが書く枝と、作業中でない Status へ動かす枝が無かった | `internal/orchestrator/reconcile.go` の `updateDirectChatMode`、`internal/orchestrator/directchat.go` の `directChatReturnState` と `writeRunningStateOnReturn` | 90% |
| 15 | 段7 | pull request の変更をレビューする。前の版は「branch の変更」だった | pull request はエージェントが出す | `internal/prompt/builtin.md` の 3-5 | 85% |
| 16 | 段8 と基本フローの POSTCONDITION の Status | `cleanup.on_states` の選択肢。前の版は `terminal_states` だった | 片付けるかどうかは `cleanup.on_states` で決まる。既定はどちらも `Done` である | `internal/workspace/cleanup.go` の `ShouldCleanup`、`internal/orchestrator/reconcile.go` の `reconcileWorktrees` | 85% |
| 17 | POSTCONDITION の branch | 「`cleanup.delete_branch` が真であれば無い」 | 偽なら branch は残る | `internal/orchestrator/lifecycle.go` の `cleanupPath` | 90% |
| 18 | `枠の上限` と `常駐の再起動` の POSTCONDITION | turn 数と、引き継いだ回数を書かない | どちらも取り込んだ先の経路で変わる。取り込んだ先が持つ | 取り込んだ2件の rucm ブロック | 80% |
| 19 | `枠の上限` と `常駐の再起動` の種別と BRANCH FROM | 任意時点の代替フロー。BRANCH FROM BASIC FLOW 5。RESUME STEP 5 | どちらも「走行中の run がある時点」でしか起こらない。続きは同じ処理である | `docs/plans/continuo_design.md#3-27`、`#3-4` | 80% |
| 20 | 5本の任意時点の代替フローが同じ分岐元を指すこと | どれも BRANCH FROM BASIC FLOW 5 | 分岐元は「フローごとに原則1点」であり、別々のフローが同じ点を指すことは制限されていない | rucm スキルの BRANCH FROM 規約 | 80% |
| 21 | 段3（Ice Box）を置いたこと | 置く | `Ice Box` を未着手の置き場として使うと人間が決めている。continuo はボードに載っていない issue を見ない | `docs/plans/continuo_design.md#4-1` | 90% |
| 22 | 段11 | 片付けの結果をログで応答する | 片付けたとき、branch を残したとき、片付けずに残したときで、ログの文が違う | `internal/orchestrator/lifecycle.go` の `cleanupPath` | 80% |
| 23 | 直接ステップと INCLUDE の使い分け | 人間の操作は直接ステップ、システムの一連の処理は INCLUDE | 人間の操作を particular_case にすると、1ステップだけのユースケースが増える | rucm スキルの scenario の設計方針 | 85% |
| 24 | 溢れた particular_case の扱い | 使い始めるまでの5件は、この scenario に取り込まない | 5件は `はじめてcontinuoを動かせるようにする` が取り込んでいる | `docs/spec/usecases/scenario/はじめてcontinuoを動かせるようにする.rucm.md` | 85% |
| 25 | フロー `作業中でないStatusへ戻す` | RFS 人間が引き取る 6 の入れ子。継続の指示を送らず、pane を閉じ、印を外して ABORT | 利用者が作業中でない Status へ動かすと、実装は direct chat の印を下ろし、「続きの指示は送りません」と記録して戻る。同じ巡回の照合が、完了の Status なら run を終え、それ以外なら worker を止めて印から外す。どちらも pane を閉じて印を外すので1本にした。基本フローの段5 へ戻る続きが無いので ABORT である。worktree を片付けるかどうかは Status で決まるので、POSTCONDITION に条件つきで書いた | `internal/orchestrator/reconcile.go` の `updateDirectChatMode` と `reconcileRunning`、`internal/orchestrator/lifecycle.go` の `finishRunClaimed` と `stopAndReleaseAsync` | 80% |
| 26 | 経路が6本から12本へ増えたこと | 分けずに1本のままにした | 増えた6本は、`人間が引き取る` の中の2本（システムが `running_state` を書く枝・作業中でない Status へ動かして終わる枝）と、取り込んだ記述が打ち切った場合を受ける4本（`片付けの見送り`・`回答を待たずに終わる`・`枠待ちからの引き渡し`・`引き継がれない再起動`）である。どれも1箇所から1本ずつで、掛け合わさらない | `docs/spec/usecases/scenario/issueを着手から片付けまで見届ける.cfg.json` | 90% |
| 27 | 取り込んだ記述が打ち切った場合の受け方 | 取り込んだ直後に検証を置いた。片付けは `片付けの見送り`（次の巡回を待って `RESUME STEP 9`）、ほかの3つは入れ子の代替フロー（ABORT） | 取り込んだ先は、worktree を残して終わる・Status が `dispatch_state` へ戻らずに終わる・`failure_state` を書いて run を終える・起動を止める、などの打ち切りを持つ。前は直後に検証が無く、打ち切られた実行が `RESUME STEP 5` や「片付けの結果を応答する」をそのまま通り、基本フローの事後条件（worktree は置き場所に無い）と両立しなかった。片付けは巡回が次の回にやり直すので戻り先が在る。ほかの3つは、入れ子の代替フローからは基本フローへ戻れないので ABORT にし、取り込んだ先の事後条件のとおりと書いた。検証は、取り込んだ先が打ち切らずに終えたときに成り立つ状態で書いた | `internal/orchestrator/reconcile.go` の `reconcileWorktrees`、`docs/spec/usecases/particular_case/` の `worktreeとbranchを片付ける`・`人間に判断を渡す`・`レートリミットで待って再開する`・`再起動して実行中のissueを引き継ぐ` の rucm ブロック | 80% |
| 28 | 冒頭の「テストを作らない理由」が足りているか（人間の指示。2026-10-02） | 3つを足した。分けて書き直しても新しく確かめられることが無いこと。利用者の操作の段は実物の GitHub と利用者の手が要ること。direct chat の段を確かめるテストの在りか | 前の文は「印を付けられない理由」（`SOURCE` を1つしか持てない）が中心で、テストを書かない理由としては道具の都合に読めた。このシナリオは INCLUDE 以外に自分の段（利用者の操作と、代替フロー `人間が引き取る`・`作業中でないStatusへ戻す` のシステムの段）を持つので、その段がどこで確かめられているか、なぜ動かさないかを書いた | 人間の指示（2026-10-02）、`test/internal/orchestrator/direct_chat_test.go`、`test/e2e/walkthrough_test.go` | 85% |
