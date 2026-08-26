# 09. HTTP ダッシュボード（任意）

**言いたいこと。**run の状況を人間が見られるようにする。**任意である。**
**`server.port` が `null` なら起動しない。**

## 読むもの（設計）

| 節 | 何が書いてあるか |
| --- | --- |
| 5-2 | `server.port`（`null` なら起動しない） |
| 3-15 | **トークンの計上。**transcript の `.message.usage` を `requestId` で重複排除して集計する |
| 8-2 | 仕様 13.7 の任意拡張であること |

## 作るもの

| パッケージ | 何を |
| --- | --- |
| `internal/server` | HTTP のハンドラ。実行中の run の一覧と、それぞれの状態 |

## 受け入れの基準

- [x] **`server.port` が `null` なら listen しない**
- [x] 実行中の run の一覧を出せる（issue / Status / turn 数 / 最後に hook を受けた時刻）
- [x] **トークンの集計を出せる**（`requestId` で重複排除する）
- [x] **設定を読み直しても `server.port` は反映しない**（自前のリソースを掴んでいる。3-24）

## 実装の記録

**言いたいこと。**読むだけの窓を1つ増やした。**127.0.0.1 にしか bind せず、GET しか受けず、
ループバック以外の宛先（`Host`）は断る。**トークンは turn の終わりに集計済みの値を `runState` から
読むので、**HTTP の要求で transcript を開かない。**

### 作ったもの

| ファイル | 何を |
| --- | --- |
| [internal/server/server.go](../../../internal/server/server.go) | listen と経路と宛先の検査。`New` は `server.port` が null なら nil を返す |
| [internal/server/view.go](../../../internal/server/view.go) | `orchestrator.RunView` を表示用へ組み替える。並べ替えと件数と合計 |
| [internal/server/template.go](../../../internal/server/template.go) | `html/template` の HTML。**JavaScript は1行も使わない** |

### 実装で決めたこと

| 決めたこと | なぜ |
| --- | --- |
| 待ち受け先は `127.0.0.1` 固定（`server.LoopbackHost`）。設定から変えられない | 認証を持たない。issue の URL・worktree のパス・トークンの消費を外へ出さない |
| **`Host` がループバック（`127.0.0.1` / `localhost` / `::1`）でなければ 421 で断る** | **bind だけでは DNS rebinding を塞げない。**攻撃者のドメインを 127.0.0.1 に解決させると、そのページから見て同一オリジンになり CORS が効かない |
| 受けるのは `GET /` と `GET /api/v1/state` の2本だけ | 書き込みの経路を作らない。`net/http` の ServeMux がメソッド違いに 405 を返す |
| CSP に `frame-ancestors 'none'` を必ず書く | この指令は `default-src` に落ちてこない。書かないと他のページの iframe に埋め込める |
| トークンは `runState` に控え、ダッシュボードは写しを読む | HTTP の要求ごとに transcript（数 MB）を開くと、応答が run の I/O に引きずられる |
| ポート番号は `New` の時点で値として写し取る | 設定への参照を持たなければ、読み直しでも待ち受け先は動かない（3-24） |
| `RunView` に Title / URL / LastHookAt / StallClockAt / Tokens を足した | 受け入れの基準が要求する項目が、第6段階の `RunView` に無かった |
| **listen に失敗しても continuo は起動を続ける**（警告を出してダッシュボード無しで走る） | 任意の機能である（`SPEC.md` 13.7 の「MUST NOT become REQUIRED for orchestrator correctness」）。ここで止めると、**直前の復元で引き継いだ pane の Claude Code が誰にも見張られないまま残る** |
| **CLI の `--port` が `server.port` を上書きする** | `SPEC.md` 13.7 の「CLI `--port` overrides `server.port` when both are present」。渡されたかどうかは `flag.FlagSet.Visit` で見る（`--port=0` は「空きポートを OS に選ばせる」という意味を持つ指定である） |

### 「最後に hook を受けた時刻」と stall の時計は別に持つ

**`runState` は2つの時刻を持つ。**混ぜると、固まったエージェントを生きていると誤認する。

