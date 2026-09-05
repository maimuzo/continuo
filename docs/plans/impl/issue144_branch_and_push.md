# issue の base と push 先を、4つのユースケースで通す（#144（worktree の branch は変えず push 先だけ分ける））

**この文書が正である。**#144（worktree の branch は変えず push 先だけ分ける）の設計は、
[docs/plans/impl/issue142_144_branch_mismatch.md](issue142_144_branch_mismatch.md) ではなくこちらを見ること。
あちらは #142（worktree が別の branch を出していると永久に飛ばされる）の分だけが正である。

**この文書は誰向けか。**節ごとに冒頭で名乗る。断りが無い節は
**不特定多数の利用者向け**（continuo を入れて使う人の環境で成り立つ話）である。
**開発者の環境向け**（このリポジトリの owner の手元・プライベートのテスト環境）の話は、その旨を書く。

---

## 1. 4つのユースケースを、1つの仕組みで通す

**言いたいこと。**4つとも「コードのリポジトリと base を、issue の Development の
リンクから取る」の1つで通る。**新しい設定は要らない。**人間がリンクを張るかどうかだけで
振る舞いが変わる。

**対象（不特定多数の利用者向け）。**

| 呼び名 | 何をしたいか | リンクを張るか |
| --- | --- | --- |
| **通常** | 既定の branch から worktree を切り、continuo の branch へ push して PR を出す | 張らない |
| **既存 branch の続き** | 人間がリンクした既存 branch を base にし、その branch へ push することもある | 張る（同じリポジトリ） |
| **1つの issue で PR を複数** | 1本目がマージされたあと、2本目を別の名前で出す。worktree の branch は変えない | 張らない |
| **既存 OSS への PR** | fork（public）で改修し、upstream へ PR を出す。issue は別のリポジトリ（private / internal）にある | **張る（別のリポジトリ＝fork）** |

**「upstream」は、fork の派生元（人間の言い方では本家）を指す。**この文書では
GitHub の API が使う語に合わせて upstream と書く。

**この文書で使う3つの呼び名。**以後この名前だけを使う。

| 呼び名 | 実体（開発者の環境で実測したときの値） |
| --- | --- |
| **issue のリポジトリ** | `<ACCOUNT>/continuo-e2e`（private。実機で確かめるための issue #1（continuo の実機確認用の器）がある） |
| **コードのリポジトリ** | `<ACCOUNT>/oss-project`（public。fork） |
| **upstream** | `<UPSTREAM>/oss-project`（PR の宛先） |

**このリポジトリは PUBLIC である**（[CLAUDE.md](../../../CLAUDE.md) の制約5）。
**以後の実測の出力でも、アカウント名を `<ACCOUNT>` / `<UPSTREAM>`、
fork の名前を `oss-project` に置き換えて載せる。**それ以外の値は原文のままである。
**`<ACCOUNT>` の実体は [docs/test_environment.md](../../test_environment.md) と同じ**
（`gh repo view --json owner --jq .owner.login` で引ける）。

---

## 2. 決め手：Development のリンクは、別のリポジトリの branch を指せる

**言いたいこと。**private な issue から public な fork の branch へリンクが張れる。
**GraphQL は、その branch 名・リポジトリ名・既定 branch・fork の派生元まで1回で返す。**
**「既存 OSS への PR」に必要な値は、これで全部そろう。**

**実測（開発者の環境。2026-09-01、gh 2.97.0）。**リンクを張るコマンド。

```console
$ gh issue develop 1 --repo <ACCOUNT>/continuo-e2e --branch-repo <ACCOUNT>/oss-project --name continuo-tmp-crossrepo-check
github.com/<ACCOUNT>/oss-project/tree/continuo-tmp-crossrepo-check
```

**そのとき GraphQL が返したもの**（読みやすさのために改行を入れた。名前は上のとおり置き換えてある）。

```json
{"data":{"repository":{"issue":{"linkedBranches":{"nodes":[
 {"ref":{"name":"continuo-tmp-crossrepo-check",
  "repository":{"nameWithOwner":"<ACCOUNT>/oss-project","isPrivate":false,"isFork":true,
   "defaultBranchRef":{"name":"main"},"parent":{"nameWithOwner":"<UPSTREAM>/oss-project"}}}}
]}}}}}
```

**使ったクエリ。**

```bash
gh api graphql -f query='query { repository(owner:"<ACCOUNT>", name:"continuo-e2e") {
  issue(number:1) { linkedBranches(first:5) { nodes { ref { name
    repository { nameWithOwner isPrivate isFork defaultBranchRef { name } parent { nameWithOwner } } } } } } } }'
```

**確かめたあと、張った branch は2本とも消した。**`gh api -X DELETE repos/<owner>/<repo>/git/refs/heads/<名前>` を
2回叩き、`linkedBranches` が `{"nodes":[]}` に戻ることを確認した。

**この1つの応答から取れるもの。**

| 取れる値 | 何に使うか |
| --- | --- |
| `ref.name` | base にする branch（`origin/<name>`） |
| `ref.repository.nameWithOwner` | **コードのリポジトリ。**clone を引く相手・worktree の置き場所 |
| `ref.repository.defaultBranchRef.name` | リンクを base に使えないときの base |
| `ref.repository.parent.nameWithOwner` | **PR の宛先**（upstream） |

---

## 3. fetch の側で実測した3つ（refspec を明示する根拠）

**言いたいこと。**`git fetch origin <名前>` は、**clone の refspec 次第で
リモート追跡 ref を作らない。**だから **refspec を明示した形でしか叩かない。**
開発者の環境の scratchpad に bare の remote と clone を作って試した（git 2.50.1）。

**1. リモート追跡 ref が無ければ worktree を切れない。**

```console
$ git worktree add -b tmp/x /tmp/wtx origin/feature/later
fatal: invalid reference: origin/feature/later
```

**2. `--single-branch` の clone では、素の `git fetch origin <名前>` が
リモート追跡 ref を作らない。**FETCH_HEAD しか動かず、worktree はやはり切れない。

```console
$ git config --get-all remote.origin.fetch
+refs/heads/main:refs/remotes/origin/main
$ git fetch --no-tags origin feature/later
 * branch            feature/later -> FETCH_HEAD
$ git worktree add -b tmp/x /tmp/wtx origin/feature/later
fatal: invalid reference: origin/feature/later
```

**3. refspec を明示すれば、その clone でも作れる。**

```console
$ git fetch --no-tags origin '+refs/heads/feature/later:refs/remotes/origin/feature/later'
 * [new branch]      feature/later -> origin/feature/later
$ git worktree add -b tmp/x /tmp/wtx origin/feature/later
Preparing worktree (new branch 'tmp/x')
```

**この形は、素の clone（`+refs/heads/*:refs/remotes/origin/*`）でも同じ結果になる。**
**だから場合分けをしない。**continuo が叩く fetch は、いつもこの1つの形である。

---

## 3b. upstream の側で実測した3つ（片付けの判定の根拠）

**言いたいこと。**upstream が自動で張られるかは **clone の refspec に依存する。**
**依存しないのは「HEAD が `refs/remotes/` のどれかに載っているか」だけである。**
だから片付けの判定の中心をそちらへ移す（9）。

**1. upstream が自動で張られるのは、clone の refspec がその branch を覆っているときだけ。**
ローカルの branch から切ったときも張られない。

```console
$ git config --get-all remote.origin.fetch      # 素の clone
+refs/heads/*:refs/remotes/origin/*
$ git worktree add -b continuo/octocat/hello-world/188 /tmp/wt188 origin/feature/existing
branch 'continuo/octocat/hello-world/188' set up to track 'origin/feature/existing'.

$ git config --get-all remote.origin.fetch      # --single-branch の clone
+refs/heads/main:refs/remotes/origin/main
$ git worktree add -b tmp/x /tmp/wtx origin/feature/later
Preparing worktree (new branch 'tmp/x')
$ git -C /tmp/wtx rev-parse --abbrev-ref --symbolic-full-name '@{u}'
fatal: no upstream configured for branch 'tmp/x'
```

**2. `-u` の無い push は upstream を張り替えない。`-u` を付ければ張り替わる。**
`Everything up-to-date` でも張り替えるので、**あとから `-u` だけ付けて叩き直しても効く。**

```console
$ git push origin HEAD:pr-2nd
 * [new branch]      HEAD -> pr-2nd
$ git rev-parse --abbrev-ref --symbolic-full-name '@{u}'
origin/feature/existing
$ git push -u origin HEAD:pr-2nd
Everything up-to-date
branch 'continuo/octocat/hello-world/188' set up to track 'origin/pr-2nd'.
```

**3. `-u` を忘れた push でも、リモート追跡 ref は更新される。**
新しい commit を積むと、同じコマンドが1行も返さなくなる。
**「HEAD が remote に載っているか」は、upstream にも base にも頼らずに答えられる。**

```console
$ git for-each-ref --count=1 --contains HEAD --format='%(refname)' refs/remotes/
refs/remotes/origin/pr-2nd
$ git commit -qam five
$ git for-each-ref --count=1 --contains HEAD --format='%(refname)' refs/remotes/
$
```

---

## 4. ユースケース「通常」の流れ

