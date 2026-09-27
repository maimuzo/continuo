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
| b1 | 候補を2つに分け、direct chat のものは専用の1パス（`prepareDirectChatPanes`）へ。**先に走らせ、`dispatchAllowed` が真のときだけ** | `dispatchCandidates` の中で `directChat` の真偽値を9箇所に散らしている | |
| b2 | `dispatchCandidates` へ `directChat` を1つも渡さない。外すのは `claimForDispatch` / `runStartOrFail` / `startRun` / `handoffGate` の4本、残すのは `startRunFromWorktree` だけ | 5本とも `directChat` を受ける | |
| b3 | pane の写像はこの1パスで1回だけ作る。`agent.list` は投げない。引けなかったと pane が無いを混ぜない | `dispatchCandidates` の中で遅延して作っている（形は合っている） | |
| b4 | 出入りの段1：担当者を 3-82h の表で判定（0人/2人以上なら `failure_state` を書き、印を持てば印も立てる。ログイン名が取れない・自分1人なら印を立てる。1人で他人なら手を離す。用意中なら印を下ろすだけ）。書き込みと `pane.close` はループの外 | 担当者を見ずに印を立てるだけ | |
| b5 | 段2：用意中の run は印を下ろし、見た Status と時刻を記録するだけ（`direct_chat_state` を見たときも書く）。`o.mu` の中で判定 | 「用意中」の記録が無い | |
| b6 | 段3：抜けた先が `terminal_states` なら「直接抜けた」印を立てる。段1 で入れるときに下ろす | 印が無い | |
| b7 | 段4：続きの指示を送る印は、ループの外の後始末（`running_state`・hold）が終わってから立てる。送る直前に `agent.get` | goroutine の中で `agent.get` を先に投げ、hold を書かない | |
| b8 | 段5：Status が direct chat の run と用意中の run は `switch` へ入れない | 用意中の扱いが無い | |
| b9 | `redispatch` の入口に direct chat の検査を置かない | 置いてある | |

### 3-82c 用意するかの門

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| c1 | 門1 印を持つ → 何も出さない | 形は合う（`dispatchCandidates` の先頭） | |
| c2 | 門2 draft issue → Debug | 無い（`Dispatchable` を飛ばしたので素通り） | |
| c3 | 門3 担当者が自分1人でない（0/2人以上は書く経路、1人で他人は Debug、ログイン名が取れないなら何もしない） | 担当の持ち回り（`handoffGate`）を通している | |
| c4 | 門4 pane が1枚でもある → Debug | WARN/Debug が混ざる | |
| c5 | 門5 空きスロット → **Debug**、`clearGate` は外へ | WARN | |
| c6 | 門6 `preflight` | 形は合う | |
| c7 | 門7 用意の失敗の間隔（専用の記録） | 無い | |
| c8 | 通さない門：`pause_above_percent`・`handoffGate`・`skipByFailure`・`required_labels`・`Dispatchable` | `handoffGate` を通している | |
| c9 | 門1 の表の段4：`redispatch` の着手の段2 が `direct_chat_state` を見たら担当者を消し戻さない（入札直後の着手では消す）。段2 は取り直した Status を返す | 消し戻す。Status を返さない | |

### 3-82d 用意の段1〜段3

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| d1 | 段1 `o.claim` を直に呼び、「用意中」を立て、閉じる集合から外す。写しの Status は書き換えない | `claimForDispatch(…, true)` | |
| d2 | 段2 失敗：自分の pane を ID で閉じ、印を外し、専用の記録へ数える。上限（`agent.max_retries`）で書く経路。WARN を毎回 | 記録も上限も無い | |
| d3 | 段3 取り直し → `o.mu` の中で用意中を下ろし、新しいほうの Status で判定。入れる／戻った／印が無い／担当者が替わった・それ以外、の4通り。書き込みはロックの外。hold はここでは書かない（戻ったときだけ書く） | カードを見ずに direct chat へ入れる | |
| d4 | 取り直しが失敗したとき：入れず、自分の pane を閉じて印を外す（失敗として数えない）＝判断票6周目 | 無い | |

### 3-82e 不変条件と `UpdateStatus` の14箇所

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| e1 | 9箇所に `protectedStates()` | 済（数え直して確かめる） | |
| e2 | 書く経路：拒否リストは選択肢のうち `direct_chat_state` 以外全部。未設定（空）なら書かない。写しが空なら WARN して書かない | 無い | |

