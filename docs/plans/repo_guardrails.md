# リポジトリ自身の運用の仕掛け（hook・スクリプト）

**言いたいこと。**このリポジトリは、「PR を出すときは `/code-review` を通す」という
CLAUDE.md の規則を、`.claude/hooks/block-merge-without-review.py`（PreToolUse hook）と
`scripts/check-release-ready.sh`（リリース前の検査）の2つの機械で支えている。
**この節は、その2つの「いまの仕様」だけを置く。修正の履歴は置かない。**

---

## 1-1. block-merge-without-review.py が何を止め、何を止めないか

**言いたいこと。**`gh pr merge` / `gh pr ready` の呼び出しを字句解析し、対象 PR の
コメントに信頼できる投稿者（OWNER / MEMBER / COLLABORATOR）が貼った
`<!-- code-review-result -->` が無ければ止める。**「規則を忘れて素直に叩く」ことだけを
止め、意図した迂回は防げない**（1-2）。

**実装。**[.claude/hooks/block-merge-without-review.py](../../.claude/hooks/block-merge-without-review.py)。
`Bash` ツールの `PreToolUse` として `.claude/settings.json` に登録されている。

**止めるもの。**
- PR 番号・PR の URL で指した `gh pr merge` / `gh pr ready`（レビューの目印が無いか、
  信頼できない投稿者しか貼っていない場合）
- 番号でも URL でも読めない語（branch 名など）で指した呼び出し。**対象の PR が
  分からずレビューの有無を確かめられないため、念のため止め、番号で指し直すよう案内する**

**止めないもの。**
- 引数を1つも書かない形（`gh pr merge` だけ。現在の branch から引く形は番号を
  取れないので見送る）
- 他所のリポジトリを指すもの（`--repo` / `-R` / URL。**URL が最優先**——`gh` 自身が
  そうする挙動に合わせた）。リポジトリ名は `CLAUDE_PROJECT_DIR`（無ければ実行時の
  ディレクトリ）で `git remote get-url origin` を実行して取る
- `gh pr ready <番号> --undo`（draft へ戻す操作）
- heredoc の中身（`<<EOF` … `EOF`。実行される文ではなく、ファイルへ書く文章である）
- `gh` が使えない・応答しない・崩れた引用符で解析できないとき（検査自体が落ちたら
  通す。見逃しより誤爆のほうが実害が大きいと判断している）

---

## 1-2. なぜこの線を引いたか

**言いたいこと。**この hook は「完璧」にはできない。**狙うのは「規則を忘れた・
読み替えた場合の素の呼び出し」だけであり、誤爆（無関係な作業を止めること）を
見逃し（素通り）より重く見る。**

**完璧にできない理由。**`eval "gh pr merge 94"` / `bash -c "…"` / 変数越しの呼び出しの
ように、文字列の中に隠す形は字句解析では見つけられない。この hook は `Bash` ツールに
渡す `command` 文字列を実行の前に読むだけで、実際にシェルへ渡して実行するわけでは
ない（`.claude/hooks/block-merge-without-review.py` 冒頭の docstring 参照）。

**誤爆と見逃しのどちらを選ぶか。**このリポジトリでは、hook が何もしていない作業を
止めると、そのぶん作業が進まなくなる実害のほうが大きい（PR #109 の2回目のレビューで
指摘）。そのため:

- **字句解析に失敗した行（崩れた引用符）は、空白区切りへ落とさず、何も拾わない。**
  誤って見逃すことはあっても、誤って壊れた解析結果で誤爆はしない
- **番号でも URL でもない語のうち、branch 名として不自然な語（シェルのリダイレクトの
  残骸 `2>` など）までは、branch 名として拾わない。**拾うと無関係なコマンドまで
  誤爆する（`_BRANCH_LIKE_RE` を参照）
- **このリポジトリの名前が分からないときは、「このリポジトリの PR かもしれない」
  として止める側へ倒す。**分からないからと見送ると、`--repo` を付けるだけで
  検査ごと素通りできてしまう

---

## 1-3. scripts/check-release-ready.sh と揃えないといけない箇所

**言いたいこと。**同じ「レビュー済みとみなす条件」を、Python と jq の2箇所に別々に
書いている。**片方だけ直すと、マージの検査とリリース前の検査が食い違う。**

| 条件 | Python 側 | jq 側 |
| --- | --- | --- |
| 目印の文字列と、先頭にあること | `MARKER`（`.claude/hooks/block-merge-without-review.py`）と `count_trusted_reviews()` | `review_of()` の `test("^\\s*<!-- code-review-result -->")` |
| 信頼する投稿者 | `TRUSTED_ASSOCIATIONS` | `review_of()` の `["OWNER", "MEMBER", "COLLABORATOR"]` |

**揃っていることは機械で確かめる。**
[.claude/hooks/tests/test_block_merge_without_review.py](../../.claude/hooks/tests/test_block_merge_without_review.py)
の `run_associations_sync_case()`（信頼する投稿者の一覧）と `run_marker_sync_case()`
（目印の文字列と、先頭にあることを求める条件）が、2つのファイルを読み比べて確かめる。
**どちらかを直したら、必ずこのテストを流してから直したと言うこと。**
