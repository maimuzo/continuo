# 着手と turn と復元の修正（29件のうち7件）

**言いたいこと。**7件はすべて `internal/orchestrator/` の中で直す。
`internal/tracker` と `internal/workspace` には1行も足さない（別の担当が同時に触るため）。
そのぶん、許可リストの検査と workspace の検算は orchestrator 側の呼び出しで閉じる。

## 直す7件と、採る直し方

| 名前 | 直し方 |
| --- | --- |
| 引き渡し中の Status を着手が上書きする | 着手の段2 で ID 指定の取り直しを1回入れ、`active_states` にあるときだけ書く |
| 身元ファイルの workspace ID を検算せずに pane を閉じる | 身元ファイルの ID を使わず、`pane.list` の `cwd` が worktree と一致する pane だけ閉じる |
| 送信できなかった turn を「hook が届かない」と偽って報告する | `turnSendFailed` を足し、herdr のエラー文をそのまま載せた別の文面にする |
| コメント復元の worktree.open に cwd が無い | `o.ws.Prepare` に寄せる（cwd / focus / label / 親 workspace の記録が全部そこにある） |
| 打ち切りのとき issue に残る理由が嘘になる | 打ち切りの分岐を `failRun` と同じ順番にし、本当の理由が投稿枠を先に取る |
| 身元ファイルの project_item_id を検算していない | 置き場所のパスから引いた owner/repo と、身元ファイル・取り直した issue の owner/repo を突き合わせる |
| herdr の一覧が1回取れないだけで生きている run を pane 無しとして扱う | `paneMatch.Unknown` を足し、真なら段8 を1件も呼ばない |

## 進捗

（着手した順に追記する）

### 7件すべてを直し、足したテストが変異で落ちることを実測した

| 指摘 | 足したテスト | 変異させた場所 |
| --- | --- | --- |
| 引き渡し中の Status を着手が上書きする | active_statesに無いStatusのissueを着手が上書きしない | 段2 の許可リストの検査を外す |
| 身元ファイルの workspace ID を検算せずに pane を閉じる | 身元ファイルのworkspaceIDを信じて別のrunのpaneを閉じない | cwd の突き合わせを外し身元ファイルの ID を宛先に戻す |
| 送信できなかった turn を偽って報告する | turnを送れなかったときStopHookのせいにしない | 送信の失敗を turnStalled に混ぜ戻す |
| コメント復元の worktree.open に cwd が無い | 復元のworktreeOpenはリポジトリ本体をcwdに渡す | Prepare を外し cwd 無しの worktree.open に戻す |
| 打ち切りのとき issue に残る理由が嘘になる | 打ち切りのときissueに残る理由が本当の理由である | 打ち切りの分岐で ensureAgentComment を先に戻す |
| 身元ファイルの project_item_id を検算していない | 置き場所と食い違う身元ファイルを鍵にしない | scanIdentities の突き合わせを外す |
| herdr の一覧が1回取れないだけで pane 無し扱い | paneの一覧を取れないだけでStatusを人間へ渡さない | paneMatch.Unknown の分岐を殺す |

**テストの置き場所。**[test/internal/orchestrator/audit_fixes_test.go](test/internal/orchestrator/audit_fixes_test.go)。
**RUCM のパスからは生成していない**（review_fixes_test.go と同じ扱い）。

### 直し方の要点

**着手の段2 は許可リストにした。**`internal/tracker` を触らない制約があるので、
`UpdateStatus` の拒否リストを許可リストに変えるのではなく、**呼ぶ前に ID 指定で取り直し、
`active_states` にあるときだけ書く**（`dispatchStatusAllowed`）。
拒否リストも残し、設定に名前が出てくる非 active の状態を全部並べる二重の守りにした。

**pane を閉じる宛先は herdr に答えさせる。**身元ファイルの `herdr_workspace_id` を使わず、
`pane.list`（絞り込みなし）の `cwd` が worktree と同じ場所を指す pane だけを閉じる。
worktree のパスは封じ込め検査を通っているのでエージェントに書き換えられない。

**コメントの復元は `workspace.Manager.Prepare` に寄せた。**`worktree.open` の `cwd` に渡す
clone の場所を知っているのはそこだけであり、`focus`・`label`・開いたものの検算・
親 workspace の控え（issue #19）も同じ1箇所から出る。**worktree の実体が無ければ呼ばない**
（Prepare は無ければ作り直すため）。

**復元の検算は owner/repo までである。**同じリポジトリの別 issue へ `project_item_id` を
差し替える経路は、この検算では止まらない（`continuo abandon` の pathAgrees と同じ限界）。
置き場所のパスから引けるのが `<owner>/<repo>` までだからである。

### RUCM を直し、CFG とテストの印を作り直した

**分岐が3つ増えたので RUCM から直した。**順序は RUCM → CFG → テストである。

| ユースケース | 足した分岐 | 直した条件 |
| --- | --- | --- |
| issue を1件処理する | 送信の失敗（BRANCH FROM BASIC FLOW 23） | ステップ10 を「取り直した Status が active_states に入っている」に |
| 再起動して実行中の issue を引き継ぐ | 名乗りの食い違い・issueの取り違え・一覧の取得の失敗 | — |

**コメントの取り戻しにステップを1つ足した**（worktree とリポジトリ本体を渡して開き直す）。

**CFG は `rucm_to_cfg.py` で作り直した。**手では直していない。
mermaid の2ブロックは両ファイルとも作り直し、`mermaid-validate validate-md` で検証済み。

**テストの印の付け替え。**パスの通し番号がずれたので、次の対応で機械的に置き換えた。

| ユースケース | 付け替え |
| --- | --- |
| issue を1件処理する | P008 以降を1つずつ繰り下げ（P008→P009 … P022→P023） |
| 再起動して実行中の issue を引き継ぐ | P009→P011、P010→P013、P011→P014 |

**担当外のファイルを1つだけ触った。**[test/internal/daemon/daemon_test.go](test/internal/daemon/daemon_test.go) は
復元のユースケースを参照しているので、**印を直さないと `check-rucm.sh` が落ちる。**
直したのは先頭の sha256 とパスの番号だけで、テストの中身は1バイトも変えていない。

**新しい3つの分岐には RUCM の印を付けていない。**印はファイル単位で1つのユースケースしか
指せず、[test/internal/orchestrator/audit_fixes_test.go](test/internal/orchestrator/audit_fixes_test.go)
は2つのユースケースにまたがるためである（review_fixes_test.go と同じ扱い）。
`check-rucm.sh` の [W1]（テスト未生成パス）に3件増えるが、これは警告であって失敗ではない。

### 設計に7件の判断を記録した

[docs/plans/continuo_design.md](docs/plans/continuo_design.md) に 3-38 から 3-44 を足した。
**既存の節は書き換えていない。**

### 直さなかったもの

**`test/e2e/fakeherdr_test.go` の `worktree.open` は `cwd` を要求しないままにした。**
本物と同じ厳しさにするのは正しいが、**担当パッケージの外である。**
同じ守りは [test/internal/orchestrator/audit_fixes_test.go](test/internal/orchestrator/audit_fixes_test.go)
の `requireCwdOnWorktreeOpen` が押さえてあり、そこが変異で落ちることも実測済みである。

**巡回の worktree の照合（reconcileWorktrees）では `project_item_id` を検算していない。**
そこは指摘に挙がっておらず、**別の run へ害が及ぶ経路が無い**ためである。
pane を閉じる宛先は cwd で決まるようになったし、片付けの宛先は走査で得た自分のパスである。
書き換えられるのは「自分の worktree をいつ片付けるか」だけである。
