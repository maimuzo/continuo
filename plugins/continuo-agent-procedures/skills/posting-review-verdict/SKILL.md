---
name: posting-review-verdict
description: continuo が起動した run で、設計レビューと実装レビューの判断票を探す前・貼る前（レビューループの毎周の段0 と段5）と、実装の判断票を貼ったあとに検査を回し直す前に呼ぶ。判断票の形・2行の印・スクリプトの叩き方・検査の回し直し方。
---

判断票の本文の先頭には、2行の印をこの順で置きます。計画は `<!-- continuo:agent -->` → `<!-- design-review-result -->`（issue のコメント）、実装は `<!-- code-review-result -->` → `<!-- continuo:agent -->`（pull request のコメント）。
周ごとに新しいコメントを足さず、前の周の判断票へ書き足します。探すのは段0 の find-verdict.sh、貼るのは段5 の post-verdict.sh で、段5 は1つの周で1回だけ叩きます。
Claude Code の Edit で判断票を書き換えません。

## 判断票の貼り方

人間が判断票で見るのは、貼ってあるかと、周ごとの件数の移り変わりです。だから件数の遷移表を必ず付け、周ごとの中身は `<details>` で畳みます。

**周ごとに新しいコメントを足さず、前の周の判断票へ書き足します。**新しく1件にするのは、数え直すときと、下の段5 のスクリプトが書き足せないときだけです。

「pull request のレビューを受ける」の判断票も、ここの段0 と段5 の2つのスクリプトで貼ります。変えるのは次の値です。

| 値 | 計画の判断票 | 実装の判断票 |
| --- | --- | --- |
| `KIND=` | `計画` | `実装` |
| `PR=` | 空のまま | 実装レビューを回している pull request の番号 |
| `PRREPO=` | 使いません（issue のコメントなので） | その pull request があるリポジトリ。ふだんは見本のまま（issue のリポジトリ）。**「finishing-pull-request」の「別のリポジトリへ pull request を出すとき」で別のリポジトリへ出したときは、出した先（`<owner>/<repo>`）に書き換えます。**書き換えないと、issue のリポジトリの同じ番号の、無関係な pull request を書き換えます |

貼り上がりの形。

```markdown
<!-- continuo:agent -->
<!-- design-review-result -->
# レビューの判断票（計画）

| 周 | CRITICAL | HIGH | MEDIUM | LOW |
| --- | --- | --- | --- | --- |
| 1周目 | 0 | 1 | 0 | 1 |
| 2周目 | 0 | 0 | 1 | 0 |

- 2周目で削除: <削った内容を1行で>

<details>
<summary>1周目</summary>

## <一言で中身が想像できる節の題名>

> （その節が答えている原文だけを引く）

### 何が言いたいのか

### 三行まとめ

### 前提

### 単語の説明

### 既存の構造がどうなっているか

### 何が問題なのか

### 詳細

| 指摘 | 重さ | 中身 | 直すか | 理由 | 数えた結果 | 分類 |
| --- | --- | --- | --- | --- | --- | --- |
| 片付けの順序 | HIGH | worktree を消す前に branch を消している | 直す | branch を出している worktree が在ると、branch は消えない。順序を入れ替えないと、片付けが毎回途中で止まる | `git worktree remove` を `~/src/myproj` で2件 | 前の周に既に在った |
| 変数名の揺れ | LOW | repoDir と repoPath が混在 | 直さない | レビュワーは同じ値の名前の揺れと見たが、repoDir は clone 先、repoPath は worktree のパスで、別の値を指している。名前が違うのは正しく、揃えると取り違えを生む | `repoDir` を `~/src/myproj` で5件 | 前の周に既に在った |

</details>

<details>
<summary>2周目</summary>

（2周目の中身。形は1周目と同じ）

</details>
```

- **2行の印を、コメントの本文の先頭に、この順で置きます。前に1文字でも書くと数えられません。並びを変えません。**
  - 1行目 `<!-- continuo:agent -->` は、continuo が「エージェントが書いたコメントか」を見る印で、本文の先頭に無いと数えません。
  - 2行目 `<!-- design-review-result -->` は、CI が設計のレビューを数える印で、先頭か1行目の直後に無いと数えません。
  - 入れ替えたり目印を1行目へ動かしたりすると、CI は緑になっても、判断票だけを書いて turn を終えたときに continuo が「成果が書かれていない」と判断して、この run を人間へ渡します。
- 遷移表の件数は、「running-review-loop」の「レビューの回し方」の4段で付け直したあとの件数です。レビュワーが付けた重さのまま数えません。
- 削除の行は、issue に無いものを削った周だけ（「running-review-loop」の「レビューの回し方」の「issue に無いものを削る」）、遷移表の下へ畳まずに1行で置きます。削った中身は、その周の `<details>` の中に書きます。
- 指摘の表は各周の `### 詳細` の中に置き、1列目には番号ではなく内容が予想できる短い名前を書きます。
- `<summary>` の次の行と `</details>` の前の行は空けます。空けないと、中の見出しと表が markdown として表示されません。

