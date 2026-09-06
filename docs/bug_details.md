# 作りの形からくる問題

このファイルは、continuo を直すときに繰り返し噛みついてくる問題を集めたものです。
**1回直せば消えるバグは書いていません。**書いてあるのは「continuo がそういう作りだから、
別の場所を直しても同じ形で戻ってくる」問題だけです。

**新しくこのリポジトリを触るときは、コードを読む前にここを読んでください。**
7つとも、ここを知らずに触ると「なぜこう書いてあるのか」が分からず、
善意で直したつもりの変更が別の場所を壊します。

## 先に用語

このファイルで使う言葉です。continuo に固有の意味を持つものだけ並べます。

| 言葉 | 何を指すか |
| --- | --- |
| **表明** | エージェントが応答の最後に書く `CONTINUO-STATUS: review` のような1行。continuo はこの行を読んで、カンバンの Status を動かす |
| **turn** | continuo がエージェントへ指示を1つ送り、エージェントが応答し終えるまでの1往復 |
| **run** | 1つの issue に対して continuo が面倒を見ている作業のまとまり。worktree・pane・Claude Code のセッションが1組ぶら下がる |
| **印** | continuo が「この issue は自分が取った」と覚えている記録。プロセスのメモリの中にある `map` である |
| **巡回** | `polling.interval_ms`（既定30秒）ごとに、カンバンを読んで状態を突き合わせる処理 |
| **カンバン** | GitHub Projects v2 のプロジェクト1枚。continuo はこの Status フィールドを読み書きする |

## 一覧

