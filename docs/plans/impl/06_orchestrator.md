<!-- 目的: 巡回・dispatch・turn ループ・照合・リトライを実装するタスク -->

# 06. orchestrator

**言いたいこと。ここで1件の issue が最初から最後まで通る。**
**着手の手順は13段あり、順番が意味を持つ。**順番を変えると、途中で落ちたときに同じ worktree で Claude Code が2つ立つ。

## 読むもの（設計）

| 節 | 何が書いてあるか |
| --- | --- |
| 3-16 | **着手の手順13段（段-1 から段11）。**なぜその順番かの根拠つき |
| 3-4 | 状態は in-memory。`runState` の型 |
| 3-5 | 完了検知の3層（turn が終わったか / 完了したか / 何をしたか） |
| 3-8 | turn ループと `max_turns` |
| 3-14 | **turn の数え方。**continuo が送った回数だけ数える |
| 3-10 | **`In Progress` も「作業中」に含める。**入れ忘れると自分の worker を殺す |
| 3-25 | **表明の読み取り（transcript を `promptSource == "typed"` 起点で区切る）**と、コメントを書かせ直す9段 |
| 3-26 | グループの扱い（代表を1件 dispatch する） |
| 3-27 | レートリミットで止まったときの復帰と、**枠待ちの判定2条件** |
| 3-29 | プロンプトに issue の本文を埋め込まない |
| 3-11 | **`blocked` が返ったら `esc` を送ってから人間へ渡す** |
| 3-21 | stall の検知とリトライ |
| 3-31 | GraphQL のコストを枠に収める |
| 4-2 | 並び順つきで読み、**空きスロットが尽きるまで dispatch する** |
| 5-3 / 5-4 | **プロンプトのテンプレートと、渡す変数の一覧。**2回目以降の文面 |
| 5-5 | 展開規則（`${VAR}` と `~`） |

## 作るもの

| パッケージ | 何を |
| --- | --- |
| `internal/orchestrator` | 巡回・dispatch・turn ループ・照合・リトライ・stall 検知 |
| `internal/orchestrator` | 表明の読み取り（transcript のパース） |
| `internal/ratelimit` | usage API を読む（`rate_limit.source: none` なら1回も叩かない） |

**第3段階のアダプタに1つ足す。**

```text
FetchIssueByIdentifier(ctx, "maimuzo/koetsumugi#45") → (Issue, bool, error)
  → グループの表明（CONTINUO-STATUS: #45 review）から item を引くのに要る（設計 3-25）。
    その issue は Ice Box にあるので、巡回で読んだ候補には入っていない
  → bool は「ボードに載っているか」である。載っていなければ (ゼロ値, false, nil) を返す。
    エージェントが存在しない issue 番号を書くことはありうるので、それをエラーにしない
```

> **戻り値は3値である**（設計 3-25、[docs/plans/continuo_design.md](docs/plans/continuo_design.md) の 3-25）。
> **「見つからない」をエラーで表さない。**エラーは通信の失敗と権限の不足だけに使う。

**`internal/herdr` の `AgentSendKeys` は実装済みである。**`["esc"]` を送るのに使う。

## 受け入れの基準

**1件の issue が最初から最後まで通ることを、Claude Code を起動せずに確かめる。**

**使うのは第2段階と同じ偽の socket サーバである。実 herdr は使わない。**
`pane.report_agent` で「agent が居る」と登録できることは第2段階で確かめてあるが、
**実 herdr を使うと、テストが落ちたときに workspace と pane が残る。**
**偽サーバなら、応答を台本として書けるので `blocked` や `working` も再現できる。**

**git は本物を使う。**テスト用の一時ディレクトリに `git init` した bare リポジトリを置き、
そこから worktree を作る。**worktree の作成と削除、`git branch -D`、
`git status --porcelain` と `git diff --quiet` の判定は、偽物では確かめられない。**

- [x] **着手の13段を、設計の順番どおりに実行する**
- [x] **pane を新しく作らない。**`worktree.open` が作った workspace の pane を `pane.list` で引く（設計 3-16 の段8）
  - **`pane.split` も `tab.create` も呼ばない**（4-5。1 worktree = 1 workspace）
- [x] **環境変数は設定ファイルの `env` に書く。**pane にも `agent.start` にも渡さない（設計 3-12。実測で確認済み）
- [x] **「実行中の一覧」と「自分が取った印の集合」は同じ `map[string]*runState` 1本である**（設計 3-25）
- [x] **バックオフが明けた run は、巡回の先頭で印の集合を走査して拾う**（設計 3-21）
  - 段0 から入り直す。段1 は飛ばす。**セッション UUID は新しく採番する**
