# 一時的な失敗の判定が使われていない — 直した内容

**言いたいこと。**`herdr.IsTransient` はテスト以外どこからも呼ばれておらず、
**herdr を再起動しただけで走行中の run が捨てられていた。**turn の失敗の2経路に判定を入れ、
捨てずに次の巡回へ持ち越すようにした。**足したテスト2本は、守りを1つ潰すと必ず落ちる。**

## 何が壊れていたか

`sendTurn` は `ErrCodeTimeout` 以外のエラーをすべて `turnSendFailed` にしていた。
`ErrCodeTransport`（socket へ届かない）と `ErrCodeReadTimeout`（continuo 側の読み取り期限）は
どちらも `Retryable` が真なのに、そこで run を諦めてリトライを1回消費する。使い切ると
issue が `failure_state` へ落ち、**herdr が何も答えていないのに引き渡しのコメントが投稿される。**

`internal/herdr/errors.go` は「**呼び出し側はこれが真のとき run を捨ててはならない**」と
約束していたが、その約束を守る分岐が実装に1つも無かった。

## 直した内容

| 何を | どこ |
| --- | --- |
| 新しい結果 `turnTransient` | internal/orchestrator/turn.go |
| `sendTurn` の失敗経路の切り分け | internal/orchestrator/turn.go（`herdr.IsTransient(err)`） |
| 枠待ちの待ち直しの切り分け | internal/orchestrator/turn.go（`afterWaitTimeout`） |
| turn ループの持ち越し | internal/orchestrator/turn.go（`turnLoop` が `awaitTurnEnd` を立てて抜ける） |
| 設計の記録 | docs/plans/continuo_design.md の 3-48 |
| テスト用herdr mock が接続を切れるようにした | test/internal/orchestrator/helpers_test.go（`DropConnection`） |

**持ち越し方は「送り直す」ではなく「待ち直す」である。**`agent.prompt` が herdr へ届いていたか
どうかは分からず、届いていた場合に送り直すと turn が二重に投入される。だから `NeedsPrompt`
ではなく `awaitTurnEnd` を立てる。届いていなかった場合は巡回の stall 検知が拾う。

**枠待ちの待ち直しでは、枠待ちの印を外さない。**外すと stall の時計が動き出し、
枠が明けるより先に stall として諦めることになる。

## 変異で確かめた記録

### turn の送信（`sendTurn`）

変異: `internal/orchestrator/turn.go` の `sendTurn` から `herdr.IsTransient(err)` の分岐を消した。

```
--- FAIL: TestTurn_herdrが一瞬落ちただけでrunを捨てない (4.23s)
    audit_fixes_test.go:450: herdr が一瞬落ちただけで Status を落とした: got "Blocked", want In Progress
    audit_fixes_test.go:453: run を捨てて issue へ引き渡しを書いた: 1 件
        理由: continuo が herdr へ指示を送れませんでした。…
        元のエラー: herdr エラー [transport]: herdr からの応答読み取りに失敗しました（method=agent.prompt）: EOF
```

### 枠待ちの待ち直し（`afterWaitTimeout`）

変異: `internal/orchestrator/turn.go` の `afterWaitTimeout` から `herdr.IsTransient(err)` の分岐を消した。

```
--- FAIL: TestTurn_枠待ちの待ち直しがherdrへ届かなくてもrunを捨てない (2.44s)
    audit_fixes_test.go:543: 待ち直しが届かなかっただけで Status を落とした: got "Blocked", want In Progress
    audit_fixes_test.go:546: run を捨てて issue へ引き渡しを書いた: 1 件
        元のエラー: herdr エラー [transport]: herdr からの応答読み取りに失敗しました（method=agent.wait）: EOF
    audit_fixes_test.go:553: 枠待ちの印を外した（stall の時計が動き出し、枠が明ける前に諦めることになる）: WaitingQuota:false
```

## 合流で守りが壊れていないことの確認

**4つの branch を合流させたあと、次の4つの守りが「外したら落ちる」ことを実測した。**

| 守り | 潰した場所 | 落ちたテスト |
| --- | --- | --- |
| 着手が `active_states` に無い Status を上書きしない | dispatch.go の `dispatchStatusAllowed` の関門 | TestDispatch_active_statesに無いStatusのissueを着手が上書きしない |
| 片付けの `delete_branch` | cleanup.go の `!DeleteBranch` の枝／sweep.go の先頭の関門 | TestCleanup_deleteBranchが偽ならbranchを残して残ったものに積む／TestSweepOrphanBranches_deleteBranchが偽なら1本も消さない |
| setup が block 形式のリストを壊さない | fill.go の `hasNestedValue` の枝 | TestUpdateStatuses_値が下の行にぶら下がっていたら書かずに止める |
| 枠の判定が一度の失敗で永久に切れない | ratelimit.go の `isTemporaryCredentialFailure` の枝 | TestFetch_keychainが1回返ってこなくても諦めない |
