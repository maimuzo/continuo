# 29件の修正に RUCM を追いつかせた — 足した分岐とテスト

**言いたいこと。**実装に増えた分岐のうち、RUCM に書かれていなかったものが4つあった。
**RUCM に無い分岐にはテストが書かれない。**4つを書き、CFG を作り直し、テストを3本足した。
**足したテストは、守りを1つ潰すと必ず落ちる。**

## 足した分岐

| ユースケース | 足した分岐 | 実装のどこ |
| --- | --- | --- |
| issue を1件処理する | 一時的な送信の失敗（run を捨てず、pane も閉じない） | internal/orchestrator/turn.go の `turnTransient` |
| worktree と branch を片付ける | `cleanup.delete_branch` が偽なら branch を残す | internal/workspace/cleanup.go の `case !m.cfg.Cleanup.DeleteBranch` |
| 再起動して実行中の issue を引き継ぐ | 起動時の掃除と、その `cleanup.delete_branch` の判定 | internal/orchestrator/sweep.go の `SweepOnStartup`、internal/workspace/sweep.go の `SweepOrphanBranches` |
| 既存のボードの Status を割り当てる | 書く前に組み立てた全文の front matter を読み直す | internal/scaffold/update.go の `ErrWouldBreakConfig` |

## 分岐の置き場所を決めた根拠

**起動時の孤児 branch の掃除は「再起動して実行中の issue を引き継ぐ」に置いた。**
片付けのユースケースの主アクターは巡回タイマーであり、掃除は起動の手順の一部である
（`SweepOnStartup` は `Restore` のあとにしか呼ばれない）。**同じ手順を2箇所に書くとずれる。**

**片付けの branch の始末は、実在の判定を設定の判定より先に置いた。**
`Cleanup` の switch が `branchAbsent` を先に見ているのと同じ順である。
**逆にすると、元から無い branch を「設定で消さないので残しました」と報告する。**

## 足したテスト

| テスト | 何を検査するか |
| --- | --- |
| `TestTurn_一時的な送信の失敗ではpaneを閉じない` | herdr が一瞬落ちたとき `pane.close` を1度も呼ばないこと |
| `TestSweepOnStartup_deleteBranchが偽なら孤児branchを1本も消さない` | 起動時の掃除が設定を見ること |
| `TestCleanup_実在しないbranchはdeleteBranchが偽でも残ったものに数えない` | 実在の判定が設定の判定より先にあること |

**3本とも `go test -overlay` で変異を当てて落ちることを確かめた**（守りを1箇所だけ潰した版を
`/tmp/` に作って走らせる）。とくに3本目は、変異させると
`branch … は残っています（cleanup.delete_branch が false です）` という**存在しない branch
についての案内**がそのまま出る。

## 手を付けなかったもの

| 何 | なぜ |
| --- | --- |
| 設定ファイルを作る（`continuo init`） | 29件の修正で分岐が増えていない。雛形へ `cleanup.on_states` を足したのは値の変更であって分岐ではない |
| 前提が揃っているかを検査する（`continuo doctor`） | socket の残骸の判定は、見出し語1つの中の結果の違いである。基本フローの繰り返しと代替フロー `前提が揃っていない` で既に表せる |
| 着手を取り消す | issue 番号の検算（ステップ10 のスラグ）も `--force` での pane 待ちの追い越し（ステップ23）も、既に書かれている |
| CRLF の扱い | 分岐ではない。CRLF でも LF でも通る経路は1本で、結果も同じである |
