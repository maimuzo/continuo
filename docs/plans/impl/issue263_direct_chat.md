# issue #263 direct chat を、確定した設計へ合わせる（進捗）

**言いたいこと。**この branch には古い版の設計で書いた実装がある。
設計 `docs/plans/continuo_design.md` の 3-82〜3-82k と 4-1（設計レビュー6周で確定）に1行ずつ当て、
**食い違いを全部直し、テストを足し、テストを全部通す。**
設計に書いていないが実装で必ず決めることは、6周目の判断票の「実装で当てる形」に従う。

## 食い違いの一覧

「直したか」の列は、直したら `済` にする。

### 3-82b 巡回のどこで走らせるか

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| b1 | 候補を2つに分け、direct chat のものは専用の1パス（`prepareDirectChatPanes`）へ。**先に走らせ、`dispatchAllowed` が真のときだけ** | `dispatchCandidates` の中で `directChat` の真偽値を9箇所に散らしている | 済 |
| b2 | `dispatchCandidates` へ `directChat` を1つも渡さない。外すのは `claimForDispatch` / `runStartOrFail` / `startRun` / `handoffGate` の4本、残すのは `startRunFromWorktree` だけ | 5本とも `directChat` を受ける | 済 |
| b3 | pane の写像はこの1パスで1回だけ作る。`agent.list` は投げない。引けなかったと pane が無いを混ぜない | `dispatchCandidates` の中で遅延して作っている（形は合っている） | 済 |
| b4 | 出入りの段1：担当者を 3-82h の表で判定（0人/2人以上なら `failure_state` を書き、印を持てば印も立てる。ログイン名が取れない・自分1人なら印を立てる。1人で他人なら手を離す。用意中なら印を下ろすだけ）。書き込みと `pane.close` はループの外 | 担当者を見ずに印を立てるだけ | 済 |
| b5 | 段2：用意中の run は印を下ろし、見た Status と時刻を記録するだけ（`direct_chat_state` を見たときも書く）。`o.mu` の中で判定 | 「用意中」の記録が無い | 済 |
| b6 | 段3：抜けた先が `terminal_states` なら「直接抜けた」印を立てる。段1 で入れるときに下ろす | 印が無い | 済 |
| b7 | 段4：続きの指示を送る印は、ループの外の後始末（`running_state`・hold）が終わってから立てる。送る直前に `agent.get` | goroutine の中で `agent.get` を先に投げ、hold を書かない | 済 |
| b8 | 段5：Status が direct chat の run と用意中の run は `switch` へ入れない | 用意中の扱いが無い | 済 |
| b9 | `redispatch` の入口に direct chat の検査を置かない | 置いてある | 済 |

### 3-82c 用意するかの門

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| c1 | 門1 印を持つ → 何も出さない | 形は合う（`dispatchCandidates` の先頭） | 済 |
| c2 | 門2 draft issue → Debug | 無い（`Dispatchable` を飛ばしたので素通り） | 済 |
| c3 | 門3 担当者が自分1人でない（0/2人以上は書く経路、1人で他人は Debug、ログイン名が取れないなら何もしない） | 担当の持ち回り（`handoffGate`）を通している | 済 |
| c4 | 門4 pane が1枚でもある → Debug | WARN/Debug が混ざる | 済 |
| c5 | 門5 空きスロット → **Debug**、`clearGate` は外へ | WARN | 済 |
| c6 | 門6 `preflight` | 形は合う | 済 |
| c7 | 門7 用意の失敗の間隔（専用の記録） | 無い | 済 |
| c8 | 通さない門：`pause_above_percent`・`handoffGate`・`skipByFailure`・`required_labels`・`Dispatchable` | `handoffGate` を通している | 済 |
| c9 | 門1 の表の段4：`redispatch` の着手の段2 が `direct_chat_state` を見たら担当者を消し戻さない（入札直後の着手では消す）。段2 は取り直した Status を返す | 消し戻す。Status を返さない | 済 |

