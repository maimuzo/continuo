<!-- 目的: 「再起動して実行中の issue を引き継ぐ」の RUCM を書くときに AI が行った判断と、その根拠を記録する -->

# 判断ログ: 再起動して実行中の issue を引き継ぐ

- 対象: `docs/spec/usecases/particular_case/再起動して実行中の issue を引き継ぐ.rucm.md`
- 作成日 / 作成モデル: 2026-08-20 / Claude Opus 5 (1M context)
- 参照した根拠資料: `docs/plans/continuo_design.md`（3-3 / 3-4 / 3-6 / 3-17 / 3-18 / 3-19 / 8-1）、`internal/orchestrator/restore.go`、`internal/orchestrator/turn.go`、`internal/workspace/scan.go`、`internal/workspace/identity.go`、`internal/lock/lock.go`

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | USE CASE NAME | 再起動して実行中の issue を引き継ぐ | 依頼で指定された名前をそのまま使う。動詞で終わる名詞句の規則も満たす | 依頼文 | 100% |
| 2 | 配置ディレクトリ | `particular_case/` | 「起動し直して1件の run を引き継ぐ」という単一目的の操作単位である | rucm スキルの粒度ガイド | 95% |
| 3 | 記述の単位 | **対象の run 1件の引き継ぎとして書く** | 復元の手順は置き場所の全件を走査するが、引き継ぐかどうかの判断は run ごとに独立している。**全件を1本のフローで書くと、Status ごとの分岐が入れ子になって読めなくなる** | `docs/plans/continuo_design.md#3-4`（段5a の表は run ごとの判断である） | 70% |
| 4 | BRIEF DESCRIPTION | 起動し直す・突き合わせる・listen して読み戻す・継続の指示を送る の4文 | 復元の9段の骨格を4文へ落とした。R12（単文のみ）を守るために文を分けた | `docs/plans/continuo_design.md#3-4` | 90% |
| 5 | PRECONDITION | 前のプロセスが終了している。身元ファイルを持つ worktree がある。herdr が待ち受けている。前の run の pane が生きている | pane が生きていないと「引き継ぐ」が成立しない（pane が無い場合は代替フローで扱う） | `docs/plans/continuo_design.md#3-4` の段4 と段8 | 90% |
| 6 | PRIMARY ACTOR | 利用者 | continuo を起動し直すのは人間である。**巡回タイマーではない**（プロセスがまだ存在しない） | `docs/plans/continuo_design.md#3-4` | 95% |
| 7 | SECONDARY ACTORS | GitHub Projects v2、herdr、Claude Code | ボードを取り直し、pane と agent を引き、継続の指示を送る相手がこの3つである | `docs/plans/continuo_design.md#3-4` | 90% |
| 8 | DEPENDENCY | なし | 他の particular_case を取り込まない | rucm スキルのファイル規約 | 95% |
| 9 | GENERALIZATION | なし | 汎化関係にあるユースケースが無い | - | 90% |
| 10 | ステップ分割方針 | 復元の段1 から段9 を、段ごとに1ステップ以上へ割る | 設計が段の順序そのものを決めている（listen を段5d、読み戻しを段5e、配送の開始を段6b にする）。**まとめると順序の意味が消える** | `docs/plans/continuo_design.md#3-4`、`internal/orchestrator/restore.go` の `Restore` | 95% |
| 11 | ステップ2（VALIDATES THAT） | ロックファイルの flock を取れる | 二重起動の判定は flock 1本だけで行う。`ps` は使えない（hook を届けるサブコマンドが同じ実行ファイル名で起動するため、turn ごとに誤判定する） | `docs/plans/continuo_design.md#3-17`、`internal/lock/lock.go` | 100% |
| 12 | ステップ3（VALIDATES THAT） | 起動時の検査をすべて通る | 検査は復元より前に置く。**落ちても pane を閉じない**（原因は continuo 側の前提であって、エージェントの側に問題があるわけではない） | `docs/plans/continuo_design.md#3-4` の段3、`#3-6` | 100% |
| 13 | ステップ4 | 置き場所を4階層まで走査する | 深さは固定で4階層（`<root>/<host>/<owner>/<repo>/<スラグ>`）。それより深くは掘らない | `docs/plans/continuo_design.md#3-4` の段2、`internal/workspace/scan.go` | 95% |
| 14 | ステップ6（VALIDATES THAT） | ボードを project item の ID 指定で取り直せる | この1回が「落ちている間に届かなかった Stop の取り戻し」も兼ねる。**hook を待たずにここで現在の Status を確定させる** | `docs/plans/continuo_design.md#3-4` の段3、`#3-19` | 95% |
| 15 | ステップ8（VALIDATES THAT） | worktree のパスと cwd が一致する pane がある | 突き合わせは両方を `filepath.EvalSymlinks` で解決してから行う。**pane の cwd は起動時の文字列がそのまま入りうる** | `docs/plans/continuo_design.md#3-4` の段4、`internal/orchestrator/restore.go` の `matchPanes` | 95% |
| 16 | ステップ9（VALIDATES THAT） | 取り直した Status が active_states に入っている | 段5a の表の分岐点である。`cleanup.on_states` は片付け、引き渡しは残す、見つからないは残す。**active_states だけが引き継ぐ側である** | `docs/plans/continuo_design.md#3-4` の段5a、`internal/orchestrator/restore.go` の `decideOne` | 95% |
| 17 | ステップ10（VALIDATES THAT） | pane の agent_status を読み取れる | 取れない値と知らない値は判断できないので、pane を閉じて worktree と Status を残す | `docs/plans/continuo_design.md#3-4` の段5a2 | 90% |
| 18 | ステップ11（VALIDATES THAT） | agent_status が blocked でない | **Status だけで決めてはならない。**`blocked` のまま引き継いで turn を送ると、保留中の権限要求が承認されて実行される（実測3/3） | `docs/plans/continuo_design.md#3-4` の段5a2、`#3-11` | 100% |
| 19 | `blocked` で esc を送らないこと | pane ごと閉じる | pane を閉じれば保留中の要求も消える。**esc を送るのは、引き継いで使い続ける場合だけである** | `docs/plans/continuo_design.md#3-4` の段5a2 | 95% |
| 20 | ステップ12（VALIDATES THAT） | 引き継いだ回数が `agent.max_takeover` に達していない | turn を送る前に判定する。**達していたら turn を1回も送らない**（無駄な turn を1回も送らないため） | `docs/plans/continuo_design.md#3-4` の段5b | 95% |
| 21 | ステップ13 | 引き継いだ回数を1つ増やして身元ファイルへ書き戻す | 落ちるたびに turn 数が1に戻るので、この回数が無いと打ち切りが永久に発火しない | `docs/plans/continuo_design.md#3-4`、`#3-18` | 100% |
| 22 | ステップ14 と15 | セッション UUID を `agent_session` から、agent 名を `agent.list` から取る | `agent.prompt` と `agent.wait` の宛先は agent 名であり、pane の ID では送れない。**hook の対応づけの復元にはセッション UUID が要る** | `docs/plans/continuo_design.md#3-4` の段5、`internal/orchestrator/restore.go` の `decideOne` | 95% |
| 23 | ステップ17 から20 の順序 | listen → 読み戻し → 印を付ける → 配送の開始 | 段5d で listen しないと、読み戻しが終わってから listen するまでの窓に落ちた hook を誰も読まない。段6 で索引ができるまで配送できない | `docs/plans/continuo_design.md#3-4` の段5d / 5e / 6 / 6b、`internal/orchestrator/restore.go` の `Restore` | 100% |
| 24 | ステップ21 | turn 数を 1 に戻す | turn 数は復元できない。`SPEC.md` 14.3 も実行中のセッションや稼働中の worker の状態がプロセスの再起動を生き延びることを意味しないと明記している | `docs/plans/continuo_design.md#3-4` の段7 | 95% |
| 25 | ステップ22 から27（IF-ELSE） | `working` なら Stop hook を待ち、そうでなければ次の turn を要する印を立てて継続の指示を送る | **どちらの分岐も正常である**ので基本フローの `IF-ELSE` で書いた。走っている最中に投げると turn が混ざる | `docs/plans/continuo_design.md#3-4` の段5a2、rucm スキルの「基本フローに書くこと」の例外 | 90% |
| 26 | 復元の中で turn を送らないこと | 印を立て、巡回の turn ループが送る | 復元の中で `agent.prompt` を呼ぶと、wait つきの呼び出しが turn の終わりまで返らない（既定1時間）。**復元がそこで止まる** | `docs/plans/continuo_design.md#3-4` の段5c、`internal/orchestrator/turn.go` の `startTurnLoop` | 100% |
| 27 | 送る本文を継続の指示にしたこと | 1回目の本文ではなく継続の指示（5-4）を送る | セッションは引き継いでいるので、エージェントは issue の URL も作法も既に知っている。**turn 数を1から数え直すのは打ち切りの計算のためであって、1回目をやり直すことではない** | `docs/plans/continuo_design.md#3-4` の段5c | 95% |
| 28 | 基本フローの POSTCONDITION | 印に入っている。引き継いだ回数が1つ増えている。turn 数が1である。pane が閉じていない。worktree が残っている。Status が running_state のまま | 引き継ぎが成立した状態を、次の巡回の振る舞いを決める値だけで書いた | `docs/plans/continuo_design.md#3-4` | 90% |
| 29 | フロー `二重起動` | RFS BASIC FLOW 2。pane を閉じずに終了する | 2つ目のプロセスが1つ目の pane を閉じたら、動いている作業を殺すことになる | `docs/plans/continuo_design.md#3-17` | 95% |
| 30 | フロー `前提の不足` | RFS BASIC FLOW 3。pane を閉じずに終了する | 段2 より前は、そもそもどの pane が continuo のものかを知らない。**設定の誤りで動いているエージェントの作業を殺してはならない** | `docs/plans/continuo_design.md#3-4` の段3 の注意書き | 100% |
| 31 | フロー `ボードの取り直しの失敗` | RFS BASIC FLOW 6。pane を閉じて worktree と Status を残す | **pane を残してはならない。**巡回には「生きている pane を見つけて引き継ぐ」経路が無いので、残すと次の巡回で同じ worktree に2つ目の Claude Code が立つ | `docs/plans/continuo_design.md#3-4` の段3 | 100% |
| 32 | フロー `paneの不在` | RFS BASIC FLOW 8。`restart.orphan_running_action` を読み、既定では何もしない | 既定の `redispatch` は復元の中では何もしない。**復元の中で dispatch すると、着手の段11 の待ちで最大1時間止まる** | `docs/plans/continuo_design.md#3-4`、`internal/orchestrator/restore.go` の `applyOrphanRunningAction` | 95% |
| 33 | フロー `引き渡し状態` | RFS BASIC FLOW 9。pane も worktree も残し、印に入れない | 再起動の直後は、その pane が「人間のレビュー待ちで正常に止まっているもの」なのか「取り残されたもの」なのかを区別できない。**消してから間違いに気づくより、残して人間に見せるほうが安全である** | `docs/plans/continuo_design.md#8-1`（再起動後は引き渡し状態の worker を止めない） | 100% |
| 34 | フロー `状態の不明` | RFS BASIC FLOW 10。pane を閉じて worktree と Status を残す | 段8b と同じ扱いにする。判断できない pane を残すと二重起動につながる | `docs/plans/continuo_design.md#3-4` の段5a2 | 90% |
| 35 | フロー `権限の確認での停止` | RFS BASIC FLOW 11。failure_state へ落として pane を閉じる | 権限の確認が出たということは、人間の判断が要るということである | `docs/plans/continuo_design.md#3-11`、`#4-1` | 95% |
| 36 | フロー `引き継ぎの上限` | RFS BASIC FLOW 12。failure_state へ落として pane を閉じ、worktree は残す | 上限に達していれば turn を1回も送らずに人間へ渡す。worktree を残すのは成果を失わないためである | `docs/plans/continuo_design.md#3-4` の段5b | 95% |
| 37 | フロー `中断` の種別と BRANCH FROM | GLOBAL ALTERNATIVE FLOW。BRANCH FROM BASIC FLOW 19 | 中断はどの時点でも起こりうるので任意時点代替フローにした。**分岐元はステップ19（印を付けた直後）である。**印はメモリなので Ctrl+C で消えるのに pane は生き残るため、ここが「印と実体の食い違い」が最大になる時点である | `docs/plans/continuo_design.md#3-10`（印が失われるとどうなるか） | 80% |
| 38 | `中断` で pane を閉じないこと | 巡回を止め、socket を閉じ、pane は閉じずに終了する | 既存の scenario（`はじめて continuo を動かせるようにする`）の `常駐の中断` と同じ扱いに揃えた | `docs/spec/usecases/scenario/はじめて continuo を動かせるようにする.rucm.md` | 85% |
| 39 | 表を2つ足したこと | 「Status で決める」と「agent_status で決める」を本文の表にした | 設計は2段階の分岐で決めており、rucm ブロックだけでは分岐の全体像が見えない。**代替フローに落とせるのは1件ずつの判断だけである** | `docs/plans/continuo_design.md#3-4` の段5a / 5a2 | 85% |
| 40 | 代替フローの POSTCONDITION の書き方 | 「Status」「pane」「worktree」「印」の4つを毎回書く | この4つが次の巡回の振る舞いを決める全部である | `docs/plans/continuo_design.md#3-4` | 85% |
| 41 | ステップ6（VALIDATES THAT）を足した理由 | 身元ファイルが名乗る owner とリポジトリ名が、置き場所の階層と一致することを見る | **`project_item_id` はエージェントが書き換えられる**（身元ファイルは worktree の直下にあり、そこでエージェントが `--permission-mode dontAsk` で動く）。走行中の別 issue の ID に書き換えて `created_at` を新しくすると、段2 が「同じ issue の worktree が2つある」と判定し、**被害者の生きた pane を『捨てた身元』として閉じる** | `docs/plans/continuo_design.md#3-43`、`internal/orchestrator/restore.go` の `pathAgrees`、`internal/abandon/abandon.go` の `pathAgrees` | 95% |
| 42 | 検算の材料を置き場所のパスに限ったこと | `<root>/<host>/<owner>/<repo>/<スラグ>` の固定4階層から引いた owner とリポジトリ名だけを根拠にする | **パスは封じ込め検査（3-20）を通っており、エージェントには書き換えられない。**身元ファイルの値を身元ファイルで検算しても意味が無い | `docs/plans/continuo_design.md#3-22`、`#3-43`、`internal/workspace/repo.go` の `OwnerRepoOf` | 100% |
| 43 | 検算を owner とリポジトリ名までにしたこと | issue の番号までは照合しない | 番号は `branch_template` を描画しないと出てこない。**テンプレートを変えた環境では、走っている worktree が全部「食い違い」になる。**`continuo abandon` の検算も同じ範囲である | `docs/plans/continuo_design.md#3-43`、`internal/abandon/abandon.go` の `pathAgrees` | 80% |
| 44 | ステップ8（VALIDATES THAT）を足した理由 | 取り直した issue の owner とリポジトリ名も置き場所と突き合わせる | 名乗りと置き場所が揃っていても、**`project_item_id` だけを別 issue のものへ差し替えれば**取り直しは別 issue を返す。ここで止めないと、その別 issue の Status を落としたり worktree を片付けたりする | `docs/plans/continuo_design.md#3-43`、`internal/orchestrator/restore.go` の `issueAgreesWithPath` | 90% |
| 45 | ステップ9 を検証ステップに変えたこと | 「pane と agent の一覧を取る」から「取れる」（VALIDATES THAT）に変えた | **一覧を1回取れないだけで、生きている pane を持つ run が全件『pane が無い』経路へ流れていた。**その経路は pane を閉じないだけで、**Status の書き換えと worktree の片付けは行う** | `docs/plans/continuo_design.md#3-44`、`internal/orchestrator/restore.go` の `matchPanes` | 95% |
| 46 | フロー `一覧の取得の失敗` を `paneの不在` と別にしたこと | 「判断を保留して次の巡回に委ねる」という別フローにした | **「pane が無いことを確かめられた」と「確かめられなかった」はまったく違う。**前者だけが `restart.orphan_running_action` を動かしてよい。混ぜると、herdr の一時障害1回で本番のボードが書き換わる | `docs/plans/continuo_design.md#3-44` | 95% |
| 47 | 新しい3つのフローで pane も worktree も消さないこと | どれも「候補から外す」「判断を保留する」で終え、ボードへ1バイトも書かない | **どちらが正しいか continuo には判断できない。**食い違いを見つけたこと自体は、消してよい根拠にならない | `docs/plans/continuo_design.md#3-4`（勝手に消さない）、`#3-43`、`#3-44` | 95% |
