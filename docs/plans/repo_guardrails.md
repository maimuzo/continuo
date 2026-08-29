# リポジトリ自身の運用の仕掛け（hook・スクリプト）

**言いたいこと。**このリポジトリは、「PR を出すときは `/code-review` を通す」という
CLAUDE.md の規則を、[.claude/hooks/block-merge-without-review.py:1](../../.claude/hooks/block-merge-without-review.py#L1)（PreToolUse hook）と
[scripts/check-release-ready.sh:1](../../scripts/check-release-ready.sh#L1)（リリース前の検査）の2つの機械で支えている。
**この節は、その2つの「いまの仕様」だけを置く。修正の履歴は置かない。**

---

## 1-1. block-merge-without-review.py が何を止め、何を止めないか

**言いたいこと。**`gh pr merge` / `gh pr ready` の呼び出しを字句解析し、対象 PR の
コメントに信頼できる投稿者（OWNER / MEMBER / COLLABORATOR）が貼った
`<!-- code-review-result -->` が無ければ止める。**「規則を忘れて素直に叩く」ことだけを
止め、意図した迂回は防げない**（1-2）。

**実装。**[.claude/hooks/block-merge-without-review.py](../../.claude/hooks/block-merge-without-review.py)。
`Bash` ツールの `PreToolUse` として [.claude/settings.json:87-96](../../.claude/settings.json#L87-L96) に登録されている。

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
- `#` から行末まで（コメント）。**`#` が語の先頭に来たときだけである**
- `gh` が使えない・応答しない・崩れた引用符で解析できないとき（検査自体が落ちたら
  通す。見逃しより誤爆のほうが実害が大きいと判断している）

**レビューの有無は、リポジトリ名を判定したのと同じリポジトリへ問い合わせる。**
`has_review()` は `gh pr view <番号> --json comments --repo <current_repo()>` を
`CLAUDE_PROJECT_DIR` のディレクトリで実行する。**指定しないと `gh` はいまいる
ディレクトリから判断するため、他所のリポジトリの worktree の中で作業していると、
判定と問い合わせが別のリポジトリの同じ番号の PR を指す。**

---

## 1-2. なぜこの線を引いたか

**言いたいこと。**この hook は「完璧」にはできない。**狙うのは「規則を忘れた・
読み替えた場合の素の呼び出し」だけであり、誤爆（無関係な作業を止めること）を
見逃し（素通り）より重く見る。**

**完璧にできない理由。**`eval "gh pr merge 94"` / `bash -c "…"` / 変数越しの呼び出しの
ように、文字列の中に隠す形は字句解析では見つけられない。この hook は `Bash` ツールに
渡す `command` 文字列を実行の前に読むだけで、実際にシェルへ渡して実行するわけでは
ない（[.claude/hooks/block-merge-without-review.py:2-91](../../.claude/hooks/block-merge-without-review.py#L2-L91) 冒頭の docstring 参照）。

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

## 1-3. どこまで直し、どこから追いかけないか

**言いたいこと。実際に踏まない形は追いかけない。**この hook は完璧にはできないので、
**素直な呼び出しを止めることに絞る。**レビューで挙がった指摘も、この線で採否を決める。

**直す（実際に踏む）。**日常の作業で普通に書く形が、誤って止まる／誤って素通りするもの。

| 直したもの | どう踏むか |
| --- | --- |
| `<<` の誤認 | `echo 'a << b'` や `<<<`、CRLF の heredoc で、以降の行がまるごと落ちて素通りする |
| `#` の中の branch 名 | 行末コメントに `gh pr ready <branch名>` と書くだけで止まる |
| 問い合わせ先の食い違い | worktree の中では、別のリポジトリの同じ番号の PR を見に行く |

**追いかけない（実際に踏まない）。**次のものは、指摘としては正しいが、**そう書く場面が
無いか、書いても実害が出ない。**直すと解析が複雑になり、そのぶん新しい誤爆を生む。

- `--repo` の重複／番号があると branch 名を見ない／壊れた解析で途中まで拾う
- 行継続と heredoc の順／`github.com` 以外のホスト／`;;` と `|&`
- ホストの違う同名リポジトリ／テストが場所に依存／逃がし口のテストが名前依存
- 目印の二重定義／同じ番号を2度引く／届かない条件

**判断の基準。**「その形を、人がこのリポジトリの作業で実際に書くか」だけを見る。
**書かないものは、直さない。**

---

## 1-4. scripts/check-release-ready.sh と揃えないといけない箇所

**言いたいこと。**同じ「レビュー済みとみなす条件」を、Python と jq の2箇所に別々に
書いている。**片方だけ直すと、マージの検査とリリース前の検査が食い違う。**

| 条件 | Python 側 | jq 側 |
| --- | --- | --- |
| 目印の文字列と、先頭にあること | `MARKER`（[.claude/hooks/block-merge-without-review.py:104](../../.claude/hooks/block-merge-without-review.py#L104)）と `count_trusted_reviews()` | `review_of()` の `test("^\\s*<!-- code-review-result -->")` |
| 信頼する投稿者 | `TRUSTED_ASSOCIATIONS` | `review_of()` の `["OWNER", "MEMBER", "COLLABORATOR"]` |

**揃っていることは機械で確かめる。**
[.claude/hooks/tests/test_block_merge_without_review.py](../../.claude/hooks/tests/test_block_merge_without_review.py)
の `run_associations_sync_case()`（信頼する投稿者の一覧）と `run_marker_sync_case()`
（目印の文字列と、先頭にあることを求める条件）が、2つのファイルを読み比べて確かめる。
**どちらかを直したら、必ずこのテストを流してから直したと言うこと。**
