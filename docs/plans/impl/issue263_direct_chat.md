# issue #263 direct chat を、確定した設計へ合わせる（進捗）

**言いたいこと。**この branch には古い版の設計で書いた実装がある。
設計 `docs/plans/continuo_design.md` の 3-83〜3-83k と 4-1（設計レビュー6周で確定）に1行ずつ当て、
**食い違いを全部直し、テストを足し、テストを全部通す。**
設計に書いていないが実装で必ず決めることは、6周目の判断票の「実装で当てる形」に従う。

## 食い違いの一覧

「直したか」の列は、直したら `済` にする。

### 3-83b 巡回のどこで走らせるか

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| b1 | 候補を2つに分け、direct chat のものは専用の1パス（`prepareDirectChatPanes`）へ。**先に走らせ、`dispatchAllowed` が真のときだけ** | `dispatchCandidates` の中で `directChat` の真偽値を9箇所に散らしている | 済 |
| b2 | `dispatchCandidates` へ `directChat` を1つも渡さない。外すのは `claimForDispatch` / `runStartOrFail` / `startRun` / `handoffGate` の4本、残すのは `startRunFromWorktree` だけ | 5本とも `directChat` を受ける | 済 |
| b3 | pane の写像はこの1パスで1回だけ作る。`agent.list` は投げない。引けなかったと pane が無いを混ぜない | `dispatchCandidates` の中で遅延して作っている（形は合っている） | 済 |
| b4 | 出入りの段1：担当者を 3-83h の表で判定（0人/2人以上なら `failure_state` を書き、印を持てば印も立てる。ログイン名が取れない・自分1人なら印を立てる。1人で他人なら手を離す。用意中なら印を下ろすだけ）。書き込みと `pane.close` はループの外 | 担当者を見ずに印を立てるだけ | 済 |
| b5 | 段2：用意中の run は印を下ろし、見た Status と時刻を記録するだけ（`direct_chat_state` を見たときも書く）。`o.mu` の中で判定 | 「用意中」の記録が無い | 済 |
| b6 | 段3：抜けた先が `terminal_states` なら「直接抜けた」印を立てる。段1 で入れるときに下ろす | 印が無い | 済 |
| b7 | 段4：続きの指示を送る印は、ループの外の後始末（`running_state`・hold）が終わってから立てる。送る直前に `agent.get` | goroutine の中で `agent.get` を先に投げ、hold を書かない | 済 |
| b8 | 段5：Status が direct chat の run と用意中の run は `switch` へ入れない | 用意中の扱いが無い | 済 |
| b9 | `redispatch` の入口に direct chat の検査を置かない | 置いてある | 済 |

### 3-83c 用意するかの門

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

### 3-83d 用意の段1〜段3

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| d1 | 段1 `o.claim` を直に呼び、「用意中」を立て、閉じる集合から外す。写しの Status は書き換えない | `claimForDispatch(…, true)` | 済 |
| d2 | 段2 失敗：自分の pane を ID で閉じ、印を外し、専用の記録へ数える。上限（`agent.max_retries`）で書く経路。WARN を毎回 | 記録も上限も無い | 済 |
| d3 | 段3 取り直し → `o.mu` の中で用意中を下ろし、新しいほうの Status で判定。入れる／戻った／印が無い／担当者が替わった・それ以外、の4通り。書き込みはロックの外。hold はここでは書かない（戻ったときだけ書く） | カードを見ずに direct chat へ入れる | 済 |
| d4 | 取り直しが失敗したとき：入れず、自分の pane を閉じて印を外す（失敗として数えない）＝判断票6周目 | 無い | 済 |

### 3-83e 不変条件と `UpdateStatus` の14箇所

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| e1 | 9箇所に `protectedStates()` | 済（数え直して確かめる） | 済 |
| e2 | 書く経路：拒否リストは選択肢のうち `direct_chat_state` 以外全部。未設定（空）なら書かない。写しが空なら WARN して書かない | 無い | 済 |

### 3-83f 印を外す道と、門だけでは足りない13箇所

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

### 3-83g・3-83h・3-83i

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| g1 | 作業中へ戻したら hold を書く（人間向けの文だけ別。印と JSON は入札の hold と同じ） | 書かない | 済 |
| g2 | `Done` へ直接抜けたら成果のコメントを書かせない | 無い | 済 |
| h1 | 判定の表（毎巡回・4行×印の有無） | 無い | 済 |
| h2 | 書く経路の文面2つ（i18n） | 無い | 済 |
| h3 | 手を離す経路 | 無い | 済 |
| i1 | 捨てる3つ（Stop を見た時刻・この turn で hook を見たか・受け口）と時計 | 時計だけ | 済 |

### 3-83j・3-4・3-9・3-23（再起動）

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| j1 | 段3 の取り直しに失敗した worktree は閉じずに閉じる集合へ | 閉じない（集合が無い） | 済 |
| j2 | herdr の一覧を取れなかった worktree も閉じる集合へ | 無い | 済 |
| j3 | direct chat で閉じない6つの道は、見送って閉じる集合へ。通知も投稿しない | 閉じないだけ | 済 |
| j4 | 段4 `closeExtraPanes`：direct chat の worktree では閉じない。agent 名を持つ pane を引き継ぎの相手に。無ければ2枚とも残す | pane ID の小さいほう | 済 |
| j5 | 3-9 の手順7b：閉じる集合の worktree では agent 名の無い pane も閉じる | agent 名のある pane だけ | 済 |

### 3-83k 設定の検査

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
| 用意の失敗の上限 | `回数 > agent.max_retries`（実装レビュー2周目で `>=` から直した） | 通常の着手の `skipByFailure` と同じ比べ方。`>=` だと `agent.max_retries: 0` で一度も書かれない |
| 門7 の間隔 | `retryBackoff(回数-1, agent.max_retry_backoff_ms)` | 通常の着手の `abandonRunClaimed` が1回目の失敗で `retryBackoff(0, …)` を使うのと揃える |
| 送る直前の `agent.get` の置き場所 | turn ループが送る直前（`busyCheckBeforeSend` の印） | 3-83g の図は「次の巡回で、送る直前に1本」。巡回のループの中で待たない |

