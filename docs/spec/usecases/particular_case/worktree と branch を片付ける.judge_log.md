<!-- 目的: 「worktree と branch を片付ける」の RUCM を書くときに AI が行った判断と、その根拠を記録する -->

# 判断ログ: worktree と branch を片付ける

- 対象: `docs/spec/usecases/particular_case/worktree と branch を片付ける.rucm.md`
- 作成日 / 作成モデル: 2026-08-20 / Claude Opus 5 (1M context)
- 参照した根拠資料: `docs/plans/continuo_design.md`（3-9 / 3-18 / 3-20 / 3-22 / 4-1 / 8-1）、`internal/workspace/cleanup.go`、`internal/workspace/sweep.go`、`internal/workspace/scan.go`、`internal/orchestrator/reconcile.go`、`internal/orchestrator/lifecycle.go`

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | USE CASE NAME | worktree と branch を片付ける | 依頼で指定された名前をそのまま使う。**`worktree` と `branch` は英語のまま書く**（日本語へ直訳すると同じものに名前が2つできる） | 依頼文、`.claude/rules/reporting.md` | 100% |
| 2 | 配置ディレクトリ | `particular_case/` | 「1つの worktree を片付ける」という単一目的の操作単位である | rucm スキルの粒度ガイド | 95% |
| 3 | BRIEF DESCRIPTION | 走査する・取り直す・失うものを確かめる・消す の4文 | 設計の後始末の手順をそのまま4文へ落とした | `docs/plans/continuo_design.md#3-9` | 90% |
| 4 | PRECONDITION | 常駐している。身元ファイルを持つ worktree がある。利用者が Status を `cleanup.on_states` の選択肢へ動かしている。`cleanup.enabled` が true である | **片付けの契機は Status が `cleanup.on_states` に入ることだけである**。`cleanup.enabled` が false のときは代替フローで扱う | `docs/plans/continuo_design.md#3-9` の手順1 と手順5、`internal/workspace/cleanup.go` の `ShouldCleanup` | 95% |
| 5 | PRIMARY ACTOR | 巡回タイマー | 片付けを起こすのは巡回の照合である（手順7）。**人間は Status を動かすだけで、片付けを要求しない** | `docs/plans/continuo_design.md#3-9` の手順7、`internal/orchestrator/reconcile.go` の `reconcileWorktrees` | 80% |
| 6 | SECONDARY ACTORS | GitHub Projects v2、herdr、git | Status を取り直す相手・worktree を消す相手・branch を消す相手である。**branch は herdr が消さないので git を独立した副アクターにした** | `docs/plans/continuo_design.md#3-9` の手順4、`#8-1`（branch を消す） | 90% |
| 7 | DEPENDENCY | なし | 他の particular_case を取り込まない | rucm スキルのファイル規約 | 95% |
| 8 | GENERALIZATION | なし | 汎化関係にあるユースケースが無い | - | 90% |
| 9 | ステップ分割方針 | 検査を1つずつ独立した `VALIDATES THAT` にする | 手順2 と手順2b は**両方通す**と設計が決めている。1つにまとめると、どちらで見送ったのかが事後条件から読めなくなる | `docs/plans/continuo_design.md#3-9` の手順2 と 2b | 95% |
| 10 | ステップ2 と3 | 置き場所を走査して身元ファイルを読む | ディレクトリ名から issue へは戻れない。**身元ファイルが無いと、どの issue のものか分からない** | `docs/plans/continuo_design.md#3-18`、`internal/workspace/scan.go` | 100% |
| 11 | ステップ4 | ボードを project item の ID 指定でまとめて取り直す | 身元ファイルから ID がまとまって取れるので、何件あっても1リクエストで済む | `docs/plans/continuo_design.md#3-9` の手順7 | 95% |
| 12 | ステップ5（VALIDATES THAT） | 取り直した Status が `cleanup.on_states` に入っている | **「active でなくなった時点」ではない。**`In Review` と `Blocked` で消すと、人間が回答して `Ready` へ戻したときに作業成果が失われる | `docs/plans/continuo_design.md#3-9` の手順1、`#4-1` | 100% |
| 13 | ステップ6（VALIDATES THAT） | worktree が `workspace.root` の内側にある | 「消す直前」がいちばん危ない検査点である。**検査に落ちたら何も消さない** | `docs/plans/continuo_design.md#3-20`、`internal/workspace/cleanup.go` の `Cleanup` | 100% |
| 14 | ステップ7（VALIDATES THAT） | worktree にコミットされていない変更がない | `git status --porcelain` の出力が空でなければ残す。**未追跡のファイルも数に入れる**（エージェントが作った成果物が消えるのを防ぐ） | `docs/plans/continuo_design.md#3-9` の手順2 | 100% |
| 15 | ステップ8（VALIDATES THAT） | branch に push されていない成果がない | **commit の有無では判定しない。「失うものがあるか」を見る。**upstream があれば `@{u}..HEAD` の件数、無ければ base からの差分で判定する | `docs/plans/continuo_design.md#3-9` の手順2b、`internal/workspace/cleanup.go` の `effectiveBase` | 95% |
| 16 | ステップ9 | `workspace_hooks.before_remove` を消す前の worktree で実行する | 手順2d である。**失敗しても記録して続ける**（片付けを止めない） | `docs/plans/continuo_design.md#3-9` の手順2d | 95% |
| 17 | ステップ10 | herdr に workspace の ID を渡して worktree の削除を要求する | 引数は path でも branch でもなく herdr workspace の ID である（実測）。**この ID は身元ファイルから読む** | `docs/plans/continuo_design.md#3-9` の手順3、`internal/workspace/cleanup.go` の `resolveWorkspaceID` | 100% |
| 18 | worktree の workspace に `workspace.close` を呼ばないこと | `worktree.remove` の応答に workspace が入るので、続けて閉じない | 設計が「workspace は別途閉じない」と明記している | `docs/plans/continuo_design.md#3-9` の手順3 | 95% |
| 18b | ステップ11〜16 を足したこと | リポジトリの親 workspace を、条件を満たすときだけ閉じる | **`worktree.open` は workspace を2つ開くのに `worktree.remove` は1つしか閉じない。**放置すると issue 1件につき1つ溜まる | `docs/plans/continuo_design.md#3-9b`、issue #19、`test/live/herdr_test.go` | 95% |
| 18c | `cwd` を渡すのをやめる案を採らなかったこと | `cwd` は外せないと判断した | 本物の herdr で確かめた。`cwd` を省くと `worktree_not_found`、`cwd` に worktree のパスを渡すと `linked_worktree_source` で断られる。**リポジトリの親 workspace は herdr の必須の親である** | 実測 2026-08-25（`test/live/herdr_test.go` の `TestLive_WorktreeOpen_cwdはリポジトリ本体しか受け付けない`） | 100% |
| 18d | 親を開いた直後に閉じる案を採らなかったこと | 閉じるのは片付けの最後にした | **親を閉じると配下の worktree の workspace と pane も一緒に消える。**開いた直後に閉じると、いま開いた pane が消える | 実測 2026-08-25（`test/live/herdr_test.go` の `TestLive_WorkspaceClose_親を閉じると配下のworktreeも消える`） | 100% |
| 18e | 閉じる条件を2つにしたこと | 「continuo が開かせた」と「配下に worktree が残っていない」の両方 | 1つ目を落とすと**人間が自分で開いた workspace**を閉じてその人の pane が消える。2つ目を落とすと**別の issue が使っている pane**が消える | `docs/plans/continuo_design.md#3-9b` | 95% |
| 18f | 閉じられなくても片付けを失敗にしないこと | 警告としてログに出して続ける | worktree はもう消えている。ここで失敗を返すと「消えたのに失敗」という、呼び出し側が扱えない結果になる | `internal/workspace/repoworkspace.go` の `closeRepoWorkspace` | 90% |
| 19 | ステップ17 | git に branch の削除を要求する | herdr は branch を消さない（実測）。**消さないと単調増加する** | `docs/plans/continuo_design.md#3-9` の手順4、`#8-1` | 100% |
| 20 | ステップ18 | issue ごとの Claude Code の設定ファイルを消す | 設定ファイルは worktree の外にあるので、worktree を消しても残る | `docs/plans/continuo_design.md#3-12`、`internal/workspace/cleanup.go` の `Cleanup` | 90% |
| 21 | 手順0（`after_run`）を書かなかったこと | このユースケースに含めない | `after_run` は run が終わったとき（worker を止める直前）に1回だけ走らせるものであり、片付けとは契機が違う。**`issue を1件処理する` と `人間に判断を渡す` が受け持つ** | `docs/plans/continuo_design.md#3-9` の手順0 | 85% |
| 22 | 基本フローの POSTCONDITION | worktree が無い。branch が無い。workspace が閉じている。設定ファイルが無い。Status が変わっていない。印が変わっていない | **片付けは Status を動かさない。**動かすと、人間が付けた `Done` を continuo が書き換えることになる | `docs/plans/continuo_design.md#3-9`、`#4-1` | 95% |
| 23 | フロー `片付けの対象外` | RFS BASIC FLOW 5。worktree を残し、`active_states` に戻っていて pane が生きていれば pane を閉じる | 手順7b である。**この条件を外してはならない。**条件なしに閉じると、人間のレビュー待ちで正常に止まっている Claude Code を毎巡回で落とす | `docs/plans/continuo_design.md#3-9` の手順7b、`internal/orchestrator/reconcile.go` の `reconcileWorktrees` | 100% |
| 24 | `片付けの対象外` に IF-ENDIF を入れたこと | 代替フローの中で `IF-ENDIF` を使った | pane を閉じる場合と閉じない場合の**どちらも正常な処理**である。代替フローをさらに分けると分岐元が条件ステップでなくなる | rucm スキルの厳密規則11 | 85% |
| 25 | フロー `置き場所の外` | RFS BASIC FLOW 6。何も消さずに ABORT | 封じ込め検査は仕様が「最も重要な移植性の制約」と呼ぶ検査である。**落ちたら1つも消さない** | `docs/plans/continuo_design.md#3-20`、`internal/workspace/cleanup.go` の `Cleanup` | 100% |
| 26 | フロー `未コミットの変更` | RFS BASIC FLOW 7。消さずに1件だけコメントし、身元ファイルへ見送った時刻を書く | **毎巡回で警告を積まない。**issue へのコメントは1回だけ書き、以後は構造化ログにのみ残す | `docs/plans/continuo_design.md#3-9` の手順2c、`internal/orchestrator/lifecycle.go` の `cleanupPath` | 95% |
| 27 | 見送った時刻を身元ファイルへ書くこと | コメントの投稿に成功したあとで書く | 投稿の前に書くと、投稿が失敗したときにコメントが永久に出なくなる | `docs/plans/continuo_design.md#3-9` の手順2c、`internal/workspace/cleanup.go` の `CleanupResult` | 95% |
| 28 | フロー `未pushの成果` | RFS BASIC FLOW 8。`未コミットの変更` と同じ扱い | 見送りの理由が違うだけで、外側に残るものは同じである。**フローを分けたのは、どちらの検査で止まったかを事後条件で区別するためである** | `docs/plans/continuo_design.md#3-9` の手順2b と 2c | 90% |
| 29 | フロー `片付けの無効` の種別と BRANCH FROM | GLOBAL ALTERNATIVE FLOW。BRANCH FROM BASIC FLOW 5 | `cleanup.enabled` が false なら、どの片付けの契機でも片付けを行わない。**分岐元はステップ5（片付けを始めると判定した直後）である。**ここが「消すか残すか」が決まる唯一の時点である | `docs/plans/continuo_design.md#3-9` の手順5、`internal/workspace/cleanup.go` の `Cleanup` | 75% |
| 30 | `片付けの無効` でコメントを書かないこと | issue にコメントを書かない | 設定で無効にしたのは人間であり、人間はそれを知っている。**コメントを書くと毎回積まれる** | `internal/workspace/cleanup.go` の `Cleanup`（`ShouldComment` を偽にする分岐） | 90% |
| 31 | 起動時の掃除（手順6）を書かなかったこと | 本文の表にだけ書いた | 起動時の掃除は復元の手順が終わったあとに走る別の契機である。**先に走らせると、これから引き継ぐ run の branch を孤児と判定して消しかねない** | `docs/plans/continuo_design.md#3-9` の手順6 | 85% |
| 32 | 孤児 branch の掃除（手順6b）を書かなかったこと | rucm ブロックに書かない | 対象が「置き場所の走査で見つかった worktree が属するリポジトリ」であり、**1件の worktree を片付けるこのユースケースの外側にある** | `docs/plans/continuo_design.md#3-9` の手順6b | 80% |
| 33 | 表を2つ足したこと | 「失うものがあるかを2つの検査で見る」と「片付けが始まる契機は3つある」を本文の表にした | 判定の中身（`git rev-list` と base からの差分）は rucm ブロックのステップに書くと長すぎる。**契機の一覧が無いと、このユースケースがいつ走るのかが読めない** | `docs/plans/continuo_design.md#3-9` | 85% |
| 34 | 代替フローの POSTCONDITION の書き方 | 「worktree」「branch」「コメント」「Status」を毎回書く | 片付けで人間が知りたいのは「消えたか」と「なぜ消えなかったか」である | `docs/plans/continuo_design.md#3-9` | 85% |
| 35 | branch を消す前に実在を確かめること | `git show-ref --verify refs/heads/<名前>` で見て、実在しなければ「消す対象が無かった」として残ったものに数えない | **「現物を引けない」と「存在しない」を区別していなかった。**着手が `git worktree add` で失敗し続けると、ディレクトリだけが残って branch は1度も作られない。そこで「消せませんでした」と積むと、**利用者は存在しないものを探して消しに行く** | `internal/workspace/cleanup.go` の `deletableBranch`、`internal/workspace/git.go` の `gitBranchExists`、issue #27 | 100% |
| 36 | 実在を確かめる位置 | 接頭辞の検査のあと、worktree の現物との突き合わせの前 | **実在しなければ、そのあとの検算が通ろうと落ちようと消すものは無い。**接頭辞より前に置くと、書き換えの痕跡（正規化で変わる名前・接頭辞違い）を警告として残せなくなる | `internal/workspace/cleanup.go` の `deletableBranch` | 90% |
| 37 | git が実在を答えられなかったときの扱い | 「無い」とは言わず、そのまま現物との突き合わせへ進む | **「無い」と「調べられない」は別である。**リポジトリを名指しできない環境で「無かった」と言うと、残っている branch が見えなくなる | `internal/workspace/cleanup.go` の `deletableBranch` | 95% |
| 38 | `cleanup.delete_branch` が false のときの扱い | 実在しなければ、設定に関わらず何も出さない | **設定で消さないことにしていても、元から無いものを「残っています」と言う理由が無い** | `internal/workspace/cleanup.go` の `Cleanup`（段4 の分岐） | 90% |
| 39 | フロー `壊れたref` の種別と BRANCH FROM | GLOBAL ALTERNATIVE FLOW。BRANCH FROM BASIC FLOW 17 | ステップ17 の実在の検査は**真と偽の2つしか持たない**が、ref が壊れているのはそのどちらでもない第3の状態である。**`WHEN` を持つ大域代替フローは、まさにその第3の状態のためにある** | `docs/plans/continuo_design.md#3-22b`、`internal/workspace/git.go` の `gitBranchDelete`、`internal/workspace/cleanup.go` の `deletableBranch` | 90% |
| 40 | 壊れた ref を消す条件を7つにしたこと | 接頭辞・refname の正しさ・`show-ref` の失敗・`rev-parse` の失敗・**解決後の**置き場所・通常のファイルであること・中身が ref として読めないこと | どれか1つでも落とすと、**正常な branch・利用者の `main`・`.git` の外のファイル**を消す経路ができる。とくに置き場所は、途中のシンボリックリンクを解決しないと前方一致を素通りされる | `docs/plans/continuo_design.md#3-22b`、`internal/workspace/brokenref.go` の `brokenBranchRef` | 95% |
| 41 | packed-refs を書き換えず、消したあとに存在を確かめ直すこと | loose な ref のファイルだけを消し、`git show-ref` で branch が残っていないかを見る | loose を消すと packed 側の ref が**生き返る**（実測 2026-08-25）。確かめずに nil を返すと、**消えていない branch を「片付けた」と答える** | `docs/plans/continuo_design.md#3-22c`、`internal/workspace/git.go` の `confirmBranchGone` | 95% |
| 42 | branch を答えず **detached でもない**ときだけ壊れた ref の判定を使うこと | ref が壊れていると `HEAD 0000000000000000000000000000000000000000` の行だけになり `branch` も `detached` も出ない。**detached HEAD でも branch 名は空になる**ので、そこは混ぜない | 検算がここで落ちると **その branch は永久に片付かない**。だが detached まで通すと、身元ファイルの値が git の現物と1度も突き合わされない。**そこで `worktrees/<名前>/HEAD` の symref で裏を取る** | 実測 2026-08-25（git 2.50.1）、`internal/workspace/cleanup.go` の `brokenRefBranchAt` | 90% |
| 43 | フロー `消さないref` が ABORT ではなく `RESUME STEP 5` で戻ること | branch を残したまま設定ファイルの削除まで進む | worktree は既に消えている。**ここで止めると issue ごとの設定ファイルだけが残る** | `internal/workspace/cleanup.go` の `Cleanup`（`Leftovers` を積んで続ける分岐） | 85% |
| 44 | 消す前に reflog から commit を控えること | `<共通ディレクトリ>/logs/refs/heads/<branch>` の最後の行から SHA を読み、戻せるコマンドを添える | **「壊れた ref には読める情報が1バイトも無い」は事実ではない。**git のコマンドからは読めないだけで、ファイルには残っている。孤児 branch の削除（手順6b）は同じ規則を既に守っている | 実測 2026-08-25、`internal/workspace/brokenref.go` の `brokenRefTip` | 90% |
| 45 | 消したことを `Notices` で画面へ出すこと | `Leftovers` とは別の並びに積み、`continuo abandon` が1行ずつ出す | `continuo abandon` は Logger を渡さないので、**ログに書くと誰も読めない**（issue #23 と同じ判断）。`.git` の中のファイルを continuo が消したことは、人間が知る手立てが要る | `internal/workspace/cleanup.go` の `CleanupResult.Notices`、`internal/abandon/abandon.go` | 90% |
| 46 | 判定と削除のあいだに読み直すこと | 判定時の大きさと最終更新時刻を控え、`os.Remove` の直前に一致を確かめる | 判定は git を4回起動するので数十ミリ秒の窓ができる。そのあいだに別のプロセスが**正常な ref を置き終える**ことがある。**この競合は踏ませて再現していない**（窓の存在をコードで示しただけである） | `internal/workspace/brokenref.go` の `pruneBrokenBranchRef` | 70% |
| 47 | 実在の検査と壊れた ref の判定の順番 | `git show-ref --verify --quiet` が「無い」と答えたら、**branchAbsent と答える前に壊れた ref かどうかを見る** | **`show-ref` は壊れた ref にも終了コード 1 を返す**（実測 2026-08-25、git 2.50.1）。そこを「元から無かった」に丸めると、**壊れた ref のファイルが誰にも消されないまま残る** | `internal/workspace/cleanup.go` の `deletableBranch` と `brokenRefBranchAt`、issue #27 と issue #28 | 95% |
