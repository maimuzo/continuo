# 空の Stop を受けても、エージェントが動いていれば turn は終わっていない

**対象。**#166（応答が hook に差し戻されたのに、continuo は turn が終わったと判断して次の指示を送る）。

**この文書が正である。**[docs/plans/continuo_design.md](../continuo_design.md) の 3-2（turn の終わりの判定の規則）を、
この1件の範囲で具体化したもの。**3-2 は残す。置き換えない。**足すのは 3-79（12 節）である。

---

## 1. 言いたいこと

**言いたいこと。**空の `Stop` は「turn が終わった」ではなく、
**「エージェントが止まってよいか hook に尋ねた」**である。
**尋ねられた hook の答えは continuo に届かない。**だから空の `Stop` だけでは turn の終わりを決められない。

**採る直し方は1つで、それに出口を1つ添える。**
**`settle_ms` の窓が閉じた瞬間に `agent.get` を1回だけ読み、`working` なら turn の終わりとせずに待ち直す。**
Go の関数名は `stillWorkingAfterStop` とする（6 節）。
**待ち直している間は `settle_ms` ごとに `agent.get` を読み直し、`Stop` が来ないままエージェントが動いていなければ、推測が外れたものとして turn を終える**（7 節）。

| 決めたこと | どこに書くか |
| --- | --- |
| **`agent.get` の裏取りを足す** | [internal/orchestrator/turn.go](../../../internal/orchestrator/turn.go) の `confirmTurnEnd` |
| **待ち直しから抜ける出口を1つ置く** | 同じ関数で `settle_ms` ごとに読み直すところ（7 節） |
| **`stop_hook_active` は判定にも記録にも使わない** | 10 節 |
| **`settle_ms` を伸ばす案は採らない** | 8 節（測定で否定） |

**壊れているもの3つは、この1箇所で全部消える**（5 節）。
**層を重ねない。**壊れ方が3つに見えるのは、**判定の前提が1つ間違っている**からである。

---

## 2. 前提：「差し戻し」とは何か

**言いたいこと。**Claude Code の `Stop` hook は、`{"decision":"block","reason":"…"}` を返すと
**turn を終わらせず、`reason` を指示としてエージェントへ渡し、同じ turn の続きとして応答を書き直させる。**
これを以下**差し戻し**と呼ぶ。