## hook の規則（CLAUDE.md の6）との当たり

人間が了承したのは「direct chat の run では、turn の終わりの hook を受け口（hookCh）へ流さない」の1件だけである
（`OnHook` の門。この branch が前から持っていたもので、今回は触っていない）。

今回触った、検知の網に掛かるファイルと判定。

| ファイル | 触ったところ | 判定 |
| --- | --- | --- |
| `internal/cli/cli.go` | `runSetup` の書けなかったキーの案内の見本だけ | 4つのどれにも当たらない（`hook` サブコマンド・`switch args[0]`・`parseErrorExitCode` に触っていない） |
| `internal/orchestrator/orchestrator.go` | `Tick` の候補の分け方・`wakeRuns` の用意中の飛ばし・`Adopt` の起動済みの pane・欄と map の初期化 | 4つのどれにも当たらない（`pendingDir`・`OnHook` は触っていない） |
| `internal/orchestrator/turn.go` | `turnBlocked` の esc の直前の確認・送る直前の `agent.get` | 4つのどれにも当たらない。turn の終わりの判定（`confirmTurnEnd`・`awaitStop`・`awaitHook`）は1行も変えていない。変えたのは「turn を送るかどうか」で、送らないときは既存の `awaitFirst` の道（復元の段5a2 と同じ）へ入るだけである |
| `internal/orchestrator/runstate.go` | 記録の欄の追加・direct chat を抜けるときに捨てる3つ（Stop を見た時刻・この turn で hook を見たか・受け口） | 4つのどれにも当たらない。hook が送る内容・受け口に流す条件・`noteHook` の記録の仕方は変えていない。捨てるのは本体の中の状態で、`beginTurn` が turn を送るたびに捨てているのと同じ3つである（設計 3-83i）。新旧の実行ファイルが混ざっても、hook の側から見える違いは無い |
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
| r1 | 立て直したあとの打ち切りで「止めた印」が残る（HIGH） | `internal/orchestrator/directchat.go` の `abortTerminalForHuman` の「印を残す」枝 | 止めた印（`markWorkerStopped`）を立てる道は `stopWorker` と `closeDirectChatSetupPane` の2つ。**どちらも `PaneID` を空にする**ので、そのあと印を残す枝（`PaneID` が空でなく `agent.start` 済み）へ入れるのは、`ensureAgentComment` の段5 が新しい pane で `agent.start` を通した後だけである（段7・段8・`failCommentRecovery`・`failCommentRecoveryBusy` の直前の4地点）。**止めた印が立っていれば `beginAttempt(true)` で世代を進める。**`beginAttempt` は `terminating` も下ろすが、この枝はこのあと `endTerminal` で同じものを下ろすので矛盾しない（`claimTerminal` → 打ち切り → `endTerminal` の順は変わらない）。**`SendFirstPrompt` は `beginAttempt` の前の値へ戻す**（立てたままにすると、戻したときに1回目の本文が送られる）。`resumed` は真にする（同じセッションなので累計トークンを畳み込まない）。古い世代の turn ループが残っていても、世代が変わるので `currentWorker` が偽になって抜ける |
| r2 | turn の終わりに direct chat を見たとき送る印を立てない（MEDIUM） | `internal/orchestrator/lifecycle.go` の `decideAfterTurn` の direct chat の枝と、`internal/orchestrator/reconcile.go` の `updateDirectChatMode` の段2 | 枝で `setNeedsPrompt` を立てる。**direct chat に入れば `wakeRuns` が飛ばす。**入らずに戻れば次の巡回で送る。**ただし入ってから戻ったときは、段4 の「書き込みが終わってから送る印を立てる」を守るため、抜けた時点でこの印を下ろす**（`takeNeedsPrompt`）。下ろさないと、hold と `running_state` を書く前に `wakeRuns` が送る |
| r3 | 門5 が Status ごとの上限まで当てる（MEDIUM） | `internal/orchestrator/dispatch.go` に全体の枠だけを見る関数を分け、`internal/orchestrator/directchat.go` の門5 から呼ぶ | 設計 3-83c の門5 は `agent.max_concurrent_agents` だけ。通常の着手（`dispatchCandidates`）は今までどおり `freeSlotBlocker` を使う |
| r4 | `failRun` と打ち切りの上限の枝に、引き渡しの通知の直前の打ち切りが無い（LOW） | `internal/orchestrator/lifecycle.go` の `failRun` と `abandonRunClaimed` のリトライを使い切った枝 | `finishRunClaimed` と同じく `UpdateStatus` のあと・`postHandoffComment` の直前に置く。`noteFailure` はその前で数える（Status は `protectedStates` が守っている）|
| r5 | `ensureAgentComment` の段7 の直前に見ない（LOW） | `internal/orchestrator/comment.go` | 段6 の `confirmStartup` を抜けたあと、`agent.prompt` の直前に1つ足す。当たれば r1 の枝を通る |
| r6 | CLAUDE.md の cli.go のリンク3本の着地先が違う（LOW） | `CLAUDE.md` の3本だけ | 着地先: `parseErrorExitCode` の関数全体、`runHook` の注記「2 を返してはならない」の2行、`--socket` と `--pending-dir` の欠落と相対パスを exit 1 にしている範囲。**cli.go の行数が変わらないことを確かめてから直す**（このあと触るのはコメント1行の数字だけ）|
| r7 | 「8つのキー」が残る（実際は9つ）（LOW） | `docs/FAQ.md`・`docs/bug_details.md`・`docs/plans/continuo_design.md` の本文・`internal/cli/cli.go` のコメント・`internal/scaffold/fill.go`・`internal/scaffold/update.go`・`test/internal/scaffold/statuses_test.go`・`docs/spec/usecases/` の RUCM | **巻き込まないもの。**設計 3-32d の見出し（題名）。RUCM の基本フロー16 と、その否定の分岐（`VALIDATES THAT WORKFLOW.md に書き換える対象の8つのキーがあり…`）。**16 は「無いと止まるキー」の数を言っており、`direct_chat_state` は無くても止めない（`statusKeys` の `optional`）ので8のままが正しい。**RUCM を直したら CFG を生成し直し、テストのマーカーのハッシュを揃える |
| r8 | doctor の注記「ここで出さないと、どこにも出ない」（LOW） | `internal/doctor/status_names.go` | 巡回も起動後に WARN を1回出す（`candidateStates` の `noteDirectChatMissing`）。**doctor の一覧に並ぶのはここだけ**、と書き直す |
| r9 | FAQ に戻す以外の抜け方が無い（LOW） | `docs/FAQ.md` の direct chat の節 | `Blocked` / `In Review` へ動かすと pane を閉じ worktree は残す。`Done` へ動かすと pane を閉じ `cleanup.on_states` なら片付ける（未 push があれば断る）。**チャットの中で PR をマージして issue が閉じると、カンバンの自動化が `Done` へ動かすので pane が閉じる**。`Ice Box` はコメントを1件書いて止める（設計 3-83g） |
| r10 | PR の本文の hook の段落が `turn.go` の変更を少なく書く（LOW） | PR #267 の本文 | `turn.go` の差分のコード行を全部並べて書き直す（待ちを打ち切るコンテキスト・ループの先頭の2つ・送る直前の確認・待ちのあと `switch` の手前・esc の直前）|
| r11 | 打ち切りの呼び出し元などにテストが無い（LOW） | `test/internal/orchestrator/` | r1 と r5 を `ensureAgentComment` から通す。r2 を turn の終わりから通す。r3・`Done` へ直接抜けたとき成果のコメントを書かせないこと・閉じる集合で作業中でない Status では残すこと |

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
| r5 | `internal/orchestrator/comment.go` の段7 の直前。設計 3-83f の地点の列挙にも段7 を足した |
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
| `TestDirectChat_Doneへ直接抜けたら成果のコメントを書かせに行かない` | 3-83g の `Done` の行 | — |
| `TestDirectChat_閉じる集合のpaneは作業中でないStatusでは閉じない` | 3-83f の閉じる集合 | — |

