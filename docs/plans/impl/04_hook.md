<!-- 目的: hook の受け口と continuo hook サブコマンドを実装するタスク -->

# 04. hook の受け口

**言いたいこと。**Claude Code の hook を Unix socket で受け、**turn の終わりを判定できるようにする。**
**`continuo hook` は転送して即終了する。**応答を待たない（差し戻しは採らない。設計 3-25）。

## 読むもの（設計）

| 節 | 何が書いてあるか |
| --- | --- |
| [1-3](../continuo_design.md) | **turn の終わりをどう判定するか。**なぜ hook 単独でも静止の長さでも判定できないか |
| 3-2 | **張る hook 7種と役目の表。**判定の規則。`continuo hook` の振る舞い |
| 3-12 | issue ごとの設定ファイルを worktree の外に置く理由と、書き込む先 |
| 3-14 | turn の数え方（continuo が送った回数だけ数える） |
| 3-19 | **落ちている間に届かなかった通知の取り戻し。**逃がし先のパスと形式 |
| 3-21 | stall の時計を中間の hook でリセットする |
| 3-23 | **socket の置き場所の探索順と、パスの長さの上限** |

## 作るもの

| パッケージ | 何を |
| --- | --- |
| `internal/hookserver` | Unix socket を listen し、届いた JSON を解釈して orchestrator へ渡す |
| `internal/hookserver` | 逃がし先のファイルを起動時に読み込む（3-19） |
| `internal/hookclient` | 標準入力を socket へ送る実体。**`cmd/continuo` はこれを呼ぶだけにする**（`package main` の非公開関数は `test/` から呼べない） |

**`continuo hook` が受け取る引数は2つである**（設計 3-2）。**設定を読まない。cwd は worktree なので届かない。**

| 引数 | 何が入るか |
| --- | --- |
| `--socket` | hook を受ける socket の絶対パス |
| `--pending-dir` | socket へ繋がらなかったときの逃がし先。`<実行時ディレクトリ>/issues/<スラグ>/pending` |

**この2つは continuo が設定ファイルを書くとき（3-16 の段5）に絶対パスへ展開して埋め込む。**

**socket 上の形式。改行区切り JSON。1コネクション1メッセージ。応答は返さない。**

```text
{"hook_event_name":"Stop","session_id":"…",…}\n   ← hook が書く
→ 書いたら閉じる。サーバは何も返さない
```

**`continuo hook` は判定しない。**差し戻しは採らないので（設計 3-25）、応答を待つ必要が無い。

#### hookserver と orchestrator の境界

| どちらが持つか | 何を |
| --- | --- |
| **hookserver** | socket の listen / JSON の解釈 / 逃がし先の読み戻し |
| **orchestrator** | `runState` の全部（`LastSeenAt` も `settle_ms` のタイマーも） |

**hookserver は判断しない。**受け取った `HookEvent` を、次の1つのメソッドで渡すだけにする。

```go
// HookSink は hookserver が hook を届ける先である。orchestrator が実装する。
type HookSink interface {
    // OnHook は hook を1件受け取る。知らない session_id なら false を返す（hookserver はログに出す）。
    OnHook(ev HookEvent) (accepted bool)
}
```

**`session_id` から run を引く索引は orchestrator が持つ。**`map[string]*runState` のキーは
project item の ID なので（設計 3-4）、**`session_id → itemID` の索引を別に持つ。**

**hook の JSON を受ける型。**イベントごとに項目が違うので、共通部分＋任意項目にする。

```go
// HookEvent は Claude Code の hook から届く JSON である（設計 1-4）。
// イベントによって入る項目が違うので、共通のものだけを必須にする。
type HookEvent struct {
    HookEventName  string          `json:"hook_event_name"`
    SessionID      string          `json:"session_id"`
    TranscriptPath string          `json:"transcript_path"`
    Cwd            string          `json:"cwd"`
    PromptID       string          `json:"prompt_id"`
    // Stop / SubagentStop にだけ入る。
    // 「項目が欠けている」と「空配列」を区別するためポインタにする（設計 3-2）。
    BackgroundTasks *[]BackgroundTask `json:"background_tasks"`
    StopHookActive  bool              `json:"stop_hook_active"`
    // UserPromptSubmit にだけ入る。<task-notification> の判定に使う。
    Prompt string `json:"prompt"`
    // Notification にだけ入る。permission_prompt を拾う（設計 3-11）。
    NotificationType string `json:"notification_type"`
    // SubagentStart / SubagentStop にだけ入る。
    AgentID   string `json:"agent_id"`
    AgentType string `json:"agent_type"`
}

// BackgroundTask は Stop / SubagentStop の background_tasks の要素である。
// 項目は docs/evidence/hooks_probe_20260817.jsonl の実測に合わせる。
// 実測で出た形は2種類:
//   {"id":"a1f9f743842d397e1","type":"subagent","status":"running","description":"ディレクトリ調査","agent_type":"Explore"}
//   {"id":"bmr1ksf9i","type":"shell","status":"running","description":"45秒スリープをバックグラウンド実行","command":"sleep 45"}
type BackgroundTask struct {
    ID          string `json:"id"`
    Type        string `json:"type"`        // "subagent" / "shell"
    Status      string `json:"status"`      // "running" など
    Description string `json:"description"`
    AgentType   string `json:"agent_type"`  // type == "subagent" のときだけ入る
    Command     string `json:"command"`     // type == "shell" のときだけ入る
}
```

