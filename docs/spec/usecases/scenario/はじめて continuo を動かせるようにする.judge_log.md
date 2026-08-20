# 判断ログ: はじめて continuo を動かせるようにする

- 対象: docs/spec/usecases/scenario/はじめて continuo を動かせるようにする.rucm.md
- 作成日 / 作成モデル: 2026-08-20 / Claude Opus 5 (1M context)
- 参照した根拠資料: docs/plans/continuo_design.md（3-6 / 3-16 / 3-17 / 3-18 / 3-22 / 3-32 / 3-32b / 3-33 / 3-34 / 5-1 / 5-2）、docs/trying_it_out.md、docs/spec/usecases/particular_case/ の5本、internal/scaffold/、internal/doctor/、CLAUDE.md

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | USE CASE NAME | はじめて continuo を動かせるようにする | 依頼で指定された名前をそのまま使う。動詞で終わる名詞句であり、ファイル名の識別子としても成立する | 依頼文 | 100% |
| 2 | 配置先ディレクトリ | scenario/ | 5本の particular_case を跨ぐ時系列の目的達成であり、単一操作単位ではない | rucm スキルの粒度ガイド | 100% |
| 3 | BRIEF DESCRIPTION | 導入から最初の issue の dispatch までを1段落で要約し、その中で「システム」の実体を定義する | scenario の主アクターは利用者だが、ビルド・gh の実行・continuo の常駐が1つの「システム」に混在する。何を指すかを最初に固定しないと各ステップの主語が読めない | docs/trying_it_out.md、docs/spec/usecases/particular_case/ボードを新規に用意する.rucm.md の BRIEF DESCRIPTION の書き方に揃えた | 85% |
| 4 | 「システム」の実体 | 利用者の手元のコマンド環境と continuo の実行ファイル | ビルド・`gh`・`ghq`・continuo 本体が同じ端末で動く。particular_case「ボードを新規に用意する」が既に「システムは利用者の手元で gh コマンドを実行するコマンド環境である」と定義しており、呼び方を揃えた | docs/spec/usecases/particular_case/ボードを新規に用意する.rucm.md | 85% |
| 5 | PRECONDITION | GitHub のアカウント、OS が macOS または Linux、herdr の待ち受け、gh と ghq と git と Go の導入、Claude Code の導入 | OS の限定は設計が Windows ネイティブ非対応を明記している。herdr・gh・ghq・Claude Code は doctor の7項目が実在を前提にしている。Go は段1 のビルドに要る | docs/plans/continuo_design.md#3-32b、docs/plans/continuo_design.md#3-32、docs/trying_it_out.md | 90% |
| 6 | PRIMARY ACTOR | 利用者 | 導入作業を開始するのは人間である。continuo は起動されて動く側であり、開始者ではない | docs/trying_it_out.md | 95% |
| 7 | SECONDARY ACTORS | GitHub、herdr、Claude Code、gh、ghq | doctor の検査項目がこの5つに対応する。GitHub Projects v2 と GitHub issue は同じ GitHub なので1つにまとめた | docs/plans/continuo_design.md#3-32 の doctor の検査表 | 85% |
| 8 | DEPENDENCY | 5本すべてを INCLUDE する | 依頼で指定された5本を1本の scenario に凝集させる方針。rucm スキルは「1つの scenario になるべく多くの particular_case をまとめる」ことを高価値とする | 依頼文、rucm スキルの scenario の設計方針 | 100% |
| 9 | GENERALIZATION | なし | 汎化関係にある上位ユースケースは存在しない | - | 95% |
| 10 | ボードの有無の表現 | 基本フローの `IF-THEN-ELSE-ENDIF`（ステップ 4-11） | どちらの分岐も正常系であり、エラー分岐ではない。RUCM は「どの分岐でも正常」の内部分岐を基本フロー内の IF で書くことを認めている | rucm_structure.md の「基本フローに書くこと」の例外規定 | 90% |
| 11 | 新規ボードの側の並び | ボードを新規に用意する → 設定ファイルを作る → status_field の書き換え | 設計は新規作成時に組み込みの `Status` に触らず `continuo Status` を別に作らせる。`continuo init` の雛形の `status_field` の既定は `Status` なので、作ったフィールド名へ書き換える段が要る | docs/plans/continuo_design.md#3-34、docs/plans/continuo_design.md#5-2、docs/trying_it_out.md | 90% |
| 12 | 既存ボードの側に「設定ファイルを作る」を置かない | 「既存のボードの Status を割り当てる」1本だけにした | その particular_case の事後条件が「WORKFLOW.md がある。5つの役割それぞれに1つの選択肢が書かれている」であり、WORKFLOW.md の生成を内部に含む。重ねて INCLUDE すると既存ファイルの上書き分岐に落ちる | docs/spec/usecases/particular_case/既存のボードの Status を割り当てる.rucm.md の POSTCONDITION | 85% |
| 13 | 既存ボードの側の入口の書き方 | scenario では入口のコマンド名を書かず INCLUDE だけにした | 割り当ての入口は**人間の決定で `continuo setup` という独立したコマンドに確定している**（2026-08-20）。ただし `cmd/continuo` にはまだサブコマンドの実装が無い。scenario 側にコマンド名を書くと同じ名前が2箇所に増え、片方だけが古くなる。INCLUDE なら particular_case の側だけを直せばよい | 人間の決定（2026-08-20）、cmd/continuo、docs/spec/usecases/particular_case/既存のボードの Status を割り当てる.rucm.md | 90% |
| 14 | clone の取得を直接ステップにした（12-13） | INCLUDE せず2ステップで書いた | 「対象リポジトリを信頼登録する」の基本フローは clone が既にあることを検証から始める。clone を置く行為はその前段であり、対応する particular_case が無い。`ghq get` と `ghq list -p -e` の出力有無でテストできる粒度がある | docs/spec/usecases/particular_case/対象リポジトリを信頼登録する.rucm.md、docs/trying_it_out.md | 85% |
| 15 | issue の作成とボードへの追加を直接ステップにした（14-20） | INCLUDE せず7ステップで書いた | 対応する particular_case が無い。かつ「最初の issue が動き出す」ためには着手待ちの issue が1件必要で、ここを省くと後続の dispatch が成立しない。issue の URL・item の実在・Status の値でテストできる | docs/trying_it_out.md の段6 | 85% |
| 16 | Status の変更をシステム経由で書いた（19-20） | 利用者がシステムに要求し、システムがボードへ書く形にした | R3 がアクター同士の直接相互作用を禁じる。手順書では GitHub の画面での操作として案内されるが、RUCM ではシステムを介す形に直す必要がある | rucm_structure.md の R3、docs/trying_it_out.md の段6 | 70% |
| 17 | 信頼登録を検査より前に置いた（21 → 22） | 「対象リポジトリを信頼登録する」を「前提が揃っているかを検査する」より先に INCLUDE した | 初回は必ず未承認なので、検査を先に置くと検査の基本フローが成立せず代替フローに落ちる。INCLUDE 先の基本フローが通る順序に並べた | docs/spec/usecases/particular_case/前提が揃っているかを検査する.rucm.md の POSTCONDITION、docs/plans/continuo_design.md#3-33 | 80% |
| 18 | 常駐後の dispatch を直接ステップにした（24-40） | INCLUDE せず17ステップで書いた | 対応する particular_case が無い。かつ設計が着手の段の順番を固定しており、順番そのものが検証対象になる。Status の書き込み・worktree・身元ファイル・pane・turn の各段でテストできる | docs/plans/continuo_design.md#3-16 | 90% |
| 19 | 着手の段の並び | 空きスロットの数え上げを省き、信頼の検査から始めた | 設計の段-1 は同時実行数の上限の話であり、はじめて動かす場面では実行中が0件で必ず通る。scenario の目的から外れる段を落とした | docs/plans/continuo_design.md#3-16 | 75% |
| 20 | ステップ分割方針 | 1ステップ1動作。要求と応答を分け、外部への要求と結果の書き込みを分けた | R4 と R15 が複合述語と連用中止形を禁じる。分けることで「どこまで進んで落ちたか」がステップ境界と一致し、設計の段ごとの残留物の表と対応が取れる | rucm_structure.md の R4 / R15、docs/plans/continuo_design.md#3-16 | 90% |
| 21 | VALIDATES THAT（2）Go の版 | Go の版が 1.26 以上である | 設計と手順書が `testing/synctest` の使用を理由に Go 1.26 以上を要求している。満たさないとビルドが通らず以降が全部進まない | docs/trying_it_out.md の段1 | 90% |
| 22 | VALIDATES THAT（23）検査結果 | 7件の見出し語に ✗ が1件もない | doctor は `✗` が1件でもあれば終了コード 1 を返す。`!` は「確かめられなかった」であって不足ではないので、条件を `✗` の不在に置いた | docs/plans/continuo_design.md#3-32 の記号と終了コードの表 | 95% |
| 23 | VALIDATES THAT（27）選択肢名 | 設定の active_states の選択肢名がボードにすべてある | 名前がずれると GraphQL がエラーを出さずに0件を返し、巡回が無言で止まる。設計は起動時にここで落とすと決めている | docs/plans/continuo_design.md#3-6、docs/plans/continuo_design.md#3-34 | 95% |
| 24 | VALIDATES THAT（29）信頼登録 | 最初の issue の対象リポジトリが Claude Code に信頼登録されている | 設計は dispatch の直前に issue ごとに検査すると決めている。未信頼のフォルダでは hook が1つも動かず turn の終了検知が全滅する | docs/plans/continuo_design.md#3-6 | 95% |
| 25 | VALIDATES THAT（38）agent_status | agent_status が idle または done である | 設計の段10 が idle と done の両方を合格とする。continuo は tab をフォーカスしないので実運用では done 側になる | docs/plans/continuo_design.md#3-16 | 90% |
| 26 | 基本フローの POSTCONDITION | 常駐・Status・worktree・身元ファイル・pane・turn の6点 | 「最初の issue が動き出す」の到達点を、外から検算できる状態だけで書いた。ボード・ファイルシステム・herdr のそれぞれで確認できる | docs/plans/continuo_design.md#3-16、docs/plans/continuo_design.md#3-18 | 85% |
| 27 | 代替フロー Goの版が古い | RFS BASIC FLOW 2、終端は ABORT | ビルドできなければ以降のどの段も実行できない。復帰先が無いので終了させた | docs/trying_it_out.md の段1 | 85% |
| 28 | 代替フロー 前提の不足 | RFS BASIC FLOW 23、終端は RESUME STEP 22 | doctor は直し方を出すので、直して再実行する往復が本来の使い方である。復帰先を検査の INCLUDE ステップにして再検査させた | docs/plans/continuo_design.md#3-32 | 90% |
| 29 | 代替フロー 選択肢名の不一致 | RFS BASIC FLOW 27、終端は ABORT | 設計が「1つでも失敗したら起動を止める」と決めている。常駐に入らないので復帰先が無い | docs/plans/continuo_design.md#3-6 | 95% |
| 30 | 代替フロー 未信頼のリポジトリ | RFS BASIC FLOW 29、終端は ABORT | 設計では continuo 自体は止まらずその issue だけを飛ばすが、この scenario の目的は最初の issue を動かすことなので、目的の不達成として終了させた。コメントは同じリポジトリにつき1回だけ書く規則をステップに含めた | docs/plans/continuo_design.md#3-6 | 75% |
| 31 | 代替フロー 確認の画面が出ている | RFS BASIC FLOW 38、終端は ABORT | 設計の段10 が blocked のとき esc を送ってから failure_state へ移すと決めている。turn を送ると本文が画面に食われて消えるため復帰させない | docs/plans/continuo_design.md#3-16 | 90% |
| 32 | 代替フローの名前 | Goの版が古い / 前提の不足 / 選択肢名の不一致 / 未信頼のリポジトリ / 確認の画面が出ている / 常駐の中断 | 内容を表す説明的名詞にし、空白・コロン・ハイフンを避ける規則に従った。既存の5本の particular_case と重複しない語を選んだ | rucm_structure.md のフロー名の付け方 | 90% |
| 33 | BRANCH FROM の分岐元 | BASIC FLOW 34（身元ファイルを書く段） | 最悪タイミングを取る規則に従った。設計は「ここまで来れば落ちても再起動後に身元が分かる」と身元ファイルを復帰の境目に置いている。直前の段33 で herdr の workspace と pane は既に実在するため、この段で落ちると「continuo が識別できない workspace が残る」状態になり、巻き戻しの手間が最大になる | docs/plans/continuo_design.md#3-16 の段6、docs/plans/continuo_design.md#3-18 | 80% |
| 34 | BRANCH FROM を1点に絞ったこと | 34 の1点だけ | 規則が原則1点と定める。Status を書く段31 の直後で落ちても、`In Progress` は active_states なので次の巡回で候補に戻り復帰できる。復帰できないのは身元ファイルが無い状態だけである | docs/plans/continuo_design.md#3-16 の「落ちた段」の表 | 80% |
| 35 | WHEN の条件 | 利用者が continuo を動かしている端末で Ctrl+C を入力する場合 | 手順書が停止手段を `Ctrl+C` と明記している。常駐中はどの段でも起こりうるので任意時点代替フローにした | docs/trying_it_out.md の段8 | 90% |
| 36 | 常駐の中断の終端 | ABORT | 手順書が「巡回を止め、hook の受け口を閉じ、turn の終わりを待ってから抜ける。pane は閉じない」と定める。プロセスが終わるので基本フローへ復帰しない | docs/trying_it_out.md の段8 | 90% |
| 37 | 常駐の中断の POSTCONDITION | Status は running_state のまま、worktree と workspace は残り、身元ファイルは無い | 分岐元を34 に置いたことの帰結をそのまま書いた。次回起動時に身元不明の workspace が残る状態を、事後条件として検算できる形にした | docs/plans/continuo_design.md#3-16、docs/plans/continuo_design.md#3-18 | 80% |
| 38 | 終端ステップ（40）の置き方 | システムが run の開始をログに応答する | 主アクターへの応答でフローを閉じる形にした。continuo は常駐プロセスで画面を持たないため、利用者に届く経路は標準エラーのログである | docs/trying_it_out.md の段7 | 75% |
| 39 | Status の名前の書き方 | 選択肢名を直接書かず dispatch_state / running_state / failure_state と設定キーで書いた | 設計はボードを既存のものに合わせる方針であり、選択肢名は設定で変わる。既定値を書くと、既定を変えたボードで読み違える | docs/plans/continuo_design.md#5-2、docs/plans/continuo_design.md#3-34 | 90% |
| 40 | 本番のボードを題材にしないこと | ボードの番号・owner を1つも書かなかった | project #3 は本番のボードで読み取りだけが許される。RUCM に具体値を書くと、下流のテスト生成が本番へ書き込む経路を作りうる | CLAUDE.md | 95% |
| 41 | mermaid フローチャート | 基本フロー40 ステップと6本の代替フローを全部描き、INCLUDE ステップはサブルーチン形状にした | rucm ブロックの全ステップ・全分岐を表現する規約に従った。INCLUDE を形で見分けられるようにした | rucm スキルのファイルテンプレート | 90% |
| 42 | mermaid シーケンス図 | 利用者・システム・GitHub・ghq・herdr・Claude Code の6者にし、ボードの有無を alt で描いた | SECONDARY ACTORS と一致させた。gh は「システムが GitHub へ要求する」経路に含めたので独立の参加者にしていない | rucm スキルのファイルテンプレート | 75% |
| 43 | この scenario に含めなかった particular_case | 無し（既存の5本すべてを取り込んだ） | 依頼で指定された5本が particular_case の全件であり、すべて INCLUDE した。第2の scenario を組む余剰は現時点で存在しない | docs/spec/usecases/particular_case/ の一覧 | 95% |
| 44 | RUCM 未化の疑いがある繋ぎ | clone の取得、issue の作成とボードへの掲載、continuo の常駐と最初の dispatch の3つ | いずれも対応する particular_case が無く、直接ステップで書いた。テスト可能な粒度は持たせたが、単独のユースケースとして切り出す価値があるかは人間の判断を要する | docs/spec/usecases/particular_case/ の一覧、docs/plans/continuo_design.md#3-16 | 65% |