### 直さずに残したもの（実装レビュー1周目）

- **設計 3-32d の見出し「8つのキーである」。**題名は巻き込まない（依頼の決まり）。本文と表は9つへ直した
- **RUCM の基本フロー16 と、その否定の分岐（CFG の条件文を含む）。**16 は「無いと止まるキー」の数を言っており、`direct_chat_state` は無くても止めないので8のままが正しい

### テストの結果（実装レビュー1周目）

`sh scripts/test-like-ci.sh`（2026-09-28 JST。`-race` あり）: 終了コード 0、`grep -c "^FAIL"` = 0、`grep -c "^--- FAIL"` = 0。
`go vet ./...`: 終了コード 0。`sh scripts/check-rucm.sh --strict`: 終了コード 0（[W1] の警告は前から在るもの）。

## 実装レビュー2周目で直すもの

**言いたいこと。**判断票（PR #267 の実装レビュー2周目）で「直す」と決めた6件を、直す前に全部並べる。

| # | 指摘 | 直す場所 | 影響範囲と確かめたこと |
| --- | --- | --- | --- |
| s1 | 控えの Status が Direct Chat のまま送る印で送る（HIGH） | `internal/orchestrator/orchestrator.go` の `wakeRuns`、`internal/orchestrator/turn.go` の turn ループの先頭、`internal/orchestrator/directchat.go` の `letGoOfDirectChatAsync` と `finishDirectChatSetup` の戻った枝、設計 3-83f の表と「印で見る」の段落 | **送る印を見て送る場所**（`takeNeedsPrompt` の呼び出し元）は `wakeRuns` と `updateDirectChatMode` の2つで、送るのは `wakeRuns` だけ。turn ループは起こされたあと先頭で送るかを決める。この2か所で「印」に加えて「控えの Status（`rs.issue().State`）が `direct_chat_state`」でも送らない。**送る印は下ろさない**（残せば、作業中へ戻した巡回で `reconcileRunning` が控えを上書きしたあと送る。1周目で守った経路）。turn ループの先頭で控えだけが当たったときは送る印を立て直して抜ける（起こされたときに `wakeRuns` が下ろしているため）。`letGoOfDirectChatAsync` は `leaveDirectChatMode` の前に `takeNeedsPrompt` で下ろす。用意の段3 の戻った枝は、控えが用意の段1 の Direct Chat のままなので、送る印を立てる前に控えを戻った先の Status へ書き換える（書き換えないと次の巡回まで送られない） |
| s2 | 用意の失敗の上限で `failure_state` を書けないと続く（MEDIUM） | `internal/orchestrator/directchat.go` の `failDirectChatSetup` と門7、設計 3-83c の門7 と 3-83d の用意の段2 | 判定を通常の着手の `skipByFailure` と同じ「回数が `agent.max_retries` を超えたら」にそろえる（`agent.max_retries: 0` なら1回目で書く）。門7 で上限を超えた issue は用意せず、書く経路だけを巡回のループの外でやり直す。記録は候補から外れた巡回で消える（`forgetDirectChatSetupFailuresNotIn`）ので、書けたあとはやり直さない |
| s3 | 戻したときの `running_state` の書き込みが拒否リストだけ（LOW） | `internal/orchestrator/directchat.go` の `writeRunningStateOnReturn`、設計 3-83g の表 | 書く経路（`writeDirectChatFailure`）と同じく、拒否リストを「選択肢のうち `dispatch_state` 以外の全部と空」にして、`UpdateStatus` が書く直前に取り直した値が `dispatch_state` のときだけ書く。GraphQL は増やさない |
| s4 | ずれた行番号のリンク（LOW） | `docs/plans/impl/issue144_branch_and_push.md`・`docs/plans/impl/issue134_136_140_blocked_notice.md`・`CLAUDE.md` の `settings.go#L352` | コードを直し終えてから、着地先の中身で1本ずつ確かめる |
| s5 | 「5つの役割」の数え残し（LOW） | `internal/setup/assign.go`（3件）・`internal/setup/setup.go`・`internal/cli/cli.go` のコメント、ほか setup が尋ねる数を言っている行 | 必須の5つを指す行（`Complete`・README の5行の表を指す行・テストの失敗文）は残す |
| s6 | 書き戻しを断られた理由のコメントが direct chat を知らない（LOW） | `internal/orchestrator/unknownstate.go` の2件 | コメントの文だけ |

