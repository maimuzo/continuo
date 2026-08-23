<!-- 目的: 再起動時に run を復元し、取り残しと二重起動を防ぐタスク -->

# 07. 再起動時の復元

**言いたいこと。**状態はメモリにしか持たないので、**再起動したらディスクとボードと herdr から組み立て直す。**
**引き継いだ run を「自分が取った」印の集合へ入れ直すのが急所である。**忘れると同じ worktree に Claude Code が2つ立つ。

## 読むもの（設計）

| 節 | 何が書いてあるか |
| --- | --- |
| 3-4 | **復元の手順9段と、取り直した Status ごとの扱い** |
| 3-18 | 身元ファイル（復元の主キー） |
| 3-19 | **落ちている間に届かなかった通知の取り戻し。**再起動直後にボードを1回取り直す |
| 3-3 | pane の label / cwd / セッション UUID の3本立て |
| 3-17 | `flock` による二重起動の防止 |
| 3-9 | 起動時の掃除（孤児 branch を消す）。**復元のあとに走らせる** |

## 作るもの

| パッケージ | 何を |
| --- | --- |
| `internal/orchestrator` | 復元の手順9段 |
| `internal/workspace` | 置き場所の走査と身元ファイルの読み込み（第5段階の再利用） |
| **`cmd/continuo`** | **常駐ループへの結線。**ここで初めて、ビルドしたバイナリから1件の issue が通る |

**結線の順序を守る**（設計 3-4 の「起動から復元までの順序」）。

| 順 | 何をするか | 落ちたらどうなるか |
| --- | --- | --- |
| 1 | 設定を読んで検証する | 起動を止める。**pane には触らない**（まだ何も発見していない） |
| 2 | `flock` を取る | 二重起動なので即座に終了する |
| 3 | 3-6 の起動時検査を全部通す | **起動を止める。生きている pane は閉じずに放置する** |
| 4 | 復元の手順（段2〜段9） | 段ごとの規則に従う |
| 5 | 巡回を始める（`Tick` を `poll_interval_ms` ごとに回す） | — |

**第6段階は結線を見送っている。**復元より先に巡回を始めると、
**これから引き継ぐ run の worktree に2つ目の Claude Code が立つ**ためである。
**この段階で繋ぐ。**

**終了の作法。**`SIGINT` / `SIGTERM` を受けたら、
**巡回を止め、hook の受け口を閉じ、走行中の turn ループの終了を待ってから抜ける。**
**pane は閉じない**（次の起動で引き継ぐ。3-4 の段5）。

## 受け入れの基準

**「どの段で落としても取り残されない」ことを、段ごとにテストで確かめる。**

- [x] **`continuo` を起動すると、復元を終えてから巡回が始まる**（`cmd/continuo`）
  - **ビルドしたバイナリから1件の issue が通ることを、テスト用socket mockで確かめる**
  - **`SIGINT` / `SIGTERM` で、巡回を止め・hook の受け口を閉じ・turn ループの終了を待ってから抜ける**
  - **終了時に pane を閉じない**（次の起動で引き継ぐ）
- [x] **起動から復元までの順序を守る**（設計 3-4）。**設定の検証 → `flock` → 3-6 の起動時検査 → 段2 以降**
  - **3-6 の検査に落ちて起動を止めるとき、生きている pane は閉じずに放置する**
  - **設定の誤りで、動いているエージェントの作業を殺さないため。**人間が直して起動し直せば段5 で引き継げる
- [x] **`flock` が取れなければ即座に終了する**
- [x] **置き場所を固定の4階層で走査する**（設計 3-4 の段2）。壊れた身元ファイルは無視してログに出す
- [x] **project item の ID が重複したら、`created_at` が新しいほうを採る**（設計 3-4 の段2）
  - **段2 で決めるのは「どちらを採るか」だけである。**採らなかったほうのパスを「捨てた身元」として覚える
  - **pane を閉じるのは段4 である**（pane の一覧を取るのは段4。段2 では誰が生きているか知らない）
  - **段4 で、捨てた身元の worktree に pane が付いていたら閉じる**（同じ issue に2つの Claude Code が居る）
  - 採らなかったほうの worktree は消さずに残す。**どちらに成果があるか判断できない**
  - **次の巡回では 3-9 の手順7 に乗る**（Status を取り直して `cleanup.on_states` なら片付ける）
