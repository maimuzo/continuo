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

- [ ] **着手の13段を、設計の順番どおりに実行する**
- [ ] **pane を新しく作らない。**`worktree.open` が作った workspace の pane を `pane.list` で引く（設計 3-16 の段8）
  - **`pane.split` も `tab.create` も呼ばない**（4-5。1 worktree = 1 workspace）
- [ ] **環境変数は設定ファイルの `env` に書く。**pane にも `agent.start` にも渡さない（設計 3-12。実測で確認済み）
- [ ] **「実行中の一覧」と「自分が取った印の集合」は同じ `map[string]*runState` 1本である**（設計 3-25）
- [ ] **バックオフが明けた run は、巡回の先頭で印の集合を走査して拾う**（設計 3-21）
  - 段0 から入り直す。段1 は飛ばす。**セッション UUID は新しく採番する**
- [ ] **stall の閾値に達したら、枠待ちの判定を先に見る**（設計 3-27 の評価順）
  - 「時計を止める」は `runState.WaitingQuota` を立てて判定を飛ばすこと。`LastSeenAt` は進めない
- [ ] **枠のトークンは `~/.claude/.credentials.json` からだけ読む。Keychain は読まない**（設計 3-15）
  - **macOS では取れないのが普通。**取れなければ枠の判定を諦め、起動は続ける
- [ ] **段2 で書き込む先は `tracker.running_state`（既定 `In Progress`）である。**ハードコードしない
- [ ] **agent 名を設計 3-3 の4段で作る**（32文字に収める。重複したら末尾に連番）
- [ ] **空きスロットの検査が、印を付ける前に走る**（段-1）
- [ ] **`Ready` と `In Progress` の両方を候補にする。**片方だけだと再起動後に取り残される
- [ ] **既に印を持っている issue を dispatch しない**
- [ ] **Status を書く前に、必ず ID 指定で取り直す。`terminal_states` に入っていたら書かない**（設計 3-4）
  - **`active_states` で絞らない。**グループの issue は `Ice Box` にあるので、絞ると1件も反映されない
- [ ] **turn の終わりを 1-3 の判定で確定する**（herdr の待ち受けが主、`<task-notification>` の検出が従）
- [ ] **turn が終わってから 0.5 秒待って transcript を読む。**見つからなければ **0.1 秒間隔で最大5回**読み直す
  - **5回で諦める**（待つ合計は 0.5 + 0.5 = 1.0 秒）。それでも無ければ「表明なし」として扱い、次の turn で促す
- [ ] **同じ1回の読み取りで、表明とトークンの両方を取る**（設計 3-15）。**2回開かない**
  - 集計は `requestId` で重複排除する。**結果は `runState` に持たず、その場でログに出す**
  - **ダッシュボード（第9段階）が使うなら、そのとき置き場所を決める**（いまは決めない）
- [ ] **表明を `promptSource == "typed"` 起点で区切って拾う。`prompt_id` では区切らない**
- [ ] **印が複数あれば、issue ごとに最後に現れたものを採る**
- [ ] **`isSidechain == false` に絞る**（subagent の発言を拾わない）
- [ ] **`max_turns` を、continuo が送った回数だけで数える**（`<task-notification>` は数えない）
- [ ] **`blocked` が返ったら、次を投げる前に `agent.send_keys` で `["esc"]` を送る**
- [ ] **worker を止めるのは `pane.close` である**（設計 3-5）。agent だけを止めるメソッドは herdr に無い
- [ ] **表明が無かった turn の次に、それを促す1文を継続の指示へ差し込む**（設計 3-8 / 3-25）
  - **hook から差し戻す仕組みは採らない**（設計 3-25）
- [ ] **stall の時計が `PreToolUse` / `PostToolUse` でリセットされる**
- [ ] **閾値を超えたら `agent_status` を1回見て、`working` なら猶予を1回だけ与える**
  - **猶予＝`LastSeenAt` を現在時刻にして、もう一度 `stall_timeout_ms` だけ待つ**（設計 3-14）
  - **与えたことを `runState` に記録し、2回目は与えない。**`working` のまま固まる場合があるため