### 3-82f 印を外す道と、門だけでは足りない13箇所

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| f1 | 打ち切り（`abortTerminalForHuman`）の終え方を「`PaneID` が空でなく `agent.start` が済んでいる」で分ける | 印を残すだけ | |
| f2 | `finishRunClaimed`：入口・`postHandoffComment` の直前・`ensureAgentComment` を抜けた直後・`release` の直前 | 入口だけ | |
| f3 | `ensureAgentComment`：入口・段5・段8・`failCommentRecovery`・`failCommentRecoveryBusy` の直前。打ち切ったことを戻り値で返す。「直接抜けた」印なら入口で抜ける | 入口で印を見るだけ | |
| f4 | `abandonRunClaimed` のリトライの枝：`after_run` のあと `addRetry` の直前 | 無い | |
| f5 | `stopAndReleaseAsync`：`release` の直前 | 入口だけ | |
| f6 | `stopForUnknownStateAsync`：コメントの直前と `release` の直前 | 無い | |
| f7 | `stopBecauseHandoffLost`：先に direct chat を抜けさせる | 無い | |
| f8 | turn ループ：`turnBlocked` で subagent を待ったあと、esc の直前にもう1度見る | 無い | |
| f9 | `decideAfterTurn` の枝は direct chat へ入れない（戻るだけ） | 入れている | |
| f10 | 閉じる集合（agent 名を問わず閉じる worktree の集合）を新設 | 無い | |
| f11 | 復元の `moveToFailure`：direct chat では通知ごと投稿しない | 投稿する | |
| f12 | 「見えなくなった」ループ：用意中の run は飛ばす | 無い | |

### 3-82g・3-82h・3-82i

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| g1 | 作業中へ戻したら hold を書く（人間向けの文だけ別。印と JSON は入札の hold と同じ） | 書かない | |
| g2 | `Done` へ直接抜けたら成果のコメントを書かせない | 無い | |
| h1 | 判定の表（毎巡回・4行×印の有無） | 無い | |
| h2 | 書く経路の文面2つ（i18n） | 無い | |
| h3 | 手を離す経路 | 無い | |
| i1 | 捨てる3つ（Stop を見た時刻・この turn で hook を見たか・受け口）と時計 | 時計だけ | |

### 3-82j・3-4・3-9・3-23（再起動）

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| j1 | 段3 の取り直しに失敗した worktree は閉じずに閉じる集合へ | 閉じない（集合が無い） | |
| j2 | herdr の一覧を取れなかった worktree も閉じる集合へ | 無い | |
| j3 | direct chat で閉じない6つの道は、見送って閉じる集合へ。通知も投稿しない | 閉じないだけ | |
| j4 | 段4 `closeExtraPanes`：direct chat の worktree では閉じない。agent 名を持つ pane を引き継ぎの相手に。無ければ2枚とも残す | pane ID の小さいほう | |
| j5 | 3-9 の手順7b：閉じる集合の worktree では agent 名の無い pane も閉じる | agent 名のある pane だけ | |

### 3-82k 設定の検査

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| k1 | 7つの相手の一覧を1箇所に置き、起動時と doctor が同じものを読む | 起動時の中に直書き | |
| k2 | エラーの文面に「このキーを書いていない場合は既定値です」 | 無い | |
| k3 | doctor：front matter を読み直して重なりを `!` で出す | 無い | |
| k4 | setup：書けなかったときの見本は `  direct_chat_state: "<選んだ値>"`（飛ばしたなら `""`） | `tracker.direct_chat_state: "Direct Chat"` の形 | |
| k5 | `abandon`：`--park`・`--to`・いまの Status の3つを断る。文面は3つ。`--force` でも通さない | 2つを同じ文面で断る | |

### 雛形・文書・組み込みの指示書

| # | 設計 | 着手前のコード | 直したか |
| --- | --- | --- | --- |
| t1 | 5-2 の雛形：担当者の1文、「たいていは」、担当者を別の1人に替えると pane を閉じる（判断票6周目） | 担当者の文が無い | |
| t2 | `internal/prompt/builtin.md` の 3-1・3-2 と設計 5-3 の「全部読む」の行を外す | 入っている | |
| t3 | FAQ・upgrading | 古い版 | |

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

## hook の規則（CLAUDE.md の6）との当たり

人間が了承したのは「direct chat の run では、turn の終わりの hook を受け口（hookCh）へ流さない」の1件だけである。

## テストの結果

（未実施）

## 止まった理由

（無し）