- [x] **stall の閾値に達したら、枠待ちの判定を先に見る**（設計 3-27 の評価順）
  - 「時計を止める」は `runState.WaitingQuota` を立てて判定を飛ばすこと。`LastSeenAt` は進めない
- [x] **枠のトークンは `~/.claude/.credentials.json` からだけ読む。Keychain は読まない**（設計 3-15）
  - **macOS では取れないのが普通。**取れなければ枠の判定を諦め、起動は続ける
- [x] **段2 で書き込む先は `tracker.running_state`（既定 `In Progress`）である。**ハードコードしない
- [x] **agent 名を設計 3-3 の4段で作る**（32文字に収める。重複したら末尾に連番）
- [x] **空きスロットの検査が、印を付ける前に走る**（段-1）
- [x] **`Ready` と `In Progress` の両方を候補にする。**片方だけだと再起動後に取り残される
- [x] **既に印を持っている issue を dispatch しない**
- [x] **Status を書く前に、必ず ID 指定で取り直す。`terminal_states` に入っていたら書かない**（設計 3-4）
  - **`active_states` で絞らない。**グループの issue は `Ice Box` にあるので、絞ると1件も反映されない
- [x] **turn の終わりを 1-3 の判定で確定する**（herdr の待ち受けが主、`<task-notification>` の検出が従）
- [x] **turn が終わってから 0.5 秒待って transcript を読む。**見つからなければ **0.1 秒間隔で最大5回**読み直す
  - **5回で諦める**（待つ合計は 0.5 + 0.5 = 1.0 秒）。それでも無ければ「表明なし」として扱い、次の turn で促す
- [x] **同じ1回の読み取りで、表明とトークンの両方を取る**（設計 3-15）。**2回開かない**
  - 集計は `requestId` で重複排除する。**結果は `runState` に持たず、その場でログに出す**
  - **ダッシュボード（第9段階）が使うなら、そのとき置き場所を決める**（いまは決めない）
- [x] **表明を `promptSource == "typed"` 起点で区切って拾う。`prompt_id` では区切らない**
- [x] **印が複数あれば、issue ごとに最後に現れたものを採る**
- [x] **`isSidechain == false` に絞る**（subagent の発言を拾わない）
- [x] **`max_turns` を、continuo が送った回数だけで数える**（`<task-notification>` は数えない）
- [x] **`blocked` が返ったら、次を投げる前に `agent.send_keys` で `["esc"]` を送る**
- [x] **worker を止めるのは `pane.close` である**（設計 3-5）。agent だけを止めるメソッドは herdr に無い
- [x] **表明が無かった turn の次に、それを促す1文を継続の指示へ差し込む**（設計 3-8 / 3-25）
  - **hook から差し戻す仕組みは採らない**（設計 3-25）
- [x] **stall の時計が `PreToolUse` / `PostToolUse` でリセットされる**
- [x] **閾値を超えたら `agent_status` を1回見て、`working` なら猶予を1回だけ与える**
  - **猶予＝`LastSeenAt` を現在時刻にして、もう一度 `stall_timeout_ms` だけ待つ**（設計 3-14）
  - **与えたことを `runState` に記録し、2回目は与えない。**`working` のまま固まる場合があるため
- [x] **枠待ちの判定が2条件の連言になっている**（`percent` が 100、かつその run から hook が来ていない）
  - **`severity` は見ない。**上限を示す値が何かを実測できていない（設計 3-27）
- [x] **`rate_limit.source: none` なら usage API を1回も叩かない**（設定の検証は対応済み）
- [x] **資格情報が取れなかったら、枠の判定を諦めて `none` と同じ動きにする。起動は止めない**（設計 3-27）
  - **macOS では `~/.claude/.credentials.json` が無いのが普通である**（Keychain にある）
- [x] **`runState.PromptID` は `UserPromptSubmit` を受けた時点で入れる**（投入時には取れない。設計 3-25）
- [x] **枠待ちの run についてだけ、stall の時計と `turn_timeout_ms` を止める**
- [x] **枠が明けたときに送る継続の指示を、turn 数に数える**（設計 3-27）
  - **数えないと、枠待ちと復帰を繰り返す間に `max_turns` が一度も発火しない**