| 呼び方 | 何が起きるか |
| --- | --- |
| [表明の受け取り口](#表明の受け取り口) | turn が途中で止められると、エージェントが言おうとしていたことが誰にも読まれずに捨てられる |
| [止める側と待つ側](#止める側と待つ側) | 巡回が pane を閉じても、待っている turn ループはそれを知らない |
| [共有されたカンバン](#共有されたカンバン) | GitHub の組み込みの自動化もカンバンを書き換える。誰が書いたかは記録から引けるが、戻し先は設定に書いてもらうしかない |
| [メモリの中の印](#メモリの中の印) | 「自分が取った」印は他の機械から見えない。2台で動かすと同じ issue を両方が取る |
| [3つの Status の集合](#3つの-status-の集合) | 設定に Status の集合が3つあり、重なり方によっては終わっていない issue を片付ける |
| [配り直せない雛形](#配り直せない雛形) | 雛形を直しても、既に動かしている人の `WORKFLOW.md` には届かない |
| [機械に貼り付く worktree](#機械に貼り付く-worktree) | worktree と Claude Code のセッションは取った機械の中にある。引き継ぐと会話の文脈が消える |

---

## 表明の受け取り口

### どういう形の問題か

**表明は turn が正常に終わったときにだけ読まれます。**turn が途中で止められると、
エージェントが書いた `CONTINUO-STATUS:` の行は Claude Code のファイルに残ったまま、
continuo からは二度と読まれません。**読み直す経路が1本もありません。**

### どこにあるか

| ファイル | 何をしているか |
| --- | --- |
| `internal/orchestrator/turn.go` | turn を送って待つ。待ち受けが返り、Stop hook を受けて初めて `turnEnded` を返す |
| `internal/orchestrator/lifecycle.go` | `handleTurnEnd` が `readSignals` を呼ぶ。**`readSignals` の呼び出し元はここ1箇所だけです** |
| `internal/orchestrator/signal.go` | `ParseSignals` が、行頭にある `CONTINUO-STATUS:` の行を拾う |
| `internal/orchestrator/transcript.go` | Claude Code が書く会話の記録（JSONL）を、その turn の範囲だけ切り出して読む |

**読まれるまでの流れは1本道です。**

```text
Claude Code が turn を終える
  → Stop hook が continuo の socket へ届く（OnHook）
  → turn ループの confirmTurnEnd が Stop を確かめ、turnEnded を返す
  → handleTurnEnd が readSignals を呼ぶ
  → 会話の記録を開いて CONTINUO-STATUS: の行を拾う
  → applySignals がカンバンの Status を書き込む
```

**この道に入らない終わり方が5つあります。**どれも `handleTurnEnd` を通りません。

| turn の終わり方 | いつ起きるか |
| --- | --- |
| 権限の確認で止まった | 許可されていないツールを使おうとした |
| Stop hook が届かなかった | hook の socket が変わった / エージェントが設定を上書きした |
| 指示を送れなかった | pane が消えた / herdr が応答しない |
| herdr との通信が一時的に失敗した | herdr を再起動した / socket が一瞬切れた |
| 巡回が横から run を終わらせた | カンバンの Status が動いた / 画面が止まったと判定された |

### どう噛みつくか

**issue #33 で実際に起きています。**エージェントが PR を作ったところ、GitHub の組み込みの
自動化がカンバンの Status を `In Progress`（continuo の設定に無い値）へ動かしました。
巡回はこれを「人間が引き渡した」と解釈して pane を閉じました。**その run は turn の途中でした。**
エージェントが何を書こうとしていたかは、読まれずに終わりました。

**「後から書かせ直す」経路はありますが、表明は読み直しません。**
`internal/orchestrator/comment.go` の `ensureAgentComment` は、run が終わるときに
「この run のコメントが issue に無ければ、セッションを復元してエージェントに書かせる」
ことをします。**書かせるのは issue のコメントだけで、表明は読み直しません。**
しかも巡回が横から止めた経路（`stopAndReleaseAsync`）は、この関数すら呼びません。

### 触るときに気をつけること

- **`readSignals` の呼び出し元を増やすときは、二重に反映されないかを先に確かめてください。**
  同じ turn の表明を2回読むと、`applySignals` が Status を2回書き、カンバンへの書き込みが2倍になります。
- **run を終わらせる新しい経路を足すときは、「表明を読むか」を必ず決めてください。**
  いまは「読む経路」と「読まない経路」が混ざっており、どちらが正しいかはコードに書かれていません。
- **会話の記録は turn の範囲を `promptId` で切り出しています。**
  次の着手ではセッションを取り直すので、その `promptId` は失われます。
  **後から読み直す仕組みを作るなら、どの範囲を読むかを別に決める必要があります。**

---

## 止める側と待つ側

### どういう形の問題か

**run を止める側（巡回のループ）と、turn の終わりを待つ側（turn ループ）は、別の goroutine です。**
巡回が pane を閉じても、待っている turn ループには何も伝わりません。
**turn ループが気づくのは、次に herdr を呼んでエラーが返ってきたときだけです。**

### どこにあるか

| ファイル | 何をしているか |
| --- | --- |
| `internal/orchestrator/orchestrator.go` | `Tick` が巡回を1回回す。`Run` が `polling.interval_ms` ごとにそれを呼ぶ |
| `internal/orchestrator/reconcile.go` | `reconcileRunning` がカンバンと突き合わせ、`checkStalls` が画面の止まりを判定する。**どちらも run を止めうる** |
| `internal/orchestrator/lifecycle.go` | `stopAndReleaseAsync` / `abandonRunAsync` / `finishRunAsync` が、別の goroutine で `stopWorker` を呼ぶ。`stopWorker` が `pane.close` を打つ |
| `internal/orchestrator/turn.go` | `turnLoop` が run ごとに1本走り、`agent.prompt` を待ち受けつきで呼んで止まっている |
| `internal/orchestrator/runstate.go` | `workerGeneration` / `currentWorker` / `beginTerminal` が「二重に諦めない」ための仕掛け |

**なぜ別々なのかには理由があります。**巡回のループから同期で run を終わらせると、
`agent.prompt` の待ち受け（既定1時間）でそこが止まり、**その間 dispatch も stall 検知も
全部止まります。**だから止める処理は `Async` の付いた関数で別の goroutine へ逃がしています。
**この分離は正しく、戻してはいけません。**問題は、分離した結果として通知の手段が無いことです。

### どう噛みつくか

**issue #33 のログにそのまま出ています。**

```text
「作業中でも完了でもない状態になったので worker を止めます（worktree は残します）」
「pane を閉じました」
「turn を送れませんでした ... herdr エラー [agent_not_running]: agent is no longer running in the target pane」
```

3行目は turn ループが出したものです。**pane が閉じられたことは、
herdr が `agent_not_running` を返してくるまで分かりませんでした。**

**この経路は、人間に見せる文面も間違えやすい形をしています。**
`turnSendFailed`（1文字も届いていない）と `turnStalled`（届いたが Stop が来なかった）を
混ぜると、**起きていないことを断定した文面が issue に残ります。**
turn.go にはそのための分類が5つ（`turnEnded` / `turnBlocked` / `turnStalled` /
`turnSendFailed` / `turnTransient`）あり、それぞれ別の文面を持っています。

### 触るときに気をつけること

- **巡回のループから、run を終わらせる関数を同期で呼ばないでください。**
  `finishRun` / `abandonRun` は待ち受けを含みます。巡回から呼ぶなら `Async` の付いたほうです。
- **止める経路を足すときは `beginTerminal` を必ず通してください。**
  通さないと、同じ run を2つの goroutine が同時に終わらせ、
  引き渡しのコメントが二重に投稿され、リトライの回数が2倍の速さで減ります。
- **turn ループ側で run を諦めるときは `currentWorker` で世代を確かめてください。**
  待っている間に巡回が先に諦めていることがあります。
- **herdr のエラーを「一時的か恒久的か」で切り分けてください。**
  herdr を再起動しただけで走っている run を捨てると、リトライを使い切って issue が保留へ落ちます。

---

## 共有されたカンバン

### どういう形の問題か

**カンバンは continuo だけのものではありません。**人間も、GitHub Projects v2 の組み込みの
自動化も、同じ Status を書き換えます。**書いたのが自動化かどうかは、issue の記録
（`ProjectV2ItemStatusChangedEvent`）の `actor.__typename` が `Bot` かどうかで引けます。**

**引けるのはそこまでです。**「その issue を本来どの Status に戻すべきか」は記録に無いので、
**`tracker.automated_state_rewrite` に人間が書いた対応表を引きます。**
**書いていなければ、いままでどおり「人間が引き渡した」と解釈して worker を止めます。**

### どこにあるか

| ファイル | 何をしているか |
| --- | --- |
| `internal/orchestrator/reconcile.go` | `reconcileRunning` が毎巡回で Status を取り直して分類する |
| `internal/orchestrator/unknownstate.go` | `claimAutomatedRewrite` が「書いたのは自動化か」を見て、戻すか止めるかを決める |
| `internal/orchestrator/lifecycle.go` | `handleTurnEnd` が turn の終わりに同じ分類をする |
| `internal/tracker/query.go` | `judgeStatusAuthor` が issue の記録から書いた主体を引く |
| `internal/tracker/adapter.go` | `UpdateStatus` が Status を書く。**書く前に読み直しますが、compare-and-swap ではありません** |

**分類は4つです。**

| 取り直した Status | continuo の解釈 | 何をするか |
| --- | --- | --- |
| `terminal_states` にある | 終わった | worktree と branch を片付ける |
| `active_states` にあり、着手できる | まだ作業中 | 次の turn を送る |
| **設定に名前が無く、書いたのは自動化で、対応表に戻し先がある** | **横取りされた** | **本来の Status へ戻す。止めない** |
| **それ以外のすべて** | **人間が引き渡した** | **worker を止める。worktree は残す** |

**最後が「それ以外のすべて」であることが、この問題の形です。**
**対応表に書いていない自動化の書き込みも、人間が手で動かした値も、まだここに落ちます。**

### どう噛みつくか

**issue #33（直しました）。**エージェントが PR を作ると、組み込みの自動化
「Pull request linked」が Status を `In Progress` へ動かしました。設定の `active_states` は
`["AI Ready", "AI In Progress"]` なので、`In Progress` はどこにも入りません。
**人間は何も操作していないのに、worker が止まりました。**
**いまは書いた主体を見て、対応表に戻し先があれば書き戻します**（設計 3-54）。
**対応表を書いていないカンバンでは、いまも止まります。**

**issue #35。**PR をマージすると、組み込みの自動化「Pull request merged」が
Status を `Done` へ動かしました。設定は `terminal_states: ["AI Done"]` なので
「終わった」とは判定されず、しかし `cleanup.on_states: ["Done"]` には入るので
**worktree は片付けられました。**「終わっていないのに片付ける」状態です
（[3つの Status の集合](#3つの-status-の集合) と同じ根です）。

**遷移先を起動時に確かめることはできません。**GraphQL の `ProjectV2Workflow` から引けるのは
`number` / `name` / `enabled` の3つだけで、**どの Status へ動かすかは返ってきません。**
カンバンの画面で見るしかありません。

**continuo が動かしたときだけは、記録が残ります。**continuo がカンバンへ書き込むと、
**何から何へ動かしたのか・なぜ動かしたのか・いつ動かしたのかを issue にコメントします**
（設計 3-29）。**カンバンの自動化が動かしたぶんは、いまも何も残りません。**
**ただし continuo がそれを書き戻したときは、書き戻したぶんの記録が1件残ります。**
記録の無い遷移を見つけたら、それは continuo 以外が書いたということです。

### 触るときに気をつけること

- **「知らない Status＝人間の引き渡し」という前提のコードを増やさないでください。**
  その前提はもう成立していません。書いた主体は人間・continuo・カンバンの自動化の3つあります。
- **`actor.__typename` を見る経路を増やすときは、`project.number` で自分のカンバンへ絞ってください。**
  1つの issue が2枚のカンバンに載っていると、**両方のカンバンのイベントが同じ配列で返ります。**
- **Status を書く経路を足すときは、書く前に読み直してください。**
  `UpdateStatus` は読んでから書きますが、**読み直しと書き込みの間に他人が動かした場合は上書きします。**
  GitHub Projects v2 に compare-and-swap はありません。
- **`updateProjectV2Field` を実データの入ったカンバンで呼んではいけません。**
  選択肢の指定は全件置き換えとして扱われ、設定済みの Status の値が全部消えます。
  選択肢を足すのは人間が画面から行う操作です。
- **新しい Status の役割を設定に足すときは、カンバンの自動化が同じ値を書きうるかを先に考えてください。**

---

## メモリの中の印

### どういう形の問題か

**「この issue は自分が取った」という印は、continuo のプロセスのメモリの中にしかありません。**
ファイルにも、カンバンにも、共有された場所にも書かれません。
**他の機械からは見えないので、2台で同じカンバンを見張ると同じ issue を両方が取ります。**

### どこにあるか

| ファイル | 何をしているか |
| --- | --- |
| `internal/orchestrator/orchestrator.go` | `runs` が印です。`map[string]*runState` で、キーは project item の ID。`claim` で足し、`release` で消す |
| `internal/lock/lock.go` | 二重起動の防止。`flock(2)` を1本取るだけで、**同じ機械の中でしか効きません** |
| `internal/tracker/adapter.go` | `UpdateStatus`。読んでから書くだけで、比較して書き換える仕組みはありません |

**設計は「状態は in-memory。永続化層を作らない」と決めています**
（`docs/plans/continuo_design.md` の 3-4）。落ちたあとは worktree の中の身元ファイル
（`.continuo.json`）から復元します。**身元ファイルもその機械のディスクの中にあります。**

### どう噛みつくか

**issue #36 が、これを正面から取り上げています。**
「チームで持ち回りにして、そのとき枠に余裕がある人の機械が処理したい」という要望です。
いまの作りでは次のようになります。

| 何 | どこまで届くか |
| --- | --- |
| 「自分が取った」印 | **そのプロセスの中だけ** |
| 二重起動の防止 | **その機械の中だけ** |
| 枠の残り | **自分の分だけ。他の人の残量は知らない** |

**取り合いの窓は巡回の間隔ぶんあります。**2台が同時に着手待ちの issue を読み、
両方が印を付け、両方が作業中の Status を書けます。既定では30秒です。

### 触るときに気をつけること

- **「印があるから安全」と書かれている箇所は、すべて「この機械の中では安全」の意味です。**
  複数台を前提にした話を足すときは、その但し書きが要ります。
- **共有された印を作るなら、比較して書き換えられる場所が要ります。**
  issue #36 は git の ref を候補にしています（既にある ref への非 fast-forward な push を
  GitHub が断ることを利用する）。カンバンの Status では代用できません。
- **`agent.max_concurrent_agents` は1台あたりの上限です。**3台で動かすと3倍になります。
- **印を取ったまま機械が落ちると、その worktree はその機械にしかありません。**
  他の機械からは「印はあるが誰も動いていない」と見分けられません。

---

## 3つの Status の集合

### どういう形の問題か

**設定には Status の集合が3つあり、互いに重なりえます。**
`tracker.active_states` / `tracker.terminal_states` / `cleanup.on_states` の3つです。
**起動時の検証は、この3つのうち一部の組み合わせしか見ていません。**
見ていない組み合わせで矛盾した設定を書くと、起動は通り、動いてから壊れます。

### どこにあるか

| ファイル | 何をしているか |
| --- | --- |
| `internal/config/validate.go` | 3つの集合の重なりを検査する。**下の表の「見ている」ものだけ** |
| `internal/config/default.go` | 既定値を決める |
| `internal/orchestrator/reconcile.go` | `reconcileRunning` が `terminal_states` と `active_states` を読む |
| `internal/orchestrator/lifecycle.go` | `handleTurnEnd` が同じ2つを読み、`finishRunClaimed` が `ShouldCleanup` を呼ぶ |
| `internal/workspace/cleanup.go` | `ShouldCleanup` が `cleanup.on_states` だけを読む |
| `internal/orchestrator/sweep.go` | 起動時の掃除も `cleanup.on_states` を読む |

**設定にある Status の項目は6つあります。**

| キー | 何を指すか |
| --- | --- |
| `tracker.active_states` | 対象にする Status の集合 |
| `tracker.terminal_states` | 終わったとみなす Status の集合 |
| `cleanup.on_states` | worktree を片付ける Status の集合 |
| `tracker.running_state` | 着手したときに書き込む Status（1つ） |
| `tracker.dispatch_state` | 着手待ちの Status（1つ） |
| `tracker.failure_state` | 打ち切ったときに落とす Status（1つ） |

**検証が見ているものと、見ていないものは次のとおりです。**

| 組み合わせ | 検証は | 破ると何が起きるか |
| --- | --- | --- |
| `running_state` が `active_states` に入るか | 見ている | 着手した直後に自分の worker を候補から外す |
| `dispatch_state` が `active_states` に入るか | 見ている | 着手待ちの issue を1件も拾わない |
| `active_states` と `terminal_states` が重なるか | 見ている | 片付けた issue を次の巡回で作業中として拾い直す |
| `failure_state` が `active_states` に入るか | 見ている | 打ち切った issue が永久に再着手される |
| `cleanup.on_states` と `active_states` が重なるか | 見ている | 作業中の worktree を消す |
| **`cleanup.on_states` が `terminal_states` に入るか** | **見ていない** | **終わっていない issue の worktree を片付ける** |

### どう噛みつくか

**issue #35 がこの穴を踏み抜いた形です。**報告された設定はこうなっていました。

```yaml
terminal_states: ["AI Done"]
cleanup:
  on_states: ["Done"]
```

**この2つは重なっていませんが、検証は通ります。**そして Status が `Done` になると、
continuo は次のように振る舞います。

| 見るもの | `Done` は入っているか | 結果 |
| --- | --- | --- |
| `terminal_states` | 入っていない | 終わったとみなさない |
| `active_states` | 入っていない | 知らない Status なので worker を止める |
| `cleanup.on_states` | **入っている** | **worktree を片付ける** |

**この3つは同じ turn の中で順に評価されます。**`handleTurnEnd` が「引き渡し」と判定して
`finishRun` を呼び、その中の `finishRunClaimed` が Status を取り直して `ShouldCleanup` に
かけ、真なので片付けます。**「終わっていない」と判定した直後に片付けています。**

### 触るときに気をつけること

- **集合を1つ足すたびに、既存の集合との重なりを全部書き出してください。**
  3つで6通りあり、いまは5通りしか見ていません。4つ目を足すと12通りになります。
- **検証を厳しくすると、既に動いている設定ファイルが起動しなくなります。**
  この点は [配り直せない雛形](#配り直せない雛形) と直結します。
  止めるのか警告に留めるのかを、先に決めてください。
- **`cleanup.on_states` を読む場所は2箇所あります**（巡回中の片付けと、起動時の掃除）。
  片方だけ直すとずれます。

---

## 配り直せない雛形

### どういう形の問題か

**`WORKFLOW.md` の雛形を書き出すのは `continuo init` だけです。**
一度書き出されたファイルを、continuo が後から作り直すことはありません。
**雛形のほうを直しても、既に動かしている人には1文字も届きません。**

### どこにあるか

| ファイル | 何をしているか |
| --- | --- |
| `internal/scaffold/template.go` | 雛形の本文。`WORKFLOW.md` の中身そのもの |
| `internal/scaffold/scaffold.go` | `WriteTemplate` が雛形を書き出す。既にファイルがあれば `--force` なしでは断る |
| `internal/scaffold/fill.go` | `statusKeys` が、`continuo setup` の書き換え対象のキーを並べる |
| `internal/scaffold/update.go` | `UpdateStatuses` が、そのキーの行だけを書き換える |

**2つのコマンドの役割は分かれています。**

| コマンド | 何をするか |
| --- | --- |
| `continuo init` | 雛形を新規に書き出す。**既にファイルがあれば断る**（`--force` を付けたときだけ上書きする） |
| `continuo setup` | 既にある `WORKFLOW.md` の、**8つのキーの行だけ**を書き換える。他の行・空行・並び順・インデント・行末のコメントは1文字も変えない |

**`continuo setup` が雛形で丸ごと上書きしないのは意図的です。**上書きすると、
利用者がその間に手で直した行（`workspace.root`、`agent.max_concurrent_agents`、
`trust.repositories` から消した行など）が全部消えるからです。**この判断は正しく、
戻してはいけません。**問題は、その結果として雛形の変更を配る手段が無いことです。

### どう噛みつくか

**issue #35 がこの形です。**`cleanup.on_states` は、当時 `continuo setup` の
書き換え対象に入っていませんでした。そのため、利用者がカンバンの Status を
`AI Done` に割り当てても、**`cleanup.on_states` は雛形の `["Done"]` のまま残りました。**

**いまは `statusKeys` に入っています。**しかしそれが効くのは、
**これから `continuo setup` を通す人だけです。**既に `WORKFLOW.md` を持っている人は、
自分で `continuo setup` を叩き直すか、手で書き換えるまで古い値のままです。

**気づく手立てもありません。**`Done` は `active_states` に入っていないので設定の検証を通り、
起動時の Status の照合は `cleanup` を見ません。利用者から見えるのは
「`Done` にしたのに worktree が消えない」だけです。

**関連: issue #38**（破壊的変更が入った版へ上げるとき、インストーラーが警告する）。

### 触るときに気をつけること

- **雛形の既定値を変えるのは、これから作る人へ配るだけの変更です。**
  既に動いている人へ届けたいなら、次のどれかが別に要ります。

  | 手段 | 何をするか |
  | --- | --- |
  | `continuo setup` の書き換え対象に足す | `internal/scaffold/fill.go` の `statusKeys` に足す。**利用者が `continuo setup` を叩き直したときにだけ効きます** |
  | 起動時の検証で気づかせる | `internal/config/validate.go`。**止めるか警告かを決めてください** |
  | release notes に手順を書く | 利用者が自分で直す。手順は具体的なコマンドで書くこと |

- **front matter は未知のキーを弾きます。**キーを消す・改名すると、
  古い `WORKFLOW.md` はその版で起動しなくなります。
- **雛形の行末のコメントも配れません。**説明を直しても、既存のファイルには古い説明が残ります。

---

## 機械に貼り付く worktree

### どういう形の問題か

**worktree も Claude Code のセッションも、その issue を取った機械の中にあります。**
別の機械が引き継ぐと、**それまでの会話の文脈は消えます。**
branch は push してあれば残りますが、エージェントが何を考えて何を試したかは残りません。

### どこにあるか

| ファイル | 何をしているか |
| --- | --- |
| `internal/workspace/layout.go` | 置き場所を決める。`<root>/<host>/<owner>/<repo>/<スラグ>` の4階層に固定 |
| `internal/workspace/identity.go` | 身元ファイル `.continuo.json` の中身。session_uuid / settings_path / herdr_workspace_id / agent_name が入る |
| `internal/orchestrator/transcript.go` | Claude Code の会話の記録を読む。ファイル名はセッション UUID |
| `internal/orchestrator/prompt.go` | 作り直すときのプロンプト。`{{if .attempt}}` の経路で「前回は完了せずに終わっている」と伝える |

**`<host>` は issue の URL のホスト名です**（`github.com`。GitHub Enterprise なら別の値）。
**機械の名前ではありません。**置き場所の根（`workspace.root`。既定 `~/worktrees`）が
その機械のローカルのパスであることが、機械に貼り付く理由です。

**身元ファイルに入っている値は、どれもその機械の中でしか意味を持ちません。**

| 項目 | なぜ他の機械で使えないか |
| --- | --- |
| `session_uuid` | Claude Code の会話の記録は、その機械のディスクにある |
| `settings_path` | continuo がその機械に書いた設定ファイルの絶対パス |
| `herdr_workspace_id` | その機械で動いている herdr の中の ID |
| `agent_name` | 同じく、その機械の herdr の中の名前 |

### どう噛みつくか

**issue #36 が「途中で枠が尽きたら」としてこれを挙げています。**
チームで持ち回りにしたいという要望に対して、選べる案は2つしかありません。

| 案 | 何が起きるか |
| --- | --- |
| 引き継がない | その機械の枠が戻るまで、その issue は止まる |
| 引き継ぐ | branch は残るが、**それまでの会話の文脈は消える** |

**引き継いだ側は、issue のコメントに書かれたことしか知りません。**
だから continuo は run が終わるときに `ensureAgentComment` でコメントを書かせています。
**あれは記録のためであると同時に、引き継ぎのための唯一の手段でもあります。**

**push していないものは、他の機械からは存在しないのと同じです。**
`cleanup.require_pushed` が守るのは、片付けのときに消さないことだけです。
**別の機械に見せる働きはありません。**

### 触るときに気をつけること

- **引き継ぎを設計するときは、何が機械に貼り付いているかを先に列挙してください。**
  worktree・会話の記録・設定ファイル・herdr の workspace と pane・agent 名の5つです。
- **`WORKFLOW.md` を複数の機械で共有するなら、機械ごとに違う値を洗い出してください。**
  とくに `rate_limit.token_source` は OS で違います（macOS は Keychain、
  Linux は `~/.claude/.credentials.json`）。
  **一覧と、配るときの手順は [FAQ.md](FAQ.md) の
  「1枚の WORKFLOW.md をチームで共有して、余裕がある機械に処理させたい」にあります。**
- **身元ファイルはエージェントが書き換えられます。**worktree の直下にあり、
  そこでエージェントが確認なしのモードで動くからです。**この値を宛先にして
  pane を閉じてはいけません**（別の run の Claude Code を turn の途中で殺せます）。
  閉じる pane は herdr 自身に答えさせてください。

---

## この文書を更新するとき

- **「1回直せば消えるバグ」はここに書かないでください。**それは issue に書きます。
- **行番号を書かないでください。**コードが動くと必ずずれます。ファイル名と関数名で指してください。
- **実例には issue の番号を添えてください。**番号だけを本文に置くのではなく、
  何が起きたかを1文で書いてから添えてください。
- **このリポジトリは公開されています。**実在の組織名・リポジトリ名・個人の絶対パスは書かず、
  `<owner>/<repo>` と `~/` を使ってください。