**hook の規則（CLAUDE.md の6）との当たり。**検知の網に掛かるのは `turn.go`・`orchestrator.go`・`internal/cli/cli.go`。
`turn.go` は turn ループの先頭で「送るか」を決める条件を1つ足すだけで、turn の終わりの判定（`confirmTurnEnd`・`awaitStop`・`awaitHook`）・hook の受け口・送る内容は変えない。
`orchestrator.go` は `wakeRuns` だけ、`cli.go` はコメントだけ。4つの定義のどれにも当たらない。

### 直した場所（実装レビュー2周目）

| # | どこ |
| --- | --- |
| s1 | `internal/orchestrator/directchat.go` の `cardInDirectChat`（新設）・`letGoOfDirectChatAsync`（`leaveDirectChatMode` の前に `takeNeedsPrompt`）・`finishDirectChatSetup` の戻った枝（`setIssueState`）。`internal/orchestrator/orchestrator.go` の `wakeRuns`。`internal/orchestrator/turn.go` の turn ループの先頭。設計 3-83f の表の2行と、その下の「印で見る」の段落 |
| s2 | `internal/orchestrator/directchat.go` の `directChatSetupLimitBody`（新設。`回数 > agent.max_retries`）・`failDirectChatSetup`・門7。設計 3-83c の門7 の行と 3-83d の用意の段2 |
| s3 | `internal/orchestrator/directchat.go` の `statusesOtherThan`（新設。`writeDirectChatFailure` の拒否リストの作り方を切り出した）・`writeRunningStateOnReturn`。設計 3-83g の `dispatch_state` の行 |
| s4 | `docs/plans/impl/issue144_branch_and_push.md` の 421・422・428・429・432・435・455・636・1077 行、`docs/plans/impl/issue134_136_140_blocked_notice.md` の 298〜303・830・1211・1217 行、`CLAUDE.md` の `settings.go` のリンク（`358-359`）。あわせて、この周のコードの変更でずれた `CLAUDE.md` の `pendingDir`・`issue134_136_140_blocked_notice.md` の `RunView`・`issue166_stop_hook_block.md` の3本を、差分の行の対応で振り直した |
| s5 | `internal/setup/assign.go`（93・110・238 行付近）・`internal/setup/setup.go`・`internal/cli/cli.go` のコメント。setup が尋ねる数を言っている `README.md`（2行）・`README.ja.md`（2行）・`install.sh`・`internal/i18n/messages/ja.json` と `en.json` の使い方の setup の行・`internal/scaffold/fill.go` の `Statuses` の説明2行・`internal/i18n/keys.go` の setup の文言の説明 |
| s6 | `internal/orchestrator/unknownstate.go` の2件。同じ誤りの `internal/orchestrator/lifecycle.go` の `rewriteAndDecide` のコメントも直した |

**残したもの（s5）。**README の「5つの役割に一度だけ対応づけます」（すぐ上の5行の表を指す）・`Complete` と必須の5つを言うコメント・
テストの失敗文・設計 3-32 の見出しと本文（setup の設計の節。見出しは巻き込まない）・RUCM（1周目と同じ理由）。

**残したもの（s4）。**`docs/plans/impl/issue134_136_140_blocked_notice.md` の `handoff.go` へのリンク（204・462・698・703・1209・1210・1223 行）は main の時点から外れているが、
レビュワーの一覧に無いので触っていない。

### 足したテスト（実装レビュー2周目）

| テスト | 何を確かめるか | 直しを外すと |
| --- | --- | --- |
| `TestDirectChat_用意の失敗が上限を超えたらfailure_stateへ動かして理由を書く`（既存の「達したら」を置き換えた） | s2。`agent.max_retries: 0` で1回目の失敗から書く | 比べ方を戻すと落ちる（確かめた） |
| `TestDirectChat_上限を超えたときに書けなかったら次の巡回で用意せずに書き直す` | s2。書き込みの失敗を門7 が次の巡回で書き直し、用意はやり直さない | 門7 の書き直しを外すと落ちる（確かめた） |
| `TestDirectChat_turnの終わりに引き取りを見たあと取り直しに失敗した巡回では指示を送らない` | s1。取り直しの失敗の入口。作業中へ戻したら送る | `wakeRuns` と turn ループの先頭の判定を外すと落ちる（確かめた） |
| `TestDirectChat_turnの終わりに引き取りを見たあと手を離す巡回では指示を送らない` | s1。手を離す入口 | 外しても通る。控えの Status の判定が同じ巡回の `wakeRuns` を止めるので、`letGoOfDirectChatAsync` の `takeNeedsPrompt` は二重の守りである |

### テストの結果（実装レビュー2周目）

`sh scripts/test-like-ci.sh`（2026-09-28 JST。`-race` あり）: 終了コード 0、`ok` 62件、`grep -c "^FAIL"` = 0、`grep -c "^--- FAIL"` = 0。
`go vet ./...`: 終了コード 0。
**使い方の文言（`messages/ja.json`）を直したので、`messages/en.json` の `_source_sha256` を入れ直した**（入れ直す前の1回目は `TestMessages_英語の資源が正の資源の版に追いついている` が落ちた）。

## 実装レビュー3周目で直すもの

**言いたいこと。**判断票（PR #267 の実装レビュー3周目）で「直す」と決めた7件のうち、PR の本文（メインが直す）を除く6件を、直す前に全部並べる。

