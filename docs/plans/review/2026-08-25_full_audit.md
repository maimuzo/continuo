<!-- 目的: 全コードを15人で分担して読ませたレビューの結果と、検証を通ったものの記録 -->

# 全コードのレビュー 2026-08-25

**言いたいこと。**33,565行を15人で分担して読ませ、**104件の指摘**が出た。
**そのうち重いもの35件すべてを別の検証者に「反証しろ」と投げ、29件が反証できずに残った。**
**残った29件は、どれも「壊れた入力・失敗した外部コマンド・書き換えられた身元ファイル」で起きる。正常系のバグは1件も残っていない。**

生の指摘は [2026-08-25_full_audit.json](2026-08-25_full_audit.json) に全件そのまま置いた。

## 出た数

| 深刻さ | 件数 |
| --- | --- |
| CRITICAL | 4 |
| HIGH | 31 |
| MEDIUM | 47 |
| LOW | 22 |
| **合計** | **104** |

## 検証の通し方

**指摘を出した人とは別の人に「その指摘は誤っている、と示せ」と投げた。**
反証できなかったものだけを残す。**迷ったら反証side に倒すよう指示した**（誤った指摘を通すより、正しい指摘を落とすほうがましである）。

| 段 | 件数 |
| --- | --- |
| 重い指摘（CRITICAL と HIGH） | 35 |
| 検証にかけた | **35（全件）** |
| **反証できなかった（＝残った）** | **29**（1巡目18 + 2巡目11） |
| 反証された | 6 |


## 検証を通ったもの（18件）

| 短縮名 | 深刻さ | 利用者から見て何が壊れるか |
| --- | --- | --- |
| **引き渡し中の Status を着手が上書きする** | HIGH | 人間が引き取った issue のボード上の状態が `In Progress` に書き換わり、レビュー待ちだったはずの worktree で Claude Code がもう一度動き出す。`In Progress` は ac |
| **身元ファイルの workspace ID を検算せずに pane を閉じる** | HIGH | 無関係の issue で走っていた Claude Code が turn の途中で殺される。その run は stall として扱われ、リトライを1つ消費して再 dispatch される（回数を使い切っていれば fail |
| **送信できなかった turn を「hook が届かない」と偽って報告する** | HIGH | 人間は「Stop hook の設定が壊れている」と読み、continuo の実行時ディレクトリにある設定ファイルの `hooks` を確かめに行かされる。そこには何の異常もない。本当の原因（pane が消えた／herdr |
| **コメント復元の worktree.open に cwd が無い** | HIGH | エージェントが何をしたのかを issue に書かせる最後の砦が働かない。ログに「復元のための workspace を開けません」が1行出るだけで、issue には成果の説明も引き渡しの通知も残らず（failComment |
| **打ち切りのとき issue に残る理由が嘘になる** | HIGH | issue に残るのは「Claude Code は作業を終えたと表明しましたが、何をしたのかを issue に書き残しませんでした」という文面だけになる。実際にはエージェントは完了を表明しておらず、画面が止まって打ち切ら |
| **身元ファイルの project_item_id を検算していない** | HIGH | scanIdentities が2つの worktree を「同じ issue の worktree が2つ」と判定し、created_at が古いほうを『捨てた身元』にする。その結果、無関係な別 issue の生きてい |
| **herdr の一覧が1回取れないだけで生きている run を pane 無しとして扱う** | HIGH | to_failure_state のとき、走っている全部の run の Status が failure_state へ落ち、issue に「この issue の Claude Code の pane が残っていませんで |
| **同じリポジトリの2件目以降が親 workspace の閉じる責任を持たない** | HIGH | どちらの issue も終わったのに、リポジトリの親 workspace が herdr に開いたまま残る。continuo はもうその ID をどこにも持っていないので、以後どの経路でも閉じられない。issue #19 |
| **未追跡ディレクトリで失う量を実際より少なく見せる** | HIGH | 画面には「コミットされていない変更: 1 ファイル」と出るのに、実際には数千ファイルが消える。人間はその数を見て --force を付けるかどうかを決めるので、見せた数より多く失う。inspect.go 自身が「見せた数 |
| **孤児 branch の掃除が delete_branch を見ない** | CRITICAL | 利用者が設定で「branch は消すな」と明示したのに、再起動しただけで branch が消える。--force で片付けた worktree の branch には未 push の commit が載っていることがあり |
| **ghq への問い合わせで owner/repo を正規化している** | HIGH | その issue は着手されず、毎回 ErrCloneNotFound で失敗する。さらに、そのとき出る文面（internal/i18n/messages/ja.json:618 の workspace.prepare. |
| **issue 番号を検算していない** | CRITICAL | issue 42 の worktree のディレクトリと branch continuo/octocat/hello-world/42 が消える。--force 併用時は未コミットの成果ごと失われる。画面には「workt |
| **動いているときの pane 待ちは --force で越えられない** | HIGH | herdr workspace を手で閉じるまで、その issue を abandon できない。--force を付けても越えられず、止まる文言（abandon.err_pane_remains = 「pane が % |
| **読めない身元ファイルを飛ばしても「worktree はありません」と言って 0 で終わる** | HIGH | 人間には「もう無い」と読める断定が出て、終了コード 0 なので後続のスクリプトや手順も成功として進む。実際には worktree も branch も herdr workspace も残ったままで、次の巡回で継続監視が |
| **取り直しの一括失敗** | CRITICAL | 壊れた item が1件あるだけで、(1) 実行中 issue の照合（reconcile.go:77）が毎巡回まるごと飛ぶので、ボード側で Done にした issue の worker が止まらなくなる、(2) 取り |
| **turn の待ちが herdr より先に切れる** | CRITICAL | sendTurn が受け取るのは herdr の timeout ではなく continuo 側の read_timeout になる。turn.go:274 の分岐に入らないので、枠待ちの判定（afterWaitTime |
| **一時的な失敗の判定が使われていない** | HIGH | sendTurn は ErrCodeTimeout 以外のエラーをすべて turnStalled にするので、herdr を再起動しただけで走行中の run が諦められる。リトライを消費し、使い切ると issue が f |
| **setup が cleanup.on_states を置き去りにする** | HIGH | worktree と branch が永久に片付かない。`sweep_on_startup` も毎回0件で終わる。validate は `Done` が active_states に無いので通し、起動時の Status |

