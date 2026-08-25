# 作業の進捗

## issue #28 壊れた ref があると着手できない

**再現した**（2026-08-25、git 2.50.1 Apple Git-155）。
`/tmp/continuo-brokenref-repro/clone/.git/refs/heads/continuo/octocat/hello-world/1303` を
0バイトで置くと、次のとおりになる。

| 実行したもの | 結果 |
| --- | --- |
| `git worktree add -b continuo/octocat/hello-world/1303 <path> develop` | `fatal: cannot lock ref …: reference broken` / exit 255 |
| `git update-ref -d refs/heads/continuo/octocat/hello-world/1303` | `error: cannot lock ref …: reference broken` / exit 1 |
| `git branch -D continuo/octocat/hello-world/1303` | `error: branch … not found` / exit 1 |
| `git show-ref --verify refs/heads/…/1303` | `fatal: … - not a valid ref` / exit 128 |
| `git for-each-ref refs/heads` | `warning: ignoring broken ref …` / 一覧には出ない |

**ref のファイルを `rm` で消してから同じ `worktree add` を叩くと成功する。**
（`Preparing worktree (new branch …)` / `HEAD is now at 098896a init` / exit 0）

**直した**（実装）。

| どこ | 何を |
| --- | --- |
| `internal/workspace/brokenref.go`（新規） | 壊れた ref かの判定（`brokenBranchRef`）と、その1ファイルの削除（`pruneBrokenBranchRef`） |
| `internal/workspace/git.go` | `gitWorktreeAdd` を「1回失敗したら壊れた ref を始末して**もう1回だけ**やり直す」に。中身は `gitWorktreeAddOnce` へ切り出し |
| `internal/workspace/git.go` | `gitBranchDelete` も同じ扱い（`git branch -D` が断ったら ref のファイルとして消す） |
| `internal/workspace/cleanup.go` | `deletableBranch` が、`worktree list` が branch を1つも答えない場合に「ref が壊れている」かを見るように |
| `internal/i18n/keys.go` / `messages/ja.json` | `workspace.broken_ref.stat_failed` / `workspace.broken_ref.remove_failed` |

**消してよい条件は5つ全部を満たすときだけである。**
接頭辞（`branch_template` から作る）が空でなく名前がそれで始まる／`git show-ref --verify` が失敗する／
`git rev-parse --verify` も失敗する／`<共通ディレクトリ>/refs/heads/<名前>` が通常のファイルである／
そのパスが `refs/heads` の内側に収まる。**packed-refs は1バイトも触らない。**

**実測で分かったこと。**ref が壊れた worktree は、`git worktree list --porcelain` では
`HEAD 0000000000000000000000000000000000000000` の行だけになり、`branch` の行も
`detached` の行も出ない。片付けの検算（`deletableBranch`）はここで落ちていたので、
「git が branch を1つも答えない」ときに壊れた ref を見るようにした。

**テスト**（`test/internal/workspace/brokenref_test.go`、6本）。全部通る。

**RUCM を直した。**`docs/spec/usecases/particular_case/` の2ファイルに代替フローを足した。

| ファイル | 足したフロー |
| --- | --- |
| `issue を1件処理する.rucm.md` | GLOBAL `壊れたref`（BRANCH FROM BASIC FLOW 12、RESUME STEP 12）と SPECIFIC `消さないref` |
| `worktree と branch を片付ける.rucm.md` | GLOBAL `壊れたref`（BRANCH FROM BASIC FLOW 17、RESUME STEP 18）と SPECIFIC `消さないref` |

**RUCM → CFG → テストの順で通した。**`rucm_validator.py` はどちらもエラー0件・警告0件。
CFG を再生成すると path ID が振り直されるので、テストの `RUCM-PATH` マーカーを対応表で書き換えた
（`issue を1件処理する` は P014 以降が2つ後ろへ、`worktree と branch を片付ける` は
P002 以降が振り直し）。`sh scripts/check-rucm.sh` は OK。
mermaid の図も直し、`mermaid-validate validate-md` で2ファイル・各2ブロックとも Valid。

**設計に1節足した。**`docs/plans/continuo_design.md` の 3-22b（壊れた ref はファイルとして消す）。
3-22 の手順にも段5b を足した。

**通したもの。**`gofmt -l ./cmd ./internal ./test`（出力なし）、`go vet ./...`（出力なし）、
`sh scripts/test-like-ci.sh`（`-race` 込みで全パッケージ ok）。
