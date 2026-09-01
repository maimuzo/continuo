---
name: pr-review-and-merge
description: PR にレビューを通し、結果をコメントへ貼り、CI の関門を緑にしてマージするまでを1本で行う。貼っただけでは検査が回り直さないので、再実行まで含める。
allowed-tools: Bash, Skill
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

## なぜこのスキルが要るか

**レビュー結果を貼っても、CI の関門は回り直さない。**

`review-gate.yml` は `pull_request` の `opened` / `synchronize` / `reopened` / `ready_for_review` で走る。
**コメントを貼っても、そのどれも飛ばない。**
**既に ready の PR に `gh pr ready` を打っても、イベントは飛ばない。**

**だから、貼ったあとに `gh run rerun` を打つ。**
**これを忘れると、レビュー結果を貼ったのに赤いまま、マージできない状態になる。**

## 手順

### 段1. PR の状態を確かめる

```bash
gh pr view <番号> --json number,title,isDraft,mergeable,mergeStateStatus \
  --jq '"#\(.number) draft=\(.isDraft) \(.mergeable)/\(.mergeStateStatus) \(.title)"'
gh pr checks <番号> --json name,bucket --jq '.[]|"\(.name): \(.bucket)"'
```

**`mergeable` が `CONFLICTING` なら、先に競合を解決する。**

### 段2. レビューを回す

```
Skill ツールで code-review を呼ぶ（引数に PR 番号）
```

**`/code-review ultra` は起動できない。**それは人間が打つもの。

### 段3. 結果をコメントへ貼る

**先頭に目印を置く。**

```markdown
<!-- code-review-result -->
## code-review の結果（PR #<番号> / <題名>）
…
```

**貼るときはファイル渡しにする。**

```bash
gh pr comment <番号> --body-file <ファイル>
```

**`--body` に直接書くと、本文の中のコマンドの例を hook が実行しようとしていると読み、投稿が止まる。**

**数える条件は2つ。両方を満たさないと数えない。**

| 条件 | 中身 |
| --- | --- |
| **目印の位置** | **コメントの本文の先頭。**途中に書いたものは数えない |
| **投稿者** | **`OWNER` / `MEMBER` / `COLLABORATOR` のいずれか** |

### 段4. 指摘に対応する

**[CLAUDE.md](../../../CLAUDE.md) の「コードレビュー記録フロー」に従う。**

**レビューのループは最大3回。**3回で収まらないときは、実装を直し続けず、**設計を疑って人間に確認する**
（[.claude/rules/issue.md](../../rules/issue.md)）。

### 段5. 検査を回し直す

**ここが忘れやすい。**

```bash
RUN=$(gh run list --workflow review-gate.yml --branch <branch 名> --limit 1 \
  --json databaseId --jq '.[0].databaseId')
gh run rerun "$RUN"
```

**待つ。**

```bash
until [ "$(gh run view "$RUN" --json status --jq .status)" = "completed" ]; do sleep 10; done
gh run view "$RUN" --json conclusion --jq .conclusion
```

**`success` になったことを確かめる。**

```bash
gh pr checks <番号> --json name,bucket --jq '.[]|select(.name=="review-result")|"\(.name): \(.bucket)"'
```

**`review-result: pass` が出れば通っている。**

### 段6. draft を外してマージする

```bash
gh pr ready <番号>
gh pr merge <番号> --merge
```

**`gh pr merge` が Claude Code の権限の判定に拒否されることがある。**
**そのときは人間に押してもらう。**

## 落とし穴

| 何 | どうなるか |
| --- | --- |
| **段5を飛ばす** | **レビュー結果を貼ったのに赤いまま。**必須の検査なのでマージできない |
| **目印を本文の途中に書く** | 数えない。**先頭に置く** |
| **`--body` で貼る** | **本文の中のコマンドの例で、投稿そのものが止まる。**`--body-file` を使う |
| **レビュー機能で投稿する** | **数えない。**issue のコメントとして貼る（`gh pr comment`） |
| **`gh pr ready` で回り直すと思う** | **既に ready の PR では飛ばない。**`gh run rerun` を使う |

## 検査が3箇所にある

**同じ判定を3つの場所が持っている。**片方だけ直すと食い違う。

| 場所 | 何を止めるか |
| --- | --- |
| [.github/workflows/review-gate.yml](../../../.github/workflows/review-gate.yml) | **PR のマージ**（branch protection の必須の検査） |
| [.claude/hooks/block-merge-without-review.py](../../hooks/block-merge-without-review.py) | **`gh pr merge` / `gh pr ready` の実行** |
| [scripts/check-release-ready.sh](../../../scripts/check-release-ready.sh) | **リリース前の検査** |

**判定の条件を変えるときは、3つとも直す。**
