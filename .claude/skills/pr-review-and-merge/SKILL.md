---
name: pr-review-and-merge
description: PR にレビューを通し、結果をコメントへ貼り、CI の関門を緑にしてマージするまでを1本で行う。貼っただけでは検査が回り直さないので、再実行まで含める。
allowed-tools: Bash, Skill, Write
user-invocable: true
---

<!-- 目的: レビュー結果を貼っても CI が回り直さない問題を含め、PR をマージまで通す手順を1本にまとめる -->
<!-- 所属: continuo リポジトリ（.claude/skills）。動けばプラグインへ移す -->

# pr-review-and-merge

**PR をレビューからマージまで通す。**

## 呼び出し方

```
/pr-review-and-merge <PR 番号>
```

## 絶対条件：段6 は worker にやらせない

**worker（subagent / Workflow の agent）に `gh pr merge` を実行させてはならない。**
**マージできる状態かどうかの確認も、メインエージェントが自分で行う。**

**なぜか。**2026-09-01、worker に6本のマージを任せ、**2本をレビュー未実施のままマージした。**
原因はメインエージェントが渡した確認コマンドで、`contains` を使っていた。

```bash
# 誤り。本文のどこかに含まれていれば1と数える
gh pr view <番号> --json comments \
  --jq '[.comments[] | select(.body | contains("code-review-result"))] | length'
```

**進捗のコメントの本文中に、手順の説明として同じ文字列が入っていた。**
**それを1件と数えて通した。**

**数え方を自分で書き直してはならない。**数える条件は3箇所の実装が持っていて
（一覧は [CLAUDE.md](../../../CLAUDE.md) の「PR を出すときの絶対条件」）、
**手で書いた jq は、投稿者の絞り込みか、ページ送りか、先頭の空白の扱いのどれかで必ずずれる。**

**見るのは GitHub 自身が出した判定だけにする。**

```bash
gh pr view <番号> --json mergeable,mergeStateStatus \
  --jq '"\(.mergeable)/\(.mergeStateStatus)"'
```

**`MERGEABLE/CLEAN` 以外はマージしない。**`review-result` は必須の検査なので、
**レビュー結果が貼られていなければ `BLOCKED` になる。**

**worker に渡してよいのは段1から段5まで。**段6はメインエージェントが自分で叩く。

## なぜこのスキルが要るか

**レビュー結果を貼っても、CI の関門は回り直さない。**

[.github/workflows/review-gate.yml](../../../.github/workflows/review-gate.yml) は
`pull_request` の `opened` / `synchronize` / `reopened` / `ready_for_review` で走る。
**コメントを貼っても、そのどれも飛ばない。**
**`ready_for_review` は draft を ready にしたときにしか起きない。**
だから**既に ready の PR に `gh pr ready` を打っても、検査は回り直さない。**

**貼ったあとに `gh run rerun` を打つ。**
**これを忘れると、レビュー結果を貼ったのに赤いまま、マージできない状態になる。**

**赤いままだとマージは本当に止まる。**`review-result` は `main` の branch protection の
必須の検査である（2026-09-01 に登録した）。確かめ方。

```bash
gh api repos/<owner>/<repo>/branches/main/protection/required_status_checks \
  --jq '.checks[].context'
```

## 手順

**シェルの変数は、この段からあの段へは持ち越せない。**
**Bash の呼び出しが1回ごとに別のシェルだからである。**
**だから下のコードの塊は、それぞれ1回の呼び出しで丸ごと叩く。**`BR` や `RUN` は塊の中で取り直す。

### 段1. PR の状態と branch 名を見る

```bash
gh pr view <番号> --json number,title,isDraft,mergeable,mergeStateStatus,headRefName \
  --jq '"#\(.number) draft=\(.isDraft) \(.mergeable)/\(.mergeStateStatus) branch=\(.headRefName) \(.title)"'
gh pr checks <番号> --json name,bucket --jq '.[]|"\(.name): \(.bucket)"'
```

**`mergeable` が `CONFLICTING` なら、先に競合を解決する。**

**branch 名は段5で使う。**`headRefName` を出しているのはそのためである。

### 段2. レビューを回す

```
/code-review <PR 番号>
```

