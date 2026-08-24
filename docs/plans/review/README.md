<!-- 目的: 2026-08-19 のコードレビューの結果と、どれを直したかの記録 -->

# コードレビュー 2026-08-19

**言いたいこと。**第1〜9段階の実装全部を `code-reviewer` と `security-reviewer` にかけ、**96件の指摘**を受けた。
**うち critical 2件は、どちらもエージェントが書き換えられる値を検証せずに使う経路である。**
**2件とも修正済みである**（各項の末尾に、直した場所を書いた）。
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

**直した。**`internal/orchestrator/hookinput.go` が `resolvePath` と `isUnder` で置き場所を確かめ、
`os.Lstat` の `Mode().IsRegular()` で種別を確かめる。`internal/orchestrator/transcript.go` は
`O_NONBLOCK` を付けて開き、**開いたあとにもう一度種別を確かめる**（先に `os.Lstat` で見るだけでは、
見てから開くまでの間に差し替えられる）。

### internal/workspace/cleanup.go:389-392（判定は cleanup.go:109）

**問題。**worktree を消す直前の封じ込め検査は req.WorktreePath に対して行うのに、実際の削除は身元ファイルに書かれた herdr_workspace_id を宛先にしている。身元ファイルは worktree の直下にあり、その worktree ではエージェントが --permission-mode dontAsk で動く。つまり削除の宛先はエージェントが書き換えられる。同じコードが Branch と SettingsPath については「エージェントが書き換えられるので検算する」と明記しているのに、この値だけ素通りしている。エージェントは同じマシンの herdr に届くので workspace の ID を列挙でき、他の run の worktree を Force: true で消させられる。3-20 の封じ込め検査も 3-9 の未コミット検査も、消される側の worktree に対しては1つも走らない。

**直し方。**deletableBranch と同じく「herdr に現物を答えさせて突き合わせる」検算を入れる。worktree.remove の直前に workspace.list ないし worktree.open の応答から、その workspace_id が指す path を引き、CheckContainmentResolved が返した resolvedPath と一致することを確かめる。一致しなければ消さず、警告を出して見送る。あるいは、身元ファイルの値ではなく runState が in-memory に持つ HerdrWorkspaceID（dispatch.go:339 で入れた値）を優先し、身元ファイルは再起動後の復元でのみ使い、そのときも path 一致を確かめる。

**直した。**`internal/workspace/cleanup.go` の `resolveWorkspaceID` が herdr に答えさせ、
`CheckContainmentResolved` が返した `resolvedPath` と突き合わせる。一致しなければ消さない。


---

# コードレビュー 2026-08-24（`continuo abandon`）

**言いたいこと。**`continuo abandon` の実装を `code-reviewer` にかけ、**14件の指摘**を受けた。
**critical 1件と high 4件は前の回で直した。**この節は残りの medium 6件と low 3件の対応である。
**9件のうち8件を直し、1件（worktree が無いときに `--to` を通す案）は形を変えて直した。**
生の指摘は [2026-08-24_abandon_code_review.json](2026-08-24_abandon_code_review.json) にそのまま置いた。

## medium の指摘と対応

| 短縮名 | 何が問題か | どうしたか |
| --- | --- | --- |
| **--to の後出し検査** | `--to` がボードの選択肢にあるかを確かめるのが `UpdateStatus` の中で、呼ぶのは worktree を消したあと | **直した。**段2 の直後に `verifyTargets` を置いた（設計 3-37-5） |
| **park が作業中の値** | `--park` に `tracker.active_states` の値を渡せる。動かしても手を離さず pane も閉じない | **直した。**同じ `verifyTargets` で、書く前に止める |
| **--to の握り潰し** | worktree が0件だと `--to` を使わずに終了コード 0 で返る | **直した。**「指定した値へは動かしていません」と1行出す。**Status だけを動かすことはしない**（URL の打ち間違いだと別の issue を動かす） |
| **失う数の頭打ち** | `git status --porcelain` の8KBの打ち切りを捨てていて、失う量を実際より少なく見せる | **直した。**`Leftover.DirtyFilesTruncated` で運び、「%d ファイル以上」と出す |
| **branch の消し忘れの記録** | 片付けのログが `BranchDeleted` を見ずに「worktree と branch を片付けました」と書く | **直した。**`internal/workspace/cleanup.go` と `internal/orchestrator/lifecycle.go` の2箇所を書き分けた |
| **CLI の結線の無検査** | `runAbandon` はフラグを `Options` へ結線する唯一の場所なのに、通すテストが1本も無い | **直した。**`test/internal/cli` に4本足した（結線・既定値・引数の誤り・`--help`） |

## low の指摘と対応

| 短縮名 | 何が問題か | どうしたか |
| --- | --- | --- |
| **中断と時間切れの同文** | `SIGINT` / `SIGTERM` で止めたときに「%v 以内に閉じませんでした」と出る | **直した。**中断専用の文言を足した（上限が短すぎたのかと読み違える） |
| **届かない照合** | `SameIssue` の「解釈できない URL は文字列で照合する」経路は、真を返しようがない | **直した。**経路を消し、GoDoc を実態（どれにも一致しない）に直した |
| **綴りで崩れる保証** | 設定の検証は完全一致、実行時は大文字小文字を無視。`failure_state` が綴り違いで `active_states` に入る | **直した。**設定の検証も大文字小文字を無視する（`containsStateFold`） |