- [ ] **枠待ちの判定が2条件の連言になっている**（`percent` が 100、かつその run から hook が来ていない）
  - **`severity` は見ない。**上限を示す値が何かを実測できていない（設計 3-27）
- [ ] **`rate_limit.source: none` なら usage API を1回も叩かない**（設定の検証は対応済み）
- [ ] **資格情報が取れなかったら、枠の判定を諦めて `none` と同じ動きにする。起動は止めない**（設計 3-27）
  - **macOS では `~/.claude/.credentials.json` が無いのが普通である**（Keychain にある）
- [ ] **`runState.PromptID` は `UserPromptSubmit` を受けた時点で入れる**（投入時には取れない。設計 3-25）
- [ ] **枠待ちの run についてだけ、stall の時計と `turn_timeout_ms` を止める**
- [ ] **枠が明けたときに送る継続の指示を、turn 数に数える**（設計 3-27）
  - **数えないと、枠待ちと復帰を繰り返す間に `max_turns` が一度も発火しない**
- [ ] **run が終わるときにコメントを確かめ、無ければ 3-25 の9段で書かせる**（毎 turn ではない）
  - **「この run が書いたもの」だけを数える**（marker があり、`runState.StartedAt` より新しいもの）
  - **worktree を再利用すると前の run のコメントが残っている**（設計 3-25）
- [ ] **権限で拒否されたことを continuo は検知しない**（設計 3-11）。エージェントが応答に書く
- [ ] **巡回1回のリクエストが3本を超えない**（候補の取得・実行中の照合・worktree の照合）
  - **Status の選択肢名の照合は毎巡回では行わない**（`tracker.verify_states_every`。既定 20 巡回に1回）
  - **`gh` の認証の検査も同じ頻度で行う**（設計 3-6 の「巡回ごとに検査するもの」）。
    **毎巡回で外部プロセスを起動しない。**失敗したらその巡回の dispatch を飛ばし、実行中の照合は止めない
- [ ] **1回目のプロンプトに issue の本文とコメントを入れない**（設計 3-8 / 3-29）
- [ ] **テンプレートの描画に `missingkey=error` を付ける。**失敗したらその issue を失敗として扱う
- [ ] **turn ループを run ごとの goroutine で動かす。**巡回のループをブロックしない（設計 3-8）
  - **`agent.prompt` を wait つきで呼ぶと turn の終わりまで返らない**（既定1時間）
- [ ] **`runState.NeedsPrompt` が立った run に turn を送る**（復元が立てる。設計 3-4 の段5c）
- [ ] **未信頼のリポジトリへのコメントは、そのリポジトリにつき1回だけ**（設計 3-6）
  - **キーは `<owner>/<repo>`。**issue ごとではない。**素朴に実装すると30秒ごとに永久に積まれる**
  - **書く先は、そのリポジトリで最初に候補に上がった issue 1件**（`Issue.NativeRef["issue_node_id"]`）
  - **draft issue はノード ID を持たないので、コメントせずログだけにする**
- [ ] **`agent.prompt` を wait つきで送る**（`until = [idle, done, blocked]` / `timeout_ms = turn_timeout_ms`）
  - **`agent.wait` を単独で使わない。**いまの状態が `until` に含まれると 0.006 秒で即返る（実測）ため、
    投入直後の `idle` を turn の終わりと取り違える
  - **timeout で返ったら枠待ちかを判定し、枠待ちなら `agent.wait` で待ち直す**（`agent.prompt` は再送しない）
- [ ] **待ち受けが返っても `Stop` が来ていなければ、`settle_ms` 待ってから stall として扱う**
- [ ] **リトライ回数とバックオフを `runState` に持つ。**バックオフ中も印に残す（拾い直されないため）

## 落とし穴（実測で分かっている）

- **`In Progress` を候補に入れ忘れると、dispatch した直後に自分の worker を殺す**
- **`background_tasks` が空でも turn は続く。**空だけで完了と判定してはいけない
- **`blocked` のまま次を投げると、保留中の権限要求が承認されて実行される**（3/3 で再現）
- **`last_assistant_message` に印は入らない**（印を書いたあと道具を呼ぶと落ちる。0/17）
- **`agent_type` が空文字の `SubagentStop` を数えると壊れる**

## 実装の記録

（着手したら書く）