**`code-review` は Claude Code に同梱されている skill である。このリポジトリの中には無い。**
（確かめ方: `git ls-tree -r --name-only HEAD -- .claude/commands .claude/skills/code-review` が1件も返さない）

**PR 番号を必ず渡す。**渡さないと、**その worktree の `HEAD` からの差分**がレビューされる。
別の branch にいたら、まったく違うものをレビューして「指摘なし」と書くことになる。
**レビューを回した証拠だけが残って、中身は対象を外している。**
**出力の冒頭で、対象が PR になっていることを確かめる。**

**`ultra` は effort level の1つで、クラウドで多数のエージェントを走らせる深いレビューである。**
**枠を大きく使うので、人間が明示的に指示したときだけ使う。**

### 段3. 結果をコメントへ貼る

**先頭に目印を置く。**

```markdown
<!-- code-review-result -->
## code-review の結果（PR #<番号> / <題名>）
…
```

**本文は Write ツールでファイルへ書き出し、`--body-file` で渡す。**

```bash
gh pr comment <番号> --body-file <ファイル>
```

**`--body` に直接書くと、投稿そのものが拒否されることがある。**
[.claude/hooks/block-merge-without-review.py](../../hooks/block-merge-without-review.py) の `MERGE_RE` は
**Bash のコマンド文字列のどこにあっても `gh pr merge <数字>` / `gh pr ready <数字>` に当たる。**
レビュー本文にその形の例を1つ書くと、**`gh pr comment … --body "…"` というコマンド全体が当たって止まる。**

**heredoc でファイルを作る場合も同じである。**heredoc の中身も Bash のコマンド文字列の一部だからである。
**だから Write ツールで書き出す。**`allowed-tools` に `Write` が入っているのはそのためである。

**数える条件は写さない。**[CLAUDE.md](../../../CLAUDE.md) の「PR を出すときの絶対条件」にある。
**写すと食い違う。**

### 段4. 指摘に対応する

**[CLAUDE.md](../../../CLAUDE.md) の「コードレビュー記録フロー」に従う。**

**レビューのループは最大3回。**3回で収まらないときは、指摘を1件ずつ潰すのをやめる。
**「この設計から捨てられる部分があるか」を検討し、捨てる案といまのままの案を並べて人間に報告し、判断を待つ。**
**勝手に捨てない。**人間が必要だと思っていたものが黙って消える。

**なぜ上限を置くか。**指摘は「守りが1箇所抜けている」という形で来る。
素直に答えると、穴を埋めるものが1つ増える。**3回回せば3層になる。**
**3回で収まらないこと自体が「設計があやふや」の証拠である。**

### 段5. 検査を回し直す

**ここが忘れやすい。**

```bash
set -eu
BR=$(gh pr view <番号> --json headRefName --jq .headRefName)
RUN=$(gh run list --workflow review-gate.yml --branch "$BR" --limit 1 \
  --json databaseId --jq '.[0].databaseId')
if [ -z "$RUN" ] || [ "$RUN" = "null" ]; then
  echo "この branch で review-gate がまだ1度も走っていない。commit を push して走らせること" >&2
  exit 1
fi
gh run rerun "$RUN"
```

**その run がまだ走っている最中だと、`gh run rerun` は 403 で落ちる。**
**その場合は再実行しなくてよい。**走っているのが最新の結果だからである。

**待つのは run ではなく、PR に付いた検査のほうである。**

```bash
gh pr checks <番号> --required --watch
```

**run の `status` や `conclusion` を見て待ってはならない。**
`gh run rerun` の直後、その run はまだ**前回の attempt** の `completed` を返す。
`until` は最初の `sleep` より前に条件を1回評価するので、その瞬間に抜けて前回の結果を読む。
`gh run watch` も同じものを読んで、即座に成功として返ることがある。
**前回が成功していたら、まだ走っているのに `success` と出る。**

**`gh pr checks` に `--json` を付けてはならない。**
**付けると終了状態が常に 0 になり、赤でも通ったように見える**（gh 2.97 で実測）。

```
$ gh pr checks <番号> --required --json name,bucket --jq '.[]|"\(.name): \(.bucket)"'
review-result: fail
EXIT=0

$ gh pr checks <番号> --required
review-result	fail	5s	https://github.com/<owner>/<repo>/actions/runs/…
EXIT=1
```