- [x] **listen は段5d、逃がし先の読み戻しは段5e、配送の開始は段6b**（設計 3-4）
  - **この順でないと、読み戻しと listen のあいだの窓に落ちた hook を誰も読まない**
  - [x] **段5e で逃がし先を2回走査する。**2回目が拾うのは
        **「1回目の走査を始めてから配送を始めるまでの間に `continuo hook` が書いたもの」**である
    - **走査は `*.json` だけを対象にする。`.tmp` は飛ばす**（書き込み中である。設計 3-19）
  - 段6 で索引ができるまで、届いた hook は溜めるだけにする
- [x] **project item の ID でまとめて取り直す**（1リクエスト）
- [x] **pane の cwd と worktree のパスを、両方 `filepath.EvalSymlinks` で解決してから比較する**（設計 3-4）
- [x] **Status の表は段8（pane が無い run）専用である。**pane が生きている run は段5〜7 で扱う
- [x] **孤児 branch の接頭辞は `branch_template` の先頭から最初の `{{` の直前まで**（設計 3-9 の手順6b）
- [x] **`agent_session` からセッション UUID を取り、hook の対応づけを復元できる**
- [x] **`agent.list` から pane_id に対応する agent 名を引き、`runState.AgentName` に入れる**
  - **`agent.prompt` / `agent.wait` の宛先は agent 名である。**pane ID では送れない
  - **agent 名が無い pane は段8b で扱う**（pane を閉じ、worktree と Status を残す）
- [x] **復元の手順の中で `agent.prompt` を呼ばない**（設計 3-4 の段5c）
  - **wait つきの呼び出しは turn の終わりまで返らない**（既定1時間）。**復元がそこで止まる**
  - 代わりに **`runState.NeedsPrompt` を立てる。**巡回の turn ループが非同期に送る（設計 3-8）
  - **前の turn が途中でも諦める。**turn 数は 1 から数え直すと決めている
- [x] **引き継ぐ前に herdr の `agent_status` を見る**（設計 3-4 の段5a2）。**Status だけで決めない**
  - **`blocked`** → **引き継がない。`failure_state` へ落として pane を閉じる。**worktree は残す
    - **これを飛ばすと、次の turn を送ったときに保留中の権限要求が承認されて実行される**（3-11 で実測。3/3）
  - **`working`** → **引き継ぐが `NeedsPrompt` を立てない。**hook を待ち、来なければ stall 検知で拾う
  - **`idle` / `done`** → そのまま引き継ぐ
  - **取れない / 知らない値** → pane を閉じ、worktree と Status を残す（段8b と同じ）
- [x] **引き継いだ回数の上限は、`NeedsPrompt` を立てる前に見る**（設計 3-4 の段5b）
  - 上限に達していたら `failure_state` へ落として pane を閉じる。**無駄な turn を1回も送らない**
- [x] **`runState.PromptID` は空にする。**空のときは `prompt_id` の照合を行わない
- [x] **`runState.LastSeenAt` に引き継いだ時刻を入れる**（設計 3-4）。ゼロ値のままだと即座に stall と判定される
- [x] **引き継いだ issue を、実行中の一覧と印の集合の両方へ入れ直す**
- [x] **引き継いだ回数を増やして身元ファイルへ書き戻す。**上限に達していれば `failure_state` へ
- [x] **turn 数は 1 から数え直す**（復元できないことを受け入れる。引き継いだ回数で打ち切る）
- [x] 身元ファイルがあるのに pane が無い → **Status を取り直してから扱いを決める**
- [x] pane があるのに身元ファイルが無い → **閉じずにログへ残す**（continuo のものと断定できない）
- [x] **`In Review` / `Blocked` の run は、pane も worktree も残して何もしない。Status を巻き戻さない**
- [x] **`active_states` のときだけ `restart.orphan_running_action` の3値で分岐する**（設計 3-4）
  - **`redispatch`（既定）は復元の中では何もしない。**印にも入れず、次の巡回に委ねる
  - **復元の中で dispatch すると、着手の段11 の待ちで最大1時間止まる**
- [x] **`NeedsPrompt` で送るのは継続の指示（5-4）。1回目の本文（5-3）ではない**（設計 3-4）
  - セッションは引き継いでいるので、エージェントは issue の URL も作法も知っている