| # | 指摘 | 直す場所 | 影響範囲と確かめたこと |
| --- | --- | --- | --- |
| u1 | 終わらせている最中の run へ続きの指示が送られる（MEDIUM） | `internal/orchestrator/runstate.go` に `isTerminating` を足す。`internal/orchestrator/orchestrator.go` の `wakeRuns`。`internal/orchestrator/lifecycle.go` の `finishRunAsync` と `stopAndReleaseAsync` | **送る印を立てる6箇所**は `decideAfterTurn` の direct chat の枝（lifecycle.go）・turn ループの先頭の2つ（turn.go。控えが direct chat のときと、待ちのコンテキストが切れているとき）・`startRunFromWorktree` の段11 の失敗（dispatch.go）・用意の段3 の戻った枝と `returnFromDirectChatAsync`（directchat.go）。このうち、印を立てたあと巡回より先に Status が `terminal_states` や引き渡しへ動きうるのは前の2つ。**`wakeRuns` の判定の順は「印・控え・用意中・終端・担当の確認・待つ印・送る印」**にし、終端の権利（`terminating`）を持つ run を起こさない。担当の確認より前に置くのは、終わらせている run で `stopBecauseHandoffLost` を走らせないため。`finishRunAsync` と `stopAndReleaseAsync` は終端の権利を取ったその場で送る印を下ろす。**下ろしても失うものは無い。**`endTerminal` で run が続くのは打ち切り（`abortTerminalForHuman`。direct chat の印が立っているときだけ）とリトライの枝（`abandonRunClaimed`。この2つの関数からは来ない）だけで、前者は作業中へ戻した巡回の段4 が送る印を立て直す |
| u2 | direct chat を抜けても turn の終わりを待つ印が残る（MEDIUM） | `internal/orchestrator/runstate.go` の `leaveDirectChatMode` | **待つ印を立てる3箇所**は turn ループの `turnTransient`（turn.go）・`startRunFromWorktree` の `ErrStartupBusy`（dispatch.go）・`wakeRuns` の起こし直し（orchestrator.go）。復元の `Adopt` は direct chat の run には立てない（restore.go が `AwaitTurnEnd && !directChat` で渡す）。**下ろしたあと、作業中へ戻す経路は段4 の送る印と、送る直前の確認（`busyCheckBeforeSend`。応答中なら送らず待つ）が受け持つ。**用意中に下ろす道（段2・担当者が他人）と用意の段3 の戻った枝は、そもそも待つ印を立てない（用意の段2 は `ErrStartupBusy` でも立てない）ので影響しない |
| u3 | 用意中に巡回が見た Status の時刻が処理した時刻（LOW） | `internal/orchestrator/reconcile.go` の `reconcileRunning` と `updateDirectChatMode` | `FetchIssuesByIDs` が返った直後の時刻を取り、`updateDirectChatMode` へ渡して `notePreparingSeen` に記録する。用意の段3（`finishDirectChatSetup`）の自分の時刻も「取り直しが返った直後」なので、比べる2つの時刻の取り方がそろう。stall の時計の引き直し（`resetStallClock`）はいままでどおり処理した時刻 |
| u4 | 用意の失敗の上限で書く処理が2本重なる（LOW） | `internal/orchestrator/directchat.go` の `failDirectChatSetup`・門7・`directChatSetupFailure` | `failDirectChatSetup` は書かない（数えて、pane を閉じ、印を外すだけ）。**書くのは門7 の1箇所だけ**にする。専用の記録に「書いている最中」を1つ持ち、門7 は立っていれば goroutine を立てない。書き終えたら下ろす（記録が消えていれば何もしない）。**上限を超えた回の書き込みは、次の巡回（既定30秒）の門7 になる。**設計 3-83d の用意の段2 と 3-83c の門7 の文をこれに合わせる |
| u5 | 門7 の書き直しが門5・門6 の後ろ（LOW） | `internal/orchestrator/directchat.go` の `prepareDirectChatPanes` | 上限を超えた issue の書き直しは枠も `preflight` も要らないので、門5 の前（門4 のあと）へ移す。間隔の判定（門7 の後半）は今の位置のまま。設計 3-83c の門の一覧に位置を書く |
| u6 | issue166 の記録のリンク3本（LOW） | `docs/plans/impl/issue166_stop_hook_block.md` の3本 | 着地先: `turnLoop` の `awaitFirst` の枝、`max_dispatch_turns` の枝、`isTurnBoundaryHook`。**コードを直し終えてから、着地先の中身で確かめる。**あわせて、この周のコードの変更でずれるリンク（CLAUDE.md の `pendingDir` ほか）を差分の行の対応で振り直す |

**hook の規則（CLAUDE.md の6）との当たり。**検知の網に掛かるのは `orchestrator.go`（`wakeRuns`）・`runstate.go`（`leaveDirectChatMode` と読み出し1つ）・`reconcile.go` は網の外。
`wakeRuns` が turn ループを起こす条件を1つ足すだけで、turn の終わりの判定（`confirmTurnEnd`・`awaitStop`・`awaitHook`）・hook の受け口・送る内容・`OnHook`・`pendingDir` は変えない。
`leaveDirectChatMode` が下ろす待つ印は本体の中の状態で、hook の側から見える違いは無い。4つの定義のどれにも当たらない。

### 直した場所（実装レビュー3周目）

| # | どこ |
| --- | --- |
| u1 | `internal/orchestrator/runstate.go` の `isTerminating`（新設）、`internal/orchestrator/orchestrator.go` の `wakeRuns`（用意中の判定の次、担当の確認の前）、`internal/orchestrator/lifecycle.go` の `finishRunAsync` と `stopAndReleaseAsync`（終端の権利を取ったその場で `takeNeedsPrompt`）。設計 3-83f の `wakeRuns` の行 |
| u2 | `internal/orchestrator/runstate.go` の `leaveDirectChatMode`（`awaitTurnEnd` を下ろす）。設計 3-83i |
| u3 | `internal/orchestrator/reconcile.go` の `reconcileRunning`（`fetchedAt`）と `updateDirectChatMode`（引数を足し、`notePreparingSeen` へ渡す）。設計 3-83b の段2 |
| u4 | `internal/orchestrator/directchat.go` の `directChatSetupFailure.Writing`（新設）・`beginDirectChatSetupLimitWrite` と `endDirectChatSetupLimitWrite`（`directChatSetupLimitBody` を置き換えた）・`failDirectChatSetup`（書かなくした）。設計 3-83c の門7 と 3-83d の用意の段2 |
| u5 | `internal/orchestrator/directchat.go` の `prepareDirectChatPanes`（上限の判定を門4 と門5 のあいだへ移した。間隔の判定は元の位置）。関数の門の一覧 |
| u6 | `docs/plans/impl/issue166_stop_hook_block.md` の3本（`turn.go#L164`・`turn.go#L174-L185`・`orchestrator.go#L1446-L1455`）。この周のコードの変更でずれた `CLAUDE.md` の `pendingDir`・設計の `reconcile.go` の1本・`issue134_136_140_blocked_notice.md` の7本・`docs/spec/event_process_system.md` の3本を、差分の行の対応で振り直した |