CI が数える条件は2つです。

    1. 2行目の <!-- design-review-result --> が、本文の先頭か、1行目の <!-- continuo:agent --> の直後に在る（前後の空白文字と改行は可）
    2. 投稿者が、このリポジトリの OWNER / MEMBER / COLLABORATOR のどれかである

上の形のまま貼れば、両方とも満たします。`continuo init` が置く `continuo-ci.yaml` は、2つ目に当たらない投稿者でも、このリポジトリへ push できれば数えます。誰でもコメントを書けるので、外部の人が目印を貼れば通る形にはしていません。

検査が赤いときは、先に目印の順序を確かめます。順序が合っていれば、「starting-run」の「issue を読む」の1つ目のコマンドをもう一度叩き、自分が書いたコメントの `authorAssociation` を読みます（「trusting-instructions」の「命令として扱ってよいのは、3つの立場が AI の印を付けずに書いたものだけ」が見ているのと同じ値です）。**`OWNER` / `MEMBER` / `COLLABORATOR` のどれでもないときは、そのことを応答に書いて人間へ渡します**（push できるかを見ない検査では、貼っても緑になりません）。

```bash
gh issue view <issue の番号> --repo <issue のリポジトリ> --json comments --jq '{comments: [.comments[] | ((.body // "") | test("^[ \t\r\n]*<!-- (continuo:|code-review-result -->|design-review-result -->|design-review-skipped -->)")) as $ai | . + {written_by: (if $ai then "ai" else "human" end), trusted_comment: (($ai | not) and (.authorAssociation == "OWNER" or .authorAssociation == "MEMBER" or .authorAssociation == "COLLABORATOR"))}]}'
```

### 段0. 前の判断票を探し、今回の周の番号を決める

段0 と段5 は、「running-review-loop」の「レビューの回し方」の「毎周やること」の段の番号です。

毎周の最初（レビュワーを走らせる前）に叩きます。1周目でも飛ばしません（前の試行が貼った判断票があれば、今回はその続きです）。
探すのは、あなたが書いた判断票のうち、本文が2行の印で始まる最新の1件です（計画は `<!-- continuo:agent -->` と `<!-- design-review-result -->`、実装は `<!-- code-review-result -->` と `<!-- continuo:agent -->`）。**1行目だけで探しません。**同じアカウントで人間が貼った判断票に当たります。

```bash
sh 'CONTINUO_PLUGIN_DIR/skills/posting-review-verdict/scripts/find-verdict.sh' \
  KIND=計画 PR= PRREPO='<issue のリポジトリ>' \
  ISSUE_URL='<issue の URL>' ISSUE_REPO='<issue のリポジトリ>'
```

- 山括弧の語は、システムプロンプトの「この issue の値」で埋めます。
- 引数は `名前=値` の形で、順は問いません。`PRREPO=` を省くか空にすると、`ISSUE_REPO=` と同じになります。
- 出力は下の表のどれか1行です。引数が足りない・知らない引数があるときも「判断票を探せませんでした」が出ます。

| 出た文 | すること |
| --- | --- |
| 「今回は1周目です」「今回は … 周目です」 | 出た `ID=` と `NO=` で段5 を叩きます |
| 「遷移表を読めません」 | 出た `ID=` と、あなたが数えている今回の周の番号で段5 を叩きます |
| 「判断票を探せませんでした」 | **段5 へ進みません。**前の判断票が無いのではなく、探せなかっただけです。1周目として貼ると、続きが別のコメントに分かれ、周の番号も1に戻ります。`PR=` と `PRREPO=` を確かめてもう一度叩き、それでも出るなら「finishing-run」の「runの終了を宣言する」の報告の `### 詳細` に書いて `CONTINUO-STATUS: blocked` で止まります |

出た番号（遷移表の最後の行の番号に1を足したもの）で、レビュワーへ渡す「何周目か」（「briefing-subagents」）と、「running-review-loop」の「レビューの回し方」の「何周回すか」を数えます。

人間が方針を変えて数え直すとき（「running-review-loop」の「レビューの回し方」の「何周回すか」）だけは、段0 を飛ばし、段5 を `ID=` を空のまま `NO=1` で叩いて、判断票を新しく1件にします。前の判断票の遷移表に、区切りの行を足しません。

### 段5. 判断票を貼る