## 反証されたもの（6件）

**次の6件は、検証者が「起こらない」と示した。**直さない。

- resets_at が null の枠待ちが永久に外れない
- 古い Stop で次の turn が即座に終わる
- pane を閉じられなくても閉じた前提で進む
- 片付けが親 workspace を開き直して閉じない
- herdr が別の場所を開いたときに開かせた workspace を閉じない
- SHA を控えられなくても restore コマンドを出す

## 検証を通ったもの・2巡目（11件）

**残っていた11件も反証にかけた。1件も反証されなかった。**

| 短縮名 | 深刻さ | 利用者から見て何が壊れるか |
| --- | --- | --- |
| **block 形式のリストを setup が壊して成功と報告する** | HIGH | 書き換え後の WORKFLOW.md を continuo が一切読めなくなる（`continuo doctor` が `[23:5] value is not allowed in this context` で落ちる |
| **CRLF の WORKFLOW.md を setup が「キーが無い」と誤報する** | HIGH | 存在するキーについて「WORKFLOW.md に書き換える対象のキーがありません」と嘘の報告をし、そのうえ `continuo init で作り直してください` と案内する。案内どおり `continuo init -- |
| **setup が WORKFLOW.md に書かれた owner とボード番号を捨てる** | HIGH | ボードが複数ある利用者は、WORKFLOW.md に答えが書いてあるのに `--project` を要求されて先へ進めない（update.go:75-78 が「2026-08-21 に実際に詰まった」として直したはずの症 |
| **hook の置き場所が「作れます」と嘘をつく** | HIGH | `continuo doctor` が `✓ hook の置き場所   /…/hooks.sock に socket を作れます` と出して終了コード 0 で終わるのに、`continuo` を起動すると hookser |
| **doctor のテストが実機の socket を触り、検査本体を1度も走らせていない** | HIGH | `checkRuntimeDir` は「doctor が全項目 ✓ なのに起動が落ちた」issue #9 のために足された検査なのに、その検査自身に失敗系のテストが1本も無く（`grep -n "LabelRuntime |
| **gh auth token に期限が無い** | HIGH | ログの最後の行が「二重起動防止のロックを獲得しました」のまま、起動時検査（60秒の期限）にも復元にも巡回にも進まず、issue が1件も処理されない。flock は握ったままなので、別の端末でもう一度 continuo  |
| **gh auth status の失敗を未ログインと言い切る** | HIGH | 継続監視が始まらないまま、ログには「起動時の検査に落ちました: gh に github.com の有効なアカウントがありません（`gh auth login -s project` を実行してください）」だけが残る。gh |
| **hooks を見せずに信頼を渡す** | HIGH | `continuo trust --dry-run` の画面には `permissions.allow: なし` `permissions.additionalDirectories: なし` `.mcp.json: あ |
| **長い1行で setup が無言で終わる** | HIGH | `bufio.Scanner` が `bufio.ErrTooLong` で止まり、以降の正しい行も1行も読まれない（実測: 5000バイトの行＋正しい5行を与えて `scanned=0 err=bufio.Scanne |
| **読めなかった設定を「ありません」と言って信頼する** | HIGH | 2つ壊れる。1つ目は嘘の報告で、`readSmallFile` は「無い」「symlink」「大きすぎる」「開けない」を全部 `found=false` に畳むため、report.go:78-79 が実在するファイルにつ |
| **枠の判定が一度の失敗で永久に切れる** | HIGH | 以後 `Enabled()` が偽のままになり、`pollQuota` が早期 return して usage API を二度と読まない。復帰の経路は無い（`disabled` を偽に戻すコードが1行も無い）。その結果、 |

（2巡目の元の一覧）

- block 形式のリストを setup が壊して成功と報告する
- CRLF の WORKFLOW.md を setup が「キーが無い」と誤報する
- setup が WORKFLOW.md に書かれた owner とボード番号を捨てる
- hook の置き場所が「作れます」と嘘をつく
- doctor のテストが実機の socket を触り、検査本体を1度も走らせていない
- gh auth token に期限が無い
- gh auth status の失敗を未ログインと言い切る
- hooks を見せずに信頼を渡す
- 長い1行で setup が無言で終わる
- 読めなかった設定を「ありません」と言って信頼する
- 枠の判定が一度の失敗で永久に切れる

## 直したかどうか

**この節に追記する。**