### 足したテスト（実装レビュー3周目）

| テスト | 何を確かめるか | 直しを外すと |
| --- | --- | --- |
| `TestDirectChat_turnの終わりに引き取りを見たあと終わらせる処理が走っているあいだは指示を送らない` | u1 | `wakeRuns` の判定と `takeNeedsPrompt` の両方を外すと落ちる（確かめた）。片方だけ外しても通る（二重の守り） |
| `TestDirectChat_turnの終わりを待つ印はdirectChatを抜けるときに下ろす` | u2 | 外すと落ちる（確かめた） |
| `TestDirectChat_上限を超えたissueへ書いている最中の巡回では2本目を立てない` | u4 | `Writing` の判定を外すと落ちる（確かめた） |
| `TestDirectChat_用意の失敗が上限を超えたらfailure_stateへ動かして理由を書く`（直した） | u4。落ちた巡回ではカードを動かさず、次の巡回の門7 が書く | — |
| `TestDirectChat_上限を超えたときに書けなかったら次の巡回で用意せずに書き直す`（直した） | u4。門7 が初めて書く巡回で失敗させ、その次の巡回で書き直す | — |

テスト用の herdr に `StopDropping`（`DropConnection` を外す）を足した。

### テストの結果（実装レビュー3周目）

`sh scripts/test-like-ci.sh`（2026-09-28 JST。`-race` あり）: 終了コード 0、`ok` 62件、`grep -c "^FAIL"` = 0、`grep -c "^--- FAIL"` = 0。
`go vet ./...`: 終了コード 0。

## 実装レビュー4周目で直すもの

**言いたいこと。**判断票（PR #267 の実装レビュー4周目）で「直す」と決めた7件のうち、PR の本文（メインが直す）を除く分を、直す前に全部並べる。

| # | 指摘 | 直す場所 | 影響範囲と確かめたこと |
| --- | --- | --- | --- |
| v1 | CI の `test (ubuntu-latest)` が赤い（HIGH） | `test/internal/orchestrator/direct_chat_setup_test.go` の `TestDirectChat_バックオフ明けに人間が引き取っていたら書いた担当者を消し戻さない` | **手元で再現した。**`go test -race -cpu 1,2 -count=10 -run 'TestDirectChat' ./test/internal/orchestrator/` で20回中10回落ちる（落ちるのはこの1本だけ）。`redispatch` の段2 は goroutine なので、印が外れるのが同じ巡回の direct chat の1パスより後になりうる。設計 3-83c の門1 の表は「次の巡回で」。**待ちの中で巡回を回す形**（ほかのテストと同じ）にし、コメントの「同じ巡回の」を「次の巡回の」へ直す。**ほかの direct chat のテストで巡回1回の効果を前提にしているもの**を洗い、同じ形に直す（用意の goroutine は同じ巡回の1パスが立てるので、1回の巡回で足りる。見るのは「別の goroutine が印を外すのを待ってから次の巡回が拾う」形だけ） |
| v2 | 用意の最中に戻したとき、動いている Claude Code へ1回目の指示を送る（MEDIUM） | `internal/orchestrator/dispatch.go` の `startRunFromWorktree`（direct chat で `ErrStartupBusy` を見たら、それと分かる値を返す）、`internal/orchestrator/directchat.go` の `setUpDirectChat` と `finishDirectChatSetup` の戻った枝 | 戻った枝で、既に動いていたなら**送る印ではなく turn の終わりを待つ印**（`setAwaitTurnEnd`）を立て、送る直前の確認の印（`setBusyCheckBeforeSend`）も立てる。通常の着手の `ErrStartupBusy` と同じ道（`wakeRuns` が `awaitFirst` の turn ループを立て、`SendFirstPrompt` は立ったままなので、走っている turn が終わった次の周で1回目の本文を送る）。**送る印は立てない**（両方立てると、turn ループが走っているあいだ `wakeRuns` が「既に走っている」で WARN を出し続ける）。入れた枝（`setupEnter`）では何も変えない（人間が話す）。`startRun`（通常の着手）の戻り値は変えない |
| v3 | 用意の段2 が段4〜段8 で落ちると、シェルの pane が残る（MEDIUM） | `internal/orchestrator/dispatch.go` の `startRunFromWorktree`（段3 の直後） | **direct chat の用意で、`worktree.open` が新しく開いた workspace（`AlreadyOpen` が偽）のときだけ**、`Prepare` が返した `HerdrPaneID` を `setPaneID` で控える。既に開いていた workspace の pane は人間のものでありうるので控えない（門4 がそもそも止めるが、二重の守り）。段8 は `resolvePane` の値で上書きする（同じ pane）。後始末（`closeDirectChatSetupPane`）は `PaneID` を閉じるので、そのまま効く。打ち切り（`abortTerminalForHuman`）は用意中の run には来ない（用意中は印が立たない）。`agent.start` 前なので `paneState` は「起動済みでない」を返し、来ても閉じる側へ倒れる |
| v4 | 門4 と用意の段2 で pane の見つけ方が違う（MEDIUM） | `internal/orchestrator/directchat.go` の `directChatPanes` と `directChatPaneExists`、設計 3-83c の門の表の4行目 | 門4 は「cwd がその worktree の pane がある」**または**「その worktree を開いている herdr workspace（`workspace.list` の `checkout_path` で引く。用意の段2 の `worktree.open` が返すのと同じ workspace）に pane が1枚でもある」で当たる。**`pane.list` と `workspace.list` は1パスで1回ずつ**。どちらかが引けなければ「引けなかった」として用意しない（「pane が無い」と混ぜない）。cwd の照合は残す（別の workspace から worktree へ入った pane も触らない側に倒す）。`paneMapByCwd` は復元が使うので変えない |
| v5 | 担当者の人数による書き込みが重なりうる（LOW） | `internal/orchestrator/directchat.go` の `writeDirectChatAssigneeFailureAsync`、`internal/orchestrator/orchestrator.go` の欄と初期化 | issue ごとの「書いている最中」の集合（`o.mu` が守る）を持ち、立っていれば goroutine を立てない。書き終えたら下ろす。呼び出し元は門3 と巡回の段1 の2つで、どちらもこの関数を通る |
| v6 | 上限を超えたあとの書き込みに成功しても記録が残る（LOW） | `internal/orchestrator/directchat.go` の `writeDirectChatFailure`（書けたかを返す）と門7 の goroutine | 実際に書けた（`Wrote`）ときに専用の記録を消す。`endDirectChatSetupLimitWrite` は記録が無ければ何もしないので、順序はどちらでもよい。担当者の人数の書き込みは記録を持たないので戻り値を使わない |
| v7 | ずれたリンク（LOW） | `CLAUDE.md` の `continuo_design.md#L10260`、`docs/plans/impl/issue142_144_branch_mismatch.md:215`、`docs/plans/release_v0114.md:70-71`、`docs/spec/event_process_system.md:121` | **コードと設計を直し終えてから、着地先の中身で1本ずつ確かめる。**CLAUDE.md の行は「エージェントが自分で `gh` から `In Progress` → `Blocked` を動かす」経路で、origin/main でも遷移表の「同上 / エージェント自身」の行を指している。そこへ向ける。この周の変更でずれる他のリンク（CLAUDE.md の `pendingDir` ほか）も差分の行の対応で確かめる |

