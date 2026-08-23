<!-- 目的: 2026-08-19 のコードレビューの結果と、どれを直したかの記録 -->

# コードレビュー 2026-08-19

**言いたいこと。**第1〜9段階の実装全部を `code-reviewer` と `security-reviewer` にかけ、**96件の指摘**を受けた。
**うち critical 2件は、どちらもエージェントが書き換えられる値を検証せずに使う経路である。**
生の指摘は [2026-08-19_code_review.json](2026-08-19_code_review.json) にそのまま置いた。

## 対象と分担

対象は `origin/main..HEAD` の11コミット（実装67ファイル・テスト74ファイル）。

| 担当 | 種類 | 件数 |
| --- | --- | --- |
| 基盤 | 品質 | 13 |
| 外部通信 | 品質 | 12 |
| hook | 品質 | 8 |
| workspace | 品質 | 13 |
| orchestrator | 品質 | 11 |
| 結線と窓 | 品質 | 11 |
| 外部入力 | セキュリティ | 10 |
| 認証と権限 | セキュリティ | 7 |
| コマンド実行とネットワーク | セキュリティ | 11 |

| 深刻さ | 件数 |
| --- | --- |
| critical | 2 |
| high | 20 |
| medium | 40 |
| low | 34 |

## 繰り返し出た6つのテーマ

**個別の指摘より、この分類のほうが直す指針になる。**

| テーマ | 何が問題か |
| --- | --- |
| **エージェントが書き換えられる値を検証せずに使う** | 身元ファイルの `herdr_workspace_id` / `session_uuid` / `settings_path`、hook の `transcript_path` / `session_id`。**worktree の中でエージェントは `dontAsk` で動くので、身元ファイルを書き換えられる** |
| **期限（context）が付いていない** | herdr の `call`、GraphQL、`Orchestrator.Close()`、起動時検査、信頼の検査。**無人運用では、返らない呼び出しがそのまま停止になる** |
| **上限が無い読み取り** | transcript の全行、hook の行数、逃がし先のファイル、`git status` の出力。**エージェントが大量に書けば落とせる** |
| **排他が無い共有状態** | `tracker.Adapter` のフィールド、`workspace.Manager` の身元ファイル |
| **設定の検証漏れ** | 時間を表す値が1つも検査されていない。`poll_wait_ms: 0` でビジーループになる |
| **仕様の未実装** | 設定の読み直し（SPEC.md 6.2 / 設計 3-24）が1行も無いのに、コメントは実装済みの前提で書かれている |

## critical 2件

### internal/orchestrator/runstate.go:316-317, internal/orchestrator/lifecycle.go:70, internal/orchestrator/transcript.go:138

**問題。**hook の JSON に入っている transcript_path が、1回も検査されずに os.Open へ渡る。エージェントが名前付きパイプ（FIFO）のパスを送ると os.Open が永久に返らず、turn ループの goroutine がそこで固まる。その goroutine は o.wg に登録されているので、Orchestrator.Close()（o.wg.Wait()）も二度と返らない。無人の常駐プロセスが、SIGTERM でも終われない状態になる。

**直し方。**transcript_path を使う前に、(1) filepath.IsAbs、(2) 既に用意されている resolvePath + isUnder（internal/orchestrator/restore.go:768-793）で「~/.claude/projects 配下（または設定で決めた許可の根）に入っているか」、(3) os.Lstat で Mode().IsRegular() が真か、の3点を確かめ、外れたら読まずに警告して抜けること。とくに (3) が無いと FIFO・デバイスファイル・ディレクトリで固まる／壊れる。あわせて os.OpenFile に O_NONBLOCK を付けるか、読み取り自体を別 goroutine に出して ctx で打ち切れるようにする。

### internal/workspace/cleanup.go:389-392（判定は cleanup.go:109）

**問題。**worktree を消す直前の封じ込め検査は req.WorktreePath に対して行うのに、実際の削除は身元ファイルに書かれた herdr_workspace_id を宛先にしている。身元ファイルは worktree の直下にあり、その worktree ではエージェントが --permission-mode dontAsk で動く。つまり削除の宛先はエージェントが書き換えられる。同じコードが Branch と SettingsPath については「エージェントが書き換えられるので検算する」と明記しているのに、この値だけ素通りしている。エージェントは同じマシンの herdr に届くので workspace の ID を列挙でき、他の run の worktree を Force: true で消させられる。3-20 の封じ込め検査も 3-9 の未コミット検査も、消される側の worktree に対しては1つも走らない。

**直し方。**deletableBranch と同じく「herdr に現物を答えさせて突き合わせる」検算を入れる。worktree.remove の直前に workspace.list ないし worktree.open の応答から、その workspace_id が指す path を引き、CheckContainmentResolved が返した resolvedPath と一致することを確かめる。一致しなければ消さず、警告を出して見送る。あるいは、身元ファイルの値ではなく runState が in-memory に持つ HerdrWorkspaceID（dispatch.go:339 で入れた値）を優先し、身元ファイルは再起動後の復元でのみ使い、そのときも path 一致を確かめる。

