package scaffold

// ciTemplate は continuo init が書き出す continuo-ci.yaml の雛形である（設計 5-3o）。
//
// **これは設定ではない。**continuo は起動時にこのファイルを1バイトも読まない。
// **利用者が中身を確かめてから .github/workflows/ へ移すための見本である。**
// そのため、書けなくても continuo init は成功で終える（設計 5-3o）。
//
// **目印の文字列と job の名前を、設定から変えられる形にしてはならない。**
// 目印は internal/prompt/builtin.md がエージェントに書かせる文字列と対でしか意味を持たず、
// 組み込みは実行ファイルの中にあって利用者が変えられない。片方だけ変えられる口を開けると、
// 「CI が探す目印」と「エージェントが書く目印」が食い違う状態を、誰にも気づけない形で作れる。
//
// **プレースホルダを埋める口だけは、WORKFLOW.md の雛形と同じ経路に載せてある**
// （TemplateWithValues と同じく CITemplateWithValues を通す）。
// **いま埋める値は0個である。**上の理由で目印と job 名を渡さないため、埋めるものが無い。
// 値を足すときは、組み込みの側にも同じ値が届く形にしてから足すこと。
//
// 文字列リテラルとして持つのは WORKFLOW.md の雛形と同じ理由である。
// 構造体から yaml.Marshal すると YAML のコメントが全部消え、雛形として役に立たなくなる。
//
// **この雛形には backtick を1文字も書かない。**Go の raw string には backtick を置けないので、
// 書こうとすると文字列を何度も連結することになり、雛形そのものが読めなくなる。
// **markdown のコード表記は使わず、地の文で書く。**GITHUB_STEP_SUMMARY はそれでも読める。
//
// **中身が YAML として読めることは test/internal/scaffold/ci_template_test.go が押さえる。**
// 壊れた YAML でも Go のビルドは通り、テストも通り、そのまま配られてしまう。
// **そして GitHub Actions は、読めない workflow を「検査が無い」として扱う。**
// 画面では「まだ走っていない」と見分けが付かない。
const ciTemplate = `# レビュー結果が貼られていない pull request を落とす。
#
# **continuo init が置いた見本です。**ここに在るだけでは何も起きません。
# **中身を確かめてから .github/workflows/ へ移してください。**
# 既に CI がある場合は、この中身を既存の CI へ組み込むよう、お使いの AI に頼んでください。
#
# **2つの検査を持ちます。**
#
#   design-review-result   設計のレビュー結果が、紐づく issue のコメントに貼られているか
#   code-review-result     実装のレビュー結果が、この pull request のコメントに貼られているか
#
# **どちらも「レビューを飛ばして先へ進む」ことを止めるためのものです。**
# **文書に書くだけでは守られません。**continuo の開発で実際に起きました
# （2026-08-29、12本をレビューせずにマージし、あとから回し直すことになりました）。
#
# **目印の文字列を変えないでください。**continuo がエージェントへ送る指示書と対になっています。
# 送られる文面は continuo prompt --show で読めます。
# **片方だけ変えると、CI が探す目印とエージェントが書く目印が食い違い、誰にも気づけません。**
#
# **job の名前も変えないでください。**branch protection の必須の検査は、この名前で登録します。
# **名前を変えると設定が宙に浮き、検査が無いのにマージできる状態になります。**
#
# **赤いだけではマージを止められません。**必須の検査への登録は、人間が1回だけ行います。
# 手順は continuo の CONTRIBUTING.md の「この検査をマージの条件にする」にあります。

name: continuo-review-gate

on:
  pull_request:
    # **ready_for_review を入れます。**gh pr ready がこのイベントを起こすので、
    # 「結果を貼る → ready にする → 検査が回り直して緑」が人手なしでつながります。
    #
    # **edited は入れません。**逃がす断りは pull request の**コメント**に貼るので、
    # edited（題名・本文・base の変更）では回り直しません。
    # **断りを後から貼ったときは gh run rerun で回し直してください。**
    types: [opened, synchronize, reopened, ready_for_review]

# **読むだけでよい。**
# **叩く先は /issues/{番号}/comments ですが、相手は pull request です。**
# 紐づく issue を引く gh pr view は GraphQL を使うので、pull-requests: read が要ります。
permissions:
  issues: read
  pull-requests: read

# **concurrency を置きません。**
# 打ち切られた run は success / skipped / neutral のどれでもないので、
# **必須の検査にしたときマージを塞ぎます。**

jobs:
  # **設計のレビュー結果が、紐づく issue のコメントに貼られているか。**
  #
  # **実装のレビュー（下の job）と分けています。**1つにまとめると、
  # 赤の理由をログまで見に行くことになります。
  design-review-result:
    runs-on: ubuntu-latest
    steps:
      - name: 設計のレビュー結果が貼ってあるか
        # **checkout しません。**この job はリポジトリの中身を1つも読みません。
        env:
          GH_TOKEN: ${{ github.token }}
          REPO: ${{ github.repository }}
          PR_NUMBER: ${{ github.event.pull_request.number }}
          IS_DRAFT: ${{ github.event.pull_request.draft }}
        run: |
          set -eu

          # **数える条件は2つです。**
          #   一、目印が本文の**先頭**か、continuo:agent の印の**直後**にある。
          #       **直後を許すのは、continuo が「エージェントが書いたか」を
          #       本文の先頭ちょうどで見ているためです。**
          #   二、投稿者が OWNER / MEMBER / COLLABORATOR である。
          #
          # **バックスラッシュ s を使いません。**Python の re と jq（Oniguruma）で
          # 当たる範囲が違い、全角空白 U+3000 を前に置いた本文が片方だけ通ります。
          # **当たる文字を並べて書きます。**半角空白・タブ・CR・LF の4文字だけです。

          # 段1. 設計のレビューを飛ばす断りが貼られているか。
          #
          # **pull request の本文ではなく、コメントを見ます。**上の2つの条件をそのまま掛けられて、
          # 数え方を1組で済ませられるためです。
          # **エージェントから守れるわけではありません。**エージェントが owner の資格情報で
          # 叩けば、その投稿も OWNER になります。**この逃がし口は自己申告です。**
          #
          # **理由は目印の次の行に書かせます。**目印の中へ書かせると、
          # 閉じの2文字を理由と読んでしまい、理由が空でも通ります。
          gh api --paginate "repos/${REPO}/issues/${PR_NUMBER}/comments?per_page=100" --jq '
            .[]
            | select((.body // "") | test("^[ \\t\\r\\n]*<!-- design-review-skipped -->[ \\t\\r\\n]*[^ \\t\\r\\n]"))
            | select(.author_association == "OWNER" or .author_association == "MEMBER" or .author_association == "COLLABORATOR")
            | .id' > skipped.txt
          if [ "$(wc -l < skipped.txt | tr -d ' ')" -gt 0 ]; then
            echo "設計のレビューを飛ばす断りが貼られています" | tee -a "${GITHUB_STEP_SUMMARY}"
            exit 0
          fi

          # 段2. 紐づく issue を引く。
          #
          # **取れなかったことと、0件だったことを分けます。**混ぜると、引き方を間違えた日から
          # 全部の pull request が「issue が無い」に落ち、**断りを書くのが正しい手順になります。**
          #
          # **closingIssuesReferences は REST では返りません。**gh pr view（GraphQL）で引きます。
          #
          # **本文の Closes / Fixes / Resolves は走査しません。**
          # **走査すると、コードの囲みや表の中の文字列まで拾います。**
          # 下のループは目印が1件見つかった時点で通すので、
          # **無関係の issue に目印があると、設計のレビューを1度もせずに緑になります。**
          if ! gh pr view "${PR_NUMBER}" --repo "${REPO}" --json closingIssuesReferences > pr.json; then
            echo "紐づく issue を引けませんでした（権限か gh の版を確かめてください）" \
              | tee -a "${GITHUB_STEP_SUMMARY}"
            exit 1
          fi

          # **このリポジトリの issue だけを残します。**別のリポジトリの issue は、この job の
          # 権限では読めません（private なら 404、public でも投稿者の立場が変わります）。
          jq -r --arg repo "${REPO}" '
            [ .closingIssuesReferences[]
              | select(.url | startswith("https://github.com/" + $repo + "/issues/"))
              | .number
            ] | unique | .[]' pr.json > issues.txt
          jq -r --arg repo "${REPO}" '
            .closingIssuesReferences[]
            | select(.url | startswith("https://github.com/" + $repo + "/issues/") | not)
            | .url' pr.json > outside.txt

          # 段3. その issue のどれか1件に、設計のレビュー結果が貼られているか。
          #
          # **1件でよい。**グループでまとめて直す pull request では、代表の issue にだけ
          # 設計が書かれます。全部に求めると、代表以外へ同じものを貼ることになります。
          # **1件が読めなくても、残りを見に行きます。**set -e の下で gh api が落ちると
          # step ごと終わるので、後ろの issue に目印があっても数えません。
          # **読めなかったことは数えておき、落ちるときの案内に出します**
          # （「無かった」と「読めなかった」を人間が見分けられるようにするためです）。
          found=0
          unreadable=0
          while read -r n; do
            [ -n "${n}" ] || continue
            if ! gh api --paginate "repos/${REPO}/issues/${n}/comments?per_page=100" --jq '
              .[]
              | select((.body // "") | test("^[ \\t\\r\\n]*(<!-- continuo:agent -->[ \\t\\r\\n]*)?<!-- design-review-result -->"))
              | select(.author_association == "OWNER" or .author_association == "MEMBER" or .author_association == "COLLABORATOR")
              | .id' > matched.txt; then
              echo "issue #${n} のコメントを読めませんでした（飛ばします）"
              unreadable=$((unreadable + 1))
              continue
            fi
            if [ "$(wc -l < matched.txt | tr -d ' ')" -gt 0 ]; then
              echo "設計のレビュー結果=有り（issue #${n}）"
              echo "設計のレビュー結果=有り（issue #${n}）" >> "${GITHUB_STEP_SUMMARY}"
              found=1
              break
            fi
          done < issues.txt
          if [ "${found}" -eq 1 ]; then
            exit 0
          fi

          # **落ちたときは、どうすれば通るかを全部書きます。**
          {
            echo "## 設計のレビュー結果が貼られていません"
            echo ""
            if [ "$(wc -l < issues.txt | tr -d ' ')" -eq 0 ]; then
              echo "**この pull request に紐づく issue が1件もありません。**"
              echo ""
              if [ "$(wc -l < outside.txt | tr -d ' ')" -gt 0 ]; then
                echo "別のリポジトリの issue は見つかりましたが、この検査は読めません。"
                sed 's/^/- /' outside.txt
                echo ""
              fi
              echo "本文へ Closes #<番号> を書くか、下の断りを貼ってください。"
            else
              echo "紐づく issue に、次の目印で始まるコメントが1件もありません。"
              echo ""
              echo "    <!-- design-review-result -->"
              echo ""
              sed 's/^/- issue #/' issues.txt
              if [ "${unreadable}" -gt 0 ]; then
                echo ""
                echo "**このうち ${unreadable} 件は、コメントを読めませんでした。**"
                echo "**「貼られていない」ではなく「確かめられなかった」です。**"
              fi
            fi
            echo ""
            echo "**通し方。**"
            echo ""
            echo "1. 設計をサブエージェントにレビューさせる"
            echo "2. 指摘ごとに「直すか / 直さないか」と理由を書いた判断票を作る"
            echo "3. それを **issue のコメント**として貼る。**先頭の3行をこの順で置く**"
            echo ""
            echo "    <!-- continuo:agent -->"
            echo "    <!-- design-review-result -->"
            echo "    <!-- continuo:ai -->"
            echo ""
            echo "   **3行目は「機械が書いた」という印です。**落としても検査は通りますが、"
            echo "   **そのコメントだけ、人間が書いたものと見分けが付かなくなります。**"
            echo ""
            echo "**設計のレビューが要らない変更のとき**（文書だけの変更、他に影響しない1行の修正）**は、"
            echo "この pull request のコメントに断りを貼ってください。**"
            echo ""
            echo "    <!-- design-review-skipped -->"
            echo "    文書だけの変更のため"
            echo ""
            echo "**この断りには <!-- continuo:ai --> を付けないでください。**"
            echo "この検査は、目印の直後に空白でない文字が1つあるかで**理由を書いたか**を数えています。"
            echo "**印を足すと、理由を1文字も書かない断りが通ります。**"
            echo ""
            echo "**2行目の理由を落とさないでください。**目印だけでは通りません。"
            echo ""
            echo "**数える条件。**"
            echo ""
            echo "- 目印が**本文の先頭**にあること。途中に書いたものは数えません"
            echo "- 投稿者が **OWNER / MEMBER / COLLABORATOR** であること"
            if [ "${IS_DRAFT}" = "true" ]; then
              echo ""
              echo "**この pull request は draft です。**draft のうちは赤のままでかまいません。"
            fi
          } | tee -a "${GITHUB_STEP_SUMMARY}"

          exit 1

  # **実装のレビュー結果が、この pull request のコメントに貼られているか。**
  code-review-result:
    runs-on: ubuntu-latest
    steps:
      - name: 実装のレビュー結果が貼ってあるか
        env:
          GH_TOKEN: ${{ github.token }}
          REPO: ${{ github.repository }}
          PR_NUMBER: ${{ github.event.pull_request.number }}
          IS_DRAFT: ${{ github.event.pull_request.draft }}
        run: |
          set -eu

          # **数える条件は上の job と同じ2つです。**
          # **数えるのを gh と同じパイプラインに置きません。**gh が落ちても wc の終了状態が
          # 返るので、**取れなかったのか無かったのかを区別できなくなります。**
          gh api --paginate "repos/${REPO}/issues/${PR_NUMBER}/comments?per_page=100" --jq '
            .[]
            | select((.body // "") | test("^[ \\t\\r\\n]*<!-- code-review-result -->"))
            | select(.author_association == "OWNER" or .author_association == "MEMBER" or .author_association == "COLLABORATOR")
            | .id' > matched.txt
          count="$(wc -l < matched.txt | tr -d ' ')"

          if [ "${count}" -gt 0 ]; then
            echo "実装のレビュー結果=有り（${count}件）"
            echo "実装のレビュー結果=有り（${count}件）" >> "${GITHUB_STEP_SUMMARY}"
            exit 0
          fi

          {
            echo "## 実装のレビュー結果が貼られていません"
            echo ""
            echo "この pull request のコメントに、次の目印で始まるものが1件もありません。"
            echo ""
            echo "    <!-- code-review-result -->"
            echo ""
            echo "**通し方。**"
            echo ""
            echo "1. コードのレビューを回す"
            echo "2. その結果を、この pull request のコメントとして貼る"
            echo "3. **目印はコメントの先頭に置く**"
            echo ""
            echo "    <!-- code-review-result -->"
            echo ""
            echo "**数える条件。**"
            echo ""
            echo "- 目印が**本文の先頭**にあること。途中に書いたものは数えません"
            echo "- 投稿者が **OWNER / MEMBER / COLLABORATOR** であること"
            if [ "${IS_DRAFT}" = "true" ]; then
              echo ""
              echo "**この pull request は draft です。**draft のうちは赤のままでかまいません。"
              echo "結果を貼ってから gh pr ready ${PR_NUMBER} を打ってください。"
            else
              echo ""
              echo "**この pull request は draft ではありません。**"
              echo "gh pr ready を打っても、この検査は回り直しません。"
              echo "gh run rerun を使うか、commit を1つ push してください。"
            fi
          } | tee -a "${GITHUB_STEP_SUMMARY}"

          exit 1
`

// CITemplate は書き出す continuo-ci.yaml の中身を、プレースホルダを埋めずにそのまま返す。
//
// **埋める値はいま0個である**（上の ciTemplate の説明）。
// それでも WORKFLOW.md の雛形と同じ形の口を置いてあるのは、
// **「必要に応じて WORKFLOW.md の内容で置き換えられる形にしておく」と決めたためである。**
// 値を足すときは CITemplateWithValues の側へ書く。
//
// 戻り値: continuo-ci.yaml の全文。
func CITemplate() string {
	return ciTemplate
}

// CITemplateWithValues は、continuo-ci.yaml の雛形に values を埋めて返す。
//
// **いま埋める値は0個なので、values は使わない。**
// 引数を受け取る形にしてあるのは、WORKFLOW.md の雛形（TemplateWithValues）と
// 同じ経路に載せるためである。**片方だけ別の形にすると、値を足すときに
// 呼び出し側を全部書き換えることになる。**
//
// values: WORKFLOW.md の front matter へ埋める値と同じもの。いまは参照しない。
// 戻り値: continuo-ci.yaml の全文。
func CITemplateWithValues(values Values) string {
	_ = values
	return ciTemplate
}