**hook の規則（CLAUDE.md の6）との当たり。**検知の網に掛かるのは `orchestrator.go`（欄1つと初期化1行）だけの見込み。
`wakeRuns`・`OnHook`・`pendingDir` は触らない。`turn.go`・`runstate.go`・`hookinput.go`・`settings.go` は触らない。
v2 は既存の `setAwaitTurnEnd` と `setBusyCheckBeforeSend` を呼ぶだけで、turn の終わりの判定・hook の受け口・送る内容は変えない。4つの定義のどれにも当たらない。

### 直した場所（実装レビュー4周目）

| # | どこ |
| --- | --- |
| v1 | `test/internal/orchestrator/direct_chat_setup_test.go` の `TestDirectChat_バックオフ明けに人間が引き取っていたら書いた担当者を消し戻さない`（待ちの中で巡回を回す）。同じファイルのほかのテストには、別の goroutine が印を外すのを同じ巡回で待つ形のものが無かった（`-race -cpu 1,2 -count=10` で direct chat のテストを回して0件） |
| v2 | `internal/orchestrator/dispatch.go` の `startRunFromWorktree`（direct chat の `ErrStartupBusy` の着地で `ErrStartupBusy` を返す）、`internal/orchestrator/directchat.go` の `setUpDirectChat`（`ErrStartupBusy` を失敗にせず段3 へ渡す）と `finishDirectChatSetup` の戻った枝（`busy` なら `setBusyCheckBeforeSend` と `setAwaitTurnEnd`）。設計 3-83d の外れ方の表の1行目 |
| v3 | `internal/orchestrator/dispatch.go` の `startRunFromWorktree` の段3 の直後（`AlreadyOpen` が偽のときだけ `setPaneID(prepared.HerdrPaneID)`）。設計 3-83d の用意の段2 |
| v4 | `internal/orchestrator/directchat.go` の `directChatPaneIndex`（新設）・`directChatPanes`・`directChatPaneExists`、`internal/orchestrator/orchestrator.go` の `HerdrClient` に `WorkspaceList`、テストの stub に `WorkspaceList`。設計 3-83b の pane の写像の段落と 3-83c の門の表の4行目 |
| v5 | `internal/orchestrator/directchat.go` の `writeDirectChatAssigneeFailureAsync`、`internal/orchestrator/orchestrator.go` の `directChatAssigneeWriting`（欄と初期化）。設計 3-83j の代償の表の「書く経路は、別の機械とは重なりうる」の行（同じ機械では重ねないに直した） |
| v6 | `internal/orchestrator/directchat.go` の `writeDirectChatFailure`（書けたかを返す）と門7 の goroutine（書けたら `forgetDirectChatSetupFailure`）。設計 3-83d の用意の段2 の「専用の記録を消す」 |
| v7 | `CLAUDE.md` の 4-1 の遷移表へのリンク（`#L10264`。「同上 / エージェント自身」の行）、`docs/plans/impl/issue142_144_branch_mismatch.md` の 5-3 の markdown ブロック（`#L10783-L12215`）、`docs/plans/release_v0114.md` の 5-3m（`#L12356-L12372`）と 5-3n（`#L12951-L12961`）、`docs/spec/event_process_system.md` の `finishRunAsync`（`lifecycle.go#L602`）。この周のコードの変更でずれた `CLAUDE.md` の `pendingDir`・`issue134_136_140_blocked_notice.md` の7本・`issue142_144_branch_mismatch.md` の `dispatch.go` の1本・`issue144_branch_and_push.md` の2本・`issue166_stop_hook_block.md` の1本を、差分の行の対応で振り直した |

**残したもの（v7）。**`docs/plans/release_v0114.md` の 69 行目（3-74c）と `docs/spec/event_process_system.md` の 122 行目（`stopAndReleaseAsync`）は着地先がずれているが、判断票の一覧に無いので触っていない。

### 足したテスト（実装レビュー4周目）

