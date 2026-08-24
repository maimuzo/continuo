<!-- 目的: 「issue を1件処理する」の RUCM を書くときに AI が行った判断と、その根拠を記録する -->

# 判断ログ: issue を1件処理する

- 対象: `docs/spec/usecases/particular_case/issue を1件処理する.rucm.md`
- 作成日 / 作成モデル: 2026-08-20 / Claude Opus 5 (1M context)
- 参照した根拠資料: `docs/plans/continuo_design.md`（3-2 / 3-5 / 3-6 / 3-8 / 3-16 / 3-16b / 3-18 / 3-21 / 3-25 / 3-34 / 4-1）、`internal/orchestrator/dispatch.go`、`internal/orchestrator/failure.go`、`internal/orchestrator/turn.go`、`internal/orchestrator/lifecycle.go`、`internal/orchestrator/comment.go`、`internal/orchestrator/signal.go`、`internal/workspace/prepare.go`、`internal/tracker/adapter.go`

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | USE CASE NAME | issue を1件処理する | 依頼で指定された名前をそのまま使う。動詞で終わる名詞句の規則も満たす | 依頼文 | 100% |
| 2 | 配置ディレクトリ | `particular_case/` | 「1件の issue を dispatch して turn を回し、表明で Status を動かす」という単一目的の操作単位である。複数ユースケースを跨ぐ時系列ではない | rucm スキルの粒度ガイド | 95% |
| 3 | BRIEF DESCRIPTION | 巡回・候補の取得・着手・turn ループ・表明の反映の5文 | 設計の「1つの turn で何が起きるか」の図をそのまま5文へ落とした。R12（単文のみ）を守るために文を分けた | `docs/plans/continuo_design.md#3-5` | 90% |
| 4 | PRECONDITION | 常駐している。flock を取っている。選択肢名が設定と一致する。dispatch_state に issue が1件以上ある。herdr が待ち受けている | 起動時の検査を全部通ってからでないと巡回が始まらない。選択肢名が合わないと GraphQL がエラーを出さずに0件を返す | `docs/plans/continuo_design.md#3-6`、`#3-17` | 90% |
| 5 | PRIMARY ACTOR | 巡回タイマー | このユースケースを起こすのは人間ではなく、`poll_interval_ms` ごとに走る巡回である。**利用者を主アクターにすると、実在しない「人間が dispatch を要求する」経路を記述することになる** | `docs/plans/continuo_design.md#3-8`（巡回のループがやることは3つだけである）、`internal/orchestrator/orchestrator.go` の `Run` と `Tick` | 70% |
| 6 | SECONDARY ACTORS | GitHub Projects v2、herdr、Claude Code | ボードを読み書きし、pane を操作し、turn を送る相手がこの3つである。git は `workspace` の内部で使うだけなのでこのユースケースでは挙げない | `docs/plans/continuo_design.md#3-1` | 85% |
| 7 | DEPENDENCY | なし | 他の particular_case を取り込まない。INCLUDE を持つのは scenario 側である | rucm スキルのファイル規約 | 95% |
| 8 | GENERALIZATION | なし | 汎化関係にあるユースケースが無い | - | 90% |
| 9 | ステップ分割方針 | 着手の13段を1段1ステップに割り、turn ループを DO-UNTIL に収める | 設計が着手の順番を「段と段の間で落ちたとき外側に何が残るか」で決めている。**段をまとめると、落ちた位置ごとの事後条件が書けなくなる** | `docs/plans/continuo_design.md#3-16` | 90% |
| 10 | ステップ2 | システムはボードから active_states の issue の一覧を取る | 巡回の GraphQL リクエストの1本目である。**`In Progress` も active_states に含める**ので、実行中の issue も候補の配列に入る | `docs/plans/continuo_design.md#3-10`、`internal/orchestrator/orchestrator.go` の `Tick` | 95% |
| 11 | ステップ5（VALIDATES THAT） | 空きスロットが1つ以上ある | 着手の段-1 である。**印を付ける前に評価する**（印を付けてから弾くと印が残る） | `docs/plans/continuo_design.md#3-16`、`internal/orchestrator/dispatch.go` の `hasFreeSlot` | 95% |
| 12 | ステップ6（VALIDATES THAT） | 対象リポジトリが Claude Code に信頼登録されている | 着手の段0 である。**Status を書く段2 より前に置く**。書いてから飛ばすと `In Progress` が毎巡回で候補に上がり、30秒ごとにコメントが積まれる | `docs/plans/continuo_design.md#3-6`、`#3-16`、`internal/orchestrator/dispatch.go` の `preflight` | 95% |
| 13 | ステップ8 と 10 の順序 | 印を先に付け、ボードの Status をそのあとに書く | 印はメモリなので落ちると消える。Status は残るので、再起動後の識別に使える。**逆にすると、印の無い状態で Status だけが動く** | `docs/plans/continuo_design.md#3-16` | 100% |
| 14 | ステップ11 から14 の順序 | worktree → workspace → 設定ファイル → 身元ファイル | 身元ファイルには herdr の workspace の ID（段3 で確定）と設定ファイルのパス（段5 で確定）が要る。**先に書けない** | `docs/plans/continuo_design.md#3-16`、`#3-18`、`internal/orchestrator/dispatch.go` の `startRun` | 95% |
| 15 | ステップ15 と16 | pane を新しく作らず、`worktree.open` が作った pane を引いて label を書く | 1 worktree を1 herdr workspace にすると決めており、`pane.split` も `tab.create` も呼ばない。label は `owner/repo/issues/N` の形の表示名であり、復元の照合には使わない | `docs/plans/continuo_design.md#3-3`、`#4-5`、`internal/orchestrator/dispatch.go` の `resolvePane` | 95% |
| 16 | ステップ19（VALIDATES THAT） | agent_status が idle または done である | 着手の段10 である。**done も合格**（continuo は tab をフォーカスしないので実運用ではほぼ常に done 側になる） | `docs/plans/continuo_design.md#3-16`、`internal/orchestrator/dispatch.go` の `confirmStartup` | 95% |
| 17 | turn ループを DO-UNTIL にしたこと | ステップ21 から27 を DO-UNTIL で囲む | turn は「表明が working でなくなるまで」繰り返す。**IF で書くと繰り返しが表現できない** | `docs/plans/continuo_design.md#3-8`、`internal/orchestrator/turn.go` の `turnLoop` | 90% |
| 18 | ステップ21（VALIDATES THAT） | turn 数が max_dispatch_turns に達していない | 打ち切りの判定は turn ループの先頭で行う。**送る前に判定しないと、上限を1回超えて送る** | `internal/orchestrator/turn.go` の `turnLoop` | 95% |
| 19 | ステップ22 | turn の本文を送る（1回目と2回目以降を1ステップにまとめた） | 送る本文の違い（1回目の本文か継続の指示か）は「いまのセッションに会話履歴があるか」で決まる内部の分岐であり、外から見た相互作用は同じ1回の送信である | `docs/plans/continuo_design.md#3-8`、`internal/orchestrator/turn.go` の `buildTurnText` | 75% |
| 20 | ステップ23 と24 を分けたこと | 「Stop hook を受ける」と「settle_ms のあいだ待つ」を別ステップにした | R4（1文1動作）。**待ちは判定の一部であり、受信とは別の動作である** | `docs/plans/continuo_design.md#3-2` | 90% |
| 21 | ステップ25（VALIDATES THAT） | settle_ms のあいだに `task-notification` で始まる UserPromptSubmit が届かない | turn の終わりの分かれ目がここである。途中の Stop では 0.033〜0.037 秒後に届き、最終 Stop の後に来るのは `SubagentStop` である | `docs/plans/continuo_design.md#3-2`（実測8/8） | 95% |
| 22 | ステップ26 | transcript から表明の行を読む | `last_assistant_message` は使えない（印を書いた17件すべてで印が入っていなかった）。**transcript を `promptSource == "typed"` から遡って読む** | `docs/plans/continuo_design.md#3-25`、`internal/orchestrator/transcript.go` | 100% |
| 23 | ステップ28（UNTIL の条件） | 表明の値が working でない | `working` は「まだ続きがある」であり、次の turn を送る。`review` と `blocked` で run が終わる | `docs/plans/continuo_design.md#3-25` | 95% |
| 24 | ステップ29 | Status に表明の値の遷移先の選択肢を書く | Status を書くのは continuo の Go のコードである。エージェントは1行書くだけである。**書く前に必ず ID 指定で取り直す**（`UpdateStatus` が内部で行う） | `docs/plans/continuo_design.md#3-25`、`#4-1`、`internal/orchestrator/lifecycle.go` の `applySignals` | 100% |
| 25 | ステップ30（VALIDATES THAT） | issue に今回の run が書いたコメントがある | run が終わるときだけ確かめる（毎 turn ではない）。**marker が付いていて、かつ作成時刻が run の開始より新しいものだけを数える** | `docs/plans/continuo_design.md#3-25`、`internal/orchestrator/comment.go` の `hasRunComment` | 95% |
| 26 | ステップ31 から33 の順序 | after_run → pane を閉じる → 印を外す | `workspace_hooks.after_run` の cwd は worktree であり、run が終わったとき（worker を止める直前）に1回だけ走らせる | `docs/plans/continuo_design.md#3-9` の手順0、`internal/orchestrator/lifecycle.go` の `finishRunClaimed` | 95% |
| 27 | 基本フローに片付けを含めなかったこと | worktree と branch は残したまま run を終える | 片付けの契機は Status が `cleanup.on_states` に入ったときだけである。表明が `review` のときは `In Review` であり、片付けてはならない | `docs/plans/continuo_design.md#3-9` の手順1、`#4-1` | 100% |
| 28 | 基本フローの POSTCONDITION | Status が表明の遷移先。コメントがある。pane が閉じている。印が外れている。worktree と branch が残っている | 「引き渡し」の定義（worker は止めるが worktree は残す）をそのまま事後条件にした | `docs/plans/continuo_design.md#3-5` | 95% |
| 29 | フロー `空きスロット不足` | RFS BASIC FLOW 5。何も dispatch せず ABORT | 空きが0なら、その巡回では以降の候補を1件も dispatch しない | `docs/plans/continuo_design.md#3-16` の段-1 | 95% |
| 30 | フロー `未信頼のリポジトリ` | RFS BASIC FLOW 6。コメントを1件書いて ABORT | 信頼していないフォルダでは hook が1つも動かず turn 終了検知が全滅する。**コメントはそのリポジトリにつき1回だけ**である | `docs/plans/continuo_design.md#3-6`、`internal/orchestrator/dispatch.go` の `noteUntrusted` | 95% |
| 31 | フロー `起動直後の確認画面` | RFS BASIC FLOW 19。esc を送ってから failure_state へ落とす | `blocked` のまま turn を送ると保留中の権限要求が承認されて実行される（3/3 で再現）。**投げた本文のほうは消える** | `docs/plans/continuo_design.md#3-11`、`internal/orchestrator/dispatch.go` の `confirmStartup` と `sendEscape` | 100% |
| 32 | フロー `上限での打ち切り` | RFS BASIC FLOW 21。failure_state へ落として人間へ渡す | 仕様は打ち切りを正常終了として継続を予約するが、continuo は失敗として扱う。**無人で回すので、上限まで使って終わらなかった作業は人間に渡す** | `docs/plans/continuo_design.md#8-1`（打ち切りを失敗として扱う） | 95% |
| 33 | フロー `turnの継続` | RFS BASIC FLOW 25。RESUME STEP 23 で Stop hook を待ち直す | `task-notification` が届いたら turn は続いている。**turn 数は増やさない** | `docs/plans/continuo_design.md#3-2` | 95% |
| 34 | フロー `コメントの取り戻し` | RFS BASIC FLOW 30。pane を閉じてからセッションを復帰させ、書かせてから RESUME STEP 31 | 先に worker を止めないと同じセッション UUID が2つ生きる。一度使った UUID をもう一度渡すと起動に失敗する | `docs/plans/continuo_design.md#3-25` の9段、`#3-3`、`internal/orchestrator/comment.go` の `ensureAgentComment` | 95% |
| 35 | フロー `コメントの取り戻しの失敗` | RFS コメントの取り戻し 6。failure_state へ落として ABORT | 9段の最後が「それでも書かれなければ failure_state へ落として人間に渡す」である。**代替フローの中の条件ステップを RFS で指した**（`VALIDATES THAT` の偽ケースは RFS を持つ特定代替フローで書く決まりのため） | `docs/plans/continuo_design.md#3-25` の9段、rucm スキルの厳密規則9 | 90% |
| 36 | フロー `権限の確認` の種別と BRANCH FROM | GLOBAL ALTERNATIVE FLOW。BRANCH FROM BASIC FLOW 22 | 権限の確認は turn を送ったどの時点でも起こりうるので任意時点代替フローにした。**分岐元はステップ22（turn の本文を送った直後）である。**ここが「保留中の権限要求が承認されて実行される」被害が最大になる時点であり、本文を送った直後に blocked が返る（2.7〜4.1 秒） | `docs/plans/continuo_design.md#3-11`（実測3/3）、`#3-2` | 80% |
| 37 | フロー `無音の打ち切り` の種別と BRANCH FROM | GLOBAL ALTERNATIVE FLOW。BRANCH FROM BASIC FLOW 23 | 画面が止まる事象はどの時点でも起こりうる。**分岐元はステップ23（Stop hook を待っている時点）である。**ここは待ち時間がいちばん長く、エージェントの作業が最も進んだ状態で失われる | `docs/plans/continuo_design.md#3-21`、`internal/orchestrator/reconcile.go` の `checkStalls` | 80% |
| 38 | `無音の打ち切り` で worktree を残すこと | pane を閉じ、印とバックオフを残す | バックオフ中の issue を印から外すと30秒後の巡回で即座に拾い直され、バックオフが効かない | `docs/plans/continuo_design.md#3-25`（バックオフ中の issue も印に残す） | 95% |
| 39 | 代替フローの POSTCONDITION の書き方 | 「Status」「pane」「worktree」「印」の4つの状態を毎回書く | この4つが、次の巡回の振る舞いを決める全部である。**どれかを書き落とすと、次に何が起きるかがテストから決められない** | `docs/plans/continuo_design.md#3-16`（落ちた段ごとに外側へ残るもの） | 85% |
| 40 | 巡回の3本のリクエストを書かなかったこと | 候補の取得だけを書き、実行中の照合と worktree の照合は書かない | 実行中の照合と worktree の照合は「issue を1件処理する」の外側にある巡回の仕事である。**別のユースケース（`worktree と branch を片付ける`）が受け持つ** | `docs/plans/continuo_design.md#3-9` の手順7 | 80% |
| 41 | ステップ12 で worktree の絶対パスを渡すと明記したこと | `worktree.open` には path だけを渡す。branch は渡さない | herdr は path と branch の**片方だけ**を受け付け、両方来ると `invalid_request: exactly one of path or branch is required` で弾く。**実機で全ての着手が落ちた**（2026-08-20）。テスト用herdr mock が branch を無視していたので、本物で叩くまで気づけなかった | `herdr worktree open` の実測、internal/workspace/prepare.go | 100% |
| 42 | `無音の打ち切り` の WHEN と最初のステップ | WHEN に「`turn_timeout_ms` のあいだ hook が1件も届かず、画面の版も増えない場合」と書き、ステップ1 で `agent_status` と画面の版を一緒に要求する | **打ち切りの物差しは turn の総実行時間ではなく、画面が変わらないまま経った時間である。**閾値に達しただけでは打ち切らず、`agent.get` を1回呼んで画面の版が増えていないことを確かめてから止める。**版が増えている限り、1つの turn に何時間かかっても打ち切らない** | `docs/plans/continuo_design.md#3-21`、`internal/orchestrator/reconcile.go` の `checkStalls`、`internal/orchestrator/runstate.go` の `noteRevision` | 100% |
| 43 | 段13 を条件ステップにした理由 | pane が起動を受け付けるかを先に確かめる | **pane を作った直後は herdr が `agent_pane_busy`（`is not an available shell`）を返す。**シェルの起動が終わるまでコマンドを受け取れない。2026-08-21 に E2E で実測 | `internal/orchestrator/dispatch.go` の `startRun` | 95% |
| 44 | 段15 に `interactive_ready` を足した理由 | `agent_status` だけでは足りない | **`idle` になっても `interactive_ready` が偽の時間がある**（0.5〜2秒）。その間に指示を送ると herdr が `agent_not_ready` で弾く。herdr 自身が `agent start` の説明で入力を受け付けられることを成功の条件としている | `internal/orchestrator/dispatch.go` の `confirmStartup` | 95% |
| 45 | 「paneがまだ使えない」で 30 秒待つ根拠 | 30 秒 | **回数（3回 × 500 ミリ秒 = 1.5 秒）では足りず、実運用で着手できなかった。**シェルの起動はプロファイルの中身とマシンの速さで変わるので、回数ではなく時間で粘る | `internal/orchestrator/dispatch.go` の `agentStartBusyBudget` | 80% |
| 46 | 「起動の待ち直し」でやり直す理由 | 待つだけでなく agent.start をやり直す | **`agent.start` が受け付けられても Claude Code が1文字も起動しないことがある**（`agent.get` が `agent_not_found` を返す）。待つだけでは永久に通らない | `internal/orchestrator/dispatch.go` の `confirmStartupWithRestart` | 85% |
| 47 | 起動に失敗したとき人間へ渡さない理由 | agent.max_retries までバックオフして着手をやり直す | **1回の失敗で人間へ渡すと、待てば通ったはずの issue が毎回止まる**（2026-08-21 に実運用で発生）。回数を使い切ってから渡す | `internal/orchestrator/dispatch.go` の `ErrStartupRetryable` と `abandonRun` | 90% |
| 48 | ステップ7（VALIDATES THAT）を足した理由 | worktree の置き場所をそのまま使えるかを、Status を書く前に見る | **着手が確定して失敗する検査が Status を書いたあとにあると、`In Progress` と `Blocked` の往復が永久に続く。**`In Progress` は active_states なので次の巡回でまた候補に上がる | `docs/plans/continuo_design.md#3-16b`、`internal/workspace/prepare.go` の `CheckWorktreeUsable` | 100% |
| 49 | ステップ7 を読み取り専用にしたこと | `git worktree list` と `git rev-parse --abbrev-ref HEAD` を読むだけにする | **段0 は「まだ何も書かない」段である。**`prune` も `worktree add` も呼ばない。段3（`Prepare`）の同じ検査は保険として残す | `docs/plans/continuo_design.md#3-16`、`internal/workspace/prepare.go` | 95% |
| 50 | ステップ9（VALIDATES THAT）を足した理由 | 書く前に取り直した Status が terminal_states にも failure_state にも入っていないことを見る | **拒否リストが terminal_states だけだと、人間が `Blocked` に置いた issue を `In Progress` へ上書きできてしまう。**候補の一覧はサーバ側の検索結果であり、載っていること自体は着手してよい根拠にならない | `docs/plans/continuo_design.md#3-16b`、`#3-34`、`internal/orchestrator/dispatch.go` の `dispatchBlockedStates` | 100% |
| 51 | 書かなかったときに failure_state へ落とさないこと | 印を外して静かにやめる（`書かずに取りやめる`） | ボードは continuo が触る前のままなので、人間へ伝えるべきことが1つも無い。**ここで落とすと、人間が置いた `Blocked` を continuo が上書きしたことになる** | `internal/orchestrator/dispatch.go` の `ErrStatusNotWritten` | 95% |
| 52 | ステップ3（VALIDATES THAT）を足した理由 | 候補の Status が active_states に入っていることを自分で確かめる | **`items(query:)` の絞り込みはサーバ側の検索であり、直前に書いた値の反映が遅れる。**`Blocked` にした issue がそのまま返る | `docs/plans/continuo_design.md#3-34`、`internal/orchestrator/dispatch.go` の `dispatchCandidates` | 95% |
| 53 | 食い違った候補で巡回を止めないこと | その item だけ落として続ける。落とすのは1件でも、止めるのは大半が外れたときだけ | **1件の食い違いで一覧ごとエラーにすると、正しく絞り込めていた他の issue の dispatch まで止まる。**絞り込みのキーを解決できないときは条件ごと無視されてボードのほぼ全件が返るので、割合で見分けられる | `docs/plans/continuo_design.md#3-34`、`internal/tracker/adapter.go` の `dropUnrequestedStates` | 85% |
| 54 | ステップ4（VALIDATES THAT）を足した理由 | 同じ issue の失敗の回数をメモリ上に持ち、max_retries を超えたら拾わない | **印は run が終わると消えるので、印の中の `RetryCount` では止まらない。**次の巡回が0回目として拾い直し、同じ失敗を30秒ごとに繰り返す | `docs/plans/continuo_design.md#3-16b`、`internal/orchestrator/failure.go` | 90% |
| 55 | ステップ12 にリポジトリ本体を足したこと | 「worktree の絶対パスとリポジトリ本体の作業ディレクトリを渡して workspace として開き、その label に owner/repo/issues/N を書く」（label の側は判断55の対象ではなく、判断15 と同じ表示名の規則である） | **`cwd` は外せない。**省くと herdr が `worktree_not_found`、worktree のパスを渡すと `linked_worktree_source` で断る | 実測 2026-08-25（`test/live/herdr_test.go` の `TestLive_WorktreeOpen_cwdはリポジトリ本体しか受け付けない`） | 100% |
| 56 | 親 workspace を控える `workspace.list` を独立したステップにしなかったこと | ステップ12 の前後に入る読み取りとして、本文の節で説明した | **読むだけで、外から見える結果を1つも変えない。**控えた ID の行き先（身元ファイル）はステップ14 が既に受け持っている。ステップを足すと、以降40箇所の RFS 参照を振り直すことになり、振り間違いのほうが害が大きい | `internal/workspace/prepare.go` の `Prepare`、`internal/workspace/repoworkspace.go` | 80% |
| 57 | 控えるのを「先に親が無かったとき」に限ったこと | 先からあったなら控えない（空のままにする） | 先からあった親は**人間が自分で開いたもの**である。片付けで閉じると、その人の pane ごと消える | `docs/plans/continuo_design.md#3-9b`、issue #19 | 90% |
