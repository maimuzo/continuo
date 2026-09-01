# 作業の記録

**このファイルは追記する。既存の内容を消さない。**

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
| `worktree と branch を片付ける.rucm.md` | GLOBAL `壊れたref`（BRANCH FROM BASIC FLOW 17、RESUME STEP 22）と SPECIFIC `消さないref` |

**RUCM → CFG → テストの順で通した。**`rucm_validator.py` はどちらもエラー0件・警告0件。
CFG を再生成すると path ID が振り直されるので、テストの `RUCM-PATH` マーカーを対応表で書き換えた
（`issue を1件処理する` は P014 以降が2つ後ろへ、`worktree と branch を片付ける` は
P002 以降が振り直し）。`sh scripts/check-rucm.sh` は OK。
mermaid の図も直し、`mermaid-validate validate-md` で2ファイル・各2ブロックとも Valid。

**設計に1節足した。**`docs/plans/continuo_design.md` の 3-22b（壊れた ref はファイルとして消す）。
3-22 の手順にも段5b を足した。

**通したもの。**`gofmt -l ./cmd ./internal ./test`（出力なし）、`go vet ./...`（出力なし）、
`sh scripts/test-like-ci.sh`（`-race` 込みで全パッケージ ok）。
## issue #27 の修正への監査（9件）を直す

**言いたいこと。**`git worktree prune` が git の守りを外していた。
撃つのをやめ、断られた理由を人間へ渡す形にした。あわせて、
「調べられない」を「無い」に丸めていた3箇所と、テストの空白を埋めた。

### 直したもの

| 指摘 | どこ | 何をしたか |
| --- | --- | --- |
| prune が守りを外す | [internal/workspace/issuebranch.go](../../internal/workspace/issuebranch.go) | `DeleteIssueBranch` から prune を外し、断られたら登録のパスと prune の案内を返す |
| prune の巻き添え | [internal/workspace/cleanup.go](../../internal/workspace/cleanup.go) | `removeWorktreeByHand` は、実体の無い登録が自分で消した1件だけのときにしか撃たない |
| prune の行にテストが無い | [test/internal/workspace/issuebranch_test.go](../../test/internal/workspace/issuebranch_test.go) | `FindIssueBranch` / `DeleteIssueBranch` / `ScanUnidentified` の5本を新設 |
| git の失敗を「無い」に丸める | [internal/workspace/git.go](../../internal/workspace/git.go) | `gitBranchExists` は終了コード 1 だけを「無い」とし、それ以外はエラーにする |
| 身元ファイルが無い worktree を数えない | [internal/workspace/scan.go](../../internal/workspace/scan.go) | `ScanUnidentified` を足し、`abandon` が「判断できないもの」に数える |
| `cleanup.delete_branch` を切った経路のテストが無い | cleanup_test.go / abandon_test.go | worktree がある経路と無い経路に1本ずつ足した |
| 「調べられなかった」経路のテストが無い | abandon_test.go | clone を引けないときに「無い」と言わないことを確かめる |
| `branch_template` に issue 番号が要らない | [internal/config/validate.go](../../internal/config/validate.go) | `.issue.number` を必須にした |
| 未 push の commit を数えない | issuebranch.go / abandon.go | `git rev-list --count <branch> --not --remotes` で数え、`--force` の前に見せる |

### あわせて直したもの

- RUCM: 着手を取り消す / worktree と branch を片付ける（フローと mermaid の2ブロックずつ）
- CFG: 両方を `rucm_to_cfg.py` で再生成し、テストの `RUCM-CFG-SHA256` を更新
- 設計: [docs/plans/continuo_design.md](continuo_design.md) に 3-37-9b / 3-37-9c / 3-37-9d を追加
- 文言: `internal/i18n/keys.go` と `internal/i18n/messages/ja.json` に8件

### 確かめたこと

- 新しいテストは、直す前の実装（prune を撃つ・分岐を消す）に戻すと**実際に落ちる**
- `gofmt -l` / `go vet` / `sh scripts/test-like-ci.sh` / `sh scripts/check-rucm.sh --strict`

## issue #27 を issue #28 のあとに乗せ直した

**言いたいこと。**同じ検査点に2つの直しが重なったので、順番を決め直した。
**`git show-ref --verify --quiet` は壊れた ref にも終了コード 1 を返す**（実測: 2026-08-25、git 2.50.1）。
そのまま「実在しない」に丸めると、issue #28 が消すはずの壊れた ref が誰にも消されないまま残る。

| どこ | 何をしたか |
| --- | --- |
| [internal/workspace/cleanup.go](../../internal/workspace/cleanup.go) | `deletableBranch` は、実在の検査が「無い」と答えたら **`brokenRefBranchAt` を先に見てから** `branchAbsent` を返す |
| [internal/workspace/issuebranch.go](../../internal/workspace/issuebranch.go) | `gitBranchDelete` の呼び出しに `brokenRefPolicy` を渡す |
| `worktree と branch を片付ける.rucm.md` | BASIC FLOW の 17〜23（実在の検査）と GLOBAL `壊れたref` の両方を持つ形に書き直し、CFG を再生成した |
| cleanup_test.go / repoworkspace_test.go | 振り直された path ID に `RUCM-PATH` と `RUCM-CFG-SHA256` を貼り直した |

## issue #113 レビュー結果が貼られていない PR を CI で落とす

**言いたいこと。**hook はコマンドの文字列から PR 番号を当てていて、書き方を変えられると外れる。
**CI なら `github.event.pull_request.number` で確実に取れる。**
あわせて、判定の条件が3箇所で違っていたのを揃えた。