- [x] **run が終わるときにコメントを確かめ、無ければ 3-25 の9段で書かせる**（毎 turn ではない）
  - **「この run が書いたもの」だけを数える**（marker があり、`runState.StartedAt` より新しいもの）
  - **worktree を再利用すると前の run のコメントが残っている**（設計 3-25）
- [x] **権限で拒否されたことを continuo は検知しない**（設計 3-11）。エージェントが応答に書く
- [x] **巡回1回のリクエストが3本を超えない**（候補の取得・実行中の照合・worktree の照合）
  - **Status の選択肢名の照合は毎巡回では行わない**（`tracker.verify_states_every`。既定 20 巡回に1回）
  - **`gh` の認証の検査も同じ頻度で行う**（設計 3-6 の「巡回ごとに検査するもの」）。
    **毎巡回で外部プロセスを起動しない。**失敗したらその巡回の dispatch を飛ばし、実行中の照合は止めない
- [x] **1回目のプロンプトに issue の本文とコメントを入れない**（設計 3-8 / 3-29）
- [x] **テンプレートの描画に `missingkey=error` を付ける。**失敗したらその issue を失敗として扱う
- [x] **turn ループを run ごとの goroutine で動かす。**巡回のループをブロックしない（設計 3-8）
  - **`agent.prompt` を wait つきで呼ぶと turn の終わりまで返らない**（既定1時間）
- [x] **`runState.NeedsPrompt` が立った run に turn を送る**（復元が立てる。設計 3-4 の段5c）
- [x] **未信頼のリポジトリへのコメントは、そのリポジトリにつき1回だけ**（設計 3-6）
  - **キーは `<owner>/<repo>`。**issue ごとではない。**素朴に実装すると30秒ごとに永久に積まれる**
  - **書く先は、そのリポジトリで最初に候補に上がった issue 1件**（`Issue.NativeRef["issue_node_id"]`）
  - **draft issue はノード ID を持たないので、コメントせずログだけにする**
- [x] **`agent.prompt` を wait つきで送る**（`until = [idle, done, blocked]` / `timeout_ms = turn_timeout_ms`）
  - **`agent.wait` を単独で使わない。**いまの状態が `until` に含まれると 0.006 秒で即返る（実測）ため、
    投入直後の `idle` を turn の終わりと取り違える
  - **timeout で返ったら枠待ちかを判定し、枠待ちなら `agent.wait` で待ち直す**（`agent.prompt` は再送しない）
- [x] **待ち受けが返っても `Stop` が来ていなければ、`settle_ms` 待ってから stall として扱う**
- [x] **リトライ回数とバックオフを `runState` に持つ。**バックオフ中も印に残す（拾い直されないため）

## 落とし穴（実測で分かっている）

- **`In Progress` を候補に入れ忘れると、dispatch した直後に自分の worker を殺す**
- **`background_tasks` が空でも turn は続く。**空だけで完了と判定してはいけない
- **`blocked` のまま次を投げると、保留中の権限要求が承認されて実行される**（3/3 で再現）
- **`last_assistant_message` に印は入らない**（印を書いたあと道具を呼ぶと落ちる。0/17）
- **`agent_type` が空文字の `SubagentStop` を数えると壊れる**

## 実装の記録

**言いたいこと。**受け入れの基準は37件すべて満たした。**1件の issue が候補に上がってから
`Done` で片付くまでを、Claude Code を起動せずに1本のテストで通してある。**
**`cmd/continuo` の常駐ループには、まだ繋いでいない**（理由は下記）。

### 作ったもの