- [x] **pane が生きている run の片付けの条件は `cleanup.on_states`**（`terminal_states` ではない。設計 3-4）
- [x] **取り直しで見つからなかった issue は、pane も worktree も残し、ログに出して印から外す**（勝手に消さない）
- [x] **引き継げなかった run の pane は必ず閉じる**（設計 3-4）。worktree と Status は残す
  - **巡回には「生きている pane を引き継ぐ」経路が無い**（設計 3-16）。残すと2つ目が立つ
- [x] **同じ worktree を cwd に持つ pane が2つあったら、1つだけ引き継いで残りを閉じる**（設計 3-4 の段4）
  - **後勝ちの写像に入れてはならない。**上書きされた pane は引き継がれも閉じられも記録もされず、
    **「同じ worktree に Claude Code が2つ」がそのまま残る**（段2 の「同じ issue の worktree が2つ」の対称形）
  - **残すほうは pane の ID の昇順で決める。**`pane.list` が返す順に結果を依存させない
- [x] **`agent.list` を取れなかったときは pane を1つも閉じない**（`pane.list` の失敗と対称にする）
  - agent 名を引けないまま先へ進むと段8b が全件で真になり、**herdr の一時的な失敗1回で
    走っている全部のエージェントの作業を捨てる**
  - **全件を段8 へ流して次の巡回に委ねる**
- [x] **取り直しそのものに失敗しても起動を続ける。**その run の pane は閉じる
- [x] **引き継ぎ回数の判定は、取り直した Status が `active_states` のときだけ行う**（設計 3-4 の段7）
- [x] **回数を増やすのは、引き継いだときと再 dispatch したときの両方**
- [x] **上限に達した run は、pane を閉じ worktree を残して `failure_state` へ落とす**
- [x] **pane はあるが agent 名が無い run は、pane を閉じて worktree と Status を残す**（設計 3-4 の段8b）
- [x] **pane が生きている run を引き継ぐかどうかを、Status ごとの表で決める**（設計 3-4 の段5a）
  - `active_states` → 引き継ぐ / **`cleanup.on_states`** → pane を閉じて片付ける
  - **引き渡し（`In Review` / `Blocked`）と、見つからなかったもの → pane も worktree も残す**（8-1）
  - **`terminal_states` ではない。**既定値はどちらも `["Done"]` だが別のキーである（`internal/config`）
  - **テストでは `cleanup.on_states` と `terminal_states` に別の値を入れた設定で確かめる。**
    既定値のままだと、どちらを見ていても同じ結果になり、取り違えを検出できない
- [x] **`In Review` / `Blocked` の run は、印にも実行中の一覧にも入れない**（設計 3-4）
- [x] **巡回の worktree の照合で、印に無い worktree に生きた pane があれば、
      Status が `active_states` に戻ったときに閉じてから dispatch する**（設計 3-9 の手順7b）
  - **`active_states` の条件を外してはならない。**復元が「pane も worktree も残す」と決めた
    `In Review` / `Blocked` の run は印に入っていないので、条件なしに閉じると
    **復元の直後の巡回が、人間のレビュー待ちで正常に止まっている Claude Code を毎巡回で落とす**
  - **`cleanup.on_states` のときはここで pane を閉じない。**`worktree.remove` が workspace ごと
    閉じる（設計 3-9 の手順3）
- [x] **起動時の掃除は、復元が終わったあとに走る**（先に走らせると、これから引き継ぐ branch を孤児と判定して消す）
- [x] **孤児 branch の掃除は `internal/workspace` に置く**（設計 3-9 の手順6b）
  - **対象は、走査で見つかった worktree が属するリポジトリだけ**（ボードを読まずに決まる）
  - **「実行中の issue も無い」の判定には、復元後の印の集合を使う**
- [x] **socket のパスが前回と違っていたら引き継がない**（設計 3-23）
  - **pane を閉じ、worktree と Status は残す。**次の巡回で再 dispatch される
  - **両方のパスをログに出す。**運用の環境が変わったことに人間が気づけるようにする

**落ちた段ごとの期待。**

| どこで落ちたか | 再起動後にどうなるか |
| --- | --- |
| 段-1〜1 | 何も残らない。`Ready` のまま次の巡回で拾う |
| 段2 | Status だけ残る。`In Progress` は候補に上がるので拾える |
| 段3〜5 | 作りかけの worktree が残る。再利用して作り直す |
| 段6以降 | **身元ファイルがある。**pane が生きていれば引き継ぎ、無ければ次の巡回で拾う |