> **判断に使うのは「空かどうか」だけである**（設計 1-3）。
> **項目を全部持つのは、記録とダッシュボード表示のためである。**
> 知らない項目が増えても落ちないよう、**未知のキーは無視する**（`encoding/json` の既定の挙動）。

## 受け入れの基準

**すべてテストで確かめる。Claude Code は起動しない。**

> **socket の置き場所・103 バイトの上限・`0700` は、第1段階で実装済みである**
> （`internal/socketpath`）。**このタスクでは作り直さず、それを使う。**
- [ ] `session_id` で run に対応づけられる。**引数に run の識別子を書かない**
- [ ] **listen は復元の段5d で始める。ただし配送は段6b まで待つ**（設計 3-4）
  - **溜める → 逃がし先を読み戻してキューの先頭へ積む → 索引ができたら流す、の順**
  - **listen を後回しにすると、読み戻しから listen までの窓に落ちた hook を誰も読まない**
  - 公開するのは `Start`（listen して溜める）/ `ReplayPending`（逃がし先を積む）/ `StartDelivery`（流し始める）/ `Close` の4つ
- [ ] **知らない `session_id` が届いたら、警告をログに出して捨てる。**落ちない
- [ ] **`continuo hook` は転送して即終了する。**応答を待たない（設計 3-2）
- [ ] **`background_tasks` の「欠けている」「空配列」「非空」を区別できる**
- [ ] **どのイベントも捨てずに `HookSink.OnHook` へ渡す**（`agent_type` が空文字の `SubagentStop` も含む）
  - **hookserver は判断しない。**捨てるかどうかは orchestrator が決める（第6段階）
- [ ] **socket へ改行区切りの JSON を1行書いて閉じる。**応答を読まない
- [ ] **標準入力が JSON として解釈できなければ、どこにも書かずに `exit 0` する**（設計 3-19）
  - 標準エラーへ理由を出す。**socket へも逃がし先へも書かない**（逃がし先の名前が決まらないため）
- [ ] **socket へ繋がらなくても `exit 0` する**（continuo が落ちていてもエージェントを止めない）
- [ ] **socket へ接続できないときに逃がし先へ書ける**（3-19 のパスと形式）
- [ ] **逃がし先へは `.json.tmp` で書き切ってから `os.Rename` する**（設計 3-19）
  - **書き込みの途中が読まれないようにするため。**読まれると `Stop` を1件失い、その run は `stall_timeout_ms` まで気づかれない
- [ ] **逃がし先を受信時刻順に読み、読んだファイルを消す**
  - **走査するのは `*.json` にだけ一致するものである。`.tmp` は必ず飛ばす**（書き込み中である）
  - **壊れた JSON は `pending/broken/` へ移す。**対象は rename 済みの `.json` だけ
  - **取り残された `.tmp` は起動時に消し、消したことをログに残す**（書いている途中で落ちた残骸）
  - **走らせるのは復元の段5e**（設計 3-4）。listen（段5d）のあと、配送（段6b）の前である
  - 走査するのは `<実行時ディレクトリ>/issues/*/pending/` の全部
  - **`<実行時ディレクトリ>` は `filepath.Dir(解決済みの socket のパス)` である**（設計 3-23）
    - 第1段階の `socketpath.ResolveHookSocketPath` で解決したものを使う。**自分で決め直さない**

## 落とし穴（実測で分かっている）

- **`background_tasks` が空でも turn は続く。**空の `Stop` 20件のうち4件が途中だった
- **途中の `Stop` の 0.033〜0.037 秒後に `<task-notification>` が来る**（8件）。最終 `Stop` の後に来るのは `SubagentStop`（1.9〜2.9秒後）
- **`agent_type` が空文字の `SubagentStop` は、対応する `SubagentStart` を持たない**（0/22、0/44）。
  **turn の終わりの判定に使わない。**数える必要も無い（数えた結果を使う判断が設計に無い）
- **Go が作る socket の権限は umask 次第で `0755` になる。**ディレクトリの権限で守る

## 実装の記録

（着手したら書く）