**作ったもの。**[.github/workflows/review-gate.yml](../../.github/workflows/review-gate.yml)。

| 何 | 決め | なぜ |
| --- | --- | --- |
| ファイル | **`ci.yml` に足さず、新しい workflow にする** | `types` は workflow 単位でしか書けず、`ready_for_review` を足すと test と build が回り直す。さらに `ci.yml` は PR の run を打ち切るので、`gh pr ready` が走行中の run を殺す |
| イベント | `opened` / `synchronize` / `reopened` / **`ready_for_review`** | `gh pr ready` で回り直す。「貼る → ready → 緑」が人手なしでつながる |
| 権限 | `issues: read` と `pull-requests: read` だけ | 叩く先は `/issues/{番号}/comments` だが相手は PR である。**checkout しないので `contents` は要らない** |
| draft | **落としたままにする**（job を飛ばさない） | **飛ばした job は「成功」として報告され、必須の検査でもマージを止められない** |
| `concurrency` | **置かない** | 打ち切られた run は success / skipped / neutral のどれでもなく、必須の検査にするとマージを塞ぐ |

**判定の条件を3箇所で揃えた。**片方だけ緩いと、緩いほうが実質の規則になる。

| どこ | 何を止めるか |
| --- | --- |
| [.claude/hooks/block-merge-without-review.py](../../.claude/hooks/block-merge-without-review.py) | 手元の `gh pr merge` / `gh pr ready` |
| [.github/workflows/review-gate.yml](../../.github/workflows/review-gate.yml) | PR のマージ |
| [scripts/check-release-ready.sh](../../scripts/check-release-ready.sh) | タグを打つこと |

**条件は「目印が本文の先頭にある」ことと「投稿者が `OWNER` / `MEMBER` / `COLLABORATOR`」の2つである。**

**hook だけは絞り込みを Python 側に移した。**jq の式に押し込むと、`gh` を叩かない限り条件を確かめられない。
`counts_as_review` を切り出し、[.claude/hooks/tests/test_block_merge_without_review.py](../../.claude/hooks/tests/test_block_merge_without_review.py) に10件足した。

**採らなかった案。**`issue_comment` で走らせる形。
**その run の `GITHUB_SHA` は既定の branch の最新の commit であり、PR の先頭の commit に紐づかない。**
必須の検査は PR の先頭の commit で通っている必要があるので、いつまでも条件を満たさない。

**`review-result` は branch protection の必須の検査に入っている**（2026-09-02 に確認）。

```
$ gh api repos/<owner>/continuo/branches/main/protection/required_status_checks --jq '.checks[].context'
test (ubuntu-latest)
test (macos-latest)
build (darwin, arm64)
build (darwin, amd64)
build (linux, amd64)
build (linux, arm64)
review-result
```

**入れ直す手順は [CONTRIBUTING.md](../../CONTRIBUTING.md) の「この検査をマージの条件にする」にある。**
**`checks` は全件置き換えである。**一部だけ渡すと、渡さなかった検査が必須から外れる。

## PR #149（作業の手順をプロジェクトの下へ集め worker にも読ませる形にする）のレビュー2巡目

**言いたいこと。**`verify` の許可リストが3通りの書き方で破れていた。
**うち1つは改行1文字で、検査が丸ごと素通しになる。**3つとも塞ぎ、テストで押さえた。

### 直したもの（6件中4件）

| 指摘 | どう直したか |
| --- | --- |
| **改行で許可リストを抜けられる**（`echo 1\ntouch x`）。`shlex` は空白として読み、shell は2つのコマンドとして走らせる | `verify_rejection` の先頭で、tab 以外の制御文字を断る |
| **展開の記号をコマンド全体で探していた。**単引用符を消す正規表現が語をまたいで対になり、間の backtick を隠す（`grep -c "don't" f \`id\` "isn't"`） | `_expansion_in` を切り出し、**トークンごとに**見る。`shlex` が引用符を尊重して切るので、対が語をまたがない |
| **`gh api` の書き込みの flag を並べていたので、値を続けて書いた形が漏れた**（`-XPOST` / `-ffoo=bar`） | `_GH_API_READ_FLAGS` に**通す flag のほうを並べる。**短い flag は先頭2文字でも見る |
| **`cmd_merge` が `done` / `merged` の行を上書きしていた。**「何をしたか」「どこへまとめたか」が消える | `--ids` に `open` でないものがあれば、**1件も書き換えずに断る** |

**実測**（2026-09-02、塞ぐ前）。

```
run_verify("echo 1\ntouch <パス>")  →  (True, '1')   ファイルができた
```

### 直していないもの（2件）。**どちらも [.claude/hooks/block-merge-without-review.py](../../.claude/hooks/block-merge-without-review.py) の `targets_other_repo` / `target_prs`**

**メインエージェントが直した箇所であり、触らないよう指示されている。**

| 指摘 | 実測 |
| --- | --- |
| **他所を指す `--repo` が1つでもあると、同じコマンド行の `gh pr merge` が全部素通しになる。**`targets_other_repo` はコマンドの文字列全体を見て、1つ当たったら空を返す | `CONTINUO_HOOK_REPO=maimuzo/continuo` で `gh release view --repo herdrdev/herdr v1 && gh pr merge 149` を `target_prs` に渡すと `[]` が返る（止まらない） |
| **`target_prs` が `MERGE_RE` より先に repo を引く。**`gh repo view`（上限5秒）が、マージと無関係な `gh … --repo …` のたびに走る | `_repo_of_cwd` は `PreToolUse` の中で `subprocess.run` する |
