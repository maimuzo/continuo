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

**セッションをまたいだら足す。**transcript のファイル名はセッション UUID なので、UUID を
採り直すと集計の対象ファイルが別物になる。**`foldTokensBase` がそれまでの累計を `tokensBase` へ
畳み込み、`setTokens` がそこへ足す。**足さないと累計が巻き戻り、使ったトークンを実際より少なく見せる。
**`--resume` で復帰したときは畳み込まない**（設計 3-3b）。同じファイルなので、畳み込むと2倍に見せる。

### run をまたぐ累計（issue #238。ここが正である）

**言いたいこと。**`runState.Tokens` は run が終わると消える。**画面の合計はそれを足していたので、
長い turn が並んでいる間ほぼ常に0だった。**run が消えても残る累計を、orchestrator が持つ。

**仕様の要求。**`SPEC.md` 4.1.8 の `codex_totals` と、13.5 の
"Accumulate aggregate totals in orchestrator state."（**訳:** 累計は orchestrator の状態に溜めること）。

#### 何を持つか

| 何 | 中身 |
| --- | --- |
| `Orchestrator.tokenTotals` | **run をまたぐ累計。**`TokenTotals()` が写しを返す |
| `Orchestrator.tokenLedger` | **issue の識別子 → 最後に計上した「セッション UUID と、そこまで計上した合計」。**差分を取る相手である |

**どちらも `o.mu` が守る。**map は `New` で作る（作らないと最初の turn で panic する）。

#### 差分で足す

**`ReadTranscript` が返すのは「その transcript 1ファイルの絶対値」であり、
追記だけで伸びるかぎり turn ごとに単調に増える**（書き換えが起きないことは測っていない）。
**そのまま毎回足すと、10 turn 回った run は10回ぶん足される。**
`SPEC.md` 13.5 が
"For absolute totals, track deltas relative to last reported totals to avoid double-counting."
（**訳:** 絶対値の合計を扱うときは、**二重計上を避けるため、最後に報告した合計との差分を追うこと**）
と名指ししている。

**`addTokenUsage` が、台帳のセッション UUID と一致するときだけ差分を取る。**
`TokenUsage.Sub` は項目ごとに引き、**負になったら0へ丸めて、丸めたことを知らせる。**
**知らせないと、累計が静かに実際より小さくなったことをあとから確かめられない**（`logTokens` が WARN を出す）。

| 経路 | セッション UUID | 何が足されるか |
| --- | --- | --- |
| 同じセッションで turn を重ねる | 同じ | 差分だけ |
| 再 dispatch でセッションを採り直す | 変わる | **新しい transcript の全部**（前のセッションの分は既に入っている） |
| 引き渡し → `release` → 同じセッションへ復帰 | 同じ | 差分だけ（**`release` では台帳を消さない**） |
| `Adopt`（再起動で引き継ぐ） | 台帳が空 | **その transcript の全部**（起動より前に書かれた分も入る） |

**丸めが起きたときは、台帳へ小さいほうを書かない。**項目ごとに大きいほうを残す。
**小さいほうを書くと、次にファイルが伸びたときに丸めたぶんをもう一度足し、
累計が transcript の絶対値を超える**（100 → 95 → 150 と読んだら 155 になる）。

#### 鍵は issue の識別子である。project item の ID ではない

**item の ID にすると、ボードから外して載せ直したときに鍵が変わりうる。**
continuo 自身が「続きを進めたいならカンバンへ戻してください。worktree は残してあります」と
案内しているので、**その操作は起きる。**鍵が変われば台帳の項目が孤児になり、同じ transcript が
最初から全部足される。**識別子なら載せ直しでも変わらないので、変わるかどうかを測らずに済む。**

**draft issue の識別子は `"draft:" + project item の ID` である**（`internal/tracker/query.go`）。
**そこだけは item の ID に戻るが、draft issue は `dispatchable=false` で `runState` を作らないので、
台帳へ入らない。**

#### 何が「同じ transcript か」を、セッション UUID で決める

**transcript のファイル名はセッション UUID であり、`--resume` で復帰しても同じファイルのままである**
（設計 3-3b の実測）。**だから「セッション UUID が同じ」と「同じ transcript」は一致する。**

**hook が名乗る `transcript_path` を鍵にしてはならない。**
あれは**エージェントが書き換えられる外部入力**である
（`internal/orchestrator/hookinput.go` が「hook の中身はエージェントが書き換えられる外部入力である」と書いている）。
`acceptTranscriptPath` が確かめるのは4つ（絶対パス・実在・許された置き場所の内側・通常のファイル）だけで、
**セッション UUID との突き合わせは1行も無い。**既定の置き場所はエージェントが書けるので、
**毎 turn 違うファイル名を名乗るだけで、同じ中身を何度でも新しい鍵として全額計上させられる。**