### 3-82d 用意の段1〜段3

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| d1 | 段1 `o.claim` を直に呼び、「用意中」を立て、閉じる集合から外す。写しの Status は書き換えない | `claimForDispatch(…, true)` | 済 |
| d2 | 段2 失敗：自分の pane を ID で閉じ、印を外し、専用の記録へ数える。上限（`agent.max_retries`）で書く経路。WARN を毎回 | 記録も上限も無い | 済 |
| d3 | 段3 取り直し → `o.mu` の中で用意中を下ろし、新しいほうの Status で判定。入れる／戻った／印が無い／担当者が替わった・それ以外、の4通り。書き込みはロックの外。hold はここでは書かない（戻ったときだけ書く） | カードを見ずに direct chat へ入れる | 済 |
| d4 | 取り直しが失敗したとき：入れず、自分の pane を閉じて印を外す（失敗として数えない）＝判断票6周目 | 無い | 済 |

### 3-82e 不変条件と `UpdateStatus` の14箇所

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| e1 | 9箇所に `protectedStates()` | 済（数え直して確かめる） | 済 |
| e2 | 書く経路：拒否リストは選択肢のうち `direct_chat_state` 以外全部。未設定（空）なら書かない。写しが空なら WARN して書かない | 無い | 済 |

### 3-82f 印を外す道と、門だけでは足りない13箇所

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| f1 | 打ち切り（`abortTerminalForHuman`）の終え方を「`PaneID` が空でなく `agent.start` が済んでいる」で分ける | 印を残すだけ | 済 |
| f2 | `finishRunClaimed`：入口・`postHandoffComment` の直前・`ensureAgentComment` を抜けた直後・`release` の直前 | 入口だけ | 済 |
| f3 | `ensureAgentComment`：入口・段5・段8・`failCommentRecovery`・`failCommentRecoveryBusy` の直前。打ち切ったことを戻り値で返す。「直接抜けた」印なら入口で抜ける | 入口で印を見るだけ | 済 |
| f4 | `abandonRunClaimed` のリトライの枝：`after_run` のあと `addRetry` の直前 | 無い | 済 |
| f5 | `stopAndReleaseAsync`：`release` の直前 | 入口だけ | 済 |
| f6 | `stopForUnknownStateAsync`：コメントの直前と `release` の直前 | 無い | 済 |
| f7 | `stopBecauseHandoffLost`：先に direct chat を抜けさせる | 無い | 済 |
| f8 | turn ループ：`turnBlocked` で subagent を待ったあと、esc の直前にもう1度見る | 無い | 済 |
| f9 | `decideAfterTurn` の枝は direct chat へ入れない（戻るだけ） | 入れている | 済 |
| f10 | 閉じる集合（agent 名を問わず閉じる worktree の集合）を新設 | 無い | 済 |
| f11 | 復元の `moveToFailure`：direct chat では通知ごと投稿しない | 投稿する | 済 |
| f12 | 「見えなくなった」ループ：用意中の run は飛ばす | 無い | 済 |

### 3-82g・3-82h・3-82i

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| g1 | 作業中へ戻したら hold を書く（人間向けの文だけ別。印と JSON は入札の hold と同じ） | 書かない | 済 |
| g2 | `Done` へ直接抜けたら成果のコメントを書かせない | 無い | 済 |
| h1 | 判定の表（毎巡回・4行×印の有無） | 無い | 済 |
| h2 | 書く経路の文面2つ（i18n） | 無い | 済 |
| h3 | 手を離す経路 | 無い | 済 |
| i1 | 捨てる3つ（Stop を見た時刻・この turn で hook を見たか・受け口）と時計 | 時計だけ | 済 |

### 3-82j・3-4・3-9・3-23（再起動）

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| j1 | 段3 の取り直しに失敗した worktree は閉じずに閉じる集合へ | 閉じない（集合が無い） | 済 |
| j2 | herdr の一覧を取れなかった worktree も閉じる集合へ | 無い | 済 |
| j3 | direct chat で閉じない6つの道は、見送って閉じる集合へ。通知も投稿しない | 閉じないだけ | 済 |
| j4 | 段4 `closeExtraPanes`：direct chat の worktree では閉じない。agent 名を持つ pane を引き継ぎの相手に。無ければ2枚とも残す | pane ID の小さいほう | 済 |
| j5 | 3-9 の手順7b：閉じる集合の worktree では agent 名の無い pane も閉じる | agent 名のある pane だけ | 済 |

