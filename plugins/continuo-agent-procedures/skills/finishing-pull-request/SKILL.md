---
name: finishing-pull-request
description: continuo が起動した run で、フローチャートの「pull request の題名と本文を書き直す」と「draft を外す」の箱に入るときに呼ぶ。pull request を作る・題名と本文を書く・draft を外す前と、別のリポジトリへ pull request を出すときにも呼ぶ。
---

**pull request の題名と本文を書き直すときも、`Closes` で始まる行は全部そのまま残します。**別のリポジトリへ出したときは、`Closes` にリポジトリ名を付け、`gh` の `--repo` を出した先に置き換えます。
実装レビューループが収まったら、実装レビューを回した pull request の draft を、次へ進む前に必ず外します（外さないのは下の「収まったら draft を外す」の3つのときだけです）。
`<!-- design-review-skipped -->` は自分で貼りません。本文に書いてはいけないもの（手元の絶対パス・資格情報・社内だけで引けるホスト名）は「writing-comments-for-humans」の「公開の場へ書かないもの」にあります。

## pull request を出す

段1・段2（既にあるかを確かめる・draft で作る）は「implementing-and-pushing」の「pull request を出す」にあります。ここは、実装が終わったあとの段3 です。

### 段3. 実装が終わったら、題名と本文を書き直す

いまの本文を読みます（読み方は「finishing-run」の「まとめて直したとき」と同じ `gh pr view <PR番号> --json body --jq .body`）。

| 本文 | すること |
| --- | --- |
| 目印の1行が残っている | 題名と本文を書き直し、目印の1行を外します。前の試行や別の機械が作った pull request でも同じです |
| 目印の1行が無い | 誰かが書き直しています。書き直しません |

```bash
T=$(mktemp)
cat > "$T" <<'TITLE'
<何を直したか>
TITLE
F=$(mktemp)
cat > "$F" <<'PRBODY'
<何をしたかの説明>

<いまの本文に在る Closes の行を、全部ここへ写す>
PRBODY
gh pr edit <PR番号> --repo <issue のリポジトリ> --title "$(cat "$T")" --body-file "$F"
```

- **`Closes` で始まる行は、全部そのまま残してください。**まとめて直した issue の分も入っています（「finishing-run」の「まとめて直したとき」）。とくに `Closes #<issue の番号>` を落とすと、pull request と issue が結びつかず、次に起動されたとき「starting-run」の「紐づく pull request を読む」にこの pull request が出てこなくなり、設計のレビューを CI で数えている project では検査が赤のままになります。
- **下の「別のリポジトリへ pull request を出すとき」で別のリポジトリへ出したときは、`gh pr view` と `gh pr edit` の `--repo` を、出した先のリポジトリに置き換えてください。**置き換えないと、この issue のリポジトリの、同じ番号の無関係な pull request を読み書きします。

### 設計のレビューを飛ばす断りを、自分で書かない

    <!-- design-review-skipped -->

この目印で始まるコメントは、設計のレビューが要らないと人間が判断したときに人間が貼るものです。あなたが貼ると、計画のレビュー（「writing-plan」で書いた計画を、「running-review-loop」の「段3. 設計レビューを回す」で回すもの）を飛ばしたことが誰にも分からなくなります。

## 別のリポジトリへ pull request を出すとき

本文の `Closes` は、リポジトリ名を付けて書きます。`Closes #<issue の番号>` は、出した先のリポジトリの同じ番号の issue を指してしまいます。

    Closes <issue のリポジトリ>#<issue の番号>

出した先で判断票を探す・貼るときの `PRREPO=` は「posting-review-verdict」にあります。

## 収まったら draft を外す

実装レビューループが「running-review-loop」の「何周回すか」で収まって終わったら（10回目で収まったときも）、実装レビューを回した pull request の draft を、次へ進む前に必ず外します。誰が draft で作ったかは問いません。

段1. draft かどうかを確かめます。

```bash
gh pr view <PR番号> --repo <issue のリポジトリ> --json isDraft --jq .isDraft
```

段2. `true` が返ったときだけ外します。`false` なら何もしません（draft でない pull request に `gh pr ready` を打っても `ready_for_review` が起きず、検査は回り直りません）。

```bash
gh pr ready <PR番号> --repo <issue のリポジトリ>
```

外さないのは、次の3つのときだけです。1と2は、収まっていない pull request をマージできる状態にしないためです。

    1. 10周で収まらずに止まったとき（「running-review-loop」の「何周回すか」）
    2. 人間に訊くことが出て、周を打ち切ったとき（「running-review-loop」の「レビューの回し方」）
    3. WORKFLOW.md の本文（「このプロジェクトの決まり」）に「draft のまま人間へ渡す」とはっきり書いてあるとき

「このプロジェクトの決まり」に「draft で作る」とだけ書いてあるときは外します（それは作るときの決まりです）。

- **上の「別のリポジトリへ pull request を出すとき」で別のリポジトリへ出したときは、上の2つのコマンドの `--repo` を、出した先のリポジトリに置き換えてください。**
- `gh pr view` か `gh pr ready` が失敗したら、そのことを「finishing-run」の「runの終了を宣言する」の報告の `### 詳細` に書きます（人間が、draft が残った理由を知れるように）。
- 外したあとに検査を回し直すかどうかと、検査の待ち方は、「posting-review-verdict」の「貼ったら、検査を回し直す」にあります。

## この指示書が決めていないこと

次の1つは、WORKFLOW.md の本文（「このプロジェクトの決まり」）に書いてあれば、そちらに従います。

    draft のまま人間へ渡すかどうか

- これは、実装レビューが収まったあとの決まりです。作るときは「implementing-and-pushing」の「pull request を出す」がいつも draft にします。「draft のまま人間へ渡す」とはっきり書いてあるときだけ、収まったあとも外しません（上の「収まったら draft を外す」）。

<!-- continuo-skill-end: finishing-pull-request CONTINUO_PLUGIN_HASH -->
