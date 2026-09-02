# 判断ログ: issue にリンクされた branch を起点にして着手する

- 対象: `docs/spec/usecases/particular_case/issue にリンクされた branch を起点にして着手する.rucm.md`
- 作成日 / 作成モデル: 2026-09-02 / Claude Opus 5 (1M context)
- 参照した根拠資料: `docs/plans/impl/issue144_branch_and_push.md`（1 / 5 / 10 / 10b / 11 / 11a / 11b / 11c / 11d / 11e）、`docs/plans/continuo_design.md`（3-22 / 3-34b / 3-68）、`internal/tracker/coderepo.go`、`internal/workspace/prepare.go`、`internal/workspace/git.go`、`internal/orchestrator/dispatch.go`、`internal/orchestrator/coderepo.go`

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | USE CASE NAME | issue にリンクされた branch を起点にして着手する | 依頼で指定された名前をそのまま使った。動詞で終わる名詞句の規則も満たす | 依頼文 | 100% |
| 2 | 配置ディレクトリ | `particular_case/` | 「1つの issue に着手する」という単一目的の単位であり、複数ユースケースを跨ぐ時系列ではない | rucm スキルの粒度ガイド | 95% |
| 3 | PRIMARY ACTOR | 巡回タイマー | 着手の起点は人間の操作ではなく巡回である。同じ判断が「worktree と branch を片付ける」でも採られている | `docs/spec/usecases/particular_case/worktree と branch を片付ける.rucm.md` | 95% |
| 4 | PRECONDITION に `herdr.worktree.base` は null と書いたこと | 前提に入れた | 設定に値が書いてあると、それがリンクより先に効く。**前提から外すと、基本フローが「設定を見る」から始まってしまい、このユースケースの主題がぼやける** | 設計 11a の段1、`internal/workspace/prepare.go` の `resolveBase` | 90% |
| 5 | ステップ3（VALIDATES THAT） | リンクされた branch が1つのリポジトリだけを指している | 別々のリポジトリを指す2本があると、どちらで作業すべきかを決められない。**偽ケースを代替フローで書くために条件ステップにした** | 設計 11a、`internal/tracker/coderepo.go` の `resolveCodeRepo` | 95% |
| 6 | ステップ3 に「窓に収まらない本数」も含めたこと | 同じ条件ステップに含めた | 実装は `totalCount` が窓を超えた場合も同じ扱い（決めない）にする。**別のステップに分けると、フローが2本に増えるのに終わり方が同じになる** | 設計 11a の表、`internal/tracker/coderepo.go` | 80% |
| 7 | ステップ4 と 5 の順番 | 置き場所を決める → clone を確かめる | 実装は `Locate` → 封じ込め検査 → `ghqList` の順である。**置き場所が決まらないと、どの clone を引くかも決まらない** | `internal/workspace/prepare.go` の `Prepare` | 90% |
| 8 | 通信を伴うステップを 6 の中に入れたこと | `IF` の中に置いた | 通信するのは「リンクがちょうど1本」のときだけである。**IF の外に置くと、リンクが0本の issue でも毎回通信すると読めてしまう** | 設計 10 の条件表 | 95% |
| 9 | ステップ7（VALIDATES THAT） | リモート追跡 ref を手元に用意できる | 「既にある」と「取ってきた」を1つの条件にまとめた。**実装も同じ形である**（あれば通信せず、無ければ取りに行く） | `internal/workspace/prepare.go` の `ensureLinkedBranch` | 90% |
| 10 | ステップ8 の base の形 | リンクされた branch の**リモート追跡 ref** | base はそのまま `git worktree add` の起点と差分の判定に渡る。**どちらもローカルに無い名前を解決できない**ので、`origin/` を付けた名前でなければならない | 設計 11a、`internal/workspace/git.go` の `gitNoDiffFromBase` | 95% |
| 11 | ステップ10 に「設定の値」を書かなかったこと | 既定 branch だけを書いた | 設定の値は任意時点代替フロー（設定のbaseが書いてある）で表す。**両方を基本フローに書くと、同じ分岐が2箇所に出る** | rucm スキルの R4 | 80% |
| 12 | ステップ13 と 14 を分けたこと | 身元ファイルとプロンプトを別ステップにした | R4（1文1動作）。**書き先が違う**（前者はディスク、後者はエージェントへの入力） | rucm スキルの R4 | 90% |
| 13 | ステップ14 の書き方 | 「リンクされた branch の名前を入れる」 | **「push 先を入れる」と書かなかった。**リンクは「どこから始めるか」であって「どこへ出すか」ではない。実装もプロンプトの中で候補としてしか出さない | 設計 11d / 14、`internal/orchestrator/prompt.go` | 90% |
| 14 | 代替フロー リンクが別々のリポジトリ の種別 | SPECIFIC ALTERNATIVE FLOW（`RFS BASIC FLOW 3`） | ステップ3の `VALIDATES THAT` が偽になる場合であり、偽ケースは RFS を持つ特定代替フローで書く決まりである | rucm スキルの厳密規則9 | 95% |
| 15 | リンクが別々のリポジトリ を **ABORT** にしたこと | ABORT | **人間が Development のリンクを直さない限り、次の巡回でも同じ結果になる。**RESUME STEP で戻すと「直っていないのに本流へ戻る」と読める。**このユースケースは目的を果たさずに終わる** | rucm スキルの R24、設計 11a | 90% |
| 16 | リンクが別々のリポジトリ に `IF`（3回・60秒）を書いたこと | 条件付きでコメントする | 実装は3回・60秒の条件を満たすまで書かない。**「1件コメントする」だけを書くと、毎巡回コメントが積まれる仕様に読める** | 設計 11e、`internal/orchestrator/coderepo.go` の `noteRepoIssue` | 90% |
| 17 | 代替フロー cloneが手元に無い に「通信を1度も行わない」を書いたこと | 事後条件と本文の両方に書いた | **clone が無い状態で fetch を叩く先が無い。**ここを書かないと、実装が先に通信してから落ちる形も許してしまう | `internal/workspace/prepare.go` の `Prepare` の順序 | 85% |
| 18 | 代替フロー リンクされたbranchを取ってこられない の終端 | ABORT | 回線か認証の問題であり、**その巡回では直らない。**利用者が直して Status を戻すまで進めない | 設計 10b、`docs/plans/continuo_design.md#3-34b` | 90% |
| 19 | 取ってこられない で Status を `failure_state` にすること | 書き換える | 3-34b が「判定できない事情は `failure_state` と issue のコメントで人間へ渡す」と決めている。**ログだけにすると、Status が動かないまま誰にも届かない** | `docs/plans/continuo_design.md#3-34b`、設計 10b | 95% |
| 20 | 取ってこられない で3回・60秒を待たないこと | その場で書く | **Status が動くこと自体が記録になる**ので、同じ issue で二度書かれることはない。待つ理由が無い | 設計 10b / 11e | 85% |
| 21 | やり直しを1回だけにしたこと | ステップ1に「もう一度だけ」と書いた | 実装は2回試して終わる。**回数を書かないと、無限に粘る実装も許してしまう** | 設計 10b、`internal/workspace/prepare.go` の `linkedBranchFetchAttempts` | 90% |
| 22 | 代替フロー 設定のbaseが書いてある の種別 | GLOBAL ALTERNATIVE FLOW | 分岐元のステップ6は `IF` であり `VALIDATES THAT` を持たない。**特定代替フローの `RFS` は条件ステップしか指せない**ので、GLOBAL を選んだ | rucm スキルのフロー種別の表 | 85% |
| 23 | 設定のbaseが書いてある の終端 | **RESUME STEP 12** | **これは失敗ではない。**base を別の方法で決めただけであり、そのあとは本流と同じ（worktree を切る）である。ABORT にすると、設定を書いた利用者が着手できないと読める | rucm スキルの R25 | 95% |
| 24 | 代替フロー 既存worktreeの再利用 の終端 | **RESUME STEP 13** | **これも失敗ではない。**worktree を作り直さないだけで、身元ファイルから先は本流と同じである。**復帰先を12 にすると、切り直すと読める** | rucm スキルの R25、`internal/workspace/prepare.go` の再利用の枝 | 85% |
| 25 | 既存worktreeの再利用 で base を決め直すこと | ステップ3に書いた | 実装も再利用の枝で `resolveBase` をもう一度呼ぶ。**書かないと、身元ファイルの base が空のまま残り、片付けが永久に見送る** | 設計 10、`internal/workspace/prepare.go` | 90% |
| 26 | ABORT と RESUME STEP の使い分けの基準 | **人間の操作を待たずに本流へ戻れるなら RESUME STEP、待つなら ABORT** | この文書では、設定の base と既存 worktree の再利用が RESUME STEP、リンクの食い違い・clone 不在・取得の失敗が ABORT である。**3つとも「人間が何かを直すまで進めない」ものである** | rucm スキルの R24 / R25 | 80% |
| 27 | 「別の branch を出している worktree」を書かなかったこと | このユースケースに入れない | **それは #142（worktree が別の branch を出していると永久に飛ばされる）の主題である。**ここに入れると、2つの issue の仕様が1つのファイルに混ざる | `docs/plans/impl/issue142_144_branch_mismatch.md` | 75% |
| 28 | 用語の統一 | 「リンクされた branch」「コードのリポジトリ」「カンバン」「worktree」「branch」「push」を固定した | プロジェクトの報告ルールが英語の技術用語を訳さないと決めている。「カンバン」は日本語で書くと決まっている | `.claude/rules/reporting.md`、`CLAUDE.md` | 95% |
| 29 | mermaid 2ブロックの内容 | flowchart に分岐と終端を、sequenceDiagram に4者のやり取りを写した | flowchart は `ABORT` と `RESUME STEP` の行き先を、sequenceDiagram は「どこへ要求を出すか」を表す。**通信するのが git だけであることを図で分けた** | 設計 10 | 85% |
| 30 | 実装との突き合わせ | 全ステップを実装の関数と対応づけた | ステップ3 は `resolveCodeRepo`、ステップ5 は `ghqList`、ステップ7 は `ensureLinkedBranch`、ステップ8 と10 は `resolveBase`、ステップ13 は `WriteIdentity`、ステップ14 は `renderFirstPrompt` である | 根拠資料の各ファイル | 90% |
