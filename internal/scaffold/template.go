package scaffold

// workflowTemplate は continuo init が書き出す WORKFLOW.md の雛形である。
//
// front matter のキー構成は docs/plans/continuo_design.md の 5-2 と、本文は 5-3 と一致させる
// （test/internal/scaffold/design_template_test.go が設計文書と直接突き合わせている）。
// ここで新しい既定値を作らない。
//
// コメントは、この雛形だけを読む人が意味を判断できる文面にする。設計文書の節番号は書かない。
// WORKFLOW.md を開く人は設計文書を持っていないので、節番号は何も伝えない。
//
// 構造体から yaml.Marshal して生成しないのは、YAML のコメントが全部消えるからである。
// コメントが無いと雛形として役に立たないため、文字列リテラルとして持つ。
//
// tracker.provider.owner と tracker.provider.project_number の2つはプレースホルダにしてある。
// continuo init は gh から引いた値でここを埋める（fill.go / detect.go）。引けなかったときは
// プレースホルダのまま残り、config.Load が名指しで落とす。
//
// trust.repositories も continuo init が埋めるが、こちらは空のままでも起動する。
// ボードから拾った owner/repo をここへ並べるだけで、**要らない行を消すのは人間である**
// （並べたものをそのまま信頼させないため。設計 3-33）。
const workflowTemplate = `---
# ===== どの issue を見張り、どう進めるか =====
tracker:
  kind: github_projects_v2                  # 見張る先の種類。いまは GitHub Projects v2 だけ
  provider:                                 # ここから下は GitHub Projects v2 に固有の設定
    owner: __FILL_ME__                      # ここを埋めること。例: https://github.com/maimuzo なら maimuzo
    project_number: 0                       # ここを埋めること。例: https://github.com/users/maimuzo/projects/3 なら 3
    status_field: Status                    # issue の進み方を読み書きする single-select フィールドの名前
    token_source: gh_auth                   # gh_auth なら gh auth token コマンドで取る。env なら下の token_env から取る
    token_env: GITHUB_TOKEN                 # token_source が env のときに読む環境変数の名前
    comments:                               # GitHub からコメントを何件どの順で取るか。GitHub の上限に縛られる項目だけを置く
      max: 50                               # 判別のために何件まで遡って読むか。GitHub は一度に100件までしか返さない
      order: oldest_first                   # 読む順番。古いコメントから読む
  comments:                                 # continuo とエージェントのあいだの取り決め。GitHub 固有ではない
    marker: "<!-- continuo:agent -->"       # エージェントが書くコメントの先頭に必ず入れさせる目印
    self_marker: "<!-- continuo:self -->"   # continuo 自身が書くコメントの目印。引き渡しの連絡だけで、成果は書かない
  status_signal_prefix: "CONTINUO-STATUS:"  # エージェントが応答の最後に書く1行の先頭。continuo はこの行を読んで Status を動かす
  status_signal_map:                        # その1行に書かれた値と、書き込む Status の対応
    review: "In Review"                     # 作業が終わり、人間のレビューに回してよいとき
    blocked: "Blocked"                      # 判断を仰ぎたいとき、または失敗したとき
    working: null                           # まだ続きがあるとき。null なので Status は動かさない
  required_labels: []                       # ここに書いたラベルが全部付いた issue だけを対象にする。空なら絞り込まない
  active_states: ["Ready", "In Progress"]   # 対象にする Status。下の running_state と dispatch_state を必ず含めること
  terminal_states: ["Done"]                 # 終わったとみなす Status。ここへ移った issue の worktree を片付ける
  running_state: "In Progress"              # エージェントを起動したときに書き込む Status
  dispatch_state: "Ready"                   # 着手待ちの Status。取り残された issue はここへ戻す
  failure_state: "Blocked"                  # 打ち切ったとき・失敗したときに落とす Status
  verify_states_every: 20                   # 上に書いた Status 名がボードに実在するかを、何巡回ごとに照合するか。
                                            # 0 なら起動したときだけ照合する。名前がずれていると issue が1件も見つからなくなる

polling:
  interval_ms: 30000                        # ボードを読み直す間隔。30000 なら30秒ごと

workspace:
  root: ~/worktrees                         # worktree を作る場所。先頭の ~ はホームディレクトリに展開する。
                                            # 中の並べ方は <root>/<ホスト>/<owner>/<repo>/<branch> に固定で、選べない
  identity_file: .continuo.json             # どの issue の worktree かを worktree の中に書き残すファイルの名前

workspace_hooks:                            # worktree の節目に走らせるコマンド。Claude Code の hook とは別物
  after_create: null                        # worktree を作った直後に走る。失敗したらその issue は進めない
  before_run: null                          # エージェントを起動する直前に走る。失敗したらその issue は進めない
  after_run: null                           # エージェントが終わった直後に走る。失敗しても記録して先へ進む
  before_remove: null                       # worktree を消す直前に走る。失敗しても記録して先へ進む
  timeout_ms: 60000                         # 上の4つのコマンドの制限時間。どれも worktree の中で走る

agent:
  max_concurrent_agents: 2                  # 同時に動かすエージェントの数の上限
  max_concurrent_agents_by_state: {}        # Status ごとの上限。空なら上の全体の上限だけを見る。
                                            # 引かれるのは running_state（下の "In Progress"）だけで、
                                            # 他の Status 名を書いても参照されない。0 以下は書けない
  max_dispatch_turns: 20                    # 1つの issue に continuo が指示を送る回数の上限。尽きたら failure_state へ落とす。
                                            # エージェントが自分で続けた turn は数えない
  max_takeover: 5                           # continuo が落ちたあと、同じ worktree を引き継いだ回数の上限
  max_retry_backoff_ms: 300000              # やり直しの前に待つ時間の上限。失敗のたびに待ち時間を伸ばしていく
  max_retries: 3                            # 応答が止まった・異常終了したときにやり直す回数の上限。0 ならやり直さない

# ===== Claude Code をどう起動するか =====
claude:
  kind: claude                              # herdr に起動させるエージェントの種別
  permission_mode: dontAsk                  # 人間に確認を出さない唯一のモード。無人で回すので必ずこれにする
  permissions:                              # dontAsk のとき、allow に書いていないツールは全部拒否される
    allow:
      - "Bash"                              # ツール名だけを書く。引数まで絞ると書き込み系の操作が拒否される
      - "Read"
      - "Glob"
      - "Grep"
      - "Edit"
      - "Write"
    deny: []                                # 明示的に禁じるツール。subagent を起動するツールは allow に書かなくても動く
  env:                                      # Claude Code に渡す環境変数
    CLAUDE_CODE_RETRY_WATCHDOG: "1"         # turn の途中で 429 / 529 が返ってきたときに、リトライを続けさせる
  poll_wait_ms: 30000                       # エージェントの状態を1回待つ時間。短く切って、経過時間は continuo 側で数える
  settle_ms: 2000                           # 応答が終わったように見えてから、続きが来ないことを確かめるまでの猶予
  wait_until: ["idle", "done", "blocked"]   # 待つのをやめる状態。書けるのは idle / working / blocked / done / unknown。
                                            # blocked を外すと、確認で止まった turn を時間切れまで拾えない
  turn_timeout_ms: 3600000                  # エージェントの画面が変わらない時間がこれを超えたら打ち切る。0 以下なら打ち切らない。
                                            # turn の総実行時間の上限ではない。画面が変わり続けている限り何時間でも待つ
  hook_bridge:                              # Claude Code の hook を continuo へ届ける仕掛け。turn の終わりはこれで知る。
                                            # 届け方は「issue ごとに作った設定ファイルを --settings で渡す」に固定で、選べない
    listen: null                            # hook を受け取る socket の置き場所。null なら continuo が決める。書くなら絶対パス。
                                            # ホーム直下のような共用のディレクトリを指さないこと。権限が 0700 でなければ起動を止める

# ===== herdr（pane と worktree をまとめる常駐プロセス）との連携 =====
herdr:
  socket: ~/.config/herdr/herdr.sock        # herdr が待ち受けている socket。既定の場所をそのまま書いてある。
                                            # 環境変数で切り替えるなら ${HERDR_SOCKET_PATH} と書く。未定義なら起動を止める
  protocol: 19                              # herdr の socket API の版。起動時に照合して、合わなければ止める
  read_timeout_ms: 5000                     # herdr の socket が応答を返すまでの制限時間。待ちを伴う呼び出しには使わない
  startup_timeout_ms: 60000                 # herdr がエージェントを起動し終えるまで待つ時間
  worktree:
    create_via_herdr: true                  # 作った worktree を herdr の workspace として開くかどうか（worktree 自体は git で作る）
    # issue ごとに作る branch の名前。二重の波括弧の部分は issue の値に置き換わる
    branch_template: "continuo/{{.issue.owner}}/{{.issue.repo}}/{{.issue.number}}"
    base: null                              # 分岐元の branch。null ならリポジトリの既定 branch から分岐する

# ===== 後始末・使用量・二重起動の防止 =====
naming:
  warn_on_information_loss: true            # issue の識別子から branch 名を作るときに文字が落ちたら警告を出す

cleanup:
  enabled: true                             # 終わった issue の worktree と branch を片付けるかどうか
  on_states: ["Done"]                       # この Status へ移った時点で片付ける
  require_clean_worktree: true              # commit していない変更が残っていたら消さない
  require_pushed: true                      # push していない commit が残っていたら消さない
  delete_branch: true                       # worktree と一緒に branch も消すかどうか
  sweep_on_startup: true                    # 起動したときに、終わっている worktree と行き場の無い branch を消す

rate_limit:
  source: oauth_usage_api                   # Claude の使用量 API から枠の残りを読む。none なら枠を見ない
  token_source: claude_credentials          # ~/.claude/.credentials.json から読む。env なら下の token_env から読む
  token_env: CLAUDE_CODE_OAUTH_TOKEN        # token_source が env のときに読む環境変数の名前
  pause_above_percent: 95                   # 枠の使用率がこれを超えたら新しい issue に着手しない。動いている turn は止めない
  poll_interval_ms: 300000                  # 枠の残りを読み直す間隔

trust:
  require_repo_trusted: true                # 信頼していないリポジトリではエージェントを起動しない
  on_untrusted: skip_and_comment            # 信頼していないときの扱い。その issue だけ飛ばし、issue にコメントを残す
  repositories: []                          # continuo trust が信頼を登録してよいリポジトリ。owner/repo を1行ずつ書く。
                                            # continuo init がボードから拾って並べるので、要らない行は消すこと。
                                            # **これから issue を作るリポジトリは、まだボードに無いので拾えない。**手で足すこと。
                                            # 巡回のループはここを読まない。continuo trust だけが読む

restart:
  orphan_running_action: redispatch         # 落ちている間に取り残された issue の扱い。redispatch は同じ worktree で
                                            # もう一度起動する。to_dispatch_state は着手待ちへ戻し、to_failure_state は失敗として落とす

runtime:
  lock_file: null                           # 二重起動を防ぐロックファイル。null なら hook の socket と同じディレクトリに置く

server:
  port: null                                # 進み具合を見る HTTP ダッシュボードのポート。null なら起動しない。
                                            # 0 なら空いているポートを OS に選ばせる。--port を渡すとそちらが優先される

# ===== 画面に出す言語 =====
language: auto                              # 画面に出す文言の言語。auto なら環境変数 LANG から決める。
                                            # ja と en を直接書いてもよい。訳の無い文言は日本語で出る
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