**セッション UUID は continuo が決める値である。**`newSessionUUID` で採るか、pane から読む。
**hook の本文からは来ない。**

**パスは、ログに出すためだけに `logTokens` へ渡す。**

#### いつ台帳を消すか

**`release` では消さない。**引き渡しのあと同じセッションへ `--resume` で復帰する経路があり、
消すと二重に数える。

**`cleanupWorktree` が worktree を実際に消したときだけ消す**（`forgetTokenLedger`）。
worktree が無ければ復帰する道が無いためである。**`cleanupPath` が false（見送り・失敗）なら消さない。**

**`cleanupPath` を呼ぶ場所は3つある**（`cleanupWorktree` / `SweepOnStartup` / 復元の `cleanupInto`）。
**台帳を落としているのは1つ目だけである。**後ろ2つは**起動時にしか走らず、その時点で台帳は空だからである。**
**巡回の最中に走る片付けを新しく足すなら、そこでも `forgetTokenLedger` を呼ぶこと。**

**巡回中に worktree を消す経路は、`cleanupPath` の外にもう1本ある**（`reconcileWorktrees` が
`ws.Cleanup` を直に叩く）。**そこも台帳を落としていないが、額は変わらない。**
worktree が消えれば身元ファイルも消え、次の着手は必ず新しいセッションを採番するためである。
**失うのは台帳の項目1件ぶんのメモリだけである。**

#### 落とす側は、テストで押さえていない

**押さえてあるのは「落とさない」側の2つだけである**（引き渡しのあとの復帰と、片付けを見送ったあとの復帰）。
**`forgetTokenLedger` の呼び出しを丸ごと消しても、その2本は落ちない。**
上と同じ理由で、**落としても落とさなくても、次に足される額は同じだからである。**
**残るのはメモリの話だけなので、そこは検査していない。**

**上限は無い。**worktree を消さずに手放した issue（引き渡し・失敗）の項目は残る。
1件あたりは識別子とセッション UUID の文字列2本と int 5本である。

#### 書き込みは1つの区間でまとめ、読む順序だけを決める

**書く側**（`addTokenUsage`）**は、累計と run ごとの値を1つの `o.mu` の区間で書く。**
**分けて書くと、その隙間にダッシュボードが両方を読み切ったときに
「累計が走行中の run の合計より小さい」写しができる。**
**1つにまとめれば、どの瞬間を切り取っても累計のほうが大きいか等しい。**
`rs.setTokens` は中で `rs.mu` を取るが、**順序は `o.mu` → `rs.mu` なので入れ子にしてよい**
（`RunViews` が `o.mu` の中で `rs.snapshot()` を呼ぶのと同じ形である）。

**読む側**（`internal/server` の `Server.snapshot`）**は、run の写しを先に、累計を後に読む。**
2つを同じ錠で2度取り直して読むので、隙間に turn が1つ終わりうる。
**累計は減らないので、run を先に読めば「累計が走行中の合計より小さい」写しは作れない。**
**逆順にすると、その瞬間だけ小さく見える。**10秒ごとに自動で再読み込みする画面なので、目に留まる。

**守るべき順序は、この読む順序1本だけである**（`TestAPIState_累計はrunの写しより後に取る` が押さえている）。

#### 累計の意味

> **この continuo が起動してから、turn の終わりに読み取った transcript の合計である。**
> **引き継いだ run では、起動より前に書かれた分も含む。**
> **走行中の turn の分はまだ入っていない。**

**「continuo を起動してから」ではない**（引き継いだ run が起動前の分を含む）。
**「continuo の一生ぶん」でもない**（メモリだけに持ち、再起動で0に戻る）。
**その中間である。**画面の注記（`dashboard.note_cumulative`）に、この意味を書いてある。

#### 画面と JSON の出し方

**run ごとのトークンの表は1文字も変えない。**合計行もそのままなので、**行を全部足すと合計行に届く。**
**累計は別の表**（`dashboard.caption_cumulative` / `dashboard.note_cumulative`）**に出す。**
同じ表へ足すと、意味の違う3つ（run ごと・合計・累計）が並び、見出しも注記もどれも説明しなくなる。

**JSON も同じ。**`totals`（走行中の run の和）は1文字も変えず、`cumulative_totals` を足す。
**既存の鍵を変えないので、外から叩いている人には壊れない。**

### JSON の形（`SPEC.md` 13.7.2 との差）

**経路は仕様どおり `GET /api/v1/state` である**（`server.APIStatePath`）。
**応答の形は仕様の suggested shape と違う。**仕様が例示するのは提案であって要求ではない。

