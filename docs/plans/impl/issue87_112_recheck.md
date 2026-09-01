# PR #112（1台で continuo を複数動かす）の再点検 — どこまで縮められるか

**この文書は何か。**PR #112（1台で continuo を複数動かす）に乗っている 55ファイル・+5145行を、
元の指示と突き合わせ、**縮めた3つの案と推奨**を出したものである。
**捨てるかどうかを決めるのは人間である。**この文書は案を並べ、推奨を1つ示すところまでを書く。

**確認した対象。**branch `feat/issue87-multi-instance` の HEAD は commit `7f6e0b9`、
main との分岐点は commit `6a894fb`。**main は既に commit `73fb41a` まで進んでいるので、
どの案を採っても rebase が要る。**以下の行番号はすべて commit `7f6e0b9` のものである。

---

## 1. 元の指示は何だったか

**言いたいこと。**指示は3本あり、**正は2026-08-29T07:36 の確定仕様である。**
そこは「`--id` は名前の重複だけを見る」「worktree と socket はテスト用の設定ファイルで書き換える」と決めた。
**目的は開発時に本番を止めずにテスト版を動かすことだけで、一般利用者向けの機能ではない。**

| いつ | 何を決めたか | 原文（抜粋） |
| --- | --- | --- |
| 2026-08-28 | 検討の出発点。プロセスごとに名前を決め、オプション付きなら複数起動 | 「continuoのプロセスごとにIDを決めておき、オプション無しでcontinuoを起動しようとすると止められるけど、オプション付きで起動すると複数起動できる(ただし挙動は使用者責任)」 |
| **2026-08-29T07:36（確定仕様）** | **`--id` の役目は重複判定だけ。置き場所は設定ファイルで分ける** | 「テスト用WORKFLOW.mdはworktreeの場所やsocketの場所を書き換えて使用する前提。--idをつけないと、多重起動をブロックする。--idをつけるとそのidが使用されていればブロックし、使われてなければブロックしない」 |
| 2026-08-29T22:23 | ロックを `~/.continuo/continuo.lock` に固定し、`runtime.lock_file` はキーだけ残して値を捨てる形を承認 | 「runtime.lock_file のキー **設定に残す。**起動は通る 提案の形でよい。」 |

**AI 自身も issue #87（1台で continuo を複数動かすことを、正式な形にする）のコメントで取り下げを書いている。**

> **「`workspace.root` の末尾に `/<名前>` を足す」は採りません。**テスト用の設定ファイルで書き換える前提になったためです。

**同じコメントの仕様表にも「worktree と socket ｜ テスト用の設定ファイルで書き換える前提でよい（自動で導出しない）」とある。**
**取り下げは issue に載っただけで、設計と実装には反映されなかった。**それが今回いちばん大きい増分の根である。

---

## 2. いま何が乗っているか

**言いたいこと。**55ファイル・+5145行・-344行。**うちコードが27ファイル、テストが17ファイル、文書が11ファイル。**
**確定仕様が求めるのは `--id` フラグ1つとロックの固定だけである。**
残りは、そこから派生させたものと、派生が開けた穴を塞ぐために足したものである。

