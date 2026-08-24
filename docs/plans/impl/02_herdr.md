<!-- 目的: herdr の socket クライアントのタスク -->

# 02. herdr クライアント

**言いたいこと。実装済み。**pane を作り、agent を起動し、**turn の終わりまで待てる。**
**完了待ち・状態検知・worktree の作成と削除は herdr が持っているので、自前で実装しない。**

## 読むもの（設計）

| 節 | 何が書いてあるか |
| --- | --- |
| 2-1 | **socket API の実在するメソッドと引数**（protocol 19 で確認） |
| 3-2 | **待ち受けの形**（`--until idle --until done --until blocked`） |
| 3-3 | 復元の識別は pane の cwd とセッション UUID の2本立て（label は表示名） |

## 実装の記録

**実装済み**（2026-08-18）。`internal/herdr` に client / agent / pane / worktree / workspace / protocol / errors / types。

**実装から見つかった設計の誤り**（設計に反映済み）。

| 設計に書いてあったもの | 実際 |
| --- | --- |
| `agent.status` | **存在しない。**`agent.get` |
| `protocol.version` | **存在しない。**`ping` |
| `pane.split` に `label` | **無い。**`pane.rename` を別途呼ぶ |
| `agent.wait` の `until` が文字列 | **配列である** |
| `agent.prompt` の `wait` が真偽値 | **オブジェクトである**（`timeout_ms` と `until`） |
| `agent.read` の `source` が `recent-unwrapped` | **socket API では `recent_unwrapped`**（CLI はハイフン） |

**あとから足したもの。**`AgentSendKeys`（`target` / `keys` の配列）を実装した。
**権限の確認を取り消すのに使う**（設計 3-11）。CLI を exec する必要は無い。

**agent だけを止めるメソッドは herdr に無い**（protocol 19 で確認）。
**`pane.close` が唯一の手段である**（設計 3-5）。`PaneClose` は実装済み。

**`worktree.open`（既にある worktree を開く）と `worktree.create`（作って開く）は別のメソッドである。**
**continuo は git で worktree を作るので、使うのは `open` のほうである**（設計 3-22 の段7）。

**`pane.split` と `tab.create` は使わない**（設計 4-5）。`worktree.open` が作る workspace の pane を
`pane.list` で引いて、そこで `agent.start` する。**`PaneList` は実装済み。**

**`workspace.close` は要らない。**`worktree.remove` の応答に `workspace` が入り、workspace ごと閉じる。

**pane と herdr workspace に貼る label は `herdr.IssueLabel(owner, repo, number)` で組み立てる**
（`internal/herdr/label.go`）。**形は `owner/repo/issues/N`**（設計 3-3）。
2箇所（`pane.rename` と `worktree.open` / `workspace.rename`）が別々に組み立てると形がずれるため、
1本に寄せてある。**owner か repo が空、または番号が0以下なら空文字を返す**（draft issue のため）。

**テストの書き方。**herdr は `pane.report_agent` で実プロセスを起動せずに
「agent が居る」と登録できる。**Claude Code を起動しない統合テストが書ける。**
`test/internal/herdr/fakeserver_test.go` に偽のサーバを持っている。