| 仕様の例 | continuo | なぜ |
| --- | --- | --- |
| `running` と `retrying` の2本の配列 | `runs` の1本 ＋ `counts` | **印の集合が1本しか無い**（設計 3-25）。バックオフ中の run も同じ集合に残る。どちらかは `backoff_until` で分かる |
| `tokens` は `input_tokens` / `output_tokens` / `total_tokens` | `api_calls` / `input` / `cache_creation` / `cache_read` / `output` / `total` | Claude Code の `usage` はキャッシュの読み書きを別に持つ（設計 3-15 の実測）。合算すると枠の使い方が読めなくなる |
| `codex_totals`（run をまたぐ累計） | **`cumulative_totals`**（issue #238） | **continuo は codex を使わない。**中身も上の行と同じ理由で6項目である |
| `codex_totals` の `seconds_running`（走った秒数の累計） | **持たない** | **人間が求めたのはトークンである。**走行秒数は求められていない。`SPEC.md` 13.7 のとおり、HTTP サーバそのものが適合に必須ではない拡張である |

**`GET /api/v1/<issue_identifier>` は作らない。**
**仕様は `Minimum endpoints:` の下にこれを挙げている**（`SPEC.md` 13.7.2）。
**それでも作らないのは、HTTP サーバそのものが適合に必須ではない拡張だからである**
（13.7 の "The HTTP server is an extension and is not REQUIRED for conformance."。
**訳:** HTTP サーバは拡張であり、仕様への適合に必須ではない）。
そのうえで、run ごとの項目は `/api/v1/state` が全部返している。**識別子に `/` と `#` を含む**ので、
経路に埋めると引用の規則を1つ増やすことになる。

### 応答の実物

```json
{"identifier":"octocat/hello-world#12","state":"In Progress","turn_count":3,
 "last_hook_at":"2026-08-19T11:58:30Z","last_hook_ago":"1分30秒前",
 "stall_clock_at":"2026-08-19T11:59:59Z",
 "tokens":{"api_calls":19,"input":38,"cache_creation":14358,"cache_read":701185,"output":1216,"total":716797},
 "tokens_at":"2026-08-19T11:58:40Z"}
```

**run をまたぐ累計は、`runs` の外に1本だけ出る**（issue #238）。

```json
{"totals":{"api_calls":19,"input":38,"cache_creation":14358,"cache_read":701185,"output":1216,"total":716797},
 "cumulative_totals":{"api_calls":362,"input":723,"cache_creation":668566,"cache_read":154673000,"output":275073,"total":155617362}}
```

### テスト

| 置き場所 | 何を確かめるか |
| --- | --- |
| [test/internal/server/](../../../test/internal/server/) | 23件。ループバック以外から接続できないこと（この機材のループバックでない IPv4 へ実際に接続する）、ループバック以外の宛先を 421 で断ること、GET 以外が 405 になること、タイトルと URL のエスケープ |
| [test/internal/orchestrator/dashboard_test.go](../../../test/internal/orchestrator/dashboard_test.go) | 3件。**本物の供給経路**（transcript を読む → 集計する → `runState` → `RunViews`）が繋がっていること、再 dispatch で累計が巻き戻らないこと、`LastHookAt` が hook でしか進まないこと |
| [test/internal/orchestrator/cumulative_tokens_test.go](../../../test/internal/orchestrator/cumulative_tokens_test.go) | 7件（issue #238）。turn を重ねても同じ transcript を二重に数えないこと、**違うファイル名を名乗られても二重に数えないこと**、**run が終わっても累計に残ること**、引き継いだ run が transcript 全体を累計へ入れること、`TokenUsage.Sub` が負を0へ丸めて知らせること、**引き渡しのあと復帰しても二重に数えないこと**（`release` では台帳を落とさない）、**片付けを見送ったあと復帰しても二重に数えないこと**（`cleanupPath` が偽なら落とさない） |
| [test/internal/server/cumulative_test.go](../../../test/internal/server/cumulative_test.go) | 3件（issue #238）。JSON に `cumulative_totals` が出て `totals` が変わっていないこと、累計を run の写しより後に取っていること、HTML に累計の表が出て run ごとの表が変わっていないこと |
| [test/internal/daemon/daemon_test.go](../../../test/internal/daemon/daemon_test.go) | 2件。ダッシュボードが開けなくても巡回まで進むこと、`--port` で開いた口に引き継いだ run が出ること |

**偽の供給元に値を直接入れるテストだけにしない。**`internal/orchestrator/lifecycle.go` の
集計をまるごと落としても全部通ってしまい、配線が切れたことに気づけないためである
（実際に落として、3件のうち2件が落ちることを確かめてある）。
