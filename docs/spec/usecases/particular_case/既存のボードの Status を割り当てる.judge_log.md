# 判断ログ: 既存のボードの Status を割り当てる

- 対象: docs/spec/usecases/particular_case/既存のボードの Status を割り当てる.rucm.md
- 作成日 / 作成モデル: 2026-08-20 / Claude Opus 5 (1M context)
- 参照した根拠資料: docs/plans/continuo_design.md#3-6、docs/plans/continuo_design.md#3-32、docs/plans/continuo_design.md#3-34、docs/plans/continuo_design.md#4-1、docs/plans/continuo_design.md#5-1、docs/plans/continuo_design.md#5-2、docs/trying_it_out.md、internal/scaffold/scaffold.go、internal/scaffold/detect.go、internal/scaffold/template.go、internal/doctor/checks.go、CLAUDE.md

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | USE CASE NAME | 既存のボードの Status を割り当てる | 依頼で指定された名前をそのまま使う。動詞で終わる名詞句の規則を満たす | 依頼文 | 100% |
| 2 | 配置先ディレクトリ | particular_case | 単一目的・単一操作の単位である。他ユースケースを跨がない | 依頼文 | 100% |
| 3 | BRIEF DESCRIPTION | 実行・選択肢の列挙・5役割の逐次選択・WORKFLOW.md への書き出しの4文 | 基本フローの骨格を4文に落とした。単文のみ（R12）を守るため文を分けた | - | 90% |
| 4 | PRECONDITION | gh のログイン済み、ボードが1枚ある、Status が single-select である | doctor が `gh auth status` の scope に `project` を求め、Bootstrap が single-select の Status フィールドを解決する。この3つが無いと選択肢を1件も読めない | docs/plans/continuo_design.md#3-32、internal/doctor/checks.go の `checkGHAuth` と `checkBoard` | 90% |
| 5 | PRIMARY ACTOR | 利用者 | ボードを既に持っている人が自分で実行するコマンドである。continuo の常駐プロセスは関与しない | 依頼文 | 95% |
| 6 | SECONDARY ACTORS | GitHub Projects v2 | 選択肢の一覧はボードから取る。システムが外部に依存する唯一の相手である | docs/plans/continuo_design.md#3-34 | 90% |
| 7 | DEPENDENCY | なし | 他の particular_case を取り込まない。`continuo doctor` は別の独立したコマンドであり、この操作の途中では呼ばない | docs/plans/continuo_design.md#3-32 | 85% |
| 8 | GENERALIZATION | なし | 汎化元・汎化先にあたるユースケースが存在しない | - | 90% |
| 9 | 実行するコマンド | `continuo setup` | **人間が決めた。**Status 割り当ての対話は独立したサブコマンドにする。`continuo init --assign-status` にはしない。理由は「対話するコマンドを1つに独立させ、`init` が自動化から叩ける状態を保つため」である。実装の都合で決めてはならない | 人間の決定（2026-08-20） | 100% |
| 10 | 設計の「対話で選ばせない」との整合 | 標準入力を握るのは `continuo setup` だけである。`continuo init` は対話しない | 設計が `init` の対話を禁じた理由は「標準入力を握ると `continuo init` を自動で叩く経路が止まる」ことである。対話を別のコマンドへ切り出せば、`init` は標準入力を1度も握らない | docs/plans/continuo_design.md#3-32、人間の決定（2026-08-20） | 100% |
| 11 | 役割の説明を先に出す方針 | ステップ8で役割の説明を応答してからステップ9で番号を受ける | 初見の利用者はどの Status がどの役割か知らない。Status 名を先に見せると、名前の似た選択肢を役割の意味と無関係に選ぶ | 依頼文 | 95% |
| 12 | 画面に出す説明の文面 | 「continuo はここから issue を取ります」等の5文 | 設計の状態遷移表にある「誰が・いつ・何をするか」を1文へ写した。Status 名ではなく continuo の振る舞いで説明する | docs/plans/continuo_design.md#4-1 | 85% |
| 13 | 選ばせ方 | 既存の選択肢を番号付きで並べ、番号を受ける | 選択肢名は空白を含みうる（`Ice Box` / `In Progress`）ので、名前の打ち込みは打ち間違いと大文字小文字の揺れを生む | docs/plans/continuo_design.md#3-34 | 90% |
| 14 | 尋ねる順序 | 着手待ち、作業中、レビュー待ち、保留、完了 | issue が実際に通る順である。ボード上の並びと同じ順に尋ねると、利用者が一覧を上から順に消化できる | docs/plans/continuo_design.md#4-1 | 85% |
| 15 | 5つの役割と設定キーの対応 | 着手待ちは `dispatch_state`、作業中は `running_state`、レビュー待ちは `status_signal_map.review`、保留は `failure_state` と `status_signal_map.blocked`、完了は `terminal_states` | 設定の各キーのコメントが役割と1対1に対応している。`active_states` は `dispatch_state` と `running_state` を必ず含める規則があるので、この2つから機械的に組み立てる | docs/plans/continuo_design.md#5-2、internal/scaffold/template.go | 90% |
| 16 | ステップ分割方針 | 検証・応答・入力・状態変更をそれぞれ独立したステップにする | 1文1動作（R4）と、代替フローの分岐元が条件ステップであること（E021）を両立させるため。応答と入力受付を1ステップに畳むと、分岐点が指せなくなる | rucm 文法リファレンスの R4 と E021 | 90% |
| 17 | ステップ2の VALIDATES THAT | WORKFLOW.md が無いか `--force` が渡されている | `WriteTemplateWithValues` が既存ファイルを見つけると `ErrAlreadyExists` を返し、`--force` のときだけ上書きする。検証を1つにまとめると、既存の実装と分岐が1対1になる | internal/scaffold/scaffold.go の `WriteTemplateWithValues` と `ErrAlreadyExists`、docs/trying_it_out.md | 90% |
| 18 | 検証の順序（先にファイル、後にボード） | ステップ2でファイル、ステップ4でボード | ボードの読み取りは GitHub への往復とレートリミットを消費する。上書きできずにどうせ止まる実行で、先に外部を叩く理由が無い | docs/plans/continuo_design.md#3-31 | 85% |
| 19 | ステップ3（owner とボード番号の取得） | gh から引く。1ステップにまとめる | `continuo init` が既に `gh api user` と `gh project list` で両方を引いている。`continuo setup` でも同じ検出をそのまま使う | internal/scaffold/detect.go の `Detect`、`detectOwner`、`detectProject` | 90% |
| 20 | ステップ4の VALIDATES THAT | ボードの Status フィールドの選択肢を読み取れる | doctor の「ボードを読めるか」と同じ検査である。ここが通らないと選択肢を1件も並べられず、対話に入る意味が無い | docs/plans/continuo_design.md#3-32、internal/doctor/checks.go の `checkBoard` | 90% |
| 21 | ステップ5の VALIDATES THAT | 読み取った選択肢が5個以上ある | 5つの役割それぞれに別の選択肢を割り当てる（判断25）ので、選択肢が5個未満なら対話は必ず途中で行き止まる。尋ねる前に止めれば、利用者は無駄な入力をしない | 判断25と同じ根拠 | 65% |
| 22 | DO と UNTIL を使う理由 | 同じ問い方を5回繰り返すため | 5つの役割で問い方と検証がまったく同じである。5回展開するとステップが5倍になり、代替フローの RFS も5組必要になる | rucm 文法リファレンスの R23 | 90% |
| 23 | ステップ10の VALIDATES THAT | 番号が一覧の範囲内である | 番号入力は範囲外の数値と数値以外の両方を受け取りうる。受け付けない入力で対話全体を打ち切ると、それまでの回答が失われる | - | 85% |
| 24 | ステップ11の VALIDATES THAT（番号が 0 でない） | 番号 `0` を「この役割に使える選択肢がボードに無い」の入力にする | 役割に対応する選択肢が無い場合の逃げ道が要る。一覧の番号は1から振るので、`0` は既存の選択肢と衝突しない | - | 60% |
| 25 | ステップ12の VALIDATES THAT（二重割り当て） | 同じ選択肢を2つの役割へ割り当てさせない。拒否して同じ役割を尋ね直す | 役割が重なると continuo が壊れる。着手待ちと完了が同じなら、取った直後の issue の worktree を片付ける。着手待ちと作業中が同じなら、書き込んだ Status がそのまま次の候補になり同じ issue を取り続ける | docs/plans/continuo_design.md#3-9、docs/plans/continuo_design.md#4-1 | 85% |
| 26 | 二重割り当てのときに打ち切らない理由 | RESUME STEP 8 で同じ役割を尋ね直す | 打ち間違いは利用者が即座に直せる。打ち切ると、それまでの回答をすべて入れ直させることになる | - | 90% |
| 27 | 該当する選択肢が無いときに ABORT する理由 | 対話を打ち切り、GitHub の画面で選択肢を足すよう案内する | 5つの役割はどれも continuo の動作に必要で、欠けたまま書いた WORKFLOW.md は起動時の検証で落ちる。選択肢を API で足すことは禁じられている（`updateProjectV2Field` は全件置き換えとして扱われ、設定済みの Status が全部消える）ので、システム側で補うこともできない | CLAUDE.md、docs/plans/continuo_design.md#4-1、docs/plans/continuo_design.md#3-34 | 85% |
| 28 | 選択肢を足す案内に「API で足すと Status が全部消える」を含める | 含める（`選択肢が足りない` と `該当する選択肢が無い` の両フロー） | この警告が無いと、利用者は `gh project field-create` や API で足そうとする。本番のボードでは設定済みの Status が全部 `null` に落ちる | CLAUDE.md、docs/plans/continuo_design.md#4-1 | 90% |
| 29 | ステップ16（書き出し）を1ステップにする | WORKFLOW.md 全体を1回で書く | `WriteTemplateWithValues` が雛形の全文を1回で書き出す。途中まで書いたファイルを残さない | internal/scaffold/scaffold.go の `WriteTemplateWithValues` | 90% |
| 30 | BASIC FLOW の POSTCONDITION | WORKFLOW.md がある。5つの役割それぞれに1つの選択肢が書かれている。同じ選択肢が2つの役割に書かれていない。ボードの選択肢と item の Status は変わっていない | ボードを1文字も書き換えないことが、このユースケースの安全性の中心である。事後条件に書かないとテストで確かめられない | CLAUDE.md、docs/plans/continuo_design.md#3-34 | 90% |
| 31 | フロー `上書きの許可が無い` | RFS BASIC FLOW 2。案内して ABORT。終了コード 1 | 既存の `continuo init` が同じ状況で終了コード 1 を返し、`--force` を案内する。`continuo setup` も同じ挙動に揃え、`--force` の旗名も合わせる | docs/trying_it_out.md、internal/scaffold/scaffold.go の `ErrAlreadyExists` | 90% |
| 32 | フロー `上書きの許可が無い` の POSTCONDITION | 中身は変わっていない。役割の割り当てを1つも尋ねていない。終了コードは 1 | 「尋ねていない」を書くのは、検証の順序（判断18）が守られたことをテストで確かめるためである | - | 85% |
| 33 | フロー `ボードを読めない` | RFS BASIC FLOW 4。理由と直し方を応答して ABORT | doctor が落ち方ごとに記号と文言を分けている。同じ理由の分類をこのコマンドでも使う | docs/plans/continuo_design.md#3-32、internal/doctor/checks.go の `boardFailure` | 85% |
| 34 | 読めない理由と案内の対応 | 候補が複数なら `--project`、owner を引けないなら `--owner`、scope 不足なら `gh auth login -s project`、レートリミットなら時間をおく | `Detect` が候補複数のときは選ばせずに `--project` での再実行を案内する既存の挙動に合わせた | internal/scaffold/detect.go、docs/plans/continuo_design.md#3-32 | 80% |
| 35 | Status フィールドが見つからないときの案内 | `--status-field <名前>` でフィールド名を渡す | `status_field` は WORKFLOW.md の設定だが、このコマンドは WORKFLOW.md を作る側なので設定から引けない。フィールド名を渡す口が要る | docs/plans/continuo_design.md#3-34 | 50% |
| 36 | フロー `ボードを読めない` の POSTCONDITION | WORKFLOW.md は作られていない。役割の割り当てを1つも尋ねていない。終了コードは 1 | 対話に入る前に落ちるので、部分的な成果物も入力も残らない | - | 90% |
| 37 | フロー `選択肢が足りない` | RFS BASIC FLOW 5。手順を応答して ABORT | 判断21と同じ。組み込みの Status は `Todo` / `In Progress` / `Done` の3つで始まるので、新しいボードでは実際に起こる | docs/plans/continuo_design.md#3-34 | 80% |
| 38 | フロー `選択肢が足りない` の POSTCONDITION | WORKFLOW.md は作られていない。ボードの選択肢は変わっていない。役割の割り当てを1つも尋ねていない。終了コードは 1 | 足りないと言うだけで、システムが選択肢を足さないことを明示する | CLAUDE.md | 90% |
| 39 | フロー `番号が範囲外` | RFS BASIC FLOW 10。範囲を応答して RESUME STEP 8 | 打ち間違いで対話全体を捨てさせない。復帰先を8にすると、同じ役割の説明からやり直す | - | 85% |
| 40 | フロー `番号が範囲外` の POSTCONDITION | 割り当ては増えていない。同じ役割の番号をもう一度待っている | 不正な入力で内部状態が進まないことを、テストで確かめられる形にした | - | 85% |
| 41 | フロー `該当する選択肢が無い` | RFS BASIC FLOW 11。手順と警告を応答して ABORT | 判断27と同じ | CLAUDE.md、docs/plans/continuo_design.md#4-1 | 85% |
| 42 | フロー `該当する選択肢が無い` の POSTCONDITION | WORKFLOW.md は作られていない。ボードの選択肢は変わっていない。それまでに選んだ番号は保存されていない。終了コードは 1 | 途中まで選んだ結果を保存すると、次回の実行が「どこまで決まっているか」を持ち越すことになり、状態の置き場所を1つ増やす。設計は状態を永続化しない方針である | docs/plans/continuo_design.md#3-4 | 80% |
| 43 | フロー `二重割り当て` | RFS BASIC FLOW 12。割り当て済みの役割の名前を応答して RESUME STEP 8 | どの役割と衝突したかを出さないと、利用者はどれを選び直せばよいか分からない | - | 85% |
| 44 | フロー `二重割り当て` の POSTCONDITION | 割り当ては増えていない。1つの選択肢は1つの役割だけに割り当てられている。同じ役割の番号をもう一度待っている | 判断25の不変条件を、この代替フローを通っても壊さないことの表明である | - | 85% |
| 45 | フロー `中断` の WHEN | 利用者が Ctrl+C を入力した場合 | 対話中に利用者が抜ける手段は端末の割り込みである。依頼で明示された条件をそのまま使う | 依頼文 | 95% |
| 46 | フロー `中断` の BRANCH FROM | BASIC FLOW 15（割り当ての一覧を応答するステップ） | 最悪のタイミングを1点選ぶ規則に従った。15 は5つの回答をすべて集め終えた直後であり、利用者の入力が最大量失われる点である。同時に、唯一の副作用である WORKFLOW.md への書き出し（16）の直前でもある。ステップ16 より前に置いたのは、書き出しが1回の書き込みで完結し（判断29）、途中で割り込んでも部分ファイルが残らないためである | rucm 文法リファレンスの BRANCH FROM 選定基準、internal/scaffold/scaffold.go の `WriteTemplateWithValues` | 80% |
| 47 | フロー `中断` の POSTCONDITION | WORKFLOW.md は作られていない。5つの役割の割り当ては保存されていない。ボードの選択肢と item の Status は変わっていない | 中断しても外部にも手元にも痕跡を残さないことを明示する。ボードを触らないことは他フローと同じく必ず書く | CLAUDE.md | 90% |
| 48 | 代替フローに ABORT と RESUME STEP のどちらを使うか | 入力の打ち間違いは RESUME STEP、前提が欠けているものは ABORT | 利用者が同じ画面で直せるものは尋ね直し、GitHub の画面での作業やコマンドの引数が要るものは打ち切る、という基準で揃えた | - | 85% |
| 49 | mermaid フローチャート | 基本フロー17ステップと7本の代替フローを subgraph で分けて表現する | 代替フローの復帰先（RESUME STEP 8）と打ち切りを線で追えるようにした。rucm ブロックの分岐をすべて写している | rucm 文法リファレンスのファイル規約 | 90% |
| 50 | mermaid シーケンス図 | 利用者、システム、GitHub Projects v2 の3者。5回の loop と alt で分岐を表現する | 副アクターへの要求（選択肢の取得）と、対話が5回繰り返されることを図の上で見せるため | rucm 文法リファレンスのファイル規約 | 90% |