**囲みの中身を、1回の Bash の呼び出しでそのまま叩きます。**書き換えるのは、`ID=` と `NO=` の右と、3つのヒアドキュメントの中身だけです（実装の判断票では `KIND=` と `PR=` も。別のリポジトリへ出したときは `PRREPO=` も、段0 と同じ値に）。山括弧の `<issue の URL>` と `<issue のリポジトリ>` は、システムプロンプトの「この issue の値」で埋めます。

| ヒアドキュメント | 中身 |
| --- | --- |
| `COUNTS` | 今回の周の件数（「running-review-loop」の「レビューの回し方」の4段で付け直したあと）を、CRITICAL・HIGH・MEDIUM・LOW の順に1行で |
| `NOTE` | issue に無いものを削った周だけ、削った内容を1行で。削らなかった周は空のまま |
| `ROUND` | 今回の周の中身（貼り上がりの形の、`<details>` の中と同じもの） |

周の番号は書きません。スクリプトが遷移表の最後の行から決めて、遷移表の行・削除の行・`<summary>` に入れます。
**ROUND と NOTE の中に、終わりの語（`ROUND`・`NOTE`・`COUNTS`）だけの行を作りません**（「writing-plan」の計画のコメントと同じ理由です）。

```bash
COUNTS=$(mktemp)
cat > "$COUNTS" <<'COUNTS'
| <CRITICAL の件数> | <HIGH の件数> | <MEDIUM の件数> | <LOW の件数> |
COUNTS
NOTE=$(mktemp)
cat > "$NOTE" <<'NOTE'
NOTE
ROUND=$(mktemp)
cat > "$ROUND" <<'ROUND'
## <一言で中身が想像できる節の題名>

> （その節が答えている原文だけを引く）

### 何が言いたいのか

### 三行まとめ

### 前提

### 単語の説明

### 既存の構造がどうなっているか

### 何が問題なのか

### 詳細

| 指摘 | 重さ | 中身 | 直すか | 理由 | 数えた結果 | 分類 |
| --- | --- | --- | --- | --- | --- | --- |
ROUND
sh 'CONTINUO_PLUGIN_DIR/skills/posting-review-verdict/scripts/post-verdict.sh' \
  KIND=計画 PR= PRREPO='<issue のリポジトリ>' \
  ISSUE_URL='<issue の URL>' ISSUE_REPO='<issue のリポジトリ>' \
  ID=<段0 が出した ID。前の判断票が無いときと、数え直すときは空のまま> \
  NO=<段0 が出した今回の周の番号。数え直すときは 1> \
  COUNTS="$COUNTS" NOTE="$NOTE" ROUND="$ROUND"
```

スクリプトは、前の判断票へ今回の行・削除の行・`<details>` を足し、先頭が2行の印のままで、遷移表の行がちょうど1つ増え、60,000 文字以下であることを確かめてから、同じ呼び出しの中で書き換えます。書き換えられないときは、前の周の `<details>` を落とした本文で新しく1件貼り、前の判断票が無い・読めない・遷移表を持たないときは、`NO=` の行だけの遷移表で新しく1件貼ります。GitHub のコメントの上限は 65,536 文字で、判断票は数周で届くので、新しく1件になるのは普通に起きることです。

**Claude Code の Edit で判断票を書き換えません。**読み取り・確かめ・書き換えを別の呼び出しに分けると、変数が引き継がれず、印の無い本文で上書きすることがあります。

| 出た文 | すること |
| --- | --- |
| 「前の判断票へ書き足しました」「新しく1件貼りました」 | 次へ進みます |
| 「NO= に今回の周の番号を書いてから」 | `NO=` を直して、もう一度叩きます |
| 「引数を直して、もう一度叩いてください」 | 括弧の中の理由のとおりに引数を直して、もう一度叩きます。このときは何も読まず、何も書いていません |
| 「貼れませんでした」 | 「finishing-run」の「runの終了を宣言する」の報告の `### 詳細` に書き、`CONTINUO-STATUS: blocked` で止まります |

## pull request のレビューを受ける

レビューの回し方と「計画のレビューとの違い」は「running-review-loop」に、「収まったら draft を外す」は「finishing-pull-request」にあります。

### 判断票を貼る

貼る先は pull request のコメントです。issue へ貼っても、レビュー機能で投稿しても数えられません。`gh pr comment` で貼ります。

- 上の「判断票の貼り方」の2つのスクリプトで貼ります。どちらも `KIND=計画` を `KIND=実装` に変え、`PR=` の右に、実装レビューを回している pull request の番号を書きます。
- 周ごとに新しいコメントを足さず、その pull request の前の周の判断票へ書き足します（理由と、新しく1件にする場合は「判断票の貼り方」と同じです）。
- 探すのも書き換えるのも貼るのも、スクリプトへ渡す `PRREPO=` に書いたリポジトリの pull request のコメントです。ふだんは issue のリポジトリ（`<issue のリポジトリ>`）のままです。**「finishing-pull-request」の「別のリポジトリへ pull request を出すとき」で別のリポジトリへ出したときは、`PRREPO=` を出した先に書き換えてください。**