## 実装の記録

**言いたいこと。**復元の9段を `internal/orchestrator/restore.go` に置き、
**常駐ループの結線は `internal/daemon` に新しく置いた。**`cmd/continuo` はそれを呼ぶだけである。
**ビルドしたバイナリを起動して、復元 → 巡回 → 1件の issue の片付け → `SIGTERM` での終了までを
`test/internal/daemon` が実際に通している。**

### 作ったもの

| ファイル | 何を |
| --- | --- |
| [internal/orchestrator/restore.go](../../../internal/orchestrator/restore.go) | 復元の段2〜段9。`Restore(ctx, HookServer)` |
| [internal/orchestrator/sweep.go](../../../internal/orchestrator/sweep.go) | 起動時の掃除（3-9 の手順6 / 6b）。`SweepOnStartup(ctx, *RestoreResult)` |
| [internal/workspace/sweep.go](../../../internal/workspace/sweep.go) | 孤児 branch の掃除。`SweepOrphanBranches` |
| [internal/tracker/ghstatus.go](../../../internal/tracker/ghstatus.go) | `gh` の有無と `gh auth status` の scope の検査（3-6） |
| [internal/daemon/daemon.go](../../../internal/daemon/daemon.go) | 起動の順序と終了の作法 |
| [internal/daemon/checks.go](../../../internal/daemon/checks.go) | 3-6 の起動時検査 |

### 実装者が決めたこと

| 決めたこと | なぜ |
| --- | --- |
| **結線の実体を `internal/daemon` に置いた** | `package main` の非公開関数は `test/` から呼べない（[tasks.md](tasks.md) の「テストの置き場所」）。`cmd/continuo` に置くと、起動の順序をテストで確かめられない |
| **`CONTINUO_GITHUB_GRAPHQL_ENDPOINT` で GraphQL の接続先を差し替えられるようにした** | これが無いと、**ビルドしたバイナリを本番のボードへ繋がずに動かす手段が1つも無い。**設定ファイルには置かない（設計 5-2 に無いキーを足さない）。設計 3-23 に記録した |
| **セッション UUID を pane からも身元ファイルからも取れないときは引き継がない** | hook はどの run のものかを `session_id` でしか名乗らない（3-2）。対応づけを復元できない run を引き継ぐと、`claude.turn_timeout_ms`（既定1時間）まで誰も気づかない。段8b と同じ扱い（pane を閉じ、worktree と Status を残す）にした |
| **段9 のログは、置き場所の内側にある pane に絞った** | `pane.list` は continuo と無関係な pane も返す。全部ログに出すと、人間が見るべき1件が埋もれる |
| **引き継いだ回数の書き戻しに失敗しても引き継ぎは続ける** | pane は生きている。ここで閉じると、書き込みの失敗だけを理由に作業の成果を捨てることになる |
| **同じ worktree に pane が2つあったら pane の ID が小さいほうを引き継ぐ** | どちらが新しいかを判断する材料が `pane.list` に無い（`created_at` を返さない）。**順序に依らず結果が決まることのほうが重要である。**残りは段4 で閉じる |
| **`agent.list` の失敗は `pane.list` の失敗と同じ扱いにした** | 先へ進むと段8b の「pane はあるが agent 名が無い」が全件で真になり、**herdr の一時的な失敗1回で走っている全部のエージェントの作業を捨てる。**空の写像を返して全件を段8 へ流せば、pane を1つも閉じずに次の巡回へ渡せる |
| **片付けに渡す base は空にした** | 身元ファイルは base を持たない（3-18）。`cleanup.require_pushed` が真で upstream が無ければ「判定できない」として見送られる（**消さない側に倒れる**） |

### 引っかかった点

| 何が起きたか | どう直したか |
| --- | --- |
| **引き継いだ直後の巡回が、引き継いだ run を即座に止めた** | `Issue.Dispatchable` が偽（リポジトリが未信頼）だと、実行中の照合が「作業中でも完了でもない」と判定する。テストの側で `~/.claude.json` と `ghq` のmockを用意した。**本番でも、信頼登録が外れると引き継いだ run が止まる**（設計どおりの挙動である） |
| `-race` がテスト用herdr mock の台本とテスト本体の競合を報告した | 台本が書きテスト本体が読む値を、排他つきの入れ物に変えた |