| パッケージ | ファイル | 何を |
| --- | --- | --- |
| `internal/orchestrator` | [orchestrator.go](../../../internal/orchestrator/orchestrator.go) | 巡回1回（`Tick`）・印の集合・hook の受け口（`OnHook`）・復元の入口（`Adopt`） |
| `internal/orchestrator` | [runstate.go](../../../internal/orchestrator/runstate.go) | `runState`。**すべてのフィールドを排他で守る**（巡回・turn ループ・hook の配送が同時に触る） |
| `internal/orchestrator` | [dispatch.go](../../../internal/orchestrator/dispatch.go) | 着手の13段・空きスロット・信頼の検査・未信頼の通知 |
| `internal/orchestrator` | [turn.go](../../../internal/orchestrator/turn.go) | turn ループ・turn の終わりの判定・枠待ちの待ち直し |
| `internal/orchestrator` | [lifecycle.go](../../../internal/orchestrator/lifecycle.go) | 表明の適用・片付け・リトライ・引き渡し |
| `internal/orchestrator` | [comment.go](../../../internal/orchestrator/comment.go) | コメントを書かせ直す9段（`--resume` での復元） |
| `internal/orchestrator` | [reconcile.go](../../../internal/orchestrator/reconcile.go) | バックオフの拾い直し・実行中の照合・worktree の照合・stall 検知 |
| `internal/orchestrator` | [transcript.go](../../../internal/orchestrator/transcript.go) | transcript の1回の読み取りで表明とトークンの両方を取る |
| `internal/orchestrator` | [signal.go](../../../internal/orchestrator/signal.go) | 表明の行の解析（グループの `#45` を含む） |
| `internal/orchestrator` | [settings.go](../../../internal/orchestrator/settings.go) | issue ごとの Claude Code の設定ファイル（hook 8種 + `permissions` + `env`） |
| `internal/orchestrator` | [prompt.go](../../../internal/orchestrator/prompt.go) | 1回目のテンプレートの描画と、2回目以降の文面の組み立て |
| `internal/orchestrator` | [agentname.go](../../../internal/orchestrator/agentname.go) | agent 名の4段とセッション UUID の採番 |
| `internal/ratelimit` | [ratelimit.go](../../../internal/ratelimit/ratelimit.go) | usage API の読み取り。`none` なら1回も叩かない |
| `internal/tracker` | [by_identifier.go](../../../internal/tracker/by_identifier.go) | `FetchIssueByIdentifier`（3値。Status で絞らない） |
| `internal/herdr` | [errors.go](../../../internal/herdr/errors.go) | `ErrCodeTimeout` を足した（turn の時間切れと枠待ちを分ける起点） |

### 自分で決めたこと（設計に書かれていなかったもの）

| 何を | どう決めたか | なぜ |
| --- | --- | --- |
| **`Tracker` / `HerdrClient` をインタフェースで受ける** | `*tracker.Adapter` と `*herdr.Client` が満たすことを `var _ Tracker = (*tracker.Adapter)(nil)` でコンパイル時に表明する | **巡回1回のリクエスト本数（3本）をテストから数えるため。**テストは偽物しか渡さないので、表明が無いとシグネチャがずれても第7段階まで誰も気づかない |
| **送る本文は turn 数ではなく `runState.FreshSession` で決める** | 新しいセッション UUID で起動した直後（着手・再 dispatch）だけ真にし、真なら1回目の本文（5-3）、偽なら継続の指示（5-4）を送る | **turn 数では分けられない。**復元で引き継いだ run は turn 数を 1 から数え直すが**セッションは引き継いでいる**ので継続の指示である（3-4 の段5c）。逆に再 dispatch は turn 数を引き継ぐが**セッションは新しい**ので、継続の指示だけでは何をすべきか伝わらない |
| **run を終わらせる処理を1本に絞る（`beginTerminal`）** | 終わらせ始めた印を立て、リトライを積んでバックオフに入るときだけ外す | 終わらせる処理は 3-25 の9段（待ち受けつきの `agent.prompt`）を通り**既定で最大1時間返らない。**印が無いと、次の巡回が同じ run をもう一度終わらせにかかり、`failure_state` の書き込みと引き渡しコメントが二重になる |
| **turn ループは worker の世代（`workerEpoch`）を持つ** | 起こされたときの世代を覚え、変わっていたら run を諦めずに抜ける | **1回の stall で abandon が2回走るのを防ぐ**（3-21）。巡回の stall 検知が pane を閉じた直後、まだ待ち受けにいた turn ループが同じ run を諦めると RetryCount が2倍の速さで消費される |
| **巡回のループから run を終わらせるときは goroutine へ逃がす** | `finishRunAsync` / `abandonRunAsync` / `stopAndReleaseAsync` が印だけ同期で確保し、本体を別の goroutine で回す | **設計 3-8 の「巡回のループの中で同期的に呼んではならない」を、照合と stall 検知の経路でも守るため。**コメントの確認は待ち受けつきの `agent.prompt` を通る |
| **`<task-notification>` のあとの待ち直しの長さ** | 待ち受けが返った直後は `settle_ms`、`<task-notification>` を受けたあとは `poll_wait_ms` | **後者で `settle_ms` しか待たないと、正常に続いている turn を stall と誤判定する。**設計 3-2 の「来ていなければ settle_ms 待って stall」は、待ち受けが返った直後の話である |
| **枠が明けたときの復帰の作り** | `agent.prompt` の待ちが timeout で返る → 枠待ちと判定 → `agent.wait` で待ち直す → `resets_at` を過ぎたら**turn ループの先頭へ戻して次の turn を送る** | 設計 3-27 の「この継続の指示は turn 数に数える」を、`TurnCount` を増やす唯一の場所（`beginTurn`）を通すことで満たす |
| **`runState` のフィールドを全部アクセサ越しにした** | 直接のフィールド参照を残さず、読み書きを排他で包んだ | **`-race` で実際に競合を検出した**（巡回の照合が `Issue` を書き換える一方、監視が読む）。turn ループ・巡回・hook の配送の3系統が同時に触る |
| **監視用の読み取り口（`RunningIdentifiers` / `RunViews`）** | 写しを返す読み取り専用のメソッドにした | 判断には使わない。第9段階のダッシュボードと検査のための口である |
| **`cmd/continuo` の常駐ループへ繋がない** | 第1段階のまま（設定・socket・ロックまで）にした | **設計 3-4 の起動の順序は「設定の検証 → `flock` → 3-6 の起動時検査 → 復元の段2 以降」であり、巡回はそのあとである。**復元は第7段階（[07_restore.md](07_restore.md) の受け入れの基準に「起動から復元までの順序を守る」がある）。**復元を飛ばして巡回を始めると、再起動のときに生きている pane を引き継げず、同じ worktree に Claude Code が2つ立つ。**この段階の受け入れの基準は「1件の issue が最初から最後まで通ることを、**Claude Code を起動せずに**確かめる」であり、バイナリからの実行は求めていない |
| **枠明けに `agent_status` が `working` なら継続の指示を送らない** | `afterQuotaReset` で `agent_status` を見て分岐する（`working` は送らずに hook を待つ） | **実装中に 3-27 へ Claude Code 2.1.234 の自動継続が追記された**（枠のリセット時にセッションを自動継続する。既定で有効）。送ると二重投入になり、`blocked` のときと同じ構造で投げた本文が消える |