**公式ドキュメント**（[Hooks reference](https://code.claude.com/docs/en/hooks) の Stop の項）。

> A `decision` field holding `"block"` to prevent Claude from stopping, or `"allow"` (or absent) to let it.
>
> （訳）`decision` に `"block"` を入れると Claude が止まるのを妨げ、`"allow"` か項目なしなら止まらせる。

**hook は並行して走り、互いの答えを見られない。**continuo が張る `Stop` hook
（[docs/plans/continuo_design.md:745](../continuo_design.md#L745)）は、
**他の hook が差し戻したかどうかを知る手立てを持たない。**

**差し戻しは transcript には残る。**手元の記録で確認した1行の形（`message.content` は文字列）。

```json
{"type":"user","isMeta":true,"isSidechain":false,"promptId":"6cc8dbec-0f1e-4074-96cf-0b0e5ceec275",
 "timestamp":"2026-09-01T10:33:34.263Z","version":"2.1.252",
 "message":{"role":"user","content":"Stop hook feedback:\n返答が「初見で理解できる形」になっていません。…"}}
```

**`promptSource` の項目が無い。**したがって
[internal/orchestrator/transcript.go:665-667](../../../internal/orchestrator/transcript.go#L665-L667) の `isTypedUser` は偽になり、
**書き直した応答は、差し戻される前の応答と同じ読み取り範囲に入る**（5 節の2つ目に効く）。

---

## 3. 誰が踏むか

**言いたいこと。**`decision:block` を返す `Stop` hook を1本でも入れている利用者は、全員踏みうる。

**continuo が渡す `--settings` は追加であって置き換えではない。**
利用者の `~/.claude/settings.json` と worktree の `.claude/settings.json` に書かれた `Stop` hook は、
**continuo が張った `Stop` hook と並行して走る。**

**このリポジトリには差し戻す `Stop` hook が2本ある。**

| hook | いつ差し戻すか |
| --- | --- |
| [.claude/hooks/check-reply-clarity.py:656](../../../.claude/hooks/check-reply-clarity.py#L656) | 200文字以上の応答で、引用が80文字未満のときなど |
| [.claude/hooks/check-verified-commands.py:339](../../../.claude/hooks/check-verified-commands.py#L339) | 確認していないコマンドを実行したと書いたとき |

**つまり continuo で continuo 自身を開発すると、ほぼ毎 turn 差し戻しが起きる。**
**ただし「差し戻しが起きる」と「continuo が誤判定する」は同じではない。**誤判定するのは 4 節の3つの経路だけである。

---

## 4. どの経路で誤判定するか（測って絞り込んだ）

**言いたいこと。****`agent.prompt` の待ちは、差し戻しでは返らない。**
だから普通の turn では窓が開かない。**窓が開くのは、待ちを介さずに `confirmTurnEnd` へ入る3つの経路だけである。**

**測った事実。**herdr は**差し戻しの最中ずっと `working` を返す**
（[docs/evidence/stop_hook_block_20260902.md](../../evidence/stop_hook_block_20260902.md)）。
`agent.prompt` は `Wait{Until: idle / done / blocked}` を付けて送る（`claude.wait_until` の既定）ので、
**差し戻しの最中には返らない。**返るのは書き直しが終わったあとであり、そのときには
2本目の空の `Stop` が届いていて `stopSeenAt` はそちらに載っている。**この経路は既に正しい。**

**窓が開く3つの経路。**

| 経路 | どこから `confirmTurnEnd` へ入るか |
| --- | --- |
| **`<task-notification>` のあと** | 窓の中で `<task-notification>` を受けて待ち直したあと、その処理の応答が差し戻される。`awaitStop` が空の `Stop` を掴み、`settle_ms` の窓が開く |
| **走行中の `Stop` のあと** | 同上。`background_tasks` が空でない `Stop` で待ち直したあと |
| **引き継いだ run** | [internal/orchestrator/turn.go:128](../../../internal/orchestrator/turn.go#L128)。`agent.prompt` を送らずに `confirmTurnEnd` から入る（3-4 の段5a2） |

**そのときの時刻。**continuo は **2.0 秒**で窓を閉じ、**2.5 秒**で transcript を読む
（`settle_ms` の既定 2000ms は [internal/config/default.go:134](../../../internal/config/default.go#L134)、
`transcriptFirstWait` の 500ms は [internal/orchestrator/orchestrator.go:65](../../../internal/orchestrator/orchestrator.go#L65)）。
**書き直しの中央値は 21.1 秒である**（8 節）。**間に合っていない。**

---

## 5. 壊れるもの3つ

**言いたいこと。**3つとも「早すぎる時点で turn の終わりを確定させた」ことから出ている。
**原因は1つである。**

| 何が壊れるか | どこで |
| --- | --- |
| **差し戻された側の応答Aで Status が動き、書き直し中の pane を閉じる** | [internal/orchestrator/lifecycle.go:37-39](../../../internal/orchestrator/lifecycle.go#L37-L39) が応答Aから表明を読み、[internal/orchestrator/lifecycle.go:91-93](../../../internal/orchestrator/lifecycle.go#L91-L93) の default の枝が `finishRun` へ進む |
| **書き直した応答Bが、どこからも読まれない** | 読み取り範囲は「`typed` の user 行から次の `typed` の user 行まで」（[internal/orchestrator/transcript.go:521-546](../../../internal/orchestrator/transcript.go#L521-L546)）。差し戻しの行は `typed` ではないので応答Bは応答Aと同じ範囲に入り、**その範囲は読み終わっている** |
| **遅れて届く2本目の空の `Stop` が、次の turn の終わりとして数えられる** | [internal/orchestrator/runstate.go:542](../../../internal/orchestrator/runstate.go#L542) が `stopSeenAt` を立て、次の `confirmTurnEnd` が即座に `turnEnded` を返す。連鎖して `max_dispatch_turns`（既定20）を空回りで食い潰し、[internal/orchestrator/turn.go:130-140](../../../internal/orchestrator/turn.go#L130-L140) が `failure_state` へ落とす |

**3つ目がいちばん見えにくい。**issue に残る理由は
**「作業が終わったという表明を出しませんでした」**になり、実際に起きたこととは別の話になる。

**2つ目は、早く読まなければ自然に直る。**応答Aと応答Bは同じ範囲に在り、
表明は**最後に現れたものが勝つ**（[internal/orchestrator/signal.go:114-115](../../../internal/orchestrator/signal.go#L114-L115)）。
**書き直しが終わってから読めば、応答Bの表明が採られる。**

---

## 6. 採る直し方：`agent.get` の裏取り

**言いたいこと。**`turnEnded` を返す唯一の場所の直前で、`agent.get` を1回だけ読む。
**`working` なら turn の終わりとせず、既にある「まだ動いている」の待ち直しへ合流させる。**

**`turnEnded` を返す場所は1箇所しかない**（`grep -n "turnEnded" internal/orchestrator/*.go` の結果は
定義2件・`turnLoop` の分岐1件・return 1件）。だから足すのも1箇所である。

**`confirmTurnEnd` の `awaitHook` が空振りした枝をこう変える。**

```go
		ev, got := o.awaitHook(ctx, rs, remaining, turnContinues)
		if !got {
			if !o.stillWorkingAfterStop(ctx, rs) {
				return turnEnded, nil
			}
			// **差し戻されて応答を書き直している最中である**（3-79）。
			o.logger.Info("空の Stop のあともエージェントが動いているので、turn の終わりとせずに待ち直します",
				"identifier", rs.issue().Identifier)
			rs.clearStopSeen()
			firstWait = false
			rewriteWait = true
			continue
		}
```

**足す関数**（同じファイルの `agentStatus` の隣）。

```go
func (o *Orchestrator) stillWorkingAfterStop(ctx context.Context, rs *runState) bool {
	st, err := o.agentStatus(ctx, rs)
	if err != nil {
		o.logger.Warn("turn の終わりの裏取りができませんでした（turn の終わりとして進みます）",
			"identifier", rs.issue().Identifier, "error", err)
		return false
	}
	return st == herdr.AgentStatusWorking
}
```

**`working` 以外は全部これまでどおりにする。**`blocked` も `unknown` も `turnEnded` である
（`blocked` の扱いを変えるのはこの issue の範囲ではない。14 節）。
**読めなかったときも `turnEnded` である。**ここで待ちに倒すと、herdr が答えない間ずっと turn が終わらない。

**agent の名前をまだ持っていない run では、聞かずに `turnEnded` へ進む。**
引き継いだ run で復元が名前を埋める前に `Stop` が届くと、`agent.get` は必ず失敗する。
**答えられないと分かっているものを毎回 WARN に出すと、本当に herdr が答えなくなったときの1行が埋もれる。**
この場合だけ debug で1行出す。

---

## 7. 待ち直したあとに何が起きるか — 外れたときの代償まで含めて

**言いたいこと。**`clearStopSeen` と `firstWait = false` で、
**既にある「`background_tasks` が空でない `Stop` を受けたとき」の待ち直しと同じ道に入る。**新しい待ちを作らない。
**ただし推測で入る待ちなので、出口を1つ置く。**

| 次に来るもの | どうなるか |
| --- | --- |
| 書き直しが終わって空の `Stop` が来る | `awaitStop` が `stopWaitEmpty` を返し、もう一度 `settle_ms` の窓を開いて裏取りする |
| 何も来ないまま `settle_ms`（既定2秒）が過ぎ、**エージェントがまだ `working`** | 待ち直す。**総時間では打ち切らない**（打ち切りは巡回の stall 検知だけが決める） |
| 何も来ないまま `settle_ms` が過ぎ、**エージェントが動いていない** | **`turnEnded` を返す**（下の「外れたとき」） |

**`working` は推測である。****`background_tasks` が空でない `Stop` は Claude Code 自身の申告だが、
こちらは herdr の見え方から当てているだけ**で、重みが違う。**書き直し以外に `working` に見える理由がある。**

| 書き直し以外の理由 | 何が起きるか |
| --- | --- |
| **遅い `Stop` hook がまだ走っている** | continuo の hook は真っ先に届くが、他の hook はまだ動いている。**herdr は `working` に見える**（8秒の hook で実測。[docs/evidence/stop_hook_block_20260902.md](../../evidence/stop_hook_block_20260902.md)） |
| **herdr の画面の見え方が一瞬ずれた** | 版が変わった直後など |

**どちらも「新しい `Stop` は二度と来ない」。**出口を置かないと、ループの先頭 → `awaitStop` が
空振り → 枠待ちでない → `blocked` でもない → `continue` を延々と繰り返し、
**巡回の stall 検知が拾うまで run が空転する。**そのときの上限は
`turn_timeout_ms`（既定 3600000ms。[internal/config/default.go:137](../../../internal/config/default.go#L137)）で、
**最大1時間である。**打ち切られると `RetryCount` を1消費し、issue に打ち切りのコメントが残る。

**だから出口を置く。**待ち直している間は `settle_ms` ごとに `agent.get` を1回読み、
**`working` でなければ `turnEnded` を返す。**代償は**最大1時間から `settle_ms`（既定2秒）へ縮む。**
**この枝は `agent.get` を増やさない。**同じ枝で `blocked` の判定のために既に1回読んでおり、その結果を使い回す。

**刻む長さを `poll_wait_ms` にしてはならない。**上の表の「遅い `Stop` hook がまだ走っている」は、
**`settle_ms`（既定2秒）を超える `Stop` hook を1本でも持つ利用者なら毎 turn 必ず通る道である。**
`poll_wait_ms`（既定30秒）で刻むと、**その利用者は毎 turn ちょうど30秒を捨てる。**
`max_dispatch_turns`（既定20）を掛けると1 run あたり10分である。
**刻んでも本物の書き直しは取り逃がさない。**
[docs/evidence/stop_hook_block_20260902.md](../../evidence/stop_hook_block_20260902.md) は
0.1 秒ごとに `agent.get` を読み、**書き直しの最中に `idle` が返った瞬間は1度も無かった**と記録している。

---

## 8. 測ったもの1：差し戻しの書き直しにかかる時間

**言いたいこと。****290件測って、`settle_ms` の既定 2.0 秒以内に終わったのは 19 件（6.6%）だった。**
`settle_ms` を伸ばす案は、これで否定される。

**測り方。**手元の Claude Code の記録（`~/.claude/projects` 配下の JSONL、4096 ファイル）を読み、
`type == "user"` かつ本文が `Stop hook feedback:` で始まる行を差し戻しの起点、
**その次の user 行の手前にある最後の `assistant` 行**を書き直しの終わりとして、時刻の差を取った。
**読むだけで、1バイトも書いていない。**2026-09-02 に実行。

**結果**（差し戻しは 311 件、時刻を取れたのは 290 件）。

| 何 | 秒 |
| --- | --- |
| 最小 | **1.71** |
| 中央値 | 21.12 |
| 90% 点 | 38.27 |
| 95% 点 | 48.09 |
| 最大 | **83.28** |

| 区切り | 収まった件数 |
| --- | --- |
| **2.0 秒以内**（`settle_ms` の既定） | **19 / 290（6.6%）** |
| 30.0 秒以内（`poll_wait_ms` の既定） | 232 / 290（80.0%） |

**この数字は短く出る側に偏っている。**終点の判定が次の user 行で止まるので、
**道具を使った書き直しは `tool_result` の user 行で切られる**（290 件のうち 83 件）。
**実際の書き直しは、その分だけ長い。**

---

## 9. 測ったもの2：`agent.get` の往復時間と回数

**言いたいこと。****1回 1.1 ミリ秒である。**turn の終わり1回につき1回しか呼ばない。**費用は無視できる。**

**測り方。**herdr 0.8.2 が listen している Unix domain socket（`~/.config/herdr/herdr.sock`）へ
`{"id":"…","method":"agent.get","params":{"target":…}}` を1行送り、応答の1行を読み終えるまでを測った。
**接続から切断までを含む**（herdr は1接続1要求で閉じる）。**`id` は文字列である**（数値だと `invalid_request`）。

| 何 | ミリ秒 |
| --- | --- |
| 最小 | 0.31 |
| 中央値 | **1.13** |
| 最大 | 1.77 |

**回数。**turn の終わりを確定させるときに1回。1 run 全体（`max_dispatch_turns` の既定20で、
毎回1度も待ち直さない場合）で 20 回 = **合計 23 ミリ秒**である。
**`settle_ms` の 2000 ミリ秒に対して 0.1% 未満である。**

**待ち直している間は `settle_ms` ごとに1回ずつ足される**（7 節）。
**既定の2秒で刻むと、書き直しの中央値 21.1 秒に対して 11 回 = 12 ミリ秒である。**
**費用はここでも無視できる。**

---

## 10. 採らなかった案と、その否定の根拠

**言いたいこと。**4つ検討して、全部落とした。**落とした理由はどれも「測った」か「観測できていない」である。**

**案：`settle_ms` を伸ばすだけ。**
**否定する。**8 節のとおり **290 件中 232 件（80.0%）しか 30 秒に収まらない。**
`settle_ms` は `poll_wait_ms` 以下でなければならず（[internal/config/validate.go:270-274](../../../internal/config/validate.go#L270-L274)）、
その既定の 30 秒まで伸ばしてもそこまでである。
**95% を拾うには 48 秒、最大を拾うには 84 秒が要る。**
**その待ちは、差し戻す hook を1本も入れていない利用者の全 turn にも掛かる。**
20 turn の run なら**まるまる 16 分から 28 分を待つだけに使う。**
**そして2回差し戻されれば、どんな固定値でも足りない。**

**案：`stop_hook_active` を判定に使う／記録だけ残す。**
**どちらも採らない。****1本目の `Stop`（差し戻される側）では偽である。**
真になるのは書き直しが終わったあとの2本目だけで（[docs/evidence/stop_hook_block_20260902.md](../../evidence/stop_hook_block_20260902.md) で実測）、
**防ぎたい瞬間には必ず偽である。**記録として残しても、狙った場面では偽しか採れない。
**裏取りが同じ穴を塞ぐ。**遅れて届いた `Stop` を受けた時点でも `agent.get` は読まれ、
**待ち行列に積まれた次の指示を処理しているエージェントは `working` を返す。**
**同じ問いに答える仕組みを2つ置かない。**

**案：transcript の末尾に `Stop hook feedback:` の行があるかを見る。**
**採らない。****この文字列は Claude Code が付ける表示用の文言であり、版が変われば変わる**
（観測できたのは 2.1.252）。判定を表示用の文字列に載せると、Claude Code の更新で静かに効かなくなる。
**ただし観測はできている**（2 節に実物）。**裏取りが効かないと分かったときの次の手として、ここに残す。**

**案：`UserPromptSubmit` を `<task-notification>` 以外も「turn が続いている」と見る。**
**採らない。****差し戻しが `UserPromptSubmit` を出すかどうかを確かめられていない。**
出さないなら1件も拾えず、出すなら
[internal/orchestrator/orchestrator.go:1092-1100](../../../internal/orchestrator/orchestrator.go#L1092-L1100) の
`isTurnBoundaryHook` が広がって、**人間が pane へ直接打った入力まで turn の判定に混ざる。**

---

## 11. テストで押さえること

**言いたいこと。**この欠陥そのものを再現する1本と、周りを固める3本を置く。
**置き場所は [test/internal/orchestrator/turn_test.go](../../../test/internal/orchestrator/turn_test.go) である。新しいファイルは作らない。**

**同じ形の欠陥を押さえる兄弟が、同じファイルの
`TestTurn_空のStopのあとに来た走行中のStopも捨てない` として既に在る。**
あちらは Claude Code 自身が「まだ動いています」と申告してくる場合、こちらは**誰も申告してこない**場合である。
**並べて置くと、次に読む人が2つを見比べられる。**

| テスト | 仕込み | 確かめること |
| --- | --- | --- |
| **差し戻して書き直している間はturnの終わりとしない** | 空の `Stop` が1本。`agent.get` は `working`。transcript は応答A（`CONTINUO-STATUS: review`）だけ | **pane が閉じられない。Status が `In Review` へ動かない。次の指示が送られない。**そのあと応答B（`CONTINUO-STATUS: working`）を足して `idle` に戻すと、応答Bの表明が採られる |
| **裏取りが読めなければ従来どおりturnを終わらせる** | `agent.get` がエラー | **待ち続けない。**turn の終わりとして進み、**待ち直しのログが1行も出ない** |
| **裏取りがidleなら空のStopでそのままturnが終わる** | `agent.get` は `idle` | `settle_ms` の経過で turn が終わり、**待ち直しのログが1行も出ない** |
| **書き直しが来ないまま止まっていれば待ち続けない** | `agent.get` が `working` → そのあと `Stop` は来ず `idle` に変わる | `settle_ms` の経過で turn の終わりとして進む（7 節の出口） |
| **遅いStophookでもpoll_wait_msを待たない** | 同上。ただし `poll_wait_ms` を `settle_ms` の何十倍にも広げる | **`settle_ms` 数回ぶんで turn が終わる。**`poll_wait_ms` で刻んでいれば必ず間に合わない |

**直す前は、1本目と4本目と5本目が落ちる。**裏取りが無いので待ち直しのログが出ず、run が畳まれる。

**`agent.get` の台本は `agentStatusScript` として切り出す。****着手の段では `idle` を返させること。**
起動の落ち着きを待つところ（`herdr.startup_timeout_ms`）も同じ `agent.get` を読むので、
最初から `working` にすると着手そのものが失敗し、狙った経路に入らない。

---

## 12. [docs/plans/continuo_design.md](../continuo_design.md) に入れる変更

**言いたいこと。**1行を直し、1節を足す。**3-2 と 3-26 は触らない。**

**直す1行。**[docs/plans/continuo_design.md:745](../continuo_design.md#L745) を、いまの実装と合わせる。

```markdown
| **`Stop`** | **turn の終わりの判定の起点。**`background_tasks` を見る。**`stop_hook_active` は使わない**（3-79） |
```

**足す1節。**`### 3-79. 空の Stop は「止まってよいか尋ねた」であって「終わった」ではない`。
中身はこの文書の 1 節・6 節・7 節を縮めたもので、**測定値の細かい内訳は入れずにこの文書を参照させる。**

**[docs/plans/continuo_design.md:3367](../continuo_design.md#L3367) の周辺は残す。**
あちらは「**continuo 自身は差し戻しを使わない**」を決めているだけで、
**「他人の hook が差し戻してきたときにどうするか」は決めていない。**3-79 がそこを埋める。

---

## 13. 触るファイル

| ファイル | 何をするか |
| --- | --- |
| [internal/orchestrator/turn.go](../../../internal/orchestrator/turn.go) | `stillWorkingAfterStop` を足し、`confirmTurnEnd` で呼ぶ。`rewriteWait` の出口を足す |
| [test/internal/orchestrator/turn_test.go](../../../test/internal/orchestrator/turn_test.go) | 11 節の4本と `agentStatusScript` |
| [test/internal/orchestrator/quota_test.go](../../../test/internal/orchestrator/quota_test.go) | 偽の `agent.get` が空の `Stop` のあとも `working` を返し続けていたのを、`idle` へ戻すようにする（下） |
| [docs/evidence/stop_hook_block_20260902.md](../../evidence/stop_hook_block_20260902.md) | 4 節・7 節・10 節が引く実測の記録 |
| [docs/plans/continuo_design.md](../continuo_design.md) | 12 節 |

**`TestQuota_枠明けにClaudeCodeが自分で継続していたら継続の指示を送らない` の偽の herdr は、
1回目だけ `idle` を返してあとは永久に `working` を返していた。****本物はそうならない。**
書き終えて `Stop` hook が通ったエージェントは、その 0.09 秒後に `idle` へ落ちる
（[docs/evidence/stop_hook_block_20260902.md](../../evidence/stop_hook_block_20260902.md)）。
**偽物だけで起きる状態を残すと、裏取りが「まだ書き直している」と読んで待ち続ける。**
だから、空の `Stop` を流したあとは `idle` を返すようにする。

**[internal/orchestrator/runstate.go](../../../internal/orchestrator/runstate.go) は触らない。**
`stop_hook_active` を控える項目は置かない（10 節）。
**[internal/hookserver/event.go](../../../internal/hookserver/event.go) も触らない。**
`StopHookActive` は既に受け取れている（[:39](../../../internal/hookserver/event.go#L39)）。

---

## 14. この設計が直さないもの

**言いたいこと。**2つ残す。**どちらも #166（応答が hook に差し戻されたのに、continuo は turn が終わったと判断して次の指示を送る）の外側である。**

| 残すもの | なぜ今回入れないか |
| --- | --- |
| **裏取りで `blocked` が返ったときに `turnBlocked` へ倒すこと** | いまは `turnEnded` になり、権限の確認の画面へ次の指示を打ち込む。**これは差し戻しとは別の欠陥である。**倒すと pane を閉じて人間へ渡す経路に入るので、**挙動の変化が大きい。**別の issue に切る |
| **2.0 秒以内に終わる書き直し（6.6%）を拾うこと** | **拾えないが、害が無い。**窓が閉じる時点で書き直しが終わっていれば herdr は `idle` を返し、裏取りは空振りする。**そのとき transcript には応答Bが既に在り、表明は最後に現れたものが勝つ**（[internal/orchestrator/signal.go:114](../../../internal/orchestrator/signal.go#L114)）。**拾いにいく仕組みを足すと、得るものが無いまま層が1つ増える** |

**herdr が `working` を返さない瞬間に窓が閉じる危険は、残っていない。**
差し戻しの最中は一貫して `working` であることを実測してある
（[docs/evidence/stop_hook_block_20260902.md](../../evidence/stop_hook_block_20260902.md)）。
**見え方が変わったときは 10 節に残した transcript の案へ移る。**