### 3-82k 設定の検査

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| k1 | 7つの相手の一覧を1箇所に置き、起動時と doctor が同じものを読む | 起動時の中に直書き | 済 |
| k2 | エラーの文面に「このキーを書いていない場合は既定値です」 | 無い | 済 |
| k3 | doctor：front matter を読み直して重なりを `!` で出す | 無い | 済 |
| k4 | setup：書けなかったときの見本は `  direct_chat_state: "<選んだ値>"`（飛ばしたなら `""`） | `tracker.direct_chat_state: "Direct Chat"` の形 | 済 |
| k5 | `abandon`：`--park`・`--to`・いまの Status の3つを断る。文面は3つ。`--force` でも通さない | 2つを同じ文面で断る | 済 |

### 雛形・文書・組み込みの指示書

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| t1 | 5-2 の雛形：担当者の1文、「たいていは」、担当者を別の1人に替えると pane を閉じる（判断票6周目） | 担当者の文が無い | 済 |
| t2 | `internal/prompt/builtin.md` の 3-1・3-2 と設計 5-3 の「全部読む」の行を外す | 入っている | 済 |
| t3 | FAQ・upgrading | 古い版 | 済 |

## 判断票6周目の「実装で当てる形」

| 何 | どう当てたか |
| --- | --- |
| `agent.start` が済んだかの持ち方 | run に「`agent.start` が成功した pane の ID」を1つ持ち、`PaneID` と一致するときだけ済んでいると読む。復元で引き取った run は引き取った pane の ID |
| 印を外すのはロックを放してから | ロックの中では外すことを決めるだけにし、`release` はロックの外 |
| 用意の段3 の取り直しが失敗 | 入れず、自分で開いた pane を閉じて印を外す（失敗として数えない） |
| 引き継いだ回数の代償 | FAQ と upgrading に書く |
| 雛形 | 担当者を別の1人に替えると pane を閉じることを1行足す |
| 順3 の「印を下ろすだけ」 | direct chat の印を下ろすだけ（`o.runs` は用意の段3 が外す） |
| 用意中に戻したときの書き込みの順序 | 書き込みが終わってから送る印を立てる |
| 3-77g の「消さない」とき released | 書かない（`undoHandoffAcquire` を呼ばない） |

## 直した場所

| 何 | どこ |
| --- | --- |
| 専用の1パス・門1〜門7・用意の段1〜段3・書く経路・手を離す経路・hold・閉じる集合・打ち切り | `internal/orchestrator/directchat.go` |
| 出入りの段1〜段5・閉じる集合の照合・見えなくなったループ | `internal/orchestrator/reconcile.go` の `updateDirectChatMode` / `reconcileRunning` / `reconcileWorktrees` / `closeOrphanPane` |
| 候補を2つに分ける・用意中の run を起こさない・閉じる集合と用意の失敗の記録 | `internal/orchestrator/orchestrator.go` の `Tick` / `wakeRuns` / `Orchestrator` |
| `dispatchCandidates` を origin/main の形へ戻す・閉じる集合を飛ばす・段2 の取り直した Status を返す・`redispatch` の入口の検査を外す・`agent.start` した pane を控える | `internal/orchestrator/dispatch.go` |
| `handoffGate` を origin/main の形へ戻す・`stopBecauseHandoffLost` は先に抜けさせる | `internal/orchestrator/handoff.go` |
| 打ち切りの地点（入口・通知の直前・`ensureAgentComment` の後・`release` の直前・リトライの枝）・`decideAfterTurn` は入れない | `internal/orchestrator/lifecycle.go` |
| `ensureAgentComment` の打ち切りの地点と戻り値・「直接抜けた」印 | `internal/orchestrator/comment.go` |
| `stopForUnknownStateAsync` の打ち切りの地点 | `internal/orchestrator/unknownstate.go` |
| esc の直前の確認・送る直前の `agent.get` | `internal/orchestrator/turn.go` |
| 用意中・起動済みの pane・直接抜けた・送る直前の確認の記録、抜けるときに捨てる3つ | `internal/orchestrator/runstate.go` |
| 取り直しの失敗・一覧の失敗・6つの道を閉じる集合へ・通知を投稿しない・2枚目の pane | `internal/orchestrator/restore.go` |
| 7つの相手の一覧（`DirectChatConflicts`）と原文の読み直し | `internal/config/states.go` / `internal/config/validate.go` |
| doctor の重なりの検査 | `internal/doctor/status_names.go` |
| setup の貼れる1行 | `internal/scaffold/fill.go` の `StatusKeyLine` と `internal/cli/cli.go`（案内の見本だけ） |
| abandon の3つの断り | `internal/abandon/abandon.go` |
| hold の人間向けの文 | `internal/handoff/handoff.go` の `FormatDirectChatHold` |
| 文言 | `internal/i18n/keys.go`・`messages/ja.json`・`messages/en.json` |
| 雛形 | `internal/scaffold/template.go`（設計 5-2 にも同じ1行を足した） |
| 「全部読む」の行を外す | `internal/prompt/builtin.md`（origin/main へ戻した）と設計 5-3 |