### 引っかかった点

| 何が | どうしたか |
| --- | --- |
| **`agent.wait` は現在の状態が `until` に含まれると即返る** | 待ち直しのループを `agent.wait` の戻りだけで回すと、`idle` のまま空回りする。**hook（`Stop` と `<task-notification>`）の到着を待ち合わせの主にした** |
| **`blocked` のあとにコメントを書かせ直すと `agent.prompt` が2回になる** | 安全の要件は「**保留中の権限要求が残ったまま**次を投げないこと」である。`esc` と `pane.close` を挟んだあとの送信は別のセッションなので安全である。テストの検査もその順序で書いた |
| **偽の herdr で `worktree.remove` を成功させるだけでは `git branch -D` が通らない** | 本物の herdr と同じ結果になるよう、偽サーバに**実体の削除と `git worktree prune`** をさせた。これをしないと片付けの段4 を検証できない |
| **`testing/synctest` の中では network I/O があると時計が進まない** | 時間に依存する検査（stall の猶予・時計のリセット・バックオフの明け）だけは、**通信を1本も行わない stub** を使って bubble の中で回した（実時間ゼロ）。turn の終わりの判定と着手の13段は偽の socket サーバで検証している |

### テスト

**置き場所は [test/internal/orchestrator/](../../../test/internal/orchestrator/) と
[test/internal/tracker/by_identifier_test.go](../../../test/internal/tracker/by_identifier_test.go) である。**

| ファイル | 何を確かめるか |
| --- | --- |
| `e2e_test.go` | **1件の issue が候補に上がってから `Done` で片付くまで**／着手の13段の順番／設定ファイルの中身 |
| `dispatch_test.go` | 候補の取り方・空きスロット・印・巡回のリクエスト本数・検査の頻度・未信頼の通知・描画の失敗・段8 と段10 |
| `turn_test.go` | 空の `Stop` だけで終わりと判定しない／項目が欠けていたら判定不能／表明の促し／`max_turns`／`blocked` の `esc`／wait の掛け方 |
| `group_test.go` | グループの表明（`Ice Box` の issue も動かす）／ボードに無い対象／コメントを書かせ直す9段 |
| `stall_test.go` | **`testing/synctest` で実時間ゼロ。**猶予は1回だけ／`PreToolUse` で時計がリセットされる／バックオフの明け |
| `quota_test.go` | 枠待ちの2条件／`pause_above_percent` は新規だけ止める／`none` なら1回も叩かない／資格情報が無くても起動は続く／**枠明けに `working` なら継続の指示を送らない** |
| `prompt_choice_test.go` | **復元で引き継いだ run には継続の指示（5-4）を送る**／**再 dispatch は UUID を採り直して1回目の本文（5-3）を `.attempt` 付きで送る** |
| `terminal_test.go` | **巡回のループがコメントの確認でブロックしない**／1回の stall で abandon が2回走らない／打ち切りの前にコメントを確かめる |
| `signal_test.go` / `transcript_test.go` / `naming_test.go` | 表明の解析／transcript の区切りとトークンの重複排除／agent 名の4段（**連番の段4 を含む**）とスラグ |