**言いたいこと。**いまと1文字も変わらない。**リンクが0本なら、今までどおりに動く。**
変わるのは、**別の名前へ push するときにも `-u` を落とさないよう本文へ書き足す**ことだけである。
既定の `git push -u origin HEAD` は
[internal/scaffold/template.go:351-356](../../../internal/scaffold/template.go#L351-L356) に既にあり、
**`-u` は最初から付いている。**

**例。**issue は `octocat/hello-world#188`。リンクは0本。

| 誰が | 何をするか |
| --- | --- |
| **人間** | ボードで Status を `Ready` にする。**それだけ** |
| **continuo（段0）** | 通信しない。置き場所と branch の空きだけを見る |
| **continuo（段4）** | 下のコマンドを叩く |
| **エージェント** | 実装し、`git push -u origin HEAD` で push する |

**continuo が叩くもの。**

```bash
ghq list -p -e octocat/hello-world
git -C ~/ghq/github.com/octocat/hello-world worktree prune
git -C ~/ghq/github.com/octocat/hello-world worktree add -b continuo/octocat/hello-world/188 \
  ~/worktrees/github.com/octocat/hello-world/continuo-octocat-hello-world-188 main
```

**`git fetch` は叩かない。**リンクが0本なので、base は `main`（ローカルにある）である。

**片付け。**`cleanup.require_pushed` の判定は、
[9. 片付けを4段にする](#9-片付けを4段にする) の段1で通る（`git push -u origin HEAD` が
`refs/remotes/origin/continuo/octocat/hello-world/188` を更新するので、
段1 の `--contains HEAD` が1行返る）。**段1 は `@{u}` を1度も見ない。**

---

## 5. ユースケース「既存 branch の続き」の流れ

**言いたいこと。**人間がリンクを1本張る。continuo はそれを fetch して base にする。
**worktree の branch は continuo のもののままで、push 先だけがリンクされた branch になる。**

**例。**issue は `octocat/hello-world#188`。人間が `feature/login` をリンクした。

| 誰が | 何をするか |
| --- | --- |
| **人間** | issue 画面の Development で既存 branch を選ぶ（CLI なら下のコマンド） |
| **continuo（段0）** | 通信しない |
| **continuo（段4）** | fetch してから、リンクされた branch を base に worktree を切る |
| **エージェント** | 続きを書き、`git push -u origin HEAD` で push する。**issue に「`feature/login` へ出せ」と書かれているときだけ** `git push -u origin HEAD:feature/login` にする（14 の推奨） |

**人間が叩くもの（画面で選んでもよい）。**

```bash
gh issue develop --list 188 --repo octocat/hello-world
```

**continuo が叩くもの。**

```bash
git -C ~/ghq/github.com/octocat/hello-world fetch --no-tags origin \
  '+refs/heads/feature/login:refs/remotes/origin/feature/login'
git -C ~/ghq/github.com/octocat/hello-world worktree add -b continuo/octocat/hello-world/188 \
  ~/worktrees/github.com/octocat/hello-world/continuo-octocat-hello-world-188 origin/feature/login
```

**この worktree に upstream が張られるかは、clone の refspec で変わる**（3b の実測1）。
**張られても張られなくても片付く**ので、continuo はここで upstream を作りに行かない。

**片付け。**エージェントが push すると、その push 先の `refs/remotes/origin/<名前>` が
HEAD を含むようになり、**9 の段1で通る**（3b の実測3）。
**`HEAD` へ出しても `HEAD:feature/login` へ出しても同じである。**
upstream が張られている clone なら段2 でも 0 になる。

---

## 6. ユースケース「1つの issue で PR を複数」の流れ

**言いたいこと。**worktree の branch は変えない。**2本目は push 先の名前を変えるだけ。**
`-u` を付けさせるので、upstream が2本目の名前に張り替わり、片付けも通る。

**例。**1本目 `continuo/octocat/hello-world/188` はマージ済み。2本目を出す。

| 誰が | 何をするか |
| --- | --- |
| **人間** | 何もしない。issue のコメントで「2本目を出して」と頼むだけ |
| **continuo** | 何も変わらない。**同じ worktree を再利用する**（branch も同じ） |
| **エージェント** | `git push -u origin HEAD:continuo/octocat/hello-world/188-2` で push し、PR を出す |
| **片付け** | 段1で通る。upstream が `origin/continuo/octocat/hello-world/188-2` に張り替わる |

**なぜ worktree の branch を変えないのか。**変えると
[internal/workspace/prepare.go:207-212](../../../internal/workspace/prepare.go#L207-L212) が
「別の branch を出している」と判定して、その issue に二度と着手できなくなる
（#142（worktree が別の branch を出していると永久に飛ばされる）が報告した症状そのもの）。

**押さえどころは1つだけである。**`git push -u origin HEAD:<新しい名前>` の `-u` を落とさせないこと。
落とすと upstream が1本目のままになり、`@{u}..HEAD` が減らない。
**そこは段2（remote に載っているか）が受け止める**ので、片付かなくはならない。

---

## 7. ユースケース「既存 OSS への PR」の流れ

**言いたいこと。**人間が fork の branch を issue にリンクする。
**それだけで continuo は「コードは fork にある」「PR の宛先は upstream」を知る。**
新しい設定は要らない。

**例。**issue は `myorg/internal-tasks#42`（private）。fork は `myorg/project`（public）。
upstream は `upstream-org/project`。

| 誰が | 何をするか |
| --- | --- |
| **人間** | fork に branch を作って issue へリンクする |
| **continuo（段0）** | 通信しない |
| **continuo（段4）** | **fork の clone** で fetch し、worktree を切る |
| **エージェント** | fork へ push し、upstream へ PR を出し、そこの指摘に答える |

**人間が叩くもの。**

```bash
gh issue develop 42 --repo myorg/internal-tasks --branch-repo myorg/project \
  --base main --name work/issue-42
```

**continuo が叩くもの。**`ghq` に引かせる相手が **fork** になるのが唯一の違いである。

```bash
ghq list -p -e myorg/project
git -C ~/ghq/github.com/myorg/project fetch --no-tags origin \
  '+refs/heads/work/issue-42:refs/remotes/origin/work/issue-42'
git -C ~/ghq/github.com/myorg/project worktree add -b continuo/myorg/internal-tasks/42 \
  ~/worktrees/github.com/myorg/project/continuo-myorg-internal-tasks-42 origin/work/issue-42
```

**置き場所の `<owner>/<repo>` は fork（コードのリポジトリ）である。**
**スラグは issue のもの**（`continuo-myorg-internal-tasks-42`）。
**この2つがそろって初めて、どのコードのどの issue かがパスだけで言える**（8b で使う）。

---

## 7b. 「既存 OSS への PR」でエージェントが叩くもの

**言いたいこと。**push 先は fork、PR の宛先は upstream。
**upstream の PR は issue に紐づかないので、head の branch 名で引く。**

```bash
git push -u origin HEAD
gh pr create --repo upstream-org/project --base main \
  --head myorg:continuo/myorg/internal-tasks/42 --draft
gh api "repos/upstream-org/project/pulls?state=all&head=myorg:continuo/myorg/internal-tasks/42"
```

**push 先は worktree の branch のままである**（`continuo/myorg/internal-tasks/42`）。
**リンクされた `work/issue-42` は base であって push 先ではない**（14 の推奨。11d）。
**リンク先へ push すると base と head が同じ ref になり、PR に1行も差分が出ない。**

**3つ目が「upstream からの修正依頼」を拾う口である。**別のリポジトリの PR は
`closingIssuesReferences` で issue に紐づかないので、**いまのプロンプトの探し方では1件も出ない。**
**head の branch 名で引く**（12b で本文を足す）。

**`gh pr create --head <owner>:<branch>` は、owner が organization のときは通らない。**
`gh pr create --help` が
*"Using an organization as the `<user>` is currently not supported."*
（**訳:** `<user>` に organization を使うことは、いまのところ対応していない）と書いている
（gh 2.97.0）。**そのときは fork の clone の中から `gh pr create --repo upstream-org/project` を叩く**
（head は cwd の branch から決まる）。**どちらで通るかは実機で1件通して確かめる**（13b の受け入れ）。

---

## 8. 置き場所は、コードのリポジトリで切る

**言いたいこと。**worktree の置き場所の `<owner>/<repo>` を、issue のリポジトリではなく
**コードのリポジトリ**にする。**そうすると
[internal/workspace/repo.go:162-202](../../../internal/workspace/repo.go#L162-L202) の
`verifiedRepo` は1行も変えずに成立する。**

**置き場所（不特定多数の利用者向け）。**

```text
<workspace.root>/<host>/<コードのリポジトリの owner>/<コードのリポジトリの repo>/<スラグ>
例: ~/worktrees/github.com/myorg/project/continuo-myorg-internal-tasks-42
```

| 階層 | 何が入るか | 誰が決めるか |
| --- | --- | --- |
| `<host>` | コードのリポジトリの URL のホスト部 | リンクの `ref.repository.url`（無ければ issue の URL） |
| `<owner>/<repo>` | **コードのリポジトリ** | リンクの `ref.repository.nameWithOwner`（無ければ issue のもの） |
| `<スラグ>` | **issue** から作った branch 名のスラグ | `herdr.worktree.branch_template`（既定は issue の owner/repo/番号を含む） |

**リンクが0本のときは、コードのリポジトリ＝issue のリポジトリである。**
**したがって既存の worktree は1つも動かない。**「通常」「1つの issue で PR を複数」では
パスが今までと同じになる。

**なぜコードのリポジトリで切るのか。**`verifiedRepo` は
**「置き場所のパスから引いた `<owner>/<repo>` を ghq に通した clone」と
「git が答えた共通ディレクトリ」が同じであること**を検算に使っている。
worktree は fork の clone から切るので、**パスが issue のリポジトリのままだと、
この検算は必ず落ちる**（fork の clone と issue のリポジトリの clone は別物である）。
**検算を緩めるのではなく、パスの意味を「コードの置き場所」に揃える。**

**4階層は増やさない。**[internal/workspace/scan.go:14](../../../internal/workspace/scan.go#L14) の
`const scanDepth = 4` を、走査
（[scan.go:38](../../../internal/workspace/scan.go#L38)・[scan.go:76](../../../internal/workspace/scan.go#L76)）と
パスの分解（[repo.go:70](../../../internal/workspace/repo.go#L70)・[broken.go:180](../../../internal/workspace/broken.go#L180)
の `len(parts) != scanDepth`）が使っている。
[docs/plans/continuo_design.md](../continuo_design.md) 3-22 の gwq との互換も4階層に依存している。
**5階層目を足すと、既にある worktree が全部「置き場所の規則に合わない」になる。**

**「どの issue か」は、パスの最下層が既に持っている。新しいコマンドは作らない。**
スラグは issue から作るので（`herdr.worktree.branch_template` の既定は
`continuo/{{.issue.owner}}/{{.issue.repo}}/{{.issue.number}}`。
[internal/config/default.go:164](../../../internal/config/default.go#L164)）、
`~/worktrees/github.com/myorg/project/continuo-myorg-internal-tasks-42` は
**2・3階層目がコードのリポジトリ、最下層が issue** である。**両方が1本のパスに出ている。**

**`continuo status` は作らない。**存在しないコマンドである。
[internal/cli/cli.go:172-186](../../../internal/cli/cli.go#L172-L186) の subcommand は
`hook` / `init` / `setup` / `doctor` / `trust` / `abandon` / `allow-keychain-access` / `version` の
8つだけであり、**13 / 13b の触るものの表にも `internal/cli` は1行も無い。**
**走っている run の一覧は、ダッシュボードが identifier の昇順で既に出している**
（[internal/server/view.go:142](../../../internal/server/view.go#L142)）。
**この設計では、そこに1行も足さない。**

**`branch_template` から issue の owner/repo を外した人は、パスから issue を引けなくなる。**
そのときの手掛かりは pane の label（`<owner>/<repo>/issues/<番号>`。3-3）である。
**8c が復元で頼るのと同じものであり、新しい仕組みではない。**

---

## 8b. 検算の錨は「パス＋スラグ」。コードのリポジトリはトラッカーが決める

**言いたいこと。**エージェントが書き換えられないのはパスだけである。
**パスの `<owner>/<repo>` がコードのリポジトリになると、
「このパスは本当にこの issue のものか」を身元ファイルだけでは言えなくなる。**
**照合の相手を、身元ファイルからトラッカーへ移す。**

**いま照合している3箇所。**どれも「パスの `<owner>/<repo>`」を
**issue の `<owner>/<repo>`** と比べている。

| どこ | いま何と比べているか |
| --- | --- |
| [internal/abandon/abandon.go:517-531](../../../internal/abandon/abandon.go#L517-L531) | 消す相手の issue の `Owner` / `Repo` |
| [internal/orchestrator/restore.go:278-295](../../../internal/orchestrator/restore.go#L278-L295) | 身元ファイルの `issue_url` から取り出した `<owner>/<repo>` |
| [internal/orchestrator/restore.go:311-321](../../../internal/orchestrator/restore.go#L311-L321) の `issueAgreesWithPath` | **取り直した issue の `Owner` / `Repo`**（[:665](../../../internal/orchestrator/restore.go#L665) と [:845](../../../internal/orchestrator/restore.go#L845) から呼ばれる） |

**変えかた。****照合を2段に分ける。**それぞれ、**その時点で手に入る材料だけ**を使う。

| どこで | 何と何を比べるか |
| --- | --- |
| `pathAgrees`（[restore.go:277](../../../internal/orchestrator/restore.go#L277)。**段2**） | **パスの最下層のディレクトリ名**と、**身元ファイルが名乗る issue から作り直したスラグ** |
| `issueAgreesWithPath`（[restore.go:311](../../../internal/orchestrator/restore.go#L311)。**段3のあと**） | **パスの2・3階層目**と、**トラッカーが答えたコードのリポジトリ** |

**段2 でトラッカーを引くことはできない。**`pathAgrees` は `scanIdentities` の中で呼ばれ
（[restore.go:221](../../../internal/orchestrator/restore.go#L221) の
`owner, repo, ok := o.pathAgrees(w.Path, w.Identity)`）、
**トラッカーの取り直し（段3 の `refetchByIdentities`）はその結果を入力に取る**
（[restore.go:121-124](../../../internal/orchestrator/restore.go#L121-L124)）。
**段2 で「トラッカーが答えたコードのリポジトリと比べる」と書くと、答えが1件も無いので
cross-repo の worktree は毎回そこで落ち、一度も引き継がれない。**

**そこで `pathAgrees` から owner/repo の比較を外し、スラグの比較に置き換える。**
既定の `branch_template` は issue の owner・repo・番号の3つを含むので
（[internal/config/default.go:164](../../../internal/config/default.go#L164)）、
**`issue_url` を別の issue に差し替えると、作り直したスラグがディレクトリ名と合わなくなる。**
`pathAgrees` が守っているのはそこである（restore.go:660-664 の「無関係の issue の pane を閉じる」）。
**弱くなっていない。**同じ形が [internal/abandon/abandon.go:534-551](../../../internal/abandon/abandon.go#L534-L551) に既にあり、
`ExpectedSlugFor` の答えと `filepath.Base` を突き合わせている。
**cross-repo でもスラグは issue から作られるので、この比較はそのまま成立する。**

**`pathAgrees` が返す owner/repo は、いままでどおり置き場所の2・3階層目である。**
8 のあとはそこにコードのリポジトリが入る。**`issueAgreesWithPath` は
`strings.EqualFold(issue.Owner, c.Owner)` を `Issue.CodeRepoNameWithOwner` との比較へ変える。**
変えないと、cross-repo の run は復元のたびに偽になり、pane も worktree も残したまま
**毎回「置き場所と違うリポジトリ」の WARN が出るだけで、一度も引き継がれない。**

**取り直せなかった issue には、新しい落とし方を作らない。**段3 で取れなければ
[restore.go:124](../../../internal/orchestrator/restore.go#L124) の `fetchFailed` が立ち、
**いまも引き継ぎの判断へ進まない。**`issueAgreesWithPath` は呼ばれない。

**身元ファイルの `code_repo`（11c）は照合に使わない。**エージェントが書き換えられるからである。
**候補を絞る手掛かりにも使わない。**使うと、書き換えるだけで候補から外せてしまう。

**これは検算を緩めていない。むしろ1段増えている。**
いまは「パス ↔ 身元ファイルの自称」を比べているが、**身元ファイルは worktree の直下にあり、
エージェントが書き換えられる。**トラッカーの答えは worktree の外にあり、書き換えられない。

**エージェントが `gh issue develop` でリンクを足したらどうなるか。**
**次の巡回でコードのリポジトリの答えが変わり、パスと食い違う。**
**食い違いは「候補から外す」であって「消す」ではない**ので、
worktree も branch も1バイトも失われない。**人間には1行で知らせる**（11f）。

---

## 8b2. `continuo abandon` はトラッカーを引かない

**言いたいこと。**abandon の照合は**いまのままで cross-repo に通る。**
[internal/abandon/abandon.go:534-551](../../../internal/abandon/abandon.go#L534-L551) が
**既にスラグを突き合わせている。**消すのは owner/repo の比較1つだけである。

**変えるのは1箇所。**[internal/abandon/abandon.go:525-532](../../../internal/abandon/abandon.go#L525-L532) の

```go
if !strings.EqualFold(owner, r.issue.Owner) || !strings.EqualFold(repo, r.issue.Repo) {
```

**を消す。**置き場所の2・3階層目はコードのリポジトリになるので、
人間が打った issue の owner/repo と一致しなくなる。**残すスラグの比較のほうが強い**（8b）。

**トラッカーは引かない。**[internal/abandon/deps.go:109-113](../../../internal/abandon/deps.go#L109-L113) の
「トラッカーは遅延して呼ぶ」を1文字も変えない。**abandon は片付けの途中で落ちた後始末に使う道具であり、
issue がボードから外れていても動かなければならない。**トラッカーを引く設計にすると、
**ボードから外れた issue で abandon が使えなくなる。**

**clone を引く相手は、照合を通った worktree のパスから取る。**
`FindIssueBranch` / `DeleteIssueBranch` が使う clone
（[internal/workspace/issuebranch.go:77](../../../internal/workspace/issuebranch.go#L77) の `clonePath`）には、
**`OwnerRepoOf(w.Path)` が返す2・3階層目**を `IssueRef.CodeOwner` / `CodeRepo` として渡す。
**パスはエージェントが書き換えられない**ので、身元ファイルの `code_repo` は使わない。

**worktree が1件も無いときは、issue のリポジトリで引く**（`abandonOrphanBranch`。
[abandon.go:588](../../../internal/abandon/abandon.go#L588)）。**手掛かりがパスしか無いのに、
そのパスが無いからである。**コードのリポジトリが違えば `ErrCloneNotFound` になり、
[abandon.go:588-592](../../../internal/abandon/abandon.go#L588-L592) が
**「調べられなかった」と言って branch を残す。**
**消す側へは倒れない**ので、ここに新しい仕組みを足さない。

---

## 8c. 身元ファイルが壊れた worktree の復元は、pane の label に頼る

**言いたいこと。**復元（3-49）は、パスから `<owner>/<repo>#<番号>` を組み立てて issue を引き直す。
**コードのリポジトリが issue のリポジトリと違うと、その組み立てが違う相手を指す。**
**pane の label が残っていれば復元できる。無ければ復元しない（消しもしない）。**

**いまの組み立て。**[internal/orchestrator/restore.go:1175-1200](../../../internal/orchestrator/restore.go#L1175-L1200)

```go
identifier := fmt.Sprintf("%s/%s#%d", b.Clue.Owner, b.Clue.Repo, number)
```

**`b.Clue.Owner` / `b.Clue.Repo` は置き場所の2階層目と3階層目である。**
8 のあとは、そこにコードのリポジトリが入る。

**手掛かりを3つに整理する。**

| 手掛かり | 何が取れるか | 使えるか |
| --- | --- | --- |
| **pane の label**（`<owner>/<repo>/issues/<番号>`。3-3） | **issue の owner / repo / 番号** | **使う。**herdr が持っており worktree の外にある |
| **置き場所の2・3階層目** | **コードのリポジトリ** | 裏取りに使う（下の照合） |
| **スラグ** | **番号だけ。**しかも issue の owner/repo が分かっているときに限る（下） | **owner と repo は取り出さない**（ハイフンで割れて曖昧。3-22 が同じ理由で禁じている） |

**スラグから番号を切り出すには、issue の owner/repo が先に要る。**
[internal/workspace/broken.go:204-207](../../../internal/workspace/broken.go#L204-L207) の
`issueNumberFromSlug` は、**渡された owner/repo を `branch_template` に流し込んで**
番号の前後の固定部分を作り、スラグと突き合わせる。
[broken.go:184-186](../../../internal/workspace/broken.go#L184-L186) はいま
**置き場所の2・3階層目（＝8 のあとはコードのリポジトリ）をそのまま渡している。**
**スラグは issue のリポジトリから作られているので、
2つが違う「既存 OSS への PR」では突き合わせが必ず外れ、`Number` が 0 になる。**

**変えることは4つ。**

| 何を | どう変えるか |
| --- | --- |
| `issueNumberFromPaneLabel`（[restore.go:1340-1349](../../../internal/orchestrator/restore.go#L1340-L1349)） | **owner / repo / 番号の3つを返す形にする。**いまは4つに割った `parts[0]` / `parts[1]` を捨てて番号だけ返している。**新しい parse は要らない** |
| `recoveryNumbers`（[restore.go:1290](../../../internal/orchestrator/restore.go#L1290)）と、それを使う `identifier` の組み立て（[restore.go:1188](../../../internal/orchestrator/restore.go#L1188) の `fmt.Sprintf("%s/%s#%d", b.Clue.Owner, b.Clue.Repo, number)`） | **返す型を「owner / repo / 番号」の組にし、`identifier` はその組から作る。**置き場所の2・3階層目はコードのリポジトリなので、**そこから組み立てると実在しない issue を引きに行く** |
| `issueNumberFromSlug` に渡す owner/repo | **pane の label から取った issue の owner/repo にする。**label が無ければ渡さず、`Number` は 0 のままにする |
| `PathClue.IssueURL()` / `Identifier()`（[broken.go:73-90](../../../internal/workspace/broken.go#L73-L90)） | **置き場所の owner/repo が issue のものだと言えないときは空文字を返す。**コードのリポジトリで `<owner>/<repo>#<番号>` を組み立てると、**実在しない issue を名乗る** |

**2つ目を落とすと、直したことにならない。**`issueNumberFromSlug` に正しい owner/repo を渡しても、
**issue を引くときの identifier は `b.Clue.Owner` / `b.Clue.Repo` から組まれたままだからである。**
cross-repo では `found=false` で終わり、**一度も復元されない。**

**`PathClueOf` の引数を増やす。**[internal/workspace/broken.go:173](../../../internal/workspace/broken.go#L173) の
`PathClueOf(worktreePath string)` に **issue の owner/repo（pane の label から取ったもの。無ければ空）**を足す。
呼び出し元は2つある。

| 呼び出し元 | どう渡すか |
| --- | --- |
| `ScanBroken`（[broken.go:260](../../../internal/workspace/broken.go#L260)） | **pane の一覧を引数で受け取り、cwd で引いた label から渡す** |
| `BrokenWorktreeGuidance`（[broken.go:152](../../../internal/workspace/broken.go#L152)） | **空を渡す。**pane を持たないので `Number` は 0 のままになり、[broken.go:129-133](../../../internal/workspace/broken.go#L129-L133) の「URL を捏造しない」分岐にそのまま落ちる |

**壊れたものを数える条件（[broken.go:267-269](../../../internal/workspace/broken.go#L267-L269) の
`Number <= 0` なら数えない）は変えない。**変えると起動が止まる。下の節で述べる。

**pane も label も無いときは、復元を1回も試さない。**`recoveryNumbers` が空を返すので
[restore.go:1187-1206](../../../internal/orchestrator/restore.go#L1187-L1206) のループが回らず、
**最後の WARN（「手掛かりから issue を確かめられないので復元できません（消しません）」）だけが出る。**
**worktree は残す。消さない。**

**照合。**[internal/orchestrator/restore.go:1310-1330](../../../internal/orchestrator/restore.go#L1310-L1330) の
`slugAgrees` が比べる相手を変える。

```text
いま: 引き直した issue の Owner/Repo  ==  置き場所の Owner/Repo
これから: 引き直した issue の コードのリポジトリ  ==  置き場所の Owner/Repo
（スラグの比較は変えない）
```

---

## 8c2. 数える条件を広げない理由 — 広げると既定で起動が止まる

**言いたいこと。**「スラグが `branch_template` の固定部分で始まるか」まで広げると、
**復元できないものがそのぶん増え、`workspace.on_broken_worktree` の既定（`stop`）で
continuo が起動しなくなる。**だから広げない。

**なぜ増えるか。**cross-repo の worktree では、置き場所の2・3階層目がコードのリポジトリなので、
**`issueNumberFromSlug` は突き合わせに失敗して `Number` が 0 になる**（上）。
pane が残っていればそちらから引けるが、**pane が無ければ手掛かりが1つも無い。**
数える条件を広げると、その worktree が「壊れたもの」に積まれ、
**復元できないまま [internal/orchestrator/restore.go:1043-1045](../../../internal/orchestrator/restore.go#L1043-L1045) が
起動を止める**（[internal/config/default.go:92-94](../../../internal/config/default.go#L92-L94) の
`OnBrokenWorktree: OnBrokenWorktreeStop`）。

**残る帰結を書いておく。**条件を広げないので、
**身元ファイルも pane も無い cross-repo の worktree は、いまと同じく「人間が置いたもの」として数えられない。**
報告にも復元にも出ない。**そのかわり起動は止まらず、worktree も branch も1バイトも消えない。**

**そこから出る道は2つある。**どちらも [docs/FAQ.md](../../FAQ.md) に書く。

| 道 | 何をするか |
| --- | --- |
| **身元ファイルを手で書く** | `<worktree>/.continuo.json` を 11c のサンプルの形で置く。次の起動で普通の候補になる |
| **worktree を手で消す** | 成果を push したあとで `git worktree remove` する |

**身元ファイルが無くなるのは、着手の段6〜段9 の途中で落ちたときだけである**（3-16）。
**そのとき pane はまだ生きている**（段5 で作り、段9 まで閉じない）ので、
**この穴が開くのは「落ちたあとに人間が pane を閉じた」場合に限られる。**

**`on_broken_worktree: skip` を選べば、この worktree があっても起動は続く**
（[internal/config/types.go:249-252](../../../internal/config/types.go#L249-L252)）。
**既定を変えることは、この設計では提案しない。**

---

## 9. 片付けを4段にする

**言いたいこと。**`git push origin HEAD:<別名>` が upstream を張らないことを、
**プロンプトで直し（`-u` を必ず付けさせる）、判定でも受け止める。**
**判定の中心を「HEAD が remote に載っているか」に移す。**

**いまの判定。**[internal/workspace/cleanup.go:751-782](../../../internal/workspace/cleanup.go#L751-L782)
は upstream が無ければ base との差分だけを見る。**commit を積んでいれば差分は必ず残るので、
push 先を分けた worktree は永久に片付かない。**

**新しい判定（`cleanup.require_pushed` が真のとき）。**

| 段 | 何を見るか | 結果 |
| --- | --- | --- |
| **1** | `git for-each-ref --count=1 --contains HEAD refs/remotes/` | **1行でも返れば消してよい。**HEAD は remote に載っている |
| **2** | upstream があれば `git rev-list --count @{u}..HEAD` | **理由を数で言うために見る**（「push されていない commit が n 件残っている」） |
| **3** | upstream が無く base があれば `git diff --quiet <base>...HEAD` | 差分が無ければ消してよい（**いまの判定をそのまま残す**） |
| **4** | 段1が偽で、upstream も base も無い | **消さない。**いまの文面（[cleanup.go:767-771](../../../internal/workspace/cleanup.go#L767-L771)）をそのまま出す |

**どの段にも当たらない場合は無い。**段1が偽なら upstream の有無で段2・段3へ、
そのどちらでもなければ段4へ落ちる。**段4を書かないと、base を復元できなかった worktree
（[internal/orchestrator/restore.go:1216](../../../internal/orchestrator/restore.go#L1216) が
「`base` と `settings_path` は復元しない」と決めている）が、見送りの理由を1行も持たないまま
片付けの対象になる。**

**段1が段2より前にある。**逆にすると、
「upstream は1本目の PR の branch のままで、2本目を別名へ push した」worktree が
`@{u}..HEAD` の件数だけで見送られる（6 のユースケース）。

**段1が偽なら段2も必ず偽である**（upstream もリモート追跡 ref の1つだから）。
**段2は理由の文面を作るためだけに残す。**

**段1は通信しない。**`refs/remotes/` は手元にある ref である。
`--contains` は history を辿るので ref の数に比例するが、
**このリポジトリ（remote の ref が52本）で 0.012 秒**であった（2026-09-01 実測）。
**ctx は段2・段3 と同じものを渡す**（[internal/workspace/git.go:694](../../../internal/workspace/git.go#L694)
の `gitHasUpstream` と同じ形）。

**段3を消さない。**remote を1つも持たない clone（人間が手で作った）では
`refs/remotes/` が空になり、段1が常に偽になる。そのとき base との差分が唯一の手掛かりである。

**段1の見落としが起きる条件。**リモート追跡 ref を記録したあとに、**remote 側でその commit が
消された**場合（force push・branch の削除）。**そのときは「載っていた」と判定して worktree を消す。**
**受け入れる。**同じことは upstream を使ういまの判定でも起きる（`@{u}` は fetch した時点の記録である）。

---

## 10. `git fetch` は「worktree を作る直前」にだけ、1本だけ叩く

**言いたいこと。**巡回のたびに通信すると、遅い回線で30秒の巡回が詰まる。
**叩くのは新しく worktree を作るときだけ**で、**リンクされた branch が手元に無いときだけ**、
**その1本だけ**である。

**いま `git fetch` は1回も呼ばれていない。**
検索パターン `"fetch"`、対象パス `internal/` と `cmd/`（`--include='*.go'`）、
対象コミット 73fb41a で **0件**である。

```console
$ grep -rn '"fetch"' --include='*.go' internal/ cmd/ | wc -l
       0
```

**叩く条件（3つ全部そろったときだけ）。**

| 条件 | 理由 |
| --- | --- |
| **`resolveBase` がリンクされた branch を返した** | 設定の base も既定 branch も手元にある |
| **`refs/remotes/origin/<名前>` が無い** | あるなら通信しない |

**`resolveBase` は2箇所から呼ばれる。両方に同じ fetch を掛ける。**

| どこ | いつ通るか |
| --- | --- |
| [internal/workspace/prepare.go:227-229](../../../internal/workspace/prepare.go#L227-L229)（段4・段5 の `default:` の枝） | worktree を新しく作るとき |
| [internal/workspace/prepare.go:243-249](../../../internal/workspace/prepare.go#L243-L249)（`if result.Base == ""` の中の `resolveBase`。[:247](../../../internal/workspace/prepare.go#L247)） | **再利用の枝を通ったあと。**片付けの手順2b が base を要るので、既存の worktree でも決め直す |

**再利用の枝を書かないと、手元に無い ref が base に入る。**
新しい `resolveBase` はリンクが1本なら `origin/<名前>` を返すので、
**fetch を通っていない worktree の身元ファイルに「手元に無い base」が書かれる。**
片付けの段3（`git diff --quiet <base>...HEAD`。
[internal/workspace/git.go:733-734](../../../internal/workspace/git.go#L733-L734)）が
その base を解決できず、**判定できないまま見送り続ける。**

**通信の回数は増えない。**再利用の枝でも「`refs/remotes/origin/<名前>` が無いときだけ」なので、
**一度取ったあとは1本も叩かない。**

**叩くもの。****refspec を明示した形だけを叩く。**

```bash
git -C <clone> fetch --no-tags origin \
  '+refs/heads/<リンクされた branch>:refs/remotes/origin/<リンクされた branch>'
```

**refspec を書かない素の `git fetch origin <名前>` にしない。**
`--single-branch` で作られた clone では FETCH_HEAD しか動かず、
**リモート追跡 ref ができないので worktree が切れない**（3 の実測2・3）。
**素の clone でも同じ結果になる**ので、clone の作られ方で場合分けしない。

**`--no-tags` を付ける。**tag は base の解決に要らず、大きなリポジトリでは
tag の取得だけで数秒かかる。**`--all` も `--prune` も付けない。**

---

## 10b. fetch の上限・失敗したときの行き先・叩いてはいけない場所

**言いたいこと。**通信を1つ足すので、**止まらないこと**と**落ちたときに人間へ届くこと**を
同時に決める。**ログだけにして黙って飛ばす経路を作らない。**

**頻度の見積り。**worktree を作るのは issue 1件につき1回である。
巡回（既定 30 秒。[internal/config/default.go:87](../../../internal/config/default.go#L87)）の
うち、**新しく着手する issue が無い回は0回**になる。

**上限。**`workspace.fetch_timeout_ms`（新設。既定 30000）。
**上限が無いと、遅い回線で巡回のループごと止まる**（`trustCheckTimeout` と同じ理由。
[internal/workspace/trust.go:23](../../../internal/workspace/trust.go#L23)）。

**失敗したらどうするか。****その issue を `failure_state` にして、issue へコメントする。**
3-34b が「判定できない事情は段3 に任せて `failure_state` と issue のコメントで人間へ渡す」と
決めているのに従う。**黙ってログだけにしない。**

**やり直しは1回だけ。**間隔は1秒。2回落ちたら上のとおり人間へ渡す。

**段0（`CheckWorktreeUsable`）では絶対に叩かない。**あそこはボードの候補ぜんぶに対して
毎巡回で走る。**1件でも通信すると、候補の数だけ通信が増える。**

---

## 11. トラッカーのクエリを広げ、`Issue` に4つ足す

**言いたいこと。**GraphQL の1行を広げ、`Issue` に4つ足す。
**`Issue.BranchName` は取られたまま誰も読んでいないので、そこへ入れ直す。**

**取られたまま捨てられている証拠。**検索パターン `BranchName`、
対象パス `internal/` `cmd/` `test/`（`--include='*.go'`）、対象コミット 73fb41a で **4件。**
[query.go:1041](../../../internal/tracker/query.go#L1041) と
[:1083](../../../internal/tracker/query.go#L1083) が代入、
[tracker.go:182](../../../internal/tracker/tracker.go#L182) と
[:185](../../../internal/tracker/tracker.go#L185) が型の定義であり、**読み出しは0件である。**

**クエリを広げる。**[internal/tracker/query.go:54](../../../internal/tracker/query.go#L54)

```graphql
linkedBranches(first: 5) { totalCount nodes { ref { name
  repository { nameWithOwner url defaultBranchRef { name } parent { nameWithOwner } } } } }
```

**`url` を落とさない。**8 の置き場所の1階層目（`<host>`）はここからしか取れない。
落とすと、コードのリポジトリが別のホストにある形を扱えなくなる。

**`first: 5` にする理由。**1本だけ取ると「2本ある」ことに気づけない。
**2本以上の扱いを決めるには、2本目が見えていなければならない**（下の表）。

**`totalCount` を必ず一緒に取る。**取らないと、**6本目が別のリポジトリを指していても気づけない。**
先頭5本がたまたま同じリポジトリなら、11a の「別々のリポジトリなら着手しない」が働かず、
**別のリポジトリで作業を始めてしまう。**
**`totalCount` を足しても connection は1本のままなので、費用は増えない**
（[internal/tracker/query.go:52](../../../internal/tracker/query.go#L52) の
`assignees(first: 10) { totalCount nodes { id login } }` が同じ形で既に取っている）。

**`Issue` に足す4つ。**[internal/tracker/tracker.go:185](../../../internal/tracker/tracker.go#L185) の隣。

```go
// CodeRepoNameWithOwner はコードのリポジトリである（**`<owner>/<repo>` の1本の文字列**）。
// **リンクが0本なら、issue のリポジトリと同じ値が入る。**空にはしない。
CodeRepoNameWithOwner string
// CodeRepoHost はコードのリポジトリの URL のホスト部である（`ref.repository.url` から取る）。
// **リンクが0本なら、issue の URL のホスト部と同じ値が入る。**空にはしない。
CodeRepoHost string
// CodeRepoDefaultBranch はコードのリポジトリの既定 branch である。
CodeRepoDefaultBranch string
// PRTarget は PR の宛先である（fork なら派生元。**`<owner>/<repo>`**）。
// **fork でなければ CodeRepoNameWithOwner と同じ。**
PRTarget string
```

**`CodeRepoNameWithOwner` という長い名前にする理由。**11b の `IssueRef.CodeRepo` は
**リポジトリ名だけ**（`project`）を指す。同じ `CodeRepo` を両方に置くと、写しで分解を忘れたときに
置き場所が `~/worktrees/github.com/myorg/myorg-project/…` になる
（[internal/workspace/layout.go:214-217](../../../internal/workspace/layout.go#L214-L217) の
`pathComponent` がスラッシュをハイフンへ潰すので、**落ちずに間違った場所へ作られる**）。
**名前で見分けられる形にして、その間違いを起こせなくする。**

**`BranchName` は、リンクがちょうど1本のときだけ埋める。**
0本・2本以上では **nil にする。**
いま [internal/tracker/query.go:968-974](../../../internal/tracker/query.go#L968-L974) は
`Nodes[0]` を無条件に使っているので、**そこも直す。**
2本以上で1本目を残すと、11a が「リンクを base に使わない」と決めた場合でも
11d の `.push_branch` にその1本目が載り、**エージェントが押し付けられた branch へ push する。**

---

## 11a. リンクの本数で振る舞いを変える。base を決める順番

**言いたいこと。**リンクは0本・1本・2本以上の3通りしかない。
**2本以上が別々のリポジトリを指していたときだけ、着手しない。**
それ以外は必ず値が決まる。**推測はしない。**

| リンクの本数 | コードのリポジトリ | base |
| --- | --- | --- |
| **0本** | issue のリポジトリ | 今までどおり（設定 → 既定 branch） |
| **1本** | そのリンクの `ref.repository.nameWithOwner` | **`origin/` + そのリンクの `ref.name`** |
| **2本以上・全部同じリポジトリ** | そのリポジトリ | **リンクを使わない。**コードのリポジトリの既定 branch（**`BranchName` は nil にする**） |
| **2本以上・別々のリポジトリ** | **決めない。着手しない**（下） |
| **`totalCount` が 5 を超える** | **決めない。着手しない。**6本目以降が見えていないので、別々のリポジトリでないと言えない（文面1 と同じ扱い） |

**base には `origin/` を付ける。**`resolveBase` が返す値は
`PrepareResult.Base` にそのまま入り、`git worktree add` の起点と
`git diff --quiet <base>...HEAD`（[internal/workspace/git.go:733-734](../../../internal/workspace/git.go#L733-L734)）の
両方に渡る。**どちらもローカルには無い名前を解決できない。**
`normalize.Normalize` はスラッシュを通すので（[internal/normalize/normalize.go:67](../../../internal/normalize/normalize.go#L67)）、
**`origin/work/issue-42` はそのまま `SafeName` になる。**

**別々のリポジトリを指す2本があったら、その issue に着手しない。**
Status を1バイトも書かずに飛ばし、issue へ1回だけコメントする（11e の文面1）。
**勝手にどちらかを選ぶと、別のリポジトリで作業を始めてしまう。**

**base を決める順番**（[internal/workspace/prepare.go:369-387](../../../internal/workspace/prepare.go#L369-L387) の `resolveBase`）。

| 順 | 何を base にするか |
| --- | --- |
| 1 | `herdr.worktree.base`（設定に明示があれば、いつでもこれが勝つ） |
| 2 | リンクが1本のとき、その branch（`origin/<名前>`） |
| 3 | `IssueRef.CodeDefaultBranch` が空でなければ、それ |
| 4 | **コードのリポジトリ＝issue のリポジトリのときに限り** `NativeRef["default_branch"]` |
| 5 | どれも無ければ `ErrBaseUnknown`（**推測しない**） |

**段4に「同じときに限り」を付ける理由。**`NativeRef["default_branch"]` は
[internal/tracker/query.go:1008-1010](../../../internal/tracker/query.go#L1008-L1010) が
**issue のリポジトリの** `defaultBranchRef` から入れている値である。
コードのリポジトリが違うのにここへ落ちると、**fork の clone で
「issue のリポジトリの既定 branch」という別物の名前を base にしようとする。**
**その組み合わせは必ず `ErrBaseUnknown` にする**（[internal/workspace/prepare.go:369-387](../../../internal/workspace/prepare.go#L369-L387) の `resolveBase`）。

**設定の base は、リポジトリをまたいで効いてしまう。**fork を使うボードでは
`herdr.worktree.base` を null のままにすること（[docs/FAQ.md](../../FAQ.md) に1行足す）。

---

## 11b. worktree へ運ぶ型（`IssueRef`）に足す5つ

**言いたいこと。**workspace はトラッカーを知らないので、写した値でしか受け取れない。
**足すのは5つ。**どれも「空なら今までどおり」に倒して、既存の呼び出しを壊さない。

**`IssueRef` に足すもの。**[internal/workspace/workspace.go:237-256](../../../internal/workspace/workspace.go#L237-L256)

```go
// CodeOwner はコードのリポジトリの所有者名である（**所有者名だけ**）。**空なら Owner を使う。**
CodeOwner string
// CodeRepo はコードのリポジトリ名である（**リポジトリ名だけ。`<owner>/<repo>` ではない**）。
// **空なら Repo を使う。**
CodeRepo string
// CodeHost はコードのリポジトリの URL のホスト部である。**空なら issue の URL から取る。**
CodeHost string
// LinkedBranch は base に使うリンクされた branch の名前である。**無ければ空。**
LinkedBranch string
// CodeDefaultBranch はコードのリポジトリの既定 branch である。**無ければ空。**
CodeDefaultBranch string
```

**`NativeRef` には入れない。**あそこは「orchestrator が中身を解釈しない」場所であり、
`default_branch` の1キーだけが例外だと 3-22 が明記している。**例外を増やさない。**

**[internal/orchestrator/dispatch.go:1151-1161](../../../internal/orchestrator/dispatch.go#L1151-L1161) の
`toIssueRef` が、`Issue` から上の5つを写す。**
**`CodeOwner` と `CodeRepo` は、`Issue.CodeRepoNameWithOwner` を
最初の `/` 1つだけで割って入れる**（`strings.Cut`）。割れなければ両方とも空にして
「今までどおり `Owner` / `Repo` を使う」に倒す。**残りの `/` はリポジトリ名の側に含めない**
（GitHub のリポジトリ名にスラッシュは入らない。入っていれば、それはコードのリポジトリの値ではない）。

---

## 11c. 身元ファイルと `WORKFLOW.md` に書くもの

**言いたいこと。**身元ファイルに3つ足す。`WORKFLOW.md` に足すのは1つだけである。
**どちらも既存のキーは1つも変えない。**

**身元ファイル**（`<worktree>/.continuo.json`。名前は `workspace.identity_file` で変えられる）。
**continuo が着手の段6 で書き、段9 で `agent_name` を追記する**（3-18）。
**足すのは下の3つで、既存のキーは変えない。**

```json
{
  "issue_url": "https://github.com/myorg/internal-tasks/issues/42",
  "issue_identifier": "myorg/internal-tasks#42",
  "code_repo": "myorg/project",
  "pr_target": "upstream-org/project",
  "linked_branch": "work/issue-42",
  "branch": "continuo/myorg/internal-tasks/42",
  "base": "origin/work/issue-42",
  "project_item_id": "PVTI_lAHNNEjOAYV2fM4N9wYE",
  "herdr_workspace_id": "ws_01H...",
  "socket_path": "~/.continuo/run/continuo.sock",
  "settings_path": "~/.continuo/settings/continuo-myorg-internal-tasks-42.json",
  "agent_name": "continuo-myorg-internal-tasks-42",
  "session_uuid": "6f1f2c1e-0000-4000-8000-000000000000",
  "created_at": "2026-09-01T12:00:00Z",
  "takeover_count": 0
}
```

**これが実際に書かれる全キーである。**
[internal/workspace/identity.go:53-108](../../../internal/workspace/identity.go#L53-L108) で
`omitempty` / `omitzero` が付くのは `herdr_repo_workspace_id` と `cleanup_deferred_at` の2つだけで、
**残りは値が空でも必ず出る。**8c が [docs/FAQ.md](../../FAQ.md) へ足す「人間が手で書く」手順は、このサンプルを指すこと。

**`base` と `linked_branch` は形が違う。**`base` は `origin/` を付けた
リモート追跡 ref の名前（`origin/work/issue-42`）であり、
`linked_branch` は**リンクされた branch の生の名前**（`work/issue-42`）である。
**`linked_branch` はプロンプトの `.push_branch`（11d）へそのまま渡るので、`origin/` を付けない。**

**足した3つ（`code_repo` / `pr_target` / `linked_branch`）は、判断に1つも使わない。**
**人間が読むためだけに書く**（判断に使う値は 8b のとおりトラッカーから取る）。
**この3つを読む continuo のコードは1行も作らない。**

**`WORKFLOW.md` に足すもの。**1つだけ。

```yaml
workspace:
  root: ~/worktrees
  identity_file: .continuo.json
  fetch_timeout_ms: 30000   # リンクされた branch を取りに行くときの上限
```

**`continuo init` が書く既定値も同じにする**
（[internal/scaffold/template.go](../../../internal/scaffold/template.go) の
front matter と [internal/config/default.go:89-95](../../../internal/config/default.go#L89-L95)）。
**[internal/config/types.go](../../../internal/config/types.go) の `WorkspaceConfig` にフィールドを足し、validate も足す。**

**[docs/plans/continuo_design.md](../continuo_design.md) 5-2 の ```yaml ブロックにも同じ行を足す。**
足さないと `TestTemplate_雛形のキー構成が設計5_2の設定例と一致する` が
「雛形にしか無いキーがある」で必ず落ちる。
[test/internal/scaffold/design_template_test.go:35-40](../../../test/internal/scaffold/design_template_test.go#L35-L40) が
**5-2 の ```yaml ブロックと雛形の front matter を、キーの集合として完全一致で突き合わせている**
（[:149-172](../../../test/internal/scaffold/design_template_test.go#L149-L172) の `assertSameKeySet`）。
**値は比べないので、コメントの文言は揃えなくてよい。**

---

## 11d. プロンプトへ渡す変数に足す4つ

**言いたいこと。**エージェントは「どこへ push するか」「PR をどこへ出すか」を
**プロンプトからしか知れない。**その4つを変数で渡す。
**リンクが0本なら、4つとも今までと同じ値になる。**

[internal/orchestrator/prompt.go:39-51](../../../internal/orchestrator/prompt.go#L39-L51) の `data`。

| 変数 | 中身 | リンクが0本のとき |
| --- | --- | --- |
| `.code.name_with_owner` | コードのリポジトリ（`myorg/project`） | issue のリポジトリと同じ |
| `.code.owner` / `.code.repo` | その分解 | 同上 |
| `.pr_target` | PR の宛先（`upstream-org/project`） | コードのリポジトリと同じ |
| `.push_branch` | **リンクされた branch の名前**（`work/issue-42`）。**push 先の既定ではない**（14 の推奨） | **空文字** |

**`.push_branch` が空文字になるのは「`Issue.BranchName` が nil のとき」である**（11 のとおり、
リンクが0本のときと2本以上のときの両方が nil になる）。**「リンクが0本のとき」と書かない。**

**push 先の既定は、いつでも `git push -u origin HEAD` である**（14 の推奨）。
`.push_branch` は「**別の名前へ出せと issue に書かれていたときの候補**」として渡す。
**「必ずここへ push しろ」と読める文言を本文に書かない**（12 の貼り先2）。
**base と push 先を同じものに固定すると、6 のユースケース（1つの issue で PR を複数）が書けなくなる。**

**5-3 の変数の表は、機械では検査されていない。**
[test/internal/scaffold/design_template_test.go:99-101](../../../test/internal/scaffold/design_template_test.go#L99-L101) の
`TestTemplate_雛形の本文が設計5_3の本文と一致する` が突き合わせるのは、
**5-3 の ```markdown ブロック（本文）だけ**である
（[:237-240](../../../test/internal/scaffold/design_template_test.go#L237-L240) の
`readDesignBodyExample` がそのブロックを読む）。表は人間が読むためのものなので、忘れても落ちない。
**だから 11d の変数を足したときは、表を直したことを PR の説明で名指しして確かめる。**

**`missingkey=error` を理由に挙げない。**この設定
（[internal/orchestrator/prompt.go:30](../../../internal/orchestrator/prompt.go#L30)）が落とすのは
**テンプレートが `data` に無いキーを参照したとき**であり、**`data` にキーを足すこと自体は
既存のテンプレートを1つも壊さない。**危ないのは逆で、**本文に `{{.push_branch}}` を入れて
`data` に足さない側である**（13 の塊1）。

---

## 11e. 人間に見せる文面（着手しない・取ってこられない）

**言いたいこと。****新しく黙って落ちる経路を3つ作る。**その3つに文面を用意する。
**ログ1行で済ませない。**3-68 が「ログは pane を見ていない限り誰にも届かない」と名指ししている。

**重複を抑える仕組みは、3つとも同じである。**先に決める。

| 何 | どうする |
| --- | --- |
| 印の置き場所 | **メモリだけ**（3-68）。`o.notified`（[internal/orchestrator/orchestrator.go:299](../../../internal/orchestrator/orchestrator.go#L299)）と同じ形 |
| 鍵 | **issue の identifier ＋ 理由の種類。**理由が変わったら数え直す |
| いつ書くか | **同じ鍵で3回続けて落ち、かつ最初に落ちてから60秒以上たったとき**（3-68） |
| 通ったら | **印を消す**（`clearUntrusted` と同じ）。直したあと再発したら、もう一度知らせる |
| 再起動したら | **印は消える。**「1回の起動につき1回」になる。**その旨を本文の末尾に1行書く**（3-68 が要求している） |

**コメントの本文に HTML の目印を置かない。**置いても読み返せないからである。

| なぜ読み返せないか | 実装 |
| --- | --- |
| `postComment` が本文の先頭に `self_marker` を付ける | [internal/orchestrator/comment.go:373-375](../../../internal/orchestrator/comment.go#L373-L375) |
| `FetchComments` が、自分が書いた `self_marker` 付きのコメントを結果から外す | [internal/tracker/adapter.go:1080-1082](../../../internal/tracker/adapter.go#L1080-L1082) の `continue` |

**だから「既に書いてあるか」を issue のコメントからは確かめられない。**
`cleanup_deferred_at` は身元ファイルに書く時刻であって、コメントを読み返す仕組みではない。
**引き合いに出さない。**

**`<!-- continuo:agent -->` も本文に書かない。**あれは
[internal/orchestrator/prompt.go:282](../../../internal/orchestrator/prompt.go#L282) が書いているとおり
`PostComment` が自分で付けるものであり、**本文に書くと二重になる。**

**依存する別の作業。**#134（ダッシュボードに「着手できずに止まっているもの」を出す）の設計が
[internal/orchestrator/gate.go](../../../internal/orchestrator/gate.go) に同じ仕組みを新設する。
**先に main へ入ったほうを使い、二重に作らない。**

**1. リンクが別々のリポジトリを指している（着手しない）。**
**この経路には worktree も身元ファイルもまだ無い。**11a のとおり `preflight` より前で止めるので、
**印を置ける場所はメモリしかない。**

```text
この issue には、別々のリポジトリの branch が2本以上リンクされています。
どちらのリポジトリで作業すべきかを continuo が決められないので、着手していません。

  myorg/project        work/issue-42
  other-org/project    hotfix/issue-42

Development のリンクを1本にしてから、Status を Ready に戻してください。
確かめ方: gh issue develop --list 42 --repo myorg/internal-tasks

この通知は、continuo の1回の起動につき1回だけ出ます。
```

**2. リンクされた branch を取ってこられなかった（`failure_state` にする）。**

```text
リンクされた branch を取ってこられなかったので、この issue に着手できませんでした。

  実行したもの: git -C ~/ghq/github.com/myorg/project fetch --no-tags origin
                '+refs/heads/work/issue-42:refs/remotes/origin/work/issue-42'
  git が言ったこと: fatal: could not read Username for 'https://github.com': terminal prompts disabled

回線か認証を直してから、Status を Ready に戻してください。
```

**文面2 だけは `failure_state` へ動かすので、3回・60秒を待たずにその場で書く。**
**Status が動くこと自体が記録になる**ので、同じ issue で二度書かれることはない。

**文面はすべて [internal/i18n/messages/ja.json](../../../internal/i18n/messages/ja.json) と
[internal/i18n/messages/en.json](../../../internal/i18n/messages/en.json) の両方へ入れ、
`en.json` の `_source_sha256` を入れ直す。**3本目は 11f。

---

## 11f. 文面3 — 置き場所とコードのリポジトリが食い違う

**言いたいこと。**候補から外すだけで、worktree も branch も消さない。
**それでも issue へ1回だけ書く。**Status が動かないので、書かないと誰にも届かない。

**issue へのコメントとログの両方を出す。****ログだけにしない。**
この経路は Status を動かさずに候補から外れ続けるので、**放っておくと
その issue は永久に着手されないまま、画面のどこにも理由が出ない。**3-68 が名指ししている形そのものである。

**コメントは、issue とこの理由の組につき1回だけ**（11e の冒頭で決めた仕組みをそのまま使う。
鍵は identifier ＋ 理由、3回・60秒、メモリだけ）。
**ここで3回・60秒を待つ意味は大きい。**ボードの候補一覧は GitHub のサーバ側の検索結果であり、
**索引の反映が遅れて1巡回だけ答えが揺れることがある**（3-34。
[internal/orchestrator/dispatch.go:198-200](../../../internal/orchestrator/dispatch.go#L198-L200) が
「直前に書いた値が索引へ反映される前に取り直すと」と書いて、同じ揺れに守りを置いている）。
**1回目で書くと、揺れただけの issue に誤った案内が付く。**

```text
この issue の worktree の置き場所が、いまリンクされているコードのリポジトリと食い違うので、
着手していません。**worktree も branch も消していません。**

  worktree      ~/worktrees/github.com/myorg/project/continuo-myorg-internal-tasks-42
  置き場所       myorg/project
  リンクの先     other-org/project

Development のリンクを元に戻すか、この worktree を手で片付けてから、Status を Ready に戻してください。

この通知は、continuo の1回の起動につき1回だけ出ます。
```

```text
level=WARN msg="worktree の置き場所がコードのリポジトリと食い違うので候補にしません（消しません）"
  path=~/worktrees/github.com/myorg/project/continuo-myorg-internal-tasks-42
  置き場所=myorg/project トラッカーが答えたコードのリポジトリ=other-org/project
```

**この文面も ja.json と en.json の両方へ入れ、`en.json` の `_source_sha256` を入れ直す**（11e と同じ）。

---

## 12. [internal/scaffold/template.go](../../../internal/scaffold/template.go) の本文に足す2つ

**言いたいこと。**足すのは「branch を切り替えるな」と
「push には `-u` を必ず付けろ」の2つである（PR の探し方は 12b）。
**同じ本文を [docs/plans/continuo_design.md](../continuo_design.md) 5-3 にも1文字違わず入れる**
（`TestTemplate_雛形の本文が設計5_3の本文と一致する` が突き合わせる）。

**貼り先1 は、[docs/plans/continuo_design.md](../continuo_design.md) 3-69 の3案のうち
「切り替えを禁じる」を採るという判断である。**3-69 は
「**どう扱うかが決まっていない**（2026-08-28 時点。人間の判断待ち）」のまま3案を並べている。
**この文書が、その1つを採る。**3-69 の節は決定済みに書き換える（未決定の表を消す。13 の塊1）。

**3-69 が「切り替えを禁じる」の損として挙げた「detached HEAD で同じ詰まりが残る」には、
既に答えがある。**[internal/workspace/prepare.go](../../../internal/workspace/prepare.go) の
`CheckWorktreeUsable` が detached HEAD を専用の番兵 `ErrWorktreeDetached` で断っており
（#132（detached HEAD の worktree を永久に飛ばし、実態と違うメッセージを出す））、
**「黙って詰まる」ではなく「番兵で名指しして止まる」になっている。**
**この設計では、そこに何も足さない。**

**貼り先1。**`## 終わったらやること` の中、
`**push 先は、この issue のために作られた branch です。**` の直前
（[internal/scaffold/template.go:351](../../../internal/scaffold/template.go#L351)）。

```text
**continuo が用意した worktree と branch のまま作業してください。**
別の branch へ checkout したり、新しい branch を作ったりしないでください。
**切り替えると、次の巡回から continuo がこの issue に着手できなくなります。**

**別の branch の内容が要るときは、切り替えずに取ってきてください。**

    git fetch origin <その branch>
    git merge FETCH_HEAD
```

**貼り先2。**すぐ下の「push 先」の3行を、次に差し替える。

```text
**push 先は、この issue のために作られた branch です。**

    git push -u origin HEAD

**別の名前へ push するときも、必ず -u を付けてください。**
2本目の PR を出すときや、issue に「この branch へ出せ」と書かれているときです。
**書かれていなければ、上の git push -u origin HEAD のままで構いません。**

    git push -u origin HEAD:{{.push_branch}}

**-u を落とすと、この worktree が片付かなくなることがあります。**
```

**`{{.push_branch}}` はリンクされた branch の名前であって、push 先の指定ではない**（11d）。
**「必ずここへ push しろ」と読める文言にしない。**

**`{{.push_branch}}` が空文字のとき、その行は `git push -u origin HEAD:` になる。**
**なるので、テンプレート側で囲む。**

```text
{{if .push_branch}}    git push -u origin HEAD:{{.push_branch}}{{end}}
```

---

## 12b. `## この issue に紐づく PR も読むこと` の節を、宛先で引き直す

**言いたいこと。****PR は `{{.pr_target}}` にあり、issue のリポジトリにあるとは限らない。**
**別のリポジトリの PR は `closingIssuesReferences` で issue に紐づかない**ので、
**push した branch の名前で引く口を1つ足す。**

**足す段落**（節の先頭。[internal/scaffold/template.go:292](../../../internal/scaffold/template.go#L292) の直後）。

```text
**PR を探す相手は {{.pr_target}} です。**この issue のリポジトリとは限りません。
**別のリポジトリの PR は、この issue に紐づきません。**その場合は下の2つで1件も出ないので、
push した branch の名前でも引いてください。

    gh api "repos/{{.pr_target}}/pulls?state=all&head={{.code.owner}}:<push した branch 名>"
```

**既存のコマンドの置き換えは、行ごとに違う。**

| いまの本文の行 | 何に変えるか |
| --- | --- |
| `gh pr list --repo {{.issue.owner}}/{{.issue.repo}}`（[:298](../../../internal/scaffold/template.go#L298)） | **`{{.pr_target}}` に変える。**PR は宛先の側にある |
| `gh api repos/…/issues/…/timeline`（[:300](../../../internal/scaffold/template.go#L300)） | **変えない。**timeline は issue の側にしか無い |
| PR を読む4本（[:304](../../../internal/scaffold/template.go#L304)・[:306](../../../internal/scaffold/template.go#L306)・[:308](../../../internal/scaffold/template.go#L308)・[:310](../../../internal/scaffold/template.go#L310)） | **`{{.pr_target}}` に変える** |

**issue のコメントを読む節（`gh issue view` と `gh api repos/…/issues/…` の側）は
1文字も変えない。**あちらは issue のリポジトリのままである。

---

## 13. 触るもの（3つの塊に分ける。前の2つ）

**言いたいこと。**3つの塊の表は**全部で30行**であり、30ファイルではない。**1本の PR にしない。**
**塊ごとに、それだけで筋の通った状態になるように切る。**順に出す（3つ目は 13b）。

**数えた単位。****行数であってファイル数ではない。**
`git.go` / `prepare.go` / `dispatch.go` / `query.go` / `template.go` / `continuo_design.md` /
`internal/i18n` / [docs/FAQ.md](../../FAQ.md) は
**2つ以上の塊にまたがるので重複して数えている。**重複を除くと**20行**になる。
そのうち `docs/FAQ.md と docs/upgrading.md` の1行は2ファイル、
`internal/config` / `internal/i18n` / `test/internal/workspace` の3行はディレクトリ（複数ファイル）である。
**展開すると、実ファイルは25前後になる。**

**塊「push と片付け」（6行）。**これだけで「通常」と「1つの issue で PR を複数」が閉じる。

| ファイル | 何を |
| --- | --- |
| [internal/scaffold/template.go](../../../internal/scaffold/template.go) | 12 の貼り先1と2（**`{{.push_branch}}` を含む行はこの塊では入れない。**下） |
| [internal/workspace/cleanup.go](../../../internal/workspace/cleanup.go) | 9 の4段 |
| [internal/workspace/git.go](../../../internal/workspace/git.go) | `gitRemoteRefContainsHead`（`for-each-ref --count=1 --contains HEAD`）。**塊2でも触る** |
| [docs/plans/continuo_design.md](../continuo_design.md) | 3-9 の手順2b、5-3 の本文、5-3b の1行、**3-69 を決定済みに書き換える**（12）。**塊2・塊3でも触る** |
| [docs/FAQ.md](../../FAQ.md) と [docs/upgrading.md](../../upgrading.md) | 「push したのに片付かない」の節 |
| [test/internal/workspace](../../../test/internal/workspace) | 4段の分岐 |

**塊1で入れる本文は、変数を使わない形である。**

```text
    git push -u origin HEAD:<新しい名前>
```

**`{{if .push_branch}}` を入れて `prompt.go` の `data` にキーが無い状態にすると、
`missingkey=error`（[internal/orchestrator/prompt.go:30](../../../internal/orchestrator/prompt.go#L30)）で
全 issue の1回目のプロンプトの変数展開が失敗する。**
**変数化は塊3で、`prompt.go` に `.push_branch` を足すのと同じ PR で行う**（12 の末尾の形へ差し替える）。

**塊「リンクを base に使う」（+10行）。**「既存 branch の続き」が閉じる。

| ファイル | 何を |
| --- | --- |
| [internal/tracker/query.go](../../../internal/tracker/query.go) | 11 のクエリと写し、`BranchName` を1本のときだけ埋める（[by_identifier.go](../../../internal/tracker/by_identifier.go) は同じ断片を使うので自動で効く） |
| [internal/tracker/tracker.go](../../../internal/tracker/tracker.go) | `Issue` に4つ |
| [internal/workspace/workspace.go](../../../internal/workspace/workspace.go) | `IssueRef` に5つ |
| [internal/workspace/prepare.go](../../../internal/workspace/prepare.go) | `resolveBase` の5段、段4 の fetch。**塊3でも触る** |
| [internal/workspace/git.go](../../../internal/workspace/git.go) | `gitFetchBranch`。**塊1でも触る** |
| [internal/config](../../../internal/config) | `workspace.fetch_timeout_ms`（`types.go` の `WorkspaceConfig` / `default.go` / validate） |
| [docs/plans/continuo_design.md](../continuo_design.md) | **5-2 の front matter に `fetch_timeout_ms`**（11c）。**塊1・塊3でも触る** |
| [internal/orchestrator/dispatch.go](../../../internal/orchestrator/dispatch.go) | `toIssueRef` の写し。**塊3でも触る** |
| [internal/i18n](../../../internal/i18n) | 11e の文面1と2（keys / ja / en）。**塊3でも触る** |
| [docs/FAQ.md](../../FAQ.md) | **fork を使うボードでは `herdr.worktree.base` を null にする**（11a）。**塊1・塊3でも触る** |

---

## 13b. 塊「コードのリポジトリを別にする」と、受け入れの通し方

**言いたいこと。**3つ目の塊で「既存 OSS への PR」が閉じる。
**`verifiedRepo` は1行も触らない。**信頼を確かめる相手だけがコード側へ移る。

**塊「コードのリポジトリを別にする」（+14行）。**

| ファイル | 何を |
| --- | --- |
| [internal/workspace/layout.go](../../../internal/workspace/layout.go) | `Locate` が使う host / owner / repo をコード側へ |
| [internal/workspace/prepare.go](../../../internal/workspace/prepare.go) | `ghqList`（[:153](../../../internal/workspace/prepare.go#L153)）と、段0 の `CheckWorktreeUsable` が使う `clonePath`（[:450](../../../internal/workspace/prepare.go#L450)）の**両方**をコード側へ。`clonePath` の内部キャッシュの鍵（[internal/workspace/repo.go:127](../../../internal/workspace/repo.go#L127) の `key := owner + "/" + repo`）もコードのリポジトリで引かれる。**塊2でも触る** |
| [internal/workspace/identity.go](../../../internal/workspace/identity.go) | 11c の3キー |
| [internal/workspace/broken.go](../../../internal/workspace/broken.go) | `PathClueOf` の引数に issue の owner/repo を足す、`issueNumberFromSlug` に渡す相手、`IssueURL()` / `Identifier()`、`ScanBroken` が pane の一覧を受け取る（8c）。**数える条件は変えない**（8c2） |
| [internal/workspace/issuebranch.go](../../../internal/workspace/issuebranch.go) | `FindIssueBranch` / `DeleteIssueBranch` が引く clone（[:77](../../../internal/workspace/issuebranch.go#L77) の `clonePath`）をコード側へ |
| [internal/orchestrator/restore.go](../../../internal/orchestrator/restore.go) | `pathAgrees` をスラグの比較へ、`issueAgreesWithPath` / `slugAgrees` の相手をコード側へ、`issueNumberFromPaneLabel` が owner/repo も返す、`recoveryNumbers` と `identifier` の組み立て（8b・8c） |
| [internal/abandon/abandon.go](../../../internal/abandon/abandon.go) | `pathAgrees` から owner/repo の比較を消す。**トラッカーは引かない**（8b2） |
| [internal/tracker/query.go](../../../internal/tracker/query.go) | **`Dispatchable` を判定する相手をコード側へ**（下）。**塊2でも触る** |
| [internal/orchestrator/prompt.go](../../../internal/orchestrator/prompt.go) | 11d の変数 |
| [internal/scaffold/template.go](../../../internal/scaffold/template.go) | 12b の段落と6本の差し替え、12 の貼り先2を `{{.push_branch}}` の形へ。**塊1でも触る** |
| [docs/plans/continuo_design.md](../continuo_design.md) | 5-3 の本文を同じ内容へ。**塊1・塊2でも触る** |
| [internal/orchestrator/dispatch.go](../../../internal/orchestrator/dispatch.go) | 信頼（`CheckTrust`）の相手をコード側へ（下）。**塊2でも触る** |
| [internal/i18n](../../../internal/i18n) | 11f の文面（keys / ja / en）。**塊2でも触る** |
| [docs/FAQ.md](../../FAQ.md) と [docs/upgrading.md](../../upgrading.md) | 身元ファイルを手で書いて復元する手順（11c のサンプルを載せる）と、pane も身元ファイルも無い cross-repo の worktree の片付け方（8c2）。**塊1・塊2でも触る** |

**[internal/workspace/repo.go](../../../internal/workspace/repo.go) は1行も触らない。**
`verifiedRepo` は「パス → ghq → git の共通ディレクトリ」の突き合わせであり、
**パスの意味を揃えれば、そのまま成立する**（8）。

**信頼の関門は4箇所ある。全部をコードのリポジトリへ移す。**
**1つでも残すと、人間に誤った文面が届くか、通知が1つに潰れる。**

| どこ | いま何を見ているか | どう変えるか |
| --- | --- | --- |
| [internal/tracker/query.go:1018](../../../internal/tracker/query.go#L1018) | `repoTrusted(owner, repo)`（**issue のリポジトリ**）が偽なら `Dispatchable` を偽にする | **コードのリポジトリで呼ぶ** |
| [internal/orchestrator/dispatch.go:595](../../../internal/orchestrator/dispatch.go#L595) | `o.ws.CheckTrust(issue.Owner, issue.Repo)` | **コードのリポジトリで呼ぶ** |
| [internal/orchestrator/dispatch.go:643](../../../internal/orchestrator/dispatch.go#L643) | `key := issue.Owner + "/" + issue.Repo`（重複を抑える鍵） | **コードのリポジトリで持つ。**issue のリポジトリで持つと、**同じ issue のリポジトリに属する別々の fork の未信頼が1つに潰れ、2つ目が通知されない** |
| [internal/orchestrator/dispatch.go:670](../../../internal/orchestrator/dispatch.go#L670) | `buildUntrustedComment(issue.Owner, issue.Repo, reason)` | **コードのリポジトリを渡す。**渡さないと「issue のリポジトリが信頼登録されていません」という**間違った直し方**が人間に届く |
| [internal/orchestrator/dispatch.go:605](../../../internal/orchestrator/dispatch.go#L605) | `o.clearUntrusted(issue.Owner, issue.Repo)`（印を消す） | **鍵と同じくコードのリポジトリで呼ぶ。**揃えないと印が二度と消えず、**信頼を付け直しても再通知できない** |

**トラッカー側を直さないと、`dispatch.go:595` へ到達しない。**
`mapRawItemToIssue` はリンクを読んで `CodeRepoNameWithOwner` を決めたあとに
`repoTrusted` を呼ぶ順に組み替える。**`notDispatchableReason` の文面も、
コードのリポジトリの名前を出すように直す**（いまは issue のリポジトリ名を出す）。

**`RepoTrustFunc` の型（[internal/tracker/tracker.go:36](../../../internal/tracker/tracker.go#L36)）は変えない。**
渡す引数を替えるだけである。
[internal/daemon/daemon.go:666-671](../../../internal/daemon/daemon.go#L666-L671) が
`trust.require_repo_trusted` が偽のときに nil を渡す形も変えない。

**理由は [internal/workspace/trust.go:69-85](../../../internal/workspace/trust.go#L69-L85) にある。**
信頼の鍵は clone の実体のパスであり、**Claude Code が開くのは fork の worktree だからである。**
**issue のリポジトリは信頼登録されていなくてよい。**そこでは1行も実行しない。

**受け入れ（塊ごとに実機で1件通す）。**
[.claude/rules/release.md](../../../.claude/rules/release.md) が「実機で issue を1件通してから出す」と
決めている。**3つ目の塊は、開発者の環境の fork（`<ACCOUNT>/oss-project`）と
テスト用のボード（project #10（実データを持たない検証用のボード））で通す。**
**本番のボード（project #3（AI自動進行管理。実データが入っている））では試さない。**

---

## 13c. 塊3 をこの版から外すか — 2つの案を並べる

**言いたいこと。**2回目のレビューの Critical 5件のうち**4件が塊3 に集中した。**
**塊1 と塊2 からは Critical が1件も出ていない。**
**外すかどうかは人間が決める。**この節は材料だけを置く。

**塊3 が背負っているのは、ユースケース「既存 OSS への PR」である。**
**これは人間の追加要件である**（#144（worktree の branch は変えず push 先だけ分ける運用へ揃え、
リンクされた branch を base に使う）の 2026-09-01 のコメント。人間の原文は
「既存OSSへのPRを行う場合。具体的には本家OSSのforkブランチ(public)があり、その中で改修して本家にPRを上げる」）。
**AI が勝手に足した節ではない。**だから案は「捨てる」ではなく「別の issue へ切り出す」である。

| 案 | 何を出すか | 閉じるユースケース |
| --- | --- | --- |
| **A. 塊3 を別の issue へ切り出す** | 塊1（6行）＋塊2（10行）の**16行** | **3つ**（通常・既存 branch の続き・1つの issue で PR を複数）。既存 OSS への PR は**着手せずに1回知らせる** |
| **B. いまのまま3つ出す** | 塊1・塊2・塊3 の**30行** | **4つとも** |

| 何 | 案A | 案B |
| --- | --- | --- |
| 2回目のレビューの Critical | **4件が丸ごと消える** | 5件ともこの版で潰した |
| 触る実ファイル | **14前後** | 25前後 |
| 復元（3-49）・`abandon`・信頼の関門 | **1行も触らない** | 3つとも相手が変わる |
| 置き場所の意味 | **変えない**（issue のリポジトリのまま） | コードのリポジトリへ移る |
| 既存 OSS への PR | **使えない。**回避は fork 側に issue を立ててボードに載せること | 使える |

**案A で cross-repo をどう止めるか。**新しい仕組みは1つも足さない。
`Issue.CodeRepoNameWithOwner` は塊2 で足すので（リンクの先を知るため）、
**その値が issue のリポジトリと違ったら着手せず、11e の文面1 と同じ形で1回だけ知らせる。**
11a の表に1行足すだけである。

| リンクの本数 | 案A での扱い |
| --- | --- |
| **1本・issue と同じリポジトリ** | この設計のとおり base に使う |
| **1本・別のリポジトリ** | **決めない。着手しない**（文面1 と同じ扱い） |

**案A で塊2 から落ちるもの。**`Issue` の `CodeRepoHost` / `CodeRepoDefaultBranch` / `PRTarget`、
`IssueRef` の `CodeOwner` / `CodeRepo` / `CodeHost` / `CodeDefaultBranch`、
11d の `.code.*` と `.pr_target`、12b（PR を宛先で引き直す）。
**残るのは `CodeRepoNameWithOwner` と `LinkedBranch` の2つだけである。**

**この文書は案B のまま書いてある。**案A を選んだら、8・8b・8b2・8c・8c2・11c・11f・12b・13b を
別の文書へ移し、11・11a・11b・11d を上の表のとおり削る。

**推奨は案A。**理由は3つ。

| 順 | 理由 |
| --- | --- |
| **1** | **Critical の出方が偏っている。**塊1 と塊2 は3回のレビューで Critical が0件である |
| **2** | 塊3 が触るのは復元・`abandon`・信頼という**壊れても静かに壊れる**ところであり、実機で1件通しても踏まない経路が多い |
| **3** | **要件は捨てない。**別の issue にすれば単独でレビューを回せる。いま一緒に出すと、**3つのユースケースの分まで塊3 の欠陥で止まる** |

---

## 14. 人間に決めてもらうこと — どこへ push させるか

**言いたいこと。**3つとも「勝手に決めると、人間の手間が増えるか、人間に届かないか」の
どちらかに倒れる判断である。**推奨は書いた。決めるのは人間である**（残り2つは 14b）。

**PR をエージェントに作らせるかは、もう決まっている。**
[docs/plans/continuo_design.md](../continuo_design.md) 5-3i のとおり、
**組み込みのプロンプトが `review` を出す前に PR を作らせる。**
**「既存 OSS への PR」で残るのは、宛先が本家で head が fork になるときの出し方だけである**
（`gh pr create --repo <本家> --head <fork の owner>:<branch>`。3-78b）。
**それは `WORKFLOW.md` の本文に書く。**組み込みは「本文に書いてあれば従え」と言っている。

**「既存 branch の続き」で、どこへ push させるか。**
人間の言い方は「その branch へ push することもある」であり、**常にではない。**

| 案 | 何が起きるか |
| --- | --- |
| **既定を `git push -u origin HEAD` にし、issue に指示があるときだけリンク先へ** | 判断がエージェントに残る。**指示を読み落とすと、続きが別の branch に載る** |
| **リンクがあるときは必ずリンク先へ** | 迷いが消える。**PR を分けたいときに邪魔になる** |

**推奨は上。**リンクは「どこから始めるか」であって「どこへ出すか」ではない。
**base と push 先を同じものに固定すると、6 のユースケースが書けなくなる。**

**この文書の本文は、推奨のほう（既定は `HEAD`）で書いてある。**
節5 の表・7b のコマンド・11d の `.push_branch`・12 の貼り先2 が、そのとおりに揃っている。
**人間が下の案（リンクがあるときは必ずリンク先へ）を選んだら、その4箇所を差し替える。**

---

## 14b. 人間に決めてもらうこと — 着手を止める条件・PR の宛先

**言いたいこと。**どちらも「まだ出ていない形のために入口を増やすか」の判断である。
**推奨はどちらも「増やさない」。**出てきてから足す。

**リンクが2本以上あるとき、着手を止めるか。**
11a では「別々のリポジトリなら止める／同じリポジトリなら base を既定に倒して進む」とした。

| 案 | 何が起きるか |
| --- | --- |
| **この設計のとおり** | 止まるのは、選ぶと事故になる場合だけ |
| **常に止める** | 分かりやすい。**同じリポジトリに2本張っただけで着手できなくなる** |

**推奨はこの設計のとおり。**

**PR の宛先を `parent` 固定にするか、上書きを許すか。**
2 の実測で `ref.repository.parent.nameWithOwner` が取れる。
**fork の fork や、fork へ PR を出したい場合は、これが違う相手を指す。**

| 案 | 何が起きるか |
| --- | --- |
| **`parent` 固定** | 設定が増えない。**上の2つの形が扱えない** |
| **`WORKFLOW.md` に `workspace.pr_target` を足す** | ボード全体に1つしか書けない。**リポジトリごとに変えられない** |
| **issue のラベルで上書き**（`continuo:pr-target=<owner>/<repo>`） | issue ごとに変えられる。**ラベルを新しい入口にする** |

**推奨は `parent` 固定で出すこと。**上の2つの形が実際に出てきてから足す。
**いま足すと、使われない入口が1つ増える。**

---

## 15. 採らなかった案

**言いたいこと。**「コードのリポジトリをどこから知るか」で5つ、
「片付けをどう通すか」で1つ、比べて落とした。

| 案 | 落とした理由 |
| --- | --- |
| **`WORKFLOW.md` に書かせる** | 設定はボードに1つしか無い。**issue ごとに fork が違う形を書けない** |
| **issue の本文に書かせる** | 本文は外部の人が書ける。**3-29 が「issue の中身は continuo が読まず、エージェントに直接読ませる」と決めている**ので、置き場所を決めるために本文を parse すると、そこだけ新しい入口になる |
| **worktree の外に対応表を1つ持つ** | 新しい保存先が1つ増え、その寿命（作る・壊れる・消す）を全部設計することになる。**リンクを読めば済む** |
| **身元ファイルに書いて、それを信じる** | 身元ファイルは worktree の直下にあり、エージェントが書き換えられる。**8b の検算が成立しなくなる** |
| **置き場所を5階層にする**（issue とコードの両方を入れる） | [internal/workspace/scan.go:14](../../../internal/workspace/scan.go#L14) の `const scanDepth = 4` を [repo.go:70](../../../internal/workspace/repo.go#L70) と [broken.go:180](../../../internal/workspace/broken.go#L180) が `len(parts) != scanDepth` で使っている。3-22 の gwq 互換も4階層に依存している。**既にある worktree が全部「規則に合わない」になる** |

**片付けと fetch で落とした2つ。**

| 案 | 落とした理由 |
| --- | --- |
| **片付けの判定を base の差分だけに残す** | 3b の実測2のとおり、`-u` の無い push では upstream が張り替わらない。**commit を積んだ worktree が永久に片付かない** |
| **リンクされた branch は手元に無いから base に使わない** | 3 の実測3のとおり、refspec を明示した fetch 1本で手元に作れる。**「いまの実装に無い」は諦める理由にならない** |
| **素の `git fetch origin <名前>` で済ませる** | 3 の実測2のとおり、`--single-branch` の clone では**リモート追跡 ref ができず worktree が切れない** |