## 直さなかったもの

**無い。**判断票6周目の「直さない」の行（18時間の言い過ぎ・終わらせる処理の最中に手を離す・pane を閉じる道の列挙・
`PaneID` が空でも閉じ損ねている・`running_state` の書き込みの失敗）は、設計どおり直していない。

## 設計に書いていなかったので実装で決めたこと

| 何 | どう決めたか | 理由 |
| --- | --- | --- |
| 書く経路で「未設定（空）なら書かない」をどう表すか | 拒否リストへ空文字を1つ足す | `UpdateStatus` は取り直した値と拒否リストを同じ正規化で比べる（`foldStatus("")` 同士が一致する）。取り直しを1本増やさずに済む |
| 閉じる集合の鍵 | project item の ID（値は worktree のパス） | 通常の候補のループ・用意の段1・`reconcileWorktrees`・復元の4箇所がどれも持っている値であり、パスの正規化の差を気にしなくてよい |
| 用意の失敗の上限の「達したら」 | `回数 >= agent.max_retries` | 「上限に達したら」の文言どおり |
| 門7 の間隔 | `retryBackoff(回数-1, agent.max_retry_backoff_ms)` | 通常の着手の `abandonRunClaimed` が1回目の失敗で `retryBackoff(0, …)` を使うのと揃える |
| 送る直前の `agent.get` の置き場所 | turn ループが送る直前（`busyCheckBeforeSend` の印） | 3-82g の図は「次の巡回で、送る直前に1本」。巡回のループの中で待たない |

## hook の規則（CLAUDE.md の6）との当たり

人間が了承したのは「direct chat の run では、turn の終わりの hook を受け口（hookCh）へ流さない」の1件だけである
（`OnHook` の門。この branch が前から持っていたもので、今回は触っていない）。

今回触った、検知の網に掛かるファイルと判定。

| ファイル | 触ったところ | 判定 |
| --- | --- | --- |
| `internal/cli/cli.go` | `runSetup` の書けなかったキーの案内の見本だけ | 4つのどれにも当たらない（`hook` サブコマンド・`switch args[0]`・`parseErrorExitCode` に触っていない） |
| `internal/orchestrator/orchestrator.go` | `Tick` の候補の分け方・`wakeRuns` の用意中の飛ばし・`Adopt` の起動済みの pane・欄と map の初期化 | 4つのどれにも当たらない（`pendingDir`・`OnHook` は触っていない） |
| `internal/orchestrator/turn.go` | `turnBlocked` の esc の直前の確認・送る直前の `agent.get` | 4つのどれにも当たらない。turn の終わりの判定（`confirmTurnEnd`・`awaitStop`・`awaitHook`）は1行も変えていない。変えたのは「turn を送るかどうか」で、送らないときは既存の `awaitFirst` の道（復元の段5a2 と同じ）へ入るだけである |
| `internal/orchestrator/runstate.go` | 記録の欄の追加・direct chat を抜けるときに捨てる3つ（Stop を見た時刻・この turn で hook を見たか・受け口） | 4つのどれにも当たらない。hook が送る内容・受け口に流す条件・`noteHook` の記録の仕方は変えていない。捨てるのは本体の中の状態で、`beginTurn` が turn を送るたびに捨てているのと同じ3つである（設計 3-82i）。新旧の実行ファイルが混ざっても、hook の側から見える違いは無い |
| `internal/orchestrator/settings.go` / `hookinput.go` / `internal/socketpath` / `hookclient` / `hookserver` / `lock` | 触っていない | — |

