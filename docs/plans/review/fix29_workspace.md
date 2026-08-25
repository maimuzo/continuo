# 片付けと着手の取り消しの7件（internal/workspace / internal/abandon）

**担当分の進捗をここに追記する。**指摘の全文は `docs/plans/review/2026-08-25_full_audit.json` にある。

## 直す7件と着手の状態

| 指摘 | 触る場所 | 状態 |
| --- | --- | --- |
| 孤児 branch の掃除が delete_branch を見ない | internal/workspace/sweep.go | 直した（変異で確認済み） |
| 未追跡ディレクトリで失う量を実際より少なく見せる | internal/workspace/git.go | 直した（変異で確認済み） |
| ghq への問い合わせで owner/repo を正規化している | internal/workspace/git.go | 直した（変異で確認済み） |
| 同じリポジトリの2件目以降が親 workspace の閉じる責任を持たない | internal/workspace/repoworkspace.go | 直した（変異で確認済み） |
| issue 番号を検算していない | internal/abandon/abandon.go | 直した（変異で確認済み） |
| 動いているときの pane 待ちは --force で越えられない | internal/abandon/abandon.go | 直した（変異で確認済み） |
| 読めない身元ファイルを飛ばしても「worktree はありません」と言って 0 で終わる | internal/abandon/abandon.go | 直した（変異で確認済み） |

## 直した中身（internal/workspace）

### 孤児 branch の掃除が delete_branch を見ない

`SweepOrphanBranches` の先頭で `cleanup.delete_branch` を見て、偽なら1本も消さずに戻す。
**壊れた ref も例外にしない。**壊れているかどうかは利用者から見えず、
「消すなと言ったのに消えた」という結果だけが同じである。

指摘の `fix` にあった「Leftovers に積んだ branch を KeepBranches へ積む」は**やっていない。**
その経路は internal/orchestrator にあり、この担当の触る範囲の外である。
なお検算に落ちた branch（正規化で名前が変わるもの）は、掃除の側も同じ検査を持っているので元から消えない。

### 未追跡ディレクトリで失う量を実際より少なく見せる

`gitStatusPorcelain` に `-uall` を足した。実測（git 2.50.1、macOS）:

- `git status --porcelain` … `?? node_modules/` の1行
- `git status --porcelain -uall` … 中の5ファイルが5行

### ghq への問い合わせで owner/repo を正規化している

`RunGhqList` / `RunGhqGet` の `normalize.Normalize` をやめ、`ghqTarget` で
「英数字・ハイフン・アンダースコア・ドットだけ、先頭が `-` でない」ことを検査して、
外れたら**別名に直さずにエラーにする。**新しいキー `workspace.ghq_target.name_invalid` を足した。

### 同じリポジトリの2件目以降が親 workspace の閉じる責任を持たない

`closeRepoWorkspace` が「他の worktree が残っているので閉じない」と決めた時点で、
残っている worktree の身元ファイルへ `herdr_repo_workspace_id` を書き移す
（`handOverRepoWorkspace` → `Manager.SetRepoWorkspaceID`）。

- **1つではなく残り全部へ渡す。**1つだけだと、その片付けが落ちた時点で責任が消える
- **置き場所の内側にある worktree にだけ書く**（人間が開いた worktree へ書き込まない）
- **既に値が入っていれば上書きしない**（別のリポジトリの親を閉じにいく身元ファイルを作らないため）

## 直した中身（internal/abandon）

### issue 番号を検算していない

`pathAgrees` に**置き場所の最下層のディレクトリ名（スラグ）の照合**を足した。
スラグは `branch_template` から作られ、既定では issue 番号を含む。
`Manager.ExpectedSlugFor(issue)` を新しく足し、`abandon.Workspace` インターフェースにも入れた。

**ホストは比べない。**同じ issue が GitHub Enterprise のホスト名と `github.com` の両方で
書かれうるので、比べると表記違いだけの正当な worktree を外す。**その代わり、
owner・リポジトリ名・スラグが一致してホストだけが違う worktree が2つあれば、
「候補が2件なら止まる」が効く**（既存の分岐が死なない）。

指摘の `fix` は `ExpectedPathFor` でパス全体を比べる案だったが、**採らなかった。**
パス全体を比べると候補は構造的に1件以下になり、「候補が2件なら止まる」経路が到達不能な
デッドコードになる。

### 動いているときの pane 待ちは --force で越えられない

2つ直した。

- `waitPaneGone` の時間切れで `--force` を見て、pane ごと消す（`abandon.pane_alive_forced` を出して続行）
- **手を離させる書き込みが入らなかったときは、そもそも待たない。**誰も閉じないので必ず時間切れになる。
  その場合は「動いていない」ときと同じ検査（`stopIfPaneAlive`）へ落とす

`abandon.err_pane_remains` の文言にも `--force` で越えられることを書き足した。

### 読めない身元ファイルを飛ばしても「worktree はありません」と言って 0 で終わる

`find()` で候補にできなかった件数が1件以上なら、`abandon.not_found` を出さずに
新しい `abandon.err_undecided` を出して `ExitStopped`（1）を返す。
残った branch の片付け（段2b）へも進まない。