| テスト | 何を確かめるか | 直しを外すと |
| --- | --- | --- |
| `TestDirectChat_用意の段2が段8より前で落ちたらworktreeOpenが開いたpaneを閉じる` | v3 | 落ちる（確かめた。`pane.close` 0 回） |
| `TestDirectChat_worktreeのworkspaceにcwdの違うpaneがあれば用意しない` | v4。その pane が無くなった巡回では用意する（空振りでない証拠） | 落ちる（確かめた。`agent.start` 1 回） |
| `TestDirectChat_用意中に戻されたときClaudeCodeが既に動いていればturnの終わりを待ってから1回目の本文を送る` | v2 | 落ちる（確かめた。`Stop` の前に `agent.prompt` 1 回） |
| `TestDirectChat_担当者が1人でないカードへ書いている最中の巡回では2本目を立てない` | v5 | 落ちる（確かめた。止めている間に `UpdateStatus` 1 回） |
| `TestDirectChat_上限を超えた書き込みに成功したら記録を消す` | v6 | 落ちる（確かめた。戻した巡回で用意し直さない） |

### テストの結果（実装レビュー4周目）

`sh scripts/test-like-ci.sh`（2026-09-28 JST。`-race` あり）: 終了コード 0、`ok` 62件、`grep -c "^FAIL"` = 0、`grep -c "^--- FAIL"` = 0。
`go vet ./...`: 終了コード 0。
`go test -race -cpu 1,2 -count=10 -run 'TestDirectChat' ./test/internal/orchestrator/`: 直す前は `--- FAIL` 10件（全部 v1 のテスト）、直したあとは終了コード 0、`^--- FAIL` 0件・`^FAIL` 0件。
**CI の結果は、push したあとメインが確かめる**（この作業では push していない）。

## 実装レビュー5周目で直すもの

**言いたいこと。**判断票（PR #267 の実装レビュー5周目）で「直す」と決めた2件を、直す前に全部並べる。

| # | 指摘 | 直す場所 | 影響範囲と確かめたこと |
| --- | --- | --- | --- |
| w1 | 閉じる集合が pane を cwd だけで探す（MEDIUM） | `internal/orchestrator/reconcile.go` の `closeOrphanPane` | **`includeUnnamed` が真のときだけ**、`workspace.list` でその worktree を開いている herdr workspace（`checkout_path` をシンボリックリンク解決して照合。門4 の `directChatPanes` と同じ見方）を引き、その workspace に属する pane も cwd を問わず閉じる。着手の段8 の `resolvePane` は workspace の中の1枚を cwd を見ずに使うので、同じ見方で閉じないと、別のディレクトリへ移ったシェルへ `agent.start` が届く。`workspace.list` を引けなければ WARN を出して偽を返す（集合に残し、次の巡回でやり直す）。**`includeUnnamed` が偽の通常の道（3-9 の手順7b）は変えない**（`workspace.list` も投げない）。リポジトリの親 workspace は `checkout_path` がリポジトリ本体なので当たらない。呼び出し元は `reconcileOrphanWorktrees` の1箇所だけ |
| w2 | FAQ の段2 が門4 の新しい判定を伝えていない（LOW） | `docs/FAQ.md` の「pane が来ないとき」の段2 | 「その worktree の pane」に、その worktree を開いている herdr の workspace の pane（別のディレクトリへ移ったシェルも含む）も数えることを1文足す |

**hook の規則（CLAUDE.md の6）との当たり。**触るのは `reconcile.go` と `docs/FAQ.md` とテストだけで、検知の網のファイルには触れない見込み。hook の引数・宛先・約束・Claude Code へ返すものは変えない。

### 直した場所（実装レビュー5周目）

| # | どこ |
| --- | --- |
| w1 | `internal/orchestrator/reconcile.go` の `closeOrphanPane`（`includeUnnamed` が真のときだけ `workspace.list` を引き、その worktree を開いている workspace の pane も cwd を問わず閉じる。引けなければ偽を返して集合に残す）と、その関数のコメント |
| w2 | `docs/FAQ.md` の「pane が来ないとき」の段2 |

**行番号のリンク。**`reconcile.go` と `FAQ.md` へ行番号で向くリンクは、`continuo_design.md` の `reconcile.go#L104-L105`（変えた行より前）と `01_inventory_repo.md` の `FAQ.md#L201-L250`（行数は変わらない）だけで、どちらもずれない。

### 足したテスト（実装レビュー5周目）

| テスト | 何を確かめるか | 直しを外すと |
| --- | --- | --- |
| `TestDirectChat_閉じる集合はworkspaceの中で別のディレクトリへ移ったシェルも閉じてから着手する` | w1。direct chat のあいだは閉じず、作業中へ戻した巡回で、workspace の中で別のディレクトリへ移ったシェルを閉じてから `agent.start` を投げる | 落ちる（確かめた。`agent.start` の宛先がそのシェル） |

**このテストは、着手が1回目の指示を送り終えるまで待ってから返る。**`agent.start` を見た直後に返す形では、`-race -cpu 1,2 -count=10` の20回に1回、後始末と走っている着手の間でデータ競合が出た（実測 2026-09-28）。待つ形にしてからは、このテストだけの80回と direct chat の全体の20回で0件。

### テストの結果（実装レビュー5周目）

`sh scripts/test-like-ci.sh`（2026-09-28 JST。`-race` あり）: 終了コード 0、`ok` 62件、`grep -c "^FAIL"` = 0、`grep -c "^--- FAIL"` = 0。
`go vet ./...`: 終了コード 0、出力0行。
`go test -race -cpu 1,2 -count=10 -run 'TestDirectChat' ./test/internal/orchestrator/`: 終了コード 0、`^FAIL` 0件・`^--- FAIL` 0件。
**hook の規則。**この周の差分（`docs/FAQ.md`・`docs/plans/impl/issue263_direct_chat.md`・`internal/orchestrator/reconcile.go`・テスト）は検知の網に1本も掛からない。網が返す `cli.go`・`orchestrator.go`・`runstate.go`・`turn.go` は前の周までの commit のもので、この周では触っていない。