## 足したテスト

| ファイル | テスト |
| --- | --- |
| `test/internal/orchestrator/direct_chat_test.go` | 担当者0人・2人の候補は failure_state／1人で他人・ログイン名が取れない候補は何もしない／印を持つ run の担当者0人・他人・ログイン名が取れない／選択肢の写しが空なら書かない／2台が同時に書いてもコメントは1件／未設定の Status へは書かない／戻したときに hold を書く／打ち切りの3通り（起動済み・`agent.start` 前・pane 無し）。既存の9本は担当者を自分1人にして通す |
| `test/internal/orchestrator/direct_chat_setup_test.go` | 用意の段1〜段3（指示を送らず、戻すと継続の指示）／用意中に戻す（1回目の本文）／用意中に担当者が替わる／用意の失敗の上限／用意が落ちた直後はやり直さない／バックオフ明けに担当者を消し戻さない／再起動で引き取る／確認の画面なら閉じず通知も書かない／取り直しの失敗の agent 名の無い pane／2枚目の pane の引き継ぎの相手／agent 名の無い2枚 |
| `test/internal/abandon/abandon_test.go` | `--to` と、いまの Status（`--force` 付き）を断る |
| `test/internal/doctor/status_names_test.go` | 重なった相手のキーを名指しする |
| `test/internal/config/validate_values_test.go` | 既定値の文面・7つの相手（`cleanup.on_states`） |
| `test/internal/cli/cli_test.go` | setup の貼れる1行 |

## テストの結果

`sh scripts/test-like-ci.sh`（2026-09-28 JST。`-race` あり）: `ok` 62件、`grep -c "^FAIL"` = 0、`--- FAIL` = 0、終了コード 0。
`go vet ./...`: 終了コード 0。

**素の `go test ./...` では `test/live` の1本が落ちた**（手元の herdr の版の違い。`test-like-ci.sh` は herdr を隠すので飛ぶ）。この変更とは関係が無い。

## 止まった理由

（無し）

## 実装レビュー1周目で直すもの

**言いたいこと。**判断票（PR #267 の実装レビュー1周目）で「直す」と決めた11件を、直す前に全部並べる。
影響範囲を確かめてから一気に直す。