| 区分 | ファイル | 追加行 | 主な中身 |
| --- | --- | --- | --- |
| **`--id` 本体とロックの固定** | 13 | +560 前後 | [internal/instance/instance.go](internal/instance/instance.go)（+350）/ [internal/cli/cli.go](internal/cli/cli.go)（+101）/ [internal/config](internal/config) 5本（+27）/ [internal/i18n](internal/i18n) 3本（+182）/ [internal/socketpath/socketpath.go](internal/socketpath/socketpath.go) |
| **置き場所と branch 名の派生** | 7 | +626 | [internal/workspace/instancemarker.go](internal/workspace/instancemarker.go)（新規126）/ [internal/workspace/workspace.go](internal/workspace/workspace.go) / [internal/workspace/layout.go](internal/workspace/layout.go) / [internal/workspace/sweep.go](internal/workspace/sweep.go) / [test/internal/workspace/instancemarker_test.go](test/internal/workspace/instancemarker_test.go)（新規335） |
| **ボードごとのロックと覚え書き** | 4 | +400 前後 | [internal/instance/board.go](internal/instance/board.go)（新規168）と [internal/daemon/daemon.go](internal/daemon/daemon.go) / [internal/abandon/abandon.go](internal/abandon/abandon.go) / [internal/abandon/deps.go](internal/abandon/deps.go) の一部 |
| **doctor の検査を2つ追加** | 6 | +678 | [internal/doctor/checks.go](internal/doctor/checks.go)（+180）/ [test/internal/doctor/lock_file_test.go](test/internal/doctor/lock_file_test.go)（新規321）/ [test/internal/doctor/no_home_test.go](test/internal/doctor/no_home_test.go)（新規113） |
| **`--dry-run` が何も作らない一式** | 3 | +244 | [internal/lock/lock.go:87-105](internal/lock/lock.go#L87-L105) の `Probe` / [test/internal/lock/lock_test.go](test/internal/lock/lock_test.go)（新規83）/ [test/internal/abandon/dryrun_test.go](test/internal/abandon/dryrun_test.go)（新規120） |
| **herdr の agent 名に名前を混ぜる** | 3 | +96 | [internal/orchestrator/agentname.go](internal/orchestrator/agentname.go) / [internal/orchestrator/orchestrator.go](internal/orchestrator/orchestrator.go) |
| **文書** | 11 | +822 | [docs/plans/continuo_design.md](docs/plans/continuo_design.md)（+318）/ [docs/FAQ.md](docs/FAQ.md)（+234）/ [docs/upgrading.md](docs/upgrading.md)（+149）/ [docs/releasing.md](docs/releasing.md) / [docs/test_environment.md](docs/test_environment.md) |
| **残りのテスト** | 9 | +1090 前後 | [test/internal/daemon/wiring_test.go](test/internal/daemon/wiring_test.go)（+413）/ [test/internal/instance/instance_test.go](test/internal/instance/instance_test.go)（新規429）/ [test/internal/cli/cli_test.go](test/internal/cli/cli_test.go)（新規161） |

**区分は重なる。**[internal/daemon/daemon.go](internal/daemon/daemon.go) と
[internal/abandon/abandon.go](internal/abandon/abandon.go) と [internal/lock/lock.go](internal/lock/lock.go) は
2つ以上の区分にまたがるので、**ファイル数の縦の合計は55にならない**（重複を除いた実数が55である）。

---

## 3. AI が足したもの

**言いたいこと。**人間が頼んでいない追加が7つある。**うち6つは消せる。**
**根は1つで、「`--id` から `workspace.root` と branch 名を導く」である。**
それを消すと、その穴を塞ぐために足した目印ファイル・接頭辞の判定・3条件の裏付けが、まとめて消える。

| 足したもの | 消せるか | 減るファイル | 消すと消える指摘 |
| --- | --- | --- | --- |
| **置き場所と branch 名の派生**（根） | **消せる** | **7** | 名前の整え方を1関数に集める案 |
| **ボードごとのロックと覚え書き** | **消せる** | **4** | 覚え書きで生死を見る Critical 1件、起動時刻・消す順序の High 2件 |
| **doctor の検査を2つ追加** | **消せる** | **6** | doctor が置き場所を5つ作る／打ち間違えた名前を作る／親だけ見て答える の High 3件 |
| **`--dry-run` が何も作らない一式** | **消せる** | **3** | 観測の道具が存在しない High 1件（`lock.Probe` ごと消える） |
| **herdr の agent 名に名前を混ぜる** | **消せる** | **3** | なし（レビュー1回目の指摘への対応） |
| **設計 3-17h / 3-17i / 3-17j / 3-17k**（commit `7f6e0b9`） | **消せる**（差し戻し） | 0（+162行） | ロックを離す Critical 1件、ほか High 3件 |
| **socket と実行時ディレクトリの導出** | **消せる** | 0 | なし |
| **`--id` の名前の検査** | **消さない** | 0 | `--id ../../etc` が `~/.continuo` の外を指す経路を塞いでいる |

**根の裏付け。**[internal/instance/instance.go:342-350](internal/instance/instance.go#L342-L350) の `Apply` が
`cfg.Workspace.Root` に `/<名前>` を、`BranchTemplate` の先頭に `<名前>/` を足す。
**この2行が、[internal/workspace/instancemarker.go](internal/workspace/instancemarker.go)（新規126行）と
`BranchPrefixForSweep`（[internal/workspace/layout.go:254-260](internal/workspace/layout.go#L254-L260)）と
[internal/workspace/sweep.go:70-92](internal/workspace/sweep.go#L70-L92) の分岐を全部呼んでいる。**

**ボードのロックは確定仕様と逆を向く。**確定仕様は「使われてなければブロックしない」だが、
[internal/instance/board.go](internal/instance/board.go) は `--id` が空いていても同じボードなら止める。
覚え書きには [internal/instance/board.go:127-128](internal/instance/board.go#L127-L128) に
「書けなくても起動を止めてはならない。これは人間のための覚え書きであって、排他の一部ではない」とあり、
**その「無くても動く」ファイルの不在を、設計 3-17i は `not_running` と読む。**

**doctor は「作らない」と書いてあるのに作る。**[internal/doctor/checks.go:436](internal/doctor/checks.go#L436) が
`EnsureLockDir` を呼び、[internal/doctor/checks.go:561-567](internal/doctor/checks.go#L561-L567) の `probeLockFile` が
`os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)` でロックファイルを作る。

---

## 4. 案を3つ並べる

**言いたいこと。****「消して済ませる」（いちばん小さい）と「消さずに直す」（中くらい）の2つが実際の選択肢である。**
**この PR が触っている箇所の Critical 2 / High 8 は、どちらでもゼロにできる。**
**残る Critical 4 / High 13 は、この PR が触っていない箇所の欠陥で、どちらを選んでも1件も減らない。**

| 案 | 何を出すか | 触るファイル | Critical / High は残るか |
| --- | --- | --- | --- |
| **いちばん小さい** | `--id` とロックの固定と文書だけ。派生・ボードのロック・doctor の追加検査・`--dry-run` 一式・agent 名・設計 3-17h〜3-17k を全部やめる | **約30** | **この PR 由来は0。**ボード全体の Critical 4 / High 13 は別の作業として残る |
| **中くらい** | いちばん小さい案に、`--dry-run` が何も作らない一式と、doctor の追加検査を「作らない形」に作り直したものを足す | **約38** | **この PR 由来は0。**ただし作り直した分の code-review が1回増える |
| **いまのまま** | 55ファイル全部 | **55** | **Critical 6 / High 21 が残る。マージできない** |

### 4-1. いちばん小さい

- **何ができるようになるか。**`continuo --id test` で本番を止めずに2本目を起動できる。名前が使われていれば止まる。
  ロックは `~/.continuo/continuo.lock` の1本に固定される。`runtime.lock_file` が書いてあっても起動は通り、値は捨てる。
  **worktree と socket の分離は、テスト用の WORKFLOW.md を書き換えて行う**（確定仕様どおり）。
- **何ができないままか。****置き場所を分け忘れたときに止めてくれない。**2本目が1本目の走行中の pane を巡回のたびに閉じうる
  （既定30秒ごと）。**確定仕様の「挙動は使用者責任」の範囲だが、危険の中身は [docs/test_environment.md](docs/test_environment.md) に書く必要がある。**
  `continuo doctor` はロックの場所を検査しない。`--dry-run` は [internal/lock/lock.go:87-105](internal/lock/lock.go#L87-L105) の
  `Probe` ごと消えるので、いままでどおりの挙動に戻る。
- **セキュリティの穴は消えるか。****この PR が開けた穴は消える。**目印ファイルを書けば worktree を隠せる経路
  （[internal/workspace/instancemarker.go](internal/workspace/instancemarker.go)）は、派生ごと無くなる。
  **ボード全体のセキュリティの Critical 3件（hook の送り主を確かめない／グループ計画の書き手を確かめない／片付けの宛先を身元ファイルから読む）は1件も消えない。**

### 4-2. 中くらい

- **何ができるようになるか。**いちばん小さい案に加えて、`continuo doctor --id test` が
  「その名前で起動できるか」を答える（[internal/socketpath/socketpath.go:239-254](internal/socketpath/socketpath.go#L239-L254) と同じく
  `Lstat` で symlink・非ディレクトリ・権限の開きすぎを見るだけで、**作らない・掴まない**形に作り直す）。
  `continuo abandon --dry-run` が1バイトも書かない状態になる。
- **何ができないままか。**ボードごとのロックは無いので、置き場所の分け忘れは止まらない（いちばん小さい案と同じ）。
  agent 名は分かれないので、別のボードの同じ番号の issue が同じ agent 名になる。
- **セキュリティの穴は消えるか。**いちばん小さい案と同じ。**doctor を作り直す分だけ、新しい欠陥が入る余地がある。**
  作り直した検査は、いままで3回のレビューで毎回指摘が出続けた場所である。

### 4-3. いまのまま

- **何ができるようになるか。**`--id` で置き場所と branch と socket と agent 名が自動で分かれ、
  ボードが同じなら2本目を止め、doctor が17項目を検査する。
- **何ができないままか。****マージできない。**この PR 由来だけで Critical 2 / High 8 が残る。
- **セキュリティの穴は消えるか。**消えない。**この案を採る理由は無い。**

---

## 5. 推奨と、その理由

**言いたいこと。****「いちばん小さい」を推奨する。**
**理由は、消す対象がすべて「人間が頼んでいない追加」であり、しかも取り下げが issue に書いてあるのに実装が残った箇所だからである。**
**中くらいは、同じ結果（この PR 由来ゼロ）を、作り直しと再レビュー1回ぶん高い代金で買うことになる。**

**根拠1。指摘が名指ししている場所が、追加分の中にしかない。**
この PR が触っている Critical 2 / High 8 のうち、Critical 2件は設計 3-17h / 3-17i（commit `7f6e0b9`、差し戻し宣言済み）と
[internal/instance/board.go:127-128](internal/instance/board.go#L127-L128) を指す。High 8件も
3-17i / 3-17h / [internal/lock/lock.go:87-105](internal/lock/lock.go#L87-L105) /
[internal/doctor/checks.go:436](internal/doctor/checks.go#L436) と [internal/doctor/checks.go:561-567](internal/doctor/checks.go#L561-L567) /
[internal/instance/board.go:86-90](internal/instance/board.go#L86-L90) を指す。**確定仕様が求めた部分を指した指摘は1件も無い。**

**根拠2。取り下げが記録済みである。**issue #87（1台で continuo を複数動かすことを、正式な形にする）のコメントに
「**「`workspace.root` の末尾に `/<名前>` を足す」は採りません。**」と書いてあるのに、
[internal/instance/instance.go:342-350](internal/instance/instance.go#L342-L350) にその実装が残っている。
**消すのは新しい判断ではなく、記録済みの判断を実装へ反映することである。**

**根拠3。中くらいは、いちばん危ない場所を作り直す。**doctor の追加検査は、code-review 3回で毎回指摘が出た場所である
（残骸を作る／名前を渡さない／廃止を告げない／書き写し／パスの組み立てが重複／設計と食い違う）。
**同じ場所を「作らない形」に作り直せば、また同じ回数のレビューが要る。**
**doctor の検査は、この PR とは別の issue として立てるほうが、PR #112（1台で continuo を複数動かす）を早く閉じられる。**

**どちらの案でも、必ずやること。**

| 何を | どこへ | なぜ |
| --- | --- | --- |
| **「古いほうを止めてから入れ替える」を書く** | [docs/upgrading.md:75](docs/upgrading.md#L75)（いま「破壊的変更はありません」で始まる節） | ロックの置き場所が変わるので、動かしたまま入れ替えると2つ起動する |
| **警告を実行ファイルの差し替えより前に出す** | [install.sh:941-975](install.sh#L941-L975) | いまは置いたあとに警告するので、「止めてから」が言えない |
| **置き場所を分け忘れたときに何が起きるかを書く** | [docs/test_environment.md](docs/test_environment.md) | ボードのロックを消すなら、危険は文書でしか伝えられない |
| **main へ rebase する** | branch `feat/issue87-multi-instance` | 分岐点は commit `6a894fb`、main は commit `73fb41a` |

**この PR を縮めても消えないもの。**ボード全体のレビュー90件のうち、この PR が触っていない
**Critical 4件（hook の送り主を確かめない／グループ計画の書き手を確かめない／片付けの宛先を身元ファイルから読む／枠が9割を超えると着手しなくなる）と High 13件は、別の issue として立てる必要がある。**
**PR #112（1台で continuo を複数動かす）のマージ条件に混ぜてはならない。**混ぜると、この PR が永久に閉じられない。
