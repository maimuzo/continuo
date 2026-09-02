# 判断ログ: issue とコードが別のリポジトリにある issue に着手する

- 対象: `docs/spec/usecases/particular_case/issue とコードが別のリポジトリにある issue に着手する.rucm.md`
- 作成日 / 作成モデル: 2026-09-02 / Claude Opus 5 (1M context)
- 参照した根拠資料: `docs/plans/impl/issue144_branch_and_push.md`（1 / 7 / 7b / 8 / 8b / 8b2 / 8c / 8c2 / 11c / 11d / 11f / 12b / 13b）、`docs/plans/continuo_design.md`（3-22 / 3-49 / 4-3）、`internal/workspace/layout.go`、`internal/workspace/broken.go`、`internal/workspace/issuebranch.go`、`internal/orchestrator/restore.go`、`internal/orchestrator/coderepo.go`、`internal/abandon/abandon.go`

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | USE CASE NAME | issue とコードが別のリポジトリにある issue に着手する | 依頼で指定された名前をそのまま使った | 依頼文 | 100% |
| 2 | 「fork」という語を PRECONDITION に残したこと | 残した | 利用者が起票時に使った語である。**訳すと同じものに名前が2つできる** | #144（worktree の branch は変えず push 先だけ分ける）の2026-09-01 のコメント、`.claude/rules/reporting.md` | 95% |
| 3 | SECONDARY ACTORS にエージェントを入れたこと | 入れた | **push するのも PR を読むのもエージェントであり、システムではない。**その行為者をシステムに書くと、実在しない自動化を記述することになる | 設計 7b、`docs/plans/continuo_design.md#5-3b` | 80% |
| 3b | **PR を作る**ステップを書かなかったこと | 書かない | **設計 5-3b が「エージェントに PR を作らせない」で凍結している。**この形でも解いていないので、書くと実装より先の仕様になる。**PR を作るのは人間である**ことを説明の節に明記した | `docs/plans/continuo_design.md#5-3b`、設計 14（人間の判断待ち） | 90% |
| 4 | ステップ3 と 4 を分けたこと | コードのリポジトリと PR の宛先を別ステップにした | R4（1文1動作）。**取り出す値が違う**（`nameWithOwner` と `parent.nameWithOwner`） | rucm スキルの R4、`internal/tracker/coderepo.go` | 90% |
| 5 | ステップ5（VALIDATES THAT）の相手 | **コードのリポジトリ**の信頼登録 | 信頼の鍵は clone の実体のパスであり、Claude Code が開くのはコードのリポジトリの worktree である。**issue のリポジトリでは1行も実行しない** | 設計 13b、`docs/plans/continuo_design.md#4-3` | 95% |
| 6 | ステップ6 と 7 を分けたこと | 置き場所とディレクトリ名を別ステップにした | **この2つが別の出どころから来ることが、このユースケースの中心である。**1文にまとめると、両方が同じリポジトリから来ると読める | 設計 8 | 90% |
| 7 | ステップ8（VALIDATES THAT） | コードのリポジトリの clone が手元にある | 実装は `ghq` に引かせる相手をコードのリポジトリに変える。**issue のリポジトリの clone は探しに行かない** | 設計 13b、`internal/workspace/prepare.go` | 95% |
| 8 | ステップ14・15（PR を引き直す）を入れたこと | 入れた | **別のリポジトリの PR は issue に紐づかない。**この2行が無いと、upstream からの指摘を読む経路が仕様から抜ける。**引く（14）と読む（15）を分けたのは R4（1文1動作）による** | 設計 7b / 12b | 85% |
| 9 | POSTCONDITION に「issue のリポジトリにはコメント以外を1バイトも書き込んでいない」を書いたこと | 書いた | **コメントと Status 以外は書かない**ことが、この形の安全性の要である。書かないと、fork に置いた成果が issue の側へ漏れる実装も許してしまう | 設計 7 / 7b | 80% |
| 10 | 代替フロー コードのリポジトリが未信頼 の本文 | **コードのリポジトリの名前**を出す | 出さないと「issue のリポジトリが信頼登録されていません」という**間違った直し方**が人間に届く | 設計 13b、`internal/orchestrator/dispatch.go` の `noteUntrusted` | 95% |
| 11 | 未信頼 の POSTCONDITION に「二度書かれない」を書いたこと | 書いた | 通知の鍵をコードのリポジトリで持つ。**issue のリポジトリで持つと、同じ issue のリポジトリに属する別々の fork の未信頼が1つに潰れ、2つ目が通知されない** | 設計 13b | 90% |
| 12 | 未信頼 の終端 | ABORT | 人間が `continuo trust` を叩くまで進めない。**このユースケースは目的を果たさずに終わる** | rucm スキルの R24 | 95% |
| 13 | 代替フロー 置き場所とリンクの食い違い の種別 | GLOBAL ALTERNATIVE FLOW | 分岐元のステップ6は `VALIDATES THAT` を持たない。**特定代替フローの `RFS` は条件ステップしか指せない** | rucm スキルのフロー種別の表 | 85% |
| 14 | 置き場所とリンクの食い違い の終端 | ABORT | **人間が Development のリンクを戻すか worktree を手で片付けるまで進めない。**候補から外し続けるだけなので、本流へ戻る道が無い | rucm スキルの R24、設計 11f | 90% |
| 15 | 置き場所とリンクの食い違い で「消さない」を2回書いたこと | 本文と POSTCONDITION の両方に書いた | **読む人がまず知りたいのはそこである。**食い違いは書き換えの跡とは限らず、人間がリンクを張り替えただけかもしれない | 設計 8b / 11f | 90% |
| 16 | 代替フロー 身元ファイルの復元 の分岐元 | `BASIC FLOW 2` | 復元は巡回の中で、着手の判断より前に走る。**ステップ2（issue を選ぶ）の時点が、置き場所を走査し終えている最初の点である** | `docs/plans/continuo_design.md#3-49`、`internal/orchestrator/restore.go` | 70% |
| 17 | 身元ファイルの復元 の終端 | **RESUME STEP 2** | **これは失敗ではない。**身元ファイルを書き直せたので、その worktree は次からふつうの候補になる。**ABORT にすると、復元したのに何も起きないと読める** | rucm スキルの R25 | 85% |
| 18 | 復元で確かめることを2つに分けたこと | ディレクトリ名の一致と、コードのリポジトリの一致 | **落ち方が違うので、代替フローも2本になる。**1つにまとめると、どちらが合わなかったのかを応答に書けない | 設計 8c、`internal/orchestrator/restore.go` の `slugAgrees` | 85% |
| 19 | ディレクトリ名が合わない / コードのリポジトリが合わない の終端 | 両方 ABORT | **どちらも「手掛かりが嘘だった」ということである。**pane の label は herdr の CLI から誰でも書き換えられるので、**書き直しに進んではならない** | rucm スキルの R24、設計 8c | 90% |
| 20 | 代替フロー 手掛かりが1つも無い の終端 | ABORT | **復元も報告もできないが、起動は止めない。**ABORT はユースケースの終わりであって、システムの停止ではない | rucm スキルの R24、設計 8c2 | 75% |
| 21 | 手掛かりが1つも無い で「壊れたものに数えない」を書いたこと | 書いた | 数えると `workspace.on_broken_worktree` の既定（`stop`）で **continuo が起動しなくなる。**数えない判断そのものが仕様である | 設計 8c2 | 95% |
| 22 | 手掛かりが1つも無い の POSTCONDITION に「報告にも復元にも出ない」を書いたこと | 書いた | **残る帰結を隠さない。**書かないと、利用者は「そのうち報告される」と思って待つ | 設計 8c2 | 90% |
| 23 | `continuo abandon` を RUCM のフローに入れなかったこと | 入れず、説明の節にだけ書いた | **abandon は人間が叩く別のユースケースである**（`docs/spec/usecases/particular_case/着手を取り消す.rucm.md` が既にある）。ここに入れると、2つのユースケースが1つのファイルに混ざる | 既存の RUCM のファイル構成 | 85% |
| 24 | abandon がカンバンを引かないことを説明の節に書いたこと | 書いた | **この形で変わるのは「照合の相手」と「clone を引く相手」だけである。**変わらないことも書かないと、実装が引きに行く形へ流れる | 設計 8b2、`internal/abandon/abandon.go` | 90% |
| 25 | 置き場所を5階層にしなかったこと | 4階層のまま | **走査の深さと gwq との互換が4階層に依存している。**5階層目を足すと、既にある worktree が全部「置き場所の規則に合わない」になる | 設計 8 / 15、`internal/workspace/scan.go` の `scanDepth` | 95% |
| 26 | 身元ファイルの3つの値を「人間だけが読む」と書いたこと | 書いた | **身元ファイルはエージェントが書き換えられる。**判断に使うと、書き換えるだけで候補から外せてしまう。**候補を絞る手掛かりにも使わない** | 設計 8b / 11c | 95% |
| 27 | 「pane の label」を復元の唯一の手掛かりにしたこと | そうした | 置き場所の2・3階層目はコードのリポジトリなので、**ディレクトリ名から issue の番号を切り出せない。**label は worktree の外にあり、herdr が持っている | 設計 8c | 85% |
| 28 | 実装が label を持たないときに置き場所の owner/repo で試すこと | 説明の節にも RUCM にも書かなかった | **それは「issue とコードが同じリポジトリ」の場合の話であり、このユースケースの前提の外である。**このファイルに書くと、cross-repo でも番号を切り出せると読める | 設計 8c、`internal/workspace/broken.go` の `PathClueOf` | 65% |
| 29 | ABORT と RESUME STEP の使い分けの基準 | **人間の操作を待たずに本流へ戻れるなら RESUME STEP、待つなら ABORT** | この文書で RESUME STEP は「身元ファイルの復元」1本だけである。**残り5本は、人間が信頼登録・clone・リンク・worktree のどれかを直すまで進めない** | rucm スキルの R24 / R25 | 80% |
| 30 | 用語の統一 | 「コードのリポジトリ」「PR の宛先」「カンバン」「fork」「worktree」「clone」を固定した | 「upstream」と「派生元」を混ぜないため、RUCM の本文では「PR の宛先」に統一した（GraphQL の `parent` を指す） | `.claude/rules/reporting.md`、設計 1 | 85% |
| 31 | mermaid 2ブロックの内容 | flowchart に6本の終端を、sequenceDiagram に5者のやり取りを写した | **エージェントが push と PR の読み取りの行為者であることを、sequenceDiagram で分けて見せる**必要がある。**PR を作るのは人間である**ことも図に注記した | 設計 7b、`docs/plans/continuo_design.md#5-3b` | 85% |
| 32 | 実装との突き合わせ | 全ステップを実装の関数と対応づけた | ステップ3・4 は `resolveCodeRepo`、ステップ5 は `CheckTrust`、ステップ6・7 は `Locate`、ステップ8 は `ghqList`、復元は `recoveryClues` と `slugAgrees`、食い違いは `issueAgreesWithPath` である | 根拠資料の各ファイル | 90% |