| # | 指摘 | 直す場所 | 影響範囲と確かめたこと |
| --- | --- | --- | --- |
| r1 | 立て直したあとの打ち切りで「止めた印」が残る（high） | `internal/orchestrator/directchat.go` の `abortTerminalForHuman` の「印を残す」枝 | 止めた印（`markWorkerStopped`）を立てる道は `stopWorker` と `closeDirectChatSetupPane` の2つ。**どちらも `PaneID` を空にする**ので、そのあと印を残す枝（`PaneID` が空でなく `agent.start` 済み）へ入れるのは、`ensureAgentComment` の段5 が新しい pane で `agent.start` を通した後だけである（段7・段8・`failCommentRecovery`・`failCommentRecoveryBusy` の直前の4地点）。**止めた印が立っていれば `beginAttempt(true)` で世代を進める。**`beginAttempt` は `terminating` も下ろすが、この枝はこのあと `endTerminal` で同じものを下ろすので矛盾しない（`claimTerminal` → 打ち切り → `endTerminal` の順は変わらない）。**`SendFirstPrompt` は `beginAttempt` の前の値へ戻す**（立てたままにすると、戻したときに1回目の本文が送られる）。`resumed` は真にする（同じセッションなので累計トークンを畳み込まない）。古い世代の turn ループが残っていても、世代が変わるので `currentWorker` が偽になって抜ける |
| r2 | turn の終わりに direct chat を見たとき送る印を立てない（mid） | `internal/orchestrator/lifecycle.go` の `decideAfterTurn` の direct chat の枝と、`internal/orchestrator/reconcile.go` の `updateDirectChatMode` の段2 | 枝で `setNeedsPrompt` を立てる。**direct chat に入れば `wakeRuns` が飛ばす。**入らずに戻れば次の巡回で送る。**ただし入ってから戻ったときは、段4 の「書き込みが終わってから送る印を立てる」を守るため、抜けた時点でこの印を下ろす**（`takeNeedsPrompt`）。下ろさないと、hold と `running_state` を書く前に `wakeRuns` が送る |
| r3 | 門5 が Status ごとの上限まで当てる（mid） | `internal/orchestrator/dispatch.go` に全体の枠だけを見る関数を分け、`internal/orchestrator/directchat.go` の門5 から呼ぶ | 設計 3-82c の門5 は `agent.max_concurrent_agents` だけ。通常の着手（`dispatchCandidates`）は今までどおり `freeSlotBlocker` を使う |
| r4 | `failRun` と打ち切りの上限の枝に、引き渡しの通知の直前の打ち切りが無い（low） | `internal/orchestrator/lifecycle.go` の `failRun` と `abandonRunClaimed` のリトライを使い切った枝 | `finishRunClaimed` と同じく `UpdateStatus` のあと・`postHandoffComment` の直前に置く。`noteFailure` はその前で数える（Status は `protectedStates` が守っている）|
| r5 | `ensureAgentComment` の段7 の直前に見ない（low） | `internal/orchestrator/comment.go` | 段6 の `confirmStartup` を抜けたあと、`agent.prompt` の直前に1つ足す。当たれば r1 の枝を通る |
| r6 | CLAUDE.md の cli.go のリンク3本の着地先が違う（low） | `CLAUDE.md` の3本だけ | 着地先: `parseErrorExitCode` の関数全体、`runHook` の注記「2 を返してはならない」の2行、`--socket` と `--pending-dir` の欠落と相対パスを exit 1 にしている範囲。**cli.go の行数が変わらないことを確かめてから直す**（このあと触るのはコメント1行の数字だけ）|
| r7 | 「8つのキー」が残る（実際は9つ）（low） | `docs/FAQ.md`・`docs/bug_details.md`・`docs/plans/continuo_design.md` の本文・`internal/cli/cli.go` のコメント・`internal/scaffold/fill.go`・`internal/scaffold/update.go`・`test/internal/scaffold/statuses_test.go`・`docs/spec/usecases/` の RUCM | **巻き込まないもの。**設計 3-32d の見出し（題名）。RUCM の基本フロー16 と、その否定の分岐（`VALIDATES THAT WORKFLOW.md に書き換える対象の8つのキーがあり…`）。**16 は「無いと止まるキー」の数を言っており、`direct_chat_state` は無くても止めない（`statusKeys` の `optional`）ので8のままが正しい。**RUCM を直したら CFG を生成し直し、テストのマーカーのハッシュを揃える |
| r8 | doctor の注記「ここで出さないと、どこにも出ない」（low） | `internal/doctor/status_names.go` | 巡回も起動後に WARN を1回出す（`candidateStates` の `noteDirectChatMissing`）。**doctor の一覧に並ぶのはここだけ**、と書き直す |
| r9 | FAQ に戻す以外の抜け方が無い（low） | `docs/FAQ.md` の direct chat の節 | `Blocked` / `In Review` へ動かすと pane を閉じ worktree は残す。`Done` へ動かすと pane を閉じ `cleanup.on_states` なら片付ける（未 push があれば断る）。**チャットの中で PR をマージして issue が閉じると、カンバンの自動化が `Done` へ動かすので pane が閉じる**。`Ice Box` はコメントを1件書いて止める（設計 3-82g） |
| r10 | PR の本文の hook の段落が `turn.go` の変更を少なく書く（low） | PR #267 の本文 | `turn.go` の差分のコード行を全部並べて書き直す（待ちを打ち切るコンテキスト・ループの先頭の2つ・送る直前の確認・待ちのあと `switch` の手前・esc の直前）|
| r11 | 打ち切りの呼び出し元などにテストが無い（low） | `test/internal/orchestrator/` | r1 と r5 を `ensureAgentComment` から通す。r2 を turn の終わりから通す。r3・`Done` へ直接抜けたとき成果のコメントを書かせないこと・閉じる集合で作業中でない Status では残すこと |

