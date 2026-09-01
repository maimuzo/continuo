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

# 正しい。本文の先頭にあるかを見る
gh api "repos/<owner>/<repo>/issues/<番号>/comments" \
  --jq '[.[] | select(.body | startswith("<!-- code-review-result -->"))] | length'
```

**進捗のコメントの本文中に、手順の説明として同じ文字列が入っていた。**
**それを1件と数えて通した。**

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

### 段1. PR の状態と branch 名を取る

```bash
gh pr view <番号> --json number,title,isDraft,mergeable,mergeStateStatus,headRefName \
  --jq '"#\(.number) draft=\(.isDraft) \(.mergeable)/\(.mergeStateStatus) branch=\(.headRefName) \(.title)"'
gh pr checks <番号> --json name,bucket --jq '.[]|"\(.name): \(.bucket)"'
```

**`mergeable` が `CONFLICTING` なら、先に競合を解決する。**

**branch 名は段5と段6で使う。**シェルの変数に取っておく。

```bash
BR=$(gh pr view <番号> --json headRefName --jq .headRefName)
```

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
RUN=$(gh run list --workflow review-gate.yml --branch "$BR" --limit 1 \
  --json databaseId --jq '.[0].databaseId')
gh run rerun "$RUN"
```

**待つ。`until` で `status` が `completed` になるのを待ってはならない。**
`until` は最初の `sleep` より前に条件を1回評価する。
**`gh run rerun` の直後、`status` はまだ前回の `completed` を返す。**
その瞬間にループが抜け、**前回の `conclusion` を読む。**
前回が成功していたら、**まだ走っているのに `success` と出る。**

```bash
# 前回の completed が消えるまで待つ（最大50秒）
for _ in 1 2 3 4 5 6 7 8 9 10; do
  [ "$(gh run view "$RUN" --json status --jq .status)" = "completed" ] || break
  sleep 5
done
gh run watch "$RUN" --exit-status
```

**`gh run watch --exit-status` は、完了を待ったうえで、失敗していれば非ゼロで終わる。**

```bash
gh pr checks <番号> --json name,bucket \
  --jq '.[]|select(.name=="review-result")|"\(.name): \(.bucket)"'
```

**`review-result: pass` が出れば通っている。**

### 段6. draft を外してマージする

**この段はメインエージェントが自分で叩く。**上の「絶対条件：段6 は worker にやらせない」を読むこと。

**`gh pr ready` は `ready_for_review` を起こし、review-gate の run を新しく1本立てる。**
**その run の完了を待たずにマージへ進んではならない。**
`review-result` は必須の検査なので、**走っている最中はマージが拒否される。**

```bash
BEFORE=$(gh run list --workflow review-gate.yml --branch "$BR" --limit 1 \
  --json databaseId --jq '.[0].databaseId')

gh pr ready <番号>

# ready_for_review で立った新しい run を掴む（最大50秒）
NEW="$BEFORE"
for _ in 1 2 3 4 5 6 7 8 9 10; do
  NEW=$(gh run list --workflow review-gate.yml --branch "$BR" --limit 1 \
    --json databaseId --jq '.[0].databaseId')
  [ "$NEW" = "$BEFORE" ] || break
  sleep 5
done
gh run watch "$NEW" --exit-status

gh pr checks <番号> --required --json name,bucket --jq '.[]|"\(.name): \(.bucket)"'
```

**必須の検査が全部 `pass` になってから、はじめてマージする。**

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
| **`until … completed` で待つ** | **前回の結果を読む。**`gh run watch --exit-status` を使う |
| **段2で PR 番号を渡さない** | **手元の `HEAD` の差分をレビューする。**対象が違う |

## 判定の条件を変えるとき

**同じ判定を3箇所が持っている。**一覧と条件は
[CLAUDE.md](../../../CLAUDE.md) の「PR を出すときの絶対条件」にある。**ここには写さない。**

**片方だけ直すと食い違う。3つとも直す。**