| 項目 | 進めるのは | 何に使うか |
| --- | --- | --- |
| `LastHookAt` | `noteHook` だけ（hook を実際に受けたとき） | **人間が生死を判断する。**ダッシュボードの「最後に hook を受けた時刻」はこれ |
| `LastSeenAt` | hook のほか、turn を送った・枠待ちを外した・画面の版が増えていたのを見たとき | 打ち切りの判定（3-21）。JSON では `stall_clock_at` として出す |

**混ぜていると何が起きるか。**hook を1件も返さずに固まったエージェントでも、continuo が次の turn を
送った瞬間に時計が進み、ダッシュボードは「0秒前」と表示する。

### トークンの置き場所（3-15 が保留していた点）

**`runState.Tokens` に持つ。**`requestId` で重複排除した**この run の累計**である。
`readSignals` が turn の終わりに transcript を1回読むとき、表明と同じ結果から書き込む（2回開かない）。
**走行中の turn の分はまだ入っていない。**ダッシュボードは「いつ時点の値か」（`tokens_at`）を必ず併記する。

**セッションをまたいだら足す。**transcript のファイル名はセッション UUID なので、再 dispatch で
UUID を採り直すと集計の対象ファイルが別物になる。**`beginAttempt` がそれまでの累計を
`tokensBase` へ畳み込み、`setTokens` がそこへ足す。**足さないと、stall で再 dispatch された run の
累計が巻き戻り、使ったトークンを実際より少なく見せる。

### JSON の形（`SPEC.md` 13.7.2 との差）

**経路は仕様どおり `GET /api/v1/state` である**（`server.APIStatePath`）。
**応答の形は仕様の suggested shape と違う。**仕様が例示するのは提案であって要求ではない。

| 仕様の例 | continuo | なぜ |
| --- | --- | --- |
| `running` と `retrying` の2本の配列 | `runs` の1本 ＋ `counts` | **印の集合が1本しか無い**（設計 3-25）。バックオフ中の run も同じ集合に残る。どちらかは `backoff_until` で分かる |
| `tokens` は `input_tokens` / `output_tokens` / `total_tokens` | `api_calls` / `input` / `cache_creation` / `cache_read` / `output` / `total` | Claude Code の `usage` はキャッシュの読み書きを別に持つ（設計 3-15 の実測）。合算すると枠の使い方が読めなくなる |

**`GET /api/v1/<issue_identifier>` は作らない。**仕様でも最低限の要求ではなく、
run ごとの項目は `/api/v1/state` が全部返している。**識別子に `/` と `#` を含む**ので、
経路に埋めると引用の規則を1つ増やすことになる。

### 応答の実物

```json
{"identifier":"maimuzo/continuo#12","state":"In Progress","turn_count":3,
 "last_hook_at":"2026-08-19T11:58:30Z","last_hook_ago":"1分30秒前",
 "stall_clock_at":"2026-08-19T11:59:59Z",
 "tokens":{"api_calls":19,"input":38,"cache_creation":14358,"cache_read":701185,"output":1216,"total":716797},
 "tokens_at":"2026-08-19T11:58:40Z"}
```

### テスト

| 置き場所 | 何を確かめるか |
| --- | --- |
| [test/internal/server/](../../../test/internal/server/) | 23件。ループバック以外から接続できないこと（この機材のループバックでない IPv4 へ実際に接続する）、ループバック以外の宛先を 421 で断ること、GET 以外が 405 になること、タイトルと URL のエスケープ |
| [test/internal/orchestrator/dashboard_test.go](../../../test/internal/orchestrator/dashboard_test.go) | 3件。**本物の供給経路**（transcript を読む → 集計する → `runState` → `RunViews`）が繋がっていること、再 dispatch で累計が巻き戻らないこと、`LastHookAt` が hook でしか進まないこと |
| [test/internal/daemon/daemon_test.go](../../../test/internal/daemon/daemon_test.go) | 2件。ダッシュボードが開けなくても巡回まで進むこと、`--port` で開いた口に引き継いだ run が出ること |

**偽の供給元に値を直接入れるテストだけにしない。**`internal/orchestrator/lifecycle.go` の
集計をまるごと落としても全部通ってしまい、配線が切れたことに気づけないためである
（実際に落として、3件のうち2件が落ちることを確かめてある）。