**hook の規則（CLAUDE.md の6）との当たり。**触るファイルのうち検知の網に掛かるのは `internal/cli/cli.go`（コメントの数字1つ）だけで、
`turn.go`・`runstate.go`・`hookinput.go`・`settings.go` は触らない。r1 は `runstate.go` の既存の `beginAttempt` を呼ぶだけで、
turn の終わりの判定・hook の受け口・送る内容は変えない。

### 直した場所（実装レビュー1周目）

| # | どこ |
| --- | --- |
| r1 | `internal/orchestrator/directchat.go` の `abortTerminalForHuman`（止めた印が立っていれば `beginAttempt(true)`、`SendFirstPrompt` は前の値へ戻す） |
| r2 | `internal/orchestrator/lifecycle.go` の `decideAfterTurn`（`setNeedsPrompt`）と `internal/orchestrator/reconcile.go` の `updateDirectChatMode`（抜けたら `takeNeedsPrompt`） |
| r3 | `internal/orchestrator/dispatch.go` の `globalFreeSlot` を足し、`internal/orchestrator/directchat.go` の門5 から呼ぶ |
| r4 | `internal/orchestrator/lifecycle.go` の `failRun` と `abandonRunClaimed` のリトライを使い切った枝 |
| r5 | `internal/orchestrator/comment.go` の段7 の直前。設計 3-82f の地点の列挙にも段7 を足した |
| r6 | `CLAUDE.md` の cli.go のリンク3本（`1773-1791`・`1746-1747`・`1601-1606`） |
| r7 | `docs/FAQ.md`・`docs/bug_details.md`・設計 3-32d の本文と表・8053 行付近・13890 行付近・`internal/cli/cli.go`・`internal/scaffold/fill.go`・`internal/scaffold/update.go`・`test/internal/scaffold/statuses_test.go`・RUCM（概要・事後条件・図2つ）と CFG とテストのマーカー7本 |
| r8 | `internal/doctor/status_names.go` |
| r9 | `docs/FAQ.md` の「作業中へ戻す以外の抜け方」 |
| r10 | PR #267 の本文の hook の段落 |
| r11 | `test/internal/orchestrator/direct_chat_setup_test.go` の5本（下） |

### 足したテスト（実装レビュー1周目）

| テスト | 何を確かめるか | 直しを外すと |
| --- | --- | --- |
| `TestDirectChat_コメントを書かせる途中で引き取って戻すと同じpaneで続く` | r1・r5 を `ensureAgentComment` の段5〜段7 から通す | r1 を外すと戻しても指示が届かずに落ち、r5 を外すと人間の pane へ指示が送られて落ちる（確かめた） |
| `TestDirectChat_turnの終わりに引き取りを見たあと巡回より先に戻しても指示が届く` | r2 | 外すと落ちる（確かめた） |
| `TestDirectChat_Statusごとの上限に達していてもpaneを用意する` | r3 | 外すと落ちる（確かめた） |
| `TestDirectChat_Doneへ直接抜けたら成果のコメントを書かせに行かない` | 3-82g の `Done` の行 | — |
| `TestDirectChat_閉じる集合のpaneは作業中でないStatusでは閉じない` | 3-82f の閉じる集合 | — |

### 直さずに残したもの（実装レビュー1周目）

- **設計 3-32d の見出し「8つのキーである」。**題名は巻き込まない（依頼の決まり）。本文と表は9つへ直した
- **RUCM の基本フロー16 と、その否定の分岐（CFG の条件文を含む）。**16 は「無いと止まるキー」の数を言っており、`direct_chat_state` は無くても止めないので8のままが正しい

### テストの結果（実装レビュー1周目）

`sh scripts/test-like-ci.sh`（2026-09-28 JST。`-race` あり）: 終了コード 0、`grep -c "^FAIL"` = 0、`grep -c "^--- FAIL"` = 0。
`go vet ./...`: 終了コード 0。`sh scripts/check-rucm.sh --strict`: 終了コード 0（[W1] の警告は前から在るもの）。
