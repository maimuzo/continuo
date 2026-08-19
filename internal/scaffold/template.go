package scaffold

// workflowTemplate は continuo init が書き出す WORKFLOW.md の雛形である。
//
// 中身は docs/plans/continuo_design.md の 5-2（front matter）と 5-3（本文）をそのまま写し、
// 3-32 の表にあるプレースホルダだけを差し替えたものである。ここで新しい既定値を作らない。
//
// 構造体から yaml.Marshal して生成しないのは、YAML のコメントが全部消えるからである。
// コメントには「ここを埋めること」と、どの設計の節に理由があるかが書いてある。
// これが無いと雛形として役に立たないため、文字列リテラルとして持つ。
//
// 本文にバッククォートが含まれるため、raw string literal をバッククォートの位置で切り、
// "`" を挟んで連結している。切れ目そのものに意味は無い。
const workflowTemplate = `---
# ===== 仕様に由来するキー（SPEC.md 5.3。名前を変えない）=====
tracker:
  kind: github_projects_v2
  provider:                                 # アダプタが所有する。仕様は中身を規定しない
    owner: __FILL_ME__                      # ここを埋めること。project を所有する GitHub の user / organization 名
    project_number: 0                       # ここを埋めること。ボードの番号（例: project #3 なら 3）。数値で書く
    status_field: Status
    token_source: gh_auth                   # gh_auth | env。continuo 自身がボードを読むための認証（3-3 の表の外）
    token_env: GITHUB_TOKEN                 # token_source が env のときだけ使う
    comments:                               # **プロンプトには埋め込まない**（3-29）。誰が書いたコメントかの判別に使う
      fetch: true                           # エージェントがコメントを書いたかを確かめるために読むかどうか
      max: 50                               # 判別のために何件まで遡るか
      order: oldest_first
      marker: "<!-- continuo:agent -->"     # エージェントに、コメントの先頭へ必ず書かせる印（2-2）
      self_marker: "<!-- continuo:self -->"  # continuo 自身が書くコメントの印（引き渡しの通知だけ。成果は書かない）
  status_signal_prefix: "CONTINUO-STATUS:"  # エージェントが最終応答に書く表明の印（3-25）
  status_signal_map:                        # 表明の値と Status の対応
    review: "In Review"
    blocked: "Blocked"
    working: null                           # null なら Status を動かさない
  required_labels: []
  active_states: ["Ready", "In Progress"]   # In Progress を必ず含める（3-10）
  terminal_states: ["Done"]                 # In Review を入れない（3-9 / 3-10）
  running_state: "In Progress"              # dispatch したときに書き込む先（3-16 の段2）
  dispatch_state: "Ready"                   # 取り残された issue を戻す先
  failure_state: "Blocked"                  # 打ち切り・失敗のときに落とす先（4-1）
  write_interval_ms: 1000                   # 書き込みどうしの間隔。GitHub の推奨が1秒以上（3-31）
  verify_states_every: 20                   # Status の選択肢名を照合する間隔（巡回の回数。3-6）。
                                            # 毎巡回では行わない。0 なら起動時だけ

polling:
  interval_ms: 30000

workspace:
  root: ~/worktrees                         # gwq の既定に合わせる（3-22）。チルダは展開する（5-5）
  layout: gwq                               # gwq なら <host>/<owner>/<repo>/<branch>
  identity_file: .continuo.json             # worktree の身元を書くファイル（3-18）

workspace_hooks:                            # 仕様 9.4。Claude Code の hook とは別物なので名前を変えた（8-1）
  after_create: null                        # 失敗したら致命。cwd は worktree
  before_run: null                          # 失敗したら致命
  after_run: null                           # 失敗しても記録して続ける
  before_remove: null                       # 失敗しても記録して続ける
  timeout_ms: 60000

agent:
  max_concurrent_agents: 2
  max_concurrent_agents_by_state: {}        # 状態ごとの上限。空なら全体の上限にフォールバック
  max_turns: 20
  max_takeover: 5                           # 引き継いだ回数の上限（3-4 / 3-18）
  max_retry_backoff_ms: 300000
  max_retries: 3                            # stall や異常終了のリトライ回数の上限。尽きたら failure_state へ

# ===== 仕様の codex セクションの置き換え（SPEC.md 5.3.6 に対応。中身は全面差し替え）=====
claude:
  kind: claude                              # herdr に渡す agent の種別
  permission_mode: dontAsk                  # 3-11。入力を待たない唯一のモード
  permissions:                              # dontAsk は許可リストの外を全部拒否する（3-11）
    allow:
      - "Bash"                              # ツール名だけ。引数を限定すると書き込み系が拒否される（3-11）
      - "Read"
      - "Glob"
      - "Grep"
      - "Edit"
      - "Write"
    deny: []                                # subagent を起動する Agent ツールは、許可リストが空でも動いた（3-11）
  env:
    CLAUDE_CODE_RETRY_WATCHDOG: "1"         # 3-11。turn の途中で 429/529 が返ったときリトライし続ける。
                                            # 枠で止まったあとの継続は Claude Code 2.1.234 が
                                            # 既定で行う（/config の Continue automatically at
                                            # usage limit）。両方が効く（3-27）
  poll_wait_ms: 30000                       # agent.wait 1回あたりの待ち時間（3-2）。短く切って continuo 側で
                                            # 総経過時間を数えるためのもの。turn の上限そのものではない
  settle_ms: 2000                           # background_tasks が空の Stop を受けてから、<task-notification> が
                                            # 来ないことを確かめるまでの猶予（1-3 / 3-2）。観測できた8件は 0.037 秒以内。
                                            # 上限は測れていないので暫定値。実際の間隔をログに出して決め直す（第6節）
  wait_until: ["idle", "done", "blocked"]   # agent.wait に渡す状態。blocked を外すと
                                            # 権限の確認で止まった turn を拾えず、時間切れまで待つことになる（3-2）
  turn_timeout_ms: 3600000                  # 1つの turn の上限。continuo が turn を送ってから Stop を受けるまでを測る
  read_timeout_ms: 5000                     # 仕様と同名だが相手が違う。対象は herdr の socket API の応答（8-1）。
                                            # ただし待ちを伴う呼び出しには適用しない。
                                            # agent.start は startup_timeout_ms、待機ありの agent.prompt は turn_timeout_ms を使う
  stall_timeout_ms: 1800000                 # 30分。0 以下で無効。理由は 3-21
  startup_timeout_ms: 60000                 # herdr の agent 起動の待ち時間
  hook_bridge:                              # turn 終了検知の実体（3-12）
    mode: settings_flag                     # settings_flag のみ。worktree_local は仕様を書いていないので受理しない（3-12）
    listen: null                            # null なら 3-23 の探索順で決める。明示するなら絶対パス。
                                            # 既にある共用のディレクトリ（ホーム直下など）を指さないこと。
                                            # continuo は自分が作っていないディレクトリの権限を書き換えず、
                                            # 権限が 0700 でなければ起動を止める（3-23）
    liveness_hooks: ["PreToolUse", "PostToolUse"]   # 生きていることの確認だけに使う（3-21）。
                                            # 判定に使う hook の一覧は 3-2 で固定しており、設定では変えられない

# ===== herdr 連携（仕様に対応物が無い。全部独自）=====
herdr:
  socket: ~/.config/herdr/herdr.sock        # herdr の socket。既定のパスをそのまま書く（2-1）。
                                            # 環境変数で切り替えたいなら ${HERDR_SOCKET_PATH} と書く。
                                            # その場合、未定義だと起動を止める（5-5。既定値へは落ちない）。
                                            # 到達できなければ起動時の検査で止まる（3-6）
  protocol: 19                              # herdr の socket API の版。起動時に照合して合わなければ止める。
                                            # 2026-08-18 に herdr api schema で 19 であることを確認済み（2-1）
  worktree:
    create_via_herdr: true
    branch_template: "continuo/{{.issue.owner}}/{{.issue.repo}}/{{.issue.number}}"
    base: null                              # null ならトラッカーが返す既定 branch を使う

# ===== continuo 独自の運用要件 =====
naming:                                     # 3-7
  warn_on_information_loss: true

cleanup:                                    # 3-9
  enabled: true
  on_states: ["Done"]                       # ここに入った時点で片付ける。active でなくなった時点ではない
  require_clean_worktree: true              # 未コミットの変更があれば消さない
  require_pushed: true                      # push していない commit があれば消さない（3-9）
  delete_branch: true
  sweep_on_startup: true                    # 起動時に、終了状態の worktree と孤児 branch を消す

rate_limit:                                 # 3-27。仕様の範囲外
  source: oauth_usage_api                   # oauth_usage_api | none。none なら枠の判定をしない
  token_source: claude_credentials          # claude_credentials | env。読み取りだけ
  token_env: CLAUDE_CODE_OAUTH_TOKEN        # token_source が env のときに読む環境変数。env のとき必須
  pause_above_percent: 95                   # 超えたら新規の dispatch を止める。run中の turn は止めない
  poll_interval_ms: 300000

trust:                                      # 3-11 / 4-3
  require_repo_trusted: true
  on_untrusted: skip_and_comment            # その issue だけ飛ばす。起動は止めない（3-6）

restart:                                    # 3-4 の段8。redispatch は worktree を再利用して再 dispatch、
                                            # to_dispatch_state は Ready へ戻す、to_failure_state は Blocked へ落とす
  orphan_running_action: redispatch         # redispatch | to_dispatch_state | to_failure_state

runtime:                                    # 3-17
  lock_file: null                           # null なら hook の socket と同じディレクトリに置く

server:                                     # SPEC 13.7 の任意拡張。キー名は仕様どおり
  port: null                                # null ならサーバを起動しない。数値なら起動する
---

{{.issue.identifier}} を実装してください。

## この issue を読むこと

**まず次のコマンドで、issue の本文とコメントを全部読んでください。**

    gh issue view {{.issue.url}} --comments

**読めなかった場合は、その旨を最終応答に書いて ` +
	"`" +
	`CONTINUO-STATUS: blocked` +
	"`" +
	` を出してください。**
中身が分からないまま作業を始めないでください。

## 終わったらやること

**作業の区切りがついたら、応答の最後に次のいずれか1行を必ず書いてください。**

    CONTINUO-STATUS: review     作業が終わり、人間のレビューに回してよい
    CONTINUO-STATUS: blocked    判断を仰ぎたい、または失敗した
    CONTINUO-STATUS: working    まだ続きがある

**` +
	"`" +
	`review` +
	"`" +
	` を出す前に、必ず commit して push してください。**
push していない作業は、この worktree が片付くときに失われます。

**読んだコメントに「まとめて対応する issue のグループ」が書かれている場合は、
同じリポジトリの issue に限り、まとめて直してください。**
その場合は issue ごとに1行ずつ表明を書いてください。

    CONTINUO-STATUS: review          （いま作業している issue）
    CONTINUO-STATUS: #45 review      （同じグループの別の issue）

**別のリポジトリの issue が含まれている場合は、直さずに次のように書いてください。**

    CONTINUO-STATUS: #99 working     （別リポジトリなので、この worktree では直せない）

**この1行を読んで Status を動かすのは continuo です。あなたが ` +
	"`" +
	`gh` +
	"`" +
	` を叩く必要はありません。**

**あわせて、何をしたかを issue のコメントに残してください。**コメントの先頭には次の1行を書いてください。

    <!-- continuo:agent -->

    gh issue comment {{.issue.url}} --body "<!-- continuo:agent -->
    ここに何をしたかを書く"

**このコメントを書かずに turn を終えた場合、continuo はセッションを復元してもう一度あなたに書かせます。**
**あなたが書かない限り、作業は完了として扱われません。**

{{if .attempt}}この作業は {{.attempt}} 回目の試行です。前回は完了せずに終わっています。{{end}}
`