### 独立した検証で出た指摘と、その扱い

**言いたいこと。**12件の指摘のうち11件を直した。**残る1件（`cmd/continuo` への結線）は
設計 3-4 の起動の順序が第7段階の復元を先に要求するため、この段階では繋がない。**
直した結果は、どれも**指摘を再現するテストを先に落としてから**確かめてある。

| 短縮名 | 何が問題だったか | どう直したか |
| --- | --- | --- |
| **復元の本文** | 引き継いだ run へ1回目の本文（5-3）を送っていた（設計 3-4 の段5c が禁じている） | 送る本文の分岐を turn 数から `runState.FreshSession` に変えた。`Adopt` は立てない |
| **再 dispatch の本文** | 会話履歴を持たない新しいセッションへ継続の指示（5-4）だけを送っていた。5-3 の `{{if .attempt}}` も到達不能だった | 同じ `FreshSession` で解決。`startRun` が立てるので、再 dispatch は 5-3 を `.attempt = RetryCount + 1` 付きで送る |
| **巡回のブロック** | 実行中の照合が `terminal_states` を見つけると、巡回のループの中でコメントの確認（待ち受けつきの `agent.prompt`）を同期的に呼んでいた | `finishRunAsync` / `abandonRunAsync` / `stopAndReleaseAsync` を足し、印だけ同期で確保して本体を goroutine へ逃がした |
| **打ち切りのコメント** | stall や時間切れで打ち切ったときに、コメントの確認を1度も走らせていなかった | `abandonRun` の「リトライを使い切った」分岐と `failRun` で、worker を止める前に走らせる。**1回も turn を送っていない run では何もしない** |
| **二重の打ち切り** | 1回の stall に対して、巡回の stall 検知と turn ループの両方が run を諦めていた | `runState.workerEpoch` と `beginTerminal` を足した。turn ループは世代が変わっていたら諦めない |
| **agent 名の直接参照** | `sendEscape` が `rs.AgentName` を排他なしで読んでいた | `rs.agentName()` に直した |
| **連番の未検証** | agent 名の段4（重複したら末尾に連番）を通すテストが無かった | `agent.list` が素の名前を返す状態で dispatch し、`continuo-koetsumugi-188-2` になることを検査する |
| **段2 の順番の未検査** | Status の書き込みが `worktree.open` より前かを比べていなかった（記録が別々の並びだった） | 偽のトラッカーと偽の herdr が**1本の並び**（`timeline`）へ積むようにし、位置を比べる |
| **UUID の未検査** | バックオフ明けにセッション UUID を採り直すことを誰も見ていなかった | 偽の socket サーバと手で進める時計を使うテストを足し、`agent.start` の `--session-id` を比べる |
| **表明の GoDoc** | `ParseSignals` の GoDoc が実装と食い違っていた（「解決できなかった行はそのまま識別子として持つ」） | 実装どおりに書き直した。**対象を解決できなかった行は、対象なしの行として扱う** |
| **インタフェースの表明** | `*tracker.Adapter` と `*herdr.Client` がインタフェースを満たすことを、コンパイル時に確かめていなかった | `var _ Tracker = (*tracker.Adapter)(nil)` と `var _ HerdrClient = (*herdr.Client)(nil)` を足した |

**`cmd/continuo` への結線は行わない。**設計 3-4 は起動の順序を
「設定の検証 → `flock` → 3-6 の起動時検査 → 復元の段2 以降」と定めており、巡回はそのあとである。
**復元は第7段階である**（[07_restore.md](07_restore.md) の受け入れの基準の1件目が
「起動から復元までの順序を守る」である）。**復元を飛ばして巡回を始めると、
再起動のときに生きている pane を引き継げず、同じ worktree に Claude Code が2つ立つ。**
この段階の受け入れの基準は「1件の issue が最初から最後まで通ることを、**Claude Code を
起動せずに**確かめる」であって、バイナリからの実行を求めていない。