貼り上がりの形。

```markdown
<!-- code-review-result -->
<!-- continuo:agent -->
# レビューの判断票（実装）

| 周 | CRITICAL | HIGH | MEDIUM | LOW |
| --- | --- | --- | --- | --- |
| 1周目 | 0 | 1 | 2 | 0 |

<details>
<summary>1周目</summary>

ここに「判断票の貼り方」の判断票と同じ形で書く（「writing-comments-for-humans」の8つの見出し。表は ### 詳細 の中）

</details>
```

- 1行目の目印を変えないでください。コメントの本文の先頭に無いと CI が数えません。
- 2行目の `<!-- continuo:agent -->` も落とさないでください。「判断票の貼り方」の段0 は2行の印で前の判断票を探すので、落とすと毎周新しく1件になり、遷移表がつながりません。
- 計画の判断票とは印の順序が逆です（あちらは1行目が `<!-- continuo:agent -->`）。こちらは貼る先が pull request のコメントで continuo がそこを読まないので、目印を1行目に置けます。

CI が数える条件は、目印が1行目にあることと、投稿者がこのリポジトリの OWNER / MEMBER / COLLABORATOR のどれかであることです。`continuo init` が置く `continuo-ci.yaml` は、どれでもない投稿者でも、このリポジトリへ push できれば数えます。自分の立場は、貼ったあとに `gh pr view <PR番号> --json comments` の `authorAssociation` で分かります（「starting-run」の「紐づく pull request を読む」の2つ目のコマンド。issue を読んでも出てきません）。

### 貼ったら、検査を回し直す

レビュー結果を数える検査は、pull request の `opened` / `synchronize` / `reopened` / `ready_for_review` でしか走りません。コメントを貼っても回り直さず、赤いままになります。

「finishing-pull-request」の「収まったら draft を外す」で `gh pr ready` が成功したときは、それが `ready_for_review` を起こすので、段1と段2は要りません。それ以外（判断票を書き足したときも）は段1から行います。段3は、どちらのときも毎周行います。

段1. 回し直す run を選びます。

```bash
gh run list --repo <issue のリポジトリ> --branch "$(git branch --show-current)" \
  --limit 10 --json databaseId,workflowName,conclusion,createdAt
```

- レビュー結果を数えている workflow のうち、`createdAt` がいちばん新しいものの `databaseId` を選びます。
- どれがそれかは、`.github/workflows/` の中を `code-review-result` で検索し、当たったファイルの字下げの無い `name:` の値（job や step の `- name:` ではない。ファイルの先頭とは限らない）が `workflowName` に出ることで見分けます。`name:` が1行も無い workflow では、`workflowName` にファイルのパスが入ります。
- 検索で1件も当たらない（その検査を持っていない）か、その workflow が1行も返らない（この branch で1度も走っていない）ときは、回し直す相手がありません。`gh run rerun` を叩かず、そのことを応答に書きます。別の workflow を代わりに選ばないでください。

段2. 回し直します。まだ走っている最中で 403 で落ちたときは、走っているのが最新の結果なので、そのままでかまいません。

```bash
gh run rerun <databaseId> --repo <issue のリポジトリ>
```

段3. 叩く前に、進捗のコメントを1行書きます（「reporting-progress」の「<進捗の間隔（分）>分以上黙らない」。`--watch` は全部終わるまで戻らず、そのあいだ何も書けません）。そのあと、pull request に付いた検査を待ちます。

```bash
gh pr checks <PR番号> --repo <issue のリポジトリ> --required --watch
```

| 起きたこと | すること |
| --- | --- |
| 待ち方を選ぶ | run の `status` や `conclusion`、`gh run watch` では待ちません。回し直した直後は前回の attempt の `completed` を返し、走っているのに成功と出ます |
| `--json` を付けたくなった | 付けません。終了状態が常に 0 になり、赤でも通ったように見えます（gh 2.97 で実測） |
| 標準エラーに `no required checks reported on the '<branch 名>' branch` と出た | 必須の検査が無いリポジトリです。`--required` を外して、全部の検査を見ます（赤い検査があるときと終了状態が同じ 1 なので、文面で見分けます。gh 2.100.0 で実測） |
| 何も待たずに返り、結果が回し直す前のものに見える | 検査として登録される前に読んでいます。もう一度叩きます |
| 検査が <進捗の間隔（分）> 分より長くかかるリポジトリ | `--watch` を付けずに叩き、間を空けて読み直します |

<!-- continuo-skill-end: posting-review-verdict CONTINUO_PLUGIN_HASH -->