**再実行が検査として登録される前に読むと、`--watch` が何も待たずに返ることがある。**
**そのときは段6の判定をもう一度叩く。**

### 段6. draft を外してマージする

**この段はメインエージェントが自分で叩く。**上の「絶対条件：段6 は worker にやらせない」を読むこと。

**`gh pr ready` は、いま draft の PR にだけ効く。**
draft を ready にすると `ready_for_review` が飛び、review-gate の run が新しく1本立つ。
**既に draft でない PR に打っても、何も起きない。**だから draft かどうかで分ける。

**新しく立った run の完了を待たずにマージへ進んではならない。**
`review-result` は必須の検査なので、**走っている最中はマージが拒否される。**

**`--fail-fast` を付けず、`set -e` も置かない。**
**赤いときこそ、下の判定の1行を出させたいからである。**

```bash
if [ "$(gh pr view <番号> --json isDraft --jq .isDraft)" = "true" ]; then
  gh pr ready <番号>
fi
gh pr checks <番号> --required --watch
gh pr view <番号> --json mergeable,mergeStateStatus \
  --jq '"\(.mergeable)/\(.mergeStateStatus)"'
```

**`MERGEABLE/CLEAN` が出てから、はじめてマージする。**

**この1行だけを合否の判定に使う。**GitHub 自身が branch protection と突き合わせて出した答えだからである。
**検査の一覧を自分で数えて判断してはならない。**必須の検査が1本も報告していない場合、
`gh pr checks` の一覧にはそもそも出てこないので、**足りないことに気づけない。**

| 何が返るか | 何をするか |
| --- | --- |
| **`MERGEABLE/CLEAN`** | **マージしてよい。**必須の検査が全部そろって通っている |
| `MERGEABLE/BLOCKED` | **マージしない。**必須の検査が足りないか落ちている。段5へ戻る |
| `MERGEABLE/UNSTABLE` | **マージしない。**必須でない検査が落ちている。何が落ちたかを確かめ、人間に報告する |
| `CONFLICTING/DIRTY` | **マージしない。**競合を先に解決する |

**`gh pr ready` を打った直後は、その run が検査として登録される前に読むことがある。**
**そのときは1つ前の結果が出る。**上の塊をもう一度叩いて、同じ答えが返ることを確かめる。

**取り違えても、レビュー未実施のものが入ることはない。**
`review-result` は必須の検査なので、**pending か fail のあいだは GitHub がマージそのものを拒む**
（そのとき `mergeStateStatus` は `BLOCKED` になる）。

```bash
gh pr merge <番号> --merge
```

**`gh pr merge` が Claude Code の権限の判定に拒否されることがある。**
**そのときは人間に押してもらう。**

## 落とし穴

| 何 | どうなるか |
| --- | --- |
| **段5を飛ばす** | **レビュー結果を貼ったのに赤いまま。**`review-result` は必須の検査なのでマージできない |
| **目印を本文の途中に書く** | 数えない。**先頭に置く** |
| **`--body` で貼る** | **本文の中の `gh pr merge <数字>` の例で、投稿そのものが止まる。**Write ツールで書き出して `--body-file` を使う |
| **レビュー機能で投稿する** | **数えない。**issue のコメントとして貼る（`gh pr comment`） |
| **`gh pr ready` で回り直すと思う** | **既に ready の PR では飛ばない。**`gh run rerun` を使う |
| **`gh pr ready` の直後にマージする** | **新しい run が走っている最中で拒否される。**完了を待つ（段6） |
| **run の `status` / `conclusion` で待つ** | **前回の attempt の結果を読む。**`gh pr checks --required --watch` を使う |
| **`gh pr checks` に `--json` を付けて合否を見る** | **終了状態が常に 0。**赤でも通ったように見える。付けずに使う |
| **検査の一覧を自分で数えて合否を決める** | **報告していない必須の検査は一覧に出ない。**`mergeStateStatus` を見る |
| **段2で PR 番号を渡さない** | **手元の `HEAD` の差分をレビューする。**対象が違う |

## 判定の条件を変えるとき

**同じ判定を3箇所が持っている。**一覧と条件は
[CLAUDE.md](../../../CLAUDE.md) の「PR を出すときの絶対条件」にある。**ここには写さない。**

**片方だけ直すと食い違う。3つとも直す。**
